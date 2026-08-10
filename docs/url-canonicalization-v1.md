# URL Canonicalization V1

MiFolyo uses one versioned URL identity contract for seed ingestion, crawl queueing, indexing, and forum discussions. Implementations in Python, Go, and PHP must pass the shared vectors in `contracts/url-canonicalization/v1.json`.

## Identity

The canonical URL remains an absolute, fetchable URL. Its opaque ID is:

```text
SHA-256("mifolyo-url:v1\0" + canonical_url)
```

The digest is lowercase hexadecimal. Canonicalization is deterministic, idempotent, and performs no DNS or network access.

## Rules

1. Input and canonical output are limited to 2,048 UTF-8 bytes.
2. Leading or trailing whitespace, control characters, DEL, raw backslashes, and malformed percent escapes are rejected.
3. Only absolute HTTP and HTTPS URLs are accepted.
4. User information is rejected rather than silently removed.
5. Scheme and hostname are lowercased.
6. Raw Unicode hostnames are rejected in V1. Valid ASCII and IDNA A-label hostnames are accepted.
7. Host labels must be valid and a trailing hostname dot is rejected.
8. Ports must be decimal integers from 1 through 65,535. HTTP port 80 and HTTPS port 443 are removed; other ports are preserved in identity.
9. An empty path becomes `/`.
10. Path case, repeated slashes, dot segments, and trailing slashes are preserved.
11. Query order, duplicate keys, blank values, `+`, and case are preserved. V1 does not remove tracking parameters.
12. Fragments are removed.
13. Existing percent escapes are uppercased. Raw non-ASCII path and query characters are UTF-8 percent-encoded. Reserved escaped bytes are not decoded.

Source-specific transformations, such as unwrapping Reddit redirect URLs, happen before canonicalization and are not part of this contract.

## Crawl Admission

Canonicalization defines identity; it does not authorize a network request. Static crawl admission additionally rejects:

- Literal IPv4 and IPv6 addresses
- Single-label and known local hostnames
- Non-default crawl ports

Before every connection and redirect, the crawler must separately resolve and reject private, loopback, link-local, reserved, and other non-global addresses. That DNS-pinned fetch authorization is a follow-up security control and is not replaced by canonicalization.

## Versioning

Changing an identity rule requires a new version, fixture, URL-ID namespace, MongoDB seed collection migration, and Redis queue namespace. Existing IDs must never be silently reinterpreted under different rules.
