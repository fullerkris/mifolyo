"""In-memory-only parent binding check, with retained content compatibility."""
import json
from pathlib import Path
import sys

ROOT = Path('/tmp/opencode/c0-runtime-images-completion-2026-10-17')
OUT = ROOT / 'security'
PACKAGE = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
sys.path.insert(0, str(PACKAGE))
import contracts as c
from oracle_protocol import SOURCE_FILES
from test_image_artifacts import elf
from test_completion import GOOD_ROOT

original = (PACKAGE / 'image_artifacts.py').read_text()
needle = '            if row.get("hardlink"):\n                self.resolve(name)\n'
assert original.count(needle) == 1
candidate = original.replace(needle,
    '            if row.get("hardlink"):\n'
    '                parent = str(PurePosixPath(name).parent)\n'
    '                require(self.resolve(parent) == parent, "IMAGE_HARDLINK_PARENT_ALIAS")\n'
    '                self.resolve(name)\n')
namespace = {'__name__': 'detached_archive_guard'}
exec(compile(candidate, '<detached-hardlink-parent-binding>', 'exec'), namespace)
ImageTree, verify = namespace['ImageTree'], namespace['verify_content']
fixtures = OUT / 'repeat-02/fixtures'
results = {}
for name in ('parent-redirect-conflict.tar', 'parent-redirect-positive.tar'):
    try: ImageTree(fixtures / name, GOOD_ROOT)
    except c.Invalid as error:
        assert str(error) == 'IMAGE_HARDLINK_PARENT_ALIAS'; results[name] = str(error)
    else: raise AssertionError('Redirected hardlink parent was not refused')
for name in ('metadata-False-0.tar', 'metadata-True-0.tar', 'literal-parent-positive.tar'):
    verify(ImageTree(fixtures / name, GOOD_ROOT), 'x86_64', 'target', {'target': elf()}, {})
assert ImageTree(fixtures / 'forward-directory.tar', GOOD_ROOT).file('/alias') == b'inert'
record = {'pass': True, 'repository_or_frozen_patch_applied': False, 'candidate_sha256': c.sha(candidate.encode()),
          'redirected_parent_refusals': results, 'direct_and_forward_positive_cases_pass': True,
          'retained_content_records_byte_identical': {}}
sources = {name: (PACKAGE / name).read_bytes() for name in (*SOURCE_FILES, 'build_oracle.py')}
for architecture in ('aarch64', 'x86_64'):
    root = Path('/tmp/opencode/c0-images-' + architecture + '-remediation-2026-10-17')
    image_set = json.loads((root / 'image-set.json').read_bytes())
    oracle = json.loads((root / 'oracle/oracle-artifacts.json').read_bytes())
    artifacts = {'target': (root / 'native/target').read_bytes(), 'observer': (root / 'native/observer').read_bytes(),
                 'oracle_elf': oracle['oracle_elf'], 'extensions': oracle['extensions']}
    record['retained_content_records_byte_identical'][architecture] = []
    for role, metadata in image_set['images'].items():
        tree = ImageTree(root / (role + '.tar'), metadata['inspection_container']['root_directory'])
        content = verify(tree, architecture, role, artifacts, sources)
        assert c.canonical(content) == (root / (role + '-content.json')).read_bytes()
        record['retained_content_records_byte_identical'][architecture].append(role)
        del tree
assert (PACKAGE / 'image_artifacts.py').read_text() == original
with (OUT / 'candidate-fix-results.json').open('x') as file:
    json.dump(record, file, sort_keys=True, indent=2); file.write('\n')
print(json.dumps(record, sort_keys=True))
