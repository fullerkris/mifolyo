"""Fixed two-run execution, complete bounded reads and redacted shared receipts."""
import re

import bounded_state as state
import claim_executor as claim
import claim_release as cr
import harness as h
import negative_executor as negative
import runtime_case as case
import shared_capacity as oracle
import shared_capacity_specs as spec
from resp import RedisError


class SharedFailure(h.InvalidArtifact):
    def __init__(self, result):
        self.result = result
        super().__init__("SHARED_STAGE_FAILED")


def fixture(request, at):
    _case(request)
    return case.fixture(request["plan"], request["fixture_id"], request["claim_material"], at)


def _case(request):
    selected = case.case_for_plan(request["plan"])
    h.require(selected in spec.CASES, "SHARED_CASE")
    return selected


def with_boot(expected, boot):
    h.require(type(expected) is dict and len(expected) == 89 and h.AUTH[0] not in expected, "SHARED_STATE_KEYS")
    h.exact(boot, set(case.BOOT_FIELDS))
    return state.with_boot(expected, boot)


def snapshot(client, expected, boot):
    return state.snapshot(client, with_boot(expected, boot))


def counters(observed, f):
    result = {label + "." + key: value for label in ("a", "b")
        for key, value in claim.counters(observed, f["actors"][label]).items() if not key.startswith("scope.")}
    # Count the two shared scopes once, not once per actor.
    for label, scope in zip(("global", "group", "origin_a", "origin_b"), f["scope_ids"]):
        row = observed[h.P + "rate:" + scope]
        values = dict(row["fields"]) if row is not None else dict.fromkeys(claim.SCOPE_COUNTERS, "0")
        result.update({"scope." + label + "." + name: claim._number(values[name].encode()) for name in claim.SCOPE_COUNTERS})
    return result


def projection(observed, f):
    jobs, expiries = [], []
    for label in ("a", "b"):
        actor = f["actors"][label]
        job = dict(observed[actor["job_key"]]["fields"])
        jobs.append({"actor": label, "state": job["state"],
            **{name: int(job[name]) for name in ("lease_started_at_ms", "lease_expires_at_ms", "lease_delivery_started", "last_request_started_at_ms",
                "last_document_request_started_at_ms", "last_document_request_fence")},
            "active_reservation_sha256": h.digest(job["active_reservation_id"].encode()),
            "document_identity_sha256": h.digest(h.canonical([job[name] for name in
                ("last_document_target_url_id", "last_document_target_url", "last_document_target_digest")]))})
        key = h.P + "reservation:" + actor["identity"]["reservation_id"]
        row = observed[key]
        if row is not None:
            values = dict(row["fields"])
            expiries.append({"actor": label, "reference_sha256": h.digest(key.encode()), "state": values["state"],
                "expires_at_ms": row["expires_at_ms"], "logical_expires_at_ms": int(values["expires_at_ms"])})
    rates = []
    for label, scope in zip(("global", "group", "origin_a", "origin_b"), f["scope_ids"]):
        row = observed[h.P + "rate:" + scope]
        fields = ("active_count", "pending_count", "started_count", "effective_concurrency", "effective_interval_ms", "next_allowed_ms", "last_started_at_ms", "updated_at_ms")
        rates.append({"kind": label, "present": row is not None,
            **{name: int(dict(row["fields"])[name]) if row is not None else 0 for name in fields}})
    first = observed[h.P + "first_request_start"]
    return {"state_sha256": h.digest(h.canonical(observed)), "counters": counters(observed, f), "jobs": jobs,
        "reservation_expiries": expiries, "rate_scopes": rates,
        "history": {"first_request_start_sha256": h.digest(h.canonical(first)),
            "first_started_at_ms": 0 if first is None else int(dict(first["fields"])["started_at_ms"])}}


def validate_resume(result, request, previous=None):
    selected = _case(request)
    h.exact(result, {"probe_evidence", "probe_evidence_sha256", "boot_epoch", "boot_record", "setup_time_ms", "setup_time_observed",
        "fixture_summary", "setup_state_sha256", "setup_counters", "early_revocation"})
    proof, plan, fid = result["probe_evidence"], request["plan"], request["fixture_id"]
    h.exact(proof, {"case", "fixture_id", "plan_sha256", "old_run_id", "new_run_id", "acknowledged_probe_sha256", "verified_at_ms", "acknowledged_loss_bound"})
    probe = "m4-probe:" + fid + ":" + h.digest(h.canonical(plan))
    h.require(proof["case"] == selected and proof["fixture_id"] == fid and proof["plan_sha256"] == h.digest(h.canonical(plan)) and
        proof["acknowledged_probe_sha256"] == h.digest(probe.encode()) and result["probe_evidence_sha256"] == h.digest(h.canonical(proof)) and
        type(proof["verified_at_ms"]) is int and 0 < proof["verified_at_ms"] <= h.MAX_EXACT and
        type(proof["acknowledged_loss_bound"]) is int and proof["acknowledged_loss_bound"] == 0 and
        all(type(proof[name]) is str and re.fullmatch(r"[0-9a-f]{40}", proof[name]) for name in ("old_run_id", "new_run_id")) and
        proof["old_run_id"] != proof["new_run_id"], "SHARED_PROBE_BINDING")
    if previous is not None:
        h.require(previous["acknowledged"] is True and previous["value"] == probe and previous["value_sha256"] == proof["acknowledged_probe_sha256"] and
            previous["old_run_id"] == proof["old_run_id"] and type(previous["at_ms"]) is int and 0 < previous["at_ms"] <= proof["verified_at_ms"], "SHARED_PROBE_HISTORY")
    at, boot = result["setup_time_ms"], result["boot_record"]
    h.require(result["setup_time_observed"] is True and type(at) is int and proof["verified_at_ms"] <= at <= h.MAX_EXACT and
        result["boot_epoch"] == h.digest((fid + ":boot").encode())[:32], "SHARED_SETUP_TIME")
    h.exact(boot, set(case.BOOT_FIELDS))
    h.require(type(boot["approved_at_ms"]) is str and re.fullmatch(r"[1-9][0-9]{0,15}", boot["approved_at_ms"]) and
        proof["verified_at_ms"] <= int(boot["approved_at_ms"]) <= at, "SHARED_BOOT_TIME")
    wanted = dict(zip(case.BOOT_FIELDS, ("1", "approved", proof["new_run_id"], result["boot_epoch"], boot["approved_at_ms"], "", "", "initial", "",
        result["probe_evidence_sha256"], str(proof["verified_at_ms"]), "0")))
    h.require(boot == wanted, "SHARED_BOOT_RECORD")
    f = fixture(request, at)
    h.require(cr._bounded(result["fixture_summary"]) == h.canonical(oracle._public_summary_validated(f)), "SHARED_FIXTURE_BINDING")
    initial = with_boot(f["initial_state"], boot)
    h.require(result["setup_state_sha256"] == h.digest(h.canonical(initial)) and
        cr._bounded(result["setup_counters"]) == h.canonical(counters(initial, f)), "SHARED_SETUP_BINDING")
    case.validate_revocation(result["early_revocation"], case.EARLY_ROLES)
    return f


def resume(request, setup, current, proof, epoch, api):
    selected = _case(request)
    credentials, plan = request["credentials"], request["plan"]
    proof_sha = h.digest(h.canonical(proof))
    with api.connect("boot", credentials) as client:
        wire = case.boot_request(current["run_id"], epoch, proof_sha, proof["verified_at_ms"])
        before = api.clock(setup)
        first = client.call(*wire)
        now = negative.reply_time(first, before, api.clock(setup))
        negative.exact_reply(first, ["OK", str(now), epoch])
        boot = dict(zip(case.BOOT_FIELDS, ("1", "approved", current["run_id"], epoch, str(now), "", "", "initial", "", proof_sha, str(proof["verified_at_ms"]), "0")))
        h.require(api.read_hash(setup, h.AUTH[0], case.BOOT_FIELDS) == boot, "SHARED_BOOT_STATE")
        replay = client.call(*wire)
        replay_at = negative.reply_time(replay, now, api.clock(setup))
        negative.exact_reply(replay, ["EXISTS_IDENTICAL", str(replay_at), epoch])
        h.require(api.read_hash(setup, h.AUTH[0], case.BOOT_FIELDS) == boot, "SHARED_BOOT_REPLAY")
    at = api.clock(setup)
    f, binding = fixture(request, at), fixture(request, 1000)
    h.require(case.acl_rules(selected, f, plan) == case.acl_rules(selected, binding, plan), "SHARED_ACL_BINDING")
    snapshot(setup, dict.fromkeys(f["initial_state"]), boot)
    for key, row in sorted(f["initial_state"].items()):
        if row is None:
            continue
        if row["type"] == "hash":
            h.require(setup.call("HSET", key, *[value for pair in row["fields"] for value in pair]) == len(row["fields"]), "SHARED_SETUP_HASH")
        elif row["type"] == "string":
            h.require(setup.call("SET", key, row["value"]) == b"OK", "SHARED_SETUP_STRING")
        elif row["type"] == "set":
            h.require(setup.call("SADD", key, *row["members"]) == len(row["members"]), "SHARED_SETUP_SET")
        else:
            h.require(row["type"] == "zset", "SHARED_SETUP_KIND")
            h.require(setup.call("ZADD", key, *[value for member, score in row["members"] for value in (score, member)]) == len(row["members"]), "SHARED_SETUP_ZSET")
    actual = snapshot(setup, f["initial_state"], boot)
    result = {"probe_evidence": proof, "probe_evidence_sha256": proof_sha, "boot_epoch": epoch, "boot_record": boot,
        "setup_time_ms": at, "setup_time_observed": True, "fixture_summary": oracle._public_summary_validated(f),
        "setup_state_sha256": h.digest(h.canonical(actual)), "setup_counters": counters(actual, f),
        "early_revocation": api.revoke(credentials, case.EARLY_ROLES)}
    validate_resume(result, request, request["previous"])
    return result


def _row(index, observation, expected, observed, f, before_counts):
    public = projection(observed, f)
    counts = public.pop("counters")
    operation, actor, status, _ = spec.SEQUENCES[spec.RUNTIME_TRACES[f["case"]]][index]
    return {"sequence": index, "assertion_id": expected["assertion_id"], "operation": operation, "actor": actor,
        "response_kind": "error" if status.startswith("CRAWL_V2_") else "reply", "response_status": status, **observation,
        "response_sha256": h.digest(h.canonical(expected["reply"])), **public,
        "capacity_denial": {"blocking_scope_kind": "group", "active_count": 1, "effective_concurrency": 1, "after_io": 0} if status == "CAPACITY_BLOCKED" else None,
        "counters": {"before": before_counts, "after": counts, "delta": {key: counts[key] - before_counts[key] for key in counts}}}


def measure(request, api):
    selected = _case(request)
    trace = spec.RUNTIME_TRACES[selected]
    steps, prefix = spec.SEQUENCES[trace], spec.ASSERTION_PREFIXES[selected]
    resumed = request["previous"]
    f = validate_resume(resumed, request)
    boot = resumed["boot_record"]
    result = {"scope": selected, "fixture_sha256": resumed["fixture_summary"]["fixture_sha256"], "steps": [], "acl_negatives": [], "clock_reference_ms": None}
    assertion = "STATE"
    try:
        with api.connect("observer", request["credentials"]) as observer, api.connect("ledger", request["credentials"]) as ledger:
            h.require(api.server(observer)["run_id"] == resumed["probe_evidence"]["new_run_id"], "SHARED_BOOT_CHANGED")
            actual = snapshot(observer, f["initial_state"], boot)
            h.require(h.digest(h.canonical(actual)) == resumed["setup_state_sha256"], "SHARED_SETUP_CHANGED")
            counts, observations = counters(actual, f), []
            result["clock_reference_ms"] = api.clock(observer)
            # The source-bound case selects its sole runtime trace. The request
            # accepts no trace/profile selector; reversed order stays offline.
            for index, wire in enumerate(oracle.wire_requests(request["plan"], f, resumed["boot_epoch"], trace)):
                assertion = prefix + f"{index + 1:02}"
                before, now = api.clock(observer), None
                is_error = steps[index][2].startswith("CRAWL_V2_")
                if is_error:
                    try:
                        ledger.call(*wire)
                    except RedisError as error:
                        h.require(error.code == steps[index][2], "SHARED_REJECTION")
                    else:
                        raise h.InvalidArtifact("SHARED_UNEXPECTED_SUCCESS")
                else:
                    reply = ledger.call(*wire)
                    h.require(type(reply) is list and 3 <= len(reply) <= 9 and all(type(value) is bytes for value in reply), "SHARED_REPLY")
                    now = claim._number(reply[1])
                observation = {"started_at_ms": before, "now_ms": now, "finished_at_ms": api.clock(observer)}
                candidate = [*observations, observation]
                expected = oracle._expected_validated(f, candidate, trace)[index]
                if not is_error:
                    negative.exact_reply(reply, expected["reply"])
                actual = snapshot(observer, expected["state"], boot)
                row = _row(index, observation, expected, actual, f, counts)
                result["steps"].append(row)
                observations, counts = candidate, row["counters"]["after"]
            assertion = "ACL"
            for authority, command in claim.acl_probes(f["actors"]["a"]):
                try:
                    ledger.call(*command)
                except RedisError as error:
                    h.require(error.code == "NOPERM", "SHARED_ACL_DENIAL")
                else:
                    raise h.InvalidArtifact("SHARED_ACL_ESCALATION")
                actual = snapshot(observer, expected["state"], boot)
                result["acl_negatives"].append({"authority_role": authority, "command": command[0], "result": "NOPERM", "state_sha256": h.digest(h.canonical(actual))})
            result.update(final_state_sha256=h.digest(h.canonical(actual)), finished_at_ms=api.clock(observer))
            validate_stage_result("measure", result, request)
        return result
    except Exception:
        result.pop("final_state_sha256", None)
        result.pop("finished_at_ms", None)
        result["failed_assertion"] = assertion
        raise SharedFailure(result) from None


def validate_stage_result(stage, result, request, successful=True):
    selected = _case(request)
    trace = spec.RUNTIME_TRACES[selected]
    total, prefix = len(spec.SEQUENCES[trace]), spec.ASSERTION_PREFIXES[selected]
    if stage == "resume":
        h.require(successful, "SHARED_RESUME_FAILED")
        validate_resume(result, request, request["previous"])
        return
    if stage != "measure":
        h.require(successful, "SHARED_FAILURE_STAGE")
        claim.validate_stage_result(stage, result, request)
        if stage == "init":
            f = fixture(request, 1000)
            h.require(result["empty_volumes_verified"] is True and result["config_sha256"] == request["plan"]["redis_config"]["sha256"] and
                result["acl_file_sha256"] == h.digest(case.acl_file(request["credentials"], selected, f, request["plan"])), "SHARED_INIT_BINDING")
        elif stage == "ready":
            h.require(type(result["run_id"]) is str and re.fullmatch(r"[0-9a-f]{40}", result["run_id"]), "SHARED_READY_BINDING")
        elif stage == "revoke":
            case.validate_revocation(result["revocation"], case.roles(selected))
        return
    resumed = request["previous"]
    f = validate_resume(resumed, request)
    h.exact(result, {"scope", "fixture_sha256", "steps", "acl_negatives", "clock_reference_ms"} |
        ({"final_state_sha256", "finished_at_ms"} if successful else {"failed_assertion"}))
    h.require(result["scope"] == selected and result["fixture_sha256"] == resumed["fixture_summary"]["fixture_sha256"] and
        type(result["steps"]) is list and len(result["steps"]) <= total and type(result["acl_negatives"]) is list and len(result["acl_negatives"]) <= 46, "SHARED_EVIDENCE_BOUND")
    reference = result["clock_reference_ms"]
    h.require((reference is None and not result["steps"] and not successful) or
        (type(reference) is int and resumed["setup_time_ms"] <= reference <= resumed["setup_time_ms"] + cr.CASE_MS), "SHARED_CLOCK_REFERENCE")
    observations = []
    for row in result["steps"]:
        h.require(type(row) is dict and all(name in row for name in ("started_at_ms", "now_ms", "finished_at_ms")), "SHARED_STEP")
        observations.append({name: row[name] for name in ("started_at_ms", "now_ms", "finished_at_ms")})
    projected = oracle._expected_validated(f, observations, trace)
    boot = resumed["boot_record"]
    counts = counters(with_boot(f["initial_state"], boot), f)
    for index, (row, expected) in enumerate(zip(result["steps"], projected)):
        wanted = _row(index, observations[index], expected, with_boot(expected["state"], boot), f, counts)
        h.require(cr._bounded(row) == h.canonical(wanted), "SHARED_ROW_BINDING")
        h.require(reference <= row["started_at_ms"] <= row["finished_at_ms"] <= reference + cr.STAGE_MS, "SHARED_MEASURE_TIME")
        counts = wanted["counters"]["after"]
    probes = claim.acl_probes(f["actors"]["a"])
    final_sha = h.digest(h.canonical(with_boot(projected[-1]["state"], boot))) if len(projected) == total else None
    for index, row in enumerate(result["acl_negatives"]):
        wanted = {"authority_role": probes[index][0], "command": probes[index][1][0], "result": "NOPERM", "state_sha256": final_sha}
        h.require(final_sha is not None and cr._bounded(row) == h.canonical(wanted), "SHARED_ACL_EVIDENCE")
    if successful:
        h.require(len(projected) == total and len(result["acl_negatives"]) == 46 and result["final_state_sha256"] == final_sha and
            type(result["finished_at_ms"]) is int and observations[-1]["finished_at_ms"] <= result["finished_at_ms"] <= reference + cr.STAGE_MS, "SHARED_INCOMPLETE")
    else:
        failed, completed = result["failed_assertion"], len(projected)
        h.require(type(failed) is str, "SHARED_FAILURE_EVIDENCE")
        if failed == "STATE":
            h.require(completed == 0 and not result["acl_negatives"], "SHARED_FAILURE_EVIDENCE")
        elif failed == "ACL":
            h.require(completed == total, "SHARED_FAILURE_EVIDENCE")
        else:
            h.require(reference is not None and completed < total and failed == prefix + f"{completed + 1:02}" and not result["acl_negatives"], "SHARED_FAILURE_EVIDENCE")
