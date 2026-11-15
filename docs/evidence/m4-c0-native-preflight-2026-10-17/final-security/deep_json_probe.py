"""Isolated benign stress probe for the bounded private Engine JSON parser.

No socket/process/config operations. Run with python3 -B and a disabled core dump
limit in this reviewer-owned process; snapshot modules are imported read-only.
"""
import json
from pathlib import Path
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path('/tmp/opencode/c0-native-preflight-2026-10-17')
sys.path.insert(0, str(ROOT/'transport-snapshot/tests/crawl-jobs-v2-observer'))
from contracts import Invalid
from host_preflight import INFO_FIELDS, metadata, checked_docker

info = {'ID': 'independent-deep-review', 'OSType': 'linux', 'Architecture': 'x86_64',
    'ServerVersion': '29.9.1', 'KernelVersion': '6.8-review', 'CgroupVersion': '2',
    'CgroupDriver': 'systemd', 'SecurityOptions': ['name=seccomp,profile=builtin', 'name=cgroupns'],
    'MemoryLimit': True, 'SwapLimit': True, 'PidsLimit': True, 'CpuCfsPeriod': True, 'CpuCfsQuota': True}
version = {'os': 'linux', 'architecture': 'amd64', 'version': '29.9.1', 'kernel': '6.8-review'}
base = json.dumps(info, separators=(',', ':')).encode()
results = []
for depth in (3000, 10000, 60000, 120000):
    nested = b'['*depth+b'0'+b']'*depth
    for selected in (False, True):
        raw = base.replace(b'"OSType":"linux"', b'"OSType":'+nested) if selected else base[:-1]+b',"Unused":'+nested+b'}'
        assert len(raw) <= 262144
        start = time.monotonic()
        try:
            value = metadata(raw, INFO_FIELDS, engine_response=True)
            checked_docker(value, version, 'x86_64')
            outcome = 'selected_metadata_valid'
            assert not selected, 'nested selected OS metadata was accepted'
        except Invalid:
            outcome = 'invalid_value_free'
        row = {'depth': depth, 'selected_field': selected, 'input_bytes': len(raw),
            'outcome': outcome, 'elapsed_seconds': round(time.monotonic()-start, 6)}
        results.append(row)
        print(json.dumps(row, sort_keys=True), flush=True)
with (ROOT/'transport-security/deep-json-original.json').open('x') as output:
    json.dump({'kind': 'bounded_private_json_stress', 'status': 'PASS', 'cases': results,
        'live_socket_or_docker_access': False}, output, indent=2, sort_keys=True)
    output.write('\n')
