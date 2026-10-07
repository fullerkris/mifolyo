# Cancellation publication / CI evidence — 2026-10-06

**PASS at published `22317dc`**, draft [PR #20](https://github.com/fullerkris/mifolyo/pull/20).
The [manifest](manifest.json) binds fifteen byte-identical records:

- [Publication receipt](publication.json) and [exact-revision CI gate](ci-gate.json).
- [Downloaded amd64 image validation](image-validation-amd64.json).
- Eight disjoint race reports: [0](shard-0.json), [1](shard-1.json), [2](shard-2.json),
  [3](shard-3.json), [4](shard-4.json), [5](shard-5.json), [6](shard-6.json), [7](shard-7.json).
- Preserved hosted-startup failures: Required Checks
  [attempt 1](required-startup-attempt1.json), [attempt 2](required-startup-attempt2.json),
  [attempt 3](required-startup-attempt3.json), and Unit Tests [attempt 1](unit-startup-attempt1.json).

All fourteen strict required contexts pass. The reports account for 480 compiled
roots/479 passes/one permitted optional skip, all eleven M4-prefixed roots,
183 final-source harness tests, 21 script tests and 74 image files/all 20 recipes.
PHP warning classifications are preserved rather than relabeled warning-free.
CI selects claim/release for amd64 preparation; the cancellation arm64 plan/image
has its own [selected-case evidence](../m4-shared-group-cancellation-preparation-2026-10-05/README.md).

The manifest/index are derived. Raw job logs, source-stage fingerprints, scan
triage and download receipts remain private. No real cancellation execution,
merge, application activation or full-M4 acceptance is implied.

See the [CI report](../../crawl-jobs-v2-m4-cancellation-ci-2026-10-06.md) and
[implementation plan](../../crawl-jobs-v2-plan.md). These are local post-CI records;
the original published head and tested tree remain explicitly bound.
