# C0-ARCH-03 x64 correctness review

**Verdict: GO_FOR_CONTINUED_IMPLEMENTATION**

No actionable correctness defect or blocker was established in the reviewed source/static-artifact implementation. The closed instruction decoder, debug-state readback, architecture-bound admission, and unchanged ARM64 confinement bytes are good safeguards. This is **not native-runtime acceptance or permission to execute/deploy**.

## Identity and scope

- Date: 2026-10-12. Branch: `feature/crawl-jobs-v2-c0-x64-runtime`.
- Base: `d8016469a5ffcceeb04329deec4e3dea2c72dd97` (merged PR26).
- Source: `/tmp/opencode/m4-c0-x64-runtime-2026-10-12/snapshot/`.
- Inventory SHA256: `7ed3b955be1fe4d2621a43896d1a3e4d49cf9edcf6e86a0c2dd567d105f14720`.
- All 51 package files plus the runner matched the inventory in both snapshot and working repository, before and after review. The tracked diff and all 22 declared static-build outputs remained unchanged.
- Review files/probes were written only in this `correctness/` directory. No repository/snapshot edits, commits, pushes, deployment, ptrace calls, filter installation, or native actor/host collector/real oracle-worker runs were performed. The permitted test suite uses simulated observations and benign processes.

## Findings

**🔴 Blockers:** None found. **🟡 Suggestions requiring remediation:** None established.

The result is deliberately scoped: passing the controls below does not establish hardware breakpoint availability, actual stop semantics, timing acceptance, or complete runtime images.

## Review coverage and independent controls

Paths below are relative to `tests/crawl-jobs-v2-observer/`. Evidence paths are relative to this report's directory unless stated otherwise.

### COR-X64-CHECK-01 — Instruction boundaries and witness stores: PASS

Reviewed `native/target.c:23–35,126–135` and `build_tools.py:54–105,129–144`.

The **actual supplied x64 target ELF** has `obs_effect`/pre at `0x6ecf0`, stores at offsets 0/7/10, and post `ret` at `0x6ecfd` (offset 13). VM/send markers are `nop; ret` at `0x6ed00`/`0x6ed08`. The exact 14-byte effect is `48c7012a000000488916488917c3`; manifest widths are 7/1/1/1, not fixed-width ARM instructions.

Disassembly at the call site (`0x6eb06`) independently confirms SysV arguments: completed=`witness+8`, transcript starting at `witness+80` and advancing by 8, ordinal in RDX, last_value=`witness+72`. The three aligned stores publish last_value, then transcript, then completed; ordinary aligned MOV stores provide the intended x86 release ordering. This is static ABI/ordering analysis, not a hardware measurement.

All **144 one-bit mutations** of the actual effect and marker bytes were rejected by manifest generation. Separately extracted production `instruction_check()` was exercised for every width 1–8 at every offset modulo 8, including cross-word reads, selected-bit corruptions, outside-instruction changes, overflow and read-budget refusal.

Evidence: `offline-controls.json`, `target-objdump.txt:1938–1986,2099–2115`, `helper-probe.json`, `helper-provenance.json`.

### COR-X64-CHECK-02 — Debug-register protocol and observer integration: PASS

Reviewed `native/x86_debug.h:7–54` and `native/observer.c:127–156,185–208,253–318` against Linux v6.18 ptrace/debug exception source.

Capture requires inactive DR0–3/DR7 and reset DR6; programming disables DR7 before changing DR0/status, limits writes to DR0/6/7, and verifies readback. Hardware-hit and step statuses are distinguished. Restoring the admitted state precedes single-step or detach. The observer checks event/signal kind, hardware-stop RIP and signal address, native regset size/CS, and non-return step advancement. The post marker is a verified `ret`, so its step is intentionally not tested as RIP+1. All production requests retain fixed TID 1, deadline checks and correct `-1`/errno handling for PEEK reads.

The supplied simulated C test passed **29 controls**. A separate harness extracted the exact production request/peek/instruction/regset helpers and included the original debug header. It passed **3,703 checks**: 2,440 request/instruction checks, 1,191 debug-state/failure/readback checks, and 72 regset checks. These include every bit of each admitted debug register at capture/hit/step, failures at each arm request, high-address PCs, CS corruption and short regsets. Its request callback models Linux's virtual-DR6 polarity; it has **no linked ptrace or seccomp symbol**.

Evidence: `test_x86_debug.log`, `helper-probe.json`, `helper_probe.c`, `helper-provenance.json`, `helper-undefined-symbols.txt`. The larger probe passed with undefined-behavior trap instrumentation. ASan/shared UBSan linking was unavailable on this host; see `helper-sanitizer-link.log` (not a source failure).

### COR-X64-CHECK-03 — LP64 and structure ABI: PASS

Reviewed `native/architecture.h:1–16`, `native/common.h:28–41`, `native/x86_debug.h:7–13`, and observer syscall assertions at `native/observer.c:21–26`.

The standalone probe checks 8-byte pointers/longs, 216-byte GP regset, RIP/CS offsets 128/136, debug offsets 848–904, 8-byte witness alignment, 8,480-byte witness size, and completed/last_value/transcript offsets 8/72/80. Compile-only controls reject ILP32, foreign OS, foreign machine and big-endian macro configurations. These complement the supplied pinned-toolchain successful builds; they are not foreign-ABI runtime tests.

Evidence: `helper-probe.json`, `abi-guards.json`.

### COR-X64-CHECK-04 — Actual artifact and confinement joins: PASS

Reviewed `build_tools.py:12–43,145–172`, `profiles.py:16–84,87–173`, and the architecture-specific Dockerfile stages.

An independent ELF reader checked both supplied build sets: ELF64/LE, expected machine 183/62, static PIE with executable entry, no interpreter, no writable/executable load or executable stack. All artifact hashes, manifests, generated-header values, embedded observer target hashes, toolchain ELF/source identities (12 inputs each), and build-package inventories (51 files each) join correctly. No independent actor rebuild was claimed.

The observer's actual compiled filter bytes match their exported files and canonical architecture generators: **272 bytes ARM64**, **440 bytes x64**. All three ARM64 OCI profiles and the ARM64 BPF digest match the base evidence; that evidence was itself compared to the base commit.

A new cBPF interpreter, separate from package tests, evaluated **26,232 ptrace inputs per layer** against both the actual embedded x64 BPF and **offline libseccomp 2.6.1 compilation/export** of the OCI profile. It covers request/TID/offset/value combinations and all single-bit perturbations of representative valid calls. It also checked **1,049 syscall numbers**, foreign audit architectures, x32-bit numbers, historical 512+ aliases, unsigned negative numbers, and narrow TLS/chmod rules. No filter was loaded. The DR0 address remains intentionally full-width; constrained DR6/DR7 values and scalar aliases cannot bypass either layer.

Evidence: `offline-controls.json`, `offline_controls.py`, `oci-*-compiled.bpf`, `provenance.json`, `final-integrity.json`; supplied `../final-build/{aarch64,x86_64}/build.json` and artifacts.

### COR-X64-CHECK-05 — Context/oracle selection and version-2 joins: PASS

Reviewed `linux_observation.py:38–51,89–103,113–134,168–195`, `oracle_worker.py:146–188`, `build_oracle.py:11–27`, `runtime_context.py:64–98,112–184`, `runtime_admission.py:28–44,98–151`, and `docker_backend.py:254–283,360–474`, including architecture propagation through resource scopes and streams.

Oracle selection uses observed supported Linux ABI and subsequently joins the interpreter ELF. Host readers retain the independently provisioned architecture rather than silently auto-selecting. Version-2 role, preparation, context, image/profile and peer bindings remain in the admission path.

An independently generated four-way architecture matrix across preparation/context/binding/identity accepted the **6 matching role combinations** and rejected **42 mixed combinations**. All **24 legacy version-1 role/phase frames** rejected. Another **32 simulated controls** cover signed foreign-architecture replies with recomputed valid HMACs, fully revalidated foreign peer receipts, foreign/class/endian interpreter headers, alias/ILP32 selection, lease mismatches, and anchored-reader refusal before procfs observations. No real collector or worker was run for these controls.

Evidence: `offline-controls.json`, `context-controls.json`, `context_controls.py`.

### COR-X64-CHECK-06 — Test continuity and integrity: PASS

The frozen runner independently passed **256/256** tests with no skips/expected failures. Both supplied umask reports are also source-bound passes. The base inventory has 238 IDs: **233 retained verbatim, exactly five approved replacements, and 18 additional tests**. No other old ID disappeared. The replacement map and full IDs are retained rather than inferred from totals.

Evidence: `offline-tests.json`, `offline-tests.log`, `offline-controls.json` (`test_continuity`), `provenance.json`, `inventory-before.json`, `inventory-after.json`, `final-integrity.json`.

## Limits and next step

- **C0-ARCH-05 remains unaccepted:** no real PTRACE_SEIZE/debug-register operation, seccomp installation, kernel/LSM permission test, hardware stop/step/restore, heartbeat execution, scheduling or timing calibration was attempted. In-memory state tests do not prove kernel behavior.
- **C0-ARCH-04 remains incomplete/outside this review:** `Dockerfile.oracle` is intentionally ARM64-only. The Python builder's x64 support is not a complete admitted dual-runtime image set.
- Offline OCI export tests used local libseccomp 2.6.1, not an admitted deployment's OCI compiler. Supplied build artifacts were inspected and hash-joined, not independently rebuilt here.
- Kernel v6.18 primary sources and seccomp(2) were consulted and retained under filenames recorded in `provenance.json`; they are references, not evidence about the host's running kernel.
- ASan could not link because local sanitizer shared libraries were missing. No packages were installed; the simulated helper passed UBSan trap-mode checks instead.

**Next step:** continue implementation under the existing gates. Keep image-set completion and separately owner-approved native acceptance as distinct work; this review does not authorize either execution or deployment.
