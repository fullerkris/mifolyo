# Positive-interval source/preparation evidence — 2026-09-29

**Independent source GO for image/CI preparation; selected arm64 preparation PASS.**
Case: `ledger-positive-interval-v1`. No real positive-rate case has run.

The [manifest](manifest.json) binds eight byte-identical exports:

- [Frozen source inventory](initial.json): 101 files and the 18-path implementation delta.
- [Verification](verification.json): 150 harness/21 script tests; positive and
  existing conformance race roots; preserved aggregate timeout and successful
  separately bounded regression commands; vet and strict Lua/bundle checks.
- [Independent review decisions](verdicts.json): correctness/security GO and
  private probe/evidence identities, with review timing limits retained.
- [Inputs](inputs.json), [plan](plan.json), [recipe](recipe.json),
  [image validation](image-validation.json) and [independent image postcheck](image-postcheck.json).

Image checks cover 71 files/all 18 recipes. Four metadata-role containers stayed
stopped, and all four containers/two volumes were independently absent. Separate
networkless Python checks and Redis `--version` passed; they are not real-case
acceptance, allocator/maximum-shape or timing evidence.

The manifest/index are derived; listed exports preserve original bytes. The frozen
inventory's pending-check status is a historical snapshot; later verification and
review records carry the completed outcomes. The initial aggregate regression
timeout is not promoted to a pass; the completed split commands cover every root.

See the [dated checkpoint](../../crawl-jobs-v2-m4-positive-interval-2026-09-29.md)
for scope/private-original locations and the [implementation plan](../../crawl-jobs-v2-plan.md)
for current status. Publication, CI, exact approval and execution decisions remain
separate gates; full M4 remains open.
