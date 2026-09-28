# Recovery lifecycle implementation and review — 2026-09-27

**Corrected result: local checks PASS; independent correctness and defensive
security GO for image/CI preparation only.** The registered
`ledger-worker-death-pre-io-v1` case is implemented but has no real-Redis execution
result yet. Full M4 remains open.

## Scope and lifecycle

The new ledger case uses one run/job, the 58-position claim-fixture basis, the
unchanged 60,000 ms lease and 86,400,000 ms terminal reservation lifetime. Only
BOOT, CLAIM, RECOVER_EXPIRED, RENEW_LEASE and RELEASE_BEFORE_IO canonical sources
are loaded. Six role credentials remain separate; the ledger gains only an
additional HSET selector for the exact recovery-outcome map.

1. Bootstrap from an acknowledged probe/SIGKILL/restart and canonical BOOT.
   Install/verify the complete fixture; retire setup/loader/BOOT roles.
2. Worker A validates ready state, makes one CLAIM, emits its redacted receipt,
   then remains alive in the fixed parked path under the 30-second process timer.
3. The controller checks the acknowledgment, process-inventory binding and
   non-reaping child liveness; sends actual container SIGKILL; proves container
   identity, non-OOM exit 137 and PID zero, and reconciles attached-command exit.
4. Remove A, then create a differently named/identified worker B. Compare complete
   claimed state and require an early zero-recovery call before the lease deadline.
5. Use at most 40 short Redis-TIME stages and bounded controller waits until the
   original lease deadline is observed. No clock or TTL shortening occurs.
6. Recover ready once, prove unchanged replay, claim/replay B's new fence, reject
   stale A claim/release, verify stale renewal's counter-only exception, renew and
   release/replay B, then observe drained recovery and 46 authority denials.
7. Prove both workers quiescent before fresh-helper role revocation; destroy and
   inspect all five containers and both volumes. Reconcile the retained journal
   before evidence can pass.

Observed-stage schemas include 38 public counters, complete state hashes,
physical/logical reservation expiry, BOOT/run-ID binding and closed isolation
receipts. Worker A's full post-claim state is observed after its intended death;
the small pre-kill acknowledgment is not mislabeled as that state observation.

The case retains a 300-second maximum, 30-second individual stages and a separate
60-second cleanup budget. Approval must allow at least 180 seconds, and a further
remaining-budget guard runs before CLAIM. The timing origin is **host receipt of
the complete claim receipt**: at most one second to observed container stop,
followed by a separately bounded five-second attached-command reconciliation.
Both intervals are recorded; neither is a Lua latency measurement.

## Initial NO-GO and corrections

The original 89-file review scope is retained as
`c6a833939a33daa1bafe87b8f706927267e8f98f71ee8b994d82fccb2a770322`.
Both reviewers initially returned NO-GO and reproduced evidence-boundary defects
using local children, temporary files or fakes—not Docker/Redis executions.

| Finding | Corrected boundary |
|---|---|
| Exited command leader with descendant-held pipes could appear parked | `waitid(WEXITED\|WNOHANG\|WNOWAIT)` observes exit without reaping; Darwin uses a narrowly scoped SDK-layout libc fallback where Python lacks the wrapper |
| Journal prefix or final truncation could leave lifecycle PASS | Canonical append sequence, exact memory-bound prefix, device/inode identity, append readback and mandatory final retained-file reconciliation; the report binds hash/length/count/identity hash and export checks again |
| Post-handshake stderr was accepted during exit reconciliation | Any additional stderr rejects with a closed value-free error, retaining cleanup |
| Contradictory/extra isolation observations could be retained | Shared exact seven-field role-specific validator before normal, failed and parked retention, plus image-check validation |
| Reaped leader was conflated with proved session cleanup | Separate state; unresolved termination remains an error on repeated cleanup without new signal authority over a reaped identity |
| One-second wording exceeded the implementation's actual timing scope | Separate explicit container-stop and attached-command bounds with an accurate host-receipt origin |

No journal is silently repaired. Unsafe ownership/attachment prevents the affected
destruction; other safe cleanup continues and incomplete proof invalidates the
case. No observer privileges or draft AOF-extinction exception are implemented.

## Corrected verification and independent review

The corrected source freeze has **90 files**, **16 recipes**, **66 execution-image
inputs** and **18 offline scenarios**:
`b5fe64eee5fded5bd3a946d7a58790617d67c95a0447138ba54a79c709aeb888`.
Both reviewers independently checked all source/snapshot hashes and recipe
identities before/after their probes. Thirteen files changed from the initial review.

Corrected verification artifact:
`25b020b039ef9e96b28000e7f75ae6067262edaa497e1b1859e0cbba3696b6b5`.

| Check | Result |
|---|---|
| Full harness, ResourceWarnings treated as errors | 128 PASS; 1,302,239 ms command wall time |
| Script suite | 21 PASS; 12,628 ms command wall time |
| Go recovery + offline-artifact roots under race | PASS; 465,061 ms command wall time; 52 canonical recovery invocations |
| Package Go vet | PASS |
| Strict complete Lua assembly and bundle pins | PASS |

The Go vector export is split into two closed profile-specific packets, each
within the existing 2 MiB bound. A four-vector combined packet hit that bound;
the bound was not relaxed. The fake lifecycle advances a simulated clock and
cannot attest elapsed Redis time or actual worker death.

- Correctness reviewer: `ses_f2a72e458ffelqguj51148wBms` — **GO for preparation**.
- Defensive security reviewer: `ses_f26b21d03ffeou51ozKIElqzFy` — **GO for preparation**.
- Recorded decisions: `06110c785a327a8015a8b755226a65f6f04155c2d9dde8b42dfe74abb0ca0008`.

Independent delta probes closed the original counterexamples, including inherited
pipes after leader exit, journal truncation/rewrite/replacement/FIFO/hard-link
cases, poisoned isolation envelopes, stderr-only failures and persistent cleanup
uncertainty. Healthy/faulted fake recoveries retain `case_evidence_valid=false`.
Reviewers did not duplicate the full/race suites or run target infrastructure.

See the [exact review package](evidence/m4-recovery-review-2026-09-27/README.md).
Private originals/logs/probes are retained in the dated review workspaces named
there. Canonical Lua, the normative document and the observer/failure proposal
are unchanged.

## Publication and execution handoff

PR #14's earlier offline/readiness checkpoint `c09ff84` passed all 14 required
checks and merged on September 27 at 11:48:01 UTC as
`7617b86e236d14d38ad54a394beca601d7d94fd5`, with identical tree
`9b0defb8a6007c383e88c040cb6e697595cacea9`.
Its CI does not attest the new lifecycle delta. A fresh
`feature/crawl-jobs-v2-worker-recovery` branch starts from that merge; all 396
pending-file fingerprints/index/status survived the same-tree switch, including
372 unrelated files.

The new [arm64 image preparation](crawl-jobs-v2-m4-recovery-preparation-2026-09-27.md)
passes. New scoped publication/protected CI, exact artifact approval and a separate
execution decision remain required. No new real-Redis case or tracing experiment
has occurred; the old approvals are consumed. Prior evidence stays bound to its
original source/image identities and is not silently relabeled as new-harness
coverage.
