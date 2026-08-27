"""Discover outbound URLs from Reddit JSON and merge V1 seed provenance."""

from __future__ import annotations

import argparse
import json
import logging
import re
import sys
from collections import Counter
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, Iterable, List, Mapping, Optional, Sequence
from urllib.parse import parse_qsl, quote, unquote_to_bytes, urlsplit, urlunsplit

import jsonschema

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


URL_PATTERN = re.compile(r"https?://[^\s<>()\[\]{}\"']+")
REDDIT_SOURCE_TYPE = "reddit_json"
MAX_REDDIT_EXPORT_BYTES = 5 * 1024 * 1024
REDDIT_EXPORT_FIELDS = {
    "schema_version",
    "source_url",
    "category",
    "priority",
    "payload",
}
REDDIT_PAGE_HOSTS = {
    "reddit.com",
    "www.reddit.com",
    "old.reddit.com",
}
AMBIGUOUS_REDDIT_PATH_ESCAPE = re.compile(r"%(?:25|2F|5C)", re.IGNORECASE)
REDDIT_PATH_SAFE = "/:@!$&'()*+,;=-._~"
with (Path(__file__).resolve().parent / "reddit-export-v1.schema.json").open(
    encoding="utf-8"
) as _schema_handle:
    REDDIT_EXPORT_VALIDATOR = jsonschema.Draft202012Validator(
        json.load(_schema_handle)
    )


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
        identity = identify_url(raw_url)
    except URLCanonicalizationError:
        return raw_url
    if not identity.crawl_eligible or not identity.canonical_url.startswith(
        "https://"
    ):
        return raw_url
    parsed = urlsplit(identity.canonical_url)
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
    if is_reddit_owned_host(host):
        return None
    return observed, candidate


def is_reddit_owned_host(host: str) -> bool:
    host = host.lower()
    return (
        host == "reddit.com"
        or host.endswith(".reddit.com")
        or host == "redd.it"
        or host.endswith(".redd.it")
    )


def _is_unambiguous_reddit_path(path: str) -> bool:
    if "\\" in path or "//" in path or AMBIGUOUS_REDDIT_PATH_ESCAPE.search(path):
        return False
    try:
        decoded = unquote_to_bytes(path).decode("utf-8", "strict")
    except UnicodeDecodeError:
        return False
    if quote(decoded, safe=REDDIT_PATH_SAFE) != path:
        return False
    return all(segment not in {".", ".."} for segment in decoded.split("/"))


def _reddit_page_parts(raw_url: str):
    try:
        identity = identify_url(raw_url)
    except URLCanonicalizationError:
        return None
    if not identity.crawl_eligible:
        return None
    parsed = urlsplit(identity.canonical_url)
    if (parsed.hostname or "").lower() not in REDDIT_PAGE_HOSTS:
        return None
    if not _is_unambiguous_reddit_path(parsed.path):
        return None
    return parsed, "?" in identity.canonical_url


def _reddit_url(host: str, path: str, query: str, has_query: bool) -> str:
    result = urlunsplit(("https", host, path, query, ""))
    if has_query and not query:
        result += "?"
    return result


def _validated_generated_reddit_url(raw_url: str) -> Optional[str]:
    try:
        identity = identify_url(raw_url)
    except URLCanonicalizationError:
        return None
    if not identity.crawl_eligible or identity.canonical_url != raw_url:
        return None
    parsed = urlsplit(identity.canonical_url)
    if (parsed.hostname or "").lower() not in REDDIT_PAGE_HOSTS:
        return None
    if not _is_unambiguous_reddit_path(parsed.path):
        return None
    return identity.canonical_url


def reddit_html_url(raw_url: str) -> Optional[str]:
    parts = _reddit_page_parts(raw_url)
    if not parts:
        return None
    parsed, has_query = parts
    host = (parsed.hostname or "").lower()
    if host == "reddit.com":
        host = "www.reddit.com"
    return _validated_generated_reddit_url(
        _reddit_url(host, _reddit_html_path(parsed.path), parsed.query, has_query)
    )


def old_reddit_url(raw_url: str) -> Optional[str]:
    parts = _reddit_page_parts(raw_url)
    if not parts:
        return None
    parsed, has_query = parts
    return _validated_generated_reddit_url(
        _reddit_url(
            "old.reddit.com",
            _reddit_html_path(parsed.path),
            parsed.query,
            has_query,
        )
    )


def _reddit_html_path(path: str) -> str:
    path = path or "/"
    if path != "/":
        path = path.rstrip("/")
    if path.endswith("/.json"):
        path = path[: -len("/.json")]
    elif path.endswith(".json"):
        path = path[: -len(".json")]
    return path or "/"


def _reddit_json_path(path: str) -> str:
    html_path = _reddit_html_path(path)
    if html_path == "/":
        return "/.json"
    return f"{html_path}.json"


def reddit_json_url(raw_url: str) -> Optional[str]:
    parts = _reddit_page_parts(raw_url)
    if not parts:
        return None
    parsed, has_query = parts
    host = (parsed.hostname or "").lower()
    if host == "reddit.com":
        host = "www.reddit.com"
    return _validated_generated_reddit_url(
        _reddit_url(
            host,
            _reddit_json_path(parsed.path),
            parsed.query,
            has_query,
        )
    )


def reddit_crawl_urls(raw_url: str) -> Optional[tuple[str, ...]]:
    """Build pending HTML and JSON targets without authorizing a fetch."""

    parts = _reddit_page_parts(raw_url)
    if not parts:
        return None
    parsed, has_query = parts

    html_path = _reddit_html_path(parsed.path)
    json_path = _reddit_json_path(parsed.path)
    variants = (
        _reddit_url("www.reddit.com", html_path, parsed.query, has_query),
        _reddit_url("old.reddit.com", html_path, parsed.query, has_query),
        _reddit_url("www.reddit.com", json_path, parsed.query, has_query),
        _reddit_url("old.reddit.com", json_path, parsed.query, has_query),
    )
    validated = tuple(_validated_generated_reddit_url(url) for url in variants)
    if any(url is None for url in validated):
        return None
    return validated


def full_reddit_permalink(permalink: str) -> Optional[str]:
    if not permalink:
        return None
    if permalink.startswith(("http://", "https://")):
        return old_reddit_url(permalink)
    return old_reddit_url(f"https://old.reddit.com{permalink}")


def load_json_file(path: str) -> object:
    with open(path, "rb") as handle:
        data = handle.read(MAX_REDDIT_EXPORT_BYTES + 1)
    if len(data) > MAX_REDDIT_EXPORT_BYTES:
        raise ValueError("Reddit export exceeds the byte limit")
    return json.loads(
        data.decode("utf-8"),
        object_pairs_hook=_unique_json_object,
        parse_constant=_reject_json_constant,
    )


def _unique_json_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON member {key!r}")
        result[key] = value
    return result


def _reject_json_constant(value):
    raise ValueError(f"non-standard JSON constant {value!r}")


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


def validate_reddit_export(document: object) -> tuple[Dict[str, object], object]:
    try:
        REDDIT_EXPORT_VALIDATOR.validate(document)
    except jsonschema.ValidationError as exc:
        raise ValueError("Reddit export does not match schema V1") from exc
    if not isinstance(document, dict) or set(document) != REDDIT_EXPORT_FIELDS:
        raise ValueError("Reddit export has invalid fields")

    source_url = document["source_url"]
    source_html_url = (
        reddit_html_url(source_url) if isinstance(source_url, str) else None
    )
    source_json_url = (
        reddit_json_url(source_url) if isinstance(source_url, str) else None
    )
    if not source_html_url or not source_json_url or source_url != source_html_url:
        raise ValueError("Reddit export has an invalid source URL")

    category = document["category"]
    priority = document["priority"]

    payload = document["payload"]
    posts = list(iter_listing_posts(payload))
    if not posts:
        raise ValueError("Reddit export payload has no listing posts")

    source_parts = urlsplit(source_url).path.strip("/").split("/")
    if len(source_parts) != 2 or source_parts[0].lower() != "r":
        raise ValueError("Reddit export source must be one subreddit listing")
    source_subreddit = source_parts[1].casefold()
    for post in posts:
        subreddit = str(post.get("subreddit") or "").casefold()
        permalink = str(post.get("permalink") or "").casefold()
        if subreddit != source_subreddit or not permalink.startswith(
            f"/r/{source_subreddit}/"
        ):
            raise ValueError("Reddit export post does not match its source")
    return (
        {
            "url": source_html_url,
            "category": category,
            "priority": priority,
            "source": "approved_reddit_export",
        },
        payload,
    )


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
        description="Discover outbound V1 crawl seeds from an approved local Reddit JSON export."
    )
    parser.add_argument(
        "--input-json", help="versioned approved local Reddit export"
    )
    parser.add_argument(
        "--mongo-uri", default=mongo_uri_from_env(), help="MongoDB connection string"
    )
    parser.add_argument("--batch-size", type=int, default=1000)
    parser.add_argument("--min-score", type=int, default=25)
    parser.add_argument("--include-comment-urls", action="store_true")
    parser.add_argument("--dry-run", action="store_true")
    return parser.parse_args(argv)


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = parse_args(argv)
    if args.batch_size < 1:
        logger.error("--batch-size must be at least 1")
        return 1
    if not args.input_json:
        logger.error(
            "Remote Reddit access is disabled pending approved API access; "
            "use --input-json with an approved local export"
        )
        return 1
    try:
        export = load_json_file(args.input_json)
        row, payload = validate_reddit_export(export)
    except (
        OSError,
        UnicodeDecodeError,
        json.JSONDecodeError,
        ValueError,
        RecursionError,
    ) as exc:
        logger.error(
            "Unable to load approved Reddit export %s: %s", args.input_json, exc
        )
        return 1

    stats = ImportStats()
    seen_url_ids = set()
    pending: Dict[str, Dict[str, Any]] = {}
    observed_at = datetime.now(timezone.utc)

    try:
        source_url = reddit_html_url(row["url"])
        source_json_url = reddit_json_url(row["url"])
        if not source_url or not source_json_url:
            raise ValueError("validated Reddit export source became invalid")

        stats.reddit_pages_seen += 1
        discovered = discover_from_payload(
            payload,
            row,
            source_url,
            source_json_url,
            args.min_score,
            args.include_comment_urls,
            stats,
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

    except Exception as exc:
        logger.error("Reddit seed discovery failed: %s", exc)
        return 2

    if not args.dry_run and pending:
        if MongoClient is None:
            logger.error("pymongo is required for non-dry-run imports")
            return 1
        client = None
        try:
            client = MongoClient(args.mongo_uri, tz_aware=True)
            client.admin.command("ping")
            collection = ensure_crawl_seeds_collection(client[DATABASE_NAME])
            items = list(pending.items())
            for offset in range(0, len(items), args.batch_size):
                flush_documents(
                    collection,
                    dict(items[offset : offset + args.batch_size]),
                    stats,
                )
        except Exception as exc:
            logger.error("Unable to write the V1 crawl seed collection: %s", exc)
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
