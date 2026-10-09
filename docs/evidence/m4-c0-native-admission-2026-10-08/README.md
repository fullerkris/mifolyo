# C0 native admission implementation evidence — 2026-10-08

**Locally implemented and verified; independent source review pending.** The
25-file package passes 99 tests without skips. Native artifacts compile under the
pinned ARM64 toolchain; native actors and tracing were not executed.

The [manifest](manifest.json) binds six byte-identical records:

- [Final source inventory](source-inventory.json): three added / eleven changed
  package files, nine native build inputs and eleven resulting artifacts.
- [Verification](verification.json): final 99-test command/log hashes, preservation
  checks and sixteen compiled-C-format / Python-wire compatibility checks.
- [Implementation disposition](implementation.json): completed implementation scope
  and explicit remaining integration/review boundaries.
- [Build](build.json): networkless, pull-disabled artifact compilation, logs and
  its recording-time package snapshot.
- [Target manifest](target-manifest.json): exact rebuilt ELF/instruction joins.
- [Toolchain](toolchain.json): compiler/library identities and actual build inputs.

The final source inventory is authoritative for the Python/test/README bytes.
The build's earlier full package snapshot predates final Python/test/README edits;
its nine compiler inputs match the final source. The initial 96-test version and
snapshot are retained privately. Self-review added pre-dispatch process-generation
anchoring and live-ready transport checks; the final snapshot has 99 tests.
These exports copy the final records exactly. Older reviewed artifacts remain in
their original preparation workspace.

All new tests use synthetic frames/fake Docker and benign Python process fixtures.
The static wire checks exercise extracted format templates, not the collector's
kernel reads. Missing kernel observations refuse admission, and the outer trusted
kernel/clock context, oracle worker and full trial/image/approval assembly remain
required before an executable calibration packet can be accepted.

Private originals, logs, source snapshots and revised ELF files remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-c0-runtime-admission-2026-10-07/`.
The workspace retained its initial date; these final implementation records are
dated October 8.

See the [implementation report](../../crawl-jobs-v2-m4-c0-native-admission-2026-10-08.md)
and [baseline reconciliation](../m4-c0-reconciliation-2026-10-07/README.md).
