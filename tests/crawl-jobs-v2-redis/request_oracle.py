"""Private deterministic two-request fixture/wires/full-state oracle; no I/O.

All timestamps are supplied observations. Error replies have no operation time.
Only public_summary() is an export projection; fixtures, wires and states are private.
"""
import copy
import hashlib
from pathlib import Path

import claim_release as cr
import harness as h
import request_specs as spec
import resp


def compile_fixture(plan, inputs):
    h.require(type(plan) is dict and type(plan.get("inputs")) is dict and
              plan["inputs"].get("scenario") == spec.SCENARIO, "REQUEST_SCENARIO")
    f = cr.compile_fixture(plan, inputs)
    # Replace the unused B/fence-2 reservation position with A's second intent.
    # The old claim/recovery fixtures and their case-specific identities are intact.
    old = h.P + "reservation:" + f["identities"]["b"]["reservation_id"]
    a, decision = f["identities"]["a"], f["document_decision"]
    decision_sha = dict(f["source_fields"])["policy_decision_sha256"]
    policy = dict(f["initial_state"][f["base_key"]]["fields"])["crawl_policy_sha256"]
    suffix = [("request_ordinal", "2"), ("request_kind", "document"), ("target_url_id", decision["target_url_id"]),
        ("canonical_target_url", cr.URL), ("target_digest", decision["target_digest"]), ("crawl_policy_sha256", policy),
        ("policy_decision_sha256", decision_sha), *[(name, decision[name]) for name in
        ("group_id", "rate_scope_id", "global_scope_id", "group_scope_id", "origin_scope_id", "global_concurrency",
         "global_interval_ms", "group_concurrency", "group_interval_ms", "origin_concurrency", "origin_interval_ms")]]
    q = cr._framed_digest("mifolyo:request-reservation:v2", f["run_id"], f["job_id"], "1", a["lease_token"],
        *[value for name, value in suffix if name != "canonical_target_url"])
    f["identities"]["b"] = {"owner_id": a["owner_id"], "lease_token": a["lease_token"], "fence": "1",
        "reservation_id": q, "intent": dict(suffix), "intent_fields": suffix}
    del f["initial_state"][old]
    f["initial_state"][h.P + "reservation:" + q] = None
    f["key_inventory"] = sorted((set(f["key_inventory"]) - {old}) | {h.P + "reservation:" + q})
    f.update(case=spec.CASE, artifact_kind="private_offline_request_fixture",
        basis_compiler_sha256=f["compiler_sha256"], compiler_sha256=h.digest(Path(__file__).read_bytes()))
    h.require(len(f["key_inventory"]) == 58 and len(f["initial_state"]) == 57, "REQUEST_INVENTORY")
    return h.decode(h.canonical(f))


def validate_fixture(plan, fixture):
    h.require(type(fixture) is dict and type(fixture.get("inputs")) is dict, "REQUEST_FIXTURE")
    expected = compile_fixture(plan, fixture["inputs"])
    h.require(cr._bounded(fixture) == h.canonical(expected), "REQUEST_FIXTURE_MISMATCH")
    return expected


def wire_requests(plan, fixture, epoch):
    f = validate_fixture(plan, fixture)
    h.require(cr._hex(epoch, 32), "BOOT_EPOCH")
    setup = f["authority_setup"]
    gate = ["active", epoch, plan["identities"]["contract_sha256"], bytes.fromhex(plan["compatibility_marker"]["record_hex"]),
        bytes.fromhex(setup["stored_guard"]["record_hex"]), bytes.fromhex(setup["legacy_retirement"]["record_hex"]), b""]
    job = dict(f["initial_state"][f["job_key"]]["fields"])
    shas = {op: hashlib.sha1((h.ROOT / "services/spider/internal/database/crawljobsv2/lua" / (op.lower() + ".lua")).read_bytes()).hexdigest()
        for op in spec.SOURCES[1:]}
    wires = []
    for operation, label, _, wrong in spec.STEPS:
        identity = f["identities"][label]
        keys = [*f["work_keys"], h.P + "reservation:" + identity["reservation_id"],
            *[h.P + "rate:" + scope + suffix for scope in f["scope_ids"] for suffix in ("", ":active", ":pending", ":started")]]
        lease = [f["run_id"], f["job_id"], identity["owner_id"], f["inputs"]["wrong_token"] if wrong else identity["lease_token"], "1"]
        if operation == spec.CLAIM:
            semantic = [f["run_id"], f["job_id"], cr.URL, "0", "1", cr.GROUP, job["rate_scope_id"],
                job["group_scope_id"], job["initial_origin_scope_id"], job["policy_decision_sha256"], "0", "1", identity["owner_id"],
                identity["lease_token"], *[value for _, value in identity["intent_fields"]], identity["claim_transition_id"]]
        elif operation == spec.RESERVE:
            semantic = [*lease, *[value for _, value in identity["intent_fields"]]]
        elif operation == spec.MAINTAIN:
            keys, semantic = [*h.AUTH, h.P + "rate_scopes"], ["0"]
        else:
            semantic = [*lease, identity["reservation_id"]]
        wire = ("EVALSHA", shas[operation], str(len(keys)), *keys, *gate, *semantic)
        h.require(len(resp.encode(wire)) <= 65536, "REQUEST_WIRE_BOUND")
        wires.append(wire)
    return wires


def _reservation(f, label, now, expiry):
    identity = f["identities"][label]
    values = dict.fromkeys(cr.RESERVATION_FIELDS, "0")
    values.update(identity["intent"], protocol_version="2", reservation_id=identity["reservation_id"], run_id=f["run_id"],
        job_id=f["job_id"], owner_id=identity["owner_id"], lease_token=identity["lease_token"], lease_fence="1",
        state="pending", created_at_ms=str(now), expires_at_ms=str(expiry))
    return cr._hash([(name, values[name]) for name in cr.RESERVATION_FIELDS])


def _scopes(state, f, label, now, expiry, phase):
    identity = f["identities"][label]
    q = identity["reservation_id"]
    for kind, scope in zip(("global", "group", "origin"), f["scope_ids"]):
        key = h.P + "rate:" + scope
        if phase == "claim":
            values = dict(protocol_version="2", scope_id=scope, scope_kind=kind,
                scope_witness="global" if kind == "global" else identity["intent"]["rate_scope_id"] if kind == "group" else cr.ORIGIN,
                effective_concurrency="2" if kind == "global" else "1", effective_interval_ms="0", next_allowed_ms="0", last_started_at_ms="0",
                active_count="1", pending_count="1", started_count="0", updated_at_ms=str(now),
                concurrency_source_sha256=identity["intent"]["crawl_policy_sha256"], interval_source_sha256=identity["intent"]["crawl_policy_sha256"])
            state[key] = cr._hash([(name, values[name]) for name in cr.RATE_FIELDS])
        elif phase == "reserve":
            cr._change(state[key], active_count=1, pending_count=1, updated_at_ms=now)
        elif phase == "start":
            previous = dict(state[key]["fields"])
            cr._change(state[key], pending_count=0, started_count=1, last_started_at_ms=now, updated_at_ms=now,
                next_allowed_ms=previous["next_allowed_ms"] if kind == "global" else max(int(previous["next_allowed_ms"]), now))
        else:
            cr._change(state[key], active_count=0, started_count=0, updated_at_ms=now)
        state[key + ":active"] = None if phase == "finish" else cr._zset({q: expiry})
        state[key + ":pending"] = cr._zset({q: expiry}) if phase in ("claim", "reserve") else None
        state[key + ":started"] = cr._zset({q: expiry}) if phase == "start" else None
    state[h.P + "rate_scopes"] = cr._zset({scope: now for scope in f["scope_ids"]})


def expected_sequence(plan, fixture, observations):
    return _expected_validated(validate_fixture(plan, fixture), observations)


def _expected_validated(f, observations):
    """Project only an observed prefix; never manufacture future reply times."""
    h.require(type(observations) is list and 0 <= len(observations) <= len(spec.STEPS), "REQUEST_OBSERVATIONS")
    state, result = copy.deepcopy(f["initial_state"]), []
    base, job = f["base_key"], f["job_key"]
    minimum = f["inputs"]["redis_time_ms"]
    for index, observation in enumerate(observations):
        h.exact(observation, {"started_at_ms", "now_ms", "finished_at_ms"})
        before, now, after = [observation[name] for name in ("started_at_ms", "now_ms", "finished_at_ms")]
        h.require(type(before) is type(after) is int and minimum <= before <= after <= f["inputs"]["redis_time_ms"] + cr.CASE_MS,
            "REQUEST_OBSERVATION_TIME")
        h.require(after - observations[0]["started_at_ms"] <= cr.STAGE_MS, "REQUEST_MEASURE_SPAN")
        minimum = after
        operation, label, status, _ = spec.STEPS[index]
        identity = f["identities"][label]
        q, qkey = identity["reservation_id"], h.P + "reservation:" + identity["reservation_id"]
        if index in spec.ERRORS:
            h.require(now is None, "REQUEST_ERROR_TIME")
            reply = {"error": status}
        else:
            h.require(type(now) is int and before <= now <= after, "REQUEST_REPLY_TIME")
            if index == 0:
                expiry = now + cr.LEASE_MS
                cr._change(state[base], claims_total=1, reservation_creations_total=1, pending_request_reservations=1,
                    last_activity_at_ms=now, last_execution_at_ms=now)
                cr._change(state[base + ":group_pending"], **{cr.GROUP: 1})
                cr._change(state[job], state="leased", claim_count=1, lease_fence=1, lease_owner=identity["owner_id"], lease_token=identity["lease_token"],
                    lease_started_at_ms=now, lease_expires_at_ms=expiry, active_reservation_id=q, next_request_ordinal=2, updated_at_ms=now,
                    last_transition_id=identity["claim_transition_id"], last_transition_status="CLAIMED")
                state[base + ":ready"] = state[base + ":ready_at"] = None
                state[base + ":leased"], state[base + ":leased_at"] = cr._zset({f["job_id"]: expiry}), cr._zset({f["job_id"]: now})
                state[h.P + "active_leases"] = cr._zset({f["run_id"] + ":" + f["job_id"]: expiry})
                state[qkey] = _reservation(f, label, now, expiry)
                _scopes(state, f, label, now, expiry, "claim")
            elif index == 11:
                expiry = int(dict(state[job]["fields"])["lease_expires_at_ms"])
                cr._change(state[base], reservation_creations_total=2, pending_request_reservations=1, last_activity_at_ms=now)
                cr._change(state[base + ":group_pending"], **{cr.GROUP: 1})
                cr._change(state[job], active_reservation_id=q, next_request_ordinal=3, updated_at_ms=now)
                state[qkey] = _reservation(f, label, now, expiry)
                _scopes(state, f, label, now, expiry, "reserve")
            elif index in (5, 14):
                count = 1 if index == 5 else 2
                expiry = int(dict(state[qkey]["fields"])["expires_at_ms"])
                cr._change(state[base], pending_request_reservations=0, started_request_reservations=1, request_starts=count,
                    last_activity_at_ms=now, last_execution_at_ms=now, last_request_started_at_ms=now)
                for suffix, amount in (("group_pending", 0), ("group_active_started", 1), ("group_started", count)):
                    cr._change(state[base + ":" + suffix], **{cr.GROUP: amount})
                cr._change(state[job], request_starts=count, delivery_attempts=1, lease_delivery_started=1, last_request_started_at_ms=now, updated_at_ms=now)
                cr._change(state[qkey], state="started", started_at_ms=now, delivery_attempts_after_start=1,
                    job_starts_after_start=count, run_starts_after_start=count, group_starts_after_start=count)
                if index == 5:
                    state[h.P + "first_request_start"] = cr._hash([("protocol_version", "2"), ("run_id", f["run_id"]),
                        ("job_id", f["job_id"]), ("lease_fence", "1"), ("started_at_ms", str(now))])
                else:
                    cr._change(state[job], last_document_request_started_at_ms=now, last_document_request_fence=1,
                        last_document_target_url_id=identity["intent"]["target_url_id"], last_document_target_url=cr.URL,
                        last_document_target_digest=identity["intent"]["target_digest"])
                _scopes(state, f, label, now, expiry, "start")
            elif index in (7, 18):
                expiry = int(dict(state[qkey]["fields"])["expires_at_ms"])
                cr._change(state[base], started_request_reservations=0, last_activity_at_ms=now)
                cr._change(state[base + ":group_active_started"], **{cr.GROUP: 0})
                cr._change(state[job], active_reservation_id="", updated_at_ms=now)
                cr._change(state[qkey], state="finished", terminal_at_ms=now)
                state[qkey]["expires_at_ms"] = now + cr.TOMBSTONE_MS
                _scopes(state, f, label, now, expiry, "finish")
            reservation = dict(state[qkey]["fields"]) if state[qkey] is not None else {}
            if operation == spec.CLAIM:
                reply = [status, str(now), "1", reservation["expires_at_ms"], q, reservation["expires_at_ms"]]
            elif operation == spec.RESERVE:
                reply = [status, str(now), q, reservation["expires_at_ms"]]
            elif operation == spec.START:
                reply = [status, str(now), q, reservation["started_at_ms"], reservation["delivery_attempts_after_start"],
                    reservation["job_starts_after_start"], reservation["run_starts_after_start"], reservation["group_starts_after_start"],
                    "1" if reservation["state"] == "started" else "0"]
            elif operation == spec.FINISH:
                reply = [status, str(now), q]
            else:
                reply = [status, str(now), "3", "0"]
        h.require(after < int(dict(state[job]["fields"])["lease_expires_at_ms"]), "REQUEST_LEASE_SPAN")
        result.append({"assertion_id": f"REQ{index + 1:02}", "operation": operation, "reply": reply, "state": copy.deepcopy(state)})
    return result


def public_summary(plan, fixture):
    return _public_summary_validated(validate_fixture(plan, fixture))


def _public_summary_validated(f):
    return {"case": spec.CASE, "purpose": "conformance_only", "execution_authorized": False, "release_eligible": False,
        "measurement_status": "not_measured", "fixture_sha256": h.digest(h.canonical(f)), "compiler_sha256": f["compiler_sha256"],
        "basis_compiler_sha256": f["basis_compiler_sha256"], "plan_sha256": f["plan_sha256"], "possible_keys": 58, "fixture_owned_keys": 57}
