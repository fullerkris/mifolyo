#!/usr/bin/env python3
"""Read-only frozen-source and working-tree inventory verification."""
import hashlib
import json
from pathlib import Path
import sys

ROOT = Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12')
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
EXPECTED = '7ed3b955be1fe4d2621a43896d1a3e4d49cf9edcf6e86a0c2dd567d105f14720'

def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

inventory_path = ROOT / 'source-inventory.json'
inventory_hash = digest(inventory_path)
inventory = json.loads(inventory_path.read_text())
entries = dict(inventory['files'])
entries['scripts/test-crawl-jobs-v2-observer.py'] = inventory['runner_sha256']
results = {}
for name, root in [('snapshot', ROOT / 'snapshot'), ('repository', REPO)]:
    files = {}
    for relative, expected in entries.items():
        actual = digest(root / relative)
        files[relative] = {'expected': expected, 'actual': actual, 'match': expected == actual}
    results[name] = {'count': len(files), 'pass': all(x['match'] for x in files.values()), 'files': files}
report = {'inventory_sha256': inventory_hash, 'inventory_hash_match': inventory_hash == EXPECTED,
          'results': results}
report['pass'] = report['inventory_hash_match'] and all(x['pass'] for x in results.values())
print(json.dumps(report, indent=2, sort_keys=True))
sys.exit(0 if report['pass'] else 1)
