# Shared group capacity: executable lifecycle source gate — 2026-10-01

**Implemented; independent correctness and security GO for image/CI preparation.**
`ledger-shared-group-capacity-v1` is now registered as the nineteenth closed
controller case. Its bounded reader, setup, fixed measurement, safe receipts and
failure/cleanup routing are implemented and simulation-tested. **No shared case
has run on a real Redis server, and no new image has been prepared.** Full M4
remains open.

The [offline foundation](crawl-jobs-v2-m4-shared-group-capacity-2026-10-01.md) and
its immutable evidence retain their original non-executable classification.
This later checkpoint extends that source on
`feature/crawl-jobs-v2-shared-group-capacity`, based on merged
`7da55b14b32c79ec4b2b95b0a9fd31dda40f0571`.

## Fixed runtime scope

Two independent run/job/owner/token identities share one group lineage but use
different origins. Global concurrency/interval stays **2/0**, and group/origin
tuples stay **1/0**. The real-case path admits only the reviewed **21-call finish
trace**: six mutations, three capacity denials, three expected errors, eight
replays and one four-scope maintenance pass. Forty-six direct authority denials
follow. Positive cancellation and reversed-contender profiles remain offline
controls; runtime requests accept no trace/profile selector.

| Binding | Closed value |
|---|---|
| Possible state positions | 90: 89 fixture-owned plus BOOT-owned durability |
| Nonempty setup entries | 45 |
| Runs / jobs / request reservations | 2 / 2 / 2 |
| Distinct scopes | Shared global/group, origin A, origin B |
| Credential roles | setup, loader, boot, ledger, observer, revoker |
| Containers / volumes | init, executor, Redis, revocation / 2 disposable volumes |
| Time limits | 300-second case, 30-second stages and measurement, separate 60-second cleanup |
| Lease / terminal retention | Original unrenewed 60-second leases / absolute one-day expiries |
| Memory | init 128 MiB; executor/revocation 256 MiB; Redis 528 MiB, maxmemory 400 MiB |
| Transport / I/O | Private Unix socket, network none; synthetic unused grants |

Each REQUEST still uses its canonical 57-key layout; the 90-position union is
the observer/setup inventory. The same ledger credential serves both logical
owners. This is neither per-owner credential isolation nor simultaneous
independent-process contention.

## Reader, setup and receipts

`bounded_state.py` adds only the closed **90-position** shape to its existing
2/58/71 allowlist. Existing scan-iteration, key, value, hash-field, collection and
RESP bounds remain. Complete DB membership, required absence, types, exact fields,
members/scores and absolute `PEXPIRETIME` are checked; BOOT remains separately owned.

`shared_executor.py` binds the preliminary probe/restart, observed setup time,
BOOT/replay and exact private fixture. It validates empty pre-setup state, writes
45 fixed entries, compares complete state and retires setup/loader/BOOT. Time-1000
ACL construction is checked against observed-time construction for identical
key/command authority.

Runtime ACL assembly adds the four short-lived roles around the reviewed
ledger/observer projection. Setup cannot write BOOT; ledger writes remain limited
by key kind and exclude authority records; observers are read-only. The loader
receives only the case's six canonical sources. Exact peer reads are necessary
for shared-scope validation and do not authorize canonical peer mutations.

Receipts contain **60 distinct counters**:

- 24 run/job/per-run-group fields for each actor: 48 total.
- Three counters for each of the four scopes: 12 total, counting shared scopes once.

Additional safe projections bind labeled job state/lease history, reservation
expiry/reference hashes, first-start history, rate tuples and capacity-denial
kind/count/limit/after-I/O fields. Raw owners, tokens, reservation IDs, run/job IDs,
transition IDs, URLs and origin witnesses are excluded and checked before
controller retention. Runtime private replies and full snapshots are compared
inside the reviewed executor; exported hashes do not reconstruct those bytes.

## Failure and cleanup boundaries

Every canonical operation is dispatched once. Unexpected errors, wrong blockers,
wrong `after_io`, state/TTL/history changes and ambiguous transport stop the case.
Only fully validated rows are retained. Failure location must match the completed
prefix: `STATE`, the next `SGC01`–`SGC21`, or `ACL` after all 21 rows. Full success
requires all 46 denials and the complete measurement within 30 seconds.

Both actual `executor.main` failure serialization and `Docker.stage` parsing route
`SharedFailure` through strict receipt validation. Partial setup, interruptions,
failed revocation and deadline failures retain fail-closed controller behavior.
Worker stop/wait/removal precedes the fresh revocation helper; revoker-last and
the separately bounded cleanup remain inherited. Simulations always retain
`case_evidence_valid=false`.

## Verification and review

The lifecycle delta is **13 source/test/build paths** from the offline foundation.
Its frozen inventory contains **107 files**. Both independent reviewers returned
**GO_FOR_IMAGE_CI_PREPARATION**, with no actionable findings and source hashes
unchanged. They inspected actual worker/adapter failure paths, role closure,
reader/receipt/clock bounds and COPY-restricted import closure. Correctness checked
23 malformed/time/state/receipt controls plus additional lifecycle faults;
security recorded 86 targeted in-memory probes. These are not target Redis results.

| Check | Result |
|---|---|
| Full harness, ResourceWarnings as errors | **171 PASS**, 1,497,728 ms command wall time |
| Script tests | **21 PASS**, 9,034 ms |
| Shared capacity/cancellation/offline-artifact Go roots under race | PASS, 822,676 ms; **81 canonical Lua invocations across five profiles** |
| Package vet | PASS |
| Strict complete 43-source Lua assembly and bundle pins | PASS |
| New lifecycle module | Ten methods, including 15 fault profiles, all 22 prefix boundaries, actual worker/adapter serialization and 90-position reader controls |

Existing cases are included in the full harness. Review also compared the 18
inherited semantic recipes, apart from source-file fingerprints, and unchanged
lifecycle/authority functions. The registry now has **19 cases / 21 scenarios**;
the execution-image source allowlist has **74 files**. New modules are included
in both execution-image and Spider-builder inputs. No normative protocol or
canonical Lua changed.

| Artifact | SHA-256 |
|---|---|
| Source inventory | `373f143179c558781922dedd2a90e9f65b461532610ca04e659fdcbe3fdea9d4` |
| Recipe | `05bfdbbea0bf5166b332e8e68225d466dcd1b076bf171fca30d062b946d88dc4` |
| Verification | `ad5e032df2aef45a59026e847f8732f35b9e165d79c62c4cd8e3c092f50bc6cf` |
| Combined review decisions | `e917642a301c3c4a4ceb40fa47034a20041bd35b150440346e6a94cf69e85594` |
| Correctness GO | `dce909d4527be7287aff9ffe98c944660258b37f5ad59df7aaedade9d3bba38a` |
| Security GO | `3bed667bd100019417bd506d99199b17676acb3a50dbaaa159bb15c8333addda` |

## Next gate

Prepare immutable images explicitly selecting this case, verify stopped-role
admission and owned-resource cleanup, then complete scoped publication and
exact-revision protected CI. Fresh artifact approval and a separate execution
decision must precede one bounded real invocation. Prior case approvals remain
consumed and old image/CI receipts do not attest these changed source bytes.

Global saturation, independent origin blocking, policy tightening, after-I/O
recovery, crash/AOF/restore, maximum-shape performance and full M4 remain open.
The [implementation plan](crawl-jobs-v2-plan.md) owns current status.

The [exact review/check exports](evidence/m4-shared-group-lifecycle-2026-10-01/README.md)
and source changes are local and uncommitted. Private originals, logs and frozen
sources remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-shared-group-lifecycle-2026-10-01/`.
