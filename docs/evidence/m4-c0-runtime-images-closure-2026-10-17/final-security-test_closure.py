"""Independent bounded constructor checks. Inert archives; no actor/helper calls."""
import io
import json
import os
from pathlib import Path
import re
import sys
import tarfile
import unittest

ROOT = Path('/tmp/opencode/c0-runtime-images-closure-2026-10-17')
BASE = ROOT / 'security'
RUN_NAME = os.environ.get('C0_CLOSURE_REVIEW_RUN', '')
if RUN_NAME and not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_-]{0,63}', RUN_NAME): raise ValueError('Invalid run name')
OUT = BASE / RUN_NAME if RUN_NAME else BASE
FIXTURES = OUT / 'fixtures'
INITIAL = Path('/tmp/opencode/c0-runtime-images-2026-10-17/security/unit-run-02/inert-fixtures')
REMEDIATION = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17/security/repeat-02/fixtures')
FINAL = Path('/tmp/opencode/c0-runtime-images-final-2026-10-17/security/repeat-02/fixtures')
LAST = Path('/tmp/opencode/c0-runtime-images-completion-2026-10-17/security/repeat-02/fixtures')
PACKAGE = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
sys.dont_write_bytecode = True
sys.path.insert(0, str(PACKAGE))
import contracts as c
import image_artifacts
from image_artifacts import ImageTree, archive_record, elf_inventory, elf_record, verify_content
from oracle_protocol import SOURCE_FILES
from test_image_artifacts import elf
assert Path(image_artifacts.__file__) == PACKAGE / 'image_artifacts.py'
GOOD_ROOT = {'uid': 0, 'gid': 0, 'mode': 0o755}
OBS = {}

def tar(name, rows):
    path = FIXTURES / name
    with tarfile.open(path, 'x', format=tarfile.USTAR_FORMAT) as archive:
        for entry in rows:
            name, kind, value = entry[:3]
            metadata = entry[3] if len(entry) > 3 else GOOD_ROOT
            member = tarfile.TarInfo(name)
            member.uid, member.gid, member.mode = (metadata[key] for key in ('uid', 'gid', 'mode'))
            if kind == 'file':
                member.size = len(value); archive.addfile(member, io.BytesIO(value))
            else:
                member.type = {'dir': tarfile.DIRTYPE, 'link': tarfile.SYMTYPE, 'hardlink': tarfile.LNKTYPE}[kind]
                member.linkname = value; archive.addfile(member)
    return path

def refusal(call):
    try: call()
    except c.Invalid as error: return str(error)
    raise AssertionError('Expected refusal')

def oracle_inputs():
    return ({'oracle_elf': c.sha(elf()), 'extensions': {'/usr/local/lib/fixture.so': c.sha(elf())}},
            {name: ('inert expected source: ' + name + '\n').encode() for name in (*SOURCE_FILES, 'build_oracle.py')})

class ExistingClosures(unittest.TestCase):
    def test_SEC01_portable_collision_and_matching_config(self):
        expected = 'sha256:c042848616c4d95ef39ed85d1fd9a9aabb0398dfa920ef335bce90db0a4ef8df'
        self.assertEqual(archive_record(INITIAL / 'portable-good.tar', expected, 'x86_64')['config_id'], expected)
        code = refusal(lambda: archive_record(INITIAL / 'portable-config-alias.tar', expected, 'x86_64'))
        self.assertEqual(code, 'IMAGE_SAVE_DUPLICATE_PATH'); OBS['SEC01'] = {'refusal': code, 'positive': True}

    def test_SEC02_symlink_order_and_matching_source(self):
        artifacts, sources = oracle_inputs()
        tree = ImageTree(INITIAL / 'oracle-link-dotdot.tar', GOOD_ROOT)
        self.assertEqual(tree.resolve('/oracle/worker.py'), '/foreign/worker.py')
        code = refusal(lambda: verify_content(tree, 'x86_64', 'oracle', artifacts, sources))
        self.assertEqual(code, 'IMAGE_ORACLE_SOURCE')
        verify_content(tree, 'x86_64', 'oracle', artifacts, sources | {'worker.py': b'inert unexpected source marker\n'})
        OBS['SEC02'] = {'refusal': code, 'matching_source_positive': True, 'root_observation': GOOD_ROOT}

    def test_SEC03_duplicate_roots_and_exact_budget(self):
        self.assertEqual(ImageTree(REMEDIATION / 'member-limit-positive.tar').member_count, 20000)
        excess = refusal(lambda: ImageTree(REMEDIATION / 'member-limit-negative.tar'))
        duplicate = refusal(lambda: ImageTree(INITIAL / 'repeated-root-headers.tar', GOOD_ROOT))
        self.assertEqual(excess, 'IMAGE_DUPLICATE_OR_COUNT'); self.assertEqual(duplicate, 'IMAGE_ROOT_DUPLICATE')
        OBS['SEC03'] = {'positive_count': 20000, 'excess_refusal': excess, 'duplicate_refusal': duplicate}

    def test_SEC04_hardlink_target_and_source_positives(self):
        artifacts, sources = oracle_inputs()
        code = refusal(lambda: ImageTree(REMEDIATION / 'hardlink-to-symlink.tar', GOOD_ROOT))
        self.assertEqual(code, 'IMAGE_HARDLINK_LINK_TARGET')
        for name in ('ordinary-hardlink.tar', 'ordinary-link.tar'):
            verify_content(ImageTree(REMEDIATION / name, GOOD_ROOT), 'x86_64', 'oracle', artifacts, sources)
        OBS['SEC04'] = {'refusal': code, 'direct_hardlink_positive': True, 'symbolic_source_positive': True}

    def test_SEC05_root_alias_metadata_and_positives(self):
        conflict = refusal(lambda: ImageTree(REMEDIATION / 'root-alias-conflict.tar', GOOD_ROOT))
        duplicate = refusal(lambda: ImageTree(REMEDIATION / 'root-alias-duplicate.tar'))
        self.assertEqual(conflict, 'IMAGE_ROOT_CHANGED'); self.assertEqual(duplicate, 'IMAGE_ROOT_DUPLICATE')
        for observed in (None, GOOD_ROOT):
            tree = ImageTree(REMEDIATION / 'root-alias-positive.tar', observed)
            self.assertNotIn('/', tree.entries)
            verify_content(tree, 'x86_64', 'target', {'target': elf()}, {})
        OBS['SEC05'] = {'conflict_refusal': conflict, 'duplicate_refusal': duplicate, 'positive_with_and_without_observation': True}

    def test_loader_execute_and_reserved_key(self):
        loader = '/usr/local/lib/loader.so'
        for required in (['/app', loader], [loader, '/app']):
            self.assertEqual(refusal(lambda: elf_inventory(ImageTree(FINAL / 'loader-mode-420.tar', GOOD_ROOT), 'x86_64', required)), 'IMAGE_FILE_EXECUTABLE')
            elf_inventory(ImageTree(FINAL / 'loader-mode-493.tar', GOOD_ROOT), 'x86_64', required)
        elf_inventory(ImageTree(FINAL / 'ordinary-readable-library.tar', GOOD_ROOT), 'x86_64', ['/app'])
        self.assertEqual(refusal(lambda: elf_record(elf(needed=('PT_INTERP',)), 'x86_64')), 'IMAGE_ELF_NEEDED')
        self.assertEqual(elf_record(elf(needed=('PT_INTERP.so',)), 'x86_64')['needed'], ['PT_INTERP.so'])
        OBS['COR004'] = {'non_executable_loader_refused_in_both_visit_orders': True, 'executable_loader_positive': True,
                         'read_only_library_positive': True, 'reserved_name_refused': True, 'similar_name_positive': True}

class ParentRecordClosure(unittest.TestCase):
    def test_exact_SEC06_parent_redirection_negative_and_literal_forward_positives(self):
        refusals = {}
        for name in ('parent-redirect-conflict.tar', 'parent-redirect-positive.tar'):
            path = LAST / name
            code = refusal(lambda: ImageTree(path, GOOD_ROOT))
            self.assertEqual(code, 'IMAGE_MEMBER_PARENT')
            refusals[name] = {'sha256': c.sha(path.read_bytes()), 'code': code}
        positives = {}
        for name in ('literal-parent-positive.tar', 'metadata-False-0.tar', 'metadata-True-0.tar'):
            result = verify_content(ImageTree(LAST / name, GOOD_ROOT), 'x86_64', 'target', {'target': elf()}, {})
            positives[name] = c.sha(c.canonical(result))
            self.assertIs(result['execution_authorized'], False)
        self.assertEqual(ImageTree(LAST / 'forward-directory.tar', GOOD_ROOT).file('/alias'), b'inert')
        OBS['SEC06_parent_record'] = {'refusals': refusals, 'positive_content_sha256': positives,
                                     'forward_directory_and_target_positive': True, 'valid_root_observation': GOOD_ROOT}

    def test_original_SEC06_metadata_and_both_member_orders(self):
        original = refusal(lambda: ImageTree(FINAL / 'unused-hardlink-conflict.tar', GOOD_ROOT))
        self.assertEqual(original, 'IMAGE_HARDLINK_METADATA')
        verify_content(ImageTree(FINAL / 'unused-hardlink-positive.tar', GOOD_ROOT), 'x86_64', 'target', {'target': elf()}, {})
        results = []
        for forward in (False, True):
            for index in (1, 2, 3):
                name = 'metadata-%s-%d.tar' % (forward, index)
                code = refusal(lambda: ImageTree(LAST / name, GOOD_ROOT))
                self.assertEqual(code, 'IMAGE_HARDLINK_METADATA'); results.append({'fixture': name, 'code': code})
        OBS['SEC06_inode_metadata'] = {'original_refusal': original, 'matching_positive': True, 'both_order_mismatch_refusals': results}

    def test_every_declared_hardlink_is_checked_not_just_selected_or_first(self):
        names = ('a/first', 'a/b/second', 'third')
        for bad_index in (None, 0, 1, 2):
            rows = [(name, 'hardlink', 'target', GOOD_ROOT | ({'mode': 0o700} if index == bad_index else {}))
                    for index, name in enumerate(names)]
            # Both parent directories and target occur after every alias.
            rows += [('target', 'file', elf()), ('a/b', 'dir', ''), ('a', 'dir', '')]
            path = tar('all-records-%s.tar' % bad_index, rows)
            if bad_index is None:
                tree = ImageTree(path, GOOD_ROOT)
                verify_content(tree, 'x86_64', 'target', {'target': elf()}, {})
                self.assertEqual({tree.resolve('/' + name) for name in names}, {'/target'})
            else:
                self.assertEqual(refusal(lambda: ImageTree(path, GOOD_ROOT)), 'IMAGE_HARDLINK_METADATA')
        OBS['all_hardlink_records'] = {'names': names, 'matching_forward_map_passes': True,
                                       'each_individual_unused_mismatch_refuses': True}

    def test_all_entry_kinds_and_nondirectory_parent_types_refuse(self):
        count = 0
        for parent_kind in ('link', 'hardlink', 'file', 'missing'):
            for child_kind in ('file', 'dir', 'link', 'hardlink'):
                rows = [('target', 'file', elf()), ('real', 'dir', ''), ('real/child', 'file', b'inert')]
                if parent_kind != 'missing':
                    rows += [('parent', parent_kind, {'link': 'real', 'hardlink': 'target', 'file': b'inert'}[parent_kind])]
                value = {'file': b'inert', 'dir': '', 'link': '../target', 'hardlink': 'target'}[child_kind]
                rows += [('parent/child', child_kind, value)]
                path = tar('parent-%s-child-%s.tar' % (parent_kind, child_kind), rows)
                self.assertEqual(refusal(lambda: ImageTree(path, GOOD_ROOT)), 'IMAGE_MEMBER_PARENT'); count += 1
        OBS['all_entry_parent_kinds'] = {'negative_combinations': count, 'refusal': 'IMAGE_MEMBER_PARENT'}

    def test_all_ancestor_levels_are_checked_with_forward_directory_positives(self):
        for forward in (False, True):
            for redirect in (False, True):
                ancestors = [('a', 'link' if redirect else 'dir', 'real' if redirect else ''), ('a/b', 'dir', '')]
                child = [('a/b/alias', 'hardlink', 'target')]
                rows = [('target', 'file', elf()), ('real', 'dir', '')] + (child + ancestors if forward else ancestors + child)
                path = tar('ancestor-%s-%s.tar' % (forward, redirect), rows)
                if redirect:
                    self.assertEqual(refusal(lambda: ImageTree(path, GOOD_ROOT)), 'IMAGE_MEMBER_PARENT')
                else:
                    tree = ImageTree(path, GOOD_ROOT)
                    self.assertEqual(tree.resolve('/a/b/alias'), '/target')
                    verify_content(tree, 'x86_64', 'target', {'target': elf()}, {})
        OBS['ancestor_induction'] = {'indirect_redirect_refused_in_both_orders': True,
                                     'literal_multi_level_forward_and_backward_maps_pass': True}

if __name__ == '__main__':
    os.umask(0o077)
    if RUN_NAME: OUT.mkdir(mode=0o700)
    FIXTURES.mkdir(mode=0o700)
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    report = {'python': sys.version, 'tests_run': result.testsRun,
              'failures': [{'id': test.id(), 'traceback': trace} for test, trace in result.failures],
              'errors': [{'id': test.id(), 'traceback': trace} for test, trace in result.errors],
              'skipped': list(result.skipped), 'observations': OBS, 'pass': result.wasSuccessful() and not result.skipped,
              'network_calls': 0, 'engine_helper_native_worker_executions': 0, 'archive_extraction': False}
    with (OUT / 'independent-tests.json').open('x') as file:
        json.dump(report, file, sort_keys=True, indent=2); file.write('\n')
    raise SystemExit(0 if report['pass'] else 1)
