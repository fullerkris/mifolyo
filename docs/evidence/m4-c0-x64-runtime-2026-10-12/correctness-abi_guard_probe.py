#!/usr/bin/env python3
"""Compile-only tests of architecture rejection, not cross-ABI runtime tests."""
import json
from pathlib import Path
import subprocess

OUT = Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12/correctness')
base = ['cc','-std=c11','-Wall','-Wextra','-Werror','-fsyntax-only',
        '-I'+str(OUT.parent/'snapshot/tests/crawl-jobs-v2-observer/native'), str(OUT/'abi_guard.c')]
cases = {'native_lp64': [], 'x32_macro': ['-D__ILP32__=1'], 'foreign_os': ['-U__linux__'],
         'foreign_machine': ['-U__x86_64__'],
         'big_endian': ['-U__BYTE_ORDER__','-D__BYTE_ORDER__=__ORDER_BIG_ENDIAN__']}
results=[]
for name,options in cases.items():
    command=base+options
    result=subprocess.run(command,capture_output=True,text=True,check=False)
    assert (result.returncode==0) == (name=='native_lp64')
    if name!='native_lp64': assert 'architecture.h:' in result.stderr and '#error' in result.stderr
    results.append({'id':name,'command':command,'returncode':result.returncode,'stderr':result.stderr,'status':'PASS'})
(OUT/'abi-guards.json').write_text(json.dumps({'status':'PASS','checks':results,'native_actor_executed':False,
    'limit':'macro rejection controls and host LP64 static assertions; not a cross compiler or foreign ABI runtime'},indent=2,sort_keys=True)+'\n')
print('PASS: native LP64 syntax and four rejected ABI macro configurations')
