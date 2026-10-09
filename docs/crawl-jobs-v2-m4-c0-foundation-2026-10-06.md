# C0 observer artifact foundation: implementation and review — 2026-10-06

**Correctness GO and security GO for continued implementation.** The isolated
15-file C0 synthetic artifact foundation and offline D02 validators compile and
pass **41 offline tests**, with all findings closed after two remediation rounds.
This is not a complete executable OBS1 packet or runtime feasibility result.

The owner selected **Start implementation** while cancellation PR #20 proceeded
through its separate publication/CI gate. Work is local and uncommitted on
`feature/crawl-jobs-v2-c0-observer`, based on `e80d005`. The 22 inherited design
files retain their bytes, as do the normative protocol and Redis fixture runtime.

## Implemented scope

The [package README](../tests/crawl-jobs-v2-observer/README.md) describes the exact
components and limits:

- Fixed 276-ID C0 preparation registry, canonical bounded JSON, capless/process
  identity contracts and COR-01 actual-end timing validation.
- Synthetic AArch64 target with 1,050 same-value effects and a separate ordinal
  transcript, explicit native markers, nested fake-VM markers, heartbeat and
  fixed-frame Unix-stream paths.
- Native sibling-observer candidate: fixed owned inner TID 1, exact ELF/load-bias
  binding, zero-capability checks, SEIZE/EXITKILL, execution-breakpoint and fixed
  single-step/re-arm. TRACEEXEC rejects exec; it does not follow it.
- Default-deny OCI profile generation and additional fixed-TID BPF, including
  request/regset/high-bit alias and post-admission exec rejection. Compiled ELF
  bytes match the generated filter exactly.
- Independent complete witness/oracle checks and pure finite coordinator model.
- Draft offline R1–R9 envelopes and cross-receipt validation for ACK/effect,
  no-regrant ACL, BOOT/refusal, stop and resource-disposal claims.

No target/observer binary, installed kernel filter, tracing experiment or Redis
fault case was executed. Native artifact construction invokes the compiler and
static inspection only. The 276 IDs enumerate proposed trials, not completed runs.

## Exact final bindings

| Binding | SHA-256 / identity |
|---|---|
| Base | `e80d00538b11c46d021427bcfe1c408e16071c68` |
| Final 15-file source inventory | `e45134cf2e78c427d0dcec085931cae60259da63822998c87304786d51f6c6b3` |
| Final read-only snapshot intake, 1,481 files | `c31dd78f32a7ade828f2af8e9b28d65a70a2766307bdf4d902ea9c3870912d05` |
| Final verification | `c9c332d3aec61c78072133c8e42d4e4bc54b5ea0b17125bf25b31a2c8b0195d4` |
| Correctness final review | `f39406b285fb592aab2b00fe2d925b662bad34e11750d364991ccd61539a3ce4` |
| Security final review | `c986954bf8d705a225600d8e72087a6eb5c67abf89568fdb0deb0707966b76c3` |
| Combined review decisions | `811a252bbe6c06dbfc6834559ad56d402df09f6a746d9cc11e321af6069ad446` |
| Synthetic target ELF | `278a7939e00495c96eb008c4c59829189a235f77311b59d770fb18a9499f55b0` |
| Native observer ELF | `555a7c9e1ddbf1e076eb6603cef81e6579abaf7902072978c0826e84fc9e07d9` |
| Additional filter bytes | `701824ff5f8ccafa359f711d2714b23c8a303789cd1987a3a85aba19a3667b93` |
| Static target manifest | `f57aabca24a94265a18d42c178bf5ef6a11eae11e6cedb32003c82f90f26c4b8` |

The compiler is GCC 14.3.0/bookworm, selected immutable arm64 manifest
`sha256:66035d353338cb93b64f621393dc6fecde85258651ca454f0cf36ff2639b1352`,
local image `sha256:3a44f591405bf6dfaf2c26e2958bec7860c627c12d810c11ddaa0363f1df190a`.
Builds use network none and pull=false after caching the pinned compiler. Versions,
selected static-library hashes and the eight build-source inputs are retained in
the toolchain record. Scratch target/observer image stages are defined; no
execution-image admission or runtime confinement result is claimed.

## Build and verification chronology

Three preliminary builds failed visibly: the Python runtime base lacked a C
compiler; the pinned compiler then rejected an unchecked diagnostic write and an
unsigned comparison against a generated zero constant under `-Werror`. Those
issues were corrected with a pinned compiler stage, checked nonblocking writes and
checked subtraction. Subsequent builds passed. The retained final build-06 record
is `0830e0fa3d35a61de61967a195d41c952d3aa29c6bdd6f6949dfd6f9ee3d9657`.

The first source snapshot passed 31 tests but both independent reviews returned
NEEDS_REMEDIATION. The 39-test revision closed the original findings, but re-review
identified the inverse no-write/replay consistency gap. The final 41-test revision
closes that gap. All eight native build inputs and eleven artifacts remained
unchanged through these Python-validator/documentation remediations.

Both final reviewers preserve all prior reports, probes and three read-only
snapshots. Correctness completed 380 independent probe records, 552 oracle case
checks and 128 ACK combinations. Security completed 13 independent tests,
including 732 specific-code rejection assertions and a 576-vector current-row
matrix. These are offline/static checks, not kernel or runtime experiments.

## Findings closed

All six COR-C0 and six SEC-C0 identifiers are closed; overlapping findings are
retained in both review streams. The implemented corrections cover:

- Reconstructing actual included per-issuer chains from sequence zero; no silent
  external anchors, gaps or stale predecessor hashes.
- Joining early/late complete replies to their required tested publication or
  backpressure effect, while representing no-write and replay explicitly.
- Distinct physical replay/original identities and rejection of any new ledger
  effect under a no-write/replay invocation.
- Complete typed P3 held and P1/P2/P3 final states; no successful N/F final credit.
- Typed case/process-bound BOOT references; state-oracle substitution rejected.
- No post-restart state/BOOT credit after prefault deny-all sealing.
- Stop-proof requirements for fault-scoped assertion and recovery credit.

## Next implementation boundary

Executable lifecycle/controller and owned watchdog, volume initialization,
runtime identity/admission, trusted native-message/session framing, client-frame
closure, complete cleanup/absence and diagnostic policy still require assembly.
Actual-end timing, aggregate pause, memory and hardware/Yama feasibility need
separately authorized runtime evidence. The pure coordinator and digests cannot
prove those events occurred.

The next step is continued implementation of those adapters, followed by review
of exact source/images/profiles/receipts before any execution decision. C1,
Redis fault cases, normative amendments, OBS1 acceptance and full M4 stay open.

The [evidence index](evidence/m4-c0-foundation-2026-10-06/README.md) binds fourteen
exact records, preserving initial and intermediate NEEDS_REMEDIATION decisions.
Native binaries, large snapshots, probes and logs remain private under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-c0-preparation-2026-10-05/`.
