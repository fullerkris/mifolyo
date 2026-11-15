"""Read-only review helpers. All outputs are exclusive creates in this directory."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

sys.dont_write_bytecode = True
REVIEW = Path(__file__).resolve().parent
ROOT = REVIEW.parents[2]
HOSTED = REVIEW.parent
SYNC = ROOT / "out/c0-main-sync-2026-10-24"
SOURCE = SYNC / "verified-merged-source"
PACKAGE = "tests/crawl-jobs-v2-observer"
OLD = ROOT / "out/c0-runtime-images-2026-10-17/1f0e82a5ca98f"
CLOSURE = ROOT / "docs/evidence/m4-c0-runtime-images-closure-2026-10-17"
PREFLIGHT = ROOT / "docs/evidence/m4-c0-native-preflight-2026-10-17"
COMMIT = "e8a0a415ac2b370319cd57f4c5262ce616a536fb"
TREE = "aaf57fc74e5799d1b5d21f58cce18747623a7676"
BACKUP = "babb2180d05698e64b7d9baf37f9cab14b3c0f7f"
STASH = "2475fda26223c43485b933089274476e497be3ab"
PACKET = "d63b6e8d9b9bab7d0a883d2cbdaa59eb7a8de8050e56dae28b447a2e638a315d"


def guard(event, args):
    if event == "subprocess.Popen":
        argv = args[1]
        if not (isinstance(argv, (list, tuple)) and argv[:1] == ["git"]):
            raise RuntimeError("Review prohibits non-Git subprocesses")
    if event in ("os.system", "os.exec", "socket.connect", "socket.bind"):
        raise RuntimeError("Review prohibits shell/host/network access")
    if event == "open":
        path, mode, flags = args
        writing = (mode and any(c in mode for c in "wax+")) or flags & (os.O_WRONLY | os.O_RDWR | os.O_CREAT | os.O_TRUNC)
        if writing and not isinstance(path, int) and not Path(path).resolve().is_relative_to(REVIEW):
            raise RuntimeError("Write outside review directory: " + str(path))


sys.addaudithook(guard)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def hashfile(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def record(path):
    assert path.is_file() and not path.is_symlink(), path
    return {"bytes": path.stat().st_size, "sha256": hashfile(path)}


def no_duplicates(pairs):
    result = {}
    for key, value in pairs:
        assert key not in result, ("duplicate JSON key", key)
        result[key] = value
    return result


def load(path):
    return json.loads(path.read_bytes(), object_pairs_hook=no_duplicates)


def write(name, data):
    path = REVIEW / name
    assert path.resolve().is_relative_to(REVIEW)
    with path.open("x") as stream:
        json.dump(data, stream, sort_keys=True, indent=2)
        stream.write("\n")


def git(*args):
    assert args[0] in {"rev-parse", "diff", "ls-files", "ls-tree", "show", "reflog", "status", "for-each-ref"}
    return subprocess.check_output(["git", "-c", "core.fsmonitor=false", "-c", "color.ui=false", *args],
        cwd=ROOT, env=os.environ | {"GIT_OPTIONAL_LOCKS": "0"})


def git_state():
    return {
        "head": git("rev-parse", "HEAD").decode().strip(),
        "branch": git("rev-parse", "--abbrev-ref", "HEAD").decode().strip(),
        "refs": git("for-each-ref", "--format=%(refname) %(objectname)").decode(),
        "index_sha256": hashfile(ROOT / git("rev-parse", "--git-path", "index").decode().strip()),
        "cached_diff": git("diff", "--no-ext-diff", "--no-textconv", "--cached", "--name-only").decode(),
        "unmerged_index": git("ls-files", "--unmerged").decode(),
    }
