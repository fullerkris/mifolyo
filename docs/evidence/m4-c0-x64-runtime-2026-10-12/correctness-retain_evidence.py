#!/usr/bin/env python3
"""Retain read-only provenance and primary-reference bytes in correctness/."""
import ctypes
import ctypes.util
import hashlib
import json
from pathlib import Path
import subprocess
import urllib.request

ROOT = Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12')
OUT = ROOT/'correctness'
def sha(raw): return hashlib.sha256(raw).hexdigest()
inventory = json.loads((ROOT/'source-inventory.json').read_bytes())
reports = {}
for name in ('tests-022.json','tests-077.json','correctness/offline-tests.json'):
    raw = (ROOT/name).read_bytes(); report = json.loads(raw)
    assert report['source_files'] == inventory['files']
    assert report['runner_sha256'] == inventory['runner_sha256']
    assert report['status'] == 'PASS' and report['tests_run'] == 256
    assert report['inventory'] == report['started'] == report['passed']
    assert not any(report[x] for x in ('skipped','expected_failures','unexpected_successes','failures','errors'))
    reports[name] = {'sha256':sha(raw),'tests':256,'all_started_and_passed':True,'source_bound':True}
urls = {
    'linux-v6.18-ptrace.c.txt':'https://raw.githubusercontent.com/torvalds/linux/v6.18/arch/x86/kernel/ptrace.c',
    'linux-v6.18-traps.c.txt':'https://raw.githubusercontent.com/torvalds/linux/v6.18/arch/x86/kernel/traps.c',
    'linux-v6.18-debugreg.h.txt':'https://raw.githubusercontent.com/torvalds/linux/v6.18/arch/x86/include/uapi/asm/debugreg.h',
    'linux-v6.18-user_64.h.txt':'https://raw.githubusercontent.com/torvalds/linux/v6.18/arch/x86/include/asm/user_64.h',
    'seccomp.2.html.txt':'https://man7.org/linux/man-pages/man2/seccomp.2.html',
}
references = {}
for name,url in urls.items():
    with urllib.request.urlopen(url,timeout=30) as response: raw=response.read()
    (OUT/name).write_bytes(raw)
    references[name]={'url':url,'sha256':sha(raw),'bytes':len(raw),'executed':False}
class Version(ctypes.Structure):
    _fields_=[('major',ctypes.c_uint),('minor',ctypes.c_uint),('micro',ctypes.c_uint)]
lib=ctypes.CDLL(ctypes.util.find_library('seccomp'))
lib.seccomp_version.restype=ctypes.POINTER(Version)
version=lib.seccomp_version().contents
tools={'compiler':subprocess.check_output(['cc','--version'],text=True),
       'libseccomp_version':f'{version.major}.{version.minor}.{version.micro}',
       'seccomp_use':'init/rule_add_array/export_bpf/release only; never load'}
(OUT/'provenance.json').write_text(json.dumps({'source_inventory_sha256':sha((ROOT/'source-inventory.json').read_bytes()),
    'test_reports':reports,'primary_references':references,'offline_tooling':tools},sort_keys=True,indent=2)+'\n')
print('PASS: source-bound test reports and primary references retained')
