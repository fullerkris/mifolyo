"""Bounded SEC-06 constructor review using inert archives only."""
import io
import json
import os
from pathlib import Path
import re
import sys
import tarfile
import unittest

ROOT = Path('/tmp/opencode/c0-runtime-images-completion-2026-10-17')
BASE = ROOT / 'security'
RUN_NAME = os.environ.get('C0_COMPLETION_REVIEW_RUN', '')
if RUN_NAME and not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_-]{0,63}', RUN_NAME): raise ValueError('Invalid run name')
OUT = BASE / RUN_NAME if RUN_NAME else BASE
FIXTURES = OUT / 'fixtures'
INITIAL = Path('/tmp/opencode/c0-runtime-images-2026-10-17/security/unit-run-02/inert-fixtures')
REMEDIATION = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17/security/repeat-02/fixtures')
LAST = Path('/tmp/opencode/c0-runtime-images-final-2026-10-17/security/repeat-02/fixtures')
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
    raise AssertionError('Expected a refusal')

def oracle_inputs():
    return ({'oracle_elf': c.sha(elf()), 'extensions': {'/usr/local/lib/fixture.so': c.sha(elf())}},
            {name: ('inert expected source: ' + name + '\n').encode() for name in (*SOURCE_FILES, 'build_oracle.py')})

class ExistingClosureChecks(unittest.TestCase):
    def test_SEC01_normalized_collision_and_matching_config(self):
        expected = 'sha256:c042848616c4d95ef39ed85d1fd9a9aabb0398dfa920ef335bce90db0a4ef8df'
        self.assertEqual(archive_record(INITIAL / 'portable-good.tar', expected, 'x86_64')['config_id'], expected)
        code = refusal(lambda: archive_record(INITIAL / 'portable-config-alias.tar', expected, 'x86_64'))
        self.assertEqual(code, 'IMAGE_SAVE_DUPLICATE_PATH'); OBS['SEC01'] = {'refusal': code, 'positive': True}

    def test_SEC02_path_order_and_matching_source(self):
        artifacts, sources = oracle_inputs()
        tree = ImageTree(INITIAL / 'oracle-link-dotdot.tar', GOOD_ROOT)
        self.assertEqual(tree.resolve('/oracle/worker.py'), '/foreign/worker.py')
        code = refusal(lambda: verify_content(tree, 'x86_64', 'oracle', artifacts, sources))
        self.assertEqual(code, 'IMAGE_ORACLE_SOURCE')
        verify_content(tree, 'x86_64', 'oracle', artifacts, sources | {'worker.py': b'inert unexpected source marker\n'})
        OBS['SEC02'] = {'refusal': code, 'positive': True, 'valid_root_observation': GOOD_ROOT}

    def test_SEC03_duplicate_and_exact_member_budget(self):
        self.assertEqual(ImageTree(REMEDIATION / 'member-limit-positive.tar').member_count, 20000)
        excess = refusal(lambda: ImageTree(REMEDIATION / 'member-limit-negative.tar'))
        duplicate = refusal(lambda: ImageTree(INITIAL / 'repeated-root-headers.tar', GOOD_ROOT))
        self.assertEqual(excess, 'IMAGE_DUPLICATE_OR_COUNT'); self.assertEqual(duplicate, 'IMAGE_ROOT_DUPLICATE')
        OBS['SEC03'] = {'positive_count': 20000, 'excess_refusal': excess, 'duplicate_refusal': duplicate}

    def test_SEC04_hardlink_inode_and_symbolic_source_positives(self):
        artifacts, sources = oracle_inputs()
        code = refusal(lambda: ImageTree(REMEDIATION / 'hardlink-to-symlink.tar', GOOD_ROOT))
        self.assertEqual(code, 'IMAGE_HARDLINK_LINK_TARGET')
        for name in ('ordinary-hardlink.tar', 'ordinary-link.tar'):
            verify_content(ImageTree(REMEDIATION / name, GOOD_ROOT), 'x86_64', 'oracle', artifacts, sources)
        OBS['SEC04'] = {'refusal': code, 'direct_hardlink_positive': True, 'symbolic_source_positive': True}

    def test_SEC05_root_metadata_alias_and_both_positives(self):
        conflict = refusal(lambda: ImageTree(REMEDIATION / 'root-alias-conflict.tar', GOOD_ROOT))
        duplicate = refusal(lambda: ImageTree(REMEDIATION / 'root-alias-duplicate.tar'))
        self.assertEqual(conflict, 'IMAGE_ROOT_CHANGED'); self.assertEqual(duplicate, 'IMAGE_ROOT_DUPLICATE')
        for root in (None, GOOD_ROOT):
            tree = ImageTree(REMEDIATION / 'root-alias-positive.tar', root)
            self.assertNotIn('/', tree.entries)
            verify_content(tree, 'x86_64', 'target', {'target': elf()}, {})
        OBS['SEC05'] = {'conflict_refusal': conflict, 'duplicate_refusal': duplicate, 'with_and_without_observation_positive': True}

    def test_loader_execute_and_reserved_dependency_closures(self):
        loader = '/usr/local/lib/loader.so'
        for required in (['/app', loader], [loader, '/app']):
            self.assertEqual(refusal(lambda: elf_inventory(ImageTree(LAST / 'loader-mode-420.tar', GOOD_ROOT), 'x86_64', required)), 'IMAGE_FILE_EXECUTABLE')
            elf_inventory(ImageTree(LAST / 'loader-mode-493.tar', GOOD_ROOT), 'x86_64', required)
        elf_inventory(ImageTree(LAST / 'ordinary-readable-library.tar', GOOD_ROOT), 'x86_64', ['/app'])
        self.assertEqual(refusal(lambda: elf_record(elf(needed=('PT_INTERP',)), 'x86_64')), 'IMAGE_ELF_NEEDED')
        self.assertEqual(elf_record(elf(needed=('PT_INTERP.so',)), 'x86_64')['needed'], ['PT_INTERP.so'])
        OBS['COR004'] = {'execute_required_in_both_visit_orders': True, 'executable_loader_positive': True,
                         'read_only_library_positive': True, 'reserved_key_refused': True, 'similar_name_positive': True}

class ConstructorChecks(unittest.TestCase):
    def test_SEC06_original_negative_and_matching_positive(self):
        negative = LAST / 'unused-hardlink-conflict.tar'
        code = refusal(lambda: ImageTree(negative, GOOD_ROOT))
        self.assertEqual(code, 'IMAGE_HARDLINK_METADATA')
        result = verify_content(ImageTree(LAST / 'unused-hardlink-positive.tar', GOOD_ROOT), 'x86_64', 'target', {'target': elf()}, {})
        self.assertEqual(c.sha(c.canonical(result)), '18359ca24129e49759f738007a3d6fecf57cb782d5c8d999b4fadf09c8eb8136')
        OBS['SEC06_original'] = {'negative_sha256': c.sha(negative.read_bytes()), 'constructor_refusal': code,
                                 'matching_positive_content_sha256': c.sha(c.canonical(result))}

    def test_unused_mode_uid_gid_mismatch_in_both_member_orders(self):
        results = []
        for forward in (False, True):
            for index, metadata in enumerate((GOOD_ROOT, GOOD_ROOT | {'mode': 0o700}, GOOD_ROOT | {'uid': 999}, GOOD_ROOT | {'gid': 999})):
                rows = [('target', 'file', elf()), ('unused', 'hardlink', 'target', metadata)]
                if forward: rows.reverse()
                path = tar('metadata-%s-%d.tar' % (forward, index), rows)
                if index == 0:
                    verify_content(ImageTree(path, GOOD_ROOT), 'x86_64', 'target', {'target': elf()}, {})
                    result = 'PASS'
                else:
                    result = refusal(lambda: ImageTree(path, GOOD_ROOT))
                    self.assertEqual(result, 'IMAGE_HARDLINK_METADATA')
                results.append({'forward': forward, 'metadata': metadata, 'result': result})
        OBS['SEC06_member_order_matrix'] = results

    def test_forward_directory_and_target_and_no_map_mutation(self):
        path = tar('forward-directory.tar', [('alias', 'hardlink', 'future/data'), ('future', 'dir', ''), ('future/data', 'file', b'inert')])
        tree = ImageTree(path, GOOD_ROOT)
        before = {name: dict(row) for name, row in tree.entries.items()}
        self.assertEqual(tree.file('/alias'), b'inert')
        self.assertEqual(before, tree.entries)
        OBS['forward_directory'] = {'resolved': tree.resolve('/alias'), 'entry_count': len(tree.entries), 'map_unchanged': True}

    def test_unused_dangling_chain_and_directory_targets_refuse_at_construction(self):
        cases = [([('unused', 'hardlink', 'missing')], 'IMAGE_DANGLING_LINK'),
                 ([('first', 'hardlink', 'second'), ('second', 'hardlink', 'first')], 'IMAGE_HARDLINK_LINK_TARGET'),
                 ([('directory', 'dir', ''), ('unused', 'hardlink', 'directory')], 'IMAGE_HARDLINK_TARGET')]
        codes = []
        for index, (rows, expected) in enumerate(cases):
            path = tar('unsupported-unused-%d.tar' % index, rows)
            code = refusal(lambda: ImageTree(path, GOOD_ROOT)); self.assertEqual(code, expected); codes.append(code)
        OBS['unused_target_refusals'] = codes

    def test_required_constructor_checks_the_actual_hardlink_record_not_a_redirect(self):
        common = [('target', 'file', elf()), ('etc', 'dir', ''), ('etc/hosts', 'file', b'inert generated hosts marker\n'),
                  ('alias-parent', 'link', 'etc')]
        good = tar('parent-redirect-positive.tar', common + [('alias-parent/hosts', 'hardlink', 'target')])
        positive = verify_content(ImageTree(good, GOOD_ROOT), 'x86_64', 'target', {'target': elf()}, {})
        bad = tar('parent-redirect-conflict.tar', common + [('alias-parent/hosts', 'hardlink', 'target', GOOD_ROOT | {'mode': 0o700})])
        tree = ImageTree(bad, GOOD_ROOT)
        result = verify_content(tree, 'x86_64', 'target', {'target': elf()}, {})
        self.assertEqual(c.canonical(result), c.canonical(positive))
        self.assertEqual(tree.resolve('/alias-parent/hosts'), '/etc/hosts')
        self.assertEqual(tree.entries['/alias-parent/hosts']['target'], '/target')
        # A direct, unshadowed name for the same hardlink record fails as intended.
        direct = tar('direct-conflict-control.tar', [('target', 'file', elf()), ('unused', 'hardlink', 'target', GOOD_ROOT | {'mode': 0o700})])
        self.assertEqual(refusal(lambda: ImageTree(direct, GOOD_ROOT)), 'IMAGE_HARDLINK_METADATA')
        literal = tar('literal-parent-positive.tar', common[:-1] + [('alias-parent', 'dir', ''), ('alias-parent/hosts', 'hardlink', 'target')])
        verify_content(ImageTree(literal, GOOD_ROOT), 'x86_64', 'target', {'target': elf()}, {})
        OBS['SEC06_parent_redirect'] = {'fixture': str(bad.relative_to(OUT)), 'sha256': c.sha(bad.read_bytes()),
            'matching_metadata_fixture': str(good.relative_to(OUT)), 'hardlink_record_name': '/alias-parent/hosts',
            'declared_hardlink_target': '/target', 'name_lookup_checks_instead': tree.resolve('/alias-parent/hosts'),
            'target_mode': tree.entries['/target']['mode'], 'unchecked_hardlink_mode': tree.entries['/alias-parent/hosts']['mode'],
            'whole_content_admission_succeeded': True, 'same_content_record_as_matching_metadata': True,
            'content_sha256': c.sha(c.canonical(result)), 'direct_unshadowed_control_refusal': 'IMAGE_HARDLINK_METADATA',
            'literal_directory_parent_positive': str(literal.relative_to(OUT)),
            'runtime_acceptance': result['runtime_acceptance'], 'execution_authorized': result['execution_authorized']}
        with self.assertRaises(c.Invalid): ImageTree(bad, GOOD_ROOT)

if __name__ == '__main__':
    os.umask(0o077)
    if RUN_NAME: OUT.mkdir(mode=0o700)
    FIXTURES.mkdir(mode=0o700)
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    report = {'python': sys.version, 'tests_run': result.testsRun,
              'failures': [{'id': test.id(), 'traceback': trace} for test, trace in result.failures],
              'errors': [{'id': test.id(), 'traceback': trace} for test, trace in result.errors],
              'skipped': list(result.skipped), 'observations': OBS,
              'engine_helper_native_worker_executions': 0, 'network_calls': 0, 'archive_extraction': False}
    with (OUT / 'independent-tests.json').open('x') as file:
        json.dump(report, file, sort_keys=True, indent=2); file.write('\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
