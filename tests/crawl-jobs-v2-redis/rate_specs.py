"""Closed positive-interval admission case; no configurable intervals or dispatch."""
from request_specs import CLAIM, RESERVE, START, FINISH, MAINTAIN, SOURCES

CASE = "ledger-positive-interval-v1"
SCENARIO = "ledger-positive-interval"
INTERVAL_MS = 8000
MAX_CLOCK_OBSERVATIONS = 20
MIN_CASE_SECONDS = 120
PHASES = ("rate_before", "rate_clock", "rate_after")
PHASE_ROLES = {"rate_before": ("ledger", "observer"), "rate_clock": ("observer",), "rate_after": ("ledger", "observer")}
PREFIX_STEPS = 12
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
    (RESERVE, "b", "RATE_BLOCKED", False),
    (RESERVE, "b", "RESERVED", False),
    (RESERVE, "b", "ALREADY_RESERVED", False),
    (FINISH, "a", "ALREADY_FINISHED", False),
    (START, "b", "STARTED", False),
    (RESERVE, "b", "ALREADY_RESERVED", False),
    (START, "b", "ALREADY_STARTED", False),
    (START, "a", "ALREADY_STARTED", False),
    (FINISH, "b", "CRAWL_V2_IMMUTABLE_MISMATCH", True),
    (FINISH, "b", "FINISHED", False),
    (FINISH, "b", "ALREADY_FINISHED", False),
    (START, "b", "ALREADY_STARTED", False),
    (MAINTAIN, "b", "BATCH_DONE", False),
)
ERRORS = frozenset((2, 3, 4, 19))
MUTATIONS = frozenset((0, 5, 7, 12, 15, 20))
