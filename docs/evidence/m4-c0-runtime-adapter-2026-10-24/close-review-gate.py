"""Verify immutable adapter evidence, export final security, and close its scope."""
import difflib
import hashlib
import json
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(__file__).resolve().parent
FINAL = OUT / "remediation"
EXPORT = ROOT / "docs/evidence/m4-c0-runtime-adapter-2026-10-24"
BASE = "e8a0a415ac2b370319cd57f4c5262ce616a536fb"
PACKET = "b3e66f6bf5cc95194e04548b7cfb4df4978898d4cec1a48aaaa357a1d4be14e7"


def record(path):
    assert path.is_file() and not path.is_symlink(), str(path)
    with path.open("rb") as stream:
        return {"bytes": path.stat().st_size, "sha256": hashlib.file_digest(stream, "sha256").hexdigest()}


def load(path):
    return json.loads(path.read_bytes())


def write(path, value):
    with path.open("x") as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write("\n")


def verify(path, expected):
    assert record(path) == {key: expected[key] for key in ("bytes", "sha256")}, str(path)


def manifest(path, base, expected_sha=None):
    if expected_sha:
        assert record(path)["sha256"] == expected_sha, str(path)
    value = load(path)
    for name, expected in value["files"].items():
        assert not Path(name).is_absolute() and ".." not in Path(name).parts
        verify(base / name, expected)
    return {"path": str(path.relative_to(ROOT)), **record(path), "files_verified": len(value["files"])}


def export(original, name):
    target = EXPORT / name
    if target.exists():
        assert record(target) == record(original), str(target)
    else:
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(original, target)
        assert record(target) == record(original)
    return record(target)


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT)


assert git("rev-parse", "HEAD").decode().strip() == BASE
assert not git("diff", "--cached", "--name-only")
branch = git("branch", "--show-current").decode().strip()
assert branch == "feature/crawl-jobs-v2-c0-native-calibration"
backup = "backup/c0-native-calibration-before-main-2026-10-24"
assert git("rev-parse", backup).decode().strip() == "babb2180d05698e64b7d9baf37f9cab14b3c0f7f"
stash = "2475fda26223c43485b933089274476e497be3ab"
assert stash in git("stash", "list", "--format=%H").decode().splitlines()

verified = [
    manifest(OUT / "review-inputs.json", ROOT),
    manifest(OUT / "review-inputs-corrected.json", ROOT,
        "ef5134712918c3b705b6076f6367ff044a669237a7d47632cc677093c3814ccf"),
    manifest(FINAL / "review-inputs.json", ROOT, PACKET),
    manifest(OUT / "before.json", OUT / "before"),
]
assert verified[2]["files_verified"] == 444
assert verified[3]["files_verified"] == 247
inventory = load(FINAL / "source-inventory.json")
assert record(FINAL / "source-inventory.json")["sha256"] == "5a3bbce905d4e49d6b4a89e4492222e5aa8bf2b2d0fa1ad92333f189c585b7db"
assert len(inventory["files"]) == 63
for name, expected in inventory["files"].items():
    verify(ROOT / name, expected)
    verify(FINAL / "source" / name, expected)
assert {p.relative_to(FINAL / "source").as_posix() for p in (FINAL / "source").rglob("*") if p.is_file()} == set(inventory["files"])
before = load(OUT / "before.json")
assert len(before["candidate"]) == 199
delta, patch = {}, []
for name, expected in sorted(inventory["files"].items()):
    if name in before["files"]:
        original = (OUT / "before" / name).read_bytes()
    elif "/native/" in name:
        original = git("show", BASE + ":" + name)
    else:
        original = b""
    current = (FINAL / "source" / name).read_bytes()
    if original != current:
        delta[name] = {"before_sha256": hashlib.sha256(original).hexdigest() if original else None,
                       "after_sha256": expected["sha256"]}
        patch.extend(difflib.unified_diff(original.decode().splitlines(True), current.decode().splitlines(True),
            fromfile="before/" + name, tofile="after/" + name))
assert len(delta) == 5
assert (OUT / "complete-source-delta.patch").read_text() == "".join(patch)

historical = load(ROOT / "out/c0-main-sync-2026-10-24/tests-022.json")
initial = load(OUT / "tests-022.json")
assert len(historical["inventory"]) == 318 and len(initial["inventory"]) == 335
tests = {}
for mode in ("022", "077"):
    path = FINAL / ("tests-" + mode + ".json")
    result = load(path)
    verify(path, inventory["tests"][mode])
    assert result["status"] == "PASS" and result["tests_run"] == 338 and result["commit"] == "local"
    assert not any(result[key] for key in ("errors", "failures", "skipped", "expected_failures", "unexpected_successes"))
    assert result["inventory"] == result["started"] == result["passed"]
    assert len(set(result["inventory"])) == 338
    assert set(historical["inventory"]) <= set(initial["inventory"]) <= set(result["inventory"])
    assert len(result["source_files"]) == 61
    for name, digest in result["source_files"].items():
        assert inventory["files"][name]["sha256"] == digest
    assert record(ROOT / "scripts/test-crawl-jobs-v2-observer.py")["sha256"] == result["runner_sha256"]
    tests[mode] = {"path": str(path.relative_to(ROOT)), **record(path), "tests": 338, "status": "PASS", "commit": "local"}

review_sets = [
    (OUT / "correctness-review", "review-artifact-sha256.json", "initial-correctness", "NEEDS_REMEDIATION"),
    (OUT / "security-review", "review-artifacts.json", "initial-security", "NEEDS_REMEDIATION"),
    (FINAL / "correctness-review", "review-artifact-sha256.json", "final-correctness", "GO_FOR_CONTINUED_IMPLEMENTATION"),
    (FINAL / "security-review", "review-artifacts.json", "final-security", "GO_FOR_CONTINUED_IMPLEMENTATION"),
]
reviews, security_exports = {}, {}
for folder, filename, target, verdict in review_sets:
    verified.append(manifest(folder / filename, folder))
    result = load(folder / "result.json")
    assert result["verdict"] == verdict
    if target.startswith("final-"):
        assert not result["findings"]
    files = {**load(folder / filename)["files"], filename: record(folder / filename)}
    for name in files:
        exported = export(folder / name, target + "/" + name)
        if target == "final-security":
            security_exports[name] = exported
    reviews[target] = {"verdict": verdict, "result": record(folder / "result.json"),
        "report": record(folder / "report.md"), "manifest": record(folder / filename), "exports": len(files)}
correctness = load(FINAL / "correctness-review/result.json")
security = load(FINAL / "security-review/result.json")
assert correctness["R1_closed"] and correctness["total_test_method_executions"] == correctness["passed_test_method_executions"] == 62
assert not any(correctness[key] for key in ("errors", "failures", "skips"))
assert security["resolved_findings"][0]["id"] == "SEC-01" and security["resolved_findings"][0]["status"] == "VERIFIED_REMEDIATED"
assert all(run["tests_run"] == run["passed"] == 13 and not run["failures"] and not run["errors"] for run in security["checks"]["runs"])
assert security["checks"]["compatibility_calls_per_umask"] == 398
assert security["checks"]["actual_cli_arguments_calls_per_umask"] == 140
assert security["checks"]["synthetic_metadata_calls_per_umask"] == 258
assert reviews["final-correctness"]["result"]["sha256"] == "b99a4ea884dfbaac244f7061de4c003e63ea1f445f88d659243cf0f374445659"
assert reviews["final-security"]["result"]["sha256"] == "a7fe04abb47a656173f650dcb4ded8b76b571fbf9ce3324f7e8b76bfe70569f5"

for path, base in [(OUT / "initial-export-map.json", EXPORT), (OUT / "initial-review-exports.json", EXPORT),
                   (FINAL / "export-map.json", EXPORT / "remediation")]:
    verified.append(manifest(path, base))
for directory, expected_count in [("m4-c0-native-preflight-2026-10-17", 111), ("m4-c0-post-merge-2026-10-24", 77)]:
    base = ROOT / "docs/evidence" / directory
    item = manifest(base / "manifest.json", base)
    assert item["files_verified"] == expected_count
    verified.append(item)
retained = manifest(ROOT / "out/c0-hosted-images-2026-10-24/durable-retention.json", ROOT,
    "2313c9b6e8447e4e3d56a23e2490e4265259b30a99fc3499a2dc0c42b11ab3e9")
assert retained["files_verified"] == 568
verified.append(retained)
subprocess.run(["git", "diff", "--check"], cwd=ROOT, check=True)

write(OUT / "final-security-export.json", {"version": 1, "kind": "c0_adapter_final_security_exports", "files": security_exports})
write(OUT / "source-delta.json", {"version": 1, "kind": "c0_adapter_complete_source_delta", "base": BASE,
    "before_snapshot": record(OUT / "before.json"), "final_source_inventory": record(FINAL / "source-inventory.json"),
    "files": delta, "complete_patch": record(OUT / "complete-source-delta.patch"),
    "original_shallow_comparison_preserved": True, "unchanged_native_files": 7})
write(OUT / "closure-verification.json", {"version": 1, "kind": "c0_adapter_closure_verification", "status": "PASS",
    "verified_manifests": verified, "current_source_and_frozen_source_paths": 63, "tests": tests, "reviews": reviews,
    "backup_branch": backup, "retained_stash": stash, "git_diff_check": "PASS", "real_index_empty": True,
    "complete_source_delta_paths": sorted(delta), "test_execution_repeated": False})
gate = {"version": 1, "kind": "c0_runtime_adapter_hardening_gate", "recorded_on": "2026-10-24",
    "status": "GO_FOR_CONTINUED_IMPLEMENTATION", "scope": "Reviewed local adapter metadata/configuration/dispatch and owned subprocess-lifecycle hardening, including R1/SEC-01 remediation.",
    "base_commit": BASE, "branch": branch, "implementation_committed": False, "implementation_pushed": False,
    "real_index_empty": True, "review_packet_sha256": PACKET, "review_packet_files": 444,
    "source_inventory_sha256": record(FINAL / "source-inventory.json")["sha256"], "package_files": 61, "auxiliary_scripts": 2,
    "implementation_delta_paths": sorted(delta), "source_delta_sha256": record(OUT / "source-delta.json")["sha256"],
    "closure_verification_sha256": record(OUT / "closure-verification.json")["sha256"], "test_reports": tests,
    "historical_test_ids_retained": 318, "initial_increment_test_ids_retained": 335, "total_tests_per_umask": 338, "new_tests": 20,
    "reviews": reviews, "closed_findings": ["R1", "SEC-01"], "findings": [],
    "correctness_independent_test_executions": 62, "security_checks_per_umask": 13,
    "security_simulated_calls_per_umask": {"total": 398, "actual_cli_actor_guard_validations": 140, "synthetic_metadata_calls": 258},
    "pre_adapter_candidate_paths_preserved": 199, "pre_adapter_snapshot_files": 247,
    "historical_preflight_exports_verified": 111, "historical_post_merge_exports_verified": 77,
    "predecessor_durable_files_verified": 568, "initial_failed_reviews_preserved": True,
    "initial_fixture_failure_and_freeze_comparison_correction_preserved": True,
    "host_assessment_sha256": record(OUT / "host-assessment.json")["sha256"],
    "host_selection": "dedicated-sibling-x64-vm", "provider_or_hypervisor_details_pending": True,
    "host_admitted": False, "collector_provisioned": False, "collector_ssh_admitted": False,
    "installed_cli_binary_version_admitted": False, "daemon_kernel_namespace_pairing_established": False,
    "full_runner_accepted": False, "runtime_acceptance": False, "execution_authorized": False,
    "native_calibration_performed": False, "native_trials_run": 0, "registry_publication": False,
    "arm64_native_calibration_deferred": True, "arm64_deferred_on": "2026-10-22", "arm64_resume_date": None,
    "pending": ["Obtain provider/project or hypervisor SSH profile and assess a dedicated sibling x64 VM",
        "Establish fresh native host, installed Docker binary/version and daemon/kernel/namespace admission",
        "Provision and admit pinned-SSH collector/context and exact x64 images",
        "Complete trial dispatch, actual-end timing, confinement/resource evidence and failure/cleanup assembly",
        "Review the full execution packet and perform native x64 calibration",
        "Resume native ARM64 acceptance before accepted dual-architecture publication"]}
write(OUT / "final-gate.json", gate)
for name in ("close-review-gate.py", "complete-source-delta.patch", "source-delta.json", "closure-verification.json", "final-gate.json", "final-security-export.json"):
    export(OUT / name, name)
print(json.dumps({"status": gate["status"], "security_exports": len(security_exports),
    "security_result_sha256": reviews["final-security"]["result"]["sha256"],
    "gate_sha256": record(OUT / "final-gate.json")["sha256"], "manifests_verified": len(verified),
    "source_paths_verified": 63, "packet_files_verified": 444}, indent=2))
