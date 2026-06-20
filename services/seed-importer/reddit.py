import argparse
import csv
import json
import logging
import os
import re
import sys
import time
from collections import Counter
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Dict, Iterable, List, Optional
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qsl, quote, unquote, urlencode, urlparse, urlunparse
from urllib.request import Request, urlopen

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


REDDIT_HOSTS = {"reddit.com", "www.reddit.com", "old.reddit.com", "redd.it", "out.reddit.com"}
TRACKING_KEYS = {"fbclid", "gclid", "dclid", "msclkid", "ref", "source"}
TRACKING_PREFIXES = ("utm_", "mc_")
URL_PATTERN = re.compile(r"https?://[^\s<>()\[\]{}\"']+")
UNRESERVED = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
PERCENT_ENCODED = re.compile(r"%([0-9A-Fa-f]{2})")


@dataclass
class RedditSeed:
    url: str
    category: str
    priority: int
    source_url: str
    source_json_url: str
    title: str = ""
    score: int = 0
    subreddit: str = ""
    reddit_permalink: str = ""


@dataclass
class ImportStats:
    reddit_pages_seen: int = 0
    reddit_pages_fetched: int = 0
    posts_seen: int = 0
    comment_urls_seen: int = 0
    outbound_urls_found: int = 0
    skipped_reddit_urls: int = 0
    skipped_low_score: int = 0
    duplicate_urls: int = 0
    imported: int = 0
    write_errors: int = 0
    by_category: Counter = field(default_factory=Counter)


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


def unwrap_reddit_out_url(raw_url: str) -> str:
    parsed = urlparse(raw_url)
    if parsed.netloc.lower() != "out.reddit.com":
        return raw_url

    params = dict(parse_qsl(parsed.query, keep_blank_values=True))
    return unquote(params.get("url", raw_url))


def canonicalize_url(raw_url: str) -> Optional[str]:
    raw_url = unwrap_reddit_out_url(raw_url.strip().rstrip(".,);]"))
    parsed = urlparse(raw_url)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        return None

    host = parsed.hostname.lower() if parsed.hostname else ""
    if not host:
        return None
    if host.startswith("www."):
        host = host[4:]

    if host in REDDIT_HOSTS:
        return None

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
    value = parsed.netloc + parsed.path
    if parsed.query:
        value += f"?{parsed.query}"
    return value.rstrip("/") if value.endswith("/") else value


def old_reddit_url(raw_url: str) -> Optional[str]:
    parsed = urlparse(raw_url.strip())
    if not parsed.netloc:
        return None

    host = parsed.netloc.lower()
    if host not in {"reddit.com", "www.reddit.com", "old.reddit.com"}:
        return None

    return urlunparse(("https", "old.reddit.com", parsed.path or "/", "", parsed.query, ""))


def reddit_json_url(raw_url: str) -> Optional[str]:
    old_url = old_reddit_url(raw_url)
    if not old_url:
        return None

    parsed = urlparse(old_url)
    path = parsed.path or "/"
    if not path.endswith(".json"):
        path = path.rstrip("/") + "/.json"
    return urlunparse((parsed.scheme, parsed.netloc, path, "", parsed.query, ""))


def full_reddit_permalink(permalink: str) -> Optional[str]:
    if not permalink:
        return None
    if permalink.startswith("http://") or permalink.startswith("https://"):
        return old_reddit_url(permalink)
    return f"https://old.reddit.com{permalink}"


def read_reddit_seed_rows(path: str) -> List[Dict[str, str]]:
    with open(path, newline="") as handle:
        rows = list(csv.DictReader(handle))
    return [row for row in rows if "reddit" in row.get("source", "").lower()]


def fetch_json(url: str, user_agent: str, timeout: int) -> object:
    request = Request(url, headers={"User-Agent": user_agent})
    with urlopen(request, timeout=timeout) as response:
        return json.loads(response.read().decode("utf-8"))


def load_json_file(path: str) -> object:
    with open(path) as handle:
        return json.load(handle)


def iter_listing_posts(payload: object) -> Iterable[Dict[str, object]]:
    if isinstance(payload, list):
        for item in payload:
            yield from iter_listing_posts(item)
        return

    if not isinstance(payload, dict):
        return

    data = payload.get("data")
    if not isinstance(data, dict):
        return

    children = data.get("children")
    if not isinstance(children, list):
        return

    for child in children:
        if not isinstance(child, dict):
            continue
        if child.get("kind") == "t3" and isinstance(child.get("data"), dict):
            yield child["data"]


def iter_comment_urls(payload: object) -> Iterable[str]:
    if isinstance(payload, list):
        for item in payload:
            yield from iter_comment_urls(item)
        return

    if not isinstance(payload, dict):
        return

    data = payload.get("data")
    if isinstance(data, dict):
        body = data.get("body") or data.get("body_html") or ""
        if isinstance(body, str):
            for match in URL_PATTERN.findall(body):
                yield match

        replies = data.get("replies")
        if replies:
            yield from iter_comment_urls(replies)

        children = data.get("children")
        if isinstance(children, list):
            for child in children:
                yield from iter_comment_urls(child)


def seed_from_url(
    url: str,
    row: Dict[str, str],
    source_url: str,
    source_json_url: str,
    title: str = "",
    score: int = 0,
    subreddit: str = "",
    reddit_permalink: str = "",
) -> Optional[RedditSeed]:
    canonical = canonicalize_url(url)
    if not canonical:
        return None

    return RedditSeed(
        url=canonical,
        category=row.get("category", "General") or "General",
        priority=int(row.get("priority", 3) or 3),
        source_url=source_url,
        source_json_url=source_json_url,
        title=title,
        score=score,
        subreddit=subreddit,
        reddit_permalink=reddit_permalink,
    )


def discover_from_payload(
    payload: object,
    row: Dict[str, str],
    source_url: str,
    source_json_url: str,
    min_score: int,
    include_comment_urls: bool,
    stats: ImportStats,
) -> List[RedditSeed]:
    discovered: List[RedditSeed] = []

    for post in iter_listing_posts(payload):
        stats.posts_seen += 1
        score = int(post.get("score") or 0)
        if score < min_score:
            stats.skipped_low_score += 1
            continue
        if post.get("is_self"):
            continue

        raw_url = post.get("url_overridden_by_dest") or post.get("url") or ""
        seed = seed_from_url(
            str(raw_url),
            row,
            source_url,
            source_json_url,
            title=str(post.get("title") or ""),
            score=score,
            subreddit=str(post.get("subreddit") or ""),
            reddit_permalink=str(post.get("permalink") or ""),
        )
        if seed:
            discovered.append(seed)
        elif raw_url:
            stats.skipped_reddit_urls += 1

    if include_comment_urls:
        for raw_url in iter_comment_urls(payload):
            stats.comment_urls_seen += 1
            seed = seed_from_url(raw_url, row, source_url, source_json_url)
            if seed:
                discovered.append(seed)
            else:
                stats.skipped_reddit_urls += 1

    return discovered


def seed_document(seed: RedditSeed) -> Dict[str, object]:
    now = datetime.now(timezone.utc)
    return {
        "_id": normalized_url(seed.url),
        "url": seed.url,
        "source": "reddit_json",
        "source_detail": seed.source_json_url,
        "category": seed.category,
        "priority": seed.priority,
        "score": seed.score,
        "status": "pending_crawl",
        "discovered_at": now,
        "last_crawled": None,
        "reddit": {
            "source_url": seed.source_url,
            "json_url": seed.source_json_url,
            "title": seed.title,
            "score": seed.score,
            "subreddit": seed.subreddit,
            "permalink": seed.reddit_permalink,
        },
    }


def make_operation(seed: RedditSeed):
    if UpdateOne is None:
        raise RuntimeError("pymongo is required for non-dry-run imports")
    document = seed_document(seed)
    return UpdateOne({"_id": document["_id"]}, {"$setOnInsert": document}, upsert=True)


def create_indexes(collection) -> None:
    collection.create_index("url", unique=True)
    collection.create_index("source")
    collection.create_index("category")
    collection.create_index("priority")
    collection.create_index("status")


def flush_operations(collection, operations: List[object], stats: ImportStats) -> None:
    if not operations:
        return
    try:
        result = collection.bulk_write(operations, ordered=False)
        stats.imported += result.upserted_count
        stats.duplicate_urls += result.matched_count
    except BulkWriteError as exc:
        details = exc.details or {}
        stats.imported += details.get("nUpserted", 0)
        stats.duplicate_urls += details.get("nMatched", 0)
        stats.write_errors += len(details.get("writeErrors", []))
        logger.warning("Bulk write completed with %s write errors", stats.write_errors)


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
    parser = argparse.ArgumentParser(description="Discover outbound crawl seeds from old.reddit.com JSON pages.")
    parser.add_argument("--seeds-csv", default="/seeds/manual-seeds.csv", help="CSV containing reddit discovery seed rows")
    parser.add_argument("--url", action="append", default=[], help="Additional reddit URL to discover from")
    parser.add_argument("--input-json", help="Parse a local Reddit JSON file instead of fetching remote URLs")
    parser.add_argument("--mongo-uri", default=mongo_uri_from_env(), help="MongoDB connection string")
    parser.add_argument("--mongo-db", default=os.getenv("MONGO_DB", "mifolyo_index"), help="MongoDB database name")
    parser.add_argument("--collection", default="crawl_seeds", help="Target MongoDB collection")
    parser.add_argument("--batch-size", type=int, default=1000, help="MongoDB bulk write batch size")
    parser.add_argument("--min-score", type=int, default=25, help="Minimum Reddit post score to accept")
    parser.add_argument("--delay", type=float, default=2.0, help="Delay between Reddit JSON requests")
    parser.add_argument("--timeout", type=int, default=20, help="HTTP timeout per Reddit JSON request")
    parser.add_argument("--user-agent", default=os.getenv("USER_AGENT", "MiFolyoBot/1.0"), help="HTTP User-Agent")
    parser.add_argument("--include-comment-urls", action="store_true", help="Also extract URLs from comment bodies in fetched JSON")
    parser.add_argument("--crawl-post-pages", action="store_true", help="Fetch each discovered Reddit post permalink as .json and inspect it")
    parser.add_argument("--max-post-pages", type=int, default=25, help="Maximum post JSON pages to fetch per listing seed")
    parser.add_argument("--dry-run", action="store_true", help="Discover and print stats without writing to MongoDB")
    return parser.parse_args()


def rows_from_args(args: argparse.Namespace) -> List[Dict[str, str]]:
    rows: List[Dict[str, str]] = []
    if args.input_json:
        rows.append({"url": "https://old.reddit.com/r/sample", "category": "General", "priority": "3", "source": "reddit_fixture"})
        return rows

    if os.path.exists(args.seeds_csv):
        rows.extend(read_reddit_seed_rows(args.seeds_csv))
    elif not args.url:
        logger.warning("Seed CSV not found: %s", args.seeds_csv)

    for url in args.url:
        rows.append({"url": url, "category": "General", "priority": "3", "source": "manual_reddit_discovery"})
    return rows


def main() -> int:
    args = parse_args()
    rows = rows_from_args(args)
    if not rows:
        logger.error("No Reddit seed rows found")
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
    seen_urls = set()
    operations: List[object] = []

    for row in rows:
        source_url = old_reddit_url(row["url"])
        source_json_url = reddit_json_url(row["url"])
        if not source_url or not source_json_url:
            logger.warning("Skipping non-Reddit seed URL: %s", row["url"])
            continue

        stats.reddit_pages_seen += 1
        try:
            if args.input_json:
                payload = load_json_file(args.input_json)
            else:
                payload = fetch_json(source_json_url, args.user_agent, args.timeout)
                stats.reddit_pages_fetched += 1
        except (HTTPError, URLError, TimeoutError, json.JSONDecodeError) as exc:
            logger.warning("Failed to fetch %s: %s", source_json_url, exc)
            continue

        discovered = discover_from_payload(
            payload,
            row,
            source_url,
            source_json_url,
            args.min_score,
            args.include_comment_urls,
            stats,
        )

        if args.crawl_post_pages and not args.input_json:
            post_pages_fetched = 0
            for post in iter_listing_posts(payload):
                if post_pages_fetched >= args.max_post_pages:
                    break
                post_url = full_reddit_permalink(str(post.get("permalink") or ""))
                post_json_url = reddit_json_url(post_url) if post_url else None
                if not post_url or not post_json_url:
                    continue
                time.sleep(args.delay)
                try:
                    post_payload = fetch_json(post_json_url, args.user_agent, args.timeout)
                    stats.reddit_pages_fetched += 1
                    post_pages_fetched += 1
                except (HTTPError, URLError, TimeoutError, json.JSONDecodeError) as exc:
                    logger.warning("Failed to fetch %s: %s", post_json_url, exc)
                    continue
                discovered.extend(
                    discover_from_payload(
                        post_payload,
                        row,
                        post_url,
                        post_json_url,
                        args.min_score,
                        args.include_comment_urls,
                        stats,
                    )
                )

        for seed in discovered:
            key = normalized_url(seed.url)
            if key in seen_urls:
                stats.duplicate_urls += 1
                continue
            seen_urls.add(key)
            stats.outbound_urls_found += 1
            stats.by_category[seed.category] += 1
            if args.dry_run:
                logger.info("Discovered %s from %s", seed.url, seed.source_json_url)
            else:
                operations.append(make_operation(seed))

            if not args.dry_run and len(operations) >= args.batch_size:
                flush_operations(collection, operations, stats)
                operations = []

        if not args.input_json:
            time.sleep(args.delay)

    if not args.dry_run:
        flush_operations(collection, operations, stats)

    logger.info("Reddit JSON discovery complete")
    logger.info("Reddit pages seen: %s", stats.reddit_pages_seen)
    logger.info("Reddit pages fetched: %s", stats.reddit_pages_fetched)
    logger.info("Posts seen: %s", stats.posts_seen)
    logger.info("Comment URLs seen: %s", stats.comment_urls_seen)
    logger.info("Outbound URLs found: %s", stats.outbound_urls_found)
    logger.info("Skipped Reddit/self/invalid URLs: %s", stats.skipped_reddit_urls)
    logger.info("Skipped low-score posts: %s", stats.skipped_low_score)
    logger.info("Duplicates: %s", stats.duplicate_urls)
    logger.info("Imported: %s", stats.imported)
    logger.info("Write errors: %s", stats.write_errors)
    for category, count in stats.by_category.most_common():
        logger.info("Category: %s = %s", category, count)

    return 0 if stats.write_errors == 0 else 2


if __name__ == "__main__":
    sys.exit(main())
