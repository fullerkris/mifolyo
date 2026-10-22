"""Read-only preservation and final-source continuity guard."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path('/tmp/opencode/c0-runtime-images-final-2026-10-17')
OUT = ROOT / 'security'
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
PRIOR = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
EXPECTED = '83288b8fd89fcbd6453626df75b6b48dd02150fda5def59d0942d92dc6483a6b'
CONTINUITY = '40e1106f132e054fa7654a94bba30ff85a3567b738f057527e24bba21704b851'

def digest(path):
    with path.open('rb') as file:
        return hashlib.file_digest(file, 'sha256').hexdigest()

def tree(root):
    result = {}
    for path in sorted(root.rglob('*')):
        name = path.relative_to(root).as_posix()
        if path.is_symlink(): result[name] = {'kind': 'symlink', 'target': str(path.readlink())}
        elif path.is_file(): result[name] = {'kind': 'file', 'bytes': path.stat().st_size, 'sha256': digest(path)}
        elif path.is_dir(): result[name] = {'kind': 'directory', 'mode': path.stat().st_mode & 0o7777}
        else: raise ValueError('Unexpected input type')
    return result

inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())
previous = json.loads((PRIOR / 'source-inventory.json').read_bytes())
current_files = inventory['files'] | inventory['auxiliary_files']
prior_files = previous['files'] | previous['auxiliary_files']
assert set(current_files) == set(prior_files)
delta = {name: {'before': prior_files[name], 'after': value} for name, value in current_files.items() if value != prior_files[name]}
assert delta == inventory['delta'] and set(delta) == {
    'tests/crawl-jobs-v2-observer/image_artifacts.py', 'tests/crawl-jobs-v2-observer/test_image_review.py'}
result = {'inventory_sha256': digest(ROOT / 'source-inventory.json'), 'delta': delta,
          'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip(),
          'branch': subprocess.check_output(['git', 'branch', '--show-current'], cwd=REPO, text=True).strip(),
          'sources': {}, 'retained': {}, 'prior_review_outputs': {}, 'final_root_evidence': {}}
for label, root in [('repository', REPO), ('frozen', ROOT / 'snapshot')]:
    result['sources'][label] = {name: {'sha256': digest(root / name), 'symlink': (root / name).is_symlink()} for name in current_files}
for architecture in ('aarch64', 'x86_64'):
    result['retained'][architecture] = tree(Path('/tmp/opencode/c0-images-' + architecture + '-remediation-2026-10-17'))
for label, path in [('initial', Path('/tmp/opencode/c0-runtime-images-2026-10-17/security')), ('remediation', PRIOR / 'security')]:
    result['prior_review_outputs'][label] = tree(path)
for name in ('artifact-revalidation.json', 'tests-022.json', 'tests-022.log', 'tests-077.json', 'tests-077.log'):
    result['final_root_evidence'][name] = digest(ROOT / name)
result['pass'] = (result['inventory_sha256'] == EXPECTED and result['head'] == inventory['base']
                  and result['branch'] == inventory['branch']
                  and result['final_root_evidence']['artifact-revalidation.json'] == CONTINUITY
                  and all(row['sha256'] == current_files[name] and not row['symlink']
                          for values in result['sources'].values() for name, row in values.items()))
phase = sys.argv[1]
if phase == 'after':
    before = json.loads((OUT / 'inputs-before.json').read_bytes())
    result['before_after_unchanged'] = all(result[key] == before[key] for key in
        ('inventory_sha256', 'head', 'branch', 'delta', 'sources', 'retained', 'prior_review_outputs', 'final_root_evidence'))
    result['pass'] &= result['before_after_unchanged']
elif phase != 'before': raise ValueError('Expected before or after')
with (OUT / ('inputs-' + phase + '.json')).open('x') as file:
    json.dump(result, file, sort_keys=True, indent=2)
    file.write('\n')
print(json.dumps({'phase': phase, 'pass': result['pass'], 'source_files_per_root': len(current_files),
                  'prior_output_entries': {key: len(value) for key, value in result['prior_review_outputs'].items()},
                  'retained_entries': {key: len(value) for key, value in result['retained'].items()}, 'delta_files': sorted(delta)}))
raise SystemExit(0 if result['pass'] else 1)
