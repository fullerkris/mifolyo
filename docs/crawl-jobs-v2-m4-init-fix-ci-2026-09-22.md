# M4 corrected checkpoint: CI and fresh approval — 2026-09-22

**Result: all fourteen required contexts pass; fresh one-case approval recorded.**
At recording time, the owner selected **Approve, record only** and no smoke
retry had started.

**Subsequent disposition:** PR #10 merged as `ff2457e`; after a separate explicit
execution request, this approval was consumed by the
[passing corrected smoke case](crawl-jobs-v2-m4-smoke-pass-2026-09-22.md).
All six resources were confirmed absent. The remaining sections preserve the
earlier approval-recording snapshot and must not be read as reusable authority.

This local post-CI record preserves the tested/approved HEAD; committing it would
create a different revision requiring new CI and approval. The
[implementation plan](crawl-jobs-v2-plan.md) remains the current status source.
The [diagnosis/review report](crawl-jobs-v2-m4-init-fix-2026-09-22.md) records the
correction; the first attempt's FAIL evidence and consumed approval are preserved.

## Published checkpoint and verification

- Commit: `a02991c322c3472f7460adbb94b2a15d82b77af6`, local/remote identity verified.
- Branch: `feature/crawl-jobs-v2-lua`; [PR #10](https://github.com/fullerkris/mifolyo/pull/10) remains draft and unmerged.
- Base: `d914a93f9ade5b63182ecf02092c1a1e74633713`.
- Tested merge tree: `45c3c08f2cfe48222b0be31349e87af9ce063b56`.
- [Required Checks 35762828908](https://github.com/fullerkris/mifolyo/actions/runs/35762828908): SUCCESS.
- [Unit Tests 35762828920](https://github.com/fullerkris/mifolyo/actions/runs/35762828920): SUCCESS.

All fourteen protected contexts were individually checked against the actual
branch-protection inventory. The eight downloaded race reports match the tested
merge revision and the locally compiled test inventory: **470 roots, 469 passed,
only `TestJobLuaNativeFactoryParity` optionally skipped**. Their complete/disjoint
coverage was revalidated with the repository's report verifier. Every other
Spider package passed the aggregate race job.

The retained Linux/amd64 image report passes all four stopped-role admissions,
exact reviewed 55-file content/source identity, memory bounds and verified cleanup.
This complements the committed Linux/arm64 report. Neither image preparation
started a Redis server. Retained CI-gate SHA-256:
`56246cb416438a03e21cf18cb81f9354b6bad88441375bb080eabfb6be38db6d`.

The 22-file scoped checkpoint matched its tested index export. The 53 harness
tests, 21 script tests, Go wire/race test (2.740 s), generator/digest and artifact
checks passed on that export. Two Gitleaks matches were independently confirmed
as repeated public `cj2_retire_legacy_keys.lua` SHA-256 values; no unresolved
credential finding remained. Unrelated user work was excluded.

## Exact fresh approval

| Item | Value |
|---|---|
| Case/operator/platform | `ledger-smoke-v1` / `fullerkris` / Linux/arm64 |
| Commit | `a02991c322c3472f7460adbb94b2a15d82b77af6` |
| Harness image | `sha256:2059066be4f192b84d4932050d1f811ed2bacf275e7d7cc0179f3e709d50e12c` |
| Redis 7.4.11 image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan | `d06a4ef887125883ecb1f0924f9192f4070bdb01c003d17ab718ca4baa451fbb` |
| Recipe | `9b0adc08f054775c24922a84012d2c4dbd950f630e151226536bd2e619ff81ab` |
| Approval SHA-256 | `c057e40cb5ebad30a98aa178382b9621706108214b820359fd998c9561dd25da` |
| Recorded | 2026-09-22 18:46:36.204 UTC |
| Expiry | **2026-09-22 19:46:36.204 UTC** (`1790106396204` ms) |
| Bounds | 300-second case plus 60-second cleanup, one attempt |

Approval and receipt were written exclusively, mode 0600, outside repository and
Docker volumes. The validator accepted the recorded bytes after rechecking exact
HEAD/source cleanliness, plan/recipe identities, local immutable images and all
fourteen required contexts. The old consumed approval's hash was also rechecked.

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-init-diagnosis-2026-09-22/execution-approval.json
```

`approval-receipt.json`, `ci-gate.json`, `ci-shards/` and `ci-image-amd64/` are
retained alongside it. Plan and recipe live in
[`docs/evidence/m4-init-fix-2026-09-22/`](evidence/m4-init-fix-2026-09-22/).

## Next execution gate

The owner requested recording only. A separate explicit run request is needed;
immediately before execution, revalidate live expiry, the whole case budget,
exact revision/artifacts, image availability and absence of conflicting resources.
Expired or consumed approval cannot be extended or reused silently. Full M4
acceptance, merge, application integration and operational activation remain open.
