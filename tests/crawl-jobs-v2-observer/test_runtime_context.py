"""Signed Linux-source fixtures; no native actors or host-context service starts."""
import copy
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import contracts as c
import runtime_admission as admission
import runtime_context as ctx
from linux_observation import LinuxReader
from streams import Deadline, WireBudget
from test_runtime_admission import context, preparation

KEY = b"context-fixture-key-32-bytes!!!!!"[:32].ljust(32, b"!")


class Clock:
    def __init__(self):
        self.now = 7000000.0  # Deliberately unrelated to the Linux nanosecond epoch.
    def __call__(self):
        return self.now


class Reader:
    def __init__(self):
        original = context()
        self.value = {key: original[key] for key in admission.CONTEXT_HASHES}
        self.value["architecture"] = original["architecture"]
        self.value.update(namespaces={name.removeprefix("host_"): original[name] for name in admission.HOST_NAMESPACES} |
            {key: original[key] for key in ("user_ns", "time_ns")}, ns_device=4, resolution_ns=1, yama_scope=0)
        self.tick = 1000000
        self.hook = None
    def kernel(self):
        if self.hook:
            self.hook()
        return copy.deepcopy(self.value)
    def monotonic_ns(self):
        self.tick += 100
        return self.tick


def fixture():
    reader, clock = Reader(), Clock()
    anchor = {"version": 2, "architecture": reader.value["architecture"], "daemon_sha256": c.sha(b"daemon"), "source_sha256": ctx.source_digest(),
        "kernel_boot_sha256": reader.value["kernel_boot_sha256"], "namespaces": reader.value["namespaces"],
        "ns_device": 4, "seccomp_filters": {"target": 1, "observer": 2}}
    prep = preparation()
    prep["artifacts"]["runtime_context_source"] = anchor["source_sha256"]
    endpoint = ctx.LinuxContextEndpoint(anchor, KEY, reader)
    source = ctx.ContextSource(anchor, KEY, endpoint.exchange, authorize=lambda *args: True)
    deadline, budget = Deadline(2, clock), WireBudget()
    return reader, clock, anchor, prep, endpoint, source, deadline, budget


def acquire(parts):
    _, _, anchor, prep, _, source, deadline, budget = parts
    return source.acquire("P1.pre.1.1", c.sha(b"scope"), prep, anchor["daemon_sha256"], deadline, budget)


class RuntimeContextTests(unittest.TestCase):
    def test_independent_linux_epoch_and_complete_signed_exchange(self):
        parts = fixture()
        lease = acquire(parts)
        value = lease.view("P1.pre.1.1", c.sha(b"scope"), parts[3], parts[6])
        self.assertEqual(value["not_before_ns"], 1000100)
        self.assertEqual(value["not_after_ns"] - value["not_before_ns"], 2000000000)
        self.assertEqual(value["host_pid_ns"], parts[2]["namespaces"]["pid_ns"])
        self.assertEqual(parts[7].frames, 4)
        self.assertLessEqual(parts[7].bytes, 4096)
        self.assertNotIn(KEY.hex(), c.canonical(lease.receipt).decode())
        self.assertFalse(lease.receipt["execution_authorized"])
        value["host_pid_ns"] = 999
        self.assertNotEqual(lease.view("P1.pre.1.1", c.sha(b"scope"), parts[3], parts[6])["host_pid_ns"], 999)

    def test_missing_authorization_and_foreign_daemon_prevent_source_io(self):
        for denied in ("authorization", "daemon", "source"):
            parts = fixture()
            called = []
            parts[5].exchange = lambda *args: called.append(args)
            if denied == "authorization":
                parts[5].authorize = None
            elif denied == "daemon":
                parts[5].anchor["daemon_sha256"] = c.sha(b"foreign")
            else:
                parts[3]["artifacts"]["runtime_context_source"] = c.sha(b"foreign")
            with self.subTest(denied=denied), self.assertRaises(c.Invalid):
                acquire(parts)
            self.assertEqual(called, [])

    def test_wrong_key_tamper_reordering_missing_and_extra_frames_refuse(self):
        for fault in ("key", "tamper", "reorder", "missing", "extra", "oversize"):
            parts = fixture()
            original = parts[5].exchange
            def changed(raw, deadline):
                reply = original(raw, deadline)
                rows = reply.splitlines(keepends=True)
                if fault == "tamper":
                    row = c.decode(rows[2]); row["payload"]["yama_scope"] = 2; rows[2] = c.canonical(row)
                elif fault == "reorder": rows[0], rows[1] = rows[1], rows[0]
                elif fault == "missing": rows.pop()
                elif fault == "extra": rows.append(rows[-1])
                elif fault == "oversize": return b"x" * 4097
                return b"".join(rows)
            parts[5].exchange = changed
            if fault == "key": parts[5]._key = b"x" * 32
            with self.subTest(fault=fault), self.assertRaises(c.Invalid):
                acquire(parts)

    def test_replayed_response_cannot_satisfy_a_fresh_challenge(self):
        parts = fixture()
        captured = []
        original = parts[5].exchange
        def capture(raw, deadline):
            captured.append(original(raw, deadline))
            return captured[-1]
        parts[5].exchange = capture
        acquire(parts)
        parts[5].exchange = lambda *_: captured[0]
        with self.assertRaises(c.Invalid): acquire(parts)

    def test_host_namespace_boot_and_mid_collection_drift_refuse(self):
        for fault in ("namespace", "boot", "drift"):
            parts = fixture()
            if fault == "namespace": parts[0].value["namespaces"]["pid_ns"] += 1
            elif fault == "boot": parts[0].value["kernel_boot_sha256"] = c.sha(b"different-boot")
            else: parts[0].hook = lambda: parts[0].value.update(yama_scope=parts[0].value["yama_scope"] + 1)
            with self.subTest(fault=fault), self.assertRaises(c.Invalid): acquire(parts)

    def test_roundtrip_expiry_and_lease_binding_fail_closed(self):
        parts = fixture()
        original = parts[5].exchange
        def delayed(raw, deadline):
            reply = original(raw, deadline)
            parts[1].now += 3
            return reply
        parts[5].exchange = delayed
        with self.assertRaises(c.Invalid): acquire(parts)
        parts = fixture(); lease = acquire(parts)
        with self.assertRaises(c.Invalid): lease.view("P1.pre.2.1", c.sha(b"scope"), parts[3], parts[6])
        with self.assertRaises(c.Invalid): lease.view("P1.pre.1.1", c.sha(b"foreign"), parts[3], parts[6])
        with self.assertRaises(c.Invalid): lease.check(Deadline(1, Clock()))
        parts[1].now += 2
        with self.assertRaises(c.Invalid): lease.check(parts[6])

    def test_uint64_linux_clocks_survive_long_uptime(self):
        parts = fixture(); parts[0].tick = 2**53 + 100
        lease = acquire(parts)
        self.assertGreater(lease.view("P1.pre.1.1", c.sha(b"scope"), parts[3], parts[6])["not_before_ns"], 2**53)

    def test_no_platform_or_fixed_read_fallback(self):
        with patch("linux_observation.os.uname", return_value=SimpleNamespace(sysname="Darwin", machine="aarch64")):
            with self.assertRaises(c.Invalid): LinuxReader().kernel()
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "input"; path.write_bytes(b"abcd")
            self.assertEqual(LinuxReader().read(str(path), 4), b"abcd")
            with self.assertRaises(c.Invalid): LinuxReader().read(str(path), 3)
            link = Path(folder) / "link"; link.symlink_to(path)
            with self.assertRaises(OSError): LinuxReader().read(str(link))
            fifo = Path(folder) / "fifo"; os.mkfifo(fifo)
            with self.assertRaises(c.Invalid): LinuxReader().read(str(fifo))

    def test_zero_key_is_not_an_authentication_configuration(self):
        with self.assertRaises(c.Invalid): ctx.mac(bytes(32), b"request")

    def test_backend_binds_signed_window_and_checks_it_after_native_admission(self):
        from test_runtime_admission import DockerAdmissionTests
        helper = DockerAdmissionTests()
        registry, _, backend, _, _ = helper.fixture()
        parts = fixture()
        anchor = parts[2] | {"daemon_sha256": registry.scope["daemon_sha256"]}
        endpoint = ctx.LinuxContextEndpoint(anchor, KEY, parts[0])
        source = ctx.ContextSource(anchor, KEY, endpoint.exchange, authorize=lambda *_: True)
        lease = backend.acquire_runtime_context(source, parts[3], parts[7], parts[6])
        actor_deadline = Deadline(4, parts[1])
        streams = []
        for role in ("target", "observer"):
            stream, ref = helper.open(registry, backend, role, actor_deadline)
            self.assertEqual(stream.sent_admission_window_end_ns, lease.view(registry.scope["case_id"], backend.scope_sha256, parts[3], actor_deadline)["not_after_ns"])
            backend.collect_native_admission(stream, ref, registry.snapshot(), parts[3], lease, actor_deadline)
            streams.append(stream)
        self.assertEqual(backend._context_lease, lease)
        parts[1].now += 3  # Actor deadline is live; the independent context is not.
        with self.assertRaisesRegex(c.Invalid, "CONTEXT_LEASE_EXPIRED"):
            streams[1].before_input()
        self.assertFalse(backend._native_admissions)

    def test_sent_linux_window_cannot_be_relabelled_during_collection(self):
        from test_runtime_admission import DockerAdmissionTests
        helper = DockerAdmissionTests()
        registry, _, backend, _, _ = helper.fixture()
        deadline = Deadline(3)
        stream, ref = helper.open(registry, backend, "target", deadline)
        stream.sent_admission_window_end_ns += 1
        with self.assertRaises(c.Invalid): helper.admit(registry, backend, stream, ref, deadline)
        self.assertTrue(stream.closed)

    def test_handshake_contains_the_exact_uint64_linux_expiry(self):
        from test_streams import binding
        from streams import ProcessStream
        # Test the transport contract without launching a process.
        stream = object.__new__(ProcessStream)
        stream.binding, stream.sent = binding(), False
        stream.admission_window_end_ns = None
        writes = []
        stream._send = writes.append
        end = 2**53 + 12345
        stream.bind_admission_window(end)
        with self.assertRaises(c.Invalid): stream.bind_admission_window(end + 1)
        stream.send_case()
        self.assertEqual(writes, [(stream.binding["case_id"] + "\n" + stream.binding["invocation_sha256"] + "\n" + str(end) + "\n").encode()])
        self.assertEqual(stream.sent_admission_window_end_ns, end)

    def test_linux_kernel_reader_uses_exact_bytes_and_zero_offset_policy(self):
        reader = LinuxReader()
        values = {"/proc/sys/kernel/random/boot_id": b"boot-identity\n", "/proc/self/uid_map": b"0 0 4294967295\n",
            "/proc/self/gid_map": b"0 0 4294967295\n", "/sys/kernel/security/lsm": b"capability,yama\n",
            "/proc/self/timens_offsets": b"monotonic 0 0\nboottime 0 0\n", "/proc/sys/kernel/yama/ptrace_scope": b"0\n"}
        reader.read = lambda path, limit=4095: values[path]
        reader.namespaces = lambda: ({name: index + 10 for index, name in enumerate(admission.NAMESPACES)}, 4)
        with patch("linux_observation.os.uname", return_value=SimpleNamespace(sysname="Linux", machine="aarch64", release="release")), \
             patch("linux_observation.time.clock_getres", return_value=0.000000001):
            result = reader.kernel()
            self.assertEqual(result["kernel_boot_sha256"], c.sha(b"boot-identity\n"))
            self.assertEqual(result["kernel_release_sha256"], c.sha(b"release"))
            self.assertEqual(result["resolution_ns"], 1)
            values["/proc/self/timens_offsets"] = b"monotonic 1 0\nboottime 0 0\n"
            with self.assertRaises(c.Invalid): reader.kernel()

    def test_oracle_security_reader_rejects_privileges_duplicate_fields_and_threads(self):
        from unittest.mock import Mock
        raw = b"Uid:\t999 999 999 999\nGid:\t999 999 999 999\nGroups:\t\nCapInh:\t0\nCapPrm:\t0\nCapEff:\t0\nCapBnd:\t0\nCapAmb:\t0\nNoNewPrivs:\t1\nSeccomp:\t2\nSeccomp_filters:\t1\nThreads:\t1\nTracerPid:\t0\n"
        reader = LinuxReader()
        prctl = Mock(side_effect=lambda operation, *_: 2 if operation == 21 else 1)
        with patch("linux_observation.ctypes.CDLL", return_value=SimpleNamespace(prctl=prctl)), \
             patch("linux_observation.os.getpid", return_value=1), patch("linux_observation.os.listdir", return_value=["1"]), \
             patch("linux_observation.resource.getrlimit", return_value=(0, 0)):
            reader.read = lambda *_: raw
            self.assertEqual(reader.security()["caps"], [0] * 5)
            for changed in (raw + b"Uid:\t999 999 999 999\n", raw.replace(b"CapBnd:\t0", b"CapBnd:\t1"),
                raw.replace(b"999 999 999 999", b"0 999 999 999", 1), raw.replace(b"Threads:\t1", b"Threads:\t2")):
                reader.read = lambda *_, data=changed: data
                with self.assertRaises(c.Invalid): reader.security()

    def test_missing_handshake_window_refuses_before_any_write(self):
        from test_streams import binding
        from streams import ProcessStream
        stream = object.__new__(ProcessStream)
        stream.binding, stream.sent, stream.admission_window_end_ns = binding(), False, None
        calls = []
        stream._send = calls.append
        with self.assertRaisesRegex(c.Invalid, "ADMISSION_WINDOW_REQUIRED"): stream.send_case()
        self.assertEqual(calls, [])


if __name__ == "__main__":
    unittest.main()
