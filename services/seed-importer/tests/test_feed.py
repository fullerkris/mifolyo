import math
import sys
import unittest
from pathlib import Path


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

from feed import (  # noqa: E402
    DEPTHS_KEY,
    REMOVE_DISABLED_SCRIPT,
    QUEUE_KEY,
    URLS_KEY,
    CrawlSeed,
    FeedStats,
    enqueue_batch,
    feed_seeds,
    log_reference,
    reconcile_disabled_batch,
    reconcile_disabled_seeds,
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
        self.eval_calls = []

    def pipeline(self, transaction):
        pipeline = FakePipeline(self, transaction)
        self.pipelines.append(pipeline)
        return pipeline

    def eval(self, script, numkeys, *values):
        self.eval_calls.append((script, numkeys, values))
        keys = values[:numkeys]
        arguments = values[numkeys:]
        if script == REMOVE_DISABLED_SCRIPT:
            for index in range(0, len(arguments), 2):
                url_id, canonical_url = arguments[index : index + 2]
                existing_url = self.hashes.get(keys[1], {}).get(url_id)
                if existing_url is not None and existing_url != canonical_url:
                    raise RuntimeError("URL_ID_COLLISION")
            for index in range(0, len(arguments), 2):
                url_id = arguments[index]
                self.sorted_sets.get(keys[0], {}).pop(url_id, None)
                self.hashes.get(keys[1], {}).pop(url_id, None)
                self.hashes.get(keys[2], {}).pop(url_id, None)
            return len(arguments) // 2
        pending = []

        def parse_depth(value):
            text = str(value)
            if text != "0" and (not text or text[0] not in "123456789"):
                raise RuntimeError("INVALID_EXISTING_DEPTH")
            if not text.isdecimal():
                raise RuntimeError("INVALID_EXISTING_DEPTH")
            parsed = int(text)
            if parsed > 9007199254740991:
                raise RuntimeError("INVALID_EXISTING_DEPTH")
            return parsed

        for index in range(0, len(arguments), 4):
            url_id, canonical_url, score, depth = arguments[index : index + 4]
            existing_url = self.hashes.get(keys[1], {}).get(url_id)
            if existing_url is not None and existing_url != canonical_url:
                raise RuntimeError("URL_ID_COLLISION")
            existing_depth = self.hashes.get(keys[2], {}).get(url_id)
            if existing_depth is not None:
                parse_depth(existing_depth)
            parsed_score = float(score)
            if not math.isfinite(parsed_score):
                raise RuntimeError("INVALID_QUEUE_SCORE")
            existing_score = self.sorted_sets.get(keys[0], {}).get(url_id)
            if existing_score is not None and not math.isfinite(existing_score):
                raise RuntimeError("INVALID_EXISTING_SCORE")
            pending.append((url_id, canonical_url, parsed_score, parse_depth(depth)))

        for url_id, canonical_url, score, depth in pending:
            existing_score = self.sorted_sets.get(keys[0], {}).get(url_id)
            self.hashes.setdefault(keys[1], {})[url_id] = canonical_url
            if existing_score is None or score < existing_score:
                self.sorted_sets.setdefault(keys[0], {})[url_id] = score
            existing_depth = self.hashes.get(keys[2], {}).get(url_id)
            if existing_score is None or existing_depth is None or depth < existing_depth:
                self.hashes.setdefault(keys[2], {})[url_id] = depth
        return len(pending)


def stored_seed(url, priority=1, enabled=True):
    identity = identify_url(url)
    return {
        "_id": identity.url_id,
        "canonical_url": identity.canonical_url,
        "priority": priority,
        "enabled": enabled,
    }


class VersionedFeederTests(unittest.TestCase):
    def test_operational_logs_use_references_and_omit_exception_messages(self):
        identity = identify_url("https://example.com/private?token=do-not-log")
        with self.assertLogs("feed", level="INFO") as captured:
            enqueue_batch(
                None,
                {identity.url_id: (identity.canonical_url, 1.0)},
                dry_run=True,
            )
            reconcile_disabled_batch(
                None,
                {identity.url_id: identity.canonical_url},
                dry_run=True,
            )
        output = "\n".join(captured.output)
        self.assertIn(log_reference(identity.canonical_url), output)
        self.assertNotIn(identity.canonical_url, output)
        self.assertNotIn(identity.url_id, output)
        self.assertNotIn("do-not-log", output)

        invalid_id = "raw-record-id-do-not-log"
        invalid = {
            "_id": invalid_id,
            "canonical_url": identity.canonical_url,
            "priority": 1,
            "enabled": True,
        }
        with self.assertLogs("feed", level="WARNING") as captured:
            self.assertEqual([], list(iter_mongo_seeds(FakeCollection([invalid]), 1)))
        output = "\n".join(captured.output)
        self.assertIn(log_reference(invalid_id), output)
        self.assertNotIn(invalid_id, output)
        self.assertNotIn(identity.canonical_url, output)

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

    def test_hash_depth_and_sorted_set_are_written_by_one_atomic_script(self):
        identity = identify_url("https://example.com/path")
        client = FakeRedis()
        count = enqueue_batch(
            client,
            {identity.url_id: (identity.canonical_url, 1.0)},
        )
        self.assertEqual(1, count)
        self.assertEqual(1, len(client.eval_calls))
        _, numkeys, values = client.eval_calls[0]
        self.assertEqual(3, numkeys)
        self.assertEqual((QUEUE_KEY, URLS_KEY, DEPTHS_KEY), values[:3])
        self.assertEqual(identity.canonical_url, client.hashes[URLS_KEY][identity.url_id])
        self.assertEqual(0, client.hashes[DEPTHS_KEY][identity.url_id])
        self.assertEqual(1.0, client.sorted_sets[QUEUE_KEY][identity.url_id])

    def test_replay_preserves_the_best_lower_score(self):
        identity = identify_url("https://example.com/path")
        client = FakeRedis()
        enqueue_batch(client, {identity.url_id: (identity.canonical_url, 0.0)})
        enqueue_batch(client, {identity.url_id: (identity.canonical_url, 2.0)})
        self.assertEqual(0.0, client.sorted_sets[QUEUE_KEY][identity.url_id])

    def test_collision_preflight_does_not_partially_write_batch(self):
        first = identify_url("https://example.com/first")
        second = identify_url("https://example.com/second")
        client = FakeRedis()
        client.hashes[URLS_KEY] = {second.url_id: "https://example.com/conflict"}

        with self.assertRaisesRegex(RuntimeError, "URL_ID_COLLISION"):
            enqueue_batch(
                client,
                {
                    first.url_id: (first.canonical_url, 0.0),
                    second.url_id: (second.canonical_url, 0.0),
                },
            )

        self.assertNotIn(first.url_id, client.hashes[URLS_KEY])
        self.assertNotIn(first.url_id, client.sorted_sets.get(QUEUE_KEY, {}))
        self.assertNotIn(first.url_id, client.hashes.get(DEPTHS_KEY, {}))

    def test_replay_rejects_noncanonical_or_negative_stored_depth(self):
        identity = identify_url("https://example.com/corrupt-depth")
        for stored_depth in ("-1", "00", "+1", "1.0", "9007199254740992"):
            with self.subTest(stored_depth=stored_depth):
                client = FakeRedis()
                client.hashes[URLS_KEY] = {
                    identity.url_id: identity.canonical_url
                }
                client.hashes[DEPTHS_KEY] = {identity.url_id: stored_depth}
                client.sorted_sets[QUEUE_KEY] = {identity.url_id: 2.0}

                with self.assertRaisesRegex(
                    RuntimeError, "INVALID_EXISTING_DEPTH"
                ):
                    enqueue_batch(
                        client,
                        {identity.url_id: (identity.canonical_url, 0.0)},
                    )

                self.assertEqual(
                    stored_depth, client.hashes[DEPTHS_KEY][identity.url_id]
                )
                self.assertEqual(2.0, client.sorted_sets[QUEUE_KEY][identity.url_id])

    def test_replay_rejects_non_finite_existing_score(self):
        identity = identify_url("https://example.com/corrupt-score")
        client = FakeRedis()
        client.hashes[URLS_KEY] = {identity.url_id: identity.canonical_url}
        client.hashes[DEPTHS_KEY] = {identity.url_id: 0}
        client.sorted_sets[QUEUE_KEY] = {identity.url_id: math.inf}

        with self.assertRaisesRegex(RuntimeError, "INVALID_EXISTING_SCORE"):
            enqueue_batch(client, {identity.url_id: (identity.canonical_url, 0.0)})

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

    def test_disabled_seed_reconciliation_removes_stale_queue_state(self):
        enabled = identify_url("https://example.com/enabled")
        disabled = identify_url("https://example.com/disabled")
        client = FakeRedis()
        enqueue_batch(
            client,
            {
                enabled.url_id: (enabled.canonical_url, 0.0),
                disabled.url_id: (disabled.canonical_url, 1.0),
            },
        )
        stats = FeedStats()

        reconcile_disabled_seeds(
            [CrawlSeed(disabled.url_id, disabled.canonical_url, 2)],
            client,
            batch_size=10,
            dry_run=False,
            stats=stats,
        )

        self.assertEqual(1, stats.disabled_reconciled)
        self.assertIn(enabled.url_id, client.sorted_sets[QUEUE_KEY])
        self.assertNotIn(disabled.url_id, client.sorted_sets[QUEUE_KEY])
        self.assertNotIn(disabled.url_id, client.hashes[URLS_KEY])
        self.assertNotIn(disabled.url_id, client.hashes[DEPTHS_KEY])


if __name__ == "__main__":
    unittest.main()
