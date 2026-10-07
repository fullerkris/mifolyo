# Cancellation run and scoped acceptance — 2026-10-06

**PASS; independent scoped evidence reviews accepted, finalized October 7.**
Fixture `cda9d3372d72a70b1bf3349b6ee2462f` ran once on `22317dc`, in 14,130 ms.
The renewed approval is consumed/non-reusable; the earlier approval was refused
before reservation/invocation and remains closed-unused.

The [manifest](manifest.json) binds twenty-one byte-identical exports:

- Original [report](report.json), [intent](intent.json) and [action journal](actions.jsonl).
- [Invocation](invocation.json), [separate execution decision](execution-decision.json)
  and [one-use reservation](attempt-reserved.json).
- [Renewed request](renewed-request.json), [owner artifact decision](renewed-owner-decision.json)
  and [renewed approval](renewed-approval.json).
- [Primary postcheck](postcheck.json) and [resumed preservation/absence check](resume-postcheck.json).
- Independent [correctness](correctness-review.json) and [security](security-review.json)
  reviews, plus [scoped reconciliation](evidence-review.json).
- [Original-output scan](secret-scan.json) and [scan summary](secret-scan-summary.json).
- Earlier [execution request](old-execution-decision.json), [window refusal](old-window-refusal.json)
  and [closed-unused disposition](old-approval-disposition.json).
- [Consumed renewed-approval disposition](approval-disposition.json).
- [Exact published-revision CI gate](pr-ci-gate.json).

All nine calls, 60 counter fields/1,680 independent integer comparisons and
46 denials pass. Six-role revocation, original 28-action journal identity and six
exact-name/six-label absence checks pass. Export copies retain journal bytes,
not its original inode; original evidence remains private. Correctness's interrupted
draft/probes were preserved and independently reverified before the final seal.

Only two normalized response hashes are publicly reconstructible. Seven
private-bearing replies and raw whole-state/membership comparisons remain
pinned-runtime attestations. Full M4, performance, elapsed aging, concurrent
workers and all-state AOF durability are not established.

See the [run report](../../crawl-jobs-v2-m4-cancellation-run-2026-10-06.md) and
[implementation plan](../../crawl-jobs-v2-plan.md). The manifest/index are derived;
all twenty-one selected records preserve original bytes. Private originals and
reviews remain under `m4-cancellation-execution-renewal-2026-10-06/` and
`m4-cancellation-renewal-2026-10-06/` in the approved temporary workspace.
