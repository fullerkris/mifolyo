# Separate bounded M4 execution request — 2026-09-22

**Request prepared; execution decision pending.** This requests one actual
`ledger-smoke-v1` attempt under the existing corrected-artifact approval.

## Merge and exact revision

PR #10 merged at 19:26:46 UTC as
`ff2457ebe998707d220e4ce3425aab500c75f5b4`. That squash commit, approved head
`a02991c322c3472f7460adbb94b2a15d82b77af6`, and tested PR merge commit
`45c3c08f2cfe48222b0be31349e87af9ce063b56` all identify Git tree
`6c448ac59e70453bb5a10ebc7d781a5a2e00fb2b`. Their repository contents are identical.

Execution stays at the exact approved `a02991c` checkout, whose runtime inputs
are tracked and clean. This does not substitute the squash commit into the
approval. The fourteen protected PR checks passed on the approved head; separate
post-merge workflows are still running and are not claimed as passed here.

## Requested operation

- Operator/platform: `fullerkris`, Linux/arm64, local Docker Unix socket.
- One fresh, networkless fixture with new exclusive control/data volumes.
- Initialize exact configuration and six short-lived ACL roles; verify isolation.
- Write one bounded persistence probe, SIGKILL/restart Redis, verify zero loss,
  then execute canonical BOOT and identical replay.
- Install the reviewed empty-ledger fixture, revoke setup roles, run empty rate
  maintenance twice and check unchanged state plus 21 ACL denials.
- Quiesce the worker, verify revocation and remove all exact owned resources.
- Budget: 300 seconds plus separate 60-second cleanup; no automatic retry.

## Approval and immutable artifacts

| Item | Identity |
|---|---|
| Approval SHA-256 | `c057e40cb5ebad30a98aa178382b9621706108214b820359fd998c9561dd25da` |
| Approved commit | `a02991c322c3472f7460adbb94b2a15d82b77af6` |
| Plan | `d06a4ef887125883ecb1f0924f9192f4070bdb01c003d17ab718ca4baa451fbb` |
| Recipe | `9b0adc08f054775c24922a84012d2c4dbd950f630e151226536bd2e619ff81ab` |
| Harness image | `sha256:2059066be4f192b84d4932050d1f811ed2bacf275e7d7cc0179f3e709d50e12c` |
| Redis 7.4.11 image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Approval expiry | **2026-09-22 19:46:36.204 UTC** |

The live validator requires more than the whole 300-second execution budget
remaining at preflight. A delayed decision therefore requires renewed approval
rather than silently extending this one. Images/source/artifact identities passed
read-only revalidation; fixture-filtered container/volume listings were empty.

Evidence destination (outside Docker volumes, private directory and mode-0600
intent/report files):

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-execution-2026-09-22/evidence/
```

The controller persists intent before mutation. Any failure stops this attempt,
retains bounded diagnostics, and invokes cleanup; failed or unproved cleanup
invalidates evidence. Record the approval as consumed when invoking the single
case. A passing smoke case still does not certify the remaining M4 matrix.

The scope excludes application services, retained datastores, public requests,
crawling, rendering, migration, deployment and production promotion.
