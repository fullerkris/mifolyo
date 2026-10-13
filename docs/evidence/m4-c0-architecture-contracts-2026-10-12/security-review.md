# Independent security review — C0-ARCH-02

**Date:** 2026-10-12  
**Verdict: GO_FOR_CONTINUED_IMPLEMENTATION**  
**Scope:** frozen source and supplied static artifacts only. This is **not** publication approval, live-host admission, native-execution authorization, or full M4 acceptance.

## Findings

**No new in-scope exploitable security finding identified. No remediation required for this increment.** This is a bounded assessment under the documented trusted operator/root/controller model, not a claim that the entire runtime is secure or ready.

## Reviewed identity and integrity

- Branch: `feature/crawl-jobs-v2-c0-architecture-contracts`.
- Base/HEAD: `ced6e1cff36101f76fb54904b6b0fa2cf33cb48f`; reviewed edits remain uncommitted.
- Source inventory SHA-256: `b5388bbdf282e286caec43f6a5e12bc5e0129a6630f43bd2c2b15b405b0d7bfb`.
- Original runner SHA-256: `9afb715125f734f5b905da63dde5c9787fc5997b2e5a73a0bf62d781b23aef84`.
- Independently checked **before and after**: all 46 package files, exact package inventory/no extra bytecode, original runner against merged-base and working-tree bytes, working-tree source equality, tracked delta equality, all 11 native artifacts, oracle manifest/interpreter, three source-bundle digests, and both platform descriptors/digests. All matched; no drift.
- The supplied local verification history was not used as a substitute for these checks or for the independent test run.

Evidence: `integrity-before.json`, `integrity-after.json`, `verify_integrity.py`.

## Independent verification

The original runner was invoked **once**, from the frozen snapshot, under **umask 077**:

```sh
TMPDIR=/tmp/opencode PYTHONDONTWRITEBYTECODE=1 \
python3 -B -W error::ResourceWarning scripts/test-crawl-jobs-v2-observer.py \
  --report /tmp/opencode/m4-c0-architecture-contracts-2026-10-12/security/tests.json
```

Result: **229/229 PASS**, 13.848 seconds; no failures, errors, skips, expected failures, unexpected successes, or ResourceWarnings. Compared test IDs directly against the merged-base source: **all 202 prior IDs preserved and passed**, with exactly 27 added architecture tests. No independent second full-suite/umask-022 run is claimed.

Additional private probes all passed:

| Boundary | Evidence and result | Source references* |
|---|---|---|
| Closed schemas and platform binding | 101 negative preparation/scope/anchor/deployment cases: legacy/missing versions, aliases, type coercions, duplicate/extra fields and stale/relabelled platform digests reject. Recomputed x64 preparation/deployment metadata remains non-authorizing; x64 resource scope rejects. | `contracts.py:106–152`; `owned_resources.py:37–64`; `runtime_context.py:26–39`; `context_provision.py:35–53` |
| Authenticated context | 17 response mutations, five request mutations, and replay reject. Re-MACed wrong/missing architecture and v1 frames still fail schema/join checks; unchanged MAC cannot conceal tampering. Requests reject before simulated kernel reads. | `runtime_context.py:47–97,125–176` |
| Reusable source and lease isolation | 12 copied/in-place acquisition mutations reject before authorization/exchange; matching reuse gets fresh challenges. Caller mutation during a callback cannot change the preparation snapshot. A modified returned context copy cannot relabel the lease. | `context_transport.py:158–183`; `runtime_context.py:109–114,125–143` |
| Unsupported architecture activation | x64 resource/backend, role/profile/filter, all three native builder modes, oracle builder, Linux reader/endpoint and process stream refuse. Direct `SSHContextTransport.__call__` refuses before credential reread, SSH-client verification or process I/O. Metadata acceptance does not activate x64. | `contracts.py:120–123`; `context_transport.py:136–155`; `streams.py:76–85`; `build_tools.py:79–83`; `build_oracle.py:10–16` |
| Native admission join | All 48 architecture combinations across preparation/context/stream binding/IDENTITY for the three roles: only the three all-ARM64 controls pass. Including legacy/type/missing cases, 96 negative validations reject. Backend mixed input/frame cases close the owned simulated stream and do not cache admission. | `runtime_admission.py:28–37,98–173`; `docker_backend.py:356–449` |
| Host service/anchor/interpreter | Nine owned simulated-install cases verify matching success plus service/anchor mismatch, alias, both-x64, missing/legacy fields, wrong digest and even a correctly pinned x64 ELF refusal. Fragmented ELF reads retain header checks and close descriptors on all tested outcomes. Foreign uname refuses before namespace reads. | `context_ssh.py:14–32`; `linux_observation.py:38–40,78–92,102–125` |
| Docker/image/profile confusion | Foreign or Linux-name-in-OCI-field daemon/image metadata refuses before create. Wrong-role/x64 profile files and 12 mixed/empty/alias profile architecture sets refuse. Full-suite start/lease/oracle tests also pass. At start, the backend checks the daemon and scope-bound container/image identity; immutable-image creation provenance remains with the trusted creator/controller. | `docker_backend.py:157–167,183–195,210–242,251–277,451–474`; `owned_resources.py:114–131,159–228` |

\* Paths are relative to `tests/crawl-jobs-v2-observer/` in the frozen snapshot. Probe sources/results: `probe_security.py` / `probes.json` (nine groups, **280 expected `Invalid` refusals**), `probe_host_join.py` / `host-join.json`.

## Fresh static artifact verification

The supplied ELFs were **read as bytes, never executed**. An independent `struct`-based ELF reader checked headers, program/section tables, symbol locations, executable ranges, writable witness storage, NX stack, RELRO and absence of writable/executable load segments. Target/observer are little-endian ELF64, EM_AARCH64 (183), ET_DYN static PIE with no PT_INTERP.

| Artifact | Verified SHA-256 |
|---|---|
| Target | `07ae68522b7e87560bda4d32fcb0f244c74fd748f7cd9ccfe1ac3cf2321deab5` |
| Observer | `53800d2281f57341269a0f1c9686809883241ed01872338b0544bf0439e88120` |
| Oracle manifest | `6168e220727cb6377e8e637740bb8c297bf7bee929ab19aac6db90aa6fbcbfa5` |
| Oracle interpreter | `cb4548d280f38c2b1df4cba45f12c90a034729e08fce5bf1a223c58add0e21e3` |

- Target effect bytes are `d2800544 f9000064 f9000022 c89ffc02 d65f03c0`: `mov x4,#42; str x4,[x3]; str x2,[x1]; stlr x2,[x0]; ret`. Both marker functions contain `nop; ret`. Checked symbol addresses/offsets against the manifest and generated observer header; observer contains the exact target digest.
- Observer's `obs_filter` symbol and `.obs_filter` section equal all **272 bytes / 34 instructions** of the generated BPF. **3,092 simulated BPF cases** include foreign audit-architecture kill, exec deny, owned TID restriction, register-write restriction and high-word confusion rejection. No filter was installed.
- All three generated ARM64 profiles, filter bytes, case table and target header match. `probe_continuity.py` independently confirms profile/filter byte equality with the merged base, not merely the author's expected hashes.
- Manifest/platform/ELF hashes join correctly. Toolchain record's nine source inputs and oracle manifest's seven worker-source inputs equal the frozen files and bundle digests. Native compiled strings carry architecture and version-2 admission frames. Source retains the AArch64/little-endian compile guard and 64-bit pointer assertion (`native/admission.h:3–6,244–248`).
- Oracle ELF is also EM_AARCH64/ELF64/LE, with `/lib/ld-linux-aarch64.so.1` PT_INTERP; this is **not** validation of its loader/shared-library closure or runtime feasibility.
- Relabelled target machine bytes refuse; requesting x64 parsing/manifest generation refuses with `ARCHITECTURE_NOT_IMPLEMENTED`.

Evidence: `probe_artifacts.py`, `artifacts.json`, `probe_continuity.py`, `continuity.json`.

## Earlier security corrections and unchanged controls

These are **verification of existing fixes**, not new vulnerability findings:

- **COR-SSH-001 preserved:** selector allocation precedes spawn; injected allocation failure launches no child. `context_transport.py:64–71,120–122`.
- **COR-SSH-002 preserved:** tested all three nonblocking and all three registration failure positions with a real owned benign Python exited-but-unreaped leader plus pipe-holding child. Each performs group kill before reap, closes all three pipes and the selector, and yields actual inherited-pipe EOF. FD delta: **0**. The critical condition remains `if setup_complete and not mux.get_map():` (`context_transport.py:104–122`). This is not remote-container absence proof.
- **COR-SSH-003 preserved:** private admitted provisioning bindings and a deep-copied preparation are checked on every acquisition before authorization/I/O (`context_transport.py:158–167`). Architecture changes cannot bypass those checks. Original public-receipt-mutation regression also passed in the full suite.
- **QA-ENV-001 preserved:** the simulated 0755 installation is explicitly chmod'd beneath its private 0700 parent under umask 077; production ownership/mode checks are not relaxed (`test_context_provisioning.py:230–257`). Independently exercised with owned files in `host-join.json`.
- `exchange_process`, per-acquisition provisioning checks, SSH options/credential readers, cleanup predicate, input control functions and the Python three-line handshake have unchanged function ASTs against base. Native target/observer/common code and watchdog are byte-identical; native nonce/expiry and emit-bound code are unchanged. Shared `LIMITS` and version-1 control messages are intentionally retained. Version-1 **admission** frames are rejected.

## Trust model and remaining gates

1. Operator/root and trusted controller code remain the trust root. Arbitrary mutation of private Python fields, replacement of trusted callbacks, fabrication of trusted context dictionaries, or malicious root/Docker-socket control is not an untrusted data-boundary bypass. Public receipts/summaries do not grant execution authority.
2. Header classification alone deliberately does not prove a valid runnable ELF, payload authenticity, image-content closure, native/non-emulated execution or runtime load bias. Image labels and uname under an operator-selected/emulated environment cannot replace the deferred live-host acceptance work.
3. x64 support is **metadata only** until C0-ARCH-03. No Podman runtime adapter was introduced. ARM64 filter/syscall/instruction bytes are not reusable x64 runtime implementations.
4. Independently identified host/daemon placement, installed source/interpreter ownership, effective live sshd policy, decoded key-pair/live SSH authentication, actual runtime image/dependency admission, LSM/Yama/seccomp availability, native breakpoint/calibration/timing and independent remote cleanup/absence proof remain open gates. No weakening of host policy or privilege fallback is recommended.
5. This review did not rebuild images, remeasure compiler libraries, execute a native actor/oracle worker/real collector, contact live SSH, launch a Docker/Podman workload, install BPF, trace, calibrate, or mutate host policy. The suite's `ssh -G` is offline configuration inspection. Only owned benign Python processes/files/pipes/sockets and simulated metadata were used.
6. No repository, frozen source, docs or Obsidian changes; no agents spawned. All reviewer outputs are confined to the private `security/` directory. Future source changes require a new integrity-bound review/test gate.

## Retained evidence hashes

| File | SHA-256 |
|---|---|
| `tests.json` | `c426dc3172d90a4592cfbf8549092ed17c11e79b2245589d65308838be884eaa` |
| `tests.log` | `d6e2223c1f19e9387940ee5995e726c14897fbf55f35641160170d3dc0733841` |
| `probes.json` | `9eb0aff82a8983a066c9a59c8bbc503db32935cf37f02c2bd7c058210e795f65` |
| `artifacts.json` | `aaab74f0eee4743568a9b90051811f3f2f5eb2246399745068d58ee4b2ba5c26` |
| `host-join.json` | `24c62c6296d2c6c4421df37b15ad859154789b07c9c2d86023c30b12b0a443a5` |
| `continuity.json` | `fb9b68bb44629bd8131651380dc5152d522766434d2b46e1d8bd83c8b9e78bdf` |
| `integrity-before.json` | `cd7f4dd1d44a4dca215140faf11653942ecf7eacffc3d9a62225f686afbb6671` |
| `integrity-after.json` | `cf5834cb0cabc8a2db5e375cdeeca45fa9a671b3c2a0131b6f4a09741f4e82cc` |

`review.json` contains machine-readable results and evidence hashes; `SHA256SUMS` also binds the reports and probe sources.
