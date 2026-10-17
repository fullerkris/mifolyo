"""Pure in-memory candidate validation; no frozen/repository edits or execution."""
import json
from pathlib import Path
import sys

ROOT = Path('/tmp/opencode/c0-runtime-images-final-2026-10-17')
OUT = ROOT / 'security'
PACKAGE = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
sys.path.insert(0, str(PACKAGE))
import contracts as c
from oracle_protocol import SOURCE_FILES
from test_final import GOOD_ROOT, elf

original = (PACKAGE / 'image_artifacts.py').read_text()
needle = '        self.total_bytes = total\n'
assert original.count(needle) == 1
candidate = original.replace(needle, needle +
    '        for name, row in self.entries.items():\n'
    '            if row.get("hardlink"):\n'
    '                self.resolve(name)\n')
namespace = {'__name__': 'detached_image_artifacts'}
exec(compile(candidate, '<detached-all-hardlink-validation>', 'exec'), namespace)
ImageTree, verify = namespace['ImageTree'], namespace['verify_content']
fixtures = OUT / 'repeat-02/fixtures'
try:
    ImageTree(fixtures / 'unused-hardlink-conflict.tar', GOOD_ROOT)
except c.Invalid as error:
    assert str(error) == 'IMAGE_HARDLINK_METADATA'
else:
    raise AssertionError('Unreferenced conflicting hardlink was not refused')
positive = verify(ImageTree(fixtures / 'unused-hardlink-positive.tar', GOOD_ROOT), 'x86_64', 'target', {'target': elf()}, {})
for mode in (0o644, 0o755):
    assert ImageTree(fixtures / ('hardlink-matching-%d.tar' % mode), GOOD_ROOT).file('/alias') == b'inert'
result = {'pass': True, 'repository_or_frozen_patch_applied': False,
          'candidate_module_sha256': c.sha(candidate.encode()),
          'bad_unused_alias_refusal': 'IMAGE_HARDLINK_METADATA',
          'matching_unused_alias_content_sha256': c.sha(c.canonical(positive)),
          'readable_and_executable_matching_aliases_pass': True,
          'retained_content_records_unchanged': {}, 'real_hardlinks_validated': {}}
sources = {name: (PACKAGE / name).read_bytes() for name in (*SOURCE_FILES, 'build_oracle.py')}
for arch in ('aarch64', 'x86_64'):
    root = Path('/tmp/opencode/c0-images-' + arch + '-remediation-2026-10-17')
    record = json.loads((root / 'image-set.json').read_bytes())
    oracle = json.loads((root / 'oracle/oracle-artifacts.json').read_bytes())
    artifacts = {'target': (root / 'native/target').read_bytes(), 'observer': (root / 'native/observer').read_bytes(),
                 'oracle_elf': oracle['oracle_elf'], 'extensions': oracle['extensions']}
    result['retained_content_records_unchanged'][arch] = []
    result['real_hardlinks_validated'][arch] = {}
    for role, metadata in record['images'].items():
        tree = ImageTree(root / (role + '.tar'), metadata['inspection_container']['root_directory'])
        content = verify(tree, arch, role, artifacts, sources)
        assert c.canonical(content) == (root / (role + '-content.json')).read_bytes()
        result['retained_content_records_unchanged'][arch].append(role)
        result['real_hardlinks_validated'][arch][role] = {
            name: tree.resolve(name) for name, row in tree.entries.items() if row.get('hardlink')}
        del tree
assert (PACKAGE / 'image_artifacts.py').read_text() == original
with (OUT / 'candidate-fix-results.json').open('x') as file:
    json.dump(result, file, sort_keys=True, indent=2)
    file.write('\n')
print(json.dumps(result, sort_keys=True))
