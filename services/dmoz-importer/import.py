import argparse
import logging
import os
import random
import re
import sys
from collections import Counter
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Dict, Iterable, List, Optional
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qsl, quote, urlencode, urlparse, urlunparse
from urllib.request import Request, urlopen

from lxml import etree

try:
    from pymongo import MongoClient, UpdateOne
    from pymongo.errors import BulkWriteError
except ModuleNotFoundError:
    MongoClient = None
    UpdateOne = None
    BulkWriteError = Exception


logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger(__name__)


INCLUDE_PREFIXES = (
    "Top/Arts",
    "Top/Computers",
    "Top/Science",
    "Top/Reference",
    "Top/News",
    "Top/Health",
    "Top/Society",
    "Top/Recreation/Photography",
)

EXCLUDE_PREFIXES = (
    "Top/Adult",
    "Top/World",
    "Top/Shopping",
    "Top/Regional",
    "Top/Games",
)

CATEGORY_MAP = (
    ("Top/Recreation/Photography", "Creative & Design"),
    ("Top/Arts", "Creative & Design"),
    ("Top/Computers", "Technology"),
    ("Top/Science", "Science & Learning"),
    ("Top/Reference", "Reference & Research"),
    ("Top/News", "Journalism & Fact-checking"),
    ("Top/Health", "Science & Learning"),
    ("Top/Society", "General"),
)

TRACKING_KEYS = {
    "fbclid",
    "gclid",
    "dclid",
    "msclkid",
    "ref",
    "source",
}

TRACKING_PREFIXES = ("utm_", "mc_")
UNRESERVED = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
PERCENT_ENCODED = re.compile(r"%([0-9A-Fa-f]{2})")


@dataclass
class ImportStats:
    processed: int = 0
    kept: int = 0
    skipped: int = 0
    invalid_url: int = 0
    missing_title: int = 0
    missing_topic: int = 0
    excluded_category: int = 0
    imported: int = 0
    duplicates: int = 0
    write_errors: int = 0
    by_category: Counter = field(default_factory=Counter)


def local_name(tag: str) -> str:
    return tag.rsplit("}", 1)[-1] if "}" in tag else tag


def attr_by_local_name(element, name: str) -> Optional[str]:
    for key, value in element.attrib.items():
        if local_name(key) == name:
            return value
    return None


def child_text_by_local_name(element, name: str) -> str:
    for child in element:
        if local_name(child.tag) == name:
            return (child.text or "").strip()
    return ""


def decode_unreserved(path: str) -> str:
    def replace(match):
        char = chr(int(match.group(1), 16))
        return char if char in UNRESERVED else match.group(0).upper()

    return PERCENT_ENCODED.sub(replace, path)


def strip_tracking_query(query: str) -> str:
    params = []
    for key, value in parse_qsl(query, keep_blank_values=True):
        lowered = key.lower()
        if lowered in TRACKING_KEYS or lowered.startswith(TRACKING_PREFIXES):
            continue
        params.append((key, value))
    return urlencode(params, doseq=True)


def canonicalize_url(raw_url: str) -> Optional[str]:
    parsed = urlparse(raw_url.strip())
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        return None

    host = parsed.hostname.lower() if parsed.hostname else ""
    if not host:
        return None
    if host.startswith("www."):
        host = host[4:]

    if parsed.port:
        host = f"{host}:{parsed.port}"

    path = decode_unreserved(parsed.path or "/")
    path = quote(path, safe="/%-._~")
    if path != "/":
        path = path.rstrip("/")

    query = strip_tracking_query(parsed.query)
    return urlunparse(("https", host, path, "", query, ""))


def normalized_url(canonical_url: str) -> str:
    parsed = urlparse(canonical_url)
    normalized = parsed.netloc + parsed.path
    if parsed.query:
        normalized += f"?{parsed.query}"
    return normalized.rstrip("/") if normalized.endswith("/") else normalized


def category_allowed(topic: str) -> bool:
    if any(topic.startswith(prefix) for prefix in EXCLUDE_PREFIXES):
        return False
    return any(topic.startswith(prefix) for prefix in INCLUDE_PREFIXES)


def map_category(topic: str) -> str:
    for prefix, category in CATEGORY_MAP:
        if topic.startswith(prefix):
            return category
    return "General"


def parse_dmoz_records(path: str) -> Iterable[Dict[str, str]]:
    context = etree.iterparse(path, events=("end",), recover=True, huge_tree=True)
    for _, element in context:
        if local_name(element.tag) != "ExternalPage":
            continue

        yield {
            "url": attr_by_local_name(element, "about") or "",
            "title": child_text_by_local_name(element, "Title"),
            "description": child_text_by_local_name(element, "Description"),
            "topic": child_text_by_local_name(element, "topic"),
        }

        element.clear(keep_tail=True)
        while element.getprevious() is not None:
            del element.getparent()[0]


def build_seed_document(record: Dict[str, str]) -> Optional[Dict[str, object]]:
    canonical = canonicalize_url(record["url"])
    if not canonical:
        return None

    category = map_category(record["topic"])
    now = datetime.now(timezone.utc)
    return {
        "_id": normalized_url(canonical),
        "url": canonical,
        "title": record["title"],
        "description": record["description"],
        "dmoz_topic": record["topic"],
        "mifolyo_category": category,
        "source": "dmoz_import",
        "status": "pending_crawl",
        "last_crawled": None,
        "indexed_at": None,
        "pagerank": None,
        "tfidf": None,
        "import_timestamp": now,
    }


def should_keep(record: Dict[str, str], stats: ImportStats) -> bool:
    if not record["topic"]:
        stats.missing_topic += 1
        return False
    if not category_allowed(record["topic"]):
        stats.excluded_category += 1
        return False
    if not record["title"]:
        stats.missing_title += 1
        return False
    if not canonicalize_url(record["url"]):
        stats.invalid_url += 1
        return False
    return True


def make_operation(document: Dict[str, object]):
    if UpdateOne is None:
        raise RuntimeError("pymongo is required for non-dry-run imports")

    return UpdateOne(
        {"_id": document["_id"]},
        {"$setOnInsert": document},
        upsert=True,
    )


def flush_operations(collection, operations: List[object], stats: ImportStats) -> None:
    if not operations:
        return

    try:
        result = collection.bulk_write(operations, ordered=False)
        stats.imported += result.upserted_count
        stats.duplicates += result.matched_count
    except BulkWriteError as exc:
        details = exc.details or {}
        stats.imported += details.get("nUpserted", 0)
        stats.duplicates += details.get("nMatched", 0)
        stats.write_errors += len(details.get("writeErrors", []))
        logger.warning("Bulk write completed with %s write errors", stats.write_errors)


def create_indexes(collection) -> None:
    collection.create_index("url", unique=True)
    collection.create_index("dmoz_topic")
    collection.create_index("mifolyo_category")
    collection.create_index("status")


def update_health_sample(sample: List[str], url: str, kept_count: int, sample_size: int) -> None:
    if sample_size <= 0:
        return
    if len(sample) < sample_size:
        sample.append(url)
        return
    index = random.randint(0, kept_count - 1)
    if index < sample_size:
        sample[index] = url


def url_is_reachable(url: str, timeout: int) -> bool:
    headers = {"User-Agent": "MiFolyoBot/1.0"}
    request = Request(url, headers=headers, method="HEAD")
    try:
        with urlopen(request, timeout=timeout) as response:
            return response.status < 400
    except HTTPError as exc:
        if exc.code == 405:
            try:
                fallback = Request(url, headers=headers, method="GET")
                with urlopen(fallback, timeout=timeout) as response:
                    return response.status < 400
            except (HTTPError, URLError, TimeoutError):
                return False
        return False
    except (URLError, TimeoutError):
        return False


def estimate_dead_rate(sample: List[str], timeout: int) -> Optional[float]:
    if not sample:
        return None
    dead = 0
    for index, url in enumerate(sample, start=1):
        if not url_is_reachable(url, timeout):
            dead += 1
        if index % 10 == 0:
            logger.info("Health checked %s/%s sampled URLs", index, len(sample))
    return dead / len(sample)


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


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Import DMOZ RDF URLs into MongoDB seeds.")
    parser.add_argument("input", help="Path to uncompressed content.rdf.u8")
    parser.add_argument("--mongo-uri", default=mongo_uri_from_env(), help="MongoDB connection string")
    parser.add_argument("--mongo-db", default=os.getenv("MONGO_DB", "mifolyo_index"), help="MongoDB database name")
    parser.add_argument("--collection", default="dmoz_seeds", help="Target MongoDB collection")
    parser.add_argument("--batch-size", type=int, default=1000, help="MongoDB bulk write batch size")
    parser.add_argument("--progress-every", type=int, default=10000, help="Log progress every N records")
    parser.add_argument("--limit", type=int, default=0, help="Stop after N processed records, for testing")
    parser.add_argument("--dry-run", action="store_true", help="Parse and report stats without writing to MongoDB")
    parser.add_argument("--health-sample-size", type=int, default=0, help="Sample kept URLs and estimate dead URL rate")
    parser.add_argument("--health-timeout", type=int, default=5, help="Timeout per sampled URL health check")
    parser.add_argument("--random-seed", type=int, default=42, help="Reservoir sampling seed")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    random.seed(args.random_seed)

    if not os.path.exists(args.input):
        logger.error("Input file does not exist: %s", args.input)
        return 1

    collection = None
    if not args.dry_run:
        if MongoClient is None:
            logger.error("pymongo is required for non-dry-run imports")
            return 1
        client = MongoClient(args.mongo_uri)
        client.admin.command("ping")
        collection = client[args.mongo_db][args.collection]
        create_indexes(collection)

    stats = ImportStats()
    operations: List[object] = []
    health_sample: List[str] = []

    for record in parse_dmoz_records(args.input):
        stats.processed += 1

        if should_keep(record, stats):
            document = build_seed_document(record)
            if document is None:
                stats.invalid_url += 1
            else:
                stats.kept += 1
                stats.by_category[document["mifolyo_category"]] += 1
                update_health_sample(health_sample, document["url"], stats.kept, args.health_sample_size)
                if not args.dry_run:
                    operations.append(make_operation(document))

        else:
            stats.skipped += 1

        if not args.dry_run and len(operations) >= args.batch_size:
            flush_operations(collection, operations, stats)
            operations = []

        if args.progress_every and stats.processed % args.progress_every == 0:
            logger.info(
                "Processed=%s kept=%s skipped=%s imported=%s duplicates=%s",
                stats.processed,
                stats.kept,
                stats.skipped,
                stats.imported,
                stats.duplicates,
            )

        if args.limit and stats.processed >= args.limit:
            break

    if not args.dry_run:
        flush_operations(collection, operations, stats)

    dead_rate = estimate_dead_rate(health_sample, args.health_timeout)

    logger.info("DMOZ import complete")
    logger.info("Processed: %s", stats.processed)
    logger.info("Kept: %s", stats.kept)
    logger.info("Skipped: %s", stats.skipped)
    logger.info("Imported: %s", stats.imported)
    logger.info("Duplicates: %s", stats.duplicates)
    logger.info("Write errors: %s", stats.write_errors)
    logger.info("Invalid URL: %s", stats.invalid_url)
    logger.info("Missing title: %s", stats.missing_title)
    logger.info("Missing topic: %s", stats.missing_topic)
    logger.info("Excluded category: %s", stats.excluded_category)
    for category, count in stats.by_category.most_common():
        logger.info("Category: %s = %s", category, count)
    if dead_rate is not None:
        logger.info("Estimated dead URL rate from sample: %.1f%%", dead_rate * 100)

    return 0 if stats.write_errors == 0 else 2


if __name__ == "__main__":
    sys.exit(main())
