"""Independent guard variants and simulated typed-operation compatibility.

No child, Docker binary, daemon socket or native actor may run in these probes.
The sole disk activity besides reports is private temporary profile fixtures
under this review directory, via an explicitly saved TemporaryDirectory class.
"""
from contextlib import ExitStack
import hashlib
import itertools
import json
import os
from pathlib import Path
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
SOURCE = HERE.parent / "source/tests/crawl-jobs-v2-observer"
sys.dont_write_bytecode = True
sys.path.insert(0, str(SOURCE))

import contracts as c
import docker_backend as db
import owned_resources as owned
from profiles import profile
from streams import Deadline, WireBudget
from test_owned_resources import DAEMON, FakeDocker, fixture_scope

REAL_TEMP = tempfile.TemporaryDirectory
ID = "a" * 64
TAIL = ["--entrypoint", "python3", "sha256:" + ID, "-I", "-B", "/oracle/worker.py"]
OPERATIONS = [
    ["container", op] for op in ("inspect", "create", "ls", "kill", "wait", "rm")
] + [["volume", op] for op in ("inspect", "create", "rm")] + [["image", "inspect"]]
SANDBOX = None
REJECTIONS, ACCEPTANCES, TYPED = [], [], []


def file_record(path):
    raw = path.read_bytes()
    return {"bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


class GuardVariants(unittest.TestCase):
    def setUp(self):
        self.fds = len(list(Path("/proc/self/fd").iterdir()))
        self.guards = ExitStack()
        self.mocks = []
        for obj, name in ((db, "verify_cli"), (db.tempfile, "TemporaryDirectory"),
                          (db.subprocess, "Popen"), (db, "read_engine_metadata")):
            self.mocks.append(self.guards.enter_context(patch.object(obj, name,
                side_effect=AssertionError("forbidden side effect: " + name))))

    def tearDown(self):
        try:
            for guard in self.mocks:
                guard.assert_not_called()
            self.assertEqual(len(list(Path("/proc/self/fd").iterdir())), self.fds)
        finally:
            self.guards.close()

    def reject(self, args, *, actor=False):
        argv = [*db.PREFIX, *args]
        with self.assertRaises(c.Invalid) as rejected:
            if actor:
                db.DockerProcessStream(argv, {}, WireBudget(), Deadline(5))
            else:
                db.command(argv, Deadline(5))
        REJECTIONS.append({"test": self._testMethodName, "actor": actor, "args": args,
            "reason": str(rejected.exception)})

    def accept(self, args, *, actor=False):
        db.cli_arguments([*db.PREFIX, *args], actor=actor)
        ACCEPTANCES.append({"test": self._testMethodName, "actor": actor, "args": args})

    def test_01_short_flag_cluster_matrix_rejects_before_side_effects(self):
        alphabet = "hHDcIsB0"
        tokens = {"-" + "".join(parts) for count in (1, 2, 3)
            for parts in itertools.product(alphabet, repeat=count)}
        tokens.update(("-", "-hh=true", "-Dh=false", "-Hunix:///review-unused.sock",
            "-H=unix:///review-unused.sock", "-creview-unused", "-c=review-unused", "-lh", "-\th", "-\nh"))
        for operation in OPERATIONS:
            for token in sorted(tokens):
                with self.subTest(operation=operation, token=token):
                    self.reject([*operation, ID, token])

    def test_02_actor_path_refuses_clustered_and_attached_forms(self):
        for token in ("-hh", "-Dh", "-hD", "-Hvalue", "-cvalue", "-I", "-B", "-"):
            for index in range(5):
                args = ["container", "start", "--attach", "--interactive", ID]
                args.insert(index, token)
                with self.subTest(token=token, index=index):
                    self.reject(args, actor=True)

    def test_03_previous_long_flag_and_operation_restrictions_remain(self):
        for token in ("--help", "--help=true", "--help=false", "--config", "--config=/review-unused",
            "--context", "--context=review-unused", "--host", "--host=unix:///review-unused.sock",
            "help", "__complete", "__completeNoDesc"):
            self.reject(["container", "inspect", ID, token])
            self.reject(["container", "create", token, *TAIL])
        for args in (["info"], ["version"], ["help"], ["buildx", "version"],
                     ["container", "exec", ID], ["container", "run", ID], ["volume", "prune", "--force"]):
            self.reject(args)
        for prefix in (["docker", *db.PREFIX[1:]], [db.DOCKER, "--host", "unix:///review-unused.sock"],
                       [db.DOCKER, "--context", "review-unused"]):
            with self.assertRaises(c.Invalid):
                db.command([*prefix, "container", "inspect", ID], Deadline(5))

    def test_04_exact_oracle_tail_and_native_creates_remain_accepted(self):
        images = ["a" * 64, "0" * 63 + "1", "f" * 64, "abcdef01" * 8]
        for digest in images:
            tail = [*TAIL[:2], "sha256:" + digest, *TAIL[3:]]
            self.accept(["container", "create", *tail])
            self.accept(["container", "create", "--pull", "never", "--read-only", *tail])
        for entrypoint in ("/target", "/observer"):
            self.accept(["container", "create", "--entrypoint", entrypoint, "sha256:" + ID])
        self.accept(["container", "start", "--attach", "--interactive", ID], actor=True)

    def test_05_oracle_exception_cannot_hide_other_short_tokens(self):
        valid = ["container", "create", *TAIL]
        for token in ("-hh", "-Dh", "-Hvalue", "-cvalue", "-I", "-B", "-IB", "-"):
            for index in range(len(valid) + 1):
                args = valid.copy()
                args.insert(index, token)
                with self.subTest(token=token, index=index):
                    self.reject(args)
        for operation in OPERATIONS:
            if operation != ["container", "create"]:
                self.reject([*operation, *TAIL])
        self.reject(["container", "create", *TAIL], actor=True)

    def test_06_malformed_misplaced_and_noncanonical_tails_reject(self):
        tails = []
        for index in range(len(TAIL)):
            tails.append(TAIL[:index] + TAIL[index + 1:])
        for index, values in {
            0: ["--entrypoint=python3", "entrypoint", "--env"],
            1: ["/usr/bin/python3", "python", "python3 ", "python3\n"],
            2: ["image:latest", "sha256:", "sha256:" + "0" * 64, "sha256:" + "A" * 64,
                "sha256:" + "a" * 63, "sha256:" + "a" * 65, "sha256:" + "g" * 64,
                "sha256:" + ID + "\n", "SHA256:" + ID],
            3: ["-IB", "-Ih", "-I=true", "--I"],
            4: ["-Bh", "-B=true", "--B", "-I"],
            5: ["/oracle/other.py", "/oracle/worker.py ", "./oracle/worker.py", "/oracle/worker.py\n"],
        }.items():
            for value in values:
                candidate = TAIL.copy()
                candidate[index] = value
                tails.append(candidate)
        tails.extend(([*TAIL, "extra"], [*TAIL[:3], "-B", "-I", TAIL[-1]],
                      [*TAIL[:3], TAIL[-1], "-I", "-B"], [*TAIL[:3], "--", *TAIL[3:]]))
        for tail in tails:
            with self.subTest(tail=tail):
                self.reject(["container", "create", *tail])

    def test_07_guard_argument_bounds_and_types_still_fail_closed(self):
        valid = [*db.PREFIX, "container", "inspect", ID]
        invalid = [None, "docker", tuple(valid), [], [*valid, ""], [*valid, None],
            [*valid, b"-hh"], [*valid, "a\x00b"], [*valid, "x" * 4097],
            [*valid, *(["x"] * 251)], [*valid, *(["x" * 4096] * 8)]]
        for value in invalid:
            with self.subTest(value_type=type(value).__name__), self.assertRaises(c.Invalid):
                db.command(value, Deadline(5))
        self.accept(["container", "inspect", *([ID] * 251)])
        self.reject(["container", "inspect", *([ID] * 252)])

    def test_08_all_typed_lifecycle_operations_and_roles_for_both_architectures(self):
        for architecture in ("x86_64", "aarch64"):
            scope = fixture_scope(architecture=architecture)
            registry = owned.Registry(scope, lambda _: None)
            fake = FakeDocker(scope)
            calls, actor_calls, local_streams = [], [], []

            def role_for_name(name, roles):
                return next(role for role in roles if owned.name(scope, role) == name)

            def runner(argv, deadline):
                args = argv[3:]
                self.assertEqual(argv[:3], db.PREFIX)
                if args == ["info", "--format", "{{.ID}}"]:
                    return 0, DAEMON, b""
                if args == ["version", "--format", "{{json .Server}}"]:
                    return 0, c.canonical({"Os": "linux", "Arch": c.runtime_architecture(architecture)["image_architecture"]}), b""
                db.cli_arguments(argv)
                calls.append(list(argv))
                kind, action = args[:2]
                if action == "inspect":
                    observed = fake.inspect(kind, args[2], deadline)
                    if observed is None:
                        return 1, b"", f"Error: No such {kind}: {args[2]}".encode()
                    return 0, c.canonical([observed]), b""
                if (kind, action) == ("container", "ls"):
                    volume = args[args.index("--filter") + 1].removeprefix("volume=")
                    return 0, "\n".join(fake.attachments(volume, deadline)).encode(), b""
                if (kind, action) == ("volume", "create"):
                    role = role_for_name(args[-1], owned.VOLUMES)
                    fake.create_volume(registry.spec(role), deadline)
                    return 0, args[-1].encode(), b""
                if (kind, action) == ("container", "create"):
                    role = role_for_name(args[args.index("--name") + 1], owned.ACTORS)
                    container_id = fake.create_container(registry.spec(role), deadline)
                    return 0, container_id.encode(), b""
                references = [row["reference"] for row in registry.entries.values() if row["reference"] is not None]
                ref = next(ref for ref in references if ref["kind"] == kind and (ref.get("id") if kind == "container" else ref["name"]) == args[-1])
                if action == "kill":
                    fake.kill(ref, deadline)
                    return 0, ref["id"].encode(), b""
                if action == "wait":
                    fake.wait(ref, deadline)
                    return 0, b"137", b""
                self.assertEqual(action, "rm")
                fake.remove(ref, deadline)
                return 0, args[-1].encode(), b""

            class FakeActor:
                def __init__(self, bound):
                    self.binding = bound
                    self.closed = False

                def close(self):
                    self.closed = True

            def factory(argv, bound, budget, deadline, **kwargs):
                db.cli_arguments(argv, actor=True)
                actor_calls.append(list(argv))
                fake.containers[bound["container_id"]]["State"].update(Running=True,
                    Pid=101 + owned.ACTORS.index(bound["role"]), Status="running", StartedAt="2026-10-07T00:00:02Z")
                stream = FakeActor(bound)
                local_streams.append(stream)
                return stream

            with REAL_TEMP(prefix="typed-profiles-", dir=SANDBOX) as folder:
                paths = {role: Path(folder) / (role + ".json") for role in owned.ACTORS}
                for role, path in paths.items():
                    path.write_bytes(c.canonical(profile(role, architecture)))
                backend = db.DockerBackend(scope, paths, runner=runner, stream_factory=factory,
                    authorize=lambda *_: True)
                prepared = db.prepare_resources(registry, backend, Deadline(5))
                self.assertFalse(prepared["execution_authorized"])
                self.assertEqual(prepared["containers_started"], 0)
                for role in owned.ACTORS:
                    ref = registry.entries[role]["reference"]
                    backend.open_actor(ref, registry.spec(role), registry.snapshot(), WireBudget(), Deadline(5))
                cleaned = owned.cleanup(registry.snapshot(), backend, Deadline(5))
                self.assertTrue(cleaned["complete"])
                self.assertEqual(cleaned["evidence_kind"], "simulated")
                for stream in local_streams:
                    stream.close()
            self.assertFalse(fake.containers or fake.volumes)
            self.assertEqual(set(tuple(argv[3:5]) for argv in calls), set(map(tuple, OPERATIONS)))
            self.assertEqual(len(actor_calls), 3)
            creates = [argv for argv in calls if argv[3:5] == ["container", "create"]]
            self.assertEqual(len(creates), 3)
            oracle = next(argv for argv in creates if argv[-1] == "/oracle/worker.py")
            self.assertEqual(oracle[-6:], ["--entrypoint", "python3", scope["images"]["oracle"], "-I", "-B", "/oracle/worker.py"])
            TYPED.append({"architecture": architecture, "builtin_calls": calls, "actor_dispatches": actor_calls,
                "builtin_operations_covered": sorted(set(tuple(argv[3:5]) for argv in calls)),
                "roles": list(owned.ACTORS), "runtime_evidence": False, "simulation_only": True})


class RecordedResult(unittest.TextTestResult):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.started, self.passed = [], []

    def startTest(self, test):
        self.started.append(test.id())
        super().startTest(test)

    def addSuccess(self, test):
        self.passed.append(test.id())
        super().addSuccess(test)


def main():
    global SANDBOX
    mode = sys.argv[1]
    assert mode in ("022", "077")
    os.umask(int(mode, 8))
    SANDBOX = HERE / ("sandbox-variants-" + mode)
    SANDBOX.mkdir(mode=0o700)
    start = time.monotonic()
    with (HERE / ("variants-" + mode + ".log")).open("x") as log:
        result = unittest.TextTestRunner(stream=log, verbosity=2, resultclass=RecordedResult).run(
            unittest.defaultTestLoader.loadTestsFromTestCase(GuardVariants))
    value = {"kind": "independent_R1_remediation_variant_controls", "umask": mode,
        "python": sys.version, "resourcewarning_as_error": True, "status": "PASS" if result.wasSuccessful() else "FAIL",
        "elapsed_seconds": round(time.monotonic() - start, 6), "tests_run": result.testsRun,
        "started": result.started, "passed": result.passed,
        "failures": [{"test": test.id(), "traceback": trace} for test, trace in result.failures],
        "errors": [{"test": test.id(), "traceback": trace} for test, trace in result.errors],
        "skipped": [{"test": test.id(), "reason": why} for test, why in result.skipped],
        "guard_rejections": len(REJECTIONS), "guard_acceptances": len(ACCEPTANCES),
        "typed_builtin_calls": sum(len(row["builtin_calls"]) for row in TYPED),
        "typed_actor_dispatches": sum(len(row["actor_dispatches"]) for row in TYPED),
        "all_negative_cases_before_binary_config_spawn_or_metadata": result.wasSuccessful(),
        "source": {"path": str(SOURCE / "docker_backend.py"), **file_record(SOURCE / "docker_backend.py")},
        "probe": file_record(Path(__file__)), "rejections": REJECTIONS,
        "acceptances": ACCEPTANCES, "typed_compatibility": TYPED,
        "limits": ["No subprocess/CLI/daemon/native actor execution in these variant controls",
            "Typed lifecycle actions use in-memory simulated metadata and fake streams only",
            "Profile fixtures written only under this review directory", "Not full-runner/native/CLI-version admission"]}
    with (HERE / ("variants-" + mode + ".json")).open("x") as stream:
        json.dump(value, stream, sort_keys=True, indent=2)
        stream.write("\n")
    print(json.dumps({key: value[key] for key in ("status", "tests_run", "elapsed_seconds", "guard_rejections",
        "guard_acceptances", "typed_builtin_calls", "typed_actor_dispatches", "failures", "errors")}, indent=2))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
