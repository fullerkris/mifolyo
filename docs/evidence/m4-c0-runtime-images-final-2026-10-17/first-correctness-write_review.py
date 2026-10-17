"""Generate final report JSON only inside this final correctness directory."""
import json
from common import OUT, BASE, FIRST, PREVIOUS, INVENTORY_SHA, REVALIDATION_SHA, files, hashfile, load

inventory = load(BASE / 'source-inventory.json')
before = load(OUT / 'inventory-before.json')
after = load(OUT / 'inventory-after.json')
probes = load(OUT / 'probes.json')
replay = load(OUT / 'replay.json')
assert probes['status'] == replay['status'] == 'PASS' and not probes['failed']
assert after['original_inputs_unchanged'] and before['protected_roots'] == after['protected_roots']
assert before['source_files'] == after['source_files'] and after['match']

closures = [
    {'id': 'C0ARCH04-COR-001', 'status': 'CLOSED_RETAINED',
     'reason': 'Podman-only construction/CLI/CI and runtime native Env rules are unchanged; final native metadata positive/PATH-negative controls and all actual role metadata checks pass.',
     'locations': ['tests/crawl-jobs-v2-observer/image_build.py:81-86,156-181,235-239', '.github/workflows/c0-runtime-images.yml:47-60']},
    {'id': 'C0ARCH04-COR-002', 'status': 'CLOSED_RETAINED',
     'reason': 'Manifest-builder and x64 test isolation are unchanged; current image tests pass real validator checks using private matching/foreign ABI extension fixtures.',
     'locations': ['tests/crawl-jobs-v2-observer/build_oracle.py:26-34,49-51', 'tests/crawl-jobs-v2-observer/test_x64_runtime.py:181-203']},
    {'id': 'C0ARCH04-COR-003', 'status': 'CLOSED_RETAINED',
     'reason': 'Original positive portable fixture remains accepted and both original normalized config/layer duplicates specifically refuse.',
     'locations': ['tests/crawl-jobs-v2-observer/image_artifacts.py:331-339']},
    {'id': 'C0ARCH04-COR-004', 'status': 'CLOSED',
     'reason': 'Required PT_INTERP edges preserve executable intent before visited-node dedup. Positive/read-only-library and negative loader/alias controls pass on both ABIs; both actual oracle mode-only mutations now refuse. Reserved DT_NEEDED edge collision is rejected.',
     'locations': ['tests/crawl-jobs-v2-observer/image_artifacts.py:227-229,271-282'],
     'actual_loader_controls': replay['actual_loader_negative_controls']},
    {'id': 'C0ARCH04-COR-005', 'status': 'CLOSED_WITH_DOCUMENTED_MODEL_LIMITS',
     'reason': 'Corrected RPATH/RUNPATH and directory-link behavior is preserved by original fixture replay/current tests; required active legacy RPATH remains explicitly unsupported.',
     'locations': ['tests/crawl-jobs-v2-observer/image_artifacts.py:230-234,244-283']},
    {'id': 'C0ARCH04-COR-006', 'status': 'CLOSED',
     'reason': 'Normalized root aliases share root observation/count/deduplication checks and cannot create entries[/]. Positive aliases, duplicate permutations, conflicting observations and the exact previous failing archives all produce expected results.',
     'locations': ['tests/crawl-jobs-v2-observer/image_artifacts.py:89-101']},
]
source_hashes = inventory['files'] | inventory['auxiliary_files']
for row in closures:
    row['source_hashes'] = {p.split(':', 1)[0]: source_hashes[p.split(':', 1)[0]] for p in row['locations']}
record = {
    'kind': 'c0_arch_04_final_bounded_correctness_review', 'version': 1, 'date': '2026-10-17',
    'verdict': 'GO_FOR_CONTINUED_IMPLEMENTATION', 'execution_authorized': False, 'runtime_acceptance': False,
    'summary': 'All COR-001 through COR-006 findings are closed within the static-image scope; no new blocker found in the two-file delta. All six final-validator content records exactly match retained bytes.',
    'findings': [], 'finding_closure': closures,
    'source': {'head': after['head'], 'branch': after['branch'], 'inventory_sha256': INVENTORY_SHA,
               'package_files': 55, 'auxiliary_files': 3, 'before_after_match': True, 'exact_inventory': True,
               'source_files': source_hashes, 'delta': replay['source_delta']},
    'revalidation': {'path': str(BASE / 'artifact-revalidation.json'), 'sha256': REVALIDATION_SHA,
                     'independently_verified': True, 'retained_source_inventory_sha256': inventory['prior_source_inventory_sha256'],
                     'unchanged_construction_inputs': replay['unchanged_construction_inputs'],
                     'unchanged_driver_and_auxiliary': replay['unchanged_driver_and_auxiliary'], 'rebuild_required': False},
    'checks': {'independent_probe_groups': probes['groups'], 'independent_probes_status': probes['status'],
               'image_unit_tests': {'count': 30, 'status': 'PASS', 'resource_warnings': 'error', 'log': 'image-tests.log'},
               'provided_full_tests': replay['tests'], 'full_286_suite_rerun': False,
               'actual_images': replay['actual_images'], 'actual_loader_controls': replay['actual_loader_negative_controls'],
               'content_records_byte_identical': 6, 'overlay_differences': 0,
               'durable_retention': replay['durable_retention'],
               'material_hardlink_logic': {'status': 'PASS', 'locations': ['image_artifacts.py:126-158'],
                   'positive': ['direct regular hardlinks at modes 0644 and 0755', 'ordinary symlink chain', 'symlink to direct regular hardlink'],
                   'negative': ['uid/gid/mode mismatch', 'hardlink to symlink inode', 'hardlink through symlink prefix', 'hardlink chain', 'missing target', 'directory target']},
               'engine_commands_executed_by_reviewer': 0, 'mounted_root_helper_executed': False, 'new_hosted_workflow_executed': False},
    'preservation': {'protected_file_count': sum(map(len, before['protected_roots'].values())),
                     'protected_roots': {p: len(v) for p, v in before['protected_roots'].items()}, 'all_original_files_unchanged': True,
                     'integrity_before_sha256': hashfile(OUT / 'inventory-before.json'), 'integrity_after_sha256': hashfile(OUT / 'inventory-after.json'),
                     'unreviewed_concurrent_parent_additions': after.get('unreviewed_new_parent_files', {}),
                     'baseline_replaced': False, 'whole_git_status_compared': False},
    'previous_reviews': {str(root / 'correctness'): {'review_md_sha256': hashfile(root / 'correctness/review.md'),
                                                   'review_json_sha256': hashfile(root / 'correctness/review.json')} for root in (FIRST, PREVIOUS)},
    'limits': [
        'Read-only review of repository, snapshots and prior artifacts. New output only in the final correctness directory.',
        'No builds, pulls, engine commands/mutations, image loads/starts, mounts/unmounts, mounted-root helper execution, real C0 actor/collector/worker-main calls, tracing, installed filters or deployment.',
        'Current image unit tests use inert archives, private synthetic extension inventories and fake engines; the mounted-root helper script was never executed in this final review.',
        'No previous review script was run or modified; old fixtures were only read. Archive extraction APIs were used only for data streams, never filesystem extraction.',
        'Existing root/container/cleanup records are historical evidence, not new live-engine observations.',
        'Overlay comparisons cover file hashes/kinds/UID/GID/modes/link targets, not timestamps/xattrs; root metadata comes from retained observations.',
        'The 286-test complete reports were source/hash/ID verified, not rerun; 30 current image tests and 90 independent probe groups were executed.',
        'Native hosted ARM/x64 image CI remains unexecuted. Runtime loader ABI/symbol/lazy-loading checks, confinement, hardware/timing calibration remain C0-ARCH-05; registry publication remains C0-ARCH-06.',
        'An initial overbroad parent-directory equality check noticed two newly added, unreviewed sibling utilities. Exact original inputs and protected roots remain unchanged; additions are disclosed without replacing the baseline.'
    ],
    'next_steps': 'Continue implementation/documentation with the finding closures preserved. Hosted/runtime work needs separate approval and evidence; this review grants no execution/publication authority.',
    'review_markdown_sha256': hashfile(OUT / 'review.md'),
}
record['support_hashes'] = {p.name: hashfile(p) for p in sorted(OUT.iterdir()) if p.is_file() and p.name not in ('review.json', 'report-hashes.txt')}
record['new_fixture_hashes'] = files(OUT / 'inert-fixtures')
(OUT / 'review.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
print('review.md', hashfile(OUT / 'review.md'))
print('review.json', hashfile(OUT / 'review.json'))
