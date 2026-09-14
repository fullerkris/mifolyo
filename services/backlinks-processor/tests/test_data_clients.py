import sys
import unittest
from pathlib import Path
from unittest.mock import MagicMock

import redis
from bson import BSON
from pymongo.errors import BulkWriteError


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

from data.mongo_client import (  # noqa: E402
    BacklinkDocumentRejected,
    MongoClient,
    MongoWriteConflict,
    MongoWriteNotAcknowledged,
    validate_mongo_auth,
)
from data.redis_client import (  # noqa: E402
    InvalidBacklinkSet,
    RedisClient,
    RedisScanLimitExceeded,
    validate_redis_auth,
)


TARGET = "https://target.example/"
SOURCE_A = "https://source-a.example/"
SOURCE_B = "https://source-b.example/"


class RedisClientTests(unittest.TestCase):
    def setUp(self):
        self.redis = RedisClient.__new__(RedisClient)
        self.redis.client = MagicMock()

    def test_authentication_is_fail_closed(self):
        with self.assertRaises(ValueError):
            validate_redis_auth("", "", False)
        with self.assertRaises(ValueError):
            validate_redis_auth("named-user", "", True)
        validate_redis_auth("", "", True)
        validate_redis_auth("", "password", False)

    def test_scan_uses_cursor_and_rejects_oversized_response(self):
        self.redis.client.scan.return_value = (7, [b"one", b"two"])

        with self.assertRaises(RedisScanLimitExceeded):
            self.redis.scan_backlink_keys(3, count_hint=11, max_items=1)
        self.redis.client.scan.assert_called_once_with(
            cursor=3, match=b"backlinks:*", count=11
        )

    def test_sscan_wrong_type_race_has_stable_reason(self):
        self.redis.client.type.return_value = b"set"
        self.redis.client.sscan.side_effect = redis.ResponseError("WRONGTYPE changed")

        with self.assertRaises(InvalidBacklinkSet) as captured:
            self.redis.scan_backlink_members(b"backlinks:key", 0)
        self.assertEqual("redis_key_wrong_type", captured.exception.reason)

    def test_sscan_rejects_oversized_response_without_removing_members(self):
        self.redis.client.type.return_value = b"set"
        self.redis.client.sscan.return_value = (7, [b"one", b"two"])
        with self.assertRaises(RedisScanLimitExceeded):
            self.redis.scan_backlink_members(b"backlinks:key", 3, max_items=1)
        self.redis.client.srem.assert_not_called()
        self.redis.client.delete.assert_not_called()


class MongoClientTests(unittest.TestCase):
    def setUp(self):
        self.collection = MagicMock()
        self.database = MagicMock()
        self.database.__getitem__.return_value = self.collection
        self.mongo = MongoClient.__new__(MongoClient)
        self.mongo.db = self.database

        self.result = MagicMock()
        self.result.acknowledged = True
        self.result.matched_count = 1
        self.result.upserted_count = 0
        self.collection.bulk_write.return_value = self.result
        self.insert_result = MagicMock()
        self.insert_result.acknowledged = True
        self.collection.insert_one.return_value = self.insert_result

    def test_authentication_is_fail_closed(self):
        with self.assertRaises(ValueError):
            validate_mongo_auth("", "", False)
        with self.assertRaises(ValueError):
            validate_mongo_auth("user", "", True)
        validate_mongo_auth("", "", True)
        validate_mongo_auth("user", "password", False)

    def test_existing_document_update_uses_exact_document_cas(self):
        existing = {"_id": TARGET, "links": [SOURCE_A], "marker": 7}
        self.collection.find_one.return_value = existing

        self.mongo.add_backlinks(TARGET, [SOURCE_B])

        operation = self.collection.bulk_write.call_args.args[0][0]
        self.assertEqual(
            {
                "_id": TARGET,
                "$expr": {"$eq": ["$$ROOT", {"$literal": existing}]},
            },
            operation._filter,
        )

    def test_document_preflight_preserves_existing_duplicates(self):
        existing = {"_id": TARGET, "links": [SOURCE_A, SOURCE_A]}
        self.collection.find_one.return_value = existing
        exact_projection = {
            "_id": TARGET,
            "links": [SOURCE_A, SOURCE_A, SOURCE_B],
        }
        deduplicated_projection = {"_id": TARGET, "links": [SOURCE_A, SOURCE_B]}
        ceiling = len(BSON.encode(exact_projection))
        self.assertGreater(ceiling, len(BSON.encode(deduplicated_projection)))

        with self.assertRaises(BacklinkDocumentRejected) as captured:
            self.mongo.add_backlinks(
                TARGET, [SOURCE_B], max_document_bytes=ceiling
            )
        self.assertEqual(
            "mongo_document_ceiling_exceeded", captured.exception.reason
        )
        self.collection.bulk_write.assert_not_called()

    def test_existing_duplicate_can_be_acknowledged_without_document_growth(self):
        self.collection.find_one.return_value = {
            "_id": TARGET,
            "links": [SOURCE_A],
        }

        self.mongo.add_backlinks(TARGET, [SOURCE_A], max_document_bytes=1)

        self.collection.bulk_write.assert_called_once()

    def test_compare_and_set_miss_is_retryable(self):
        self.collection.find_one.return_value = {"_id": TARGET, "links": []}
        self.result.matched_count = 0

        with self.assertRaises(MongoWriteConflict):
            self.mongo.add_backlinks(TARGET, [SOURCE_A])

    def test_unacknowledged_insert_and_update_are_retryable(self):
        self.result.acknowledged = False
        self.insert_result.acknowledged = False
        for existing in (None, {"_id": TARGET, "links": []}):
            with self.subTest(existing=existing):
                self.collection.find_one.return_value = existing
                with self.assertRaises(MongoWriteNotAcknowledged):
                    self.mongo.add_backlinks(TARGET, [SOURCE_A])

    def test_bulk_write_concern_failure_is_retryable(self):
        self.collection.find_one.return_value = {"_id": TARGET, "links": []}
        self.collection.bulk_write.side_effect = BulkWriteError({
            "writeErrors": [],
            "writeConcernErrors": [{"code": 64, "errmsg": "write concern timed out"}],
        })
        with self.assertRaises(MongoWriteNotAcknowledged):
            self.mongo.add_backlinks(TARGET, [SOURCE_A])

    def test_absent_document_uses_insert_only_creation(self):
        self.collection.find_one.return_value = None

        self.mongo.add_backlinks(TARGET, [SOURCE_A])

        self.collection.insert_one.assert_called_once_with(
            {"_id": TARGET, "links": [SOURCE_A]}
        )
        self.collection.bulk_write.assert_not_called()

    def test_batch_member_and_byte_bounds_are_enforced(self):
        self.collection.find_one.return_value = None
        with self.assertRaises(BacklinkDocumentRejected):
            self.mongo.add_backlinks(TARGET, [SOURCE_A, SOURCE_B], max_members=1)
        with self.assertRaises(BacklinkDocumentRejected):
            self.mongo.add_backlinks(
                TARGET, [SOURCE_A], max_batch_url_bytes=len(SOURCE_A) - 1
            )


if __name__ == "__main__":
    unittest.main()
