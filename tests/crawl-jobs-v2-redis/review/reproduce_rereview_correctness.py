#!/usr/bin/env python3
"""Independent correctness re-review: local IPC/processes/fakes only.

No Redis, Docker, listener, datastore, image, or external network is used.
The Unix peer-close case calls the real executor.revoke, connect and RESP
client; AUTH/ACL/fresh-AUTH server replies are synthetic, not Redis evidence.
The timer and process-group checks run bounded local Python children only.
Original review scripts and their inventories are deliberately untouched.
"""
from __future__ import annotations

import argparse
import errno
import os
from pathlib import Path
import select
import signal
import socket
import subprocess
import sys
from unittest.mock import patch

HERE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(HERE))

import controller as ctl
import executor as worker
import resp
import runtime_case as case
from test_review_regressions import ReplySocket


def revoked_unix_peer():
    """A real, orderly AF_UNIX close can fail revocation before any recv probe.

    The held socket is an already-connected socket pair. Its connect() facade
    asserts the expected endpoint but never connects to that filesystem path.
    A duplicate client descriptor supplies independent EOF observation *after*
    the reviewed implementation fails. That observation is not fed to the code.
    """
    credentials = {role: format(i + 1, "064x") for i, role in enumerate(case.ROLES)}
    client_socket, peer_socket = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM)
    peer_socket.settimeout(2)
    oracle_socket = client_socket.dup()
    oracle_socket.settimeout(2)
    expected_auth = resp.encode(("AUTH", "cj2_setup", credentials["setup"]))
    # Real AUTH request bytes travel over IPC; only the successful reply is fake.
    peer_socket.sendall(b"+OK\r\n")

    class HeldSocket:
        recv_calls = 0

        def connect(self, endpoint):
            assert endpoint == resp.SOCKET  # No filesystem socket connection.

        def settimeout(self, value):
            client_socket.settimeout(value)

        def sendall(self, raw):
            client_socket.sendall(raw)

        def recv(self, count):
            self.recv_calls += 1
            return client_socket.recv(count)

        def close(self):
            client_socket.close()

    class AdminSocket(ReplySocket):
        def sendall(self, raw):
            if raw == resp.encode(("ACL", "DELUSER", "cj2_setup")):
                # Drain all prior client data to produce an orderly close.
                seen = bytearray()
                while len(seen) < len(expected_auth):
                    part = peer_socket.recv(len(expected_auth) - len(seen))
                    assert part
                    seen.extend(part)
                assert seen == expected_auth
                peer_socket.close()
            super().sendall(raw)

    held = HeldSocket()
    admin = AdminSocket([b"+OK\r\n", b":1\r\n"])
    reconnect = ReplySocket([b"-WRONGPASS synthetic\r\n"])
    try:
        with patch.object(resp.socket, "socket", side_effect=[admin, held, reconnect]) as factory:
            try:
                worker.revoke(credentials, ("setup",))
            except resp.TransportError as exc:
                assert not isinstance(exc, resp.PeerDisconnected)
                assert isinstance(exc.__cause__, BrokenPipeError)
                assert exc.__cause__.errno == errno.EPIPE
                assert factory.call_count == 2  # Fresh AUTH never reached.
                assert held.recv_calls == 1  # AUTH reply only; no PING recv.
                assert oracle_socket.recv(1) == b""  # Independently observed EOF.
                print("REPRODUCED: orderly Unix peer close -> sendall EPIPE -> COMMAND_INCOMPLETE")
                print("revoke rejected; fresh AUTH not reached; independent duplicate-fd recv returned EOF")
                print("Limits: actual local Unix IPC plus fake Redis replies, not a Redis/Docker run.")
            else:
                raise AssertionError("counterexample no longer reproduces")
    finally:
        client_socket.close()
        peer_socket.close()
        oracle_socket.close()


def group_abort():
    """Exercise real command() against an exited leader and pipe-holding child."""
    real_popen = subprocess.Popen
    started = []
    copies = []
    writers_gone = False

    def capture(*args, **kwargs):
        child = real_popen(*args, **kwargs)
        started.append(child)
        copies.append(os.dup(child.stdout.fileno()))
        return child

    code = ("import os,time; pid=os.fork(); "
            "os._exit(0) if pid else time.sleep(5)")
    try:
        with patch.object(ctl.subprocess, "Popen", side_effect=capture):
            try:
                ctl.command([sys.executable, "-B", "-c", code], timeout=1)
            except ctl.CommandError as exc:
                assert str(exc) == "COMMAND_TIMEOUT"
            else:
                raise AssertionError("expected a timeout from the child's held pipes")
        assert started[0].returncode == 0, "leader did not exit before the deadline"
        assert select.select(copies, [], [], 1)[0] == copies
        assert os.read(copies[0], 1) == b"", "child still holds stdout open"
        writers_gone = True
        print("PASS COR-2: exited leader, child-held pipe, command timeout, observed EOF after group kill")
    finally:
        for child in started:
            # Local, freshly created group only; always bound leftover lifetime.
            if not writers_gone:
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            child.wait(timeout=5)
        for fd in copies:
            os.close(fd)


def worker_timers():
    """Call real main() while stdin, a fake validator, or output is blocked."""
    scenarios = {
        "stdin": [sys.executable, "-B", str(HERE / "executor.py"), "ready", "250"],
        "validation": [sys.executable, "-B", "-c",
            "import sys,time; sys.path.insert(0,sys.argv[1]); import executor as w; "
            "w.validate_request=lambda _: time.sleep(10); "
            "sys.argv=['executor.py','ready','250']; raise SystemExit(w.main())", str(HERE)],
        "output": [sys.executable, "-B", "-c",
            "import sys; sys.path.insert(0,sys.argv[1]); import executor as w; "
            "w.validate_request=lambda _: None; w.environment=lambda **_: {}; "
            "w.probe=lambda _: {'pad':'x'*1048576}; "
            "sys.argv=['executor.py','probe','250']; raise SystemExit(w.main())", str(HERE)],
    }
    for name, argv in scenarios.items():
        child = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            if name != "stdin":
                child.stdin.write(b"{}\n")
                child.stdin.close()
            # Intentionally do not drain stdout until exit: output must block.
            assert child.wait(timeout=5) == 124, (name, child.returncode)
            out, err = child.stdout.read(), child.stderr.read()
            assert err == b"", (name, err)
            assert (0 < len(out) < 1048576) if name == "output" else out == b""
            print(f"PASS COR-1 worker timer: {name} blocked -> real main() exited 124")
        finally:
            if child.poll() is None:
                child.kill()
                child.wait(timeout=5)
            for stream in (child.stdin, child.stdout, child.stderr):
                stream.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("revoked-unix-peer", "group-abort", "worker-timers"))
    args = parser.parse_args()
    globals()[args.mode.replace("-", "_")]()


if __name__ == "__main__":
    main()
