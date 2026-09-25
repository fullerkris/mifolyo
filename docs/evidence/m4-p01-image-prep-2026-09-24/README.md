# P01 image/artifact preparation — 2026-09-24

**Preparation PASS** for `ledger-candidate-compat-present-v1` on Linux/arm64,
binding published commit `963b67b73e2cd67ff73f559fa9984813b9d5946a`.
These are P01-selected artifacts, generated with the explicit case selector.
Preparation does not authorize or execute the acceptance case.

| Artifact | SHA-256 |
|---|---|
| `inputs.json` | `46a7b72b4afb9e9ef2e9136b6e3ec99b86206608f7f8fef0c3a19c8f9231bdaa` |
| `plan.json` | `503f6ae40e0bd2ce7e833b90eb8c9d62b48d391972b508fb06b243ae0b33d79c` |
| `recipe.json` | `7123ed770cb9c900319f412356652e7cb27eef6c3f2704818d3dee402fa4ee74` |
| `image-validation.json` | `b66b1d27ef92935dc12b22a91a57b109297b734cf64c167ee98e362bad7affd1` |
| `image-postcheck.json` | `9c110f02336b56db345dadbbde4b947859fe1394cb3a8c666c2edeba0dd99e4b` |

Four metadata-only containers remained stopped. All four containers and two
volumes were subsequently inspected absent; image-check container listings were
also empty. Init/executor checks verified all 62 image files and all 15 recipes,
with observed memory peaks 48,861,184 / 48,943,104 bytes. All 81 independently
reviewed source files remain unchanged.

Private originals and verification helpers:
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-p01-execution-2026-09-24/`.
The four generated artifacts are under `preparation/`; the independent postcheck
is `image-postcheck.json`. The public copies preserve the exact original bytes.

See the [dated preparation record](../../crawl-jobs-v2-m4-p01-preparation-2026-09-24.md)
for source/CI/image identity, scope and the subsequent approval gate.
