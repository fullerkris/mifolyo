"""Freeze the tested adapter increment and its preservation/review inputs."""
import difflib
import hashlib
import json
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(__file__).resolve().parent


def record(path):
    with path.open("rb") as stream:
        return {"sha256": hashlib.file_digest(stream, "sha256").hexdigest(), "bytes": path.stat().st_size}


def write(path, value):
    with path.open("x") as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write("\n")


before = json.loads((OUT / "before.json").read_bytes())
assert subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip() == before["head"]
assert not subprocess.check_output(["git", "diff", "--cached", "--name-only"])
source = sorted(path for path in (ROOT / "tests/crawl-jobs-v2-observer").rglob("*")
                if path.is_file() and "__pycache__" not in path.parts)
source += [ROOT / "scripts/test-crawl-jobs-v2-observer.py", ROOT / "scripts/preflight-crawl-jobs-v2-native-host.py"]
snapshot = OUT / "source"
snapshot.mkdir(mode=0o700)
changes, difference = {}, []
for path in source:
    name = path.relative_to(ROOT).as_posix()
    target = snapshot / name
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(path, target)
    assert record(path) == record(target)
    previous = before["files"].get(name)
    current = record(target)
    if previous is None or previous["sha256"] != current["sha256"]:
        changes[name] = {"before": previous["sha256"] if previous else None, "after": current["sha256"]}
        old = (OUT / "before" / name).read_text().splitlines(True) if previous else []
        difference.extend(difflib.unified_diff(old, target.read_text().splitlines(True), fromfile="before/" + name, tofile="after/" + name))
(OUT / "source.patch").write_text("".join(difference))
historical = json.loads((ROOT / "out/c0-main-sync-2026-10-24/tests-022.json").read_bytes())
reports = {}
for mode in ("022", "077"):
    path = OUT / ("tests-" + mode + ".json")
    result = json.loads(path.read_bytes())
    assert result["status"] == "PASS" and result["tests_run"] == 335
    assert set(historical["inventory"]) <= set(result["inventory"])
    assert result["inventory"] == result["started"] == result["passed"]
    assert all(record(ROOT / name)["sha256"] == digest for name, digest in result["source_files"].items())
    reports[mode] = record(path)
for name, item in before["files"].items():
    assert record(OUT / "before" / name)["sha256"] == item["sha256"]
    if name.startswith("docs/evidence/"):
        assert record(ROOT / name)["sha256"] == item["sha256"]
write(OUT / "source-inventory.json", {"version": 1, "base": before["head"], "changes": changes,
    "files": {path.relative_to(ROOT).as_posix(): record(path) for path in source},
    "tests": reports, "prior_ids_retained": len(historical["inventory"]), "new_tests": 17,
    "native_execution": False, "prior_candidate_paths_preserved": len(before["candidate"])})
inputs = sorted(path for path in OUT.rglob("*") if path.is_file())
write(OUT / "review-inputs.json", {"version": 1, "kind": "c0_runtime_adapter_review_inputs",
    "files": {path.relative_to(ROOT).as_posix(): record(path) for path in inputs}})
print(json.dumps({"source_files": len(source), "changed_paths": list(changes), "frozen_inputs": len(inputs),
    "packet_sha256": record(OUT / "review-inputs.json")["sha256"]}, indent=2))
