"""Read-only snapshot/evidence audit; outputs only into correctness-final."""
import argparse
import difflib
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path('/tmp/opencode/c0-native-preflight-2026-10-17')
OUT = ROOT / 'correctness-final'
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
INITIAL_SHA = 'aa84e8b932141d6032a70e0f85a3fbc638dabec72992c6cb46a14c3cacc67414'
REMEDIATION_SHA = '4046bd7f3604bdf51ddd50e159a41c394ec9c55e4715cbbfef69e54d2e743ba6'
CHANGED = {
    'tests/crawl-jobs-v2-observer/host_preflight.py',
    'tests/crawl-jobs-v2-observer/test_host_preflight.py',
    'docs/crawl-jobs-v2-m4-c0-native-preflight-2026-10-17.md',
}
FIRST_INCREMENT = CHANGED | {
    'scripts/preflight-crawl-jobs-v2-native-host.py',
    'tests/crawl-jobs-v2-observer/README.md',
}


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def tree(root):
    return {str(path.relative_to(root)): sha(path) for path in sorted(root.rglob('*')) if path.is_file()}


def verify_suite(report, inventory):
    assert report['status'] == 'PASS'
    assert len(set(report['inventory'])) == len(report['inventory']) == report['tests_run']
    assert sorted(report['passed']) == sorted(report['started']) == sorted(report['inventory'])
    for field in ('errors', 'failures', 'skipped', 'expected_failures', 'unexpected_successes'):
        assert report[field] == [], field
    package = {name: value for name, value in inventory['files'].items() if name.startswith('tests/crawl-jobs-v2-observer/')}
    assert report['source_files'] == package
    assert report['runner_sha256'] == inventory['files']['scripts/test-crawl-jobs-v2-observer.py']
    return package


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--phase', choices=('before', 'after'), required=True)
    phase = parser.parse_args().phase
    assert sha(ROOT / 'source-inventory.json') == INITIAL_SHA
    assert sha(ROOT / 'remediation-source-inventory.json') == REMEDIATION_SHA
    initial = json.loads((ROOT / 'source-inventory.json').read_text())
    revised = json.loads((ROOT / 'remediation-source-inventory.json').read_text())
    assert initial['base'] == revised['base'] == 'babb2180d05698e64b7d9baf37f9cab14b3c0f7f'
    assert tree(ROOT / 'snapshot') == initial['files']
    assert tree(ROOT / 'remediation-snapshot') == revised['files']
    assert len(initial['files']) == len(revised['files']) == 60
    assert set(initial['files']) == set(revised['files'])
    changed = sorted(name for name in initial['files'] if initial['files'][name] != revised['files'][name])
    assert set(changed) == CHANGED
    context = sorted(set(initial['files']) - FIRST_INCREMENT)
    for name in context:
        original = subprocess.run(['git', 'show', initial['base'] + ':' + name], cwd=REPO,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True).stdout
        assert hashlib.sha256(original).hexdigest() == revised['files'][name], name

    retained = tree(ROOT / 'correctness')
    old_manifest = json.loads((ROOT / 'correctness/review-artifact-sha256.json').read_text())
    assert {name: value for name, value in retained.items() if name != 'review-artifact-sha256.json'} == old_manifest
    archive = REPO / 'docs/evidence/m4-c0-native-preflight-2026-10-17/initial-correctness'
    assert tree(archive) == retained

    old_tests = json.loads((ROOT / 'tests-022.json').read_text())
    tests = json.loads((ROOT / 'remediation-tests-022.json').read_text())
    verify_suite(old_tests, initial)
    package = verify_suite(tests, revised)
    assert old_tests['tests_run'] == 302 and tests['tests_run'] == 304
    assert set(old_tests['inventory']) < set(tests['inventory'])
    added = sorted(set(tests['inventory']) - set(old_tests['inventory']))
    assert added == [
        'test_host_preflight.HostPreflightTests.test_documented_no_new_privileges_metadata_preserves_other_checks',
        'test_host_preflight.HostPreflightTests.test_wait_timeouts_keep_json_refusal_and_reap_benign_children',
    ]
    log = (ROOT / 'remediation-tests-022.log').read_text()
    assert 'Ran 304 tests' in log and '\nOK\n' in log and 'PASS: C0 offline coverage (304 tests)' in log
    log_passed = []
    for line in log.splitlines():
        if line.endswith(' ... ok') and ' (' in line:
            method, rest = line.split(' (', 1)
            klass, tail = rest.rsplit(')', 1)
            assert tail == ' ... ok'
            log_passed.append(klass if klass.endswith('.' + method) else klass + '.' + method)
    assert sorted(log_passed) == sorted(tests['inventory'])

    result = {
        'phase': phase, 'base': initial['base'],
        'initial_inventory_sha256': INITIAL_SHA,
        'remediation_inventory_sha256': REMEDIATION_SHA,
        'verified_files_each_snapshot': 60,
        'only_changed_files': changed, 'unchanged_since_initial': 57,
        'context_files_byte_identical_to_base': len(context),
        'initial_correctness_artifacts': retained,
        'initial_correctness_archive_identical': True,
        'full_suite_artifact': {
            'independently_rerun': False,
            'json_sha256': sha(ROOT / 'remediation-tests-022.json'),
            'log_sha256': sha(ROOT / 'remediation-tests-022.log'),
            'runner_sha256': tests['runner_sha256'],
            'source_files_bound': len(package), 'source_binding_matches': True,
            'passed': tests['tests_run'], 'all_log_ids_reconciled': True,
            'original_ids_retained': old_tests['tests_run'], 'new_ids': added,
            'failures_errors_skips_expected_failures_unexpected_successes': 0,
        },
    }
    if phase == 'after':
        before = json.loads((OUT / 'audit-before.json').read_text())
        assert {k:v for k,v in result.items() if k != 'phase'} == {k:v for k,v in before.items() if k != 'phase'}
    else:
        diff = ''.join(''.join(difflib.unified_diff(
            (ROOT / 'snapshot' / name).read_text().splitlines(keepends=True),
            (ROOT / 'remediation-snapshot' / name).read_text().splitlines(keepends=True),
            fromfile='initial/' + name, tofile='remediation/' + name)) for name in changed)
        (OUT / 'snapshot-diff.patch').write_text(diff)
    (OUT / ('audit-' + phase + '.json')).write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({k:v for k,v in result.items() if k != 'initial_correctness_artifacts'}, indent=2))


if __name__ == '__main__':
    main()
