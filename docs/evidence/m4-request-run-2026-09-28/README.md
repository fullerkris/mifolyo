# Request-lifecycle run evidence — 2026-09-28

**Accepted scoped PASS**, fixture `1d8b4e394dffdada8518e189538449f4`, case
`ledger-request-lifecycle-v1`, exact merged-main commit
`561774f3c2e84bab5b4a32512420f0ddb9cb1710`.

The [manifest](manifest.json) binds eleven byte-identical exports:

- [Report](report.json), [initial incomplete intent](intent.json) and
  [retained action journal](actions.jsonl).
- [Separate execution decision](execution-decision.json) and
  [one-invocation receipt](invocation.json).
- [Runtime journal/resource audit](runtime-postcheck.json),
  [final postcheck](postcheck.json) and [consumed disposition](approval-disposition.json).
- [Independent correctness review](correctness-review.json) and
  [independent defensive-security review](security-review.json).
- [Post-merge main CI verification](main-ci-gate.json).

One 12,944-ms invocation passes 22 calls, 33 counters, history/rate/expiry checks
and 46 authority denials. Both independent reviews accept the scoped evidence.
Six-role revocation, original 28-action journal/file identity and exact name/label
absence of four containers/two volumes were verified. Approval is consumed and
non-reusable; the original evidence scan has zero findings.

The manifest/index are derived; listed exports retain original bytes. The initial
intent and intermediate runtime audit retain their original incomplete/pending
classification. Final disposition is in the postcheck and consumed receipt.
The copied journal does not claim the original inode; that identity was verified
privately before byte-identical export.

Six public reply fingerprints are independently recomputed; sixteen private-bearing
replies and START permission bits rely on reviewed producer comparisons. Full
private state/identities cannot be recovered from hashes. No full M4, positive-rate,
shared-concurrency, after-I/O recovery or request-durability acceptance follows.

See the [dated run report](../../crawl-jobs-v2-m4-request-run-2026-09-28.md) for
scope and private-original locations, and the [implementation plan](../../crawl-jobs-v2-plan.md)
for current status. This local evidence/status batch is not yet committed or published.
