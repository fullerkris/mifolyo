# Recovery case image preparation — 2026-09-27

**Preparation PASS.** The corrected, independently reviewed recovery lifecycle
has a new immutable Linux/arm64 image and case-specific artifacts. Publication/CI
and new execution decisions are still required.

## Bound artifacts

| Item | Identity |
|---|---|
| Case | `ledger-worker-death-pre-io-v1` |
| Scenario | `ledger-worker-death-pre-io` |
| Corrected source review | `b5fe64eee5fded5bd3a946d7a58790617d67c95a0447138ba54a79c709aeb888` |
| Review verdicts | `06110c785a327a8015a8b755226a65f6f04155c2d9dde8b42dfe74abb0ca0008` |
| Harness / stand-in image | `sha256:b164fb8610949e2a31393b8897c2a4bfe620f636f24c636caa20b590acad8da2` |
| Redis 7.4.11 image | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan | `1e00625d41c9f4baa99936abd9a936b454b3e29f54c7ec181e95932d0041acd8` |
| Recipe | `ac0d4fb95391062e271c26d6c8bf3d70d3994470b3273fea6b78751bbe6da987` |
| Image validation | `e6301a832e83ee414eaafb034aad208e105bf7d66264d14282d5d1825a695f39` |
| Independent image postcheck | `ef313e8df650fd855a1ec4d44bb5b68e9e8dd9bce3e8048833d44ebaf00631df` |

The image was built with `--network none`, `--pull=false` and the pinned Python
3.13.15 arm64 platform manifest
`sha256:ad4c34ff79289506e235b40dce75d629e25f226b597a2455804220f037e07531`,
whose local base ID is
`sha256:adc3d531c29fbd69fa9ca49cd8e241c68aaf4e1dd7462be6c2432044b7a39ea8`.
All 90 reviewed source hashes remained unchanged through build and validation.

## Observations

Explicit `--case ledger-worker-death-pre-io-v1` preparation admitted five roles
while stopped: init, executor, Redis, executor B and revocation. Each was checked
individually, preserving the intended sequential worker identities and bounded
control-volume attachment inventory. **Zero metadata-container starts** occurred.

Metadata fixture `a3d2a7d1ac7f54ac030e3282268c01a7` was fully removed. The
independent postcheck inspected all five exact container names and both volumes
absent, with empty case/image-check listings.

Separate networkless Redis-version and Python checks passed. Both Python roles
verified **66 exact image files, all 16 recipe digests**, canonical source
identities and the new closed isolation-receipt schema. Init/executor peaks were
**49,262,592 / 49,389,568 bytes**, below 128/256 MiB limits. These are image checks,
not maximum-shape Redis evidence or a claimed recovery measurement.

No acceptance Redis server started. The [exact preparation exports](evidence/m4-recovery-image-prep-2026-09-27/README.md)
retain `execution_authorized=false`; private originals and the postcheck script
are in
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-recovery-prep-2026-09-27/`.

## Intended bounded run

The [implementation/review record](crawl-jobs-v2-m4-recovery-review-2026-09-27.md)
defines the exact thirteen-operation sequence and process choreography. The first
worker must genuinely remain alive after its validated claim receipt, then be
SIGKILLed and removed before a distinct replacement starts. Real Redis TIME—not
host sleep alone—must establish the unchanged 60-second lease deadline.

The case checks early zero recovery, exactly one ready recovery and replay,
retained baseline/start history, B's new fence/reservation and replay, stale A
claim/release/renewal, valid B renewal/release/replay, final drained recovery,
38 counters and 46 direct authority denials. A's expired and B's cancelled
reservations keep exact one-day absolute tombstone deadlines.

Bounds remain 300 seconds/case, 30 seconds/individual stage, 60 seconds/cleanup,
128/256/528 MiB role limits and 400 MiB Redis maxmemory. A separately declared
one-second host-receipt-to-container-stop bound and five-second attached-command
reconciliation bound apply. Five container identities and two volumes require
teardown; all six role credentials require ordinary reachable-server revocation.
The completed external journal must exactly match the report's action sequence.

Next: publish the reviewed source, pass protected exact-revision CI, and obtain
fresh case-specific approval plus a separate execution decision. No old approval,
prior harness image or CI result can substitute for those bindings. Full M4 and
the observer/AOF-failure design gates remain open.
