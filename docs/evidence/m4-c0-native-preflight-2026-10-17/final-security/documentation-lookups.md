# Documentation lookups retained for this re-review

Date: 2026-10-17. All requests were read-only public documentation/source fetches, not local/remote Engine requests. No private source or credentials were supplied in documentation queries.

## Context7

1. Resolved `Docker Engine API`; selected official `/docker/docs` over the separately indexed API 1.33 OpenAPI mirror.
2. Queried GET `/info` and GET `/version` wire names, including `CpuCfsPeriod`, `CpuCfsQuota`, info `OSType`/`Architecture`, version `Os`/`Arch`/`Version`/`KernelVersion`, and local Unix-socket operation.

Returned documentation:

- https://github.com/docker/docs/blob/main/_vendor/github.com/moby/moby/api/docs/v1.20.md — GET info, JSON `CpuCfsPeriod` and `CpuCfsQuota`; includes possible `HttpProxy`/`HttpsProxy` fields.
- https://github.com/docker/docs/blob/main/_vendor/github.com/moby/moby/api/docs/v1.22.md — GET info and version; confirms field-name casing in the independent literal fixtures.
- https://github.com/docker/docs/blob/main/layouts/api-docs.markdown.md — Engine version query using `/var/run/docker.sock`.

These are historical API examples returned by the index, not proof of current deployment compatibility or a version-pinned API 1.56 contract.

## Unsuccessful attempts to obtain newer source/schema

All three returned HTTP 404; no content from them was used as evidence:

- https://raw.githubusercontent.com/moby/moby/v29.0.0/api/types/system/info.go
- https://raw.githubusercontent.com/moby/moby/v29.0.0/api/types/system/version.go
- https://raw.githubusercontent.com/docker/docs/main/_vendor/github.com/moby/moby/api/swagger.yaml

The security verdict rests on the frozen code, strict fail-closed contracts and offline adversarial evidence. It does not claim that this review exercised a live Docker 29.9.1/API 1.56 endpoint.
