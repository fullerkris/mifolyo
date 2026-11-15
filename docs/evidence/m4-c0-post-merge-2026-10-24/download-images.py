"""Retain exact-run GitHub artifact ZIPs and verify digests before extraction."""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path, PurePosixPath
import stat
import subprocess
import zipfile

OUT = Path(__file__).resolve().parent
REPO = "fullerkris/mifolyo"
RUN = 39858333396
COMMIT = "e8a0a415ac2b370319cd57f4c5262ce616a536fb"
EXPECTED = {
    "c0-runtime-images-aarch64": (12461066588, "7cac65afbed201004f1417def2a5c2ea28f1e0e8766ef8c8e1cf4e6d158e22f8"),
    "c0-runtime-images-x86_64": (12459699018, "6b30fdb8d9661fd08d9acd6eaecf0ecf81e45eff603a3e84c61778d8a28ee8e9"),
}


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def api(endpoint, filename):
    raw = subprocess.check_output(["gh", "api", "repos/" + REPO + "/" + endpoint], timeout=120)
    with (OUT / filename).open("xb") as stream:
        stream.write(raw)
    return json.loads(raw)


run = api(f"actions/runs/{RUN}", "run.json")
assert run["id"] == RUN and run["head_sha"] == COMMIT and run["head_branch"] == "main"
assert run["event"] == "push" and run["run_attempt"] == 1
assert run["status"] == "completed" and run["conclusion"] == "success"
jobs = api(f"actions/runs/{RUN}/attempts/1/jobs?per_page=100", "jobs.json")
assert jobs["total_count"] == len(jobs["jobs"]) == 2
assert all(job["head_sha"] == COMMIT and job["conclusion"] == "success" for job in jobs["jobs"])
artifacts = api(f"actions/runs/{RUN}/artifacts?per_page=100", "artifacts.json")
assert artifacts["total_count"] == len(artifacts["artifacts"]) == 2
assert {row["name"] for row in artifacts["artifacts"]} == set(EXPECTED)


def download(row):
    identity, wanted = EXPECTED[row["name"]]
    assert row["id"] == identity and row["digest"] == "sha256:" + wanted
    assert row["expired"] is False
    assert row["workflow_run"]["id"] == RUN and row["workflow_run"]["head_sha"] == COMMIT
    archive = OUT / (row["name"] + ".zip")
    with archive.open("xb") as stream:
        subprocess.run(["gh", "api", f"repos/{REPO}/actions/artifacts/{identity}/zip"],
                       stdout=stream, check=True, timeout=600)
    assert digest(archive) == wanted and archive.stat().st_size == row["size_in_bytes"]
    destination = OUT / row["name"]
    destination.mkdir(mode=0o700)
    files = {}
    with zipfile.ZipFile(archive) as zipped:
        entries = zipped.infolist()
        assert 0 < len(entries) <= 1000
        assert sum(entry.file_size for entry in entries) <= 1024 * 1024 * 1024
        seen = set()
        for entry in entries:
            name = PurePosixPath(entry.filename)
            assert not name.is_absolute() and str(name) == entry.filename.rstrip("/")
            assert name.parts and not any(part in (".", "..") for part in name.parts)
            assert "\\" not in entry.filename and "\0" not in entry.filename
            assert str(name) not in seen
            seen.add(str(name))
            mode = entry.external_attr >> 16
            assert not stat.S_ISLNK(mode) and stat.S_IFMT(mode) in (0, stat.S_IFREG, stat.S_IFDIR)
            target = destination / str(name)
            if entry.is_dir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            assert entry.file_size <= 512 * 1024 * 1024
            target.parent.mkdir(parents=True, exist_ok=True)
            total = 0
            with zipped.open(entry) as source, target.open("xb") as output:
                for block in iter(lambda: source.read(1024 * 1024), b""):
                    total += len(block)
                    assert total <= entry.file_size
                    output.write(block)
            assert total == entry.file_size
            files[str(name)] = {"bytes": total, "sha256": digest(target)}
    result = {"id": identity, "name": row["name"], "commit": COMMIT,
              "archive_sha256": wanted, "archive_bytes": archive.stat().st_size,
              "github_digest_matches": True, "files": files}
    print(json.dumps({"artifact": row["name"], "sha256_verified": True,
                      "files": len(files), "extracted_bytes": sum(x["bytes"] for x in files.values())}), flush=True)
    return result


with ThreadPoolExecutor(max_workers=2) as pool:
    downloaded = list(pool.map(download, artifacts["artifacts"]))
record = {"version": 1, "kind": "c0_hosted_image_download_integrity", "status": "PASS",
          "run_id": RUN, "commit": COMMIT, "attempt": 1, "artifacts": downloaded,
          "image_content_verification_pending": True}
with (OUT / "download-integrity.json").open("x") as stream:
    json.dump(record, stream, sort_keys=True, indent=2)
    stream.write("\n")
