"""Fixed cancellation lifecycle, exact peer-safe replays and bounded failure receipts."""
import copy
from contextlib import nullcontext
import io
import unittest
from unittest.mock import patch

import controller as ctl
import executor as worker
import harness as h
import runtime_case as case
import shared_capacity as oracle
import shared_capacity_specs as spec
import shared_executor as live
import resp
import test_execution as base
from test_claim_execution import permits
from test_shared_capacity import context, fields
from test_shared_execution import SharedRedis, SharedDocker, SerializedSharedDocker


class CancellationRedis(SharedRedis):
    def configure(self, request):
        self.plan = request["plan"]
        self.fixture = case.fixture(self.plan, request["fixture_id"], request["claim_material"], self.ms)
        self.wires = oracle.wire_requests(self.plan, self.fixture, h.digest((request["fixture_id"] + ":boot").encode())[:32], "cancel")

    def call(self, role, *args):
        if args[0] != "EVALSHA" or role != "ledger":
            return super().call(role, *args)
        self.calls.append((role, *args))
        index = self.target_calls
        self.target_calls += 1
        assert tuple(args) == self.wires[index]
        assert permits(self.rules[role], "EVALSHA", *args[3:3 + int(args[2])])
        before = self.ms
        self.ms += 1
        self.observations.append({"started_at_ms": before, "now_ms": self.ms, "finished_at_ms": self.ms})
        expected = oracle._expected_validated(self.fixture, self.observations, "cancel")[index]
        self.install_model(expected["state"])
        a, b = self.fixture["actors"]["a"], self.fixture["actors"]["b"]
        qkey = h.P + "reservation:" + a["identity"]["reservation_id"]
        ambiguous = {"ambiguous-cancel": 2, "ambiguous-immediate-replay": 3,
            "ambiguous-historical-replay": 5, "ambiguous-start": 6, "ambiguous-finish": 7}
        if self.fault in ambiguous and index == ambiguous[self.fault]:
            raise resp.TransportError("private-ambiguous-cancel-canary")
        if self.fault == "blocked-mutates" and index == 1:
            self.data[b["job_key"]]["claim_count"] = b"1"
        if self.fault == "phantom-origin" and index == 1:
            key = h.P + "rate:" + b["scope_ids"][2]
            self.data[key], self.kinds[key] = {"protocol_version": b"2"}, "hash"
        if self.fault == "cancel-keeps-membership" and index == 2:
            key = h.P + "rate:" + a["scope_ids"][1] + ":pending"
            self.data[key] = {a["identity"]["reservation_id"]: int(self.data[qkey]["expires_at_ms"])}
            self.kinds[key] = "zset"
        if self.fault == "cancel-consumes-start" and index == 2:
            self.data[a["base_key"]]["request_starts"] = b"1"
        if self.fault == "creation-refund" and index == 2:
            self.data[a["base_key"]]["reservation_creations_total"] = b"0"
        if self.fault == "cancel-ends-lease" and index == 2:
            self.data[h.P + "active_leases"].pop(a["run_id"] + ":" + a["job_id"])
        if self.fault == "ttl-extension" and index == 3:
            self.expiries[qkey] += 1
        if self.fault == "replay-refunds-peer" and index == 5:
            self.data[h.P + "rate:" + b["scope_ids"][1]]["active_count"] = b"0"
        if self.fault == "replay-clears-peer" and index == 5:
            self.data[b["job_key"]]["active_reservation_id"] = b""
        if self.fault == "first-start-wrong-owner" and index == 6:
            self.data[h.P + "first_request_start"]["run_id"] = a["run_id"].encode()
        if self.fault == "document-history" and index == 6:
            self.data[b["job_key"]]["last_document_request_started_at_ms"] = str(self.ms).encode()
        if self.fault == "measurement-span" and index == 8:
            self.ms += 30001
        reply = list(expected["reply"])
        if self.fault == "wrong-blocker" and index == 1:
            reply[2] = a["scope_ids"][0]
        if self.fault == "wrong-after-io" and index == 1:
            reply[-1] = "1"
        if self.fault == "wrong-cancel-status" and index == 2:
            reply[0] = "ALREADY_CANCELLED"
        if self.fault == "wrong-cancel-identity" and index == 3:
            reply[2] = b["identity"]["reservation_id"]
        if self.fault == "wrong-start-permission" and index == 6:
            reply[-1] = "0"
        return [value.encode() for value in reply]


class CancellationDocker(SharedDocker):
    def __init__(self, fail=None, fault=None):
        super().__init__(fail, fault)
        self.redis = CancellationRedis(fault)


class SerializedCancellationDocker(SerializedSharedDocker):
    def __init__(self, fail=None, fault=None):
        super().__init__(fail, fault)
        self.redis = CancellationRedis(fault)


class SharedCancellationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plan = base.test_plan(spec.CANCEL_SCENARIO)

    def run_case(self, backend):
        return ctl.execute(self.plan, dict(base.approval(self.plan), max_seconds=300), backend, revision_check=lambda _: None)

    def test_complete_cancellation_through_actual_adapter_has_zero_A_starts_and_no_acceptance_claim(self):
        backend = SerializedCancellationDocker()
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "PASS", report.get("failure_details"))
        self.assertEqual(report["case"], spec.CANCEL_CASE)
        self.assertFalse(report["case_evidence_valid"])
        self.assertFalse(report["m4_accepted"])
        measured = report["stages"]["measure"]["result"]
        self.assertEqual((backend.redis.target_calls, len(measured["steps"]), len(measured["acl_negatives"])), (9, 9, 46))
        self.assertEqual([row["assertion_id"] for row in measured["steps"]], [f"SGCANCEL{i:02}" for i in range(1, 10)])
        final = measured["steps"][-1]
        counts = final["counters"]["after"]
        self.assertEqual(len(counts), 60)
        for actor, starts in (("a", 0), ("b", 1)):
            for key in ("run.request_starts", "job.request_starts", "job.delivery_attempts"):
                self.assertEqual(counts[actor + "." + key], starts)
            self.assertEqual(counts[actor + ".run.claims_total"], 1)
            self.assertEqual(counts[actor + ".run.reservation_creations_total"], 1)
            self.assertEqual(counts[actor + ".job.next_request_ordinal"], 2)
        self.assertEqual([row["state"] for row in final["jobs"]], ["leased", "leased"])
        self.assertEqual([row["state"] for row in final["reservation_expiries"]], ["cancelled", "finished"])
        self.assertEqual([row["lease_delivery_started"] for row in final["jobs"]], [0, 1])
        self.assertEqual(final["history"]["first_started_at_ms"], measured["steps"][6]["now_ms"])
        self.assertTrue(all(row["last_document_request_started_at_ms"] == 0 for row in final["jobs"]))
        self.assertEqual(len(report["cleanup"]), 6)
        self.assertEqual(set(backend.redis.revoked), set(case.ROLES))
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
        self.assertLess(backend.events.index("wait:executor"), backend.events.index("create:revocation"))
        for value in backend.private | backend.passwords:
            self.assertNotIn(value.encode(), h.canonical(report))

    def test_initial_cancel_and_replays_share_status_but_only_first_releases_capacity(self):
        backend = CancellationDocker()
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "PASS")
        rows = report["stages"]["measure"]["result"]["steps"]
        self.assertEqual([rows[index]["response_status"] for index in (2, 3, 5)], ["RESERVATION_CANCELLED"] * 3)
        self.assertNotEqual(rows[2]["state_sha256"], rows[1]["state_sha256"])
        for index in (3, 5):
            self.assertEqual(rows[index]["state_sha256"], rows[index - 1]["state_sha256"])
            self.assertTrue(all(value == 0 for value in rows[index]["counters"]["delta"].values()))
        self.assertEqual(rows[2]["counters"]["delta"]["a.run.pending_request_reservations"], -1)
        self.assertEqual(rows[2]["counters"]["delta"]["a.run.reservation_creations_total"], 0)
        self.assertEqual(rows[5]["counters"]["after"]["b.run.pending_request_reservations"], 1)
        self.assertEqual(rows[5]["counters"]["after"]["scope.group.active_count"], 1)
        self.assertEqual(rows[5]["jobs"][1], rows[4]["jobs"][1])
        self.assertEqual(rows[5]["history"]["first_started_at_ms"], 0)
        expiry = rows[2]["reservation_expiries"][0]["expires_at_ms"]
        self.assertEqual(expiry, rows[2]["now_ms"] + 86400000)
        self.assertEqual([row["reservation_expiries"][0]["expires_at_ms"] for row in rows[2:]], [expiry] * 7)
        for actor, claim_index in ((0, 0), (1, 4)):
            self.assertEqual(rows[-1]["jobs"][actor]["lease_expires_at_ms"], rows[claim_index]["now_ms"] + 60000)

    def test_fixed_registration_fixture_case_binding_and_recipe(self):
        plan, fixture, times, trace = context("cancel-spaced", spec.CANCEL_SCENARIO)
        self.assertEqual(case.case_for_plan(plan), spec.CANCEL_CASE)
        self.assertEqual(fixture["case"], spec.CANCEL_CASE)
        self.assertEqual(oracle.public_summary(plan, fixture)["case"], spec.CANCEL_CASE)
        self.assertEqual(len(oracle.expected_sequence(plan, fixture, times, trace)), 9)
        recipe = case.recipe(spec.CANCEL_CASE)
        self.assertEqual((recipe["measurement_steps"], recipe["effective_mutations"], recipe["exact_replays"],
            recipe["expected_errors"], recipe["capacity_denials"], recipe["request_starts"]), (9, 5, 2, 0, 1, 1))
        self.assertEqual((recipe["possible_keys"], recipe["counter_fields"], recipe["acl_denials"]), (90, 60, 46))
        self.assertEqual(recipe["assertions"], [f"SGCANCEL{i:02}" for i in range(1, 10)])
        self.assertEqual(case.sources(spec.CANCEL_CASE), spec.SOURCES)
        old_plan, old_fixture, _, _ = context()
        self.assertNotEqual(fixture["actors"]["a"]["run_id"], old_fixture["actors"]["a"]["run_id"])
        self.assertNotEqual(fixture["actors"]["a"]["scope_ids"][1], old_fixture["actors"]["a"]["scope_ids"][1])
        with self.assertRaises(h.InvalidArtifact):
            oracle.validate_fixture(old_plan, fixture)
        with self.assertRaises(h.InvalidArtifact):
            oracle.validate_fixture(plan, old_fixture)
        with self.assertRaisesRegex(h.InvalidArtifact, "SHARED_ACL_CASE"):
            case.acl_rules(spec.CANCEL_CASE, old_fixture, old_plan)
        with self.assertRaisesRegex(h.InvalidArtifact, "SHARED_ACL_CASE"):
            case.acl_rules(spec.CASE, fixture, plan)
        self.assertEqual(case.recipe(spec.CASE)["measurement_steps"], 21)

    def test_faults_refuse_mutated_state_reply_history_and_expiry_without_retry(self):
        faults = (("wrong-blocker", 2, 1), ("wrong-after-io", 2, 1), ("blocked-mutates", 2, 1),
            ("phantom-origin", 2, 1), ("cancel-keeps-membership", 3, 2), ("cancel-consumes-start", 3, 2),
            ("creation-refund", 3, 2), ("cancel-ends-lease", 3, 2), ("wrong-cancel-status", 3, 2),
            ("wrong-cancel-identity", 4, 3), ("ttl-extension", 4, 3), ("replay-refunds-peer", 6, 5),
            ("replay-clears-peer", 6, 5), ("first-start-wrong-owner", 7, 6), ("document-history", 7, 6),
            ("wrong-start-permission", 7, 6), ("measurement-span", 9, 8), ("acl-success", 9, 9))
        for fault, calls, prefix in faults:
            with self.subTest(fault=fault):
                backend = CancellationDocker(fault=fault)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertEqual(report["failure_phase"], "measure")
                result = report["stages"]["measure"]["result"]
                self.assertEqual((backend.redis.target_calls, len(result["steps"])), (calls, prefix))
                self.assertEqual(result["failed_assertion"], "ACL" if prefix == 9 else f"SGCANCEL{prefix + 1:02}")
                self.assertEqual(report["revocation"], "verified")
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_ambiguous_cancel_replays_start_and_finish_keep_real_adapter_prefix(self):
        for fault, calls, prefix in (("ambiguous-cancel", 3, 2), ("ambiguous-immediate-replay", 4, 3),
                ("ambiguous-historical-replay", 6, 5), ("ambiguous-start", 7, 6), ("ambiguous-finish", 8, 7)):
            with self.subTest(fault=fault):
                backend = SerializedCancellationDocker(fault=fault)
                report = self.run_case(backend)
                self.assertEqual(report["failure_details"]["code"], "EXECUTOR_STAGE_FAILED")
                result = report["stages"]["measure"]["result"]
                self.assertEqual((backend.redis.target_calls, len(result["steps"]), result["failed_assertion"]),
                    (calls, prefix, f"SGCANCEL{prefix + 1:02}"))
                self.assertEqual(report["revocation"], "verified")
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
                for value in backend.private | backend.passwords | {"private-ambiguous-cancel-canary", "private-diagnostic-canary"}:
                    self.assertNotIn(value.encode(), h.canonical(report))

    def test_actual_worker_serializes_the_cancellation_failure(self):
        backend = CancellationDocker(fault="ambiguous-cancel")
        report = self.run_case(backend)
        result, request = report["stages"]["measure"]["result"], backend.requests["measure"]
        class Pipe:
            def __init__(self, raw=b""):
                self.buffer = io.BytesIO(raw)
        output = Pipe()
        with patch.object(worker.sys, "argv", ["executor.py", "measure", "30000"]), \
                patch.object(worker.sys, "stdin", Pipe(h.canonical(request))), \
                patch.object(worker.sys, "stdout", output), \
                patch.object(worker.sys, "stderr", io.StringIO()), \
                patch.object(worker, "stage_deadline", lambda _: nullcontext()), \
                patch.object(worker, "environment", return_value=base.fake_isolation("measure")), \
                patch.object(worker, "measure", side_effect=live.SharedFailure(copy.deepcopy(result))):
            self.assertEqual(worker.main(), 1)
        envelope = h.decode(output.buffer.getvalue())
        self.assertEqual(envelope["status"], "FAIL")
        self.assertEqual((envelope["result"]["failed_assertion"], len(envelope["result"]["steps"])), ("SGCANCEL03", 2))
        live.validate_stage_result("measure", envelope["result"], request, successful=False)

    def test_all_ten_failure_boundaries_are_case_bound_and_require_complete_rows_before_ACL(self):
        backend = CancellationDocker()
        report = self.run_case(backend)
        result, request = report["stages"]["measure"]["result"], backend.requests["measure"]
        for completed in range(10):
            value = {key: copy.deepcopy(result[key]) for key in ("scope", "fixture_sha256", "steps", "acl_negatives", "clock_reference_ms")}
            value["steps"] = value["steps"][:completed]
            value["acl_negatives"] = []
            value["failed_assertion"] = "ACL" if completed == 9 else f"SGCANCEL{completed + 1:02}"
            live.validate_stage_result("measure", value, request, successful=False)
            value["failed_assertion"] = f"SGC{completed + 1:02}"
            with self.assertRaises(h.InvalidArtifact):
                live.validate_stage_result("measure", value, request, successful=False)
        value.update(steps=[], acl_negatives=[], clock_reference_ms=None, failed_assertion="STATE")
        live.validate_stage_result("measure", value, request, successful=False)
        for change in (lambda value: value.update(steps=value["steps"][:-1]),
                lambda value: value.update(steps=value["steps"] + [value["steps"][-1]]),
                lambda value: value.update(acl_negatives=value["acl_negatives"][:-1])):
            altered = copy.deepcopy(result)
            change(altered)
            with self.assertRaises(h.InvalidArtifact):
                live.validate_stage_result("measure", altered, request)

    def test_cancellation_receipt_forgery_is_rejected(self):
        backend = CancellationDocker()
        report = self.run_case(backend)
        result, request = report["stages"]["measure"]["result"], backend.requests["measure"]
        for change in (lambda value: value.update(scope=spec.CASE),
                lambda value: value["steps"][2].update(assertion_id="SGC03"),
                lambda value: value["steps"][3].update(response_status="ALREADY_CANCELLED"),
                lambda value: value["steps"][3].update(now_ms=None),
                lambda value: value["steps"][2]["counters"]["after"].update({"a.run.request_starts": True}),
                lambda value: value["steps"][5]["counters"]["after"].update({"b.run.pending_request_reservations": 0}),
                lambda value: value["steps"][5]["reservation_expiries"][0].update(expires_at_ms=1),
                lambda value: value["steps"][6]["history"].update(first_started_at_ms=0),
                lambda value: value["steps"][5]["jobs"][1].update(lease_expires_at_ms=1),
                lambda value: value["steps"][1]["capacity_denial"].update(after_io=1)):
            altered = copy.deepcopy(result)
            change(altered)
            with self.assertRaises(h.InvalidArtifact):
                live.validate_stage_result("measure", altered, request)

    def test_cross_case_resume_and_caller_trace_controls_are_rejected(self):
        backend = CancellationDocker()
        report = self.run_case(backend)
        result, request = report["stages"]["resume"]["result"], backend.requests["resume"]
        for change in (lambda value: value["probe_evidence"].update(case=spec.CASE),
                lambda value: value["fixture_summary"].update(case=spec.CASE),
                lambda value: value.update(setup_time_ms=True),
                lambda value: value["early_revocation"]["boot"].update(server_reachable=False)):
            altered = copy.deepcopy(result)
            change(altered)
            with self.assertRaises(h.InvalidArtifact):
                live.validate_resume(altered, request, request["previous"])
        for field in ("trace", "profile", "steps", "expected", "wires", "endpoint"):
            altered = copy.deepcopy(backend.requests["measure"])
            altered[field] = "finish"
            with self.assertRaises(h.InvalidArtifact):
                worker.validate_request(altered, "measure")
        for stage in ("cancel", "rate_before", "rate_clock", "recover", "claim_park"):
            with self.assertRaises(h.InvalidArtifact):
                worker.validate_request(backend.requests["measure"], stage)

    def test_setup_interrupt_and_revocation_failures_still_clean_up(self):
        for phase in ("resume", "measure", "interrupt", "revoke"):
            backend = CancellationDocker(fail=phase)
            report = self.run_case(backend)
            self.assertEqual(report["verdict"], "FAIL")
            self.assertFalse(report["case_evidence_valid"])
            self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
        backend = CancellationDocker(fault="partial-setup")
        report = self.run_case(backend)
        self.assertEqual((report["verdict"], report["failure_phase"], backend.redis.target_calls), ("FAIL", "resume", 0))
        self.assertEqual(report["revocation"], "verified")

    def test_case_ACLs_remain_closed_to_exact_owned_keys_and_read_only_observer(self):
        plan, fixture, _, _ = context("cancel-spaced", spec.CANCEL_SCENARIO)
        rules = case.acl_rules(spec.CANCEL_CASE, fixture, plan)
        self.assertEqual(set(rules), set(case.ROLES))
        self.assertFalse(permits(rules["setup"], "HSET", h.AUTH[0]))
        self.assertTrue(permits(rules["boot"], "HSET", h.AUTH[0]))
        for actor in fixture["actors"].values():
            self.assertTrue(permits(rules["ledger"], "HSET", h.P + "reservation:" + actor["identity"]["reservation_id"]))
            self.assertFalse(permits(rules["observer"], "HSET", actor["job_key"]))
        for key in (*h.AUTH, "unowned-key"):
            for command in ("SET", "HSET", "DEL", "EXPIRE", "RENAME"):
                self.assertFalse(permits(rules["ledger"], command, key))

    def test_cancellation_clock_limit_exact_boundary_and_replay_expiry(self):
        plan, fixture, times, trace = context("cancel-spaced", spec.CANCEL_SCENARIO)
        times[-1]["finished_at_ms"] = times[0]["started_at_ms"] + 30000
        rows = oracle.expected_sequence(plan, fixture, times, trace)
        a = fixture["actors"]["a"]
        self.assertEqual(len(rows), 9)
        self.assertEqual(fields(rows[-1]["state"], a["base_key"])["request_starts"], "0")
        times[-1]["finished_at_ms"] += 1
        with self.assertRaisesRegex(h.InvalidArtifact, "SHARED_MEASURE_SPAN"):
            oracle.expected_sequence(plan, fixture, times, trace)


if __name__ == "__main__":
    unittest.main()
