"""Independent positive-rate expectations and bounded public Go vector packets."""
import copy
import sys
import unittest

import claim_release as cr
import harness as h
import rate_specs as spec
import request_oracle as oracle
import runtime_case as case
import resp
from test_claim_release import captured
from test_harness import inputs


def context(profile="at"):
    h.require(profile in ("before", "at", "after"), "RATE_VECTOR_PROFILE")
    plan = h.compile_plan(inputs(spec.SCENARIO))
    f = oracle.compile_fixture(plan, captured())
    deadline = 1000017 + 8000
    observations = []
    for index in range(12 if profile == "before" else 24):
        before, after = 1000001 + index * 3, 1000003 + index * 3
        if index == 11:
            before = after = deadline - 1
        elif index >= 12:
            before = after = deadline + int(profile == "after") + (index - 12) * 3
        observations.append({"started_at_ms": before, "now_ms": None if index in spec.ERRORS else (before + after) // 2, "finished_at_ms": after})
    return plan, f, observations


def go_packet(profile):
    plan, f, observations = context(profile)
    count = len(observations)
    raw = h.canonical({"purpose": "offline_public_rate_vectors", "profile": profile, "execution_authorized": False,
        "fixture": f, "observations": observations, "expected": oracle.expected_sequence(plan, f, observations),
        "acl_rules": case.acl_rules(spec.CASE, f, plan), "wires": [
            {"parts_hex": [(part.encode() if type(part) is str else part).hex() for part in wire], "resp_size": len(resp.encode(wire))}
            for wire in oracle.wire_requests(plan, f, "7" * 32)[:count]]})
    h.require(len(raw) <= h.MAX_ARTIFACT_BYTES, "RATE_VECTOR_BOUND")
    return raw


class PositiveRateOracleTests(unittest.TestCase):
    def test_fixed_policy_binds_group_origin_and_private_ids(self):
        plan, f, _ = context()
        self.assertEqual(dict(f["policy_group_fields"])["interval_ms"], "8000")
        self.assertEqual(dict(f["initial_state"][f["base_key"] + ":group_interval_ms"]["fields"]), {cr.GROUP: "8000"})
        descriptor = f["policy_descriptors"]["crawl_policy"]
        self.assertEqual((descriptor["case"], descriptor["global_interval_ms"], descriptor["group_interval_ms"], descriptor["origin_interval_ms"]), (spec.CASE, 0, 8000, 8000))
        self.assertEqual(dict(f["initial_state"][f["base_key"]]["fields"])["crawl_policy_sha256"], h.digest(h.canonical(descriptor)))
        for label in ("a", "b"):
            intent = f["identities"][label]["intent"]
            self.assertEqual((intent["group_interval_ms"], intent["origin_interval_ms"]), ("8000", "8000"))
        for mutate in (lambda value: value["inputs"].update(interval_ms=1),
                       lambda value: value["identities"]["b"]["intent"].update(origin_interval_ms="16000"),
                       lambda value: value["policy_descriptors"]["crawl_policy"].update(group_interval_ms=True)):
            changed = copy.deepcopy(f)
            mutate(changed)
            with self.assertRaises(h.InvalidArtifact):
                oracle.validate_fixture(plan, changed)

    def test_denial_then_exact_deadline_admission_without_counter_loss(self):
        for profile in ("at", "after"):
            plan, f, observations = context(profile)
            rows = oracle.expected_sequence(plan, f, observations)
            deadline = observations[5]["now_ms"] + 8000
            self.assertEqual(rows[11]["reply"], ["RATE_BLOCKED", str(deadline - 1), f["scope_ids"][1], str(deadline), "1"])
            self.assertEqual(rows[11]["state"], rows[10]["state"])
            self.assertIsNone(rows[11]["state"][h.P + "reservation:" + f["identities"]["b"]["reservation_id"]])
            self.assertEqual(rows[12]["reply"][0], "RESERVED")
            self.assertEqual(rows[12]["reply"][1], str(deadline + int(profile == "after")))
            for index, row in enumerate(rows):
                if index > 0 and index not in spec.MUTATIONS:
                    self.assertEqual(row["state"], rows[index - 1]["state"])
            self.assertEqual(rows[16]["reply"][0], "ALREADY_RESERVED")
            self.assertEqual(rows[18]["reply"][4:], ["1", "1", "1", "1", "0"])
            final = rows[-1]["state"]
            self.assertEqual(dict(final[f["base_key"]]["fields"])["reservation_creations_total"], "2")
            self.assertEqual(dict(final[f["job_key"]]["fields"])["delivery_attempts"], "1")
            self.assertEqual([dict(final[h.P + "rate:" + scope]["fields"])["next_allowed_ms"] for scope in f["scope_ids"]],
                ["0", str(observations[15]["now_ms"] + 8000), str(observations[15]["now_ms"] + 8000)])
            for label, index in (("a", 7), ("b", 20)):
                reservation = final[h.P + "reservation:" + f["identities"][label]["reservation_id"]]
                self.assertEqual(reservation["expires_at_ms"], observations[index]["now_ms"] + 86400000)
                self.assertEqual(dict(reservation["fields"])["expires_at_ms"], str(observations[0]["now_ms"] + 60000))

    def test_missed_windows_and_fabricated_times_fail(self):
        plan, f, original = context()
        for change, end in ((lambda rows: rows[11].update(now_ms=1008017, finished_at_ms=1008017), 12),
                (lambda rows: rows[12].update(started_at_ms=1008016, now_ms=1008016, finished_at_ms=1008016), 13),
                (lambda rows: rows[16].update(started_at_ms=1016026, now_ms=1016026, finished_at_ms=1016026), 17),
                (lambda rows: rows[11].update(now_ms=True), 12),
                (lambda rows: rows[3].update(now_ms=1000010), 4)):
            rows = copy.deepcopy(original)
            change(rows)
            with self.assertRaises(h.InvalidArtifact):
                oracle.expected_sequence(plan, f, rows[:end])

    def test_bounded_before_at_after_packets_and_same_prefix(self):
        packets = [h.decode(go_packet(profile)) for profile in ("before", "at", "after")]
        self.assertEqual([len(value["expected"]) for value in packets], [12, 24, 24])
        self.assertEqual(packets[0]["fixture"], packets[1]["fixture"])
        self.assertEqual(packets[1]["fixture"], packets[2]["fixture"])
        for value in packets:
            self.assertFalse(value["execution_authorized"])
            self.assertEqual(value["expected"][:12], packets[0]["expected"])
            self.assertLessEqual(len(h.canonical(value)), 2 * 1024 * 1024)
        with self.assertRaises(h.InvalidArtifact):
            go_packet("unapproved")


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "--go-vectors":
        sys.stdout.buffer.write(go_packet(sys.argv[2]))
    else:
        unittest.main()
