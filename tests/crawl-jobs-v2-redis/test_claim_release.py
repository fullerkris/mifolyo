"""Offline fixture controls. All identities emitted by --go-vectors are test-only."""
import copy
import json
import sys
import unittest
from unittest.mock import patch

import claim_release as cr
import harness as h
import resp
import runtime_case
from test_harness import inputs


def captured(now=1000000):
    # Public lexical controls, never runtime tokens/credentials or approvals.
    return {"fixture_id": "1" * 32, "redis_time_ms": now, "owner_a": "2" * 32, "owner_b": "3" * 32,
            "token_a": "4" * 64, "token_b": "5" * 64, "wrong_token": "6" * 64}


class ClaimReleaseTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plan = h.compile_plan(inputs(cr.SCENARIO))
        cls.fixture = cr.compile_fixture(cls.plan, captured())
        cls.times = [1000000 + index for index in range(9)]

    def test_complete_private_fixture_and_scenario_binding(self):
        f = self.fixture
        self.assertEqual(h.canonical(f), h.canonical(cr.compile_fixture(self.plan, captured())))
        self.assertEqual(len(f["key_inventory"]), 58)
        self.assertEqual(len(set(f["key_inventory"])), 58)
        self.assertEqual(set(f["initial_state"]), set(f["key_inventory"]) - {h.AUTH[0]})
        self.assertNotIn(h.AUTH[0], {row["key"] for row in f["authority_setup"]["writes"]})
        self.assertEqual(len(dict(f["initial_state"][f["base_key"]]["fields"])), 59)
        self.assertEqual(len(dict(f["initial_state"][f["job_key"]]["fields"])), 54)
        self.assertFalse(f["execution_authorized"])
        self.assertFalse(f["time_observation_verified"])
        for artifact in f["authority_setup"]["empty_artifacts"].values():
            self.assertEqual(artifact["scenario"], cr.SCENARIO)
        smoke = h.ledger_setup(h.compile_plan(inputs()), captured()["redis_time_ms"])
        self.assertNotEqual(smoke["legacy_retirement"]["sha256"], f["authority_setup"]["legacy_retirement"]["sha256"])
        for scenario in ("ledger-smoke", "administrative-fresh", "administrative-migration"):
            with self.subTest(scenario=scenario), self.assertRaises(h.InvalidArtifact):
                cr.compile_fixture(h.compile_plan(inputs(scenario)), captured())

    def test_closed_inputs_identity_reuse_and_exact_time_bounds(self):
        changes = {"redis_time_ms": [0, 400, h.MAX_EXACT, True, 1.0, "1000000"],
                   "fixture_id": ["0" * 32, "A" * 32, "../escape", 1],
                   "owner_b": [captured()["owner_a"], "short"],
                   "token_b": [captured()["token_a"], "short"], "wrong_token": [captured()["token_b"]],
                   "extra": ["secret-canary"]}
        for key, values in changes.items():
            for value in values:
                with self.subTest(key=key, value=value), self.assertRaises(h.InvalidArtifact):
                    cr.compile_fixture(self.plan, dict(captured(), **{key: value}))
        for key in captured():
            data = captured()
            del data[key]
            with self.assertRaises(h.InvalidArtifact):
                cr.compile_fixture(self.plan, data)
        for now in (401, h.MAX_EXACT - cr.TOMBSTONE_MS - 600000):
            cr.validate_fixture(self.plan, cr.compile_fixture(self.plan, captured(now)))

    def test_setup_inventory_shape_provenance_and_data_mutations_reject(self):
        f = self.fixture
        for change in (
            lambda d: d["key_inventory"].pop(),
            lambda d: d["key_inventory"].append(d["key_inventory"][0]),
            lambda d: d["key_inventory"].reverse(),
            lambda d: d["bootstrap_owned_keys"].clear(),
            lambda d: d["external_required_absent"].pop(),
            lambda d: d["initial_state"].update({"unlisted": None}),
            lambda d: d["initial_state"].pop(f["job_key"]),
            lambda d: d["initial_state"][f["job_key"]].update(type="string"),
            lambda d: d["initial_state"][f["base_key"]].update(expires_at_ms=1000001),
            lambda d: d["initial_state"][f["job_key"]]["fields"].append(["unlisted", "0"]),
            lambda d: d["initial_state"][f["job_key"]]["fields"].reverse(),
            lambda d: d["identities"]["a"].update(reservation_id="a" * 64),
            lambda d: d.update(execution_authorized=True),
            lambda d: d.update(time_observation_verified=True),
            lambda d: d.update(compiler_sha256="a" * 64),
            lambda d: d["policy_descriptors"]["authorization"].update(release_eligible=True),
        ):
            changed = copy.deepcopy(f)
            change(changed)
            with self.assertRaises(h.InvalidArtifact):
                cr.validate_fixture(self.plan, changed)
        other = cr.compile_fixture(self.plan, dict(captured(), fixture_id="7" * 32))
        changed = copy.deepcopy(f)
        changed["initial_state"] = other["initial_state"]
        with self.assertRaises(h.InvalidArtifact):
            cr.validate_fixture(self.plan, changed)

    def test_model_membership_counts_fences_and_replays(self):
        f = self.fixture
        states = cr.expected_sequence(self.plan, f, self.times)
        self.assertEqual([r["reply"][0] for r in states], ["CLAIMED", "ALREADY_CLAIMED", "LEASE_LOST", "RELEASED_READY",
            "RELEASED_READY", "CLAIMED", "LEASE_LOST", "RELEASED_READY", "RELEASED_READY"])
        for left, right in ((0, 1), (1, 2), (3, 4), (5, 6), (7, 8)):
            self.assertEqual(states[left]["state"], states[right]["state"])
        for index, row in enumerate(states):
            cr.validate_state(self.plan, f, self.times, index, row["state"])
            run, job = [dict(row["state"][key]["fields"]) for key in (f["base_key"], f["job_key"])]
            count = "1" if index < 5 else "2"
            leased = index in (0, 1, 2, 5, 6)
            self.assertEqual((run["claims_total"], run["reservation_creations_total"], job["claim_count"]), (count,) * 3)
            self.assertEqual(job["lease_fence"], count)
            self.assertEqual(job["state"], "leased" if leased else "ready")
            self.assertEqual(run["pending_request_reservations"], "1" if leased else "0")
            self.assertEqual(run["open_job_count"], "1")
            for key in ("request_starts", "delivery_attempts", "lease_request_starts_baseline"):
                self.assertEqual(job[key], "0")
            self.assertIsNone(row["state"][h.P + "first_request_start"])
            self.assertEqual(len(row["state"]), 57)
        self.assertEqual(states[0]["reply"][2:], states[1]["reply"][2:])
        self.assertEqual(states[6]["reply"][2], "2")

    def test_absolute_tombstone_expiry_and_no_lease_ttl(self):
        f = self.fixture
        states = cr.expected_sequence(self.plan, f, self.times)
        for identity, created, released in (("a", 0, 3), ("b", 5, 7)):
            key = h.P + "reservation:" + f["identities"][identity]["reservation_id"]
            self.assertEqual(states[created]["state"][key]["expires_at_ms"], -1)
            for row in states[released:]:
                self.assertEqual(row["state"][key]["expires_at_ms"], self.times[released] + 86400000)
                fields = dict(row["state"][key]["fields"])
                self.assertEqual(fields["expires_at_ms"], str(self.times[created] + 60000))
                self.assertEqual(fields["terminal_at_ms"], str(self.times[released]))

    def test_state_oracle_rejects_wrong_counts_indexes_ttls_and_partial_reads(self):
        f = self.fixture
        states = cr.expected_sequence(self.plan, f, self.times)
        key = h.P + "reservation:" + f["identities"]["a"]["reservation_id"]
        for index, change in (
            (1, lambda s: cr._change(s[f["base_key"]], claims_total=2)),
            (6, lambda s: cr._change(s[f["job_key"]], state="ready")),
            (4, lambda s: s[key].update(expires_at_ms=s[key]["expires_at_ms"] + 1)),
            (0, lambda s: s.update({h.P + "active_leases": None})),
            (0, lambda s: s.pop(f["job_key"])),
            (0, lambda s: s.update({"unknown": None})),
            (0, lambda s: cr._change(s[f["job_key"]], lease_request_starts_baseline=1)),
            (0, lambda s: s[h.P + "active_leases"].update(expires_at_ms=True)),
        ):
            observed = copy.deepcopy(states[index]["state"])
            change(observed)
            with self.assertRaises(h.InvalidArtifact):
                cr.validate_state(self.plan, f, self.times, index, observed)

    def test_time_bounds_equal_milliseconds_and_out_of_order_reject(self):
        cr.expected_sequence(self.plan, self.fixture, [1000000] * 9)
        cr.expected_sequence(self.plan, self.fixture, [1030001 + index for index in range(9)])
        for times in ([1000000] * 8, [True] * 9, [1000000.0] * 9, [999999] * 9,
                      [1300001] * 9, [1000000] + [1030001] * 8, list(reversed(self.times))):
            with self.assertRaises(h.InvalidArtifact):
                cr.expected_sequence(self.plan, self.fixture, times)
        for step in (True, -1, 9):
            with self.assertRaises(h.InvalidArtifact):
                cr.validate_state(self.plan, self.fixture, self.times, step, {})

    def test_wire_counts_exact_replays_and_binary_gate_fields(self):
        wires = cr.wire_requests(self.plan, self.fixture, "7" * 32)
        for index, wire in enumerate(wires):
            keys = int(wire[2])
            self.assertEqual(keys, 57 if index in (0, 1, 5) else 44)
            self.assertEqual(len(wire) - 3 - keys, 40 if keys == 57 else 13)
            self.assertEqual(len(set(wire[3:3 + keys])), keys)
            self.assertTrue(all(type(wire[3 + keys + i]) is bytes for i in (3, 4, 5, 6)))
            self.assertLessEqual(len(resp.encode(wire)), resp.MAX_REQUEST)
        self.assertEqual(wires[0], wires[1])
        self.assertEqual(wires[3], wires[4])
        self.assertEqual(wires[3], wires[6])
        self.assertEqual(wires[7], wires[8])
        self.assertNotEqual(wires[2][-1], wires[3][-1])
        for epoch in ("", "x" * 32, "0" * 32):
            with self.assertRaises(h.InvalidArtifact):
                cr.wire_requests(self.plan, self.fixture, epoch)

    def test_public_projection_redaction_no_io_and_no_implicit_approval(self):
        with patch("socket.socket", side_effect=AssertionError("network activity")), \
             patch("subprocess.Popen", side_effect=AssertionError("process activity")):
            fixture = cr.compile_fixture(self.plan, captured())
            cr.wire_requests(self.plan, fixture, "7" * 32)
            cr.expected_sequence(self.plan, fixture, self.times)
            public = json.dumps(cr.public_summary(self.plan, fixture))
        for sensitive in (cr.URL, cr.ROBOTS, *[captured()[key] for key in ("fixture_id", "owner_a", "owner_b", "token_a", "token_b", "wrong_token")],
                          fixture["identities"]["a"]["reservation_id"]):
            self.assertNotIn(sensitive, public)
        with self.assertRaises(h.InvalidArtifact):
            runtime_case.validate_approval(self.plan, {}, 1000000)
        changed = copy.deepcopy(fixture)
        changed["initial_state"]["secret-canary"] = {"secret": "must-not-appear"}
        with self.assertRaises(h.InvalidArtifact) as failure:
            cr.public_summary(self.plan, changed)
        self.assertNotIn("secret-canary", str(failure.exception))
        self.assertNotIn("must-not-appear", str(failure.exception))


def go_vectors():
    plan = h.compile_plan(inputs(cr.SCENARIO))
    vectors = []
    for now, equal in ((1000000, False), (h.MAX_EXACT - 100000000, True)):
        f = cr.compile_fixture(plan, captured(now))
        times = [now if equal else now + 5000 + index for index in range(9)]
        wires = cr.wire_requests(plan, f, "7" * 32)
        vectors.append({"fixture": f, "times": times, "acl_rules": runtime_case.acl_rules(cr.CASE, f, plan),
                        "expected": cr.expected_sequence(plan, f, times),
                        "wires": [{"parts_hex": [(value if type(value) is bytes else value.encode()).hex() for value in wire],
                                   "resp_size": len(resp.encode(wire))} for wire in wires]})
    sys.stdout.buffer.write(h.canonical({"vectors": vectors}))


if __name__ == "__main__":
    if sys.argv[1:] == ["--go-vectors"]:
        go_vectors()
    else:
        unittest.main()
