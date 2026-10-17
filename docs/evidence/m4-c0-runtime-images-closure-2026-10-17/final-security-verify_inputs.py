"""Read-only source, evidence and prior-output preservation checks."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path('/tmp/opencode/c0-runtime-images-closure-2026-10-17')
OUT = ROOT / 'security'
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
LAST = Path('/tmp/opencode/c0-runtime-images-completion-2026-10-17')
BUILD = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
EXPECTED = '1f0e82a5ca98f02bf36deb4292501a0b97a38ac8bf567fd170fc09c8d4890376'
CONTINUITY = 'a8f6aee7dee482c03b925bef48a57e2ed5eb5d46edc61ea11f67c9790b5211f7'

def digest(path):
    with path.open('rb') as file: return hashlib.file_digest(file, 'sha256').hexdigest()

def tree(root):
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
previous = json.loads((LAST / 'source-inventory.json').read_bytes())
previous = previous['files'] | previous['auxiliary_files']
baseline = json.loads((BUILD / 'source-inventory.json').read_bytes())
baseline = baseline['files'] | baseline['auxiliary_files']
assert set(current) == set(previous) == set(baseline)
last_delta = {name: {'before': previous[name], 'after': value} for name, value in current.items() if previous[name] != value}
build_delta = {name: {'before': baseline[name], 'after': value} for name, value in current.items() if baseline[name] != value}
assert build_delta == inventory['delta']
assert set(last_delta) == set(build_delta) == {'tests/crawl-jobs-v2-observer/image_artifacts.py', 'tests/crawl-jobs-v2-observer/test_image_review.py'}
old = (LAST / 'snapshot/tests/crawl-jobs-v2-observer/image_artifacts.py').read_text()
new = (ROOT / 'snapshot/tests/crawl-jobs-v2-observer/image_artifacts.py').read_text()
block = ('        # Flattened filesystem entries must have literal directory ancestors.\n'
         '        # Otherwise a name lookup could inspect a different, shadowing entry.\n'
         '        for name in self.entries:\n'
         '            parent = str(PurePosixPath(name).parent)\n'
         '            require(parent == "/" or self.entries.get(parent, {}).get("kind") == "directory", "IMAGE_MEMBER_PARENT")\n')
assert old.replace('        self.total_bytes = total\n', '        self.total_bytes = total\n' + block) == new
result = {'inventory_sha256': digest(ROOT / 'source-inventory.json'), 'delta_from_previous_review': last_delta,
          'delta_from_build': build_delta, 'validator_delta_is_exact_parent_pass': True,
          'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip(),
          'branch': subprocess.check_output(['git', 'branch', '--show-current'], cwd=REPO, text=True).strip(),
          'sources': {}, 'prior_outputs': {}, 'retained': {}, 'root_evidence': {}}
for label, root in [('repository', REPO), ('frozen', ROOT / 'snapshot')]:
    result['sources'][label] = {name: {'sha256': digest(root / name), 'symlink': (root / name).is_symlink()} for name in current}
for label, root in [('initial', Path('/tmp/opencode/c0-runtime-images-2026-10-17')),
                    ('remediation', BUILD), ('final', Path('/tmp/opencode/c0-runtime-images-final-2026-10-17')), ('completion', LAST)]:
    result['prior_outputs'][label] = tree(root / 'security')
for architecture in ('aarch64', 'x86_64'):
    result['retained'][architecture] = tree(Path('/tmp/opencode/c0-images-' + architecture + '-remediation-2026-10-17'))
for name in ('artifact-revalidation.json', 'tests-022.json', 'tests-022.log', 'tests-077.json', 'tests-077.log'):
    result['root_evidence'][name] = digest(ROOT / name)
result['pass'] = (result['inventory_sha256'] == EXPECTED and result['root_evidence']['artifact-revalidation.json'] == CONTINUITY
                  and result['head'] == inventory['base'] and result['branch'] == inventory['branch']
                  and all(row['sha256'] == current[name] and not row['symlink'] for values in result['sources'].values() for name, row in values.items()))
phase = sys.argv[1]
if phase == 'after':
    before = json.loads((OUT / 'inputs-before.json').read_bytes())
    result['before_after_unchanged'] = all(result[key] == before[key] for key in
        ('inventory_sha256', 'delta_from_previous_review', 'delta_from_build', 'head', 'branch', 'sources', 'prior_outputs', 'retained', 'root_evidence'))
    result['pass'] &= result['before_after_unchanged']
elif phase != 'before': raise ValueError('Expected before or after')
with (OUT / ('inputs-' + phase + '.json')).open('x') as file:
    json.dump(result, file, sort_keys=True, indent=2); file.write('\n')
print(json.dumps({'phase': phase, 'pass': result['pass'], 'sources_per_root': len(current),
                  'prior_output_entries': {key: len(value) for key, value in result['prior_outputs'].items()},
                  'retained_entries': {key: len(value) for key, value in result['retained'].items()}}))
raise SystemExit(0 if result['pass'] else 1)
