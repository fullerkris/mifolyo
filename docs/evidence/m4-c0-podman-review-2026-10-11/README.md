# C0 Podman ARM64 reconstruction/review evidence — 2026-10-11

The [project report](../../crawl-jobs-v2-m4-c0-podman-review-2026-10-11.md)
records the owner-approved rootless Podman/QEMU reconstruction of the corrected
45-file pinned-SSH package and the independent correctness/security review.

The [artifact manifest](artifact-manifest.json) binds 13 exact reconstruction exports:

- [source-inventory.json](source-inventory.json): unchanged corrected 45-file source map.
- [preflight.json](preflight.json): rootless builder, emulator and host provenance.
- [Native base retrieval](pull-native.json) and [oracle base retrieval](pull-oracle.json):
  exact pinned ARM64 image/platform identities.
- [Native build](build-native.json), [oracle build](build-oracle.json),
  [native log](native-build.log) and [oracle log](oracle-build.log): exact artifact-only
  commands, timings, output image identities and source continuity.
- [artifact-verification.json](artifact-verification.json): all 11 native outputs
  and oracle manifest/interpreter match preceding hashes; actual static joins pass.
- [oracle-extraction.json](oracle-extraction.json): interpreter extraction from a
  uniquely owned never-started container, with removal and independent absence check.
- [Native toolchain](native-toolchain.json), [target manifest](target-manifest.json)
  and [oracle manifest](oracle-artifacts.json): rebuilt exact-byte artifact records.

The artifact manifest's pending-review flag is its recording-time state. The
[initial-review manifest](initial-review-manifest.json) binds 12 subsequent exact
exports: correctness/security reports, full-suite reports and focused probe/static
artifact evidence. Correctness returns NEEDS_REMEDIATION for COR-SSH-002, with
COR-SSH-003 as a medium follow-up. Security returns scoped GO and retains its
196-pass/one-umask-error full run plus the isolated unchanged-test pass.

The [remediation checkpoint](../../crawl-jobs-v2-m4-c0-ssh-remediation-2026-10-11.md)
owns the corrected-source checks and final re-review. Raw ARM64 binaries, generated filter/
profile/header files, the frozen review snapshot and private review outputs remain
under `/tmp/opencode/m4-c0-podman-arm64-2026-10-11/`.

Construction/static verification does not establish native runtime acceptance.
The x64 C0 runtime is planned under C0-ARCH-02–06; the current source and rebuilt
artifacts remain ARM64-specific.
