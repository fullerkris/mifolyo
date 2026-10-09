import copy
import unittest

import contracts as c
import d02

D = c.sha(b"synthetic-only")
BOOT = c.sha(b"synthetic-canonical-boot-receipt")
OLD = c.sha(b"old-process")
NEW = c.sha(b"new-process")
RESOURCES = {c.sha(b"target-resource"), c.sha(b"control-resource")}


def payloads():
    effects = {"effects": [{"invocation_sha256": D, "effect": "publication", "oracle_sha256": D,
        "recovery": "verified", "evidence_refs": [D]}]}
    inventory = c.sha(c.canonical({"resources": sorted(RESOURCES)}))
    return {
        "R1": {"fault": "unmodified_aof_commit_process_crash", "tested_invocation_sha256": D,
            "tested_effect": {"kind": "publication", "mutation_invocation_sha256": D, "oracle_sha256": D},
            "boundary_manifest_sha256": D, "effect_ledger_sha256": d02.ledger_identity(effects), "inventory_sha256": inventory,
            "assertions": ["atomicity", "durability", "boot", "online_revocation", "disposal"], "outcomes": ["loaded"],
            "case_ms": 300000, "stage_ms": 30000, "cleanup_ms": 60000},
        "R2": {"invocation_sha256": D, "session_sha256": D, "request_sha256": D, "response_schema_sha256": D,
            "redis_process_sha256": OLD, "attempt": 1, "connection_generation": 1, "received_before_cut": True,
            "affirmative_pre_ack": False, "cut_order_proved": True, "closure_proved": True, "complete_reply_after_cut": False,
            "target_stopped": True, "client_retired": True, "classification": "acknowledged", "response_effect": "publication"},
        "R3": effects,
        "R4": {"before_process_sha256": OLD, "after_process_sha256": NEW, "volume_identity_sha256": D,
            "aof_before_sha256": D, "aof_after_sha256": D, "declared_edit": False, "loader": "loaded", "error": "none",
            "source_sha256": D, "source_line_start": 100, "source_line_end": 110, "diagnostic_sha256": D,
            "stop_proved": True, "ordinary_proof_available": True},
        "R5": {"post_state": "verified", "oracle_sha256": D, "current_boot": "approved", "new_boot_approved": True, "boot_receipt_sha256": BOOT},
        "R6": {"observed": True, "prefault": False, "role_inventory_sha256": D, "all_sessions_terminated": True,
            "all_reconnects_wrongpass": True, "server_process_sha256": NEW, "default_denied": True, "revoker_last": True},
        "R8": {"inventory_sha256": inventory, "resources": [{"identity_sha256": value, "owned": True,
            "foreign_attachment": False, "stopped": True, "removed": True, "absent": True, "inspection_succeeded": True} for value in sorted(RESOURCES)], "unresolved": []},
        "R9": {"assertions": {key: "pass" if key not in ("extinction", "startup_refusal") else "not_applicable" for key in d02.ASSERTIONS},
            "execution_authorized": False, "implementation_proven": False, "m4_accepted": False, "extinction_eligible": False}}


def packet(values):
    bindings = {key: D for key in d02.BINDINGS}
    bindings.update(case_id="D02_PROCESS_COMMIT" if values["R1"]["fault"] == "unmodified_aof_commit_process_crash" else "D02_CORRUPTION_ADMISSION",
        fixture_id="a" * 32, redis_image="sha256:" + D, harness_image="sha256:" + D)
    output, contexts, chains = {}, {}, {}
    for kind, value in values.items():
        role = d02.ROLES[kind]
        sequence, previous = chains.get(role, (0, None))
        issuer = {"role": role, "image": "sha256:" + D, "process_identity_sha256": c.sha(role.encode())}
        row = {"domain": d02.DOMAIN, "version": 1, "kind": kind, "bindings": bindings,
            "issuer": issuer, "sequence": sequence, "previous_receipt_sha256": previous,
            "payload": value, "evidence_refs": [D, BOOT] if kind == "R5" and value["new_boot_approved"] else [D]}
        raw = c.canonical(row)
        output[kind] = raw
        contexts[kind] = {"bindings": bindings, "issuer": issuer, "sequence": sequence,
            "previous": previous, "registered_refs": {
                D: {"kind": "state_oracle", "bindings_sha256": c.sha(c.canonical(bindings)), "redis_process_sha256": NEW},
                BOOT: {"kind": "canonical_boot_approval", "bindings_sha256": c.sha(c.canonical(bindings)), "redis_process_sha256": NEW}}}
        chains[role] = sequence + 1, c.sha(raw)
    return output, contexts


def check(values):
    raw, contexts = packet(values)
    return d02.reconcile(raw, contexts, RESOURCES)


class D02Tests(unittest.TestCase):
    def test_valid_synthetic_packet_never_grants_execution_or_acceptance(self):
        result = check(payloads())
        self.assertTrue(result["structural_and_semantic_contract_valid"])
        self.assertTrue(all(result[key] is False for key in ("execution_authorized", "implementation_proven", "extinction_eligible", "m4_accepted")))

    def test_ack_nonreceipt_is_affirmative_and_closure_is_separate(self):
        value = payloads()["R2"]
        self.assertEqual(d02.ack(value)["classification_at_cut"], "acknowledged")
        value.update(received_before_cut=False, affirmative_pre_ack=True, classification="known_pre_acknowledgment", response_effect=None)
        self.assertTrue(d02.ack(value)["closed_no_reply_window"])
        value["complete_reply_after_cut"] = True
        value["response_effect"] = "publication"
        self.assertEqual(d02.ack(value)["classification_at_cut"], "known_pre_acknowledgment")
        self.assertFalse(d02.ack(value)["closed_no_reply_window"])
        value.update(affirmative_pre_ack=False)
        with self.assertRaises(c.Invalid):
            d02.ack(value)
        value["classification"] = "unknown"
        self.assertFalse(d02.ack(value)["closed_no_reply_window"])

    def test_ack_contradiction_wrong_order_and_bool_integer_reject(self):
        for changes in ({"affirmative_pre_ack": True}, {"cut_order_proved": False}, {"attempt": True},
                        {"connection_generation": 2}, {"received_before_cut": 1}, {"session_sha256": "0" * 64}):
            with self.subTest(changes=changes), self.assertRaises(c.Invalid):
                d02.ack(dict(payloads()["R2"], **changes))

    def test_current_exception_stays_disabled_for_all_ack_classes(self):
        for ack in ("known_pre_acknowledgment", "acknowledged", "unknown"):
            value = payloads()
            value["R2"].update(classification=ack, received_before_cut=ack == "acknowledged",
                affirmative_pre_ack=ack == "known_pre_acknowledgment", cut_order_proved=ack != "unknown",
                response_effect="publication" if ack == "acknowledged" else None)
            value["R9"]["extinction_eligible"] = True
            with self.assertRaises(c.Invalid):
                check(value)

    def test_cannot_borrow_invocation_or_change_crash_to_corruption(self):
        for change in (lambda x: x["R2"].update(invocation_sha256=NEW), lambda x: x["R4"].update(declared_edit=True),
                       lambda x: x["R1"].update(tested_invocation_sha256=None), lambda x: x.pop("R2")):
            value = payloads()
            change(value)
            with self.assertRaises(c.Invalid):
                check(value)

    def test_refusal_cannot_supply_state_durability_or_boot(self):
        value = payloads()
        value["R1"]["outcomes"] = ["fatal_aof_damage"]
        value["R4"].update(loader="fatal_aof_damage", error="truncated_resp")
        value["R5"].update(post_state="unavailable", oracle_sha256=None, current_boot="unavailable", new_boot_approved=False, boot_receipt_sha256=None)
        value["R3"]["effects"][0]["recovery"] = "unavailable"
        for name in ("atomicity", "durability", "boot"):
            value["R9"]["assertions"][name] = "unproven"
        self.assertFalse(check(value)["m4_accepted"])
        for name in ("atomicity", "durability", "boot"):
            changed = copy.deepcopy(value)
            changed["R9"]["assertions"][name] = "pass"
            with self.assertRaises(c.Invalid):
                check(changed)
        changed = copy.deepcopy(value)
        changed["R3"]["effects"][0]["recovery"] = "verified"
        with self.assertRaises(c.Invalid):
            check(changed)

    def test_generic_error_is_not_damage_and_old_process_is_not_restart(self):
        for changes in ({"loader": "fatal_aof_damage", "error": "other"}, {"error": "truncated_resp"},
                        {"after_process_sha256": OLD}, {"source_line_start": True}, {"loader": "timeout"}):
            value = payloads()
            value["R4"].update(changes)
            with self.assertRaises(c.Invalid):
                check(value)

    def test_prior_effects_cannot_disappear_or_get_unverified_durability_credit(self):
        for change in (lambda x: x["R3"]["effects"].clear(), lambda x: x["R3"]["effects"][0].update(recovery="not_tested_in_this_scope"),
                       lambda x: x["R3"]["effects"][0].update(effect="first_backpressure"),
                       lambda x: x["R3"]["effects"].append(copy.deepcopy(x["R3"]["effects"][0]))):
            value = payloads()
            change(value)
            with self.assertRaises(c.Invalid):
                check(value)

    def test_empty_acl_unsynced_acl_and_surviving_writer_fail(self):
        seal = {"prior_acl_sha256": D, "deny_all_sha256": c.sha(d02.DENY_ALL_ACL), "file_identity_sha256": D,
            "regular_single_link": True, "no_follow": True, "owner_matches": True, "atomic_replace": True,
            "file_synced": True, "directory_synced": True, "readback_matches": True, "writer_absent": True, "no_regrant": True}
        d02.payload("R7", seal, {D})
        for key in set(seal) - {"prior_acl_sha256", "deny_all_sha256", "file_identity_sha256"}:
            with self.assertRaises(c.Invalid):
                d02.payload("R7", dict(seal, **{key: False}), {D})
        with self.assertRaises(c.Invalid):
            d02.payload("R7", dict(seal, deny_all_sha256=c.sha(b"")), {D})

    def test_missing_role_or_transport_error_is_not_online_retirement(self):
        for key in ("all_sessions_terminated", "all_reconnects_wrongpass", "default_denied", "revoker_last", "observed"):
            value = payloads()
            value["R6"][key] = False
            with self.assertRaises(c.Invalid):
                check(value)

    def test_unsafe_deletion_inspection_failure_or_omitted_resource_fails(self):
        for key in ("owned", "stopped", "inspection_succeeded"):
            value = payloads()
            value["R8"]["resources"][0][key] = False
            with self.assertRaises(c.Invalid):
                check(value)
        value = payloads()
        value["R8"]["resources"][0]["foreign_attachment"] = True
        with self.assertRaises(c.Invalid):
            check(value)
        value = payloads()
        value["R8"]["resources"].pop()
        with self.assertRaises(c.Invalid):
            check(value)

    def test_blocked_resource_does_not_prevent_other_safe_cleanup_but_cannot_pass(self):
        value = payloads()
        affected = value["R8"]["resources"][0]
        affected.update(owned=False, removed=False, absent=False)
        value["R8"]["unresolved"] = [affected["identity_sha256"]]
        value["R9"]["assertions"]["disposal"] = "unproven"
        self.assertFalse(check(value)["m4_accepted"])
        value["R9"]["assertions"]["disposal"] = "pass"
        with self.assertRaises(c.Invalid):
            check(value)

    def test_required_assertion_and_authority_flags_cannot_be_relabeled(self):
        value = payloads()
        value["R9"]["assertions"]["durability"] = "not_applicable"
        with self.assertRaises(c.Invalid):
            check(value)
        for key in ("execution_authorized", "implementation_proven", "m4_accepted"):
            value = payloads()
            value["R9"][key] = True
            with self.assertRaises(c.Invalid):
                check(value)

    def test_raw_envelope_must_be_revalidated_and_match_independent_context(self):
        raw, contexts = packet(payloads())
        for kind in raw:
            for key in ("version", "issuer", "sequence", "bindings", "extra"):
                changed = c.decode(raw[kind])
                changed[key] = True
                bad = dict(raw, **{kind: c.canonical(changed)})
                with self.subTest(kind=kind, key=key), self.assertRaises(c.Invalid):
                    d02.reconcile(bad, contexts, RESOURCES)
        changed = c.decode(raw["R2"])
        changed["evidence_refs"] = [NEW]
        with self.assertRaises(c.Invalid):
            d02.reconcile(dict(raw, R2=c.canonical(changed)), contexts, RESOURCES)

    def test_cross_server_ack_and_retirement_cannot_be_borrowed(self):
        for change in (lambda x: x["R2"].update(redis_process_sha256=NEW),
                       lambda x: x["R6"].update(server_process_sha256=OLD),
                       lambda x: x["R4"].update(ordinary_proof_available=False)):
            value = payloads()
            change(value)
            with self.assertRaises(c.Invalid):
                check(value)

    def test_included_chain_is_rebuilt_from_actual_bytes(self):
        raw, contexts = packet(payloads())
        changed = c.decode(raw["R4"])
        changed["payload"]["source_line_end"] += 1
        raw["R4"] = c.canonical(changed)
        # R4 still matches its external predecessor context. R8 must not accept
        # its old predecessor hash after the actual included R4 bytes changed.
        with self.assertRaisesRegex(c.Invalid, "INCLUDED_CHAIN"):
            d02.reconcile(raw, contexts, RESOURCES)
        raw, contexts = packet(payloads())
        changed = c.decode(raw["R1"])
        changed.update(sequence=1, previous_receipt_sha256=D)
        raw["R1"] = c.canonical(changed)
        contexts["R1"].update(sequence=1, previous=D)
        with self.assertRaisesRegex(c.Invalid, "INCLUDED_CHAIN"):
            d02.reconcile(raw, contexts, RESOURCES)

    def test_tested_early_or_late_ack_cannot_hide_behind_a_recovered_probe(self):
        for kind in ("publication", "first_backpressure"):
            for late in (False, True):
                value = payloads()
                value["R1"]["tested_effect"]["kind"] = kind
                value["R2"].update(response_effect=kind, received_before_cut=not late,
                    affirmative_pre_ack=late, classification="known_pre_acknowledgment" if late else "acknowledged", complete_reply_after_cut=late)
                value["R3"]["effects"][0].update(invocation_sha256=OLD, effect="probe")
                value["R1"]["effect_ledger_sha256"] = d02.ledger_identity(value["R3"])
                with self.subTest(kind=kind, late=late), self.assertRaisesRegex(c.Invalid, "TESTED_ACK_EFFECT_MISSING"):
                    check(value)

    def test_no_write_and_replay_ack_dispositions_do_not_invent_mutations(self):
        for kind, original in (("no_write", "probe"), ("publication_replay", "publication"), ("backpressure_replay", "first_backpressure")):
            value = payloads()
            value["R1"]["tested_effect"] = {"kind": kind, "mutation_invocation_sha256": None if kind == "no_write" else OLD,
                "oracle_sha256": None if kind == "no_write" else D}
            value["R2"]["response_effect"] = kind
            value["R3"]["effects"][0].update(invocation_sha256=OLD, effect=original)
            value["R1"]["effect_ledger_sha256"] = d02.ledger_identity(value["R3"])
            self.assertFalse(check(value)["m4_accepted"])
        value = payloads()
        value["R1"]["tested_effect"]["kind"] = "first_backpressure"
        value["R2"]["response_effect"] = "first_backpressure"
        value["R3"]["effects"][0]["effect"] = "first_backpressure"
        value["R1"]["effect_ledger_sha256"] = d02.ledger_identity(value["R3"])
        self.assertTrue(check(value)["structural_and_semantic_contract_valid"])

    def test_registered_oracle_digest_cannot_substitute_for_boot_or_another_process(self):
        value = payloads()
        value["R5"]["boot_receipt_sha256"] = D
        with self.assertRaisesRegex(c.Invalid, "BOOT_PROOF_KIND"):
            check(value)
        for ref, reason in ((BOOT, "BOOT_PROCESS_BINDING"), (D, "STATE_PROCESS_BINDING")):
            raw, contexts = packet(payloads())
            contexts["R5"]["registered_refs"][ref]["redis_process_sha256"] = OLD
            with self.assertRaisesRegex(c.Invalid, reason):
                d02.reconcile(raw, contexts, RESOURCES)

    def test_prefault_deny_all_seal_cannot_gain_post_restart_state_or_boot(self):
        value = payloads()
        value["R1"].update(fault="deliberate_corruption_admission", tested_invocation_sha256=None, tested_effect=None)
        value.pop("R2")
        value["R3"]["effects"][0].update(invocation_sha256=OLD, effect="probe", recovery="unavailable")
        value["R1"]["effect_ledger_sha256"] = d02.ledger_identity(value["R3"])
        value["R4"].update(declared_edit=True, ordinary_proof_available=False)
        value["R6"].update(prefault=True, server_process_sha256=OLD)
        value["R7"] = {"prior_acl_sha256": D, "deny_all_sha256": c.sha(d02.DENY_ALL_ACL), "file_identity_sha256": D,
            "regular_single_link": True, "no_follow": True, "owner_matches": True, "atomic_replace": True,
            "file_synced": True, "directory_synced": True, "readback_matches": True, "writer_absent": True, "no_regrant": True}
        for assertion in ("atomicity", "durability", "boot"):
            value["R9"]["assertions"][assertion] = "unproven"
        with self.assertRaisesRegex(c.Invalid, "NO_REGRANT"):
            check(value)
        value["R5"].update(post_state="unavailable", oracle_sha256=None, current_boot="unavailable", new_boot_approved=False, boot_receipt_sha256=None)
        self.assertFalse(check(value)["m4_accepted"])
        value["R5"].update(post_state="verified", oracle_sha256=D)
        with self.assertRaisesRegex(c.Invalid, "NO_REGRANT"):
            check(value)

    def test_fault_scoped_credit_requires_stop_proof(self):
        value = payloads()
        value["R4"]["stop_proved"] = False
        with self.assertRaisesRegex(c.Invalid, "FAULT_NOT_PROVED"):
            check(value)
        for assertion in ("atomicity", "durability", "boot"):
            value["R9"]["assertions"][assertion] = "unproven"
        value["R3"]["effects"][0]["recovery"] = "unavailable"
        self.assertFalse(check(value)["m4_accepted"])
        value["R1"]["outcomes"] = ["fatal_aof_damage"]
        value["R4"].update(loader="fatal_aof_damage", error="truncated_resp")
        value["R5"].update(post_state="unavailable", oracle_sha256=None, current_boot="unavailable", new_boot_approved=False, boot_receipt_sha256=None)
        value["R9"]["assertions"]["startup_refusal"] = "pass"
        with self.assertRaisesRegex(c.Invalid, "FAULT_NOT_PROVED"):
            check(value)

    def test_no_write_or_replay_cannot_also_create_a_current_mutation(self):
        for kind, original in (("no_write", "probe"), ("publication_replay", "publication"), ("backpressure_replay", "first_backpressure")):
            for late in (False, True):
                value = payloads()
                value["R1"]["tested_effect"] = {"kind": kind, "mutation_invocation_sha256": None if kind == "no_write" else OLD,
                    "oracle_sha256": None if kind == "no_write" else D}
                value["R2"].update(response_effect=kind, received_before_cut=not late, complete_reply_after_cut=late,
                    affirmative_pre_ack=late, classification="known_pre_acknowledgment" if late else "acknowledged")
                value["R3"]["effects"][0].update(invocation_sha256=OLD, effect=original)
                value["R1"]["effect_ledger_sha256"] = d02.ledger_identity(value["R3"])
                self.assertTrue(check(value)["structural_and_semantic_contract_valid"])
                value["R3"]["effects"].append({"invocation_sha256": D, "effect": "publication", "oracle_sha256": D,
                    "recovery": "verified", "evidence_refs": [D]})
                value["R1"]["effect_ledger_sha256"] = d02.ledger_identity(value["R3"])
                raw, contexts = packet(value)
                for receipt, body in raw.items():
                    d02.validate(body, **contexts[receipt])
                with self.subTest(kind=kind, late=late), self.assertRaisesRegex(c.Invalid, "NONMUTATING_INVOCATION_EFFECT"):
                    d02.reconcile(raw, contexts, RESOURCES)

    def test_replay_original_is_a_distinct_physical_invocation(self):
        for kind, effect in (("publication_replay", "publication"), ("backpressure_replay", "first_backpressure")):
            for late in (False, True):
                value = payloads()
                value["R1"]["tested_effect"]["kind"] = kind
                value["R2"].update(response_effect=kind, received_before_cut=not late, complete_reply_after_cut=late,
                    affirmative_pre_ack=late, classification="known_pre_acknowledgment" if late else "acknowledged")
                value["R3"]["effects"][0]["effect"] = effect
                value["R1"]["effect_ledger_sha256"] = d02.ledger_identity(value["R3"])
                with self.subTest(kind=kind, late=late), self.assertRaisesRegex(c.Invalid, "REPLAY_IDENTITY"):
                    check(value)


if __name__ == "__main__":
    unittest.main()
