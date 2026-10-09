# C0 synthetic observer preparation

Implementation-only artifacts for the independently reviewed OBS1 design. This
directory is separate from the Redis fixture harness and has no runtime imports
from it. Native code is compiled for static review; the preparation build never
executes either output program. The Python APIs provide contracts/oracles and
bounded, externally authorized admission collection from the owned actor streams.

## Implemented artifact components

- `contracts.py`: fixed 276-trial C0 registry, immutable preparation identities,
  exact capless/process bindings, bounded canonical JSON and case-bound receipts.
  COR-01's hold check includes clock uncertainty and requires actual continuation
  or confirmed stop. A dispatch timestamp cannot close the interval.
  Preparation now requires an `admission_source` digest in addition to the other
  frozen artifacts; old preparation objects lacking it reject.
- `native/target.c`: AArch64 PIE synthetic target with explicit pre/post instruction
  labels, 1,050 same-value effects and separate ordinal transcript; nested fake-VM
  prototype/PC markers; helper heartbeat and fixed-frame Unix-stream cases.
- `native/observer.c`: fixed owned inner TID 1, no caller address/PID, zero-capability
  checks, exact target ELF hash and executable-mapping load bias, SEIZE/EXITKILL,
  hardware execute-breakpoint candidate and fixed single-step/re-arm. TRACEEXEC
  exists solely to reject exec, never follow it. Errors are closed/value-free;
  output is nonblocking. No software patch, GPR write, C1 fallback or Redis code.
- `profiles.py`: default-deny AArch64 OCI profiles and an additional deterministic
  TID-1 filter. The latter rejects other TIDs, write regsets, high-bit request/regset
  aliases and post-admission exec; the OCI profile supplies the broader syscall
  boundary. TID 1 must be independently admitted as the owned namespace init.
- `build_tools.py`: bounded static ELF/symbol/instruction joins and frozen headers;
  verifies generated BPF bytes against the compiled observer's actual ELF bytes.
- `oracle.py`: independent complete ordinal-transcript, same-value and nested
  prototype/PC checks. Its fixed witness reader opens only `/witness/state`,
  read-only/no-follow, with independently supplied file identity and stable reads.
- `controller.py`: pure finite coordination model and role specifications; rejects
  early release, reordered/borrowed messages, dispatch-only timing and incomplete
  cleanup. It remains the coordination contract rather than an approval issuer.
- `d02.py`: draft offline R1–R9 envelope/payload and cross-receipt validation,
  including separate at-cut ACK versus post-cut client closure, durable ACL-seal
  requirements, complete resource inventory and no refusal credit for state/BOOT/
  acknowledged durability. The optional extinction exception remains disabled.
- `owned_resources.py` / `docker_backend.py`: fixed local-Docker resource and
  metadata-admission adapters. Fresh control/witness volumes use bounded 1 MiB
  tmpfs mounts owned by 999:999; no privileged initializer is introduced. Only
  the observer joins the target PID namespace. The oracle has a private namespace
  so target PID-1 exit cannot kill the independent completion reader. Mutations
  require the outer controller's authorizer; the default adapter is read-only.
  Actor activation rechecks the full acknowledged attachment inventory. Image
  healthcheck commands are rejected, and container creation disables healthchecks
  explicitly. Cleanup uses the supported Docker CLI wait command plus independent
  stopped-state/PID-zero inspection.
- `streams.py`: bounded NDJSON framing, shared 64-frame/64-KiB input budget,
  fixed native case/nonce/start/arm/resume writes, stderr refusal and owned local process
  handling. EOF/reaping a Docker CLI is not remote-container or process-group
  absence proof.
  Input dispatch also rejects pending partial output: withholding a newline cannot
  turn an early FINISHED/CONTINUATION frame into a post-dispatch observation.
- `watchdog.py`: separate-session host supervisor with bounded private IPC,
  acknowledged resource-prefix updates and exclusive mode-0600 retained journal.
  Owner EOF, deadline or protocol failure triggers independently scoped cleanup.
  Final cleanup bytes are reconciled against the retained journal and file identity.
  A separate external cleanup authorizer must delegate the exact scope; a scope
  hash alone is never authority. The parent requires the exact acknowledged armed
  prefix and verified retention before accepting the final receipt. Cleanup still
  runs if retention fails, but its completion receipt is rejected as unproven.

The adapter layer is tested with synthetic Docker metadata and benign local
Python process/pipe fixtures, including actual owner-process exit. No adapter test
creates Docker resources or starts a native actor. Docker metadata predicates are
not observations of runtime capabilities, seccomp installation or tracing support.
The original adapter layer retained its native artifacts. The subsequent admission
increment changes native sources/profiles and rebuilds the target and observer;
earlier artifact identities remain historical records.

## Native-runtime admission exchange

`native/admission.h` collects fixed-path Linux/AArch64 startup observations in the
target and observer, after case selection and a nonzero 64-hex invocation/scope
nonce. It emits four canonical frames, each still limited to 1,024 bytes:

1. **IDENTITY:** executable SHA-256, boot identity, namespace inodes/device,
   UID/GID-map digests, process/main-thread IDs and start ticks.
2. **CONFINEMENT:** all UID/GID slots, supplementary groups, five zero capability
   sets, no-new-privileges, seccomp mode/filter count, dumpability, core limits,
   tracer identity and the initial singleton task inventory.
3. **RESOURCES:** private cgroup-v2 placement, effective memory/swap/CPU/PID
   limits, memory usage/peak and singleton process membership.
4. **CLOCK:** kernel-release, LSM and time-offset digests, Yama policy, monotonic
   timestamp and resolution. Time-namespace offsets must be zero.

Absolute C0 clock values use exact unsigned 64-bit nanoseconds, including trusted
observation windows, hold endpoints and native witness timestamps. This preserves
fresh observations beyond the 2^53-nanosecond uptime boundary. Other counters keep
their existing bounds, and boolean/float/coerced values remain rejected.

The native collector hashes bounded private inputs instead of exporting raw
procfs, maps, executable bytes or kernel strings. It checks identity stability
around collection. Missing/unsupported kernel files, unbounded reads, unexpected
tasks or changed identity refuse admission. In particular, inaccessible LSM/Yama
or cgroup/time-namespace observations are refusals; no privileged helper, mount,
sysctl change or capability fallback is introduced. Read-only `uname`,
`clock_getres`, `PR_GET_DUMPABLE` and `PR_GET_SECCOMP` are added to the native-role
profiles; policy setters remain excluded and the oracle profile is unchanged.

`runtime_admission.py` independently checks the four frames against exact prepared
ELFs, case/scope/role bindings and an **externally trusted kernel/clock context**.
That context supplies boot/release and mapping hashes, expected user/time namespace,
host namespace exclusions, LSM/Yama values, seccomp-filter counts and a maximum
10-second observation window. Its producer must establish the Linux clock domain;
the macOS controller's clock cannot be substituted. The context must not be derived
from the same untrusted frames it is meant to check. Context acquisition/approval
is an outer integration obligation, not an authority issued by this validator.

Observer admission revalidates the target's original frames. It requires shared
PID/user/time namespaces, distinct mount/network/IPC/cgroup namespaces and coherent
clock ordering. Numeric coercions, foreign/replayed bindings, stale windows,
changed executable/mappings, extra privileges/tasks, missing filters and expanded
resource limits reject. Filter counts are observations, not kernel-bytecode readback;
exact OCI content and the compiled self-install code provide the separate artifact
bindings. Hardware slot/attach feasibility remains an observation-stage concern.

`DockerBackend.collect_native_admission()` joins these frames to an already-owned
running container and rechecks platform, labels, attachments, PID/start time and
restart count before/after collection. The process generation is anchored before
case/nonce dispatch, so buffered frames cannot be joined to a container restarted
before collection. EOF or extra/partial output at READY also refuses admission.
Target admission precedes observer admission.
The target stays at READY; the observer emits ADMISSION_READY and waits for a fixed
`A` byte **before ptrace attachment**. The backend blocks `A` without admission and
blocks target `G` until the admitted observer is ARMED. After the final input
authorizer, a nonblocking peer check requires a quiet live READY/ARMED transport;
readable EOF, stderr, partial output and queued frames (including pre-G HELD)
reject dispatch. Own and peer admission expiries are rechecked after callbacks /
polling and again at the write boundary. The stream checks its deadline immediately
before each write. Collection performs final live-ready checks after metadata /
authorization work. These checks observe currently available loss; they do not
guarantee future channel lifetime atomically. No Docker inspection is added to
the short HELD/resume path.

Typical integration order (only under the outer execution authorizer): open the
target stream, send its fixed case/nonce, collect target admission; open the observer,
send its case/nonce, collect observer admission; arm/receive ARMED; start the target.
The admission collector accepts only backend-owned streams and complete resource
snapshots. Failure closes the local stream and raises a value-free error; independent
watchdog cleanup and remote absence proof remain mandatory.

The compact digest summary is not a transferable capability. Both validation and
collection retain `execution_authorized=false` and `runtime_evidence=false`; injected
Docker/stream fixtures are explicitly labelled simulated. A pre-case snapshot does
not establish confinement/resource usage for a whole trial, held timing, hardware
breakpoint precision, or oracle/client closure. The oracle worker is still pending.

Creation candidates are journalled before mutation; identities are bound before
start. A lost create response is never retried. Currently observed owned resources
are disposed of when safe, but a candidate-only create remains **unsettled** even
if its name is absent: a daemon operation might complete later. Such a prefix
cannot yield complete cleanup. Foreign ownership/attachments and inspect failures
also stay unresolved while other safe cleanup continues. The adapter binds daemon
identity around reads so a different daemon's empty inventory is not absence proof.

The D02 preparation contract caps each receipt at 64 KiB, references at 128,
effect rows at 128, resources at 32 and issuer sequence at 1,024. It preserves
300/30/60-second case/stage/cleanup ceilings. These closed preparation bounds do
not claim the full maximum-shape M4 inventory. The intent binds the immutable
effect definitions, separately from subsequent recovery outcomes; no circular
hash of a future recovery receipt is required.

The implementation-review corrections require complete per-issuer chains from
sequence zero and join each predecessor to the actual included receipt bytes;
external journal anchors and gaps are not admitted. The intent explicitly names
the tested publication/backpressure, replay or no-write effect, and every early
or late complete response is joined to that disposition and its required ledger
mutation. A recovered unrelated probe cannot replace a tested acknowledged write.
Invocation hashes identify physical invocations/attempts, not just shared logical
idempotency keys. A replay binds a distinct original mutation; no-write and replay
dispositions forbid a new ledger effect under the current invocation identity.
Reference metadata is typed and bound to the case and Redis process; an oracle
digest cannot substitute for canonical BOOT proof. A prefault deny-all seal cannot
yield post-restart state/BOOT credit. Missing stop proof prevents fault-scoped
refusal, atomicity, durability, BOOT and recovery credit. The independent oracle
now checks exact complete held/final state for each supported native family and
rejects N controls and failure trials as successful final-state results.

## Build and offline checks

```sh
python3 -B -W error::ResourceWarning -m unittest discover -s tests/crawl-jobs-v2-observer -v
docker build --network none --pull=false --platform linux/arm64 \
  --target artifacts --output type=local,dest=/absolute/private/empty-directory \
  tests/crawl-jobs-v2-observer
```

The compiler stage uses the immutable GCC 14.3.0/bookworm arm64 manifest
`sha256:66035d353338cb93b64f621393dc6fecde85258651ca454f0cf36ff2639b1352`.
It requires that image to be cached. Both native programs are static PIEs;
separate `target` and `observer` image targets use scratch with only one binary
and UID/GID 999:999. Compiler/linker versions, selected static-library hashes,
source bytes and native ELF identities are retained in `toolchain.json`.

The compiler image and static libraries are build inputs, not audited production
release dependencies. A successful compile proves neither kernel debug support,
Yama/LSM access, capless attachment, timing precision nor a complete experiment.

## Remaining execution assembly

The resource, stream, watchdog and native admission collection/validation layers
now exist as implementation components. They still need trusted runtime-context
acquisition, the oracle worker/client-frame closure bridge, full trial dispatch,
complete per-trial receipt assembly and exact runtime images. The outer authorizer must be bound to the
separate reviewed approval/execution decision; no approval parser or `run` CLI is
provided by this layer. Oracle-worker argv is reserved by the closed blueprint,
not a claim that that worker image is implemented or admitted.

The pure coordinator consumes independently validated evidence digests; it cannot
prove events occurred. The new stream objects bind local process handles and the
closed protocol, but do not by themselves prove the final runtime process/session
or clock identities. No complete executable OBS1 packet is emitted while those
bridges and the remaining trial handlers are absent.

The 276 IDs enumerate the reviewed proposed suite; this package does not claim
that 276 trials ran or that all execution adapters exist. N controls are reserved
for closed validator or owned-canary checks; they are refused by native actors.
Failure/loss injection belongs to the future owned controller/watchdog, never an
arbitrary process selector. Measured aggregate pause, actual-end hold bounds,
common clock identity, memory, task inventory and retained diagnostic policy need
independent runtime evidence before any calibration result can pass.

The native observer emits continuation **dispatch**, not a duration/cleanup PASS.
`PTRACE_O_EXITKILL` is only one layer; no claim is made that it disposes of every
container, helper, volume or transport. C0 denial remains denial; C1 and Redis
experiments require separate decisions. All current Python result objects retain
`execution_authorized=false`/`runtime_evidence=false` or equivalent preparation
flags. The normative Redis protocol and current acceptance validators are intact.

See the [design review](../../docs/crawl-jobs-v2-m4-design-review-2026-10-05.md)
and [original calibration proposal](../../docs/evidence/m4-design-gates-2026-10-05/d01/calibration-plan.md).
