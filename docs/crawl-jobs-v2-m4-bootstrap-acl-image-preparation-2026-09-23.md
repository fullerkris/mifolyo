# Bootstrap/ACL Step 4 image preparation — 2026-09-23

**Local arm64 image gate: PASS.** All 81 reviewed source hashes remain unchanged.
The prepared case is **PC01 / `ledger-claim-release-v1`**, the refreshed positive
control required before accepting the new negative-case observations. The image
validator checks all 15 recipes; emitted plan/recipe artifacts select PC01 exactly.

## Reviewed source and branch

- The [Step 3 independent reviews](crawl-jobs-v2-m4-bootstrap-acl-review-2026-09-23.md)
  are correctness GO and security GO for image/CI preparation.
- Frozen inventory: `dbe881b2c269535b633188986c6f3adbdac0226df23528290bc4554c93277113`.
- PR #11 was found merged as `9b6b8f9948d04b5dff4378f491a52638a1517254`
  on September 23 at 16:00:17 UTC. Its tree is byte-identical to reviewed base
  `b4bda19`: `784afcea527f2bac4ae140837195fca0f87b4f02`.
- New branch `feature/crawl-jobs-v2-bootstrap-acl` starts directly from that
  freshly fetched merged main. Before/after fingerprints matched all **414
  pending files** (24 tracked changes, 390 untracked files), index and worktree
  status. Prior evidence and unrelated user work were preserved.
- Docker Engine 29.5.2 / Docker Desktop 4.75.0, Linux/arm64. Build network disabled;
  immutable cached Python 3.13.15 platform manifest; explicit file-only allowlist.

## Exact local artifacts

| Artifact | Identity |
|---|---|
| Python platform manifest | `docker.io/library/python@sha256:ad4c34ff79289506e235b40dce75d629e25f226b597a2455804220f037e07531` |
| Python daemon-local base | `sha256:adc3d531c29fbd69fa9ca49cd8e241c68aaf4e1dd7462be6c2432044b7a39ea8` |
| Reviewed harness image | `sha256:51bc4896057b8015e1bb67ff7e57448ed6356449c98a6df9100c06693de8e265` |
| Redis 7.4.11 image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Inputs SHA-256 | `5815580acf73766e8e0712488b605d52ffa7ccafb88dbb96d09c595691458788` |
| PC01 plan SHA-256 | `0861036947cfbcce72c855a40e1b489ab572479021fbb417a59a719681b3d76c` |
| PC01 recipe SHA-256 | `11be906770c6f8c8ebfceef60022f4b73720498cb7141e2e3fdf2f634d83fd8b` |
| Image-validation report SHA-256 | `f828d6c01d919393879075c2d271c67fe68b68091c9e5a68db3d084b79ee2dd6` |

The local tag `mifolyo-cj2-m4:bootstrap-acl-reviewed-dbe881b` is only a convenience;
the plan binds the immutable ID. The [artifact package](evidence/m4-bootstrap-acl-image-prep-2026-09-23/README.md)
contains exact-byte exports of the four generated artifacts.

## Observed checks

Metadata fixture `eb9619e6ddc3390520ca4ac31f82b41e` passed init, executor, Redis
and revocation admission. All remained created/PID-zero with **zero metadata
container starts**. Exact commands, image-bound environments, capability/user/
mount/resource restrictions and same-case attachment checks passed.

Separate networkless containers invoked Redis `--version` and Python image
validation only:

| Check | Outcome | Peak / limit |
|---|---|---|
| Redis version | PASS: 7.4.11, exit 0, no OOM, cleanup verified | 268,435,456-byte limit |
| Init image validation | PASS: exact 62-file/source inventory, all 15 recipes and request schemas, network/capability/process checks | 48,926,720 / 134,217,728 bytes |
| Executor image validation | PASS: same source/recipe inventory and isolation checks | 48,852,992 / 268,435,456 bytes |

Both Python image-validation processes were PID 1 with zero external routes;
init effective capability was CHOWN only, executor capabilities zero. The runtime
holder-plus-exec process arrangement is still a later actual-case observation.

All four metadata containers and two volumes were removed and separately
inspected absent. Fixture-filtered container/volume listings and the image-check
container listing were empty. The version/image-check containers each exited
successfully, were not OOM-killed, and had verified cleanup.

## Publication scope and remaining gates

The owner requested Step 4, including image preparation, the scoped checkpoint,
new draft PR and protected CI. Publication includes the reviewed bootstrap/ACL
implementation, tests, planning/review/image records and previously local claim
PASS/CI/status evidence. Unrelated agent, seed, client and Backlinks Processor
work is excluded. All required contexts and exhaustive race coverage remain
enabled; the CI preparer already explicitly selects `ledger-claim-release-v1`,
matching PC01, while its image checker validates all 15 recipes.

Exact-index verification, scoped secret scanning, publication identity and new
protected results are recorded in the [primary plan](crawl-jobs-v2-plan.md) as
they complete. Preparation generated no execution approval and started no Redis
server or acceptance case. Actual PC01/new-negative behavior, ACL parsing,
durability and stage timing require fresh exact-artifact approval and a separate
execution decision after the remaining gates.

Private build/preparation, branch-preservation and independent cleanup receipts:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-bootstrap-acl-prep-2026-09-23/
```
