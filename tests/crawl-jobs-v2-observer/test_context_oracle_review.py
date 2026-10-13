"""Independent-review counterexamples: fake metadata/clocks and benign real pipes."""
from contextlib import contextmanager
from pathlib import Path
import socket
import sys
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import contracts as c
from controller import native_message
from oracle_bridge import OracleSession
import oracle_protocol as wire
from oracle_worker import FrameClient
import owned_resources as owned
import runtime_admission as admission
import runtime_context as ctx
from streams import Deadline, ProcessStream, WireBudget
from test_owned_resources import FakeDocker, fixture_scope
import test_runtime_admission as admission_tests
from test_runtime_context import fixture, KEY


class GuardStream(admission_tests.FakeStream):
    """Inert descriptors; production dispatch and readiness code, no native actor."""
    _send = ProcessStream._send
    start_target = ProcessStream.start_target
    require_quiet_live = ProcessStream.require_quiet_live

    def __init__(self, binding, deadline, **kwargs):
        super().__init__(binding, deadline, kwargs["before_input"])
        self.before_write = kwargs["before_write"]
        self.frames, self.decoder = [], SimpleNamespace(partial=bytearray())
        self._process = SimpleNamespace(stdin=SimpleNamespace(fileno=lambda: 12345))
        self.poll_hook = lambda: None

    def _pump(self, timeout):
        self.poll_hook()


# Correctly chained, individually valid responses; several modes deliberately
# contradict earlier client observations. No actual witness/kernel is claimed.
RESPONDER = r'''
import os, sys
sys.path.insert(0, sys.argv[1])
import contracts as c
import oracle_protocol as wire
mode = sys.argv[2]
identity = {"device": 1, "inode": 2, "uid": 999, "gid": 999, "mode": 0o600, "bytes": 8480}
previous, sequence = None, 0
for raw in sys.stdin.buffer:
    value = c.decode(raw)
    binding = {key: value[key] for key in ("case_id", "invocation_sha256")}
    wire.parse_request(raw, binding, sequence, previous)
    action = value["action"]
    enabled = binding["case_id"].startswith("P3.")
    client = {"bytes": 0, "complete_ns": 0, "eof": False, "closed": False,
        "transport_error": False, "cut_proved": False,
        "classification_at_cut": "unknown" if enabled else "not_applicable", "complete_after_cut": False}
    if action == "INIT":
        payload = {"witness": identity, "state_sha256": c.sha(b"initial")}
    else:
        payload = {"witness_sha256": c.sha(c.canonical(identity))}
        if mode != "uncut":
            client.update(cut_proved=True)
            if enabled:
                client.update(bytes=17, classification_at_cut="known_pre_acknowledgment")
            if mode in ("acknowledged", "tied", "unordered"):
                client.update(bytes=1024, complete_ns={"acknowledged": 90, "tied": 100, "unordered": 150}[mode],
                    classification_at_cut="acknowledged" if mode == "acknowledged" else "unknown")
        if action == "HELD":
            payload.update(state_sha256=c.sha(b"held"), observer_sha256=value["payload"]["observer_sha256"],
                lower_ns=100, upper_ns=200, ordinal=1)
        elif action == "FINAL":
            payload.update(state_sha256=c.sha(b"final"), post_ns=300)
            if mode in ("erase-complete", "late"):
                client.update(bytes=1024, complete_ns=250, complete_after_cut=True)
            elif mode in ("erase-error", "reset"):
                client["transport_error"] = True
        elif action == "CLOSE":
            client.update(closed=True, eof=enabled and mode not in ("reset", "unresolved", "uncut"))
            if mode == "late":
                client.update(bytes=1024, complete_ns=250, complete_after_cut=True)
            elif mode == "reset":
                client["transport_error"] = True
    payload["client"] = client
    result = {**binding, "version": 1, "sequence": sequence, "previous": previous,
        "phase": wire.ACTIONS[action], "payload": payload, "execution_authorized": False, "runtime_evidence": False}
    encoded = c.canonical(result)
    wire.response(encoded, binding, sequence, previous, wire.ACTIONS[action])
    sys.stdout.buffer.write(encoded); sys.stdout.buffer.flush()
    previous, sequence = c.sha(encoded), sequence + 1
    if action == "CLOSE":
        # Publish EOF while the owned fixture leader remains alive for disposal;
        # avoid Darwin's departed-process-group signaling race in this control.
        os.close(1); os.close(2)
        sys.stdin.buffer.read(1)
        break
'''


class QuietActor:
    def __init__(self, binding, phase):
        self.binding, self.phase, self.deadline = binding, phase, Deadline(5)
        self.last_message = native_message(c.canonical({"phase": "HELD", "version": 1, "events": 1,
            "ordinal": 1, "read_bytes": 8}), "HELD")
        self.last_message_sha256 = c.sha(c.canonical(self.last_message))

    def require_quiet_live(self, phase):
        assert phase == self.phase


@contextmanager
def oracle_fixture(mode, case="P3.receipt-race.1"):
    scope = fixture_scope(case)
    fake, registry = FakeDocker(scope), owned.Registry(scope, lambda _: None)
    deadline, prep = Deadline(5), admission_tests.preparation()
    for role in (*owned.VOLUMES, *owned.ACTORS):
        registry.candidate(role)
        spec = registry.spec(role)
        if role in owned.VOLUMES:
            fake.create_volume(spec, deadline)
            row = fake.inspect("volume", spec["name"], deadline)
        else:
            identifier = fake.create_container(spec, deadline)
            row = fake.inspect("container", identifier, deadline)
        registry.bind(role, row)
    def binding(role):
        return {"architecture": scope["architecture"], "case_id": case, "invocation_sha256": c.sha(c.canonical(scope)), "role": role,
            "container_id": registry.entries[role]["reference"]["id"], "image": scope["images"][role]}
    target_validated = admission.validate(admission_tests.frames("target", bound=binding("target")),
        binding("target"), prep, admission_tests.context())
    validated = admission.validate(admission_tests.frames("oracle", bound=binding("oracle")),
        binding("oracle"), prep, admission_tests.context(), target=target_validated)
    target, observer = QuietActor(binding("target"), "START_DISPATCHED"), QuietActor(binding("observer"), "HELD")
    ref = registry.entries["oracle"]["reference"]
    with ProcessStream([sys.executable, "-I", "-B", "-c", RESPONDER, str(Path(__file__).resolve().parent), mode],
            binding("oracle"), WireBudget(), deadline) as stream:
        stream.sent, stream.phase = True, "ORACLE_READY"
        backend = SimpleNamespace(scope=scope, _authorize=lambda _: None, inspect=fake.inspect,
            _actor_streams={ref["id"]: stream}, _runtime_starts={}, _native_admissions={
                "oracle": {"stream": stream, "validated": validated, "expires_at": deadline.end},
                "target": {"stream": target}, "observer": {"stream": observer}})
        for role in ("target", "oracle"):
            identifier = registry.entries[role]["reference"]["id"]
            fake.containers[identifier]["RestartCount"] = 0
            fake.containers[identifier]["State"].update(Running=False, Pid=0, Status="exited", StartedAt="2026-10-09T00:00:02Z")
            backend._runtime_starts[identifier] = {"started_at": "2026-10-09T00:00:02Z"}
        session = OracleSession(backend, stream, ref, registry.snapshot(), prep)
        session.initialize()
        yield session, target, observer


class ContextOracleReviewTests(unittest.TestCase):
    def dispatch_expiry(self, which):
        helper = admission_tests.DockerAdmissionTests()
        registry, fake, backend, _, _ = helper.fixture()
        reader, clock, anchor, prep, _, _, deadline, budget = fixture()
        anchor = anchor | {"daemon_sha256": registry.scope["daemon_sha256"]}
        endpoint = ctx.LinuxContextEndpoint(anchor, KEY, reader)
        lease = backend.acquire_runtime_context(ctx.ContextSource(anchor, KEY, endpoint.exchange, authorize=lambda *_: True),
            prep, budget, deadline)
        streams = {}
        def factory(argv, binding, budget, deadline, **kwargs):
            role = binding["role"]
            fake.containers[binding["container_id"]]["State"].update(Running=True, Pid=101 if role == "target" else 102,
                Status="running", StartedAt="2026-10-09T00:00:02Z")
            fake.containers[binding["container_id"]]["RestartCount"] = 0
            stream = GuardStream(binding, deadline, **kwargs)
            streams[role] = stream
            return stream
        backend.stream_factory = factory
        for role in ("target", "observer"):
            limit = Deadline(1 if which == role else 8, clock)
            stream, ref = helper.open(registry, backend, role, limit)
            backend.collect_native_admission(stream, ref, registry.snapshot(), prep, lease, limit)
        target, observer = streams["target"], streams["observer"]
        observer.phase = "ARMED"
        oracle = GuardStream(target.binding | {"role": "oracle"}, Deadline(1 if which == "oracle" else 8, clock),
            before_input=None, before_write=None)
        oracle.phase, oracle.sent = "ORACLE_INITIALIZED", True
        backend._native_admissions["oracle"] = {"stream": oracle, "expires_at": oracle.deadline.end}
        session = object.__new__(OracleSession)
        session.failed, session.phase, session.stream, session.backend = False, "INITIALIZED", oracle, backend
        backend._oracle_session = session
        polls, writes = [], []
        def poll():
            polls.append(clock.now)
            if len(polls) == 2:
                clock.now += 0.25 if which == "live" else 2.5 if which == "lease" else 1.5
        oracle.poll_hook = poll
        with patch("streams.os.write", side_effect=lambda fd, raw: writes.append(raw) or len(raw)):
            if which == "live":
                target.start_target()
            else:
                with self.assertRaises(c.Invalid): target.start_target()
        self.assertEqual(len(polls), 2)
        self.assertEqual(writes, [b"G"] if which == "live" else [])
        if which != "live":
            self.assertNotEqual(target.phase, "START_DISPATCHED")
            self.assertEqual(backend._native_admissions, {})

    def test_final_oracle_poll_rechecks_observer_expiry(self): self.dispatch_expiry("observer")
    def test_final_oracle_poll_rechecks_context_lease(self): self.dispatch_expiry("lease")
    def test_final_oracle_poll_rechecks_target_expiry(self): self.dispatch_expiry("target")
    def test_final_oracle_poll_rechecks_oracle_expiry(self): self.dispatch_expiry("oracle")
    def test_live_final_oracle_poll_still_allows_start(self): self.dispatch_expiry("live")

    def test_resume_poll_rechecks_oracle_operation_deadline(self):
        clock = fixture()[1]
        observer = QuietActor({"architecture": "aarch64", "case_id": "P1.pre.1.1", "invocation_sha256": c.sha(b"scope")}, "HELD")
        stream = GuardStream(observer.binding | {"role": "oracle"}, Deadline(1, clock), before_input=None, before_write=None)
        stream.phase, stream.sent = "ORACLE_HELD", True
        stream.poll_hook = lambda: setattr(clock, "now", clock.now + 2)
        session = object.__new__(OracleSession)
        session.failed, session.phase, session.stream = False, "HELD", stream
        session.matched_observer = observer.last_message_sha256
        session.backend = SimpleNamespace(_native_admissions={"observer": {"stream": observer}})
        with self.assertRaises(c.Invalid): session.authorize_resume(observer)

    def test_equal_clock_receipt_and_zero_width_cut_remain_unordered(self):
        for lower, upper, completed, expected in ((100, 200, 99, "acknowledged"), (100, 200, 100, "unknown"),
                (100, 100, 100, "unknown"), (100, 100, 101, "unknown")):
            with self.subTest(lower=lower, upper=upper, completed=completed):
                reader, writer = socket.socketpair()
                client = FrameClient("P3.received.1", clock=lambda: completed)
                reader.setblocking(False)
                client.sock, client.connected = reader, True
                try:
                    writer.sendall(b"C" * 1024); client.pump()
                    client.transcript.cut(lower, upper)
                    value = client.transcript.summary()
                    self.assertEqual(value["classification_at_cut"], expected)
                    self.assertEqual(value["complete_after_cut"], completed > upper)
                finally:
                    client.close(); writer.close()

    def reject_history(self, mode):
        with oracle_fixture(mode) as (session, target, observer):
            if mode != "fabricate-cut":
                session.observe_held(observer)
                target.phase = "FINISHED"
                session.observe_final(target)
            with self.assertRaises(c.Invalid): session.close()
            self.assertTrue(session.failed)
            self.assertTrue(session.stream.closed)
            self.assertIsNone(session.closed_receipt)
            with self.assertRaises(c.Invalid): session.closure(Deadline(1))

    def test_chained_close_cannot_erase_complete_receipt(self): self.reject_history("erase-complete")
    def test_chained_close_cannot_erase_transport_error(self): self.reject_history("erase-error")
    def test_chained_close_cannot_invent_held_cut(self): self.reject_history("fabricate-cut")

    def test_honest_partial_late_reset_and_unresolved_closure_remain_distinct(self):
        for mode in ("partial", "late", "reset", "unresolved", "uncut", "nonstream", "acknowledged", "tied", "unordered"):
            case = "P1.pre.1.1" if mode == "nonstream" else "P3.received.1" if mode in ("acknowledged", "tied", "unordered") else "P3.receipt-race.1"
            with self.subTest(mode=mode), oracle_fixture(mode, case) as (session, target, observer):
                if mode != "uncut":
                    result = session.observe_held(observer)
                    # Public receipt mutation must not rewrite retained history.
                    result["payload"]["client"]["bytes"] = 0
                    target.phase = "FINISHED"
                    session.observe_final(target)
                session.close()
                joined = session.closure(Deadline(1))
                self.assertEqual(joined["closed_no_reply_window"], mode == "partial")
                self.assertFalse(joined["execution_authorized"] or joined["runtime_evidence"])
                self.assertTrue(joined["cleanup_required"])
                if mode == "late": self.assertTrue(joined["client"]["complete_after_cut"])
                if mode == "reset": self.assertTrue(joined["client"]["transport_error"])
                if mode in ("tied", "unordered"): self.assertEqual(joined["client"]["classification_at_cut"], "unknown")

    def test_history_rejects_changed_completion_cut_or_terminal_bytes(self):
        initial = wire.ClientTranscript("P3.received.1").summary()
        held = initial | {"bytes": 1024, "complete_ns": 100, "cut_proved": True}
        for change in ({"complete_ns": 101}, {"classification_at_cut": "acknowledged"}, {"cut_proved": False},
                {"complete_after_cut": True}):
            with self.subTest(change=change):
                history = wire.ClientHistory("P3.received.1")
                history.accept("ORACLE_INITIALIZED", {"client": initial})
                history.accept("ORACLE_HELD", {"client": held, "lower_ns": 100, "upper_ns": 200})
                with self.assertRaises(c.Invalid): history.accept("ORACLE_CLOSED", {"client": held | {"closed": True} | change})
        history = wire.ClientHistory("P3.receipt-race.1")
        history.accept("ORACLE_INITIALIZED", {"client": initial})
        partial = initial | {"bytes": 17, "cut_proved": True, "classification_at_cut": "known_pre_acknowledgment"}
        history.accept("ORACLE_HELD", {"client": partial, "lower_ns": 100, "upper_ns": 200})
        history.accept("ORACLE_FINAL", {"client": partial | {"eof": True}})
        with self.assertRaises(c.Invalid):
            history.accept("ORACLE_CLOSED", {"client": partial | {"bytes": 18, "eof": True, "closed": True}})


if __name__ == "__main__":
    unittest.main()
