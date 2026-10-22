"""Dual-ABI controls using inert ELF bytes, interpreted filters and simulated I/O."""
import copy
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
from linux_observation import LinuxReader
import owned_resources as owned
import profiles
import runtime_admission as admission
import runtime_context as context
from streams import Deadline, ProcessStream, WireBudget
from test_architecture_contracts import header
from test_filter import interpret
from test_owned_resources import fixture_scope
import test_runtime_admission as native
from test_runtime_context import KEY, acquire, fixture
from test_streams import binding


def oci_allows(policy, name, args=(0, 0, 0, 0, 0, 0)):
    """Independent interpretation of the emitted OCI rules, including 64-bit args."""
    for row in policy["syscalls"]:
        if name not in row["names"]:
            continue
        matches = True
        for condition in row["args"]:
            actual = args[condition["index"]]
            if condition["op"] == "SCMP_CMP_EQ": matches &= actual == condition["value"]
            elif condition["op"] == "SCMP_CMP_MASKED_EQ": matches &= actual & condition["value"] == condition["valueTwo"]
            else: raise AssertionError("unexpected OCI comparison")
        if matches:
            return row["action"] == "SCMP_ACT_ALLOW"
    return False


def inert_x64_elf():
    """ELF/table fixture is data only; no executable fixture is launched."""
    raw = bytearray(0x1000)
    struct.pack_into("<16sHHIQQQIHHHHHH", raw, 0, b"\x7fELF\x02\x01\x01" + bytes(9), 3, 62, 1, 0x100, 64, 0xb00, 0, 64, 56, 2, 64, 5, 1)
    struct.pack_into("<IIQQQQQQ", raw, 64, 1, 5, 0, 0, 0, 0x700, 0x700, 0x1000)
    struct.pack_into("<IIQQQQQQ", raw, 120, 1, 6, 0x700, 0x2700, 0x2700, 8, 8, 0x1000)
    raw[0x100:0x10e] = bytes.fromhex("48c7012a000000488916488917c3")
    raw[0x120:0x122] = raw[0x128:0x12a] = b"\x90\xc3"
    bpf = b"".join(struct.pack("<HBBI", *row) for row in profiles.target_filter_program("x86_64"))
    raw[0x400:0x400 + len(bpf)] = bpf
    symbols = (("obs_effect", 0x100, 14, 3), ("obs_pre_effect", 0x100, 0, 3), ("obs_post_effect", 0x10d, 0, 3),
        ("obs_vm_marker", 0x120, 2, 3), ("obs_send_marker", 0x128, 2, 3),
        ("obs_witness_address", 0x2700, 8, 4), ("obs_filter", 0x400, len(bpf), 3))
    strings, offsets = bytearray(b"\0"), {}
    for index, (name, address, size, section) in enumerate(symbols, 1):
        offset = len(strings); strings.extend(name.encode() + b"\0")
        entry = 0x900 + 24 * index; offsets[name] = entry
        struct.pack_into("<IBBHQQ", raw, entry, offset, 0x11 if name in ("obs_witness_address", "obs_filter") else 0x12,
            0, section, address, size)
    raw[0x800:0x800 + len(strings)] = strings
    struct.pack_into("<IIQQQQIIQQ", raw, 0xb40, 0, 3, 0, 0, 0x800, len(strings), 0, 0, 1, 0)
    struct.pack_into("<IIQQQQIIQQ", raw, 0xb80, 0, 2, 0, 0, 0x900, 24 * 8, 1, 1, 8, 24)
    struct.pack_into("<IIQQQQIIQQ", raw, 0xbc0, 0, 1, 6, 0x100, 0x100, 0x600, 0, 0, 8, 0)
    struct.pack_into("<IIQQQQIIQQ", raw, 0xc00, 0, 1, 3, 0x2700, 0x700, 8, 0, 0, 8, 0)
    return bytes(raw), bpf, offsets


class X64FilterTests(unittest.TestCase):
    def test_x32_compat_negative_and_foreign_syscalls_cannot_bypass_self_filter(self):
        program = profiles.target_filter_program("x86_64")
        for number in (512, 521, 547, 548, 0x40000000, 0x4000003b, 0x40000065, 0x40000142, -1):
            self.assertEqual(interpret(program, number=number, architecture=0xc000003e), 0x80000000)
        for arch in (0xc00000b7, 0x40000003, 0, 62):
            self.assertEqual(interpret(program, number=101, architecture=arch), 0x80000000)
        for number in (59, 322):
            self.assertEqual(interpret(program, number=number, architecture=0xc000003e), 0x50001)
        self.assertEqual(interpret(program, number=0, architecture=0xc000003e), 0x7fff0000)

    def test_ptrace_requests_tid_high_bits_and_gp_write_denials_in_both_filters(self):
        program, policy = profiles.target_filter_program("x86_64"), profiles.profile("observer", "x86_64")
        simple = {2, 7, 9, 17, 0x4202, 0x4206, 0x4207}
        for request in [*range(33), *range(0x4200, 0x4210), 2**32 + 7]:
            for tid in (0, 1, 2, 2**32 + 1):
                for offset in (0, 1, 0x402, 0x403, 848, 888, 896, 904, 2**32 + 848):
                    args = (request, tid, offset, 0, 0, 0)
                    expected = tid == 1 and (request in simple or request == 0x4204 and offset == 1 or
                        request == 3 and offset in (848, 856, 864, 872, 896, 904) or request == 6 and offset in (848, 904))
                    with self.subTest(args=args):
                        self.assertEqual(interpret(program, number=101, architecture=0xc000003e, args=args), 0x7fff0000 if expected else 0x50001)
                        self.assertEqual(oci_allows(policy, "ptrace", args), expected)

    def test_only_dr0_address_dr6_reset_and_dr7_local_execute_are_writable(self):
        program, policy = profiles.target_filter_program("x86_64"), profiles.profile("observer", "x86_64")
        for offset in (*range(0, 912, 8), 849, 2**32 + 904):
            for value in (0, 1, 2, 3, 0x400, 0x10001, 0x90001, 0xffff0ff0, 0xffff0ff1, 2**32, 2**32 + 1):
                args = (6, 1, offset, value, 0, 0)
                expected = offset == 848 or offset == 896 and value == 0xffff0ff0 or offset == 904 and value in (0, 1)
                self.assertEqual(interpret(program, number=101, architecture=0xc000003e, args=args), 0x7fff0000 if expected else 0x50001)
                self.assertEqual(oci_allows(policy, "ptrace", args), expected)

    def test_x64_loader_tls_and_chmod_are_narrow_and_roles_remain_confined(self):
        for role in ("target", "observer", "oracle"):
            policy = profiles.profile(role, "x86_64")
            self.assertEqual(policy["architectures"], ["SCMP_ARCH_X86_64"])
            self.assertEqual(policy["defaultAction"], "SCMP_ACT_ERRNO")
            for operation in (0, 0x1001, 0x1002, 0x1003, 0x1012, 0x1022, 2**32 + 0x1002):
                self.assertEqual(oci_allows(policy, "arch_prctl", (operation, 0, 0, 0, 0, 0)), operation == 0x1002)
            self.assertEqual(oci_allows(policy, "chmod"), role == "target")
            for name in ("process_vm_writev", "perf_event_open", "bpf", "setns", "unshare", "execveat", "mount", "kill"):
                self.assertFalse(oci_allows(policy, name))
            if role != "observer": self.assertFalse(oci_allows(policy, "ptrace", (7, 1, 0, 0, 0, 0)))
            self.assertFalse(oci_allows(policy, "mprotect", (0, 4096, 5, 0, 0, 0)))
            self.assertFalse(oci_allows(policy, "mmap", (0, 4096, 7, 2, 0, 0)))
            self.assertFalse(oci_allows(policy, "mmap", (0, 4096, 5, 0x22, 0, 0)))
            self.assertEqual(oci_allows(policy, "mmap", (0, 4096, 5, 2, 0, 0)), role == "oracle")


class X64BuildTests(unittest.TestCase):
    def test_variable_instruction_lengths_and_complete_effect_boundary(self):
        raw, _, _ = inert_x64_elf()
        value = build_tools.target_manifest(raw, "x86_64")
        self.assertEqual(value["platform_sha256"], c.platform_digest("x86_64"))
        self.assertEqual(value["symbols"]["obs_post_effect"]["vaddr"], 0x10d)
        self.assertEqual([value["symbols"][name]["instruction_bytes"] for name in
            ("obs_pre_effect", "obs_post_effect", "obs_vm_marker", "obs_send_marker")], [7, 1, 1, 1])
        self.assertEqual(value["symbols"]["obs_pre_effect"]["instruction"].to_bytes(7, "little"), bytes.fromhex("48c7012a000000"))
        self.assertFalse(value["execution_authorized"])
        with self.assertRaises(c.Invalid): build_tools.target_manifest(raw, "aarch64")

    def test_every_effect_and_marker_byte_mutation_is_rejected(self):
        raw, _, _ = inert_x64_elf()
        for offset in (*range(0x100, 0x10e), 0x120, 0x121, 0x128, 0x129):
            changed = bytearray(raw); changed[offset] ^= 1
            with self.subTest(offset=offset), self.assertRaises(c.Invalid): build_tools.target_manifest(bytes(changed), "x86_64")

    def test_shifted_truncated_nonexec_and_aliased_markers_are_rejected(self):
        raw, _, offsets = inert_x64_elf()
        for name, displacement, value in (("obs_pre_effect", 8, 0x101), ("obs_post_effect", 8, 0x10c),
                ("obs_effect", 16, 13), ("obs_vm_marker", 16, 1), ("obs_vm_marker", 8, 0x128),
                ("obs_vm_marker", 8, 0x2700), ("obs_witness_address", 8, 0x2701), ("obs_witness_address", 16, 1)):
            changed = bytearray(raw); struct.pack_into("<Q", changed, offsets[name] + displacement, value)
            with self.subTest(name=name, value=value), self.assertRaises(c.Invalid): build_tools.target_manifest(bytes(changed), "x86_64")
        changed = bytearray(raw); struct.pack_into("<Q", changed, 64 + 32, 0x10d)
        with self.assertRaises(c.Invalid): build_tools.target_manifest(bytes(changed), "x86_64")

    def test_x64_headers_and_manifest_bind_the_compiler_abi(self):
        raw, _, _ = inert_x64_elf()
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); (root / "target").write_bytes(raw)
            for mode in ("headers", "manifest"):
                with patch.object(sys, "argv", ["build_tools.py", mode, folder, "x86_64"]): build_tools.main()
            header_text = (root / "target_manifest.h").read_text()
            self.assertIn("#define OBS_TARGET_MACHINE UINT64_C(62)", header_text)
            self.assertIn("#define OBS_PRE_BYTES UINT64_C(7)", header_text)
            self.assertIn("#define OBS_POST_BYTES UINT64_C(1)", header_text)
            self.assertEqual(c.decode((root / "observer-seccomp.json").read_bytes())["architectures"], ["SCMP_ARCH_X86_64"])

    def test_record_rejects_foreign_compiled_filter_before_toolchain_queries(self):
        raw, bpf, _ = inert_x64_elf()
        foreign = b"".join(struct.pack("<HBBI", *row) for row in profiles.target_filter_program())
        # A tampered filter file and matching embedded bytes do not prove canonicality.
        changed = bytearray(raw); changed[0x400:0x400 + len(bpf)] = bytes(len(bpf))
        changed[0x400:0x400 + len(foreign)] = foreign
        _, _, symbols = inert_x64_elf()
        struct.pack_into("<Q", changed, symbols["obs_filter"] + 16, len(foreign))
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            for name, data in (("target", raw), ("observer", bytes(changed)), ("target-filter.bpf", foreign),
                    ("target-manifest.json", c.canonical(build_tools.target_manifest(raw, "x86_64")))):
                (root / name).write_bytes(data)
            with patch.object(sys, "argv", ["build_tools.py", "record", folder, "x86_64"]), \
                    patch.object(build_tools.subprocess, "run") as compiler, self.assertRaisesRegex(c.Invalid, "BUILD_FILTER_ARCHITECTURE"):
                build_tools.main()
            compiler.assert_not_called()
            self.assertFalse((root / "toolchain.json").exists())

    def test_oracle_manifest_binds_x64_interpreter_without_running_worker(self):
        from importlib.machinery import ExtensionFileLoader
        import oracle_worker
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); executable = root / "python"; executable.write_bytes(header("x86_64"))
            extension = root / "fixture.so"; extension.write_bytes(header("x86_64"))
            module = SimpleNamespace(__spec__=SimpleNamespace(loader=ExtensionFileLoader("fixture", str(extension)), origin=str(extension)))
            collect = build_oracle.extension_inventory
            destination = root / "artifacts"
            with patch.object(sys, "argv", ["build_oracle.py", str(destination), "x86_64"]), \
                    patch.object(sys, "executable", str(executable)), \
                    patch.object(build_oracle.os, "uname", return_value=SimpleNamespace(sysname="Linux", machine="x86_64")), \
                    patch.object(build_oracle, "extension_inventory", side_effect=lambda arch: collect(arch, [module])), \
                    patch.object(oracle_worker, "main") as worker_main, patch.object(oracle_worker, "Engine") as engine:
                build_oracle.main()
                worker_main.assert_not_called(); engine.assert_not_called()
            record = c.decode((destination / "oracle-artifacts.json").read_bytes())
            self.assertEqual(record["architecture"], "x86_64")
            self.assertEqual(record["oracle_elf"], c.sha(header("x86_64")))
            self.assertEqual(record["version"], 3)
            self.assertTrue(record["worker_import_checked"] and record["extensions"])
            self.assertEqual(record["extensions"], {str(extension): c.sha(header("x86_64"))})
            self.assertFalse(record["worker_executed"] or record["runtime_feasibility_proven"])


class DualArchitectureRuntimeTests(unittest.TestCase):
    def test_x64_backend_and_benign_oracle_join_real_stream_protocol(self):
        from test_oracle_bridge import OraclePair
        with OraclePair(architecture="x86_64") as pair:
            target, observer = pair.pair()
            session = pair.open_oracle()
            self.assertEqual(session.phase, "INITIALIZED")
            self.assertEqual(set(pair.backend._native_admissions), {"target", "observer", "oracle"})
            for entry in pair.backend._native_admissions.values():
                self.assertEqual(entry["validated"]["binding"]["architecture"], "x86_64")
            observer.arm_observer(); observer.receive("ARMED")
            target.start_target()
            self.assertEqual(target.sent_admission_window_end_ns, native.context()["not_after_ns"])

    def test_x64_start_refuses_actual_arm64_image_before_transport(self):
        from test_admission_dispatch import PipePair
        with PipePair(architecture="x86_64") as pair:
            original = pair.fake.inspect
            def foreign(kind, selector, deadline):
                row = original(kind, selector, deadline)
                if kind == "image": row["Architecture"] = "arm64"
                return row
            pair.fake.inspect = foreign
            with patch.object(pair.backend, "stream_factory") as spawn, self.assertRaises(c.Invalid): pair.open("target")
            spawn.assert_not_called()

    def test_signed_x64_context_binds_request_receipt_and_lease(self):
        parts = list(fixture())
        parts[0].value["architecture"] = parts[2]["architecture"] = "x86_64"
        parts[3] = c.preparation(parts[3]["artifacts"], "x86_64")
        parts[4] = context.LinuxContextEndpoint(parts[2], KEY, parts[0])
        parts[5] = context.ContextSource(parts[2], KEY, parts[4].exchange, authorize=lambda *_: True)
        lease = acquire(parts)
        self.assertEqual(lease.receipt["architecture"], "x86_64")
        self.assertEqual(lease.view("P1.pre.1.1", c.sha(b"scope"), parts[3], parts[6])["architecture"], "x86_64")
        foreign = c.preparation(parts[3]["artifacts"], "aarch64")
        with self.assertRaises(c.Invalid): lease.view("P1.pre.1.1", c.sha(b"scope"), foreign, parts[6])

    def test_all_native_roles_admit_both_architectures_and_refuse_opposite_peer(self):
        for architecture in ("aarch64", "x86_64"):
            prep = c.preparation(native.preparation()["artifacts"], architecture)
            ctx = native.context() | {"architecture": architecture}
            target = None
            for role in ("target", "observer", "oracle"):
                bound = native.binding(role) | {"architecture": architecture}
                frames = native.frames(role, bound=bound)
                value = admission.validate(frames, bound, prep, ctx, target=target)
                if role == "target": target = value
                self.assertEqual(admission.summary(value)["architecture"], architecture)
                self.assertFalse(value["execution_authorized"] or value["runtime_evidence"])
                foreign = "x86_64" if architecture == "aarch64" else "aarch64"
                with self.assertRaises(c.Invalid): admission.validate(native.changed(frames, 0, architecture=foreign), bound, prep, ctx,
                    target=target if role != "target" else None)

    def test_x64_resource_profiles_cannot_reuse_arm64_digests(self):
        old = fixture_scope()
        value = owned.scope(old["case_id"], old["invocation_id"], old["images"], old["environment_sha256"], old["daemon_sha256"], "x86_64")
        self.assertEqual(owned.validate_scope(value), value)
        for role in owned.ACTORS:
            self.assertNotEqual(value["profiles"][role], old["profiles"][role])
            changed = copy.deepcopy(value); changed["profiles"][role] = old["profiles"][role]
            with self.assertRaises(c.Invalid): owned.validate_scope(changed)

    def test_oracle_selects_observed_native_abi_and_rejects_unknown_or_ilp32(self):
        for architecture in ("aarch64", "x86_64", "amd64", "i686", "x32"):
            with patch("linux_observation.os.uname", return_value=SimpleNamespace(sysname="Linux", machine=architecture)):
                if architecture in ("aarch64", "x86_64"):
                    self.assertEqual(LinuxReader.native_process().architecture, architecture)
                else:
                    with self.assertRaises(c.Invalid): LinuxReader.native_process()
        with patch("linux_observation.ctypes.sizeof", return_value=4), self.assertRaises(c.Invalid): LinuxReader.native_process()
        with patch("linux_observation.os.uname", return_value=SimpleNamespace(sysname="Darwin", machine="x86_64")), self.assertRaises(c.Invalid):
            LinuxReader.native_process()

    def test_x64_reader_requires_matching_elf_even_when_uname_matches(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "python"
            real_open = os.open
            for architecture in ("aarch64", "x86_64"):
                raw = header(architecture) + b"private-fixture"; path.write_bytes(raw)
                with patch("linux_observation.os.open", side_effect=lambda *_: real_open(path, os.O_RDONLY)):
                    if architecture == "x86_64": self.assertEqual(LinuxReader("x86_64").executable(), c.sha(raw))
                    else:
                        with self.assertRaises(c.Invalid): LinuxReader("x86_64").executable()

    def test_x64_benign_stream_keeps_three_line_handshake_and_binding(self):
        bound = binding() | {"architecture": "x86_64"}
        end = 2**53 + 123
        code = 'import sys; assert [sys.stdin.readline().strip() for _ in range(3)] == sys.argv[1:4]; sys.stdout.write(sys.argv[4] + \'{"phase":"READY","version":1}\\n\'); sys.stdout.flush(); sys.stdin.read()'
        frames = b"".join(native.frames(bound=bound)).decode()
        stream = ProcessStream([sys.executable, "-I", "-B", "-c", code, bound["case_id"], bound["invocation_sha256"], str(end), frames], bound, WireBudget(), Deadline(3))
        try:
            stream.bind_admission_window(end); stream.send_case()
            for phase in admission.PHASES: stream.receive(phase)
            self.assertEqual(stream.receive("READY")["phase"], "READY")
            self.assertEqual(stream.binding["architecture"], "x86_64")
            self.assertEqual(stream.sent_admission_window_end_ns, end)
        finally:
            stream.close()


if __name__ == "__main__":
    unittest.main()
