"""Independent exact-archive census, guarded static replay and narrow negative controls."""
import ast
from collections import Counter, defaultdict
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import sys
import tarfile

ROOT = Path(__file__).resolve().parents[3]
OUT = Path(__file__).resolve().parent
HOSTED = OUT.parent
SOURCE = ROOT / "out/c0-main-sync-2026-10-24/verified-merged-source"
PACKAGE = SOURCE / "tests/crawl-jobs-v2-observer"


def need(condition, message):
    if not condition:
        raise ValueError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def load(path):
    return json.loads(path.read_bytes())


events_blocked = []


def guard(event, args):
    if event in ("subprocess.Popen", "os.system", "os.fork", "os.forkpty", "os.posix_spawn", "os.exec", "socket.connect", "socket.bind", "socket.getaddrinfo"):
        events_blocked.append(event)
        raise PermissionError("review disallows process/network activity")
    if event == "open":
        path, mode, flags = args
        if flags & (os.O_WRONLY | os.O_RDWR | os.O_CREAT | os.O_TRUNC | os.O_APPEND):
            need(not isinstance(path, int) and Path(path).resolve().is_relative_to(OUT), "write outside review directory")
    if event in ("os.mkdir", "os.remove", "os.rmdir", "os.chmod", "os.link", "os.symlink", "os.rename"):
        need(Path(args[0]).resolve().is_relative_to(OUT), "filesystem mutation outside review directory")


sys.dont_write_bytecode = True
sys.addaudithook(guard)
sys.path.insert(0, str(PACKAGE))
from contracts import Invalid, canonical, platform_digest
from image_artifacts import BASES, ImageTree, archive_record, image_metadata, verify_content
from image_build import ROOT_METADATA_SCRIPT, source_files, verify_artifacts
from oracle_protocol import SOURCE_FILES

# Load only the actual new pure replay function; do not execute verify.py's
# module import setup or its main routine, both of which target retained paths.
module = ast.parse((HOSTED / "verify.py").read_bytes())
definition = next(row for row in module.body if isinstance(row, ast.FunctionDef) and row.name == "reconstruct")
namespace = {"tarfile": tarfile, "json": json, "sha": sha, "PurePosixPath": PurePosixPath, "io": io}
exec(compile(ast.Module(body=[definition], type_ignores=[]), str(HOSTED / "verify.py"), "exec"), namespace)
reconstruct = namespace["reconstruct"]


def member_record(member, archive):
    need(set(member.pax_headers) <= {"path", "linkpath"}, "unmodeled PAX/xattr semantics")
    need(not member.issparse(), "sparse semantics")
    row = {"type": member.type.decode(), "uid": member.uid, "gid": member.gid, "mode": member.mode,
           "size": member.size, "linkname": member.linkname}
    if member.isfile():
        with archive.extractfile(member) as data:
            raw = data.read()
        need(len(raw) == member.size, "member size")
        row["sha256"] = sha(raw)
    else:
        need(member.size == 0, "nonfile data")
    return row


def exact_layer_map(portable, expected):
    """Independent map of every path, not just declared executables/ELFs."""
    final, details, links, hard_targets = {}, [], {}, set()
    with tarfile.open(portable, "r:") as saved:
        manifest = json.load(saved.extractfile("manifest.json"))[0]
        need(len({m.name for m in saved.getmembers()}) == len(saved.getmembers()), "outer duplicate")
        config_raw = saved.extractfile(manifest["Config"]).read()
        config = json.loads(config_raw)
        need("sha256:" + sha(config_raw) == expected["id"], "config ID")
        need(config["rootfs"]["type"] == "layers" and len(manifest["Layers"]) == len(config["rootfs"]["diff_ids"]), "layer inventory")
        for name, expected_hash in zip(manifest["Layers"], config["rootfs"]["diff_ids"]):
            layer_bytes = saved.extractfile(name).read()
            need("sha256:" + sha(layer_bytes) == expected_hash, "layer hash")
            layer_map, roots, types, pax, replacements = {}, [], Counter(), Counter(), []
            with tarfile.open(fileobj=io.BytesIO(layer_bytes), mode="r:") as layer:
                for member in layer:
                    name_in_layer = member.name.rstrip("/")
                    need(not member.name.startswith("/") and ".." not in member.name.split("/") and not any(c in member.name for c in "\0\r\n"), "unsafe layer path")
                    need(str(PurePosixPath(name_in_layer)) == name_in_layer, "noncanonical exact layer path")
                    need(not any(part.startswith(".wh.") for part in PurePosixPath(name_in_layer).parts), "whiteout")
                    row = member_record(member, layer)
                    types[row["type"]] += 1
                    pax.update(member.pax_headers.keys())
                    if name_in_layer == ".":
                        need(member.isdir() and not roots, "root header")
                        roots.append({k: row[k] for k in ("uid", "gid", "mode")})
                        continue
                    need(name_in_layer not in layer_map and name_in_layer not in hard_targets, "duplicate/hardlink target replacement")
                    need(member.isfile() or member.isdir() or member.issym() or member.islnk(), "special file")
                    if name_in_layer in final:
                        need(row["type"] == final[name_in_layer]["type"] and row["type"] in ("0", "5"), "type-changing replacement")
                        replacements.append(name_in_layer)
                    if member.islnk():
                        target = member.linkname
                        need(target in layer_map or target in final, "hardlink target not present before alias")
                        target_row = layer_map.get(target, final.get(target))
                        need(target_row["type"] == "0" and all(row[k] == target_row[k] for k in ("mode", "uid", "gid")), "hardlink inode mismatch")
                        links[name_in_layer] = target
                        hard_targets.add(target)
                    layer_map[name_in_layer] = row
            final.update(layer_map)
            details.append({"name": name, "entries": len(layer_map), "root_headers": roots,
                            "replaced_paths": replacements, "whiteouts": 0, "member_types": dict(types), "pax_keys": dict(pax)})
    for name in final:
        parent = str(PurePosixPath(name).parent)
        need(parent == "." or final.get(parent, {}).get("type") == "5", "nonliteral directory parent")
    return final, details, links


def retained_flat_map(path):
    result = {}
    with tarfile.open(path, "r:") as flat:
        for member in flat:
            need(member.name not in result and member.name not in (".", "./"), "flat duplicate or merged root")
            result[member.name] = member_record(member, flat)
    return result


def commands(directory, record):
    """Check the actual closed command sequence and lifecycle, not only totals."""
    stages, root_observations, counts, creations = {}, defaultdict(list), Counter(), {}
    allowed = {"info", "version", "pull", "image", "build", "create", "container", "unshare", "cp", "export", "rm", "ps", "save"}
    for i, command in enumerate(record["commands"]):
        argv = command["argv"]
        need(argv[:2] == ["/usr/bin/podman", "--remote=false"] and argv[2] in allowed, "unexpected engine command")
        op, args = argv[2], argv[3:]
        counts[op] += 1
        logfile = directory / f"command-{i:03d}.log"
        need(command["log"] == logfile.name and command["returncode"] == 0 and digest(logfile) == command["log_sha256"], "command log binding")
        if op == "info":
            need(args == ["--format", "{{.Host.Security.Rootless}}"] and logfile.read_bytes().strip() == b"true", "rootless record")
        elif op == "version":
            need(args == [] and "4.9.3" in logfile.read_text() and logfile.read_text() == record["builder_version"], "builder version")
        elif op == "image":
            need(len(args) == 2 and args[0] == "inspect", "image inspection only")
        elif op == "pull":
            need(args[0] == "--platform" and args[2] in BASES[record["architecture"]].values() and len(args) == 3, "digest-pinned pull")
        elif op == "build":
            need(args[args.index("--network") + 1] == "none" and "--pull=never" in args and "--http-proxy=false" in args and "--identity-label=false" in args, "build boundary")
            need("--privileged" not in args and not any(a.startswith(("--volume", "-v=", "--secret", "--ssh")) for a in args), "build privilege expansion")
        elif op == "create":
            cid = logfile.read_text().strip()
            fixed = ["--name", args[1], "--network", "none", "--read-only", "--user", "999:999", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--no-healthcheck", "--entrypoint", "/__obs1_inspection_only__", args[-1]]
            need(args == fixed and cid not in stages, "create boundary")
            stages[cid] = ["create"]
            creations[cid] = {"name": args[1], "image": args[-1]}
        elif op == "container":
            need(len(args) == 2 and args[0] == "inspect" and args[1] in stages, "container inspection")
            cid = args[1]
            values = load(logfile)
            need(len(values) == 1, "inspect row count")
            value = values[0]
            state = value["State"]
            need(value["Id"] == cid and value["Image"] == creations[cid]["image"].removeprefix("sha256:") and value["Name"].lstrip("/") == creations[cid]["name"], "inspected ownership")
            need(state["Running"] is False and type(state["Pid"]) is int and state["Pid"] == 0 and state["Status"] in ("created", "configured") and state["StartedAt"] in ("0001-01-01T00:00:00Z", "0001-01-01T00:00:00+00:00"), "started container")
            stages[cid].append("inspect")
        elif op == "unshare":
            need(len(args) == 7 and args[0].startswith("/opt/hostedtoolcache/Python/") and args[1:4] == ["-I", "-B", "-c"] and args[4] == ROOT_METADATA_SCRIPT and args[5] == "/usr/bin/podman" and args[6] in stages, "host root observer binding")
            cid = args[6]
            stages[cid].append("root")
            root_observations[cid].append(load(logfile))
        elif op in ("cp", "export"):
            if op == "cp":
                need(len(args) == 2 and ":" in args[0], "cp shape")
                cid, image_path = args[0].split(":", 1)
                need(image_path in ("/out/.", "/artifacts/."), "cp input path")
            else:
                need(len(args) == 3 and args[0] == "--output", "export shape")
                cid = args[2]
            need(cid in stages, "export ownership")
            stages[cid].append(op)
        elif op == "rm":
            need(len(args) == 1 and args[0] in stages, "cleanup ownership")
            stages[args[0]].append("rm")
        elif op == "ps":
            need(len(args) == 6 and args[:3] == ["--all", "--no-trunc", "--filter"] and args[3].startswith("id=") and args[4:] == ["--format", "{{.ID}}"], "absence check")
            cid = args[3][3:]
            need(cid in stages and not logfile.read_bytes().strip(), "cleanup absence")
            stages[cid].append("absent")
        elif op == "save":
            need(len(args) == 5 and args[:3] == ["--format", "docker-archive", "--output"] and args[4] in {v["id"] for v in record["images"].values()}, "save identity")
    need(len(record["commands"]) == 61 and len(stages) == 5 and len(record["cleanup"]) == 5, "command counts")
    for cid, stage in stages.items():
        expected = ["create", "inspect", "root", "export", "inspect", "root", "inspect", "rm", "absent"] if cid in root_observations else ["create", "inspect", "cp", "inspect", "inspect", "rm", "absent"]
        need(stage == expected, "unexpected lifecycle order")
        cleanup = [v for v in record["cleanup"] if v["id"] == "sha256:" + cid]
        need(len(cleanup) == 1 and cleanup[0]["removed"] is True and cleanup[0]["running"] is False and cleanup[0]["pid"] == 0 and cleanup[0]["name"] == creations[cid]["name"] and cleanup[0]["image"] == creations[cid]["image"], "cleanup record")
    return root_observations, dict(counts)


def negative_controls():
    fixtures = OUT / "fixtures"
    fixtures.mkdir()

    def tar_bytes(entries):
        raw = io.BytesIO()
        with tarfile.open(fileobj=raw, mode="w:", format=tarfile.PAX_FORMAT) as archive:
            for name, kind, target, data, mode in entries:
                member = tarfile.TarInfo(name)
                member.type, member.linkname, member.mode = kind, target, mode
                member.size = len(data)
                archive.addfile(member, io.BytesIO(data))
        return raw.getvalue()

    def f(name, data=b"aaaaaaaa", mode=0o644):
        return (name, tarfile.REGTYPE, "", data, mode)

    def d(name):
        return (name, tarfile.DIRTYPE, "", b"", 0o755)

    def h(name, target, mode=0o644):
        return (name, tarfile.LNKTYPE, target, b"", mode)

    def s(name, target):
        return (name, tarfile.SYMTYPE, target, b"", 0o777)

    def portable(name, layers):
        raws = [tar_bytes(entries) for entries in layers]
        hashes = ["sha256:" + sha(raw) for raw in raws]
        config = {"architecture": "amd64", "os": "linux", "config": {"User": "999:999"}, "rootfs": {"type": "layers", "diff_ids": hashes}}
        config_raw = canonical(config)
        layer_names = [f"layer-{i}.tar" for i in range(len(raws))]
        manifest = [{"Config": "config.json", "Layers": layer_names}]
        raw = tar_bytes([f("manifest.json", canonical(manifest)), f("config.json", config_raw), *[f(n, raw) for n, raw in zip(layer_names, raws)]])
        path = fixtures / (name + ".tar")
        with path.open("xb") as stream:
            stream.write(raw)
        inspected = {"Config": config["config"], "RootFS": {"Layers": hashes}}
        return path, inspected, "sha256:" + sha(config_raw), hashes

    results = []

    def run(name, action, rejected=True):
        try:
            action()
        except (AssertionError, Invalid, ValueError) as error:
            need(rejected, "unexpected control rejection: " + name)
            results.append({"name": name, "status": "PASS", "outcome": "rejected", "exception": type(error).__name__, "reason": str(error)})
        else:
            need(not rejected, "negative control accepted: " + name)
            results.append({"name": name, "status": "PASS", "outcome": "accepted positive control"})

    valid = portable("valid-hardlink", [[d("d"), f("d/f"), h("d/h", "d/f")]])
    run("valid_hardlink_static_replay", lambda: reconstruct(*valid, fixtures / "valid-flat.tar"), False)
    tree = ImageTree(fixtures / "valid-flat.tar", {"uid": 0, "gid": 0, "mode": 0o555})
    need(tree.file("/d/h") == b"aaaaaaaa", "positive alias resolution")
    cases = {
        "whiteout": [[f(".wh.deleted")]],
        "parent_traversal": [[f("../escape")]],
        "absolute_member": [[f("/escape")]],
        "normalized_duplicate": [[d("d"), f("d/f"), f("d//f")]],
        "type_changing_replacement": [[f("f")], [d("f")]],
        "hardlink_target_replacement": [[f("f"), h("h", "f")], [f("f", b"bbbbbbbb")]],
        "hardlink_to_symlink": [[f("f"), s("s", "f"), h("h", "s")]],
        "hardlink_metadata_mismatch": [[f("f"), h("h", "f", 0o666)]],
        "special_device": [[("dev", tarfile.CHRTYPE, "", b"", 0o600)]],
        "duplicate_root": [[d("."), d(".")]],
    }
    for name, layers in cases.items():
        args = portable(name, layers)
        run(name, lambda args=args, name=name: reconstruct(*args, fixtures / (name + "-flat.tar")))
    wrong = dict(valid[1], Config={"User": "0:0"})
    run("inspect_config_mismatch", lambda: reconstruct(valid[0], wrong, valid[2], valid[3], fixtures / "wrong-config-flat.tar"))
    run("config_identity_mismatch", lambda: reconstruct(valid[0], valid[1], "sha256:" + "1" * 64, valid[3], fixtures / "wrong-id-flat.tar"))
    run("valid_portable_layers", lambda: archive_record(valid[0], valid[2], "x86_64"), False)
    corrupted = fixtures / "corrupted-layer.tar"
    with corrupted.open("xb") as stream:
        stream.write(valid[0].read_bytes().replace(b"aaaaaaaa", b"bbbbbbbb", 1))
    run("tampered_layer_digest", lambda: archive_record(corrupted, valid[2], "x86_64"))
    run("wrong_portable_architecture", lambda: archive_record(valid[0], valid[2], "aarch64"))
    bad_root = fixtures / "root-mismatch.tar"
    with bad_root.open("xb") as stream:
        stream.write(tar_bytes([d("."), f("f")]))
    run("layer_root_not_host_root", lambda: ImageTree(bad_root, {"uid": 0, "gid": 0, "mode": 0o555}))
    bad_parent = fixtures / "symlink-parent.tar"
    with bad_parent.open("xb") as stream:
        stream.write(tar_bytes([d("d"), s("s", "d"), f("s/f")]))
    run("flattened_symlink_parent", lambda: ImageTree(bad_parent, {"uid": 0, "gid": 0, "mode": 0o555}))
    return results


def main():
    need(sys.flags.optimize == 0, "retained replay relies on enabled assertions")
    source_map = source_files(PACKAGE)
    source_bytes = {name: (PACKAGE / name).read_bytes() for name in (*SOURCE_FILES, "build_oracle.py")}
    retained = load(HOSTED / "verification.json")
    results = {}
    for arch in ("aarch64", "x86_64"):
        directory = HOSTED / ("c0-runtime-images-" + arch) / "c0-images"
        record = load(directory / "image-set.json")
        need(record["source_files"] == source_map and record["source_snapshot_sha256"] == sha(canonical(source_map)), "image source identity")
        need(record["platform_sha256"] == platform_digest(arch) and record["status"] == "PASS_STATIC_IMAGE_SET", "image set status")
        need(record["runtime_acceptance"] is record["execution_authorized"] is False and record["c0_actors_started"] == record["inspection_containers_started"] == 0, "authority escalation")
        artifacts = verify_artifacts(directory / "native", directory / "oracle", source_map, arch)
        observations, counts = commands(directory, record)
        roles = {}
        for role, image in record["images"].items():
            inspected = load(directory / (role + "-inspect.json"))
            metadata = image_metadata(inspected, arch, role, record["bases"]["python"]["environment"] if role == "oracle" else [])
            need(all(image[k] == v for k, v in metadata.items()), "image metadata mismatch")
            portable = directory / (role + "-image.tar")
            need(archive_record(portable, image["id"], arch) == image["portable_archive"], "portable record mismatch")
            full_map, layers, hardlinks = exact_layer_map(portable, image)
            known = retained["architectures"][arch]["roles"][role]
            need([{k: row[k] for k in ("name", "entries", "root_headers", "replaced_paths", "whiteouts")} for row in layers] == known["layers"], "new replay layer details mismatch")
            flat = HOSTED / "reconstructed" / (arch + "-" + role + ".tar")
            need(digest(flat) == known["reconstructed_tar_sha256"] and retained_flat_map(flat) == full_map, "full flattened map differs from independently applied layers")
            root = image["inspection_container"]["root_directory"]
            cid = image["inspection_container"]["id"].removeprefix("sha256:")
            need(observations[cid] == [root, root] and root == {"uid": 0, "gid": 0, "mode": 0o555}, "host root observations")
            image_tree = ImageTree(flat, root)
            content = verify_content(image_tree, arch, role, artifacts, source_bytes)
            reported = load(directory / (role + "-content.json"))
            need(canonical(content) == canonical(reported) and sha(canonical(content)) == image["content_sha256"], "exact content replay mismatch")
            need(content["payload_sha256"] == image["payload_sha256"] == known["payload_sha256"], "payload mismatch")
            roles[role] = {"image_id": image["id"], "portable_sha256": digest(portable), "full_member_map_sha256": sha(canonical(full_map)),
                           "flat_sha256": digest(flat), "members_independently_compared": len(full_map), "hardlinks": hardlinks,
                           "layers": layers, "host_root": root, "content_sha256": image["content_sha256"], "payload_sha256": image["payload_sha256"],
                           "elf_count": len(content["elfs"]), "required_elf_count": sum(bool(v["required_dependency_closure"]) for v in content["elfs"].values()), "full_content_record_equal": True}
            del image_tree
        results[arch] = {"commands": counts, "stopped_lifecycles": 5, "root_observations": 6, "roles": roles}
    controls = negative_controls()
    need(not events_blocked, "unexpected blocked side effects occurred")
    result = {"status": "PASS", "architectures": results, "controls": controls, "control_count": len(controls),
              "negative_controls": sum(v["outcome"] == "rejected" for v in controls), "positive_controls": sum(v["outcome"] != "rejected" for v in controls),
              "process_network_audit_events_blocked": events_blocked, "image_execution": False, "image_path_extraction": False,
              "runtime_admission_certified": False, "native_calibration_performed": False,
              "python_version": sys.version, "assertions_enabled": True}
    with (OUT / "replay-checks.json").open("x") as stream:
        json.dump(result, stream, sort_keys=True, indent=2)
        stream.write("\n")
    print(json.dumps({"status": "PASS", "images": 6, "command_logs": 122, "never_started_lifecycles": 10, "controls": len(controls),
                      "negative_controls": result["negative_controls"], "positive_controls": result["positive_controls"], "blocked_side_effects": events_blocked}, sort_keys=True))


if __name__ == "__main__":
    main()
