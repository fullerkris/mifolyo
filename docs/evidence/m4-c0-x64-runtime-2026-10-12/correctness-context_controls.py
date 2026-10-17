#!/usr/bin/env python3
"""Additional pure/simulated ABI, host-anchor and authenticated-response controls."""
import copy
import json
import os
from pathlib import Path
import sys
from types import SimpleNamespace
from unittest.mock import patch

ROOT = Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12')
OUT = ROOT/'correctness'
sys.dont_write_bytecode = True
sys.path.insert(0, str(ROOT/'snapshot/tests/crawl-jobs-v2-observer'))
import contracts as c
import runtime_context as context
from linux_observation import LinuxReader
from test_runtime_context import fixture, acquire, KEY
import test_runtime_admission as native
import runtime_admission as admission

checks = []
def success(name): checks.append({'id': name, 'status': 'PASS'})
def refused(name, call):
    try: call()
    except c.Invalid: success(name)
    else: raise AssertionError(name+' unexpectedly accepted')

for arch in ('aarch64','x86_64'):
    foreign = 'x86_64' if arch == 'aarch64' else 'aarch64'
    parts = list(fixture())
    parts[0].value['architecture'] = parts[2]['architecture'] = arch
    parts[3] = c.preparation(parts[3]['artifacts'], arch)
    parts[4] = context.LinuxContextEndpoint(parts[2], KEY, parts[0])
    parts[5] = context.ContextSource(parts[2], KEY, parts[4].exchange, authorize=lambda *_: True)
    lease = acquire(parts)
    assert lease.view('P1.pre.1.1', c.sha(b'scope'), parts[3], parts[6])['architecture'] == arch
    success(arch+':matched-host-lease')
    refused(arch+':lease-foreign-preparation', lambda: lease.view('P1.pre.1.1', c.sha(b'scope'), c.preparation(parts[3]['artifacts'], foreign), parts[6]))
    # The adversary knows the test HMAC key: rejection must come from ABI binding,
    # not a stale or invalid MAC. No host reader/collector is run.
    endpoint = parts[4]
    def forged_reply(raw, deadline):
        frames = endpoint.exchange(raw, deadline).splitlines(keepends=True)
        payload = c.decode(frames[0]); payload['payload']['architecture'] = foreign
        frames[0] = c.canonical(payload)
        seal = c.decode(frames[3]); seal['mac'] = context.mac(KEY, raw+b''.join(frames[:3]))
        frames[3] = c.canonical(seal)
        return b''.join(frames)
    parts[5] = context.ContextSource(parts[2], KEY, forged_reply, authorize=lambda *_: True)
    refused(arch+':valid-MAC-foreign-kernel-reply', lambda: acquire(parts))
    # Build a valid foreign peer receipt, including all of its digests; do not
    # rely on merely corrupting an architecture label in a previously valid one.
    peer_binding = native.binding() | {'architecture': foreign}
    peer = admission.validate(native.frames(bound=peer_binding), peer_binding, native.preparation(foreign), native.context(foreign))
    for role in ('observer','oracle'):
        bound = native.binding(role) | {'architecture': arch}
        refused(arch+':recomputed-'+role+'-foreign-peer', lambda: admission.validate(native.frames(role,bound=bound),bound,native.preparation(arch),native.context(arch),target=peer))
    # Select from simulated uname, then read only the inert ELF in correctness/.
    for elf_arch in ('aarch64','x86_64'):
        raw = (ROOT/'final-build'/elf_arch/'artifacts/target').read_bytes()[:64]
        path = OUT/('elf-header-'+arch+'-'+elf_arch)
        path.write_bytes(raw)
        real_open = os.open
        with patch('linux_observation.os.uname', return_value=SimpleNamespace(sysname='Linux', machine=arch)), \
             patch('linux_observation.os.open', side_effect=lambda *_: real_open(path, os.O_RDONLY)):
            reader = LinuxReader.native_process()
            if arch == elf_arch:
                assert reader.executable() == c.sha(raw); success(arch+':matching-ELF')
            else: refused(arch+':foreign-ELF-after-matching-uname', reader.executable)
    for label, index, byte in [('x32-class',4,1), ('big-endian',5,2), ('ELF-version',6,0)]:
        raw = bytearray((ROOT/'final-build'/arch/'artifacts/target').read_bytes()[:64]); raw[index]=byte
        path = OUT/('elf-header-'+arch+'-'+label); path.write_bytes(raw)
        with patch('linux_observation.os.open', side_effect=lambda *_: real_open(path, os.O_RDONLY)):
            refused(arch+':'+label, LinuxReader(arch).executable)
    # Anchored host reader must not silently select another native ABI.
    with patch('linux_observation.os.uname', return_value=SimpleNamespace(sysname='Linux',machine=foreign)), \
         patch.object(LinuxReader, 'namespaces', side_effect=AssertionError('foreign host observation continued')):
        refused(arch+':anchored-reader-rejects-before-procfs', LinuxReader(arch).kernel)

for machine in ('amd64','arm64','x32','i686','aarch64_be','riscv64',''):
    with patch('linux_observation.os.uname', return_value=SimpleNamespace(sysname='Linux',machine=machine)):
        refused('native-selector:alias-or-unknown:'+machine, LinuxReader.native_process)
for size in (0,4,16):
    with patch('linux_observation.os.uname', return_value=SimpleNamespace(sysname='Linux',machine='x86_64')), \
         patch('linux_observation.ctypes.sizeof', return_value=size):
        refused('native-selector:pointer-size:'+str(size), LinuxReader.native_process)

report = {'status':'PASS','checks':checks,'count':len(checks),'kernel_and_uname_simulated':True,
          'collectors_executed':False,'oracle_workers_executed':False,'native_actors_executed':False,
          'limit':'ELF-header classifier and mocked source/peer boundaries; not interpreter or deployment acceptance'}
(OUT/'context-controls.json').write_text(json.dumps(report,sort_keys=True,indent=2)+'\n')
print('PASS:',len(checks),'additional simulated context/oracle/peer controls')
