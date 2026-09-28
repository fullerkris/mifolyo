"""Closed single-lease synthetic request lifecycle; no dispatch or authority."""
CASE = "ledger-request-lifecycle-v1"
SCENARIO = "ledger-request-lifecycle"
CLAIM, RESERVE = "CJ2_TRY_CLAIM", "CJ2_RESERVE_REQUEST"
START, FINISH, MAINTAIN = "CJ2_START_REQUEST", "CJ2_FINISH_REQUEST", "CJ2_MAINTAIN_RATE_SCOPES"
SOURCES = ("CJ2_APPROVE_BOOT", CLAIM, RESERVE, START, FINISH, MAINTAIN)
# Operation, reservation label, exact status/error, wrong-token control.
STEPS = (
    (CLAIM, "a", "CLAIMED", False),
    (CLAIM, "a", "ALREADY_CLAIMED", False),
    (FINISH, "a", "CRAWL_V2_INVALID_STATE", False),
    (RESERVE, "b", "CRAWL_V2_INVALID_STATE", False),
    (START, "a", "CRAWL_V2_IMMUTABLE_MISMATCH", True),
    (START, "a", "STARTED", False),
    (START, "a", "ALREADY_STARTED", False),
    (FINISH, "a", "FINISHED", False),
    (FINISH, "a", "ALREADY_FINISHED", False),
    (START, "a", "ALREADY_STARTED", False),
    (MAINTAIN, "a", "BATCH_DONE", False),
    (RESERVE, "b", "RESERVED", False),
    (RESERVE, "b", "ALREADY_RESERVED", False),
    (FINISH, "a", "ALREADY_FINISHED", False),
    (START, "b", "STARTED", False),
    (START, "b", "ALREADY_STARTED", False),
    (START, "a", "ALREADY_STARTED", False),
    (FINISH, "b", "CRAWL_V2_IMMUTABLE_MISMATCH", True),
    (FINISH, "b", "FINISHED", False),
    (FINISH, "b", "ALREADY_FINISHED", False),
    (START, "b", "ALREADY_STARTED", False),
    (MAINTAIN, "b", "BATCH_DONE", False),
)
ERRORS = frozenset((2, 3, 4, 17))
MUTATIONS = frozenset((0, 5, 7, 11, 14, 18))
