# Bootstrap/ACL Step 4 publication and CI — 2026-09-23

**Step 4 complete: reviewed arm64/amd64 image preparation and all fourteen
required CI contexts PASS on the published checkpoint.** PC01 selects the
refreshed `ledger-claim-release-v1` control. At this checkpoint, fresh owner
approval and a separate execution request were the next gate.

**Subsequent Step 5 result: PC01 PASS.** The owner approved the exact artifacts
and separately requested execution. Fixture `0a1a9641a6044e4dfde80a5c3d381216`
passed on unchanged `6340401`; all six resources were independently confirmed
absent and approval is consumed. See the [dated result](crawl-jobs-v2-m4-pc01-run-2026-09-23.md).

**Subsequent merge and review:** PR #12 merged at 20:24:56 UTC on September 23 as
`320bce31db07e25758015b7466342fc73300f739`, after the PC01 run. Its tree exactly
matches `6340401`. Step 6's [September 24 evidence review](crawl-jobs-v2-m4-pc01-evidence-review-2026-09-24.md)
accepts the scoped control and retains all negative cases as unrun. Draft/unmerged
wording below records the Step 4 publication-time state.

This post-CI record was kept local during PC01 approval/execution to preserve the
exact tested HEAD and is published in the later documentation/evidence checkpoint.
The [implementation plan](crawl-jobs-v2-plan.md) owns current status. No new
acceptance case or execution approval was created during Step 4.

## Published revision and scope

| Item | Identity |
|---|---|
| Published commit | `634040131b36e1cbbc2e251364dacbec2ae5dd01` |
| Branch | `feature/crawl-jobs-v2-bootstrap-acl`; local/remote identities match |
| Merged-main base | `9b6b8f9948d04b5dff4378f491a52638a1517254` / PR #11 |
| Exact staged/committed tree | `1ebb7168db71ad488de66da157ab85978a3b6d45` |
| Tested PR merge commit | `92e7964eb683ca901b2ae5e08c6e45599b541259` |
| PR | [#12](https://github.com/fullerkris/mifolyo/pull/12), **draft and unmerged** |
| Scoped publication | **48 intended files**; 372 unrelated pending-file fingerprints preserved |
| Frozen independent-review inventory | `dbe881b2c269535b633188986c6f3adbdac0226df23528290bc4554c93277113` |

PR #11 was found merged during preflight. Its `9b6b8f9` tree exactly matches
reviewed base `b4bda19`; the new branch was created from fetched main while
preserving all 414 prior pending files, index and status. All 81 reviewed source
hashes remained unchanged throughout build, publication and CI validation.

The exact staged export passed **95 harness tests (638.884 s)**, **21 script
tests (8.966 s)**, combined targeted M4 race checks (**213.182 s**), package vet,
strict Lua/bundle/digest checks and actionlint. The committed tree matches that
tested export. All 43 canonical source and normative identities remain unchanged.

The scoped staged secret scan found three generic-key matches. Each exact match
was independently verified as the existing public `cj2_retire_legacy_keys.lua`
SHA-256, repeated in the review/image inventories:
`702b096b843d80cd1c85186cf79fafa29c08766ea22c599af29877e1e8013fc7`.
There are no unresolved credential findings.

## Protected CI and retained evidence

- [Required Checks 35902232103](https://github.com/fullerkris/mifolyo/actions/runs/35902232103): **SUCCESS**.
- [Unit Tests 35902232181](https://github.com/fullerkris/mifolyo/actions/runs/35902232181): **SUCCESS**.

All fourteen required contexts were re-read from current branch protection and
individually confirmed SUCCESS on this revision. All eight V2 race shards and
the final required Spider aggregate passed; no required context or assertion
was removed, relaxed or bypassed.

The eight downloaded reports bind the tested merge commit above and match the
locally compiled inventory. The repository verifier confirms complete, disjoint
coverage of **475 roots: 474 passed; only `TestJobLuaNativeFactoryParity`
optionally skipped**. The four newly added Go test roots remain included.

The downloaded amd64 image report explicitly selects PC01. Independent checking
confirmed all four stopped-role admissions, all **62 source files / 15 recipes**,
source identities, Linux/amd64 image bindings, recompiled plan identity, resource
limits, exit/OOM checks and cleanup receipts. Init/executor memory peaks were
**48,001,024 / 48,037,888 bytes**. It records `redis_started=false` and
`execution_authorized=false`.

| Retained evidence | SHA-256 |
|---|---|
| CI gate | `8e1016298327f32c75fac38cb20e85c3e052f500237af28f749546d526c3e026` |
| Downloaded amd64 image report | `218efe7f7cc30c87127e2417cfbd04da7cbdbc624492d50c217dca3bfe3119a5` |
| Committed arm64 image report | `f828d6c01d919393879075c2d271c67fe68b68091c9e5a68db3d084b79ee2dd6` |

The private directory retains `ci-gate.json`, `ci-shards/`, `ci-image-amd64/`,
`ci-image-postcheck.json`, build/preparation records, exact-index verification
and scan triage:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-bootstrap-acl-prep-2026-09-23/
```

## Exact arm64 PC01 handoff

| Artifact / bound | Value |
|---|---|
| Case | `ledger-claim-release-v1` — refreshed PC01 |
| Commit | `634040131b36e1cbbc2e251364dacbec2ae5dd01` |
| Harness image | `sha256:51bc4896057b8015e1bb67ff7e57448ed6356449c98a6df9100c06693de8e265` |
| Redis 7.4.11 image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan | `0861036947cfbcce72c855a40e1b489ab572479021fbb417a59a719681b3d76c` |
| Recipe | `11be906770c6f8c8ebfceef60022f4b73720498cb7141e2e3fdf2f634d83fd8b` |
| Intended operator/platform | `fullerkris` / Linux/arm64 |
| Limits | One attempt, 300-second case, 30-second stages, separate 60-second cleanup |

[Committed preparation artifacts](evidence/m4-bootstrap-acl-image-prep-2026-09-23/README.md)
bind the reviewed source and local validation. The private
`execution-request.json` is deliberately **unapproved** (`approved=false`, expiry
zero); it is a handoff template, not authority.

Step 5 subsequently recorded approval
`3e034bd6f56cc595fd4d8cba566f2722a3428691f185cecfd59228dedde7a695`
and the separate execution decision, with live revalidation before the single
attempt. That approval is consumed. The initial Step 4 request template remains
unapproved and unchanged. PC01 PASS leaves the 13 negative cases and broader M4
matrix open. Merge and application/runtime activation remain separate gates.
