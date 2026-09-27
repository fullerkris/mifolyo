# Full M4 acceptance progress — 2026-09-25

**M4-P3 bootstrap/ACL readiness is accepted within the frozen checkpoint scope.**
Independent correctness and defensive security reviews both returned GO after
reconciling the completed fourteen-case Redis matrix and H/I admission evidence.
**Full M4 remains open:** M4-P4's full matrix and M4-P5's final acceptance gates
are not complete.

The next implementation foundation is an offline pre-I/O recovery oracle, with
six Python tests and 26 independent Go/canonical-Lua differential invocations.
It is not a registered executable case and does not observe actual worker death.

## M4-P3 reconciliation

The accepted checkpoint is `963b67b73e2cd67ff73f559fa9984813b9d5946a`, with frozen
inventory `dbe881b2c269535b633188986c6f3adbdac0226df23528290bc4554c93277113`.
At reconciliation, all 81 source hashes and 15 recipes matched. The prior
[fourteen-case matrix](crawl-jobs-v2-m4-negative-remainder-2026-09-24.md) retains
actual Redis bootstrap, BOOT, ledger/admin separation, active transition/replay
and successful credential/resource teardown. Its historical files are unchanged.

Additional reconciliation observed:

- Four newly created role containers admitted while **stopped**, with all six
  metadata resources inspected absent after cleanup.
- **82 exact planned H/I variants** passing, with captured valid target metadata
  substituted where applicable and real backend/socket dispatch guarded.
- **Seven supplemental host checks:** Memory/NanoCpus metadata and simulated
  replica/AOF/loading/pre-stage-memory observations.
- **Nine target-image predicate controls** in one separate networkless Python
  container: network input mutations, proxy/process canaries and temporary
  filesystem predicates. No Redis server or acceptance case started.

Evidence classes remain explicit. Captured metadata mutations are not unsafe
target-container runs; synthetic process rows are not actual extra workers;
configuration facades are not faulty Redis executions. Positive target admission
does not prove every adverse target or interruption path.

Both independent reviewers checked source/evidence hashes and relevant receipt
relationships without starting infrastructure. Correctness review session:
`ses_f2a72e458ffelqguj51148wBms`. Defensive security review session:
`ses_f26b21d03ffeou51ozKIElqzFy`. Neither found a blocking issue for narrow P3.

The predicate helper's exported cleanup receipt contains only the producer's
successful cleanup assertion, without a separate resource identity/absence
receipt. This limitation is retained, not upgraded to independently reobserved
cleanup. The actual Redis cases have their separate retained teardown evidence.
The reconciliation driver and predicate payload hashes are pinned in the review
record.

| Artifact | SHA-256 |
|---|---|
| Admission reconciliation | `6e74e8291b578463f774edc921b44f400cc93c94e01496c786543d26bcdf94f4` |
| Target predicate controls | `081f30e41d4125a7c9b82738f6780a2a8e09e6eb111eaba6ac474fe82072d4fe` |
| Independent review decisions | `a1ab951eb1ad4c3256b30eba5c0c0236cbd0268fc1f95a57b3dcba34bb9d4066` |
| Scoped P3 acceptance | `4ea4a7ee41f17fe56563526c9101d4f47ae09e8fd154e04e2c3ffae33e183c76` |
| Full M4 planning inventory | `d69db20eb96e3ae47163722d0c31cacdeed674b8004d111e4eec1f4a918c2ada` |

The [evidence package](evidence/m4-readiness-2026-09-25/README.md) records this
new scoped decision. Earlier `m4_p3_accepted=false` snapshots remain historical;
they are not rewritten. New source work must be reviewed separately and evidence
affected by its changes reassessed.

## Full M4 inventory

The immutable, non-executable
[work inventory](../tests/crawl-jobs-v2-redis/planning/full-m4-acceptance-v1.json)
retains all **104 normative requirement entries**, **43 operations** and **52
allowed gate variants**. Seven variants have scoped real observations, not full
variant closure. No full requirement or variant is marked closed by this plan.

It maps planned test IDs into twelve work packages:

1. Recovery, actual synthetic-worker death and stale fencing.
2. Request accounting, reservation limits, shared rates and contention.
3. Run creation, enqueue, audit, seal and activation.
4. Outcomes, retry/backoff, dead/cancel/no-output behavior.
5. Finalization, retention, archive and purge.
6. Stages, all chunk families, output, abort/cleanup and backpressure.
7. Full fresh/migration administration and planned restart.
8. All-state durability, corruption and restore.
9. Observed internal commit crash boundaries.
10. Combined maximum shapes and allocator/safety-floor measurements.
11. Per-operation maximum-shape latency, at least 1,000 complete samples including AOF.
12. Exact source/oracle/CI coverage and independent final evidence review.

Mixed obligations retain their later owners: real Spider/consumer/Monitoring
behavior belongs to M5, operational coordinated cutover/restore to M6, and final
runtime-image provenance/release assembly to M5/M7. Synthetic M4 observations
cannot mark those integrations passed.

## Recovery-oracle foundation

New files:

- `tests/crawl-jobs-v2-redis/recovery_oracle.py`
- `tests/crawl-jobs-v2-redis/test_recovery_oracle.py`
- `services/spider/internal/database/crawljobsv2/m4_recovery_oracle_test.go`

The Spider builder allowlist copies the two Python modules for differential
tests; its final runtime image does not acquire these modules. The executable
Redis registry and acceptance-image contents remain unchanged.

The thirteen-step offline sequence covers claim A, early recovery, due recovery,
recovery replay, B's new fence/claim replay, stale A claim/release/renewal, B's
successful renewal/release/replay and final drained recovery. It checks complete
58-position state, original lease and tombstone constants, counter preservation,
the stale-renewal counter-only exception, and exact physical expiries. Early
recovery is tested at **E−1**, followed by due recovery at **E** or **E+1**.

Go independently constructs every wire and executes unchanged canonical Lua in
an in-memory command facade. It compares replies, state, expiry, BOOT preservation,
read-only steps and the proposed exact command/key selectors. One proposed HSET
grant is limited to the run's recovery-outcome map; no live ACL is changed.

Independent correctness review returned **GO for the offline foundation only**.
Two nonblocking findings were corrected and re-reviewed: malformed nested plan
inputs now produce the closed artifact error, and a still-sorted equal-time
renewal test isolates the intended phase predicate. The 30,000/30,001 ms phase
boundary controls are also explicit. No correctness findings remain in that scope.

Final corrected checks:

- Six Python tests: PASS, 4,584 ms wall time.
- New Go differential root under `-race`: PASS, 195,334 ms wall time,
  **26 canonical invocations** across the two vectors.
- Package `go vet`: PASS.
- Strict complete Lua assembly and bundle-pin checks: PASS.

These are local offline checks, not protected CI for the new changes, elapsed
Redis time, AOF acceptance or actual process death. The prospective
`ledger-worker-death-pre-io-v1` case remains outside the executable registry.
Its claimant acknowledgment/park/SIGKILL handshake, distinct replacement worker,
short-stage Redis-TIME wait loop, evidence validation and complete lifecycle
integration still need implementation/review before image preparation and
fresh exact-artifact execution approval.

## Design gates identified by independent research

The owner selected **Local Docker** and **Draft proposal for review**. The
[corrected design proposal](crawl-jobs-v2-m4-observer-and-failure-design-2026-09-25.md)
is now at proposal-intake GO from both correctness and defensive security reviewers,
SHA-256 `f6568eb6b322fa84fc9164d1b616d983a4f71cdc1dc85d42bccc4cb59b1479ae`.
The initial acknowledgment, fault-class and cleanup ambiguities were corrected
and re-reviewed. Review record:
`5a93f4e492641b65652d536c26db70e93a3bd5f1b439336c8a1d7a6b35e28ae9`.
This is not approval to apply its proposed normative amendment or trace Redis.

A subsequent static inspection copied the pinned Redis executable from a stopped
owned container, which remained `created` with PID zero and was removed with
name/ID absence checked. The 16,892,616-byte AArch64 PIE has ELF SHA-256
`772f79e9154598fe509961928dc4d7c1ac577a948be6e86b793fdb7b0599e1b6` and GNU build ID
`a6678635938881e640c42ca2824a014aea114c4f`. Symbol tables and DWARF information are
present, including candidate Lua VM, command dispatch and AOF symbols. Report:
`fa3c1c00fa0392438f748d7902ba2e0e083a05311c84cbeeaa60921f8a728ff3`.
Redis was not started and tracing was not performed. Exact source/address mapping,
confinement and stop precision remain unproven; this does not close OBS0–OBS4.

**D01 — Internal commit-boundary observation.** Current stage-level kills and
MONITOR-triggered/random kills cannot certify exact internal execution cuts.
Redis 7.4.11 rejects debugger-enabled EVALSHA; synchronous Lua debugging switches
to EVAL and changes timeout/debug behavior, so it is not an equivalent substitute.
The strongest researched candidate is an external Linux-side held-boundary
observer preserving canonical Lua and normal Redis execution. Its exact ELF/source
mapping, narrowly scoped process access, hardware support, timing and failure
handling need an approved design and feasibility evidence. No such tracing
capability or method has been demonstrated or authorized here.

Static source review identifies approximately 1,050 mutation-command attempts for
one specified combined maximum-count fresh-discovery/alias COMMIT branch. This
is a branch-specific static derivation, not an observed trace or proof of the
maximum approved byte/memory fixture. Pure-Lua prevalidation, no-write/backpressure
branches, AOF and acknowledgment edges require separate boundary identification.

**D02 — Unrestartable AOF and credential teardown.** A permitted fail-closed AOF
startup outcome must be reconciled with the requirement to prove revocation and
failed reconnect while Redis is reachable. No accepted lifecycle method yet
resolves an unrestartable instance. Silent AOF repair or authentication against a
substituted server is not a solution. A reviewed design resolution—and, if truly
necessary, an explicit narrow owner-approved protocol clarification—is required.

**D03 — Long-lived expiry and benchmark profiles.** Current 300-second cases and
30-second stages do not directly cover 900-second stage lifetime, one-day
tombstones or all benchmark workloads. New bounded profiles require review;
constants and clocks must not be shortened or seeded history relabeled as
observed elapsed-time behavior.

**D04 — Maximum-shape transport/state readers.** The current small-case 2 MiB
request limit and 2/58/71-key reader do not support every allowed maximum blob,
LIST or large retained-state inventory. Extend by specific operation and shape,
not unrestricted caller-controlled limits.

**D05 — Final acceptance.** New reviewed source/images must pass exact-revision
protected checks, required real-case coverage cannot be skipped, and final
measurements require independent review. Prior CI cannot attest new uncommitted
code merely because canonical Lua is unchanged.

Research session: `ses_f2a72e472ffe4hEEimWMxFU37w`. Relevant version-specific
references include [Redis 7.4.11 eval/debugger implementation](https://github.com/redis/redis/blob/7.4.11/src/eval.c)
and [AOF implementation](https://github.com/redis/redis/blob/7.4.11/src/aof.c).
No tracing experiment, unsafe container, new Redis case or normative amendment
was performed during this work. The [primary plan](crawl-jobs-v2-plan.md) owns
the current implementation status and decisions.
