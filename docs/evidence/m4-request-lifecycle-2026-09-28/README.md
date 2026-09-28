# Request lifecycle source and preparation evidence — 2026-09-28

**Corrected source GO for image/CI preparation; selected arm64 preparation PASS.**
Case: `ledger-request-lifecycle-v1`. No real case has run.

The [manifest](manifest.json) binds byte-identical exports:

- [Initial source inventory](initial.json) and [corrected source inventory](corrected.json):
  96 files each, with exactly three paths changed after independent findings.
- [Verification](verification.json): both initial and corrected local check
  records and original log hashes; corrected scope has 140 harness/21 script
  tests, four Go roots under race, vet and strict Lua/bundle pins.
- [Independent review decisions](verdicts.json): initial NEEDS_WORK, closed
  production-adapter/failure-prefix findings and corrected GO decisions.
- [Preparation inputs](inputs.json), [plan](plan.json), [recipe](recipe.json),
  [image validation](image-validation.json) and [independent postcheck](image-postcheck.json).

The image scope is 69 files and 17 recipes. Four metadata role containers remained
stopped, and all four containers/two volumes were independently absent. Separate
networkless image checks passed. Image-check memory peaks are not workload or
maximum-shape measurements; the Redis invocation was `--version` only.

These artifacts do not grant publication or execution authority. The new image,
recipe and source must pass their own publication/CI and exact approval gates.
Source GO and simulated results are not real-Redis acceptance or full M4.

The manifest/index are derived; listed JSON exports preserve private original
bytes. Earlier recovery-run evidence remains separately bound to its executed
checkpoint. See the [dated checkpoint](../../crawl-jobs-v2-m4-request-lifecycle-2026-09-28.md)
and the mutable [implementation plan](../../crawl-jobs-v2-plan.md).
