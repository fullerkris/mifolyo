"""Inert constructor controls. No engine/helper/actor execution."""
import json
import sys
from common import OUT, PACKAGE, ROOT, elf, fixture

sys.path.insert(0, str(PACKAGE))
from image_artifacts import ImageTree, verify_content

results = []


def check(label, path, arch, expected_accept, reason=None):
    try:
        tree = ImageTree(path)
    except Exception as error:
        observed = {'constructor_accepted': False, 'exception': type(error).__name__, 'reason': str(error)}
    else:
        # No access to an alias is needed for the role. This proves whether the
        # constructor validates an unused declared hardlink by itself.
        try:
            content = verify_content(tree, arch, 'target', {'target': elf(arch)}, {})
            observed = {'constructor_accepted': True, 'role_accepted': True, 'payload_sha256': content['payload_sha256']}
        except Exception as error:
            observed = {'constructor_accepted': True, 'role_accepted': False, 'exception': type(error).__name__, 'reason': str(error)}
        observed['hardlink_entries'] = {name: {'metadata': {k: row[k] for k in ('mode', 'uid', 'gid')}, 'target': row['target'],
                                               'resolved_own_name': tree.resolve(name)} for name, row in tree.entries.items() if row.get('hardlink')}
    passed = observed['constructor_accepted'] == expected_accept and (not expected_accept or observed.get('role_accepted') is True)
    passed &= reason is None or observed.get('reason') == reason
    results.append({'id': label, 'fixture': str(path), 'expected_accept': expected_accept, 'expected_reason': reason, 'passed': passed, 'observed': observed})


for arch in ('aarch64', 'x86_64'):
    target = ('target', 'file', elf(arch), 0o755, 0, 0)
    for first in (False, True):
        for label, mode, uid, gid in (('valid', 0o755, 0, 0), ('mode', 0o700, 0, 0), ('uid', 0o755, 1, 0), ('gid', 0o755, 0, 1)):
            alias = ('unused', 'hardlink', 'target', mode, uid, gid)
            rows = [alias, target] if first else [target, alias]
            path = fixture(f'{arch}-direct-{first}-{label}', rows)
            check(f'{arch}-direct-{first}-{label}', path, arch, label == 'valid', None if label == 'valid' else 'IMAGE_HARDLINK_METADATA')
    for name, link_target, extra, reason in (
        ('missing', 'absent', [], 'IMAGE_DANGLING_LINK'),
        ('directory', 'dir', [('dir', 'directory', '', 0o755, 0, 0)], 'IMAGE_HARDLINK_TARGET'),
        ('symlink', 'sym', [('sym', 'link', 'target', 0o755, 0, 0)], 'IMAGE_HARDLINK_LINK_TARGET'),
        ('hardlink-chain', 'other', [('other', 'hardlink', 'target', 0o755, 0, 0)], 'IMAGE_HARDLINK_LINK_TARGET'),
    ):
        p = fixture(f'{arch}-unused-{name}', [target, *extra, ('unused', 'hardlink', link_target, 0o755, 0, 0)])
        check(f'{arch}-unused-{name}', p, arch, False, reason)
    # Real directory ancestry keeps the hardlink's own row addressable.
    for mode in (0o755, 0o700):
        p = fixture(f'{arch}-directory-parent-{oct(mode)}', [('dir', 'directory', '', 0o755, 0, 0),
                    ('dir/target', 'hardlink', 'target', mode, 0, 0), target])
        check(f'{arch}-directory-parent-{oct(mode)}', p, arch, mode == 0o755, None if mode == 0o755 else 'IMAGE_HARDLINK_METADATA')
    # If the alias's own parent is a symlink, resolve(name) can visit another
    # row rather than validate the hardlink that the constructor is iterating.
    for label, mode, uid, gid, link_target in (
        ('mode', 0o700, 0, 0, 'target'), ('uid', 0o755, 1, 0, 'target'),
        ('gid', 0o755, 0, 1, 'target'), ('missing-target', 0o755, 0, 0, 'absent'),
    ):
        p = fixture(f'{arch}-shadowed-{label}', [target, ('dir', 'link', '.', 0o755, 0, 0), ('dir/target', 'hardlink', link_target, mode, uid, gid)])
        check(f'{arch}-shadowed-{label}', p, arch, False)

assert len({r['id'] for r in results}) == len(results)
unexpected = [r['id'] for r in results if not r['passed']]
for arch in ('aarch64', 'x86_64'):
    positive = next(r for r in results if r['id'] == f'{arch}-direct-False-valid')['observed']['payload_sha256']
    for r in results:
        if r['id'].startswith(arch + '-shadowed-') and r['observed'].get('role_accepted'):
            r['observed']['same_payload_as_positive'] = r['observed']['payload_sha256'] == positive
            assert r['observed']['same_payload_as_positive']
report = {'status': 'COMPLETED_WITH_FINDING' if unexpected else 'PASS', 'groups': len(results), 'unexpected': unexpected, 'results': results}
(OUT / 'probes.json').write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps({'status': report['status'], 'groups': len(results), 'unexpected': unexpected,
                  'example': next((r for r in results if not r['passed']), None)}, indent=2))
