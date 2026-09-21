"""Closed first-case recipe and ACLs. Pure construction; never starts a fixture."""
from __future__ import annotations

import hashlib
import re

import harness as h

CASE = "ledger-smoke-v1"
RATE = h.P + "rate_scopes"
PROBE = h.AUTH[2]  # Temporary pre-BOOT probe; removed before authority setup.
WIRE_KEYS = (*h.AUTH, RATE)
STORED_KEYS = tuple(h.AUTH[i] for i in (0, 1, 2, 5, 6))
ROLES = ("setup", "loader", "boot", "ledger", "observer", "revoker")
EARLY_ROLES = ("setup", "loader", "boot")
SOURCES = ("CJ2_APPROVE_BOOT", "CJ2_MAINTAIN_RATE_SCOPES")
FILES = ("harness.py", "resp.py", "runtime_case.py", "executor.py", "controller.py",
         "redis.conf", "Dockerfile.execution", "Dockerfile.execution.dockerignore")
CONFIG = {"port": "0", "unixsocket": "/run/cj2/redis.sock", "unixsocketperm": "600",
          "aclfile": "/run/cj2/fixture.acl", "dir": "/data", "appendonly": "yes",
          "appendfsync": "always", "aof-use-rdb-preamble": "yes", "aof-load-truncated": "no",
          "no-appendfsync-on-rewrite": "no", "maxmemory-policy": "noeviction",
          "stop-writes-on-bgsave-error": "yes", "maxmemory": "419430400",
          "lua-time-limit": "5000", "cluster-enabled": "no"}


def selector(commands, keys, access="%R~"):
    return "(" + commands + (" " + " ".join(access + key for key in sorted(keys)) if keys else "") + ")"


def acl_rules():
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


def credentials_valid(credentials):
    h.exact(credentials, set(ROLES))
    h.require(all(type(value) is str and re.fullmatch(r"[0-9a-f]{64}", value) for value in credentials.values()), "CREDENTIAL_FORMAT")
    h.require(len(set(credentials.values())) == len(ROLES), "CREDENTIAL_REUSE")


def acl_file(credentials):
    credentials_valid(credentials)
    lines = ["user default off resetpass resetkeys resetchannels -@all"]
    for role, rules in acl_rules().items():
        hashed = hashlib.sha256(credentials[role].encode()).hexdigest()
        lines.append("user cj2_" + role + " reset on #" + hashed + " " + " ".join(rules))
    return ("\n".join(lines) + "\n").encode()


def recipe():
    return {"case": CASE, "profile": "ledger", "source_operations": list(SOURCES),
            "wire_keys": list(WIRE_KEYS), "stored_keys": list(STORED_KEYS),
            "probe_key": PROBE, "probe_limit_bytes": 128, "direct_setup_count": 4,
            "derived_output_keys": [], "acl_rules": acl_rules(),
            "network_mode": "none", "user": "65534:65534",
            "redis_memory_bytes": 553648128, "executor_memory_bytes": 268435456,
            "max_seconds": 300, "command_timeout_seconds": 30, "cleanup_timeout_seconds": 60,
            "stage_timeout_milliseconds": 30000,
            "teardown_executor": "fresh_revocation_container_after_worker_stop_wait_remove",
            "files": {name: h.digest((h.HERE / name).read_bytes()) for name in FILES}}


def recipe_sha256():
    return h.digest(h.canonical(recipe()))


def validate_approval(plan, approval, now_ms):
    h.validate_plan(plan)
    h.require(plan["inputs"]["scenario"] == "ledger-smoke" and
              plan["inputs"]["standin_image"] == plan["inputs"]["harness_image"], "FIRST_CASE_ONLY")
    h.exact(approval, {"version", "case", "approved", "operator", "commit", "plan_sha256",
                       "recipe_sha256", "expires_at_ms", "max_seconds", "architecture"})
    h.require(type(approval["version"]) is int and approval["version"] == 1 and
              approval["case"] == CASE and approval["approved"] is True, "EXECUTION_NOT_APPROVED")
    h.require(type(approval["operator"]) is str and re.fullmatch(r"[A-Za-z0-9_.-]{1,64}", approval["operator"]), "OPERATOR")
    h.require(type(approval["commit"]) is str and re.fullmatch(r"[0-9a-f]{40}", approval["commit"]), "COMMIT")
    h.require(approval["plan_sha256"] == h.digest(h.canonical(plan)) and
              approval["recipe_sha256"] == recipe_sha256(), "APPROVAL_ARTIFACT_MISMATCH")
    h.require(type(approval["max_seconds"]) is int and 1 <= approval["max_seconds"] <= 300, "TIME_LIMIT")
    h.require(type(approval["expires_at_ms"]) is int and now_ms + approval["max_seconds"] * 1000 <
              approval["expires_at_ms"] <= min(h.MAX_EXACT, now_ms + 86400000), "APPROVAL_EXPIRY")
    h.require(approval["architecture"] in ("amd64", "arm64"), "PLATFORM")


def script_bytes(operation):
    h.require(operation in SOURCES, "SOURCE_NOT_IN_CASE")
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
