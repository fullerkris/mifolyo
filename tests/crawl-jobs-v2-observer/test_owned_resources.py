"""Synthetic Docker metadata and failure-prefix tests; no Docker calls."""
import copy
import json
from pathlib import Path
import tempfile
import unittest

import contracts as c
import docker_backend as db
import owned_resources as owned
from profiles import profile
from streams import Deadline
from watchdog import WatchState, validate_cleanup

DAEMON = b"synthetic-daemon"


def fixture_scope(case="P1.pre.1.1"):
    return owned.scope(case, "1" * 32,
        {role: "sha256:" + str(i + 1) * 64 for i, role in enumerate(owned.ACTORS)},
        {role: c.sha(c.canonical([])) for role in owned.ACTORS}, c.sha(DAEMON))


def volume_metadata(spec):
    return {"Name": spec["name"], "Labels": spec["labels"], "Driver": "local", "Scope": "local",
        "Options": dict(owned.TMPFS_OPTIONS), "CreatedAt": "2026-10-06T00:00:00Z", "Mountpoint": "/synthetic/" + spec["name"]}


def container_metadata(spec, volume_metadata_by_name):
    mounts, declared = [], []
    for row in spec["mounts"]:
        mounts.append({"Type": "volume", "Name": row["name"], "Driver": "local", "Source": volume_metadata_by_name[row["name"]]["Mountpoint"],
            "Destination": row["destination"], "RW": not row["read_only"], "Propagation": ""})
        declared.append({"Type": "volume", "Source": row["name"], "Target": row["destination"], "ReadOnly": row["read_only"], "VolumeOptions": {"NoCopy": True}})
    return {"Id": c.sha(spec["name"].encode()), "Name": "/" + spec["name"], "Created": "2026-10-06T00:00:01Z", "Image": spec["image"],
        "Config": {"Image": spec["image"], "User": "999:999", "Labels": spec["labels"], "Entrypoint": spec["entrypoint"],
            "Cmd": spec["command"], "WorkingDir": "/", "Env": [], "Tty": False, "OpenStdin": True,
            "AttachStdin": True, "AttachStdout": True, "AttachStderr": True, "ExposedPorts": None, "Volumes": None, "Healthcheck": {"Test": ["NONE"]}},
        "HostConfig": {"NetworkMode": "none", "ReadonlyRootfs": True, "Privileged": False, "PidMode": spec["pid_mode"],
            "IpcMode": "private", "CgroupnsMode": "private", "UsernsMode": "", "UTSMode": "", "Init": False,
            "CapDrop": ["ALL"], "CapAdd": None, "Memory": spec["memory"], "MemorySwap": spec["memory"],
            "NanoCpus": 1000000000, "PidsLimit": spec["pids_limit"], "RestartPolicy": {"Name": "no", "MaximumRetryCount": 0},
            "AutoRemove": False, "LogConfig": {"Type": "none", "Config": {}}, "Ulimits": [{"Name": "core", "Hard": 0, "Soft": 0}],
            "PublishAllPorts": False, "Runtime": "runc", "SecurityOpt": ["no-new-privileges:true", "seccomp=" + json.dumps(profile(spec["role"]))],
            "Mounts": declared},
        "Mounts": mounts, "State": {"Running": False, "Pid": 0, "Status": "created", "ExitCode": 0,
            "Paused": False, "Restarting": False, "Dead": False, "OOMKilled": False}}


class FakeDocker:
    evidence_kind = "simulated"

    def __init__(self, value):
        self.scope, self.volumes, self.containers, self.events = value, {}, {}, []
        self.fail_create = None
        self.fail_inspect = None
        self.foreign_attachments = {}

    def verify_platform(self, deadline):
        deadline.check()
        return {"runtime_feasibility_proven": False}

    def inspect(self, kind, selector, deadline):
        deadline.check()
        if self.fail_inspect == selector:
            raise c.Invalid("INSPECTION_FAILED")
        if kind == "volume":
            return copy.deepcopy(self.volumes.get(selector))
        rows = [value for value in self.containers.values() if value["Id"] == selector or value["Name"] == "/" + selector]
        return copy.deepcopy(rows[0]) if rows else None

    def create_volume(self, spec, deadline):
        deadline.check()
        self.events.append(("create_volume", spec["role"]))
        self.volumes[spec["name"]] = volume_metadata(spec)
        if self.fail_create == spec["role"]:
            raise c.Invalid("AMBIGUOUS_CREATE")

    def create_container(self, spec, deadline):
        deadline.check()
        self.events.append(("create_container", spec["role"]))
        row = container_metadata(spec, self.volumes)
        self.containers[row["Id"]] = row
        if self.fail_create == spec["role"]:
            raise c.Invalid("AMBIGUOUS_CREATE")
        return row["Id"]

    def attachments(self, volume, deadline):
        deadline.check()
        return sorted([row["Id"] for row in self.containers.values() if any(item["Name"] == volume for item in row["Mounts"])] + self.foreign_attachments.get(volume, []))

    def kill(self, ref, deadline):
        deadline.check()
        self.events.append(("kill", ref["role"]))
        self.containers[ref["id"]]["State"].update(Running=False, Pid=0, Status="exited", ExitCode=137)

    def wait(self, ref, deadline):
        deadline.check()
        self.events.append(("wait", ref["role"]))

    def remove(self, ref, deadline):
        deadline.check()
        self.events.append(("remove", ref["role"]))
        if ref["kind"] == "container":
            assert self.containers[ref["id"]]["State"]["Pid"] == 0
            del self.containers[ref["id"]]
        else:
            assert self.attachments(ref["name"], deadline) == []
            del self.volumes[ref["name"]]


def prepared():
    value = fixture_scope()
    history = []
    registry = owned.Registry(value, lambda row: history.append(copy.deepcopy(row)))
    backend = FakeDocker(value)
    result = db.prepare_resources(registry, backend, Deadline(5))
    return registry, backend, history, result


class OwnedResourceTests(unittest.TestCase):
    def test_exact_stopped_roles_and_oracle_survives_target_namespace(self):
        registry, backend, history, result = prepared()
        self.assertEqual(result["containers_started"], 0)
        self.assertEqual(result["runtime_process_admission"], "pending")
        self.assertEqual(len(history), 11)
        self.assertEqual(registry.spec("target")["pid_mode"], "")
        self.assertEqual(registry.spec("oracle")["pid_mode"], "")
        self.assertEqual(registry.spec("observer")["pid_mode"], "container:" + registry.entries["target"]["reference"]["id"])
        self.assertEqual(registry.spec("observer")["mounts"], [])
        self.assertTrue(all(mount["read_only"] for mount in registry.spec("oracle")["mounts"]))
        self.assertEqual(set(backend.volumes), {owned.name(registry.scope, role) for role in owned.VOLUMES})
        state = WatchState(registry.scope)
        for snapshot in history:
            state.accept(snapshot)
        self.assertEqual(state.current, registry.snapshot())

    def test_ownership_cleanup_orders_actor_quiescence_before_volumes(self):
        registry, backend, _, _ = prepared()
        for i, row in enumerate(backend.containers.values()):
            row["State"].update(Running=True, Pid=100 + i, Status="running")
        result = owned.cleanup(registry.snapshot(), backend, Deadline(5))
        validate_cleanup(result, registry.snapshot())
        self.assertTrue(result["complete"])
        self.assertEqual(result["evidence_kind"], "simulated")
        self.assertFalse(backend.containers or backend.volumes)
        self.assertEqual([role for event, role in backend.events if event == "remove"], ["observer", "oracle", "target", "witness", "control"])
        for role in owned.ACTORS:
            self.assertLess(backend.events.index(("kill", role)), backend.events.index(("wait", role)))
            self.assertLess(backend.events.index(("wait", role)), backend.events.index(("remove", role)))

    def test_every_ambiguous_create_is_once_and_recovered_only_in_cleanup(self):
        for role in (*owned.VOLUMES, *owned.ACTORS):
            value = fixture_scope()
            registry = owned.Registry(value, lambda _: None)
            backend = FakeDocker(value)
            backend.fail_create = role
            with self.subTest(role=role), self.assertRaises(c.Invalid):
                db.prepare_resources(registry, backend, Deadline(5))
            self.assertTrue(registry.failed)
            self.assertEqual(sum(action.startswith("create") and item == role for action, item in backend.events), 1)
            result = owned.cleanup(registry.snapshot(), backend, Deadline(5))
            self.assertFalse(result["complete"])
            self.assertEqual([row["role"] for row in result["resources"] if not row["creation_settled"]], [role])
            self.assertFalse(backend.containers or backend.volumes)

    def test_journal_failure_prevents_mutation_and_poisons_registry(self):
        calls = []
        def publish(snapshot):
            calls.append(snapshot["generation"])
            if snapshot["generation"] == 1:
                raise OSError("private-canary")
        value = fixture_scope()
        registry = owned.Registry(value, publish)
        backend = FakeDocker(value)
        with self.assertRaises(OSError):
            db.prepare_resources(registry, backend, Deadline(5))
        self.assertEqual(backend.events, [])
        with self.assertRaises(c.Invalid):
            registry.candidate("witness")

    def test_foreign_attachment_blocks_only_affected_volume(self):
        registry, backend, _, _ = prepared()
        volume = owned.name(registry.scope, "witness")
        backend.foreign_attachments[volume] = ["f" * 64]
        with self.assertRaises(c.Invalid):
            db.verify_attachments(registry, backend, Deadline(5))
        result = owned.cleanup(registry.snapshot(), backend, Deadline(5))
        self.assertFalse(result["complete"])
        self.assertFalse(backend.containers)
        self.assertEqual(set(backend.volumes), {volume})
        self.assertEqual(result["resources"][-1]["result"], "removed_and_absent")

    def test_replaced_container_is_not_removed_by_name(self):
        registry, backend, _, _ = prepared()
        ref = registry.entries["observer"]["reference"]
        row = backend.containers.pop(ref["id"])
        row["Id"] = "f" * 64
        backend.containers[row["Id"]] = row
        result = owned.cleanup(registry.snapshot(), backend, Deadline(5))
        self.assertFalse(result["complete"])
        self.assertEqual(set(backend.containers), {"f" * 64})
        self.assertNotIn(("remove", "observer"), backend.events)

    def test_admission_failure_does_not_prevent_owned_created_resource_cleanup(self):
        registry, backend, _, _ = prepared()
        ref = registry.entries["target"]["reference"]
        backend.containers[ref["id"]]["HostConfig"]["Privileged"] = True
        with self.assertRaises(c.Invalid):
            owned.admit_container(backend.containers[ref["id"]], registry.spec("target"),
                {role: registry.entries[role]["reference"] for role in owned.VOLUMES})
        self.assertTrue(owned.cleanup(registry.snapshot(), backend, Deadline(5))["complete"])

    def test_inspection_error_and_volume_replacement_never_mean_absence(self):
        for failure in ("inspect", "replace"):
            registry, backend, _, _ = prepared()
            volume = owned.name(registry.scope, "witness")
            if failure == "inspect":
                backend.fail_inspect = volume
            else:
                backend.volumes[volume]["CreatedAt"] = "2026-10-06T00:00:02Z"
            result = owned.cleanup(registry.snapshot(), backend, Deadline(5))
            self.assertFalse(result["complete"])
            self.assertIn(volume, backend.volumes)

    def test_full_admission_rejects_authority_expansion_and_coercions(self):
        registry, backend, _, _ = prepared()
        spec = registry.spec("target")
        original = backend.containers[registry.entries["target"]["reference"]["id"]]
        volumes = {role: registry.entries[role]["reference"] for role in owned.VOLUMES}
        changes = [lambda x: x["HostConfig"].update(Privileged=True), lambda x: x["HostConfig"].update(PidMode="host"),
            lambda x: x["HostConfig"].update(UsernsMode="host"), lambda x: x["HostConfig"].update(CapAdd=["SYS_PTRACE"]),
            lambda x: x["HostConfig"].update(Memory=True), lambda x: x["HostConfig"]["RestartPolicy"].update(MaximumRetryCount=False),
            lambda x: x["HostConfig"]["Ulimits"][0].update(Hard=False), lambda x: x["Config"].update(Env=False),
            lambda x: x["Config"].update(Tty=True), lambda x: x["HostConfig"]["Mounts"][0]["VolumeOptions"].update(NoCopy=False),
            lambda x: x["HostConfig"]["Mounts"][0]["VolumeOptions"].update(DriverConfig={"Name": "foreign"}),
            lambda x: x["Mounts"][0].update(Type="bind"), lambda x: x["HostConfig"].update(SecurityOpt=["seccomp=unconfined", "no-new-privileges"]),
            lambda x: x["State"].update(Running=True, Pid=42), lambda x: x["Config"].update(Entrypoint=["/bin/sh"])]
        changes += [lambda x: x["Config"].update(Healthcheck={"Test": ["CMD-SHELL", "private-canary"]}),
            lambda x: x["State"].update(Health={"Status": "healthy"})]
        for index, change in enumerate(changes):
            changed = copy.deepcopy(original)
            change(changed)
            with self.subTest(index=index), self.assertRaises(c.Invalid):
                owned.admit_container(changed, spec, volumes)

    def test_scope_and_snapshot_cannot_inject_roles_or_skip_bindings(self):
        registry, _, history, _ = prepared()
        for changed in (dict(registry.scope, execution_authorized=True), dict(registry.scope, invocation_id="../escape")):
            with self.assertRaises(c.Invalid):
                owned.validate_scope(changed)
        changed = copy.deepcopy(history[1])
        changed["entries"] = {"observer": {"reference": None}}
        with self.assertRaises(c.Invalid):
            owned.validate_snapshot(changed)
        state = WatchState(registry.scope)
        for value in history:
            state.accept(value)
        with self.assertRaises(c.Invalid):
            state.accept(history[-1])
        changed = copy.deepcopy(history[-1])
        changed["entries"]["target"]["reference"]["id"] = "e" * 64
        changed["generation"] += 1
        with self.assertRaises(c.Invalid):
            state.accept(changed)


class DockerAdapterTests(unittest.TestCase):
    def adapter(self, registry, fake, calls, starts=None):
        def runner(argv, deadline):
            calls.append(argv)
            args = argv[3:]
            if args[0] == "info":
                return 0, DAEMON + b"\n", b""
            kind, operation = args[:2]
            if operation == "inspect":
                value = fake.inspect(kind, args[2], deadline)
                if value is None:
                    return 1, b"[]", ("Error: No such " + kind + ": " + args[2]).encode()
                return 0, json.dumps([value]).encode(), b""
            if operation == "ls":
                volume = args[args.index("--filter") + 1].removeprefix("volume=")
                return 0, ("\n".join(fake.attachments(volume, deadline)) + "\n").encode() if fake.attachments(volume, deadline) else b"", b""
            ref = next(row["reference"] for row in registry.entries.values() if row["reference"] and
                (row["reference"].get("id") == args[-1] or row["reference"]["name"] == args[-1]))
            if operation == "wait":
                if len(args) != 3:
                    return 125, b"", b"unknown flag: --condition"
                fake.wait(ref, deadline)
                return 0, b"0\n", b""
            if operation == "kill":
                fake.kill(ref, deadline)
            elif operation == "rm":
                fake.remove(ref, deadline)
            else:
                raise AssertionError("unexpected synthetic CLI operation")
            return 0, b"", b""
        options = {"authorize": lambda *_: True, "runner": runner}
        if starts is not None:
            options["stream_factory"] = lambda *args, **kwargs: starts.append((args, kwargs))
        return db.DockerBackend(registry.scope, {role: "/not-used" for role in owned.ACTORS}, **options)

    def test_cli_cleanup_uses_supported_wait_grammar_and_removes_all_resources(self):
        registry, fake, _, _ = prepared()
        for i, row in enumerate(fake.containers.values()):
            row["State"].update(Running=True, Pid=i + 100, Status="running")
        calls = []
        backend = self.adapter(registry, fake, calls)
        result = owned.cleanup(registry.snapshot(), backend, Deadline(5))
        self.assertTrue(result["complete"])
        self.assertEqual(result["evidence_kind"], "simulated")
        self.assertFalse(fake.containers or fake.volumes)
        waits = [argv[3:] for argv in calls if argv[3:5] == ["container", "wait"]]
        self.assertEqual(len(waits), 3)
        self.assertTrue(all(len(argv) == 3 and "--condition" not in argv for argv in waits))

    def test_start_rechecks_acknowledged_attachment_inventory(self):
        registry, fake, _, _ = prepared()
        fake.foreign_attachments[owned.name(registry.scope, "witness")] = ["f" * 64]
        calls, starts = [], []
        backend = self.adapter(registry, fake, calls, starts)
        with self.assertRaisesRegex(c.Invalid, "FOREIGN_ATTACHMENT"):
            backend.open_actor(registry.entries["target"]["reference"], registry.spec("target"), registry.snapshot(), None, Deadline(5))
        self.assertFalse(starts)
        self.assertTrue(any("ls" in argv for argv in calls))

    def test_default_adapter_has_no_mutation_authority(self):
        calls = []
        value = fixture_scope()
        backend = db.DockerBackend(value, {role: "/not-used" for role in owned.ACTORS}, runner=lambda *args: calls.append(args))
        with self.assertRaisesRegex(c.Invalid, "EXECUTION_NOT_APPROVED"):
            backend.create_volume(owned.volume_spec(value, "control"), Deadline(1))
        self.assertEqual(calls, [])

    def test_inspect_recognizes_only_exact_absence_errors(self):
        value = fixture_scope()
        selector = owned.name(value, "target")
        for error, missing in ((f"Error: No such object: {selector}", True), ("permission denied", False),
                               (f"Error: No such object: {selector}-other", False)):
            def runner(argv, _, error=error):
                return (0, DAEMON + b"\n", b"") if argv[3] == "info" else (1, b"[]", error.encode())
            backend = db.DockerBackend(value, {role: "/not-used" for role in owned.ACTORS}, runner=runner)
            if missing:
                self.assertIsNone(backend.inspect("container", selector, Deadline(1)))
            else:
                with self.assertRaisesRegex(c.Invalid, "INSPECTION_FAILED"):
                    backend.inspect("container", selector, Deadline(1))

    def test_daemon_swap_cannot_produce_a_false_absence(self):
        value = fixture_scope()
        calls = []
        def runner(argv, _):
            calls.append(argv)
            if argv[3] == "info":
                return 0, DAEMON if len(calls) == 1 else b"replacement-daemon", b""
            return 1, b"[]", ("Error: No such object: " + owned.name(value, "target")).encode()
        backend = db.DockerBackend(value, {role: "/not-used" for role in owned.ACTORS}, runner=runner)
        with self.assertRaisesRegex(c.Invalid, "DAEMON_IDENTITY"):
            backend.inspect("container", owned.name(value, "target"), Deadline(1))

    def test_foreign_reference_and_daemon_drift_refuse_cleanup_dispatch(self):
        registry, fake, _, _ = prepared()
        ref = registry.entries["target"]["reference"]
        calls = []
        def runner(argv, _):
            calls.append(argv)
            if argv[3] == "info":
                return 0, DAEMON + b"\n", b""
            row = copy.deepcopy(fake.containers[ref["id"]])
            row["Config"]["Labels"][owned.KEY + "scope"] = "e" * 64
            return 0, json.dumps([row]).encode(), b""
        backend = db.DockerBackend(registry.scope, {role: "/not-used" for role in owned.ACTORS}, authorize=lambda *_: True, runner=runner)
        with self.assertRaisesRegex(c.Invalid, "CONTAINER_OWNER"):
            backend.kill(ref, Deadline(1))
        self.assertFalse(any("kill" in argv for argv in calls))
        backend.runner = lambda *_: (0, b"different-daemon", b"")
        with self.assertRaisesRegex(c.Invalid, "DAEMON_IDENTITY"):
            backend.kill(ref, Deadline(1))

    def test_generated_create_argv_is_closed_and_profile_bound(self):
        value = fixture_scope()
        calls = []
        def runner(argv, _):
            calls.append(argv)
            if argv[3] == "info":
                return 0, DAEMON + b"\n", b""
            if argv[3:5] == ["image", "inspect"]:
                return 0, json.dumps([{"Id": value["images"]["target"], "Os": "linux", "Architecture": "arm64", "Config": {"Env": [], "OnBuild": None, "Volumes": None}}]).encode(), b""
            return 0, ("a" * 64 + "\n").encode(), b""
        with tempfile.TemporaryDirectory() as directory:
            paths = {}
            for role in owned.ACTORS:
                path = Path(directory) / (role + ".json")
                path.write_bytes(c.canonical(profile(role)))
                paths[role] = path
            backend = db.DockerBackend(value, paths, authorize=lambda *_: True, runner=runner)
            self.assertEqual(backend.create_container(owned.container_spec(value, "target"), Deadline(2)), "a" * 64)
            argv = calls[-1]
            self.assertEqual(argv[:3], ["docker", "--host", "unix:///var/run/docker.sock"])
            self.assertIn("--read-only", argv)
            self.assertIn("core=0:0", argv)
            self.assertIn("--no-healthcheck", argv)
            self.assertNotIn("--privileged", argv)
            self.assertNotIn("--cap-add", argv)
            self.assertNotIn("--force", argv)
            self.assertEqual(argv[argv.index("--network") + 1], "none")
            paths["target"].write_bytes(b"{}\n")
            with self.assertRaisesRegex(c.Invalid, "PROFILE_FILE_CONTENT"):
                backend.create_container(owned.container_spec(value, "target"), Deadline(2))

    def test_image_healthcheck_rejects_before_create(self):
        value = fixture_scope()
        calls = []
        def runner(argv, _):
            calls.append(argv)
            if argv[3] == "info":
                return 0, DAEMON, b""
            return 0, json.dumps([{"Id": value["images"]["target"], "Os": "linux", "Architecture": "arm64",
                "Config": {"Env": [], "Volumes": None, "OnBuild": None, "Healthcheck": {"Test": ["CMD-SHELL", "private-canary"]}}}]).encode(), b""
        backend = db.DockerBackend(value, {role: "/not-used" for role in owned.ACTORS}, authorize=lambda *_: True, runner=runner)
        with self.assertRaisesRegex(c.Invalid, "HEALTHCHECK"):
            backend.create_container(owned.container_spec(value, "target"), Deadline(2))
        self.assertFalse(any("create" in argv for argv in calls))


if __name__ == "__main__":
    unittest.main()
