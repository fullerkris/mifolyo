# C0 architecture-contract evidence — 2026-10-12

The [implementation checkpoint](../../crawl-jobs-v2-m4-c0-architecture-contracts-2026-10-12.md)
records the C0-ARCH-02 increment on merged baseline `ced6e1c`.
The [local manifest](local-manifest.json) binds 16 exact exports:

- [source-inventory.json](source-inventory.json): all 46 package hashes, 22 changed
  paths/one new test file, platform/source identities and fresh artifact hashes.
- [verification.json](verification.json): 229/229 tests under both umasks, all 202
  baseline IDs retained, builds/static joins and execution boundaries.
- [Final 022 results](final-tests-022.json) / [log](final-tests-022.log) and
  [final 077 results](final-tests-077.json) / [log](final-tests-077.log).
- [Initial migration results](migration-tests.json) / [log](migration-tests.log):
  two fixture-shape/version-response updates, preserved without relabelling the run.
- [Native build](native-build.json) / [log](native-build.log) and
  [oracle build](oracle-build.json) / [log](oracle-build.log): pinned ARM64
  artifact-only commands and exact source/output bindings.
- [Target manifest](target-manifest.json), [toolchain](native-toolchain.json) and
  [oracle manifest](oracle-artifacts.json): actual rebuilt version-2 outputs.
- [Merged-main CI gate](merged-main-ci-gate.json): the completed PR #25 baseline.

The local pending-review fields retain recording-time status. Independent review
verdicts are retained separately. The [security review](security-review.md) returns
GO_FOR_CONTINUED_IMPLEMENTATION with 229/229 tests under umask 077, 280 negative
refusals and 3,092 interpreted BPF cases. Its [manifest](security-manifest.json)
binds 14 exact exports. The [initial correctness review](correctness-review.md)
returns NEEDS_REMEDIATION for three reproduced architecture-binding gaps;
its [manifest](correctness-manifest.json) binds 14 exports. The
[remediation checkpoint](../../crawl-jobs-v2-m4-c0-architecture-remediation-2026-10-12.md)
owns the corrected 238-test snapshot and final re-review.

Raw binaries, the frozen snapshot and private
review outputs remain under `/tmp/opencode/m4-c0-architecture-contracts-2026-10-12/`.
X64 metadata support does not imply an implemented or accepted x64 runtime.
