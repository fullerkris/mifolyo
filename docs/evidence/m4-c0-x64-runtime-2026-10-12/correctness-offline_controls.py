#!/usr/bin/env python3
"""Independent static/offline controls. Never loads seccomp or runs native actors.

ELF parsing, cBPF evaluation and the ptrace policy oracle here are independent of
the package's interpreters. Existing admission fixtures supply inert inputs only.
libseccomp is used ONLY to compile/export OCI rules to files (no seccomp_load).
"""
import ast
import copy
import ctypes as C
import ctypes.util
import hashlib
import itertools
import json
from pathlib import Path
import re
import struct
import subprocess
import sys

ROOT = Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12')
OUT = ROOT / 'correctness'
PKG = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
BASE = 'd8016469a5ffcceeb04329deec4e3dea2c72dd97'
sys.dont_write_bytecode = True
sys.path.insert(0, str(PKG))
import build_tools
import contracts as c
import profiles
import runtime_admission as admission
import test_runtime_admission as fixture

def sha(raw): return hashlib.sha256(raw).hexdigest()
def data(path): return json.loads(path.read_bytes())
def check(value, message):
    if not value: raise AssertionError(message)

class ELF:
    def __init__(self, raw):
        self.raw = raw
        h = struct.unpack_from('<16sHHIQQQIHHHHHH', raw)
        check(h[0][:7] == b'\x7fELF\x02\x01\x01' and h[1] == 3, 'ELF header')
        self.machine, self.entry = h[2], h[4]
        self.programs = [struct.unpack_from('<IIQQQQQQ', raw, h[5] + n*h[9]) for n in range(h[10])]
        self.sections = [struct.unpack_from('<IIQQQQIIQQ', raw, h[6] + n*h[11]) for n in range(h[12])]
        self.loads = [p for p in self.programs if p[0] == 1]
        check(not any(p[0] == 3 for p in self.programs), 'unexpected PT_INTERP')
        check(any(p[1] & 1 and p[3] <= self.entry < p[3]+p[5] for p in self.loads), 'entry not executable')
        check(all(p[1] & 3 != 3 for p in self.loads), 'writable executable segment')
        check(all(not p[1] & 1 for p in self.programs if p[0] == 0x6474e551), 'executable stack')
        self.symbols = {}
        for s in self.sections:
            if s[1] != 2: continue
            strings = self.sections[s[6]]
            strings = raw[strings[4]:strings[4]+strings[5]]
            for off in range(s[4], s[4]+s[5], s[9]):
                name, info, other, index, address, size = struct.unpack_from('<IBBHQQ', raw, off)
                label = strings[name:strings.index(b'\0', name)].decode()
                if label.startswith('obs_') and index:
                    check(label not in self.symbols, 'duplicate symbol')
                    self.symbols[label] = dict(vaddr=address, size=size, type=info&15, symtab_offset=off)

    def bytes_at(self, address, size):
        matches = [p for p in self.loads if p[3] <= address and address + size <= p[3] + p[5]]
        check(len(matches) == 1, 'ambiguous mapping')
        offset = matches[0][2] + address - matches[0][3]
        return offset, self.raw[offset:offset+size]

def bpf(raw): return list(struct.iter_unpack('<HBBI', raw))

def evaluate(program, number, arch=0xc000003e, args=(0, 1, 0, 0, 0, 0)):
    packet = struct.pack('<IIQ6Q', number & 0xffffffff, arch, 0, *args)
    pc = a = 0
    for _ in range(len(program) + 1):
        op, yes, no, k = program[pc]
        if op == 0x20:
            a = struct.unpack_from('<I', packet, k)[0]; pc += 1
        elif op in (0x15, 0x25, 0x35, 0x45):
            matched = {0x15: a == k, 0x25: a > k, 0x35: a >= k, 0x45: bool(a & k)}[op]
            pc += 1 + (yes if matched else no)
        elif op == 0x05: pc += 1 + k
        elif op == 0x54: a &= k; pc += 1
        elif op == 0x06: return k
        else: raise AssertionError(f'unsupported cBPF {op:x}')
        check(pc < len(program), 'cBPF out-of-bounds')
    raise AssertionError('cBPF nontermination')

class Cmp(C.Structure):
    _fields_ = [('arg', C.c_uint), ('op', C.c_int), ('datum_a', C.c_uint64), ('datum_b', C.c_uint64)]

def compile_oci(policy, filename):
    """Pure libseccomp rule compilation; syscall/filter installation is forbidden."""
    lib = C.CDLL(ctypes.util.find_library('seccomp'))
    lib.seccomp_init.argtypes, lib.seccomp_init.restype = [C.c_uint32], C.c_void_p
    lib.seccomp_release.argtypes = [C.c_void_p]
    lib.seccomp_syscall_resolve_name.argtypes, lib.seccomp_syscall_resolve_name.restype = [C.c_char_p], C.c_int
    lib.seccomp_rule_add_array.argtypes = [C.c_void_p, C.c_uint32, C.c_int, C.c_uint, C.POINTER(Cmp)]
    lib.seccomp_export_bpf.argtypes = [C.c_void_p, C.c_int]
    lib.seccomp_arch_native.restype = C.c_uint32
    check(lib.seccomp_arch_native() == 0xc000003e, 'offline OCI compiler requires x64 host ABI')
    check(policy['architectures'] == ['SCMP_ARCH_X86_64'], 'OCI architecture')
    ctx = lib.seccomp_init(0x50000 | policy['defaultErrnoRet'])
    check(ctx, 'seccomp_init')
    try:
        for rule in policy['syscalls']:
            action = 0x7fff0000 if rule['action'] == 'SCMP_ACT_ALLOW' else 0x50000 | rule['errnoRet']
            items = [Cmp(arg['index'], 4 if arg['op'] == 'SCMP_CMP_EQ' else 7, arg['value'], arg.get('valueTwo', 0)) for arg in rule['args']]
            array = (Cmp * len(items))(*items)
            for name in rule['names']:
                number = lib.seccomp_syscall_resolve_name(name.encode())
                check(number >= 0, 'unknown OCI syscall ' + name)
                check(lib.seccomp_rule_add_array(ctx, action, number, len(items), array) == 0, 'OCI compile ' + name)
        with (OUT / filename).open('xb') as stream:
            check(lib.seccomp_export_bpf(ctx, stream.fileno()) == 0, 'OCI export')
    finally:
        lib.seccomp_release(ctx)
    return bpf((OUT / filename).read_bytes())

def ptrace_allowed(args):
    request, tid, offset, value, _, _ = args
    if tid != 1: return False
    if request in {2, 7, 9, 17, 0x4202, 0x4206, 0x4207}: return True
    if request == 0x4204: return offset == 1
    if request == 3: return offset in {848, 856, 864, 872, 896, 904}
    if request == 6:
        return offset == 848 or offset == 896 and value == 0xffff0ff0 or offset == 904 and value in {0, 1}
    return False

report = {'kind': 'independent_c0_x64_static_controls', 'native_actors_executed': False,
          'ptrace_invoked': False, 'seccomp_installed': False, 'status': 'PASS', 'artifacts': {}}
inventory = data(ROOT/'source-inventory.json')['files']
baseline = data(REPO/'docs/evidence/m4-c0-architecture-remediation-2026-10-12/correctness-artifact-checks.json')
for arch, machine in [('aarch64', 183), ('x86_64', 62)]:
    folder = ROOT/'final-build'/arch
    artifacts = folder/'artifacts'
    build = data(folder/'build.json')
    for path, expected in build['artifacts'].items(): check(sha((artifacts/path).read_bytes()) == expected, arch+' artifact hash '+path)
    check(sha((folder/'build.log').read_bytes()) == build['log_sha256'], 'build log')
    for name, expected in build['source_files'].items():
        check(expected == inventory['tests/crawl-jobs-v2-observer/'+name], 'build source inventory')
        check(sha((folder/'package'/name).read_bytes()) == expected, 'build package source')
    toolchain = data(artifacts/'toolchain.json')
    for name, expected in toolchain['sources'].items(): check(expected == inventory['tests/crawl-jobs-v2-observer/'+name], 'compiler source input')
    check(toolchain['architecture'] == arch and toolchain['commands']['cc -dumpmachine'].strip().startswith(arch+'-'), 'compiler target')
    check(toolchain['platform_sha256'] == c.platform_digest(arch), 'compiler platform binding')
    target, observer = [ELF((artifacts/name).read_bytes()) for name in ('target', 'observer')]
    check(target.machine == observer.machine == machine, 'ELF machine')
    for name in ('target', 'observer'): check(toolchain[name+'_sha256'] == sha((artifacts/name).read_bytes()), 'toolchain ELF join')
    manifest = data(artifacts/'target-manifest.json')
    check((artifacts/'target-manifest.json').read_bytes() == c.canonical(build_tools.target_manifest(target.raw, arch)), 'manifest recomputation')
    check(manifest['elf_sha256'] == sha(target.raw), 'manifest ELF hash')
    check(sha(target.raw).encode() in observer.raw, 'observer embedded target identity')
    for role in ('observer', 'target', 'oracle'):
        raw = (artifacts/(role+'-seccomp.json')).read_bytes()
        check(raw == c.canonical(profiles.profile(role, arch)), 'profile artifact join')
        if arch == 'aarch64': check(sha(raw) == baseline['profiles_sha256'][role], 'ARM profile continuity')
    raw_filter = (artifacts/'target-filter.bpf').read_bytes()
    sym = observer.symbols['obs_filter']
    offset, embedded = observer.bytes_at(sym['vaddr'], sym['size'])
    check(embedded == raw_filter == b''.join(struct.pack('<HBBI', *row) for row in profiles.target_filter_program(arch)), 'compiled filter join')
    if arch == 'aarch64': check(sha(raw_filter) == baseline['compiled_filter']['sha256'], 'ARM BPF continuity')
    instructions = {}
    for name in ('obs_pre_effect', 'obs_post_effect', 'obs_vm_marker', 'obs_send_marker'):
        row = manifest['symbols'][name]
        size = row.get('instruction_bytes', 4)
        where, raw = target.bytes_at(row['vaddr'], size)
        check(where == row['file_offset'] and int.from_bytes(raw, 'little') == row['instruction'], 'instruction join')
        check(row['vaddr'] == target.symbols[name]['vaddr'], 'symbol join')
        instructions[name] = {'vaddr': row['vaddr'], 'file_offset': where, 'bytes': raw.hex(), 'width': size}
    header = (artifacts/'target_manifest.h').read_text()
    defines = {name: int(number) for name, number in re.findall(r'#define (OBS_\w+) UINT64_C\((\d+)\)', header)}
    check(defines['OBS_TARGET_MACHINE'] == machine and defines['OBS_TARGET_SIZE'] == len(target.raw), 'header ABI')
    check(defines['OBS_WITNESS_SYMBOL'] == target.symbols['obs_witness_address']['vaddr'], 'header witness')
    for key, symbol in [('PRE', 'obs_pre_effect'), ('POST', 'obs_post_effect'), ('NOP', 'obs_vm_marker')]:
        check(defines['OBS_'+key+'_BYTES'] == instructions[symbol]['width'], 'header width')
        check(defines['OBS_'+key+'_INSTRUCTION'] == int.from_bytes(bytes.fromhex(instructions[symbol]['bytes']), 'little'), 'header instruction')
    for name, key in [('obs_pre_effect','PRE_EFFECT'), ('obs_post_effect','POST_EFFECT'), ('obs_vm_marker','VM_MARKER'), ('obs_send_marker','SEND_MARKER')]:
        check(defines['OBS_'+key] == target.symbols[name]['vaddr'], 'header marker')
    executable = [p for p in target.loads if p[1] & 1]
    check(len(executable) == 1, 'unique exec load')
    for key, value in [('OFFSET', executable[0][2]&~4095), ('VADDR', executable[0][3]&~4095), ('SIZE', executable[0][5])]:
        check(defines['OBS_EXEC_'+key] == value, 'exec mapping header')
    mutations = 0
    if arch == 'x86_64':
        sym = target.symbols['obs_effect']; start = sym['vaddr']
        where, raw = target.bytes_at(start, sym['size'])
        check(raw == bytes.fromhex('48c7012a000000488916488917c3'), 'SysV effect')
        check(target.symbols['obs_pre_effect']['vaddr'] == start and target.symbols['obs_post_effect']['vaddr'] == start+13, 'effect boundaries')
        # Mutate every bit of the real compiled effect and marker functions, not the toy ELF.
        regions = [(where, 14)] + [(target.bytes_at(target.symbols[n]['vaddr'], 2)[0], 2) for n in ('obs_vm_marker','obs_send_marker')]
        for where, width in regions:
            for index, bit in itertools.product(range(width), range(8)):
                changed = bytearray(target.raw); changed[where+index] ^= 1<<bit
                try: build_tools.target_manifest(bytes(changed), arch)
                except c.Invalid: mutations += 1
                else: raise AssertionError('real ELF instruction bit corruption accepted')
    report['artifacts'][arch] = {'machine': machine, 'build_record_sha256': sha((folder/'build.json').read_bytes()),
        'artifact_hashes': build['artifacts'], 'compiled_filter_offset': offset, 'compiled_filter_bytes': len(embedded),
        'instruction_mapping': instructions, 'effect_marker_bit_mutations_rejected': mutations,
        'source_files_verified': len(build['source_files']), 'toolchain_sources_verified': len(toolchain['sources'])}

self_filter = bpf((ROOT/'final-build/x86_64/artifacts/target-filter.bpf').read_bytes())
policies = {role: compile_oci(profiles.profile(role, 'x86_64'), 'oci-'+role+'-compiled.bpf') for role in ('target','observer','oracle')}
count = 0
def assert_filters(args):
    global count
    expected = ptrace_allowed(args)
    check(evaluate(self_filter, 101, args=args) == (0x7fff0000 if expected else 0x50001), 'self filter ptrace '+str(args))
    check((evaluate(policies['observer'], 101, args=args) == 0x7fff0000) == expected, 'compiled OCI ptrace '+str(args))
    count += 1

for request, tid, offset, value in itertools.product([*range(33), *range(0x4200,0x4210)], [0,1,2,2**32+1],
        [0,1,0x402,848,856,864,872,880,888,896,904,912,849,2**32+848], [0,1,2,0x400,0x10001,0xffff0ff0,0xffff0ff1,2**32+1,2**64-1]):
    assert_filters((request,tid,offset,value,0,0))
for base in [(6,1,848,0x7ffff012301d,0,0), (6,1,896,0xffff0ff0,0,0), (6,1,904,1,0,0),
             (3,1,848,0,0,0), (0x4204,1,1,0,0,0), (7,1,0,0,0,0)]:
    for field, bit in itertools.product(range(4), range(64)):
        args = list(base); args[field] ^= 1<<bit
        assert_filters(tuple(args))
syscall_checks = 0
for nr in [*range(1024), *(1<<bit for bit in range(10,32)), 0xffffffff, 0x40000065, 0x40000209]:
    expected = 0x80000000 if nr >= 512 else 0x50001 if nr in (59,322) else 0x7fff0000
    check(evaluate(self_filter, nr, args=(7,1,0,0,0,0)) == expected, 'syscall guard')
    if nr >= 512:
        for role, program in policies.items(): check(evaluate(program, nr) != 0x7fff0000, role+' x32/compat allowed')
    syscall_checks += 1
for arch in (0,62,0xc00000b7,0x40000003,0x4000003e):
    check(evaluate(self_filter, 101, arch) == 0x80000000, 'self foreign ABI')
    for program in policies.values(): check(evaluate(program, 0, arch) != 0x7fff0000, 'OCI foreign ABI')
for role, program in policies.items():
    for op in (0,0x1001,0x1002,0x1003,0x1012,0x1022,2**32+0x1002):
        check((evaluate(program, 158, args=(op,0,0,0,0,0)) == 0x7fff0000) == (op==0x1002), 'TLS guard')
    check((evaluate(program,90) == 0x7fff0000) == (role=='target'), 'chmod role')
report['filters'] = {'ptrace_adversarial_inputs_per_layer': count, 'syscall_numbers': syscall_checks,
    'layers': ['actual ELF-embedded BPF', 'libseccomp offline-compiled OCI'], 'seccomp_loaded': False,
    'oci_compiled_sha256': {role: sha((OUT/('oci-'+role+'-compiled.bpf')).read_bytes()) for role in policies}}

admissions = rejected = 0
for prep_arch, ctx_arch, bind_arch, frame_arch in itertools.product(('aarch64','x86_64'), repeat=4):
    prep, ctx = fixture.preparation(prep_arch), fixture.context(ctx_arch)
    peer_binding = fixture.binding() | {'architecture': prep_arch}
    peer = admission.validate(fixture.frames(bound=peer_binding), peer_binding, prep, fixture.context(prep_arch))
    for role in ('target','observer','oracle'):
        bound = fixture.binding(role) | {'architecture': bind_arch}
        frames = fixture.changed(fixture.frames(role,bound=bound),0,architecture=frame_arch)
        try: result = admission.validate(frames,bound,prep,ctx,target=None if role=='target' else peer)
        except c.Invalid:
            check(len({prep_arch,ctx_arch,bind_arch,frame_arch}) != 1, 'valid architecture matrix refused')
            rejected += 1
        else:
            check(len({prep_arch,ctx_arch,bind_arch,frame_arch}) == 1, 'mixed ABI matrix admitted')
            check(not result['execution_authorized'] and not result['runtime_evidence'], 'authority escalated')
            admissions += 1
legacy = 0
for architecture, role, index in itertools.product(('aarch64','x86_64'), ('target','observer','oracle'), range(4)):
    bound = fixture.binding(role) | {'architecture':architecture}
    frames = fixture.changed(fixture.frames(role,bound=bound), index, version=1)
    try: admission.native_frame(frames[index], admission.PHASES[index])
    except c.Invalid: legacy += 1
    else: raise AssertionError('legacy frame accepted')
report['admission'] = {'matched_architecture_combinations': admissions, 'mixed_architecture_combinations_rejected': rejected,
                       'legacy_role_phase_frames_rejected': legacy, 'fixture_use': 'inert existing fixture builders only; matrix generated independently'}

base_tests = data(REPO/'docs/evidence/m4-c0-architecture-remediation-2026-10-12/final-tests-022.json')['inventory']
current_tests = data(OUT/'offline-tests.json')['inventory']
renames = {
 'test_build_entrypoints_refuse_x64_before_writing_outputs': 'test_build_entrypoints_refuse_unknown_abi_before_writing_outputs',
 'test_known_x64_deployment_is_metadata_only_until_the_port': 'test_x64_deployment_generates_a_bound_private_bundle',
 'test_direct_ssh_transport_refuses_x64_before_client_or_process_io': 'test_direct_x64_ssh_transport_still_requires_pinned_client',
 'test_admission_summary_cannot_promote_an_unsupported_architecture': 'test_admission_summary_refuses_mixed_architectures',
 'test_x64_process_stream_refuses_before_spawning': 'test_unknown_abi_process_stream_refuses_before_spawning',
}
removed = sorted(set(base_tests)-set(current_tests))
check({name.rsplit('.',1)[1] for name in removed} == set(renames), 'unexpected old test removal')
for name in removed: check(name.rsplit('.',1)[0]+'.'+renames[name.rsplit('.',1)[1]] in current_tests, 'missing replacement')
report['test_continuity'] = {'baseline_tests': len(base_tests), 'current_tests': len(current_tests), 'removed_ids': removed,
    'only_five_approved_renames': True, 'new_ids': sorted(set(current_tests)-set(base_tests))}
(OUT/'offline-controls.json').write_text(json.dumps(report, indent=2, sort_keys=True)+'\n')
print(json.dumps({'status':report['status'], 'ptrace_controls_per_layer':count, 'syscall_controls':syscall_checks,
                  'matched_admissions':admissions, 'rejected_mismatches':rejected, 'legacy_rejections':legacy,
                  'native_actors_executed':False, 'ptrace_invoked':False, 'filters_installed':False}, indent=2))
