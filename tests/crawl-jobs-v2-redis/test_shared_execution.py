"""Shared lifecycle simulations: real adapter/reader paths, never Redis acceptance."""
import copy
from contextlib import nullcontext
import io
import unittest
from unittest.mock import patch

import bounded_state
import controller as ctl
import executor as worker
import harness as h
import runtime_case as case
import shared_capacity as oracle
import shared_capacity_specs as spec
import shared_executor as live
import resp
import test_execution as base
from test_claim_execution import ClaimRedis, permits
from test_claim_release import captured


class SharedRedis(ClaimRedis):
    def __init__(self, fault=None):
        super().__init__(fault)
        self.observations, self.target_calls, self.setup_writes = [], 0, 0

    def configure(self, request):
        self.plan = request["plan"]
        self.fixture = case.fixture(self.plan, request["fixture_id"], request["claim_material"], self.ms)
        self.wires = oracle.wire_requests(self.plan, self.fixture, h.digest((request["fixture_id"] + ":boot").encode())[:32])

    def call(self, role, *args):
        if role == "setup" and args[0] == "HSET":
            self.setup_writes += 1
            if self.fault == "partial-setup" and self.setup_writes == 10:
                raise resp.TransportError("private-setup-canary")
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
        a, b = self.fixture["actors"]["a"], self.fixture["actors"]["b"]
        if (self.fault == "ambiguous-start" and index == 5) or (self.fault == "ambiguous-finish" and index == 10):
            raise resp.TransportError("private-ambiguous-canary")
        if self.fault == "blocked-mutates" and index == 2:
            self.data[b["job_key"]]["claim_count"] = b"1"
        if self.fault == "phantom-origin" and index == 2:
            key = h.P + "rate:" + b["scope_ids"][2]
            self.data[key], self.kinds[key] = {"protocol_version": b"2"}, "hash"
        if self.fault == "timestamp-churn" and index == 3:
            self.data[h.P + "rate:" + a["scope_ids"][1]]["updated_at_ms"] = str(self.ms).encode()
        if self.fault == "counter-refund" and index == 10:
            self.data[a["base_key"]]["request_starts"] = b"0"
        if self.fault == "peer-lease-lost" and index == 11:
            self.data[h.P + "active_leases"].pop(a["run_id"] + ":" + a["job_id"])
        if self.fault == "ttl-extension" and index == 13:
            self.expiries[h.P + "reservation:" + a["identity"]["reservation_id"]] += 1
        if self.fault == "first-start-overwrite" and index == 15:
            self.data[h.P + "first_request_start"]["run_id"] = b["run_id"].encode()
        if self.fault == "measurement-span" and index == 20:
            self.ms += 30001
        if index in spec.ERRORS:
            if self.fault == "wrong-error" and index == 4:
                raise resp.RedisError(b"ERR CRAWL_V2_NO_CAPACITY private-canary")
            raise resp.RedisError(("ERR " + spec.STEPS[index][2]).encode())
        reply = list(expected["reply"])
        if self.fault == "wrong-blocker" and index == 2:
            reply[2] = a["scope_ids"][0]
        if self.fault == "wrong-after-io" and index == 7:
            reply[-1] = "1"
        if self.fault == "historical-permission" and index == 14:
            reply[-1] = "1"
        return [value.encode() for value in reply]


class SharedDocker(base.FakeDocker):
    def __init__(self, fail=None, fault=None):
        super().__init__(fail)
        self.redis = SharedRedis(fault)
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
            raise ctl.CommandError("private-stage-canary")
        if self.fail == "interrupt" and stage == "measure":
            raise KeyboardInterrupt()
        worker.validate_request(request, stage)
        selected = case.case_for_plan(request["plan"])
        envelope = {"stage": stage, "status": "PASS", "recipe_sha256": case.recipe_sha256(selected), "isolation": base.fake_isolation(stage)}
        if stage == "init":
            binding = case.fixture(request["plan"], request["fixture_id"], request["claim_material"])
            self.redis.rules = case.acl_rules(selected, binding, request["plan"])
            for actor in binding["actors"].values():
                self.private.update(actor[name] for name in ("run_id", "job_id", "url", "robots_url", "origin"))
                self.private.update(actor["identity"][name] for name in ("reservation_id", "claim_transition_id"))
            envelope["result"] = {"empty_volumes_verified": True, "config_sha256": request["plan"]["redis_config"]["sha256"],
                "acl_file_sha256": h.digest(case.acl_file(request["credentials"], selected, binding, request["plan"]))}
        else:
            if stage == "resume":
                self.redis.configure(request)
            with patch.object(worker, "connect", self.redis.connect):
                try:
                    envelope["result"] = {"revocation": worker.revoke(request["credentials"], case.ROLES)} if stage == "revoke" else getattr(worker, stage)(request)
                except live.SharedFailure as failure:
                    envelope.update(status="FAIL", result=failure.result)
                    raise ctl.StageFailure(envelope) from None
        return envelope


class SerializedSharedDocker(SharedDocker):
    """Use the real Docker.stage parser with only the process response replaced."""
    def stage(self, name, stage, request, timeout):
        try:
            envelope = super().stage(name, stage, request, timeout)
            code = 0
        except ctl.StageFailure as failure:
            envelope, code = failure.receipt, 1
        with patch.object(ctl.shutil, "which", return_value="/fake/docker"):
            adapter = ctl.Docker()
        adapter.deadline, adapter.approval_expires_at_ms = self.deadline, self.approval_expires_at_ms
        with patch.object(ctl, "command", return_value=(code, h.canonical(envelope), b"private-diagnostic-canary")):
            return adapter.stage(name, stage, request, timeout)


class SharedExecutionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plan = base.test_plan(spec.SCENARIO)

    def run_case(self, backend):
        return ctl.execute(self.plan, dict(base.approval(self.plan), max_seconds=300), backend, revision_check=lambda _: None)

    def test_complete_facade_two_runs_is_redacted_and_not_real_acceptance(self):
        backend = SerializedSharedDocker()
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "PASS", report.get("failure_details"))
        self.assertFalse(report["case_evidence_valid"])
        self.assertFalse(report["m4_accepted"])
        measured = report["stages"]["measure"]["result"]
        self.assertEqual((backend.redis.target_calls, len(measured["steps"]), len(measured["acl_negatives"])), (21, 21, 46))
        final = measured["steps"][-1]
        self.assertEqual(len(final["counters"]["after"]), 60)
        for actor in ("a", "b"):
            self.assertEqual(final["counters"]["after"][actor + ".run.request_starts"], 1)
            self.assertEqual(final["counters"]["after"][actor + ".job.delivery_attempts"], 1)
        self.assertEqual([row["state"] for row in final["jobs"]], ["leased", "leased"])
        self.assertEqual(len(final["rate_scopes"]), 4)
        self.assertEqual(len(report["cleanup"]), 6)
        self.assertEqual(set(backend.redis.revoked), set(case.ROLES))
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
        self.assertLess(backend.events.index("wait:executor"), backend.events.index("create:revocation"))
        for value in backend.private | backend.passwords:
            self.assertNotIn(value.encode(), h.canonical(report))

    def test_faults_keep_valid_prefix_do_not_retry_and_revoke(self):
        faults = (("wrong-blocker", 3, 2), ("wrong-after-io", 8, 7), ("blocked-mutates", 3, 2),
            ("phantom-origin", 3, 2), ("timestamp-churn", 4, 3), ("wrong-error", 5, 4),
            ("ambiguous-start", 6, 5), ("ambiguous-finish", 11, 10), ("counter-refund", 11, 10),
            ("peer-lease-lost", 12, 11), ("ttl-extension", 14, 13), ("historical-permission", 15, 14),
            ("first-start-overwrite", 16, 15), ("measurement-span", 21, 20), ("acl-success", 21, 21))
        for fault, calls, prefix in faults:
            with self.subTest(fault=fault):
                backend = SharedDocker(fault=fault)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertEqual(report["failure_phase"], "measure")
                self.assertEqual(backend.redis.target_calls, calls)
                self.assertEqual(len(report["stages"]["measure"]["result"]["steps"]), prefix)
                self.assertEqual(report["revocation"], "verified")
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
                self.assertNotIn(b"private-canary", h.canonical(report))

    def test_real_adapter_retains_ambiguous_start_and_finish_prefixes(self):
        for fault, calls, prefix, failed in (("ambiguous-start", 6, 5, "SGC06"), ("ambiguous-finish", 11, 10, "SGC11")):
            backend = SerializedSharedDocker(fault=fault)
            report = self.run_case(backend)
            self.assertEqual(report["failure_details"]["code"], "EXECUTOR_STAGE_FAILED")
            result = report["stages"]["measure"]["result"]
            self.assertEqual((backend.redis.target_calls, len(result["steps"]), result["failed_assertion"]), (calls, prefix, failed))
            self.assertEqual(report["revocation"], "verified")
            self.assertNotIn(b"private-diagnostic-canary", h.canonical(report))

    def test_actual_worker_main_serializes_only_the_valid_failure_envelope(self):
        backend = SharedDocker(fault="ambiguous-start")
        report = self.run_case(backend)
        result = report["stages"]["measure"]["result"]
        request = backend.requests["measure"]
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
            code = worker.main()
        self.assertEqual(code, 1)
        envelope = h.decode(output.buffer.getvalue())
        self.assertEqual(envelope["status"], "FAIL")
        self.assertEqual(envelope["result"]["failed_assertion"], "SGC06")
        self.assertEqual(len(envelope["result"]["steps"]), 5)
        live.validate_stage_result("measure", envelope["result"], request, successful=False)

    def test_every_failure_boundary_is_bound_to_its_prefix(self):
        backend = SharedDocker()
        report = self.run_case(backend)
        result, request = report["stages"]["measure"]["result"], backend.requests["measure"]
        for completed in range(22):
            value = copy.deepcopy(result)
            value.pop("final_state_sha256")
            value.pop("finished_at_ms")
            value["steps"] = value["steps"][:completed]
            value["acl_negatives"] = []
            value["failed_assertion"] = "ACL" if completed == 21 else f"SGC{completed + 1:02}"
            live.validate_stage_result("measure", value, request, successful=False)
            value["failed_assertion"] = "SGC01" if completed else "SGC02"
            with self.assertRaises(h.InvalidArtifact):
                live.validate_stage_result("measure", value, request, successful=False)
        value = {key: copy.deepcopy(result[key]) for key in ("scope", "fixture_sha256", "steps", "acl_negatives", "clock_reference_ms")}
        value.update(steps=[], acl_negatives=[], clock_reference_ms=None, failed_assertion="STATE")
        live.validate_stage_result("measure", value, request, successful=False)

    def test_partial_setup_stage_failures_interrupt_and_revocation_fail_closed(self):
        for phase in ("resume", "measure", "interrupt", "revoke"):
            backend = SharedDocker(fail=phase)
            report = self.run_case(backend)
            self.assertEqual(report["verdict"], "FAIL")
            self.assertFalse(report["case_evidence_valid"])
            self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
        backend = SharedDocker(fault="partial-setup")
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertEqual(report["failure_phase"], "resume")
        self.assertEqual(backend.redis.target_calls, 0)
        self.assertEqual(report["revocation"], "verified")
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_receipt_reconstruction_rejects_counter_history_expiry_and_scope_forgery(self):
        backend = SharedDocker()
        report = self.run_case(backend)
        result, request = report["stages"]["measure"]["result"], backend.requests["measure"]
        for change in (
            lambda value: value["steps"][2]["capacity_denial"].update(after_io=1),
            lambda value: value["steps"][3]["capacity_denial"].update(blocking_scope_kind="origin_b"),
            lambda value: value["steps"][4].update(now_ms=value["steps"][4]["started_at_ms"]),
            lambda value: value["steps"][11]["counters"]["after"].update({"b.run.claims_total": True}),
            lambda value: value["steps"][12]["jobs"][0].update(lease_expires_at_ms=0),
            lambda value: value["steps"][13]["reservation_expiries"][0].update(expires_at_ms=1),
            lambda value: value["steps"][15]["history"].update(first_started_at_ms=0),
            lambda value: value["steps"][20]["rate_scopes"][1].update(next_allowed_ms=0),
            lambda value: value["steps"][14].update(response_sha256=value["steps"][6]["response_sha256"]),
            lambda value: value.update(extra="private-canary"),
        ):
            altered = copy.deepcopy(result)
            change(altered)
            with self.assertRaises(h.InvalidArtifact):
                live.validate_stage_result("measure", altered, request)

    def test_resume_binding_and_closed_runtime_trace_roles(self):
        backend = SharedDocker()
        report = self.run_case(backend)
        resume, request = report["stages"]["resume"]["result"], backend.requests["resume"]
        for change in (
            lambda value: value["setup_counters"].update({"b.job.next_request_ordinal": 2}),
            lambda value: value["probe_evidence"].update(new_run_id=value["probe_evidence"]["old_run_id"]),
            lambda value: value.update(setup_time_ms=True),
            lambda value: value["early_revocation"]["boot"].update(server_reachable=False),
            lambda value: value.update(trace="cancel"),
        ):
            changed = copy.deepcopy(resume)
            change(changed)
            with self.assertRaises(h.InvalidArtifact):
                live.validate_resume(changed, request, request["previous"])
        for stage in ("rate_before", "rate_clock", "rate_after", "recover", "claim_park", "cancel"):
            with self.assertRaises(h.InvalidArtifact):
                worker.validate_request(backend.requests["measure"], stage)
        for key in ("trace", "wires", "expected", "endpoint"):
            changed = copy.deepcopy(backend.requests["measure"])
            changed[key] = "private-canary"
            with self.assertRaises(h.InvalidArtifact):
                worker.validate_request(changed, "measure")
        for field, value in (("empty_volumes_verified", False), ("config_sha256", "0" * 64), ("acl_file_sha256", "0" * 64)):
            changed = copy.deepcopy(report["stages"]["init"]["result"])
            changed[field] = value
            with self.assertRaises(h.InvalidArtifact):
                live.validate_stage_result("init", changed, backend.requests["init"])
        with self.assertRaises(h.InvalidArtifact):
            live.validate_stage_result("ready", {"run_id": "private-canary"}, backend.requests["ready"])
        changed = copy.deepcopy(report["stages"]["revoke"]["result"])
        changed["revocation"]["revoker"]["server_reachable"] = False
        with self.assertRaises(h.InvalidArtifact):
            live.validate_stage_result("revoke", changed, backend.requests["revoke"])

    def test_acl_setup_boot_and_peer_key_boundaries(self):
        from test_shared_capacity import context
        plan, f, _, _ = context()
        rules = case.acl_rules(spec.CASE, f, plan)
        self.assertEqual(set(rules), set(case.ROLES))
        self.assertFalse(permits(rules["setup"], "HSET", h.AUTH[0]))
        self.assertTrue(permits(rules["boot"], "HSET", h.AUTH[0]))
        for actor in f["actors"].values():
            self.assertTrue(permits(rules["ledger"], "HMGET", actor["base_key"]))
            self.assertTrue(permits(rules["ledger"], "HSET", actor["job_key"]))
            self.assertFalse(permits(rules["observer"], "HSET", actor["job_key"]))
        for key in (*h.AUTH, "unowned-key"):
            for command in ("SET", "HSET", "DEL", "EXPIRE", "RENAME"):
                self.assertFalse(permits(rules["ledger"], command, key))
        self.assertEqual(case.sources(spec.CASE), spec.SOURCES)
        self.assertEqual(case.recipe(spec.CASE)["counter_fields"], 60)

    def test_reader_90_positions_rejects_unknown_keys_and_bad_types(self):
        from test_shared_capacity import context
        _, f, _, _ = context()
        boot = dict(zip(case.BOOT_FIELDS, ("1", "approved", "b" * 40, "7" * 32, "999600", "", "", "initial", "", "8" * 64, "999600", "0")))
        for fault in (None, "extra", "expiry", "field", "type"):
            redis = SharedRedis()
            model = live.with_boot(f["initial_state"], boot)
            redis.install_model(model)
            key = f["actors"]["b"]["job_key"]
            if fault == "extra":
                redis.data["private-unowned-canary"] = b"must-not-read"
            elif fault == "expiry":
                redis.expiries[key] = 123
            elif fault == "field":
                redis.data[key]["private-extra-canary"] = b"x"
            elif fault == "type":
                redis.kinds[key] = "string"
            with redis.connect("observer", {"observer": "9" * 64}) as client:
                if fault is None:
                    self.assertEqual(live.snapshot(client, f["initial_state"], boot), model)
                else:
                    with self.assertRaises(h.InvalidArtifact):
                        live.snapshot(client, f["initial_state"], boot)
        with self.assertRaises(h.InvalidArtifact):
            bounded_state.inventory(None, 91)
        with self.assertRaises(h.InvalidArtifact):
            live.with_boot({**f["initial_state"], h.AUTH[0]: None}, boot)


if __name__ == "__main__":
    unittest.main()
