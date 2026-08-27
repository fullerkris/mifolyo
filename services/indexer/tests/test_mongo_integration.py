import os
import threading
import unittest
import uuid
from datetime import datetime, timezone

import pymongo

from data.mongo_client import (
    CANONICAL_LOCKS_COLLECTION,
    WORDS_COLLECTION,
    MongoClient,
)
from models.outlinks import Outlinks
from models.page import Page


@unittest.skipUnless(
    os.getenv("INDEXER_MONGO_INTEGRATION_URI"),
    "INDEXER_MONGO_INTEGRATION_URI is not configured",
)
class MongoOwnershipIntegrationTests(unittest.TestCase):
    def setUp(self):
        self.raw = pymongo.MongoClient(
            os.environ["INDEXER_MONGO_INTEGRATION_URI"], serverSelectionTimeoutMS=5000
        )
        self.database_name = f"mifolyo_indexer_test_{uuid.uuid4().hex}"
        self.mongo = MongoClient.__new__(MongoClient)
        self.mongo.client = self.raw
        self.mongo.db = self.raw[self.database_name]
        self.mongo.db[WORDS_COLLECTION].create_index(
            [("word", 1), ("url", 1)], unique=True
        )
        self.mongo.db[CANONICAL_LOCKS_COLLECTION].create_index(
            "owner_token", unique=True
        )

    def tearDown(self):
        self.raw.drop_database(self.database_name)
        self.raw.close()

    def test_old_epoch_pause_blocks_new_owner_and_inserts_no_stale_word(self):
        url = "https://example.org/interleaving"
        publication_a = "a" * 64
        publication_b = "b" * 64
        token_a = self.mongo.lock_token("process-a", publication_a)
        token_b = self.mongo.lock_token("process-b", publication_b)
        self.assertTrue(
            self.mongo.acquire_canonical_lock(url, token_a, publication_a, 7)
        )
        page = Page(
            normalized_url=url,
            html="<html>old</html>",
            content_type="text/html",
            status_code=200,
            last_crawled=datetime.now(timezone.utc),
        )
        paused = threading.Event()
        resume = threading.Event()
        old_epoch_is_current = True

        def old_owner_check():
            paused.set()
            self.assertTrue(resume.wait(5))
            return old_epoch_is_current

        outcome = []
        thread = threading.Thread(
            target=lambda: outcome.append(
                self.mongo.replace_search_state(
                    page,
                    {
                        "title": "Old",
                        "description": "",
                        "summary_text": "",
                        "text": ["stale"],
                    },
                    {"stale": 1},
                    Outlinks(_id=url, links=set()),
                    7,
                    token_a,
                    owner_check=old_owner_check,
                )
            )
        )
        thread.start()
        self.assertTrue(paused.wait(5))

        # Epoch B cannot steal the non-expiring URL lock and cannot mutate by
        # merely presenting its higher epoch.
        self.assertFalse(
            self.mongo.acquire_canonical_lock(url, token_b, publication_b, 8)
        )
        self.assertFalse(
            self.mongo.replace_search_state(
                page,
                {"title": "New", "description": "", "summary_text": "", "text": ["new"]},
                {"new": 1},
                Outlinks(_id=url, links=set()),
                8,
                token_b,
            )
        )
        old_epoch_is_current = False
        resume.set()
        thread.join(5)
        self.assertEqual([False], outcome)
        self.assertEqual(0, self.mongo.db[WORDS_COLLECTION].count_documents({"url": url}))

        # A different token cannot release A. Crash/stale-lock cleanup is an
        # explicit operator decision, not lease expiry or automatic stealing.
        self.assertFalse(self.mongo.release_canonical_lock(url, token_b))
        self.assertTrue(self.mongo.release_canonical_lock(url, token_a))


if __name__ == "__main__":
    unittest.main()
