# Init-boundary correction: candidate image evidence

This package follows the failed `ledger-smoke-v1` attempt
`6c963c07b5526561459830bac338b997`. The old attempt/approval are preserved and
must not be reused.

The diagnosis was reproduced using create/inspect/remove only: Docker emitted
`HostConfig.CapAdd=["CAP_CHOWN"]` for requested `CHOWN`. The old exact-string
comparison rejected it. The corrected verifier accepts only the two equivalent
single-capability spellings for init; all other roles still have no added caps.
The real captured projection is retained as
`tests/crawl-jobs-v2-redis/testdata/created-init-docker-29.json`.

Candidate Linux/arm64 image IDs:

- Harness: `sha256:2059066be4f192b84d4932050d1f811ed2bacf275e7d7cc0179f3e709d50e12c`.
- Redis: `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c`.
- Python base platform manifest:
  `docker.io/library/python@sha256:ad4c34ff79289506e235b40dce75d629e25f226b597a2455804220f037e07531`.

The image-preparation report now includes actual stopped-container admission for
init, executor, Redis and revocation roles. Those containers are never started.
Only the existing isolated Python checks and `redis-server --version` are run;
no Redis server or acceptance case starts in image preparation.

The emitted plan/recipe remain non-executable. Publication, protected CI and a
new owner approval must bind these revised artifacts before another attempt.

## Verified artifact hashes

| File | SHA-256 |
|---|---|
| `inputs.json` | `d953d1d99b4ccdce7261808f9d77f5f3c17dd231a50ffe18bd959f51240f0bae` |
| `plan.json` | `d06a4ef887125883ecb1f0924f9192f4070bdb01c003d17ab718ca4baa451fbb` |
| `recipe.json` | `9b0adc08f054775c24922a84012d2c4dbd950f630e151226536bd2e619ff81ab` |
| `image-validation.json` | `a7d25edcedd8249e409eab534c598af3408646d8d77386014201304ae8c6d7d4` |
| `prestart-observation.json` | `5480c1952d8051cde4400c80d92b01dfe074448381190d184634657ce6d8013d` |
| `fixed-prestart.json` | `7e2a8c360e5c4d7f9448dda6b069b710b4d44c49a2bb41a63aac5447c5ebd99b` |

The two diagnostic files are exact-byte copies of the coordinator's observations.
The first reproduces the old verifier failure; the second checks corrected
metadata admission using existing images without starting a container.
`image-validation.json` separately validates the newly rebuilt image and records
its own four-role metadata check. See the
[diagnosis/review report](../../crawl-jobs-v2-m4-init-fix-2026-09-22.md).
