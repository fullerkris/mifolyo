# C0 synthetic observer preparation

Implementation-only artifacts for the independently reviewed OBS1 design. This
directory is separate from the Redis fixture harness and has no runtime imports
from it. Native code is compiled for static review; the preparation build never
executes either output program. The Python APIs provide contracts/oracles and
bounded, externally authorized admission collection from the owned actor streams.

## Architecture-bound contracts (C0-ARCH-02)

Architecture selection uses the canonical Linux names below. `contracts.platform()`
returns a closed descriptor; `platform_digest()` binds its exact canonical bytes.
Aliases are not accepted in preparation/deployment/admission packets.

| Contract name | OCI platform | ELF machine | Seccomp architecture |
|---|---|---|---|
| `aarch64` | `linux/arm64` | 183 (`EM_AARCH64`), ELF64 little-endian | `SCMP_ARCH_AARCH64` |
| `x86_64` | `linux/amd64` | 62 (`EM_X86_64`), ELF64 little-endian | `SCMP_ARCH_X86_64` |

Both names have native implementations, profile/filter generation, resource
scopes and context/oracle support. Each platform has its own instruction/debug
ABI and artifact identities. Native acceptance requires that platform's admitted
host, images, confinement and calibration evidence (C0-ARCH-04/05).

Preparation and resource scopes are **version 2**, with an explicit architecture
and `platform_sha256`. Deployment objects and their anchors are **version 2**
with matching architectures. Generated host service configuration and bundle
manifests carry the same selection. Missing/legacy fields reject rather than
being inferred; producer convenience defaults still select ARM64 explicitly.

The signed context protocol and all four native admission frames are **version 2**.
Requests bind the selected architecture; the authenticated KERNEL payload carries
the collector's observed architecture. Native IDENTITY carries the actor's
architecture, joined to the preparation, trusted context, owned stream binding
and resource scope. Control messages and the three-line case/nonce/Linux-expiry
handshake retain their existing format. Frame and shared-byte limits are unchanged.

Docker daemon/image metadata must match the selected OCI architecture before
container creation/start. Profile content stays exact and architecture-bound.
Build manifests/toolchain records bind the platform digest, actual ELF headers
and artifact hashes; runtime interpreter hashing also checks ELF architecture.
The ELF-header classifier alone is not complete ELF, image or runtime admission.

**Regenerate** old preparations, source/deployment bundles, resource snapshots,
context exchanges, native ELFs/instruction manifests and oracle manifests for this
version. Old version-1 admission frames and unbound packets are incompatible.
ARM64 seccomp profile and compiled-filter bytes retain their existing constraints.
Dual-runtime image admission and native host/backend calibration remain
C0-ARCH-04/05 work; these contracts do not grant execution authority.

### X64 native port (C0-ARCH-03)

`native/architecture.h` accepts only Linux little-endian LP64 AArch64/x86-64.
The x64 target uses the SysV calling convention: `movq $42,(%rcx)`, the ordinal
transcript store, and the release-ordered completed-counter store. X86 TSO and
aligned 64-bit stores preserve the witness contract. The post boundary is the
one-byte `ret`; VM/send markers are `nop; ret`. The static decoder checks the
complete 14-byte effect, exact boundaries and two-byte marker functions rather
than treating x86 as fixed-width instructions. Runtime reads compare the full
selected instruction, including variable-length/unaligned boundaries.

`native/x86_debug.h` uses Linux's LP64 `struct user` offsets, compile-time checked
against the filter constants. It admits inactive DR0..3/DR7 and reset DR6, arms
only DR0 with local one-byte execution mode, and verifies register readback.
DR6 must identify only DR0 at a hardware stop or only single-step at the step
stop. Disabling restores the exact admitted state before stepping/detaching.
The observer validates `TRAP_HWBKPT`, signal address, 216-byte native regset,
64-bit code selector and RIP; non-return single-step advancement is checked.

Both x64 filters restrict `PTRACE_PEEKUSER` to DR0..3/DR6/DR7 offsets and
`PTRACE_POKEUSER` to DR0, reset-only DR6 and DR7 values 0/1. GP writes, ARM
regsets, DR1..5 writes and data/watchpoint modes reject. The additional filter
denies `execve`/`execveat`, kills foreign audit architectures and syscall numbers
>=512 (including x32, historical compat aliases and unsigned negative numbers),
and checks the high words of request/TID/offset/value arguments. OCI profiles
remain default-deny, with x64 libc TLS (`ARCH_SET_FS` only), `readlink`, and the
target's native `chmod` wrapper added. ARM64 profile/filter bytes are unchanged.

The oracle selects a supported ABI from its observed Linux kernel, verifies
LP64 and joins its actual interpreter ELF to that selection. Host collectors
still use their independently provisioned architecture. All cross-architecture
context/image/profile/peer refusals and the three-line handshake remain in force.

Artifact-only build (choose `linux/arm64` + `aarch64` for ARM64):

```sh
podman build --platform linux/amd64 --build-arg C0_ARCHITECTURE=x86_64 \
  --network none --http-proxy=false --target artifacts \
  --output type=local,dest=/absolute/new-artifacts \
  tests/crawl-jobs-v2-observer
```

The Dockerfile pins GCC 14.3.0/bookworm independently for each architecture.
The build runs compilers and static inspectors only. `Dockerfile.oracle` still
selects the pinned ARM64 Python base; the x64 Python/runtime image set is the
C0-ARCH-04 deliverable. `build_oracle.py DEST x86_64` supports an independently
selected x64 interpreter. `native/test_x86_debug.c` is an offline C probe with
an in-memory request callback; it can be compiled on x64 without running actors
or invoking ptrace. Python tests interpret both filter layers and exercise
positive/mixed-architecture artifact, context and benign-process protocol paths.

ABI references: Linux [x86 ptrace implementation](https://github.com/torvalds/linux/blob/v6.18/arch/x86/kernel/ptrace.c),
[debug-register UAPI](https://github.com/torvalds/linux/blob/v6.18/arch/x86/include/uapi/asm/debugreg.h),
and [seccomp ABI/compat caveats](https://man7.org/linux/man-pages/man2/seccomp.2.html).

## Implemented artifact components

- `contracts.py`: fixed 276-trial C0 registry, immutable preparation identities,
  exact capless/process bindings, bounded canonical JSON and case-bound receipts.
  COR-01's hold check includes clock uncertainty and requires actual continuation
  or confirmed stop. A dispatch timestamp cannot close the interval.
   Preparation requires `admission_source`, `runtime_context_source`,
   `oracle_worker_source` and `oracle_elf` identities as well as the other frozen
   artifacts. Older preparation objects lacking these inputs reject.
- `native/target.c`: AArch64/x86-64 PIE synthetic target with explicit pre/post instruction
  labels, 1,050 same-value effects and separate ordinal transcript; nested fake-VM
  prototype/PC markers; helper heartbeat and fixed-frame Unix-stream cases.
- `native/observer.c`: fixed owned inner TID 1, no caller address/PID, zero-capability
  checks, exact target ELF hash and executable-mapping load bias, SEIZE/EXITKILL,
  hardware execute-breakpoint candidate and fixed single-step/re-arm. TRACEEXEC
  exists solely to reject exec, never follow it. Errors are closed/value-free;
  output is nonblocking. No software patch, GPR write, C1 fallback or Redis code.
- `profiles.py`: per-architecture default-deny OCI profiles and an additional deterministic
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

`native/admission.h` collects fixed-path Linux/AArch64 or Linux/x86-64 startup observations in the
target and observer, after case selection and a nonzero 64-hex invocation/scope
nonce. It emits four canonical frames, each still limited to 1,024 bytes:

1. **IDENTITY:** explicit architecture, executable SHA-256, boot identity, namespace inodes/device,
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
profiles; policy setters remain excluded. The oracle's separate mapping expansion
is described below.

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
reject dispatch. All readiness polls, including the oracle's, precede the final
own/peer admission and context-lease expiry checks at the write boundary. The
oracle also rechecks its expiry after its poll. A failed pre-dispatch join clears
all three cached admissions. The stream checks its deadline immediately
before each write. Collection performs final live-ready checks after metadata /
authorization work. These checks observe currently available loss; they do not
guarantee future channel lifetime atomically. No Docker inspection is added to
the short HELD/resume path.

Typical integration order (only under the outer execution authorizer): acquire
trusted context; open the target and send case/nonce/Linux expiry, then collect
target admission; do the same for the observer; open/admit/initialize the oracle;
arm/receive ARMED; start the target.
The admission collector accepts only backend-owned streams and complete resource
snapshots. Failure closes the local stream and raises a value-free error; independent
watchdog cleanup and remote absence proof remain mandatory.

The compact digest summary is not a transferable capability. Both validation and
collection retain `execution_authorized=false` and `runtime_evidence=false`; injected
Docker/stream fixtures are explicitly labelled simulated. A pre-case snapshot does
not establish confinement/resource usage for a whole trial, held timing, hardware
breakpoint precision, or complete trial acceptance. The oracle worker and its
bounded client-closure bridge are described below; deployment/calibration remain gated.

## Independently acquired Linux context

`runtime_context.py` supplies an authenticated acquisition protocol and an expiring
`ContextLease`. `context_worker.py` is the fixed, read-only Linux-host endpoint;
`linux_observation.py` reads bounded, fixed procfs/sysfs leaves. It never enters a
namespace, changes policy/credentials or substitutes the controller's macOS clock.

The source is an **operator-provisioned trust root**, separate from all three
actors. Its root-owned `/etc/obs1/context-source.json` fixes the Docker daemon
identity, collector source-bundle hash, Linux boot hash, host namespace/device
anchor and expected OCI/filter counts. The 32-byte authentication key lives in
mode-0600 `/etc/obs1/context-source.key`, owned by the context service's user.
Provisioning must independently attest that anchor in the Docker daemon's actual
Linux environment; actor receipts must never populate it. The endpoint refuses
if its actual boot/namespaces differ, or if the selected Linux ABI, zero time offsets,
LSM/Yama access or stable observations are unavailable. No privileged fallback,
host-namespace Docker role or policy-setting helper is introduced.

The controller's `ContextSource` sends a fresh challenge, case/scope/preparation
binding and a bounded validity duration, authenticated by HMAC-SHA256. Three
canonical observation frames plus a final MAC are individually capped at 1,024
bytes and charged to the shared wire budget. The key is never sent to actors or
included in exported receipts. Wrong source/daemon/anchor, bad MAC, replayed or
reordered frames, mid-read drift, expired responses and foreign leases reject.
The source's provisioning and Mac-to-Linux transport remain explicit outer
integration obligations: pass a bounded transport for the source's stdio exchange;
an arbitrary actor stream is not an authenticated host source.

### Operator-provisioned pinned-SSH tooling

`context_provision.py` creates and validates a **new private staging bundle**;
`context_transport.py` supplies the concrete bounded SSH exchange;
`context_ssh.py` is the fixed server forced command. The owner selected tooling
for an operator-managed Linux/AArch64 Docker host. Actual installation and live
SSH admission await identification of that host. The current Docker Desktop
LinuxKit VM has no collector endpoint established by this work.

The collector must share the selected Docker daemon's actual Linux kernel and
host namespaces. A collector on another server cannot supply context for Docker
Desktop. The backend still selects only `unix:///var/run/docker.sock`; this
increment does not switch daemons, add remote Docker endpoints, enter namespaces
or install a privileged helper. The deployment target/daemon pairing must be
admitted before the new transport is used for a real trial.

The closed deployment object contains:

- `version: 2`, canonical `architecture`, and an independently admitted version-2
  `anchor` with the same architecture;
- a canonical numeric IPv4/IPv6 `host` and integer `port` (no DNS names/options);
- dedicated `service_uid` / `service_gid`, neither root nor actor UID/GID 999;
- exactly one out-of-band verified `ssh-ed25519` host key and client public key,
  each in `ssh-ed25519 BASE64` form without comments/options;
- reviewed `ssh_client_sha256` for controller `/usr/bin/ssh` and
  `host_python_sha256` for the host's forced-command interpreter.

The tool validates these supplied values; it does not discover, infer or approve
host namespace anchors or SSH host keys. Deployment JSON must use the existing
canonical JSON encoding. Keep it and the unencrypted OpenSSH identity in an
owner-only mode-0700 directory, each mode 0600. Materialize a new destination:

```sh
python3 -I -B tests/crawl-jobs-v2-observer/context_provision.py \
  --deployment /absolute/private/deployment.json \
  --identity /absolute/private/id_ed25519 \
  --output /absolute/private/new-context-bundle
python3 -I -B tests/crawl-jobs-v2-observer/context_provision.py \
  --check-bundle /absolute/private/new-context-bundle
```

The generator creates separate `host/` and `controller/` payloads. It generates
a fresh 32-byte HMAC key unless one is supplied to the Python API, and places
copies only in the private host/controller key files. The SSH private identity
stays in `controller/`; it is never in the host payload. Public manifests/receipts
contain neither secret values nor secret-file hashes. Existing output paths are
refused; failed partial staging directories remain private and are not installed.
`--check-bundle` verifies exact source/config bytes, policy, file inventory,
ownership/modes, matching nonzero HMAC keys and the closed manifest.

#### Host installation contract (operator step)

The staging bundle is not an installer and performs no privileged OS mutation.
On the independently identified test host, the operator must:

1. Establish a dedicated `obs1-context` account with the admitted UID/GID, no
   additional groups, no password authentication and no writable startup files.
   Verify root ownership/non-writable, non-symlink ancestors of the install paths.
2. Install exactly the seven `host/code/` source files under
   `/opt/obs1/context-source/`: directory root-owned 0755, files root-owned 0444.
   Extra modules or bytecode files are refused; the interpreter uses `-I -B`.
3. Install `host/etc/` under root-owned 0755 `/etc/obs1/`. Public anchor/service/
   authorized-key files are root-owned 0444. `context-source.key` is owned by the
   service UID/GID with mode 0600 and exactly 32 bytes. Never mount it into actors.
4. Review/install the root-owned `sshd_config.fragment` as
   `/etc/ssh/sshd_config.d/obs1-context.conf`, with its `Match User obs1-context`
   restrictions. Confirm the server includes that directory and validates with
   `sshd -t`; collect the effective `sshd -T -C ...` configuration for this user
   and actual client address. `validate_sshd_output()` checks the required policy
   and global `permituserenvironment no`; validating supplied text alone is not
   proof that the live daemon loaded it. Reload belongs to the operator's change.
5. Verify collector placement and permissions without adding capabilities,
   changing Yama/LSM policy, mounting securityfs, or joining host namespaces.
   Missing observations remain a refusal.

Both SSH configuration and `authorized_keys` fix the command to
`/usr/bin/python3 -I -B /opt/obs1/context-source/context_ssh.py`. The entrypoint
requires original command `obs1-context-v1`, the admitted service identity,
matching service/anchor architecture, exact root-owned code inventory and
architecture-checked interpreter digest. It then delegates to
the bounded signed context worker. It does not offer a shell or caller pathname.

#### Controller preflight

The controller payload remains owner-owned 0700 with mode 0600 files. Bind the
preparation's new `runtime_context_deployment` artifact to
`deployment_digest(deployment)` and `runtime_context_transport` to
`transport_digest()`, in addition to the host source bundle. Old packets require
regeneration; ordinary validation does not grant execution authority.

`DockerBackend.acquire_provisioned_context(bundle, preparation, budget, deadline,
authorize_source=...)` admits these bindings before actor activation, preserves
the existing outer authorizers and selected-daemon checks, and returns the
existing expiring lease with a secret-free provisioning receipt. Actual host
installation/SSH admission is not inferred from possession of bundle files.

SSH uses fixed `/usr/bin/ssh`, explicit pinned known-host data, the supplied
identity, isolated configuration, no agent/password/interactive authentication,
no proxy/control sockets/local command/forwarding, one connection attempt and
the existing deadline. It launches without a shell or ambient SSH environment.
Each exchange uses private credential copies and checks credentials/client
identity before/after. Both pipes are nonblocking and bounded: request 1024 bytes,
response 4096 bytes; stderr, nonzero exit, partial/extra frames, hung peers and
unsettled EOF refuse. Local process cleanup is bounded and never remote-stop proof.

Tests use real benign Python pipes and owned files, simulated Linux/metadata,
and the real client's offline `ssh -G` configuration output. They do not establish
live SSH authentication, installation on Docker Desktop, or native calibration.

`DockerBackend.acquire_runtime_context()` must run before actor activation and
rechecks the daemon around acquisition. It binds the returned lease to this
backend/scope. Local elapsed-time expiry is a conservative transport budget, not
an equality between clock epochs. **The native handshake now has three lines:**
case ID, 64-hex scope nonce, and the exact unsigned-64-bit Linux `not_after_ns`.
The actor checks that deadline in its own kernel clock, including after reading
a control byte. Thus controller sleep or clock-domain differences cannot extend
native A/G admission. The backend joins the transmitted window to the validated
context and rechecks the lease at the write boundary. Fresh native artifacts are
required; old binaries/preparation packets are not compatible with this handshake.

## Oracle worker and client closure

`worker.py` implements the reserved `/oracle/worker.py` entrypoint.
`oracle_worker.py`, `oracle_protocol.py` and `oracle_bridge.py` provide a bounded,
case/scope-bound INIT → HELD → FINAL → CLOSE exchange, with sequence/predecessor
checks. The worker emits independently checked startup observations, including
its interpreter ELF and worker-source bundle identities. Its private PID namespace,
32 MiB memory limit, eight-PID ceiling, UID/GID 999, five zero capability sets,
NNP, seccomp and kernel/clock observations are admitted against the same trusted
context. The oracle uses the target's expected base OCI-filter count; only the
observer adds a self-installed filter.

The worker independently admits `/witness/state` through the read-only volume,
checks regular/single-link identity, ownership, mode and fixed 8,480-byte size,
then applies the existing full ordinal/transcript and final-state validators.
No caller pathname or PID is accepted. Opening is nonblocking/no-follow and
identity/permissions are checked again after the actual reads.

For the three P3 stream families, a read-only client connects only to
`/control/frame.sock`, checks its inode/owner/mode and peer credentials, and reads
the fixed 1,024-byte `C` frame. It never sends application bytes to the target.
Fragments, complete receipt time, EOF, reset/error, late completion and unresolved
retirement stay distinct. The held witness supplies a lower cut bound; the
oracle's causal post-HELD read supplies an upper bound in the same Linux clock.
Only a receipt strictly before the lower bound gets timestamp-based ACK credit.
Equality, including a zero-width cut interval, remains unordered. Late completion is retained even
when the case was affirmatively pre-acknowledgment at the cut.

`DockerBackend.open_oracle()` binds an owned, admitted worker and its initial
witness receipt. Target G requires this live initialized oracle. After receiving
the owned observer's HELD frame, `OracleSession.advance_held()` joins its digest
and ordinal to the actual oracle result before advancing the coordinator. R
requires that same live oracle match. These short held/resume checks add no Docker
inspection. `observe_final()` checks final witness evidence; `close()` performs a
bounded client drain and retains partial/error outcomes. `closure()` separately
joins two stable inspections of the original target/oracle generations as exited
with PID zero. Socket/CLI EOF alone never proves remote stop. A closed no-reply
window additionally requires a proved pre-ACK cut, clean client EOF and no complete
reply; unresolved/reset paths cannot receive that credit. Resource disposal and
independent absence proof remain the watchdog's responsibility.

The controller independently retains `ClientHistory` across accepted replies.
Byte counts cannot decrease; completion timestamps and terminal EOF/error facts
cannot disappear or change. Only a HELD response introduces a cut, its classification
is immutable, and subsequent late-completion flags are derived from the retained
cut bounds. No-reply credit additionally requires the successfully matched owned
HELD digest. A correctly hashed chain of contradictory summaries poisons the
session rather than replacing earlier evidence. Oracle operation deadlines are
also rechecked after the resume-readiness poll.

`test_context_oracle_review.py` retains the review counterexamples and positive
controls: final-poll expiry, tied timestamps, completion/error rollback and a
fabricated cut, plus honest partial/late/reset/unresolved/non-stream outcomes.
It uses synthetic admission/stop metadata, deterministic clocks and benign Python
pipes/sockets. Exact review findings and verdicts are recorded in the
[review checkpoint](../../docs/crawl-jobs-v2-m4-c0-context-oracle-review-2026-10-09.md).

The oracle's OCI profile admits CPython's **read-only file-backed** executable
mapping, while anonymous executable memory, W+X, executable `mprotect`, ptrace and
namespace setters stay denied. A separately pinned Python image recipe is provided
in `Dockerfile.oracle`; artifact generation runs only `build_oracle.py`, never the
worker. Its real OCI/runtime behavior still needs separately approved admission
and calibration. Synthetic client receipts do not themselves become Redis/D02 ACK
or durability evidence.

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
docker build --network none --pull=false --platform linux/arm64 \
  --file tests/crawl-jobs-v2-observer/Dockerfile.oracle --target artifacts \
  --output type=local,dest=/absolute/private/another-empty-directory \
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

The resource, stream, watchdog, independently authenticated context acquisition,
three-role admission and oracle/client-closure components now exist. Remaining
work includes trusted Linux-source provisioning/transport in the actual Docker
kernel domain, reviewed runtime images and admission, full trial dispatch,
complete per-trial receipt/timing assembly and whole-trial confinement evidence.
The outer authorizer must be bound to the separate reviewed approval/execution
decision; no approval issuer or complete `run` CLI is provided by this layer.

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
