"""Regressions for the independent image-set reviews; inert data and fake engines."""
import copy
import io
from importlib.machinery import ExtensionFileLoader
import json
from pathlib import Path
import struct
import sys
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import build_oracle
import contracts as c
from image_artifacts import ImageTree, archive_record, elf_inventory, verify_content
from image_build import Builder, prepare
from test_architecture_contracts import header
import test_image_artifacts as image_tests
from test_image_artifacts import ID, elf, metadata, tar


def searched_elf(*, rpath=None, runpath=None, needed="libc0fixtureonly.so"):
    raw = bytearray(elf(needed=(needed,)))
    strings = bytearray(b"\0" + needed.encode() + b"\0")
    extra = []
    for tag, value in ((15, rpath), (29, runpath)):
        if value is not None:
            extra.append((tag, len(strings))); strings.extend(value.encode() + b"\0")
    raw[0x500:0x500 + len(strings)] = strings
    tags = [(5, 0x500), (10, len(strings)), (1, 1), *extra, (0, 0)]
    for index, row in enumerate(tags): struct.pack_into("<QQ", raw, 0x300 + 16 * index, *row)
    struct.pack_into("<QQ", raw, 120 + 32, len(tags) * 16, len(tags) * 16)
    return bytes(raw)


class ImageReviewTests(unittest.TestCase):
    def test_flat_archive_entries_require_literal_directory_ancestors(self):
        rows = [("target", "file", elf()), ("etc", "directory", ""), ("etc/hosts", "file", b"inert generated file")]
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"
            for kind, value in (("hardlink", "target"), ("file", b"shadowed"), ("directory", ""), ("link", "../target")):
                tar(path, rows + [("alias-parent", "link", "etc"), ("alias-parent/hosts", kind, value)])
                with self.subTest(kind=kind), self.assertRaisesRegex(c.Invalid, "IMAGE_MEMBER_PARENT"): ImageTree(path)
            for parent_first in (False, True):
                parent, alias = ("literal", "directory", ""), ("literal/alias", "hardlink", "target")
                tar(path, rows + ([parent, alias] if parent_first else [alias, parent]))
                verify_content(ImageTree(path), "x86_64", "target", {"target": elf()}, {})
            tar(path, rows + [("missing/alias", "hardlink", "target")])
            with self.assertRaisesRegex(c.Invalid, "IMAGE_MEMBER_PARENT"): ImageTree(path)

    def test_unused_hardlinks_are_checked_after_the_complete_archive_map(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"
            for alias_first in (False, True):
                for mode, uid, gid in ((0o755, 0, 0), (0o700, 0, 0), (0o755, 1, 0), (0o755, 0, 1)):
                    with tarfile.open(path, "w") as archive:
                        root = tarfile.TarInfo("."); root.type = tarfile.DIRTYPE; root.mode = 0o755; archive.addfile(root)
                        target = tarfile.TarInfo("target"); target.mode = 0o755; target.size = len(elf())
                        alias = tarfile.TarInfo("unused-alias"); alias.type = tarfile.LNKTYPE; alias.linkname = "target"
                        alias.mode, alias.uid, alias.gid = mode, uid, gid
                        for member in (alias, target) if alias_first else (target, alias):
                            archive.addfile(member, io.BytesIO(elf()) if member is target else None)
                    if (mode, uid, gid) == (0o755, 0, 0):
                        tree = ImageTree(path)
                        verify_content(tree, "x86_64", "target", {"target": elf()}, {})
                    else:
                        with self.subTest(alias_first=alias_first, mode=mode, uid=uid, gid=gid), self.assertRaisesRegex(c.Invalid, "IMAGE_HARDLINK_METADATA"):
                            ImageTree(path)

    def test_hardlink_targets_cannot_traverse_symlink_inodes_or_prefixes(self):
        rows = [(name, "directory", "") for name in ("src", "src/deep", "oracle")]
        rows += [("src/worker.py", "file", b"expected"), ("worker.py", "file", b"different"),
            ("src/deep/link", "link", "../worker.py"), ("oracle/worker.py", "hardlink", "src/deep/link")]
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"; tar(path, rows)
            with self.assertRaisesRegex(c.Invalid, "IMAGE_HARDLINK_LINK_TARGET"): ImageTree(path).file("/oracle/worker.py")
            tar(path, rows[:-1] + [("oracle/worker.py", "hardlink", "src/worker.py")])
            self.assertEqual(ImageTree(path).file("/oracle/worker.py"), b"expected")
            tree = ImageTree(path); tree.entries["/oracle/worker.py"]["mode"] = 0o700
            with self.assertRaisesRegex(c.Invalid, "IMAGE_HARDLINK_METADATA"): tree.file("/oracle/worker.py")
            tar(path, rows[:-1] + [("oracle/worker.py", "link", "../src/deep/link")])
            self.assertEqual(ImageTree(path).file("/oracle/worker.py"), b"expected")
            tar(path, rows[:-1] + [("alias", "link", "src"), ("oracle/worker.py", "hardlink", "alias/worker.py")])
            with self.assertRaisesRegex(c.Invalid, "IMAGE_HARDLINK_LINK_TARGET"): ImageTree(path).file("/oracle/worker.py")

    def test_normalized_root_aliases_share_observation_and_duplicate_checks(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"
            for alias in ("./.", ".//.", "././"):
                for mode, duplicate in ((0o755, False), (0o700, False), (0o700, True)):
                    with tarfile.open(path, "w") as archive:
                        if duplicate:
                            root = tarfile.TarInfo("."); root.type = tarfile.DIRTYPE; root.mode = 0o755; archive.addfile(root)
                        root = tarfile.TarInfo(alias); root.type = tarfile.DIRTYPE; root.mode = mode; archive.addfile(root)
                        target = tarfile.TarInfo("target"); target.mode = 0o755; target.size = len(elf()); archive.addfile(target, io.BytesIO(elf()))
                    observed = {"uid": 0, "gid": 0, "mode": 0o755}
                    if mode == 0o755 and not duplicate:
                        for root in (None, observed):
                            tree = ImageTree(path, root)
                            self.assertNotIn("/", tree.entries)
                            verify_content(tree, "x86_64", "target", {"target": elf()}, {})
                    else:
                        with self.subTest(alias=alias, mode=mode, duplicate=duplicate), self.assertRaises(c.Invalid): ImageTree(path, observed)

    def test_required_loader_execute_intent_survives_an_earlier_library_visit(self):
        rows = [(name, "directory", "") for name in ("usr", "usr/local", "usr/local/lib")]
        loader = "/usr/local/lib/loader.so"
        rows += [("app", "file", elf(interpreter=loader)), (loader.lstrip("/"), "file", elf())]
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"; tar(path, rows); tree = ImageTree(path)
            elf_inventory(tree, "x86_64", ["/app", loader])
            tree.entries[loader]["mode"] = 0o644
            with self.assertRaisesRegex(c.Invalid, "IMAGE_FILE_EXECUTABLE"): elf_inventory(tree, "x86_64", ["/app", loader])
            # Read-only ordinary libraries remain valid; only PT_INTERP needs execute.
            tar(path, rows[:-2] + [("app", "file", elf(needed=("loader.so",))), (loader.lstrip("/"), "file", elf())])
            tree = ImageTree(path); tree.entries[loader]["mode"] = 0o644
            elf_inventory(tree, "x86_64", ["/app"])
            from image_artifacts import elf_record
            with self.assertRaises(c.Invalid): elf_record(elf(needed=("PT_INTERP",)), "x86_64")

    def test_unsupported_docker_builder_refuses_before_mutating_outputs(self):
        with tempfile.TemporaryDirectory() as folder:
            out = Path(folder) / "out"
            with patch("image_build.subprocess.run") as run, self.assertRaises(c.Invalid): prepare("x86_64", "docker", out)
            run.assert_not_called(); self.assertFalse(out.exists())
            with self.assertRaises(c.Invalid): Builder("docker", out)
        # A real BuildKit-style PATH remains forbidden by native image admission.
        from image_artifacts import image_metadata
        row = metadata(); row["Config"]["Env"] = ["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"]
        with self.assertRaises(c.Invalid): image_metadata(row, "x86_64", "target", [])

    def test_oracle_extension_abi_isolated_from_host_and_refused_before_output(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            executable, extension = root / "python", root / "fixture.so"
            module = SimpleNamespace(__spec__=SimpleNamespace(loader=ExtensionFileLoader("fixture", str(extension)), origin=str(extension)))
            collect = build_oracle.extension_inventory
            for architecture, foreign in (("aarch64", "x86_64"), ("x86_64", "aarch64")):
                executable.write_bytes(header(architecture)); extension.write_bytes(header(foreign))
                destination = root / architecture
                with patch.object(sys, "argv", ["build_oracle.py", str(destination), architecture]), \
                        patch.object(sys, "executable", str(executable)), \
                        patch.object(build_oracle.os, "uname", return_value=SimpleNamespace(sysname="Linux", machine=architecture)), \
                        patch.object(build_oracle, "extension_inventory", side_effect=lambda arch: collect(arch, [module])), self.assertRaises(c.Invalid):
                    build_oracle.main()
                self.assertFalse(destination.exists())
                extension.write_bytes(header(architecture))
                self.assertEqual(collect(architecture, [module]), {str(extension): c.sha(header(architecture))})

    def test_portable_config_and_layer_alias_collisions_are_refused(self):
        layer = b"inert layer"
        config = c.canonical({"os": "linux", "architecture": "amd64", "rootfs": {"diff_ids": ["sha256:" + c.sha(layer)]}})
        manifest = json.dumps([{"Config": "config.json", "Layers": ["payload/layer.tar"]}]).encode()
        rows = [("manifest.json", "file", manifest), ("config.json", "file", config), ("payload/layer.tar", "file", layer)]
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "image.tar"
            for alias in ("./config.json", "payload//layer.tar", "payload/./layer.tar"):
                tar(path, rows + [(alias, "file", b"substituted")])
                with self.subTest(alias=alias), self.assertRaises(c.Invalid): archive_record(path, "sha256:" + c.sha(config), "x86_64")
            tar(path, rows + [("legacy/layer.tar", "link", "../payload/layer.tar")])
            self.assertEqual(archive_record(path, "sha256:" + c.sha(config), "x86_64")["config_id"], "sha256:" + c.sha(config))

    def test_link_parent_components_follow_filesystem_order(self):
        rows = [(name, "directory", "") for name in ("oracle", "foreign", "foreign/subdir")]
        rows += [("oracle/worker.py", "link", "../jump/../worker.py"), ("jump", "link", "foreign/subdir"),
            ("worker.py", "file", b"expected inert source"), ("foreign/worker.py", "file", b"different inert source")]
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"; tar(path, rows); tree = ImageTree(path)
            self.assertEqual(tree.resolve("/oracle/worker.py"), "/foreign/worker.py")
            self.assertEqual(tree.file("/oracle/worker.py"), b"different inert source")
            self.assertNotEqual(tree.file("/oracle/worker.py"), tree.file("/worker.py"))

    def test_root_markers_are_counted_and_cannot_repeat(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"
            tar(path, [("target", "file", elf())]); self.assertEqual(ImageTree(path).member_count, 2)
            tar(path, [(".", "directory", ""), ("target", "file", elf())])
            with self.assertRaises(c.Invalid): ImageTree(path)
            tar(path, [("d" + str(index), "directory", "") for index in range(20000)])
            with self.assertRaises(c.Invalid): ImageTree(path)

    def test_configured_user_requires_read_and_execute_bits_on_native_files(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"
            for role in ("target", "observer"):
                tar(path, [(role, "file", elf())]); tree = ImageTree(path)
                for mode in (0o700, 0o750, 0o111, 0o644):
                    tree.entries["/" + role]["mode"] = mode
                    with self.subTest(role=role, mode=mode), self.assertRaises(c.Invalid):
                        verify_content(tree, "x86_64", role, {role: elf()}, {})
                tree.entries["/" + role]["mode"] = 0o555
                verify_content(tree, "x86_64", role, {role: elf()}, {})

    def test_root_and_alias_directory_access_and_source_read_permissions(self):
        rows = [("oracle", "directory", ""), ("real", "directory", ""), ("real/subdir", "directory", ""),
            ("alias", "link", "real/subdir"), ("real/source.py", "file", b"inert"), ("oracle/source.py", "link", "../alias/../source.py")]
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"; tar(path, rows); tree = ImageTree(path)
            self.assertEqual(tree.file("/oracle/source.py"), b"inert")
            for location in ("/", "/oracle", "/real/subdir", "/real/source.py"):
                changed = copy.deepcopy(tree)
                (changed.root if location == "/" else changed.entries[location])["mode"] = 0o600
                with self.subTest(location=location), self.assertRaises(c.Invalid): changed.file("/oracle/source.py")
            changed = copy.deepcopy(tree); changed.root = None
            with self.assertRaises(c.Invalid): changed.file("/oracle/source.py")

    def test_root_observation_must_agree_with_archive_root_marker(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"; tar(path, [("target", "file", elf())])
            with self.assertRaises(c.Invalid): ImageTree(path, {"uid": 0, "gid": 0, "mode": 0o700})
            with self.assertRaises(c.Invalid): ImageTree(path, {"uid": False, "gid": 0, "mode": 0o755})

    def test_runpath_overrides_rpath_and_legacy_inheritance_refuses(self):
        rows = [("old", "directory", ""), ("new", "directory", ""), ("old/libc0fixtureonly.so", "file", elf())]
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"
            tar(path, rows + [("app", "file", searched_elf(rpath="/old", runpath="/new"))])
            with self.assertRaises(c.Invalid): elf_inventory(ImageTree(path), "x86_64", ["/app"])
            tar(path, rows + [("app", "file", searched_elf(rpath="/old"))])
            with self.assertRaisesRegex(c.Invalid, "IMAGE_UNSUPPORTED_RPATH"): elf_inventory(ImageTree(path), "x86_64", ["/app"])
            tar(path, rows + [("new/libc0fixtureonly.so", "file", elf()), ("app", "file", searched_elf(rpath="/old", runpath="/new"))])
            result = elf_inventory(ImageTree(path), "x86_64", ["/app"])
            self.assertEqual(result["/app"]["dependencies"]["libc0fixtureonly.so"], "/new/libc0fixtureonly.so")

    def test_runpath_directory_links_resolve_and_malformed_links_do_not_fall_through(self):
        rows = [("real", "directory", ""), ("real/libc0fixtureonly.so", "file", elf()), ("app", "file", searched_elf(runpath="/alias"))]
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "root.tar"; tar(path, rows + [("alias", "link", "real")])
            result = elf_inventory(ImageTree(path), "x86_64", ["/app"])
            self.assertEqual(result["/app"]["dependencies"]["libc0fixtureonly.so"], "/real/libc0fixtureonly.so")
            for target in ("missing", "alias"):
                tar(path, rows + [("alias", "link", target)])
                with self.assertRaises(c.Invalid): elf_inventory(ImageTree(path), "x86_64", ["/app"])

    def test_root_observation_drift_and_bad_started_fields_refuse(self):
        helper = image_tests.StoppedImageLifecycleTests()
        with tempfile.TemporaryDirectory() as folder:
            builder, state, _ = helper.builder(folder)
            with patch.object(builder, "root_metadata", side_effect=[{"uid": 0, "gid": 0, "mode": 0o555}, {"uid": 0, "gid": 0, "mode": 0o700}]), self.assertRaises(c.Invalid):
                builder.export(ID, Path(folder) / "root.tar")
            self.assertTrue(state["removed"])
        for key, value in (("Pid", False), ("StartedAt", None), ("StartedAt", "")):
            with tempfile.TemporaryDirectory() as folder:
                builder, state, calls = helper.builder(folder); state["State"][key] = value
                with self.assertRaises(c.Invalid): builder.export(ID, Path(folder) / "root.tar")
                self.assertFalse(any(row[0] == "rm" for row in calls))


if __name__ == "__main__":
    unittest.main()
