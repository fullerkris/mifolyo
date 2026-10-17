# C0 x64 runtime port evidence — 2026-10-12

The [implementation report](../../crawl-jobs-v2-m4-c0-x64-runtime-2026-10-12.md)
describes the C0-ARCH-03 source/static-artifact increment. The
[local manifest](local-manifest.json) binds 30 exact exports:

- [Source inventory](source-inventory.json): 51 package files and original runner.
- [Local verification](local-verification.json): source/test/build joins, previous
  test-ID replacements, artifact hashes and source-bundle identities.
- [022 tests](tests-022.json) / [log](tests-022.log) and
  [077 tests](tests-077.json) / [log](tests-077.log): 256/256 passing in each run.
- [ARM64 build](aarch64-build.json) / [log](aarch64-build.log),
  [x64 build](x86_64-build.json) / [log](x86_64-build.log), and each platform's
  exact target manifest, compiler/source record and three OCI profiles.
- [Oracle probes](oracle-probes.json): rebuilt ARM64 oracle manifest and a
  separately identified x64 **host interpreter** probe. The latter is not an
  x64 runtime-image build or admission result.
- [Initial tests](initial-tests.json) / [log](initial-tests.log),
  [intermediate tests](tests-pass1.json) / [log](tests-pass1.log), and the
  [initial x64 build](initial-x64-build.json) / [log](initial-x64-build.log)
  preserve the superseded expectations, simulation fixture update, missing
  benign handshake frames and ARM-only header conflict encountered during the port.

Native ELF outputs and the frozen source are retained privately at
`/tmp/opencode/m4-c0-x64-runtime-2026-10-12/`.

Both independent reviews are complete with **GO_FOR_CONTINUED_IMPLEMENTATION**:

- [Correctness report](correctness-review.md) / [JSON](correctness-review.json):
  independent 256-test pass, actual instruction/artifact joins, 3,703 additional
  simulated helper controls, 26,232 ptrace vectors per layer including offline
  compiled OCI filters, and adversarial architecture/ELF/context controls.
  Its [manifest](correctness-manifest.json) binds 32 exact exports.
- [Security report](security-review.md) / [JSON](security-review.json): independent
  256-test pass, 31 x64 guard replays, 158,796 compiled-filter vectors and 342
  adversarial refusals. Its [manifest](security-manifest.json) binds 32 exports.

The [final gate](final-gate.json) binds the unchanged source to both verdicts;
the [final manifest](manifest.json) covers **98 exact exports**. Original failures
and intermediate pending fields preserve their recording-time state. Full large
disassemblies/reference sources stay in the private review directories, bound by
the reviewer reports. Compilation, static inspection, offline OCI filter export
and simulated C probes are not native runtime calibration.
