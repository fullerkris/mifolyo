"""Reproduce original findings and adjacent controls using inert fixtures only."""
import contextlib
import copy
import importlib.machinery
import importlib.util
import io
import json
import os
import stat
import subprocess
import sys
import tempfile
import types
from unittest.mock import patch
from review_common import OUT, PACKAGE, OLD, ROOT, fixture, load, make_elf, sha

(OUT / 'tmp').mkdir(exist_ok=True)
os.environ['TMPDIR'] = tempfile.tempdir = str(OUT / 'tmp')
sys.path.insert(0, str(PACKAGE))
import contracts as c
import build_oracle
import image_build
from image_artifacts import ImageTree, archive_record, elf_inventory, image_metadata, verify_content
from oracle_protocol import SOURCE_FILES

results = []
ID = 'sha256:' + 'a' * 64
OLD_FIXTURES = OLD / 'inert-fixtures'


def check(label, call, expected_accept, *, finding=None, reason=None, category='closure', details=None):
    try:
        value = call()
        r = {'accepted': True}
        if isinstance(value, dict):
            r['value'] = {k: v for k, v in value.items() if k not in ('elfs',)}
    except Exception as e:
        r = {'accepted': False, 'exception': type(e).__name__, 'reason': str(e)}
    r.update(id=label, finding=finding, category=category, expected_accept=expected_accept)
    r['matches_expectation'] = r['accepted'] == expected_accept and (reason is None or r.get('reason') == reason)
    if details is not None:
        r['details'] = details
    results.append(r)
    return r


def tree(path):
    # All old rootfs fixtures legitimately omit the root header. Explicit root
    # observation prevents unrelated missing-root failures from hiding findings.
    return ImageTree(path, ROOT)


def engine_scope():
    with tempfile.TemporaryDirectory(dir=OUT / 'tmp') as temp:
        target = __import__('pathlib').Path(temp) / 'never-created'
        with patch.object(image_build.subprocess, 'run', side_effect=AssertionError('engine access forbidden')) as run, \
                patch.object(image_build.shutil, 'which', return_value='/fake/podman'):
            check('001-docker-prepare-refused', lambda: image_build.prepare('x86_64', 'docker', target), False,
                  finding='C0ARCH04-COR-001', reason='BUILD_OUTPUT_SCOPE')
            check('001-docker-builder-refused', lambda: image_build.Builder('docker', target), False,
                  finding='C0ARCH04-COR-001', reason='BUILD_ENGINE')
            builder = image_build.Builder('podman', target)
            assert builder.prefix == ['/fake/podman', '--remote=false']
            assert not target.exists()
            run.assert_not_called()
        calls = []
        def rootful(args, **kwargs):
            calls.append(args)
            return 0, b'false\n'
        fake = types.SimpleNamespace(run=rootful, commands=[], cleanup=[])
        with patch.object(image_build, 'Builder', return_value=fake):
            check('001-rootful-preflight-refused', lambda: image_build.prepare('x86_64', 'podman', target), False,
                  finding='C0ARCH04-COR-001', reason='BUILD_ROOTLESS_REQUIRED')
        assert calls == [['info', '--format', '{{.Host.Security.Rootless}}']]
        assert load(target / 'image-set.json')['status'] == 'FAIL' and not (target / 'source').exists()
        for arch in ('aarch64', 'x86_64'):
            for role in ('target', 'observer', 'oracle'):
                builder.output = __import__('pathlib').Path(temp)
                calls = []
                def run(args, **kwargs):
                    calls.append(args)
                    __import__('pathlib').Path(args[args.index('--iidfile') + 1]).write_text(ID)
                    return 0, b''
                builder.run = run
                value = builder.build(PACKAGE, arch, 'Dockerfile.oracle' if role == 'oracle' else 'Dockerfile', role, arch + role)
                assert value == ID and len(calls) == 1
                a = calls[0]
                assert '--no-cache' in a and '--identity-label=false' in a
                assert ('--unsetenv' in a) == (role != 'oracle')
                assert a[a.index('--network') + 1] == 'none'
                results.append({'id': f'001-podman-flags-{arch}-{role}', 'finding': 'C0ARCH04-COR-001', 'category': 'positive', 'matches_expectation': True, 'fake_command': a})
    row = {'Id': ID, 'Os': 'linux', 'Architecture': 'amd64', 'Config': {'User': '999:999', 'WorkingDir': '/', 'Entrypoint': ['/target'], 'Env': []}}
    check('001-empty-env-positive', lambda: image_metadata(row, 'x86_64', 'target', []), True, finding='C0ARCH04-COR-001')
    changed = copy.deepcopy(row)
    changed['Config']['Env'] = ['PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin']
    check('001-buildkit-path-still-forbidden', lambda: image_metadata(changed, 'x86_64', 'target', []), False, finding='C0ARCH04-COR-001', reason='IMAGE_ENVIRONMENT')


def host_extensions():
    from test_x64_runtime import X64BuildTests
    method = 'test_oracle_manifest_binds_x64_interpreter_without_running_worker'
    module = types.ModuleType('_review_inert_arm_extension')
    path = OLD_FIXTURES / 'inert-aarch64-extension.so'
    loader = importlib.machinery.ExtensionFileLoader(module.__name__, str(path))
    module.__spec__ = importlib.util.spec_from_file_location(module.__name__, str(path), loader=loader)
    # Spec only: never dlopen or execute the inert ELF.
    with patch.dict(sys.modules, {module.__name__: module}):
        check('002-original-x64-test-with-arm-module', lambda: getattr(X64BuildTests(method), method)(), True, finding='C0ARCH04-COR-002')
    for arch, foreign in (('aarch64', 'x86_64'), ('x86_64', 'aarch64')):
        for selected in (arch, foreign):
            with tempfile.TemporaryDirectory(dir=OUT / 'tmp') as temp:
                root = __import__('pathlib').Path(temp)
                executable, extension, destination = root / 'python', root / 'extension.so', root / 'output'
                executable.write_bytes(make_elf(arch)); extension.write_bytes(make_elf(selected))
                mod = types.SimpleNamespace(__spec__=types.SimpleNamespace(loader=importlib.machinery.ExtensionFileLoader('inert', str(extension)), origin=str(extension)))
                collect = build_oracle.extension_inventory
                import oracle_worker
                with patch.object(sys, 'argv', ['build_oracle.py', str(destination), arch]), \
                        patch.object(sys, 'executable', str(executable)), \
                        patch.object(build_oracle.os, 'uname', return_value=types.SimpleNamespace(sysname='Linux', machine=arch)), \
                        patch.object(build_oracle, 'extension_inventory', side_effect=lambda a: collect(a, [mod])), \
                        patch.object(oracle_worker, 'main', side_effect=AssertionError('worker main forbidden')), \
                        patch.object(oracle_worker, 'Engine', side_effect=AssertionError('worker engine forbidden')):
                    r = check(f'002-manifest-{arch}-extension-{selected}', build_oracle.main, selected == arch, finding='C0ARCH04-COR-002', reason=None if selected == arch else 'ORACLE_BUILD_ARCHITECTURE')
                assert destination.exists() == (selected == arch)
                if selected == arch:
                    record = load(destination / 'oracle-artifacts.json')
                    assert record['extensions'] == {str(extension): sha(make_elf(arch))}
                    assert not record['worker_executed'] and not record['execution_authorized']


def portable_names():
    old_results = load(OLD / 'adversarial-results.json')['results']
    identity = next(r for r in old_results if r['id'] == 'portable-normalized-config-duplicate')['observed']['value']['config_id']
    check('003-original-positive-archive', lambda: archive_record(OLD_FIXTURES / 'portable-control.tar', identity, 'x86_64'), True, finding='C0ARCH04-COR-003')
    for kind in ('config', 'layer'):
        check('003-original-normalized-' + kind + '-duplicate', lambda kind=kind: archive_record(OLD_FIXTURES / ('portable-alias-' + kind + '.tar'), identity, 'x86_64'), False,
              finding='C0ARCH04-COR-003', reason='IMAGE_SAVE_DUPLICATE_PATH')


def oracle_fixture(*, loader_mode=None):
    raw = make_elf()
    python = make_elf(interpreter='/loader') if loader_mode is not None else raw
    sources = {p: (p + ' inert source').encode() for p in (*SOURCE_FILES, 'build_oracle.py')}
    rows = [(p, 'directory', '') for p in ('oracle', 'usr', 'usr/local', 'usr/local/bin', 'usr/local/lib')]
    rows += [('oracle/' + p, 'file', data) for p, data in sources.items()]
    rows += [('usr/local/bin/python3', 'file', python), ('usr/local/lib/fixture.so', 'file', raw)]
    if loader_mode is not None:
        rows += [('loader', 'file', raw, loader_mode)]
    artifacts = {'oracle_elf': sha(python), 'extensions': {'/usr/local/lib/fixture.so': sha(raw)}}
    return rows, artifacts, sources


def permission_checks():
    for role in ('target', 'observer'):
        for mode, accept in ((0o755, True), (0o700, False), (0o750, False), (0o600, False)):
            p = OLD_FIXTURES / (role + '-mode-' + oct(mode) + '.tar')
            raw = tree(p).entries['/' + role]['raw']
            check(f'004-original-{role}-{oct(mode)}', lambda p=p, raw=raw, role=role: verify_content(tree(p), 'x86_64', role, {role: raw}, {}), accept, finding='C0ARCH04-COR-004')
        p = fixture(role + '-read-but-not-executable', [(role, 'file', make_elf(), 0o744)])
        check('004-' + role + '-other-execute-required', lambda: verify_content(tree(p), 'x86_64', role, {role: make_elf()}, {}), False, finding='C0ARCH04-COR-004', reason='IMAGE_FILE_EXECUTABLE')
    rows, artifacts, sources = oracle_fixture()
    p = fixture('oracle-access-positive', rows)
    check('004-oracle-positive-with-observed-root', lambda: verify_content(tree(p), 'x86_64', 'oracle', artifacts, sources), True, finding='C0ARCH04-COR-004')
    for leaf in ('oracle/worker.py', 'oracle', 'usr/local/lib/fixture.so'):
        p = OLD_FIXTURES / ('oracle-unreadable-' + leaf.replace('/', '-') + '.tar')
        check('004-original-' + leaf, lambda p=p: verify_content(tree(p), 'x86_64', 'oracle', artifacts, sources), False, finding='C0ARCH04-COR-004', reason='IMAGE_DIRECTORY_ACCESS' if leaf == 'oracle' else 'IMAGE_FILE_IDENTITY')
    p = fixture('native-root-observation', [('target', 'file', make_elf())])
    for mode in (0o555, 0o755, 0o700):
        observed = ROOT | {'mode': mode}
        check('004-observed-root-' + oct(mode), lambda observed=observed: verify_content(ImageTree(p, observed), 'x86_64', 'target', {'target': make_elf()}, {}), mode != 0o700, finding='C0ARCH04-COR-004', reason='IMAGE_DIRECTORY_ACCESS' if mode == 0o700 else None)
    for mode in (0o755, 0o644):
        rows, artifacts, sources = oracle_fixture(loader_mode=mode)
        path = fixture('oracle-pt-interp-mode-' + oct(mode), rows)
        check('004-pt-interp-mode-' + oct(mode), lambda path=path: verify_content(tree(path), 'x86_64', 'oracle', artifacts, sources), mode == 0o755, finding='C0ARCH04-COR-004', category='adjacent-permission-control', details={'loader_mode': oct(mode), 'root_metadata': ROOT})


def dependency_checks():
    check('005-original-symlink-search-positive', lambda: elf_inventory(tree(OLD_FIXTURES / 'dependency-symlink-parent.tar'), 'x86_64', ['/app']), True, finding='C0ARCH04-COR-005')
    check('005-original-rpath-ignored-negative', lambda: elf_inventory(tree(OLD_FIXTURES / 'dependency-rpath-ignored.tar'), 'x86_64', ['/app']), False, finding='C0ARCH04-COR-005', reason='IMAGE_LIBRARY_MISSING')
    lib = 'libc0fixtureonly.so'
    dirs = [('old', 'directory', ''), ('new', 'directory', '')]
    both = make_elf(needed=(lib,), paths=((15, '/old'), (29, '/new')))
    p = fixture('runpath-overrides-positive', [*dirs, ('app', 'file', both), ('old/' + lib, 'file', make_elf()), ('new/' + lib, 'file', make_elf())])
    r = check('005-runpath-overrides-positive', lambda: elf_inventory(tree(p), 'x86_64', ['/app']), True, finding='C0ARCH04-COR-005')
    assert r['value']['/app']['dependencies'][lib] == '/new/' + lib
    p = fixture('active-rpath-unsupported', [*dirs, ('app', 'file', make_elf(needed=(lib,), paths=((15, '/old'),))), ('old/' + lib, 'file', make_elf())])
    check('005-active-rpath-explicit-refusal', lambda: elf_inventory(tree(p), 'x86_64', ['/app']), False, finding='C0ARCH04-COR-005', reason='IMAGE_UNSUPPORTED_RPATH')
    for target, reason in (('missing', 'IMAGE_DANGLING_LINK'), ('alias', 'IMAGE_LINK_DEPTH')):
        p = fixture('search-link-' + target, [('app', 'file', make_elf(needed=(lib,), paths=((29, '/alias'),))), ('alias', 'link', target)])
        check('005-search-link-' + target, lambda: elf_inventory(tree(p), 'x86_64', ['/app']), False, finding='C0ARCH04-COR-005', reason=reason)
    rows = [('one', 'directory', ''), ('two', 'directory', ''), ('app', 'file', make_elf(needed=(lib,), paths=((29, '/one:/two'),))), ('one/' + lib, 'file', make_elf()), ('two/' + lib, 'file', make_elf())]
    p = fixture('ambiguous-libraries', rows)
    check('005-distinct-library-candidates-refused', lambda: elf_inventory(tree(p), 'x86_64', ['/app']), False, finding='C0ARCH04-COR-005', reason='IMAGE_LIBRARY_AMBIGUOUS')


def root_aliases():
    original = [('.', 'directory', '', 0o755), ('target', 'file', make_elf())]
    p = fixture('root-single-control', original)
    check('root-single-header-positive', lambda: verify_content(ImageTree(p), 'x86_64', 'target', {'target': make_elf()}, {}), True, category='root-header-control')
    p = fixture('root-exact-duplicate', original + [('.', 'directory', '', 0o700)])
    check('root-exact-duplicate-negative', lambda: ImageTree(p), False, reason='IMAGE_ROOT_DUPLICATE', category='root-header-control')
    for i, alias in enumerate(('./.', './/.', '././')):
        p = fixture('root-alias-duplicate-' + str(i), original + [(alias, 'directory', '', 0o700)])
        check('root-normalized-duplicate-' + alias, lambda p=p: verify_content(ImageTree(p), 'x86_64', 'target', {'target': make_elf()}, {}), False,
              category='adjacent-root-header-control', details={'canonical_root_mode': '0o755', 'alias': alias, 'alias_mode': '0o700'})
    p = fixture('root-alias-disagrees-with-observation', [('./.', 'directory', '', 0o700), ('target', 'file', make_elf())])
    check('root-alias-conflicts-observed-root', lambda: verify_content(ImageTree(p, ROOT), 'x86_64', 'target', {'target': make_elf()}, {}), False,
          category='adjacent-root-header-control', details={'observed_root': ROOT, 'alias_header_mode': '0o700'})
    rows = [('oracle', 'directory', ''), ('foreign', 'directory', ''), ('foreign/subdir', 'directory', ''), ('jump', 'link', 'foreign/subdir'),
            ('oracle/worker.py', 'link', '../jump/../worker.py'), ('worker.py', 'file', b'wrong'), ('foreign/worker.py', 'file', b'correct')]
    p = fixture('symlink-parent-order', rows)
    t = tree(p)
    assert t.file('/oracle/worker.py') == b'correct'
    t.entries['/foreign/subdir']['mode'] = 0o700
    check('symlink-parent-traversed-access', lambda: t.file('/oracle/worker.py'), False, reason='IMAGE_DIRECTORY_ACCESS', category='path-control')


def root_helper():
    for fault in ('none', 'mount', 'stat', 'unmount'):
        calls = []
        def mount(args, **kw):
            calls.append(args)
            if fault == 'mount': raise OSError('inert mount failure')
            return '/inert-root\n'
        def getstat(path, **kw):
            assert path == '/inert-root' and kw == {'follow_symlinks': False}
            if fault == 'stat': raise OSError('inert stat failure')
            return types.SimpleNamespace(st_mode=stat.S_IFDIR | 0o555, st_uid=0, st_gid=0)
        def unmount(args, **kw):
            calls.append(args)
            assert kw['check'] is True
            if fault == 'unmount': raise OSError('inert unmount failure')
        capture = io.StringIO()
        with patch.object(sys, 'argv', ['-c', '/fake/podman', 'b' * 64]), \
                patch.object(subprocess, 'check_output', side_effect=mount), patch.object(subprocess, 'run', side_effect=unmount), \
                patch.object(os, 'stat', side_effect=getstat), contextlib.redirect_stdout(capture):
            r = check('root-helper-finally-' + fault, lambda: exec(compile(image_build.ROOT_METADATA_SCRIPT, '<inert-root-helper>', 'exec'), {}), fault == 'none', category='fake-helper-control')
        assert calls == [['/fake/podman', '--remote=false', 'mount', 'b' * 64], ['/fake/podman', '--remote=false', 'unmount', 'b' * 64]]
        r['fake_commands'] = calls
        if fault == 'none': assert json.loads(capture.getvalue()) == {'uid': 0, 'gid': 0, 'mode': 0o555}


engine_scope()
host_extensions()
portable_names()
permission_checks()
dependency_checks()
root_aliases()
root_helper()
report = {'status': 'COMPLETED', 'root_metadata_for_headerless_fixtures': ROOT, 'results': results,
          'unexpected_results': [r['id'] for r in results if not r['matches_expectation']]}
(OUT / 'closure-probes.json').write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps({'groups': len(results), 'unexpected_results': report['unexpected_results'], 'per_finding': {
    f'C0ARCH04-COR-{i:03}': [{'id': r['id'], 'accepted': r.get('accepted'), 'reason': r.get('reason'), 'matches': r['matches_expectation']} for r in results if r.get('finding') == f'C0ARCH04-COR-{i:03}'] for i in range(1, 6)}}, indent=2))
