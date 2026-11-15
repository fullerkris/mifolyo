"""Plugin-free, read-only Docker metadata at one fixed local Unix socket.

No CLI, client configuration, environment proxy, authentication or subprocess.
The response body stays private; callers export only checked selected fields.
"""
import re
import socket

from contracts import Invalid, require

ENGINE_SOCKET = "/var/run/docker.sock"
MAX_HEADERS = 8192
MAX_BODY = 262144
MAX_WIRE = MAX_HEADERS + MAX_BODY + 65536
MAX_CHUNKS = 4096


class Response:
    def __init__(self, connection, deadline):
        self.connection, self.deadline = connection, deadline
        self.pending = bytearray()
        self.wire = 0

    def receive(self):
        self.connection.settimeout(self.deadline.remaining())
        raw = self.connection.recv(4096)
        self.deadline.check()
        self.wire += len(raw)
        require(self.wire <= MAX_WIRE, "ENGINE_WIRE_BOUND")
        self.pending.extend(raw)
        return bool(raw)

    def line(self, limit):
        while True:
            end = self.pending.find(b"\r\n")
            if end >= 0:
                require(end <= limit, "ENGINE_LINE_BOUND")
                value = bytes(self.pending[:end])
                del self.pending[:end + 2]
                return value
            require(len(self.pending) <= limit + 1, "ENGINE_LINE_BOUND")
            require(self.receive(), "ENGINE_TRUNCATED")

    def take(self, count):
        require(0 <= count <= MAX_BODY, "ENGINE_BODY_BOUND")
        while len(self.pending) < count:
            require(self.receive(), "ENGINE_TRUNCATED")
        raw = bytes(self.pending[:count])
        del self.pending[:count]
        return raw

    def eof(self):
        require(not self.pending and not self.receive(), "ENGINE_EXTRA_DATA")

    def body(self):
        status = self.line(1024)
        require(re.fullmatch(rb"HTTP/1\.[01] 200(?: [\x20-\x7e]*)?", status) is not None, "ENGINE_STATUS")
        used, headers = len(status) + 2, {}
        while True:
            line = self.line(MAX_HEADERS)
            used += len(line) + 2
            require(used <= MAX_HEADERS, "ENGINE_HEADER_BOUND")
            if not line:
                break
            key, separator, value = line.partition(b":")
            require(separator and re.fullmatch(rb"[!#$%&'*+.^_`|~0-9A-Za-z-]+", key) is not None, "ENGINE_HEADER")
            require(all(char == 9 or 32 <= char <= 126 for char in value), "ENGINE_HEADER")
            key, value = key.lower(), value.strip(b" \t")
            require(key not in headers and len(headers) < 64, "ENGINE_HEADER")
            headers[key] = value
        require(headers.get(b"content-type", b"").split(b";", 1)[0].lower() == b"application/json", "ENGINE_CONTENT_TYPE")
        require(headers.get(b"content-encoding", b"identity").lower() == b"identity", "ENGINE_ENCODING")
        length, transfer = headers.get(b"content-length"), headers.get(b"transfer-encoding")
        require(length is None or transfer is None, "ENGINE_FRAMING")
        if length is not None:
            require(re.fullmatch(rb"[0-9]{1,9}", length) is not None and int(length) <= MAX_BODY, "ENGINE_BODY_BOUND")
            body = self.take(int(length))
            self.eof()
        elif transfer is not None:
            require(transfer.lower() == b"chunked", "ENGINE_FRAMING")
            parts, size = [], 0
            for _ in range(MAX_CHUNKS):
                line = self.line(16)
                require(re.fullmatch(rb"[0-9a-fA-F]{1,8}", line) is not None, "ENGINE_CHUNK")
                count = int(line, 16)
                require(size + count <= MAX_BODY, "ENGINE_BODY_BOUND")
                if count == 0:
                    # Docker metadata has no trailers. Do not accept an ambiguous
                    # second set of headers, extensions, upgrades or redirects.
                    require(self.line(MAX_HEADERS) == b"", "ENGINE_TRAILERS")
                    self.eof()
                    break
                parts.append(self.take(count))
                size += count
                require(self.take(2) == b"\r\n", "ENGINE_CHUNK")
            else:
                raise Invalid("ENGINE_CHUNK_BOUND")
            body = b"".join(parts)
        else:
            body = bytearray(self.pending)
            self.pending.clear()
            while self.receive():
                require(len(body) + len(self.pending) <= MAX_BODY, "ENGINE_BODY_BOUND")
                body.extend(self.pending)
                self.pending.clear()
            body = bytes(body)
        require(0 < len(body) <= MAX_BODY, "ENGINE_BODY_BOUND")
        self.deadline.check()
        return body


def read_engine_metadata(operation, deadline):
    require(type(operation) is str and operation in ("info", "version"), "ENGINE_OPERATION")
    deadline.check()
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
            connection.settimeout(deadline.remaining())
            connection.connect(ENGINE_SOCKET)
            connection.settimeout(deadline.remaining())
            connection.sendall(("GET /" + operation + " HTTP/1.1\r\nHost: localhost\r\n"
                "Accept: application/json\r\nConnection: close\r\n\r\n").encode("ascii"))
            deadline.check()
            return Response(connection, deadline).body()
    except OSError:
        # Includes socket timeout/reset/permission failures; never echo received
        # bytes, environment values, socket errors or raw daemon diagnostics.
        raise Invalid("ENGINE_TRANSPORT") from None
