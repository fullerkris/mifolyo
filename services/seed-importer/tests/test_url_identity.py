import json
import sys
import unittest
from pathlib import Path


SERVICE_ROOT = Path(__file__).resolve().parents[1]
REPO_ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(SERVICE_ROOT))

from url_identity import (  # noqa: E402
    CANONICALIZATION_VERSION,
    ID_NAMESPACE,
    MAX_URL_BYTES,
    URLCanonicalizationError,
    canonicalize_url,
    identify_url,
    url_id_for_canonical_url,
)


class SharedURLIdentityVectorTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        fixture_path = REPO_ROOT / "contracts" / "url-canonicalization" / "v1.json"
        cls.fixture = json.loads(fixture_path.read_text(encoding="utf-8"))

    def test_all_shared_valid_vectors(self):
        self.assertEqual(self.fixture["version"], CANONICALIZATION_VERSION)
        self.assertEqual(self.fixture["id_namespace"], ID_NAMESPACE)
        self.assertEqual(self.fixture["max_url_bytes"], MAX_URL_BYTES)
        for vector in self.fixture["valid"]:
            with self.subTest(vector=vector["name"]):
                result = identify_url(vector["input"])
                self.assertEqual(vector["canonical_url"], result.canonical_url)
                self.assertEqual(vector["url_id"], result.url_id)
                self.assertEqual(vector["crawl_eligible"], result.crawl_eligible)
                self.assertEqual(vector["crawl_rejection"], result.crawl_rejection)
                self.assertEqual(
                    result.canonical_url, canonicalize_url(result.canonical_url)
                )
                self.assertEqual(
                    result.url_id,
                    url_id_for_canonical_url(result.canonical_url),
                )

    def test_all_shared_invalid_vectors_have_exact_error_codes(self):
        for vector in self.fixture["invalid"]:
            with self.subTest(vector=vector["name"]):
                with self.assertRaises(URLCanonicalizationError) as raised:
                    identify_url(vector["input"])
                self.assertEqual(vector["error"], raised.exception.code)

    def test_component_encoding_empty_query_and_port_normalization(self):
        cases = {
            "https://example.com/a path/{item}?value=a b|c": (
                "https://example.com/a%20path/%7Bitem%7D?value=a%20b%7Cc"
            ),
            "https://example.com:08443/path": "https://example.com:8443/path",
            "http://example.com:00080/path": "http://example.com/path",
            "https://example.com/path?#fragment": "https://example.com/path?",
        }
        for raw_url, expected in cases.items():
            with self.subTest(raw_url=raw_url):
                self.assertEqual(expected, canonicalize_url(raw_url))

    def test_structurally_invalid_idna_and_ipv6_zone_are_rejected(self):
        for raw_url in (
            "https://xn--abc.example/",
            "https://xn--a-0hc.example/",
            "https://[fe80::1%25eth0]/",
        ):
            with self.subTest(raw_url=raw_url):
                with self.assertRaises(URLCanonicalizationError) as raised:
                    identify_url(raw_url)
                self.assertEqual("invalid_host", raised.exception.code)

    def test_reserved_local_suffix_is_not_crawl_eligible(self):
        for suffix in ("test", "onion", "alt", "arpa", "example"):
            with self.subTest(suffix=suffix):
                result = identify_url(f"https://service.{suffix}/path")
                self.assertFalse(result.crawl_eligible)
                self.assertEqual("local_name_forbidden", result.crawl_rejection)

    def test_percent_encoded_c0_del_and_c1_controls_share_stable_error(self):
        for suffix in ("%00", "%1F", "%7f", "%C2%85"):
            with self.subTest(suffix=suffix):
                with self.assertRaises(URLCanonicalizationError) as raised:
                    identify_url(f"https://example.com/{suffix}")
                self.assertEqual("encoded_control", raised.exception.code)
        self.assertEqual(
            "https://example.com/%20",
            canonicalize_url("https://example.com/%20"),
        )


if __name__ == "__main__":
    unittest.main()
