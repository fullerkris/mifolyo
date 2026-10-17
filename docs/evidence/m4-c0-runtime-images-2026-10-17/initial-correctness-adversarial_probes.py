"""Inert archive and fake-engine probes only. No engine or C0 actor is run."""
import copy
import hashlib
import importlib.machinery
import importlib.util
import io
import json
import os
from pathlib import Path
import posixpath
import struct
import subprocess
import sys
import tarfile
import tempfile
import types
from unittest.mock import patch

OUT = Path(__file__).resolve().parent
os.environ['TMPDIR'] = str(OUT / 'tmp')
tempfile.tempdir = str(OUT / 'tmp')
sys.path.insert(0, str(OUT.parent / 'snapshot/tests/crawl-jobs-v2-observer'))
import contracts as c
from image_artifacts import ImageTree, archive_record, elf_inventory, image_metadata, verify_content
from image_build import Builder
from oracle_protocol import SOURCE_FILES

FIXTURES = OUT / 'inert-fixtures'
FIXTURES.mkdir(exist_ok=True)
ID = 'sha256:' + 'a' * 64
CID = 'b' * 64
RESULTS = []


def outcome(call):
    try:
        value = call()
        return {'accepted': True, 'value': value}
    except Exception as e:
        return {'accepted': False, 'exception': type(e).__name__, 'reason': str(e)}


def make_elf(architecture='x86_64', needed=(), paths=()):
    """Non-executed 4 KiB fake ELF. Paths are (DT_* tag, string) pairs."""
    b = bytearray(4096)
    dynamic = bool(needed or paths)
    struct.pack_into('<16sHHIQQQIHHHHHH', b, 0, b'\x7fELF\x02\x01\x01' + bytes(9),
                     3, {'x86_64': 62, 'aarch64': 183}[architecture], 1, 0x100, 64, 0, 0, 64, 56,
                     1 + dynamic, 0, 0, 0)
    struct.pack_into('<IIQQQQQQ', b, 64, 1, 5, 0, 0, 0, len(b), len(b), 4096)
    if dynamic:
        strings, tags = bytearray(b'\0'), []
        for tag, text in [(1, n) for n in needed] + list(paths):
            tags.append((tag, len(strings)))
            strings.extend(text.encode() + b'\0')
        tags = [(5, 0x500), (10, len(strings)), *tags, (0, 0)]
        b[0x500:0x500 + len(strings)] = strings
        struct.pack_into('<IIQQQQQQ', b, 120, 2, 4, 0x300, 0x300, 0, 16 * len(tags), 16 * len(tags), 8)
        for index, (tag, value) in enumerate(tags):
            struct.pack_into('<QQ', b, 0x300 + index * 16, tag, value)
    return bytes(b)


def tar_bytes(rows):
    out = io.BytesIO()
    with tarfile.open(fileobj=out, mode='w', format=tarfile.USTAR_FORMAT) as t:
        for row in rows:
            path, kind, value = row[:3]
            m = tarfile.TarInfo(path)
            m.mode = row[3] if len(row) == 4 else 0o755
            if kind == 'file':
                m.size = len(value)
                t.addfile(m, io.BytesIO(value))
            else:
                m.type = {'directory': tarfile.DIRTYPE, 'link': tarfile.SYMTYPE, 'fifo': tarfile.FIFOTYPE}[kind]
                m.linkname = value
                t.addfile(m)
    return out.getvalue()


def archive(label, rows):
    p = FIXTURES / (label + '.tar')
    p.write_bytes(tar_bytes(rows))
    return p


def probe_buildkit_environment():
    default = 'PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin'
    base = {'Id': ID, 'Os': 'linux', 'Architecture': 'amd64', 'Config': {
        'User': '999:999', 'WorkingDir': '/', 'Entrypoint': ['/target'], 'Env': [default]}}
    result = outcome(lambda: image_metadata(base, 'x86_64', 'target', []))
    assert not result['accepted'] and result['reason'] == 'IMAGE_ENVIRONMENT'
    builder = object.__new__(Builder)
    builder.engine, builder.output = 'docker', FIXTURES
    calls = []
    def run(args, **kw):
        calls.append(args)
        Path(args[args.index('--iidfile') + 1]).write_text(ID)
        return 0, b''
    builder.run = run
    assert builder.build(FIXTURES, 'x86_64', 'Dockerfile', 'target', 'docker-fake') == ID
    assert '--unsetenv' not in calls[0]
    RESULTS.append({'id': 'buildkit-scratch-path', 'finding': 'C0ARCH04-COR-001',
                    'observed': result, 'fake_engine_command': calls[0],
                    'limitation': 'Modeled documented BuildKit output, not a Docker build.'})


def probe_arm_ci_fixture():
    from test_x64_runtime import X64BuildTests
    method = 'test_oracle_manifest_binds_x64_interpreter_without_running_worker'
    case = X64BuildTests(method)
    control = outcome(lambda: getattr(case, method)())
    assert control['accepted'], control
    arm = FIXTURES / 'inert-aarch64-extension.so'
    arm.write_bytes(make_elf('aarch64'))
    module = types.ModuleType('_c0_inert_native_arm_extension')
    loader = importlib.machinery.ExtensionFileLoader(module.__name__, str(arm))
    module.__spec__ = importlib.util.spec_from_file_location(module.__name__, str(arm), loader=loader)
    # Metadata only. The .so is not loaded or executed.
    with patch.dict(sys.modules, {module.__name__: module}):
        result = outcome(lambda: getattr(case, method)())
    assert not result['accepted'] and result['reason'] == 'ORACLE_BUILD_ARCHITECTURE', result
    RESULTS.append({'id': 'arm-ci-x64-manifest-fixture', 'finding': 'C0ARCH04-COR-002',
                    'control_on_x64': control, 'observed': result,
                    'limitation': 'One inert native-ARM extension metadata entry simulates the real ARM host extension inventory; no ARM actor or image executes.'})


def probe_portable_aliases():
    original_layer = tar_bytes([('payload', 'file', b'original inert bytes')])
    changed_layer = tar_bytes([('payload', 'file', b'different inert bytes')])
    original_config = c.canonical({'architecture': 'amd64', 'os': 'linux', 'config': {'User': '999:999'},
                                  'rootfs': {'type': 'layers', 'diff_ids': ['sha256:' + c.sha(original_layer)]}})
    changed_config = original_config.replace(b'999:999', b'000:000')
    manifest = c.canonical([{'Config': 'config.json', 'Layers': ['layer.tar']}])
    rows = [('manifest.json', 'file', manifest), ('config.json', 'file', original_config), ('layer.tar', 'file', original_layer)]
    identity = 'sha256:' + c.sha(original_config)
    assert outcome(lambda: archive_record(archive('portable-control', rows), identity, 'x86_64'))['accepted']
    for label, leaf, data in [('config', 'config.json', changed_config), ('layer', 'layer.tar', changed_layer)]:
        path = archive('portable-alias-' + label, rows + [('./' + leaf, 'file', data)])
        result = outcome(lambda: archive_record(path, identity, 'x86_64'))
        assert result['accepted'], result
        normalized = {}
        with tarfile.open(path, 'r:') as t:
            for m in t:
                normalized[posixpath.normpath(m.name)] = c.sha(t.extractfile(m).read())
        wanted = c.sha(original_config if label == 'config' else original_layer)
        assert normalized[leaf] != wanted
        RESULTS.append({'id': 'portable-normalized-' + label + '-duplicate', 'finding': 'C0ARCH04-COR-003',
                        'observed': result, 'normalized_last_member_sha256': normalized[leaf], 'claimed_sha256': wanted,
                        'fixture': str(path), 'limitation': 'No extraction or image load. Conflicting normalized member identities established by pure reads.'})
    duplicate = outcome(lambda: archive_record(archive('portable-exact-duplicate', rows + [('config.json', 'file', changed_config)]), identity, 'x86_64'))
    assert not duplicate['accepted'] and duplicate['reason'] == 'IMAGE_SAVE_MEMBER'
    RESULTS.append({'id': 'portable-exact-duplicate-control', 'observed': duplicate, 'expected_refusal': True})


def probe_permissions():
    raw = make_elf()
    for role in ('target', 'observer'):
        results = {}
        for mode in (0o755, 0o700, 0o750, 0o600):
            path = archive(role + '-mode-' + oct(mode), [(role, 'file', raw, mode)])
            result = outcome(lambda: verify_content(ImageTree(path), 'x86_64', role, {role: raw}, {}))
            results[oct(mode)] = {'accepted': result['accepted'], 'payload': result.get('value', {}).get('payload_sha256'), 'reason': result.get('reason')}
        assert results['0o755']['accepted'] and results['0o700']['accepted'] and results['0o750']['accepted']
        assert not results['0o600']['accepted']
        assert results['0o755']['payload'] == results['0o700']['payload']
        RESULTS.append({'id': role + '-not-executable-by-999', 'finding': 'C0ARCH04-COR-004', 'observed': results})
    sources = {p: (p + ' inert source').encode() for p in (*SOURCE_FILES, 'build_oracle.py')}
    rows = [(p, 'directory', '') for p in ('oracle', 'usr', 'usr/local', 'usr/local/bin', 'usr/local/lib')]
    rows += [('oracle/' + p, 'file', v) for p, v in sources.items()]
    rows += [('usr/local/bin/python3', 'file', raw), ('usr/local/lib/fixture.so', 'file', raw)]
    artifacts = {'oracle_elf': c.sha(raw), 'extensions': {'/usr/local/lib/fixture.so': c.sha(raw)}}
    for leaf, mode in [('oracle/worker.py', 0o600), ('oracle', 0o700), ('usr/local/lib/fixture.so', 0o600)]:
        changed = [(*r[:3], mode) if r[0] == leaf else r for r in rows]
        path = archive('oracle-unreadable-' + leaf.replace('/', '-'), changed)
        result = outcome(lambda: verify_content(ImageTree(path), 'x86_64', 'oracle', artifacts, sources))
        assert result['accepted'], result
        RESULTS.append({'id': 'inaccessible-' + leaf, 'finding': 'C0ARCH04-COR-004',
                        'accepted': result['accepted'], 'mode': oct(mode), 'owner': '0:0', 'runtime_user': '999:999'})


def probe_dependency_resolution():
    needed = 'libc0fixtureonly.so'
    raw = make_elf(needed=(needed,), paths=((29, '/alias'),))
    rows = [('app', 'file', raw), ('real', 'directory', ''), ('alias', 'link', '/real'), ('real/' + needed, 'file', make_elf())]
    path = archive('dependency-symlink-parent', rows)
    tree = ImageTree(path)
    assert tree.resolve('/alias/' + needed) == '/real/' + needed
    result = outcome(lambda: elf_inventory(tree, 'x86_64', ['/app']))
    assert not result['accepted'] and result['reason'] == 'IMAGE_LIBRARY_MISSING'
    RESULTS.append({'id': 'runpath-symlink-directory-false-refusal', 'finding': 'C0ARCH04-COR-005',
                    'observed': result, 'existing_resolved_dependency': tree.resolve('/alias/' + needed)})
    # GNU ld.so ignores DT_RPATH when DT_RUNPATH is present.
    raw = make_elf(needed=(needed,), paths=((15, '/old-rpath'), (29, '/new-runpath')))
    rows = [('app', 'file', raw), ('old-rpath', 'directory', ''), ('new-runpath', 'directory', ''),
            ('old-rpath/' + needed, 'file', make_elf())]
    result = outcome(lambda: elf_inventory(ImageTree(archive('dependency-rpath-ignored', rows)), 'x86_64', ['/app']))
    assert result['accepted'] and result['value']['/app']['dependencies'][needed] == '/old-rpath/' + needed
    RESULTS.append({'id': 'rpath-runpath-false-positive-closure', 'finding': 'C0ARCH04-COR-005',
                    'observed': result, 'correct_search': ['/new-runpath', 'cache', 'default dirs'],
                    'fixture_has_dependency_only_in_ignored_rpath': True})


def probe_lifecycle():
    for failure in ('create-timeout', 'malformed-create-output', 'copy-failure', 'export-timeout',
                    'remove-failure', 'absence-failure', 'foreign-during-cleanup', 'started-after-export'):
        builder = object.__new__(Builder)
        builder.engine, builder.output, builder.cleanup, builder.commands = 'docker', FIXTURES, [], []
        state = {'Id': CID, 'Image': ID, 'State': {'Running': False, 'Pid': 0, 'Status': 'created', 'StartedAt': '0001-01-01T00:00:00Z'}}
        calls = []
        def run(args, **kw):
            calls.append(args)
            if args[0] == 'create':
                state['Name'] = args[args.index('--name') + 1]
                if failure == 'create-timeout':
                    raise subprocess.TimeoutExpired(args, 60)
                return 0, b'bad-id' if failure == 'malformed-create-output' else CID.encode()
            if args[0] == 'cp' and failure == 'copy-failure':
                raise OSError('synthetic cp failure')
            if args[0] == 'export':
                if failure == 'export-timeout':
                    raise subprocess.TimeoutExpired(args, 120)
                if failure == 'foreign-during-cleanup':
                    state['Image'] = 'sha256:' + 'c' * 64
                if failure == 'started-after-export':
                    state['State'].update(Running=True, Pid=123)
            if args[0] == 'rm':
                if failure == 'remove-failure':
                    raise OSError('synthetic rm failure')
                state['removed'] = True
            if args[0] == 'ps':
                return 0, b'' if state.get('removed') and failure != 'absence-failure' else CID.encode()
            return 0, b''
        builder.run = run
        builder.inspect = lambda *_: copy.deepcopy(state)
        kwargs = {'directory': '/out'} if failure == 'copy-failure' else {}
        destination = FIXTURES / ('lifecycle-' + failure)
        if failure == 'copy-failure' and destination.exists():
            destination.rmdir()  # Our own empty fake-copy fixture, not reviewed data.
        result = outcome(lambda: builder.export(ID, destination, **kwargs))
        assert not result['accepted'], (failure, result)
        clean_expected = failure in ('create-timeout', 'malformed-create-output', 'copy-failure', 'export-timeout')
        assert bool(builder.cleanup) == clean_expected
        assert not any(a[0] in ('run', 'start', 'exec', 'kill') for a in calls)
        if failure in ('foreign-during-cleanup', 'started-after-export'):
            assert not any(a[0] == 'rm' for a in calls)
        RESULTS.append({'id': 'lifecycle-' + failure, 'observed': result,
                        'cleanup': builder.cleanup, 'commands': calls, 'control': 'expected fail-closed'})
        if failure == 'copy-failure':
            destination.rmdir()


probe_buildkit_environment()
probe_arm_ci_fixture()
probe_portable_aliases()
probe_permissions()
probe_dependency_resolution()
probe_lifecycle()
(OUT / 'adversarial-results.json').write_text(json.dumps({'status': 'PROBES_COMPLETED', 'results': RESULTS}, indent=2) + '\n')
print(json.dumps([{'id': r['id'], 'finding': r.get('finding'), 'control': r.get('control')} for r in RESULTS], indent=2))
