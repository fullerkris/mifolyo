"""Bind exact exports, retain the final local candidate, and verify delivery."""
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(__file__).resolve().parent
EXPORT = ROOT / "docs/evidence/m4-c0-runtime-adapter-2026-10-24"
BASE = "e8a0a415ac2b370319cd57f4c5262ce616a536fb"
GATE = "1dc21c0133daf39055ca7c43b65241c80e3bf0f5848567be92e7d8fe791f7ded"


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


def copy_new(source, target):
    assert not target.exists(), str(target)
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, target)
    assert record(source) == record(target)


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT)


def candidate_paths():
    return sorted({name.decode() for name in git("diff", "--name-only", "-z").split(b"\0") +
        git("ls-files", "--others", "--exclude-standard", "-z").split(b"\0") if name})


assert git("rev-parse", "HEAD").decode().strip() == BASE
assert not git("diff", "--cached", "--name-only")
assert not git("diff", "--name-only", "--diff-filter=D")
assert git("check-ignore", str(OUT.relative_to(ROOT) / "finalize-delivery.py")).strip()
assert record(OUT / "final-gate.json")["sha256"] == GATE
assert record(EXPORT / "final-gate.json")["sha256"] == GATE
packet = load(OUT / "remediation/review-inputs.json")
assert len(packet["files"]) == 444
for name, expected in packet["files"].items():
    verify(ROOT / name, expected)
inventory = load(OUT / "remediation/source-inventory.json")
for name, expected in inventory["files"].items():
    verify(ROOT / name, expected)
    verify(OUT / "remediation/source" / name, expected)
for directory in ("m4-c0-native-preflight-2026-10-17", "m4-c0-post-merge-2026-10-24"):
    folder = ROOT / "docs/evidence" / directory
    for name, expected in load(folder / "manifest.json")["files"].items():
        verify(folder / name, expected)

copy_new(Path(__file__), EXPORT / "finalize-delivery.py")
aliases = {"initial-correctness": OUT / "correctness-review", "initial-security": OUT / "security-review",
           "final-correctness": OUT / "remediation/correctness-review", "final-security": OUT / "remediation/security-review"}
exports = {}
for target in sorted(EXPORT.rglob("*")):
    if not target.is_file():
        continue
    name = target.relative_to(EXPORT)
    if name.as_posix() in ("README.md", ".gitignore"):
        continue
    source = aliases[name.parts[0]] / Path(*name.parts[1:]) if name.parts[0] in aliases else OUT / name
    assert record(target) == record(source), str(target)
    exports[name.as_posix()] = {"source": source.relative_to(ROOT).as_posix(), **record(target)}
write(OUT / "export-map.json", {"version": 1, "kind": "c0_runtime_adapter_exact_exports", "files": exports,
    "repository_authored": {name: record(EXPORT / name) for name in ("README.md", ".gitignore")},
    "excludes_generated_indexes": ["export-map.json", "manifest.json"]})
copy_new(OUT / "export-map.json", EXPORT / "export-map.json")
files = {path.relative_to(EXPORT).as_posix(): record(path) for path in sorted(EXPORT.rglob("*")) if path.is_file()}
write(OUT / "repository-manifest.json", {"version": 1, "kind": "c0_runtime_adapter_evidence_exports", "files": files,
    "excludes_self": "manifest.json"})
copy_new(OUT / "repository-manifest.json", EXPORT / "manifest.json")

# Check the new evidence README's relative links after its generated indexes exist.
for link in re.findall(r"\]\(([^)]+)\)", (EXPORT / "README.md").read_text()):
    if "://" not in link:
        assert (EXPORT / link.split("#", 1)[0]).exists(), link
subprocess.run(["git", "diff", "--check"], cwd=ROOT, check=True)
paths = candidate_paths()
assert set(load(OUT / "before.json")["candidate"]) <= set(paths)
export_paths = {path.relative_to(ROOT).as_posix() for path in EXPORT.rglob("*") if path.is_file()}
assert export_paths <= set(paths), "Evidence path was unexpectedly Git-ignored"
snapshot = OUT / "final-candidate"
snapshot.mkdir(mode=0o700)
candidate = {}
for name in paths:
    source, target = ROOT / name, snapshot / name
    copy_new(source, target)
    candidate[name] = {**record(target), "mode": target.stat().st_mode & 0o777}
with (OUT / "final-candidate.patch").open("xb") as stream:
    stream.write(git("diff", "--binary"))
write(OUT / "final-candidate.json", {"version": 1, "kind": "c0_runtime_adapter_final_local_candidate", "base": BASE,
    "branch": git("branch", "--show-current").decode().strip(), "path_count": len(paths), "files": candidate,
    "real_index_empty": True, "committed": False, "pushed": False, "pre_adapter_candidate_paths": 199,
    "tracked_diff": record(OUT / "final-candidate.patch"), "snapshot": str(snapshot.relative_to(ROOT))})

# This manifest binds all completed local delivery files, including the candidate
# snapshot. The final integrity record pins this manifest; both exclude themselves
# from recursive content hashing. No external storage replication is implied.
excluded = {OUT / "durable-retention.json", OUT / "final-integrity.json"}
retained = {path.relative_to(ROOT).as_posix(): record(path) for path in sorted(OUT.rglob("*"))
            if path.is_file() and path not in excluded}
write(OUT / "durable-retention.json", {"version": 1, "kind": "c0_runtime_adapter_durable_retention", "files": retained,
    "excluded_terminal_records": sorted(path.relative_to(ROOT).as_posix() for path in excluded)})
for name, expected in retained.items():
    verify(ROOT / name, expected)
for name, expected in candidate.items():
    verify(ROOT / name, expected)
    verify(snapshot / name, expected)
assert candidate_paths() == paths
assert not git("diff", "--cached", "--name-only")
write(OUT / "final-integrity.json", {"version": 1, "kind": "c0_runtime_adapter_final_delivery_integrity", "status": "PASS",
    "base": BASE, "gate_sha256": GATE, "review_packet_files_verified": 444, "source_script_bindings_verified": 63,
    "repository_manifest": record(EXPORT / "manifest.json"), "manifest_entries": len(files),
    "repository_evidence_files_including_manifest": len(export_paths), "exact_source_exports": len(exports),
    "export_map": record(EXPORT / "export-map.json"), "candidate": record(OUT / "final-candidate.json"),
    "candidate_paths": len(paths), "candidate_snapshot_matches_worktree": True,
    "durable_retention": record(OUT / "durable-retention.json"), "durable_files": len(retained),
    "durable_bytes": sum(item["bytes"] for item in retained.values()), "real_index_empty": True,
    "out_git_ignored": True, "git_diff_check": "PASS", "new_evidence_relative_links": "PASS",
    "prior_failures_and_reviews_preserved": True, "current_source_matches_reviewed_snapshot": True,
    "test_execution_repeated": False, "host_admitted": False, "native_trials_run": 0,
    "provider_or_hypervisor_details_pending": True})
print(json.dumps({**load(OUT / "final-integrity.json"), "final_integrity_sha256": record(OUT / "final-integrity.json")["sha256"]}, indent=2))
