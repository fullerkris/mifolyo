"""Fixed positive-rate phases with observed Redis-time waiting and bound prefixes."""
import copy

import claim_executor as claim
import claim_release as cr
import harness as h
import negative_executor as negative
import rate_specs as spec
import request_executor as requests
import request_oracle as oracle
from resp import RedisError


class RateFailure(h.InvalidArtifact):
    def __init__(self, result):
        self.result = result
        super().__init__("RATE_STAGE_FAILED")


def deadline(prefix):
    return prefix["steps"][5]["now_ms"] + spec.INTERVAL_MS


def lease_expiry(prefix):
    return prefix["steps"][0]["now_ms"] + cr.LEASE_MS


def _observations(rows):
    result = []
    for row in rows:
        h.require(type(row) is dict and all(name in row for name in ("started_at_ms", "now_ms", "finished_at_ms")), "RATE_STEP")
        result.append({name: row[name] for name in ("started_at_ms", "now_ms", "finished_at_ms")})
    return result


def validate_result(result, request, resumed, f, *, after=False, successful=True):
    fields = {"scope", "fixture_sha256", "resume_sha256", "pre_rate_sha256", "wait_sha256", "clock_reference_ms", "steps", "acl_negatives"}
    h.exact(result, fields | ({"final_state_sha256", "finished_at_ms"} if successful else {"failed_assertion"}))
    h.require(result["scope"] == spec.CASE and result["fixture_sha256"] == resumed["fixture_summary"]["fixture_sha256"] and
        result["resume_sha256"] == h.digest(h.canonical(resumed)), "RATE_FIXTURE_BINDING")
    limit = len(spec.STEPS) if after else spec.PREFIX_STEPS
    rows, negatives, reference = result["steps"], result["acl_negatives"], result["clock_reference_ms"]
    h.require(type(rows) is list and len(rows) <= limit and type(negatives) is list and len(negatives) <= 46 and
        (after or not negatives), "RATE_RESULT_BOUND")
    h.require((reference is None and not rows and not successful) or (type(reference) is int and
        resumed["setup_time_ms"] <= reference <= resumed["setup_time_ms"] + cr.CASE_MS), "RATE_REFERENCE")
    if after:
        prior = request["previous"]
        h.require(result["pre_rate_sha256"] == h.digest(h.canonical(prior["prefix"])) and
            result["wait_sha256"] == h.digest(h.canonical(prior["wait"])) and reference == prior["prefix"]["clock_reference_ms"] and
            len(rows) >= spec.PREFIX_STEPS and cr._bounded({"steps": rows[:spec.PREFIX_STEPS]}) == h.canonical({"steps": prior["prefix"]["steps"]}), "RATE_PREFIX_BINDING")
    else:
        h.require(result["pre_rate_sha256"] is result["wait_sha256"] is None, "RATE_PREFIX_BINDING")
    observations = _observations(rows)
    projected = oracle._expected_validated(f, observations)
    counts = claim.counters(requests.with_boot(f["initial_state"], resumed["boot_record"]), f)
    for index, (row, expected) in enumerate(zip(rows, projected)):
        wanted = requests._row(index, observations[index], expected, requests.with_boot(expected["state"], resumed["boot_record"]), f, counts)
        h.require(cr._bounded(row) == h.canonical(wanted), "RATE_ROW_BINDING")
        h.require(reference <= row["started_at_ms"] <= row["finished_at_ms"] <= reference + cr.STAGE_MS, "RATE_MEASURE_TIME")
        counts = wanted["counters"]["after"]
    if after and len(rows) > spec.PREFIX_STEPS:
        h.require(rows[spec.PREFIX_STEPS]["started_at_ms"] >= request["previous"]["wait"]["observations"][-1]["now_ms"], "RATE_WAIT_DISPATCH_ORDER")
    final_sha = h.digest(h.canonical(requests.with_boot(projected[-1]["state"], resumed["boot_record"]))) if projected else None
    probes = claim.acl_probes(f)
    for index, row in enumerate(negatives):
        wanted = {"authority_role": probes[index][0], "command": probes[index][1][0], "result": "NOPERM", "state_sha256": final_sha}
        h.require(after and len(rows) == len(spec.STEPS) and cr._bounded(row) == h.canonical(wanted), "RATE_ACL_EVIDENCE")
    if successful:
        h.require(len(rows) == limit and len(negatives) == (46 if after else 0) and result["final_state_sha256"] == final_sha and
            type(result["finished_at_ms"]) is int and rows[-1]["finished_at_ms"] <= result["finished_at_ms"] <= reference + cr.STAGE_MS and
            result["finished_at_ms"] < lease_expiry(result), "RATE_INCOMPLETE")
    else:
        failed, completed = result["failed_assertion"], len(rows)
        h.require(type(failed) is str, "RATE_FAILURE_EVIDENCE")
        if failed == "STATE":
            h.require(completed == (spec.PREFIX_STEPS if after else 0) and not negatives, "RATE_FAILURE_EVIDENCE")
        elif failed == "FINAL":
            h.require(completed == limit and len(negatives) == (46 if after else 0), "RATE_FAILURE_EVIDENCE")
        elif failed == "ACL":
            h.require(after and completed == len(spec.STEPS), "RATE_FAILURE_EVIDENCE")
        else:
            h.require(reference is not None and completed < limit and failed == f"RATE{completed + 1:02}" and not negatives, "RATE_FAILURE_EVIDENCE")
    return projected


def _context(request, after=False):
    prior = request["previous"]
    h.exact(prior, {"resume", "prefix", "wait"} if after else {"resume", "prefix"})
    resumed = prior["resume"]
    f = requests.validate_resume(resumed, request)
    validate_result(prior["prefix"], request, resumed, f)
    if after:
        validate_wait(prior["wait"], resumed, prior["prefix"])
    return prior, f


def validate_clock(result, resumed, prefix):
    h.exact(result, {"scope", "run_id", "fixture_sha256", "prefix_sha256", "now_ms", "deadline_ms", "lease_expires_at_ms"})
    h.require(result["scope"] == spec.CASE and result["run_id"] == resumed["probe_evidence"]["new_run_id"] and
        result["fixture_sha256"] == resumed["fixture_summary"]["fixture_sha256"] and result["prefix_sha256"] == h.digest(h.canonical(prefix)) and
        type(result["now_ms"]) is int and prefix["finished_at_ms"] <= result["now_ms"] < lease_expiry(prefix) and
        result["now_ms"] <= prefix["clock_reference_ms"] + cr.STAGE_MS and
        type(result["deadline_ms"]) is type(result["lease_expires_at_ms"]) is int and
        result["deadline_ms"] == deadline(prefix) and result["lease_expires_at_ms"] == lease_expiry(prefix), "RATE_CLOCK_EVIDENCE")


def validate_wait(wait, resumed, prefix):
    h.exact(wait, {"observations"})
    values = wait["observations"]
    h.require(type(values) is list and 1 <= len(values) <= spec.MAX_CLOCK_OBSERVATIONS, "RATE_WAIT_BOUND")
    minimum = prefix["finished_at_ms"]
    for index, row in enumerate(values):
        validate_clock(row, resumed, prefix)
        h.require(row["now_ms"] >= minimum and (index == len(values) - 1 or row["now_ms"] < deadline(prefix)), "RATE_WAIT_ORDER")
        minimum = row["now_ms"]
    h.require(minimum >= deadline(prefix), "RATE_WAIT_INCOMPLETE")


def rate_clock(request, api):
    prior, _ = _context(request)
    resumed, prefix = prior["resume"], prior["prefix"]
    with api.connect("observer", request["credentials"]) as observer:
        run_id, now = api.server(observer)["run_id"], api.clock(observer)
    result = {"scope": spec.CASE, "run_id": run_id, "fixture_sha256": resumed["fixture_summary"]["fixture_sha256"],
        "prefix_sha256": h.digest(h.canonical(prefix)), "now_ms": now, "deadline_ms": deadline(prefix), "lease_expires_at_ms": lease_expiry(prefix)}
    validate_clock(result, resumed, prefix)
    return result


def rate_before(request, api):
    resumed = request["previous"]
    f = requests.validate_resume(resumed, request)
    return _measure(request, api, resumed, f)


def rate_after(request, api):
    prior, f = _context(request, after=True)
    return _measure(request, api, prior["resume"], f, after=True)


def _measure(request, api, resumed, f, after=False):
    prior = request["previous"] if after else None
    rows = copy.deepcopy(prior["prefix"]["steps"]) if after else []
    result = {"scope": spec.CASE, "fixture_sha256": resumed["fixture_summary"]["fixture_sha256"], "resume_sha256": h.digest(h.canonical(resumed)),
        "pre_rate_sha256": h.digest(h.canonical(prior["prefix"])) if after else None,
        "wait_sha256": h.digest(h.canonical(prior["wait"])) if after else None,
        "clock_reference_ms": prior["prefix"]["clock_reference_ms"] if after else None, "steps": rows, "acl_negatives": []}
    assertion = "STATE"
    try:
        with api.connect("observer", request["credentials"]) as observer, api.connect("ledger", request["credentials"]) as ledger:
            h.require(api.server(observer)["run_id"] == resumed["probe_evidence"]["new_run_id"], "RATE_BOOT_CHANGED")
            observations = _observations(rows)
            state = oracle._expected_validated(f, observations)[-1]["state"] if after else f["initial_state"]
            actual = requests.snapshot(observer, state, resumed["boot_record"])
            expected_sha = prior["prefix"]["final_state_sha256"] if after else resumed["setup_state_sha256"]
            h.require(h.digest(h.canonical(actual)) == expected_sha, "RATE_STATE_CHANGED")
            counts = claim.counters(actual, f)
            if not after:
                result["clock_reference_ms"] = api.clock(observer)
            wires = oracle.wire_requests(request["plan"], f, resumed["boot_epoch"])
            start, stop = (spec.PREFIX_STEPS, len(spec.STEPS)) if after else (0, spec.PREFIX_STEPS)
            for index in range(start, stop):
                assertion = f"RATE{index + 1:02}"
                before, now = api.clock(observer), None
                if after:
                    h.require(before >= prior["wait"]["observations"][-1]["now_ms"], "RATE_NOT_DUE")
                if index in spec.ERRORS:
                    try:
                        ledger.call(*wires[index])
                    except RedisError as error:
                        h.require(error.code == spec.STEPS[index][2], "RATE_REJECTION")
                    else:
                        raise h.InvalidArtifact("RATE_UNEXPECTED_SUCCESS")
                else:
                    reply = ledger.call(*wires[index])
                    h.require(type(reply) is list and 3 <= len(reply) <= 9 and all(type(value) is bytes for value in reply), "RATE_REPLY")
                    now = claim._number(reply[1])
                observation = {"started_at_ms": before, "now_ms": now, "finished_at_ms": api.clock(observer)}
                candidate = [*observations, observation]
                expected = oracle._expected_validated(f, candidate)[index]
                if index not in spec.ERRORS:
                    negative.exact_reply(reply, expected["reply"])
                actual = requests.snapshot(observer, expected["state"], resumed["boot_record"])
                row = requests._row(index, observation, expected, actual, f, counts)
                rows.append(row)
                observations, counts = candidate, row["counters"]["after"]
            if after:
                assertion = "ACL"
                for authority, command in claim.acl_probes(f):
                    try:
                        ledger.call(*command)
                    except RedisError as error:
                        h.require(error.code == "NOPERM", "RATE_ACL_DENIAL")
                    else:
                        raise h.InvalidArtifact("RATE_ACL_ESCALATION")
                    actual = requests.snapshot(observer, expected["state"], resumed["boot_record"])
                    result["acl_negatives"].append({"authority_role": authority, "command": command[0], "result": "NOPERM", "state_sha256": h.digest(h.canonical(actual))})
            assertion = "FINAL"
            result.update(final_state_sha256=h.digest(h.canonical(actual)), finished_at_ms=api.clock(observer))
            validate_result(result, request, resumed, f, after=after)
        return result
    except Exception:
        result.pop("final_state_sha256", None)
        result.pop("finished_at_ms", None)
        result["failed_assertion"] = assertion
        raise RateFailure(result) from None


def validate_stage_result(stage, result, request, successful=True):
    if stage not in spec.PHASES:
        requests.validate_stage_result(stage, result, request, successful=successful)
    elif stage == "rate_before":
        resumed = request["previous"]
        f = requests.validate_resume(resumed, request)
        validate_result(result, request, resumed, f, successful=successful)
    else:
        h.require(stage != "rate_clock" or successful, "RATE_CLOCK_FAILURE")
        prior, f = _context(request, after=stage == "rate_after")
        if stage == "rate_clock":
            validate_clock(result, prior["resume"], prior["prefix"])
        else:
            validate_result(result, request, prior["resume"], f, after=True, successful=successful)
