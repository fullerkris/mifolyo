"""Independent offline ZIP, provenance, command-chain and layer/content replay.

Does not call download.py/verify.py or any engine; portable layers are read as
inert data. A separately implemented metadata/content map checks every retained
flattened member before the unchanged reviewed content verifier is replayed.
"""
from collections import Counter
import copy
import io
from pathlib import PurePosixPath
import stat
import tarfile
import zipfile
from review_common import *

sys.path.insert(0, str(SOURCE / PACKAGE))
from contracts import Invalid, canonical, platform_digest
from image_artifacts import BASES, ImageTree, archive_record, image_metadata, verify_content
from image_build import ROOT_METADATA_SCRIPT, verify_artifacts
from oracle_protocol import SOURCE_FILES

EXPECTED_ARTIFACTS = {
    "aarch64": (12461066588, "7cac65afbed201004f1417def2a5c2ea28f1e0e8766ef8c8e1cf4e6d158e22f8", 84),
    "x86_64": (12459699018, "6b30fdb8d9661fd08d9acd6eaecf0ecf81e45eff603a3e84c61778d8a28ee8e9", 85),
}
NEGATIVE = []


def rejected(label, function, expected):
    try:
        function()
    except Invalid as error:
        assert str(error) == expected, (label, str(error), expected)
        NEGATIVE.append({"probe": label, "refusal": str(error)})
    else:
        raise AssertionError("false positive: " + label)


def check_provenance():
    run, jobs, artifacts = (load(HOSTED / name) for name in ("run.json", "jobs.json", "artifacts.json"))
    assert run["id"] == 39858333396 and run["run_attempt"] == 1
    assert run["head_sha"] == COMMIT and run["head_branch"] == "main" and run["event"] == "push"
    assert run["status"] == "completed" and run["conclusion"] == "success"
    assert run["path"] == ".github/workflows/c0-runtime-images.yml" and run["name"] == "C0 Runtime Images"
    for key in ("repository", "head_repository"):
        assert run[key]["id"] == 1220983139 and run[key]["full_name"] == "fullerkris/mifolyo" and not run[key]["fork"]
    assert jobs["total_count"] == len(jobs["jobs"]) == artifacts["total_count"] == len(artifacts["artifacts"]) == 2
    assert {r["name"] for r in jobs["jobs"]} == {f"C0 runtime images ({a})" for a in EXPECTED_ARTIFACTS}
    assert {r["name"] for r in artifacts["artifacts"]} == {"c0-runtime-images-" + a for a in EXPECTED_ARTIFACTS}
    download = load(HOSTED / "download-integrity.json")
    assert download["run_id"] == run["id"] and download["attempt"] == 1 and download["commit"] == COMMIT
    assert download["status"] == "PASS" and len(download["artifacts"]) == 2
    results = {}
    prior_tests = load(CLOSURE / "tests-022.json")
    inventory = load(CLOSURE / "source-inventory.json")
    for arch, (artifact_id, digest, count) in EXPECTED_ARTIFACTS.items():
        name = "c0-runtime-images-" + arch
        job = next(row for row in jobs["jobs"] if row["name"] == f"C0 runtime images ({arch})")
        assert job["run_id"] == run["id"] and job["run_attempt"] == 1 and job["head_sha"] == COMMIT and job["head_branch"] == "main"
        assert job["status"] == "completed" and job["conclusion"] == "success"
        assert job["labels"] == ["ubuntu-24.04-arm" if arch == "aarch64" else "ubuntu-24.04"]
        steps = {row["name"]: row for row in job["steps"]}
        assert len(steps) == len(job["steps"])
        for step in job["steps"]:
            expected = "skipped" if arch == "aarch64" and step["name"] == "Run simulated x64 debug-register controls" else "success"
            assert step["status"] == "completed" and step["conclusion"] == expected
        for required in ("Run source-bound offline controls", "Verify rootless image builder", "Build and verify stopped image contents", "Retain image evidence and portable images"):
            assert steps[required]["conclusion"] == "success"
        artifact = next(row for row in artifacts["artifacts"] if row["name"] == name)
        assert artifact["id"] == artifact_id and artifact["digest"] == "sha256:" + digest and artifact["expired"] is False
        assert artifact["workflow_run"] == {"id": run["id"], "repository_id": 1220983139, "head_repository_id": 1220983139,
            "head_branch": "main", "head_sha": COMMIT}
        downloaded = next(row for row in download["artifacts"] if row["name"] == name)
        assert downloaded["commit"] == COMMIT and downloaded["id"] == artifact_id
        zipped = HOSTED / (name + ".zip")
        assert hashfile(zipped) == digest == downloaded["archive_sha256"]
        assert zipped.stat().st_size == artifact["size_in_bytes"] == downloaded["archive_bytes"]
        actual_files, seen = {}, set()
        with zipfile.ZipFile(zipped) as archive:
            assert 0 < len(archive.infolist()) <= 1000
            for entry in archive.infolist():
                path = PurePosixPath(entry.filename)
                assert not path.is_absolute() and path.parts and not set(path.parts) & {".", ".."}
                assert str(path) == entry.filename.rstrip("/") and "\\" not in entry.filename and "\0" not in entry.filename
                assert str(path) not in seen
                seen.add(str(path))
                mode = entry.external_attr >> 16
                assert not stat.S_ISLNK(mode) and stat.S_IFMT(mode) in (0, stat.S_IFDIR, stat.S_IFREG)
                if entry.is_dir():
                    continue
                assert 0 <= entry.file_size <= 512 * 1024 * 1024
                with archive.open(entry) as stream:
                    h, size = hashlib.sha256(), 0
                    for block in iter(lambda: stream.read(1024 * 1024), b""):
                        h.update(block); size += len(block)
                assert size == entry.file_size
                actual_files[entry.filename] = {"bytes": size, "sha256": h.hexdigest()}
                assert actual_files[entry.filename] == record(HOSTED / name / entry.filename)
        assert len(actual_files) == count and actual_files == downloaded["files"]
        assert set(actual_files) == {str(p.relative_to(HOSTED / name)) for p in (HOSTED / name).rglob("*") if p.is_file()}
        test = load(HOSTED / name / "c0-offline-tests.json")
        assert test["commit"] == COMMIT and test["status"] == "PASS" and test["tests_run"] == 288
        assert len(set(test["inventory"])) == len(test["inventory"]) == 288
        assert test["inventory"] == test["started"] == test["passed"] == prior_tests["inventory"]
        assert all(test[k] == [] for k in ("skipped", "expected_failures", "unexpected_successes", "failures", "errors"))
        assert test["source_files"] == inventory["files"]
        assert test["runner_sha256"] == inventory["auxiliary_files"]["scripts/test-crawl-jobs-v2-observer.py"]
        if arch == "x86_64":
            assert (HOSTED / name / "c0-debug-probe.log").read_bytes() == b"PASS: 29 offline x86 debug-state controls; ptrace simulated\n"
        results[arch] = {"job_id": job["id"], "artifact_id": artifact_id, "zip_sha256": digest,
            "zip_bytes": artifact["size_in_bytes"], "exact_zip_and_extracted_file_count": count,
            "zip_crc_checked_by_complete_reads": True, "hosted_test_ids_verified": 288, "hosted_sources_verified": 55}
    return results


def check_commands(directory, image_set, arch):
    commands = image_set["commands"]
    assert len(commands) == 61
    counts = Counter()
    for index, command in enumerate(commands):
        assert command["argv"][:2] == ["/usr/bin/podman", "--remote=false"]
        assert command["log"] == f"command-{index:03d}.log"
        assert command["returncode"] == 0 and type(command["returncode"]) is int
        assert hashfile(directory / command["log"]) == command["log_sha256"]
        counts[command["argv"][2]] += 1
    assert counts == {"info": 1, "version": 1, "pull": 2, "image": 8, "build": 5, "create": 5,
        "container": 15, "unshare": 6, "cp": 2, "export": 3, "rm": 5, "ps": 5, "save": 3}
    def argv(index): return commands[index]["argv"][2:]
    def raw(index): return (directory / commands[index]["log"]).read_bytes()
    def logged(index): return load(directory / commands[index]["log"])
    assert argv(0) == ["info", "--format", "{{.Host.Security.Rootless}}"] and raw(0).strip() == b"true"
    assert argv(1) == ["version"] and raw(1).decode() == image_set["builder_version"]
    assert "Version:      4.9.3\n" in image_set["builder_version"]
    machine = "arm64" if arch == "aarch64" else "amd64"
    assert "OS/Arch:      linux/" + machine in image_set["builder_version"]
    platform = "linux/" + machine
    for pull_index, kind in ((2, "compiler"), (4, "python")):
        reference = BASES[arch][kind]
        assert argv(pull_index) == ["pull", "--platform", platform, reference]
        assert argv(pull_index + 1) == ["image", "inspect", reference]
        base = logged(pull_index + 1)
        assert len(base) == 1
        base = base[0]
        assert base["Architecture"] == machine and base["Os"] == "linux" and reference in base["RepoDigests"]
        assert image_set["bases"][kind] == {"id": "sha256:" + base["Id"].removeprefix("sha256:"),
            "reference": reference, "environment": base["Config"]["Env"]}
    root = "/home/runner/work/_temp/c0-images"
    for index, target, recipe, label in ((6, "build", "Dockerfile", "native-builder"), (7, "oracle-build", "Dockerfile.oracle", "oracle-builder"),
        (22, "target", "Dockerfile", "target"), (35, "observer", "Dockerfile", "observer"), (48, "oracle", "Dockerfile.oracle", "oracle")):
        ignore = "Dockerfile.oracle.dockerignore" if recipe.endswith(".oracle") else ".dockerignore"
        expected = ["build", "--no-cache", "--platform", platform, "--build-arg", "C0_ARCHITECTURE=" + arch,
            "--network", "none", "--pull=never", "--http-proxy=false", "--layers=false", "--force-rm", "--identity-label=false",
            "--ignorefile", root + "/source/" + ignore]
        if target in ("target", "observer"): expected += ["--unsetenv", "PATH"]
        expected += ["--target", target, "--file", root + "/source/" + recipe, "--iidfile", root + "/" + label + ".iid", root + "/source"]
        assert argv(index) == expected
    cleanup, roots = [], {}
    for create_index, role in ((8, "native"), (15, "oracle-builder"), (24, "target"), (37, "observer"), (50, "oracle")):
        args = argv(create_index)
        cid = raw(create_index).decode().strip()
        assert len(cid) == 64 and all(c in "0123456789abcdef" for c in cid)
        name, identity = args[2], args[-1]
        assert args == ["create", "--name", name, "--network", "none", "--read-only", "--user", "999:999", "--cap-drop", "ALL",
            "--security-opt", "no-new-privileges", "--no-healthcheck", "--entrypoint", "/__obs1_inspection_only__", identity]
        native_builder = role in ("native", "oracle-builder")
        expected_ops = ["container", "cp", "container", "container", "rm", "ps"] if native_builder else [
            "container", "unshare", "export", "container", "unshare", "container", "rm", "ps"]
        assert [argv(create_index + i + 1)[0] for i in range(len(expected_ops))] == expected_ops
        snapshots, observations = [], []
        for index in range(create_index + 1, create_index + len(expected_ops) + 1):
            a = argv(index)
            if a[0] == "container":
                assert a == ["container", "inspect", cid]
                values = logged(index)
                assert len(values) == 1
                value = values[0]
                assert value["Id"].removeprefix("sha256:") == cid and value["Image"].removeprefix("sha256:") == identity.removeprefix("sha256:")
                assert value["Name"].lstrip("/") == name
                status = value["State"]
                assert status["Running"] is False and type(status["Pid"]) is int and status["Pid"] == 0
                assert status["Status"] in ("created", "configured") and status["StartedAt"] in ("0001-01-01T00:00:00Z", "0001-01-01T00:00:00+00:00")
                assert value["Config"]["User"] == "999:999"
                snapshots.append({"id": "sha256:" + cid, "image": identity, "name": name, "running": False, "pid": 0, "status": status["Status"]})
            elif a[0] == "unshare":
                host_arch = "arm64" if arch == "aarch64" else "x64"
                assert a == ["unshare", f"/opt/hostedtoolcache/Python/3.13.16/{host_arch}/bin/python", "-I", "-B", "-c", ROOT_METADATA_SCRIPT, "/usr/bin/podman", cid]
                assert logged(index) == {"uid": 0, "gid": 0, "mode": 0o555}
                observations.append({"command_index": index, "root": logged(index)})
            elif a[0] == "cp":
                origin, dest = ("/out/.", "native") if role == "native" else ("/artifacts/.", "oracle")
                assert a == ["cp", cid + ":" + origin, root + "/" + dest]
            elif a[0] == "export": assert a == ["export", "--output", root + "/" + role + ".tar", cid]
            elif a[0] == "rm":
                assert a == ["rm", cid] and raw(index).decode().strip() == cid
            elif a[0] == "ps":
                assert a == ["ps", "--all", "--no-trunc", "--filter", "id=" + cid, "--format", "{{.ID}}"]
                assert not raw(index).strip()
        assert len(snapshots) == 3 and snapshots[0] == snapshots[1] == snapshots[2]
        cleanup.append(snapshots[0] | {"removed": True})
        if not native_builder:
            image = image_set["images"][role]
            assert image["id"] == identity and image["inspection_container"] == snapshots[0] | {"root_directory": observations[0]["root"]}
            assert len(observations) == 2 and observations[0]["command_index"] < create_index + 3 < observations[1]["command_index"]
            for image_index in (create_index - 1, create_index + 9):
                assert argv(image_index) == ["image", "inspect", identity]
                assert logged(image_index) == [load(directory / (role + "-inspect.json"))]
            assert argv(create_index + 10) == ["save", "--format", "docker-archive", "--output", root + "/" + role + "-image.tar", identity]
            roots[role] = {"container_id": cid, "observations": observations}
    assert len({r["id"] for r in cleanup}) == 5 and cleanup == image_set["cleanup"]
    return {"logs_hashed": 61, "exact_argv_and_lifecycle_checked": True, "counts": dict(counts), "cleanup_records": cleanup, "roots": roots}


def read_members(archive):
    rows, roots, pax = {}, [], set()
    total = 0
    for i, member in enumerate(archive, 1):
        assert i <= 20000
        assert not member.name.startswith("/") and ".." not in member.name.split("/")
        assert not any(c in member.name for c in "\0\r\n")
        parts = [p for p in member.name.split("/") if p not in ("", ".")]
        assert not any(p.startswith(".wh.") for p in parts)
        path = "/" + "/".join(parts)
        assert len(path) <= 4096
        assert 0 <= member.uid < 2**32 and 0 <= member.gid < 2**32 and member.mode & ~0o7777 == 0
        pax.update(member.pax_headers)
        assert not set(member.pax_headers) - {"path", "linkpath", "mtime", "atime", "ctime"}
        row = {"uid": member.uid, "gid": member.gid, "mode": member.mode, "type": member.type.decode("ascii")}
        if path == "/":
            assert member.isdir() and member.size == 0 and not roots
            roots.append({k: row[k] for k in ("uid", "gid", "mode")})
            continue
        assert path not in rows, path
        if member.isfile():
            assert not member.issparse() and 0 <= member.size <= 64 * 1024 * 1024
            with archive.extractfile(member) as stream:
                data = stream.read(member.size + 1)
            assert len(data) == member.size
            row.update(bytes=len(data), sha256=sha(data))
            total += len(data)
            assert total <= 512 * 1024 * 1024
        else:
            assert member.size == 0
            if member.issym() or member.islnk(): row["linkname"] = member.linkname
            else: assert member.isdir()
        rows[path] = row
    return rows, roots, sorted(pax)


def layer_map(path, inspected, image, arch):
    merged, descriptions, hard_targets = {}, [], set()
    with tarfile.open(path, "r:") as portable:
        manifests = json.loads(portable.extractfile("manifest.json").read(), object_pairs_hook=no_duplicates)
        assert len(manifests) == 1
        manifest = manifests[0]
        config_bytes = portable.extractfile(manifest["Config"]).read()
        assert "sha256:" + sha(config_bytes) == image["id"]
        config = json.loads(config_bytes, object_pairs_hook=no_duplicates)
        assert config["config"] == inspected["Config"] and config["os"] == "linux"
        assert config["architecture"] == ("arm64" if arch == "aarch64" else "amd64")
        assert config["rootfs"]["type"] == "layers"
        diffs = config["rootfs"]["diff_ids"]
        assert diffs == inspected["RootFS"]["Layers"] == image["portable_archive"]["layers"]
        assert len(manifest["Layers"]) == len(diffs) and len(set(manifest["Layers"])) == len(diffs)
        for layer_name, wanted in zip(manifest["Layers"], diffs):
            with portable.extractfile(layer_name) as stream:
                assert "sha256:" + hashlib.file_digest(stream, "sha256").hexdigest() == wanted
            with portable.extractfile(layer_name) as stream, tarfile.open(fileobj=stream, mode="r:") as layer:
                additions, roots, pax = read_members(layer)
            assert not hard_targets & additions.keys()
            replaced = []
            for name, row in additions.items():
                if name in merged:
                    assert merged[name]["type"] == row["type"] and row["type"] in ("0", "5")
                    replaced.append(name.lstrip("/"))
                if row["type"] == "1":
                    link = row["linkname"]
                    assert not link.startswith("/") and ".." not in link.split("/")
                    target = "/" + str(PurePosixPath(link))
                    value = additions.get(target, merged.get(target))
                    assert value is not None and value["type"] == "0"
                    assert all(row[k] == value[k] for k in ("mode", "uid", "gid"))
                    hard_targets.add(target)
                merged[name] = row
            descriptions.append({"name": layer_name, "entries": len(additions), "root_headers": roots,
                "replaced_paths": replaced, "whiteouts": 0, "pax_keys": pax})
    for name in merged:
        parent = str(PurePosixPath(name).parent)
        assert parent == "/" or merged[parent]["type"] == "5", name
    return merged, descriptions


def main():
    provenance = check_provenance()
    verification = load(HOSTED / "verification.json")
    inventory = load(CLOSURE / "source-inventory.json")["files"]
    sources = {name.removeprefix(PACKAGE + "/"): digest for name, digest in inventory.items()}
    source_bytes = {name: (SOURCE / PACKAGE / name).read_bytes() for name in (*SOURCE_FILES, "build_oracle.py")}
    final = {}
    for arch in EXPECTED_ARTIFACTS:
        directory = HOSTED / ("c0-runtime-images-" + arch) / "c0-images"
        image_set = load(directory / "image-set.json")
        assert image_set["commit"] == COMMIT and image_set["architecture"] == arch and image_set["platform_sha256"] == platform_digest(arch)
        assert image_set["status"] == "PASS_STATIC_IMAGE_SET" and image_set["engine"] == "podman" and image_set["cleanup_complete"] is True
        assert image_set["runtime_acceptance"] is image_set["execution_authorized"] is False
        assert image_set["c0_actors_started"] == image_set["inspection_containers_started"] == 0
        assert image_set["source_files"] == sources and image_set["source_snapshot_sha256"] == sha(canonical(sources))
        assert set(image_set["images"]) == {"target", "observer", "oracle"}
        artifacts = verify_artifacts(directory / "native", directory / "oracle", sources, arch)
        old_set = load(OLD / arch / "image-set.json")
        assert image_set["artifacts"] == old_set["artifacts"] and len(image_set["artifacts"]) == 12
        assert sha(canonical(image_set["artifacts"])) == image_set["stable_artifacts_sha256"]
        for name, digest in image_set["artifacts"].items():
            assert hashfile(directory / name) == hashfile(OLD / arch / name) == digest
        command_result = check_commands(directory, image_set, arch)
        roles = {}
        for role, image in image_set["images"].items():
            inspected = load(directory / (role + "-inspect.json"))
            environment = image_set["bases"]["python"]["environment"] if role == "oracle" else []
            metadata = image_metadata(inspected, arch, role, environment)
            assert all(image[key] == value for key, value in metadata.items())
            assert image["inspect_sha256"] == hashfile(directory / (role + "-inspect.json"))
            assert image["engine_manifest_digest"] == inspected["Digest"]
            portable = directory / (role + "-image.tar")
            assert archive_record(portable, image["id"], arch) == image["portable_archive"]
            model, layers = layer_map(portable, inspected, image, arch)
            retained = HOSTED / "reconstructed" / (arch + "-" + role + ".tar")
            with tarfile.open(retained, "r:") as flat:
                flattened, roots, pax = read_members(flat)
            assert not roots and set(pax) <= {"path", "linkpath"} and model == flattened
            supplied = verification["architectures"][arch]["roles"][role]
            assert hashfile(retained) == supplied["reconstructed_tar_sha256"]
            assert [{k: v for k, v in row.items() if k != "pax_keys"} for row in layers] == supplied["layers"]
            root = image["inspection_container"]["root_directory"]
            assert root == {"uid": 0, "gid": 0, "mode": 0o555}
            tree = ImageTree(retained, root)
            content = verify_content(tree, arch, role, artifacts, source_bytes)
            assert canonical(content) == (directory / (role + "-content.json")).read_bytes()
            assert sha(canonical(content)) == image["content_sha256"] == supplied["content_sha256"]
            assert content["payload_sha256"] == image["payload_sha256"] == old_set["images"][role]["payload_sha256"]
            assert len(content["elfs"]) == image["elf_files"]
            required = [r for r in content["elfs"].values() if r["required_dependency_closure"]]
            assert len(required) == (18 if role == "oracle" else 1) and all(not row["unresolved_dependencies"] for row in required)
            old_content = load(OLD / arch / (role + "-content.json"))
            assert sha(canonical(old_content)) == old_set["images"][role]["content_sha256"]
            delta_keys = [k for k in content if content[k] != old_content[k]]
            assert delta_keys == (["root_directory"] if role == "oracle" else [])
            if role == "oracle":
                assert old_content["root_directory"] == {"uid": 0, "gid": 0, "mode": 0o755}
                assert [r for layer in layers for r in layer["root_headers"]] == [{"uid": 0, "gid": 0, "mode": 0o755}]
            else:
                assert not any(layer["root_headers"] for layer in layers)
            other_arch = "x86_64" if arch == "aarch64" else "aarch64"
            rejected(arch + "/" + role + "/wrong-config-id", lambda: archive_record(portable, "sha256:" + "f" * 64, arch), "IMAGE_SAVE_CONFIG")
            rejected(arch + "/" + role + "/wrong-architecture", lambda: archive_record(portable, image["id"], other_arch), "IMAGE_SAVE_PLATFORM")
            tree.root = {"uid": 0, "gid": 0, "mode": 0o777}
            rejected(arch + "/" + role + "/writable-root", lambda: verify_content(tree, arch, role, artifacts, source_bytes), "IMAGE_DIRECTORY_ACCESS")
            tree.root = root
            if role == "oracle":
                wrong = source_bytes | {"build_oracle.py": b"not the reviewed source"}
                rejected(arch + "/oracle/wrong-source", lambda: verify_content(tree, arch, role, artifacts, wrong), "IMAGE_ORACLE_SOURCE")
                tree.root = old_content["root_directory"]
                alternate = verify_content(tree, arch, role, artifacts, source_bytes)
                assert alternate["payload_sha256"] == content["payload_sha256"] and sha(canonical(alternate)) != image["content_sha256"]
                assert alternate == old_content
                tree.root = root
            else:
                wrong = artifacts | {role: artifacts[role] + b"tamper"}
                rejected(arch + "/" + role + "/wrong-native-bytes", lambda: verify_content(tree, arch, role, wrong, source_bytes), "IMAGE_NATIVE_CONTENT")
            roles[role] = {"image_id": image["id"], "portable_archive": image["portable_archive"],
                "reconstructed_tar_sha256": hashfile(retained), "independently_compared_members": len(model),
                "member_map_sha256": sha(canonical(model)), "full_content_record_sha256": image["content_sha256"],
                "payload_sha256": image["payload_sha256"], "payload_matches_historical": True,
                "elfs": len(content["elfs"]), "required_dependency_closure_elfs": len(required),
                "nonrequired_elfs_with_unresolved_dependencies": sum(bool(r["unresolved_dependencies"]) for r in content["elfs"].values() if not r["required_dependency_closure"]),
                "hosted_root": root, "historical_root": old_content["root_directory"], "historical_content_changed_keys": delta_keys, "layers": layers}
            del tree
        final[arch] = {"provenance": provenance[arch], "stable_artifacts_verified": 12,
            "commands": command_result, "roles": roles}
        print(json.dumps({"architecture": arch, "roles": 3, "commands": 61, "zip_files": provenance[arch]["exact_zip_and_extracted_file_count"], "status": "PASS"}), flush=True)

    # Damaged portable archives are retained as inert negative-test fixtures only.
    directory = HOSTED / "c0-runtime-images-aarch64/c0-images"
    portable = directory / "target-image.tar"
    identity = load(directory / "image-set.json")["images"]["target"]["id"]
    original = portable.read_bytes()
    with tarfile.open(fileobj=io.BytesIO(original), mode="r:") as archive:
        manifest = json.loads(archive.extractfile("manifest.json").read())[0]
        layer = archive.getmember(manifest["Layers"][0])
        config = archive.getmember(manifest["Config"])
    for name, offset, expected in (("tampered-layer.tar", layer.offset_data + 1024, "IMAGE_SAVE_LAYER_DIGEST"),
        ("tampered-config.tar", config.offset_data + 16, "IMAGE_SAVE_CONFIG")):
        changed = bytearray(original); changed[offset] ^= 1
        fixture = REVIEW / name
        with fixture.open("xb") as out: out.write(changed)
        rejected(name, lambda: archive_record(fixture, identity, "aarch64"), expected)
    result = {"status": "PASS", "commit": COMMIT, "run_id": 39858333396, "run_attempt": 1,
        "architectures": final, "negative_controls": NEGATIVE, "negative_controls_passed": len(NEGATIVE),
        "root_evidence_boundary_controls": 2, "engine_commands_executed": 0, "network_requests": 0,
        "runtime_acceptance": False, "execution_authorized": False}
    assert len(NEGATIVE) == 26
    write("image-checks.json", result)
    print(json.dumps({"status": "PASS", "image_count": 6, "negative_controls_passed": len(NEGATIVE), "root_evidence_boundary_controls": 2}))


if __name__ == "__main__":
    main()
