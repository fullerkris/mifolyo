"""Read-only, bounded checks of exported image contents; never extract or execute."""
import os
import hashlib
import json
from collections import deque
from pathlib import PurePosixPath
import re
import stat
import struct
import tarfile

from contracts import canonical, digest, elf_architecture, exact, integer, require, runtime_architecture, sha
from oracle_protocol import SOURCE_FILES

BASES = {
    "aarch64": {"compiler": "docker.io/library/gcc@sha256:66035d353338cb93b64f621393dc6fecde85258651ca454f0cf36ff2639b1352",
        "python": "docker.io/library/python@sha256:ad4c34ff79289506e235b40dce75d629e25f226b597a2455804220f037e07531"},
    "x86_64": {"compiler": "docker.io/library/gcc@sha256:a689e29bc3adf4663ef9a141d23081252764d1319c63f591a027bd6fd676f4c1",
        "python": "docker.io/library/python@sha256:3e2de9c40ca4e3d73240059f9d48baff27908f10293e985a2f382a0378e6df4a"}}
MAX_FILE = 64 * 1024 * 1024
MAX_TOTAL = 512 * 1024 * 1024


def image_id(value):
    require(type(value) is str, "IMAGE_ID")
    value = value.removeprefix("sha256:")
    digest(value)
    return "sha256:" + value


def image_metadata(value, architecture, role, environment):
    selected = runtime_architecture(architecture)
    require(role in ("target", "observer", "oracle"), "IMAGE_ROLE")
    require(type(value) is dict and value.get("Os") == "linux" and value.get("Architecture") == selected["image_architecture"], "IMAGE_PLATFORM")
    identity = image_id(value.get("Id"))
    config = value.get("Config")
    require(type(config) is dict, "IMAGE_CONFIG")
    require(config.get("User") == "999:999" and config.get("WorkingDir") in (None, "", "/"), "IMAGE_USER")
    require(config.get("Entrypoint") == (["python3"] if role == "oracle" else ["/" + role]), "IMAGE_ENTRYPOINT")
    require(config.get("Cmd") == ["-I", "-B", "/oracle/worker.py"] if role == "oracle" else config.get("Cmd") in (None, []), "IMAGE_COMMAND")
    for name, kind in (("OnBuild", list), ("Volumes", dict), ("Healthcheck", dict), ("ExposedPorts", dict), ("Labels", dict)):
        value = config.get(name)
        require(value is None or type(value) is kind and not value, "IMAGE_HOOK")
    require(type(environment) is list and all(type(row) is str and "=" in row and "\0" not in row for row in environment), "IMAGE_ENVIRONMENT")
    require(len({row.split("=", 1)[0] for row in environment}) == len(environment), "IMAGE_ENVIRONMENT")
    actual_environment = [] if config.get("Env") is None else config["Env"]
    require(type(actual_environment) is list and actual_environment == environment and (role == "oracle" or environment == []), "IMAGE_ENVIRONMENT")
    return {"id": identity, "architecture": architecture, "role": role, "environment_sha256": sha(canonical(environment)),
        "config_sha256": sha(canonical(config)), "execution_authorized": False}


def path_parts(path):
    require(type(path) is str and 0 < len(path) <= 4096 and not any(c in path for c in "\0\r\n"), "IMAGE_PATH")
    return path.split("/")


def normalized(path):
    """Lexical archive-name normalization. Link traversal uses component order."""
    parts = []
    for part in path_parts(path):
        if part in ("", "."): continue
        if part == "..":
            require(bool(parts), "IMAGE_PATH_ESCAPE")
            parts.pop()
        else: parts.append(part)
    return "/" + "/".join(parts)


class ImageTree:
    def __init__(self, path, root_metadata=None):
        self.entries, total = {}, 0
        self.member_count, self.root = 0, None
        if root_metadata is not None:
            exact(root_metadata, ("mode", "uid", "gid"))
            for name, limit in (("mode", 0o7777), ("uid", 2**32 - 1), ("gid", 2**32 - 1)):
                integer(root_metadata[name], 0, limit)
            self.root = dict(root_metadata)
        fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, "rb") as stream:
            before = os.fstat(stream.fileno())
            require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and 0 < before.st_size <= MAX_TOTAL + 32 * 1024 * 1024, "IMAGE_ARCHIVE")
            with tarfile.open(fileobj=stream, mode="r:") as archive:
                root_seen = False
                for member_count, member in enumerate(archive, 1):
                    require(member_count <= 20000, "IMAGE_DUPLICATE_OR_COUNT")
                    self.member_count = member_count
                    require(0 <= member.uid <= 2**32 - 1 and 0 <= member.gid <= 2**32 - 1 and member.mode & ~0o7777 == 0, "IMAGE_METADATA")
                    row = {"mode": member.mode, "uid": member.uid, "gid": member.gid}
                    name = member.name.rstrip("/")
                    if name in ("", "."):
                        require(member.isdir() and member.size == 0 and not root_seen, "IMAGE_ROOT_DUPLICATE")
                        require(self.root is None or self.root == row, "IMAGE_ROOT_CHANGED")
                        self.root, root_seen = row, True
                        continue
                    require(not name.startswith("/") and ".." not in name.split("/"), "IMAGE_MEMBER_PATH")
                    name = normalized(name)
                    if name == "/":
                        require(member.isdir() and member.size == 0 and not root_seen, "IMAGE_ROOT_DUPLICATE")
                        require(self.root is None or self.root == row, "IMAGE_ROOT_CHANGED")
                        self.root, root_seen = row, True
                        continue
                    require(name not in self.entries and len(self.entries) < 20000, "IMAGE_DUPLICATE_OR_COUNT")
                    require(not any(part.startswith(".wh.") for part in name.split("/")), "IMAGE_WHITEOUT")
                    if member.isfile():
                        require(not member.issparse() and 0 <= member.size <= MAX_FILE and total + member.size <= MAX_TOTAL, "IMAGE_FILE_BOUND")
                        with archive.extractfile(member) as data:
                            raw = data.read(member.size + 1)
                        require(len(raw) == member.size, "IMAGE_FILE_SIZE")
                        row.update(kind="file", raw=raw, sha256=sha(raw)); total += len(raw)
                    elif member.isdir():
                        require(member.size == 0, "IMAGE_DIRECTORY")
                        row.update(kind="directory")
                    elif member.issym() or member.islnk():
                        target = member.linkname
                        require(type(target) is str and target and member.size == 0, "IMAGE_LINK")
                        base = str(PurePosixPath(name).parent) + "/" if member.issym() and not target.startswith("/") else "/"
                        normalized(base + target)  # Validate lexical bounds without losing link/.. order.
                        row.update(kind="link", target=base + target, hardlink=member.islnk())
                    else: require(False, "IMAGE_SPECIAL_FILE")
                    self.entries[name] = row
            after = os.fstat(stream.fileno())
            require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns) ==
                (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns), "IMAGE_ARCHIVE_CHANGED")
        self.total_bytes = total
        # Flattened filesystem entries must have literal directory ancestors.
        # Otherwise a name lookup could inspect a different, shadowing entry.
        for name in self.entries:
            parent = str(PurePosixPath(name).parent)
            require(parent == "/" or self.entries.get(parent, {}).get("kind") == "directory", "IMAGE_MEMBER_PARENT")
        # Every alias shares inode metadata, even if the selected role never
        # traverses it. Wait for the complete map so forward targets are valid.
        for name, row in self.entries.items():
            if row.get("hardlink"):
                self.resolve(name)

    def _resolve(self, path, missing_ok=False):
        pending = deque((part, False) for part in path_parts(path))
        resolved, directories, links, hard_target, hard_metadata = [], ["/"], 0, False, []
        while pending:
            part, target_component = pending.popleft()
            if part in ("", "."): continue
            if part == "..":
                require(bool(resolved), "IMAGE_PATH_ESCAPE")
                resolved.pop(); continue
            prefix = "/" + "/".join([*resolved, part])
            row = self.entries.get(prefix)
            if row is None:
                require(not target_component, "IMAGE_DANGLING_LINK")
                if missing_ok: return None, directories
                require(False, "IMAGE_FILE_MISSING")
            if row["kind"] == "link":
                require(not hard_target, "IMAGE_HARDLINK_LINK_TARGET")
                links += 1; require(links <= 40, "IMAGE_LINK_DEPTH")
                require(not row.get("hardlink") or not pending, "IMAGE_PATH_PARENT")
                hard_target |= row.get("hardlink", False)
                if row.get("hardlink"):
                    hard_metadata.append({key: row[key] for key in ("uid", "gid", "mode")})
                target = path_parts(row["target"])
                require(len(row["target"]) + sum(len(p) + 1 for p, _ in pending) <= 4096, "IMAGE_PATH")
                pending = deque([(p, True) for p in target] + list(pending)); resolved = []
            else:
                require(not pending or row["kind"] == "directory", "IMAGE_PATH_PARENT")
                resolved.append(part)
                if row["kind"] == "directory": directories.append(prefix)
        name = "/" + "/".join(resolved)
        require(name == "/" or name in self.entries, "IMAGE_FILE_MISSING")
        require(not hard_target or name != "/" and self.entries[name]["kind"] == "file", "IMAGE_HARDLINK_TARGET")
        require(all(metadata == {key: self.entries[name][key] for key in ("uid", "gid", "mode")} for metadata in hard_metadata), "IMAGE_HARDLINK_METADATA")
        return name, directories

    def resolve(self, path, missing_ok=False):
        return self._resolve(path, missing_ok)[0]

    def directory_access(self, name):
        row = self.root if name == "/" else self.entries.get(name)
        require(row is not None and row["uid"] == row["gid"] == 0 and row["mode"] & 0o005 == 0o005 and
            not row["mode"] & 0o6022, "IMAGE_DIRECTORY_ACCESS")

    def file(self, path, executable=False):
        name, directories = self._resolve(path)
        for directory in directories: self.directory_access(directory)
        row = self.entries.get(name)
        require(row is not None and row["kind"] == "file" and row["uid"] == row["gid"] == 0 and
            row["mode"] & 0o004 and not row["mode"] & 0o6022, "IMAGE_FILE_IDENTITY")
        require(not executable or row["mode"] & 0o001, "IMAGE_FILE_EXECUTABLE")
        return row["raw"]


def elf_record(raw, architecture):
    selected = runtime_architecture(architecture)
    require(64 <= len(raw) <= MAX_FILE and raw[:7] == b"\x7fELF\x02\x01\x01", "IMAGE_ELF_ARCHITECTURE")
    h = struct.unpack_from("<16sHHIQQQIHHHHHH", raw)
    require(h[2] == selected["elf_machine"] and h[3] == 1, "IMAGE_ELF_ARCHITECTURE")
    if h[1] == 1:  # Python's pinned base includes a non-loadable config/python.o.
        require(h[10] == 0 and h[11] == 64 and 0 < h[12] <= 1024 and h[6] + 64 * h[12] <= len(raw), "IMAGE_ELF_RELOCATABLE")
        for index in range(h[12]):
            section = struct.unpack_from("<IIQQQQIIQQ", raw, h[6] + 64 * index)
            require(section[1] == 8 or section[4] + section[5] <= len(raw), "IMAGE_ELF_RELOCATABLE")
        return {"sha256": sha(raw), "bytes": len(raw), "machine": h[2], "type": 1, "interpreter": None, "needed": [], "search_paths": [], "rpath": [], "runpath": []}
    require(elf_architecture(raw[:64]) == architecture, "IMAGE_ELF_ARCHITECTURE")
    phoff, size, count = h[5], h[9], h[10]
    require(size == 56 and 0 < count <= 128 and phoff + size * count <= len(raw), "IMAGE_ELF_TABLE")
    rows = [struct.unpack_from("<IIQQQQQQ", raw, phoff + size * i) for i in range(count)]
    require(all(p[2] + p[5] <= len(raw) for p in rows if p[0] in (1, 2, 3)), "IMAGE_ELF_BOUNDS")
    loads = [p for p in rows if p[0] == 1]
    require(loads and all(p[5] <= p[6] and p[1] & 3 != 3 for p in loads), "IMAGE_ELF_LOAD")
    require(all(not p[1] & 1 for p in rows if p[0] == 0x6474e551), "IMAGE_ELF_STACK")
    interpreters = [raw[p[2]:p[2] + p[5]] for p in rows if p[0] == 3]
    require(len(interpreters) <= 1, "IMAGE_ELF_INTERPRETER")
    interpreter = None
    if interpreters:
        value = interpreters[0]
        require(value.endswith(b"\0") and b"\0" not in value[:-1], "IMAGE_ELF_INTERPRETER")
        interpreter = value[:-1].decode("ascii"); require(interpreter.startswith("/"), "IMAGE_ELF_INTERPRETER")
    dynamic = [p for p in rows if p[0] == 2]
    require(len(dynamic) <= 1, "IMAGE_ELF_DYNAMIC")
    tags = []
    if dynamic:
        p = dynamic[0]; require(p[5] % 16 == 0, "IMAGE_ELF_DYNAMIC")
        for offset in range(p[2], p[2] + p[5], 16):
            tag, value = struct.unpack_from("<QQ", raw, offset)
            if tag == 0: break
            tags.append((tag, value))
        else: require(False, "IMAGE_ELF_DYNAMIC_END")
    strings = b""
    if tags:
        pointers = [v for t, v in tags if t == 5]; lengths = [v for t, v in tags if t == 10]
        require(len(pointers) == len(lengths) == 1 and 0 < lengths[0] <= MAX_FILE, "IMAGE_ELF_STRINGS")
        match = [p for p in loads if p[3] <= pointers[0] and pointers[0] + lengths[0] <= p[3] + p[5]]
        require(len(match) == 1, "IMAGE_ELF_STRINGS")
        offset = match[0][2] + pointers[0] - match[0][3]
        strings = raw[offset:offset + lengths[0]]
    def text(offset):
        require(offset < len(strings), "IMAGE_ELF_STRING")
        end = strings.find(b"\0", offset); require(end >= offset, "IMAGE_ELF_STRING")
        return strings[offset:end].decode("ascii")
    needed = [text(v) for t, v in tags if t == 1]
    require(all(re.fullmatch(r"[A-Za-z0-9_.+-]+", name) and name != "PT_INTERP" for name in needed) and
        len(set(needed)) == len(needed), "IMAGE_ELF_NEEDED")
    rpath = [text(v) for t, v in tags if t == 15]
    runpath = [text(v) for t, v in tags if t == 29]
    require(len(rpath) <= 1 and len(runpath) <= 1, "IMAGE_ELF_SEARCH_TAGS")
    return {"sha256": sha(raw), "bytes": len(raw), "machine": h[2], "type": h[1], "interpreter": interpreter,
        "needed": needed, "search_paths": runpath if runpath else rpath, "rpath": rpath, "runpath": runpath}


def elf_inventory(tree, architecture, required=None):
    runtime_architecture(architecture)
    records = {name: elf_record(row["raw"], architecture) for name, row in tree.entries.items()
        if row["kind"] == "file" and row["raw"].startswith(b"\x7fELF")}
    require(records, "IMAGE_ELFS_MISSING")
    triplet = "aarch64-linux-gnu" if architecture == "aarch64" else "x86_64-linux-gnu"
    defaults = ["/usr/local/lib", "/lib/" + triplet, "/usr/lib/" + triplet, "/lib", "/usr/lib"]
    for name, record in records.items():
        search = []
        for paths in record["search_paths"]:
            for path in paths.split(":"):
                path = path.replace("${ORIGIN}", str(PurePosixPath(name).parent)).replace("$ORIGIN", str(PurePosixPath(name).parent))
                require(path.startswith("/") and "$" not in path, "IMAGE_ELF_SEARCH_PATH")
                path_parts(path); search.append(path)
        dependencies, dependency_paths, missing = {}, {}, []
        for needed in record["needed"]:
            candidates = []
            for parent in search + defaults:
                candidate = parent + "/" + needed
                resolved = tree.resolve(candidate, missing_ok=True)
                if resolved is not None: candidates.append((resolved, candidate))
            require(len({name for name, _ in candidates}) <= 1, "IMAGE_LIBRARY_AMBIGUOUS")
            if candidates and candidates[0][0] in records and records[candidates[0][0]]["type"] == 3:
                dependencies[needed], dependency_paths[needed] = candidates[0]
            else:
                missing.append(needed)
        if record["interpreter"]:
            loader = tree.resolve(record["interpreter"])
            require(loader in records and records[loader]["type"] in (2, 3), "IMAGE_LOADER_MISSING")
            dependencies["PT_INTERP"] = loader
            dependency_paths["PT_INTERP"] = record["interpreter"]
        record["dependencies"] = dependencies
        record["dependency_paths"] = dependency_paths
        record["unresolved_dependencies"] = missing
    pending = [(path, False) for path in (records if required is None else required)]
    visited = set()
    while pending:
        path, executable = pending.pop()
        tree.file(path, executable=executable)
        name = tree.resolve(path)
        if name in visited: continue
        require(name in records and not records[name]["unresolved_dependencies"], "IMAGE_LIBRARY_MISSING")
        # Legacy RPATH inheritance is deliberately outside this pinned-base model.
        require(not records[name]["rpath"] or records[name]["runpath"], "IMAGE_UNSUPPORTED_RPATH")
        visited.add(name)
        pending.extend((path, edge == "PT_INTERP") for edge, path in records[name]["dependency_paths"].items())
    for name in records: records[name]["required_dependency_closure"] = name in visited
    return dict(sorted(records.items()))


def verify_content(tree, architecture, role, artifacts, sources):
    require(role in ("target", "observer", "oracle"), "IMAGE_ROLE")
    if role == "oracle":
        require(type(artifacts.get("extensions")) is dict and 0 < len(artifacts["extensions"]) <= 128, "IMAGE_ORACLE_EXTENSIONS")
    required = ["/" + role] if role != "oracle" else ["/usr/local/bin/python3", *artifacts["extensions"]]
    elfs = elf_inventory(tree, architecture, required)
    if role != "oracle":
        raw = tree.file("/" + role, executable=True)
        generated = {"/etc/hosts", "/etc/hostname", "/etc/resolv.conf", "/.dockerenv", "/run/.containerenv"}
        require({name for name, row in tree.entries.items() if row["kind"] == "file"} <= generated | {"/" + role}, "IMAGE_NATIVE_FILES")
        require(raw == artifacts[role] and set(elfs) == {"/" + role} and elfs["/" + role]["type"] == 3 and not elfs["/" + role]["needed"] and
            elfs["/" + role]["interpreter"] is None, "IMAGE_NATIVE_CONTENT")
        declared = {"/" + role: sha(raw)}
    else:
        declared = {}
        expected = set(SOURCE_FILES) | {"build_oracle.py"}
        require({name.removeprefix("/oracle/") for name in tree.entries if name.startswith("/oracle/")} == expected, "IMAGE_ORACLE_INVENTORY")
        for name in sorted(expected):
            raw = tree.file("/oracle/" + name)
            require(raw == sources[name], "IMAGE_ORACLE_SOURCE")
            declared["/oracle/" + name] = sha(raw)
        python = tree.resolve("/usr/local/bin/python3")
        require(python in elfs and elfs[python]["type"] in (2, 3) and sha(tree.file(python, executable=True)) == artifacts["oracle_elf"], "IMAGE_ORACLE_ELF")
        declared[python] = artifacts["oracle_elf"]
        for name, expected in artifacts["extensions"].items():
            resolved = tree.resolve(name)
            require(resolved in elfs and elfs[resolved]["type"] == 3 and sha(tree.file(resolved)) == expected, "IMAGE_ORACLE_EXTENSION")
            declared[resolved] = expected
    return {"version": 2, "architecture": architecture, "role": role, "declared_files": declared, "elfs": elfs,
        "root_directory": tree.root,
        "payload_sha256": sha(canonical({"declared_files": declared, "elfs": elfs})),
        "archive_members": tree.member_count, "regular_file_bytes": tree.total_bytes,
        "runtime_acceptance": False, "execution_authorized": False}


def archive_record(path, identity, architecture):
    """Check a portable Docker archive's actual config and uncompressed layers."""
    selected = runtime_architecture(architecture)
    identity = image_id(identity)
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and 0 < before.st_size <= MAX_TOTAL, "IMAGE_ARCHIVE")
        with tarfile.open(fileobj=stream, mode="r:") as archive:
            members = {}
            seen_paths = set()
            for member in archive:
                require(member.name not in members and len(members) < 1024 and (member.isfile() or member.isdir() or member.issym()), "IMAGE_SAVE_MEMBER")
                require(not member.name.startswith("/") and ".." not in member.name.split("/"), "IMAGE_SAVE_PATH")
                key = normalized(member.name)
                require(key not in seen_paths, "IMAGE_SAVE_DUPLICATE_PATH")
                seen_paths.add(key)
                members[member.name] = member
            def pairs(rows):
                result = {}
                for key, value in rows:
                    require(key not in result, "IMAGE_SAVE_DUPLICATE_JSON")
                    result[key] = value
                return result
            def data(name, limit):
                member = members.get(name)
                require(member is not None and member.isfile() and 0 < member.size <= limit, "IMAGE_SAVE_FILE")
                with archive.extractfile(member) as file: return file.read(limit + 1)
            manifest = json.loads(data("manifest.json", 1024 * 1024), object_pairs_hook=pairs)
            require(type(manifest) is list and len(manifest) == 1 and type(manifest[0]) is dict, "IMAGE_SAVE_MANIFEST")
            raw = data(manifest[0]["Config"], 1024 * 1024)
            require("sha256:" + sha(raw) == identity, "IMAGE_SAVE_CONFIG")
            config = json.loads(raw, object_pairs_hook=pairs)
            require(config["architecture"] == selected["image_architecture"] and config["os"] == "linux", "IMAGE_SAVE_PLATFORM")
            layers = manifest[0]["Layers"]
            expected = config["rootfs"]["diff_ids"]
            require(type(layers) is list and 0 < len(layers) == len(expected) <= 64, "IMAGE_SAVE_LAYERS")
            for member in members.values():
                if member.issym():
                    # Podman emits legacy Docker layer.tar aliases. Never follow
                    # them: permit only an in-archive alias to a listed regular layer.
                    require(member.name.endswith("/layer.tar") and not member.linkname.startswith("/"), "IMAGE_SAVE_LINK")
                    target = normalized(str(PurePosixPath(member.name).parent) + "/" + member.linkname).lstrip("/")
                    require(target in layers and target in members and members[target].isfile(), "IMAGE_SAVE_LINK")
            verified, total = [], 0
            for name, wanted in zip(layers, expected):
                member = members.get(name)
                require(member is not None and member.isfile() and 0 < member.size <= MAX_TOTAL and total + member.size <= MAX_TOTAL, "IMAGE_SAVE_LAYER")
                digest = hashlib.sha256()
                with archive.extractfile(member) as file:
                    for chunk in iter(lambda: file.read(65536), b""): digest.update(chunk)
                require("sha256:" + digest.hexdigest() == wanted, "IMAGE_SAVE_LAYER_DIGEST")
                verified.append(wanted); total += member.size
        stream.seek(0); digest = hashlib.sha256()
        for chunk in iter(lambda: stream.read(65536), b""): digest.update(chunk)
        after = os.fstat(stream.fileno())
        require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns) ==
            (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns), "IMAGE_ARCHIVE_CHANGED")
    return {"format": "docker-archive", "sha256": digest.hexdigest(), "bytes": before.st_size,
        "config_id": identity, "layers": verified, "architecture": architecture}
