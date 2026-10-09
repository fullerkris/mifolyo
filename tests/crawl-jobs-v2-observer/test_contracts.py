import copy
import struct
import unittest

import contracts as c
import controller
import oracle
import profiles

D = c.sha(b"synthetic-only")


class PreparationTests(unittest.TestCase):
    def test_closed_276_trial_inventory_and_no_authority(self):
        self.assertEqual(len(c.TRIALS), 276)
        self.assertEqual(len(set(c.TRIALS)), 276)
        self.assertEqual([sum(name.startswith(prefix) for name in c.TRIALS) for prefix in ("P1.", "P2.", "P3.", "F.", "N.")], [140, 60, 40, 24, 12])
        value = c.preparation({key: D for key in c.ARTIFACTS})
        self.assertEqual(c.validate_preparation(c.decode(c.canonical(value))), value)
        for field in ("execution_authorized", "m4_accepted"):
            changed = copy.deepcopy(value)
            changed[field] = True
            with self.assertRaises(c.Invalid):
                c.validate_preparation(changed)

    def test_artifact_or_suite_changes_cannot_select_an_unreviewed_profile(self):
        value = c.preparation({key: D for key in c.ARTIFACTS})
        for change in (lambda x: x.update(candidate="C1"), lambda x: x["limits"].update(hold_ns=20000001),
                       lambda x: x["trial_ids"].pop(), lambda x: x["trial_ids"].append("P1.pre.3.1"),
                       lambda x: x["artifacts"].update(target_elf="0" * 64), lambda x: x.update(version=True),
                       lambda x: x.update(pid=1), lambda x: x["artifacts"].pop("filter_generator")):
            changed = copy.deepcopy(value)
            change(changed)
            with self.assertRaises(c.Invalid):
                c.validate_preparation(changed)

    def test_strict_json_rejects_ambiguous_encodings(self):
        for raw in (b'{"a":1,"a":2}\n', b'{"a":{"x":1,"x":2}}\n', b'{"a":NaN}\n', b'{"a":1.0}\n',
                    b'{"a":1}', b' {"a":1}\n', b'[]\n', b'{"a":"\xff"}\n', b'{"a":1}\n\n', b"{" * 2048):
            with self.subTest(raw=raw[:24]), self.assertRaises(c.Invalid):
                c.decode(raw)
        with self.assertRaises(c.Invalid):
            c.decode(b"x" * (c.MAX_ARTIFACT + 1))

    def test_hold_uses_effective_end_and_uncertainty(self):
        hold = {"action": "resume", "clock_domain_sha256": D, "uncertainty_ns": 1,
            "pre_witness_ns": 1000000, "dispatch_ns": 15000000, "post_continuation_ns": 20000000, "confirmed_stop_ns": None}
        self.assertEqual(c.hold_bound(hold)["value"], 19000002)
        for changed in (dict(hold, post_continuation_ns=50000000), dict(hold, post_continuation_ns=None),
                        dict(hold, uncertainty_ns=1000000), dict(hold, confirmed_stop_ns=20000000),
                        dict(hold, clock_domain_sha256="0" * 64), dict(hold, dispatch_ns=True)):
            with self.assertRaises(c.Invalid):
                c.hold_bound(changed)
        kill = dict(hold, action="kill", post_continuation_ns=None, confirmed_stop_ns=20000000)
        self.assertEqual(c.hold_bound(kill)["value"], 19000002)
        with self.assertRaises(c.Invalid):
            c.hold_bound(dict(kill, confirmed_stop_ns=50000000))

    def test_post_entrypoint_identity_is_exact_and_capless(self):
        value = {"container_sha256": D, "executable_sha256": D, "kernel_boot_sha256": D,
            "pid_namespace": 1, "user_namespace": 2, "time_namespace": 3, "uid_map_sha256": D, "gid_map_sha256": D,
            "start_ticks": 100, "pid": 1, "tid": 1, "uid": 999, "gid": 999,
            "capabilities": {key: 0 for key in c.CAPS}}
        c.same_identity(value, copy.deepcopy(value))
        for key in ("start_ticks", "pid_namespace", "user_namespace", "time_namespace", "pid", "tid"):
            changed = copy.deepcopy(value)
            changed[key] += 1
            with self.assertRaises(c.Invalid):
                c.same_identity(value, changed)
        for key in c.CAPS:
            changed = copy.deepcopy(value)
            changed["capabilities"][key] = 0x80000
            with self.assertRaises(c.Invalid):
                c.identity(changed)

    def test_hold_clock_range_matches_native_uint64(self):
        stamp = 105 * 86400 * 1000000000
        hold = {"action": "resume", "clock_domain_sha256": D, "uncertainty_ns": 1,
            "pre_witness_ns": stamp, "dispatch_ns": stamp + 1000000,
            "post_continuation_ns": stamp + 10000000, "confirmed_stop_ns": None}
        self.assertEqual(c.hold_bound(hold)["value"], 10000002)
        for change in ({"pre_witness_ns": True}, {"post_continuation_ns": 2**64},
                       {"post_continuation_ns": stamp + 20000001}):
            with self.assertRaises(c.Invalid):
                c.hold_bound(hold | change)

    def test_profiles_exclude_other_pid_write_registers_and_namespace_escalation(self):
        for role in ("target", "observer", "oracle"):
            profile = profiles.profile(role)
            self.assertEqual(profile["defaultAction"], "SCMP_ACT_ERRNO")
            self.assertEqual(profile["architectures"], ["SCMP_ARCH_AARCH64"])
            all_names = {name for row in profile["syscalls"] for name in row["names"]}
            self.assertFalse(all_names & {"setns", "unshare", "mount", "bpf", "perf_event_open", "process_vm_readv", "process_vm_writev"})
            if role != "observer":
                self.assertNotIn("ptrace", all_names)
        rows = [row for row in profiles.profile("observer")["syscalls"] if row["names"] == ["ptrace"]]
        requests = {row["args"][0]["value"] for row in rows}
        self.assertEqual(requests, {0x4206, 0x4207, 0x4202, 0x4204, 0x4205, 2, 7, 9, 17})
        self.assertTrue(all(row["args"][1] == {"index": 1, "value": 1, "op": "SCMP_CMP_EQ"} for row in rows))
        self.assertEqual([row["args"][2]["value"] for row in rows if row["args"][0]["value"] == 0x4205], [0x402])
        for row in profiles.profile("observer")["syscalls"]:
            if "prctl" in row["names"]:
                self.assertIn(row["args"][0]["value"], (3, 21, 22, 39))

    def test_role_specs_never_grant_tracing_authority_or_target_mount_to_observer(self):
        value = controller.role_spec("observer", "sha256:" + D, D)
        self.assertIsNone(value["witness_mount"])
        self.assertEqual(value["cap_add"], [])
        self.assertFalse(value["execution_authorized"])
        self.assertEqual(value["pid_namespace"], "exact_owned_target")
        with self.assertRaises(c.Invalid):
            controller.role_spec("C1", "sha256:" + D, D)


class OracleTests(unittest.TestCase):
    def test_native_witness_preserves_long_uptime_clock_words(self):
        stamp = 105 * 86400 * 1000000000
        words = [oracle.MAGIC, 0, 1, 0, 0, stamp, stamp - 1, 0, 0, 0] + [0] * 1050
        state = oracle.decode_witness(struct.pack("<1060Q", *words))
        self.assertTrue(oracle.held("P1.pre.1.1", state)["ordinal_matches"])
        for bad in (True, -1, 2**64):
            with self.assertRaises(c.Invalid):
                oracle.validate_state(state | {"pre_ns": bad})

    def state(self, ordinal=129, post=False):
        done = ordinal if post else ordinal - 1
        words = [oracle.MAGIC, done, ordinal, 0, 0, 1000, 900, 0, 0, 42 if done else 0]
        words += list(range(1, done + 1)) + [0] * (1050 - done)
        return oracle.decode_witness(struct.pack("<1060Q", *words))

    def test_pre_post_boundaries_preserve_same_values_and_distinct_ordinals(self):
        for ordinal in (1, 2, 127, 128, 129, 1049, 1050):
            for post in (False, True):
                state = self.state(ordinal, post)
                self.assertTrue(oracle.held(f"P1.{'post' if post else 'pre'}.{ordinal}.1", state)["ordinal_matches"])
                changed = copy.deepcopy(state)
                changed["completed"] = (changed["completed"] + 1) % 1051
                with self.assertRaises(c.Invalid):
                    oracle.held(f"P1.{'post' if post else 'pre'}.{ordinal}.1", changed)

    def test_counter_without_complete_transcript_fails(self):
        state = self.state()
        for index in (0, 126, 127, 128, 1049):
            changed = copy.deepcopy(state)
            changed["transcript"] = list(changed["transcript"])
            changed["transcript"][index] = 42
            with self.assertRaises(c.Invalid):
                oracle.held("P1.pre.129.1", changed)

    def test_nested_fake_vm_checks_prototype_current_pc_and_occurrence(self):
        for prototype, pc, ordinal in (("a", 0, 1), ("a", 1, 2), ("b", 0, 3), ("b", 1, 4), ("b", 7, 5), ("a", 7, 6)):
            state = self.state(1)
            state.update(prototype=1 if prototype == "a" else 2, bytecode_pc=pc, ordinal=ordinal)
            oracle.held(f"P2.{prototype}.{pc}.1", state)
            for key in ("prototype", "bytecode_pc", "ordinal"):
                changed = dict(state)
                changed[key] += 1
                with self.assertRaises(c.Invalid):
                    oracle.held(f"P2.{prototype}.{pc}.1", changed)

    def test_async_held_states_match_the_fixed_native_cut(self):
        state = self.state(1)
        for name in ("heartbeat", "pre-send", "received", "receipt-race"):
            case = f"P3.{name}.1"
            oracle.held(case, state)
            for change in (lambda x: x.update(ordinal=777, completed=776),
                           lambda x: x.update(prototype=1), lambda x: x.update(last_value=42),
                           lambda x: x.update(transcript=[1] + [0] * 1049)):
                altered = copy.deepcopy(state)
                change(altered)
                with self.assertRaises(c.Invalid):
                    oracle.held(case, altered)
        for name in ("pre-send", "received", "receipt-race"):
            with self.assertRaises(c.Invalid):
                oracle.held(f"P3.{name}.1", dict(state, heartbeat=1))

    def test_final_state_is_complete_typed_and_case_specific(self):
        loop = self.state(1050, True)
        loop.update(finished=1, post_ns=1001)
        vm = self.state(1)
        vm.update(finished=1, prototype=1, bytecode_pc=7, ordinal=6, post_ns=1001)
        frame = self.state(1)
        frame.update(finished=1, post_ns=1001)
        for case, state in (("P1.pre.1.1", loop), ("P3.heartbeat.1", loop), ("P2.a.0.1", vm), ("P3.pre-send.1", frame)):
            oracle.completed(case, state)
            for change in (lambda x: x.pop("magic"), lambda x: x.update(finished=True),
                           lambda x: x.update(transcript=[]), lambda x: x.update(ordinal=0),
                           lambda x: x.update(extra=1), lambda x: x.update(pre_ns=1.5)):
                altered = copy.deepcopy(state)
                change(altered)
                with self.assertRaises(c.Invalid):
                    oracle.completed(case, altered)
        for case, changed in (("P2.a.0.1", dict(vm, prototype=2, bytecode_pc=0, ordinal=1)),
                              ("P2.a.0.1", dict(vm, completed=1)), ("P1.pre.1.1", dict(loop, ordinal=1))):
            with self.assertRaises(c.Invalid):
                oracle.completed(case, changed)
        for case in ("N.01", "F.held-loss.1"):
            with self.assertRaises(c.Invalid):
                oracle.completed(case, loop)


class CoordinatorTests(unittest.TestCase):
    def event(self, model, kind, body=None):
        return {"case_id": model.case_id, "invocation_sha256": D, "sequence": model.count,
            "previous": model.previous, "kind": kind, "payload": body if body is not None else {"evidence_sha256": D}}

    def test_no_early_release_or_dispatch_only_completion(self):
        model = controller.Coordinator("P1.pre.1.1", D)
        with self.assertRaises(c.Invalid):
            model.advance(self.event(model, "RESUME_DISPATCHED"))
        for phase in ("ADMITTED", "TARGET_READY", "OBSERVER_ARMED", "TARGET_STARTED", "HELD", "ORACLE_MATCHED", "RESUME_DISPATCHED"):
            result = model.advance(self.event(model, phase))
            self.assertFalse(result["runtime_evidence"])
        with self.assertRaises(c.Invalid):
            model.advance(self.event(model, "TARGET_FINISHED"))
        with self.assertRaises(c.Invalid):
            model.advance(self.event(model, "ACTUAL_END", {"dispatch_ns": 1}))

    def test_failure_cannot_reenter_successful_trace_and_requires_absence(self):
        model = controller.Coordinator("P1.pre.1.1", D)
        result = model.advance(self.event(model, "INVALID", {"cleanup_required": True}))
        self.assertTrue(result["failed"])
        with self.assertRaises(c.Invalid):
            model.advance(self.event(model, "ADMITTED"))
        with self.assertRaises(c.Invalid):
            model.advance(self.event(model, "ABSENCE_VERIFIED", {"inventory_sha256": D, "all_owned_absent": True,
                "independent_inspection": False, "unresolved": []}))

    def test_duplicate_reordered_cross_case_and_leaky_native_receipts_fail(self):
        model = controller.Coordinator("P1.pre.1.1", D)
        event = self.event(model, "ADMITTED")
        model.advance(event)
        with self.assertRaises(c.Invalid):
            model.advance(event)
        changed = self.event(model, "TARGET_READY")
        changed["case_id"] = "P1.pre.2.1"
        with self.assertRaises(c.Invalid):
            model.advance(changed)
        value = {"events": 2, "ordinal": 1, "phase": "HELD", "read_bytes": 8, "version": 1}
        controller.native_message(c.canonical(value), "HELD")
        for key in ("pc", "registers", "memory", "token", "error"):
            with self.assertRaises(c.Invalid):
                controller.native_message(c.canonical(dict(value, **{key: "private-canary"})), "HELD")


if __name__ == "__main__":
    unittest.main()
