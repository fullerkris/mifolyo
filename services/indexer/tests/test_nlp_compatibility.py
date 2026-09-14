"""Offline literal goldens from pinned upstream, not from the replacement.

Run normally with unittest/pytest; no reference NLTK installation or data
download is needed. Every real-NLP check starts with clean modules and denies
network access and any installed-nltk fallback before application imports.
"""

import hashlib
from pathlib import Path
import sys
import unittest
from unittest.mock import patch


sys.path.insert(0, str(Path(__file__).resolve().parent))
from nlp_test_support import (  # noqa: E402
    SERVICE_ROOT, assert_component_nlp, assert_real_nlp, expected_html, fixture, run_offline,
    text_parts, token_list,
)


def check_component_tokens():
    """Direct component parity remains independently runnable during wiring."""
    test = unittest.TestCase()
    nlp = assert_component_nlp(test)
    baseline = fixture()
    for case in baseline["token_cases"]:
        test.assertEqual(token_list(case["raw_tokens"]), nlp.word_tokenize(text_parts(case["input_parts"])), case["id"])
    for case in baseline["chunk_cases"]:
        text = text_parts(case["input_parts"])
        test.assertEqual(token_list(case["unchunked_tokens"]), nlp.word_tokenize(text), case["id"])
        test.assertEqual(
            token_list(case["raw_tokens"]),
            [word for offset in range(0, len(text), 10000)
             for word in nlp.word_tokenize(text[offset:offset + 10000])],
            case["id"],
        )


def check_tokens(import_utils_first=False):
    if import_utils_first:
        from utils import utils  # noqa: F401
    test = unittest.TestCase()
    nlp, utils = assert_real_nlp(test)
    stop_words = nlp.english_stopwords()
    from bs4 import BeautifulSoup

    cases = fixture()["token_cases"]
    # Repeat in reverse order in the SAME child as well, exposing stateful/cache
    # contamination without relying on another test's prior initialization.
    for case in cases + list(reversed(cases)):
        text = text_parts(case["input_parts"])
        raw = token_list(case["raw_tokens"])
        filtered = token_list(case["filtered_tokens"])
        test.assertEqual(raw, nlp.word_tokenize(text), case["id"])
        test.assertEqual(
            filtered,
            [word.lower() for word in nlp.word_tokenize(text)
             if word.lower() not in stop_words and word.lower().isalnum()],
            case["id"],
        )
        # Exercise the real legacy process_text path, not just copied filtering.
        # A NavigableString prevents synthetic '<angle>' input becoming markup.
        soup = BeautifulSoup("<p></p>", "lxml")
        soup.p.append(text)
        result = utils.process_text(soup)
        test.assertEqual(filtered, result["filtered_text"], case["id"])
        test.assertEqual(" ".join(text.split()), result["summary_text"], case["id"])


def check_chunks():
    test = unittest.TestCase()
    nlp, utils = assert_real_nlp(test)
    for case in fixture()["chunk_cases"]:
        text = text_parts(case["input_parts"])
        test.assertEqual(case["character_count"], len(text), case["id"])
        test.assertEqual(case["utf8_byte_count"], len(text.encode("utf-8")), case["id"])
        test.assertEqual(token_list(case["unchunked_tokens"]), nlp.word_tokenize(text), case["id"])
        # Default argument must remain 10000 CHARACTERS, even through a word.
        raw = utils.tokenize_large_text(text)
        test.assertEqual(token_list(case["raw_tokens"]), raw, case["id"])
        test.assertEqual(
            token_list(case["filtered_tokens"]),
            [word.lower() for word in raw if word.lower() not in utils.stop_words_set and word.lower().isalnum()],
            case["id"],
        )


def check_html():
    test = unittest.TestCase()
    _, utils = assert_real_nlp(test)
    from bs4 import BeautifulSoup

    for case in fixture()["html_cases"]:
        html = text_parts(case["html_parts"])
        extracted = utils.extract_page_text(BeautifulSoup(html, "lxml"), rendered=case["rendered"])
        test.assertEqual(
            text_parts(case["page_text_parts"]),
            extracted,
            case["id"],
        )
        test.assertEqual(token_list(case["raw_tokens"]), utils.tokenize_large_text(extracted), case["id"])
        # Only classification is controlled. Parsing, summary, sample slicing,
        # tokenization and stopword filtering all use the real application.
        with patch.object(utils.langid, "classify", return_value=(case["language"], 0.99)) as classify:
            result = utils.get_html_data(html, rendered=case["rendered"])
        test.assertEqual(expected_html(case), result, case["id"])
        classify.assert_called_once_with(text_parts(case["language_sample_parts"]))
        test.assertLessEqual(len(result["summary_text"].split()), 500)
        test.assertLessEqual(len(text_parts(case["language_sample_parts"])), 1000)


def check_reference_downloads():
    """Synthetic HTTP/cache checks only; no reference imports or real network."""
    import importlib.util
    import io
    import tempfile

    test = unittest.TestCase()
    spec = importlib.util.spec_from_file_location(
        "reference_download_probe", SERVICE_ROOT / "tools/generate_nlp_reference.py"
    )
    reference = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(reference)
    test.assertEqual(
        {"nltk-source.tar.gz": 16 * 1024 * 1024, "punkt_tab.zip": 4319076, "stopwords.zip": 37733},
        reference.ARCHIVE_BYTE_LIMITS,
    )
    payload = b"synthetic archive"
    name = "stopwords.zip"
    url = "https://reference.invalid/pinned"

    class Response(io.BytesIO):
        def read(self, size=-1):
            test.assertEqual(len(payload) + 1, size)
            return super().read(size)

        def geturl(self):
            return url

    # Small synthetic sizes exercise the same bounds without corpus bytes.
    with patch.dict(reference.ARCHIVE_BYTE_LIMITS, {name: len(payload)}), \
            patch.dict(reference.ASSETS, {name: {"url": url, "sha256": hashlib.sha256(payload).hexdigest()}}):
        for label, body, error in (
            ("valid", payload, None),
            ("oversized", payload + b"!", "size"),
            ("truncated", payload[:-1], "size"),
            ("bad_hash", b"!" * len(payload), "hash"),
        ):
            with tempfile.TemporaryDirectory(prefix="nlp-download-test-") as scratch, \
                    patch.object(reference.urllib.request, "build_opener") as opener:
                directory = Path(scratch)
                destination = directory / name
                opener.return_value.open.return_value = Response(body)

                def authenticate(data):
                    test.assertFalse(destination.exists(), "Cache was written before authentication")
                    return hashlib.sha256(data).hexdigest()

                with patch.object(reference, "sha256", side_effect=authenticate):
                    if error:
                        with test.assertRaisesRegex(ValueError, error, msg=label):
                            reference.provision_archive(directory, name)
                        test.assertFalse(destination.exists(), label)
                    else:
                        test.assertEqual(payload, reference.provision_archive(directory, name))
                        test.assertEqual(payload, destination.read_bytes())
                test.assertIsInstance(opener.call_args.args[0], reference.NoRedirect)
                opener.return_value.open.assert_called_once_with(url, timeout=60)

        with tempfile.TemporaryDirectory(prefix="nlp-redirect-test-") as scratch, \
                patch.object(reference.urllib.request, "build_opener") as opener:
            response = Response(payload)
            response.geturl = lambda: "https://other.invalid/"
            opener.return_value.open.return_value = response
            with test.assertRaisesRegex(ValueError, "redirect"):
                reference.provision_archive(Path(scratch), name)
            test.assertFalse((Path(scratch) / name).exists())

        with tempfile.TemporaryDirectory(prefix="nlp-cache-test-") as scratch, \
                patch.object(reference.urllib.request, "build_opener") as opener:
            directory = Path(scratch)
            destination = directory / name
            for body in (payload, b"!" * len(payload)):
                destination.write_bytes(body)
                if body == payload:
                    test.assertEqual(payload, reference.provision_archive(directory, name))
                else:
                    with test.assertRaisesRegex(ValueError, "hash"):
                        reference.provision_archive(directory, name)
                test.assertEqual(body, destination.read_bytes(), "Existing cache was overwritten")
            opener.assert_not_called()

    with test.assertRaisesRegex(ValueError, "redirect"):
        reference.NoRedirect().redirect_request(None, None, 302, "", {}, "https://other.invalid/")


class NLPCompatibilityTests(unittest.TestCase):
    def test_fixture_records_independent_pinned_reference(self):
        data = fixture()
        self.assertEqual(2, data["schema_version"])
        source = data["provenance"]
        self.assertEqual("303f6e2ba8e4548a5f54fd65d86bb5c9a949f1db", source["source_commit"])
        self.assertEqual("550b6625bcef1f2abff2ff770a5a0d272c9c6b2a", source["data_commit"])
        self.assertEqual(5, len(source["data_files_sha256"]))
        self.assertTrue(all(source["reference_crosschecks"].values()))
        stopwords = data["english_stopwords"]
        self.assertEqual(
            {"count", "sorted_set_sha256", "canonicalization", "resource"}, set(stopwords)
        )
        self.assertEqual(198, stopwords["count"])
        self.assertRegex(stopwords["sorted_set_sha256"], r"^[0-9a-f]{64}$")
        self.assertEqual("UTF-8 of LF-joined sorted unique words, with a final LF", stopwords["canonicalization"])
        self.assertEqual("corpora/stopwords/english", stopwords["resource"])
        self.assertEqual(
            "f6d005956f407dbc6ea32e5ff0c7e8e6f71488d3239b9023efdc7fc139d6375b",
            source["data_files_sha256"][stopwords["resource"]],
        )
        self.assertEqual((15, 8, 10, 1), tuple(len(data[key]) for key in
                         ("token_cases", "chunk_cases", "html_cases", "postings_cases")))
        generator = SERVICE_ROOT / "tools/generate_nlp_reference.py"
        self.assertEqual(source["generator_sha256"], hashlib.sha256(generator.read_bytes()).hexdigest())

    def test_ordered_raw_tokens_and_real_filtering_nlp_first(self):
        run_offline(self, "from test_nlp_compatibility import check_tokens; check_tokens()")

    def test_component_api_matches_pinned_tokens_and_stopword_hashes(self):
        run_offline(self, "from test_nlp_compatibility import check_component_tokens; check_component_tokens()")

    def test_no_import_order_or_global_cache_dependency_utils_first(self):
        run_offline(self, "from test_nlp_compatibility import check_tokens; check_tokens(import_utils_first=True)")

    def test_default_character_chunk_boundaries(self):
        run_offline(self, "from test_nlp_compatibility import check_chunks; check_chunks()")

    def test_real_static_rendered_html_summary_and_language_sample(self):
        run_offline(self, "from test_nlp_compatibility import check_html; check_html()")

    def test_reference_download_bounds_authentication_and_cache_preservation(self):
        run_offline(self, "from test_nlp_compatibility import check_reference_downloads; check_reference_downloads()")


if __name__ == "__main__":
    unittest.main()
