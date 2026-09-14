#!/usr/bin/env python3
"""Maintenance-only reference; normal tests never execute its NLTK oracle.

Provision with --provision --workdir <approved scratch directory>. Install the
five pinned REFERENCE_DEPENDENCIES in a scratch venv (not the application).
Then run that venv's Python with -I -B, this script, --workdir and --output.
Generation denies sockets/subprocesses/unpickling before importing NLTK, uses
only the pinned source tree and five frozen plaintext data files, and blocks
nltk.download. No current application or candidate modules are imported.

The JSON is a reviewed, checked-in literal fixture, not an oracle calculated
by the candidate. --check <fixture> verifies a previous generation byte-for-byte.
Its repository path is services/indexer/tests/fixtures/nlp-v1.json. Only hashes
and synthetic inputs/outputs may be distributed: never emit the full stopword
list or corpus bytes. Private provisioning does not resolve redistribution rights.
All text/HTML below is synthetic. HTML expectations explicitly describe the
existing extraction contract; NLP outputs come ONLY from upstream NLTK.
"""

import argparse
from collections import Counter
import hashlib
import importlib.metadata
import itertools
import json
import os
from pathlib import Path
import platform
import re
import sys
import tarfile
import unicodedata
import urllib.request
import zipfile


SOURCE_COMMIT = "303f6e2ba8e4548a5f54fd65d86bb5c9a949f1db"
DATA_COMMIT = "550b6625bcef1f2abff2ff770a5a0d272c9c6b2a"
REFERENCE_DEPENDENCIES = {
    "defusedxml": "0.7.1",
    "click": "8.1.8",
    "joblib": "1.4.2",
    "regex": "2024.11.6",
    "tqdm": "4.67.0",
}
ASSETS = {
    "nltk-source.tar.gz": {
        "url": f"https://codeload.github.com/nltk/nltk/tar.gz/{SOURCE_COMMIT}",
        "sha256": "b3e1f694b8b83f5dab29669ebeb48d329e571f0b39f2e863f825903985f23972",
    },
    "punkt_tab.zip": {
        "url": f"https://raw.githubusercontent.com/nltk/nltk_data/{DATA_COMMIT}/packages/tokenizers/punkt_tab.zip",
        "sha256": "e57f64187974277726a3417ca6f181ec5403676c717672eef6a748a7b20e0106",
    },
    "stopwords.zip": {
        "url": f"https://raw.githubusercontent.com/nltk/nltk_data/{DATA_COMMIT}/packages/corpora/stopwords.zip",
        "sha256": "48c0e52d8b52546e827f53761fb30300c0ab94f70660d28bd65ba0a86270946b",
    },
}
ARCHIVE_BYTE_LIMITS = {
    "nltk-source.tar.gz": 16 * 1024 * 1024,
    "punkt_tab.zip": 4319076,
    "stopwords.zip": 37733,
}
DATA_MEMBERS = {
    "punkt_tab.zip": {
        f"punkt_tab/english/{name}": f"tokenizers/punkt_tab/english/{name}"
        for name in (
            "abbrev_types.txt", "collocations.tab", "ortho_context.tab", "sent_starters.txt"
        )
    },
    "stopwords.zip": {"stopwords/english": "corpora/stopwords/english"},
}
SOURCE_FILES = (
    "nltk/VERSION", "nltk/__init__.py", "nltk/tokenize/__init__.py",
    "nltk/tokenize/punkt.py", "nltk/tokenize/destructive.py",
    "nltk/tokenize/treebank.py", "nltk/tabdata.py", "nltk/data.py",
    "nltk/corpus/reader/wordlist.py", "LICENSE.txt",
)


def sha256(data):
    return hashlib.sha256(data).hexdigest()


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ValueError("Refusing redirect away from the literal pinned input URL")


def read_verified_archive(name, stream):
    limit = ARCHIVE_BYTE_LIMITS[name]
    # A single excess byte detects oversized responses without unbounded reads.
    data = stream.read(limit + 1)
    if len(data) > limit or (name.endswith(".zip") and len(data) != limit):
        raise ValueError(f"Archive size mismatch: {name}")
    if sha256(data) != ASSETS[name]["sha256"]:
        raise ValueError(f"Archive hash mismatch: {name}")
    return data


def read_cached_archive(workdir, name):
    path = workdir / name
    if path.is_symlink() or not path.is_file():
        raise ValueError(f"Missing or non-regular reference archive: {name}")
    with path.open("rb") as stream:
        return read_verified_archive(name, stream)


def provision_archive(workdir, name):
    destination = workdir / name
    if destination.exists() or destination.is_symlink():
        # A corrupt cache is an error, not permission to fetch/overwrite it.
        return read_cached_archive(workdir, name)
    url = ASSETS[name]["url"]
    with urllib.request.build_opener(NoRedirect()).open(url, timeout=60) as response:
        if response.geturl() != url:
            raise ValueError(f"Unexpected download redirect: {name}")
        data = read_verified_archive(name, response)
    # Authenticate all bytes BEFORE creating a cache file. Exclusive creation
    # also refuses to overwrite a file that appeared while the fetch ran.
    with destination.open("xb") as output:
        output.write(data)
    return data


def provision(workdir):
    """Only explicit --provision permits network access to the pinned assets."""
    workdir.mkdir(parents=False, exist_ok=True)
    for name in ASSETS:
        print(f"{name}: {sha256(provision_archive(workdir, name))}")

    # Only regular files underneath the expected archive root; never extractall.
    prefix = f"nltk-{SOURCE_COMMIT}/"
    with tarfile.open(workdir / "nltk-source.tar.gz", "r:gz") as archive:
        for member in archive.getmembers():
            if not member.isfile():
                continue
            if not member.name.startswith(prefix):
                raise ValueError(f"Unexpected source archive member: {member.name}")
            relative = Path(member.name[len(prefix):])
            if relative.is_absolute() or ".." in relative.parts:
                raise ValueError("Unsafe source archive path")
            target = workdir / "source" / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(archive.extractfile(member).read())

    for archive_name, members in DATA_MEMBERS.items():
        with zipfile.ZipFile(workdir / archive_name) as archive:
            for member, relative in members.items():
                target = workdir / "data" / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(archive.read(member))


def deny_network_and_pickle():
    import pickle
    import _pickle

    def denied(*args, **kwargs):
        raise RuntimeError("Reference forbids network, downloads, and unpickling")

    def audit(event, args):
        if event.startswith("socket.") or event in {
            "subprocess.Popen", "os.system", "os.posix_spawn", "pickle.find_class"
        }:
            denied()

    sys.addaudithook(audit)
    pickle.load = pickle.loads = _pickle.load = _pickle.loads = denied
    return denied


def frozen_reference(workdir):
    """Validate archives AND extracted bytes before permitting reference imports."""
    for name in ASSETS:
        read_cached_archive(workdir, name)
    source_root = (workdir / "source").resolve()
    prefix = f"nltk-{SOURCE_COMMIT}/"
    with tarfile.open(workdir / "nltk-source.tar.gz", "r:gz") as archive:
        source_members = {
            item.name[len(prefix):]: archive.extractfile(item).read()
            for item in archive.getmembers()
            if item.isfile() and item.name.startswith(prefix)
        }
    # Every extracted source byte, not just the tokenizer, is checked. In
    # particular, no extra .pyc files can override this verified source.
    actual_source = {
        path.relative_to(source_root).as_posix()
        for path in source_root.rglob("*") if path.is_file()
    }
    if actual_source != set(source_members):
        raise ValueError("Missing or extra reference source files")
    for relative, content in source_members.items():
        if (source_root / relative).read_bytes() != content:
            raise ValueError(f"Missing or modified source: {relative}")

    data_root = (workdir / "data").resolve()
    data_hashes = {}
    for archive_name, members in DATA_MEMBERS.items():
        with zipfile.ZipFile(workdir / archive_name) as archive:
            for member, relative in members.items():
                content = (data_root / relative).read_bytes()
                if content != archive.read(member):
                    raise ValueError(f"Modified reference data: {relative}")
                data_hashes[relative] = sha256(content)
    actual_files = {
        path.relative_to(data_root).as_posix()
        for path in data_root.rglob("*") if path.is_file()
    }
    if actual_files != set(data_hashes):
        raise ValueError("Reference data must contain exactly five plaintext files")
    for package, version in REFERENCE_DEPENDENCIES.items():
        if importlib.metadata.version(package) != version:
            raise ValueError(f"Install reference dependency {package}=={version}")
    denied = deny_network_and_pickle()
    os.environ["NLTK_DATA"] = str(data_root)
    sys.path.insert(0, str(source_root))
    if "nltk" in sys.modules:
        raise RuntimeError("Reference must run in a fresh process")
    import nltk
    import nltk.downloader
    from nltk.corpus import stopwords
    from nltk.tokenize import NLTKWordTokenizer

    if Path(nltk.__file__).resolve() != source_root / "nltk/__init__.py":
        raise RuntimeError("Installed NLTK is NOT the reference")
    if nltk.__version__ != "3.10.3":
        raise RuntimeError(f"Unexpected reference version: {nltk.__version__}")
    nltk.data.path[:] = [str(data_root)]
    nltk.download = nltk.downloader.download = denied
    nltk.downloader.Downloader.download = denied
    if not isinstance(nltk.tokenize._treebank_word_tokenizer, NLTKWordTokenizer):
        raise RuntimeError("Expected the improved NLTKWordTokenizer")
    stop_words = frozenset(stopwords.words("english"))
    # Independently exercise the guards, rather than merely claiming isolation.
    import socket
    import pickle

    for operation in (socket.socket, lambda: nltk.download("punkt"), lambda: pickle.loads(b"N.")):
        try:
            operation()
        except RuntimeError as error:
            if "Reference forbids" not in str(error):
                raise
        else:
            raise RuntimeError("Reference isolation guard did not reject an operation")
    provenance = {
        "reference": "NLTK 3.10.3 word_tokenize(text), default English/Punkt, preserve_line=False",
        "source_commit": SOURCE_COMMIT,
        "data_commit": DATA_COMMIT,
        "archives": ASSETS,
        "source_files_sha256": {
            relative: sha256((source_root / relative).read_bytes())
            for relative in SOURCE_FILES
        },
        "data_files_sha256": data_hashes,
        "generator": "services/indexer/tools/generate_nlp_reference.py",
        "generator_sha256": sha256(Path(__file__).read_bytes()),
        "tools": {
            "python": platform.python_version(),
            "unicode_database": unicodedata.unidata_version,
            "dependencies": REFERENCE_DEPENDENCIES,
        },
        "isolation": {
            "network": "socket audit events and subprocess execution denied before NLTK import",
            "downloads": "nltk.download and Downloader.download blocked",
            "pickle": "pickle/_pickle load(s) and pickle.find_class blocked",
            "data_path": "only the verified five English plaintext assets",
            "application_imports": False,
        },
    }
    return nltk.word_tokenize, stop_words, provenance


def expand(parts):
    return "".join(part if isinstance(part, str) else part["text"] * part["repeat"] for part in parts)


def compact(tokens):
    result = []
    for token, group in itertools.groupby(tokens):
        count = sum(1 for _ in group)
        result.append(token if count == 1 else {"token": token, "repeat": count})
    return result


def compact_text(text):
    return [
        item if isinstance(item, str) else {"text": item["token"], "repeat": item["repeat"]}
        for item in compact(re.findall(r"\S+\s*|\s+", text))
    ]


TOKEN_CASES = [
    ("empty", ""),
    ("stopwords_only", "I me my myself we our ours ourselves the AND is to not no nor"),
    ("duplicate_order_and_case", "Echo echo ECHO beta Beta alpha echo."),
    ("straight_contractions", "Can't won't shouldn't isn't I'd I'll we're they've you've don't. Cannot gonna wanna gotta lemme gimme d'ye more'n 'tis 'twas."),
    ("curly_contractions", "Can’t won’t shouldn’t isn’t I’d I’ll we’re they’ve you’ve don’t. O’Reilly’s café."),
    ("abbreviations", "Dr. Ada met Prof. Birch at 5 p.m. in the U.S. They spoke. Next sentence ends."),
    ("initials_and_titles", "Mr. J. Smith met Ms. A. Jones. E.g. examples followed, i.e. more words. Jan. 3 was cold."),
    ("sentence_final_periods", "First sentence. Second sentence! Third? Tail. ... Wait.. Final."),
    ("numbers", "Costs $3.88, 1,234.50 or 42; -7 +8 3/4 12:30 50% ½ ² ١٢٣ １２３."),
    ("quotes", "He said, \"Hello.\" 'Goodbye,' she said. ‘single’ “double” «bonjour» ``ready''."),
    ("punctuation", "@user #tag & * (round) [square] {brace} a/b a\\b x=y a+b <angle> ... !!! ??? — – …"),
    ("hyphens_underscores", "state-of-the-art foo_bar foo--bar end- -start under_score A-B x_y 10-year-old."),
    ("unicode_normalization", "Café café CAFÉ Å Å naïve naïve İ İstanbul Straße STRASSE é é."),
    ("non_ascii_alnum", "中文 東京 Москва Ελληνικά العربية हिन्दी 한글 Ⅳ ① 𝔘𝔫𝔦𝔠𝔬𝔡𝔢 abc１２３ emoji🙂."),
    ("whitespace_and_sentences", "Amber\tbirch\r\nCedar\u00a0delta\u2003elm.\n\nFir grows. Oak stays."),
]

CHUNK_CASES = [
    ("empty_chunks", [""]),
    ("exact_10000_characters", [{"text": " ", "repeat": 9995}, "alpha"]),
    ("split_word", [{"text": " ", "repeat": 9997}, "alphabet soup."]),
    ("split_straight_contraction", [{"text": " ", "repeat": 9997}, "can't leave."]),
    ("split_curly_contraction", [{"text": " ", "repeat": 9997}, "can’t leave."]),
    ("multibyte_character_not_byte_boundary", [{"text": "　", "repeat": 9999}, "éclair 世界."]),
    ("decomposed_character_boundary", [{"text": " ", "repeat": 9999}, "élan café."]),
    ("abbreviation_on_boundary", [{"text": " ", "repeat": 9997}, "Dr. Ada left. Next."]),
]

EXTRACTION_HTML = """<html><head><title>Not metadata</title><meta name="title" content="Fallback title"><meta property="og:title" content="Synthetic title"><meta name="description" content="Fallback description"><meta property="og:description" content="Synthetic description"></head><body><article><p>Teaser echoes.</p></article><main><x-copy>Visible catalog.</x-copy><p>Café café [citation] can't 42.</p><script>script secret</script><style>style secret</style><noscript>noscript secret</noscript><template>template secret</template><svg>svg secret</svg><div hidden>hidden secret</div><div aria-hidden="true">aria secret</div></main><footer><p>Footer echoes.</p></footer></body></html>"""

HTML_CASES = [
    {"id": "static_paragraphs", "html_parts": [EXTRACTION_HTML], "rendered": False,
     "page_text_parts": ["Teaser echoes. Café café  can't 42. Footer echoes."],
     "title": "Synthetic title", "description": "Synthetic description", "language": "en"},
    {"id": "rendered_semantic_and_hidden", "html_parts": [EXTRACTION_HTML], "rendered": True,
     "page_text_parts": ["Visible catalog. Café café  can't 42."],
     "title": "Synthetic title", "description": "Synthetic description", "language": "en"},
    {"id": "summary_499", "html_parts": ["<p>", {"text": "café ", "repeat": 498}, "boundary</p>"], "rendered": False,
     "page_text_parts": [{"text": "café ", "repeat": 498}, "boundary"],
     "title": None, "description": None, "language": "en"},
    {"id": "summary_500", "html_parts": ["<p>", {"text": "café ", "repeat": 499}, "boundary</p>"], "rendered": False,
     "page_text_parts": [{"text": "café ", "repeat": 499}, "boundary"],
     "title": None, "description": None, "language": "en"},
    {"id": "summary_501", "html_parts": ["<p>", {"text": "café ", "repeat": 499}, "boundary beyond</p>"], "rendered": True,
     "page_text_parts": [{"text": "café ", "repeat": 499}, "boundary beyond"],
     "title": None, "description": None, "language": "en"},
    {"id": "static_without_paragraphs", "html_parts": ["<title>No fallback</title><main>Ignored words.</main>"], "rendered": False,
     "page_text_parts": [""], "title": None, "description": None, "language": "en"},
    {"id": "stopword_html", "html_parts": ["<p>The and or is to not.</p>"], "rendered": False,
     "page_text_parts": ["The and or is to not."], "title": None, "description": None, "language": "en"},
    {"id": "non_english_rendered", "html_parts": ["<main>Bonjour café monde.</main>"], "rendered": True,
     "page_text_parts": ["Bonjour café monde."], "title": None, "description": None, "language": "fr"},
    {"id": "postings_duplicates", "html_parts": ["<p>Echo echo ECHO beta Beta café can't.</p>"], "rendered": False,
     "page_text_parts": ["Echo echo ECHO beta Beta café can't."], "title": None, "description": None, "language": "en"},
    {"id": "html_chunk_split_word", "html_parts": ["<p>", {"text": "the ", "repeat": 2499}, "alphabet soup.</p>"], "rendered": False,
     "page_text_parts": [{"text": "the ", "repeat": 2499}, "alphabet soup."],
     "title": None, "description": None, "language": "en"},
]


def generate(workdir):
    tokenize, stop_words, provenance = frozen_reference(workdir)
    from nltk import sent_tokenize
    from nltk.tokenize import TreebankWordTokenizer

    discriminators = {
        "preserve_line_true_differs": [name for name, text in TOKEN_CASES if tokenize(text) != tokenize(text, preserve_line=True)],
        "treebank_even_with_punkt_differs": [
            name for name, text in TOKEN_CASES
            if tokenize(text) != [word for sentence in sent_tokenize(text) for word in TreebankWordTokenizer().tokenize(sentence)]
        ],
    }
    if not all(discriminators.values()):
        raise RuntimeError("Corpus must reject both sentence-splitting and improved-tokenizer downgrades")
    provenance["reference_crosschecks"] = discriminators

    def filtered(tokens):
        return [token.lower() for token in tokens if token.lower() not in stop_words and token.lower().isalnum()]

    def chunked(text):
        return [token for offset in range(0, len(text), 10000) for token in tokenize(text[offset:offset + 10000])]

    result = {
        "schema_version": 2,
        "provenance": provenance,
        "english_stopwords": {
            "count": len(stop_words),
            "sorted_set_sha256": sha256(("\n".join(sorted(stop_words)) + "\n").encode("utf-8")),
            "canonicalization": "UTF-8 of LF-joined sorted unique words, with a final LF",
            "resource": "corpora/stopwords/english",
        },
    }
    result["token_cases"] = [
        {"id": name, "input_parts": [text], "raw_tokens": compact(tokenize(text)),
         "filtered_tokens": compact(filtered(tokenize(text)))}
        for name, text in TOKEN_CASES
    ]
    result["chunk_cases"] = [
        {"id": name, "input_parts": parts, "chunk_size": 10000,
         "character_count": len(expand(parts)), "utf8_byte_count": len(expand(parts).encode("utf-8")),
         "raw_tokens": compact(chunked(expand(parts))),
         "unchunked_tokens": compact(tokenize(expand(parts))),
         "filtered_tokens": compact(filtered(chunked(expand(parts))))}
        for name, parts in CHUNK_CASES
    ]
    result["html_cases"] = []
    for case in HTML_CASES:
        text = expand(case["page_text_parts"])
        summary = " ".join(text.split()[:500])
        result["html_cases"].append({
            **case, "raw_tokens": compact(chunked(text)),
            "expected": {"title": case["title"], "description": case["description"],
                         "summary_parts": compact_text(summary), "text": compact(filtered(chunked(text))),
                         "language": case["language"]},
            "language_sample_parts": compact_text(summary[:1000]),
        })
    # Frozen lifecycle algorithm, with reference NLP and no app/constants import.
    url = "https://echo.example.org/echo/echo/new/new"
    words = [word.lower() for word in re.split(r"[.,\-_\/+\:\(\)]", url) if word and word.lower() != "org"]
    posting_case = next(case for case in HTML_CASES if case["id"] == "postings_duplicates")
    keywords = dict(Counter(filtered(chunked(expand(posting_case["page_text_parts"])))).most_common(1000))
    for word in words:
        previous = keywords.get(word, 0)
        keywords[word] = previous * 50 if previous else 10
    result["postings_cases"] = [{"id": "repeated_url_boosts", "html_case": posting_case["id"],
                                "url": url, "url_tokens": words, "keywords": keywords}]
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workdir", required=True, type=Path)
    parser.add_argument("--provision", action="store_true")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--check", type=Path)
    args = parser.parse_args()
    if args.provision:
        if args.output or args.check:
            parser.error("Provision and offline generation must be separate invocations")
        provision(args.workdir)
        return
    if not args.output and not args.check:
        parser.error("Offline generation requires --output or --check")
    result = generate(args.workdir)
    content = json.dumps(result, ensure_ascii=False, indent=2) + "\n"
    if args.output:
        args.output.write_text(content, encoding="utf-8")
    if args.check and args.check.read_text(encoding="utf-8") != content:
        raise SystemExit("Reference fixture differs from the independently regenerated baseline")
    print(f"Offline NLTK reference: {len(result['token_cases'])} token, "
          f"{len(result['chunk_cases'])} chunk, {len(result['html_cases'])} HTML cases; "
          f"{result['english_stopwords']['count']} English stopwords (hash only); "
          f"JSON sha256={sha256(content.encode('utf-8'))}")


if __name__ == "__main__":
    main()
