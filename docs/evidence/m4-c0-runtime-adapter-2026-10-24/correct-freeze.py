"""Correct the first diff's shallow baseline inventory; retain that first result."""
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(__file__).resolve().parent


def record(path):
    with path.open("rb") as stream:
        return {"sha256": hashlib.file_digest(stream, "sha256").hexdigest(), "bytes": path.stat().st_size}


def write(name, value):
    with (OUT / name).open("x") as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write("\n")


value = json.loads((OUT / "source-inventory.json").read_bytes())
prior = json.loads((ROOT / "out/c0-main-sync-2026-10-24/tests-022.json").read_bytes())
missing = {name: item for name, item in value["changes"].items() if "/native/" in name}
assert len(missing) == 7
for name, item in missing.items():
    raw = subprocess.check_output(["git", "show", value["base"] + ":" + name])
    assert hashlib.sha256(raw).hexdigest() == prior["source_files"][name] == item["after"] == record(ROOT / name)["sha256"]
    del value["changes"][name]
value["unchanged_native_baseline_proof"] = {name: item["after"] for name, item in missing.items()}
value["initial_comparison_correction"] = "capture-before.py snapshots top-level package files and all 199 changed/untracked candidate paths. Seven unchanged tracked native subdirectory files were absent from that extra source snapshot. They match HEAD, prior tested source and current source; the initial freeze incorrectly classified them as additions."
assert len(value["changes"]) == 5
write("source-inventory-corrected.json", value)
inputs = sorted(path for path in OUT.rglob("*") if path.is_file())
write("review-inputs-corrected.json", {"version": 1, "kind": "c0_runtime_adapter_review_inputs",
    "source_inventory": "source-inventory-corrected.json", "supersedes_comparison_only": "review-inputs.json",
    "files": {path.relative_to(ROOT).as_posix(): record(path) for path in inputs}})
print(json.dumps({"changed_paths": list(value["changes"]), "frozen_inputs": len(inputs),
    "packet_sha256": record(OUT / "review-inputs-corrected.json")["sha256"]}, indent=2))
