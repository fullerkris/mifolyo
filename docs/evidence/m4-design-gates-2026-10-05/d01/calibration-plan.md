# OBS1 closed calibration proposal — D01 only

**DRAFT, NOT AN EXECUTION RECIPE OR APPROVAL. Nothing below was run.**
The behavioral scope, candidate privilege profiles, finite controls and exit
decisions are closed here; image/program/seccomp hashes and actual Linux-platform
capability observations are deliberately unfilled. An executable packet with
any of those missing must be refused. This plan is for a purpose-built synthetic
process, **not Redis, canonical Lua, an acceptance fixture or D02 credential tests**.

## 1. Question and alternatives

Can a separate non-root, confined observer hold an owned ARM64 execution thread
at a hash-bound instruction *before* a selected effect, identify repeated/nested
occurrences without confusing them, and fail closed when the observer disappears?

| Option | Gain | Cost / decision |
|---|---|---|
| C0: same UID/GID, no capabilities, private shared target PID namespace | Smallest requested authority; preferred if genuinely sufficient | Sibling attachment may be denied by Yama/LSM/dumpability/seccomp. No parentage or `PR_SET_PTRACER` change to manufacture equivalence. |
| C1: same UID/GID and namespace, only `CAP_SYS_PTRACE`, narrowly filtered seccomp | Candidate for sibling attachment without changing the target | Security-critical target-memory and debug-register authority. Non-root effective capability retention, user-namespace relationship and kernel support are unproved. Explicit separate approval is needed; never fallback automatically. |
| Cooperative in-process gates / modified runtime | Simple deterministic calibration markers | Useful only as an independent synthetic oracle; cannot be the Redis stop mechanism or substitute for hardware-stop precision. |

Use hardware **execution** breakpoints first. Watchpoints are not interchangeable:
a data watchpoint may report after the access. No software breakpoint, memory
patch, injected signal handler/hook, debugger expression, function call, remote
syscall, Redis DEBUG/EVAL substitution, forced AOF flush or GPR/PC/SP editing.
For repeated execution of the same instruction, the proposed narrow exception is
debug-register programming and a fixed one-instruction hardware single-step to
re-arm. Its effects on debug state and timing are explicit review subjects, not
an implicit fallback. If reviewers reject that mechanism, return to design;
do not substitute an unreviewed two-breakpoint or software-breakpoint method.

## 2. Artifacts that must be frozen before any request to execute

1. Purpose-built AArch64 PIE target, source, compiler/linker/dependency identities,
   build log, ELF SHA/size/build ID, native instruction manifest and reviewed
   disassembly. It must not link/start Redis or execute user-supplied code.
2. Observer and controller build/image/source hashes, fixed argv and closed case
   registry; no shell/debugger server in the observer. Observer image must contain
   only the required code/loader/runtime. Reviewed output/error/core-dump policy.
3. Calibration client/oracle artifact and its independent state assertions, plus
   exact immutable case data and expected ordinal/prototype/PC identities.
4. Docker/OCI configuration and exact seccomp **content hash** for each separately
   approved candidate; kernel/architecture/runtime/user-namespace identity,
   read-only Yama/LSM status, cgroup limits, ARM64 hardware debug-slot support and
   attach/dumpability observations are required runtime receipts, not assumptions.
5. Controller's exact ownership/absence validator, per-case deadlines and
   observer-loss watch, receipt schema/validator, redaction negative controls.
6. Independent correctness/security review, then owner approval for the exact
   OBS1 packet. Current D01 research permission does not satisfy either step.

**Current demonstrated profile: none.** Image history's Redis UID 999 is account
metadata, not proof of the eventual Redis process UID. OBS1 uses UID/GID **999:999**
as a proposed synthetic identity; OBS2 must bind whatever approved Redis runtime
identity is actually observed. A Docker `cap-add` flag is not a CapEff receipt.

## 3. Confinement contract for both candidates

```text
Owner-approved controller (only holder of Docker socket / lifecycle authority)
  ├─ disposable target container: its own PID namespace; synthetic process only
  ├─ separate observer container: joins that exact target PID namespace ONLY
  └─ independent client/oracle: fixed synthetic transport, no tracing authority

executor -> closed case ID -> controller -> validated identities / fixed artifacts
observer -> bounded HELD receipt -> controller -> closed resume-or-kill decision
```

- No host PID namespace, host IPC/user-namespace promotion, host mounts, network,
  published ports, Docker socket, writable target/data mount or reusable
  credentials in the observer. No `--privileged`, root fallback, `seccomp=unconfined`,
  daemon policy/sysctl change, target `PR_SET_DUMPABLE`/`PR_SET_PTRACER` workaround,
  extra capabilities, process name selection, `perf_event_open` or BPF.
- Read-only root filesystem; proposed limits: observer 64 MiB / 1 CPU / 16 PIDs,
  target 64 MiB / 1 CPU / 16 PIDs, client/oracle 32 MiB / 1 CPU / 8 PIDs.
  `RLIMIT_CORE=0`; private tmpfs at most 4 MiB, no swap allowance increase.
  `no-new-privileges`; fixed working directory and environment; no secret inputs.
- Control IPC must be specifically inventoried: propose a controller-owned,
  per-case private Unix-domain channel, fixed endpoint and modes, at most 64 KiB
  queued. An exact transport-only mount/FD is not a target data mount; if this
  cannot be realized with reviewed confinement, block the recipe rather than
  adding a host path. No observer-selected socket path or arbitrary FD passing.
- C0 has CapEff/CapPrm/CapInh/CapAmb/CapBnd all zero. C1 requests only
  `SYS_PTRACE` (bit 19, `0x80000`) in effective/permitted/bounding sets, inheritable
  and ambient zero, with a directly launched observer that does not exec after
  admission. **Demonstrate those exact sets** under the selected runtime; if the
  runtime cannot provide them to non-root, record C1 failure, not root fallback.
- UID/GID/user-namespace inode and mappings, PID namespace inode, target
  container ID, kernel boot identity, `/proc` start ticks, executable digest and
  owned main TID must agree. PID reuse, target restart/exec, ambiguous namespace,
  extra unscheduled processes or `/proc` inspection failure invalidate the case.
  Resolve ELF load bias from verified PT_LOAD mappings, not `maps`' first address.
  Cross-check expected mapped instruction bytes without publishing them.

### Seccomp candidate (specification, not an invented profile hash)

Start from an audited allowlist for the minimal observer runtime, default deny;
pin its actual file before review. Do not merely turn off Docker's filter.
Needed observation requests are closed:

- `PTRACE_SEIZE` with `PTRACE_O_EXITKILL`, `PTRACE_INTERRUPT`, `GETSIGINFO`,
  `GETREGSET` for `NT_PRSTATUS`/`NT_ARM_HW_BREAK`, bounded `PEEKDATA`,
  `SETREGSET` **only** for `NT_ARM_HW_BREAK`, `CONT`, `SINGLESTEP`, `DETACH`.
- Reject `ATTACH` fallback, `TRACEME`, `POKE*`, `SETREGS`, `SETREGSET` for
  `NT_PRSTATUS`/other regsets, `SYSCALL` tracing and unlisted requests. Reject
  `process_vm_writev`, `process_vm_readv` (not needed by this candidate),
  `ptrace` of any other TID and all target memory writes.
- Pre-attachment admission installs an additional filter restricting ptrace's
  PID argument to the immutable selected target TID. It never accepts a PID from
  the executor. The reviewed synthetic extra-thread case does **not** expand
  attachment to that thread; its ongoing heartbeat demonstrates the limitation.
  Any future all-thread mode is another profile/review.
- GETREGSET output, saved debug-register contents and at most one requested
  execute breakpoint slot are checked privately. Query the actual slot capacity
  and unsupported-regset/slot errors. No assumption about virtualized ARM64
  hardware. Existing debug state must be empty/expected; otherwise reject.
- A seccomp filter cannot examine the contents behind an iovec pointer passed
  to SETREGSET, nor make SYS_PTRACE inherently read-only. The exact observer is
  trusted, security-critical code. It must permit only its built-in slot
  programming/restoration and fixed single-step state machine. Request-number
  filtering alone does not prove those higher-level invariants.

No dynamic privilege search: approve C0 first. If it fails, retain its bounded
failure/cleanup receipts and seek a separate C1 decision. If C0 works, do not run
C1. To call C1 *minimal demonstrated*, repeat the identical profile without the
capability and prove why it is necessary, inspect all capability sets, and pass
the negative controls; still no claim of globally minimal authority.

## 4. Purpose-built target and independent oracle

The target contains a fixed 1,050-iteration operation loop (one native source
line), several same-value descriptors, two nested fake VM prototypes sharing
source-line numbers, known 32-bit bytecode indices, a monotonic completed-effect
counter and an independent append-only ordinal transcript. This is **not Lua**:
it calibrates identity/filter/PC semantics, not Redis's optimized register mapping.

The observer must not learn the expected counter from its own prior hits alone.
A separate oracle validates the target's immutable input and effect transcript,
and checks before/after values while held. In a small synthetic shared witness
mapping, target owns writes; observer has no mapping/write access; client/oracle
reads only. The exact IPC/mapping is inventoried. Native disassembly must prove
where the atomic/volatile marker loads/stores occur; source labels alone are not
accepted. No oracle may release a target gate before the held-stop receipt.

Add a separate thread incrementing a synthetic heartbeat and a controlled
fixed-frame stream sender/reader. They demonstrate that holding the selected
thread does not hold other threads or already-buffered bytes. Synthetic
write/fdatasync/response markers calibrate ordering and error classification only;
they are not storage durability or Redis AOF measurements.

Closed IDs resolve internally to the frozen target manifest. The caller cannot
submit an address, arbitrary ordinal, PID, expression, path, raw request, shell
argument or write payload. The finite ordinal set is
`{1,2,127,128,129,1049,1050}`. All nonspecified IDs are rejected before attachment.

## 5. Finite candidate suite and exact outcomes

Each positive trial uses a fresh process identity and one selected held cut;
completed trials cannot be reused as another case. No retry-until-success or
dropped samples. Ten repetitions must have fresh ASLR observations; do not disable
ASLR or demand a particular randomized address. Suites are per admitted profile.

| Group | Cases / repetitions | Required outcome |
|---|---|---|
| P1 native-loop precision | PRE and POST × 7 listed ordinals × 10 = **140** | On PRE(i), completed counter `i−1`, effect i absent; on POST(i), counter `i`, no i+1. Independently matched descriptor digest, exact TID/PC and no early release. Same-value descriptors must not collapse ordinals. |
| P2 fake-VM discrimination | two prototypes × bytecode PC `{0,1,7}` × 10 = **60** | Correct prototype/PC/occurrence despite shared source line. Wrong prototype or current/next-PC convention cannot yield HELD. |
| P3 async/ack distinctions | four cases × 10 = **40** | (a) held main + moving helper heartbeat; (b) independently proven pre-send/no complete frame; (c) complete frame received while main held; (d) last byte/receipt race or dropped receipt classified unknown. Never infer no-ack from silence or fsync. |
| F lifecycle failures | eight cases × 3 = **24** | Observer loss before seize; death after seize; death during selected hold; controller loss; transport EOF; receipt backpressure; hold deadline; unexpected target exit. Invalid experiment, bounded cleanup, independently observed absence. Exec/restart identity mismatch is also rejected by the N01/N02 identity controls, not an additional uncounted trial. |
| N admission/receipt controls | twelve cases × 1 = **12** | N01 wrong ELF/build; N02 stale start-time/PID; N03 wrong PID/user namespace; N04 wrong manifest/load-bias/instruction bytes; N05 wrong case/nonce; N06 unknown boundary ID; N07 missing/duplicate/reordered receipt; N08 wrong stop reason (ordinary SIGTRAP or group stop); N09 missing/extra capability or ineffective non-root cap; N10 denied seccomp/regset/insufficient slots; N11 out-of-case PID/write-regset request rejected; N12 canary leakage / truncated / overlong receipt rejected. |

**276 trials** after admission, no unbounded generation. A negative case whose
setup would itself breach confinement is performed against the closed validator
or an explicitly owned canary process in the disposable namespace; label that
evidence as synthetic validation, not a real denied host attachment. A denied
operation control must never intentionally target a foreign/host process.
Unsupported controls are failures/blocked, never silent skips.

Candidate hard limits, subject to exact-packet review:

- Per trial: 10 seconds admission/startup, 2 seconds active observation,
  5 seconds cleanup; overall suite 90 minutes. Controller monotonic deadlines,
  independent of observer liveness. No new work after authority expiry.
- Selected hold ≤ **20 ms**, using an enclosing interval from the independent
  target's last pre-instruction monotonic witness to release/kill dispatch;
  all observation-induced pauses ≤ **250 ms** per invocation; ≤ **8,192**
  breakpoint/single-step events; target-memory reads ≤ **4 MiB** per trial.
  These are candidate calibration rejection limits, **not measured capabilities
  or approved Redis equivalence bounds**. Record actual hit count, bytes read,
  max/total pause, wall time and peak memory. A cap exceeded fails even if the
  eventual effect counter is correct.
- Also record wait-event acquisition and dispatch times, but do not equate their
  difference with the entire pause: stop-to-observer scheduling delay is real.
  Target pre/post witnesses and the controller's arming/continuation handshake
  must give a conservative enclosing bound. Verify common monotonic/time-namespace
  basis or bounded clock offsets; otherwise fail the timing assertion. These
  synthetic witnesses cannot be inserted into canonical Redis later. OBS2 needs
  its own non-invasive bound including unobserved delivery/scheduling delay.
- At most 64 receipts/trial, 1,024 bytes each, and 64 KiB total. Per-hit internal
  state is bounded; do not log each scanned word. One selected cut per trial.
  A full/blocked output channel aborts rather than extending a hold.
- Immediately abort on wrong identity, wrong stop, unknown signal, disappeared
  thread, unexpected extra task, instruction mismatch, unsupported regset,
  out-of-bounds read, exhausted budget, ordinary GPR/memory difference caused by
  observer, schema/error leakage or inability to preserve the fixed state machine.

## 6. State machine, receipts and loss handling

`INTENT → ADMITTED → IDENTITY_BOUND → SEIZED → ARMED → HELD →
{RESUME_SELECTED | KILL_SELECTED} → TARGET_EXITED → OBSERVER_EXITED →
RESOURCES_REMOVED → ABSENCE_VERIFIED`. Every state may transition to
`INVALID → CLEANUP_REQUIRED`; that never becomes a successful precision result.

`HELD` is emitted only after wait status, siginfo and PRSTATUS confirm the
approved hardware breakpoint and the private PC/identity/ordinal checks. A
SIGTRAP without the appropriate hardware breakpoint reason is insufficient.
`PTRACE_INTERRUPT` admission stops and single-step traps are separate phases,
never selected-boundary receipts. No asynchronous notification followed by a
later stop qualifies. The controller does not dispatch its fault before it has
validated a case-bound HELD receipt and rechecked its own deadline.

The allowlisted receipt schema has `additionalProperties=false` and bounded
strings/enums/integers. Export only schema/version; approval/case/invocation IDs;
artifact/manifest hashes; role/container/process-identity binding; increasing
sequence and preceding-receipt digest; monotonic timestamps/durations; closed
phase and boundary ID; count/ordinal/PC-match/prototype-match/digest-match
booleans; closed stop reason; byte/event/memory budgets; dispatch/exit/cleanup
outcomes. Prototype/argument values are compared privately; no raw PC/register,
memory, stack, request, source byte, arbitrary error, token or credential dump.
Two producers have distinct sequences and controller-observed causal handshakes;
do not compare uncalibrated clocks as though they were a total order.

The independent client produces a separately bound acknowledgment classification:
`known_pre_acknowledgment | acknowledged | unknown` plus the closed proof method.
For known-pre-ack, an affirmative client/transport observation must be ordered
with the held pre-send boundary such that the tested complete frame **cannot**
arrive in the gap. After bytes are buffered, main-thread hold alone is inadequate;
without a separately reviewed client barrier/complete-frame bound, use unknown.
Full frame received but no retained receipt is unknown, not no-ack. Receiving a
different fixture/setup frame cannot satisfy the tested-invocation classification.
This is only an interface for parent/D02; it does not decide refusal eligibility.

`EXITKILL` is only one layer. It must be enabled atomically with SEIZE, tested by
actual observer death on this disposable target, and independently reconciled
with target/thread exit. Do not assume it cleans up untraced siblings/IPC. Before
seize, or when the controller disappears, its independently owned watchdog must
still terminate the **exact owned** target, observer, oracle and helpers. The
watchdog has fixed resource identities, not arbitrary kill authority in the
executor. Unknown/foreign ownership prevents the affected unsafe deletion;
other proven-owned cleanup continues and unresolved obligations stay named.

For a healthy resume trial, remove the selected breakpoint, fixed-step/re-arm
only as specified, restore the originally captured debug state, and independently
verify the expected single continuation and final transcript before termination.
On any failure do **not** detach-and-run as a success path: keep held where possible
and have the controller kill/wait the owned target. Observer also exits; every
process/container/control channel/tmpfs or other owned artifact is removed and
inspected absent by independent identity. Command success is not absence proof;
inspection error is not absence. No orphan paused target, helper, transport or
debug state may be silently retained. Unresolved cleanup invalidates the suite.

## 7. Pass/fail and the next decisions

1. **Admission failure:** receipt + complete cleanup; reject that profile. For C0,
   ask whether C1 warrants a new exact approval. For C1 failure, **NO-GO external
   observer on this target**; return for a target/mechanism decision. No escalation.
2. **Any precision, identity, secrecy, ordering, bounds or cleanup failure:**
   OBS1 fails. Fix/review/freeze changed artifacts and request a new suite; never
   convert failed controls to acceptance or retain only successful samples.
3. **Every admitted required trial passes and all cleanup independently closes:**
   candidate **GO for independent OBS1 evidence review only**. Reviewers assess
   demonstrated minimum profile and limitations. It is not owner approval for Redis.
4. **After OBS1 is independently accepted:** request a separate OBS2 design and
   exact-artifact decision. OBS2 must solve the actual Lua prototype/bytecode and
   native register mapping, target configuration/thread identity, memory bounds,
   invocation/ordinal correlation and unchanged timing/expiry behavior. It begins
   with a small canonical operation, not the maximum COMMIT suite.
5. **OBS3/OBS4 remain closed to execution** until every branch/boundary, maximum
   byte/memory shape, AOF/client binding and parent-owned lifecycle decision is
   frozen. Traced samples never satisfy untraced p99 gates.
