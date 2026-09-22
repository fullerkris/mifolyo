#!/usr/bin/env python3
"""Independent review reproductions; no Docker, Redis, network, or file writes.

Run from the repository root with Python 3.10+ and -B. All image IDs,
credentials, approvals, sockets, and observations here are synthetic. These
checks demonstrate controller/oracle behavior, NOT a target Redis/Docker bug
or real acceptance. No environment variable or candidate secret value is
printed. Only this reproduction file is intended to be added by this review.

Threat/fault preconditions:
* expiry: a valid short-lived approval, slow but successful local revision and
  Docker operations, and an otherwise honest backend. Every modeled Docker
  delay is below the timeout computed by the actual Docker.timeout method.
  No clock rollback, forged approval, hostile image, or hostile daemon is used.
* revocation: ACL DELUSER is acknowledged; fresh AUTH gets WRONGPASS. The held
  connection then returns NOPERM or times out instead of proving EOF/reset.
  These are fault-injected ambiguous/negative observations of the property the
  harness is supposed to test. We do not assert that real Redis DELUSER behaves
  this way. Actual executor, RESP encoder/parser, and revocation validator run.
"""
from __future__ import annotations

import argparse
import ast
import hashlib
import json
from pathlib import Path
import re
import sys
from unittest.mock import patch

sys.dont_write_bytecode = True
HERE = Path(__file__).resolve().parents[1]
ROOT = HERE.parents[1]
sys.path.insert(0, str(HERE))

import controller as ctl
import executor as worker
import harness as h
import resp
import runtime_case as case
import test_execution as fixtures


def emit(label, **facts):
    print(json.dumps({"check": label, "evidence_kind": "simulated", **facts}, sort_keys=True))


def secret_scan():
    """Scoped candidate scan: locations/categories/context only, never values."""
    names = (*case.FILES, "test_execution.py", "test_harness.py", "README.md")
    paths = [*(HERE / name for name in names), ROOT / "docs/crawl-jobs-v2.md",
             ROOT / "docs/crawl-jobs-v2-plan.md"]
    patterns = {
        "private-key": re.compile(r"-----BEGIN (?:[A-Z0-9 ]+ )?PRIVATE KEY-----"),
        "provider-token": re.compile(r"(?:AKIA|ASIA)[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}"),
        "literal-secret-assignment": re.compile(r"(?i)(?:password|passwd|secret|token|api[_-]?key)\s*[:=]\s*[\"'][^\"'\n]{8,}[\"']"),
        "redis-uri": re.compile(r"rediss?://[^\s\"']+"),
    }
    candidates = []
    for path in paths:
        text = path.read_text(encoding="utf-8")
        tree = ast.parse(text) if path.suffix == ".py" else None
        functions = [node for node in ast.walk(tree) if isinstance(node, ast.FunctionDef)] if tree else []
        for category, pattern in patterns.items():
            for match in pattern.finditer(text):
                line = text.count("\n", 0, match.start()) + 1
                context = next((fn.name for fn in functions if fn.lineno <= line <= fn.end_lineno), None)
                row = {"path": str(path.relative_to(ROOT)), "line": line,
                       "category": category, "test_context": bool(context and context.startswith("test_"))}
                if category == "redis-uri":
                    row["contains_userinfo"] = "@" in match.group(0).partition("://")[2].partition("/")[0]
                candidates.append(row)
    emit("scoped-secret-candidates", files_scanned=len(paths), candidates=candidates,
         values_printed=False, scope="13 requested implementation/test/doc files; no history/environment/retained data")


class Clock:
    def __init__(self):
        self.elapsed = 0.0

    def time(self):
        return 2_000_000_000.0 + self.elapsed

    def monotonic(self):
        return 10_000.0 + self.elapsed


def expiry_replay():
    clock = Clock()
    plan = fixtures.test_plan()

    class SlowHonestBackend(fixtures.FakeDocker):
        def __init__(self):
            super().__init__()
            self.operations = []
            self.redis_inspects = 0

        def delay(self, operation, seconds):
            allowed = ctl.Docker.timeout(self)
            assert seconds < allowed
            self.operations.append({"operation": operation, "at_seconds": clock.elapsed,
                                    "allowed_timeout": allowed, "duration": seconds})
            clock.elapsed += seconds

        def inspect(self, kind, name):
            if kind == "container" and name.endswith("-redis") and self.redis_inspects < 2:
                self.redis_inspects += 1
                self.delay("redis-inspect", 20)
            return super().inspect(kind, name)

        def create(self, spec):
            if spec["role"] == "redis":
                self.delay("redis-create", 20)
            return super().create(spec)

        def start(self, name):
            if name.endswith("-redis"):
                # Use the real timeout calculation which Docker.call uses before
                # dispatching container start, not a fake timeout policy.
                allowed = ctl.Docker.timeout(self)
                self.started_at = clock.elapsed
                self.start_timeout = allowed
            return super().start(name)

    with patch.object(ctl.time, "time", clock.time), patch.object(ctl.time, "monotonic", clock.monotonic):
        approval = fixtures.approval(plan)
        approval["max_seconds"] = 120
        approval["expires_at_ms"] = int((clock.time() + 125) * 1000)
        backend = SlowHonestBackend()

        def slow_revision(_):
            # verify_revision has three separate command(..., timeout=30)
            # calls. Model 26 + 27 + 27 seconds, each successful/in bounds.
            clock.elapsed += 80

        report = ctl.execute(plan, approval, backend, revision_check=slow_revision)
    assert backend.started_at == 140
    assert report["failure_phase"] == "start" and report["verdict"] == "FAIL"
    assert set(backend.resources) == {("volume", "retained-evidence")}
    emit("expired-approval-still-starts-redis", approval_expires_at_seconds=125,
         redis_started_at_seconds=backend.started_at, start_timeout=backend.start_timeout,
         operations=backend.operations, eventual_verdict=report["verdict"],
         failure_phase=report["failure_phase"], cleanup_removed_fixture=True)


class ReplySocket:
    """No socket creation: bounded, in-memory RESP replies for actual Client."""
    def __init__(self, replies):
        self.replies = iter(replies)
        self.pending = b""
        self.closed = False
        self.commands = 0

    def settimeout(self, _):
        pass

    def connect(self, endpoint):
        assert endpoint == resp.SOCKET

    def sendall(self, _):
        self.commands += 1
        self.pending = next(self.replies)

    def recv(self, count):
        if isinstance(self.pending, Exception):
            raise self.pending
        result, self.pending = self.pending[:count], self.pending[count:]
        return result

    def close(self):
        self.closed = True


def revocation_replay():
    credentials = {role: hashlib.sha256(("review-only:" + role).encode()).hexdigest() for role in case.ROLES}
    for fault in ("live-NOPERM", "timeout-no-EOF"):
        admin = ReplySocket([b"+OK\r\n", b":1\r\n"])
        held = ReplySocket([b"+OK\r\n", b"-NOPERM synthetic\r\n" if fault == "live-NOPERM" else TimeoutError()])
        reconnect = ReplySocket([b"-WRONGPASS synthetic\r\n"])
        with patch.object(resp.socket, "socket", side_effect=[admin, held, reconnect]) as factory:
            result = worker.revoke(credentials, ("setup",))
        ctl.verify_revocation(result, ("setup",))
        assert factory.call_count == 3 and held.commands == 2
        assert result["setup"]["held_session"] == "terminated"
        emit("unproven-session-termination-accepted", injected_observation=fault,
             peer_eof_or_reset_observed=False, held_session_claim=result["setup"]["held_session"],
             reconnect=result["setup"]["reconnect"], controller_validation="accepted",
             client_closed_locally=held.closed)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--case", choices=("all", "secrets", "expiry", "revocation"), default="all")
    chosen = parser.parse_args().case
    for label, replay in (("secrets", secret_scan), ("expiry", expiry_replay), ("revocation", revocation_replay)):
        if chosen in ("all", label):
            replay()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
