#!/usr/bin/env python3
"""Strict offline verifier for the Crawl Jobs V2 shared conformance vectors.

Requires Python 3.10+ (the pinned URL helper uses dataclass(slots=True)); CI
tests with Python 3.13. Only the standard library and checked-in inputs are
needed, with no Go/Lua runtime, network access, or running services.
Run from repository root: python3 -B scripts/verify-crawl-jobs-v2-digests.py

The optional fixture path defaults to contracts/crawl-jobs-v2/digest-vectors.json
under the repository containing this script, not the current directory.
This verifier reads inputs; it never rewrites fixtures or pins. --print-computed
prints diagnostic JSON after schema validation, computation, and negative
checks, but skips the normal final comparison with stored expected results.
It is not fixture regeneration or a substitute for a normal verification run.

URL checks use the fixed sibling crawl_jobs_v2_url.py and its hash-checked
Unicode literals, not the active Seed Importer normalizer. These are dormant
conformance checks, not crawl admission or operational acceptance. See
services/spider/internal/database/crawljobsv2/lua_src/README.md for Unicode and
Python maintenance, exact Go/module pins, and regeneration order.
"""

from __future__ import annotations

import argparse
import base64
import copy
import datetime as dt
import hashlib
import importlib.util
import json
import math
import re
import stat
import struct
import sys
import unicodedata
from pathlib import Path


# Fixed sibling import also works under runpy and from a non-repository cwd.
# Neither a fixture path nor PYTHONPATH chooses the URL implementation/data.
_url_spec = importlib.util.spec_from_file_location(
    "_crawl_jobs_v2_url_pinned", Path(__file__).resolve().with_name("crawl_jobs_v2_url.py")
)
if _url_spec is None or _url_spec.loader is None:
    raise ImportError("pinned Crawl Jobs V2 URL helper unavailable")
_url_helper = importlib.util.module_from_spec(_url_spec)
sys.modules[_url_spec.name] = _url_helper
_url_spec.loader.exec_module(_url_helper)


MAX_U64 = (1 << 64) - 1
MAX_EXACT_INTEGER = 9_007_199_254_740_991
MAX_CANONICAL_URL_BYTES = 2_048
MAX_PAGE_BLOB_BYTES = 5 * 1_024 * 1_024
MAX_COMBINED_HTML_BYTES = 10 * 1_024 * 1_024
MAX_CONTENT_TYPE_BYTES = 1_024
MAX_IMAGE_ALT_BYTES = 1_024
MAX_GROUP_ID_BYTES = 128
MAX_RENDER_RULE_BYTES = 128
MAX_OUTLINKS = 256
MAX_DISCOVERIES = 128
MAX_IMAGES = 64
MAX_ALIASES = 5
MAX_SOURCE_JOBS = 10_000
MAX_POLICY_GROUPS = 64
MAX_NON_BLOB_CHUNK_RECORDS = 64
MAX_IMAGE_MANIFEST_BYTES = 393_216
MAX_REQUEST_STARTS = 10
MAX_REQUEST_ORDINAL = 100
FIXTURE_POSITIVE_CASE_COUNT = 40
FIXTURE_NEGATIVE_CASE_COUNT = 157
FIXTURE_INVENTORY_SHA256 = "8c360cf46c283ec1111e9c4a4e9896a2424afc10824566e174bdfcbabc6c15f8"

SCORE_RE = re.compile(
    r"^(?:0|-?(?:[1-9][0-9]*(?:\.[0-9]{0,5}[1-9])?|0\.[0-9]{0,5}[1-9]))$"
)
HEX_32_RE = re.compile(r"^[0-9a-f]{32}$")
HEX_64_RE = re.compile(r"^[0-9a-f]{64}$")
HEX_RE = re.compile(r"^(?:[0-9a-fA-F]{2})*$")
CANONICAL_DECIMAL_RE = re.compile(r"^(?:0|[1-9][0-9]*)$")
REDIS_FLOAT_RE = re.compile(
    r"^[+-]?(?:(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?|inf(?:inity)?|nan)$",
    re.IGNORECASE,
)
CONTENT_TYPE_RE = re.compile(
    r'^text/html(?: *; *charset *= *(?:utf-8|"utf-8"))?$', re.IGNORECASE
)
SOURCE_NAME_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._/-]*$")

CHUNK_KINDS = (
    "page_fields",
    "html",
    "original_html",
    "outlinks",
    "discoveries",
    "aliases",
    "images",
    "image_manifest",
)
OUTPUT_SECTION_LABELS = ("page", "outlinks", "images", "discoveries", "aliases")
TRANSITION_NAMES = (
    "reject_ready",
    "try_claim",
    "release_before_io",
    "retry",
    "dead",
    "cancel_job",
    "complete_no_output",
    "abort_stage",
)

RETRYABLE_REASONS = {
    "request_timeout",
    "dns_temporary",
    "dial_temporary",
    "request_temporary",
    "http_429",
    "http_5xx",
    "robots_temporary",
    "renderer_temporary",
    "downstream_backpressure",
    "capacity_blocked_after_io",
    "run_budget_exhausted_after_io",
    "group_budget_exhausted_after_io",
    "rate_blocked_after_io",
    "lease_expired_after_io",
    "worker_shutdown_after_io",
}
DEAD_REASONS = {
    "policy_denied",
    "policy_scope_changed",
    "robots_denied",
    "robots_invalid",
    "job_malformed",
    "url_identity_mismatch",
    "static_url_denied",
    "dns_prohibited",
    "http_4xx",
    "response_invalid",
    "body_too_large",
    "html_invalid",
    "discovery_limit",
    "renderer_permanent",
    "output_invalid",
    "run_job_limit",
    "reservation_limit_exhausted",
    "retry_exhausted",
    "pre_io_recovery_exhausted",
    "protocol_corrupt",
}
CANCELLATION_REASONS = {"authorization_expired", "operator_cancelled", "source_cancelled"}
REJECT_READY_REASONS = {
    "policy_denied",
    "policy_scope_changed",
    "job_malformed",
    "url_identity_mismatch",
    "static_url_denied",
}


class FixtureError(Exception):
    """The fixture format or an asserted result is invalid."""


class Rejection(Exception):
    """A stable validation rejection produced by one conformance operation."""

    def __init__(self, rejection_class: str):
        super().__init__(rejection_class)
        self.rejection_class = rejection_class


def reject(rejection_class: str) -> None:
    raise Rejection(rejection_class)


def _strict_pairs(pairs: list[tuple[str, object]], rejection_mode: bool) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            if rejection_mode:
                reject("DUPLICATE_JSON_KEY")
            raise FixtureError(f"duplicate JSON key {key!r}")
        result[key] = value
    return result


def loads_strict(raw: str, *, rejection_mode: bool = False) -> object:
    def bad_constant(value: str) -> object:
        if rejection_mode:
            reject("NON_STANDARD_JSON_NUMBER")
        raise FixtureError(f"non-standard JSON number {value!r}")

    try:
        return json.loads(
            raw,
            object_pairs_hook=lambda pairs: _strict_pairs(pairs, rejection_mode),
            parse_constant=bad_constant,
        )
    except json.JSONDecodeError as error:
        if rejection_mode:
            reject("INVALID_JSON")
        raise FixtureError(f"invalid JSON: {error}") from error


def expect_object(value: object, path: str) -> dict[str, object]:
    if type(value) is not dict:
        raise FixtureError(f"{path}: expected object, got {type(value).__name__}")
    return value


def expect_list(value: object, path: str) -> list[object]:
    if type(value) is not list:
        raise FixtureError(f"{path}: expected array, got {type(value).__name__}")
    return value


def expect_text(value: object, path: str, *, utf8: bool = True) -> str:
    if type(value) is not str:
        raise FixtureError(f"{path}: expected string, got {type(value).__name__}")
    if utf8:
        try:
            value.encode("utf-8", "strict")
        except UnicodeEncodeError as error:
            raise FixtureError(f"{path}: string is not valid UTF-8") from error
    return value


def expect_integer(value: object, path: str) -> int:
    if type(value) is not int:
        raise FixtureError(f"{path}: expected integer (boolean is not an integer)")
    return value


def expect_boolean(value: object, path: str) -> bool:
    if type(value) is not bool:
        raise FixtureError(f"{path}: expected boolean")
    return value


def expect_keys(value: object, expected: set[str] | tuple[str, ...], path: str) -> dict[str, object]:
    obj = expect_object(value, path)
    expected_set = set(expected)
    actual = set(obj)
    if actual != expected_set:
        missing = sorted(expected_set - actual)
        extra = sorted(actual - expected_set)
        raise FixtureError(f"{path}: key set differs; missing={missing}, extra={extra}")
    for key in obj:
        if type(key) is not str:
            raise FixtureError(f"{path}: object key is not a string")
    return obj


def expect_hex(value: object, length: int, path: str) -> str:
    text = expect_text(value, path)
    pattern = HEX_32_RE if length == 32 else HEX_64_RE if length == 64 else None
    if pattern is None or pattern.fullmatch(text) is None:
        raise FixtureError(f"{path}: expected {length} lowercase hexadecimal characters")
    return text


def expect_hex_bytes(value: object, path: str) -> str:
    text = expect_text(value, path)
    if HEX_RE.fullmatch(text) is None:
        raise FixtureError(f"{path}: expected even-length hexadecimal bytes")
    return text


def schema_string_list(value: object, path: str) -> None:
    for index, item in enumerate(expect_list(value, path)):
        expect_text(item, f"{path}[{index}]")


def schema_digest_map(value: object, keys: set[str] | tuple[str, ...], path: str) -> None:
    obj = expect_keys(value, keys, path)
    for key in obj:
        expect_hex(obj[key], 64, f"{path}.{key}")


def schema_counts(value: object, path: str) -> None:
    obj = expect_keys(value, OUTPUT_SECTION_LABELS, path)
    for key in obj:
        expect_integer(obj[key], f"{path}.{key}")


def schema_chunk_digest_map(value: object, path: str) -> None:
    obj = expect_keys(value, CHUNK_KINDS, path)
    for kind in CHUNK_KINDS:
        entries = expect_list(obj[kind], f"{path}.{kind}")
        for index, digest in enumerate(entries):
            expect_hex(digest, 64, f"{path}.{kind}[{index}]")


GUARD_CORE_KEYS = {
    "redis_version",
    "redis_config_sha256",
    "maximum_shape_sha256",
    "memory_fixture_sha256",
    "lua_benchmark_sha256",
    "aof_crash_evidence_sha256",
    "cutover_mode",
    "candidate_run_id",
}


def schema_guard_core(value: object, path: str) -> None:
    guard = expect_keys(value, GUARD_CORE_KEYS, path)
    expect_text(guard["redis_version"], f"{path}.redis_version")
    for field in (
        "redis_config_sha256",
        "maximum_shape_sha256",
        "memory_fixture_sha256",
        "lua_benchmark_sha256",
        "aof_crash_evidence_sha256",
    ):
        expect_hex(guard[field], 64, f"{path}.{field}")
    expect_text(guard["cutover_mode"], f"{path}.cutover_mode")
    candidate = expect_text(guard["candidate_run_id"], f"{path}.candidate_run_id")
    if candidate and HEX_32_RE.fullmatch(candidate) is None:
        raise FixtureError(f"{path}.candidate_run_id: invalid run ID")


def schema_repeat_generator(value: object, path: str) -> None:
    item = expect_keys(value, {"text", "repeat_count", "expected_utf8_bytes"}, path)
    expect_text(item["text"], f"{path}.text")
    expect_integer(item["repeat_count"], f"{path}.repeat_count")
    expect_integer(item["expected_utf8_bytes"], f"{path}.expected_utf8_bytes")


def schema_indexed_template_generator(value: object, path: str) -> None:
    item = expect_keys(
        value,
        {
            "grammar", "template", "count", "first_index", "index_width", "index_radix",
            "fill_to_bytes", "fill_byte_hex", "input_order",
        },
        path,
    )
    for field in ("grammar", "template", "fill_byte_hex", "input_order"):
        expect_text(item[field], f"{path}.{field}")
    for field in ("count", "first_index", "index_width", "index_radix", "fill_to_bytes"):
        expect_integer(item[field], f"{path}.{field}")
    if item["grammar"] != "indexed_template_v1":
        raise FixtureError(f"{path}.grammar: unsupported indexed-template grammar")
    if re.fullmatch(r"[0-9a-f]{2}", expect_text(item["fill_byte_hex"], f"{path}.fill_byte_hex")) is None:
        raise FixtureError(f"{path}.fill_byte_hex: expected exactly one lowercase hexadecimal byte")


def schema_generated_policy_group(value: object, path: str) -> None:
    item = expect_keys(
        value,
        {
            "group_id", "rate_scope_id", "request_start_limit", "global_concurrency",
            "global_interval_ms", "concurrency", "interval_ms", "origin_concurrency",
            "origin_interval_ms",
        },
        path,
    )
    schema_repeat_generator(item["group_id"], f"{path}.group_id")
    schema_repeat_generator(item["rate_scope_id"], f"{path}.rate_scope_id")
    for field in (
        "request_start_limit", "global_concurrency", "global_interval_ms", "concurrency",
        "interval_ms", "origin_concurrency", "origin_interval_ms",
    ):
        expect_integer(item[field], f"{path}.{field}")


def schema_output_context(value: object, path: str) -> None:
    context = expect_keys(
        value,
        {"source_job_index", "lease_request_starts_baseline", "terminal_request_starts_generation", "requests"},
        path,
    )
    expect_integer(context["source_job_index"], f"{path}.source_job_index")
    for field in ("lease_request_starts_baseline", "terminal_request_starts_generation"):
        expect_text(context[field], f"{path}.{field}")
    for index, request in enumerate(expect_list(context["requests"], f"{path}.requests")):
        request_path = f"{path}.requests[{index}]"
        item = expect_keys(
            request, {"request_kind", "target", "started_at_ms", "job_request_starts", "request_ordinal"},
            request_path,
        )
        for field in ("request_kind", "target", "job_request_starts", "request_ordinal"):
            expect_text(item[field], f"{request_path}.{field}")
        expect_integer(item["started_at_ms"], f"{request_path}.started_at_ms")


def schema_maximum_output_generator(value: object, path: str) -> None:
    generator = expect_keys(
        value,
        {
            "grammar", "identity", "policy_group", "aliases", "source", "request_chain",
            "page", "outlinks", "images", "discoveries",
        },
        path,
    )
    if expect_text(generator["grammar"], f"{path}.grammar") != "maximum_output_v1":
        raise FixtureError(f"{path}.grammar: unsupported maximum-output grammar")
    identity = expect_keys(
        generator["identity"],
        {
            "run_id", "job_id_from", "owner_id", "alternate_owner_id", "lease_token", "fence",
            "rate_scope_id_from", "crawl_policy_sha256",
        },
        f"{path}.identity",
    )
    for field in (
        "run_id", "job_id_from", "owner_id", "alternate_owner_id", "lease_token",
        "rate_scope_id_from", "crawl_policy_sha256",
    ):
        expect_text(identity[field], f"{path}.identity.{field}")
    expect_integer(identity["fence"], f"{path}.identity.fence")
    schema_generated_policy_group(generator["policy_group"], f"{path}.policy_group")
    schema_indexed_template_generator(generator["aliases"], f"{path}.aliases")
    source = expect_keys(
        generator["source"],
        {"target_index", "score_text", "depth", "request_kind", "policy_group_ref"},
        f"{path}.source",
    )
    expect_integer(source["target_index"], f"{path}.source.target_index")
    expect_integer(source["depth"], f"{path}.source.depth")
    for field in ("score_text", "request_kind", "policy_group_ref"):
        expect_text(source[field], f"{path}.source.{field}")
    request_chain = expect_keys(
        generator["request_chain"],
        {
            "initial_request_kind", "subsequent_request_kind", "first_started_at_ms",
            "started_at_step_ms", "policy_group_ref", "lease_request_starts_baseline",
            "terminal_request_starts_generation", "job_request_starts", "request_ordinals",
        },
        f"{path}.request_chain",
    )
    for field in ("initial_request_kind", "subsequent_request_kind", "policy_group_ref"):
        expect_text(request_chain[field], f"{path}.request_chain.{field}")
    for field in ("first_started_at_ms", "started_at_step_ms"):
        expect_integer(request_chain[field], f"{path}.request_chain.{field}")
    for field in ("lease_request_starts_baseline", "terminal_request_starts_generation"):
        expect_text(request_chain[field], f"{path}.request_chain.{field}")
    for field in ("job_request_starts", "request_ordinals"):
        schema_string_list(request_chain[field], f"{path}.request_chain.{field}")
    page = expect_keys(
        generator["page"],
        {
            "normalized_target_index", "html", "original_html", "content_type", "status_code",
            "rendered", "render_policy_rule", "render_policy_sha256",
        },
        f"{path}.page",
    )
    expect_integer(page["normalized_target_index"], f"{path}.page.normalized_target_index")
    schema_repeat_generator(page["html"], f"{path}.page.html")
    schema_repeat_generator(page["original_html"], f"{path}.page.original_html")
    content_type = expect_keys(
        page["content_type"], {"prefix", "fill_text", "suffix", "total_utf8_bytes"},
        f"{path}.page.content_type",
    )
    for field in ("prefix", "fill_text", "suffix"):
        expect_text(content_type[field], f"{path}.page.content_type.{field}")
    expect_integer(content_type["total_utf8_bytes"], f"{path}.page.content_type.total_utf8_bytes")
    expect_integer(page["status_code"], f"{path}.page.status_code")
    expect_boolean(page["rendered"], f"{path}.page.rendered")
    schema_repeat_generator(page["render_policy_rule"], f"{path}.page.render_policy_rule")
    expect_text(page["render_policy_sha256"], f"{path}.page.render_policy_sha256")
    schema_indexed_template_generator(generator["outlinks"], f"{path}.outlinks")
    images = expect_keys(generator["images"], {"urls", "alt"}, f"{path}.images")
    schema_indexed_template_generator(images["urls"], f"{path}.images.urls")
    schema_repeat_generator(images["alt"], f"{path}.images.alt")
    discoveries = expect_keys(
        generator["discoveries"],
        {"urls", "score_text", "depth", "request_kind", "policy_group_ref"},
        f"{path}.discoveries",
    )
    schema_indexed_template_generator(discoveries["urls"], f"{path}.discoveries.urls")
    expect_integer(discoveries["depth"], f"{path}.discoveries.depth")
    for field in ("score_text", "request_kind", "policy_group_ref"):
        expect_text(discoveries[field], f"{path}.discoveries.{field}")


def schema_maximum_source_generator(value: object, path: str) -> None:
    generator = expect_keys(value, {"grammar", "policy_group", "jobs", "source"}, path)
    if expect_text(generator["grammar"], f"{path}.grammar") != "maximum_source_v1":
        raise FixtureError(f"{path}.grammar: unsupported maximum-source grammar")
    schema_generated_policy_group(generator["policy_group"], f"{path}.policy_group")
    schema_indexed_template_generator(generator["jobs"], f"{path}.jobs")
    source = expect_keys(
        generator["source"], {"score_text", "depth", "request_kind", "policy_group_ref"},
        f"{path}.source",
    )
    expect_integer(source["depth"], f"{path}.source.depth")
    for field in ("score_text", "request_kind", "policy_group_ref"):
        expect_text(source[field], f"{path}.source.{field}")


def validate_case_schema(case: object, path: str) -> None:
    obj = expect_keys(case, {"name", "kind", "input", "expected"}, path)
    case_name = expect_text(obj["name"], f"{path}.name")
    if not case_name or len(case_name.encode("utf-8")) > 160:
        raise FixtureError(f"{path}.name: case name must contain 1 through 160 UTF-8 bytes")
    kind = expect_text(obj["kind"], f"{path}.kind")
    case_input = expect_object(obj["input"], f"{path}.input")
    expected = expect_object(obj["expected"], f"{path}.expected")

    if kind == "u64_max":
        expect_keys(case_input, {"decimal"}, f"{path}.input")
        expect_text(case_input["decimal"], f"{path}.input.decimal")
        expect_keys(expected, {"u64_hex", "frame_length_hex"}, f"{path}.expected")
        expect_hex_bytes(expected["u64_hex"], f"{path}.expected.u64_hex")
        expect_hex_bytes(expected["frame_length_hex"], f"{path}.expected.frame_length_hex")
    elif kind == "empty_section":
        expect_keys(case_input, {"label"}, f"{path}.input")
        expect_text(case_input["label"], f"{path}.input.label")
        expect_keys(expected, {"section_hex", "section_sha256"}, f"{path}.expected")
        expect_hex_bytes(expected["section_hex"], f"{path}.expected.section_hex")
        expect_hex(expected["section_sha256"], 64, f"{path}.expected.section_sha256")
    elif kind == "valid_score":
        expect_keys(case_input, {"score_text", "redis_value"}, f"{path}.input")
        expect_text(case_input["score_text"], f"{path}.input.score_text")
        expect_text(case_input["redis_value"], f"{path}.input.redis_value")
        expect_keys(expected, {"binary64_hex"}, f"{path}.expected")
        value = expect_text(expected["binary64_hex"], f"{path}.expected.binary64_hex")
        if re.fullmatch(r"[0-9a-f]{16}", value) is None:
            raise FixtureError(f"{path}.expected.binary64_hex: expected 16 lowercase hex characters")
    elif kind == "utf8_order":
        expect_keys(case_input, {"values"}, f"{path}.input")
        schema_string_list(case_input["values"], f"{path}.input.values")
        expect_keys(expected, {"ordered_values", "section_sha256"}, f"{path}.expected")
        schema_string_list(expected["ordered_values"], f"{path}.expected.ordered_values")
        expect_hex(expected["section_sha256"], 64, f"{path}.expected.section_sha256")
    elif kind == "contract_digest":
        expect_keys(case_input, {"document", "lua_sources"}, f"{path}.input")
        document = expect_keys(case_input["document"], {"kind", "value"}, f"{path}.input.document")
        document_kind = expect_text(document["kind"], f"{path}.input.document.kind")
        if document_kind not in {"path", "utf8"}:
            raise FixtureError(f"{path}.input.document.kind: unknown document kind {document_kind!r}")
        expect_text(document["value"], f"{path}.input.document.value")
        for index, source in enumerate(expect_list(case_input["lua_sources"], f"{path}.input.lua_sources")):
            source_obj = expect_keys(
                source,
                {"source_name", "source_text"},
                f"{path}.input.lua_sources[{index}]",
            )
            expect_text(source_obj["source_name"], f"{path}.input.lua_sources[{index}].source_name")
            expect_text(source_obj["source_text"], f"{path}.input.lua_sources[{index}].source_text")
        expect_keys(expected, {"lua_source_order", "contract_sha256"}, f"{path}.expected")
        schema_string_list(expected["lua_source_order"], f"{path}.expected.lua_source_order")
        expect_hex(expected["contract_sha256"], 64, f"{path}.expected.contract_sha256")
    elif kind == "canonical_lua_bundle":
        schema_canonical_lua_bundle(case_input, expected, path)
    elif kind == "policy_group_boundary":
        generator = expect_keys(
            case_input,
            {
                "grammar", "count", "first_index", "index_width", "index_radix",
                "group_id_template", "rate_scope_id_template", "request_start_limit",
                "concurrency", "interval_ms",
            },
            f"{path}.input",
        )
        for field in ("grammar", "group_id_template", "rate_scope_id_template"):
            expect_text(generator[field], f"{path}.input.{field}")
        for field in (
            "count", "first_index", "index_width", "index_radix", "request_start_limit",
            "concurrency", "interval_ms",
        ):
            expect_integer(generator[field], f"{path}.input.{field}")
        if generator["grammar"] != "indexed_policy_groups_v1":
            raise FixtureError(f"{path}.input.grammar: unsupported policy-group grammar")
        expect_keys(expected, {"count", "policy_group_map_sha256"}, f"{path}.expected")
        expect_integer(expected["count"], f"{path}.expected.count")
        expect_hex(expected["policy_group_map_sha256"], 64, f"{path}.expected.policy_group_map_sha256")
    elif kind == "guard_chain":
        expect_keys(
            case_input,
            {"guard_mode", "contract_case", "guard_core", "compatibility", "approved_at_ms"},
            f"{path}.input",
        )
        if expect_text(case_input["guard_mode"], f"{path}.input.guard_mode") != "production":
            raise FixtureError(f"{path}.input.guard_mode: guard chains must be production")
        expect_text(case_input["contract_case"], f"{path}.input.contract_case")
        schema_guard_core(case_input["guard_core"], f"{path}.input.guard_core")
        compatibility = expect_keys(
            case_input["compatibility"],
            {
                "redis_config_sha256",
                "spider_image",
                "seed_importer_image",
                "crawl_admin_image",
                "indexer_image",
                "image_indexer_image",
                "backlinks_processor_image",
                "monitoring_image",
                "render_worker_image",
            },
            f"{path}.input.compatibility",
        )
        expect_hex(compatibility["redis_config_sha256"], 64, f"{path}.input.compatibility.redis_config_sha256")
        for field in (
            "spider_image",
            "seed_importer_image",
            "crawl_admin_image",
            "indexer_image",
            "image_indexer_image",
            "backlinks_processor_image",
            "monitoring_image",
            "render_worker_image",
        ):
            expect_text(compatibility[field], f"{path}.input.compatibility.{field}")
        expect_integer(case_input["approved_at_ms"], f"{path}.input.approved_at_ms")
        expect_keys(
            expected,
            {
                "guard_core_sha256",
                "compatibility_manifest_sha256",
                "compatibility_marker_sha256",
                "stored_guard_sha256",
            },
            f"{path}.expected",
        )
        for field in expected:
            expect_hex(expected[field], 64, f"{path}.expected.{field}")
    elif kind == "guard_core":
        expect_keys(case_input, {"guard_mode", "contract_case", "guard_core"}, f"{path}.input")
        mode = expect_text(case_input["guard_mode"], f"{path}.input.guard_mode")
        if mode not in {"production", "provisional_fixture"}:
            raise FixtureError(f"{path}.input.guard_mode: unknown guard mode {mode!r}")
        expect_text(case_input["contract_case"], f"{path}.input.contract_case")
        schema_guard_core(case_input["guard_core"], f"{path}.input.guard_core")
        expect_keys(expected, {"guard_core_sha256", "record_hex"}, f"{path}.expected")
        expect_hex(expected["guard_core_sha256"], 64, f"{path}.expected.guard_core_sha256")
        expect_hex_bytes(expected["record_hex"], f"{path}.expected.record_hex")
    elif kind == "transition_mutation":
        expect_keys(case_input, {"changed_reason", "changed_payload_owner_id"}, f"{path}.input")
        expect_text(case_input["changed_reason"], f"{path}.input.changed_reason")
        expect_hex(case_input["changed_payload_owner_id"], 32, f"{path}.input.changed_payload_owner_id")
        expect_keys(
            expected,
            {
                "base_payload_digest",
                "changed_payload_digest",
                "base_transition_id",
                "changed_reason_transition_id",
                "changed_payload_transition_id",
                "replay_rejection_class",
            },
            f"{path}.expected",
        )
        for field in (
            "base_payload_digest",
            "changed_payload_digest",
            "base_transition_id",
            "changed_reason_transition_id",
            "changed_payload_transition_id",
        ):
            expect_hex(expected[field], 64, f"{path}.expected.{field}")
        expect_text(expected["replay_rejection_class"], f"{path}.expected.replay_rejection_class")
    elif kind == "publication_independence":
        expect_keys(case_input, {"alternate_publication_id"}, f"{path}.input")
        expect_hex(case_input["alternate_publication_id"], 64, f"{path}.input.alternate_publication_id")
        expect_keys(
            expected,
            {"output_digest", "projection_a_sha256", "projection_b_sha256"},
            f"{path}.expected",
        )
        for field in expected:
            expect_hex(expected[field], 64, f"{path}.expected.{field}")
    elif kind == "transcript_binding":
        expect_keys(case_input, {"output_context"}, f"{path}.input")
        schema_output_context(case_input["output_context"], f"{path}.input.output_context")
        expect_keys(
            expected, {"output_digest", "publication_id", "commit_id", "chunk_digest", "terminal_witness"},
            f"{path}.expected",
        )
        for field in ("output_digest", "publication_id", "commit_id", "chunk_digest"):
            expect_hex(expected[field], 64, f"{path}.expected.{field}")
        schema_string_list(expected["terminal_witness"], f"{path}.expected.terminal_witness")
    elif kind == "output_profile":
        profile = expect_text(case_input.get("profile"), f"{path}.input.profile")
        if profile == "maximum":
            expect_keys(case_input, {"profile", "generator"}, f"{path}.input")
            schema_maximum_output_generator(case_input["generator"], f"{path}.input.generator")
        else:
            expect_keys(case_input, {"profile"}, f"{path}.input")
        expect_keys(
            expected,
            {
                "output_digest",
                "publication_id",
                "commit_id",
                "counts",
                "section_sha256",
                "chunk_digests",
            },
            f"{path}.expected",
        )
        for field in ("output_digest", "publication_id", "commit_id"):
            expect_hex(expected[field], 64, f"{path}.expected.{field}")
        schema_counts(expected["counts"], f"{path}.expected.counts")
        schema_digest_map(expected["section_sha256"], OUTPUT_SECTION_LABELS, f"{path}.expected.section_sha256")
        schema_chunk_digest_map(expected["chunk_digests"], f"{path}.expected.chunk_digests")
    elif kind == "source_profile":
        profile = expect_text(case_input.get("profile"), f"{path}.input.profile")
        if profile == "maximum":
            expect_keys(case_input, {"profile", "generator"}, f"{path}.input")
            schema_maximum_source_generator(case_input["generator"], f"{path}.input.generator")
        else:
            expect_keys(case_input, {"profile"}, f"{path}.input")
        expect_keys(expected, {"count", "section_sha256", "source_sha256"}, f"{path}.expected")
        expect_integer(expected["count"], f"{path}.expected.count")
        expect_hex(expected["section_sha256"], 64, f"{path}.expected.section_sha256")
        expect_hex(expected["source_sha256"], 64, f"{path}.expected.source_sha256")
    elif kind == "stage_chunks":
        expect_keys(case_input, {"profile"}, f"{path}.input")
        expect_text(case_input["profile"], f"{path}.input.profile")
        expect_keys(expected, {"chunk_digests"}, f"{path}.expected")
        schema_chunk_digest_map(expected["chunk_digests"], f"{path}.expected.chunk_digests")
    elif kind == "field_limits":
        expect_keys(case_input, {"profile", "generator"}, f"{path}.input")
        if expect_text(case_input["profile"], f"{path}.input.profile") != "maximum":
            raise FixtureError(f"{path}.input.profile: field limits require maximum")
        schema_maximum_output_generator(case_input["generator"], f"{path}.input.generator")
        length_fields = {
            "canonical_url_bytes",
            "html_bytes",
            "original_html_bytes",
            "combined_html_bytes",
            "content_type_bytes",
            "render_policy_rule_bytes",
            "image_alt_bytes",
            "group_id_bytes",
            "outlink_chunk_records",
            "discovery_chunk_records",
            "alias_records",
            "image_chunk_records",
            "image_manifest_bytes",
            "page_record_bytes",
            "final_image_record_bytes",
            "source_record_bytes",
        }
        expect_keys(expected, {"lengths", "record_sha256"}, f"{path}.expected")
        lengths = expect_keys(expected["lengths"], length_fields, f"{path}.expected.lengths")
        for field in lengths:
            expect_integer(lengths[field], f"{path}.expected.lengths.{field}")
        schema_digest_map(
            expected["record_sha256"],
            {"page", "final_image", "source", "image_manifest"},
            f"{path}.expected.record_sha256",
        )
    else:
        raise FixtureError(f"{path}.kind: unknown positive case kind {kind!r}")


NEGATIVE_INPUT_KEYS: dict[str, set[str]] = {
    "transition_reason": {"operation", "value"},
    "source_policy_request_kind": {"value"},
    "discovery_depth": {"value"},
    "output_normalized_target": {"value"},
    "score_text": {"value"},
    "lease_token": {"value"},
    "redis_score": {"score_text", "redis_value"},
    "canonical_url": {"value"},
    "reservation_target": {"value"},
    "output_mutation": {"mutation"},
    "source_shape": {"count"},
    "section_shape": {"section", "count"},
    "stage_chunk_mutation": {"mutation"},
    "fixture_type": {"field", "value"},
    "json_text": {"text"},
    "u64": {"decimal"},
    "output_utf8": {"field", "bytes_hex"},
    "transition_replay": {"mutation"},
    "policy_group_boundary": {
        "grammar", "count", "first_index", "index_width", "index_radix", "group_id_template",
        "rate_scope_id_template", "request_start_limit", "concurrency", "interval_ms",
    },
    "group_id": {"value"},
    "policy_group_binding": {"mutation"},
    "guard_core_mutation": {"base_case", "mutation"},
    "guard_chain_mutation": {"base_case", "mutation"},
    "transcript_binding_mutation": {"base_case", "mutation"},
    "canonical_lua_bundle_mutation": {"base_case", "mutation"},
}


def validate_negative_schema(case: object, path: str) -> None:
    obj = expect_keys(case, {"name", "kind", "input", "expected_rejection_class"}, path)
    case_name = expect_text(obj["name"], f"{path}.name")
    if not case_name or len(case_name.encode("utf-8")) > 160:
        raise FixtureError(f"{path}.name: case name must contain 1 through 160 UTF-8 bytes")
    kind = expect_text(obj["kind"], f"{path}.kind")
    if kind not in NEGATIVE_INPUT_KEYS:
        raise FixtureError(f"{path}.kind: unknown negative case kind {kind!r}")
    case_input = expect_keys(obj["input"], NEGATIVE_INPUT_KEYS[kind], f"{path}.input")
    rejection_class = expect_text(obj["expected_rejection_class"], f"{path}.expected_rejection_class")
    if re.fullmatch(r"[A-Z][A-Z0-9_]*", rejection_class) is None:
        raise FixtureError(f"{path}.expected_rejection_class: invalid rejection class")

    text_fields = {
        "transition_reason": ("operation", "value"),
        "source_policy_request_kind": ("value",),
        "output_normalized_target": ("value",),
        "score_text": ("value",),
        "lease_token": ("value",),
        "redis_score": ("score_text", "redis_value"),
        "canonical_url": ("value",),
        "reservation_target": ("value",),
        "output_mutation": ("mutation",),
        "stage_chunk_mutation": ("mutation",),
        "json_text": ("text",),
        "u64": ("decimal",),
        "output_utf8": ("field", "bytes_hex"),
        "transition_replay": ("mutation",),
        "group_id": ("value",),
        "policy_group_binding": ("mutation",),
        "guard_core_mutation": ("base_case", "mutation"),
        "guard_chain_mutation": ("base_case", "mutation"),
        "transcript_binding_mutation": ("base_case", "mutation"),
        "canonical_lua_bundle_mutation": ("base_case", "mutation"),
    }
    for field in text_fields.get(kind, ()):
        expect_text(case_input[field], f"{path}.input.{field}", utf8=kind != "json_text")
    if kind == "discovery_depth":
        expect_integer(case_input["value"], f"{path}.input.value")
    elif kind == "source_shape":
        expect_integer(case_input["count"], f"{path}.input.count")
    elif kind == "section_shape":
        expect_text(case_input["section"], f"{path}.input.section")
        expect_integer(case_input["count"], f"{path}.input.count")
    elif kind == "fixture_type":
        expect_text(case_input["field"], f"{path}.input.field")
    elif kind == "output_utf8":
        expect_hex_bytes(case_input["bytes_hex"], f"{path}.input.bytes_hex")
    elif kind == "policy_group_boundary":
        for field in ("grammar", "group_id_template", "rate_scope_id_template"):
            expect_text(case_input[field], f"{path}.input.{field}")
        for field in (
            "count", "first_index", "index_width", "index_radix", "request_start_limit",
            "concurrency", "interval_ms",
        ):
            expect_integer(case_input[field], f"{path}.input.{field}")


def validate_fixture_schema(data: object) -> dict[str, object]:
    top = expect_keys(
        data,
        {
            "fixture_version",
            "suite_name",
            "baseline_case_name",
            "identities",
            "targets",
            "policy_decisions",
            "policy_groups",
            "source_jobs",
            "reservation",
            "try_claim",
            "output_context",
            "output",
            "transition_reasons",
            "chunk",
            "expected",
            "cases",
            "negative_vectors",
        },
        "$",
    )
    if expect_integer(top["fixture_version"], "$.fixture_version") != 2:
        raise FixtureError("$.fixture_version: unsupported fixture version")
    if expect_text(top["suite_name"], "$.suite_name") != "crawl-jobs-v2-digest-conformance":
        raise FixtureError("$.suite_name: unexpected suite name")
    expect_text(top["baseline_case_name"], "$.baseline_case_name")

    identities = expect_keys(
        top["identities"],
        {
            "run_id",
            "job_id",
            "owner_id",
            "alternate_owner_id",
            "lease_token",
            "fence",
            "rate_scope_id",
            "crawl_policy_sha256",
        },
        "$.identities",
    )
    for field in ("run_id", "owner_id", "alternate_owner_id", "rate_scope_id"):
        expect_hex(identities[field], 32, f"$.identities.{field}")
    for field in ("job_id", "lease_token", "crawl_policy_sha256"):
        expect_hex(identities[field], 64, f"$.identities.{field}")
    expect_integer(identities["fence"], "$.identities.fence")

    target_names = {"page", "target", "discovered_a", "discovered_z"}
    targets = expect_keys(top["targets"], target_names, "$.targets")
    for name, target in targets.items():
        target_obj = expect_keys(target, {"url_id", "canonical_url"}, f"$.targets.{name}")
        expect_hex(target_obj["url_id"], 64, f"$.targets.{name}.url_id")
        expect_text(target_obj["canonical_url"], f"$.targets.{name}.canonical_url")

    decision_names = {
        "page_document",
        "target_document_depth_2",
        "target_document_depth_3",
        "discovered_a_document",
        "discovered_z_document",
    }
    decisions = expect_keys(top["policy_decisions"], decision_names, "$.policy_decisions")
    for name, decision in decisions.items():
        decision_obj = expect_keys(
            decision,
            {"request_kind", "target", "depth", "group_id", "rate_scope_id", "concurrency", "interval_ms"},
            f"$.policy_decisions.{name}",
        )
        for field in ("request_kind", "target", "group_id", "rate_scope_id"):
            expect_text(decision_obj[field], f"$.policy_decisions.{name}.{field}")
        for field in ("depth", "concurrency", "interval_ms"):
            expect_integer(decision_obj[field], f"$.policy_decisions.{name}.{field}")

    groups = expect_list(top["policy_groups"], "$.policy_groups")
    for index, group in enumerate(groups):
        group_obj = expect_keys(
            group,
            {"group_id", "rate_scope_id", "request_start_limit", "concurrency", "interval_ms"},
            f"$.policy_groups[{index}]",
        )
        for field in ("group_id", "rate_scope_id"):
            expect_text(group_obj[field], f"$.policy_groups[{index}].{field}")
        for field in ("request_start_limit", "concurrency", "interval_ms"):
            expect_integer(group_obj[field], f"$.policy_groups[{index}].{field}")

    def source_item_schema(item: object, path: str) -> None:
        source = expect_keys(
            item,
            {"target", "score_text", "depth", "group_id", "rate_scope_id", "decision"},
            path,
        )
        for field in ("target", "score_text", "group_id", "rate_scope_id", "decision"):
            expect_text(source[field], f"{path}.{field}")
        expect_integer(source["depth"], f"{path}.depth")

    for index, item in enumerate(expect_list(top["source_jobs"], "$.source_jobs")):
        source_item_schema(item, f"$.source_jobs[{index}]")

    reservation = expect_keys(top["reservation"], {"request_ordinal", "target", "decision"}, "$.reservation")
    expect_integer(reservation["request_ordinal"], "$.reservation.request_ordinal")
    expect_text(reservation["target"], "$.reservation.target")
    expect_text(reservation["decision"], "$.reservation.decision")

    claim = expect_keys(
        top["try_claim"],
        {"source_job_index", "expected_prior_fence", "request_ordinal", "target", "decision"},
        "$.try_claim",
    )
    for field in ("source_job_index", "expected_prior_fence", "request_ordinal"):
        expect_integer(claim[field], f"$.try_claim.{field}")
    for field in ("target", "decision"):
        expect_text(claim[field], f"$.try_claim.{field}")

    schema_output_context(top["output_context"], "$.output_context")

    output = expect_keys(top["output"], {"page", "outlinks", "images", "discoveries"}, "$.output")
    page = expect_keys(
        output["page"],
        {
            "normalized_target",
            "html",
            "original_html",
            "content_type",
            "status_code",
            "rendered",
            "render_policy_rule",
            "render_policy_sha256",
        },
        "$.output.page",
    )
    for field in (
        "normalized_target",
        "html",
        "original_html",
        "content_type",
        "render_policy_rule",
        "render_policy_sha256",
    ):
        expect_text(page[field], f"$.output.page.{field}")
    expect_integer(page["status_code"], "$.output.page.status_code")
    expect_boolean(page["rendered"], "$.output.page.rendered")
    schema_string_list(output["outlinks"], "$.output.outlinks")
    for index, image in enumerate(expect_list(output["images"], "$.output.images")):
        image_obj = expect_keys(image, {"normalized_source_url", "alt"}, f"$.output.images[{index}]")
        expect_text(image_obj["normalized_source_url"], f"$.output.images[{index}].normalized_source_url")
        expect_text(image_obj["alt"], f"$.output.images[{index}].alt")
    for index, discovery in enumerate(expect_list(output["discoveries"], "$.output.discoveries")):
        source_item_schema(discovery, f"$.output.discoveries[{index}]")

    reasons = expect_keys(top["transition_reasons"], {"reject_ready", "retry", "dead", "cancel_job", "complete_no_output"}, "$.transition_reasons")
    for field in reasons:
        expect_text(reasons[field], f"$.transition_reasons.{field}")

    chunk = expect_keys(top["chunk"], {"kind", "ordinal", "records"}, "$.chunk")
    expect_text(chunk["kind"], "$.chunk.kind")
    expect_integer(chunk["ordinal"], "$.chunk.ordinal")
    for index, chunk_record in enumerate(expect_list(chunk["records"], "$.chunk.records")):
        record_obj = expect_keys(chunk_record, {"target_url"}, f"$.chunk.records[{index}]")
        expect_text(record_obj["target_url"], f"$.chunk.records[{index}].target_url")

    expected = expect_keys(
        top["expected"],
        {
            "framing",
            "scope_ids",
            "url_ids",
            "target_digests",
            "policy_decision_digests",
            "policy_group_map_sha256",
            "token_digest",
            "reservation_id",
            "source_sha256",
            "output_digest",
            "publication_id",
            "commit_id",
            "chunk_digest",
            "transition_payload_digests",
            "transition_ids",
        },
        "$.expected",
    )
    framing = expect_keys(expected["framing"], {"u64_1_hex", "f_A_hex", "record_hex", "section_hex"}, "$.expected.framing")
    for field in framing:
        expect_hex_bytes(framing[field], f"$.expected.framing.{field}")
    schema_digest_map(expected["scope_ids"], {"global", "group_a", "origin"}, "$.expected.scope_ids")
    schema_digest_map(expected["url_ids"], target_names, "$.expected.url_ids")
    schema_digest_map(expected["target_digests"], target_names, "$.expected.target_digests")
    schema_digest_map(expected["policy_decision_digests"], decision_names, "$.expected.policy_decision_digests")
    for field in (
        "policy_group_map_sha256",
        "token_digest",
        "reservation_id",
        "source_sha256",
        "output_digest",
        "publication_id",
        "commit_id",
        "chunk_digest",
    ):
        expect_hex(expected[field], 64, f"$.expected.{field}")
    schema_digest_map(expected["transition_payload_digests"], TRANSITION_NAMES, "$.expected.transition_payload_digests")
    schema_digest_map(expected["transition_ids"], TRANSITION_NAMES, "$.expected.transition_ids")

    names: set[str] = set()
    baseline_name = expect_text(top["baseline_case_name"], "$.baseline_case_name")
    if not baseline_name:
        raise FixtureError("$.baseline_case_name: empty case name")
    names.add(baseline_name)
    positive_cases = expect_list(top["cases"], "$.cases")
    negative_cases = expect_list(top["negative_vectors"], "$.negative_vectors")
    for index, case in enumerate(positive_cases):
        validate_case_schema(case, f"$.cases[{index}]")
        name = expect_object(case, f"$.cases[{index}]")["name"]
        if name in names:
            raise FixtureError(f"$.cases[{index}].name: duplicate case name {name!r}")
        names.add(name)  # type: ignore[arg-type]
    for index, case in enumerate(negative_cases):
        validate_negative_schema(case, f"$.negative_vectors[{index}]")
        name = expect_object(case, f"$.negative_vectors[{index}]")["name"]
        if name in names:
            raise FixtureError(f"$.negative_vectors[{index}].name: duplicate case name {name!r}")
        names.add(name)  # type: ignore[arg-type]
    if len(positive_cases) != FIXTURE_POSITIVE_CASE_COUNT or len(negative_cases) != FIXTURE_NEGATIVE_CASE_COUNT:
        raise FixtureError(
            "fixture inventory count changed: "
            f"positive={len(positive_cases)}/{FIXTURE_POSITIVE_CASE_COUNT} "
            f"negative={len(negative_cases)}/{FIXTURE_NEGATIVE_CASE_COUNT}"
        )
    inventory = hashlib.sha256()
    inventory.update(f"B\0{baseline_name}\n".encode("utf-8"))
    for case in positive_cases:
        item = expect_object(case, "positive inventory case")
        inventory.update(f"P\0{item['name']}\0{item['kind']}\n".encode("utf-8"))
    for case in negative_cases:
        item = expect_object(case, "negative inventory case")
        inventory.update(
            f"N\0{item['name']}\0{item['kind']}\0{item['expected_rejection_class']}\n".encode("utf-8")
        )
    if inventory.hexdigest() != FIXTURE_INVENTORY_SHA256:
        raise FixtureError(
            f"fixture inventory digest {inventory.hexdigest()} != pinned {FIXTURE_INVENTORY_SHA256}"
        )
    validate_canonical_bundle_fixture_extension(top)
    return top


def model_text(value: object) -> str:
    if type(value) is not str:
        reject("FIXTURE_TYPE")
    try:
        value.encode("utf-8", "strict")
    except UnicodeEncodeError:
        reject("INVALID_UTF8")
    return value


def model_integer(value: object) -> int:
    if type(value) is not int:
        reject("FIXTURE_TYPE")
    return value


def model_boolean(value: object) -> bool:
    if type(value) is not bool:
        reject("FIXTURE_TYPE")
    return value


def require_exact_integer(value: object, *, positive: bool = False) -> int:
    number = model_integer(value)
    minimum = 1 if positive else 0
    if number < minimum or number > MAX_EXACT_INTEGER:
        reject("INVALID_UNSIGNED_DECIMAL")
    return number


def parse_canonical_decimal(value: str, *, maximum: int = MAX_U64) -> int:
    if type(value) is not str or CANONICAL_DECIMAL_RE.fullmatch(value) is None:
        reject("INVALID_UNSIGNED_DECIMAL")
    parsed = int(value, 10)
    if parsed > maximum:
        reject("U64_RANGE")
    return parsed


def u64(value: int) -> bytes:
    if type(value) is not int or value < 0 or value > MAX_U64:
        reject("U64_RANGE")
    return struct.pack(">Q", value)


def frame(value: bytes | str) -> bytes:
    if type(value) is str:
        try:
            raw = value.encode("utf-8", "strict")
        except UnicodeEncodeError:
            reject("INVALID_UTF8")
    elif type(value) is bytes:
        raw = value
    else:
        reject("FIXTURE_TYPE")
    return u64(len(raw)) + raw


def record(fields: list[tuple[str, bytes | str]]) -> bytes:
    if type(fields) is not list:
        reject("FIXTURE_TYPE")
    names: set[str] = set()
    encoded = bytearray(u64(len(fields)))
    for item in fields:
        if type(item) is not tuple or len(item) != 2:
            reject("FIXTURE_TYPE")
        name, value = item
        if type(name) is not str or not name or not name.isascii() or name in names:
            reject("INVALID_RECORD_FIELDS")
        names.add(name)
        encoded.extend(frame(name))
        encoded.extend(frame(value))
    return bytes(encoded)


def section(label: str, records: list[list[tuple[str, bytes | str]]]) -> bytes:
    if type(label) is not str or not label or not label.isascii():
        reject("INVALID_SECTION_LABEL")
    if type(records) is not list:
        reject("FIXTURE_TYPE")
    encoded = bytearray(frame(label))
    encoded.extend(u64(len(records)))
    for item in records:
        encoded.extend(frame(record(item)))
    return bytes(encoded)


def digest_framed(domain: str, *values: str) -> str:
    digest = hashlib.sha256()
    digest.update(frame(domain))
    for value in values:
        digest.update(frame(value))
    return digest.hexdigest()


def digest_sections(domain: str, *encoded_parts: bytes) -> str:
    digest = hashlib.sha256()
    digest.update(frame(domain))
    for encoded in encoded_parts:
        if type(encoded) is not bytes:
            reject("FIXTURE_TYPE")
        digest.update(encoded)
    return digest.hexdigest()


def plain_sha256(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def utf8_bytes(value: object, *, limit: int | None = None, limit_class: str = "FIELD_LIMIT") -> bytes:
    text = model_text(value)
    raw = text.encode("utf-8", "strict")
    if limit is not None and len(raw) > limit:
        reject(limit_class)
    return raw


def validate_wire_utf8(value: bytes) -> str:
    try:
        return value.decode("utf-8", "strict")
    except UnicodeDecodeError:
        reject("INVALID_UTF8")


def _valid_idna_alabel(label: str) -> bool:
    # Compatibility entry point, no host-Python Unicode/IDNA heuristic.
    return _url_helper.valid_alabel(label)


def canonical_url_v1(value: object) -> tuple[str, str | None]:
    """Identity-only fixed point. IP identities have no request origin."""
    try:
        result = _url_helper.validate_identity(model_text(value))
    except _url_helper.URLValidationError as error:
        reject(error.code)
    return result.canonical_url, result.origin


def canonical_origin_v1(value: object) -> str:
    """Request/source/discovery policy layer: canonical identity PLUS no IP."""
    try:
        return _url_helper.derive_origin(model_text(value))
    except _url_helper.URLValidationError as error:
        reject(error.code)


def url_id(canonical_url: str) -> str:
    canonical, _ = canonical_url_v1(canonical_url)
    return hashlib.sha256(b"mifolyo-url:v1\0" + canonical.encode("utf-8")).hexdigest()


def validate_target(target: dict[str, object]) -> None:
    if type(target) is not dict or set(target) != {"url_id", "canonical_url"}:
        reject("FIXTURE_SCHEMA")
    supplied_id = model_text(target["url_id"])
    if HEX_64_RE.fullmatch(supplied_id) is None:
        reject("INVALID_URL_ID")
    canonical = model_text(target["canonical_url"])
    if url_id(canonical) != supplied_id:
        reject("URL_IDENTITY_MISMATCH")


def validate_score_text(value: object) -> float:
    score = model_text(value)
    if SCORE_RE.fullmatch(score) is None:
        reject("INVALID_SCORE_SYNTAX")
    try:
        parsed = float(score)
    except ValueError:
        reject("INVALID_SCORE_SYNTAX")
    if not math.isfinite(parsed):
        reject("SCORE_NOT_FINITE")
    if parsed < -1000 or parsed > 10000:
        reject("SCORE_OUT_OF_RANGE")
    return parsed


def parse_redis_binary64(value: object) -> float:
    text = model_text(value)
    if REDIS_FLOAT_RE.fullmatch(text) is None:
        reject("INVALID_REDIS_SCORE")
    try:
        parsed = float(text)
    except ValueError:
        reject("INVALID_REDIS_SCORE")
    if not math.isfinite(parsed):
        reject("REDIS_SCORE_NOT_FINITE")
    return parsed


def validate_redis_score(score_text: object, redis_value: object) -> float:
    canonical = validate_score_text(score_text)
    redis_score = parse_redis_binary64(redis_value)
    if struct.pack(">d", canonical) != struct.pack(">d", redis_score):
        reject("SCORE_BINARY64_MISMATCH")
    return canonical


def validate_group_id(value: object) -> str:
    group_id = model_text(value)
    raw = group_id.encode("utf-8")
    if not raw or len(raw) > MAX_GROUP_ID_BYTES:
        reject("GROUP_ID_LIMIT")
    if any(unicodedata.category(character) == "Cc" for character in group_id):
        reject("INVALID_GROUP_ID")
    return group_id


def validate_rate_scope_id(value: object) -> str:
    rate_scope_id = model_text(value)
    if HEX_32_RE.fullmatch(rate_scope_id) is None:
        reject("INVALID_RATE_SCOPE_ID")
    return rate_scope_id


def group_scope(rate_scope_id: str) -> str:
    validate_rate_scope_id(rate_scope_id)
    return digest_framed("mifolyo:rate:group:v2", rate_scope_id)


def origin_scope(canonical_url: str) -> str:
    origin = canonical_origin_v1(canonical_url)
    return digest_framed("mifolyo:rate:origin:v2", origin)


def target_digest(target: dict[str, object]) -> str:
    validate_target(target)
    return digest_framed(
        "mifolyo:request-target:v2",
        model_text(target["url_id"]),
        model_text(target["canonical_url"]),
    )


def decision_fields(vector: dict[str, object], targets: dict[str, dict[str, object]]) -> list[tuple[str, str]]:
    request_kind = model_text(vector["request_kind"])
    if request_kind not in {"robots", "document", "redirect", "render_resource"}:
        reject("INVALID_REQUEST_KIND")
    target_name = model_text(vector["target"])
    if target_name not in targets:
        reject("UNKNOWN_TARGET")
    target = targets[target_name]
    depth = require_exact_integer(vector["depth"])
    group_id = validate_group_id(vector["group_id"])
    rate_scope_id = validate_rate_scope_id(vector["rate_scope_id"])
    concurrency = model_integer(vector["concurrency"])
    interval_ms = model_integer(vector["interval_ms"])
    if not 1 <= concurrency <= 32 or not 0 <= interval_ms <= 3_600_000:
        reject("DECISION_NUMBER_RANGE")
    return [
        ("request_kind", request_kind),
        ("target_url_id", model_text(target["url_id"])),
        ("target_digest", target_digest(target)),
        ("depth", str(depth)),
        ("group_id", group_id),
        ("rate_scope_id", rate_scope_id),
        ("global_scope_id", digest_framed("mifolyo:rate:global:v2")),
        ("group_scope_id", group_scope(rate_scope_id)),
        ("origin_scope_id", origin_scope(model_text(target["canonical_url"]))),
        ("global_concurrency", "2"),
        ("global_interval_ms", "0"),
        ("group_concurrency", str(concurrency)),
        ("group_interval_ms", str(interval_ms)),
        ("origin_concurrency", str(concurrency)),
        ("origin_interval_ms", str(interval_ms)),
    ]


def decision_digest(vector: dict[str, object], targets: dict[str, dict[str, object]]) -> str:
    return digest_sections(
        "mifolyo:policy-decision:v2",
        section("decision", [decision_fields(vector, targets)]),
    )


def validate_document_binding(
    item: dict[str, object],
    decision: dict[str, object],
    targets: dict[str, dict[str, object]],
) -> None:
    if decision["request_kind"] != "document" or item["target"] != decision["target"]:
        reject("POLICY_BINDING_MISMATCH")
    for name in ("depth", "group_id", "rate_scope_id"):
        if item[name] != decision[name]:
            reject("POLICY_BINDING_MISMATCH")
    target_name = model_text(item["target"])
    if target_name not in targets:
        reject("UNKNOWN_TARGET")
    validate_target(targets[target_name])
    # Matches Go validateDocumentPolicyBinding's explicit origin derivation;
    # validating/hashing an identity alone does not admit a source/discovery.
    canonical_origin_v1(targets[target_name]["canonical_url"])


def source_job_fields(
    item: dict[str, object],
    data: dict[str, object],
    complete: bool = False,
) -> list[tuple[str, str]]:
    targets = data["targets"]
    decisions = data["policy_decisions"]
    if type(targets) is not dict or type(decisions) is not dict:
        reject("FIXTURE_TYPE")
    target_name = model_text(item["target"])
    decision_name = model_text(item["decision"])
    if target_name not in targets or decision_name not in decisions:
        reject("UNKNOWN_REFERENCE")
    target = targets[target_name]
    decision = decisions[decision_name]
    if type(target) is not dict or type(decision) is not dict:
        reject("FIXTURE_TYPE")
    validate_document_binding(item, decision, targets)  # type: ignore[arg-type]
    groups = data.get("policy_groups")
    if groups is None:
        reject("FIXTURE_SCHEMA")
    validate_decision_policy_group_binding(decision, targets, groups)  # type: ignore[arg-type]
    score = model_text(item["score_text"])
    validate_score_text(score)
    depth = require_exact_integer(item["depth"])
    group_id = validate_group_id(item["group_id"])
    rate_scope_id = validate_rate_scope_id(item["rate_scope_id"])
    fields: list[tuple[str, str]] = [
        ("job_id", model_text(target["url_id"])),
        ("canonical_url", model_text(target["canonical_url"])),
        ("score_text", score),
        ("depth", str(depth)),
        ("group_id", group_id),
        ("rate_scope_id", rate_scope_id),
    ]
    if complete:
        fields.extend(
            [
                ("group_scope_id", group_scope(rate_scope_id)),
                ("initial_origin_scope_id", origin_scope(model_text(target["canonical_url"]))),
            ]
        )
    fields.append(("policy_decision_sha256", decision_digest(decision, targets)))  # type: ignore[arg-type]
    return fields


def validate_identity_values(identities: dict[str, object]) -> None:
    for name in ("run_id", "owner_id", "alternate_owner_id", "rate_scope_id"):
        if HEX_32_RE.fullmatch(model_text(identities[name])) is None:
            reject("INVALID_IDENTITY")
    for name in ("job_id", "lease_token", "crawl_policy_sha256"):
        if HEX_64_RE.fullmatch(model_text(identities[name])) is None:
            reject("INVALID_LEASE_TOKEN" if name == "lease_token" else "INVALID_IDENTITY")
    require_exact_integer(identities["fence"], positive=True)


def reservation_id(data: dict[str, object], reservation: dict[str, object]) -> str:
    identities = data["identities"]
    targets = data["targets"]
    decisions = data["policy_decisions"]
    if type(identities) is not dict or type(targets) is not dict or type(decisions) is not dict:
        reject("FIXTURE_TYPE")
    target_name = model_text(reservation["target"])
    decision_name = model_text(reservation["decision"])
    if target_name not in targets or decision_name not in decisions:
        reject("UNKNOWN_REFERENCE")
    decision = decisions[decision_name]
    if type(decision) is not dict:
        reject("FIXTURE_TYPE")
    if decision["target"] != target_name:
        reject("RESERVATION_TARGET_MISMATCH")
    target = targets[target_name]
    if type(target) is not dict:
        reject("FIXTURE_TYPE")
    validate_decision_policy_group_binding(decision, targets, data.get("policy_groups"))  # type: ignore[arg-type]
    fields = dict(decision_fields(decision, targets))  # type: ignore[arg-type]
    ordinal = require_exact_integer(reservation["request_ordinal"], positive=True)
    return digest_framed(
        "mifolyo:request-reservation:v2",
        model_text(identities["run_id"]),
        model_text(identities["job_id"]),
        str(require_exact_integer(identities["fence"], positive=True)),
        model_text(identities["lease_token"]),
        str(ordinal),
        fields["request_kind"],
        model_text(target["url_id"]),
        fields["target_digest"],
        model_text(identities["crawl_policy_sha256"]),
        decision_digest(decision, targets),  # type: ignore[arg-type]
        fields["group_id"],
        fields["rate_scope_id"],
        fields["global_scope_id"],
        fields["group_scope_id"],
        fields["origin_scope_id"],
        fields["global_concurrency"],
        "0",
        fields["group_concurrency"],
        fields["group_interval_ms"],
        fields["origin_concurrency"],
        fields["origin_interval_ms"],
    )


def transition_payload_digest(fields: list[tuple[str, str]]) -> str:
    return digest_sections(
        "mifolyo:transition-payload:v2",
        section("arguments", [fields]),
    )


def transition_id(
    data: dict[str, object],
    operation: str,
    reason: str,
    fields: list[tuple[str, str]],
    leased: bool,
) -> str:
    identities = data["identities"]
    if type(identities) is not dict:
        reject("FIXTURE_TYPE")
    payload = transition_payload_digest(fields)
    return digest_framed(
        "mifolyo:crawl-transition:v2",
        operation,
        model_text(identities["run_id"]),
        model_text(identities["job_id"]),
        str(require_exact_integer(identities["fence"], positive=True)) if leased else "0",
        model_text(identities["lease_token"]) if leased else "",
        reason,
        payload,
    )


def validate_transition_reason(operation: str, reason: str) -> None:
    allowed: set[str]
    if operation == "CJ2_REJECT_READY":
        allowed = REJECT_READY_REASONS
    elif operation in {"CJ2_TRY_CLAIM", "CJ2_RELEASE_BEFORE_IO", "CJ2_ABORT_STAGE"}:
        allowed = {"none"}
    elif operation == "CJ2_RETRY":
        allowed = RETRYABLE_REASONS - {"lease_expired_after_io"}
    elif operation == "CJ2_DEAD":
        allowed = DEAD_REASONS - {"policy_scope_changed", "retry_exhausted", "pre_io_recovery_exhausted"}
    elif operation == "CJ2_CANCEL_JOB":
        allowed = CANCELLATION_REASONS
    elif operation == "CJ2_COMPLETE_NO_OUTPUT":
        allowed = {"already_visited"}
    else:
        reject("UNKNOWN_TRANSITION_OPERATION")
    if reason not in allowed:
        reject("INVALID_TRANSITION_REASON")


def verify_replay(stored_id: str, submitted_id: str) -> None:
    if stored_id != submitted_id:
        reject("IMMUTABLE_MISMATCH")


def format_last_crawled(milliseconds: object) -> str:
    value = require_exact_integer(milliseconds)
    try:
        instant = dt.datetime.fromtimestamp(value // 1000, tz=dt.timezone.utc)
    except (OverflowError, OSError, ValueError):
        reject("INVALID_TIMESTAMP")
    weekdays = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
    months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
    return (
        f"{weekdays[instant.weekday()]}, {instant.day:02d} {months[instant.month - 1]} "
        f"{instant.year:04d} {instant.hour:02d}:{instant.minute:02d}:{instant.second:02d} UTC"
    )


def validate_content_type(value: object) -> str:
    content_type = model_text(value)
    raw = content_type.encode("utf-8")
    if len(raw) > MAX_CONTENT_TYPE_BYTES:
        reject("CONTENT_TYPE_LIMIT")
    if not raw or content_type != content_type.strip() or CONTENT_TYPE_RE.fullmatch(content_type) is None:
        reject("INVALID_CONTENT_TYPE")
    return content_type


def validate_render_rule(value: object) -> str:
    rule = model_text(value)
    raw = rule.encode("utf-8")
    if len(raw) > MAX_RENDER_RULE_BYTES:
        reject("RENDER_RULE_LIMIT")
    if not raw or any(unicodedata.category(character) == "Cc" for character in rule):
        reject("INVALID_RENDER_RELATION")
    return rule


def transcript_decimal(value: object, maximum: int, *, positive: bool = False) -> int:
    if type(value) is not str or CANONICAL_DECIMAL_RE.fullmatch(value) is None:
        reject("TRANSCRIPT_BINDING_MISMATCH")
    number = int(value)
    if number > maximum or (positive and number == 0):
        reject("TRANSCRIPT_BINDING_MISMATCH")
    return number


def request_start_interval(context: dict[str, object]) -> tuple[int, int]:
    baseline = transcript_decimal(context["lease_request_starts_baseline"], MAX_REQUEST_STARTS)
    generation = transcript_decimal(context["terminal_request_starts_generation"], MAX_REQUEST_STARTS, positive=True)
    if baseline >= generation:
        reject("TRANSCRIPT_BINDING_MISMATCH")
    return baseline, generation


def output_transcript(
    data: dict[str, object], witness: list[str] | None = None,
) -> tuple[list[dict[str, object]], list[str]]:
    context = expect_object(data["output_context"], "output_context")
    schema_output_context(context, "output_context")
    baseline, generation = request_start_interval(context)
    sources = expect_list(data["source_jobs"], "source_jobs")
    source_index = model_integer(context["source_job_index"])
    if not 0 <= source_index < len(sources):
        reject("OUTPUT_CONTEXT_MISMATCH")
    source = expect_object(sources[source_index], "source")
    source_job_fields(source, data)
    identities = expect_object(data["identities"], "identities")
    validate_identity_values(identities)
    targets = expect_object(data["targets"], "targets")
    if identities["job_id"] != targets[source["target"]]["url_id"]:
        reject("OUTPUT_CONTEXT_MISMATCH")
    requests = expect_list(context["requests"], "requests")
    if not requests or len(requests) != generation - baseline:
        reject("TRANSCRIPT_BINDING_MISMATCH")
    documents: list[dict[str, object]] = []
    previous_count, previous_ordinal, previous_time = baseline, 0, 0
    has_document = False
    initial_robots = 0
    for request in requests:
        item = expect_object(request, "request")
        count = transcript_decimal(item["job_request_starts"], MAX_REQUEST_STARTS, positive=True)
        ordinal = transcript_decimal(item["request_ordinal"], MAX_REQUEST_ORDINAL, positive=True)
        started = require_exact_integer(item["started_at_ms"], positive=True)
        if count != previous_count + 1 or ordinal <= previous_ordinal or ordinal < count or started < previous_time:
            reject("TRANSCRIPT_BINDING_MISMATCH")
        target_name = model_text(item["target"])
        if target_name not in targets:
            reject("OUTPUT_CONTEXT_MISMATCH")
        validate_target(expect_object(targets[target_name], "request target"))
        # Every actual request kind (including robots/render resources) has a
        # policy origin. Keep IP rejection here, not in general URL identity.
        canonical_origin_v1(targets[target_name]["canonical_url"])
        kind = model_text(item["request_kind"])
        if kind == "document":
            if has_document or target_name != source["target"]:
                reject("OUTPUT_CONTEXT_MISMATCH")
            has_document = True
            documents.append(item)
        elif kind == "robots":
            if not has_document:
                initial_robots += 1
                if initial_robots > 1:
                    reject("OUTPUT_CONTEXT_MISMATCH")
        elif kind in {"redirect", "render_resource"}:
            if not has_document:
                reject("OUTPUT_CONTEXT_MISMATCH")
            if kind == "redirect":
                documents.append(item)
        else:
            reject("OUTPUT_CONTEXT_MISMATCH")
        previous_count, previous_ordinal, previous_time = count, ordinal, started
    if not documents or previous_count != generation:
        reject("TRANSCRIPT_BINDING_MISMATCH")
    final = documents[-1]
    target = expect_object(targets[final["target"]], "final target")
    expected_witness = [
        str(final["started_at_ms"]), str(identities["fence"]), model_text(target["url_id"]),
        model_text(target["canonical_url"]), target_digest(target), str(generation), str(previous_time),
        str(baseline), "leased", model_text(identities["owner_id"]), model_text(identities["lease_token"]),
        str(identities["fence"]), "",
    ]
    if witness is not None:
        if type(witness) is not list or len(witness) != 13:
            reject("RESPONSE_ARITY")
        if any(type(value) is not str for value in witness) or witness != expected_witness:
            reject("TRANSCRIPT_BINDING_MISMATCH")
    return documents, expected_witness


def output_records(
    data: dict[str, object],
    *,
    authorized_render_policy_sha256: str | None = None,
    enabled_render_rules: set[str] | None = None,
) -> tuple[list[list[tuple[str, bytes | str]]], ...]:
    output = data["output"]
    context = data["output_context"]
    sources = data["source_jobs"]
    targets = data["targets"]
    decisions = data["policy_decisions"]
    if not all(type(value) is dict for value in (output, context, targets, decisions)) or type(sources) is not list:
        reject("FIXTURE_TYPE")
    source_index = model_integer(context["source_job_index"])  # type: ignore[index]
    if source_index < 0 or source_index >= len(sources):
        reject("OUTPUT_CONTEXT_MISMATCH")
    source = sources[source_index]
    if type(source) is not dict:
        reject("FIXTURE_TYPE")
    source_job_fields(source, data)
    requests, _witness = output_transcript(data)
    request_targets = [model_text(request["target"]) for request in requests]

    final_target_name = request_targets[-1]
    page = output["page"]  # type: ignore[index]
    if type(page) is not dict:
        reject("FIXTURE_TYPE")
    if model_text(page["normalized_target"]) != final_target_name:
        reject("OUTPUT_CONTEXT_MISMATCH")
    final_target = targets[final_target_name]  # type: ignore[index]
    if type(final_target) is not dict:
        reject("FIXTURE_TYPE")
    normalized_url = model_text(final_target["canonical_url"])
    canonical_url_v1(normalized_url)
    html = model_text(page["html"])
    original_html = model_text(page["original_html"])
    html_bytes = utf8_bytes(html)
    original_bytes = utf8_bytes(original_html)
    if len(html_bytes) > MAX_PAGE_BLOB_BYTES or len(original_bytes) > MAX_PAGE_BLOB_BYTES:
        reject("PAGE_BLOB_LIMIT")
    if len(html_bytes) + len(original_bytes) > MAX_COMBINED_HTML_BYTES:
        reject("COMBINED_HTML_LIMIT")
    content_type = validate_content_type(page["content_type"])
    status_code = model_integer(page["status_code"])
    if status_code < 100 or status_code > 399:
        reject("STATUS_CODE_RANGE")
    rendered = model_boolean(page["rendered"])
    render_rule = model_text(page["render_policy_rule"])
    render_digest = model_text(page["render_policy_sha256"])
    if rendered:
        render_rule = validate_render_rule(render_rule)
        if HEX_64_RE.fullmatch(render_digest) is None:
            reject("INVALID_RENDER_POLICY_DIGEST")
        if not original_bytes:
            reject("INVALID_RENDER_RELATION")
        if authorized_render_policy_sha256 is None or render_digest != authorized_render_policy_sha256:
            reject("RENDER_POLICY_MISMATCH")
        if enabled_render_rules is None or render_rule not in enabled_render_rules:
            reject("INVALID_RENDER_RELATION")
    elif original_bytes or render_rule or render_digest:
        reject("INVALID_RENDER_RELATION")

    page_record: list[tuple[str, bytes | str]] = [
        ("normalized_url", normalized_url),
        ("html", html),
        ("original_html", original_html),
        ("content_type", content_type),
        ("status_code", f"{status_code:03d}"),
        ("last_crawled", format_last_crawled(requests[-1]["started_at_ms"])),
        ("rendered", "true" if rendered else "false"),
        ("render_policy_rule", render_rule),
        ("render_policy_sha256", render_digest),
    ]

    raw_outlinks = output["outlinks"]  # type: ignore[index]
    if type(raw_outlinks) is not list:
        reject("FIXTURE_TYPE")
    if len(raw_outlinks) > MAX_OUTLINKS:
        reject("OUTPUT_COUNT_LIMIT")
    ordered_outlinks = sorted((model_text(item) for item in raw_outlinks), key=lambda item: item.encode("utf-8"))
    outlinks: list[list[tuple[str, bytes | str]]] = []
    previous = None
    for value in ordered_outlinks:
        canonical_url_v1(value)
        if value == normalized_url:
            reject("INVALID_OUTLINK")
        if value == previous:
            reject("DUPLICATE_OUTLINK")
        previous = value
        outlinks.append([("target_url", value)])

    raw_images = output["images"]  # type: ignore[index]
    if type(raw_images) is not list:
        reject("FIXTURE_TYPE")
    if len(raw_images) > MAX_IMAGES:
        reject("OUTPUT_COUNT_LIMIT")
    ordered_images = sorted(
        raw_images,
        key=lambda item: model_text(item["normalized_source_url"]).encode("utf-8") if type(item) is dict else b"",
    )
    images: list[list[tuple[str, bytes | str]]] = []
    previous = None
    for image in ordered_images:
        if type(image) is not dict:
            reject("FIXTURE_TYPE")
        source_url = model_text(image["normalized_source_url"])
        canonical_url_v1(source_url)
        alt = model_text(image["alt"])
        if len(alt.encode("utf-8")) > MAX_IMAGE_ALT_BYTES:
            reject("IMAGE_ALT_LIMIT")
        if source_url == previous:
            reject("DUPLICATE_IMAGE")
        previous = source_url
        images.append([("normalized_source_url", source_url), ("alt", alt)])

    raw_discoveries = output["discoveries"]  # type: ignore[index]
    if type(raw_discoveries) is not list:
        reject("FIXTURE_TYPE")
    if len(raw_discoveries) > MAX_DISCOVERIES:
        reject("OUTPUT_COUNT_LIMIT")
    discovery_items: list[tuple[str, dict[str, object]]] = []
    for item in raw_discoveries:
        if type(item) is not dict:
            reject("FIXTURE_TYPE")
        target_name = model_text(item["target"])
        if target_name not in targets:  # type: ignore[operator]
            reject("UNKNOWN_TARGET")
        target = targets[target_name]  # type: ignore[index]
        if type(target) is not dict:
            reject("FIXTURE_TYPE")
        discovery_items.append((model_text(target["url_id"]), item))
    discovery_items.sort(key=lambda item: item[0].encode("ascii"))
    discoveries: list[list[tuple[str, bytes | str]]] = []
    previous = None
    for job_id_value, item in discovery_items:
        if job_id_value == previous:
            reject("DUPLICATE_DISCOVERY")
        previous = job_id_value
        decision_name = model_text(item["decision"])
        if decision_name not in decisions:  # type: ignore[operator]
            reject("UNKNOWN_REFERENCE")
        decision = decisions[decision_name]  # type: ignore[index]
        if type(decision) is not dict:
            reject("FIXTURE_TYPE")
        validate_document_binding(item, decision, targets)  # type: ignore[arg-type]
        validate_decision_policy_group_binding(decision, targets, data.get("policy_groups"))  # type: ignore[arg-type]
        score = model_text(item["score_text"])
        validate_score_text(score)
        depth = require_exact_integer(item["depth"])
        group_id = validate_group_id(item["group_id"])
        rate_scope_id = validate_rate_scope_id(item["rate_scope_id"])
        target = targets[model_text(item["target"])]  # type: ignore[index]
        discoveries.append(
            [
                ("job_id", job_id_value),
                ("canonical_url", model_text(target["canonical_url"])),  # type: ignore[index]
                ("depth", str(depth)),
                ("score_text", score),
                ("group_id", group_id),
                ("rate_scope_id", rate_scope_id),
                ("policy_decision_sha256", decision_digest(decision, targets)),  # type: ignore[arg-type]
            ]
        )

    alias_names = {model_text(source["target"]), *request_targets}
    alias_records: list[list[tuple[str, bytes | str]]] = []
    depth = require_exact_integer(source["depth"])
    for target_name in sorted(alias_names, key=lambda name: model_text(targets[name]["url_id"]).encode("ascii")):  # type: ignore[index]
        target = targets[target_name]  # type: ignore[index]
        if type(target) is not dict:
            reject("FIXTURE_TYPE")
        validate_target(target)
        alias_records.append(
            [
                ("url_id", model_text(target["url_id"])),
                ("canonical_url", model_text(target["canonical_url"])),
                ("depth", str(depth)),
            ]
        )
    if not 1 <= len(alias_records) <= MAX_ALIASES:
        reject("ALIAS_COUNT_LIMIT")
    return [page_record], outlinks, images, discoveries, alias_records


def output_digest_from_records(records: tuple[list[list[tuple[str, bytes | str]]], ...]) -> str:
    page, outlinks, images, discoveries, aliases = records
    return digest_sections(
        "mifolyo:crawl-output:v2",
        section("page", page),
        section("outlinks", outlinks),
        section("images", images),
        section("discoveries", discoveries),
        section("aliases", aliases),
    )


def validate_policy_groups(groups_value: object) -> list[list[tuple[str, str]]]:
    groups = groups_value
    if type(groups) is not list:
        reject("FIXTURE_TYPE")
    if not 1 <= len(groups) <= MAX_POLICY_GROUPS:
        reject("POLICY_GROUP_COUNT_LIMIT")
    ordered = sorted(groups, key=lambda item: model_text(item["group_id"]).encode("utf-8") if type(item) is dict else b"")
    records: list[list[tuple[str, str]]] = []
    seen: set[str] = set()
    for item in ordered:
        if type(item) is not dict:
            reject("FIXTURE_TYPE")
        group_id = validate_group_id(item["group_id"])
        if group_id in seen:
            reject("DUPLICATE_POLICY_GROUP")
        seen.add(group_id)
        rate_scope_id = validate_rate_scope_id(item["rate_scope_id"])
        request_limit = model_integer(item["request_start_limit"])
        concurrency = model_integer(item["concurrency"])
        interval = model_integer(item["interval_ms"])
        if not 1 <= request_limit <= 10 or not 1 <= concurrency <= 32 or not 0 <= interval <= 3_600_000:
            reject("POLICY_GROUP_NUMBER_RANGE")
        records.append(
            [
                ("group_id", group_id),
                ("rate_scope_id", rate_scope_id),
                ("group_scope_id", group_scope(rate_scope_id)),
                ("request_start_limit", str(request_limit)),
                ("concurrency", str(concurrency)),
                ("interval_ms", str(interval)),
            ]
        )
    return records


def policy_group_index(groups_value: object) -> dict[str, dict[str, object]]:
    validate_policy_groups(groups_value)
    groups = expect_list(groups_value, "policy groups")
    indexed: dict[str, dict[str, object]] = {}
    for item in groups:
        if type(item) is not dict:
            reject("FIXTURE_TYPE")
        group_id = validate_group_id(item["group_id"])
        indexed[group_id] = item
    return indexed


def generated_policy_groups_boundary(generator: dict[str, object]) -> list[dict[str, object]]:
    if model_text(generator["grammar"]) != "indexed_policy_groups_v1":
        raise FixtureError("unsupported indexed policy-group grammar")
    count = model_integer(generator["count"])
    first = model_integer(generator["first_index"])
    width = model_integer(generator["index_width"])
    radix = model_integer(generator["index_radix"])
    group_template = model_text(generator["group_id_template"])
    rate_template = model_text(generator["rate_scope_id_template"])
    if (
        count < 0 or count > MAX_POLICY_GROUPS + 1 or first < 0 or not 1 <= width <= 16
        or radix not in {10, 16} or group_template.count("{index}") != 1
        or rate_template.count("{index}") != 1
    ):
        raise FixtureError("invalid indexed policy-group generator")
    groups: list[dict[str, object]] = []
    for offset in range(count):
        raw_index = format(first + offset, "d" if radix == 10 else "x")
        if len(raw_index) > width:
            raise FixtureError("policy-group index exceeds its declared width")
        index = raw_index.rjust(width, "0")
        group_id = group_template.replace("{index}", index, 1)
        rate_scope_id = rate_template.replace("{index}", index, 1)
        validate_group_id(group_id)
        validate_rate_scope_id(rate_scope_id)
        groups.append(
            {
                "group_id": group_id,
                "rate_scope_id": rate_scope_id,
                "request_start_limit": model_integer(generator["request_start_limit"]),
                "concurrency": model_integer(generator["concurrency"]),
                "interval_ms": model_integer(generator["interval_ms"]),
            }
        )
    return groups


def validate_decision_policy_group_binding(
    decision: dict[str, object],
    targets: dict[str, dict[str, object]],
    groups_value: object,
    *,
    origin_concurrency: int | None = None,
    origin_interval_ms: int | None = None,
) -> None:
    fields = dict(decision_fields(decision, targets))
    groups = policy_group_index(groups_value)
    group = groups.get(fields["group_id"])
    if group is None:
        reject("POLICY_GROUP_BINDING_MISMATCH")
    group_rate_scope = validate_rate_scope_id(group["rate_scope_id"])
    group_concurrency = model_integer(group["concurrency"])
    group_interval = model_integer(group["interval_ms"])
    decision_origin_concurrency = (
        int(fields["origin_concurrency"])
        if origin_concurrency is None
        else model_integer(origin_concurrency)
    )
    decision_origin_interval = (
        int(fields["origin_interval_ms"])
        if origin_interval_ms is None
        else model_integer(origin_interval_ms)
    )
    if (
        fields["rate_scope_id"] != group_rate_scope
        or fields["group_scope_id"] != group_scope(group_rate_scope)
        or int(fields["group_concurrency"]) != group_concurrency
        or int(fields["group_interval_ms"]) != group_interval
        or decision_origin_concurrency != int(fields["group_concurrency"])
        or decision_origin_interval != int(fields["group_interval_ms"])
        or decision_origin_concurrency != group_concurrency
        or decision_origin_interval != group_interval
    ):
        reject("POLICY_GROUP_BINDING_MISMATCH")


def validate_run_policy_bindings(data: dict[str, object]) -> None:
    targets = data.get("targets")
    decisions = data.get("policy_decisions")
    groups = data.get("policy_groups")
    if type(targets) is not dict or type(decisions) is not dict or groups is None:
        reject("FIXTURE_TYPE")
    policy_group_index(groups)
    for decision in decisions.values():
        if type(decision) is not dict:
            reject("FIXTURE_TYPE")
        validate_decision_policy_group_binding(decision, targets, groups)  # type: ignore[arg-type]
    sources = data.get("source_jobs", [])
    if type(sources) is not list:
        reject("FIXTURE_TYPE")
    for source in sources:
        if type(source) is not dict:
            reject("FIXTURE_TYPE")
        source_job_fields(source, data)
    output = data.get("output")
    if output is not None:
        if type(output) is not dict or type(output.get("discoveries")) is not list:
            reject("FIXTURE_TYPE")
        for discovery in output["discoveries"]:
            if type(discovery) is not dict:
                reject("FIXTURE_TYPE")
            source_job_fields(discovery, data)


def validate_stage_chunk(
    commit_id: str,
    kind: str,
    ordinal: int,
    records: list[list[tuple[str, bytes | str]]],
) -> None:
    if HEX_64_RE.fullmatch(commit_id) is None:
        reject("INVALID_COMMIT_ID")
    if kind not in CHUNK_KINDS:
        reject("UNKNOWN_CHUNK_KIND")
    require_exact_integer(ordinal)

    def field_names(item: list[tuple[str, bytes | str]]) -> tuple[str, ...]:
        return tuple(name for name, _ in item)

    if kind == "page_fields":
        if ordinal != 0 or len(records) != 1 or field_names(records[0]) != (
            "normalized_url",
            "content_type",
            "status_code",
            "last_crawled",
            "rendered",
            "render_policy_rule",
            "render_policy_sha256",
            "publication_id",
        ):
            reject("INVALID_CHUNK_SHAPE")
        values = [model_text(value) for _, value in records[0]]
        canonical_url_v1(values[0])
        validate_content_type(values[1])
        if re.fullmatch(r"[0-9]{3}", values[2]) is None or not 100 <= int(values[2]) <= 399:
            reject("STATUS_CODE_RANGE")
        timestamp_match = re.fullmatch(
            r"(Mon|Tue|Wed|Thu|Fri|Sat|Sun), ([0-9]{2}) "
            r"(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) "
            r"([0-9]{4}) ([0-9]{2}):([0-9]{2}):([0-9]{2}) UTC",
            values[3],
        )
        if timestamp_match is None:
            reject("INVALID_TIMESTAMP")
        weekday, day, month, year, hour, minute, second = timestamp_match.groups()
        months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
        weekdays = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
        try:
            parsed = dt.datetime(
                int(year), months.index(month) + 1, int(day), int(hour), int(minute), int(second),
                tzinfo=dt.timezone.utc,
            )
        except ValueError:
            reject("INVALID_TIMESTAMP")
        if weekdays[parsed.weekday()] != weekday:
            reject("INVALID_TIMESTAMP")
        if values[4] not in {"true", "false"}:
            reject("INVALID_RENDER_RELATION")
        if values[4] == "false" and (values[5] or values[6]):
            reject("INVALID_RENDER_RELATION")
        if values[4] == "true":
            validate_render_rule(values[5])
            if HEX_64_RE.fullmatch(values[6]) is None:
                reject("INVALID_RENDER_POLICY_DIGEST")
        if HEX_64_RE.fullmatch(values[7]) is None:
            reject("INVALID_PUBLICATION_ID")
    elif kind in {"html", "original_html"}:
        if ordinal != 0 or len(records) != 1 or field_names(records[0]) != ("field_name", "field_bytes"):
            reject("INVALID_CHUNK_SHAPE")
        field_name = model_text(records[0][0][1])
        if field_name != kind:
            reject("INVALID_CHUNK_SHAPE")
        value = records[0][1][1]
        if type(value) is bytes:
            validate_wire_utf8(value)
            raw = value
        else:
            raw = utf8_bytes(value)
        if len(raw) > MAX_PAGE_BLOB_BYTES:
            reject("PAGE_BLOB_LIMIT")
    elif kind == "outlinks":
        if ordinal >= 4 or not 1 <= len(records) <= MAX_NON_BLOB_CHUNK_RECORDS:
            reject("CHUNK_RECORD_COUNT_LIMIT")
        previous = None
        for item in records:
            if field_names(item) != ("target_url",):
                reject("INVALID_CHUNK_SHAPE")
            value = model_text(item[0][1])
            canonical_url_v1(value)
            if previous is not None and value.encode("utf-8") <= previous.encode("utf-8"):
                reject("DUPLICATE_CHUNK_RECORD" if value == previous else "CHUNK_ORDER")
            previous = value
    elif kind == "discoveries":
        if ordinal >= 2 or not 1 <= len(records) <= MAX_NON_BLOB_CHUNK_RECORDS:
            reject("CHUNK_RECORD_COUNT_LIMIT")
        previous = None
        expected_names = (
            "job_id",
            "canonical_url",
            "depth",
            "score_text",
            "group_id",
            "rate_scope_id",
            "group_scope_id",
            "initial_origin_scope_id",
            "policy_decision_sha256",
        )
        for item in records:
            if field_names(item) != expected_names:
                reject("INVALID_CHUNK_SHAPE")
            values = [model_text(value) for _, value in item]
            if HEX_64_RE.fullmatch(values[0]) is None or url_id(values[1]) != values[0]:
                reject("URL_IDENTITY_MISMATCH")
            parse_canonical_decimal(values[2], maximum=MAX_EXACT_INTEGER)
            validate_score_text(values[3])
            validate_group_id(values[4])
            validate_rate_scope_id(values[5])
            for digest in values[6:]:
                if HEX_64_RE.fullmatch(digest) is None:
                    reject("INVALID_DIGEST")
            if previous is not None and values[0] <= previous:
                reject("DUPLICATE_CHUNK_RECORD" if values[0] == previous else "CHUNK_ORDER")
            previous = values[0]
    elif kind == "aliases":
        if ordinal != 0 or not 1 <= len(records) <= MAX_ALIASES:
            reject("ALIAS_COUNT_LIMIT")
        previous = None
        depth = None
        for item in records:
            if field_names(item) != ("url_id", "canonical_url", "depth"):
                reject("INVALID_CHUNK_SHAPE")
            values = [model_text(value) for _, value in item]
            if HEX_64_RE.fullmatch(values[0]) is None or url_id(values[1]) != values[0]:
                reject("URL_IDENTITY_MISMATCH")
            parse_canonical_decimal(values[2], maximum=MAX_EXACT_INTEGER)
            if depth is None:
                depth = values[2]
            elif values[2] != depth:
                reject("INVALID_ALIAS_DEPTH")
            if previous is not None and values[0] <= previous:
                reject("DUPLICATE_CHUNK_RECORD" if values[0] == previous else "CHUNK_ORDER")
            previous = values[0]
    elif kind == "images":
        if ordinal != 0 or not 1 <= len(records) <= MAX_IMAGES:
            reject("CHUNK_RECORD_COUNT_LIMIT")
        previous = None
        for item in records:
            if field_names(item) != ("normalized_source_url", "alt"):
                reject("INVALID_CHUNK_SHAPE")
            source = model_text(item[0][1])
            canonical_url_v1(source)
            alt = model_text(item[1][1])
            if len(alt.encode("utf-8")) > MAX_IMAGE_ALT_BYTES:
                reject("IMAGE_ALT_LIMIT")
            if previous is not None and source.encode("utf-8") <= previous.encode("utf-8"):
                reject("DUPLICATE_CHUNK_RECORD" if source == previous else "CHUNK_ORDER")
            previous = source
    elif kind == "image_manifest":
        if ordinal != 0 or len(records) != 1 or field_names(records[0]) != (
            "contract_version",
            "publication_id",
            "normalized_url",
            "image_count",
            "image_keys",
        ):
            reject("INVALID_CHUNK_SHAPE")
        values = [model_text(value) for _, value in records[0]]
        if values[0] != "1" or HEX_64_RE.fullmatch(values[1]) is None:
            reject("INVALID_CHUNK_SHAPE")
        canonical_url_v1(values[2])
        count = parse_canonical_decimal(values[3], maximum=MAX_EXACT_INTEGER)
        if count > MAX_IMAGES:
            reject("OUTPUT_COUNT_LIMIT")
        manifest_raw = values[4].encode("utf-8")
        if len(manifest_raw) > MAX_IMAGE_MANIFEST_BYTES:
            reject("IMAGE_MANIFEST_LIMIT")
        try:
            keys = loads_strict(values[4], rejection_mode=True)
        except Rejection as error:
            if error.rejection_class in {"INVALID_JSON", "DUPLICATE_JSON_KEY", "NON_STANDARD_JSON_NUMBER"}:
                reject("INVALID_IMAGE_MANIFEST")
            raise
        if type(keys) is not list or len(keys) != count or any(type(key) is not str for key in keys):
            reject("INVALID_IMAGE_MANIFEST")
        compact = json.dumps(keys, ensure_ascii=True, separators=(",", ":"))
        if compact != values[4]:
            reject("INVALID_IMAGE_MANIFEST")
        page64 = base64.urlsafe_b64encode(values[2].encode("utf-8")).decode("ascii").rstrip("=")
        prefix = f"image_data:{values[1]}:{page64}:"
        previous_source = None
        for key in keys:
            if not key.startswith(prefix) or key == prefix or ":" in key[len(prefix):]:
                reject("INVALID_IMAGE_MANIFEST")
            encoded = key[len(prefix):]
            try:
                padded = encoded + "=" * ((4 - len(encoded) % 4) % 4)
                source_raw = base64.b64decode(padded, altchars=b"-_", validate=True)
                source = validate_wire_utf8(source_raw)
            except (ValueError, UnicodeError):
                reject("INVALID_IMAGE_MANIFEST")
            if base64.urlsafe_b64encode(source_raw).decode("ascii").rstrip("=") != encoded:
                reject("INVALID_IMAGE_MANIFEST")
            canonical_url_v1(source)
            if previous_source is not None and source.encode("utf-8") <= previous_source.encode("utf-8"):
                reject("DUPLICATE_CHUNK_RECORD" if source == previous_source else "CHUNK_ORDER")
            previous_source = source


def derive_chunk_digest(
    commit_id: str,
    kind: str,
    ordinal: int,
    records: list[list[tuple[str, bytes | str]]],
) -> str:
    validate_stage_chunk(commit_id, kind, ordinal, records)
    return digest_sections(
        "mifolyo:stage-chunk:v2",
        frame(commit_id),
        frame(kind),
        frame(str(ordinal)),
        section("records", records),
    )


def complete_discovery_records(data: dict[str, object]) -> list[list[tuple[str, str]]]:
    output = data["output"]
    targets = data["targets"]
    if type(output) is not dict or type(targets) is not dict:
        reject("FIXTURE_TYPE")
    raw = output["discoveries"]
    if type(raw) is not list:
        reject("FIXTURE_TYPE")
    ordered = sorted(raw, key=lambda item: model_text(targets[model_text(item["target"])]["url_id"]))  # type: ignore[index]
    result: list[list[tuple[str, str]]] = []
    for item in ordered:
        if type(item) is not dict:
            reject("FIXTURE_TYPE")
        semantic = source_job_fields(item, data, complete=True)
        result.append(
            [
                semantic[0],
                semantic[1],
                semantic[3],
                semantic[2],
                semantic[4],
                semantic[5],
                semantic[6],
                semantic[7],
                semantic[8],
            ]
        )
    return result


def image_data_key(publication_id: str, page_url: str, source_url: str) -> str:
    page64 = base64.urlsafe_b64encode(page_url.encode("utf-8")).decode("ascii").rstrip("=")
    source64 = base64.urlsafe_b64encode(source_url.encode("utf-8")).decode("ascii").rstrip("=")
    return f"image_data:{publication_id}:{page64}:{source64}"


def stage_chunks_for_output(
    data: dict[str, object],
    records: tuple[list[list[tuple[str, bytes | str]]], ...],
    publication_id: str,
    commit_id: str,
) -> tuple[dict[str, list[str]], dict[str, object]]:
    page, outlinks, images, _discoveries, aliases = records
    full_page = page[0]
    page_fields = [
        full_page[0],
        full_page[3],
        full_page[4],
        full_page[5],
        full_page[6],
        full_page[7],
        full_page[8],
        ("publication_id", publication_id),
    ]
    chunks: dict[str, list[list[list[tuple[str, bytes | str]]]]] = {kind: [] for kind in CHUNK_KINDS}
    chunks["page_fields"] = [[page_fields]]
    chunks["html"] = [[[('field_name', 'html'), ('field_bytes', full_page[1][1])]]]
    chunks["original_html"] = [[[('field_name', 'original_html'), ('field_bytes', full_page[2][1])]]]
    chunks["outlinks"] = [outlinks[index:index + 64] for index in range(0, len(outlinks), 64)]
    complete_discoveries = complete_discovery_records(data)
    chunks["discoveries"] = [
        complete_discoveries[index:index + 64]
        for index in range(0, len(complete_discoveries), 64)
    ]
    chunks["aliases"] = [aliases]
    chunks["images"] = [images] if images else []
    page_url = model_text(full_page[0][1])
    image_keys = [image_data_key(publication_id, page_url, model_text(item[0][1])) for item in images]
    manifest_json = json.dumps(image_keys, ensure_ascii=True, separators=(",", ":"))
    chunks["image_manifest"] = [[
        [
            ("contract_version", "1"),
            ("publication_id", publication_id),
            ("normalized_url", page_url),
            ("image_count", str(len(image_keys))),
            ("image_keys", manifest_json),
        ]
    ]]
    digests: dict[str, list[str]] = {kind: [] for kind in CHUNK_KINDS}
    for kind in CHUNK_KINDS:
        for ordinal, chunk_records in enumerate(chunks[kind]):
            digests[kind].append(derive_chunk_digest(commit_id, kind, ordinal, chunk_records))
    return digests, dict(chunks)


def publication_and_commit(data: dict[str, object], output_digest: str) -> tuple[str, str]:
    identities = data["identities"]
    if type(identities) is not dict:
        reject("FIXTURE_TYPE")
    baseline, generation = request_start_interval(expect_object(data["output_context"], "output_context"))
    publication = digest_framed(
        "mifolyo:page-publication:v2",
        model_text(identities["run_id"]),
        model_text(identities["job_id"]),
        str(require_exact_integer(identities["fence"], positive=True)),
        output_digest,
    )
    commit = digest_framed(
        "mifolyo:crawl-commit:v2",
        model_text(identities["run_id"]),
        model_text(identities["job_id"]),
        str(require_exact_integer(identities["fence"], positive=True)),
        model_text(identities["lease_token"]),
        publication,
        str(baseline),
        str(generation),
    )
    return publication, commit


def compute_baseline(data: dict[str, object]) -> dict[str, object]:
    identities = data["identities"]
    targets = data["targets"]
    decisions = data["policy_decisions"]
    if not all(type(value) is dict for value in (identities, targets, decisions)):
        reject("FIXTURE_TYPE")
    validate_identity_values(identities)  # type: ignore[arg-type]
    validate_run_policy_bindings(data)
    for target in targets.values():  # type: ignore[union-attr]
        if type(target) is not dict:
            reject("FIXTURE_TYPE")
        validate_target(target)
    if identities["job_id"] != targets["page"]["url_id"]:  # type: ignore[index]
        reject("URL_IDENTITY_MISMATCH")

    sample_record = [("a", "b"), ("x", "")]
    sample_section = section("s", [[("a", "b")]])
    target_digests = {name: target_digest(value) for name, value in targets.items()}  # type: ignore[union-attr,arg-type]
    decision_digests = {
        name: decision_digest(value, targets)  # type: ignore[arg-type]
        for name, value in decisions.items()  # type: ignore[union-attr]
    }
    group_records = validate_policy_groups(data["policy_groups"])
    group_map_digest = digest_sections(
        "mifolyo:policy-group-map:v2",
        section("groups", group_records),
    )
    token_digest = digest_framed(
        "mifolyo:lease-token:v2",
        model_text(identities["run_id"]),
        model_text(identities["job_id"]),
        str(require_exact_integer(identities["fence"], positive=True)),
        model_text(identities["lease_token"]),
    )
    reservation = reservation_id(data, data["reservation"])  # type: ignore[arg-type]
    source_jobs = data["source_jobs"]
    if type(source_jobs) is not list:
        reject("FIXTURE_TYPE")
    if len(source_jobs) > MAX_SOURCE_JOBS:
        reject("SOURCE_COUNT_LIMIT")
    source_records = [
        source_job_fields(item, data)
        for item in sorted(source_jobs, key=lambda item: model_text(targets[model_text(item["target"])]["url_id"]))  # type: ignore[index]
    ]
    source_digest = digest_sections("mifolyo:crawl-source:v2", section("jobs", source_records))

    output = output_records(data)
    output_digest = output_digest_from_records(output)
    publication_id, commit_id = publication_and_commit(data, output_digest)

    reasons = data["transition_reasons"]
    claim = data["try_claim"]
    if type(reasons) is not dict or type(claim) is not dict:
        reject("FIXTURE_TYPE")
    claim_index = model_integer(claim["source_job_index"])
    if claim_index < 0 or claim_index >= len(source_jobs):
        reject("FIXTURE_SCHEMA")
    source_complete = source_job_fields(source_jobs[claim_index], data, complete=True)
    reject_payload = source_complete[1:]
    decision_name = model_text(claim["decision"])
    target_name = model_text(claim["target"])
    if decision_name not in decisions or target_name not in targets:  # type: ignore[operator]
        reject("UNKNOWN_REFERENCE")
    claim_decision = decisions[decision_name]  # type: ignore[index]
    claim_target = targets[target_name]  # type: ignore[index]
    if type(claim_decision) is not dict or type(claim_target) is not dict:
        reject("FIXTURE_TYPE")
    if claim_decision["target"] != target_name:
        reject("RESERVATION_TARGET_MISMATCH")
    claim_fields = dict(decision_fields(claim_decision, targets))  # type: ignore[arg-type]
    try_payload = [
        ("canonical_url", source_complete[1][1]),
        ("score_text", source_complete[2][1]),
        ("depth", source_complete[3][1]),
        ("job_group_id", source_complete[4][1]),
        ("job_rate_scope_id", source_complete[5][1]),
        ("job_group_scope_id", source_complete[6][1]),
        ("job_initial_origin_scope_id", source_complete[7][1]),
        ("job_policy_decision_sha256", source_complete[8][1]),
        ("expected_prior_fence", str(require_exact_integer(claim["expected_prior_fence"]))),
        ("owner_id", model_text(identities["owner_id"])),
        ("request_ordinal", str(require_exact_integer(claim["request_ordinal"], positive=True))),
        ("request_kind", claim_fields["request_kind"]),
        ("target_url_id", model_text(claim_target["url_id"])),
        ("canonical_target_url", model_text(claim_target["canonical_url"])),
        ("target_digest", claim_fields["target_digest"]),
        ("crawl_policy_sha256", model_text(identities["crawl_policy_sha256"])),
        ("policy_decision_sha256", decision_digest(claim_decision, targets)),  # type: ignore[arg-type]
        ("group_id", claim_fields["group_id"]),
        ("rate_scope_id", claim_fields["rate_scope_id"]),
        ("global_scope_id", claim_fields["global_scope_id"]),
        ("group_scope_id", claim_fields["group_scope_id"]),
        ("origin_scope_id", claim_fields["origin_scope_id"]),
        ("global_concurrency", claim_fields["global_concurrency"]),
        ("global_interval_ms", claim_fields["global_interval_ms"]),
        ("group_concurrency", claim_fields["group_concurrency"]),
        ("group_interval_ms", claim_fields["group_interval_ms"]),
        ("origin_concurrency", claim_fields["origin_concurrency"]),
        ("origin_interval_ms", claim_fields["origin_interval_ms"]),
    ]
    owner_payload = [("owner_id", model_text(identities["owner_id"]))]
    transition_specs = {
        "reject_ready": ("CJ2_REJECT_READY", model_text(reasons["reject_ready"]), reject_payload, False),
        "try_claim": ("CJ2_TRY_CLAIM", "none", try_payload, True),
        "release_before_io": ("CJ2_RELEASE_BEFORE_IO", "none", owner_payload, True),
        "retry": ("CJ2_RETRY", model_text(reasons["retry"]), owner_payload, True),
        "dead": ("CJ2_DEAD", model_text(reasons["dead"]), owner_payload, True),
        "cancel_job": ("CJ2_CANCEL_JOB", model_text(reasons["cancel_job"]), owner_payload, True),
        "complete_no_output": ("CJ2_COMPLETE_NO_OUTPUT", model_text(reasons["complete_no_output"]), owner_payload, True),
        "abort_stage": ("CJ2_ABORT_STAGE", "none", owner_payload + [("commit_id", commit_id)], True),
    }
    transition_ids: dict[str, str] = {}
    payload_digests: dict[str, str] = {}
    for name, (operation, reason, payload, leased) in transition_specs.items():
        validate_transition_reason(operation, reason)
        payload_digests[name] = transition_payload_digest(payload)
        transition_ids[name] = transition_id(data, operation, reason, payload, leased)

    chunk = data["chunk"]
    if type(chunk) is not dict:
        reject("FIXTURE_TYPE")
    chunk_records_value = chunk["records"]
    if type(chunk_records_value) is not list:
        reject("FIXTURE_TYPE")
    chunk_records = [[("target_url", model_text(item["target_url"]))] for item in chunk_records_value]
    chunk_digest = derive_chunk_digest(
        commit_id,
        model_text(chunk["kind"]),
        model_integer(chunk["ordinal"]),
        chunk_records,
    )
    return {
        "framing": {
            "u64_1_hex": u64(1).hex(),
            "f_A_hex": frame("A").hex(),
            "record_hex": record(sample_record).hex(),
            "section_hex": sample_section.hex(),
        },
        "scope_ids": {
            "global": digest_framed("mifolyo:rate:global:v2"),
            "group_a": group_scope(model_text(identities["rate_scope_id"])),
            "origin": origin_scope(model_text(targets["page"]["canonical_url"])),  # type: ignore[index]
        },
        "url_ids": {name: url_id(model_text(value["canonical_url"])) for name, value in targets.items()},  # type: ignore[union-attr,index]
        "target_digests": target_digests,
        "policy_decision_digests": decision_digests,
        "policy_group_map_sha256": group_map_digest,
        "token_digest": token_digest,
        "reservation_id": reservation,
        "source_sha256": source_digest,
        "output_digest": output_digest,
        "publication_id": publication_id,
        "commit_id": commit_id,
        "chunk_digest": chunk_digest,
        "transition_payload_digests": payload_digests,
        "transition_ids": transition_ids,
    }


def sized_url(host: str, namespace: str, index: int, size: int = MAX_CANONICAL_URL_BYTES) -> str:
    prefix = f"https://{host}/{namespace}/{index:05d}/"
    if len(prefix.encode("ascii")) > size:
        raise FixtureError("generated URL prefix exceeds requested size")
    return prefix + "x" * (size - len(prefix))


def generated_repeat(value: object) -> str:
    if type(value) is not dict:
        raise FixtureError("repeated-text generator is not an object")
    text = model_text(value["text"])
    count = model_integer(value["repeat_count"])
    expected_bytes = model_integer(value["expected_utf8_bytes"])
    if not text or count < 1 or expected_bytes < 1:
        raise FixtureError("invalid repeated-text generator")
    result = text * count
    if len(result.encode("utf-8")) != expected_bytes:
        raise FixtureError("repeated-text generator did not produce its declared UTF-8 width")
    return result


def generated_padded_text(value: object) -> str:
    if type(value) is not dict:
        raise FixtureError("padded-text generator is not an object")
    prefix = model_text(value["prefix"])
    fill = model_text(value["fill_text"])
    suffix = model_text(value["suffix"])
    total = model_integer(value["total_utf8_bytes"])
    fixed = len(prefix.encode("utf-8")) + len(suffix.encode("utf-8"))
    fill_width = len(fill.encode("utf-8"))
    if fill_width == 0 or total < fixed or (total - fixed) % fill_width:
        raise FixtureError("invalid padded-text generator width")
    result = prefix + fill * ((total - fixed) // fill_width) + suffix
    if len(result.encode("utf-8")) != total:
        raise FixtureError("padded-text generator did not produce its declared UTF-8 width")
    return result


def generated_indexed_urls(value: object) -> list[str]:
    if type(value) is not dict or model_text(value["grammar"]) != "indexed_template_v1":
        raise FixtureError("unsupported indexed-template generator")
    template = model_text(value["template"])
    count = model_integer(value["count"])
    first = model_integer(value["first_index"])
    width = model_integer(value["index_width"])
    radix = model_integer(value["index_radix"])
    fill_to = model_integer(value["fill_to_bytes"])
    order = model_text(value["input_order"])
    try:
        fill = bytes.fromhex(model_text(value["fill_byte_hex"])).decode("utf-8", "strict")
    except (ValueError, UnicodeDecodeError) as error:
        raise FixtureError("indexed-template fill byte is invalid") from error
    if (
        count < 1 or count > MAX_SOURCE_JOBS or first < 0 or not 1 <= width <= 16
        or radix not in {10, 16} or template.count("{index}") != 1
        or len(fill.encode("utf-8")) != 1 or order not in {"ascending", "descending"}
    ):
        raise FixtureError("invalid indexed-template generator fields")
    result: list[str] = []
    for offset in range(count):
        raw_index = format(first + offset, "d" if radix == 10 else "x")
        if len(raw_index) > width:
            raise FixtureError("indexed-template index exceeds its declared width")
        rendered = template.replace("{index}", raw_index.rjust(width, "0"), 1)
        rendered_bytes = len(rendered.encode("utf-8"))
        if fill_to:
            if rendered_bytes > fill_to:
                raise FixtureError("indexed-template prefix exceeds fill_to_bytes")
            rendered += fill * (fill_to - rendered_bytes)
        canonical_url_v1(rendered)
        result.append(rendered)
    if order == "descending":
        result.reverse()
    return result


def generated_policy_group(value: object) -> tuple[dict[str, object], dict[str, object]]:
    if type(value) is not dict:
        raise FixtureError("generated policy group is not an object")
    group_id = generated_repeat(value["group_id"])
    rate_scope_id = generated_repeat(value["rate_scope_id"])
    group = {
        "group_id": group_id,
        "rate_scope_id": rate_scope_id,
        "request_start_limit": model_integer(value["request_start_limit"]),
        "concurrency": model_integer(value["concurrency"]),
        "interval_ms": model_integer(value["interval_ms"]),
    }
    if (
        model_integer(value["global_concurrency"]) != 2
        or model_integer(value["global_interval_ms"]) != 0
        or model_integer(value["origin_concurrency"]) != group["concurrency"]
        or model_integer(value["origin_interval_ms"]) != group["interval_ms"]
    ):
        reject("POLICY_GROUP_BINDING_MISMATCH")
    validate_policy_groups([group])
    return group, value


def generated_decision(
    request_kind: object,
    target: str,
    depth: object,
    group: dict[str, object],
    tuple_spec: dict[str, object],
) -> dict[str, object]:
    if model_text(request_kind) not in {"robots", "document", "redirect", "render_resource"}:
        reject("INVALID_REQUEST_KIND")
    return {
        "request_kind": model_text(request_kind),
        "target": target,
        "depth": require_exact_integer(depth),
        "group_id": model_text(group["group_id"]),
        "rate_scope_id": model_text(group["rate_scope_id"]),
        "concurrency": model_integer(tuple_spec["concurrency"]),
        "interval_ms": model_integer(tuple_spec["interval_ms"]),
    }


def build_maximum_output_data(generator_value: object) -> tuple[dict[str, object], str, set[str]]:
    if type(generator_value) is not dict or model_text(generator_value["grammar"]) != "maximum_output_v1":
        raise FixtureError("maximum output is missing its declarative generator")
    identity_spec = generator_value["identity"]
    policy_spec_value = generator_value["policy_group"]
    if type(identity_spec) is not dict or type(policy_spec_value) is not dict:
        raise FixtureError("maximum output generator identity/policy shape mismatch")
    group, policy_spec = generated_policy_group(policy_spec_value)
    alias_urls = generated_indexed_urls(generator_value["aliases"])
    if len(alias_urls) != MAX_ALIASES:
        raise FixtureError("maximum output alias generator is not at the protocol maximum")
    targets: dict[str, dict[str, object]] = {}
    decisions: dict[str, dict[str, object]] = {}
    alias_names = [f"alias_{index}" for index in range(len(alias_urls))]
    for name, canonical in zip(alias_names, alias_urls):
        targets[name] = {"url_id": url_id(canonical), "canonical_url": canonical}
    source_spec = generator_value["source"]
    if type(source_spec) is not dict or model_text(source_spec["policy_group_ref"]) != "policy_group":
        raise FixtureError("maximum output source has an invalid policy-group reference")
    source_index = model_integer(source_spec["target_index"])
    if source_index < 0 or source_index >= len(alias_names):
        raise FixtureError("maximum output source target index is out of range")
    source_target = alias_names[source_index]
    source_decision_name = "source_document"
    decisions[source_decision_name] = generated_decision(
        source_spec["request_kind"], source_target, source_spec["depth"], group, policy_spec,
    )
    if decisions[source_decision_name]["request_kind"] != "document":
        raise FixtureError("maximum output source decision must be document")
    if model_text(identity_spec["job_id_from"]) != "source_target_url_id":
        raise FixtureError("maximum output identity has an unknown job-ID source")
    if model_text(identity_spec["rate_scope_id_from"]) != "policy_group.rate_scope_id":
        raise FixtureError("maximum output identity has an unknown rate-scope source")
    identities: dict[str, object] = {
        "run_id": model_text(identity_spec["run_id"]),
        "job_id": targets[source_target]["url_id"],
        "owner_id": model_text(identity_spec["owner_id"]),
        "alternate_owner_id": model_text(identity_spec["alternate_owner_id"]),
        "lease_token": model_text(identity_spec["lease_token"]),
        "fence": model_integer(identity_spec["fence"]),
        "rate_scope_id": group["rate_scope_id"],
        "crawl_policy_sha256": model_text(identity_spec["crawl_policy_sha256"]),
    }
    discovery_spec = generator_value["discoveries"]
    if type(discovery_spec) is not dict or model_text(discovery_spec["policy_group_ref"]) != "policy_group":
        raise FixtureError("maximum discoveries have an invalid policy-group reference")
    discovery_urls = generated_indexed_urls(discovery_spec["urls"])
    if len(discovery_urls) != MAX_DISCOVERIES:
        raise FixtureError("maximum discovery generator is not at the protocol maximum")
    discovery_items: list[dict[str, object]] = []
    for index, canonical in enumerate(discovery_urls):
        name = f"discovery_{index}"
        targets[name] = {"url_id": url_id(canonical), "canonical_url": canonical}
        decision_name = f"discovery_decision_{index}"
        decisions[decision_name] = generated_decision(
            discovery_spec["request_kind"], name, discovery_spec["depth"], group, policy_spec,
        )
        if decisions[decision_name]["request_kind"] != "document":
            raise FixtureError("maximum discovery decision must be document")
        discovery_items.append(
            {
                "target": name,
                "score_text": model_text(discovery_spec["score_text"]),
                "depth": require_exact_integer(discovery_spec["depth"]),
                "group_id": group["group_id"],
                "rate_scope_id": group["rate_scope_id"],
                "decision": decision_name,
            }
        )
    chain_spec = generator_value["request_chain"]
    if type(chain_spec) is not dict or model_text(chain_spec["policy_group_ref"]) != "policy_group":
        raise FixtureError("maximum request chain has an invalid policy-group reference")
    first_started = require_exact_integer(chain_spec["first_started_at_ms"], positive=True)
    step = require_exact_integer(chain_spec["started_at_step_ms"], positive=True)
    initial_kind = model_text(chain_spec["initial_request_kind"])
    subsequent_kind = model_text(chain_spec["subsequent_request_kind"])
    if initial_kind != "document" or subsequent_kind != "redirect":
        raise FixtureError("maximum request chain must declare document then redirect")
    counts = expect_list(chain_spec["job_request_starts"], "request_chain.job_request_starts")
    ordinals = expect_list(chain_spec["request_ordinals"], "request_chain.request_ordinals")
    if len(counts) != len(alias_names) or len(ordinals) != len(alias_names):
        raise FixtureError("maximum request chain snapshot arrays must cover every event")
    requests = [
        {
            "request_kind": initial_kind if index == 0 else subsequent_kind,
            "target": name,
            "started_at_ms": first_started + index * step,
            "job_request_starts": counts[index],
            "request_ordinal": ordinals[index],
        }
        for index, name in enumerate(alias_names)
    ]
    outlinks = generated_indexed_urls(generator_value["outlinks"])
    if len(outlinks) != MAX_OUTLINKS:
        raise FixtureError("maximum outlink generator is not at the protocol maximum")
    images_spec = generator_value["images"]
    if type(images_spec) is not dict:
        raise FixtureError("maximum image generator is not an object")
    image_urls = generated_indexed_urls(images_spec["urls"])
    if len(image_urls) != MAX_IMAGES:
        raise FixtureError("maximum image generator is not at the protocol maximum")
    image_alt = generated_repeat(images_spec["alt"])
    page_spec = generator_value["page"]
    if type(page_spec) is not dict:
        raise FixtureError("maximum page generator is not an object")
    normalized_index = model_integer(page_spec["normalized_target_index"])
    if normalized_index < 0 or normalized_index >= len(alias_names):
        raise FixtureError("maximum page normalized target index is out of range")
    render_digest = model_text(page_spec["render_policy_sha256"])
    if render_digest != identities["crawl_policy_sha256"]:
        raise FixtureError("maximum page render digest differs from its identity input")
    render_rule = generated_repeat(page_spec["render_policy_rule"])
    generated: dict[str, object] = {
        "identities": identities,
        "targets": targets,
        "policy_decisions": decisions,
        "policy_groups": [group],
        "source_jobs": [
            {
                "target": source_target,
                "score_text": model_text(source_spec["score_text"]),
                "depth": require_exact_integer(source_spec["depth"]),
                "group_id": group["group_id"],
                "rate_scope_id": group["rate_scope_id"],
                "decision": source_decision_name,
            }
        ],
        "output_context": {
            "source_job_index": 0,
            "lease_request_starts_baseline": chain_spec["lease_request_starts_baseline"],
            "terminal_request_starts_generation": chain_spec["terminal_request_starts_generation"],
            "requests": requests,
        },
        "output": {
            "page": {
                "normalized_target": alias_names[normalized_index],
                "html": generated_repeat(page_spec["html"]),
                "original_html": generated_repeat(page_spec["original_html"]),
                "content_type": generated_padded_text(page_spec["content_type"]),
                "status_code": model_integer(page_spec["status_code"]),
                "rendered": model_boolean(page_spec["rendered"]),
                "render_policy_rule": render_rule,
                "render_policy_sha256": render_digest,
            },
            "outlinks": outlinks,
            "images": [{"normalized_source_url": value, "alt": image_alt} for value in image_urls],
            "discoveries": discovery_items,
        },
    }
    validate_identity_values(identities)
    validate_run_policy_bindings(generated)
    return generated, render_digest, {render_rule}


def build_output_profile_data(
    profile: str,
    base: dict[str, object],
    generator: object | None = None,
) -> tuple[dict[str, object], str | None, set[str] | None]:
    if profile == "baseline":
        if generator is not None:
            raise FixtureError("baseline output profile must not provide a generator")
        return base, None, None
    if profile == "maximum":
        if generator is None:
            raise FixtureError("maximum output profile requires a declarative generator")
        return build_maximum_output_data(generator)
    if generator is not None:
        raise FixtureError(f"non-maximum output profile {profile!r} must not provide a generator")
    changed = copy.deepcopy(base)
    output = changed["output"]
    context = changed["output_context"]
    identities = changed["identities"]
    if type(output) is not dict or type(context) is not dict or type(identities) is not dict:
        raise FixtureError("baseline data has invalid internal shape")
    page = output["page"]
    if type(page) is not dict:
        raise FixtureError("baseline page has invalid internal shape")
    if profile == "empty":
        requests = context["requests"]
        if type(requests) is not list:
            raise FixtureError("baseline requests have invalid internal shape")
        context["requests"] = requests[:1]
        context["terminal_request_starts_generation"] = requests[0]["job_request_starts"]
        page.update(
            {
                "normalized_target": "page",
                "html": "",
                "original_html": "",
                "content_type": "text/html",
                "status_code": 100,
                "rendered": False,
                "render_policy_rule": "",
                "render_policy_sha256": "",
            }
        )
        output["outlinks"] = []
        output["images"] = []
        output["discoveries"] = []
        return changed, None, None
    if profile == "rendered":
        rule = "render-main"
        digest = model_text(identities["crawl_policy_sha256"])
        page.update(
            {
                "html": "<html>rendered</html>",
                "original_html": "<html>source</html>",
                "rendered": True,
                "render_policy_rule": rule,
                "render_policy_sha256": digest,
            }
        )
        return changed, digest, {rule}
    raise FixtureError(f"unknown output profile {profile!r}")


_OUTPUT_PROFILE_CACHE: dict[str, dict[str, object]] = {}


def profile_cache_key(profile: str, generator: object | None) -> str:
    encoded = json.dumps(generator, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return f"{profile}:{plain_sha256(encoded.encode('utf-8'))}"


def output_profile_result(
    profile: str,
    base: dict[str, object],
    generator: object | None = None,
) -> dict[str, object]:
    cache_key = profile_cache_key(profile, generator)
    if cache_key in _OUTPUT_PROFILE_CACHE:
        return _OUTPUT_PROFILE_CACHE[cache_key]
    data, render_digest, rules = build_output_profile_data(profile, base, generator)
    validate_identity_values(data["identities"])  # type: ignore[arg-type]
    records = output_records(
        data,
        authorized_render_policy_sha256=render_digest,
        enabled_render_rules=rules,
    )
    digest = output_digest_from_records(records)
    publication, commit = publication_and_commit(data, digest)
    chunk_digests, chunks = stage_chunks_for_output(data, records, publication, commit)
    encoded_sections = {
        label: section(label, records[index])
        for index, label in enumerate(OUTPUT_SECTION_LABELS)
    }
    result: dict[str, object] = {
        "data": data,
        "records": records,
        "chunks": chunks,
        "expected": {
            "output_digest": digest,
            "publication_id": publication,
            "commit_id": commit,
            "counts": {label: len(records[index]) for index, label in enumerate(OUTPUT_SECTION_LABELS)},
            "section_sha256": {label: plain_sha256(encoded_sections[label]) for label in OUTPUT_SECTION_LABELS},
            "chunk_digests": chunk_digests,
        },
    }
    _OUTPUT_PROFILE_CACHE[cache_key] = result
    return result


_SOURCE_PROFILE_CACHE: dict[str, dict[str, object]] = {}


def source_profile_result(profile: str, generator: object | None = None) -> dict[str, object]:
    cache_key = profile_cache_key(profile, generator)
    if cache_key in _SOURCE_PROFILE_CACHE:
        return _SOURCE_PROFILE_CACHE[cache_key]
    if profile == "empty":
        if generator is not None:
            raise FixtureError("empty source profile must not provide a generator")
        records: list[list[tuple[str, str]]] = []
    elif profile == "maximum":
        if type(generator) is not dict or model_text(generator["grammar"]) != "maximum_source_v1":
            raise FixtureError("maximum source profile requires a maximum_source_v1 generator")
        group_value = generator["policy_group"]
        source = generator["source"]
        if type(group_value) is not dict or type(source) is not dict:
            raise FixtureError("maximum source generator shape mismatch")
        group, tuple_spec = generated_policy_group(group_value)
        if model_text(source["policy_group_ref"]) != "policy_group":
            raise FixtureError("maximum source has an invalid policy-group reference")
        request_kind = model_text(source["request_kind"])
        if request_kind != "document":
            raise FixtureError("maximum source request kind must be document")
        score_text = model_text(source["score_text"])
        validate_score_text(score_text)
        depth = require_exact_integer(source["depth"])
        urls = generated_indexed_urls(generator["jobs"])
        if len(urls) != MAX_SOURCE_JOBS:
            raise FixtureError("maximum source generator is not at the protocol maximum")
        sortable: list[tuple[str, list[tuple[str, str]]]] = []
        for canonical in urls:
            target = {"url_id": url_id(canonical), "canonical_url": canonical}
            decision = generated_decision(request_kind, "item", depth, group, tuple_spec)
            data: dict[str, object] = {
                "targets": {"item": target},
                "policy_decisions": {"decision": decision},
                "policy_groups": [group],
            }
            item: dict[str, object] = {
                "target": "item",
                "score_text": score_text,
                "depth": depth,
                "group_id": group["group_id"],
                "rate_scope_id": group["rate_scope_id"],
                "decision": "decision",
            }
            sortable.append((model_text(target["url_id"]), source_job_fields(item, data)))
        records = [item for _, item in sorted(sortable, key=lambda pair: pair[0].encode("ascii"))]
    else:
        raise FixtureError(f"unknown source profile {profile!r}")
    encoded = section("jobs", records)
    result = {
        "count": len(records),
        "section_sha256": plain_sha256(encoded),
        "source_sha256": digest_sections("mifolyo:crawl-source:v2", encoded),
    }
    _SOURCE_PROFILE_CACHE[cache_key] = result
    return result


def contract_case_result(case_input: dict[str, object], root: Path) -> dict[str, object]:
    document = case_input["document"]
    sources = case_input["lua_sources"]
    if type(document) is not dict or type(sources) is not list:
        raise FixtureError("contract case internal schema mismatch")
    document_kind = model_text(document["kind"])
    document_value = model_text(document["value"])
    if document_kind == "path":
        if document_value != "docs/crawl-jobs-v2.md":
            raise FixtureError(f"contract case path is not the normative document: {document_value!r}")
        document_bytes = (root / document_value).read_bytes()
        try:
            document_bytes.decode("utf-8", "strict")
        except UnicodeDecodeError as error:
            raise FixtureError("normative contract document is not UTF-8") from error
    elif document_kind == "utf8":
        document_bytes = document_value.encode("utf-8", "strict")
    else:
        raise FixtureError(f"unknown contract document kind {document_kind!r}")
    named_sources: list[tuple[str, bytes]] = []
    seen: set[str] = set()
    for item in sources:
        if type(item) is not dict:
            raise FixtureError("contract source is not an object")
        name = model_text(item["source_name"])
        if not name.isascii() or SOURCE_NAME_RE.fullmatch(name) is None or name in seen:
            raise FixtureError(f"invalid or duplicate Lua source name {name!r}")
        seen.add(name)
        named_sources.append((name, model_text(item["source_text"]).encode("utf-8")))
    named_sources.sort(key=lambda item: item[0].encode("ascii"))
    digest = digest_sections(
        "mifolyo:crawl-contract:v2",
        section("document", [[("document_bytes", document_bytes)]]),
        section("lua", [[("source_name", name), ("source_bytes", source)] for name, source in named_sources]),
    )
    return {"lua_source_order": [name for name, _ in named_sources], "contract_sha256": digest}


# Canonical bundle extension. This independent literal order is not imported
# from Go, the assembler/generator, or fixture expectations. Fixture path/source
# text can never select the files being hashed by this verifier.
CANONICAL_LUA_OPERATIONS = (
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
CANONICAL_LUA_DIRECTORY = "services/spider/internal/database/crawljobsv2/lua"
CANONICAL_DOCUMENT_PATH = "docs/crawl-jobs-v2.md"
CANONICAL_LUA_CASE = "canonical-lua-bundle"


def validate_canonical_bundle_fixture_extension(data: dict[str, object]) -> None:
    # Preserve the original 39/139 identity/rejection inventory independently of
    # the new suite pin. Only the new canonical cases may be filtered out.
    legacy_positive = [case for case in data["cases"] if case["name"] != CANONICAL_LUA_CASE]
    legacy_negative = [case for case in data["negative_vectors"] if case["kind"] != "canonical_lua_bundle_mutation"]
    inventory = hashlib.sha256(f"B\0{data['baseline_case_name']}\n".encode("utf-8"))
    for case in legacy_positive:
        inventory.update(f"P\0{case['name']}\0{case['kind']}\n".encode("utf-8"))
    for case in legacy_negative:
        inventory.update(f"N\0{case['name']}\0{case['kind']}\0{case['expected_rejection_class']}\n".encode("utf-8"))
    if (len(legacy_positive) != 39 or len(legacy_negative) != 139
            or inventory.hexdigest() != "56797748de64aa57618104bb5135d0300c9d192f41995e743ddb319219a248df"):
        raise FixtureError("canonical extension did not preserve the foundation 39/139 inventory")
    for case in data["cases"]:
        if case["kind"] in {"guard_core", "guard_chain"} and case["input"]["contract_case"] != CANONICAL_LUA_CASE:
            raise FixtureError("current guard/compatibility chain must bind the real canonical bundle independently")


def _canonical_bundle_input(case_input: object) -> None:
    obj = expect_keys(case_input, {"document_path", "lua_directory"}, "canonical bundle input")
    if obj != {"document_path": CANONICAL_DOCUMENT_PATH, "lua_directory": CANONICAL_LUA_DIRECTORY}:
        raise FixtureError("canonical bundle requires exact closed repository paths, not caller sources")


def schema_canonical_lua_bundle(case_input: object, expected: object, path: str) -> None:
    _canonical_bundle_input(case_input)
    obj = expect_keys(expected, {"entries", "lua_source_order", "source_set_sha256", "contract_sha256", "bundle_seal_sha256"},
                      f"{path}.expected")
    for field in ("source_set_sha256", "contract_sha256", "bundle_seal_sha256"):
        expect_hex(obj[field], 64, f"{path}.expected.{field}")
    names = [op.lower() + ".lua" for op in CANONICAL_LUA_OPERATIONS]
    if obj["lua_source_order"] != sorted(names, key=lambda name: name.encode("ascii")):
        raise FixtureError("canonical contract source order is not the exact ASCII filename order")
    entries = expect_list(obj["entries"], f"{path}.expected.entries")
    if len(entries) != 43:
        raise FixtureError("canonical fixture must pin all 43 source identities")
    for index, (op, raw_entry) in enumerate(zip(CANONICAL_LUA_OPERATIONS, entries)):
        entry = expect_keys(raw_entry, {"index", "operation", "source_name", "source_bytes", "redis_sha1", "source_sha256"},
                            f"{path}.expected.entries[{index}]")
        if (expect_integer(entry["index"], "bundle index") != index or entry["operation"] != op
                or entry["source_name"] != names[index]):
            raise FixtureError("canonical fixture entry identity/order differs from literal inventory")
        if expect_integer(entry["source_bytes"], "bundle source length") <= 0:
            raise FixtureError("canonical source byte count must be positive")
        if re.fullmatch(r"[0-9a-f]{40}", expect_text(entry["redis_sha1"], "Redis SHA-1")) is None:
            raise FixtureError("invalid canonical Redis SHA-1")
        expect_hex(entry["source_sha256"], 64, "source SHA-256")


def _canonical_closed_path(root: Path, relative: str) -> Path:
    path = root
    parts = relative.split("/")
    for index, part in enumerate(parts):
        if part in {"", ".", ".."}:
            reject("BUNDLE_INVENTORY")
        path = path / part
        mode = path.lstat().st_mode
        if stat.S_ISLNK(mode) or (index < len(parts) - 1 and not stat.S_ISDIR(mode)):
            reject("BUNDLE_INVENTORY")
    return path


def _canonical_read_text(path: Path) -> bytes:
    if not stat.S_ISREG(path.lstat().st_mode):
        reject("BUNDLE_INVENTORY")
    raw = path.read_bytes()
    _canonical_validate_text(raw)
    return raw


def _canonical_validate_text(raw: bytes) -> None:
    if type(raw) is not bytes or not raw or not raw.endswith(b"\n"):
        reject("BUNDLE_SOURCE_BYTES")
    try:
        raw.decode("utf-8", "strict")
    except UnicodeDecodeError:
        reject("BUNDLE_SOURCE_BYTES")


def _canonical_read_bundle(root: Path) -> tuple[bytes, list[dict[str, object]]]:
    # root is an internal test seam only. The public fixture evaluator below
    # ALWAYS chooses the repository containing this verifier, not its caller.
    if len(CANONICAL_LUA_OPERATIONS) != 43 or len(set(CANONICAL_LUA_OPERATIONS)) != 43:
        raise FixtureError("invalid independent canonical operation inventory")
    try:
        directory = _canonical_closed_path(root, CANONICAL_LUA_DIRECTORY)
        if not stat.S_ISDIR(directory.lstat().st_mode):
            reject("BUNDLE_INVENTORY")
        files = list(directory.iterdir())  # Include hidden files and subdirectories.
        names = [op.lower() + ".lua" for op in CANONICAL_LUA_OPERATIONS]
        if len(files) != 43 or {p.name for p in files} != set(names):
            reject("BUNDLE_INVENTORY")
        entries = []
        for index, op in enumerate(CANONICAL_LUA_OPERATIONS):
            source = _canonical_read_text(_canonical_closed_path(root, CANONICAL_LUA_DIRECTORY + "/" + names[index]))
            entries.append({"index": index, "operation": op, "source_name": names[index], "source": source,
                            "redis_sha1": hashlib.sha1(source).hexdigest(), "source_sha256": plain_sha256(source)})
        document = _canonical_read_text(_canonical_closed_path(root, CANONICAL_DOCUMENT_PATH))
        return document, entries
    except OSError:
        reject("BUNDLE_INVENTORY")


def _canonical_bundle_compute(document: bytes, entries: list[dict[str, object]]) -> dict[str, object]:
    if len(entries) != 43:
        reject("BUNDLE_INVENTORY")
    _canonical_validate_text(document)
    identities = []
    sources = []
    seen_sha1: set[str] = set()
    seen_sha256: set[str] = set()
    source_set = hashlib.sha256(frame("mifolyo:crawl-jobs-v2:lua-source-set:v1") + frame("43"))
    for index, (operation, entry) in enumerate(zip(CANONICAL_LUA_OPERATIONS, entries)):
        name = operation.lower() + ".lua"
        if type(entry["index"]) is not int or entry["index"] != index or entry["operation"] != operation or entry["source_name"] != name:
            reject("BUNDLE_INVENTORY")
        source = entry["source"]
        _canonical_validate_text(source)
        redis_sha1 = hashlib.sha1(source).hexdigest()
        source_sha256 = plain_sha256(source)
        if (entry["redis_sha1"] != redis_sha1 or entry["source_sha256"] != source_sha256
                or redis_sha1 in seen_sha1 or source_sha256 in seen_sha256):
            reject("BUNDLE_IDENTITY_MISMATCH")
        seen_sha1.add(redis_sha1)
        seen_sha256.add(source_sha256)
        for value in (str(index), operation, name, source, redis_sha1, source_sha256):
            source_set.update(frame(value))
        sources.append((name, source))
        identities.append({"index": index, "operation": operation, "source_name": name, "source_bytes": len(source),
                           "redis_sha1": redis_sha1, "source_sha256": source_sha256})
    ordered = sorted(sources, key=lambda source: source[0].encode("ascii"))
    contract = digest_sections("mifolyo:crawl-contract:v2", section("document", [[("document_bytes", document)]]),
                               section("lua", [[("source_name", name), ("source_bytes", source)] for name, source in ordered]))
    source_digest = source_set.hexdigest()
    seal = digest_framed("mifolyo:crawl-jobs-v2:lua-bundle-seal:v1", source_digest, contract)
    return {"entries": identities, "lua_source_order": [name for name, _ in ordered], "source_set_sha256": source_digest,
            "contract_sha256": contract, "bundle_seal_sha256": seal}


def canonical_lua_bundle_result(case_input: dict[str, object]) -> dict[str, object]:
    _canonical_bundle_input(case_input)
    return _canonical_bundle_compute(*_canonical_read_bundle(Path(__file__).resolve().parents[1]))


def run_canonical_lua_bundle_mutation(case_input: dict[str, object], data: dict[str, object]) -> None:
    if case_input["base_case"] != CANONICAL_LUA_CASE:
        raise FixtureError("canonical bundle mutation requires the real bundle case")
    base = next((case for case in data["cases"] if case["name"] == CANONICAL_LUA_CASE), None)
    if base is None or base["kind"] != "canonical_lua_bundle":
        raise FixtureError("canonical bundle case missing")
    _canonical_bundle_input(base["input"])
    document, entries = _canonical_read_bundle(Path(__file__).resolve().parents[1])
    # Expected fixture values are deliberately NOT mutation authority. Recompute
    # a baseline from real repository files and independently check the mutant.
    baseline = _canonical_bundle_compute(document, entries)
    changed = copy.deepcopy(baseline)
    mutation = case_input["mutation"]
    if mutation == "missing_source":
        entries.pop()
    elif mutation == "extra_source":
        entries.append(copy.deepcopy(entries[0]))
    elif mutation == "swapped_sources":
        entries[0]["source"], entries[1]["source"] = entries[1]["source"], entries[0]["source"]
    elif mutation == "duplicate_sources":
        entries[1]["source"] = entries[0]["source"]
    elif mutation == "duplicate_entry":
        entries[1] = copy.deepcopy(entries[0])
    elif mutation == "swapped_entries":
        entries[0], entries[1] = entries[1], entries[0]
    elif mutation == "source_alias_path":
        entries[0]["source_name"] = "../lua/" + entries[0]["source_name"]
    elif mutation == "invalid_utf8":
        entries[0]["source"] = b"\xff" + entries[0]["source"][1:]
    elif mutation == "missing_newline":
        entries[0]["source"] = entries[0]["source"][:-1]
    elif mutation in {"one_byte_drift", "source_resealed"}:
        entries[0]["source"] = bytes([entries[0]["source"][0] ^ 1]) + entries[0]["source"][1:]
        if mutation == "source_resealed":
            entries[0]["redis_sha1"] = hashlib.sha1(entries[0]["source"]).hexdigest()
            entries[0]["source_sha256"] = plain_sha256(entries[0]["source"])
    elif mutation == "stale_redis_sha1":
        entries[0]["redis_sha1"] = "1" * 40
    elif mutation == "stale_source_sha256":
        entries[0]["source_sha256"] = "1" * 64
    elif mutation == "document_one_byte_drift":
        document = bytes([document[0] ^ 1]) + document[1:]
    elif mutation in {"source_set_substitution", "bundle_seal_substitution", "contract_substitution", "contract_resealed"}:
        field = {"source_set_substitution": "source_set_sha256", "bundle_seal_substitution": "bundle_seal_sha256",
                 "contract_substitution": "contract_sha256", "contract_resealed": "contract_sha256"}[mutation]
        changed[field] = "1" * 64
        if mutation == "contract_resealed":
            changed["bundle_seal_sha256"] = digest_framed("mifolyo:crawl-jobs-v2:lua-bundle-seal:v1",
                                                         changed["source_set_sha256"], changed["contract_sha256"])
    else:
        raise FixtureError("unknown canonical bundle mutation: " + str(mutation))
    actual = _canonical_bundle_compute(document, entries)
    if actual != baseline or changed != baseline:
        reject("BUNDLE_IDENTITY_MISMATCH")


def _valid_nonzero_digest(value: object) -> str:
    digest = model_text(value)
    if HEX_64_RE.fullmatch(digest) is None or digest == "0" * 64:
        reject("INVALID_GUARD_RELATION")
    return digest


def _valid_evidence_digest(value: object) -> str:
    digest = model_text(value)
    if HEX_64_RE.fullmatch(digest) is None:
        reject("INVALID_DIGEST")
    return digest


def _validate_image_digest(value: object, *, allow_disabled: bool = False) -> str:
    image = model_text(value)
    if allow_disabled and image == "disabled":
        return image
    if (
        not image.startswith("sha256:")
        or HEX_64_RE.fullmatch(image[7:]) is None
        or image[7:] == "0" * 64
    ):
        reject("INVALID_IMAGE_DIGEST")
    return image


def guard_core_fields(
    case_input: dict[str, object],
    prior_cases: dict[str, dict[str, object]],
) -> list[tuple[str, str]]:
    contract_case = model_text(case_input["contract_case"])
    if contract_case not in prior_cases or "contract_sha256" not in prior_cases[contract_case]:
        raise FixtureError(f"guard references unavailable contract case {contract_case!r}")
    contract_sha = _valid_nonzero_digest(prior_cases[contract_case]["contract_sha256"])
    guard = case_input["guard_core"]
    if type(guard) is not dict:
        raise FixtureError("guard core case internal schema mismatch")
    guard_mode = model_text(case_input["guard_mode"])
    redis_version = model_text(guard["redis_version"])
    if (
        len(redis_version.encode("ascii", "ignore")) > 64
        or re.fullmatch(r"[0-9]+(?:\.[0-9]+){1,3}", redis_version) is None
        or not redis_version.startswith("7.")
    ):
        reject("INVALID_REDIS_VERSION")
    digest_fields: dict[str, str] = {
        "redis_config_sha256": _valid_nonzero_digest(guard["redis_config_sha256"]),
    }
    evidence_names = (
        "maximum_shape_sha256",
        "memory_fixture_sha256",
        "lua_benchmark_sha256",
        "aof_crash_evidence_sha256",
    )
    for field in evidence_names:
        digest_fields[field] = _valid_evidence_digest(guard[field])
    zero_evidence = sum(digest_fields[field] == "0" * 64 for field in evidence_names)
    mode = model_text(guard["cutover_mode"])
    candidate = model_text(guard["candidate_run_id"])
    if guard_mode == "provisional_fixture":
        # Historical non-executable framing/rejection controls, not Redis setup.
        if zero_evidence == 0 or mode != "fresh" or candidate:
            reject("INVALID_GUARD_RELATION")
    elif guard_mode == "production":
        if zero_evidence:
            reject("INVALID_GUARD_RELATION")
        if mode == "fresh":
            if candidate:
                reject("INVALID_GUARD_RELATION")
        elif mode == "v1_migration":
            if HEX_32_RE.fullmatch(candidate) is None:
                reject("INVALID_GUARD_RELATION")
        else:
            reject("INVALID_GUARD_RELATION")
    else:
        raise FixtureError(f"unknown guard mode {guard_mode!r}")
    return [
        ("protocol_version", "2"),
        ("contract_sha256", contract_sha),
        ("redis_version", redis_version),
        ("redis_config_sha256", digest_fields["redis_config_sha256"]),
        ("maximum_shape_sha256", digest_fields["maximum_shape_sha256"]),
        ("memory_fixture_sha256", digest_fields["memory_fixture_sha256"]),
        ("lua_benchmark_sha256", digest_fields["lua_benchmark_sha256"]),
        ("aof_crash_evidence_sha256", digest_fields["aof_crash_evidence_sha256"]),
        ("cutover_mode", mode),
        ("candidate_run_id", candidate),
        ("approved", "1"),
    ]


def guard_core_result(
    case_input: dict[str, object],
    prior_cases: dict[str, dict[str, object]],
) -> dict[str, object]:
    guard_fields = guard_core_fields(case_input, prior_cases)
    guard_bytes = record(guard_fields)
    if len(guard_bytes) > 16 * 1024:
        reject("AUTHORITY_RECORD_LIMIT")
    guard_digest = plain_sha256(guard_bytes)
    return {"guard_core_sha256": guard_digest, "record_hex": guard_bytes.hex()}


def guard_chain_result(
    case_input: dict[str, object],
    prior_cases: dict[str, dict[str, object]],
) -> dict[str, object]:
    if model_text(case_input["guard_mode"]) != "production":
        raise FixtureError("stored guard chains require production mode")
    guard_fields = guard_core_fields(case_input, prior_cases)
    guard_bytes = record(guard_fields)
    if len(guard_bytes) > 16 * 1024:
        reject("AUTHORITY_RECORD_LIMIT")
    guard_digest = plain_sha256(guard_bytes)
    compatibility = case_input["compatibility"]
    if type(compatibility) is not dict:
        raise FixtureError("guard compatibility case internal schema mismatch")
    digest_fields = dict(guard_fields)
    compatibility_redis = _valid_nonzero_digest(compatibility["redis_config_sha256"])
    if compatibility_redis != digest_fields["redis_config_sha256"]:
        reject("GUARD_CONFIG_MISMATCH")
    image_fields = [
        ("spider_image", _validate_image_digest(compatibility["spider_image"])),
        ("seed_importer_image", _validate_image_digest(compatibility["seed_importer_image"])),
        ("crawl_admin_image", _validate_image_digest(compatibility["crawl_admin_image"])),
        ("indexer_image", _validate_image_digest(compatibility["indexer_image"])),
        ("image_indexer_image", _validate_image_digest(compatibility["image_indexer_image"])),
        ("backlinks_processor_image", _validate_image_digest(compatibility["backlinks_processor_image"])),
        ("monitoring_image", _validate_image_digest(compatibility["monitoring_image"])),
        ("render_worker_image", _validate_image_digest(compatibility["render_worker_image"], allow_disabled=True)),
    ]
    compatibility_fields = [
        ("manifest_version", "1"),
        ("crawl_jobs", "2"),
        ("crawl_policy", "2"),
        ("canonicalization", "1"),
        ("page_publication", "1"),
        ("image_manifest", "1"),
        ("backlink_projection", "1"),
        ("render_ipc", "2"),
        ("signal_queue", "retired"),
        ("global_request_concurrency", "2"),
        ("redis_config_sha256", compatibility_redis),
        ("commit_guard_sha256", guard_digest),
        *image_fields,
    ]
    compatibility_bytes = record(compatibility_fields)
    if len(compatibility_bytes) > 16 * 1024:
        reject("AUTHORITY_RECORD_LIMIT")
    manifest_digest = plain_sha256(compatibility_bytes)
    marker_fields = [compatibility_fields[0], ("manifest_sha256", manifest_digest), *compatibility_fields[1:]]
    marker_bytes = record(marker_fields)
    approved_at = require_exact_integer(case_input["approved_at_ms"], positive=True)
    stored_fields = [
        guard_fields[0],
        guard_fields[1],
        ("compatibility_manifest_sha256", manifest_digest),
        *guard_fields[2:10],
        ("approved_at_ms", str(approved_at)),
        guard_fields[10],
    ]
    stored_bytes = record(stored_fields)
    reconstructed = [stored_fields[0], stored_fields[1], *stored_fields[3:11], stored_fields[12]]
    if record(reconstructed) != guard_bytes:
        raise FixtureError("stored guard does not reconstruct the exact guard core")
    if stored_fields[2][1] != manifest_digest or compatibility_fields[11][1] != guard_digest:
        raise FixtureError("acyclic guard/compatibility relation is broken")
    return {
        "guard_core_sha256": guard_digest,
        "compatibility_manifest_sha256": manifest_digest,
        "compatibility_marker_sha256": plain_sha256(marker_bytes),
        "stored_guard_sha256": plain_sha256(stored_bytes),
    }


def transition_mutation_result(case_input: dict[str, object], data: dict[str, object]) -> dict[str, object]:
    identities = data["identities"]
    reasons = data["transition_reasons"]
    if type(identities) is not dict or type(reasons) is not dict:
        raise FixtureError("baseline transition data shape mismatch")
    base_reason = model_text(reasons["retry"])
    changed_reason = model_text(case_input["changed_reason"])
    validate_transition_reason("CJ2_RETRY", base_reason)
    validate_transition_reason("CJ2_RETRY", changed_reason)
    base_payload = [("owner_id", model_text(identities["owner_id"]))]
    changed_payload = [("owner_id", model_text(case_input["changed_payload_owner_id"]))]
    base_payload_digest = transition_payload_digest(base_payload)
    changed_payload_digest = transition_payload_digest(changed_payload)
    base_id = transition_id(data, "CJ2_RETRY", base_reason, base_payload, True)
    changed_reason_id = transition_id(data, "CJ2_RETRY", changed_reason, base_payload, True)
    changed_payload_id = transition_id(data, "CJ2_RETRY", base_reason, changed_payload, True)
    if len({base_id, changed_reason_id, changed_payload_id}) != 3 or base_payload_digest == changed_payload_digest:
        raise FixtureError("transition mutation did not change every bound identity")
    for submitted in (changed_reason_id, changed_payload_id):
        try:
            verify_replay(base_id, submitted)
        except Rejection as error:
            if error.rejection_class != "IMMUTABLE_MISMATCH":
                raise FixtureError(f"unexpected replay class {error.rejection_class}") from error
        else:
            raise FixtureError("changed transition replay was accepted")
    return {
        "base_payload_digest": base_payload_digest,
        "changed_payload_digest": changed_payload_digest,
        "base_transition_id": base_id,
        "changed_reason_transition_id": changed_reason_id,
        "changed_payload_transition_id": changed_payload_id,
        "replay_rejection_class": "IMMUTABLE_MISMATCH",
    }


def projection_for_publication(
    profile: dict[str, object],
    publication_id: str,
) -> tuple[list[tuple[str, bytes | str]], list[list[tuple[str, bytes | str]]], list[tuple[str, bytes | str]]]:
    records = profile["records"]
    if type(records) is not tuple:
        raise FixtureError("profile records missing")
    page, _outlinks, images, _discoveries, _aliases = records
    final_page = [*page[0], ("publication_id", publication_id)]
    page_url = model_text(page[0][0][1])
    payloads = [
        [
            ("contract_version", "1"),
            ("publication_id", publication_id),
            ("normalized_page_url", page_url),
            ("normalized_source_url", item[0][1]),
            ("alt", item[1][1]),
        ]
        for item in images
    ]
    keys = [image_data_key(publication_id, page_url, model_text(item[0][1])) for item in images]
    manifest = [
        ("contract_version", "1"),
        ("publication_id", publication_id),
        ("normalized_url", page_url),
        ("image_count", str(len(keys))),
        ("image_keys", json.dumps(keys, ensure_ascii=True, separators=(",", ":"))),
    ]
    return final_page, payloads, manifest


def digest_projection_semantics(
    profile: dict[str, object],
    final_page: list[tuple[str, bytes | str]],
    payloads: list[list[tuple[str, bytes | str]]],
    manifest: list[tuple[str, bytes | str]],
) -> str:
    records = profile["records"]
    if type(records) is not tuple:
        raise FixtureError("profile records missing")
    _page, outlinks, images, discoveries, aliases = records
    if tuple(name for name, _ in final_page) != (
        "normalized_url", "html", "original_html", "content_type", "status_code", "last_crawled",
        "rendered", "render_policy_rule", "render_policy_sha256", "publication_id",
    ):
        reject("INVALID_PROJECTION")
    publication_id = model_text(final_page[9][1])
    if HEX_64_RE.fullmatch(publication_id) is None:
        reject("INVALID_PUBLICATION_ID")
    if tuple(name for name, _ in manifest) != (
        "contract_version", "publication_id", "normalized_url", "image_count", "image_keys",
    ) or model_text(manifest[1][1]) != publication_id:
        reject("INVALID_PROJECTION")
    for payload in payloads:
        if tuple(name for name, _ in payload) != (
            "contract_version", "publication_id", "normalized_page_url", "normalized_source_url", "alt",
        ) or model_text(payload[1][1]) != publication_id:
            reject("INVALID_PROJECTION")
    if len(payloads) != len(images):
        reject("INVALID_PROJECTION")
    semantic_page = [final_page[:9]]
    return output_digest_from_records((semantic_page, outlinks, images, discoveries, aliases))


def publication_independence_result(case_input: dict[str, object], data: dict[str, object]) -> dict[str, object]:
    profile = output_profile_result("baseline", data)
    expected = profile["expected"]
    if type(expected) is not dict:
        raise FixtureError("baseline profile expected result missing")
    publication_a = model_text(expected["publication_id"])
    publication_b = model_text(case_input["alternate_publication_id"])
    if publication_a == publication_b:
        raise FixtureError("alternate publication ID is not different")
    projection_a = projection_for_publication(profile, publication_a)
    projection_b = projection_for_publication(profile, publication_b)
    digest_a = digest_projection_semantics(profile, *projection_a)
    digest_b = digest_projection_semantics(profile, *projection_b)
    if digest_a != digest_b:
        raise FixtureError("publication-derived fields entered the output digest")
    fingerprint_a = plain_sha256(b"".join([record(projection_a[0]), *(record(item) for item in projection_a[1]), record(projection_a[2])]))
    fingerprint_b = plain_sha256(b"".join([record(projection_b[0]), *(record(item) for item in projection_b[1]), record(projection_b[2])]))
    if fingerprint_a == fingerprint_b:
        raise FixtureError("publication projection mutation did not change derived records")
    return {
        "output_digest": digest_a,
        "projection_a_sha256": fingerprint_a,
        "projection_b_sha256": fingerprint_b,
    }


def field_limits_result(profile_name: str, data: dict[str, object], generator: object) -> dict[str, object]:
    if profile_name != "maximum":
        raise FixtureError(f"unknown field-limit profile {profile_name!r}")
    profile = output_profile_result("maximum", data, generator)
    generated = profile["data"]
    records = profile["records"]
    chunks = profile["chunks"]
    expected = profile["expected"]
    if type(generated) is not dict or type(records) is not tuple or type(chunks) is not dict or type(expected) is not dict:
        raise FixtureError("maximum profile internals missing")
    page, _outlinks, images, _discoveries, _aliases = records
    page_record = record(page[0])
    publication = model_text(expected["publication_id"])
    final_image = [
        ("contract_version", "1"),
        ("publication_id", publication),
        ("normalized_page_url", page[0][0][1]),
        ("normalized_source_url", images[0][0][1]),
        ("alt", images[0][1][1]),
    ]
    source_jobs = generated["source_jobs"]
    if type(source_jobs) is not list:
        raise FixtureError("maximum source jobs missing")
    source_record = source_job_fields(source_jobs[0], generated)
    manifest_chunk = chunks["image_manifest"]
    if type(manifest_chunk) is not list:
        raise FixtureError("maximum manifest chunk missing")
    manifest_record = manifest_chunk[0][0]
    output = generated["output"]
    if type(output) is not dict or type(output["page"]) is not dict or type(output["images"]) is not list:
        raise FixtureError("maximum output missing")
    page_input = output["page"]
    first_image = output["images"][0]
    if type(first_image) is not dict:
        raise FixtureError("maximum image missing")
    lengths = {
        "canonical_url_bytes": len(model_text(page[0][0][1]).encode("utf-8")),
        "html_bytes": len(model_text(page_input["html"]).encode("utf-8")),
        "original_html_bytes": len(model_text(page_input["original_html"]).encode("utf-8")),
        "combined_html_bytes": len(model_text(page_input["html"]).encode("utf-8")) + len(model_text(page_input["original_html"]).encode("utf-8")),
        "content_type_bytes": len(model_text(page_input["content_type"]).encode("utf-8")),
        "render_policy_rule_bytes": len(model_text(page_input["render_policy_rule"]).encode("utf-8")),
        "image_alt_bytes": len(model_text(first_image["alt"]).encode("utf-8")),
        "group_id_bytes": len(model_text(source_jobs[0]["group_id"]).encode("utf-8")),
        "outlink_chunk_records": len(chunks["outlinks"][0]),
        "discovery_chunk_records": len(chunks["discoveries"][0]),
        "alias_records": len(chunks["aliases"][0]),
        "image_chunk_records": len(chunks["images"][0]),
        "image_manifest_bytes": len(model_text(manifest_record[4][1]).encode("utf-8")),
        "page_record_bytes": len(page_record),
        "final_image_record_bytes": len(record(final_image)),
        "source_record_bytes": len(record(source_record)),
    }
    return {
        "lengths": lengths,
        "record_sha256": {
            "page": plain_sha256(page_record),
            "final_image": plain_sha256(record(final_image)),
            "source": plain_sha256(record(source_record)),
            "image_manifest": plain_sha256(record(manifest_record)),
        },
    }


def transcript_binding_result(case_input: dict[str, object], data: dict[str, object]) -> dict[str, object]:
    changed = copy.deepcopy(data)
    changed["output_context"] = copy.deepcopy(case_input["output_context"])
    _documents, witness = output_transcript(changed)
    baseline = compute_baseline(changed)
    expected = expect_object(data["expected"], "expected")
    for field in ("output_digest", "publication_id"):
        if baseline[field] != expected[field]:
            raise FixtureError(f"transcript-only binding changed {field}")
    return {
        **{field: baseline[field] for field in ("output_digest", "publication_id", "commit_id", "chunk_digest")},
        "terminal_witness": witness,
    }


def run_transcript_binding_mutation(case_input: dict[str, object], data: dict[str, object]) -> None:
    base_name = model_text(case_input["base_case"])
    base = next((case for case in data["cases"] if case["name"] == base_name), None)
    if base is None or base["kind"] != "transcript_binding":
        raise FixtureError(f"unknown transcript binding case {base_name!r}")
    changed = copy.deepcopy(data)
    context = copy.deepcopy(base["input"]["output_context"])
    changed["output_context"] = context
    _documents, witness = output_transcript(changed)
    requests = context["requests"]
    mutation = model_text(case_input["mutation"])
    if mutation == "omit_leading":
        del requests[0]
    elif mutation == "omit_middle":
        del requests[2]
    elif mutation == "omit_trailing":
        del requests[-1]
    elif mutation == "duplicate_start_count":
        requests[2]["job_request_starts"] = requests[1]["job_request_starts"]
    elif mutation == "skipped_start_count":
        requests[2]["job_request_starts"] = "7"
    elif mutation == "duplicate_ordinal":
        requests[-1]["request_ordinal"] = requests[-2]["request_ordinal"]
    elif mutation == "start_count_exceeds_ordinal":
        requests[0]["request_ordinal"] = "3"
    elif mutation == "baseline_equal_generation":
        context["lease_request_starts_baseline"] = context["terminal_request_starts_generation"]
    elif mutation == "baseline_negative":
        context["lease_request_starts_baseline"] = "-1"
    elif mutation == "baseline_noncanonical":
        context["lease_request_starts_baseline"] = "03"
    elif mutation == "generation_one_over":
        context["terminal_request_starts_generation"] = "11"
    elif mutation == "generation_noncanonical":
        context["terminal_request_starts_generation"] = "07"
    elif mutation == "count_noncanonical":
        requests[0]["job_request_starts"] = "04"
    elif mutation == "ordinal_noncanonical":
        requests[0]["request_ordinal"] = "07"
    elif mutation == "ordinal_one_over":
        requests[-1]["request_ordinal"] = "101"
    elif mutation == "old_projection":
        witness = witness[:7]
    elif mutation == "witness_lease_mismatch":
        witness[9] = model_text(changed["identities"]["alternate_owner_id"])
    elif mutation == "witness_active_reservation":
        witness[12] = "a" * 64
    elif mutation == "witness_document_timestamp":
        witness[0] = str(int(witness[0]) - 1)
    elif mutation in {"stale_generation_binding", "stale_baseline_binding"}:
        output_digest = output_digest_from_records(output_records(changed))
        _publication, commit = publication_and_commit(changed, output_digest)
        field = (
            "terminal_request_starts_generation" if mutation == "stale_generation_binding"
            else "lease_request_starts_baseline"
        )
        context[field] = str(int(context[field]) - 1)
        _publication, stale_commit = publication_and_commit(changed, output_digest)
        verify_replay(commit, stale_commit)
        return
    else:
        raise FixtureError(f"unknown transcript binding mutation {mutation!r}")
    output_transcript(changed, witness)


def evaluate_cases(cases: list[object], data: dict[str, object], root: Path) -> dict[str, dict[str, object]]:
    results: dict[str, dict[str, object]] = {}
    for raw_case in cases:
        case = expect_object(raw_case, "case")
        name = model_text(case["name"])
        kind = model_text(case["kind"])
        case_input = case["input"]
        if type(case_input) is not dict:
            raise FixtureError(f"case {name!r} input is not an object")
        if kind == "u64_max":
            value = parse_canonical_decimal(model_text(case_input["decimal"]))
            actual = {"u64_hex": u64(value).hex(), "frame_length_hex": u64(value).hex()}
        elif kind == "empty_section":
            encoded = section(model_text(case_input["label"]), [])
            actual = {"section_hex": encoded.hex(), "section_sha256": plain_sha256(encoded)}
        elif kind == "valid_score":
            parsed = validate_redis_score(case_input["score_text"], case_input["redis_value"])
            actual = {"binary64_hex": struct.pack(">d", parsed).hex()}
        elif kind == "utf8_order":
            values = case_input["values"]
            if type(values) is not list:
                raise FixtureError(f"case {name!r} values are not an array")
            ordered = sorted((model_text(value) for value in values), key=lambda value: value.encode("utf-8"))
            encoded = section("values", [[("value", value)] for value in ordered])
            actual = {"ordered_values": ordered, "section_sha256": plain_sha256(encoded)}
        elif kind == "contract_digest":
            actual = contract_case_result(case_input, root)
        elif kind == "canonical_lua_bundle":
            actual = canonical_lua_bundle_result(case_input)
        elif kind == "policy_group_boundary":
            groups = generated_policy_groups_boundary(case_input)
            group_records = validate_policy_groups(groups)
            actual = {
                "count": len(groups),
                "policy_group_map_sha256": digest_sections(
                    "mifolyo:policy-group-map:v2", section("groups", group_records)
                ),
            }
        elif kind == "guard_chain":
            actual = guard_chain_result(case_input, results)
        elif kind == "guard_core":
            actual = guard_core_result(case_input, results)
        elif kind == "transition_mutation":
            actual = transition_mutation_result(case_input, data)
        elif kind == "publication_independence":
            actual = publication_independence_result(case_input, data)
        elif kind == "output_profile":
            profile = model_text(case_input["profile"])
            profile_data = output_profile_result(profile, data, case_input.get("generator"))
            actual_value = profile_data["expected"]
            if type(actual_value) is not dict:
                raise FixtureError(f"case {name!r} profile result missing")
            actual = actual_value
        elif kind == "transcript_binding":
            actual = transcript_binding_result(case_input, data)
        elif kind == "source_profile":
            actual = source_profile_result(model_text(case_input["profile"]), case_input.get("generator"))
        elif kind == "stage_chunks":
            profile = output_profile_result(model_text(case_input["profile"]), data)
            profile_expected = profile["expected"]
            if type(profile_expected) is not dict or type(profile_expected["chunk_digests"]) is not dict:
                raise FixtureError(f"case {name!r} chunk result missing")
            actual = {"chunk_digests": profile_expected["chunk_digests"]}
        elif kind == "field_limits":
            actual = field_limits_result(model_text(case_input["profile"]), data, case_input["generator"])
        else:
            raise FixtureError(f"case {name!r}: unknown positive kind {kind!r}")
        results[name] = actual
    return results


OUTPUT_MUTATIONS = {
    "outlinks_one_over",
    "discoveries_one_over",
    "images_one_over",
    "duplicate_outlink",
    "duplicate_image",
    "duplicate_discovery",
    "html_one_over",
    "original_html_one_over",
    "alt_one_over",
    "content_type_empty",
    "content_type_one_over",
    "content_type_untrimmed",
    "content_type_non_html",
    "content_type_extra_parameter",
    "content_type_wrong_charset",
    "render_false_with_original",
    "render_false_with_rule",
    "render_false_with_digest",
    "render_true_without_original",
    "render_true_without_rule",
    "render_true_rule_one_over",
    "render_true_rule_control",
    "render_true_bad_digest",
    "status_below",
    "status_above",
    "url_one_over",
    "group_id_one_over",
    "target_id_mismatch",
}


def run_output_mutation(mutation: str, data: dict[str, object]) -> None:
    if mutation not in OUTPUT_MUTATIONS:
        raise FixtureError(f"unknown output mutation {mutation!r}")
    changed = copy.deepcopy(data)
    output = changed["output"]
    identities = changed["identities"]
    if type(output) is not dict or type(identities) is not dict or type(output["page"]) is not dict:
        raise FixtureError("baseline output shape mismatch")
    page = output["page"]
    render_digest: str | None = None
    render_rules: set[str] | None = None
    if mutation == "outlinks_one_over":
        output["outlinks"] = [f"https://overflow.example.com/{index:03d}" for index in range(MAX_OUTLINKS + 1)]
    elif mutation == "discoveries_one_over":
        output["discoveries"] = [copy.deepcopy(output["discoveries"][0]) for _ in range(MAX_DISCOVERIES + 1)]  # type: ignore[index]
    elif mutation == "images_one_over":
        output["images"] = [
            {"normalized_source_url": f"https://images.example.com/{index:03d}", "alt": ""}
            for index in range(MAX_IMAGES + 1)
        ]
    elif mutation == "duplicate_outlink":
        output["outlinks"].append(output["outlinks"][0])  # type: ignore[union-attr,index]
    elif mutation == "duplicate_image":
        output["images"].append(copy.deepcopy(output["images"][0]))  # type: ignore[union-attr,index]
    elif mutation == "duplicate_discovery":
        output["discoveries"].append(copy.deepcopy(output["discoveries"][0]))  # type: ignore[union-attr,index]
    elif mutation == "html_one_over":
        page["html"] = "H" * (MAX_PAGE_BLOB_BYTES + 1)
    elif mutation == "original_html_one_over":
        page["original_html"] = "O" * (MAX_PAGE_BLOB_BYTES + 1)
    elif mutation == "alt_one_over":
        output["images"][0]["alt"] = "a" * (MAX_IMAGE_ALT_BYTES + 1)  # type: ignore[index]
    elif mutation == "content_type_empty":
        page["content_type"] = ""
    elif mutation == "content_type_one_over":
        page["content_type"] = "text/html;" + " " * (MAX_CONTENT_TYPE_BYTES + 1)
    elif mutation == "content_type_untrimmed":
        page["content_type"] = " text/html"
    elif mutation == "content_type_non_html":
        page["content_type"] = "application/xhtml+xml"
    elif mutation == "content_type_extra_parameter":
        page["content_type"] = "text/html;charset=utf-8;level=1"
    elif mutation == "content_type_wrong_charset":
        page["content_type"] = "text/html;charset=iso-8859-1"
    elif mutation in {
        "render_false_with_original",
        "render_false_with_rule",
        "render_false_with_digest",
    }:
        if mutation == "render_false_with_original":
            page["original_html"] = "source"
        elif mutation == "render_false_with_rule":
            page["render_policy_rule"] = "render-main"
        else:
            page["render_policy_sha256"] = model_text(identities["crawl_policy_sha256"])
    elif mutation in {
        "render_true_without_original",
        "render_true_without_rule",
        "render_true_rule_one_over",
        "render_true_rule_control",
        "render_true_bad_digest",
    }:
        page["rendered"] = True
        page["original_html"] = "source"
        page["render_policy_rule"] = "render-main"
        page["render_policy_sha256"] = model_text(identities["crawl_policy_sha256"])
        render_digest = model_text(identities["crawl_policy_sha256"])
        render_rules = {"render-main"}
        if mutation == "render_true_without_original":
            page["original_html"] = ""
        elif mutation == "render_true_without_rule":
            page["render_policy_rule"] = ""
        elif mutation == "render_true_rule_one_over":
            page["render_policy_rule"] = "r" * (MAX_RENDER_RULE_BYTES + 1)
        elif mutation == "render_true_rule_control":
            page["render_policy_rule"] = "render\nmain"
        elif mutation == "render_true_bad_digest":
            page["render_policy_sha256"] = "z" * 64
    elif mutation == "status_below":
        page["status_code"] = 99
    elif mutation == "status_above":
        page["status_code"] = 400
    elif mutation == "url_one_over":
        output["outlinks"] = [sized_url("too-long.example.com", "url", 0, MAX_CANONICAL_URL_BYTES + 1)]
    elif mutation == "group_id_one_over":
        discovery = output["discoveries"][0]  # type: ignore[index]
        if type(discovery) is not dict or type(changed["policy_decisions"]) is not dict:
            raise FixtureError("baseline discovery shape mismatch")
        discovery["group_id"] = "g" * (MAX_GROUP_ID_BYTES + 1)
        decision = changed["policy_decisions"][discovery["decision"]]
        if type(decision) is not dict:
            raise FixtureError("baseline decision shape mismatch")
        decision["group_id"] = discovery["group_id"]
    elif mutation == "target_id_mismatch":
        if type(changed["targets"]) is not dict or type(changed["targets"]["page"]) is not dict:
            raise FixtureError("baseline target shape mismatch")
        changed["targets"]["page"]["url_id"] = "f" * 64
    output_records(
        changed,
        authorized_render_policy_sha256=render_digest,
        enabled_render_rules=render_rules,
    )


def run_stage_mutation(mutation: str, data: dict[str, object]) -> None:
    baseline = output_profile_result("baseline", data)
    expected = baseline["expected"]
    records = baseline["records"]
    if type(expected) is not dict or type(records) is not tuple:
        raise FixtureError("baseline profile missing")
    commit = model_text(expected["commit_id"])
    if mutation == "unknown_kind":
        derive_chunk_digest(commit, "generic", 0, [])
    elif mutation == "outlinks_65_records":
        chunk_records = [[("target_url", f"https://chunks.example.com/{index:03d}")] for index in range(65)]
        derive_chunk_digest(commit, "outlinks", 0, chunk_records)
    elif mutation == "outlinks_empty":
        derive_chunk_digest(commit, "outlinks", 0, [])
    elif mutation == "discoveries_65_records":
        complete = complete_discovery_records(data)
        derive_chunk_digest(commit, "discoveries", 0, [complete[0] for _ in range(65)])
    elif mutation == "aliases_duplicate":
        alias = records[4][0]
        derive_chunk_digest(commit, "aliases", 0, [alias, copy.deepcopy(alias)])
    elif mutation == "image_manifest_one_over":
        manifest = [
            ("contract_version", "1"),
            ("publication_id", model_text(expected["publication_id"])),
            ("normalized_url", model_text(records[0][0][0][1])),
            ("image_count", "0"),
            ("image_keys", "[" + " " * MAX_IMAGE_MANIFEST_BYTES + "]"),
        ]
        derive_chunk_digest(commit, "image_manifest", 0, [manifest])
    elif mutation == "blob_one_over":
        derive_chunk_digest(
            commit,
            "html",
            0,
            [[("field_name", "html"), ("field_bytes", b"H" * (MAX_PAGE_BLOB_BYTES + 1))]],
        )
    else:
        raise FixtureError(f"unknown stage chunk mutation {mutation!r}")


def fixture_positive_case(data: dict[str, object], name: str) -> dict[str, object]:
    cases = data.get("cases")
    if type(cases) is not list:
        raise FixtureError("positive fixture case inventory is unavailable")
    for value in cases:
        if type(value) is dict and value.get("name") == name:
            return value
    raise FixtureError(f"unknown positive fixture case {name!r}")


def run_policy_group_binding_mutation(mutation: str, data: dict[str, object]) -> None:
    changed = copy.deepcopy(data)
    groups = changed.get("policy_groups")
    decisions = changed.get("policy_decisions")
    targets = changed.get("targets")
    if type(groups) is not list or type(decisions) is not dict or type(targets) is not dict:
        raise FixtureError("baseline policy binding shape mismatch")
    decision = decisions.get("page_document")
    if type(decision) is not dict:
        raise FixtureError("baseline page decision is unavailable")
    if mutation == "missing_group":
        changed["policy_groups"] = [
            group for group in groups
            if type(group) is dict and group.get("group_id") != decision.get("group_id")
        ]
        validate_decision_policy_group_binding(decision, targets, changed["policy_groups"])
        return
    elif mutation == "wrong_rate_lineage":
        alternate = next(
            (
                group for group in groups
                if type(group) is dict and group.get("group_id") != decision.get("group_id")
            ),
            None,
        )
        if type(alternate) is not dict:
            raise FixtureError("baseline alternate rate lineage is unavailable")
        sources = changed.get("source_jobs")
        if type(sources) is not list or not sources or type(sources[0]) is not dict:
            raise FixtureError("baseline source policy surface is unavailable")
        source = sources[0]
        source_decision = decisions.get(source.get("decision"))
        if type(source_decision) is not dict:
            raise FixtureError("baseline source decision is unavailable")
        source["rate_scope_id"] = alternate["rate_scope_id"]
        source_decision["rate_scope_id"] = alternate["rate_scope_id"]
        source_job_fields(source, changed)
        return
    elif mutation == "changed_group_tuple":
        matching = next(
            (group for group in groups if type(group) is dict and group.get("group_id") == decision.get("group_id")),
            None,
        )
        if type(matching) is not dict:
            raise FixtureError("baseline matching policy group is unavailable")
        matching["concurrency"] = model_integer(matching["concurrency"]) - 1
        output = changed.get("output")
        if type(output) is not dict or type(output.get("discoveries")) is not list or not output["discoveries"]:
            raise FixtureError("baseline discovery policy surface is unavailable")
        discovery = output["discoveries"][0]
        if type(discovery) is not dict:
            raise FixtureError("baseline discovery policy surface is invalid")
        source_job_fields(discovery, changed)
        return
    elif mutation == "unequal_group_origin_tuple":
        validate_decision_policy_group_binding(
            decision, targets, groups,
            origin_concurrency=model_integer(decision["concurrency"]) - 1,
            origin_interval_ms=model_integer(decision["interval_ms"]),
        )
        return
    elif mutation == "unrelated_group_map":
        changed["policy_groups"] = [
            {
                "group_id": "unrelated-policy-group",
                "rate_scope_id": "9" * 32,
                "request_start_limit": 1,
                "concurrency": 1,
                "interval_ms": 0,
            }
        ]
        unrelated = changed["policy_groups"]
        sources = changed.get("source_jobs")
        output = changed.get("output")
        if (
            type(sources) is not list or not sources or type(sources[0]) is not dict
            or type(output) is not dict or type(output.get("discoveries")) is not list
            or not output["discoveries"] or type(output["discoveries"][0]) is not dict
        ):
            raise FixtureError("baseline policy surfaces are unavailable")
        checks = (
            lambda: validate_decision_policy_group_binding(decision, targets, unrelated),
            lambda: source_job_fields(sources[0], changed),
            lambda: source_job_fields(output["discoveries"][0], changed),
        )
        for index, check in enumerate(checks):
            try:
                check()
            except Rejection as error:
                if error.rejection_class != "POLICY_GROUP_BINDING_MISMATCH":
                    raise FixtureError(
                        f"unrelated policy-group surface {index} returned {error.rejection_class}"
                    ) from error
            else:
                raise FixtureError(f"unrelated policy-group surface {index} was accepted")
        reject("POLICY_GROUP_BINDING_MISMATCH")
    else:
        raise FixtureError(f"unknown policy-group binding mutation {mutation!r}")


def run_guard_core_mutation(
    base_case: str,
    mutation: str,
    data: dict[str, object],
    positive_results: dict[str, dict[str, object]],
) -> None:
    base = fixture_positive_case(data, base_case)
    base_input = base.get("input")
    if type(base_input) is not dict or base.get("kind") not in {"guard_core", "guard_chain"}:
        raise FixtureError(f"guard mutation base {base_case!r} is invalid")
    changed = copy.deepcopy(base_input)
    guard = changed.get("guard_core")
    if type(guard) is not dict:
        raise FixtureError("guard mutation base core is invalid")
    prior = copy.deepcopy(positive_results)
    if mutation == "zero_contract":
        contract_case = model_text(changed["contract_case"])
        if contract_case not in prior:
            raise FixtureError("guard mutation contract result is unavailable")
        prior[contract_case]["contract_sha256"] = "0" * 64
    elif mutation == "zero_redis_config":
        guard["redis_config_sha256"] = "0" * 64
    elif mutation == "all_nonzero_provisional":
        guard.update(
            {
                "maximum_shape_sha256": "2" * 64,
                "memory_fixture_sha256": "3" * 64,
                "lua_benchmark_sha256": "4" * 64,
                "aof_crash_evidence_sha256": "5" * 64,
            }
        )
    elif mutation == "migration_provisional":
        guard["cutover_mode"] = "v1_migration"
        guard["candidate_run_id"] = "a" * 32
    elif mutation == "production_zero_evidence":
        guard["maximum_shape_sha256"] = "0" * 64
    elif mutation == "redis_6":
        guard["redis_version"] = "6.2.0"
    elif mutation == "redis_8":
        guard["redis_version"] = "8.0.0"
    else:
        raise FixtureError(f"unknown guard-core mutation {mutation!r}")
    record(guard_core_fields(changed, prior))


def run_guard_chain_mutation(
    base_case: str,
    mutation: str,
    data: dict[str, object],
    positive_results: dict[str, dict[str, object]],
) -> None:
    base = fixture_positive_case(data, base_case)
    base_input = base.get("input")
    if type(base_input) is not dict or base.get("kind") != "guard_chain":
        raise FixtureError(f"guard-chain mutation base {base_case!r} is invalid")
    changed = copy.deepcopy(base_input)
    compatibility = changed.get("compatibility")
    if type(compatibility) is not dict:
        raise FixtureError("guard-chain compatibility input is invalid")
    if mutation == "redis_config_disagreement":
        compatibility["redis_config_sha256"] = "f" * 64
    elif mutation == "zero_image_digest":
        compatibility["spider_image"] = "sha256:" + "0" * 64
    else:
        raise FixtureError(f"unknown guard-chain mutation {mutation!r}")
    guard_chain_result(changed, positive_results)


def run_negative_case(
    case: dict[str, object],
    data: dict[str, object],
    positive_results: dict[str, dict[str, object]],
) -> None:
    name = model_text(case["name"])
    kind = model_text(case["kind"])
    case_input = case["input"]
    if type(case_input) is not dict:
        raise FixtureError(f"negative case {name!r} input is not an object")
    expected = model_text(case["expected_rejection_class"])
    try:
        if kind == "transition_reason":
            validate_transition_reason(model_text(case_input["operation"]), model_text(case_input["value"]))
        elif kind == "source_policy_request_kind":
            changed = copy.deepcopy(data)
            source = changed["source_jobs"][0]  # type: ignore[index]
            decision = changed["policy_decisions"][source["decision"]]  # type: ignore[index]
            decision["request_kind"] = case_input["value"]  # type: ignore[index]
            source_job_fields(source, changed)  # type: ignore[arg-type]
        elif kind == "policy_group_boundary":
            groups = generated_policy_groups_boundary(case_input)
            validate_policy_groups(groups)
        elif kind == "group_id":
            validate_group_id(case_input["value"])
        elif kind == "policy_group_binding":
            run_policy_group_binding_mutation(model_text(case_input["mutation"]), data)
        elif kind == "guard_core_mutation":
            run_guard_core_mutation(
                model_text(case_input["base_case"]), model_text(case_input["mutation"]),
                data, positive_results,
            )
        elif kind == "guard_chain_mutation":
            run_guard_chain_mutation(
                model_text(case_input["base_case"]), model_text(case_input["mutation"]),
                data, positive_results,
            )
        elif kind == "discovery_depth":
            changed = copy.deepcopy(data)
            discovery = changed["output"]["discoveries"][0]  # type: ignore[index]
            discovery["depth"] = case_input["value"]
            output_records(changed)
        elif kind == "output_normalized_target":
            changed = copy.deepcopy(data)
            changed["output"]["page"]["normalized_target"] = case_input["value"]  # type: ignore[index]
            output_records(changed)
        elif kind == "score_text":
            validate_score_text(case_input["value"])
        elif kind == "lease_token":
            value = model_text(case_input["value"])
            if HEX_64_RE.fullmatch(value) is None:
                reject("INVALID_LEASE_TOKEN")
        elif kind == "redis_score":
            validate_redis_score(case_input["score_text"], case_input["redis_value"])
        elif kind == "canonical_url":
            # This existing vector kind includes request-origin IP rejection,
            # exactly like Go fixtureCanonicalURLRejection. Output identities
            # are covered separately; no fixture expectations/names change.
            canonical_origin_v1(case_input["value"])
        elif kind == "reservation_target":
            changed = copy.deepcopy(data["reservation"])
            changed["target"] = case_input["value"]  # type: ignore[index]
            reservation_id(data, changed)  # type: ignore[arg-type]
        elif kind == "output_mutation":
            run_output_mutation(model_text(case_input["mutation"]), data)
        elif kind == "canonical_lua_bundle_mutation":
            run_canonical_lua_bundle_mutation(case_input, data)
        elif kind == "transcript_binding_mutation":
            run_transcript_binding_mutation(case_input, data)
        elif kind == "source_shape":
            count = model_integer(case_input["count"])
            if count > MAX_SOURCE_JOBS:
                reject("SOURCE_COUNT_LIMIT")
        elif kind == "section_shape":
            section_name = model_text(case_input["section"])
            count = model_integer(case_input["count"])
            if section_name == "page" and count != 1:
                reject("PAGE_SECTION_SHAPE")
            if section_name == "outlinks" and not 0 <= count <= MAX_OUTLINKS:
                reject("OUTPUT_COUNT_LIMIT")
            if section_name == "images" and not 0 <= count <= MAX_IMAGES:
                reject("OUTPUT_COUNT_LIMIT")
            if section_name == "discoveries" and not 0 <= count <= MAX_DISCOVERIES:
                reject("OUTPUT_COUNT_LIMIT")
            if section_name == "aliases" and not 1 <= count <= MAX_ALIASES:
                reject("ALIAS_COUNT_LIMIT")
            if section_name not in OUTPUT_SECTION_LABELS:
                raise FixtureError(f"negative case {name!r}: unknown output section {section_name!r}")
        elif kind == "stage_chunk_mutation":
            run_stage_mutation(model_text(case_input["mutation"]), data)
        elif kind == "fixture_type":
            field = model_text(case_input["field"])
            if field == "rendered":
                model_boolean(case_input["value"])
            elif field == "fence":
                require_exact_integer(case_input["value"], positive=True)
            elif field == "status_code":
                model_integer(case_input["value"])
            else:
                raise FixtureError(f"negative case {name!r}: unknown fixture type field {field!r}")
        elif kind == "json_text":
            loads_strict(model_text(case_input["text"]), rejection_mode=True)
        elif kind == "u64":
            parse_canonical_decimal(model_text(case_input["decimal"]))
        elif kind == "output_utf8":
            field = model_text(case_input["field"])
            if field not in {"html", "original_html", "alt", "url", "content_type", "render_policy_rule"}:
                raise FixtureError(f"negative case {name!r}: unknown UTF-8 field {field!r}")
            validate_wire_utf8(bytes.fromhex(model_text(case_input["bytes_hex"])))
        elif kind == "transition_replay":
            mutation = model_text(case_input["mutation"])
            identities = data["identities"]
            reasons = data["transition_reasons"]
            if type(identities) is not dict or type(reasons) is not dict:
                raise FixtureError("baseline transition shape mismatch")
            base_payload = [("owner_id", model_text(identities["owner_id"]))]
            base = transition_id(data, "CJ2_RETRY", model_text(reasons["retry"]), base_payload, True)
            if mutation == "legal_reason":
                submitted = transition_id(data, "CJ2_RETRY", "dns_temporary", base_payload, True)
            elif mutation == "semantic_payload":
                submitted = transition_id(
                    data,
                    "CJ2_RETRY",
                    model_text(reasons["retry"]),
                    [("owner_id", model_text(identities["alternate_owner_id"]))],
                    True,
                )
            else:
                raise FixtureError(f"negative case {name!r}: unknown replay mutation {mutation!r}")
            verify_replay(base, submitted)
        else:
            raise FixtureError(f"negative case {name!r}: unknown kind {kind!r}")
    except Rejection as error:
        if error.rejection_class != expected:
            raise FixtureError(
                f"negative case {name!r}: expected {expected}, got {error.rejection_class}"
            ) from error
    else:
        raise FixtureError(f"negative case {name!r}: accepted; expected {expected}")


def verify_negatives(
    cases: list[object],
    data: dict[str, object],
    positive_results: dict[str, dict[str, object]],
) -> None:
    for raw_case in cases:
        case = expect_object(raw_case, "negative case")
        run_negative_case(case, data, positive_results)


def compare(expected: object, actual: object, path: str) -> list[str]:
    failures: list[str] = []
    if type(expected) is dict and type(actual) is dict:
        expected_dict: dict[str, object] = expected
        actual_dict: dict[str, object] = actual
        if set(expected_dict) != set(actual_dict):
            failures.append(
                f"{path}: key set differs; expected={sorted(expected_dict)}, computed={sorted(actual_dict)}"
            )
            return failures
        for key in sorted(expected_dict):
            failures.extend(compare(expected_dict[key], actual_dict[key], f"{path}.{key}"))
    elif type(expected) is list and type(actual) is list:
        expected_list: list[object] = expected
        actual_list: list[object] = actual
        if len(expected_list) != len(actual_list):
            failures.append(f"{path}: expected length {len(expected_list)}, computed {len(actual_list)}")
            return failures
        for index, (expected_item, actual_item) in enumerate(zip(expected_list, actual_list)):
            failures.extend(compare(expected_item, actual_item, f"{path}[{index}]"))
    elif type(expected) is not type(actual) or expected != actual:
        failures.append(f"{path}: expected {expected!r}, computed {actual!r}")
    return failures


def main() -> int:
    root = Path(__file__).resolve().parents[1]
    default_fixture = root / "contracts" / "crawl-jobs-v2" / "digest-vectors.json"
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("fixture", nargs="?", type=Path, default=default_fixture)
    parser.add_argument("--print-computed", action="store_true")
    args = parser.parse_args()
    try:
        raw_bytes = args.fixture.read_bytes()
        try:
            raw_text = raw_bytes.decode("utf-8", "strict")
        except UnicodeDecodeError as error:
            raise FixtureError("fixture file is not valid UTF-8") from error
        data = validate_fixture_schema(loads_strict(raw_text))
        baseline = compute_baseline(data)
        cases_value = data["cases"]
        negatives_value = data["negative_vectors"]
        if type(cases_value) is not list or type(negatives_value) is not list:
            raise FixtureError("validated case arrays changed type")
        cases = evaluate_cases(cases_value, data, root)
        verify_negatives(negatives_value, data, cases)
        computed = {"expected": baseline, "cases": cases}
        if args.print_computed:
            print(json.dumps(computed, ensure_ascii=False, indent=2, sort_keys=True))
            return 0
        failures = compare(data["expected"], baseline, "$.expected")
        for raw_case in cases_value:
            case = expect_object(raw_case, "case")
            name = model_text(case["name"])
            failures.extend(compare(case["expected"], cases[name], f"$.cases[{name!r}].expected"))
        if failures:
            for failure in failures:
                print(failure, file=sys.stderr)
            return 1
    except (OSError, FixtureError, Rejection) as error:
        detail = error.rejection_class if isinstance(error, Rejection) else str(error)
        print(f"crawl-jobs-v2 vector verification failed: {detail}", file=sys.stderr)
        return 1
    except Exception as error:  # An unexpected exception is always a fixture failure.
        print(
            f"crawl-jobs-v2 vector verification failed: unexpected {type(error).__name__}: {error}",
            file=sys.stderr,
        )
        return 1
    print("crawl-jobs-v2 digest vectors verified (fixture v2)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
