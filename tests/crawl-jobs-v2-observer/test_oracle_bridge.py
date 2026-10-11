"""Real oracle protocol/benign pipes with synthetic Docker and Linux observations."""
import copy
import json
import os
from pathlib import Path
import socket
import sys
import time
import unittest
from unittest.mock import patch

import contracts as c
from controller import Coordinator
import oracle_protocol as wire
from oracle_worker import FrameClient
from profiles import profile
from streams import Deadline, ProcessStream
from test_admission_dispatch import ACTOR, PipePair
from test_oracle_worker import IDENTITY, initial, held, final
from test_runtime_admission import context, frames, preparation

ORACLE = r'''
import json, sys, time
from pathlib import Path
sys.path.insert(0, sys.argv[1])
config = json.loads(Path(sys.argv[2]).read_text())
state = Path(sys.argv[3])
import oracle_worker as worker
base = worker.Engine
class SimulatedLinux:
    def monotonic_ns(self):
        return 2000000 + time.monotonic_ns() - config["clock_origin"]
    def actor_frames(self, case_id, invocation):
        assert case_id == config["case_id"] and invocation == config["invocation_sha256"]
        return [raw.encode() for raw in config["frames"]]
class SimulatedWitness(base):
    def __init__(self, binding, **kwargs):
        super().__init__(binding, admit=lambda: config["identity"], read=lambda _: json.loads(state.read_text()), **kwargs)
worker.LinuxReader = SimulatedLinux
worker.Engine = SimulatedWitness
if config.get("wrong_source"):
    worker.wire.source_digest = lambda: "f" * 64
sys.argv = ["/oracle/worker.py"]
raise SystemExit(worker.main())
'''


class OraclePair(PipePair):
    def __init__(self, wrong_source=False):
        super().__init__(8, 8)
        self.preparation = preparation()
        self.clock_origin = time.monotonic_ns()
        self.preparation["artifacts"]["oracle_worker_source"] = wire.source_digest()
        self.deadlines["oracle"] = Deadline(8)
        self.state_path = self.root / "witness.json"
        self.set_state(initial())
        changed = ACTOR.replace('elif cfg["role"] == "target" and byte == b"G":\n        os.write(1, b\'{"phase":"FINISHED","version":1}\\n\')',
            'elif cfg["role"] == "target" and byte == b"G":\n        (root / "started").write_text("yes")')
        changed = changed.replace('    trigger = root / "fault"',
            '    if (root / "finish").exists():\n        os.write(1, b\'{"phase":"FINISHED","version":1}\\n\')\n        break\n    trigger = root / "fault"')
        changed = changed.replace('    else:\n        raise AssertionError("fixture input")',
            '    elif cfg["role"] == "observer" and byte == b"R":\n        os.write(1, b\'{"phase":"CONTINUATION_DISPATCHED","version":1}\\n\')\n    else:\n        raise AssertionError("fixture input")')
        assert changed != ACTOR
        self.script.write_text(changed)
        original = self.backend.stream_factory
        def factory(argv, bound, budget, deadline, **kwargs):
            if bound["role"] != "oracle":
                return original(argv, bound, budget, deadline, **kwargs)
            path = self.root / "oracle-config.json"
            path.write_text(json.dumps(bound | {"frames": [raw.decode() for raw in frames("oracle", bound=bound)],
                "identity": IDENTITY, "wrong_source": wrong_source, "clock_origin": self.clock_origin}))
            value = self.fake.containers[bound["container_id"]]
            value["State"].update(Running=True, Pid=103, Status="running", StartedAt="2026-10-09T00:00:02Z")
            value["RestartCount"] = 0
            stream = ProcessStream([sys.executable, "-I", "-B", "-c", ORACLE, str(Path(__file__).resolve().parent),
                str(path), str(self.state_path)], bound, budget, deadline, **kwargs)
            self.streams["oracle"] = stream
            return stream
        self.backend.stream_factory = factory

    def set_state(self, value):
        pending = self.state_path.with_suffix(".pending")
        pending.write_text(json.dumps(value))
        pending.replace(self.state_path)

    def linux_ns(self):
        return 2000000 + time.monotonic_ns() - self.clock_origin

    def collect(self, role):
        return self.backend.collect_native_admission(self.streams[role], self.registry.entries[role]["reference"],
            self.registry.snapshot(), self.preparation, context(), self.deadlines[role])

    def open_oracle(self):
        return self.backend.open_oracle(self.registry.entries["oracle"]["reference"], self.registry.snapshot(),
            self.preparation, context(), self.budget, self.deadlines["oracle"])

    def stopped(self, role):
        value = self.fake.containers[self.registry.entries[role]["reference"]["id"]]
        value["State"].update(Running=False, Pid=0, Status="exited", ExitCode=0, OOMKilled=False)


class OracleBridgeTests(unittest.TestCase):
    def test_no_oracle_blocks_target_without_writing_g(self):
        with PipePair() as pair:
            target, observer = pair.pair()
            observer.arm_observer(); observer.receive("ARMED")
            writes, original = [], os.write
            def spy(fd, data):
                if fd == target._process.stdin.fileno(): writes.append(data)
                return original(fd, data)
            with patch("streams.os.write", side_effect=spy), self.assertRaisesRegex(c.Invalid, "ORACLE_NOT_READY"):
                target.start_target()
            self.assertEqual(writes, [])

    def test_owned_oracle_drives_held_coordinator_resume_and_final_closure(self):
        with OraclePair() as pair:
            target, observer = pair.pair()
            session = pair.open_oracle()
            self.assertEqual(session.phase, "INITIALIZED")
            observer.arm_observer(); observer.receive("ARMED")
            count = len(pair.events)
            target.start_target()
            self.assertEqual(len(pair.events), count)
            pair.set_state(held(pre=pair.linux_ns() - 1000))
            pair.fault("observer", "held"); observer.receive("HELD")
            model = Coordinator(pair.registry.scope["case_id"], pair.backend.scope_sha256)
            for kind in ("ADMITTED", "TARGET_READY", "OBSERVER_ARMED", "TARGET_STARTED", "HELD"):
                model.advance({"case_id": model.case_id, "invocation_sha256": model.invocation_sha256,
                    "sequence": model.count, "previous": model.previous, "kind": kind, "payload": {"evidence_sha256": c.sha(kind.encode())}})
            result = session.advance_held(model, observer)
            self.assertEqual(result["phase"], "ORACLE_MATCHED")
            observer.resume_observer(); observer.receive("CONTINUATION_DISPATCHED")
            self.assertEqual(len(pair.events), count)
            pair.set_state(final(pre=pair.linux_ns()))
            (pair.folders["target"] / "finish").write_text("yes")
            target.receive("FINISHED"); target.finish_transport()
            session.observe_final(target)
            session.close()
            with self.assertRaises(c.Invalid): session.closure(Deadline(1))
            pair.stopped("target"); pair.stopped("oracle")
            joined = session.closure(Deadline(1))
            self.assertTrue(joined["target_stopped"] and joined["client_retired"])
            self.assertFalse(joined["runtime_evidence"] or joined["execution_authorized"] or joined["closed_no_reply_window"])
            self.assertTrue(joined["cleanup_required"])

    def test_worker_source_mismatch_cannot_initialize_an_oracle(self):
        with OraclePair(wrong_source=True) as pair:
            pair.pair()
            with self.assertRaises(c.Invalid): pair.open_oracle()
            self.assertIsNone(pair.backend._oracle_session)
            self.assertNotIn("oracle", pair.backend._native_admissions)

    def test_oracle_loss_blocks_start_and_missing_match_blocks_resume(self):
        for fault in ("oracle_loss", "missing_match"):
            with self.subTest(fault=fault), OraclePair() as pair:
                target, observer = pair.pair()
                session = pair.open_oracle()
                observer.arm_observer(); observer.receive("ARMED")
                if fault == "oracle_loss":
                    session.stream.close()
                    with self.assertRaises(c.Invalid): target.start_target()
                else:
                    target.start_target()
                    pair.fault("observer", "held"); observer.receive("HELD")
                    with self.assertRaisesRegex(c.Invalid, "ORACLE_MATCH_REQUIRED"): observer.resume_observer()

    def test_bad_held_state_poisoning_prevents_resume_credit(self):
        with OraclePair() as pair:
            target, observer = pair.pair(); session = pair.open_oracle()
            observer.arm_observer(); observer.receive("ARMED"); target.start_target()
            pair.fault("observer", "held"); observer.receive("HELD")
            # Initial state is not the required held ordinal/state.
            with self.assertRaises(c.Invalid): session.observe_held(observer)
            self.assertTrue(session.failed)
            with self.assertRaises(c.Invalid): observer.resume_observer()

    def test_final_authorizer_cannot_turn_restart_into_stop_proof(self):
        with OraclePair() as pair:
            pair.pair(); session = pair.open_oracle(); session.close()
            pair.stopped("target"); pair.stopped("oracle")
            calls = []
            def authorize(action):
                if action == "oracle_closure":
                    calls.append(action)
                    if len(calls) == 2:
                        value = pair.fake.containers[pair.registry.entries["target"]["reference"]["id"]]
                        value["RestartCount"] = 1
                return True
            pair.auth_hook = authorize
            with self.assertRaises(c.Invalid): session.closure(Deadline(1))

    def test_returned_receipt_mutation_cannot_change_closure_credit(self):
        with OraclePair() as pair:
            pair.pair(); session = pair.open_oracle()
            reply = session.close()
            reply["payload"]["client"].update(classification_at_cut="known_pre_acknowledgment", cut_proved=True, eof=True)
            pair.stopped("target"); pair.stopped("oracle")
            joined = session.closure(Deadline(1))
            self.assertEqual(joined["client"]["classification_at_cut"], "not_applicable")
            self.assertFalse(joined["closed_no_reply_window"])

    def test_real_unix_stream_keeps_fragment_completion_and_eof(self):
        reader, writer = socket.socketpair()
        client = FrameClient("P3.receipt-race.1", clock=lambda: stamp[0])
        stamp = [90]
        reader.setblocking(False)
        client.sock, client.connected = reader, True
        try:
            writer.sendall(b"C" * 1023); client.pump()
            client.transcript.cut(100, 200)
            self.assertEqual(client.transcript.summary()["classification_at_cut"], "known_pre_acknowledgment")
            stamp[0] = 250
            writer.sendall(b"C"); writer.close(); client.pump()
            self.assertTrue(client.transcript.summary()["complete_after_cut"])
            self.assertTrue(client.transcript.eof)
        finally:
            client.close(); writer.close()

    def test_oracle_profile_only_adds_readonly_file_backed_exec_mapping(self):
        rows = profile("oracle")["syscalls"]
        def allowed(name, args):
            def matches(rule):
                actual = args[rule["index"]]
                if rule["op"] == "SCMP_CMP_EQ": return actual == rule["value"]
                self.assertEqual(rule["op"], "SCMP_CMP_MASKED_EQ")
                return actual & rule["value"] == rule["valueTwo"]
            return any(name in row["names"] and row["action"] == "SCMP_ACT_ALLOW" and all(matches(rule) for rule in row["args"]) for row in rows)
        for prot, flags, expected in ((5, 2, True), (7, 2, False), (5, 34, False), (3, 34, True)):
            with self.subTest(prot=prot, flags=flags):
                self.assertEqual(allowed("mmap", [0, 4096, prot, flags, 3, 0]), expected)
        self.assertFalse(allowed("mprotect", [0, 4096, 5]))
        self.assertTrue(allowed("getsockopt", [3, 1, 17, 0, 0]))
        self.assertFalse(allowed("getsockopt", [3, 1, 7, 0, 0]))
        for name in ("ptrace", "setns", "unshare", "mount", "clock_settime"):
            self.assertFalse(allowed(name, [0] * 6))


if __name__ == "__main__":
    unittest.main()
