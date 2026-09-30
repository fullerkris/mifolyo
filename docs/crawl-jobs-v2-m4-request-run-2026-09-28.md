# Single request-lifecycle execution — 2026-09-28

**PASS; independent correctness and defensive-security reviews accept this
case's scoped evidence.** `ledger-request-lifecycle-v1` ran once on the approved
merged-main revision. All six role credentials were revoked, the original journal
reconciled, and all four containers/two volumes independently confirmed absent.
The approval is **consumed and non-reusable**. Full M4 remains open.

## Exact bindings and authority

| Item | Identity |
|---|---|
| Fixture | `1d8b4e394dffdada8518e189538449f4` |
| Executed merged-main commit | `561774f3c2e84bab5b4a32512420f0ddb9cb1710` |
| Tree | `392a1026f8b3d3130964db5965f1ff7e6c9da940` |
| Source review | `36e8aed8723ba5a90eb8233572caeb2d3cedd612d5cdd4893879e880693fac57` |
| Plan | `98259b8ebef77f3d060cbcfccbdd124c14a968b6aa68888dd895946ef7d92389` |
| Recipe | `b005236f8091e2064de50a39284b0eb1be5eb7cba955b56444a5d9b6a2ca5b4f` |
| Harness / stand-in | `sha256:66b9cadf929c2e28b6965addbe7d398539c05ec1149ec537633ad31cdcef6457` |
| Redis 7.4.11 | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Sealed artifact request | `f43035ae5cd456951f9f754a117eb7e685b2a4a91921bbb977638b6977f9f517` |
| One-use approval | `cce16a366477f08267b229fae4013ffadc0f80b7a8f70a4bacf6289150cc2eb4` |
| Separate execution decision | `3033bd461f13702612d25f4db4b5396236279d87f3947d4f28017418e034299f` |
| Post-merge main CI | `7a18a5f2e7714a93c9c459c4a8b739dd18a4cfbbb1f5f48e8a5a9ed09e3a3405` |

PR #16 merged at 17:50:25 UTC after an authorized, tree-identical conflict
resolution. Main-push CI passed all fourteen required checks. Downloaded race
reports match all 477 compiled roots: 476 pass, one explicitly optional native
parity skip. Logs confirm 140 harness and 21 script tests with no Python skips.
The amd64 CI preparation selects claim/release while verifying all 69 files/17
recipes; actual execution uses the separately checked arm64 request artifacts.

The owner approved the sealed artifact set, then separately selected **Execute
approved case**. Operator: `kfuller` via OpenCode. Approval window:
**20:10:30.199–21:10:30.199 UTC**. Source, CI, image, ownership and expiry checks
preceded the exclusive one-use reservation. Invocation:
**20:15:48.536–20:16:01.480 UTC**, **12,944 ms**, exit 0. No automatic retry occurred.

The original 300-second case, 30-second stage and separate 60-second cleanup
bounds were retained, as were network isolation, private Unix transport, memory
limits, six credential roles, 60-second lease and one-day terminal retention.

## Measured result

| Steps | Verified scope |
|---|---|
| REQ01–05 | Claim/replay; pending FINISH and second-active-intent errors; wrong-token START error |
| REQ06–11 | Robots START/replay, FINISH/replay, historical START and idle-scope integrity |
| REQ12–17 | Document reserve/replay, old FINISH while the new reservation is pending, document START/replay and old robots START |
| REQ18–22 | Wrong-token FINISH error, document FINISH/replay, historical START and final rate integrity |

All **22 ordered calls** match: six mutations, four expected errors, ten replays
and two read-only maintenance passes. Expected protocol replays are distinct from
the single controller invocation. Each error has `now_ms=null` and separately
captured before/after observations, rather than an invented reply timestamp.

Independent literal expectations match all **33 accounting fields**, including
**2,211 setup/before/after/delta values**. Final counters show:

- One claim, fence 1, two reservation creations and next ordinal 3.
- Two cumulative job/run/group starts, **one delivery attempt**, baseline 0.
- Zero pending/active-started run/group capacity and zero active/pending/started
  capacity in all three rate scopes.
- One open job, with no completion, cancellation, retry, recovery or output charge.

Sixteen nonmutating steps preserve their preceding complete-state fingerprint;
seven distinct fingerprints include setup and the six mutation results. The
reviewed producer performed complete private state/expiry comparisons, including
BOOT, expected key inventory and required absence. Reviewers checked retained
public projections and fingerprint relationships, not reconstructed private state.

## History, rate state and exact expiry

All values below are recorded Redis milliseconds:

| Event | Timestamp |
|---|---:|
| Claim | `1790626556171` |
| Robots START | `1790626556553` |
| Robots FINISH | `1790626556762` |
| Document reserve | `1790626557115` |
| Document START | `1790626557359` |
| Document FINISH | `1790626557717` |
| Logical lease/reservation expiry | `1790626616171` |
| Measurement end | `1790626559447` |

The complete measurement spans **3,436 ms**; operation brackets span 1,898 ms.
Successful replies fit their time brackets, and 56,724 ms of logical lease margin
remained at measurement end. These are lifecycle observations, not benchmarks.

The first-start timestamp/fingerprint remains the robots start. Document history
changes only at the document start, with fence 1, and remains stable afterward.
Global `next_allowed_ms` stays zero; group/origin deadlines retain
`1790626557359`, including after FINISH and maintenance. Effective intervals stay
zero and concurrency values stay 2/1/1.

Both reservation records retain logical expiry `1790626616171`. Their Redis key
expiries are persistent while pending/started, then become:

- Robots: **`1790712956762`**, exactly FINISH + 86,400,000 ms.
- Document: **`1790712957717`**, exactly FINISH + 86,400,000 ms.

Replays preserve those deadlines. This is an absolute-expiry observation, not an
elapsed one-day test. All **46 ordered authority-denial receipts** report `NOPERM`
and preserve the final state fingerprint.

## Teardown and independent review

Five retained stage envelopes satisfy their closed isolation predicates: init is
CHOWN-only UID 0; subsequent stages are UID 65534 with zero capabilities and zero
external routes. Their process count is two. The two ready-stage completions have
journal entries but no retained readiness envelopes.

Setup/loader/BOOT retire early. The final fresh revocation helper reports all six
reconnects denied while Redis is reachable; worker quiescence precedes that helper,
and revocation precedes Redis/volume destruction. Revoker-last follows the frozen
role iteration, not the order of a sorted JSON map.

The **28-action journal** exactly matches the report by canonical bytes, sequence,
hash and original file-identity hash. Eleven actions span cleanup from worker
quiescence. Independent read-only checks find all six exact resource names absent
and fixture/case label listings empty. Raw container IDs were not exported for
this case, so absence is not described as a by-ID check. The original evidence
secret scan has zero findings.

Both independent reviewers return **ACCEPT for this case's evidence**, with no
actionable finding. Their exact records and the postchecks are linked in the
[evidence index](evidence/m4-request-run-2026-09-28/README.md).

| Artifact | SHA-256 |
|---|---|
| Report | `af696dd242f7f9a0ca59b4b6dbfc41e1506b898644772a875561d616916eb7bc` |
| Runtime journal/resource audit | `fc66a549687e01a730994761068a4e065afc417bbb235ce4cd097cd44c54b7c0` |
| Correctness ACCEPT | `e25c7aad478981618deb5e522167133a02dbcc30d872075b34690e86f8bf8601` |
| Defensive-security ACCEPT | `dcc873c5714cadb550da97c18a66995ed8644150a11389ce9cbe970512c33dcc` |
| Final postcheck | `e2fdccfe33e54e833f97cf2f8b89c3ffe153dd26efb63372c6c85e3b8c921602` |
| Consumed disposition | `8156a76d3e0d8da0b02f3f77979d937d145a0ca3024ffffab2d40e5f71c4ce12` |

## Evidence boundaries and next work

Six public response projections—four errors and two maintenance replies—are
independently rehashed. The other sixteen replies contain discarded reservation
identities. **START `io_permission` is not publicly exported**: its exact value
and historical reply snapshots rely on the reviewed producer's private reply
comparison. No private identities or raw response values were reconstructed.
Synthetic unused STARTs do not establish network success or application permits.

Raw container/process inspections are opaque source-bound observations. Full
private state cannot be reconstructed from hashes. The preliminary persistence
probe precedes START and does not establish request-write crash durability.

This is one single-lease, zero-interval case. Positive-interval blocking, shared
concurrency, budget/creation-limit exhaustion, after-I/O recovery, broader
protocol/admin, crash/AOF/restore, maximum-shape and latency acceptance remain.
The [implementation plan](crawl-jobs-v2-plan.md) owns current status. Application
V2 remains dormant and full M4 is unaccepted.

These result exports/status updates are local and uncommitted after the executed
`561774f` checkpoint. Private originals, approval/decision records, review scripts
and checks remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-request-execution-2026-09-28/`.
