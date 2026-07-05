import argparse
import csv
import logging
import os
import sys
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Dict, Iterable, List, Optional
from urllib.parse import urlparse

try:
    from pymongo import MongoClient, UpdateOne
except ModuleNotFoundError:
    MongoClient = None
    UpdateOne = None

try:
    import redis
except ModuleNotFoundError:
    redis = None


logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger(__name__)


SPIDER_QUEUE_KEY = "spider_queue"
REDDIT_HOSTS = {"reddit.com", "www.reddit.com", "old.reddit.com", "redd.it", "out.reddit.com"}


@dataclass
class CrawlSeed:
    id: str
    url: str
    priority: int = 1
    source: str = "unknown"
    category: str = "General"


@dataclass
class FeedStats:
    seen: int = 0
    normalized: int = 0
    skipped_invalid: int = 0
    skipped_reddit: int = 0
    enqueued: int = 0
    mongo_updated: int = 0


def normalize_for_spider(raw_url: str) -> Optional[str]:
    parsed = urlparse(raw_url.strip())
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        return None

    host = parsed.netloc.lower()
    if host.startswith("www."):
        host = host[4:]

    path = parsed.path.rstrip("/")
    return f"{host}{path}"


def is_reddit_url(raw_url: str) -> bool:
    parsed = urlparse(raw_url.strip())
    return (parsed.hostname or "").lower() in REDDIT_HOSTS


def redis_score(priority: int) -> float:
    return max(priority - 1, 0)


def mongo_uri_from_env() -> str:
    if os.getenv("MONGO_URI"):
        return os.environ["MONGO_URI"]

    host = os.getenv("MONGO_HOST", "localhost")
    port = os.getenv("MONGO_PORT", "27017")
    username = os.getenv("MONGO_USERNAME", "")
    password = os.getenv("MONGO_PASSWORD", "")
    db = os.getenv("MONGO_DB", "mifolyo_index")

    if username:
        return f"mongodb://{username}:{password}@{host}:{port}/{db}?authSource=admin"
    return f"mongodb://{host}:{port}/{db}"


def redis_client_from_env():
    if redis is None:
        raise RuntimeError("redis is required unless --dry-run is used")

    return redis.Redis(
        host=os.getenv("REDIS_HOST", "localhost"),
        port=int(os.getenv("REDIS_PORT", "6379")),
        password=os.getenv("REDIS_PASSWORD") or None,
        db=int(os.getenv("REDIS_DB", "0")),
        decode_responses=True,
    )


def iter_mongo_seeds(collection, limit: int, status: str) -> Iterable[CrawlSeed]:
    query = {"status": status}
    cursor = collection.find(query).sort([("priority", 1), ("discovered_at", 1), ("_id", 1)])
    if limit:
        cursor = cursor.limit(limit)

    for document in cursor:
        yield CrawlSeed(
            id=str(document.get("_id")),
            url=str(document.get("url") or ""),
            priority=int(document.get("priority") or 1),
            source=str(document.get("source") or "mongo"),
            category=str(document.get("category") or document.get("mifolyo_category") or "General"),
        )


def iter_csv_seeds(path: str, limit: int) -> Iterable[CrawlSeed]:
    count = 0
    with open(path, newline="") as handle:
        for row in csv.DictReader(handle):
            yield CrawlSeed(
                id=row.get("url", ""),
                url=row.get("url", ""),
                priority=int(row.get("priority") or 1),
                source=row.get("source", "csv"),
                category=row.get("category", "General"),
            )
            count += 1
            if limit and count >= limit:
                break


def enqueue_batch(redis_client, queue_key: str, members: Dict[str, float], dry_run: bool) -> int:
    if not members:
        return 0
    if dry_run:
        for member, score in members.items():
            logger.info("Would enqueue %s score=%s", member, score)
        return len(members)
    return int(redis_client.zadd(queue_key, members))


def update_mongo_status(collection, operations: List[object], dry_run: bool) -> int:
    if dry_run or not operations:
        return 0
    result = collection.bulk_write(operations, ordered=False)
    return result.modified_count


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Feed pending crawl seeds into Redis spider_queue.")
    parser.add_argument("--source", choices=["mongo", "csv"], default="mongo", help="Seed source to feed")
    parser.add_argument("--csv", default="/seeds/manual-seeds.csv", help="CSV path when --source=csv")
    parser.add_argument("--mongo-uri", default=mongo_uri_from_env(), help="MongoDB connection string")
    parser.add_argument("--mongo-db", default=os.getenv("MONGO_DB", "mifolyo_index"), help="MongoDB database name")
    parser.add_argument("--collection", default="crawl_seeds", help="MongoDB crawl seed collection")
    parser.add_argument("--status", default="pending_crawl", help="MongoDB seed status to feed")
    parser.add_argument("--queue-key", default=SPIDER_QUEUE_KEY, help="Redis sorted set key consumed by spider")
    parser.add_argument("--limit", type=int, default=1000, help="Maximum seeds to enqueue")
    parser.add_argument("--batch-size", type=int, default=500, help="Redis/Mongo batch size")
    parser.add_argument("--include-reddit", action="store_true", help="Allow Reddit URLs when feeding CSV seeds")
    parser.add_argument("--dry-run", action="store_true", help="Print intended queue writes without Redis or Mongo updates")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    stats = FeedStats()

    mongo_collection = None
    if args.source == "mongo":
        if MongoClient is None:
            logger.error("pymongo is required for Mongo seed feeding")
            return 1
        client = MongoClient(args.mongo_uri)
        client.admin.command("ping")
        mongo_collection = client[args.mongo_db][args.collection]
        seeds = iter_mongo_seeds(mongo_collection, args.limit, args.status)
    else:
        if not os.path.exists(args.csv):
            logger.error("CSV file does not exist: %s", args.csv)
            return 1
        seeds = iter_csv_seeds(args.csv, args.limit)

    redis_client = None if args.dry_run else redis_client_from_env()
    if redis_client is not None:
        redis_client.ping()

    queue_members: Dict[str, float] = {}
    mongo_updates: List[object] = []
    queued_at = datetime.now(timezone.utc)

    for seed in seeds:
        stats.seen += 1
        if is_reddit_url(seed.url) and not args.include_reddit:
            stats.skipped_reddit += 1
            continue

        normalized = normalize_for_spider(seed.url)
        if not normalized:
            stats.skipped_invalid += 1
            continue

        stats.normalized += 1
        score = redis_score(seed.priority)
        queue_members[normalized] = score

        if args.source == "mongo" and UpdateOne is not None:
            mongo_updates.append(
                UpdateOne(
                    {"_id": seed.id},
                    {
                        "$set": {
                            "status": "queued",
                            "queued_at": queued_at,
                            "spider_queue_key": args.queue_key,
                            "spider_queue_score": score,
                            "normalized_url": normalized,
                        }
                    },
                )
            )

        if len(queue_members) >= args.batch_size:
            stats.enqueued += enqueue_batch(redis_client, args.queue_key, queue_members, args.dry_run)
            stats.mongo_updated += update_mongo_status(mongo_collection, mongo_updates, args.dry_run)
            queue_members = {}
            mongo_updates = []

    stats.enqueued += enqueue_batch(redis_client, args.queue_key, queue_members, args.dry_run)
    stats.mongo_updated += update_mongo_status(mongo_collection, mongo_updates, args.dry_run)

    logger.info("Crawl seed feed complete")
    logger.info("Seen: %s", stats.seen)
    logger.info("Normalized: %s", stats.normalized)
    logger.info("Skipped invalid: %s", stats.skipped_invalid)
    logger.info("Skipped Reddit: %s", stats.skipped_reddit)
    logger.info("Enqueued: %s", stats.enqueued)
    logger.info("Mongo updated: %s", stats.mongo_updated)
    return 0


if __name__ == "__main__":
    sys.exit(main())
