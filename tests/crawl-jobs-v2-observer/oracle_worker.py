"""Read-only synthetic oracle worker. Fixed witness/socket paths; no tracing.

The target creates /witness/state and /control/frame.sock in the scoped volumes.
The oracle opens both through read-only mounts and never sends application bytes
to the target. Controller requests are closed, chained and case/scope bound.
"""
import errno
import os
from pathlib import Path
import select
import socket
import stat
import struct
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parent))
from contracts import LIMITS, TRIALS, Invalid, canonical, digest, require, sha
from linux_observation import LinuxReader
import oracle
import oracle_protocol as wire


class FrameClient:
    def __init__(self, case_id, clock=time.monotonic_ns, end=None):
        self.transcript, self.clock = wire.ClientTranscript(case_id), clock
        self.sock, self.identity = None, None
        self.connected, self.events = False, 0
        self.end = time.monotonic() + LIMITS["admission_ms"] / 1000 if end is None else end

    def pump(self):
        t = self.transcript
        if not t.enabled or t.closed or t.eof or t.transport_error:
            return
        self.events += 1
        require(self.events <= LIMITS["events"], "CLIENT_EVENTS")
        path = "/control/frame.sock"
        if not self.connected:
            try:
                info = os.lstat(path)
            except FileNotFoundError:
                return
            require(stat.S_ISSOCK(info.st_mode) and info.st_nlink == 1 and
                (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) == (999, 999, 0o600), "CLIENT_SOCKET")
            identity = (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode)
            require(self.identity is None or self.identity == identity, "CLIENT_SOCKET_CHANGED")
            self.identity = identity
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM | socket.SOCK_NONBLOCK | socket.SOCK_CLOEXEC)
            try:
                sock.connect(path)
                peer = struct.unpack("3i", sock.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
                require(peer[1:] == (999, 999), "CLIENT_PEER")
                after = os.lstat(path)
                require(identity == (after.st_dev, after.st_ino, after.st_uid, after.st_gid, after.st_mode), "CLIENT_SOCKET_CHANGED")
            except OSError as error:
                sock.close()
                if error.errno in (errno.ENOENT, errno.ECONNREFUSED, errno.EAGAIN):
                    return
                raise Invalid("CLIENT_CONNECT") from None
            except BaseException:
                sock.close()
                raise
            self.sock, self.connected = sock, True
        while True:
            try:
                raw = self.sock.recv(1025)
            except BlockingIOError:
                return
            except OSError:
                t.transport_error = True
                return
            when = self.clock()
            if not raw:
                t.finish(when)
                return
            t.feed(raw, when)

    def drain(self):
        until = min(self.end, time.monotonic() + LIMITS["observation_ms"] / 1000)
        while self.transcript.enabled and not self.transcript.eof and not self.transcript.transport_error and time.monotonic() < until:
            self.pump()
            if not self.transcript.eof:
                time.sleep(0.001)

    def close(self):
        if self.sock is not None:
            self.sock.close()
        self.transcript.closed = True


class Engine:
    def __init__(self, binding, *, client=None, admit=oracle.admit_witness, read=oracle.read_witness, clock=time.monotonic_ns):
        self.binding = dict(binding)
        require(binding["case_id"] in TRIALS and not binding["case_id"].startswith("N."), "ORACLE_CASE")
        digest(binding["invocation_sha256"])
        self.client = FrameClient(binding["case_id"], clock) if client is None else client
        self.admit, self.read, self.clock = admit, read, clock
        self.identity, self.previous, self.sequence, self.phase = None, None, 0, "READY"
        self.output_bytes = 0

    def handle(self, raw):
        require(self.sequence < LIMITS["receipts"], "ORACLE_RECEIPTS")
        value = wire.parse_request(raw, self.binding, self.sequence, self.previous)
        action = value["action"]
        allowed = {"READY": {"INIT"}, "INITIALIZED": {"HELD", "CLOSE"}, "HELD": {"FINAL", "CLOSE"}, "FINAL": {"CLOSE"}}
        require(action in allowed.get(self.phase, set()), "ORACLE_ORDER")
        self.client.pump()
        if action == "INIT":
            self.identity = self.admit()
            state = self.read(self.identity)
            oracle.validate_state(state)
            require(all(state[key] == 0 for key in oracle.FIELDS if key != "magic") and not any(state["transcript"]), "WITNESS_NOT_INITIAL")
            require(not self.client.connected and not self.client.transcript.data and not self.client.transcript.eof and
                not self.client.transcript.transport_error, "CLIENT_BEFORE_START")
            payload = {"witness": self.identity, "state_sha256": sha(canonical(state))}
        elif action == "HELD":
            before = self.clock()
            require(before >= self.client.transcript.last_ns, "CLIENT_CLOCK")
            state = self.read(self.identity)
            oracle.held(self.binding["case_id"], state)
            require(not self.client.transcript.enabled or self.client.connected, "CLIENT_NOT_CONNECTED")
            self.client.pump()
            self.client.transcript.cut(state["pre_ns"], before)
            payload = {"witness_sha256": sha(canonical(self.identity)), "state_sha256": sha(canonical(state)),
                "observer_sha256": value["payload"]["observer_sha256"], "lower_ns": state["pre_ns"], "upper_ns": before,
                "ordinal": state["ordinal"]}
        elif action == "FINAL":
            state = self.read(self.identity)
            oracle.completed(self.binding["case_id"], state)
            payload = {"witness_sha256": sha(canonical(self.identity)), "state_sha256": sha(canonical(state)), "post_ns": state["post_ns"]}
        else:
            self.client.drain()
            self.client.close()
            payload = {"witness_sha256": sha(canonical(self.identity))}
        payload["client"] = self.client.transcript.summary()
        result = {**self.binding, "version": 1, "sequence": self.sequence, "previous": self.previous,
            "phase": wire.ACTIONS[action], "payload": payload, "execution_authorized": False, "runtime_evidence": False}
        encoded = canonical(result)
        wire.response(encoded, self.binding, self.sequence, self.previous, result["phase"])
        require(self.output_bytes + len(encoded) <= LIMITS["output_bytes"], "ORACLE_OUTPUT_BOUND")
        self.output_bytes += len(encoded)
        self.previous, self.sequence, self.phase = sha(encoded), self.sequence + 1, "INITIALIZED" if action == "INIT" else action
        return encoded


def main():
    engine = None
    try:
        require(len(sys.argv) == 1, "ORACLE_ARGV")
        until = time.monotonic() + LIMITS["admission_ms"] / 1000
        reader, linux_end, admitting = LinuxReader(), None, True
        os.set_blocking(0, False)
        os.set_blocking(1, False)
        poll = select.poll()
        poll.register(0, select.POLLIN | select.POLLHUP | select.POLLERR)
        def emit(raw):
            require(not admitting or linux_end is None or reader.monotonic_ns() < linux_end, "ORACLE_ADMISSION_EXPIRED")
            require(len(raw) <= LIMITS["receipt_bytes"] and os.write(1, raw) == len(raw), "ORACLE_BACKPRESSURE")
        def line(limit):
            raw = bytearray()
            while time.monotonic() < until:
                require(not admitting or linux_end is None or reader.monotonic_ns() < linux_end, "ORACLE_ADMISSION_EXPIRED")
                if engine is not None:
                    engine.client.pump()
                if not poll.poll(1):
                    continue
                byte = os.read(0, 1)
                require(byte, "ORACLE_EOF")
                raw.extend(byte)
                require(len(raw) <= limit, "ORACLE_INPUT_BOUND")
                if byte == b"\n":
                    return bytes(raw)
            raise Invalid("ORACLE_DEADLINE")
        case_id = line(80)[:-1].decode("ascii")
        invocation = line(65)[:-1].decode("ascii")
        expiry = line(21)[:-1]
        require(expiry and all(48 <= byte <= 57 for byte in expiry), "ORACLE_ADMISSION_WINDOW")
        from contracts import clock_ns
        linux_end = clock_ns(int(expiry), 1)
        now = reader.monotonic_ns()
        require(now < linux_end <= now + LIMITS["admission_ms"] * 1000000, "ORACLE_ADMISSION_WINDOW")
        require(case_id in TRIALS and not case_id.startswith("N."), "ORACLE_CASE")
        digest(invocation)
        binding = {"case_id": case_id, "invocation_sha256": invocation}
        for raw in reader.actor_frames(case_id, invocation):
            emit(raw)
        emit(canonical({"phase": "ORACLE_READY", "source_sha256": wire.source_digest(), "version": 1}))
        engine = Engine(binding, client=FrameClient(case_id, clock=reader.monotonic_ns, end=until), clock=reader.monotonic_ns)
        while engine.phase != "CLOSE":
            require(time.monotonic() < until, "ORACLE_DEADLINE")
            emit(engine.handle(line(LIMITS["receipt_bytes"])))
            if admitting and engine.phase == "INITIALIZED":
                admitting = False
                until += LIMITS["observation_ms"] / 1000
                engine.client.end = until
        return 0
    except Exception:
        try:
            os.set_blocking(1, False)
            os.write(1, canonical({"status": "INVALID", "reason": "oracle", "cleanup_required": True, "version": 1}))
        except OSError:
            pass
        return 1
    finally:
        if engine is not None:
            engine.client.close()


if __name__ == "__main__":
    raise SystemExit(main())
