import base64
import binascii
import hashlib
import json
import logging
import re
import uuid
from dataclasses import dataclass
from typing import List, Optional, Tuple

import redis

from models.image import Image
from utils.constants import (
    IMAGE_DEAD_LETTER_MAX_ENTRIES,
    IMAGE_DEAD_LETTER_QUEUE_KEY,
    IMAGE_FENCE_EPOCH_KEY,
    IMAGE_INDEXER_QUEUE_KEY,
    IMAGE_LOCK_TTL_SECONDS,
    IMAGE_OWNER_LOCK_KEY,
    IMAGE_PREFIX,
    IMAGE_PROCESSING_QUEUE_KEY,
    IMAGE_RECOVERY_BATCH_SIZE,
    MAX_CANONICAL_URL_BYTES,
    MAX_IMAGE_ALT_BYTES,
    MAX_IMAGES_PER_PAGE,
    MAX_MANIFEST_BYTES,
    PAGE_IMAGES_PREFIX,
)


logger = logging.getLogger(__name__)
PUBLICATION_ID_PATTERN = re.compile(r"^[0-9a-f]{64}$")
MAX_ENCODED_URL_BYTES = (MAX_CANONICAL_URL_BYTES * 4 + 2) // 3


class InvalidImageWork(ValueError):
    pass


class LockLostError(RuntimeError):
    pass


def claim_reference(value: str) -> str:
    return hashlib.sha256(str(value).encode()).hexdigest()[:16]


def validate_redis_auth(username: str, password: str, allow_insecure: bool) -> None:
    if username and not password:
        raise ValueError("REDIS_USERNAME requires REDIS_PASSWORD")
    if not password and not allow_insecure:
        raise ValueError("Redis authentication is required")


def _decode_url(encoded_url: str) -> str:
    if (
        not encoded_url
        or len(encoded_url) > MAX_ENCODED_URL_BYTES
        or not encoded_url.isascii()
    ):
        raise InvalidImageWork("invalid encoded URL")
    try:
        raw = base64.b64decode(
            encoded_url + "=" * (-len(encoded_url) % 4),
            altchars=b"-_",
            validate=True,
        )
        if len(raw) > MAX_CANONICAL_URL_BYTES:
            raise InvalidImageWork("decoded URL exceeds limit")
        value = raw.decode("utf-8")
    except (UnicodeDecodeError, ValueError, binascii.Error) as error:
        if isinstance(error, InvalidImageWork):
            raise
        raise InvalidImageWork("invalid URL encoding") from error
    if not value or base64.urlsafe_b64encode(raw).decode().rstrip("=") != encoded_url:
        raise InvalidImageWork("non-canonical URL encoding")
    return value


def parse_manifest_key(manifest_key: str) -> Tuple[str, str, str]:
    if not isinstance(manifest_key, str):
        raise InvalidImageWork("queue value must be text")
    parts = manifest_key.split(":", 2)
    if (
        len(parts) != 3
        or parts[0] != PAGE_IMAGES_PREFIX
        or not PUBLICATION_ID_PATTERN.fullmatch(parts[1])
    ):
        raise InvalidImageWork("invalid manifest key")
    return parts[1], _decode_url(parts[2]), parts[2]


def parse_payload_key(payload_key: str) -> Tuple[str, str, str]:
    if not isinstance(payload_key, str):
        raise InvalidImageWork("payload key must be text")
    parts = payload_key.split(":", 3)
    if (
        len(parts) != 4
        or parts[0] != IMAGE_PREFIX
        or not PUBLICATION_ID_PATTERN.fullmatch(parts[1])
    ):
        raise InvalidImageWork("invalid payload key")
    return parts[1], _decode_url(parts[2]), _decode_url(parts[3])


@dataclass(frozen=True)
class ImageWork:
    manifest_key: str
    publication_id: str
    page_url: str
    payload_keys: List[str]
    images: List[Image]
    serialized_keys: str


ACQUIRE_LOCK_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local counter_type = redis.call('TYPE', KEYS[2])['ok']
if lock_type ~= 'none' and lock_type ~= 'string' then return redis.error_reply('owner lock must be a string') end
if counter_type ~= 'none' and counter_type ~= 'string' then return redis.error_reply('fence epoch must be a string') end
if redis.call('EXISTS', KEYS[1]) == 1 then return false end
local current = redis.call('GET', KEYS[2])
if current and (not string.match(current, '^[1-9][0-9]*$') or tonumber(current) >= 9007199254740991) then return redis.error_reply('invalid fence epoch') end
redis.call('INCR', KEYS[2])
local epoch = redis.call('GET', KEYS[2])
redis.call('SET', KEYS[1], ARGV[1] .. ':' .. epoch, 'PX', ARGV[2])
return epoch
"""

RENEW_LOCK_SCRIPT = """
if redis.call('TYPE', KEYS[1])['ok'] ~= 'string' or redis.call('TYPE', KEYS[2])['ok'] ~= 'string' then return redis.error_reply('owner keys must be strings') end
if redis.call('GET', KEYS[1]) ~= ARGV[1] or redis.call('GET', KEYS[2]) ~= ARGV[2] then return 0 end
return redis.call('PEXPIRE', KEYS[1], ARGV[3])
"""

DROP_LOCK_SCRIPT = """
if redis.call('TYPE', KEYS[1])['ok'] ~= 'string' or redis.call('TYPE', KEYS[2])['ok'] ~= 'string' then return 0 end
if redis.call('GET', KEYS[1]) ~= ARGV[1] or redis.call('GET', KEYS[2]) ~= ARGV[2] then return 0 end
return redis.call('DEL', KEYS[1])
"""

CLAIM_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local epoch_type = redis.call('TYPE', KEYS[2])['ok']
local queue_type = redis.call('TYPE', KEYS[3])['ok']
local processing_type = redis.call('TYPE', KEYS[4])['ok']
if lock_type ~= 'string' or epoch_type ~= 'string' then return redis.error_reply('owner keys must be strings') end
if queue_type ~= 'none' and queue_type ~= 'list' then return redis.error_reply('queue must be a list') end
if processing_type ~= 'none' and processing_type ~= 'list' then return redis.error_reply('processing must be a list') end
if redis.call('GET', KEYS[1]) ~= ARGV[1] or redis.call('GET', KEYS[2]) ~= ARGV[2] then return redis.error_reply('LOCK_LOST') end
return redis.call('RPOPLPUSH', KEYS[3], KEYS[4])
"""

RECOVER_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local epoch_type = redis.call('TYPE', KEYS[2])['ok']
local processing_type = redis.call('TYPE', KEYS[3])['ok']
local queue_type = redis.call('TYPE', KEYS[4])['ok']
if lock_type ~= 'string' or epoch_type ~= 'string' then return redis.error_reply('owner keys must be strings') end
if processing_type ~= 'none' and processing_type ~= 'list' then return redis.error_reply('processing must be a list') end
if queue_type ~= 'none' and queue_type ~= 'list' then return redis.error_reply('queue must be a list') end
if redis.call('GET', KEYS[1]) ~= ARGV[1] or redis.call('GET', KEYS[2]) ~= ARGV[2] then return redis.error_reply('LOCK_LOST') end
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

RELEASE_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local epoch_type = redis.call('TYPE', KEYS[2])['ok']
local processing_type = redis.call('TYPE', KEYS[3])['ok']
local queue_type = redis.call('TYPE', KEYS[4])['ok']
if lock_type ~= 'string' or epoch_type ~= 'string' then return redis.error_reply('owner keys must be strings') end
if processing_type ~= 'none' and processing_type ~= 'list' then return redis.error_reply('processing must be a list') end
if queue_type ~= 'none' and queue_type ~= 'list' then return redis.error_reply('queue must be a list') end
if redis.call('GET', KEYS[1]) ~= ARGV[1] or redis.call('GET', KEYS[2]) ~= ARGV[2] then return redis.error_reply('LOCK_LOST') end
if not redis.call('LPOS', KEYS[3], ARGV[3]) then return 0 end
redis.call('RPUSH', KEYS[4], ARGV[3])
redis.call('LREM', KEYS[3], 1, ARGV[3])
return 1
"""

QUARANTINE_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local epoch_type = redis.call('TYPE', KEYS[2])['ok']
local processing_type = redis.call('TYPE', KEYS[3])['ok']
local dead_type = redis.call('TYPE', KEYS[4])['ok']
if lock_type ~= 'string' or epoch_type ~= 'string' then return redis.error_reply('owner keys must be strings') end
if processing_type ~= 'none' and processing_type ~= 'list' then return redis.error_reply('processing must be a list') end
if dead_type ~= 'none' and dead_type ~= 'list' then return redis.error_reply('dead letter must be a list') end
if redis.call('GET', KEYS[1]) ~= ARGV[1] or redis.call('GET', KEYS[2]) ~= ARGV[2] then return redis.error_reply('LOCK_LOST') end
if not redis.call('LPOS', KEYS[3], ARGV[3]) then return 0 end
local limit = tonumber(ARGV[4])
if not limit or limit < 1 or limit > 10000 then return redis.error_reply('invalid dead-letter limit') end
redis.call('LPUSH', KEYS[4], ARGV[3])
redis.call('LTRIM', KEYS[4], 0, limit - 1)
redis.call('LREM', KEYS[3], 1, ARGV[3])
return 1
"""

ACK_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local epoch_type = redis.call('TYPE', KEYS[2])['ok']
local processing_type = redis.call('TYPE', KEYS[3])['ok']
local manifest_type = redis.call('TYPE', KEYS[4])['ok']
if lock_type ~= 'string' or epoch_type ~= 'string' then return redis.error_reply('owner keys must be strings') end
if processing_type ~= 'list' then return redis.error_reply('processing must be a list') end
if manifest_type ~= 'hash' then return redis.error_reply('manifest must be a hash') end
local count = tonumber(ARGV[5])
if not count or count < 0 or count > 1000 or #KEYS ~= count + 4 or #ARGV ~= 7 + (count * 2) then return redis.error_reply('invalid payload count') end
for index = 5, #KEYS do
  if redis.call('TYPE', KEYS[index])['ok'] ~= 'hash' then return redis.error_reply('payload must be a hash') end
end
if redis.call('GET', KEYS[1]) ~= ARGV[1] or redis.call('GET', KEYS[2]) ~= ARGV[2] then return redis.error_reply('LOCK_LOST') end
if not redis.call('LPOS', KEYS[3], ARGV[3]) then return 0 end
if redis.call('HGET', KEYS[4], 'image_keys') ~= ARGV[4] or redis.call('HGET', KEYS[4], 'image_count') ~= ARGV[5] then return redis.error_reply('manifest changed') end
if redis.call('HGET', KEYS[4], 'publication_id') ~= ARGV[6] or redis.call('HGET', KEYS[4], 'normalized_url') ~= ARGV[7] then return redis.error_reply('manifest identity mismatch') end
if redis.call('HGET', KEYS[4], 'contract_version') ~= '1' then return redis.error_reply('manifest contract changed') end
for index = 5, #KEYS do
  local payload_index = index - 5
  if redis.call('HGET', KEYS[index], 'publication_id') ~= ARGV[6] or redis.call('HGET', KEYS[index], 'normalized_page_url') ~= ARGV[7] then return redis.error_reply('payload identity mismatch') end
  if redis.call('HGET', KEYS[index], 'contract_version') ~= '1' or redis.call('HGET', KEYS[index], 'normalized_source_url') ~= ARGV[8 + (payload_index * 2)] or redis.call('HGET', KEYS[index], 'alt') ~= ARGV[9 + (payload_index * 2)] then return redis.error_reply('payload changed') end
end
redis.call('LREM', KEYS[3], 1, ARGV[3])
redis.call('DEL', unpack(KEYS, 4, #KEYS))
return 1
"""

WORK_SIZES_SCRIPT = """
local lock_type = redis.call('TYPE', KEYS[1])['ok']
local epoch_type = redis.call('TYPE', KEYS[2])['ok']
local queue_type = redis.call('TYPE', KEYS[3])['ok']
local processing_type = redis.call('TYPE', KEYS[4])['ok']
if lock_type ~= 'string' or epoch_type ~= 'string' then return redis.error_reply('owner keys must be strings') end
if queue_type ~= 'none' and queue_type ~= 'list' then return redis.error_reply('queue must be a list') end
if processing_type ~= 'none' and processing_type ~= 'list' then return redis.error_reply('processing must be a list') end
if redis.call('GET', KEYS[1]) ~= ARGV[1] or redis.call('GET', KEYS[2]) ~= ARGV[2] then return redis.error_reply('LOCK_LOST') end
return {redis.call('LLEN', KEYS[3]), redis.call('LLEN', KEYS[4])}
"""

MANIFEST_PREFLIGHT_SCRIPT = """
if redis.call('TYPE', KEYS[1])['ok'] ~= 'hash' then return redis.error_reply('manifest must be a hash') end
if redis.call('HLEN', KEYS[1]) ~= 5 then return redis.error_reply('manifest field count is invalid') end
if redis.call('HSTRLEN', KEYS[1], 'image_keys') > tonumber(ARGV[1]) then return redis.error_reply('manifest exceeds limit') end
return 1
"""

PAYLOAD_PREFLIGHT_SCRIPT = """
if #KEYS > 1000 then return redis.error_reply('too many payloads') end
for index = 1, #KEYS do
  if redis.call('TYPE', KEYS[index])['ok'] ~= 'hash' then return redis.error_reply('payload must be a hash') end
  if redis.call('HLEN', KEYS[index]) ~= 5 then return redis.error_reply('payload field count is invalid') end
  if redis.call('HSTRLEN', KEYS[index], 'alt') > tonumber(ARGV[1]) then return redis.error_reply('payload alt exceeds limit') end
  if redis.call('HSTRLEN', KEYS[index], 'normalized_page_url') > tonumber(ARGV[2]) then return redis.error_reply('payload page exceeds limit') end
  if redis.call('HSTRLEN', KEYS[index], 'normalized_source_url') > tonumber(ARGV[2]) then return redis.error_reply('payload source exceeds limit') end
end
return 1
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
        username, password = username or "", password or ""
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
            raise LockLostError("image indexer has no fencing epoch")
        return f"{self.owner_token}:{self.owner_epoch}"

    def acquire_lock(self, ttl_seconds=IMAGE_LOCK_TTL_SECONDS) -> bool:
        self._require_client()
        result = self.client.eval(
            ACQUIRE_LOCK_SCRIPT,
            2,
            IMAGE_OWNER_LOCK_KEY,
            IMAGE_FENCE_EPOCH_KEY,
            self.owner_token,
            int(ttl_seconds * 1000),
        )
        if result is None:
            return False
        self.owner_epoch = int(result)
        return True

    def renew_lock(self, ttl_seconds=IMAGE_LOCK_TTL_SECONDS) -> bool:
        self._require_client()
        return int(
            self.client.eval(
                RENEW_LOCK_SCRIPT,
                2,
                IMAGE_OWNER_LOCK_KEY,
                IMAGE_FENCE_EPOCH_KEY,
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
                IMAGE_OWNER_LOCK_KEY,
                IMAGE_FENCE_EPOCH_KEY,
                self.owner_value,
                str(self.owner_epoch),
            )
        ) == 1

    def claim(self) -> Optional[str]:
        self._require_client()
        return self.client.eval(
            CLAIM_SCRIPT,
            4,
            IMAGE_OWNER_LOCK_KEY,
            IMAGE_FENCE_EPOCH_KEY,
            IMAGE_INDEXER_QUEUE_KEY,
            IMAGE_PROCESSING_QUEUE_KEY,
            self.owner_value,
            str(self.owner_epoch),
        )

    def recover(self, limit=IMAGE_RECOVERY_BATCH_SIZE) -> int:
        self._require_client()
        return int(
            self.client.eval(
                RECOVER_SCRIPT,
                4,
                IMAGE_OWNER_LOCK_KEY,
                IMAGE_FENCE_EPOCH_KEY,
                IMAGE_PROCESSING_QUEUE_KEY,
                IMAGE_INDEXER_QUEUE_KEY,
                self.owner_value,
                str(self.owner_epoch),
                limit,
            )
        )

    def release_claim(self, claim: str) -> bool:
        self._require_client()
        return int(
            self.client.eval(
                RELEASE_SCRIPT,
                4,
                IMAGE_OWNER_LOCK_KEY,
                IMAGE_FENCE_EPOCH_KEY,
                IMAGE_PROCESSING_QUEUE_KEY,
                IMAGE_INDEXER_QUEUE_KEY,
                self.owner_value,
                str(self.owner_epoch),
                claim,
            )
        ) == 1

    def quarantine(self, claim: str) -> bool:
        self._require_client()
        return int(
            self.client.eval(
                QUARANTINE_SCRIPT,
                4,
                IMAGE_OWNER_LOCK_KEY,
                IMAGE_FENCE_EPOCH_KEY,
                IMAGE_PROCESSING_QUEUE_KEY,
                IMAGE_DEAD_LETTER_QUEUE_KEY,
                self.owner_value,
                str(self.owner_epoch),
                claim,
                IMAGE_DEAD_LETTER_MAX_ENTRIES,
            )
        ) == 1

    def get_work_sizes(self) -> Tuple[int, int]:
        self._require_client()
        queued, processing = self.client.eval(
            WORK_SIZES_SCRIPT,
            4,
            IMAGE_OWNER_LOCK_KEY,
            IMAGE_FENCE_EPOCH_KEY,
            IMAGE_INDEXER_QUEUE_KEY,
            IMAGE_PROCESSING_QUEUE_KEY,
            self.owner_value,
            str(self.owner_epoch),
        )
        return int(queued), int(processing)

    def load_work(self, manifest_key: str) -> ImageWork:
        self._require_client()
        publication_id, page_url, encoded_page = parse_manifest_key(manifest_key)
        try:
            self.client.eval(
                MANIFEST_PREFLIGHT_SCRIPT,
                1,
                manifest_key,
                MAX_MANIFEST_BYTES,
            )
        except redis.ResponseError as error:
            raise InvalidImageWork("manifest preflight failed") from error
        manifest = self.client.hgetall(manifest_key)
        required = {
            "contract_version",
            "publication_id",
            "normalized_url",
            "image_count",
            "image_keys",
        }
        if set(manifest) != required:
            raise InvalidImageWork("manifest is missing or corrupt")
        if (
            manifest["contract_version"] != "1"
            or manifest["publication_id"] != publication_id
            or manifest["normalized_url"] != page_url
            or len(manifest["image_keys"].encode()) > MAX_MANIFEST_BYTES
        ):
            raise InvalidImageWork("manifest identity is corrupt")
        try:
            count = int(manifest["image_count"])
        except (TypeError, ValueError) as error:
            raise InvalidImageWork("manifest count is invalid") from error
        if count < 0 or count > MAX_IMAGES_PER_PAGE or str(count) != manifest["image_count"]:
            raise InvalidImageWork("manifest count exceeds limit")
        try:
            payload_keys = json.loads(manifest["image_keys"])
        except (TypeError, ValueError) as error:
            raise InvalidImageWork("manifest key list is invalid") from error
        if (
            not isinstance(payload_keys, list)
            or len(payload_keys) != count
            or any(not isinstance(key, str) for key in payload_keys)
            or len(set(payload_keys)) != count
        ):
            raise InvalidImageWork("manifest key list does not match count")
        serialized = json.dumps(payload_keys, separators=(",", ":"))
        if serialized != manifest["image_keys"]:
            raise InvalidImageWork("manifest key list is not canonical")
        source_urls = []
        for payload_key in payload_keys:
            payload_publication, payload_page, source_url = parse_payload_key(payload_key)
            if payload_publication != publication_id or payload_page != page_url:
                raise InvalidImageWork("payload key identity mismatch")
            expected = f"{IMAGE_PREFIX}:{publication_id}:{encoded_page}:" + base64.urlsafe_b64encode(source_url.encode()).decode().rstrip("=")
            if payload_key != expected:
                raise InvalidImageWork("payload key is not canonical")
            source_urls.append(source_url)
        if source_urls != sorted(source_urls):
            raise InvalidImageWork("payload keys are not source sorted")

        payloads = []
        if payload_keys:
            try:
                self.client.eval(
                    PAYLOAD_PREFLIGHT_SCRIPT,
                    len(payload_keys),
                    *payload_keys,
                    MAX_IMAGE_ALT_BYTES,
                    MAX_CANONICAL_URL_BYTES,
                )
            except redis.ResponseError as error:
                raise InvalidImageWork("payload preflight failed") from error
            pipeline = self.client.pipeline(transaction=False)
            for payload_key in payload_keys:
                pipeline.hgetall(payload_key)
            payloads = pipeline.execute()
        images = []
        for payload, source_url in zip(payloads, source_urls):
            try:
                image = Image.from_contract(payload, publication_id, page_url)
                if (
                    image.source_url != source_url
                    or len(image.alt.encode()) > MAX_IMAGE_ALT_BYTES
                ):
                    raise ValueError("payload metadata does not match key")
            except (TypeError, ValueError) as error:
                raise InvalidImageWork("image payload validation failed") from error
            images.append(image)
        return ImageWork(
            manifest_key=manifest_key,
            publication_id=publication_id,
            page_url=page_url,
            payload_keys=payload_keys,
            images=images,
            serialized_keys=serialized,
        )

    def ack(self, work: ImageWork) -> bool:
        self._require_client()
        keys = [
            IMAGE_OWNER_LOCK_KEY,
            IMAGE_FENCE_EPOCH_KEY,
            IMAGE_PROCESSING_QUEUE_KEY,
            work.manifest_key,
            *work.payload_keys,
        ]
        payload_values = []
        for image in work.images:
            payload_values.extend([image.source_url, image.alt])
        return int(
            self.client.eval(
                ACK_SCRIPT,
                len(keys),
                *keys,
                self.owner_value,
                str(self.owner_epoch),
                work.manifest_key,
                work.serialized_keys,
                str(len(work.payload_keys)),
                work.publication_id,
                work.page_url,
                *payload_values,
            )
        ) == 1
