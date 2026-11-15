"""Check re-review evidence, then write result and artifact manifest locally."""
import hashlib
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
INITIAL = ROOT.parent
REPO = INITIAL.parents[1]


def record(path):
    raw = path.read_bytes()
    return {"bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


def tree(path):
    return {p.relative_to(path).as_posix(): record(p) for p in sorted(path.rglob("*")) if p.is_file()}


before = json.loads((HERE / "integrity-before.json").read_bytes())
after = json.loads((HERE / "integrity-after.json").read_bytes())
packet_path = ROOT / "review-inputs.json"
packet = json.loads(packet_path.read_bytes())
inventory = json.loads((ROOT / "source-inventory.json").read_bytes())
assert record(packet_path)["sha256"] == before["packet_expected_sha256"] == after["packet"]["sha256"]
assert all(record(REPO / p) == row for p, row in packet["files"].items())
assert tree(ROOT / "source") == inventory["files"]
assert all(record(REPO / p) == row for p, row in inventory["files"].items())
assert all(after[k] for k in after if k.endswith("_unchanged"))
assert before["review_exports_exact"] and after["review_exports_exact"]
assert before["packet_hash_matches"] and after["packet_hash_matches"]
for kind in ("security", "correctness"):
    assert tree(INITIAL / (kind + "-review")) == before["original_reviews"][kind]
    assert tree(REPO / "docs/evidence/m4-c0-runtime-adapter-2026-10-24" / ("initial-" + kind)) == before["review_exports"][kind]

runs = {mode: json.loads((HERE / ("probes-" + mode + ".json")).read_bytes()) for mode in ("022", "077")}
for mode, run in runs.items():
    assert run["mode"] == mode and run["effective_umask"] == oct(int(mode, 8))
    assert run["tests_run"] == run["passed"] == 13 and not run["failures"] and not run["errors"]
    assert not any(run["audit"].values()) and run["scratch_empty"]
    assert not list((HERE / ("scratch-" + mode)).iterdir())
    assert all(row["rejected"] and not row["reached_transport"] for row in run["original_regression"]["observations"])
    assert run["details"]["negative_control"]["status"] == "EXPECTED_FAILURE_DETECTED"
    assert run["details"]["compatibility"]["calls"] == 398
    assert run["details"]["compatibility"]["actual_cli_arguments_calls"] == 140
    assert run["details"]["compatibility"]["metadata_fixture_calls"] == 258
    assert not run["details"]["compatibility"]["proposed_replacement_guard_used"]
    assert run["guard_sha256"] == inventory["files"]["tests/crawl-jobs-v2-observer/docker_backend.py"]["sha256"]
assert runs["022"]["details"] == runs["077"]["details"]
assert runs["022"]["original_regression"] == runs["077"]["original_regression"]
assert "**GO_FOR_CONTINUED_IMPLEMENTATION**" in (HERE / "report.md").read_text()

observed = runs["022"]["details"]
result = {
    "version": 1,
    "kind": "c0_runtime_adapter_SEC01_independent_security_rereview",
    "date": "2026-10-24",
    "verdict": "GO_FOR_CONTINUED_IMPLEMENTATION",
    "findings": [],
    "resolved_findings": [{"id": "SEC-01", "previous_severity": "Low", "status": "VERIFIED_REMEDIATED",
        "location": "remediation/source/tests/crawl-jobs-v2-observer/docker_backend.py:39-63",
        "same_root_cause_as_initial_correctness": "R1",
        "correctness_verdict_reissued": False,
        "remediation": "Reject noncanonical single-dash tokens before side effects, except the final fixed oracle -I/-B tail with exact entrypoint and valid nonzero image digest.",
        "original_regression": runs["022"]["original_regression"],
        "old_guard_negative_control": observed["negative_control"],
        "actual_final_guard_compatibility": observed["compatibility"]}],
    "integrity": {
        "packet_path": str(packet_path.relative_to(REPO)),
        "packet_expected_sha256": before["packet_expected_sha256"],
        "packet_before_sha256": before["packet"]["sha256"],
        "packet_after_sha256": after["packet"]["sha256"],
        "packet_reverified_at_assembly": True,
        "bound_files_verified": 444,
        "snapshot_files_verified": 63,
        "snapshot_extra_or_missing_files": [],
        "working_source_matches_snapshot_and_unchanged": True,
        "authoritative_inventory": "remediation/source-inventory.json",
        "changed_paths": after["actual_changes_since_initial_review"],
        "original_329_input_bindings_carried_forward": True,
        "original_packet_sha256": after["original_packet"]["sha256"],
        "old_review_file_counts": {kind: len(rows) for kind, rows in after["original_reviews"].items()},
        "original_reviews_and_exact_exports_unchanged": True,
    },
    "checks": {
        "runs": [{"umask": mode, "tests_run": run["tests_run"], "passed": run["passed"], "failures": 0, "errors": 0,
                  "resource_warning_filter": run["resource_warning_filter"], "log": "probes-" + mode + ".log",
                  "result": "probes-" + mode + ".json", "audit": run["audit"]} for mode, run in runs.items()],
        "python": runs["022"]["python"],
        "rejection_invocations_per_umask": sum(observed[k]["rejected_calls"] for k in
            ("short_cluster_matrix", "attached_and_value_positions", "malformed_oracle", "tail_scope", "unchanged_gates")),
        "rejection_counts": {k: observed[k]["rejected_calls"] for k in
            ("short_cluster_matrix", "attached_and_value_positions", "malformed_oracle", "tail_scope", "unchanged_gates")},
        "compatibility_calls_per_umask": 398,
        "actual_cli_arguments_calls_per_umask": 140,
        "synthetic_metadata_calls_per_umask": 258,
        "proposed_replacement_used": False,
        "both_umask_observations_identical": True,
        "positive_controls": observed["positive_transport_controls"],
        "unchanged_lifecycle": observed["unchanged_implementation"],
        "new_product_tests_executed_per_umask": 3,
        "full_338_test_suite_rerun": False,
        "supplied_test_reports_verified": after["supplied_test_reports"],
    },
    "preserved_history": {
        "initial_security": {"tests": 15, "passed": 14, "failures": 1, "verdict_preserved": "NEEDS_REMEDIATION"},
        "initial_correctness_each_umask": {"tests": 23, "passed": 21, "failures": 2, "original_verdict_unchanged": True},
        "old_proposed_guard_398_call_evidence_unchanged": True,
        "initial_fixture_expectation_failure_and_correction_unchanged": True,
        "prior_test_ids_retained": 335,
        "historical_test_ids_retained": 318,
        "new_remediation_tests": 3,
        "total_new_since_historical": 20,
    },
    "actions": {
        "write_scope": str(HERE.relative_to(REPO)),
        "product_or_old_evidence_edits": False,
        "git_commands": False,
        "docker_execution": False,
        "ssh_or_remote_host_access": False,
        "native_actors_or_calibration": False,
        "other_agents": False,
        "child_processes_spawned_by_probes": 0,
        "real_daemon_connections": 0,
        "historical_module_top_levels_executed": False,
        "old_guard_substituted_in_memory_only_for_negative_control": True,
        "actual_final_guard_restored_and_verified": True,
        "temporary_fixtures_confined_and_removed": True,
    },
    "limits": {
        "scope": "SEC-01 guard remediation only; not repository-wide or full-runner certification",
        "lifecycle_reexercised": False,
        "installed_cli_binary_admitted": False,
        "fresh_full_preflight": False,
        "daemon_kernel_namespace_pairing_proven": False,
        "process_group_or_remote_container_absence_proven": False,
        "native_x64_or_arm64_accepted": False,
        "native_calibration_performed": False,
        "full_runner_certification": False,
        "execution_authorized": False,
        "host_provider_hypervisor_details_pending": True,
        "host_or_vm_or_collector_changes": False,
    },
    "documentation": {
        "original_pinned_Docker29_notes": {"path": str((INITIAL / "security-review/upstream-notes.md").relative_to(REPO)),
                                          **record(INITIAL / "security-review/upstream-notes.md")},
        "current_context7_used_for_AST_replay": "/python/cpython",
    },
    "artifacts": {p.name: record(p) for p in sorted(HERE.iterdir()) if p.is_file() and p.name not in ("result.json", "review-artifacts.json")},
}
assert result["checks"]["rejection_invocations_per_umask"] == 8362
with (HERE / "result.json").open("x") as handle:
    json.dump(result, handle, indent=2, sort_keys=True)
    handle.write("\n")
manifest = {"kind": "SEC01_security_rereview_artifact_sha256", "excludes_self": True,
            "files": {p.name: record(p) for p in sorted(HERE.iterdir()) if p.is_file() and p.name != "review-artifacts.json"}}
with (HERE / "review-artifacts.json").open("x") as handle:
    json.dump(manifest, handle, indent=2, sort_keys=True)
    handle.write("\n")
print(json.dumps({"verdict": result["verdict"], "resolved_findings": ["SEC-01"], "open_findings": 0,
    "report": record(HERE / "report.md"), "result": record(HERE / "result.json"),
    "manifest": record(HERE / "review-artifacts.json")}, indent=2))
