"""Protect original data; never run any previous review script."""
import json
from pathlib import Path
import subprocess
import sys
from common import OUT, BASE, PACKAGE, REPO, GO, INVENTORY_SHA, REVALIDATION_SHA, files, hashfile, load

phase = sys.argv[1]; assert phase in ('before', 'after')
inv = load(BASE / 'source-inventory.json')
assert hashfile(BASE / 'source-inventory.json') == INVENTORY_SHA
assert hashfile(BASE / 'artifact-revalidation.json') == REVALIDATION_SHA
expected = inv['files'] | inv['auxiliary_files']
rows = {p: {'expected': v, 'repository': hashfile(REPO / p), 'snapshot': hashfile(BASE / 'snapshot' / p)} for p, v in expected.items()}
assert all(r['expected'] == r['repository'] == r['snapshot'] for r in rows.values())
assert files(BASE / 'snapshot') == expected
assert {p: v for p, v in files(REPO / 'tests/crawl-jobs-v2-observer').items() if '__pycache__' not in p.split('/') and not p.endswith('.pyc')} == {p.removeprefix('tests/crawl-jobs-v2-observer/'): v for p, v in inv['files'].items()}
prior = load(GO / 'correctness/inventory-before.json')
roots = [Path(p) for p in prior['protected_roots']] + [GO]
protected = {str(p): files(p) for p in roots}
assert hashfile(GO / 'correctness/review.json') == 'c245393c64a69cdc782b6a1a810c8d575b3aaf37c6f66a7b5bfe657fc5aef0fc'
head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip()
branch = subprocess.check_output(['git', 'branch', '--show-current'], cwd=REPO, text=True).strip()
assert head == inv['base'] and branch == inv['branch']
inputs = {p.name: hashfile(p) for p in BASE.iterdir() if p.is_file()}
report = {'head': head, 'branch': branch, 'inventory_sha256': INVENTORY_SHA, 'revalidation_sha256': REVALIDATION_SHA,
          'source_files': rows, 'protected_roots': protected, 'input_files': inputs, 'match': True}
if phase == 'after':
    before = load(OUT / 'inventory-before.json')
    assert report | {'input_files': {p: inputs[p] for p in before['input_files']}} == before
    report['unreviewed_parent_additions'] = {p: v for p, v in inputs.items() if p not in before['input_files']}
    report['original_inputs_unchanged'] = True
(OUT / ('inventory-' + phase + '.json')).write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
print(json.dumps({'phase': phase, 'source_pairs': len(rows), 'protected_files': sum(map(len, protected.values())),
                  'protected_roots': {p: len(v) for p, v in protected.items()}, 'match': True, 'head': head}, indent=2))
