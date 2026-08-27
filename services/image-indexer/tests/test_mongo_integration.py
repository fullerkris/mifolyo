import os
import threading
import unittest
import uuid

import pymongo

from data.mongo_client import (
    CANONICAL_LOCKS_COLLECTION,
    IMAGE_COLLECTION,
    IMAGE_PAGE_STATE_COLLECTION,
    WORD_IMAGES_COLLECTION,
    MongoClient,
)
from models.image import Image


@unittest.skipUnless(
    os.getenv("IMAGE_INDEXER_MONGO_INTEGRATION_URI"),
    "IMAGE_INDEXER_MONGO_INTEGRATION_URI is not configured",
)
class ImageMongoIntegrationTests(unittest.TestCase):
    def setUp(self):
        self.raw = pymongo.MongoClient(
            os.environ["IMAGE_INDEXER_MONGO_INTEGRATION_URI"],
            serverSelectionTimeoutMS=5000,
        )
        self.database_name = f"mifolyo_image_test_{uuid.uuid4().hex}"
        self.mongo = MongoClient.__new__(MongoClient)
        self.mongo.client = self.raw
        self.mongo.db = self.raw[self.database_name]
        self.mongo.db[WORD_IMAGES_COLLECTION].create_index(
            [("word", 1), ("association_id", 1)],
            unique=True,
            partialFilterExpression={"association_id": {"$type": "string"}},
        )
        self.mongo.db[CANONICAL_LOCKS_COLLECTION].create_index(
            "owner_token", unique=True
        )

    def tearDown(self):
        self.raw.drop_database(self.database_name)
        self.raw.close()

    @staticmethod
    def image(page, source, publication="a" * 64, alt="shared"):
        return Image.from_contract(
            {
                "contract_version": "1",
                "publication_id": publication,
                "normalized_page_url": page,
                "normalized_source_url": source,
                "alt": alt,
            },
            publication,
            page,
        )

    def reconcile(self, page, publication, images, epoch):
        token = self.mongo.lock_token(f"process-{epoch}", publication)
        self.assertTrue(
            self.mongo.acquire_canonical_lock(page, token, publication, epoch)
        )
        try:
            self.assertTrue(
                self.mongo.replace_page_images(
                    page,
                    publication,
                    images,
                    {"shared": 2},
                    epoch,
                    token,
                )
            )
        finally:
            self.assertTrue(self.mongo.release_canonical_lock(page, token))

    def test_shared_source_is_page_scoped_and_empty_recrawl_preserves_other_page(self):
        page_a = "https://example.org/a"
        page_b = "https://example.org/b"
        source = "https://cdn.example.org/shared.jpg"

        # A legacy source-keyed record is safely adopted only by its page.
        self.mongo.db[IMAGE_COLLECTION].insert_one(
            {"_id": source, "page_url": page_a, "alt": "legacy", "filename": "shared.jpg"}
        )
        self.mongo.db[WORD_IMAGES_COLLECTION].insert_one(
            {"word": "shared", "url": source, "weight": 1}
        )
        image_a = self.image(page_a, source, "a" * 64)
        image_b = self.image(page_b, source, "b" * 64)
        self.reconcile(page_a, "a" * 64, [image_a], 1)
        self.reconcile(page_b, "b" * 64, [image_b], 2)

        self.assertNotEqual(image_a._id, image_b._id)
        self.assertEqual(2, self.mongo.db[IMAGE_COLLECTION].count_documents({"source_url": source}))
        self.assertIsNone(self.mongo.db[IMAGE_COLLECTION].find_one({"_id": source}))
        self.assertIsNone(self.mongo.db[WORD_IMAGES_COLLECTION].find_one({"url": source}))

        self.reconcile(page_a, "c" * 64, [], 3)
        self.assertIsNone(self.mongo.db[IMAGE_COLLECTION].find_one({"_id": image_a._id}))
        self.assertIsNotNone(self.mongo.db[IMAGE_COLLECTION].find_one({"_id": image_b._id}))
        self.assertEqual(
            0,
            self.mongo.db[WORD_IMAGES_COLLECTION].count_documents({"page_url": page_a}),
        )
        self.assertGreater(
            self.mongo.db[WORD_IMAGES_COLLECTION].count_documents({"page_url": page_b}),
            0,
        )
        state = self.mongo.db[IMAGE_PAGE_STATE_COLLECTION].find_one({"_id": page_a})
        self.assertEqual(0, state["image_count"])
        self.assertEqual([], state["association_ids"])

    def test_old_epoch_pause_blocks_new_owner_and_inserts_no_stale_image(self):
        page = "https://example.org/race"
        source = "https://cdn.example.org/stale.jpg"
        publication_a, publication_b = "d" * 64, "e" * 64
        token_a = self.mongo.lock_token("old", publication_a)
        token_b = self.mongo.lock_token("new", publication_b)
        self.assertTrue(
            self.mongo.acquire_canonical_lock(page, token_a, publication_a, 7)
        )
        paused, resume = threading.Event(), threading.Event()
        old_epoch_is_current = True

        def old_owner_check():
            paused.set()
            self.assertTrue(resume.wait(5))
            return old_epoch_is_current

        outcome = []
        thread = threading.Thread(
            target=lambda: outcome.append(
                self.mongo.replace_page_images(
                    page,
                    publication_a,
                    [self.image(page, source, publication_a)],
                    {"stale": 1},
                    7,
                    token_a,
                    owner_check=old_owner_check,
                )
            )
        )
        thread.start()
        self.assertTrue(paused.wait(5))
        self.assertFalse(
            self.mongo.acquire_canonical_lock(page, token_b, publication_b, 8)
        )
        self.assertFalse(
            self.mongo.replace_page_images(
                page,
                publication_b,
                [self.image(page, source, publication_b)],
                {},
                8,
                token_b,
            )
        )
        old_epoch_is_current = False
        resume.set()
        thread.join(5)
        self.assertEqual([False], outcome)
        self.assertEqual(0, self.mongo.db[IMAGE_COLLECTION].count_documents({"page_url": page}))
        self.assertTrue(self.mongo.release_canonical_lock(page, token_a))


if __name__ == "__main__":
    unittest.main()
