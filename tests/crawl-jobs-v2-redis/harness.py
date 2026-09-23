#!/usr/bin/env python3
"""Offline M4 artifact compiler. No Redis, sockets, subprocesses or lifecycle API.

Plans are deliberately non-executable. Image digests are declarations pending
target-image verification, not proof that an image exists or has been reviewed.
Only fixed recipes generate marker bytes; no caller-supplied marker is accepted.
Uses Python 3.10+ and the standard library.
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import runpy
import stat
import struct
import sys

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
MAX_ARTIFACT_BYTES = 2 * 1024 * 1024
MAX_EXACT = 9007199254740991
P = "mifolyo:crawl:v2:"
AUTH = (P + "durability", "mifolyo:contracts:active", P + "contract",
        "mifolyo:contracts:candidate", P + "contract:candidate", P + "commit_guard",
        P + "legacy_retirement", P + "admin_freeze")
LEGACY = ("mifolyo:crawl:v1:queue", "mifolyo:crawl:v1:urls",
          "mifolyo:crawl:v1:depths", "spider_queue", "signal_queue")
ABSENCE_ONLY = (AUTH[3], AUTH[4], AUTH[7])
EVIDENCE_FIELDS = ("maximum_shape_sha256", "memory_fixture_sha256",
                   "lua_benchmark_sha256", "aof_crash_evidence_sha256")
IMAGE_FIELDS = ("spider_image", "seed_importer_image", "crawl_admin_image",
                "indexer_image", "image_indexer_image", "backlinks_processor_image",
                "monitoring_image")
SCENARIOS = {"ledger-smoke": ("ledger", "fresh"),
             "ledger-claim-release": ("ledger", "fresh"),
             "administrative-fresh": ("administrative", "fresh"),
             "administrative-migration": ("administrative", "v1_migration")}
CANDIDATE_RUN_OPS = frozenset(("CJ2_CREATE_RUN", "CJ2_ENQUEUE_BATCH",
    "CJ2_BEGIN_RUN_AUDIT", "CJ2_AUDIT_RUN_BATCH", "CJ2_SEAL_RUN",
    "CJ2_CANCEL_RUN", "CJ2_CANCEL_BATCH", "CJ2_PURGE_RUN_BATCH"))
INPUT_FIELDS = frozenset(("format_version", "scenario", "redis_version",
                         "redis_image", "harness_image", "standin_image"))


class InvalidArtifact(ValueError):
    """A value-redacted validation failure."""


def require(condition: bool, code: str) -> None:
    if not condition:
        raise InvalidArtifact(code)


def exact(value: object, fields: set | frozenset) -> None:
    require(type(value) is dict and set(value) == fields, "INVALID_FIELDS")


def digest(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def nonzero(value: object) -> bool:
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None and value != "0" * 64


def canonical(value: object) -> bytes:
    # Closed input validators prohibit floats. Output comes only from recipes.
    return (json.dumps(value, sort_keys=True, separators=(",", ":"),
                       ensure_ascii=False, allow_nan=False) + "\n").encode("utf-8")


def _pairs(pairs: list) -> dict:
    result = {}
    for key, value in pairs:
        require(key not in result, "DUPLICATE_FIELD")
        result[key] = value
    return result


def _no_number(_: str) -> None:
    raise InvalidArtifact("INVALID_NUMBER")


def decode(raw: bytes) -> dict:
    require(0 < len(raw) <= MAX_ARTIFACT_BYTES, "ARTIFACT_SIZE")
    try:
        value = json.loads(raw.decode("utf-8"), object_pairs_hook=_pairs,
                           parse_float=_no_number, parse_constant=_no_number)
        require(type(value) is dict, "INVALID_FIELDS")
        # Reject lone surrogates even in unknown fields, before canonicalization.
        canonical(value)
        return value
    except (UnicodeError, json.JSONDecodeError, RecursionError) as exc:
        raise InvalidArtifact("INVALID_JSON") from exc


def read_artifact(path: Path) -> bytes:
    # Do not accept devices, FIFOs or symlinks, and bound reads before parsing.
    # Check the opened descriptor, not a racy lstat/open pair. NONBLOCK also
    # prevents a substituted FIFO from hanging before fstat rejects it.
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode), "INVALID_FILE")
        require(info.st_size <= MAX_ARTIFACT_BYTES, "ARTIFACT_SIZE")
        raw = stream.read(MAX_ARTIFACT_BYTES + 1)
    require(0 < len(raw) <= MAX_ARTIFACT_BYTES, "ARTIFACT_SIZE")
    return raw


def record(fields: list[tuple[str, str]]) -> bytes:
    def frame(text: str) -> bytes:
        raw = text.encode("utf-8")
        return struct.pack(">Q", len(raw)) + raw
    require(len({key for key, _ in fields}) == len(fields), "DUPLICATE_FIELD")
    raw = struct.pack(">Q", len(fields)) + b"".join(frame(k) + frame(v) for k, v in fields)
    require(len(raw) <= 16384, "RECORD_SIZE")
    return raw


def packed(fields: list[tuple[str, str]]) -> dict:
    raw = record(fields)
    return {"fields": [[k, v] for k, v in fields], "record_hex": raw.hex(), "sha256": digest(raw)}


def source_identity() -> tuple[dict, tuple]:
    # Reuse the strict source-inventory checker, not file discovery or fixture
    # hashes supplied by a caller. Compare against both checked-in pin outputs.
    generator = runpy.run_path(str(ROOT / "scripts/generate-crawl-jobs-v2-bundle.py"))
    _, _, result = generator["bundle"](ROOT)
    require(generator["generated_go"](result) ==
            (ROOT / generator["GENERATED"]).read_bytes(), "STALE_BUNDLE_PINS")
    fixture = decode((ROOT / generator["FIXTURE"]).read_bytes())
    cases = [case for case in fixture["cases"] if case["name"] == "canonical-lua-bundle"]
    require(len(cases) == 1 and cases[0]["expected"] == result, "STALE_BUNDLE_FIXTURE")
    identities = {key: result[key] for key in
                  ("contract_sha256", "source_set_sha256", "bundle_seal_sha256")}
    return identities, generator["OPERATIONS"]


def operation_inventory(operations: tuple) -> list[dict]:
    result = []
    for operation in operations:
        modes = ["active"]
        if operation == "CJ2_APPROVE_BOOT":
            modes = ["ungated"]
        elif operation == "CJ2_INSTALL_CANDIDATE_MARKERS":
            modes = ["boot_only"]
        elif operation == "CJ2_RETIRE_LEGACY_KEYS":
            modes = ["candidate_before_retirement", "candidate_after_retirement"]
        elif operation == "CJ2_PROMOTE_CANDIDATE_CONTRACTS":
            modes = ["candidate_after_retirement"]
        elif operation in CANDIDATE_RUN_OPS:
            modes.append("candidate_before_retirement")
        for mode in modes:
            result.append({"case_id": operation.lower() + "/" + mode,
                           "operation": operation, "gate": mode,
                           "execution_status": "not_run"})
    require(len(operations) == 43 and len(result) == 52, "INVENTORY_MISMATCH")
    return result


def requirement_inventory() -> list[dict]:
    """Retain each section-17 requirement verbatim and hash it; never claim tests."""
    text = (ROOT / "docs/crawl-jobs-v2.md").read_text(encoding="utf-8")
    section = text.split("## 17. Required tests and acceptance evidence\n", 1)[1].split("## 18.", 1)[0]
    rows, current, subsection = [], [], "17"

    def finish() -> None:
        if current:
            wording = "\n".join(current).rstrip()
            rows.append({"requirement_id": subsection + "/" + digest(wording.encode())[:16],
                         "section": subsection, "requirement": wording,
                         "test_ids": [], "evidence": [], "status": "unimplemented"})
            current.clear()

    for line in section.splitlines():
        if line.startswith("### 17."):
            finish()
            subsection = line.split()[1]
        elif re.match(r"^(?:- |[0-9]+\. )", line):
            finish()
            current.append(line)
        elif current or line.strip():
            current.append(line)
    finish()
    require({row["section"] for row in rows} == {"17", *(f"17.{n}" for n in range(1, 8))}, "REQUIREMENT_INVENTORY")
    return rows


def compile_plan(inputs: dict) -> dict:
    exact(inputs, INPUT_FIELDS)
    require(type(inputs["format_version"]) is int and inputs["format_version"] == 1, "INVALID_VERSION")
    require(type(inputs["scenario"]) is str and inputs["scenario"] in SCENARIOS, "INVALID_SCENARIO")
    require(type(inputs["redis_version"]) is str and
            re.fullmatch(r"7\.[0-9]+(?:\.[0-9]+){0,2}", inputs["redis_version"]) is not None and
            len(inputs["redis_version"]) <= 64, "INVALID_REDIS_VERSION")
    for field in ("redis_image", "harness_image", "standin_image"):
        value = inputs[field]
        require(type(value) is str and value.startswith("sha256:") and nonzero(value[7:]), "INVALID_IMAGE")
    identities, operations = source_identity()
    config = (HERE / "redis.conf").read_bytes()
    profile, cutover = SCENARIOS[inputs["scenario"]]
    descriptors = {}
    for field in EVIDENCE_FIELDS:
        descriptor = {"domain": "mifolyo:crawl:v2:m4-test-input:v1",
                      "purpose": "conformance_only", "measurement_status": "not_measured",
                      "evidence_field": field, "scenario": inputs["scenario"],
                      "contract_sha256": identities["contract_sha256"],
                      "source_set_sha256": identities["source_set_sha256"],
                      "redis_config_sha256": digest(config)}
        descriptors[field] = {"artifact": descriptor, "sha256": digest(canonical(descriptor))}
    candidate = digest((inputs["scenario"] + ":candidate-run").encode())[:32] if cutover == "v1_migration" else ""
    core_fields = [("protocol_version", "2"), ("contract_sha256", identities["contract_sha256"]),
                   ("redis_version", inputs["redis_version"]), ("redis_config_sha256", digest(config))]
    core_fields += [(field, descriptors[field]["sha256"]) for field in EVIDENCE_FIELDS]
    core_fields += [("cutover_mode", cutover), ("candidate_run_id", candidate), ("approved", "1")]
    core = packed(core_fields)
    compatibility_fields = [("manifest_version", "1"), ("crawl_jobs", "2"), ("crawl_policy", "2"),
        ("canonicalization", "1"), ("page_publication", "1"), ("image_manifest", "1"),
        ("backlink_projection", "1"), ("render_ipc", "2"), ("signal_queue", "retired"),
        ("global_request_concurrency", "2"), ("redis_config_sha256", digest(config)),
        ("commit_guard_sha256", core["sha256"])]
    compatibility_fields += [(field, inputs["standin_image"]) for field in IMAGE_FIELDS]
    compatibility_fields += [("render_worker_image", "disabled")]
    compatibility = packed(compatibility_fields)
    marker = packed([compatibility_fields[0], ("manifest_sha256", compatibility["sha256"]), *compatibility_fields[1:]])
    return {"format_version": 1, "purpose": "conformance_only", "artifact_kind": "offline_plan",
            "execution_authorized": False, "release_eligible": False,
            "measurement_status": "not_measured", "profile": profile, "inputs": inputs,
            "identities": identities, "compiler_sha256": digest(Path(__file__).read_bytes()),
            "redis_config": {"sha256": digest(config), "bytes_b64": base64.b64encode(config).decode()},
            "image_verification": "pending", "isolation_verification": "pending",
            "role_images": {field: {"digest": inputs["standin_image"], "standin": True} for field in IMAGE_FIELDS},
            "test_descriptors": descriptors, "guard_core": core,
            "compatibility": compatibility, "compatibility_marker": marker,
            "operation_inventory": operation_inventory(operations),
            "required_execution_gates": ["reviewed_recipe_and_artifact_pins", "actual_image_verification",
                "complete_case_and_acl_inventory", "isolation_and_resource_verification",
                "real_preliminary_boot_evidence", "explicit_execution_approval", "verified_teardown"]}


def validate_plan(plan: dict) -> dict:
    require(type(plan) is dict and type(plan.get("inputs")) is dict, "INVALID_PLAN")
    expected = compile_plan(plan["inputs"])
    # Byte comparison, not Python equality (where True equals 1).
    require(canonical(plan) == canonical(expected), "PLAN_MISMATCH")
    return expected


def ledger_setup(plan: dict, redis_time_ms: int) -> dict:
    """Compile ONLY the ledger authority setup, never boot approval or live data.

    redis_time_ms is a recorded input for an offline projection. This function
    does not assert it was observed; the future runner must capture Redis TIME.
    """
    plan = validate_plan(plan)
    require(plan["profile"] == "ledger", "PROFILE_SETUP_FORBIDDEN")
    require(type(redis_time_ms) is int and 0 < redis_time_ms <= MAX_EXACT, "INVALID_REDIS_TIME")
    core = plan["guard_core"]["fields"]
    manifest = plan["compatibility"]["sha256"]
    guard = packed([*core[:2], ("compatibility_manifest_sha256", manifest), *core[2:10],
                    ("approved_at_ms", str(redis_time_ms)), core[10]])
    # Named synthetic artifacts, not alleged retained backups or observations.
    scenario = plan["inputs"]["scenario"]
    empty_artifacts = {name: {"purpose": "conformance_only", "scenario": scenario,
                             "kind": name, "entries": []} for name in
                       ("backup", "source", "queue", "urls", "depths", "spider", "signal")}
    empty = {name: digest(canonical(value)) for name, value in empty_artifacts.items()}
    legacy = packed([("protocol_version", "2"), ("freeze_nonce", digest(("m4-" + scenario + "-freeze").encode())[:32]),
        ("backup_sha256", empty["backup"]), ("v1_count", "0"), ("v1_url_field_count", "0"),
        ("v1_depth_field_count", "0"), ("v1_source_sha256", empty["source"]),
        ("v1_queue_evidence_sha256", empty["queue"]), ("v1_urls_evidence_sha256", empty["urls"]),
        ("v1_depths_evidence_sha256", empty["depths"]), ("spider_queue_type", "none"),
        ("spider_queue_count", "0"), ("spider_queue_evidence_sha256", empty["spider"]),
        ("signal_queue_type", "none"), ("signal_queue_count", "0"),
        ("signal_queue_evidence_sha256", empty["signal"]), ("deleted_bitmap", "00000"),
        ("retired_at_ms", str(redis_time_ms))])
    writes = []
    for key, artifact in ((AUTH[1], plan["compatibility_marker"]), (AUTH[5], guard), (AUTH[6], legacy)):
        writes.append({"key": key, "type": "hash", "fields": artifact["fields"],
                       "record_sha256": artifact["sha256"], "expiry": "persistent"})
    writes.append({"key": AUTH[2], "type": "string", "value": plan["identities"]["contract_sha256"],
                   "value_sha256": digest(plan["identities"]["contract_sha256"].encode()), "expiry": "persistent"})
    return {"format_version": 1, "artifact_kind": "offline_setup_projection",
            "purpose": "conformance_only", "execution_authorized": False,
            "plan_sha256": digest(canonical(plan)), "redis_time_ms": redis_time_ms,
            "time_observation_verified": False, "writes": sorted(writes, key=lambda row: row["key"]),
            "required_absent": sorted((*ABSENCE_ONLY, *LEGACY)),
            "boot_required": "real_probe_and_canonical_approve_boot",
            "empty_artifacts": empty_artifacts, "stored_guard": guard, "legacy_retirement": legacy}


def validate_setup(plan: dict, setup: dict) -> None:
    require(type(setup) is dict, "INVALID_SETUP")
    expected = ledger_setup(plan, setup.get("redis_time_ms"))
    require(canonical(setup) == canonical(expected), "SETUP_MISMATCH")


def ledger_acl_selectors(wire_keys: tuple[str, ...], data_read: tuple[str, ...],
                         data_write: tuple[str, ...]) -> tuple[str, ...]:
    """Compile a bounded ledger ACL fragment, not a user/password or admin ACL.

    Concrete keys only; a future approved case must supply its complete derived
    inventory. This compiler enforces authority separation but does not prove
    operation-specific membership or Redis ACL behavior.
    """
    for keys in (wire_keys, data_read, data_write):
        require(type(keys) is tuple and 0 < len(keys) <= 512 and len(set(keys)) == len(keys), "KEY_INVENTORY")
        for key in keys:
            require(type(key) is str and re.fullmatch(r"[a-zA-Z0-9_:.-]{1,256}", key) is not None, "INVALID_ACL_KEY")
    require(set(AUTH) <= set(wire_keys), "MISSING_AUTH_KEYS")
    # No caller may classify an authority or legacy name as writable data.
    reserved = set(AUTH) | set(LEGACY)
    require(not reserved.intersection((*data_read, *data_write)), "AUTHORITY_DATA_OVERLAP")
    require(set(data_write) <= set(data_read), "WRITE_WITHOUT_READ")
    require(not set(LEGACY).intersection(wire_keys), "LEGACY_REQUIRES_SEPARATE_ROLE")
    require(set(wire_keys) <= set(AUTH) | set(data_read), "UNLISTED_WIRE_KEY")

    def selector(commands: str, keys: tuple[str, ...], mode: str) -> str:
        return "(" + commands + " " + " ".join(mode + key for key in sorted(keys)) + ")"
    return ("resetkeys resetchannels clearselectors -@all",
        selector("+evalsha", wire_keys, "~"),
        selector("+type", ABSENCE_ONLY, "%R~"),
        selector("+type +hlen +hstrlen +hmget +strlen +get",
                 tuple(key for key in AUTH if key not in ABSENCE_ONLY), "%R~"),
        selector("+type +hlen +hstrlen +hmget +strlen +get +pttl +hkeys +scard +sismember "
                 "+smembers +zcard +zscore +zrange +zrangebyscore +zrangebylex +llen +lrange",
                 data_read, "%R~"),
        selector("+hset +set +unlink +hdel +sadd +srem +zadd +zrem +rename +persist +lpush +pexpireat +expire",
                 data_write, "~"),
        "(+time +info|server +info|memory)")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("inventory", help="Emit unexecuted operation and requirement inventories")
    prepare = sub.add_parser("prepare", help="Compile a non-executable plan from digest-bound inputs")
    prepare.add_argument("--inputs", type=Path, required=True)
    prepare.add_argument("--expected-input-sha256", required=True)
    validate = sub.add_parser("validate", help="Recompute a plan and optional setup projection")
    validate.add_argument("--plan", type=Path, required=True)
    validate.add_argument("--setup", type=Path)
    args = parser.parse_args()
    try:
        if args.command == "inventory":
            identities, operations = source_identity()
            output = {"purpose": "conformance_only", "execution_authorized": False,
                      "identities": identities, "operations": operation_inventory(operations),
                      "requirements": requirement_inventory()}
        elif args.command == "prepare":
            raw = read_artifact(args.inputs)
            require(nonzero(args.expected_input_sha256) and digest(raw) == args.expected_input_sha256,
                    "INPUT_DIGEST_MISMATCH")
            output = compile_plan(decode(raw))
        else:
            plan = validate_plan(decode(read_artifact(args.plan)))
            if args.setup:
                validate_setup(plan, decode(read_artifact(args.setup)))
            output = {"offline_validation": "PASS", "execution_authorized": False,
                      "release_eligible": False, "plan_sha256": digest(canonical(plan))}
        sys.stdout.buffer.write(canonical(output))
        return 0
    except (InvalidArtifact, OSError, ValueError, KeyError, TypeError, RecursionError):
        # Never emit submitted marker fields, paths, tokens or raw exception data.
        print("M4 offline validation failed; no execution authorized", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
