"""MiFolyo URL identity and static crawl-admission rules, version 1.

This module intentionally performs no source-specific cleanup and no network
access.  Callers such as the Reddit adapter must unwrap redirects and exclude
source-owned URLs before passing a value here.
"""

from __future__ import annotations

import hashlib
import ipaddress
import re
import unicodedata
from dataclasses import dataclass
from typing import Optional
from urllib.parse import quote, urlsplit

import idna


CANONICALIZATION_VERSION = 1
ID_NAMESPACE = "mifolyo-url:v1\0"
ID_NAMESPACE_BYTES = ID_NAMESPACE.encode("utf-8")
MAX_URL_BYTES = 2048

ERROR_WHITESPACE_OR_CONTROL = "whitespace_or_control"
ERROR_URL_TOO_LONG = "url_too_long"
ERROR_SCHEME_NOT_ALLOWED = "scheme_not_allowed"
ERROR_ABSOLUTE_URL_REQUIRED = "absolute_url_required"
ERROR_USERINFO_FORBIDDEN = "userinfo_forbidden"
ERROR_INVALID_PORT = "invalid_port"
ERROR_MALFORMED_ESCAPE = "malformed_escape"
ERROR_ENCODED_CONTROL = "encoded_control"
ERROR_BACKSLASH_FORBIDDEN = "backslash_forbidden"
ERROR_NON_ASCII_HOST = "non_ascii_host_v1"
ERROR_INVALID_HOST = "invalid_host"
ERROR_INVALID_ENCODING = "invalid_utf8"
ERROR_INVALID_URL = "invalid_url"

REJECTION_NON_DEFAULT_PORT = "non_default_crawl_port"
REJECTION_IP_LITERAL = "ip_literal_forbidden"
REJECTION_LOCAL_NAME = "local_name_forbidden"

_PERCENT_ESCAPE = re.compile(r"%([0-9A-Fa-f]{2})")
_MALFORMED_PERCENT_ESCAPE = re.compile(r"%(?![0-9A-Fa-f]{2})")
_HOST_LABEL = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$")
_DOTTED_NUMERIC_HOST = re.compile(r"^[0-9]+(?:\.[0-9]+){3}$")

# RFC 3986 pchar plus '/'. Percent is safe only after malformed escapes have
# been rejected and every valid escape has been normalized to uppercase.
_PATH_SAFE = "/:@!$&'()*+,;=-._~%"
_QUERY_SAFE = "/?:@!$&'()*+,;=-._~%"

_LOCAL_NAMES = (
    "localhost",
    "local",
    "localdomain",
    "internal",
    "intranet",
    "home",
    "home.arpa",
    "lan",
    "test",
    "invalid",
)


class URLCanonicalizationError(ValueError):
    """A deterministic V1 URL rejection.

    ``code`` is stable API surface.  Human-readable exception text must not be
    parsed by callers.
    """

    def __init__(self, code: str, message: str = "") -> None:
        self.code = code
        super().__init__(message or code)


@dataclass(frozen=True)
class CrawlEligibility:
    eligible: bool
    rejection: Optional[str] = None


@dataclass(frozen=True)
class URLIdentity:
    canonical_url: str
    url_id: str
    crawl_eligible: bool
    crawl_rejection: Optional[str]
    canonicalization_version: int = CANONICALIZATION_VERSION


def _raise(code: str, message: str = "") -> None:
    raise URLCanonicalizationError(code, message)


def _utf8_length(value: str) -> int:
    try:
        return len(value.encode("utf-8"))
    except UnicodeEncodeError as exc:
        raise URLCanonicalizationError(ERROR_INVALID_ENCODING) from exc


def _uppercase_percent_escapes(value: str) -> str:
    return _PERCENT_ESCAPE.sub(lambda match: "%" + match.group(1).upper(), value)


def _has_encoded_control(value: str) -> bool:
    index = 0
    while index + 2 < len(value):
        if value[index] != "%":
            index += 1
            continue

        decoded = int(value[index + 1 : index + 3], 16)
        if decoded < 0x20 or decoded == 0x7F:
            return True

        # U+0080 through U+009F use C2 80..9F in UTF-8. Reject the
        # percent-encoded representation just as V1 rejects the raw controls.
        if (
            decoded == 0xC2
            and index + 5 < len(value)
            and value[index + 3] == "%"
            and 0x80 <= int(value[index + 4 : index + 6], 16) <= 0x9F
        ):
            return True
        index += 3
    return False


def _encode_component(value: str, safe: str) -> str:
    normalized = _uppercase_percent_escapes(value)
    try:
        return quote(normalized, safe=safe, encoding="utf-8", errors="strict")
    except UnicodeEncodeError as exc:
        raise URLCanonicalizationError(ERROR_INVALID_ENCODING) from exc


def _validate_domain_host(host: str) -> None:
    if len(host) > 253 or host.endswith("."):
        _raise(ERROR_INVALID_HOST)

    labels = host.split(".")
    if not labels or any(not _HOST_LABEL.fullmatch(label) for label in labels):
        _raise(ERROR_INVALID_HOST)

    # Python's built-in codec implements IDNA 2003 and rejects valid IDNA 2008
    # A-labels such as the contract's xn--fa-hia, so use explicit IDNA 2008 /
    # UTS 46 non-transitional validation and require a round trip.
    for label in labels:
        if not label.lower().startswith("xn--"):
            continue
        try:
            decoded = idna.decode(label, uts46=True, std3_rules=True)
            round_trip = idna.encode(decoded, uts46=True, std3_rules=True).decode(
                "ascii"
            )
        except idna.IDNAError as exc:
            raise URLCanonicalizationError(ERROR_INVALID_HOST) from exc
        if not decoded or round_trip.lower() != label.lower():
            _raise(ERROR_INVALID_HOST)


def _validated_host(host: str) -> tuple[str, bool]:
    if not host:
        _raise(ERROR_INVALID_HOST)
    if any(ord(character) > 127 for character in host):
        _raise(ERROR_NON_ASCII_HOST)

    lowered = host.lower()
    if "%" in lowered and ":" in lowered:
        _raise(ERROR_INVALID_HOST)
    try:
        ipaddress.ip_address(lowered)
        return lowered, True
    except ValueError:
        pass

    # A colon outside a valid IP literal cannot occur in a DNS hostname.  A
    # dotted numeric value that is not a valid IPv4 address is not accepted as
    # a lookalike DNS name.
    if ":" in lowered or _DOTTED_NUMERIC_HOST.fullmatch(lowered):
        _raise(ERROR_INVALID_HOST)

    _validate_domain_host(lowered)
    return lowered, False


def _explicit_port(netloc: str, is_ip_literal: bool) -> Optional[int]:
    if is_ip_literal and netloc.startswith("["):
        closing = netloc.find("]")
        if closing < 0:
            _raise(ERROR_INVALID_HOST)
        remainder = netloc[closing + 1 :]
        if not remainder:
            return None
        if not remainder.startswith(":"):
            _raise(ERROR_INVALID_HOST)
        text = remainder[1:]
    else:
        if ":" not in netloc:
            return None
        _, _, text = netloc.rpartition(":")

    if not text or not text.isascii() or not text.isdecimal():
        _raise(ERROR_INVALID_PORT)
    port = int(text, 10)
    if port < 1 or port > 65535:
        _raise(ERROR_INVALID_PORT)
    return port


def canonicalize_url(raw_url: str) -> str:
    """Return the exact V1 canonical URL or raise a coded error."""

    if not isinstance(raw_url, str) or not raw_url:
        _raise(ERROR_ABSOLUTE_URL_REQUIRED)
    if _utf8_length(raw_url) > MAX_URL_BYTES:
        _raise(ERROR_URL_TOO_LONG)
    if raw_url != raw_url.strip() or any(
        unicodedata.category(character) == "Cc" for character in raw_url
    ):
        _raise(ERROR_WHITESPACE_OR_CONTROL)
    if "\\" in raw_url:
        _raise(ERROR_BACKSLASH_FORBIDDEN)
    if _MALFORMED_PERCENT_ESCAPE.search(raw_url):
        _raise(ERROR_MALFORMED_ESCAPE)
    if _has_encoded_control(raw_url):
        _raise(ERROR_ENCODED_CONTROL)

    try:
        parsed = urlsplit(raw_url)
    except ValueError as exc:
        raise URLCanonicalizationError(ERROR_INVALID_HOST) from exc

    if not parsed.scheme:
        _raise(ERROR_ABSOLUTE_URL_REQUIRED)

    scheme = parsed.scheme.lower()
    if scheme not in {"http", "https"}:
        _raise(ERROR_SCHEME_NOT_ALLOWED)
    if not parsed.netloc:
        _raise(ERROR_ABSOLUTE_URL_REQUIRED)
    if "@" in parsed.netloc:
        _raise(ERROR_USERINFO_FORBIDDEN)

    try:
        parsed_host = parsed.hostname or ""
    except ValueError as exc:
        raise URLCanonicalizationError(ERROR_INVALID_HOST) from exc
    host, is_ip_literal = _validated_host(parsed_host)

    # Accessing SplitResult.port catches malformed and out-of-range ports.  We
    # then parse the authority ourselves so an empty port ("host:") is also a
    # stable invalid_port rejection rather than being silently removed.
    try:
        parsed.port
    except ValueError as exc:
        raise URLCanonicalizationError(ERROR_INVALID_PORT) from exc
    port = _explicit_port(parsed.netloc, is_ip_literal)

    default_port = 80 if scheme == "http" else 443
    authority = f"[{host}]" if is_ip_literal and ":" in host else host
    if port is not None and port != default_port:
        authority = f"{authority}:{port}"

    path = _encode_component(parsed.path or "/", _PATH_SAFE)
    query = _encode_component(parsed.query, _QUERY_SAFE)
    before_fragment = raw_url.split("#", 1)[0]
    has_query_marker = "?" in before_fragment
    canonical = f"{scheme}://{authority}{path}"
    if has_query_marker:
        canonical += f"?{query}"
    if _utf8_length(canonical) > MAX_URL_BYTES:
        _raise(ERROR_URL_TOO_LONG)
    return canonical


def url_id_for_canonical_url(canonical_url: str) -> str:
    """Return the opaque V1 ID for an already-canonical URL."""

    return hashlib.sha256(
        ID_NAMESPACE_BYTES + canonical_url.encode("utf-8")
    ).hexdigest()


def static_crawl_eligibility(canonical_url: str) -> CrawlEligibility:
    """Apply V1 static admission checks without DNS or network access."""

    parsed = urlsplit(canonical_url)
    host = parsed.hostname or ""
    try:
        ipaddress.ip_address(host)
        return CrawlEligibility(False, REJECTION_IP_LITERAL)
    except ValueError:
        pass

    lowered = host.lower()
    if "." not in lowered or any(
        lowered == name or lowered.endswith(f".{name}") for name in _LOCAL_NAMES
    ):
        return CrawlEligibility(False, REJECTION_LOCAL_NAME)

    try:
        port = parsed.port
    except ValueError:
        # Canonical URLs produced above cannot reach this path, but retaining a
        # stable rejection is safer for direct callers.
        return CrawlEligibility(False, REJECTION_NON_DEFAULT_PORT)
    default_port = 80 if parsed.scheme == "http" else 443
    if port is not None and port != default_port:
        return CrawlEligibility(False, REJECTION_NON_DEFAULT_PORT)

    return CrawlEligibility(True, None)


def identify_url(raw_url: str) -> URLIdentity:
    canonical = canonicalize_url(raw_url)
    eligibility = static_crawl_eligibility(canonical)
    return URLIdentity(
        canonical_url=canonical,
        url_id=url_id_for_canonical_url(canonical),
        crawl_eligible=eligibility.eligible,
        crawl_rejection=eligibility.rejection,
    )


def assert_canonical_identity(canonical_url: str, expected_url_id: str) -> URLIdentity:
    """Validate that a stored URL and ID are an exact, crawlable V1 pair."""

    identity = identify_url(canonical_url)
    if identity.canonical_url != canonical_url or identity.url_id != expected_url_id:
        raise ValueError("stored URL does not match its V1 identity")
    return identity
