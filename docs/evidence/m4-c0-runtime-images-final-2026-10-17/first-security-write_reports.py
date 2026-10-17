import hashlib
import json
from pathlib import Path

ROOT = Path('/tmp/opencode/c0-runtime-images-final-2026-10-17')
OUT = ROOT / 'security'
def read(name): return json.loads((OUT / name).read_bytes())
def digest(path):
    with path.open('rb') as file:
        return hashlib.file_digest(file, 'sha256').hexdigest()

inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())
before, after = read('inputs-before.json'), read('inputs-after.json')
tests, existing = read('repeat-02/independent-tests.json'), read('existing-image-tests.json')
artifacts, candidate = read('artifact-checks.json'), read('candidate-fix-results.json')
assert before['pass'] and after['pass'] and after['before_after_unchanged'] and artifacts['pass'] and candidate['pass']
assert tests['tests_run'] == 11 and len(tests['failures']) == 1 and not tests['errors'] and not tests['skipped']
assert existing['tests_run'] == 30 and existing['pass']
prior_report_hashes = {label: {name: values[name]['sha256'] for name in ('review.md', 'review.json')}
                       for label, values in after['prior_review_outputs'].items()}
assert prior_report_hashes == {
    'initial': {'review.md': '7ad721556be8fb019fde0a74a00e306f8efbcf43ae53dafcf4b45753bd5c4ced',
                'review.json': '27e8242087612e0ada7eba888c1c3b27d5b5d00b5b5a985b11987e7b88090f5d'},
    'remediation': {'review.md': 'd0b8a56c8b489648927bfd5fba85e5906da676ba12e8107c061fb77639109783',
                    'review.json': '6fe0049309b4d0b220b90743ee858f052ec9f5db7699065772bfea75c8526095'}}
proof = dict(tests['observations']['C0-IMG-SEC-06'])
proof['fixture'] = 'repeat-02/' + proof['fixture']
proof['positive_fixture'] = 'repeat-02/' + proof['positive_fixture']
assert digest(OUT / proof['fixture']) == proof['sha256']
evidence = ['review.md', 'verify_inputs.py', 'inputs-before.json', 'inputs-after.json',
            'test_final.py', 'independent-tests.log', 'independent-tests.json',
            'repeat-02/independent-tests.json', 'independent-tests-repeat.log',
            'run_existing_tests.py', 'existing-image-tests.json', 'existing-image-tests.log',
            'check_artifact_continuity.py', 'artifact-checks.json', 'artifact-checks.log',
            'candidate-fix.patch', 'check_candidate_fix.py', 'candidate-fix-results.json',
            'candidate-fix-results.log', 'write_reports.py', proof['fixture'], proof['positive_fixture']]
all_sources = inventory['files'] | inventory['auxiliary_files']
source_names = ['tests/crawl-jobs-v2-observer/' + name for name in
                ('image_artifacts.py', 'test_image_review.py', 'image_build.py', 'build_oracle.py')]
source_names += list(inventory['auxiliary_files'])
report = {
    'schema_version': 1, 'date': '2026-10-17', 'verdict': 'NEEDS_REMEDIATION',
    'scope': 'Final bounded read-only Python archive/configuration/file-handling re-review; host-only two-file delta.',
    'no_new_blocker_established': False, 'head': inventory['base'], 'branch': inventory['branch'],
    'source_inventory_sha256': digest(ROOT / 'source-inventory.json'),
    'source_files': {'package': 55, 'auxiliary': 3}, 'exact_delta': after['delta'],
    'before_after_sources_and_inputs_unchanged': True,
    'reviewed_source_sha256': {name: all_sources[name] for name in source_names},
    'prior_outputs_preserved': {label: {'entries': len(values), 'report_sha256': prior_report_hashes[label]}
                                for label, values in after['prior_review_outputs'].items()},
    'retained_entries_unchanged': {key: len(value) for key, value in after['retained'].items()},
    'closures': {'C0-IMG-SEC-%02d' % number: {'status': 'CLOSED_FOR_REPORTED_DEFECT', 'positive_control_passed': True,
                  'evidence': tests['observations']['SEC%02d' % number]} for number in range(1, 6)},
    'COR004': {'status': 'PASS_BOUNDED_REPRODUCTION', 'checks': tests['observations']['COR004'],
               'visit_trace': tests['observations']['COR004_visit_trace'],
               'reserved_key': tests['observations']['reserved_dependency_key']},
    'findings': [{'id': 'C0-IMG-SEC-06', 'severity': 'Medium', 'cwe': 'CWE-20',
                   'title': 'Unused hardlink bypasses the aliased inode metadata invariant',
                   'location': 'image_artifacts.py:126-158,237-282,293-298',
                   'impact': 'Whole-content admission accepts conflicting metadata for a required inode and emits the exact same content record as a matching positive.',
                   'prerequisite': 'Malformed/substituted candidate archive reaches static admission; no private-output compromise or remote input route established.',
                   'proof': proof,
                   'remediation': {'code': 'candidate-fix.patch', 'description': 'Resolve all hardlinks after the full archive map has been read.',
                                   'tested_in_memory_only': True, 'applied_to_repository': False,
                                   'all_six_retained_content_records_byte_identical': True},
                   'limits': 'No extraction or image loading. Benign inode permission demonstration was confined to the private fixture directory. No runtime-authorization bypass demonstrated.'}],
    'finding_counts': {'Critical': 0, 'High': 0, 'Medium': 1, 'Low': 0},
    'tests': {'python': tests['python'], 'existing_image_tests': {'run': 30, 'passed': 30},
              'independent_tests': {'run': 11, 'passed': 10, 'exposed_defects': 1, 'errors': 0, 'skipped': 0},
              'distinct_unit_tests': 41, 'independent_defect_reproduced_twice': True,
              'full_286_suite_independently_rerun': False, 'recorded_full_suites': artifacts['recorded_suites']},
    'artifact_continuity': {'supplied_record_sha256': artifacts['continuity_record_sha256'],
                            'retained_source_inventory_sha256': artifacts['retained_source_inventory_sha256'],
                            'final_source_inventory_sha256': artifacts['final_source_inventory_sha256'],
                            'construction_inputs_unchanged': True, 'old_source_maps_not_relabelled': True,
                            'images_independently_revalidated': 6, 'recorded_commands_checked': 122,
                            'recorded_root_observations_checked': 12, 'recorded_cleanup_checks': 10,
                            'image_bytes_and_identities_unchanged': True},
    'constraints': {'network_calls': 0, 'engine_commands': 0, 'helper_executions': 0, 'image_loads_or_executions': 0,
                    'native_C_review_or_execution': False, 'worker_main_or_collector_execution': False,
                    'system_policy_changes': False, 'deployment': False, 'archive_extraction': False,
                    'repository_or_frozen_edits': False, 'benign_file_permission_changes_inside_private_fixture_only': True,
                    'execution_authorized': False, 'runtime_acceptance': False},
    'limits': ['No demonstrated host escape or out-of-directory mutation.',
               'No claim that the retained images are malicious.',
               'No exhaustive tar/POSIX proof, real extraction/loading, live engine races, or exhaustion testing.',
               'Hosted matrix, native runtime/hardware/timing and publishing remain unexecuted/out of scope.',
               'Candidate global hardlink validation is tested but not integrated into frozen sources.'],
    'evidence_sha256': {name: digest(OUT / name) for name in evidence},
}
with (OUT / 'review.json').open('x') as file:
    json.dump(report, file, sort_keys=True, indent=2)
    file.write('\n')
lines = [digest(OUT / name) + '  ' + name for name in ('review.md', 'review.json')]
with (OUT / 'report-sha256.txt').open('x') as file:
    file.write('\n'.join(lines) + '\n')
print('\n'.join(lines))
