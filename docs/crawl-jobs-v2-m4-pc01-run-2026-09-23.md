# M4 refreshed PC01 control — 2026-09-23

**Verdict: PASS for PC01 / `ledger-claim-release-v1` on the reviewed bootstrap/ACL
harness.** The single approved invocation completed and all six disposable
resources were independently confirmed absent. Approval is consumed. The 13
negative cases and full M4 acceptance remain open (`m4_accepted=false`).

**Subsequent evidence review (2026-09-24):** PC01 is accepted as the scoped
package control. See the [Step 6 assessment](crawl-jobs-v2-m4-pc01-evidence-review-2026-09-24.md)
for the partial coverage ledger and P01 preparation handoff. The original run
observations and evidence bytes below remain preserved.

## Exact authority and invocation

After [Step 4 publication and CI](crawl-jobs-v2-m4-bootstrap-acl-ci-2026-09-23.md),
the owner selected **Approve exact case**, then separately **Execute approved
case**. Approval was recorded privately at **20:15:21.001 UTC**, expiring at
**21:15:21.001 UTC**. The original approval/receipt remain unchanged; the separate
decision, single-use reservation and final disposition record the later execution.

| Item | Executed identity |
|---|---|
| Commit | `634040131b36e1cbbc2e251364dacbec2ae5dd01` |
| Branch / PR at execution | `feature/crawl-jobs-v2-bootstrap-acl` / draft, unmerged [PR #12](https://github.com/fullerkris/mifolyo/pull/12) |
| Fixture | `0a1a9641a6044e4dfde80a5c3d381216` |
| Case / operator / platform | `ledger-claim-release-v1` (PC01) / `fullerkris` / Linux/arm64 |
| Harness image | `sha256:51bc4896057b8015e1bb67ff7e57448ed6356449c98a6df9100c06693de8e265` |
| Redis 7.4.11 image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan | `0861036947cfbcce72c855a40e1b489ab572479021fbb417a59a719681b3d76c` |
| Recipe | `11be906770c6f8c8ebfceef60022f4b73720498cb7141e2e3fdf2f634d83fd8b` |
| Consumed approval | `3e034bd6f56cc595fd4d8cba566f2722a3428691f185cecfd59228dedde7a695` |
| Bounds | One attempt; 300-second case, 30-second stages, separate 60-second cleanup |

Preflight revalidated clean tracked execution inputs, all 81 reviewed hashes,
62 image inputs, local immutable images, plan/recipe and all fourteen required
CI checks. One GitHub HTTP 503 interrupted the read-only branch-protection check
**before reservation, evidence-directory creation or controller invocation**.
That read-only verification was repeated successfully; the controller ran once.
No case was automatically retried and no check was bypassed.

The separate execution decision was recorded at **20:22:39.230 UTC**. The final
report's host file timestamp is **20:22:51.406 UTC**, within the live approval.
Decision-to-report elapsed time was **12,176 ms**; the resume-to-measure
completion-receipt interval was **4,388 ms**. These are lifecycle observations,
not per-operation latency measurements or percentile benchmarks.

## Actual observations

| Check | Result |
|---|---|
| Four actual container admissions | PASS; immutable images, bound environments, exact commands, users/capabilities, mounts, resource limits and private sharing |
| Runtime process/isolation predicates | PASS in init/probe/resume/measure/revoke: exactly two processes (holder and current exec), zero external routes, UID 0 with CHOWN only for init, UID 65534 with zero effective capabilities otherwise |
| Persistence probe | Acknowledged bytes survived SIGKILL/same-volume restart; zero observed acknowledged loss |
| Restart identity | Changed from `523875915d46f5c97af3c098573aadf314ea0917` to `86ee39a270eb18ca2dd77e8f9e664f9edca7258e` |
| Canonical BOOT and replay | PASS; durability binds current run ID and measured probe evidence |
| Setup / early retirement | Observed Redis time, exact 26-key setup and 58-position inventory checked; setup/loader/BOOT revoked before measurement |
| Claim/release sequence | All nine prescribed results and full state/absolute-expiry assertions passed |
| Authority ACL probes | **46/46 `NOPERM`**, with unchanged state |
| Public counters | **33 counters per snapshot**, with exact before/after/delta validation |
| Controller journal | **28 ordered actions**, including **11 cleanup actions**, matching the report exactly |
| Quiescence and revocation | Executor stopped/waited/PID zero/removed before fresh revoker; all six credentials revoked with session/reconnect checks |
| Cleanup | Four containers and two volumes removed; separate direct inspections and fixture-filtered listings confirmed absence |

The transition results were:

1. Claim A → `CLAIMED`.
2. Exact claim replay → `ALREADY_CLAIMED`.
3. Well-formed wrong-token release → `LEASE_LOST`.
4. Release A → `RELEASED_READY`.
5. Release A replay → `RELEASED_READY`.
6. Claim B → `CLAIMED`.
7. Old release A against fence B → `LEASE_LOST`.
8. Release B → `RELEASED_READY`.
9. Release B replay → `RELEASED_READY`.

Final claims/creations/job claim count/fence were 2 and next ordinal was 3. Total
job count and run/group open counts stayed 1; final pending/active/started
reservation capacity was zero. Request starts, delivery attempts, output commits,
retries, recoveries and terminal-job counts stayed zero. Replay/rejection steps
had zero counter deltas and identical prior state hashes. Pending reservations
had no key expiry; cancellation set the absolute expiry to release time plus
86,400,000 ms, unchanged by replay.

## Postcheck and exact evidence

The coordinator's postcheck at **20:26:54.614767 UTC** revalidated report/intent/
approval/artifact bindings, current environment-bound admission specs, all stage
and process receipts, probe/BOOT, counter sequences, replay hashes, absolute
expiries, revocation and journal ordering. It then directly inspected each of
the six exact resource names and repeated fixture-filtered listings.

The [evidence package](evidence/m4-pc01-run-2026-09-23/README.md) contains exact-byte
report, immutable intent, action journal, execution decision and postcheck exports.
The report is **49,637 bytes**, SHA-256
`2e1d4817d835d5a76a42552ca31645f97b406843b0bd980a0651353843158e86`.
It records `case_passed=true`, `case_evidence_valid=true`,
`evidence_kind=real_redis`, `revocation=verified` and `m4_accepted=false`.

Private mode-0600 originals, approval/decision/reservation/disposition, verification
scripts and scan output remain outside fixture volumes under:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-pc01-execution-2026-09-23/
```

The exported evidence scan found no leaks. The final private disposition binds
this fixture and report, records independently verified cleanup and marks the
approval **consumed**, `reusable=false`. Execution inputs stayed unchanged at
`6340401`; the result/evidence/status records are published separately in the
documentation/evidence checkpoint.

## Coverage and next gate

PC01 now supplies the refreshed positive control on the new harness. Its
preliminary persistence probe is not durability coverage for every transition,
and controller actions are not an independent internal Lua crash-boundary trace.
Full private state was checked inside the executor; the postcheck validates
public receipts and independently observes cleanup without recreating that state.

Proceed to Step 6 review of this PC01 evidence, retaining the 13 negative cases
as unrun. The first planned negative is **P01 /
`ledger-candidate-compat-present-v1`**. Prepare/revalidate its own selected-case
artifacts and obtain its own exact approval and separate execution decision
before running it. No further case, merge or application activation is authorized
by this consumed PC01 approval. The primary [plan](crawl-jobs-v2-plan.md) owns
the remaining sequence and acceptance status.
