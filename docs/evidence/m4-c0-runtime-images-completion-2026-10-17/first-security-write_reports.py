import hashlib
import json
from pathlib import Path

ROOT = Path('/tmp/opencode/c0-runtime-images-completion-2026-10-17')
OUT = ROOT / 'security'
def read(name): return json.loads((OUT / name).read_bytes())
def digest(path):
    with path.open('rb') as file: return hashlib.file_digest(file, 'sha256').hexdigest()

inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())
before, after = read('inputs-before.json'), read('inputs-after.json')
tests, existing = read('repeat-02/independent-tests.json'), read('existing-image-tests.json')
artifacts, candidate = read('artifact-checks.json'), read('candidate-fix-results.json')
assert before['pass'] and after['pass'] and after['before_after_unchanged'] and artifacts['pass'] and candidate['pass']
assert tests['tests_run'] == 11 and len(tests['failures']) == 1 and not tests['errors'] and not tests['skipped']
assert existing['tests_run'] == 31 and existing['pass']
old_reports = {label: {name: values[name]['sha256'] for name in ('review.md', 'review.json')} for label, values in after['prior_outputs'].items()}
assert old_reports['final'] == {'review.md': '80318947a8b181289b3d844abeb47eafbd491665a3e8ea15b84c3eac83480d65',
                                'review.json': '6ce9057ac233f69e36d66177123581b82d944596e5c381b63566d8ba7fa47485'}
proof = dict(tests['observations']['SEC06_parent_redirect'])
for name in ('fixture', 'matching_metadata_fixture', 'literal_directory_parent_positive'): proof[name] = 'repeat-02/' + proof[name]
assert digest(OUT / proof['fixture']) == proof['sha256']
source_map = inventory['files'] | inventory['auxiliary_files']
source_names = ['tests/crawl-jobs-v2-observer/' + name for name in ('image_artifacts.py', 'test_image_review.py', 'image_build.py', 'build_oracle.py')]
source_names += list(inventory['auxiliary_files'])
evidence = ['review.md', 'verify_inputs.py', 'inputs-before.json', 'inputs-after.json', 'test_completion.py',
            'independent-tests.json', 'independent-tests.log', 'independent-tests-repeat.log', 'repeat-02/independent-tests.json',
            'run_existing_tests.py', 'existing-image-tests.json', 'existing-image-tests.log',
            'check_artifacts.py', 'artifact-checks.json', 'artifact-checks.log',
            'candidate-fix.patch', 'check_candidate_fix.py', 'candidate-fix-results.json', 'candidate-fix-results.log',
            'write_reports.py', proof['fixture'], proof['matching_metadata_fixture'], proof['literal_directory_parent_positive'],
            'repeat-02/fixtures/direct-conflict-control.tar', 'repeat-02/fixtures/forward-directory.tar']
report = {
    'schema_version': 1, 'date': '2026-10-17', 'verdict': 'NEEDS_REMEDIATION',
    'scope': 'Bounded read-only SEC-06 constructor closure review; Python archives and inert fixtures only.',
    'head': inventory['base'], 'branch': inventory['branch'],
    'source_inventory_sha256': digest(ROOT / 'source-inventory.json'),
    'source_file_counts': {'package': 55, 'auxiliary': 3}, 'source_and_input_preservation_verified': True,
    'validator_delta_is_only_new_global_alias_pass': after['validator_change_is_only_global_alias_pass'],
    'delta_from_previous_review': after['delta_from_last_review'], 'delta_from_actual_build': after['delta_from_build'],
    'reviewed_source_sha256': {name: source_map[name] for name in source_names},
    'prior_outputs_preserved': {label: {'entries': len(values), 'report_sha256': old_reports[label]} for label, values in after['prior_outputs'].items()},
    'retained_image_set_entries_unchanged': {key: len(value) for key, value in after['retained'].items()},
    'closures': {'C0-IMG-SEC-%02d' % number: {'status': 'CLOSED_FOR_REPORTED_DEFECT', 'evidence': tests['observations']['SEC%02d' % number]} for number in range(1, 6)},
    'SEC06': {'status': 'PARTIALLY_REMEDIATED_REMAINS_OPEN',
              'original_reproducer': tests['observations']['SEC06_original'],
              'both_member_order_matrix': tests['observations']['SEC06_member_order_matrix'],
              'forward_target_positive': tests['observations']['forward_directory'],
              'unused_target_refusals': tests['observations']['unused_target_refusals']},
    'COR004': tests['observations']['COR004'],
    'findings': [{'id': 'C0-IMG-SEC-06', 'severity': 'Medium', 'status': 'OPEN_REMAINING_VARIANT',
                   'title': 'Global hardlink validation can follow a parent alias and check a different entry',
                   'cwe': ['CWE-20', 'CWE-59'], 'location': 'image_artifacts.py:125-129,131-163',
                   'prerequisite': 'Malformed or substituted archive with a hardlink member beneath a symlinked parent reaches static admission.',
                   'impact': 'Required-inode metadata inconsistency is not checked; whole-content admission returns the same record as matching metadata.',
                   'proof': proof,
                   'remediation': {'code': 'candidate-fix.patch', 'description': 'Require the hardlink member parent to resolve to itself before resolving the member name.',
                                   'tested_in_memory': True, 'applied_to_sources': False,
                                   'direct_literal_parent_and_forward_positives_pass': True,
                                   'all_six_retained_content_records_unchanged': True},
                   'limits': 'No extraction or live loader behavior tested; no private-output compromise, remote route, host escape or execution-authorization bypass established.'}],
    'finding_counts': {'Critical': 0, 'High': 0, 'Medium': 1, 'Low': 0},
    'tests': {'python': tests['python'], 'repository_image_tests': {'run': 31, 'passed': 31},
              'independent_tests': {'run': 11, 'passed': 10, 'defect_failures': 1, 'errors': 0, 'skipped': 0},
              'distinct_unit_tests': 42, 'remaining_variant_reproduced_twice': True,
              'full_287_suite_independently_rerun': False, 'recorded_full_suite_checks': artifacts['recorded_suites']},
    'artifact_continuity': {'supplied_record_sha256': artifacts['continuity_sha256'],
                            'current_inventory_sha256': artifacts['current_inventory_sha256'],
                            'historical_inventory_sha256': artifacts['historical_inventory_sha256'],
                            'construction_inputs_unchanged': True, 'old_full_source_maps_not_relabelled': True,
                            'images_revalidated': 6, 'content_records_byte_identical': True,
                            'logs_rehashed': 122, 'root_observations_rejoined': 12, 'unchanged_cleanup_records': 10},
    'constraints': {'network_calls': 0, 'engine_commands': 0, 'helper_executions': 0,
                    'container_image_loading_or_execution': False, 'native_review_or_execution': False,
                    'worker_main_or_collector_execution': False, 'system_policy_changes': False, 'deployment': False,
                    'archive_extraction': False, 'repository_or_frozen_source_edits': False,
                    'execution_authorized': False, 'runtime_acceptance': False},
    'limits': ['No demonstrated out-of-directory mutation or host escape.',
               'No allegation that retained images contain the malformed fixture pattern.',
               'No live helper/engine races, extraction/loading, exhaustive archive proof or exhaustion testing.',
               'Hosted image CI, native runtime/hardware/timing and publishing remain separate unexecuted gates.',
               'Previous global-pass proposal was insufficient for parent-alias name shadowing; additional guard is tested but not applied.'],
    'evidence_sha256': {name: digest(OUT / name) for name in evidence},
}
with (OUT / 'review.json').open('x') as file:
    json.dump(report, file, sort_keys=True, indent=2); file.write('\n')
lines = [digest(OUT / name) + '  ' + name for name in ('review.md', 'review.json')]
with (OUT / 'report-sha256.txt').open('x') as file: file.write('\n'.join(lines) + '\n')
print('\n'.join(lines))
