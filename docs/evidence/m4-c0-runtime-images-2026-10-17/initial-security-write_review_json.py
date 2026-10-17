"""Package already-produced read-only review evidence; writes only new reports."""
import hashlib
import json
from pathlib import Path

ROOT = Path('/tmp/opencode/c0-runtime-images-2026-10-17')
OUT = ROOT / 'security'

def digest(path):
    with path.open('rb') as file:
        return hashlib.file_digest(file, 'sha256').hexdigest()

inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())
after = json.loads((OUT / 'inventory-after.json').read_bytes())
retained = json.loads((OUT / 'retained-unchanged-after.json').read_bytes())
tests = json.loads((OUT / 'unit-run-02/security-unit-results.json').read_bytes())
prototype = json.loads((OUT / 'remediation-prototype-results.json').read_bytes())
assert after['pass'] and retained['pass'] and prototype['pass']
assert tests['tests_run'] == 11 and len(tests['failures']) == 3 and not tests['errors'] and not tests['skipped']
scope_files = ['tests/crawl-jobs-v2-observer/' + name for name in
               ('image_artifacts.py', 'image_build.py', 'build_oracle.py')]
scope_files += list(inventory['auxiliary_files'])
all_sources = inventory['files'] | inventory['auxiliary_files']
findings = [
    {'id': 'C0-IMG-SEC-01', 'severity': 'Medium', 'cwe': 'CWE-180',
     'title': 'Portable config identity is ambiguous under normalized archive names',
     'location': 'image_artifacts.py:270-287',
     'prerequisite': 'Malformed/substituted portable archive is supplied to archive_record with an expected config identity.',
     'impact': 'Accepted archive contains distinct configurations for one canonical pathname; reported config identity authenticates only one spelling.',
     'proof': tests['observations']['C0-IMG-SEC-01'],
     'remediation_code': 'review.md C0-IMG-SEC-01; remediation_prototype.py:check_unique_portable_members',
     'remediation_applied_to_repository': False,
     'limits': 'No extraction or engine loading was performed; normalized-name consumer behavior was modeled in memory.'},
    {'id': 'C0-IMG-SEC-02', 'severity': 'Medium', 'cwe': 'CWE-59',
     'title': 'Early dot-dot collapse certifies different bytes than pathname resolution reads',
     'location': 'image_artifacts.py:51-61,90-116,243-245',
     'prerequisite': 'Malformed/substituted candidate rootfs supplies a symlink chain containing parent components.',
     'impact': 'verify_content accepts expected source hashes while the same source pathname resolves to different image-internal bytes.',
     'proof': tests['observations']['C0-IMG-SEC-02'],
     'remediation_code': 'review.md C0-IMG-SEC-02; remediation_prototype.py:absolute_link_target,resolve_components',
     'remediation_applied_to_repository': False,
     'limits': 'Benign text reads only; no worker execution, host escape, or runtime-authorization bypass demonstrated.'},
    {'id': 'C0-IMG-SEC-03', 'severity': 'Low', 'cwe': 'CWE-400',
     'title': 'Repeated root directory headers bypass archive member-count checks',
     'location': 'image_artifacts.py:72-79',
     'prerequisite': 'Ordinary tar data with repeated root-directory headers reaches ImageTree.',
     'impact': '20,002 members accepted with one reported tree entry; parser work is not covered by the stated member budget.',
     'proof': tests['observations']['C0-IMG-SEC-03'],
     'remediation_code': 'review.md C0-IMG-SEC-03; remediation_prototype.py:check_root_member_budget',
     'remediation_applied_to_repository': False,
     'limits': 'Raw-file size ceiling remains. No memory exhaustion or hidden PAX/GNU metadata exhaustion was attempted.'},
]
evidence_names = [
    'inventory-before.json', 'inventory-resumed-before.json', 'inventory-after.json',
    'retained-verification.json', 'retained-verification.log',
    'retained-unchanged-after.json', 'retained-unchanged-after.log',
    'existing-image-unit-tests.log', 'security-unit-tests-rerun.log',
    'unit-run-02/security-unit-results.json',
    'remediation-prototype-results.json', 'remediation-prototype.log',
    'test_security_review.py', 'remediation_prototype.py', 'check_remediation_prototype.py',
    'verify_inventory.py', 'check_retained.py', 'check_retained_unchanged.py',
    'write_review_json.py', 'review.md',
]
for finding in findings:
    name = 'unit-run-02/' + finding['proof']['fixture']
    assert digest(OUT / name) == finding['proof']['fixture_sha256']
    finding['proof']['fixture'] = name
    evidence_names.append(name)
result = {
    'schema_version': 1, 'date': '2026-10-17',
    'decision': 'NEEDS_REMEDIATION',
    'scope': 'Changed Python archive parsing, checksum/configuration validation, file handling and cleanup; CLI/CI static review.',
    'head': inventory['base'], 'branch': inventory['branch'],
    'inventory': {'path': str(ROOT / 'source-inventory.json'), 'sha256': digest(ROOT / 'source-inventory.json'),
                  'package_file_count': 54, 'auxiliary_file_count': 3,
                  'repository_and_frozen_before_after_match': True},
    'reviewed_source_sha256': {name: all_sources[name] for name in scope_files},
    'retained_files_unchanged': retained['files_checked'],
    'finding_counts': {'Critical': 0, 'High': 0, 'Medium': 2, 'Low': 1},
    'findings': findings,
    'tests': {'python': tests['python'], 'existing_image_unit_tests': {'run': 16, 'passed': 16},
              'new_tests': {'run': 11, 'passed': 8, 'failed_regressions': 3, 'errors': 0, 'skipped': 0},
              'new_suite_reproduced_twice': True, 'distinct_unit_tests': 27,
              'full_272_suite_rerun': False, 'detached_remediation_checks_pass': True},
    'constraints': {'repository_or_frozen_edits': False, 'retained_artifact_edits': False,
                    'engine_commands_executed': 0, 'network_calls_after_scope_narrowing': 0,
                    'earlier_preliminary_external_documentation_lookup': True,
                    'external_documentation_required_for_scoped_verdict': False,
                    'native_C_review_in_narrowed_scope': False, 'native_programs_executed': False,
                    'worker_main_or_collector_executed': False, 'archive_extraction_performed': False,
                    'benign_text_link_resolution_check_within_private_directory': True,
                    'execution_authorized': False, 'runtime_acceptance': False},
    'limits': ['No engine loading/execution or live daemon interruption test.',
               'No claim that successful retained image sets are malicious.',
               'No demonstrated host filesystem escape or external-file mutation.',
               'No exhaustive parser fuzzing, OOM exercise, hardlink-semantics proof, or live loader verification.',
               'No native C, runtime/hardware/timing, registry publishing or deployment acceptance.',
               'Only detached prototype remediation was tested; frozen implementation remains unchanged.'],
    'evidence_sha256': {name: digest(OUT / name) for name in evidence_names},
}
with (OUT / 'review.json').open('x') as file:
    json.dump(result, file, indent=2, sort_keys=True)
    file.write('\n')
hash_lines = [digest(OUT / name) + '  ' + name for name in ('review.md', 'review.json')]
with (OUT / 'report-sha256.txt').open('x') as file:
    file.write('\n'.join(hash_lines) + '\n')
print('\n'.join(hash_lines))
