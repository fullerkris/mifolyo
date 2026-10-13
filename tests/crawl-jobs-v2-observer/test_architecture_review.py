"""Architecture-review counterexamples: fake metadata and inert ELF bytes only."""
import json
from pathlib import Path
import struct
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import build_tools
import contracts as c
import docker_backend as db
import profiles
import runtime_context as context
from streams import Deadline, WireBudget
from test_owned_resources import DAEMON, prepared
from test_runtime_context import KEY, acquire, fixture


def inert_elf():
    """A tiny non-executed ELF/table fixture accepted by the static manifest reader."""
    raw = bytearray(0xa00)
    ident = b"\x7fELF\x02\x01\x01" + bytes(9)
    struct.pack_into("<16sHHIQQQIHHHHHH", raw, 0, ident, 3, 183, 1, 0x100, 64, 0x800, 0, 64, 56, 2, 64, 5, 1)
    struct.pack_into("<IIQQQQQQ", raw, 64, 1, 5, 0, 0, 0, 0x600, 0x600, 0x1000)
    struct.pack_into("<IIQQQQQQ", raw, 120, 1, 6, 0x600, 0x2600, 0x2600, 8, 8, 0x1000)
    for offset, word in ((0x100, 0xd2800544), (0x110, 0xd65f03c0), (0x120, 0xd503201f), (0x124, 0xd503201f)):
        struct.pack_into("<I", raw, offset, word)
    bpf = b"".join(struct.pack("<HBBI", *row) for row in profiles.target_filter_program())
    raw[0x400:0x400 + len(bpf)] = bpf
    symbols = (("obs_pre_effect", 0x100, 4, 3), ("obs_post_effect", 0x110, 4, 3),
        ("obs_vm_marker", 0x120, 4, 3), ("obs_send_marker", 0x124, 4, 3),
        ("obs_witness_address", 0x2600, 8, 4), ("obs_filter", 0x400, len(bpf), 3))
    strings = bytearray(b"\0")
    for index, (name, address, size, section) in enumerate(symbols, 1):
        offset = len(strings); strings.extend(name.encode() + b"\0")
        struct.pack_into("<IBBHQQ", raw, 0x700 + 24 * index, offset, 0x11 if name in ("obs_filter", "obs_witness_address") else 0x12, 0, section, address, size)
    raw[0x620:0x620 + len(strings)] = strings
    struct.pack_into("<IIQQQQIIQQ", raw, 0x840, 0, 3, 0, 0, 0x620, len(strings), 0, 0, 1, 0)
    struct.pack_into("<IIQQQQIIQQ", raw, 0x880, 0, 2, 0, 0, 0x700, 24 * 7, 1, 1, 8, 24)
    struct.pack_into("<IIQQQQIIQQ", raw, 0x8c0, 0, 1, 6, 0x100, 0x100, 0x500, 0, 0, 4, 0)
    struct.pack_into("<IIQQQQIIQQ", raw, 0x900, 0, 1, 3, 0x2600, 0x600, 8, 0, 0, 8, 0)
    return bytes(raw), bpf


class ArchitectureReviewTests(unittest.TestCase):
    def test_start_checks_each_roles_actual_image_architecture(self):
        for role in ("target", "observer", "oracle"):
            for architecture in ("amd64", None, "arm64"):
                with self.subTest(role=role, architecture=architecture):
                    registry, fake, _, _ = prepared()
                    if role == "observer":
                        fake.containers[registry.entries["target"]["reference"]["id"]]["State"].update(Running=True, Pid=100, Status="running")
                    image_reads, starts = [], []
                    def runner(argv, deadline):
                        args = argv[3:]
                        if args[0] == "info": return 0, DAEMON, b""
                        if args[0] == "version": return 0, b'{"Os":"linux","Arch":"arm64"}', b""
                        if args[1] == "ls":
                            return 0, "\n".join(fake.attachments(args[args.index("--filter") + 1].removeprefix("volume="), deadline)).encode(), b""
                        row = fake.inspect(args[0], args[2], deadline)
                        if args[0] == "image":
                            image_reads.append(args[2])
                            if architecture is None: row.pop("Architecture")
                            else: row["Architecture"] = architecture
                        return 0, json.dumps([row]).encode(), b""
                    def spawn(argv, bound, budget, deadline, **kwargs):
                        starts.append(argv)
                        return SimpleNamespace(binding=bound)
                    backend = db.DockerBackend(registry.scope, {name: "/unused" for name in ("target", "observer", "oracle")},
                        authorize=lambda *_: True, runner=runner, stream_factory=spawn)
                    args = (registry.entries[role]["reference"], registry.spec(role), registry.snapshot(), WireBudget(), Deadline(2))
                    if architecture == "arm64":
                        backend.open_actor(*args)
                        self.assertEqual(len(starts), 1)
                    else:
                        with self.assertRaisesRegex(c.Invalid, "IMAGE_IDENTITY"): backend.open_actor(*args)
                        self.assertEqual(starts, [])
                    self.assertEqual(image_reads, [registry.scope["images"][role]])

    def test_anchor_drift_during_authorization_refuses_before_exchange(self):
        for field, value in (("version", 1), ("architecture", "x86_64"), ("ns_device", 5)):
            parts = fixture()
            def authorize(*_):
                parts[5].anchor[field] = value
                parts[4].anchor[field] = value
                return True
            parts[5].authorize = authorize
            with patch.object(parts[5], "exchange", wraps=parts[4].exchange) as exchange, self.assertRaises(c.Invalid):
                acquire(parts)
            exchange.assert_not_called()
            self.assertEqual(parts[5].used, set())

    def test_signed_response_cannot_follow_a_mutated_public_anchor(self):
        parts = fixture(); original = parts[5].exchange
        def changed(raw, deadline):
            rows = original(raw, deadline).splitlines(keepends=True)
            kernel = c.decode(rows[0]); kernel["payload"]["architecture"] = "x86_64"
            rows[0] = c.canonical(kernel)
            seal = c.decode(rows[-1]); seal["mac"] = context.mac(KEY, raw + b"".join(rows[:-1])); rows[-1] = c.canonical(seal)
            parts[5].anchor["architecture"] = "x86_64"
            return b"".join(rows)
        parts[5].exchange = changed
        with self.assertRaises(c.Invalid): acquire(parts)

    def test_legacy_anchor_is_revalidated_at_each_source_acquisition(self):
        parts = fixture()
        parts[5].anchor["version"] = parts[4].anchor["version"] = 1
        with patch.object(parts[5], "authorize", return_value=True) as authorize, \
                patch.object(parts[5], "exchange", wraps=parts[4].exchange) as exchange, self.assertRaises(c.Invalid):
            acquire(parts)
        authorize.assert_not_called(); exchange.assert_not_called()

    def test_endpoint_and_request_parser_reject_legacy_anchor(self):
        parts, requests = fixture(), []
        original = parts[5].exchange
        parts[5].exchange = lambda raw, deadline: (requests.append(raw), original(raw, deadline))[1]
        acquire(parts)
        parts[4].anchor["version"] = 1
        request = c.decode(requests[0]); request["body"]["anchor_sha256"] = c.sha(c.canonical(parts[4].anchor))
        request["mac"] = context.mac(KEY, c.canonical(request["body"]))
        raw = c.canonical(request)
        with self.assertRaises(c.Invalid): context.request(raw, parts[4].anchor, KEY)
        with patch.object(parts[0], "kernel", wraps=parts[0].kernel) as kernel, self.assertRaises(c.Invalid):
            parts[4].exchange(raw, parts[6])
        kernel.assert_not_called()

    def test_endpoint_anchor_drift_during_observation_refuses(self):
        parts = fixture()
        parts[0].hook = lambda: parts[4].anchor.update(version=1)
        with self.assertRaises(c.Invalid): acquire(parts)

    def test_valid_context_receipt_remains_bound_to_the_signed_request(self):
        parts, requests = fixture(), []
        original = parts[5].exchange
        parts[5].exchange = lambda raw, deadline: (requests.append(c.decode(raw)["body"]), original(raw, deadline))[1]
        lease = acquire(parts)
        ctx = lease.view("P1.pre.1.1", c.sha(b"scope"), parts[3], parts[6])
        self.assertEqual(ctx["architecture"], requests[0]["architecture"])
        self.assertEqual(lease.receipt["anchor_sha256"], requests[0]["anchor_sha256"])
        self.assertEqual(lease.receipt["architecture"], lease.binding["architecture"])

    def test_record_rejects_foreign_target_and_stale_or_legacy_manifest(self):
        target, bpf = inert_elf()
        for fault in ("foreign", "stale", "legacy"):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as folder:
                root = Path(folder)
                manifest = build_tools.target_manifest(target)
                changed = bytearray(target)
                if fault == "foreign": struct.pack_into("<H", changed, 18, 62)
                elif fault == "stale": changed[-1] ^= 1
                else: manifest["version"] = 1
                for name, raw in (("target", bytes(changed)), ("observer", target), ("target-filter.bpf", bpf),
                        ("target-manifest.json", c.canonical(manifest))):
                    (root / name).write_bytes(raw)
                library = root / "library"; library.write_bytes(b"inert-library")
                with patch.object(sys, "argv", ["build_tools.py", "record", str(root), "aarch64"]), \
                        patch.object(build_tools.subprocess, "run", return_value=SimpleNamespace(stdout=b"aarch64-linux-gnu\n")) as compiler, \
                        patch.object(build_tools.subprocess, "check_output", return_value=str(library)) as lookup, \
                        patch.object(build_tools.Path, "rglob", return_value=[]):
                    with self.assertRaises(c.Invalid): build_tools.main()
                    compiler.assert_not_called(); lookup.assert_not_called()
                self.assertFalse((root / "toolchain.json").exists())

    def test_record_accepts_matching_arm64_target_and_manifest(self):
        target, bpf = inert_elf()
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            for name, raw in (("target", target), ("observer", target), ("target-filter.bpf", bpf),
                    ("target-manifest.json", c.canonical(build_tools.target_manifest(target)))):
                (root / name).write_bytes(raw)
            library = root / "library"; library.write_bytes(b"inert-library")
            with patch.object(sys, "argv", ["build_tools.py", "record", str(root), "aarch64"]), \
                    patch.object(build_tools.subprocess, "run", return_value=SimpleNamespace(stdout=b"aarch64-linux-gnu\n")), \
                    patch.object(build_tools.subprocess, "check_output", return_value=str(library)), \
                    patch.object(build_tools.Path, "rglob", return_value=[]):
                build_tools.main()
            record = c.decode((root / "toolchain.json").read_bytes())
            self.assertEqual(record["target_sha256"], c.sha(target))
            self.assertEqual(record["architecture"], "aarch64")
            self.assertEqual(record["platform_sha256"], c.platform_digest("aarch64"))
            self.assertFalse(record["native_programs_executed"])


if __name__ == "__main__":
    unittest.main()
