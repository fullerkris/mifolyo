# Corrected C0-ARCH-04 image preparation — 2026-10-17

**Local image construction verified; first security re-review requires follow-up.** The
[local manifest](local-manifest.json) binds **156 exact exports**:

- [Corrected source inventory](source-inventory.json): 55 package files plus
  CLI, workflow and original runner; SHA-256 `14ac223d…`.
- [022 tests](tests-022.json) / [log](tests-022.log) and
  [077 tests](tests-077.json) / [log](tests-077.log): **283/283**, all prior 272 IDs retained.
- [Local verification](local-verification.json), per-platform `image-set.json`
  reports, six role content/inspection records, artifact manifests and all 61
  command logs per platform. Five never-started inspections were removed per set.
- [Corrected source patch](corrected-source.patch) and
  [durable delivery manifest](durable-retention.json).

The six portable images and supporting files are retained locally at
`out/c0-runtime-images-2026-10-17/14ac223d10a72/` (Git-ignored, 103 files).
No C0 actor, live context collector or native calibration ran. The new native
ARM64/x64 hosted image workflow awaits publication. The
[initial review](../m4-c0-runtime-images-2026-10-17/README.md) retains all original findings.

The [first security re-review](first-security-review.md) closes SEC-01–03 and
revalidates all six retained images, but identifies SEC-04 (hardlink-to-symlink
source resolution) and SEC-05 (normalized root aliases). Its
[manifest](first-security-manifest.json) binds 22 exact exports. Candidate guards
were initially tested in memory only. The
[first correctness re-review](first-correctness-review.md) additionally requires
execute access for required ELF loaders (remaining COR-004) and identifies the
same root-alias issue (COR-006); its [manifest](first-correctness-manifest.json)
binds 28 exports. Both original reports remain unchanged. The
[final validator follow-up](../m4-c0-runtime-images-final-2026-10-17/README.md)
now passes 286 tests and revalidates these same images with unchanged content.
