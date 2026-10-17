"""Hash final sources and protected previous review/source/artifact roots."""
import json
from pathlib import Path
import subprocess
import sys
from common import OUT, BASE, REPO, FIRST, PREVIOUS, DURABLE, INVENTORY_SHA, REVALIDATION_SHA, files, hashfile, image_dir, load

mode = sys.argv[1]
assert mode in ('before', 'after')
inv = load(BASE / 'source-inventory.json')
assert hashfile(BASE / 'source-inventory.json') == INVENTORY_SHA
assert hashfile(BASE / 'artifact-revalidation.json') == REVALIDATION_SHA
all_sources = inv['files'] | inv['auxiliary_files']
rows = {p: {'expected': v, 'repository': hashfile(REPO / p), 'snapshot': hashfile(BASE / 'snapshot' / p)} for p, v in all_sources.items()}
assert all(r['expected'] == r['repository'] == r['snapshot'] for r in rows.values())
assert files(BASE / 'snapshot') == all_sources
current = {p: v for p, v in files(REPO / 'tests/crawl-jobs-v2-observer').items() if '__pycache__' not in p.split('/') and not p.endswith('.pyc')}
assert current == {p.removeprefix('tests/crawl-jobs-v2-observer/'): v for p, v in inv['files'].items()}
roots = [FIRST, PREVIOUS, DURABLE, *[image_dir(a) for a in ('aarch64', 'x86_64')],
         *[Path(f'/tmp/opencode/c0-images-{a}-pass5-2026-10-17') for a in ('aarch64', 'x86_64')]]
protected = {str(root): files(root) for root in roots}
assert all(protected.values())
final_inputs = {p.name: hashfile(p) for p in BASE.iterdir() if p.is_file()}
head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=REPO, text=True).strip()
branch = subprocess.check_output(['git', 'branch', '--show-current'], cwd=REPO, text=True).strip()
assert head == inv['base'] and branch == inv['branch']
report = {'head': head, 'branch': branch, 'inventory_sha256': INVENTORY_SHA, 'artifact_revalidation_sha256': REVALIDATION_SHA,
          'source_files': rows, 'protected_roots': protected, 'final_inputs': final_inputs, 'match': True,
          'snapshot_inventory_exact': True, 'package_inventory_exact': True, 'unrelated_git_status_compared': False}
if mode == 'after':
    before = load(OUT / 'inventory-before.json')
    # Other sessions may add sibling report/export utilities outside the frozen
    # inventory. Never replace the baseline: require every originally present
    # input and every protected prior file to remain identical, and disclose adds.
    original_inputs = {name: final_inputs[name] for name in before['final_inputs']}
    assert report | {'final_inputs': original_inputs} == before
    report['original_inputs_unchanged'] = True
    report['unreviewed_new_parent_files'] = {name: value for name, value in final_inputs.items() if name not in before['final_inputs']}
(OUT / ('inventory-' + mode + '.json')).write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
print(json.dumps({'phase': mode, 'source_files': len(rows), 'protected_file_count': sum(map(len, protected.values())),
                  'protected_roots': {p: len(v) for p, v in protected.items()}, 'match': True,
                  'head': head, 'inventory_sha256': INVENTORY_SHA, 'artifact_revalidation_sha256': REVALIDATION_SHA}, indent=2))
