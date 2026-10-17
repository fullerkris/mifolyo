"""Read-only source/build inventory verification; writes only security evidence."""
import hashlib
import json
import pathlib
import subprocess
import sys

ROOT = pathlib.Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12')
REPO = pathlib.Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
EXPECTED = '7ed3b955be1fe4d2621a43896d1a3e4d49cf9edcf6e86a0c2dd567d105f14720'

def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())
files = dict(inventory['files'])
files['scripts/test-crawl-jobs-v2-observer.py'] = inventory['runner_sha256']
checks = {}
for label, directory in [('snapshot', ROOT / 'snapshot'), ('repository', REPO)]:
    actual = {name: digest(directory / name) for name in files}
    checks[label] = {'count': len(actual), 'hashes': actual,
                     'mismatches': [name for name, value in files.items() if actual[name] != value]}
result = {'inventory_sha256': digest(ROOT / 'source-inventory.json'),
          'expected_inventory_sha256': EXPECTED, 'checks': checks,
          'build_files': {str(p.relative_to(ROOT)): digest(p)
                          for p in sorted((ROOT / 'final-build').rglob('*')) if p.is_file()},
          'git_head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip(),
          'git_status': subprocess.check_output(['git', 'status', '--porcelain=v1'], cwd=REPO, text=True)}
result['passed'] = result['inventory_sha256'] == EXPECTED and all(not c['mismatches'] for c in checks.values())
if sys.argv[1] == 'after':
    before = json.loads((ROOT / 'security/integrity-before.json').read_bytes())
    result['source_build_and_head_unchanged_since_before'] = all(result[k] == before[k] for k in ('inventory_sha256', 'checks', 'build_files', 'git_head'))
    result['git_status_unchanged_since_before'] = result['git_status'] == before['git_status']
    result['git_status_added_lines'] = sorted(set(result['git_status'].splitlines()) - set(before['git_status'].splitlines()))
    result['git_status_removed_lines'] = sorted(set(before['git_status'].splitlines()) - set(result['git_status'].splitlines()))
    result['passed'] &= result['source_build_and_head_unchanged_since_before']
(ROOT / 'security' / ('integrity-' + sys.argv[1] + '.json')).write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({'passed': result['passed'], 'inventory_sha256': result['inventory_sha256'],
                  'source_entries_per_tree': len(files), 'build_entries': len(result['build_files']),
                  'source_build_and_head_unchanged_since_before': result.get('source_build_and_head_unchanged_since_before'),
                  'git_status_unchanged_since_before': result.get('git_status_unchanged_since_before')}))
raise SystemExit(0 if result['passed'] else 1)
