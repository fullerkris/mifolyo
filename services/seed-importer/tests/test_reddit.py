import json
import sys
import unittest
from datetime import datetime, timezone
from pathlib import Path


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

from crawl_seed import ROOT_FIELDS, merge_seed_documents  # noqa: E402
from reddit import (  # noqa: E402
    ImportStats,
    discover_from_payload,
    seed_document,
    seed_from_url,
)


class RedditV1Tests(unittest.TestCase):
    def setUp(self):
        self.row = {
            "url": "https://old.reddit.com/r/programming",
            "category": "Technology",
            "priority": "3",
            "source": "manual_reddit_discovery",
        }
        self.source_url = "https://old.reddit.com/r/programming"
        self.source_json_url = "https://old.reddit.com/r/programming/.json"

    def test_fixture_uses_v1_identity_without_removing_tracking_query(self):
        payload = json.loads(
            (SERVICE_ROOT / "testdata" / "reddit-listing.json").read_text(
                encoding="utf-8"
            )
        )
        stats = ImportStats()
        seeds = discover_from_payload(
            payload,
            self.row,
            self.source_url,
            self.source_json_url,
            min_score=25,
            include_comment_urls=False,
            stats=stats,
        )
        self.assertEqual(1, len(seeds))
        self.assertEqual(
            "https://developer.mozilla.org/en-US/docs/Web/API?utm_source=reddit",
            seeds[0].url,
        )
        document = seed_document(
            seeds[0], datetime(2026, 1, 1, tzinfo=timezone.utc)
        )
        self.assertEqual(ROOT_FIELDS, frozenset(document))
        self.assertEqual(seeds[0].url_id, document["_id"])
        self.assertEqual("reddit_json", document["sources"][0]["type"])
        self.assertNotIn("reddit", document.keys())
        self.assertEqual(3, stats.posts_seen)
        self.assertEqual(1, stats.skipped_low_score)

    def test_out_reddit_unwrap_and_reddit_destination_exclusion_are_adapter_specific(self):
        wrapped = seed_from_url(
            "https://out.reddit.com/t3_x?url=https%3A%2F%2FExample.com%2FPath%23part",
            self.row,
            self.source_url,
            self.source_json_url,
            reddit_permalink="/r/programming/comments/x/post/",
        )
        self.assertIsNotNone(wrapped)
        self.assertEqual("https://example.com/Path", wrapped.url)
        self.assertTrue(wrapped.raw_url.startswith("https://out.reddit.com/"))

        excluded = seed_from_url(
            "https://www.reddit.com/r/python",
            self.row,
            self.source_url,
            self.source_json_url,
        )
        self.assertIsNone(excluded)

    def test_two_reddit_posts_for_one_url_merge_both_provenance_sources(self):
        observed = datetime(2026, 1, 1, tzinfo=timezone.utc)
        first = seed_from_url(
            "https://example.com/article#first",
            self.row,
            self.source_url,
            self.source_json_url,
            title="First post",
            score=100,
            reddit_permalink="/r/programming/comments/one/post/",
        )
        second = seed_from_url(
            "https://example.com/article#second",
            {**self.row, "category": "Reference & Research", "priority": "1"},
            self.source_url,
            self.source_json_url,
            title="Second post",
            score=200,
            reddit_permalink="/r/programming/comments/two/post/",
        )
        merged = merge_seed_documents(
            seed_document(first, observed), seed_document(second, observed)
        )
        self.assertEqual(2, len(merged["sources"]))
        self.assertEqual(1, merged["priority"])
        self.assertEqual(
            ["Reference & Research", "Technology"], merged["categories"]
        )
        self.assertEqual(
            {"First post", "Second post"},
            {source["metadata"]["title"] for source in merged["sources"]},
        )


if __name__ == "__main__":
    unittest.main()
