"""Independent static ELF/filter and malicious-record probes; never loads BPF or ELF."""
import ast
import copy
import hashlib
import json
import os
from pathlib import Path
import random
import struct
import subprocess
import sys
import tempfile
from types import SimpleNamespace
from unittest.mock import patch

ROOT = Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12')
SOURCE = ROOT / 'snapshot/tests/crawl-jobs-v2-observer'
OUT = ROOT / 'security'
REPO = Path('/var/home/kfuller/Documents/Git_Projects/mifolyo')
BASE = 'd8016469a5ffcceeb04329deec4e3dea2c72dd97'
sys.path.insert(0, str(SOURCE))
import build_tools as build
import contracts as c
import profiles

result = {'kind': 'independent_static_security_probes', 'native_execution': False, 'filter_installation': False,
          'artifact_checks': {}, 'counters': {}, 'record_probes': [], 'status': 'RUNNING'}

def check(name, condition):
    assert condition, name
    result['counters'][name] = result['counters'].get(name, 0) + 1

def elf(raw):
    """Reviewer-side decoding, independent of build_tools.parse_elf."""
    h = struct.unpack_from('<16sHHIQQQIHHHHHH', raw)
    assert h[0][:7] == b'\x7fELF\x02\x01\x01' and h[1] == 3
    ph = [struct.unpack_from('<IIQQQQQQ', raw, h[5] + i*h[9]) for i in range(h[10])]
    sh = [struct.unpack_from('<IIQQQQIIQQ', raw, h[6] + i*h[11]) for i in range(h[12])]
    names = raw[sh[h[13]][4]:sh[h[13]][4] + sh[h[13]][5]]
    sections, syms = {}, {}
    for s in sh:
        n = names[s[0]:names.find(b'\0', s[0])].decode()
        sections[n] = s
        if s[1] != 2: continue
        strings = sh[s[6]]
        table = raw[strings[4]:strings[4] + strings[5]]
        for off in range(s[4], s[4] + s[5], s[9]):
            name, info, other, index, address, size = struct.unpack_from('<IBBHQQ', raw, off)
            label = table[name:table.find(b'\0', name)].decode()
            if label.startswith('obs_'):
                assert label not in syms
                syms[label] = (address, size, index, off)
    return h, ph, sections, syms

def blob(program): return b''.join(struct.pack('<HBBI', *row) for row in program)

def vm(program, number, args=(0,0,0,0,0,0), arch=0xc000003e):
    """Bounded reviewer BPF interpreter over real seccomp_data byte layout."""
    data = struct.pack('<IIQ6Q', number & 0xffffffff, arch, 0, *(v & ((1<<64)-1) for v in args))
    pc, acc = 0, 0
    for _ in range(len(program)+1):
        code, jt, jf, k = program[pc]
        if code == 0x20:
            assert k % 4 == 0 and k <= len(data)-4
            acc = struct.unpack_from('<I', data, k)[0]
            pc += 1
        elif code in (0x15, 0x35):
            yes = acc == k if code == 0x15 else acc >= k
            pc += 1 + (jt if yes else jf)
        elif code == 0x06:
            return k
        else: raise AssertionError(('opcode', code))
        assert 0 <= pc < len(program)
    raise AssertionError('nonterminating filter')

def oci(policy, name, args):
    for r in policy['syscalls']:
        if name not in r['names']: continue
        ok = True
        for cmp in r['args']:
            value = args[cmp['index']] & ((1<<64)-1)
            if cmp['op'] == 'SCMP_CMP_EQ': ok &= value == cmp['value']
            elif cmp['op'] == 'SCMP_CMP_MASKED_EQ': ok &= value & cmp['value'] == cmp['valueTwo']
            else: raise AssertionError('OCI operation')
        if ok: return r['action'] == 'SCMP_ACT_ALLOW'
    return False

def expected(number, args, arch=0xc000003e):
    if arch != 0xc000003e or number & 0xffffffff >= 512: return 0x80000000
    if number in (59,322): return 0x50001
    if number != 101: return 0x7fff0000
    req, tid, off, value, _, _ = (v & ((1<<64)-1) for v in args)
    allow = tid == 1 and (req in (2,7,9,17,0x4202,0x4206,0x4207) or req == 0x4204 and off == 1 or
                         req == 3 and off in (848,856,864,872,896,904) or
                         req == 6 and (off == 848 or off == 896 and value == 0xffff0ff0 or off == 904 and value in (0,1)))
    return 0x7fff0000 if allow else 0x50001

def base_file(name):
    return subprocess.check_output(['git', 'show', BASE + ':tests/crawl-jobs-v2-observer/' + name], cwd=REPO)

# Baseline generation is pure Python and uses the same already-validated ARM descriptor.
baseline = {'__name__': 'baseline_profiles'}
exec(compile(base_file('profiles.py'), 'merged-base/profiles.py', 'exec'), baseline)
inventory = json.loads((ROOT / 'source-inventory.json').read_bytes())['files']
for arch in ('aarch64', 'x86_64'):
    folder = ROOT / 'final-build' / arch
    artifacts = folder / 'artifacts'
    record = json.loads((folder / 'build.json').read_bytes())
    toolchain = json.loads((artifacts / 'toolchain.json').read_bytes())
    check('build_success_and_platform', record['status'] == 'PASS' and record['returncode'] == 0 and record['architecture'] == arch)
    for name, digest in record['artifacts'].items(): check('record_artifact_hashes', c.sha((artifacts/name).read_bytes()) == digest)
    for name, digest in record['source_files'].items():
        check('build_source_hashes', digest == inventory['tests/crawl-jobs-v2-observer/'+name] == c.sha((folder/'package'/name).read_bytes()))
    for name, digest in toolchain['sources'].items():
        check('compiler_source_inputs', digest == inventory['tests/crawl-jobs-v2-observer/'+name])
    check('toolchain_platform', toolchain['architecture'] == arch and toolchain['platform_sha256'] == c.platform_digest(arch))
    bpf = (artifacts/'target-filter.bpf').read_bytes()
    check('canonical_generated_bpf', bpf == blob(profiles.target_filter_program(arch)))
    profiles_info = {}
    for role in ('target','observer','oracle'):
        raw = (artifacts/(role+'-seccomp.json')).read_bytes()
        check('canonical_oci_profile', raw == c.canonical(profiles.profile(role, arch)))
        if arch == 'aarch64': check('unchanged_arm64_profiles', raw == c.canonical(baseline['profile'](role)))
        profiles_info[role] = c.sha(raw)
    if arch == 'aarch64': check('unchanged_arm64_bpf', bpf == blob(baseline['target_filter_program']()))
    details = {'profiles': profiles_info, 'filter_sha256': c.sha(bpf), 'filter_bytes': len(bpf), 'elfs': {}}
    for role in ('target','observer'):
        raw = (artifacts/role).read_bytes()
        h, ph, sections, syms = elf(raw)
        check('native_static_pie_header', h[2] == {'aarch64':183, 'x86_64':62}[arch] and not any(p[0] == 3 for p in ph))
        check('no_writable_executable_loads', all(p[1] & 3 != 3 for p in ph if p[0] == 1))
        check('nonexec_stack_and_relro', any(p[0] == 0x6474e551 and not p[1]&1 for p in ph) and any(p[0] == 0x6474e552 for p in ph))
        check('toolchain_elf_hash', c.sha(raw) == toolchain[role+'_sha256'])
        details['elfs'][role] = {'sha256': c.sha(raw), 'bytes':len(raw), 'elf_machine':h[2]}
        if role == 'observer':
            addr, size, index, _ = syms['obs_filter']
            s = sections['.obs_filter']
            check('compiled_symbol_equals_filter_section', s[3] == addr and s[5] == size and raw[s[4]:s[4]+size] == bpf)
            loads = [p for p in ph if p[0] == 1 and p[3] <= addr and addr+size <= p[3]+p[5]]
            # ARM64's pinned linker combines text/rodata in RX; read-only does
            # not require a dedicated R-only PT_LOAD. Neither mapping is writable.
            check('compiled_filter_readonly_mapped', len(loads) == 1 and loads[0][1] & 6 == 4 and not s[2] & 1)
            check('target_identity_embedded', record['artifacts']['target'].encode()+b'\0' in raw)
            details['filter_vaddr'] = addr
            details['filter_file_offset'] = s[4]
    check('target_manifest_recomputed', c.canonical(build.target_manifest((artifacts/'target').read_bytes(), arch)) == (artifacts/'target-manifest.json').read_bytes())
    result['artifact_checks'][arch] = details

# Interpret the ACTUAL bytes extracted from the x64 observer, not generator output.
artifact = ROOT/'final-build/x86_64/artifacts'
observer = (artifact/'observer').read_bytes()
_, _, sections, _ = elf(observer)
s = sections['.obs_filter']
program = list(struct.iter_unpack('<HBBI', observer[s[4]:s[4]+s[5]]))
policy = json.loads((artifact/'observer-seccomp.json').read_bytes())

def vector(name, number, args=(0,0,0,0,0,0), arch=0xc000003e):
    check(name, vm(program, number, args, arch) == expected(number, args, arch))
    if number == 101 and arch == 0xc000003e:
        check('oci_ptrace_vectors', oci(policy, 'ptrace', args) == (expected(number,args) == 0x7fff0000))

for nr in list(range(1024)) + [0x40000000 | i for i in range(1024)] + [-1,-2,-2147483648,0x7fffffff,0xffffffff]:
    vector('syscall_alias_vectors', nr)
for arch in (0,62,183,0x40000003,0xc00000b7,0x4000003e,0x8000003e):
    for nr in (0,59,101,322,521,0x40000065): vector('foreign_audit_vectors', nr, arch=arch)
for request in (*range(34), *range(0x4200,0x4240)):
    for tid in (0,1,2,0xffffffff,0x100000001,0xffffffffffffffff):
        for off in (0,1,4,848,856,864,872,880,888,896,904,912,1026,1027,0x100000350):
            vector('request_tid_offset_vectors', 101, (request,tid,off,0,0,0))
for off in range(920):
    for value in (0,1,2,0xffff0ff0,0xffff0ff1,0x100000001):
        vector('all_user_offsets_vectors',101,(6,1,off,value,0,0))
        vector('all_user_offsets_vectors',101,(3,1,off,value,0,0))
for value in range(1<<17):
    vector('dr7_low_modes_vectors',101,(6,1,904,value,0,0))
for bit in range(64):
    for value in (1<<bit, (1<<bit)|1, (1<<bit)^0xffff0ff0):
        for off in (848,896,904): vector('debug_fullwidth_vectors',101,(6,1,off,value,0,0))
    for field in (0,1,2):
        args = [6,1,904,1,0,0]; args[field] |= 1<<bit
        vector('selector_fullwidth_vectors',101,tuple(args))
rng = random.Random(20261012)
for _ in range(5000):
    req = rng.choice((3,6,0x4204,0x4205,4,5,13,0x420f))
    off = rng.choice((848,856,864,872,880,888,896,904,1,0x402))
    vector('random_debug_values',101,(req,1,off,rng.getrandbits(64),0,0))
for role in ('target','observer','oracle'):
    policy_role = profiles.profile(role,'x86_64')
    for name in ('process_vm_writev','process_vm_readv','perf_event_open','bpf','setns','unshare','execveat','mount','kill','ptrace'):
        if role == 'observer' and name == 'ptrace': continue
        check('forbidden_syscall_profiles', not oci(policy_role,name,(7,1,0,0,0,0)))
    for bit in range(64):
        operation = 0x1002 | 1<<bit
        check('arch_prctl_fullwidth', oci(policy_role,'arch_prctl',(operation,0,0,0,0,0)) == (operation == 0x1002))

# Mutate BOTH sidecar and actual embedded filter: equality alone must not suffice.
def record_probe(name, target, observed, bpf, manifest, expected_error):
    with tempfile.TemporaryDirectory(prefix='record-') as directory:
        path = Path(directory)
        for filename, raw in [('target',target),('observer',observed),('target-filter.bpf',bpf),('target-manifest.json',manifest)]:
            (path/filename).write_bytes(raw)
        with patch.object(sys,'argv',['build_tools.py','record',str(path),'x86_64']), patch.object(build.subprocess,'run') as run, patch.object(build.subprocess,'check_output') as look:
            try: build.main()
            except c.Invalid as error: code = str(error)
            else: raise AssertionError('accepted record: '+name)
        check('invalid_record_refused_before_compiler', code == expected_error and not run.called and not look.called and not (path/'toolchain.json').exists())
        result['record_probes'].append({'name':name,'refusal':code,'compiler_queries':0,'output_written':False})

target = (artifact/'target').read_bytes()
manifest = (artifact/'target-manifest.json').read_bytes()
canonical_filter = (artifact/'target-filter.bpf').read_bytes()
for index,(code,jt,jf,k) in enumerate(program):
    if code not in (0x15,0x35) or k not in (0xc000003e,512,0,1,848,896,904,0xffff0ff0): continue
    changed = list(program); changed[index] = (code,jt,jf,k^1)
    raw_bpf = blob(changed)
    raw_observer = bytearray(observer); raw_observer[s[4]:s[4]+s[5]] = raw_bpf
    record_probe('matching-embedded-sidecar-cmp-'+str(index),target,bytes(raw_observer),raw_bpf,manifest,'BUILD_FILTER_ARCHITECTURE')
for arch in ('aarch64',):
    foreign = ROOT/'final-build'/arch/'artifacts'
    record_probe('foreign-target', (foreign/'target').read_bytes(),observer,canonical_filter,manifest,'ELF_ARCHITECTURE')
    record_probe('foreign-observer',target,(foreign/'observer').read_bytes(),canonical_filter,manifest,'ELF_ARCHITECTURE')
for name,index,new in [('elf32-target',4,1),('big-endian-target',5,2),('invalid-elf-version',6,0)]:
    raw = bytearray(target); raw[index] = new
    record_probe(name,bytes(raw),observer,canonical_filter,manifest,'ELF_ARCHITECTURE')
for field,value in [('architecture','aarch64'),('platform_sha256',c.platform_digest('aarch64')),('execution_authorized',True),('version',1)]:
    value_manifest = json.loads(manifest); value_manifest[field] = value
    record_probe('manifest-'+field,target,observer,canonical_filter,c.canonical(value_manifest),'BUILD_TARGET_MANIFEST')

result['status'] = 'PASS'
(OUT/'static-probes.json').write_text(json.dumps(result,sort_keys=True,indent=2)+'\n')
print(json.dumps({'status':result['status'],'counters':result['counters'],'record_probes':len(result['record_probes'])},sort_keys=True))
