"""Private deterministic negative fixtures, exact wires and independent projections.

No connection, mutation dispatcher or caller-supplied setup/command is accepted.
All returned state/wire objects are private; public_summary is the export boundary.
"""
from __future__ import annotations

import copy
import hashlib
from pathlib import Path
import re

import harness as h
import claim_release as cr
import negative_specs as ns
import resp


def selected(plan):
    h.require(type(plan) is dict and type(plan.get("inputs")) is dict, "NEGATIVE_PLAN")
    scenario = plan["inputs"].get("scenario")
    h.require(type(scenario) is str and scenario in ns.CASES.values(), "NEGATIVE_CASE")
    return next(name for name, value in ns.CASES.items() if value == scenario)


def worker_plan(plan):
    # Administrative fixtures use ONLY the derived key namespace of a ledger
    # projection. Its authority/job rows must never be installed in that profile.
    if selected(plan) in ns.ADMIN:
        return h.compile_plan(dict(plan["inputs"], scenario=cr.SCENARIO))
    return plan


def string(value):
    return {"type": "string", "value": value, "expires_at_ms": -1}


def freeze(plan, fixture_id, now, process_sha):
    h.require(h.nonzero(process_sha) and type(now) is int and 0 < now <= h.MAX_EXACT, "FREEZE_INPUT")
    return h.packed([("protocol_version", "2"), ("freeze_nonce", nonce(plan, fixture_id)),
        ("process_stop_evidence_sha256", process_sha), ("candidate_manifest_sha256", plan["compatibility"]["sha256"]),
        ("candidate_contract_sha256", plan["identities"]["contract_sha256"]), ("created_at_ms", str(now))])


def nonce(plan, fixture_id):
    h.require(cr._hex(fixture_id, 32), "FIXTURE_ID")
    return h.digest((selected(plan) + ":" + fixture_id + ":freeze").encode())[:32]


def compile_fixture(plan, inputs):
    plan = h.validate_plan(plan)
    case_id = selected(plan)
    if case_id == ns.BOOT:
        h.exact(inputs, {"fixture_id", "redis_time_ms"})
        h.require(cr._hex(inputs["fixture_id"], 32) and type(inputs["redis_time_ms"]) is int and
                  0 < inputs["redis_time_ms"] <= h.MAX_EXACT - 600000, "BOOT_FIXTURE_INPUT")
        worker, state = None, {h.AUTH[2]: None}
        keys = sorted((h.AUTH[0], h.AUTH[2]))
    else:
        worker = cr.compile_fixture(worker_plan(plan), inputs)
        keys = worker["key_inventory"]
        if case_id in ns.ADMIN:
            keys = sorted(set(keys) | set(h.LEGACY) | set(ns.DOWNSTREAM))
            state = {key: None for key in keys if key != h.AUTH[0]}
        else:
            state = copy.deepcopy(worker["initial_state"])
            code = ns.STORED.get(case_id, (None,))[0]
            if code == "P01":
                state[h.AUTH[3]] = cr._hash(plan["compatibility_marker"]["fields"])
            elif code == "P02":
                state[h.AUTH[4]] = string(plan["identities"]["contract_sha256"])
            elif code == "P03":
                # Explicit malformed combined active state, never admin evidence.
                proof = h.digest(h.canonical({"purpose": "negative_stored_state", "case": case_id, "fixture_id": inputs["fixture_id"]}))
                state[h.AUTH[7]] = cr._hash(freeze(plan, inputs["fixture_id"], inputs["redis_time_ms"], proof)["fields"])
            elif code == "S01":
                state[h.AUTH[1]] = None
            elif code == "S02":
                state[h.AUTH[2]] = cr._hash([("fixture_invalid", "1")])
            elif code == "S03":
                value = h.digest(b"bootstrap-acl-negatives-v1:other-contract")
                h.require(value != plan["identities"]["contract_sha256"], "NEGATIVE_COLLISION")
                state[h.AUTH[2]] = string(value)
            elif code == "S04":
                cr._change(state[h.AUTH[5]], approved_at_ms=inputs["redis_time_ms"] - 1)
            elif code == "S05":
                state[h.AUTH[1]]["fields"].append(["fixture_invalid", "1"])
    expected_keys = 2 if case_id == ns.BOOT else (71 if case_id in ns.ADMIN else 58)
    h.require(len(keys) == expected_keys and set(state) == set(keys) - {h.AUTH[0]}, "NEGATIVE_INVENTORY")
    return h.decode(h.canonical({"version": 1, "case": case_id, "artifact_kind": "private_offline_negative_fixture",
        "purpose": "conformance_only", "execution_authorized": False, "release_eligible": False,
        "measurement_status": "not_measured", "time_observation_verified": False,
        "plan_sha256": h.digest(h.canonical(plan)), "compiler_sha256": h.digest(Path(__file__).read_bytes()),
        "inputs": inputs, "worker": worker, "key_inventory": keys, "initial_state": state,
        "bootstrap_owned_keys": [h.AUTH[0]], "admin_nonce": nonce(plan, inputs["fixture_id"])}))


def validate_fixture(plan, fixture):
    h.require(type(fixture) is dict and type(fixture.get("inputs")) is dict, "NEGATIVE_FIXTURE")
    expected = compile_fixture(plan, fixture["inputs"])
    h.require(cr._bounded(fixture) == h.canonical(expected), "NEGATIVE_FIXTURE_MISMATCH")
    return expected


def public_summary(plan, fixture):
    fixture = validate_fixture(plan, fixture)
    return {"case": fixture["case"], "purpose": "conformance_only", "execution_authorized": False,
            "release_eligible": False, "measurement_status": "not_measured", "fixture_sha256": h.digest(h.canonical(fixture)),
            "compiler_sha256": fixture["compiler_sha256"], "plan_sha256": fixture["plan_sha256"],
            "possible_keys": len(fixture["key_inventory"]), "fixture_owned_keys": len(fixture["initial_state"])}


def source_wire(operation, keys, arguments):
    h.require(operation in {"CJ2_APPROVE_BOOT", cr.CLAIM, cr.RELEASE, ns.INSTALL, ns.RETIRE, ns.PROMOTE}, "NEGATIVE_SOURCE")
    source = (h.ROOT / "services/spider/internal/database/crawljobsv2/lua" / (operation.lower() + ".lua")).read_bytes()
    wire = ("EVALSHA", hashlib.sha1(source).hexdigest(), str(len(keys)), *keys, *arguments)
    resp.encode(wire)
    return wire


def ledger_wires(plan, fixture, epoch):
    fixture = validate_fixture(plan, fixture)
    h.require(fixture["case"] in (*ns.STORED, ns.WIRE), "NEGATIVE_LEDGER_CASE")
    wires = cr.wire_requests(plan, fixture["worker"], epoch)
    if fixture["case"] in ns.STORED:
        assertion, code = ns.STORED[fixture["case"]]
        return [(assertion, "CRAWL_V2_" + code, wires[0])]
    base = list(wires[0])
    start = 3 + int(base[2])
    frozen = bytes.fromhex(freeze(plan, fixture["inputs"]["fixture_id"], fixture["inputs"]["redis_time_ms"],
                                 h.digest(b"negative-wire-freeze-record"))["record_hex"])
    epoch2 = h.digest((epoch + ":other").encode())[:32]
    h.require(epoch2 != epoch, "NEGATIVE_COLLISION")
    replacements = {0: "candidate", 1: epoch2, 2: "0" * 64, 3: b"", 4: b"", 5: b"", 6: frozen}
    result = []
    for index, code in enumerate(ns.WIRE_CODES):
        wire = list(base)
        if index < 7:
            wire[start + index] = replacements[index]
        elif index == 7:
            wire[start + 3] = wire[start + 3][:-1]
        elif index == 8:
            other = h.digest(b"bootstrap-acl-negatives-v1:other-contract")
            h.require(other != base[start + 2], "NEGATIVE_COLLISION")
            wire[start + 2] = other
        elif index == 9:
            wire[46], wire[47] = wire[47], wire[46]  # one-based KEYS 44/45
        elif index == 10:
            wire[47] = h.P + "reservation:" + fixture["worker"]["identities"]["b"]["reservation_id"]
        else:
            wire.pop()
        resp.encode(wire)
        result.append((f"W{index + 1:02}", "CRAWL_V2_" + code, tuple(wire)))
    return result


def boot_wire(run_id, epoch, evidence_sha256, at_ms):
    h.require(type(run_id) is str and re.fullmatch(r"[0-9a-f]{40}", run_id) and cr._hex(epoch, 32) and
              h.nonzero(evidence_sha256) and type(at_ms) is int and 0 < at_ms <= h.MAX_EXACT, "BOOT_INPUT")
    return source_wire("CJ2_APPROVE_BOOT", (h.AUTH[0],), (run_id, epoch, evidence_sha256, str(at_ms), "0", "", "", "initial"))


def boot_wires(wire, old_run_id, captured_ms):
    h.require(type(old_run_id) is str and re.fullmatch(r"[0-9a-f]{40}", old_run_id) and old_run_id != wire[4] and
              type(captured_ms) is int and 2592000001 < captured_ms <= h.MAX_EXACT - 600000, "BOOT_NEGATIVE_INPUT")
    changes = [(None, None), (6, ""), (6, "0" * 64), (7, str(captured_ms - 2592000001)),
               (7, str(captured_ms + 600000)), (4, old_run_id), (5, "G" * 32), (8, "1"), (11, "unknown-mode")]
    result = []
    for index, ((position, value), code) in enumerate(zip(changes, ns.BOOT_CODES), 1):
        changed = list(wire)
        if position is None:
            changed.pop()
        else:
            changed[position] = value
        resp.encode(changed)
        result.append((f"B{index:02}", "CRAWL_V2_" + code, tuple(changed)))
    return result


def boot_record(wire, now):
    h.require(type(now) is int and 0 < now <= h.MAX_EXACT, "BOOT_TIME")
    return dict(zip(ns.BOOT_FIELDS, ("1", "approved", wire[4], wire[5], str(now), "", "", "initial", "", wire[6], wire[7], "0")))


def validate_probe_evidence(plan, fixture_id, previous, evidence, expected_sha):
    """Reconcile exact observed history; a self-consistent foreign digest is insufficient."""
    h.exact(previous, {"old_run_id", "acknowledged", "at_ms", "key", "type", "value", "expiry", "value_sha256"})
    value = "m4-probe:" + fixture_id + ":" + h.digest(h.canonical(plan))
    h.require(previous["acknowledged"] is True and previous["key"] == h.AUTH[2] and previous["type"] == "string" and
              previous["expiry"] == "persistent" and previous["value"] == value and
              previous["value_sha256"] == h.digest(value.encode()), "BOOT_PROVENANCE")
    h.exact(evidence, {"case", "fixture_id", "plan_sha256", "old_run_id", "new_run_id", "acknowledged_probe_sha256",
                       "verified_at_ms", "acknowledged_loss_bound"})
    h.require(evidence["case"] == selected(plan) and evidence["fixture_id"] == fixture_id and
              evidence["plan_sha256"] == h.digest(h.canonical(plan)) and evidence["old_run_id"] == previous["old_run_id"] and
              all(type(evidence[k]) is str and re.fullmatch(r"[0-9a-f]{40}", evidence[k]) for k in ("old_run_id", "new_run_id")) and
              evidence["new_run_id"] != evidence["old_run_id"] and type(previous["at_ms"]) is int and
              type(evidence["verified_at_ms"]) is int and 0 < previous["at_ms"] <= evidence["verified_at_ms"] <= h.MAX_EXACT and
              evidence["acknowledged_probe_sha256"] == previous["value_sha256"] and
              type(evidence["acknowledged_loss_bound"]) is int and evidence["acknowledged_loss_bound"] == 0 and
              h.nonzero(expected_sha) and expected_sha == h.digest(h.canonical(evidence)), "BOOT_PROVENANCE")


def admin_observations(plan, fixture, boot, process_sha, at_ms):
    h.require(selected(plan) in ns.ADMIN and type(at_ms) is int and
              fixture["inputs"]["redis_time_ms"] <= at_ms <= h.MAX_EXACT and h.nonzero(process_sha), "ADMIN_OBSERVATION")
    h.exact(boot, set(ns.BOOT_FIELDS))
    empty = {**fixture["initial_state"], h.AUTH[0]: cr._hash([(k, boot[k]) for k in ns.BOOT_FIELDS])}
    h.require(all(value is None for value in fixture["initial_state"].values()), "ADMIN_DIRECT_SETUP")
    return {"case": selected(plan), "fixture_id": fixture["inputs"]["fixture_id"], "plan_sha256": fixture["plan_sha256"],
            "captured_at_ms": at_ms, "empty_inventory_sha256": h.digest(h.canonical(empty)),
            "process_inventory_sha256": process_sha, "boot_record_sha256": h.digest(h.canonical(boot))}


def validate_admin_observations(plan, fixture, observed):
    h.exact(observed, {"case", "fixture_id", "plan_sha256", "captured_at_ms", "empty_inventory_sha256",
                       "process_inventory_sha256", "boot_record_sha256"})
    h.require(observed["case"] == selected(plan) and selected(plan) in ns.ADMIN and
              observed["fixture_id"] == fixture["inputs"]["fixture_id"] and observed["plan_sha256"] == fixture["plan_sha256"] and
              type(observed["captured_at_ms"]) is int and fixture["inputs"]["redis_time_ms"] <= observed["captured_at_ms"] <= h.MAX_EXACT and
              all(h.nonzero(observed[key]) for key in ("empty_inventory_sha256", "process_inventory_sha256", "boot_record_sha256")),
              "ADMIN_PROVENANCE")


def admin_records(plan, fixture, observed, install_ms, retire_ms):
    validate_admin_observations(plan, fixture, observed)
    h.require(all(type(t) is int and observed["captured_at_ms"] <= t <= h.MAX_EXACT for t in (install_ms, retire_ms)) and
              install_ms <= retire_ms, "ADMIN_TIMES")
    # These are projections of independently observed empty-case artifacts, never
    # fabricated backup/stop measurements or direct authority setup rows.
    artifacts = {kind: {"purpose": "conformance_only", "case": selected(plan), "fixture_id": observed["fixture_id"],
                        "kind": kind, "observations": observed, "entries": []}
                 for kind in ("backup", "source", "queue", "urls", "depths", "spider", "signal", "process_stop")}
    hashes = {kind: h.digest(h.canonical(value)) for kind, value in artifacts.items()}
    frozen = freeze(plan, observed["fixture_id"], install_ms, hashes["process_stop"])
    retirement = h.packed([("protocol_version", "2"), ("freeze_nonce", fixture["admin_nonce"]),
        ("backup_sha256", hashes["backup"]), ("v1_count", "0"), ("v1_url_field_count", "0"), ("v1_depth_field_count", "0"),
        ("v1_source_sha256", hashes["source"]), ("v1_queue_evidence_sha256", hashes["queue"]),
        ("v1_urls_evidence_sha256", hashes["urls"]), ("v1_depths_evidence_sha256", hashes["depths"]),
        ("spider_queue_type", "none"), ("spider_queue_count", "0"), ("spider_queue_evidence_sha256", hashes["spider"]),
        ("signal_queue_type", "none"), ("signal_queue_count", "0"), ("signal_queue_evidence_sha256", hashes["signal"]),
        ("deleted_bitmap", "00000"), ("retired_at_ms", str(retire_ms))])
    return {"freeze": frozen, "retirement": retirement, "artifact_hashes": hashes}


def admin_projection(plan, fixture, observed, epoch, times):
    """All three fresh transitions; each prefix depends only on observed prior times."""
    fixture = validate_fixture(plan, fixture)
    h.require(selected(plan) in ns.ADMIN and cr._hex(epoch, 32) and type(times) is list and len(times) == 3 and
              all(type(t) is int for t in times) and times == sorted(times) and
              observed["captured_at_ms"] <= times[0] and times[-1] <= observed["captured_at_ms"] + 300000, "ADMIN_PROJECTION")
    records = admin_records(plan, fixture, observed, times[0], times[1])
    frozen, retirement = records["freeze"], records["retirement"]
    marker, contract = plan["compatibility_marker"], plan["identities"]["contract_sha256"]
    gate = ("boot_only", epoch, "", b"", b"", b"", b"")
    install = source_wire(ns.INSTALL, h.AUTH, (*gate, fixture["admin_nonce"], records["artifact_hashes"]["process_stop"],
                                              contract, *[value for _, value in marker["fields"]]))
    state = copy.deepcopy(fixture["initial_state"])
    state[h.AUTH[3]], state[h.AUTH[4]], state[h.AUTH[7]] = cr._hash(marker["fields"]), string(contract), cr._hash(frozen["fields"])
    result = [{"operation": ns.INSTALL, "wire": install, "reply": ["CANDIDATE_INSTALLED", str(times[0]), marker["fields"][1][1], contract],
               "state": copy.deepcopy(state)}]
    gate = ("candidate", epoch, contract, bytes.fromhex(marker["record_hex"]), b"", b"", bytes.fromhex(frozen["record_hex"]))
    values = dict(retirement["fields"])
    fields = [key for key, _ in retirement["fields"] if key not in ("protocol_version", "deleted_bitmap", "retired_at_ms")]
    confirmation = ":".join(values[key] for key in ("freeze_nonce", "backup_sha256", "v1_count", "v1_source_sha256",
        "v1_queue_evidence_sha256", "v1_urls_evidence_sha256", "v1_depths_evidence_sha256", "spider_queue_evidence_sha256", "signal_queue_evidence_sha256"))
    retire = source_wire(ns.RETIRE, (*h.AUTH, *cr.LIVE[:3], *h.LEGACY), (*gate, *[values[key] for key in fields], confirmation))
    state[h.AUTH[6]] = cr._hash(retirement["fields"])
    result.append({"operation": ns.RETIRE, "wire": retire, "reply": ["LEGACY_RETIRED", str(times[1]), "00000", values["v1_source_sha256"]],
                   "state": copy.deepcopy(state)})
    gate = (*gate[:5], bytes.fromhex(retirement["record_hex"]), gate[6])
    core = plan["guard_core"]
    promote = source_wire(ns.PROMOTE, (*h.AUTH, *cr.LIVE, *h.LEGACY, *ns.DOWNSTREAM),
                          (*gate, fixture["admin_nonce"], core["sha256"], *[value for _, value in core["fields"]]))
    guard = [*core["fields"][:2], ["compatibility_manifest_sha256", plan["compatibility"]["sha256"]],
             *core["fields"][2:10], ["approved_at_ms", str(times[2])], core["fields"][10]]
    state[h.AUTH[1]], state[h.AUTH[2]], state[h.AUTH[5]] = cr._hash(marker["fields"]), string(contract), cr._hash(guard)
    for key in h.ABSENCE_ONLY:
        state[key] = None
    result.append({"operation": ns.PROMOTE, "wire": promote,
                   "reply": ["CONTRACTS_PROMOTED", str(times[2]), plan["compatibility"]["sha256"], contract, core["sha256"]],
                   "state": state})
    return result
