"""Verify restoration and freeze the exact merged source for artifact checking."""
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(__file__).resolve().parent
MERGE = "e8a0a415ac2b370319cd57f4c5262ce616a536fb"


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


before = json.loads((OUT / "before.json").read_bytes())
assert git("rev-parse", "HEAD").decode().strip() == MERGE
assert git("rev-parse", "origin/main").decode().strip() == MERGE
assert not git("diff", "--cached", "--name-only")
paths = {name.decode() for name in (
    git("diff", "--name-only", "-z").split(b"\0")
    + git("ls-files", "--others", "--exclude-standard", "-z").split(b"\0")
) if name}
assert paths == set(before["files"])
changed = []
for name, record in before["files"].items():
    path = ROOT / name
    assert not path.is_symlink() and path.stat().st_mode & 0o777 == record["mode"]
    if sha(path.read_bytes()) != record["sha256"]:
        changed.append(name)
assert changed == ["docs/crawl-jobs-v2-plan.md"]
addition = (
    "The [October 22 certificate-validation follow-up](go-x509-security-2026-10-22.md)\n"
    "advances the current Go toolchain and exact oracle pins to **1.26.10** for\n"
    "GO-2026-6809. Offline regeneration preserves the sealed Unicode oracle/data\n"
    "hashes; all six URL/Unicode race-test roots pass. Fresh exact-head hosted CI\n"
    "verification remains pending.\n\n"
).encode()
plan = (ROOT / changed[0]).read_bytes()
assert plan.count(addition) == 1
assert plan.replace(addition, b"") == (OUT / "before" / changed[0]).read_bytes()
(OUT / "restored-plan.md").write_bytes(plan)
source = json.loads((ROOT / "docs/evidence/m4-c0-native-preflight-2026-10-17/final-source-inventory.json").read_bytes())
reviewed = {name: digest for name, digest in source["files"].items() if not name.startswith("docs/")}
assert len(reviewed) == 61 and all(sha((ROOT / name).read_bytes()) == digest for name, digest in reviewed.items())
snapshot = OUT / "merged-source"
snapshot.mkdir()
package = "tests/crawl-jobs-v2-observer"
names = git("ls-tree", "-r", "--name-only", MERGE, "--", package).decode().splitlines()
assert len(names) == 55
names += ["scripts/prepare-crawl-jobs-v2-observer.py", "scripts/prepare-crawl-jobs-v2-observer-images.py",
          "scripts/test-crawl-jobs-v2-observer.py", ".github/workflows/c0-runtime-images.yml"]
source_map = {}
for name in names:
    raw = git("show", MERGE + ":" + name)
    path = snapshot / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(raw)
    source_map[name] = sha(raw)
record = {"version": 1, "kind": "c0_preflight_merged_main_sync", "status": "PASS",
          "base_commit": MERGE, "base_tree": git("rev-parse", MERGE + "^{tree}").decode().strip(),
          "original_base": before["head"], "branch": before["branch"],
          "backup_branch": "backup/c0-native-calibration-before-main-2026-10-24",
          "retained_stash": "2475fda26223c43485b933089274476e497be3ab",
          "pull": "git pull --ff-only origin main", "pull_result": "Already up to date.",
          "restored_candidate_paths": len(paths), "byte_identical_paths": len(paths) - len(changed),
          "plan_only_delta": "Six upstream Go-security follow-up lines retained",
          "plan_sha256": sha(plan), "reviewed_package_and_scripts_verified": 61,
          "index_empty": True, "committed": False, "pushed": False,
          "merged_source_snapshot": str(snapshot.relative_to(ROOT)), "merged_source_files": source_map}
with (OUT / "sync-verification.json").open("x") as stream:
    json.dump(record, stream, sort_keys=True, indent=2)
    stream.write("\n")
print(json.dumps({key: value for key, value in record.items() if key != "merged_source_files"}, indent=2))
