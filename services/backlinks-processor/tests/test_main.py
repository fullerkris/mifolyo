import sys
import threading
import unittest
from collections import deque
from pathlib import Path
from unittest.mock import MagicMock, patch

from pymongo.errors import ConnectionFailure, WTimeoutError
from redis.exceptions import ConnectionError as RedisConnectionError


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

import main as processor_main  # noqa: E402
from config import MAX_CANONICAL_URL_BYTES, ProcessingLimits  # noqa: E402
from data.mongo_client import (  # noqa: E402
    BacklinkDocumentRejected,
    MongoClient,
    MongoWriteConflict,
    MongoWriteNotAcknowledged,
)
from data.redis_client import RedisScanLimitExceeded  # noqa: E402
from models.backlinks import BacklinkBatch, BacklinkMember  # noqa: E402


TARGET = "https://target.example/"
SOURCE_A = "https://source-a.example/"
SOURCE_B = "https://source-b.example/"
KEY = b"backlinks:" + TARGET.encode("utf-8")


def limits(**overrides):
    values = {
        "scan_count_hint": 2,
        "max_scan_calls_per_cycle": 1,
        "max_targets_per_cycle": 2,
        "max_pending_target_keys": 8,
        "max_scan_response_items": 4,
        "sscan_count_hint": 4,
        "max_members_per_target_per_cycle": 4,
        "max_members_per_ack_batch": 4,
        "max_ack_batch_url_bytes": 4096,
        "max_mongo_document_bytes": 1024 * 1024,
    }
    values.update(overrides)
    return ProcessingLimits(**values)


def batch(source=SOURCE_A):
    raw = source.encode("utf-8")
    return BacklinkBatch(
        redis_key=KEY,
        target_url=TARGET,
        members=(BacklinkMember(raw=raw, url=source),),
        encoded_url_bytes=len(raw),
    )


def process(redis_client, mongo_client, value=None):
    return processor_main.process_batch(
        redis_client,
        mongo_client,
        value or batch(),
        max_members=4,
        max_batch_url_bytes=4096,
        max_document_bytes=1024 * 1024,
    )


class BatchProcessingTests(unittest.TestCase):
    def test_mongo_ack_precedes_exact_raw_redis_ack(self):
        redis_client = MagicMock()
        mongo_client = MagicMock()
        events = []
        mongo_client.add_backlinks.side_effect = lambda *args, **kwargs: events.append(
            "mongo"
        )
        redis_client.srem_backlink_members.side_effect = (
            lambda key, members: events.append("redis") or 1
        )

        result = process(redis_client, mongo_client)

        self.assertEqual("acked", result.status)
        self.assertEqual(["mongo", "redis"], events)
        redis_client.srem_backlink_members.assert_called_once_with(
            KEY, (SOURCE_A.encode("utf-8"),)
        )

    def test_mongo_failure_removes_zero_redis_members(self):
        for failure in (
            ConnectionFailure("offline"),
            WTimeoutError("write concern timed out"),
            MongoWriteConflict("conflict"),
            MongoWriteNotAcknowledged("unknown"),
            BacklinkDocumentRejected("mongo_document_ceiling_exceeded"),
        ):
            with self.subTest(failure=type(failure).__name__):
                redis_client = MagicMock()
                mongo_client = MagicMock()
                mongo_client.add_backlinks.side_effect = failure

                result = process(redis_client, mongo_client)

                self.assertNotEqual("acked", result.status)
                redis_client.srem_backlink_members.assert_not_called()

    def test_ambiguous_redis_ack_is_retryable_after_mongo(self):
        redis_client = MagicMock()
        mongo_client = MagicMock()
        redis_client.srem_backlink_members.side_effect = RedisConnectionError(
            "response lost"
        )

        result = process(redis_client, mongo_client)

        self.assertEqual("retry_later", result.status)
        self.assertEqual("redis_ack_unavailable", result.reason)
        mongo_client.add_backlinks.assert_called_once()

    def test_applied_mongo_write_with_lost_reply_replays_idempotently(self):
        redis_client = MagicMock()
        redis_client.srem_backlink_members.return_value = 1
        mongo_client = MongoClient.__new__(MongoClient)
        mongo_client.db = MagicMock()
        collection = mongo_client.db["backlinks"]
        document = {"_id": TARGET, "links": []}
        collection.find_one.side_effect = lambda *args: {
            "_id": TARGET,
            "links": list(document["links"]),
        }

        def apply_write(operations, **kwargs):
            operation = operations[0]
            for source in operation._doc["$addToSet"]["links"]["$each"]:
                if source not in document["links"]:
                    document["links"].append(source)
            if collection.bulk_write.call_count == 1:
                raise ConnectionFailure("applied but reply lost")
            return MagicMock(acknowledged=True, matched_count=1, upserted_count=0)

        collection.bulk_write.side_effect = apply_write

        self.assertEqual("retry_later", process(redis_client, mongo_client).status)
        self.assertEqual([SOURCE_A], document["links"])
        redis_client.srem_backlink_members.assert_not_called()
        self.assertEqual("acked", process(redis_client, mongo_client).status)
        self.assertEqual([SOURCE_A], document["links"])
        redis_client.srem_backlink_members.assert_called_once_with(
            KEY, (SOURCE_A.encode("utf-8"),)
        )

    def test_concurrent_addition_is_not_part_of_exact_ack(self):
        pending = {SOURCE_A.encode("utf-8")}
        redis_client = MagicMock()
        mongo_client = MagicMock()

        def add_concurrent_member(*args, **kwargs):
            pending.add(SOURCE_B.encode("utf-8"))

        def exact_remove(key, members):
            self.assertEqual(KEY, key)
            removed = 0
            for member in members:
                if member in pending:
                    pending.remove(member)
                    removed += 1
            return removed

        mongo_client.add_backlinks.side_effect = add_concurrent_member
        redis_client.srem_backlink_members.side_effect = exact_remove

        self.assertEqual("acked", process(redis_client, mongo_client).status)
        self.assertEqual({SOURCE_B.encode("utf-8")}, pending)


class CycleProcessingTests(unittest.TestCase):
    def configured_clients(self, members=(SOURCE_A, SOURCE_B)):
        redis_client = MagicMock()
        redis_client.scan_backlink_keys.return_value = (0, (KEY,))
        redis_client.scan_backlink_members.return_value = (
            0,
            tuple(value.encode("utf-8") for value in members),
        )
        redis_client.srem_backlink_members.side_effect = lambda key, values: len(
            values
        )
        return redis_client, MagicMock()

    def test_pending_byte_budget_bounds_retries_and_successful_partial_pages(self):
        cycle_limits = limits(
            max_scan_response_items=64,
            max_pending_target_keys=128,
            max_targets_per_cycle=8,
            max_members_per_target_per_cycle=16,
            max_members_per_ack_batch=16,
            max_ack_batch_url_bytes=32_000,
            max_pending_member_bytes=256 * 1024,
        )
        raw_members = tuple(
            f"https://source.example/{index}/".encode().ljust(2000, b"x")
            for index in range(64)
        )
        for failure in ("mongo", "redis_ack", None):
            with self.subTest(failure=failure):
                source_work = {
                    f"backlinks:https://target-{index}.example/".encode(): set(raw_members)
                    for index in range(40)
                }
                redis_client, mongo_client = self.configured_clients()
                redis_client.scan_backlink_keys.side_effect = (
                    lambda *args, **kwargs: (0, tuple(source_work))
                )

                def scan_members(key, *args, **kwargs):
                    page = tuple(source_work[key])
                    self.assertLessEqual(
                        sum(len(raw) for target in state.targets.values()
                            for raw in target.pending_members)
                        + sum(len(raw) for raw in page),
                        cycle_limits.max_pending_member_bytes,
                    )
                    return 0, page

                redis_client.scan_backlink_members.side_effect = scan_members

                def acknowledge(key, members):
                    pending = source_work[key]
                    before = len(pending)
                    pending.difference_update(members)
                    removed = before - len(pending)
                    if not pending:
                        del source_work[key]
                    return removed

                redis_client.srem_backlink_members.side_effect = acknowledge
                if failure == "mongo":
                    mongo_client.add_backlinks.side_effect = ConnectionFailure("offline")
                elif failure == "redis_ack":
                    redis_client.srem_backlink_members.side_effect = RedisConnectionError(
                        "offline"
                    )
                state = processor_main.ProcessorState()
                with patch.object(processor_main.logger, "warning"):
                    for _ in range(20):
                        processor_main.process_cycle(
                            redis_client, mongo_client, state, threading.Event(),
                            limits=cycle_limits,
                        )
                        self.assertLessEqual(
                            sum(len(raw) for target in state.targets.values()
                                for raw in target.pending_members),
                            cycle_limits.max_pending_member_bytes,
                        )
                    if failure:
                        self.assertEqual(40, len(source_work))
                        self.assertTrue(all(
                            values == set(raw_members) for values in source_work.values()
                        ))
                        for target in state.targets.values():
                            if target.pending_members:
                                self.assertEqual(64, len(target.pending_members))
                                self.assertEqual(
                                    set(raw_members), set(target.pending_members)
                                )
                    if failure == "mongo":
                        redis_client.srem_backlink_members.assert_not_called()
                    mongo_client.add_backlinks.side_effect = None
                    redis_client.srem_backlink_members.side_effect = acknowledge
                    # Allow each batch a full sweep of the bounded target queue.
                    for _ in range(40 * 4 * 5):
                        if not source_work:
                            break
                        processor_main.process_cycle(
                            redis_client, mongo_client, state, threading.Event(),
                            limits=cycle_limits,
                        )
                        self.assertLessEqual(
                            sum(len(raw) for target in state.targets.values()
                                for raw in target.pending_members),
                            cycle_limits.max_pending_member_bytes,
                        )
                self.assertEqual(0, len(source_work))
                self.assertFalse(state.targets)
                self.assertFalse(state.pending_keys)
                self.assertIsNone(state.member_scan_waiter)

    def test_full_page_reservation_preserves_cursor_and_prioritizes_waiter(self):
        buffered_key = b"backlinks:https://buffered.example/"
        later_key = b"backlinks:https://later.example/"
        page = tuple(
            f"https://source.example/{index}/".encode().ljust(MAX_CANONICAL_URL_BYTES, b"x")
            for index in range(4)
        )
        state = processor_main.ProcessorState()
        state.targets[buffered_key] = processor_main.TargetScanState(
            scan_complete=True, pending_members=deque(page[:2])
        )
        state.targets[KEY] = processor_main.TargetScanState(cursor=17)
        for key in (KEY, buffered_key, later_key):
            processor_main._queue_key(state, key)
        redis_client, mongo_client = self.configured_clients()
        redis_client.scan_backlink_keys.return_value = (0, ())
        redis_client.scan_backlink_members.side_effect = (
            lambda key, cursor, **kwargs: (
                (23, page) if key == KEY and cursor == 17 else (0, (SOURCE_A.encode(),))
            )
        )
        cycle_limits = limits(
            max_targets_per_cycle=3,
            max_members_per_ack_batch=1,
            max_pending_member_bytes=4 * MAX_CANONICAL_URL_BYTES,
        )
        for _ in range(2):
            result = processor_main.process_cycle(
                redis_client, mongo_client, state, threading.Event(), limits=cycle_limits
            )
            self.assertIn("pending_member_byte_limit", result.retries)
            self.assertEqual(17, state.targets[KEY].cursor)
            redis_client.scan_backlink_members.assert_not_called()
        for _ in range(8):
            processor_main.process_cycle(
                redis_client, mongo_client, state, threading.Event(), limits=cycle_limits
            )
        self.assertEqual(
            [(KEY, 17), (later_key, 0), (KEY, 23)],
            [call.args for call in redis_client.scan_backlink_members.call_args_list],
        )
        self.assertEqual(
            [*page, SOURCE_A.encode()],
            [
                raw for call in redis_client.srem_backlink_members.call_args_list
                if call.args[0] == KEY for raw in call.args[1]
            ],
        )
        self.assertFalse(state.targets)

    def test_full_budget_uses_interruptible_backoff(self):
        page = (SOURCE_A.encode().ljust(MAX_CANONICAL_URL_BYTES, b"x"),)
        state = processor_main.ProcessorState()
        processor_main._queue_key(state, KEY)
        state.targets[b"backlinks:https://buffered.example/"] = processor_main.TargetScanState(
            pending_members=deque(page)
        )
        processor_main._queue_key(state, b"backlinks:https://buffered.example/")
        redis_client, mongo_client = self.configured_clients()
        redis_client.scan_backlink_keys.return_value = (0, ())
        stop_event = threading.Event()
        waits = []

        def stop_on_wait(delay):
            waits.append(delay)
            stop_event.set()
            return True

        with patch.object(processor_main, "ProcessorState", return_value=state):
            processor_main.run_processor(
                redis_client, mongo_client, stop_event, wait=stop_on_wait,
                limits=limits(
                    max_targets_per_cycle=1, max_scan_response_items=1,
                    max_pending_member_bytes=MAX_CANONICAL_URL_BYTES,
                ),
            )
        self.assertEqual([processor_main.RETRY_BACKOFF_SECONDS], waits)
        redis_client.scan_backlink_members.assert_not_called()
        redis_client.srem_backlink_members.assert_not_called()

    def test_batch_count_and_byte_splitting_release_invalid_members(self):
        for bounds in (
            {"max_members_per_ack_batch": 1},
            {"max_ack_batch_url_bytes": len(SOURCE_A)},
        ):
            with self.subTest(bounds=bounds):
                redis_client, mongo_client = self.configured_clients()
                redis_client.scan_backlink_members.return_value = (
                    0, (SOURCE_A.encode(), b"\xff", SOURCE_B.encode())
                )
                state = processor_main.ProcessorState()
                with self.assertLogs(processor_main.logger, level="WARNING"):
                    first = processor_main.process_cycle(
                        redis_client, mongo_client, state, threading.Event(),
                        limits=limits(**bounds),
                    )
                self.assertEqual({"member_invalid_utf8": 1}, first.rejections)
                self.assertEqual(
                    [SOURCE_B.encode()], list(state.targets[KEY].pending_members)
                )
                second = processor_main.process_cycle(
                    redis_client, mongo_client, state, threading.Event(),
                    limits=limits(**bounds),
                )
                self.assertEqual((1, 1), (first.members_acked, second.members_acked))
                self.assertEqual(
                    [(KEY, (SOURCE_A.encode(),)), (KEY, (SOURCE_B.encode(),))],
                    [call.args for call in redis_client.srem_backlink_members.call_args_list],
                )
                redis_client.scan_backlink_members.assert_called_once()
                self.assertFalse(state.targets)

    def test_pending_byte_budget_must_hold_a_full_valid_scan_page(self):
        self.assertEqual(8 * 1024 * 1024, ProcessingLimits().max_pending_member_bytes)
        with self.assertRaisesRegex(ValueError, "full member scan response"):
            limits(max_pending_member_bytes=4 * MAX_CANONICAL_URL_BYTES - 1)

    def test_rejected_member_or_batch_releases_budget_for_next_target(self):
        buffered_key = b"backlinks:https://buffered.example/"
        for raw, failure in (
            (b"x" * MAX_CANONICAL_URL_BYTES, None),
            (
                SOURCE_B.encode().ljust(MAX_CANONICAL_URL_BYTES, b"x"),
                BacklinkDocumentRejected("mongo_document_ceiling_exceeded"),
            ),
        ):
            with self.subTest(failure=failure):
                state = processor_main.ProcessorState()
                state.targets[buffered_key] = processor_main.TargetScanState(
                    scan_complete=True, pending_members=deque([raw])
                )
                for key in (buffered_key, KEY):
                    processor_main._queue_key(state, key)
                redis_client, mongo_client = self.configured_clients((SOURCE_A,))
                redis_client.scan_backlink_keys.return_value = (0, ())
                if failure:
                    mongo_client.add_backlinks.side_effect = [failure, None]
                with self.assertLogs(processor_main.logger, level="WARNING"):
                    result = processor_main.process_cycle(
                        redis_client, mongo_client, state, threading.Event(),
                        limits=limits(
                            max_scan_response_items=1,
                            max_pending_member_bytes=MAX_CANONICAL_URL_BYTES,
                        ),
                    )
                self.assertEqual(1, result.members_acked)
                self.assertTrue(result.rejections)
                redis_client.srem_backlink_members.assert_called_once_with(
                    KEY, (SOURCE_A.encode(),)
                )
                self.assertFalse(state.targets)

    def test_target_is_processed_at_most_once_per_cycle(self):
        redis_client, mongo_client = self.configured_clients()
        state = processor_main.ProcessorState()

        result = processor_main.process_cycle(
            redis_client,
            mongo_client,
            state,
            threading.Event(),
            limits=limits(
                max_targets_per_cycle=4,
                max_members_per_target_per_cycle=1,
                max_members_per_ack_batch=1,
            ),
        )

        self.assertEqual(1, result.targets_visited)
        self.assertEqual(1, result.members_acked)
        self.assertEqual(1, mongo_client.add_backlinks.call_count)
        self.assertEqual(deque([KEY]), state.pending_keys)

    def test_retry_restores_the_exact_snapshot(self):
        redis_client, mongo_client = self.configured_clients()
        mongo_client.add_backlinks.side_effect = MongoWriteConflict("race")
        state = processor_main.ProcessorState()

        result = processor_main.process_cycle(
            redis_client,
            mongo_client,
            state,
            threading.Event(),
            limits=limits(),
        )

        self.assertEqual({"mongo_write_conflict": 1}, result.retries)
        self.assertEqual(
            [SOURCE_A.encode("utf-8"), SOURCE_B.encode("utf-8")],
            list(state.targets[KEY].pending_members),
        )
        self.assertEqual(deque([KEY]), state.pending_keys)
        redis_client.srem_backlink_members.assert_not_called()

    def test_shutdown_after_snapshot_restores_members_without_writing(self):
        redis_client, mongo_client = self.configured_clients((SOURCE_A,))
        stop_event = threading.Event()

        def scan_and_stop(*args, **kwargs):
            stop_event.set()
            return 0, (SOURCE_A.encode("utf-8"),)

        redis_client.scan_backlink_members.side_effect = scan_and_stop
        state = processor_main.ProcessorState()
        result = processor_main.process_cycle(
            redis_client,
            mongo_client,
            state,
            stop_event,
            limits=limits(),
        )

        self.assertTrue(result.interrupted)
        self.assertEqual([SOURCE_A.encode("utf-8")], list(state.targets[KEY].pending_members))
        mongo_client.add_backlinks.assert_not_called()
        redis_client.srem_backlink_members.assert_not_called()

    def test_shutdown_finishes_started_ack_and_keeps_the_remaining_page(self):
        redis_client, mongo_client = self.configured_clients()
        stop_event = threading.Event()
        mongo_client.add_backlinks.side_effect = lambda *args, **kwargs: stop_event.set()
        state = processor_main.ProcessorState()
        cycle_limits = limits(max_members_per_ack_batch=1)

        result = processor_main.process_cycle(
            redis_client, mongo_client, state, stop_event, limits=cycle_limits
        )

        self.assertTrue(result.interrupted)
        self.assertEqual(1, result.members_acked)
        redis_client.srem_backlink_members.assert_called_once_with(
            KEY, (SOURCE_A.encode(),)
        )
        self.assertEqual([SOURCE_B.encode()], list(state.targets[KEY].pending_members))
        stop_event.clear()
        mongo_client.add_backlinks.side_effect = None
        processor_main.process_cycle(
            redis_client, mongo_client, state, stop_event, limits=cycle_limits
        )
        redis_client.srem_backlink_members.assert_called_with(KEY, (SOURCE_B.encode(),))
        redis_client.scan_backlink_members.assert_called_once()
        self.assertFalse(state.targets)

    def test_outer_scan_advances_while_a_target_remains_active(self):
        key_b = b"backlinks:https://target-b.example/"
        redis_client, mongo_client = self.configured_clients()
        redis_client.scan_backlink_keys.side_effect = [
            (9, (KEY,)),
            (0, (key_b,)),
        ]
        state = processor_main.ProcessorState()
        cycle_limits = limits(
            max_targets_per_cycle=1,
            max_members_per_target_per_cycle=1,
            max_members_per_ack_batch=1,
        )

        processor_main.process_cycle(
            redis_client,
            mongo_client,
            state,
            threading.Event(),
            limits=cycle_limits,
        )
        processor_main.process_cycle(
            redis_client,
            mongo_client,
            state,
            threading.Event(),
            limits=cycle_limits,
        )

        self.assertEqual(2, redis_client.scan_backlink_keys.call_count)
        self.assertIn(key_b, state.pending_keys)

    def test_transient_scan_failure_uses_interruptible_backoff(self):
        redis_client = MagicMock()
        redis_client.scan_backlink_keys.side_effect = RedisConnectionError("offline")
        stop_event = threading.Event()
        waits = []

        def stop_on_wait(delay):
            waits.append(delay)
            stop_event.set()
            return True

        self.assertEqual(
            0,
            processor_main.run_processor(
                redis_client,
                MagicMock(),
                stop_event,
                wait=stop_on_wait,
                limits=limits(),
            ),
        )
        self.assertEqual([processor_main.RETRY_BACKOFF_SECONDS], waits)

    def test_member_scan_failure_requeues_target_without_advancing_cursor(self):
        redis_client, mongo_client = self.configured_clients((SOURCE_A,))
        redis_client.scan_backlink_members.side_effect = RedisScanLimitExceeded(
            "member_scan_response_limit_exceeded"
        )
        state = processor_main.ProcessorState()

        with self.assertRaises(RedisScanLimitExceeded):
            processor_main.process_cycle(
                redis_client,
                mongo_client,
                state,
                threading.Event(),
                limits=limits(),
            )

        self.assertEqual(deque([KEY]), state.pending_keys)
        self.assertEqual(0, state.targets[KEY].cursor)
        mongo_client.add_backlinks.assert_not_called()

    def test_member_scan_byte_overflow_does_not_advance_or_ack(self):
        redis_client, mongo_client = self.configured_clients()
        redis_client.scan_backlink_members.return_value = (
            29, (b"x" * (4 * MAX_CANONICAL_URL_BYTES + 1),)
        )
        state = processor_main.ProcessorState()
        state.targets[KEY] = processor_main.TargetScanState(cursor=17)
        with self.assertRaises(RedisScanLimitExceeded):
            processor_main.process_cycle(
                redis_client, mongo_client, state, threading.Event(), limits=limits()
            )
        self.assertEqual(17, state.targets[KEY].cursor)
        self.assertFalse(state.targets[KEY].pending_members)
        self.assertEqual(deque([KEY]), state.pending_keys)
        mongo_client.add_backlinks.assert_not_called()
        redis_client.srem_backlink_members.assert_not_called()
        redis_client.scan_backlink_members.return_value = (0, (SOURCE_A.encode(),))
        processor_main.process_cycle(
            redis_client, mongo_client, state, threading.Event(), limits=limits()
        )
        self.assertEqual((KEY, 17), redis_client.scan_backlink_members.call_args.args)
        self.assertFalse(state.targets)

    def test_key_scan_response_limit_still_processes_queued_work(self):
        redis_client, mongo_client = self.configured_clients((SOURCE_A,))
        redis_client.scan_backlink_keys.side_effect = RedisScanLimitExceeded(
            "key_scan_response_limit_exceeded"
        )
        state = processor_main.ProcessorState()
        processor_main._queue_key(state, KEY)

        result = processor_main.process_cycle(
            redis_client,
            mongo_client,
            state,
            threading.Event(),
            limits=limits(),
        )

        self.assertEqual(
            {"redis_key_scan_response_limit_exceeded": 1}, result.retries
        )
        self.assertEqual(1, result.members_acked)

    def test_failure_log_does_not_include_url_or_redis_key(self):
        redis_client, mongo_client = self.configured_clients((SOURCE_A,))
        mongo_client.add_backlinks.side_effect = ConnectionFailure(TARGET)

        with self.assertLogs(processor_main.logger, level="WARNING") as captured:
            processor_main.process_cycle(
                redis_client,
                mongo_client,
                processor_main.ProcessorState(),
                threading.Event(),
                limits=limits(),
            )

        output = "\n".join(captured.output)
        self.assertNotIn(TARGET, output)
        self.assertNotIn(KEY.decode("utf-8"), output)
        self.assertIn("ref=", output)

    def test_insecure_datastore_opt_in_requires_exact_true(self):
        for value, expected in (("true", True), ("TRUE", False), ("1", False), ("", False)):
            with self.subTest(value=value), patch.dict(
                processor_main.os.environ,
                {"ALLOW_INSECURE_DATASTORES": value},
                clear=True,
            ):
                self.assertEqual(expected, processor_main.allow_insecure_datastores())


if __name__ == "__main__":
    unittest.main()
