import re
from urllib.parse import unquote, urlsplit

from utils.constants import FILE_TYPES


NAME_SPLIT_PATTERN = re.compile(r"[-_./\s]+")


def split_name(filename: str):
    """Tokenize spider-authorized metadata without dereferencing its URL."""
    parts = NAME_SPLIT_PATTERN.split(filename)
    return [
        part.lower()
        for part in parts
        if part
        and len(part.encode()) <= 256
        and part.lower() not in FILE_TYPES
        and "px" not in part.lower()
    ][:100]


def image_filename(normalized_source_url: str) -> str:
    """Derive the legacy response filename field from normalized metadata."""
    return unquote(urlsplit(normalized_source_url).path.rsplit("/", 1)[-1])[:512]
