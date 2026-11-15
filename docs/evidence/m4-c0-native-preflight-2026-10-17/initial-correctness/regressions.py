"""Desired-behavior regressions expected to fail on this frozen snapshot.

No Docker/SSH calls or native actors. Only fixture metadata and owned Python
children are used. Test evidence is written beside this file, never to snapshot.
"""
import json
from pathlib import Path
import signal
import sys
import time
import unittest

sys.dont_write_bytecode = True
from controls import FixtureProbe, HostProbe, Deadline, benign_transport, inspect_host

EVIDENCE = []


class Regressions(unittest.TestCase):
    def test_closed_output_pipes_timeout_returns_structured_blocker(self):
        for arch in ('x86_64', 'aarch64'):
            with self.subTest(architecture=arch):
                probe = FixtureProbe(arch)
                probe.docker = HostProbe(arch).docker
                source = 'import os, time; os.close(1); os.close(2); time.sleep(20)'
                row = {'control': 'closed_pipes_then_late_exit', 'architecture': arch}
                start = time.monotonic()
                def action():
                    try:
                        result = inspect_host(arch, probe=probe, deadline=Deadline(0.5))
                        row.update(status=result['status'], blockers=result['blockers'])
                        return result
                    except Exception as error:
                        row.update(exception_type=type(error).__name__, exception=str(error))
                        return None
                result, children = benign_transport(source, action)
                row.update(elapsed_seconds=round(time.monotonic() - start, 6),
                    returncodes=[child.returncode for child in children],
                    local_children_reaped=all(child.returncode is not None for child in children),
                    fallback_cleanup_used=any(child.review_fallback_cleanup_used for child in children),
                    pipes_closed=all(child.stdout.closed and child.stderr.closed for child in children))
                EVIDENCE.append(row)
                self.assertEqual(row['returncodes'], [-signal.SIGKILL])
                self.assertFalse(row['fallback_cleanup_used'])
                self.assertIsNotNone(result, 'Expected BLOCKED JSON result, got ' + row.get('exception_type', 'no result'))
                self.assertEqual(result['status'], 'BLOCKED')
                self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])

    def test_supported_no_new_privileges_metadata_is_not_malformed(self):
        for arch in ('x86_64', 'aarch64'):
            with self.subTest(architecture=arch):
                baseline = inspect_host(arch, probe=FixtureProbe(arch))
                self.assertEqual(baseline['status'], 'HOST_CANDIDATE')
                probe = FixtureProbe(arch)
                probe.info['security_options'].append('name=no-new-privileges')
                result = inspect_host(arch, probe=probe)
                EVIDENCE.append({'control': 'valid_no_new_privileges', 'architecture': arch,
                    'baseline_status': baseline['status'], 'status': result['status'],
                    'blockers': result['blockers'], 'docker_observation': result['observations']['docker']})
                self.assertEqual(result['status'], 'HOST_CANDIDATE', result['blockers'])
                self.assertIs(result['host_admitted'], False)
                self.assertIs(result['execution_authorized'], False)
                self.assertIs(result['runtime_evidence'], False)


if __name__ == '__main__':
    program = unittest.main(verbosity=2, exit=False)
    (Path(__file__).parent / 'regression-evidence.json').write_text(json.dumps(EVIDENCE, indent=2) + '\n')
    raise SystemExit(0 if program.result.wasSuccessful() else 1)
