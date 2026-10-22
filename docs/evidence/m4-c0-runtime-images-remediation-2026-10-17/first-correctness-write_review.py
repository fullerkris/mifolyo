"""Serialize the re-review; never change original review or implementation files."""
import json
from review_common import OUT, BASE, OLD, files, hashfile, load

inventory = load(BASE / 'source-inventory.json')
integrity = load(OUT / 'inventory-after.json')
actual = load(OUT / 'actual-verification.json')
probes = load(OUT / 'closure-probes.json')
sources = inventory['files'] | inventory['auxiliary_files']

closures = [
    {'id': 'C0ARCH04-COR-001', 'status': 'CLOSED', 'original_reproducers': 'PASS',
     'reason': 'Podman-only constructor/prepare/CLI and workflow; Docker refuses without engine access/output creation; strict empty native Env and labels remain; rootful preflight fails before pull.',
     'locations': ['tests/crawl-jobs-v2-observer/image_build.py:81-86,156-181,235-239', '.github/workflows/c0-runtime-images.yml:47-60'],
     'limits': 'No hosted workflow or new Podman build run by this reviewer.'},
    {'id': 'C0ARCH04-COR-002', 'status': 'CLOSED', 'original_reproducers': 'PASS',
     'reason': 'The original x64 test passes with inert ARM sys.modules contamination; private synthetic extension inventory still uses real validation; positive/foreign tests cover both ABIs before output creation.',
     'locations': ['tests/crawl-jobs-v2-observer/build_oracle.py:26-34,49-51', 'tests/crawl-jobs-v2-observer/test_x64_runtime.py:181-203'],
     'limits': 'ARM fixture simulation, not an actual native ARM-host test run.'},
    {'id': 'C0ARCH04-COR-003', 'status': 'CLOSED', 'original_reproducers': 'PASS',
     'reason': 'Original positive portable fixture accepts and both original normalized config/layer collisions reject with IMAGE_SAVE_DUPLICATE_PATH.',
     'locations': ['tests/crawl-jobs-v2-observer/image_artifacts.py:321-329'],
     'limits': 'No extraction/load; pure archive read checks.'},
    {'id': 'C0ARCH04-COR-004', 'status': 'PARTIALLY_CLOSED', 'original_reproducers': 'PASS',
     'reason': 'All original permission cases are fixed with valid roots/positive controls, but required PT_INTERP edges still lack execute-permission checks.',
     'locations': ['tests/crawl-jobs-v2-observer/image_artifacts.py:155-167,253-272,299'],
     'remaining_probe_ids': ['004-pt-interp-mode-0o644'],
     'actual_tree_reproductions': actual['adjacent_permission_probes']},
    {'id': 'C0ARCH04-COR-005', 'status': 'CLOSED_WITH_DOCUMENTED_MODEL_LIMITS', 'original_reproducers': 'PASS',
     'reason': 'RUNPATH overrides RPATH; required active legacy RPATH explicitly refuses; directory-link candidates resolve; malformed and distinct ambiguous candidates refuse; actual required graphs pass.',
     'locations': ['tests/crawl-jobs-v2-observer/image_artifacts.py:220-224,234-273'],
     'limits': 'Deliberately restricted pinned-base search model, not full ld.so runtime emulation.'},
]

remaining = [
    {'id': 'C0ARCH04-COR-004', 'status': 'PARTIALLY_CLOSED', 'priority': 'P2', 'marker': 'suggestion',
     'prevents_complete_original_finding_closure': True,
     'title': 'Required PT_INTERP loaders still need not be executable by UID/GID 999',
     'location': 'tests/crawl-jobs-v2-observer/image_artifacts.py:253-272',
     'why': 'Interpreter dependencies are queued as ordinary file paths and checked only with file(executable=False). Linux opens PT_INTERP with open_exec/MAY_EXEC.',
     'reproducer': 'probe_closures.py: 004-pt-interp-mode-0o755 positive versus 004-pt-interp-mode-0o644 negative; verify_actual.py repeats mode-only in-memory changes on both real oracle loader rows.',
     'observed': 'All three non-executable-loader cases accept; both real-tree changes preserve the payload digest.',
     'suggestion': 'Check required PT_INTERP paths with executable=True while preserving alias traversal checks; do not require execute bits for ordinary shared libraries.',
     'actual_artifacts_affected': False, 'actual_artifact_note': 'Both retained loaders are mode 0755 and pass independent execute checks.',
     'evidence': ['closure-probes.json', 'actual-verification.json', 'linux-v6.18-binfmt_elf.c:907', 'linux-v6.18-exec.c:764-775']},
    {'id': 'C0ARCH04-COR-006', 'status': 'OPEN', 'priority': 'P2', 'marker': 'suggestion',
     'title': 'Normalized root aliases bypass duplicate and observed-root checks',
     'location': 'tests/crawl-jobs-v2-observer/image_artifacts.py:89-98',
     'why': 'Raw-root recognition happens before normalization. ./., .//. and ././ become entries[/] instead of self.root, bypassing root_seen/IMAGE_ROOT_CHANGED and being ignored by directory_access(/).',
     'reproducer': 'probe_closures.py: valid single root positive; exact duplicate refusal; three normalized duplicate negatives; root-alias-conflicts-observed-root.',
     'observed': 'Four conflicting-root cases pass full native content verification; root_directory remains 0755 despite a normalized root header declaring 0700.',
     'suggestion': 'Normalize before root recognition and apply shared root-count/deduplication/observation checks, or reject noncanonical root aliases. Disallow a separate entries[/] row.',
     'actual_artifacts_affected': False, 'actual_artifact_note': 'The supplied flat exports have no conflicting root headers and use matching external root observations.',
     'evidence': ['closure-probes.json', 'inert-fixtures/root-alias-duplicate-0.tar', 'inert-fixtures/root-alias-disagrees-with-observation.tar']},
]
for row in closures:
    row['source_hashes'] = {p.split(':', 1)[0]: sources[p.split(':', 1)[0]] for p in row['locations']}
for row in remaining:
    p = row['location'].split(':', 1)[0]
    row['source_sha256'] = sources[p]

record = {
    'kind': 'c0_arch_04_correctness_remediation_rereview', 'version': 1, 'date': '2026-10-17',
    'verdict': 'NEEDS_REMEDIATION', 'execution_authorized': False, 'runtime_acceptance': False,
    'decision_reason': 'All original concrete reproducers are fixed, but COR-004 remains incomplete for required ELF interpreter execute access; a new normalized-root-header gap is also reproduced. The all-original-findings-closed condition is not met.',
    'source_identity': {'head': integrity['head'], 'branch': integrity['branch'], 'inventory_sha256': integrity['inventory_sha256'],
                        'package_files': 55, 'auxiliary_files': 3, 'before_after_match': True,
                        'snapshot_inventory_exact': True, 'package_inventory_exact': True, 'whole_git_status_compared': False},
    'original_review_preservation': {'path': str(OLD), 'unchanged': True, 'files': integrity['original_review_file_count'],
                                     'review_md_sha256': hashfile(OLD / 'review.md'), 'review_json_sha256': hashfile(OLD / 'review.json'),
                                     'all_file_hashes': integrity['original_review_files']},
    'finding_closure': closures, 'remaining_findings': remaining,
    'actual_artifact_status': actual['status'], 'actual_images': actual['images'],
    'checks': {'actual_archives': 6, 'native_historical_artifacts_identical_per_architecture': 11,
               'portable_overlay_difference_count': 0, 'command_log_records_per_architecture': 61,
               'stopped_cleanup_records_per_architecture': 5, 'root_observations_per_architecture': 6,
               'actual_required_pt_interp_execute_bits': 'PASS',
               'existing_image_tests_executed': {'count': 27, 'status': 'PASS', 'log': 'image-tests.log', 'resource_warnings': 'error'},
               'independent_closure_groups': {'count': len(probes['results']), 'expected_behavior_matched': len(probes['results']) - len(probes['unexpected_results']),
                                            'mismatches': probes['unexpected_results'], 'root_metadata_for_headerless_fixtures': probes['root_metadata_for_headerless_fixtures']},
               'actual_tree_in_memory_permission_probes': actual['adjacent_permission_probes'],
               'fake_root_helper_paths': ['success', 'mount failure', 'stat failure', 'unmount failure'],
               'test_continuity': actual['test_continuity'], 'full_283_suite_rerun': False,
               'ci': {'selected_engine': 'rootless podman', 'native_matrix': ['aarch64/ubuntu-24.04-arm', 'x86_64/ubuntu-24.04'],
                      'action_pins_unchanged_from_original_review': True, 'hosted_run': False, 'review': 'static code/docs and fake-engine checks'},
               'evidence_files_rechecked_unchanged': integrity['evidence_files_unchanged']},
    'source_inventory_files': sources, 'input_evidence_hashes': actual['evidence_hashes'],
    'public_references': load(OUT / 'references.json'),
    'limits': [
        'All writes restricted to the new remediation correctness/ directory. All 49 original correctness files preserved exactly.',
        'No repository/snapshot/image archive edits, commits/pushes, image build/pull/create/start/load, engine inspection/mutation or real mount/unmount were performed.',
        'No real C0 worker/main/collector, tracing, installed filter, live SSH or runtime calibration executed.',
        'Root helper source was executed only with fake subprocess/stat objects; no real engine or namespace command was called.',
        'Actual source/config/content/archive/root/cleanup facts were joined within retained evidence, not freshly observed on an engine.',
        'Portable overlays compare file-content hashes, kinds, UID/GID, modes and raw image-local link targets, not timestamps/xattrs; root metadata is externally observed because the flat archives omit root headers.',
        '27 image tests and targeted synthetic manifest checks ran; provided full 283-test reports/272-ID continuity were verified rather than rerun.',
        'ARM contamination and ABI tests use inert files and real validation; native ARM-host CI was not run. Local builder evidence identifies Podman 5.8.7 linux/amd64.',
        'Library loader execution, symbol versions, lazy imports, confinement/hardware/timing acceptance remain C0-ARCH-05; registry publication remains C0-ARCH-06.'
    ],
    'next_steps': 'Finish COR-004 PT_INTERP execute access and COR-006 normalized-root handling, add paired positive/negative regressions, freeze a new source inventory and repeat bounded review.',
    'review_markdown_sha256': hashfile(OUT / 'review.md'),
}
record['support_hashes'] = {p.name: hashfile(p) for p in sorted(OUT.iterdir()) if p.is_file() and p.name not in ('review.json', 'report-hashes.txt')}
record['new_fixture_hashes'] = files(OUT / 'inert-fixtures')
(OUT / 'review.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
print('review.md', hashfile(OUT / 'review.md'))
print('review.json', hashfile(OUT / 'review.json'))
