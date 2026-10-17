"""Protect frozen inputs and every prior review/source/artifact root."""
import json
from pathlib import Path
import subprocess
import sys
from common import OUT, BASE, REPO, PREVIOUS, INVENTORY_SHA, REVALIDATION_SHA, files, hashfile, load

phase = sys.argv[1]; assert phase in ('before', 'after')
inventory = load(BASE / 'source-inventory.json')
assert hashfile(BASE / 'source-inventory.json') == INVENTORY_SHA
assert hashfile(BASE / 'artifact-revalidation.json') == REVALIDATION_SHA
expected = inventory['files'] | inventory['auxiliary_files']
rows = {path: {'expected': value, 'repository': hashfile(REPO / path), 'snapshot': hashfile(BASE / 'snapshot' / path)} for path, value in expected.items()}
assert all(row['expected'] == row['repository'] == row['snapshot'] for row in rows.values())
assert files(BASE / 'snapshot') == expected
assert {path: value for path, value in files(REPO / 'tests/crawl-jobs-v2-observer').items() if '__pycache__' not in path.split('/') and not path.endswith('.pyc')} == {path.removeprefix('tests/crawl-jobs-v2-observer/'): value for path, value in inventory['files'].items()}
prior = load(PREVIOUS / 'correctness/inventory-before.json')
roots = [Path(path) for path in prior['protected_roots']] + [PREVIOUS]
protected = {str(root): files(root) for root in roots}
assert hashfile(PREVIOUS / 'correctness/review.json') == '12559ac96078b1f18b202debb86772480ada3330c6c17b75e220c11b7ce51990'
head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip()
branch = subprocess.check_output(['git', 'branch', '--show-current'], cwd=REPO, text=True).strip()
assert head == inventory['base'] and branch == inventory['branch']
inputs = {path.name: hashfile(path) for path in BASE.iterdir() if path.is_file()}
report = {'head': head, 'branch': branch, 'inventory_sha256': INVENTORY_SHA, 'artifact_revalidation_sha256': REVALIDATION_SHA,
          'source_files': rows, 'protected_roots': protected, 'input_files': inputs, 'match': True}
if phase == 'after':
    before = load(OUT / 'inventory-before.json')
    assert report | {'input_files': {path: inputs[path] for path in before['input_files']}} == before
    report['original_inputs_unchanged'] = True
    report['unreviewed_parent_additions'] = {path: value for path, value in inputs.items() if path not in before['input_files']}
(OUT / ('inventory-' + phase + '.json')).write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
print(json.dumps({'phase': phase, 'source_pairs': len(rows), 'protected_files': sum(map(len, protected.values())),
                  'protected_roots': {path: len(value) for path, value in protected.items()}, 'match': True, 'head': head}, indent=2))
