"""Watchdog protocol and benign supervisor-process tests; fake Docker only."""
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

import contracts as c
from owned_resources import Registry
from test_owned_resources import fixture_scope, prepared
from watchdog import Journal, WatchdogProcess, WatchState, _default_backend, validate_cleanup


class AbsentBackend:
    evidence_kind = "simulated"

    def inspect(self, kind, selector, deadline):
        deadline.check()
        return None


def absent_backend(value, profile_files, cleanup_authorizer):
    return AbsentBackend()


def delegate_cleanup(action, scope_sha256):
    return action == "cleanup" and scope_sha256 == c.sha(c.canonical(fixture_scope()))


def profiles():
    return {role: "/synthetic-unused" for role in ("target", "observer", "oracle")}


class WatchStateTests(unittest.TestCase):
    def test_resource_prefix_cannot_rollback_rebind_or_skip_generation(self):
        registry, _, history, _ = prepared()
        for fault in ("rollback", "rebind", "skip", "scope"):
            watch = WatchState(registry.scope)
            for row in history:
                watch.accept(row)
            changed = copy.deepcopy(history[-1])
            if fault == "rollback":
                changed = copy.deepcopy(history[-2])
            elif fault == "rebind":
                changed["entries"]["target"]["reference"]["id"] = "f" * 64
            elif fault == "skip":
                changed["generation"] += 2
            else:
                changed["scope"]["invocation_id"] = "2" * 32
            with self.subTest(fault=fault), self.assertRaises(c.Invalid):
                watch.accept(changed)
            self.assertEqual(watch.current, history[-1])

    def test_complete_cleanup_cannot_omit_resources_or_coerce_true(self):
        from owned_resources import cleanup
        from streams import Deadline
        registry, backend, _, _ = prepared()
        result = cleanup(registry.snapshot(), backend, Deadline(2))
        validate_cleanup(result, registry.snapshot())
        for change in (lambda value: value["resources"].pop(), lambda value: value.update(complete=1),
                       lambda value: value["resources"][0].update(absent=1), lambda value: value.update(execution_authorized=True)):
            changed = copy.deepcopy(result)
            change(changed)
            with self.assertRaises(c.Invalid):
                validate_cleanup(changed, registry.snapshot())


class JournalTests(unittest.TestCase):
    def test_exclusive_file_prefix_and_replacement_are_checked(self):
        with tempfile.TemporaryDirectory() as directory:
            journal = Journal(directory, "1" * 32)
            try:
                journal.append("ready", {"synthetic": True})
                with self.assertRaises(FileExistsError):
                    Journal(directory, "1" * 32)
                original = journal.path.read_bytes()
                journal.path.write_bytes(original.replace(b"true", b"fals"))
                with self.assertRaisesRegex(c.Invalid, "JOURNAL_CHANGED"):
                    journal.append("triggered", {"reason": "requested"})
            finally:
                journal.close()
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(c.Invalid):
                Journal(directory, "../escape")


class WatchdogProcessTests(unittest.TestCase):
    def test_scope_hash_alone_never_grants_cleanup_authority(self):
        value = fixture_scope()
        backend = _default_backend(value, profiles())
        with self.assertRaisesRegex(c.Invalid, "EXECUTION_NOT_APPROVED"):
            backend._authorize("cleanup")
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(c.Invalid, "CLEANUP_NOT_DELEGATED"):
                WatchdogProcess(value, profiles(), directory, backend_factory=absent_backend)
            self.assertEqual(list(Path(directory).iterdir()), [])
        backend = _default_backend(value, profiles(), delegate_cleanup)
        backend._authorize("cleanup")
        with self.assertRaisesRegex(c.Invalid, "EXECUTION_NOT_APPROVED"):
            backend._authorize("start")

    def test_separate_session_acknowledges_each_candidate_and_retains_cleanup(self):
        value = fixture_scope()
        with tempfile.TemporaryDirectory() as directory:
            watchdog = WatchdogProcess(value, profiles(), directory, cleanup_authorizer=delegate_cleanup, backend_factory=absent_backend)
            try:
                self.assertNotEqual(os.getsid(0), os.getsid(watchdog._process.pid))
                registry = Registry(value, watchdog.publish)
                registry.candidate("control")
                result = watchdog.close()
                self.assertEqual(result["reason"], "requested")
                self.assertTrue(result["journal_intact"])
                self.assertFalse(result["result"]["complete"])
                self.assertFalse(result["result"]["resources"][0]["creation_settled"])
                self.assertEqual(result["result"]["evidence_kind"], "simulated")
            finally:
                if not watchdog.closed:
                    watchdog.abandon()
            rows = [json.loads(line) for line in (Path(directory) / (value["invocation_id"] + ".watchdog.jsonl")).read_bytes().splitlines()]
            self.assertEqual([row["kind"] for row in rows], ["ready", "armed", "armed", "triggered", "cleanup"])

    def test_owner_endpoint_eof_is_an_independent_cleanup_trigger(self):
        value = fixture_scope()
        with tempfile.TemporaryDirectory() as directory:
            watchdog = WatchdogProcess(value, profiles(), directory, cleanup_authorizer=delegate_cleanup, backend_factory=absent_backend)
            registry = Registry(value, watchdog.publish)
            registry.candidate("witness")
            watchdog.abandon()
            rows = [json.loads(line) for line in (Path(directory) / (value["invocation_id"] + ".watchdog.jsonl")).read_bytes().splitlines()]
            self.assertEqual(rows[-1]["payload"]["reason"], "owner_eof")
            self.assertFalse(rows[-1]["payload"]["result"]["complete"])
            self.assertTrue(rows[-1]["payload"]["result"]["resources"][0]["absent"])

    def test_watchdog_survives_actual_benign_owner_process_exit(self):
        value = fixture_scope()
        with tempfile.TemporaryDirectory() as directory:
            script = "\n".join(("import os,sys", "sys.path.insert(0," + repr(str(Path(__file__).resolve().parent)) + ")",
                "from test_watchdog import absent_backend,profiles,delegate_cleanup", "from test_owned_resources import fixture_scope",
                "from watchdog import WatchdogProcess", "from owned_resources import Registry",
                "value=fixture_scope()", "watch=WatchdogProcess(value,profiles()," + repr(directory) + ",cleanup_authorizer=delegate_cleanup,backend_factory=absent_backend)",
                "registry=Registry(value,watch.publish)", "registry.candidate('control')", "os._exit(0)"))
            result = subprocess.run([sys.executable, "-I", "-B", "-c", script], capture_output=True, timeout=10)
            self.assertEqual(result.returncode, 0)
            path = Path(directory) / (value["invocation_id"] + ".watchdog.jsonl")
            deadline = time.monotonic() + 7
            final = None
            while time.monotonic() < deadline:
                raw = path.read_bytes()
                if raw.endswith(b"\n"):
                    rows = [json.loads(line) for line in raw.splitlines()]
                    if rows[-1]["kind"] == "cleanup":
                        final = rows[-1]["payload"]
                        break
                time.sleep(0.02)
            self.assertIsNotNone(final)
            self.assertEqual(final["reason"], "owner_eof")
            self.assertFalse(final["result"]["complete"])
            self.assertEqual(final["result"]["evidence_kind"], "simulated")

    def test_deadline_triggers_without_an_owner_cleanup_request(self):
        value = fixture_scope()
        with tempfile.TemporaryDirectory() as directory:
            watchdog = WatchdogProcess(value, profiles(), directory, cleanup_authorizer=delegate_cleanup, lifetime_seconds=1.5, backend_factory=absent_backend)
            Registry(value, watchdog.publish)
            result = watchdog.wait_for_cleanup()
            self.assertEqual(result["reason"], "deadline")
            self.assertTrue(result["journal_intact"])
            self.assertTrue(result["result"]["complete"])

    def test_close_collects_an_already_queued_deadline_result(self):
        value = fixture_scope()
        with tempfile.TemporaryDirectory() as directory:
            watchdog = WatchdogProcess(value, profiles(), directory, cleanup_authorizer=delegate_cleanup,
                lifetime_seconds=1.5, backend_factory=absent_backend)
            Registry(value, watchdog.publish)
            watchdog._process.join(4)
            self.assertFalse(watchdog._process.is_alive())
            result = watchdog.close()
            self.assertEqual(result["reason"], "deadline")
            self.assertTrue(result["journal_intact"])

    def test_final_retention_requires_exact_acknowledged_prefix(self):
        value = fixture_scope()
        with tempfile.TemporaryDirectory() as directory:
            watchdog = WatchdogProcess(value, profiles(), directory, cleanup_authorizer=delegate_cleanup, backend_factory=absent_backend)
            Registry(value, watchdog.publish)
            final = watchdog.close()
            with self.assertRaisesRegex(c.Invalid, "WATCHDOG_RETENTION_UNPROVEN"):
                watchdog._validate_result(dict(final, journal_intact=False, journal_sha256=None))
            original = watchdog.journal_path.read_bytes()
            rows = [json.loads(line) for line in original.splitlines()]
            rows = [row for row in rows if row["kind"] != "armed"]
            rebuilt = b""
            for number, row in enumerate(rows):
                row.update(sequence=number, previous=c.sha(rebuilt) if rebuilt else None)
                rebuilt += c.canonical(row)
            watchdog.journal_path.write_bytes(rebuilt)
            with self.assertRaisesRegex(c.Invalid, "WATCHDOG_ACKNOWLEDGED_PREFIX"):
                watchdog._validate_result(dict(final, journal_sha256=c.sha(rebuilt)))


if __name__ == "__main__":
    unittest.main()
