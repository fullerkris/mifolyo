import hashlib
import json
import pathlib
import subprocess
import sys

ROOT = pathlib.Path('/tmp/opencode/c0-runtime-images-2026-10-17')
REPO = pathlib.Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
EXPECTED = 'f789846f36fd23c21cad7b7ef461ec1dbb5521e0ad6d8948ae5cace2fde9dec4'
raw = (ROOT / 'source-inventory.json').read_bytes()
inventory = json.loads(raw)
files = inventory['files'] | inventory['auxiliary_files']
result = {
    'inventory_sha256': hashlib.sha256(raw).hexdigest(),
    'expected_inventory_sha256': EXPECTED,
    'package_files': len(inventory['files']),
    'auxiliary_files': len(inventory['auxiliary_files']),
    'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip(),
    'branch': subprocess.check_output(['git', 'branch', '--show-current'], cwd=REPO, text=True).strip(),
    'roots': {},
}
for label, root in [('repository', REPO), ('frozen', ROOT / 'snapshot')]:
    checked = {}
    for name, expected in files.items():
        path = root / name
        actual = hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None
        checked[name] = {'sha256': actual, 'matches': actual == expected, 'symlink': path.is_symlink()}
    result['roots'][label] = checked
result['pass'] = (
    result['inventory_sha256'] == EXPECTED
    and result['head'] == inventory['base']
    and result['branch'] == inventory['branch']
    and all(row['matches'] and not row['symlink'] for group in result['roots'].values() for row in group.values())
)
destination = ROOT / 'security' / sys.argv[1]
with destination.open('x') as file:
    json.dump(result, file, indent=2, sort_keys=True)
    file.write('\n')
print(json.dumps({key: value for key, value in result.items() if key != 'roots'}, sort_keys=True))
sys.exit(0 if result['pass'] else 1)
