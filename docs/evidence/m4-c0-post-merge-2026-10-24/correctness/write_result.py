"""Seal the completed correctness review, without changing its inputs."""
from review_common import *

image = load(REVIEW / "image-checks.json")
sync = load(REVIEW / "source-sync-checks.json")
summary = load(REVIEW / "summary-bindings.json")
audit = load(REVIEW / "audit-after.json")
assert all(row["status"] == "PASS" for row in (image, sync, summary))
assert audit["frozen_unchanged"] and audit["additional_unchanged"] and audit["git_state_unchanged"] and not audit["failures"]
assert git_state() == audit["git_state"]
adjustment = load(REVIEW / "probe-adjustments.json")
original_probe = (REVIEW / "check_images.py").read_bytes().replace(b'"f" * 64, arch', b'"0" * 64, arch')
assert sha(original_probe) == adjustment["initial_probe_sha256"]
assert hashfile(REVIEW / adjustment["initial_log"]) == adjustment["initial_log_sha256"]
important = [HOSTED / name for name in ("review-inputs.json", "download.py", "download-integrity.json", "verify.py", "verification.json", "run.json", "jobs.json", "artifacts.json", "c0-runtime-images-aarch64.zip", "c0-runtime-images-x86_64.zip")]
important += [SYNC / name for name in ("before.json", "before.patch", "sync-verification.json", "test-continuity.json", "tests-022.json", "tests-077.json", "initial-check-failure.json", "restored-plan.md")]
important += [SOURCE / PACKAGE / name for name in ("image_artifacts.py", "image_build.py")]
important += [PREFLIGHT / name for name in ("final-gate.json", "final-source-inventory.json", "manifest.json")]
important += [REVIEW / name for name in ("audit-before.json", "audit-after.json", "image-checks.json", "source-sync-checks.json", "summary-bindings.json", "report.md")]
result = {
    "version": 1,
    "kind": "c0_merged_main_hosted_images_and_preflight_sync_correctness_review",
    "verdict": "GO_FOR_CONTINUED_IMPLEMENTATION",
    "scope": "Exact merged-main hosted image download/static verification and preservation of the already-reviewed C0-ARCH-05 preflight candidate during main synchronization.",
    "commit": COMMIT, "approved_tree": TREE, "backup_commit": BACKUP, "retained_stash": STASH,
    "packet_sha256": PACKET,
    "findings": [], "blocking_findings": 0,
    "checks_performed": {
        "frozen_files_before_after": 428, "frozen_input_bytes": 729668911,
        "additional_files_before_after": 329, "git_refs_head_branch_index_unchanged": True,
        "merged_source_package_files": 55, "merged_source_auxiliary_files": 3,
        "committed_closure_evidence_files_bound": 48, "old_durable_retention_files_verified": 105,
        "run_id": 39858333396, "run_attempt": 1, "event": "push", "head_branch": "main",
        "hosted_jobs": 2, "zip_sha256_and_size_verified": 2, "zip_crc_and_exact_extracted_files_verified": 169,
        "hosted_test_ids_each_architecture": 288, "hosted_source_hashes_each_architecture": 55,
        "portable_archives_and_config_ids_verified": 6, "layer_digests_verified": 14,
        "independent_layer_to_reconstructed_member_comparisons": 13171,
        "full_canonical_content_records_replayed_exactly": 6,
        "stable_native_oracle_artifact_files_per_architecture": 12,
        "payloads_equal_historical": 6, "required_oracle_elf_closure_per_architecture": 18,
        "command_log_hashes_and_arguments_checked": 122, "recorded_never_started_inspections": 30,
        "stopped_cleanup_records": 10, "host_root_observations": 12,
        "before_snapshot_files_and_stash_blobs_verified": 120, "stash_tracked_paths": 2, "stash_untracked_paths": 118,
        "before_patch_equals_stash_diff": True, "frozen_restored_plan_upstream_inserted_lines": 6,
        "reviewed_current_package_and_script_hashes_unchanged": 61, "historical_preflight_exports_verified": 111,
        "integrated_retained_test_ids_each_report_and_log": 318, "integrated_test_source_hashes_verified": 59,
        "preserved_failed_harness_partial_source_files": 55, "negative_controls_passed": 26,
        "root_metadata_boundary_controls_passed": 2, "aggregate_records_cross_checked": True
    },
    "root_evidence": {
        "hosted_stopped_container_root_mode_all_roles": "0555",
        "oracle_layer_root_header_mode": "0755", "historical_local_oracle_root_mode": "0755",
        "oracle_historical_full_content_delta": ["root_directory"],
        "sources_kept_distinct": True, "payload_equivalence_is_not_root_equivalence": True
    },
    "preserved_harness_failures": [
        {"scope": "supplied sync helper", "path": "out/c0-main-sync-2026-10-24/initial-check-failure.json", "assessment": "Nonexistent auxiliary filename; corrected 58-file snapshot independently verified. Not a product or restoration failure."},
        {"scope": "review negative fixture", "path": str((REVIEW / "probe-adjustments.json").relative_to(ROOT)), "assessment": "All-zero identity correctly failed earlier at DIGEST; valid-but-wrong nonzero fixture reached IMAGE_SAVE_CONFIG. Corrected complete run passed; initial log retained."}
    ],
    "limitations": [
        "Retained GitHub metadata and logs were checked locally; no fresh remote attestation or observation of the hosted execution was obtained.",
        "No native actors, engines, containers, remote hosts, calibration, network/API requests, additional agents, commit or push were invoked.",
        "No broad test suite was rerun. Retained 022/077 execution logs, reports and source hashes were checked; process umasks were not independently measured.",
        "Integrated reports intentionally retain commit=local; source/Git/sync bindings establish the baseline separately.",
        "Concurrent authorized chronology edits are outside the frozen runtime/evidence packet; whole-worktree byte identity is not claimed.",
        "The exact whiteout-free portable layers were replayed; no general OCI layer/runtime equivalence or cross-build timestamp identity is claimed.",
        "Original hosted container export tars were not retained by the upload workflow; portable archives, reconstructed maps and full hosted content records were checked instead.",
        "Required Python/oracle ELF dependency closure is verified; the nonrequired _tkinter extension retains unresolved libtk8.6.so/libtcl8.6.so in both architectures, unchanged from the reviewed baseline.",
        "This review does not authorize execution, admit native hosts, certify runtime adapters, or establish native runtime acceptance."
    ],
    "runtime_acceptance": False, "execution_authorized": False, "native_calibration_performed": False,
    "engine_commands_executed_by_review": 0, "network_requests_by_review": 0,
    "product_or_retained_input_changes_by_review": False, "committed": False, "pushed": False,
    "evidence": {name: str((REVIEW / name).relative_to(ROOT)) for name in ("report.md", "image-checks.json", "source-sync-checks.json", "summary-bindings.json", "audit-before.json", "audit-after.json", "probe-adjustments.json")},
    "relevant_hashes": {str(path.relative_to(ROOT)): record(path) for path in important},
    "next_step": "Continue C0-ARCH-05 implementation; eligible native-host/runtime admission, calibration and independent acceptance remain separately gated."
}
write("result.json", result)
write("review-artifact-sha256.json", {"version": 1, "kind": "correctness_review_artifact_seal",
    "files": {str(path.relative_to(REVIEW)): record(path) for path in sorted(REVIEW.rglob("*")) if path.is_file() and path.name != "review-artifact-sha256.json"}})
print(json.dumps({"verdict": result["verdict"], "report": str((REVIEW / "report.md").relative_to(ROOT)), "result": str((REVIEW / "result.json").relative_to(ROOT))}))
