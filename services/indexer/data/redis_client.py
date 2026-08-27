import base64
import binascii
import hashlib
import logging
import re
import uuid
from typing import Optional, Tuple

import redis

from models.outlinks import Outlinks
from models.page import Page
from utils.constants import *


logger = logging.getLogger(__name__)
PUBLICATION_ID_PATTERN = re.compile(r"^[0-9a-f]{64}$")
MAX_ENCODED_URL_BYTES = (MAX_CANONICAL_URL_BYTES * 4 + 2) // 3


class LockLostError(RuntimeError):
    pass


class InvalidPublicationKey(ValueError):
    pass


class PageDataDecodeError(ValueError):
    def __init__(self, message: str, normalized_url: Optional[str] = None):
        super().__init__(message)
        self.normalized_url = normalized_url


def validate_redis_auth(username: str, password: str, allow_insecure: bool) -> None:
    if username and not password:
        raise ValueError("REDIS_USERNAME requires REDIS_PASSWORD")
    if not password and not allow_insecure:
        raise ValueError(
            "Redis authentication is required unless "
            "ALLOW_INSECURE_DATASTORES=true is explicitly set for local testing"
        )


def claim_reference(queue_value: str) -> str:
    """Return a non-reversible log reference without exposing crawled URLs."""
    return hashlib.sha256(str(queue_value).encode()).hexdigest()[:16]


def parse_page_publication_key(page_key: str) -> Tuple[str, str, str]:
    """Validate and decode an immutable publication key with bounded allocation."""
    if not isinstance(page_key, str):
        raise InvalidPublicationKey("queue value must be text")
    parts = page_key.split(":", 2)
    if len(parts) != 3 or parts[0] != PAGE_PREFIX:
        raise InvalidPublicationKey("queue value is not a page publication key")
    publication_id, encoded_url = parts[1], parts[2]
    if not PUBLICATION_ID_PATTERN.fullmatch(publication_id) or not encoded_url:
        raise InvalidPublicationKey("queue value has an invalid publication identity")
    # Base64url expands by 4/3. Reject oversized input before decode allocates.
    if len(encoded_url) > MAX_ENCODED_URL_BYTES:
        raise InvalidPublicationKey("queue value exceeds the canonical URL size limit")
    if not encoded_url.isascii():
        raise InvalidPublicationKey("queue value URL encoding must be ASCII")
    try:
        padding = "=" * (-len(encoded_url) % 4)
        url_bytes = base64.b64decode(
            encoded_url + padding, altchars=b"-_", validate=True
        )
        if len(url_bytes) > MAX_CANONICAL_URL_BYTES:
            raise InvalidPublicationKey("decoded canonical URL exceeds 2048 bytes")
        normalized_url = url_bytes.decode("utf-8")
    except (UnicodeDecodeError, ValueError, binascii.Error) as error:
        if isinstance(error, InvalidPublicationKey):
            raise
        raise InvalidPublicationKey("queue value has an invalid URL encoding") from error
    canonical_encoding = base64.urlsafe_b64encode(url_bytes).decode().rstrip("=")
    if not normalized_url or canonical_encoding != encoded_url:
        raise InvalidPublicationKey("queue value has a non-canonical URL encoding")
    return (
        publication_id,
        normalized_url,
        f"{OUTLINKS_PREFIX}:{publication_id}:{encoded_url}",
    )


def image_manifest_key_from_page_key(page_key: str) -> str:
    publication_id, _, _ = parse_page_publication_key(page_key)
    encoded_url = page_key.split(":", 2)[2]
    return f"{PAGE_IMAGES_PREFIX}:{publication_id}:{encoded_url}"


# Epoch values are intentionally limited to Redis Lua's exact integer range.
# Every owner-fenced script validates lock/counter types and compares both the
# random token+epoch lock value and the current monotonic epoch before writing.
ACQUIRE_LOCK_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local counter_type = redis.call('TYPE', KEYS[2])['ok']
if lock_type ~= 'none' and lock_type ~= 'string' then return redis.error_reply('owner lock must be a string') end
if counter_type ~= 'none' and counter_type ~= 'string' then return redis.error_reply('fence epoch counter must be a string') end
if redis.call('EXISTS', KEYS[1]) == 1 then return false end
local current = redis.call('GET', KEYS[2])
if current and (not string.match(current, '^[1-9][0-9]*$') or tonumber(current) >= 9007199254740991) then
  return redis.error_reply('invalid fence epoch counter')
end
redis.call('INCR', KEYS[2])
local epoch = redis.call('GET', KEYS[2])
redis.call('SET', KEYS[1], ARGV[1] .. ':' .. epoch, 'PX', ARGV[2])
return epoch
"""

RENEW_LOCK_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local counter_type = redis.call('TYPE', KEYS[2])['ok']
if lock_type ~= 'string' or counter_type ~= 'string' then return redis.error_reply('owner lock and epoch must be strings') end
if redis.call('GET', KEYS[2]) ~= ARGV[2] then return 0 end
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
return redis.call('PEXPIRE', KEYS[1], ARGV[3])
"""

DROP_LOCK_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local counter_type = redis.call('TYPE', KEYS[2])['ok']
if lock_type ~= 'string' or counter_type ~= 'string' then return 0 end
if redis.call('GET', KEYS[2]) ~= ARGV[2] then return 0 end
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
return redis.call('DEL', KEYS[1])
"""

CLAIM_PAGE_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local counter_type = redis.call('TYPE', KEYS[2])['ok']
local queue_type = redis.call('TYPE', KEYS[3])['ok']
local processing_type = redis.call('TYPE', KEYS[4])['ok']
if lock_type ~= 'string' or counter_type ~= 'string' then return redis.error_reply('owner lock and epoch must be strings') end
if queue_type ~= 'none' and queue_type ~= 'list' then return redis.error_reply('queue key must be a list') end
if processing_type ~= 'none' and processing_type ~= 'list' then return redis.error_reply('processing key must be a list') end
if redis.call('GET', KEYS[2]) ~= ARGV[2] or redis.call('GET', KEYS[1]) ~= ARGV[1] then return redis.error_reply('LOCK_LOST') end
return redis.call('RPOPLPUSH', KEYS[3], KEYS[4])
"""

RECOVER_CLAIMS_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local counter_type = redis.call('TYPE', KEYS[2])['ok']
local processing_type = redis.call('TYPE', KEYS[3])['ok']
local queue_type = redis.call('TYPE', KEYS[4])['ok']
if lock_type ~= 'string' or counter_type ~= 'string' then return redis.error_reply('owner lock and epoch must be strings') end
if processing_type ~= 'none' and processing_type ~= 'list' then return redis.error_reply('processing key must be a list') end
if queue_type ~= 'none' and queue_type ~= 'list' then return redis.error_reply('queue key must be a list') end
if redis.call('GET', KEYS[2]) ~= ARGV[2] or redis.call('GET', KEYS[1]) ~= ARGV[1] then return redis.error_reply('LOCK_LOST') end
local limit = tonumber(ARGV[3])
if not limit or limit < 1 or limit > 1000 then return redis.error_reply('invalid recovery limit') end
local recovered = 0
while recovered < limit do
  local value = redis.call('LINDEX', KEYS[3], 0)
  if not value then break end
  redis.call('RPUSH', KEYS[4], value)
  redis.call('LPOP', KEYS[3])
  recovered = recovered + 1
end
return recovered
"""

RELEASE_CLAIM_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local counter_type = redis.call('TYPE', KEYS[2])['ok']
local processing_type = redis.call('TYPE', KEYS[3])['ok']
local queue_type = redis.call('TYPE', KEYS[4])['ok']
if lock_type ~= 'string' or counter_type ~= 'string' then return redis.error_reply('owner lock and epoch must be strings') end
if processing_type ~= 'none' and processing_type ~= 'list' then return redis.error_reply('processing key must be a list') end
if queue_type ~= 'none' and queue_type ~= 'list' then return redis.error_reply('queue key must be a list') end
if redis.call('GET', KEYS[2]) ~= ARGV[2] or redis.call('GET', KEYS[1]) ~= ARGV[1] then return redis.error_reply('LOCK_LOST') end
if not redis.call('LPOS', KEYS[3], ARGV[3]) then return 0 end
redis.call('RPUSH', KEYS[4], ARGV[3])
redis.call('LREM', KEYS[3], 1, ARGV[3])
return 1
"""

COMPLETE_CLAIM_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local counter_type = redis.call('TYPE', KEYS[2])['ok']
local processing_type = redis.call('TYPE', KEYS[3])['ok']
local page_type = redis.call('TYPE', KEYS[4])['ok']
local outlinks_type = redis.call('TYPE', KEYS[5])['ok']
local manifest_type = redis.call('TYPE', KEYS[6])['ok']
local images_type = redis.call('TYPE', KEYS[7])['ok']
if lock_type ~= 'string' or counter_type ~= 'string' then return redis.error_reply('owner lock and epoch must be strings') end
if processing_type ~= 'none' and processing_type ~= 'list' then return redis.error_reply('processing key must be a list') end
if page_type ~= 'none' and page_type ~= 'hash' then return redis.error_reply('page key must be a hash') end
if outlinks_type ~= 'none' and outlinks_type ~= 'set' then return redis.error_reply('outlinks key must be a set') end
if manifest_type ~= 'hash' then return redis.error_reply('image manifest key must be a hash') end
if images_type ~= 'none' and images_type ~= 'list' then return redis.error_reply('image queue key must be a list') end
if redis.call('GET', KEYS[2]) ~= ARGV[2] or redis.call('GET', KEYS[1]) ~= ARGV[1] then return redis.error_reply('LOCK_LOST') end
if not redis.call('LPOS', KEYS[3], ARGV[3]) then return 0 end
if redis.call('HGET', KEYS[6], 'publication_id') ~= ARGV[4] then return redis.error_reply('image manifest publication mismatch') end
if redis.call('HGET', KEYS[6], 'normalized_url') ~= ARGV[5] then return redis.error_reply('image manifest URL mismatch') end
redis.call('LPUSH', KEYS[7], KEYS[6])
redis.call('LREM', KEYS[3], 1, ARGV[3])
redis.call('DEL', KEYS[4], KEYS[5])
return 1
"""

SKIP_CLAIM_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local counter_type = redis.call('TYPE', KEYS[2])['ok']
local processing_type = redis.call('TYPE', KEYS[3])['ok']
local page_type = redis.call('TYPE', KEYS[4])['ok']
local outlinks_type = redis.call('TYPE', KEYS[5])['ok']
local manifest_type = redis.call('TYPE', KEYS[6])['ok']
local images_type = redis.call('TYPE', KEYS[7])['ok']
if lock_type ~= 'string' or counter_type ~= 'string' then return redis.error_reply('owner lock and epoch must be strings') end
if processing_type ~= 'none' and processing_type ~= 'list' then return redis.error_reply('processing key must be a list') end
if page_type ~= 'none' and page_type ~= 'hash' then return redis.error_reply('page key must be a hash') end
if outlinks_type ~= 'none' and outlinks_type ~= 'set' then return redis.error_reply('outlinks key must be a set') end
if manifest_type ~= 'hash' then return redis.error_reply('image manifest key must be a hash') end
if images_type ~= 'none' and images_type ~= 'list' then return redis.error_reply('image queue key must be a list') end
if redis.call('GET', KEYS[2]) ~= ARGV[2] or redis.call('GET', KEYS[1]) ~= ARGV[1] then return redis.error_reply('LOCK_LOST') end
if not redis.call('LPOS', KEYS[3], ARGV[3]) then return 0 end
if redis.call('HGET', KEYS[6], 'publication_id') ~= ARGV[4] then return redis.error_reply('image manifest publication mismatch') end
if redis.call('HGET', KEYS[6], 'normalized_url') ~= ARGV[5] then return redis.error_reply('image manifest URL mismatch') end
redis.call('LPUSH', KEYS[7], KEYS[6])
redis.call('LREM', KEYS[3], 1, ARGV[3])
redis.call('DEL', KEYS[4], KEYS[5])
return 1
"""

QUARANTINE_CLAIM_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local counter_type = redis.call('TYPE', KEYS[2])['ok']
local processing_type = redis.call('TYPE', KEYS[3])['ok']
local dead_type = redis.call('TYPE', KEYS[4])['ok']
if lock_type ~= 'string' or counter_type ~= 'string' then return redis.error_reply('owner lock and epoch must be strings') end
if processing_type ~= 'none' and processing_type ~= 'list' then return redis.error_reply('processing key must be a list') end
if dead_type ~= 'none' and dead_type ~= 'list' then return redis.error_reply('dead-letter key must be a list') end
if redis.call('GET', KEYS[2]) ~= ARGV[2] or redis.call('GET', KEYS[1]) ~= ARGV[1] then return redis.error_reply('LOCK_LOST') end
if not redis.call('LPOS', KEYS[3], ARGV[3]) then return 0 end
local dead_limit = tonumber(ARGV[4])
if not dead_limit or dead_limit < 1 or dead_limit > 10000 then return redis.error_reply('invalid dead-letter limit') end
redis.call('LPUSH', KEYS[4], ARGV[3])
redis.call('LTRIM', KEYS[4], 0, dead_limit - 1)
redis.call('LREM', KEYS[3], 1, ARGV[3])
return 1
"""

WORK_SIZES_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local counter_type = redis.call('TYPE', KEYS[2])['ok']
local queue_type = redis.call('TYPE', KEYS[3])['ok']
local processing_type = redis.call('TYPE', KEYS[4])['ok']
if lock_type ~= 'string' or counter_type ~= 'string' then return redis.error_reply('owner lock and epoch must be strings') end
if queue_type ~= 'none' and queue_type ~= 'list' then return redis.error_reply('queue key must be a list') end
if processing_type ~= 'none' and processing_type ~= 'list' then return redis.error_reply('processing key must be a list') end
if redis.call('GET', KEYS[2]) ~= ARGV[2] or redis.call('GET', KEYS[1]) ~= ARGV[1] then return redis.error_reply('LOCK_LOST') end
return {redis.call('LLEN', KEYS[3]), redis.call('LLEN', KEYS[4])}
"""


class RedisClient:
    def __init__(
        self,
        host="localhost",
        port=6379,
        username="",
        password="",
        db=0,
        decode_responses=True,
        socket_timeout=5,
        owner_token=None,
        allow_insecure=False,
    ):
        username = username or ""
        password = password or ""
        validate_redis_auth(username, password, allow_insecure)
        self.owner_token = owner_token or uuid.uuid4().hex
        self.owner_epoch = None
        try:
            self.client = redis.Redis(
                host=host,
                port=port,
                username=username or None,
                password=password,
                db=db,
                decode_responses=decode_responses,
                socket_connect_timeout=socket_timeout,
                socket_timeout=socket_timeout,
            )
            self.client.ping()
        except Exception:
            logger.error("Failed to connect to Redis")
            self.client = None

    def _require_client(self):
        if self.client is None:
            raise redis.RedisError("Redis connection not initialized")

    @property
    def owner_value(self):
        if self.owner_epoch is None:
            raise LockLostError("indexer has no fencing epoch")
        return f"{self.owner_token}:{self.owner_epoch}"

    def acquire_lock(self, ttl_seconds=INDEXER_LOCK_TTL_SECONDS) -> bool:
        self._require_client()
        result = self.client.eval(
            ACQUIRE_LOCK_SCRIPT,
            2,
            INDEXER_OWNER_LOCK_KEY,
            INDEXER_FENCE_EPOCH_KEY,
            self.owner_token,
            int(ttl_seconds * 1000),
        )
        if result is None:
            return False
        self.owner_epoch = int(result)
        return True

    def renew_lock(self, ttl_seconds=INDEXER_LOCK_TTL_SECONDS) -> bool:
        self._require_client()
        return int(
            self.client.eval(
                RENEW_LOCK_SCRIPT,
                2,
                INDEXER_OWNER_LOCK_KEY,
                INDEXER_FENCE_EPOCH_KEY,
                self.owner_value,
                str(self.owner_epoch),
                int(ttl_seconds * 1000),
            )
        ) == 1

    def release_lock(self) -> bool:
        if self.client is None or self.owner_epoch is None:
            return False
        return int(
            self.client.eval(
                DROP_LOCK_SCRIPT,
                2,
                INDEXER_OWNER_LOCK_KEY,
                INDEXER_FENCE_EPOCH_KEY,
                self.owner_value,
                str(self.owner_epoch),
            )
        ) == 1

    def claim_page(self) -> Optional[str]:
        self._require_client()
        return self.client.eval(
            CLAIM_PAGE_SCRIPT,
            4,
            INDEXER_OWNER_LOCK_KEY,
            INDEXER_FENCE_EPOCH_KEY,
            INDEXER_QUEUE_KEY,
            INDEXER_PROCESSING_QUEUE_KEY,
            self.owner_value,
            str(self.owner_epoch),
        )

    def recover_abandoned_claims(self, limit=INDEXER_RECOVERY_BATCH_SIZE) -> int:
        self._require_client()
        return int(
            self.client.eval(
                RECOVER_CLAIMS_SCRIPT,
                4,
                INDEXER_OWNER_LOCK_KEY,
                INDEXER_FENCE_EPOCH_KEY,
                INDEXER_PROCESSING_QUEUE_KEY,
                INDEXER_QUEUE_KEY,
                self.owner_value,
                str(self.owner_epoch),
                limit,
            )
        )

    def release_page(self, page_key: str) -> bool:
        try:
            return int(
                self.client.eval(
                    RELEASE_CLAIM_SCRIPT,
                    4,
                    INDEXER_OWNER_LOCK_KEY,
                    INDEXER_FENCE_EPOCH_KEY,
                    INDEXER_PROCESSING_QUEUE_KEY,
                    INDEXER_QUEUE_KEY,
                    self.owner_value,
                    str(self.owner_epoch),
                    page_key,
                )
            ) == 1
        except Exception:
            logger.error("Could not release claim=%s", claim_reference(page_key))
            return False

    def quarantine_page(self, queue_value: str) -> bool:
        try:
            return int(
                self.client.eval(
                    QUARANTINE_CLAIM_SCRIPT,
                    4,
                    INDEXER_OWNER_LOCK_KEY,
                    INDEXER_FENCE_EPOCH_KEY,
                    INDEXER_PROCESSING_QUEUE_KEY,
                    INDEXER_DEAD_LETTER_QUEUE_KEY,
                    self.owner_value,
                    str(self.owner_epoch),
                    queue_value,
                    INDEXER_DEAD_LETTER_MAX_ENTRIES,
                )
            ) == 1
        except Exception:
            logger.error("Could not quarantine claim=%s", claim_reference(queue_value))
            return False

    def get_work_sizes(self) -> Tuple[int, int]:
        self._require_client()
        queued, processing = self.client.eval(
            WORK_SIZES_SCRIPT,
            4,
            INDEXER_OWNER_LOCK_KEY,
            INDEXER_FENCE_EPOCH_KEY,
            INDEXER_QUEUE_KEY,
            INDEXER_PROCESSING_QUEUE_KEY,
            self.owner_value,
            str(self.owner_epoch),
        )
        return int(queued), int(processing)

    def signal_crawler(self) -> bool:
        self._require_client()
        if not self.renew_lock():
            return False
        return self.client.lpush(SIGNAL_QUEUE_KEY, RESUME_CRAWL) > 0

    def complete_page(self, page_key: str, normalized_url: str) -> bool:
        try:
            publication_id, key_url, outlinks_key = parse_page_publication_key(page_key)
            if key_url != normalized_url:
                return False
            manifest_key = image_manifest_key_from_page_key(page_key)
            return int(
                self.client.eval(
                    COMPLETE_CLAIM_SCRIPT,
                    7,
                    INDEXER_OWNER_LOCK_KEY,
                    INDEXER_FENCE_EPOCH_KEY,
                    INDEXER_PROCESSING_QUEUE_KEY,
                    page_key,
                    outlinks_key,
                    manifest_key,
                    IMAGE_INDEXER_QUEUE_KEY,
                    self.owner_value,
                    str(self.owner_epoch),
                    page_key,
                    publication_id,
                    normalized_url,
                )
            ) == 1
        except Exception:
            logger.error("Could not complete claim=%s", claim_reference(page_key))
            return False

    def skip_page(self, page_key: str) -> bool:
        try:
            publication_id, normalized_url, outlinks_key = parse_page_publication_key(page_key)
            manifest_key = image_manifest_key_from_page_key(page_key)
            return int(
                self.client.eval(
                    SKIP_CLAIM_SCRIPT,
                    7,
                    INDEXER_OWNER_LOCK_KEY,
                    INDEXER_FENCE_EPOCH_KEY,
                    INDEXER_PROCESSING_QUEUE_KEY,
                    page_key,
                    outlinks_key,
                    manifest_key,
                    IMAGE_INDEXER_QUEUE_KEY,
                    self.owner_value,
                    str(self.owner_epoch),
                    page_key,
                    publication_id,
                    normalized_url,
                )
            ) == 1
        except Exception:
            logger.error("Could not skip claim=%s", claim_reference(page_key))
            return False

    def get_page_data(self, page_key: str) -> Optional[Page]:
        self._require_client()
        publication_id, key_url, _ = parse_page_publication_key(page_key)
        page_hashed = self.client.hgetall(page_key)
        if not page_hashed:
            return None
        try:
            if page_hashed.get("publication_id") != publication_id:
                raise ValueError("page publication ID does not match immutable key")
            page = Page.from_hash(page_hashed)
            if page.normalized_url != key_url:
                raise ValueError("page normalized URL does not match immutable key")
            return page
        except (KeyError, TypeError, ValueError) as error:
            raise PageDataDecodeError(str(error), normalized_url=key_url) from error

    def get_outlinks(self, page_key: str, normalized_url: str) -> Outlinks:
        self._require_client()
        _, key_url, outlinks_key = parse_page_publication_key(page_key)
        if key_url != normalized_url:
            raise InvalidPublicationKey("outlinks URL does not match publication")
        return Outlinks(_id=normalized_url, links=self.client.smembers(outlinks_key))
