import time
import logging
import pymongo

from typing import Optional, List, Set
from pymongo.cursor import Cursor
from pymongo import UpdateOne

# SETUP LOGGER
logger = logging.getLogger(__name__)
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger(__name__)

# COLLECTIONS
METADATA_COLLECTION = "metadata"
WORDS_COLLECTION = "words"


class MongoClient:
    def __init__(
        self, host="localhost", port=27017, password="", db="test", username=""
    ):
        try:
            mongo_uri = f"mongodb://{host}:{port}/{db}"
            if username:
                mongo_uri = f"mongodb://{username}:{password}@{host}:{port}/{db}?authSource=admin"

            self.client = pymongo.MongoClient(
                mongo_uri
            )
            self.db = self.client[db]
            self.client.admin.command("ping")
            logger.info("Successfully connected to mongo!")
        except Exception as e:
            logger.error(f"Failed to connect to mongo")
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
            res = self.db[collection_name].bulk_write(operations, ordered=False)
            return res
        except Exception as e:
            logger.error(f"Error performing batch operations: {e}")
            return None

    # --------------------- METADATA ---------------------
    def get_document_count(self) -> Optional[int]:
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        collection = self.db[METADATA_COLLECTION]
        # Use estimated_document_count for performance
        # For TF-IDF we don't need that much accuracy
        result = collection.estimated_document_count({})

        return result

    # --------------------- METADATA ---------------------

    # --------------------- WORD ---------------------
    def get_unique_words(self) -> Optional[Cursor]:
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        collection = self.db[WORDS_COLLECTION]
        # Use aggregation to get unique words
        pipeline = [
            {"$group": {"_id": "$word"}},
            {"$project": {"word": "$_id", "_id": 0}},
        ]
        cursor = collection.aggregate(pipeline, allowDiskUse=True)

        # Return a cursor instead of loading all words at once
        return cursor

    def get_word_document_count(self, word: str) -> Optional[int]:
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        collection = self.db[WORDS_COLLECTION]
        count = collection.count_documents({"word": word})
        return count

    def get_word_documents(self, word: str) -> Optional[Cursor]:
        if self.client is None:
            logger.error("Mongo connection not initialized")
            return None

        collection = self.db[WORDS_COLLECTION]
        try:
            cursor = collection.find({"word": word})
            return cursor
        except Exception as e:
            logger.error(f"Failed to retrieve documents for word '{word}': {e}")
            return None

    def update_page_tfidf_op(
        self, word: str, url: str, idf: float, tfidf: float
    ) -> UpdateOne:
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        return UpdateOne(
            {"word": word, "url": url}, {"$set": {"weight": tfidf, "idf": idf}}
        )

    def update_page_tfidf_bulk(self, operations: List[UpdateOne]):
        if not operations:
            return
        return self.perform_batch_operations(operations, WORDS_COLLECTION)

    # --------------------- WORD ---------------------
