"""Synthetic native frames, independent expectations and fake Docker only."""
import copy
import json
import unittest

import contracts as c
import controller
import docker_backend as db
import owned_resources as owned
import runtime_admission as admission
from profiles import profile
from streams import Deadline
from test_owned_resources import DAEMON, prepared

D = c.sha(b"synthetic-admission")


def preparation():
    artifacts = {key: D for key in c.ARTIFACTS}
    for role in ("target", "observer", "oracle"):
        artifacts[role + "_profile"] = c.sha(c.canonical(profile(role)))
    artifacts.update(target_elf=c.sha(b"target-elf"), observer_elf=c.sha(b"observer-elf"))
    return c.preparation(artifacts)


def context():
    return {**{name: c.sha(name.encode()) for name in admission.CONTEXT_HASHES}, **{name: 100 + i for i, name in enumerate(admission.HOST_NAMESPACES)},
        "user_ns": 20, "time_ns": 30, "ns_device": 4, "resolution_ns": 1, "yama_scope": 0,
        "seccomp_filters": {"target": 1, "observer": 2}, "not_before_ns": 1000000, "not_after_ns": 9000000000}


def binding(role="target", invocation=D, case="P1.pre.1.1"):
    return {"case_id": case, "invocation_sha256": invocation, "container_id": c.sha(role.encode()),
        "image": "sha256:" + c.sha((role + "-image").encode()), "role": role}


def frames(role="target", *, bound=None):
    bound = binding(role) if bound is None else bound
    common = {key: bound[key] for key in ("case_id", "invocation_sha256", "role")} | {"version": 1}
    pid = 1 if role == "target" else 2
    ctx = context()
    identity = common | {"phase": "IDENTITY", "boot_sha256": ctx["kernel_boot_sha256"], "executable_sha256": preparation()["artifacts"][role + "_elf"],
        "uid_map_sha256": ctx["uid_map_sha256"], "gid_map_sha256": ctx["gid_map_sha256"], "pid": pid, "tid": pid, "start_ticks": 123 + pid,
        "pid_ns": 10, "user_ns": 20, "time_ns": 30, "ns_device": 4,
        "cgroup_ns": 40 + pid, "mnt_ns": 50 + pid, "net_ns": 60 + pid, "ipc_ns": 70 + pid}
    security = common | {"phase": "CONFINEMENT", "caps": [0] * 5, "uids": [999] * 4, "gids": [999] * 4, "groups": [],
        "core_soft": 0, "core_hard": 0, "dumpable": 1, "nnp": 1, "seccomp": 2, "seccomp_filters": 1 if role == "target" else 2,
        "tasks": [pid], "threads": 1, "tracer_pid": 0}
    resources = common | {"phase": "RESOURCES", "cgroup_sha256": c.sha(b"0::/\n"), "memory_max": 67108864,
        "memory_current": 1048576, "memory_peak": 2097152, "swap_max": 0, "cpu_period": 100000, "cpu_quota": 100000,
        "pids_max": 16, "pids_current": 1, "processes": [pid]}
    clock = common | {"phase": "CLOCK", "kernel_release_sha256": ctx["kernel_release_sha256"], "lsm_sha256": ctx["lsm_sha256"], "time_offsets_sha256": ctx["time_offsets_sha256"],
        "monotonic_ns": 2000000 + pid, "resolution_ns": 1, "yama_scope": 0}
    return [c.canonical(row) for row in (identity, security, resources, clock)]


def changed(raw_frames, index, **updates):
    result = list(raw_frames)
    result[index] = c.canonical(c.decode(result[index]) | updates)
    return result


class AdmissionTests(unittest.TestCase):
    def validate(self, raw=None, role="target", **kwargs):
        return admission.validate(frames(role) if raw is None else raw, binding(role), preparation(), context(), **kwargs)

    def test_positive_native_pair_is_bounded_and_never_grants_execution(self):
        target = self.validate()
        observer = self.validate(role="observer", target=target)
        for result in (target, observer):
            self.assertTrue(all(len(raw) <= 1024 for raw in result["frames"]))
            self.assertLessEqual(len(c.canonical(admission.summary(result))), 1024)
            self.assertFalse(result["execution_authorized"])
            self.assertFalse(result["runtime_evidence"])
        self.assertNotEqual(target["identity_sha256"], observer["identity_sha256"])

    def test_missing_duplicate_reordered_and_foreign_frames_reject(self):
        raw = frames()
        for candidate in (raw[:-1], raw + raw[:1], [raw[1], raw[0], *raw[2:]], [raw[0], raw[0], *raw[2:]],
                          changed(raw, 0, case_id="P1.pre.2.1"), changed(raw, 1, invocation_sha256="f" * 64),
                          changed(raw, 2, role="observer"), changed(raw, 3, private="private-canary")):
            with self.subTest(candidate=candidate), self.assertRaises(c.Invalid):
                self.validate(candidate)

    def test_noncanonical_float_duplicate_and_overlong_wire_rejects(self):
        for raw in (b'{}', b'{"version":1,"version":1}\n', b'{"value":1.0}\n', b'{"value":NaN}\n', b'x' * 1025):
            with self.assertRaises(c.Invalid):
                admission.native_frame(raw, "IDENTITY")

    def test_summary_cannot_relabel_or_export_unvalidated_values(self):
        original = self.validate()
        for update in ({"runtime_evidence": True}, {"execution_authorized": True}, {"identity_sha256": "e" * 64},
                       {"observation_sha256": "e" * 64}, {"private": "private-canary"},
                       {"binding": original["binding"] | {"role": "private-canary"}}):
            with self.assertRaises(c.Invalid) as caught:
                admission.summary(original | update)
            self.assertNotIn("private-canary", str(caught.exception))

    def test_wrong_executable_restart_or_host_namespace_rejects(self):
        raw = frames()
        for updates in ({"executable_sha256": "e" * 64}, {"start_ticks": 0}, {"start_ticks": True},
                        {"pid": 2}, {"tid": 2}, {"ns_device": 7}, {"uid_map_sha256": "f" * 64}, {"boot_sha256": "f" * 64}):
            with self.subTest(updates=updates), self.assertRaises(c.Invalid):
                self.validate(changed(raw, 0, **updates))
        for name in admission.HOST_NAMESPACES:
            with self.assertRaises(c.Invalid):
                self.validate(changed(raw, 0, **{name.removeprefix("host_"): context()[name]}))

    def test_cross_field_hash_substitution_and_allowed_primary_group(self):
        original = frames()
        identity = c.decode(original[0])
        with self.assertRaises(c.Invalid):
            self.validate(changed(original, 0, uid_map_sha256=identity["gid_map_sha256"]))
        clock = c.decode(original[3])
        with self.assertRaises(c.Invalid):
            self.validate(changed(original, 3, lsm_sha256=clock["kernel_release_sha256"]))
        self.validate(changed(original, 1, groups=[999]))

    def test_nonzero_capabilities_all_uid_slots_and_extra_groups_reject(self):
        for index in range(5):
            caps = [0] * 5
            caps[index] = 0x80000
            with self.assertRaises(c.Invalid):
                self.validate(changed(frames(), 1, caps=caps))
        for key in ("uids", "gids"):
            for index in range(4):
                ids = [999] * 4
                ids[index] = 0
                with self.assertRaises(c.Invalid):
                    self.validate(changed(frames(), 1, **{key: ids}))
        for groups in ([0], [999, 999], [True], False):
            with self.assertRaises(c.Invalid):
                self.validate(changed(frames(), 1, groups=groups))

    def test_security_and_task_inventory_fail_closed(self):
        for key, value in (("nnp", 0), ("seccomp", 0), ("seccomp", 1), ("seccomp_filters", 2),
                           ("dumpable", 0), ("core_soft", 1), ("core_hard", True), ("tracer_pid", 42),
                           ("threads", 2), ("tasks", [2]), ("tasks", [1, 2]), ("caps", [False] * 5)):
            with self.subTest(key=key, value=value), self.assertRaises(c.Invalid):
                self.validate(changed(frames(), 1, **{key: value}))

    def test_actual_cgroup_limits_and_usage_are_checked(self):
        for key, value in (("cgroup_sha256", D), ("memory_max", 67108865), ("memory_current", 0),
                           ("memory_peak", 1048575), ("memory_peak", 67108865), ("swap_max", 1),
                           ("cpu_quota", 200000), ("cpu_period", 0), ("pids_max", 17),
                           ("pids_current", 2), ("processes", [2]), ("processes", [1, 2])):
            with self.subTest(key=key, value=value), self.assertRaises(c.Invalid):
                self.validate(changed(frames(), 2, **{key: value}))

    def test_kernel_clock_context_and_stale_windows_reject(self):
        for key, value in (("kernel_release_sha256", "e" * 64), ("lsm_sha256", "e" * 64),
                           ("time_offsets_sha256", "e" * 64), ("resolution_ns", 2), ("yama_scope", 1),
                           ("monotonic_ns", 999999), ("monotonic_ns", 9000000001)):
            with self.assertRaises(c.Invalid):
                self.validate(changed(frames(), 3, **{key: value}))
        for update in ({"not_after_ns": 11000000001}, {"not_before_ns": True}, {"unexpected": 1},
                       {"seccomp_filters": {"target": 1, "observer": 1}}):
            with self.assertRaises(c.Invalid):
                admission.validate_context(context() | update)

    def test_native_clock_uses_exact_uint64_without_widening_counters(self):
        for stamp in (2**53 - 1, 2**53, 105 * 86400 * 1000000000, 2**64 - 2):
            ctx = context() | {"not_before_ns": stamp - 1, "not_after_ns": stamp + 1}
            target = admission.validate(changed(frames(), 3, monotonic_ns=stamp), binding(), preparation(), ctx)
            observer = admission.validate(changed(frames("observer"), 3, monotonic_ns=stamp + 1), binding("observer"), preparation(), ctx, target=target)
            self.assertEqual(c.decode(target["frames"][3])["monotonic_ns"], stamp)
            self.assertFalse(observer["runtime_evidence"])
        with self.assertRaises(c.Invalid):
            c.integer(2**53)
        for stamp in (True, False, 1.0, -1, 2**64):
            with self.assertRaises(c.Invalid):
                admission.native_frame(changed(frames(), 3, monotonic_ns=stamp)[3], "CLOCK")
            with self.assertRaises(c.Invalid):
                admission.validate_context(context() | {"not_after_ns": stamp})
        with self.assertRaises(c.Invalid):
            admission.validate_context(context() | {"not_before_ns": 2**64 - 1, "not_after_ns": 2**64})

    def test_observer_peer_must_be_revalidated_and_in_the_exact_namespaces(self):
        target = self.validate()
        with self.assertRaises(c.Invalid):
            self.validate(role="observer")
        for key in admission.NAMESPACES:
            value = 999 if key in ("pid_ns", "user_ns", "time_ns") else c.decode(target["frames"][0])[key]
            with self.assertRaises(c.Invalid):
                self.validate(changed(frames("observer"), 0, **{key: value}), "observer", target=target)
        for peer in ({}, target | {"identity_sha256": "e" * 64}, target | {"runtime_evidence": True},
                     target | {"frames": changed(target["frames"], 1, caps=[1] * 5)}):
            with self.assertRaises(c.Invalid):
                self.validate(role="observer", target=peer)
        with self.assertRaises(c.Invalid):
            self.validate(changed(frames("observer"), 3, monotonic_ns=2000000), "observer", target=target)


class FakeStream:
    def __init__(self, bound, deadline, callback):
        self.binding, self.deadline, self.before_input = bound, deadline, callback
        self.sent, self.closed, self.phase = False, False, None
        self.admission_frames, self.hook = [], None
        self.stdout_eof = False
        self.raw = frames(bound["role"], bound=bound)

    def send_case(self):
        self.before_input()
        self.sent = True

    def receive(self, phase):
        if phase in admission.PHASES:
            raw = self.raw[len(self.admission_frames)]
            result = controller.native_message(raw, phase)
            self.admission_frames.append(raw)
        else:
            result = controller.native_message(c.canonical({"phase": phase, "version": 1}), phase)
        self.phase = phase
        if self.hook:
            self.hook(phase)
        return result

    def close(self):
        self.closed = True

    def require_admission_ready(self):
        c.require(self.phase in ("READY", "ADMISSION_READY"), "NATIVE_ADMISSION_TRANSPORT")
        self.require_quiet_live(self.phase)

    def require_quiet_live(self, phase):
        c.require(not self.closed and not self.stdout_eof and self.phase == phase, "NATIVE_ADMISSION_TRANSPORT")


class DockerAdmissionTests(unittest.TestCase):
    def fixture(self):
        registry, fake, _, _ = prepared()
        events, streams = [], {}
        def runner(argv, deadline):
            args = argv[3:]
            events.append(args)
            if args[0] == "info":
                return 0, DAEMON, b""
            if args[0] == "version":
                return 0, b'{"Os":"linux","Arch":"arm64"}', b""
            if args[1] == "ls":
                volume = args[args.index("--filter") + 1].removeprefix("volume=")
                return 0, "\n".join(fake.attachments(volume, deadline)).encode(), b""
            self.assertEqual(args[1], "inspect")
            return 0, json.dumps([fake.inspect(args[0], args[2], deadline)]).encode(), b""
        def factory(argv, bound, budget, deadline, **kwargs):
            value = fake.containers[bound["container_id"]]
            value["State"].update(Running=True, Pid=101 if bound["role"] == "target" else 102, Status="running", StartedAt="2026-10-07T00:00:02Z")
            value["RestartCount"] = 0
            stream = FakeStream(bound, deadline, kwargs["before_input"])
            streams[bound["role"]] = stream
            return stream
        backend = db.DockerBackend(registry.scope, {role: "/unused" for role in owned.ACTORS},
            authorize=lambda *_: True, runner=runner, stream_factory=factory)
        return registry, fake, backend, streams, events

    def open(self, registry, backend, role, deadline):
        ref = registry.entries[role]["reference"]
        stream = backend.open_actor(ref, registry.spec(role), registry.snapshot(), None, deadline)
        stream.send_case()
        return stream, ref

    def admit(self, registry, backend, stream, ref, deadline):
        return backend.collect_native_admission(stream, ref, registry.snapshot(), preparation(), context(), deadline)

    def test_native_pair_is_joined_to_running_metadata_and_blocks_early_input(self):
        registry, fake, backend, _, events = self.fixture()
        deadline = Deadline(5)
        target, ref = self.open(registry, backend, "target", deadline)
        result = self.admit(registry, backend, target, ref, deadline)
        self.assertEqual(result["evidence_kind"], "simulated")
        self.assertFalse(result["runtime_evidence"] or result["execution_authorized"])
        self.assertEqual(target.phase, "READY")
        observer, ref = self.open(registry, backend, "observer", deadline)
        self.admit(registry, backend, observer, ref, deadline)
        observer.before_input()
        observer.phase = "ARMED"
        target.before_input()
        self.assertTrue(any(args[0] == "version" for args in events))
        self.assertFalse(any(args[0:2] == ["container", "exec"] for args in events))

    def test_premature_dispatch_invalidates_cached_admission(self):
        registry, _, backend, _, _ = self.fixture()
        deadline = Deadline(5)
        target, ref = self.open(registry, backend, "target", deadline)
        self.admit(registry, backend, target, ref, deadline)
        with self.assertRaisesRegex(c.Invalid, "NATIVE_OBSERVER_NOT_ARMED"):
            target.before_input()
        self.assertNotIn("target", backend._native_admissions)

    def test_no_receipt_cannot_enable_target_or_observer(self):
        for role in ("target", "observer"):
            registry, fake, backend, _, _ = self.fixture()
            deadline = Deadline(5)
            target, _ = self.open(registry, backend, "target", deadline)
            stream = target if role == "target" else self.open(registry, backend, "observer", deadline)[0]
            stream.phase = "READY" if role == "target" else "ADMISSION_READY"
            with self.assertRaisesRegex(c.Invalid, "NATIVE_ADMISSION_REQUIRED"):
                stream.before_input()

    def test_expired_or_lost_peer_cannot_enable_native_input(self):
        for failure in ("expired", "closed"):
            registry, _, backend, _, _ = self.fixture()
            clock = [0.0]
            deadline = Deadline(5, clock=lambda: clock[0])
            target, ref = self.open(registry, backend, "target", deadline)
            self.admit(registry, backend, target, ref, deadline)
            observer, ref = self.open(registry, backend, "observer", deadline)
            self.admit(registry, backend, observer, ref, deadline)
            if failure == "expired":
                clock[0] = 6.0
            else:
                target.close()
            with self.assertRaises(c.Invalid):
                observer.before_input()

    def test_restart_during_collection_and_stale_peer_reject(self):
        for role in ("target", "observer"):
            registry, fake, backend, _, _ = self.fixture()
            deadline = Deadline(5)
            target, target_ref = self.open(registry, backend, "target", deadline)
            if role == "target":
                target.hook = lambda phase: fake.containers[target_ref["id"]]["State"].update(Pid=999) if phase == "CLOCK" else None
                stream, ref = target, target_ref
            else:
                self.admit(registry, backend, target, target_ref, deadline)
                stream, ref = self.open(registry, backend, "observer", deadline)
                fake.containers[target_ref["id"]]["State"]["Pid"] = 999
            with self.assertRaisesRegex(c.Invalid, "NATIVE_ADMISSION_REJECTED"):
                self.admit(registry, backend, stream, ref, deadline)
            self.assertTrue(stream.closed)
            self.assertNotIn(role, backend._native_admissions)

    def test_buffered_frames_cannot_survive_restart_before_collection(self):
        registry, fake, backend, _, _ = self.fixture()
        deadline = Deadline(5)
        stream, ref = self.open(registry, backend, "target", deadline)
        fake.containers[ref["id"]]["State"].update(Pid=999, StartedAt="2026-10-07T00:00:03Z")
        with self.assertRaisesRegex(c.Invalid, "NATIVE_ADMISSION_REJECTED"):
            self.admit(registry, backend, stream, ref, deadline)
        self.assertEqual(stream.admission_frames, [])
        self.assertTrue(stream.closed)

    def test_closed_native_transport_cannot_be_admitted_from_buffered_frames(self):
        registry, _, backend, _, _ = self.fixture()
        deadline = Deadline(5)
        stream, ref = self.open(registry, backend, "target", deadline)
        stream.stdout_eof = True
        with self.assertRaisesRegex(c.Invalid, "NATIVE_ADMISSION_REJECTED"):
            self.admit(registry, backend, stream, ref, deadline)
        self.assertTrue(stream.closed)

    def test_wrong_artifact_scope_authority_and_private_errors_close_stream(self):
        for fail in ("authority", "profile", "frames", "snapshot", "private_error"):
            registry, _, backend, _, _ = self.fixture()
            deadline = Deadline(5)
            stream, ref = self.open(registry, backend, "target", deadline)
            prep, saved = preparation(), registry.snapshot()
            if fail == "authority":
                backend.authorize = None
            elif fail == "profile":
                prep["artifacts"]["target_profile"] = D
            elif fail == "frames":
                stream.raw = changed(stream.raw, 0, invocation_sha256="e" * 64)
            elif fail == "snapshot":
                saved["entries"]["target"]["reference"]["id"] = "e" * 64
            else:
                def error(_):
                    raise OSError("private-canary")
                stream.hook = error
            with self.assertRaisesRegex(c.Invalid, "^NATIVE_ADMISSION_REJECTED$"):
                backend.collect_native_admission(stream, ref, saved, prep, context(), deadline)
            self.assertTrue(stream.closed)
            self.assertNotIn("target", backend._native_admissions)


if __name__ == "__main__":
    unittest.main()
