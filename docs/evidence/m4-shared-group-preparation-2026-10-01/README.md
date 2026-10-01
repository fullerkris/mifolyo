# Shared-group selected image evidence — 2026-10-01

**PASS for selected Linux/arm64 image preparation**, case
`ledger-shared-group-capacity-v1`. No acceptance Redis server or case was started.

The [manifest](manifest.json) binds six byte-identical exports:

- [Pinned-base build record](build.json).
- [Immutable inputs](inputs.json), [selected plan](plan.json) and [recipe](recipe.json).
- [Image/stopped-role validation](image-validation.json).
- [Independent binding and resource-absence postcheck](image-postcheck.json).

The harness/stand-in is `sha256:03bf14679ffdda67c7940ffca0cfd52d38fcf5b9d8e0ca4f173b3fbe2e76d84d`.
The plan is `526593999b3eefe48bf0492cb5221603d5475ab1686460944253c44668b0f4eb`;
the recipe is `05bfdbbea0bf5166b332e8e68225d466dcd1b076bf171fca30d062b946d88dc4`.

Both Python image checks match all **74 files / 19 recipes**. Four metadata roles
were admitted while stopped; all four exact container names and two volume names
were independently absent, with fixture/case/image-check label queries empty.
Image-check memory peaks are below their limits and are not workload benchmarks.

The manifest/index are derived; all six exports preserve original bytes. The
[107-file source/review gate](../m4-shared-group-lifecycle-2026-10-01/README.md)
and [earlier offline foundation](../m4-shared-group-capacity-2026-10-01/README.md)
remain separate historical records. No approval or release authority is granted
by this package.

See the [dated preparation report](../../crawl-jobs-v2-m4-shared-group-preparation-2026-10-01.md)
and [implementation plan](../../crawl-jobs-v2-plan.md). Scoped publication/CI and
fresh artifact approval/separate execution decision remain ahead. This batch is
local and uncommitted; full M4 remains open.
