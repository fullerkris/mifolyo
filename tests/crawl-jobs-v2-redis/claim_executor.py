"""Fixed claim/release setup, bounded live reads and redacted measurement receipts.

Connections are supplied only by the fixed Unix-socket executor. This module
accepts no endpoint or command sequence and never exports a private fixture.
"""
from __future__ import annotations

import re

import claim_release as cr
import harness as h
import runtime_case as case
from resp import RedisError

MAX_KEYS = 58
AUTH_ROLES = ("durability", "active_compatibility", "contract", "candidate_compatibility",
              "candidate_contract", "commit_guard", "legacy_retirement", "admin_freeze")
RUN_COUNTERS = ("job_count", "open_job_count", "claims_total", "reservation_creations_total",
                "pending_request_reservations", "started_request_reservations", "request_starts", "retries_total",
                "recovered_leases_total", "renewal_rejections_total", "completed_total", "dead_total", "cancelled_total", "output_commits_total")
JOB_COUNTERS = ("claim_count", "lease_fence", "next_request_ordinal", "lease_request_starts_baseline", "request_starts", "delivery_attempts")
GROUP_COUNTERS = ("group_open_jobs", "group_pending", "group_active_started", "group_started")
SCOPE_COUNTERS = ("active_count", "pending_count", "started_count")


class MeasurementFailure(h.InvalidArtifact):
    def __init__(self, result):
        self.result = result
        super().__init__("CLAIM_MEASUREMENT_FAILED")


def _text(value, maximum=2048):
    h.require(type(value) is bytes and len(value) <= maximum, "CLAIM_TEXT_BOUND")
    try:
        return value.decode("utf-8")
    except UnicodeError:
        raise h.InvalidArtifact("CLAIM_TEXT") from None


def _number(value):
    h.require(type(value) is bytes and re.fullmatch(rb"(?:0|[1-9][0-9]{0,15})", value) is not None, "CLAIM_NUMBER")
    result = int(value)
    h.require(result <= h.MAX_EXACT, "CLAIM_NUMBER_BOUND")
    return result


def inventory(client):
    count = client.call("DBSIZE")
    h.require(type(count) is int and 0 <= count <= MAX_KEYS, "CLAIM_KEY_COUNT")
    keys, cursor = set(), b"0"
    for _ in range(64):
        reply = client.call("SCAN", cursor, "COUNT", "16")
        h.require(type(reply) is list and len(reply) == 2 and type(reply[0]) is bytes and
                  re.fullmatch(rb"[0-9]{1,20}", reply[0]) is not None and type(reply[1]) is list and
                  len(reply[1]) <= MAX_KEYS, "CLAIM_SCAN")
        # COUNT is a hint; a reply may legitimately contain more than 16 keys.
        keys.update(_text(key, 256) for key in reply[1])
        h.require(len(keys) <= MAX_KEYS, "CLAIM_SCAN_BOUND")
        cursor = reply[0]
        if cursor == b"0":
            h.require(len(keys) == count, "CLAIM_SCAN_CHANGED")
            return keys
    raise h.InvalidArtifact("CLAIM_SCAN_BUDGET")


def _count(client, command, key, expected):
    value = client.call(command, key)
    h.require(type(value) is int and value == expected, "CLAIM_CARDINALITY")


def snapshot(client, expected, boot_record):
    """Read the complete exclusive DB, then compare typed values and absolute TTLs.

    No observed value supplies the expected schema/member list. Fixed projection
    bounds precede reads; neither unknown keys nor missing known keys are hidden.
    """
    h.require(type(expected) is dict and len(expected) == 57 and h.AUTH[0] not in expected, "CLAIM_STATE_KEYS")
    h.exact(boot_record, set(case.BOOT_FIELDS))
    all_expected = {**expected, h.AUTH[0]: cr._hash([(key, boot_record[key]) for key in case.BOOT_FIELDS])}
    present = {key for key, row in all_expected.items() if row is not None}
    h.require(inventory(client) == present, "CLAIM_INVENTORY")
    actual = {}
    for key, row in sorted(all_expected.items()):
        kind = client.call("TYPE", key)
        if row is None:
            h.require(kind == b"none", "CLAIM_REQUIRED_ABSENCE")
            actual[key] = None
            continue
        h.require(kind == row["type"].encode(), "CLAIM_TYPE")
        expiry = client.call("PEXPIRETIME", key)
        h.require(type(expiry) is int and expiry == row["expires_at_ms"], "CLAIM_EXPIRY")
        if row["type"] == "hash":
            fields = [name for name, _ in row["fields"]]
            h.require(0 < len(fields) <= 64 and len(set(fields)) == len(fields), "CLAIM_HASH_FIELDS")
            _count(client, "HLEN", key, len(fields))
            for name in fields:
                length = client.call("HSTRLEN", key, name)
                h.require(type(length) is int and 0 <= length <= 2048, "CLAIM_HASH_BOUND")
            values = client.call("HMGET", key, *fields)
            h.require(type(values) is list and len(values) == len(fields), "CLAIM_HASH_VALUES")
            actual[key] = cr._hash(list(zip(fields, [_text(value) for value in values])), expiry)
        elif row["type"] == "string":
            length = client.call("STRLEN", key)
            h.require(type(length) is int and 0 <= length <= 2048, "CLAIM_STRING_BOUND")
            actual[key] = {"type": "string", "value": _text(client.call("GET", key)), "expires_at_ms": expiry}
        elif row["type"] == "set":
            size = len(row["members"])
            h.require(0 < size <= MAX_KEYS, "CLAIM_SET_BOUND")
            _count(client, "SCARD", key, size)
            members = client.call("SMEMBERS", key)
            h.require(type(members) is list and len(members) == size, "CLAIM_SET_MEMBERS")
            texts = [_text(member, 256) for member in members]
            h.require(len(set(texts)) == len(texts), "CLAIM_DUPLICATE_MEMBER")
            actual[key] = cr._set(texts)
        elif row["type"] == "zset":
            size = len(row["members"])
            h.require(0 < size <= MAX_KEYS, "CLAIM_ZSET_BOUND")
            _count(client, "ZCARD", key, size)
            members = client.call("ZRANGE", key, "0", str(size - 1), "WITHSCORES")
            h.require(type(members) is list and len(members) == size * 2, "CLAIM_ZSET_MEMBERS")
            names = [_text(value, 256) for value in members[::2]]
            h.require(len(set(names)) == len(names), "CLAIM_DUPLICATE_MEMBER")
            actual[key] = cr._zset(dict(zip(names, [_number(value) for value in members[1::2]])))
        else:
            raise h.InvalidArtifact("CLAIM_STATE_TYPE")
    h.require(h.canonical(actual) == h.canonical(all_expected), "CLAIM_STATE_MISMATCH")
    return actual


def install(client, plan, fixture, boot_record):
    fixture = cr.validate_fixture(plan, fixture)
    state = fixture["initial_state"]
    # The entire finalized manifest is validated before the first data write.
    snapshot(client, {key: None for key in state}, boot_record)
    for key, row in sorted(state.items()):
        if row is None:
            continue
        if row["type"] == "hash":
            args = [value for field in row["fields"] for value in field]
            h.require(client.call("HSET", key, *args) == len(row["fields"]), "CLAIM_SETUP_HASH")
        elif row["type"] == "string":
            h.require(client.call("SET", key, row["value"]) == b"OK", "CLAIM_SETUP_STRING")
        elif row["type"] == "set":
            h.require(client.call("SADD", key, *row["members"]) == len(row["members"]), "CLAIM_SETUP_SET")
        elif row["type"] == "zset":
            args = [value for member, score in row["members"] for value in (score, member)]
            h.require(client.call("ZADD", key, *args) == len(row["members"]), "CLAIM_SETUP_ZSET")
        else:
            raise h.InvalidArtifact("CLAIM_SETUP_TYPE")
    return h.digest(h.canonical(snapshot(client, state, boot_record)))


def acl_probes(fixture):
    rows = []
    for index, key in enumerate(h.AUTH):
        commands = [("SET", key, "x"), ("HSET", key, "x", "x"), ("DEL", key),
                    ("EXPIRE", key, "1"), ("RENAME", key, fixture["job_key"])]
        if key in h.ABSENCE_ONLY:
            commands = [("GET", key), ("HGET", key, "x"), *commands]
        rows.extend((AUTH_ROLES[index], command) for command in commands)
    h.require(len(rows) == 46, "CLAIM_ACL_INVENTORY")
    return rows


def counters(observed, fixture):
    """Non-sensitive measurements from an already verified live snapshot."""
    result = {}
    for prefix, key, fields in (("run", fixture["base_key"], RUN_COUNTERS), ("job", fixture["job_key"], JOB_COUNTERS)):
        values = dict(observed[key]["fields"])
        for field in fields:
            result[prefix + "." + field] = _number(values[field].encode())
    for field in GROUP_COUNTERS:
        values = dict(observed[fixture["base_key"] + ":" + field]["fields"])
        result["group." + field] = _number(values[cr.GROUP].encode())
    for kind, scope in zip(("global", "group", "origin"), fixture["scope_ids"]):
        row = observed[h.P + "rate:" + scope]
        # Only the verified initial snapshot can lack these scope records.
        values = dict(row["fields"]) if row is not None else dict.fromkeys(SCOPE_COUNTERS, "0")
        for field in SCOPE_COUNTERS:
            result["scope." + kind + "." + field] = _number(values[field].encode())
    return result


def expected_counters(step):
    """Closed public receipt contract, independent of caller-supplied counts."""
    result = {}
    groups = (("run", RUN_COUNTERS), ("job", JOB_COUNTERS), ("group", GROUP_COUNTERS),
              ("scope.global", SCOPE_COUNTERS), ("scope.group", SCOPE_COUNTERS), ("scope.origin", SCOPE_COUNTERS))
    for prefix, fields in groups:
        result.update({prefix + "." + field: 0 for field in fields})
    claims = 0 if step < 0 else (1 if step < 5 else 2)
    live = int(step in (0, 1, 2, 5, 6))
    result.update({"run.job_count": 1, "run.open_job_count": 1, "group.group_open_jobs": 1,
                   "run.claims_total": claims, "run.reservation_creations_total": claims,
                   "job.claim_count": claims, "job.lease_fence": claims, "job.next_request_ordinal": claims + 1,
                   "run.pending_request_reservations": live, "group.group_pending": live})
    for kind in ("global", "group", "origin"):
        for field in ("active_count", "pending_count"):
            result["scope." + kind + "." + field] = live
    return result


def measure(request, observer, ledger):
    previous, plan = request["previous"], request["plan"]
    fixture = case.claim_fixture(plan, request["fixture_id"], request["claim_material"], previous["setup_time_ms"])
    summary = cr.public_summary(plan, fixture)
    h.require(h.canonical(summary) == h.canonical(previous["fixture_summary"]), "CLAIM_FIXTURE_BINDING")
    boot = previous["boot_record"]
    result = {"scope": cr.CASE, "fixture_sha256": summary["fixture_sha256"], "steps": [], "acl_negatives": []}
    assertion = "CR11"
    try:
        initial = snapshot(observer, fixture["initial_state"], boot)
        h.require(h.digest(h.canonical(initial)) == previous["setup_state_sha256"], "CLAIM_SETUP_CHANGED")
        before_counts = counters(initial, fixture)
        wires = cr.wire_requests(plan, fixture, previous["boot_epoch"])
        times = []
        final_state = None
        for index, wire in enumerate(wires):
            assertion = f"CR{index + 1:02}"
            reply = ledger.call(*wire)  # One dispatch; never retry an ambiguous write.
            h.require(type(reply) is list and len(reply) in (3, 6), "CLAIM_REPLY")
            now = _number(reply[1])
            times.append(now)
            # Future times are placeholders for projection only. This prefix's
            # expected state depends solely on the already observed replies.
            projected = cr.expected_sequence(plan, fixture, times + [now] * (9 - len(times)))[index]
            h.require(reply == [value.encode() for value in projected["reply"]], "CLAIM_REPLY_MISMATCH")
            final_state = projected["state"]
            actual = snapshot(observer, final_state, boot)
            after_counts = counters(actual, fixture)
            result["steps"].append({"assertion_id": assertion, "response_status": projected["reply"][0],
                "now_ms": now, "reply_sha256": h.digest(h.canonical(projected["reply"])),
                "state_sha256": h.digest(h.canonical(actual)),
                "counters": {"before": before_counts, "after": after_counts,
                             "delta": {key: after_counts[key] - before_counts[key] for key in after_counts}},
                "reservation_expiries": [{"reference_sha256": h.digest(key.encode()), "expires_at_ms": row["expires_at_ms"]}
                    for key, row in sorted(final_state.items()) if row is not None and key.startswith(h.P + "reservation:")]})
            before_counts = after_counts
        assertion = "CR10"
        for role, command in acl_probes(fixture):
            try:
                ledger.call(*command)
            except RedisError as error:
                h.require(error.code == "NOPERM", "CLAIM_ACL_DENIAL")
            else:
                raise h.InvalidArtifact("CLAIM_ACL_ESCALATION")
            state_sha = h.digest(h.canonical(snapshot(observer, final_state, boot)))
            result["acl_negatives"].append({"command": command[0], "authority_role": role, "result": "NOPERM", "state_sha256": state_sha})
        result["final_state_sha256"] = result["steps"][-1]["state_sha256"]
        return result
    except Exception:
        result["failed_assertion"] = assertion
        raise MeasurementFailure(result) from None


def validate_measurement(result, successful):
    """Closed report schema; no raw replies, keys, owners, tokens or exception text."""
    h.exact(result, {"scope", "fixture_sha256", "steps", "acl_negatives", "final_state_sha256" if successful else "failed_assertion"})
    h.require(result["scope"] == cr.CASE and h.nonzero(result["fixture_sha256"]), "CLAIM_EVIDENCE")
    steps, negatives = result["steps"], result["acl_negatives"]
    h.require(type(steps) is list and len(steps) <= 9 and type(negatives) is list and len(negatives) <= 46, "CLAIM_EVIDENCE_BOUND")
    statuses = ("CLAIMED", "ALREADY_CLAIMED", "LEASE_LOST", "RELEASED_READY", "RELEASED_READY", "CLAIMED", "LEASE_LOST", "RELEASED_READY", "RELEASED_READY")
    previous_time = 0
    for index, row in enumerate(steps):
        h.exact(row, {"assertion_id", "response_status", "now_ms", "reply_sha256", "state_sha256", "reservation_expiries", "counters"})
        h.require(row["assertion_id"] == f"CR{index + 1:02}" and row["response_status"] == statuses[index] and
                  type(row["now_ms"]) is int and previous_time <= row["now_ms"] <= h.MAX_EXACT and row["now_ms"] > 0 and
                  h.nonzero(row["reply_sha256"]) and h.nonzero(row["state_sha256"]), "CLAIM_STEP_EVIDENCE")
        previous_time = row["now_ms"]
        before, after = expected_counters(index - 1), expected_counters(index)
        h.exact(row["counters"], {"before", "after", "delta"})
        for name, expected in (("before", before), ("after", after),
                               ("delta", {key: after[key] - before[key] for key in after})):
            measured = row["counters"][name]
            h.exact(measured, set(expected))
            h.require(all(type(value) is int for value in measured.values()) and measured == expected, "CLAIM_COUNTER_EVIDENCE")
        h.require(type(row["reservation_expiries"]) is list and len(row["reservation_expiries"]) == (1 if index < 5 else 2), "CLAIM_EXPIRY_EVIDENCE")
        for expiry in row["reservation_expiries"]:
            h.exact(expiry, {"reference_sha256", "expires_at_ms"})
            h.require(h.nonzero(expiry["reference_sha256"]) and type(expiry["expires_at_ms"]) is int and
                      (expiry["expires_at_ms"] == -1 or 0 < expiry["expires_at_ms"] <= h.MAX_EXACT), "CLAIM_EXPIRY_EVIDENCE")
    expected_probes = [(role, command[0]) for role, command in acl_probes({"job_key": "unused"})]
    for index, row in enumerate(negatives):
        h.exact(row, {"command", "authority_role", "result", "state_sha256"})
        h.require((row["authority_role"], row["command"]) == expected_probes[index] and row["result"] == "NOPERM" and
                  len(steps) == 9 and row["state_sha256"] == steps[-1]["state_sha256"], "CLAIM_ACL_EVIDENCE")
    if successful:
        h.require(len(steps) == 9 and len(negatives) == 46 and result["final_state_sha256"] == steps[-1]["state_sha256"], "CLAIM_EVIDENCE_INCOMPLETE")
    else:
        h.require(result["failed_assertion"] in {f"CR{i:02}" for i in range(1, 13)}, "CLAIM_FAILURE_EVIDENCE")


def validate_stage_result(stage, result, request):
    """Validate the claim case's public stage boundary before controller retention."""
    if stage == "measure":
        validate_measurement(result, True)
        h.require(result["fixture_sha256"] == request["previous"]["fixture_summary"]["fixture_sha256"], "CLAIM_EVIDENCE_BINDING")
    elif stage == "init":
        h.exact(result, {"empty_volumes_verified", "config_sha256", "acl_file_sha256"})
    elif stage == "ready":
        h.exact(result, {"run_id"})
    elif stage == "probe":
        h.exact(result, {"old_run_id", "acknowledged", "at_ms", "key", "type", "value", "expiry", "value_sha256"})
        value = "m4-probe:" + request["fixture_id"] + ":" + h.digest(h.canonical(request["plan"]))
        h.require(result["value"] == value and result["value_sha256"] == h.digest(value.encode()) and
                  result["key"] == case.PROBE and result["type"] == "string" and result["expiry"] == "persistent" and
                  result["acknowledged"] is True and type(result["at_ms"]) is int and 0 < result["at_ms"] <= h.MAX_EXACT, "CLAIM_PROBE_EVIDENCE")
    elif stage == "resume":
        h.exact(result, {"probe_evidence", "probe_evidence_sha256", "boot_epoch", "boot_record", "setup_time_ms",
                         "fixture_summary", "setup_state_sha256", "setup_time_observed", "early_revocation"})
        h.exact(result["boot_record"], set(case.BOOT_FIELDS))
        h.require(all(type(value) is str and len(value) <= 2048 for value in result["boot_record"].values()), "CLAIM_BOOT_EVIDENCE")
        proof = result["probe_evidence"]
        h.exact(proof, {"case", "fixture_id", "plan_sha256", "old_run_id", "new_run_id", "acknowledged_probe_sha256", "verified_at_ms", "acknowledged_loss_bound"})
        h.require(proof["case"] == cr.CASE and proof["fixture_id"] == request["fixture_id"] and
                  proof["plan_sha256"] == h.digest(h.canonical(request["plan"])) and
                  proof["old_run_id"] == request["previous"]["old_run_id"] and proof["new_run_id"] != proof["old_run_id"] and
                  proof["acknowledged_probe_sha256"] == request["previous"]["value_sha256"] and
                  type(proof["acknowledged_loss_bound"]) is int and proof["acknowledged_loss_bound"] == 0 and
                  result["probe_evidence_sha256"] == h.digest(h.canonical(proof)), "CLAIM_PROBE_EVIDENCE")
        h.require(result["boot_record"]["boot_epoch"] == result["boot_epoch"] and
                  result["boot_record"]["boot_state"] == "approved" and result["boot_record"]["approved_redis_run_id"] == proof["new_run_id"] and
                  result["boot_record"]["rehearsal_evidence_sha256"] == result["probe_evidence_sha256"] and
                  result["setup_time_observed"] is True and h.nonzero(result["setup_state_sha256"]), "CLAIM_SETUP_EVIDENCE")
        fixture = case.claim_fixture(request["plan"], request["fixture_id"], request["claim_material"], result["setup_time_ms"])
        h.require(h.canonical(result["fixture_summary"]) == h.canonical(cr.public_summary(request["plan"], fixture)), "CLAIM_SUMMARY_EVIDENCE")
    elif stage == "revoke":
        h.exact(result, {"revocation"})
    else:
        raise h.InvalidArtifact("CLAIM_STAGE")
