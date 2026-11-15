"""Read frozen inputs; write only a named review evidence file beside this script."""
import hashlib
import json
from pathlib import Path
import sys

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[2]
ROOT = HERE.parent
EXPECTED = "ef5134712918c3b705b6076f6367ff044a669237a7d47632cc677093c3814ccf"


def record(path):
    raw = path.read_bytes()
    return {"bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


def main():
    phase = sys.argv[1]
    assert phase in ("before", "after")
    packet_path = ROOT / "review-inputs-corrected.json"
    packet = json.loads(packet_path.read_bytes())
    inventory = json.loads((ROOT / "source-inventory-corrected.json").read_bytes())
    packet_actual = {p: record(REPO / p) for p in packet["files"]}
    source_actual = {p: record(ROOT / "source" / p) for p in inventory["files"]}
    current_actual = {p: record(REPO / p) for p in inventory["files"]}
    before = json.loads((ROOT / "before.json").read_bytes())
    result = {
        "phase": phase,
        "packet": record(packet_path),
        "packet_expected_sha256": EXPECTED,
        "packet_hash_matches": record(packet_path)["sha256"] == EXPECTED,
        "bound_file_count": len(packet_actual),
        "bound_file_mismatches": [p for p, v in packet_actual.items() if v != packet["files"][p]],
        "source_file_count": len(source_actual),
        "source_mismatches": [p for p, v in source_actual.items() if v != inventory["files"][p]],
        "current_source_mismatches": [p for p, v in current_actual.items() if v != inventory["files"][p]],
        "authoritative_changed_paths": inventory["changes"],
        "preexisting_candidate_path_count": len(before["candidate"]),
        "preservation_snapshot_file_count": len(before["files"]),
        "bound_files_actual": packet_actual,
        "current_source_actual": current_actual,
    }
    if phase == "after":
        prior = json.loads((HERE / "integrity-before.json").read_bytes())
        result["bound_files_unchanged_during_review"] = packet_actual == prior["bound_files_actual"]
        result["current_source_unchanged_during_review"] = current_actual == prior["current_source_actual"]
    output = HERE / ("integrity-" + phase + ".json")
    with output.open("x") as handle:
        json.dump(result, handle, indent=2, sort_keys=True)
        handle.write("\n")
    print(json.dumps({k: v for k, v in result.items() if k not in ("bound_files_actual", "current_source_actual")}, indent=2))
    assert result["packet_hash_matches"] and not result["bound_file_mismatches"] and not result["source_mismatches"]


if __name__ == "__main__":
    main()
