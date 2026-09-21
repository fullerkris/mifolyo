# M4 immutable image-preparation evidence

This directory records the first `ledger-smoke-v1` image-preparation artifacts.
It is test evidence, not a release bundle or execution approval. `plan.json`
remains explicitly non-executable; approval is a separate owner decision after
exact-revision CI and review.

Selected Linux/arm64 images:

- Python 3.13.15 base index:
  `docker.io/library/python@sha256:2325bb286ec344af3e5898cc224b5844e2707ac6e26b1632516fd3edc84a5e26`.
- Exact arm64 Python platform manifest used in the local build:
  `docker.io/library/python@sha256:ad4c34ff79289506e235b40dce75d629e25f226b597a2455804220f037e07531`.
- Redis 7.4.11 platform manifest:
  `docker.io/library/redis@sha256:cd953e4e9b4725f0d87a2b170c3d313ad641be5370033b46262e07f8010788a3`.
- Harness daemon-local immutable image ID:
  `sha256:4b0ca8a3cca08646616bfe6952fc2f2423cb3bd55496ee5e0a1312be223f480a`.
- Redis daemon-local immutable image ID:
  `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c`.

`image-validation.json` records actual isolated version/content/memory checks,
their complete 55-file content hashes, and cleanup outcomes. Redis was invoked
only with `--version`; no Redis server, acceptance fixture or retained datastore
operation was started. Python checks used network mode `none`, read-only roots,
bounded memory/PIDs/CPU and the intended root-CHOWN/non-root capability sets.

The initial broad-context candidate was rejected and deleted. File-only Docker
ignore exceptions now admit exactly the intended 55 files. Target Linux's known
DOWN fallback tunnel devices are explicitly checked alongside both routing tables
and capabilities. Streaming digest construction preserves all existing contract
pins while reducing measured init memory from approximately 127 MiB to 47 MiB.

Reproduction (fresh output directory required):

```bash
python3 -B scripts/prepare-crawl-jobs-v2-images.py \
  --harness-image sha256:4b0ca8a3cca08646616bfe6952fc2f2423cb3bd55496ee5e0a1312be223f480a \
  --redis-image sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c \
  --architecture arm64 --output-dir /absolute/path/to/fresh-evidence-directory
```

The current mutable gate status belongs to `docs/crawl-jobs-v2-plan.md`.
