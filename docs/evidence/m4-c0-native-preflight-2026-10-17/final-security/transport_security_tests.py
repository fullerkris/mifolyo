"""Independent adversarial re-review. No real Engine/SSH/native actor access.

Engine socket creation is replaced only with scripted streams or reviewer-owned
connected socketpairs. The production connect address is asserted, never used.
The original run's log/JSON are exclusive evidence and are not overwritten.
"""
from contextlib import ExitStack, contextmanager, redirect_stdout, redirect_stderr
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import random
import socket
import subprocess
import sys
import threading
import time
import unittest
from unittest.mock import patch

ROOT = Path('/tmp/opencode/c0-native-preflight-2026-10-17')
OUT = ROOT / 'transport-security'
SOURCE = ROOT / 'transport-snapshot/tests/crawl-jobs-v2-observer'
sys.dont_write_bytecode = True
sys.path.insert(0, str(SOURCE))

import engine_metadata as engine
import host_preflight as hp
from contracts import Invalid
from streams import Deadline

EVENTS = []
CLI_ENV_KEYS = set()
CANARY = 'SYNTHETIC_TRANSPORT_SECRET_CANARY'
REAL_INSPECT = hp.inspect_host
REAL_SOCKETPAIR = socket.socketpair


def encode(value):
    return json.dumps(value, ensure_ascii=True, separators=(',', ':')).encode('utf-8')


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def documents(arch='x86_64'):
    # Deliberately independent wire literals, not generated from production maps.
    info = {'ID': 'independent-review-daemon', 'OSType': 'linux', 'Architecture': arch,
        'ServerVersion': '29.9.1', 'KernelVersion': '6.8-review', 'CgroupVersion': '2',
        'CgroupDriver': 'systemd', 'SecurityOptions': ['name=seccomp,profile=builtin', 'name=cgroupns', 'name=no-new-privileges'],
        'MemoryLimit': True, 'SwapLimit': True, 'PidsLimit': True, 'CpuCfsPeriod': True, 'CpuCfsQuota': True}
    version = {'Os': 'linux', 'Arch': 'amd64' if arch == 'x86_64' else 'arm64',
        'Version': '29.9.1', 'KernelVersion': '6.8-review', 'ApiVersion': '1.56'}
    return info, version


class Probe:
    def __init__(self, arch='x86_64'):
        self.arch = arch
        self.docker = hp.HostProbe(arch).docker
        names = ('pid_ns', 'user_ns', 'time_ns', 'cgroup_ns', 'mnt_ns', 'net_ns', 'ipc_ns')
        self.context = {'architecture': arch, 'namespaces': {name: i+100 for i, name in enumerate(names)},
            'ns_device': 4, 'resolution_ns': 1, 'yama_scope': 0,
            **{key: sha(key.encode()) for key in ('kernel_boot_sha256', 'uid_map_sha256', 'gid_map_sha256', 'lsm_sha256', 'time_offsets_sha256')},
            'kernel_release_sha256': sha(b'6.8-review')}
    def native(self):
        return {'os': 'Linux', 'architecture': self.arch, 'pointer_bytes': 8}
    def kernel(self):
        return copy.deepcopy(self.context)
    def interpreter(self):
        return sha(b'independent-review-interpreter')
    def controllers(self):
        return {'cpu': True, 'memory': True, 'pids': True}
    def endpoint(self):
        return {'socket_present': True}
    def collector(self):
        return {'anchor': 'PRESENT_UNVERIFIED', 'code': 'PRESENT_UNVERIFIED'}


def response(body, framing='length', extra=b'', status=b'200 OK'):
    prefix = b'HTTP/1.1 ' + status + b'\r\nContent-Type: application/json\r\n' + extra
    if framing == 'length':
        return prefix + b'Content-Length: ' + str(len(body)).encode() + b'\r\n\r\n' + body
    if framing == 'chunked':
        # Intentionally split chunks across JSON/UTF-8 boundaries.
        chunks = b''.join(format(len(body[i:i+7]), 'x').encode() + b'\r\n' + body[i:i+7] + b'\r\n' for i in range(0, len(body), 7))
        return prefix + b'Transfer-Encoding: chunked\r\n\r\n' + chunks + b'0\r\n\r\n'
    return prefix + b'\r\n' + body


class NoEnvironment:
    # argparse/gettext may consult harmless locale/color controls. The metadata
    # reader must not consult config/proxy/credential/endpoint settings at all.
    allowed = {'LANGUAGE', 'LC_ALL', 'LC_MESSAGES', 'LANG', 'TERM', 'COLORTERM',
        'NO_COLOR', 'FORCE_COLOR', 'PYTHON_COLORS', 'COLUMNS', 'LINES'}
    def forbidden(self, *args, **kwargs):
        raise AssertionError('preflight transport consulted ambient environment')
    def get(self, key, default=None):
        if key not in self.allowed:
            self.forbidden()
        CLI_ENV_KEYS.add(key)
        return default
    def __getitem__(self, key):
        self.get(key)
        raise KeyError(key)
    def __contains__(self, key):
        self.get(key)
        return False
    __iter__ = items = keys = values = copy = forbidden


class ScriptedSocket:
    def __init__(self, wire, fragment=31, fault=None, clock=None, event_cost=None):
        self.wire, self.fragment, self.offset = wire, fragment, 0
        self.fault, self.clock, self.event_cost = fault, clock, event_cost or {}
        self.closed, self.address, self.sent = False, None, b''
        self.timeouts, self.received = [], 0
    def step(self, name):
        if self.clock is not None:
            self.clock[0] += self.event_cost.get(name, 0)
        if self.fault and self.fault[0] == name:
            raise self.fault[1]
    def __enter__(self):
        self.step('enter')
        return self
    def __exit__(self, *args):
        self.closed = True
        self.step('close')
    def settimeout(self, timeout):
        assert timeout > 0
        self.timeouts.append(timeout)
        self.step('settimeout')
    def connect(self, address):
        assert address == '/var/run/docker.sock'
        self.address = address
        self.step('connect')
    def sendall(self, raw):
        assert raw in tuple(('GET /'+op+' HTTP/1.1\r\nHost: localhost\r\nAccept: application/json\r\nConnection: close\r\n\r\n').encode() for op in ('info', 'version'))
        self.sent += raw
        self.step('sendall')
    def recv(self, count):
        assert count == 4096
        raw = self.wire[self.offset:self.offset + min(count, self.fragment)]
        self.offset += len(raw)
        self.received += len(raw)
        self.step('recv' if raw else 'eof')
        return raw


class ConnectedSocket:
    def __init__(self, connection):
        self.connection = connection
        self.closed = False
        self.address, self.sent = None, b''
    def __enter__(self):
        return self
    def __exit__(self, *args):
        self.connection.close()
        self.closed = True
    def connect(self, address):
        assert address == '/var/run/docker.sock'
        # Intentionally do not call connect: already an owned socketpair endpoint.
        self.address = address
    def settimeout(self, timeout):
        self.connection.settimeout(timeout)
    def sendall(self, raw):
        self.sent += raw
        self.connection.sendall(raw)
    def recv(self, count):
        return self.connection.recv(count)


@contextmanager
def guarded(sockets, used):
    pending = iter(sockets)
    def factory(*args, **kwargs):
        assert args == (socket.AF_UNIX, socket.SOCK_STREAM) and kwargs == {}
        value = next(pending)
        if isinstance(value, BaseException):
            raise value
        used.append(value)
        return value
    def forbidden(*args, **kwargs):
        raise AssertionError('forbidden process/file/DNS side effect')
    with ExitStack() as stack:
        stack.enter_context(patch.object(engine.socket, 'socket', side_effect=factory))
        stack.enter_context(patch.object(os, 'environ', NoEnvironment()))
        for target in ('builtins.open', 'io.open', 'os.open', 'socket.getaddrinfo', 'socket.gethostbyname',
                       'socket.create_connection', 'subprocess.Popen', 'os.system', 'os.fork', 'os.posix_spawn'):
            stack.enter_context(patch(target, side_effect=forbidden))
        yield


def run_main(sockets, arch='x86_64', deadline=None, probe=None):
    deadline = Deadline(2) if deadline is None else deadline
    probe = Probe(arch) if probe is None else probe
    used, output, errors = [], io.StringIO(), io.StringIO()
    with guarded(sockets, used), redirect_stdout(output), redirect_stderr(errors), \
            patch.object(hp, 'inspect_host', side_effect=lambda selected: REAL_INSPECT(selected, probe=probe, deadline=deadline)):
        code = hp.main(['--architecture', arch])
    result = json.loads(output.getvalue())
    assert errors.getvalue() == ''
    assert CANARY not in output.getvalue()
    assert len(output.getvalue().encode()) <= 8192
    assert not (result['host_admitted'] or result['runtime_evidence'] or result['execution_authorized'])
    assert result['native_trials_run'] == 0 and result['evidence_kind'] == 'simulated'
    assert all(item.closed for item in used)
    return code, result, used


def read_wire(raw, operation='info', fragment=31, deadline=None):
    item, used = ScriptedSocket(raw, fragment=fragment), []
    with guarded([item], used):
        try:
            return engine.read_engine_metadata(operation, Deadline(2) if deadline is None else deadline)
        finally:
            if used:
                assert item.closed


class IndependentTransportTests(unittest.TestCase):
    def test_01_both_architectures_all_framings_no_ambient_side_effects(self):
        cases = 0
        for arch in ('x86_64', 'aarch64'):
            for framing in ('length', 'chunked', 'close'):
                for fragment in (1, 7, 4096):
                    info, version = documents(arch)
                    info.update(HttpProxy='http://user:'+CANARY+'@invalid.test', RegistryConfig={'auth': CANARY}, PrivateDiagnostic=CANARY)
                    version['Components'] = [{'Details': {'CANARY': CANARY}}]
                    sockets = [ScriptedSocket(response(encode(doc), framing=framing), fragment=fragment) for doc in (info, version, info)]
                    with self.subTest(arch=arch, framing=framing, fragment=fragment):
                        code, result, used = run_main(sockets, arch)
                        self.assertEqual((code, result['status']), (0, 'HOST_CANDIDATE'))
                        self.assertEqual([item.sent.split(b' ')[1] for item in used], [b'/info', b'/version', b'/info'])
                        self.assertTrue(all(CANARY.encode() not in item.sent for item in used))
                        self.assertIn('daemon_kernel_namespace_pairing', result['pending'])
                        cases += 1
        EVENTS.append({'test': 'both_arch_all_framings', 'cases': cases, 'subprocess_file_sensitive_env_dns_canaries': 'no accesses'})

    def test_02_projection_discards_unselected_data_without_digest_or_drift(self):
        results = []
        for size in (1, 40000):
            info, version = documents()
            before, after = copy.deepcopy(info), copy.deepcopy(info)
            before['HttpProxy'], after['HttpProxy'] = CANARY*size, 'other-proxy-value'
            # Keep the full raw body inside the advertised 256-KiB cap.
            before['HttpProxy'] = before['HttpProxy'][:100000]
            before['unknown'] = {'opaque': '\ud800', 'nested': [False, None, 'unselected']}
            sockets = [ScriptedSocket(response(encode(doc)), 4096) for doc in (before, version, after)]
            code, result, _ = run_main(sockets)
            self.assertEqual(code, 0)
            results.append(result)
        self.assertEqual(results[0], results[1])
        EVENTS.append({'test': 'unselected_projection', 'same_report_despite_private_body_changes': True, 'large_private_field_bytes': 100000})

    def test_03_strict_request_allowlist_precedes_socket_creation(self):
        for value in ('Info', '/info', 'info?x=1', 'info\r\nX: bad', '../info', 'version#x', 'POST /containers/create', '', None, 1, True, [], {}):
            used = []
            with self.subTest(type=type(value).__name__), guarded([], used), self.assertRaises(Invalid):
                engine.read_engine_metadata(value, Deadline(1))
            self.assertEqual(used, [])

    def test_04_transport_fault_matrix_is_structured_redacted_and_closed(self):
        info, version = documents()
        count = 0
        for stage in ('settimeout', 'connect', 'sendall', 'recv', 'eof', 'close'):
            for error in (TimeoutError(CANARY), PermissionError(CANARY), ConnectionResetError(CANARY), BrokenPipeError(CANARY), InterruptedError(CANARY)):
                item = ScriptedSocket(response(encode(info)), 4096, fault=(stage, error))
                with self.subTest(stage=stage, kind=type(error).__name__):
                    code, result, used = run_main([item])
                    self.assertEqual((code, result['status']), (1, 'BLOCKED'))
                    self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
                    self.assertEqual(len(used), 1)
                    count += 1
        for error in (OSError(CANARY), TimeoutError(CANARY)):
            code, result, used = run_main([error])
            self.assertEqual((code, result['status']), (1, 'BLOCKED'))
            self.assertEqual(used, [])
            count += 1
        EVENTS.append({'test': 'transport_faults', 'cases': count, 'all_json_blocked_no_stderr': True})

    def test_05_late_connect_send_receive_and_eof_never_succeed(self):
        info, _ = documents()
        for stage in ('connect', 'sendall', 'recv', 'eof'):
            now = [0]
            item = ScriptedSocket(response(encode(info)), 4096, clock=now, event_cost={stage: 2})
            code, result, used = run_main([item], deadline=Deadline(1, clock=lambda: now[0]))
            self.assertEqual((code, result['status']), (1, 'BLOCKED'))
            self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
            self.assertTrue(used[0].closed)

    def test_06_three_observations_share_one_deadline(self):
        info, version = documents()
        now = [0]
        sockets = [ScriptedSocket(response(encode(doc)), 4096, clock=now, event_cost={'recv': 0.2, 'eof': 0.1}) for doc in (info, version, info)]
        code, result, used = run_main(sockets, deadline=Deadline(0.5, clock=lambda: now[0]))
        self.assertEqual((code, result['status']), (1, 'BLOCKED'))
        self.assertEqual(len(used), 2)
        self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
        self.assertLessEqual(max(used[1].timeouts), 0.2 + 1e-12)
        EVENTS.append({'test': 'aggregate_deadline', 'connections_attempted': len(used), 'simulated_elapsed': now[0], 'budget': 0.5})

    def test_07_ambiguous_framing_and_control_characters_block(self):
        info, version = documents()
        body = encode(info)
        header = b'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n'
        valid = response(body)
        corpus = [response(body, status=status) for status in (b'100 Continue', b'101 Switching Protocols', b'204 No Content', b'301 Moved', b'302 Found', b'307 Temporary Redirect', b'401 '+CANARY.encode(), b'500 '+CANARY.encode())]
        corpus += [response(body, extra=extra) for extra in (
            b'Content-Length: 1\r\n', b'cOnTeNt-LeNgTh: 1\r\n', b'Transfer-Encoding: chunked\r\n',
            b'Content-Encoding: gzip\r\n', b'Content-Encoding: br\r\n', b'Content-Encoding: identity, gzip\r\n',
            b'X: a\r\nx: b\r\n', b' X: folded\r\n', b'\tX: folded\r\n', b'X : bad\r\n', b'Bad\r\n',
            b'X: '+CANARY.encode()+b'\x00\r\n', b'X: bad\x7f\r\n', b'X: bad\xff\r\n')]
        corpus += [header+b'Content-Length: '+n+b'\r\n\r\n'+body for n in (b'-1', b'+2', b'2,2', b'2 2', b'1e2', b'9999999999')]
        corpus += [valid+b'X', valid+b'HTTP/1.1 200 OK\r\n\r\n', valid[:-1],
            header+b'Transfer-Encoding: chunked, gzip\r\n\r\n']
        # Separate additions avoid relying on production framing helpers.
        ch = header+b'Transfer-Encoding: chunked\r\n\r\n'
        corpus += [ch+chunks for chunks in (b'2;ignored=yes\r\n{}\r\n0\r\n\r\n', b'2\r\n{}\r\n0\r\nSecret: '+CANARY.encode()+b'\r\n\r\n', b'0\r\n\r\n', b'-1\r\n', b'+1\r\n', b'1 \r\nx\r\n0\r\n\r\n', b'fffffffff\r\n', b'1\r\nxXX0\r\n\r\n')]
        for i, raw in enumerate(corpus):
            with self.subTest(case=i):
                code, result, used = run_main([ScriptedSocket(raw, 13), ScriptedSocket(response(encode(version))), ScriptedSocket(response(body))])
                self.assertEqual((code, result['status']), (1, 'BLOCKED'))
                self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
                self.assertEqual(len(used), 1)
        EVENTS.append({'test': 'malformed_http_corpus', 'cases': len(corpus), 'all_blocked': True})

    def test_08_json_semantic_boundary_rejects_duplicates_numbers_and_encoding(self):
        info, version = documents()
        valid = encode(info)
        invalid = [b'null', b'[]', b'false', b'{}', valid+b'{}', b'\xff', valid.decode().encode('utf-16'),
            valid[:-1]+b',"ID":"other"}', valid[:-1]+b',"unknown":{"x":1,"x":2}}',
            valid[:-1]+b',"unknown":NaN}', valid[:-1]+b',"unknown":1.0}',
            valid.replace(b'"linux"', b'"\\ud800"', 1),
            valid.replace(b'"linux"', b'['*1500+b'0'+b']'*1500, 1)]
        for index, raw in enumerate(invalid):
            with self.subTest(case=index):
                code, result, _ = run_main([ScriptedSocket(response(raw), 4096), ScriptedSocket(response(encode(version))), ScriptedSocket(response(valid))])
                self.assertEqual((code, result['status']), (1, 'BLOCKED'))
                self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
        EVENTS.append({'test': 'malformed_json', 'cases': len(invalid), 'all_blocked': True})

    def test_09_selected_field_shape_matrix_and_missing_fields_block(self):
        info, version = documents()
        count = 0
        for key in info:
            for value in (None, {}, [], 'bad\n'+CANARY, '\ud800'):
                changed = copy.deepcopy(info)
                changed[key] = value
                with self.subTest(key=key, type=type(value).__name__):
                    code, result, _ = run_main([ScriptedSocket(response(encode(doc)), 4096) for doc in (changed, version, changed)])
                    self.assertEqual((code, result['status']), (1, 'BLOCKED'))
                    count += 1
            changed = copy.deepcopy(info)
            del changed[key]
            code, result, _ = run_main([ScriptedSocket(response(encode(changed)), 4096)])
            self.assertEqual((code, result['status']), (1, 'BLOCKED'))
            count += 1
        EVENTS.append({'test': 'selected_field_matrix', 'cases': count, 'all_blocked': True})

    def test_10_body_chunk_and_total_wire_bounds(self):
        # The transport returns bytes; JSON semantic validity is tested separately.
        for framing in ('length', 'chunked', 'close'):
            if framing == 'chunked':
                prefix = b'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n'
                wrap = lambda data: prefix+format(len(data), 'x').encode()+b'\r\n'+data+b'\r\n0\r\n\r\n'
            else:
                wrap = lambda data: response(data, framing)
            self.assertEqual(len(read_wire(wrap(b'x'*engine.MAX_BODY), fragment=4096)), engine.MAX_BODY)
            with self.assertRaises(Invalid):
                read_wire(wrap(b'x'*(engine.MAX_BODY+1)), fragment=4096)
        prefix = b'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n'
        self.assertEqual(len(read_wire(prefix+b'1\r\nx\r\n'*(engine.MAX_CHUNKS-1)+b'0\r\n\r\n', fragment=4096)), engine.MAX_CHUNKS-1)
        with self.assertRaises(Invalid):
            read_wire(prefix+b'1\r\nx\r\n'*engine.MAX_CHUNKS+b'0\r\n\r\n', fragment=4096)
        # Exercise the independent aggregate wire guard, not only body limits.
        item = ScriptedSocket(b'ab', 2)
        reader = engine.Response(item, Deadline(1))
        reader.wire = engine.MAX_WIRE-1
        with self.assertRaisesRegex(Invalid, '^ENGINE_WIRE_BOUND$'):
            reader.receive()
        EVENTS.append({'test': 'byte_bounds', 'body': engine.MAX_BODY, 'wire': engine.MAX_WIRE, 'chunk_iterations': engine.MAX_CHUNKS})

    def test_11_header_aggregate_and_field_count_exact_boundaries(self):
        base = b'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 2\r\nX: \r\n\r\n'
        wire = base.replace(b'X: \r\n', b'X: '+b'a'*(engine.MAX_HEADERS-len(base))+b'\r\n')+b'{}'
        self.assertEqual(read_wire(wire), b'{}')
        with self.assertRaises(Invalid):
            read_wire(wire.replace(b'X: ', b'X: a'))
        for fields, valid in ((62, True), (63, False)):
            extra = b''.join(b'X-'+str(i).encode()+b': ok\r\n' for i in range(fields))
            wire = response(b'{}', extra=extra)
            if valid:
                self.assertEqual(read_wire(wire), b'{}')
            else:
                with self.assertRaises(Invalid):
                    read_wire(wire)

    def test_12_no_new_privileges_is_exact_and_does_not_erase_mapping_checks(self):
        cases = 0
        for arch in ('x86_64', 'aarch64'):
            info, version = documents(arch)
            for token, expected in (
                ('name=rootless', 'DOCKER_NAMESPACE_MAPPING_UNSUPPORTED'),
                ('name=rootless,profile=default', 'DOCKER_NAMESPACE_MAPPING_UNSUPPORTED'),
                ('name=userns', 'DOCKER_NAMESPACE_MAPPING_UNSUPPORTED'),
                ('name=no-new-privileges', 'DOCKER_UNAVAILABLE'),
                ('name=no-new-privileges,profile=builtin', 'DOCKER_UNAVAILABLE'),
                ('name=unknown-hyphenated', 'DOCKER_UNAVAILABLE'),
                (' name=no-new-privileges', 'DOCKER_UNAVAILABLE')):
                changed = copy.deepcopy(info)
                changed['SecurityOptions'].append(token)
                code, result, _ = run_main([ScriptedSocket(response(encode(doc)), 4096) for doc in (changed, version, changed)], arch)
                self.assertEqual(code, 1)
                self.assertIn(expected, result['blockers'])
                cases += 1
        EVENTS.append({'test': 'COR02_regressions', 'cases': cases})

    def test_13_selected_daemon_drift_and_foreign_architecture_still_block(self):
        info, version = documents()
        for key, value in (('ID', 'replacement'), ('KernelVersion', '6.9-foreign'), ('Architecture', 'aarch64')):
            after = copy.deepcopy(info)
            after[key] = value
            code, result, _ = run_main([ScriptedSocket(response(encode(doc)), 4096) for doc in (info, version, after)])
            self.assertEqual(code, 1)
            self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
        version['Arch'] = 'arm64'
        code, result, _ = run_main([ScriptedSocket(response(encode(doc)), 4096) for doc in (info, version, info)])
        self.assertEqual(code, 1)
        self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])

    def test_14_seeded_wire_mutations_have_no_uncaught_errors_or_unclosed_socket(self):
        rng = random.Random(20261017)
        original = response(encode(documents()[0]))
        accepted = rejected = 0
        for _ in range(600):
            raw = bytearray(original)
            for _ in range(rng.randint(1, 4)):
                where = rng.randrange(len(raw))
                action = rng.randrange(3)
                if action == 0:
                    raw[where] = rng.randrange(256)
                elif action == 1:
                    del raw[where]
                else:
                    raw[where:where] = bytes([rng.randrange(256)])
            try:
                body = read_wire(bytes(raw), fragment=rng.choice((1, 5, 31, 4096)))
                self.assertTrue(0 < len(body) <= engine.MAX_BODY)
                accepted += 1
            except Invalid as error:
                self.assertTrue(str(error).startswith('ENGINE_'))
                rejected += 1
        EVENTS.append({'test': 'seeded_wire_mutation', 'seed': 20261017, 'cases': 600,
            'transport_accepted': accepted, 'transport_rejected': rejected,
            'note': 'Accepted wire mutations may be legal headers/body changes; semantic validity is separately enforced.'})

    def test_15_real_socket_slowloris_eof_and_keepopen_fail_as_json(self):
        info, _ = documents()
        full = response(encode(info))
        header, body = full.split(b'\r\n\r\n', 1)
        for mode in ('partial-status', 'trickle-headers', 'trickle-body', 'keep-open', 'truncated', 'extra-after-body', 'chunk-no-zero'):
            client, peer = REAL_SOCKETPAIR(socket.AF_UNIX, socket.SOCK_STREAM)
            item = ConnectedSocket(client)
            stop = threading.Event()
            state = {'mode': mode, 'request': None, 'server_error': None}
            def server():
                try:
                    with peer:
                        peer.settimeout(2)
                        request = b''
                        while b'\r\n\r\n' not in request:
                            raw = peer.recv(1024)
                            if not raw:
                                break
                            request += raw
                            if len(request) > 1024:
                                raise AssertionError('unbounded fixture request')
                        state['request'] = request.decode('ascii')
                        if mode == 'partial-status':
                            peer.sendall(b'HTT')
                            stop.wait(2)
                        elif mode == 'trickle-headers':
                            for byte in header:
                                peer.sendall(bytes([byte]))
                                if stop.wait(0.02):
                                    break
                        elif mode == 'trickle-body':
                            peer.sendall(header+b'\r\n\r\n')
                            for byte in body:
                                peer.sendall(bytes([byte]))
                                if stop.wait(0.02):
                                    break
                        elif mode == 'keep-open':
                            peer.sendall(full)
                            stop.wait(2)
                        elif mode == 'truncated':
                            peer.sendall(full[:-1])
                        elif mode == 'extra-after-body':
                            peer.sendall(full+b'X')
                        else:
                            peer.sendall(response(encode(info), 'chunked')[:-5])
                except (BrokenPipeError, ConnectionResetError):
                    pass  # Expected client refusal while fixture is sending.
                except BaseException as error:
                    state['server_error'] = type(error).__name__
            thread = threading.Thread(target=server, name='owned-preflight-security-peer')
            thread.start()
            start = time.monotonic()
            try:
                with self.subTest(mode=mode):
                    code, result, used = run_main([item], deadline=Deadline(0.15))
                    elapsed = time.monotonic()-start
                    self.assertEqual((code, result['status']), (1, 'BLOCKED'))
                    self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
                    self.assertEqual(client.fileno(), -1)
                    self.assertLess(elapsed, 1.0)
                    state.update(elapsed_seconds=round(elapsed, 4), status=result['status'], client_fd_closed=True)
            finally:
                stop.set()
                client.close()
                thread.join(3)
                peer.close()
            self.assertFalse(thread.is_alive())
            self.assertIsNone(state['server_error'])
            self.assertEqual(state['request'], 'GET /info HTTP/1.1\r\nHost: localhost\r\nAccept: application/json\r\nConnection: close\r\n\r\n')
            state['peer_thread_joined'] = True
            EVENTS.append({'test': 'real_owned_socketpair', **state})

    def test_16_real_success_and_failure_repeated_without_fd_leak(self):
        before = len(os.listdir('/proc/self/fd'))
        for index in range(80):
            client, peer = REAL_SOCKETPAIR(socket.AF_UNIX, socket.SOCK_STREAM)
            item = ConnectedSocket(client)
            wire = response(b'{}')
            if index % 2:
                wire = wire[:-1]
            try:
                peer.sendall(wire)
                peer.shutdown(socket.SHUT_WR)
                used = []
                with guarded([item], used):
                    if index % 2:
                        with self.assertRaises(Invalid):
                            engine.read_engine_metadata('info', Deadline(1))
                    else:
                        self.assertEqual(engine.read_engine_metadata('info', Deadline(1)), b'{}')
                self.assertTrue(item.closed)
                self.assertEqual(client.fileno(), -1)
            finally:
                client.close()
                peer.close()
        after = len(os.listdir('/proc/self/fd'))
        self.assertEqual(after, before)
        EVENTS.append({'test': 'fd_repetition', 'socketpairs': 80, 'fds_before': before, 'fds_after': after})


if __name__ == '__main__':
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(IndependentTransportTests))
    report = {'kind': 'independent_preflight_transport_security_controls', 'tests_run': result.testsRun,
        'status': 'PASS' if result.wasSuccessful() else 'FAIL', 'failures': [test.id() for test, _ in result.failures],
        'errors': [test.id() for test, _ in result.errors], 'skipped': result.skipped, 'events': EVENTS,
        'harmless_cli_locale_color_environment_keys_consulted': sorted(CLI_ENV_KEYS),
        'real_docker_ssh_ptrace_native_actors_run': False, 'source_modified': False}
    with (OUT/'independent-original.json').open('x') as output:
        json.dump(report, output, indent=2, sort_keys=True)
        output.write('\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
