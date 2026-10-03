# Shared-group run evidence — 2026-10-02

**Accepted scoped PASS**, case `ledger-shared-group-capacity-v1`, fixture
`e47d57da09b95aec8dd2735186574aad`, executed published commit
`c381287384d41f243516a851dd6ded6f56b9fcad`.

The [manifest](manifest.json) binds fifteen byte-identical exports:

- [Report](report.json), [initial incomplete intent](intent.json), and
  [original action-journal bytes](actions.jsonl).
- [Separate execution decision](execution-decision.json) and
  [one-invocation receipt](invocation.json).
- [Authority/source/CI/journal/resource postcheck](provenance-postcheck.json),
  [combined scoped postcheck](postcheck.json), and
  [consumed approval disposition](approval-disposition.json).
- [Independent correctness review](correctness-review.json) and
  [independent defensive-security review](security-review.json).
- [Exact-revision protected PR CI verification](pr-ci-gate.json).
- The first approval's [expiry refusal](expired-approval-stop.json) and
  [expired-unused disposition](expired-approval-disposition.json).
- [Zero-finding scanner result](secret-scan.json) and
  [original-five scan receipt](secret-scan-summary.json).

The first approval expired before reservation or invocation. A fresh artifact
approval and subsequent execution decision preceded the single **15,186-ms** run.
All **21 calls, 60 counter fields, and 46 authority denials** pass. The three
capacity blocks isolate the shared group across two distinct runs/origins while
global capacity remains available. The complete Redis measurement spans 4,508 ms.

Both independent reviewers accept the scoped evidence. Six-role reachable-server
revocation, the original **28-action / 2,904-byte journal** and exact name/label
absence of four containers/two volumes pass. The renewed approval is consumed and
non-reusable. Gitleaks scanned five original-artifact copies (report, intent,
journal and both controller logs), **192,925 bytes**, with zero findings and no
added suppressions. Routine controller/scanner logs remain private and hash-bound.

The manifest/index are derived. Listed exports preserve original bytes and
historical classifications, including the initial incomplete intent. Copied
journals do not claim the original inode; that identity was checked on the
retained original before export. The reservation marker and approvals remain
private, linked by the provenance and disposition records.

Seven normalized public reply hashes are independently reconstructed; fourteen
reservation-bearing replies, private whole-state snapshots and unexported START
permissions rely on the pinned runtime's comparisons. Five retained stage
envelopes cover seven completions, with two journal-only `ready` completions.
Resource absence is by exact names/labels, not unavailable container IDs.

This is serial contention between logical owners sharing a ledger credential,
not simultaneous physical workers or per-owner credential isolation. Positive
cancellation and reversed order remain offline controls. Independent origin/global
saturation, actual network I/O, after-START durability and full M4 remain open.

See the [dated result](../../crawl-jobs-v2-m4-shared-group-run-2026-10-02.md) for
exact bindings, scope and private-original locations. The
[implementation plan](../../crawl-jobs-v2-plan.md) owns current publication status.
