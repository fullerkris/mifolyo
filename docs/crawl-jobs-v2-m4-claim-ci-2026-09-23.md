# Claim/release Step 5 publication and CI — 2026-09-23

**Step 5 complete: immutable image preparation and all fourteen required CI
contexts pass on the published checkpoint.** At this checkpoint, fresh execution
approval was pending and no real claim/release case had run.

**Subsequent Step 6 result: PASS.** The owner approved the exact artifacts below
and separately requested execution. Fixture `f59af8adfe82573b64b8dfda427a0b00`
passed on unchanged `b4bda19`; its one-case approval is consumed and all six
disposable resources were independently confirmed absent. See the
[dated execution result](crawl-jobs-v2-m4-claim-run-2026-09-23.md).

**Subsequent merge:** PR #11 merged as `9b6b8f9948d04b5dff4378f491a52638a1517254`
at 16:00:17 UTC on September 23. Its tree matches the reviewed/executed `b4bda19`
tree exactly. Draft/unmerged wording below preserves the publication-time snapshot.

This is a local post-CI record. It preserves the exact tested HEAD while recording
results that could only be known after publication. The
[implementation plan](crawl-jobs-v2-plan.md) owns current status.

## Exact published revision

- Commit: `b4bda19f07bb22f37737508cd10424b75690d6f5`.
- Branch: `feature/crawl-jobs-v2-claim-release`; local/remote identity verified.
- Base: `ff2457ebe998707d220e4ce3425aab500c75f5b4`.
- Tested PR merge commit: `f26f87392ac7f88f1fca7291da53bbb2e1a727dc`.
- [PR #11](https://github.com/fullerkris/mifolyo/pull/11): **draft and unmerged**.
- Scope: 43 intended files; unrelated local work excluded.

The exact staged export passed 79 harness tests (160.507 s), 21 script tests,
targeted Go M4 race checks (140.877 s), package vet, bundle/digest checks,
reviewed-source/artifact verification and actionlint. Its tree identity was
`784afcea527f2bac4ae140837195fca0f87b4f02`.

The scoped Gitleaks scan produced four generic-key findings. Every matched value
was independently confirmed as the existing public
`cj2_retire_legacy_keys.lua` SHA-256, repeated in review/image inventories:
`702b096b843d80cd1c85186cf79fafa29c08766ea22c599af29877e1e8013fc7`.
No unresolved credential finding remained.

## Protected CI

- [Required Checks 35857689984](https://github.com/fullerkris/mifolyo/actions/runs/35857689984): **SUCCESS**.
- [Unit Tests 35857690233](https://github.com/fullerkris/mifolyo/actions/runs/35857690233): **SUCCESS**.

All fourteen required contexts were checked against the current branch-protection
inventory and individually confirmed SUCCESS. The transient GitHub API timeout
affected polling only; no workflow or assertion was bypassed or rerun around a failure.

The eight downloaded shard reports match the tested PR merge commit and the
locally compiled inventory. Repository verification confirms complete, disjoint
coverage of **471 roots: 470 passed and only `TestJobLuaNativeFactoryParity`
optionally skipped**. The required Spider aggregate also passed every other
package and its integration/dependency checks.

The downloaded amd64 image report explicitly selects `ledger-claim-release-v1`.
All four stopped-role admissions, exact 57-file contents, both recipe identities,
source identities, memory bounds, exit/OOM checks and cleanup receipts passed
independent revalidation. It records `redis_started=false` and
`execution_authorized=false`.

Retained CI-gate SHA-256:
`39dbe3d122515f116d1330b793d7def07cf01bc3ea5d3f7f6a0f5af8f160f519`.

## Candidate arm64 execution artifacts

| Artifact | Identity |
|---|---|
| Harness image | `sha256:b8de7cf09495bca22f6bba158bfdbe776b65ab726895e484682a3bb4f95a8a01` |
| Redis 7.4.11 image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Claim plan | `9833e6c25c74d9b0b80cc6370c5e6d38165cea2032f471aab879e146183dba41` |
| Claim recipe | `07d27ddc8218d6c2aa5adf0f05227795a9f097802f50a534585e87206f8a2ad1` |
| Arm64 image report | `6d356f63cb0e7254d34c22cd51ff46b9d629078d9dca7b1be6f59c4548f10917` |

[Committed preparation artifacts](evidence/m4-claim-image-prep-2026-09-23/README.md)
bind the reviewed source and local image validation. These identities are not
approval and do not establish target claim/lease execution behavior.

The private preparation directory retains `ci-gate.json`, `ci-shards/`,
`ci-image-amd64/`, build metadata, exact-index verification and scan triage:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-claim-prep-2026-09-23/
```

`execution-request.json` is deliberately **unapproved** (`approved=false`, expiry
zero). No execution approval was created during Step 5.

## Step 6 gate at publication (subsequently completed)

The next gate was fresh owner approval for one exact `ledger-claim-release-v1`
case binding the published commit, image/plan/recipe identities, operator,
platform, bounds, evidence destination and expiry, followed by a separate
execution request and live-approval revalidation. Step 6 completed those checks
under approval `bb116f3d918a000388b246f2caad1340a424e4f281c0d0cb9e47f03b8c3832cc`,
with the unchanged 300-second case and separate 60-second cleanup budgets.
That approval is consumed. The initial Step 5 request template remains
unapproved and unchanged. Full M4 acceptance, merge and runtime activation
remain separate gates.
