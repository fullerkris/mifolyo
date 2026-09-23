"""Finite M4-P3 case vocabulary. No inputs, imports, transport or setup override."""

BOOT = "bootstrap-rejections-v1"
WIRE = "ledger-wire-negatives-v1"
INSTALL = "CJ2_INSTALL_CANDIDATE_MARKERS"
RETIRE = "CJ2_RETIRE_LEGACY_KEYS"
PROMOTE = "CJ2_PROMOTE_CANDIDATE_CONTRACTS"
STORED = {
    "ledger-candidate-compat-present-v1": ("P01", "INVALID_STATE"),
    "ledger-candidate-contract-present-v1": ("P02", "INVALID_STATE"),
    "ledger-admin-freeze-present-v1": ("P03", "INVALID_STATE"),
    "ledger-active-compat-missing-v1": ("S01", "COMPATIBILITY_MISMATCH"),
    "ledger-active-contract-wrong-type-v1": ("S02", "WRONG_TYPE"),
    "ledger-active-contract-mismatch-v1": ("S03", "CONTRACT_MISMATCH"),
    "ledger-guard-mismatch-v1": ("S04", "IMMUTABLE_MISMATCH"),
    "ledger-active-compat-extra-field-v1": ("S05", "INVALID_STATE"),
}
ADMIN = {
    "ledger-install-denied-v1": ("A01", INSTALL, "CRAWL_V2_BOOT_UNAPPROVED", "release_admin", "CANDIDATE_INSTALLED"),
    "ledger-retire-denied-v1": ("A02", RETIRE, "NOPERM", "migration_admin", "LEGACY_RETIRED"),
    "ledger-promote-denied-v1": ("A03", PROMOTE, "NOPERM", "release_admin", "CONTRACTS_PROMOTED"),
}
CASES = {name: name.removesuffix("-v1") for name in (*STORED, WIRE, BOOT, *ADMIN)}
LEDGER_SCENARIOS = tuple(CASES[name] for name in (*STORED, WIRE))
WIRE_CODES = ("INVALID_ARGUMENT", "BOOT_UNAPPROVED", "INVALID_ARGUMENT", "COMPATIBILITY_MISMATCH",
              "INVALID_ARGUMENT", "INVALID_ARGUMENT", "INVALID_ARGUMENT", "INVALID_ARGUMENT",
              "CONTRACT_MISMATCH", "INVALID_ARGUMENT", "INVALID_ARGUMENT", "INVALID_ARGUMENT")
BOOT_CODES = ("INVALID_ARGUMENT", "INVALID_IDENTIFIER", "INVALID_ARGUMENT", "BOOT_UNAPPROVED",
              "BOOT_UNAPPROVED", "BOOT_UNAPPROVED", "INVALID_IDENTIFIER", "INVALID_ARGUMENT", "INVALID_ARGUMENT")
DOWNSTREAM = ("pages_queue", "pages_queue:processing", "pages_queue:dead", "image_indexer_queue",
              "image_indexer_queue:processing", "image_indexer_queue:dead", "pages_queue:indexer_owner", "image_indexer_queue:owner")
BOOT_FIELDS = ("schema_version", "boot_state", "approved_redis_run_id", "boot_epoch", "approved_at_ms",
               "planned_shutdown_nonce", "planned_shutdown_evidence_sha256", "last_approval_mode",
               "consumed_planned_shutdown_nonce", "rehearsal_evidence_sha256", "rehearsal_at_ms", "acknowledged_loss_bound")


def source_operations(case_id):
    if case_id == BOOT:
        return ("CJ2_APPROVE_BOOT",)
    if case_id in ADMIN:
        target = ADMIN[case_id][1]
        return ("CJ2_APPROVE_BOOT", INSTALL, *(() if target == INSTALL else (RETIRE,)),
                *((PROMOTE,) if target == PROMOTE else ()))
    return ("CJ2_APPROVE_BOOT", "CJ2_TRY_CLAIM", *(("CJ2_RELEASE_BEFORE_IO",) if case_id == WIRE else ()))


def extra_roles(case_id):
    if case_id not in ADMIN:
        return ()
    return ("release_admin",) if ADMIN[case_id][1] == INSTALL else ("release_admin", "migration_admin")


def measurement_sequence(case_id):
    """(assertion, actor, expected code/status), including each explicit replay."""
    if case_id in STORED:
        assertion, code = STORED[case_id]
        return [(assertion, "ledger", "CRAWL_V2_" + code)] * 2
    if case_id == WIRE:
        return [(f"W{i:02}", "ledger", "CRAWL_V2_" + code) for i, code in enumerate(WIRE_CODES, 1) for _ in range(2)] + [
            ("WP01", "ledger", "CLAIMED"), ("WP01", "ledger", "RELEASED_READY")]
    if case_id == BOOT:
        return [(f"B{i:02}", "boot", "CRAWL_V2_" + code) for i, code in enumerate(BOOT_CODES, 1) for _ in range(2)] + [
            ("BP01", "boot", "OK"), ("BP01", "boot", "EXISTS_IDENTICAL")]
    assertion, _, code, actor, status = ADMIN[case_id]
    return [(assertion, "ledger", code)] * 2 + [(assertion, actor, status)]
