# Shared-group cancellation: independent source review — 2026-10-05

**Correctness GO and security GO for image/CI preparation.** Both independent
reviewers accepted the fixed source of `ledger-shared-group-cancellation-v1`,
with no actionable findings or blockers. The 108-file source inventory and
recipe remained unchanged before and after review. No source correction was
needed during this review round.

This is a source gate, not real-Redis acceptance. New immutable image preparation,
scoped publication/exact-revision CI, and separate artifact/execution decisions
remain ahead. No real cancellation case has run; full M4 remains open.

## Exact reviewed snapshot

| Binding | Identity |
|---|---|
| Base commit | `e80d00538b11c46d021427bcfe1c408e16071c68` |
| Source inventory, 108 files | `2221812ca56635055b8acb6271b2fd0fbf27f8d603703171c54fc58bb14062d4` |
| Recipe | `b36d4bb7d2a4e657afbcda6a9cc3160b68678b19d590f7fb8c75b1f4de6d7a4f` |
| Correctness review | `7f5f119ff50c827a6cb0bb68d123276c52cce19729f6b786a89a1b18fea6e0b9` |
| Security review | `867ecba4ee8df573560bfdfbd44bcc9538534d5411fb1290b00fb5595559f13a` |
| Combined source-review decisions | `80606aaeed151b2713764a0f3a3cb89069269e0ca2e76996817cdac665b3723e` |

Reviewers used an isolated, filesystem-read-only copy of the tracked base plus
the exact 23-file implementation/documentation overlay. All 108 inventory hashes
were checked before and after. The security review also verified the unchanged
1,452-file snapshot. The original working source and all 372 unrelated pending
files were preserved. Parallel D01/D02 research used a different worktree and did
not alter the cancellation source or normative protocol.

## Correctness review

The review covers the nine-call closed trace, case-specific identity and recipe,
two-run inventory, exact cancellation/replay state, counters, first-start history,
lease/tombstone expiry, and success/failure receipt reconstruction.

Independent final-byte checks include:

- All 33 shared-family Python tests.
- Go race verification of 18 cancellation canonical-Lua calls and all 22 offline
  artifact scenarios; vet passed.
- 2,175 state corruptions, 672 receipt mutations and 60 invalid failure labels.
- All nine ambiguous dispatch boundaries and seven transport-fault controls.

These are offline/facade checks. Exact raw-response and whole-state equality
remain the pinned runtime's responsibility during a later real case. The review
record retains command/log hashes, interrupted exploratory attempts, successful
replacement checks and the limits of each evidence class.

## Security review

All 88 independent final-byte security checks passed. The review covers:

- Cross-case plan/recipe/fixture/ACL binding and rejection of caller selectors.
- Exact key-kind grants, setup/BOOT separation and read-only observer authority.
- Bounded success/failure envelopes, private-value redaction and exact prefixes.
- Single dispatch after ambiguous operations and worker-first/revoker-last cleanup.

The existing scan match was independently confirmed as the public canonical
RETIRE Lua checksum, not a credential. No privilege expansion, new protocol
exception, tracing, server execution or image acceptance is implied.

## Verification chronology and next gate

The [initial local checkpoint](crawl-jobs-v2-m4-shared-group-cancellation-2026-10-05.md)
records the full 183-test harness, 21 scripts, 81 canonical race invocations and
pins/vet before the final one-line ACL case-id guard and its two cross-case
assertions. The original final 33-test run and the independent final-byte checks
above follow that guard. **No final-byte full-183 rerun is claimed.** Initial
inventory/verification records keep their original pending-review fields; the
new [review evidence](evidence/m4-shared-group-cancellation-review-2026-10-05/README.md)
records the subsequent GO decisions.

Next: prepare and independently verify new selected-case images/artifacts on this
exact reviewed source, then scoped publication and protected CI. Current registry
is 20 cases/22 scenarios with the same 74 image paths. Source fingerprints changed
from earlier accepted cases, so their old images/approvals cannot be reused.

The [implementation plan](crawl-jobs-v2-plan.md) owns mutable status. Private
snapshot, probes and logs remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-parallel-review-2026-10-05/`.
