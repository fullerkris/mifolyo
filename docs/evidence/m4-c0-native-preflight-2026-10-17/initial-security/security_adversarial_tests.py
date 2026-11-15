"""Independent offline tests of the frozen preflight; no Docker/SSH/native actors.

Run with python3 -B. Only reviewer-owned benign Python processes are executed.
Failures are deliberately retained: the source snapshot is never changed.
"""
import copy
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
import time
import unittest
from unittest.mock import patch

ROOT = Path('/tmp/opencode/c0-native-preflight-2026-10-17')
SECURITY = ROOT / 'security'
SOURCE = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
sys.dont_write_bytecode = True
sys.path.insert(0, str(SOURCE))

import docker_backend as backend
import host_preflight as hp
from contracts import Invalid, canonical
from streams import Deadline
from test_host_preflight import FixtureProbe

REAL_POPEN = subprocess.Popen
EVENTS = []
BENIGN_ENV = {'PATH': '/usr/bin:/bin', 'LC_ALL': 'C', 'PYTHONDONTWRITEBYTECODE': '1'}


def benign_command(code, seconds=1):
    spawned = []
    def spawn(*args, **kwargs):
        # Deny real Docker or other processes even if the fixture changes.
        assert args[0][:4] == [sys.executable, '-I', '-B', '-c']
        child = REAL_POPEN(*args, **kwargs)
        spawned.append(child)
        return child
    start = time.monotonic()
    try:
        with patch.dict(os.environ, BENIGN_ENV, clear=True), patch.object(backend.subprocess, 'Popen', side_effect=spawn):
            return backend.command([sys.executable, '-I', '-B', '-c', code], Deadline(seconds))
    finally:
        EVENTS.append({'event': 'benign_command', 'elapsed_seconds': round(time.monotonic() - start, 4),
            'leaders': [{'pid': p.pid, 'returncode': p.returncode} for p in spawned]})
        for child in spawned:
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=2)


class SecurityAdversarialTests(unittest.TestCase):
    def test_01_ambient_config_and_secret_environment_are_not_forwarded(self):
        synthetic = {
            'PATH': '/usr/bin:/bin', 'HOME': '/synthetic-home-never-opened',
            'DOCKER_HOST': 'ssh://invalid.test', 'DOCKER_CONTEXT': 'invalid',
            'DOCKER_TLS': '1', 'DOCKER_TLS_VERIFY': '1', 'DOCKER_CERT_PATH': '/synthetic-certs',
            'DOCKER_CONFIG': '/synthetic-config-never-opened', 'DOCKER_CLI_HOOKS': 'true',
            'DOCKER_CUSTOM_HEADERS': 'X-Review=SYNTHETIC_HEADER_CANARY',
            'OTEL_EXPORTER_OTLP_ENDPOINT': 'https://invalid.test',
            'LD_PRELOAD': '/synthetic-library-never-opened',
            'AWS_SECRET_ACCESS_KEY': 'SYNTHETIC_SECRET_CANARY',
        }
        # Popen is stopped before execution. No synthetic or real credential path is read.
        with patch.dict(os.environ, synthetic, clear=True), patch.object(backend.subprocess, 'Popen', side_effect=OSError('fixture')) as spawn:
            with self.assertRaises(OSError):
                backend.command([*hp.PREFIX, 'info', '--format', hp.template(hp.INFO_FIELDS)], Deadline(1))
            forwarded = spawn.call_args.kwargs['env']
        unexpected = sorted(set(forwarded) - {'PATH'})
        EVENTS.append({'event': 'ambient_environment', 'forwarded_keys': sorted(forwarded),
            'removed_endpoint_keys': sorted(set(synthetic) - set(forwarded)), 'real_process_started': False})
        self.assertEqual(unexpected, [], 'Implicit CLI configuration/plugin/telemetry and secret environment still forwarded')

    def test_02_closed_pipe_timeout_returns_structured_blocker(self):
        fixture = FixtureProbe()
        real_probe = hp.HostProbe('x86_64')
        fixture.docker = real_probe.docker
        spawned = []
        def spawn(*args, **kwargs):
            assert args[0][:4] == [sys.executable, '-I', '-B', '-c']
            child = REAL_POPEN(*args, **kwargs)
            spawned.append(child)
            return child
        def substituted_command(argv, deadline):
            self.assertEqual(argv[:3], list(hp.PREFIX))
            self.assertIn(argv[3], ('info', 'version'))
            return backend.command([sys.executable, '-I', '-B', '-c',
                'import os,time; os.close(1); os.close(2); time.sleep(30)'], deadline)
        start = time.monotonic()
        try:
            with patch.dict(os.environ, BENIGN_ENV, clear=True), patch.object(backend.subprocess, 'Popen', side_effect=spawn), \
                    patch.object(hp, 'command', side_effect=substituted_command):
                result = hp.inspect_host('x86_64', probe=fixture, deadline=Deadline(0.3))
            self.assertEqual(result['status'], 'BLOCKED')
            self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
            self.assertFalse(result['host_admitted'] or result['execution_authorized'] or result['runtime_evidence'])
        finally:
            EVENTS.append({'event': 'closed_pipe_timeout', 'elapsed_seconds': round(time.monotonic() - start, 4),
                'leaders': [{'pid': child.pid, 'returncode': child.returncode} for child in spawned]})
            for child in spawned:
                if child.poll() is None:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait(timeout=2)

    def test_03_success_does_not_leave_a_running_descendant(self):
        # The descendant is a benign sleeping Python process with a five-second
        # self-expiry. It closes the transport pipes, exposing the normal-return
        # cleanup branch. A pidfd ensures reviewer cleanup cannot hit a reused PID.
        leader = (
            'import subprocess,sys; '
            'p=subprocess.Popen([sys.executable,"-I","-B","-c","import time; time.sleep(5)"],'
            'stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,close_fds=True); '
            'print(p.pid,flush=True)'
        )
        code, out, err = benign_command(leader)
        self.assertEqual((code, err), (0, b''))
        pid = int(out.strip())
        fd = os.pidfd_open(pid)
        try:
            running = not bool(select.select([fd], [], [], 0)[0])
            EVENTS.append({'event': 'successful_command_descendant', 'descendant_pid': pid,
                'running_after_command_return': running, 'command_returncode': code})
            try:
                self.assertFalse(running, 'Owned descendant is still running after command() returns successfully')
            finally:
                if running:
                    signal.pidfd_send_signal(fd, signal.SIGKILL)
                terminated = bool(select.select([fd], [], [], 2)[0])
                EVENTS.append({'event': 'reviewer_descendant_cleanup', 'descendant_pid': pid,
                    'termination_confirmed_by_pidfd': terminated})
                if not terminated:
                    raise RuntimeError('reviewer-owned benign descendant did not terminate')
        finally:
            os.close(fd)

    def test_04_open_pipe_stall_is_bounded_and_reaped(self):
        with self.assertRaisesRegex(Invalid, '^DEADLINE$'):
            benign_command('import time; time.sleep(30)', seconds=0.2)
        self.assertEqual(EVENTS[-1]['leaders'][0]['returncode'], -signal.SIGKILL)

    def test_05_stdout_flood_is_bounded_and_reaped(self):
        with self.assertRaisesRegex(Invalid, '^COMMAND_OUTPUT_BOUND$'):
            benign_command('import os,time; os.write(1,b"x"*300000); time.sleep(30)')
        self.assertEqual(EVENTS[-1]['leaders'][0]['returncode'], -signal.SIGKILL)

    def test_06_stderr_flood_is_bounded_and_reaped(self):
        with self.assertRaisesRegex(Invalid, '^COMMAND_OUTPUT_BOUND$'):
            benign_command('import os,time; os.write(2,b"x"*300000); time.sleep(30)')
        self.assertEqual(EVENTS[-1]['leaders'][0]['returncode'], -signal.SIGKILL)

    def test_07_error_bytes_are_never_exported(self):
        for code, stdout, stderr in (
                (1, b'SYNTHETIC_STDOUT_CANARY', b'SYNTHETIC_STDERR_CANARY'),
                (0, canonical(FixtureProbe().info), b'https://user:SYNTHETIC_PROXY_CANARY@invalid.test'),
                (0, b'{"error":"SYNTHETIC_JSON_CANARY"}', b'')):
            with self.subTest(code=code, malformed=not stderr):
                fixture = FixtureProbe()
                fixture.docker = hp.HostProbe('x86_64').docker
                with patch.object(hp, 'command', return_value=(code, stdout, stderr)):
                    result = hp.inspect_host('x86_64', probe=fixture)
                self.assertEqual(result['status'], 'BLOCKED')
                self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
                self.assertNotIn(b'CANARY', canonical(result))

    def test_08_malformed_docker_fields_never_admit_or_leak(self):
        failures = 0
        variants = [None, [], {}, True, False, 0, -1, 2**63, '', 'SYNTHETIC_CANARY\n', '\ud800']
        for key in hp.INFO_FIELDS:
            for value in variants:
                probe = FixtureProbe()
                probe.info[key] = value
                with self.subTest(field=key, type=type(value).__name__):
                    result = hp.inspect_host('x86_64', probe=probe)
                    # False is valid resource metadata but still a blocker.
                    # True is the fixture's valid value for these five fields.
                    if key in ('memory', 'swap', 'pids', 'cpu_period', 'cpu_quota') and value is True:
                        self.assertEqual(result['status'], 'HOST_CANDIDATE')
                    else:
                        self.assertEqual(result['status'], 'BLOCKED')
                        failures += 1
                    self.assertNotIn(b'CANARY', canonical(result))
                    self.assertFalse(result['host_admitted'] or result['runtime_evidence'] or result['execution_authorized'])
        EVENTS.append({'event': 'malformed_docker_matrix', 'cases': len(hp.INFO_FIELDS)*len(variants), 'blocked': failures})

    def test_09_json_bound_and_recursive_data_are_rejected(self):
        samples = [b'{}', b'null', b'true', b'0', b'[]', b'\xff', b'{}{}',
            b'{"os":1e100}', b'{"os":NaN}', b'{"os":Infinity}', b'{"os":-Infinity}',
            b'{"os":{"duplicate":1,"duplicate":2}}',
            b'{"os":' + b'['*1500 + b'0' + b']'*1500 + b'}', b'x'*16385]
        for raw in samples:
            with self.subTest(length=len(raw)), self.assertRaises(Invalid):
                hp.metadata(raw, {'os': 'OSType'})

    def test_10_all_missing_observations_block_without_raw_errors(self):
        for field in ('native', 'kernel', 'interpreter', 'controllers', 'endpoint', 'docker', 'collector'):
            for error in (OSError(errno.EACCES, 'SYNTHETIC_CANARY'), ValueError('SYNTHETIC_CANARY'), TypeError('SYNTHETIC_CANARY')):
                probe = FixtureProbe()
                def unavailable(*args):
                    raise error
                setattr(probe, field, unavailable)
                with self.subTest(field=field, exception=type(error).__name__):
                    result = hp.inspect_host('x86_64', probe=probe)
                    self.assertEqual(result['status'], 'BLOCKED')
                    self.assertNotIn(b'CANARY', canonical(result))
                    self.assertFalse(result['host_admitted'] or result['runtime_evidence'] or result['execution_authorized'])

    def test_11_collector_uses_lstat_only_and_refuses_symlinks(self):
        probe = hp.HostProbe('x86_64')
        with patch.object(hp.os, 'lstat', return_value=os.stat_result((stat.S_IFLNK | 0o777,0,0,0,0,0,0,0,0,0))) as lstat, \
                patch('builtins.open', side_effect=AssertionError('no collector contents may be opened')):
            result = probe.collector()
        self.assertEqual(result, {'anchor': 'UNEXPECTED_TYPE', 'code': 'UNEXPECTED_TYPE'})
        self.assertEqual([c.args[0] for c in lstat.call_args_list], ['/etc/obs1/context-source.json', '/opt/obs1/context-source'])

    def test_12_runtime_provenance_is_never_promoted_to_admission(self):
        for architecture in ('x86_64', 'aarch64'):
            result = hp.inspect_host(architecture, probe=FixtureProbe(architecture))
            self.assertEqual(result['status'], 'HOST_CANDIDATE')
            self.assertEqual(result['evidence_kind'], 'simulated')
            self.assertIn('daemon_kernel_namespace_pairing', result['pending'])
            self.assertFalse(result['host_admitted'] or result['runtime_evidence'] or result['execution_authorized'])
            self.assertEqual(result['native_trials_run'], 0)
            self.assertLessEqual(len(canonical(result)), 8192)


if __name__ == '__main__':
    # A top-level deny guard means accidental additions cannot contact Docker,
    # SSH, or a native actor. Explicit reviewed benign process fixtures use the
    # REAL_POPEN reference and a fixed interpreter-only argv.
    with patch.object(subprocess, 'Popen', side_effect=AssertionError('unapproved process start')):
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(SecurityAdversarialTests)
        result = unittest.TextTestRunner(verbosity=2).run(suite)
    report = {'status': 'PASS' if result.wasSuccessful() else 'FAIL', 'tests_run': result.testsRun,
        'failures': [test.id() for test, _ in result.failures], 'errors': [test.id() for test, _ in result.errors],
        'skipped': result.skipped, 'events': EVENTS, 'source_unmodified': True,
        'live_docker_ssh_ptrace_or_native_actors_run': False}
    with (SECURITY / 'adversarial-original.json').open('x') as f:
        json.dump(report, f, indent=2, sort_keys=True)
        f.write('\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
