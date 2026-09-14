import ipaddress
import re
import unicodedata
from urllib.parse import urlsplit

import idna

from config import MAX_CANONICAL_URL_BYTES


_HOST_LABEL = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$")
_DOTTED_NUMERIC_HOST = re.compile(r"^[0-9]+(?:\.[0-9]+){3}$")
_MALFORMED_PERCENT_ESCAPE = re.compile(r"%(?![0-9A-Fa-f]{2})")
_PERCENT_ESCAPE = re.compile(r"%[0-9A-Fa-f]{2}")
_PATH_CHARACTERS = frozenset("/:@!$&'()*+,;=-._~%")
_QUERY_CHARACTERS = _PATH_CHARACTERS | frozenset("?")


class CanonicalURLValidationError(ValueError):
    pass


def _has_encoded_control(value: str) -> bool:
    index = 0
    while index + 2 < len(value):
        if value[index] != "%":
            index += 1
            continue
        decoded = int(value[index + 1 : index + 3], 16)
        if decoded < 0x20 or decoded == 0x7F:
            return True
        if (
            decoded == 0xC2
            and index + 5 < len(value)
            and value[index + 3] == "%"
            and 0x80 <= int(value[index + 4 : index + 6], 16) <= 0x9F
        ):
            return True
        index += 3
    return False


def _valid_component(value: str, extra_characters: frozenset[str]) -> bool:
    return all(
        character.isascii()
        and (character.isalnum() or character in extra_characters)
        for character in value
    )


def validate_canonical_url(value: str) -> None:
    """Reject values that cannot be an exact MiFolyo V1 canonical URL."""

    if not isinstance(value, str) or not value:
        raise CanonicalURLValidationError("url_invalid")
    try:
        encoded = value.encode("utf-8")
    except UnicodeEncodeError as error:
        raise CanonicalURLValidationError("url_invalid") from error
    if len(encoded) > MAX_CANONICAL_URL_BYTES:
        raise CanonicalURLValidationError("url_too_long")
    if not value.isascii() or value != value.strip() or "\\" in value:
        raise CanonicalURLValidationError("url_noncanonical")
    if any(unicodedata.category(character) == "Cc" for character in value):
        raise CanonicalURLValidationError("url_noncanonical")
    if _MALFORMED_PERCENT_ESCAPE.search(value) or any(
        match.group(0) != match.group(0).upper()
        for match in _PERCENT_ESCAPE.finditer(value)
    ):
        raise CanonicalURLValidationError("url_noncanonical")
    if _has_encoded_control(value) or "#" in value:
        raise CanonicalURLValidationError("url_noncanonical")

    try:
        parsed = urlsplit(value)
        port = parsed.port
    except ValueError as error:
        raise CanonicalURLValidationError("url_invalid") from error
    if (
        parsed.scheme not in {"http", "https"}
        or not value.startswith(f"{parsed.scheme}://")
        or not parsed.netloc
    ):
        raise CanonicalURLValidationError("url_invalid")
    if parsed.username is not None or parsed.password is not None:
        raise CanonicalURLValidationError("url_noncanonical")

    host = parsed.hostname or ""
    if (
        not host
        or "%" in host
        or host != host.lower()
        or any(ord(character) > 127 for character in host)
    ):
        raise CanonicalURLValidationError("url_noncanonical")
    try:
        address = ipaddress.ip_address(host)
    except ValueError:
        if (
            _DOTTED_NUMERIC_HOST.fullmatch(host)
            or len(host) > 253
            or host.endswith(".")
            or any(
                not _HOST_LABEL.fullmatch(label) for label in host.split(".")
            )
        ):
            raise CanonicalURLValidationError("url_invalid")
        for label in host.split("."):
            if not label.startswith("xn--"):
                continue
            try:
                decoded = idna.decode(label, uts46=True, std3_rules=True)
                round_trip = idna.encode(
                    decoded, uts46=True, std3_rules=True
                ).decode("ascii")
            except idna.IDNAError as error:
                raise CanonicalURLValidationError("url_invalid") from error
            if not decoded or round_trip.lower() != label:
                raise CanonicalURLValidationError("url_invalid")
        authority = host
    else:
        authority = f"[{host}]" if address.version == 6 else host

    default_port = 80 if parsed.scheme == "http" else 443
    if port is not None:
        if port < 1 or port == default_port:
            raise CanonicalURLValidationError("url_noncanonical")
        authority = f"{authority}:{port}"
    if parsed.netloc != authority or not parsed.path:
        raise CanonicalURLValidationError("url_noncanonical")
    if not _valid_component(parsed.path, _PATH_CHARACTERS) or not _valid_component(
        parsed.query, _QUERY_CHARACTERS
    ):
        raise CanonicalURLValidationError("url_noncanonical")
