# M4 bounded claim/release case — 2026-09-23

**Step 6 complete: PASS for `ledger-claim-release-v1`; all six disposable
resources independently confirmed absent.** One owner-approved attempt exercised
two pre-I/O claim/release cycles on real Redis. Full M4 remains open
(`m4_accepted=false`). The [implementation plan](crawl-jobs-v2-plan.md) owns the
remaining coverage and next gate.

## Authorization and exact revision

After [Step 5 publication and protected CI](crawl-jobs-v2-m4-claim-ci-2026-09-23.md),
the owner selected **Approve exact case**, then separately **Execute approved
case**. Approval and its single-use reservation were recorded privately before
the controller invocation. There was one attempt and no automatic retry.

| Item | Executed identity |
|---|---|
| Reviewed/executed commit | `b4bda19f07bb22f37737508cd10424b75690d6f5` |
| Branch / PR | `feature/crawl-jobs-v2-claim-release` / [PR #11](https://github.com/fullerkris/mifolyo/pull/11), draft and unmerged at execution |
| Fixture | `f59af8adfe82573b64b8dfda427a0b00` |
| Case/operator/platform | `ledger-claim-release-v1` / `fullerkris` / Linux/arm64 |
| Harness image | `sha256:b8de7cf09495bca22f6bba158bfdbe776b65ab726895e484682a3bb4f95a8a01` |
| Redis 7.4.11 image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan | `9833e6c25c74d9b0b80cc6370c5e6d38165cea2032f471aab879e146183dba41` |
| Recipe | `07d27ddc8218d6c2aa5adf0f05227795a9f097802f50a534585e87206f8a2ad1` |
| Consumed approval | `bb116f3d918a000388b246f2caad1340a424e4f281c0d0cb9e47f03b8c3832cc` |
| Bounds | 300-second case, 30-second stages, separate 60-second cleanup |

Preflight revalidated the exact HEAD, clean tracked execution inputs, all 71
reviewed file hashes, 57 image sources, immutable local images, plan/recipe and
all fourteen required PR checks. Approval was recorded at **13:19:17.647 UTC**,
with expiry **14:19:17.647 UTC**, and remained live through execution.

The separate execution decision was recorded at **13:25:42.372 UTC**. The final
report's host file timestamp is **13:25:53.880 UTC**: an **11,508 ms**
decision-to-report interval. The resume-to-measure completion-receipt interval
was **4,326 ms**. These are bounded lifecycle observations, not per-operation
latency or percentile benchmarks. The controller returned PASS with exit status 0.

## Observed claim/release sequence

| Assertion | Invocation | Observed result |
|---|---|---|
| CR01 | Claim A | `CLAIMED`; fence 1, ordinal 1, one lease/pending reservation; claims and creations each 1 |
| CR02 | Identical claim A replay | `ALREADY_CLAIMED`; state and absolute expiries unchanged |
| CR03 | Release with a distinct, correctly framed wrong token | `LEASE_LOST`; current fence 1, no mutation |
| CR04 | Release A | `RELEASED_READY`; pending capacity refunded, lease cleared, cancelled reservation tombstone |
| CR05 | Identical release A replay | `RELEASED_READY`; ready timestamp, state and tombstone expiry unchanged |
| CR06 | Claim B | `CLAIMED`; fence 2, ordinal 2, distinct reservation; claims and creations each 2; A tombstone retained |
| CR07 | Old release A while B owns the lease | `LEASE_LOST`; current fence 2, B and both reservations unchanged |
| CR08 | Release B | `RELEASED_READY`; job ready, no live lease/pending/started capacity, two cancelled tombstones |
| CR09 | Identical release B replay | `RELEASED_READY`; state and both absolute expiries unchanged |

Every step exported 33 observed numeric counters with before/after/delta values.
The postcheck verified claims/creations/fence counts
`[1,1,1,1,1,2,2,2,2]` and pending counts `[1,1,1,0,0,1,1,0,0]`.
Replay/rejection steps CR02/03/05/07/09 had zero counter deltas and identical
preceding state hashes. Final next ordinal was 3; total job count and run/group
open counts stayed 1. Request starts, delivery attempts, started reservations, output commits,
retry/recovery and terminal-job counts stayed zero.

The reviewed executor checked complete typed state and indexes against its
independent fixture oracle over all 58 possible keys, including the BOOT-owned
durability key. A pending reservation had no Redis key expiry; a cancelled
tombstone's absolute expiry, observed via `PEXPIRETIME`, was exactly the release
timestamp plus 86,400,000 ms. Replays did not extend either tombstone.
Full private state was compared inside the executor;
exported receipts retain hashes, counters and redacted references.

## Bootstrap, ACLs and teardown

| Check | Result |
|---|---|
| Actual init/executor/Redis/revocation admission | PASS; exact images, users, capabilities, mounts, isolation and resource limits |
| Init | PASS; fresh empty volumes and bound configuration/ACL checksums |
| Preliminary persistence probe | PASS; acknowledged probe survived SIGKILL/same-volume restart, zero observed acknowledged loss |
| Restart identity | Changed from `1165c597d61301d7093a7c8363a870c947c03ce1` to `6735065a0bf33ac782dc491d7b8daa087f92c90b` |
| Canonical BOOT/replay and setup | PASS; durability binds the new run ID and probe evidence; observed Redis setup time, validated 26-key setup and full inventory |
| Early credential retirement | Setup, loader and BOOT revoked before measurement |
| CR10 authority ACL probes | 46/46 `NOPERM`: 21 absence-key checks plus 25 stored-authority mutation denials; state unchanged after every probe |
| CR11 state invariants | Exact records/indexes/counters and forbidden-key absence checked throughout |
| CR12 quiescence and revocation | Executor stopped/PID zero/removed before fresh cleanup helper; all six role credentials revoked with held-session/fresh-authentication checks |
| Owned-resource destruction | Four containers and two volumes removed; separate direct inspections and fixture-filtered listings confirmed absence |

All five stages (`init`, `probe`, `resume`, `measure`, `revoke`) passed. The
action journal contains **28 ordered receipts**, including **11 cleanup
actions**, and exactly matches the report's action array. Quiescence precedes
creation of the revoker; verified revocation precedes Redis removal. No stage,
journal or cleanup failure was recorded.

The coordinator's postcheck at **13:32:38.858454 UTC** revalidated receipt and
artifact bindings, counter/delta sequences, replay hashes, absolute expiries,
admissions, probe/BOOT and revocation evidence. It separately inspected the six
exact resource names and repeated fixture-filtered listings. This postcheck did
not reconstruct discarded private state or re-execute the case.

## Retained evidence and disposition

The [evidence package](evidence/m4-claim-run-2026-09-23/README.md) retains exact
report, intent, action-journal, execution-decision and postcheck bytes, with their
SHA-256 identities. The final report is **48,203 bytes**, SHA-256
`a2e16e91a190df09cee6fe6c76a7365a4f0e73632a7e354c593111878b62ff0b`.
It records `case_passed=true`, `case_evidence_valid=true`,
`evidence_kind=real_redis`, `revocation=verified` and `m4_accepted=false`.

Private originals, the unchanged approval/receipt, consumption reservation,
final `execution-approval.disposition.json` and scoped scan output remain under:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-claim-execution-2026-09-23/
```

The report, intent, action journal and execution decision originals are mode
0600 outside fixture volumes. All five exported files match their originals.
The scoped redacted Gitleaks scan found **no leaks**. The final private
disposition binds this fixture/report, records independently verified cleanup,
and marks approval **consumed**, `reusable=false`. The original approval cannot
authorize another attempt. Execution inputs stayed unchanged at `b4bda19`; this
result record and post-run status updates remain local.

## Remaining acceptance and next gate

This case establishes its bounded claim/release and ACL assertions. It does not
complete the 104-requirement/52-variant M4 inventory. Controller action receipts
are not an independent internal Lua crash-boundary trace, and the preliminary
probe does not certify durability of every claim transition or backup/restore.

Next, review and define the remaining M4-P3 bootstrap/ACL negative fixtures:
candidate/freeze presence, valid administrator-operation denial under ledger
credentials, malformed gates and isolation negatives. Close that coverage before
the wider worker-death/lease-expiry, request-start/finish and nonzero-baseline
cases. Concurrency, retry/dead/cancel, stage/commit, administrative profiles,
maximum-shape and latency coverage remain open. Further real runs require fresh
scope and exact-artifact authority. V2 application integration remains gated.
