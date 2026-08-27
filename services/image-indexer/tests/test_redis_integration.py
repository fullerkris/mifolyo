import base64
import json
import os
import subprocess
import sys
import unittest
import uuid
from pathlib import Path

import redis

from data.redis_client import MANIFEST_PREFLIGHT_SCRIPT, RedisClient
from utils.constants import (
    IMAGE_DEAD_LETTER_QUEUE_KEY,
    IMAGE_FENCE_EPOCH_KEY,
    IMAGE_INDEXER_QUEUE_KEY,
    IMAGE_OWNER_LOCK_KEY,
    IMAGE_PROCESSING_QUEUE_KEY,
    MAX_MANIFEST_BYTES,
)


@unittest.skipUnless(
    os.getenv("IMAGE_INDEXER_REDIS_INTEGRATION_ADDR"),
    "IMAGE_INDEXER_REDIS_INTEGRATION_ADDR is not configured",
)
class RedisLifecycleIntegrationTests(unittest.TestCase):
    def setUp(self):
        address = os.environ["IMAGE_INDEXER_REDIS_INTEGRATION_ADDR"]
        url = address if "://" in address else f"redis://{address}/15"
        self.redis_url = url
        self.raw = redis.Redis.from_url(url, decode_responses=True)
        self.raw.ping()
        self.raw.flushdb()
        self.owner = self.new_owner("owner")

    def tearDown(self):
        self.raw.flushdb()
        self.raw.close()

    def new_owner(self, prefix):
        owner = RedisClient.__new__(RedisClient)
        owner.client = self.raw
        owner.owner_token = prefix + "-" + uuid.uuid4().hex
        owner.owner_epoch = None
        return owner

    @staticmethod
    def encoded(value):
        return base64.urlsafe_b64encode(value.encode()).decode().rstrip("=")

    def publication(self, publication_id, page, image=None):
        page_encoded = self.encoded(page)
        manifest = f"page_images:{publication_id}:{page_encoded}"
        payloads = []
        if image:
            payload = f"image_data:{publication_id}:{page_encoded}:{self.encoded(image)}"
            payloads.append(payload)
            self.raw.hset(payload, mapping={
                "contract_version": "1",
                "publication_id": publication_id,
                "normalized_page_url": page,
                "normalized_source_url": image,
                "alt": "metadata",
            })
        self.raw.hset(manifest, mapping={
            "contract_version": "1",
            "publication_id": publication_id,
            "normalized_url": page,
            "image_count": str(len(payloads)),
            "image_keys": json.dumps(payloads, separators=(",", ":")),
        })
        return manifest, payloads

    def test_a_b_isolation_crash_recovery_ack_and_old_owner_fencing(self):
        page = "https://example.org/page"
        manifest_a, payload_a = self.publication("a" * 64, page, "https://cdn.example.org/a.jpg")
        manifest_b, payload_b = self.publication("b" * 64, page, "https://cdn.example.org/b.jpg")
        self.raw.lpush(IMAGE_INDEXER_QUEUE_KEY, manifest_a)
        self.assertTrue(self.owner.acquire_lock())
        self.assertEqual(manifest_a, self.owner.claim())
        self.raw.lpush(IMAGE_INDEXER_QUEUE_KEY, manifest_b)

        # Simulated crash/lease loss: A remains durable in processing.
        self.raw.delete(IMAGE_OWNER_LOCK_KEY)
        replacement = self.new_owner("replacement")
        self.assertTrue(replacement.acquire_lock())
        self.assertGreater(replacement.owner_epoch, self.owner.owner_epoch)
        with self.assertRaises(redis.ResponseError):
            self.owner.ack(self.owner.load_work(manifest_a))
        self.assertEqual(1 + len(payload_a), self.raw.exists(manifest_a, *payload_a))
        self.assertEqual(1, replacement.recover(100))
        self.assertEqual(manifest_a, replacement.claim())
        work_a = replacement.load_work(manifest_a)
        self.assertTrue(replacement.ack(work_a))
        self.assertEqual(0, self.raw.exists(manifest_a, *payload_a))
        self.assertEqual(1 + len(payload_b), self.raw.exists(manifest_b, *payload_b))
        self.assertEqual([manifest_b], self.raw.lrange(IMAGE_INDEXER_QUEUE_KEY, 0, -1))

    def test_empty_manifest_ack_and_corrupt_work_quarantine_preserve_inputs(self):
        page = "https://example.org/empty"
        empty, _ = self.publication("c" * 64, page)
        self.raw.lpush(IMAGE_INDEXER_QUEUE_KEY, empty)
        self.assertTrue(self.owner.acquire_lock())
        self.assertEqual(empty, self.owner.claim())
        self.assertTrue(self.owner.ack(self.owner.load_work(empty)))
        self.assertEqual(0, self.raw.exists(empty))

        corrupt, payloads = self.publication("d" * 64, page, "https://cdn.example.org/d.jpg")
        self.raw.hset(payloads[0], "publication_id", "e" * 64)
        self.raw.lpush(IMAGE_INDEXER_QUEUE_KEY, corrupt)
        self.assertEqual(corrupt, self.owner.claim())
        with self.assertRaises(Exception):
            self.owner.load_work(corrupt)
        self.assertTrue(self.owner.quarantine(corrupt))
        self.assertEqual([corrupt], self.raw.lrange(IMAGE_DEAD_LETTER_QUEUE_KEY, 0, -1))
        self.assertEqual(1 + len(payloads), self.raw.exists(corrupt, *payloads))
        self.assertEqual([], self.raw.lrange(IMAGE_PROCESSING_QUEUE_KEY, 0, -1))

    def test_real_indexer_completion_hands_immutable_manifest_to_image_indexer(self):
        page = "https://example.org/cross-service"
        publication_id = "f" * 64
        manifest, payloads = self.publication(
            publication_id, page, "https://cdn.example.org/cross.jpg"
        )
        page_key = f"page_data:{publication_id}:{self.encoded(page)}"
        self.raw.hset(page_key, mapping={"publication_id": publication_id})
        self.raw.lpush("pages_queue:processing", page_key)

        indexer_root = Path(__file__).resolve().parents[2] / "indexer"
        script = """
import os
import redis
from data.redis_client import RedisClient

raw = redis.Redis.from_url(os.environ['CROSS_REDIS_URL'], decode_responses=True)
owner = RedisClient.__new__(RedisClient)
owner.client = raw
owner.owner_token = 'cross-indexer-owner'
owner.owner_epoch = None
assert owner.acquire_lock()
assert owner.complete_page(os.environ['CROSS_PAGE_KEY'], os.environ['CROSS_PAGE_URL'])
owner.release_lock()
raw.close()
"""
        environment = os.environ.copy()
        environment.update(
            CROSS_REDIS_URL=self.redis_url,
            CROSS_PAGE_KEY=page_key,
            CROSS_PAGE_URL=page,
        )
        subprocess.run(
            [sys.executable, "-c", script],
            cwd=indexer_root,
            env=environment,
            check=True,
            capture_output=True,
            text=True,
            timeout=15,
        )

        self.assertEqual([manifest], self.raw.lrange(IMAGE_INDEXER_QUEUE_KEY, 0, -1))
        self.assertEqual(0, self.raw.exists(page_key))
        self.assertEqual(1 + len(payloads), self.raw.exists(manifest, *payloads))
        self.assertTrue(self.owner.acquire_lock())
        self.assertEqual(manifest, self.owner.claim())
        work = self.owner.load_work(manifest)
        self.assertEqual(publication_id, work.publication_id)
        self.assertTrue(self.owner.ack(work))

    def test_manifest_serialized_size_accepts_boundary_and_rejects_over_limit(self):
        key = "manifest-size-boundary"
        fields = {
            "contract_version": "1",
            "publication_id": "a" * 64,
            "normalized_url": "https://example.org",
            "image_count": "0",
            "image_keys": "x" * MAX_MANIFEST_BYTES,
        }
        self.raw.hset(key, mapping=fields)
        self.assertEqual(
            1,
            self.raw.eval(
                MANIFEST_PREFLIGHT_SCRIPT, 1, key, MAX_MANIFEST_BYTES
            ),
        )
        self.raw.hset(key, "image_keys", "x" * (MAX_MANIFEST_BYTES + 1))
        with self.assertRaises(redis.ResponseError):
            self.raw.eval(MANIFEST_PREFLIGHT_SCRIPT, 1, key, MAX_MANIFEST_BYTES)


if __name__ == "__main__":
    unittest.main()
