import sys
import unittest
from pathlib import Path


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

from feed import (  # noqa: E402
    QUEUE_KEY,
    URLS_KEY,
    CrawlSeed,
    FeedStats,
    enqueue_batch,
    feed_seeds,
    iter_mongo_seeds,
)
from url_identity import identify_url  # noqa: E402


class FakeCursor:
    def __init__(self, documents):
        self.documents = list(documents)
        self.sort_spec = None
        self.limit_value = None

    def sort(self, spec):
        self.sort_spec = spec
        return self

    def limit(self, value):
        self.limit_value = value
        self.documents = self.documents[:value]
        return self

    def __iter__(self):
        return iter(self.documents)


class FakeCollection:
    def __init__(self, documents):
        self.documents = list(documents)
        self.query = None
        self.cursor = None

    def find(self, query):
        self.query = query
        matching = [
            document
            for document in self.documents
            if document.get("enabled") is query.get("enabled")
        ]
        self.cursor = FakeCursor(matching)
        return self.cursor

    def bulk_write(self, *args, **kwargs):
        raise AssertionError("the feeder must never update MongoDB")


class FakePipeline:
    def __init__(self, owner, transaction):
        self.owner = owner
        self.transaction = transaction
        self.commands = []

    def hset(self, key, mapping):
        self.commands.append(("hset", key, dict(mapping)))
        return self

    def zadd(self, key, mapping, **options):
        self.commands.append(("zadd", key, dict(mapping), options))
        return self

    def execute(self):
        self.owner.executed.append(self)
        for command in self.commands:
            if command[0] == "hset":
                self.owner.hashes.setdefault(command[1], {}).update(command[2])
            elif command[0] == "zadd":
                scores = self.owner.sorted_sets.setdefault(command[1], {})
                for member, score in command[2].items():
                    if member not in scores or score < scores[member]:
                        scores[member] = score
        return [len(command[2]) for command in self.commands]


class FakeRedis:
    def __init__(self):
        self.pipelines = []
        self.executed = []
        self.hashes = {}
        self.sorted_sets = {}

    def pipeline(self, transaction):
        pipeline = FakePipeline(self, transaction)
        self.pipelines.append(pipeline)
        return pipeline


def stored_seed(url, priority=1, enabled=True):
    identity = identify_url(url)
    return {
        "_id": identity.url_id,
        "canonical_url": identity.canonical_url,
        "priority": priority,
        "enabled": enabled,
    }


class VersionedFeederTests(unittest.TestCase):
    def test_reader_queries_enabled_records_and_never_writes_mongo(self):
        enabled = stored_seed("https://example.com/a", priority=2)
        disabled = stored_seed("https://example.com/b", enabled=False)
        invalid = {
            "_id": "0" * 64,
            "canonical_url": "https://example.com/c",
            "priority": 1,
            "enabled": True,
        }
        collection = FakeCollection([enabled, disabled, invalid])
        stats = FeedStats()
        seeds = list(iter_mongo_seeds(collection, limit=10, stats=stats))
        self.assertEqual({"enabled": True}, collection.query)
        self.assertEqual(10, collection.cursor.limit_value)
        self.assertEqual(1, len(seeds))
        self.assertEqual(enabled["_id"], seeds[0].id)
        self.assertEqual(2, stats.seen)
        self.assertEqual(1, stats.skipped_invalid)

    def test_hash_and_sorted_set_are_written_in_the_same_transactional_pipeline(self):
        identity = identify_url("https://example.com/path")
        client = FakeRedis()
        count = enqueue_batch(
            client,
            {identity.url_id: (identity.canonical_url, 1.0)},
        )
        self.assertEqual(1, count)
        self.assertEqual(1, len(client.pipelines))
        pipeline = client.pipelines[0]
        self.assertTrue(pipeline.transaction)
        self.assertEqual("hset", pipeline.commands[0][0])
        self.assertEqual(URLS_KEY, pipeline.commands[0][1])
        self.assertEqual("zadd", pipeline.commands[1][0])
        self.assertEqual(QUEUE_KEY, pipeline.commands[1][1])
        self.assertEqual({"lt": True}, pipeline.commands[1][3])
        self.assertEqual(identity.canonical_url, client.hashes[URLS_KEY][identity.url_id])
        self.assertEqual(1.0, client.sorted_sets[QUEUE_KEY][identity.url_id])

    def test_replay_preserves_the_best_lower_score(self):
        identity = identify_url("https://example.com/path")
        client = FakeRedis()
        enqueue_batch(client, {identity.url_id: (identity.canonical_url, 0.0)})
        enqueue_batch(client, {identity.url_id: (identity.canonical_url, 2.0)})
        self.assertEqual(0.0, client.sorted_sets[QUEUE_KEY][identity.url_id])

    def test_feed_batches_url_ids_and_maps_priorities_to_scores(self):
        first = identify_url("https://example.com/first")
        second = identify_url("https://example.com/second")
        seeds = [
            CrawlSeed(first.url_id, first.canonical_url, 3),
            CrawlSeed(second.url_id, second.canonical_url, 1),
        ]
        client = FakeRedis()
        stats = FeedStats()
        feed_seeds(
            seeds,
            client,
            batch_size=2,
            dry_run=False,
            stats=stats,
        )
        self.assertEqual(2, stats.enqueued)
        self.assertEqual(2.0, client.sorted_sets[QUEUE_KEY][first.url_id])
        self.assertEqual(0.0, client.sorted_sets[QUEUE_KEY][second.url_id])
        self.assertEqual(first.canonical_url, client.hashes[URLS_KEY][first.url_id])


if __name__ == "__main__":
    unittest.main()
