"""Read-only packet/source/export verification; only new review outputs written."""
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import sys

HERE = Path(__file__).resolve().parent
OUT = HERE.parent
INITIAL = OUT.parent
ROOT = INITIAL.parents[1]
EXPECTED = "b3e66f6bf5cc95194e04548b7cfb4df4978898d4cec1a48aaaa357a1d4be14e7"


def record(path):
    data = path.read_bytes()
    return {"bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()}


def tree(directory):
    return {path.relative_to(directory).as_posix(): record(path)
        for path in sorted(directory.rglob("*")) if path.is_file()}


packet_path = OUT / "review-inputs.json"
packet_record = record(packet_path)
assert packet_record["sha256"] == EXPECTED
packet = json.loads(packet_path.read_bytes())
assert len(packet["files"]) == 444
errors = []
for name, expected in packet["files"].items():
    try:
        actual = record(ROOT / name)
        if actual != expected:
            errors.append({"path": name, "expected": expected, "actual": actual})
    except Exception as error:
        errors.append({"path": name, "error": repr(error)})
inventory = json.loads((OUT / "source-inventory.json").read_bytes())
source = tree(OUT / "source")
assert len(source) == 63
assert source == inventory["files"]
working_mismatches = [name for name, expected in source.items() if record(ROOT / name) != expected]
old = json.loads((INITIAL / "source-inventory-corrected.json").read_bytes())
changed = {name: {"before": old["files"].get(name, {}).get("sha256"), "after": item["sha256"]}
    for name, item in source.items() if item != old["files"].get(name)}
assert changed == inventory["changes_since_initial_review"]
assert set(changed) == {"tests/crawl-jobs-v2-observer/docker_backend.py", "tests/crawl-jobs-v2-observer/test_live_adapter.py"}
assert record(INITIAL / "correctness-review/report.md")["sha256"] == "f0fdb199cac0cc03d70a0a7a08146a49148953492bc052e88b8f0f53bf8655e3"
assert record(INITIAL / "correctness-review/result.json")["sha256"] == "0f0c3656f1308658c899a2a293c69c51da63b3b91b17eead1436b0147d909128"
assert record(INITIAL / "correctness-review/review-artifact-sha256.json")["sha256"] == "13346d4428f891fd81db632bd0bdcc54208c3cfd9f8452d8b2de7a957f7d33e3"
exports = {}
for kind in ("correctness", "security"):
    original = INITIAL / (kind + "-review")
    export = ROOT / "docs/evidence/m4-c0-runtime-adapter-2026-10-24" / ("initial-" + kind)
    original_files, exported_files = tree(original), tree(export)
    assert original_files and original_files == exported_files, kind
    exports[kind] = {"original_path": str(original.relative_to(ROOT)), "export_path": str(export.relative_to(ROOT)),
        "files": len(original_files), "exact_match": True, "file_records": original_files}
value = {"checked_at_utc": datetime.now(timezone.utc).isoformat(),
    "packet": {"path": str(packet_path.relative_to(ROOT)), **packet_record}, "packet_files": len(packet["files"]),
    "inventory": {"path": str((OUT / "source-inventory.json").relative_to(ROOT)), **record(OUT / "source-inventory.json")},
    "frozen_source_files": len(source), "source_records": source,
    "changes_since_initial_review": changed, "working_source_mismatches": working_mismatches,
    "original_review_exports": exports, "original_correctness_report_known_hashes_match": True,
    "errors": errors, "pass": not errors and not working_mismatches}
name = sys.argv[1]
assert name in ("integrity-before.json", "integrity-after.json")
with (HERE / name).open("x") as stream:
    json.dump(value, stream, sort_keys=True, indent=2)
    stream.write("\n")
print(json.dumps({key: val for key, val in value.items() if key not in ("source_records", "original_review_exports")}, indent=2))
print("Exact exports:", {key: val["files"] for key, val in exports.items()})
raise SystemExit(0 if value["pass"] else 1)
