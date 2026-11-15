"""Materialize the read-only review verdict and hash-bind its artifacts."""
import hashlib
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent


def record(path):
    raw = path.read_bytes()
    return {"bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


before = json.loads((HERE / "integrity-before.json").read_bytes())
after = json.loads((HERE / "integrity-after.json").read_bytes())
probes = json.loads((HERE / "security-probes.json").read_bytes())
extra = json.loads((HERE / "supplementary-probes.json").read_bytes())
assert before["packet_hash_matches"] and after["packet_hash_matches"]
assert after["bound_files_unchanged_during_review"] and after["current_source_unchanged_during_review"]
assert not any(after[k] for k in ("bound_file_mismatches", "source_mismatches", "current_source_mismatches"))
assert probes["tests_run"] == 15 and len(probes["failures"]) == 1 and not probes["errors"]
assert "test_02_regression_clustered_help_must_be_rejected" in probes["failures"][0]["test"]
assert not Path("/usr/bin/docker").exists()
assert not list((HERE / "scratch").iterdir())
assert len(extra["call_shapes"]["calls"]) == 398
assert "**NEEDS_REMEDIATION**" in (HERE / "report.md").read_text()

evidence_files = ["check_integrity.py", "integrity-before.json", "integrity-after.json", "security_probes.py",
                  "security-probes.json", "security-probes.log", "supplementary_probes.py", "supplementary-probes.json",
                  "upstream-notes.md", "report.md", "build_result.py"]
result = {
    "version": 1,
    "kind": "c0_runtime_adapter_independent_read_only_security_review",
    "date": "2026-10-24",
    "verdict": "NEEDS_REMEDIATION",
    "reason": "SEC-01: clustered short help flags bypass the newly claimed closed CLI argument guard; small remediation required, no broader redesign requested.",
    "reviewed_packet": {
        "path": "out/c0-runtime-adapter-2026-10-24/review-inputs-corrected.json",
        "expected_sha256": before["packet_expected_sha256"],
        "before_sha256": before["packet"]["sha256"],
        "after_sha256": after["packet"]["sha256"],
        "bound_files_verified_before_and_after": 329,
        "source_files_verified_before_and_after": 63,
        "bound_files_unchanged": True,
        "current_source_matches_snapshot_and_unchanged": True,
        "authoritative_inventory": "source-inventory-corrected.json",
        "changed_paths": after["authoritative_changed_paths"],
    },
    "findings": [{
        "id": "SEC-01",
        "severity": "Low",
        "cwe": "CWE-184",
        "title": "Clustered shorthand help bypasses the closed-operation argument guard",
        "location": "source/tests/crawl-jobs-v2-observer/docker_backend.py:39-53,104-107",
        "proof": {"argv_tail": ["container", "inspect", "-hh"],
                  "also_accepted": ["-hhh", "-hh=true", "-sh"],
                  "frozen_guard_reached_transport": True,
                  "regression": "SecurityChecks.test_02_regression_clustered_help_must_be_rejected",
                  "upstream_path": "Docker CLI v29.0.0 pflag -hh -> persistent help -> Cobra HelpFunc -> AddPluginCommandStubs -> ListPlugins -> candidate.Metadata exec",
                  "docker_or_plugin_execution_demonstrated": False,
                  "no_untrusted_injection_path_found_through_current_typed_backend": True},
        "impact": "A raw-argv caller can trigger installed plugin metadata execution rather than only an admitted builtin. Plugin compromise and caller reachability are additional prerequisites; no current RCE or authorization bypass is claimed.",
        "cvss": None,
        "cvss_reason": "No deployed external attack path established; scoped hardening-contract defect rather than a scored production exploit.",
        "remediation": "Reject non-emitted single-dash tokens, allowing only the fixed oracle positional -I/-B tail after its image digest; retain current prefix/operation/long-flag gates. Exact copy-paste code in report.md; equivalent review-only control in security_probes.py corrected_guard.",
        "product_fix_applied": False,
        "proposed_guard_focused_safe_forms": 5,
        "proposed_guard_focused_negative_forms": 8,
        "proposed_guard_simulated_backend_calls": 398,
    }],
    "independent_checks": {"probe_tests": 15, "passed": 14, "failed": 1, "errors": 0,
                           "failure_is_retained_defect_regression": True,
                           "interpreter": probes["python"],
                           "resource_warning_filter": "error::ResourceWarning",
                           "supplementary_call_shapes": "PASS",
                           "supplementary_factory_redaction": "PASS",
                           "supplementary_preservation": "PASS",
                           "signal_disabled_mutation_control_detected_survivor": True,
                           "group_escape_control_confirmed_scope_without_absence_claim": True,
                           "full_335_test_suite_rerun": False},
    "preservation": {"before_snapshot_copies_verified": 247, "preexisting_candidate_paths_recorded": 199,
                     "current_historical_evidence_files_matched": 190,
                     "prior_test_ids_retained": 318, "new_test_ids": 17,
                     "supplied_test_report_passes": {"022": 335, "077": 335},
                     "initial_targeted_failure_retained": {"tests": 17, "failures": 1, "cause": "fixture expected arm64 but default x64 reports amd64", "corrected_targeted_passes": 17},
                     "seven_false_native_additions_not_treated_as_changes": True,
                     "seven_native_hashes_match_prior_test_and_corrected_inventory": True,
                     "head_independently_rechecked_with_git": False,
                     "supplemental_prior_report": extra["preservation"]["prior_report"]},
    "host": {"assessment_path": "host-assessment.json", "assessment_sha256": record(ROOT / "host-assessment.json")["sha256"],
             "assessment_revalidated_over_ssh": False, "selected_x64_admitted": False,
             "replacement_vm_created": False, "management_details_pending": True,
             "fresh_full_preflight": False, "daemon_kernel_namespace_pairing_established": False,
             "native_arm64_deferred": True},
    "scope": {"product_source_changed": False, "frozen_inputs_changed": False,
              "git_commands_executed": False, "remote_host_access": False, "other_agents_invoked": False,
              "docker_executed": False, "native_actors_executed": False, "calibration_performed": False,
              "native_or_full_runner_certification": False, "execution_authorized": False,
              "local_docker_binary_present": False, "public_documentation_https_used": True,
              "write_boundary": "out/c0-runtime-adapter-2026-10-24/security-review/",
              "temporary_config_requests_redirected_to_review": True,
              "process_local_subreaper_used_only_for_inert_probe_cleanup": True,
              "probe_scratch_directory_empty": True},
    "remaining_obligations": ["SEC-01 regression and guard fix, offline gates and new immutable packet",
                              "Future host/provider selection and independently approved host preflight",
                              "Installed CLI binary/version admission and native runtime proof",
                              "Daemon/collector namespace pairing and independent remote cleanup proof",
                              "Native calibration and full-runner certification are not supplied"],
    "artifacts": {name: record(HERE / name) for name in evidence_files},
}
with (HERE / "result.json").open("x") as handle:
    json.dump(result, handle, indent=2, sort_keys=True)
    handle.write("\n")
manifest = {"kind": "security_review_artifact_sha256", "excludes_self": True,
            "files": {p.name: record(p) for p in sorted(HERE.iterdir()) if p.is_file() and p.name != "review-artifacts.json"}}
with (HERE / "review-artifacts.json").open("x") as handle:
    json.dump(manifest, handle, indent=2, sort_keys=True)
    handle.write("\n")
print(json.dumps({"verdict": result["verdict"], "report": record(HERE / "report.md"), "result": record(HERE / "result.json"),
                  "review_manifest": record(HERE / "review-artifacts.json"), "open_findings": 1, "severity": "Low"}, indent=2))
