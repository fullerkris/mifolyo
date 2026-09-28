# Bounded request lifecycle checkpoint — 2026-09-28

**Implemented; corrected independent correctness/security GO for image/CI
preparation; selected arm64 preparation PASS.** New case
`ledger-request-lifecycle-v1` provides a single-lease robots/document lifecycle.
The [implementation plan](crawl-jobs-v2-plan.md) owns current status.

## Closed case

- One run/job/lease, two distinct reservations on the same owner/token/fence.
- **22 calls:** six mutations, four expected RESP errors, ten exact replays and
  two read-only `CJ2_MAINTAIN_RATE_SCOPES` integrity passes.
- Sources: BOOT, TRY_CLAIM, RESERVE_REQUEST, START_REQUEST, FINISH_REQUEST and
  MAINTAIN_RATE_SCOPES. REQUEST has exactly 57 wire keys; the complete fixture
  has 58 positions, including BOOT-owned durability.
- Two synthetic recorded request starts, **one** delivery attempt and baseline
  zero. The START grants are unused; no external request or output dispatch exists.
- **33 accounting counters**, full typed state/absolute-expiry comparisons,
  first-start/document history, rate-state projections and 46 authority denials.
- Original 60-second lease, one-day tombstones, 300-second case, 30-second stage
  and separate 60-second cleanup limits. Four container roles, two volumes,
  six credential roles, worker-first teardown and revoker last.

| Steps | Assertions |
|---|---|
| REQ01–05 | Claim/replay; reject premature FINISH, second active intent and wrong-token START |
| REQ06–11 | Robots START/replay, FINISH/replay, historical START without I/O permission, idle-scope integrity |
| REQ12–17 | Document reserve/replay, old FINISH leaves new pending reservation intact, document START/replay, old robots START retains its original snapshots |
| REQ18–22 | Wrong-token document FINISH rejects; FINISH/replay and historical START preserve history/expiry; final idle scopes remain intact |

The new fixture replaces the unused second-fence robots reservation position with
the same-lease document reservation. Initial state is reconstructed from closed
inputs; runtime snapshots retain the complete database inventory, required absence,
types, field cardinalities, members/scores and absolute expiry comparison.
Error receipts carry `now_ms=null`, with separate Redis-time brackets.

Rate intervals are zero. Group/origin deadlines retain their latest start time;
the global zero-interval deadline remains zero. FINISH refunds active capacity
without refunding cumulative starts or refreshing terminal tombstones. The first
start record stays immutable, while document START establishes its document witness.

## Independent source review and corrections

Initial 96-file inventory:
`d6284b4d72ded5ed25ac6be5d9abce5d5c4aa73003e0a8822e2f2b5292d1bced`.
The initial suites passed, but both independent reviews returned NEEDS_WORK:

1. **Medium:** production `Docker.stage()` did not dispatch request-case failed
   envelopes, discarding a verified prefix while still failing closed.
2. **Low:** an allowed failure label could contradict the completed prefix.

Both findings are closed. The actual adapter now validates and propagates these
receipts. Failure labels require STATE before rows, the exact next REQ number,
or ACL after all 22 rows. Serialized-adapter regressions retain five rows for an
ambiguous START and eighteen for an ambiguous FINISH, without retries; teardown
and redaction remain intact. Typed/contradictory labels reject.

Corrected inventory:
`36e8aed8723ba5a90eb8233572caeb2d3cedd612d5cdd4893879e880693fac57`.
Only `controller.py`, `request_executor.py` and `test_request_execution.py` changed
between freezes. Correctness and defensive-security re-reviews independently
verified all 96 working/snapshot hashes and returned **GO for image/CI preparation**.
Initial snapshots and findings remain preserved.

## Corrected verification

| Check | Result |
|---|---|
| Python harness, ResourceWarnings treated as errors | **140 PASS**, 1,048,302 ms command wall time |
| Script tests | **21 PASS**, 9,418 ms |
| Four focused Go roots under race | PASS, 844,561 ms; **44 new request + 70 prior claim/recovery canonical invocations** |
| Go vet | PASS |
| Strict complete Lua assembly and bundle pins | PASS; canonical Lua/protocol unchanged |

The two request vector profiles cover spaced and equal-millisecond observations.
Go independently derives targets, decisions, source/group/reservation/transition
identities and literal semantic wires before executing embedded canonical Lua.
Full state/expiry, first-start schema, start-generation transitions, no-write
replays/errors/maintenance and actual command-trace selector feasibility are checked.
Python lifecycle facades remain simulations and cannot claim real acceptance.

Verification record: `db54b66d7d030af686feff9a505b1430547adea5249f0ffdbf57830f9f25e62e`.
Independent review record: `92e82586e77bb572177dda88a3a7b09600a84e4f1e80647aa7abebcaef128746`.

## Immutable arm64 preparation

| Artifact | Identity |
|---|---|
| Harness / stand-in image | `sha256:66b9cadf929c2e28b6965addbe7d398539c05ec1149ec537633ad31cdcef6457` |
| Redis 7.4.11 image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan | `98259b8ebef77f3d060cbcfccbdd124c14a968b6aa68888dd895946ef7d92389` |
| Recipe | `b005236f8091e2064de50a39284b0eb1be5eb7cba955b56444a5d9b6a2ca5b4f` |
| Image report | `ed5af7bd532c6fa7b74e7a7907c6c9bb5a687a6625a5a5361544c5e069f360a0` |
| Independent postcheck | `d8d1c1aacb92818bf6319b2ff21b285e8b06c6cb8e0c3f7b8ef55b6de41751aa` |

Build used `--network none --pull=false`, Linux/arm64 and Python 3.13.15 platform
manifest `sha256:ad4c34ff79289506e235b40dce75d629e25f226b597a2455804220f037e07531`.
Its local base ID is `sha256:adc3d531c29fbd69fa9ca49cd8e241c68aaf4e1dd7462be6c2432044b7a39ea8`.

Selected-case preparation admits init, executor, Redis and revocation roles while
stopped. Metadata fixture `f02ce82de0eace19b48e813169b43fa4` started no containers;
all four exact names and both volumes were independently found absent afterward.
Separate networkless Python checks verify **69 files/all 17 recipes**, source
identities and isolation. Init/executor peaks were **48,898,048 / 48,955,392 bytes**.
These are preparation observations, not maximum-shape measurements. Redis ran only
`--version`; no request acceptance server or case was started.

## Remaining gates

The implementation and prior recovery-run documentation remain local and
uncommitted on the branch based at `c009282`. Next: scoped publication, protected
exact-revision CI, fresh artifact approval and a separate execution decision.
Old CI, images and consumed approvals do not bind this changed case.

The zero-interval, single-job sequence does not demonstrate positive-interval
blocking, shared concurrency across workers/runs, budget or creation-limit
exhaustion, after-I/O recovery, actual network permits, request-write durability,
internal crash cuts or maximum-shape latency. The final job remains leased with
no active reservation; no inappropriate pre-I/O release is appended after START.
Full M4 remains open and application V2 remains dormant.

See the [exact evidence index](evidence/m4-request-lifecycle-2026-09-28/README.md).
Private originals, logs, review snapshots and checkers are retained under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-request-lifecycle-2026-09-28/`.
