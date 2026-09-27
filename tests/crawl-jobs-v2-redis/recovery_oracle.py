"""Offline pre-I/O recovery oracle over the existing private claim fixture.

This is not an executable case, controller, clock, process-death observation or
execution approval. The future runtime slice needs its own reviewed scenario,
ACL, claimant-park/kill handshake and real-time lifecycle before admission.
"""
from __future__ import annotations

import copy
import hashlib

import claim_release as cr
import harness as h
import resp

CASE = "ledger-worker-death-pre-io-v1"
RECOVER = "CJ2_RECOVER_EXPIRED"
RENEW = "CJ2_RENEW_LEASE"
OPERATIONS = (cr.CLAIM, RECOVER, RECOVER, RECOVER, cr.CLAIM, cr.CLAIM,
              cr.CLAIM, cr.RELEASE, RENEW, RENEW, cr.RELEASE, cr.RELEASE, RECOVER)
LABELS = ("claim_a", "recover_before_expiry", "recover_due", "recover_replay", "claim_b", "claim_b_replay",
          "stale_claim_a", "stale_release_a", "stale_renew_a", "renew_b", "release_b", "release_b_replay", "recover_drained")


def fixture_for(plan, fixture):
    h.require(type(plan) is dict and type(plan.get("inputs")) is dict and
              plan["inputs"].get("scenario") == cr.SCENARIO, "RECOVERY_ORACLE_BASIS")
    return cr.validate_fixture(plan, fixture)


def wire_requests(plan, fixture, boot_epoch):
    f = fixture_for(plan, fixture)
    basis = cr.wire_requests(plan, f, boot_epoch)
    gate = list(basis[0][3 + int(basis[0][2]):3 + int(basis[0][2]) + 7])
    maintenance = [*h.AUTH, *cr.LIVE[:3], *cr.LIVE[4:],
                   *[f["base_key"] + (":" + suffix if suffix else "") for suffix in cr.RUN_SUFFIXES]]
    h.require(len(maintenance) == 42 and len(set(maintenance)) == 42, "RECOVERY_KEYS")
    sha = {op: hashlib.sha1((h.ROOT / "services/spider/internal/database/crawljobsv2/lua" / (op.lower() + ".lua")).read_bytes()).hexdigest()
           for op in (RECOVER, RENEW)}
    recover = ("EVALSHA", sha[RECOVER], "42", *maintenance, *gate, f["run_id"])
    renew = {}
    for label in ("a", "b"):
        who = f["identities"][label]
        renew[label] = ("EVALSHA", sha[RENEW], "44", *f["work_keys"], *gate,
                       f["run_id"], f["job_id"], who["owner_id"], who["lease_token"], who["fence"])
    wires = (basis[0], recover, recover, recover, basis[5], basis[5], basis[0], basis[3],
             renew["a"], renew["b"], basis[7], basis[7], recover)
    for wire in wires:
        resp.encode(wire)
    return wires


def _claim(state, f, label, now):
    identity = f["identities"][label]
    fence, q = identity["fence"], identity["reservation_id"]
    base, job, jid = f["base_key"], f["job_key"], f["job_id"]
    deadline = now + cr.LEASE_MS
    cr._change(state[base], pending_request_reservations=1, reservation_creations_total=fence, claims_total=fence,
               last_activity_at_ms=now, last_execution_at_ms=now)
    cr._change(state[base + ":group_pending"], **{cr.GROUP: 1})
    cr._change(state[job], state="leased", claim_count=fence, next_request_ordinal=int(fence) + 1,
        lease_owner=identity["owner_id"], lease_token=identity["lease_token"], lease_fence=fence,
        lease_started_at_ms=now, lease_expires_at_ms=deadline, lease_request_starts_baseline=0,
        lease_delivery_started=0, active_reservation_id=q, updated_at_ms=now,
        last_transition_id=identity["claim_transition_id"], last_transition_status="CLAIMED")
    state[base + ":ready"] = state[base + ":ready_at"] = None
    state[base + ":leased"], state[base + ":leased_at"] = cr._zset({jid: deadline}), cr._zset({jid: now})
    state[h.P + "active_leases"] = cr._zset({f["run_id"] + ":" + jid: deadline})
    values = {name: "0" for name in cr.RESERVATION_FIELDS}
    values.update(identity["intent"], protocol_version="2", reservation_id=q, run_id=f["run_id"], job_id=jid,
        owner_id=identity["owner_id"], lease_token=identity["lease_token"], lease_fence=fence, state="pending",
        created_at_ms=str(now), expires_at_ms=str(deadline))
    state[h.P + "reservation:" + q] = cr._hash([(name, values[name]) for name in cr.RESERVATION_FIELDS])
    for kind, scope in zip(("global", "group", "origin"), f["scope_ids"]):
        key = h.P + "rate:" + scope
        values = dict(protocol_version="2", scope_id=scope, scope_kind=kind,
            scope_witness="global" if kind == "global" else identity["intent"]["rate_scope_id"] if kind == "group" else cr.ORIGIN,
            effective_concurrency="2" if kind == "global" else "1", effective_interval_ms="0", next_allowed_ms="0",
            last_started_at_ms="0", active_count="1", pending_count="1", started_count="0",
            concurrency_source_sha256=identity["intent"]["crawl_policy_sha256"],
            interval_source_sha256=identity["intent"]["crawl_policy_sha256"], updated_at_ms=str(now))
        state[key] = cr._hash([(name, values[name]) for name in cr.RATE_FIELDS])
        state[key + ":active"] = state[key + ":pending"] = cr._zset({q: deadline})
    state[h.P + "rate_scopes"] = cr._zset({scope: now for scope in f["scope_ids"]})
    return ["CLAIMED", str(now), fence, str(deadline), q, str(deadline)]


def _ready(state, f, label, now, recovered):
    base, job, jid = f["base_key"], f["job_key"], f["job_id"]
    who = f["identities"][label]
    cr._change(state[base], pending_request_reservations=0, last_activity_at_ms=now)
    cr._change(state[base + ":group_pending"], **{cr.GROUP: 0})
    changes = dict(state="ready", lease_owner="", lease_token="", lease_started_at_ms=0, lease_expires_at_ms=0,
        active_reservation_id="", updated_at_ms=now,
        last_transition_id="" if recovered else who["release_transition_id"],
        last_transition_status="" if recovered else "RELEASED_READY")
    if recovered:
        changes["pre_io_recoveries"] = 1
        cr._change(state[base], recovered_leases_total=1)
        cr._change(state[base + ":recovery_outcome_counts"], ready=1)
    cr._change(state[job], **changes)
    state[base + ":leased"] = state[base + ":leased_at"] = state[h.P + "active_leases"] = None
    state[base + ":ready"], state[base + ":ready_at"] = cr._zset({jid: 0}), cr._zset({jid: now})
    key = h.P + "reservation:" + who["reservation_id"]
    cr._change(state[key], state="expired" if recovered else "cancelled", terminal_at_ms=now)
    state[key]["expires_at_ms"] = now + cr.TOMBSTONE_MS
    for scope in f["scope_ids"]:
        key = h.P + "rate:" + scope
        cr._change(state[key], active_count=0, pending_count=0, updated_at_ms=now)
        state[key + ":active"] = state[key + ":pending"] = None
    state[h.P + "rate_scopes"] = cr._zset({scope: now for scope in f["scope_ids"]})


def expected_sequence(plan, fixture, times):
    f = fixture_for(plan, fixture)
    at = f["inputs"]["redis_time_ms"]
    h.require(type(times) is list and len(times) == len(OPERATIONS) and
              all(type(now) is int and at <= now <= at + cr.CASE_MS for now in times) and times == sorted(times), "RECOVERY_TIMES")
    expiry = times[0] + cr.LEASE_MS
    h.require(times[1] < expiry <= times[2] and times[9] > times[4] and
              times[-1] < times[4] + cr.LEASE_MS and times[-1] - times[2] <= cr.STAGE_MS, "RECOVERY_PHASE_TIME")
    state, result = copy.deepcopy(f["initial_state"]), []
    for index, now in enumerate(times):
        if index in (0, 4):
            reply = _claim(state, f, "a" if index == 0 else "b", now)
        elif index == 2:
            _ready(state, f, "a", now, True)
            reply = ["BATCH_DONE", str(now), "1", "0"]
        elif index in (1, 3, 12):
            reply = ["BATCH_DONE", str(now), "0", "0"]
        elif index == 5:
            job = dict(state[f["job_key"]]["fields"])
            reply = ["ALREADY_CLAIMED", str(now), "2", job["lease_expires_at_ms"], f["identities"]["b"]["reservation_id"], job["lease_expires_at_ms"]]
        elif index in (6, 7, 8):
            if index == 8:
                cr._change(state[f["base_key"]], renewal_rejections_total=1)
            reply = ["LEASE_LOST", str(now), "2"]
        elif index == 9:
            deadline = now + cr.LEASE_MS
            q = f["identities"]["b"]["reservation_id"]
            cr._change(state[f["job_key"]], lease_expires_at_ms=deadline, updated_at_ms=now)
            cr._change(state[f["base_key"]], last_activity_at_ms=now)
            cr._change(state[h.P + "reservation:" + q], expires_at_ms=deadline)
            state[f["base_key"] + ":leased"] = cr._zset({f["job_id"]: deadline})
            state[h.P + "active_leases"] = cr._zset({f["run_id"] + ":" + f["job_id"]: deadline})
            for scope in f["scope_ids"]:
                key = h.P + "rate:" + scope
                cr._change(state[key], updated_at_ms=now)
                state[key + ":active"] = state[key + ":pending"] = cr._zset({q: deadline})
            state[h.P + "rate_scopes"] = cr._zset({scope: now for scope in f["scope_ids"]})
            reply = ["RENEWED", str(now), str(deadline)]
        elif index == 10:
            _ready(state, f, "b", now, False)
            reply = ["RELEASED_READY", str(now), str(now)]
        else:
            reply = ["RELEASED_READY", str(now), str(times[10])]
        result.append({"assertion_id": "RCV" + str(index + 1).zfill(2), "label": LABELS[index],
                       "operation": OPERATIONS[index], "reply": reply, "state": copy.deepcopy(state)})
    return result


def validate_state(plan, fixture, times, step, observed):
    h.require(type(step) is int and 0 <= step < len(OPERATIONS), "RECOVERY_STEP")
    expected = expected_sequence(plan, fixture, times)[step]["state"]
    h.require(type(observed) is dict and cr._bounded(observed) == h.canonical(expected), "RECOVERY_STATE_MISMATCH")
