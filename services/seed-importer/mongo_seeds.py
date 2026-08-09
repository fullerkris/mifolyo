"""MongoDB persistence contract for ``mifolyo_index.crawl_seeds``."""

from __future__ import annotations

from typing import Any, Dict, Iterable, List, Mapping

from crawl_seed import (
    ROOT_FIELDS,
    SOURCE_RAW_URL_MAX_UTF8_BYTES,
    validate_seed_document,
)

try:
    from pymongo import ASCENDING, UpdateOne
except ModuleNotFoundError:  # Unit tests and dry-runs do not require pymongo.
    ASCENDING = 1
    UpdateOne = None


DATABASE_NAME = "mifolyo_index"
COLLECTION_NAME = "crawl_seeds"


class IncompatibleCrawlSeedCollection(RuntimeError):
    """A nonempty collection cannot be safely upgraded in place."""


SOURCE_BSON_SCHEMA: Dict[str, Any] = {
    "bsonType": "object",
    "additionalProperties": False,
    "required": [
        "key",
        "type",
        "source_ref",
        "raw_url",
        "category",
        "priority",
        "observed_at",
        "metadata",
    ],
    "properties": {
        "key": {"bsonType": "string", "minLength": 1},
        "type": {"bsonType": "string", "minLength": 1},
        "source_ref": {"bsonType": "string"},
        "raw_url": {"bsonType": "string", "minLength": 1, "maxLength": 2048},
        "category": {"bsonType": "string", "minLength": 1},
        "priority": {"bsonType": "int", "minimum": 1, "maximum": 3},
        "observed_at": {"bsonType": "date"},
        "metadata": {"bsonType": "object"},
    },
}

CRAWL_SEED_BSON_SCHEMA: Dict[str, Any] = {
    "bsonType": "object",
    "additionalProperties": False,
    "required": [
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
    ],
    "properties": {
        "_id": {"bsonType": "string", "pattern": "^[a-f0-9]{64}$"},
        "schema_version": {"bsonType": "int", "enum": [1]},
        "canonicalization_version": {"bsonType": "int", "enum": [1]},
        "canonical_url": {
            "bsonType": "string",
            "minLength": 1,
            "maxLength": 2048,
            "pattern": "^https?://",
        },
        "enabled": {"bsonType": "bool"},
        "priority": {"bsonType": "int", "minimum": 1, "maximum": 3},
        "categories": {
            "bsonType": "array",
            "minItems": 1,
            "uniqueItems": True,
            "items": {"bsonType": "string", "minLength": 1},
        },
        "sources": {
            "bsonType": "array",
            "minItems": 1,
            "maxItems": 100,
            "items": SOURCE_BSON_SCHEMA,
        },
        "discovered_at": {"bsonType": "date"},
        "updated_at": {"bsonType": "date"},
    },
}

RAW_URL_BYTE_LENGTH_VALIDATOR: Dict[str, Any] = {
    "$expr": {
        "$cond": [
            {"$isArray": "$sources"},
            {
                "$allElementsTrue": [
                    {
                        "$map": {
                            "input": "$sources",
                            "as": "source",
                            "in": {
                                "$cond": [
                                    {
                                        "$eq": [
                                            {"$type": "$$source.raw_url"},
                                            "string",
                                        ]
                                    },
                                    {
                                        "$lte": [
                                            {"$strLenBytes": "$$source.raw_url"},
                                            SOURCE_RAW_URL_MAX_UTF8_BYTES,
                                        ]
                                    },
                                    False,
                                ]
                            },
                        }
                    }
                ]
            },
            False,
        ]
    }
}

# MongoDB permits query expressions alongside $jsonSchema in validators.
# maxLength counts characters, while the V1 source contract requires a UTF-8
# byte ceiling, so $strLenBytes closes that gap for nested raw URLs.
COLLECTION_VALIDATOR = {
    "$and": [
        {"$jsonSchema": CRAWL_SEED_BSON_SCHEMA},
        RAW_URL_BYTE_LENGTH_VALIDATOR,
    ]
}


def ensure_seed_indexes(collection: Any) -> None:
    collection.create_index(
        [("canonical_url", ASCENDING)],
        name="uq_crawl_seeds_canonical_url",
        unique=True,
        collation={"locale": "simple"},
    )
    collection.create_index(
        [
            ("enabled", ASCENDING),
            ("priority", ASCENDING),
            ("discovered_at", ASCENDING),
            ("_id", ASCENDING),
        ],
        name="ix_crawl_seeds_feed",
    )
    collection.create_index(
        [("sources.key", ASCENDING)],
        name="ix_crawl_seeds_source_key",
        collation={"locale": "simple"},
    )


def create_crawl_seeds_collection(database: Any, name: str = COLLECTION_NAME) -> Any:
    collection = database.create_collection(
        name,
        validator=COLLECTION_VALIDATOR,
        validationLevel="strict",
        validationAction="error",
    )
    ensure_seed_indexes(collection)
    return collection


def assert_nonempty_collection_is_v1_compatible(collection: Any) -> None:
    """Read-only preflight before any collMod or index operation."""

    count_before = int(collection.count_documents({}))
    if count_before == 0:
        return

    checked = 0
    try:
        for document in collection.find({}):
            validate_seed_document(document)
            checked += 1
        count_after = int(collection.count_documents({}))
        if checked != count_before or count_after != count_before:
            raise RuntimeError("collection changed during compatibility preflight")
    except Exception as exc:
        raise IncompatibleCrawlSeedCollection(
            "Nonempty crawl_seeds is not V1-compatible; no validator or index "
            "changes were applied. Automated production migration is out of "
            "scope. Use the guarded rebuild only against an approved local-"
            "development MongoDB target."
        ) from exc


def ensure_crawl_seeds_collection(database: Any) -> Any:
    """Safely create or tighten the V1 collection after a read-only preflight."""

    if COLLECTION_NAME in database.list_collection_names():
        collection = database[COLLECTION_NAME]
        assert_nonempty_collection_is_v1_compatible(collection)
        database.command(
            {
                "collMod": COLLECTION_NAME,
                "validator": COLLECTION_VALIDATOR,
                "validationLevel": "strict",
                "validationAction": "error",
            }
        )
    else:
        return create_crawl_seeds_collection(database)

    ensure_seed_indexes(collection)
    return collection


def mongo_merge_pipeline(document: Mapping[str, Any]) -> List[Dict[str, Any]]:
    """Build an atomic provenance merge update for one validated V1 record."""

    validate_seed_document(document)
    incoming_sources = list(document["sources"])
    incoming_keys = [source["key"] for source in incoming_sources]
    literal_keys = {"$literal": incoming_keys}
    literal_discovered = {"$literal": document["discovered_at"]}
    literal_updated = {"$literal": document["updated_at"]}
    existing_sources = {"$ifNull": ["$sources", []]}

    preserve_conditions: List[Dict[str, Any]] = [
        {"$not": [{"$in": ["$$source.key", literal_keys]}]}
    ]
    incoming_arrays: List[Dict[str, Any]] = []
    for source in incoming_sources:
        literal_key = {"$literal": source["key"]}
        literal_observed = {"$literal": source["observed_at"]}
        stored_is_same_or_newer = {
            "$and": [
                {"$eq": ["$$source.key", literal_key]},
                {"$gte": ["$$source.observed_at", literal_observed]},
            ]
        }
        preserve_conditions.append(stored_is_same_or_newer)
        incoming_arrays.append(
            {
                "$cond": [
                    {
                        "$anyElementTrue": [
                            {
                                "$map": {
                                    "input": existing_sources,
                                    "as": "source",
                                    "in": stored_is_same_or_newer,
                                }
                            }
                        ]
                    },
                    [],
                    {"$literal": [source]},
                ]
            }
        )

    preserved_existing = {
        "$filter": {
            "input": existing_sources,
            "as": "source",
            "cond": {"$or": preserve_conditions},
        }
    }

    return [
        {
            "$set": {
                "schema_version": {"$literal": 1},
                "canonicalization_version": {"$literal": 1},
                "canonical_url": {"$literal": document["canonical_url"]},
                "enabled": {
                    "$ifNull": ["$enabled", {"$literal": document["enabled"]}]
                },
                "sources": {
                    "$sortArray": {
                        "input": {
                            "$concatArrays": [
                                preserved_existing,
                                *incoming_arrays,
                            ]
                        },
                        "sortBy": {"key": 1},
                    }
                },
                "discovered_at": {
                    "$cond": [
                        {
                            "$or": [
                                {
                                    "$eq": [
                                        {"$type": "$discovered_at"},
                                        "missing",
                                    ]
                                },
                                {
                                    "$gt": [
                                        "$discovered_at",
                                        literal_discovered,
                                    ]
                                },
                            ]
                        },
                        literal_discovered,
                        "$discovered_at",
                    ]
                },
                "updated_at": {
                    "$cond": [
                        {
                            "$or": [
                                {"$eq": [{"$type": "$updated_at"}, "missing"]},
                                {"$lt": ["$updated_at", literal_updated]},
                            ]
                        },
                        literal_updated,
                        "$updated_at",
                    ]
                },
            }
        },
        {
            "$set": {
                "priority": {"$min": "$sources.priority"},
                "categories": {
                    "$sortArray": {
                        "input": {"$setUnion": ["$sources.category", []]},
                        "sortBy": 1,
                    }
                },
            }
        },
        {"$project": {field: 1 for field in sorted(ROOT_FIELDS)}},
    ]


def make_merge_operation(document: Mapping[str, Any]) -> Any:
    if UpdateOne is None:
        raise RuntimeError("pymongo is required for MongoDB seed writes")
    return UpdateOne(
        {
            "_id": document["_id"],
            "$or": [
                {"canonical_url": document["canonical_url"]},
                {"canonical_url": {"$exists": False}},
            ],
        },
        mongo_merge_pipeline(document),
        upsert=True,
        collation={"locale": "simple"},
    )


def write_seed_documents(collection: Any, documents: Iterable[Mapping[str, Any]]) -> Any:
    ordered_documents = sorted(documents, key=lambda document: document["_id"])
    operations = [make_merge_operation(document) for document in ordered_documents]
    if not operations:
        return None
    # Ordering makes baseline rebuilds reproducible.  Each operation itself is
    # atomic and merges sources by key, so replays do not duplicate provenance.
    return collection.bulk_write(operations, ordered=True)
