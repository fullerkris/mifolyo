"""Independent offline/benign-process controls; never invokes Docker or SSH."""
import copy
import io
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import unittest
from unittest.mock import patch

ROOT = Path('/tmp/opencode/c0-native-preflight-2026-10-17')
sys.dont_write_bytecode = True
sys.path.insert(0, str(ROOT / 'snapshot/tests/crawl-jobs-v2-observer'))

import docker_backend
import host_preflight
from contracts import Invalid, canonical, sha
from host_preflight import HostProbe, INFO_FIELDS, VERSION_FIELDS, inspect_host, metadata
from runtime_admission import CONTEXT_HASHES, NAMESPACES
from streams import Deadline
from test_host_preflight import FixtureProbe


def benign_transport(source, action):
    """Run only this owned Python process in place of the fixed Docker argv."""
    original_popen = subprocess.Popen
    spawned = []

    def spawn(argv, **kwargs):
        assert argv == [sys.executable, '-B', '-c', source], argv
        process = original_popen(argv, **kwargs)
        process.review_fallback_cleanup_used = False
        spawned.append(process)
        return process

    def runner(argv, deadline):
        assert argv[:3] == list(host_preflight.PREFIX), argv
        assert argv[3] in ('info', 'version') and argv[4] == '--format', argv
        return docker_backend.command([sys.executable, '-B', '-c', source], deadline)

    try:
        with patch('docker_backend.subprocess.Popen', side_effect=spawn), patch('host_preflight.command', side_effect=runner):
            return action(), spawned
    finally:
        # The production runner must reap its own children. No unrelated processes
        # are inspected/signalled. This fallback only cleans up a probe failure.
        for process in spawned:
            if process.returncode is None:
                process.review_fallback_cleanup_used = True
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait(timeout=3)


class Controls(unittest.TestCase):
    def assert_no_authority(self, result):
        self.assertIs(result['host_admitted'], False)
        self.assertIs(result['runtime_evidence'], False)
        self.assertIs(result['execution_authorized'], False)
        self.assertEqual(result['native_trials_run'], 0)
        self.assertEqual(result['evidence_kind'], 'simulated')
        self.assertEqual(result['pending'], list(host_preflight.PENDING))
        self.assertLessEqual(len(canonical(result)), 8192)

    def test_native_and_image_architecture_aliases(self):
        for arch, image in (('aarch64', 'arm64'), ('x86_64', 'amd64')):
            for info_arch in (arch, image):
                with self.subTest(architecture=arch, info_arch=info_arch):
                    probe = FixtureProbe(arch)
                    probe.info['architecture'] = info_arch
                    result = inspect_host(arch, probe=probe)
                    self.assertEqual(result['status'], 'HOST_CANDIDATE')
                    self.assertEqual(result['observations']['docker']['architecture'], image)
                    self.assert_no_authority(result)

    def test_raw_json_docker_bridge_both_architectures(self):
        for arch in ('aarch64', 'x86_64'):
            probe = FixtureProbe(arch)
            probe.docker = HostProbe(arch).docker
            def runner(argv, deadline):
                self.assertEqual(argv[:3], list(host_preflight.PREFIX))
                self.assertIn(argv[3], ('info', 'version'))
                return 0, canonical(probe.info if argv[3] == 'info' else probe.server), b''
            with patch('host_preflight.command', side_effect=runner) as mock:
                result = inspect_host(arch, probe=probe)
            self.assertEqual(result['status'], 'HOST_CANDIDATE')
            self.assertEqual([call.args[0][3] for call in mock.call_args_list], ['info', 'version', 'info'])
            self.assert_no_authority(result)

    def test_exact_metadata_bound_and_adversarial_json(self):
        prefix = b'{"os":"linux"}'
        raw = prefix + b' ' * (16384 - len(prefix))
        self.assertEqual(metadata(raw, {'os': 'OSType'}), {'os': 'linux'})
        cases = [raw + b' ', b'', b'{"os":{"x":1,"x":2}}', b'{"os":Infinity}',
                 b'{"os":-Infinity}', b'{"os":-0.0}', b'{"os":1e1}',
                 b'{"os":"linux"}\x00', b'{"os":"linux"} null',
                 b'{"os":"linux","o\\u0073":"linux"}']
        for index, value in enumerate(cases):
            with self.subTest(case=index), self.assertRaises(Invalid):
                metadata(value, {'os': 'OSType'})

    def test_nested_wrong_type_rejected_at_validation_boundary(self):
        # JSON nesting is valid syntax. Decoder recursion behavior varies between
        # Python versions; acceptance as JSON must still fail the field schema.
        raw = b'{"os":' + b'[' * 1200 + b'0' + b']' * 1200 + b'}'
        try:
            parsed = metadata(raw, {'os': 'OSType'})
        except Invalid:
            return
        probe = FixtureProbe()
        probe.info['os'] = parsed['os']
        result = inspect_host('x86_64', probe=probe)
        self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
        self.assert_no_authority(result)

    def test_docker_wrong_type_matrix_is_structured(self):
        fixture = FixtureProbe()
        values = (None, [], {}, True, False, 1, 0, 1.0, 'CANARY\nVALUE')
        for key, original in fixture.info.items():
            for bad in values:
                # Booleans are valid resource metadata, including False blockers.
                if type(original) is bool and type(bad) is bool:
                    continue
                if key == 'security_options' and type(bad) is list:
                    continue
                with self.subTest(field=key, bad=repr(bad)):
                    probe = FixtureProbe()
                    probe.info[key] = bad
                    result = inspect_host('x86_64', probe=probe)
                    self.assertEqual(result['status'], 'BLOCKED')
                    self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
                    self.assertNotIn(b'CANARY', canonical(result))
                    self.assert_no_authority(result)

    def test_all_daemon_selected_field_drift_is_rejected(self):
        for key in FixtureProbe().info:
            with self.subTest(field=key):
                probe = FixtureProbe()
                original = probe.docker
                def changed(operation, deadline):
                    value = original(operation, deadline)
                    if len(probe.calls) == 3:
                        value[key] = 'CHANGED'
                    return value
                probe.docker = changed
                result = inspect_host('x86_64', probe=probe)
                self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
                self.assertIsNone(result['observations']['docker'])
                self.assert_no_authority(result)

    def test_all_kernel_context_drift_is_rejected(self):
        fields = (*CONTEXT_HASHES, *NAMESPACES, 'ns_device', 'resolution_ns', 'yama_scope')
        for field in fields:
            with self.subTest(field=field):
                probe = FixtureProbe()
                original = probe.kernel
                def changed():
                    value = original()
                    if probe.kernels == 2:
                        if field in NAMESPACES:
                            value['namespaces'][field] += 1
                        elif field in CONTEXT_HASHES:
                            value[field] = sha(b'CHANGED')
                        else:
                            value[field] += 1
                    return value
                probe.kernel = changed
                result = inspect_host('x86_64', probe=probe)
                self.assertIn('HOST_OBSERVATION_CHANGED', result['blockers'])
                self.assert_no_authority(result)

    def test_kernel_integers_reject_boolean_negative_and_oversize(self):
        for value in (True, False, -1, 0, 2**53, '1', 1.0):
            with self.subTest(value=repr(value)):
                probe = FixtureProbe()
                probe.context['namespaces']['user_ns'] = value
                result = inspect_host('x86_64', probe=probe)
                self.assertIn('KERNEL_UNAVAILABLE', result['blockers'])
                self.assert_no_authority(result)

    def test_nested_snapshot_is_detached_after_return(self):
        probe = FixtureProbe()
        probe.kernel = lambda: probe.context
        probe.docker = lambda operation, deadline: probe.info if operation == 'info' else probe.server
        result = inspect_host('x86_64', probe=probe)
        frozen = canonical(result)
        probe.context['namespaces']['user_ns'] += 1
        probe.info['security_options'].append('name=rootless')
        self.assertEqual(canonical(result), frozen)

    def test_expired_deadline_skips_all_producer_actions(self):
        now = [0]
        deadline = Deadline(1, clock=lambda: now[0])
        now[0] = 1
        probe = FixtureProbe()
        def not_called(*args):
            raise AssertionError('expired probe was called')
        for method in ('native', 'kernel', 'interpreter', 'controllers', 'endpoint', 'docker', 'collector'):
            setattr(probe, method, not_called)
        result = inspect_host('x86_64', probe=probe, deadline=deadline)
        self.assertEqual(result['status'], 'BLOCKED')
        self.assert_no_authority(result)

    def test_collector_lstats_only_fixed_paths(self):
        import stat
        class Info:
            def __init__(self, mode):
                self.st_mode = mode
        with patch('host_preflight.os.lstat', side_effect=[Info(stat.S_IFREG | 0o644), Info(stat.S_IFDIR | 0o755)]) as lstat:
            value = HostProbe('x86_64').collector()
        self.assertEqual([call.args[0] for call in lstat.call_args_list], ['/etc/obs1/context-source.json', '/opt/obs1/context-source'])
        self.assertEqual(value, {'anchor': 'PRESENT_UNVERIFIED', 'code': 'PRESENT_UNVERIFIED'})
        with patch('host_preflight.os.lstat', return_value=Info(stat.S_IFLNK | 0o777)):
            self.assertEqual(set(HostProbe('aarch64').collector().values()), {'UNEXPECTED_TYPE'})

    def test_benign_open_pipe_timeout_is_structured_and_reaped(self):
        probe = FixtureProbe()
        probe.docker = HostProbe('x86_64').docker
        result, spawned = benign_transport('import time; time.sleep(20)',
            lambda: inspect_host('x86_64', probe=probe, deadline=Deadline(0.5)))
        self.assertEqual(result['status'], 'BLOCKED')
        self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
        self.assertEqual([p.returncode for p in spawned], [-signal.SIGKILL])
        self.assertFalse(any(p.review_fallback_cleanup_used for p in spawned))
        self.assertTrue(all(p.stdout.closed and p.stderr.closed for p in spawned))
        self.assert_no_authority(result)

    def test_benign_command_output_flood_is_bounded_and_reaped(self):
        probe = FixtureProbe()
        probe.docker = HostProbe('x86_64').docker
        source = 'import os, time; os.write(1, b"x" * 300000); time.sleep(20)'
        result, spawned = benign_transport(source,
            lambda: inspect_host('x86_64', probe=probe, deadline=Deadline(3)))
        self.assertEqual(result['status'], 'BLOCKED')
        self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
        self.assertEqual([p.returncode for p in spawned], [-signal.SIGKILL])
        self.assertFalse(any(p.review_fallback_cleanup_used for p in spawned))
        self.assert_no_authority(result)

    def test_benign_stderr_is_not_exported(self):
        probe = FixtureProbe()
        probe.docker = HostProbe('x86_64').docker
        result, spawned = benign_transport('import os; os.write(2, b"PRIVATE_CANARY")',
            lambda: inspect_host('x86_64', probe=probe, deadline=Deadline(3)))
        self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
        self.assertNotIn(b'PRIVATE_CANARY', canonical(result))
        self.assertEqual([p.returncode for p in spawned], [0])
        self.assertFalse(any(p.review_fallback_cleanup_used for p in spawned))
        self.assert_no_authority(result)

    def test_cli_candidate_is_json_not_admission(self):
        value = inspect_host('x86_64', probe=FixtureProbe())
        output = io.StringIO()
        with patch('host_preflight.inspect_host', return_value=value), patch('sys.stdout', output):
            self.assertEqual(host_preflight.main(['--architecture', 'x86_64']), 0)
        self.assertEqual(json.loads(output.getvalue()), value)
        self.assert_no_authority(value)


if __name__ == '__main__':
    unittest.main(verbosity=2)
