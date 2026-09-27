# Recovery image/artifact preparation — 2026-09-27

**Preparation PASS** for `ledger-worker-death-pre-io-v1` on Linux/arm64.
The explicit case selector produced these exact artifacts:

| Artifact | SHA-256 |
|---|---|
| `inputs.json` | `29cb43828c53995f32ba1e7ee34b4251f8f774865c7dd592aad17f91281b2af7` |
| `plan.json` | `1e00625d41c9f4baa99936abd9a936b454b3e29f54c7ec181e95932d0041acd8` |
| `recipe.json` | `ac0d4fb95391062e271c26d6c8bf3d70d3994470b3273fea6b78751bbe6da987` |
| `image-validation.json` | `e6301a832e83ee414eaafb034aad208e105bf7d66264d14282d5d1825a695f39` |
| `image-postcheck.json` | `ef313e8df650fd855a1ec4d44bb5b68e9e8dd9bce3e8048833d44ebaf00631df` |

Harness image:
`sha256:b164fb8610949e2a31393b8897c2a4bfe620f636f24c636caa20b590acad8da2`.
Redis 7.4.11 image:
`sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c`.

The build used the pinned Python arm64 manifest with disabled build networking.
Five role containers were admitted **while stopped**, individually rather than
as five simultaneous control-volume attachments. All five exact containers and
both volumes were independently inspected absent. The metadata fixture was
`a3d2a7d1ac7f54ac030e3282268c01a7`.

Separate networkless image checks verified all **66 files / 16 recipes**, exact
source identities and closed isolation receipts. Init/executor memory peaks were
**49,262,592 / 49,389,568 bytes**. These are image-validation observations, not
maximum-shape Redis measurements or a worker-death execution result.

Private originals and postcheck:
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-recovery-prep-2026-09-27/`.
The four generated artifacts are in `arm64/`; the independent postcheck is at
the workspace root. Public copies preserve the exact bytes.

See the [preparation report](../../crawl-jobs-v2-m4-recovery-preparation-2026-09-27.md).
Publication/CI and fresh exact-artifact approval/separate execution decision
remain required. No new Redis acceptance server was started.
