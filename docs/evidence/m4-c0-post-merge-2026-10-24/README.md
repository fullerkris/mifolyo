# C0 merged-main images and preflight synchronization — 2026-10-24

**Status:** completed. Both independent read-only reviewers return
**GO_FOR_CONTINUED_IMPLEMENTATION**, with no findings. Scope is exact hosted
static-image evidence and baseline synchronization. Native runtime
admission/calibration remains open.

[Final gate](final-gate.json):
`80a18b438747e0f51b687e5f3da1ccefb73a05e952cc7584e8aee7337536123b`.

## Exact revision and pull

- PR #32 merged on 2026-10-22 at 21:34:13 UTC as
  `e8a0a415ac2b370319cd57f4c5262ce616a536fb`.
- Merge tree `aaf57fc74e5799d1b5d21f58cce18747623a7676` matches final PR head
  `1e659441270e7287e052554616cc8b64f5546657`.
- Actual main CI: [Unit Tests](https://github.com/fullerkris/mifolyo/actions/runs/39858333318)
  20/20, [Required Checks](https://github.com/fullerkris/mifolyo/actions/runs/39858333367)
  3/3, [C0 Runtime Images](https://github.com/fullerkris/mifolyo/actions/runs/39858333396)
  2/2; all completed successfully on attempt 1.
- Preserved the 120-path preflight candidate before moving its dependent branch
  onto the squash-merged baseline. `git pull --ff-only origin main` then returned
  **Already up to date**; stash application was conflict-free.
- At restoration, 119 paths were byte-identical and the roadmap gained only six
  upstream Go-security lines. Current chronology updates are separate.
- Backup branch: `backup/c0-native-calibration-before-main-2026-10-24`;
  retained stash: `2475fda26223c43485b933089274476e497be3ab`.

[Synchronization proof](sync-verification.json),
[original candidate inventory](sync-before.json), and
[integrated test continuity](test-continuity.json) retain the exact bindings.

## Downloaded hosted images

| Architecture | Artifact ID | Original ZIP SHA-256 |
|---|---|---|
| ARM64 | `12461066588` | `7cac65afbed201004f1417def2a5c2ea28f1e0e8766ef8c8e1cf4e6d158e22f8` |
| x64 | `12459699018` | `6b30fdb8d9661fd08d9acd6eaecf0ecf81e45eff603a3e84c61778d8a28ee8e9` |

Both raw ZIPs match the exact-run GitHub metadata digests and sizes. All 169
extracted files are inventoried. Each hosted test report passes all 288 IDs and
binds the exact 55 merged package files and test runner. All 58 package/auxiliary
paths match the final reviewed C0-ARCH-04 inventory `1f0e82a5…`.

[Download inventory](download-integrity.json),
[image replay](image-verification.json),
[ARM64 hosted tests](hosted-tests-aarch64.json), and
[x64 hosted tests](hosted-tests-x86_64.json) contain the source-bound results.

The unchanged reviewed validators replay all six portable image configuration
IDs, layer digests and complete content records. An inert, narrowly scoped
whiteout-free layer reconstruction produces tar data for the validators; it does
not extract image paths onto the host or execute an engine. Native ELF, compiled
filter, target manifest, profiles, oracle sources/interpreter/extensions and
required library closure all bind to actual bytes. All 12 retained native/oracle
artifact files per architecture and six payload hashes match the prior reviewed
local image sets. Hosted image IDs and ZIP/archive hashes remain their own
builder-dependent identities.

All 122 recorded command logs verify; ten tool-owned inspection containers have
never-started observations, removal and absence evidence. Each image root is joined
to two matching host observations around export. Hosted Podman 4.9.3 records root
mode **0555 for all roles**. Oracle base-layer root headers are **0755**, as were
the earlier local builder's observed oracle roots; these are distinct observations
and retain their actual values. No layer-derived claim replaces the host-root
observation.

## Integrated preflight continuity

All 61 reviewed package/script paths remain byte-identical to `faf8d794…`.
Fresh full tests pass **318/318 under umasks 022 and 077**, preserving all 318
historical IDs and all 59 package hashes. The original 111 evidence exports,
failed checks, review reports and frozen inventories remain unchanged.

The initial snapshot-export helper referenced a nonexistent auxiliary path and
failed after its restoration checks. Its script, failure record and partial
snapshot remain retained. The corrected exporter takes the three auxiliary paths
from the reviewed inventory and verifies all 58 files. This was a verification
harness error, not a source or Git integration failure.

## Retention and next gate

The [security review](security/report.md) returns
**GO_FOR_CONTINUED_IMPLEMENTATION**, with zero findings. It independently verifies
all 428 frozen inputs before/after, all 169 ZIP members, 58 exact-main source blobs,
all six complete content records and 13,171 final image members. **18/18 targeted
controls pass** (two positives, sixteen expected rejections). Candidate/stash
preservation, 61 reviewed code/script hashes, root semantics and all ten recorded
never-started lifecycles also pass. Its [machine-readable result](security/result.json)
and exact probes/fixtures are retained unchanged.

The [correctness review](correctness/report.md) independently confirms all six
image replays, 428 frozen inputs, 329 additional historical/backup files and all
105 prior durable-image files. **26 negative controls and two root-boundary
controls pass**, with no findings. Its [result](correctness/result.json), original
failed zero-digest fixture probe, corrected probe and logs are preserved. The two
larger inert corruption archives remain in the durable review directory, bound by
[the fixture inventory](correctness/durable-fixtures.json) and reviewer manifest.

- Correctness result SHA-256:
  `f0ebb25eeaa6c827f94ef4c5a9da3eccd11b0585d594ef73a86b643d4f717b74`.
- Security result SHA-256:
  `45635c09d058540e56187d087a11ee84e9e5eb8539bde03c9faca4c313b7d659`.

- Exact downloads, metadata, replay tooling, reconstructed tar data and review
  packet: `out/c0-hosted-images-2026-10-24/`.
- Before-sync candidate, retained initial helper failure, corrected source
  snapshot and integrated test evidence: `out/c0-main-sync-2026-10-24/`.
- Frozen review-input manifest: `review-inputs.json`, SHA-256
  `d63b6e8d9b9bab7d0a883d2cbdaa59eb7a8de8050e56dae28b447a2e638a315d` (428 paths).

Next: resolve selected x64 host eligibility and runtime-adapter isolation/lifecycle,
provision/admit the collector
and exact images, and finish trial/timing/confinement/cleanup assembly. Native
ARM64 calibration remains deferred; accepted dual-architecture publication still
requires both variants' eventual native acceptance.
