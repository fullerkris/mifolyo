"""Recovery lifecycle/failure facades; never Docker, real Redis or acceptance."""
import copy
import runpy
import unittest
from unittest.mock import patch

import controller as ctl
import executor as worker
import harness as h
import recovery_executor as live
import recovery_oracle as oracle
import recovery_specs as spec
import runtime_case as case
import resp
import parked_command
import test_execution as base
from test_claim_execution import ClaimRedis, permits


class RecoveryRedis(ClaimRedis):
    def __init__(self, fault=None):
        super().__init__(fault)
        self.times, self.setup_time = [], self.ms
        self.target_calls = 0
        self.current_stage = None

    def configure(self, request, stage):
        self.plan = request["plan"]
        self.current_stage = stage
        if stage == "resume":
            self.setup_time = self.ms
        self.fixture = case.claim_fixture(self.plan, request["fixture_id"], request["claim_material"], self.setup_time)
        self.wires = oracle.wire_requests(self.plan, self.fixture, h.digest((request["fixture_id"] + ":boot").encode())[:32])

    def call(self, role, *args):
        if args[0] == "TIME" and self.fault == "clock-backwards" and self.current_stage == "lease_clock":
            value = self.times[-1] - 1
            return [str(value // 1000).encode(), str(value % 1000 * 1000).encode()]
        if args[0] != "EVALSHA" or role != "ledger":
            return super().call(role, *args)
        self.calls.append((role, *args))
        index = self.target_calls
        self.target_calls += 1
        assert tuple(args) == self.wires[index]
        assert permits(self.rules[role], "EVALSHA", *args[3:3 + int(args[2])])
        self.ms += 1
        times = [*self.times, self.ms]
        projection = oracle.expected_sequence(self.plan, self.fixture, oracle.prefix_times(times))[index]
        self.install_model(projection["state"])
        self.times.append(self.ms)
        if self.fault == "ambiguous-recover" and index == 2:
            raise resp.TransportError("private-ambiguous-canary")
        if self.fault == "wrong-counter" and index == 3:
            self.data[self.fixture["base_key"]]["recovered_leases_total"] = b"9"
        if self.fault == "extended-tombstone" and index == 3:
            key = h.P + "reservation:" + self.fixture["identities"]["a"]["reservation_id"]
            self.expiries[key] += 1
        return [value.encode() for value in projection["reply"]]


class FakeParked:
    def __init__(self, fault=None):
        self.received_at = ctl.time.monotonic() - (2 if fault == "kill-delay" else 0)
        self.fault, self.aborted = fault, False
    def require_waiting(self):
        if self.fault == "early-exit":
            raise parked_command.ParkedCommandError("PARK_NOT_WAITING")
    def finish(self, _):
        return 0 if self.fault == "cli-exit" else 137
    def abort(self):
        self.aborted = True


class RecoveryDocker(base.FakeDocker):
    def __init__(self, fail=None, fault=None):
        super().__init__(fail)
        self.redis = RecoveryRedis(fault)
        self.fault, self.requests, self.private = fault, {}, set()
        self.parked = None

    def timeout(self, requested=30):
        return ctl.Docker.timeout(self, requested)

    def create(self, spec):
        super().create(spec)
        value = self.resources[("container", spec["name"])]
        value["Id"] = h.digest(spec["name"].encode())
        value["State"].update(Status="created", ExitCode=0, OOMKilled=False)
        if self.fail == "replacement" and spec["role"] == "executor_b":
            raise ctl.CommandError("ambiguous replacement create")

    def start(self, name):
        super().start(name)
        self.resources[("container", name)]["State"]["Status"] = "running"

    def kill(self, name):
        super().kill(name)
        state = self.resources[("container", name)]["State"]
        state.update(Status="exited", ExitCode=137)
        if name.endswith("-redis"):
            self.redis.run_id = "b" * 40
        elif self.fault == "bad-exit":
            state["ExitCode"] = 0
        elif self.fault == "lost-kill-reply":
            raise ctl.CommandError("ambiguous kill")

    def wait_interval(self, seconds):
        assert 0 < seconds <= 2
        if self.fault != "clock-never-due":
            # Simulated-clock advancement is deliberately not elapsed-time proof.
            self.redis.ms = self.redis.times[0] + 60000

    def stage(self, name, stage, request, timeout):
        self.events.append(stage)
        self.requests[stage] = copy.deepcopy(request)
        self.passwords.update(request["credentials"].values())
        self.private.update(request["claim_material"].values())
        if self.fail == stage:
            raise ctl.CommandError("simulated stage failure")
        worker.validate_request(request, stage)
        envelope = {"stage": stage, "status": "PASS", "recipe_sha256": case.recipe_sha256(spec.CASE), "isolation": base.fake_isolation(stage)}
        self.redis.configure(request, stage)
        if stage == "init":
            binding = case.fixture(request["plan"], request["fixture_id"], request["claim_material"])
            self.redis.rules = case.acl_rules(spec.CASE, binding, request["plan"])
            self.private.update(binding["identities"][label]["reservation_id"] for label in ("a", "b"))
            envelope["result"] = {"empty_volumes_verified": True, "config_sha256": request["plan"]["redis_config"]["sha256"],
                                  "acl_file_sha256": h.digest(case.acl_file(request["credentials"], spec.CASE, binding, request["plan"]))}
        else:
            with patch.object(worker, "connect", self.redis.connect):
                try:
                    if stage == "revoke":
                        result = {"revocation": worker.revoke(request["credentials"], case.roles(spec.CASE))}
                    elif stage in spec.PHASES:
                        result = getattr(live, stage)(request, worker)
                    else:
                        result = getattr(worker, stage)(request)
                    envelope["result"] = result
                except live.RecoveryFailure as failure:
                    envelope.update(status="FAIL", result=failure.result)
                    raise ctl.StageFailure(envelope) from None
        return envelope

    def park_stage(self, name, request, timeout):
        self.parked = FakeParked(self.fault)
        self.parked.require_waiting()
        receipt = self.stage(name, "claim_park", request, timeout)
        self.parked.received_at = ctl.time.monotonic() - (2 if self.fault == "kill-delay" else 0)
        return self.parked, receipt

    def kill_claimant(self, name, fixture_id, expected_id, parked):
        if self.fault == "wrong-worker-id":
            expected_id = "0" * 64
        if self.fault == "oom":
            self.resources[("container", name)]["State"]["OOMKilled"] = True
        return ctl.Docker.kill_claimant(self, name, fixture_id, expected_id, parked)


class RecoveryExecutionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plan = base.test_plan(spec.SCENARIO)

    def run_case(self, backend):
        approved = dict(base.approval(self.plan), max_seconds=300)
        return ctl.execute(self.plan, approved, backend, revision_check=lambda _: None)

    def test_complete_simulated_lifecycle_and_no_real_acceptance(self):
        backend = RecoveryDocker()
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "PASS", report.get("failure_phase"))
        self.assertFalse(report["case_evidence_valid"])
        self.assertFalse(report["m4_accepted"])
        self.assertEqual(backend.redis.target_calls, 13)
        self.assertEqual(len(report["stages"]["recover"]["result"]["steps"]), 11)
        self.assertEqual(len(report["stages"]["recover"]["result"]["acl_negatives"]), 46)
        self.assertEqual(len(report["cleanup"]), 7)
        self.assertEqual(len(report["worker_quiescence"]["workers"]), 2)
        self.assertEqual(set(backend.redis.revoked), set(case.roles(spec.CASE)))
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
        self.assertLess(backend.events.index("kill:executor"), backend.events.index("create:executor_b"))
        self.assertLess(backend.events.index("wait:b"), backend.events.index("create:revocation"))
        raw = h.canonical(report)
        for value in backend.private | backend.passwords | {case.claim.URL, case.claim.ROBOTS}:
            self.assertNotIn(value.encode(), raw)
        self.assertTrue(backend.parked.aborted)

    def test_death_uncertainty_never_starts_replacement(self):
        for fault in ("early-exit", "kill-delay", "bad-exit", "cli-exit", "wrong-worker-id", "oom", "lost-kill-reply"):
            with self.subTest(fault=fault):
                backend = RecoveryDocker(fault=fault)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertNotIn("create:executor_b", backend.events)
                self.assertFalse(report["case_evidence_valid"])
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_replacement_and_wait_failures_still_clean_both_workers(self):
        for fail, fault in (("replacement", None), ("observe_claim", None), (None, "clock-backwards"), (None, "clock-never-due")):
            with self.subTest(fail=fail, fault=fault):
                backend = RecoveryDocker(fail=fail, fault=fault)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertNotIn("recover", report["stages"])
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
                self.assertEqual(report["revocation"], "verified")

    def test_ambiguous_mutation_not_retried_and_prefix_is_retained(self):
        for fault, calls, prefix in (("ambiguous-recover", 3, 0), ("wrong-counter", 4, 1), ("extended-tombstone", 4, 1), ("acl-success", 13, 11)):
            with self.subTest(fault=fault):
                backend = RecoveryDocker(fault=fault)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertEqual(report["failure_phase"], "recover")
                self.assertEqual(backend.redis.target_calls, calls)
                self.assertEqual(len(report["stages"]["recover"]["result"]["steps"]), prefix)
                self.assertEqual(len(report["stages"]["recover"]["result"]["observed_times"]), prefix + 2)
                self.assertEqual(report["revocation"], "verified")
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_insufficient_whole_case_budget_rejected_before_mutation(self):
        backend = RecoveryDocker()
        with self.assertRaisesRegex(h.InvalidArtifact, "RECOVERY_TIME_LIMIT"):
            ctl.execute(self.plan, base.approval(self.plan), backend, revision_check=lambda _: None)
        self.assertEqual(backend.events, [])

    def test_new_phases_cannot_be_selected_for_old_cases(self):
        for selected in (case.CASE, case.CLAIM_CASE, "ledger-candidate-compat-present-v1"):
            for stage in spec.PHASES:
                with self.subTest(case=selected, stage=stage), self.assertRaises(h.InvalidArtifact):
                    case.stage_roles(selected, stage)
        with self.assertRaises(h.InvalidArtifact):
            case.stage_roles(spec.CASE, "measure")

    def test_five_stopped_roles_are_prepared_without_start_or_foreign_cleanup(self):
        prepare = runpy.run_path(str(h.ROOT / "scripts/prepare-crawl-jobs-v2-images.py"))
        backend = RecoveryDocker()
        backend.deadline = ctl.time.monotonic() + 60
        report = prepare["validate_prestart"](backend, self.plan["inputs"]["harness_image"], self.plan["inputs"]["redis_image"], spec.CASE)
        self.assertEqual(report["status"], "PASS")
        self.assertEqual(report["containers_started"], 0)
        self.assertEqual([row["role"] for row in report["roles"]], list(spec.CONTAINER_ROLES))
        self.assertEqual(len(report["cleanup"]), 7)
        self.assertTrue(all(row["removed"] for row in report["cleanup"]))
        self.assertFalse(any(event.startswith("start:") for event in backend.events))
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_receipt_forgery_and_boolean_counter_substitution_reject(self):
        backend = RecoveryDocker()
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "PASS")
        request = backend.requests["observe_claim"]
        result = report["stages"]["observe_claim"]["result"]
        changed = copy.deepcopy(result)
        changed["claimed_snapshot"]["counters"]["job.claim_count"] = True
        with self.assertRaises(h.InvalidArtifact):
            live.validate_stage_result("observe_claim", changed, request)
        changed = copy.deepcopy(result)
        changed["claim_receipt_sha256"] = "0" * 64
        with self.assertRaises(h.InvalidArtifact):
            live.validate_stage_result("observe_claim", changed, request)
        request = copy.deepcopy(backend.requests["recover"])
        request["previous"]["wait"]["observations"][-1]["now_ms"] = request["previous"]["claim"]["lease_expires_at_ms"] - 1
        with self.assertRaises(h.InvalidArtifact):
            live.validate_stage_result("recover", report["stages"]["recover"]["result"], request)


if __name__ == "__main__":
    unittest.main()
