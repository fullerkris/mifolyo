"""Preserve pinned upstream references and source-bound review cross-checks."""
import hashlib
import json
import os
from pathlib import Path
import sys
import unittest
from unittest.mock import patch
from urllib.request import urlopen

HERE = Path(__file__).resolve().parent
OUT = HERE.parent
ROOT = OUT.parents[1]
SOURCE = OUT / "source/tests/crawl-jobs-v2-observer"
sys.dont_write_bytecode = True
sys.path.insert(0, str(SOURCE))
sys.path.insert(0, str(HERE))

import independent_probes as p


def record(path):
    raw = path.read_bytes()
    return {"bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


def write(path, value):
    with path.open("x") as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write("\n")


def ids(suite):
    for test in suite:
        if isinstance(test, unittest.TestSuite):
            yield from ids(test)
        else:
            yield test.id()


def coverage():
    inventory = json.loads((OUT / "source-inventory-corrected.json").read_bytes())
    expected = {key: item["sha256"] for key, item in inventory["files"].items()
        if key.startswith("tests/crawl-jobs-v2-observer/")}
    with patch.object(p.subprocess, "Popen", side_effect=p.forbidden_spawn):
        discovered = sorted(ids(unittest.defaultTestLoader.discover(str(SOURCE))))
    reports = {}
    for name in ("tests-022.json", "tests-077.json"):
        value = json.loads((OUT / name).read_bytes())
        assert value["status"] == "PASS" and value["tests_run"] == 335
        assert value["inventory"] == value["started"] == value["passed"] == discovered
        assert value["source_files"] == expected
        assert value["runner_sha256"] == record(OUT / "source/scripts/test-crawl-jobs-v2-observer.py")["sha256"]
        for field in ("failures", "errors", "skipped", "expected_failures", "unexpected_successes"):
            assert value[field] == []
        reports[name] = {**record(OUT / name), "tests_run": value["tests_run"],
            "source_hashes_match_frozen_inventory": True, "inventory_matches_discovery": True}
    historical_path = ROOT / "out/c0-main-sync-2026-10-24/tests-022.json"
    old = json.loads(historical_path.read_bytes())
    assert old["tests_run"] == 318 and old["status"] == "PASS"
    historical_ids = set(old["inventory"])
    assert len(historical_ids) == 318 and historical_ids.issubset(discovered)
    new = sorted(set(discovered) - historical_ids)
    assert len(new) == 17
    write(HERE / "coverage-crosscheck.json", {"pass": True, "reports": reports,
        "discovered_count": len(discovered), "retained_historical_count": len(historical_ids), "new_test_ids": new,
        "historical_report": {"path": str(historical_path.relative_to(ROOT)), **record(historical_path),
            "binding": "Supplemental read-only comparison; not part of the corrected 329-file packet"},
        "source_package_files": len(expected), "broad_suite_reexecuted": False})


def dispatch():
    p.SANDBOX = HERE / "sandbox-dispatch"
    p.SANDBOX.mkdir(mode=0o700)
    rows = []
    for flag in ("-hh", "-Dh", "-hD", "-Hunix:///review-unused.sock", "-creview-unused"):
        with p.review_configs() as dirs, patch.object(p.db, "verify_cli"), p.children('print("benign surrogate only")') as (owned, calls):
            result = p.db.command([*p.db.PREFIX, "container", "inspect", "a" * 64, flag], p.Deadline(3))
            assert result == (0, b"benign surrogate only\n", b"")
            assert len(calls) == 1 and calls[0][0][-1] == flag
            assert owned[0].returncode == 0 and owned[0].stdout.closed and owned[0].stderr.closed
            rows.append({"flag": flag, "passed_cli_guard": True, "reached_popen_wrapper": True,
                "original_argv": calls[0][0], "executed_only": p.PYTHON + ['print("benign surrogate only")'],
                "private_config_removed": not dirs[0].exists()})
    write(HERE / "dispatch-path-evidence.json", {"actual_docker_invoked": False,
        "local_docker_path_present": Path(p.db.DOCKER).exists(), "rows": rows,
        "limits": "Only the Python adapter's dispatch is executed. Popen argv is replaced with an inert Python child; verify_cli is stubbed and temporary allocation redirected inside the review."})


def references():
    destination = HERE / "references"
    destination.mkdir()
    names = ["vendor.mod", "cmd/docker/docker.go", "cli/command/cli.go", "cli-plugins/manager/manager.go",
        "cli-plugins/manager/plugin.go", "vendor/github.com/spf13/pflag/flag.go",
        "vendor/github.com/spf13/cobra/command.go", "cli/command/container/inspect.go"]
    rows = []
    for name in names:
        url = "https://raw.githubusercontent.com/docker/cli/v29.0.0/" + name
        target = destination / name.replace("/", "__")
        with urlopen(url, timeout=30) as response:
            data = response.read(262145)
            assert len(data) <= 262144
        with target.open("xb") as stream:
            stream.write(data)
        rows.append({"url": url, "local_path": str(target.relative_to(HERE)), **record(target)})
    write(HERE / "reference-source-hashes.json", {"upstream_version": "Docker CLI v29.0.0",
        "references": rows, "sources_executed": False,
        "discovery_note": "Context7 resolved /docker/cli and /python/cpython; current docs were supplemented with pinned v29.0.0 source, including its vendored Cobra 1.10.1 and pflag 1.0.10. Initial go.mod lookup was HTTP 404 because this repository uses vendor.mod."})


if __name__ == "__main__":
    os.umask(0o077)
    action = sys.argv[1]
    {"coverage": coverage, "dispatch": dispatch, "references": references}[action]()
    print(action + ": complete")
