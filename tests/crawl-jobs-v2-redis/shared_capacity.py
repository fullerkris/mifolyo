"""Private offline combined-state oracle for two runs sharing group capacity.

Captured identities/time only; no connection, clock, randomness or execution API.
Synthetic test vectors may expose fixed test material. Public summaries never do.
Runtime callers use only the fixed finish trace; other traces are offline controls.
"""
import copy
import hashlib
from pathlib import Path

import claim_release as cr
import harness as h
import resp
import shared_capacity_specs as spec


def sequence(name):
    h.require(type(name) is str and name in spec.SEQUENCES, "SHARED_SEQUENCE")
    return spec.SEQUENCES[name]


def compile_fixture(plan, inputs):
    plan = h.validate_plan(plan)
    h.require(plan["inputs"]["scenario"] == spec.SCENARIO, "SHARED_SCENARIO")
    cr._inputs(inputs)
    now, fixture_id = inputs["redis_time_ms"], inputs["fixture_id"]
    lineage = cr._framed_digest("mifolyo:m4:shared-group-capacity:lineage:v1", fixture_id)[:32]
    global_scope = cr._framed_digest("mifolyo:rate:global:v2")
    group_scope = cr._framed_digest("mifolyo:rate:group:v2", lineage)
    group = [("group_id", spec.GROUP), ("rate_scope_id", lineage), ("group_scope_id", group_scope),
        ("request_start_limit", "10"), ("concurrency", "1"), ("interval_ms", "0")]
    descriptors = {name: {"purpose": "conformance_only", "case": spec.CASE, "kind": name, "release_eligible": False}
        for name in ("authorization", "authorization_scope", "canonicalization", "crawl_policy", "render_policy")}
    descriptors["crawl_policy"].update(global_concurrency=2, global_interval_ms=0, group_concurrency=1,
        group_interval_ms=0, origin_concurrency=1, origin_interval_ms=0)
    pins = {name: h.digest(h.canonical(value)) for name, value in descriptors.items()}
    actors, states = {}, {}
    keys = set((*h.AUTH, *cr.LIVE))
    for label in ("a", "b"):
        rid = cr._framed_digest("mifolyo:m4:shared-group-capacity:run:v1", fixture_id, label)[:32]
        origin = "https://m4-capacity-" + label + ".invalid:443"
        url = "https://m4-capacity-" + label + ".invalid/document"
        robots_url = "https://m4-capacity-" + label + ".invalid/robots.txt"
        scopes = [global_scope, group_scope, cr._framed_digest("mifolyo:rate:origin:v2", origin)]
        document, document_sha = cr._decision("document", url, lineage, scopes)
        robots, robots_sha = cr._decision("robots", robots_url, lineage, scopes)
        jid = document["target_url_id"]
        base, job_key = h.P + "run:" + rid, h.P + "run:" + rid + ":job:" + jid
        source = [("job_id", jid), ("canonical_url", url), ("score_text", "0"), ("depth", "1"),
            ("group_id", spec.GROUP), ("rate_scope_id", lineage), ("policy_decision_sha256", document_sha)]
        run = dict.fromkeys(cr.RUN_FIELDS, "0")
        run.update(protocol_version="2", contract_sha256=plan["identities"]["contract_sha256"], state="active", source_kind="mongo",
            source_sha256=cr._section_digest("mifolyo:crawl-source:v2", "jobs", [source]), expected_seed_count="1",
            authorization_sha256=pins["authorization"], authorization_scope_sha256=pins["authorization_scope"],
            authorization_expires_at_ms=str(now + 600000), canonicalization_version="1", canonicalization_sha256=pins["canonicalization"],
            crawl_policy_version="2", crawl_policy_sha256=pins["crawl_policy"], render_policy_version="1", render_policy_sha256=pins["render_policy"],
            policy_group_count="1", policy_group_map_sha256=cr._section_digest("mifolyo:policy-group-map:v2", "groups", [group]),
            max_jobs="10000", max_request_starts="10", global_concurrency_limit="2", max_delivery_attempts="3",
            job_count="1", open_job_count="1", load_revision="1", audit_revision="1", audit_count="1", audit_cursor=jid,
            audit_complete="1", created_at_ms=str(now - 400), sealed_at_ms=str(now - 300), activated_at_ms=str(now - 200),
            last_activity_at_ms=str(now - 100), archive_sha256="", purge_state="none", purge_evidence_sha256="", terminal_reason="none")
        job = {name: "0" if name in cr.JOB_NUMBERS else "" for name in cr.JOB_FIELDS}
        job.update(protocol_version="2", run_id=rid, job_id=jid, url_id=jid, canonical_url=url, depth="1", score_text="0",
            state="ready", group_id=spec.GROUP, rate_scope_id=lineage, group_scope_id=group_scope, initial_origin_scope_id=scopes[2],
            policy_decision_sha256=document_sha, next_request_ordinal="1", last_reason="none", last_failure_reason="none",
            commit_backpressure_reason="none", created_at_ms=str(now - 100), updated_at_ms=str(now - 100))
        identity = {"owner_id": inputs["owner_" + label], "lease_token": inputs["token_" + label], "fence": "1"}
        suffix = [("request_ordinal", "1"), ("request_kind", "robots"), ("target_url_id", robots["target_url_id"]),
            ("canonical_target_url", robots_url), ("target_digest", robots["target_digest"]), ("crawl_policy_sha256", pins["crawl_policy"]),
            ("policy_decision_sha256", robots_sha), *[(name, robots[name]) for name in
            ("group_id", "rate_scope_id", "global_scope_id", "group_scope_id", "origin_scope_id", "global_concurrency",
             "global_interval_ms", "group_concurrency", "group_interval_ms", "origin_concurrency", "origin_interval_ms")]]
        q = cr._framed_digest("mifolyo:request-reservation:v2", rid, jid, "1", identity["lease_token"],
            *[value for name, value in suffix if name != "canonical_target_url"])
        payload = [("canonical_url", url), ("score_text", "0"), ("depth", "1"), ("job_group_id", spec.GROUP),
            ("job_rate_scope_id", lineage), ("job_group_scope_id", group_scope), ("job_initial_origin_scope_id", scopes[2]),
            ("job_policy_decision_sha256", document_sha), ("expected_prior_fence", "0"), ("owner_id", identity["owner_id"]), *suffix]
        identity.update(reservation_id=q, intent=dict(suffix), intent_fields=suffix,
            claim_transition_id=cr._transition(spec.CLAIM, rid, jid, identity, payload))
        work = [*h.AUTH, *cr.LIVE, *[base + (":" + name if name else "") for name in cr.RUN_SUFFIXES], job_key]
        h.require(len(work) == 44, "SHARED_WORK_KEYS")
        keys.update(work)
        keys.add(h.P + "reservation:" + q)
        keys.update(h.P + "rate:" + scope + ending for scope in scopes for ending in ("", ":active", ":pending", ":started"))
        states[base] = cr._hash([(name, run[name]) for name in cr.RUN_FIELDS])
        states[job_key] = cr._hash([(name, job[name]) for name in cr.JOB_FIELDS])
        states[base + ":jobs"] = cr._set([jid])
        for name, score in (("job_order", 0), ("ready", 0), ("ready_at", now - 100)):
            states[base + ":" + name] = cr._zset({jid: score})
        maps = {"group_limits": "10", "group_rate_scope_ids": lineage, "group_scope_ids": group_scope,
            "group_concurrency": "1", "group_interval_ms": "0", "group_started": "0", "group_pending": "0",
            "group_active_started": "0", "group_open_jobs": "1", "audit_group_counts": "1"}
        for name, value in maps.items():
            states[base + ":" + name] = cr._hash([(spec.GROUP, value)])
        for name, reasons in (("retry_reason_counts", cr.RETRY_REASONS), ("recovery_outcome_counts", cr.RECOVERY_REASONS),
                ("disposition_reason_counts", cr.DISPOSITION_REASONS)):
            states[base + ":" + name] = cr._hash([(reason, "0") for reason in sorted(reasons)])
        actors[label] = {"run_id": rid, "job_id": jid, "base_key": base, "job_key": job_key, "url": url,
            "robots_url": robots_url, "origin": origin, "scope_ids": scopes, "work_keys": work,
            "document_decision": document, "robots_decision": robots, "source_fields": source, "identity": identity}
    h.require(actors["a"]["run_id"] != actors["b"]["run_id"] and actors["a"]["job_id"] != actors["b"]["job_id"], "SHARED_IDENTITY_COLLISION")
    scope_ids = [global_scope, group_scope, actors["a"]["scope_ids"][2], actors["b"]["scope_ids"][2]]
    h.require(len(set(scope_ids)) == 4 and len(keys) == spec.POSITIONS, "SHARED_INVENTORY")
    authority = h.ledger_setup(plan, now)
    state = {key: None for key in keys if key != h.AUTH[0]}
    for row in authority["writes"]:
        state[row["key"]] = cr._hash(row["fields"]) if row["type"] == "hash" else {"type": "string", "value": row["value"], "expires_at_ms": -1}
    state.update(states)
    runs = [actors[label]["run_id"] for label in ("a", "b")]
    state[h.P + "runs"] = cr._zset({rid: now - 400 for rid in runs})
    for key in (h.P + "active_runs", h.P + "unarchived_runs"):
        state[key] = cr._set(runs)
    h.require(len(state) == 89 and sum(row is not None for row in state.values()) == 45, "SHARED_SETUP_COUNT")
    result = {"version": 1, "case": spec.CASE, "artifact_kind": "private_offline_shared_capacity_fixture",
        "purpose": "conformance_only", "execution_authorized": False, "release_eligible": False,
        "measurement_status": "not_measured", "time_observation_verified": False,
        "compiler_sha256": h.digest(Path(__file__).read_bytes()), "plan_sha256": h.digest(h.canonical(plan)), "inputs": dict(inputs),
        "actors": actors, "scope_ids": scope_ids, "policy_descriptors": descriptors, "policy_group_fields": group,
        "authority_setup": authority, "key_inventory": sorted(keys), "bootstrap_owned_keys": [h.AUTH[0]],
        "external_required_absent": sorted(h.LEGACY), "initial_state": state}
    return h.decode(h.canonical(result))


def validate_fixture(plan, fixture):
    h.require(type(fixture) is dict and type(fixture.get("inputs")) is dict, "SHARED_FIXTURE")
    expected = compile_fixture(plan, fixture["inputs"])
    h.require(cr._bounded(fixture) == h.canonical(expected), "SHARED_FIXTURE_MISMATCH")
    return expected


def wire_requests(plan, fixture, epoch, trace="finish"):
    f = validate_fixture(plan, fixture)
    h.require(cr._hex(epoch, 32), "BOOT_EPOCH")
    setup = f["authority_setup"]
    gate = ["active", epoch, plan["identities"]["contract_sha256"], bytes.fromhex(plan["compatibility_marker"]["record_hex"]),
        bytes.fromhex(setup["stored_guard"]["record_hex"]), bytes.fromhex(setup["legacy_retirement"]["record_hex"]), b""]
    shas = {op: hashlib.sha1((h.ROOT / "services/spider/internal/database/crawljobsv2/lua" / (op.lower() + ".lua")).read_bytes()).hexdigest()
        for op in spec.SOURCES[1:]}
    wires = []
    for operation, label, _, fault in sequence(trace):
        actor, peer = f["actors"][label], f["actors"]["b" if label == "a" else "a"]
        identity, source = actor["identity"], dict(actor["source_fields"])
        keys = [*actor["work_keys"], h.P + "reservation:" + identity["reservation_id"],
            *[h.P + "rate:" + scope + ending for scope in actor["scope_ids"] for ending in ("", ":active", ":pending", ":started")]]
        if operation == spec.CLAIM:
            semantic = [actor["run_id"], actor["job_id"], actor["url"], "0", "1", spec.GROUP, identity["intent"]["rate_scope_id"],
                actor["scope_ids"][1], actor["scope_ids"][2], source["policy_decision_sha256"], "0", "1", identity["owner_id"],
                identity["lease_token"], *[value for _, value in identity["intent_fields"]], identity["claim_transition_id"]]
        elif operation == spec.MAINTAIN:
            keys, semantic = [*h.AUTH, h.P + "rate_scopes"], ["0"]
        else:
            semantic = [actor["run_id"], actor["job_id"], peer["identity"]["owner_id"] if fault == "owner" else identity["owner_id"],
                peer["identity"]["lease_token"] if fault == "token" else identity["lease_token"], "1", identity["reservation_id"]]
        wire = ("EVALSHA", shas[operation], str(len(keys)), *keys, *gate, *semantic)
        h.require(len(resp.encode(wire)) <= 65536, "SHARED_WIRE_BOUND")
        wires.append(wire)
    return wires


def _members(state, key, changes):
    current = {} if state[key] is None else {name: int(score) for name, score in state[key]["members"]}
    for name, score in changes.items():
        if score is None:
            current.pop(name, None)
        else:
            current[name] = score
    state[key] = cr._zset(current) if current else None


def _scopes(state, actor, now, expiry, action):
    identity = actor["identity"]
    q = identity["reservation_id"]
    for kind, scope in zip(("global", "group", "origin"), actor["scope_ids"]):
        key = h.P + "rate:" + scope
        if state[key] is None:
            h.require(action == "claim", "SHARED_SCOPE_MISSING")
            values = dict(protocol_version="2", scope_id=scope, scope_kind=kind,
                scope_witness="global" if kind == "global" else identity["intent"]["rate_scope_id"] if kind == "group" else actor["origin"],
                effective_concurrency="2" if kind == "global" else "1", effective_interval_ms="0", next_allowed_ms="0", last_started_at_ms="0",
                active_count="0", pending_count="0", started_count="0", updated_at_ms=str(now),
                concurrency_source_sha256=identity["intent"]["crawl_policy_sha256"], interval_source_sha256=identity["intent"]["crawl_policy_sha256"])
            state[key] = cr._hash([(name, values[name]) for name in cr.RATE_FIELDS])
        values = dict(state[key]["fields"])
        active, pending, started = [int(values[name]) for name in ("active_count", "pending_count", "started_count")]
        changes = {"updated_at_ms": now}
        if action == "claim":
            changes.update(active_count=active + 1, pending_count=pending + 1)
            _members(state, key + ":active", {q: expiry})
            _members(state, key + ":pending", {q: expiry})
        elif action == "start":
            changes.update(pending_count=pending - 1, started_count=started + 1, last_started_at_ms=now,
                next_allowed_ms=0 if kind == "global" else max(int(values["next_allowed_ms"]), now))
            _members(state, key + ":pending", {q: None})
            _members(state, key + ":started", {q: expiry})
        else:
            changes.update(active_count=active - 1)
            changes["pending_count" if action == "cancel" else "started_count"] = (pending if action == "cancel" else started) - 1
            _members(state, key + ":active", {q: None})
            _members(state, key + (":pending" if action == "cancel" else ":started"), {q: None})
        cr._change(state[key], **changes)
        _members(state, h.P + "rate_scopes", {scope: now})


def expected_sequence(plan, fixture, observations, trace="finish"):
    return _expected_validated(validate_fixture(plan, fixture), observations, trace)


def _expected_validated(f, observations, trace="finish"):
    steps = sequence(trace)
    h.require(type(observations) is list and 0 <= len(observations) <= len(steps), "SHARED_OBSERVATIONS")
    state, rows, minimum = copy.deepcopy(f["initial_state"]), [], f["inputs"]["redis_time_ms"]
    for index, observation in enumerate(observations):
        h.exact(observation, {"started_at_ms", "now_ms", "finished_at_ms"})
        before, now, after = [observation[name] for name in ("started_at_ms", "now_ms", "finished_at_ms")]
        h.require(type(before) is type(after) is int and minimum <= before <= after <= f["inputs"]["redis_time_ms"] + cr.CASE_MS, "SHARED_OBSERVATION_TIME")
        h.require(after - observations[0]["started_at_ms"] <= cr.STAGE_MS, "SHARED_MEASURE_SPAN")
        minimum = after
        operation, label, status, _ = steps[index]
        actor = f["actors"][label]
        base, job_key, rid, jid = [actor[name] for name in ("base_key", "job_key", "run_id", "job_id")]
        identity = actor["identity"]
        q, qkey = identity["reservation_id"], h.P + "reservation:" + identity["reservation_id"]
        if status.startswith("CRAWL_V2_"):
            h.require(now is None, "SHARED_ERROR_TIME")
            reply = {"error": status}
        else:
            h.require(type(now) is int and before <= now <= after, "SHARED_REPLY_TIME")
            if index in spec.MUTATIONS[trace]:
                if status == "CLAIMED":
                    expiry = now + cr.LEASE_MS
                    cr._change(state[base], claims_total=1, reservation_creations_total=1, pending_request_reservations=1,
                        last_activity_at_ms=now, last_execution_at_ms=now)
                    cr._change(state[base + ":group_pending"], **{spec.GROUP: 1})
                    cr._change(state[job_key], state="leased", claim_count=1, lease_fence=1, lease_owner=identity["owner_id"], lease_token=identity["lease_token"],
                        lease_started_at_ms=now, lease_expires_at_ms=expiry, active_reservation_id=q, next_request_ordinal=2, updated_at_ms=now,
                        last_transition_id=identity["claim_transition_id"], last_transition_status="CLAIMED")
                    state[base + ":ready"] = state[base + ":ready_at"] = None
                    state[base + ":leased"], state[base + ":leased_at"] = cr._zset({jid: expiry}), cr._zset({jid: now})
                    _members(state, h.P + "active_leases", {rid + ":" + jid: expiry})
                    values = dict.fromkeys(cr.RESERVATION_FIELDS, "0")
                    values.update(identity["intent"], protocol_version="2", reservation_id=q, run_id=rid, job_id=jid,
                        owner_id=identity["owner_id"], lease_token=identity["lease_token"], lease_fence="1", state="pending",
                        created_at_ms=str(now), expires_at_ms=str(expiry))
                    state[qkey] = cr._hash([(name, values[name]) for name in cr.RESERVATION_FIELDS])
                    _scopes(state, actor, now, expiry, "claim")
                elif status == "STARTED":
                    expiry = int(dict(state[qkey]["fields"])["expires_at_ms"])
                    cr._change(state[base], pending_request_reservations=0, started_request_reservations=1, request_starts=1,
                        last_activity_at_ms=now, last_execution_at_ms=now, last_request_started_at_ms=now)
                    for name, value in (("group_pending", 0), ("group_active_started", 1), ("group_started", 1)):
                        cr._change(state[base + ":" + name], **{spec.GROUP: value})
                    cr._change(state[job_key], request_starts=1, delivery_attempts=1, lease_delivery_started=1, last_request_started_at_ms=now, updated_at_ms=now)
                    cr._change(state[qkey], state="started", started_at_ms=now, delivery_attempts_after_start=1,
                        job_starts_after_start=1, run_starts_after_start=1, group_starts_after_start=1)
                    if state[h.P + "first_request_start"] is None:
                        state[h.P + "first_request_start"] = cr._hash([("protocol_version", "2"), ("run_id", rid),
                            ("job_id", jid), ("lease_fence", "1"), ("started_at_ms", str(now))])
                    _scopes(state, actor, now, expiry, "start")
                else:
                    cancelling = status == "RESERVATION_CANCELLED"
                    h.require(cancelling or status == "FINISHED", "SHARED_MUTATION")
                    expiry = int(dict(state[qkey]["fields"])["expires_at_ms"])
                    cr._change(state[base], **{"pending_request_reservations" if cancelling else "started_request_reservations": 0, "last_activity_at_ms": now})
                    cr._change(state[base + (":group_pending" if cancelling else ":group_active_started")], **{spec.GROUP: 0})
                    cr._change(state[job_key], active_reservation_id="", updated_at_ms=now)
                    cr._change(state[qkey], state="cancelled" if cancelling else "finished", terminal_at_ms=now)
                    state[qkey]["expires_at_ms"] = now + cr.TOMBSTONE_MS
                    _scopes(state, actor, now, expiry, "cancel" if cancelling else "finish")
            reservation = dict(state[qkey]["fields"]) if state[qkey] is not None else {}
            if status == "CAPACITY_BLOCKED":
                reply = [status, str(now), actor["scope_ids"][1], "1", "1", "0"]
            elif operation == spec.CLAIM:
                reply = [status, str(now), "1", reservation["expires_at_ms"], q, reservation["expires_at_ms"]]
            elif operation == spec.START:
                reply = [status, str(now), q, reservation["started_at_ms"], reservation["delivery_attempts_after_start"],
                    reservation["job_starts_after_start"], reservation["run_starts_after_start"], reservation["group_starts_after_start"],
                    "1" if reservation["state"] == "started" else "0"]
            elif operation == spec.MAINTAIN:
                reply = [status, str(now), "4", "0"]
            else:
                reply = [status, str(now), q]
        for other in f["actors"].values():
            job = dict(state[other["job_key"]]["fields"])
            h.require(job["state"] != "leased" or after < int(job["lease_expires_at_ms"]), "SHARED_LEASE_SPAN")
        rows.append({"assertion_id": "SGC" + f"{index + 1:02}", "operation": operation, "reply": reply, "state": copy.deepcopy(state)})
    return rows


def acl_rules(plan, fixture):
    """Closed ledger/observer projection; setup and short-lived roles wrap it."""
    import runtime_case as runtime
    f = validate_fixture(plan, fixture)
    rates = [h.P + "rate:" + scope for scope in f["scope_ids"]]
    reservations = [h.P + "reservation:" + actor["identity"]["reservation_id"] for actor in f["actors"].values()]
    sets = [h.P + "active_runs", h.P + "unarchived_runs", *[actor["base_key"] + ":jobs" for actor in f["actors"].values()]]
    hashes = [h.P + "stage_slots", h.P + "first_request_start", *rates, *reservations]
    writes, zwrite = [h.P + "first_request_start", *rates, *reservations], [h.P + "active_leases", h.P + "rate_scopes"]
    for actor in f["actors"].values():
        base = actor["base_key"]
        hashes.extend([base, actor["job_key"], *[base + ":" + suffix for suffix in cr.RUN_SUFFIXES[12:]]])
        writes.extend([base, actor["job_key"], *[base + ":" + suffix for suffix in ("group_pending", "group_started", "group_active_started")]])
        zwrite.extend(base + ":" + suffix for suffix in ("ready", "ready_at", "leased", "leased_at"))
    zsets = sorted(set(f["key_inventory"]) - set(h.AUTH) - set(hashes) - set(sets))
    zwrite.extend(key + ending for key in rates for ending in (":active", ":pending", ":started"))
    selector = runtime.selector
    readable = [key for key in f["key_inventory"] if key not in h.ABSENCE_ONLY]
    reads = (selector("+type", f["key_inventory"]), selector("+pttl", readable),
        selector("+hlen +hstrlen +hmget", (h.AUTH[i] for i in (0, 1, 5, 6))), selector("+strlen +get", (h.AUTH[2],)),
        selector(runtime.CLAIM_COMMANDS["hash_read"], sorted(set(hashes))),
        selector(runtime.CLAIM_COMMANDS["set_read"], sets), selector(runtime.CLAIM_COMMANDS["zset_read"], zsets))
    return {"ledger": ("+ping +time +info|server +info|memory", selector("+evalsha", f["key_inventory"], "~"), *reads,
        selector("+hset", sorted(set(writes)), "~"), selector("+zadd +zrem", sorted(set(zwrite)), "~"), selector("+pexpireat", reservations, "~")),
        "observer": ("+ping +time +dbsize +scan +info|server", *reads, selector("+pexpiretime", readable), selector("+type", h.LEGACY))}


def public_summary(plan, fixture):
    return _public_summary_validated(validate_fixture(plan, fixture))


def _public_summary_validated(f):
    return {"case": spec.CASE, "purpose": "conformance_only", "execution_authorized": False, "release_eligible": False,
        "measurement_status": "not_measured", "fixture_sha256": h.digest(h.canonical(f)), "compiler_sha256": f["compiler_sha256"],
        "plan_sha256": f["plan_sha256"], "possible_keys": 90, "fixture_owned_keys": 89, "runs": 2, "jobs": 2, "scopes": 4}
