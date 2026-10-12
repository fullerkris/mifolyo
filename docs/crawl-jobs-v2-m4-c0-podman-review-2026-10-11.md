# C0 Podman ARM64 reconstruction and independent review — 2026-10-11

**Status: artifact reconstruction/static verification and initial independent
reviews complete; subsequent source remediation is independently reviewed GO.** The owner approved Podman/QEMU
artifact reconstruction and two separate read-only reviewers for the corrected
pinned-SSH snapshot. The owner also requested both ARM64 and x64 C0 runtimes in
the [implementation plan](crawl-jobs-v2-plan.md#dual-architecture-c0-runtime-plan--2026-10-11).

## Scope and source

The initial reconstructed/reviewed 45-file package is bound to source inventory
`f53058a955de01027efcdbaa6adace7d6904197c845e5886b25e7d9db41bc227`, including
the [COR-SSH-001 cleanup correction](crawl-jobs-v2-m4-c0-context-provisioning-review-2026-10-11.md).
Its retained 197-test result preserves all 194 preceding IDs. Package bytes were
verified before and after each pull/build and copied into a separate read-only
review snapshot. The branch remains `feature/crawl-jobs-v2-c0-context-provisioning`
at pushed `1e95696`, with the correction and successor documentation/evidence local.

## Build environment and procedure

- Linux/x86_64, local **rootless Podman 5.8.7**, **Buildah 1.43.4**,
  **QEMU AArch64 10.2.2** and enabled ARM64 `binfmt_misc` registration.
- Exact ARM64 GCC base:
  `docker.io/library/gcc@sha256:66035d353338cb93b64f621393dc6fecde85258651ca454f0cf36ff2639b1352`.
- Exact ARM64 Python base:
  `docker.io/library/python@sha256:2325bb286ec344af3e5898cc224b5844e2707ac6e26b1632516fd3edc84a5e26`.
- Bases were retrieved separately and their architecture/repository digests checked.
  Builds used `--platform linux/arm64 --pull=never --network none`, disabled proxy
  forwarding/cache/layers, and selected only `--target artifacts`.
- Each recipe supplied its explicit ignore file: `.dockerignore` for the native
  build, `Dockerfile.oracle.dockerignore` for the oracle build. Local output exports
  used fresh private directories.
- The oracle interpreter was copied from a uniquely labelled, **never-started**
  pinned-base container with a fixed `/bin/false` entrypoint. Created/PID-zero state
  was checked before/after copying; the container was removed and its absence
  verified. The pre-existing container inventory was preserved.

The compiler and manifest builders ran under emulation. Target, observer, oracle
worker and real host collector were not executed. This is artifact evidence;
native tracing, confinement, clock and timing acceptance remain later work.

## Reconstructed artifact verification

| Check | Result |
|---|---|
| Native artifact-only build | PASS, 10.934 seconds |
| Oracle artifact-only build | PASS, 1.717 seconds |
| Native outputs against preceding retained hashes | **11/11 exact matches**, including both ELFs, headers, instruction manifest, BPF, three profiles and toolchain record |
| Oracle manifest | Exact-byte match: `3f51809ebddbad24cbe03611e67b678df003121b77aff0743de0251baf538131` |
| Extracted oracle interpreter | ARM64 ELF; matches `cb4548d280f38c2b1df4cba45f12c90a034729e08fce5bf1a223c58add0e21e3` |
| Static joins | ARM64 ELF headers; target instruction manifest; actual observer-embedded BPF; all three profiles; toolchain/source/ELF and oracle source/interpreter bindings |

Reconstruction verification:
`569dfdc93881a44d18dc6b8b2d1bdfbc1a5a2453153257de0160751d86d09318`.
The [evidence index](evidence/m4-c0-podman-review-2026-10-11/README.md) and
[artifact manifest](evidence/m4-c0-podman-review-2026-10-11/artifact-manifest.json)
bind 13 exact reconstruction exports. Artifact-manifest identity:
`98f624acc84b16bcb72a77f925405497689362a82c2178903b35ee7249c8083b`.
The retained earlier [build evidence](evidence/m4-c0-context-provisioning-2026-10-11/build-verification.json)
keeps its original bytes. Rootless Podman reconstruction resolves the earlier
workstation artifact-availability blocker without changing any package source.

## Independent review

Both independent reviewers verified the frozen source and actual static artifacts.
Correctness returns **NEEDS_REMEDIATION**; security returns scoped
**GO_FOR_CONTINUED_IMPLEMENTATION**. The combined source gate requires remediation.

| Finding | Review result |
|---|---|
| COR-SSH-001 | Original selector-allocation leak independently reproduced and correction verified |
| COR-SSH-002 — high, blocking | First pipe-setup failure can reap an exited leader before killing its pipe-holding descendant |
| COR-SSH-003 — medium | Reused provisioned source omits deployment/transport preparation checks; normal one-shot backend path unaffected |
| QA-ENV-001 — informational | Simulated install-directory mode depends on the caller's umask |

Correctness ran **197/197 PASS** and checked 1,482 compiled-BPF cases. Security
ran **196 passes / one fixture error under umask 077**, followed by the unchanged
single-test pass under 022; its original full-suite report remains FAIL. Security
also passed 13 probe groups and 987 compiled-BPF vectors. Both reviews verified
all 11 native output hashes and oracle manifest/interpreter/source joins.

The [initial-review manifest](evidence/m4-c0-podman-review-2026-10-11/initial-review-manifest.json)
binds 12 exact review/test/probe exports, identity
`55865d4e88a1c5b06d84e58ab81e50a68f30d6befaf1d6b2391b5efda0cf855e`.
Correctness review: `ada46c4392db346166ad9aa8531baf3c3aaa293f4a42cced5b8ad9418fdf77f7`.
Security review: `e90fdea81bb4954e3d8d3835472112aa74c4e73c3f4a2381e2832fd8714810e8`.

The subsequent [source remediation](crawl-jobs-v2-m4-c0-ssh-remediation-2026-10-11.md)
closes all three follow-ups and passes **202 tests under both umasks**. Both final
independent reviewers return GO on its new source/transport identities; these
initial reports retain their original source binding and verdicts.

Private source snapshot, raw artifacts, logs, verification scripts and review
directories: `/tmp/opencode/m4-c0-podman-arm64-2026-10-11/`.

## Dual-architecture follow-up

The owner-requested **C0-ARCH-01–06** plan covers ARM64 and x64 target/observer/oracle
images with matching context-source support. The present implementation remains
ARM64-specific. The x64 work includes explicit architecture binding, instruction
markers/decoding, hardware debug registers, seccomp filters, platform observations,
pinned compiler/Python bases and native acceptance on each architecture.

Next: the separate scoped PR/publication decision for the reviewed remediation.
Runtime-image admission, actual host installation/context acquisition,
full trial assembly and native calibration remain open.
