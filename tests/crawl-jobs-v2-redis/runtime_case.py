"""Closed ledger/bootstrap/administrative recipes and ACLs; starts no fixture."""
from __future__ import annotations

import hashlib
import re

import harness as h
import claim_release as claim
import negative_cases as negative
import negative_specs as ns

CASE = "ledger-smoke-v1"
CLAIM_CASE = claim.CASE
CASES = {CASE: "ledger-smoke", CLAIM_CASE: claim.SCENARIO}
CASES.update(ns.CASES)
RATE = h.P + "rate_scopes"
PROBE = h.AUTH[2]  # Temporary pre-BOOT probe; removed before authority setup.
WIRE_KEYS = (*h.AUTH, RATE)
STORED_KEYS = tuple(h.AUTH[i] for i in (0, 1, 2, 5, 6))
ROLES = ("setup", "loader", "boot", "ledger", "observer", "revoker")
EARLY_ROLES = ("setup", "loader", "boot")
BOOT_FIELDS = ("schema_version", "boot_state", "approved_redis_run_id", "boot_epoch", "approved_at_ms",
               "planned_shutdown_nonce", "planned_shutdown_evidence_sha256", "last_approval_mode",
               "consumed_planned_shutdown_nonce", "rehearsal_evidence_sha256", "rehearsal_at_ms", "acknowledged_loss_bound")
SOURCES = ("CJ2_APPROVE_BOOT", "CJ2_MAINTAIN_RATE_SCOPES")
FILES = ("harness.py", "resp.py", "runtime_case.py", "executor.py", "controller.py",
          "claim_release.py", "claim_executor.py", "negative_specs.py", "negative_cases.py", "negative_executor.py",
          "bounded_state.py", "admission.py", "redis.conf", "Dockerfile.execution", "Dockerfile.execution.dockerignore")
CONFIG = {"port": "0", "unixsocket": "/run/cj2/redis.sock", "unixsocketperm": "600",
          "aclfile": "/run/cj2/fixture.acl", "dir": "/data", "appendonly": "yes",
          "appendfsync": "always", "aof-use-rdb-preamble": "yes", "aof-load-truncated": "no",
          "no-appendfsync-on-rewrite": "no", "maxmemory-policy": "noeviction",
          "stop-writes-on-bgsave-error": "yes", "maxmemory": "419430400",
          "lua-time-limit": "5000", "cluster-enabled": "no"}


def selector(commands, keys, access="%R~"):
    return "(" + commands + (" " + " ".join(access + key for key in sorted(keys)) if keys else "") + ")"


def case_for_plan(plan):
    h.require(type(plan) is dict and type(plan.get("inputs")) is dict, "CASE_PLAN")
    scenario = plan["inputs"].get("scenario")
    h.require(type(scenario) is str and scenario in CASES.values(), "UNSUPPORTED_CASE")
    return next(name for name, value in CASES.items() if value == scenario)


def sources(case_id=CASE):
    h.require(type(case_id) is str and case_id in CASES, "UNSUPPORTED_CASE")
    if case_id in ns.CASES:
        return ns.source_operations(case_id)
    return SOURCES if case_id == CASE else ("CJ2_APPROVE_BOOT", claim.CLAIM, claim.RELEASE)


def roles(case_id=CASE):
    sources(case_id)
    # DELUSER terminates the revoker's own session: it must remain the final
    # identity even when a case introduces administrative credentials.
    return (*ROLES[:-1], *ns.extra_roles(case_id), ROLES[-1])


def uses_material(case_id):
    sources(case_id)
    return case_id not in (CASE, ns.BOOT)


def early_roles(case_id):
    sources(case_id)
    if case_id == ns.BOOT:
        return ("setup", "loader")
    if case_id in ns.ADMIN:
        target = ns.ADMIN[case_id][1]
        return (*EARLY_ROLES, *(("release_admin",) if target == ns.RETIRE else ("migration_admin",) if target == ns.PROMOTE else ()))
    return EARLY_ROLES


def measure_roles(case_id):
    sources(case_id)
    if case_id == ns.BOOT:
        return ("observer", "boot", "revoker")
    if case_id in ns.ADMIN:
        return ("ledger", "observer", "revoker", ns.ADMIN[case_id][3])
    return ("ledger", "observer")


def stage_roles(case_id, stage):
    sources(case_id)
    choices = {"init": roles(case_id), "ready": ("setup",), "probe": ("setup",),
               "resume": ("setup", "loader", "boot", "observer", "revoker", *ns.extra_roles(case_id)),
               "measure": measure_roles(case_id), "revoke": roles(case_id)}
    h.require(stage in choices, "STAGE")
    return choices[stage]


def validate_revocation(result, targets):
    h.require(type(result) is dict and set(result) == set(targets), "REVOCATION_INVENTORY")
    for row in result.values():
        h.exact(row, {"reconnect", "server_reachable", "held_session"})
        h.require(row["reconnect"] == "denied" and row["server_reachable"] is True and
                  row["held_session"] in ("terminated", "already_revoked"), "REVOCATION_EVIDENCE")


def fixture(plan, fixture_id, material, at_ms=1000):
    if case_for_plan(plan) in ns.CASES:
        values = {"fixture_id": fixture_id, "redis_time_ms": at_ms}
        if case_for_plan(plan) != ns.BOOT:
            h.exact(material, {"owner_a", "owner_b", "token_a", "token_b", "wrong_token"})
            values.update(material)
        else:
            h.exact(material, set())
        return negative.compile_fixture(plan, values)
    return claim_fixture(plan, fixture_id, material, at_ms)


def worker_fixture(binding):
    return binding.get("worker") if "worker" in binding else binding


def claim_fixture(plan, fixture_id, material, at_ms=1000):
    """T=1000 is a key-only ACL projection, never an observed setup timestamp."""
    h.exact(material, {"owner_a", "owner_b", "token_a", "token_b", "wrong_token"})
    return claim.compile_fixture(plan, dict(material, fixture_id=fixture_id, redis_time_ms=at_ms))


def claim_key_groups(fixture):
    base = fixture["base_key"]
    reservations = [h.P + "reservation:" + fixture["identities"][label]["reservation_id"] for label in ("a", "b")]
    rates = [h.P + "rate:" + scope for scope in fixture["scope_ids"]]
    sets = [h.P + "active_runs", h.P + "unarchived_runs", base + ":jobs"]
    hashes = [key for key, row in fixture["initial_state"].items() if row is not None and row["type"] == "hash" and key not in h.AUTH]
    hashes += [base + ":visited_depth", base + ":visited_urls", h.P + "stage_slots", h.P + "first_request_start", *rates, *reservations]
    zsets = sorted(set(fixture["key_inventory"]) - set(h.AUTH) - set(hashes) - set(sets))
    write_hashes = [base, fixture["job_key"], base + ":group_pending", *rates, *reservations]
    write_zsets = [base + ":" + name for name in ("ready", "ready_at", "leased", "leased_at")]
    write_zsets += [h.P + "active_leases", *[key + suffix for key in rates for suffix in (":active", ":pending")]]
    return {"hashes": sorted(hashes), "sets": sorted(sets), "zsets": zsets,
            "write_hashes": sorted(write_hashes), "write_zsets": sorted(write_zsets), "reservations": sorted(reservations)}


# Used directly by both the ACL builder and the static recipe. Commands are
# separated by key kind; mutation grants never include AUTH or the whole union.
CLAIM_COMMANDS = {
    "hash_read": "+hlen +hstrlen +hmget +hkeys",
    "set_read": "+scard +sismember +smembers",
    "zset_read": "+zcard +zscore +zrange",
    "hash_write": "+hset", "zset_write": "+zadd +zrem", "reservation_expiry": "+pexpireat",
}


def acl_rules(case_id=CASE, fixture=None, plan=None):
    sources(case_id)
    if case_id in ns.CASES:
        return negative_acl_rules(case_id, fixture, plan)
    if case_id == CLAIM_CASE:
        fixture = claim.validate_fixture(plan, fixture)
        groups = claim_key_groups(fixture)
        readable = tuple(key for key in fixture["key_inventory"] if key not in h.ABSENCE_ONLY)
        fixed_hashes = tuple(h.AUTH[i] for i in (0, 1, 5, 6))
        data_reads = (selector("+type", fixture["key_inventory"]), selector("+pttl", readable),
                      selector("+hlen +hstrlen +hmget", fixed_hashes), selector("+strlen +get", (h.AUTH[2],)),
                      selector(CLAIM_COMMANDS["hash_read"], groups["hashes"]),
                      selector(CLAIM_COMMANDS["set_read"], groups["sets"]),
                      selector(CLAIM_COMMANDS["zset_read"], groups["zsets"]))
        observe = ("+ping +time +dbsize +scan +info|server", *data_reads,
                   selector("+pexpiretime", readable), selector("+type", h.LEGACY))
        setup_hashes = [key for key, row in fixture["initial_state"].items() if row is not None and row["type"] == "hash"]
        setup_sets = [key for key, row in fixture["initial_state"].items() if row is not None and row["type"] == "set"]
        setup_zsets = [key for key, row in fixture["initial_state"].items() if row is not None and row["type"] == "zset"]
        return {
            "setup": (*observe, "+info|persistence +info|memory +info|cluster +info|replication +config|get",
                      selector("+set +del", (PROBE,), "~"), selector("+hset", setup_hashes, "~"),
                      selector("+sadd", setup_sets, "~"), selector("+zadd", setup_zsets, "~")),
            "loader": ("+ping +script|load",), "boot": acl_rules()["boot"],
            "ledger": ("+ping +time +info|server +info|memory", selector("+evalsha", fixture["key_inventory"], "~"),
                       *data_reads, selector(CLAIM_COMMANDS["hash_write"], groups["write_hashes"], "~"),
                       selector(CLAIM_COMMANDS["zset_write"], groups["write_zsets"], "~"),
                       selector("+zadd", (RATE,), "~"), selector(CLAIM_COMMANDS["reservation_expiry"], groups["reservations"], "~")),
            "observer": observe, "revoker": ("+ping +acl|deluser",),
        }
    hashes = tuple(h.AUTH[i] for i in (0, 1, 5, 6))
    authority_reads = selector("+type +hlen +hstrlen +hmget", hashes)
    observe = ("+ping +time +dbsize +scan +info|server +info|persistence +info|memory +info|cluster +info|replication +config|get",
               selector("+type +pttl +dump +strlen +get +hlen +hstrlen +hmget", STORED_KEYS),
               selector("+type", (*h.ABSENCE_ONLY, *h.LEGACY, RATE)))
    return {
        "setup": (*observe, selector("+set +del", (PROBE,), "~"),
                  selector("+hset", tuple(h.AUTH[i] for i in (1, 5, 6)), "~")),
        "loader": ("+ping +script|load",),
        "boot": ("+ping +time +info|server", selector("+evalsha", (h.AUTH[0],), "~"),
                 selector("+type +hlen +hstrlen +hmget", (h.AUTH[0],)),
                 selector("+hset", (h.AUTH[0],), "~")),
        # Empty inventory is intentionally read-only. No needless mutation grants.
        "ledger": ("+ping +time +info|server", selector("+evalsha", WIRE_KEYS, "~"),
                   authority_reads, selector("+type +strlen +get", (h.AUTH[2],)),
                   selector("+type", h.ABSENCE_ONLY), selector("+type +zcard", (RATE,))),
        "observer": observe,
        "revoker": ("+ping +acl|deluser",),
    }


def negative_acl_rules(case_id, fixture, plan):
    fixture = negative.validate_fixture(plan, fixture)
    h.require(fixture["case"] == case_id, "NEGATIVE_ACL_CASE")
    metadata = "+info|persistence +info|memory +info|cluster +info|replication +config|get"
    if case_id == ns.BOOT:
        observe = ("+ping +time +dbsize +scan +info|server", selector("+type +pttl +pexpiretime", fixture["key_inventory"]),
                   selector("+hlen +hstrlen +hmget", (h.AUTH[0],)), selector("+strlen +get", (PROBE,)))
        return {"setup": (*observe, metadata, selector("+set +del", (PROBE,), "~")),
                "loader": ("+ping +script|load",), "boot": acl_rules()["boot"], "ledger": ("+ping",),
                "observer": observe, "revoker": ("+ping +acl|deluser",)}
    basis = fixture["worker"]
    result = acl_rules(CLAIM_CASE, basis, negative.worker_plan(plan))
    # Observer/setup must see the exact negative record bytes, but these grants
    # are NEVER appended to the ledger identity's selectors.
    observe = (*result["observer"], selector("+type +pttl +pexpiretime", fixture["key_inventory"]),
               selector("+hlen +hstrlen +hmget", (h.AUTH[1], h.AUTH[3], h.AUTH[7], h.AUTH[2])),
               selector("+strlen +get", (h.AUTH[4],)))
    result["observer"] = observe
    hashes = [key for key, row in fixture["initial_state"].items() if row is not None and row["type"] == "hash"]
    strings = [key for key, row in fixture["initial_state"].items() if row is not None and row["type"] == "string"]
    sets = [key for key, row in fixture["initial_state"].items() if row is not None and row["type"] == "set"]
    zsets = [key for key, row in fixture["initial_state"].items() if row is not None and row["type"] == "zset"]
    result["setup"] = (*observe, metadata, selector("+set +del", (PROBE,), "~"),
                       selector("+hset", hashes, "~"), selector("+set", strings, "~"),
                       selector("+sadd", sets, "~"), selector("+zadd", zsets, "~"))
    if case_id not in ns.ADMIN:
        return result
    # No direct setup row is allowed in administrative cases (only the probe).
    h.require(not hashes and not strings and not sets and not zsets, "ADMIN_DIRECT_SETUP")
    result["setup"] = (*observe, metadata, selector("+set +del", (PROBE,), "~"))
    proto = (*h.AUTH, *claim.LIVE, *h.LEGACY, *ns.DOWNSTREAM)
    hash_authority = tuple(h.AUTH[i] for i in (0, 1, 3, 5, 6, 7))
    reads = ("+ping +time +info|server +info|memory", selector("+type", proto),
             selector("+hlen +hstrlen +hmget", hash_authority), selector("+strlen +get", (h.AUTH[2], h.AUTH[4])),
             selector("+hlen +hkeys +hstrlen +hmget", (h.P + "stage_slots",)),
             selector("+scard +smembers", (h.P + "active_runs", h.P + "unarchived_runs")),
             selector("+zcard +zrange", (h.P + "runs",)), selector("+hlen", h.LEGACY[1:3]),
             selector("+zcard", (h.LEGACY[0],)), selector("+llen", (*h.LEGACY[3:], *ns.DOWNSTREAM[:6])))
    promotion = ns.ADMIN[case_id][1] == ns.PROMOTE
    result["release_admin"] = (*reads, selector("+evalsha", proto if promotion else h.AUTH, "~"),
        selector("+hset", (h.AUTH[3], h.AUTH[7], *((h.AUTH[5],) if promotion else ())), "~"),
        selector("+set", (h.AUTH[4],), "~"),
        *((selector("+rename", (h.AUTH[1], h.AUTH[2], h.AUTH[3], h.AUTH[4]), "~"),
           selector("+unlink", (h.AUTH[7],), "~")) if promotion else ()))
    if "migration_admin" in roles(case_id):
        result["migration_admin"] = (*reads, selector("+evalsha", (*h.AUTH, *claim.LIVE[:3], *h.LEGACY), "~"),
                                     selector("+hset", (h.AUTH[6],), "~"))
    h.require(set(result) == set(roles(case_id)), "NEGATIVE_ROLE_INVENTORY")
    return result


def credentials_valid(credentials, case_id=CASE):
    h.exact(credentials, set(roles(case_id)))
    h.require(all(type(value) is str and re.fullmatch(r"[0-9a-f]{64}", value) for value in credentials.values()), "CREDENTIAL_FORMAT")
    h.require(len(set(credentials.values())) == len(roles(case_id)), "CREDENTIAL_REUSE")


def acl_file(credentials, case_id=CASE, fixture=None, plan=None):
    credentials_valid(credentials, case_id)
    lines = ["user default off resetpass resetkeys resetchannels -@all"]
    for role, rules in acl_rules(case_id, fixture, plan).items():
        hashed = hashlib.sha256(credentials[role].encode()).hexdigest()
        lines.append("user cj2_" + role + " reset on #" + hashed + " " + " ".join(rules))
    return ("\n".join(lines) + "\n").encode()


def recipe(case_id=CASE):
    selected_sources = sources(case_id)
    result = {"case": case_id, "profile": "ledger", "source_operations": list(selected_sources),
            "wire_keys": list(WIRE_KEYS), "stored_keys": list(STORED_KEYS),
            "probe_key": PROBE, "probe_limit_bytes": 128, "direct_setup_count": 4,
             "derived_output_keys": [], "acl_rules": acl_rules(), "roles": list(roles(case_id)),
            "network_mode": "none", "user": "65534:65534",
            "redis_memory_bytes": 553648128, "executor_memory_bytes": 268435456,
            "max_seconds": 300, "command_timeout_seconds": 30, "cleanup_timeout_seconds": 60,
             "stage_timeout_milliseconds": 30000,
             "lifecycle_receipts": "bounded_controller_actions_jsonl",
            "teardown_executor": "fresh_revocation_container_after_worker_stop_wait_remove",
             "files": {name: h.digest((h.HERE / name).read_bytes()) for name in FILES}}
    if case_id == CLAIM_CASE:
        result.update(wire_keys="closed WORK/REQUEST templates from claim_release.py", stored_keys="57 fixture-owned plus bootstrap-owned durability",
                      direct_setup_count=26, derived_output_keys="two reservations and three four-key rate blocks",
                      acl_rules={"policy": "claim-key-kinds-v1", "commands": CLAIM_COMMANDS,
                                 "keys": "exact validated fixture identities; no wildcard grants"},
                      possible_keys=58, assertions=[f"CR{i:02}" for i in range(1, 13)], acl_denials=46,
                       measurement_steps=9, request_starts=0, state_expiry="absolute PEXPIRETIME", report_values="redacted")
    elif case_id in ns.CASES:
        result.update(profile="administrative" if case_id in ns.ADMIN else "ledger",
                      wire_keys="closed negative_specs/negative_cases wire inventory", stored_keys="case-specific typed manifest; durability BOOT-owned",
                      direct_setup_count=(0 if case_id in ns.ADMIN or case_id == ns.BOOT else
                                          27 if ns.STORED.get(case_id, ("",))[0].startswith("P") else
                                          25 if ns.STORED.get(case_id, ("",))[0] == "S01" else 26),
                      derived_output_keys="fixed per-case projection",
                      possible_keys=2 if case_id == ns.BOOT else 71 if case_id in ns.ADMIN else 58,
                      acl_rules={"policy": "negative-case-literal-v1", "ledger": "unused ping-only identity" if case_id == ns.BOOT else "unchanged claim-key-kinds-v1",
                                 "admin": "distinct per-operation roles" if case_id in ns.ADMIN else "none"},
                      assertions=[list(row) for row in ns.measurement_sequence(case_id)],
                      measurement_steps=len(ns.measurement_sequence(case_id)),
                      acl_denials=46 if case_id in (*ns.STORED, ns.WIRE) else 0,
                      request_starts=0, state_expiry="absolute PEXPIRETIME", report_values="redacted")
    return result


def recipe_sha256(case_id=CASE):
    return h.digest(h.canonical(recipe(case_id)))


def validate_approval(plan, approval, now_ms):
    h.validate_plan(plan)
    selected = case_for_plan(plan)
    h.require(plan["inputs"]["standin_image"] == plan["inputs"]["harness_image"], "STANDIN_IMAGE")
    h.exact(approval, {"version", "case", "approved", "operator", "commit", "plan_sha256",
                       "recipe_sha256", "expires_at_ms", "max_seconds", "architecture"})
    h.require(type(approval["version"]) is int and approval["version"] == 1 and
              approval["case"] == selected and approval["approved"] is True, "EXECUTION_NOT_APPROVED")
    h.require(type(approval["operator"]) is str and re.fullmatch(r"[A-Za-z0-9_.-]{1,64}", approval["operator"]), "OPERATOR")
    h.require(type(approval["commit"]) is str and re.fullmatch(r"[0-9a-f]{40}", approval["commit"]), "COMMIT")
    h.require(approval["plan_sha256"] == h.digest(h.canonical(plan)) and
              approval["recipe_sha256"] == recipe_sha256(selected), "APPROVAL_ARTIFACT_MISMATCH")
    h.require(type(approval["max_seconds"]) is int and 1 <= approval["max_seconds"] <= 300, "TIME_LIMIT")
    h.require(type(approval["expires_at_ms"]) is int and now_ms + approval["max_seconds"] * 1000 <
              approval["expires_at_ms"] <= min(h.MAX_EXACT, now_ms + 86400000), "APPROVAL_EXPIRY")
    h.require(approval["architecture"] in ("amd64", "arm64"), "PLATFORM")


def script_bytes(operation, case_id=CASE):
    h.require(operation in sources(case_id), "SOURCE_NOT_IN_CASE")
    h.source_identity()
    name = operation.lower() + ".lua"
    source = (h.ROOT / "services/spider/internal/database/crawljobsv2/lua" / name).read_bytes()
    h.require(0 < len(source) <= 1024 * 1024, "SOURCE_SIZE")
    return source, hashlib.sha1(source).hexdigest()


def active_request(plan, setup, boot_epoch):
    h.validate_setup(plan, setup)
    h.require(type(boot_epoch) is str and re.fullmatch(r"[0-9a-f]{32}", boot_epoch), "BOOT_EPOCH")
    _, sha = script_bytes("CJ2_MAINTAIN_RATE_SCOPES")
    prefix = ("active", boot_epoch, plan["identities"]["contract_sha256"],
              bytes.fromhex(plan["compatibility_marker"]["record_hex"]),
              bytes.fromhex(setup["stored_guard"]["record_hex"]),
              bytes.fromhex(setup["legacy_retirement"]["record_hex"]), b"")
    return ("EVALSHA", sha, "9", *WIRE_KEYS, *prefix, "0")


def boot_request(redis_run_id, boot_epoch, evidence_sha256, at_ms):
    h.require(type(redis_run_id) is str and re.fullmatch(r"[0-9a-f]{40}", redis_run_id) and
              type(boot_epoch) is str and re.fullmatch(r"[0-9a-f]{32}", boot_epoch) and
              h.nonzero(evidence_sha256) and type(at_ms) is int and 0 < at_ms <= h.MAX_EXACT, "BOOT_INPUT")
    _, sha = script_bytes("CJ2_APPROVE_BOOT")
    return ("EVALSHA", sha, "1", h.AUTH[0], redis_run_id, boot_epoch, evidence_sha256, str(at_ms), "0", "", "", "initial")
