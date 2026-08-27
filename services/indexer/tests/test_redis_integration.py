import base64
import os
import sys
import unittest
import uuid
from pathlib import Path

import redis


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

from data.redis_client import RedisClient  # noqa: E402
from utils.constants import (  # noqa: E402
    IMAGE_INDEXER_QUEUE_KEY,
    INDEXER_DEAD_LETTER_QUEUE_KEY,
    INDEXER_FENCE_EPOCH_KEY,
    INDEXER_OWNER_LOCK_KEY,
    INDEXER_PROCESSING_QUEUE_KEY,
    INDEXER_QUEUE_KEY,
)


@unittest.skipUnless(
    os.getenv("INDEXER_REDIS_INTEGRATION_ADDR"),
    "INDEXER_REDIS_INTEGRATION_ADDR is not configured",
)
class RedisLifecycleIntegrationTests(unittest.TestCase):
    def setUp(self):
        address = os.environ["INDEXER_REDIS_INTEGRATION_ADDR"]
        url = address if "://" in address else f"redis://{address}/15"
        self.raw = redis.Redis.from_url(url, decode_responses=True)
        self.raw.ping()
        self.owner = RedisClient.__new__(RedisClient)
        self.owner.client = self.raw
        self.owner.owner_token = "integration-owner-" + uuid.uuid4().hex
        self.owner.owner_epoch = None
        self.keys = [
            INDEXER_QUEUE_KEY,
            INDEXER_PROCESSING_QUEUE_KEY,
            INDEXER_DEAD_LETTER_QUEUE_KEY,
            INDEXER_OWNER_LOCK_KEY,
            INDEXER_FENCE_EPOCH_KEY,
            IMAGE_INDEXER_QUEUE_KEY,
        ]
        self.raw.delete(*self.keys)

    def tearDown(self):
        self.raw.delete(*self.keys)
        self.raw.close()

    @staticmethod
    def page_key(publication, url):
        encoded = base64.urlsafe_b64encode(url.encode()).decode().rstrip("=")
        return f"page_data:{publication}:{encoded}"

    def test_a_claimed_b_published_a_completed_b_intact_and_old_owner_fenced(self):
        url = "https://example.org/integration"
        key_a = self.page_key("a" * 64, url)
        key_b = self.page_key("b" * 64, url)
        out_a = key_a.replace("page_data:", "outlinks:", 1)
        out_b = key_b.replace("page_data:", "outlinks:", 1)
        manifest_a = key_a.replace("page_data:", "page_images:", 1)
        manifest_b = key_b.replace("page_data:", "page_images:", 1)
        self.raw.hset(key_a, mapping={"publication_id": "a" * 64})
        self.raw.sadd(out_a, "https://example.org/a")
        self.raw.hset(manifest_a, mapping={
            "publication_id": "a" * 64,
            "normalized_url": url,
            "image_count": "0",
            "image_keys": "[]",
            "contract_version": "1",
        })
        self.raw.lpush(INDEXER_QUEUE_KEY, key_a)
        self.assertTrue(self.owner.acquire_lock())
        self.assertEqual(key_a, self.owner.claim_page())

        self.raw.hset(key_b, mapping={"publication_id": "b" * 64})
        self.raw.sadd(out_b, "https://example.org/b")
        self.raw.hset(manifest_b, mapping={
            "publication_id": "b" * 64,
            "normalized_url": url,
            "image_count": "0",
            "image_keys": "[]",
            "contract_version": "1",
        })
        self.raw.lpush(INDEXER_QUEUE_KEY, key_b)
        self.keys.extend([key_a, key_b, out_a, out_b, manifest_a, manifest_b])

        self.assertTrue(self.owner.complete_page(key_a, url))
        self.assertEqual(1, self.raw.exists(key_b))
        self.assertEqual(1, self.raw.exists(out_b))
        self.assertEqual([key_b], self.raw.lrange(INDEXER_QUEUE_KEY, 0, -1))
        self.assertEqual([manifest_a], self.raw.lrange(IMAGE_INDEXER_QUEUE_KEY, 0, -1))
        self.assertEqual(1, self.raw.exists(manifest_a))

        self.raw.lpush(INDEXER_PROCESSING_QUEUE_KEY, key_b)
        self.raw.delete(INDEXER_OWNER_LOCK_KEY)
        replacement = RedisClient.__new__(RedisClient)
        replacement.client = self.raw
        replacement.owner_token = "replacement-" + uuid.uuid4().hex
        replacement.owner_epoch = None
        self.assertTrue(replacement.acquire_lock())
        self.assertFalse(self.owner.complete_page(key_b, url))
        self.assertEqual(1, self.raw.exists(key_b))
        self.assertIn(key_b, self.raw.lrange(INDEXER_PROCESSING_QUEUE_KEY, 0, -1))

    def test_replacement_recovery_preserves_a_before_b_and_fences_old_claim(self):
        url = "https://example.org/order"
        key_a = self.page_key("c" * 64, url)
        key_b = self.page_key("d" * 64, url)
        self.raw.lpush(INDEXER_QUEUE_KEY, key_a)
        self.assertTrue(self.owner.acquire_lock())
        self.assertEqual(key_a, self.owner.claim_page())
        self.raw.lpush(INDEXER_QUEUE_KEY, key_b)
        self.keys.extend([key_a, key_b])

        self.raw.delete(INDEXER_OWNER_LOCK_KEY)
        replacement = RedisClient.__new__(RedisClient)
        replacement.client = self.raw
        replacement.owner_token = "replacement-" + uuid.uuid4().hex
        replacement.owner_epoch = None
        self.assertTrue(replacement.acquire_lock())
        self.assertGreater(replacement.owner_epoch, self.owner.owner_epoch)
        self.assertEqual(1, replacement.recover_abandoned_claims(limit=100))

        queue_before = self.raw.lrange(INDEXER_QUEUE_KEY, 0, -1)
        with self.assertRaises(redis.ResponseError):
            self.owner.claim_page()
        self.assertEqual(queue_before, self.raw.lrange(INDEXER_QUEUE_KEY, 0, -1))
        self.assertEqual(key_a, replacement.claim_page())
        self.assertTrue(replacement.release_page(key_a))
        self.assertEqual(key_a, replacement.claim_page())
        self.raw.lrem(INDEXER_PROCESSING_QUEUE_KEY, 1, key_a)
        self.assertEqual(key_b, replacement.claim_page())

    def test_epoch_counter_wrong_type_blocks_lock_acquisition(self):
        self.raw.rpush(INDEXER_FENCE_EPOCH_KEY, "wrong-type")
        with self.assertRaises(redis.ResponseError):
            self.owner.acquire_lock()


if __name__ == "__main__":
    unittest.main()
