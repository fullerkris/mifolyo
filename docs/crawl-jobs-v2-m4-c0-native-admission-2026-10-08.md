# C0 native-runtime admission — 2026-10-08

**Implemented locally; 99 tests and the strict ARM64 static build pass. Independent
review is pending.** This increment adds native startup observations, their bounded
Python validation and the owned-stream gates that precede target execution and
observer attachment. It remains on `feature/crawl-jobs-v2-c0-observer`, based at
verified `ab5f21a`. Full M4 remains open.

## Implementation

- **Native collector:** `native/admission.h` reads fixed Linux procfs/cgroup paths,
  hashes private inputs and emits IDENTITY, CONFINEMENT, RESOURCES and CLOCK frames.
  Each is canonical JSON, at most 1,024 bytes, and bound to the closed case plus a
  fresh invocation/scope digest supplied with the case. No caller PID or path exists.
- **Observed facts:** executable/boot/mapping hashes; namespace inodes/device;
  PID/main-TID/start ticks; UID/GID slots and groups; all five zero capability sets;
  no-new-privileges, dumpability, seccomp mode/count, core limits and task inventory;
  cgroup-v2 limits/usage; kernel/LSM/Yama and monotonic-clock information.
- **Validation:** `runtime_admission.py` checks exact schemas, prepared ELF identities,
  externally trusted kernel/clock expectations, resource bounds, fresh windows and
  peer relationships. Observer admission revalidates the target's original frames
  and requires shared PID/user/time namespaces with private mount/network/IPC/cgroup
  namespaces. Summaries contain bounded digests and carry no execution authority.
- **Integration:** `DockerBackend.collect_native_admission()` verifies an already
  owned stream and complete resource snapshot, checks running metadata before/after
  collection, rejects restart/PID changes, and rechecks attachment inventory. It
  anchors the process generation before case/nonce dispatch and rejects EOF or
  extra/partial output at readiness, preventing buffered old frames from being
  associated with a new process generation.
  Observer `A` is gated on admission; target `G` also requires an admitted ARMED
  observer. Expired/lost peers and revoked input authority reject dispatch.
- **Native ordering:** the target stays READY awaiting `G`. The observer now emits
  ADMISSION_READY and waits for `A` before ptrace attachment. Existing held/resume
  timing remains separate; no Docker inspection was added to that short path.

Preparation requires an additional `admission_source` digest. The native-role OCI
profiles add only read-only `uname`, `clock_getres`, `PR_GET_DUMPABLE` and
`PR_GET_SECCOMP` operations; policy setters and privilege escalation remain excluded.
The oracle profile is unchanged. The target now links the pinned compiler image's
existing static crypto library for its own executable digest.

## Verification and exact bindings

The package contains **25 source/test/documentation files**: three additions and
eleven modified files relative to the reconciled baseline. The new tests reject
foreign/replayed frames, coercions, invalid hashes/namespaces/capabilities, extra
tasks, expanded limits, stale clocks, missing peers, changed container identity,
expired admission and unsafe stream ordering. Benign Python processes exercise the
new case/nonce and pre-arm exchange. Docker operations in tests remain simulated.

The full suite passes **99 tests, zero skips**, with ResourceWarnings treated as
errors. A networkless, pull-disabled build uses the retained GCC 14.3.0/bookworm
arm64 manifest and strict `-Wall -Wextra -Werror -Wformat=2` compilation. Static ELF
and compiled-BPF joins pass. Sixteen checks compare format strings from both built
ELFs against independently authored Python fixture bytes, including the longest
native case name; the largest tested frame is 645 bytes. These are static wire
compatibility checks, not execution of the collector or native actors.

| Binding | SHA-256 |
|---|---|
| Final 25-file source inventory | `a24bb741c5b22de576ca62abff8d133e675eaf1ee059509349d4138d12bfcf12` |
| Final verification | `5bd44ff6c645e2e2b4298580fdae76d81262815051383c00b031f42c7d2f7e2e` |
| Native artifact build | `f228da9b431cd953ba75fe8fbc3dfe5695f2540729d7bb62f13bd57a0e792723` |
| Revised target ELF | `45f78ae0bce897a430b78e4e5c02908308d0432dcc959f8b20380dbea0f7bef5` |
| Revised observer ELF | `77576d303391c7e0dcaf6fe2efcef85beee0f73dae70ca93341ea3d599e5021f` |
| Implementation disposition | `8886f62468dd36c936d35f31a8e779398f0c0b44b65c34db540151952e3177f5` |

The build record retains its recording-time package snapshot. Python admission/
stream checks, test fixtures and README were finalized afterward; all **nine
compiler inputs** still match the compiled artifacts. The final source inventory
and 99-test record bind the final
Python/documentation bytes. The earlier eleven native artifacts and all historical
review/reconciliation records are preserved separately.

Self-review of the initial 96-test version identified a pre-collection restart /
buffered-EOF gap. Process-generation anchoring and live-ready checks close it;
three added regressions cover the stale-generation and closed/extra-output paths.
The initial verification and source snapshot remain private, alongside the final
99-test snapshot. This self-review does not substitute for independent review.

## Remaining boundaries

The collector refuses inaccessible LSM/Yama, cgroup or time-namespace observations;
it does not add a privileged helper or manufacture a fallback. Runtime compatibility
under the selected Docker/Linux environment remains unproved. The kernel/clock
context must come from an independently trusted outer source, including a valid
Linux observation window; actor frames cannot supply their own expectations.

Seccomp counts are not kernel-bytecode readback. Exact OCI metadata and the pinned
compiled self-install code provide separate bindings. Pre-case observations do not
establish whole-trial resource confinement, hardware-debug availability, held-boundary
precision or actual-end timing. The oracle/client-closure worker, full trial
dispatcher, runtime images and approval/execution assembly remain subsequent work.

No native actor, tracing experiment or Redis case ran. The new source has not yet
received independent correctness/security review; previous GO records retain their
original scopes. The changes remain local and unstaged in the C0 worktree.

See the [package contract](../tests/crawl-jobs-v2-observer/README.md),
[exact evidence index](evidence/m4-c0-native-admission-2026-10-08/README.md) and
[baseline reconciliation](crawl-jobs-v2-m4-c0-reconciliation-2026-10-07.md).
