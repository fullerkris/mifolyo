# C0-ARCH-03 independent security review

**2026-10-12 — GO_FOR_CONTINUED_IMPLEMENTATION**

No in-scope blocking security finding was reproduced. This decision applies to the reviewed **source/static artifacts only**. It does **not** authorize native execution, deployment, publication, kernel/tracing/timing acceptance, or M4 acceptance.

## Scope and integrity

- Repository branch: `feature/crawl-jobs-v2-c0-x64-runtime`; HEAD/base: `d8016469a5ffcceeb04329deec4e3dea2c72dd97`.
- Frozen snapshot: `/tmp/opencode/m4-c0-x64-runtime-2026-10-12/snapshot`.
- Source inventory SHA-256: **`7ed3b955be1fe4d2621a43896d1a3e4d49cf9edcf6e86a0c2dd567d105f14720`**.
- All **51 package files plus the runner** matched the inventory in both snapshot and repository before and after review. All **130 files** under the supplied `final-build/` tree and Git HEAD remained unchanged.
- Whole-repository Git status changed concurrently for four documentation paths outside the frozen inventory. The original strict whole-status check is preserved in `integrity-after-initial-global-status.*`; the final scoped check explicitly records this distinction. The reviewer did not write those paths, did not modify repository/snapshot/build artifacts, and did not commit, push or deploy.

All source references below are relative to `tests/crawl-jobs-v2-observer/`. New probes, transient fixtures, binaries and reports were written only inside this review's `security/` directory.

## Independent evidence

| Verification | Result |
|---|---|
| Frozen original runner, independently invoked under umask 077 | **256/256 PASS**, 13.670 s; no failures, errors, skips, expected failures, unexpected successes or ResourceWarnings |
| Existing authorization/expiry tests replayed with x64 fixture data | **31/31 PASS**; all 57 constructed backend scopes independently asserted `x86_64`; production source and test assertions unchanged |
| Reviewer interpreter over BPF extracted from the actual x64 observer ELF | **158,796 vectors PASS**; not just generator output |
| Independent interpretation of emitted OCI ptrace rules | **156,701 matching vectors PASS**; also 192 full-width TLS-operation and 29 forbidden-syscall checks |
| Malicious actual-filter/sidecar, ELF and manifest substitutions | **29 refusals**, all before compiler metadata queries or toolchain-output creation |
| Cross-ABI/input/provenance/authorization probes | **313 expected refusals**, **24 positive controls** across both ABIs |
| Permitted `native/test_x86_debug.c` | **29 in-memory controls PASS**; compiled locally with warnings-as-errors; undefined-symbol inspection shows no ptrace import |
| ARM64 continuity | All three profile byte streams and the 272-byte self-filter exactly match merged-base generation |

Evidence: `tests.{json,log}`, `x64-guards.{json,log}`, `static-probes.{json,log}`, `runtime-probes.{json,log}`, `debug-probe.log`, `debug-undefined-symbols.log`, `continuity.json`, and `integrity-{before,after}.json`. The original 256-test report is deterministic and has the same hash as both owner reports; the new execution log and reviewer probes document the independent run. The additional x64 replay changes **test fixtures only**, including fake daemon metadata, not production guards or assertions.

### Coverage continuity

The merged base had 238 IDs. **233 remain**; the only five removed IDs are the owner-authorized obsolete x64-unimplemented cases, replaced by five current architecture-bound cases. Eighteen further x64 tests yield 256 total. **229 retained test-method ASTs are unchanged**; four retained methods adjust only the now-implemented x64 expectations while preserving foreign-ABI, malformed-ELF and ARM64-byte refusals. The exact IDs/AST comparison is retained in `continuity.json`.

Nineteen authority, context, transport, oracle, watchdog, resource and control files are byte-identical to the merged base, including the prior architecture-remediation guards. These unchanged files were not treated as proof by themselves: their critical boundaries were replayed with x64 inputs.

## Security boundary assessment

### 1. Syscall aliases, register writes and mode widening — PASS

`profiles.py:63–84,87–173` and the **compiled** self-filter enforce:

- Native `AUDIT_ARCH_X86_64` only; unsigned syscall numbers >=512 kill, including x32-bit calls, historical 512–547 compat aliases (521 included), and negative numbers. Native `execve` and `execveat` deny after self-filter installation.
- Full-width request, owned TID 1 and regset/debug-register offset selection. The permitted PEEKUSER offsets are exactly DR0..3, DR6 and DR7; DR4/5 aliases reject.
- POKEUSER writes only DR0 address, DR6=`0x00000000ffff0ff0`, and DR7=0/1. Tests cover all user offsets 0–919, all DR7 values 0–131071, every individual 64-bit selector/value bit and reproducibly randomized values.
- No x64 SETREGSET, GP-register writes, POKETEXT/POKEDATA, DR1..5 writes, data-watchpoint modes or global-enable widening. Unrestricted DR0 address data is intentional: the native caller selects a pinned target-manifest address plus checked load bias, not a caller-supplied address.
- The OCI layer remains default-deny. `ARCH_SET_FS` is the sole allowed arch_prctl operation. Target-only chmod and native readlink do not broaden ptrace authority. Executable mprotect, W+X and anonymous executable mmap remain denied; the previously authorized oracle-only read-only file-backed executable mapping exception is unchanged.

The self-filter supplements, and does not replace, the required OCI filter. OCI comparisons were interpreted offline; no libseccomp/runc or kernel-installed OCI bytecode is claimed.

### 2. Actual compiled filter and build provenance — PASS

Both actual ELF `.obs_filter` sections equal their symbol extents, sidecars and canonical generators, and reside in non-writable load segments. The x64 filter is **440 bytes / 55 instructions**, SHA-256 `b080f5d5047014d30d7f9e40280b6dd89f71abc029fb75081a7a8b40e1f2b88b`.

Static x64 disassembly independently shows `main` supplying `.obs_filter` at relative address **0x3ce5e0**, length **0x37**, to `prctl(22, 2, ...)`, checking failure, then collecting admission and waiting for the fixed `A` byte before SEIZE. Thus the evidence is not merely an unused correctly named BPF blob. See `x64-main-disassembly.log:359–385`.

Twenty mutations of embedded comparison constants **and matching sidecars together** still fail `BUILD_FILTER_ARCHITECTURE`; equal malicious bytes alone cannot pass record mode. Foreign target/observer ELFs, ELF32/big-endian/bad-version targets and stale/foreign/authorizing manifests also refuse. `build_tools.py:145–157` rechecks the target/manifest and canonical architecture-specific filter before querying the compiler.

All 22 supplied artifact hashes, 102 package-copy source hashes and 24 compiler-source input hashes were joined to the frozen inventory. Both native pairs are ELF64/LE static PIE of the selected machine, without PT_INTERP or writable-executable PT_LOAD, with non-executable stack and RELRO. Target hashes are embedded in the corresponding observer. Pinned GCC14.3/bookworm build records were inspected and their files rehashed; the reviewer did not rebuild or execute native actors.

ARM64 filter SHA-256 remains `701824ff5f8ccafa359f711d2714b23c8a303789cd1987a3a85aba19a3667b93`; profile hashes are retained in `static-probes.json`. Native ARM64 ELF hashes themselves change with this source revision and are **not** claimed unchanged.

### 3. LP64, signal/debug-state and executable identity — PASS within static scope

`native/architecture.h:3–14` rejects non-Linux, big-endian, ILP32 and unsupported builds and asserts pointer/long width. `native/x86_debug.h:7–13` asserts the LP64 debug offsets, 216-byte GP regset and DR6/local-enable constants. Capture checks every accessible debug register, not only DR0. Arm/reset and detach paths read back DR0/6/7 and require DR1..3 remain zero.

`native/observer.c:185–199,270–278,299–315` requires native regset length, CS=0x33, RIP, SIGTRAP/stop-event shape, TRAP_HWBKPT signal address and exact DR6/DR7 state. The supplied simulation rejects dirty initial state, extra DR6 causes/high bits, altered addresses/control and corrupt readback, and verifies restoration. Disassembly confirms fixed TID 1, the readback and regset checks in the actual x64 artifact; see `x64-{request,breakpoint,debug-verify,stopped-pc}-disassembly.log`.

The existing exact target ELF hash/size, namespace/start-time and executable-mapping/load-bias joins remain at `native/observer.c:81–118,237–249`; native admission still hashes `/proc/self/exe` and joins it to the prepared role ELF (`native/admission.h:115–139`, `runtime_admission.py:98–147`). This is not proof of live hardware behavior, kernel stop semantics or timing.

### 4. Cross-architecture and oracle/context provenance — PASS

The independent probes reject opposite architecture at preparation, trusted context, actor binding, identity frame, peer and executable joins, as well as malformed privilege/resource/clock data. Every role's **create and direct-start** path rejects foreign/missing/alias/x32 image architecture, wrong OS/ID, missing image/config, and active OnBuild/healthcheck/environment/volume substitutions before simulated dispatch (`docker_backend.py:197–206,221–246,254–338`). Inspected foreign profiles reject too.

Deployment/anchor/preparation/bundle digests cannot be swapped between ABIs. Signed test-key responses with foreign ABI, request/boot/namespace, widened window, stale sample, missing/extra fields, reordered/duplicate frames or bad MAC refuse. Replay and callback-time public-anchor mutation refuse; absent/non-boolean approval and wrong daemon/source refuse before source I/O. Lease equality-at-expiry refuses. Test-key-signed counterexamples establish schema/join enforcement, **not** an unauthenticated HMAC forgery claim (`runtime_context.py:47–98,129–184`, `context_transport.py:158–184`).

The oracle now selects from observed Linux uname and pointer width, then checks its actual interpreter's bounded, stable ELF bytes (`linux_observation.py:42–51,113–136,168–194`; `oracle_worker.py:149–187`). Fragmented-header probes reject a foreign interpreter even when uname matches; x32/ILP32, i686, amd64 alias and non-Linux inputs refuse. Host collectors still use independently provisioned service/anchor/interpreter bindings, not actor-supplied architecture.

### 5. Authorization, expiry and evidence authority — PASS within offline scope

The 31-case x64 replay includes final-authorizer revocation/latency, own/peer expiry, sender-pump expiry, expiry during the final oracle readiness poll for target/observer/oracle/context lease, EOF/partial/stderr/early-HELD output, buffered frames after restart, missing native admission, absent/lost oracle, missing held match, poisoned held evidence and process-generation/closure substitution. Invalid dispatch sends no `A`/`G`; failed pre-dispatch joins clear cached admissions. Oracle/source mismatch never initializes a session. Exact cleanup/closure proof is still required; a local pipe/CLI EOF does not prove remote absence.

No reviewed return value converts preparation/context/oracle observations into execution authority. External approval, live admission and the independent cleanup obligation remain required. The three-line handshake and existing output/event/read budgets remain intact.

## Findings and limits

**Blocking findings: none. Remediation required for this source/static gate: none.**

1. **C0-ARCH-04 remains open.** `Dockerfile.oracle` is intentionally still ARM64. Builder API support and synthetic x64 interpreter checks do not establish the complete dual-runtime images, loader/dependency closure or actual image contents. Regenerate architecture-bound preparations, host/deployment bundles and oracle manifests for this revision; current source bundle hashes are recorded in `continuity.json`.
2. **C0-ARCH-05 remains open.** No actual ptrace, native target/observer execution, filter installation, real collector, live oracle against host state, Docker/Podman workload, live SSH, kernel-policy change or timing/calibration test occurred. Worker-main tests used simulated Linux/witness readers and benign pipes. No claim is made about actual DR6 behavior, attach feasibility, non-emulated execution, LSM/Yama, installed filter bytes or stop precision.
3. Owner/operator configuration, compiler and authenticated context source remain trust roots. A hash/join check is not a sandbox against arbitrary trusted Python callbacks or a compromised root/compiler; static reads are not atomic attestation of concurrently writable artifact directories. No full dependency-CVE/SBOM audit or formal proof is claimed.
4. This review assesses exploitable widening/provenance boundaries, not the separate correctness role's instruction/witness algorithms. A different inventory or artifact set requires a fresh gate.
5. One initial reviewer assertion incorrectly required the ARM64 filter's load segment to be R-only. The actual segment is **RX, not writable**, as permitted by the pinned linker. The probe was corrected to test non-writability; the initial failure log is preserved. No product control was relaxed.

`review.json` contains the machine-readable decision, evidence hashes and exclusions. `SHA256SUMS` binds the complete retained review directory, including probe sources and reports.
