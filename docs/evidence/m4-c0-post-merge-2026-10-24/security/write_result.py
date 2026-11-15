"""Assemble the final machine-readable review from completed read-only checks."""
import hashlib
import json
from pathlib import Path

OUT = Path(__file__).resolve().parent
ROOT = OUT.parents[2]


def load(name):
    return json.loads((OUT / name).read_bytes())


def sha(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


before, after = load("integrity-before.json"), load("integrity-after.json")
evidence, replay, final = load("evidence-checks.json"), load("replay-checks.json"), load("final-boundaries.json")
assert all(row["status"] == "PASS" for row in (before, after, evidence, replay, final))
assert before["files"] == after["files"] and not after["mismatches"]
assert replay["control_count"] == 18 and not replay["process_network_audit_events_blocked"]

primary_paths = [
    *("out/c0-hosted-images-2026-10-24/" + name for name in (
        "download.py", "download-integrity.json", "verify.py", "verification.json", "run.json", "jobs.json", "artifacts.json",
        "c0-runtime-images-aarch64.zip", "c0-runtime-images-x86_64.zip")),
    *("out/c0-main-sync-2026-10-24/" + name for name in (
        "before.json", "before.patch", "capture-candidate.py", "check-sync.py", "check-sync-initial.py", "initial-check-failure.json",
        "restored-plan.md", "sync-verification.json", "test-continuity.json", "tests-022.json", "tests-077.json")),
]
primary = {name: after["files"][name] for name in primary_paths}
primary["out/c0-hosted-images-2026-10-24/review-inputs.json"] = {
    "bytes": (OUT.parent / "review-inputs.json").stat().st_size,
    "sha256": after["packet_sha256"],
}
for name, value in primary.items():
    assert sha(ROOT / name) == value["sha256"]

architectures = {}
for arch, record in replay["architectures"].items():
    architectures[arch] = evidence["zip_results"][arch] | {
        "command_counts": record["commands"],
        "never_started_inspection_lifecycles_verified": record["stopped_lifecycles"],
        "host_root_observations_verified": record["root_observations"],
        "roles": {role: {key: value for key, value in image.items() if key not in ("layers", "hardlinks")}
                  for role, image in record["roles"].items()},
    }

generated = {str(path.relative_to(OUT)): {"bytes": path.stat().st_size, "sha256": sha(path)}
             for path in sorted(OUT.rglob("*")) if path.is_file() and path.name != "result.json"}
result = {
    "version": 1,
    "kind": "c0_exact_merged_main_images_and_preserved_arch05_independent_security_review",
    "date": "2026-10-24",
    "verdict": "GO_FOR_CONTINUED_IMPLEMENTATION",
    "status": "PASS",
    "findings": [],
    "finding_counts": {"critical": 0, "high": 0, "medium": 0, "low": 0, "informational": 0},
    "remediation_required": False,
    "commit": after["commit"],
    "tree": "aaf57fc74e5799d1b5d21f58cce18747623a7676",
    "run_id": 39858333396,
    "run_attempt": 1,
    "run_event": "push",
    "run_branch": "main",
    "frozen_packet_sha256": after["packet_sha256"],
    "source_inventory_sha256": "1f0e82a5ca98f02bf36deb4292501a0b97a38ac8bf567fd170fc09c8d4890376",
    "actual_checks": {
        "frozen_files_verified_before_and_after": 428,
        "frozen_bytes_verified_each_pass": after["bytes_checked"],
        "frozen_input_mismatches": [],
        "supplemental_files_verified_before_after_and_at_completion": 213,
        "exact_main_source_blobs_verified": 58,
        "hosted_zip_files_crc_and_hash_verified": 169,
        "image_config_and_archive_identities_verified": 6,
        "layer_digests_verified": 14,
        "independently_compared_final_image_members": 13171,
        "complete_content_records_recomputed_and_equal": 6,
        "elf_records_checked": 1585,
        "required_elf_closure_records_checked": 40,
        "stable_artifacts_equal_to_prior_review_per_architecture": 12,
        "command_logs_verified": 122,
        "never_started_inspection_lifecycles_verified": 10,
        "hosted_offline_test_ids_verified_per_architecture": 288,
        "durable_candidate_copy_paths_verified": 120,
        "stash_candidate_paths_verified": 120,
        "stash_tracked_changed_paths": 2,
        "stash_untracked_paths": 118,
        "prior_reviewed_candidate_source_and_script_hashes_unchanged": 61,
        "retained_upstream_roadmap_lines": 6,
        "integrated_test_report_ids_verified_per_umask": 318,
        "integrated_source_hashes_verified": 59,
        "initial_partial_source_files_verified": 55,
        "targeted_controls": 18,
        "expected_rejection_controls": 16,
        "positive_controls": 2,
        "broad_test_suites_rerun": 0,
    },
    "architectures": architectures,
    "controls": replay["controls"],
    "preservation": {
        "branch": "feature/crawl-jobs-v2-c0-native-calibration",
        "original_base": "babb2180d05698e64b7d9baf37f9cab14b3c0f7f",
        "backup_branch": "backup/c0-native-calibration-before-main-2026-10-24",
        "retained_stash": evidence["retained_stash"],
        "git_index_sha256_unchanged": final["git_index_sha256_unchanged"],
        "refs_unchanged": final["git_refs_unchanged"],
        "permitted_live_chronology_changes_observed_not_certified": evidence["live_chronology_changes_observed"],
        "retained_sync_record_byte_identical_paths": 119,
        "retained_sync_roadmap_delta_separately_verified": True,
    },
    "root_semantics": {
        "hosted_root_modes_all_roles": "0555",
        "host_observations_per_image": 2,
        "oracle_base_layer_root_mode": "0755",
        "prior_reviewed_oracle_root_mode": "0755",
        "prior_reviewed_native_root_mode": "0555",
        "root_layer_headers_and_host_observations_kept_separate": True,
        "future_host_root_mode_inferred": False,
    },
    "boundaries": {
        "image_engine_commands_executed_by_review": 0,
        "remote_calls_by_review": 0,
        "image_paths_extracted_by_review": 0,
        "source_or_retained_inputs_modified_by_review": False,
        "git_mutations_by_review": False,
        "delegation_used": False,
        "writes_confined_to": "out/c0-hosted-images-2026-10-24/security-review/",
        "runtime_adapter_certified": False,
        "native_admission_certified": False,
        "native_calibration_performed": False,
        "execution_authorized": False,
        "runtime_acceptance": False,
        "m4_accepted": False,
    },
    "scope_limits": [
        "Exact retained artifacts and reviewed C0-ARCH-05 synchronization only; not a generic untrusted archive service assessment.",
        "GitHub identity/digest checks use frozen retained responses; no fresh network authentication or runner attestation.",
        "Historical command logs and stopped-state records were verified, not independently observed on hosted runners.",
        "Old/new stable artifact and declared/ELF payload equality does not mean archive/config/root metadata equality.",
        "No fresh builds, broad suite reruns, dependency/CVE audit, native target execution, runtime adapter or host certification.",
        "Runtime image root metadata must be observed independently on any future admitted host.",
        "Current chronology documents may change and are not certified; original candidate copies/stash and frozen evidence were checked.",
        "Retained task-specific scripts use assertions; optimized Python and adversarial concurrent input mutation were not assessed.",
        "Initial snapshot helper failure remains retained; only corrected 58-file source snapshot was imported for checks.",
    ],
    "primary_input_hashes": primary,
    "review_output_hashes": generated,
    "report": "out/c0-hosted-images-2026-10-24/security-review/report.md",
    "report_sha256": generated["report.md"]["sha256"],
}
with (OUT / "result.json").open("x") as stream:
    json.dump(result, stream, sort_keys=True, indent=2)
    stream.write("\n")
print(json.dumps({"verdict": result["verdict"], "report_sha256": result["report_sha256"], "result_sha256": sha(OUT / "result.json")}, sort_keys=True))
