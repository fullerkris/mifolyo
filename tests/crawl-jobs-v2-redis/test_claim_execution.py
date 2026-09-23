"""Claim executor/controller boundary tests; no Docker daemon or Redis server."""
import copy
import hashlib
import json
from pathlib import Path
import re
import runpy
import tempfile
import unittest
from unittest.mock import patch

import claim_executor as live
import claim_release as cr
import controller as ctl
import executor as worker
import harness as h
import resp
import runtime_case as case
import test_execution as fixtures
from test_claim_release import captured


def request(plan=None):
    material = captured()
    material.pop("redis_time_ms")
    fid = material.pop("fixture_id")
    return {"plan": plan or fixtures.test_plan(cr.SCENARIO), "recipe_sha256": case.recipe_sha256(cr.CASE),
            "fixture_id": fid, "claim_material": material,
            "credentials": {role: hashlib.sha256(role.encode()).hexdigest() for role in case.ROLES}, "previous": {}}


def permits(rules, command, *keys):
    """Small independent parser for these positive-command, literal-key selectors."""
    text = " ".join(rules)
    selectors = re.findall(r"\(([^()]*)\)", text)
    selectors.append(re.sub(r"\([^()]*\)", "", text))
    for selector in selectors:
        tokens = selector.split()
        if "+" + command.lower() not in tokens:
            continue
        writing = command.lower() in {"evalsha", "hset", "set", "del", "unlink", "zadd", "zrem", "pexpireat", "expire", "rename", "sadd"}
        admitted = {token[1:] for token in tokens if token.startswith("~")}
        if not writing:
            admitted.update(token[3:] for token in tokens if token.startswith("%R~"))
        if all(key in admitted for key in keys):
            return True
    return False


class ClaimRedis(fixtures.RedisFake):
    """Typed command facade using the independently Go/Lua-checked offline model.

    These replies are simulated, not evidence of real ACL parsing or atomicity.
    Faults intentionally violate the model to exercise the live read boundaries.
    """
    def __init__(self, fault=None):
        super().__init__()
        self.ms, self.kinds, self.expiries = 1000000, {}, {}
        self.fault, self.evals, self.fixture, self.wires = fault, 0, None, None
        self.rules = None

    def configure(self, req):
        self.plan = req["plan"]
        self.fixture = case.claim_fixture(req["plan"], req["fixture_id"], req["claim_material"], self.ms)
        self.wires = cr.wire_requests(req["plan"], self.fixture, h.digest((req["fixture_id"] + ":boot").encode())[:32])

    def install_model(self, state):
        for key, row in state.items():
            if row is None:
                self.data.pop(key, None)
                self.kinds.pop(key, None)
                self.expiries.pop(key, None)
            else:
                self.kinds[key], self.expiries[key] = row["type"], row["expires_at_ms"]
                if row["type"] == "hash":
                    self.data[key] = {name: value.encode() for name, value in row["fields"]}
                elif row["type"] == "string":
                    self.data[key] = row["value"].encode()
                elif row["type"] == "set":
                    self.data[key] = set(row["members"])
                elif row["type"] == "zset":
                    self.data[key] = dict(row["members"])

    def call(self, role, *args):
        cmd = args[0]
        if role == "ledger" and len(args) > 1 and args[1] in h.AUTH and cmd != "TYPE":
            self.calls.append((role, *args))
            if self.fault == "acl-success":
                return b"OK"
            if self.fault == "acl-error":
                raise resp.RedisError(b"WRONGTYPE not an ACL denial")
            if self.fault == "acl-mutates":
                self.data[h.AUTH[2]] = b"corrupt"
            raise resp.RedisError(b"NOPERM synthetic")
        if self.rules is not None:
            command = cmd.lower()
            keys = []
            if cmd in ("INFO", "CONFIG", "SCRIPT", "ACL"):
                command += "|" + args[1].lower()
            elif cmd == "EVALSHA":
                keys = args[3:3 + int(args[2])]
            elif cmd not in ("PING", "TIME", "DBSIZE", "SCAN"):
                keys = args[1:2]
            if not permits(self.rules[role], command, *keys):
                raise AssertionError("case ACL omitted a required command/key grant: " + command)
        if cmd == "TIME":
            return [str(self.ms // 1000).encode(), str(self.ms % 1000 * 1000).encode()]
        if cmd == "EVALSHA" and role == "ledger":
            self.calls.append((role, *args))
            index = self.evals
            self.evals += 1
            assert tuple(args) == self.wires[index]
            if self.fault == "ambiguous":
                raise resp.TransportError("COMMAND_INCOMPLETE")
            times = [self.ms + i + 1 for i in range(9)]
            projected = cr.expected_sequence(self.plan, self.fixture, times)[index]
            self.install_model(projected["state"])
            if self.fault == "unknown-key":
                self.data["unlisted-secret-canary"] = b"must-not-read"
            if self.fault == "wrong-count":
                self.data[self.fixture["base_key"]]["claims_total"] = b"99"
            if self.fault == "ttl-extension" and index == 4:
                key = h.P + "reservation:" + self.fixture["identities"]["a"]["reservation_id"]
                self.expiries[key] += 1
            if self.fault == "wrong-reply":
                return [b"CLAIMED", str(times[index]).encode(), b"0"]
            return [value.encode() for value in projected["reply"]]
        if cmd == "EVALSHA" and role == "boot":
            self.calls.append((role, *args))
            exists = h.AUTH[0] in self.data
            values = ("1", "approved", args[4], args[5], str(self.ms), "", "", "initial", "", args[6], args[7], "0")
            self.data[h.AUTH[0]] = {key: value.encode() for key, value in zip(case.BOOT_FIELDS, values)}
            return [b"EXISTS_IDENTICAL" if exists else b"OK", str(self.ms).encode(), args[5].encode()]
        if cmd in ("TYPE", "PEXPIRETIME", "HSTRLEN", "STRLEN", "SADD", "SCARD", "SMEMBERS", "ZADD", "ZCARD", "ZRANGE"):
            self.calls.append((role, *args))
            key, value = args[1], self.data.get(args[1])
            if cmd == "TYPE":
                return b"none" if value is None else self.kinds.get(key, "hash" if type(value) is dict else "string").encode()
            if cmd == "PEXPIRETIME":
                return self.expiries.get(key, -1) if value is not None else -2
            if cmd == "HSTRLEN":
                return len(value.get(args[2], b"")) if value is not None else 0
            if cmd == "STRLEN":
                return len(value) if value is not None else 0
            if cmd == "SADD":
                self.kinds[key], self.data[key] = "set", set(args[2:])
                return len(self.data[key])
            if cmd == "SCARD" or cmd == "ZCARD":
                return len(value) if value is not None else 0
            if cmd == "SMEMBERS":
                return [member.encode() for member in sorted(value)]
            if cmd == "ZADD":
                self.kinds[key], self.data[key] = "zset", {member: score for score, member in zip(args[2::2], args[3::2])}
                return len(self.data[key])
            if cmd == "ZRANGE":
                return [part.encode() for item in sorted(value.items(), key=lambda row: (int(row[1]), row[0])) for part in item]
        return super().call(role, *args)


class ClaimDocker(fixtures.FakeDocker):
    def __init__(self, fail=None, fault=None):
        super().__init__(fail)
        self.redis = ClaimRedis(fault)
        self.private = set()

    def kill(self, name):
        super().kill(name)
        if name.endswith("-redis"):
            self.redis.run_id = "b" * 40

    def stage(self, name, stage, req, timeout):
        self.events.append(stage)
        self.passwords.update(req["credentials"].values())
        self.private.update(req["claim_material"].values())
        if self.fail == stage:
            raise ctl.CommandError("stage failure")
        if self.fail == "interrupt" and stage == "measure":
            raise KeyboardInterrupt()
        envelope = {"stage": stage, "status": "PASS", "recipe_sha256": case.recipe_sha256(cr.CASE), "isolation": {"interfaces": ["lo"]}}
        if stage == "init":
            binding = case.claim_fixture(req["plan"], req["fixture_id"], req["claim_material"])
            self.redis.rules = case.acl_rules(cr.CASE, binding, req["plan"])
            self.private.update(binding["identities"][label]["reservation_id"] for label in ("a", "b"))
            envelope["result"] = {"empty_volumes_verified": True, "config_sha256": req["plan"]["redis_config"]["sha256"],
                                  "acl_file_sha256": h.digest(case.acl_file(req["credentials"], cr.CASE, binding, req["plan"]))}
        else:
            if stage == "resume":
                self.redis.configure(req)
            with patch.object(worker, "connect", self.redis.connect):
                try:
                    envelope["result"] = ({"revocation": worker.revoke(req["credentials"], case.ROLES)} if stage == "revoke"
                                          else getattr(worker, stage)(req))
                except live.MeasurementFailure as failure:
                    envelope.update(status="FAIL", result=failure.result)
                    raise ctl.StageFailure(envelope) from None
        return envelope


class ClaimIntegrationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plan = fixtures.test_plan(cr.SCENARIO)

    def run_case(self, backend):
        return ctl.execute(self.plan, fixtures.approval(self.plan), backend, revision_check=lambda _: None)

    def test_complete_facade_lifecycle_and_redacted_receipts(self):
        backend = ClaimDocker()
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "PASS")
        self.assertEqual(report["case"], cr.CASE)
        self.assertFalse(report["case_evidence_valid"])
        self.assertFalse(report["m4_accepted"])
        result = report["stages"]["measure"]["result"]
        self.assertEqual((len(result["steps"]), len(result["acl_negatives"])), (9, 46))
        self.assertEqual(backend.redis.evals, 9)
        self.assertLess(backend.events.index("wait:executor"), backend.events.index("create:revocation"))
        self.assertEqual(set(backend.redis.revoked), set(case.ROLES))
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
        raw = h.canonical(report)
        for value in backend.private | backend.passwords | {cr.URL, cr.ROBOTS}:
            self.assertNotIn(value.encode(), raw)
        self.assertNotIn("setup_projection", report["stages"]["resume"]["result"])
        actions = report["actions"]
        self.assertTrue(any(row["action"] == "worker_quiesced_and_removed" for row in actions))
        self.assertTrue(any(row["action"] == "credentials_revoked_and_verified" for row in actions))
        cleanup_names = {row["name"] for row in report["cleanup"]}
        self.assertEqual({row["subject"] for row in actions if row["subject"] in cleanup_names}, cleanup_names)

    def test_partial_measurement_failure_retains_prefix_and_cleans_up(self):
        backend = ClaimDocker(fault="ttl-extension")
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertEqual(report["failure_phase"], "measure")
        self.assertEqual(report["failure_details"]["code"], "EXECUTOR_STAGE_FAILED")
        evidence = report["stages"]["measure"]
        self.assertEqual(evidence["status"], "FAIL")
        self.assertEqual(len(evidence["result"]["steps"]), 4)
        self.assertEqual(evidence["result"]["failed_assertion"], "CR05")
        self.assertEqual(backend.redis.evals, 5)
        self.assertEqual(report["revocation"], "verified")
        self.assertTrue(all(row["removed"] for row in report["cleanup"]))

    def test_counter_receipts_export_observed_before_after_and_strict_deltas(self):
        report = self.run_case(ClaimDocker())
        result = report["stages"]["measure"]["result"]
        steps = result["steps"]
        self.assertEqual([row["counters"]["after"]["run.claims_total"] for row in steps], [1, 1, 1, 1, 1, 2, 2, 2, 2])
        self.assertEqual([row["counters"]["after"]["run.pending_request_reservations"] for row in steps], [1, 1, 1, 0, 0, 1, 1, 0, 0])
        self.assertEqual(steps[0]["counters"]["before"]["job.next_request_ordinal"], 1)
        self.assertEqual(steps[-1]["counters"]["after"]["job.next_request_ordinal"], 3)
        self.assertEqual(steps[3]["counters"]["delta"]["scope.origin.pending_count"], -1)
        for index, row in enumerate(steps):
            counts = row["counters"]
            self.assertTrue(all(type(value) is int for values in counts.values() for value in values.values()))
            self.assertEqual(counts["after"]["job.lease_request_starts_baseline"], 0)
            self.assertEqual(counts["after"]["job.request_starts"], 0)
            self.assertEqual(counts["after"]["job.delivery_attempts"], 0)
            if index in (1, 2, 4, 6, 8):
                self.assertTrue(all(value == 0 for value in counts["delta"].values()))
        for field, value in (("before", 1), ("after", 2), ("after", True), ("delta", 2)):
            changed = copy.deepcopy(result)
            changed["steps"][0]["counters"][field]["run.claims_total"] = value
            with self.subTest(field=field, value=value), self.assertRaises(h.InvalidArtifact):
                live.validate_measurement(changed, True)
        changed = copy.deepcopy(result)
        changed["steps"][0]["counters"]["after"]["unlisted-private-field"] = 0
        with self.assertRaises(h.InvalidArtifact):
            live.validate_measurement(changed, True)

    def test_substituted_failed_fixture_receipt_is_discarded_with_cleanup(self):
        class Substituted(ClaimDocker):
            def stage(self, name, stage, req, timeout):
                try:
                    return super().stage(name, stage, req, timeout)
                except ctl.StageFailure as failure:
                    failure.receipt["result"]["fixture_sha256"] = "f" * 64
                    raise
        backend = Substituted(fault="ttl-extension")
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertNotIn("measure", report["stages"])
        self.assertEqual(report["failure_phase"], "measure")
        self.assertEqual(report["revocation"], "verified")
        self.assertTrue(all(row["removed"] for row in report["cleanup"]))
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_cleanup_journal_failures_never_skip_teardown_or_allow_pass(self):
        for action in ("worker_quiesced_and_removed", "credentials_revoked_and_verified", "volume_removed_and_verified"):
            backend = ClaimDocker()
            seen = []
            def journal(fixture_id, event):
                seen.append(event)
                if event["action"] == action:
                    raise OSError("secret-cleanup-canary")
            with self.subTest(action=action):
                report = ctl.execute(self.plan, fixtures.approval(self.plan), backend, revision_check=lambda _: None, action_journal=journal)
                self.assertTrue(report["case_passed"])
                self.assertEqual(report["verdict"], "FAIL")
                self.assertFalse(report["case_evidence_valid"])
                self.assertIn("journal_failure", report)
                self.assertEqual(report["revocation"], "verified")
                self.assertTrue(all(row["removed"] for row in report["cleanup"]))
                self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
                self.assertEqual(len([row for row in seen if row["action"] == "volume_removed_and_verified"]), 2)
                self.assertNotIn("secret-cleanup-canary", json.dumps(report))
        backend = fixtures.FakeDocker()
        for name in ("owned-a", "owned-b"):
            backend.volume(name, "a" * 32)
        def broken_callback(*_):
            raise OSError("secret-cleanup-canary")
        rows = ctl.cleanup(backend, [("volume", name) for name in ("owned-a", "owned-b")], "a" * 32,
                           on_removed=broken_callback)
        self.assertTrue(all(row["removed"] and "journal_failure" in row for row in rows))
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
        self.assertNotIn("secret-cleanup-canary", json.dumps(rows))

    def test_ambiguous_reply_state_and_acl_faults_stop_without_retry(self):
        for fault in ("ambiguous", "wrong-reply", "wrong-count", "unknown-key", "acl-success", "acl-error", "acl-mutates"):
            with self.subTest(fault=fault):
                backend = ClaimDocker(fault=fault)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertEqual(backend.redis.evals, 9 if fault.startswith("acl-") else 1)
                self.assertTrue(all(row["removed"] for row in report["cleanup"]))
                self.assertFalse(any(call[1:3] == ("TYPE", "unlisted-secret-canary") for call in backend.redis.calls))

    def test_failure_interrupt_and_cleanup_uncertainty_invalidate_case(self):
        for failure in ("init", "resume", "interrupt", "ambiguous-volume", "ambiguous-redis", "revoke", "cleanup-volume"):
            with self.subTest(failure=failure):
                backend = ClaimDocker(fail=failure)
                report = self.run_case(backend)
                self.assertEqual(report["verdict"], "FAIL")
                self.assertFalse(report["case_evidence_valid"])
                if failure != "cleanup-volume":
                    self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_cross_case_approval_recipe_and_ownership_rejected(self):
        approval = fixtures.approval(self.plan)
        for field, value in (("case", case.CASE), ("recipe_sha256", case.recipe_sha256())):
            backend = ClaimDocker()
            with self.subTest(field=field), self.assertRaises(h.InvalidArtifact):
                ctl.execute(self.plan, dict(approval, **{field: value}), backend, revision_check=lambda _: None)
            self.assertEqual(backend.events, [])
        spec = ctl.container_spec("fixture", "executor", "a" * 32, "sha256:" + "b" * 64,
                                  {"control": "c", "data": "d"}, cr.CASE)
        observed = fixtures.inspected_container(spec)
        ctl.verify_container(observed, spec, False)
        observed["Config"]["Labels"]["io.mifolyo.cj2.case"] = case.CASE
        with self.assertRaises(h.InvalidArtifact):
            ctl.verify_container(observed, spec, False)
        backend = ClaimDocker()
        backend.case_id = cr.CASE
        backend.resources[("container", "fixture")] = observed
        self.assertFalse(ctl.cleanup(backend, [("container", "fixture")], "a" * 32)[0]["removed"])

    def test_private_stage_values_never_enter_report(self):
        class Leaking(ClaimDocker):
            def stage(self, name, stage, req, timeout):
                value = super().stage(name, stage, req, timeout)
                if stage == "resume":
                    value["result"]["unexpected"] = req["claim_material"]["token_a"]
                return value
        backend = Leaking()
        report = self.run_case(backend)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertNotIn("resume", report["stages"])
        for value in backend.private | backend.passwords:
            self.assertNotIn(value, json.dumps(report))

    def test_action_journal_failure_enters_cleanup(self):
        backend = ClaimDocker()
        seen = []
        def journal(fixture_id, event):
            seen.append(event)
            if event["action"] == "container_started_and_verified" and event["subject"] == "executor":
                raise OSError("secret-canary")
        report = ctl.execute(self.plan, fixtures.approval(self.plan), backend, revision_check=lambda _: None, action_journal=journal)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})
        self.assertNotIn("start:redis", backend.events)
        self.assertEqual([row["sequence"] for row in seen], list(range(len(seen))))
        self.assertNotIn("secret-canary", json.dumps(report))

    def test_docker_stage_preserves_only_closed_failure_receipt(self):
        req = request(self.plan)
        result = {"scope": cr.CASE, "fixture_sha256": "a" * 64, "steps": [], "acl_negatives": [], "failed_assertion": "CR01"}
        envelope = {"stage": "measure", "status": "FAIL", "recipe_sha256": case.recipe_sha256(cr.CASE), "isolation": {}, "result": result}
        with patch.object(ctl.shutil, "which", return_value="/fake/docker"):
            backend = ctl.Docker()
        with patch.object(ctl, "command", return_value=(1, h.canonical(envelope), b"secret-canary")), self.assertRaises(ctl.StageFailure) as failure:
            backend.stage("fixture", "measure", req, 30)
        self.assertEqual(failure.exception.receipt, envelope)
        result["unlisted"] = "secret-canary"
        with patch.object(ctl, "command", return_value=(1, h.canonical(envelope), b"")), self.assertRaises(h.InvalidArtifact):
            backend.stage("fixture", "measure", req, 30)


class ClaimACLTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.req = request()
        cls.fixture = case.claim_fixture(cls.req["plan"], cls.req["fixture_id"], cls.req["claim_material"])
        cls.rules = case.acl_rules(cr.CASE, cls.fixture, cls.req["plan"])

    def test_exact_command_key_grants_and_authority_separation(self):
        f, rules = self.fixture, self.rules
        groups = case.claim_key_groups(f)
        self.assertTrue(permits(rules["ledger"], "hset", f["job_key"]))
        self.assertTrue(permits(rules["ledger"], "evalsha", *f["work_keys"]))
        self.assertTrue(permits(rules["ledger"], "pexpireat", groups["reservations"][0]))
        for key in (*h.AUTH, "unlisted"):
            for command in ("hset", "set", "del", "unlink", "zadd", "zrem", "pexpireat", "expire", "rename"):
                self.assertFalse(permits(rules["ledger"], command, key), (command, key))
        for key in h.ABSENCE_ONLY:
            self.assertTrue(permits(rules["ledger"], "type", key))
            for command in ("get", "hget", "pttl", "hmget"):
                self.assertFalse(permits(rules["ledger"], command, key))
        for role in ("ledger", "observer"):
            for command in ("acl|setuser", "script|load", "config|set"):
                self.assertFalse(permits(rules[role], command))
        for row in rules.values():
            text = " ".join(row)
            self.assertFalse(any(value in text for value in ("*", "?", "[", "+@")))
        self.assertFalse(permits(rules["setup"], "hset", h.AUTH[0]))
        self.assertFalse(permits(rules["boot"], "hset", f["job_key"]))

    def test_acl_time_independence_and_manifest_injection_rejection(self):
        f = case.claim_fixture(self.req["plan"], self.req["fixture_id"], self.req["claim_material"], 2000000)
        self.assertEqual(case.acl_rules(cr.CASE, f, self.req["plan"]), self.rules)
        changed = copy.deepcopy(f)
        changed["key_inventory"].append("*")
        with self.assertRaises(h.InvalidArtifact):
            case.acl_rules(cr.CASE, changed, self.req["plan"])
        raw = case.acl_file(self.req["credentials"], cr.CASE, f, self.req["plan"])
        for secret in (*self.req["credentials"].values(), *self.req["claim_material"].values()):
            self.assertNotIn(secret.encode(), raw)

    def test_snapshot_count_hint_bounds_and_no_blind_setup(self):
        client = ClaimRedis()
        # More than COUNT keys in one SCAN reply is normal.
        client.data = {"key" + str(i): b"x" for i in range(30)}
        with client.connect("observer", {}) as connection:
            self.assertEqual(len(live.inventory(connection)), 30)
            with patch.object(client, "call", side_effect=lambda role, *args: 59 if args[0] == "DBSIZE" else None), self.assertRaises(h.InvalidArtifact):
                live.inventory(connection)
            with self.assertRaises(h.InvalidArtifact):
                live.install(connection, self.req["plan"], self.fixture, dict.fromkeys(case.BOOT_FIELDS, ""))
        self.assertFalse(any(row[1] in ("HSET", "SADD", "ZADD", "SET") for row in client.calls))

    def test_request_and_receipt_schema_fail_closed(self):
        worker.validate_request(self.req)
        for mutate in (lambda r: r.pop("claim_material"), lambda r: r["claim_material"].update(endpoint="secret-canary"),
                       lambda r: r.update(recipe_sha256=case.recipe_sha256())):
            changed = copy.deepcopy(self.req)
            mutate(changed)
            with self.assertRaises(h.InvalidArtifact):
                worker.validate_request(changed)
        result = {"scope": cr.CASE, "fixture_sha256": "a" * 64, "steps": [], "acl_negatives": [], "failed_assertion": "CR01"}
        live.validate_measurement(result, False)
        with self.assertRaises(h.InvalidArtifact):
            live.validate_measurement(dict(result, private="secret-canary"), False)
        with self.assertRaises(h.InvalidArtifact):
            live.validate_measurement(result, True)

    def test_new_case_prestart_metadata_never_starts_and_restores_context(self):
        prepare = runpy.run_path(str(h.ROOT / "scripts/prepare-crawl-jobs-v2-images.py"))
        class Metadata(fixtures.FakeDocker):
            def create(self, spec):
                super().create(spec)
                self.resources[("container", spec["name"])]["State"]["Status"] = "created"
            def start(self, name):
                raise AssertionError("metadata check started a container")
        backend = Metadata()
        backend.deadline = float("inf")
        backend.case_id = case.CASE
        report = prepare["validate_prestart"](backend, "sha256:" + "1" * 64, "sha256:" + "2" * 64, cr.CASE)
        self.assertEqual(report["status"], "PASS")
        self.assertEqual(report["case"], cr.CASE)
        self.assertEqual(backend.case_id, case.CASE)
        self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_action_receipts_are_exclusive_bounded_and_private(self):
        event = {"sequence": 0, "action": "stage_completed", "subject": "resume", "at_ms": 1}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            ctl.write_action(root, "a" * 32, event)
            ctl.write_action(root, "a" * 32, dict(event, sequence=1, subject="measure"))
            for sequence, action in enumerate(("worker_quiesced_and_removed", "credentials_revoked_and_verified", "volume_removed_and_verified"), 2):
                ctl.write_action(root, "a" * 32, dict(event, sequence=sequence, action=action, subject="cleanup"))
            path = root / ("a" * 32 + ".actions.jsonl")
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual([json.loads(line)["sequence"] for line in path.read_bytes().splitlines()], [0, 1, 2, 3, 4])
            with self.assertRaises(FileExistsError):
                ctl.write_action(root, "a" * 32, event)
            with self.assertRaises(h.InvalidArtifact):
                ctl.write_action(root, "a" * 32, dict(event, sequence=True))
            path.write_bytes(b"x" * ctl.OUTPUT_LIMIT)
            with self.assertRaises(h.InvalidArtifact):
                ctl.write_action(root, "a" * 32, dict(event, sequence=2))


if __name__ == "__main__":
    unittest.main()
