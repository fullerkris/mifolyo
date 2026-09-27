"""Closed recovery phases and strict private-input/public-receipt reconciliation."""
import os
import re

import claim_executor as claim
import claim_release as cr
import harness as h
import negative_executor as negative
import recovery_oracle as oracle
import recovery_specs as spec
import runtime_case as case
from resp import RedisError


class RecoveryFailure(h.InvalidArtifact):
    def __init__(self, result):
        self.result = result
        super().__init__("RECOVERY_STAGE_FAILED")


def fixture(request, resume):
    result = case.claim_fixture(request["plan"], request["fixture_id"], request["claim_material"], resume["setup_time_ms"])
    h.require(h.canonical(oracle._public_summary_validated(result)) == h.canonical(resume["fixture_summary"]), "RECOVERY_FIXTURE_BINDING")
    return result


def counts(state, f):
    result = claim.counters(state, f)
    result["job.pre_io_recoveries"] = claim._number(dict(state[f["job_key"]]["fields"])["pre_io_recoveries"].encode())
    values = dict(state[f["base_key"] + ":recovery_outcome_counts"]["fields"])
    for reason in cr.RECOVERY_REASONS:
        result["recovery." + reason] = claim._number(values[reason].encode())
    h.require(len(result) == 38, "RECOVERY_COUNTER_INVENTORY")
    return result


def summary(state, f):
    expiries = []
    for label in ("a", "b"):
        key = h.P + "reservation:" + f["identities"][label]["reservation_id"]
        row = state[key]
        if row is not None:
            values = dict(row["fields"])
            expiries.append({"reference_sha256": h.digest(key.encode()), "state": values["state"],
                "expires_at_ms": row["expires_at_ms"], "logical_expires_at_ms": claim._number(values["expires_at_ms"].encode())})
    return {"state_sha256": h.digest(h.canonical(state)), "counters": counts(state, f), "reservation_expiries": expiries}


def projected(plan, f, boot, times, index):
    state = f["initial_state"] if index < 0 else oracle._expected_sequence_validated(f, oracle.prefix_times(times))[index]["state"]
    return {**state, h.AUTH[0]: cr._hash([(key, boot[key]) for key in case.BOOT_FIELDS])}


def observe(observer, f, boot, expected):
    return claim.snapshot(observer, {key: row for key, row in expected.items() if key != h.AUTH[0]}, boot)


def _probe_binding(plan, fid, previous, proof, proof_sha):
    value = "m4-probe:" + fid + ":" + h.digest(h.canonical(plan))
    h.exact(proof, {"case", "fixture_id", "plan_sha256", "old_run_id", "new_run_id", "acknowledged_probe_sha256", "verified_at_ms", "acknowledged_loss_bound"})
    h.require(proof["case"] == spec.CASE and proof["fixture_id"] == fid and proof["plan_sha256"] == h.digest(h.canonical(plan)) and
              proof["acknowledged_probe_sha256"] == h.digest(value.encode()) and proof_sha == h.digest(h.canonical(proof)) and
              proof["old_run_id"] != proof["new_run_id"] and type(proof["verified_at_ms"]) is int and 0 < proof["verified_at_ms"] <= h.MAX_EXACT and
              type(proof["acknowledged_loss_bound"]) is int and proof["acknowledged_loss_bound"] == 0 and
              all(type(proof[key]) is str and re.fullmatch(r"[0-9a-f]{40}", proof[key]) for key in ("old_run_id", "new_run_id")), "RECOVERY_BOOT_PROVENANCE")
    if previous is not None:
        h.require(previous["acknowledged"] is True and previous["value"] == value and previous["value_sha256"] == proof["acknowledged_probe_sha256"] and
                  previous["old_run_id"] == proof["old_run_id"] and type(previous["at_ms"]) is int and 0 < previous["at_ms"] <= proof["verified_at_ms"], "RECOVERY_PROBE_BINDING")


def validate_resume(result, request, previous=None):
    h.exact(result, {"probe_evidence", "probe_evidence_sha256", "boot_epoch", "boot_record", "setup_time_ms", "setup_time_observed",
                     "fixture_summary", "setup_state_sha256", "setup_counters", "early_revocation"})
    plan, fid = request["plan"], request["fixture_id"]
    proof = result["probe_evidence"]
    _probe_binding(plan, fid, previous, proof, result["probe_evidence_sha256"])
    h.require(result["boot_epoch"] == h.digest((fid + ":boot").encode())[:32] and result["setup_time_observed"] is True and
              type(result["setup_time_ms"]) is int and proof["verified_at_ms"] <= result["setup_time_ms"] <= h.MAX_EXACT, "RECOVERY_SETUP_TIME")
    boot = result["boot_record"]
    h.exact(boot, set(case.BOOT_FIELDS))
    h.require(type(boot["approved_at_ms"]) is str and re.fullmatch(r"[1-9][0-9]{0,15}", boot["approved_at_ms"]) and
              proof["verified_at_ms"] <= int(boot["approved_at_ms"]) <= result["setup_time_ms"], "RECOVERY_BOOT_TIME")
    wanted = dict(zip(case.BOOT_FIELDS, ("1", "approved", proof["new_run_id"], result["boot_epoch"], boot["approved_at_ms"], "", "", "initial", "",
                                      result["probe_evidence_sha256"], str(proof["verified_at_ms"]), "0")))
    h.require(boot == wanted, "RECOVERY_BOOT_RECORD")
    f = fixture(request, result)
    initial = projected(plan, f, boot, [], -1)
    h.require(result["setup_state_sha256"] == h.digest(h.canonical(initial)) and result["setup_counters"] == counts(initial, f) and
              all(type(value) is int for value in result["setup_counters"].values()), "RECOVERY_SETUP_BINDING")
    case.validate_revocation(result["early_revocation"], case.EARLY_ROLES)
    return f


def resume(request, setup, current, proof, epoch, api):
    plan, credentials = request["plan"], request["credentials"]
    proof_sha = h.digest(h.canonical(proof))
    _probe_binding(plan, request["fixture_id"], request["previous"], proof, proof_sha)
    wire = case.boot_request(current["run_id"], epoch, proof_sha, proof["verified_at_ms"])
    with api.connect("boot", credentials) as client:
        before = api.clock(setup)
        first = client.call(*wire)
        now = negative.reply_time(first, before, api.clock(setup))
        negative.exact_reply(first, ["OK", str(now), epoch])
        boot = dict(zip(case.BOOT_FIELDS, ("1", "approved", current["run_id"], epoch, str(now), "", "", "initial", "", proof_sha, str(proof["verified_at_ms"]), "0")))
        h.require(api.read_hash(setup, h.AUTH[0], case.BOOT_FIELDS) == boot, "RECOVERY_BOOT_STATE")
        replay = client.call(*wire)
        replay_now = negative.reply_time(replay, now, api.clock(setup))
        negative.exact_reply(replay, ["EXISTS_IDENTICAL", str(replay_now), epoch])
        h.require(api.read_hash(setup, h.AUTH[0], case.BOOT_FIELDS) == boot, "RECOVERY_BOOT_REPLAY")
    at = api.clock(setup)
    f = case.claim_fixture(plan, request["fixture_id"], request["claim_material"], at)
    binding = case.claim_fixture(plan, request["fixture_id"], request["claim_material"])
    h.require(case.acl_rules(spec.CASE, f, plan) == case.acl_rules(spec.CASE, binding, plan), "RECOVERY_ACL_BINDING")
    state_sha = claim.install(setup, plan, f, boot)
    actual = claim.snapshot(setup, f["initial_state"], boot)
    h.require(h.digest(h.canonical(actual)) == state_sha, "RECOVERY_SETUP_CHANGED")
    return {"probe_evidence": proof, "probe_evidence_sha256": proof_sha, "boot_epoch": epoch, "boot_record": boot,
            "setup_time_ms": at, "setup_time_observed": True, "fixture_summary": oracle._public_summary_validated(f),
            "setup_state_sha256": state_sha, "setup_counters": counts(actual, f),
            "early_revocation": api.revoke(credentials, case.EARLY_ROLES)}


def validate_claim(result, request, resume):
    f = fixture(request, resume)
    _validate_claim(result, resume, f)


def _validate_claim(result, resume, f):
    h.exact(result, {"fixture_sha256", "resume_sha256", "worker_pid", "response_code", "claim_time_ms", "lease_expires_at_ms", "response_sha256"})
    now = result["claim_time_ms"]
    h.require(result["fixture_sha256"] == resume["fixture_summary"]["fixture_sha256"] and result["resume_sha256"] == h.digest(h.canonical(resume)) and
              type(result["worker_pid"]) is int and result["worker_pid"] > 1 and result["response_code"] == "CLAIMED" and
              type(now) is int and resume["setup_time_ms"] <= now <= resume["setup_time_ms"] + 120000 and
              type(result["lease_expires_at_ms"]) is int and result["lease_expires_at_ms"] == now + cr.LEASE_MS, "RECOVERY_CLAIM_RECEIPT")
    q = f["identities"]["a"]["reservation_id"]
    wanted = ["CLAIMED", str(now), "1", str(now + cr.LEASE_MS), q, str(now + cr.LEASE_MS)]
    h.require(result["response_sha256"] == h.digest(h.canonical(wanted)), "RECOVERY_CLAIM_REPLY")


def claim_park(request, api):
    resumed, credentials, plan = request["previous"], request["credentials"], request["plan"]
    f = validate_resume(resumed, request)
    with api.connect("observer", credentials) as observer, api.connect("ledger", credentials) as ledger:
        h.require(api.server(observer)["run_id"] == resumed["probe_evidence"]["new_run_id"], "RECOVERY_BOOT_CHANGED")
        state = claim.snapshot(observer, f["initial_state"], resumed["boot_record"])
        h.require(h.digest(h.canonical(state)) == resumed["setup_state_sha256"], "RECOVERY_SETUP_CHANGED")
        wire = oracle.wire_requests(plan, f, resumed["boot_epoch"])[0]
        before = api.clock(observer)
        reply = ledger.call(*wire)
        now = negative.reply_time(reply, before, api.clock(observer))
        q = f["identities"]["a"]["reservation_id"]
        response_sha = negative.exact_reply(reply, ["CLAIMED", str(now), "1", str(now + cr.LEASE_MS), q, str(now + cr.LEASE_MS)])
    # The process, not its Redis connection, parks after this acknowledgment.
    return {"fixture_sha256": resumed["fixture_summary"]["fixture_sha256"], "resume_sha256": h.digest(h.canonical(resumed)),
            "worker_pid": os.getpid(), "response_code": "CLAIMED", "claim_time_ms": now,
            "lease_expires_at_ms": now + cr.LEASE_MS, "response_sha256": response_sha}


def _row(index, now, response_sha, actual, f, before):
    measured = summary(actual, f)
    return {"sequence": index, "assertion_id": f"RCV{index + 1:02}", "operation": oracle.OPERATIONS[index], "now_ms": now,
            "response_sha256": response_sha, "state_sha256": measured["state_sha256"], "reservation_expiries": measured["reservation_expiries"],
            "counters": {"before": before, "after": measured["counters"], "delta": {key: measured["counters"][key] - before[key] for key in before}}}


def validate_row(row, request, resumed, f, times, index):
    h.exact(row, {"sequence", "assertion_id", "operation", "now_ms", "response_sha256", "state_sha256", "reservation_expiries", "counters"})
    h.require(type(row["sequence"]) is int and row["sequence"] == index and row["assertion_id"] == f"RCV{index + 1:02}" and
              row["operation"] == oracle.OPERATIONS[index] and type(row["now_ms"]) is int and row["now_ms"] == times[index], "RECOVERY_ROW")
    plan, boot = request["plan"], resumed["boot_record"]
    expected = oracle._expected_sequence_validated(f, oracle.prefix_times(times))
    before = summary(projected(plan, f, boot, times, index - 1), f)
    after = summary(projected(plan, f, boot, times, index), f)
    h.require(row["response_sha256"] == h.digest(h.canonical(expected[index]["reply"])) and row["state_sha256"] == after["state_sha256"] and
              row["reservation_expiries"] == after["reservation_expiries"], "RECOVERY_ROW_BINDING")
    h.exact(row["counters"], {"before", "after", "delta"})
    wanted = {"before": before["counters"], "after": after["counters"],
              "delta": {key: after["counters"][key] - before["counters"][key] for key in before["counters"]}}
    h.require(row["counters"] == wanted and all(type(value) is int for group in row["counters"].values() for value in group.values()), "RECOVERY_COUNTER_EVIDENCE")


def _context(request, fields):
    prior = request["previous"]
    h.exact(prior, fields)
    f = validate_resume(prior["resume"], request)
    _validate_claim(prior["claim"], prior["resume"], f)
    return prior, f


def observe_claim(request, api):
    previous, f = _context(request, {"resume", "claim"})
    resumed, claimed = previous["resume"], previous["claim"]
    times = [claimed["claim_time_ms"]]
    with api.connect("observer", request["credentials"]) as observer, api.connect("ledger", request["credentials"]) as ledger:
        h.require(api.server(observer)["run_id"] == resumed["probe_evidence"]["new_run_id"], "RECOVERY_BOOT_CHANGED")
        actual = observe(observer, f, resumed["boot_record"], projected(request["plan"], f, resumed["boot_record"], times, 0))
        claimed_summary = summary(actual, f)
        observed_at = api.clock(observer)
        h.require(times[0] <= observed_at < claimed["lease_expires_at_ms"], "RECOVERY_EARLY_WINDOW_MISSED")
        wire = oracle.wire_requests(request["plan"], f, resumed["boot_epoch"])[1]
        reply = ledger.call(*wire)
        now = negative.reply_time(reply, observed_at, api.clock(observer))
        times.append(now)
        expected = oracle._expected_sequence_validated(f, oracle.prefix_times(times))[1]
        response_sha = negative.exact_reply(reply, expected["reply"])
        actual = observe(observer, f, resumed["boot_record"], projected(request["plan"], f, resumed["boot_record"], times, 1))
    return {"fixture_sha256": resumed["fixture_summary"]["fixture_sha256"], "claim_receipt_sha256": h.digest(h.canonical(claimed)),
            "claimed_snapshot": claimed_summary, "claimed_observed_at_ms": observed_at, "observed_times": times,
            "early_recovery": _row(1, now, response_sha, actual, f, claimed_summary["counters"])}


def validate_observation(result, request, resumed, claimed, f):
    h.exact(result, {"fixture_sha256", "claim_receipt_sha256", "claimed_snapshot", "claimed_observed_at_ms", "observed_times", "early_recovery"})
    times = result["observed_times"]
    h.require(type(times) is list and len(times) == 2 and times[0] == claimed["claim_time_ms"] and
              result["fixture_sha256"] == resumed["fixture_summary"]["fixture_sha256"] and
              result["claim_receipt_sha256"] == h.digest(h.canonical(claimed)) and type(result["claimed_observed_at_ms"]) is int and
              times[0] <= result["claimed_observed_at_ms"] <= times[1] < claimed["lease_expires_at_ms"], "RECOVERY_OBSERVATION")
    wanted = summary(projected(request["plan"], f, resumed["boot_record"], times, 0), f)
    h.require(h.canonical(result["claimed_snapshot"]) == h.canonical(wanted), "RECOVERY_CLAIM_STATE")
    validate_row(result["early_recovery"], request, resumed, f, times, 1)


def lease_clock(request, api):
    previous, f = _context(request, {"resume", "claim", "observation"})
    validate_observation(previous["observation"], request, previous["resume"], previous["claim"], f)
    with api.connect("observer", request["credentials"]) as observer:
        run_id = api.server(observer)["run_id"]
        h.require(run_id == previous["resume"]["probe_evidence"]["new_run_id"], "RECOVERY_BOOT_CHANGED")
        now = api.clock(observer)
    return {"now_ms": now, "run_id": run_id}


def validate_clock(result, resumed, minimum):
    h.exact(result, {"now_ms", "run_id"})
    h.require(result["run_id"] == resumed["probe_evidence"]["new_run_id"] and type(result["now_ms"]) is int and
              minimum <= result["now_ms"] <= resumed["setup_time_ms"] + cr.CASE_MS, "RECOVERY_CLOCK_EVIDENCE")


def recover(request, api):
    previous, f = _context(request, {"resume", "claim", "observation", "wait"})
    resumed, claimed, observation = previous["resume"], previous["claim"], previous["observation"]
    validate_observation(observation, request, resumed, claimed, f)
    validate_wait(previous["wait"], resumed, claimed, observation)
    times = list(observation["observed_times"])
    result = {"fixture_sha256": resumed["fixture_summary"]["fixture_sha256"], "observed_times": times, "steps": [], "acl_negatives": []}
    assertion = "STATE"
    try:
        with api.connect("observer", request["credentials"]) as observer, api.connect("ledger", request["credentials"]) as ledger:
            h.require(api.server(observer)["run_id"] == resumed["probe_evidence"]["new_run_id"], "RECOVERY_BOOT_CHANGED")
            actual = observe(observer, f, resumed["boot_record"], projected(request["plan"], f, resumed["boot_record"], times, 1))
            before_counts = counts(actual, f)
            h.require(api.clock(observer) >= max(claimed["lease_expires_at_ms"], previous["wait"]["observations"][-1]["now_ms"]), "RECOVERY_NOT_DUE")
            wires = oracle.wire_requests(request["plan"], f, resumed["boot_epoch"])
            for index in range(2, 13):
                assertion = f"RCV{index + 1:02}"
                before = api.clock(observer)
                reply = ledger.call(*wires[index])
                now = negative.reply_time(reply, before, api.clock(observer))
                candidate_times = [*times, now]
                expected = oracle._expected_sequence_validated(f, oracle.prefix_times(candidate_times))[index]
                response_sha = negative.exact_reply(reply, expected["reply"])
                actual = observe(observer, f, resumed["boot_record"], projected(request["plan"], f, resumed["boot_record"], candidate_times, index))
                result["steps"].append(_row(index, now, response_sha, actual, f, before_counts))
                times.append(now)
                before_counts = counts(actual, f)
            assertion = "ACL"
            for authority, command in claim.acl_probes(f):
                try:
                    ledger.call(*command)
                except RedisError as error:
                    h.require(error.code == "NOPERM", "RECOVERY_ACL_DENIAL")
                else:
                    raise h.InvalidArtifact("RECOVERY_ACL_ESCALATION")
                actual = observe(observer, f, resumed["boot_record"], projected(request["plan"], f, resumed["boot_record"], times, 12))
                result["acl_negatives"].append({"authority_role": authority, "command": command[0], "result": "NOPERM", "state_sha256": h.digest(h.canonical(actual))})
            result["final_state_sha256"] = h.digest(h.canonical(actual))
        return result
    except Exception:
        result["failed_assertion"] = assertion
        raise RecoveryFailure(result) from None


def validate_wait(wait, resumed, claimed, observation):
    h.exact(wait, {"observations"})
    values = wait["observations"]
    h.require(type(values) is list and 1 <= len(values) <= spec.MAX_CLOCK_OBSERVATIONS, "RECOVERY_WAIT_BOUND")
    minimum = observation["observed_times"][-1]
    for index, row in enumerate(values):
        validate_clock(row, resumed, minimum)
        h.require(index == len(values) - 1 or row["now_ms"] < claimed["lease_expires_at_ms"], "RECOVERY_WAIT_ORDER")
        minimum = row["now_ms"]
    h.require(minimum >= claimed["lease_expires_at_ms"], "RECOVERY_WAIT_INCOMPLETE")


def validate_stage_result(stage, result, request, successful=True):
    if stage == "resume":
        h.require(successful, "RECOVERY_RESUME_FAILED")
        validate_resume(result, request, request["previous"])
        return
    if stage not in spec.PHASES:
        h.require(successful, "RECOVERY_FAILURE_STAGE")
        claim.validate_stage_result(stage, result, request)
        return
    h.require(stage == "recover" or successful, "RECOVERY_FAILURE_STAGE")
    if stage == "claim_park":
        f = validate_resume(request["previous"], request)
        _validate_claim(result, request["previous"], f)
        return
    fields = {"resume", "claim"} | ({"observation"} if stage in ("lease_clock", "recover") else set()) | ({"wait"} if stage == "recover" else set())
    previous, f = _context(request, fields)
    resumed, claimed = previous["resume"], previous["claim"]
    if stage == "observe_claim":
        validate_observation(result, request, resumed, claimed, f)
        return
    observation = previous["observation"]
    validate_observation(observation, request, resumed, claimed, f)
    if stage == "lease_clock":
        validate_clock(result, resumed, observation["observed_times"][-1])
        return
    validate_wait(previous["wait"], resumed, claimed, observation)
    h.exact(result, {"fixture_sha256", "observed_times", "steps", "acl_negatives", "final_state_sha256" if successful else "failed_assertion"})
    h.require(result["fixture_sha256"] == resumed["fixture_summary"]["fixture_sha256"] and type(result["steps"]) is list and
              len(result["steps"]) <= 11 and type(result["acl_negatives"]) is list and len(result["acl_negatives"]) <= 46, "RECOVERY_RESULT")
    times = result["observed_times"]
    # Only fully checked steps enter the retained observed-time prefix.
    h.require(type(times) is list and len(times) == len(result["steps"]) + 2 and
              times[:2] == observation["observed_times"], "RECOVERY_TIME_PREFIX")
    oracle._expected_sequence_validated(f, oracle.prefix_times(times))
    h.require(len(times) == 2 or times[2] >= previous["wait"]["observations"][-1]["now_ms"], "RECOVERY_CLOCK_ORDER")
    for index, row in enumerate(result["steps"], 2):
        validate_row(row, request, resumed, f, times, index)
    final = projected(request["plan"], f, resumed["boot_record"], times, 12) if len(result["steps"]) == 11 else None
    probes = claim.acl_probes(f)
    for index, row in enumerate(result["acl_negatives"]):
        h.exact(row, {"authority_role", "command", "result", "state_sha256"})
        h.require(final is not None and row == {"authority_role": probes[index][0], "command": probes[index][1][0],
                  "result": "NOPERM", "state_sha256": h.digest(h.canonical(final))}, "RECOVERY_ACL_EVIDENCE")
    if successful:
        h.require(len(times) == 13 and len(result["steps"]) == 11 and len(result["acl_negatives"]) == 46 and
                  result["final_state_sha256"] == h.digest(h.canonical(final)), "RECOVERY_INCOMPLETE")
    else:
        h.require(result["failed_assertion"] in {"STATE", "ACL", *[f"RCV{i:02}" for i in range(3, 14)]}, "RECOVERY_FAILURE_EVIDENCE")
