"""Independent-review regressions; local children, temporary files and fakes only."""
import copy
import io
import os
from pathlib import Path
import signal
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import admission
import controller as ctl
import harness as h
import parked_command as parked
import recovery_specs as spec
import test_execution as base
from test_recovery_execution import RecoveryDocker, FakeParked


class ParkReviewTests(unittest.TestCase):
    def test_nonreaping_exit_observation_and_inherited_open_pipes(self):
        with tempfile.TemporaryDirectory() as directory:
            marker = Path(directory) / "reparented"
            source = """import os,signal,sys,time
from pathlib import Path
sys.stdin.buffer.read()
leader=os.getpid()
child=os.fork()
if child == 0:
    deadline=time.monotonic()+3
    while os.getppid()==leader and time.monotonic()<deadline:
        time.sleep(.01)
    Path(sys.argv[1]).write_text(str(os.getppid()!=leader))
    signal.alarm(5)
    signal.pause()
    os._exit(0)
print('{"ready":true}',flush=True)
time.sleep(.1)
os._exit(137)
"""
            handle = None
            try:
                try:
                    handle, _ = parked.start([sys.executable, "-B", "-c", source, str(marker)], b"{}\n", 8)
                except parked.ParkedCommandError as error:
                    self.assertIn(str(error), ("PARK_EARLY_EXIT", "PARK_NOT_WAITING"))
                    return
                deadline = time.monotonic() + 3
                while not marker.exists() and time.monotonic() < deadline:
                    time.sleep(.01)
                self.assertEqual(marker.read_text(), "True")
                self.assertTrue(parked.child_has_exited(handle.process.pid))
                self.assertTrue(parked.child_has_exited(handle.process.pid))
                self.assertIsNone(handle.process.returncode)
                with self.assertRaisesRegex(parked.ParkedCommandError, "PARK_EARLY_EXIT"):
                    handle.require_waiting()
            finally:
                if handle is not None:
                    handle.abort()
                    self.assertTrue(handle.finished)

    def test_post_handshake_stderr_is_not_a_valid_exit_receipt(self):
        source = "import sys,time; sys.stdin.buffer.read(); print('{\"ready\":true}',flush=True); time.sleep(.2); sys.stderr.write('private-diagnostic-canary\\n'); sys.stderr.flush(); sys.exit(137)"
        handle, _ = parked.start([sys.executable, "-B", "-c", source], b"{}\n", 5)
        try:
            with self.assertRaisesRegex(parked.ParkedCommandError, "PARK_EXTRA_STDERR") as error:
                handle.finish(2)
            self.assertNotIn("private-diagnostic-canary", str(error.exception))
            self.assertTrue(handle.finished)
        finally:
            handle.abort()

    def test_unproven_group_cleanup_stays_unproven_after_reaping(self):
        process = SimpleNamespace(pid=12345, returncode=None, stdin=None, stdout=io.BytesIO(), stderr=io.BytesIO(), wait=lambda **_: 137)
        handle = parked.ParkedCommand(process, time.monotonic() + 30)
        with patch.object(parked.os, "killpg", side_effect=[PermissionError(), None]) as kill:
            with self.assertRaisesRegex(parked.ParkedCommandError, "PARK_SESSION_TERMINATION_UNPROVEN"):
                handle.abort()
            self.assertTrue(handle.leader_reaped)
            self.assertFalse(handle.finished)
            with self.assertRaisesRegex(parked.ParkedCommandError, "PARK_SESSION_TERMINATION_UNPROVEN"):
                handle.abort()
            self.assertEqual(kill.call_count, 2)

    def test_container_stop_and_attached_exit_have_distinct_explicit_bounds(self):
        for elapsed in (2, 6):
            backend = RecoveryDocker()
            backend.case_id, backend.deadline, backend.approval_expires_at_ms = spec.CASE, 200.0, None
            fid, name = "a" * 32, "owned-executor"
            model = ctl.container_spec(name, "executor", fid, "sha256:" + "2" * 64,
                                       {"control": "own-control", "data": "own-data"}, spec.CASE)
            backend.create(model)
            backend.start(name)
            clock = [100.0]
            class Attached:
                received_at = 100.0
                def require_waiting(self):
                    pass
                def finish(self, timeout):
                    self.timeout = timeout
                    clock[0] += elapsed
                    return 137
            with patch.object(ctl.time, "monotonic", lambda: clock[0]):
                if elapsed == 6:
                    with self.assertRaisesRegex(h.InvalidArtifact, "RECOVERY_CLAIMANT_EXIT_TIMEOUT"):
                        ctl.Docker.kill_claimant(backend, name, fid, backend.resources[("container", name)]["Id"], Attached())
                else:
                    proof = ctl.Docker.kill_claimant(backend, name, fid, backend.resources[("container", name)]["Id"], Attached())
                    self.assertEqual(proof["ack_to_stopped_ms"], 0)
                    self.assertEqual(proof["attached_exit_reconciliation_ms"], 2000)
                    self.assertEqual(proof["receipt_to_attached_exit_ms"], 2000)
                    self.assertEqual(proof["timing_origin"], "host_received_complete_claim_receipt")


class JournalReviewTests(unittest.TestCase):
    def event(self, sequence, subject="probe"):
        return {"sequence": sequence, "action": "stage_completed", "subject": subject, "at_ms": 1000 + sequence}

    def test_truncated_replaced_and_forged_prefixes_reject(self):
        for fault in ("truncate", "rewrite", "replace"):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                root, fid = Path(directory), "a" * 32
                journal = ctl.ActionJournal(root)
                journal(fid, self.event(0))
                path = root / (fid + ".actions.jsonl")
                if fault == "truncate":
                    path.write_bytes(b"")
                elif fault == "rewrite":
                    path.write_bytes(h.canonical(self.event(0, "different")))
                else:
                    replacement = root / "replacement"
                    replacement.write_bytes(path.read_bytes())
                    replacement.chmod(0o600)
                    replacement.replace(path)
                with self.assertRaises(h.InvalidArtifact):
                    journal(fid, self.event(1))

    def test_stateless_append_also_rejects_truncated_sequence(self):
        with tempfile.TemporaryDirectory() as directory:
            root, fid = Path(directory), "b" * 32
            ctl.write_action(root, fid, self.event(0))
            (root / (fid + ".actions.jsonl")).write_bytes(b"")
            with self.assertRaisesRegex(h.InvalidArtifact, "ACTION_SEQUENCE"):
                ctl.write_action(root, fid, self.event(1))

    def test_final_reconciliation_binds_exact_report_actions(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            journal = ctl.ActionJournal(root)
            plan = base.test_plan()
            report = ctl.execute(plan, base.approval(plan), base.FakeDocker(), revision_check=lambda _: None,
                journal=lambda value: ctl.write_report(root, value, intent=True), action_journal=journal)
            self.assertEqual(report["verdict"], "PASS")
            self.assertFalse(report["case_evidence_valid"])
            self.assertEqual(journal.verify(report["fixture_id"], report["actions"]), report["journal_integrity"])
            self.assertEqual(report["journal_integrity"]["actions"], len(report["actions"]))

    def test_truncation_after_last_append_invalidates_pass_without_skipping_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            class Truncated(ctl.ActionJournal):
                def __call__(self, fid, event):
                    super().__call__(fid, event)
                    if event["action"] == "volume_removed_and_verified" and event["subject"].endswith("-data"):
                        (self.directory / (fid + ".actions.jsonl")).write_bytes(b"")
            backend, plan = base.FakeDocker(), base.test_plan()
            report = ctl.execute(plan, base.approval(plan), backend, revision_check=lambda _: None,
                journal=lambda value: ctl.write_report(root, value, intent=True), action_journal=Truncated(root))
            self.assertTrue(report["case_passed"])
            self.assertEqual(report["verdict"], "FAIL")
            self.assertIn("journal_failure", report)
            self.assertEqual(report["journal_integrity"], "not_proven")
            self.assertTrue(all(row["removed"] for row in report["cleanup"]))

    def test_missing_final_verifier_cannot_claim_journal_integrity(self):
        with tempfile.TemporaryDirectory() as directory:
            root, plan = Path(directory), base.test_plan()
            report = ctl.execute(plan, base.approval(plan), base.FakeDocker(), revision_check=lambda _: None,
                action_journal=lambda fid, event: ctl.write_action(root, fid, event))
            self.assertEqual(report["verdict"], "FAIL")
            self.assertIn("journal_failure", report)

    def test_recovery_kill_prefix_truncation_stops_progress_and_cleans_up(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            class Truncated(ctl.ActionJournal):
                def __call__(self, fid, event):
                    if event["action"] == "container_killed_and_verified" and event["subject"] == "executor":
                        (self.directory / (fid + ".actions.jsonl")).write_bytes(b"")
                    super().__call__(fid, event)
            backend, plan = RecoveryDocker(), base.test_plan(spec.SCENARIO)
            report = ctl.execute(plan, dict(base.approval(plan), max_seconds=300), backend, revision_check=lambda _: None,
                journal=lambda value: ctl.write_report(root, value, intent=True), action_journal=Truncated(root))
            self.assertEqual(report["verdict"], "FAIL")
            self.assertIn("journal_failure", report)
            self.assertNotIn("create:executor_b", backend.events)
            self.assertEqual(report["revocation"], "verified")
            self.assertTrue(all(row["removed"] for row in report["cleanup"]))
            self.assertEqual(set(backend.resources), {("volume", "retained-evidence")})

    def test_complete_recovery_has_bound_retained_journal(self):
        with tempfile.TemporaryDirectory() as directory:
            root, plan, backend = Path(directory), base.test_plan(spec.SCENARIO), RecoveryDocker()
            journal = ctl.ActionJournal(root)
            report = ctl.execute(plan, dict(base.approval(plan), max_seconds=300), backend, revision_check=lambda _: None,
                journal=lambda value: ctl.write_report(root, value, intent=True), action_journal=journal)
            self.assertEqual(report["verdict"], "PASS", report.get("failure_details"))
            self.assertFalse(report["case_evidence_valid"])
            self.assertEqual(report["journal_integrity"], journal.verify(report["fixture_id"], report["actions"]))
            self.assertEqual(len(report["cleanup"]), 7)


class IsolationReviewTests(unittest.TestCase):
    def test_closed_isolation_schema_and_role_predicates(self):
        for stage in ("init", "measure"):
            admission.validate_isolation_receipt(base.fake_isolation(stage), init=stage == "init")
        valid = base.fake_isolation()
        for field, value in (("uid", 0), ("effective_capabilities", "0000000000000001"),
                ("external_routes", 1), ("external_routes", False), ("process_count", 2.0),
                ("process_inventory_sha256", "0" * 64), ("interfaces", ["lo", "eth0"]),
                ("interfaces", ["lo", "lo"]), ("inactive_fallbacks", ["tunl0"]),
                ("unexpected_diagnostic", "private-diagnostic-canary")):
            with self.subTest(field=field), self.assertRaises(h.InvalidArtifact) as error:
                admission.validate_isolation_receipt(dict(valid, **{field: value}))
            self.assertNotIn("private-diagnostic-canary", str(error.exception))

    def test_normal_failed_and_parked_envelopes_reject_unknown_isolation(self):
        with patch.object(ctl.shutil, "which", return_value="/fake/docker"):
            backend = ctl.Docker()
        for status in ("PASS", "FAIL"):
            plan = base.test_plan()
            response = {"stage": "ready", "status": status, "recipe_sha256": case_recipe(plan),
                        "isolation": dict(base.fake_isolation(), unexpected="private-canary"), "result": {}}
            with patch.object(ctl, "command", return_value=(0 if status == "PASS" else 1, h.canonical(response), b"")), self.assertRaises(h.InvalidArtifact):
                backend.stage("fixture", "ready", {"plan": plan}, 5)
        plan = base.test_plan(spec.SCENARIO)
        handle = FakeParked()
        response = {"stage": "claim_park", "status": "PASS", "recipe_sha256": case_recipe(plan),
                    "isolation": dict(base.fake_isolation(), uid=0), "result": {}}
        with patch.object(parked, "start", return_value=(handle, response)), self.assertRaises(h.InvalidArtifact):
            backend.park_stage("fixture", {"plan": plan}, 5)
        self.assertTrue(handle.aborted)

    def test_controller_retention_rejects_invalid_backend_isolation(self):
        class Substituted(base.FakeDocker):
            def stage(self, *args):
                value = super().stage(*args)
                value["isolation"]["raw_diagnostic"] = "unlisted-private-canary"
                return value
        plan = base.test_plan()
        report = ctl.execute(plan, base.approval(plan), Substituted(), revision_check=lambda _: None)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertNotIn("init", report["stages"])
        self.assertNotIn(b"unlisted-private-canary", h.canonical(report))
        self.assertTrue(all(row["removed"] for row in report["cleanup"]))


def case_recipe(plan):
    return ctl.case.recipe_sha256(ctl.case.case_for_plan(plan))


if __name__ == "__main__":
    unittest.main()
