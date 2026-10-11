"""Independent oracle state/frame regressions; Linux namespace checks are separate."""
import copy
import os
import struct
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import contracts as c
import oracle
import oracle_protocol as wire
from oracle_worker import Engine

IDENTITY = {"device": 1, "inode": 2, "uid": 999, "gid": 999, "mode": 0o600, "bytes": 8480}


def initial():
    value = dict.fromkeys(oracle.FIELDS, 0)
    value.update(magic=oracle.MAGIC, transcript=[0] * 1050)
    return value


def held(case="P1.pre.1.1", pre=100):
    return initial() | {"ordinal": 1, "pre_ns": pre}


def final(case="P1.pre.1.1", pre=300):
    value = initial() | {"ordinal": 1, "pre_ns": pre, "post_ns": pre + 1, "finished": 1}
    if case.startswith("P1."):
        value.update(completed=1050, ordinal=1050, last_value=42, transcript=list(range(1, 1051)))
    return value


class Client:
    def __init__(self, case):
        self.transcript, self.connected = wire.ClientTranscript(case), False
        self.after = None
    def pump(self): pass
    def drain(self):
        if self.after: self.after()
    def close(self): self.transcript.closed = True


def fixture(case="P1.pre.1.1"):
    state = [initial()]
    client = Client(case)
    engine = Engine({"case_id": case, "invocation_sha256": c.sha(b"scope")}, client=client,
        admit=lambda: dict(IDENTITY), read=lambda expected: copy.deepcopy(state[0]), clock=lambda: 200)
    return engine, state, client


def call(engine, action, payload=None):
    return c.decode(engine.handle(c.canonical({**engine.binding, "version": 1, "sequence": engine.sequence,
        "previous": engine.previous, "action": action, "payload": payload})))


class OracleWorkerTests(unittest.TestCase):
    def test_initial_held_final_closed_witness_chain(self):
        engine, state, _ = fixture()
        first = call(engine, "INIT")
        self.assertEqual(first["payload"]["witness"], IDENTITY)
        state[0] = held()
        second = call(engine, "HELD", {"observer_sha256": c.sha(b"held")})
        self.assertEqual(second["previous"], c.sha(c.canonical(first)))
        self.assertEqual(second["payload"]["lower_ns"], 100)
        self.assertEqual(second["payload"]["upper_ns"], 200)
        state[0] = final()
        third = call(engine, "FINAL")
        last = call(engine, "CLOSE")
        for row in (first, second, third, last):
            self.assertLessEqual(len(c.canonical(row)), 1024)
            self.assertFalse(row["runtime_evidence"])
            self.assertFalse(row["execution_authorized"])
        self.assertTrue(last["payload"]["client"]["closed"])

    def test_receipt_race_keeps_late_complete_reply(self):
        case = "P3.receipt-race.1"
        engine, state, client = fixture(case)
        call(engine, "INIT"); client.connected = True
        client.transcript.feed(b"C" * 1023, 90)
        state[0] = held(case)
        at_cut = call(engine, "HELD", {"observer_sha256": c.sha(b"cut")})
        self.assertEqual(at_cut["payload"]["client"]["classification_at_cut"], "known_pre_acknowledgment")
        client.after = lambda: (client.transcript.feed(b"C", 250), client.transcript.finish(260))
        closed = call(engine, "CLOSE")["payload"]["client"]
        self.assertTrue(closed["complete_after_cut"])
        self.assertTrue(closed["eof"])
        self.assertEqual(closed["bytes"], 1024)

    def test_received_before_and_during_cut_are_not_equivalent(self):
        for timestamp, expected in ((90, "acknowledged"), (150, "unknown"), (210, "unknown")):
            with self.subTest(timestamp=timestamp):
                engine, state, client = fixture("P3.received.1")
                call(engine, "INIT"); client.connected = True
                if timestamp <= 200:
                    client.transcript.feed(b"C" * 1024, timestamp)
                else:
                    calls = []
                    def delayed():
                        calls.append(True)
                        if len(calls) == 2:
                            client.transcript.feed(b"C" * 1024, timestamp)
                    client.pump = delayed
                state[0] = held("P3.received.1")
                result = call(engine, "HELD", {"observer_sha256": c.sha(b"cut")})["payload"]["client"]
                self.assertEqual(result["classification_at_cut"], expected)
                self.assertEqual(result["complete_after_cut"], timestamp > 200)

    def test_partial_eof_and_unresolved_close_remain_distinct(self):
        for eof in (False, True):
            engine, state, client = fixture("P3.receipt-race.1")
            call(engine, "INIT"); client.connected = True
            client.transcript.feed(b"C" * 17, 90)
            state[0] = held()
            call(engine, "HELD", {"observer_sha256": c.sha(b"cut")})
            if eof: client.transcript.finish(210)
            result = call(engine, "CLOSE")["payload"]["client"]
            self.assertEqual(result["eof"], eof)
            self.assertEqual(result["bytes"], 17)
            self.assertEqual(result["complete_ns"], 0)

    def test_transport_reset_retains_partial_receipt_without_inventing_eof(self):
        engine, state, client = fixture("P3.receipt-race.1")
        call(engine, "INIT"); client.connected = True
        client.transcript.feed(b"C" * 37, 90); state[0] = held()
        call(engine, "HELD", {"observer_sha256": c.sha(b"cut")})
        client.transcript.transport_error = True
        value = call(engine, "CLOSE")["payload"]["client"]
        self.assertEqual(value["bytes"], 37)
        self.assertTrue(value["transport_error"] and value["closed"])
        self.assertFalse(value["eof"])

    def test_backwards_client_clock_cannot_label_a_held_snapshot(self):
        engine, state, client = fixture("P3.received.1")
        call(engine, "INIT"); client.connected = True
        client.transcript.feed(b"C", 201); state[0] = held()
        with self.assertRaisesRegex(c.Invalid, "CLIENT_CLOCK"):
            call(engine, "HELD", {"observer_sha256": c.sha(b"cut")})

    def test_pre_send_marker_rejects_any_early_bytes(self):
        engine, state, client = fixture("P3.pre-send.1")
        call(engine, "INIT"); client.connected = True
        client.transcript.feed(b"C", 90); state[0] = held()
        with self.assertRaises(c.Invalid): call(engine, "HELD", {"observer_sha256": c.sha(b"cut")})

    def test_same_value_effect_cannot_hide_wrong_ordinal_transcript(self):
        engine, state, _ = fixture()
        call(engine, "INIT"); state[0] = held(); state[0]["transcript"][2] = 42
        with self.assertRaises(c.Invalid): call(engine, "HELD", {"observer_sha256": c.sha(b"cut")})

    def test_order_binding_and_chain_replay_refuse(self):
        engine, _, _ = fixture()
        with self.assertRaises(c.Invalid): call(engine, "FINAL")
        raw = c.canonical({**engine.binding, "version": 1, "sequence": 0, "previous": None, "action": "INIT", "payload": None})
        engine.handle(raw)
        with self.assertRaises(c.Invalid): engine.handle(raw)
        for field, value in (("case_id", "P1.pre.2.1"), ("invocation_sha256", c.sha(b"foreign")), ("previous", c.sha(b"wrong"))):
            body = {**engine.binding, "version": 1, "sequence": engine.sequence, "previous": engine.previous, "action": "CLOSE", "payload": None}
            body[field] = value
            with self.assertRaises(c.Invalid): engine.handle(c.canonical(body))

    def test_extra_malformed_and_oversized_client_data_refuse(self):
        for raw in (b"X", b"C" * 1025):
            with self.assertRaises(c.Invalid): wire.ClientTranscript("P3.received.1").feed(raw, 10)
        transcript = wire.ClientTranscript("P3.received.1")
        transcript.feed(b"C" * 1024, 10)
        with self.assertRaises(c.Invalid): transcript.feed(b"C", 11)
        transcript.finish(12)
        with self.assertRaises(c.Invalid): transcript.feed(b"C", 13)

    def test_missing_connection_or_early_eof_cannot_prove_a_cut(self):
        for connected, eof in ((False, False), (True, True)):
            engine, state, client = fixture("P3.received.1")
            call(engine, "INIT"); client.connected = connected
            if eof: client.transcript.finish(50)
            state[0] = held()
            with self.assertRaises(c.Invalid): call(engine, "HELD", {"observer_sha256": c.sha(b"cut")})

    def test_fixed_witness_reader_checks_identity_after_actual_reads(self):
        state = initial()
        raw = struct.pack("<1060Q", *[state[key] for key in oracle.FIELDS], *state["transcript"])
        with tempfile.TemporaryFile() as file:
            file.write(raw); file.flush()
            real_dup = os.dup
            info = SimpleNamespace(st_mode=0o100600, st_nlink=1, st_dev=1, st_ino=2, st_uid=999, st_gid=999, st_size=8480)
            def opening(path, flags):
                self.assertEqual(path, "/witness/state")
                self.assertTrue(flags & os.O_NOFOLLOW and flags & os.O_NONBLOCK)
                return real_dup(file.fileno())
            with patch("oracle.os.open", side_effect=opening), patch("oracle.os.fstat", return_value=info):
                self.assertEqual(oracle.admit_witness(), IDENTITY)
                self.assertEqual(oracle.read_witness(IDENTITY)["magic"], oracle.MAGIC)
            altered = SimpleNamespace(**(vars(info) | {"st_mode": 0o100644}))
            with patch("oracle.os.open", side_effect=opening), patch("oracle.os.fstat", side_effect=[info, altered]):
                with self.assertRaises(c.Invalid): oracle.read_witness(IDENTITY)


if __name__ == "__main__":
    unittest.main()
