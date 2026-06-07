import logging
import pymongo

from typing import Optional, List
from models.image import Image

from pymongo import UpdateOne

from collections import Counter

# SETUP LOGGER
logger = logging.getLogger(__name__)
logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger(__name__)

# COLLECTIONS
IMAGE_COLLECTION = "images"
METADATA_COLLECTION = "metadata"
WORD_COLLECTION = "words"
WORD_IMAGES_COLLECTION = "word_images"

STOP_WORDS = [
    "the",
    "to",
    "is",
    "in",
    "for",
    "on",
    "and",
    "a",
    "an",
    "of",
    "with",
    "as",
    "by",
    "at",
    "this",
    "that",
    "their",
    "there",
    "it",
    "its",
    "they",
    "them",
    "he",
    "she",
    "we",
    "you",
    "your",
    "my",
    "me",
    "us",
    "our",
    "hers",
    "him",
    "his",
    "her",
    "them",
    "they're",
    "we're",
    "you're",
    "i'm",
    "it's",
    "that's",
    "who",
    "what",
    "where",
    "when",
    "why",
    "how",
    "which",
    "whom",
    "whose",
    "if",
    "then",
    "else",
    "but",
    "or",
    "not",
    "so",
    "than",
    "too",
    "very",
    "just",
    "only",
    "also",
    "such",
    "more",
    "most",
    "some",
    "any",
    "all",
    "each",
    "every",
    "few",
    "less",
    "least",
    "many",
    "much",
    "more",
    "most",
    "several",
    "both",
    "either",
    "neither",
    "one",
    "two",
    "three",
    "four",
    "a",
    "about",
    "above",
    "after",
    "again",
    "against",
    "all",
    "am",
    "an",
    "and",
    "any",
    "are",
    "aren't",
    "as",
    "at",
    "be",
    "because",
    "been",
    "before",
    "being",
    "below",
    "between",
    "both",
    "but",
    "by",
    "can't",
    "cannot",
    "could",
    "couldn't",
    "did",
    "didn't",
    "do",
    "does",
    "doesn't",
    "doing",
    "don't",
    "down",
    "during",
    "each",
    "few",
    "for",
    "from",
    "further",
    "had",
    "hadn't",
    "has",
    "hasn't",
    "have",
    "haven't",
    "having",
    "he",
    "he'd",
    "he'll",
    "he's",
    "her",
    "here",
    "here's",
    "hers",
    "herself",
    "him",
    "himself",
    "his",
    "how",
    "how's",
    "i",
    "i'd",
    "i'll",
    "i'm",
    "i've",
    "if",
    "in",
    "into",
    "is",
    "isn't",
    "it",
    "it's",
    "its",
    "itself",
    "let's",
    "me",
    "more",
    "most",
    "mustn't",
    "my",
    "myself",
    "no",
    "nor",
    "not",
    "of",
    "off",
    "on",
    "once",
    "only",
    "or",
    "other",
    "ought",
    "our",
    "ours",
    "ourselves",
    "out",
    "over",
    "own",
    "same",
    "shan't",
    "she",
    "she'd",
    "she'll",
    "she's",
    "should",
    "shouldn't",
    "so",
    "some",
]


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

            logger.info("Creating indexes...")
            word_images = self.db[WORD_IMAGES_COLLECTION]
            word_images.create_index([("word", 1), ("url", 1)], unique=True)
            word_images.create_index("word")
            word_images.create_index("url")

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
    def get_keywords(self, mongo_id: str) -> Optional[List[str]]:
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        logger.info(f"Fetching keywords for {mongo_id}")
        collection = self.db[METADATA_COLLECTION]
        result = collection.find_one({"_id": mongo_id}, {"keywords": 1})

        # Check if keywords exist
        if result is None or "keywords" not in result:
            logger.error(f"No keywords found for {mongo_id}")
            logger.info(f"Fetching for the whole document")

            result = collection.find_one({"_id": mongo_id})
            if result is None:
                logger.error(f"No result found for {mongo_id}")
                return {}

            # Total words
            total_words = []
            # Get summary text
            summary_text = result.get("summary_text", "")
            # convert the summary text to a list of words and remove stop words
            summary_text = summary_text.lower()
            summary_text = summary_text.split()
            summary_text = [word for word in summary_text if word not in STOP_WORDS]

            # Get description
            description = result.get("description", "") or ""
            # convert the description text to a list of words and remove stop words
            description = description.lower()
            description = description.split()
            description = [word for word in description if word not in STOP_WORDS]

            # Get title
            title = result.get("title", "")
            # convert the title text to a list of words and remove stop words
            title = title.lower()
            title = title.split()
            title = [word for word in title if word not in STOP_WORDS]

            # Add all words to the total words list
            total_words += summary_text
            total_words += description
            total_words += title

            # Count the words
            word_count = Counter(total_words)
            # Get the most common words
            most_common_words = dict(word_count.most_common(1000))

            return most_common_words

        # Extract keywords
        keywords = result["keywords"]
        return keywords

    # --------------------- METADATA ---------------------

    # --------------------- WORD IMAGES ---------------------
    def create_word_images_entry_operation(
        self, word: str, url: str, weight: int
    ) -> None:
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        # Write the new word entry to the new database
        return UpdateOne(
            {"word": word, "url": url},
            {
                "$set": {
                    "weight": weight,
                }
            },
            upsert=True,
        )

    def create_word_images_bulk(self, operations: List[UpdateOne]):
        if not operations:
            return
        return self.perform_batch_operations(operations, WORD_IMAGES_COLLECTION)

    # --------------------- WORD IMAGES ---------------------

    # --------------------- IMAGE ---------------------
    def create_image_operation(self, image: Image) -> UpdateOne:
        if self.client is None:
            logger.error(f"Mongo connection not initialized")
            return None

        return UpdateOne({"_id": image._id}, {"$set": image.to_dict()}, upsert=True)

    def create_images_bulk(self, save_image_operations: List[UpdateOne]):
        if not save_image_operations:
            return
        return self.perform_batch_operations(save_image_operations, IMAGE_COLLECTION)

    # --------------------- IMAGE ---------------------
