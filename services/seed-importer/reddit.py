"""Discover outbound URLs from Reddit JSON and merge V1 seed provenance."""

from __future__ import annotations

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
from typing import Any, Dict, Iterable, List, Mapping, Optional, Sequence
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qsl, urlsplit, urlunsplit
from urllib.request import Request, urlopen

from crawl_seed import (
    make_source,
    merge_seed_documents,
    new_seed_document,
    stable_source_key,
)
from crawl_seeds import mongo_uri_from_env
from mongo_seeds import (
    DATABASE_NAME,
    ensure_crawl_seeds_collection,
    make_merge_operation,
    write_seed_documents,
)
from url_identity import MAX_URL_BYTES, URLCanonicalizationError, identify_url

try:
    from pymongo import MongoClient
    from pymongo.errors import BulkWriteError
except ModuleNotFoundError:
    MongoClient = None
    BulkWriteError = Exception


logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger(__name__)


REDDIT_HOSTS = {
    "reddit.com",
    "www.reddit.com",
    "old.reddit.com",
    "redd.it",
    "out.reddit.com",
}
URL_PATTERN = re.compile(r"https?://[^\s<>()\[\]{}\"']+")
REDDIT_SOURCE_TYPE = "reddit_json"


@dataclass
class RedditSeed:
    # ``url`` is the exact V1 canonical URL. ``raw_url`` retains the source
    # observation and may be an out.reddit.com wrapper.
    url: str
    category: str
    priority: int
    source_url: str
    source_json_url: str
    title: str = ""
    score: int = 0
    subreddit: str = ""
    reddit_permalink: str = ""
    raw_url: str = ""
    url_id: str = ""


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


def _clean_extracted_url(raw_url: str) -> str:
    # Reddit body extraction can include sentence punctuation.  This adapter
    # cleanup is deliberately outside the shared canonicalizer.
    return raw_url.strip().rstrip(".,);]")


def unwrap_reddit_out_url(raw_url: str) -> str:
    """Unwrap Reddit's outbound redirect; this is not identity behavior."""

    try:
        parsed = urlsplit(raw_url)
    except ValueError:
        return raw_url
    if (parsed.hostname or "").lower() != "out.reddit.com":
        return raw_url

    for key, value in parse_qsl(parsed.query, keep_blank_values=True):
        if key == "url" and value:
            return value
    return raw_url


def prepare_reddit_outbound_url(raw_url: str) -> Optional[tuple[str, str]]:
    """Return ``(raw observation, identity input)`` or exclude Reddit URLs."""

    observed = _clean_extracted_url(raw_url)
    candidate = unwrap_reddit_out_url(observed)
    try:
        parsed = urlsplit(candidate)
        host = (parsed.hostname or "").lower()
    except ValueError:
        return None
    if host in REDDIT_HOSTS:
        return None
    return observed, candidate


def old_reddit_url(raw_url: str) -> Optional[str]:
    try:
        parsed = urlsplit(raw_url.strip())
    except ValueError:
        return None
    if (parsed.hostname or "").lower() not in {
        "reddit.com",
        "www.reddit.com",
        "old.reddit.com",
    }:
        return None
    return urlunsplit(("https", "old.reddit.com", parsed.path or "/", parsed.query, ""))


def reddit_json_url(raw_url: str) -> Optional[str]:
    old_url = old_reddit_url(raw_url)
    if not old_url:
        return None
    parsed = urlsplit(old_url)
    path = parsed.path or "/"
    if not path.endswith(".json"):
        path = path.rstrip("/") + "/.json"
    return urlunsplit((parsed.scheme, parsed.netloc, path, parsed.query, ""))


def full_reddit_permalink(permalink: str) -> Optional[str]:
    if not permalink:
        return None
    if permalink.startswith(("http://", "https://")):
        return old_reddit_url(permalink)
    return old_reddit_url(f"https://old.reddit.com{permalink}")


def read_reddit_seed_rows(path: str) -> List[Dict[str, str]]:
    with open(path, newline="", encoding="utf-8") as handle:
        rows = list(csv.DictReader(handle))
    return [
        row
        for row in rows
        if row.get("source", "").strip() == "manual_reddit_discovery"
    ]


def fetch_json(url: str, user_agent: str, timeout: int) -> object:
    request = Request(url, headers={"User-Agent": user_agent})
    with urlopen(request, timeout=timeout) as response:
        return json.loads(response.read().decode("utf-8"))


def load_json_file(path: str) -> object:
    with open(path, encoding="utf-8") as handle:
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
        if (
            isinstance(child, dict)
            and child.get("kind") == "t3"
            and isinstance(child.get("data"), dict)
        ):
            yield child["data"]


def iter_comment_urls(payload: object) -> Iterable[str]:
    if isinstance(payload, list):
        for item in payload:
            yield from iter_comment_urls(item)
        return
    if not isinstance(payload, dict):
        return

    data = payload.get("data")
    if not isinstance(data, dict):
        return
    body = data.get("body") or data.get("body_html") or ""
    if isinstance(body, str):
        yield from URL_PATTERN.findall(body)
    replies = data.get("replies")
    if replies:
        yield from iter_comment_urls(replies)
    children = data.get("children")
    if isinstance(children, list):
        for child in children:
            yield from iter_comment_urls(child)


def seed_from_url(
    url: str,
    row: Mapping[str, str],
    source_url: str,
    source_json_url: str,
    title: str = "",
    score: int = 0,
    subreddit: str = "",
    reddit_permalink: str = "",
) -> Optional[RedditSeed]:
    prepared = prepare_reddit_outbound_url(url)
    if not prepared:
        return None
    raw_observation, candidate = prepared
    try:
        if (
            len(raw_observation) > MAX_URL_BYTES
            or len(raw_observation.encode("utf-8")) > MAX_URL_BYTES
        ):
            return None
    except UnicodeEncodeError:
        return None
    try:
        identity = identify_url(candidate)
    except URLCanonicalizationError:
        return None
    if not identity.crawl_eligible:
        return None

    try:
        priority = int(row.get("priority", 3) or 3)
    except (TypeError, ValueError):
        return None
    if priority not in {1, 2, 3}:
        return None

    return RedditSeed(
        url=identity.canonical_url,
        url_id=identity.url_id,
        raw_url=raw_observation,
        category=row.get("category", "General") or "General",
        priority=priority,
        source_url=source_url,
        source_json_url=source_json_url,
        title=title,
        score=score,
        subreddit=subreddit,
        reddit_permalink=reddit_permalink,
    )


def discover_from_payload(
    payload: object,
    row: Mapping[str, str],
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


def seed_document(
    seed: RedditSeed, observed_at: Optional[datetime] = None
) -> Dict[str, Any]:
    observed_at = observed_at or datetime.now(timezone.utc)
    permalink = full_reddit_permalink(seed.reddit_permalink) or ""
    source_ref = permalink or seed.source_json_url
    source = make_source(
        source_type=REDDIT_SOURCE_TYPE,
        source_ref=source_ref,
        raw_url=seed.raw_url or seed.url,
        category=seed.category,
        priority=seed.priority,
        observed_at=observed_at,
        metadata={
            "source_url": seed.source_url,
            "json_url": seed.source_json_url,
            "title": seed.title,
            "score": seed.score,
            "subreddit": seed.subreddit,
            "permalink": permalink,
        },
        key=stable_source_key(REDDIT_SOURCE_TYPE, source_ref),
    )
    return new_seed_document(seed.url, source, updated_at=observed_at)


def make_operation(seed: RedditSeed, observed_at: Optional[datetime] = None) -> Any:
    return make_merge_operation(seed_document(seed, observed_at))


def flush_documents(
    collection: Any,
    documents: Mapping[str, Mapping[str, Any]],
    stats: ImportStats,
) -> None:
    if not documents:
        return
    try:
        result = write_seed_documents(collection, documents.values())
        stats.imported += getattr(result, "upserted_count", 0)
        stats.duplicate_urls += getattr(result, "matched_count", 0)
    except BulkWriteError as exc:
        details = exc.details or {}
        stats.imported += details.get("nUpserted", 0)
        stats.duplicate_urls += details.get("nMatched", 0)
        stats.write_errors += len(details.get("writeErrors", []))
        logger.warning("Bulk write completed with %s write errors", stats.write_errors)


def parse_args(argv: Optional[Sequence[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Discover outbound V1 crawl seeds from old.reddit.com JSON pages."
    )
    parser.add_argument(
        "--seeds-csv",
        default="/seeds/manual-seeds.csv",
        help="CSV containing manual_reddit_discovery rows",
    )
    parser.add_argument(
        "--url", action="append", default=[], help="additional Reddit discovery URL"
    )
    parser.add_argument(
        "--input-json", help="parse a local Reddit JSON file instead of fetching"
    )
    parser.add_argument(
        "--mongo-uri", default=mongo_uri_from_env(), help="MongoDB connection string"
    )
    parser.add_argument("--batch-size", type=int, default=1000)
    parser.add_argument("--min-score", type=int, default=25)
    parser.add_argument("--delay", type=float, default=2.0)
    parser.add_argument("--timeout", type=int, default=20)
    parser.add_argument(
        "--user-agent", default=os.getenv("USER_AGENT", "MiFolyoBot/1.0")
    )
    parser.add_argument("--include-comment-urls", action="store_true")
    parser.add_argument("--crawl-post-pages", action="store_true")
    parser.add_argument("--max-post-pages", type=int, default=25)
    parser.add_argument("--dry-run", action="store_true")
    return parser.parse_args(argv)


def rows_from_args(args: argparse.Namespace) -> List[Dict[str, str]]:
    if args.input_json:
        return [
            {
                "url": "https://old.reddit.com/r/sample",
                "category": "General",
                "priority": "3",
                "source": "reddit_fixture",
            }
        ]

    rows: List[Dict[str, str]] = []
    if os.path.exists(args.seeds_csv):
        rows.extend(read_reddit_seed_rows(args.seeds_csv))
    elif not args.url:
        logger.warning("Seed CSV not found: %s", args.seeds_csv)
    for url in args.url:
        rows.append(
            {
                "url": url,
                "category": "General",
                "priority": "3",
                "source": "manual_reddit_discovery",
            }
        )
    return rows


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = parse_args(argv)
    if args.batch_size < 1:
        logger.error("--batch-size must be at least 1")
        return 1
    rows = rows_from_args(args)
    if not rows:
        logger.error("No Reddit discovery rows found")
        return 1

    client = None
    collection = None
    if not args.dry_run:
        if MongoClient is None:
            logger.error("pymongo is required for non-dry-run imports")
            return 1
        try:
            client = MongoClient(args.mongo_uri, tz_aware=True)
            client.admin.command("ping")
            collection = ensure_crawl_seeds_collection(client[DATABASE_NAME])
        except Exception as exc:
            if client is not None:
                client.close()
            logger.error("Unable to configure the V1 crawl seed collection: %s", exc)
            return 2

    stats = ImportStats()
    seen_url_ids = set()
    pending: Dict[str, Dict[str, Any]] = {}
    observed_at = datetime.now(timezone.utc)

    try:
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
                        post_payload = fetch_json(
                            post_json_url, args.user_agent, args.timeout
                        )
                        stats.reddit_pages_fetched += 1
                        post_pages_fetched += 1
                    except (
                        HTTPError,
                        URLError,
                        TimeoutError,
                        json.JSONDecodeError,
                    ) as exc:
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
                document = seed_document(seed, observed_at)
                if document["_id"] in seen_url_ids:
                    stats.duplicate_urls += 1
                else:
                    seen_url_ids.add(document["_id"])
                    stats.outbound_urls_found += 1
                    stats.by_category[seed.category] += 1

                pending[document["_id"]] = merge_seed_documents(
                    pending.get(document["_id"]), document
                )
                if args.dry_run:
                    logger.info(
                        "Discovered %s id=%s from %s",
                        seed.url,
                        document["_id"],
                        seed.source_json_url,
                    )

                if len(pending) >= args.batch_size:
                    if not args.dry_run:
                        flush_documents(collection, pending, stats)
                    pending = {}

            if not args.input_json:
                time.sleep(args.delay)

        if not args.dry_run:
            flush_documents(collection, pending, stats)
    except Exception as exc:
        logger.error("Reddit seed discovery failed: %s", exc)
        return 2
    finally:
        if client is not None:
            client.close()

    logger.info("Reddit JSON discovery complete")
    logger.info("Reddit pages seen: %s", stats.reddit_pages_seen)
    logger.info("Reddit pages fetched: %s", stats.reddit_pages_fetched)
    logger.info("Posts seen: %s", stats.posts_seen)
    logger.info("Comment URLs seen: %s", stats.comment_urls_seen)
    logger.info("Unique outbound URLs found: %s", stats.outbound_urls_found)
    logger.info("Skipped Reddit/ineligible/invalid URLs: %s", stats.skipped_reddit_urls)
    logger.info("Skipped low-score posts: %s", stats.skipped_low_score)
    logger.info("Duplicate URL observations or existing records: %s", stats.duplicate_urls)
    logger.info("Inserted: %s", stats.imported)
    logger.info("Write errors: %s", stats.write_errors)
    for category, count in stats.by_category.most_common():
        logger.info("Category: %s = %s", category, count)
    return 0 if stats.write_errors == 0 else 2


if __name__ == "__main__":
    sys.exit(main())
