# C0 baseline reconciliation — 2026-10-07

**PASS: the preserved C0 worktree is reconciled with verified merged `main`, and
the existing 78-test baseline passes.** The owner selected baseline reconciliation
as C0 step 1. The branch advanced by fast-forward from `e80d005` to `ab5f21a`;
all 72 existing local files retain their original bytes, modes and file identities.

## Baseline and preservation

| Binding | Identity |
|---|---|
| C0 branch | `feature/crawl-jobs-v2-c0-observer` |
| Previous base | `e80d00538b11c46d021427bcfe1c408e16071c68` |
| Reconciled base | `ab5f21a7421f3ed48cfb32481613fa5656dcb8a4` |
| Reconciled tracked tree | `1d9abb85e61d9ca8e0d4ef93857afcf6b82e71b7` |
| Reviewed 22-file source inventory | `c754392b6a3f8ebbd03601a626bcce908da4a86ebe82d8cea8ef213913eb74e6` |
| Prior independent review verdicts | `a3d9e0853ab4eaf2d6615070e2172bd94729b012d3088ca846ecf671aa8adfb8` |
| Verified merged-main CI | `7991442ee8e72ef36eab96cde19b7269fae4bbc42d8b3d7d4749a061babf6b55` |

The incoming 98-path main change set has no file or parent/child path collisions
with the 72-file C0 overlay. Both the overlay and the eleven native build artifacts
were copied to private read-only snapshots before the branch move. Eight native
build inputs and all eleven artifacts match their recorded hashes before and after
reconciliation and testing.

The primary worktree remains on the documentation branch at `e9e6aed`, with all
372 unrelated pending-file fingerprints preserved. The original design worktree
retains its `e80d005` head and 22 local files. PR #21 was open when the target was
selected; the target here is the already merged and verified `ab5f21a` baseline.
All worktree indexes remain empty. The fast-forward created no new commit and
performed no push or stash.

Main CI covers the merged tracked baseline. The uncommitted C0 package is covered
by the local baseline run recorded below. Its earlier source-review records retain
their original `e80d005` bindings; this checkpoint joins those unchanged source
bytes to the new tracked baseline.

## C0 baseline verification

Run from the C0 worktree:

```sh
python3 -B -W error::ResourceWarning -m unittest discover -s tests/crawl-jobs-v2-observer -v
```

- **78 tests pass, zero skips**, with ResourceWarnings treated as errors.
- All 78 successful test IDs match the reviewed adapter baseline.
- Python 3.12.7; command elapsed time 4,454 ms, exit 0.
- The suite covers offline contracts, fake Docker metadata and bounded benign
  Python pipe/watchdog processes, including owner-process exit.
- All 22 package files, eight native build inputs and eleven native artifacts
  retain their reviewed hashes. No compatibility edits were needed.

| Record | SHA-256 |
|---|---|
| Alignment | `d5fe5d8c475b23f79478b0de4bb2b592b1d275e150918b7a2ec985de791376bc` |
| Reconciled test verification | `04dda9f56c7eaa2fa54e8b10e1c54339fc7fcdec3b52b6c498555a76bf2c85ec` |
| Combined reconciliation | `535c062bac8577aea6d1cd3562789b4604ada4c43dc737994a27b796bf15efd2` |

## Result and next work

The C0 branch is ready for the **native-runtime admission implementation** increment,
followed by independent oracle/client-closure integration. Reconciliation itself
adds no admission feature and proves no native tracing feasibility. Native actors,
Docker mutations and tracing were not exercised; the retained native binaries were
not rebuilt. Full trial/image assembly and experiment approvals remain later work.

Four exact records, their manifest/index and this report add seven local
documentation/evidence files. The original 72 files remain intact, giving 79 local
untracked C0 paths. Existing reviews retain their scope; full M4 remains open.

See the [evidence index](evidence/m4-c0-reconciliation-2026-10-07/README.md),
[reviewed adapter layer](crawl-jobs-v2-m4-c0-adapters-2026-10-06.md) and
[package contract](../tests/crawl-jobs-v2-observer/README.md).
