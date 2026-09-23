"""Private, offline claim/release fixture, wire builder and expected-state oracle.

No connection, clock, randomness or execution API. Inputs are captured by a future
reviewed caller. Returned fixtures/wires/states contain private lease identities:
never log/export them. Only public_summary() is a report projection. BOOT remains
owned by real bootstrap; this module never constructs or writes its record.
"""
from __future__ import annotations

import copy
import hashlib
from pathlib import Path
import re
import struct

import harness as h
import resp

CASE = "ledger-claim-release-v1"
SCENARIO = "ledger-claim-release"
CLAIM, RELEASE = "CJ2_TRY_CLAIM", "CJ2_RELEASE_BEFORE_IO"
URL = "https://m4-fixture.invalid/document"
ROBOTS = "https://m4-fixture.invalid/robots.txt"
ORIGIN = "https://m4-fixture.invalid:443"
GROUP = "fixture"
LEASE_MS, TOMBSTONE_MS, STAGE_MS, CASE_MS = 60000, 86400000, 30000, 300000
INPUT_FIELDS = {"fixture_id", "redis_time_ms", "owner_a", "owner_b", "token_a", "token_b", "wrong_token"}
RUN_SUFFIXES = ("", "jobs", "job_order", "ready", "ready_at", "leased", "leased_at", "delayed",
    "commit_backpressure", "completed", "dead", "cancelled", "group_limits", "group_rate_scope_ids",
    "group_scope_ids", "group_concurrency", "group_interval_ms", "group_started", "group_pending",
    "group_active_started", "group_open_jobs", "audit_group_counts", "retry_reason_counts",
    "recovery_outcome_counts", "disposition_reason_counts", "visited_depth", "visited_urls")
LIVE = tuple(h.P + name for name in ("runs", "active_runs", "unarchived_runs", "first_request_start",
                                     "active_leases", "stage_expiry", "stage_slots", "rate_scopes"))
RETRY_REASONS = "request_timeout dns_temporary dial_temporary request_temporary http_429 http_5xx robots_temporary renderer_temporary downstream_backpressure capacity_blocked_after_io run_budget_exhausted_after_io group_budget_exhausted_after_io rate_blocked_after_io lease_expired_after_io worker_shutdown_after_io".split()
RECOVERY_REASONS = "ready delayed dead cancelled".split()
DISPOSITION_REASONS = "published already_visited policy_denied policy_scope_changed robots_denied robots_invalid job_malformed url_identity_mismatch static_url_denied dns_prohibited http_4xx response_invalid body_too_large html_invalid discovery_limit renderer_permanent output_invalid run_job_limit reservation_limit_exhausted retry_exhausted pre_io_recovery_exhausted protocol_corrupt authorization_expired operator_cancelled source_cancelled".split()
RUN_FIELDS = """protocol_version contract_sha256 state source_kind source_sha256 expected_seed_count
authorization_sha256 authorization_scope_sha256 authorization_expires_at_ms canonicalization_version canonicalization_sha256
crawl_policy_version crawl_policy_sha256 render_policy_version render_policy_sha256 policy_group_count policy_group_map_sha256
max_jobs max_request_starts global_concurrency_limit max_delivery_attempts job_count open_job_count request_starts
reservation_creations_total pending_request_reservations started_request_reservations claims_total retries_total
recovered_leases_total renewal_rejections_total completed_total dead_total cancelled_total output_commits_total load_revision
audit_revision audit_count audit_cursor audit_complete created_at_ms sealed_at_ms activated_at_ms budget_exhausted_at_ms
cancelled_at_ms completed_at_ms finalized_at_ms last_activity_at_ms last_execution_at_ms last_request_started_at_ms
last_terminal_transition_at_ms retention_anchor_ms archived_at_ms archive_sha256 purge_state purge_evidence_sha256
purge_started_at_ms purged_job_count terminal_reason""".split()
JOB_FIELDS = """protocol_version run_id job_id url_id canonical_url depth score_text state group_id rate_scope_id
group_scope_id initial_origin_scope_id policy_decision_sha256 claim_count delivery_attempts request_starts
lease_request_starts_baseline retry_count pre_io_recoveries next_request_ordinal last_request_started_at_ms
last_document_request_started_at_ms last_document_request_fence last_document_target_url_id last_document_target_url
last_document_target_digest last_reason last_failure_reason lease_owner lease_token lease_fence lease_started_at_ms
lease_expires_at_ms lease_delivery_started active_reservation_id active_stage_commit_id last_stage_commit_id last_stage_fence
not_before_ms commit_backpressure_fence commit_backpressure_reason commit_backpressure_started_at_ms commit_backpressure_deadline_ms
output_digest publication_id commit_id published_page_key last_transition_id last_transition_status created_at_ms updated_at_ms
completed_at_ms dead_at_ms cancelled_at_ms""".split()
JOB_NUMBERS = set("""protocol_version depth claim_count delivery_attempts request_starts lease_request_starts_baseline
retry_count pre_io_recoveries next_request_ordinal last_request_started_at_ms last_document_request_started_at_ms
last_document_request_fence lease_fence lease_started_at_ms lease_expires_at_ms lease_delivery_started last_stage_fence
not_before_ms commit_backpressure_fence commit_backpressure_started_at_ms commit_backpressure_deadline_ms created_at_ms
updated_at_ms completed_at_ms dead_at_ms cancelled_at_ms""".split())
RESERVATION_FIELDS = """protocol_version reservation_id run_id job_id owner_id lease_token lease_fence request_ordinal
state request_kind target_url_id canonical_target_url target_digest crawl_policy_sha256 policy_decision_sha256 group_id
rate_scope_id global_scope_id group_scope_id origin_scope_id global_concurrency global_interval_ms group_concurrency
group_interval_ms origin_concurrency origin_interval_ms created_at_ms started_at_ms terminal_at_ms delivery_attempts_after_start
job_starts_after_start run_starts_after_start group_starts_after_start expires_at_ms""".split()
RATE_FIELDS = """protocol_version scope_id scope_kind scope_witness effective_concurrency effective_interval_ms
next_allowed_ms last_started_at_ms active_count pending_count started_count concurrency_source_sha256
interval_source_sha256 updated_at_ms""".split()


def _frame(value):
    raw = value if type(value) is bytes else value.encode("utf-8")
    return struct.pack(">Q", len(raw)) + raw


def _framed_digest(domain, *values):
    return h.digest(b"".join(_frame(value) for value in (domain, *values)))


def _section_digest(domain, label, records):
    section = _frame(label) + struct.pack(">Q", len(records)) + b"".join(_frame(h.record(row)) for row in records)
    return h.digest(_frame(domain) + section)


def _hex(value, size):
    return type(value) is str and re.fullmatch("[0-9a-f]{" + str(size) + "}", value) is not None and value != "0" * size


def _inputs(value):
    h.exact(value, INPUT_FIELDS)
    h.require(type(value["redis_time_ms"]) is int and 400 < value["redis_time_ms"] <= h.MAX_EXACT - TOMBSTONE_MS - 600000,
              "FIXTURE_TIME")
    h.require(all(_hex(value[key], 32 if key in ("fixture_id", "owner_a", "owner_b") else 64)
                  for key in INPUT_FIELDS - {"redis_time_ms"}), "FIXTURE_IDENTITY")
    h.require(value["owner_a"] != value["owner_b"] and
              len({value["token_a"], value["token_b"], value["wrong_token"]}) == 3, "FIXTURE_IDENTITY_REUSE")


def _descriptor(kind):
    return {"purpose": "conformance_only", "case": CASE, "kind": kind, "release_eligible": False}


def _hash(fields, expiry=-1):
    return {"type": "hash", "fields": [[key, str(value)] for key, value in fields], "expires_at_ms": expiry}


def _zset(members):
    return {"type": "zset", "members": [[key, str(value)] for key, value in
            sorted(members.items(), key=lambda row: (int(row[1]), row[0]))], "expires_at_ms": -1}


def _set(members):
    return {"type": "set", "members": sorted(members), "expires_at_ms": -1}


def _change(entry, **changes):
    h.require(set(changes) <= {key for key, _ in entry["fields"]}, "ORACLE_FIELD")
    entry["fields"] = [[key, str(changes.get(key, value))] for key, value in entry["fields"]]


def _decision(kind, url, lineage, scopes):
    target_id = h.digest(("mifolyo-url:v1\0" + url).encode())
    fields = [("request_kind", kind), ("target_url_id", target_id),
        ("target_digest", _framed_digest("mifolyo:request-target:v2", target_id, url)), ("depth", "1"),
        ("group_id", GROUP), ("rate_scope_id", lineage), ("global_scope_id", scopes[0]),
        ("group_scope_id", scopes[1]), ("origin_scope_id", scopes[2]), ("global_concurrency", "2"),
        ("global_interval_ms", "0"), ("group_concurrency", "1"), ("group_interval_ms", "0"),
        ("origin_concurrency", "1"), ("origin_interval_ms", "0")]
    return dict(fields), _section_digest("mifolyo:policy-decision:v2", "decision", [fields])


def _transition(operation, run_id, job_id, identity, payload):
    payload_sha = _section_digest("mifolyo:transition-payload:v2", "arguments", [payload])
    return _framed_digest("mifolyo:crawl-transition:v2", operation, run_id, job_id,
                          identity["fence"], identity["lease_token"], "none", payload_sha)


def compile_fixture(plan, inputs):
    """Return a private deterministic projection, not measured setup or authority."""
    plan = h.validate_plan(plan)
    h.require(plan["inputs"]["scenario"] in (SCENARIO, *h.negative.LEDGER_SCENARIOS), "CLAIM_SCENARIO")
    _inputs(inputs)
    inputs = dict(inputs)
    now, run_id = inputs["redis_time_ms"], inputs["fixture_id"]
    lineage = _framed_digest("mifolyo:m4:claim-release:lineage:v1", run_id)[:32]
    scopes = [_framed_digest("mifolyo:rate:global:v2"), _framed_digest("mifolyo:rate:group:v2", lineage),
              _framed_digest("mifolyo:rate:origin:v2", ORIGIN)]
    document, document_sha = _decision("document", URL, lineage, scopes)
    robots, robots_sha = _decision("robots", ROBOTS, lineage, scopes)
    job_id = document["target_url_id"]
    base, job_key = h.P + "run:" + run_id, h.P + "run:" + run_id + ":job:" + job_id
    descriptors = {name: _descriptor(name) for name in ("authorization", "authorization_scope", "canonicalization", "crawl_policy", "render_policy")}
    pins = {name: h.digest(h.canonical(value)) for name, value in descriptors.items()}
    group = [("group_id", GROUP), ("rate_scope_id", lineage), ("group_scope_id", scopes[1]),
             ("request_start_limit", "10"), ("concurrency", "1"), ("interval_ms", "0")]
    source = [("job_id", job_id), ("canonical_url", URL), ("score_text", "0"), ("depth", "1"),
              ("group_id", GROUP), ("rate_scope_id", lineage), ("policy_decision_sha256", document_sha)]
    # Explicit fixed-shape literals; Go independently checks every field/order.
    run = {name: "0" for name in RUN_FIELDS}
    run.update(protocol_version="2", contract_sha256=plan["identities"]["contract_sha256"], state="active", source_kind="mongo",
        source_sha256=_section_digest("mifolyo:crawl-source:v2", "jobs", [source]), expected_seed_count="1",
        authorization_sha256=pins["authorization"], authorization_scope_sha256=pins["authorization_scope"],
        authorization_expires_at_ms=str(now + 600000), canonicalization_version="1", canonicalization_sha256=pins["canonicalization"],
        crawl_policy_version="2", crawl_policy_sha256=pins["crawl_policy"], render_policy_version="1", render_policy_sha256=pins["render_policy"],
        policy_group_count="1", policy_group_map_sha256=_section_digest("mifolyo:policy-group-map:v2", "groups", [group]),
        max_jobs="10000", max_request_starts="10", global_concurrency_limit="2", max_delivery_attempts="3",
        job_count="1", open_job_count="1", load_revision="1", audit_revision="1", audit_count="1", audit_cursor=job_id,
        audit_complete="1", created_at_ms=str(now - 400), sealed_at_ms=str(now - 300), activated_at_ms=str(now - 200),
        last_activity_at_ms=str(now - 100), archive_sha256="", purge_state="none", purge_evidence_sha256="", terminal_reason="none")
    job = {name: "0" if name in JOB_NUMBERS else "" for name in JOB_FIELDS}
    job.update(protocol_version="2", run_id=run_id, job_id=job_id, url_id=job_id, canonical_url=URL, depth="1", score_text="0",
        state="ready", group_id=GROUP, rate_scope_id=lineage, group_scope_id=scopes[1], initial_origin_scope_id=scopes[2],
        policy_decision_sha256=document_sha, next_request_ordinal="1", last_reason="none", last_failure_reason="none",
        commit_backpressure_reason="none", created_at_ms=str(now - 100), updated_at_ms=str(now - 100))
    identities, reservation_ids = {}, []
    for label, fence in (("a", 1), ("b", 2)):
        identity = {"owner_id": inputs["owner_" + label], "lease_token": inputs["token_" + label], "fence": str(fence)}
        suffix = [("request_ordinal", str(fence)), ("request_kind", "robots"), ("target_url_id", robots["target_url_id"]),
            ("canonical_target_url", ROBOTS), ("target_digest", robots["target_digest"]), ("crawl_policy_sha256", pins["crawl_policy"]),
            ("policy_decision_sha256", robots_sha), *[(key, robots[key]) for key in
                ("group_id", "rate_scope_id", "global_scope_id", "group_scope_id", "origin_scope_id", "global_concurrency",
                 "global_interval_ms", "group_concurrency", "group_interval_ms", "origin_concurrency", "origin_interval_ms")]]
        values = dict(suffix)
        reservation = _framed_digest("mifolyo:request-reservation:v2", run_id, job_id, str(fence), identity["lease_token"],
                                    *[value for key, value in suffix if key != "canonical_target_url"])
        payload = [("canonical_url", URL), ("score_text", "0"), ("depth", "1"), ("job_group_id", GROUP),
            ("job_rate_scope_id", lineage), ("job_group_scope_id", scopes[1]), ("job_initial_origin_scope_id", scopes[2]),
            ("job_policy_decision_sha256", document_sha), ("expected_prior_fence", str(fence - 1)), ("owner_id", identity["owner_id"]), *suffix]
        identity.update(reservation_id=reservation, intent=values, intent_fields=suffix,
            claim_transition_id=_transition(CLAIM, run_id, job_id, identity, payload),
            release_transition_id=_transition(RELEASE, run_id, job_id, identity, [("owner_id", identity["owner_id"])]))
        identities[label] = identity
        reservation_ids.append(reservation)
    wrong = {"owner_id": inputs["owner_a"], "lease_token": inputs["wrong_token"], "fence": "1"}
    wrong["release_transition_id"] = _transition(RELEASE, run_id, job_id, wrong, [("owner_id", wrong["owner_id"])])
    identities["wrong"] = wrong
    work_keys = [*h.AUTH, *LIVE, *[base + (":" + suffix if suffix else "") for suffix in RUN_SUFFIXES], job_key]
    scope_keys = [h.P + "rate:" + scope + suffix for scope in scopes for suffix in ("", ":active", ":pending", ":started")]
    keys = sorted(set(work_keys + [h.P + "reservation:" + q for q in reservation_ids] + scope_keys))
    h.require(len(work_keys) == 44 and len(keys) == 58, "CLAIM_KEY_INVENTORY")
    authority = h.ledger_setup(plan, now)
    state = {key: None for key in keys if key != h.AUTH[0]}
    for row in authority["writes"]:
        state[row["key"]] = (_hash(row["fields"]) if row["type"] == "hash" else
                             {"type": "string", "value": row["value"], "expires_at_ms": -1})
    state[base], state[job_key] = _hash([(k, run[k]) for k in RUN_FIELDS]), _hash([(k, job[k]) for k in JOB_FIELDS])
    state[h.P + "runs"] = _zset({run_id: now - 400})
    for key in (h.P + "active_runs", h.P + "unarchived_runs"):
        state[key] = _set([run_id])
    state[base + ":jobs"] = _set([job_id])
    for suffix, score in (("job_order", 0), ("ready", 0), ("ready_at", now - 100)):
        state[base + ":" + suffix] = _zset({job_id: score})
    maps = {"group_limits": "10", "group_rate_scope_ids": lineage, "group_scope_ids": scopes[1], "group_concurrency": "1",
            "group_interval_ms": "0", "group_started": "0", "group_pending": "0", "group_active_started": "0",
            "group_open_jobs": "1", "audit_group_counts": "1"}
    for suffix, value in maps.items():
        state[base + ":" + suffix] = _hash([(GROUP, value)])
    for suffix, reasons in (("retry_reason_counts", RETRY_REASONS), ("recovery_outcome_counts", RECOVERY_REASONS),
                            ("disposition_reason_counts", DISPOSITION_REASONS)):
        state[base + ":" + suffix] = _hash([(name, "0") for name in sorted(reasons)])
    result = {"version": 1, "case": CASE, "artifact_kind": "private_offline_claim_fixture", "purpose": "conformance_only",
        "execution_authorized": False, "release_eligible": False, "time_observation_verified": False, "measurement_status": "not_measured",
        "compiler_sha256": h.digest(Path(__file__).read_bytes()), "plan_sha256": h.digest(h.canonical(plan)), "inputs": inputs,
        "policy_descriptors": descriptors, "authority_setup": authority, "policy_group_fields": group,
        "document_decision": document, "robots_decision": robots, "source_fields": source,
        "run_id": run_id, "job_id": job_id, "base_key": base, "job_key": job_key, "scope_ids": scopes,
        "identities": identities, "work_keys": work_keys, "key_inventory": keys, "bootstrap_owned_keys": [h.AUTH[0]],
        "external_required_absent": sorted(h.LEGACY), "initial_state": state}
    # Normalize pairs to JSON arrays and enforce the envelope's hard byte limit.
    return h.decode(h.canonical(result))


def validate_fixture(plan, fixture):
    h.require(type(fixture) is dict and type(fixture.get("inputs")) is dict, "CLAIM_FIXTURE")
    expected = compile_fixture(plan, fixture["inputs"])
    h.require(_bounded(fixture) == h.canonical(expected), "CLAIM_FIXTURE_MISMATCH")
    return expected


def _bounded(value):
    try:
        raw = h.canonical(value)
        h.decode(raw)  # rejects floats, booleans later compare distinctly
        return raw
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise h.InvalidArtifact("CLAIM_ARTIFACT") from None


def wire_requests(plan, fixture, boot_epoch):
    fixture = validate_fixture(plan, fixture)
    h.require(_hex(boot_epoch, 32), "BOOT_EPOCH")
    setup = fixture["authority_setup"]
    gate = ["active", boot_epoch, plan["identities"]["contract_sha256"],
            bytes.fromhex(plan["compatibility_marker"]["record_hex"]), bytes.fromhex(setup["stored_guard"]["record_hex"]),
            bytes.fromhex(setup["legacy_retirement"]["record_hex"]), b""]
    source = dict(fixture["source_fields"])
    job = dict(fixture["initial_state"][fixture["job_key"]]["fields"])
    requests = []
    sequence = [(CLAIM, "a"), (CLAIM, "a"), (RELEASE, "wrong"), (RELEASE, "a"), (RELEASE, "a"),
                (CLAIM, "b"), (RELEASE, "a"), (RELEASE, "b"), (RELEASE, "b")]
    shas = {op: hashlib.sha1((h.ROOT / "services/spider/internal/database/crawljobsv2/lua" / (op.lower() + ".lua")).read_bytes()).hexdigest()
            for op in (CLAIM, RELEASE)}
    for operation, label in sequence:
        identity = fixture["identities"][label]
        keys = list(fixture["work_keys"])
        if operation == CLAIM:
            keys += [h.P + "reservation:" + identity["reservation_id"]]
            keys += [h.P + "rate:" + scope + suffix for scope in fixture["scope_ids"] for suffix in ("", ":active", ":pending", ":started")]
            semantic = [fixture["run_id"], fixture["job_id"], URL, "0", "1", GROUP, job["rate_scope_id"],
                job["group_scope_id"], job["initial_origin_scope_id"], source["policy_decision_sha256"], str(int(identity["fence"]) - 1),
                identity["fence"], identity["owner_id"], identity["lease_token"],
                *[value for _, value in identity["intent_fields"]], identity["claim_transition_id"]]
        else:
            semantic = [fixture["run_id"], fixture["job_id"], identity["owner_id"], identity["lease_token"],
                        identity["fence"], identity["release_transition_id"]]
        wire = ["EVALSHA", shas[operation], str(len(keys)), *keys, *gate, *semantic]
        resp.encode(wire)  # bound the actual serialized request, not an estimate
        requests.append(tuple(wire))
    return requests


def expected_sequence(plan, fixture, times):
    """Pure expected states for CR01–09, from supplied Redis-time observations.

    States enumerate all 57 fixture-owned keys (None means absent). The 58th,
    durability, must be compared to verified BOOT by the lifecycle caller; it
    cannot be fabricated as a setup write. No result here is execution evidence.
    """
    f = validate_fixture(plan, fixture)
    t = f["inputs"]["redis_time_ms"]
    # Setup/revocation can precede the timed measurement stage. Bound both the
    # case-relative observations and their measurement span, not T+stage_time.
    h.require(type(times) is list and len(times) == 9 and
              all(type(now) is int and t <= now <= t + CASE_MS for now in times) and times == sorted(times) and
              times[-1] - times[0] <= STAGE_MS, "ORACLE_TIMES")
    state = copy.deepcopy(f["initial_state"])
    base, job_key, jid, rid = f["base_key"], f["job_key"], f["job_id"], f["run_id"]
    results = []
    for index, now in enumerate(times):
        label = "a" if index < 5 else "b"
        identity = f["identities"][label]
        fence, q = identity["fence"], identity["reservation_id"]
        qkey = h.P + "reservation:" + q
        if index in (0, 5):
            deadline = now + LEASE_MS
            _change(state[base], pending_request_reservations=1, reservation_creations_total=fence, claims_total=fence,
                    last_activity_at_ms=now, last_execution_at_ms=now)
            _change(state[base + ":group_pending"], **{GROUP: 1})
            _change(state[job_key], state="leased", claim_count=fence, next_request_ordinal=int(fence) + 1,
                lease_owner=identity["owner_id"], lease_token=identity["lease_token"], lease_fence=fence,
                lease_started_at_ms=now, lease_expires_at_ms=deadline, lease_request_starts_baseline=0,
                lease_delivery_started=0, active_reservation_id=q, updated_at_ms=now,
                last_transition_id=identity["claim_transition_id"], last_transition_status="CLAIMED")
            state[base + ":ready"] = state[base + ":ready_at"] = None
            state[base + ":leased"], state[base + ":leased_at"] = _zset({jid: deadline}), _zset({jid: now})
            state[h.P + "active_leases"] = _zset({rid + ":" + jid: deadline})
            reservation = {name: "0" for name in RESERVATION_FIELDS}
            reservation.update(identity["intent"], protocol_version="2", reservation_id=q, run_id=rid, job_id=jid,
                owner_id=identity["owner_id"], lease_token=identity["lease_token"], lease_fence=fence, state="pending",
                created_at_ms=str(now), expires_at_ms=str(deadline))
            state[qkey] = _hash([(name, reservation[name]) for name in RESERVATION_FIELDS])
            for kind, scope in zip(("global", "group", "origin"), f["scope_ids"]):
                key = h.P + "rate:" + scope
                values = dict(protocol_version="2", scope_id=scope, scope_kind=kind,
                    scope_witness="global" if kind == "global" else (identity["intent"]["rate_scope_id"] if kind == "group" else ORIGIN),
                    effective_concurrency="2" if kind == "global" else "1", effective_interval_ms="0", next_allowed_ms="0",
                    last_started_at_ms="0", active_count="1", pending_count="1", started_count="0",
                    concurrency_source_sha256=identity["intent"]["crawl_policy_sha256"],
                    interval_source_sha256=identity["intent"]["crawl_policy_sha256"], updated_at_ms=str(now))
                state[key] = _hash([(name, values[name]) for name in RATE_FIELDS])
                state[key + ":active"] = _zset({q: deadline})
                state[key + ":pending"] = _zset({q: deadline})
            state[h.P + "rate_scopes"] = _zset({scope: now for scope in f["scope_ids"]})
            reply = ["CLAIMED", str(now), fence, str(deadline), q, str(deadline)]
        elif index in (3, 7):
            _change(state[base], pending_request_reservations=0, last_activity_at_ms=now)
            _change(state[base + ":group_pending"], **{GROUP: 0})
            _change(state[job_key], state="ready", lease_owner="", lease_token="", lease_started_at_ms=0,
                lease_expires_at_ms=0, active_reservation_id="", updated_at_ms=now,
                last_transition_id=identity["release_transition_id"], last_transition_status="RELEASED_READY")
            state[base + ":leased"] = state[base + ":leased_at"] = state[h.P + "active_leases"] = None
            state[base + ":ready"], state[base + ":ready_at"] = _zset({jid: 0}), _zset({jid: now})
            _change(state[qkey], state="cancelled", terminal_at_ms=now)
            state[qkey]["expires_at_ms"] = now + TOMBSTONE_MS
            for scope in f["scope_ids"]:
                key = h.P + "rate:" + scope
                _change(state[key], active_count=0, pending_count=0, updated_at_ms=now)
                state[key + ":active"] = state[key + ":pending"] = None
            state[h.P + "rate_scopes"] = _zset({scope: now for scope in f["scope_ids"]})
            reply = ["RELEASED_READY", str(now), str(now)]
        elif index == 1:
            job = dict(state[job_key]["fields"])
            reply = ["ALREADY_CLAIMED", str(now), "1", job["lease_expires_at_ms"], q, job["lease_expires_at_ms"]]
        elif index in (2, 6):
            reply = ["LEASE_LOST", str(now), "1" if index == 2 else "2"]
        else:
            reply = ["RELEASED_READY", str(now), dict(state[job_key]["fields"])["updated_at_ms"]]
        results.append({"assertion_id": f"CR{index + 1:02}", "reply": reply, "state": copy.deepcopy(state)})
    return results


def validate_state(plan, fixture, times, step, observed):
    h.require(type(step) is int and 0 <= step < 9, "ORACLE_STEP")
    expected = expected_sequence(plan, fixture, times)[step]["state"]
    h.require(type(observed) is dict and _bounded(observed) == h.canonical(expected), "CLAIM_STATE_MISMATCH")


def public_summary(plan, fixture):
    fixture = validate_fixture(plan, fixture)
    return {"case": CASE, "purpose": "conformance_only", "execution_authorized": False, "release_eligible": False,
            "measurement_status": "not_measured", "fixture_sha256": h.digest(h.canonical(fixture)),
            "compiler_sha256": fixture["compiler_sha256"], "plan_sha256": fixture["plan_sha256"],
            "possible_keys": len(fixture["key_inventory"]), "fixture_owned_keys": len(fixture["initial_state"])}
