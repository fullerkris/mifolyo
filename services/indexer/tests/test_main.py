import base64
import logging
import signal
import sys
import unittest
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import ANY, MagicMock, patch


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

# Importing main installs process-wide handlers. Collection must not change the
# host test runner's SIGINT/SIGTERM behavior (including other integration tests).
_signal_handlers = {sig: signal.getsignal(sig) for sig in (signal.SIGINT, signal.SIGTERM)}
try:
    import main as indexer_main  # noqa: E402
finally:
    for _signal, _handler in _signal_handlers.items():
        signal.signal(_signal, _handler)
from data.redis_client import PageDataDecodeError  # noqa: E402
from models.outlinks import Outlinks  # noqa: E402
from models.page import Page  # noqa: E402


URL = "https://example.org/app"
ENCODED_URL = base64.urlsafe_b64encode(URL.encode()).decode().rstrip("=")
PAGE_KEY = f"page_data:{'a' * 64}:{ENCODED_URL}"


class IndexerMainTests(unittest.TestCase):
    @staticmethod
    def page(rendered=False):
        return Page(
            normalized_url=URL,
            html="<html><main>hello world</main></html>",
            content_type="text/html",
            status_code=200,
            last_crawled=datetime.now(timezone.utc),
            rendered=rendered,
            original_html="<div id='root'></div>" if rendered else "",
            render_policy_rule="fixture" if rendered else "",
            render_policy_sha256="a" * 64 if rendered else "",
        )

    def configured_clients(self):
        redis = MagicMock()
        redis.owner_token = "redis-owner"
        redis.owner_epoch = 7
        redis.renew_lock.return_value = True
        redis.get_page_data.return_value = self.page()
        redis.get_outlinks.return_value = Outlinks(_id=URL, links=set())
        redis.complete_page.return_value = True
        redis.skip_page.return_value = True
        mongo = MagicMock()
        mongo.replace_search_state.return_value = True
        mongo.remove_search_state.return_value = True
        mongo.save_page_artifact.return_value = True
        mongo.acquire_canonical_lock.return_value = True
        mongo.owns_canonical_lock.return_value = True
        mongo.release_canonical_lock.return_value = True
        return redis, mongo

    @patch.object(indexer_main, "split_url", return_value=[])
    @patch.object(
        indexer_main,
        "get_html_data",
        return_value={
            "language": "en",
            "text": ["hello", "world"],
            "title": "Title",
            "description": "Description",
            "summary_text": "hello world",
        },
    )
    def test_redis_ack_occurs_only_after_complete_mongo_reconciliation(self, _html, _split):
        redis, mongo = self.configured_clients()
        events = []
        mongo.replace_search_state.side_effect = lambda *_, **__: events.append("mongo") or True
        redis.complete_page.side_effect = lambda *_: events.append("redis") or True

        self.assertEqual(
            indexer_main.COMPLETED,
            indexer_main.process_claim(redis, mongo, PAGE_KEY),
        )
        self.assertEqual(["mongo", "redis"], events)
        redis.get_outlinks.assert_called_once_with(PAGE_KEY, URL)

    @patch.object(indexer_main, "split_url", return_value=[])
    @patch.object(
        indexer_main,
        "get_html_data",
        return_value={
            "language": "en",
            "text": ["hello"],
            "title": "Title",
            "description": "Description",
            "summary_text": "hello",
        },
    )
    def test_mongo_failure_leaves_claim_unacked(self, _html, _split):
        redis, mongo = self.configured_clients()
        mongo.replace_search_state.return_value = False

        self.assertEqual(
            indexer_main.TRANSIENT_FAILURE,
            indexer_main.process_claim(redis, mongo, PAGE_KEY),
        )
        redis.complete_page.assert_not_called()
        redis.skip_page.assert_not_called()

    @patch.object(
        indexer_main,
        "get_html_data",
        return_value={"language": "fr", "text": ["bonjour"]},
    )
    def test_permanent_skip_removes_prior_mongo_state_before_redis_ack(self, _html):
        redis, mongo = self.configured_clients()
        events = []
        mongo.remove_search_state.side_effect = lambda *_, **__: events.append("mongo-remove") or True
        redis.skip_page.side_effect = lambda *_: events.append("redis-skip") or True

        self.assertEqual(
            indexer_main.SKIPPED,
            indexer_main.process_claim(redis, mongo, PAGE_KEY),
        )
        self.assertEqual(["mongo-remove", "redis-skip"], events)
        lock_token = indexer_main.MongoClient.lock_token("redis-owner", "a" * 64)
        mongo.remove_search_state.assert_called_once_with(
            URL, 7, lock_token, owner_check=ANY
        )

    @patch.object(
        indexer_main,
        "get_html_data",
        return_value={"language": "fr", "text": ["bonjour"]},
    )
    def test_rendered_artifact_is_retained_before_non_indexable_cleanup(self, _html):
        redis, mongo = self.configured_clients()
        redis.get_page_data.return_value = self.page(rendered=True)

        self.assertEqual(
            indexer_main.SKIPPED,
            indexer_main.process_claim(redis, mongo, PAGE_KEY),
        )
        lock_token = indexer_main.MongoClient.lock_token("redis-owner", "a" * 64)
        mongo.save_page_artifact.assert_called_once_with(
            redis.get_page_data.return_value,
            7,
            lock_token,
            owner_check=ANY,
        )
        mongo.remove_search_state.assert_called_once_with(
            URL, 7, lock_token, owner_check=ANY
        )

    def test_absent_payload_is_quarantined_without_mongo_mutation(self):
        redis, mongo = self.configured_clients()
        redis.get_page_data.return_value = None
        redis.quarantine_page.return_value = True

        self.assertEqual(
            indexer_main.QUARANTINED,
            indexer_main.process_claim(redis, mongo, PAGE_KEY),
        )
        redis.quarantine_page.assert_called_once_with(PAGE_KEY)
        mongo.remove_search_state.assert_not_called()
        mongo.replace_search_state.assert_not_called()

    def test_malformed_payload_is_quarantined_without_mongo_mutation(self):
        redis, mongo = self.configured_clients()
        redis.get_page_data.side_effect = PageDataDecodeError("bad", URL)
        redis.quarantine_page.return_value = True

        self.assertEqual(
            indexer_main.QUARANTINED,
            indexer_main.process_claim(redis, mongo, PAGE_KEY),
        )
        mongo.remove_search_state.assert_not_called()
        mongo.replace_search_state.assert_not_called()

    @patch.object(indexer_main, "split_url", return_value=[])
    @patch.object(
        indexer_main,
        "get_html_data",
        return_value={
            "language": "en",
            "text": ["hello"],
            "title": "Title",
            "description": "Description",
            "summary_text": "hello",
        },
    )
    def test_duplicate_notification_is_quarantined_without_deleting_index(self, _html, _split):
        redis, mongo = self.configured_clients()
        redis.get_page_data.side_effect = [self.page(), None]
        redis.quarantine_page.return_value = True

        self.assertEqual(indexer_main.COMPLETED, indexer_main.process_claim(redis, mongo, PAGE_KEY))
        self.assertEqual(indexer_main.QUARANTINED, indexer_main.process_claim(redis, mongo, PAGE_KEY))
        mongo.replace_search_state.assert_called_once()
        mongo.remove_search_state.assert_not_called()

    @patch.object(indexer_main, "process_claim", return_value=indexer_main.TRANSIENT_FAILURE)
    def test_transient_failure_gets_one_owner_fenced_release_then_exits(self, process):
        redis = MagicMock()
        redis.acquire_lock.return_value = True
        redis.renew_lock.return_value = True
        redis.recover_abandoned_claims.return_value = 0
        redis.get_work_sizes.return_value = (1, 0)
        redis.claim_page.return_value = PAGE_KEY
        original_running = indexer_main.running
        indexer_main.running = True
        try:
            self.assertEqual(1, indexer_main.run_indexer(redis, MagicMock()))
        finally:
            indexer_main.running = original_running
        redis.release_page.assert_called_once_with(PAGE_KEY)
        redis.release_lock.assert_called_once()

    @patch.object(indexer_main, "process_claim")
    def test_sigterm_during_claim_leaves_processing_entry(self, process):
        redis = MagicMock()
        redis.acquire_lock.return_value = True
        redis.renew_lock.return_value = True
        redis.recover_abandoned_claims.return_value = 0
        redis.get_work_sizes.return_value = (1, 0)

        def claim_and_signal():
            indexer_main.handle_exit(None, None)
            return PAGE_KEY

        redis.claim_page.side_effect = claim_and_signal
        original_running = indexer_main.running
        indexer_main.running = True
        try:
            self.assertEqual(0, indexer_main.run_indexer(redis, MagicMock()))
        finally:
            indexer_main.running = original_running
        process.assert_not_called()
        redis.release_page.assert_not_called()

    def test_overlapping_instance_waits_and_sigterm_stops_boundedly(self):
        redis = MagicMock()
        redis.acquire_lock.return_value = False
        sleeps = []

        def stop(delay):
            sleeps.append(delay)
            indexer_main.handle_exit(None, None)

        original_running = indexer_main.running
        indexer_main.running = True
        try:
            self.assertEqual(0, indexer_main.run_indexer(redis, MagicMock(), sleep=stop))
        finally:
            indexer_main.running = original_running
        self.assertEqual([indexer_main.LOCK_WAIT_SECONDS], sleeps)
        redis.recover_abandoned_claims.assert_not_called()

    def test_empty_claim_poll_is_short_and_sigterm_bounded(self):
        redis = MagicMock()
        redis.acquire_lock.return_value = True
        redis.renew_lock.return_value = True
        redis.recover_abandoned_claims.return_value = 0
        redis.get_work_sizes.return_value = (0, 0)
        redis.signal_crawler.return_value = True
        redis.claim_page.return_value = None
        sleeps = []

        def stop(delay):
            sleeps.append(delay)
            indexer_main.handle_exit(None, None)

        original_running = indexer_main.running
        indexer_main.running = True
        try:
            self.assertEqual(0, indexer_main.run_indexer(redis, MagicMock(), sleep=stop))
        finally:
            indexer_main.running = original_running
        self.assertEqual([indexer_main.INDEXER_CLAIM_POLL_SECONDS], sleeps)
        self.assertLessEqual(sleeps[0], 1)

    @patch.object(indexer_main, "process_claim")
    def test_invalid_queue_value_is_quarantined_not_read_as_key(self, process):
        redis = MagicMock()
        redis.acquire_lock.return_value = True
        redis.renew_lock.return_value = True
        redis.recover_abandoned_claims.return_value = 0
        redis.get_work_sizes.return_value = (1, 0)
        redis.claim_page.side_effect = ["not-a-publication", RuntimeError("stop")]
        redis.quarantine_page.return_value = True
        original_running = indexer_main.running
        indexer_main.running = True
        try:
            self.assertEqual(1, indexer_main.run_indexer(redis, MagicMock()))
        finally:
            indexer_main.running = original_running
        redis.quarantine_page.assert_called_once_with("not-a-publication")
        process.assert_not_called()

    def test_recovery_uses_bounded_batches(self):
        redis = MagicMock()
        redis.renew_lock.return_value = True
        redis.recover_abandoned_claims.side_effect = [
            indexer_main.INDEXER_RECOVERY_BATCH_SIZE,
            2,
        ]

        self.assertTrue(indexer_main.recover_claims(redis))
        self.assertEqual(2, redis.recover_abandoned_claims.call_count)
        redis.recover_abandoned_claims.assert_called_with(
            limit=indexer_main.INDEXER_RECOVERY_BATCH_SIZE
        )

    def test_insecure_datastore_opt_in_requires_exact_true(self):
        for value, expected in [("true", True), ("TRUE", False), ("1", False), ("", False)]:
            with self.subTest(value=value), patch.dict(
                indexer_main.os.environ,
                {indexer_main.ALLOW_INSECURE_DATASTORES_ENV: value},
                clear=True,
            ):
                self.assertEqual(expected, indexer_main.allow_insecure_datastores())

    def test_claim_failure_log_omits_url_and_page_key(self):
        redis, mongo = self.configured_clients()
        redis.get_page_data.side_effect = RuntimeError("failure involving " + URL)

        with self.assertLogs(indexer_main.logger, level=logging.ERROR) as captured:
            self.assertEqual(
                indexer_main.TRANSIENT_FAILURE,
                indexer_main.process_claim(redis, mongo, PAGE_KEY),
            )

        output = "\n".join(captured.output)
        self.assertNotIn(URL, output)
        self.assertNotIn(PAGE_KEY, output)
        self.assertIn("claim=", output)


def check_real_nlp_postings():
    from nlp_test_support import assert_real_nlp, expected_html, fixture, text_parts

    test = IndexerMainTests()
    _, utils = assert_real_nlp(test)
    baseline = fixture()
    postings = baseline["postings_cases"][0]
    case = next(case for case in baseline["html_cases"] if case["id"] == postings["html_case"])
    redis, mongo = test.configured_clients()
    page = redis.get_page_data.return_value
    page.html = text_parts(case["html_parts"])
    page.normalized_url = postings["url"]
    encoded = base64.urlsafe_b64encode(page.normalized_url.encode()).decode().rstrip("=")
    key = f"page_data:{'a' * 64}:{encoded}"
    outlinks = Outlinks(_id=page.normalized_url, links={"https://target.example/path"})
    redis.get_outlinks.return_value = outlinks
    events = []
    mongo.replace_search_state.side_effect = lambda *_, **__: events.append("replace") or True
    redis.complete_page.side_effect = lambda *_: events.append("ack") or True

    with patch.object(utils.langid, "classify", return_value=("en", 0.99)) as classify:
        test.assertEqual(indexer_main.COMPLETED, indexer_main.process_claim(redis, mongo, key))

    # Literal counts independently reviewed against the reference token golden:
    # echo=3, beta=2; each of THREE URL echo tokens multiplies by 50. An absent
    # URL token starts at 10, so the second 'new' multiplies that 10 by 50.
    keywords = {"echo": 375000, "beta": 2, "café": 1, "ca": 1, "https": 10, "example": 10, "new": 500}
    test.assertEqual(keywords, postings["keywords"])
    test.assertEqual(postings["url_tokens"], utils.split_url(page.normalized_url))
    token = indexer_main.MongoClient.lock_token("redis-owner", "a" * 64)
    mongo.replace_search_state.assert_called_once_with(
        page, expected_html(case), keywords, outlinks, 7, token, owner_check=ANY
    )
    test.assertEqual(list(keywords), list(mongo.replace_search_state.call_args.args[2]))
    test.assertEqual(["replace", "ack"], events)
    classify.assert_called_once_with(text_parts(case["language_sample_parts"]))
    redis.get_outlinks.assert_called_once_with(key, page.normalized_url)
    redis.complete_page.assert_called_once_with(key, page.normalized_url)
    redis.skip_page.assert_not_called()
    redis.quarantine_page.assert_not_called()
    mongo.remove_search_state.assert_not_called()
    mongo.save_page_artifact.assert_not_called()
    mongo.release_canonical_lock.assert_called_once_with(page.normalized_url, token)


def check_real_nlp_permanent_skips():
    from nlp_test_support import assert_real_nlp, fixture, text_parts

    test = IndexerMainTests()
    _, utils = assert_real_nlp(test)
    selected = {"static_without_paragraphs", "stopword_html", "non_english_rendered"}
    for case in fixture()["html_cases"]:
        if case["id"] not in selected:
            continue
        redis, mongo = test.configured_clients()
        page = test.page(rendered=case["rendered"])
        page.html = text_parts(case["html_parts"])
        redis.get_page_data.return_value = page
        events = []
        mongo.save_page_artifact.side_effect = lambda *_, **__: events.append("artifact") or True
        mongo.remove_search_state.side_effect = lambda *_, **__: events.append("remove") or True
        redis.skip_page.side_effect = lambda *_: events.append("skip") or True
        with patch.object(utils.langid, "classify", return_value=(case["language"], 0.99)) as classify:
            test.assertEqual(indexer_main.SKIPPED, indexer_main.process_claim(redis, mongo, PAGE_KEY), case["id"])
        token = indexer_main.MongoClient.lock_token("redis-owner", "a" * 64)
        mongo.remove_search_state.assert_called_once_with(URL, 7, token, owner_check=ANY)
        test.assertEqual((["artifact"] if case["rendered"] else []) + ["remove", "skip"], events)
        if case["rendered"]:
            mongo.save_page_artifact.assert_called_once_with(page, 7, token, owner_check=ANY)
        else:
            mongo.save_page_artifact.assert_not_called()
        classify.assert_called_once_with(text_parts(case["language_sample_parts"]))
        mongo.replace_search_state.assert_not_called()
        redis.get_outlinks.assert_not_called()
        redis.skip_page.assert_called_once_with(PAGE_KEY)
        redis.complete_page.assert_not_called()
        redis.quarantine_page.assert_not_called()
        mongo.release_canonical_lock.assert_called_once_with(URL, token)


def check_real_nlp_exception():
    from nlp_test_support import assert_real_nlp

    test = IndexerMainTests()
    nlp, utils = assert_real_nlp(test)
    for rendered, failure_type in (
        (False, RuntimeError), (True, RuntimeError),
        (False, nlp.NLPDataError), (True, nlp.NLPDataError),
    ):
        redis, mongo = test.configured_clients()
        page = test.page(rendered=rendered)
        # The first chunk succeeds with REAL NLP; the second fails. No partial
        # postings/cleanup/ACK is allowed, including the component's data-error
        # type. Rendered artifacts still precede NLP; no corpus files are changed.
        page.html = "<main><p>" + "echo " * 2001 + "</p></main>"
        redis.get_page_data.return_value = page
        calls = []

        def fail_second_chunk(text):
            calls.append(text)
            if len(calls) == 2:
                raise failure_type("synthetic NLP failure")
            return nlp.word_tokenize(text)

        with patch.object(utils, "word_tokenize", side_effect=fail_second_chunk), \
                patch.object(utils.langid, "classify") as classify, \
                test.assertLogs(indexer_main.logger, level=logging.ERROR):
            test.assertEqual(indexer_main.TRANSIENT_FAILURE, indexer_main.process_claim(redis, mongo, PAGE_KEY))
        test.assertEqual(2, len(calls))
        test.assertEqual(10000, len(calls[0]))
        classify.assert_not_called()
        mongo.replace_search_state.assert_not_called()
        mongo.remove_search_state.assert_not_called()
        redis.get_outlinks.assert_not_called()
        for method in (redis.complete_page, redis.skip_page, redis.quarantine_page, redis.release_page):
            method.assert_not_called()
        token = indexer_main.MongoClient.lock_token("redis-owner", "a" * 64)
        mongo.release_canonical_lock.assert_called_once_with(URL, token)
        if rendered:
            mongo.save_page_artifact.assert_called_once_with(page, 7, token, owner_check=ANY)
        else:
            mongo.save_page_artifact.assert_not_called()


class IndexerRealNLPTests(unittest.TestCase):
    def run_check(self, name):
        # Imports/audit hooks/caches/signals remain in the short-lived child.
        sys.path.insert(0, str(Path(__file__).resolve().parent))
        from nlp_test_support import run_offline

        run_offline(self, f"from test_main import {name}; {name}()")

    def test_real_nlp_counter_url_boosts_and_ack_order(self):
        self.run_check("check_real_nlp_postings")

    def test_real_nlp_empty_and_non_english_remove_state_before_skip(self):
        self.run_check("check_real_nlp_permanent_skips")

    def test_nlp_exception_leaves_search_state_and_claim_unmodified(self):
        self.run_check("check_real_nlp_exception")


if __name__ == "__main__":
    unittest.main()
