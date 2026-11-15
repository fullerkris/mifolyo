"""Independent, inert-only probes against the frozen snapshot; no Docker execution.

TemporaryDirectory requests for /tmp are asserted, then routed into this review's
scratch directory to respect the write boundary. No bytecode/cache is written.
"""
from contextlib import contextmanager
import ctypes
import errno
import io
import json
import os
from pathlib import Path
import select
import signal
import stat
import subprocess
import sys
import tempfile
import time
import traceback
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
HERE = Path(__file__).resolve().parent
SOURCE = HERE.parent / "source/tests/crawl-jobs-v2-observer"
sys.path.insert(0, str(SOURCE))
import contracts as c
import docker_backend as db
import engine_metadata as em
import local_process as lp
import owned_resources as owned
import streams
from streams import Deadline, ProcessStream, WireBudget

SCRATCH = HERE / "scratch"
SCRATCH.mkdir(exist_ok=True)
DETAILS = {}
PYTHON = [sys.executable, "-I", "-B", "-c"]
REAL_POPEN = subprocess.Popen
REAL_TEMP = tempfile.TemporaryDirectory
CANARY = "SECURITY-REVIEW-SYNTHETIC-PRIVATE-CANARY"


def bound():
    return {"architecture": "x86_64", "case_id": "P1.pre.1.1", "invocation_sha256": "a" * 64,
            "container_id": "b" * 64, "image": "sha256:" + "c" * 64, "role": "target"}


def scope():
    return owned.scope("P1.pre.1.1", "1" * 32,
        {r: "sha256:" + str(i + 1) * 64 for i, r in enumerate(owned.ACTORS)},
        {r: c.sha(c.canonical([])) for r in owned.ACTORS}, c.sha(b"review-daemon"), "x86_64")


def backend(**kwargs):
    return db.DockerBackend(scope(), {r: "/unused/" + r for r in owned.ACTORS}, **kwargs)


def metadata_values():
    return {
        "info": {"ID": "review-daemon", "OSType": "linux", "Architecture": "x86_64", "ServerVersion": "29.0.0",
                 "KernelVersion": "synthetic-kernel", "CgroupVersion": "2", "CgroupDriver": "systemd",
                 "SecurityOptions": ["name=seccomp,profile=builtin", "name=cgroupns"],
                 "MemoryLimit": True, "SwapLimit": True, "PidsLimit": True, "CpuCfsPeriod": True, "CpuCfsQuota": True},
        "version": {"Os": "linux", "Arch": "amd64", "Version": "29.0.0", "KernelVersion": "synthetic-kernel"},
    }


@contextmanager
def configs():
    names, requests = [], []
    def factory(*args, **kwargs):
        assert not args and kwargs == {"prefix": "obs1-docker-", "dir": "/tmp"}, kwargs
        requests.append(dict(kwargs))
        item = REAL_TEMP(prefix="review-config-", dir=SCRATCH)
        names.append(item.name)
        return item
    try:
        with patch.object(db.tempfile, "TemporaryDirectory", side_effect=factory):
            yield names, requests
    finally:
        assert not any(Path(p).exists() for p in names), "configuration leaked"


@contextmanager
def children(replacement=None, pass_fds=()):
    processes, calls = [], []
    def spawn(argv, **kwargs):
        calls.append((list(argv), dict(kwargs)))
        actual = PYTHON + [replacement] if replacement is not None else argv
        assert actual[:4] == PYTHON, "non-inert process execution prohibited"
        if pass_fds:
            kwargs["pass_fds"] = pass_fds
        p = REAL_POPEN(actual, **kwargs)
        processes.append(p)
        return p
    try:
        with patch.object(db.subprocess, "Popen", side_effect=spawn):
            yield processes, calls
    finally:
        for p in processes:
            if p.returncode is None:
                # Reserve the known child identity; never signal a reaped PID.
                try:
                    os.waitid(os.P_PID, p.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
                except ChildProcessError:
                    pass
                else:
                    try:
                        os.killpg(p.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                p.wait(timeout=3)
            for fd in (p.stdin, p.stdout, p.stderr):
                if fd is not None:
                    fd.close()


def corrected_guard(argv, *, actor=False):
    """Review-only proposed addition, NOT a change to product source."""
    db.cli_arguments(argv, actor=actor)
    args = argv[3:]
    flag_args = args
    # The only emitted short tokens are positional oracle Python arguments.
    if (not actor and args[:2] == ["container", "create"] and len(args) >= 6
            and args[-3:] == ["-I", "-B", "/oracle/worker.py"]):
        c.require(args[-4].startswith("sha256:"), "DOCKER_ARGV")
        c.digest(args[-4][7:])
        flag_args = args[:-3]
    c.require(not any(a.startswith("-") and not a.startswith("--") for a in flag_args), "DOCKER_ARGV")


class Wire:
    def __init__(self, raw):
        self.raw, self.sent, self.connected, self.closed = raw, [], [], False
    def __enter__(self):
        return self
    def __exit__(self, *_):
        self.closed = True
    def settimeout(self, value):
        assert 0 < value <= 3
    def connect(self, path):
        assert path == "/var/run/docker.sock"
        self.connected.append(path)
    def sendall(self, value):
        self.sent.append(value)
    def recv(self, count):
        part, self.raw = self.raw[:min(count, 17)], self.raw[min(count, 17):]
        return part


def response(body):
    return b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: " + str(len(body)).encode() + b"\r\n\r\n" + body


class SecurityChecks(unittest.TestCase):
    def test_01_long_and_actor_rejections_precede_all_side_effects(self):
        denied = [[], ["info"], ["buildx", "version"], ["container", "start", "x"],
                  ["container", "inspect", "--help=true"], ["container", "inspect", "__completeNoDesc"],
                  ["volume", "create", "--config=/hostile"], ["container", "inspect", "--context=hostile"],
                  ["container", "inspect", "x\0y"], ["container", "inspect", "x" * 4097]]
        with patch.object(db, "verify_cli", side_effect=AssertionError("binary touched")), \
                patch.object(db.tempfile, "TemporaryDirectory", side_effect=AssertionError("tempdir touched")), \
                patch.object(db.subprocess, "Popen", side_effect=AssertionError("spawned")):
            for tail in denied:
                with self.subTest(tail=tail[:3]), self.assertRaises(c.Invalid):
                    db.command([*db.PREFIX, *tail], Deadline(1))
            for tail in (["container", "start", "-ai", "a" * 64],
                         ["container", "start", "--attach", "--interactive", "-hh"],
                         ["container", "start", "--attach", "--interactive", "a" * 64, "-hh"]):
                with self.assertRaises(c.Invalid):
                    db.DockerProcessStream([*db.PREFIX, *tail], bound(), WireBudget(), Deadline(1))
        DETAILS["pre_side_effect_rejections"] = len(denied) + 3

    def test_02_regression_clustered_help_must_be_rejected(self):
        observations = []
        for flag in ("-hh", "-hhh", "-hh=true", "-sh"):
            argv = [*db.PREFIX, "container", "inspect", flag]
            dispatched = []
            with patch.object(db, "verify_cli"), configs(), \
                    patch.object(db, "bounded_command", side_effect=lambda a, d, e: (dispatched.append(a) or (0, b"", b""))):
                try:
                    db.command(argv, Deadline(1))
                    rejected = False
                except c.Invalid:
                    rejected = True
            observations.append({"flag": flag, "rejected": rejected, "reached_transport": bool(dispatched)})
        DETAILS["clustered_help"] = observations
        self.assertTrue(all(v["rejected"] for v in observations), "clustered help reaches transport; Docker v29 help discovers plugins")

    def test_03_proposed_guard_retains_oracle_tail_and_closed_commands(self):
        safe = [["container", "inspect", "a" * 64], ["volume", "create", "--driver", "local", "obs1-test"],
                ["container", "ls", "--all", "--format", "{{.ID}}"],
                ["container", "create", "--entrypoint", "python3", "sha256:" + "a" * 64, "-I", "-B", "/oracle/worker.py"]]
        for tail in safe:
            corrected_guard([*db.PREFIX, *tail])
        corrected_guard([*db.PREFIX, "container", "start", "--attach", "--interactive", "b" * 64], actor=True)
        for flag in ("-hh", "-hhh", "-hh=true", "-sh", "-cother", "-Hunix:///alternate", "-I", "-B"):
            with self.assertRaises(c.Invalid):
                corrected_guard([*db.PREFIX, "container", "inspect", flag])
        DETAILS["review_only_remediation_control"] = {"safe_forms": len(safe) + 1, "rejected_forms": 8}

    def test_04_metadata_full_wire_projection_and_platform_negative(self):
        bodies = metadata_values()
        for body in bodies.values():
            body["HTTPProxy"] = CANARY
            body["UnusedPrivate"] = {"credentials": CANARY}
        wires = []
        def factory(family, kind):
            self.assertEqual((family, kind), (em.socket.AF_UNIX, em.socket.SOCK_STREAM))
            op = "info" if len(wires) % 2 == 0 else "version"
            wire = Wire(response(c.canonical(bodies[op])))
            wires.append(wire)
            return wire
        with patch.object(em.socket, "socket", side_effect=factory), \
                patch.object(db.subprocess, "Popen", side_effect=AssertionError("spawned")), \
                patch.object(db, "verify_cli", side_effect=AssertionError("CLI touched")):
            valid = backend().verify_platform(Deadline(3))
            self.assertFalse(valid["runtime_feasibility_proven"])
            self.assertNotIn(CANARY, json.dumps(valid))
            bodies["version"]["Arch"] = "arm64"
            with self.assertRaisesRegex(c.Invalid, "^PLATFORM$"):
                backend().verify_platform(Deadline(3))
        self.assertEqual(len(wires), 4)
        for i, wire in enumerate(wires):
            op = "info" if i % 2 == 0 else "version"
            self.assertTrue(wire.closed)
            self.assertEqual(wire.sent, [("GET /" + op + " HTTP/1.1\r\nHost: localhost\r\nAccept: application/json\r\nConnection: close\r\n\r\n").encode()])
        DETAILS["metadata_wire"] = {"fake_connections": 4, "real_connections": 0, "x64_pass_arm64_rejected": True}

    def test_05_metadata_malformed_and_private_errors_are_redacted(self):
        good = c.canonical(metadata_values()["info"])
        bad = [response(good[:-2] + b',"ID":"duplicate"}\n'), response(b"[" * 1500),
               response(b'{"ID":"' + CANARY.encode() + b'","x":NaN}'),
               response(b"\xff"), response(b"{}"),
               b"HTTP/1.1 302 " + CANARY.encode() + b"\r\n\r\n",
               response(good).replace(b"Content-Length:", b"Transfer-Encoding: chunked\r\nContent-Length:"),
               response(good) + CANARY.encode()]
        for raw in bad:
            wire = Wire(raw)
            with patch.object(em.socket, "socket", return_value=wire), \
                    patch.object(db.subprocess, "Popen", side_effect=AssertionError("spawned")):
                with self.assertRaisesRegex(c.Invalid, "^DOCKER_TRANSPORT$") as result:
                    backend()._call(["info", "--format", "{{.ID}}"], Deadline(3))
                self.assertNotIn(CANARY, str(result.exception))
                self.assertTrue(wire.closed)
        for error in (OSError(errno.EIO, CANARY), subprocess.TimeoutExpired([CANARY], 1), RuntimeError(CANARY)):
            b = backend(runner=lambda *args: (_ for _ in ()).throw(error))
            try:
                b._call(["container", "inspect", "a" * 64], Deadline(1))
            except c.Invalid as exc:
                self.assertEqual(str(exc), "DOCKER_TRANSPORT")
                self.assertNotIn(CANARY, "".join(traceback.format_exception(exc)))
            else:
                self.fail("transport failure accepted")
        DETAILS["redacted_negatives"] = {"wire": len(bad), "exception_classes": 3}

    def test_06_backend_selector_and_authorizer_prevent_argv_injection(self):
        b = backend()
        with patch.object(b, "runner", side_effect=AssertionError("runner reached")):
            for kind in ("container", "volume", "image"):
                for selector in ("-hh", "--help", "a" * 64 + "\n-hh"):
                    with self.assertRaises(c.Invalid):
                        b.inspect(kind, selector, Deadline(1))
            with self.assertRaisesRegex(c.Invalid, "^EXECUTION_NOT_APPROVED$"):
                b.create_volume(owned.volume_spec(b.scope, "control"), Deadline(1))
            with self.assertRaisesRegex(c.Invalid, "^EXECUTION_NOT_APPROVED$"):
                b.create_container(owned.container_spec(b.scope, "target"), Deadline(1))
        DETAILS["typed_backend_injection"] = "9 malformed selectors and 2 unauthorized mutations rejected before runner"

    def test_07_concurrent_private_config_environment_and_spawn_failure(self):
        actor = [*db.PREFIX, "container", "start", "--attach", "--interactive", "b" * 64]
        hostile = {key: CANARY for key in ("HOME", "DOCKER_CONFIG", "TMPDIR", "TEMP", "TMP", "PATH", "LD_PRELOAD", "LD_AUDIT",
            "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_API_VERSION", "HTTP_PROXY", "http_proxy", "BASH_ENV", "GODEBUG", "OTEL_RESOURCE_ATTRIBUTES")}
        with patch.dict(os.environ, hostile), patch.object(db, "verify_cli"), configs() as (names, requests), \
                children('import sys; sys.stdin.read()') as (ps, calls):
            first = db.DockerProcessStream(actor, bound(), WireBudget(), Deadline(3))
            second = db.DockerProcessStream(actor, bound(), WireBudget(), Deadline(3))
            try:
                self.assertEqual(len(set(names)), 2)
                for argv, kw in calls:
                    cfg = kw["env"]["HOME"]
                    self.assertEqual(cfg, kw["env"]["DOCKER_CONFIG"])
                    self.assertEqual(kw["env"], db.clean_environment(cfg))
                    self.assertEqual(stat.S_IMODE(os.stat(cfg).st_mode), 0o700)
                    self.assertEqual(os.listdir(cfg), [])
                    self.assertEqual(kw["cwd"], "/")
                    self.assertTrue(kw["close_fds"] and kw["start_new_session"])
                    self.assertNotIn(CANARY, repr(kw))
                    self.assertEqual(argv[3:5], ["--config", cfg])
                first.close()
                self.assertFalse(Path(names[0]).exists())
                self.assertTrue(Path(names[1]).is_dir())
            finally:
                first.close()
                second.close()
        with patch.object(db, "verify_cli"), configs(), \
                patch.object(db.subprocess, "Popen", side_effect=OSError(errno.ENOENT, CANARY)):
            with self.assertRaises(c.Invalid) as result:
                backend()._call(["container", "inspect", "a" * 64], Deadline(1))
            self.assertEqual(str(result.exception), "DOCKER_TRANSPORT")
        DETAILS["private_config"] = {"simultaneous_unique_configs": 2, "explicit_requested_dir": "/tmp", "physical_probe_dir": str(SCRATCH)}

    def test_08_binary_admission_matrix_without_opening_executables(self):
        good_file = SimpleNamespace(st_mode=stat.S_IFREG | 0o755, st_uid=0)
        good_dir = SimpleNamespace(st_mode=stat.S_IFDIR | 0o755, st_uid=0)
        class File(io.BytesIO):
            def fileno(self):
                return 987654
        matrix = [(good_file, b"\x7fELF", True),
                  (SimpleNamespace(st_mode=stat.S_IFREG | 0o777, st_uid=0), b"\x7fELF", False),
                  (SimpleNamespace(st_mode=stat.S_IFREG | 0o4755, st_uid=0), b"\x7fELF", False),
                  (SimpleNamespace(st_mode=stat.S_IFREG | 0o2755, st_uid=0), b"\x7fELF", False),
                  (SimpleNamespace(st_mode=stat.S_IFREG | 0o755, st_uid=1000), b"\x7fELF", False),
                  (SimpleNamespace(st_mode=stat.S_IFREG | 0o644, st_uid=0), b"\x7fELF", False),
                  (SimpleNamespace(st_mode=stat.S_IFIFO | 0o755, st_uid=0), b"\x7fELF", False),
                  (good_file, b"#!/b", False)]
        for info, magic, accepted in matrix:
            with patch.object(db.os, "lstat", return_value=good_dir), patch.object(db.os, "open", return_value=987654) as opened, \
                    patch.object(db.os, "fdopen", return_value=File(magic)), patch.object(db.os, "fstat", return_value=info):
                if accepted:
                    db.verify_cli()
                else:
                    with self.assertRaisesRegex(c.Invalid, "^DOCKER_BINARY$"):
                        db.verify_cli()
                self.assertEqual(opened.call_args.args[0], "/usr/bin/docker")
                self.assertTrue(opened.call_args.args[1] & os.O_NOFOLLOW)
        for mode in (stat.S_IFLNK | 0o755, stat.S_IFDIR | 0o777):
            with patch.object(db.os, "lstat", return_value=SimpleNamespace(st_mode=mode, st_uid=0)), \
                    patch.object(db.os, "open", side_effect=AssertionError("open before directory admission")):
                with self.assertRaises(c.Invalid):
                    db.verify_cli()
        DETAILS["binary_matrix"] = {"synthetic_cases": len(matrix) + 2, "installed_binary_executed": False}

    def test_09_wnowait_and_signal_before_reap_order(self):
        with children() as (ps, _):
            p = subprocess.Popen(PYTHON + ["import os; os._exit(23)"], start_new_session=True)
            lp.wait_unreaped(p, Deadline(2))
            observations = [lp.observe_exit(p) for _ in range(32)]
            self.assertTrue(all(v.si_pid == p.pid and v.si_status == 23 for v in observations))
            self.assertIsNone(p.returncode)
            events, real_signal, real_wait = [], os.killpg, p.wait
            def group(pid, sig):
                self.assertIsNone(p.returncode)
                self.assertIsNotNone(lp.observe_exit(p))
                events.append("signal")
                real_signal(pid, sig)
            def wait(*args, **kwargs):
                events.append("reap")
                return real_wait(*args, **kwargs)
            with patch.object(lp.os, "killpg", side_effect=group), patch.object(p, "wait", side_effect=wait):
                lp.kill_and_reap(p)
            self.assertEqual((events, p.returncode), (["signal", "reap"], 23))
            with patch.object(lp.os, "killpg") as sig, self.assertRaisesRegex(c.Invalid, "^LOCAL_PROCESS_OWNERSHIP$"):
                lp.kill_and_reap(p)
            sig.assert_not_called()
        DETAILS["ownership_order"] = {"nonreaping_observations": 32, "events": events, "reaped_pid_not_signalled": True}

    def descendant(self, *, stream_mode=False, skip_signal=False, escape_group=False):
        reader, writer = os.pipe()
        child_pid = None
        script = f'''import os,time
r,w=os.pipe()
pid=os.fork()
if pid==0:
    os.close(r)
    if {escape_group!r}: os.setpgid(0,0)
    os.close(1); os.close(2)
    os.write({writer}, (str(os.getpid())+","+str(os.getpgrp())+","+str(os.getsid(0))+"\\n").encode())
    os.write(w,b"R"); os.close(w)
    time.sleep(5)
    os._exit(0)
os.close(w); os.read(r,1); os.close(r)
os._exit(0)
'''
        try:
            with children(pass_fds=(writer,)) as (ps, _):
                original = lp.os.killpg
                with patch.object(lp.os, "killpg", side_effect=(lambda *_: None) if skip_signal else original):
                    if stream_mode:
                        s = ProcessStream(PYTHON + [script], bound(), WireBudget(), Deadline(2))
                        s.phase = "FINISHED"
                        flags = s.finish_transport()
                        self.assertFalse(flags["remote_exit_proven"] or flags["local_process_group_absence_proven"])
                    else:
                        self.assertEqual(db.bounded_command(PYTHON + [script], Deadline(2), {}), (0, b"", b""))
                os.close(writer)
                writer = None
                self.assertTrue(select.select([reader], [], [], 1)[0])
                raw = os.read(reader, 256)
                child_pid, pgid, sid = map(int, raw.strip().split(b","))
                terminated = bool(select.select([reader], [], [], 0.15)[0])
                if terminated:
                    self.assertEqual(os.read(reader, 1), b"")
                self.assertEqual(sid, ps[0].pid)
                self.assertEqual(pgid, child_pid if escape_group else ps[0].pid)
                return {"terminated_by_adapter": terminated, "same_session": sid == ps[0].pid,
                        "same_group": pgid == ps[0].pid, "signal_disabled_control": skip_signal}
        finally:
            if writer is not None:
                os.close(writer)
            os.close(reader)
            if child_pid is not None:
                # This probe process is a subreaper: child remains reserved until
                # this wait. pidfd cleanup also covers intentional negative controls.
                fd = os.pidfd_open(child_pid)
                try:
                    signal.pidfd_send_signal(fd, signal.SIGKILL)
                finally:
                    os.close(fd)
                os.waitpid(child_pid, 0)

    def test_10_descendant_cleanup_and_signal_disabled_negative_control(self):
        values = [self.descendant(), self.descendant(stream_mode=True), self.descendant(skip_signal=True)]
        self.assertEqual([v["terminated_by_adapter"] for v in values], [True, True, False])
        DETAILS["same_group_descendants"] = values

    def test_11_group_escape_is_not_misreported_as_absence(self):
        value = self.descendant(stream_mode=True, escape_group=True)
        self.assertFalse(value["terminated_by_adapter"])
        DETAILS["documented_group_limit"] = value

    def test_12_cleanup_permission_failure_is_sticky_and_closes_fds(self):
        with children() as (ps, _):
            s = ProcessStream(PYTHON + ["import time; time.sleep(0.05)"], bound(), WireBudget(), Deadline(2))
            with patch.object(lp.os, "killpg", side_effect=PermissionError(errno.EPERM, CANARY)) as kill:
                with self.assertRaisesRegex(c.Invalid, "^LOCAL_PROCESS_CLEANUP$"):
                    s.close()
                with self.assertRaisesRegex(c.Invalid, "^LOCAL_PROCESS_CLEANUP$"):
                    s.close()
                self.assertEqual(kill.call_count, 1)
            self.assertTrue(all(fd.closed for fd in (ps[0].stdin, ps[0].stdout, ps[0].stderr)))
            self.assertIsNone(s.selector.get_map())
            self.assertTrue(s._cleanup_failed)
        DETAILS["sticky_cleanup"] = "signal-denied then natural leader exit still fails on first and repeated close"

    def test_13_reap_timeout_and_lost_ownership_are_redacted(self):
        for scenario in ("reap-timeout", "lost-ownership"):
            with children() as (ps, _):
                s = ProcessStream(PYTHON + ["import time; time.sleep(3)"], bound(), WireBudget(), Deadline(1))
                target = patch.object(ps[0], "wait", side_effect=subprocess.TimeoutExpired([CANARY], 5)) if scenario == "reap-timeout" else \
                         patch.object(lp.os, "waitid", side_effect=ChildProcessError(errno.ECHILD, CANARY))
                with target:
                    with self.assertRaises(c.Invalid) as failure:
                        s.close()
                self.assertIn(str(failure.exception), ("LOCAL_PROCESS_CLEANUP", "LOCAL_PROCESS_OWNERSHIP"))
                self.assertNotIn(CANARY, str(failure.exception))
                self.assertTrue(all(fd.closed for fd in (ps[0].stdin, ps[0].stdout, ps[0].stderr)))
                with self.assertRaisesRegex(c.Invalid, "^LOCAL_PROCESS_CLEANUP$"):
                    s.close()
        DETAILS["cleanup_faults"] = ["reap timeout redacted", "lost ownership not signalled", "sticky subsequent close"]

    def test_14_byte_limit_exactness_and_descriptor_stability(self):
        before = len(os.listdir("/proc/self/fd"))
        for n, accepted in ((db.MAX_COMMAND_BYTES, True), (db.MAX_COMMAND_BYTES + 1, False)):
            with children() as (ps, _):
                script = f'import os; os.write(1,b"x"*{n // 2}); os.write(2,b"y"*{n - n // 2})'
                if accepted:
                    code, out, err = db.bounded_command(PYTHON + [script], Deadline(2), {})
                    self.assertEqual((code, len(out) + len(err)), (0, n))
                else:
                    with self.assertRaisesRegex(c.Invalid, "^COMMAND_OUTPUT_BOUND$"):
                        db.bounded_command(PYTHON + [script], Deadline(2), {})
                self.assertTrue(ps[0].stdout.closed and ps[0].stderr.closed)
        for _ in range(12):
            with children():
                self.assertEqual(db.bounded_command(PYTHON + ["pass"], Deadline(2), {}), (0, b"", b""))
        after = len(os.listdir("/proc/self/fd"))
        self.assertEqual(before, after)
        DETAILS["fd_and_byte_limits"] = {"before_fds": before, "after_fds": after, "limit": db.MAX_COMMAND_BYTES, "extra_byte_rejected": True}

    def test_15_selector_interrupt_kills_and_reaps_before_redacted_return(self):
        selector = db.selectors.DefaultSelector()
        with patch.object(db, "verify_cli"), configs(), children('import time; time.sleep(3)') as (ps, _), \
                patch.object(db.selectors, "DefaultSelector", return_value=selector), \
                patch.object(selector, "select", side_effect=OSError(errno.EIO, CANARY)):
            with self.assertRaisesRegex(c.Invalid, "^DOCKER_TRANSPORT$"):
                backend()._call(["container", "inspect", "a" * 64], Deadline(1))
            self.assertEqual(ps[0].returncode, -signal.SIGKILL)
            self.assertTrue(ps[0].stdout.closed and ps[0].stderr.closed)
            self.assertIsNone(selector.get_map())
        DETAILS["selector_failure"] = "child killed/reaped, pipes+selector+config closed, redacted"


def main():
    # Linux process-local adoption only, so even deliberately surviving inert
    # descendants can be killed and reaped; no host or remote policy changes.
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.prctl(36, 1, 0, 0, 0) != 0:  # PR_SET_CHILD_SUBREAPER
        raise OSError(ctypes.get_errno(), "probe subreaper unavailable")
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(SecurityChecks)
    with (HERE / "security-probes.log").open("x") as log:
        result = unittest.TextTestRunner(stream=log, verbosity=2).run(suite)
    report = {"kind": "independent_inert_security_probes", "tests_run": result.testsRun,
              "failures": [{"test": t.id(), "traceback": text} for t, text in result.failures],
              "errors": [{"test": t.id(), "traceback": text} for t, text in result.errors],
              "details": DETAILS, "python": sys.version,
              "source_snapshot": str(SOURCE), "docker_executed": False, "native_actors_executed": False,
              "remote_connections": 0, "tempfiles_redirected_to_review": True,
              "process_local_subreaper": True}
    with (HERE / "security-probes.json").open("x") as handle:
        json.dump(report, handle, indent=2, sort_keys=True)
        handle.write("\n")
    print(json.dumps({k: v for k, v in report.items() if k != "details"}, indent=2))
    print((HERE / "security-probes.log").read_text())
    sys.exit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
