"""Test two proposed guards in memory. Never writes/patches frozen sources.

Only image_artifacts.py module definitions are compiled into a detached namespace.
No engine/helper/worker/native code is invoked. Inputs are inert fixtures and
read-only retained image files.
"""
import json
from pathlib import Path
import sys
import tarfile

ROOT = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
OUT = ROOT / 'security'
PACKAGE = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
sys.path.insert(0, str(PACKAGE))
import contracts as c
from oracle_protocol import SOURCE_FILES
from test_rereview import ROOT_OBSERVATION, elf, oracle_fixture

original = (PACKAGE / 'image_artifacts.py').read_text()
first = '            if row["kind"] == "link":\n                links += 1;'
second = '                    name = normalized(name)\n                    require(name not in self.entries'
assert original.count(first) == original.count(second) == 1
patched = original.replace(first,
    '            if row["kind"] == "link":\n                require(not hard_target, "IMAGE_HARDLINK_LINK_TARGET")\n                links += 1;')
patched = patched.replace(second,
    '                    name = normalized(name)\n'
    '                    if name == "/":\n'
    '                        require(member.isdir() and member.size == 0 and not root_seen, "IMAGE_ROOT_DUPLICATE")\n'
    '                        require(self.root is None or self.root == row, "IMAGE_ROOT_CHANGED")\n'
    '                        self.root, root_seen = row, True\n'
    '                        continue\n'
    '                    require(name not in self.entries')
namespace = {'__name__': 'detached_review_image_artifacts'}
exec(compile(patched, '<detached-reviewed-guards>', 'exec'), namespace)
ImageTree, verify = namespace['ImageTree'], namespace['verify_content']

def refusal(call):
    try:
        call()
    except c.Invalid as error:
        return str(error)
    raise AssertionError('Expected refusal')

_, artifacts, sources = oracle_fixture('hardlink', 'src/deep/link')
fixtures = OUT / 'fixtures'
bad_link = refusal(lambda: verify(ImageTree(fixtures / 'hardlink-to-symlink.tar', ROOT_OBSERVATION), 'x86_64', 'oracle', artifacts, sources))
assert bad_link == 'IMAGE_HARDLINK_LINK_TARGET'
bad_root = refusal(lambda: ImageTree(fixtures / 'root-alias-conflict.tar', ROOT_OBSERVATION))
assert bad_root == 'IMAGE_ROOT_CHANGED'
bad_duplicate = refusal(lambda: ImageTree(fixtures / 'root-alias-duplicate.tar'))
assert bad_duplicate == 'IMAGE_ROOT_DUPLICATE'
for kind in ('hardlink', 'link'):
    verify(ImageTree(fixtures / ('ordinary-' + kind + '.tar'), ROOT_OBSERVATION), 'x86_64', 'oracle', artifacts, sources)
for observed in (None, ROOT_OBSERVATION):
    verify(ImageTree(fixtures / 'root-alias-positive.tar', observed), 'x86_64', 'target', {'target': elf()}, {})
result = {'pass': True, 'repository_patch_applied': False, 'in_memory_module_sha256': c.sha(patched.encode()),
          'refusals': {'C0-IMG-SEC-04': bad_link, 'C0-IMG-SEC-05-conflict': bad_root, 'C0-IMG-SEC-05-duplicate': bad_duplicate},
          'positive_hardlink_and_symlink_cases_pass': True, 'root_alias_with_and_without_observation_pass': True,
          'retained_content_records_unchanged': {}, 'hardlink_targets': {}}
sources = {name: (PACKAGE / name).read_bytes() for name in (*SOURCE_FILES, 'build_oracle.py')}
for arch in ('aarch64', 'x86_64'):
    root = Path('/tmp/opencode/c0-images-' + arch + '-remediation-2026-10-17')
    record = json.loads((root / 'image-set.json').read_bytes())
    manifest = json.loads((root / 'oracle/oracle-artifacts.json').read_bytes())
    artifacts = {'target': (root / 'native/target').read_bytes(), 'observer': (root / 'native/observer').read_bytes(),
                 'oracle_elf': manifest['oracle_elf'], 'extensions': manifest['extensions']}
    result['retained_content_records_unchanged'][arch] = []
    result['hardlink_targets'][arch] = {}
    for role, value in record['images'].items():
        tree = ImageTree(root / (role + '.tar'), value['inspection_container']['root_directory'])
        checked = verify(tree, arch, role, artifacts, sources)
        assert c.canonical(checked) == (root / (role + '-content.json')).read_bytes()
        result['retained_content_records_unchanged'][arch].append(role)
        result['hardlink_targets'][arch][role] = {
            name: {'target': row['target'], 'resolved': tree.resolve(name)}
            for name, row in tree.entries.items() if row.get('hardlink')}
        del tree
assert (PACKAGE / 'image_artifacts.py').read_text() == original
with (OUT / 'candidate-fix-results.json').open('x') as file:
    json.dump(result, file, indent=2, sort_keys=True)
    file.write('\n')
print(json.dumps(result, sort_keys=True))
