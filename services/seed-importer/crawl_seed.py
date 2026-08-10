"""Construction and deterministic provenance merging for crawl seed V1."""

from __future__ import annotations

import copy
import hashlib
import re
from datetime import datetime, timezone
from typing import Any, Dict, Iterable, Mapping, Optional

from url_identity import CANONICALIZATION_VERSION, MAX_URL_BYTES, identify_url


SCHEMA_VERSION = 1
MAX_SOURCES = 100
SOURCE_RAW_URL_MAX_UTF8_BYTES = MAX_URL_BYTES
SOURCE_KEY_NAMESPACE = b"mifolyo-crawl-source:v1\0"

ROOT_FIELDS = frozenset(
    {
        "_id",
        "schema_version",
        "canonicalization_version",
        "canonical_url",
        "enabled",
        "priority",
        "categories",
        "sources",
        "discovered_at",
        "updated_at",
    }
)
SOURCE_FIELDS = frozenset(
    {
        "key",
        "type",
        "source_ref",
        "raw_url",
        "category",
        "priority",
        "observed_at",
        "metadata",
    }
)
_URL_ID = re.compile(r"^[a-f0-9]{64}$")


class CrawlSeedValidationError(ValueError):
    pass


def utc_datetime(value: datetime) -> datetime:
    if not isinstance(value, datetime):
        raise CrawlSeedValidationError("timestamp must be a datetime")
    if value.tzinfo is None or value.utcoffset() is None:
        raise CrawlSeedValidationError("timestamp must include a UTC offset")
    normalized = value.astimezone(timezone.utc)
    # BSON dates have millisecond precision. Normalize before comparisons so
    # runtime and MongoDB apply identical newer/tie decisions.
    return normalized.replace(microsecond=(normalized.microsecond // 1000) * 1000)


def utf8_byte_length(value: str, field: str) -> int:
    try:
        return len(value.encode("utf-8"))
    except UnicodeEncodeError as exc:
        raise CrawlSeedValidationError(f"{field} must be valid UTF-8") from exc


def stable_source_key(source_type: str, source_ref: str) -> str:
    """Build a stable key from a source adapter's natural reference."""

    if not source_type:
        raise CrawlSeedValidationError("source type must not be empty")
    payload = f"{source_type}\0{source_ref}".encode("utf-8")
    digest = hashlib.sha256(SOURCE_KEY_NAMESPACE + payload).hexdigest()
    return f"{source_type}:{digest}"


def make_source(
    *,
    source_type: str,
    source_ref: str,
    raw_url: str,
    category: str,
    priority: int,
    observed_at: datetime,
    metadata: Optional[Mapping[str, Any]] = None,
    key: Optional[str] = None,
) -> Dict[str, Any]:
    source = {
        "key": key or stable_source_key(source_type, source_ref),
        "type": source_type,
        "source_ref": source_ref,
        "raw_url": raw_url,
        "category": category,
        "priority": priority,
        "observed_at": utc_datetime(observed_at),
        "metadata": copy.deepcopy(dict(metadata or {})),
    }
    _validate_source(source)
    return source


def new_seed_document(
    raw_url: str,
    source: Mapping[str, Any],
    *,
    enabled: bool = True,
    updated_at: Optional[datetime] = None,
) -> Dict[str, Any]:
    """Create one exact V1 record from a crawl-eligible URL and source."""

    identity = identify_url(raw_url)
    if not identity.crawl_eligible:
        raise CrawlSeedValidationError(
            f"URL is not statically crawl eligible: {identity.crawl_rejection}"
        )

    normalized_source = copy.deepcopy(dict(source))
    _validate_source(normalized_source)
    observed_at = utc_datetime(normalized_source["observed_at"])
    document = {
        "_id": identity.url_id,
        "schema_version": SCHEMA_VERSION,
        "canonicalization_version": CANONICALIZATION_VERSION,
        "canonical_url": identity.canonical_url,
        "enabled": enabled,
        "priority": normalized_source["priority"],
        "categories": [normalized_source["category"]],
        "sources": [normalized_source],
        "discovered_at": observed_at,
        "updated_at": utc_datetime(updated_at or observed_at),
    }
    validate_seed_document(document)
    return document


def merge_seed_documents(
    existing: Optional[Mapping[str, Any]],
    incoming: Mapping[str, Any],
    *,
    updated_at: Optional[datetime] = None,
) -> Dict[str, Any]:
    """Merge provenance by ``sources[].key`` and recompute all rollups.

    Existing ``enabled`` state wins so a non-destructive importer cannot undo
    an operator's explicit disable. A source with the same key is replaced
    only when ``observed_at`` is strictly newer; the stored source wins ties,
    making equal-timestamp replays deterministic. Distinct keys are retained.
    """

    validate_seed_document(incoming)
    if existing is None:
        result = copy.deepcopy(dict(incoming))
        if updated_at is not None:
            result["updated_at"] = utc_datetime(updated_at)
        validate_seed_document(result)
        return result

    validate_seed_document(existing)
    if existing["_id"] != incoming["_id"]:
        raise CrawlSeedValidationError("cannot merge different URL IDs")
    if existing["canonical_url"] != incoming["canonical_url"]:
        raise CrawlSeedValidationError("same URL ID has conflicting canonical URLs")

    sources = {
        source["key"]: copy.deepcopy(source) for source in existing["sources"]
    }
    for source in incoming["sources"]:
        stored = sources.get(source["key"])
        if stored is None or utc_datetime(source["observed_at"]) > utc_datetime(
            stored["observed_at"]
        ):
            sources[source["key"]] = copy.deepcopy(source)
    merged_sources = [sources[key] for key in sorted(sources)]
    if len(merged_sources) > MAX_SOURCES:
        raise CrawlSeedValidationError(f"a seed may have at most {MAX_SOURCES} sources")

    discovered_at = min(
        utc_datetime(existing["discovered_at"]),
        utc_datetime(incoming["discovered_at"]),
    )
    merged_updated_at = (
        utc_datetime(updated_at)
        if updated_at is not None
        else max(
            utc_datetime(existing["updated_at"]),
            utc_datetime(incoming["updated_at"]),
        )
    )
    if merged_updated_at < discovered_at:
        raise CrawlSeedValidationError("updated_at must not precede discovered_at")

    result = {
        "_id": existing["_id"],
        "schema_version": SCHEMA_VERSION,
        "canonicalization_version": CANONICALIZATION_VERSION,
        "canonical_url": existing["canonical_url"],
        "enabled": existing["enabled"],
        "priority": min(source["priority"] for source in merged_sources),
        "categories": sorted({source["category"] for source in merged_sources}),
        "sources": merged_sources,
        "discovered_at": discovered_at,
        "updated_at": merged_updated_at,
    }
    validate_seed_document(result)
    return result


def merge_many_seed_documents(documents: Iterable[Mapping[str, Any]]) -> Dict[str, Any]:
    merged: Optional[Dict[str, Any]] = None
    for document in documents:
        merged = merge_seed_documents(merged, document)
    if merged is None:
        raise CrawlSeedValidationError("at least one seed document is required")
    return merged


def _validate_source(source: Mapping[str, Any]) -> None:
    if set(source) != SOURCE_FIELDS:
        raise CrawlSeedValidationError("source fields do not exactly match V1")
    for field in ("key", "type", "raw_url", "category"):
        if not isinstance(source[field], str) or not source[field]:
            raise CrawlSeedValidationError(f"source.{field} must be a non-empty string")
    if not isinstance(source["source_ref"], str):
        raise CrawlSeedValidationError("source.source_ref must be a string")
    if len(source["raw_url"]) > MAX_URL_BYTES or utf8_byte_length(
        source["raw_url"], "source.raw_url"
    ) > SOURCE_RAW_URL_MAX_UTF8_BYTES:
        raise CrawlSeedValidationError("source.raw_url exceeds the V1 limit")
    if (
        isinstance(source["priority"], bool)
        or not isinstance(source["priority"], int)
        or source["priority"] not in {1, 2, 3}
    ):
        raise CrawlSeedValidationError("source.priority must be an integer from 1 to 3")
    utc_datetime(source["observed_at"])
    if not isinstance(source["metadata"], dict):
        raise CrawlSeedValidationError("source.metadata must be an object")


def validate_seed_document(document: Mapping[str, Any]) -> None:
    """Validate contract shape plus deterministic V1 semantic invariants."""

    if set(document) != ROOT_FIELDS:
        raise CrawlSeedValidationError("seed fields do not exactly match V1")
    if not isinstance(document["_id"], str) or not _URL_ID.fullmatch(document["_id"]):
        raise CrawlSeedValidationError("_id must be a lowercase SHA-256 digest")
    if (
        isinstance(document["schema_version"], bool)
        or not isinstance(document["schema_version"], int)
        or document["schema_version"] != SCHEMA_VERSION
    ):
        raise CrawlSeedValidationError("unsupported schema_version")
    if (
        isinstance(document["canonicalization_version"], bool)
        or not isinstance(document["canonicalization_version"], int)
        or document["canonicalization_version"] != CANONICALIZATION_VERSION
    ):
        raise CrawlSeedValidationError("unsupported canonicalization_version")
    if not isinstance(document["enabled"], bool):
        raise CrawlSeedValidationError("enabled must be a boolean")
    if (
        isinstance(document["priority"], bool)
        or not isinstance(document["priority"], int)
        or document["priority"] not in {1, 2, 3}
    ):
        raise CrawlSeedValidationError("priority must be an integer from 1 to 3")
    if not isinstance(document["canonical_url"], str):
        raise CrawlSeedValidationError("canonical_url must be a string")

    identity = identify_url(document["canonical_url"])
    if identity.canonical_url != document["canonical_url"]:
        raise CrawlSeedValidationError("canonical_url is not V1 canonical")
    if identity.url_id != document["_id"]:
        raise CrawlSeedValidationError("_id does not match canonical_url")
    if not identity.crawl_eligible:
        raise CrawlSeedValidationError("canonical_url is not statically crawl eligible")

    sources = document["sources"]
    if not isinstance(sources, list) or not 1 <= len(sources) <= MAX_SOURCES:
        raise CrawlSeedValidationError("sources must contain 1 to 100 entries")
    for source in sources:
        if not isinstance(source, dict):
            raise CrawlSeedValidationError("each source must be an object")
        _validate_source(source)
    source_keys = [source["key"] for source in sources]
    if len(source_keys) != len(set(source_keys)):
        raise CrawlSeedValidationError("source keys must be unique within a seed")
    if source_keys != sorted(source_keys):
        raise CrawlSeedValidationError("sources must be sorted by key")

    expected_priority = min(source["priority"] for source in sources)
    if document["priority"] != expected_priority:
        raise CrawlSeedValidationError("priority must be the lowest source priority")
    expected_categories = sorted({source["category"] for source in sources})
    if document["categories"] != expected_categories:
        raise CrawlSeedValidationError("categories must be the sorted source union")

    discovered_at = utc_datetime(document["discovered_at"])
    updated_at = utc_datetime(document["updated_at"])
    if updated_at < discovered_at:
        raise CrawlSeedValidationError("updated_at must not precede discovered_at")
