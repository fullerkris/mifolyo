# Bootstrap/ACL Step 4 image preparation — 2026-09-23

Selected case: `ledger-claim-release-v1`, the refreshed PC01 control required by
the bootstrap/ACL package. The image checker covers all 15 recipes and the exact
62-file execution-image inventory.

These are preparation artifacts, not an execution approval or a real-Redis case
result. Exact source, image, artifact and validation identities are recorded below.

**PASS:** four stopped-role admissions, immutable arm64 image/source checks,
all 15 recipe/request checks, bounded memory, isolation and cleanup. No Redis
server was started. Separate inspection confirmed the four metadata containers
and two volumes absent.

| Artifact | SHA-256 |
|---|---|
| `inputs.json` | `5815580acf73766e8e0712488b605d52ffa7ccafb88dbb96d09c595691458788` |
| `plan.json` | `0861036947cfbcce72c855a40e1b489ab572479021fbb417a59a719681b3d76c` |
| `recipe.json` | `11be906770c6f8c8ebfceef60022f4b73720498cb7141e2e3fdf2f634d83fd8b` |
| `image-validation.json` | `f828d6c01d919393879075c2d271c67fe68b68091c9e5a68db3d084b79ee2dd6` |

Harness image: `sha256:51bc4896057b8015e1bb67ff7e57448ed6356449c98a6df9100c06693de8e265`.
Redis 7.4.11 image: `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c`.
Reviewed 81-file inventory: `dbe881b2c269535b633188986c6f3adbdac0226df23528290bc4554c93277113`.

See the [dated preparation report](../../crawl-jobs-v2-m4-bootstrap-acl-image-preparation-2026-09-23.md)
for measured scope and remaining publication/CI/execution gates. Private originals
and cleanup postcheck remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-bootstrap-acl-prep-2026-09-23/`.
