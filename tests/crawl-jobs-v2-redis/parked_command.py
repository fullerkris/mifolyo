"""Bounded host-side acknowledgment with a still-attached command.

This owns only its subprocess session. Container termination is separately proved
by the Docker controller; killing this CLI is never worker-death evidence.
"""
import ctypes
import errno
import os
import selectors
import signal
import subprocess
import sys
import time

import harness as h


class ParkedCommandError(Exception):
    pass


class _DarwinSigval(ctypes.Union):
    _fields_ = [("integer", ctypes.c_int), ("pointer", ctypes.c_void_p)]


class _DarwinSiginfo(ctypes.Structure):
    # Darwin sys/signal.h siginfo_t; used only when Python omits os.waitid.
    _fields_ = [("signo", ctypes.c_int), ("error", ctypes.c_int), ("code", ctypes.c_int),
                ("pid", ctypes.c_int), ("uid", ctypes.c_uint), ("status", ctypes.c_int),
                ("address", ctypes.c_void_p), ("value", _DarwinSigval), ("band", ctypes.c_long),
                ("reserved", ctypes.c_ulong * 7)]


def child_has_exited(pid):
    """Observe only our child, without reaping/releasing its process identity."""
    h.require(type(pid) is int and 0 < pid <= 2147483647, "PARK_CHILD_IDENTITY")
    flags = os.WEXITED | os.WNOHANG | os.WNOWAIT
    try:
        if hasattr(os, "waitid"):
            result = os.waitid(os.P_PID, pid, flags)
            if result is None or result.si_pid == 0:
                return False
            h.require(result.si_pid == pid, "PARK_CHILD_IDENTITY")
            return True
        if sys.platform != "darwin" or ctypes.sizeof(ctypes.c_void_p) != 8:
            raise ParkedCommandError("PARK_LIVENESS_UNSUPPORTED")
        h.require(ctypes.sizeof(_DarwinSiginfo) == 104 and _DarwinSiginfo.pid.offset == 12, "PARK_SIGINFO_LAYOUT")
        libc = ctypes.CDLL(None, use_errno=True)
        waitid = libc.waitid
        waitid.argtypes = [ctypes.c_int, ctypes.c_uint, ctypes.POINTER(_DarwinSiginfo), ctypes.c_int]
        waitid.restype = ctypes.c_int
        result = _DarwinSiginfo()
        if waitid(os.P_PID, pid, ctypes.byref(result), flags) != 0:
            if ctypes.get_errno() == errno.ECHILD:
                raise ParkedCommandError("PARK_CHILD_OWNERSHIP_LOST")
            raise ParkedCommandError("PARK_CHILD_STATUS_UNKNOWN")
        h.require(result.pid in (0, pid), "PARK_CHILD_IDENTITY")
        return result.pid == pid
    except ChildProcessError:
        raise ParkedCommandError("PARK_CHILD_OWNERSHIP_LOST") from None
    except (OSError, AttributeError):
        raise ParkedCommandError("PARK_CHILD_STATUS_UNKNOWN") from None


class ParkedCommand:
    def __init__(self, process, deadline):
        self.process, self.deadline = process, deadline
        self.received_at = None
        self.stdout, self.stderr = bytearray(), bytearray()
        self.finished = False
        self.leader_reaped = False
        self.cleanup_error = False

    def _close_streams(self):
        for stream in (self.process.stdin, self.process.stdout, self.process.stderr):
            if stream is not None:
                stream.close()

    def _terminate_session(self, timeout=5):
        if self.cleanup_error:
            raise ParkedCommandError("PARK_SESSION_TERMINATION_UNPROVEN")
        if self.finished:
            return self.process.returncode
        if self.leader_reaped or self.process.returncode is not None:
            self.cleanup_error = True
            raise ParkedCommandError("PARK_SESSION_IDENTITY_LOST")
        # Keep the leader unreaped until its owned session has been terminated.
        permission_error = False
        try:
            os.killpg(self.process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        except PermissionError:
            # Darwin can report EPERM for a group whose leader has just exited.
            # Reaping is required; EPERM alone is never termination evidence.
            permission_error = True
        try:
            code = self.process.wait(timeout=timeout)
            self.leader_reaped = True
            if permission_error:
                try:
                    os.killpg(self.process.pid, 0)
                except ProcessLookupError:
                    pass
                except PermissionError:
                    raise ParkedCommandError("PARK_SESSION_TERMINATION_UNPROVEN") from None
                else:
                    raise ParkedCommandError("PARK_SESSION_TERMINATION_UNPROVEN")
            self.finished = True
            return code
        except BaseException:
            self.cleanup_error = True
            raise
        finally:
            self._close_streams()

    def abort(self):
        self._terminate_session()

    def require_waiting(self, margin_seconds=2):
        if self.finished or self.leader_reaped or self.cleanup_error:
            raise ParkedCommandError("PARK_NOT_WAITING")
        try:
            exited = child_has_exited(self.process.pid)
        except ParkedCommandError as error:
            if error.args == ("PARK_CHILD_OWNERSHIP_LOST",):
                self.cleanup_error = True
            raise
        if exited:
            raise ParkedCommandError("PARK_EARLY_EXIT")
        if time.monotonic() + margin_seconds >= self.deadline:
            raise ParkedCommandError("PARK_DEADLINE_MARGIN")
        # EOF means an exited command; additional output breaks the park contract.
        # Do not poll/reap before possible process-group cleanup.
        for stream in (self.process.stdout, self.process.stderr):
            try:
                os.read(stream.fileno(), 1)
            except BlockingIOError:
                continue
            raise ParkedCommandError("PARK_NOT_WAITING")

    def finish(self, timeout):
        deadline = time.monotonic() + timeout
        extra = bytearray()
        try:
            with selectors.DefaultSelector() as mux:
                mux.register(self.process.stdout, selectors.EVENT_READ, "out")
                mux.register(self.process.stderr, selectors.EVENT_READ, "err")
                while mux.get_map():
                    left = deadline - time.monotonic()
                    if left <= 0:
                        raise ParkedCommandError("PARK_EXIT_TIMEOUT")
                    for event, _ in mux.select(min(left, 0.1)):
                        chunk = os.read(event.fd, 16384)
                        if not chunk:
                            mux.unregister(event.fileobj)
                        elif event.data == "out":
                            extra.extend(chunk)
                            h.require(not extra, "PARK_EXTRA_OUTPUT")
                        else:
                            self.stderr.extend(chunk)
                            if self.stderr:
                                raise ParkedCommandError("PARK_EXTRA_STDERR")
                        if len(self.stdout) + len(extra) + len(self.stderr) > h.MAX_ARTIFACT_BYTES:
                            raise ParkedCommandError("PARK_OUTPUT_LIMIT")
            # EOF is not a guarantee that descendants exited. Terminate the owned
            # session before reaping its leader; a live CLI would return -SIGKILL,
            # not the worker's required attached-command exit status 137.
            return self._terminate_session(max(0.01, deadline - time.monotonic()))
        except BaseException:
            self.abort()
            raise


def start(argv, data, timeout):
    h.require(type(data) is bytes and 0 < len(data) <= h.MAX_ARTIFACT_BYTES and 0 < timeout <= 30, "PARK_COMMAND_BOUND")
    env = {key: value for key, value in os.environ.items() if key not in
           ("DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH")}
    process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               env=env, start_new_session=True)
    parked = ParkedCommand(process, time.monotonic() + timeout)
    offset = 0
    try:
        with selectors.DefaultSelector() as mux:
            for stream, event, tag in ((process.stdin, selectors.EVENT_WRITE, "in"),
                                       (process.stdout, selectors.EVENT_READ, "out"), (process.stderr, selectors.EVENT_READ, "err")):
                os.set_blocking(stream.fileno(), False)
                mux.register(stream, event, tag)
            while mux.get_map():
                left = parked.deadline - time.monotonic()
                if left <= 0:
                    raise ParkedCommandError("PARK_ACK_TIMEOUT")
                for event, _ in mux.select(min(left, 0.1)):
                    if event.data == "in":
                        offset += os.write(event.fd, data[offset:offset + 16384])
                        if offset == len(data):
                            mux.unregister(event.fileobj)
                            event.fileobj.close()
                            process.stdin = None
                    else:
                        chunk = os.read(event.fd, 16384)
                        if not chunk:
                            raise ParkedCommandError("PARK_EARLY_EXIT")
                        (parked.stdout if event.data == "out" else parked.stderr).extend(chunk)
                        if len(parked.stdout) + len(parked.stderr) > h.MAX_ARTIFACT_BYTES:
                            raise ParkedCommandError("PARK_OUTPUT_LIMIT")
                if b"\n" in parked.stdout:
                    parked.received_at = time.monotonic()
                    h.require(offset == len(data) and parked.stdout.count(b"\n") == 1 and parked.stdout.endswith(b"\n") and not parked.stderr,
                              "PARK_RECEIPT_FRAME")
                    result = h.decode(bytes(parked.stdout))
                    parked.require_waiting()
                    return parked, result
        raise ParkedCommandError("PARK_EARLY_EXIT")
    except BaseException:
        parked.abort()
        raise
