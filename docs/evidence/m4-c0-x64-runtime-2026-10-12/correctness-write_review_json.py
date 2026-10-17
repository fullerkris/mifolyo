#!/usr/bin/env python3
"""Assemble the machine-readable review from retained offline evidence."""
import hashlib
import json
from pathlib import Path

ROOT = Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12')
OUT = ROOT/'correctness'
def sha(path): return hashlib.sha256(path.read_bytes()).hexdigest()
def load(name): return json.loads((OUT/name).read_bytes())
before,after=load('inventory-before.json'),load('inventory-after.json')
assert before['pass'] and after['pass'] and before == after
controls,helpers,ctx,tests=map(load,('offline-controls.json','helper-probe.json','context-controls.json','offline-tests.json'))
assert all(record['status']=='PASS' for record in (controls,helpers,ctx,tests,load('abi-guards.json'),load('final-integrity.json')))
def check(identifier, topic, sources, evidence, result):
    return {'id':identifier,'topic':topic,'status':'PASS','source_locations':sources,
            'evidence_paths':[str(OUT/name) for name in evidence],'result':result}
report={
    'kind':'c0_arch_03_x64_read_only_correctness_review','version':1,'date':'2026-10-12',
    'verdict':'GO_FOR_CONTINUED_IMPLEMENTATION',
    'summary':'No actionable source/static-artifact correctness defect established. No native-runtime acceptance or execution/deployment authority.',
    'repository':'/var/home/kfuller/Documents/Git_Projects/mifolyo',
    'branch':'feature/crawl-jobs-v2-c0-x64-runtime',
    'base_commit':'d8016469a5ffcceeb04329deec4e3dea2c72dd97',
    'snapshot_path':str(ROOT/'snapshot'),
    'source_inventory_path':str(ROOT/'source-inventory.json'),
    'source_inventory_sha256':sha(ROOT/'source-inventory.json'),
    'source_integrity':{'package_files':51,'runner_files':1,'snapshot_before_and_after_match':True,
                       'repository_before_and_after_match':True,'tracked_diff_unchanged':True,
                       'all_22_declared_artifacts_unchanged':True},
    'findings':[], 'blocker_count':0,'remediation_suggestion_count':0,
    'positive_patterns':[
        'Exact full x64 effect decoding with explicit variable-width instruction boundaries.',
        'Disable/change/reset/arm ordering with exact debug-state readback and fixed owned TID.',
        'Architecture-bound context, image/profile and version-2 admission joins.',
        'ARM64 OCI/BPF byte continuity and explicit unsigned syscall cutoff for x32/compat aliases.'
    ],
    'checks':[
        check('COR-X64-CHECK-01','instruction boundaries and SysV witness stores',
              ['native/target.c:23-35,126-135','build_tools.py:54-105,129-144'],
              ['offline-controls.json','target-objdump.txt','helper-probe.json'],
              {'real_elf_effect_marker_bit_mutations_rejected':144,'instruction_widths':[7,1,1,1],
               'release_ordering_basis':'static x86 TSO/aligned-store analysis, not hardware observation'}),
        check('COR-X64-CHECK-02','debug protocol and observer helper integration',
              ['native/x86_debug.h:7-54','native/observer.c:127-156,185-208,253-318'],
              ['test_x86_debug.log','helper-probe.json','helper-provenance.json','helper_probe.c','helper-undefined-symbols.txt'],
              {'supplied_simulated_debug_controls':29,'independent_helper_controls':3703,
               'request_instruction_controls':2440,'debug_controls':1191,'regset_controls':72,
               'all_ptrace_requests_simulated':True,'ubsan_trap_mode_passed':True}),
        check('COR-X64-CHECK-03','LP64 and UAPI/structure offsets',
              ['native/architecture.h:1-16','native/common.h:28-41','native/x86_debug.h:7-13','native/observer.c:21-26'],
              ['helper-probe.json','abi-guards.json'],
              {'user_regs_bytes':216,'rip_offset':128,'cs_offset':136,'witness_bytes':8480,
               'completed_offset':8,'last_value_offset':72,'transcript_offset':80,
               'macro_configurations_rejected':4,'foreign_runtime_tested':False}),
        check('COR-X64-CHECK-04','actual static artifacts and confinement joins',
              ['build_tools.py:12-43,145-172','profiles.py:16-84,87-173','Dockerfile:1-18'],
              ['offline-controls.json','offline_controls.py','provenance.json','final-integrity.json'],
              {'artifact_sets':['aarch64','x86_64'],'source_files_per_build':51,'toolchain_source_inputs_per_build':12,
               'arm64_bpf_bytes':272,'x64_bpf_bytes':440,'arm64_profiles_and_bpf_baseline_identical':True,
               'ptrace_inputs_per_filter_layer':26232,'syscall_numbers':1049,
               'oci_compiler':'libseccomp 2.6.1 offline export only','filters_installed':False}),
        check('COR-X64-CHECK-05','context/oracle ABI selection and version-2 admission joins',
              ['linux_observation.py:38-51,89-103,113-134,168-195','oracle_worker.py:146-188',
               'build_oracle.py:11-27','runtime_context.py:64-98,112-184','runtime_admission.py:28-44,98-151',
               'docker_backend.py:254-283,360-474'],
              ['offline-controls.json','context-controls.json','context_controls.py'],
              {'matching_role_combinations_accepted':6,'mixed_architecture_combinations_rejected':42,
               'legacy_role_phase_frames_rejected':24,'additional_simulated_context_controls':32,
               'valid_mac_foreign_replies_rejected':True,'real_host_collectors_or_oracle_workers_run':False}),
        check('COR-X64-CHECK-06','test continuity and frozen integrity',
              ['scripts/test-crawl-jobs-v2-observer.py:42-86','test_architecture_contracts.py'],
              ['offline-tests.json','offline-tests.log','offline-controls.json','provenance.json',
               'inventory-before.json','inventory-after.json','final-integrity.json'],
              {'tests_run':256,'tests_passed':256,'skips':0,'expected_failures':0,
               'base_tests':238,'retained_old_ids':233,'approved_replacements':5,'additional_tests':18,
               'supplied_umask_022_and_077_reports_verified':True}),
    ],
    'authorization_and_safety':{
        'scope':'source/static-artifact implementation only', 'repository_or_snapshot_modified':False,
        'ptrace_invoked':False,'filters_installed':False,'native_actors_executed':False,
        'real_host_collectors_executed':False,'real_oracle_workers_executed':False,
        'simulated_oracle_protocol_test_processes_run':True,'benign_test_processes_run':True,
        'committed':False,'pushed':False,'deployed':False,
        'execution_authorized':False,'runtime_acceptance':False,'m4_accepted':False,
        'writes_confined_to':str(OUT)
    },
    'limits':[
        'C0-ARCH-05 hardware breakpoint/kernel/LSM/backend/timing acceptance remains untested and unaccepted.',
        'C0-ARCH-04 complete dual-runtime image set is outside scope; oracle Dockerfile intentionally remains ARM64-only.',
        'No native actor or real collector/worker was executed. C probes substitute all ptrace requests with in-memory callbacks.',
        'Supplied native builds were inspected and hash-joined, not independently rebuilt during this review.',
        'Offline OCI compilation used local libseccomp 2.6.1, not a deployment-admitted OCI compiler.',
        'ASan/shared UBSan could not link due to missing host sanitizer libraries. The independent helper passed UBSan trap instrumentation instead.',
        'Linux v6.18 primary sources and seccomp(2) are references, not evidence of running-host capabilities.',
        'Admission fixtures are synthetic; hashes/ELF header classification do not replace full interpreter/image/runtime admission.'
    ],
    'next_step':'Continue implementation; keep image completion and separately owner-approved native acceptance behind their existing gates.',
    'review_markdown_path':str(OUT/'review.md'),'review_markdown_sha256':sha(OUT/'review.md'),
    'evidence_files':{str(path):{'sha256':sha(path),'bytes':path.stat().st_size}
                      for path in sorted(OUT.iterdir()) if path.is_file() and path.name!='review.json'}
}
(OUT/'review.json').write_text(json.dumps(report,indent=2,sort_keys=True)+'\n')
for name in ('review.md','review.json'): print(sha(OUT/name)+'  '+str(OUT/name))
