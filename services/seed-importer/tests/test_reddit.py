import json
import sys
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path
from unittest import mock


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

from crawl_seed import ROOT_FIELDS, merge_seed_documents  # noqa: E402
from reddit import (  # noqa: E402
    ImportStats,
    discover_from_payload,
    is_reddit_owned_host,
    iter_listing_posts,
    load_json_file,
    main,
    reddit_crawl_urls,
    reddit_json_url,
    seed_document,
    seed_from_url,
    validate_reddit_export,
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
        self.source_json_url = "https://old.reddit.com/r/programming.json"

    def test_reddit_json_url_replaces_or_appends_the_path_suffix(self):
        cases = {
            "https://www.reddit.com/r/games/": "https://www.reddit.com/r/games.json",
            "https://www.reddit.com/r/games": "https://www.reddit.com/r/games.json",
            "https://old.reddit.com/r/games.json": "https://old.reddit.com/r/games.json",
            "https://old.reddit.com/r/games.json/": "https://old.reddit.com/r/games.json",
            "https://old.reddit.com/r/games/.json": "https://old.reddit.com/r/games.json",
            "https://old.reddit.com/r/games/.json/": "https://old.reddit.com/r/games.json",
            "https://reddit.com/r/games/?sort=top": "https://www.reddit.com/r/games.json?sort=top",
            "https://old.reddit.com/": "https://old.reddit.com/.json",
        }
        for raw_url, expected in cases.items():
            with self.subTest(raw_url=raw_url):
                self.assertEqual(expected, reddit_json_url(raw_url))

    def test_reddit_crawl_urls_expands_www_old_and_json_variants(self):
        self.assertEqual(
            (
                "https://www.reddit.com/r/games",
                "https://old.reddit.com/r/games",
                "https://www.reddit.com/r/games.json",
                "https://old.reddit.com/r/games.json",
            ),
            reddit_crawl_urls("https://www.reddit.com/r/games/"),
        )
        self.assertIsNone(reddit_crawl_urls("https://example.com/r/games/"))
        for raw_url in (
            "ftp://www.reddit.com/r/games",
            "https://user@www.reddit.com/r/games",
            "https://www.reddit.com:444/r/games",
            " https://www.reddit.com/r/games",
        ):
            with self.subTest(raw_url=raw_url):
                self.assertIsNone(reddit_crawl_urls(raw_url))

        for raw_url in (
            "https://www.reddit.com/r/games//new",
            "https://www.reddit.com/r/games/%2Fnew",
            "https://www.reddit.com/r/games/../new",
            "https://www.reddit.com/r/%70rogramming",
            "https://www.reddit.com/r/%FF",
            "https://www.reddit.com/r/%C0%AFadmin",
        ):
            with self.subTest(raw_url=raw_url):
                self.assertIsNone(reddit_crawl_urls(raw_url))

        self.assertEqual(
            (
                "https://www.reddit.com/r/games?",
                "https://old.reddit.com/r/games?",
                "https://www.reddit.com/r/games.json?",
                "https://old.reddit.com/r/games.json?",
            ),
            reddit_crawl_urls("https://www.reddit.com/r/games?"),
        )

        prefix = "https://www.reddit.com/r/"
        maximum_input = prefix + ("a" * (2048 - len(prefix)))
        self.assertIsNone(reddit_crawl_urls(maximum_input))

    def test_remote_reddit_discovery_is_disabled(self):
        self.assertEqual(1, main(["--dry-run"]))

    def test_local_export_binds_source_and_category_to_payload(self):
        row, payload = validate_reddit_export(
            load_json_file(str(SERVICE_ROOT / "testdata" / "reddit-listing.json"))
        )
        self.assertEqual(
            {
                "url": "https://old.reddit.com/r/programming",
                "category": "Technology",
                "priority": 3,
                "source": "approved_reddit_export",
            },
            row,
        )
        self.assertEqual(3, len(list(iter_listing_posts(payload))))

    def test_missing_or_malformed_local_export_fails(self):
        self.assertEqual(
            1,
            main(
                [
                    "--input-json",
                    "/does/not/exist.json",
                    "--dry-run",
                ]
            ),
        )
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json") as export:
            export.write("{")
            export.flush()
            self.assertEqual(
                1,
                main(
                    [
                        "--input-json",
                        export.name,
                        "--dry-run",
                    ]
                ),
            )

        with tempfile.NamedTemporaryFile(mode="w", suffix=".json") as export:
            json.dump({}, export)
            export.flush()
            self.assertEqual(1, main(["--input-json", export.name, "--dry-run"]))

        invalid_source = load_json_file(
            str(SERVICE_ROOT / "testdata" / "reddit-listing.json")
        )
        invalid_source["source_url"] = "https://example.com/r/programming"
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json") as export:
            json.dump(invalid_source, export)
            export.flush()
            self.assertEqual(1, main(["--input-json", export.name, "--dry-run"]))

    def test_runtime_export_validation_matches_schema_and_source(self):
        fixture = load_json_file(
            str(SERVICE_ROOT / "testdata" / "reddit-listing.json")
        )

        invalid = dict(fixture)
        invalid["schema_version"] = True
        with self.assertRaises(ValueError):
            validate_reddit_export(invalid)

        invalid = dict(fixture)
        invalid["category"] = "Technology\nInjected"
        with self.assertRaises(ValueError):
            validate_reddit_export(invalid)

        invalid = json.loads(json.dumps(fixture))
        invalid["payload"]["data"]["children"][0]["data"]["subreddit"] = "science"
        with self.assertRaises(ValueError):
            validate_reddit_export(invalid)

        with tempfile.NamedTemporaryFile(mode="w", suffix=".json") as export:
            export.write(
                '{"schema_version":1,"schema_version":1,'
                '"source_url":"https://old.reddit.com/r/programming",'
                '"category":"Technology","priority":3,"payload":{}}'
            )
            export.flush()
            with self.assertRaises(ValueError):
                load_json_file(export.name)

    def test_consumed_post_fields_are_validated_before_mongo(self):
        invalid = load_json_file(
            str(SERVICE_ROOT / "testdata" / "reddit-listing.json")
        )
        invalid["payload"]["data"]["children"][0]["data"]["score"] = "invalid"
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json") as export:
            json.dump(invalid, export)
            export.flush()
            with mock.patch("reddit.MongoClient") as mongo_client:
                self.assertEqual(2, main(["--input-json", export.name]))
                mongo_client.assert_not_called()

        with tempfile.NamedTemporaryFile(mode="w", suffix=".json") as export:
            export.write(
                '{"schema_version":1,'
                '"source_url":"https://old.reddit.com/r/programming",'
                '"category":"Technology","priority":3,'
                '"payload":{"data":{"children":[{"kind":"t3","data":{'
                '"subreddit":"programming",'
                '"permalink":"/r/programming/comments/x/post/",'
                '"score":NaN}}]}}}'
            )
            export.flush()
            with mock.patch("reddit.MongoClient") as mongo_client:
                self.assertEqual(1, main(["--input-json", export.name]))
                mongo_client.assert_not_called()

    def test_reddit_owned_subdomains_are_not_outbound_seeds(self):
        for host in ("oauth.reddit.com", "i.redd.it", "deep.oauth.reddit.com"):
            with self.subTest(host=host):
                self.assertTrue(is_reddit_owned_host(host))
                self.assertIsNone(
                    seed_from_url(
                        f"https://{host}/asset",
                        self.row,
                        self.source_url,
                        self.source_json_url,
                    )
                )

    def test_fixture_uses_v1_identity_without_removing_tracking_query(self):
        _, payload = validate_reddit_export(
            load_json_file(str(SERVICE_ROOT / "testdata" / "reddit-listing.json"))
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
