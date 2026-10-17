"""Build and inspect one C0 platform image set; never start a C0 entrypoint."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import uuid

from build_tools import parse_elf, target_manifest
from contracts import canonical, decode, digest, exact, integer, platform_digest, require, runtime_architecture, sha
from image_artifacts import BASES, ImageTree, archive_record, image_id, image_metadata, verify_content
from oracle_protocol import SOURCE_FILES
from profiles import profile, target_filter_program
import struct

ROOT_METADATA_SCRIPT = '''import json,os,stat,subprocess,sys
command=[sys.argv[1],"--remote=false"]
try:
    path=subprocess.check_output(command+["mount",sys.argv[2]],text=True,timeout=30).strip()
    assert path.startswith("/") and "\\n" not in path
    value=os.stat(path,follow_symlinks=False)
    assert stat.S_ISDIR(value.st_mode)
    print(json.dumps({"uid":value.st_uid,"gid":value.st_gid,"mode":stat.S_IMODE(value.st_mode)}))
finally:
    subprocess.run(command+["unmount",sys.argv[2]],check=True,stdout=subprocess.DEVNULL,timeout=30)
'''


def source_files(root):
    result = {}
    for path in sorted(root.rglob("*")):
        if "__pycache__" in path.parts or path.suffix == ".pyc": continue
        require(not path.is_symlink(), "BUILD_SOURCE_LINK")
        if path.is_file(): result[path.relative_to(root).as_posix()] = sha(path.read_bytes())
    return result


def verify_artifacts(native, oracle, sources, architecture):
    expected = {"target", "observer", "cases.h", "target_filter.h", "target-filter.bpf", "target_manifest.h",
        "target-manifest.json", "toolchain.json", *(role + "-seccomp.json" for role in ("target", "observer", "oracle"))}
    require({p.name for p in native.iterdir()} == expected and {p.name for p in oracle.iterdir()} == {"oracle-artifacts.json"}, "BUILD_OUTPUT_INVENTORY")
    for path in (*native.iterdir(), *oracle.iterdir()):
        require(path.is_file() and not path.is_symlink() and path.stat().st_nlink == 1 and path.stat().st_size <= 32 * 1024 * 1024, "BUILD_OUTPUT_FILE")
    target, observer = (native / "target").read_bytes(), (native / "observer").read_bytes()
    require((native / "target-manifest.json").read_bytes() == canonical(target_manifest(target, architecture)), "BUILD_TARGET_MANIFEST")
    bpf = b"".join(struct.pack("<HBBI", *row) for row in target_filter_program(architecture))
    require((native / "target-filter.bpf").read_bytes() == bpf, "BUILD_FILTER")
    loads, symbols = parse_elf(observer, architecture)
    symbol = symbols["obs_filter"]
    segments = [p for p in loads if p[3] <= symbol["vaddr"] and symbol["vaddr"] + symbol["size"] <= p[3] + p[5]]
    require(len(segments) == 1 and symbol["size"] == len(bpf), "BUILD_COMPILED_FILTER")
    offset = segments[0][2] + symbol["vaddr"] - segments[0][3]
    require(observer[offset:offset + len(bpf)] == bpf and sha(target).encode() in observer, "BUILD_COMPILED_BINDING")
    for role in ("target", "observer", "oracle"):
        require((native / (role + "-seccomp.json")).read_bytes() == canonical(profile(role, architecture)), "BUILD_PROFILE")
    toolchain = decode((native / "toolchain.json").read_bytes())
    require(toolchain["version"] == 2 and toolchain["architecture"] == architecture and toolchain["platform_sha256"] == platform_digest(architecture), "BUILD_TOOLCHAIN_ARCHITECTURE")
    require(toolchain["target_sha256"] == sha(target) and toolchain["observer_sha256"] == sha(observer), "BUILD_TOOLCHAIN_ELF")
    require(toolchain["native_programs_executed"] is False and toolchain["tracing_performed"] is False and
        set(toolchain["libraries"]) == {"libc.a", "libcrypto.a", "crtbeginS.o"}, "BUILD_TOOLCHAIN_RECORD")
    for value in toolchain["libraries"].values(): digest(value)
    require(toolchain["commands"]["cc -dumpmachine"].strip().startswith(architecture + "-"), "BUILD_TOOLCHAIN_COMPILER")
    # Dockerignore is a closed build-input list; compare the recorded compiler inputs.
    expected_sources = {"Dockerfile", ".dockerignore", "contracts.py", "profiles.py", "build_tools.py"} | {
        name for name in sources if name.startswith("native/") and name.endswith((".c", ".h"))}
    require(toolchain["sources"] == {name: sources[name] for name in expected_sources}, "BUILD_TOOLCHAIN_SOURCE")
    manifest = decode((oracle / "oracle-artifacts.json").read_bytes())
    require(manifest["version"] == 3 and manifest["architecture"] == architecture and manifest["platform_sha256"] == platform_digest(architecture), "BUILD_ORACLE_ARCHITECTURE")
    require(manifest["files"] == {name: sources[name] for name in SOURCE_FILES} and
        manifest["oracle_worker_source"] == sha(canonical(manifest["files"])), "BUILD_ORACLE_SOURCE")
    require(all(manifest[key] is False for key in ("worker_executed", "runtime_feasibility_proven", "execution_authorized")), "BUILD_ORACLE_AUTHORITY")
    require(manifest["worker_import_checked"] is True and type(manifest["extensions"]) is dict and 0 < len(manifest["extensions"]) <= 128, "BUILD_ORACLE_EXTENSIONS")
    for name, value in manifest["extensions"].items():
        require(type(name) is str and name.startswith("/") and len(name) <= 4096, "BUILD_ORACLE_EXTENSION_PATH")
        digest(value)
    return {"target": target, "observer": observer, "oracle_elf": manifest["oracle_elf"], "extensions": manifest["extensions"]}


class Builder:
    def __init__(self, engine, output):
        require(engine == "podman", "BUILD_ENGINE")
        executable = shutil.which(engine); require(executable is not None, "BUILD_ENGINE_MISSING")
        self.engine, self.output = engine, output
        self.prefix = [executable, "--remote=false"]
        self.commands, self.cleanup = [], []

    def run(self, args, *, allow_failure=False, timeout=900):
        command = self.prefix + list(args)
        index = len(self.commands)
        self.commands.append({"argv": command})
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout,
            env=os.environ | {"TMPDIR": str(self.output), "DOCKER_BUILDKIT": "1"})
        require(len(result.stdout) + len(result.stderr) <= 16 * 1024 * 1024, "BUILD_LOG_BOUND")
        log = self.output / ("command-%03d.log" % index)
        log.write_bytes(result.stdout + result.stderr)
        self.commands[-1].update(returncode=result.returncode, log=log.name, log_sha256=sha(log.read_bytes()))
        require(allow_failure or result.returncode == 0, "BUILD_COMMAND_FAILED")
        return result.returncode, result.stdout

    def inspect(self, kind, reference):
        _, raw = self.run([kind, "inspect", reference], timeout=30)
        rows = json.loads(raw); require(type(rows) is list and len(rows) == 1, "BUILD_INSPECTION")
        return rows[0]

    def stopped(self, identifier, expected_image, name):
        value = self.inspect("container", identifier)
        require(image_id(value["Id"]) == image_id(identifier) and image_id(value["Image"]) == expected_image and
            value["Name"].lstrip("/") == name, "BUILD_CONTAINER_IDENTITY")
        state = value["State"]
        require(state["Running"] is False and type(state["Pid"]) is int and state["Pid"] == 0 and state["Status"] in ("created", "configured") and
            state.get("StartedAt") in ("0001-01-01T00:00:00Z", "0001-01-01T00:00:00+00:00"), "BUILD_CONTAINER_STARTED")
        return {"id": image_id(identifier), "image": expected_image, "name": name, "running": False, "pid": 0, "status": state["Status"]}

    def root_metadata(self, identifier):
        # Read only the owned, stopped filesystem's root attributes in Podman's
        # existing rootless storage namespace. No container process is started.
        _, raw = self.run(["unshare", sys.executable, "-I", "-B", "-c", ROOT_METADATA_SCRIPT, self.prefix[0], identifier], timeout=90)
        value = json.loads(raw)
        exact(value, ("uid", "gid", "mode"))
        for name, limit in (("uid", 2**32 - 1), ("gid", 2**32 - 1), ("mode", 0o7777)):
            integer(value[name], 0, limit)
        return value

    def export(self, image, destination, *, directory=None):
        name = "obs1-image-inspect-" + uuid.uuid4().hex
        identifier = None
        try:
            _, raw = self.run(["create", "--name", name, "--network", "none", "--read-only", "--user", "999:999", "--cap-drop", "ALL",
                "--security-opt", "no-new-privileges", "--no-healthcheck", "--entrypoint", "/__obs1_inspection_only__", image], timeout=60)
            identifier = image_id(raw.decode().strip())[7:]
            before = self.stopped(identifier, image, name)
            root = self.root_metadata(identifier) if directory is None else None
            if directory is None:
                self.run(["export", "--output", str(destination), identifier], timeout=120)
            else:
                destination.mkdir(mode=0o700)
                self.run(["cp", identifier + ":" + directory + "/.", str(destination)], timeout=120)
            require(self.stopped(identifier, image, name) == before, "BUILD_CONTAINER_CHANGED")
            if root is not None:
                require(self.root_metadata(identifier) == root, "BUILD_ROOT_CHANGED")
            return before | ({"root_directory": root} if root is not None else {})
        finally:
            # A failed create may have created an object even without returning its ID.
            if identifier is None:
                _, found = self.run(["ps", "--all", "--no-trunc", "--filter", "name=^" + name + "$", "--format", "{{.ID}}"], timeout=30)
                if found.strip(): identifier = image_id(found.decode().strip())[7:]
            if identifier is not None:
                proof = self.stopped(identifier, image, name)
                self.run(["rm", identifier], timeout=60)
                _, remaining = self.run(["ps", "--all", "--no-trunc", "--filter", "id=" + identifier, "--format", "{{.ID}}"], timeout=30)
                require(not remaining.strip(), "BUILD_CONTAINER_CLEANUP")
                self.cleanup.append(proof | {"removed": True})

    def build(self, source, architecture, recipe, target, label):
        selected = runtime_architecture(architecture)
        iid = self.output / (label + ".iid")
        options = ["--pull=never", "--http-proxy=false", "--layers=false", "--force-rm", "--identity-label=false", "--ignorefile",
            str(source / ("Dockerfile.oracle.dockerignore" if recipe == "Dockerfile.oracle" else ".dockerignore"))]
        if target in ("target", "observer"):
            options += ["--unsetenv", "PATH"]
        self.run(["build", "--no-cache", "--platform", selected["oci_platform"], "--build-arg", "C0_ARCHITECTURE=" + architecture,
            "--network", "none", *options, "--target", target, "--file", str(source / recipe), "--iidfile", str(iid), str(source)])
        return image_id(iid.read_text().strip())


def prepare(architecture, engine, output):
    selected = runtime_architecture(architecture)
    package = Path(__file__).resolve().parent
    require(engine == "podman" and not output.resolve().is_relative_to(package), "BUILD_OUTPUT_SCOPE")
    require(output.is_absolute() and not output.exists(), "BUILD_OUTPUT_DIRECTORY")
    output.mkdir(mode=0o700)
    builder = Builder(engine, output)
    record = {"version": 1, "kind": "c0_runtime_image_set", "architecture": architecture, "platform_sha256": platform_digest(architecture),
        "status": "FAIL", "engine": engine, "commit": os.environ.get("GITHUB_SHA", "local"),
        "inspection_containers_started": 0, "c0_actors_started": 0, "build_tool_processes_executed": True,
        "cleanup_complete": False, "runtime_acceptance": False, "execution_authorized": False}
    try:
        _, rootless = builder.run(["info", "--format", "{{.Host.Security.Rootless}}"], timeout=30)
        require(rootless.strip() == b"true", "BUILD_ROOTLESS_REQUIRED")
        _, version = builder.run(["version"], timeout=30)
        record["builder_version"] = version.decode("utf-8")
        before = source_files(package)
        source = output / "source"
        shutil.copytree(package, source, ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
        require(source_files(source) == before == source_files(package), "BUILD_SOURCE_CHANGED")
        record["source_files"] = before
        for recipe, kind in (("Dockerfile", "compiler"), ("Dockerfile.oracle", "python")):
            text = (source / recipe).read_text()
            for arch, bases in BASES.items(): require(bases[kind] in text, "BUILD_RECIPE_PIN")
        bases = {}
        for kind, reference in BASES[architecture].items():
            builder.run(["pull", "--platform", selected["oci_platform"], reference], timeout=600)
            value = builder.inspect("image", reference)
            require(value["Os"] == "linux" and value["Architecture"] == selected["image_architecture"] and reference in value["RepoDigests"], "BUILD_BASE_IDENTITY")
            bases[kind] = {"reference": reference, "id": image_id(value["Id"]), "environment": value["Config"].get("Env") or []}
        record["bases"] = bases
        native_image = builder.build(source, architecture, "Dockerfile", "build", "native-builder")
        oracle_image = builder.build(source, architecture, "Dockerfile.oracle", "oracle-build", "oracle-builder")
        builder.export(native_image, output / "native", directory="/out")
        builder.export(oracle_image, output / "oracle", directory="/artifacts")
        artifacts = verify_artifacts(output / "native", output / "oracle", before, architecture)
        record["artifacts"] = {str(p.relative_to(output)): sha(p.read_bytes()) for folder in (output / "native", output / "oracle") for p in sorted(folder.iterdir())}
        images = {}
        for role in ("target", "observer", "oracle"):
            identity = builder.build(source, architecture, "Dockerfile.oracle" if role == "oracle" else "Dockerfile", role, role)
            metadata = builder.inspect("image", identity)
            env = bases["python"]["environment"] if role == "oracle" else []
            admitted = image_metadata(metadata, architecture, role, env)
            require(admitted["id"] == identity, "BUILD_IMAGE_IDENTITY")
            archive = output / (role + ".tar")
            stopped = builder.export(identity, archive)
            content = verify_content(ImageTree(archive, stopped["root_directory"]), architecture, role, artifacts,
                {name: (source / name).read_bytes() for name in (*SOURCE_FILES, "build_oracle.py")})
            require(image_metadata(builder.inspect("image", identity), architecture, role, env) == admitted, "BUILD_IMAGE_CHANGED")
            (output / (role + "-inspect.json")).write_bytes(canonical(metadata))
            (output / (role + "-content.json")).write_bytes(canonical(content))
            portable = output / (role + "-image.tar")
            builder.run(["save", "--format", "docker-archive", "--output", str(portable), identity], timeout=120)
            saved = archive_record(portable, identity, architecture)
            images[role] = admitted | {"content_sha256": sha(canonical(content)), "payload_sha256": content["payload_sha256"],
                "elf_files": len(content["elfs"]), "inspection_container": stopped, "portable_archive": saved,
                "engine_manifest_digest": metadata.get("Digest"), "inspect_sha256": sha(canonical(metadata))}
        require(source_files(source) == before == source_files(package), "BUILD_SOURCE_CHANGED")
        require(len(builder.cleanup) == 5, "BUILD_CLEANUP_COUNT")
        record.update(status="PASS_STATIC_IMAGE_SET", images=images, source_snapshot_sha256=sha(canonical(before)),
            stable_artifacts_sha256=sha(canonical(record["artifacts"])), cleanup_complete=True)
        return record
    finally:
        record.update(commands=builder.commands, cleanup=builder.cleanup)
        (output / "image-set.json").write_bytes(canonical(record))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--architecture", required=True, choices=("aarch64", "x86_64"))
    parser.add_argument("--engine", choices=("podman",), default="podman")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    result = prepare(args.architecture, args.engine, args.output)
    print(canonical({key: result[key] for key in ("status", "architecture", "images", "cleanup_complete", "runtime_acceptance")}).decode(), end="")
    return 0
