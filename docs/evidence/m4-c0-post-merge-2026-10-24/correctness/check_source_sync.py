"""Independently establish merged-source, candidate/stash and test continuity."""
import difflib
import re
from review_common import *

state = git_state()
assert state["head"] == COMMIT
assert state["branch"] == "feature/crawl-jobs-v2-c0-native-calibration"
assert not state["cached_diff"] and not state["unmerged_index"]
assert git("rev-parse", "origin/main").decode().strip() == COMMIT
assert git("rev-parse", COMMIT + "^{tree}").decode().strip() == TREE
assert git("rev-parse", "refs/heads/backup/c0-native-calibration-before-main-2026-10-24").decode().strip() == BACKUP
assert git("rev-parse", "refs/stash").decode().strip() == STASH
assert git("rev-parse", STASH + "^1").decode().strip() == BACKUP
assert git("rev-parse", STASH + "^2^{tree}") == git("rev-parse", BACKUP + "^{tree}")
assert not git("diff", "--no-ext-diff", "--no-textconv", "HEAD", "origin/main", "--name-only")

# The historical closure records are committed evidence, not just matching copies.
closure_manifest = load(CLOSURE / "manifest.json")
for path in CLOSURE.rglob("*"):
    if path.is_file():
        assert path.read_bytes() == git("show", COMMIT + ":" + str(path.relative_to(ROOT))), path
for name, row in closure_manifest["files"].items():
    actual = record(CLOSURE / name)
    assert actual["sha256"] == row["sha256"] and actual["bytes"] == row["bytes"], name
closure_inventory = load(CLOSURE / "source-inventory.json")
source_map = closure_inventory["files"] | closure_inventory["auxiliary_files"]
assert len(closure_inventory["files"]) == 55 and len(closure_inventory["auxiliary_files"]) == 3
assert set(git("ls-tree", "-r", "--name-only", COMMIT, "--", PACKAGE).decode().splitlines()) == set(closure_inventory["files"])
assert {str(p.relative_to(SOURCE)) for p in SOURCE.rglob("*") if p.is_file()} == set(source_map)
for name, expected in source_map.items():
    assert sha(git("show", COMMIT + ":" + name)) == expected
    assert hashfile(SOURCE / name) == expected
assert load(SYNC / "sync-verification.json")["merged_source_files"] == source_map
assert load(HOSTED / "verification.json")["merged_source_files"] == source_map

# All historical retained image files, including both old archives and their source snapshot.
retained = load(OLD / "retention.json")
assert retained == load(CLOSURE / "durable-retention.json")
for name, expected in retained["files"].items():
    assert record(OLD / name) == expected, name
assert hashfile(OLD / "final-gate.json") == retained["final_gate_sha256"]
assert hashfile(CLOSURE / "source-inventory.json") == retained["source_inventory_sha256"]

before = load(SYNC / "before.json")
assert before["head"] == BACKUP and before["candidate_paths"] == len(before["files"]) == 120
tracked = set(git("diff", "--no-ext-diff", "--no-textconv", "--name-only", BACKUP, STASH).decode().splitlines())
untracked = set(git("ls-tree", "-r", "--name-only", STASH + "^3").decode().splitlines())
assert not tracked & untracked and tracked | untracked == set(before["files"])
assert git("diff", "--no-ext-diff", "--no-textconv", "--binary", BACKUP, STASH) == (SYNC / "before.patch").read_bytes()
current_deltas = []
for name, row in before["files"].items():
    saved = SYNC / "before" / name
    assert record(saved) == {k: row[k] for k in ("bytes", "sha256")}
    assert saved.stat().st_mode & 0o777 == row["mode"]
    stash_ref = STASH if name in tracked else STASH + "^3"
    assert saved.read_bytes() == git("show", stash_ref + ":" + name)
    path = ROOT / name
    assert not path.is_symlink() and path.stat().st_mode & 0o777 == row["mode"]
    if hashfile(path) != row["sha256"]:
        current_deltas.append(name)

# Test the frozen restored plan, not live chronology that the coordinating session may update.
plan_name = "docs/crawl-jobs-v2-plan.md"
def insertions_only(old, new):
    old, new = old.splitlines(keepends=True), new.splitlines(keepends=True)
    changes = []
    for op, a, b, c, d in difflib.SequenceMatcher(a=old, b=new, autojunk=False).get_opcodes():
        if op != "equal":
            assert op == "insert", (op, a, b, c, d)
            changes.append(new[c:d])
    return changes

upstream_additions = insertions_only(git("show", BACKUP + ":" + plan_name), git("show", COMMIT + ":" + plan_name))
restored_additions = insertions_only((SYNC / "before" / plan_name).read_bytes(), (SYNC / "restored-plan.md").read_bytes())
assert restored_additions == upstream_additions and len(upstream_additions) == 1 and len(upstream_additions[0]) == 6
assert hashfile(SYNC / "restored-plan.md") == load(SYNC / "sync-verification.json")["plan_sha256"]
allowed_live_chronology = {plan_name, "docs/crawl-jobs-v2-m4-c0-native-preflight-2026-10-17.md"}
assert set(current_deltas) <= allowed_live_chronology, current_deltas

final_inventory = load(PREFLIGHT / "final-source-inventory.json")
reviewed = {name: digest for name, digest in final_inventory["files"].items() if not name.startswith("docs/")}
assert len(reviewed) == 61
for name, expected in reviewed.items():
    assert hashfile(ROOT / name) == expected, name
for name, expected in final_inventory["files"].items():
    location = SYNC / "before" / name if name in before["files"] else ROOT / name
    assert hashfile(location) == expected, name
exports = load(PREFLIGHT / "manifest.json")["files"]
assert len(exports) == 111
for name, row in exports.items():
    actual = record(PREFLIGHT / name)
    assert actual["sha256"] == row["sha256"] and actual["bytes"] == row["bytes"], name

partial = SYNC / "merged-source"
partial_names = {str(p.relative_to(partial)) for p in partial.rglob("*") if p.is_file()}
assert partial_names == set(closure_inventory["files"])
for name in partial_names:
    assert hashfile(partial / name) == source_map[name]
assert not git("ls-tree", "-r", "--name-only", COMMIT, "--", "scripts/prepare-crawl-jobs-v2-observer.py")
failure = load(SYNC / "initial-check-failure.json")
assert failure["status"] == "FAILED_VERIFICATION_HARNESS"

historical = load(PREFLIGHT / "transport-tests-022.json")
gate = load(PREFLIGHT / "final-gate.json")
continuity = load(SYNC / "test-continuity.json")
assert hashfile(PREFLIGHT / "transport-tests-022.json") == gate["tests_sha256"] == continuity["historical_report_sha256"]
assert len(historical["source_files"]) == 59
for name, expected in historical["source_files"].items():
    assert hashfile(ROOT / name) == expected, name
assert hashfile(ROOT / "scripts/test-crawl-jobs-v2-observer.py") == historical["runner_sha256"]
reports = {}
for mask in ("022", "077"):
    report = load(SYNC / ("tests-" + mask + ".json"))
    assert report == historical and report["commit"] == "local"
    assert report["status"] == "PASS" and report["tests_run"] == 318
    assert len(report["inventory"]) == len(set(report["inventory"])) == 318
    assert report["inventory"] == report["started"] == report["passed"]
    assert all(report[k] == [] for k in ("skipped", "expected_failures", "unexpected_successes", "failures", "errors"))
    log = SYNC / ("tests-" + mask + ".log")
    ids = []
    for line in log.read_text().splitlines():
        match = re.fullmatch(r"\S+ \(([^)]+)\) \.\.\. ok", line)
        if match:
            ids.append(match[1])
    assert sorted(ids) == report["inventory"]
    assert "Ran 318 tests in " in log.read_text() and "\nOK\nPASS: C0 offline coverage (318 tests)\n" in log.read_text()
    expected = continuity["suites"][mask]
    assert hashfile(log) == expected["log_sha256"]
    assert hashfile(SYNC / ("tests-" + mask + ".json")) == expected["report_sha256"]
    reports[mask] = {"individual_log_passes_verified": len(ids), "source_file_count": len(report["source_files"]),
        "report_sha256": expected["report_sha256"], "log_sha256": expected["log_sha256"], "commit_field": report["commit"]}

assert git_state() == state
result = {"status": "PASS", "commit": COMMIT, "tree": TREE, "backup": BACKUP, "stash": STASH,
    "snapshot_package_files": 55, "snapshot_auxiliary_files": 3,
    "closure_evidence_git_bound_files": sum(p.is_file() for p in CLOSURE.rglob("*")),
    "old_retention_files_verified": len(retained["files"]),
    "before_files_verified": 120, "stash_tracked_files": len(tracked), "stash_untracked_files": len(untracked),
    "before_patch_matches_stash_diff": True, "restored_plan_upstream_added_lines": 6,
    "current_deltas_from_before": current_deltas, "unchanged_reviewed_source_and_scripts": 61,
    "historical_evidence_exports_verified": 111, "preserved_partial_snapshot_files": len(partial_names),
    "initial_helper_failure": failure, "tests": reports, "git_state_unchanged": True,
    "limitations": ["Retained test execution was inspected, not re-run; umasks are run labels, not independently measured here.",
        "Reports use the runner's local marker; the separate sync packet and source hashes bind the integrated baseline.",
        "No pull/rebase/stash apply was repeated; present state, stash contents and branch reflog were inspected read-only."]}
write("source-sync-checks.json", result)
print(json.dumps(result, indent=2))
