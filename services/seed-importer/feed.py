"""Feed enabled crawl seed V1 IDs and URLs to versioned Redis structures."""

from __future__ import annotations

import argparse
import hashlib
import logging
import os
import sys
from dataclasses import dataclass
from typing import Any, Dict, Iterable, Mapping, Optional, Sequence, Tuple

from crawl_seeds import mongo_uri_from_env
from mongo_seeds import COLLECTION_NAME, DATABASE_NAME
from url_identity import URLCanonicalizationError, identify_url

try:
    from pymongo import MongoClient
except ModuleNotFoundError:
    MongoClient = None

try:
    import redis
except ModuleNotFoundError:
    redis = None


logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger(__name__)


QUEUE_KEY = "mifolyo:crawl:v1:queue"
URLS_KEY = "mifolyo:crawl:v1:urls"
DEPTHS_KEY = "mifolyo:crawl:v1:depths"


def log_reference(value: Any) -> str:
    """Return a stable non-reversible reference for operational correlation."""

    return hashlib.sha256(str(value).encode("utf-8", errors="replace")).hexdigest()[:16]

ENQUEUE_SCRIPT = """
local function is_finite_number(value)
  return value and value == value and value ~= math.huge and value ~= -math.huge
end

local function parse_canonical_depth(value)
  if value == '0' then
    return 0
  end
  if not string.match(value, '^[1-9][0-9]*$') then
    return nil
  end
  local parsed = tonumber(value)
  if not parsed or parsed ~= math.floor(parsed) or parsed > 9007199254740991 then
    return nil
  end
  return parsed
end

local queue_type = redis.call('TYPE', KEYS[1]).ok
local urls_type = redis.call('TYPE', KEYS[2]).ok
local depths_type = redis.call('TYPE', KEYS[3]).ok
if queue_type ~= 'none' and queue_type ~= 'zset' then
  return redis.error_reply('INVALID_QUEUE_KEY_TYPE')
end
if urls_type ~= 'none' and urls_type ~= 'hash' then
  return redis.error_reply('INVALID_URLS_KEY_TYPE')
end
if depths_type ~= 'none' and depths_type ~= 'hash' then
  return redis.error_reply('INVALID_DEPTHS_KEY_TYPE')
end

for index = 1, #ARGV, 4 do
  local url_id = ARGV[index]
  local canonical_url = ARGV[index + 1]
  local score = tonumber(ARGV[index + 2])
  local depth = parse_canonical_depth(ARGV[index + 3])
  local existing_url = redis.call('HGET', KEYS[2], url_id)
  local existing_score = redis.call('ZSCORE', KEYS[1], url_id)
  local existing_depth = redis.call('HGET', KEYS[3], url_id)
  if existing_url and existing_url ~= canonical_url then
    return redis.error_reply('URL_ID_COLLISION')
  end
  if not is_finite_number(score) then
    return redis.error_reply('INVALID_QUEUE_SCORE')
  end
  if existing_score and not is_finite_number(tonumber(existing_score)) then
    return redis.error_reply('INVALID_EXISTING_SCORE')
  end
  if not depth then
    return redis.error_reply('INVALID_CRAWL_DEPTH')
  end
  if existing_depth and not parse_canonical_depth(existing_depth) then
    return redis.error_reply('INVALID_EXISTING_DEPTH')
  end
end

for index = 1, #ARGV, 4 do
  local url_id = ARGV[index]
  local canonical_url = ARGV[index + 1]
  local score = tonumber(ARGV[index + 2])
  local depth = parse_canonical_depth(ARGV[index + 3])
  local existing_score = redis.call('ZSCORE', KEYS[1], url_id)
  local existing_depth = redis.call('HGET', KEYS[3], url_id)
  redis.call('HSET', KEYS[2], url_id, canonical_url)
  if not existing_score or score < tonumber(existing_score) then
    redis.call('ZADD', KEYS[1], score, url_id)
  end
  if not existing_score or not existing_depth or depth < tonumber(existing_depth) then
    redis.call('HSET', KEYS[3], url_id, depth)
  end
end
return #ARGV / 4
"""

REMOVE_DISABLED_SCRIPT = """
local queue_type = redis.call('TYPE', KEYS[1]).ok
local urls_type = redis.call('TYPE', KEYS[2]).ok
local depths_type = redis.call('TYPE', KEYS[3]).ok
if queue_type ~= 'none' and queue_type ~= 'zset' then
  return redis.error_reply('INVALID_QUEUE_KEY_TYPE')
end
if urls_type ~= 'none' and urls_type ~= 'hash' then
  return redis.error_reply('INVALID_URLS_KEY_TYPE')
end
if depths_type ~= 'none' and depths_type ~= 'hash' then
  return redis.error_reply('INVALID_DEPTHS_KEY_TYPE')
end

for index = 1, #ARGV, 2 do
  local url_id = ARGV[index]
  local canonical_url = ARGV[index + 1]
  local existing_url = redis.call('HGET', KEYS[2], url_id)
  if existing_url and existing_url ~= canonical_url then
    return redis.error_reply('URL_ID_COLLISION')
  end
end

for index = 1, #ARGV, 2 do
  local url_id = ARGV[index]
  redis.call('ZREM', KEYS[1], url_id)
  redis.call('HDEL', KEYS[2], url_id)
  redis.call('HDEL', KEYS[3], url_id)
end
return #ARGV / 2
"""


@dataclass(frozen=True)
class CrawlSeed:
    id: str
    canonical_url: str
    priority: int

    @property
    def url(self) -> str:
        return self.canonical_url


@dataclass
class FeedStats:
    seen: int = 0
    skipped_invalid: int = 0
    prepared: int = 0
    enqueued: int = 0
    disabled_seen: int = 0
    disabled_reconciled: int = 0


def redis_score(priority: int) -> float:
    if (
        isinstance(priority, bool)
        or not isinstance(priority, int)
        or priority not in {1, 2, 3}
    ):
        raise ValueError("priority must be 1, 2, or 3")
    return float(priority - 1)


def seed_from_document(document: Mapping[str, Any]) -> CrawlSeed:
    url_id = document.get("_id")
    canonical_url = document.get("canonical_url")
    priority = document.get("priority")
    if not isinstance(url_id, str) or not isinstance(canonical_url, str):
        raise ValueError("seed is missing its V1 ID or canonical URL")
    if isinstance(priority, bool) or not isinstance(priority, int):
        raise ValueError("seed priority is not an integer")

    try:
        identity = identify_url(canonical_url)
    except URLCanonicalizationError as exc:
        raise ValueError(f"invalid stored canonical URL: {exc.code}") from exc
    if identity.canonical_url != canonical_url or identity.url_id != url_id:
        raise ValueError("stored canonical URL and V1 ID do not match")
    if not identity.crawl_eligible:
        raise ValueError(f"stored URL is not crawl eligible: {identity.crawl_rejection}")
    redis_score(priority)
    return CrawlSeed(id=url_id, canonical_url=canonical_url, priority=priority)


def iter_mongo_seeds(
    collection: Any,
    limit: int,
    stats: Optional[FeedStats] = None,
) -> Iterable[CrawlSeed]:
    """Read only enabled catalog records; this function never updates MongoDB."""

    cursor = collection.find({"enabled": True}).sort(
        [("priority", 1), ("discovered_at", 1), ("_id", 1)]
    )
    if limit:
        cursor = cursor.limit(limit)

    for document in cursor:
        if stats is not None:
            stats.seen += 1
        try:
            yield seed_from_document(document)
        except ValueError:
            if stats is not None:
                stats.skipped_invalid += 1
            logger.warning(
                "Skipping invalid crawl seed ref=%s",
                log_reference(document.get("_id")),
            )


def iter_mongo_disabled_seeds(
    collection: Any,
    stats: Optional[FeedStats] = None,
) -> Iterable[CrawlSeed]:
    cursor = collection.find({"enabled": False}).sort([("_id", 1)])
    for document in cursor:
        if stats is not None:
            stats.disabled_seen += 1
        try:
            yield seed_from_document(document)
        except ValueError:
            if stats is not None:
                stats.skipped_invalid += 1
            logger.warning(
                "Skipping invalid disabled crawl seed ref=%s",
                log_reference(document.get("_id")),
            )


def redis_client_from_env() -> Any:
    if redis is None:
        raise RuntimeError("redis is required unless --dry-run is used")
    return redis.Redis(
        host=os.getenv("REDIS_HOST", "localhost"),
        port=int(os.getenv("REDIS_PORT", "6379")),
        password=os.getenv("REDIS_PASSWORD") or None,
        db=int(os.getenv("REDIS_DB", "0")),
        decode_responses=True,
    )


def enqueue_batch(
    redis_client: Any,
    members: Mapping[str, Tuple[str, float]],
    dry_run: bool = False,
) -> int:
    """Atomically map IDs to URLs and retain each ID's lowest queue score."""

    if not members:
        return 0
    if dry_run:
        for url_id, (canonical_url, score) in sorted(members.items()):
            logger.info(
                "Would enqueue crawl seed ref=%s score=%s",
                log_reference(canonical_url),
                score,
            )
        return len(members)

    arguments = []
    for url_id, (canonical_url, score) in sorted(members.items()):
        arguments.extend((url_id, canonical_url, score, 0))
    result = redis_client.eval(
        ENQUEUE_SCRIPT,
        3,
        QUEUE_KEY,
        URLS_KEY,
        DEPTHS_KEY,
        *arguments,
    )
    if result != len(members):
        raise RuntimeError(
            f"Redis accepted {result} of {len(members)} crawl seed entries"
        )
    return result


def reconcile_disabled_batch(
    redis_client: Any,
    members: Mapping[str, str],
    dry_run: bool = False,
) -> int:
    if not members:
        return 0
    if dry_run:
        for url_id, canonical_url in sorted(members.items()):
            logger.info(
                "Would remove disabled crawl seed ref=%s",
                log_reference(canonical_url),
            )
        return len(members)

    arguments = []
    for url_id, canonical_url in sorted(members.items()):
        arguments.extend((url_id, canonical_url))
    result = redis_client.eval(
        REMOVE_DISABLED_SCRIPT,
        3,
        QUEUE_KEY,
        URLS_KEY,
        DEPTHS_KEY,
        *arguments,
    )
    if result != len(members):
        raise RuntimeError(
            f"Redis reconciled {result} of {len(members)} disabled crawl seed entries"
        )
    return result


def reconcile_disabled_seeds(
    seeds: Iterable[CrawlSeed],
    redis_client: Any,
    *,
    batch_size: int,
    dry_run: bool,
    stats: FeedStats,
) -> None:
    pending: Dict[str, str] = {}
    for seed in seeds:
        existing = pending.get(seed.id)
        if existing is not None and existing != seed.canonical_url:
            raise ValueError(f"conflicting canonical URLs for URL ID {seed.id}")
        pending[seed.id] = seed.canonical_url
        if len(pending) >= batch_size:
            stats.disabled_reconciled += reconcile_disabled_batch(
                redis_client, pending, dry_run
            )
            pending = {}
    stats.disabled_reconciled += reconcile_disabled_batch(
        redis_client, pending, dry_run
    )


def feed_seeds(
    seeds: Iterable[CrawlSeed],
    redis_client: Any,
    *,
    batch_size: int,
    dry_run: bool,
    stats: FeedStats,
) -> None:
    if batch_size < 1:
        raise ValueError("batch_size must be at least 1")

    pending: Dict[str, Tuple[str, float]] = {}
    for seed in seeds:
        score = redis_score(seed.priority)
        existing = pending.get(seed.id)
        if existing is not None and existing[0] != seed.canonical_url:
            raise ValueError(f"conflicting canonical URLs for URL ID {seed.id}")
        if existing is None or score < existing[1]:
            pending[seed.id] = (seed.canonical_url, score)
        stats.prepared += 1

        if len(pending) >= batch_size:
            stats.enqueued += enqueue_batch(redis_client, pending, dry_run)
            pending = {}

    stats.enqueued += enqueue_batch(redis_client, pending, dry_run)


def parse_args(argv: Optional[Sequence[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Feed enabled mifolyo_index.crawl_seeds into the V1 Redis queue, URL map, and depth map."
        )
    )
    parser.add_argument(
        "--mongo-uri", default=mongo_uri_from_env(), help="MongoDB connection string"
    )
    parser.add_argument("--limit", type=int, default=1000)
    parser.add_argument("--batch-size", type=int, default=500)
    parser.add_argument(
        "--dry-run", action="store_true", help="read MongoDB but do not write Redis"
    )
    return parser.parse_args(argv)


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = parse_args(argv)
    if args.limit < 0 or args.batch_size < 1:
        logger.error("--limit must be non-negative and --batch-size must be positive")
        return 1
    if MongoClient is None:
        logger.error("pymongo is required for crawl seed feeding")
        return 1

    mongo_client = None
    redis_client = None
    stats = FeedStats()
    try:
        mongo_client = MongoClient(args.mongo_uri)
        mongo_client.admin.command("ping")
        collection = mongo_client[DATABASE_NAME][COLLECTION_NAME]
        seeds = iter_mongo_seeds(collection, args.limit, stats)
        disabled_seeds = iter_mongo_disabled_seeds(collection, stats)

        if not args.dry_run:
            redis_client = redis_client_from_env()
            redis_client.ping()
        reconcile_disabled_seeds(
            disabled_seeds,
            redis_client,
            batch_size=args.batch_size,
            dry_run=args.dry_run,
            stats=stats,
        )
        feed_seeds(
            seeds,
            redis_client,
            batch_size=args.batch_size,
            dry_run=args.dry_run,
            stats=stats,
        )
    except Exception:
        logger.error("Crawl seed feed failed")
        return 2
    finally:
        if mongo_client is not None:
            mongo_client.close()
        if redis_client is not None and hasattr(redis_client, "close"):
            redis_client.close()

    logger.info("Crawl seed V1 feed complete")
    logger.info("Enabled records seen: %s", stats.seen)
    logger.info("Disabled records seen: %s", stats.disabled_seen)
    logger.info("Invalid records skipped: %s", stats.skipped_invalid)
    logger.info("Records prepared: %s", stats.prepared)
    logger.info("IDs submitted: %s", stats.enqueued)
    logger.info("Disabled IDs reconciled: %s", stats.disabled_reconciled)
    return 0


if __name__ == "__main__":
    sys.exit(main())
