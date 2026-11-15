"""Read-only inventory/history verification for the replacement transport."""
import argparse
import difflib
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path('/tmp/opencode/c0-native-preflight-2026-10-17')
OUT = ROOT / 'transport-correctness'
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
EXPECTED = 'faf8d794802f7a3b7603bea199689014cef21d415764571725ff9e3b66414baf'


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def tree(path):
    return {str(p.relative_to(path)): sha(p) for p in sorted(path.rglob('*')) if p.is_file()}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--phase', choices=('before', 'after'), required=True)
    args = parser.parse_args()
    assert sha(ROOT / 'transport-source-inventory.json') == EXPECTED
    current = json.loads((ROOT / 'transport-source-inventory.json').read_text())
    previous = json.loads((ROOT / 'remediation-source-inventory.json').read_text())
    original = json.loads((ROOT / 'source-inventory.json').read_text())
    assert current['base'] == previous['base'] == original['base'] == 'babb2180d05698e64b7d9baf37f9cab14b3c0f7f'
    for directory, inventory in (('transport-snapshot', current), ('remediation-snapshot', previous), ('snapshot', original)):
        assert tree(ROOT / directory) == inventory['files'], directory
    assert len(current['files']) == 62
    added = sorted(set(current['files']) - set(previous['files']))
    changed = sorted(name for name in previous['files'] if previous['files'][name] != current['files'].get(name))
    assert added == ['tests/crawl-jobs-v2-observer/engine_metadata.py', 'tests/crawl-jobs-v2-observer/test_engine_metadata.py']
    assert changed == ['docs/crawl-jobs-v2-m4-c0-native-preflight-2026-10-17.md',
        'tests/crawl-jobs-v2-observer/README.md', 'tests/crawl-jobs-v2-observer/host_preflight.py',
        'tests/crawl-jobs-v2-observer/test_host_preflight.py']
    context = sorted(set(original['files']) - set(changed) - {'scripts/preflight-crawl-jobs-v2-native-host.py'})
    assert len(context) == 55
    for name in context:
        raw = subprocess.run(['git', 'show', current['base'] + ':' + name], cwd=REPO,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True).stdout
        assert hashlib.sha256(raw).hexdigest() == current['files'][name], name

    prior = {}
    for directory in ('correctness', 'correctness-final'):
        files = tree(ROOT / directory)
        manifest = json.loads((ROOT / directory / 'review-artifact-sha256.json').read_text())
        assert {k:v for k,v in files.items() if k != 'review-artifact-sha256.json'} == manifest
        prior[directory] = files
    result = {'phase': args.phase, 'source_inventory_sha256': EXPECTED,
        'base': current['base'], 'transport_files_verified': 62,
        'added': added, 'changed_since_intermediate': changed,
        'unchanged_since_intermediate': 56, 'context_files_byte_identical_to_base': 55,
        'prior_review_artifacts': prior, 'all_three_snapshots_match_inventories': True}

    suite_path = ROOT / 'transport-tests-022.json'
    if suite_path.exists():
        report = json.loads(suite_path.read_text())
        assert report['status'] == 'PASS'
        assert len(set(report['inventory'])) == len(report['inventory']) == report['tests_run'] == 318
        assert report['inventory'] == sorted(report['started']) == sorted(report['passed'])
        for key in ('errors', 'failures', 'skipped', 'expected_failures', 'unexpected_successes'):
            assert report[key] == []
        package = {name: value for name, value in current['files'].items() if name.startswith('tests/crawl-jobs-v2-observer/')}
        assert len(package) == 59 and report['source_files'] == package
        assert report['runner_sha256'] == current['files']['scripts/test-crawl-jobs-v2-observer.py']
        old = json.loads((ROOT / 'remediation-tests-022.json').read_text())
        assert set(old['inventory']) <= set(report['inventory'])
        new_ids = sorted(set(report['inventory']) - set(old['inventory']))
        assert len(new_ids) == 14 and all(name.startswith('test_engine_metadata.') for name in new_ids)
        log_path = ROOT / 'transport-tests-022.log'
        log = log_path.read_text()
        ids = []
        for line in log.splitlines():
            if line.endswith(' ... ok') and ' (' in line:
                method, rest = line.split(' (', 1)
                fullname, tail = rest.rsplit(')', 1)
                assert tail == ' ... ok'
                ids.append(fullname if fullname.endswith('.' + method) else fullname + '.' + method)
        assert sorted(ids) == report['inventory']
        assert 'Ran 318 tests' in log and '\nOK\n' in log and 'PASS: C0 offline coverage (318 tests)' in log
        result['supplied_full_suite'] = {'independently_rerun': False, 'reported_passed': 318,
            'prior_ids_retained': 304, 'new_ids': new_ids, 'source_hashes_verified': 59,
            'runner_sha256': report['runner_sha256'], 'json_sha256': sha(suite_path),
            'log_sha256': sha(log_path), 'individual_log_ids_reconciled': True}
    else:
        result['supplied_full_suite'] = {'available': False, 'independently_rerun': False}

    if args.phase == 'before':
        diff = ''.join(''.join(difflib.unified_diff(
            (ROOT / 'remediation-snapshot' / name).read_text().splitlines(keepends=True) if name not in added else [],
            (ROOT / 'transport-snapshot' / name).read_text().splitlines(keepends=True),
            fromfile='intermediate/' + name, tofile='transport/' + name)) for name in sorted(added + changed))
        (OUT / 'transport-diff.patch').write_text(diff)
    else:
        before = json.loads((OUT / 'audit-before.json').read_text())
        assert before['prior_review_artifacts'] == prior
    (OUT / ('audit-' + args.phase + '.json')).write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({key: value for key, value in result.items() if key != 'prior_review_artifacts'}, indent=2))


if __name__ == '__main__':
    main()
