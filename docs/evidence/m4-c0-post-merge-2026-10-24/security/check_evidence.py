"""Independent retained-evidence checks. No network, extraction, or Git writes."""
from collections import Counter
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import stat
import subprocess
import sys
import zipfile

ROOT = Path(__file__).resolve().parents[3]
OUT = Path(__file__).resolve().parent
HOSTED = OUT.parent
SYNC = ROOT / "out/c0-main-sync-2026-10-24"
SOURCE = SYNC / "verified-merged-source"
PRE = ROOT / "docs/evidence/m4-c0-native-preflight-2026-10-17"
CLOSURE = ROOT / "docs/evidence/m4-c0-runtime-images-closure-2026-10-17"
OLD = ROOT / "out/c0-runtime-images-2026-10-17/1f0e82a5ca98f"
MAIN = "e8a0a415ac2b370319cd57f4c5262ce616a536fb"
STASH = "2475fda26223c43485b933089274476e497be3ab"
PINS = {
    "aarch64": (12461066588, "7cac65afbed201004f1417def2a5c2ea28f1e0e8766ef8c8e1cf4e6d158e22f8"),
    "x86_64": (12459699018, "6b30fdb8d9661fd08d9acd6eaecf0ecf81e45eff603a3e84c61778d8a28ee8e9"),
}


def need(condition, message):
    if not condition:
        raise ValueError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def digest(path):
    need(path.is_file() and not path.is_symlink(), "nonregular input: " + str(path))
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def pairs(rows):
    result = {}
    for key, value in rows:
        need(key not in result, "duplicate JSON key")
        result[key] = value
    return result


def load(path):
    return json.loads(path.read_bytes(), object_pairs_hook=pairs)


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False, allow_nan=False) + "\n").encode()


def write(name, value):
    with (OUT / name).open("x") as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write("\n")


git_calls = []


def git(*args):
    need(args[0] in ("rev-parse", "cat-file", "ls-tree", "diff", "symbolic-ref"), "non-read-only Git subcommand")
    git_calls.append(list(args))
    return subprocess.check_output(
        ["/usr/bin/git", "--no-pager", "--no-optional-locks", *args], cwd=ROOT,
        env=os.environ | {"GIT_OPTIONAL_LOCKS": "0", "GIT_NO_REPLACE_OBJECTS": "1", "GIT_NO_LAZY_FETCH": "1"},
        timeout=30,
    )


def tree(ref):
    result = {}
    for entry in git("ls-tree", "-r", "-z", ref).split(b"\0"):
        if not entry:
            continue
        header, rawpath = entry.split(b"\t", 1)
        mode, kind, oid = header.decode().split()
        result[rawpath.decode()] = (mode, kind, oid)
    return result


def snapshot(paths):
    return {str(path.relative_to(ROOT)): {"bytes": path.stat().st_size, "sha256": digest(path)} for path in sorted(set(paths))}


def main():
    before = load(SYNC / "before.json")
    sync = load(SYNC / "sync-verification.json")
    inventory = load(CLOSURE / "source-inventory.json")
    retention = load(CLOSURE / "durable-retention.json")
    closure_names = ("source-inventory.json", "local-manifest.json", "tests-022.json", "durable-retention.json", "final-gate.json", "artifact-revalidation.json")
    supplemental = [SYNC / "before" / name for name in before["files"]]
    supplemental += [CLOSURE / name for name in closure_names]
    supplemental += [path for path in (SYNC / "merged-source").rglob("*") if path.is_file()]
    for arch in PINS:
        old = load(OLD / arch / "image-set.json")
        supplemental += [OLD / arch / name for name in ["image-set.json", *old["artifacts"], *(role + "-content.json" for role in old["images"])]]
    supplemental_before = snapshot(supplemental)
    write("supplemental-integrity-before.json", supplemental_before)

    refs = git("rev-parse", "HEAD", "refs/remotes/origin/main", "refs/heads/" + sync["backup_branch"], "refs/stash").decode().splitlines()
    index_path = Path(git("rev-parse", "--absolute-git-dir").decode().strip()) / "index"
    index_before = digest(index_path)
    need(refs == [MAIN, MAIN, before["head"], STASH], "Git references differ from retained sync")
    need(git("symbolic-ref", "--short", "HEAD").decode().strip() == sync["branch"] == before["branch"], "wrong branch")
    need(not git("diff", "--no-ext-diff", "--no-textconv", "--cached", "--name-only"), "index is not empty")
    need(git("rev-parse", MAIN + "^{tree}").decode().strip() == sync["base_tree"], "main tree mismatch")

    main_tree = tree(MAIN)
    merged = sync["merged_source_files"]
    need(merged == inventory["files"] | inventory["auxiliary_files"] and len(merged) == 58, "reviewed merged-source inventory differs")
    need(digest(CLOSURE / "source-inventory.json") == "1f0e82a5ca98f02bf36deb4292501a0b97a38ac8bf567fd170fc09c8d4890376", "old reviewed inventory binding")
    package_names = {n for n in main_tree if n.startswith("tests/crawl-jobs-v2-observer/")}
    need(package_names == set(inventory["files"]) and len(package_names) == 55, "package tree inventory")
    for name, wanted in merged.items():
        mode, kind, oid = main_tree[name]
        need(kind == "blob" and mode in ("100644", "100755"), "source type")
        need(sha(git("cat-file", "blob", oid)) == digest(SOURCE / name) == wanted, "source does not match exact main: " + name)
    for name in closure_names:
        path = CLOSURE / name
        need(sha(git("cat-file", "blob", MAIN + ":" + str(path.relative_to(ROOT)))) == digest(path), "old review metadata does not match main")
    local_manifest = load(CLOSURE / "local-manifest.json")
    for name in ("source-inventory.json", "tests-022.json", "artifact-revalidation.json"):
        need(digest(CLOSURE / name) == local_manifest["files"][name]["sha256"], "closure manifest mismatch")

    # Verify all three preservation routes, not only the summary's PASS value.
    stash_meta = git("cat-file", "-p", STASH).decode().split("\n\n", 1)[0]
    parents = [line.split()[1] for line in stash_meta.splitlines() if line.startswith("parent ")]
    need(len(parents) == 3 and parents[0] == before["head"], "stash parent binding")
    stash_tree, untracked_tree, base_tree = tree(STASH), tree(parents[2]), tree(before["head"])
    need(tree(parents[1]) == base_tree, "stash index differs from clean original index")
    changed = {name for name in set(stash_tree) | set(base_tree) if stash_tree.get(name) != base_tree.get(name)}
    need(changed | set(untracked_tree) == set(before["files"]), "stash missing candidate paths")
    live_changes, modes_changed = [], []
    for name, record in before["files"].items():
        copy = SYNC / "before" / name
        need(digest(copy) == record["sha256"] and copy.stat().st_size == record["bytes"] and stat.S_IMODE(copy.stat().st_mode) == record["mode"], "durable-copy mismatch: " + name)
        mode, kind, oid = (untracked_tree if name in untracked_tree else stash_tree)[name]
        need(kind == "blob" and sha(git("cat-file", "blob", oid)) == record["sha256"] and int(mode, 8) & 0o777 == record["mode"], "stash mismatch: " + name)
        live = ROOT / name
        if digest(live) != record["sha256"]:
            live_changes.append(name)
        if stat.S_IMODE(live.stat().st_mode) != record["mode"]:
            modes_changed.append(name)
    permitted = {"docs/crawl-jobs-v2-plan.md", "docs/crawl-jobs-v2-m4-c0-native-preflight-2026-10-17.md"}
    need(set(live_changes) <= permitted and not modes_changed, "unexpected live candidate change")
    reviewed_code = {name: value for name, value in load(PRE / "final-source-inventory.json")["files"].items() if not name.startswith("docs/")}
    need(len(reviewed_code) == 61 and all(digest(ROOT / name) == wanted for name, wanted in reviewed_code.items()), "61 reviewed paths drifted")
    addition = ("The [October 22 certificate-validation follow-up](go-x509-security-2026-10-22.md)\n"
                "advances the current Go toolchain and exact oracle pins to **1.26.10** for\n"
                "GO-2026-6809. Offline regeneration preserves the sealed Unicode oracle/data\n"
                "hashes; all six URL/Unicode race-test roots pass. Fresh exact-head hosted CI\n"
                "verification remains pending.\n\n").encode()
    restored = (SYNC / "restored-plan.md").read_bytes()
    need(restored.count(addition) == 1 and restored.replace(addition, b"") == (SYNC / "before/docs/crawl-jobs-v2-plan.md").read_bytes(), "roadmap delta")
    need(sha(restored) == sync["plan_sha256"], "restored roadmap hash")
    plan_name = "docs/crawl-jobs-v2-plan.md"
    main_plan = git("cat-file", "blob", MAIN + ":" + plan_name)
    old_plan = git("cat-file", "blob", before["head"] + ":" + plan_name)
    need(main_plan.count(addition) == 1 and main_plan.replace(addition, b"") == old_plan, "six-line addition not exact upstream delta")
    for path in (SYNC / "merged-source").rglob("*"):
        if path.is_file():
            name = str(path.relative_to(SYNC / "merged-source"))
            need(digest(path) == sha(git("cat-file", "blob", MAIN + ":" + name)), "partial initial snapshot contains wrong bytes")

    continuity = load(SYNC / "test-continuity.json")
    historical = PRE / "transport-tests-022.json"
    need(digest(historical) == continuity["historical_report_sha256"], "historical test binding")
    htest = load(historical)
    for mask in ("022", "077"):
        path = SYNC / ("tests-" + mask + ".json")
        test = load(path)
        need(test == htest and test["status"] == "PASS" and test["tests_run"] == 318, "integrated test continuity")
        need(test["inventory"] == test["started"] == test["passed"] and len(set(test["inventory"])) == 318, "integrated test inventory")
        need(all(not test[k] for k in ("errors", "failures", "skipped", "expected_failures", "unexpected_successes")), "integrated tests incomplete")
        need(len(test["source_files"]) == 59 and all(digest(ROOT / name) == wanted for name, wanted in test["source_files"].items()), "integrated source binding")
        need(digest(path) == continuity["suites"][mask]["report_sha256"] and digest(SYNC / ("tests-" + mask + ".log")) == continuity["suites"][mask]["log_sha256"], "continuity log binding")
        need(test["runner_sha256"] == digest(SOURCE / "scripts/test-crawl-jobs-v2-observer.py"), "test runner binding")

    run, jobs, metadata, download = [load(HOSTED / (name + ".json")) for name in ("run", "jobs", "artifacts", "download-integrity")]
    need((run["id"], run["head_sha"], run["head_branch"], run["event"], run["run_attempt"], run["status"], run["conclusion"]) == (39858333396, MAIN, "main", "push", 1, "completed", "success"), "run binding")
    need(run["repository"]["id"] == run["head_repository"]["id"] == 1220983139 and run["repository"]["full_name"] == "fullerkris/mifolyo", "repository binding")
    need(run["path"] == ".github/workflows/c0-runtime-images.yml", "workflow binding")
    need(jobs["total_count"] == len(jobs["jobs"]) == metadata["total_count"] == len(metadata["artifacts"]) == 2, "run member count")
    zip_results = {}
    for arch, (aid, pin) in PINS.items():
        name = "c0-runtime-images-" + arch
        row = next(row for row in metadata["artifacts"] if row["name"] == name)
        retained = next(row for row in download["artifacts"] if row["name"] == name)
        job = next(row for row in jobs["jobs"] if row["name"] == "C0 runtime images (" + arch + ")")
        need((job["run_id"], job["head_sha"], job["run_attempt"], job["conclusion"]) == (39858333396, MAIN, 1, "success"), "job binding")
        need(job["labels"] == ["ubuntu-24.04-arm" if arch == "aarch64" else "ubuntu-24.04"], "runner architecture")
        need(row["id"] == retained["id"] == aid and row["digest"] == "sha256:" + pin and not row["expired"], "artifact metadata pin")
        need(row["workflow_run"]["id"] == 39858333396 and row["workflow_run"]["head_sha"] == MAIN and row["workflow_run"]["head_branch"] == "main", "artifact run binding")
        archive = HOSTED / (name + ".zip")
        need(digest(archive) == retained["archive_sha256"] == pin and archive.stat().st_size == row["size_in_bytes"] == retained["archive_bytes"], "ZIP pin or byte count")
        contents, modes, total = {}, Counter(), 0
        with zipfile.ZipFile(archive) as zipped:
            need(0 < len(zipped.infolist()) <= 1000, "ZIP entry bound")
            for entry in zipped.infolist():
                path = PurePosixPath(entry.filename)
                need(str(path) == entry.filename and not path.is_absolute() and not any(p in (".", "..") for p in path.parts) and not any(c in entry.filename for c in "\\\0\r\n"), "ZIP path")
                mode = entry.external_attr >> 16
                need(stat.S_ISREG(mode) and not entry.is_dir() and not entry.flag_bits & 1, "unexpected ZIP member type/encryption")
                need(entry.filename not in contents and 0 <= entry.file_size <= 512 * 1024 * 1024, "ZIP duplicate/bound")
                with zipped.open(entry) as data:
                    raw_digest = hashlib.file_digest(data, "sha256").hexdigest()
                target = HOSTED / name / entry.filename
                need(digest(target) == raw_digest and target.stat().st_size == entry.file_size, "ZIP to extracted bytes")
                need(not target.stat().st_mode & 0o111, "downloaded executable was made executable locally")
                contents[entry.filename] = {"bytes": entry.file_size, "sha256": raw_digest}
                modes[oct(mode)] += 1
                total += entry.file_size
        need(total <= 1024**3 and contents == retained["files"], "ZIP retained inventory")
        actual = {str(p.relative_to(HOSTED / name)) for p in (HOSTED / name).rglob("*") if p.is_file()}
        need(actual == set(contents), "extra extracted file")
        test = load(HOSTED / name / "c0-offline-tests.json")
        prior_test = load(CLOSURE / "tests-022.json")
        need(test["status"] == "PASS" and test["commit"] == MAIN and test["tests_run"] == 288, "hosted tests binding")
        need(test["inventory"] == test["started"] == test["passed"] == prior_test["inventory"] and len(set(test["inventory"])) == 288, "hosted test IDs")
        need(all(not test[k] for k in ("errors", "failures", "skipped", "expected_failures", "unexpected_successes")), "hosted tests incomplete")
        need(test["source_files"] == inventory["files"] and test["runner_sha256"] == merged["scripts/test-crawl-jobs-v2-observer.py"], "hosted source binding")
        current, old = load(HOSTED / name / "c0-images/image-set.json"), load(OLD / arch / "image-set.json")
        need(current["artifacts"] == old["artifacts"] and len(old["artifacts"]) == 12, "stable artifacts differ")
        for n in ["image-set.json", *old["artifacts"], *(role + "-content.json" for role in old["images"])]:
            need(digest(OLD / arch / n) == retention["files"][arch + "/" + n]["sha256"], "old retained binding")
        for n, wanted in old["artifacts"].items():
            need(digest(OLD / arch / n) == digest(HOSTED / name / "c0-images" / n) == wanted, "stable artifact bytes")
        for role in old["images"]:
            old_content = load(OLD / arch / (role + "-content.json"))
            need(sha(canonical(old_content)) == old["images"][role]["content_sha256"], "old content binding")
            payload = sha(canonical({k: old_content[k] for k in ("declared_files", "elfs")}))
            need(payload == old_content["payload_sha256"] == current["images"][role]["payload_sha256"], "old payload mismatch")
        zip_results[arch] = {"artifact_id": aid, "archive_sha256": pin, "files": len(contents), "uncompressed_bytes": total, "zip_modes": dict(modes), "zip_crc_and_content_hashes_verified": True, "hosted_test_ids": 288, "source_paths": 55, "stable_artifacts": 12}

    supplemental_after = snapshot(supplemental)
    need(supplemental_after == supplemental_before, "supplemental inputs changed during checks")
    write("supplemental-integrity-after.json", supplemental_after)
    need(digest(index_path) == index_before, "Git index changed")
    need(git("rev-parse", "HEAD", "refs/remotes/origin/main", "refs/heads/" + sync["backup_branch"], "refs/stash").decode().splitlines() == refs, "Git refs changed")
    result = {"status": "PASS", "commit": MAIN, "merged_source_paths_verified_against_git": 58, "reviewed_candidate_paths_unchanged": 61,
              "durable_copy_paths_verified": 120, "stash_paths_verified": 120, "stash_tracked_changed_paths": len(changed), "stash_untracked_paths": len(untracked_tree),
              "roadmap_delta_exact_upstream_lines": 6, "live_chronology_changes_observed": live_changes, "retained_stash": STASH,
              "integrated_test_ids_per_report": 318, "integrated_source_paths": 59, "broad_test_suites_rerun": 0,
              "zip_results": zip_results, "supplemental_inputs_verified_before_after": len(supplemental_before),
              "git_index_sha256_before_after": index_before, "git_refs_before_after": refs, "read_only_git_commands": git_calls,
              "network_requests": 0, "image_engine_commands": 0, "image_files_extracted": 0}
    write("evidence-checks.json", result)
    print(json.dumps({k: v for k, v in result.items() if k != "read_only_git_commands"}, sort_keys=True))


if __name__ == "__main__":
    main()
