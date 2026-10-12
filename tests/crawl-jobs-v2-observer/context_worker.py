"""Fixed read-only Linux-host endpoint using operator-provisioned configuration.

Provision the anchor as root-owned /etc/obs1/context-source.json and the 32-byte
authentication key as mode-0600 /etc/obs1/context-source.key owned by this service.
Neither an actor nor a request supplies these paths, keys or namespace anchors.
"""
import os
from pathlib import Path
import select
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parent))
from contracts import LIMITS, canonical, decode, require
from runtime_context import LinuxContextEndpoint
import context_files


class Deadline:
    def __init__(self):
        self.end = time.monotonic() + LIMITS["admission_ms"] / 1000

    def check(self):
        require(time.monotonic() < self.end, "CONTEXT_DEADLINE")


def provisioned(path, key=False):
    expected = context_files.CONFIG / ("context-source.key" if key else "context-source.json")
    require(Path(path) == expected, "CONTEXT_CONFIG_PATH")
    raw = context_files.read_config(expected.name, os.geteuid() if key else 0,
        (0o600,) if key else (0o400, 0o444, 0o644), 32 if key else 4096)
    require(not key or len(raw) == 32, "CONTEXT_CONFIG")
    return raw


def main():
    try:
        require(len(sys.argv) == 1, "CONTEXT_ARGV")
        deadline = Deadline()
        endpoint = LinuxContextEndpoint(decode(provisioned("/etc/obs1/context-source.json"), 4096), provisioned("/etc/obs1/context-source.key", True))
        os.set_blocking(0, False)
        poll = select.poll()
        poll.register(0, select.POLLIN | select.POLLHUP | select.POLLERR)
        raw = bytearray()
        while b"\n" not in raw:
            deadline.check()
            if poll.poll(10):
                chunk = os.read(0, LIMITS["receipt_bytes"] + 1 - len(raw))
                require(chunk, "CONTEXT_EOF")
                raw.extend(chunk)
                require(len(raw) <= LIMITS["receipt_bytes"], "CONTEXT_INPUT_BOUND")
        output = endpoint.exchange(bytes(raw), deadline)
        os.set_blocking(1, False)
        require(os.write(1, output) == len(output), "CONTEXT_OUTPUT")
        return 0
    except Exception:
        try:
            os.set_blocking(1, False)
            os.write(1, canonical({"status": "INVALID", "reason": "context", "version": 1}))
        except OSError:
            pass
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
