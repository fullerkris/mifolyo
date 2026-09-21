"""COR-1/COR-2/SEC-1/SEC-2 regressions. No Docker or Redis is started.

Boundary fakes call the real controller, transport and revocation logic. Local
Python children exercise process-side timers; no external service is used.
"""
from __future__ import annotations

import hashlib
import signal
import subprocess
import sys
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import controller as ctl
import executor as worker
import harness as h
import resp
import runtime_case as case
import test_execution as fixtures


class Clock:
    def __init__(self):
        self.elapsed = 0.0
        self.wall_adjustment = 0.0

    def time(self):
        return 2000000000.0 + self.elapsed + self.wall_adjustment

    def monotonic(self):
        return 10000.0 + self.elapsed


class LifecycleReviewTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plan = fixtures.test_plan()

    def test_cor1_timeout_worker_is_removed_before_fresh_revoker(self):
        class LateWorker(fixtures.FakeDocker):
            late_worker_running = False
            old_executor = None
            revoke_overlap = False

            def stage(self, name, stage, request, timeout):
                if stage == "resume":
                    self.late_worker_running = True
                    self.old_executor = name
                    raise ctl.CommandError("COMMAND_TIMEOUT")
                if stage == "revoke":
                    self.revoke_overlap = self.late_worker_running
                    assert name != self.old_executor
                    assert ("container", self.old_executor) not in self.resources
                return super().stage(name, stage, request, timeout)

            def kill(self, name):
                super().kill(name)
                if name == self.old_executor:
                    self.late_worker_running = False

        backend = LateWorker()
        report = ctl.execute(self.plan, fixtures.approval(self.plan), backend, revision_check=lambda _: None)
        self.assertFalse(backend.revoke_overlap)
        self.assertFalse(backend.late_worker_running)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertEqual(report["revocation"], "verified")
        self.assertTrue(report["worker_quiescence"]["removed"])
        self.assertLess(backend.events.index("wait:executor"), backend.events.index("create:revocation"))
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_cor1_unknown_stop_blocks_revocation_and_cleanup_removes_worker_first(self):
        class UnprovedWorker(fixtures.FakeDocker):
            def quiesce(self, name, fixture_id):
                raise ctl.CommandError("WAIT_TIMEOUT")

            def remove(self, kind, name):
                if name.endswith("-redis"):
                    assert not any(k == "container" and n.endswith("-executor") for k, n in self.resources)
                super().remove(kind, name)

        backend = UnprovedWorker()
        report = ctl.execute(self.plan, fixtures.approval(self.plan), backend, revision_check=lambda _: None)
        self.assertNotIn("revoke", backend.events)
        self.assertNotIn("create:revocation", backend.events)
        self.assertEqual(report["revocation"], "not_proven")
        self.assertEqual(report["verdict"], "FAIL")

    def test_cor1_failed_revoker_is_removed_before_redis(self):
        class LateRevoker(fixtures.FakeDocker):
            def remove(self, kind, name):
                if name.endswith("-redis"):
                    assert not any(k == "container" and n.endswith(("-executor", "-revocation")) for k, n in self.resources)
                super().remove(kind, name)
        backend = LateRevoker("revoke")
        report = ctl.execute(self.plan, fixtures.approval(self.plan), backend, revision_check=lambda _: None)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertTrue(all(row["removed"] for row in report["cleanup"]))

    def test_cor1_quiesce_requires_owner_wait_and_zero_pid(self):
        backend = fixtures.FakeDocker()
        spec = ctl.container_spec("fixture-executor", "executor", "a" * 32, "sha256:" + "1" * 64,
                                  {"control": "own-control", "data": "own-data"})
        backend.create(spec)
        backend.start(spec["name"])
        with self.assertRaises(h.InvalidArtifact):
            backend.quiesce(spec["name"], "b" * 32)
        self.assertNotIn("kill:executor", backend.events)
        original_call = backend.call
        def bad_wait(*args, **kwargs):
            result = original_call(*args, **kwargs)
            if args[:2] == ("container", "wait"):
                backend.resources[("container", spec["name"])]["State"]["Pid"] = 999
            return result
        with patch.object(backend, "call", side_effect=bad_wait), self.assertRaises(h.InvalidArtifact):
            backend.quiesce(spec["name"], "a" * 32)
        self.assertIn(("container", spec["name"]), backend.resources)

    def test_cor1_ready_failure_is_not_retried_in_another_exec(self):
        backend = fixtures.FakeDocker("ready")
        report = ctl.execute(self.plan, fixtures.approval(self.plan), backend, revision_check=lambda _: None)
        self.assertEqual(backend.events.count("ready"), 1)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertIn("wait:executor", backend.events)

    def test_sec2_slow_revision_or_journal_cannot_start_resources(self):
        for where in ("revision", "journal"):
            clock = Clock()
            with self.subTest(where=where), patch.object(ctl.time, "time", clock.time), \
                    patch.object(ctl.time, "monotonic", clock.monotonic):
                approval = fixtures.approval(self.plan)
                approval["expires_at_ms"] = int((clock.time() + 125) * 1000)
                backend = fixtures.FakeDocker()
                def delay(_):
                    clock.elapsed += 80
                with self.assertRaises(h.InvalidArtifact):
                    ctl.execute(self.plan, approval, backend,
                                revision_check=delay if where == "revision" else lambda _: None,
                                journal=delay if where == "journal" else None)
                self.assertEqual(backend.events, [])

    def test_sec2_expiry_between_create_and_start_blocks_start_but_allows_cleanup(self):
        clock = Clock()
        class SlowInspect(fixtures.FakeDocker):
            delayed = False
            def inspect(self, kind, name):
                value = super().inspect(kind, name)
                if kind == "container" and name.endswith("-redis") and value and not self.delayed:
                    self.delayed = True
                    # A forward clock jump can exhaust approval independently of
                    # the monotonic execution budget. Start must check both.
                    clock.wall_adjustment = 1001
                return value
            def start(self, name):
                if name.endswith("-redis"):
                    raise AssertionError("Redis started after expiry")
                super().start(name)
        with patch.object(ctl.time, "time", clock.time), patch.object(ctl.time, "monotonic", clock.monotonic):
            backend = SlowInspect()
            report = ctl.execute(self.plan, fixtures.approval(self.plan), backend, revision_check=lambda _: None)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertNotIn("start:redis", backend.events)
        self.assertIn("create:revocation", backend.events)
        self.assertIsNone(backend.approval_expires_at_ms)
        self.assertTrue(all(row["removed"] for row in report["cleanup"]))

    def test_sec2_every_real_docker_dispatch_checks_wall_and_monotonic_deadlines(self):
        clock = Clock()
        with patch.object(ctl.shutil, "which", return_value="/fake/docker"):
            docker = ctl.Docker()
        with patch.object(ctl.time, "time", clock.time), patch.object(ctl.time, "monotonic", clock.monotonic):
            docker.deadline = clock.monotonic() + 120
            docker.approval_expires_at_ms = int((clock.time() + 100) * 1000)
            clock.wall_adjustment = 101
            with patch.object(ctl, "command") as command:
                for action in (lambda: docker.start("fixture"), lambda: docker.volume("fixture", "a" * 32),
                               lambda: docker.inspect("container", "fixture")):
                    with self.assertRaises(ctl.CommandError):
                        action()
                command.assert_not_called()
            clock.wall_adjustment = -1000
            clock.elapsed = 121
            with self.assertRaises(ctl.CommandError):
                docker.timeout()


class ProcessReviewTests(unittest.TestCase):
    def test_cor2_exited_leader_does_not_skip_group_kill_on_any_abort(self):
        for fault in ("timeout", "output", "interrupt"):
            with self.subTest(fault=fault):
                process = SimpleNamespace(pid=12345, stdin=Mock(), stdout=Mock(), stderr=Mock(),
                                          poll=Mock(return_value=0), wait=Mock(return_value=0))
                mux = Mock()
                mux.get_map.return_value = {1: "child-held pipe"}
                event = SimpleNamespace(data="out", fd=1, fileobj=process.stdout)
                mux.select.return_value = [(event, 1)]
                if fault == "interrupt":
                    mux.select.side_effect = KeyboardInterrupt()
                manager = Mock()
                manager.__enter__ = Mock(return_value=mux)
                manager.__exit__ = Mock(return_value=False)
                times = [10.0, 11.0] if fault == "timeout" else [10.0, 10.01]
                with patch.object(ctl.subprocess, "Popen", return_value=process), \
                     patch.object(ctl.selectors, "DefaultSelector", return_value=manager), \
                     patch.object(ctl.os, "set_blocking"), \
                     patch.object(ctl.os, "read", return_value=b"x" * (ctl.OUTPUT_LIMIT + 1)), \
                     patch.object(ctl.time, "monotonic", side_effect=times), \
                     patch.object(ctl.os, "killpg") as killpg:
                    with self.assertRaises(KeyboardInterrupt if fault == "interrupt" else ctl.CommandError):
                        ctl.command(["fake-command"], timeout=0.1)
                    killpg.assert_called_once_with(12345, signal.SIGKILL)
                    process.poll.assert_not_called()
                    process.wait.assert_called_once_with(timeout=5)

    def test_cor1_worker_deadline_exits_while_blocked_on_stdin(self):
        code = ("import sys; sys.path.insert(0, sys.argv[1]); import executor; "
                "guard=executor.stage_deadline(250); guard.__enter__(); "
                "print('armed', flush=True); sys.stdin.buffer.read(1); print('resumed', flush=True)")
        child = subprocess.Popen([sys.executable, "-B", "-c", code, str(h.HERE)],
                                 stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            self.assertEqual(child.wait(timeout=10), 124)
            self.assertEqual(child.stdout.read(), b"armed\n")
            self.assertEqual(child.stderr.read(), b"")
        finally:
            if child.poll() is None:
                child.kill()
                child.wait(timeout=5)
            for stream in (child.stdin, child.stdout, child.stderr):
                stream.close()

    def test_cor1_docker_stage_passes_bounded_worker_timer(self):
        with patch.object(ctl.shutil, "which", return_value="/fake/docker"):
            docker = ctl.Docker()
        response = {"stage": "ready", "status": "PASS", "recipe_sha256": case.recipe_sha256(),
                    "isolation": {}, "result": {}}
        with patch.object(docker, "call", return_value=h.canonical(response)) as call:
            docker.stage("fixture", "ready", {}, 0.125)
            self.assertEqual(call.call_args.args[-1], "125")
        for timeout in (0, -1, 0.0001):
            with patch.object(docker, "call") as call, self.assertRaises((ctl.CommandError, h.InvalidArtifact)):
                docker.stage("fixture", "ready", {}, timeout)
            call.assert_not_called()


class ReplySocket:
    def __init__(self, replies):
        self.replies = iter(replies)
        self.pending = b""
        self.closed = False

    def settimeout(self, _):
        pass

    def connect(self, endpoint):
        assert endpoint == resp.SOCKET

    def sendall(self, _):
        self.pending = next(self.replies)

    def recv(self, count):
        # A revocation notification/EOF is observed without a new client write.
        if self.pending == b"":
            self.pending = next(self.replies, b"")
        if isinstance(self.pending, Exception):
            raise self.pending
        value, self.pending = self.pending[:count], self.pending[count:]
        return value

    def close(self):
        self.closed = True


class RevocationReviewTests(unittest.TestCase):
    def replay(self, held_observation, reconnect_observation=b"-WRONGPASS synthetic\r\n"):
        credentials = {role: hashlib.sha256(role.encode()).hexdigest() for role in case.ROLES}
        sockets = [ReplySocket([b"+OK\r\n", b":1\r\n"]), ReplySocket([b"+OK\r\n", held_observation]),
                   ReplySocket([reconnect_observation])]
        with patch.object(resp.socket, "socket", side_effect=sockets):
            result = worker.revoke(credentials, ("setup",))
        ctl.verify_revocation(result, ("setup",))
        return result

    def test_sec1_only_observed_eof_or_reset_proves_termination(self):
        for observation in (b"", ConnectionResetError()):
            with self.subTest(observation=type(observation).__name__):
                self.assertEqual(self.replay(observation)["setup"]["held_session"], "terminated")

    def test_sec1_redis_errors_timeouts_and_unreachable_server_do_not_prove_termination(self):
        for observation in (b"-NOPERM synthetic\r\n", b"-NOAUTH synthetic\r\n", b"-ERR synthetic\r\n",
                            TimeoutError(), OSError("unreachable"), b"+PONG\r\n"):
            with self.subTest(observation=type(observation).__name__), \
                    self.assertRaises((resp.RedisError, resp.TransportError, h.InvalidArtifact)):
                self.replay(observation)
        with self.assertRaises(resp.TransportError):
            self.replay(b"", reconnect_observation=TimeoutError())

    def test_sec1_ambiguous_failure_invalidates_whole_case(self):
        class UnprovedRevocation(fixtures.FakeDocker):
            def stage(self, name, stage, request, timeout):
                if stage == "revoke":
                    RevocationReviewTests().replay(TimeoutError())
                return super().stage(name, stage, request, timeout)
        plan = fixtures.test_plan()
        report = ctl.execute(plan, fixtures.approval(plan), UnprovedRevocation(), revision_check=lambda _: None)
        self.assertTrue(report["case_passed"])
        self.assertEqual(report["revocation"], "not_proven")
        self.assertEqual(report["verdict"], "FAIL")

    def test_sec1_closed_unix_peer_is_proved_without_a_write(self):
        from review import reproduce_rereview_correctness as followup
        actual_revoke = worker.revoke
        captured = []
        def record_result(*args):
            result = actual_revoke(*args)
            captured.append(result)
            return result
        # The unchanged reviewer probe uses actual AF_UNIX socket-pair IPC. Its
        # bug assertion now rejects because revoke completed through fresh AUTH.
        with patch.object(worker, "revoke", side_effect=record_result), \
                self.assertRaisesRegex(AssertionError, "counterexample no longer reproduces"):
            followup.revoked_unix_peer()
        self.assertEqual(len(captured), 1)
        ctl.verify_revocation(captured[0], ("setup",))
        self.assertEqual(captured[0]["setup"]["held_session"], "terminated")

    def test_sec1_receive_probe_rejects_buffered_data_and_never_sends(self):
        sock = fixtures.SocketFake(b"")
        client = resp.Client(connection=sock)
        client.buffer.extend(b"-NOPERM synthetic\r\n")
        with self.assertRaisesRegex(resp.TransportError, "PEER_SENT_DATA"):
            client.observe_peer_disconnect()
        self.assertEqual(sock.sent, [])
        self.assertTrue(sock.closed)


class ArchivedCounterexampleTests(unittest.TestCase):
    def test_all_four_review_counterexamples_now_hit_the_corrected_boundary(self):
        # Preserve the original reproduction files as dated evidence. Their
        # bug assertions must now fail for the specific corrected reason.
        from review import reproduce_correctness as correctness
        from review import reproduce_security as security
        with self.assertRaisesRegex(AssertionError, "cleanup waited for the exec"):
            correctness.timeout_exec_order()
        with self.assertRaisesRegex(AssertionError, "process group was killed"):
            correctness.timeout_process_group()
        with self.assertRaisesRegex(h.InvalidArtifact, "APPROVAL_EXPIRY"):
            security.expiry_replay()
        # Adapt the old PING-driven fake's delivery to the receive-only probe;
        # the injected live NOPERM bytes remain the same negative observation.
        with patch.object(security, "ReplySocket", ReplySocket), \
                self.assertRaisesRegex(resp.TransportError, "PEER_SENT_DATA"):
            security.revocation_replay()


if __name__ == "__main__":
    unittest.main()
