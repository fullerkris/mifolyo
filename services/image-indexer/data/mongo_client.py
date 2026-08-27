import logging
import hashlib
from datetime import datetime, timezone
from typing import Callable, Dict, Iterable, List, Optional

import pymongo
from pymongo import UpdateOne
from pymongo.errors import BulkWriteError, DuplicateKeyError, OperationFailure

from models.image import Image
from utils.constants import MAX_INDEX_WORDS
from utils.utils import split_name


logger = logging.getLogger(__name__)
IMAGE_COLLECTION = "images"
METADATA_COLLECTION = "metadata"
WORD_IMAGES_COLLECTION = "word_images"
IMAGE_PAGE_STATE_COLLECTION = "image_page_state"
CANONICAL_LOCKS_COLLECTION = "canonical_url_ownership_locks"
MONGO_BATCH_SIZE = 500


def validate_mongo_auth(username: str, password: str, allow_insecure: bool) -> None:
    if bool(username) != bool(password):
        raise ValueError("MONGO_USERNAME and MONGO_PASSWORD must be set together")
    if not username and not allow_insecure:
        raise ValueError("MongoDB authentication is required")


class MongoClient:
    def __init__(
        self,
        host="localhost",
        port=27017,
        password="",
        db="test",
        username="",
        allow_insecure=False,
        timeout_ms=5000,
    ):
        username, password = username or "", password or ""
        validate_mongo_auth(username, password, allow_insecure)
        options = {
            "connectTimeoutMS": timeout_ms,
            "serverSelectionTimeoutMS": timeout_ms,
            "socketTimeoutMS": timeout_ms,
            "retryWrites": True,
        }
        if username:
            options.update(username=username, password=password, authSource="admin")
        try:
            self.client = pymongo.MongoClient(host=host, port=port, **options)
            self.db = self.client[db]
            self.client.admin.command("ping")
            word_images = self.db[WORD_IMAGES_COLLECTION]
            # Retire the old global-source identity index. A partial index lets
            # legacy rows coexist until their owning page is recrawled.
            for name, specification in word_images.index_information().items():
                if specification.get("key") == [("word", 1), ("url", 1)]:
                    try:
                        word_images.drop_index(name)
                    except OperationFailure as error:
                        # Another fail-closed instance may have completed the
                        # same idempotent startup migration concurrently.
                        if error.code != 27:  # IndexNotFound
                            raise
            self.db[WORD_IMAGES_COLLECTION].create_index(
                [("word", 1), ("association_id", 1)],
                unique=True,
                partialFilterExpression={"association_id": {"$type": "string"}},
                name="word_association_unique",
            )
            self.db[WORD_IMAGES_COLLECTION].create_index("word")
            self.db[WORD_IMAGES_COLLECTION].create_index("url")
            self.db[WORD_IMAGES_COLLECTION].create_index("page_url")
            self.db[IMAGE_COLLECTION].create_index("page_url")
            self.db[CANONICAL_LOCKS_COLLECTION].create_index(
                "owner_token", unique=True, name="canonical_owner_token_unique"
            )
        except Exception:
            logger.error("Failed to initialize MongoDB")
            self.client = None

    @staticmethod
    def _acknowledged(result) -> bool:
        return result is not None and bool(getattr(result, "acknowledged", False))

    @staticmethod
    def _fenced_filter(identity: Dict, fence_epoch: int) -> Dict:
        if not isinstance(fence_epoch, int) or fence_epoch < 1:
            raise ValueError("fence epoch must be positive")
        return {
            "$and": [
                identity,
                {
                    "$or": [
                        {"fence_epoch": {"$exists": False}},
                        {"fence_epoch": {"$lte": fence_epoch}},
                    ]
                },
            ]
        }

    def _page_epoch_is_current(self, page_url: str, fence_epoch: int) -> bool:
        state = self.db[IMAGE_PAGE_STATE_COLLECTION].find_one(
            {"_id": page_url}, {"fence_epoch": 1}
        )
        return bool(state and state.get("fence_epoch") == fence_epoch)

    @staticmethod
    def lock_token(process_token: str, publication_id: str) -> str:
        if not isinstance(process_token, str) or not process_token:
            raise ValueError("process token is required")
        if not isinstance(publication_id, str) or not publication_id:
            raise ValueError("publication ID is required")
        digest = hashlib.sha256(
            f"image-indexer\0{process_token}\0{publication_id}".encode()
        ).hexdigest()
        return f"image-indexer:{digest}"

    def acquire_canonical_lock(
        self, page_url: str, owner_token: str, publication_id: str, fence_epoch: int
    ) -> bool:
        """Atomically acquire a non-expiring, fail-closed canonical URL lock."""
        if self.client is None or not page_url or not owner_token:
            return False
        try:
            result = self.db[CANONICAL_LOCKS_COLLECTION].insert_one(
                {
                    "_id": page_url,
                    "owner_token": owner_token,
                    "service": "image-indexer",
                    "publication_id": publication_id,
                    "fence_epoch": fence_epoch,
                    "acquired_at": datetime.now(timezone.utc),
                }
            )
            return self._acknowledged(result)
        except DuplicateKeyError:
            logger.warning(
                "Canonical image ownership lock is held (url_ref=%s)",
                hashlib.sha256(page_url.encode()).hexdigest()[:16],
            )
            return False
        except Exception:
            logger.error("Could not acquire canonical image ownership lock")
            return False

    def owns_canonical_lock(self, page_url: str, owner_token: str) -> bool:
        if self.client is None:
            return False
        try:
            return (
                self.db[CANONICAL_LOCKS_COLLECTION].find_one(
                    {"_id": page_url, "owner_token": owner_token}, {"_id": 1}
                )
                is not None
            )
        except Exception:
            return False

    def release_canonical_lock(self, page_url: str, owner_token: str) -> bool:
        """Release only an exact owner's lock; never expires or steals locks."""
        if self.client is None:
            return False
        try:
            result = self.db[CANONICAL_LOCKS_COLLECTION].delete_one(
                {"_id": page_url, "owner_token": owner_token}
            )
            return self._acknowledged(result) and result.deleted_count == 1
        except Exception:
            logger.error("Could not release canonical image ownership lock")
            return False

    def _publication_is_current(
        self,
        page_url: str,
        owner_token: str,
        fence_epoch: int,
        owner_check: Callable[[], bool],
        require_epoch: bool = True,
    ) -> bool:
        if not owner_check() or not self.owns_canonical_lock(page_url, owner_token):
            return False
        return not require_epoch or self._page_epoch_is_current(page_url, fence_epoch)

    def _bulk_write_current(
        self,
        page_url: str,
        fence_epoch: int,
        operations: Iterable[UpdateOne],
        owner_check: Callable[[], bool],
        canonical_owner_token: str,
    ) -> bool:
        batch = []
        for operation in operations:
            batch.append(operation)
            if len(batch) < MONGO_BATCH_SIZE:
                continue
            if not self._publication_is_current(
                page_url, canonical_owner_token, fence_epoch, owner_check
            ):
                return False
            result = self.db[WORD_IMAGES_COLLECTION].bulk_write(
                batch, ordered=True
            )
            if not self._acknowledged(result):
                return False
            batch = []
        if batch:
            if not self._publication_is_current(
                page_url, canonical_owner_token, fence_epoch, owner_check
            ):
                return False
            result = self.db[WORD_IMAGES_COLLECTION].bulk_write(batch, ordered=True)
            if not self._acknowledged(result):
                return False
        return True

    def get_keywords(self, page_url: str) -> Dict[str, int]:
        if self.client is None:
            raise RuntimeError("MongoDB is not initialized")
        result = self.db[METADATA_COLLECTION].find_one(
            {"_id": page_url}, {"keywords": 1}
        )
        keywords = result.get("keywords", {}) if result else {}
        if not isinstance(keywords, dict):
            return {}
        validated = {}
        for word, weight in keywords.items():
            if (
                isinstance(word, str)
                and word
                and len(word.encode()) <= 256
                and isinstance(weight, (int, float))
            ):
                validated[word.lower()] = int(weight)
            if len(validated) >= MAX_INDEX_WORDS:
                break
        return validated

    @staticmethod
    def _word_weights(image: Image, keywords: Dict[str, int]) -> Dict[str, int]:
        weights = dict(keywords)
        metadata_words = split_name(image.filename) + split_name(image.alt)
        for word in metadata_words:
            weights[word] = keywords[word] * 100 if word in keywords else 30
        return weights

    def replace_page_images(
        self,
        page_url: str,
        publication_id: str,
        images: List[Image],
        keywords: Dict[str, int],
        fence_epoch: int,
        canonical_owner_token: str,
        owner_check: Optional[Callable[[], bool]] = None,
    ) -> bool:
        """Reconcile one complete publication, including an explicit empty one."""
        if self.client is None or not page_url:
            return False
        owner_check = owner_check or (lambda: True)
        association_ids = [image._id for image in images]
        if (
            len(association_ids) != len(set(association_ids))
            or any(
                image.page_url != page_url
                or image._id != Image.association_id(page_url, image.source_url)
                for image in images
            )
        ):
            return False
        try:
            if not self._publication_is_current(
                page_url,
                canonical_owner_token,
                fence_epoch,
                owner_check,
                require_epoch=False,
            ):
                return False
            old_association_ids = self.db[IMAGE_COLLECTION].distinct(
                "_id", {"page_url": page_url}
            )
            state = {
                "_id": page_url,
                "publication_id": publication_id,
                "association_ids": sorted(association_ids),
                "source_urls": sorted(image.source_url for image in images),
                "image_count": len(association_ids),
                "fence_epoch": fence_epoch,
            }
            state_result = self.db[IMAGE_PAGE_STATE_COLLECTION].replace_one(
                self._fenced_filter({"_id": page_url}, fence_epoch),
                state,
                upsert=True,
            )
            if not self._acknowledged(state_result) or not self._page_epoch_is_current(
                page_url, fence_epoch
            ):
                return False

            if not self._publication_is_current(
                page_url, canonical_owner_token, fence_epoch, owner_check
            ):
                return False
            stale_images = self.db[IMAGE_COLLECTION].delete_many(
                self._fenced_filter(
                    {"page_url": page_url, "_id": {"$nin": association_ids}},
                    fence_epoch,
                )
            )
            if not self._acknowledged(stale_images):
                return False

            image_operations = [
                UpdateOne(
                    self._fenced_filter({"_id": image._id}, fence_epoch),
                    {
                        "$set": {
                            **{
                                key: value
                                for key, value in image.to_dict().items()
                                if key != "_id"
                            },
                            "publication_id": publication_id,
                            "fence_epoch": fence_epoch,
                        },
                        "$setOnInsert": {"_id": image._id},
                    },
                    upsert=True,
                )
                for image in images
            ]
            for offset in range(0, len(image_operations), MONGO_BATCH_SIZE):
                if not self._publication_is_current(
                    page_url, canonical_owner_token, fence_epoch, owner_check
                ):
                    return False
                result = self.db[IMAGE_COLLECTION].bulk_write(
                    image_operations[offset : offset + MONGO_BATCH_SIZE], ordered=True
                )
                if not self._acknowledged(result):
                    return False

            stale_word_selector = {
                "$or": [
                    {"page_url": page_url},
                    {
                        "page_url": {"$exists": False},
                        "url": {"$in": old_association_ids},
                    },
                ]
            }
            if not self._publication_is_current(
                page_url, canonical_owner_token, fence_epoch, owner_check
            ):
                return False
            stale_words = self.db[WORD_IMAGES_COLLECTION].delete_many(
                self._fenced_filter(stale_word_selector, fence_epoch)
            )
            if not self._acknowledged(stale_words):
                return False

            word_operations = (
                UpdateOne(
                    self._fenced_filter(
                        {"word": word, "association_id": image._id}, fence_epoch
                    ),
                    {
                        "$set": {
                            "word": word,
                            "association_id": image._id,
                            "weight": weight,
                            "page_url": page_url,
                            "publication_id": publication_id,
                            "fence_epoch": fence_epoch,
                        }
                    },
                    upsert=True,
                )
                for image in images
                for word, weight in sorted(
                    self._word_weights(image, keywords).items()
                )
            )
            if not self._bulk_write_current(
                page_url,
                fence_epoch,
                word_operations,
                owner_check,
                canonical_owner_token,
            ):
                return False
            return self._publication_is_current(
                page_url, canonical_owner_token, fence_epoch, owner_check
            )
        except (BulkWriteError, DuplicateKeyError):
            logger.warning("MongoDB image fencing conflict")
            return False
        except Exception:
            logger.error("MongoDB image reconciliation failed")
            return False
