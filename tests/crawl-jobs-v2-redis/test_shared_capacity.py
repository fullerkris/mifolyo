"""Closed synthetic vectors and cross-run capacity/state-preservation controls."""
import copy
import sys
import unittest

import harness as h
import resp
import runtime_case as runtime
import shared_capacity as oracle
import shared_capacity_specs as spec
from test_claim_release import captured
from test_harness import inputs

PROFILES = {"spaced": ("finish", True), "same-ms": ("finish", False), "reversed": ("finish-reversed", True),
    "cancel-spaced": ("cancel", True), "cancel-same-ms": ("cancel", False)}


def context(profile="spaced", scenario=spec.SCENARIO):
    h.require(type(profile) is str and profile in PROFILES, "SHARED_VECTOR_PROFILE")
    trace, spaced = PROFILES[profile]
    plan = h.compile_plan(inputs(scenario))
    fixture = oracle.compile_fixture(plan, captured())
    observations = []
    for index, (_, _, status, _) in enumerate(spec.SEQUENCES[trace]):
        before = 1000001 + (index * 3 if spaced else 0)
        after = before + (2 if spaced else 0)
        observations.append({"started_at_ms": before, "now_ms": None if status.startswith("CRAWL_V2_") else (before + after) // 2,
            "finished_at_ms": after})
    return plan, fixture, observations, trace


def go_packet(profile):
    h.require(type(profile) is str and profile in PROFILES, "SHARED_VECTOR_PROFILE")
    plan, fixture, observations, trace = context(profile, spec.CANCEL_SCENARIO if profile.startswith("cancel-") else spec.SCENARIO)
    value = {"purpose": "offline_public_shared_capacity_vectors", "execution_authorized": False, "profile": profile, "trace": trace,
        "fixture": fixture, "observations": observations, "expected": oracle.expected_sequence(plan, fixture, observations, trace),
        "acl_rules": oracle.acl_rules(plan, fixture), "wires": [
            {"parts_hex": [(part.encode() if type(part) is str else part).hex() for part in wire], "resp_size": len(resp.encode(wire))}
            for wire in oracle.wire_requests(plan, fixture, "7" * 32, trace)]}
    raw = h.canonical(value)
    h.require(len(raw) <= h.MAX_ARTIFACT_BYTES, "SHARED_VECTOR_BOUND")
    return raw


def fields(state, key):
    return dict(state[key]["fields"])


class SharedCapacityTests(unittest.TestCase):
    def test_two_runs_shared_group_distinct_origins_and_full_inventory(self):
        plan, f, _, _ = context()
        a, b = [f["actors"][label] for label in ("a", "b")]
        self.assertNotEqual(a["run_id"], b["run_id"])
        self.assertNotEqual(a["job_id"], b["job_id"])
        self.assertEqual(a["scope_ids"][:2], b["scope_ids"][:2])
        self.assertNotEqual(a["scope_ids"][2], b["scope_ids"][2])
        self.assertEqual((len(f["key_inventory"]), len(f["initial_state"])), (90, 89))
        self.assertEqual(sum(row is not None for row in f["initial_state"].values()), 45)
        self.assertNotIn(h.AUTH[0], f["initial_state"])
        self.assertEqual(set(f["initial_state"][h.P + "active_runs"]["members"]), {a["run_id"], b["run_id"]})
        for actor in (a, b):
            self.assertEqual(actor["identity"]["fence"], "1")
            self.assertEqual(actor["identity"]["intent"]["request_ordinal"], "1")
            self.assertEqual(actor["identity"]["intent"]["request_kind"], "robots")
        self.assertEqual(fields(f["initial_state"], a["base_key"])["policy_group_map_sha256"], fields(f["initial_state"], b["base_key"])["policy_group_map_sha256"])
        self.assertNotEqual(fields(f["initial_state"], a["base_key"])["source_sha256"], fields(f["initial_state"], b["base_key"])["source_sha256"])
        summary = oracle.public_summary(plan, f)
        self.assertFalse(summary["execution_authorized"])
        self.assertEqual(summary["measurement_status"], "not_measured")
        for private in (a["url"], a["origin"], f["inputs"]["owner_a"], f["inputs"]["token_a"], a["identity"]["reservation_id"]):
            self.assertNotIn(private.encode(), h.canonical(summary))

    def test_blocked_claims_preserve_all_state_and_do_not_materialize_peer_origin(self):
        plan, f, times, trace = context()
        rows = oracle.expected_sequence(plan, f, times, trace)
        b = f["actors"]["b"]
        for index in (2, 3, 7):
            self.assertEqual(rows[index]["reply"], ["CAPACITY_BLOCKED", str(times[index]["now_ms"]), b["scope_ids"][1], "1", "1", "0"])
            self.assertEqual(rows[index]["state"], rows[index - 1]["state"])
            self.assertEqual(rows[index]["state"][b["job_key"]], f["initial_state"][b["job_key"]])
            for ending in ("", ":active", ":pending", ":started"):
                self.assertIsNone(rows[index]["state"][h.P + "rate:" + b["scope_ids"][2] + ending])
            self.assertIsNone(rows[index]["state"][h.P + "reservation:" + b["identity"]["reservation_id"]])
            self.assertEqual(fields(rows[index]["state"], h.P + "rate:" + b["scope_ids"][0])["active_count"], "1")
        self.assertEqual(fields(rows[2]["state"], h.P + "rate:" + b["scope_ids"][1])["pending_count"], "1")
        self.assertEqual(fields(rows[7]["state"], h.P + "rate:" + b["scope_ids"][1])["started_count"], "1")

    def test_finish_releases_capacity_without_refunds_or_ending_either_lease(self):
        plan, f, times, trace = context()
        rows = oracle.expected_sequence(plan, f, times, trace)
        final = rows[-1]["state"]
        self.assertEqual(len(final[h.P + "active_leases"]["members"]), 2)
        self.assertEqual(len(final[h.P + "rate_scopes"]["members"]), 4)
        for label, claim_index, start_index, finish_index in (("a", 0, 5, 10), ("b", 11, 15, 17)):
            actor = f["actors"][label]
            run, job = fields(final, actor["base_key"]), fields(final, actor["job_key"])
            self.assertEqual([run[key] for key in ("claims_total", "reservation_creations_total", "request_starts", "open_job_count")], ["1"] * 4)
            self.assertEqual([job[key] for key in ("state", "next_request_ordinal", "request_starts", "delivery_attempts", "lease_request_starts_baseline", "active_reservation_id")], ["leased", "2", "1", "1", "0", ""])
            self.assertEqual(job["lease_expires_at_ms"], str(times[claim_index]["now_ms"] + 60000))
            self.assertEqual(job["last_document_request_started_at_ms"], "0")
            qkey = h.P + "reservation:" + actor["identity"]["reservation_id"]
            self.assertEqual(final[qkey]["expires_at_ms"], times[finish_index]["now_ms"] + 86400000)
            self.assertEqual(fields(final, qkey)["expires_at_ms"], job["lease_expires_at_ms"])
            origin = fields(final, h.P + "rate:" + actor["scope_ids"][2])
            self.assertEqual(origin["last_started_at_ms"], str(times[start_index]["now_ms"]))
            self.assertEqual(origin["next_allowed_ms"], origin["last_started_at_ms"])
        first = fields(final, h.P + "first_request_start")
        self.assertEqual((first["run_id"], first["started_at_ms"]), (f["actors"]["a"]["run_id"], str(times[5]["now_ms"])))
        self.assertEqual(rows[14]["reply"][4:], ["1", "1", "1", "1", "0"])
        self.assertEqual(rows[19]["reply"][4:], ["1", "1", "1", "1", "0"])

    def test_foreign_identity_controls_keep_the_victim_record_and_keys(self):
        plan, f, _, _ = context()
        wires = oracle.wire_requests(plan, f, "7" * 32)
        a, b = f["actors"]["a"], f["actors"]["b"]
        self.assertEqual(wires[4][-6:], (a["run_id"], a["job_id"], b["identity"]["owner_id"], a["identity"]["lease_token"], "1", a["identity"]["reservation_id"]))
        self.assertEqual(wires[9][-6:], (a["run_id"], a["job_id"], a["identity"]["owner_id"], b["identity"]["lease_token"], "1", a["identity"]["reservation_id"]))
        for step, wire in zip(spec.FINISH_STEPS, wires):
            self.assertEqual(int(wire[2]), 9 if step[0] == spec.MAINTAIN else 57)
            self.assertLessEqual(len(resp.encode(wire)), 65536)

    def test_all_nonmutating_rows_preserve_combined_state_in_each_closed_trace(self):
        for profile in PROFILES:
            plan, f, times, trace = context(profile)
            rows = oracle.expected_sequence(plan, f, times, trace)
            previous = f["initial_state"]
            for index, row in enumerate(rows):
                self.assertEqual(row["state"] != previous, index in spec.MUTATIONS[trace])
                previous = row["state"]
            self.assertEqual(rows[-1]["reply"], ["BATCH_DONE", str(times[-1]["now_ms"]), "4", "0"])

    def test_cancel_replay_cannot_debit_the_peer_and_keeps_zero_start_history(self):
        plan, f, times, trace = context("cancel-spaced")
        rows = oracle.expected_sequence(plan, f, times, trace)
        a, b = f["actors"]["a"], f["actors"]["b"]
        self.assertEqual([rows[i]["reply"][0] for i in (2, 3, 5)], ["RESERVATION_CANCELLED"] * 3)
        self.assertEqual(rows[5]["state"], rows[4]["state"])
        self.assertIsNone(rows[5]["state"][h.P + "first_request_start"])
        self.assertEqual(fields(rows[-1]["state"], a["base_key"])["request_starts"], "0")
        self.assertEqual(fields(rows[-1]["state"], a["base_key"])["reservation_creations_total"], "1")
        self.assertEqual(fields(rows[-1]["state"], h.P + "first_request_start")["run_id"], b["run_id"])
        qkey = h.P + "reservation:" + a["identity"]["reservation_id"]
        self.assertEqual(rows[-1]["state"][qkey]["expires_at_ms"], times[2]["now_ms"] + 86400000)
        self.assertEqual(fields(rows[-1]["state"], h.P + "rate:" + a["scope_ids"][2])["last_started_at_ms"], "0")

    def test_reversed_contender_owns_the_first_start_without_losing_peer_state(self):
        plan, f, times, trace = context("reversed")
        rows = oracle.expected_sequence(plan, f, times, trace)
        self.assertEqual(fields(rows[-1]["state"], h.P + "first_request_start")["run_id"], f["actors"]["b"]["run_id"])
        self.assertEqual(fields(rows[7]["state"], f["actors"]["a"]["job_key"])["state"], "ready")
        self.assertEqual(len(rows[-1]["state"][h.P + "active_leases"]["members"]), 2)

    def test_fixture_mutation_and_extra_configuration_reject(self):
        plan, f, _, _ = context()
        for mutate in (
            lambda value: value["actors"]["b"].update(run_id=value["actors"]["a"]["run_id"]),
            lambda value: value["actors"]["b"]["scope_ids"].__setitem__(2, value["actors"]["a"]["scope_ids"][2]),
            lambda value: value["actors"]["b"]["identity"].update(fence="2"),
            lambda value: value["key_inventory"].pop(),
            lambda value: value["initial_state"].pop(value["actors"]["a"]["job_key"]),
            lambda value: value["policy_group_fields"][-1].__setitem__(1, "8000"),
            lambda value: value.update(interval=0),
        ):
            changed = copy.deepcopy(f)
            mutate(changed)
            with self.assertRaises(h.InvalidArtifact):
                oracle.validate_fixture(plan, changed)
        for change in ({"run_a": "1" * 32}, {"group_concurrency": 2}, {"urls": []}):
            with self.assertRaises(h.InvalidArtifact):
                oracle.compile_fixture(plan, captured() | change)

    def test_observation_prefix_time_and_error_shapes_are_strict(self):
        plan, f, times, trace = context()
        for size in range(len(times) + 1):
            self.assertEqual(len(oracle.expected_sequence(plan, f, times[:size], trace)), size)
        for mutate in (
            lambda rows: rows[4].update(now_ms=1000014),
            lambda rows: rows[0].update(now_ms=True),
            lambda rows: rows[2].update(started_at_ms=1),
            lambda rows: rows[-1].update(now_ms=1030003, finished_at_ms=1030003),
            lambda rows: rows[1].update(extra="private-canary"),
        ):
            changed = copy.deepcopy(times)
            mutate(changed)
            with self.assertRaises(h.InvalidArtifact):
                oracle.expected_sequence(plan, f, changed, trace)
        for invalid in (None, [], "arbitrary"):
            with self.assertRaises(h.InvalidArtifact):
                oracle.expected_sequence(plan, f, times, invalid)

    def test_packet_bounds_and_closed_runtime_case_registration(self):
        for profile in PROFILES:
            raw = go_packet(profile)
            self.assertLessEqual(len(raw), 2 * 1024 * 1024)
            packet = h.decode(raw)
            self.assertFalse(packet["execution_authorized"])
            self.assertEqual(len(packet["expected"]), 9 if profile.startswith("cancel-") else 21)
        plan, f, _, _ = context()
        self.assertEqual(runtime.case_for_plan(plan), spec.CASE)
        self.assertEqual(runtime.fixture(plan, f["inputs"]["fixture_id"], {key: value for key, value in f["inputs"].items()
            if key not in ("fixture_id", "redis_time_ms")}, f["inputs"]["redis_time_ms"]), f)
        with self.assertRaises(h.InvalidArtifact):
            runtime.stage_roles(spec.CASE, "rate_before")
        rules = oracle.acl_rules(plan, f)
        self.assertFalse(any("+hset" in rule or "~*" in rule for rule in rules["observer"]))
        for actor in f["actors"].values():
            self.assertTrue(any(actor["base_key"] in rule and "+hmget" in rule for rule in rules["ledger"]))
        with self.assertRaises(h.InvalidArtifact):
            go_packet("unreviewed")

    def test_delayed_exact_thirty_second_boundary_and_malformed_prefixes(self):
        plan, f, times, trace = context()
        delayed = [{key: None if value is None else value + 200000 for key, value in row.items()} for row in times]
        delayed[-1]["finished_at_ms"] = delayed[0]["started_at_ms"] + 30000
        self.assertEqual(len(oracle.expected_sequence(plan, f, delayed, trace)), 21)
        beyond = copy.deepcopy(delayed)
        beyond[-1]["finished_at_ms"] += 1
        with self.assertRaisesRegex(h.InvalidArtifact, "SHARED_MEASURE_SPAN"):
            oracle.expected_sequence(plan, f, beyond, trace)
        with self.assertRaisesRegex(h.InvalidArtifact, "SHARED_OBSERVATIONS"):
            oracle.expected_sequence(plan, f, times + [times[-1]], trace)
        missing = copy.deepcopy(times)
        del missing[4]["now_ms"]
        with self.assertRaises(h.InvalidArtifact):
            oracle.expected_sequence(plan, f, missing, trace)


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "--go-vectors":
        sys.stdout.buffer.write(go_packet(sys.argv[2]))
    else:
        unittest.main()
