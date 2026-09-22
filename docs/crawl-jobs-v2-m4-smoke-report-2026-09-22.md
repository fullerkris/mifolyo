# M4 bounded smoke attempt — 2026-09-22

**Verdict: FAIL in `init`, before Redis startup. Cleanup verified.**

The owner explicitly requested execution of the already-approved single
`ledger-smoke-v1` case. The attempt used the unchanged reviewed revision,
images, plan, recipe and live approval. It did not reach the persistence probe,
BOOT, active maintenance or ACL measurements. This is failure evidence, not M4
acceptance or evidence of a Redis/Lua transition defect.

## Exact identity and preflight

| Item | Recorded value |
|---|---|
| Commit | `340906c694ee6f51d67ea0c9b448b07f29df4834` |
| Fixture ID | `6c963c07b5526561459830bac338b997` |
| Case/platform | `ledger-smoke-v1`, Linux/arm64 |
| Harness image | `sha256:4b0ca8a3cca08646616bfe6952fc2f2423cb3bd55496ee5e0a1312be223f480a` |
| Redis image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan SHA-256 | `bf22f79cd2b23e09aa77fa288b70c190c7c04ef24384d5c48ea5d5a885c362c4` |
| Recipe SHA-256 | `2cfb26736d94c9c05188989e8da814272c7a662ac0441d045ff941c5508b078b` |
| Approval SHA-256 | `c63cbb050d81466b767ad7a55787ec450bb6df6c472f39fd4e19ee87dcd012c3` |
| Approval expiry | 2026-09-22 17:18:43.933 UTC |
| Budget | 300-second case plus 60-second cleanup |

Before invocation, the approval/plan/recipe/revision validation passed, both exact
images were present as Linux/arm64, all fourteen required PR checks remained
SUCCESS, and no `ledger-smoke-v1` labeled containers or volumes existed. Local
execution-source paths were tracked and clean. PR #10 remained draft and unmerged.

## Observed outcome

The controller exited nonzero with `M4 case FAIL`. Its report records:

- `failure_phase=init`;
- no container-admission receipt and no completed stage output;
- `revocation=server_never_started`;
- `case_passed=false`, `case_evidence_valid=false`, `m4_accepted=false`;
- removal of the init container and both new volumes.

Docker's fixture-filtered events between 16:36 and 16:40 UTC show creation and
destruction of the init container at **16:38:49 UTC**, with no `start` event. The
container ID was
`4a6ffba9893e282e7cd6dce0688ba573f8b9fcf25f06ddcccf3db954c2cbe11e`.
These observations localize the failure to init-container setup/pre-start
handling. They do not identify the exact failed inspection predicate or Docker
error: the approved controller retained only the phase, not that diagnostic.

No Redis container was created or started. The persistent probe, BOOT approval,
marker setup, maintenance calls, ACL-denial tests and revocation tests did not
run. `evidence_kind=real_redis` names the selected backend in the report; it is
not proof that Redis actually ran.

## Cleanup and retained evidence

The controller reported `removed=true` for:

- `cj2-m4-6c963c07b5526561459830bac338b997-init`
- `cj2-m4-6c963c07b5526561459830bac338b997-control`
- `cj2-m4-6c963c07b5526561459830bac338b997-data`

At 16:39 UTC, separate Docker container/volume listings filtered by this exact
fixture label returned no resources. Direct inspection of the init container
returned `No such container`; direct inspection of both exact volume names also
returned `no such volume`. No commands targeted retained-stack data.

Exact-byte copies are stored under
[`docs/evidence/m4-smoke-2026-09-22/`](evidence/m4-smoke-2026-09-22/).

| Artifact | SHA-256 |
|---|---|
| `6c963c07b5526561459830bac338b997.json` | `016d79db4345f5b2a74236bb6159fa1610fda49eb54d31ad0a45362c826fef88` |
| `6c963c07b5526561459830bac338b997.intent.json` | `ea3e026fed276e53185aae4cbbe6a924de4ed5e2f177c9c7d0bf7effcbe29636` |

The original mode-0600 report/intent remain outside Docker volumes at:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-image-prep-2026-09-21/smoke-2026-09-22/
```

`docker-events.jsonl` preserves the two returned events. The original intent
continues to say `INCOMPLETE` because it is an immutable pre-mutation record;
the separate final FAIL report binds its hash and records cleanup.

## Next gate

The one-case approval has been used by this attempt, despite time remaining
before its expiry. Do not retry with it. Diagnose the init-container inspection/
start boundary using narrowly scoped metadata and value-redacted diagnostics,
then add an appropriate target-Docker regression. The present evidence is not
sufficient to choose a specific normalization fix or relax an isolation check.

Any implementation change requires reviewed new source/image/recipe identities,
applicable CI and a fresh exact-artifact approval before another smoke attempt.
The approved implementation was not changed or retried in this run task.

## Subsequent diagnosis

The separately authorized [init correction](crawl-jobs-v2-m4-init-fix-2026-09-22.md)
reproduced the pre-start rejection: Docker reports the requested `CHOWN` as
`CAP_CHOWN`. A captured-metadata regression, narrow alias admission, value-free
diagnostics and stopped-role preparation checks now pass independent review and
revised-image validation. The original FAIL artifacts above remain unchanged.
Another attempt still requires the corrective checkpoint's CI and fresh approval.
