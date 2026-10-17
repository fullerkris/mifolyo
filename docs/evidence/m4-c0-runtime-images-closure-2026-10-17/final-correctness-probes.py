"""COR-007 exact reproductions and literal-ancestry controls, using inert data."""
import json
import sys
from common import OUT, PACKAGE, PREVIOUS, ROOT, elf, fixture

sys.path.insert(0, str(PACKAGE))
from image_artifacts import ImageTree, verify_content

results = []


def check(label, operation, accepted, reason=None):
    try:
        value = operation()
        observed = {'accepted': True}
        if isinstance(value, dict): observed['value'] = value
    except Exception as error:
        observed = {'accepted': False, 'exception': type(error).__name__, 'reason': str(error)}
    passed = observed['accepted'] == accepted and (reason is None or observed.get('reason') == reason)
    results.append({'id': label, 'expected_accept': accepted, 'expected_reason': reason, 'passed': passed, 'observed': observed})
    return observed


def native(path, architecture):
    tree = ImageTree(path)
    content = verify_content(tree, architecture, 'target', {'target': elf(architecture)}, {})
    return {'root': tree.root, 'entries': len(tree.entries), 'payload_sha256': content['payload_sha256']}


def access(path, query, expected):
    tree = ImageTree(path)
    assert tree.file(query) == expected
    return {'root': tree.root, 'resolved': tree.resolve(query), 'bytes_match': True}


for arch in ('aarch64', 'x86_64'):
    target = ('target', 'file', elf(arch), 0o755, 0, 0)
    # Read the exact prior counterexamples, never rewrite them.
    for variant in ('mode', 'uid', 'gid', 'missing-target'):
        old = PREVIOUS / 'correctness/inert-fixtures' / f'{arch}-shadowed-{variant}.tar'
        check(f'{arch}-exact-COR007-{variant}', lambda old=old: ImageTree(old), False, 'IMAGE_MEMBER_PARENT')
    # Corresponding old direct-hardlink positive/negative cases retain meaning.
    for forward in (False, True):
        for variant in ('valid', 'mode', 'uid', 'gid'):
            old = PREVIOUS / 'correctness/inert-fixtures' / f'{arch}-direct-{forward}-{variant}.tar'
            check(f'{arch}-old-direct-{forward}-{variant}', lambda old=old: native(old, arch), variant == 'valid',
                  None if variant == 'valid' else 'IMAGE_HARDLINK_METADATA')
    # Every entry kind gets the same ancestry rule, not merely hardlinks.
    for kind, value in (('hardlink', 'target'), ('file', b'inert shadow'), ('directory', ''), ('link', '../target')):
        for parent_kind, parent_value in (('link', '.'), ('file', b'not a directory')):
            path = fixture(f'{arch}-parent-{parent_kind}-child-{kind}', [target, ('parent', parent_kind, parent_value, 0o755, 0, 0),
                         ('parent/entry', kind, value, 0o755, 0, 0)])
            check(f'{arch}-parent-{parent_kind}-child-{kind}', lambda path=path: ImageTree(path), False, 'IMAGE_MEMBER_PARENT')
        path = fixture(f'{arch}-missing-parent-{kind}', [target, ('missing/entry', kind, value, 0o755, 0, 0)])
        check(f'{arch}-missing-parent-{kind}', lambda path=path: ImageTree(path), False, 'IMAGE_MEMBER_PARENT')
    # Child's immediate parent can be a directory while a higher ancestor is
    # invalid. Deliberately store the deepest child first.
    for invalid in ('missing', 'link', 'file'):
        rows = [('ancestor/middle/alias', 'hardlink', 'target', 0o755, 0, 0),
                ('ancestor/middle', 'directory', '', 0o755, 0, 0), target]
        if invalid != 'missing':
            rows.append(('ancestor', invalid, '.' if invalid == 'link' else b'inert', 0o755, 0, 0))
        path = fixture(f'{arch}-higher-ancestor-{invalid}', rows)
        check(f'{arch}-higher-ancestor-{invalid}', lambda path=path: ImageTree(path), False, 'IMAGE_MEMBER_PARENT')
    # Literal parents and targets may come before OR after their children.
    dirs = [('literal', 'directory', '', 0o755, 0, 0), ('literal/deep', 'directory', '', 0o755, 0, 0)]
    alias = ('literal/deep/alias', 'hardlink', 'target', 0o755, 0, 0)
    for parent_first in (False, True):
        for target_first in (False, True):
            rows = (dirs + [alias]) if parent_first else ([alias] + list(reversed(dirs)))
            rows = [target, *rows] if target_first else [*rows, target]
            path = fixture(f'{arch}-forward-positive-{parent_first}-{target_first}', rows)
            check(f'{arch}-forward-positive-{parent_first}-{target_first}', lambda path=path: native(path, arch), True)
            check(f'{arch}-forward-access-{parent_first}-{target_first}', lambda path=path: access(path, '/literal/deep/alias', elf(arch)), True)
    # Literal dot-like names remain ordinary directories, not traversal aliases.
    for directory in ('.hidden', '..literal'):
        path = fixture(f'{arch}-literal-{directory}', [target, (directory + '/alias', 'hardlink', 'target', 0o755, 0, 0),
                       (directory, 'directory', '', 0o755, 0, 0)])
        check(f'{arch}-literal-name-{directory}', lambda path=path: native(path, arch), True)
    # Rejecting stored children under symlinks must not reject legitimate path
    # lookups through symlinks whose physical entries live under real directories.
    path = fixture(f'{arch}-reference-symlinks-positive', [target, ('literal', 'directory', '', 0o755, 0, 0),
                   ('literal/hard', 'hardlink', 'target', 0o755, 0, 0), ('literal/sym', 'link', '../target', 0o755, 0, 0),
                   ('query', 'link', 'literal', 0o755, 0, 0)])
    check(f'{arch}-stored-symlinks-positive', lambda path=path: native(path, arch), True)
    check(f'{arch}-symlink-query-to-hardlink', lambda path=path: access(path, '/query/hard', elf(arch)), True)
    check(f'{arch}-symlink-query-to-symlink', lambda path=path: access(path, '/query/sym', elf(arch)), True)

assert len({row['id'] for row in results}) == len(results)
failed = [row['id'] for row in results if not row['passed']]
report = {'status': 'PASS' if not failed else 'FAIL', 'groups': len(results), 'failed': failed,
          'root_metadata': ROOT, 'exact_prior_counterexamples': 8, 'results': results}
(OUT / 'probes.json').write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps({'status': report['status'], 'groups': len(results), 'failed': failed, 'exact_prior_counterexamples': 8}, indent=2))
assert not failed
