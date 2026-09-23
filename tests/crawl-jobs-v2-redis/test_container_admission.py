"""Replay actual created-container metadata and reject privilege broadening."""
import copy
import json
from pathlib import Path
import runpy
import unittest

import controller as ctl
import harness as h
import test_execution as fixtures


class ContainerAdmissionTests(unittest.TestCase):
    def setUp(self):
        self.capture = json.loads((Path(__file__).parent / "testdata/created-init-docker-29.json").read_text())
        self.spec = self.capture["expected_spec"]
        self.spec["mounts"] = [tuple(row) for row in self.spec["mounts"]]
        self.observed = self.capture["observed"]
        # The historical capture intentionally omitted command/environment.
        # Fill only those new predicates with labeled synthetic controls; the
        # existing capability/isolation fields remain actual captured metadata.
        self.spec["entrypoint"], self.spec["command"] = ctl.admission.command_for("init")
        self.observed["Config"].update(Entrypoint=self.spec["entrypoint"], Cmd=self.spec["command"], Env=[])
        self.spec["environment_sha256"] = ctl.admission.environment_digest([])

    def test_actual_docker_created_init_and_legacy_spelling_both_pass(self):
        self.assertEqual(self.observed["HostConfig"]["CapAdd"], ["CAP_CHOWN"])
        self.assertNotEqual(self.observed["HostConfig"]["CapAdd"], self.spec["cap_add"])
        ctl.verify_container(self.observed, self.spec, False)
        self.observed["HostConfig"]["CapAdd"] = ["CHOWN"]
        ctl.verify_container(self.observed, self.spec, False)

    def test_missing_duplicate_extra_unknown_and_malformed_caps_reject(self):
        for value in (None, [], ["CHOWN", "CAP_CHOWN"], ["CAP_CHOWN", "CAP_SYS_ADMIN"], ["ALL"],
                      ["CAP_ALL"], ["cap_chown"], ["CAP_CAP_CHOWN"], ["SYS_ADMIN"], "CAP_CHOWN", {"CAP_CHOWN": True}):
            with self.subTest(value=value):
                self.observed["HostConfig"]["CapAdd"] = value
                with self.assertRaises(ctl.ContainerAdmissionError) as rejected:
                    ctl.verify_container(self.observed, self.spec, False)
                self.assertEqual(ctl.failure_details(rejected.exception),
                                 {"code": "ISOLATION", "checks": ["HostConfig.CapAdd"]})

    def test_runtime_roles_cannot_gain_chown_and_cap_drop_remains_exact(self):
        for role in ("redis", "executor", "revocation"):
            spec = ctl.container_spec("test", role, "a" * 32, self.spec["image"], {"control": "c", "data": "d"})
            observed = fixtures.inspected_container(spec)
            for empty in (None, []):
                observed["HostConfig"]["CapAdd"] = empty
                ctl.verify_container(observed, spec, False)
            observed["HostConfig"]["CapAdd"] = ["CAP_CHOWN"]
            with self.assertRaises(ctl.ContainerAdmissionError):
                ctl.verify_container(observed, spec, False)
        for drop in (None, [], ["CAP_ALL"], ["ALL", "CHOWN"]):
            self.observed["HostConfig"]["CapDrop"] = drop
            with self.assertRaises(ctl.ContainerAdmissionError) as rejected:
                ctl.verify_container(self.observed, self.spec, False)
            self.assertIn("HostConfig.CapDrop", rejected.exception.checks)

    def test_other_isolation_failures_still_reject_with_names_only(self):
        changes = {"NetworkMode": "secret-canary", "Memory": 0, "MemorySwap": -1,
                   "PidsLimit": -1, "NanoCpus": 0, "Privileged": True, "ReadonlyRootfs": False,
                   "SecurityOpt": ["secret-canary"], "Binds": ["secret-canary"],
                   "Devices": ["secret-canary"], "IpcMode": "host", "PidMode": "host",
                   "PortBindings": {"secret-canary": []}, "PublishAllPorts": True,
                   "RestartPolicy": {"Name": "always"}, "Tmpfs": {"/": "secret-canary"}}
        for field, value in changes.items():
            observed = copy.deepcopy(self.observed)
            observed["HostConfig"][field] = value
            with self.subTest(field=field), self.assertRaises(ctl.ContainerAdmissionError) as rejected:
                ctl.verify_container(observed, self.spec, False)
            detail = ctl.failure_details(rejected.exception)
            self.assertEqual(detail, {"code": "ISOLATION", "checks": ["HostConfig." + field]})
            self.assertNotIn("secret-canary", json.dumps(detail))

    def test_report_retains_precise_admission_check_without_secret_values(self):
        class BadMetadata(fixtures.FakeDocker):
            def create(self, spec):
                super().create(spec)
                if spec["role"] == "init":
                    self.resources[("container", spec["name"])]["HostConfig"]["CapAdd"] = ["secret-canary"]
        plan, backend = fixtures.test_plan(), BadMetadata()
        report = ctl.execute(plan, fixtures.approval(plan), backend, revision_check=lambda _: None)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertEqual(report["failure_phase"], "init")
        self.assertEqual(report["failure_details"], {"code": "ISOLATION", "checks": ["HostConfig.CapAdd"]})
        self.assertNotIn("secret-canary", json.dumps(report))
        self.assertTrue(all(row["removed"] for row in report["cleanup"]))
        self.assertNotIn("start:init", backend.events)

    def test_malformed_inspection_and_diagnostic_mutation_do_not_leak(self):
        for value in (None, [], {"Config": "secret-canary"}):
            with self.assertRaises(ctl.ContainerAdmissionError) as rejected:
                ctl.verify_container(value, self.spec, False)
            self.assertNotIn("secret-canary", json.dumps(ctl.failure_details(rejected.exception)))
        with self.assertRaises(ValueError):
            ctl.ContainerAdmissionError("ISOLATION", ["secret-canary"])
        error = ctl.ContainerAdmissionError("ISOLATION", ["HostConfig.CapAdd"])
        error.checks = ("secret-canary",)
        self.assertEqual(ctl.failure_details(error), {"code": "UNEXPECTED_FAILURE"})
        for error in (ValueError("secret-canary"), ctl.CommandError("secret-canary"), h.InvalidArtifact("secret-canary")):
            self.assertNotIn("secret-canary", json.dumps(ctl.failure_details(error)))

    def test_image_preparation_checks_all_roles_without_starting_any(self):
        preparation = runpy.run_path(str(h.ROOT / "scripts/prepare-crawl-jobs-v2-images.py"))
        class MetadataBackend(fixtures.FakeDocker):
            def create(self, spec):
                super().create(spec)
                observed = self.resources[("container", spec["name"])]
                observed["State"]["Status"] = "created"
                if spec["role"] == "init":
                    observed["HostConfig"]["CapAdd"] = ["CAP_CHOWN"]
            def start(self, name):
                raise AssertionError("metadata-only check attempted start")
        backend = MetadataBackend()
        backend.deadline = float("inf")
        result = preparation["validate_prestart"](backend, self.spec["image"], "sha256:" + "2" * 64)
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(result["containers_started"], 0)
        self.assertEqual([row["role"] for row in result["roles"]], ["init", "executor", "redis", "revocation"])
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_metadata_preparation_failure_keeps_diagnostic_and_cleans_up(self):
        preparation = runpy.run_path(str(h.ROOT / "scripts/prepare-crawl-jobs-v2-images.py"))
        class InvalidMetadata(fixtures.FakeDocker):
            def create(self, spec):
                super().create(spec)
                self.resources[("container", spec["name"])]["HostConfig"]["CapAdd"] = ["CAP_SYS_ADMIN"]
        backend = InvalidMetadata()
        backend.deadline = float("inf")
        result = preparation["validate_prestart"](backend, self.spec["image"], "sha256:" + "2" * 64)
        self.assertEqual(result["status"], "FAIL")
        self.assertEqual(result["failure_details"], {"code": "ISOLATION", "checks": ["HostConfig.CapAdd"]})
        self.assertTrue(all(row["removed"] for row in result["cleanup"]))
        self.assertFalse(any(event.startswith("start:") for event in backend.events))


if __name__ == "__main__":
    unittest.main()
