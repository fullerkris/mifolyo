"""Source-bound independent review controls; only inert Python children run.

All disk writes, including private-config stand-ins, are under this directory.
The config factory records/checks the production request for /tmp, then redirects
the allocation to this review directory. No Docker, SSH, native actor or daemon
connection is permitted. This is a reviewer probe, not product code.
"""
from contextlib import contextmanager
import ctypes
import errno
import hashlib
import json
import os
from pathlib import Path
import select
import selectors
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
SOURCE = HERE.parent / "source/tests/crawl-jobs-v2-observer"
sys.dont_write_bytecode = True
sys.path.insert(0, str(SOURCE))

import contracts as c
import docker_backend as db
import local_process as lp
import streams
from streams import Deadline, ProcessStream, WireBudget

PYTHON = [sys.executable, "-I", "-B", "-c"]
SLEEP = "import signal,time; signal.alarm(8); time.sleep(6)"
REAL_POPEN = subprocess.Popen
REAL_TEMP = tempfile.TemporaryDirectory
ACTOR = [*db.PREFIX, "container", "start", "--attach", "--interactive", "a" * 64]
OBSERVATIONS = {}
SANDBOX = None


def binding():
    return {"architecture": "x86_64", "case_id": "P1.pre.1.1", "invocation_sha256": "b" * 64,
            "container_id": "a" * 64, "image": "sha256:" + "c" * 64, "role": "target"}


def fd_count():
    return len(list(Path("/proc/self/fd").iterdir()))


def forbidden_spawn(*args, **kwargs):
    raise AssertionError("No subprocess authorized outside the inert-child helper")


@contextmanager
def children(replacement=None, pass_fds=()):
    owned, calls = [], []

    def spawn(argv, **kwargs):
        calls.append((list(argv), dict(kwargs)))
        if replacement is not None:
            argv = PYTHON + [replacement]
        if argv[:len(PYTHON)] != PYTHON:
            raise AssertionError("Only -I -B inert Python children may run")
        if pass_fds:
            kwargs["pass_fds"] = pass_fds
        child = REAL_POPEN(argv, **kwargs)
        owned.append(child)
        return child

    try:
        with patch.object(subprocess, "Popen", side_effect=spawn):
            yield owned, calls
    finally:
        for child in owned:
            if child.returncode is None:
                # Exclusively owned, waitable leader: never signal after reaping.
                try:
                    os.waitid(os.P_PID, child.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
                except ChildProcessError:
                    raise AssertionError("probe fallback lost child ownership")
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                child.wait(timeout=3)
            for pipe in (child.stdin, child.stdout, child.stderr):
                if pipe is not None:
                    pipe.close()


@contextmanager
def review_configs():
    made = []

    def directory(*args, **kwargs):
        assert kwargs.get("dir") == "/tmp", kwargs
        assert kwargs.get("prefix") == "obs1-docker-", kwargs
        kwargs["dir"] = str(SANDBOX)
        value = REAL_TEMP(*args, **kwargs)
        made.append(Path(value.name))
        assert value.name.startswith(str(SANDBOX) + "/")
        assert Path(value.name).stat().st_mode & 0o777 == 0o700
        return value

    with patch.object(tempfile, "TemporaryDirectory", side_effect=directory):
        yield made
    assert all(not path.exists() for path in made), made


@contextmanager
def subreaper():
    # Process-local harness setting only: adopt and reap our orphaned witness.
    libc = ctypes.CDLL(None, use_errno=True)
    old = ctypes.c_int()
    assert libc.prctl(37, ctypes.byref(old), 0, 0, 0) == 0
    assert libc.prctl(36, 1, 0, 0, 0) == 0
    try:
        yield
    finally:
        assert libc.prctl(36, old.value, 0, 0, 0) == 0


class IndependentControls(unittest.TestCase):
    def setUp(self):
        self.fds_before = fd_count()

    def tearDown(self):
        self.assertEqual(fd_count(), self.fds_before, "review control leaked file descriptors")

    def closed(self, process, selector=None):
        self.assertIsNotNone(process.returncode)
        for pipe in (process.stdin, process.stdout, process.stderr):
            if pipe is not None:
                self.assertTrue(pipe.closed)
        if selector is not None:
            self.assertIsNone(selector.get_map())

    def test_01_dual_pipe_exact_combined_cap_and_nonzero_status(self):
        half = db.MAX_COMMAND_BYTES // 2
        script = f'''import os,signal,threading
signal.alarm(8)
def output(fd, byte):
    left = {half}
    while left:
        count = os.write(fd, byte * min(left, 4096))
        left -= count
t = threading.Thread(target=output, args=(2,b"E")); t.start()
output(1,b"O"); t.join(); os._exit(73)
'''
        with children() as (owned, _):
            code, out, err = db.bounded_command(PYTHON + [script], Deadline(3), {})
            self.assertEqual((code, out, err), (73, b"O" * half, b"E" * half))
            self.closed(owned[0])
        OBSERVATIONS["dual_pipe_exact_cap"] = {"code": code, "stdout_bytes": len(out), "stderr_bytes": len(err)}

    def test_02_natural_signal_exit_status_not_replaced_by_cleanup_kill(self):
        with children() as (owned, _):
            result = db.bounded_command(PYTHON + ['import os,signal; os.write(1,b"before"); os.kill(os.getpid(),signal.SIGTERM)'], Deadline(3), {})
            self.assertEqual(result, (-signal.SIGTERM, b"before", b""))
            self.closed(owned[0])
        OBSERVATIONS["signal_exit_returncode"] = result[0]

    def test_03_intermittent_read_errors_do_not_drop_output(self):
        original = os.read
        raised = []

        def read(fd, size):
            # Popen itself uses read for its exec-error pipe with size 50000.
            if size == 4096 and len(raised) < 2:
                exc = (InterruptedError, BlockingIOError)[len(raised)]
                raised.append(exc.__name__)
                raise exc(errno.EAGAIN, "injected")
            return original(fd, size)

        with children() as (owned, _), patch.object(db.os, "read", side_effect=read):
            result = db.bounded_command(PYTHON + ['import os; os.write(1,b"kept"); os.write(2,b"also")'], Deadline(3), {})
            self.assertEqual(result, (0, b"kept", b"also"))
            self.closed(owned[0])
        self.assertEqual(raised, ["InterruptedError", "BlockingIOError"])

    def test_04_command_spawn_failure_closes_preallocated_selector(self):
        mux = selectors.DefaultSelector()
        with patch.object(db.selectors, "DefaultSelector", return_value=mux), \
                patch.object(subprocess, "Popen", side_effect=OSError(errno.ENOENT, "private-canary")), \
                self.assertRaises(OSError):
            db.bounded_command(PYTHON + ["pass"], Deadline(3), {})
        self.assertIsNone(mux.get_map())

    def test_05_second_command_selector_registration_failure(self):
        mux = selectors.DefaultSelector()
        original = mux.register
        seen = []

        def register(*args):
            seen.append(args)
            if len(seen) == 2:
                raise OSError(errno.EIO, "second registration")
            return original(*args)

        with patch.object(db.selectors, "DefaultSelector", return_value=mux), \
                patch.object(mux, "register", side_effect=register), children() as (owned, _), \
                self.assertRaisesRegex(OSError, "second registration"):
            db.bounded_command(PYTHON + [SLEEP], Deadline(3), {})
        self.assertEqual(len(seen), 2)
        self.closed(owned[0], mux)

    def test_06_final_stream_pipe_setup_failure(self):
        mux = selectors.DefaultSelector()
        original = os.set_blocking
        seen = []

        def nonblock(*args):
            seen.append(args)
            if len(seen) == 3:
                raise OSError(errno.EIO, "stdin setup")
            return original(*args)

        with patch.object(streams.selectors, "DefaultSelector", return_value=mux), \
                patch.object(streams.os, "set_blocking", side_effect=nonblock), children() as (owned, _), \
                self.assertRaisesRegex(OSError, "stdin setup"):
            ProcessStream(PYTHON + [SLEEP], binding(), WireBudget(), Deadline(3))
        self.assertEqual(len(seen), 3)
        self.closed(owned[0], mux)

    def test_07_pre_spawn_command_deadline_closes_selector(self):
        now = [0]
        deadline = Deadline(1, clock=lambda: now[0])
        mux = selectors.DefaultSelector()

        def selector():
            now[0] = 2
            return mux

        with patch.object(db.selectors, "DefaultSelector", side_effect=selector), \
                patch.object(subprocess, "Popen") as spawn, self.assertRaisesRegex(c.Invalid, "^DEADLINE$"):
            db.bounded_command(PYTHON + ["pass"], deadline, {})
        spawn.assert_not_called()
        self.assertIsNone(mux.get_map())

    def test_08_repeated_success_close_never_signals_reaped_pid(self):
        real_killpg = os.killpg
        with children() as (owned, _), patch.object(lp.os, "killpg", wraps=real_killpg) as killpg:
            stream = ProcessStream(PYTHON + [SLEEP], binding(), WireBudget(), Deadline(3))
            stream.close()
            stream.close()
            stream.close()
            self.assertEqual(killpg.call_count, 1)
            self.closed(owned[0], stream.selector)

    def test_09_group_signal_failure_is_sticky_after_real_reap(self):
        with children() as (owned, _):
            stream = ProcessStream(PYTHON + ["raise SystemExit(17)"], binding(), WireBudget(), Deadline(3))
            lp.wait_unreaped(owned[0], Deadline(3))
            with patch.object(lp.os, "killpg", side_effect=PermissionError(errno.EPERM, "private-canary")) as killpg:
                for _ in range(2):
                    with self.assertRaisesRegex(c.Invalid, "^LOCAL_PROCESS_CLEANUP$"):
                        stream.close()
                self.assertEqual(killpg.call_count, 1)
            self.assertEqual(owned[0].returncode, 17)
            self.closed(owned[0], stream.selector)
        OBSERVATIONS["sticky_signal_failure"] = True

    def test_10_wait_timeout_sticky_value_free_and_fds_closed(self):
        with children() as (owned, _):
            stream = ProcessStream(PYTHON + [SLEEP], binding(), WireBudget(), Deadline(3))
            child = owned[0]
            real_wait = child.wait
            with patch.object(child, "wait", side_effect=subprocess.TimeoutExpired(["private-canary"], 5)) as wait:
                for _ in range(2):
                    with self.assertRaisesRegex(c.Invalid, "^LOCAL_PROCESS_CLEANUP$"):
                        stream.close()
                wait.assert_called_once_with(timeout=5)
            self.assertTrue(all(pipe.closed for pipe in (child.stdin, child.stdout, child.stderr)))
            self.assertIsNone(stream.selector.get_map())
            # Injected wait error deliberately skipped the reap; the harness owns it.
            real_wait(timeout=3)
            self.assertEqual(child.returncode, -signal.SIGKILL)

    def test_11_raw_external_reap_refuses_stale_numeric_group_signal(self):
        with children() as (owned, _):
            child = subprocess.Popen(PYTHON + ["raise SystemExit(19)"], start_new_session=True,
                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            lp.wait_unreaped(child, Deadline(3))
            _, status = os.waitpid(child.pid, 0)
            self.assertIsNone(child.returncode)
            try:
                with patch.object(lp.os, "killpg") as killpg, \
                        self.assertRaisesRegex(c.Invalid, "^LOCAL_PROCESS_OWNERSHIP$"):
                    lp.kill_and_reap(child)
                killpg.assert_not_called()
            finally:
                child.returncode = os.waitstatus_to_exitcode(status)

    def test_12_real_orphan_witness_killed_without_changing_leader_status(self):
        reader, writer = os.pipe()
        grandchild = None
        script = f'''import os,signal,time
signal.alarm(8)
r,w=os.pipe()
pid=os.fork()
if pid==0:
    signal.alarm(5)
    os.close(r); os.close(1); os.close(2)
    os.write({writer}, b"witness"); os.write(w,b"R"); os.close(w)
    time.sleep(4); os._exit(0)
os.close(w); assert os.read(r,1)==b"R"; os.close(r)
os.write(1, str(pid).encode()); os.kill(os.getpid(),signal.SIGTERM)
'''
        with subreaper():
            try:
                with children(pass_fds=(writer,)) as (owned, _):
                    code, raw, err = db.bounded_command(PYTHON + [script], Deadline(3), {})
                    grandchild = int(raw)
                    os.close(writer)
                    writer = None
                    self.assertEqual((code, err), (-signal.SIGTERM, b""))
                    self.assertTrue(select.select([reader], [], [], 1)[0])
                    self.assertEqual(os.read(reader, 7), b"witness")
                    self.assertTrue(select.select([reader], [], [], 1)[0])
                    self.assertEqual(os.read(reader, 1), b"")
                    info = os.waitid(os.P_PID, grandchild, os.WEXITED | os.WNOWAIT)
                    self.assertEqual(info.si_status, signal.SIGKILL)
                    self.assertEqual(info.si_code, os.CLD_KILLED)
                    self.closed(owned[0])
                    OBSERVATIONS["orphan_witness"] = {"leader_returncode": code,
                        "witness_signal": info.si_status, "witness_pipe_eof": True,
                        "all_descendants_absence_proven": False}
            finally:
                if grandchild is not None:
                    info = os.waitid(os.P_PID, grandchild, os.WEXITED | os.WNOWAIT | os.WNOHANG)
                    if info is None:
                        os.kill(grandchild, signal.SIGKILL)
                    os.waitpid(grandchild, 0)
                if writer is not None:
                    os.close(writer)
                os.close(reader)

    def test_13_actor_config_is_retained_through_group_cleanup(self):
        real_cleanup = streams.kill_and_reap
        with review_configs() as dirs, patch.object(db, "verify_cli"), children(SLEEP) as (owned, calls):
            stream = db.DockerProcessStream(ACTOR, binding(), WireBudget(), Deadline(3))
            directory = dirs[0]

            def cleanup(process):
                self.assertTrue(directory.is_dir())
                self.assertEqual(list(directory.iterdir()), [])
                real_cleanup(process)
                self.assertTrue(directory.is_dir())

            with patch.object(streams, "kill_and_reap", side_effect=cleanup):
                stream.close()
            self.assertFalse(directory.exists())
            self.assertEqual(calls[0][1]["env"], db.clean_environment(str(directory)))
            self.assertEqual(calls[0][1]["cwd"], "/")
            self.closed(owned[0], stream.selector)
        OBSERVATIONS["config_allocation"] = {"requested_dir": "/tmp", "actual_dir_redirected_for_review": True,
            "mode": "0700", "present_until_reap": True, "removed_after_close": True}

    def test_14_actor_early_constructor_failures_remove_config(self):
        for failure in ("binding", "selector", "spawn"):
            with self.subTest(failure=failure), review_configs() as dirs, patch.object(db, "verify_cli"):
                if failure == "binding":
                    with self.assertRaises(c.Invalid):
                        db.DockerProcessStream(ACTOR, {}, WireBudget(), Deadline(3))
                elif failure == "selector":
                    with patch.object(streams.selectors, "DefaultSelector", side_effect=OSError(errno.ENFILE, "selector")), self.assertRaises(OSError):
                        db.DockerProcessStream(ACTOR, binding(), WireBudget(), Deadline(3))
                else:
                    with patch.object(subprocess, "Popen", side_effect=OSError(errno.EACCES, "spawn")), self.assertRaises(OSError):
                        db.DockerProcessStream(ACTOR, binding(), WireBudget(), Deadline(3))
                self.assertEqual(len(dirs), 1)
                self.assertFalse(dirs[0].exists())

    def test_15_actor_config_removed_on_postallocation_deadline(self):
        now = [0]
        deadline = Deadline(1, clock=lambda: now[0])
        with review_configs() as dirs, patch.object(db, "verify_cli"):
            factory = db.tempfile.TemporaryDirectory

            def expired(*args, **kwargs):
                result = factory(*args, **kwargs)
                now[0] = 2
                return result

            with patch.object(db.tempfile, "TemporaryDirectory", side_effect=expired), \
                    patch.object(subprocess, "Popen") as spawn, self.assertRaisesRegex(c.Invalid, "^DEADLINE$"):
                db.DockerProcessStream(ACTOR, binding(), WireBudget(), deadline)
            spawn.assert_not_called()
            self.assertFalse(dirs[0].exists())

    def test_16_simultaneous_actors_do_not_share_config_lifetimes(self):
        with review_configs() as dirs, patch.object(db, "verify_cli"), children(SLEEP):
            first = db.DockerProcessStream(ACTOR, binding(), WireBudget(), Deadline(3))
            second = None
            try:
                second = db.DockerProcessStream(ACTOR, binding(), WireBudget(), Deadline(3))
                self.assertNotEqual(dirs[0], dirs[1])
                second.close()
                self.assertTrue(dirs[0].is_dir())
                self.assertFalse(dirs[1].exists())
            finally:
                first.close()
                if second is not None:
                    second.close()

    def test_17_real_factory_setup_error_normalized_without_private_text(self):
        from test_runtime_admission import DockerAdmissionTests
        registry, _, backend, _, _ = DockerAdmissionTests().fixture()
        backend.stream_factory = db.DockerProcessStream
        ref = registry.entries["target"]["reference"]
        original = os.set_blocking
        seen = []

        def nonblock(*args):
            seen.append(args)
            if len(seen) == 3:
                raise OSError(errno.EIO, "private-factory-canary")
            return original(*args)

        with review_configs(), patch.object(db, "verify_cli"), children(SLEEP) as (owned, _), \
                patch.object(streams.os, "set_blocking", side_effect=nonblock):
            with self.assertRaisesRegex(c.Invalid, "^DOCKER_TRANSPORT$"):
                backend.open_actor(ref, registry.spec("target"), registry.snapshot(), WireBudget(), Deadline(3))
            self.assertEqual(backend._actor_streams, {})
            self.closed(owned[0])

    def test_18_exact_metadata_projection_has_no_cli_dependency(self):
        info = {field: "unused" for field in db.INFO_FIELDS.values()}
        info.update(ID="review-daemon", HTTPProxy="private-canary")
        version = {"Os": "linux", "Arch": "amd64", "Version": "29.0.0", "KernelVersion": "review-kernel", "Secret": "private-canary"}
        values = {"info": c.canonical(info), "version": c.canonical(version)}
        with patch.object(db, "verify_cli", side_effect=AssertionError("CLI inspection forbidden")), \
                patch.object(db, "read_engine_metadata", side_effect=lambda op, _: values[op]) as reader:
            self.assertEqual(db.command([*db.PREFIX, "info", "--format", "{{.ID}}"], Deadline(3)), (0, b"review-daemon", b""))
            code, raw, err = db.command([*db.PREFIX, "version", "--format", "{{json .Server}}"], Deadline(3))
            self.assertEqual((code, err), (0, b""))
            self.assertEqual(json.loads(raw), {key: version[key] for key in db.VERSION_FIELDS.values()})
            self.assertNotIn(b"private-canary", raw)
            self.assertEqual(reader.call_count, 2)

    def test_19_malformed_metadata_fail_closed_and_are_normalized(self):
        backend = object.__new__(db.DockerBackend)
        backend.runner, backend.prefix = db.command, list(db.PREFIX)
        for raw in (b"{}", b'{"ID":"a","ID":"b"}', b"[1]", b'{"x":NaN}'):
            with self.subTest(raw=raw), patch.object(db, "read_engine_metadata", return_value=raw), \
                    self.assertRaisesRegex(c.Invalid, "^DOCKER_TRANSPORT$"):
                backend._call(["info", "--format", "{{.ID}}"], Deadline(3))

    def test_20_clustered_help_flags_refused_by_closed_dispatch(self):
        admitted = []
        for flag in ("-hh", "-Dh", "-hD"):
            try:
                db.cli_arguments([*db.PREFIX, "container", "inspect", flag])
            except c.Invalid:
                pass
            else:
                admitted.append(flag)
        OBSERVATIONS["unexpected_help_flags_admitted"] = admitted
        self.assertEqual(admitted, [], "clustered short flags can set Docker help=true")

    def test_21_attached_endpoint_short_flags_refused_by_closed_dispatch(self):
        admitted = []
        for flag in ("-Hunix:///review-unused.sock", "-creview-unused"):
            try:
                db.cli_arguments([*db.PREFIX, "container", "inspect", "a" * 64, flag])
            except c.Invalid:
                pass
            else:
                admitted.append(flag)
        OBSERVATIONS["unexpected_endpoint_flag_spellings_admitted"] = admitted
        self.assertEqual(admitted, [], "attached short option values bypass the token blacklist")

    def test_22_repeated_mixed_failures_do_not_accumulate_descriptors(self):
        for i in range(6):
            with children() as (owned, _):
                script = "import os; os.write(1,b'x'*262145)" if i % 2 else "import os,time,signal; signal.alarm(8); os.close(1); os.close(2); time.sleep(6)"
                reason = "COMMAND_OUTPUT_BOUND" if i % 2 else "DEADLINE"
                with self.assertRaisesRegex(c.Invalid, "^" + reason + "$"):
                    db.bounded_command(PYTHON + [script], Deadline(1 if i % 2 else 0.1), {})
                self.closed(owned[0])

    def test_23_stream_finish_retains_explicit_no_remote_or_group_proof(self):
        with children() as (owned, _):
            stream = ProcessStream(PYTHON + ["pass"], binding(), WireBudget(), Deadline(3))
            stream.phase = "FINISHED"
            result = stream.finish_transport()
            self.assertEqual(result, {"stdout_closed": True, "extra_frames": False,
                "remote_exit_proven": False, "local_process_group_absence_proven": False})
            self.closed(owned[0], stream.selector)


class RecordedResult(unittest.TextTestResult):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.started = []
        self.passed = []

    def startTest(self, test):
        self.started.append(test.id())
        super().startTest(test)

    def addSuccess(self, test):
        self.passed.append(test.id())
        super().addSuccess(test)


def main():
    global SANDBOX
    mode = sys.argv[1]
    assert mode in ("022", "077")
    os.umask(int(mode, 8))
    SANDBOX = HERE / ("sandbox-" + mode)
    SANDBOX.mkdir(mode=0o700)
    started = time.monotonic()
    # Every test must explicitly opt into the audited Python-only spawn helper.
    with patch.object(subprocess, "Popen", side_effect=forbidden_spawn), \
            patch.object(db, "read_engine_metadata", side_effect=AssertionError("No daemon connections allowed")), \
            (HERE / ("independent-" + mode + ".log")).open("x") as log:
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(IndependentControls)
        result = unittest.TextTestRunner(stream=log, verbosity=2, resultclass=RecordedResult).run(suite)
    value = {"kind": "independent_read_only_correctness_probes", "umask": mode,
        "python": sys.version, "elapsed_seconds": round(time.monotonic() - started, 6),
        "status": "PASS" if result.wasSuccessful() else "FAIL", "tests_run": result.testsRun,
        "started": result.started, "passed": result.passed,
        "failures": [{"test": test.id(), "traceback": detail} for test, detail in result.failures],
        "errors": [{"test": test.id(), "traceback": detail} for test, detail in result.errors],
        "skipped": [{"test": test.id(), "reason": why} for test, why in result.skipped],
        "observations": OBSERVATIONS, "probe_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "source_files": {path.name: hashlib.sha256(path.read_bytes()).hexdigest()
            for path in (SOURCE / "docker_backend.py", SOURCE / "streams.py", SOURCE / "local_process.py")},
        "limits": ["No Docker, SSH, native actors, calibration or real daemon sockets invoked",
            "Temporary config requests verified as /tmp but allocation redirected inside review directory",
            "No proof of absent escaped descendants, all process groups, remote containers or native readiness"]}
    with (HERE / ("independent-" + mode + ".json")).open("x") as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write("\n")
    print(json.dumps({key: value[key] for key in ("status", "tests_run", "elapsed_seconds", "observations", "failures", "errors")}, indent=2))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
