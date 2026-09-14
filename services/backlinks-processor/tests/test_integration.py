import os
import sys
import threading
import unittest
import uuid
from pathlib import Path
from unittest.mock import Mock

import pymongo
import redis
from pymongo.write_concern import WriteConcern


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

import main as processor_main  # noqa: E402
from config import ProcessingLimits  # noqa: E402
from data.mongo_client import (  # noqa: E402
    BACKLINKS_COLLECTION,
    MongoClient,
    MongoWriteConflict,
)
from data.redis_client import RedisClient, parse_backlink_member  # noqa: E402
from models.backlinks import BacklinkBatch  # noqa: E402
from reconcile import reconcile_backlinks  # noqa: E402


def integration_redis_client(address):
    url = address if "://" in address else f"redis://{address}/15"
    client = redis.Redis.from_url(
        url,
        db=15,
        decode_responses=False,
        socket_connect_timeout=5,
        socket_timeout=5,
    )
    if client.connection_pool.connection_kwargs["db"] != 15:
        client.close()
        raise ValueError("Backlinks integration tests require Redis DB15")
    return client


class NamespacedRedisClient(RedisClient):
    def __init__(self, client, key_prefix):
        self.client = client
        self.key_prefix = key_prefix

    def scan_backlink_keys(self, *args, **kwargs):
        cursor, keys = super().scan_backlink_keys(*args, **kwargs)
        return cursor, tuple(key for key in keys if key.startswith(self.key_prefix))


class IntegrationIsolationTests(unittest.TestCase):
    def test_bare_address_and_uri_default_to_db15_without_connecting(self):
        for address in (
            "127.0.0.1:6379",
            "redis://127.0.0.1:6379",
            "redis://127.0.0.1:6379/15",
            "redis://127.0.0.1:6379?db=15",
        ):
            with self.subTest(address=address):
                client = integration_redis_client(address)
                self.addCleanup(client.close)
                self.assertEqual(15, client.connection_pool.connection_kwargs["db"])

    def test_explicit_other_database_is_rejected_before_connecting(self):
        for address in (
            "redis://127.0.0.1:6379/0",
            "redis://127.0.0.1:6379/14",
            "redis://127.0.0.1:6379/15?db=0",
        ):
            with self.subTest(address=address):
                with self.assertRaisesRegex(ValueError, "require Redis DB15"):
                    integration_redis_client(address)

    def test_scan_preserves_cursor_but_only_admits_owned_keys(self):
        raw = Mock()
        owned = b"backlinks:https://owned.target.example/"
        unrelated = b"backlinks:https://another.target.example/"
        client = NamespacedRedisClient(raw, b"backlinks:https://owned.")
        raw.scan.return_value = (7, [unrelated, owned])
        self.assertEqual((7, (owned,)), client.scan_backlink_keys(0))
        raw.scan.return_value = (9, [unrelated])
        self.assertEqual((9, ()), client.scan_backlink_keys(7))


@unittest.skipUnless(
    os.getenv("BACKLINKS_REDIS_INTEGRATION_ADDR")
    and os.getenv("BACKLINKS_MONGO_INTEGRATION_URI"),
    "Backlinks Processor integration datastores are not configured",
)
class BacklinkPersistenceIntegrationTests(unittest.TestCase):
    def setUp(self):
        self.test_id = uuid.uuid4().hex
        self.target_prefix = f"https://f5-{self.test_id}."
        self.key_prefix = b"backlinks:" + self.target_prefix.encode("utf-8")
        self.keys = []
        self.raw_redis = integration_redis_client(
            os.environ["BACKLINKS_REDIS_INTEGRATION_ADDR"]
        )
        self.addCleanup(self.raw_redis.close)
        self.addCleanup(
            lambda: self.raw_redis.delete(*self.keys) if self.keys else None
        )
        self.raw_redis.ping()
        self.redis = NamespacedRedisClient(self.raw_redis, self.key_prefix)

        self.raw_mongo = pymongo.MongoClient(
            os.environ["BACKLINKS_MONGO_INTEGRATION_URI"],
            serverSelectionTimeoutMS=5000,
            connectTimeoutMS=5000,
            socketTimeoutMS=5000,
        )
        self.addCleanup(self.raw_mongo.close)
        self.raw_mongo.admin.command("ping")
        self.database_name = f"mifolyo_backlinks_test_{self.test_id}"
        self.addCleanup(self.raw_mongo.drop_database, self.database_name)
        self.mongo = MongoClient.__new__(MongoClient)
        self.mongo.client = self.raw_mongo
        self.mongo.db = self.raw_mongo.get_database(
            self.database_name,
            write_concern=WriteConcern(w="majority", wtimeout=5000),
        )

    def target_url(self, name):
        return f"{self.target_prefix}{name}.example/"

    def backlink_key(self, target):
        key = b"backlinks:" + target.encode("utf-8")
        self.assertTrue(key.startswith(self.key_prefix))
        self.keys.append(key)
        return key

    @staticmethod
    def process(redis_client, mongo_client, batch):
        return processor_main.process_batch(
            redis_client,
            mongo_client,
            batch,
            max_members=256,
            max_batch_url_bytes=512 * 1024,
            max_document_bytes=12 * 1024 * 1024,
        )

    def test_concurrent_add_after_snapshot_remains_pending(self):
        target = self.target_url("integration-target")
        source_a = b"https://integration-source-a.example/"
        source_b = b"https://integration-source-b.example/"
        key = self.backlink_key(target)
        self.raw_redis.sadd(key, source_a)
        _, snapshot = self.redis.scan_backlink_members(key, 0)
        self.assertEqual((source_a,), snapshot)
        self.raw_redis.sadd(key, source_b)
        members = tuple(parse_backlink_member(raw) for raw in snapshot)

        result = self.process(
            self.redis,
            self.mongo,
            BacklinkBatch(key, target, members, sum(map(len, snapshot))),
        )

        self.assertEqual("acked", result.status)
        self.assertEqual({source_b}, self.raw_redis.smembers(key))
        document = self.mongo.db[BACKLINKS_COLLECTION].find_one({"_id": target})
        self.assertEqual([source_a.decode("utf-8")], document["links"])

    def test_multi_page_sscan_eventually_persists_a_concurrent_member(self):
        target = self.target_url("multipage-target")
        sources = {
            f"https://multipage-source.example/{index:03d}-{'x' * 80}".encode("utf-8")
            for index in range(40)
        }
        concurrent = b"https://multipage-concurrent.example/"
        key = self.backlink_key(target)
        self.raw_redis.sadd(key, *sources)
        self.assertEqual(b"hashtable", self.raw_redis.object("ENCODING", key))
        sentinel_key = (
            f"backlinks:https://sentinel-{self.test_id}.example/".encode("utf-8")
        )
        self.keys.append(sentinel_key)
        self.raw_redis.sadd(sentinel_key, b"https://sentinel-source.example/")
        raw_redis = self.raw_redis
        service_redis = self.redis

        class ConcurrentRedis:
            def __init__(self):
                self.added = False
                self.nonzero_cursor_seen = False

            def scan_backlink_keys(self, *args, **kwargs):
                return service_redis.scan_backlink_keys(*args, **kwargs)

            def scan_backlink_members(self, *args, **kwargs):
                result = service_redis.scan_backlink_members(*args, **kwargs)
                self.nonzero_cursor_seen |= result[0] != 0
                if not self.added:
                    self.added = True
                    raw_redis.sadd(key, concurrent)
                return result

            def srem_backlink_members(self, *args, **kwargs):
                return service_redis.srem_backlink_members(*args, **kwargs)

        redis_client = ConcurrentRedis()
        state = processor_main.ProcessorState()
        scan_limits = ProcessingLimits(
            scan_count_hint=10,
            max_scan_calls_per_cycle=1,
            max_targets_per_cycle=1,
            max_pending_target_keys=200,
            max_scan_response_items=100,
            sscan_count_hint=1,
            max_members_per_target_per_cycle=1,
            max_members_per_ack_batch=1,
            max_ack_batch_url_bytes=4096,
            max_mongo_document_bytes=1024 * 1024,
        )

        for _ in range(200):
            processor_main.process_cycle(
                redis_client,
                self.mongo,
                state,
                threading.Event(),
                limits=scan_limits,
            )
            if not self.raw_redis.exists(key):
                break
        else:
            self.fail("bounded SSCAN cycles did not drain the integration set")

        document = self.mongo.db[BACKLINKS_COLLECTION].find_one({"_id": target})
        self.assertTrue(redis_client.nonzero_cursor_seen)
        self.assertEqual(
            {b"https://sentinel-source.example/"},
            self.raw_redis.smembers(sentinel_key),
        )
        self.assertIsNone(
            self.mongo.db[BACKLINKS_COLLECTION].find_one(
                {"_id": sentinel_key.removeprefix(b"backlinks:").decode("utf-8")}
            )
        )
        self.assertEqual(
            {value.decode("utf-8") for value in sources | {concurrent}},
            set(document["links"]),
        )

    def test_crash_after_mongo_ack_replays_without_duplicate_edge(self):
        target = self.target_url("replay-target")
        source = b"https://replay-source.example/"
        key = self.backlink_key(target)
        self.raw_redis.sadd(key, source)
        member = parse_backlink_member(source)
        value = BacklinkBatch(key, target, (member,), len(source))

        self.mongo.add_backlinks(target, [member.url])
        self.assertEqual({source}, self.raw_redis.smembers(key))
        result = self.process(self.redis, self.mongo, value)

        self.assertEqual("acked", result.status)
        self.assertEqual(set(), self.raw_redis.smembers(key))
        document = self.mongo.db[BACKLINKS_COLLECTION].find_one({"_id": target})
        self.assertEqual([member.url], document["links"])

    def test_applied_srem_with_lost_reply_replays_safely(self):
        target = self.target_url("ambiguous-target")
        source = b"https://ambiguous-source.example/"
        key = self.backlink_key(target)
        self.raw_redis.sadd(key, source)
        member = parse_backlink_member(source)
        value = BacklinkBatch(key, target, (member,), len(source))
        raw_redis = self.raw_redis

        class AmbiguousRedisAck:
            def __init__(self):
                self.first = True

            def srem_backlink_members(self, redis_key, members):
                removed = raw_redis.srem(redis_key, *members)
                if self.first:
                    self.first = False
                    raise redis.ConnectionError("reply lost after apply")
                return removed

        redis_ack = AmbiguousRedisAck()
        first = self.process(redis_ack, self.mongo, value)
        second = self.process(redis_ack, self.mongo, value)

        self.assertEqual("retry_later", first.status)
        self.assertEqual("acked", second.status)
        self.assertEqual(0, second.removed_members)
        self.assertEqual(set(), self.raw_redis.smembers(key))
        document = self.mongo.db[BACKLINKS_COLLECTION].find_one({"_id": target})
        self.assertEqual([member.url], document["links"])

    def test_absent_document_race_is_insert_conflict_not_unmeasured_update(self):
        target = "https://insert-race-target.example/"
        source = "https://insert-race-source.example/"
        raw_collection = self.mongo.db[BACKLINKS_COLLECTION]

        class RacingCollection:
            def find_one(self, query):
                document = raw_collection.find_one(query)
                raw_collection.insert_one(
                    {"_id": target, "marker": "concurrent", "payload": "x" * 4096}
                )
                return document

            def insert_one(self, document):
                return raw_collection.insert_one(document)

        class RacingDatabase:
            def __getitem__(self, name):
                self_outer.assertEqual(BACKLINKS_COLLECTION, name)
                return RacingCollection()

        self_outer = self
        client = MongoClient.__new__(MongoClient)
        client.db = RacingDatabase()

        with self.assertRaises(MongoWriteConflict):
            client.add_backlinks(target, [source])
        document = raw_collection.find_one({"_id": target})
        self.assertNotIn("links", document)
        self.assertEqual("concurrent", document["marker"])

    def test_exact_document_cas_detects_concurrent_updates(self):
        target = "https://cas-target.example/"
        sources = (
            "https://cas-source-a.example/",
            "https://cas-source-b.example/",
        )
        collection = self.mongo.db[BACKLINKS_COLLECTION]
        collection.insert_one({"_id": target, "links": []})
        barrier = threading.Barrier(2)
        raw_collection = collection

        class PausingCollection:
            def find_one(self, query):
                document = raw_collection.find_one(query)
                barrier.wait(5)
                return document

            def bulk_write(self, operations, ordered=True):
                return raw_collection.bulk_write(operations, ordered=ordered)

        class PausingDatabase:
            def __getitem__(self, name):
                self_outer.assertEqual(BACKLINKS_COLLECTION, name)
                return PausingCollection()

        self_outer = self
        outcomes = []
        lock = threading.Lock()

        def add(source):
            client = MongoClient.__new__(MongoClient)
            client.db = PausingDatabase()
            try:
                client.add_backlinks(target, [source])
                outcome = (source, "written")
            except MongoWriteConflict:
                outcome = (source, "conflict")
            with lock:
                outcomes.append(outcome)

        threads = [threading.Thread(target=add, args=(source,)) for source in sources]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join(10)
            self.assertFalse(thread.is_alive())

        self.assertEqual(["conflict", "written"], sorted(value for _, value in outcomes))
        conflicted_source = next(source for source, value in outcomes if value == "conflict")
        self.mongo.add_backlinks(target, [conflicted_source])
        document = collection.find_one({"_id": target})
        self.assertEqual(set(sources), set(document["links"]))

    def test_reconciliation_adds_missing_edges_and_keeps_extra_history(self):
        outlinks = self.mongo.db["outlinks"]
        outlinks.insert_many(
            [
                {
                    "_id": "https://reconcile-source-a.example/",
                    "links": ["https://reconcile-target.example/"],
                },
                {
                    "_id": "https://reconcile-source-b.example/",
                    "links": ["https://reconcile-target.example/"],
                },
            ]
        )
        self.mongo.db[BACKLINKS_COLLECTION].insert_one(
            {
                "_id": "https://historical-target.example/",
                "links": ["https://historical-source.example/"],
            }
        )

        before = reconcile_backlinks(self.mongo, apply=False)
        after = reconcile_backlinks(self.mongo, apply=True)

        self.assertEqual(2, before.missing_after)
        self.assertEqual(2, after.added)
        self.assertEqual(0, after.missing_after)
        self.assertEqual(1, after.extra_historical_edges)
        self.assertIsNotNone(
            self.mongo.db[BACKLINKS_COLLECTION].find_one(
                {"_id": "https://historical-target.example/"}
            )
        )


if __name__ == "__main__":
    unittest.main()
