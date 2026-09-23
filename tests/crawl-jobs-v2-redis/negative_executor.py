"""Bounded M4-P3 stages and closed, value-redacted evidence validation."""
from __future__ import annotations

import copy
import re

import harness as h
import runtime_case as case
import negative_cases as nc
import negative_specs as ns
import claim_release as cr
import claim_executor as claim
import bounded_state as state
from resp import RedisError


class NegativeFailure(h.InvalidArtifact):
    def __init__(self, result):
        self.result = result
        super().__init__("NEGATIVE_STAGE_FAILED")


def digest_state(observer, expected):
    actual = state.snapshot(observer, expected)
    return actual, h.digest(h.canonical(actual))


def install(client, plan, fixture, boot):
    fixture = nc.validate_fixture(plan, fixture)
    state.snapshot(client, state.with_boot({key: None for key in fixture["initial_state"]}, boot))
    for key, row in sorted(fixture["initial_state"].items()):
        if row is None:
            continue
        kind = row["type"]
        if kind == "hash":
            flat = [value for pair in row["fields"] for value in pair]
            h.require(client.call("HSET", key, *flat) == len(row["fields"]), "NEGATIVE_SETUP_HASH")
        elif kind == "string":
            h.require(client.call("SET", key, row["value"]) == b"OK", "NEGATIVE_SETUP_STRING")
        elif kind == "set":
            h.require(client.call("SADD", key, *row["members"]) == len(row["members"]), "NEGATIVE_SETUP_SET")
        elif kind == "zset":
            flat = [value for member, score in row["members"] for value in (score, member)]
            h.require(client.call("ZADD", key, *flat) == len(row["members"]), "NEGATIVE_SETUP_ZSET")
        else:
            raise h.InvalidArtifact("NEGATIVE_SETUP_KIND")
    return digest_state(client, state.with_boot(fixture["initial_state"], boot))[1]


def reply_time(reply, before, after):
    h.require(type(reply) is list and 3 <= len(reply) <= 6 and all(type(value) is bytes for value in reply), "NEGATIVE_REPLY")
    now = state.number(reply[1])
    h.require(before <= now <= after, "NEGATIVE_REPLY_TIME")
    return now


def exact_reply(reply, expected):
    h.require(reply == [value.encode() for value in expected], "NEGATIVE_REPLY_MISMATCH")
    return h.digest(h.canonical(expected))


def prefix_count(case_id):
    return (0, 1, 2)[(ns.INSTALL, ns.RETIRE, ns.PROMOTE).index(ns.ADMIN[case_id][1])] if case_id in ns.ADMIN else 0


def resume(request, setup, current, evidence, epoch, api):
    plan, credentials = request["plan"], request["credentials"]
    selected = case.case_for_plan(plan)
    evidence_sha = h.digest(h.canonical(evidence))
    nc.validate_probe_evidence(plan, request["fixture_id"], request["previous"], evidence, evidence_sha)
    boot = None
    if selected != ns.BOOT:
        wire = nc.boot_wire(current["run_id"], epoch, evidence_sha, evidence["verified_at_ms"])
        with api.connect("boot", credentials) as client:
            before = api.clock(setup)
            first = client.call(*wire)
            after = api.clock(setup)
            now = reply_time(first, before, after)
            exact_reply(first, ["OK", str(now), epoch])
            boot = nc.boot_record(wire, now)
            replay = client.call(*wire)
            replay_now = reply_time(replay, now, api.clock(setup))
            exact_reply(replay, ["EXISTS_IDENTICAL", str(replay_now), epoch])
        h.require(api.read_hash(setup, h.AUTH[0], case.BOOT_FIELDS) == boot, "NEGATIVE_BOOT_STATE")
    at = api.clock(setup)
    fixture = case.fixture(plan, request["fixture_id"], request.get("claim_material", {}), at)
    binding = case.fixture(plan, request["fixture_id"], request.get("claim_material", {}))
    h.require(case.acl_rules(selected, binding, plan) == case.acl_rules(selected, fixture, plan), "ACL_BINDING_DRIFT")
    summary = nc.public_summary(plan, fixture)
    result = {"probe_evidence": evidence, "probe_evidence_sha256": evidence_sha, "boot_epoch": epoch, "boot_record": boot,
              "setup_time_observed": True, "setup_time_ms": at, "fixture_summary": summary,
              "setup_state_sha256": install(setup, plan, fixture, boot), "early_revocation": {},
              "prefix_steps": [], "admin_observations": None}
    try:
        result["early_revocation"] = api.revoke(credentials, ("setup", "loader") if selected == ns.BOOT else case.EARLY_ROLES)
        if selected in ns.ADMIN:
            with api.connect("observer", credentials) as observer:
                _, empty_sha = digest_state(observer, state.with_boot(fixture["initial_state"], boot))
                isolation = api.environment()
                observed = nc.admin_observations(plan, fixture, boot, isolation["process_inventory_sha256"], api.clock(observer))
                h.require(observed["empty_inventory_sha256"] == empty_sha, "ADMIN_EMPTY_STATE")
                result["admin_observations"] = observed
                times = []
                for index in range(prefix_count(selected)):
                    provisional = api.clock(observer)
                    projected = nc.admin_projection(plan, fixture, observed, epoch, times + [provisional] * (3 - len(times)))[index]
                    actor = "release_admin" if index == 0 else "migration_admin"
                    with api.connect(actor, credentials) as client:
                        before = api.clock(observer)
                        reply = client.call(*projected["wire"])
                        now = reply_time(reply, before, api.clock(observer))
                    times.append(now)
                    projected = nc.admin_projection(plan, fixture, observed, epoch, times + [now] * (3 - len(times)))[index]
                    response_sha = exact_reply(reply, projected["reply"])
                    _, state_sha = digest_state(observer, state.with_boot(projected["state"], boot))
                    result["prefix_steps"].append({"operation": projected["operation"], "response_status": projected["reply"][0],
                        "now_ms": now, "reply_sha256": response_sha, "state_sha256": state_sha})
                    result["setup_state_sha256"] = state_sha
                    if actor in case.early_roles(selected):
                        result["early_revocation"].update(api.revoke(credentials, (actor,)))
        return result
    except Exception:
        result["failed_assertion"] = "RESUME"
        raise NegativeFailure(result) from None


def counts(observed, fixture):
    return {} if fixture["case"] in ns.ADMIN or fixture["case"] == ns.BOOT else claim.counters(observed, fixture["worker"])


def initial_state(plan, fixture, previous):
    if fixture["case"] in ns.ADMIN and previous["prefix_steps"]:
        times = [row["now_ms"] for row in previous["prefix_steps"]]
        projected = nc.admin_projection(plan, fixture, previous["admin_observations"], previous["boot_epoch"],
                                        times + [times[-1]] * (3 - len(times)))
        return projected[len(times) - 1]["state"]
    return fixture["initial_state"]


def measure(request, api):
    plan, previous, credentials = request["plan"], request["previous"], request["credentials"]
    selected = case.case_for_plan(plan)
    fixture = case.fixture(plan, request["fixture_id"], request.get("claim_material", {}), previous["setup_time_ms"])
    summary = nc.public_summary(plan, fixture)
    h.require(h.canonical(summary) == h.canonical(previous["fixture_summary"]), "NEGATIVE_FIXTURE_BINDING")
    managed, boot = initial_state(plan, fixture, previous), previous["boot_record"]
    expected = state.with_boot(managed, boot)
    result = {"scope": selected, "fixture_sha256": summary["fixture_sha256"], "steps": [], "acl_negatives": [],
              "counter_scope": "not_applicable" if selected in ns.ADMIN or selected == ns.BOOT else "run_job_group_scopes",
              "retired_roles": {}}
    assertion = "STATE"
    try:
        with api.connect("observer", credentials) as observer:
            h.require(api.server(observer)["run_id"] == previous["probe_evidence"]["new_run_id"], "BOOT_CHANGED")
            actual, current_sha = digest_state(observer, expected)
            h.require(current_sha == previous["setup_state_sha256"], "NEGATIVE_SETUP_CHANGED")
            before_counts = counts(actual, fixture)
            reference = api.clock(observer)
            result["clock_reference_ms"] = reference
            jobs, times = [], []
            if selected in (*ns.STORED, ns.WIRE):
                for label, code, wire in nc.ledger_wires(plan, fixture, previous["boot_epoch"]):
                    jobs.extend([(label, "ledger", code, wire, None)] * 2)
                if selected == ns.WIRE:
                    wires = cr.wire_requests(plan, fixture["worker"], previous["boot_epoch"])
                    jobs += [("WP01", "ledger", "CLAIMED", wires[0], "claim"), ("WP01", "ledger", "RELEASED_READY", wires[3], "release")]
            elif selected == ns.BOOT:
                wire = nc.boot_wire(previous["probe_evidence"]["new_run_id"], previous["boot_epoch"],
                                    previous["probe_evidence_sha256"], previous["probe_evidence"]["verified_at_ms"])
                for label, code, mutated in nc.boot_wires(wire, previous["probe_evidence"]["old_run_id"], reference):
                    jobs.extend([(label, "boot", code, mutated, None)] * 2)
                jobs += [("BP01", "boot", "OK", wire, "boot"), ("BP01", "boot", "EXISTS_IDENTICAL", wire, "boot_replay")]
            else:
                label, _, code, actor, status = ns.ADMIN[selected]
                prefix_times = [row["now_ms"] for row in previous["prefix_steps"]]
                projected = nc.admin_projection(plan, fixture, previous["admin_observations"], previous["boot_epoch"],
                                                prefix_times + [reference] * (3 - len(prefix_times)))[prefix_count(selected)]
                jobs = [(label, "ledger", code, projected["wire"], None)] * 2 + [(label, actor, status, projected["wire"], "admin")]
            h.require(len(jobs) <= 32, "NEGATIVE_CALL_BOUND")
            for assertion, actor, wanted, wire, positive in jobs:
                before = api.clock(observer)
                now = None
                with api.connect(actor, credentials) as client:
                    if positive is None:
                        try:
                            client.call(*wire)
                        except RedisError as error:
                            h.require(error.code == wanted, "NEGATIVE_REJECTION")
                        else:
                            raise h.InvalidArtifact("NEGATIVE_UNEXPECTED_SUCCESS")
                        response_sha = h.digest(h.canonical({"error": wanted}))
                    else:
                        reply = client.call(*wire)
                        now = reply_time(reply, before, api.clock(observer))
                        if positive in ("claim", "release"):
                            times.append(now)
                            oracle_times = [times[0]] * 3 + [now] * 6
                            projected = cr.expected_sequence(plan, fixture["worker"], oracle_times)[0 if positive == "claim" else 3]
                            managed = projected["state"]
                            expected_reply = projected["reply"]
                        elif positive.startswith("boot"):
                            if positive == "boot":
                                boot = nc.boot_record(wire, now)
                            expected_reply = [wanted, str(now), previous["boot_epoch"]]
                        else:
                            projected = nc.admin_projection(plan, fixture, previous["admin_observations"], previous["boot_epoch"],
                                                            prefix_times + [now] * (3 - len(prefix_times)))[prefix_count(selected)]
                            managed, expected_reply = projected["state"], projected["reply"]
                        response_sha = exact_reply(reply, expected_reply)
                        expected = state.with_boot(managed, boot)
                after = api.clock(observer)
                h.require(reference <= before <= after <= reference + 30000, "NEGATIVE_TIME_BOUND")
                actual, state_sha = digest_state(observer, expected)
                after_counts = counts(actual, fixture)
                if positive is None:
                    h.require(state_sha == current_sha and before_counts == after_counts, "NEGATIVE_MUTATION")
                result["steps"].append({"sequence": len(result["steps"]), "assertion_id": assertion, "actor": actor,
                    "response_code": wanted, "response_kind": "error" if positive is None else "reply", "now_ms": now,
                    "started_at_ms": before, "finished_at_ms": after, "response_sha256": response_sha, "state_sha256": state_sha,
                    "counters": {"before": before_counts, "after": after_counts,
                                 "delta": {key: after_counts[key] - before_counts[key] for key in after_counts}}})
                current_sha, before_counts = state_sha, after_counts
            if selected in (*ns.STORED, ns.WIRE):
                assertion = "ACL"
                with api.connect("ledger", credentials) as ledger:
                    for authority, command in claim.acl_probes(fixture["worker"]):
                        try:
                            ledger.call(*command)
                        except RedisError as error:
                            h.require(error.code == "NOPERM", "NEGATIVE_ACL_DENIAL")
                        else:
                            raise h.InvalidArtifact("NEGATIVE_ACL_ESCALATION")
                        _, state_sha = digest_state(observer, expected)
                        h.require(state_sha == current_sha, "NEGATIVE_ACL_MUTATION")
                        result["acl_negatives"].append({"authority_role": authority, "command": command[0], "result": "NOPERM", "state_sha256": state_sha})
            assertion = "RETIRE"
            retired = ("boot",) if selected == ns.BOOT else ((ns.ADMIN[selected][3],) if selected in ns.ADMIN else ())
            if retired:
                result["retired_roles"] = api.revoke(credentials, retired)
            result["final_state_sha256"] = current_sha
            return result
    except Exception:
        # A missing reference means the clock observation itself failed. No
        # successful step is inferred and the entire case still fails.
        result.setdefault("clock_reference_ms", None)
        result["failed_assertion"] = assertion
        raise NegativeFailure(result) from None


def validate_measurement(result, successful, request):
    selected, previous, plan = case.case_for_plan(request["plan"]), request["previous"], request["plan"]
    h.exact(result, {"scope", "fixture_sha256", "steps", "acl_negatives", "counter_scope", "retired_roles", "clock_reference_ms",
                     "final_state_sha256" if successful else "failed_assertion"})
    h.require(result["scope"] == selected and result["fixture_sha256"] == previous["fixture_summary"]["fixture_sha256"], "NEGATIVE_EVIDENCE_BINDING")
    fixture = case.fixture(plan, request["fixture_id"], request.get("claim_material", {}), previous["setup_time_ms"])
    h.require(result["fixture_sha256"] == nc.public_summary(plan, fixture)["fixture_sha256"], "NEGATIVE_EVIDENCE_BINDING")
    sequence = ns.measurement_sequence(selected)
    steps, probes = result["steps"], result["acl_negatives"]
    h.require(type(steps) is list and len(steps) <= len(sequence) and type(probes) is list and len(probes) <= 46, "NEGATIVE_EVIDENCE_BOUND")
    no_job = selected in ns.ADMIN or selected == ns.BOOT
    h.require(result["counter_scope"] == ("not_applicable" if no_job else "run_job_group_scopes"), "NEGATIVE_COUNTER_SCOPE")
    reference = result["clock_reference_ms"]
    if reference is None:
        h.require(not successful and not steps and not probes, "NEGATIVE_MISSING_CLOCK")
    else:
        h.require(type(reference) is int and previous["setup_time_ms"] <= reference <= previous["setup_time_ms"] + 300000, "NEGATIVE_CLOCK_EVIDENCE")
    managed, boot = initial_state(plan, fixture, previous), previous["boot_record"]
    expected = state.with_boot(managed, boot)
    current_sha = h.digest(h.canonical(expected))
    h.require(current_sha == previous["setup_state_sha256"], "NEGATIVE_STATE_BINDING")
    before_counts = {} if no_job else claim.expected_counters(-1)
    last = reference
    times = []
    for index, row in enumerate(steps):
        h.exact(row, {"sequence", "assertion_id", "actor", "response_code", "response_kind", "now_ms", "started_at_ms", "finished_at_ms",
                      "response_sha256", "state_sha256", "counters"})
        assertion, actor, code = sequence[index]
        negative = code == "NOPERM" or code.startswith("CRAWL_V2_")
        h.require(type(row["sequence"]) is int and row["sequence"] == index and
                  (row["assertion_id"], row["actor"], row["response_code"]) == (assertion, actor, code) and
                  row["response_kind"] == ("error" if negative else "reply") and
                  type(row["started_at_ms"]) is int and type(row["finished_at_ms"]) is int and
                  last <= row["started_at_ms"] <= row["finished_at_ms"] <= min(reference + 30000, h.MAX_EXACT), "NEGATIVE_STEP_EVIDENCE")
        last = row["finished_at_ms"]
        after_counts = before_counts
        if negative:
            h.require(row["now_ms"] is None, "NEGATIVE_ERROR_CLOCK")
            response_sha = h.digest(h.canonical({"error": code}))
        else:
            now = row["now_ms"]
            h.require(type(now) is int and row["started_at_ms"] <= now <= row["finished_at_ms"], "NEGATIVE_REPLY_CLOCK")
            if selected == ns.WIRE:
                times.append(now)
                projected = cr.expected_sequence(plan, fixture["worker"], [times[0]] * 3 + [now] * 6)[0 if code == "CLAIMED" else 3]
                managed, reply = projected["state"], projected["reply"]
                after_counts = claim.expected_counters(0 if code == "CLAIMED" else 3)
            elif selected == ns.BOOT:
                wire = nc.boot_wire(previous["probe_evidence"]["new_run_id"], previous["boot_epoch"], previous["probe_evidence_sha256"],
                                    previous["probe_evidence"]["verified_at_ms"])
                if code == "OK":
                    boot = nc.boot_record(wire, now)
                reply = [code, str(now), previous["boot_epoch"]]
            else:
                prefix = [value["now_ms"] for value in previous["prefix_steps"]]
                projected = nc.admin_projection(plan, fixture, previous["admin_observations"], previous["boot_epoch"], prefix + [now] * (3 - len(prefix)))[prefix_count(selected)]
                managed, reply = projected["state"], projected["reply"]
            current_sha = h.digest(h.canonical(state.with_boot(managed, boot)))
            response_sha = h.digest(h.canonical(reply))
        h.require(row["state_sha256"] == current_sha and row["response_sha256"] == response_sha, "NEGATIVE_STATE_EVIDENCE")
        h.exact(row["counters"], {"before", "after", "delta"})
        for name, wanted in (("before", before_counts), ("after", after_counts),
                             ("delta", {key: after_counts[key] - before_counts[key] for key in after_counts})):
            values = row["counters"][name]
            h.exact(values, set(wanted))
            h.require(all(type(value) is int for value in values.values()) and values == wanted, "NEGATIVE_COUNTER_EVIDENCE")
        before_counts = after_counts
    wanted_probes = claim.acl_probes(fixture["worker"]) if selected in (*ns.STORED, ns.WIRE) else []
    h.require(len(probes) <= len(wanted_probes), "NEGATIVE_ACL_BOUND")
    for index, row in enumerate(probes):
        authority, command = wanted_probes[index]
        h.exact(row, {"authority_role", "command", "result", "state_sha256"})
        h.require(len(steps) == len(sequence) and row == {"authority_role": authority, "command": command[0],
                  "result": "NOPERM", "state_sha256": current_sha}, "NEGATIVE_ACL_EVIDENCE")
    retired = ("boot",) if selected == ns.BOOT else ((ns.ADMIN[selected][3],) if selected in ns.ADMIN else ())
    if successful:
        h.require(len(steps) == len(sequence) and len(probes) == len(wanted_probes) and result["final_state_sha256"] == current_sha, "NEGATIVE_INCOMPLETE")
        case.validate_revocation(result["retired_roles"], retired)
    else:
        h.require(result["failed_assertion"] in {"STATE", "ACL", "RETIRE", *[row[0] for row in sequence]}, "NEGATIVE_FAILURE_EVIDENCE")
        h.require(type(result["retired_roles"]) is dict and set(result["retired_roles"]) <= set(retired), "NEGATIVE_RETIREMENT")
        case.validate_revocation(result["retired_roles"], tuple(result["retired_roles"]))


def validate_stage_result(stage, result, request, isolation=None, successful=True):
    selected, plan = case.case_for_plan(request["plan"]), request["plan"]
    if stage == "measure":
        validate_measurement(result, successful, request)
        return
    if stage != "resume":
        h.require(successful, "NEGATIVE_FAILURE_STAGE")
        if stage == "init":
            h.exact(result, {"empty_volumes_verified", "config_sha256", "acl_file_sha256"})
        elif stage == "ready":
            h.exact(result, {"run_id"})
        elif stage == "probe":
            h.exact(result, {"old_run_id", "acknowledged", "at_ms", "key", "type", "value", "expiry", "value_sha256"})
            value = "m4-probe:" + request["fixture_id"] + ":" + h.digest(h.canonical(plan))
            h.require(result["value"] == value and result["value_sha256"] == h.digest(value.encode()) and result["acknowledged"] is True and
                      result["key"] == h.AUTH[2] and result["type"] == "string" and result["expiry"] == "persistent" and
                      type(result["at_ms"]) is int and 0 < result["at_ms"] <= h.MAX_EXACT and
                      type(result["old_run_id"]) is str and re.fullmatch(r"[0-9a-f]{40}", result["old_run_id"]), "BOOT_PROVENANCE")
        elif stage == "revoke":
            h.exact(result, {"revocation"})
        else:
            raise h.InvalidArtifact("NEGATIVE_STAGE")
        return
    fields = {"probe_evidence", "probe_evidence_sha256", "boot_epoch", "boot_record", "setup_time_observed", "setup_time_ms",
              "fixture_summary", "setup_state_sha256", "early_revocation", "prefix_steps", "admin_observations"}
    h.exact(result, fields | (set() if successful else {"failed_assertion"}))
    if not successful:
        h.require(result["failed_assertion"] == "RESUME", "NEGATIVE_RESUME_FAILURE")
    nc.validate_probe_evidence(plan, request["fixture_id"], request["previous"], result["probe_evidence"], result["probe_evidence_sha256"])
    h.require(result["setup_time_observed"] is True and type(result["setup_time_ms"]) is int and
              result["probe_evidence"]["verified_at_ms"] <= result["setup_time_ms"] <= h.MAX_EXACT and
              cr._hex(result["boot_epoch"], 32), "NEGATIVE_SETUP_EVIDENCE")
    fixture = case.fixture(plan, request["fixture_id"], request.get("claim_material", {}), result["setup_time_ms"])
    h.require(h.canonical(result["fixture_summary"]) == h.canonical(nc.public_summary(plan, fixture)), "NEGATIVE_FIXTURE_EVIDENCE")
    if selected == ns.BOOT:
        h.require(result["boot_record"] is None, "NEGATIVE_BOOT_NOT_YET_APPROVED")
    else:
        boot = result["boot_record"]
        h.exact(boot, set(case.BOOT_FIELDS))
        now = boot.get("approved_at_ms")
        h.require(type(now) is str and re.fullmatch(r"[1-9][0-9]{0,15}", now) and
                  result["probe_evidence"]["verified_at_ms"] <= int(now) <= result["setup_time_ms"], "NEGATIVE_BOOT_TIME")
        wire = nc.boot_wire(result["probe_evidence"]["new_run_id"], result["boot_epoch"], result["probe_evidence_sha256"],
                            result["probe_evidence"]["verified_at_ms"])
        h.require(h.canonical(boot) == h.canonical(nc.boot_record(wire, int(now))), "NEGATIVE_BOOT_EVIDENCE")
    prefix = result["prefix_steps"]
    h.require(type(prefix) is list and len(prefix) <= prefix_count(selected), "NEGATIVE_PREFIX_BOUND")
    if selected in ns.ADMIN:
        observed = result["admin_observations"]
        if observed is None:
            h.require(not successful and not prefix, "ADMIN_PROVENANCE")
        else:
            nc.validate_admin_observations(plan, fixture, observed)
            h.require(type(isolation) is dict and observed["process_inventory_sha256"] == isolation.get("process_inventory_sha256"), "ADMIN_PROCESS_BINDING")
            expected_obs = nc.admin_observations(plan, fixture, result["boot_record"], observed["process_inventory_sha256"], observed["captured_at_ms"])
            h.require(h.canonical(observed) == h.canonical(expected_obs), "ADMIN_OBSERVATION_BINDING")
    else:
        h.require(result["admin_observations"] is None, "ADMIN_PROVENANCE")
    managed = fixture["initial_state"]
    times = []
    for index, row in enumerate(prefix):
        h.exact(row, {"operation", "response_status", "now_ms", "reply_sha256", "state_sha256"})
        h.require(type(row["now_ms"]) is int, "NEGATIVE_PREFIX_TIME")
        times.append(row["now_ms"])
        projected = nc.admin_projection(plan, fixture, result["admin_observations"], result["boot_epoch"], times + [times[-1]] * (3 - len(times)))[index]
        h.require(row["operation"] == projected["operation"] and row["response_status"] == projected["reply"][0] and
                  row["reply_sha256"] == h.digest(h.canonical(projected["reply"])) and
                  row["state_sha256"] == h.digest(h.canonical(state.with_boot(projected["state"], result["boot_record"]))), "NEGATIVE_PREFIX_EVIDENCE")
        managed = projected["state"]
    h.require(result["setup_state_sha256"] == h.digest(h.canonical(state.with_boot(managed, result["boot_record"]))), "NEGATIVE_SETUP_BINDING")
    if successful:
        h.require(len(prefix) == prefix_count(selected), "NEGATIVE_PREFIX_INCOMPLETE")
        case.validate_revocation(result["early_revocation"], case.early_roles(selected))
    else:
        h.require(type(result["early_revocation"]) is dict and set(result["early_revocation"]) <= set(case.early_roles(selected)), "NEGATIVE_EARLY_ROLES")
        case.validate_revocation(result["early_revocation"], tuple(result["early_revocation"]))
