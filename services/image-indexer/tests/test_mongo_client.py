import unittest
from unittest.mock import MagicMock

from data.mongo_client import (
    CANONICAL_LOCKS_COLLECTION,
    IMAGE_COLLECTION,
    IMAGE_PAGE_STATE_COLLECTION,
    WORD_IMAGES_COLLECTION,
    MongoClient,
    validate_mongo_auth,
)


class MongoClientTests(unittest.TestCase):
    LOCK_TOKEN = "image-indexer:" + "f" * 64

    def test_auth_fails_closed(self):
        with self.assertRaises(ValueError):
            validate_mongo_auth("", "", False)
        validate_mongo_auth("", "", True)

    def test_higher_page_epoch_fences_stale_reconciliation_before_cleanup(self):
        client = MongoClient.__new__(MongoClient)
        client.client = object()
        client.db = MagicMock()
        images = MagicMock()
        state = MagicMock()
        words = MagicMock()
        locks = MagicMock()
        locks.find_one.return_value = {"_id": "page"}
        client.db.__getitem__.side_effect = lambda name: {
            IMAGE_COLLECTION: images,
            IMAGE_PAGE_STATE_COLLECTION: state,
            WORD_IMAGES_COLLECTION: words,
            CANONICAL_LOCKS_COLLECTION: locks,
        }[name]
        images.distinct.return_value = ["old"]
        state.replace_one.return_value.acknowledged = True
        state.find_one.return_value = {"fence_epoch": 8}

        self.assertFalse(
            client.replace_page_images(
                "page", "a" * 64, [], {}, 7, self.LOCK_TOKEN
            )
        )
        images.delete_many.assert_not_called()
        words.delete_many.assert_not_called()

    def test_empty_publication_removes_images_and_word_images_and_records_zero(self):
        client = MongoClient.__new__(MongoClient)
        client.client = object()
        client.db = MagicMock()
        images, state, words = MagicMock(), MagicMock(), MagicMock()
        locks = MagicMock()
        locks.find_one.return_value = {"_id": "page"}
        client.db.__getitem__.side_effect = lambda name: {
            IMAGE_COLLECTION: images,
            IMAGE_PAGE_STATE_COLLECTION: state,
            WORD_IMAGES_COLLECTION: words,
            CANONICAL_LOCKS_COLLECTION: locks,
        }[name]
        images.distinct.return_value = ["old"]
        state.replace_one.return_value.acknowledged = True
        state.find_one.return_value = {"fence_epoch": 9}
        images.delete_many.return_value.acknowledged = True
        words.delete_many.return_value.acknowledged = True

        self.assertTrue(
            client.replace_page_images(
                "page", "b" * 64, [], {}, 9, self.LOCK_TOKEN
            )
        )
        replacement = state.replace_one.call_args.args[1]
        self.assertEqual(0, replacement["image_count"])
        self.assertEqual([], replacement["association_ids"])
        self.assertEqual([], replacement["source_urls"])
        images.delete_many.assert_called_once()
        words.delete_many.assert_called_once()


if __name__ == "__main__":
    unittest.main()
