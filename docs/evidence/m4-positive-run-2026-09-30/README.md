# Positive-interval run evidence — 2026-09-30

**Accepted scoped PASS**, case `ledger-positive-interval-v1`, fixture
`99e1a00b4804b4e104e4d638cdb81b19`, executed published commit
`743159909c89cc63b1a4b67cff4cca323cd85e26`.

The [manifest](manifest.json) binds sixteen byte-identical exports:

- [Report](report.json), [initial incomplete intent](intent.json) and
  [original action-journal bytes](actions.jsonl).
- [Separate execution decision](execution-decision.json) and
  [one-invocation receipt](invocation.json).
- [Authority/source/CI postcheck](provenance-postcheck.json),
  [combined scoped postcheck](postcheck.json) and
  [consumed approval disposition](approval-disposition.json).
- [Independent correctness review](correctness-review.json) and
  [independent defensive-security review](security-review.json).
- [Exact-revision protected PR CI verification](pr-ci-gate.json).
- The first approval's [expiry refusal](expired-approval-stop.json) and
  [expired-unused disposition](expired-approval-disposition.json).
- Actual zero-finding scanner results for [original evidence](secret-scan-evidence.json),
  [execution decision](secret-scan-decision.json) and [invocation](secret-scan-invocation.json).

The first approval expired before reservation or invocation. Fresh exact-artifact
approval and a subsequent execution decision preceded the one 21,736-ms run.
All 24 calls, 33 counters and 46 denials pass. The block is 7,432 ms before its
8-second deadline; four observer samples cross it by 818 ms, then admission follows
1,793 ms after the deadline. The complete Redis measurement spans 12,748 ms.

Both independent reviewers accept the scoped evidence. Six-role revocation,
original 33-action journal/file identity and exact name/label absence of four
containers/two volumes pass. The renewed approval is consumed and non-reusable.
The original five-file scan covers 194,040 bytes with zero raw findings and no
dismissals or suppressions. Scanner logs remain privately retained and hash-bound
in the security review.

The manifest/index are derived. Listed exports preserve original bytes and
historical classifications, including the initial incomplete intent and the
reservation marker referenced by the provenance audit. Copied journals do not
claim the original inode; that identity was checked on the retained original.

Seven public reply hashes are independently reconstructed; seventeen replies
contain discarded reservation IDs, and START permission bits remain unexported.
Full private state relies on reviewed producer comparisons. Ten distinct runtime
envelopes cover twelve stage completions, with two journal-only ready completions
and a duplicate stored final clock envelope. Resource absence is by exact names
and labels, not unavailable container IDs. No independent origin-blocking,
shared-concurrency, real network I/O, after-START durability or full-M4 acceptance
follows from this case.

See the [dated result](../../crawl-jobs-v2-m4-positive-run-2026-09-30.md) for scope,
limits and private-original locations, and the
[implementation plan](../../crawl-jobs-v2-plan.md) for current status. This local
result/status batch is not yet committed or published.
