import base64
import sys
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

from data.redis_client import (  # noqa: E402
    ACQUIRE_LOCK_SCRIPT,
    CLAIM_PAGE_SCRIPT,
    COMPLETE_CLAIM_SCRIPT,
    QUARANTINE_CLAIM_SCRIPT,
    RECOVER_CLAIMS_SCRIPT,
    RELEASE_CLAIM_SCRIPT,
    InvalidPublicationKey,
    RedisClient,
    image_manifest_key_from_page_key,
    parse_page_publication_key,
    validate_redis_auth,
)
from utils.constants import (  # noqa: E402
    IMAGE_INDEXER_QUEUE_KEY,
    INDEXER_DEAD_LETTER_MAX_ENTRIES,
    INDEXER_DEAD_LETTER_QUEUE_KEY,
    INDEXER_FENCE_EPOCH_KEY,
    INDEXER_OWNER_LOCK_KEY,
    INDEXER_PROCESSING_QUEUE_KEY,
    INDEXER_QUEUE_KEY,
)


URL = "https://example.org/app"
ENCODED_URL = base64.urlsafe_b64encode(URL.encode()).decode().rstrip("=")
PAGE_KEY = f"page_data:{'a' * 64}:{ENCODED_URL}"
OUTLINKS_KEY = f"outlinks:{'a' * 64}:{ENCODED_URL}"
MANIFEST_KEY = f"page_images:{'a' * 64}:{ENCODED_URL}"


class RedisClientTests(unittest.TestCase):
    def setUp(self):
        self.redis = RedisClient.__new__(RedisClient)
        self.redis.client = MagicMock()
        self.redis.owner_token = "owner-a"
        self.redis.owner_epoch = 7

    def test_publication_key_validation_and_size_bound(self):
        publication, url, outlinks = parse_page_publication_key(PAGE_KEY)
        self.assertEqual("a" * 64, publication)
        self.assertEqual(URL, url)
        self.assertEqual(OUTLINKS_KEY, outlinks)
        invalid = [
            "page_data:https://example.org/app",
            "other:" + "a" * 64 + ":x",
            "page_data:" + "a" * 64 + ":" + "a" * 2732,
            "",
        ]
        for value in invalid:
            with self.assertRaises(InvalidPublicationKey):
                parse_page_publication_key(value)

    def test_redis_auth_fails_closed_without_explicit_local_opt_in(self):
        with self.assertRaises(ValueError):
            validate_redis_auth("", "", allow_insecure=False)
        validate_redis_auth("", "", allow_insecure=True)
        validate_redis_auth("", "password", allow_insecure=False)
        with patch("data.redis_client.redis.Redis") as client_factory:
            with self.assertRaises(ValueError):
                RedisClient()
            client_factory.assert_not_called()

    def test_successful_lock_acquisition_assigns_monotonic_epoch(self):
        self.redis.owner_epoch = None
        self.redis.client.eval.return_value = "8"
        self.assertTrue(self.redis.acquire_lock())
        self.assertEqual(8, self.redis.owner_epoch)
        call = self.redis.client.eval.call_args
        self.assertEqual(ACQUIRE_LOCK_SCRIPT, call.args[0])
        self.assertEqual(INDEXER_FENCE_EPOCH_KEY, call.args[3])
        self.assertIn("invalid fence epoch counter", ACQUIRE_LOCK_SCRIPT)

    def test_claim_is_one_owner_fenced_nonblocking_lua_operation(self):
        self.redis.client.eval.return_value = PAGE_KEY
        self.assertEqual(PAGE_KEY, self.redis.claim_page())
        self.redis.client.eval.assert_called_once_with(
            CLAIM_PAGE_SCRIPT,
            4,
            INDEXER_OWNER_LOCK_KEY,
            INDEXER_FENCE_EPOCH_KEY,
            INDEXER_QUEUE_KEY,
            INDEXER_PROCESSING_QUEUE_KEY,
            "owner-a:7",
            "7",
        )
        self.assertIn("RPOPLPUSH", CLAIM_PAGE_SCRIPT)
        self.assertNotIn("BRPOP", CLAIM_PAGE_SCRIPT)

    def test_recovery_is_owner_fenced_and_bounded(self):
        self.redis.client.eval.return_value = 100
        self.assertEqual(100, self.redis.recover_abandoned_claims(limit=100))
        call = self.redis.client.eval.call_args
        self.assertEqual(RECOVER_CLAIMS_SCRIPT, call.args[0])
        self.assertEqual(INDEXER_FENCE_EPOCH_KEY, call.args[3])
        self.assertEqual("owner-a:7", call.args[-3])
        self.assertIn("recovered < limit", RECOVER_CLAIMS_SCRIPT)

    def test_completion_is_fenced_allocates_before_ack_and_uses_exact_keys(self):
        self.redis.client.eval.side_effect = [1, 0]
        self.assertTrue(self.redis.complete_page(PAGE_KEY, URL))
        self.assertFalse(self.redis.complete_page(PAGE_KEY, URL))
        call = self.redis.client.eval.call_args_list[0]
        self.assertEqual(COMPLETE_CLAIM_SCRIPT, call.args[0])
        self.assertEqual(PAGE_KEY, call.args[5])
        self.assertEqual(OUTLINKS_KEY, call.args[6])
        self.assertEqual(MANIFEST_KEY, call.args[7])
        self.assertEqual(IMAGE_INDEXER_QUEUE_KEY, call.args[8])
        self.assertEqual(MANIFEST_KEY, image_manifest_key_from_page_key(PAGE_KEY))
        self.assertLess(
            COMPLETE_CLAIM_SCRIPT.index("redis.call('LPUSH', KEYS[7]"),
            COMPLETE_CLAIM_SCRIPT.index("redis.call('LREM', KEYS[3]"),
        )

    def test_release_and_quarantine_allocate_before_removal(self):
        self.redis.client.eval.return_value = 1
        self.assertTrue(self.redis.release_page(PAGE_KEY))
        self.assertLess(
            RELEASE_CLAIM_SCRIPT.index("redis.call('RPUSH'"),
            RELEASE_CLAIM_SCRIPT.index("redis.call('LREM'"),
        )
        self.redis.client.reset_mock()
        self.assertTrue(self.redis.quarantine_page("bad"))
        call = self.redis.client.eval.call_args
        self.assertEqual(QUARANTINE_CLAIM_SCRIPT, call.args[0])
        self.assertEqual(INDEXER_DEAD_LETTER_QUEUE_KEY, call.args[5])
        self.assertEqual(INDEXER_DEAD_LETTER_MAX_ENTRIES, call.args[-1])
        self.assertLess(
            QUARANTINE_CLAIM_SCRIPT.index("redis.call('LPUSH'"),
            QUARANTINE_CLAIM_SCRIPT.index("redis.call('LREM'"),
        )

    def test_stale_owner_script_failure_cannot_complete(self):
        self.redis.client.eval.side_effect = RuntimeError("LOCK_LOST")
        self.assertFalse(self.redis.complete_page(PAGE_KEY, URL))
        self.assertEqual("owner-a:7", self.redis.client.eval.call_args.args[-5])


if __name__ == "__main__":
    unittest.main()
