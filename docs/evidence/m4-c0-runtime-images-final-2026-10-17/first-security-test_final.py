"""Bounded, inert-data final checks; no engine/helper/worker/native execution."""
import io
import json
import os
from pathlib import Path
import re
import struct
import sys
import tarfile
import unittest

ROOT = Path('/tmp/opencode/c0-runtime-images-final-2026-10-17')
BASE = ROOT / 'security'
RUN_NAME = os.environ.get('C0_FINAL_REVIEW_RUN', '')
if RUN_NAME and not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_-]{0,63}', RUN_NAME):
    raise ValueError('Run name must be one simple component')
OUT = BASE / RUN_NAME if RUN_NAME else BASE
FIXTURES = OUT / 'fixtures'
INITIAL = Path('/tmp/opencode/c0-runtime-images-2026-10-17/security/unit-run-02/inert-fixtures')
PREVIOUS = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17/security/repeat-02/fixtures')
sys.dont_write_bytecode = True
sys.path.insert(0, str(ROOT / 'snapshot/tests/crawl-jobs-v2-observer'))
import contracts as c
from image_artifacts import ImageTree, archive_record, elf_inventory, elf_record, verify_content
from oracle_protocol import SOURCE_FILES

OBS = {}
GOOD_ROOT = {'uid': 0, 'gid': 0, 'mode': 0o755}

def elf(needed=(), interpreter=None):
    raw = bytearray(4096)
    count = 1 + bool(needed) + bool(interpreter)
    struct.pack_into('<16sHHIQQQIHHHHHH', raw, 0, b'\x7fELF\x02\x01\x01' + bytes(9),
                     3, 62, 1, 0x100, 64, 0, 0, 64, 56, count, 0, 0, 0)
    struct.pack_into('<IIQQQQQQ', raw, 64, 1, 5, 0, 0, 0, len(raw), len(raw), 4096)
    cursor = 120
    if interpreter:
        data = interpreter.encode() + b'\0'
        raw[0x200:0x200 + len(data)] = data
        struct.pack_into('<IIQQQQQQ', raw, cursor, 3, 4, 0x200, 0x200, 0, len(data), len(data), 1)
        cursor += 56
    if needed:
        strings, offsets = bytearray(b'\0'), []
        for name in needed:
            offsets.append(len(strings)); strings.extend(name.encode() + b'\0')
        raw[0x500:0x500 + len(strings)] = strings
        tags = [(5, 0x500), (10, len(strings)), *((1, offset) for offset in offsets), (0, 0)]
        struct.pack_into('<IIQQQQQQ', raw, cursor, 2, 4, 0x300, 0x300, 0, len(tags) * 16, len(tags) * 16, 8)
        for index, row in enumerate(tags): struct.pack_into('<QQ', raw, 0x300 + 16 * index, *row)
    return bytes(raw)

def tar(name, rows):
    path = FIXTURES / name
    with tarfile.open(path, 'x', format=tarfile.USTAR_FORMAT) as archive:
        for entry in rows:
            name, kind, value = entry[:3]
            meta = entry[3] if len(entry) > 3 else GOOD_ROOT
            member = tarfile.TarInfo(name)
            member.mode, member.uid, member.gid = meta['mode'], meta['uid'], meta['gid']
            if kind == 'file':
                member.size = len(value); archive.addfile(member, io.BytesIO(value))
            else:
                member.type = {'dir': tarfile.DIRTYPE, 'link': tarfile.SYMTYPE, 'hardlink': tarfile.LNKTYPE}[kind]
                member.linkname = value; archive.addfile(member)
    return path

def refused(call):
    try: call()
    except c.Invalid as error: return str(error)
    raise AssertionError('Unexpected success')

def oracle_inputs():
    sources = {name: ('inert expected source: ' + name + '\n').encode() for name in (*SOURCE_FILES, 'build_oracle.py')}
    artifacts = {'oracle_elf': c.sha(elf()), 'extensions': {'/usr/local/lib/fixture.so': c.sha(elf())}}
    return artifacts, sources

class OriginalClosureTests(unittest.TestCase):
    def test_SEC01_collision_and_positive_config(self):
        expected = 'sha256:c042848616c4d95ef39ed85d1fd9a9aabb0398dfa920ef335bce90db0a4ef8df'
        accepted = archive_record(INITIAL / 'portable-good.tar', expected, 'x86_64')
        code = refused(lambda: archive_record(INITIAL / 'portable-config-alias.tar', expected, 'x86_64'))
        self.assertEqual(code, 'IMAGE_SAVE_DUPLICATE_PATH')
        self.assertEqual(accepted['config_id'], expected)
        OBS['SEC01'] = {'refusal': code, 'positive_config_id': expected}

    def test_SEC02_ordered_symlink_lookup_and_matching_source(self):
        artifacts, sources = oracle_inputs()
        tree = ImageTree(INITIAL / 'oracle-link-dotdot.tar', GOOD_ROOT)
        self.assertEqual(tree.resolve('/oracle/worker.py'), '/foreign/worker.py')
        code = refused(lambda: verify_content(tree, 'x86_64', 'oracle', artifacts, sources))
        self.assertEqual(code, 'IMAGE_ORACLE_SOURCE')
        checked = verify_content(tree, 'x86_64', 'oracle', artifacts, sources | {'worker.py': b'inert unexpected source marker\n'})
        OBS['SEC02'] = {'refusal': code, 'root_observation': GOOD_ROOT, 'matching_source_sha256': checked['declared_files']['/oracle/worker.py']}

    def test_SEC03_exact_header_budget_and_duplicate_roots(self):
        positive = ImageTree(PREVIOUS / 'member-limit-positive.tar')
        self.assertEqual(positive.member_count, 20000)
        excess = refused(lambda: ImageTree(PREVIOUS / 'member-limit-negative.tar'))
        duplicate = refused(lambda: ImageTree(INITIAL / 'repeated-root-headers.tar', GOOD_ROOT))
        self.assertEqual(excess, 'IMAGE_DUPLICATE_OR_COUNT')
        self.assertEqual(duplicate, 'IMAGE_ROOT_DUPLICATE')
        OBS['SEC03'] = {'positive_count': 20000, 'excess_refusal': excess, 'duplicate_refusal': duplicate}

    def test_SEC04_original_bad_inode_alias_and_both_source_positives(self):
        artifacts, sources = oracle_inputs()
        code = refused(lambda: verify_content(ImageTree(PREVIOUS / 'hardlink-to-symlink.tar', GOOD_ROOT), 'x86_64', 'oracle', artifacts, sources))
        self.assertEqual(code, 'IMAGE_HARDLINK_LINK_TARGET')
        positives = {}
        for name in ('ordinary-hardlink.tar', 'ordinary-link.tar'):
            result = verify_content(ImageTree(PREVIOUS / name, GOOD_ROOT), 'x86_64', 'oracle', artifacts, sources)
            positives[name] = result['declared_files']['/oracle/worker.py']
            self.assertEqual(positives[name], c.sha(sources['worker.py']))
        OBS['SEC04'] = {'refusal': code, 'root_observation': GOOD_ROOT, 'positive_sources': positives}

    def test_SEC05_root_alias_conflict_duplicate_and_both_positive_forms(self):
        conflict = refused(lambda: ImageTree(PREVIOUS / 'root-alias-conflict.tar', GOOD_ROOT))
        duplicate = refused(lambda: ImageTree(PREVIOUS / 'root-alias-duplicate.tar'))
        self.assertEqual(conflict, 'IMAGE_ROOT_CHANGED')
        self.assertEqual(duplicate, 'IMAGE_ROOT_DUPLICATE')
        for observation in (None, GOOD_ROOT):
            tree = ImageTree(PREVIOUS / 'root-alias-positive.tar', observation)
            self.assertNotIn('/', tree.entries)
            verify_content(tree, 'x86_64', 'target', {'target': elf()}, {})
        OBS['SEC05'] = {'conflict_refusal': conflict, 'duplicate_refusal': duplicate, 'positive_with_and_without_observation': True}

class MaterialChangeTests(unittest.TestCase):
    def test_referenced_hardlink_requires_all_inode_metadata_fields(self):
        refusals = {}
        for index, (field, value) in enumerate((('uid', 999), ('gid', 999), ('mode', 0o700), ('mode', 0o777), ('mode', 0o4755))):
            path = tar('hardlink-metadata-%d.tar' % index, [('real', 'file', b'inert'), ('alias', 'hardlink', 'real', GOOD_ROOT | {field: value})])
            code = refused(lambda: ImageTree(path, GOOD_ROOT).file('/alias'))
            self.assertEqual(code, 'IMAGE_HARDLINK_METADATA')
            refusals[field + '=' + str(value)] = code
        for mode in (0o644, 0o755):
            meta = GOOD_ROOT | {'mode': mode}
            path = tar('hardlink-matching-%d.tar' % mode, [('real', 'file', b'inert', meta), ('alias', 'hardlink', 'real', meta)])
            self.assertEqual(ImageTree(path, GOOD_ROOT).file('/alias'), b'inert')
        OBS['referenced_hardlink_metadata'] = {'refusals': refusals, 'matching_modes_passed': [0o644, 0o755]}

    def test_hardlink_link_prefix_chain_directory_and_missing_targets_refuse(self):
        common = [('src', 'dir', ''), ('src/data', 'file', b'inert'), ('prefix', 'link', 'src'), ('direct', 'hardlink', 'src/data')]
        refusals = {}
        for index, (target, expected) in enumerate((('prefix/data', 'IMAGE_HARDLINK_LINK_TARGET'), ('direct', 'IMAGE_HARDLINK_LINK_TARGET'),
                                                    ('src', 'IMAGE_HARDLINK_TARGET'), ('missing', 'IMAGE_DANGLING_LINK'))):
            path = tar('hardlink-target-%d.tar' % index, common + [('alias', 'hardlink', target)])
            code = refused(lambda: ImageTree(path, GOOD_ROOT).file('/alias'))
            self.assertEqual(code, expected); refusals[target] = code
        OBS['hardlink_target_model'] = refusals

    def test_normalized_root_variants_and_non_directory_root_refuse(self):
        aliases = ('./.', './/.', '././', './//./.')
        for index, alias in enumerate(aliases):
            positive = tar('root-variant-%d.tar' % index, [(alias, 'dir', ''), ('target', 'file', elf())])
            tree = ImageTree(positive)
            self.assertEqual(tree.root, GOOD_ROOT); self.assertNotIn('/', tree.entries)
            verify_content(tree, 'x86_64', 'target', {'target': elf()}, {})
            bad = tar('root-nondirectory-%d.tar' % index, [(alias, 'file', b'')])
            self.assertEqual(refused(lambda: ImageTree(bad, GOOD_ROOT)), 'IMAGE_ROOT_DUPLICATE')
        OBS['root_variants'] = {'positive_aliases': aliases, 'non_directory_refused': True}

    def test_COR004_execute_intent_checked_even_after_read_only_visit(self):
        loader = '/usr/local/lib/loader.so'
        dirs = [(name, 'dir', '') for name in ('usr', 'usr/local', 'usr/local/lib')]
        for mode in (0o755, 0o644):
            path = tar('loader-mode-%d.tar' % mode, dirs + [('app', 'file', elf(needed=('loader.so',), interpreter=loader)),
                                                          (loader.lstrip('/'), 'file', elf(), GOOD_ROOT | {'mode': mode})])
            for required in (['/app', loader], [loader, '/app']):
                tree = ImageTree(path, GOOD_ROOT)
                calls = []
                original = tree.file
                def recording_file(path, executable=False):
                    calls.append({'path': path, 'executable': executable})
                    return original(path, executable=executable)
                tree.file = recording_file
                if mode == 0o755:
                    elf_inventory(tree, 'x86_64', required)
                else:
                    self.assertEqual(refused(lambda: elf_inventory(tree, 'x86_64', required)), 'IMAGE_FILE_EXECUTABLE')
                    if required == ['/app', loader]:
                        self.assertEqual(calls[0], {'path': loader, 'executable': False})
                        self.assertTrue(any(row['path'] == loader and row['executable'] for row in calls))
                        OBS['COR004_visit_trace'] = calls
        library = tar('ordinary-readable-library.tar', dirs + [('app', 'file', elf(needed=('loader.so',))),
                      (loader.lstrip('/'), 'file', elf(), GOOD_ROOT | {'mode': 0o644})])
        elf_inventory(ImageTree(library, GOOD_ROOT), 'x86_64', ['/app'])
        OBS['COR004'] = {'non_executable_interpreter_refused_in_both_visit_orders': True,
                         'executable_interpreter_passed': True, 'read_only_DT_NEEDED_library_passed': True}

    def test_reserved_dependency_key_refuses_but_similar_library_name_passes(self):
        self.assertEqual(refused(lambda: elf_record(elf(needed=('PT_INTERP',)), 'x86_64')), 'IMAGE_ELF_NEEDED')
        self.assertEqual(elf_record(elf(needed=('PT_INTERP.so',)), 'x86_64')['needed'], ['PT_INTERP.so'])
        OBS['reserved_dependency_key'] = {'exact_name_refused': True, 'non_reserved_similar_name_passed': True}

    def test_required_unreferenced_hardlink_metadata_cannot_modify_checked_inode(self):
        positive = tar('unused-hardlink-positive.tar', [('target', 'file', elf()), ('unused-alias', 'hardlink', 'target')])
        positive_record = verify_content(ImageTree(positive, GOOD_ROOT), 'x86_64', 'target', {'target': elf()}, {})
        bad = tar('unused-hardlink-conflict.tar', [('target', 'file', elf()),
                  ('unused-alias', 'hardlink', 'target', GOOD_ROOT | {'mode': 0o700})])
        tree = ImageTree(bad, GOOD_ROOT)
        result = verify_content(tree, 'x86_64', 'target', {'target': elf()}, {})
        self.assertEqual(c.canonical(result), c.canonical(positive_record))
        direct_alias_code = refused(lambda: tree.file('/unused-alias'))
        self.assertEqual(direct_alias_code, 'IMAGE_HARDLINK_METADATA')
        # Plain-text inode counterpart, constructed explicitly, never extracted.
        physical = FIXTURES / 'benign-shared-inode'
        physical.mkdir(mode=0o700)
        (physical / 'target.txt').write_bytes(b'inert shared text\n')
        (physical / 'target.txt').chmod(0o755)
        os.link(physical / 'target.txt', physical / 'alias.txt')
        (physical / 'alias.txt').chmod(0o700)
        self.assertEqual((physical / 'target.txt').stat().st_ino, (physical / 'alias.txt').stat().st_ino)
        self.assertEqual((physical / 'target.txt').stat().st_mode & 0o7777, 0o700)
        OBS['C0-IMG-SEC-06'] = {'fixture': str(bad.relative_to(OUT)), 'sha256': c.sha(bad.read_bytes()),
            'positive_fixture': str(positive.relative_to(OUT)), 'root_observation': GOOD_ROOT,
            'declared_target_sha256': result['declared_files']['/target'],
            'target_header_mode': tree.entries['/target']['mode'], 'hardlink_header_mode': tree.entries['/unused-alias']['mode'],
            'direct_alias_refusal': direct_alias_code, 'whole_content_admission_succeeded': True,
            'content_record_identical_to_matching_positive': True, 'content_sha256': c.sha(c.canonical(result)),
            'benign_shared_inode_mode_after_alias_chmod': 0o700,
            'execution_authorized': result['execution_authorized'], 'runtime_acceptance': result['runtime_acceptance']}
        with self.assertRaisesRegex(c.Invalid, 'IMAGE_HARDLINK_METADATA'):
            verify_content(ImageTree(bad, GOOD_ROOT), 'x86_64', 'target', {'target': elf()}, {})

if __name__ == '__main__':
    os.umask(0o077)
    if RUN_NAME: OUT.mkdir(mode=0o700)
    FIXTURES.mkdir(mode=0o700)
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    report = {'python': sys.version, 'tests_run': result.testsRun,
              'failures': [{'id': item.id(), 'traceback': text} for item, text in result.failures],
              'errors': [{'id': item.id(), 'traceback': text} for item, text in result.errors],
              'skipped': list(result.skipped), 'observations': OBS,
              'network_calls': 0, 'engine_or_helper_executions': 0, 'native_or_worker_executions': 0, 'archive_extraction': False}
    with (OUT / 'independent-tests.json').open('x') as file:
        json.dump(report, file, sort_keys=True, indent=2)
        file.write('\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
