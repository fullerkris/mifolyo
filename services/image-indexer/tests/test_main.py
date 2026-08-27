import unittest
import json
from unittest.mock import MagicMock

import main
from data.redis_client import InvalidImageWork, RedisClient, claim_reference


class MainTests(unittest.TestCase):
    def test_failure_log_uses_claim_reference_and_omits_queue_and_exception_values(self):
        redis_client = MagicMock()
        redis_client.owner_token = "owner"
        redis_client.owner_epoch = 9
        redis_client.renew_lock.return_value = True
        redis_client.load_work.side_effect = RuntimeError(
            "https://example.org/private alt=do-not-log raw-queue-value"
        )
        mongo_client = MagicMock()
        mongo_client.acquire_canonical_lock.return_value = True
        mongo_client.release_canonical_lock.return_value = True
        manifest = "page_images:" + "a" * 64 + ":aHR0cHM6Ly9leGFtcGxlLm9yZy9wcml2YXRl"

        with self.assertLogs("main", level="ERROR") as captured:
            self.assertEqual(
                main.TRANSIENT_FAILURE,
                main.process_claim(redis_client, mongo_client, manifest),
            )

        output = "\n".join(captured.output)
        self.assertIn(claim_reference(manifest), output)
        self.assertNotIn(manifest, output)
        self.assertNotIn("example.org", output)
        self.assertNotIn("do-not-log", output)
        self.assertNotIn("raw-queue-value", output)

    def test_corrupt_work_is_quarantined_without_mongo_access(self):
        redis_client = MagicMock()
        redis_client.owner_token = "owner"
        redis_client.owner_epoch = 9
        redis_client.renew_lock.return_value = True
        redis_client.load_work.side_effect = InvalidImageWork("corrupt")
        redis_client.quarantine.return_value = True
        mongo_client = MagicMock()
        mongo_client.acquire_canonical_lock.return_value = True
        mongo_client.owns_canonical_lock.return_value = True
        mongo_client.release_canonical_lock.return_value = True
        manifest = "page_images:" + "a" * 64 + ":aHR0cHM6Ly9leGFtcGxlLm9yZw"
        self.assertEqual(
            main.QUARANTINED,
            main.process_claim(redis_client, mongo_client, manifest),
        )
        mongo_client.get_keywords.assert_not_called()
        mongo_client.replace_page_images.assert_not_called()

    def test_ack_occurs_only_after_acknowledged_reconciliation_and_renewal(self):
        redis_client = MagicMock()
        redis_client.owner_token = "owner"
        redis_client.owner_epoch = 9
        redis_client.renew_lock.return_value = True
        work = MagicMock()
        work.page_url = "page"
        work.publication_id = "a" * 64
        work.images = []
        redis_client.load_work.return_value = work
        redis_client.ack.return_value = True
        mongo_client = MagicMock()
        mongo_client.acquire_canonical_lock.return_value = True
        mongo_client.owns_canonical_lock.return_value = True
        mongo_client.release_canonical_lock.return_value = True
        mongo_client.get_keywords.return_value = {}
        mongo_client.replace_page_images.return_value = True
        manifest = "page_images:" + "a" * 64 + ":aHR0cHM6Ly9leGFtcGxlLm9yZw"
        self.assertEqual(main.COMPLETED, main.process_claim(redis_client, mongo_client, manifest))
        redis_client.ack.assert_called_once_with(work)

        redis_client.reset_mock()
        redis_client.owner_epoch = 9
        redis_client.renew_lock.return_value = True
        redis_client.load_work.return_value = work
        mongo_client.replace_page_images.return_value = False
        self.assertEqual(main.TRANSIENT_FAILURE, main.process_claim(redis_client, mongo_client, manifest))
        redis_client.ack.assert_not_called()

    def test_contract_type_error_quarantines_through_production_process_claim(self):
        publication = "a" * 64
        page = "https://example.org"
        source = "https://cdn.example.org/image.jpg"
        manifest = "page_images:" + publication + ":aHR0cHM6Ly9leGFtcGxlLm9yZw"
        payload = (
            "image_data:" + publication
            + ":aHR0cHM6Ly9leGFtcGxlLm9yZw:"
            + "aHR0cHM6Ly9jZG4uZXhhbXBsZS5vcmcvaW1hZ2UuanBn"
        )
        loader = RedisClient.__new__(RedisClient)
        loader.client = MagicMock()
        loader.client.hgetall.return_value = {
            "contract_version": "1",
            "publication_id": publication,
            "normalized_url": page,
            "image_count": "1",
            "image_keys": json.dumps([payload], separators=(",", ":")),
        }
        pipeline = MagicMock()
        pipeline.execute.return_value = [{
            "contract_version": "1",
            "publication_id": publication,
            "normalized_page_url": page,
            "normalized_source_url": source,
            "alt": ["invalid", "type"],
        }]
        loader.client.pipeline.return_value = pipeline
        loader.client.eval.return_value = 1

        redis_client = MagicMock()
        redis_client.owner_token = "owner"
        redis_client.owner_epoch = 9
        redis_client.renew_lock.return_value = True
        redis_client.load_work.side_effect = loader.load_work
        redis_client.quarantine.return_value = True
        mongo_client = MagicMock()
        mongo_client.acquire_canonical_lock.return_value = True
        mongo_client.owns_canonical_lock.return_value = True
        mongo_client.release_canonical_lock.return_value = True

        self.assertEqual(
            main.QUARANTINED,
            main.process_claim(redis_client, mongo_client, manifest),
        )
        redis_client.quarantine.assert_called_once_with(manifest)
        mongo_client.replace_page_images.assert_not_called()


if __name__ == "__main__":
    unittest.main()
