# Claim/release immutable image preparation — 2026-09-23

This package retains the passing `ledger-claim-release-v1` image validation
and matching offline inputs, plan and recipe. Source identity is pinned by
`docs/evidence/m4-claim-review-2026-09-23/corrected.json`.

Preparation selects the claim case explicitly. It creates/inspects/removes four
stopped role specifications, then runs isolated Python image checks and Redis
`--version` only. It does not start a Redis server or acceptance case and does not
generate execution approval.

Pinned Linux/arm64 bases:

- Python 3.13.15 manifest:
  `docker.io/library/python@sha256:ad4c34ff79289506e235b40dce75d629e25f226b597a2455804220f037e07531`.
- Redis 7.4.11 daemon-local image:
  `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c`.

## Validated result

- Harness image: `sha256:b8de7cf09495bca22f6bba158bfdbe776b65ab726895e484682a3bb4f95a8a01`.
- Exact image inventory: 57 files, all matching the reviewed source bytes.
- All four stopped-role admissions passed; zero starts in that metadata phase.
- Python image checks exited 0 without OOM, with peaks of 48,193,536 bytes
  for init (128 MiB limit) and 48,386,048 for executor (256 MiB limit).
- Cleanup verified; separate label-filtered listings found no remaining resources.

| File | SHA-256 |
|---|---|
| `inputs.json` | `1494a1620f673a90ce3796290bf9f473833e657c7137b6416ed2db9d9a635613` |
| `plan.json` | `9833e6c25c74d9b0b80cc6370c5e6d38165cea2032f471aab879e146183dba41` |
| `recipe.json` | `07d27ddc8218d6c2aa5adf0f05227795a9f097802f50a534585e87206f8a2ad1` |
| `image-validation.json` | `6d356f63cb0e7254d34c22cd51ff46b9d629078d9dca7b1be6f59c4548f10917` |

The report declares `execution_authorized=false` and `redis_started=false`.
These artifacts are preparation evidence, not a real claim/release acceptance run.
