# C0 x64 native/context/oracle port — 2026-10-12

**Status: source/static-artifact implementation and independent review complete;
both reviewers return GO_FOR_CONTINUED_IMPLEMENTATION, with no blockers.** Branch
`feature/crawl-jobs-v2-c0-x64-runtime` starts at PR #26 owner merge
`d8016469a5ffcceeb04329deec4e3dea2c72dd97`. That merge preserves the reviewed
architecture-contract tree `f5d215c7b0e0656649364d315bcecf87e213b92a`;
its own [main CI is verified](evidence/m4-c0-architecture-merge-2026-10-12/README.md):
14 required contexts / 23 jobs passed on attempt 1, with exact 238-test / 47-file
C0, race and image evidence. Main gate:
`e11f449a2483677e3b7642e97df12a4b65cc615af9f263ccbd4e69b6e2aaa111`.

## Implementation

| Boundary | C0-ARCH-03 implementation |
|---|---|
| Native ABI | Compile-time Linux little-endian LP64 checks, canonical `aarch64`/`x86_64` identities and target-manifest/observer machine agreement |
| X64 effects | SysV argument registers, three ordered 64-bit stores, explicit pre boundary and post `ret`, separate VM/send `nop; ret` markers |
| Static decoder | Exact 14-byte x64 effect/function size and symbol boundaries; exact marker functions; variable instruction widths; aligned writable witness symbol |
| Debug state | Fixed TID 1, inactive-state admission, DR0 local execution breakpoint, DR6 reason validation, readback after arm/restore, RIP/CS/siginfo checks and step advancement |
| Seccomp | Native x64 syscall numbers; foreign/x32/compat syscall rejection; exact full-width debug-register offsets; only DR0, reset DR6 and DR7=0/1 writes; no GP/register-set or target-memory writes |
| OCI profiles | Architecture-specific x64 TLS/readlink and target chmod wrappers with the existing default-deny role restrictions; ARM64 bytes preserved |
| Context/oracle | Both runtime ABIs enabled; observed Linux LP64 oracle selection and actual interpreter ELF join; independently provisioned collector architecture remains required |
| Builds | Independently pinned GCC 14.3.0/bookworm compiler stages selected with explicit `C0_ARCHITECTURE`; artifact-only builds for each platform |

The version-2 architecture joins, fixed 276-case registry, three-line
case/nonce/Linux-expiry handshake and control-frame format are preserved. The
oracle Dockerfile still uses the existing ARM64 Python base. C0-ARCH-04 supplies
the separately pinned x64 Python/runtime image set and image admission evidence.

## Verification

- **51 source files; 256/256 offline tests under umasks 022 and 077**, with no
  skipped, failed, errored or expected-failure cases. All 233 other previous
  IDs remain (229 test-method ASTs unchanged; four update x64 expectations).
  Five tests asserting x64 was unimplemented have explicit replacements;
  18 new methods cover x64 filter, ELF, context and benign-process protocol behavior.
- **Both native artifact builds pass** from the same frozen package. Each exports
  eleven files with architecture-specific target/observer ELF, marker manifest,
  BPF, OCI profiles and toolchain/source record.
- ARM64's three OCI profiles, BPF, generated filter header and case header remain
  byte-identical to C0-ARCH-02. Native binaries/manifests have new source identities.
- **29 offline C debug-state controls pass**. The production helper is exercised
  with an in-memory request callback; this probe never calls ptrace.
- The rebuilt ARM64 oracle manifest passes. The x64 manifest builder also passes
  against the recorded workstation interpreter (Python 3.14.7); that probe is
  explicitly separate from the future x64 runtime image.
- The initial failed tests and x64 compiler/header conflict remain retained
  unchanged in the [evidence directory](evidence/m4-c0-x64-runtime-2026-10-12/README.md).

| Identity | SHA-256 |
|---|---|
| Frozen source inventory | `7ed3b955be1fe4d2621a43896d1a3e4d49cf9edcf6e86a0c2dd567d105f14720` |
| Local verification | `942e0fda99fff4b3418af90a6aa929a802ffdee1f8908d5962ab32017c99bd49` |
| 30-export local manifest | `91dcd971f61bd2505d4f695ed467da0e4d997e9d2aa456994bdc5d701cd3ef33` |
| New host-context source bundle | `1710b96326062a711ed549e1172440e53c4ef1b3a760a45524dbce10d110bb32` |
| New oracle worker source bundle | `ddbe957a80cbe1a490c5536719dd080be0793d53ea511f1e72e430905e5c95ed` |
| Correctness review | `cc905968d584a9db2889e0e5cc7700804772b58d83ca17c1c92d182e5d6303ca` |
| Security review | `0303b1fb74b96edcaf8c2a5aa2fcdf3751fc870ee8fa8b8afa6c93c0e140ba7d` |
| Final source/static gate | `25711d4fc2c428bd3f1cc77ee658b308c258015b4f233a3fea15987d0d6f7de5` |
| 98-export final manifest | `3fc07915ba1fd99b85453b2ad991baf495786e844d55bb15a1f0787652d67cd9` |

Dependent preparation/deployment/anchor/manifest bindings must be regenerated.
Existing dated source/review records retain their original identities.

## Review and next steps

Both independent read-only reviewers return **GO_FOR_CONTINUED_IMPLEMENTATION**
on the unchanged 51-file source inventory, with no findings requiring remediation.

- [Correctness](evidence/m4-c0-x64-runtime-2026-10-12/correctness-review.md):
  independent 256-test pass, 144 real-ELF marker bit mutations rejected, **3,703
  additional simulated native-helper controls**, LP64 compile guards, both actual
  ELF/filter joins, **26,232 ptrace vectors per layer**, and offline libseccomp
  2.6.1 compilation/export of the OCI profiles. No filter was installed.
- [Security](evidence/m4-c0-x64-runtime-2026-10-12/security-review.md):
  independent 256-test pass under umask 077, **31 x64 guard replays**, **158,796
  compiled-self-filter vectors**, 156,701 interpreted OCI ptrace vectors and
  **342 adversarial refusals**. Both reviewers confirm ARM64 confinement-byte
  continuity and source/artifact integrity before/after review.

The [final gate](evidence/m4-c0-x64-runtime-2026-10-12/final-gate.json) joins both
decisions, tests, artifact checks and verified main baseline. The 98-export
manifest preserves exact reviewer outputs, probe sources and earlier failed
checks. The correctness review's ASan/shared-UBSan link failure is retained;
the simulated helper passed UBSan trap mode. Full large disassemblies and
reference-source copies remain private, hash-bound by that review's report.

The scoped publication/CI step is next, preceding C0-ARCH-04's
dual-runtime image set. Native host/backend admission and hardware-breakpoint,
confinement, timing and cleanup calibration follow per platform in C0-ARCH-05.
This checkpoint is local/uncommitted; full M4 acceptance remains open.
