"""Offline D02 receipt contracts. Never grants current or proposed execution authority."""
from contracts import Invalid, boolean, canonical, decode, digest, exact, integer, require, sha
import re

DOMAIN = "mifolyo:crawl:v2:m4-fault-receipt"
MAX_RECEIPT = 65536
MAX_REFS = 128
BINDINGS = frozenset(("case_id", "fixture_id", "plan_sha256", "approval_sha256", "recipe_sha256", "contract_sha256",
    "source_set_sha256", "redis_image", "harness_image", "redis_config_sha256"))
CASES = frozenset(("D02_PROCESS_COMMIT", "D02_CORRUPTION_ADMISSION"))
ROLES = {"R1": "controller", "R2": "client", "R3": "controller", "R4": "controller", "R5": "observer",
    "R6": "revoker", "R7": "acl_writer", "R8": "controller", "R9": "controller"}
ASSERTIONS = frozenset(("startup_refusal", "atomicity", "durability", "boot", "online_revocation", "extinction", "disposal"))
ACKS = frozenset(("known_pre_acknowledgment", "acknowledged", "unknown"))
TESTED_EFFECTS = frozenset(("publication", "first_backpressure", "publication_replay", "backpressure_replay", "no_write"))
DENY_ALL_ACL = b"user default off resetpass resetkeys resetchannels clearselectors -@all\n"


def refs(value, registered):
    require(type(value) is list and len(value) <= MAX_REFS, "REFERENCES")
    for ref in value:
        digest(ref)
        require(ref in registered, "UNREGISTERED_REFERENCE")
    require(len(value) == len(set(value)), "DUPLICATE_REFERENCE")


def ack(value):
    exact(value, ("invocation_sha256", "session_sha256", "request_sha256", "response_schema_sha256", "redis_process_sha256",
        "attempt", "connection_generation", "received_before_cut", "affirmative_pre_ack", "cut_order_proved",
        "closure_proved", "complete_reply_after_cut", "target_stopped", "client_retired", "classification", "response_effect"))
    for key in ("invocation_sha256", "session_sha256", "request_sha256", "response_schema_sha256", "redis_process_sha256"):
        digest(value[key])
    integer(value["attempt"], 1, 1)
    integer(value["connection_generation"], 1, 1)
    for key in ("received_before_cut", "affirmative_pre_ack", "cut_order_proved", "closure_proved", "complete_reply_after_cut", "target_stopped", "client_retired"):
        boolean(value[key])
    if value["received_before_cut"] or value["complete_reply_after_cut"]:
        require(type(value["response_effect"]) is str and value["response_effect"] in TESTED_EFFECTS, "RESPONSE_EFFECT")
    else:
        require(value["response_effect"] is None, "RESPONSE_EFFECT")
    classification = "unknown"
    if value["cut_order_proved"] and not (value["received_before_cut"] and value["affirmative_pre_ack"]):
        if value["received_before_cut"]:
            classification = "acknowledged"
        elif value["affirmative_pre_ack"]:
            classification = "known_pre_acknowledgment"
    require(value["classification"] == classification, "ACK_CLASSIFICATION")
    return {"classification_at_cut": classification, "closed_no_reply_window": classification == "known_pre_acknowledgment"
        and value["closure_proved"] and value["target_stopped"] and value["client_retired"] and not value["complete_reply_after_cut"]}


def payload(kind, value, registered):
    if kind == "R1":
        exact(value, ("fault", "tested_invocation_sha256", "tested_effect", "boundary_manifest_sha256", "effect_ledger_sha256", "inventory_sha256", "assertions", "outcomes", "case_ms", "stage_ms", "cleanup_ms"))
        require(value["fault"] in ("unmodified_aof_commit_process_crash", "deliberate_corruption_admission"), "FAULT")
        if value["fault"] == "deliberate_corruption_admission":
            require(value["tested_invocation_sha256"] is None and value["tested_effect"] is None, "INVOCATION")
        else:
            digest(value["tested_invocation_sha256"])
            effect = value["tested_effect"]
            exact(effect, ("kind", "mutation_invocation_sha256", "oracle_sha256"))
            require(type(effect["kind"]) is str and effect["kind"] in TESTED_EFFECTS, "TESTED_EFFECT")
            if effect["kind"] == "no_write":
                require(effect["mutation_invocation_sha256"] is None and effect["oracle_sha256"] is None, "NO_WRITE_EFFECT")
            else:
                digest(effect["mutation_invocation_sha256"])
                digest(effect["oracle_sha256"])
                if effect["kind"] in ("publication", "first_backpressure"):
                    require(effect["mutation_invocation_sha256"] == value["tested_invocation_sha256"], "MUTATION_IDENTITY")
                else:
                    require(effect["mutation_invocation_sha256"] != value["tested_invocation_sha256"], "REPLAY_IDENTITY")
        for key in ("boundary_manifest_sha256", "effect_ledger_sha256", "inventory_sha256"):
            digest(value[key])
        require(type(value["assertions"]) is list and 0 < len(value["assertions"]) <= len(ASSERTIONS), "ASSERTIONS")
        require(all(type(item) is str and item in ASSERTIONS for item in value["assertions"]) and len(set(value["assertions"])) == len(value["assertions"]), "ASSERTIONS")
        require(value["outcomes"] in (["loaded"], ["fatal_aof_damage"], ["loaded", "fatal_aof_damage"]), "OUTCOMES")
        for key, high in (("case_ms", 300000), ("stage_ms", 30000), ("cleanup_ms", 60000)):
            integer(value[key], 1, high)
    elif kind == "R2":
        ack(value)
    elif kind == "R3":
        exact(value, ("effects",))
        require(type(value["effects"]) is list and len(value["effects"]) <= 128, "EFFECTS")
        seen = set()
        for row in value["effects"]:
            exact(row, ("invocation_sha256", "effect", "oracle_sha256", "recovery", "evidence_refs"))
            digest(row["invocation_sha256"])
            digest(row["oracle_sha256"])
            require(row["invocation_sha256"] not in seen, "DUPLICATE_EFFECT")
            seen.add(row["invocation_sha256"])
            require(row["effect"] in ("publication", "first_backpressure", "probe", "probe_deletion", "boot", "setup"), "EFFECT")
            require(row["recovery"] in ("verified", "failed", "unavailable", "not_tested_in_this_scope"), "RECOVERY")
            refs(row["evidence_refs"], registered)
    elif kind == "R4":
        exact(value, ("before_process_sha256", "after_process_sha256", "volume_identity_sha256", "aof_before_sha256", "aof_after_sha256",
            "declared_edit", "loader", "error", "source_sha256", "source_line_start", "source_line_end", "diagnostic_sha256", "stop_proved", "ordinary_proof_available"))
        for key in ("before_process_sha256", "after_process_sha256", "volume_identity_sha256", "aof_before_sha256", "aof_after_sha256", "source_sha256", "diagnostic_sha256"):
            digest(value[key])
        for key in ("declared_edit", "stop_proved", "ordinary_proof_available"):
            boolean(value[key])
        require(value["before_process_sha256"] != value["after_process_sha256"], "RESTART_IDENTITY")
        require(value["loader"] in ("loaded", "fatal_aof_damage", "other_failure"), "LOADER")
        require(value["error"] in ("none", "truncated_resp", "unfinished_multi", "invalid_manifest", "invalid_rdb", "other"), "ERROR_CLASS")
        require((value["loader"] == "fatal_aof_damage") == (value["error"] in ("truncated_resp", "unfinished_multi", "invalid_manifest", "invalid_rdb")), "ATTRIBUTION")
        require(value["loader"] != "loaded" or value["error"] == "none", "ATTRIBUTION")
        require(value["loader"] != "other_failure" or value["error"] == "other", "ATTRIBUTION")
        integer(value["source_line_start"], 1, 100000)
        integer(value["source_line_end"], value["source_line_start"], 100000)
    elif kind == "R5":
        exact(value, ("post_state", "oracle_sha256", "current_boot", "new_boot_approved", "boot_receipt_sha256"))
        require(value["post_state"] in ("verified", "failed", "unavailable"), "POST_STATE")
        require(value["current_boot"] in ("unapproved", "approved", "unavailable"), "BOOT")
        boolean(value["new_boot_approved"])
        if value["new_boot_approved"]:
            digest(value["boot_receipt_sha256"])
            require(value["boot_receipt_sha256"] in registered and value["current_boot"] == "approved", "BOOT_PROOF")
            require(registered[value["boot_receipt_sha256"]]["kind"] == "canonical_boot_approval", "BOOT_PROOF_KIND")
        else:
            require(value["boot_receipt_sha256"] is None and value["current_boot"] != "approved", "BOOT_PROOF")
        if value["post_state"] == "unavailable":
            require(value["oracle_sha256"] is None and value["current_boot"] == "unavailable" and value["new_boot_approved"] is False, "UNAVAILABLE_STATE")
        else:
            digest(value["oracle_sha256"])
            require(value["oracle_sha256"] in registered and registered[value["oracle_sha256"]]["kind"] == "state_oracle", "STATE_PROOF_KIND")
            require(not value["new_boot_approved"] or value["current_boot"] == "approved", "BOOT")
    elif kind == "R6":
        exact(value, ("observed", "prefault", "role_inventory_sha256", "all_sessions_terminated", "all_reconnects_wrongpass", "server_process_sha256", "default_denied", "revoker_last"))
        digest(value["role_inventory_sha256"])
        digest(value["server_process_sha256"])
        for key in ("observed", "prefault", "all_sessions_terminated", "all_reconnects_wrongpass", "default_denied", "revoker_last"):
            boolean(value[key])
        require(not value["observed"] or all(value[key] for key in ("all_sessions_terminated", "all_reconnects_wrongpass", "default_denied", "revoker_last")), "RETIREMENT")
    elif kind == "R7":
        exact(value, ("prior_acl_sha256", "deny_all_sha256", "file_identity_sha256", "regular_single_link", "no_follow", "owner_matches", "atomic_replace", "file_synced", "directory_synced", "readback_matches", "writer_absent", "no_regrant"))
        for key in ("prior_acl_sha256", "deny_all_sha256", "file_identity_sha256"):
            digest(value[key])
        require(value["deny_all_sha256"] == sha(DENY_ALL_ACL), "DENY_ALL")
        require(all(type(value[key]) is bool and value[key] is True for key in set(value) - {"prior_acl_sha256", "deny_all_sha256", "file_identity_sha256"}), "ACL_SEAL")
    elif kind == "R8":
        exact(value, ("inventory_sha256", "resources", "unresolved"))
        digest(value["inventory_sha256"])
        require(type(value["resources"]) is list and 0 < len(value["resources"]) <= 32, "RESOURCES")
        names = set()
        for row in value["resources"]:
            exact(row, ("identity_sha256", "owned", "foreign_attachment", "stopped", "removed", "absent", "inspection_succeeded"))
            digest(row["identity_sha256"])
            require(row["identity_sha256"] not in names, "DUPLICATE_RESOURCE")
            names.add(row["identity_sha256"])
            for key in set(row) - {"identity_sha256"}:
                boolean(row[key])
            require(not row["removed"] or (row["owned"] and not row["foreign_attachment"] and row["stopped"]), "UNSAFE_REMOVAL")
            require(not row["absent"] or (row["removed"] and row["inspection_succeeded"]), "ABSENCE")
        refs(value["unresolved"], names)
        require(set(value["unresolved"]) == {row["identity_sha256"] for row in value["resources"] if not row["absent"]}, "UNRESOLVED")
    elif kind == "R9":
        exact(value, ("assertions", "execution_authorized", "implementation_proven", "m4_accepted", "extinction_eligible"))
        require(all(value[key] is False for key in ("execution_authorized", "implementation_proven", "m4_accepted", "extinction_eligible")), "AUTHORITY")
        exact(value["assertions"], ASSERTIONS)
        require(all(type(result) is str and result in ("pass", "fail", "unproven", "not_applicable") for result in value["assertions"].values()), "ASSERTION_RESULT")
    else:
        raise Invalid("KIND")


def validate(raw, *, bindings, issuer, sequence, previous, registered_refs):
    value = decode(raw, MAX_RECEIPT)
    exact(value, ("domain", "version", "kind", "bindings", "issuer", "sequence", "previous_receipt_sha256", "payload", "evidence_refs"))
    require(value["domain"] == DOMAIN, "DOMAIN")
    integer(value["version"], 1, 1)
    exact(bindings, BINDINGS)
    require(type(bindings["case_id"]) is str and bindings["case_id"] in CASES, "CASE")
    require(type(bindings["fixture_id"]) is str and re.fullmatch(r"[0-9a-f]{32}", bindings["fixture_id"]) is not None and bindings["fixture_id"] != "0" * 32, "FIXTURE")
    for key in BINDINGS - {"case_id", "fixture_id", "redis_image", "harness_image"}:
        digest(bindings[key])
    for key in ("redis_image", "harness_image"):
        require(type(bindings[key]) is str and bindings[key].startswith("sha256:"), "IMAGE")
        digest(bindings[key][7:])
    require(canonical(value["bindings"]) == canonical(bindings), "BINDING")
    exact(issuer, ("role", "image", "process_identity_sha256"))
    require(type(value["kind"]) is str and value["kind"] in ROLES and issuer["role"] == ROLES[value["kind"]], "ISSUER_ROLE")
    require(type(issuer["image"]) is str and issuer["image"].startswith("sha256:"), "IMAGE")
    digest(issuer["image"][7:])
    digest(issuer["process_identity_sha256"])
    require(canonical(value["issuer"]) == canonical(issuer), "ISSUER")
    integer(sequence, 0, 1023)
    integer(value["sequence"], sequence, sequence)
    if sequence == 0:
        require(previous is None and value["previous_receipt_sha256"] is None, "CHAIN")
    else:
        digest(previous)
        require(value["previous_receipt_sha256"] == previous, "CHAIN")
    require(type(registered_refs) is dict and len(registered_refs) <= MAX_REFS, "REFERENCE_REGISTRY")
    for ref, metadata in registered_refs.items():
        digest(ref)
        exact(metadata, ("kind", "bindings_sha256", "redis_process_sha256"))
        require(type(metadata["kind"]) is str and metadata["kind"] in ("evidence", "state_oracle", "canonical_boot_approval"), "REFERENCE_KIND")
        require(metadata["bindings_sha256"] == sha(canonical(bindings)), "REFERENCE_BINDING")
        digest(metadata["redis_process_sha256"])
    refs(value["evidence_refs"], registered_refs)
    payload(value["kind"], value["payload"], registered_refs)
    if value["kind"] == "R5" and value["payload"]["new_boot_approved"]:
        require(value["payload"]["boot_receipt_sha256"] in value["evidence_refs"], "BOOT_REFERENCE")
    return value


def ledger_identity(value):
    """Intent binds predeclared effects, not future recovery results or receipt bytes."""
    return sha(canonical({"effects": [{key: row[key] for key in ("invocation_sha256", "effect", "oracle_sha256")}
        for row in value["effects"]]}))


def reconcile(raw_receipts, contexts, expected_resources):
    """Revalidate every envelope against independent caller context before closure."""
    require(type(raw_receipts) is dict and type(contexts) is dict and set(raw_receipts) == set(contexts), "CONTEXTS")
    receipts = {kind: validate(raw, **contexts[kind]) for kind, raw in raw_receipts.items()}
    # This closed preparation packet includes every receipt of each issuer from
    # sequence zero. External anchors/gaps need a separately reviewed variant.
    issuers, chains = {}, {}
    for kind, row in receipts.items():
        role = row["issuer"]["role"]
        encoded_issuer = canonical(row["issuer"])
        require(role not in issuers or issuers[role] == encoded_issuer, "ISSUER_CHANGED")
        issuers[role] = encoded_issuer
        chains.setdefault(role, []).append((row["sequence"], row["previous_receipt_sha256"], sha(raw_receipts[kind])))
    for chain in chains.values():
        previous = None
        for expected_sequence, (sequence, prior, current) in enumerate(sorted(chain)):
            require(sequence == expected_sequence and prior == previous, "INCLUDED_CHAIN")
            previous = current
    require(type(expected_resources) is set and 0 < len(expected_resources) <= 32, "RESOURCE_REGISTRY")
    for resource in expected_resources:
        digest(resource)
    exact(receipts, {"R1", "R3", "R4", "R5", "R6", "R8", "R9"} | ({"R2"} if "R2" in receipts else set()) | ({"R7"} if "R7" in receipts else set()))
    intent = receipts["R1"]["payload"]
    binding = receipts["R1"]["bindings"]
    for kind, row in receipts.items():
        require(row["kind"] == kind and canonical(row["bindings"]) == canonical(binding), "CROSS_BINDING")
    crash = intent["fault"] == "unmodified_aof_commit_process_crash"
    require(crash == ("R2" in receipts), "INVOCATION_RECEIPT")
    require(binding["case_id"] == ("D02_PROCESS_COMMIT" if crash else "D02_CORRUPTION_ADMISSION"), "CASE_FAULT")
    if crash:
        require(not receipts["R4"]["payload"]["declared_edit"], "CRASH_EDIT")
        require(receipts["R2"]["payload"]["invocation_sha256"] == intent["tested_invocation_sha256"], "INVOCATION_BINDING")
        require(receipts["R2"]["payload"]["redis_process_sha256"] == receipts["R4"]["payload"]["before_process_sha256"], "ACK_PROCESS_BINDING")
        observed = receipts["R2"]["payload"]
        effect = intent["tested_effect"]
        if effect["kind"] in ("no_write", "publication_replay", "backpressure_replay"):
            require(all(row["invocation_sha256"] != intent["tested_invocation_sha256"] for row in receipts["R3"]["payload"]["effects"]), "NONMUTATING_INVOCATION_EFFECT")
        if observed["received_before_cut"] or observed["complete_reply_after_cut"]:
            require(observed["response_effect"] == effect["kind"], "TESTED_EFFECT_BINDING")
            if effect["kind"] != "no_write":
                expected_effect = {"publication_replay": "publication", "backpressure_replay": "first_backpressure"}.get(effect["kind"], effect["kind"])
                matches = [row for row in receipts["R3"]["payload"]["effects"] if row["invocation_sha256"] == effect["mutation_invocation_sha256"]]
                require(len(matches) == 1 and matches[0]["effect"] == expected_effect and matches[0]["oracle_sha256"] == effect["oracle_sha256"], "TESTED_ACK_EFFECT_MISSING")
    else:
        require(receipts["R4"]["payload"]["declared_edit"], "CORRUPTION_EDIT")
    require(ledger_identity(receipts["R3"]["payload"]) == intent["effect_ledger_sha256"], "LEDGER_BINDING")
    require(receipts["R8"]["payload"]["inventory_sha256"] == intent["inventory_sha256"], "INVENTORY_BINDING")
    require(intent["inventory_sha256"] == sha(canonical({"resources": sorted(expected_resources)})), "INVENTORY_BINDING")
    require({row["identity_sha256"] for row in receipts["R8"]["payload"]["resources"]} == expected_resources, "INVENTORY_COMPLETE")
    if receipts["R6"]["payload"]["prefault"]:
        require(not crash and "R7" in receipts, "PREFAULT_SEAL")
    if "R7" in receipts:
        require(not crash and receipts["R6"]["payload"]["prefault"] and receipts["R6"]["payload"]["observed"], "SEAL_ROUTE")
        state = receipts["R5"]["payload"]
        require(state["post_state"] == state["current_boot"] == "unavailable" and state["new_boot_approved"] is False, "NO_REGRANT")
    retirement = receipts["R6"]["payload"]
    expected_process = receipts["R4"]["payload"]["before_process_sha256" if retirement["prefault"] else "after_process_sha256"]
    require(retirement["server_process_sha256"] == expected_process, "RETIREMENT_PROCESS_BINDING")
    if retirement["observed"] and not retirement["prefault"]:
        require(receipts["R4"]["payload"]["ordinary_proof_available"], "ONLINE_PROOF_CONTRADICTION")
    loader = receipts["R4"]["payload"]["loader"]
    require(loader in intent["outcomes"], "OUTCOME")
    results = receipts["R9"]["payload"]["assertions"]
    if receipts["R5"]["payload"]["new_boot_approved"]:
        proof = contexts["R5"]["registered_refs"][receipts["R5"]["payload"]["boot_receipt_sha256"]]
        require(proof["redis_process_sha256"] == receipts["R4"]["payload"]["after_process_sha256"], "BOOT_PROCESS_BINDING")
    if receipts["R5"]["payload"]["post_state"] != "unavailable":
        proof = contexts["R5"]["registered_refs"][receipts["R5"]["payload"]["oracle_sha256"]]
        require(proof["redis_process_sha256"] == receipts["R4"]["payload"]["after_process_sha256"], "STATE_PROCESS_BINDING")
    if not receipts["R4"]["payload"]["stop_proved"]:
        require(all(results[name] != "pass" for name in ("startup_refusal", "atomicity", "durability", "boot")), "FAULT_NOT_PROVED")
        require(all(row["recovery"] != "verified" for row in receipts["R3"]["payload"]["effects"]), "RECOVERY_WITHOUT_FAULT")
    for assertion in intent["assertions"]:
        require(results[assertion] != "not_applicable", "REQUIRED_ASSERTION")
    if loader != "loaded":
        require(receipts["R5"]["payload"]["post_state"] == "unavailable", "REFUSAL_STATE")
        require(all(results[name] != "pass" for name in ("atomicity", "durability", "boot")), "REFUSAL_CREDIT")
        require(all(row["recovery"] != "verified" for row in receipts["R3"]["payload"]["effects"]), "EFFECT_CREDIT")
    if results["atomicity"] == "pass" or results["durability"] == "pass":
        require(loader == "loaded" and receipts["R5"]["payload"]["post_state"] == "verified", "STATE_CREDIT")
    if results["durability"] == "pass":
        require(bool(receipts["R3"]["payload"]["effects"]) and all(row["recovery"] == "verified" for row in receipts["R3"]["payload"]["effects"]), "DURABILITY_CREDIT")
    if results["startup_refusal"] == "pass":
        require(loader == "fatal_aof_damage", "REFUSAL_CREDIT")
    if results["boot"] == "pass":
        require(loader == "loaded" and receipts["R5"]["payload"]["current_boot"] == "approved", "BOOT_CREDIT")
    if results["disposal"] == "pass":
        require(not receipts["R8"]["payload"]["unresolved"], "CLEANUP_CREDIT")
    if results["online_revocation"] == "pass":
        require(receipts["R6"]["payload"]["observed"], "REVOCATION_CREDIT")
    require(results["extinction"] != "pass", "UNAPPROVED_EXCEPTION")
    return {"structural_and_semantic_contract_valid": True, "execution_authorized": False,
        "implementation_proven": False, "extinction_eligible": False, "m4_accepted": False}
