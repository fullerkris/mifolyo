import json
import sys
import unittest
from pathlib import Path


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

from config import BACKLINK_KEY_PREFIX, MAX_CANONICAL_URL_BYTES  # noqa: E402
from data.redis_client import (  # noqa: E402
    InvalidBacklinkSet,
    parse_backlink_key,
    parse_backlink_member,
)
from url_validation import (  # noqa: E402
    CanonicalURLValidationError,
    validate_canonical_url,
)


class CanonicalURLValidationTests(unittest.TestCase):
    def test_all_shared_v1_canonical_outputs_are_accepted(self):
        vectors_path = (
            SERVICE_ROOT.parent
            / "spider"
            / "internal"
            / "utils"
            / "testdata"
            / "url-canonicalization-v1.json"
        )
        vectors = json.loads(vectors_path.read_text(encoding="utf-8"))

        for vector in vectors["valid"]:
            with self.subTest(vector=vector["name"]):
                validate_canonical_url(vector["canonical_url"])

    def test_noncanonical_or_unsafe_values_are_rejected(self):
        values = (
            "HTTPS://example.com/",
            "https://Example.com/",
            "https://example.com",
            "https://example.com:443/",
            "https://example.com/%2f",
            "https://example.com/%00",
            "https://example.com/path#fragment",
            "https://user@example.com/",
            "https://bad_host.example/",
            "https://xn--ab-j1t.example/",
            "https://[fe80::1%25eth0]/",
            "/relative",
        )
        for value in values:
            with self.subTest(value=value):
                with self.assertRaises(CanonicalURLValidationError):
                    validate_canonical_url(value)

    def test_percent_escape_does_not_consume_following_hex_path_text(self):
        validate_canonical_url("https://example.com/%2Fabc")

    def test_parsers_preserve_exact_raw_member_for_ack(self):
        target = "https://example.com/target"
        source = "https://example.com/source?x=a+b"

        self.assertEqual(
            target,
            parse_backlink_key(BACKLINK_KEY_PREFIX + target.encode("utf-8")),
        )
        member = parse_backlink_member(source.encode("utf-8"))
        self.assertEqual(source, member.url)
        self.assertEqual(source.encode("utf-8"), member.raw)

    def test_parsers_enforce_url_byte_limit_before_persistence(self):
        oversized = b"https://example.com/" + b"a" * MAX_CANONICAL_URL_BYTES

        with self.assertRaises(InvalidBacklinkSet) as member_error:
            parse_backlink_member(oversized)
        self.assertEqual("member_url_too_long", member_error.exception.reason)

        with self.assertRaises(InvalidBacklinkSet) as key_error:
            parse_backlink_key(BACKLINK_KEY_PREFIX + oversized)
        self.assertEqual("target_url_too_long", key_error.exception.reason)


if __name__ == "__main__":
    unittest.main()
