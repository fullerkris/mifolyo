"""Write only the new completion review JSON and report hashes."""
import json
from common import OUT, BASE, GO, INVENTORY_SHA, REVALIDATION_SHA, files, hashfile, load

inventory = load(BASE / 'source-inventory.json')
before = load(OUT / 'inventory-before.json')
after = load(OUT / 'inventory-after.json')
probes = load(OUT / 'probes.json')
replay = load(OUT / 'replay.json')
prior = load(GO / 'correctness/review.json')
assert after['original_inputs_unchanged'] and before['source_files'] == after['source_files'] and before['protected_roots'] == after['protected_roots']
assert replay['status'] == 'PASS_RETAINED_ARTIFACTS'
closures = []
for original in prior['finding_closure']:
    closures.append({'id': original['id'], 'status': 'CLOSED_RETAINED', 'prior_status': original['status'],
                     'reason': 'Prior correction remains present; only the post-parse hardlink scan and its new test differ from the GO snapshot. Current image regression tests pass.'})
finding = {
    'id': 'C0ARCH04-COR-007', 'status': 'OPEN', 'priority': 'P2', 'marker': 'suggestion',
    'blocks_requested_constructor_confirmation': True,
    'title': 'Parent symlink can redirect constructor resolution away from the hardlink row being audited',
    'locations': ['tests/crawl-jobs-v2-observer/image_artifacts.py:127-129', 'tests/crawl-jobs-v2-observer/image_artifacts.py:140-155'],
    'source_sha256': inventory['files']['tests/crawl-jobs-v2-observer/image_artifacts.py'],
    'cause': 'self.resolve(name) follows the alias pathname parent symlink and reaches another regular entry. The iterated hardlink row is never visited; neither its inode metadata nor its declared target is checked.',
    'minimal_fixture': [{'path': '.', 'kind': 'directory', 'mode': '0755', 'uid': 0, 'gid': 0},
                        {'path': 'target', 'kind': 'inert regular ELF', 'mode': '0755', 'uid': 0, 'gid': 0},
                        {'path': 'dir', 'kind': 'symlink', 'target': '.'},
                        {'path': 'dir/target', 'kind': 'hardlink', 'target': 'target', 'mode': '0700', 'uid': 0, 'gid': 0}],
    'observed': 'Constructor and full native content validation accept mode/UID/GID mismatch variants, and even a hardlink declaring a missing target. resolve(/dir/target) reaches /target instead. Payload digests equal positive controls.',
    'reproducer_script': str(OUT / 'probes.py'), 'reproducer_results': 'probes.json',
    'probe_ids': probes['unexpected'], 'architectures': ['aarch64', 'x86_64'],
    'suggestion': 'Reject ambiguous hardlink member ancestry (for example require real directory rows for its archive-member parents), or validate the iterated row and declared target explicitly while retaining hardlink-target restrictions and post-map forward-reference support.',
    'actual_images_affected': False,
    'actual_image_check': 'All four actual oracle hardlinks have real directory parents and matching metadata; both native roles contain no hardlinks. All six content/config/archive records revalidate unchanged.',
    'limit': 'Pure parser/in-memory checks only; no filesystem extraction or external loader behavior is claimed.'
}
record = {
    'kind': 'c0_arch_04_completion_constructor_correctness_review', 'version': 1, 'date': '2026-10-17',
    'verdict': 'NEEDS_REMEDIATION', 'execution_authorized': False, 'runtime_acceptance': False,
    'summary': 'COR-001 through COR-006 closures remain valid and all retained images revalidate. The new all-hardlink constructor guarantee is incomplete for shadowed member paths; COR-007 is open.',
    'source': {'head': after['head'], 'branch': after['branch'], 'inventory_sha256': INVENTORY_SHA,
               'package_files': 55, 'auxiliary_files': 3, 'source_before_after_match': True,
               'files': inventory['files'] | inventory['auxiliary_files'], 'delta_from_go': replay['delta_from_go'],
               'delta_from_construction': replay['delta_from_construction']},
    'prior_go': {'path': str(GO / 'correctness/review.json'), 'sha256': hashfile(GO / 'correctness/review.json'),
                 'closures_retained': True, 'finding_closure': closures},
    'findings': [finding],
    'checks': {'constructor_probe_groups': probes['groups'], 'expected_outcomes': probes['groups'] - len(probes['unexpected']),
               'unexpected_accepts': len(probes['unexpected']), 'probe_results': 'probes.json',
               'image_tests': {'count': 31, 'status': 'PASS', 'log': 'image-tests.log', 'resource_warnings': 'error'},
               'full_local_reports': replay['tests'], 'full_287_suite_rerun': False,
               'retained_artifact_replay': replay['status'], 'images': replay['images'],
               'all_six_content_records_byte_identical': True,
               'construction_inputs_unchanged': replay['construction_inputs_unchanged'],
               'driver_and_auxiliary_unchanged': replay['driver_and_auxiliary_unchanged'],
               'prior_overlay_result_retained_by_identical_archive_hashes': True, 'independent_overlay_recomputed_this_round': False,
               'engine_commands_executed': 0, 'mounted_root_helper_executed': False, 'hosted_image_ci_executed': False},
    'artifact_revalidation': {'path': str(BASE / 'artifact-revalidation.json'), 'sha256': REVALIDATION_SHA,
                              'independently_verified_for_retained_images': True, 'rebuild_required_for_reviewed_delta': False},
    'preservation': {'all_original_inputs_unchanged': True, 'protected_file_count': sum(map(len, before['protected_roots'].values())),
                     'protected_roots': {p: len(v) for p, v in before['protected_roots'].items()},
                     'before_manifest_sha256': hashfile(OUT / 'inventory-before.json'), 'after_manifest_sha256': hashfile(OUT / 'inventory-after.json'),
                     'unreviewed_parent_additions': after.get('unreviewed_parent_additions', {}), 'whole_git_status_compared': False},
    'limits': [
        'Writes only to the new completion correctness directory. No previous output or script modified/executed.',
        'No engine/helper operation, image build/pull/load/start, mounts/unmounts, C0 actor/collector/worker-main execution, tracing, installed filters, deployment or runtime calibration.',
        'Archive data read as streams; no filesystem extraction. Reproducers use inert ELF bytes and legitimate root metadata.',
        'Existing image/root/cleanup observations are historical evidence, not new live-engine/native-host observations.',
        'The 31 image tests and 36 independent constructor groups ran; source-bound full 287-test reports were verified rather than rerun.',
        'Actual archives are byte-identical to prior GO evidence; their previous independent overlay relation is carried forward, not recomputed in this small-delta confirmation.',
        'Native hosted image CI and native/runtime acceptance remain unclaimed. C0-ARCH-05 admission/calibration and C0-ARCH-06 publication remain separately gated.'
    ],
    'next_steps': 'Bind the constructor check to each actual stored hardlink row, add parent-symlink shadowing/missing-target regressions, and repeat the bounded confirmation. No image rebuild or runtime operation is requested.',
    'review_markdown_sha256': hashfile(OUT / 'review.md'),
}
record['support_hashes'] = {p.name: hashfile(p) for p in sorted(OUT.iterdir()) if p.is_file() and p.name not in ('review.json', 'report-hashes.txt')}
record['new_fixture_hashes'] = files(OUT / 'inert-fixtures')
(OUT / 'review.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
print('review.md', hashfile(OUT / 'review.md'))
print('review.json', hashfile(OUT / 'review.json'))
