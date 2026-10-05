# Shared-group cancellation: local executable implementation — 2026-10-05

**Implemented and locally verified; independent source review is pending.**
`ledger-shared-group-cancellation-v1` is registered as the twentieth closed case.
It promotes the existing nine-call cancellation control into the bounded worker,
controller and actual failure-receipt parser. No real cancellation case has run;
fresh images, publication/CI and execution decisions remain later gates. Full M4
remains open.

## Baseline and source identity

Work is local on `feature/crawl-jobs-v2-shared-group-cancellation`, from merged
`e80d00538b11c46d021427bcfe1c408e16071c68`. PR #19's documentation/CI cleanup and
branch consolidation completed first. Exact merged-main CI passed all fourteen
required contexts, 480 race roots (479 passes/one permitted optional skip),
171 harness/21 script tests and 74-file/19-recipe image checks. That CI covers
the baseline, not these subsequent implementation bytes.

| Current local artifact | SHA-256 |
|---|---|
| Final 108-file source inventory | `2221812ca56635055b8acb6271b2fd0fbf27f8d603703171c54fc58bb14062d4` |
| Cancellation recipe | `b36d4bb7d2a4e657afbcda6a9cc3160b68678b19d590f7fb8c75b1f4de6d7a4f` |
| Verification and chronology | `2175c6e26ee0b2a36d60af08db66283bc31f2461e46990235c8a6610a71e6a2b` |
| Baseline main-CI gate | `4796388319f4e306786c92a5b4fda6dbd6b429c13ae9c97a023a44a91f01e9fc` |

The source delta is twelve paths: seven shared/runtime modules, three Python
test files and two Go test files. The runtime image path set remains **74 files**;
all changed runtime modules are already in its closed COPY inventory. Existing
builder inputs also cover the updated Go-vector helpers. There is no new runtime
module or expanded image-input allowlist.

The current local registry has **20 executable cases / 22 offline scenarios**.
All nineteen inherited recipe semantics match the baseline when source-file
hashes are excluded. Shared-module changes still alter recipe fingerprints, so
earlier images and real runs retain their original bindings. The normative
protocol, canonical Lua sources, production Go behavior and protocol pins are
unchanged.

## Fixed nine-call program

Both runs use distinct jobs/owners/tokens and origins, one shared group slot,
global/group/origin concurrency 2/1/1 and zero intervals. The combined fixture
retains 90 possible positions, 89 fixture-owned positions, 45 setup entries and
four rate scopes. Cancellation gets case-specific policy descriptors, run IDs
and group lineage; captured fixtures cannot be substituted across cases.

| Assertion | Operation and expectation |
|---|---|
| SGCANCEL01 | A CLAIM → `CLAIMED` |
| SGCANCEL02 | B CLAIM → exact shared-group `CAPACITY_BLOCKED`, `after_io=0` |
| SGCANCEL03 | A CANCEL → `RESERVATION_CANCELLED`; pending capacity released |
| SGCANCEL04 | Immediate A CANCEL replay → same status, no state change |
| SGCANCEL05 | B CLAIM → `CLAIMED`; consumes the freed slot |
| SGCANCEL06 | Historical A CANCEL replay → same status; B's reservation preserved |
| SGCANCEL07 | B START → `STARTED`; first-start history belongs to B |
| SGCANCEL08 | B FINISH → `FINISHED` |
| SGCANCEL09 | MAINTAIN → `BATCH_DONE`, processed 4, more 0 |

The nine calls contain **five mutations, one capacity denial, two replays and one
maintenance pass**, with zero expected RESP errors and one unused synthetic START
grant. Exact reply and complete typed-state comparisons distinguish initial
cancellation from its identically named replays.

All **60 counters** are projected before/after each call. A retains zero starts,
delivery attempts and document history; its reservation creation is not refunded.
Both jobs stay leased after reservation termination. B's later reservation and
first-start history cannot be cleared or overwritten by A's replay. Both leases
stay at claim + 60,000 ms; terminal keys retain cancellation/FINISH + 86,400,000 ms.
Forty-six ledger authority negatives follow the complete trace.

## Runtime and failure boundaries

The plan's closed case identity selects the sole trace. Requests accept no new
trace, profile, wire, endpoint or expected-state field. The old capacity case
continues to use its fixed 21-call finish program; reversed order stays offline.
Resume, probe/BOOT, fixture summaries, ACL rendering, measurement scope and
assertion prefixes are case-bound. The ACL builder explicitly checks that the
validated fixture belongs to the requested shared-group case.

Failure receipts retain only verified prefixes. Labels are `STATE`, the next
`SGCANCEL01`–`SGCANCEL09`, or `ACL` after nine complete rows. Ambiguous cancellation,
either replay, START or FINISH dispatches once. The actual worker serializer and
`Docker.stage` parser validate the case-bound failed envelope. Existing redaction,
worker-first/revoker-last teardown and six-role reachable-server revocation remain.

Bounds remain 300 seconds per case, 30 seconds per stage and complete observed
measurement, and separate 60-second cleanup. Four networkless containers, two
volumes, six roles and private Unix transport are unchanged. The reader retains
its existing size/type/transport limits.

## Verification chronology

| Check | Observed result |
|---|---|
| Initial focused cancellation lifecycle | 12 tests PASS, ResourceWarnings as errors |
| Independent Go/canonical Lua under race | Three roots PASS; 63 finish-profile and 18 cancellation-profile invocations, 81 total |
| Full Python harness before final ACL guard | 183 tests PASS, zero skips, ResourceWarnings as errors; 1,839,533 ms command time |
| Script suite | 21 tests PASS |
| Vet and strict digest/Lua/bundle checks | PASS; all 43 canonical sources and pins verified |
| Final shared-family regression after ACL guard | 33 tests PASS, zero skips, ResourceWarnings as errors; 630,908 ms command time |

The final self-review added one case-id/fixture guard to the ACL builder and two
cross-case substitution assertions. Those are the only two changed files between
the full-harness snapshot and final source inventory. The final 33-test run covers
all three affected shared-group test modules. **A second full 183-test run after that guard
is not claimed.** The verification record preserves each command, original log
hash, source snapshot and the two-file delta.

Twelve new methods cover eighteen state/reply/ACL fault profiles, five ambiguous
operation profiles, all ten failure-prefix boundaries, exact measurement limits,
cross-case fixture/ACL/receipt substitution, zero-start and peer preservation,
partial setup, interrupts, and failed revocation. These facade tests are
simulations and retain `case_evidence_valid=false`. Go tests execute canonical Lua
in the existing in-memory facade; they are not target-Redis acceptance evidence.

## Next gate

Independent correctness/security source review is **pending**. After that gate,
prepare fresh immutable images, publish the reviewed source, verify exact-revision
CI, and obtain fresh exact-artifact approval plus a separate execution decision
before one real case. No prior consumed approval is reusable.

The [evidence index](evidence/m4-shared-group-cancellation-2026-10-05/README.md)
links the exact source/verification/recipe exports. Private command logs and
check snapshots remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-shared-group-cancellation-2026-10-05/`.
The [implementation plan](crawl-jobs-v2-plan.md) owns current status.
