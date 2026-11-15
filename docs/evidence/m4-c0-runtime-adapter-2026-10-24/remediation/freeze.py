"""Freeze R1/SEC-01 remediation without changing any earlier review input."""
import difflib
import hashlib
import json
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[3]
OUT = Path(__file__).resolve().parent
BASE = OUT.parent


def record(path):
    with path.open("rb") as stream:
        return {"bytes": path.stat().st_size, "sha256": hashlib.file_digest(stream, "sha256").hexdigest()}


def write(path, value):
    with path.open("x") as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write("\n")


packet = json.loads((BASE / "review-inputs-corrected.json").read_bytes())
for name, item in packet["files"].items():
    assert record(ROOT / name) == item
initial = json.loads((BASE / "source-inventory-corrected.json").read_bytes())
snapshot = OUT / "source"
snapshot.mkdir(mode=0o700)
files, changes, difference = {}, {}, []
for name, item in initial["files"].items():
    path, target = ROOT / name, snapshot / name
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(path, target)
    files[name] = record(target)
    assert files[name] == record(path)
    if files[name] != item:
        changes[name] = {"before": item["sha256"], "after": files[name]["sha256"]}
        difference.extend(difflib.unified_diff((BASE / "source" / name).read_text().splitlines(True),
            path.read_text().splitlines(True), fromfile="initial/" + name, tofile="remediated/" + name))
assert set(changes) == {"tests/crawl-jobs-v2-observer/docker_backend.py", "tests/crawl-jobs-v2-observer/test_live_adapter.py"}
(OUT / "source.patch").write_text("".join(difference))
initial_ids = json.loads((BASE / "tests-022.json").read_bytes())["inventory"]
reports = {}
for mode in ("022", "077"):
    path = OUT / ("tests-" + mode + ".json")
    result = json.loads(path.read_bytes())
    assert result["status"] == "PASS" and result["tests_run"] == 338
    assert result["inventory"] == result["started"] == result["passed"]
    assert set(initial_ids) <= set(result["inventory"])
    assert all(files[name]["sha256"] == digest for name, digest in result["source_files"].items())
    reports[mode] = record(path)
assert not subprocess.check_output(["git", "diff", "--cached", "--name-only"])
write(OUT / "source-inventory.json", {"version": 1, "base": initial["base"], "files": files,
    "changes_since_initial_review": changes, "original_packet_sha256": record(BASE / "review-inputs-corrected.json")["sha256"],
    "tests": reports, "historical_ids_retained": 318, "initial_increment_ids_retained": 335,
    "total_tests": 338, "new_remediation_tests": 3, "findings_locally_remediated": ["R1", "SEC-01"],
    "independent_rereview_pending": True, "native_execution": False})
inputs = sorted(path for path in BASE.rglob("*") if path.is_file() and "__pycache__" not in path.parts)
assert all(not path.is_symlink() for path in inputs)
write(OUT / "review-inputs.json", {"version": 1, "kind": "c0_runtime_adapter_remediation_review_inputs",
    "files": {path.relative_to(ROOT).as_posix(): record(path) for path in inputs}})
print(json.dumps({"files": len(inputs), "source_files": len(files), "source_changes_since_review": list(changes),
    "packet_sha256": record(OUT / "review-inputs.json")["sha256"]}, indent=2))
