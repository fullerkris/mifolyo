"""Additional read-only boundary controls for the two scoped fixes."""
import copy
import io
import json
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch

ROOT = Path('/tmp/opencode/c0-native-preflight-2026-10-17')
OUT = ROOT / 'correctness-final'
sys.dont_write_bytecode = True
sys.path.insert(0, str(ROOT / 'remediation-snapshot/tests/crawl-jobs-v2-observer'))

import host_preflight
from contracts import Invalid, canonical, sha
from host_preflight import HostProbe, INFO_FIELDS, VERSION_FIELDS, PREFIX, PENDING, inspect_host, main, template
from streams import Deadline
from test_host_preflight import FixtureProbe

EVIDENCE = []


def wire_runner(probe, calls, timeout_at=None):
    def run(argv, deadline):
        assert argv[:3] == list(PREFIX)
        operation = argv[3]
        fields = INFO_FIELDS if operation == 'info' else VERSION_FIELDS
        assert argv == [*PREFIX, operation, '--format', template(fields)]
        calls.append(operation)
        if len(calls) == timeout_at:
            raise subprocess.TimeoutExpired(['PRIVATE_CANARY'], 0.5,
                output=b'PRIVATE_CANARY', stderr=b'PRIVATE_CANARY')
        return 0, canonical(probe.info if operation == 'info' else probe.server), b''
    return run


class FixBoundaries(unittest.TestCase):
    def assert_non_authorizing(self, result):
        for field in ('host_admitted', 'runtime_evidence', 'execution_authorized'):
            self.assertIs(result[field], False)
        self.assertEqual(result['native_trials_run'], 0)
        self.assertEqual(result['evidence_kind'], 'simulated')
        self.assertEqual(result['pending'], list(PENDING))
        self.assertLessEqual(len(canonical(result)), 8192)

    def test_timeout_at_each_metadata_read_preserves_cli_json(self):
        real_inspect = inspect_host
        for arch in ('x86_64', 'aarch64'):
            for failing_read in (1, 2, 3):
                with self.subTest(architecture=arch, failing_read=failing_read):
                    probe, calls = FixtureProbe(arch), []
                    probe.docker = HostProbe(arch).docker
                    output, errors = io.StringIO(), io.StringIO()
                    def inspected(architecture):
                        return real_inspect(architecture, probe=probe, deadline=Deadline(2))
                    with patch('host_preflight.command', side_effect=wire_runner(probe, calls, failing_read)), \
                            patch('host_preflight.inspect_host', side_effect=inspected), \
                            patch('sys.stdout', output), patch('sys.stderr', errors):
                        code = main(['--architecture', arch])
                    result = json.loads(output.getvalue())
                    self.assertEqual(code, 1)
                    self.assertEqual(errors.getvalue(), '')
                    self.assertNotIn('PRIVATE_CANARY', output.getvalue())
                    self.assertNotIn('TimeoutExpired', output.getvalue())
                    self.assertEqual(result['status'], 'BLOCKED')
                    self.assertEqual(result['blockers'], ['DOCKER_UNAVAILABLE'])
                    self.assertIsNone(result['observations']['docker'])
                    self.assertEqual(calls, ['info', 'version', 'info'][:failing_read])
                    self.assert_non_authorizing(result)
                    EVIDENCE.append({'case': 'timeout_at_metadata_read', 'architecture': arch,
                        'read': failing_read, 'calls': calls, 'exit_status': code,
                        'stderr_empty': True, 'status': result['status'], 'blockers': result['blockers'],
                        'private_canary_absent': True})

    def test_direct_timeout_translation_has_stable_value_free_error(self):
        for operation in ('info', 'version'):
            error = subprocess.TimeoutExpired(['PRIVATE_CANARY'], 0.5,
                output=b'PRIVATE_CANARY', stderr=b'PRIVATE_CANARY')
            with patch('host_preflight.command', side_effect=error), self.assertRaises(Invalid) as caught:
                HostProbe('x86_64').docker(operation, Deadline(1))
            self.assertEqual(str(caught.exception), 'PREFLIGHT_DOCKER_TIMEOUT')
            self.assertIsNone(caught.exception.__cause__)
            self.assertTrue(caught.exception.__suppress_context__)

    def test_exact_nnp_token_acceptance_and_negative_variants_via_wire(self):
        bad = ('name=no-new-privileges,profile=builtin', 'name=no-new-privileges=true',
            'name=no-new-privileges-extra', 'name=No-new-privileges',
            'name=no_new_privileges', 'name=unknown-hyphenated',
            'name=no-new-privileges\n', 'name=no-new-privileges\x00')
        for arch in ('x86_64', 'aarch64'):
            for option in ('name=no-new-privileges', *bad):
                with self.subTest(architecture=arch, option=repr(option)):
                    probe, calls = FixtureProbe(arch), []
                    probe.info['security_options'].append(option)
                    probe.docker = HostProbe(arch).docker
                    with patch('host_preflight.command', side_effect=wire_runner(probe, calls)):
                        result = inspect_host(arch, probe=probe)
                    self.assert_non_authorizing(result)
                    if option == 'name=no-new-privileges':
                        self.assertEqual(result['status'], 'HOST_CANDIDATE')
                        self.assertEqual(result['blockers'], [])
                        self.assertEqual(calls, ['info', 'version', 'info'])
                        self.assertEqual(result['observations']['docker']['security_options_sha256'],
                            sha(canonical(probe.info['security_options'])))
                    else:
                        self.assertEqual(result['status'], 'BLOCKED')
                        self.assertEqual(result['blockers'], ['DOCKER_UNAVAILABLE'])
                    EVIDENCE.append({'case': 'exact_nnp_wire', 'architecture': arch,
                        'option': option, 'status': result['status'], 'blockers': result['blockers']})

    def test_nnp_does_not_relax_duplicates_bounds_or_other_blockers(self):
        for arch in ('x86_64', 'aarch64'):
            for variant in ('duplicate', 'rootless', 'userns', 'no_seccomp', 'no_cgroupns', 'no_memory', '16_options', '17_options'):
                with self.subTest(architecture=arch, variant=variant):
                    probe, calls = FixtureProbe(arch), []
                    options = probe.info['security_options']
                    options.append('name=no-new-privileges')
                    if variant == 'duplicate':
                        options.append('name=no-new-privileges')
                    elif variant in ('rootless', 'userns'):
                        options.append('name=' + variant)
                    elif variant == 'no_seccomp':
                        options.remove('name=seccomp,profile=builtin')
                    elif variant == 'no_cgroupns':
                        options.remove('name=cgroupns')
                    elif variant == 'no_memory':
                        probe.info['memory'] = False
                    else:
                        options.extend('name=z' + chr(ord('a') + i) for i in range(int(variant[:2]) - 4))
                    probe.docker = HostProbe(arch).docker
                    with patch('host_preflight.command', side_effect=wire_runner(probe, calls)):
                        result = inspect_host(arch, probe=probe)
                    expected = {
                        'duplicate': ['DOCKER_UNAVAILABLE'], '17_options': ['DOCKER_UNAVAILABLE'],
                        'rootless': ['DOCKER_NAMESPACE_MAPPING_UNSUPPORTED'],
                        'userns': ['DOCKER_NAMESPACE_MAPPING_UNSUPPORTED'],
                        'no_seccomp': ['DOCKER_CONFINEMENT_UNSUPPORTED'],
                        'no_cgroupns': ['DOCKER_CONFINEMENT_UNSUPPORTED'],
                        'no_memory': ['DOCKER_RESOURCE_LIMITS_UNSUPPORTED'], '16_options': [],
                    }[variant]
                    self.assertEqual(result['blockers'], expected)
                    self.assertEqual(result['status'], 'BLOCKED' if expected else 'HOST_CANDIDATE')
                    self.assert_non_authorizing(result)
                    EVIDENCE.append({'case': 'nnp_preserves_other_checks', 'architecture': arch,
                        'variant': variant, 'status': result['status'], 'blockers': result['blockers']})


if __name__ == '__main__':
    program = unittest.main(verbosity=2, exit=False)
    (OUT / 'fix-boundary-evidence.json').write_text(json.dumps(EVIDENCE, indent=2) + '\n')
    raise SystemExit(0 if program.result.wasSuccessful() else 1)
