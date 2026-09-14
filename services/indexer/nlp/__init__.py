"""Fixed-English, inference-only tokenization; see README.md before deployment.

No NLTK installation, resource search, downloads, or caller-selected model paths.
Data are privately provisioned at build/test setup, never downloaded at runtime
or checked into Git. Data/image distribution rights remain unresolved.
"""

from functools import lru_cache
from hashlib import sha256
from pathlib import Path
import stat

from ._asset_hashes import ASSETS as _ASSETS
from ._destructive import NLTKWordTokenizer as _NLTKWordTokenizer
from ._punkt import PunktParameters as _PunktParameters
from ._punkt import PunktSentenceTokenizer as _PunktSentenceTokenizer

__all__ = ["word_tokenize", "english_stopwords"]


class NLPDataError(RuntimeError):
    """A required fixed, bundled resource is absent, altered, or invalid."""


class _OrthographicContext(dict):
    """NLTK's zero-on-miss lookup, without retaining words from users' text.

    Inference only reads this mapping. Unlike defaultdict(int), a miss must not
    grow a process-wide cache with previously unseen (possibly private) words.
    """

    def __missing__(self, key):
        return 0


def _verified_asset_lines():
    """Read ONLY the five literal package-relative names in the pinned table."""
    root = Path(__file__).absolute().parent
    expected_files = set(_ASSETS)
    expected_dirs = {"data", "data/punkt_tab", "data/punkt_tab/english"}
    found_files = set()
    lines = {}
    try:
        if not stat.S_ISDIR(root.lstat().st_mode):
            raise NLPDataError("NLP package directory must not be a symlink")
        pending = [root / "data"]
        while pending:
            directory = pending.pop()
            if not stat.S_ISDIR(directory.lstat().st_mode):
                raise NLPDataError("NLP data directories must not be symlinks")
            for path in directory.iterdir():
                relative = path.relative_to(root).as_posix()
                mode = path.lstat().st_mode
                if stat.S_ISDIR(mode) and relative in expected_dirs:
                    pending.append(path)
                elif stat.S_ISREG(mode) and relative in expected_files:
                    found_files.add(relative)
                else:
                    raise NLPDataError(f"Unexpected NLP asset or symlink: {relative}")
        if found_files != expected_files:
            raise NLPDataError(
                "Missing bundled NLP assets: " + ", ".join(sorted(expected_files - found_files))
            )
        for relative, metadata in _ASSETS.items():
            path = root / relative
            if path.stat().st_size != metadata["bytes"]:
                raise NLPDataError(f"NLP asset size mismatch: {relative}")
            content = path.read_bytes()
            if sha256(content).hexdigest() != metadata["sha256"]:
                raise NLPDataError(f"NLP asset checksum mismatch: {relative}")
            # Match upstream text iteration/rm_nl, not Unicode splitlines().
            rows = content.decode("utf-8").split("\n")
            if rows[-1] == "":
                rows.pop()
            if len(rows) != metadata["rows"] or any(not row for row in rows):
                raise NLPDataError(f"Invalid NLP asset records: {relative}")
            lines[relative] = rows
    except (OSError, UnicodeError) as exc:
        raise NLPDataError(
            "Bundled English NLP data unavailable or unreadable; no fallback is allowed. "
            "Run explicit private build provisioning before offline use. "
            "Data/image distribution remains blocked; see nlp/README.md."
        ) from exc
    return lines


@lru_cache(maxsize=1)
def _bundle():
    # Verify the entire fixed bundle before returning even an empty token list.
    lines = _verified_asset_lines()
    prefix = "data/punkt_tab/english/"
    try:
        params = _PunktParameters()
        params.abbrev_types = set(lines[prefix + "abbrev_types.txt"])
        params.sent_starters = set(lines[prefix + "sent_starters.txt"])
        params.collocations = {
            tuple(row.split("\t")) for row in lines[prefix + "collocations.tab"]
        }
        if any(len(pair) != 2 for pair in params.collocations):
            raise ValueError("invalid collocation")
        params.ortho_context = _OrthographicContext(
            (word, int(flags))
            for word, flags in (
                row.split("\t") for row in lines[prefix + "ortho_context.tab"]
            )
        )
        stopwords = frozenset(lines["data/english_stopwords.txt"])
    except (KeyError, TypeError, ValueError) as exc:
        raise NLPDataError("Invalid fixed English NLP data format") from exc
    return _PunktSentenceTokenizer(params), _NLTKWordTokenizer(), stopwords


def word_tokenize(text: str) -> list[str]:
    """NLTK 3.10.3 default English Punkt + NLTKWordTokenizer token sequence.

    Always performs sentence segmentation (preserve_line=False). Does not
    lowercase, remove punctuation/stopwords, filter isalnum, or chunk input.
    """
    if not isinstance(text, str):
        raise TypeError("word_tokenize expects str")
    sentence_tokenizer, word_tokenizer, _ = _bundle()
    return [
        token
        for sentence in sentence_tokenizer.tokenize(text)
        for token in word_tokenizer.tokenize(sentence)
    ]


def english_stopwords() -> frozenset[str]:
    """Return the exact immutable pinned English stopword set, or fail."""
    return _bundle()[2]
