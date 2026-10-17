"""Independent inert-data re-review: no engine, helper, worker, or native execution."""
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

ROOT = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
BASE = ROOT / 'security'
RUN_NAME = os.environ.get('C0_REREVIEW_RUN', '')
if RUN_NAME and not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_-]{0,63}', RUN_NAME):
    raise ValueError('Run name must be a single simple component')
OUT = BASE / RUN_NAME if RUN_NAME else BASE
FIXTURES = OUT / 'fixtures'
OLD = Path('/tmp/opencode/c0-runtime-images-2026-10-17/security/unit-run-02/inert-fixtures')
sys.dont_write_bytecode = True
sys.path.insert(0, str(ROOT / 'snapshot/tests/crawl-jobs-v2-observer'))
import contracts as c
from image_artifacts import ImageTree, archive_record, verify_content
from image_build import Builder
from oracle_protocol import SOURCE_FILES

ROOT_OBSERVATION = {'uid': 0, 'gid': 0, 'mode': 0o755}
OBS = {}

def elf():
    raw = bytearray(4096)
    struct.pack_into('<16sHHIQQQIHHHHHH', raw, 0, b'\x7fELF\x02\x01\x01' + bytes(9),
                     3, 62, 1, 0x100, 64, 0, 0, 64, 56, 1, 0, 0, 0)
    struct.pack_into('<IIQQQQQQ', raw, 64, 1, 5, 0, 0, 0, len(raw), len(raw), 4096)
    return bytes(raw)  # Inert parser data, never executed or loaded.

def write_tar(name, rows):
    path = FIXTURES / name
    with tarfile.open(path, 'x', format=tarfile.USTAR_FORMAT) as archive:
        for row in rows:
            member_name, kind, value = row[:3]
            item = tarfile.TarInfo(member_name)
            item.mode = row[3] if len(row) > 3 else 0o755
            if kind == 'file':
                item.size = len(value)
                archive.addfile(item, io.BytesIO(value))
            else:
                item.type = {'dir': tarfile.DIRTYPE, 'link': tarfile.SYMTYPE, 'hardlink': tarfile.LNKTYPE}[kind]
                item.linkname = value
                archive.addfile(item)
    return path

def oracle_fixture(worker_kind='hardlink', worker_target='src/worker.py'):
    sources = {name: ('inert expected source: ' + name + '\n').encode() for name in (*SOURCE_FILES, 'build_oracle.py')}
    rows = [(name, 'dir', '') for name in ('oracle', 'src', 'src/deep', 'usr', 'usr/local', 'usr/local/bin', 'usr/local/lib')]
    rows += [('oracle/' + name, 'file', value) for name, value in sources.items() if name != 'worker.py']
    rows += [('src/worker.py', 'file', sources['worker.py']),
             ('src/deep/link', 'link', '../worker.py'),
             ('oracle/worker.py', worker_kind, worker_target),
             ('worker.py', 'file', b'inert alternate source\n'),
             ('usr/local/bin/python3', 'file', elf()),
             ('usr/local/lib/fixture.so', 'file', elf())]
    artifacts = {'oracle_elf': c.sha(elf()), 'extensions': {'/usr/local/lib/fixture.so': c.sha(elf())}}
    return rows, artifacts, sources

def snapshot_error(call):
    try:
        call()
    except c.Invalid as error:
        return str(error)
    raise AssertionError('Expected a specific refusal, not a success')

class OriginalClosures(unittest.TestCase):
    def test_SEC01_original_collision_refuses_and_original_positive_accepts(self):
        expected = 'sha256:c042848616c4d95ef39ed85d1fd9a9aabb0398dfa920ef335bce90db0a4ef8df'
        accepted = archive_record(OLD / 'portable-good.tar', expected, 'x86_64')
        code = snapshot_error(lambda: archive_record(OLD / 'portable-config-alias.tar', expected, 'x86_64'))
        self.assertEqual(code, 'IMAGE_SAVE_DUPLICATE_PATH')
        self.assertEqual(accepted['config_id'], expected)
        OBS['SEC01'] = {'negative': str(OLD / 'portable-config-alias.tar'), 'refusal': code,
                        'positive': str(OLD / 'portable-good.tar'), 'positive_config_id': accepted['config_id']}

    def test_SEC02_original_chain_refuses_source_join_and_matching_actual_bytes_accept(self):
        path = OLD / 'oracle-link-dotdot.tar'
        sources = {name: ('inert expected source: ' + name + '\n').encode() for name in (*SOURCE_FILES, 'build_oracle.py')}
        artifacts = {'oracle_elf': c.sha(elf()), 'extensions': {'/usr/local/lib/fixture.so': c.sha(elf())}}
        tree = ImageTree(path, ROOT_OBSERVATION)
        self.assertEqual(tree.resolve('/oracle/worker.py'), '/foreign/worker.py')
        code = snapshot_error(lambda: verify_content(tree, 'x86_64', 'oracle', artifacts, sources))
        self.assertEqual(code, 'IMAGE_ORACLE_SOURCE')
        matching = sources | {'worker.py': b'inert unexpected source marker\n'}
        good = verify_content(tree, 'x86_64', 'oracle', artifacts, matching)
        self.assertIs(good['execution_authorized'], False)
        OBS['SEC02'] = {'fixture': str(path), 'root_observation': ROOT_OBSERVATION,
                        'resolved': tree.resolve('/oracle/worker.py'), 'refusal': code,
                        'matching_source_admitted_sha256': good['declared_files']['/oracle/worker.py']}

    def test_SEC03_original_roots_refuse_and_boundary_member_counts_are_exact(self):
        code = snapshot_error(lambda: ImageTree(OLD / 'repeated-root-headers.tar', ROOT_OBSERVATION))
        self.assertEqual(code, 'IMAGE_ROOT_DUPLICATE')
        accepted = write_tar('member-limit-positive.tar', [('.', 'dir', '')] +
                             [('d%d' % number, 'dir', '') for number in range(19999)])
        tree = ImageTree(accepted)
        self.assertEqual(tree.member_count, 20000)
        self.assertEqual(tree.root, ROOT_OBSERVATION)
        excess = write_tar('member-limit-negative.tar', [('.', 'dir', '')] +
                           [('d%d' % number, 'dir', '') for number in range(20000)])
        excess_code = snapshot_error(lambda: ImageTree(excess))
        self.assertEqual(excess_code, 'IMAGE_DUPLICATE_OR_COUNT')
        OBS['SEC03'] = {'original_refusal': code, 'positive_members': tree.member_count,
                        'excess_refusal': excess_code}

class AdditionalControls(unittest.TestCase):
    def test_matching_and_conflicting_explicit_root_observation(self):
        path = write_tar('root-observation.tar', [('.', 'dir', ''), ('target', 'file', elf())])
        tree = ImageTree(path, ROOT_OBSERVATION)
        verify_content(tree, 'x86_64', 'target', {'target': elf()}, {})
        self.assertEqual(snapshot_error(lambda: ImageTree(path, ROOT_OBSERVATION | {'mode': 0o700})), 'IMAGE_ROOT_CHANGED')
        no_root = write_tar('no-root-marker.tar', [('target', 'file', elf())])
        self.assertEqual(snapshot_error(lambda: verify_content(ImageTree(no_root), 'x86_64', 'target', {'target': elf()}, {})), 'IMAGE_DIRECTORY_ACCESS')
        verify_content(ImageTree(no_root, ROOT_OBSERVATION), 'x86_64', 'target', {'target': elf()}, {})

    def test_regular_file_hardlink_and_symbolic_source_alias_both_match(self):
        for kind in ('hardlink', 'link'):
            target = 'src/worker.py' if kind == 'hardlink' else '../src/deep/../worker.py'
            rows, artifacts, sources = oracle_fixture(kind, target)
            path = write_tar('ordinary-' + kind + '.tar', rows)
            result = verify_content(ImageTree(path, ROOT_OBSERVATION), 'x86_64', 'oracle', artifacts, sources)
            self.assertEqual(result['declared_files']['/oracle/worker.py'], c.sha(sources['worker.py']))

    def test_host_archive_symlink_and_hardlink_identity_refuse(self):
        original = write_tar('identity.tar', [('.', 'dir', ''), ('marker', 'file', b'inert')])
        before = c.sha(original.read_bytes())
        link = FIXTURES / 'identity-symlink'
        link.symlink_to(original.name)
        with self.assertRaises(OSError): ImageTree(link)
        hard = FIXTURES / 'identity-hardlink'
        os.link(original, hard)
        self.assertEqual(snapshot_error(lambda: ImageTree(hard)), 'IMAGE_ARCHIVE')
        self.assertEqual(c.sha(original.read_bytes()), before)

    def test_root_observation_failure_cleans_owned_stopped_mock_and_propagates(self):
        builder = object.__new__(Builder)
        builder.cleanup = []
        calls = []
        state = {'Id': 'b' * 64, 'Image': 'sha256:' + 'a' * 64,
                 'State': {'Running': False, 'Pid': 0, 'Status': 'created', 'StartedAt': '0001-01-01T00:00:00Z'}}
        def run(args, **kwargs):
            calls.append(args[0])
            if args[0] == 'create':
                state['Name'] = args[args.index('--name') + 1]
                return 0, ('b' * 64).encode()
            if args[0] == 'rm': state['removed'] = True
            if args[0] == 'ps': return 0, b'' if state.get('removed') else ('b' * 64).encode()
            return 0, b''
        builder.run = run
        builder.inspect = lambda *_: copy.deepcopy(state)
        builder.root_metadata = lambda *_: (_ for _ in ()).throw(subprocess.TimeoutExpired('MOCK-NOT-EXECUTED', 90))
        with self.assertRaises(subprocess.TimeoutExpired): builder.export(state['Image'], FIXTURES / 'not-created.tar')
        self.assertEqual(calls, ['create', 'rm', 'ps'])
        self.assertTrue(builder.cleanup[0]['removed'])
        OBS['mock_root_failure'] = {'calls': calls, 'removed': True, 'exception_propagated': True}

    def test_helper_result_shape_and_docker_selection_refuse_without_execution(self):
        builder = object.__new__(Builder)
        builder.prefix = ['/inert/not-executed/podman', '--remote=false']
        for payload in (b'{"uid":false,"gid":0,"mode":493}', b'{"uid":0,"gid":0,"mode":493,"extra":1}'):
            builder.run = lambda *args, **kwargs: (0, payload)
            with self.assertRaises(c.Invalid): builder.root_metadata('b' * 64)
        builder.run = lambda *args, **kwargs: (0, b'{"uid":0,"gid":0,"mode":493}')
        self.assertEqual(builder.root_metadata('b' * 64), ROOT_OBSERVATION)
        with patch('image_build.subprocess.run') as run:
            self.assertEqual(snapshot_error(lambda: Builder('docker', FIXTURES)), 'BUILD_ENGINE')
            run.assert_not_called()

class RemainingBoundaryProbes(unittest.TestCase):
    def test_required_hardlink_to_relative_symlink_cannot_bind_different_bytes(self):
        rows, artifacts, sources = oracle_fixture('hardlink', 'src/deep/link')
        path = write_tar('hardlink-to-symlink.tar', rows)
        tree = ImageTree(path, ROOT_OBSERVATION)
        result = verify_content(tree, 'x86_64', 'oracle', artifacts, sources)
        # Explicit benign filesystem counterpart, not tar extraction.
        physical = FIXTURES / 'hardlink-path-semantics'
        (physical / 'src/deep').mkdir(parents=True, mode=0o700)
        (physical / 'oracle').mkdir(mode=0o700)
        (physical / 'src/worker.py').write_bytes(sources['worker.py'])
        (physical / 'worker.py').write_bytes(b'inert alternate source\n')
        (physical / 'src/deep/link').symlink_to('../worker.py')
        os.link(physical / 'src/deep/link', physical / 'oracle/worker.py', follow_symlinks=False)
        self.assertEqual((physical / 'src/deep/link').lstat().st_ino, (physical / 'oracle/worker.py').lstat().st_ino)
        actual = (physical / 'oracle/worker.py').read_bytes()
        self.assertNotEqual(actual, sources['worker.py'])
        OBS['C0-IMG-SEC-04'] = {'fixture': str(path.relative_to(OUT)), 'sha256': c.sha(path.read_bytes()),
            'root_observation': ROOT_OBSERVATION, 'admitted_path': tree.resolve('/oracle/worker.py'),
            'admitted_sha256': result['declared_files']['/oracle/worker.py'],
            'actual_relative_path': str((physical / 'oracle/worker.py').resolve().relative_to(physical)),
            'actual_sha256': c.sha(actual), 'same_symlink_inode_confirmed': True,
            'execution_authorized': result['execution_authorized'], 'runtime_acceptance': result['runtime_acceptance']}
        with self.assertRaises(c.Invalid):
            verify_content(ImageTree(path, ROOT_OBSERVATION), 'x86_64', 'oracle', artifacts, sources)

    def test_required_normalized_root_alias_must_join_observation(self):
        good = write_tar('root-alias-positive.tar', [('./.', 'dir', ''), ('target', 'file', elf())])
        verify_content(ImageTree(good, ROOT_OBSERVATION), 'x86_64', 'target', {'target': elf()}, {})
        bad = write_tar('root-alias-conflict.tar', [('./.', 'dir', '', 0o700), ('target', 'file', elf())])
        tree = ImageTree(bad, ROOT_OBSERVATION)
        result = verify_content(tree, 'x86_64', 'target', {'target': elf()}, {})
        duplicate = write_tar('root-alias-duplicate.tar', [('.', 'dir', ''), ('./.', 'dir', '', 0o700), ('target', 'file', elf())])
        duplicates = ImageTree(duplicate)
        verify_content(duplicates, 'x86_64', 'target', {'target': elf()}, {})
        OBS['C0-IMG-SEC-05'] = {'fixture': str(bad.relative_to(OUT)), 'sha256': c.sha(bad.read_bytes()),
            'reported_root': result['root_directory'], 'unjoined_root_alias': {k: v for k, v in tree.entries['/'].items() if k != 'raw'},
            'duplicate_fixture': str(duplicate.relative_to(OUT)), 'duplicate_sha256': c.sha(duplicate.read_bytes()),
            'duplicate_archive_accepted': True, 'positive_fixture': str(good.relative_to(OUT))}
        with self.assertRaisesRegex(c.Invalid, 'IMAGE_ROOT_CHANGED'):
            ImageTree(bad, ROOT_OBSERVATION)

if __name__ == '__main__':
    os.umask(0o077)
    if RUN_NAME:
        OUT.mkdir(mode=0o700)
    FIXTURES.mkdir(mode=0o700)
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    report = {'python': sys.version, 'tests_run': result.testsRun,
              'failures': [{'id': t.id(), 'traceback': text} for t, text in result.failures],
              'errors': [{'id': t.id(), 'traceback': text} for t, text in result.errors],
              'skipped': list(result.skipped), 'observations': OBS,
              'engine_commands_executed': 0, 'root_metadata_helper_executed': False,
              'networking_performed': False, 'native_or_worker_execution': False}
    with (OUT / 'independent-tests.json').open('x') as file:
        json.dump(report, file, sort_keys=True, indent=2)
        file.write('\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
