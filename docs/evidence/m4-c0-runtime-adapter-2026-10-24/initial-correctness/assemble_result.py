"""Write final review results and an immutable-artifact hash index."""
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
OUT = HERE.parent
ROOT = OUT.parents[1]


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
packet = json.loads((OUT / "review-inputs-corrected.json").read_bytes())
assert record(OUT / "review-inputs-corrected.json")["sha256"] == before["packet"]["sha256"]
for name, expected in packet["files"].items():
    assert record(ROOT / name) == expected, name
inventory = json.loads((OUT / "source-inventory-corrected.json").read_bytes())
actual_paths = {p.relative_to(OUT / "source").as_posix() for p in (OUT / "source").rglob("*") if p.is_file()}
assert actual_paths == set(inventory["files"]), sorted(actual_paths ^ set(inventory["files"]))
for path in HERE.glob("sandbox-*"):
    assert list(path.iterdir()) == [], path
probes = [read("independent-" + mode + ".json") for mode in ("022", "077")]
for run in probes:
    assert run["tests_run"] == 23 and len(run["passed"]) == 21
    assert not run["errors"] and not run["skipped"] and len(run["failures"]) == 2
    assert run["probe_sha256"] == record(HERE / "independent_probes.py")["sha256"]
coverage = read("coverage-crosscheck.json")
dispatch = read("dispatch-path-evidence.json")
assert coverage["pass"] and len(dispatch["rows"]) == 5
source = "out/c0-runtime-adapter-2026-10-24/source/tests/crawl-jobs-v2-observer/"
finding = {
    "id": "R1", "priority_marker": "red", "severity": "medium", "blocks_scoped_hardening_signoff": True,
    "title": "Clustered short help flags bypass closed CLI dispatch validation",
    "source_path": source + "docker_backend.py", "lines": [39, 53], "primary_lines": [44, 45],
    "observed": "-hh, -Dh and -hD pass cli_arguments and reach command()'s intercepted Popen boundary; attached -H/-c values also pass.",
    "impact": "Docker v29.0.0's pinned pflag/Cobra/help path can enumerate and execute system-plugin metadata despite the empty private config and disabled hooks.",
    "reachability_limit": "No current higher-level backend method was found to generate these spellings. This is a low-level hardening-contract defect, not a demonstrated high-level authorization bypass or actual plugin execution.",
    "endpoint_limit": "Attached -H/-c acceptance is the same parser gap, not proof of remote rerouting; pinned Docker has independent conflicting-host/context checks.",
    "suggestion": "Validate a closed per-operation grammar or reject noncanonical short flags in the Docker option portion; preserve the exact oracle -I -B argument tail after its image. Reject before binary inspection, config allocation and spawn.",
    "evidence": ["independent-022.json", "independent-077.json", "dispatch-path-evidence.json", "documentation-crosscheck.md"],
}
value = {
    "version": 1, "kind": "c0_runtime_adapter_independent_correctness_review",
    "created_at_utc": datetime.now(timezone.utc).isoformat(), "verdict": "NEEDS_REMEDIATION",
    "scope": "Source-bound x64-host assessment and live-adapter config/plugin/subprocess boundary review; no runner/native certification",
    "review_packet": before["packet"], "source_inventory": before["inventory"],
    "integrity": {"before": {"pass": True, **record(HERE / "integrity-before.json")},
        "after": {"pass": True, **record(HERE / "integrity-after.json")},
        "packet_files": 329, "frozen_source_files": 63, "frozen_source_extra_files": [],
        "working_source_mismatches": [], "packet_reverified_at_assembly": True},
    "changed_paths": inventory["changes"], "findings": [finding],
    "checks": {
        "independent_probes": [{"umask": mode, "tests_run": 23, "passed": 21, "failed": 2, "errors": 0, "skipped": 0,
            "resourcewarning_as_error": True, "result": "FAIL", "result_path": "independent-" + mode + ".json",
            "log_path": "independent-" + mode + ".log"} for mode in ("022", "077")],
        "independent_unique_tests": 23, "total_executions": 46, "total_passes": 42, "total_failures": 4,
        "unique_failing_tests": 2, "finding_root_causes": 1, "dispatch_surrogate_variants": 5,
        "existing_broad_suite": {"umasks": ["022", "077"], "retained_results": "PASS", "tests_per_run": 335,
            "historical_tests_retained": 318, "new_tests": 17, "reexecuted_by_reviewer": False,
            "fresh_discovery_matches": True, "source_hashes_match": True, "evidence": "coverage-crosscheck.json"},
        "lifecycle_control_result": "PASS for all independently exercised lifecycle controls",
        "config_control_result": "PASS with actual temporary allocations redirected inside review directory",
        "metadata_control_result": "PASS with injected metadata only"},
    "host_assessment": {"source": str((OUT / "host-assessment.json").relative_to(ROOT)), **record(OUT / "host-assessment.json"),
        "accepted_as": "Scoped recorded observations, not a fresh full preflight or daemon pairing",
        "current_host_admitted": False, "provider_or_hypervisor_details_supplied": False,
        "replacement_vm_created": False, "native_x64_runtime_admitted": False, "arm64_native_deferred": True},
    "actions": {"product_code_edited": False, "existing_evidence_edited": False, "git_state_edited": False,
        "other_agents_spawned": False, "docker_invoked": False, "ssh_invoked": False, "native_actors_invoked": False,
        "calibration_invoked": False, "real_daemon_socket_contacted": False, "host_policy_changed": False,
        "write_scope": str(HERE.relative_to(ROOT)), "benign_python_children_only": True,
        "process_local_subreaper_used_for_witness_cleanup": True},
    "limits": [
        "Docker binary absent locally; verify_cli stubbed only in surrogate config/dispatch controls",
        "Actual /tmp allocation redirected to review directory while checking production request /tmp and private mode 0700",
        "Pinned Docker v29.0.0 sources were inspected, not compiled/executed; no blanket claim about other versions",
        "Metadata was injected; the previously reviewed AF_UNIX reader was not exercised against a real daemon",
        "No full process-group or escaped-descendant absence proof and no remote-container cleanup proof",
        "No complete runner or native readiness certification; no VM or collector provisioning"],
    "report": {"path": "report.md", **record(HERE / "report.md")},
    "supporting_artifacts": {p.relative_to(HERE).as_posix(): record(p) for p in sorted(HERE.rglob("*")) if p.is_file()},
    "final_artifact_manifest": "review-artifact-sha256.json",
}
write("result.json", value)
write("review-artifact-sha256.json", {"version": 1, "kind": "independent_review_artifacts",
    "excluded_self": "review-artifact-sha256.json", "files": {
        p.relative_to(HERE).as_posix(): record(p) for p in sorted(HERE.rglob("*")) if p.is_file()}})
print(json.dumps({"verdict": value["verdict"], "findings": len(value["findings"]),
    "result": record(HERE / "result.json"), "report": record(HERE / "report.md"),
    "manifest": record(HERE / "review-artifact-sha256.json")}, indent=2))
