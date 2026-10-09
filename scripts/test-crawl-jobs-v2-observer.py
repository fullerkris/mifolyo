#!/usr/bin/env python3
"""Run all C0 offline/benign-process tests, refusing missing or skipped coverage.

This invokes the Python preparation suite, not the native actors or Docker builds.
CI retains the test inventory and exact package hashes for revision verification.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "tests/crawl-jobs-v2-observer"


def test_ids(suite):
    for test in suite:
        if isinstance(test, unittest.TestSuite):
            yield from test_ids(test)
        else:
            yield test.id()


class CoverageResult(unittest.TextTestResult):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.started = []
        self.passed = []

    def startTest(self, test):
        self.started.append(test.id())
        super().startTest(test)

    def addSuccess(self, test):
        self.passed.append(test.id())
        super().addSuccess(test)


def run_suite(suite, stream=None):
    inventory = sorted(test_ids(suite))
    if not inventory or len(set(inventory)) != len(inventory):
        raise ValueError("empty or duplicate C0 test inventory")
    result = unittest.TextTestRunner(stream=stream, verbosity=2, resultclass=CoverageResult).run(suite)
    complete = (result.testsRun == len(inventory) and sorted(result.started) == inventory
                and sorted(result.passed) == inventory)
    passed = (complete and result.wasSuccessful() and not result.skipped
              and not result.expectedFailures and not result.unexpectedSuccesses)
    return {"status": "PASS" if passed else "FAIL", "inventory": inventory,
            "tests_run": result.testsRun, "started": sorted(result.started), "passed": sorted(result.passed),
            "skipped": [test.id() for test, _ in result.skipped],
            "expected_failures": [test.id() for test, _ in result.expectedFailures],
            "unexpected_successes": [test.id() for test in result.unexpectedSuccesses],
            "failures": [test.id() for test, _ in result.failures],
            "errors": [test.id() for test, _ in result.errors]}


def source_hashes():
    return {path.relative_to(ROOT).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in sorted(SOURCE.rglob("*"))
            if path.is_file() and "__pycache__" not in path.parts and path.suffix != ".pyc"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", type=Path, help="Retain source-bound JSON results for CI verification")
    args = parser.parse_args()
    report = {"kind": "c0_offline_tests", "version": 1, "commit": os.environ.get("GITHUB_SHA", "local"),
              "status": "FAIL", "runner_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
    try:
        report["source_files"] = source_hashes()
        suite = unittest.TestLoader().discover(str(SOURCE))
        report.update(run_suite(suite))
        if source_hashes() != report["source_files"]:
            raise ValueError("C0 package changed during the test run")
    except (ImportError, OSError, ValueError) as error:
        report.update(status="FAIL", refusal=str(error))
    if args.report:
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, sort_keys=True, indent=2) + "\n")
    print(f"{report['status']}: C0 offline coverage ({report.get('tests_run', 0)} tests)", flush=True)
    if report.get("refusal"):
        print(report["refusal"], file=sys.stderr)
    return 0 if report["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
