"""Read-only packet/source/preservation checks; all output stays beside this file."""
import hashlib
import json
from pathlib import Path
import sys

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
INITIAL = ROOT.parent
REPO = INITIAL.parents[1]
EXPECTED = "b3e66f6bf5cc95194e04548b7cfb4df4978898d4cec1a48aaaa357a1d4be14e7"
ORIGINAL = "ef5134712918c3b705b6076f6367ff044a669237a7d47632cc677093c3814ccf"


def record(path):
    raw = path.read_bytes()
    return {"bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


def tree(path):
    return {p.relative_to(path).as_posix(): record(p) for p in sorted(path.rglob("*")) if p.is_file()}


def main():
    phase = sys.argv[1]
    assert phase in ("before", "after")
    packet_path = ROOT / "review-inputs.json"
    packet = json.loads(packet_path.read_bytes())
    inventory = json.loads((ROOT / "source-inventory.json").read_bytes())
    old_inventory = json.loads((INITIAL / "source-inventory-corrected.json").read_bytes())
    original_packet = json.loads((INITIAL / "review-inputs-corrected.json").read_bytes())
    actual = {p: record(REPO / p) for p in packet["files"]}
    source = {p: record(ROOT / "source" / p) for p in inventory["files"]}
    current = {p: record(REPO / p) for p in inventory["files"]}
    assert set(source) == set(old_inventory["files"])
    changes = {p: {"before": old_inventory["files"][p]["sha256"], "after": row["sha256"]}
               for p, row in source.items() if row != old_inventory["files"][p]}
    original_reviews, exports = {}, {}
    export_root = REPO / "docs/evidence/m4-c0-runtime-adapter-2026-10-24"
    for kind in ("security", "correctness"):
        original_reviews[kind] = tree(INITIAL / (kind + "-review"))
        exports[kind] = tree(export_root / ("initial-" + kind))
    reports = {}
    prior = json.loads((INITIAL / "tests-022.json").read_bytes())
    historic_path = REPO / "out/c0-main-sync-2026-10-24/tests-022.json"
    historic = json.loads(historic_path.read_bytes())
    for mode in ("022", "077"):
        path = ROOT / ("tests-" + mode + ".json")
        value = json.loads(path.read_bytes())
        assert value["status"] == "PASS" and value["tests_run"] == 338
        assert value["inventory"] == value["started"] == value["passed"]
        assert set(historic["inventory"]) <= set(prior["inventory"]) <= set(value["inventory"])
        assert all(not value[k] for k in ("errors", "failures", "skipped", "expected_failures", "unexpected_successes"))
        assert all(source[p]["sha256"] == digest for p, digest in value["source_files"].items())
        reports[mode] = {"report": record(path), "recorded_passes": value["tests_run"],
                         "prior_retained": len(prior["inventory"]), "historical_retained": len(historic["inventory"]),
                         "new_since_initial_review": sorted(set(value["inventory"]) - set(prior["inventory"])),
                         "new_since_historical": len(set(value["inventory"]) - set(historic["inventory"])),
                         "source_hashes_matched": len(value["source_files"])}
    result = {
        "phase": phase,
        "packet": record(packet_path),
        "packet_expected_sha256": EXPECTED,
        "packet_hash_matches": record(packet_path)["sha256"] == EXPECTED,
        "bound_file_count": len(actual),
        "bound_file_mismatches": [p for p, row in actual.items() if row != packet["files"][p]],
        "source_file_count": len(source),
        "source_mismatches": [p for p, row in source.items() if row != inventory["files"][p]],
        "current_source_mismatches": [p for p, row in current.items() if row != inventory["files"][p]],
        "actual_changes_since_initial_review": changes,
        "changes_match_authoritative_inventory": changes == inventory["changes_since_initial_review"],
        "original_packet": record(INITIAL / "review-inputs-corrected.json"),
        "original_packet_hash_matches": record(INITIAL / "review-inputs-corrected.json")["sha256"] == ORIGINAL,
        "all_original_329_bindings_carried_forward": all(packet["files"].get(p) == row for p, row in original_packet["files"].items()),
        "original_reviews": original_reviews,
        "review_exports": exports,
        "review_exports_exact": exports == original_reviews,
        "supplied_test_reports": reports,
        "supplemental_historical_report": {"path": str(historic_path.relative_to(REPO)), **record(historic_path)},
        "bound_files_actual": actual,
        "current_source_actual": current,
        "no_git_or_docker_or_ssh_invoked": True,
    }
    if phase == "after":
        old = json.loads((HERE / "integrity-before.json").read_bytes())
        for key in ("bound_files_actual", "current_source_actual", "original_reviews", "review_exports", "supplemental_historical_report"):
            result[key + "_unchanged"] = old[key] == result[key]
    with (HERE / ("integrity-" + phase + ".json")).open("x") as handle:
        json.dump(result, handle, indent=2, sort_keys=True)
        handle.write("\n")
    summary = {k: v for k, v in result.items() if k not in ("bound_files_actual", "current_source_actual", "original_reviews", "review_exports")}
    print(json.dumps(summary, indent=2))
    assert result["packet_hash_matches"] and result["original_packet_hash_matches"]
    assert len(actual) == 444 and len(source) == 63 and len(changes) == 2
    assert not result["bound_file_mismatches"] and not result["source_mismatches"] and not result["current_source_mismatches"]
    assert result["changes_match_authoritative_inventory"] and result["review_exports_exact"]
    assert result["all_original_329_bindings_carried_forward"]
    if phase == "after":
        assert all(result[k] for k in result if k.endswith("_unchanged"))


if __name__ == "__main__":
    main()
