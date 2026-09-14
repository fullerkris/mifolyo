from typing import Iterator, Sequence

import pymongo
from bson import BSON
from bson.errors import InvalidDocument
from pymongo import UpdateOne
from pymongo.errors import BulkWriteError, DuplicateKeyError
from pymongo.write_concern import WriteConcern

from config import (
    DATASTORE_TIMEOUT_SECONDS,
    MAX_ACK_BATCH_URL_BYTES,
    MAX_CANONICAL_URL_BYTES,
    MAX_MEMBERS_PER_ACK_BATCH,
    MAX_MONGO_DOCUMENT_BYTES,
)
from url_validation import CanonicalURLValidationError, validate_canonical_url


BACKLINKS_COLLECTION = "backlinks"
OUTLINKS_COLLECTION = "outlinks"


class BacklinkDocumentRejected(ValueError):
    def __init__(self, reason: str):
        super().__init__(reason)
        self.reason = reason


class MongoWriteNotAcknowledged(RuntimeError):
    pass


class MongoWriteConflict(RuntimeError):
    pass


def validate_mongo_auth(username: str, password: str, allow_insecure: bool) -> None:
    if bool(username) != bool(password):
        raise ValueError("MONGO_USERNAME and MONGO_PASSWORD must be set together")
    if not username and not allow_insecure:
        raise ValueError(
            "MongoDB authentication is required unless "
            "ALLOW_INSECURE_DATASTORES=true is explicitly set for local testing"
        )


def _validate_url(value: str, reason: str) -> None:
    if not isinstance(value, str) or not value:
        raise BacklinkDocumentRejected(reason)
    try:
        encoded = value.encode("utf-8")
    except UnicodeEncodeError as error:
        raise BacklinkDocumentRejected(reason) from error
    if len(encoded) > MAX_CANONICAL_URL_BYTES:
        raise BacklinkDocumentRejected(reason)
    try:
        validate_canonical_url(value)
    except CanonicalURLValidationError as error:
        raise BacklinkDocumentRejected(reason) from error


class MongoClient:
    def __init__(
        self,
        host: str = "localhost",
        port: int = 27017,
        password: str = "",
        db: str = "test",
        username: str = "",
        *,
        allow_insecure: bool = False,
        timeout_ms: int = DATASTORE_TIMEOUT_SECONDS * 1000,
    ) -> None:
        username = username or ""
        password = password or ""
        validate_mongo_auth(username, password, allow_insecure)
        options = {
            "connectTimeoutMS": timeout_ms,
            "serverSelectionTimeoutMS": timeout_ms,
            "socketTimeoutMS": timeout_ms,
        }
        if username:
            options.update(
                username=username,
                password=password,
                authSource="admin",
            )
        self.client = pymongo.MongoClient(host=host, port=port, **options)
        write_concern = WriteConcern(w="majority", wtimeout=timeout_ms)
        self.db = self.client.get_database(db, write_concern=write_concern)
        self.client.admin.command("ping")

    def add_backlinks(
        self,
        target_url: str,
        source_urls: Sequence[str],
        *,
        max_members: int = MAX_MEMBERS_PER_ACK_BATCH,
        max_batch_url_bytes: int = MAX_ACK_BATCH_URL_BYTES,
        max_document_bytes: int = MAX_MONGO_DOCUMENT_BYTES,
    ):
        _validate_url(target_url, "target_url_invalid")
        unique_sources = sorted(set(source_urls))
        if not unique_sources:
            raise BacklinkDocumentRejected("member_batch_empty")
        if len(unique_sources) > max_members:
            raise BacklinkDocumentRejected("member_batch_too_large")
        for source_url in unique_sources:
            _validate_url(source_url, "member_url_invalid")
        if (
            sum(len(value.encode("utf-8")) for value in unique_sources)
            > max_batch_url_bytes
        ):
            raise BacklinkDocumentRejected("member_batch_too_large")

        collection = self.db[BACKLINKS_COLLECTION]
        existing = collection.find_one({"_id": target_url})
        if existing is None:
            existing_links = []
            projected = {"_id": target_url}
        else:
            existing_links = existing.get("links", [])
            if not isinstance(existing_links, list) or any(
                not isinstance(value, str) for value in existing_links
            ):
                raise BacklinkDocumentRejected("mongo_links_invalid")
            selector = {
                "_id": target_url,
                "$expr": {"$eq": ["$$ROOT", {"$literal": existing}]},
            }
            projected = dict(existing)

        existing_sources = set(existing_links)
        new_sources = [
            source_url
            for source_url in unique_sources
            if source_url not in existing_sources
        ]
        projected["links"] = existing_links + new_sources
        try:
            projected_size = len(BSON.encode(projected))
        except (InvalidDocument, OverflowError) as error:
            raise BacklinkDocumentRejected("mongo_document_invalid") from error
        if new_sources and projected_size >= max_document_bytes:
            raise BacklinkDocumentRejected("mongo_document_ceiling_exceeded")

        if existing is None:
            try:
                result = collection.insert_one(projected)
            except DuplicateKeyError as error:
                raise MongoWriteConflict("mongo_write_conflict") from error
            if not result.acknowledged:
                raise MongoWriteNotAcknowledged("mongo_write_not_acknowledged")
            return result

        operation = UpdateOne(
            selector,
            {
                "$setOnInsert": {"_id": target_url},
                "$addToSet": {"links": {"$each": unique_sources}},
            },
            upsert=False,
        )
        try:
            result = collection.bulk_write([operation], ordered=True)
        except DuplicateKeyError as error:
            raise MongoWriteConflict("mongo_write_conflict") from error
        except BulkWriteError as error:
            codes = {
                item.get("code") for item in error.details.get("writeErrors", [])
            }
            if 11000 in codes:
                raise MongoWriteConflict("mongo_write_conflict") from error
            if error.has_error_label("RetryableWriteError") or error.details.get(
                "writeConcernErrors"
            ):
                raise MongoWriteNotAcknowledged(
                    "mongo_write_not_acknowledged"
                ) from error
            raise BacklinkDocumentRejected("mongo_write_rejected") from error
        if not result.acknowledged:
            raise MongoWriteNotAcknowledged("mongo_write_not_acknowledged")
        if result.matched_count + result.upserted_count != 1:
            raise MongoWriteConflict("mongo_write_conflict")
        return result

    def iter_outlinks(self, *, batch_size: int = 100) -> Iterator[dict]:
        return self.db[OUTLINKS_COLLECTION].find(
            {}, {"_id": 1, "links": 1}
        ).batch_size(batch_size)

    def iter_backlinks(self, *, batch_size: int = 100) -> Iterator[dict]:
        return self.db[BACKLINKS_COLLECTION].find(
            {}, {"_id": 1, "links": 1}
        ).batch_size(batch_size)

    def has_backlink(self, target_url: str, source_url: str) -> bool:
        return (
            self.db[BACKLINKS_COLLECTION].count_documents(
                {"_id": target_url, "links": source_url}, limit=1
            )
            == 1
        )

    def has_outlink(self, source_url: str, target_url: str) -> bool:
        return (
            self.db[OUTLINKS_COLLECTION].count_documents(
                {"_id": source_url, "links": target_url}, limit=1
            )
            == 1
        )

    def close(self) -> None:
        self.client.close()
