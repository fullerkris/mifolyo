# Claim/release Step 5 image preparation — 2026-09-23

**Local image gate: PASS.** The corrected reviewed Linux/arm64 harness image and
claim-specific plan/recipe have been validated. Scoped publication and protected
CI on the resulting commit are the next checkpoint gates. No claim/release
acceptance case or Redis server was started.

## Reviewed source and build

- Working branch: `feature/crawl-jobs-v2-claim-release`, based on merged main
  `ff2457ebe998707d220e4ce3425aab500c75f5b4`.
- All 71 file hashes in the [corrected review inventory](evidence/m4-claim-review-2026-09-23/corrected.json)
  matched before and after preparation. Its SHA-256 is
  `6d070f91d06fc4e335a176d2f597b69b4c728aa64ca9bb67738267bd07248717`.
- Independent correctness/security GO and follow-up closure remain recorded in
  the [Step 4 report](crawl-jobs-v2-m4-claim-review-2026-09-23.md).
- Docker Engine 29.5.2, Linux/arm64; build network disabled, immutable Python
  3.13.15 platform manifest, explicit file-only COPY allowlist.

| Artifact | Identity |
|---|---|
| Python platform manifest | `docker.io/library/python@sha256:ad4c34ff79289506e235b40dce75d629e25f226b597a2455804220f037e07531` |
| Python daemon-local base | `sha256:adc3d531c29fbd69fa9ca49cd8e241c68aaf4e1dd7462be6c2432044b7a39ea8` |
| Corrected harness image | `sha256:b8de7cf09495bca22f6bba158bfdbe776b65ab726895e484682a3bb4f95a8a01` |
| Redis 7.4.11 image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Claim plan SHA-256 | `9833e6c25c74d9b0b80cc6370c5e6d38165cea2032f471aab879e146183dba41` |
| Claim recipe SHA-256 | `07d27ddc8218d6c2aa5adf0f05227795a9f097802f50a534585e87206f8a2ad1` |
| Validation report SHA-256 | `6d356f63cb0e7254d34c22cd51ff46b9d629078d9dca7b1be6f59c4548f10917` |

The local image tag is a convenience only; plans bind the immutable image ID.
Source contract, canonical Lua source-set and bundle-seal identities remain
unchanged. The complete 57-file image inventory and both case recipe hashes
matched the reviewed host sources.

## Observed validation

The preparation command explicitly selected **`ledger-claim-release-v1`**.
Fresh metadata fixture `6edd69a4f30fe82bbfba91c794d13279` passed admission for
init, executor, Redis and revocation specifications. Each remained `created`,
not running, with PID zero. All four containers and both volumes were removed.

Separate networkless checks invoked Redis `--version` and Python validation only:

| Check | Outcome | Peak / limit |
|---|---|---|
| Redis version | PASS: 7.4.11, exit 0, no OOM, cleanup verified | 256 MiB configured limit |
| Init image validation | PASS: exact inventory, identities, requests/recipes and isolation | 48,193,536 / 134,217,728 bytes |
| Executor image validation | PASS: exact inventory, identities, requests/recipes and isolation | 48,386,048 / 268,435,456 bytes |

Separate fixture/image-check label listings were empty after cleanup. The
[artifact package](evidence/m4-claim-image-prep-2026-09-23/README.md) retains the
actual report, inputs and non-executable plan/recipe. These checks do not prove
real claim/lease transitions, target ACL semantics, AOF durability or stage latency.

## Publication and CI scope

The owner requested Step 5, including the scoped checkpoint/PR and protected CI.
The checkpoint includes the reviewed offline fixture and executor integration,
its tests, review and preparation artifacts, and the previously local smoke
PASS/request/status evidence. Unrelated local agent, client, seed and Backlinks
Processor changes are outside this publication.

The single CI change adds `--case ledger-claim-release-v1` to the existing
`required-build` image-preparation invocation. It now produces claim-specific
amd64 evidence, while still checking both case recipes. Required contexts,
assertions and exhaustive race-shard coverage remain intact. Actionlint passes.

Exact-index checks, a scoped secret scan, commit/push identity verification and
the protected PR results are recorded as they complete in the primary
[implementation plan](crawl-jobs-v2-plan.md). Another execution requires fresh
approval binding the final commit, the exact artifacts above, bounds, operator
and expiry, followed by a separate run request.
