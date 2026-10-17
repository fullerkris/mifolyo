"""Compare inventoried inputs and retained evidence, ignoring unrelated Git changes."""
import hashlib
import json
from pathlib import Path
import subprocess

OUT = Path(__file__).resolve().parent
BASE = OUT.parent
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')


def sha(p):
    h = hashlib.sha256()
    with p.open('rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


inventory = json.loads((BASE / 'source-inventory.json').read_bytes())
before = json.loads((OUT / 'inventory-before.json').read_bytes())
rows = []
for p, wanted in (inventory['files'] | inventory['auxiliary_files']).items():
    rows.append({'path': p, 'expected': wanted, 'repository': sha(REPO / p), 'snapshot': sha(BASE / 'snapshot' / p)})
report = {'inventory_sha256': sha(BASE / 'source-inventory.json'),
          'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip(),
          'branch': subprocess.check_output(['git', 'branch', '--show-current'], cwd=REPO, text=True).strip(),
          'files': rows}
report['match'] = all(row['expected'] == row['repository'] == row['snapshot'] for row in rows)
assert report['match'] and all(report[k] == before[k] for k in ('inventory_sha256', 'head', 'branch', 'files'))
snapshot_files = {p.relative_to(BASE / 'snapshot').as_posix() for p in (BASE / 'snapshot').rglob('*') if p.is_file()}
assert snapshot_files == set(inventory['files']) | set(inventory['auxiliary_files'])
current_package_files = {p.relative_to(REPO).as_posix() for p in (REPO / 'tests/crawl-jobs-v2-observer').rglob('*') if p.is_file() and '__pycache__' not in p.parts and p.suffix != '.pyc'}
assert current_package_files == set(inventory['files'])
evidence = json.loads((OUT / 'evidence-verification.json').read_bytes())['evidence_hashes']
checked = []
for p, hashes in evidence.items():
    if isinstance(hashes, dict):
        for leaf, wanted in hashes.items():
            assert sha(Path(p) / leaf) == wanted, (p, leaf)
            checked.append(str(Path(p) / leaf))
    else:
        assert sha(Path(p)) == hashes, p
        checked.append(p)
report.update(evidence_files_checked=len(checked), evidence_unchanged=True, snapshot_inventory_exact=True,
              package_inventory_exact=True, unrelated_git_status_not_compared=True)
(OUT / 'inventory-after.json').write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps({k: v for k, v in report.items() if k != 'files'}, indent=2))
