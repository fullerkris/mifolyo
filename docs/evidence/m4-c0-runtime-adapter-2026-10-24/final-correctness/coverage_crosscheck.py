"""Verify retained broad-suite bindings, test retention and minimal source delta."""
import ast
import hashlib
import json
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
OUT = HERE.parent
INITIAL = OUT.parent
ROOT = INITIAL.parents[1]
SOURCE = OUT / "source/tests/crawl-jobs-v2-observer"
OLD_SOURCE = INITIAL / "source/tests/crawl-jobs-v2-observer"
sys.dont_write_bytecode = True
sys.path.insert(0, str(SOURCE))


def record(path):
    raw = path.read_bytes()
    return {"bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


def ids(suite):
    for test in suite:
        if isinstance(test, unittest.TestSuite):
            yield from ids(test)
        else:
            yield test.id()


with patch("subprocess.Popen", side_effect=AssertionError("No subprocesses during discovery")):
    discovered = sorted(ids(unittest.defaultTestLoader.discover(str(SOURCE))))
inventory = json.loads((OUT / "source-inventory.json").read_bytes())
expected = {name: item["sha256"] for name, item in inventory["files"].items()
    if name.startswith("tests/crawl-jobs-v2-observer/")}
reports = {}
for name in ("tests-022.json", "tests-077.json"):
    value = json.loads((OUT / name).read_bytes())
    assert value["status"] == "PASS" and value["tests_run"] == 338
    assert value["inventory"] == value["started"] == value["passed"] == discovered
    assert value["source_files"] == expected
    assert value["runner_sha256"] == record(OUT / "source/scripts/test-crawl-jobs-v2-observer.py")["sha256"]
    for field in ("errors", "failures", "skipped", "expected_failures", "unexpected_successes"):
        assert value[field] == []
    reports[name] = {**record(OUT / name), "status": value["status"], "tests_run": value["tests_run"],
        "all_package_and_runner_hashes_match": True, "inventory_matches_fresh_discovery": True}
initial = json.loads((INITIAL / "tests-022.json").read_bytes())
assert initial["tests_run"] == 335 and initial["status"] == "PASS"
assert set(initial["inventory"]).issubset(discovered)
new_tests = sorted(set(discovered) - set(initial["inventory"]))
assert len(new_tests) == 3
historical_path = ROOT / "out/c0-main-sync-2026-10-24/tests-022.json"
historical = json.loads(historical_path.read_bytes())
assert historical["tests_run"] == 318 and historical["status"] == "PASS"
assert set(historical["inventory"]).issubset(initial["inventory"])
assert len(set(discovered) - set(historical["inventory"])) == 20


def node_map(path):
    tree = ast.parse(path.read_text())
    return {getattr(node, "name", f"{type(node).__name__}:{index}"): ast.dump(node, include_attributes=False)
        for index, node in enumerate(tree.body)}


old_ast, new_ast = node_map(OLD_SOURCE / "docker_backend.py"), node_map(SOURCE / "docker_backend.py")
changed_nodes = sorted(name for name in old_ast.keys() | new_ast.keys() if old_ast.get(name) != new_ast.get(name))
assert changed_nodes == ["cli_arguments"], changed_nodes


def tests(path):
    result = {}
    for cls in ast.parse(path.read_text()).body:
        if not isinstance(cls, ast.ClassDef):
            continue
        for method in cls.body:
            if isinstance(method, ast.FunctionDef) and method.name.startswith("test_"):
                result[cls.name + "." + method.name] = ast.dump(method, include_attributes=False)
    return result


old_methods, new_methods = tests(OLD_SOURCE / "test_live_adapter.py"), tests(SOURCE / "test_live_adapter.py")
assert all(new_methods[name] == code for name, code in old_methods.items())
assert len(new_methods) - len(old_methods) == 3
value = {"pass": True, "broad_suite_reexecuted": False, "reports": reports,
    "source_package_files": len(expected), "discovered_tests": len(discovered),
    "initial_tests_retained": len(initial["inventory"]), "historical_tests_retained": len(historical["inventory"]),
    "new_tests_since_initial_review": new_tests, "total_new_since_historical": 20,
    "historical_report": {"path": str(historical_path.relative_to(ROOT)), **record(historical_path),
        "binding": "Supplemental read-only baseline; new packet already binds the original review's prior cross-check"},
    "docker_backend_changed_ast_nodes": changed_nodes,
    "original_live_adapter_test_methods_preserved_verbatim_ast": len(old_methods),
    "lifecycle_source_unchanged": {name: record(SOURCE / name) == record(OLD_SOURCE / name)
        for name in ("streams.py", "local_process.py")}}
with (HERE / "coverage-crosscheck.json").open("x") as stream:
    json.dump(value, stream, sort_keys=True, indent=2)
    stream.write("\n")
print(json.dumps(value, indent=2))
