import base64
import json
import unittest
from unittest.mock import MagicMock, patch

from data.redis_client import (
    ACK_SCRIPT,
    CLAIM_SCRIPT,
    QUARANTINE_SCRIPT,
    RECOVER_SCRIPT,
    RELEASE_SCRIPT,
    InvalidImageWork,
    RedisClient,
    parse_manifest_key,
    parse_payload_key,
    validate_redis_auth,
)


PUB = "a" * 64
PAGE = "https://example.org/page"
IMAGE = "https://cdn.example.org/image.jpg"
PAGE_ENCODED = base64.urlsafe_b64encode(PAGE.encode()).decode().rstrip("=")
IMAGE_ENCODED = base64.urlsafe_b64encode(IMAGE.encode()).decode().rstrip("=")
MANIFEST = f"page_images:{PUB}:{PAGE_ENCODED}"
PAYLOAD = f"image_data:{PUB}:{PAGE_ENCODED}:{IMAGE_ENCODED}"


class RedisClientTests(unittest.TestCase):
    def setUp(self):
        self.redis = RedisClient.__new__(RedisClient)
        self.redis.client = MagicMock()
        self.redis.owner_token = "owner-a"
        self.redis.owner_epoch = 7

    def test_key_contract_and_auth_fail_closed(self):
        self.assertEqual((PUB, PAGE, PAGE_ENCODED), parse_manifest_key(MANIFEST))
        self.assertEqual((PUB, PAGE, IMAGE), parse_payload_key(PAYLOAD))
        for value in ("", f"page_images:{PUB}:bad=", "page_images:bad:x"):
            with self.assertRaises(InvalidImageWork):
                parse_manifest_key(value)
        with self.assertRaises(ValueError):
            validate_redis_auth("", "", False)
        validate_redis_auth("", "", True)
        with patch("data.redis_client.redis.Redis") as factory:
            with self.assertRaises(ValueError):
                RedisClient()
            factory.assert_not_called()

    def test_claim_recovery_release_and_quarantine_allocate_first(self):
        self.redis.client.eval.return_value = MANIFEST
        self.assertEqual(MANIFEST, self.redis.claim())
        self.assertEqual(CLAIM_SCRIPT, self.redis.client.eval.call_args.args[0])
        self.redis.client.eval.return_value = 1
        self.assertEqual(1, self.redis.recover(100))
        self.assertIn("recovered < limit", RECOVER_SCRIPT)
        self.assertTrue(self.redis.release_claim(MANIFEST))
        self.assertLess(
            RELEASE_SCRIPT.index("redis.call('RPUSH'"),
            RELEASE_SCRIPT.index("redis.call('LREM'"),
        )
        self.assertTrue(self.redis.quarantine(MANIFEST))
        self.assertLess(
            QUARANTINE_SCRIPT.index("redis.call('LPUSH'"),
            QUARANTINE_SCRIPT.index("redis.call('LREM'"),
        )

    def test_load_validated_work_and_ack_exact_keys(self):
        serialized = json.dumps([PAYLOAD], separators=(",", ":"))
        self.redis.client.hgetall.return_value = {
            "contract_version": "1",
            "publication_id": PUB,
            "normalized_url": PAGE,
            "image_count": "1",
            "image_keys": serialized,
        }
        pipeline = MagicMock()
        pipeline.execute.return_value = [{
            "contract_version": "1",
            "publication_id": PUB,
            "normalized_page_url": PAGE,
            "normalized_source_url": IMAGE,
            "alt": "safe metadata",
        }]
        self.redis.client.pipeline.return_value = pipeline
        self.redis.client.eval.side_effect = [1, 1, 1]
        work = self.redis.load_work(MANIFEST)
        self.assertEqual([IMAGE], [image.source_url for image in work.images])
        self.assertNotEqual(IMAGE, work.images[0]._id)
        self.assertTrue(self.redis.ack(work))
        ack_call = self.redis.client.eval.call_args_list[-1]
        self.assertEqual(ACK_SCRIPT, ack_call.args[0])
        self.assertIn(MANIFEST, ack_call.args)
        self.assertIn(PAYLOAD, ack_call.args)
        self.assertIn("redis.call('DEL', unpack(KEYS, 4, #KEYS))", ACK_SCRIPT)

    def test_contract_type_errors_are_invalid_work(self):
        serialized = json.dumps([PAYLOAD], separators=(",", ":"))
        self.redis.client.hgetall.return_value = {
            "contract_version": "1",
            "publication_id": PUB,
            "normalized_url": PAGE,
            "image_count": "1",
            "image_keys": serialized,
        }
        pipeline = MagicMock()
        pipeline.execute.return_value = [{
            "contract_version": "1",
            "publication_id": PUB,
            "normalized_page_url": PAGE,
            "normalized_source_url": IMAGE,
            "alt": ["not", "text"],
        }]
        self.redis.client.pipeline.return_value = pipeline
        self.redis.client.eval.return_value = 1

        with self.assertRaises(InvalidImageWork):
            self.redis.load_work(MANIFEST)


if __name__ == "__main__":
    unittest.main()
