"""Inert rootfs/image archives and simulated stopped-image lifecycle controls."""
import copy
import io
import json
from pathlib import Path
import struct
import tarfile
import tempfile
import unittest

import contracts as c
from image_artifacts import ImageTree, archive_record, elf_inventory, elf_record, image_metadata, verify_content
from image_build import Builder, prepare
from oracle_protocol import SOURCE_FILES

ID = "sha256:" + "a" * 64


def elf(architecture="x86_64", needed=(), interpreter=None):
    raw = bytearray(4096)
    count = 1 + bool(needed) + bool(interpreter)
    struct.pack_into("<16sHHIQQQIHHHHHH", raw, 0, b"\x7fELF\x02\x01\x01" + bytes(9), 3,
        62 if architecture == "x86_64" else 183, 1, 0x100, 64, 0, 0, 64, 56, count, 0, 0, 0)
    struct.pack_into("<IIQQQQQQ", raw, 64, 1, 5, 0, 0, 0, len(raw), len(raw), 4096)
    cursor = 120
    if interpreter:
        data = interpreter.encode() + b"\0"; raw[0x200:0x200 + len(data)] = data
        struct.pack_into("<IIQQQQQQ", raw, cursor, 3, 4, 0x200, 0x200, 0, len(data), len(data), 1); cursor += 56
    if needed:
        strings, offsets = bytearray(b"\0"), []
        for name in needed:
            offsets.append(len(strings)); strings.extend(name.encode() + b"\0")
        raw[0x500:0x500 + len(strings)] = strings
        tags = [(5, 0x500), (10, len(strings)), *((1, offset) for offset in offsets), (0, 0)]
        struct.pack_into("<IIQQQQQQ", raw, cursor, 2, 4, 0x300, 0x300, 0, len(tags) * 16, len(tags) * 16, 8)
        for index, (tag, value) in enumerate(tags): struct.pack_into("<QQ", raw, 0x300 + 16 * index, tag, value)
    return bytes(raw)


def tar(path, rows):
    with tarfile.open(path, "w") as out:
        root = tarfile.TarInfo("."); root.type = tarfile.DIRTYPE; root.mode = 0o755; out.addfile(root)
        for name, kind, value in rows:
            member = tarfile.TarInfo(name); member.mode = 0o755
            if kind == "file":
                member.size = len(value); out.addfile(member, io.BytesIO(value))
            else:
                member.type = {"directory": tarfile.DIRTYPE, "link": tarfile.SYMTYPE, "hardlink": tarfile.LNKTYPE,
                    "fifo": tarfile.FIFOTYPE, "device": tarfile.CHRTYPE}[kind]
                member.linkname = value; out.addfile(member)


def metadata(role="target", architecture="x86_64"):
    return {"Id": ID, "Os": "linux", "Architecture": c.platform(architecture)["image_architecture"], "Config": {
        "User": "999:999", "WorkingDir": "/", "Env": ["PATH=/usr/local/bin"] if role == "oracle" else [],
        "Entrypoint": ["python3"] if role == "oracle" else ["/" + role],
        "Cmd": ["-I", "-B", "/oracle/worker.py"] if role == "oracle" else None}}


class ImageMetadataTests(unittest.TestCase):
    def test_all_platform_role_metadata_and_foreign_architecture_refusal(self):
        for arch in ("aarch64", "x86_64"):
            for role in ("target", "observer", "oracle"):
                value = metadata(role, arch)
                self.assertEqual(image_metadata(value, arch, role, value["Config"]["Env"])["id"], ID)
                value["Architecture"] = "amd64" if arch == "aarch64" else "arm64"
                with self.assertRaises(c.Invalid): image_metadata(value, arch, role, value["Config"]["Env"])

    def test_metadata_mutations_including_builder_defaults_are_refused(self):
        for key, value in (("User", "0"), ("Entrypoint", ["/bin/sh"]), ("Cmd", ["run"]), ("WorkingDir", "/tmp"),
                ("Env", ["PATH=/usr/bin"]), ("Env", ""), ("Env", 0), ("Labels", []), ("OnBuild", {}),
                ("Healthcheck", {"Test": ["CMD", "bad"]}), ("OnBuild", ["RUN bad"]),
                ("Volumes", {"/witness": {}}), ("Labels", {"io.buildah.version": "test"}), ("ExposedPorts", {"80/tcp": {}})):
            row = metadata(); row["Config"][key] = value
            with self.subTest(key=key), self.assertRaises(c.Invalid): image_metadata(row, "x86_64", "target", [])
        row = metadata("oracle"); row["Config"]["Env"].append("LD_PRELOAD=/bad.so")
        with self.assertRaises(c.Invalid): image_metadata(row, "x86_64", "oracle", ["PATH=/usr/local/bin"])

    def test_invalid_build_scope_and_alias_refuse_before_output_creation(self):
        with tempfile.TemporaryDirectory() as folder:
            output = Path(folder) / "out"
            for arch, engine in (("amd64", "podman"), ("x32", "podman"), ("x86_64", "docker"), ("x86_64", "unknown")):
                with self.assertRaises(c.Invalid): prepare(arch, engine, output)
                self.assertFalse(output.exists())
        with self.assertRaises(c.Invalid): prepare("x86_64", "podman", Path(__file__).resolve().parent / "nested-build")


class ImageContentTests(unittest.TestCase):
    def test_relocatable_base_objects_are_recorded_but_cannot_supply_libraries(self):
        raw = bytearray(128)
        struct.pack_into("<16sHHIQQQIHHHHHH", raw, 0, b"\x7fELF\x02\x01\x01" + bytes(9), 1, 62, 1, 0, 0, 64, 0, 64, 0, 0, 64, 1, 0)
        self.assertEqual(elf_record(bytes(raw), "x86_64")["type"], 1)
        with self.assertRaises(c.Invalid): elf_record(bytes(raw), "aarch64")
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"
            tar(path, [("target", "file", bytes(raw))])
            with self.assertRaises(c.Invalid): verify_content(ImageTree(path), "x86_64", "target", {"target": bytes(raw)}, {})
            rows = [(name, "directory", "") for name in ("usr", "usr/local", "usr/local/lib")]
            rows += [("app", "file", elf(needed=("libfake.so",))), ("usr/local/lib/libfake.so", "file", bytes(raw))]
            tar(path, rows)
            with self.assertRaises(c.Invalid): elf_inventory(ImageTree(path), "x86_64")

    def test_native_payload_requires_exact_built_elf_for_both_architectures(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"
            for arch in ("aarch64", "x86_64"):
                raw = elf(arch); tar(path, [("target", "file", raw)])
                result = verify_content(ImageTree(path), arch, "target", {"target": raw}, {})
                self.assertEqual(result["declared_files"], {"/target": c.sha(raw)})
                self.assertFalse(result["runtime_acceptance"])
                with self.assertRaises(c.Invalid): verify_content(ImageTree(path), arch, "target", {"target": raw + b"changed"}, {})
                with self.assertRaises(c.Invalid): verify_content(ImageTree(path), "x86_64" if arch == "aarch64" else "aarch64", "target", {"target": raw}, {})
                tar(path, [("target", "file", raw), ("unexpected-config", "file", b"unreviewed")])
                with self.assertRaises(c.Invalid): verify_content(ImageTree(path), arch, "target", {"target": raw}, {})

    def test_oracle_sources_interpreter_links_and_foreign_module_refuse(self):
        sources = {name: (name + " inert source").encode() for name in (*SOURCE_FILES, "build_oracle.py")}
        rows = [(name, "directory", "") for name in ("oracle", "usr", "usr/local", "usr/local/bin", "usr/local/lib")]
        rows += [("oracle/" + name, "file", raw) for name, raw in sources.items()]
        rows += [("usr/local/bin/python3.13", "file", elf()), ("usr/local/bin/python3", "link", "python3.13"),
            ("usr/local/lib/fixture.so", "file", elf())]
        artifacts = {"oracle_elf": c.sha(elf()), "extensions": {"/usr/local/lib/fixture.so": c.sha(elf())}}
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"; tar(path, rows)
            result = verify_content(ImageTree(path), "x86_64", "oracle", artifacts, sources)
            self.assertIn("/usr/local/bin/python3.13", result["declared_files"])
            for extra in (("oracle/extra.py", "file", b"extra"), ("foreign.so", "file", elf("aarch64"))):
                tar(path, rows + [extra])
                with self.assertRaises(c.Invalid): verify_content(ImageTree(path), "x86_64", "oracle", artifacts, sources)
            tar(path, rows)
            wrong = sources | {"worker.py": b"changed"}
            with self.assertRaises(c.Invalid): verify_content(ImageTree(path), "x86_64", "oracle", artifacts, wrong)
            for extensions in ({}, {"/usr/local/lib/fixture.so": c.sha(b"wrong")}, {"/missing.so": c.sha(elf())}):
                with self.assertRaises(c.Invalid): verify_content(ImageTree(path), "x86_64", "oracle", artifacts | {"extensions": extensions}, sources)

    def test_optional_unresolved_extension_is_recorded_but_required_closure_refuses(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"
            tar(path, [("app", "file", elf()), ("optional.so", "file", elf(needed=("libmissing.so",)))])
            rows = elf_inventory(ImageTree(path), "x86_64", ["/app"])
            self.assertEqual(rows["/optional.so"]["unresolved_dependencies"], ["libmissing.so"])
            self.assertFalse(rows["/optional.so"]["required_dependency_closure"])
            with self.assertRaises(c.Invalid): elf_inventory(ImageTree(path), "x86_64", ["/app", "/optional.so"])

    def test_dynamic_library_and_loader_closure_and_missing_edges(self):
        rows = [(name, "directory", "") for name in ("usr", "usr/lib", "usr/lib/x86_64-linux-gnu")]
        rows += [("lib", "link", "usr/lib"), ("lib64", "link", "usr/lib/x86_64-linux-gnu"),
            ("app", "file", elf(needed=("libfixture.so.1",), interpreter="/lib64/loader.so")),
            ("usr/lib/x86_64-linux-gnu/libfixture.so.1", "file", elf()), ("usr/lib/x86_64-linux-gnu/loader.so", "file", elf())]
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"; tar(path, rows)
            found = elf_inventory(ImageTree(path), "x86_64")["/app"]["dependencies"]
            self.assertEqual(found, {"libfixture.so.1": "/usr/lib/x86_64-linux-gnu/libfixture.so.1", "PT_INTERP": "/usr/lib/x86_64-linux-gnu/loader.so"})
            for removed in (-1, -2):
                changed = list(rows); changed.pop(removed); tar(path, changed)
                with self.assertRaises(c.Invalid): elf_inventory(ImageTree(path), "x86_64")

    def test_archive_traversal_duplicates_special_files_and_whiteouts_refuse(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"
            for rows in ([("../escape", "file", b"x")], [("/absolute", "file", b"x")],
                    [("same", "file", b"x"), ("./same", "file", b"y")], [(".wh.hidden", "file", b"x")],
                    [("fifo", "fifo", "")], [("device", "device", "")], [("escape", "link", "../../host")]):
                tar(path, rows)
                with self.subTest(rows=rows), self.assertRaises(c.Invalid): ImageTree(path)
            self.assertFalse((Path(folder) / "escape").exists())

    def test_link_cycles_parent_files_permissions_and_writable_witness_refuse(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"
            for rows, query in (([("a", "link", "b"), ("b", "link", "a")], "/a"),
                    ([("a", "file", b"x"), ("a/b", "file", b"y")], "/a/b"),
                    ([("a", "hardlink", "missing")], "/a")):
                tar(path, rows)
                with self.assertRaises(c.Invalid): ImageTree(path).file(query)
            tar(path, [("target", "file", elf())]); tree = ImageTree(path)
            for key, value in (("uid", 999), ("gid", 999), ("mode", 0o777), ("mode", 0o4755)):
                changed = copy.deepcopy(tree); changed.entries["/target"][key] = value
                with self.assertRaises(c.Invalid): changed.file("/target", executable=True)

    def test_malformed_dynamic_tables_and_foreign_elf_headers_refuse(self):
        raw = elf(needed=("libfixture.so",))
        for offset, value in ((4, 1), (5, 2), (18, 183), (20, 0)):
            changed = bytearray(raw); changed[offset] = value
            with self.assertRaises(c.Invalid): elf_record(bytes(changed), "x86_64")
        for offset, value in ((0x308, 0xffff), (0x318, 999999), (0x328, 0xffff)):
            changed = bytearray(raw); struct.pack_into("<Q", changed, offset, value)
            with self.assertRaises(c.Invalid): elf_record(bytes(changed), "x86_64")


class PortableArchiveTests(unittest.TestCase):
    def test_config_id_platform_and_layer_content_are_joined(self):
        layer = b"inert layer bytes"
        config = c.canonical({"architecture": "amd64", "os": "linux", "rootfs": {"diff_ids": ["sha256:" + c.sha(layer)]}})
        manifest = json.dumps([{"Config": "config.json", "Layers": ["layer.tar"]}]).encode()
        rows = [("manifest.json", "file", manifest), ("config.json", "file", config), ("layer.tar", "file", layer)]
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "image.tar"; tar(path, rows)
            result = archive_record(path, "sha256:" + c.sha(config), "x86_64")
            self.assertEqual(result["layers"], ["sha256:" + c.sha(layer)])
            tar(path, rows + [("legacy/layer.tar", "link", "../layer.tar")])
            self.assertEqual(archive_record(path, "sha256:" + c.sha(config), "x86_64")["layers"], result["layers"])
            for target in ("../../outside", "/layer.tar", "../config.json", "missing"):
                tar(path, rows + [("legacy/layer.tar", "link", target)])
                with self.assertRaises(c.Invalid): archive_record(path, "sha256:" + c.sha(config), "x86_64")
            tar(path, rows)
            for identity, arch in ((ID, "x86_64"), ("sha256:" + c.sha(config), "aarch64")):
                with self.assertRaises(c.Invalid): archive_record(path, identity, arch)
            tar(path, rows[:-1] + [("layer.tar", "file", b"tampered")])
            with self.assertRaises(c.Invalid): archive_record(path, "sha256:" + c.sha(config), "x86_64")


class StoppedImageLifecycleTests(unittest.TestCase):
    def test_build_flags_preserve_empty_native_environment_without_cached_metadata(self):
        with tempfile.TemporaryDirectory() as folder:
            for engine in ("podman",):
                for role in ("target", "observer", "oracle"):
                    builder = object.__new__(Builder); builder.engine, builder.output = engine, Path(folder)
                    calls = []
                    def run(args, **kwargs):
                        calls.append(args); Path(args[args.index("--iidfile") + 1]).write_text(ID)
                        return 0, b""
                    builder.run = run
                    self.assertEqual(builder.build(Path(folder), "x86_64", "Dockerfile.oracle" if role == "oracle" else "Dockerfile", role, engine + role), ID)
                    self.assertIn("--no-cache", calls[0])
                    self.assertEqual("--unsetenv" in calls[0], engine == "podman" and role != "oracle")
                    if engine == "podman": self.assertIn("--identity-label=false", calls[0])

    def builder(self, folder, failure=None):
        builder = object.__new__(Builder)
        builder.engine, builder.output, builder.commands, builder.cleanup = "podman", Path(folder), [], []
        builder.prefix = ["/fake/podman", "--remote=false"]
        state = {"Id": "b" * 64, "Image": ID, "State": {"Running": False, "Pid": 0, "Status": "created", "StartedAt": "0001-01-01T00:00:00Z"}}
        calls = []
        def run(args, **kwargs):
            calls.append(args)
            if args[0] == "create":
                state["Name"] = args[args.index("--name") + 1]
                if failure == "ambiguous": raise c.Invalid("CREATE_FAILED")
                return 0, ("b" * 64 + "\n").encode()
            if args[0] == "export" and failure == "export": raise c.Invalid("EXPORT_FAILED")
            if args[0] == "unshare": return 0, b'{"uid":0,"gid":0,"mode":365}'
            if args[0] == "rm": state["removed"] = True
            if args[0] == "ps": return 0, b"" if state.get("removed") else ("b" * 64 + "\n").encode()
            return 0, b""
        builder.run = run
        builder.inspect = lambda *_: copy.deepcopy(state)
        return builder, state, calls

    def test_never_started_export_and_verified_cleanup(self):
        with tempfile.TemporaryDirectory() as folder:
            builder, state, calls = self.builder(folder)
            result = builder.export(ID, Path(folder) / "root.tar")
            self.assertFalse(result["running"])
            self.assertTrue(state["removed"] and builder.cleanup[0]["removed"])
            self.assertFalse(any(row[0] in ("run", "start", "exec", "kill") for row in calls))
            self.assertIn("/__obs1_inspection_only__", calls[0])

    def test_ambiguous_create_and_export_failures_still_cleanup_owned_stopped_object(self):
        for fault in ("ambiguous", "export"):
            with tempfile.TemporaryDirectory() as folder:
                builder, state, _ = self.builder(folder, fault)
                with self.assertRaises(c.Invalid): builder.export(ID, Path(folder) / "root.tar")
                self.assertTrue(state["removed"] and builder.cleanup[0]["removed"])

    def test_started_or_foreign_container_refuses_export_and_removal(self):
        for fault in ("running", "foreign", "started"):
            with tempfile.TemporaryDirectory() as folder:
                builder, state, calls = self.builder(folder)
                if fault == "running": state["State"].update(Running=True, Pid=42)
                elif fault == "foreign": state["Image"] = "sha256:" + "c" * 64
                else: state["State"]["StartedAt"] = "2026-10-17T00:00:00Z"
                with self.assertRaises(c.Invalid): builder.export(ID, Path(folder) / "root.tar")
                self.assertFalse(any(row[0] in ("export", "rm", "kill") for row in calls))


if __name__ == "__main__":
    unittest.main()
