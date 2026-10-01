# Shared group capacity: offline foundation — 2026-10-01

**Implemented and independently reviewed GO for the offline foundation only.**
`ledger-shared-group-capacity-v1` has a closed two-run fixture, complete expected
state oracle, proposed ledger/observer ACL projection and independent Go/canonical
Lua conformance. It is **not an executable controller case**. No shared-capacity
Redis run or image preparation occurred. Full M4 remains open.

## Merged baseline

The owner merged PR #17 at 2026-09-30 21:00:20 UTC as
`7da55b14b32c79ec4b2b95b0a9fd31dda40f0571`. Its tree
`a8234936f9fcaa3e773aa3692e11bcfa8a3e1bf2` exactly matches final PR head
`b7ef61928350f445846508dcf727a4fa41e697ea`, including the separately approved
Laravel/CommonMark/Axios security fixes. The prior real positive-interval case
remains bound to executed `7431599` and its consumed approval.

Local alignment to merged main preserved all 372 unrelated files and an empty
index. Main-push Required Checks `36776540750` and Unit Tests `36776540732` passed
all fourteen protected contexts. Downloaded reports match 478 compiled roots:
477 passed, only the explicitly optional native-parity skip. Logs confirm
150 harness/21 script tests with no Python skips. Main image evidence matches
71 files/all 18 recipes; its selected case remains claim/release.

Main CI gate: `ee9f70a199263e4a7037413786a49580e9e00c97e158a7ff0b585cdd1b964ef9`.
These results attest the merged baseline, not the new uncommitted oracle bytes.
The current implementation branch is `feature/crawl-jobs-v2-shared-group-capacity`.

## Closed next slice

Two distinct runs, jobs, owners and lease tokens share the same immutable group
lineage but have **different origins**. Both maps pin group/origin concurrency
1 and interval 0; the normative global tuple stays **2/0**. A held request in
one run therefore fills the shared group while global capacity remains available
and the other origin has no occupancy.

The compiler derives each run ID from the captured fixture identity and fixed
actor label. Source URLs, document/robots decisions, source digests, reservation
IDs and claim-transition identities are recomputed independently for each actor.
There are no caller-controlled URLs, groups, intervals or operation lists.
Both initial requests are synthetic robots intents; no grant performs network I/O.

The complete union has **90 possible positions**, **89 fixture-owned positions**,
**45 nonempty fixture setup entries**, two runs/jobs/reservations and four scopes.
BOOT owns the remaining position. Each REQUEST retains its canonical **57 KEYS**;
the combined inventory is not appended to the wire. Maintenance uses nine keys.

### Main 21-call trace

| Calls | Required result |
|---|---|
| 1–2 | A claims; exact claim replay |
| 3–4 | B's identical claim is group `CAPACITY_BLOCKED` twice while A is pending |
| 5 | Cancelling A with B's owner, retaining A's token/Q/keys, is `CRAWL_V2_IMMUTABLE_MISMATCH` |
| 6–7 | A starts; exact START replay |
| 8 | B is still group `CAPACITY_BLOCKED` while A is started |
| 9–10 | Cancelling started A is `CRAWL_V2_INVALID_STATE`; finishing A with B's token is `CRAWL_V2_IMMUTABLE_MISMATCH` |
| 11–13 | A finishes; B claims and replays the claim |
| 14–15 | Historical A FINISH/START replays preserve B's held slot |
| 16–20 | B starts/replays, finishes/replays, then replays historical START |
| 21 | Maintenance returns `BATCH_DONE`, processed 4, more 0 |

Classification: **six mutations, three capacity denials, three errors, eight
replays and one maintenance pass**. The exact denial tail is:

```text
[CAPACITY_BLOCKED, now_ms, shared_group_scope_id, 1, 1, 0]
```

Values are bulk strings. `after_io=0` is relative to blocked B's fence, including
after A starts. Both tuples are already equal, so denial must preserve the entire
database: no lease, reservation, missing-origin materialization, timestamp churn,
budget change or hidden tightening.

### Pending-cancellation sibling

The closed nine-call offline sibling is A claim → B blocked → A cancellation →
cancellation replay → B claim → A cancellation replay while B holds capacity →
B START → B FINISH → maintenance. Five calls mutate state; two are replays,
one is blocked and one is maintenance.

The canonical operation is **`CJ2_CANCEL_RESERVATION`**. Initial cancellation
and replay both return **`RESERVATION_CANCELLED`**; mutation classification is
separate. An early design suggestion used incorrect operation/status names;
the implementation was checked against the normative and canonical definitions
before its initial source freeze.

## State, bounds and authority checks

- Complete typed state and absolute expiry are compared after every operation.
  Errors have null reply times, with independent before/after observations.
- Global run registries, both active leases, four durable rate records and
  inventory timestamps are preserved as unions, rather than overwritten by B.
- Per-run claims/creations/starts/delivery counts stay independent. FINISH and
  cancellation do not end leases or refund cumulative creation/start charges.
- The first actual synthetic START sets the singleton first-start record; the
  peer cannot overwrite it. Robots-only traces leave document-history fields empty.
- Logical expiry remains the owning claim's 60-second deadline. Live reservations
  have persistent physical TTL; terminal hashes expire at terminal time plus one
  day, and replays extend neither deadline.
- Captured observations stay within a 300-second case envelope and 30-second
  measurement span. Delayed setup, exact 30-second equality, +1 ms rejection,
  malformed/overlong prefixes and omitted error timestamps have explicit controls.
- The proposed ledger ACL has exact 90-key selectors and narrow mutation key
  kinds; it denies authority writes and candidate/freeze content reads. Observer
  grants are read-only. Canonical peer reads do not grant peer mutation authority.
- Both actors are covered by the same proposed ledger role. This is not per-actor
  credential isolation. CLI vectors use fixed public synthetic material; real
  captured material remains private, and public summaries contain only safe counts
  and fingerprints.

## Verification and independent review

Initial source freeze: **105 files**, six changed paths from merged main.
Two nonblocking correctness recommendations were then implemented in test files
only: literal Go per-scope accounting/history expectations and a retained delayed
measurement-boundary test. The other **103 files stayed identical**. Both reviewers
checked the small delta and retained **GO/OFFLINE_ONLY / GO_OFFLINE_ONLY**, with
zero blocking findings and both recommendations closed.

| Check | Evidence |
|---|---|
| Full initial harness, ResourceWarnings as errors | **160 PASS**, 1,270,051 ms command wall time |
| Script tests | **21 PASS** |
| Final targeted shared-capacity Python tests | **11 PASS**, including the added boundary regression |
| Initial Go shared/cancellation/offline-artifact roots under race | PASS, 834,327 ms command wall time |
| Final strengthened Go roots under race | PASS, 848,724 ms; **81 canonical invocations across five profiles** |
| Final package vet; complete Lua and bundle pins | PASS |
| Current compiled Go inventory | **480 roots**; protected CI on these new bytes remains a later gate |

The main trace uses fresh spaced, same-millisecond and reversed-contender
profiles: 3 × 21 = 63 canonical calls. Cancellation uses fresh spaced and
same-millisecond profiles: 2 × 9 = 18 calls. Repeated initial/final runs do not
increase the unique conformance scope. Go independently derives policies,
identities, literal wires and key inventories, checks schemas, counters, scope
history and ACL trace predicates, and executes the unchanged canonical Lua.

The current Python discovery contains **161 tests**. The retained full run is the
initial 160-test suite; final validation re-ran the changed 11-test module. No new
full-161-suite invocation is claimed. Reviews likewise retain their original
test-outcome qualifications; the aggregate verification binds the final reruns.

| Artifact | SHA-256 |
|---|---|
| Initial inventory | `cf80849ea90d6d6eef4b8af8efbb4378b1cc6602526f4c3cfa6b93d68faf2bee` |
| Strengthened inventory | `13cdf67b113a2254e3a593947aa2420857a36b365c21b953c155b4554902c103` |
| Verification | `0b6ccedce6ba3d601526009d233ffa0aa66fefbae62f6a59164ee55a3f50fe0b` |
| Combined review decisions | `6ef794569921564a4766be298289780ac498a4769a7a12d946d98918b7c8c037` |
| Correctness delta GO | `77a413cf9a561ae402fd9cc7acd3e28c05ecf1a3d6531760ad5273e21d02f67e` |
| Security delta GO | `be51429f4858d25b4a6ade331aed9fb7e432110d65a98e4ed43b9c340b90b777` |

## Next gate and evidence limits

The offline scenario inventory is **21**, but the executable registry remains
**18**. Runtime selection/approval for this new case must reject. The existing
bounded Redis reader still accepts only 2/58/71 positions; executable 90-position
setup/observation, fixed dispatch, redacted per-run receipts and failure/cleanup
wiring remain the next implementation gate. Source review, appropriate images,
scoped publication, exact-revision CI, fresh artifact approval and a separate
execution decision must precede any real run.

This foundation proves in-memory serial canonical enforcement across two logical
run/owner sessions. It is not simultaneous independent-process contention, target
Redis/ACL/AOF evidence, global saturation, independent origin blocking, tightening,
after-I/O recovery, HTTP/permit integration, maximum-shape performance or full M4.
No normative protocol, canonical Lua or production Go behavior changed.

See the [exact evidence index](evidence/m4-shared-group-capacity-2026-10-01/README.md)
and [implementation plan](crawl-jobs-v2-plan.md). Changes are local and uncommitted.
Private originals, frozen sources, logs and independent review probes remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-shared-group-capacity-2026-10-01/`.
