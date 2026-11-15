"""Replay exact merged-main image evidence without loading or running images."""
from collections import Counter, defaultdict
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import sys
import tarfile
import traceback

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(__file__).resolve().parent
SYNC = ROOT / "out/c0-main-sync-2026-10-24"
SOURCE = SYNC / "verified-merged-source"
PACKAGE = SOURCE / "tests/crawl-jobs-v2-observer"
OLD = ROOT / "out/c0-runtime-images-2026-10-17/1f0e82a5ca98f"
COMMIT = "e8a0a415ac2b370319cd57f4c5262ce616a536fb"
sys.dont_write_bytecode = True
sys.path.insert(0, str(PACKAGE))
from contracts import canonical, platform_digest, sha
from image_artifacts import BASES, ImageTree, archive_record, image_id, image_metadata, verify_content
from image_build import ROOT_METADATA_SCRIPT, source_files, verify_artifacts
from oracle_protocol import SOURCE_FILES


def load(path):
    return json.loads(path.read_bytes())


def hashfile(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def same_files(root, files):
    for name, value in files.items():
        path = root / name
        wanted = value if isinstance(value, str) else value["sha256"]
        assert path.is_file() and not path.is_symlink() and hashfile(path) == wanted, name


def reconstruct(portable, inspected, identity, layers, destination):
    """Strictly replay these pinned, whiteout-free layers into an inert tar.

    No archive paths are extracted. Refuse type-changing replacements, hardlink
    target replacement, whiteouts and ambiguous names rather than generalizing
    beyond the six downloaded images. Filesystem root mode comes from the two
    recorded host observations; layer root headers are retained separately.
    """
    rows, hard_targets, details = {}, set(), []
    with tarfile.open(portable, "r:") as saved:
        manifest = json.load(saved.extractfile("manifest.json"))[0]
        raw = saved.extractfile(manifest["Config"]).read()
        assert "sha256:" + sha(raw) == identity
        config = json.loads(raw)
        assert config["config"] == inspected["Config"]
        assert config["rootfs"] == {"type": "layers", "diff_ids": layers}
        assert inspected["RootFS"]["Layers"] == layers
        for layer_name in manifest["Layers"]:
            additions, roots, total = {}, [], 0
            with saved.extractfile(layer_name) as stream, tarfile.open(fileobj=stream, mode="r:") as layer:
                for index, member in enumerate(layer, 1):
                    assert index <= 20000
                    assert not member.name.startswith("/") and ".." not in member.name.split("/")
                    path = str(PurePosixPath(member.name))
                    assert len(path) <= 4096 and not any(c in path for c in "\0\r\n")
                    assert not any(part.startswith(".wh.") for part in PurePosixPath(path).parts)
                    assert 0 <= member.uid <= 2**32 - 1 and 0 <= member.gid <= 2**32 - 1
                    assert member.mode & ~0o7777 == 0
                    if path == ".":
                        assert member.isdir() and not roots and member.size == 0
                        roots.append({"uid": member.uid, "gid": member.gid, "mode": member.mode})
                        continue
                    assert path not in additions
                    assert member.isfile() or member.isdir() or member.issym() or member.islnk()
                    data = None
                    if member.isfile():
                        assert not member.issparse() and 0 <= member.size <= 64 * 1024 * 1024
                        total += member.size
                        assert total <= 512 * 1024 * 1024
                        with layer.extractfile(member) as source:
                            data = source.read(member.size + 1)
                        assert len(data) == member.size
                    else:
                        assert member.size == 0
                    additions[path] = (member, data)
            assert not hard_targets.intersection(additions), "layer replaces an existing hardlink target"
            replaced = []
            for path, value in additions.items():
                member, _ = value
                if path in rows:
                    assert rows[path][0].type == member.type and (member.isdir() or member.isfile())
                    replaced.append(path)
                if member.islnk():
                    assert not member.linkname.startswith("/") and ".." not in member.linkname.split("/")
                    target = str(PurePosixPath(member.linkname))
                    target_row = additions.get(target, rows.get(target))
                    assert target_row is not None and target_row[0].isfile()
                    assert all(getattr(member, key) == getattr(target_row[0], key) for key in ("mode", "uid", "gid"))
                    hard_targets.add(target)
                rows[path] = value
            details.append({"name": layer_name, "entries": len(additions),
                            "root_headers": roots, "replaced_paths": replaced, "whiteouts": 0})
    assert len(rows) <= 20000 and sum(len(raw) for _, raw in rows.values() if raw is not None) <= 512 * 1024 * 1024
    assert not destination.exists()
    with tarfile.open(destination, "x:", format=tarfile.PAX_FORMAT) as flattened:
        for name, (original, raw) in sorted(rows.items()):
            member = tarfile.TarInfo(name)
            member.type, member.mode = original.type, original.mode
            member.uid, member.gid = original.uid, original.gid
            member.linkname, member.size = original.linkname, original.size
            flattened.addfile(member, io.BytesIO(raw) if raw is not None else None)
    return details


def commands_and_cleanup(directory, record):
    assert len(record["commands"]) == 61
    observations, created, inspected, removed, absent = defaultdict(list), {}, Counter(), [], []
    counts = Counter()
    for index, command in enumerate(record["commands"]):
        args = command["argv"]
        assert args[:2] == ["/usr/bin/podman", "--remote=false"]
        assert command["log"] == f"command-{index:03d}.log"
        log = directory / command["log"]
        assert command["returncode"] == 0 and hashfile(log) == command["log_sha256"]
        op = args[2]
        counts[op] += 1
        assert op in {"info", "version", "pull", "image", "build", "create", "container", "unshare", "cp", "export", "rm", "ps", "save"}
        if op == "info":
            assert log.read_bytes().strip() == b"true"
        elif op == "create":
            cid = log.read_text().strip()
            assert image_id(cid) == "sha256:" + cid and cid not in created
            assert args[args.index("--entrypoint") + 1] == "/__obs1_inspection_only__"
            assert args[args.index("--network") + 1] == "none" and "--read-only" in args
            assert args[args.index("--user") + 1] == "999:999"
            assert args[args.index("--cap-drop") + 1] == "ALL" and "no-new-privileges" in args
            created[cid] = {"name": args[args.index("--name") + 1], "image": image_id(args[-1])}
        elif op == "container":
            assert args[3] == "inspect"
            rows = load(log)
            assert len(rows) == 1
            value, cid = rows[0], args[4]
            assert image_id(value["Id"]) == image_id(cid)
            assert image_id(value["Image"]) == created[cid]["image"]
            assert value["Name"].lstrip("/") == created[cid]["name"]
            state = value["State"]
            assert state["Running"] is False and type(state["Pid"]) is int and state["Pid"] == 0
            assert state["Status"] in ("created", "configured")
            assert state["StartedAt"] in ("0001-01-01T00:00:00Z", "0001-01-01T00:00:00+00:00")
            inspected[cid] += 1
        elif op == "unshare":
            assert len(args) == 10 and args[4:7] == ["-I", "-B", "-c"]
            assert args[7] == ROOT_METADATA_SCRIPT and args[8] == args[0] and args[9] in created
            observations[args[9]].append((index, load(log)))
        elif op == "rm":
            assert len(args) == 4 and args[3] in created
            removed.append(args[3])
        elif op == "ps":
            assert not log.read_bytes().strip()
            selected = args[args.index("--filter") + 1]
            assert selected.startswith("id=") and selected[3:] in removed
            absent.append(selected[3:])
    assert len(created) == len(set(removed)) == len(set(absent)) == len(record["cleanup"]) == 5
    assert all(inspected[cid] == 3 for cid in created)
    assert len(observations) == 3 and all(len(rows) == 2 for rows in observations.values())
    assert {row["id"].removeprefix("sha256:") for row in record["cleanup"]} == set(created)
    for row in record["cleanup"]:
        cid = row["id"].removeprefix("sha256:")
        assert row["removed"] is True and row["running"] is False and row["pid"] == 0
        assert row["name"] == created[cid]["name"] and row["image"] == created[cid]["image"]
    return observations, dict(counts)


def main():
    sync = load(SYNC / "sync-verification.json")
    same_files(SOURCE, sync["merged_source_files"])
    current_sources = source_files(PACKAGE)
    assert len(current_sources) == 55
    download = load(OUT / "download-integrity.json")
    assert download["status"] == "PASS" and download["commit"] == COMMIT and download["run_id"] == 39858333396
    prior_tests = load(ROOT / "docs/evidence/m4-c0-runtime-images-closure-2026-10-17/tests-022.json")
    source_bytes = {name: (PACKAGE / name).read_bytes() for name in (*SOURCE_FILES, "build_oracle.py")}
    result = {"version": 1, "kind": "c0_merged_main_hosted_image_verification", "status": "PASS",
              "commit": COMMIT, "run_id": 39858333396, "run_attempt": 1,
              "merged_source_files": sync["merged_source_files"], "architectures": {},
              "engine_commands_executed_by_verifier": 0, "runtime_acceptance": False,
              "execution_authorized": False, "native_calibration_performed": False}
    derived = OUT / "reconstructed"
    derived.mkdir()
    for downloaded in download["artifacts"]:
        artifact = OUT / downloaded["name"]
        same_files(artifact, downloaded["files"])
        assert hashfile(OUT / (downloaded["name"] + ".zip")) == downloaded["archive_sha256"]
        arch = downloaded["name"].removeprefix("c0-runtime-images-")
        job = next(job for job in load(OUT / "jobs.json")["jobs"] if job["name"] == f"C0 runtime images ({arch})")
        assert ("ubuntu-24.04-arm" if arch == "aarch64" else "ubuntu-24.04") in job["labels"]
        test = load(artifact / "c0-offline-tests.json")
        assert test["commit"] == COMMIT and test["status"] == "PASS" and test["tests_run"] == 288
        assert test["inventory"] == test["started"] == test["passed"] == prior_tests["inventory"]
        assert not any(test[key] for key in ("skipped", "expected_failures", "unexpected_successes", "failures", "errors"))
        assert test["source_files"] == {"tests/crawl-jobs-v2-observer/" + key: value for key, value in current_sources.items()}
        assert test["runner_sha256"] == hashfile(SOURCE / "scripts/test-crawl-jobs-v2-observer.py")
        if arch == "x86_64":
            assert (artifact / "c0-debug-probe.log").read_text() == "PASS: 29 offline x86 debug-state controls; ptrace simulated\n"
        directory = artifact / "c0-images"
        record = load(directory / "image-set.json")
        assert record["commit"] == COMMIT and record["architecture"] == arch and record["platform_sha256"] == platform_digest(arch)
        assert record["status"] == "PASS_STATIC_IMAGE_SET" and record["engine"] == "podman" and record["cleanup_complete"] is True
        assert record["runtime_acceptance"] is record["execution_authorized"] is False
        assert record["c0_actors_started"] == record["inspection_containers_started"] == 0
        assert record["source_files"] == current_sources and record["source_snapshot_sha256"] == sha(canonical(current_sources))
        native = verify_artifacts(directory / "native", directory / "oracle", current_sources, arch)
        same_files(directory, record["artifacts"])
        assert record["stable_artifacts_sha256"] == sha(canonical(record["artifacts"]))
        old_record = load(OLD / arch / "image-set.json")
        assert record["artifacts"] == old_record["artifacts"]
        same_files(OLD / arch, old_record["artifacts"])
        observations, counts = commands_and_cleanup(directory, record)
        for kind, index in (("compiler", 3), ("python", 5)):
            base = load(directory / f"command-{index:03d}.log")[0]
            assert record["bases"][kind]["reference"] == BASES[arch][kind]
            assert BASES[arch][kind] in base["RepoDigests"]
            assert image_id(base["Id"]) == record["bases"][kind]["id"]
            assert base["Config"]["Env"] == record["bases"][kind]["environment"]
        roles = {}
        assert set(record["images"]) == {"target", "observer", "oracle"}
        for role, image in record["images"].items():
            inspected = load(directory / (role + "-inspect.json"))
            metadata = image_metadata(inspected, arch, role, record["bases"]["python"]["environment"] if role == "oracle" else [])
            assert all(image[key] == value for key, value in metadata.items())
            assert hashfile(directory / (role + "-inspect.json")) == image["inspect_sha256"]
            portable = archive_record(directory / (role + "-image.tar"), image["id"], arch)
            assert portable == image["portable_archive"]
            cid = image["inspection_container"]["id"].removeprefix("sha256:")
            root = image["inspection_container"]["root_directory"]
            assert all(value == root for _, value in observations[cid])
            exports = [i for i, command in enumerate(record["commands"]) if command["argv"][2] == "export" and command["argv"][-1] == cid]
            assert len(exports) == 1 and observations[cid][0][0] < exports[0] < observations[cid][1][0]
            flattened = derived / (arch + "-" + role + ".tar")
            layers = reconstruct(directory / (role + "-image.tar"), inspected, image["id"], portable["layers"], flattened)
            tree = ImageTree(flattened, root)
            content = verify_content(tree, arch, role, native, source_bytes)
            reported = load(directory / (role + "-content.json"))
            assert canonical(content) == canonical(reported), (arch, role, "content mismatch", {k: [content[k], reported[k]] for k in content if content[k] != reported[k]})
            assert sha(canonical(content)) == image["content_sha256"]
            assert content["payload_sha256"] == image["payload_sha256"] == old_record["images"][role]["payload_sha256"]
            assert len(content["elfs"]) == image["elf_files"]
            required = [row for row in content["elfs"].values() if row["required_dependency_closure"]]
            assert all(not row["unresolved_dependencies"] for row in required)
            roles[role] = {"id": image["id"], "portable_archive": portable, "root_directory": root,
                           "root_evidence": "two matching stopped-container host observations around export",
                           "content_sha256": image["content_sha256"], "payload_sha256": image["payload_sha256"],
                           "payload_matches_reviewed_local_image": True, "content_replay_exact": True,
                           "elf_count": len(content["elfs"]), "required_elf_count": len(required),
                           "reconstructed_tar_sha256": hashfile(flattened), "layers": layers}
            del tree
        result["architectures"][arch] = {"artifact_id": downloaded["id"], "artifact_zip_sha256": downloaded["archive_sha256"],
            "downloaded_files_verified": len(downloaded["files"]), "hosted_tests": 288, "source_files_verified": 55,
            "test_report_sha256": hashfile(artifact / "c0-offline-tests.json"), "image_set_sha256": hashfile(directory / "image-set.json"),
            "unchanged_stable_artifacts": len(record["artifacts"]), "commands_verified": 61,
            "command_counts": counts, "stopped_containers_removed": 5, "roles": roles}
        same_files(artifact, downloaded["files"])
        print(json.dumps({"architecture": arch, "hosted_tests": 288, "images_replayed": 3,
                          "source_files_verified": 55, "commands_verified": 61, "cleanup_records_verified": 5}), flush=True)
    same_files(SOURCE, sync["merged_source_files"])
    with (OUT / "verification.json").open("x") as stream:
        json.dump(result, stream, sort_keys=True, indent=2)
        stream.write("\n")


if __name__ == "__main__":
    try:
        main()
    except Exception:
        with (OUT / "verification-failure.txt").open("x") as stream:
            stream.write(traceback.format_exc())
        raise
