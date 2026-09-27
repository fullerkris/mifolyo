"""Closed vocabulary for one pre-I/O worker-death recovery case."""

CASE = "ledger-worker-death-pre-io-v1"
SCENARIO = "ledger-worker-death-pre-io"
SOURCES = ("CJ2_APPROVE_BOOT", "CJ2_TRY_CLAIM", "CJ2_RECOVER_EXPIRED", "CJ2_RENEW_LEASE", "CJ2_RELEASE_BEFORE_IO")
PHASES = ("claim_park", "observe_claim", "lease_clock", "recover")
PHASE_ROLES = {"claim_park": ("ledger", "observer"), "observe_claim": ("ledger", "observer"),
               "lease_clock": ("observer",), "recover": ("ledger", "observer")}
CONTAINER_ROLES = ("init", "executor", "redis", "executor_b", "revocation")
MAX_CLOCK_OBSERVATIONS = 40
MAX_ACK_TO_KILL_MS = 1000
MAX_ATTACHED_EXIT_SECONDS = 5
MIN_CASE_SECONDS = 180
