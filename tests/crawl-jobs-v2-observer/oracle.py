"""Independent synthetic witness interpretation; no tracing and no target writes."""
import os
import stat
import struct

from contracts import TRIALS, clock_ns, exact, integer, require, sha

WITNESS_BYTES = 8480
MAGIC = 0x4F42533143303031
FIELDS = ("magic", "completed", "ordinal", "prototype", "bytecode_pc", "pre_ns", "post_ns", "heartbeat", "finished", "last_value")


def validate_state(value):
    exact(value, (*FIELDS, "transcript"))
    require(type(value["magic"]) is int and value["magic"] == MAGIC, "WITNESS_MAGIC")
    for name in set(FIELDS) - {"magic", "pre_ns", "post_ns"}:
        integer(value[name])
    for name in ("pre_ns", "post_ns"):
        clock_ns(value[name])
    integer(value["completed"], 0, 1050)
    integer(value["ordinal"], 0, 1050)
    integer(value["prototype"], 0, 2)
    integer(value["bytecode_pc"], 0, 7)
    integer(value["finished"], 0, 1)
    require(type(value["transcript"]) in (tuple, list) and len(value["transcript"]) == 1050, "TRANSCRIPT")
    for item in value["transcript"]:
        integer(item, 0, 1050)
    return value


def decode_witness(raw):
    require(type(raw) is bytes and len(raw) == WITNESS_BYTES, "WITNESS_BYTES")
    words = struct.unpack("<1060Q", raw)
    value = dict(zip(FIELDS, words[:10]))
    value["transcript"] = words[10:]
    return validate_state(value)


def read_witness(expected):
    """Caller supplies independently admitted file identity, never a pathname."""
    exact(expected, ("device", "inode", "uid", "gid", "mode", "bytes"))
    for key in ("device", "inode"):
        integer(expected[key], 1)
    require((expected["uid"], expected["gid"], expected["mode"], expected["bytes"]) == (999, 999, 0o600, WITNESS_BYTES), "WITNESS_IDENTITY")
    fd = os.open("/witness/state", os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1, "WITNESS_FILE")
        observed = {"device": before.st_dev, "inode": before.st_ino, "uid": before.st_uid, "gid": before.st_gid,
            "mode": stat.S_IMODE(before.st_mode), "bytes": before.st_size}
        require(observed == expected, "WITNESS_IDENTITY")
        first = os.pread(fd, WITNESS_BYTES, 0)
        second = os.pread(fd, WITNESS_BYTES, 0)
        after = os.fstat(fd)
        require((after.st_dev, after.st_ino, after.st_size, after.st_nlink) == (before.st_dev, before.st_ino, WITNESS_BYTES, 1), "WITNESS_CHANGED")
        # The explicitly admitted helper heartbeat is the only concurrently
        # writable field while main is held. Do not mistake a torn main view for proof.
        require(first[:56] + first[64:] == second[:56] + second[64:], "WITNESS_UNSTABLE")
        return decode_witness(second)
    finally:
        os.close(fd)


def held(case_id, state):
    require(type(case_id) is str and case_id in TRIALS, "CASE")
    validate_state(state)
    require(state["finished"] == 0, "NOT_HELD")
    if case_id.startswith("P1.") or case_id.startswith("P3.heartbeat.") or case_id.startswith("F."):
        if case_id.startswith("P1."):
            _, boundary, ordinal, _ = case_id.split(".")
            ordinal = int(ordinal)
        else:
            boundary, ordinal = "pre", 1 if case_id.startswith("P3.") else 129
        completed = ordinal - (boundary == "pre")
        require(state["completed"] == completed and state["ordinal"] == ordinal, "ORDINAL")
        require(list(state["transcript"]) == list(range(1, completed + 1)) + [0] * (1050 - completed), "EFFECT_TRANSCRIPT")
        require(state["last_value"] == (42 if completed else 0), "SAME_VALUE")
        require(state["prototype"] == state["bytecode_pc"] == 0, "NATIVE_VM_STATE")
    elif case_id.startswith("P2."):
        _, prototype, pc, _ = case_id.split(".")
        expected = {("a", 0): 1, ("a", 1): 2, ("b", 0): 3, ("b", 1): 4, ("b", 7): 5, ("a", 7): 6}
        require((state["prototype"], state["bytecode_pc"], state["ordinal"]) == (1 if prototype == "a" else 2, int(pc), expected[prototype, int(pc)]), "FAKE_VM")
        require(state["completed"] == state["last_value"] == 0 and not any(state["transcript"]), "FAKE_VM_EFFECT")
    else:
        require(case_id.startswith("P3."), "VALIDATOR_CONTROL")
        require(state["ordinal"] == 1 and state["completed"] == state["last_value"] == state["prototype"] == state["bytecode_pc"] == 0,
            "FRAME_STATE")
        require(not any(state["transcript"]), "FRAME_TRANSCRIPT")
    if not case_id.startswith("P3.heartbeat."):
        require(state["heartbeat"] == 0, "UNSCHEDULED_HEARTBEAT")
    require(state["pre_ns"] > 0 and state["post_ns"] < state["pre_ns"], "PRE_WITNESS")
    return {"case_id": case_id, "ordinal_matches": True, "prototype_pc_matches": True,
        "transcript_sha256": sha(struct.pack("<1050Q", *state["transcript"])), "runtime_evidence": False}


def completed(case_id, state):
    require(type(case_id) is str and case_id in TRIALS and case_id.startswith(("P1.", "P2.", "P3.")), "FINAL_CASE")
    validate_state(state)
    require(state["finished"] == 1, "NOT_FINISHED")
    require(state["post_ns"] >= state["pre_ns"] > 0, "POST_WITNESS")
    if case_id.startswith("P1.") or case_id.startswith("P3.heartbeat."):
        require(state["completed"] == state["ordinal"] == 1050 and list(state["transcript"]) == list(range(1, 1051)) and state["last_value"] == 42, "FINAL_TRANSCRIPT")
        require(state["prototype"] == state["bytecode_pc"] == 0, "FINAL_VM_STATE")
    else:
        require(state["completed"] == state["last_value"] == 0 and not any(state["transcript"]), "FINAL_EFFECTS")
        expected = (1, 7, 6) if case_id.startswith("P2.") else (0, 0, 1)
        require((state["prototype"], state["bytecode_pc"], state["ordinal"]) == expected, "FINAL_MARKER")
    if not case_id.startswith("P3.heartbeat."):
        require(state["heartbeat"] == 0, "UNSCHEDULED_HEARTBEAT")
    return {"case_id": case_id, "final_contract_valid": True, "runtime_evidence": False}
