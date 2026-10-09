# Cancellation publication, merge and main-CI closeout — 2026-10-07

**PASS: PR #20 is merged and its exact merged-main revision is verified.** The
98-path PR contains the bounded cancellation implementation, accepted run evidence
and separately approved Query Engine dependency fixes. Its squash commit has the
same tree as the reviewed final PR head. All fourteen required main checks and
all twenty-three workflow jobs pass. Full M4 remains open.

## Revision continuity

| Binding | Identity |
|---|---|
| Original executed source | `22317dc21017f6157e5338a23c685a908be0bce4` |
| Sixty-path result/status publication | `8fb7f590918fa50f95056a8866c1d1f3bb84035c` |
| MongoDB security-patch commit | `6460c3796cc35e315a5a2d6032d7620f6f414d64` |
| Final reviewed PR head / npm-patch commit | `4e699ece2f7f8ca3fc59a19b18d0e38ddb9604f9` |
| Actual squash merge | `ab5f21a7421f3ed48cfb32481613fa5656dcb8a4` |
| Merge parent | `e80d00538b11c46d021427bcfe1c408e16071c68` |
| Identical reviewed-head / merged tree | `1d9abb85e61d9ca8e0d4ef93857afcf6b82e71b7` |
| Merge time | 2026-10-07 17:59:45 UTC |
| PR | [#20 — merged](https://github.com/fullerkris/mifolyo/pull/20) |

The four PR commits, exact 98-path scope, actual merge parent, local/remote main
and same-tree relationship were checked. All **108 reviewed runtime-source hashes**
match both the original executed-source inventory and the merge. The source
inventory remains `2221812ca56635055b8acb6271b2fd0fbf27f8d603703171c54fc58bb14062d4`.

The dependency follow-ups fix `mongodb/mongodb` at 1.21.5, `shell-quote` at 1.11.0
through a targeted `concurrently` override, and `source-map-js` at 1.2.2. They address
CVE-2026-88023, CVE-2026-102422 and CVE-2026-93749. Original audit failures remain
retained; no audit suppression, forced downgrade or workflow weakening was used.

## Exact merged-main verification

- [Required Checks — 37663385780](https://github.com/fullerkris/mifolyo/actions/runs/37663385780)
- [Unit Tests — 37663385581](https://github.com/fullerkris/mifolyo/actions/runs/37663385581)

Both are successful **attempt-1 push workflows on `main` at `ab5f21a`**. Their
results are verified independently of the earlier PR checks.

| Verification | Result |
|---|---|
| Strict protected contexts | 14/14 SUCCESS, including Test Spider Service |
| Workflow jobs | 23/23 SUCCESS |
| Compiled Go race inventory | 480 roots accounted for once across eight downloaded reports |
| Go outcomes | 479 passes; sole permitted optional `TestJobLuaNativeFactoryParity` skip |
| M4-prefixed subset | All eleven roots pass once |
| Python harness / script tests | 183 / 21, zero skips |
| amd64 image evidence | Exact 74-file inventory, all 20 recipes, identities and isolation match |
| Forum PHP | 129 completed / 547 assertions: 85 warning results and 44 warning-free passes |
| Query PHP | 31 completed / 193 assertions: 30 warning results and one warning-free pass |
| PHP failures / skips | Zero; existing warning classifications retained |
| Dependency audits | Both Composer audits pass; built Query Engine image npm audit reports zero vulnerabilities |

The Go reports name the **actual squash commit**, not the earlier synthetic PR
merge ref. Their inventory matches an independent local compiled listing. Python
and PHP counts are bound to unique raw job-log command blocks and successful
step time windows. Query Engine installs MongoDB library 1.21.5 with extension
1.21.0 in CI.

The downloaded amd64 preparer selects claim/release while checking all recipes;
it is not a new cancellation run. The original cancellation-specific arm64
preparation remains separately bound in the
[preparation report](crawl-jobs-v2-m4-shared-group-cancellation-preparation-2026-10-05.md).

| Verification record | SHA-256 |
|---|---|
| Final PR CI | `12065831758b15064470e1d46e5c07ee2da47926ad50a7c92a25030e9561ee5f` |
| Merged-main CI | `7991442ee8e72ef36eab96cde19b7269fae4bbc42d8b3d7d4749a061babf6b55` |
| Merged-main amd64 image validation | `d666f9c4a6dbe984e843258df4db91f25f8d2f47fb8d94378a252c648732ecbe` |
| Merge closeout | `b4bb2da44eca95916dccebcbd359829c57da285bcecd039a3b5710d6bd3fb7af` |
| Next implementation handoff | `ffa9415bca2575810a3525af67aca8d6c071144f66dce5d6104d4ee0959b6e0d` |

Final PR CI had required one full Unit Tests retry after nineteen successful jobs
were recorded without the required dependent Spider job. GitHub refused a failed-job
retry; the unchanged full workflow then passed. Those records remain historical;
the merged-main workflows both passed on their first attempts.

## Original execution and local alignment

The accepted fixture `cda9d3372d72a70b1bf3349b6ee2462f` retains its original
`22317dc` plan/recipe/images and single 14,130-ms invocation. Its five original
outputs, including the 28-action journal, still match their bytes and original
device/inode identities. The renewed approval remains consumed/non-reusable.
See the [run and independent acceptance](crawl-jobs-v2-m4-cancellation-run-2026-10-06.md).

This checkpoint preserves the earlier cleanup observations and does not assert
a new Docker absence measurement. Merge and CI do not expand the case into
concurrent-worker, elapsed-aging, all-state AOF or maximum-shape performance proof.

Local `main` was already at `ab5f21a`. Documentation work is on
`docs/crawl-jobs-v2-cancellation-merge-checkpoint`, based on that merge. The staging
index is empty, and all 372 unrelated pending-file fingerprints are preserved.
The separate C0 and design worktrees retain their 72 and 22 local paths, respectively.
This closeout documentation is a local follow-up to the verified merged tree.

## Next implementation task

Continue **C0 native-runtime admission and oracle/client-closure integration**
from the existing 22-file, 78-test source-reviewed package. First reconcile the
preserved C0 worktree, still based at `e80d005`, with the merged baseline. Then
implement the process/kernel admission receipts and bounded independent worker
bridge, with meaningful rejection controls and source review.

Full trial dispatch, exact runtime images, actual-end timing/resource evidence and
separate experiment approvals remain subsequent work. The 276 proposed trial IDs
are not completed trials. C1 escalation and any D02 normative decisions retain
their separate gates. Application V2 remains dormant and **full M4 is open**.

The [evidence index](evidence/m4-cancellation-merge-2026-10-07/README.md) binds seven
byte-identical exports. The [implementation plan](crawl-jobs-v2-plan.md) owns the
updated baseline and next-task status.
