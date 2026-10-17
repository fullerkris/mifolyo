"""Write the source-bound closure report only under this new review directory."""
import json
from common import OUT, BASE, PREVIOUS, INVENTORY_SHA, REVALIDATION_SHA, files, hashfile, load

inventory = load(BASE / 'source-inventory.json')
before = load(OUT / 'inventory-before.json')
after = load(OUT / 'inventory-after.json')
probes = load(OUT / 'probes.json')
replay = load(OUT / 'replay.json')
prior = load(PREVIOUS / 'correctness/review.json')
assert probes['status'] == replay['status'] == 'PASS' and not probes['failed']
assert after['original_inputs_unchanged'] and before['source_files'] == after['source_files'] and before['protected_roots'] == after['protected_roots']
closures = [dict(row) for row in prior['prior_go']['finding_closure']]
closures.append({'id': 'C0ARCH04-COR-007', 'status': 'CLOSED',
                 'location': 'tests/crawl-jobs-v2-observer/image_artifacts.py:125-134',
                 'source_sha256': inventory['files']['tests/crawl-jobs-v2-observer/image_artifacts.py'],
                 'reason': 'Global literal-parent validation runs on the complete map before hardlink traversal. Every nonroot ancestor must be a real directory record; own-name lookup cannot bypass the stored hardlink through a symlink/file parent. Forward parents/targets and legitimate symbolic-reference lookup remain supported.',
                 'exact_prior_counterexamples_refused': 8, 'refusal': 'IMAGE_MEMBER_PARENT',
                 'independent_controls': 'probes.json', 'current_regression': 'test_image_review.ImageReviewTests.test_flat_archive_entries_require_literal_directory_ancestors'})
record = {
    'kind': 'c0_arch_04_cor007_bounded_closure_review', 'version': 1, 'date': '2026-10-17',
    'verdict': 'GO_FOR_CONTINUED_IMPLEMENTATION', 'execution_authorized': False, 'runtime_acceptance': False,
    'summary': 'COR-007 is closed; COR-001 through COR-006 remain closed. No blocker found in the small constructor delta. Actual retained content/config/archive records remain unchanged.',
    'findings': [], 'finding_closure': closures,
    'source': {'head': after['head'], 'branch': after['branch'], 'inventory_sha256': INVENTORY_SHA,
               'package_files': 55, 'auxiliary_files': 3, 'before_after_match': True, 'exact_inventory': True,
               'files': inventory['files'] | inventory['auxiliary_files'], 'delta_from_completion': replay['delta_from_completion'],
               'delta_from_construction': replay['delta_from_construction']},
    'artifact_revalidation': {'path': str(BASE / 'artifact-revalidation.json'), 'sha256': REVALIDATION_SHA,
                              'independently_verified': True, 'rebuild_required': False,
                              'unchanged_construction_inputs': replay['unchanged_construction_inputs'],
                              'unchanged_driver_and_auxiliary': replay['unchanged_driver_and_auxiliary']},
    'checks': {'independent_probe_groups': probes['groups'], 'independent_probe_status': probes['status'],
               'exact_prior_counterexamples': probes['exact_prior_counterexamples'],
               'image_unit_tests': {'count': 32, 'status': 'PASS', 'log': 'image-tests.log', 'resource_warnings': 'error'},
               'full_local_reports': replay['tests'], 'full_288_suite_rerun': False,
               'actual_images': replay['images'], 'content_records_byte_identical': 6,
               'actual_ancestry_verification': 'Every ancestor of every stored entry independently checked, in addition to constructor validation.',
               'prior_overlay_relation_retained_by_identical_archive_hashes': True, 'independent_overlay_recomputed_this_round': False,
               'engine_commands_executed': 0, 'mounted_root_helper_executed': False, 'native_hosted_image_ci_executed': False},
    'preservation': {'all_original_files_unchanged': True, 'protected_file_count': sum(map(len, before['protected_roots'].values())),
                     'protected_roots': {path: len(value) for path, value in before['protected_roots'].items()},
                     'before_manifest_sha256': hashfile(OUT / 'inventory-before.json'), 'after_manifest_sha256': hashfile(OUT / 'inventory-after.json'),
                     'unreviewed_parent_additions': after.get('unreviewed_parent_additions', {}), 'whole_git_status_compared': False},
    'prior_review': {'path': str(PREVIOUS / 'correctness/review.json'), 'sha256': hashfile(PREVIOUS / 'correctness/review.json'),
                     'report_md_sha256': hashfile(PREVIOUS / 'correctness/review.md'), 'originals_preserved': True},
    'limits': [
        'New writes only in the closure correctness directory. No prior report/probe/script/source/artifact edited; no previous review script executed.',
        'No engine or mounted-root helper execution, builds/pulls/loads/starts, mounts/unmounts, real C0 native actor/collector/worker-main execution, tracing, installed filters, deployment or calibration.',
        'Image tests use inert fixtures/private synthetic extension inventories/fake engines. Mounted-root helper text was only compared to retained commands, never executed.',
        'Archive members were read as data streams, never extracted onto the host or executed.',
        'Historical root/container/cleanup observations are joined within retained evidence, not freshly observed on an engine.',
        '32 current image unit tests and 80 independent groups ran; complete 288-test source-bound reports/287-ID continuity were verified rather than rerun.',
        'Previous independent overlay result carried forward by identical archive bytes; this round independently checks all actual entry ancestors. No timestamps/xattrs or full dynamic-loader execution claim.',
        'Native hosted image CI and native/runtime acceptance remain unexecuted/unproven. C0-ARCH-05 calibration and C0-ARCH-06 publication remain separately gated.'
    ],
    'next_steps': 'Continue implementation/documentation with COR-001 through COR-007 closures preserved. Hosted/native execution still requires separate approval and evidence.',
    'review_markdown_sha256': hashfile(OUT / 'review.md'),
}
record['support_hashes'] = {path.name: hashfile(path) for path in sorted(OUT.iterdir()) if path.is_file() and path.name not in ('review.json', 'report-hashes.txt')}
record['new_fixture_hashes'] = files(OUT / 'inert-fixtures')
(OUT / 'review.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
print('review.md', hashfile(OUT / 'review.md'))
print('review.json', hashfile(OUT / 'review.json'))
