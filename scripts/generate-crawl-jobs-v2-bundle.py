#!/usr/bin/env python3
"""Pin the complete, dormant Crawl Jobs V2 canonical Lua bundle at build time.

Only the fixed 43 existing canonical files and exact normative document bytes
are inputs. This is NOT an assembler, source-behavior review, commit-guard
approval, or runtime loader. Missing sources are fatal, never synthesized.
Run again after an explicitly reviewed canonical regeneration; --check writes
nothing and rejects stale Go pins, fixture identities, and dependent guards.
"""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import stat
import struct
import sys


# Independent, literal protocol order. Never discover policy from files, Go,
# the assembler, or the fixture being generated.
OPERATIONS = (
    "CJ2_APPROVE_BOOT", "CJ2_INSTALL_CANDIDATE_MARKERS", "CJ2_RETIRE_LEGACY_KEYS",
    "CJ2_PROMOTE_CANDIDATE_CONTRACTS", "CJ2_MARK_PLANNED_SHUTDOWN", "CJ2_CREATE_RUN",
    "CJ2_ENQUEUE_BATCH", "CJ2_BEGIN_RUN_AUDIT", "CJ2_AUDIT_RUN_BATCH", "CJ2_SEAL_RUN",
    "CJ2_ACTIVATE_RUN", "CJ2_REJECT_READY", "CJ2_TRY_CLAIM", "CJ2_RENEW_LEASE",
    "CJ2_RESERVE_REQUEST", "CJ2_START_REQUEST", "CJ2_FINISH_REQUEST", "CJ2_CANCEL_RESERVATION",
    "CJ2_RELEASE_BEFORE_IO", "CJ2_RETRY", "CJ2_DEAD", "CJ2_CANCEL_JOB", "CJ2_COMPLETE_NO_OUTPUT",
    "CJ2_BEGIN_STAGE", "CJ2_STAGE_PAGE_FIELDS", "CJ2_STAGE_PAGE_BLOB", "CJ2_STAGE_OUTLINKS_BATCH",
    "CJ2_STAGE_DISCOVERIES_BATCH", "CJ2_STAGE_ALIASES_BATCH", "CJ2_STAGE_IMAGES_BATCH",
    "CJ2_STAGE_IMAGE_MANIFEST", "CJ2_ABORT_STAGE", "CJ2_SEAL_STAGE", "CJ2_COMMIT",
    "CJ2_PROMOTE_DUE", "CJ2_RECOVER_EXPIRED", "CJ2_CANCEL_RUN", "CJ2_CANCEL_BATCH",
    "CJ2_FINALIZE_RUN", "CJ2_ARCHIVE_RUN", "CJ2_PURGE_RUN_BATCH", "CJ2_CLEAN_STAGE",
    "CJ2_MAINTAIN_RATE_SCOPES",
)
PACKAGE = "services/spider/internal/database/crawljobsv2"
DOCUMENT = "docs/crawl-jobs-v2.md"
FIXTURE = "contracts/crawl-jobs-v2/digest-vectors.json"
GENERATED = PACKAGE + "/script_bundle_generated.go"
CANONICAL_CASE = "canonical-lua-bundle"
CANONICAL_INPUT = {"document_path": DOCUMENT, "lua_directory": PACKAGE + "/lua"}
BUNDLE_MUTATIONS = (
    ("missing_source", "BUNDLE_INVENTORY"),
    ("extra_source", "BUNDLE_INVENTORY"),
    ("swapped_sources", "BUNDLE_IDENTITY_MISMATCH"),
    ("duplicate_sources", "BUNDLE_IDENTITY_MISMATCH"),
    ("duplicate_entry", "BUNDLE_INVENTORY"),
    ("swapped_entries", "BUNDLE_INVENTORY"),
    ("source_alias_path", "BUNDLE_INVENTORY"),
    ("invalid_utf8", "BUNDLE_SOURCE_BYTES"),
    ("missing_newline", "BUNDLE_SOURCE_BYTES"),
    ("one_byte_drift", "BUNDLE_IDENTITY_MISMATCH"),
    ("stale_redis_sha1", "BUNDLE_IDENTITY_MISMATCH"),
    ("stale_source_sha256", "BUNDLE_IDENTITY_MISMATCH"),
    ("source_set_substitution", "BUNDLE_IDENTITY_MISMATCH"),
    ("bundle_seal_substitution", "BUNDLE_IDENTITY_MISMATCH"),
    ("contract_substitution", "BUNDLE_IDENTITY_MISMATCH"),
    ("contract_resealed", "BUNDLE_IDENTITY_MISMATCH"),
    ("document_one_byte_drift", "BUNDLE_IDENTITY_MISMATCH"),
    ("source_resealed", "BUNDLE_IDENTITY_MISMATCH"),
)


def frame(value: str | bytes) -> bytes:
    raw = value.encode("utf-8") if isinstance(value, str) else value
    return struct.pack(">Q", len(raw)) + raw


def record(fields: list[tuple[str, str | bytes]]) -> bytes:
    return struct.pack(">Q", len(fields)) + b"".join(frame(k) + frame(v) for k, v in fields)


def section(label: str, records: list[list[tuple[str, str | bytes]]]) -> bytes:
    return frame(label) + struct.pack(">Q", len(records)) + b"".join(frame(record(r)) for r in records)


def sha256(raw: bytes) -> str:
    return hashlib.sha256(raw).hexdigest()


def closed_path(root: Path, relative: str, *, optional: bool = False) -> Path:
    path = root
    parts = relative.split("/")
    for index, part in enumerate(parts):
        if part in {"", ".", ".."}:
            raise ValueError("noncanonical repository path")
        path = path / part
        try:
            mode = path.lstat().st_mode
        except FileNotFoundError:
            if optional and index == len(parts) - 1:
                return path
            raise
        if stat.S_ISLNK(mode) or (index < len(parts) - 1 and not stat.S_ISDIR(mode)):
            raise ValueError(f"symlink or non-directory in fixed repository path: {relative}")
    return path


def read_text(path: Path) -> bytes:
    if not stat.S_ISREG(path.lstat().st_mode):
        raise ValueError(f"not a regular file: {path.name}")
    raw = path.read_bytes()
    raw.decode("utf-8", "strict")
    if not raw or not raw.endswith(b"\n"):
        raise ValueError(f"expected nonempty UTF-8 with final newline: {path.name}")
    return raw


def bundle(root: Path) -> tuple[bytes, list[tuple[str, bytes]], dict]:
    if len(OPERATIONS) != 43 or len(set(OPERATIONS)) != 43:
        raise ValueError("invalid literal 43-operation inventory")
    directory = closed_path(root, PACKAGE + "/lua")
    if not stat.S_ISDIR(directory.lstat().st_mode):
        raise ValueError("canonical lua path is not a directory")
    names = [op.lower() + ".lua" for op in OPERATIONS]
    files = list(directory.iterdir())  # Includes hidden entries; NEVER glob *.lua.
    if len(files) != 43 or {p.name for p in files} != set(names):
        raise ValueError("canonical Lua inventory must be exactly the literal 43 files")
    sources = [(name, read_text(closed_path(root, PACKAGE + "/lua/" + name))) for name in names]
    document = read_text(closed_path(root, DOCUMENT))
    identities = []
    encoded = frame("mifolyo:crawl-jobs-v2:lua-source-set:v1") + frame("43")
    for index, (op, (name, source)) in enumerate(zip(OPERATIONS, sources)):
        redis_sha1 = hashlib.sha1(source).hexdigest()  # Redis identity, not security authority.
        source_sha256 = sha256(source)
        identities.append({"index": index, "operation": op, "source_name": name,
                           "source_bytes": len(source), "redis_sha1": redis_sha1,
                           "source_sha256": source_sha256})
        encoded += b"".join(frame(v) for v in (str(index), op, name, source, redis_sha1, source_sha256))
    for field in ("redis_sha1", "source_sha256"):
        if len({entry[field] for entry in identities}) != 43:
            raise ValueError("duplicate/aliased canonical Lua sources")
    source_set = sha256(encoded)
    ordered = sorted(sources, key=lambda item: item[0].encode("ascii"))
    contract = sha256(frame("mifolyo:crawl-contract:v2") + section("document", [[("document_bytes", document)]])
                      + section("lua", [[("source_name", name), ("source_bytes", raw)] for name, raw in ordered]))
    seal = sha256(frame("mifolyo:crawl-jobs-v2:lua-bundle-seal:v1") + frame(source_set) + frame(contract))
    for digest in (source_set, contract, seal):
        if any(digest.encode("ascii") in raw for raw in [document, *(source for _, source in sources)]):
            raise ValueError("self-referential Lua/document bundle digest")
    return document, sources, {"entries": identities, "lua_source_order": [name for name, _ in ordered],
                               "source_set_sha256": source_set, "contract_sha256": contract, "bundle_seal_sha256": seal}


def generated_go(result: dict) -> bytes:
    lines = [
        "// Code generated by scripts/generate-crawl-jobs-v2-bundle.py; DO NOT EDIT.",
        "// Complete source identity only; NOT commit-guard approval or runtime activation.",
        "package crawljobsv2", "", 'import "embed"', "",
        "// all: includes hidden files and subdirectories so validation cannot silently omit them.", "//",
        "//go:embed all:lua", "var authoritativeLuaFS embed.FS", "",
        'const canonicalSourceSetSHA256 Digest = "' + result["source_set_sha256"] + '"',
        'const canonicalContractSHA256 Digest = "' + result["contract_sha256"] + '"',
        'const canonicalBundleSealSHA256 Digest = "' + result["bundle_seal_sha256"] + '"', "",
        "// A fresh literal array, never a mutable package-level manifest.",
        "func canonicalScriptBindingPins() [43]scriptBinding {", "\treturn [43]scriptBinding{",
    ]
    for entry in result["entries"]:
        lines.append('\t\t{operation: "' + entry["operation"] + '", sourceName: "' + entry["source_name"]
                     + '", redisSHA1: "' + entry["redis_sha1"] + '", sourceSHA256: "' + entry["source_sha256"] + '"},')
    lines.extend(["\t}", "}", ""])
    return "\n".join(lines).encode("utf-8")


def strict_pairs(pairs: list[tuple[str, object]]) -> dict:
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate fixture JSON key: " + key)
        result[key] = value
    return result


def guard_expected(case: dict, contract: str) -> dict:
    # Build-time-only independent RECORD formulas. The verifier does not import
    # this generator; Go and Python each recompute these from real artifacts.
    data = case["input"]
    core = data["guard_core"]
    fields = [("protocol_version", "2"), ("contract_sha256", contract)] + [
        (key, core[key]) for key in ("redis_version", "redis_config_sha256", "maximum_shape_sha256",
                                    "memory_fixture_sha256", "lua_benchmark_sha256", "aof_crash_evidence_sha256",
                                    "cutover_mode", "candidate_run_id")
    ] + [("approved", "1")]
    core_hash = sha256(record(fields))
    if case["kind"] == "guard_core":
        return {"guard_core_sha256": core_hash, "record_hex": record(fields).hex()}
    compatibility = data["compatibility"]
    manifest = [("manifest_version", "1"), ("crawl_jobs", "2"), ("crawl_policy", "2"),
                ("canonicalization", "1"), ("page_publication", "1"), ("image_manifest", "1"),
                ("backlink_projection", "1"), ("render_ipc", "2"), ("signal_queue", "retired"),
                ("global_request_concurrency", "2"), ("redis_config_sha256", compatibility["redis_config_sha256"]),
                ("commit_guard_sha256", core_hash)] + [
                    (key, compatibility[key]) for key in ("spider_image", "seed_importer_image", "crawl_admin_image",
                                                         "indexer_image", "image_indexer_image", "backlinks_processor_image",
                                                         "monitoring_image", "render_worker_image")]
    manifest_hash = sha256(record(manifest))
    marker = [manifest[0], ("manifest_sha256", manifest_hash), *manifest[1:]]
    stored = [*fields[:2], ("compatibility_manifest_sha256", manifest_hash), *fields[2:10],
              ("approved_at_ms", str(data["approved_at_ms"])), fields[10]]
    return {"guard_core_sha256": core_hash, "compatibility_manifest_sha256": manifest_hash,
            "compatibility_marker_sha256": sha256(record(marker)), "stored_guard_sha256": sha256(record(stored))}


def fixture_bytes(raw: bytes, document: bytes, result: dict) -> bytes:
    text = raw.decode("utf-8", "strict")
    data = json.loads(text, object_pairs_hook=strict_pairs)
    names = [case["name"] for case in data["cases"] + data["negative_vectors"]]
    if len(names) != len(set(names)):
        raise ValueError("duplicate fixture case name")
    replacements = {}
    canonical = {"name": CANONICAL_CASE, "kind": "canonical_lua_bundle", "input": CANONICAL_INPUT, "expected": result}
    found = False
    for case in data["cases"]:
        if case["name"] == CANONICAL_CASE:
            if case["kind"] != canonical["kind"] or case["input"] != CANONICAL_INPUT:
                raise ValueError("canonical case may not supply other paths or sources")
            replacements[case["name"]] = canonical
            found = True
        elif case["kind"] in {"guard_chain", "guard_core"}:
            case["input"]["contract_case"] = CANONICAL_CASE
            case["expected"] = guard_expected(case, result["contract_sha256"])
            replacements[case["name"]] = case
        elif case["kind"] == "contract_digest" and case["input"]["document"] == {"kind": "path", "value": DOCUMENT}:
            # Preserve foundation empty/synthetic Lua framing as primitive cases,
            # including when the core owner explicitly changes the document.
            sources = sorted(case["input"]["lua_sources"], key=lambda source: source["source_name"].encode("ascii"))
            case["expected"] = {"lua_source_order": [s["source_name"] for s in sources], "contract_sha256": sha256(
                frame("mifolyo:crawl-contract:v2") + section("document", [[("document_bytes", document)]])
                + section("lua", [[("source_name", s["source_name"]), ("source_bytes", s["source_text"])] for s in sources]))}
            replacements[case["name"]] = case
    decoder = json.JSONDecoder(object_pairs_hook=strict_pairs)

    def formatted(value: dict) -> str:
        return json.dumps(value, ensure_ascii=False, indent=2).replace("\n", "\n    ")

    # Replace only selected case objects; preserve all other agents' fixture
    # content/formatting, including every preexisting negative vector.
    start = text.index('"cases": [') + len('"cases": [')
    position = start
    edits = []
    first_guard = None
    while True:
        while text[position].isspace() or text[position] == ",":
            position += 1
        if text[position] == "]":
            break
        case, end = decoder.raw_decode(text, position)
        if first_guard is None and case["kind"] in {"guard_chain", "guard_core"}:
            first_guard = position
        if case["name"] in replacements:
            edits.append((position, end, formatted(replacements[case["name"]])))
        position = end
    if not found:
        if first_guard is None:
            raise ValueError("missing independent guard chain")
        edits.append((first_guard, first_guard, formatted(canonical) + ",\n    "))
    for start, end, replacement in sorted(edits, reverse=True):
        text = text[:start] + replacement + text[end:]
    additions = []
    existing = {case["name"]: case for case in data["negative_vectors"]}
    for mutation, rejection in BUNDLE_MUTATIONS:
        case = {"name": "canonical-bundle-" + mutation.replace("_", "-"), "kind": "canonical_lua_bundle_mutation",
                "input": {"base_case": CANONICAL_CASE, "mutation": mutation}, "expected_rejection_class": rejection}
        if case["name"] in existing:
            if existing[case["name"]] != case:
                raise ValueError("canonical negative case differs from explicit inventory")
        else:
            additions.append(case)
    if additions:
        position = text.rindex("]")
        before = text[:position].rstrip()
        text = before + ",\n    " + ",\n    ".join(formatted(case) for case in additions) + "\n  " + text[position:]
    return text.encode("utf-8")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="fail on missing/stale pins; never write")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    document, sources, result = bundle(root)  # Complete validation before ANY output write.
    fixture = closed_path(root, FIXTURE)
    raw_fixture = read_text(fixture)
    go_path = closed_path(root, GENERATED, optional=True)
    outputs = [(go_path, generated_go(result)), (fixture, fixture_bytes(raw_fixture, document, result))]
    # Recheck the input snapshot before publishing; a concurrent canonical
    # regeneration must not silently produce mixed-source authority.
    if (document, sources, result) != bundle(root) or raw_fixture != read_text(fixture):
        raise ValueError("canonical inputs changed during generation; retry after review/regeneration")
    stale = []
    for path, expected in outputs:
        current = read_text(path) if path.exists() else None
        if current != expected:
            stale.append(str(path.relative_to(root)))
    if args.check and stale:
        raise ValueError("stale/missing bundle pins: " + ", ".join(stale))
    if not args.check:
        for path, expected in outputs:
            if str(path.relative_to(root)) in stale:
                path.write_bytes(expected)
    print("canonical Lua bundle: 43/43 (source identity only; dormant, no behavior/operational acceptance)")
    for key in ("contract_sha256", "source_set_sha256", "bundle_seal_sha256"):
        print(key + "=" + result[key])
    vectors = json.loads(outputs[1][1], object_pairs_hook=strict_pairs)
    inventory = hashlib.sha256(("B\0" + vectors["baseline_case_name"] + "\n").encode("utf-8"))
    for case in vectors["cases"]:
        inventory.update(("P\0" + case["name"] + "\0" + case["kind"] + "\n").encode("utf-8"))
    for case in vectors["negative_vectors"]:
        inventory.update(("N\0" + case["name"] + "\0" + case["kind"] + "\0" + case["expected_rejection_class"] + "\n").encode("utf-8"))
    print(f"fixture cases: positive={len(vectors['cases'])} negative={len(vectors['negative_vectors'])} (plus baseline)")
    print("fixture_inventory_sha256=" + inventory.hexdigest())
    print("fixture_file_sha256=" + sha256(outputs[1][1]))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, KeyError) as error:
        print("canonical bundle generation failed: " + str(error), file=sys.stderr)
        raise SystemExit(1)
