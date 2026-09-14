import sys
import unittest
from pathlib import Path


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

from reconcile import ReconciliationRejected, reconcile_backlinks  # noqa: E402


class FakeMongo:
    def __init__(self):
        self.outlinks = [
            {
                "_id": "https://source-a.example/",
                "links": [
                    "https://target.example/",
                    "https://target.example/",
                ],
            },
            {
                "_id": "https://source-b.example/",
                "links": ["https://target.example/"],
            },
        ]
        self.backlinks = {
            "https://historical.example/": {"https://stale.example/"}
        }

    def iter_outlinks(self):
        return iter(self.outlinks)

    def iter_backlinks(self):
        return iter(
            {"_id": target, "links": sorted(sources)}
            for target, sources in self.backlinks.items()
        )

    def has_backlink(self, target, source):
        return source in self.backlinks.get(target, set())

    def has_outlink(self, source, target):
        return any(
            document["_id"] == source and target in document["links"]
            for document in self.outlinks
        )

    def add_backlinks(self, target, sources):
        self.backlinks.setdefault(target, set()).update(sources)


class ReconciliationTests(unittest.TestCase):
    def test_report_mode_is_read_only_and_reports_missing_and_extra_edges(self):
        mongo = FakeMongo()

        report = reconcile_backlinks(mongo, apply=False)

        self.assertEqual(2, report.authoritative_edges)
        self.assertEqual(2, report.missing_before)
        self.assertEqual(2, report.missing_after)
        self.assertEqual(0, report.added)
        self.assertEqual(1, report.extra_historical_edges)
        self.assertNotIn("https://target.example/", mongo.backlinks)

    def test_apply_mode_adds_missing_edges_without_removing_history(self):
        mongo = FakeMongo()

        report = reconcile_backlinks(mongo, apply=True)

        self.assertEqual(2, report.added)
        self.assertEqual(0, report.missing_after)
        self.assertEqual(1, report.extra_historical_edges)
        self.assertEqual(
            {
                "https://source-a.example/",
                "https://source-b.example/",
            },
            mongo.backlinks["https://target.example/"],
        )
        self.assertEqual(
            {"https://stale.example/"},
            mongo.backlinks["https://historical.example/"],
        )

    def test_invalid_outlinks_document_fails_closed(self):
        mongo = FakeMongo()
        mongo.outlinks = [{"_id": "https://source.example/", "links": "not-a-list"}]

        with self.assertRaises(ReconciliationRejected):
            reconcile_backlinks(mongo, apply=True)


if __name__ == "__main__":
    unittest.main()
