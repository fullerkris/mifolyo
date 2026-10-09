"""Closed C0 preparation contracts. Pure validation; no process or tracing API."""
import hashlib
import json
import re

DOMAIN = "mifolyo:m4:obs1:c0:preparation"
MAX_ARTIFACT = 2 * 1024 * 1024
LIMITS = {"admission_ms": 10000, "observation_ms": 2000, "cleanup_ms": 5000,
    "suite_ms": 5400000, "hold_ns": 20000000, "pause_ns": 250000000,
    "events": 8192, "read_bytes": 4194304, "receipts": 64, "receipt_bytes": 1024,
    "output_bytes": 65536, "observer_memory": 67108864, "target_memory": 67108864,
    "oracle_memory": 33554432, "observer_pids": 16, "target_pids": 16, "oracle_pids": 8}
CAPS = frozenset(("effective", "permitted", "inheritable", "ambient", "bounding"))
ARTIFACTS = frozenset(("target_source", "observer_source", "oracle_source", "validator_source",
    "target_elf", "observer_elf", "instruction_manifest", "observer_profile", "target_profile",
    "oracle_profile", "filter_generator", "controller_source", "toolchain_record", "admission_source"))


class Invalid(ValueError):
    """Stable value-free failure; never copy rejected data into diagnostics."""


def require(condition, code):
    if not condition:
        raise Invalid(code)


def exact(value, fields):
    require(type(value) is dict and set(value) == set(fields), "FIELDS")


def integer(value, low=0, high=2**53 - 1):
    require(type(value) is int and low <= value <= high, "INTEGER")
    return value


def clock_ns(value, low=0):
    """Exact native uint64 nanoseconds; counters keep their existing safe range."""
    return integer(value, low, 2**64 - 1)


def boolean(value):
    require(type(value) is bool, "BOOLEAN")
    return value


def digest(value):
    require(type(value) is str and re.fullmatch(r"[0-9a-f]{64}", value) is not None and value != "0" * 64, "DIGEST")
    return value


def canonical(value):
    try:
        return (json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False, allow_nan=False) + "\n").encode()
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise Invalid("JSON") from None


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def decode(raw, limit=MAX_ARTIFACT):
    require(type(raw) is bytes and 0 < len(raw) <= limit, "BYTES")

    def pairs(rows):
        result = {}
        for key, value in rows:
            require(key not in result, "DUPLICATE")
            result[key] = value
        return result

    def forbidden(_):
        raise Invalid("NUMBER")

    try:
        value = json.loads(raw.decode("utf-8"), object_pairs_hook=pairs, parse_float=forbidden, parse_constant=forbidden)
        require(type(value) is dict and canonical(value) == raw, "CANONICAL")
        return value
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise Invalid("JSON") from None


def trials():
    result = []
    for boundary in ("pre", "post"):
        for ordinal in (1, 2, 127, 128, 129, 1049, 1050):
            result.extend(f"P1.{boundary}.{ordinal}.{repeat}" for repeat in range(1, 11))
    for prototype in ("a", "b"):
        for pc in (0, 1, 7):
            result.extend(f"P2.{prototype}.{pc}.{repeat}" for repeat in range(1, 11))
    for name in ("heartbeat", "pre-send", "received", "receipt-race"):
        result.extend(f"P3.{name}.{repeat}" for repeat in range(1, 11))
    for name in ("before-seize-loss", "seized-loss", "held-loss", "controller-loss",
                 "transport-eof", "backpressure", "hold-deadline", "target-exit"):
        result.extend(f"F.{name}.{repeat}" for repeat in range(1, 4))
    result.extend(f"N.{number:02}" for number in range(1, 13))
    return result


TRIALS = tuple(trials())


def preparation(artifacts):
    exact(artifacts, ARTIFACTS)
    for value in artifacts.values():
        digest(value)
    return {"domain": DOMAIN, "version": 1, "candidate": "C0", "architecture": "aarch64",
        "artifacts": dict(artifacts), "trial_ids": list(TRIALS), "limits": dict(LIMITS),
        "implementation_only": True, "execution_authorized": False, "m4_accepted": False}


def validate_preparation(value):
    exact(value, ("domain", "version", "candidate", "architecture", "artifacts", "trial_ids", "limits",
                  "implementation_only", "execution_authorized", "m4_accepted"))
    integer(value["version"], 1, 1)
    require(canonical(value) == canonical(preparation(value["artifacts"])), "PREPARATION")
    return value


def identity(value):
    exact(value, ("container_sha256", "executable_sha256", "kernel_boot_sha256", "pid_namespace",
        "user_namespace", "time_namespace", "uid_map_sha256", "gid_map_sha256", "start_ticks", "pid", "tid", "uid", "gid", "capabilities"))
    for name in ("container_sha256", "executable_sha256", "kernel_boot_sha256", "uid_map_sha256", "gid_map_sha256"):
        digest(value[name])
    for name in ("pid_namespace", "user_namespace", "time_namespace", "start_ticks"):
        integer(value[name], 1)
    for name in ("pid", "tid"):
        integer(value[name], 1, 1)
    integer(value["uid"], 999, 999)
    integer(value["gid"], 999, 999)
    exact(value["capabilities"], CAPS)
    for cap in value["capabilities"].values():
        integer(cap, 0, 0)
    return value


def same_identity(before, after):
    identity(before)
    identity(after)
    require(canonical(before) == canonical(after), "IDENTITY_CHANGED")


def hold_bound(value):
    """COR-01: dispatch is never an end witness. Input witnesses remain external."""
    exact(value, ("action", "clock_domain_sha256", "uncertainty_ns", "pre_witness_ns", "dispatch_ns",
                  "post_continuation_ns", "confirmed_stop_ns"))
    digest(value["clock_domain_sha256"])
    integer(value["uncertainty_ns"], 0, LIMITS["hold_ns"])
    start = clock_ns(value["pre_witness_ns"], 1)
    dispatched = clock_ns(value["dispatch_ns"], start)
    if value["action"] == "resume":
        require(value["confirmed_stop_ns"] is None, "END_VARIANT")
        end = clock_ns(value["post_continuation_ns"], dispatched)
    else:
        require(value["action"] == "kill" and value["post_continuation_ns"] is None, "END_VARIANT")
        end = clock_ns(value["confirmed_stop_ns"], dispatched)
    bound = end - start + 2 * value["uncertainty_ns"]
    require(bound <= LIMITS["hold_ns"], "HOLD_BOUND")
    return {"quantity": "conservative_hold_upper_bound_ns", "value": bound, "execution_evidence": False}


def receipt(raw, *, case_id, invocation_sha256, artifacts_sha256, role, sequence, previous):
    value = decode(raw, LIMITS["receipt_bytes"])
    exact(value, ("domain", "version", "case_id", "invocation_sha256", "artifacts_sha256", "role", "sequence", "previous", "phase", "payload"))
    require(case_id in TRIALS and value["case_id"] == case_id and value["domain"] == DOMAIN, "CASE")
    integer(value["version"], 1, 1)
    require(role in ("controller", "observer", "oracle") and value["role"] == role, "ROLE")
    for key, expected in (("invocation_sha256", invocation_sha256), ("artifacts_sha256", artifacts_sha256)):
        digest(expected)
        require(value[key] == expected, "BINDING")
    integer(sequence, 0, LIMITS["receipts"] - 1)
    integer(value["sequence"], sequence, sequence)
    if sequence == 0:
        require(previous is None and value["previous"] is None, "CHAIN")
    else:
        digest(previous)
        require(value["previous"] == previous, "CHAIN")
    phase = value["phase"]
    if phase == "held":
        require(role == "observer", "PHASE_ROLE")
        exact(value["payload"], ("identity_sha256", "boundary_sha256", "stop", "ordinal", "events", "read_bytes"))
        for key in ("identity_sha256", "boundary_sha256"):
            digest(value["payload"][key])
        require(value["payload"]["stop"] == "hardware_execution_breakpoint", "STOP_REASON")
        integer(value["payload"]["ordinal"], 1, 1050)
        integer(value["payload"]["events"], 1, LIMITS["events"])
        integer(value["payload"]["read_bytes"], 0, LIMITS["read_bytes"])
    elif phase == "invalid":
        exact(value["payload"], ("reason", "cleanup_required"))
        require(value["payload"]["reason"] in ("admission", "identity", "ptrace_denied", "stop_reason", "deadline", "transport", "bounds"), "REASON")
        require(value["payload"]["cleanup_required"] is True, "CLEANUP")
    else:
        raise Invalid("PHASE")
    return value
