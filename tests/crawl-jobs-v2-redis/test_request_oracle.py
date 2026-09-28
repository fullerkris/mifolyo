"""Public synthetic vectors and independent request-history regression checks."""
import copy
import sys
import unittest

import claim_release as cr
import harness as h
import request_oracle as oracle
import request_specs as spec
import resp
import runtime_case as case
from test_claim_release import captured
from test_harness import inputs


def context(profile="spaced"):
    h.require(profile in ("spaced", "same-ms"), "REQUEST_VECTOR_PROFILE")
    plan = h.compile_plan(inputs(spec.SCENARIO))
    f = oracle.compile_fixture(plan, captured())
    observations = []
    for index in range(22):
        before = 1000001 + (index * 3 if profile == "spaced" else 0)
        after = before + (2 if profile == "spaced" else 0)
        observations.append({"started_at_ms": before, "now_ms": None if index in spec.ERRORS else (before + after) // 2, "finished_at_ms": after})
    return plan, f, observations


def go_packet(profile):
    plan, f, observations = context(profile)
    rows = oracle.expected_sequence(plan, f, observations)
    packet = {"purpose": "offline_public_request_vectors", "profile": profile, "execution_authorized": False,
        "fixture": f, "observations": observations, "expected": rows, "acl_rules": case.acl_rules(spec.CASE, f, plan),
        "wires": [{"parts_hex": [(part.encode() if type(part) is str else part).hex() for part in wire], "resp_size": len(resp.encode(wire))}
            for wire in oracle.wire_requests(plan, f, "7" * 32)]}
    raw = h.canonical(packet)
    h.require(len(raw) <= h.MAX_ARTIFACT_BYTES, "REQUEST_VECTOR_BOUND")
    return raw


class RequestOracleTests(unittest.TestCase):
    def test_two_same_lease_reservations_and_exact_wire_shapes(self):
        plan, f, times = context()
        a, b = [f["identities"][label] for label in ("a", "b")]
        self.assertEqual((b["owner_id"], b["lease_token"], b["fence"]), (a["owner_id"], a["lease_token"], "1"))
        self.assertNotEqual(a["reservation_id"], b["reservation_id"])
        self.assertEqual(b["intent"]["request_ordinal"], "2")
        self.assertEqual(b["intent"]["request_kind"], "document")
        self.assertEqual((len(f["initial_state"]), len(f["key_inventory"])), (57, 58))
        wires = oracle.wire_requests(plan, f, "7" * 32)
        for row, wire in zip(spec.STEPS, wires):
            self.assertEqual(int(wire[2]), 9 if row[0] == spec.MAINTAIN else 57)
            self.assertEqual(len(wire) - 3 - int(wire[2]), {spec.CLAIM: 40, spec.RESERVE: 30, spec.START: 13, spec.FINISH: 13, spec.MAINTAIN: 8}[row[0]])
        self.assertEqual(wires[4][-3], f["inputs"]["wrong_token"])
        self.assertEqual(wires[4][-1], a["reservation_id"])

    def test_start_history_capacity_and_terminal_deadlines(self):
        plan, f, observations = context()
        rows = oracle.expected_sequence(plan, f, observations)
        base, job = f["base_key"], f["job_key"]
        initial = rows[0]["state"]
        self.assertIsNone(initial[h.P + "first_request_start"])
        for index, starts, pending, active, ordinal in ((0, 0, 1, 0, 2), (5, 1, 0, 1, 2), (7, 1, 0, 0, 2), (11, 1, 1, 0, 3), (14, 2, 0, 1, 3), (18, 2, 0, 0, 3)):
            state = rows[index]["state"]
            r, j = dict(state[base]["fields"]), dict(state[job]["fields"])
            self.assertEqual((r["request_starts"], r["pending_request_reservations"], r["started_request_reservations"]), tuple(map(str, (starts, pending, active))))
            self.assertEqual((j["delivery_attempts"], j["request_starts"], j["lease_request_starts_baseline"], j["next_request_ordinal"]), (str(int(starts > 0)), str(starts), "0", str(ordinal)))
            for scope in f["scope_ids"]:
                values = dict(state[h.P + "rate:" + scope]["fields"])
                self.assertEqual(tuple(values[name] for name in ("active_count", "pending_count", "started_count")), tuple(map(str, (pending + active, pending, active))))
        self.assertEqual(rows[16]["reply"][4:], ["1", "1", "1", "1", "0"])
        self.assertEqual(rows[20]["reply"][4:], ["1", "2", "2", "2", "0"])
        self.assertEqual(rows[5]["state"][h.P + "first_request_start"], rows[21]["state"][h.P + "first_request_start"])
        self.assertEqual(dict(rows[7]["state"][job]["fields"])["last_document_target_url"], "")
        self.assertEqual(dict(rows[21]["state"][job]["fields"])["last_document_target_url"], cr.URL)
        for label, finished_index in (("a", 7), ("b", 18)):
            key = h.P + "reservation:" + f["identities"][label]["reservation_id"]
            row = rows[-1]["state"][key]
            self.assertEqual(row["expires_at_ms"], observations[finished_index]["now_ms"] + 86400000)
            self.assertEqual(dict(row["fields"])["expires_at_ms"], str(observations[0]["now_ms"] + 60000))
        deadlines = [dict(rows[-1]["state"][h.P + "rate:" + scope]["fields"])["next_allowed_ms"] for scope in f["scope_ids"]]
        self.assertEqual(deadlines, ["0", str(observations[14]["now_ms"]), str(observations[14]["now_ms"])])

    def test_errors_replays_and_maintenance_preserve_entire_state(self):
        plan, f, times = context()
        rows = oracle.expected_sequence(plan, f, times)
        for index in range(1, 22):
            if index not in spec.MUTATIONS:
                self.assertEqual(rows[index]["state"], rows[index - 1]["state"])
        for index in spec.ERRORS:
            self.assertIsNone(times[index]["now_ms"])
            self.assertEqual(rows[index]["reply"], {"error": spec.STEPS[index][2]})

    def test_time_bounds_and_malformed_nested_inputs_reject(self):
        plan, f, observations = context()
        for mutate in (
            lambda rows: rows[2].update(now_ms=1000009),
            lambda rows: rows[5].update(now_ms=True),
            lambda rows: rows[5].update(started_at_ms=0),
            lambda rows: rows[-1].update(now_ms=1030003, finished_at_ms=1030003),
            lambda rows: rows[1].update(extra="private-canary"),
        ):
            rows = copy.deepcopy(observations)
            mutate(rows)
            with self.assertRaises(h.InvalidArtifact):
                oracle.expected_sequence(plan, f, rows)
        for bad in (None, [], {}, {"inputs": []}):
            with self.assertRaises(h.InvalidArtifact):
                oracle.compile_fixture(bad, captured())
        changed = copy.deepcopy(f)
        changed["identities"]["b"]["fence"] = "2"
        with self.assertRaises(h.InvalidArtifact):
            oracle.validate_fixture(plan, changed)

    def test_same_millisecond_and_packet_bounds(self):
        for profile in ("spaced", "same-ms"):
            raw = go_packet(profile)
            self.assertLessEqual(len(raw), 2 * 1024 * 1024)
            value = h.decode(raw)
            self.assertEqual(value["profile"], profile)
            self.assertFalse(value["execution_authorized"])
            self.assertEqual(len(value["expected"]), 22)
        with self.assertRaises(h.InvalidArtifact):
            go_packet("unreviewed")


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "--go-vectors":
        sys.stdout.buffer.write(go_packet(sys.argv[2]))
    else:
        unittest.main()
