"""Bounded RESP2 over one fixed Unix socket; no TCP, DNS, pooling or write retry."""
from __future__ import annotations

import re
import socket
import time

SOCKET = "/run/cj2/redis.sock"
MAX_REQUEST = 2 * 1024 * 1024
MAX_REPLY = 256 * 1024
MAX_ITEMS = 512


class TransportError(Exception):
    pass


class PeerDisconnected(TransportError):
    """Observed recv EOF/reset, never a timeout or a Redis error reply."""


class RedisError(Exception):
    def __init__(self, raw: bytes):
        # Raw server messages can echo submitted arguments. Never retain them.
        prefix = raw.split(b" ", 1)[0]
        self.code = prefix.decode("ascii") if prefix in (b"NOPERM", b"WRONGPASS", b"NOAUTH", b"NOSCRIPT") else "REDIS_ERROR"
        match = re.match(rb"ERR CRAWL_V2_([A-Z_]+)(?: |$)", raw)
        if match:
            self.code = "CRAWL_V2_" + match[1].decode("ascii")
        super().__init__(self.code)


def encode(parts: tuple) -> bytes:
    if not parts or len(parts) > 1024:
        raise TransportError("REQUEST_BOUNDS")
    result = bytearray(b"*" + str(len(parts)).encode() + b"\r\n")
    for part in parts:
        if type(part) is str:
            part = part.encode("utf-8")
        if type(part) is not bytes or len(part) > MAX_REQUEST:
            raise TransportError("REQUEST_BOUNDS")
        result.extend(b"$" + str(len(part)).encode() + b"\r\n" + part + b"\r\n")
        if len(result) > MAX_REQUEST:
            raise TransportError("REQUEST_BOUNDS")
    return bytes(result)


class Client:
    def __init__(self, *, connection=None, timeout=5.0):
        self.timeout = timeout
        self.sock = connection or socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.buffer = bytearray()
        try:
            self.sock.settimeout(timeout)
            if connection is None:
                self.sock.connect(SOCKET)
        except OSError as exc:
            self.close()
            raise TransportError("CONNECT_FAILED") from exc

    def close(self):
        self.sock.close()

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()

    def observe_peer_disconnect(self):
        """Bounded receive-only proof after revocation; do not PING a closed peer.

        Sending first can fail with EPIPE before the available EOF is read. Data,
        timeouts and local socket errors are not evidence of peer termination.
        """
        try:
            if self.buffer:
                raise TransportError("PEER_SENT_DATA")
            self.sock.settimeout(self.timeout)
            try:
                raw = self.sock.recv(1)
            except ConnectionResetError:
                return "reset"
            if raw != b"":
                raise TransportError("PEER_SENT_DATA")
            return "eof"
        except OSError as exc:
            raise TransportError("DISCONNECT_NOT_OBSERVED") from exc
        finally:
            self.close()

    def call(self, *parts):
        raw = encode(parts)
        self.remaining = MAX_REPLY
        self.items = 0
        self.deadline = time.monotonic() + self.timeout
        try:
            self.sock.settimeout(self.timeout)
            self.sock.sendall(raw)
            return self._value(0)
        except RedisError:
            raise
        except PeerDisconnected:
            self.close()
            raise
        except (OSError, ValueError, TransportError) as exc:
            # Outcome of a sent command is uncertain. Caller must stop/reconcile.
            self.close()
            raise TransportError("COMMAND_INCOMPLETE") from exc

    def _take(self, count):
        if count < 0 or count > self.remaining:
            raise TransportError("REPLY_BOUNDS")
        while len(self.buffer) < count:
            remaining_time = self.deadline - time.monotonic()
            if remaining_time <= 0:
                raise TransportError("DEADLINE")
            self.sock.settimeout(remaining_time)
            try:
                chunk = self.sock.recv(min(16384, self.remaining - len(self.buffer)))
            except ConnectionResetError as exc:
                raise PeerDisconnected("PEER_RESET") from exc
            if not chunk:
                raise PeerDisconnected("PEER_EOF")
            self.buffer.extend(chunk)
        result = bytes(self.buffer[:count])
        del self.buffer[:count]
        self.remaining -= count
        return result

    def _line(self):
        line = bytearray()
        while len(line) <= 4096:
            byte = self._take(1)
            if byte == b"\r":
                if self._take(1) != b"\n":
                    raise TransportError("INVALID_RESP")
                return bytes(line)
            line.extend(byte)
        raise TransportError("REPLY_BOUNDS")

    def _number(self):
        raw = self._line()
        if not re.fullmatch(rb"-?(?:0|[1-9][0-9]{0,15})", raw):
            raise TransportError("INVALID_INTEGER")
        return int(raw)

    def _value(self, depth):
        self.items += 1
        if depth > 4 or self.items > MAX_ITEMS:
            raise TransportError("REPLY_BOUNDS")
        kind = self._take(1)
        if kind in (b"+", b"-"):
            raw = self._line()
            if kind == b"-":
                raise RedisError(raw)
            return raw
        if kind == b":":
            return self._number()
        if kind in (b"$", b"*"):
            size = self._number()
            if size == -1:
                return None
            if size < 0:
                raise TransportError("INVALID_RESP")
            if kind == b"*":
                if size > MAX_ITEMS - self.items:
                    raise TransportError("REPLY_BOUNDS")
                return [self._value(depth + 1) for _ in range(size)]
            raw = self._take(size)
            if self._take(2) != b"\r\n":
                raise TransportError("INVALID_RESP")
            return raw
        raise TransportError("INVALID_RESP")
