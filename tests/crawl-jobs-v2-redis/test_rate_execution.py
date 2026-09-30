"""Bounded rate lifecycle/failure simulations; not real time or acceptance."""
import copy
import unittest
from unittest.mock import patch

import controller as ctl
import executor as worker
import harness as h
import rate_executor as live
import rate_specs as spec
import request_oracle as oracle
import runtime_case as case
import resp
import test_execution as base
from test_claim_execution import ClaimRedis, permits


class RateRedis(ClaimRedis):
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
        index = self.target_calls
        self.target_calls += 1
        self.calls.append((role, *args))
        assert tuple(args) == self.wires[index] and permits(self.rules[role], "EVALSHA", *args[3:3 + int(args[2])])
        before = self.ms
        self.ms += 1
        observation = {"started_at_ms": before, "now_ms": None if index in spec.ERRORS else self.ms, "finished_at_ms": self.ms}
        self.observations.append(observation)
        expected = oracle._expected_validated(self.fixture, self.observations)[index]
        self.install_model(expected["state"])
        if self.fault == "ambiguous-reserve" and index == 12:
            raise resp.TransportError("private-ambiguous-canary")
        if self.fault == "counter-change" and index == 13:
            self.data[self.fixture["base_key"]]["request_starts"] = b"99"
        if self.fault == "ttl-change" and index == 11:
            self.expiries[h.P + "reservation:" + self.fixture["identities"]["a"]["reservation_id"]] += 1
        if index in spec.ERRORS:
            raise resp.RedisError(("ERR " + spec.STEPS[index][2]).encode())
        reply = list(expected["reply"])
        if self.fault == "wrong-blocker" and index == 11:
            reply[2] = self.fixture["scope_ids"][2]
        if self.fault == "wrong-deadline" and index == 11:
            reply[3] = str(int(reply[3]) + 1)
        if self.fault == "wrong-after-io" and index == 11:
            reply[4] = "0"
        return [part.encode() for part in reply]


class RateDocker(base.FakeDocker):
    def __init__(self, fail=None, fault=None, serialized=False):
        super().__init__(fail)
        self.redis = RateRedis(fault)
        self.fault, self.serialized = fault, serialized
        self.private, self.requests, self.clock_requests = set(), {}, []

    def kill(self, name):
        super().kill(name)
        if name.endswith("-redis"):
            self.redis.run_id = "b" * 40

    def wait_interval(self, seconds):
        assert 0 < seconds <= 2
        if self.fault != "never-due":
            self.redis.ms += int(seconds * 1000)

    def stage(self, name, stage, request, timeout):
        self.events.append(stage)
        self.requests[stage] = copy.deepcopy(request)
        self.passwords.update(request["credentials"].values())
        self.private.update(request["claim_material"].values())
        if stage == "rate_clock":
            self.clock_requests.append(copy.deepcopy(request))
            assert set(request["credentials"]) == {"observer"}
        if self.fail == stage:
            raise ctl.CommandError("private-stage-canary")
        if self.fail == "interrupt" and stage == "rate_after":
            raise KeyboardInterrupt()
        worker.validate_request(request, stage)
        envelope = {"stage": stage, "status": "PASS", "recipe_sha256": case.recipe_sha256(spec.CASE), "isolation": base.fake_isolation(stage)}
        code = 0
        if stage == "init":
            binding = case.fixture(request["plan"], request["fixture_id"], request["claim_material"])
            self.redis.rules = case.acl_rules(spec.CASE, binding, request["plan"])
            self.private.update(binding["identities"][label]["reservation_id"] for label in ("a", "b"))
            envelope["result"] = {"empty_volumes_verified": True, "config_sha256": request["plan"]["redis_config"]["sha256"],
                "acl_file_sha256": h.digest(case.acl_file(request["credentials"], spec.CASE, binding, request["plan"]))}
        else:
            if stage == "resume":
                self.redis.configure(request)
            if stage == "rate_after" and self.fault == "wait-state-change":
                self.redis.data[self.redis.fixture["base_key"]]["request_starts"] = b"9"
            with patch.object(worker, "connect", self.redis.connect):
                try:
                    if stage == "revoke":
                        result = {"revocation": worker.revoke(request["credentials"], case.ROLES)}
                    elif stage in spec.PHASES:
                        result = getattr(live, stage)(request, worker)
                    else:
                        result = getattr(worker, stage)(request)
                    envelope["result"] = result
                except live.RateFailure as failure:
                    envelope.update(status="FAIL", result=failure.result)
                    code = 1
        if stage == "rate_clock" and self.fault == "wrong-clock-run":
            envelope["result"]["run_id"] = "c" * 40
        if self.serialized:
            with patch.object(ctl.shutil, "which", return_value="/fake/docker"):
                adapter = ctl.Docker()
            adapter.deadline, adapter.approval_expires_at_ms = self.deadline, self.approval_expires_at_ms
            with patch.object(ctl, "command", return_value=(code, h.canonical(envelope), b"private-diagnostic-canary")):
                return adapter.stage(name, stage, request, timeout)
        if code:
            raise ctl.StageFailure(envelope)
        return envelope


class PositiveRateExecutionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plan = base.test_plan(spec.SCENARIO)

    def run_case(self, backend):
        return ctl.execute(self.plan, dict(base.approval(self.plan), max_seconds=300), backend, revision_check=lambda _: None)

    def test_complete_simulation_and_observer_only_wait(self):
        backend = RateDocker(serialized=True)
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "PASS", report.get("failure_details"))
        self.assertFalse(report["case_evidence_valid"])
        self.assertFalse(report["m4_accepted"])
        before, after = [report["stages"][phase]["result"] for phase in ("rate_before", "rate_after")]
        self.assertEqual((len(before["steps"]), len(after["steps"]), backend.redis.target_calls), (12, 24, 24))
        self.assertEqual(after["steps"][:12], before["steps"])
        self.assertEqual(before["steps"][-1]["rate_denial"]["after_io"], 1)
        self.assertEqual(len(after["acl_negatives"]), 46)
        self.assertEqual(len(report["rate_clock_stages"]), len(report["rate_wait"]["observations"]))
        self.assertTrue(backend.clock_requests)
        self.assertEqual(report["revocation"], "verified")
        self.assertEqual(len(report["cleanup"]), 6)
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
        for value in backend.private | backend.passwords | {case.claim.URL, case.claim.ROBOTS}:
            self.assertNotIn(value.encode(), h.canonical(report))

    def test_actual_adapter_retains_prefix_and_never_retries(self):
        for fault, stage, prefix, calls in (("wrong-blocker", "rate_before", 11, 12), ("wrong-deadline", "rate_before", 11, 12),
                ("wrong-after-io", "rate_before", 11, 12), ("ttl-change", "rate_before", 11, 12),
                ("ambiguous-reserve", "rate_after", 12, 13), ("counter-change", "rate_after", 13, 14), ("acl-success", "rate_after", 24, 24)):
            with self.subTest(fault=fault):
                backend = RateDocker(fault=fault, serialized=True)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertEqual(report["failure_details"]["code"], "EXECUTOR_STAGE_FAILED")
                self.assertEqual(len(report["stages"][stage]["result"]["steps"]), prefix)
                self.assertEqual(backend.redis.target_calls, calls)
                self.assertEqual(report["revocation"], "verified")
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
                self.assertNotIn(b"private-diagnostic-canary", h.canonical(report))

    def test_wait_bounds_identity_and_full_state_are_enforced(self):
        for fault in ("never-due", "wrong-clock-run", "wait-state-change"):
            with self.subTest(fault=fault):
                backend = RateDocker(fault=fault)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertLessEqual(len(backend.clock_requests), spec.MAX_CLOCK_OBSERVATIONS)
                self.assertEqual(backend.redis.target_calls, 12)
                self.assertEqual(report["revocation"], "verified")
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_receipt_and_cross_phase_tampering_reject(self):
        backend = RateDocker()
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "PASS")
        request = backend.requests["rate_after"]
        result = report["stages"]["rate_after"]["result"]
        for mutate in (lambda value: value.update(pre_rate_sha256="0" * 64), lambda value: value.update(wait_sha256="0" * 64),
                       lambda value: value["steps"][11]["rate_denial"].update(after_io=True),
                       lambda value: value["steps"][16]["counters"]["after"].update({"run.request_starts": 99})):
            changed = copy.deepcopy(result)
            mutate(changed)
            with self.assertRaises(h.InvalidArtifact):
                live.validate_stage_result("rate_after", changed, request)
        for mutate in (lambda value: value["previous"]["wait"]["observations"][-1].update(now_ms=live.deadline(value["previous"]["prefix"]) - 1),
                       lambda value: value["previous"]["wait"]["observations"][-1].update(deadline_ms=True),
                       lambda value: value["previous"]["prefix"]["steps"][11]["rate_denial"].update(next_allowed_ms=1)):
            changed = copy.deepcopy(request)
            mutate(changed)
            with self.assertRaises(h.InvalidArtifact):
                live.validate_stage_result("rate_after", result, changed)

    def test_short_budget_and_cross_case_phases_reject(self):
        backend = RateDocker()
        with self.assertRaisesRegex(h.InvalidArtifact, "RATE_TIME_LIMIT"):
            ctl.execute(self.plan, dict(base.approval(self.plan), max_seconds=119), backend, revision_check=lambda _: None)
        self.assertEqual(backend.events, [])
        for selected in (case.CASE, case.CLAIM_CASE, "ledger-request-lifecycle-v1", "ledger-worker-death-pre-io-v1"):
            for phase in spec.PHASES:
                with self.assertRaises(h.InvalidArtifact):
                    case.stage_roles(selected, phase)
        with self.assertRaises(h.InvalidArtifact):
            case.stage_roles(spec.CASE, "measure")

    def test_stage_failure_and_interrupt_preserve_cleanup(self):
        for failure in ("rate_before", "rate_clock", "rate_after", "interrupt", "revoke"):
            with self.subTest(failure=failure):
                backend = RateDocker(fail=failure)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertFalse(report["case_evidence_valid"])
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})


if __name__ == "__main__":
    unittest.main()
