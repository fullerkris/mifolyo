"""Cross-check supplied aggregate assertions against independently derived checks."""
from review_common import *

actual = load(REVIEW / "image-checks.json")
supplied = load(HOSTED / "verification.json")
assert supplied["version"] == 1 and supplied["kind"] == "c0_merged_main_hosted_image_verification"
assert supplied["status"] == "PASS" and supplied["commit"] == actual["commit"] == COMMIT
assert supplied["run_id"] == actual["run_id"] == 39858333396 and supplied["run_attempt"] == actual["run_attempt"] == 1
assert supplied["engine_commands_executed_by_verifier"] == actual["engine_commands_executed"] == 0
assert all(supplied[name] is False for name in ("runtime_acceptance", "execution_authorized", "native_calibration_performed"))
assert set(supplied["architectures"]) == set(actual["architectures"]) == {"aarch64", "x86_64"}
for arch, row in actual["architectures"].items():
    saved = supplied["architectures"][arch]
    assert saved["artifact_id"] == row["provenance"]["artifact_id"]
    assert saved["artifact_zip_sha256"] == row["provenance"]["zip_sha256"]
    assert saved["downloaded_files_verified"] == row["provenance"]["exact_zip_and_extracted_file_count"]
    assert saved["hosted_tests"] == row["provenance"]["hosted_test_ids_verified"] == 288
    assert saved["source_files_verified"] == row["provenance"]["hosted_sources_verified"] == 55
    assert saved["commands_verified"] == row["commands"]["logs_hashed"] == 61
    assert saved["command_counts"] == row["commands"]["counts"]
    assert saved["stopped_containers_removed"] == len(row["commands"]["cleanup_records"]) == 5
    assert saved["unchanged_stable_artifacts"] == row["stable_artifacts_verified"] == 12
    artifact = HOSTED / ("c0-runtime-images-" + arch)
    assert saved["image_set_sha256"] == hashfile(artifact / "c0-images/image-set.json")
    assert saved["test_report_sha256"] == hashfile(artifact / "c0-offline-tests.json")
    assert set(saved["roles"]) == set(row["roles"])
    for role, image in row["roles"].items():
        claim = saved["roles"][role]
        assert claim["id"] == image["image_id"]
        assert claim["content_sha256"] == image["full_content_record_sha256"]
        assert claim["payload_sha256"] == image["payload_sha256"]
        assert claim["portable_archive"] == image["portable_archive"]
        assert claim["reconstructed_tar_sha256"] == image["reconstructed_tar_sha256"]
        assert claim["elf_count"] == image["elfs"] and claim["required_elf_count"] == image["required_dependency_closure_elfs"]
        assert claim["root_directory"] == image["hosted_root"]
        assert claim["content_replay_exact"] is claim["payload_matches_reviewed_local_image"] is True
        assert claim["root_evidence"] == "two matching stopped-container host observations around export"

sync = load(SYNC / "sync-verification.json")
source = load(REVIEW / "source-sync-checks.json")
assert sync["base_commit"] == COMMIT and sync["base_tree"] == TREE and sync["original_base"] == BACKUP
assert sync["retained_stash"] == STASH
assert sync["restored_candidate_paths"] == source["before_files_verified"] == 120
assert sync["byte_identical_paths"] == 119
assert sync["reviewed_package_and_scripts_verified"] == source["unchanged_reviewed_source_and_scripts"] == 61
assert sync["index_empty"] is True and sync["committed"] is sync["pushed"] is False
gate = load(PREFLIGHT / "final-gate.json")
assert gate["correctness_review_sha256"] == hashfile(PREFLIGHT / "final-correctness/result.json")
assert gate["security_review_sha256"] == hashfile(PREFLIGHT / "final-security/review-summary.json")
assert gate["source_inventory_sha256"] == hashfile(PREFLIGHT / "transport-source-inventory.json")
assert gate["status"] == "GO_FOR_CONTINUED_IMPLEMENTATION" and gate["open_preflight_findings"] == []
assert all(gate[k] is False for k in ("runtime_acceptance", "execution_authorized", "host_admitted", "runtime_adapter_certified"))
write("summary-bindings.json", {"status": "PASS", "hosted_aggregate_matches_independent_checks": True,
    "sync_aggregate_matches_preserved_state": True, "preflight_final_review_bindings_verified": True,
    "runtime_acceptance": False, "execution_authorized": False,
    "hosted_verification_sha256": hashfile(HOSTED / "verification.json"),
    "sync_verification_sha256": hashfile(SYNC / "sync-verification.json"),
    "preflight_final_gate_sha256": hashfile(PREFLIGHT / "final-gate.json")})
print("PASS: aggregate assertions agree with independent checks; preflight final-review bindings retained")
