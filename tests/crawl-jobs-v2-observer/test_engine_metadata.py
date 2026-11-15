"""Plugin/config canaries and bounded fake-engine Unix/HTTP controls."""
import os
import socket
import threading
import unittest
from unittest.mock import patch

from contracts import Invalid, canonical
from engine_metadata import ENGINE_SOCKET, MAX_BODY, MAX_CHUNKS, MAX_HEADERS, read_engine_metadata
from host_preflight import HostProbe, INFO_FIELDS, VERSION_FIELDS, inspect_host
from streams import Deadline
from test_host_preflight import FixtureProbe


def response(body=b"{}", *, framing="length", extra=b"", status=b"200 OK"):
    headers = b"HTTP/1.1 " + status + b"\r\nContent-Type: application/json\r\n" + extra
    if framing == "length":
        return headers + b"Content-Length: " + str(len(body)).encode() + b"\r\n\r\n" + body
    if framing == "chunked":
        return headers + b"Transfer-Encoding: chunked\r\n\r\n" + format(len(body), "x").encode() + b"\r\n" + body + b"\r\n0\r\n\r\n"
    return headers + b"\r\n" + body


class FakeSocket:
    def __init__(self, raw, fragment=4096):
        self.raw, self.fragment, self.offset = raw, fragment, 0
        self.closed, self.address, self.request = False, None, None
        self.timeouts = []

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.closed = True

    def settimeout(self, value):
        self.timeouts.append(value)

    def connect(self, address):
        self.address = address

    def sendall(self, raw):
        self.request = raw

    def recv(self, count):
        result = self.raw[self.offset:self.offset + min(count, self.fragment)]
        self.offset += len(result)
        return result


class EngineMetadataTests(unittest.TestCase):
    def read(self, raw, operation="info", fragment=4096):
        connection = FakeSocket(raw, fragment)
        try:
            with patch("engine_metadata.socket.socket", return_value=connection) as factory:
                result = read_engine_metadata(operation, Deadline(2))
            factory.assert_called_once_with(socket.AF_UNIX, socket.SOCK_STREAM)
            self.assertEqual(connection.address, ENGINE_SOCKET)
            self.assertEqual(connection.request, ("GET /" + operation + " HTTP/1.1\r\nHost: localhost\r\n"
                "Accept: application/json\r\nConnection: close\r\n\r\n").encode())
            self.assertTrue(all(0 < timeout <= 2 for timeout in connection.timeouts))
            return result
        finally:
            self.assertTrue(connection.closed)

    def test_only_fixed_read_only_operations_and_endpoint(self):
        for operation in ("info", "version"):
            self.assertEqual(self.read(response(), operation), b"{}")
        for operation in ("POST /containers/create", "info?x=1", "../info", "info\r\nX: y", "run", None):
            with patch("engine_metadata.socket.socket") as factory, self.assertRaises(Invalid):
                read_engine_metadata(operation, Deadline(1))
            factory.assert_not_called()

    def test_fragmented_content_length_chunked_and_close_delimited(self):
        for framing in ("length", "chunked", "close"):
            for fragment in (1, 3, 4096):
                self.assertEqual(self.read(response(b'{"a":true}', framing=framing), fragment=fragment), b'{"a":true}')

    def test_no_configuration_plugins_processes_or_proxy_environment(self):
        for arch in ("x86_64", "aarch64"):
            fixture = FixtureProbe(arch)
            fixture.docker = HostProbe(arch).docker
            info = {source: fixture.info[key] for key, source in INFO_FIELDS.items()}
            version = {source: fixture.server[key] for key, source in VERSION_FIELDS.items()}
            info.update(HttpProxy="PRIVATE_CANARY", RegistryConfig={"auth": "PRIVATE_CANARY"})
            connections = [FakeSocket(response(canonical(value))) for value in (info, version, info)]
            environment = {name: "PRIVATE_CANARY" for name in ("HOME", "DOCKER_CONFIG", "DOCKER_HOST", "DOCKER_CLI_HOOKS",
                "DOCKER_CUSTOM_HEADERS", "HTTP_PROXY", "HTTPS_PROXY", "LD_PRELOAD", "OTEL_EXPORTER_OTLP_ENDPOINT")}
            with patch.dict(os.environ, environment, clear=True), \
                    patch("engine_metadata.socket.socket", side_effect=connections), \
                    patch("socket.getaddrinfo", side_effect=AssertionError("DNS forbidden")), \
                    patch("builtins.open", side_effect=AssertionError("config read forbidden")), \
                    patch("os.open", side_effect=AssertionError("credential read forbidden")), \
                    patch("subprocess.Popen", side_effect=AssertionError("plugin/child forbidden")) as popen:
                result = inspect_host(arch, probe=fixture)
            self.assertEqual(result["status"], "HOST_CANDIDATE")
            self.assertNotIn(b"PRIVATE_CANARY", canonical(result))
            self.assertTrue(all(connection.closed and b"PRIVATE_CANARY" not in connection.request for connection in connections))
            popen.assert_not_called()

    def test_actual_api_cpu_field_names(self):
        # API wire names differ from the former CLI Go-template field names.
        document = {"ID": "fixture-daemon", "OSType": "linux", "Architecture": "x86_64", "ServerVersion": "29.9.1",
            "KernelVersion": "6.8.0-fixture", "CgroupVersion": "2", "CgroupDriver": "systemd",
            "SecurityOptions": ["name=seccomp,profile=builtin", "name=cgroupns"],
            "MemoryLimit": True, "SwapLimit": True, "PidsLimit": True, "CpuCfsPeriod": False, "CpuCfsQuota": True}
        connection = FakeSocket(response(canonical(document)))
        with patch("engine_metadata.socket.socket", return_value=connection):
            value = HostProbe("x86_64").docker("info", Deadline(1))
        self.assertIs(value["cpu_period"], False)
        self.assertIs(value["cpu_quota"], True)
        self.assertTrue(connection.closed)

    def test_non_success_redirect_upgrade_and_raw_error_bodies_reject(self):
        for status in (b"500 PRIVATE_CANARY", b"302 Found", b"101 Switching Protocols", b"100 Continue", b"204 No Content"):
            with self.assertRaises(Invalid) as caught:
                self.read(response(b"PRIVATE_CANARY", status=status))
            self.assertNotIn("PRIVATE_CANARY", str(caught.exception))

    def test_conflicting_duplicate_or_invalid_headers_reject(self):
        extras = (b"Content-Length: 2\r\n", b"content-length: 2\r\n", b"Transfer-Encoding: chunked\r\n",
            b"Content-Encoding: gzip\r\n", b"Content-Type: text/html\r\n", b" Folded: bad\r\n", b"MissingColon\r\n",
            b"X-Test: bad\x00value\r\n", b"X-Test: ok\r\nx-test: again\r\n")
        for extra in extras:
            with self.subTest(extra=extra), self.assertRaises(Invalid):
                self.read(response(extra=extra))
        for length in (b"-1", b"+2", b"2, 2", b"1.0", b"99999999999"):
            with self.assertRaises(Invalid):
                self.read(b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: " + length + b"\r\n\r\n{}")

    def test_missing_content_type_or_unsupported_transfer_rejects(self):
        for raw in (b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n{}",
                response(framing="chunked").replace(b"chunked", b"gzip, chunked"),
                response().replace(b"application/json", b"text/html")):
            with self.assertRaises(Invalid):
                self.read(raw)

    def test_header_byte_count_and_line_bounds(self):
        base = response(extra=b"X-Padding: \r\n")
        count = base.index(b"\r\n\r\n") + 4
        exact = base.replace(b"X-Padding: \r\n", b"X-Padding: " + b"x" * (MAX_HEADERS - count) + b"\r\n")
        self.assertEqual(self.read(exact), b"{}")
        for raw in (exact.replace(b"X-Padding: ", b"X-Padding: x"), b"x" * (MAX_HEADERS + 2),
                response(extra=b"".join(b"X-" + str(index).encode() + b": v\r\n" for index in range(65)))):
            with self.assertRaises(Invalid):
                self.read(raw)

    def test_body_bound_in_each_framing(self):
        for framing in ("length", "chunked", "close"):
            with self.subTest(framing=framing):
                self.assertEqual(len(self.read(response(b"x" * MAX_BODY, framing=framing))), MAX_BODY)
                with self.assertRaises(Invalid):
                    self.read(response(b"x" * (MAX_BODY + 1), framing=framing))

    def test_truncated_or_extra_data_never_passes(self):
        complete = response()
        for raw in (complete[:-1], complete + b"PRIVATE_CANARY", response(framing="chunked")[:-2],
                b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 0\r\n\r\n"):
            with self.assertRaises(Invalid):
                self.read(raw)

    def test_chunk_syntax_extensions_trailers_and_count_are_bounded(self):
        header = b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n"
        for chunks in (b"-1\r\n", b"2;ext=x\r\n{}\r\n0\r\n\r\n", b"2\r\n{}xx0\r\n\r\n",
                b"2\r\n{}\r\n0\r\nX-Trailer: x\r\n\r\n", b"fffffffff\r\n", b"z\r\n"):
            with self.assertRaises(Invalid):
                self.read(header + chunks)
        self.assertEqual(len(self.read(header + b"1\r\nx\r\n" * (MAX_CHUNKS - 1) + b"0\r\n\r\n")), MAX_CHUNKS - 1)
        with self.assertRaises(Invalid):
            self.read(header + b"1\r\nx\r\n" * MAX_CHUNKS + b"0\r\n\r\n")

    def test_socket_errors_are_value_free_and_always_close(self):
        for method in ("connect", "sendall", "recv"):
            for error in (TimeoutError("PRIVATE_CANARY"), ConnectionResetError("PRIVATE_CANARY"), PermissionError("PRIVATE_CANARY")):
                connection = FakeSocket(response())
                with patch.object(connection, method, side_effect=error), \
                        patch("engine_metadata.socket.socket", return_value=connection), self.assertRaisesRegex(Invalid, "^ENGINE_TRANSPORT$"):
                    read_engine_metadata("info", Deadline(1))
                self.assertTrue(connection.closed)

    def test_shared_deadline_is_rechecked_after_receive(self):
        now = [0]
        deadline = Deadline(1, clock=lambda: now[0])
        connection = FakeSocket(response())
        original = connection.recv
        def expired(count):
            raw = original(count)
            now[0] = 2
            return raw
        with patch.object(connection, "recv", side_effect=expired), \
                patch("engine_metadata.socket.socket", return_value=connection), self.assertRaises(Invalid):
            read_engine_metadata("info", deadline)
        self.assertTrue(connection.closed)

    def test_real_owned_socket_eof_and_stall_are_bounded(self):
        for mode in ("success", "truncated", "stall", "keep-open"):
            with self.subTest(mode=mode):
                client, peer = socket.socketpair()
                stop = threading.Event()
                received, errors = [], []
                class Connected:
                    def __enter__(self):
                        return self
                    def __exit__(self, *args):
                        client.close()
                    def connect(self, address):
                        received.append(address)
                    def settimeout(self, timeout):
                        client.settimeout(timeout)
                    def sendall(self, raw):
                        client.sendall(raw)
                    def recv(self, count):
                        return client.recv(count)
                def serve():
                    try:
                        with peer:
                            peer.settimeout(2)
                            request = b""
                            while not request.endswith(b"\r\n\r\n"):
                                part = peer.recv(1024)
                                if not part:
                                    return
                                request += part
                            received.append(request)
                            if mode != "stall":
                                peer.sendall(response()[:-1] if mode == "truncated" else response())
                            if mode in ("stall", "keep-open"):
                                stop.wait(2)
                    except OSError as error:
                        errors.append(type(error).__name__)
                thread = threading.Thread(target=serve)
                thread.start()
                try:
                    with patch("engine_metadata.socket.socket", return_value=Connected()):
                        if mode == "success":
                            self.assertEqual(read_engine_metadata("info", Deadline(0.5)), b"{}")
                        else:
                            with self.assertRaises(Invalid):
                                read_engine_metadata("info", Deadline(0.5))
                    self.assertEqual(client.fileno(), -1)
                finally:
                    stop.set()
                    client.close()
                    thread.join(3)
                    peer.close()
                self.assertFalse(thread.is_alive())
                self.assertEqual(errors, [])
                self.assertEqual(received[0], ENGINE_SOCKET)
                self.assertTrue(received[1].startswith(b"GET /info HTTP/1.1\r\n"))


if __name__ == "__main__":
    unittest.main()
