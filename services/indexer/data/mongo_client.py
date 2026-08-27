import logging
import hashlib
import pymongo

from datetime import datetime, timedelta, timezone
from typing import Callable, Optional, List, Set, Dict
from models.page import Page
from models.metadata import Metadata
from models.outlinks import Outlinks

from pymongo import UpdateOne
from pymongo.errors import BulkWriteError, DuplicateKeyError

# SETUP LOGGER
logger = logging.getLogger(__name__)
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger(__name__)

# COLLECTIONS
WORDS_COLLECTION = "words"
METADATA_COLLECTION = "metadata"
OUTLINKS_COLLECTION = "outlinks"
DICTIONARY_COLLECTION = "dictionary"
PAGE_ARTIFACTS_COLLECTION = "page_artifacts"
CANONICAL_LOCKS_COLLECTION = "canonical_url_ownership_locks"
PAGE_ARTIFACT_RETENTION_DAYS = 30


def validate_mongo_auth(username: str, password: str, allow_insecure: bool) -> None:
    if bool(username) != bool(password):
        raise ValueError("MONGO_USERNAME and MONGO_PASSWORD must be set together")
    if not username and not allow_insecure:
        raise ValueError(
            "MongoDB authentication is required unless "
            "ALLOW_INSECURE_DATASTORES=true is explicitly set for local testing"
        )


class MongoClient:
    def __init__(
        self,
        host="localhost",
        port=27017,
        password="",
        db="test",
        username="",
        allow_insecure=False,
    ):
        username = username or ""
        password = password or ""
        validate_mongo_auth(username, password, allow_insecure)
        try:
            # Bound dependency calls so SIGTERM cannot become an unbounded
            # driver wait. Unfinished Redis claims remain recoverable.
            client_options = dict(
                connectTimeoutMS=5000,
                serverSelectionTimeoutMS=5000,
                socketTimeoutMS=5000,
            )
            if username:
                client_options.update(
                    username=username,
                    password=password,
                    authSource="admin",
                )
            self.client = pymongo.MongoClient(host=host, port=port, **client_options)

            self.db = self.client[db]
            self.client.admin.command("ping")
            logger.info("Successfully connected to mongo!")

            logger.info("Creating indexes...")
            words = self.db[WORDS_COLLECTION]
            # Create a compound index to ensure uniqueness on word and url
            words.create_index([("word", 1), ("url", 1)], unique=True)
            # Create a compound index to easily sort by word and weight
            words.create_index([("word", 1), ("weight", -1)])

            # Create single field indexes
            words.create_index("word")
            words.create_index("url")

            self.db[PAGE_ARTIFACTS_COLLECTION].create_index(
                "expires_at",
                expireAfterSeconds=0,
                name="expires_at_ttl",
            )
            self.db[CANONICAL_LOCKS_COLLECTION].create_index(
                "owner_token", unique=True, name="canonical_owner_token_unique"
            )
        except Exception:
            logger.error("Failed to initialize MongoDB")
            self.client = None

    def perform_batch_operations(
        self, operations: List[UpdateOne], collection_name: str
    ):
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        if not operations:
            logger.warning(f"No operations to perform")
            return None

        try:
            return self.db[collection_name].bulk_write(operations, ordered=False)
        except Exception:
            logger.error("Error performing MongoDB batch operations")
            return None

    @staticmethod
    def _acknowledged(result) -> bool:
        return result is not None and bool(getattr(result, "acknowledged", False))

    @staticmethod
    def _fenced_filter(identity: Dict, fence_epoch: int) -> Dict:
        if not isinstance(fence_epoch, int) or fence_epoch < 1:
            raise ValueError("fence epoch must be a positive integer")
        return {
            **identity,
            "$or": [
                {"fence_epoch": {"$exists": False}},
                {"fence_epoch": {"$lte": fence_epoch}},
            ],
        }

    @staticmethod
    def lock_token(process_token: str, publication_id: str) -> str:
        if not isinstance(process_token, str) or not process_token:
            raise ValueError("process token is required")
        if not isinstance(publication_id, str) or not publication_id:
            raise ValueError("publication ID is required")
        digest = hashlib.sha256(
            f"page-indexer\0{process_token}\0{publication_id}".encode()
        ).hexdigest()
        return f"page-indexer:{digest}"

    def acquire_canonical_lock(
        self,
        normalized_url: str,
        owner_token: str,
        publication_id: str,
        fence_epoch: int,
    ) -> bool:
        """Atomically insert a non-expiring lock; existing locks are never stolen."""
        if self.client is None or not normalized_url or not owner_token:
            return False
        try:
            result = self.db[CANONICAL_LOCKS_COLLECTION].insert_one(
                {
                    "_id": normalized_url,
                    "owner_token": owner_token,
                    "service": "page-indexer",
                    "publication_id": publication_id,
                    "fence_epoch": fence_epoch,
                    "acquired_at": datetime.now(timezone.utc),
                }
            )
            return self._acknowledged(result)
        except DuplicateKeyError:
            logger.warning(
                "Canonical page ownership lock is held (url_ref=%s)",
                hashlib.sha256(normalized_url.encode()).hexdigest()[:16],
            )
            return False
        except Exception:
            logger.error("Could not acquire canonical page ownership lock")
            return False

    def owns_canonical_lock(self, normalized_url: str, owner_token: str) -> bool:
        if self.client is None:
            return False
        try:
            return (
                self.db[CANONICAL_LOCKS_COLLECTION].find_one(
                    {"_id": normalized_url, "owner_token": owner_token}, {"_id": 1}
                )
                is not None
            )
        except Exception:
            return False

    def release_canonical_lock(self, normalized_url: str, owner_token: str) -> bool:
        """Delete a canonical lock only when URL and owner token both match."""
        if self.client is None:
            return False
        try:
            result = self.db[CANONICAL_LOCKS_COLLECTION].delete_one(
                {"_id": normalized_url, "owner_token": owner_token}
            )
            return self._acknowledged(result) and result.deleted_count == 1
        except Exception:
            logger.error("Could not release canonical page ownership lock")
            return False

    def _publication_is_owned(
        self,
        normalized_url: str,
        canonical_owner_token: str,
        owner_check: Callable[[], bool],
    ) -> bool:
        return owner_check() and self.owns_canonical_lock(
            normalized_url, canonical_owner_token
        )

    def replace_search_state(
        self,
        page_data: Page,
        html_data: Dict,
        top_words: Dict[str, int],
        outlinks: Outlinks,
        fence_epoch: int,
        canonical_owner_token: str,
        owner_check: Optional[Callable[[], bool]] = None,
    ) -> bool:
        """Idempotently reconcile the complete searchable state for one URL."""
        if self.client is None:
            return False
        owner_check = owner_check or (lambda: True)
        normalized_url = page_data.normalized_url
        metadata = Metadata(
            _id=normalized_url,
            title=html_data["title"],
            description=html_data["description"],
            summary_text=html_data["summary_text"],
            last_crawled=page_data.last_crawled,
            keywords=top_words,
        )
        word_operations = [
            self.create_words_entry_operation(
                word, normalized_url, frequency, fence_epoch=fence_epoch
            )
            for word, frequency in top_words.items()
        ]
        dictionary_operations = [
            UpdateOne({"_id": word.lower()}, {"$set": {"_id": word.lower()}}, upsert=True)
            for word in {value.lower() for value in html_data["text"]}
        ]
        try:
            if not self._publication_is_owned(
                normalized_url, canonical_owner_token, owner_check
            ):
                return False
            stale_result = self.db[WORDS_COLLECTION].delete_many(
                self._fenced_filter(
                    {"url": normalized_url, "word": {"$nin": list(top_words)}},
                    fence_epoch,
                )
            )
            if not self._acknowledged(stale_result):
                return False
            if word_operations:
                if not self._publication_is_owned(
                    normalized_url, canonical_owner_token, owner_check
                ) or not self._acknowledged(
                    self.db[WORDS_COLLECTION].bulk_write(
                        word_operations, ordered=False
                    )
                ):
                    return False
            if dictionary_operations:
                if not self._publication_is_owned(
                    normalized_url, canonical_owner_token, owner_check
                ) or not self._acknowledged(
                    self.db[DICTIONARY_COLLECTION].bulk_write(
                        dictionary_operations, ordered=False
                    )
                ):
                    return False
            if not self._publication_is_owned(
                normalized_url, canonical_owner_token, owner_check
            ):
                return False
            if not self._acknowledged(
                self.db[METADATA_COLLECTION].replace_one(
                    self._fenced_filter({"_id": normalized_url}, fence_epoch),
                    {**metadata.to_dict(), "fence_epoch": fence_epoch},
                    upsert=True,
                )
            ):
                return False
            outlinks_document = {
                "_id": normalized_url,
                "links": sorted(outlinks.links),
                "fence_epoch": fence_epoch,
            }
            if not self._publication_is_owned(
                normalized_url, canonical_owner_token, owner_check
            ):
                return False
            if not self._acknowledged(
                self.db[OUTLINKS_COLLECTION].replace_one(
                    self._fenced_filter({"_id": normalized_url}, fence_epoch),
                    outlinks_document,
                    upsert=True,
                )
            ):
                return False
            return True
        except (BulkWriteError, DuplicateKeyError):
            logger.warning("MongoDB fencing conflict while reconciling search state")
            return False
        except Exception:
            logger.error("Error reconciling MongoDB search state")
            return False

    def remove_search_state(
        self,
        normalized_url: str,
        fence_epoch: int,
        canonical_owner_token: str,
        owner_check: Optional[Callable[[], bool]] = None,
    ) -> bool:
        """Idempotently remove all searchable state for a permanent skip."""
        if self.client is None or not normalized_url:
            return False
        owner_check = owner_check or (lambda: True)
        try:
            operations = (
                (WORDS_COLLECTION, "delete_many", {"url": normalized_url}),
                (METADATA_COLLECTION, "delete_one", {"_id": normalized_url}),
                (OUTLINKS_COLLECTION, "delete_one", {"_id": normalized_url}),
            )
            for collection, operation, identity in operations:
                if not self._publication_is_owned(
                    normalized_url, canonical_owner_token, owner_check
                ):
                    return False
                result = getattr(self.db[collection], operation)(
                    self._fenced_filter(identity, fence_epoch)
                )
                if not self._acknowledged(result):
                    return False
            return self._publication_is_owned(
                normalized_url, canonical_owner_token, owner_check
            )
        except Exception:
            logger.error("Error removing MongoDB search state")
            return False

    # --------------------- WORDS ---------------------
    def create_words_entry_operation(
        self, word: str, url: str, tf: int, fence_epoch: int
    ) -> None:
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        selector = self._fenced_filter({"word": word, "url": url}, fence_epoch)
        values = {
            "word": word,
            "url": url,
            "tf": tf,
            "weight": 0,
            "fence_epoch": fence_epoch,
        }
        return UpdateOne(
            selector,
            {"$set": values},
            upsert=True,
        )

    def create_words_bulk(self, operations: List[UpdateOne]):
        if not operations:
            return
        return self.perform_batch_operations(operations, WORDS_COLLECTION)

    # --------------------- WORDS ---------------------

    # --------------------- METADATA ---------------------
    def get_metadata(self, normalized_url: str) -> Optional[Metadata]:
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        collection = self.db[METADATA_COLLECTION]
        result = collection.find_one(
            {"_id": normalized_url},
        )

        return Metadata.from_dict(result)

    def create_metadata_entry_operation(
        self,
        page_data: Page,
        html_data: Metadata,
        top_words: Dict[str, int],
        fence_epoch: int,
    ) -> None:
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        # Create Metadata object
        normalized_url = page_data.normalized_url
        metadata = Metadata(
            _id=normalized_url,
            title=html_data["title"],
            description=html_data["description"],
            summary_text=html_data["summary_text"],
            last_crawled=page_data.last_crawled,
            keywords=top_words,
        )

        return UpdateOne(
            self._fenced_filter({"_id": normalized_url}, fence_epoch),
            {
                "$set": {**metadata.to_dict(), "fence_epoch": fence_epoch},
            },
            upsert=True,
        )

    def create_metadata_bulk(self, operations: List[UpdateOne]):
        if not operations:
            return
        return self.perform_batch_operations(operations, METADATA_COLLECTION)

    # --------------------- METADATA ---------------------

    # --------------------- PAGE ARTIFACTS ---------------------
    def save_page_artifact(
        self,
        page_data: Page,
        fence_epoch: int,
        canonical_owner_token: str,
        owner_check: Optional[Callable[[], bool]] = None,
    ) -> bool:
        if not page_data.rendered:
            return True
        if self.client is None:
            logger.error("Mongo connection not initialized")
            return False
        owner_check = owner_check or (lambda: True)

        artifact = {
            "_id": page_data.normalized_url,
            "original_html": page_data.original_html,
            "rendered_html": page_data.html,
            "rendered": True,
            "render_policy_rule": page_data.render_policy_rule,
            "render_policy_sha256": page_data.render_policy_sha256,
            "content_type": page_data.content_type,
            "status_code": page_data.status_code,
            "last_crawled": page_data.last_crawled,
            "expires_at": page_data.last_crawled
            + timedelta(days=PAGE_ARTIFACT_RETENTION_DAYS),
            "fence_epoch": fence_epoch,
        }
        try:
            if not self._publication_is_owned(
                page_data.normalized_url, canonical_owner_token, owner_check
            ):
                return False
            result = self.db[PAGE_ARTIFACTS_COLLECTION].replace_one(
                self._fenced_filter(
                    {"_id": page_data.normalized_url}, fence_epoch
                ),
                artifact,
                upsert=True,
            )
            if not result.acknowledged:
                logger.error("Rendered page artifact write was not acknowledged")
                return False
            return True
        except (DuplicateKeyError, BulkWriteError):
            logger.warning("Rendered artifact fencing conflict")
            return False
        except Exception:
            logger.error("Error saving rendered page artifact")
            return False

    # --------------------- PAGE ARTIFACTS ---------------------

    # --------------------- OUTLINKS ---------------------
    def create_outlinks_entry_operation(
        self, outlinks: Outlinks, fence_epoch: int
    ) -> None:
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        if not outlinks:
            logger.error(f"Outlinks is None")
            return

        return UpdateOne(
            self._fenced_filter({"_id": outlinks._id}, fence_epoch),
            {
                "$set": {**outlinks.to_dict(), "fence_epoch": fence_epoch},
            },
            upsert=True,
        )

    def create_outlinks_bulk(self, operations: List[UpdateOne]):
        if not operations:
            return
        return self.perform_batch_operations(operations, OUTLINKS_COLLECTION)

    # --------------------- OUTLINKS ---------------------

    # --------------------- DICTIONARY ---------------------
    def add_words_to_dictionary(self, words: Set[str]) -> None:
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        operations = [
            UpdateOne(
                {"_id": word},
                {
                    "$set": {
                        "_id": word,
                    }
                },
                upsert=True,
            )
            for word in words
        ]

        if not operations:
            logger.warning(f"No operations to perform")
            return None

        return self.perform_batch_operations(operations, DICTIONARY_COLLECTION)

    # --------------------- DICTIONARY ---------------------
