# C0 architecture-bound contracts — 2026-10-12

**Initial checkpoint: local C0-ARCH-02 implementation verified; initial review
findings are closed by the subsequent independently reviewed remediation.**
Work is on `feature/crawl-jobs-v2-c0-architecture-contracts`, based on PR #25's
verified merge `ced6e1cff36101f76fb54904b6b0fa2cf33cb48f`. The **46-file package
passes 229 C0 tests under both umasks 022 and 077**, preserving all 202 baseline
IDs with 27 additions. Fresh ARM64 native/oracle artifact-only builds and static
source/ELF/manifest/filter/profile/platform joins pass. Changes remain uncommitted.

## Merged baseline

PR #25's approved tree `f0cbc978a882f0f5659a7cf542387a6e8d348d13` is preserved in
the actual merge. Main Required Checks `38276843818` and Unit Tests `38276843901`
passed on attempt 1: 14 required contexts / 23 jobs, 202 C0 / 31 script / 183
harness tests, all 480 race roots and 74-file / 20-recipe M4 image evidence.
Main gate: `d0803f350b07ecf037100c8033915b92433659b638574958931f007a08f23666`.
The [retained main gate](evidence/m4-c0-architecture-contracts-2026-10-12/merged-main-ci-gate.json)
records that baseline separately from this new source increment.

## Implemented contract

| Contract name | OCI platform | ELF | Seccomp / audit architecture |
|---|---|---|---|
| `aarch64` | `linux/arm64` | ELF64 LE, machine 183 | `SCMP_ARCH_AARCH64` / `0xc00000b7` |
| `x86_64` | `linux/amd64` | ELF64 LE, machine 62 | `SCMP_ARCH_X86_64` / `0xc000003e` |

Canonical descriptors and their digests bind these names; aliases and coercions
reject. Both names can describe preparation/deployment metadata. Runtime, profile,
filter, collector, process-stream, SSH dispatch and artifact-build entrypoints
remain ARM64-only until the separately planned x64 native port.

- **Version 2:** preparations, resource scopes, deployments/anchors, signed context
  exchange and all four native admission frames. Preparation/resource/artifact
  records bind the platform digest. Missing/legacy/unbound fields reject.
- **Cross-layer joins:** selected preparation, resource/stream binding, daemon,
  image, exact profile, host service/anchor/interpreter, signed collector context
  and native IDENTITY must agree on architecture.
- **Actual artifact checks:** build and runtime interpreter hashing checks ELF
  architecture; native parsing retains its full AArch64 static-PIE constraints.
  Header classification alone does not certify a complete ELF or runtime image.
- **Native contract:** compile-time AArch64/little-endian/64-bit guard and explicit
  architecture in version-2 IDENTITY. Control-message formats and the existing
  case/nonce/Linux-expiry handshake remain intact.
- **Preservation:** ARM64 profile and compiled-filter bytes are unchanged, along
  with the earlier cleanup/source-reuse corrections and admission bounds.

Old preparation/deployment/source bundles, resource snapshots and native/oracle
artifacts require regeneration. The new contract does not establish x64 runtime
support, Podman runtime-backend compatibility or native calibration acceptance.

## Verification

| Check | Result |
|---|---|
| Full C0 suite, umask 022 | **229/229 PASS** |
| Full C0 suite, umask 077 | **229/229 PASS** |
| Coverage | All 202 previous IDs retained; 27 new mismatch/legacy/unsupported-path and positive tests |
| Native artifact-only build | PASS; new target/observer ELFs and version-2 instruction/toolchain manifests |
| Oracle artifact-only build | PASS; new version-2 source/platform manifest, same pinned interpreter |
| Static joins | Actual ELF headers, target instruction manifest, embedded observer BPF, three profiles and source/platform identities PASS |
| Runtime execution | No C0 actor, oracle worker, real host collector, live SSH or native calibration |

The first migration run identified two fixture-only changes: explicit stream
architecture and the fake Docker startup adapter's version reply. Assertions were
retained. A subsequent negative test exposed the missing x64 refusal in direct
SSH transport dispatch; the guard is included in the final snapshot.

The oracle interpreter was extracted from a uniquely owned, never-started pinned
base container; removal and preservation of the prior container inventory were
verified. The owner approved separate read-only correctness and security reviewers
for the frozen final source and rebuilt artifacts.

## Identities and evidence

| Identity | SHA-256 |
|---|---|
| 46-file source inventory | `b5388bbdf282e286caec43f6a5e12bc5e0129a6630f43bd2c2b15b405b0d7bfb` |
| Local verification | `c4b6c596b76147d8ac3d2c3f959d728a0836e649b355a8b511439b6f906ed864` |
| Full-suite report, each umask | `c426dc3172d90a4592cfbf8549092ed17c11e79b2245589d65308838be884eaa` |
| Host-source bundle | `aa6db9a37d50c3ab91420de5a4ea55b42718484548811d26b7d668ce73165d0d` |
| Transport bundle | `72ca166c9680cf4e37142c83335995ba396deec4d978923f23b83bda2cc98001` |
| Oracle-worker bundle | `f55a4c825523fc12e6d5bc3f5efaa97c8266a1ef640e4454c912346d36d3a86a` |
| Target ELF | `07ae68522b7e87560bda4d32fcb0f244c74fd748f7cd9ccfe1ac3cf2321deab5` |
| Observer ELF | `53800d2281f57341269a0f1c9686809883241ed01872338b0544bf0439e88120` |
| Oracle manifest | `6168e220727cb6377e8e637740bb8c297bf7bee929ab19aac6db90aa6fbcbfa5` |

The [evidence index](evidence/m4-c0-architecture-contracts-2026-10-12/README.md) and
[local manifest](evidence/m4-c0-architecture-contracts-2026-10-12/local-manifest.json)
bind 16 exact exports; manifest identity
`c00650bcf1b5aec1af866cc369c4f65d52c041a73df3f845006295f03407bd86`.
Private source snapshot, binaries and reviewer outputs:
`/tmp/opencode/m4-c0-architecture-contracts-2026-10-12/`.

## Next gates

**Security review complete:** GO_FOR_CONTINUED_IMPLEMENTATION, 229/229 tests
independently passed under umask 077, 280 expected invalid-input refusals and
3,092 interpreted compiled-BPF cases. Source/artifact integrity and earlier fixes
were verified without drift. Review identity:
`9f7ef408e625e1d4401ac7f345a4a5926603fbe777818f997775b0438afbe364`.
The [security export manifest](evidence/m4-c0-architecture-contracts-2026-10-12/security-manifest.json)
binds 14 exact records. Correctness returned **NEEDS_REMEDIATION** after reproducing
COR-ARCH-001 (startup image check), COR-ARCH-002 (mutable anchor consistency) and
COR-ARCH-003 (record-mode target/manifest consistency). Its review identity is
`cfec175b78f6a21f47a6407ffec213a1d2ee09e64cf25941defc168a6796fbe4`, with
[14 exact exports](evidence/m4-c0-architecture-contracts-2026-10-12/correctness-manifest.json).
The [subsequent remediation](crawl-jobs-v2-m4-c0-architecture-remediation-2026-10-12.md)
closes all three findings and passes 238 tests under both umasks; both final
independent reviewers return GO. Initial verdicts retain their original scope.

1. Complete both independent reviews and remediate any findings.
2. Separate scoped publication/CI decision for the reviewed increment.
3. X64 native/context/oracle port and both runtime image sets (C0-ARCH-03–04),
   alongside native host/backend selection.
4. Per-platform installation, live context/OCI/native admission and calibration
   (C0-ARCH-05), accepted dual-architecture publication (C0-ARCH-06), and full M4.
