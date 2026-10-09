"""Real bounded Python pipes at the admission boundary; Docker stays synthetic."""
import json
import os
from pathlib import Path
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

import contracts as c
import docker_backend as db
import owned_resources as owned
from streams import Deadline, ProcessStream, WireBudget
from test_owned_resources import DAEMON, prepared
from test_runtime_admission import context, frames, preparation

ACTOR = r'''
import json, os, select, sys, time
from pathlib import Path
root = Path(sys.argv[1])
cfg = json.loads((root / "config.json").read_text())
assert sys.stdin.buffer.readline().decode().rstrip("\n") == cfg["case_id"]
assert sys.stdin.buffer.readline().decode().rstrip("\n") == cfg["invocation_sha256"]
for raw in cfg["frames"]:
    os.write(1, raw.encode())
phase = "READY" if cfg["role"] == "target" else "ADMISSION_READY"
os.write(1, (json.dumps({"phase": phase, "version": 1}, sort_keys=True, separators=(",", ":")) + "\n").encode())
injected = False
until = time.monotonic() + 15
while time.monotonic() < until:
    trigger = root / "fault"
    if trigger.exists() and not injected:
        mode = trigger.read_text()
        if mode == "eof":
            os.close(1)
        elif mode == "stderr":
            os.write(2, b"private-diagnostic-canary\n")
        elif mode == "partial":
            os.write(1, b'{"phase":')
        elif mode == "extra":
            os.write(1, b'{"phase":"FINISHED","version":1}\n')
        elif mode == "invalid":
            os.write(1, b'{"cleanup_required":true,"reason":"transport","status":"INVALID","version":1}\n')
        elif mode == "held":
            os.write(1, b'{"events":1,"ordinal":1,"phase":"HELD","read_bytes":8,"version":1}\n')
        else:
            raise AssertionError("fixture fault")
        (root / "fault-ready").write_text("ready")
        injected = True
    if not select.select([0], [], [], 0.002)[0]:
        continue
    byte = os.read(0, 1)
    if not byte:
        break
    with (root / "received").open("ab") as log:
        log.write(byte)
    if cfg["role"] == "observer" and byte == b"A":
        os.write(1, b'{"phase":"ARMED","version":1}\n')
    elif cfg["role"] == "target" and byte == b"G":
        os.write(1, b'{"phase":"FINISHED","version":1}\n')
    else:
        raise AssertionError("fixture input")
'''


class PipePair:
    def __init__(self, target_seconds=5, observer_seconds=5):
        self.directory = tempfile.TemporaryDirectory(prefix="admission-dispatch-")
        self.root = Path(self.directory.name)
        self.script = self.root / "actor.py"
        self.script.write_text(ACTOR)
        self.registry, self.fake, _, _ = prepared()
        self.streams, self.folders, self.events = {}, {}, []
        self.deadlines = {"target": Deadline(target_seconds), "observer": Deadline(observer_seconds)}
        self.auth_hook = self.meta_hook = None

        def authorize(action, scope):
            return True if self.auth_hook is None else self.auth_hook(action)

        def runner(argv, deadline):
            args = argv[3:]
            self.events.append(args)
            if self.meta_hook:
                self.meta_hook(args)
            if args[0] == "info":
                return 0, DAEMON, b""
            if args[0] == "version":
                return 0, b'{"Os":"linux","Arch":"arm64"}', b""
            if args[1] == "ls":
                volume = args[args.index("--filter") + 1].removeprefix("volume=")
                return 0, "\n".join(self.fake.attachments(volume, deadline)).encode(), b""
            assert args[1] == "inspect"
            return 0, json.dumps([self.fake.inspect(args[0], args[2], deadline)]).encode(), b""

        def factory(argv, bound, budget, deadline, **kwargs):
            role = bound["role"]
            folder = self.root / role
            folder.mkdir()
            cfg = dict(bound, frames=[raw.decode() for raw in frames(role, bound=bound)])
            (folder / "config.json").write_text(json.dumps(cfg))
            value = self.fake.containers[bound["container_id"]]
            value["State"].update(Running=True, Pid=101 if role == "target" else 102,
                Status="running", StartedAt="2026-10-08T00:00:02Z")
            value["RestartCount"] = 0
            stream = ProcessStream([sys.executable, "-I", "-B", str(self.script), str(folder)], bound, budget, deadline, **kwargs)
            self.streams[role], self.folders[role] = stream, folder
            return stream

        self.backend = db.DockerBackend(self.registry.scope, {role: "/unused" for role in owned.ACTORS},
            authorize=authorize, runner=runner, stream_factory=factory)
        self.budget = WireBudget()

    def open(self, role):
        ref = self.registry.entries[role]["reference"]
        stream = self.backend.open_actor(ref, self.registry.spec(role), self.registry.snapshot(), self.budget, self.deadlines[role])
        stream.send_case()
        return stream

    def collect(self, role):
        return self.backend.collect_native_admission(self.streams[role], self.registry.entries[role]["reference"],
            self.registry.snapshot(), preparation(), context(), self.deadlines[role])

    def pair(self):
        for role in ("target", "observer"):
            self.open(role)
            result = self.collect(role)
            assert result["evidence_kind"] == "simulated" and result["runtime_evidence"] is False
        return self.streams["target"], self.streams["observer"]

    def fault(self, role, kind):
        # Publish only a complete command: the peer polls `fault` concurrently.
        trigger = self.folders[role] / "fault"
        pending = trigger.with_name("fault.pending")
        pending.write_text(kind)
        pending.replace(trigger)
        until = time.monotonic() + 2
        while not (self.folders[role] / "fault-ready").exists():
            assert time.monotonic() < until
            time.sleep(0.002)
        assert self.streams[role].selector.select(0), "fault must already be readable"

    def close(self):
        errors = []
        try:
            for stream in self.streams.values():
                try:
                    stream.close()
                except c.Invalid as error:
                    process = stream._process
                    # These fixtures have no descendants. Darwin may refuse a
                    # signal to a departed group even though close() completed
                    # its finally/reap. This remains a refusal in production;
                    # test teardown verifies local disposal, not remote absence.
                    if str(error) != "LOCAL_PROCESS_CLEANUP" or process.returncode is None or not all(
                            pipe.closed for pipe in (process.stdin, process.stdout, process.stderr)):
                        errors.append(error)
        finally:
            self.directory.cleanup()
        if errors:
            raise errors[0]

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()


class DispatchRegressionTests(unittest.TestCase):
    def test_fault_publication_is_atomic_to_the_polling_peer(self):
        for fault in ("eof", "stderr", "partial", "extra", "invalid", "held"):
            with self.subTest(fault=fault), PipePair() as h:
                _, observer = h.pair()
                folder = h.folders["target"]
                trigger = folder / "fault"
                original_write_text = Path.write_text
                stages = []
                def interrupted_write(path, text, *args, **kwargs):
                    if path.parent != folder:
                        return original_write_text(path, text, *args, **kwargs)
                    # Expose both incomplete-write states deterministically.
                    # The real peer must not be able to see either via `fault`.
                    with path.open("w", encoding="utf-8") as stream:
                        self.assertFalse(trigger.exists(), "empty fault command became visible")
                        stages.append("empty")
                        stream.write(text[:1])
                        stream.flush()
                        self.assertFalse(trigger.exists(), "partial fault command became visible")
                        stages.append("partial")
                        stream.write(text[1:])
                    return len(text)
                with patch.object(Path, "write_text", interrupted_write):
                    h.fault("target", fault)
                self.assertEqual(stages, ["empty", "partial"])
                self.assertEqual(trigger.read_text(), fault)
                self.assertTrue((folder / "fault-ready").exists())
                self.dispatch(observer, observer.arm_observer)

    def dispatch(self, stream, command, should_pass=False):
        sent = []
        fd = stream._process.stdin.fileno()
        original_write = os.write
        def spy(descriptor, raw):
            if descriptor == fd:
                sent.append(raw)
            return original_write(descriptor, raw)
        with patch("streams.os.write", side_effect=spy):
            if should_pass:
                command()
            else:
                with self.assertRaises(c.Invalid) as caught:
                    command()
                self.assertNotIn("private-diagnostic-canary", str(caught.exception))
        self.assertEqual(sent, [b"A" if stream.binding["role"] == "observer" else b"G"] if should_pass else [])

    def test_live_pair_allows_arm_then_start_without_dispatch_docker_reads(self):
        with PipePair() as h:
            target, observer = h.pair()
            events = len(h.events)
            self.dispatch(observer, observer.arm_observer, True)
            observer.receive("ARMED")
            self.dispatch(target, target.start_target, True)
            target.receive("FINISHED")
            self.assertEqual(len(h.events), events)

    def test_peer_pipe_faults_block_both_arm_and_start(self):
        for command in ("arm", "start"):
            for fault in ("eof", "stderr", "partial", "extra", "invalid", "held"):
                with self.subTest(command=command, fault=fault), PipePair() as h:
                    target, observer = h.pair()
                    if command == "start":
                        observer.arm_observer()
                        observer.receive("ARMED")
                    peer = "target" if command == "arm" else "observer"
                    h.fault(peer, fault)
                    stream = observer if command == "arm" else target
                    self.dispatch(stream, stream.arm_observer if command == "arm" else stream.start_target)
                    self.assertFalse(h.backend._native_admissions)

    def test_known_peer_eof_and_pre_start_held_cannot_be_relabelled(self):
        for fault in ("eof", "held"):
            with self.subTest(fault=fault), PipePair() as h:
                target, observer = h.pair()
                observer.arm_observer()
                observer.receive("ARMED")
                h.fault("observer", fault)
                observer._pump(0)
                self.dispatch(target, target.start_target)
                self.assertTrue(observer.closed)

    def test_postflight_metadata_transport_loss_prevents_admission(self):
        with PipePair() as h:
            stream = h.open("target")
            injected = []
            def metadata(args):
                if args[:2] == ["container", "inspect"] and stream.phase == "READY" and not injected:
                    injected.append(True)
                    h.fault("target", "eof")
            h.meta_hook = metadata
            with self.assertRaisesRegex(c.Invalid, "NATIVE_ADMISSION_REJECTED"):
                h.collect("target")
            self.assertTrue(injected and stream.closed)
            self.assertFalse(h.backend._native_admissions)

    def test_final_collection_authorizer_transport_loss_prevents_admission(self):
        with PipePair() as h:
            stream = h.open("target")
            def authorize(action):
                if action == "runtime_admission" and stream.phase == "READY":
                    h.fault("target", "partial")
                return True
            h.auth_hook = authorize
            with self.assertRaisesRegex(c.Invalid, "NATIVE_ADMISSION_REJECTED"):
                h.collect("target")
            self.assertTrue(stream.closed)

    def test_final_authorizer_latency_cannot_extend_own_or_peer_expiry(self):
        for command in ("arm", "start"):
            for peer_only in (False, True):
                short = 0.75
                target_seconds = short if not peer_only or command == "arm" else 5
                observer_seconds = short if not peer_only or command == "start" else 5
                with self.subTest(command=command, peer_only=peer_only), PipePair(target_seconds, observer_seconds) as h:
                    target, observer = h.pair()
                    if command == "start":
                        observer.arm_observer()
                        observer.receive("ARMED")
                    stream = observer if command == "arm" else target
                    expiry = min(h.deadlines["target"].end, h.deadlines["observer"].end)
                    calls = []
                    def authorize(action):
                        if action == "actor_input":
                            calls.append(action)
                            if len(calls) == 2:
                                time.sleep(max(0, expiry - time.monotonic()) + 0.02)
                        return True
                    h.auth_hook = authorize
                    self.dispatch(stream, stream.arm_observer if command == "arm" else stream.start_target)
                    self.assertEqual(len(calls), 2)
                    if peer_only:
                        self.assertGreater(stream.deadline.remaining(), 0)

    def test_peer_expiry_during_sender_pump_is_checked_at_write(self):
        with PipePair(0.75, 5) as h:
            _, observer = h.pair()
            original_pump = observer._pump
            def delayed_pump(timeout):
                original_pump(timeout)
                time.sleep(max(0, h.deadlines["target"].end - time.monotonic()) + 0.02)
            with patch.object(observer, "_pump", side_effect=delayed_pump):
                self.dispatch(observer, observer.arm_observer)
            self.assertGreater(observer.deadline.remaining(), 0)

    def test_final_authorizer_revocation_blocks_dispatch(self):
        with PipePair() as h:
            _, observer = h.pair()
            calls = []
            def authorize(action):
                if action == "actor_input":
                    calls.append(action)
                    return len(calls) != 2
                return True
            h.auth_hook = authorize
            self.dispatch(observer, observer.arm_observer)


if __name__ == "__main__":
    unittest.main()
