"""Host selection controls: no Docker resources, ptrace or context provisioning."""
import copy
import io
import json
import unittest
from unittest.mock import patch

from contracts import Invalid, canonical, platform, sha
from host_preflight import HostProbe, INFO_FIELDS, VERSION_FIELDS, inspect_host, main, metadata
from runtime_admission import CONTEXT_HASHES, NAMESPACES
from streams import Deadline


class FixtureProbe:
    def __init__(self, architecture="x86_64"):
        self.architecture = architecture
        self.calls = []
        self.kernels = 0
        self.context = {"architecture": architecture, **{key: sha(key.encode()) for key in CONTEXT_HASHES},
            "namespaces": {name: index + 1 for index, name in enumerate(NAMESPACES)},
            "ns_device": 4, "resolution_ns": 1, "yama_scope": 0}
        self.context["kernel_release_sha256"] = sha(b"6.8.0-fixture")
        self.info = {"id": "fixture-daemon", "os": "linux", "architecture": architecture, "version": "29.9.1",
            "kernel": "6.8.0-fixture", "cgroup_version": "2", "cgroup_driver": "systemd",
            "security_options": ["name=seccomp,profile=builtin", "name=apparmor,profile=default", "name=cgroupns"],
            "memory": True, "swap": True, "pids": True, "cpu_period": True, "cpu_quota": True}
        self.server = {"os": "linux", "architecture": platform(architecture)["image_architecture"],
            "version": "29.9.1", "kernel": "6.8.0-fixture"}

    def native(self):
        return {"os": "Linux", "architecture": self.architecture, "pointer_bytes": 8}

    def kernel(self):
        self.kernels += 1
        return copy.deepcopy(self.context)

    def interpreter(self):
        return sha(b"fixture-interpreter")

    def controllers(self):
        return {name: True for name in ("cpu", "memory", "pids")}

    def endpoint(self):
        return {"socket_present": True}

    def docker(self, operation, deadline):
        self.calls.append(operation)
        return copy.deepcopy(self.info if operation == "info" else self.server)

    def collector(self):
        return {"anchor": "PRESENT_UNVERIFIED", "code": "PRESENT_UNVERIFIED"}


class HostPreflightTests(unittest.TestCase):
    def test_both_architectures_are_candidates_never_admission(self):
        for arch in ("x86_64", "aarch64"):
            with self.subTest(architecture=arch):
                probe = FixtureProbe(arch)
                value = inspect_host(arch, probe=probe)
                self.assertEqual(value["status"], "HOST_CANDIDATE")
                self.assertEqual(value["blockers"], [])
                self.assertEqual(probe.calls, ["info", "version", "info"])
                self.assertEqual(probe.kernels, 2)
                self.assertEqual(value["evidence_kind"], "simulated")
                self.assertTrue(value["pending"])
                self.assertFalse(value["host_admitted"] or value["runtime_evidence"] or value["execution_authorized"])
                self.assertEqual(value["native_trials_run"], 0)
                self.assertNotIn(b"fixture-daemon", canonical(value))

    def test_yama_restrictions_are_blockers_for_both_architectures(self):
        for arch in ("aarch64", "x86_64"):
            for scope in (1, 2, 3):
                probe = FixtureProbe(arch)
                probe.context["yama_scope"] = scope
                result = inspect_host(arch, probe=probe)
                self.assertEqual(result["blockers"], ["YAMA_RESTRICTS_C0_SIBLING_PTRACE"])
                self.assertEqual(result["status"], "BLOCKED")

    def test_wrong_native_platform_prevents_docker_queries(self):
        probe = FixtureProbe("x86_64")
        result = inspect_host("aarch64", probe=probe)
        self.assertIn("NATIVE_PLATFORM_MISMATCH", result["blockers"])
        self.assertEqual(probe.calls, [])
        for native in ({"os": "Linux", "architecture": "x86_64", "pointer_bytes": 4}, {"secret": "CANARY"}):
            probe.native = lambda: native
            result = inspect_host("x86_64", probe=probe)
            self.assertIn("NATIVE_PLATFORM_MISMATCH", result["blockers"])
            self.assertNotIn(b"CANARY", canonical(result))

    def test_missing_docker_endpoint_is_not_a_podman_fallback(self):
        probe = FixtureProbe()
        probe.endpoint = lambda: {"socket_present": False}
        result = inspect_host("x86_64", probe=probe)
        self.assertIn("LOCAL_DOCKER_ENDPOINT_MISSING", result["blockers"])
        self.assertEqual(probe.calls, [])

    def test_changed_host_and_daemon_cannot_be_candidates(self):
        for what in ("kernel", "daemon", "in_place_kernel", "in_place_daemon"):
            with self.subTest(what=what):
                probe = FixtureProbe()
                original_kernel, original_docker = probe.kernel, probe.docker
                def kernel():
                    value = original_kernel()
                    if probe.kernels == 2:
                        probe.context["namespaces"]["user_ns"] += 100
                        value["namespaces"]["user_ns"] += 100
                    return probe.context if what == "in_place_kernel" else value
                def docker(operation, deadline):
                    value = original_docker(operation, deadline)
                    if len(probe.calls) == 3:
                        probe.info["id"] = value["id"] = "replacement"
                    return probe.info if what == "in_place_daemon" and operation == "info" else value
                if "kernel" in what:
                    probe.kernel = kernel
                else:
                    probe.docker = docker
                result = inspect_host("x86_64", probe=probe)
                self.assertIn("HOST_OBSERVATION_CHANGED" if "kernel" in what else "DOCKER_UNAVAILABLE", result["blockers"])

    def test_foreign_daemon_architecture_or_kernel_rejects(self):
        for field, value in (("architecture", "arm64"), ("kernel", "foreign-kernel"), ("os", "windows")):
            probe = FixtureProbe()
            probe.server[field] = value
            if field == "kernel":
                probe.info[field] = value
            result = inspect_host("x86_64", probe=probe)
            self.assertEqual(result["status"], "BLOCKED")
            self.assertIn("DOCKER_HOST_KERNEL_MISMATCH" if field == "kernel" else "DOCKER_UNAVAILABLE", result["blockers"])

    def test_limits_confinement_and_namespace_mapping_blockers(self):
        changes = [("cgroup_version", "1"), ("cgroup_driver", "none")]
        changes += [(name, False) for name in ("memory", "swap", "pids", "cpu_period", "cpu_quota")]
        for key, value in changes:
            probe = FixtureProbe()
            probe.info[key] = value
            self.assertIn("DOCKER_RESOURCE_LIMITS_UNSUPPORTED", inspect_host("x86_64", probe=probe)["blockers"])
        for option in ("name=rootless", "name=userns"):
            probe = FixtureProbe()
            probe.info["security_options"].append(option)
            self.assertIn("DOCKER_NAMESPACE_MAPPING_UNSUPPORTED", inspect_host("x86_64", probe=probe)["blockers"])
        probe = FixtureProbe()
        probe.info["security_options"] = []
        self.assertIn("DOCKER_CONFINEMENT_UNSUPPORTED", inspect_host("x86_64", probe=probe)["blockers"])
        probe.controllers = lambda: {"cpu": True, "memory": False, "pids": True}
        self.assertIn("CGROUP_CONTROLLERS_MISSING", inspect_host("x86_64", probe=probe)["blockers"])

    def test_unavailable_observations_are_value_free_and_not_skipped(self):
        for method in ("kernel", "interpreter", "controllers", "endpoint", "docker", "collector"):
            with self.subTest(method=method):
                probe = FixtureProbe()
                def failure(*args):
                    raise OSError("PRIVATE_CANARY")
                setattr(probe, method, failure)
                result = inspect_host("x86_64", probe=probe)
                self.assertEqual(result["status"], "BLOCKED")
                self.assertNotIn(b"PRIVATE_CANARY", canonical(result))

    def test_missing_collector_does_not_issue_an_anchor(self):
        for state in ("MISSING", "UNEXPECTED_TYPE"):
            probe = FixtureProbe()
            probe.collector = lambda: {"anchor": state, "code": "PRESENT_UNVERIFIED"}
            result = inspect_host("x86_64", probe=probe)
            self.assertIn("COLLECTOR_NOT_PROVISIONED", result["blockers"])
            self.assertNotIn("seccomp_filters", result["observations"]["kernel"])
            self.assertFalse(result["host_admitted"])

    def test_typed_metadata_no_coercion_or_unknown_fields(self):
        for key, value in (("memory", 1), ("pids", "true"), ("id", "PRIVATE\nCANARY"), ("version", "CANARY\n"),
                ("cgroup_version", 2), ("security_options", ["name=seccomp,profile=builtin"] * 2), ("unexpected", True)):
            probe = FixtureProbe()
            probe.info[key] = value
            result = inspect_host("x86_64", probe=probe)
            self.assertIn("DOCKER_UNAVAILABLE", result["blockers"])
            self.assertNotIn(b"CANARY", canonical(result))
        probe = FixtureProbe()
        probe.context["yama_scope"] = True
        self.assertIn("KERNEL_UNAVAILABLE", inspect_host("x86_64", probe=probe)["blockers"])

    def test_metadata_bound_duplicates_numbers_and_trailing_data(self):
        for raw in (b'{"os":"linux","os":"linux"}', b'{"os":NaN}', b'{"os":1.0}',
                b'{"os":"linux"} {}', b'x' * 16385, b'{"other":true}', b'[]', b'\xff'):
            with self.assertRaises(Invalid):
                metadata(raw, {"os": "OSType"})
        self.assertEqual(metadata(b'{"os":"linux"}\n', {"os": "OSType"}), {"os": "linux"})

    def test_fixed_docker_reads_exclude_unselected_metadata(self):
        probe = HostProbe("x86_64")
        fixture = FixtureProbe()
        for operation, fields, value in (("info", INFO_FIELDS, fixture.info), ("version", VERSION_FIELDS, fixture.server)):
            document = {source: value[key] for key, source in fields.items()}
            document["HttpProxy"] = "PRIVATE_CANARY"
            with patch("host_preflight.read_engine_metadata", return_value=canonical(document)) as reader:
                deadline = Deadline(1)
                result = probe.docker(operation, deadline)
                self.assertEqual(result, value)
                reader.assert_called_once_with(operation, deadline)
                self.assertNotIn(b"PRIVATE_CANARY", canonical(result))
        with patch("host_preflight.read_engine_metadata") as reader, self.assertRaises(Invalid):
            probe.docker("run", Deadline(1))
        reader.assert_not_called()

    def test_transport_errors_stderr_and_deadline_fail_closed(self):
        fixture = FixtureProbe()
        probe = HostProbe("x86_64")
        # Historical transport-regression ID: arbitrary server diagnostics now
        # arrive as bounded HTTP/JSON bytes, never as a subprocess stderr pipe.
        for response in (b"PRIVATE_CANARY", b'{"error":"warning"}'):
            with patch("host_preflight.read_engine_metadata", return_value=response), self.assertRaises(Invalid):
                probe.docker("info", Deadline(1))
        now = [0]
        deadline = Deadline(1, clock=lambda: now[0])
        original = fixture.docker
        def expired(operation, deadline):
            result = original(operation, deadline)
            now[0] = 2
            return result
        fixture.docker = expired
        result = inspect_host("x86_64", probe=fixture, deadline=deadline)
        self.assertIn("DOCKER_UNAVAILABLE", result["blockers"])
        self.assertEqual(result["status"], "BLOCKED")

    def test_cli_refusal_is_structured_and_nonzero(self):
        probe = FixtureProbe()
        probe.context["yama_scope"] = 1
        value = inspect_host("x86_64", probe=probe)
        output = io.StringIO()
        with patch("host_preflight.inspect_host", return_value=value), patch("sys.stdout", output):
            self.assertEqual(main(["--architecture", "x86_64"]), 1)
        self.assertEqual(json.loads(output.getvalue()), value)
        with self.assertRaises(Invalid):
            inspect_host("amd64", probe=probe)

    def test_wait_timeouts_keep_json_refusal_and_reap_benign_children(self):
        # Preserve COR-01's regression ID. The replacement transport must start
        # zero children: its timeout path now owns only a socket, not a CLI tree.
        for arch in ("x86_64", "aarch64"):
            for error in (TimeoutError("PRIVATE_CANARY"), ConnectionResetError("PRIVATE_CANARY")):
                with self.subTest(architecture=arch, error=type(error).__name__):
                    probe = FixtureProbe(arch)
                    probe.docker = HostProbe(arch).docker
                    def inspected(architecture):
                        return inspect_host(architecture, probe=probe, deadline=Deadline(0.5))
                    output = io.StringIO()
                    with patch("subprocess.Popen", side_effect=AssertionError("subprocess forbidden")) as popen, \
                            patch("engine_metadata.socket.socket", side_effect=error), \
                            patch("host_preflight.inspect_host", side_effect=inspected), patch("sys.stdout", output):
                        self.assertEqual(main(["--architecture", arch]), 1)
                    popen.assert_not_called()
                    result = json.loads(output.getvalue())
                    self.assertEqual(result["status"], "BLOCKED")
                    self.assertIn("DOCKER_UNAVAILABLE", result["blockers"])
                    self.assertNotIn("PRIVATE_CANARY", output.getvalue())

    def test_documented_no_new_privileges_metadata_preserves_other_checks(self):
        for arch in ("x86_64", "aarch64"):
            with self.subTest(architecture=arch):
                probe = FixtureProbe(arch)
                probe.info["security_options"].append("name=no-new-privileges")
                self.assertEqual(inspect_host(arch, probe=probe)["status"], "HOST_CANDIDATE")
                for option in ("name=rootless", "name=userns", "name=no-new-privileges", "name=unknown-hyphenated"):
                    probe.info["security_options"].append(option)
                    result = inspect_host(arch, probe=probe)
                    expected = "DOCKER_NAMESPACE_MAPPING_UNSUPPORTED" if option in ("name=rootless", "name=userns") else "DOCKER_UNAVAILABLE"
                    self.assertIn(expected, result["blockers"])
                    probe.info["security_options"].pop()


if __name__ == "__main__":
    unittest.main()
