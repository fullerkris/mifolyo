"""Recheck reviewed bytes and preserve all original review outputs exactly."""
import json
import subprocess
from review_common import OUT, BASE, PACKAGE, REPO, OLD, files, hashfile, load

before = load(OUT / 'inventory-before.json')
inventory = load(BASE / 'source-inventory.json')
rows = []
for p, wanted in (inventory['files'] | inventory['auxiliary_files']).items():
    rows.append({'path': p, 'expected': wanted, 'repository': hashfile(REPO / p), 'snapshot': hashfile(BASE / 'snapshot' / p)})
report = {'inventory_sha256': hashfile(BASE / 'source-inventory.json'),
          'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip(),
          'branch': subprocess.check_output(['git', 'branch', '--show-current'], cwd=REPO, text=True).strip(),
          'files': rows, 'original_review_files': files(OLD)}
report['match'] = all(r['expected'] == r['repository'] == r['snapshot'] for r in rows)
assert report['match'] and all(report[k] == before[k] for k in ('inventory_sha256', 'head', 'branch', 'files', 'original_review_files'))
assert files(BASE / 'snapshot') == inventory['files'] | inventory['auxiliary_files']
repo_package = {p: h for p, h in files(REPO / 'tests/crawl-jobs-v2-observer').items() if '__pycache__' not in p.split('/') and not p.endswith('.pyc')}
assert repo_package == {p.removeprefix('tests/crawl-jobs-v2-observer/'): h for p, h in inventory['files'].items()}
evidence = load(OUT / 'actual-verification.json')['evidence_hashes']
count = 0
for p, hashes in evidence.items():
    from pathlib import Path
    if isinstance(hashes, dict):
        assert files(Path(p)) == hashes, p
        count += len(hashes)
    else:
        assert hashfile(Path(p)) == hashes, p
        count += 1
report.update(evidence_files_unchanged=count, original_review_file_count=len(report['original_review_files']),
              original_review_unchanged=True, snapshot_inventory_exact=True, package_inventory_exact=True,
              unrelated_git_status_not_compared=True)
(OUT / 'inventory-after.json').write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps({k: v for k, v in report.items() if k not in ('files', 'original_review_files')}, indent=2))
