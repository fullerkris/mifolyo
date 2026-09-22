#!/usr/bin/env python3
"""Run a deterministic, exhaustive partition of the compiled V2 Go test inventory.

All top-level tests/examples/fuzz seed tests belong to exactly one shard. No
payload, assertion, subtest, or race instrumentation is removed. The Go package
timeout remains 90 minutes; sharding addresses the observed aggregate CI timeout.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SPIDER = ROOT / "services/spider"
PACKAGE = "./internal/database/crawljobsv2"
COUNT = 8
OPTIONAL_SKIPS = {"TestJobLuaNativeFactoryParity"}
FULL_PACKAGE = "github.com/IonelPopJara/search-engine/services/spider/internal/database/crawljobsv2"


def partition(names, count=COUNT):
    if not names or len(set(names)) != len(names) or not 1 <= count <= 32:
        raise ValueError("invalid test inventory")
    if any(not re.fullmatch(r"(?:Test|Example|Fuzz)[A-Za-z0-9_]*", name) for name in names):
        raise ValueError("unsupported test name")
    result = [[] for _ in range(count)]
    for name in sorted(names):
        shard = int(hashlib.sha256(name.encode()).hexdigest(), 16) % count
        result[shard].append(name)
    if any(not group for group in result):
        raise ValueError("empty shard")
    return result


def inventory():
    result = subprocess.run(["go", "test", "-mod=readonly", "-race", "-list", ".", PACKAGE], cwd=SPIDER,
                            capture_output=True, text=True, timeout=180, check=True)
    names = []
    for line in result.stdout.splitlines():
        if re.fullmatch(r"(?:Test|Example|Fuzz)[A-Za-z0-9_]*", line):
            names.append(line)
        elif line.startswith("Benchmark") or line.startswith("ok\t") or line.startswith("ok  "):
            # Benchmarks were never invoked by the normal go test command.
            continue
        elif line.strip():
            raise ValueError("unexpected compiled test listing")
    partition(names)
    return sorted(names)


def report_for(names, shard):
    groups = partition(names)
    return {"version": 1, "count": COUNT, "shard": shard, "inventory": names,
            "selected": groups[shard], "race": True, "status": "NOT_RUN",
            "passed": [], "skipped": [],
            "commit": os.environ.get("GITHUB_SHA", "local")}


def verify_reports(reports):
    if len(reports) != COUNT or {row["shard"] for row in reports} != set(range(COUNT)):
        raise ValueError("missing/duplicate shard")
    names, commit = reports[0]["inventory"], reports[0]["commit"]
    if os.environ.get("GITHUB_SHA") and commit != os.environ["GITHUB_SHA"]:
        raise ValueError("shard commit differs from verifier revision")
    groups = partition(names)
    for row in reports:
        if (set(row) != {"version", "count", "shard", "inventory", "selected", "race", "status", "commit", "passed", "skipped"}
                or type(row["version"]) is not int or row["version"] != 1
                or type(row["count"]) is not int or row["count"] != COUNT
                or type(row["shard"]) is not int or row["race"] is not True
                or row["inventory"] != names or row["commit"] != commit
                or row["selected"] != groups[row["shard"]] or row["status"] != "PASS"
                or sorted(row["passed"] + row["skipped"]) != row["selected"]
                or not set(row["skipped"]) <= OPTIONAL_SKIPS):
            raise ValueError("shard coverage/result mismatch")
    return len(names)


def other_packages():
    listed = subprocess.run(["go", "list", "./..."], cwd=SPIDER, capture_output=True,
                            text=True, timeout=180, check=True).stdout.splitlines()
    if listed.count(FULL_PACKAGE) != 1 or len(set(listed)) != len(listed):
        raise ValueError("unexpected Spider package inventory")
    selected = [name for name in listed if name != FULL_PACKAGE]
    if not selected or any(not name.startswith("github.com/IonelPopJara/search-engine/services/spider/") for name in selected):
        raise ValueError("unexpected non-V2 package")
    return subprocess.run(["go", "test", "-mod=readonly", "-race", "-timeout", "90m", "-v", *selected],
                          cwd=SPIDER, timeout=5500).returncode


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="mode", required=True)
    sub.add_parser("others", help="Race-test every Spider package except the separately verified V2 package")
    run = sub.add_parser("run")
    run.add_argument("--shard", type=int, choices=range(COUNT), required=True)
    run.add_argument("--report-dir", type=Path, required=True)
    run.add_argument("--list-only", action="store_true")
    verify = sub.add_parser("verify")
    verify.add_argument("--report-dir", type=Path, required=True)
    args = parser.parse_args()
    if args.mode == "others":
        return other_packages()
    if args.mode == "verify":
        paths = sorted(args.report_dir.glob("shard-*.json"))
        count = verify_reports([json.loads(path.read_text()) for path in paths])
        print(f"PASS: all {count} compiled top-level cases covered once across {COUNT} race shards")
        return 0
    names = inventory()
    report = report_for(names, args.shard)
    print(json.dumps(report, sort_keys=True), flush=True)
    if args.list_only:
        return 0
    args.report_dir.mkdir(parents=True, exist_ok=True)
    path = args.report_dir / f"shard-{args.shard}.json"
    expression = "^(?:" + "|".join(report["selected"]) + ")$"
    if len(expression) > 100000:
        raise ValueError("test selector too large")
    with tempfile.TemporaryFile(mode="w+t") as log:
        result = subprocess.run(["go", "test", "-mod=readonly", "-race", "-timeout", "90m", "-count=1",
                                 "-json", "-run", expression, PACKAGE], cwd=SPIDER, stdout=log, timeout=5500)
        log.seek(0)
        completed = set()
        for line in log:
            sys.stdout.write(line)
            event = json.loads(line)
            name, action = event.get("Test"), event.get("Action")
            # A skipped child can have a passing parent and a zero Go exit code.
            # Retain every full skip name, not just selected top-level names.
            if action == "skip" and name is not None:
                if name in report["skipped"]:
                    raise ValueError("duplicate skip result")
                report["skipped"].append(name)
            if name in report["selected"] and action in {"pass", "skip", "fail"}:
                if name in completed:
                    raise ValueError("duplicate top-level result")
                completed.add(name)
                if action == "pass":
                    report["passed"].append(name)
    report["passed"].sort()
    report["skipped"].sort()
    complete = sorted(report["passed"] + report["skipped"]) == report["selected"]
    allowed_skips = set(report["skipped"]) <= OPTIONAL_SKIPS
    report["status"] = "PASS" if result.returncode == 0 and complete and allowed_skips else "FAIL"
    path.write_text(json.dumps(report, sort_keys=True, indent=2) + "\n")
    return 0 if report["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
