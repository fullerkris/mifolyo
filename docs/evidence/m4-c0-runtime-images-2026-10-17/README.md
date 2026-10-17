# Initial C0-ARCH-04 image preparation and review — 2026-10-17

**Historical initial snapshot: NEEDS_REMEDIATION.** The actual six pass5 images
passed static replay; the reviews identified broader CI and validator defects.
See the [implementation/remediation report](../../crawl-jobs-v2-m4-c0-runtime-images-2026-10-17.md).

- [Local manifest](local-manifest.json): **150 exact exports**, including 272-test
  runs under both umasks, both image sets, command logs, source/ELF/profile records
  and retained initial build failures.
- [Correctness review](initial-correctness-review.md) /
  [JSON](initial-correctness-review.json): COR-001–005, with 27 exports bound by its
  [manifest](initial-correctness-manifest.json).
- [Security review](initial-security-review.md) /
  [JSON](initial-security-review.json): SEC-01–03, with 23 exports bound by its
  [manifest](initial-security-manifest.json).
- [Initial source patch](initial-source.patch): reconstructs the 54-package /
  three-auxiliary-file snapshot on base `7ef2041`; inventory `f789846f…`.

Original failures, reports and source identities are unchanged. Corrected local
evidence is in the [successor directory](../m4-c0-runtime-images-remediation-2026-10-17/README.md).
