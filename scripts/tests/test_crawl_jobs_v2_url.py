"""Offline URL verifier regressions; Go differential driver is in lua_url_test.go.

Prerequisites: Python 3.10+ (the URL helper uses dataclass(slots=True)), the
standard library, and checked-in pinned Unicode data. CI tests with Python 3.13.
Run from repository root: python3 -B -m unittest discover -s scripts/tests -v

The --go-oracle mode only consumes expectations computed by the pinned Go
authorities. It never starts Go/Lua, generates Unicode data, or uses Lua as an
oracle. The normal Python unit suite needs no installed Go/Lua runtime. The
separate Go differential driver requires the exact Go/module pins documented in
services/spider/internal/database/crawljobsv2/lua_src/README.md under Unicode
and Python maintenance.
"""

from __future__ import annotations

import copy
import hashlib
import importlib.util
import json
import os
import runpy
import subprocess
import sys
import tempfile
import unittest
import unicodedata
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
VERIFIER = ROOT / "scripts/verify-crawl-jobs-v2-digests.py"
FIXTURE = ROOT / "contracts/crawl-jobs-v2/digest-vectors.json"
spec = importlib.util.spec_from_file_location("_cj2_url_tests", ROOT / "scripts/crawl_jobs_v2_url.py")
assert spec is not None and spec.loader is not None
url = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = url
spec.loader.exec_module(url)
verifier = runpy.run_path(str(VERIFIER))


def alabel(decoded: str) -> str:
    return "xn--" + decoded.encode("punycode").decode("ascii")


class UnicodeDataTests(unittest.TestCase):
    def test_full_literal_inventory_and_exhaustive_go_source_proof(self):
        source = url.DATA_PATH.read_bytes()
        self.assertEqual(hashlib.sha256(source).hexdigest(), url.DATA_SHA256)
        packed = url.parse_pinned_literals(source)
        self.assertEqual(tuple(packed), ("ranges", "props", "decomp", "compositions", "ascii"))
        self.assertEqual([len(packed[k]) for k in packed], [32305, 18891, 9804, 6587, 256])
        self.assertEqual(sum(map(len, packed.values())), 67843)
        self.assertEqual(len(url.TABLES.starts), 6461)
        self.assertEqual(len(url.TABLES.properties), 2099)
        self.assertEqual(len(url.TABLES.compositions), 941)
        self.assertEqual(url.TABLES.verify_exhaustive_inventory(), url.GO_PROPERTY_ORACLE_SHA256)
        with self.assertRaises(TypeError):
            packed["props"] = b""
        with self.assertRaises(TypeError):
            url.TABLES.compositions[0] = 0

    def test_no_data_substitution_or_lua_execution_fallback(self):
        source = url.DATA_PATH.read_bytes()
        mutations = [
            source[:-1], source + b"os.execute('never run')\n",
            source.replace(b"local ranges =", b"local ranges= ", 1),
            source.replace(b"\\009", b"\\999", 1),
            source.replace(b"\\009", b"\\008", 1),
            source.replace(b"local props =", b"local other =", 1),
            source.replace(b"15.0.0", b"17.0.0"),
        ]
        for changed in mutations:
            with self.subTest(size=len(changed)), self.assertRaises(url.TableDataError):
                url.tables_from_source(changed)
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(url, "DATA_PATH", Path(directory) / "missing"):
                with self.assertRaises(url.TableDataError):
                    url._load_tables()

    def test_host_python_unicode_database_is_not_consulted(self):
        with patch.object(unicodedata, "normalize", side_effect=AssertionError("host NFC")), \
             patch.object(unicodedata, "category", side_effect=AssertionError("host category")), \
             patch.object(unicodedata, "combining", side_effect=AssertionError("host CCC")), \
             patch.object(unicodedata, "bidirectional", side_effect=AssertionError("host bidi")):
            for decoded in ("1é", "a·b", "😀", "क्\u200dष", "ب\u200c1"):
                self.assertTrue(url.valid_alabel(alabel(decoded)))
                self.assertTrue(verifier["_valid_idna_alabel"](alabel(decoded)))
            for decoded in ("É", "e\u0301", "q" + "\u0301" * 31, "\u0903a", "é--a"):
                self.assertFalse(url.valid_alabel(alabel(decoded)))


class CanonicalURLTests(unittest.TestCase):
    def reject(self, value, code="CANONICAL_URL_NOT_CANONICAL"):
        with self.assertRaises(url.URLValidationError) as caught:
            url.validate_identity(value)
        self.assertEqual(caught.exception.code, code)

    def test_confirmed_go_counterexamples(self):
        accepted = ["faß", "bücher", "1é", "a·b", "☃", "😀", "क्\u200dष", "ب\u200cب", "ب\u200c1", "éa--b", "ς", "א1", "א١", "א\u05b0"]
        rejected = ["É", "\u0903a", "q" + "\u0301" * 31, "é--a", "ab--é", "a\u200cb", "e\u0301", "aא", "אa", "א1١", "א·", "\ue000", "\ufdd0", "\U0001fae9", "¼", "²"]
        for decoded in accepted:
            with self.subTest(decoded=decoded):
                value = "https://" + alabel(decoded) + ".example/"
                self.assertTrue(url.valid_alabel(alabel(decoded)))
                self.assertEqual(url.validate_identity(value).canonical_url, value)
        for decoded in rejected:
            with self.subTest(decoded=decoded):
                self.assertFalse(url.valid_alabel(alabel(decoded)))
                self.reject("https://" + alabel(decoded) + ".example/")
        for payload in ("", "-a", "abc-", "z" * 59, "9" * 59, "a" * 60):
            self.assertFalse(url.valid_alabel("xn--" + payload))

    def test_nfc_stream_safety_and_hangul(self):
        for text in ("é", "가", "각", "q" + "\u0301" * 30, "éa--b"):
            self.assertTrue(url.nfc_normal(text), repr(text))
        for text in ("e\u0301", "ḋ\u0323", "\u1100\u1161", "\u1100\u1161\u11a8", "q" + "\u0301" * 31):
            self.assertFalse(url.nfc_normal(text), repr(text))
        for decoded in ("中" * 40, "\U00020000" * 40):
            self.assertGreater(len(decoded.encode("utf-8")), 63)
            self.assertTrue(url.valid_alabel(alabel(decoded)))

    def test_percent_bytes_and_encoded_controls(self):
        for byte in range(256):
            for prefix in ("https://example.com/", "https://example.com/?x="):
                escaped = prefix + f"%{byte:02X}"
                if byte < 32 or byte == 127:
                    self.reject(escaped)
                else:
                    self.assertEqual(url.validate_identity(escaped).canonical_url, escaped)
                pair = prefix + f"%C2%{byte:02X}"
                if byte < 32 or byte == 127 or 128 <= byte <= 159:
                    self.reject(pair)
                else:
                    self.assertEqual(url.validate_identity(pair).canonical_url, pair)
        for suffix in ("%FF", "%80", "%ED%A0%80", "%C0%AF", "%2500", "%5C", "%C2%2585", "%C2x%85"):
            url.validate_identity("https://example.com/" + suffix)
        for suffix in ("%", "%0", "%g0", "%2f", "%c2%85", "a b", "\x7f", "\u0085", "\u2028", "café"):
            self.reject("https://example.com/" + suffix)

    def test_syntax_bounds_and_preserved_query_identity(self):
        for value in (None, b"https://example.com/", 1, True, [], {}):
            self.reject(value, "FIXTURE_TYPE")
        self.reject("https://example.com/\ud800", "INVALID_UTF8")
        base = "https://example.com/"
        self.assertEqual(len(url.validate_identity(base + "x" * (2048 - len(base))).canonical_url), 2048)
        self.reject(base + "x" * (2049 - len(base)), "URL_TOO_LONG")
        for port in ("0", "65536", "1.5", "½", "١", "1e2", "+1", "-1", "08443", ""):
            self.reject("https://example.com:" + port + "/")
        for value in ("https://example.com:443/", "http://example.com:80/"):
            self.reject(value, "URL_DEFAULT_PORT_FORBIDDEN")
        self.reject("https://user:pass@example.com/", "URL_USERINFO_FORBIDDEN")
        self.reject(base + "#", "URL_FRAGMENT_FORBIDDEN")
        for host in ("a..b", "a.", "-a.com", "a-.com", "a_b.com", "A.com", "a" * 64 + ".com", "%65xample.com"):
            self.reject("https://" + host + "/")
        for suffix in ("", "?", "??", "a//../b/?x=1&x=&x=2;+", "%7E", "~", "%61", "a"):
            value = base + suffix
            self.assertEqual(url.validate_identity(value).canonical_url, value)
        self.assertNotEqual(url.validate_identity(base).url_id, url.validate_identity(base + "?").url_id)
        self.assertNotEqual(url.validate_identity(base + "%61").url_id, url.validate_identity(base + "a").url_id)

    def test_ip_identity_origin_and_no_static_policy_conflation(self):
        for host in ("127.0.0.1", "0.0.0.0", "255.255.255.255", "[::]", "[::1]", "[2001:0db8:0000::1]", "[::ffff:192.0.2.1]", "[1:2:3:4:5:6:192.0.2.1]"):
            value = "https://" + host + "/"
            result = url.validate_identity(value)
            self.assertTrue(result.is_ip)
            self.assertIsNone(result.origin)
            self.assertEqual(result.url_id, hashlib.sha256(b"mifolyo-url:v1\0" + value.encode()).hexdigest())
            with self.assertRaises(url.URLValidationError) as caught:
                url.derive_origin(value)
            self.assertEqual(caught.exception.code, "URL_IP_LITERAL_FORBIDDEN")
        for host in ("127.00.0.1", "256.1.2.3", "[127.0.0.1]", "[:::1]", "[1::2::3]", "[2001:DB8::1]", "[fe80::1%25eth0]", "[::ffff:192.00.2.1]", "[1:2:3:4:5:6:7:8::]", "[v1.a]"):
            self.reject("https://" + host + "/")
        for host in ("localhost", "service.onion", "service.test", "127.1", "2130706433", "ab--cd.example"):
            self.assertEqual(url.derive_origin("https://" + host + "/"), "https://" + host + ":443")
        self.assertEqual(url.derive_origin("https://example.com:8443/"), "https://example.com:8443")


class VerifierLayerTests(unittest.TestCase):
    def setUp(self):
        self.data = json.loads(FIXTURE.read_text(encoding="utf-8"))
        self.ip = "https://[2001:0db8:0000::1]/"
        self.target = {"canonical_url": self.ip, "url_id": hashlib.sha256(b"mifolyo-url:v1\0" + self.ip.encode()).hexdigest()}

    def assert_ip_rejection(self, function, *args):
        with self.assertRaises(verifier["Rejection"]) as caught:
            function(*args)
        self.assertEqual(caught.exception.rejection_class, "URL_IP_LITERAL_FORBIDDEN")

    def test_identity_and_structural_output_fields_accept_ips(self):
        self.assertEqual(verifier["canonical_url_v1"](self.ip), (self.ip, None))
        verifier["validate_target"](self.target)
        verifier["target_digest"](self.target)
        self.data["output"]["outlinks"].append(self.ip)
        self.data["output"]["images"].append({"normalized_source_url": self.ip, "alt": ""})
        verifier["output_records"](self.data)
        publication = "1" * 64
        timestamp = verifier["format_last_crawled"](1788266096789)
        image_keys = json.dumps(
            [verifier["image_data_key"](publication, self.ip, self.ip)], separators=(",", ":")
        )
        for kind, record in (
            ("page_fields", list(zip(
                ("normalized_url", "content_type", "status_code", "last_crawled", "rendered", "render_policy_rule", "render_policy_sha256", "publication_id"),
                (self.ip, "text/html", "200", timestamp, "false", "", "", publication),
            ))),
            ("image_manifest", list(zip(
                ("contract_version", "publication_id", "normalized_url", "image_count", "image_keys"),
                ("1", publication, self.ip, "1", image_keys),
            ))),
            ("outlinks", [("target_url", self.ip)]),
            ("images", [("normalized_source_url", self.ip), ("alt", "")]),
            ("aliases", [("url_id", self.target["url_id"]), ("canonical_url", self.ip), ("depth", "0")]),
            # Go validateDiscoveriesChunk is structural; semantic source/policy
            # binding (tested below) is where its request origin is required.
            ("discoveries", list(zip(
                ("job_id", "canonical_url", "depth", "score_text", "group_id", "rate_scope_id", "group_scope_id", "initial_origin_scope_id", "policy_decision_sha256"),
                (self.target["url_id"], self.ip, "0", "0", "group-a", "1" * 32, "1" * 64, "1" * 64, "1" * 64),
            ))),
        ):
            verifier["validate_stage_chunk"]("1" * 64, kind, 0, [record])

    def test_policy_sources_discoveries_and_reservations_still_reject_ips(self):
        self.assert_ip_rejection(verifier["origin_scope"], self.ip)
        for name in self.data["targets"]:
            changed = copy.deepcopy(self.data)
            changed["targets"][name] = self.target
            decisions = [v for v in changed["policy_decisions"].values() if v["target"] == name]
            for decision in decisions:
                self.assert_ip_rejection(verifier["decision_fields"], decision, changed["targets"])
            for item in changed["source_jobs"] + changed["output"]["discoveries"]:
                if item["target"] == name:
                    self.assert_ip_rejection(verifier["source_job_fields"], item, changed)
        self.data["targets"]["target"] = self.target
        self.assert_ip_rejection(verifier["reservation_id"], self.data, self.data["reservation"])

    def test_all_transcript_request_kinds_explicitly_reject_ips(self):
        for kind in ("document", "redirect", "robots", "render_resource"):
            changed = copy.deepcopy(self.data)
            changed["targets"]["ip"] = self.target
            changed["output_context"]["requests"][1].update(request_kind=kind, target="ip")
            self.assert_ip_rejection(verifier["output_transcript"], changed)

    def test_fixture_inventory_and_existing_ip_negative_context(self):
        verifier["validate_fixture_schema"](self.data)
        self.assertEqual(len(self.data["cases"]), 40)
        self.assertEqual(len(self.data["negative_vectors"]), 157)
        for case in self.data["negative_vectors"]:
            if case["kind"] == "canonical_url":
                with self.assertRaises(verifier["Rejection"]) as caught:
                    verifier["canonical_origin_v1"](case["input"]["value"])
                self.assertEqual(caught.exception.rejection_class, case["expected_rejection_class"])

    def test_cli_root_and_foreign_cwd_with_shadow_helper(self):
        with tempfile.TemporaryDirectory() as directory:
            Path(directory, "crawl_jobs_v2_url.py").write_text("raise AssertionError('shadow helper executed')\n", encoding="utf-8")
            env = {**os.environ, "PYTHONPATH": directory, "PYTHONDONTWRITEBYTECODE": "1"}
            for cwd in (ROOT, Path(directory)):
                result = subprocess.run([sys.executable, "-B", str(VERIFIER)], cwd=cwd, env=env, capture_output=True, text=True, timeout=120)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("digest vectors verified", result.stdout)


def go_oracle_driver() -> None:
    """Consume JSON expectations from TestPythonURLGoOracle; never create them."""
    vectors = json.loads(sys.stdin.buffer.read().decode("utf-8", "strict"))
    for number, case in enumerate(vectors["urls"]):
        value = bytes.fromhex(case["hex"]).decode("utf-8", "surrogateescape")
        try:
            identity = url.validate_identity(value)
        except url.URLValidationError:
            identity = None
        if (identity is not None) != case["valid"]:
            raise AssertionError(f"Go/Python identity mismatch case {number}: {value!r}")
        try:
            origin = url.derive_origin(value)
        except url.URLValidationError:
            origin = None
        if origin != case["origin"]:
            raise AssertionError(f"Go/Python origin mismatch case {number}: {value!r}")
        try:
            wrapper = verifier["canonical_url_v1"](value)
        except verifier["Rejection"]:
            wrapper = None
        if identity is not None:
            for key in ("scheme", "host", "port", "explicit_port", "is_ip", "url_id"):
                if getattr(identity, key) != case[key]:
                    raise AssertionError(f"Go/Python component {key} mismatch case {number}")
            if wrapper != (value, origin):
                raise AssertionError(f"verifier identity layer mismatch case {number}")
            digest = verifier["target_digest"]({"url_id": identity.url_id, "canonical_url": value})
            if digest != case["target_digest"]:
                raise AssertionError(f"Go/Python target digest mismatch case {number}")
        elif wrapper is not None:
            raise AssertionError(f"verifier accepted a noncanonical identity case {number}")
    for number, case in enumerate(vectors["nfc"]):
        if url.nfc_normal(case["text"]) != case["normal"]:
            raise AssertionError(f"Go/Python NFC mismatch case {number}: {case['text']!r}")
    for case in vectors["chunks"]:
        verifier["validate_stage_chunk"]("1" * 64, case["kind"], 0, [list(map(tuple, case["fields"]))])
    print(json.dumps({"urls": len(vectors["urls"]), "nfc": len(vectors["nfc"]), "chunks": len(vectors["chunks"]), "data_sha256": url.DATA_SHA256}, sort_keys=True))


if __name__ == "__main__":
    if sys.argv[1:] == ["--go-oracle"]:
        go_oracle_driver()
    else:
        unittest.main()
