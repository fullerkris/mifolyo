"""Fixed networkless fixture stages. Invoked only by the reviewed controller."""
from __future__ import annotations

import os
from contextlib import contextmanager
from pathlib import Path
import re
import signal
import sys
import time

import harness as h
import runtime_case as case
from resp import Client, RedisError, TransportError

CONTROL = Path("/run/cj2")
BOOT_FIELDS = ("schema_version", "boot_state", "approved_redis_run_id", "boot_epoch", "approved_at_ms",
               "planned_shutdown_nonce", "planned_shutdown_evidence_sha256", "last_approval_mode",
               "consumed_planned_shutdown_nonce", "rehearsal_evidence_sha256", "rehearsal_at_ms", "acknowledged_loss_bound")


def connect(role, credentials):
    h.require(role in case.ROLES and role in credentials, "ROLE")
    client = Client()
    try:
        h.require(client.call("AUTH", "cj2_" + role, credentials[role]) == b"OK", "AUTH")
        return client
    except Exception:
        client.close()
        raise


def clock(client):
    parts = client.call("TIME")
    h.require(type(parts) is list and len(parts) == 2 and
              all(type(v) is bytes and re.fullmatch(rb"[0-9]{1,16}", v) for v in parts), "REDIS_TIME")
    seconds, micros = map(int, parts)
    value = seconds * 1000 + micros // 1000
    h.require(0 <= micros < 1000000 and 0 < value <= h.MAX_EXACT, "REDIS_TIME")
    return value


def info(client, section):
    raw = client.call("INFO", section)
    h.require(type(raw) is bytes, "INFO")
    result = {}
    for line in raw.decode("ascii").splitlines():
        if not line or line.startswith("#"):
            continue
        key, sep, value = line.partition(":")
        h.require(sep and key not in result, "INFO")
        result[key] = value
    return result


def server(client):
    result = info(client, "SERVER")
    h.require(re.fullmatch(r"[0-9a-f]{40}", result.get("run_id", "")), "RUN_ID")
    return result


def validate_network(interfaces, ipv4_routes, ipv6_routes):
    """Accept only loopback plus known, DOWN kernel fallback tunnel devices.

    Some Linux kernels instantiate these devices even in Docker network=none.
    None may be administratively UP, and neither routing table may provide an
    external route. The caller separately verifies the absence of NET_ADMIN.
    """
    fallback = {"tunl0", "gre0", "gretap0", "erspan0", "ip_vti0", "ip6_vti0", "sit0", "ip6tnl0", "ip6gre0"}
    h.require("lo" in interfaces and set(interfaces) <= fallback | {"lo"}, "NETWORK_INTERFACE")
    for name, observed in interfaces.items():
        flags = int(observed["flags"], 16)
        if name == "lo":
            h.require(flags & 0x8 != 0, "LOOPBACK_FLAGS")
        else:
            h.require(flags & 0x1 == 0 and observed["operstate"] == "down", "ACTIVE_INTERFACE")
    v4 = ipv4_routes.splitlines()
    h.require(v4 and v4[0].split()[:3] == ["Iface", "Destination", "Gateway"] and
              not any(line.strip() for line in v4[1:]), "IPV4_ROUTE")
    for line in ipv6_routes.splitlines():
        fields = line.split()
        h.require(len(fields) == 10 and fields[-1] == "lo" and
                  all(re.fullmatch(r"[0-9a-f]+", field) for field in fields[:-1]), "IPV6_ROUTE")
        destination, prefix, source, source_prefix, gateway = fields[:5]
        flags = int(fields[8], 16)
        h.require(source == "0" * 32 and source_prefix == "00" and gateway == "0" * 32 and
                  ((destination == "0" * 31 + "1" and prefix == "80") or
                   (destination == "0" * 32 and prefix == "00" and flags & 0x200 != 0)), "IPV6_ROUTE")
    return {"interfaces": sorted(interfaces), "inactive_fallbacks": sorted(set(interfaces) - {"lo"}),
            "external_routes": 0}


def environment(init=False):
    h.require(os.geteuid() == (0 if init else 65534), "EXECUTOR_USER")
    h.require(not any(k.lower().endswith("proxy") and v for k, v in os.environ.items()), "PROXY")
    # bonding_masters is a sysfs control file, not an interface directory.
    interfaces = {p.name: {field: (p / field).read_text().strip() for field in ("flags", "operstate")}
                  for p in Path("/sys/class/net").iterdir() if p.is_dir()}
    network = validate_network(interfaces, Path("/proc/net/route").read_text(), Path("/proc/net/ipv6_route").read_text())
    status = Path("/proc/self/status").read_text()
    cap = next(line.split()[1] for line in status.splitlines() if line.startswith("CapEff:"))
    h.require(int(cap, 16) == (1 if init else 0), "CAPABILITIES")
    return {**network, "effective_capabilities": cap, "uid": os.geteuid()}


def validate_request(request):
    h.exact(request, {"plan", "recipe_sha256", "fixture_id", "credentials", "previous"})
    h.validate_plan(request["plan"])
    h.require(request["plan"]["inputs"]["scenario"] == "ledger-smoke", "SCENARIO")
    h.require(request["recipe_sha256"] == case.recipe_sha256(), "RECIPE")
    h.require(type(request["fixture_id"]) is str and re.fullmatch(r"[0-9a-f]{32}", request["fixture_id"]), "FIXTURE_ID")
    h.require(type(request["credentials"]) is dict and set(request["credentials"]) <= set(case.ROLES), "ROLE")
    h.require(all(type(value) is str and re.fullmatch(r"[0-9a-f]{64}", value)
                  for value in request["credentials"].values()), "CREDENTIAL_FORMAT")


def configuration(client, plan):
    raw = client.call("CONFIG", "GET", *case.CONFIG)
    h.require(type(raw) is list and len(raw) == len(case.CONFIG) * 2, "CONFIG")
    actual = dict(zip(raw[::2], raw[1::2]))
    h.require(actual == {k.encode(): v.encode() for k, v in case.CONFIG.items()}, "CONFIG")
    current = server(client)
    h.require(current["redis_version"] == plan["inputs"]["redis_version"], "REDIS_VERSION")
    persistence = info(client, "PERSISTENCE")
    h.require(persistence.get("aof_enabled") == "1" and persistence.get("aof_last_write_status") == "ok" and
              persistence.get("loading") == "0", "AOF")
    h.require(info(client, "CLUSTER").get("cluster_enabled") == "0", "CLUSTER")
    replication = info(client, "REPLICATION")
    h.require(replication.get("role") == "master" and replication.get("connected_slaves") == "0", "REPLICATION")
    memory = info(client, "MEMORY")
    h.require(0 < int(memory["used_memory"]) <= 128 * 1024 * 1024, "PRE_STAGE_MEMORY")
    return current


def probe_value(request):
    return "m4-probe:" + request["fixture_id"] + ":" + h.digest(h.canonical(request["plan"]))


def inventory(client):
    size = client.call("DBSIZE")
    h.require(type(size) is int and 0 <= size <= 8, "KEY_COUNT")
    cursor, keys = b"0", set()
    for _ in range(8):
        reply = client.call("SCAN", cursor, "COUNT", "16")
        h.require(type(reply) is list and len(reply) == 2 and type(reply[0]) is bytes and
                  re.fullmatch(rb"[0-9]{1,20}", reply[0]) and type(reply[1]) is list and
                  len(reply[1]) <= 16 and all(type(v) is bytes and len(v) <= 256 for v in reply[1]), "SCAN")
        cursor = reply[0]
        keys.update(reply[1])
        h.require(len(keys) <= 8, "KEY_COUNT")
        if cursor == b"0":
            h.require(len(keys) == size, "INVENTORY_CHANGED")
            return keys
    raise h.InvalidArtifact("SCAN_BUDGET")


def read_hash(client, key, fields):
    h.require(client.call("TYPE", key) == b"hash" and client.call("HLEN", key) == len(fields), "HASH_SHAPE")
    for field in fields:
        length = client.call("HSTRLEN", key, field)
        h.require(type(length) is int and 0 <= length <= 2048, "HASH_BOUND")
    values = client.call("HMGET", key, *fields)
    h.require(type(values) is list and len(values) == len(fields) and all(type(v) is bytes for v in values), "HASH_FIELDS")
    return {key: value.decode("utf-8") for key, value in zip(fields, values)}


def snapshot(client):
    h.require(inventory(client) == {k.encode() for k in case.STORED_KEYS}, "INVENTORY")
    result = {}
    for key in case.STORED_KEYS:
        h.require(client.call("PTTL", key) == -1, "UNEXPECTED_EXPIRY")
        raw = client.call("DUMP", key)
        h.require(type(raw) is bytes and len(raw) <= 16384, "DUMP_BOUND")
        result[key] = h.digest(raw)
    for key in (*h.ABSENCE_ONLY, *h.LEGACY, case.RATE):
        h.require(client.call("TYPE", key) == b"none", "REQUIRED_ABSENCE")
    return result


def loaded(loader, operation):
    source, sha = case.script_bytes(operation)
    h.require(loader.call("SCRIPT", "LOAD", source) == sha.encode(), "SCRIPT_SHA")
    return sha


def revoke(credentials, targets):
    """DELUSER terminates existing sessions. Test both held and fresh connections."""
    results = {}
    admin = connect("revoker", credentials)
    try:
        for role in targets:
            held = None
            try:
                held = connect(role, credentials)
            except RedisError as exc:
                h.require(exc.code == "WRONGPASS", "REVOCATION_AUTH")
            removed = admin.call("ACL", "DELUSER", "cj2_" + role)
            h.require(removed in (0, 1), "REVOCATION_REPLY")
            if held:
                try:
                    h.require(held.observe_peer_disconnect() in ("eof", "reset"), "DISCONNECT_NOT_OBSERVED")
                finally:
                    held.close()
            try:
                with connect(role, credentials):
                    raise h.InvalidArtifact("RECONNECT_SUCCEEDED")
            except RedisError as exc:
                h.require(exc.code == "WRONGPASS", "REVOCATION_NOT_PROVEN")
            results[role] = {"reconnect": "denied", "server_reachable": True,
                             "held_session": "terminated" if held else "already_revoked"}
    finally:
        admin.close()
    return results


def initialize(request):
    environment(init=True)
    case.credentials_valid(request["credentials"])
    for directory in (CONTROL, Path("/data")):
        h.require(directory.is_dir() and not directory.is_symlink() and not list(directory.iterdir()), "VOLUME_NOT_EMPTY")
        os.chmod(directory, 0o700)
    config = (h.HERE / "redis.conf").read_bytes()
    acl = case.acl_file(request["credentials"])
    for name, raw in (("redis.conf", config), ("fixture.acl", acl)):
        path = CONTROL / name
        with path.open("xb") as stream:
            stream.write(raw)
        os.chmod(path, 0o600)
        os.chown(path, 65534, 65534)
    for directory in (CONTROL, Path("/data")):
        os.chown(directory, 65534, 65534)
    return {"empty_volumes_verified": True, "config_sha256": h.digest(config), "acl_file_sha256": h.digest(acl)}


def probe(request):
    with connect("setup", request["credentials"]) as client:
        current = configuration(client, request["plan"])
        h.require(inventory(client) == set(), "NOT_EMPTY")
        value = probe_value(request)
        h.require(len(value.encode()) <= 128, "PROBE_BOUND")
        at = clock(client)
        h.require(client.call("SET", case.PROBE, value) == b"OK", "PROBE_ACK")
        h.require(client.call("GET", case.PROBE) == value.encode(), "PROBE_READ")
        return {"old_run_id": current["run_id"], "acknowledged": True, "at_ms": at,
                "key": case.PROBE, "type": "string", "value": value, "expiry": "persistent",
                "value_sha256": h.digest(value.encode())}


def resume(request):
    credentials, plan = request["credentials"], request["plan"]
    previous = request["previous"]
    h.require(previous.get("acknowledged") is True and previous.get("value") == probe_value(request) and
              previous.get("value_sha256") == h.digest(probe_value(request).encode()), "PROBE_HISTORY")
    with connect("setup", credentials) as setup_client:
        current = configuration(setup_client, plan)
        h.require(current["run_id"] != previous["old_run_id"] and
                  inventory(setup_client) == {case.PROBE.encode()} and
                  setup_client.call("GET", case.PROBE) == probe_value(request).encode() and
                  setup_client.call("PTTL", case.PROBE) == -1, "PERSISTENCE_LOSS")
        at = clock(setup_client)
        h.require(type(previous["at_ms"]) is int and 0 < previous["at_ms"] <= at, "PROBE_CLOCK")
        evidence = {"case": case.CASE, "fixture_id": request["fixture_id"], "plan_sha256": h.digest(h.canonical(plan)),
                    "old_run_id": previous["old_run_id"], "new_run_id": current["run_id"],
                    "acknowledged_probe_sha256": previous["value_sha256"], "verified_at_ms": at,
                    "acknowledged_loss_bound": 0}
        h.require(setup_client.call("DEL", case.PROBE) == 1 and inventory(setup_client) == set(), "PROBE_CLEANUP")
        with connect("loader", credentials) as loader:
            loaded(loader, "CJ2_APPROVE_BOOT")
            loaded(loader, "CJ2_MAINTAIN_RATE_SCOPES")
        epoch = h.digest((request["fixture_id"] + ":boot").encode())[:32]
        evidence_sha = h.digest(h.canonical(evidence))
        boot_request = case.boot_request(current["run_id"], epoch, evidence_sha, at)
        with connect("boot", credentials) as boot:
            first = boot.call(*boot_request)
            h.require(type(first) is list and len(first) == 3 and first[0] == b"OK" and first[2] == epoch.encode(), "BOOT_REPLY")
            replay = boot.call(*boot_request)
            h.require(type(replay) is list and len(replay) == 3 and replay[0] == b"EXISTS_IDENTICAL" and replay[2] == epoch.encode(), "BOOT_REPLAY")
        expected_boot = dict(zip(BOOT_FIELDS, ("1", "approved", current["run_id"], epoch, first[1].decode(),
                            "", "", "initial", "", evidence_sha, str(at), "0")))
        h.require(read_hash(setup_client, h.AUTH[0], BOOT_FIELDS) == expected_boot, "BOOT_STATE")
        setup = h.ledger_setup(plan, clock(setup_client))
        for row in setup["writes"]:
            h.require(setup_client.call("TYPE", row["key"]) == b"none", "SETUP_PREEXISTING")
            if row["type"] == "hash":
                flat = tuple(v for field in row["fields"] for v in field)
                h.require(setup_client.call("HSET", row["key"], *flat) == len(row["fields"]), "SETUP_WRITE")
                h.require(read_hash(setup_client, row["key"], tuple(k for k, _ in row["fields"])) == dict(row["fields"]), "SETUP_BYTES")
            else:
                h.require(setup_client.call("SET", row["key"], row["value"]) == b"OK" and
                          setup_client.call("GET", row["key"]) == row["value"].encode(), "SETUP_BYTES")
        before = snapshot(setup_client)
    revoked = revoke(credentials, case.EARLY_ROLES)
    return {"probe_evidence": evidence, "probe_evidence_sha256": evidence_sha, "boot_epoch": epoch,
            "boot_record": expected_boot, "setup_projection": setup, "setup_time_observed": True,
            "snapshot": before, "early_revocation": revoked}


def measure(request):
    previous, credentials = request["previous"], request["credentials"]
    wire = case.active_request(request["plan"], previous["setup_projection"], previous["boot_epoch"])
    with connect("observer", credentials) as observer, connect("ledger", credentials) as ledger:
        h.require(server(observer)["run_id"] == previous["boot_record"]["approved_redis_run_id"], "BOOT_CHANGED")
        h.require(snapshot(observer) == previous["snapshot"], "SETUP_CHANGED")
        replies = []
        for _ in range(2):
            reply = ledger.call(*wire)
            h.require(type(reply) is list and len(reply) == 4 and reply[0] == b"BATCH_DONE" and
                      reply[2:] == [b"0", b"0"] and type(reply[1]) is bytes and reply[1].isdigit(), "ACTIVE_REPLY")
            replies.append([v.decode() for v in reply])
            h.require(snapshot(observer) == previous["snapshot"], "IDLE_MUTATION")
        negatives = []
        for key in h.ABSENCE_ONLY:
            h.require(ledger.call("TYPE", key) == b"none", "ABSENCE_READ")
            for command in (("GET", key), ("HGET", key, "x"), ("SET", key, "x"), ("HSET", key, "x", "x"),
                            ("DEL", key), ("EXPIRE", key, "1"), ("RENAME", key, case.RATE)):
                try:
                    ledger.call(*command)
                except RedisError as exc:
                    h.require(exc.code == "NOPERM", "ACL_DENIAL_NOT_PROVEN")
                else:
                    raise h.InvalidArtifact("ACL_ESCALATION")
                negatives.append({"command": command[0], "key": key, "result": "NOPERM"})
        h.require(snapshot(observer) == previous["snapshot"], "NEGATIVE_MUTATION")
        return {"active_replies": replies, "acl_negatives": negatives,
                "before_after_snapshot_sha256": h.digest(h.canonical(previous["snapshot"])),
                "scope": "BOOT_and_empty_rate_maintenance_only"}


@contextmanager
def stage_deadline(milliseconds):
    """Hard process exit: even blocked stdin/Redis I/O cannot resume after expiry.

    The controller additionally stops/waits/removes the worker container before
    teardown; terminating a Docker CLI is not evidence of worker termination.
    """
    h.require(type(milliseconds) is int and 1 <= milliseconds <= 30000, "STAGE_TIMEOUT")
    def expired(*_):
        os._exit(124)
    previous = signal.signal(signal.SIGALRM, expired)
    signal.setitimer(signal.ITIMER_REAL, milliseconds / 1000)
    try:
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)


def ready(request):
    # One bounded stage does read-only startup polling. The controller never
    # retries an ambiguous docker-exec invocation that might still be alive.
    while True:
        try:
            with connect("setup", request["credentials"]) as client:
                current = configuration(client, request["plan"])
            return {"run_id": current["run_id"]}
        except TransportError:
            time.sleep(0.1)


def main():
    if sys.argv[1:] == ["hold"]:
        time.sleep(600)
        return 0
    stages = {"init": initialize, "ready": ready, "probe": probe, "resume": resume, "measure": measure,
              "revoke": lambda request: {"revocation": revoke(request["credentials"], case.ROLES)}}
    try:
        h.require(len(sys.argv) == 3 and sys.argv[1] in stages and
                  re.fullmatch(r"[1-9][0-9]{0,4}", sys.argv[2]), "STAGE")
        stage = sys.argv[1]
        with stage_deadline(int(sys.argv[2])):
            request = h.decode(sys.stdin.buffer.read(h.MAX_ARTIFACT_BYTES + 1))
            validate_request(request)
            isolation = environment(init=stage == "init")
            result = stages[stage](request)
            output = {"stage": stage, "status": "PASS", "recipe_sha256": case.recipe_sha256(),
                      "isolation": isolation, "result": result}
            raw = h.canonical(output)
            h.require(len(raw) <= h.MAX_ARTIFACT_BYTES, "REPORT_BOUND")
            sys.stdout.buffer.write(raw)
            sys.stdout.buffer.flush()
        return 0
    except Exception:
        print("M4 executor stage failed", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
