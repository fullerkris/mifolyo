# Crawl Jobs V2 Foundation Status - 2026-09-07

## Purpose

This document records the current F3 Crawl Jobs V2 checkpoint. The checkpoint is
safe to preserve on a feature branch because it is dormant and has no runtime
wiring. It is not a production-readiness claim and is not ready to merge or
activate.

## Scope completed

- Added the normative Crawl Jobs V2 protocol contract in `docs/crawl-jobs-v2.md`.
- Added a dormant Go package at
  `services/spider/internal/database/crawljobsv2`.
- Added exact constants, identifiers, key grammar, response/status codes,
  canonical framing, digest identities, transition identities, stage chunks,
  fixed record schemas, and RESP2 request-size accounting.
- Added value-aware codecs for compatibility, commit-guard, legacy-retirement,
  admin-freeze, durability, first-request-start, final-page, final-image, and
  image-manifest records.
- Added an operation-aware seven-field transport gate that validates
  boot-only, candidate, and active artifact combinations and restricts the
  candidate run path to `source_kind=v1_migration`.
- Replaced caller-constructible request-start authority with an opaque result
  bound to the exact reservation intent. Only a Redis response carrying
  `io_permission=1` can produce an I/O permit.
- Added an ordinal-checked document/redirect transcript, final Redis document
  witness, complete alias derivation, and run-pinned render-policy binding.
- Tightened response validation for request counters, TTLs, retries, staging,
  batch limits, terminal timestamps, and archive/purge eligibility.
- Added fail-closed formatting, JSON/text, and structured-log redaction for
  sensitive identifiers plus keyed HMAC-SHA-256 operational references.
- Upgraded the shared conformance fixture to version 2 with 34 named positive
  cases and 99 exact-class negative cases.
- Added independent Go and Python consumers for every fixture v2 case. No case
  is skipped.

## Verification at this checkpoint

The focused checks completed successfully before branch creation:

```text
go test ./internal/database/crawljobsv2 -count=1
python3 scripts/verify-crawl-jobs-v2-digests.py
```

The Python verifier reports:

```text
crawl-jobs-v2 digest vectors verified (fixture v2)
```

The fixture currently computes the contract digest using the exact contract
document and an empty authoritative Lua source set. A synthetic case verifies
ASCII Lua source-name ordering. The contract digest must be regenerated when
authoritative Lua files are added.

## Safety properties of this checkpoint

- The package has no runtime imports or activation wiring.
- No Redis mutation, Lua transition, crawler start, rendering start, migration,
  deployment, or public crawl was performed.
- Existing V1 Redis behavior was not modified by this F3 checkpoint.
- `ALREADY_STARTED` with `io_permission=0` is reconciliation-only and cannot
  authorize DNS or become successful output evidence.
- Candidate gates cannot authorize ordinary active operation.
- Output remains invisible because no commit implementation or runtime binding
  exists.

## Work still required

- Complete an independent post-remediation blocker/high review. The attempted
  review was interrupted by network connectivity and has not issued a final
  gate verdict.
- Resolve every finding from that review before treating the Go foundation as
  authoritative.
- Implement the exact dormant Lua transitions and sealed Go/Python bindings.
- Add disposable standalone Redis tests for transition idempotency, fencing,
  crash recovery, AOF durability, memory bounds, and maximum-shape latency.
- Add full Spider, feeder, consumer, monitoring, migration, and crawl-admin
  integration only after the dormant transition layer passes its release gate.
- Regenerate contract, guard, compatibility, and fixture digests after the Lua
  source set is final.
- Run the complete repository test, race, vet, security, and protected PR check
  suite.
- Obtain explicit authorization before any migration, service start, deployment,
  or crawl.

## Branch and review status

This checkpoint may be pushed to a dedicated feature branch for backup and
collaboration. It should be labeled work in progress. It must not be merged into
`main`, used to install candidate markers, or used as evidence that F3 is
complete until the outstanding review and Redis/Lua acceptance work passes.
