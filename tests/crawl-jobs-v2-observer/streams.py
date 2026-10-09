"""Bounded owned-process streams. No Docker or native actor starts at import."""
import os
import selectors
import signal
import subprocess
import time

from contracts import LIMITS, TRIALS, Invalid, decode, digest, exact, integer, require
from controller import native_message
from runtime_admission import PHASES

NATIVE_ERRORS = frozenset(("admission", "transport", "case", "deadline", "validator_control", "identity", "bounds",
    "ptrace_denied", "regset", "debug_state", "instruction", "stop_reason", "controller_kill_required", "helper", "witness"))


class Deadline:
    def __init__(self, seconds, clock=time.monotonic):
        require(type(seconds) in (int, float) and 0 < seconds <= 5400, "DEADLINE_BOUND")
        self.clock, self.end = clock, clock() + seconds

    def remaining(self):
        left = self.end - self.clock()
        require(left > 0, "DEADLINE")
        return left

    def check(self):
        self.remaining()


class WireBudget:
    def __init__(self):
        self.bytes = self.frames = 0

    def consume(self, count):
        integer(count, 0, LIMITS["output_bytes"])
        require(self.bytes + count <= LIMITS["output_bytes"], "STREAM_BYTES")
        self.bytes += count

    def frame(self):
        require(self.frames < LIMITS["receipts"], "STREAM_FRAMES")
        self.frames += 1


class FrameBuffer:
    def __init__(self, budget):
        self.budget, self.partial, self.closed = budget, bytearray(), False

    def feed(self, data):
        require(not self.closed and type(data) is bytes, "STREAM_STATE")
        self.budget.consume(len(data))
        frames = []
        for part in data.splitlines(keepends=True):
            require(len(self.partial) + len(part) <= LIMITS["receipt_bytes"], "FRAME_SIZE")
            self.partial.extend(part)
            if part.endswith(b"\n"):
                raw = bytes(self.partial)
                decode(raw, LIMITS["receipt_bytes"])
                self.budget.frame()
                frames.append(raw)
                self.partial.clear()
        return frames

    def eof(self):
        require(not self.closed, "STREAM_EOF")
        self.closed = True
        require(not self.partial, "TRUNCATED_FRAME")


class ProcessStream:
    """Own a new local process group; abort it before reaping its leader.

    This is a transport primitive, not an authorization boundary. The Docker
    adapter supplies its fixed argv only after the outer authorization callback.
    Killing this CLI group never counts as stopping a remote container.
    """
    def __init__(self, argv, binding, budget, deadline, *, env=None, before_input=None, before_write=None):
        exact(binding, ("case_id", "invocation_sha256", "container_id", "image", "role"))
        require(type(binding["case_id"]) is str and binding["case_id"] in TRIALS, "CASE")
        digest(binding["invocation_sha256"])
        digest(binding["container_id"])
        require(type(binding["image"]) is str and binding["image"].startswith("sha256:"), "IMAGE")
        digest(binding["image"][7:])
        require(binding["role"] in ("target", "observer", "oracle"), "ROLE")
        require(type(argv) is list and 1 <= len(argv) <= 32 and all(type(item) is str and item and "\x00" not in item for item in argv), "ARGV")
        deadline.check()
        require(before_input is None or callable(before_input), "INPUT_AUTHORIZER")
        require(before_write is None or callable(before_write), "INPUT_GUARD")
        self.before_input, self.before_write = before_input, before_write
        self.binding = dict(binding)
        self.budget, self.deadline = budget, deadline
        self.decoder, self.frames = FrameBuffer(budget), []
        self.sent, self.phase, self.closed, self.stdout_eof = False, None, False, False
        self.admission_frames = []
        self.selector = selectors.DefaultSelector()
        self._process = None
        try:
            self._process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                env=env, start_new_session=True, close_fds=True)
            for stream, key in ((self._process.stdout, "stdout"), (self._process.stderr, "stderr")):
                os.set_blocking(stream.fileno(), False)
                self.selector.register(stream, selectors.EVENT_READ, key)
            os.set_blocking(self._process.stdin.fileno(), False)
        except BaseException:
            self.close()
            raise

    def _pump(self, timeout):
        for event, _ in self.selector.select(timeout):
            try:
                chunk = os.read(event.fd, 4096)
            except BlockingIOError:
                continue
            if not chunk:
                self.selector.unregister(event.fileobj)
                if event.data == "stdout":
                    self.stdout_eof = True
                    self.decoder.eof()
                continue
            if event.data == "stderr":
                self.budget.consume(len(chunk))
                raise Invalid("ACTOR_STDERR")
            self.frames.extend(self.decoder.feed(chunk))

    def _send(self, raw):
        require(not self.closed and type(raw) is bytes and 0 < len(raw) <= 160, "INPUT_BOUND")
        offset = 0
        try:
            while offset < len(raw):
                self.deadline.check()
                if self.before_input is not None:
                    self.before_input()
                self._pump(0)
                require(not self.frames and not self.decoder.partial and not self.stdout_eof, "EARLY_ACTOR_OUTPUT")
                if self.before_write is not None:
                    self.before_write()
                self.deadline.check()
                try:
                    count = os.write(self._process.stdin.fileno(), raw[offset:])
                    require(count > 0, "INPUT_CLOSED")
                    offset += count
                except BlockingIOError:
                    self._pump(min(self.deadline.remaining(), 0.01))
        except BaseException:
            self.close()
            raise

    def send_case(self):
        require(not self.sent and self.binding["role"] in ("target", "observer"), "CASE_ALREADY_SENT")
        require(not self.binding["case_id"].startswith("N."), "VALIDATOR_ONLY")
        self._send((self.binding["case_id"] + "\n" + self.binding["invocation_sha256"] + "\n").encode())
        self.sent = True

    def start_target(self):
        require(self.binding["role"] == "target" and self.phase == "READY", "EARLY_START")
        self._send(b"G")
        self.phase = "START_DISPATCHED"

    def resume_observer(self):
        # The outer coordinator must separately prove the oracle match. This
        # primitive enforces only this stream's own HELD-before-resume ordering.
        require(self.binding["role"] == "observer" and self.phase == "HELD", "EARLY_RESUME")
        self._send(b"R")
        self.phase = "RESUME_DISPATCHED"

    def arm_observer(self):
        require(self.binding["role"] == "observer" and self.phase == "ADMISSION_READY", "EARLY_ARM")
        self._send(b"A")
        self.phase = "ARM_DISPATCHED"

    def require_admission_ready(self):
        require(self.phase in ("READY", "ADMISSION_READY"), "NATIVE_ADMISSION_TRANSPORT")
        self.require_quiet_live(self.phase)

    def require_quiet_live(self, expected_phase):
        """Observe pending peer loss/output without consuming a protocol frame.

        This is a nonblocking point-in-time check, not future liveness or remote
        absence proof. In particular, ARMED may not already have buffered HELD.
        """
        require(expected_phase in ("READY", "ADMISSION_READY", "ARMED"), "NATIVE_ADMISSION_TRANSPORT")
        try:
            require(not self.closed and self.phase == expected_phase, "NATIVE_ADMISSION_TRANSPORT")
            self._pump(0)
            require(not self.stdout_eof and not self.frames and not self.decoder.partial, "NATIVE_ADMISSION_TRANSPORT")
        except BaseException:
            self.close()
            raise

    def receive(self, expected):
        allowed = {("target", "CLOCK"): "READY", ("target", "START_DISPATCHED"): "FINISHED",
            ("observer", "CLOCK"): "ADMISSION_READY", ("observer", "ARM_DISPATCHED"): "ARMED", ("observer", "ARMED"): "HELD",
            ("observer", "RESUME_DISPATCHED"): "CONTINUATION_DISPATCHED"}
        for role in ("target", "observer"):
            for before, after in zip((None, *PHASES[:-1]), PHASES):
                allowed[(role, before)] = after
        require(not self.closed and self.sent and allowed.get((self.binding["role"], self.phase)) == expected, "STREAM_ORDER")
        try:
            self.deadline.check()
            while not self.frames:
                require(not self.stdout_eof, "EARLY_EOF")
                self._pump(min(self.deadline.remaining(), 0.01))
            self._pump(0)
            raw = self.frames.pop(0)
            value = decode(raw, LIMITS["receipt_bytes"])
            if value.get("status") == "INVALID":
                exact(value, ("cleanup_required", "reason", "status", "version"))
                integer(value["version"], 1, 1)
                require(value["cleanup_required"] is True and type(value["reason"]) is str and value["reason"] in NATIVE_ERRORS, "INVALID_FAILURE")
                raise Invalid("ACTOR_INVALID")
            result = native_message(raw, expected)
            if expected in PHASES:
                require(all(result[key] == self.binding[key] for key in ("case_id", "invocation_sha256", "role")), "ADMISSION_BINDING")
                self.admission_frames.append(raw)
            self.phase = expected
            return result
        except BaseException:
            self.close()
            raise

    def finish_transport(self):
        """Drain to EOF; caller still needs independent remote exit/absence proof."""
        require(self.phase in ("FINISHED", "CONTINUATION_DISPATCHED"), "INCOMPLETE_STREAM")
        try:
            while self.selector.get_map():
                self._pump(min(self.deadline.remaining(), 0.01))
            require(not self.frames and self.stdout_eof, "EXTRA_FRAMES")
            return {"stdout_closed": True, "extra_frames": False, "remote_exit_proven": False,
                "local_process_group_absence_proven": False}
        finally:
            self.close()

    def close(self):
        if self.closed:
            return
        self.closed = True
        if self._process is None:
            self.selector.close()
            return
        # Normal EOF permits reaping the leader; it is not group-absence proof.
        # With any pipe still open, never reap first: a departed leader can have
        # left a child holding that pipe, and its reserved PID prevents reuse.
        reaped = False
        if not self.selector.get_map():
            try:
                self._process.wait(timeout=0)
                reaped = True
            except subprocess.TimeoutExpired:
                pass
        try:
            if not reaped:
                os.killpg(self._process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        except PermissionError:
            raise Invalid("LOCAL_PROCESS_CLEANUP") from None
        finally:
            try:
                self._process.wait(timeout=5)
            finally:
                self.selector.close()
                for stream in (self._process.stdin, self._process.stdout, self._process.stderr):
                    stream.close()

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()
