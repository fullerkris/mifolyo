"""Retain the complete current candidate and adapter sources before editing."""
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
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


assert not git("diff", "--cached", "--name-only")
candidate = sorted({name.decode() for name in (
    git("diff", "--name-only", "-z").split(b"\0") +
    git("ls-files", "--others", "--exclude-standard", "-z").split(b"\0")) if name})
assert len(candidate) == 199
for folder in ("m4-c0-native-preflight-2026-10-17", "m4-c0-post-merge-2026-10-24"):
    base = ROOT / "docs/evidence" / folder
    manifest = json.loads((base / "manifest.json").read_bytes())
    assert all(sha(base / name) == item["sha256"] for name, item in manifest["files"].items())
source = sorted(str(path.relative_to(ROOT)) for path in (ROOT / "tests/crawl-jobs-v2-observer").iterdir()
                if path.is_file()) + ["scripts/test-crawl-jobs-v2-observer.py", "scripts/preflight-crawl-jobs-v2-native-host.py"]
destination = OUT / "before"
destination.mkdir(mode=0o700)
files = {}
for name in sorted(set(candidate + source)):
    original, target = ROOT / name, destination / name
    assert original.is_file() and not original.is_symlink()
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(original, target)
    assert sha(original) == sha(target)
    files[name] = {"sha256": sha(target), "bytes": target.stat().st_size, "mode": target.stat().st_mode & 0o777}
(OUT / "before.patch").write_bytes(git("diff", "--binary"))
record = {"version": 1, "kind": "c0_runtime_adapter_before", "head": git("rev-parse", "HEAD").decode().strip(),
          "branch": git("branch", "--show-current").decode().strip(), "candidate": candidate,
          "index_empty": True, "files": files}
with (OUT / "before.json").open("x") as stream:
    json.dump(record, stream, sort_keys=True, indent=2)
    stream.write("\n")
print(json.dumps({"candidate_paths": len(candidate), "snapshot_files": len(files), "head": record["head"]}))
