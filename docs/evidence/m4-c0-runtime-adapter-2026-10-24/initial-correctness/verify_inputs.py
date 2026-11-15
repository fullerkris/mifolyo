"""Read-only verification; write only new review artifacts beside this script."""
import hashlib
import json
from pathlib import Path
import sys
from datetime import datetime, timezone

HERE = Path(__file__).resolve().parent
OUT = HERE.parent
ROOT = OUT.parents[1]
EXPECTED = "ef5134712918c3b705b6076f6367ff044a669237a7d47632cc677093c3814ccf"


def record(path):
    raw = path.read_bytes()
    return {"bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


packet_path = OUT / "review-inputs-corrected.json"
packet_record = record(packet_path)
assert packet_record["sha256"] == EXPECTED, packet_record
packet = json.loads(packet_path.read_bytes())
assert len(packet["files"]) == 329, len(packet["files"])
errors = []
for name, expected in packet["files"].items():
    try:
        actual = record(ROOT / name)
        if actual != expected:
            errors.append({"path": name, "expected": expected, "actual": actual})
    except Exception as exc:
        errors.append({"path": name, "error": repr(exc)})
inventory_path = OUT / "source-inventory-corrected.json"
inventory = json.loads(inventory_path.read_bytes())
source_records = {}
working_source_mismatches = []
for name, expected in inventory["files"].items():
    actual = record(OUT / "source" / name)
    source_records[name] = actual
    if actual != expected:
        errors.append({"frozen_source_path": name, "expected": expected, "actual": actual})
    if record(ROOT / name) != expected:
        working_source_mismatches.append(name)
result = {
    "checked_at_utc": datetime.now(timezone.utc).isoformat(),
    "packet": {"path": str(packet_path.relative_to(ROOT)), **packet_record},
    "packet_files": len(packet["files"]),
    "inventory": {"path": str(inventory_path.relative_to(ROOT)), **record(inventory_path)},
    "frozen_source_files": len(source_records),
    "source_records": source_records,
    "changed_paths": inventory["changes"],
    "working_source_mismatches": working_source_mismatches,
    "errors": errors,
    "pass": not errors and not working_source_mismatches,
}
name = sys.argv[1]
assert name in {"integrity-before.json", "integrity-after.json"}
with (HERE / name).open("x") as stream:
    json.dump(result, stream, sort_keys=True, indent=2)
    stream.write("\n")
print(json.dumps({k: v for k, v in result.items() if k != "source_records"}, indent=2))
sys.exit(0 if result["pass"] else 1)
