"""Bootstrap or guarded-rebuild the local crawl seed V1 catalog."""

from __future__ import annotations

import argparse
import csv
import logging
import os
import sys
import uuid
from collections import Counter
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List, Mapping, Optional, Sequence

from crawl_seed import (
    make_source,
    merge_seed_documents,
    new_seed_document,
    stable_source_key,
)
from mongo_seeds import (
    COLLECTION_NAME,
    DATABASE_NAME,
    create_crawl_seeds_collection,
    ensure_crawl_seeds_collection,
    write_seed_documents,
)
from url_identity import identify_url

try:
    from pymongo import MongoClient
    from pymongo.uri_parser import parse_uri
except ModuleNotFoundError:
    MongoClient = None
    parse_uri = None


logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger(__name__)


EXPECTED_MANUAL_ROWS = 70
EXPECTED_ENABLED_MANUAL_ROWS = 67
EXPECTED_REDDIT_DISCOVERY_ROWS = 8
DIRECT_SOURCE = "manual"
REDDIT_DISCOVERY_SOURCE = "manual_reddit_discovery"
DISABLED_MANUAL_CANONICAL_URLS = frozenset(
    {
        "https://www.bbc.com/news",
        "https://www.khanacademy.org/",
        "https://www.politifact.com/",
    }
)
REBUILD_ENVIRONMENTS = frozenset({"development", "test"})
REBUILD_ENVIRONMENT_VARIABLE = "MIFOLYO_ENV"
LOCAL_MONGO_NODES = frozenset(
    {
        ("localhost", 27017),
        ("127.0.0.1", 27017),
        ("::1", 27017),
        ("mongo", 27017),
    }
)

# The CSV has no observation timestamps.  A fixed UTC sentinel makes a clean
# local rebuild reproducible rather than changing every BSON date on
# each replay.
MANUAL_BASELINE_OBSERVED_AT = datetime(1970, 1, 1, tzinfo=timezone.utc)

_LOCAL_SEEDS_PATH = (
    Path(__file__).resolve().parent.parent.parent / "seeds" / "manual-seeds.csv"
)
DEFAULT_SEEDS_PATH = (
    Path("/seeds/manual-seeds.csv")
    if Path("/seeds/manual-seeds.csv").exists()
    else _LOCAL_SEEDS_PATH
)


class BaselineSeedError(ValueError):
    pass


@dataclass(frozen=True)
class LocalMongoTarget:
    host: str
    port: int
    database: str = DATABASE_NAME
    collection: str = COLLECTION_NAME

    @property
    def authority(self) -> str:
        host = f"[{self.host}]" if ":" in self.host else self.host
        return f"{host}:{self.port}"

    @property
    def confirmation(self) -> str:
        return f"{self.authority}/{self.database}/{self.collection}"


def parse_local_mongo_target(mongo_uri: str) -> LocalMongoTarget:
    """Parse and allow only documented single-node local-development targets."""

    if parse_uri is None:
        raise RuntimeError("pymongo is required to validate the MongoDB target")
    if not mongo_uri.startswith("mongodb://"):
        raise PermissionError(
            "Only mongodb:// local-development targets are supported. Automated "
            "production migration is out of scope."
        )
    try:
        parsed = parse_uri(mongo_uri)
    except Exception as exc:
        raise ValueError(f"Invalid MongoDB URI: {exc}") from exc

    nodes = parsed.get("nodelist") or []
    database = parsed.get("database")
    if len(nodes) != 1:
        raise PermissionError(
            "Rebuild/bootstrap requires one documented local MongoDB node. "
            "Automated production migration is out of scope."
        )
    host, port = nodes[0]
    node = (str(host).lower(), int(port))
    if database != DATABASE_NAME or node not in LOCAL_MONGO_NODES:
        raise PermissionError(
            "MongoDB target must be one of localhost:27017, 127.0.0.1:27017, "
            "[::1]:27017, or mongo:27017 with database mifolyo_index. "
            "Automated production migration is out of scope."
        )
    return LocalMongoTarget(host=node[0], port=node[1])


def mongo_uri_from_env() -> str:
    if os.getenv("MONGO_URI"):
        return os.environ["MONGO_URI"]

    host = os.getenv("MONGO_HOST", "localhost")
    port = os.getenv("MONGO_PORT", "27017")
    username = os.getenv("MONGO_USERNAME", "")
    password = os.getenv("MONGO_PASSWORD", "")
    if username:
        return (
            f"mongodb://{username}:{password}@{host}:{port}/{DATABASE_NAME}"
            "?authSource=admin"
        )
    return f"mongodb://{host}:{port}/{DATABASE_NAME}"


def read_baseline_rows(path: Path | str) -> List[Dict[str, str]]:
    with Path(path).open(newline="", encoding="utf-8") as handle:
        reader = csv.DictReader(handle)
        expected_fields = ["url", "category", "priority", "source", "notes"]
        if reader.fieldnames != expected_fields:
            raise BaselineSeedError(
                f"manual seed header must be exactly {','.join(expected_fields)}"
            )
        rows = [dict(row) for row in reader]

    counts = Counter(row.get("source", "") for row in rows)
    expected_counts = {
        DIRECT_SOURCE: EXPECTED_MANUAL_ROWS,
        REDDIT_DISCOVERY_SOURCE: EXPECTED_REDDIT_DISCOVERY_ROWS,
    }
    if len(rows) != sum(expected_counts.values()) or counts != expected_counts:
        raise BaselineSeedError(
            "manual-seeds.csv must contain exactly 70 manual rows and "
            "8 manual_reddit_discovery rows"
        )
    return rows


def build_manual_seed_documents(
    path: Path | str,
    *,
    observed_at: datetime = MANUAL_BASELINE_OBSERVED_AT,
) -> List[Dict[str, Any]]:
    """Build exactly the 70 direct baseline targets in deterministic order."""

    rows = read_baseline_rows(path)
    documents: Dict[str, Dict[str, Any]] = {}

    for row in rows:
        if row["source"] == REDDIT_DISCOVERY_SOURCE:
            continue

        try:
            priority = int(row["priority"])
        except (TypeError, ValueError) as exc:
            raise BaselineSeedError(f"invalid priority for {row['url']}") from exc
        identity = identify_url(row["url"])
        if not identity.crawl_eligible:
            raise BaselineSeedError(
                f"manual URL is not crawl eligible ({identity.crawl_rejection}): "
                f"{row['url']}"
            )

        source_ref = f"seeds/manual-seeds.csv#{identity.url_id}"
        source = make_source(
            source_type=DIRECT_SOURCE,
            source_ref=source_ref,
            raw_url=row["url"],
            category=row["category"],
            priority=priority,
            observed_at=observed_at,
            metadata={"notes": row["notes"]},
            key=stable_source_key(DIRECT_SOURCE, source_ref),
        )
        incoming = new_seed_document(
            row["url"],
            source,
            enabled=identity.canonical_url not in DISABLED_MANUAL_CANONICAL_URLS,
            updated_at=observed_at,
        )
        documents[incoming["_id"]] = merge_seed_documents(
            documents.get(incoming["_id"]), incoming
        )

    if len(documents) != EXPECTED_MANUAL_ROWS:
        raise BaselineSeedError(
            "the 70 direct rows must resolve to exactly 70 distinct V1 URL IDs"
        )
    enabled_count = sum(document["enabled"] for document in documents.values())
    if enabled_count != EXPECTED_ENABLED_MANUAL_ROWS:
        raise BaselineSeedError(
            "the direct baseline must contain exactly 67 enabled and 3 disabled records"
        )
    return [documents[url_id] for url_id in sorted(documents)]


def require_rebuild_authorization(
    target: LocalMongoTarget,
    confirmation: str,
    *,
    environ: Optional[Mapping[str, str]] = None,
) -> None:
    """Require an explicit safe environment and exact parsed-target confirmation."""

    environment = os.environ if environ is None else environ
    normalized_environment = (
        environment.get(REBUILD_ENVIRONMENT_VARIABLE, "").strip().lower()
    )
    if normalized_environment not in REBUILD_ENVIRONMENTS:
        raise PermissionError(
            f"rebuild requires the process environment variable "
            f"{REBUILD_ENVIRONMENT_VARIABLE}=development or "
            f"{REBUILD_ENVIRONMENT_VARIABLE}=test; a CLI flag cannot authorize "
            "destructive execution"
        )
    if confirmation != target.confirmation:
        raise PermissionError(
            f"rebuild requires --confirm-rebuild {target.confirmation} for the "
            "actual parsed host/database/collection"
        )


def bootstrap_database(database: Any, documents: Sequence[Mapping[str, Any]]) -> Any:
    collection = ensure_crawl_seeds_collection(database)
    return write_seed_documents(collection, documents)


def rebuild_database(
    database: Any,
    documents: Sequence[Mapping[str, Any]],
    *,
    mongo_uri: str,
    confirmation: str,
    environ: Optional[Mapping[str, str]] = None,
    temporary_name: Optional[str] = None,
) -> Any:
    """Stage and validate a complete collection, then atomically replace V1."""

    target = parse_local_mongo_target(mongo_uri)
    require_rebuild_authorization(target, confirmation, environ=environ)
    database_name = getattr(database, "name", target.database)
    if database_name != target.database:
        raise PermissionError(
            "parsed confirmation database does not match the connected database"
        )
    if not documents:
        raise RuntimeError("rebuild refuses to replace crawl_seeds with an empty baseline")
    document_ids = [document["_id"] for document in documents]
    if len(document_ids) != len(set(document_ids)):
        raise RuntimeError("rebuild baseline contains duplicate URL IDs")
    stage_name = temporary_name or f"{COLLECTION_NAME}__rebuild_v1_{uuid.uuid4().hex}"
    if stage_name == COLLECTION_NAME or stage_name in database.list_collection_names():
        raise RuntimeError("rebuild staging collection name is not safely available")

    staged_collection = None
    renamed = False
    try:
        staged_collection = create_crawl_seeds_collection(database, stage_name)
        result = write_seed_documents(staged_collection, documents)

        expected_ids = sorted(document_ids)
        actual_count = int(staged_collection.count_documents({}))
        actual_ids = sorted(staged_collection.distinct("_id"))
        if actual_count != len(documents) or actual_ids != expected_ids:
            raise RuntimeError(
                "staged crawl seed collection is incomplete; target was not replaced"
            )

        validation = database.command({"validate": stage_name, "full": True})
        if validation.get("valid") is not True:
            raise RuntimeError(
                "staged crawl seed collection failed MongoDB validation; "
                "target was not replaced"
            )

        staged_collection.rename(COLLECTION_NAME, dropTarget=True)
        renamed = True
        return result
    finally:
        if not renamed:
            database.drop_collection(stage_name)


def parse_args(argv: Optional[Sequence[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Bootstrap or guarded-rebuild mifolyo_index.crawl_seeds V1."
    )
    parser.add_argument("action", choices=("bootstrap", "rebuild"))
    parser.add_argument(
        "--seeds-csv",
        default=str(DEFAULT_SEEDS_PATH),
        help="authoritative manual-seeds.csv path",
    )
    parser.add_argument(
        "--mongo-uri", default=mongo_uri_from_env(), help="MongoDB connection string"
    )
    parser.add_argument(
        "--confirm-rebuild",
        default="",
        help="rebuild confirmation bound to parsed host/database/collection",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="validate and report the exact operation without connecting to MongoDB",
    )
    return parser.parse_args(argv)


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = parse_args(argv)
    try:
        target = parse_local_mongo_target(args.mongo_uri)
        if args.action == "rebuild" and not args.dry_run:
            require_rebuild_authorization(target, args.confirm_rebuild)
        elif (
            args.action == "rebuild"
            and args.confirm_rebuild
            and args.confirm_rebuild != target.confirmation
        ):
            raise PermissionError(
                f"confirmation does not match parsed target; expected "
                f"{target.confirmation}"
            )
        documents = build_manual_seed_documents(args.seeds_csv)
    except (OSError, ValueError, PermissionError, RuntimeError) as exc:
        logger.error("%s", exc)
        return 1

    logger.info(
        "%s will import %s direct manual records (%s enabled, %s disabled) and exclude %s Reddit discovery rows",
        args.action.capitalize(),
        len(documents),
        EXPECTED_ENABLED_MANUAL_ROWS,
        len(DISABLED_MANUAL_CANONICAL_URLS),
        EXPECTED_REDDIT_DISCOVERY_ROWS,
    )
    if args.dry_run:
        if args.action == "rebuild":
            logger.info(
                "Dry-run target: %s; execution requires %s=development or "
                "%s=test, plus --confirm-rebuild %s",
                target.confirmation,
                REBUILD_ENVIRONMENT_VARIABLE,
                REBUILD_ENVIRONMENT_VARIABLE,
                target.confirmation,
            )
        logger.info("Dry-run: no MongoDB collection or document was changed")
        return 0
    if MongoClient is None:
        logger.error("pymongo is required for non-dry-run operations")
        return 1

    client = None
    try:
        client = MongoClient(args.mongo_uri, tz_aware=True)
        client.admin.command("ping")
        database = client[target.database]
        if args.action == "rebuild":
            result = rebuild_database(
                database,
                documents,
                mongo_uri=args.mongo_uri,
                confirmation=args.confirm_rebuild,
            )
        else:
            result = bootstrap_database(database, documents)
    except Exception as exc:  # PyMongo errors are reported without partial retry.
        logger.error("Crawl seed %s failed: %s", args.action, exc)
        return 2
    finally:
        if client is not None:
            client.close()

    logger.info(
        "Crawl seed %s complete: inserted=%s merged=%s",
        args.action,
        getattr(result, "upserted_count", 0),
        getattr(result, "matched_count", 0),
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
