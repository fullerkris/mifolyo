# Cancellation merge and main-CI checkpoint — 2026-10-07

**PASS: PR #20 is merged as `ab5f21a`; exact merged-main CI and downloaded evidence
are verified.** The merge has the same tree as reviewed PR head `4e699ec`. All
fourteen required contexts and twenty-three workflow jobs pass.

The [manifest](manifest.json) binds **seven byte-identical exports**:

- [Publication closeout](publication-closeout.json): the 60-path result checkpoint,
  separately approved three-file dependency follow-ups and final PR CI.
- [Final PR CI gate](pr-ci-gate.json): exact `4e699ec` verification, including its
  retained missing-Spider workflow failure/retry chronology.
- [Merge receipt](merge.json): actual squash commit, parent and reviewed-tree match.
- [Local alignment](alignment.json): documentation branch based at the already
  aligned local main, with unrelated/C0/design work preserved.
- [Merged-main CI gate](main-ci-gate.json): actual `ab5f21a` push workflows, checks,
  downloaded artifacts and raw-log bindings.
- [Merge closeout](merge-closeout.json): completed merge/main-CI gate and preserved
  original execution/consumed-approval provenance.
- [Next-slice handoff](next-slice.json): proposed C0 native-runtime admission and
  oracle/client-closure implementation scope.

## Verified bindings

| Binding | Identity |
|---|---|
| Merge | `ab5f21a7421f3ed48cfb32481613fa5656dcb8a4` |
| Reviewed final PR head | `4e699ece2f7f8ca3fc59a19b18d0e38ddb9604f9` |
| Merge parent | `e80d00538b11c46d021427bcfe1c408e16071c68` |
| Identical tree | `1d9abb85e61d9ca8e0d4ef93857afcf6b82e71b7` |
| Main-CI gate SHA-256 | `7991442ee8e72ef36eab96cde19b7269fae4bbc42d8b3d7d4749a061babf6b55` |
| Closeout SHA-256 | `b4bb2da44eca95916dccebcbd359829c57da285bcecd039a3b5710d6bd3fb7af` |

[Required Checks](https://github.com/fullerkris/mifolyo/actions/runs/37663385780)
and [Unit Tests](https://github.com/fullerkris/mifolyo/actions/runs/37663385581)
both pass on attempt 1. Eight downloaded reports account for 480 Go roots:
479 passes and only the permitted optional native-parity skip. All eleven M4
roots pass once. Raw logs verify 183 harness tests, 21 script tests and both PHP
suites, with zero failures/skips and their existing warning classifications.
The amd64 image evidence matches 74 files/all 20 recipes; it selects claim/release.
Composer and built-image npm audits pass.

The original cancellation run remains bound to `22317dc`, fixture `cda9d337…`,
and its consumed approval. Its five original files and identities are preserved.
The closeout retains the original dated cleanup evidence rather than claiming a
new absence measurement or another invocation.

## Reading historical states

The publication closeout predates merge and still records a draft/unmerged PR.
The merge receipt records main CI as pending at intake. The alignment receipt
also predates CI completion. **`merge-closeout.json` records the final outcome**;
the earlier exact bytes are intentionally preserved.

The manifest/index are derived. Full main-CI downloads, raw logs and verification
scripts remain private under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-cancellation-merge-2026-10-07/`.
Publication originals remain in sibling `m4-cancellation-result-publication-2026-10-07/`.

See the [merge report](../../crawl-jobs-v2-m4-cancellation-merge-2026-10-07.md),
[original run](../../crawl-jobs-v2-m4-cancellation-run-2026-10-06.md) and
[current implementation plan](../../crawl-jobs-v2-plan.md). Full M4 remains open;
C0 runtime preparation is the next implementation increment.
