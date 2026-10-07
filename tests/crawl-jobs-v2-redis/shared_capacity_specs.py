"""Closed two-run capacity/cancellation cases; reversed order is offline only."""
CASE = "ledger-shared-group-capacity-v1"
SCENARIO = "ledger-shared-group-capacity"
CANCEL_CASE = "ledger-shared-group-cancellation-v1"
CANCEL_SCENARIO = "ledger-shared-group-cancellation"
CASES = {CASE: SCENARIO, CANCEL_CASE: CANCEL_SCENARIO}
CLAIM, START, FINISH = "CJ2_TRY_CLAIM", "CJ2_START_REQUEST", "CJ2_FINISH_REQUEST"
CANCEL, MAINTAIN = "CJ2_CANCEL_RESERVATION", "CJ2_MAINTAIN_RATE_SCOPES"
SOURCES = ("CJ2_APPROVE_BOOT", CLAIM, START, FINISH, CANCEL, MAINTAIN)
GROUP = "fixture"
POSITIONS = 90
# Operation, actor, expected status/error, deliberately mismatched identity part.
FINISH_STEPS = (
    (CLAIM, "a", "CLAIMED", ""),
    (CLAIM, "a", "ALREADY_CLAIMED", ""),
    (CLAIM, "b", "CAPACITY_BLOCKED", ""),
    (CLAIM, "b", "CAPACITY_BLOCKED", ""),
    (CANCEL, "a", "CRAWL_V2_IMMUTABLE_MISMATCH", "owner"),
    (START, "a", "STARTED", ""),
    (START, "a", "ALREADY_STARTED", ""),
    (CLAIM, "b", "CAPACITY_BLOCKED", ""),
    (CANCEL, "a", "CRAWL_V2_INVALID_STATE", ""),
    (FINISH, "a", "CRAWL_V2_IMMUTABLE_MISMATCH", "token"),
    (FINISH, "a", "FINISHED", ""),
    (CLAIM, "b", "CLAIMED", ""),
    (CLAIM, "b", "ALREADY_CLAIMED", ""),
    (FINISH, "a", "ALREADY_FINISHED", ""),
    (START, "a", "ALREADY_STARTED", ""),
    (START, "b", "STARTED", ""),
    (START, "b", "ALREADY_STARTED", ""),
    (FINISH, "b", "FINISHED", ""),
    (FINISH, "b", "ALREADY_FINISHED", ""),
    (START, "b", "ALREADY_STARTED", ""),
    (MAINTAIN, "a", "BATCH_DONE", ""),
)
# Cancellation replay deliberately returns the same canonical status; mutation
# classification is separate so a replay cannot refund the next actor's slot.
CANCEL_STEPS = (
    (CLAIM, "a", "CLAIMED", ""),
    (CLAIM, "b", "CAPACITY_BLOCKED", ""),
    (CANCEL, "a", "RESERVATION_CANCELLED", ""),
    (CANCEL, "a", "RESERVATION_CANCELLED", ""),
    (CLAIM, "b", "CLAIMED", ""),
    (CANCEL, "a", "RESERVATION_CANCELLED", ""),
    (START, "b", "STARTED", ""),
    (FINISH, "b", "FINISHED", ""),
    (MAINTAIN, "a", "BATCH_DONE", ""),
)
SEQUENCES = {"finish": FINISH_STEPS, "cancel": CANCEL_STEPS,
    "finish-reversed": tuple((op, "b" if actor == "a" else "a", status, fault) for op, actor, status, fault in FINISH_STEPS)}
MUTATIONS = {"finish": frozenset((0, 5, 10, 11, 15, 17)), "finish-reversed": frozenset((0, 5, 10, 11, 15, 17)),
    "cancel": frozenset((0, 2, 4, 6, 7))}
# Existing finish-case aliases remain fixed; runtime selects only by closed case.
STEPS = FINISH_STEPS
ERRORS = frozenset((4, 8, 9))
RUNTIME_TRACES = {CASE: "finish", CANCEL_CASE: "cancel"}
ASSERTION_PREFIXES = {CASE: "SGC", CANCEL_CASE: "SGCANCEL"}
