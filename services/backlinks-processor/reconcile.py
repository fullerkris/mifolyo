import argparse
import logging
import os
import sys
from dataclasses import dataclass
from typing import Iterator, Optional, Sequence

from data.mongo_client import BacklinkDocumentRejected, MongoClient
from url_validation import CanonicalURLValidationError, validate_canonical_url


logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger(__name__)


class ReconciliationRejected(ValueError):
    def __init__(self, reason: str):
        super().__init__(reason)
        self.reason = reason


@dataclass(frozen=True)
class ReconciliationReport:
    authoritative_edges: int
    missing_before: int
    added: int
    missing_after: int
    historical_edges: int
    extra_historical_edges: int


def _validated_document_edges(document: dict) -> Iterator[tuple[str, str]]:
    source_url = document.get("_id")
    links = document.get("links")
    if not isinstance(source_url, str) or not isinstance(links, list):
        raise ReconciliationRejected("outlinks_document_invalid")
    try:
        validate_canonical_url(source_url)
    except CanonicalURLValidationError as error:
        raise ReconciliationRejected("outlinks_source_invalid") from error
    if any(not isinstance(target_url, str) for target_url in links):
        raise ReconciliationRejected("outlinks_target_invalid")
    for target_url in sorted(set(links)):
        if not isinstance(target_url, str):
            raise ReconciliationRejected("outlinks_target_invalid")
        try:
            validate_canonical_url(target_url)
        except CanonicalURLValidationError as error:
            raise ReconciliationRejected("outlinks_target_invalid") from error
        yield target_url, source_url


def _authoritative_edges(mongo_client: MongoClient) -> Iterator[tuple[str, str]]:
    for document in mongo_client.iter_outlinks():
        yield from _validated_document_edges(document)


def _count_missing(mongo_client: MongoClient) -> tuple[int, int]:
    authoritative_edges = 0
    missing = 0
    for target_url, source_url in _authoritative_edges(mongo_client):
        authoritative_edges += 1
        if not mongo_client.has_backlink(target_url, source_url):
            missing += 1
    return authoritative_edges, missing


def _count_historical(mongo_client: MongoClient) -> tuple[int, int]:
    historical_edges = 0
    extra = 0
    for document in mongo_client.iter_backlinks():
        target_url = document.get("_id")
        links = document.get("links")
        if not isinstance(target_url, str) or not isinstance(links, list):
            raise ReconciliationRejected("backlinks_document_invalid")
        try:
            validate_canonical_url(target_url)
        except CanonicalURLValidationError as error:
            raise ReconciliationRejected("backlinks_target_invalid") from error
        if any(not isinstance(source_url, str) for source_url in links):
            raise ReconciliationRejected("backlinks_source_invalid")
        for source_url in set(links):
            try:
                validate_canonical_url(source_url)
            except CanonicalURLValidationError as error:
                raise ReconciliationRejected("backlinks_source_invalid") from error
            historical_edges += 1
            if not mongo_client.has_outlink(source_url, target_url):
                extra += 1
    return historical_edges, extra


def reconcile_backlinks(
    mongo_client: MongoClient, *, apply: bool
) -> ReconciliationReport:
    authoritative_edges, missing_before = _count_missing(mongo_client)
    added = 0
    if apply and missing_before:
        for target_url, source_url in _authoritative_edges(mongo_client):
            if mongo_client.has_backlink(target_url, source_url):
                continue
            mongo_client.add_backlinks(target_url, [source_url])
            added += 1

    _, missing_after = _count_missing(mongo_client)
    historical_edges, extra = _count_historical(mongo_client)
    return ReconciliationReport(
        authoritative_edges=authoritative_edges,
        missing_before=missing_before,
        added=added,
        missing_after=missing_after,
        historical_edges=historical_edges,
        extra_historical_edges=extra,
    )


def _int_env(name: str, default: int) -> int:
    value = int(os.getenv(name, str(default)))
    if value < 0 or value > 65535:
        raise ValueError(f"{name} is out of range")
    return value


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Add missing reverse edges without deleting historical backlinks"
    )
    parser.add_argument(
        "--apply", action="store_true", help="write missing reverse edges"
    )
    parser.add_argument(
        "--confirm-database",
        help="must exactly match MONGO_DB when --apply is used",
    )
    return parser


def main(argv: Optional[Sequence[str]] = None) -> int:
    parser = _parser()
    args = parser.parse_args(argv)
    database_name = os.getenv("MONGO_DB", "test")
    if args.apply and args.confirm_database != database_name:
        parser.error("--apply requires --confirm-database to exactly match MONGO_DB")

    mongo_client = None
    try:
        mongo_client = MongoClient(
            host=os.getenv("MONGO_HOST", "localhost"),
            port=_int_env("MONGO_PORT", 27017),
            username=os.getenv("MONGO_USERNAME", ""),
            password=os.getenv("MONGO_PASSWORD", ""),
            db=database_name,
            allow_insecure=os.getenv("ALLOW_INSECURE_DATASTORES") == "true",
        )
        report = reconcile_backlinks(mongo_client, apply=args.apply)
    except ReconciliationRejected as error:
        logger.error("Backlink reconciliation rejected reason=%s", error.reason)
        return 1
    except BacklinkDocumentRejected as error:
        logger.error("Backlink reconciliation write rejected reason=%s", error.reason)
        return 1
    except Exception as error:
        logger.error("Backlink reconciliation failed type=%s", type(error).__name__)
        return 1
    finally:
        if mongo_client is not None:
            mongo_client.close()

    logger.info(
        "Backlink reconciliation mode=%s authoritative=%d missing_before=%d "
        "added=%d missing_after=%d historical=%d extra_historical=%d",
        "apply" if args.apply else "report",
        report.authoritative_edges,
        report.missing_before,
        report.added,
        report.missing_after,
        report.historical_edges,
        report.extra_historical_edges,
    )
    return 0 if report.missing_after == 0 else 2


if __name__ == "__main__":
    sys.exit(main())
