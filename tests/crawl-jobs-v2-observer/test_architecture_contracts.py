"""Architecture joins use synthetic metadata/private ELF headers, never native execution."""
import copy
import json
import os
from pathlib import Path
import struct
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import build_oracle
import build_tools
import contracts as c
import context_provision as provision
import context_transport as transport
import docker_backend as db
from linux_observation import LinuxReader
import owned_resources as owned
import profiles
import runtime_admission as admission
import runtime_context as context
from streams import Deadline, ProcessStream, WireBudget
import test_context_provisioning as provisioning
import test_runtime_admission as native
from test_owned_resources import DAEMON, fixture_scope, prepared
from test_runtime_context import KEY, acquire, fixture
from test_streams import binding as stream_binding


def header(architecture):
    raw = bytearray(64)
    raw[:7] = b"\x7fELF\x02\x01\x01"
    struct.pack_into("<HHI", raw, 16, 3, c.platform(architecture)["elf_machine"], 1)
    return bytes(raw)


class ArchitectureContractTests(unittest.TestCase):
    def test_canonical_platforms_bind_linux_oci_elf_and_seccomp_names(self):
        for name, image, machine, seccomp, audit in (("aarch64", "arm64", 183, "SCMP_ARCH_AARCH64", 0xc00000b7),
                ("x86_64", "amd64", 62, "SCMP_ARCH_X86_64", 0xc000003e)):
            value = c.platform(name)
            self.assertEqual((value["os"], value["architecture"], value["image_architecture"], value["oci_platform"]),
                ("linux", name, image, "linux/" + image))
            self.assertEqual((value["elf_class"], value["elf_data"], value["elf_machine"], value["seccomp_architecture"], value["audit_architecture"]),
                (2, 1, machine, seccomp, audit))
            self.assertEqual(c.platform_digest(name), c.sha(c.canonical(value)))
            value["elf_machine"] = 0
            self.assertEqual(c.platform(name)["elf_machine"], machine)
        self.assertNotEqual(c.platform_digest("aarch64"), c.platform_digest("x86_64"))

    def test_aliases_unknown_platforms_and_coercions_are_rejected(self):
        for value in ("arm64", "amd64", "x64", "ARM64", "linux/arm64", "aarch64_be", "riscv64", "", None, True, 183, [], {}):
            with self.subTest(value=value), self.assertRaises(c.Invalid): c.platform(value)

    def test_both_preparation_contracts_are_explicit_and_non_authorizing(self):
        artifacts = native.preparation()["artifacts"]
        for name in ("aarch64", "x86_64"):
            value = c.preparation(artifacts, name)
            self.assertEqual(c.validate_preparation(c.decode(c.canonical(value))), value)
            self.assertEqual(value["version"], 2)
            self.assertEqual(value["platform_sha256"], c.platform_digest(name))
            self.assertFalse(value["execution_authorized"] or value["m4_accepted"])
        with self.assertRaisesRegex(c.Invalid, "ARCHITECTURE_NOT_IMPLEMENTED"):
            c.runtime_architecture("x86_64")

    def test_preparation_relabeling_and_legacy_packets_refuse(self):
        value = native.preparation()
        for update in ({"architecture": "x86_64"}, {"platform_sha256": c.platform_digest("x86_64")},
                {"version": 1}, {"version": True}, {"architecture": "arm64"}):
            with self.subTest(update=update), self.assertRaises(c.Invalid): c.validate_preparation(value | update)
        for key in ("architecture", "platform_sha256"):
            changed = copy.deepcopy(value); changed.pop(key)
            with self.assertRaises(c.Invalid): c.validate_preparation(changed)

    def test_elf_headers_are_classified_from_bytes_not_requested_labels(self):
        for name in ("aarch64", "x86_64"):
            self.assertEqual(c.elf_architecture(header(name)), name)
        with self.assertRaisesRegex(c.Invalid, "ELF_ARCHITECTURE"):
            build_tools.parse_elf(header("x86_64"), "aarch64")
        with self.assertRaises(c.Invalid): build_tools.parse_elf(header("aarch64"))  # Header alone is not an ELF artifact.
        with self.assertRaisesRegex(c.Invalid, "ARCHITECTURE_NOT_IMPLEMENTED"):
            build_tools.parse_elf(header("x86_64"), "x86_64")

    def test_elf_class_endianness_machine_version_and_short_headers_refuse(self):
        original = header("aarch64")
        for offset, value in ((0, 0), (4, 1), (5, 2), (6, 0), (16, 0), (18, 243), (20, 0)):
            raw = bytearray(original); raw[offset] = value
            with self.subTest(offset=offset), self.assertRaises(c.Invalid): c.elf_architecture(bytes(raw))
        for raw in (original[:63], original.decode("latin1"), bytearray(original)):
            with self.assertRaises(c.Invalid): c.elf_architecture(raw)

    def test_existing_arm64_profiles_and_compiled_filter_bytes_are_preserved(self):
        expected = {"target": "6a1ffdf7326ae1370bd55cc2edb2bcfb49b2d65ce7090205576d3b6e9496ab3e",
            "observer": "abf581851c19631ba5125ac03443f4ad72c6e8fd7b92fa086218060e07b56bf2",
            "oracle": "f82e0ae5335e1393c402265c4a14a4e56ad8df27ab48eee70907745fef3b6835"}
        for role, digest in expected.items():
            self.assertEqual(c.sha(c.canonical(profiles.profile(role, "aarch64"))), digest)
            with self.assertRaisesRegex(c.Invalid, "ARCHITECTURE_NOT_IMPLEMENTED"): profiles.profile(role, "x86_64")
        raw = b"".join(struct.pack("<HBBI", *row) for row in profiles.target_filter_program("aarch64"))
        self.assertEqual(c.sha(raw), "701824ff5f8ccafa359f711d2714b23c8a303789cd1987a3a85aba19a3667b93")
        with self.assertRaisesRegex(c.Invalid, "ARCHITECTURE_NOT_IMPLEMENTED"): profiles.target_filter_program("x86_64")

    def test_build_entrypoints_refuse_x64_before_writing_outputs(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            with patch.object(sys, "argv", ["build_tools.py", "headers", str(root), "x86_64"]), self.assertRaises(c.Invalid):
                build_tools.main()
            self.assertEqual(list(root.iterdir()), [])
            with patch.object(sys, "argv", ["build_oracle.py", str(root / "oracle"), "x86_64"]), \
                    patch.object(build_oracle.os, "uname") as uname, self.assertRaises(c.Invalid):
                build_oracle.main()
            uname.assert_not_called()
            self.assertFalse((root / "oracle").exists())

    def test_resource_scope_binds_architecture_and_refuses_legacy_or_relabeling(self):
        value = fixture_scope()
        self.assertEqual(value["architecture"], "aarch64")
        self.assertEqual(value["platform_sha256"], c.platform_digest("aarch64"))
        self.assertEqual(owned.container_spec(value, "target")["architecture"], value["architecture"])
        for update in ({"version": 1}, {"platform_sha256": c.platform_digest("x86_64")},
                {"architecture": "x86_64", "platform_sha256": c.platform_digest("x86_64")}):
            with self.assertRaises(c.Invalid): owned.validate_scope(value | update)
        changed = copy.deepcopy(value); changed.pop("architecture")
        with self.assertRaises(c.Invalid): owned.validate_scope(changed)

    def test_mislabeled_oci_profile_cannot_be_admitted(self):
        registry, fake, _, _ = prepared()
        volumes = {role: registry.entries[role]["reference"] for role in owned.VOLUMES}
        for role in owned.ACTORS:
            expected = registry.spec(role)
            row = fake.inspect("container", expected["name"], Deadline(1))
            policy = profiles.profile(role); policy["architectures"] = ["SCMP_ARCH_X86_64"]
            row["HostConfig"]["SecurityOpt"][1] = "seccomp=" + json.dumps(policy)
            with self.assertRaisesRegex(c.Invalid, "PROFILE_CONTENT"): owned.admit_container(row, expected, volumes)


class ArchitectureBoundaryTests(unittest.TestCase):
    def test_deployment_and_anchor_architecture_must_agree(self):
        value = provisioning.deployment()
        for location in ("deployment", "anchor"):
            changed = copy.deepcopy(value)
            (changed if location == "deployment" else changed["anchor"])["architecture"] = "x86_64"
            with self.assertRaisesRegex(c.Invalid, "CONTEXT_ARCHITECTURE"): provision.validate_deployment(changed)
        legacy = copy.deepcopy(value); legacy["version"] = 1
        with self.assertRaises(c.Invalid): provision.validate_deployment(legacy)
        legacy = copy.deepcopy(value["anchor"]); legacy.pop("architecture")
        with self.assertRaises(c.Invalid): context.validate_anchor(legacy)

    def test_known_x64_deployment_is_metadata_only_until_the_port(self):
        value = provisioning.deployment()
        value["architecture"] = value["anchor"]["architecture"] = "x86_64"
        self.assertEqual(provision.validate_deployment(value), value)
        with tempfile.TemporaryDirectory() as folder:
            output = Path(folder) / "bundle"
            with self.assertRaisesRegex(c.Invalid, "ARCHITECTURE_NOT_IMPLEMENTED"):
                provision.create_bundle(output, value, provisioning.identity_bytes(), key=KEY)
            self.assertFalse(output.exists())
        with self.assertRaises(c.Invalid): context.LinuxContextEndpoint(value["anchor"], KEY, fixture()[0])

    def test_direct_ssh_transport_refuses_x64_before_client_or_process_io(self):
        value = provisioning.deployment()
        value["architecture"] = value["anchor"]["architecture"] = "x86_64"
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            for name, raw in (("deployment.json", c.canonical(value)), ("context-source.key", KEY),
                    ("id_ed25519", provisioning.identity_bytes())):
                provision.write_new(root / name, raw)
            channel = transport.SSHContextTransport(root)
            with patch.object(transport, "verify_client") as client, \
                    patch.object(transport, "exchange_process", return_value=b"reply\n") as exchange:
                with self.assertRaises(c.Invalid): channel(b"probe\n", Deadline(2))
                client.assert_not_called(); exchange.assert_not_called()

    def test_bundle_manifest_and_host_configuration_bind_the_architecture(self):
        with tempfile.TemporaryDirectory() as folder:
            output, value = Path(folder) / "bundle", provisioning.deployment()
            record = provision.create_bundle(output, value, provisioning.identity_bytes(), key=KEY)
            self.assertEqual(record["version"], 2)
            self.assertEqual(record["architecture"], "aarch64")
            self.assertEqual(record["platform_sha256"], c.platform_digest("aarch64"))
            for name in ("context-source.json", "context-service.json"):
                self.assertEqual(c.decode((output / "host/etc" / name).read_bytes())["architecture"], "aarch64")
            record["architecture"] = "x86_64"
            (output / "manifest.json").write_bytes(c.canonical(record))
            with self.assertRaises(c.Invalid): provision.verify_bundle(output)

    def test_mixed_preparation_refuses_before_source_authorization_and_io(self):
        for change in ("preparation", "anchor"):
            parts = fixture()
            if change == "preparation": parts[3].update(c.preparation(parts[3]["artifacts"], "x86_64"))
            else: parts[5].anchor["architecture"] = "x86_64"
            with patch.object(parts[5], "authorize", return_value=True) as authorize, \
                    patch.object(parts[5], "exchange") as exchange, self.assertRaises(c.Invalid):
                acquire(parts)
            authorize.assert_not_called(); exchange.assert_not_called()
            self.assertEqual(parts[7].frames, 0)
            self.assertEqual(parts[7].bytes, 0)

    def test_authenticated_wrong_architecture_context_response_refuses(self):
        parts = fixture(); original = parts[5].exchange
        def wrong_architecture(raw, deadline):
            frames = original(raw, deadline).splitlines(keepends=True)
            kernel = c.decode(frames[0]); kernel["payload"]["architecture"] = "x86_64"
            frames[0] = c.canonical(kernel)
            seal = c.decode(frames[-1]); seal["mac"] = context.mac(KEY, raw + b"".join(frames[:-1]))
            frames[-1] = c.canonical(seal)
            return b"".join(frames)
        parts[5].exchange = wrong_architecture
        with self.assertRaisesRegex(c.Invalid, "CONTEXT_ACQUISITION_REJECTED"): acquire(parts)

    def test_collector_observation_must_match_the_independent_anchor(self):
        parts = fixture(); parts[0].value["architecture"] = "x86_64"
        with self.assertRaises(c.Invalid): acquire(parts)

    def test_linux_reader_rejects_foreign_kernel_before_observations(self):
        with self.assertRaises(c.Invalid): LinuxReader("x86_64")
        reader = LinuxReader()
        for system, machine in (("Linux", "x86_64"), ("Darwin", "aarch64"), ("Linux", "arm64")):
            with patch("linux_observation.os.uname", return_value=SimpleNamespace(sysname=system, machine=machine)), \
                    patch.object(reader, "namespaces") as namespaces, self.assertRaises(c.Invalid):
                reader.kernel()
            namespaces.assert_not_called()

    def test_linux_executable_hash_also_checks_fragmented_elf_header(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "elf"
            real_open, real_read = os.open, os.read
            def open_executable(name, flags):
                self.assertEqual(name, "/proc/self/exe")
                return real_open(path, flags)
            for name in ("aarch64", "x86_64"):
                raw = header(name) + b"private-fixture-payload"
                path.write_bytes(raw)
                with patch("linux_observation.os.open", side_effect=open_executable), \
                        patch("linux_observation.os.read", side_effect=lambda fd, n: real_read(fd, min(n, 7))):
                    if name == "aarch64": self.assertEqual(LinuxReader().executable(), c.sha(raw))
                    else:
                        with self.assertRaisesRegex(c.Invalid, "LINUX_EXECUTABLE_ARCHITECTURE"): LinuxReader().executable()

    def test_native_preparation_context_binding_and_identity_must_agree(self):
        for changed in ("preparation", "context", "binding", "frame"):
            prep, ctx, bound, frames = native.preparation(), native.context(), native.binding(), native.frames()
            if changed == "preparation": prep = c.preparation(prep["artifacts"], "x86_64")
            elif changed == "context": ctx["architecture"] = "x86_64"
            elif changed == "binding": bound["architecture"] = "x86_64"
            else: frames = native.changed(frames, 0, architecture="x86_64")
            with self.subTest(changed=changed), self.assertRaises(c.Invalid): admission.validate(frames, bound, prep, ctx)

    def test_observer_and_oracle_cannot_borrow_foreign_architecture_peer(self):
        prep, ctx = native.preparation(), native.context()
        target = admission.validate(native.frames(), native.binding(), prep, ctx)
        for role in ("observer", "oracle"):
            admission.validate(native.frames(role), native.binding(role), prep, ctx, target=target)
            changed = copy.deepcopy(target); changed["binding"]["architecture"] = "x86_64"
            with self.assertRaises(c.Invalid): admission.validate(native.frames(role), native.binding(role), prep, ctx, target=changed)
            with self.assertRaises(c.Invalid): admission.validate(native.changed(native.frames(role), 0, architecture="x86_64"), native.binding(role), prep, ctx, target=target)

    def test_legacy_native_frames_and_contexts_do_not_get_an_inferred_architecture(self):
        for index, phase in enumerate(admission.PHASES):
            row = c.decode(native.frames()[index]); row["version"] = 1
            with self.assertRaises(c.Invalid): admission.native_frame(c.canonical(row), phase)
        row = c.decode(native.frames()[0]); row.pop("architecture")
        with self.assertRaises(c.Invalid): admission.native_frame(c.canonical(row), "IDENTITY")
        ctx = native.context(); ctx.pop("architecture")
        with self.assertRaises(c.Invalid): admission.validate_context(ctx)

    def test_admission_summary_cannot_promote_an_unsupported_architecture(self):
        value = admission.validate(native.frames(), native.binding(), native.preparation(), native.context())
        summary = admission.summary(value)
        self.assertEqual((summary["version"], summary["architecture"]), (2, "aarch64"))
        value["binding"]["architecture"] = "x86_64"
        value["frames"] = native.changed(value["frames"], 0, architecture="x86_64")
        value["identity_sha256"] = c.sha(value["frames"][0]); value["observation_sha256"] = c.sha(b"".join(value["frames"]))
        with self.assertRaises(c.Invalid): admission.summary(value)

    def test_x64_process_stream_refuses_before_spawning(self):
        bound = stream_binding() | {"architecture": "x86_64"}
        with patch("streams.subprocess.Popen") as spawn, self.assertRaisesRegex(c.Invalid, "ARCHITECTURE_NOT_IMPLEMENTED"):
            ProcessStream([sys.executable, "-c", "pass"], bound, WireBudget(), Deadline(1))
        spawn.assert_not_called()

    def test_backend_context_architecture_mismatch_prevents_callbacks(self):
        registry, _, backend, _, _ = native.DockerAdmissionTests().fixture()
        parts = fixture()
        for name in ("runtime", "provisioned"):
            prep = c.preparation(parts[3]["artifacts"], "x86_64")
            with patch.object(backend, "authorize", return_value=True) as authorize, patch.object(backend, "runner") as runner, self.assertRaises(c.Invalid):
                if name == "runtime": backend.acquire_runtime_context(parts[5], prep, WireBudget(), Deadline(1))
                else: backend.acquire_provisioned_context("/nonexistent/bundle", prep, WireBudget(), Deadline(1))
            authorize.assert_not_called(); runner.assert_not_called()

    def test_backend_platform_or_image_mismatch_prevents_create(self):
        value = fixture_scope()
        for foreign in ("daemon", "image"):
            calls = []
            def runner(argv, deadline):
                args = argv[3:]; calls.append(args)
                if args[0] == "info": return 0, DAEMON, b""
                if args[0] == "version": return 0, json.dumps({"Os": "linux", "Arch": "amd64" if foreign == "daemon" else "arm64"}).encode(), b""
                if args[:2] == ["image", "inspect"]: return 0, json.dumps([{"Id": value["images"]["target"], "Os": "linux", "Architecture": "amd64"}]).encode(), b""
                raise AssertionError("unexpected mutation")
            backend = db.DockerBackend(value, {role: "/unused" for role in owned.ACTORS}, authorize=lambda *_: True, runner=runner)
            with self.assertRaisesRegex(c.Invalid, "PLATFORM" if foreign == "daemon" else "IMAGE_IDENTITY"):
                backend.create_container(owned.container_spec(value, "target"), Deadline(1))
            self.assertFalse(any(args[:2] == ["container", "create"] for args in calls))

    def test_foreign_daemon_blocks_actor_start_and_mixed_context_blocks_oracle(self):
        registry, fake, _, _ = prepared()
        backend = db.DockerBackend(registry.scope, {role: "/unused" for role in owned.ACTORS}, authorize=lambda *_: True,
            runner=lambda argv, _: (0, DAEMON if argv[3] == "info" else b'{"Os":"linux","Arch":"amd64"}', b""))
        with patch.object(backend, "stream_factory") as spawn, self.assertRaisesRegex(c.Invalid, "PLATFORM"):
            backend.open_actor(registry.entries["target"]["reference"], registry.spec("target"), registry.snapshot(), WireBudget(), Deadline(1))
        spawn.assert_not_called()
        with patch.object(backend, "open_actor") as start, self.assertRaisesRegex(c.Invalid, "ADMISSION_ARCHITECTURE"):
            backend.open_oracle(registry.entries["oracle"]["reference"], registry.snapshot(), native.preparation(),
                native.context() | {"architecture": "x86_64"}, WireBudget(), Deadline(1))
        start.assert_not_called()


if __name__ == "__main__":
    unittest.main()
