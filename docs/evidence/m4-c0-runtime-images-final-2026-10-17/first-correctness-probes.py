"""Pure archive/ELF controls. No engine, mounted-root helper, or actor calls."""
import json
import sys
from common import OUT, PACKAGE, FIRST, PREVIOUS, ROOT, fixture, load, make_elf, sha

sys.path.insert(0, str(PACKAGE))
import contracts as c
from image_artifacts import ImageTree, archive_record, elf_inventory, elf_record, image_metadata, verify_content
from oracle_protocol import SOURCE_FILES

results = []


def check(label, call, accepted, reason=None, finding=None):
    try:
        value = call()
        observed = {'accepted': True}
        if isinstance(value, dict):
            observed['value'] = {k: v for k, v in value.items() if k != 'elfs'}
    except Exception as e:
        observed = {'accepted': False, 'exception': type(e).__name__, 'reason': str(e)}
    passed = observed['accepted'] == accepted and (reason is None or observed.get('reason') == reason)
    row = {'id': label, 'finding': finding, 'expected_accept': accepted, 'expected_reason': reason, 'passed': passed, 'observed': observed}
    results.append(row)
    return row


def native(path, arch='x86_64', root=ROOT):
    return verify_content(ImageTree(path, root), arch, 'target', {'target': make_elf(arch)}, {})


def loader_controls():
    # Read the exact old failing oracle fixtures, without modifying them.
    sources = {p: (p + ' inert source').encode() for p in (*SOURCE_FILES, 'build_oracle.py')}
    artifacts = {'oracle_elf': sha(make_elf(interpreter='/loader')), 'extensions': {'/usr/local/lib/fixture.so': sha(make_elf())}}
    for mode, accepted, reason in ((0o755, True, None), (0o644, False, 'IMAGE_FILE_EXECUTABLE')):
        old = PREVIOUS / 'correctness/inert-fixtures' / ('oracle-pt-interp-mode-' + oct(mode) + '.tar')
        check('004-exact-old-oracle-' + oct(mode), lambda old=old: verify_content(ImageTree(old, ROOT), 'x86_64', 'oracle', artifacts, sources), accepted, reason, 'C0ARCH04-COR-004')
    for arch in ('aarch64', 'x86_64'):
        loader = '/usr/local/lib/loader.so'
        dirs = [(p, 'directory', '') for p in ('usr', 'usr/local', 'usr/local/lib')]
        # Both orderings matter: pending is LIFO, and a read-only first visit
        # must not bypass the later PT_INTERP execute check.
        for order in ([loader, '/app'], ['/app', loader]):
            for mode in (0o555, 0o755, 0o644, 0o744, 0o111):
                p = fixture(arch + '-loader-' + str(order.index(loader)) + '-' + oct(mode), dirs + [
                    ('app', 'file', make_elf(arch, interpreter=loader)), (loader.lstrip('/'), 'file', make_elf(arch), mode)])
                reason = 'IMAGE_FILE_IDENTITY' if mode == 0o111 else 'IMAGE_FILE_EXECUTABLE' if mode in (0o644, 0o744) else None
                check(f'004-{arch}-visited-{order.index(loader)}-{oct(mode)}', lambda p=p, order=order: elf_inventory(ImageTree(p, ROOT), arch, order), mode in (0o555, 0o755), reason, 'C0ARCH04-COR-004')
        p = fixture(arch + '-ordinary-library-0644', dirs + [('app', 'file', make_elf(arch, needed=('loader.so',))), (loader.lstrip('/'), 'file', make_elf(arch), 0o644)])
        check('004-' + arch + '-ordinary-library-0644-positive', lambda: elf_inventory(ImageTree(p, ROOT), arch, ['/app']), True, finding='C0ARCH04-COR-004')
        for interpreter in (None, loader):
            check('reserved-edge-' + arch + '-' + str(bool(interpreter)), lambda interpreter=interpreter: elf_record(make_elf(arch, needed=('PT_INTERP',), interpreter=interpreter), arch), False, 'IMAGE_ELF_NEEDED', 'C0ARCH04-COR-004')
        p = fixture(arch + '-reserved-lookalike-positive', dirs + [('app', 'file', make_elf(arch, needed=('PT_INTERP.so',))), ('usr/local/lib/PT_INTERP.so', 'file', make_elf(arch), 0o644)])
        check('reserved-lookalike-' + arch + '-ordinary-positive', lambda: elf_inventory(ImageTree(p, ROOT), arch, ['/app']), True, finding='C0ARCH04-COR-004')
        for mode in (0o755, 0o700):
            p = fixture(arch + '-loader-alias-directory-' + oct(mode), [('private', 'directory', '', mode), ('private/alias', 'link', '/loader'),
                ('loader', 'file', make_elf(arch)), ('app', 'file', make_elf(arch, interpreter='/private/alias'))])
            check('004-' + arch + '-alias-access-' + oct(mode), lambda: elf_inventory(ImageTree(p, ROOT), arch, ['/app', '/loader']), mode == 0o755,
                  None if mode == 0o755 else 'IMAGE_DIRECTORY_ACCESS', 'C0ARCH04-COR-004')


def root_controls():
    for i, alias in enumerate(('.', './.', './/.', '././')):
        for observed in (None, ROOT):
            p = fixture('root-positive-' + str(i), [(alias, 'directory', '', 0o755), ('target', 'file', make_elf())])
            def positive():
                t = ImageTree(p, observed)
                assert '/' not in t.entries and t.member_count == 2 and t.root == ROOT
                return verify_content(t, 'x86_64', 'target', {'target': make_elf()}, {})
            check(f'006-positive-{i}-observed-{observed is not None}', positive, True, finding='C0ARCH04-COR-006')
        for ordering, (first, second) in enumerate(((alias, '.'), ('.', alias))):
            p = fixture(f'root-duplicate-{i}-{ordering}', [(first, 'directory', '', 0o755), (second, 'directory', '', 0o755), ('target', 'file', make_elf())])
            check(f'006-duplicate-{i}-{ordering}', lambda: native(p), False, 'IMAGE_ROOT_DUPLICATE', 'C0ARCH04-COR-006')
        p = fixture('root-observed-conflict-' + str(i), [(alias, 'directory', '', 0o700), ('target', 'file', make_elf())])
        check('006-conflicting-observation-' + str(i), lambda: native(p), False, 'IMAGE_ROOT_CHANGED', 'C0ARCH04-COR-006')
        check('006-restrictive-unobserved-root-' + str(i), lambda: native(p, root=None), False, 'IMAGE_DIRECTORY_ACCESS', 'C0ARCH04-COR-006')
        p = fixture('root-not-directory-' + str(i), [(alias, 'file', b'not a directory'), ('target', 'file', make_elf())])
        check('006-file-root-' + str(i), lambda: ImageTree(p, ROOT), False, 'IMAGE_ROOT_DUPLICATE', 'C0ARCH04-COR-006')
    for i in range(3):
        p = PREVIOUS / 'correctness/inert-fixtures' / f'root-alias-duplicate-{i}.tar'
        check('006-exact-previous-duplicate-' + str(i), lambda p=p: native(p, root=None), False, 'IMAGE_ROOT_DUPLICATE', 'C0ARCH04-COR-006')
    p = PREVIOUS / 'correctness/inert-fixtures/root-alias-disagrees-with-observation.tar'
    check('006-exact-previous-observation-conflict', lambda: native(p), False, 'IMAGE_ROOT_CHANGED', 'C0ARCH04-COR-006')
    p = fixture('root-absent', [('target', 'file', make_elf())])
    check('006-missing-observation-refuses', lambda: native(p, root=None), False, 'IMAGE_DIRECTORY_ACCESS', 'C0ARCH04-COR-006')
    check('006-headerless-valid-observation-positive', lambda: native(p), True, finding='C0ARCH04-COR-006')


def hardlink_controls():
    dirs = [('src', 'directory', ''), ('dst', 'directory', '')]
    for mode in (0o644, 0o755):
        p = fixture('hardlink-regular-' + oct(mode), dirs + [('src/file', 'file', b'inert', mode), ('dst/file', 'hardlink', 'src/file', mode)])
        check('hardlink-direct-' + oct(mode), lambda: {'bytes_match': ImageTree(p, ROOT).file('/dst/file') == b'inert'}, True)
        for field, row in [('mode', ('dst/file', 'hardlink', 'src/file', 0o700)), ('uid', ('dst/file', 'hardlink', 'src/file', mode, 999, 0)), ('gid', ('dst/file', 'hardlink', 'src/file', mode, 0, 999))]:
            p = fixture('hardlink-metadata-' + oct(mode) + '-' + field, dirs + [('src/file', 'file', b'inert', mode), row])
            check('hardlink-metadata-' + oct(mode) + '-' + field, lambda: ImageTree(p, ROOT).file('/dst/file'), False, 'IMAGE_HARDLINK_METADATA')
    base = dirs + [('src/file', 'file', b'inert'), ('src/link', 'link', 'file')]
    for label, target, extra, reason in (
        ('symlink-inode', 'src/link', [], 'IMAGE_HARDLINK_LINK_TARGET'),
        ('symlink-parent', 'alias/file', [('alias', 'link', 'src')], 'IMAGE_HARDLINK_LINK_TARGET'),
        ('hardlink-chain', 'src/hard', [('src/hard', 'hardlink', 'src/file')], 'IMAGE_HARDLINK_LINK_TARGET'),
        ('dangling', 'src/missing', [], 'IMAGE_DANGLING_LINK'),
        ('directory', 'src', [], 'IMAGE_HARDLINK_TARGET'),
    ):
        p = fixture('hardlink-' + label, base + extra + [('dst/file', 'hardlink', target)])
        check('hardlink-' + label, lambda: ImageTree(p, ROOT).file('/dst/file'), False, reason)
    p = fixture('symlink-chain-positive', base + [('dst/file', 'link', '../src/link')])
    check('symlink-chain-still-positive', lambda: {'bytes_match': ImageTree(p, ROOT).file('/dst/file') == b'inert'}, True)
    p = fixture('symlink-to-hardlink-positive', dirs + [('src/file', 'file', b'inert'), ('src/hard', 'hardlink', 'src/file'), ('dst/file', 'link', '../src/hard')])
    check('symlink-to-direct-hardlink-positive', lambda: {'bytes_match': ImageTree(p, ROOT).file('/dst/file') == b'inert'}, True)


def preservation_controls():
    # COR-001 and COR-002 implementation is unchanged; the current image tests
    # also cover refusal/ABI isolation. Keep the native Env refusal explicit.
    value = {'Id': 'sha256:' + 'a' * 64, 'Os': 'linux', 'Architecture': 'amd64', 'Config': {'User': '999:999', 'Entrypoint': ['/target'], 'Env': []}}
    check('001-empty-native-env-positive', lambda: image_metadata(value, 'x86_64', 'target', []), True, finding='C0ARCH04-COR-001')
    value['Config']['Env'] = ['PATH=/usr/bin']
    check('001-native-path-remains-forbidden', lambda: image_metadata(value, 'x86_64', 'target', []), False, 'IMAGE_ENVIRONMENT', 'C0ARCH04-COR-001')
    old = FIRST / 'correctness/inert-fixtures'
    previous = load(FIRST / 'correctness/adversarial-results.json')['results']
    identity = next(r for r in previous if r['id'] == 'portable-normalized-config-duplicate')['observed']['value']['config_id']
    check('003-original-positive', lambda: archive_record(old / 'portable-control.tar', identity, 'x86_64'), True, finding='C0ARCH04-COR-003')
    for kind in ('config', 'layer'):
        check('003-original-duplicate-' + kind, lambda kind=kind: archive_record(old / ('portable-alias-' + kind + '.tar'), identity, 'x86_64'), False, 'IMAGE_SAVE_DUPLICATE_PATH', 'C0ARCH04-COR-003')
    check('005-original-directory-link-positive', lambda: elf_inventory(ImageTree(old / 'dependency-symlink-parent.tar', ROOT), 'x86_64', ['/app']), True, finding='C0ARCH04-COR-005')
    check('005-original-runpath-overrides-rpath', lambda: elf_inventory(ImageTree(old / 'dependency-rpath-ignored.tar', ROOT), 'x86_64', ['/app']), False, 'IMAGE_LIBRARY_MISSING', 'C0ARCH04-COR-005')


loader_controls()
root_controls()
hardlink_controls()
preservation_controls()
failed = [r['id'] for r in results if not r['passed'] or r['observed'].get('value', {}).get('bytes_match') is False]
assert len({r['id'] for r in results}) == len(results)
report = {'status': 'PASS' if not failed else 'FAIL', 'groups': len(results), 'failed': failed, 'root_metadata_for_headerless_fixtures': ROOT, 'results': results}
(OUT / 'probes.json').write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps({'status': report['status'], 'groups': len(results), 'failed': failed}, indent=2))
assert not failed
