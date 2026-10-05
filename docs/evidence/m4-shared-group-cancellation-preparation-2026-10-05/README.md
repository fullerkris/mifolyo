# Cancellation selected image evidence — 2026-10-05

**PASS for selected Linux/arm64 image preparation**, case
`ledger-shared-group-cancellation-v1`. The [manifest](manifest.json) binds seven
byte-identical exports:

- [Pinned-base build](build.json) and [selected preparation invocation](preparation-invocation.json).
- [Immutable inputs](inputs.json), [selected plan](plan.json) and [recipe](recipe.json).
- [Image/stopped-role validation](image-validation.json).
- [Independent binding and resource-absence postcheck](image-postcheck.json).

Harness/stand-in:
`sha256:e57da19e8b99ffbd630545730b3883d00f6088a5f56068ca0b5aed2bba32d0ee`.
Plan: `da7ab623673d9b221a8e47a9f4c77fcf983af3636badb90016a03af37542d7c3`.
Recipe: `b36d4bb7d2a4e657afbcda6a9cc3160b68678b19d590f7fb8c75b1f4de6d7a4f`.

Both Python image checks match all **74 files / 20 recipes**. Four metadata roles
were admitted while stopped; exact container/volume names and fixture/case/check
label queries independently confirm cleanup. Image-check memory observations
are below their limits, not workload benchmarks. No acceptance Redis server or
cancellation case was started.

The manifest and index are derived; the seven exports retain original bytes.
The [source review](../m4-shared-group-cancellation-review-2026-10-05/README.md)
and [initial implementation](../m4-shared-group-cancellation-2026-10-05/README.md)
keep their original recording-time states. Private preflight and logs remain
outside the repository.

See the [preparation report](../../crawl-jobs-v2-m4-shared-group-cancellation-preparation-2026-10-05.md)
and [implementation plan](../../crawl-jobs-v2-plan.md). Scoped publication/exact CI
and separate artifact/execution decisions remain ahead; full M4 is open.
