# Shared-group cancellation local source checkpoint — 2026-10-05

**Implemented and locally verified; independent source review pending.**
Case `ledger-shared-group-cancellation-v1` is local and uncommitted on the branch
based at `e80d00538b11c46d021427bcfe1c408e16071c68`. No real case or new image
validation is claimed.

The [manifest](manifest.json) binds four byte-identical records:

- [Final source inventory](source-inventory.json): 108 files, twelve changed paths,
  twenty recipe identities, 22 scenarios and the unchanged 74-file image path set.
- [Verification chronology](verification.json): original check/log hashes,
  full 183-test pre-guard result, final 33-test shared-family regression and the exact
  two-file final ACL-guard delta.
- [Closed cancellation recipe](recipe.json): nine calls, five mutations, one
  capacity denial, two replays, one maintenance pass, 60 counters and 46 denials.
- [Baseline main CI](base-main-ci.json): verified `e80d005`, before these source
  changes; not CI acceptance of this implementation.

The full pre-guard 183 harness tests, 21 script tests, 81 canonical Lua invocations
under Go race, vet and strict pins pass. The final case-id/fixture guard and its
cross-case assertions then pass all 33 shared-family tests. No full 183-test rerun after
that final guard is claimed. The manifest and this index are derived; listed
records preserve the original bytes and verification classifications.

All nineteen prior recipe semantics are unchanged apart from source-file hashes.
Those changed hashes still require fresh images and exact-revision CI. Runtime
registration is not execution approval; local fake lifecycles are simulations.
Independent source review, image/preparation, publication/CI and the separate
artifact/execution decisions remain ahead. Full M4 remains open.

See the [dated checkpoint](../../crawl-jobs-v2-m4-shared-group-cancellation-2026-10-05.md)
and [current implementation plan](../../crawl-jobs-v2-plan.md).
