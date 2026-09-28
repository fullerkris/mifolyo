# Pre-I/O recovery run evidence — 2026-09-27

**Accepted scoped PASS** for `ledger-worker-death-pre-io-v1`, fixture
`d02e7fa6424d446089fd1333206bb294`, at exact published commit
`c009282ba5473e89984e95466efec37bf7ab2a3a`.

## Exact exports

The [manifest](manifest.json) records the SHA-256 of each byte-identical export:

- [Report](report.json), [initial incomplete intent](intent.json), and
  [retained action journal](actions.jsonl).
- [Separate execution decision](execution-decision.json) and
  [single invocation receipt](invocation.json).
- [Independent correctness review](correctness-review.json) and
  [independent defensive-security review](security-review.json).
- [Journal/resource postcheck](postcheck.json) and
  [consumed, non-reusable approval disposition](approval-disposition.json).
- [Exact-revision CI verification](ci-gate.json), linked to downloaded race/image
  artifacts and the public workflow runs.

One 79,457-ms invocation passed thirteen operations, 38 counters and 46 authority
denials. Worker A was observed stopped 654 ms after host receipt; twenty Redis-TIME
observations crossed its lease. Six-role revocation, the original 55-action
journal/file identity, five-container name/ID absence and two-volume absence were
verified. The scan of the original evidence found zero secrets.

The manifest and readable index are derived; listed exports retain their original
bytes. Approval/private preflight originals and review scripts stay in the private
execution folder identified by the [dated report](../../crawl-jobs-v2-m4-recovery-run-2026-09-27.md).
The original journal inode identity is attested by the private postcheck; the
exported copy is independently byte-identical and does not claim that inode.

Private full state cannot be reconstructed from exported hashes. Historical
source/preparation reviews retain their pre-execution classification. This case
does not establish internal crash cuts, all-transition durability, after-I/O
recovery, maximum shapes, p99, service integration or full M4 acceptance.

Current status belongs to the [implementation plan](../../crawl-jobs-v2-plan.md).
This evidence batch is local and uncommitted after the published `c009282`
execution checkpoint; no additional invocation or publication is authorized here.
