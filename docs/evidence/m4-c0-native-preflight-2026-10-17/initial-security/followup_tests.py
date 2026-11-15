"""Follow-up disposition tests; preserve the original failing run unmodified."""
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
import host_preflight as hp
import docker_backend as backend
from contracts import canonical
from streams import Deadline
from test_host_preflight import FixtureProbe

REAL_POPEN = subprocess.Popen
EVENTS = []


class FollowupTests(unittest.TestCase):
    def test_deep_json_is_rejected_at_preflight_validation_boundary(self):
        fixture = FixtureProbe()
        raw = json.dumps(fixture.info).encode().replace(b'"kernel": "6.8.0-fixture"',
            b'"kernel": ' + b'[' * 1500 + b'0' + b']' * 1500)
        fixture.docker = hp.HostProbe('x86_64').docker
        with patch.object(hp, 'command', return_value=(0, raw, b'')):
            result = hp.inspect_host('x86_64', probe=fixture)
        self.assertEqual(result['status'], 'BLOCKED')
        self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
        self.assertIsNone(result['observations']['docker'])
        self.assertFalse(result['host_admitted'] or result['execution_authorized'] or result['runtime_evidence'])
        EVENTS.append({'event': 'deep_json_end_to_end', 'depth': 1500, 'raw_bytes': len(raw),
            'status': result['status'], 'blockers': result['blockers'],
            'original_failure_disposition': 'test overconstrained metadata() alone; typed preflight still rejects'})

    def test_main_timeout_escapes_without_a_json_report(self):
        fixture = FixtureProbe()
        fixture.docker = hp.HostProbe('x86_64').docker
        spawned = []
        def spawn(*args, **kwargs):
            assert args[0][:4] == [sys.executable, '-I', '-B', '-c']
            child = REAL_POPEN(*args, **kwargs)
            spawned.append(child)
            return child
        def substitute(argv, deadline):
            assert argv[:3] == list(hp.PREFIX) and argv[3] in ('info', 'version')
            return backend.command([sys.executable, '-I', '-B', '-c',
                'import os,time; os.close(1); os.close(2); time.sleep(30)'], deadline)
        output = io.StringIO()
        try:
            with patch.object(hp, 'HostProbe', return_value=fixture), patch.object(hp, 'Deadline', side_effect=lambda _: Deadline(0.2)), \
                    patch.object(hp, 'command', side_effect=substitute), patch.object(backend.subprocess, 'Popen', side_effect=spawn), \
                    patch.dict(os.environ, {'PATH': '/usr/bin:/bin', 'LC_ALL': 'C'}, clear=True), patch('sys.stdout', output):
                with self.assertRaises(subprocess.TimeoutExpired):
                    hp.main(['--architecture', 'x86_64'])
            self.assertEqual(output.getvalue(), '')
            self.assertEqual(spawned[0].returncode, -signal.SIGKILL)
            EVENTS.append({'event': 'main_closed_pipe_timeout', 'exception': 'subprocess.TimeoutExpired',
                'stdout_bytes': len(output.getvalue()), 'leader_returncode': spawned[0].returncode,
                'json_report_produced': False, 'interpretation': 'confirms S2, not a remediation'})
        finally:
            for child in spawned:
                if child.poll() is None:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait(timeout=2)


if __name__ == '__main__':
    with patch.object(subprocess, 'Popen', side_effect=AssertionError('unapproved process start')):
        result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(FollowupTests))
    with (ROOT / 'security/followup-original.json').open('x') as f:
        json.dump({'tests_run': result.testsRun, 'status': 'PASS' if result.wasSuccessful() else 'FAIL', 'events': EVENTS}, f, indent=2, sort_keys=True)
        f.write('\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
