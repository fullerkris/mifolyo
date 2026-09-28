"""Simulated request lifecycle boundaries; never real Redis or acceptance."""
import copy
import unittest
from unittest.mock import patch

import controller as ctl
import executor as worker
import harness as h
import request_executor as live
import request_oracle as oracle
import request_specs as spec
import runtime_case as case
import resp
import test_execution as base
from test_claim_execution import ClaimRedis, permits


class RequestRedis(ClaimRedis):
    def __init__(self, fault=None):
        super().__init__(fault)
        self.observations, self.target_calls = [], 0

    def configure(self, request):
        self.plan = request["plan"]
        self.fixture = case.fixture(self.plan, request["fixture_id"], request["claim_material"], self.ms)
        self.wires = oracle.wire_requests(self.plan, self.fixture, h.digest((request["fixture_id"] + ":boot").encode())[:32])

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
        observation = {"started_at_ms": before, "now_ms": None if index in spec.ERRORS else self.ms, "finished_at_ms": self.ms}
        self.observations.append(observation)
        expected = oracle._expected_validated(self.fixture, self.observations)[index]
        self.install_model(expected["state"])
        if (self.fault == "ambiguous-start" and index == 5) or (self.fault == "ambiguous-finish" and index == 18):
            raise resp.TransportError("private-ambiguous-canary")
        if self.fault == "counter-change" and index == 6:
            self.data[self.fixture["base_key"]]["request_starts"] = b"9"
        if self.fault == "ttl-extension" and index == 8:
            self.expiries[h.P + "reservation:" + self.fixture["identities"]["a"]["reservation_id"]] += 1
        if index in spec.ERRORS:
            if self.fault == "wrong-error" and index == 2:
                raise resp.RedisError(b"ERR CRAWL_V2_NO_CAPACITY private-canary")
            raise resp.RedisError(("ERR " + spec.STEPS[index][2]).encode())
        reply = list(expected["reply"])
        if self.fault == "historical-permission" and index == 16:
            reply[-1] = "1"
        return [value.encode() for value in reply]


class RequestDocker(base.FakeDocker):
    def __init__(self, fail=None, fault=None):
        super().__init__(fail)
        self.redis = RequestRedis(fault)
        self.requests, self.private = {}, set()

    def kill(self, name):
        super().kill(name)
        if name.endswith("-redis"):
            self.redis.run_id = "b" * 40

    def stage(self, name, stage, request, timeout):
        self.events.append(stage)
        self.requests[stage] = copy.deepcopy(request)
        self.passwords.update(request["credentials"].values())
        self.private.update(request["claim_material"].values())
        if self.fail == stage:
            raise ctl.CommandError("simulated private failure")
        if self.fail == "interrupt" and stage == "measure":
            raise KeyboardInterrupt()
        worker.validate_request(request, stage)
        envelope = {"stage": stage, "status": "PASS", "recipe_sha256": case.recipe_sha256(spec.CASE), "isolation": base.fake_isolation(stage)}
        if stage == "init":
            binding = case.fixture(request["plan"], request["fixture_id"], request["claim_material"])
            self.redis.rules = case.acl_rules(spec.CASE, binding, request["plan"])
            self.private.update(binding["identities"][label]["reservation_id"] for label in ("a", "b"))
            envelope["result"] = {"empty_volumes_verified": True, "config_sha256": request["plan"]["redis_config"]["sha256"],
                "acl_file_sha256": h.digest(case.acl_file(request["credentials"], spec.CASE, binding, request["plan"]))}
        else:
            if stage == "resume":
                self.redis.configure(request)
            with patch.object(worker, "connect", self.redis.connect):
                try:
                    envelope["result"] = {"revocation": worker.revoke(request["credentials"], case.ROLES)} if stage == "revoke" else getattr(worker, stage)(request)
                except live.RequestFailure as failure:
                    envelope.update(status="FAIL", result=failure.result)
                    raise ctl.StageFailure(envelope) from None
        return envelope


class SerializedRequestDocker(RequestDocker):
    """Exercise the real adapter parser with only process execution replaced."""
    def stage(self, name, stage, request, timeout):
        try:
            envelope = super().stage(name, stage, request, timeout)
            code = 0
        except ctl.StageFailure as failure:
            envelope, code = failure.receipt, 1
        with patch.object(ctl.shutil, "which", return_value="/fake/docker"):
            adapter = ctl.Docker()
        adapter.deadline = self.deadline
        adapter.approval_expires_at_ms = self.approval_expires_at_ms
        with patch.object(ctl, "command", return_value=(code, h.canonical(envelope), b"private-diagnostic-canary")):
            return adapter.stage(name, stage, request, timeout)


class RequestExecutionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plan = base.test_plan(spec.SCENARIO)

    def run_case(self, backend):
        return ctl.execute(self.plan, dict(base.approval(self.plan), max_seconds=300), backend, revision_check=lambda _: None)

    def test_complete_facade_is_redacted_and_never_real_acceptance(self):
        backend = RequestDocker()
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "PASS", report.get("failure_details"))
        self.assertFalse(report["case_evidence_valid"])
        self.assertFalse(report["m4_accepted"])
        measured = report["stages"]["measure"]["result"]
        self.assertEqual((backend.redis.target_calls, len(measured["steps"]), len(measured["acl_negatives"])), (22, 22, 46))
        final = measured["steps"][-1]
        self.assertEqual(final["counters"]["after"]["run.request_starts"], 2)
        self.assertEqual(final["counters"]["after"]["job.delivery_attempts"], 1)
        self.assertEqual(final["history"]["document_fence"], 1)
        self.assertEqual(len(report["cleanup"]), 6)
        self.assertEqual(set(backend.redis.revoked), set(case.ROLES))
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
        self.assertLess(backend.events.index("wait:executor"), backend.events.index("create:revocation"))
        raw = h.canonical(report)
        for value in backend.private | backend.passwords | {case.claim.URL, case.claim.ROBOTS}:
            self.assertNotIn(value.encode(), raw)

    def test_faults_keep_only_valid_prefix_never_retry_and_revoke(self):
        for fault, calls, prefix in (("wrong-error", 3, 2), ("ambiguous-start", 6, 5), ("counter-change", 7, 6),
                ("ttl-extension", 9, 8), ("historical-permission", 17, 16), ("ambiguous-finish", 19, 18), ("acl-success", 22, 22)):
            with self.subTest(fault=fault):
                backend = RequestDocker(fault=fault)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertEqual(report["failure_phase"], "measure")
                self.assertEqual(backend.redis.target_calls, calls)
                self.assertEqual(len(report["stages"]["measure"]["result"]["steps"]), prefix)
                self.assertEqual(report["revocation"], "verified")
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
                self.assertNotIn(b"private-canary", h.canonical(report))

    def test_real_adapter_retains_serialized_ambiguous_failure_prefix(self):
        for fault, calls, prefix, assertion in (("ambiguous-start", 6, 5, "REQ06"), ("ambiguous-finish", 19, 18, "REQ19")):
            with self.subTest(fault=fault):
                backend = SerializedRequestDocker(fault=fault)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertEqual(report["failure_details"]["code"], "EXECUTOR_STAGE_FAILED")
                result = report["stages"]["measure"]["result"]
                self.assertEqual((len(result["steps"]), result["failed_assertion"]), (prefix, assertion))
                self.assertEqual(backend.redis.target_calls, calls)
                self.assertEqual(report["revocation"], "verified")
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
                self.assertLess(backend.events.index("wait:executor"), backend.events.index("create:revocation"))
                self.assertNotIn(b"private-diagnostic-canary", h.canonical(report))

    def test_failure_location_must_match_completed_prefix(self):
        backend = RequestDocker(fault="ambiguous-finish")
        report = self.run_case(backend)
        result, request = report["stages"]["measure"]["result"], backend.requests["measure"]
        live.validate_stage_result("measure", result, request, successful=False)
        for assertion in ("STATE", "REQ01", "REQ18", "REQ20", "REQ22", "ACL", None, True, []):
            with self.subTest(assertion=assertion):
                changed = copy.deepcopy(result)
                changed["failed_assertion"] = assertion
                with self.assertRaises(h.InvalidArtifact):
                    live.validate_stage_result("measure", changed, request, successful=False)

    def test_partial_setup_measurement_interrupt_and_revocation_fail_closed(self):
        for phase in ("resume", "measure", "interrupt", "revoke"):
            with self.subTest(phase=phase):
                backend = RequestDocker(fail=phase)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertFalse(report["case_evidence_valid"])
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_full_receipt_reconstruction_rejects_forgery(self):
        backend = RequestDocker()
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "PASS")
        result, request = report["stages"]["measure"]["result"], backend.requests["measure"]
        for change in (
            lambda value: value["steps"][5]["counters"]["after"].update({"run.request_starts": True}),
            lambda value: value["steps"][2].update(now_ms=value["steps"][2]["started_at_ms"]),
            lambda value: value["steps"][16].update(response_sha256=value["steps"][15]["response_sha256"]),
            lambda value: value["steps"][20]["history"].update(first_started_at_ms=0),
            lambda value: value["steps"][21]["rate_scopes"][1].update(next_allowed_ms=0),
            lambda value: value["steps"][8]["reservation_expiries"][0].update(expires_at_ms=1),
            lambda value: value.update(extra="private-canary"),
        ):
            altered = copy.deepcopy(result)
            change(altered)
            with self.assertRaises(h.InvalidArtifact):
                live.validate_stage_result("measure", altered, request)

    def test_new_acl_writes_are_exact_and_have_no_authority_mutation(self):
        from test_request_oracle import context
        plan, f, _ = context()
        rules = case.acl_rules(spec.CASE, f, plan)["ledger"]
        for key in (h.P + "first_request_start", f["base_key"] + ":group_started", f["base_key"] + ":group_active_started"):
            self.assertTrue(permits(rules, "HSET", key))
        for scope in f["scope_ids"]:
            self.assertTrue(permits(rules, "ZADD", h.P + "rate:" + scope + ":started"))
            self.assertTrue(permits(rules, "ZREM", h.P + "rate:" + scope + ":started"))
        for key in (*h.AUTH, "foreign-key"):
            for command in ("HSET", "SET", "DEL", "EXPIRE", "RENAME"):
                self.assertFalse(permits(rules, command, key))


if __name__ == "__main__":
    unittest.main()
