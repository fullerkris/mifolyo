import sys
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest.mock import MagicMock, patch

from pymongo.errors import DuplicateKeyError


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

from data.mongo_client import (  # noqa: E402
    DICTIONARY_COLLECTION,
    CANONICAL_LOCKS_COLLECTION,
    METADATA_COLLECTION,
    OUTLINKS_COLLECTION,
    PAGE_ARTIFACTS_COLLECTION,
    PAGE_ARTIFACT_RETENTION_DAYS,
    WORDS_COLLECTION,
    MongoClient,
    validate_mongo_auth,
)
from models.outlinks import Outlinks  # noqa: E402
from models.page import Page  # noqa: E402


class MongoClientTests(unittest.TestCase):
    LOCK_TOKEN = "page-indexer:" + "f" * 64

    def setUp(self):
        self.collection = MagicMock()
        self.collection.replace_one.return_value.acknowledged = True
        self.lock_collection = MagicMock()
        self.lock_collection.find_one.return_value = {"_id": "locked"}
        self.mongo = MongoClient.__new__(MongoClient)
        self.mongo.client = object()
        self.mongo.db = {
            PAGE_ARTIFACTS_COLLECTION: self.collection,
            CANONICAL_LOCKS_COLLECTION: self.lock_collection,
        }

    def rendered_page(self):
        return Page(
            normalized_url="https://example.org/app",
            html="<html><main>Rendered</main></html>",
            content_type="text/html",
            status_code=200,
            last_crawled=datetime(2026, 8, 20, 12, 0, tzinfo=timezone.utc),
            rendered=True,
            original_html="<html><main id='root'></main></html>",
            render_policy_rule="inline-fixture",
            render_policy_sha256="a" * 64,
        )

    def test_save_page_artifact_persists_distinguishable_html(self):
        page = self.rendered_page()

        self.assertTrue(self.mongo.save_page_artifact(page, 7, self.LOCK_TOKEN))

        self.collection.replace_one.assert_called_once()
        selector, artifact = self.collection.replace_one.call_args.args
        self.assertEqual(page.normalized_url, selector["_id"])
        self.assertEqual(7, selector["$or"][1]["fence_epoch"]["$lte"])
        self.assertEqual(page.original_html, artifact["original_html"])
        self.assertEqual(page.html, artifact["rendered_html"])
        self.assertTrue(artifact["rendered"])
        self.assertEqual(page.render_policy_rule, artifact["render_policy_rule"])
        self.assertEqual(page.render_policy_sha256, artifact["render_policy_sha256"])
        self.assertEqual(7, artifact["fence_epoch"])
        self.assertEqual(
            page.last_crawled + timedelta(days=PAGE_ARTIFACT_RETENTION_DAYS),
            artifact["expires_at"],
        )
        self.assertTrue(self.collection.replace_one.call_args.kwargs["upsert"])

    def test_mongo_auth_fails_closed_without_explicit_local_opt_in(self):
        with self.assertRaises(ValueError):
            validate_mongo_auth("", "", allow_insecure=False)
        validate_mongo_auth("", "", allow_insecure=True)

        with patch("data.mongo_client.pymongo.MongoClient") as client_factory:
            with self.assertRaises(ValueError):
                MongoClient()
            client_factory.assert_not_called()

    def test_mongo_auth_requires_paired_username_and_password(self):
        for username, password in [("user", ""), ("", "password")]:
            with self.subTest(username=bool(username), password=bool(password)):
                with self.assertRaises(ValueError):
                    validate_mongo_auth(username, password, allow_insecure=True)
        validate_mongo_auth("user", "password", allow_insecure=False)

    def test_mongo_initialization_creates_page_artifact_ttl_index(self):
        client = MagicMock()
        database = MagicMock()
        collections = {
            WORDS_COLLECTION: MagicMock(),
            PAGE_ARTIFACTS_COLLECTION: MagicMock(),
        }
        database.__getitem__.side_effect = lambda name: collections.setdefault(
            name, MagicMock()
        )
        client.__getitem__.return_value = database

        with patch("data.mongo_client.pymongo.MongoClient", return_value=client):
            mongo = MongoClient(allow_insecure=True)

        self.assertIs(mongo.client, client)
        collections[PAGE_ARTIFACTS_COLLECTION].create_index.assert_called_once_with(
            "expires_at",
            expireAfterSeconds=0,
            name="expires_at_ttl",
        )

    def test_save_page_artifact_reports_mongo_failure(self):
        self.collection.replace_one.side_effect = RuntimeError("write failed")

        self.assertFalse(
            self.mongo.save_page_artifact(self.rendered_page(), 7, self.LOCK_TOKEN)
        )

    def test_save_page_artifact_requires_acknowledged_write(self):
        self.collection.replace_one.return_value.acknowledged = False

        self.assertFalse(
            self.mongo.save_page_artifact(self.rendered_page(), 7, self.LOCK_TOKEN)
        )

    def test_static_page_does_not_create_artifact(self):
        page = self.rendered_page()
        page.rendered = False
        page.original_html = ""
        page.render_policy_rule = ""
        page.render_policy_sha256 = ""

        self.assertTrue(self.mongo.save_page_artifact(page, 7, self.LOCK_TOKEN))
        self.collection.replace_one.assert_not_called()

    def test_successive_disjoint_crawls_remove_stale_words_and_replace_empty_outlinks(self):
        collections = {
            name: MagicMock()
            for name in [
                WORDS_COLLECTION,
                DICTIONARY_COLLECTION,
                METADATA_COLLECTION,
                OUTLINKS_COLLECTION,
                CANONICAL_LOCKS_COLLECTION,
            ]
        }
        for collection in collections.values():
            collection.delete_many.return_value.acknowledged = True
            collection.bulk_write.return_value.acknowledged = True
            collection.replace_one.return_value.acknowledged = True
        mongo = MongoClient.__new__(MongoClient)
        mongo.client = object()
        mongo.db = collections
        collections[CANONICAL_LOCKS_COLLECTION].find_one.return_value = {"_id": "locked"}
        page = self.rendered_page()

        def html(text):
            return {
                "title": "Title",
                "description": "Description",
                "summary_text": "Summary",
                "text": text,
            }

        self.assertTrue(
            mongo.replace_search_state(
                page,
                html(["alpha"]),
                {"alpha": 1},
                Outlinks(_id=page.normalized_url, links={"https://example.org/a"}),
                7,
                self.LOCK_TOKEN,
            )
        )
        self.assertTrue(
            mongo.replace_search_state(
                page,
                html(["beta"]),
                {"beta": 1},
                Outlinks(_id=page.normalized_url, links=set()),
                7,
                self.LOCK_TOKEN,
            )
        )

        stale_filters = [
            call.args[0]
            for call in collections[WORDS_COLLECTION].delete_many.call_args_list
        ]
        self.assertEqual({"$nin": ["alpha"]}, stale_filters[0]["word"])
        self.assertEqual({"$nin": ["beta"]}, stale_filters[1]["word"])
        self.assertEqual(7, stale_filters[1]["$or"][1]["fence_epoch"]["$lte"])
        second_word_operations = collections[WORDS_COLLECTION].bulk_write.call_args_list[1].args[0]
        self.assertEqual(
            "beta",
            second_word_operations[0]._filter["word"],
        )
        self.assertEqual(7, second_word_operations[0]._doc["$set"]["fence_epoch"])
        second_outlinks = collections[OUTLINKS_COLLECTION].replace_one.call_args_list[1]
        self.assertEqual([], second_outlinks.args[1]["links"])
        self.assertTrue(second_outlinks.kwargs["upsert"])

    def test_permanent_skip_removes_all_searchable_collections(self):
        collections = {
            WORDS_COLLECTION: MagicMock(),
            METADATA_COLLECTION: MagicMock(),
            OUTLINKS_COLLECTION: MagicMock(),
            CANONICAL_LOCKS_COLLECTION: MagicMock(),
        }
        for collection in collections.values():
            collection.delete_many.return_value.acknowledged = True
            collection.delete_one.return_value.acknowledged = True
        mongo = MongoClient.__new__(MongoClient)
        mongo.client = object()
        mongo.db = collections
        collections[CANONICAL_LOCKS_COLLECTION].find_one.return_value = {"_id": "locked"}

        self.assertTrue(
            mongo.remove_search_state(
                "https://example.org/app", 9, self.LOCK_TOKEN
            )
        )
        deletion_filter = collections[WORDS_COLLECTION].delete_many.call_args.args[0]
        self.assertEqual("https://example.org/app", deletion_filter["url"])
        self.assertEqual(9, deletion_filter["$or"][1]["fence_epoch"]["$lte"])
        collections[METADATA_COLLECTION].delete_one.assert_called_once()
        collections[OUTLINKS_COLLECTION].delete_one.assert_called_once()

    def test_unacknowledged_reconciliation_blocks_success(self):
        words = MagicMock()
        words.delete_many.return_value.acknowledged = False
        mongo = MongoClient.__new__(MongoClient)
        mongo.client = object()
        mongo.db = {
            WORDS_COLLECTION: words,
            DICTIONARY_COLLECTION: MagicMock(),
            METADATA_COLLECTION: MagicMock(),
            OUTLINKS_COLLECTION: MagicMock(),
            CANONICAL_LOCKS_COLLECTION: MagicMock(),
        }
        mongo.db[CANONICAL_LOCKS_COLLECTION].find_one.return_value = {"_id": "locked"}
        self.assertFalse(
            mongo.replace_search_state(
                self.rendered_page(),
                {
                    "title": "Title",
                    "description": "Description",
                    "summary_text": "Summary",
                    "text": ["word"],
                },
                {"word": 1},
                Outlinks(_id="https://example.org/app", links=set()),
                7,
                self.LOCK_TOKEN,
            )
        )

    def test_lower_epoch_filters_reject_higher_epoch_documents(self):
        selector = MongoClient._fenced_filter({"_id": "https://example.org/app"}, 4)
        self.assertEqual(
            [
                {"fence_epoch": {"$exists": False}},
                {"fence_epoch": {"$lte": 4}},
            ],
            selector["$or"],
        )

    def test_duplicate_key_fencing_conflict_is_retryable_failure(self):
        collections = {
            WORDS_COLLECTION: MagicMock(),
            DICTIONARY_COLLECTION: MagicMock(),
            METADATA_COLLECTION: MagicMock(),
            OUTLINKS_COLLECTION: MagicMock(),
            CANONICAL_LOCKS_COLLECTION: MagicMock(),
        }
        collections[CANONICAL_LOCKS_COLLECTION].find_one.return_value = {"_id": "locked"}
        collections[WORDS_COLLECTION].delete_many.return_value.acknowledged = True
        collections[WORDS_COLLECTION].bulk_write.side_effect = DuplicateKeyError(
            "higher epoch owns posting"
        )
        mongo = MongoClient.__new__(MongoClient)
        mongo.client = object()
        mongo.db = collections
        self.assertFalse(
            mongo.replace_search_state(
                self.rendered_page(),
                {
                    "title": "Title",
                    "description": "Description",
                    "summary_text": "Summary",
                    "text": ["old"],
                },
                {"old": 1},
                Outlinks(_id="https://example.org/app", links=set()),
                3,
                self.LOCK_TOKEN,
            )
        )


if __name__ == "__main__":
    unittest.main()
