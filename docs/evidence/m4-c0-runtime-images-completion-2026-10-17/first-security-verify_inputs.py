"""Checksum-only preservation guard; no engine or native operations."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path('/tmp/opencode/c0-runtime-images-completion-2026-10-17')
OUT = ROOT / 'security'
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
LAST = Path('/tmp/opencode/c0-runtime-images-final-2026-10-17')
BUILD = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
EXPECTED = '2afe7326f090a3b8ba3ef882a8b3fa89d3e4dd73a9c8c074288d541364487043'
CONTINUITY = '55a7c9f9738d942a745ab47626afe9ef157998039a2d2e8779bee24f992aa3fd'

def digest(path):
    with path.open('rb') as file: return hashlib.file_digest(file, 'sha256').hexdigest()

def inventory_tree(root):
    result = {}
    for path in sorted(root.rglob('*')):
        name = path.relative_to(root).as_posix()
        if path.is_symlink(): result[name] = {'kind': 'symlink', 'target': str(path.readlink())}
        elif path.is_file(): result[name] = {'kind': 'file', 'bytes': path.stat().st_size, 'sha256': digest(path)}
        elif path.is_dir(): result[name] = {'kind': 'directory', 'mode': path.stat().st_mode & 0o7777}
        else: raise ValueError('Unexpected input kind')
    return result

inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())
current = inventory['files'] | inventory['auxiliary_files']
baseline = json.loads((BUILD / 'source-inventory.json').read_bytes())
baseline = baseline['files'] | baseline['auxiliary_files']
previous = json.loads((LAST / 'source-inventory.json').read_bytes())
previous = previous['files'] | previous['auxiliary_files']
assert set(current) == set(baseline) == set(previous)
build_delta = {name: {'before': baseline[name], 'after': value} for name, value in current.items() if baseline[name] != value}
last_delta = {name: {'before': previous[name], 'after': value} for name, value in current.items() if previous[name] != value}
assert build_delta == inventory['delta']
assert set(build_delta) == set(last_delta) == {'tests/crawl-jobs-v2-observer/image_artifacts.py', 'tests/crawl-jobs-v2-observer/test_image_review.py'}
old_code = (LAST / 'snapshot/tests/crawl-jobs-v2-observer/image_artifacts.py').read_text()
new_code = (ROOT / 'snapshot/tests/crawl-jobs-v2-observer/image_artifacts.py').read_text()
insert = ('        # Every alias shares inode metadata, even if the selected role never\n'
          '        # traverses it. Wait for the complete map so forward targets are valid.\n'
          '        for name, row in self.entries.items():\n'
          '            if row.get("hardlink"):\n'
          '                self.resolve(name)\n')
assert old_code.replace('        self.total_bytes = total\n', '        self.total_bytes = total\n' + insert) == new_code
result = {'inventory_sha256': digest(ROOT / 'source-inventory.json'), 'delta_from_build': build_delta,
          'delta_from_last_review': last_delta, 'validator_change_is_only_global_alias_pass': True,
          'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip(),
          'branch': subprocess.check_output(['git', 'branch', '--show-current'], cwd=REPO, text=True).strip(),
          'sources': {}, 'retained': {}, 'prior_outputs': {}, 'root_evidence': {}}
for label, path in [('repository', REPO), ('frozen', ROOT / 'snapshot')]:
    result['sources'][label] = {name: {'sha256': digest(path / name), 'symlink': (path / name).is_symlink()} for name in current}
for arch in ('aarch64', 'x86_64'):
    result['retained'][arch] = inventory_tree(Path('/tmp/opencode/c0-images-' + arch + '-remediation-2026-10-17'))
for label, path in [('initial', Path('/tmp/opencode/c0-runtime-images-2026-10-17/security')),
                    ('remediation', BUILD / 'security'), ('final', LAST / 'security')]:
    result['prior_outputs'][label] = inventory_tree(path)
for name in ('artifact-revalidation.json', 'tests-022.json', 'tests-022.log', 'tests-077.json', 'tests-077.log'):
    result['root_evidence'][name] = digest(ROOT / name)
result['pass'] = (result['inventory_sha256'] == EXPECTED and result['head'] == inventory['base']
                  and result['branch'] == inventory['branch'] and result['root_evidence']['artifact-revalidation.json'] == CONTINUITY
                  and all(row['sha256'] == current[name] and not row['symlink'] for values in result['sources'].values() for name, row in values.items()))
phase = sys.argv[1]
if phase == 'after':
    before = json.loads((OUT / 'inputs-before.json').read_bytes())
    result['before_after_unchanged'] = all(result[key] == before[key] for key in
        ('inventory_sha256', 'delta_from_build', 'delta_from_last_review', 'head', 'branch', 'sources', 'retained', 'prior_outputs', 'root_evidence'))
    result['pass'] &= result['before_after_unchanged']
elif phase != 'before': raise ValueError('Expected before or after')
with (OUT / ('inputs-' + phase + '.json')).open('x') as file:
    json.dump(result, file, sort_keys=True, indent=2); file.write('\n')
print(json.dumps({'phase': phase, 'pass': result['pass'], 'sources_per_root': len(current),
                  'prior_output_entries': {key: len(value) for key, value in result['prior_outputs'].items()},
                  'retained_entries': {key: len(value) for key, value in result['retained'].items()}}))
raise SystemExit(0 if result['pass'] else 1)
