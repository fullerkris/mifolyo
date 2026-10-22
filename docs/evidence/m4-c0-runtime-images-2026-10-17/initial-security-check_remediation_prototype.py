"""Offline compatibility checks of detached helpers, not a repository patch."""
import json
from pathlib import Path
import sys
import tarfile

ROOT = Path('/tmp/opencode/c0-runtime-images-2026-10-17')
OUT = ROOT / 'security'
PACKAGE = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
sys.path.insert(0, str(PACKAGE))
from contracts import Invalid, canonical, sha
from image_artifacts import ImageTree, normalized, verify_content
from oracle_protocol import SOURCE_FILES
from remediation_prototype import (absolute_link_target, check_root_member_budget,
                                   check_unique_portable_members, resolve_components)


def with_raw_link_targets(path):
    # Test adapter for already-known private fixtures/retained files. Production
    # must retain the raw link target during ImageTree's original single-FD read,
    # not use this two-pass test adapter at an untrusted file boundary.
    tree = ImageTree(path)
    parent_links = 0
    with tarfile.open(path, 'r:') as archive:
        for member in archive:
            if member.issym() or member.islnk():
                name = normalized(member.name.rstrip('/'))
                tree.entries[name]['target'] = absolute_link_target(name, member.linkname, member.issym())
                parent_links += '..' in member.linkname.split('/')
    tree.resolve = lambda path: resolve_components(tree.entries, path)
    return tree, parent_links


result = {'mode': 'detached-test-only-not-applied', 'fixtures': {}, 'retained_sets': {}}
fixtures = OUT / 'inert-fixtures'
for name, guard in [('portable-config-alias.tar', check_unique_portable_members),
                    ('repeated-root-headers.tar', check_root_member_budget)]:
    try:
        with tarfile.open(fixtures / name, 'r:') as archive:
            guard(archive)
    except Invalid as error:
        result['fixtures'][name] = {'refused': True, 'reason': str(error)}
    else:
        raise AssertionError('Regression fixture was not refused')
tree, _ = with_raw_link_targets(fixtures / 'oracle-link-dotdot.tar')
assert tree.resolve('/oracle/worker.py') == '/foreign/worker.py'
result['fixtures']['oracle-link-dotdot.tar'] = {
    'correct_resolved_path': tree.resolve('/oracle/worker.py'),
    'correct_source_sha256': sha(tree.file('/oracle/worker.py'))}
sources = {name: (PACKAGE / name).read_bytes() for name in (*SOURCE_FILES, 'build_oracle.py')}
for architecture in ('aarch64', 'x86_64'):
    root = Path('/tmp/opencode/c0-images-' + architecture + '-pass5-2026-10-17')
    entry = {'portable_member_counts': {}, 'rootfs_member_counts': {}}
    for role in ('target', 'observer', 'oracle'):
        with tarfile.open(root / (role + '-image.tar'), 'r:') as archive:
            entry['portable_member_counts'][role] = check_unique_portable_members(archive)
        with tarfile.open(root / (role + '.tar'), 'r:') as archive:
            entry['rootfs_member_counts'][role] = check_root_member_budget(archive)
    tree, parent_links = with_raw_link_targets(root / 'oracle.tar')
    manifest = json.loads((root / 'oracle/oracle-artifacts.json').read_bytes())
    artifacts = {key: manifest[key] for key in ('oracle_elf', 'extensions')}
    content = verify_content(tree, architecture, 'oracle', artifacts, sources)
    assert canonical(content) == (root / 'oracle-content.json').read_bytes()
    entry['oracle_content_unchanged'] = True
    entry['parent_component_links_preserved'] = parent_links
    result['retained_sets'][architecture] = entry
result['pass'] = True
with (OUT / 'remediation-prototype-results.json').open('x') as file:
    json.dump(result, file, indent=2, sort_keys=True)
    file.write('\n')
print(json.dumps(result, sort_keys=True))
