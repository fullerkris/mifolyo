"""Checksum-only guard for the corrected review and preserved original outputs."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
OUT = ROOT / 'security'
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
EXPECTED = '14ac223d10a72ff859d653c29bf721e35af320fc3dbfe823a4c8176180576670'

def digest(path):
    with path.open('rb') as file:
        return hashlib.file_digest(file, 'sha256').hexdigest()

def inventory_tree(root):
    result = {}
    for path in sorted(root.rglob('*')):
        name = path.relative_to(root).as_posix()
        if path.is_symlink():
            result[name] = {'kind': 'symlink', 'target': str(path.readlink())}
        elif path.is_file():
            result[name] = {'kind': 'file', 'bytes': path.stat().st_size, 'sha256': digest(path)}
        elif path.is_dir():
            result[name] = {'kind': 'directory', 'mode': path.stat().st_mode & 0o7777}
        else:
            raise ValueError('Unexpected review input type')
    return result

inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())
files = inventory['files'] | inventory['auxiliary_files']
result = {'inventory_sha256': digest(ROOT / 'source-inventory.json'),
          'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip(),
          'branch': subprocess.check_output(['git', 'branch', '--show-current'], cwd=REPO, text=True).strip(),
          'package_files': len(inventory['files']), 'auxiliary_files': len(inventory['auxiliary_files']),
          'sources': {}, 'retained': {}}
for name, root in [('repository', REPO), ('frozen', ROOT / 'snapshot')]:
    result['sources'][name] = {path: {'sha256': digest(root / path), 'symlink': (root / path).is_symlink()}
                               for path in files}
for arch in ('aarch64', 'x86_64'):
    result['retained'][arch] = inventory_tree(Path('/tmp/opencode/c0-images-' + arch + '-remediation-2026-10-17'))
result['original_security_outputs'] = inventory_tree(Path('/tmp/opencode/c0-runtime-images-2026-10-17/security'))
result['pass'] = (result['inventory_sha256'] == EXPECTED and result['head'] == inventory['base']
                  and result['branch'] == inventory['branch']
                  and all(row['sha256'] == files[path] and not row['symlink']
                          for group in result['sources'].values() for path, row in group.items()))
phase = sys.argv[1]
if phase == 'after':
    before = json.loads((OUT / 'inputs-before.json').read_bytes())
    result['before_after_unchanged'] = all(result[key] == before[key] for key in
        ('inventory_sha256', 'head', 'branch', 'sources', 'retained', 'original_security_outputs'))
    result['pass'] &= result['before_after_unchanged']
elif phase != 'before':
    raise ValueError('Expected before or after')
with (OUT / ('inputs-' + phase + '.json')).open('x') as file:
    json.dump(result, file, sort_keys=True, indent=2)
    file.write('\n')
print(json.dumps({'pass': result['pass'], 'phase': phase, 'inventory_sha256': result['inventory_sha256'],
                  'source_files_per_root': len(files), 'original_output_entries': len(result['original_security_outputs']),
                  'retained_entries': {key: len(value) for key, value in result['retained'].items()}}))
raise SystemExit(0 if result['pass'] else 1)
