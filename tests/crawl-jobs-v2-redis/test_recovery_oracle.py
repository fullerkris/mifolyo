"""Offline recovery vectors; simulated time never attests actual worker death."""
import copy
import sys
import unittest

import claim_release as cr
import harness as h
import recovery_oracle as recovery
import resp
import runtime_case as case
from test_claim_release import captured
from test_harness import inputs


def context(offset=0):
    plan = h.compile_plan(inputs(cr.SCENARIO))
    fixture = cr.compile_fixture(plan, captured())
    claimed = 1000001
    due = claimed + 60000 + offset
    times = [claimed, claimed + 59999, *range(due, due + 11)]
    return plan, fixture, times


class RecoveryOracleTests(unittest.TestCase):
    def test_recovery_deadline_and_unchanged_replays(self):
        for offset in (0, 1):
            plan, fixture, times = context(offset)
            rows = recovery.expected_sequence(plan, fixture, times)
            self.assertEqual([row["reply"][0] for row in rows], ["CLAIMED", "BATCH_DONE", "BATCH_DONE", "BATCH_DONE",
                "CLAIMED", "ALREADY_CLAIMED", "LEASE_LOST", "LEASE_LOST", "LEASE_LOST", "RENEWED", "RELEASED_READY", "RELEASED_READY", "BATCH_DONE"])
            for before, after in ((0, 1), (2, 3), (4, 5), (5, 6), (6, 7), (10, 11), (11, 12)):
                self.assertEqual(rows[before]["state"], rows[after]["state"])
            for row in rows:
                self.assertEqual(set(row["state"]), set(fixture["initial_state"]))
                self.assertIsNone(row["state"][h.P + "first_request_start"])
            final = rows[-1]["state"]
            run, job = [dict(final[key]["fields"]) for key in (fixture["base_key"], fixture["job_key"])]
            self.assertEqual([run[name] for name in ("claims_total", "reservation_creations_total", "recovered_leases_total", "renewal_rejections_total")], ["2", "2", "1", "1"])
            self.assertEqual([job[name] for name in ("lease_fence", "claim_count", "next_request_ordinal", "pre_io_recoveries")], ["2", "2", "3", "1"])
            self.assertEqual([job[name] for name in ("request_starts", "lease_request_starts_baseline", "delivery_attempts")], ["0"] * 3)
            self.assertEqual(dict(final[fixture["base_key"] + ":recovery_outcome_counts"]["fields"]), {"ready": "1", "delayed": "0", "dead": "0", "cancelled": "0"})

    def test_stale_renewal_counter_exception_and_exact_expiries(self):
        plan, f, times = context()
        rows = recovery.expected_sequence(plan, f, times)
        before, after = copy.deepcopy(rows[7]["state"]), rows[8]["state"]
        cr._change(before[f["base_key"]], renewal_rejections_total=1)
        self.assertEqual(before, after)
        qa, qb = [h.P + "reservation:" + f["identities"][label]["reservation_id"] for label in ("a", "b")]
        for row in rows[2:]:
            self.assertEqual(row["state"][qa]["expires_at_ms"], times[2] + 86400000)
            self.assertEqual(dict(row["state"][qa]["fields"])["state"], "expired")
            self.assertEqual(dict(row["state"][qa]["fields"])["expires_at_ms"], str(times[0] + 60000))
        self.assertEqual(rows[9]["state"][qb]["expires_at_ms"], -1)
        self.assertEqual(dict(rows[9]["state"][qb]["fields"])["expires_at_ms"], str(times[9] + 60000))
        for row in rows[10:]:
            self.assertEqual(row["state"][qb]["expires_at_ms"], times[10] + 86400000)
            self.assertEqual(dict(row["state"][qb]["fields"])["state"], "cancelled")

    def test_invalid_phase_times_and_corrupt_observations_reject(self):
        plan, f, times = context()
        for index, value in ((1, times[0] + 60000), (2, times[0] + 59999), (0, True), (0, 1000001.0), (12, 1400000)):
            changed = list(times)
            changed[index] = value
            with self.assertRaises(h.InvalidArtifact):
                recovery.expected_sequence(plan, f, changed)
        rows = recovery.expected_sequence(plan, f, times)
        for index, change in ((2, lambda state: cr._change(state[f["job_key"]], pre_io_recoveries=0)),
                              (2, lambda state: cr._change(state[f["base_key"]], reservation_creations_total=0)),
                              (8, lambda state: cr._change(state[f["job_key"]], updated_at_ms=times[8])),
                              (10, lambda state: state.pop(h.P + "reservation:" + f["identities"]["a"]["reservation_id"]))):
            changed = copy.deepcopy(rows[index]["state"])
            change(changed)
            with self.assertRaises(h.InvalidArtifact):
                recovery.validate_state(plan, f, times, index, changed)

    def test_wire_shapes_and_offline_only_admission(self):
        plan, f, _ = context()
        wires = recovery.wire_requests(plan, f, "7" * 32)
        self.assertEqual([int(wire[2]) for wire in wires], [57, 42, 42, 42, 57, 57, 57, 44, 44, 44, 44, 44, 42])
        self.assertEqual([len(wire) - 3 - int(wire[2]) for wire in wires], [40, 8, 8, 8, 40, 40, 40, 13, 12, 12, 13, 13, 8])
        self.assertTrue(all(len(resp.encode(wire)) <= 65536 for wire in wires))
        self.assertNotIn(recovery.CASE, case.CASES)
        self.assertFalse(f["execution_authorized"])
        with self.assertRaises(h.InvalidArtifact):
            recovery.fixture_for(h.compile_plan(inputs()), f)

    def test_malformed_nested_plan_has_closed_artifact_error(self):
        _, f, _ = context()
        for value in (None, [], 1, True, "ledger-claim-release"):
            with self.subTest(inputs=value), self.assertRaisesRegex(h.InvalidArtifact, "^RECOVERY_ORACLE_BASIS$"):
                recovery.fixture_for({"inputs": value}, f)

    def test_sorted_nonadvancing_renewal_and_phase_span_boundaries(self):
        plan, f, times = context()
        changed = list(times)
        changed[4:10] = [times[4]] * 6
        self.assertEqual(changed, sorted(changed))
        with self.assertRaisesRegex(h.InvalidArtifact, "^RECOVERY_PHASE_TIME$"):
            recovery.expected_sequence(plan, f, changed)
        changed = list(times)
        changed[-1] = times[2] + 30000
        self.assertEqual(recovery.expected_sequence(plan, f, changed)[-1]["reply"], ["BATCH_DONE", str(changed[-1]), "0", "0"])
        changed[-1] += 1
        self.assertEqual(changed, sorted(changed))
        with self.assertRaisesRegex(h.InvalidArtifact, "^RECOVERY_PHASE_TIME$"):
            recovery.expected_sequence(plan, f, changed)


def go_vectors():
    vectors = []
    for offset in (0, 1):
        plan, f, times = context(offset)
        rules = copy.deepcopy(case.acl_rules(case.CLAIM_CASE, f, plan))
        # Proposed exact recovery-only addition, not granted to any live role.
        rules["ledger"] = (*rules["ledger"], case.selector("+hset", {f["base_key"] + ":recovery_outcome_counts"}, "~"))
        vectors.append({"fixture": f, "times": times, "operations": list(recovery.OPERATIONS),
            "expected": recovery.expected_sequence(plan, f, times), "acl_rules": rules,
            "wires": [{"parts_hex": [(value.encode() if type(value) is str else value).hex() for value in wire], "resp_size": len(resp.encode(wire))}
                      for wire in recovery.wire_requests(plan, f, "7" * 32)]})
    raw = h.canonical({"purpose": "offline_public_recovery_vectors", "execution_authorized": False, "vectors": vectors})
    h.require(len(raw) <= h.MAX_ARTIFACT_BYTES, "VECTOR_BOUND")
    sys.stdout.buffer.write(raw)


if __name__ == "__main__":
    if sys.argv[1:] == ["--go-vectors"]:
        go_vectors()
    else:
        unittest.main()
