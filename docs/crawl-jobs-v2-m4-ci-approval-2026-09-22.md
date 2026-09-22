# M4 exact-revision CI and bounded approval — 2026-09-22

**Gate result:** immutable image preparation and all fourteen protected CI
contexts pass. The owner renewed approval for one exact `ledger-smoke-v1` case.
**Subsequent disposition:** the owner requested the run; the one-case approval
was used by the [failed init attempt](crawl-jobs-v2-m4-smoke-report-2026-09-22.md).
Redis never started and cleanup was verified. Do not reuse this approval.
Full M4 acceptance, merge and runtime activation remain separate gates.

This historical post-CI record was initially held locally to preserve the approved
HEAD. It is included with the subsequently authorized init-correction checkpoint
after that approval was consumed. The original approval remains outside the
repository and disposable Docker volumes, mode 0600.

## Published revision and CI

- Reviewed/published head: `340906c694ee6f51d67ea0c9b448b07f29df4834`.
- Main/base: `d914a93f9ade5b63182ecf02092c1a1e74633713`.
- Tested PR merge tree: `642b0e6fa6d2ce7c0235ce8dbbe7727c822718fa`.
- [PR #10](https://github.com/fullerkris/mifolyo/pull/10) remains **draft and unmerged**.
- [Required Checks run 35652511870](https://github.com/fullerkris/mifolyo/actions/runs/35652511870): SUCCESS.
- [Unit Tests run 35652511709](https://github.com/fullerkris/mifolyo/actions/runs/35652511709): SUCCESS.

All fourteen required contexts were individually verified SUCCESS, then
rechecked after connectivity returned on September 22:

| Required context | Result |
|---|---|
| required-tests | SUCCESS |
| required-build | SUCCESS |
| required-smoke | SUCCESS |
| Test Spider Service | SUCCESS |
| Test Seed Importer Service | SUCCESS |
| Test Image Indexer Service | SUCCESS |
| Audit backlinks-processor Python Dependencies | SUCCESS |
| Audit dmoz-importer Python Dependencies | SUCCESS |
| Audit tfidf Python Dependencies | SUCCESS |
| Check Monitoring Service | SUCCESS |
| Test Query Engine | SUCCESS |
| Test PageRank Service | SUCCESS |
| Test Indexer Service | SUCCESS |
| Test Render Worker | SUCCESS |

The retained eight shard reports account for 470 test roots: **469 passed** and
the single allowed optional `TestJobLuaNativeFactoryParity` skipped. Their
commit identities match the tested merge tree, and disjoint/full coverage was
revalidated with the actual shard verifier. Every other Spider package also ran
under the race detector. Image validation additionally passed in Linux/amd64 CI.

Publication history is explicit: `db8a059` contains the M4/image preparation;
its initial CI encountered a hosted-runner finalization cancellation after a
successful shard, then an aggregate shell-quoting error. `340906c` fixes that
command using a tested Python argument list. The final revision reran the full
protected matrix successfully. No check or assertion was bypassed.

## Exact approved artifacts

| Artifact | Identity |
|---|---|
| Case/platform | `ledger-smoke-v1`, Linux/arm64 |
| Commit | `340906c694ee6f51d67ea0c9b448b07f29df4834` |
| Harness image | `sha256:4b0ca8a3cca08646616bfe6952fc2f2423cb3bd55496ee5e0a1312be223f480a` |
| Redis image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan SHA-256 | `bf22f79cd2b23e09aa77fa288b70c190c7c04ef24384d5c48ea5d5a885c362c4` |
| Recipe SHA-256 | `2cfb26736d94c9c05188989e8da814272c7a662ac0441d045ff941c5508b078b` |
| Retained CI-gate record SHA-256 | `a7427392a4256a06b3cb82abe3c218d1c2a89ef2d6fe430e84d6034b0045c21e` |
| Renewed approval SHA-256 | `c63cbb050d81466b767ad7a55787ec450bb6df6c472f39fd4e19ee87dcd012c3` |
| Renewed expiry | **2026-09-22 17:18:43.933 UTC** (`1790097523933` ms) |

Scope is one fresh isolated case, network mode `none`, BOOT persistence/restart
and empty maintenance only, a 300-second execution budget plus 60-second cleanup.
It authorizes no public requests, crawl, rendering, retained-data mutation or
administrative promotion. The owner explicitly selected **record only** in this
step. The subsequent explicit execution request resulted in the failed attempt
linked above; the original approval bytes remain unchanged as evidence.

The original one-hour approval expired during the connectivity outage at
2026-09-21 22:19:04.925 UTC. The validator correctly rejected it with
`APPROVAL_EXPIRY`. The owner explicitly renewed the identical artifact scope;
the old approval was preserved, not silently extended. After the new expiry,
obtain another explicit approval rather than editing the timestamp.

Local approval/evidence directory:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-image-prep-2026-09-21/
```

The consumed file is `execution-approval-2026-09-22.json`; its original receipt is
`approval-receipt-2026-09-22.json`, with disposition recorded in
`execution-approval-2026-09-22.consumed.json`. The historical offline plan remains
at `docs/evidence/m4-image-prep-2026-09-21/plan.json`. The
[init correction](crawl-jobs-v2-m4-init-fix-2026-09-22.md) creates new image/plan/
recipe identities and needs a new exact-revision CI result and owner approval.
