"""Offline C0 coordinator contract. No Docker, run command, or approval issuer."""
from contracts import LIMITS, TRIALS, boolean, canonical, decode, digest, exact, hold_bound, integer, require, sha
import oracle
import runtime_admission


def role_spec(role, image, profile_sha256):
    require(role in ("target", "observer", "oracle"), "ROLE")
    require(type(image) is str and image.startswith("sha256:"), "IMAGE")
    digest(image[7:])
    digest(profile_sha256)
    return {"role": role, "image": image, "profile_sha256": profile_sha256, "user": "999:999", "network": "none",
        "read_only": True, "cap_drop": ["ALL"], "cap_add": [], "no_new_privileges": True,
        "memory": LIMITS[role + "_memory"], "memory_swap": LIMITS[role + "_memory"], "cpus": 1,
        "pids_limit": LIMITS[role + "_pids"], "core_soft": 0, "core_hard": 0,
        "pid_namespace": "exact_owned_target" if role == "observer" else "private",
        "witness_mount": "rw" if role == "target" else "ro" if role == "oracle" else None,
        "control_mount": None if role == "observer" else "private_transport_only", "execution_authorized": False}


def native_message(raw, expected):
    if expected in runtime_admission.PHASES:
        return runtime_admission.native_frame(raw, expected)
    value = decode(raw, LIMITS["receipt_bytes"])
    if expected == "HELD":
        exact(value, ("events", "ordinal", "phase", "read_bytes", "version"))
        integer(value["events"], 1, LIMITS["events"])
        integer(value["ordinal"], 1, 1050)
        integer(value["read_bytes"], 0, LIMITS["read_bytes"])
    else:
        require(expected in ("READY", "ADMISSION_READY", "ARMED", "FINISHED", "CONTINUATION_DISPATCHED"), "NATIVE_PHASE")
        exact(value, ("phase", "version"))
    integer(value["version"], 1, 1)
    require(value["phase"] == expected, "NATIVE_PHASE")
    return value


class Coordinator:
    """Pure finite transition model; external adapters/owned watchdog remain gated."""
    def __init__(self, case_id, invocation_sha256):
        require(type(case_id) is str and case_id in TRIALS, "CASE")
        digest(invocation_sha256)
        self.case_id, self.invocation_sha256 = case_id, invocation_sha256
        self.phase, self.count, self.bytes = "INTENT", 0, 0
        self.previous = None
        self.failed = False

    def advance(self, event):
        exact(event, ("case_id", "invocation_sha256", "sequence", "previous", "kind", "payload"))
        require(event["case_id"] == self.case_id and event["invocation_sha256"] == self.invocation_sha256, "BINDING")
        integer(event["sequence"], self.count, self.count)
        require(event["previous"] == self.previous, "CHAIN")
        raw = canonical(event)
        require(len(raw) <= LIMITS["receipt_bytes"] and self.bytes + len(raw) <= LIMITS["output_bytes"] and self.count < LIMITS["receipts"], "BOUNDS")
        order = {"INTENT": "ADMITTED", "ADMITTED": "TARGET_READY", "TARGET_READY": "OBSERVER_ARMED",
            "OBSERVER_ARMED": "TARGET_STARTED", "TARGET_STARTED": "HELD", "HELD": "ORACLE_MATCHED",
            "ORACLE_MATCHED": "RESUME_DISPATCHED", "RESUME_DISPATCHED": "ACTUAL_END",
            "ACTUAL_END": "TARGET_FINISHED", "TARGET_FINISHED": "CLEANUP_REQUIRED", "CLEANUP_REQUIRED": "ABSENCE_VERIFIED"}
        if event["kind"] == "INVALID":
            exact(event["payload"], ("cleanup_required",))
            require(event["payload"]["cleanup_required"] is True, "CLEANUP")
            self.failed = True
            self.phase = "CLEANUP_REQUIRED"
        else:
            require(self.phase in order and event["kind"] == order[self.phase], "ORDER")
            body = event["payload"]
            if event["kind"] == "ACTUAL_END":
                hold_bound(body)
                require(body["action"] == "resume", "ACTION")
            elif event["kind"] == "ABSENCE_VERIFIED":
                exact(body, ("inventory_sha256", "all_owned_absent", "independent_inspection", "unresolved"))
                digest(body["inventory_sha256"])
                require(body["all_owned_absent"] is True and body["independent_inspection"] is True and body["unresolved"] == [], "ABSENCE")
            else:
                exact(body, ("evidence_sha256",))
                digest(body["evidence_sha256"])
            self.phase = event["kind"]
        self.previous = sha(raw)
        self.count += 1
        self.bytes += len(raw)
        return {"phase": self.phase, "failed": self.failed, "receipt_sha256": self.previous,
            "execution_authorized": False, "runtime_evidence": False, "m4_accepted": False}
