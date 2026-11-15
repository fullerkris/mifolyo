"""Live transport boundary controls using inert Python children, never Docker."""
from contextlib import contextmanager
import errno
import json
import os
from pathlib import Path
import select
import signal
import subprocess
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import contracts as c
import docker_backend as db
import local_process
from streams import Deadline, ProcessStream, WireBudget
from test_host_preflight import FixtureProbe
from test_streams import binding
from test_owned_resources import DAEMON, fixture_scope
from owned_resources import ACTORS, container_spec
from profiles import profile

PYTHON = [sys.executable, "-I", "-B", "-c"]


@contextmanager
def children(script=None, *, inherited=()):
    owned, calls = [], []
    spawn = subprocess.Popen
    def start(argv, **kwargs):
        calls.append((list(argv), dict(kwargs)))
        if inherited:
            kwargs["pass_fds"] = inherited
        child = spawn(PYTHON + [script] if script is not None else argv, **kwargs)
        owned.append(child)
        return child
    try:
        with patch.object(db.subprocess, "Popen", side_effect=start):
            yield owned, calls
    finally:
        for child in owned:
            # Tests also clean up the intentionally broken implementation when
            # run as regression controls. Never signal after releasing its PID.
            if child.returncode is None:
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                child.wait(timeout=3)
            for pipe in (child.stdin, child.stdout, child.stderr):
                if pipe is not None:
                    pipe.close()


class ConfigurationTests(unittest.TestCase):
    def test_metadata_never_spawns_cli_or_reads_ambient_configuration(self):
        fixture = FixtureProbe()
        values = {"info": {field: fixture.info[key] for key, field in db.INFO_FIELDS.items()},
                  "version": {field: fixture.server[key] for key, field in db.VERSION_FIELDS.items()}}
        # The private proxy body must not survive selected-field projection.
        values["info"]["HTTPProxy"] = "private-proxy-credential"
        with patch.dict(os.environ, {"DOCKER_CONFIG": "/hostile", "HOME": "/hostile", "DOCKER_HOST": "ssh://hostile"}), \
                patch.object(db, "read_engine_metadata", side_effect=lambda op, _: c.canonical(values[op])) as reader, \
                patch.object(db.subprocess, "Popen", side_effect=AssertionError("plugin spawned")), \
                patch.object(db, "verify_cli", side_effect=AssertionError("CLI touched")):
            self.assertEqual(db.command([*db.PREFIX, "info", "--format", "{{.ID}}"], Deadline(1)), (0, fixture.info["id"].encode(), b""))
            code, raw, err = db.command([*db.PREFIX, "version", "--format", "{{json .Server}}"], Deadline(1))
            self.assertEqual((code, err), (0, b""))
            self.assertEqual(json.loads(raw)["Arch"], fixture.server["architecture"])
            self.assertNotIn(b"private", raw)
            self.assertEqual([call.args[0] for call in reader.call_args_list], ["info", "version"])

    def test_private_config_fixed_binary_cwd_and_environment_reach_real_child(self):
        script = 'import json,os; print(json.dumps({"env":dict(os.environ),"cwd":os.getcwd(),"config":os.listdir(os.environ["DOCKER_CONFIG"]),"mode":os.stat(os.environ["DOCKER_CONFIG"]).st_mode & 511}))'
        hostile = {key: "private-canary" for key in ("DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS", "DOCKER_TLS_VERIFY",
            "DOCKER_CONFIG", "DOCKER_CERT_PATH", "DOCKER_API_VERSION", "DOCKER_CUSTOM_HEADERS", "DOCKER_CLI_PLUGIN_EXTRA_DIRS",
            "DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "SSL_CERT_FILE",
            "LD_PRELOAD", "LD_LIBRARY_PATH", "PYTHONPATH", "GODEBUG", "OTEL_EXPORTER_OTLP_ENDPOINT", "SSH_AUTH_SOCK", "HOME", "TMPDIR", "PATH")}
        with patch.dict(os.environ, hostile), patch.object(db, "verify_cli"), children(script) as (owned, calls):
            code, raw, err = db.command([*db.PREFIX, "container", "inspect", "a" * 64], Deadline(3))
            self.assertEqual((code, err), (0, b""))
            value = json.loads(raw)
            self.assertEqual((value["cwd"], value["config"], value["mode"]), ("/", [], 0o700))
            config = value["env"]["DOCKER_CONFIG"]
            self.assertEqual(value["env"], db.clean_environment(config))
            self.assertNotIn("private-canary", raw.decode())
            self.assertFalse(Path(config).exists())
            argv, options = calls[0]
            self.assertEqual(argv, [*db.PREFIX, "--config", config, "container", "inspect", "a" * 64])
            self.assertTrue(options["start_new_session"] and options["close_fds"])
            self.assertEqual(owned[0].returncode, 0)

    def test_actor_owns_configuration_until_close_and_uses_same_isolation(self):
        with patch.dict(os.environ, {"DOCKER_CONFIG": "/hostile", "HOME": "/hostile"}), \
                patch.object(db, "verify_cli"), children('import sys; sys.stdin.read()') as (owned, calls):
            stream = db.DockerProcessStream([*db.PREFIX, "container", "start", "--attach", "--interactive", "a" * 64],
                binding(), WireBudget(), Deadline(2))
            try:
                config = calls[0][1]["env"]["DOCKER_CONFIG"]
                self.assertTrue(Path(config).is_dir())
                self.assertEqual(list(Path(config).iterdir()), [])
                self.assertEqual(calls[0][1]["env"], db.clean_environment(config))
                self.assertEqual(calls[0][1]["cwd"], "/")
                self.assertIn("--config", calls[0][0])
            finally:
                stream.close()
            self.assertFalse(Path(config).exists())
            self.assertIsNotNone(owned[0].returncode)
            stream.close()

    def test_plugin_help_completion_and_endpoint_overrides_refuse_before_spawn(self):
        for args in (["info"], ["version"], ["buildx", "version"], ["container"],
                     ["container", "inspect", "--help"], ["container", "inspect", "__complete"],
                     ["container", "inspect", "__completeNoDesc"], ["container", "inspect", "--config=/hostile"],
                     ["container", "inspect", "--host=ssh://hostile"], ["container", "inspect", "--context=hostile"]):
            with self.subTest(args=args), patch.object(db.subprocess, "Popen") as spawn, \
                    self.assertRaisesRegex(c.Invalid, "DOCKER_ARGV"):
                db.command([*db.PREFIX, *args], Deadline(1))
            spawn.assert_not_called()
        with patch.object(db.subprocess, "Popen") as spawn, self.assertRaisesRegex(c.Invalid, "DOCKER_ARGV"):
            db.command(["docker", *db.PREFIX[1:], "container", "inspect", "a" * 64], Deadline(1))
        spawn.assert_not_called()

    def test_actor_setup_failure_removes_configuration_and_reaps_child(self):
        with patch.object(db, "verify_cli"), children('import time; time.sleep(20)') as (owned, calls), \
                patch("streams.os.set_blocking", side_effect=OSError(errno.EIO, "private-canary")), self.assertRaises(OSError):
            db.DockerProcessStream([*db.PREFIX, "container", "start", "--attach", "--interactive", "a" * 64],
                binding(), WireBudget(), Deadline(2))
        self.assertFalse(Path(calls[0][1]["env"]["DOCKER_CONFIG"]).exists())
        self.assertIsNotNone(owned[0].returncode)

    def test_clustered_help_and_attached_short_overrides_refuse_before_side_effects(self):
        for token in ("-hh", "-hhh", "-hh=true", "-Dh", "-hD", "-sh", "-Hunix:///unused.sock", "-cunused"):
            with self.subTest(token=token), patch.object(db, "verify_cli", side_effect=AssertionError("binary touched")), \
                    patch.object(db.tempfile, "TemporaryDirectory", side_effect=AssertionError("config allocated")), \
                    patch.object(db.subprocess, "Popen", side_effect=AssertionError("child spawned")), \
                    self.assertRaisesRegex(c.Invalid, "DOCKER_ARGV"):
                db.command([*db.PREFIX, "container", "inspect", "a" * 64, token], Deadline(1))

    def test_short_option_exception_requires_exact_oracle_entrypoint_image_and_tail(self):
        tail = ["--entrypoint", "python3", "sha256:" + "a" * 64, "-I", "-B", "/oracle/worker.py"]
        bad = [tail[:-1], tail + ["-hh"], [*tail[:3], "-IB", tail[-1]],
               [tail[0], "/other", *tail[2:]], [*tail[:2], "image:latest", *tail[3:]],
               [*tail[:2], "sha256:" + "0" * 64, *tail[3:]], ["-hh", *tail],
               [*tail[:3], "-I", "-h", tail[-1]], [*tail[:3], "-I", "-B", "/other"]]
        for suffix in bad:
            with self.subTest(suffix=suffix), self.assertRaises(c.Invalid):
                db.cli_arguments([*db.PREFIX, "container", "create", *suffix])
        with self.assertRaises(c.Invalid):
            db.cli_arguments([*db.PREFIX, "container", "inspect", *tail])

    def test_generated_oracle_create_remains_accepted_for_both_architectures(self):
        for architecture in ("aarch64", "x86_64"):
            with self.subTest(architecture=architecture), tempfile.TemporaryDirectory() as folder:
                value = fixture_scope(architecture=architecture)
                arch = c.runtime_architecture(architecture)["image_architecture"]
                calls = []
                def runner(argv, _):
                    calls.append(argv)
                    if argv[3] == "info":
                        return 0, DAEMON, b""
                    if argv[3] == "version":
                        return 0, c.canonical({"Os": "linux", "Arch": arch}), b""
                    db.cli_arguments(argv)
                    if argv[3:5] == ["image", "inspect"]:
                        return 0, c.canonical([{"Id": value["images"]["oracle"], "Os": "linux", "Architecture": arch,
                            "Config": {"Env": [], "OnBuild": None, "Volumes": None}}]), b""
                    return 0, b"a" * 64, b""
                paths = {role: Path(folder) / (role + ".json") for role in ACTORS}
                for role, path in paths.items():
                    path.write_bytes(c.canonical(profile(role, architecture)))
                backend = db.DockerBackend(value, paths, runner=runner, authorize=lambda *_: True)
                self.assertEqual(backend.create_container(container_spec(value, "oracle"), Deadline(2)), "a" * 64)
                self.assertEqual(calls[-1][-6:], ["--entrypoint", "python3", value["images"]["oracle"], "-I", "-B", "/oracle/worker.py"])

    def test_binary_requires_root_owned_native_nonwritable_regular_file(self):
        with tempfile.TemporaryDirectory() as folder:
            binary = Path(folder) / "docker"
            binary.write_bytes(b"#!/bin/sh\n")
            binary.chmod(0o755)
            with patch.object(db, "DOCKER", str(binary)), self.assertRaisesRegex(c.Invalid, "DOCKER_BINARY"):
                db.verify_cli()
            link = Path(folder) / "link"
            link.symlink_to(binary)
            with patch.object(db, "DOCKER", str(link)), self.assertRaises(OSError):
                db.verify_cli()


class LifecycleTests(unittest.TestCase):
    def test_normal_nonzero_exit_status_and_both_pipes_are_preserved(self):
        with children() as (owned, _):
            result = db.bounded_command(PYTHON + ['import os; os.write(1,b"out"); os.write(2,b"err"); os._exit(42)'], Deadline(2), {})
        self.assertEqual(result, (42, b"out", b"err"))
        self.assertTrue(owned[0].stdout.closed and owned[0].stderr.closed)

    def descendant_control(self, stream_mode, hold_output):
        reader, writer = os.pipe()
        script = f'''import os,time
child = os.fork()
if child == 0:
    if not {hold_output!r}:
        os.close(1); os.close(2)
    os.write({writer}, b"R")
    time.sleep(20)
    os._exit(0)
os._exit(0)
'''
        signals, owned = [], []
        real_killpg = os.killpg
        def killpg(pid, sig):
            self.assertIsNone(owned[0].returncode, "leader reaped before group signal")
            signals.append((pid, sig))
            return real_killpg(pid, sig)
        try:
            with children(script, inherited=(writer,)) as (owned, _), patch.object(local_process.os, "killpg", side_effect=killpg):
                if stream_mode:
                    stream = ProcessStream(PYTHON + [script], binding(), WireBudget(), Deadline(0.5))
                    stream.phase = "FINISHED"
                    if hold_output:
                        with self.assertRaisesRegex(c.Invalid, "DEADLINE"):
                            stream.finish_transport()
                    else:
                        result = stream.finish_transport()
                        self.assertFalse(result["remote_exit_proven"] or result["local_process_group_absence_proven"])
                elif hold_output:
                    with self.assertRaisesRegex(c.Invalid, "DEADLINE"):
                        db.bounded_command(PYTHON + [script], Deadline(0.5), {})
                else:
                    self.assertEqual(db.bounded_command(PYTHON + [script], Deadline(2), {}), (0, b"", b""))
                os.close(writer)
                writer = None
                self.assertEqual(signals, [(owned[0].pid, signal.SIGKILL)])
                self.assertTrue(select.select([reader], [], [], 1)[0])
                self.assertEqual(os.read(reader, 1), b"R")
                self.assertTrue(select.select([reader], [], [], 1)[0], "descendant survived cleanup")
                self.assertEqual(os.read(reader, 1), b"")
        finally:
            if writer is not None:
                os.close(writer)
            os.close(reader)

    def test_command_success_kills_closed_pipe_descendant_before_reaping(self):
        self.descendant_control(False, False)

    def test_command_timeout_kills_pipe_holding_descendant_before_reaping(self):
        self.descendant_control(False, True)

    def test_stream_normal_eof_kills_closed_pipe_descendant_before_reaping(self):
        self.descendant_control(True, False)

    def test_stream_timeout_kills_pipe_holding_descendant_before_reaping(self):
        self.descendant_control(True, True)

    def test_closed_pipes_with_live_leader_obey_deadline_without_timeout_leak(self):
        start = time.monotonic()
        with children() as (owned, _), self.assertRaisesRegex(c.Invalid, "^DEADLINE$"):
            db.bounded_command(PYTHON + ['import os,time; os.close(1); os.close(2); time.sleep(20)'], Deadline(0.15), {})
        self.assertLess(time.monotonic() - start, 2)
        self.assertEqual(owned[0].returncode, -signal.SIGKILL)

    def test_floods_are_bounded_and_cleanup_closes_descriptors(self):
        for fd in (1, 2):
            with self.subTest(fd=fd), children() as (owned, _), self.assertRaisesRegex(c.Invalid, "COMMAND_OUTPUT_BOUND"):
                db.bounded_command(PYTHON + [f'import os;\nwhile True: os.write({fd}, b"x"*8192)'], Deadline(2), {})
            self.assertIsNotNone(owned[0].returncode)
            self.assertTrue(owned[0].stdout.closed and owned[0].stderr.closed)

    def test_selector_failure_occurs_before_command_spawn(self):
        with patch.object(db.selectors, "DefaultSelector", side_effect=OSError(errno.ENFILE, "selector")), \
                patch.object(db.subprocess, "Popen") as spawn, self.assertRaises(OSError):
            db.bounded_command(PYTHON + ["pass"], Deadline(1), {})
        spawn.assert_not_called()

    def test_pipe_setup_failure_kills_group_and_closes_selector(self):
        mux = db.selectors.DefaultSelector()
        with patch.object(db.selectors, "DefaultSelector", return_value=mux), \
                children('import time; time.sleep(20)') as (owned, _), \
                patch.object(db.os, "set_blocking", side_effect=OSError(errno.EIO, "setup")), self.assertRaises(OSError):
            db.bounded_command(PYTHON + ["pass"], Deadline(1), {})
        self.assertIsNone(mux.get_map())
        self.assertIsNotNone(owned[0].returncode)
        self.assertTrue(owned[0].stdout.closed and owned[0].stderr.closed)

    def test_reaped_leader_can_never_trigger_numeric_group_signal(self):
        process = SimpleNamespace(pid=12345, returncode=0)
        with patch.object(local_process.os, "killpg") as signal_group, self.assertRaisesRegex(c.Invalid, "LOCAL_PROCESS_OWNERSHIP"):
            local_process.kill_and_reap(process)
        signal_group.assert_not_called()

    def test_cleanup_timeout_is_value_free(self):
        process = SimpleNamespace(pid=12345, returncode=None)
        process.wait = lambda **kwargs: (_ for _ in ()).throw(subprocess.TimeoutExpired(["private-canary"], 5))
        with patch.object(local_process, "observe_exit"), patch.object(local_process.os, "killpg"), \
                self.assertRaisesRegex(c.Invalid, "^LOCAL_PROCESS_CLEANUP$"):
            local_process.kill_and_reap(process)


if __name__ == "__main__":
    unittest.main()
