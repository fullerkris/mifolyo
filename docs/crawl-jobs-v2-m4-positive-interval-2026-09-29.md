# Positive-interval rate admission checkpoint — 2026-09-29

**Implemented; independent correctness/security GO for image/CI preparation;
selected arm64 preparation PASS.** Case `ledger-positive-interval-v1` has not
run on an acceptance Redis server. The [implementation plan](crawl-jobs-v2-plan.md)
owns current status and the remaining full-M4 work.

## Closed scope

One fresh run/job/lease contains robots then document reservations. The submitted
group/origin tuples must match under normative section 8.5, so both intervals are
fixed at **8,000 ms**, global interval zero, concurrency 2/1/1. No configurable
interval or additional mutation authority is accepted. All policy, group,
decision, source/job and reservation/transition identities are consistently bound.

The 58-position fixture and complete typed/absolute-expiry reader are retained.
The 24 fixed calls cover six mutations, four expected RESP errors, eleven exact
replays, one group-first rate denial and two read-only integrity passes:

| Calls | Required scope |
|---|---|
| RATE01–05 | Claim/replay; pending FINISH, competing intent and wrong-token START reject |
| RATE06–11 | Robots START/replay, FINISH/replay, historical START and idle-scope integrity |
| RATE12 | Document RESERVE before the deadline returns `RATE_BLOCKED` with exact group scope, `robots START + 8000` and `after_io=1`; complete state is unchanged |
| Observer wait | Same Redis run, at most twenty read-only TIME observations, no ledger credential, sleeps ≤2 seconds |
| RATE13–19 | After verified deadline crossing: RESERVE/replay, historical FINISH, document START, started-reservation RESERVE/START replays before the new deadline, old robots START |
| RATE20–24 | Wrong-token FINISH rejects; document FINISH/replay, historical START, final rate integrity |

`after_io=1` describes a recorded START on this fence. The grants are synthetic
and unused; no external fetch or other network I/O is dispatched. Group precedence
means this fresh equal-deadline case does not independently demonstrate origin
blocking or overlapping-worker concurrency.

## Time, state and failure boundaries

- The deadline is derived from the validated Redis START reply, not host time.
- `rate_before` retains twelve verified rows. `rate_clock` has observer-only
  credentials and binds the run, fixture, complete prefix, deadline and lease.
- `rate_after` binds the prefix and wait, verifies unchanged full state across
  the wait, and requires its dispatch observation to follow the final TIME sample.
- The original 60-second lease, one-day terminal expiry, 300-second case maximum,
  30-second stages and separate 60-second cleanup remain. The entire observed
  measurement also fits 30 seconds. Approval minimum is 120 seconds; a 60-second
  remaining-case guard precedes the prefix.
- Missed blocking/replay windows, wrong clocks, incomplete waits, ambiguous
  replies, unexpected state and malformed receipts invalidate the case. No
  automatic mutation retry or relabeling of a missed window is allowed.
- Normal/failure output remains closed and redacted. Valid failure prefixes pass
  through the actual adapter parser; worker-first teardown and revoker-last remain.

The public projection retains 33 accounting counters, first/document history,
absolute reservation expiries and rate tuples. RATE12 additionally exports a safe
blocker-kind/deadline/after-I/O projection. Positive intervals do not cause FINISH,
replay or maintenance to refund cumulative starts or shorten deadlines.

## Independent review and checks

Frozen inventory: **101 files**, 18 changed paths from the request checkpoint.

`c4c97b7672e8cd813ca17e574d4ec5bfbb865c8752e77f6d5bcd593e378ed33b`

Both reviewers returned **GO for image/CI preparation**, with no actionable
correctness finding or blocking security finding. Hash checks verified working
and frozen source before/after. Independent probes cover policy/framing and
identities, all cross-case phase refusals, forged clocks/waits, failure-prefix
boundaries, actual adapter serialization, expired-budget cleanup and old-case
behavior. These are offline checks, not real Redis observations.

| Check | Result |
|---|---|
| Full harness with ResourceWarnings as errors | **150 PASS**, 1,721,205 ms command wall time |
| Script tests | **21 PASS**, 13,919 ms command wall time |
| Positive-rate Go root under race | PASS, 644,097 ms; **60 canonical invocations** |
| Prior four Go conformance roots under race | All PASS in two bounded batches, 446,979 / 717,623 ms; **114 canonical invocations** |
| Vet, strict complete Lua assembly and bundle pins | PASS |

The initial combined prior-root race command **timed out after 900 seconds**.
Its failed log remains retained. Splitting the same roots into separately bounded
commands completed coverage without increasing their timeout, dropping assertions
or removing race instrumentation. A reviewer's first focused batch also timed out;
its completed and remaining-method results are separately classified in the review
record rather than relabeled as an initial aggregate pass.

The Go positive profiles use fresh identical prefixes: **deadline−1 rejects;
deadline and deadline+1 admit**. They construct policy descriptors, identities,
literal keys/arguments and denial tails independently before running embedded
canonical Lua and full-state/schema/expiry/ACL-trace checks. Those virtual-clock
cuts do not claim observed live millisecond equality.

Verification record: `5b7343d9c4e0b57df1a48b4b572de214839b0623e5d04bf905c613a637d25301`.
Review decisions: `e9a60d0effe44b8f2ac602ca52d45ed4fe84a274d80514231461eafff71d2e36`.

## Immutable arm64 preparation

| Artifact | Identity |
|---|---|
| Harness / stand-in | `sha256:2d74e377e77eefddef7cb179d8f0f388650ae02f090f13ca5cf9ab91593f07fc` |
| Redis 7.4.11 | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan | `ca4eafd255a559907c89a2ccc7b3e70d101370415577440c47702c08029cee1a` |
| Recipe | `7ddf11563a526574d12dcea0f97ba1c611f1cf49efbfa6774d9e6434f31381d8` |
| Image validation | `5894051206e7295fafd096f65efcd2b8b2a2316dae3bd8f8a2635ddff4288e06` |
| Independent postcheck | `b32087c1a2abe840323a7152f58812b91954939f725795545b8ac6782e522ca7` |

Build used `--network none --pull=false`, Linux/arm64 and pinned Python 3.13.15
platform manifest `sha256:ad4c34ff79289506e235b40dce75d629e25f226b597a2455804220f037e07531`.
The local base ID is `sha256:adc3d531c29fbd69fa9ca49cd8e241c68aaf4e1dd7462be6c2432044b7a39ea8`.

All **71 image files and 18 recipes** match. Four metadata-role containers were
admitted while stopped. All four names and two volumes belonging to fixture
`8dd65a0b5ecfdb52d4f81c38431bb174` were independently absent after cleanup.
Init/executor image-check peaks were **48,328,704 / 48,459,776 bytes**. These are
image checks, not maximum-shape workload measurements. Redis ran only `--version`;
no positive-rate acceptance server or case was started.

## Next gate and limits

The source/preparation and retained request-run evidence are local and uncommitted
on the `561774f` base. Next: scoped publication, protected exact-revision CI, fresh
artifact approval and a separate execution decision. Historical approvals are
consumed and earlier CI does not attest these changed source bytes.

Independent origin-block windows, shared concurrency, policy tightening, budgets,
after-I/O recovery, crash durability, maximum-shape performance and full M4 remain
open. The normative protocol, protocol constants and canonical Lua remain unchanged.
No application V2 activation or external request is authorized by preparation.

See the [exact evidence index](evidence/m4-positive-interval-2026-09-29/README.md).
Private originals, logs and snapshots are retained under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-positive-interval-2026-09-29/`.
