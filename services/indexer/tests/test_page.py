import sys
import unittest
from pathlib import Path


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

from models.page import Page  # noqa: E402


class PageTests(unittest.TestCase):
    def base_hash(self):
        return {
            "normalized_url": "https://example.org/app",
            "html": "<html></html>",
            "content_type": "text/html",
            "status_code": "200",
            "last_crawled": "Wed, 19 Aug 2026 12:00:00 UTC",
        }

    def test_legacy_static_hash_remains_supported(self):
        page = Page.from_hash(self.base_hash())
        self.assertFalse(page.rendered)
        self.assertEqual("", page.original_html)

    def test_rendered_hash_requires_provenance(self):
        page_hash = self.base_hash()
        page_hash.update(
            {
                "rendered": "true",
                "original_html": "<html><main id='root'></main></html>",
                "render_policy_rule": "inline-fixture",
                "render_policy_sha256": "a" * 64,
            }
        )
        page = Page.from_hash(page_hash)
        self.assertTrue(page.rendered)
        self.assertEqual("inline-fixture", page.render_policy_rule)
        self.assertEqual("a" * 64, page.render_policy_sha256)

        del page_hash["original_html"]
        with self.assertRaisesRegex(ValueError, "missing provenance"):
            Page.from_hash(page_hash)


if __name__ == "__main__":
    unittest.main()
