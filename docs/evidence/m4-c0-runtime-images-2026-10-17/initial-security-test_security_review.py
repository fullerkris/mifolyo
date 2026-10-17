"""Inert archive/unit fixtures only. No engine CLI, networking or actor execution.

Tests named test_required_* are regression specifications: they intentionally fail
on the frozen implementation where the security invariant is not enforced.
Every generated file is below the review's private security directory.
"""
import copy
import io
import json
import os
from pathlib import Path
import re
import struct
import subprocess
import sys
import tarfile
import unittest
from unittest.mock import patch

ROOT = Path('/tmp/opencode/c0-runtime-images-2026-10-17')
BASE = ROOT / 'security'
RUN_NAME = os.environ.get('C0_SECURITY_RUN', '')
if RUN_NAME and not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}', RUN_NAME):
    raise ValueError('Run name must be a single simple path component')
OUT = BASE / RUN_NAME if RUN_NAME else BASE
FIXTURES = OUT / 'inert-fixtures'
sys.dont_write_bytecode = True
sys.path.insert(0, str(ROOT / 'snapshot/tests/crawl-jobs-v2-observer'))
import contracts as c
from image_artifacts import ImageTree, archive_record, image_metadata, verify_content
from image_build import Builder
from build_oracle import elf_bytes
from oracle_protocol import SOURCE_FILES

OBSERVATIONS = {}
ID = 'sha256:' + 'a' * 64
CID = 'b' * 64


def inert_elf():
    # Parser fixture, not an executable program; never run or dynamically loaded.
    raw = bytearray(4096)
    struct.pack_into('<16sHHIQQQIHHHHHH', raw, 0,
                     b'\x7fELF\x02\x01\x01' + bytes(9),
                     3, 62, 1, 0x100, 64, 0, 0, 64, 56, 1, 0, 0, 0)
    struct.pack_into('<IIQQQQQQ', raw, 64, 1, 5, 0, 0, 0,
                     len(raw), len(raw), 4096)
    return bytes(raw)


def tar_bytes(rows):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode='w', format=tarfile.USTAR_FORMAT) as archive:
        for name, kind, value in rows:
            item = tarfile.TarInfo(name)
            item.mode = 0o755 if kind != 'file' else 0o644
            if kind == 'file':
                item.size = len(value)
                archive.addfile(item, io.BytesIO(value))
            elif kind == 'executable':
                item.mode = 0o755
                item.size = len(value)
                archive.addfile(item, io.BytesIO(value))
            else:
                item.type = {'dir': tarfile.DIRTYPE, 'link': tarfile.SYMTYPE,
                             'hardlink': tarfile.LNKTYPE, 'fifo': tarfile.FIFOTYPE}[kind]
                item.linkname = value
                archive.addfile(item)
    return output.getvalue()


def write_tar(name, rows):
    path = FIXTURES / name
    with path.open('xb') as file:
        file.write(tar_bytes(rows))
    return path


def portable_rows():
    layer = tar_bytes([('marker.txt', 'file', b'inert layer marker\n')])
    config = c.canonical({'architecture': 'amd64', 'os': 'linux',
                          'rootfs': {'type': 'layers', 'diff_ids': ['sha256:' + c.sha(layer)]},
                          'config': {'User': '999:999', 'Env': [], 'Entrypoint': ['/target']}})
    manifest = c.canonical([{'Config': 'config.json', 'Layers': ['layer.tar'], 'RepoTags': []}])
    return [('manifest.json', 'file', manifest), ('config.json', 'file', config), ('layer.tar', 'file', layer)], config


def lifecycle(mode):
    builder = object.__new__(Builder)
    builder.engine, builder.output, builder.commands, builder.cleanup = 'podman', FIXTURES, [], []
    state = {'Id': CID, 'Image': ID, 'Name': None,
             'State': {'Running': False, 'Pid': 0, 'Status': 'created',
                       'StartedAt': '0001-01-01T00:00:00Z'}}
    calls = []

    def run(args, **kwargs):
        calls.append(list(args))
        if args[0] == 'create':
            state['Name'] = args[args.index('--name') + 1]
            if mode == 'create_timeout':
                raise subprocess.TimeoutExpired('INERT-MOCK', 60)
            if mode == 'malformed_create':
                return 0, b'ordinary truncated response\n'
            return 0, (CID + '\n').encode()
        if args[0] == 'export' and mode == 'export_timeout':
            raise subprocess.TimeoutExpired('INERT-MOCK', 120)
        if args[0] == 'rm':
            state['removed'] = True
        if args[0] == 'ps':
            return 0, b'' if state.get('removed') and mode != 'cleanup_remaining' else (CID + '\n').encode()
        return 0, b''

    builder.run = run
    builder.inspect = lambda *_: copy.deepcopy(state)
    if mode == 'foreign':
        state['Image'] = 'sha256:' + 'c' * 64
    if mode == 'started':
        state['State']['StartedAt'] = '2026-10-17T00:00:00Z'
    return builder, state, calls


class ScopeControls(unittest.TestCase):
    def test_correct_portable_config_layers_and_checksums(self):
        rows, config = portable_rows()
        path = write_tar('portable-good.tar', rows)
        result = archive_record(path, 'sha256:' + c.sha(config), 'x86_64')
        self.assertEqual(result['config_id'], 'sha256:' + c.sha(config))
        self.assertEqual(result['sha256'], c.sha(path.read_bytes()))

    def test_wrong_config_layer_platform_and_duplicate_json_refuse(self):
        rows, config = portable_rows()
        variants = [
            ('wrong-config', rows, ID, 'x86_64', 'IMAGE_SAVE_CONFIG'),
            ('wrong-platform', rows, 'sha256:' + c.sha(config), 'aarch64', 'IMAGE_SAVE_PLATFORM'),
            ('wrong-layer', rows[:-1] + [('layer.tar', 'file', b'changed')],
             'sha256:' + c.sha(config), 'x86_64', 'IMAGE_SAVE_LAYER_DIGEST'),
            ('duplicate-json', [('manifest.json', 'file', b'[{"Config":"config.json","Config":"other.json","Layers":["layer.tar"]}]')] + rows[1:],
             'sha256:' + c.sha(config), 'x86_64', 'IMAGE_SAVE_DUPLICATE_JSON'),
        ]
        for name, changed, identity, arch, code in variants:
            with self.subTest(name=name):
                path = write_tar(name + '.tar', changed)
                with self.assertRaisesRegex(c.Invalid, code):
                    archive_record(path, identity, arch)

    def test_host_symlinks_and_multilink_files_refuse_without_target_changes(self):
        rows, config = portable_rows()
        path = write_tar('file-identity.tar', rows)
        before = c.sha(path.read_bytes())
        linked = FIXTURES / 'archive-symlink'
        linked.symlink_to(path.name)
        for read in (lambda p: ImageTree(p), lambda p: archive_record(p, 'sha256:' + c.sha(config), 'x86_64'),
                     lambda p: elf_bytes(p, 'x86_64')):
            with self.assertRaises(OSError):
                read(linked)
        hard = FIXTURES / 'archive-hardlink'
        os.link(path, hard)
        for read in (lambda p: ImageTree(p), lambda p: archive_record(p, 'sha256:' + c.sha(config), 'x86_64')):
            with self.assertRaisesRegex(c.Invalid, 'IMAGE_ARCHIVE'):
                read(hard)
        self.assertEqual(c.sha(path.read_bytes()), before)

    def test_rootfs_traversal_duplicate_special_and_oversize_refuse(self):
        variants = [
            [('..' + '/outside', 'file', b'inert')],
            [('/absolute', 'file', b'inert')],
            [('same', 'file', b'one'), ('./same', 'file', b'two')],
            [('pipe', 'fifo', '')],
            [('.wh.hidden', 'file', b'inert')],
            [('escape', 'link', '../../outside')],
        ]
        for index, rows in enumerate(variants):
            with self.subTest(index=index):
                with self.assertRaises(c.Invalid):
                    ImageTree(write_tar('rootfs-refuse-%d.tar' % index, rows))
        header = tarfile.TarInfo('oversize')
        header.size = 64 * 1024 * 1024 + 1
        path = FIXTURES / 'oversize-header.tar'
        path.write_bytes(header.tobuf(format=tarfile.USTAR_FORMAT) + bytes(1024))
        with self.assertRaisesRegex(c.Invalid, 'IMAGE_FILE_BOUND'):
            ImageTree(path)

    def test_native_metadata_empty_env_and_hooks_fail_closed(self):
        value = {'Id': ID, 'Os': 'linux', 'Architecture': 'amd64', 'Config': {
            'User': '999:999', 'WorkingDir': '/', 'Entrypoint': ['/target'], 'Cmd': None, 'Env': []}}
        accepted = image_metadata(value, 'x86_64', 'target', [])
        self.assertIs(accepted['execution_authorized'], False)
        for key, changed in [('Env', ['PATH=/usr/bin']), ('Labels', {'inherited': 'true'}),
                             ('OnBuild', ['not-executed']), ('Volumes', {'/unwanted': {}}),
                             ('Healthcheck', {'Test': ['not-executed']}), ('User', '0:0')]:
            with self.subTest(key=key):
                bad = copy.deepcopy(value)
                bad['Config'][key] = changed
                with self.assertRaises(c.Invalid):
                    image_metadata(bad, 'x86_64', 'target', [])

    def test_interrupted_and_malformed_create_cleanup_remains_owned_and_stopped(self):
        for mode in ('create_timeout', 'malformed_create', 'export_timeout'):
            with self.subTest(mode=mode):
                builder, state, calls = lifecycle(mode)
                with self.assertRaises((subprocess.TimeoutExpired, c.Invalid)):
                    builder.export(ID, FIXTURES / ('never-written-' + mode))
                self.assertTrue(state['removed'])
                self.assertEqual(len(builder.cleanup), 1)
                self.assertTrue(builder.cleanup[0]['removed'])
                self.assertFalse(any(row[0] in ('start', 'run', 'exec', 'kill') for row in calls))
                OBSERVATIONS[mode] = {'command_kinds': [row[0] for row in calls], 'cleanup': builder.cleanup}

    def test_foreign_started_or_incomplete_cleanup_cannot_report_success(self):
        for mode in ('foreign', 'started', 'cleanup_remaining'):
            with self.subTest(mode=mode):
                builder, state, calls = lifecycle(mode)
                with self.assertRaises(c.Invalid):
                    builder.export(ID, FIXTURES / ('never-written-' + mode))
                self.assertEqual(builder.cleanup, [])
                if mode != 'cleanup_remaining':
                    self.assertFalse(any(row[0] in ('export', 'rm', 'kill') for row in calls))
                OBSERVATIONS[mode] = {'command_kinds': [row[0] for row in calls], 'cleanup': builder.cleanup}

    def test_run_timeout_and_oversize_output_do_not_report_success(self):
        builder = object.__new__(Builder)
        builder.prefix, builder.output, builder.commands = ['INERT-NOT-EXECUTED'], FIXTURES, []
        with patch('image_build.subprocess.run', side_effect=subprocess.TimeoutExpired('INERT-MOCK', 1)):
            with self.assertRaises(subprocess.TimeoutExpired):
                builder.run(['inspect'], timeout=1)
        self.assertNotIn('returncode', builder.commands[-1])
        result = subprocess.CompletedProcess(['INERT-NOT-EXECUTED'], 0, stdout=b'x' * (16 * 1024 * 1024 + 1), stderr=b'')
        with patch('image_build.subprocess.run', return_value=result):
            with self.assertRaisesRegex(c.Invalid, 'BUILD_LOG_BOUND'):
                builder.run(['inspect'])
        self.assertNotIn('returncode', builder.commands[-1])


class RequiredRegressionGuards(unittest.TestCase):
    def test_required_portable_normalized_aliases_are_rejected(self):
        rows, config = portable_rows()
        changed = json.loads(config)
        changed['config']['Entrypoint'] = ['/inert-substitution-marker']
        alternate = c.canonical(changed)
        path = write_tar('portable-config-alias.tar', rows + [('./config.json', 'file', alternate)])
        accepted = archive_record(path, 'sha256:' + c.sha(config), 'x86_64')
        # Model only pathname canonicalization in memory. No archive extraction.
        view = {}
        with tarfile.open(path, 'r:') as archive:
            for member in archive:
                if member.isfile():
                    view[str(Path(member.name))] = archive.extractfile(member).read()
        OBSERVATIONS['C0-IMG-SEC-01'] = {
            'fixture': str(path.relative_to(OUT)), 'fixture_sha256': c.sha(path.read_bytes()),
            'admitted_config_id': accepted['config_id'],
            'normalized_path_config_id': 'sha256:' + c.sha(view['config.json']),
            'normalized_path_entrypoint': json.loads(view['config.json'])['config']['Entrypoint'],
            'engine_load_performed': False, 'extraction_performed': False,
        }
        self.assertNotEqual(accepted['config_id'], 'sha256:' + c.sha(view['config.json']))
        # Required fix: reject colliding archive names before declaring identity.
        with self.assertRaises(c.Invalid):
            archive_record(path, 'sha256:' + c.sha(config), 'x86_64')

    def test_required_link_dotdot_does_not_substitute_verified_source(self):
        sources = {name: ('inert expected source: ' + name + '\n').encode() for name in (*SOURCE_FILES, 'build_oracle.py')}
        rows = [(name, 'dir', '') for name in ('oracle', 'foreign', 'foreign/subdir', 'usr', 'usr/local', 'usr/local/bin', 'usr/local/lib')]
        rows += [('oracle/' + name, 'file', raw) for name, raw in sources.items() if name != 'worker.py']
        rows += [('oracle/worker.py', 'link', '../jump/../worker.py'),
                 ('jump', 'link', 'foreign/subdir'),
                 ('worker.py', 'file', sources['worker.py']),
                 ('foreign/worker.py', 'file', b'inert unexpected source marker\n'),
                 ('usr/local/bin/python3', 'executable', inert_elf()),
                 ('usr/local/lib/fixture.so', 'file', inert_elf())]
        path = write_tar('oracle-link-dotdot.tar', rows)
        artifacts = {'oracle_elf': c.sha(inert_elf()), 'extensions': {'/usr/local/lib/fixture.so': c.sha(inert_elf())}}
        tree = ImageTree(path)
        admitted = verify_content(tree, 'x86_64', 'oracle', artifacts, sources)
        # Construct just benign text files and links explicitly, never extract tar.
        physical = FIXTURES / 'benign-posix-paths'
        (physical / 'oracle').mkdir(parents=True, mode=0o700)
        (physical / 'foreign/subdir').mkdir(parents=True, mode=0o700)
        (physical / 'worker.py').write_bytes(sources['worker.py'])
        (physical / 'foreign/worker.py').write_bytes(b'inert unexpected source marker\n')
        (physical / 'jump').symlink_to('foreign/subdir', target_is_directory=True)
        (physical / 'oracle/worker.py').symlink_to('../jump/../worker.py')
        actual = (physical / 'oracle/worker.py').read_bytes()
        OBSERVATIONS['C0-IMG-SEC-02'] = {
            'fixture': str(path.relative_to(OUT)), 'fixture_sha256': c.sha(path.read_bytes()),
            'admitted_resolved_path': tree.resolve('/oracle/worker.py'),
            'admitted_source_sha256': admitted['declared_files']['/oracle/worker.py'],
            'posix_resolved_relative_path': str((physical / 'oracle/worker.py').resolve().relative_to(physical)),
            'posix_read_source_sha256': c.sha(actual),
            'runtime_acceptance': admitted['runtime_acceptance'],
            'execution_authorized': admitted['execution_authorized'],
            'code_execution_performed': False, 'extraction_performed': False,
        }
        self.assertNotEqual(actual, sources['worker.py'])
        self.assertNotEqual(c.sha(actual), admitted['declared_files']['/oracle/worker.py'])
        with self.assertRaises(c.Invalid):
            verify_content(ImageTree(path), 'x86_64', 'oracle', artifacts, sources)

    def test_required_root_headers_count_toward_archive_bound(self):
        roots = 20001
        path = write_tar('repeated-root-headers.tar', [('.', 'dir', '')] * roots + [('marker', 'file', b'inert')])
        tree = ImageTree(path)
        OBSERVATIONS['C0-IMG-SEC-03'] = {
            'fixture': str(path.relative_to(OUT)), 'fixture_sha256': c.sha(path.read_bytes()),
            'physical_member_count': roots + 1, 'reported_archive_members': len(tree.entries),
            'fixture_bytes': path.stat().st_size, 'exhaustion_attempted': False,
        }
        self.assertEqual(len(tree.entries), 1)
        with self.assertRaises(c.Invalid):
            ImageTree(path)


if __name__ == '__main__':
    os.umask(0o077)
    if RUN_NAME:
        OUT.mkdir(mode=0o700)  # Each reproduction uses a new private directory.
    FIXTURES.mkdir(mode=0o700)
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    inventory = []
    def collect(items):
        for item in items:
            if isinstance(item, unittest.TestSuite):
                collect(item)
            else:
                inventory.append(item.id())
    collect(suite)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    report = {'scope': 'changed Python archive/config/file/cleanup logic only', 'python': sys.version,
              'tests_run': result.testsRun, 'inventory': inventory,
              'failures': [{'id': test.id(), 'traceback': trace} for test, trace in result.failures],
              'errors': [{'id': test.id(), 'traceback': trace} for test, trace in result.errors],
              'skipped': list(result.skipped), 'observations': OBSERVATIONS,
              'engine_commands_executed': 0, 'networking_performed': False,
              'native_programs_executed': False, 'worker_main_or_collector_executed': False}
    with (OUT / 'security-unit-results.json').open('x') as file:
        json.dump(report, file, indent=2, sort_keys=True)
        file.write('\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
