# C0 baseline reconciliation evidence — 2026-10-07

**PASS: `feature/crawl-jobs-v2-c0-observer` advanced from `e80d005` to verified
merged main `ab5f21a`; the same 78-test C0 baseline passes with zero skips.**

The [manifest](manifest.json) binds four byte-identical exports:

- [Preflight summary](preflight-summary.json): reviewed source/native bindings,
  read-only snapshots, verified target and disjoint incoming paths.
- [Alignment](alignment.json): exact fast-forward command, branch/head/tree and
  before/after preservation results.
- [Verification](verification.json): command, exact 78-test identity list, log
  hashes and preserved source/artifact identities on the reconciled base.
- [Reconciliation](reconciliation.json): completed baseline reconciliation and
  the unchanged source inventory joined to `ab5f21a`.

The 72 existing local files, eight native build inputs and eleven native artifacts
retain their bytes and identities. The primary worktree's 372 unrelated files and
the design worktree's 22 local files are preserved. No tracked C0 edit or staged
change was introduced. These seven new documentation/evidence paths bring the
local C0 overlay to 79 files.

The preflight records its pre-alignment state, and alignment records test
verification as pending. `reconciliation.json` records the completed outcome;
the earlier exact bytes are preserved. The previous inventory/review files keep
their original base identity and are not rewritten to imply a new source review.

Tests use offline models, fake Docker and benign Python process fixtures. Native
runtime admission implementation, native experiments and full M4 acceptance remain
future work. The main-CI result covers the tracked baseline; this local test run
supplies the separate C0 verification.

Private originals, logs and read-only source/native snapshots remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-c0-reconciliation-2026-10-07/`.

See the [reconciliation report](../../crawl-jobs-v2-m4-c0-reconciliation-2026-10-07.md)
and [prior adapter evidence](../m4-c0-adapters-2026-10-06/README.md).
