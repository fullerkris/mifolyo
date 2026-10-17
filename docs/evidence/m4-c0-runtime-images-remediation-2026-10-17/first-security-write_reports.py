import hashlib
import json
from pathlib import Path

ROOT = Path('/tmp/opencode/c0-runtime-images-remediation-2026-10-17')
OUT = ROOT / 'security'
def digest(path):
    with path.open('rb') as file:
        return hashlib.file_digest(file, 'sha256').hexdigest()
def read(name): return json.loads((OUT / name).read_bytes())

inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())
before, after = read('inputs-before.json'), read('inputs-after.json')
independent = read('repeat-02/independent-tests.json')
existing = read('existing-image-tests.json')
sets, candidate = read('corrected-set-checks.json'), read('candidate-fix-results.json')
assert before['pass'] and after['pass'] and after['before_after_unchanged'] and sets['pass'] and candidate['pass']
assert independent['tests_run'] == 10 and len(independent['failures']) == 2 and not independent['errors'] and not independent['skipped']
assert existing['tests_run'] == 27 and existing['pass']
old_hashes = {name: after['original_security_outputs'][name]['sha256'] for name in ('review.md', 'review.json')}
assert old_hashes == {'review.md': '7ad721556be8fb019fde0a74a00e306f8efbcf43ae53dafcf4b45753bd5c4ced',
                      'review.json': '27e8242087612e0ada7eba888c1c3b27d5b5d00b5b5a985b11987e7b88090f5d'}
source_names = ['tests/crawl-jobs-v2-observer/' + name for name in
                ('image_artifacts.py', 'image_build.py', 'build_oracle.py', 'test_image_artifacts.py', 'test_image_review.py')]
source_names += list(inventory['auxiliary_files'])
all_sources = inventory['files'] | inventory['auxiliary_files']
evidence_names = ['review.md', 'inputs-before.json', 'inputs-after.json', 'verify_inputs.py',
                  'test_rereview.py', 'run_existing_tests.py', 'existing-image-tests.json', 'existing-image-tests.log',
                  'independent-tests.json', 'independent-tests.log', 'independent-tests-repeat.log',
                  'repeat-02/independent-tests.json', 'check_corrected_sets.py', 'corrected-set-checks.json',
                  'corrected-set-checks.log', 'corrected-set-checks-pass.log',
                  'candidate-fixes.patch', 'check_candidate_fixes.py', 'candidate-fix-results.json',
                  'candidate-fix-results.log', 'write_reports.py']
for name in ('hardlink-to-symlink.tar', 'ordinary-hardlink.tar', 'ordinary-link.tar',
             'root-alias-positive.tar', 'root-alias-conflict.tar', 'root-alias-duplicate.tar',
             'member-limit-positive.tar', 'member-limit-negative.tar'):
    evidence_names.append('repeat-02/fixtures/' + name)
closures = {}
for number in (1, 2, 3):
    key = 'SEC%02d' % number
    closures['C0-IMG-SEC-%02d' % number] = {
        'status': 'CLOSED_FOR_ORIGINAL_REPRODUCED_DEFECT',
        'positive_control_passed': True, 'evidence': independent['observations'][key]}
findings = []
for key, title, location, cwe, impact in (
    ('C0-IMG-SEC-04', 'Hardlink to relative symlink binds source bytes at the wrong parent',
     'image_artifacts.py:108-113,136-149,294-297', 'CWE-59',
     'Content admission authenticates different image-internal source bytes than a real read of the aliased symlink inode.'),
    ('C0-IMG-SEC-05', 'Normalized root aliases bypass root observation and duplicate checks',
     'image_artifacts.py:89-97,155-158,305-306', 'CWE-180',
     'Content admission succeeds despite conflicting root header metadata, and permits two canonical root representations.')):
    proof = dict(independent['observations'][key])
    proof['fixture'] = 'repeat-02/' + proof['fixture']
    if 'duplicate_fixture' in proof: proof['duplicate_fixture'] = 'repeat-02/' + proof['duplicate_fixture']
    if 'positive_fixture' in proof: proof['positive_fixture'] = 'repeat-02/' + proof['positive_fixture']
    assert digest(OUT / proof['fixture']) == proof['sha256']
    findings.append({'id': key, 'severity': 'Medium', 'classification_scope': 'static admission integrity',
                     'title': title, 'location': location, 'cwe': cwe, 'impact': impact,
                     'prerequisite': 'Malformed or substituted candidate archive reaches the parser; no remote input path or private-output compromise established.',
                     'proof': proof, 'positive_controls_passed': True,
                     'remediation': {'code': 'candidate-fixes.patch', 'explanation': 'review.md section ' + key,
                                     'tested_in_memory_only': True, 'applied_to_repository': False,
                                     'all_six_retained_content_records_unchanged_with_guards': True}})
report = {
    'schema_version': 1, 'date': '2026-10-17', 'verdict': 'NEEDS_REMEDIATION',
    'scope': 'Read-only re-review of corrected Python archive/configuration/file-handling/cleanup logic and CLI/CI wrappers.',
    'no_new_blocker_established': False, 'execution_authorized': False, 'runtime_acceptance': False,
    'head': inventory['base'], 'branch': inventory['branch'],
    'inventory_sha256': digest(ROOT / 'source-inventory.json'),
    'inventory_files': {'package': 55, 'auxiliary': 3}, 'sources_before_after_match': True,
    'reviewed_source_sha256': {name: all_sources[name] for name in source_names},
    'original_review_outputs_preserved': {'entries': len(after['original_security_outputs']), 'report_sha256': old_hashes},
    'corrected_retained_trees_preserved': {key: len(value) for key, value in after['retained'].items()},
    'original_findings': closures, 'findings': findings,
    'finding_counts': {'Critical': 0, 'High': 0, 'Medium': 2, 'Low': 0},
    'tests': {'python': independent['python'], 'existing_image_tests': {'run': 27, 'passed': 27},
              'independent_tests': {'run': 10, 'passed': 8, 'exposed_defects': 2, 'errors': 0, 'skipped': 0},
              'independent_suite_reproduced_twice': True, 'distinct_unit_tests': 37,
              'full_283_suite_independently_rerun': False,
              'recorded_283_reports_verified': sets['reported_suites']},
    'retained_verification': {'images': 6, 'commands': 122, 'root_observations': 12, 'cleanup_records': 10,
                               'all_existing_content_metadata_archive_source_checks_pass': True,
                               'helper_sha256': sets['root_metadata_helper_sha256'],
                               'helper_executed_by_reviewer': False, 'native_artifacts_compared_as_hashes_and_bytes_only': True},
    'constraints': {'network_calls': 0, 'engine_commands_executed': 0, 'root_metadata_helper_executions': 0,
                    'native_C_review_or_execution': False, 'worker_main_or_collector_execution': False,
                    'container_or_image_mutations': False, 'system_policy_changes': False, 'deployment': False,
                    'repository_or_frozen_source_edits': False, 'archive_extraction': False,
                    'benign_text_symlink_inode_test_inside_private_output_only': True},
    'limits': ['No demonstrated host filesystem escape or external-file mutation.',
               'No claim that supplied corrected image sets are malicious.',
               'No live helper, engine loading, daemon interruption/race or hosted CI testing.',
               'No exhaustive tar/POSIX/dynamic-loader semantics proof or resource-exhaustion exercise.',
               'Native runtime/hardware/timing and registry publishing remain C0-ARCH-05/06.',
               'Candidate fixes tested in memory only; frozen implementation still contains new defects.'],
    'evidence_sha256': {name: digest(OUT / name) for name in evidence_names},
}
with (OUT / 'review.json').open('x') as file:
    json.dump(report, file, sort_keys=True, indent=2)
    file.write('\n')
lines = [digest(OUT / name) + '  ' + name for name in ('review.md', 'review.json')]
with (OUT / 'report-sha256.txt').open('x') as file:
    file.write('\n'.join(lines) + '\n')
print('\n'.join(lines))
