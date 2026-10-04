# Shared-group publication, merge and main CI — 2026-10-03

**PASS: PR #18 is merged and its exact merged-main revision is verified.**
This checkpoint records publication and merge continuity for the accepted
`ledger-shared-group-capacity-v1` result. It does not represent another real run.
These retained October 3 records were added to the documentation on October 4.

## Exact revisions

| Binding | Identity |
|---|---|
| Original executed source | `c381287384d41f243516a851dd6ded6f56b9fcad` |
| Result publication / reviewed PR head | `c8c5ee2bb4d7ce8b61546dbea2a98529542d8265` |
| Actual squash merge | `d3b241e8790614957b68e2106d9dd1bda91dfae4` |
| Merge parent | `7da55b14b32c79ec4b2b95b0a9fd31dda40f0571` |
| Identical PR-head / merged tree | `6a42a0331da9e4ab59baee02b20d07e85fc24b64` |
| Merge time | 2026-10-03 13:35:55 UTC |
| PR | [#18 — merged](https://github.com/fullerkris/mifolyo/pull/18) |

Result publication comprised 22 files, including 15 byte-identical run exports.
The complete PR had two commits/67 paths. All 107 reviewed source hashes remained
unchanged through publication and merge; the real run retains its original
`c381287` plan/recipe/image bindings and consumed approval.

## Exact evidence

The [manifest](manifest.json) binds **seven byte-identical exports**:

- [Publication closeout](publication-closeout.json): completed result publication,
  matching local/remote head, source preservation and exact-head PR CI.
- [PR CI gate](pr-ci-gate.json): verified checks and downloaded evidence for `c8c5ee2`.
- [Merge receipt](merge.json): actual squash commit, parent and reviewed-tree match.
- [Local alignment](alignment.json): local `main` alignment and preserved pending work.
- [Merged-main CI gate](main-ci-gate.json): verified push workflows for `d3b241e`.
- [Combined merge closeout](merge-closeout.json): completed merge/main gate and
  retained original-run provenance.
- [Next-slice handoff](next-slice.json): source-grounded cancellation proposal,
  explicitly not implemented or registered.

| Verification record | SHA-256 |
|---|---|
| PR CI | `954e6a42a48385a91cc13c8ddb31f1d241df479ac6fa5c1507c6cb508706f4d2` |
| Main CI | `db86bef93ff6930007ef7e5d971bd663a9b26b76626db294fc67a0b10c51afc7` |
| Merge closeout | `1fa82c2d15afc1822e55e3a3d816beb04a1eb58ed1cadacded4696015d7931bb` |
| Next-slice handoff | `b28218b607bf54ea2bb11d83f4d2b0ee3de25765a8a4ddd6d80fde0bc5b83002` |

## Merged-main verification

- [Required Checks — 37126728098](https://github.com/fullerkris/mifolyo/actions/runs/37126728098)
- [Unit Tests — 37126728079](https://github.com/fullerkris/mifolyo/actions/runs/37126728079)

Both push workflows completed successfully on attempt 1 for the actual merge
commit. All **14 required contexts** pass. Eight downloaded race reports match the
independently compiled **480-root inventory**: 479 passes and only the permitted
optional `TestJobLuaNativeFactoryParity` skip. All seven M4 roots pass once.
Raw job logs bound to successful step metadata/times confirm **171 harness and
21 script tests**, with zero Python skips.

Forum completed 129 tests/547 assertions: 85 warning results and 44 warning-free
passes. Query completed 31 tests/193 assertions: 30 warning results and one
warning-free pass. Both have zero failures/skips; warnings are not relabeled.
Composer audits pass. Downloaded amd64 image evidence verifies **74 files/all
19 recipes**, stopped-role admission and cleanup. Its selected case is
claim/release; selected shared-group arm64 preparation remains separate.

Local `main` matched the remote merge, the staging index was empty, and all 372
unrelated pending-file fingerprints were preserved. The final closeout rechecked
the original run journal identity, consumed disposition, selected arm64 images,
six owned-resource absences and six empty label queries. These are dated
verification results, not new case invocations or full M4 acceptance.

## Record interpretation and next work

Exports retain their recording-time classifications. In particular,
`publication-closeout.json` predates merge and `alignment.json` predates main-CI
completion. `merge-closeout.json` records the final outcome.

The initial alignment preflight stopped because local `main` had already advanced
to the verified squash commit. The observed `previous_local_main` in
`alignment.json` supersedes the earlier assumed `local_main_before_alignment`
field in `merge.json`; the original records are preserved, not rewritten.

The next proposed case is **`ledger-shared-group-cancellation-v1`**. Its existing
nine-call offline control checks A's pending cancellation, replay, B's admission,
historical A replay while B holds capacity, and B's START/FINISH. Executable
integration and independent review are next; the current registry remains
19 cases and no real cancellation run exists.

The manifest/index are derived; all seven listed records preserve their original
bytes. Full CI downloads, logs and private verification scripts remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-shared-group-merge-2026-10-03/`;
publication originals remain in sibling `m4-shared-group-result-publication-2026-10-02/`.

See the [current F3 plan](../../crawl-jobs-v2-plan.md),
[cancellation implementation handoff](../../crawl-jobs-v2-plan.md#next-bounded-slice-shared-group-cancellation),
and [original run report](../../crawl-jobs-v2-m4-shared-group-run-2026-10-02.md).
Application V2 remains dormant; **full M4 remains open**.
