"""Negative-case live-boundary/failure tests using explicit simulated backends."""
import copy
import unittest
from unittest.mock import patch

import harness as h
import runtime_case as case
import controller as ctl
import executor as worker
import negative_specs as ns
import negative_cases as nc
import negative_executor as live
import claim_release as cr
import resp
import test_execution as base
from test_claim_execution import ClaimRedis, permits
from test_negative_cases import AT

PROCESS_SHA = h.digest(b"simulated process-inventory receipt; not target evidence")


def isolation(**_):
    return {"uid": 65534, "interfaces": ["lo"], "external_routes": 0, "effective_capabilities": "0",
            "process_count": 2, "process_inventory_sha256": PROCESS_SHA}


class NegativeRedis(ClaimRedis):
    def __init__(self, fault=None):
        super().__init__()
        self.ms = AT
        self.negative_fault = fault
        self.target_calls = 0
        self.admin_times = []
        self.boot_wire = None
        self.setup_time = AT
        self.positive_times = []
        self.prefix_calls = 0

    def configure(self, request):
        self.request = request
        self.plan = request["plan"]
        self.selected = case.case_for_plan(self.plan)
        self.epoch = h.digest((request["fixture_id"] + ":boot").encode())[:32]
        self.sources = {case.script_bytes(op, self.selected)[1]: op for op in case.sources(self.selected)}
        if "setup_time_ms" in request["previous"]:
            self.setup_time = request["previous"]["setup_time_ms"]

    def fixture_now(self):
        return case.fixture(self.plan, self.request["fixture_id"], self.request.get("claim_material", {}), self.setup_time)

    def current_boot(self):
        return {key: value.decode() for key, value in self.data[h.AUTH[0]].items()}

    def negative_error(self, expected_error):
        self.target_calls += 1
        if self.negative_fault == "ambiguous":
            raise resp.TransportError("secret-ambiguous-canary")
        if self.negative_fault == "wrong-error":
            raise resp.RedisError(b"NOSCRIPT secret-error-canary")
        if self.negative_fault == "success":
            return [b"UNEXPECTED_SUCCESS"]
        if self.negative_fault == "unknown-key":
            self.data["unlisted-secret-canary"] = b"secret-state-canary"
        if self.negative_fault == "mutates":
            self.data.setdefault(h.AUTH[0], {})["approved_at_ms"] = b"1"
        raise resp.RedisError(expected_error.encode() if expected_error == "NOPERM" else ("ERR " + expected_error).encode())

    def call(self, role, *args):
        if args[0] != "EVALSHA":
            return super().call(role, *args)
        self.calls.append((role, *args))
        self.ms += 1
        operation = self.sources.get(args[1])
        assert operation is not None
        if operation == "CJ2_APPROVE_BOOT":
            if self.selected == ns.BOOT and "setup_time_ms" in self.request["previous"]:
                previous = self.request["previous"]
                original = nc.boot_wire(self.run_id, self.epoch, previous["probe_evidence_sha256"], previous["probe_evidence"]["verified_at_ms"])
                # The worker's reference clock is the time before its first call.
                negatives = nc.boot_wires(original, previous["probe_evidence"]["old_run_id"], self.setup_time)
                wanted = next((code for _, code, wire in negatives if tuple(args) == wire), None)
                if wanted:
                    return self.negative_error(wanted)
            exists = h.AUTH[0] in self.data
            if not exists:
                self.data[h.AUTH[0]] = {key: value.encode() for key, value in nc.boot_record(args, self.ms).items()}
            self.boot_wire = tuple(args)
            if self.selected != ns.BOOT:
                self.setup_time = self.ms
            return [b"EXISTS_IDENTICAL" if exists else b"OK", str(self.ms).encode(), self.epoch.encode()]
        fixture = self.fixture_now()
        if self.selected in ns.ADMIN:
            observed = nc.admin_observations(self.plan, fixture, self.current_boot(), PROCESS_SHA, self.setup_time)
            index = (ns.INSTALL, ns.RETIRE, ns.PROMOTE).index(operation)
            projected = nc.admin_projection(self.plan, fixture, observed, self.epoch,
                                             self.admin_times + [self.ms] * (3 - len(self.admin_times)))[index]
            assert tuple(args) == projected["wire"]
            if role == "ledger":
                if operation != ns.INSTALL:
                    assert not permits(self.rules[role], "EVALSHA", *args[3:3 + int(args[2])])
                return self.negative_error(ns.ADMIN[self.selected][2])
            if self.negative_fault == "prefix" and operation == ns.INSTALL:
                raise resp.TransportError("secret-prefix-canary")
            assert permits(self.rules[role], "EVALSHA", *args[3:3 + int(args[2])])
            self.admin_times.append(self.ms)
            self.install_model(projected["state"])
            self.prefix_calls += 1
            return [value.encode() for value in projected["reply"]]
        rows = nc.ledger_wires(self.plan, fixture, self.epoch)
        for _, code, wire in rows:
            if tuple(args) == wire:
                return self.negative_error(code)
        assert self.selected == ns.WIRE
        wires = cr.wire_requests(self.plan, fixture["worker"], self.epoch)
        index = 0 if operation == cr.CLAIM else 3
        assert tuple(args) == wires[index]
        self.positive_times.append(self.ms)
        row = cr.expected_sequence(self.plan, fixture["worker"], [self.positive_times[0]] * 3 + [self.ms] * 6)[index]
        self.install_model(row["state"])
        return [value.encode() for value in row["reply"]]


class NegativeDocker(base.FakeDocker):
    def __init__(self, fail=None, fault=None):
        super().__init__(fail)
        self.redis = NegativeRedis(fault)
        self.requests = {}
        self.private = set()

    def kill(self, name):
        super().kill(name)
        if name.endswith("-redis"):
            self.redis.run_id = "b" * 40

    def stage(self, name, stage, request, timeout):
        self.events.append(stage)
        self.passwords.update(request["credentials"].values())
        self.private.update(request.get("claim_material", {}).values())
        selected = case.case_for_plan(request["plan"])
        self.requests[stage] = copy.deepcopy(request)
        if self.fail == stage:
            raise ctl.CommandError("simulated stage failure")
        if self.fail == "interrupt" and stage == "measure":
            raise KeyboardInterrupt()
        worker.validate_request(request, stage)
        envelope = {"stage": stage, "status": "PASS", "recipe_sha256": case.recipe_sha256(selected), "isolation": isolation()}
        if stage == "init":
            fixture = case.fixture(request["plan"], request["fixture_id"], request.get("claim_material", {}))
            self.private.add(fixture["admin_nonce"])
            self.redis.rules = case.acl_rules(selected, fixture, request["plan"])
            if fixture["worker"]:
                self.private.update(fixture["worker"]["identities"][label]["reservation_id"] for label in ("a", "b"))
            envelope["result"] = {"empty_volumes_verified": True, "config_sha256": request["plan"]["redis_config"]["sha256"],
                                  "acl_file_sha256": h.digest(case.acl_file(request["credentials"], selected, fixture, request["plan"]))}
        else:
            self.redis.configure(request)
            with patch.object(worker, "connect", self.redis.connect), patch.object(worker, "environment", side_effect=isolation):
                try:
                    envelope["result"] = ({"revocation": worker.revoke(request["credentials"], case.roles(selected))} if stage == "revoke"
                                           else getattr(worker, stage)(request))
                except live.NegativeFailure as failure:
                    envelope.update(status="FAIL", result=failure.result)
                    raise ctl.StageFailure(envelope) from None
        return envelope


class NegativeLifecycleTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plans = {name: base.test_plan(scenario) for name, scenario in ns.CASES.items()}

    def run_case(self, name, backend):
        plan = self.plans[name]
        return ctl.execute(plan, base.approval(plan), backend, revision_check=lambda _: None)

    def test_all_thirteen_closed_lifecycles_and_case_roles(self):
        for name in ns.CASES:
            with self.subTest(case=name):
                backend = NegativeDocker()
                report = self.run_case(name, backend)
                self.assertEqual(report["verdict"], "PASS", report.get("failure_details"))
                self.assertFalse(report["case_evidence_valid"])
                self.assertFalse(report["m4_accepted"])
                measured = report["stages"]["measure"]["result"]
                self.assertEqual(len(measured["steps"]), len(ns.measurement_sequence(name)))
                self.assertEqual(len(measured["acl_negatives"]), 46 if name in (*ns.STORED, ns.WIRE) else 0)
                self.assertEqual(set(backend.redis.revoked), set(case.roles(name)))
                self.assertEqual(case.roles(name)[-1], "revoker")
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
                self.assertLess(backend.events.index("wait:executor"), backend.events.index("create:revocation"))
                raw = h.canonical(report)
                for value in backend.private | backend.passwords | {cr.URL, cr.ROBOTS}:
                    self.assertNotIn(value.encode(), raw)
                for stage, request in backend.requests.items():
                    self.assertEqual(set(request["credentials"]), set(case.stage_roles(name, stage)))

    def test_errors_success_and_mutation_fail_without_retry_and_revoke_every_role(self):
        for name in (next(iter(ns.STORED)), ns.WIRE, ns.BOOT, "ledger-promote-denied-v1"):
            for fault in ("ambiguous", "wrong-error", "success", "mutates", "unknown-key"):
                with self.subTest(case=name, fault=fault):
                    backend = NegativeDocker(fault=fault)
                    report = self.run_case(name, backend)
                    self.assertEqual(report["verdict"], "FAIL")
                    self.assertEqual(report["failure_phase"], "measure")
                    self.assertEqual(backend.redis.target_calls, 1)
                    self.assertEqual(report["revocation"], "verified")
                    self.assertTrue(all(row["removed"] for row in report["cleanup"]))
                    self.assertEqual(set(backend.redis.revoked), set(case.roles(name)))
                    self.assertNotIn("secret-", str(report))

    def test_failed_admin_prefix_is_retained_without_inventing_success(self):
        backend = NegativeDocker(fault="prefix")
        report = self.run_case("ledger-promote-denied-v1", backend)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertEqual(report["failure_phase"], "resume")
        self.assertEqual(report["stages"]["resume"]["result"]["prefix_steps"], [])
        self.assertEqual(report["revocation"], "verified")
        self.assertTrue(all(row["removed"] for row in report["cleanup"]))

    def test_receipt_schema_counter_and_fixture_substitution_reject(self):
        name = ns.WIRE
        backend = NegativeDocker()
        report = self.run_case(name, backend)
        self.assertEqual(report["verdict"], "PASS")
        result = report["stages"]["measure"]["result"]
        for mutate in (lambda r: r.update(fixture_sha256="f" * 64),
                       lambda r: r["steps"][0].update(response_code="NOPERM"),
                       lambda r: r["steps"][0].update(now_ms=0),
                       lambda r: r["steps"][0].update(state_sha256="e" * 64),
                       lambda r: r["steps"][0].update(extra="secret-canary"),
                       lambda r: r["steps"][0]["counters"]["after"].update({"run.claims_total": True}),
                       lambda r: r["steps"][0]["counters"]["delta"].update({"job.claim_count": 1}),
                       lambda r: r["acl_negatives"].pop(), lambda r: r.update(retired_roles={"setup": {}})):
            changed = copy.deepcopy(result)
            mutate(changed)
            with self.assertRaises(h.InvalidArtifact):
                live.validate_measurement(changed, True, backend.requests["measure"])

    def test_failed_receipt_binding_is_checked_before_retention(self):
        class Swapped(NegativeDocker):
            def stage(self, name, stage, request, timeout):
                try:
                    return super().stage(name, stage, request, timeout)
                except ctl.StageFailure as error:
                    error.receipt["result"]["fixture_sha256"] = "f" * 64
                    raise
        backend = Swapped(fault="ambiguous")
        report = self.run_case(ns.WIRE, backend)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertNotIn("measure", report["stages"])
        self.assertEqual(report["revocation"], "verified")

    def test_stage_interrupt_cleanup_and_journal_errors_cannot_be_acceptance(self):
        for failure in ("resume", "interrupt", "revoke", "cleanup-volume"):
            report = self.run_case("ledger-retire-denied-v1", NegativeDocker(fail=failure))
            self.assertEqual(report["verdict"], "FAIL")
            self.assertFalse(report["case_evidence_valid"])
        plan = self.plans[ns.BOOT]
        backend = NegativeDocker()
        def journal(_, event):
            if event["action"] == "credentials_revoked_and_verified":
                raise OSError("secret-journal-canary")
        report = ctl.execute(plan, base.approval(plan), backend, revision_check=lambda _: None, action_journal=journal)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertTrue(all(row["removed"] for row in report["cleanup"]))
        self.assertNotIn("secret-journal-canary", str(report))


if __name__ == "__main__":
    unittest.main()
