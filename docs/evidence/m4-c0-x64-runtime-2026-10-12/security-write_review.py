"""Assemble the machine-readable review from retained evidence; output stays here."""
import hashlib
import json
from pathlib import Path

ROOT = Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12')
OUT = ROOT/'security'
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
def load(name): return json.loads((OUT/name).read_bytes())
def digest(path): return hashlib.sha256(path.read_bytes()).hexdigest()

before, after = load('integrity-before.json'), load('integrity-after.json')
static, runtime, guards = load('static-probes.json'), load('runtime-probes.json'), load('x64-guards.json')
tests, continuity = load('tests.json'), load('continuity.json')
assert before['passed'] and after['passed'] and after['source_build_and_head_unchanged_since_before']
assert all(value['status'] == 'PASS' for value in (static,runtime,guards,tests,continuity))
assert tests['tests_run'] == 256 and guards['tests_run'] == 31
assert len(runtime['refusals']) == 313 and len(runtime['accepted']) == 24
expected = set(json.loads((ROOT/'source-inventory.json').read_bytes())['files'])
enumerations = {}
for name, directory in [('snapshot',ROOT/'snapshot'),('repository',REPO)]:
    actual = {p.relative_to(directory).as_posix() for p in (directory/'tests/crawl-jobs-v2-observer').rglob('*') if p.is_file()}
    assert actual == expected
    enumerations[name] = {'exact_package_file_inventory':True,'file_count':len(actual)}

result = {
    'schema_version':1,
    'kind':'independent_c0_arch_03_security_review',
    'date':'2026-10-12',
    'decision':'GO_FOR_CONTINUED_IMPLEMENTATION',
    'execution_authorized':False,
    'runtime_acceptance':False,
    'm4_accepted':False,
    'repository':str(REPO),
    'branch':'feature/crawl-jobs-v2-c0-x64-runtime',
    'base_and_reviewed_head':before['git_head'],
    'snapshot':str(ROOT/'snapshot'),
    'inventory':{'path':str(ROOT/'source-inventory.json'),'sha256':before['inventory_sha256'],
                 'package_files':51,'additional_runner_files':1,'verified_before_and_after':True,
                 'source_trees':enumerations,'final_build_files_verified_unchanged':len(after['build_files'])},
    'scope':'Source and static-artifact security boundaries for C0-ARCH-03 continued implementation; no instruction/witness algorithm certification.',
    'findings':[],
    'blocking_findings':[],
    'finding_counts':{'critical':0,'high':0,'medium':0,'low':0,'informational':0},
    'remediation_required':False,
    'test_results':{
        'independent_original_runner':{'status':tests['status'],'tests_run':tests['tests_run'],'umask':'077',
            'elapsed_seconds_from_log':13.670,'resource_warnings_fatal':True,'report':'tests.json','log':'tests.log'},
        'x64_guard_replay':{'status':guards['status'],'tests_run':guards['tests_run'],
            'x64_backend_instances':len(guards['backend_architectures']),'production_edits':False,
            'test_assertion_changes':False,'fixture_only_parameterization':True,'report':'x64-guards.json'},
        'compiled_x64_filter_vectors':158796,
        'oci_ptrace_vectors':static['counters']['oci_ptrace_vectors'],
        'malicious_artifact_refusals':len(static['record_probes']),
        'input_provenance_refusals':len(runtime['refusals']),
        'input_provenance_positive_controls':len(runtime['accepted']),
        'in_memory_c_debug_controls':29,
        'actual_ptrace_calls':0,
        'filter_installations':0,
        'native_actor_executions':0,
        'prior_test_ids_retained':continuity['retained_prior_test_ids'],
        'prior_test_ids_intentionally_replaced':continuity['removed_ids'],
        'unchanged_prior_test_method_asts':continuity['unchanged_retained_method_asts'],
        'changed_prior_test_method_asts':continuity['changed_retained_test_method_asts']},
    'boundaries':[
        {'id':'SEC-C0-X64-B01','status':'PASS','subject':'Native audit architecture; x32/historical compat/negative syscall aliases; post-admission exec denial',
         'source':['profiles.py:87-173'],'evidence':['static-probes.json','x64-main-disassembly.log']},
        {'id':'SEC-C0-X64-B02','status':'PASS','subject':'Full-width ptrace/TID/offset/data constraints; DR0..3/6/7 read versus DR0/reset-only DR6/DR7=0,1 writes; no GPR/memory/data-watchpoint widening',
         'source':['profiles.py:63-84','profiles.py:113-163','native/x86_debug.h:7-54'],'evidence':['static-probes.json','debug-probe.log','x64-debug-verify-disassembly.log']},
        {'id':'SEC-C0-X64-B03','status':'PASS','subject':'Actual embedded BPF and installation pointer/length versus canonical generator, sidecar and record-mode malicious mutations',
         'source':['build_tools.py:145-157','native/observer.c:120-125'],'evidence':['static-probes.json','x64-main-disassembly.log']},
        {'id':'SEC-C0-X64-B04','status':'PASS_STATIC_ONLY','subject':'LP64, native GP regset, CS/RIP/signal/debug-state readback and executable identity joins',
         'source':['native/architecture.h:3-14','native/observer.c:81-118','native/observer.c:185-199','native/observer.c:237-278','runtime_admission.py:98-147'],
         'evidence':['debug-probe.log','static-probes.json','runtime-probes.json','x64-stopped-pc-disassembly.log','x64-main-disassembly.log']},
        {'id':'SEC-C0-X64-B05','status':'PASS_OFFLINE','subject':'Image/profile/deployment/anchor/context/ELF cross-architecture substitutions; observed oracle ABI',
         'source':['docker_backend.py:197-338','runtime_context.py:47-184','context_transport.py:158-184','linux_observation.py:42-51','linux_observation.py:113-194','oracle_worker.py:149-187'],
         'evidence':['runtime-probes.json','tests.json','continuity.json']},
        {'id':'SEC-C0-X64-B06','status':'PASS_OFFLINE','subject':'Authorization, own/peer/oracle/context expiry after final polls, held-evidence joins, replay, restart and fail-closed dispatch',
         'source':['docker_backend.py:287-338','docker_backend.py:360-478','oracle_bridge.py:37-170','streams.py:128-207'],
         'evidence':['x64-guards.json','runtime-probes.json','tests.json']},
        {'id':'SEC-C0-X64-B07','status':'PASS','subject':'ARM64 three-profile/BPF bytes unchanged from merged-base generation',
         'source':['profiles.py'],'evidence':['static-probes.json','continuity.json']}],
    'artifacts':static['artifact_checks'],
    'source_bundle_digests':{'runtime_context_source':continuity['runtime_context_source_sha256'],
                            'oracle_worker_source':continuity['oracle_worker_source_sha256']},
    'limits':[
        'C0-ARCH-04 complete dual-runtime images remain unproven; Dockerfile.oracle intentionally remains ARM64.',
        'C0-ARCH-05 actual kernel/tracing/debug-register stop semantics/timing/calibration and non-emulated execution remain unproven.',
        'OCI rules interpreted offline; no OCI libseccomp/runc compilation or installed kernel-filter readback is claimed.',
        'No native actor, actual ptrace, filter installation, live Docker/Podman workload, real host collector, live SSH or host policy mutation.',
        'Oracle worker main exercised only with simulated Linux and witness readers in permitted benign-pipe tests.',
        'Only the in-memory native/test_x86_debug.c executable was compiled/run; actor build records inspected, not independently rebuilt.',
        'Operator/context source/compiler remain trust roots; fixture-key-signed counterexamples are not MAC forgery or hostile-root demonstrations.',
        'No atomic multi-artifact filesystem attestation, full SCA/CVE/SBOM audit, or separate correctness-role algorithm approval.',
        'Fresh architecture-bound preparation/deployment/oracle manifests are required for the recorded new source bundle identities.'
    ],
    'reviewer_corrections':[
        {'artifact':'static-probes-initial-reviewer-assertion.log','explanation':'Initial reviewer check required an R-only PT_LOAD; the ARM64 filter resides in RX, not writable. Corrected to require readable/non-writable mapping. No product change.'},
        {'artifact':'integrity-after-initial-global-status.json','explanation':'Whole Git-status equality failed only because of concurrent out-of-inventory documentation edits. All inventoried source/build bytes and HEAD matched. Final check separates scoped integrity from global status.'}
    ],
    'out_of_scope_git_status_changes':{'added_lines':after['git_status_added_lines'],'removed_lines':after['git_status_removed_lines'],
                                     'reviewer_modified_these_paths':False},
    'baseline_report_reference':{'path':'docs/evidence/m4-c0-architecture-remediation-2026-10-12/security-review.md',
        'sha256':digest(REPO/'docs/evidence/m4-c0-architecture-remediation-2026-10-12/security-review.md')},
    'review_markdown_sha256':digest(OUT/'review.md'),
    'evidence_sha256':{p.relative_to(OUT).as_posix():digest(p) for p in sorted(OUT.rglob('*'))
                       if p.is_file() and p.name not in ('review.json','SHA256SUMS') and 'test-tmp' not in p.parts}
}
(OUT/'review.json').write_text(json.dumps(result,sort_keys=True,indent=2)+'\n')
checksums = ''.join(digest(p)+'  '+p.relative_to(OUT).as_posix()+'\n' for p in sorted(OUT.rglob('*')) if p.is_file() and p.name != 'SHA256SUMS')
(OUT/'SHA256SUMS').write_text(checksums)
print(result['decision'])
for name in ('review.md','review.json','SHA256SUMS'): print(digest(OUT/name)+'  '+name)
