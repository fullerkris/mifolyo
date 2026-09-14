"""Small, stdlib-only fixture reader and isolated offline test runner."""

import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys


SERVICE_ROOT = Path(__file__).resolve().parents[1]
FIXTURE_PATH = SERVICE_ROOT / "tests/fixtures/nlp-v1.json"


def fixture():
    return json.loads(FIXTURE_PATH.read_text(encoding="utf-8"))


def stopword_set_digest(words):
    return hashlib.sha256(("\n".join(sorted(words)) + "\n").encode("utf-8")).hexdigest()


def text_parts(parts):
    return "".join(part if isinstance(part, str) else part["text"] * part["repeat"] for part in parts)


def token_list(tokens):
    # Expands literal run-length encoding; never calls a tokenizer/oracle.
    return [token for item in tokens for token in
            ([item] if isinstance(item, str) else [item["token"]] * item["repeat"])]


def expected_html(case):
    expected = dict(case["expected"])
    expected["summary_text"] = text_parts(expected.pop("summary_parts"))
    expected["text"] = token_list(expected["text"])
    return expected


OFFLINE_BOOTSTRAP = r'''
import importlib.abc
from pathlib import Path
import socket
import sys

def deny_io(event, args):
    if event.startswith("socket.") or event in {
        "subprocess.Popen", "os.system", "os.posix_spawn"
    }:
        raise RuntimeError("NLP test denies network and child processes")

class RejectInstalledNLTK(importlib.abc.MetaPathFinder):
    def find_spec(self, fullname, path=None, target=None):
        if fullname == "nltk" or fullname.startswith("nltk."):
            raise ImportError("NLP test forbids installed NLTK fallback")

assert not any(name == "nltk" or name.startswith("nltk.") for name in sys.modules)
assert "utils.nlp_utils" not in sys.modules
sys.addaudithook(deny_io)
sys.meta_path.insert(0, RejectInstalledNLTK())
# Prove the guard is active, without opening a socket or loading installed NLTK.
try:
    socket.socket()
except RuntimeError as error:
    assert "denies network" in str(error)
else:
    raise AssertionError("Offline guard failed")
try:
    __import__("nltk")
except ImportError as error:
    assert "forbids installed NLTK" in str(error)
else:
    raise AssertionError("NLTK fallback guard failed")
root = Path(sys.argv[1]).resolve()
sys.path[:0] = [str(root), str(root / "tests")]
'''


def run_offline(testcase, code):
    """Guards/caches/signals belong only to this child, never the test collector."""
    result = subprocess.run(
        [sys.executable, "-I", "-B", "-c", OFFLINE_BOOTSTRAP + "\n" + code, str(SERVICE_ROOT)],
        cwd=SERVICE_ROOT,
        env={**os.environ, "NLTK_DATA": "/nonexistent/nlp-regression-no-user-data"},
        capture_output=True,
        text=True,
        timeout=60,
        check=False,
    )
    testcase.assertEqual(
        0, result.returncode,
        f"Offline NLP worker failed ({code}):\nstdout:\n{result.stdout}\nstderr:\n{result.stderr}",
    )


def assert_component_nlp(testcase):
    import nlp

    testcase.assertTrue(Path(nlp.__file__).resolve().is_relative_to(SERVICE_ROOT / "nlp"))
    stop_words = nlp.english_stopwords()
    testcase.assertIsInstance(stop_words, frozenset)
    baseline = fixture()
    stopword_identity = baseline["english_stopwords"]
    testcase.assertEqual(stopword_identity["count"], len(stop_words))
    testcase.assertEqual(
        stopword_identity["sorted_set_sha256"],
        stopword_set_digest(stop_words),
    )
    # Hash the actual privately provisioned, ordered resource as well as the
    # public API's set. Corpus bytes never enter a fixture or an assertion diff.
    resource = Path(nlp.__file__).resolve().parent / "data/english_stopwords.txt"
    testcase.assertEqual(
        baseline["provenance"]["data_files_sha256"][stopword_identity["resource"]],
        hashlib.sha256(resource.read_bytes()).hexdigest(),
    )
    testcase.assertFalse(any(name == "nltk" or name.startswith("nltk.") for name in sys.modules))
    return nlp


def assert_real_nlp(testcase):
    nlp = assert_component_nlp(testcase)
    stopword_identity = fixture()["english_stopwords"]
    from utils import utils

    testcase.assertEqual(stopword_identity["count"], len(utils.stop_words_set))
    testcase.assertEqual(stopword_identity["sorted_set_sha256"], stopword_set_digest(utils.stop_words_set))
    testcase.assertIs(nlp.word_tokenize, utils.word_tokenize)
    testcase.assertFalse(any(name == "nltk" or name.startswith("nltk.") for name in sys.modules))
    return nlp, utils
