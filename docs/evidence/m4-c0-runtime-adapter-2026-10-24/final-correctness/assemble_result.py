"""Assemble source-bound final re-review result and artifact hash manifest."""
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
OUT = HERE.parent
INITIAL = OUT.parent
ROOT = INITIAL.parents[1]


def record(path):
    raw = path.read_bytes()
    return {"bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


def read(name):
    return json.loads((HERE / name).read_bytes())


def write(name, value):
    with (HERE / name).open("x") as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write("\n")


before, after = read("integrity-before.json"), read("integrity-after.json")
assert before["pass"] and after["pass"]
assert before["source_records"] == after["source_records"]
assert before["original_review_exports"] == after["original_review_exports"]
packet = json.loads((OUT / "review-inputs.json").read_bytes())
assert record(OUT / "review-inputs.json")["sha256"] == "b3e66f6bf5cc95194e04548b7cfb4df4978898d4cec1a48aaaa357a1d4be14e7"
for name, expected in packet["files"].items():
    assert record(ROOT / name) == expected, name
for name, expected in after["source_records"].items():
    assert record(ROOT / name) == expected, name
for path in HERE.glob("sandbox-*"):
    assert list(path.iterdir()) == [], path
coverage = read("coverage-crosscheck.json")
assert coverage["pass"] and coverage["docker_backend_changed_ast_nodes"] == ["cli_arguments"]
runs = []
for mode in ("022", "077"):
    old, variants = read("independent-" + mode + ".json"), read("variants-" + mode + ".json")
    bindings = read("replay-binding-" + mode + ".json")
    assert old["status"] == variants["status"] == "PASS"
    assert old["tests_run"] == len(old["passed"]) == 23
    assert variants["tests_run"] == len(variants["passed"]) == 8
    assert not any(old[field] or variants[field] for field in ("errors", "failures", "skipped"))
    assert not old["observations"]["unexpected_help_flags_admitted"]
    assert not old["observations"]["unexpected_endpoint_flag_spellings_admitted"]
    assert not bindings["old_source_modules_loaded"]
    assert bindings["old_probe_sha256"] == old["probe_sha256"] == record(INITIAL / "correctness-review/independent_probes.py")["sha256"]
    assert variants["probe"] == record(HERE / "variant_probes.py")
    assert variants["source"]["sha256"] == after["source_records"]["tests/crawl-jobs-v2-observer/docker_backend.py"]["sha256"]
    runs.append({"umask": mode, "resourcewarning_as_error": True, "original_probes": {"status": "PASS", "tests": 23,
        "result": "independent-" + mode + ".json", "log": "independent-" + mode + ".log",
        "binding_evidence": "replay-binding-" + mode + ".json", "test_methods_modified": False},
        "independent_variants": {"status": "PASS", "test_methods": 8, "guard_rejections": variants["guard_rejections"],
            "guard_acceptances": variants["guard_acceptances"], "typed_builtin_argv_validated": variants["typed_builtin_calls"],
            "simulated_actor_argv_validated": variants["typed_actor_dispatches"],
            "result": "variants-" + mode + ".json", "log": "variants-" + mode + ".log"}})
value = {
    "version": 1, "kind": "c0_runtime_adapter_R1_independent_correctness_rereview",
    "created_at_utc": datetime.now(timezone.utc).isoformat(),
    "verdict": "GO_FOR_CONTINUED_IMPLEMENTATION", "R1_closed": True,
    "scope": "Correctness re-review of R1 remediation for the scoped adapter-hardening boundary only",
    "findings": [], "resolved_findings": [{"id": "R1", "status": "CLOSED", "related_security_finding": "SEC-01",
        "original_report": str((INITIAL / "correctness-review/report.md").relative_to(ROOT)),
        "original_report_sha256": record(INITIAL / "correctness-review/report.md")["sha256"],
        "original_verdict_retained": "NEEDS_REMEDIATION",
        "source_path": str((OUT / "source/tests/crawl-jobs-v2-observer/docker_backend.py").relative_to(ROOT)),
        "lines": [39, 63], "primary_lines": [49, 55],
        "resolution": "Reject all single-dash tokens except the exact last three fixed oracle interpreter arguments when the required complete container-create suffix and nonzero image digest match.",
        "evidence": ["independent-022.json", "independent-077.json", "variants-022.json", "variants-077.json"],
        "scope_limit": "Closes the correctness finding; not an independent security sign-off, remote-rerouting proof or installed CLI/native admission."}],
    "packet": before["packet"], "source_inventory": before["inventory"],
    "integrity": {"before": {"pass": True, **record(HERE / "integrity-before.json")},
        "after": {"pass": True, **record(HERE / "integrity-after.json")},
        "packet_files": 444, "final_frozen_source_files": 63, "working_source_mismatches": [],
        "original_correctness_known_hashes_match": True, "reverified_at_assembly": True,
        "original_review_exports_exact": {name: {"files": item["files"], "exact_match": item["exact_match"]}
            for name, item in after["original_review_exports"].items()}},
    "changes_since_initial_review": after["changes_since_initial_review"],
    "product_changed_ast_nodes": coverage["docker_backend_changed_ast_nodes"],
    "lifecycle_source_unchanged": coverage["lifecycle_source_unchanged"],
    "runs": runs, "total_test_method_executions": 62, "passed_test_method_executions": 62,
    "failures": 0, "errors": 0, "skips": 0,
    "broad_suite_evidence": {"reexecuted_by_reviewer": False, "reported_tests_per_umask": 338,
        "retained_status": "PASS", "umasks": ["022", "077"], "initial_test_ids_retained": 335,
        "historical_test_ids_retained": 318, "new_since_initial": 3, "total_new_since_historical": 20,
        "fresh_discovery_and_all_source_hashes_match": True, "evidence": "coverage-crosscheck.json"},
    "host_assessment": {"source": str((INITIAL / "host-assessment.json").relative_to(ROOT)), **record(INITIAL / "host-assessment.json"),
        "fresh_preflight_performed": False, "provider_or_hypervisor_details_awaited": True,
        "current_host_admitted": False, "daemon_kernel_namespace_pairing_established": False,
        "replacement_vm_created": False, "native_x64_runtime_admitted": False, "arm64_native_deferred": True},
    "actions": {"write_scope": str(HERE.relative_to(ROOT)), "product_code_edited": False,
        "git_state_edited": False, "old_evidence_or_reviews_edited": False, "other_agents_spawned": False,
        "docker_invoked": False, "ssh_invoked": False, "native_actors_invoked": False,
        "calibration_invoked": False, "real_daemon_socket_contacted": False,
        "host_policy_changed": False, "benign_python_children_only_in_original_probe_replay": True,
        "new_variant_controls_subprocesses": 0},
    "limits": [
        "No full-runner, native-runtime, host or installed-CLI-version admission",
        "Original replay config allocations redirected into this review after verifying explicit /tmp request",
        "All typed operation compatibility uses simulated metadata/fake streams, not actual Docker or native execution",
        "Original pinned Docker v29.0.0 parser/help references retained; no claim across every CLI version",
        "No proof of all process-group members, escaped descendants or remote-container absence",
        "Host management details awaited; no VM/collector provisioning or fresh daemon/kernel/namespace pairing",
        "ARM64 native remains deferred despite simulated aarch64 argument compatibility"],
    "report": {"path": "report.md", **record(HERE / "report.md")},
    "supporting_artifacts": {p.relative_to(HERE).as_posix(): record(p) for p in sorted(HERE.rglob("*")) if p.is_file()},
    "artifact_manifest": "review-artifact-sha256.json",
}
write("result.json", value)
write("review-artifact-sha256.json", {"version": 1, "kind": "R1_correctness_rereview_artifacts",
    "excluded_self": "review-artifact-sha256.json", "files": {
        p.relative_to(HERE).as_posix(): record(p) for p in sorted(HERE.rglob("*")) if p.is_file()}})
print(json.dumps({"verdict": value["verdict"], "R1_closed": value["R1_closed"],
    "test_method_executions": 62, "result": record(HERE / "result.json"), "report": record(HERE / "report.md"),
    "manifest": record(HERE / "review-artifact-sha256.json")}, indent=2))
