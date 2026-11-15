"""Preserve the reviewed, uncommitted candidate before changing its Git base."""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(__file__).resolve().parent


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT)


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


assert not git("diff", "--cached", "--name-only")
evidence = ROOT / "docs/evidence/m4-c0-native-preflight-2026-10-17"
source = json.loads((evidence / "final-source-inventory.json").read_bytes())
assert all(sha(ROOT / name) == digest for name, digest in source["files"].items())
exports = json.loads((evidence / "manifest.json").read_bytes())
assert all(sha(evidence / name) == item["sha256"] for name, item in exports["files"].items())
paths = sorted({name.decode() for name in (
    git("diff", "--name-only", "-z").split(b"\0")
    + git("ls-files", "--others", "--exclude-standard", "-z").split(b"\0")
) if name})
assert len(paths) == 120
destination = OUT / "before"
destination.mkdir(mode=0o700)
files = {}
for name in paths:
    original, copy = ROOT / name, destination / name
    assert original.is_file() and not original.is_symlink()
    copy.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(original, copy)
    digest = sha(original)
    assert sha(copy) == digest
    files[name] = {"sha256": digest, "bytes": original.stat().st_size,
                   "mode": original.stat().st_mode & 0o777}
(OUT / "before.patch").write_bytes(git("diff", "--binary"))
record = {"version": 1, "kind": "c0_preflight_before_main_sync",
          "head": git("rev-parse", "HEAD").decode().strip(),
          "branch": git("branch", "--show-current").decode().strip(),
          "index_empty": True, "candidate_paths": len(paths),
          "reviewed_source_paths_verified": len(source["files"]),
          "evidence_exports_verified": len(exports["files"]), "files": files}
with (OUT / "before.json").open("x") as stream:
    json.dump(record, stream, sort_keys=True, indent=2)
    stream.write("\n")
print(json.dumps({key: value for key, value in record.items() if key != "files"}, indent=2))
