# P01 execution result — 2026-09-24

**Result: PASS.** One separately approved `ledger-candidate-compat-present-v1`
attempt completed on the exact prepared revision. Both CLAIM calls returned
`CRAWL_V2_INVALID_STATE`, complete state stayed unchanged and all 46 authority
probes returned `NOPERM`. All six roles were revoked and all four containers/two
volumes were independently found absent. **Approval is consumed.**

## Authority and exact identity

| Item | Identity |
|---|---|
| Executed commit | `963b67b73e2cd67ff73f559fa9984813b9d5946a` |
| Case / assertion | `ledger-candidate-compat-present-v1` / P01 |
| Fixture | `f3c66e07c31db6d3a141c083ba70c56d` |
| Operator / platform | `fullerkris` / Linux arm64 |
| Plan SHA-256 | `503f6ae40e0bd2ce7e833b90eb8c9d62b48d391972b508fb06b243ae0b33d79c` |
| Recipe SHA-256 | `7123ed770cb9c900319f412356652e7cb27eef6c3f2704818d3dee402fa4ee74` |
| Consumed approval SHA-256 | `d8eba81cc15e148c302f150bb96e40181ad4cde526c3dacc3f5354c964a5edc5` |
| Report SHA-256 | `745ffa976a63b186f319c2f0d0ab2ff8df0b0e4c2b2044bd8e5b69da0fc710fe` |
| Postcheck SHA-256 | `ef021599e7f8efa33b220cdf205436f539ee6fcace88e6979f48a757d1b39e87` |
| Coverage ledger SHA-256 | `3663474dd2abf9bf200cb25e2b48bee5bd4d230004b4e6363b91439053379719` |

The owner selected **Approve exact case**, then separately **Execute approved
case**. Approval was recorded at **17:53:20.739 UTC**, expiring at
**18:53:20.739 UTC**. Final preflight rechecked source, artifacts, local immutable
images, all 14 protected checks and absence of previous case resources before
recording the decision and exclusive one-use reservation. The controller was
invoked **once**. No automatic retry occurred.

The [P01 preparation record](crawl-jobs-v2-m4-p01-preparation-2026-09-24.md) binds
harness image `sha256:51bc4896057b8015e1bb67ff7e57448ed6356449c98a6df9100c06693de8e265`
and Redis 7.4.11 image
`sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c`.
All 81 independently reviewed sources and 15 recipes remain unchanged. The
published, tested and merged PR #13 trees match; the executed HEAD is the
explicitly approved published commit above. PC01 remains the accepted control.

## Observed result

P01's negative-stored-state fixture added only the correctly typed compatibility
hash at `mifolyo:contracts:candidate`. Candidate contract and `admin_freeze`
remained absent. The fixture had 27 direct setup keys and 58 possible positions,
with durability BOOT-owned. Only canonical `CJ2_APPROVE_BOOT` and `CJ2_TRY_CLAIM`
were loaded. Setup, loader and BOOT credentials were retired before measurement.

| Observation | Verified result |
|---|---|
| First otherwise valid CLAIM | `CRAWL_V2_INVALID_STATE`, from the active-gate presence rejection |
| Second prescribed CLAIM | Same exact rejection after a definite first result and unchanged-state check |
| Full typed state / absolute expiry | Reviewed executor/controller asserted equality; both exported state hashes equal setup and final state |
| Numeric accounting | 33 literal counters per before/after/delta snapshot; both deltas entirely zero |
| Direct authority checks | All 46 expected command/authority pairs return `NOPERM`, retaining the same state hash |
| Runtime admission | Init, executor, Redis and fresh revocation-container inspections pass exact specs |
| Process isolation | All five stages report holder plus current exec only, zero external routes, exact UID/capability predicates |
| Worker stop | Executor stopped, waited, PID zero and removed before fresh revoker creation |
| Revocation | All six roles reject reconnect while Redis remains reachable; held sessions terminated/already revoked, revoker last |
| Cleanup | Four exact containers and two volumes destroyed, then independently inspected absent |
| Journal | 28 ordered actions, including 11 cleanup actions; JSONL matches the final report |

Final counts remain at the initial ready state: one total/open job and one open
group job; next request ordinal 1; zero claims, lease fence, reservations, pending
capacity, starts, deliveries, retries, recoveries or output commits.

The preliminary acknowledged probe survived restart from Redis run ID
`ac6d3dbbf8f365d74015df51b6471c8bd4a9c5f0` to
`6fe9eba0b0664a0aca9c048d2c5c7cc9b2072bf5`. Probe hash, fixture/plan identity,
observed Redis time and resulting BOOT record are cross-bound in retained
receipts. The approved 300/30/60-second budgets, 128/256/528 MiB role memory bounds
and 400 MiB Redis maxmemory were retained.

| Timing | Observed UTC / interval |
|---|---|
| Execution decision / reservation | 18:04:28.167 |
| Final report written | 18:04:40.906 |
| Independent postcheck complete | 18:08:48.062438 |
| Decision to report | 12,739 ms |
| Resume-to-measure receipt interval | 3,106 ms |

These lifecycle intervals are not per-Lua latency measurements or the required
1,000-sample latency gate.

## Evidence, verification and disposition

The [run evidence package](evidence/m4-p01-run-2026-09-24/README.md) contains exact
copies of the 27,230-byte final report, intent, action journal, execution decision
and independent postcheck, plus a separate coverage ledger. Intent remains
`INCOMPLETE` by design; the final report records `case_passed=true`,
`case_evidence_valid=true`, `evidence_kind=real_redis`, `revocation=verified` and
`m4_accepted=false`.

The postcheck recomputed literal counter values, the exact two error receipts,
46 command/authority pairs, state-hash relationships, public probe/BOOT bindings,
approval timing, all container specs and journal ordering. It independently
inspected each named resource and empty fixture-filtered listings. Only this
read-only postcheck was repeated after correcting its scanner-array JSON parser;
the case and immutable receipts were not rerun or changed.

Run evidence and exported run package scans reported zero findings. Preparation
scanning found two copies of the unchanged public RETIRE Lua source SHA-256;
both exact locations were verified against canonical source bytes, leaving zero
unresolved findings without suppression changes.

Private directory (0700), original approval/decision/reservation/disposition
files (0600), raw receipts and validation scripts are retained under:
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-p01-execution-2026-09-24/`.
The final disposition binds this report and records **consumed**, `reusable=false`.
Remaining expiry does not authorize reuse.

## Coverage and next gate

P01 provides observed partial evidence for
**`17.7/e763d134a36fb68c`**: candidate-compatibility presence rejection and this
case's authority-denial control. E01–E05 observations are scoped to this successful
case; interruption/failure paths and H/I negative predicates gain no new target
evidence. No full requirement or operation variant is closed. Original planning
packets and prior evidence remain immutable.

**Next gate: scoped P01 evidence review**, before preparing P02 /
`ledger-candidate-contract-present-v1` with its own artifacts and fresh decisions.
The package now has accepted PC01 control, postchecked P01 PASS awaiting that
scoped review, and **12 negative cases unrun**. M4-P3/full M4 remain open.

Private full-state comparisons were performed inside the reviewed executor and
cannot be reconstructed from exported hashes. Controller actions do not observe
internal Lua crash boundaries. This probe does not establish all-transition AOF,
restore, maximum-shape memory, latency or final release provenance. The
[primary plan](crawl-jobs-v2-plan.md) retains those gates.
