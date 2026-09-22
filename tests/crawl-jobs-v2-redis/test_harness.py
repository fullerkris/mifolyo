"""Offline adversarial controls and independent Python/Go wire interoperability.

Unit-test image identities below are fabricated lexical controls. They are
never claimed to be actual OCI images and cannot authorize an executor.
"""
from __future__ import annotations

import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import runpy
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("m4_offline_harness", HERE / "harness.py")
h = importlib.util.module_from_spec(spec)
spec.loader.exec_module(h)


def inputs(scenario="ledger-smoke"):
    return {"format_version": 1, "scenario": scenario, "redis_version": "7.2.0",
            **{name: "sha256:" + hashlib.sha256(("unit-test-only:" + name).encode()).hexdigest()
               for name in ("redis_image", "harness_image", "standin_image")}}


class OfflineHarnessTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.ledger = h.compile_plan(inputs())
        cls.verifier = runpy.run_path(str(h.ROOT / "scripts/verify-crawl-jobs-v2-digests.py"))

    def test_deterministic_plan_and_descriptor_provenance(self):
        self.assertEqual(h.canonical(self.ledger), h.canonical(h.compile_plan(inputs())))
        self.assertFalse(self.ledger["execution_authorized"])
        self.assertFalse(self.ledger["release_eligible"])
        self.assertEqual(self.ledger["image_verification"], "pending")
        for field, item in self.ledger["test_descriptors"].items():
            descriptor = item["artifact"]
            self.assertEqual(descriptor["evidence_field"], field)
            self.assertEqual(descriptor["measurement_status"], "not_measured")
            self.assertEqual(item["sha256"], hashlib.sha256(h.canonical(descriptor)).hexdigest())
            self.assertNotEqual(item["sha256"], "0" * 64)
            self.assertEqual(dict(self.ledger["guard_core"]["fields"])[field], item["sha256"])

    def test_guard_chain_matches_independent_python_oracle_for_each_profile(self):
        for scenario in h.SCENARIOS:
            with self.subTest(scenario=scenario):
                plan = h.compile_plan(inputs(scenario))
                core = dict(plan["guard_core"]["fields"])
                compatibility = dict(plan["compatibility"]["fields"])
                data = {"contract_case": "canonical", "guard_mode": "production",
                        "guard_core": {key: core[key] for key in ("redis_version", "redis_config_sha256",
                            *h.EVIDENCE_FIELDS, "cutover_mode", "candidate_run_id")},
                        "compatibility": {key: compatibility[key] for key in
                            ("redis_config_sha256", *h.IMAGE_FIELDS, "render_worker_image")},
                        "approved_at_ms": 1000}
                result = self.verifier["guard_chain_result"](data, {"canonical": plan["identities"]})
                self.assertEqual(result["guard_core_sha256"], plan["guard_core"]["sha256"])
                self.assertEqual(result["compatibility_manifest_sha256"], plan["compatibility"]["sha256"])
                self.assertEqual(result["compatibility_marker_sha256"], plan["compatibility_marker"]["sha256"])
                # The oracle's historical 'production' label means structural
                # nonzero validation, not release provenance or execution approval.
                data["guard_core"]["memory_fixture_sha256"] = "0" * 64
                with self.assertRaises(self.verifier["Rejection"]):
                    self.verifier["guard_chain_result"](data, {"canonical": plan["identities"]})

    def test_inputs_are_closed_and_values_are_not_coerced(self):
        changes = [("format_version", True), ("format_version", 2), ("scenario", "production"),
                   ("scenario", []), ("redis_version", "8.0.0"), ("redis_version", "7.2.x"),
                   ("redis_version", "7." + "1" * 64), ("redis_image", "redis:7"),
                   ("harness_image", "sha256:" + "0" * 64), ("standin_image", "sha256:" + "A" * 64),
                   ("marker", {}), ("endpoint", "redis://retained"), ("render_worker_image", "enabled")]
        for field, value in changes:
            with self.subTest(field=field, value=value):
                changed = inputs()
                changed[field] = value
                with self.assertRaises(h.InvalidArtifact):
                    h.compile_plan(changed)
        for field in inputs():
            changed = inputs()
            del changed[field]
            with self.assertRaises(h.InvalidArtifact):
                h.compile_plan(changed)

    def test_plan_validation_recomputes_all_bytes(self):
        for field, value in (("execution_authorized", True), ("execution_authorized", 0),
                             ("release_eligible", True), ("profile", "administrative"),
                             ("image_verification", "PASS"), ("extra", "secret-canary")):
            with self.subTest(field=field):
                changed = copy.deepcopy(self.ledger)
                changed[field] = value
                with self.assertRaises(h.InvalidArtifact):
                    h.validate_plan(changed)
        for key in ("guard_core", "compatibility", "compatibility_marker"):
            changed = copy.deepcopy(self.ledger)
            changed[key]["fields"].reverse()
            with self.assertRaises(h.InvalidArtifact):
                h.validate_plan(changed)
        changed = copy.deepcopy(self.ledger)
        descriptor = changed["test_descriptors"]["memory_fixture_sha256"]
        descriptor["artifact"]["measurement_status"] = "PASS"
        descriptor["sha256"] = h.digest(h.canonical(descriptor["artifact"]))
        with self.assertRaises(h.InvalidArtifact):
            h.validate_plan(changed)

    def test_stale_contract_or_source_pins_fail(self):
        actual = Path.read_bytes
        for name in ("script_bundle_generated.go", "cj2_approve_boot.lua", "crawl-jobs-v2.md"):
            def changed(path, target=name):
                raw = actual(path)
                return raw + b"\n" if path.name == target else raw
            with self.subTest(name=name), patch.object(Path, "read_bytes", changed):
                with self.assertRaises(h.InvalidArtifact):
                    h.source_identity()

    def test_setup_projection_is_exact_and_does_not_claim_live_observation(self):
        setup = h.ledger_setup(self.ledger, 1000)
        h.validate_setup(self.ledger, setup)
        self.assertFalse(setup["time_observation_verified"])
        self.assertFalse(setup["execution_authorized"])
        self.assertEqual({row["key"] for row in setup["writes"]}, {h.AUTH[i] for i in (1, 2, 5, 6)})
        self.assertNotIn(h.AUTH[0], {row["key"] for row in setup["writes"]})
        mutations = []
        for transform in (
            lambda s: s["writes"].append(copy.deepcopy(s["writes"][0])),
            lambda s: s["writes"].pop(),
            lambda s: s["writes"][0].update(key="signal_queue"),
            lambda s: s["writes"][0].update(type="list"),
            lambda s: s["writes"][0].update(expiry=1001),
            lambda s: s["required_absent"].pop(),
            lambda s: s.update(time_observation_verified=True),
            lambda s: s.update(plan_sha256="a" * 64),
        ):
            changed = copy.deepcopy(setup)
            transform(changed)
            mutations.append(changed)
        for changed in mutations:
            with self.assertRaises(h.InvalidArtifact):
                h.validate_setup(self.ledger, changed)
        for time in (0, -1, True, 1.5, "1000", h.MAX_EXACT + 1):
            with self.assertRaises(h.InvalidArtifact):
                h.ledger_setup(self.ledger, time)
        for scenario in ("administrative-fresh", "administrative-migration"):
            with self.assertRaises(h.InvalidArtifact):
                h.ledger_setup(h.compile_plan(inputs(scenario)), 1000)

    def test_zero_setup_guard_cannot_be_laundered_by_rehashing(self):
        changed = h.ledger_setup(self.ledger, 1000)
        fields = changed["stored_guard"]["fields"]
        for field in fields:
            if field[0] == "memory_fixture_sha256":
                field[1] = "0" * 64
        changed["stored_guard"] = h.packed(fields)
        with self.assertRaises(h.InvalidArtifact):
            h.validate_setup(self.ledger, changed)

    def test_acl_outer_selector_never_grants_inner_authority_writes(self):
        data = (h.P + "stage_slots",)
        selectors = h.ledger_acl_selectors(h.AUTH + data, data, data)
        self.assertIn("+evalsha", selectors[1])
        self.assertNotIn("+hset", selectors[1])
        for selector in selectors:
            if "+hset" in selector:
                for key in h.AUTH:
                    self.assertNotIn("~" + key + " ", selector + " ")
            if any("~" + key in selector for key in h.ABSENCE_ONLY):
                self.assertTrue(selector.startswith("(+evalsha ") or selector.startswith("(+type "))
        for bad in (*h.AUTH, *h.LEGACY, "*", "foo?", "foo[ab]", "foo\\bar", "foo +@all", "foo\nbar"):
            with self.subTest(bad=bad), self.assertRaises(h.InvalidArtifact):
                h.ledger_acl_selectors(h.AUTH + data, (bad,), (bad,))
        with self.assertRaises(h.InvalidArtifact):
            h.ledger_acl_selectors(h.AUTH, data, (h.P + "runs",))

    def test_inventory_has_no_fabricated_acceptance(self):
        rows = self.ledger["operation_inventory"]
        self.assertEqual(len(rows), 52)
        self.assertEqual(len({row["operation"] for row in rows}), 43)
        self.assertTrue(all(row["execution_status"] == "not_run" for row in rows))
        requirements = h.requirement_inventory()
        self.assertEqual({row["section"] for row in requirements}, {"17", *(f"17.{i}" for i in range(1, 8))})
        self.assertTrue(all(row["status"] == "unimplemented" and not row["evidence"] for row in requirements))
        wording = "\n".join(row["requirement"] for row in requirements)
        self.assertIn("Authoritative script tests MUST use real Redis 7", wording)
        self.assertIn("Using a disposable Redis configured exactly as section 2 requires", wording)

    def test_json_file_and_cli_fail_closed_without_leaking_values(self):
        for raw in (b'{"a":1,"a":2}', b'{"a":NaN}', b'{"a":1.0}', b'[]', b'\xff',
                    b'{"a":"\\ud800"}', b" " * (h.MAX_ARTIFACT_BYTES + 1)):
            with self.assertRaises(h.InvalidArtifact):
                h.decode(raw)
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "inputs.json"
            raw = h.canonical(inputs())
            path.write_bytes(raw)
            command = [sys.executable, "-B", str(HERE / "harness.py"), "prepare", "--inputs", str(path),
                       "--expected-input-sha256", h.digest(raw)]
            result = subprocess.run(command, capture_output=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(h.decode(result.stdout), self.ledger)
            path.write_bytes(b'{"secret-canary":"must-not-appear"}')
            result = subprocess.run(command, capture_output=True, timeout=30)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(result.stdout, b"")
            self.assertNotIn(b"secret-canary", result.stderr)
            self.assertNotIn(b"must-not-appear", result.stderr)
            link = Path(temp) / "link.json"
            link.symlink_to(path)
            with self.assertRaises((h.InvalidArtifact, OSError)):
                h.read_artifact(link)


def go_vectors():
    """Test-only generator for the independent Go codecs and literal gate oracle."""
    import resp
    import runtime_case

    rows = []
    for scenario in h.SCENARIOS:
        plan = h.compile_plan(inputs(scenario))
        row = {"scenario": scenario, "core_hex": plan["guard_core"]["record_hex"],
               "marker_hex": plan["compatibility_marker"]["record_hex"],
               "core_sha256": plan["guard_core"]["sha256"],
               "manifest_sha256": plan["compatibility"]["sha256"],
               "variants": plan["operation_inventory"]}
        if scenario == "ledger-smoke":
            setup = h.ledger_setup(plan, 1000)
            row["stored_guard_hex"] = setup["stored_guard"]["record_hex"]
            row["legacy_hex"] = setup["legacy_retirement"]["record_hex"]
            for label, wire in (
                ("active", runtime_case.active_request(plan, setup, "1" * 32)),
                ("boot", runtime_case.boot_request("b" * 40, "1" * 32, "2" * 64, 1000)),
            ):
                row[label + "_parts_hex"] = [(part.encode() if type(part) is str else part).hex() for part in wire]
                row[label + "_resp_size"] = len(resp.encode(wire))
        rows.append(row)
    sys.stdout.buffer.write(h.canonical({"cases": rows}))


if __name__ == "__main__":
    if sys.argv[1:] == ["--go-vectors"]:
        go_vectors()
    else:
        unittest.main()
