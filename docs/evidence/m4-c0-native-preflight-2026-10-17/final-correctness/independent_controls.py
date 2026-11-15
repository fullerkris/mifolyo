"""Independent HTTP/projection/deadline controls; fake peers and socketpairs only.

Never connects to a filesystem socket, starts a child, or reads client config.
All source imports are bound to the new frozen transport snapshot.
"""
import copy
import io
import json
import os
from pathlib import Path
import socket
import stat
import sys
import threading
import time
import unittest
from unittest.mock import patch

ROOT = Path('/tmp/opencode/c0-native-preflight-2026-10-17')
OUT = ROOT / 'transport-correctness'
SOURCE = ROOT / 'transport-snapshot/tests/crawl-jobs-v2-observer'
sys.dont_write_bytecode = True
sys.path.insert(0, str(SOURCE))

import engine_metadata as engine
import host_preflight as host
from contracts import Invalid, canonical
from streams import Deadline
from test_host_preflight import FixtureProbe

EVIDENCE = []


def frame(body, framing='length', extra=()):
    lines = [b'HTTP/1.1 200 OK', b'Content-Type: application/json', *extra]
    if framing == 'length':
        lines.append(b'Content-Length: ' + str(len(body)).encode())
        payload = body
    elif framing == 'chunked':
        lines.append(b'Transfer-Encoding: chunked')
        # Independent multi-chunk encoding, not the supplied one-chunk helper.
        parts = [body[i:i + 11] for i in range(0, len(body), 11)]
        payload = b''.join(format(len(part), 'x').encode() + b'\r\n' + part + b'\r\n' for part in parts) + b'0\r\n\r\n'
    else:
        payload = body
    return b'\r\n'.join(lines) + b'\r\n\r\n' + payload


class Peer:
    def __init__(self, raw, split=None):
        self.parts = [raw] if split is None else [raw[:split], raw[split:]]
        self.parts = [part for part in self.parts if part]
        self.closed = False
        self.address = None
        self.request = None
        self.timeouts = []
        self.receives = 0
        self.hook = lambda event: None

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.closed = True

    def settimeout(self, seconds):
        assert seconds > 0
        self.timeouts.append(seconds)

    def connect(self, address):
        self.address = address
        self.hook('connect')

    def sendall(self, request):
        self.request = request
        self.hook('sendall')

    def recv(self, size):
        assert size == 4096
        self.receives += 1
        if not self.parts:
            self.hook('eof')
            return b''
        result = self.parts[0][:size]
        self.parts[0] = self.parts[0][size:]
        if not self.parts[0]:
            self.parts.pop(0)
        self.hook('recv')
        return result


def api_documents(probe):
    info = {source: probe.info[key] for key, source in host.INFO_FIELDS.items()}
    version = {source: probe.server[key] for key, source in host.VERSION_FIELDS.items()}
    return [copy.deepcopy(info), version, copy.deepcopy(info)]


class TransportControls(unittest.TestCase):
    def no_authority(self, result):
        self.assertIs(result['read_only'], True)
        for field in ('host_admitted', 'execution_authorized', 'runtime_evidence'):
            self.assertIs(result[field], False)
        self.assertEqual(result['native_trials_run'], 0)
        self.assertEqual(result['pending'], list(host.PENDING))
        self.assertEqual(result['evidence_kind'], 'simulated')
        self.assertLessEqual(len(canonical(result)), 8192)

    def read_peer(self, peer, operation='info', deadline=None):
        with patch('engine_metadata.socket.socket', return_value=peer) as factory:
            try:
                return engine.read_engine_metadata(operation, Deadline(2) if deadline is None else deadline)
            finally:
                self.assertTrue(peer.closed)
                factory.assert_called_once_with(socket.AF_UNIX, socket.SOCK_STREAM)
                self.assertEqual(peer.address, '/var/run/docker.sock')

    def inspect_responses(self, arch, documents, framing='length', *, deadline=None, hook=None, cli=False):
        probe = FixtureProbe(arch)
        probe.docker = host.HostProbe(arch).docker
        peers = [Peer(frame(doc if type(doc) is bytes else canonical(doc), framing)) for doc in documents]
        opened = []
        def factory(family, kind):
            self.assertEqual((family, kind), (socket.AF_UNIX, socket.SOCK_STREAM))
            peer = peers[len(opened)]
            if hook:
                peer.hook = hook
            opened.append(peer)
            return peer
        output, errors = io.StringIO(), io.StringIO()
        real_inspect = host.inspect_host
        def inspect(architecture):
            return real_inspect(architecture, probe=probe, deadline=Deadline(2) if deadline is None else deadline)
        with patch('engine_metadata.socket.socket', side_effect=factory), \
                patch('subprocess.Popen', side_effect=AssertionError('no child permitted')), \
                patch('socket.getaddrinfo', side_effect=AssertionError('no DNS permitted')):
            if cli:
                with patch('host_preflight.inspect_host', side_effect=inspect), patch('sys.stdout', output), patch('sys.stderr', errors):
                    code = host.main(['--architecture', arch])
                result = json.loads(output.getvalue())
                self.assertEqual(code, 0 if result['status'] == 'HOST_CANDIDATE' else 1)
                self.assertEqual(errors.getvalue(), '')
            else:
                result = inspect(arch)
        self.assertTrue(all(peer.closed for peer in opened))
        for peer, operation in zip(opened, ('info', 'version', 'info')):
            self.assertEqual(peer.request, ('GET /' + operation + ' HTTP/1.1\r\nHost: localhost\r\nAccept: application/json\r\nConnection: close\r\n\r\n').encode())
        self.no_authority(result)
        return result, opened

    def test_every_split_preserves_each_framing(self):
        body = b'{"value":"\xc3\xa9","number":7}'
        for framing in ('length', 'chunked', 'close'):
            raw = frame(body, framing)
            for split in range(len(raw) + 1):
                with self.subTest(framing=framing, split=split):
                    self.assertEqual(self.read_peer(Peer(raw, split)), body)

    def test_every_truncation_of_framed_response_rejects(self):
        for framing in ('length', 'chunked'):
            raw = frame(b'{"a":true}', framing)
            for length in range(len(raw)):
                with self.subTest(framing=framing, length=length), self.assertRaises(Invalid):
                    self.read_peer(Peer(raw[:length]))

    def test_header_count_and_wire_accounting_exact_boundaries(self):
        # Content-Type/Content-Length consume two of the 64 available fields.
        for count in (62, 63):
            raw = frame(b'{}', extra=[b'X-' + str(i).encode() + b': a' for i in range(count)])
            if count == 62:
                self.assertEqual(self.read_peer(Peer(raw)), b'{}')
            else:
                with self.assertRaises(Invalid):
                    self.read_peer(Peer(raw))
        response = engine.Response(Peer(b'xy'), Deadline(1))
        response.wire = engine.MAX_WIRE - 2
        self.assertTrue(response.receive())
        self.assertEqual(response.wire, engine.MAX_WIRE)
        response.connection = Peer(b'z')
        with self.assertRaisesRegex(Invalid, '^ENGINE_WIRE_BOUND$'):
            response.receive()

    def test_framing_ambiguities_and_pipelined_responses_reject(self):
        raw = frame(b'{}')
        for malformed in (
            raw + raw, raw + b'\r\n',
            raw.replace(b'Content-Length: 2', b'Content-Length: 2\r\nTransfer-Encoding: identity'),
            raw.replace(b'Content-Length: 2', b'Content-Length: 2\r\nContent-Length: 2'),
            raw.replace(b'Content-Length: 2', b'Content-Length : 2'),
            raw.replace(b'Content-Length: 2', b'Content-Length: 2\x0b'),
            raw.replace(b'HTTP/1.1 200 OK', b'HTTP/2.0 200 OK'),
            frame(b'{}', 'chunked').replace(b'0\r\n\r\n', b'0\r\nX-Private: PRIVATE_CANARY\r\n\r\n'),
            frame(b'{}', 'chunked') + raw,
        ):
            with self.subTest(raw=repr(malformed[:100])), self.assertRaises(Invalid) as caught:
                self.read_peer(Peer(malformed))
            self.assertNotIn('PRIVATE_CANARY', str(caught.exception))

    def test_expiry_at_connect_send_receive_or_eof_closes_socket(self):
        for event in ('connect', 'sendall', 'recv', 'eof'):
            with self.subTest(event=event):
                now = [0]
                deadline = Deadline(1, clock=lambda: now[0])
                peer = Peer(frame(b'{}'))
                def expire(at):
                    if at == event:
                        now[0] = 1
                peer.hook = expire
                with self.assertRaises(Invalid):
                    self.read_peer(peer, deadline=deadline)

    def test_expired_deadline_does_not_allocate_socket(self):
        now = [0]
        deadline = Deadline(1, clock=lambda: now[0])
        now[0] = 1
        with patch('engine_metadata.socket.socket') as factory, self.assertRaises(Invalid):
            engine.read_engine_metadata('info', deadline)
        factory.assert_not_called()

    def test_three_requests_share_one_deadline(self):
        for arch in ('aarch64', 'x86_64'):
            now = [0]
            deadline = Deadline(1, clock=lambda: now[0])
            def elapsed(event):
                if event in ('recv', 'eof'):
                    now[0] += 0.25
            result, peers = self.inspect_responses(arch, api_documents(FixtureProbe(arch)), deadline=deadline, hook=elapsed, cli=True)
            self.assertEqual(result['status'], 'BLOCKED')
            self.assertIn('DOCKER_UNAVAILABLE', result['blockers'])
            self.assertEqual(len(peers), 2)
            timeouts = [value for peer in peers for value in peer.timeouts]
            self.assertEqual(timeouts, sorted(timeouts, reverse=True))
            EVIDENCE.append({'control': 'shared_deadline', 'architecture': arch, 'requests': len(peers), 'remaining_timeouts': timeouts, 'status': result['status']})

    def test_api_metadata_both_architectures_and_all_framings(self):
        for arch in ('aarch64', 'x86_64'):
            for framing in ('length', 'chunked', 'close'):
                result, peers = self.inspect_responses(arch, api_documents(FixtureProbe(arch)), framing, cli=True)
                self.assertEqual(result['status'], 'HOST_CANDIDATE')
                self.assertEqual(len(peers), 3)
                EVIDENCE.append({'control': 'api_integration', 'architecture': arch, 'framing': framing, 'status': result['status'], 'requests': 3, 'sockets_closed': True})

    def test_private_unselected_metadata_does_not_change_exported_report(self):
        for arch in ('aarch64', 'x86_64'):
            docs = api_documents(FixtureProbe(arch))
            baseline, _ = self.inspect_responses(arch, docs)
            docs[0]['HttpProxy'] = 'PRIVATE_CANARY:before'
            docs[2]['HttpProxy'] = 'PRIVATE_CANARY:after'
            for doc in docs:
                doc['RegistryConfig'] = {'credential': 'PRIVATE_CANARY'}
            environment = {key: 'PRIVATE_CANARY' for key in ('HOME', 'DOCKER_CONFIG', 'DOCKER_HOST', 'DOCKER_CONTEXT', 'DOCKER_CUSTOM_HEADERS', 'HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY')}
            with patch.dict(os.environ, environment, clear=True), \
                    patch('builtins.open', side_effect=AssertionError('no config read')), \
                    patch('os.open', side_effect=AssertionError('no credential read')):
                result, peers = self.inspect_responses(arch, docs)
            self.assertEqual(canonical(result), canonical(baseline))
            self.assertNotIn(b'PRIVATE_CANARY', canonical(result))
            self.assertTrue(all(b'PRIVATE_CANARY' not in peer.request for peer in peers))

    def test_api_body_and_projected_metadata_bounds(self):
        docs = api_documents(FixtureProbe())
        large = dict(docs[0], Unselected='')
        large['Unselected'] = 'x' * (engine.MAX_BODY - len(canonical(large)))
        self.assertEqual(len(canonical(large)), engine.MAX_BODY)
        result, _ = self.inspect_responses('x86_64', [large, docs[1], large])
        self.assertEqual(result['status'], 'HOST_CANDIDATE')
        large['Unselected'] += 'x'
        result, _ = self.inspect_responses('x86_64', [large, docs[1], large])
        self.assertEqual(result['blockers'], ['DOCKER_UNAVAILABLE'])
        selected = copy.deepcopy(docs)
        selected[0]['ID'] = 'x' * 16384
        result, _ = self.inspect_responses('x86_64', selected)
        self.assertEqual(result['blockers'], ['DOCKER_UNAVAILABLE'])

    def test_api_json_schema_duplicates_encoding_and_wire_casing(self):
        for arch in ('aarch64', 'x86_64'):
            docs = api_documents(FixtureProbe(arch))
            raw = canonical(docs[0])
            bad = [raw + b'{}', b'\xff', raw.decode().encode('utf-16'), b'[]',
                   raw.replace(b'"CpuCfsQuota":true', b'"CpuCfsQuota":true,"CpuCfsQuota":true'),
                   raw.replace(b'"CpuCfsQuota":true', b'"CPUCfsQuota":true'),
                   raw.replace(b'"CpuCfsQuota":true', b'"CpuCfsQuota":1'),
                   raw.replace(b'"CpuCfsQuota":true', b'"CpuCfsQuota":1.0'),
                   raw.replace(b'"CpuCfsQuota":true', b'"CpuCfsQuota":NaN'),
                   raw.rstrip()[:-1] + b',"Ignored":{"x":1,"x":2}}']
            for body in bad:
                with self.subTest(architecture=arch, prefix=repr(body[:60])):
                    result, _ = self.inspect_responses(arch, [body, docs[1], docs[2]], cli=True)
                    self.assertEqual(result['blockers'], ['DOCKER_UNAVAILABLE'])

    def test_selected_field_drift_still_blocks_after_projection(self):
        for arch in ('aarch64', 'x86_64'):
            for source in host.INFO_FIELDS.values():
                docs = api_documents(FixtureProbe(arch))
                docs[2][source] = 'changed'
                result, _ = self.inspect_responses(arch, docs)
                self.assertEqual(result['blockers'], ['DOCKER_UNAVAILABLE'])

    def test_nnp_exact_token_keeps_namespace_and_duplicate_checks(self):
        for arch in ('aarch64', 'x86_64'):
            for option in (None, 'name=rootless', 'name=userns', 'name=no-new-privileges', 'name=unknown-hyphenated', 'name=no-new-privileges,profile=builtin'):
                probe = FixtureProbe(arch)
                probe.info['security_options'].append('name=no-new-privileges')
                if option is not None:
                    probe.info['security_options'].append(option)
                result, _ = self.inspect_responses(arch, api_documents(probe))
                expected = [] if option is None else ['DOCKER_NAMESPACE_MAPPING_UNSUPPORTED'] if option in ('name=rootless', 'name=userns') else ['DOCKER_UNAVAILABLE']
                self.assertEqual(result['blockers'], expected)

    def test_endpoint_no_longer_requires_cli_installation(self):
        class Info:
            st_mode = stat.S_IFSOCK | 0o600
        with patch('host_preflight.os.stat', return_value=Info()) as stat_call, \
                patch('os.access', side_effect=AssertionError('no CLI executable check')):
            self.assertEqual(host.HostProbe('x86_64').endpoint(), {'socket_present': True})
        stat_call.assert_called_once_with('/var/run/docker.sock')

    def test_real_socketpairs_full_cli_flow_and_late_eof(self):
        for arch in ('aarch64', 'x86_64'):
            for mode in ('success', 'truncated', 'keep-open'):
                probe = FixtureProbe(arch)
                probe.docker = host.HostProbe(arch).docker
                docs = api_documents(probe)
                pairs = [socket.socketpair() for _ in range(3)]
                stop = threading.Event()
                threads, errors, requests, addresses, clients = [], [], [], [], []
                def factory(family, kind):
                    self.assertEqual((family, kind), (socket.AF_UNIX, socket.SOCK_STREAM))
                    index = len(clients)
                    client, server = pairs[index]
                    clients.append(client)
                    class Connected:
                        def __enter__(self):
                            return self
                        def __exit__(self, *args):
                            client.close()
                        def connect(self, address):
                            addresses.append(address)
                        def settimeout(self, value):
                            client.settimeout(value)
                        def sendall(self, value):
                            client.sendall(value)
                        def recv(self, size):
                            return client.recv(size)
                    def serve():
                        try:
                            with server:
                                server.settimeout(2)
                                request = b''
                                while not request.endswith(b'\r\n\r\n'):
                                    part = server.recv(1024)
                                    if not part:
                                        return
                                    request += part
                                    assert len(request) < 1024
                                requests.append(request)
                                raw = frame(canonical(docs[index]), 'chunked')
                                server.sendall(raw[:-2] if mode == 'truncated' else raw)
                                if mode == 'keep-open':
                                    stop.wait(2)
                        except BaseException as error:
                            errors.append(type(error).__name__)
                    thread = threading.Thread(target=serve)
                    thread.start()
                    threads.append(thread)
                    return Connected()
                real_inspect = host.inspect_host
                def inspected(architecture):
                    return real_inspect(architecture, probe=probe, deadline=Deadline(0.2))
                output, stderr = io.StringIO(), io.StringIO()
                start = time.monotonic()
                try:
                    with patch('engine_metadata.socket.socket', side_effect=factory), \
                            patch('host_preflight.inspect_host', side_effect=inspected), \
                            patch('sys.stdout', output), patch('sys.stderr', stderr), \
                            patch('subprocess.Popen', side_effect=AssertionError('no child')):
                        code = host.main(['--architecture', arch])
                    result = json.loads(output.getvalue())
                    self.no_authority(result)
                    self.assertEqual(code, 0 if mode == 'success' else 1)
                    self.assertEqual(result['status'], 'HOST_CANDIDATE' if mode == 'success' else 'BLOCKED')
                    self.assertEqual(stderr.getvalue(), '')
                    self.assertTrue(all(client.fileno() == -1 for client in clients))
                    self.assertEqual(len(clients), 3 if mode == 'success' else 1)
                    self.assertTrue(all(address == '/var/run/docker.sock' for address in addresses))
                    elapsed = time.monotonic() - start
                    self.assertLess(elapsed, 1.5)
                    EVIDENCE.append({'control': 'real_socketpairs_cli', 'architecture': arch, 'mode': mode,
                        'status': result['status'], 'exit_status': code, 'elapsed_seconds': round(elapsed, 6),
                        'requests': len(clients), 'owned_sockets_closed': True, 'stderr_empty': True})
                finally:
                    stop.set()
                    for client, server in pairs:
                        client.close()
                    for thread in threads:
                        thread.join(3)
                    for client, server in pairs:
                        server.close()
                self.assertFalse(any(thread.is_alive() for thread in threads))
                self.assertEqual(errors, [])
                for request, operation in zip(requests, ('info', 'version', 'info')):
                    self.assertEqual(request, ('GET /' + operation + ' HTTP/1.1\r\nHost: localhost\r\nAccept: application/json\r\nConnection: close\r\n\r\n').encode())


if __name__ == '__main__':
    assert Path(engine.__file__).resolve() == SOURCE / 'engine_metadata.py'
    assert Path(host.__file__).resolve() == SOURCE / 'host_preflight.py'
    program = unittest.main(verbosity=2, exit=False)
    evidence = {'tests_run': program.result.testsRun, 'passed': program.result.wasSuccessful(),
        'failures': [test.id() for test, _ in program.result.failures],
        'errors': [test.id() for test, _ in program.result.errors],
        'skips': [test.id() for test, _ in program.result.skipped], 'observations': EVIDENCE}
    (OUT / 'independent-evidence.json').write_text(json.dumps(evidence, indent=2) + '\n')
    raise SystemExit(0 if program.result.wasSuccessful() else 1)
