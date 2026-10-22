import hashlib
import json
from pathlib import Path

ROOT = Path('/tmp/opencode/c0-runtime-images-closure-2026-10-17')
OUT = ROOT / 'security'
def read(name): return json.loads((OUT / name).read_bytes())
def digest(path):
    with path.open('rb') as file: return hashlib.file_digest(file, 'sha256').hexdigest()

inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())
before, after = read('inputs-before.json'), read('inputs-after.json')
tests, existing = read('repeat-02/independent-tests.json'), read('existing-image-tests.json')
first_tests, artifacts = read('independent-tests.json'), read('artifact-checks.json')
assert before['pass'] and after['pass'] and after['before_after_unchanged'] and artifacts['pass']
assert tests['tests_run'] == first_tests['tests_run'] == 11 and tests['pass'] and first_tests['pass']
assert not tests['failures'] and not tests['errors'] and not tests['skipped']
assert existing['tests_run'] == 32 and existing['pass']
old_reports = {label: {name: values[name]['sha256'] for name in ('review.md', 'review.json')}
               for label, values in after['prior_outputs'].items()}
assert old_reports['completion'] == {'review.md': '9bff16a9a8fc05ca834406f4be10e051ac9adb78d47f9fa9b673e538f89af16d',
                                     'review.json': 'd9906dd6cb2a30f640d5a3acd18a1916500f9c7539dc6ee57ff6b5bc69c1aee9'}
assert tests['observations']['SEC06_parent_record']['refusals']['parent-redirect-conflict.tar']['sha256'] == 'bff3872ac94c209349e1fcb237d4e93bed9f774e682282309009da0cc0c6e600'
source_map = inventory['files'] | inventory['auxiliary_files']
source_names = ['tests/crawl-jobs-v2-observer/' + name for name in ('image_artifacts.py', 'test_image_review.py', 'image_build.py', 'build_oracle.py')]
source_names += list(inventory['auxiliary_files'])
evidence = ['review.md', 'verify_inputs.py', 'inputs-before.json', 'inputs-after.json', 'test_closure.py',
            'independent-tests.json', 'independent-tests.log', 'repeat-02/independent-tests.json', 'independent-tests-repeat.log',
            'run_existing_tests.py', 'existing-image-tests.json', 'existing-image-tests.log',
            'check_artifacts.py', 'artifact-checks.json', 'artifact-checks.log', 'write_reports.py']
evidence += [path.relative_to(OUT).as_posix() for path in sorted((OUT / 'repeat-02/fixtures').glob('*.tar'))]
closures = {'C0-IMG-SEC-%02d' % number: {'status': 'CLOSED', 'positive_controls_passed': True,
              'historical_severity': 'Low' if number == 3 else 'Medium',
              'evidence': tests['observations']['SEC%02d' % number]} for number in range(1, 6)}
closures['C0-IMG-SEC-06'] = {'status': 'CLOSED', 'historical_severity': 'Medium', 'positive_controls_passed': True,
    'parent_record_reproducer': tests['observations']['SEC06_parent_record'],
    'inode_metadata_reproducer': tests['observations']['SEC06_inode_metadata'],
    'each_declared_record_checked': tests['observations']['all_hardlink_records'],
    'all_parent_entry_kinds': tests['observations']['all_entry_parent_kinds'],
    'ancestor_chain_checks': tests['observations']['ancestor_induction']}
report = {
    'schema_version': 1, 'date': '2026-10-17', 'verdict': 'GO_FOR_CONTINUED_IMPLEMENTATION',
    'scope': 'Inventory-bound Python static-admission constructor and prior SEC-01..06 closure checks only.',
    'no_unresolved_blocker_found_in_reviewed_delta': True,
    'head': inventory['base'], 'branch': inventory['branch'],
    'source_inventory_sha256': digest(ROOT / 'source-inventory.json'),
    'source_file_counts': {'package': 55, 'auxiliary': 3}, 'source_and_input_preservation_verified': True,
    'validator_delta_is_exact_parent_pass': after['validator_delta_is_exact_parent_pass'],
    'delta_from_previous_review': after['delta_from_previous_review'], 'delta_from_actual_build': after['delta_from_build'],
    'reviewed_source_sha256': {name: source_map[name] for name in source_names},
    'prior_outputs_preserved': {label: {'entries': len(values), 'report_sha256': old_reports[label]}
                                for label, values in after['prior_outputs'].items()},
    'retained_image_set_entries_unchanged': {key: len(value) for key, value in after['retained'].items()},
    'closed_findings': closures, 'COR004': tests['observations']['COR004'],
    'open_findings': [], 'open_finding_counts': {'Critical': 0, 'High': 0, 'Medium': 0, 'Low': 0},
    'constructor_assessment': {
        'parent_validation_precedes_all_hardlink_lookups': True,
        'every_entry_including_directories_is_checked': True,
        'canonical_parent_chains_strictly_shorten_to_root': True,
        'no_link_or_missing_parent_can_redirect_declared_member_lookup': True,
        'full_map_before_validation_preserves_forward_declarations': True,
        'integrated_control_location': 'image_artifacts.py:125-134',
        'scope_limit': 'This is the flattened in-memory archive-map invariant, not a complete tar/POSIX/loader proof.'},
    'tests': {'python': tests['python'], 'repository_image_tests': {'run': 32, 'passed': 32},
              'independent_tests': {'run': 11, 'passed': 11, 'failures': 0, 'errors': 0, 'skipped': 0},
              'distinct_targeted_unit_tests': 43, 'independent_suite_passed_twice': True,
              'full_288_suite_independently_rerun': False, 'recorded_full_suite_checks': artifacts['recorded_suites']},
    'artifact_continuity': {'supplied_record_sha256': artifacts['continuity_sha256'],
                            'current_inventory_sha256': artifacts['current_inventory_sha256'],
                            'historical_inventory_sha256': artifacts['historical_inventory_sha256'],
                            'construction_inputs_unchanged': True, 'historical_full_source_maps_not_relabelled': True,
                            'images_revalidated': 6, 'content_records_byte_identical': True,
                            'recorded_commands_verified': 122, 'root_observations_rejoined': 12,
                            'cleanup_records_verified': 10, 'engine_or_helper_executions': 0},
    'constraints': {'network_calls': 0, 'engine_commands': 0, 'helper_executions': 0,
                    'container_image_loading_or_execution': False, 'unchanged_native_review_or_execution': False,
                    'worker_main_or_collector_execution': False, 'system_policy_changes': False, 'deployment': False,
                    'archive_extraction': False, 'repository_or_frozen_source_edits': False},
    'authority': {'continued_implementation_static_review_only': True, 'execution_authorized': False,
                  'runtime_acceptance': False, 'publishing_authorized': False, 'hosted_image_CI_executed': False},
    'limits': ['No exhaustive archive fuzzing, resource-exhaustion exercise or complete tar/POSIX/ELF/loader proof.',
               'No actual image loading, live engine/helper races, or new cleanup observation.',
               'No native runtime/hardware/timing, hosted image matrix, registry publishing or deployment acceptance.',
               'GO applies only to the specified inventory-bound static-review scope; C0-ARCH-05/06 remain separate gates.'],
    'evidence_sha256': {name: digest(OUT / name) for name in evidence},
}
with (OUT / 'review.json').open('x') as file:
    json.dump(report, file, sort_keys=True, indent=2); file.write('\n')
lines = [digest(OUT / name) + '  ' + name for name in ('review.md', 'review.json')]
with (OUT / 'report-sha256.txt').open('x') as file: file.write('\n'.join(lines) + '\n')
print('\n'.join(lines))
