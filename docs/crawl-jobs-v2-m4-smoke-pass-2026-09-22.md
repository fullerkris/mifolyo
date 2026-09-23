# M4 corrected bounded smoke case — 2026-09-22

**Verdict: PASS for `ledger-smoke-v1`; all six disposable resources confirmed absent.**
This is the first passing real-Redis smoke case. Full M4 remains open
(`m4_accepted=false`). The earlier [init FAIL](crawl-jobs-v2-m4-smoke-report-2026-09-22.md)
and its evidence are preserved separately.

## Authorization and merge identity

The owner reported PR #10 merged and requested preparation of the separate
execution request, then explicitly selected **Execute approved case**. The
[original request packet](crawl-jobs-v2-m4-execution-request-2026-09-22.md) is
preserved at SHA-256
`ef579c707f954835d17ee6a81cf7c250739f5a0c18b78a6d4f0446b427181627`.
Its pending-decision wording describes preparation time; the separate execution
decision and this final report record the later disposition.

| Item | Executed identity |
|---|---|
| Reviewed/executed commit | `a02991c322c3472f7460adbb94b2a15d82b77af6` |
| PR #10 squash merge | `ff2457ebe998707d220e4ce3425aab500c75f5b4` at 19:26:46 UTC |
| Common Git tree: approved head, tested PR merge and squash merge | `6c448ac59e70453bb5a10ebc7d781a5a2e00fb2b` |
| Fixture | `f9692c58d9f07689660a97fbc70ea973` |
| Case/operator/platform | `ledger-smoke-v1` / `fullerkris` / Linux/arm64 |
| Harness image | `sha256:2059066be4f192b84d4932050d1f811ed2bacf275e7d7cc0179f3e709d50e12c` |
| Redis 7.4.11 image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan | `d06a4ef887125883ecb1f0924f9192f4070bdb01c003d17ab718ca4baa451fbb` |
| Recipe | `9b0adc08f054775c24922a84012d2c4dbd950f630e151226536bd2e619ff81ab` |
| Consumed approval | `c057e40cb5ebad30a98aa178382b9621706108214b820359fd998c9561dd25da` |
| Approved bounds | 300-second case plus 60-second cleanup, one attempt |

The checkout stayed at the exact approved revision, with all execution inputs
tracked and clean. Fourteen required PR checks were rechecked against branch
protection. Image IDs/platform/layers, all 55 source files, plan/recipe and live
approval expiry were validated before invocation. Fixture-filtered listings were
empty. The separate post-merge CI runs were still in progress at preflight;
this case relies on the already-passing exact approved revision, not a fabricated
post-merge CI result.

The private execution decision was persisted at **19:36:47.975 UTC**; the
controller wrote its final report at **19:36:52.707 UTC**, before approval expiry
at 19:46:36.204 UTC. The host decision-to-report interval was **4,732 ms**; this is
not a Lua latency benchmark. The controller ran once and returned exit status 0.

## Observed checks

| Check | Result |
|---|---|
| Actual init/executor/Redis/revocation container admission | PASS; exact images, users, capabilities, mounts, isolation and resource limits |
| Init stage | PASS; fresh empty volumes and exact configuration/ACL-file checksums |
| Preliminary persistence probe | PASS; acknowledged probe preserved across the controller's SIGKILL/same-volume restart sequence, zero observed acknowledged loss |
| Restart identity | Redis run ID changed from `099eae52fb65c0094cbc05a871e88e2e77dbc714` to `a1fa046eb441be03a41da7e0b26f28e4aa5329c0` |
| Canonical BOOT and identical replay | PASS; approved durability record bound to the new Redis run ID and measured probe evidence |
| Exact four-record ledger setup | PASS; complete setup bytes/types/absence checks, observed Redis time, setup/loader/BOOT revocation before measurement |
| Empty maintenance and replay | Two `BATCH_DONE` replies, each with zero work counts; complete persistent state unchanged |
| Candidate/freeze ACL denials | 21/21 returned `NOPERM`; post-state unchanged |
| Worker quiescence | Executor stopped, PID zero and removed before the fresh cleanup helper |
| Credential revocation | All six roles rejected reconnect; held-session termination/previous revocation proven while Redis was reachable |
| Resource destruction | All four containers and both volumes removed; separate direct inspection confirmed each absent |

The final report sets `case_passed=true`, `case_evidence_valid=true`,
`evidence_kind=real_redis`, `revocation=verified` and `m4_accepted=false`.
The coordinator's postcheck revalidated report/intent/artifact binding, stage and
admission inventories, probe/BOOT/setup/state evidence, all 21 denials, revocation
and cleanup receipts. It then directly inspected every exact resource and repeated
fixture-filtered container/volume listings; all were absent.

## Retained evidence and limits

Exact-byte exports are in [`docs/evidence/m4-smoke-pass-2026-09-22/`](evidence/m4-smoke-pass-2026-09-22/).

| Artifact | SHA-256 |
|---|---|
| `f9692c58d9f07689660a97fbc70ea973.json` | `6152a302a95da89eaee3340f1376ec75c8c1833fe80763a587ce4307d6b3b7c5` |
| `f9692c58d9f07689660a97fbc70ea973.intent.json` | `a672893ae0c91e0f8697a2f9601b71cc6d1dbecb5fb7135c658659fab117fe77` |
| `execution-decision.json` | `a054b29782b7a2720f3d8f6d81d8646c37d5b2b5993967c0fe37cd2dcda7133c` |

`postcheck.json` retains the separate six-resource absence checks and report
validation. The post-run fixture-filtered historical Docker event query returned
**zero records**; its empty `docker-events.jsonl` is preserved explicitly, not
claimed as independent event-timeline proof. The optional event-count assertion
could not be established. Lifecycle evidence comes from the reviewed controller's
stage/inspection receipts and Redis observations, with separate live absence
checks for cleanup. No run was repeated to fill this optional history gap.

The original mode-0600 report/intent and execution decision remain under:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-execution-2026-09-22/
```

A scoped Gitleaks scan of the exported evidence found no leaks. The approval's
private consumption reservation and final disposition bind this fixture/result;
it cannot authorize another attempt. No implementation bytes were edited during
execution, and no retained datastore or application service was targeted.

## Next gate

Review and plan the next bounded M4 acceptance slice, including coverage and
evidence requirements, before seeking new exact-artifact execution authority.
Job/lease/stage/commit transitions, broader crash/concurrency cases, administrative
profiles, maximum shapes, latency and backup/restore remain unaccepted. This
smoke result does not authorize V2 application integration or a crawl.
