"""Read-only checksum comparison against the initial retained-artifact check."""
import hashlib
import json
from pathlib import Path

ROOT = Path('/tmp/opencode/c0-runtime-images-2026-10-17')
OUT = ROOT / 'security'
before = json.loads((OUT / 'retained-verification.json').read_bytes())
inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())

def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

checks = {}
for architecture, prior in before['sets'].items():
    root = Path('/tmp/opencode/c0-images-' + architecture + '-pass5-2026-10-17')
    expected = {'image-set.json': prior['image_set_sha256'], **prior['artifacts']}
    record = json.loads((root / 'image-set.json').read_bytes())
    for role, image in prior['images'].items():
        expected[role + '.tar'] = image['rootfs_sha256']
        expected[role + '-image.tar'] = image['portable']['sha256']
        expected[role + '-content.json'] = image['content_sha256']
        expected[role + '-inspect.json'] = record['images'][role]['inspect_sha256']
    for command in record['commands']:
        expected[command['log']] = command['log_sha256']
    for name, value in inventory['files'].items():
        expected['source/' + name.removeprefix('tests/crawl-jobs-v2-observer/')] = value
    checks[architecture] = {name: {'sha256': digest(root / name), 'expected_sha256': value}
                            for name, value in expected.items()}
passed = all(row['sha256'] == row['expected_sha256'] for group in checks.values() for row in group.values())
result = {'pass': passed, 'files_checked': sum(map(len, checks.values())), 'sets': checks}
with (OUT / 'retained-unchanged-after.json').open('x') as file:
    json.dump(result, file, sort_keys=True, indent=2)
    file.write('\n')
print(json.dumps({'pass': passed, 'files_checked': result['files_checked']}))
raise SystemExit(0 if passed else 1)
