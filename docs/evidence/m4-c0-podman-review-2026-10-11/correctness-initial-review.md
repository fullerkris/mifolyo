# MiFolyo pinned-SSH C0 correctness review

**Verdict: NEEDS_REMEDIATION** — one reproducible local-cleanup blocker remains.

The selector-before-spawn correction resolves the stated **COR-SSH-001** allocation leak. The new implementation also has good closed schemas, value-free refusal handling, isolated SSH configuration, stable credential reads, and explicit separation of preparation/static evidence from runtime authorization. The complete offline suite passes, and the fresh ARM64 static artifacts join correctly to the frozen source. However, startup cleanup still conflates “nothing was registered” with “all pipes reached EOF.”

This is the independent **correctness** review, not the separate security review. No agents were spawned. No repository, frozen source, documentation, or Obsidian file was changed.

## Reviewed inputs and scope

- Frozen package: `/tmp/opencode/m4-c0-podman-arm64-2026-10-11/snapshot/tests/crawl-jobs-v2-observer`.
- All **45 package files** matched the supplied inventory before and after review, with no extra package files/bytecode. The copied runner was also hashed and verified unchanged.
- Inventory SHA256: `f53058a955de01027efcdbaa6adace7d6904197c845e5886b25e7d9db41bc227`.
- Retained full delta SHA256: `fe2748bb828e444a6236cb8d824232bfcc9678d09a6d02abd81f0ae8538499b1`.
- Baseline/commit labels supplied with the snapshot: `64c98a78e0e0ae1731f1516c06932bc881e8becf` / `1e956962596f7bb86143d45dd62ebf863c0b64eb`; findings refer to the frozen **uncommitted correction**, not just that commit.
- Compared the retained original transport to the corrected transport. The only package differences from the prior retained inventory are `context_transport.py` and `test_context_provisioning.py`.
- Read the full new provisioning, file-reader, forced-command and transport implementations; the package README; their preparation, context-worker, authenticated-source/lease and Docker-backend integration; and the provisioning tests. Inspected supporting deadline/framing/admission contracts and native build/profile/source joins. The review was not restricted to the small selector fix.

All retained review outputs and probes are in this private `correctness/` directory. The requested suite used `TMPDIR=/tmp/opencode` for its owned temporary fixtures.

## Findings

### 🔴 COR-SSH-002 — First setup failure can leave a pipe-holding descendant alive

**Severity:** High / P1; must fix for the bounded local-cleanup contract.

**Location:** `tests/crawl-jobs-v2-observer/context_transport.py:103–116`, especially **108–110**; triggering setup is **73–76**. The new regression at `test_context_provisioning.py:455–477` covers a sleeping leader, not an already-exited leader with inherited pipes.

**Why:** An empty `mux.get_map()` is treated as permission to reap the leader. It is empty both after genuinely observing all pipe EOFs **and before the first successful registration**. If the child has forked a pipe-holding descendant and exited when the first `os.set_blocking()` or `mux.register()` fails, `wait(timeout=0)` succeeds, sets `reaped=True`, and skips `killpg`. Parent pipe objects and the selector are closed, but the descendant remains live in the owned process group. This is a rejected acquisition, not a false successful lease; the defect is leaked local work/resources on its failure path.

**Reproduction:**

```sh
TMPDIR=/tmp/opencode python3 -B -W error::ResourceWarning \
  /tmp/opencode/m4-c0-podman-arm64-2026-10-11/correctness/probe_cleanup.py
```

The probe launches only owned benign Python processes. `waitid(WNOWAIT)` waits for the leader's exit without reaping it, preserving the production cleanup's responsibility and PID reservation. It then injects either `EIO` on the first `set_blocking`, or `ENOSPC` on the first registration.

| Case | Leader reaped by transport | Parent pipes/selector closed | Descendant live after transport |
|---|---:|---:|---:|
| First `set_blocking` failure | Yes, exit 0 | Yes | **Yes** |
| First registration failure | Yes, exit 0 | Yes | **Yes** |
| Registered-pipes timeout control | Yes | Yes | No |

The probe separately killed any surviving **owned** process group and checked the descendant stopped. It neither contacted SSH nor executed an actor/collector. Detailed evidence: `probe-cleanup.json` and `probe-cleanup.log`.

**Minimal suggested fix:** Track successful completion of pipe setup (or explicit observed EOF for both output pipes). Allow the early non-killing reap only after setup completed and the map subsequently drained. On incomplete setup, signal the owned group **before** reaping the leader, even if the map is empty. For example, a `setup_complete` guard set after all registrations can distinguish these cases without restructuring the transport. Add both exited-leader/inherited-pipe tests and keep the existing selector-allocation/spawn/setup regressions.

**Scope qualification:** This is proven in the owned-process fault model exercised by the transport's own descendant tests. It does not assert that the fixed production SSH flags normally cause an SSH helper to fork, nor that killing a local group proves remote cessation.

### 🟡 COR-SSH-003 — A returned provisioned source loses its deployment/transport preparation guard on reuse

**Severity:** Medium / P2 suggestion; **not an additional blocker for the current recommended one-shot backend entrypoint**.

**Location:** `tests/crawl-jobs-v2-observer/context_transport.py:155–167`, especially **160–163**; downstream `runtime_context.py:118–122,131–165` and reusable backend API `docker_backend.py:83–92`.

**Why:** `provisioned_source(bundle, prep_a)` checks all three provisioning identities at construction, but returns a generic `ContextSource`. Its public `acquire(..., preparation, ...)` subsequently rechecks only `runtime_context_source`. Reusing that object with `prep_b` whose deployment or transport artifact differs succeeds, and the resulting lease is bound to `sha(prep_b)` despite using the original provisioned deployment/transport. Passing the same packet directly to the factory correctly rejects it.

**Reproduction:**

```sh
python3 -B -W error::ResourceWarning \
  /tmp/opencode/m4-c0-podman-arm64-2026-10-11/correctness/probe_preparation.py
```

This uses real private staging files and a **simulated** Linux reader/endpoint; no process transport or live collector runs. For a source constructed with A:

- Change only B's `runtime_context_deployment`: acquisition and `lease.view(..., B, ...)` succeed.
- Change only B's `runtime_context_transport`: acquisition and `lease.view(..., B, ...)` succeed.
- Change B's `runtime_context_source`: acquisition refuses (`CONTEXT_SOURCE_BINDING`, positive control).
- A fresh `provisioned_source(..., B)` refuses all three mismatches (positive control).

Detailed evidence: `probe-preparation.json`.

**Minimal suggested fix:** Retain the admitted deployment and transport artifact values on the returned source and compare them with each acquisition's preparation before exchange. Binding the entire original preparation digest is also sufficient if that is the intended API. Add a reuse test with changed preparation and a same-preparation positive control.

**Scope qualification:** `DockerBackend.acquire_provisioned_context()` creates and immediately acquires a new source with the same preparation. The normal path at `docker_backend.py:94–101` is not demonstrated broken by this probe. This is a reusable-public-helper consistency gap, not a demonstrated external authorization bypass.

## COR-SSH-001 correction assessment

- Original transport SHA256: `4f7961384e1b814869885db35fe1a2cfe3ce89ecbb49d670e4a1a9c197ce9276`.
- Corrected transport SHA256: `effb0794f77066062e8de0cb0134bbbefe1f313dd5e43f2199c906ce692a05a7`.
- `DefaultSelector()` is now allocated before spawn; failure cannot strand a spawned child or its three pipes.
- Spawn and pipe setup are now inside the cleanup region, with `process is not None` guards. The selector is closed on spawn failure.
- All three newly added failure-path tests passed, and all prior 194 test IDs remain present and passed.
- The allocation defect itself is fixed. COR-SSH-002 is a distinct remaining startup-state ambiguity, not a claim that the selector ordering regressed.

## Offline test execution

Executed the entire suite **once**, from the frozen snapshot, with:

```sh
TMPDIR=/tmp/opencode python3 -B -W error::ResourceWarning \
  scripts/test-crawl-jobs-v2-observer.py \
  --report /tmp/opencode/m4-c0-podman-arm64-2026-10-11/correctness/c0-tests.json
```

Result: **197/197 PASS**, 13.267 seconds; no skipped, expected-failure, unexpected-success, failed or errored tests. The run/started/passed ID lists are identical. No `ResourceWarning`, ignored exception or traceback appeared in the retained log. Report source hashes equal all 45 supplied inventory hashes. The runner SHA256 is `9afb715125f734f5b905da63dde5c9787fc5997b2e5a73a0bf62d781b23aef84`.

This is local CPython **3.14.7** offline/benign-fixture coverage, not the ARM64 oracle interpreter running under its OCI profile. `test-verification.json` records the prior 194-ID comparison and the exact three added test IDs.

## Static ARM64 reconstruction checks

`check_artifacts.py` independently reads and parses the artifact bytes and emits `artifact-checks.json`. It does not execute an ELF or repeat a build. The main session's boolean PASS fields were not used as the acceptance criterion.

1. **All 11 native file SHA256s** exactly equal both prior `source-inventory.json` and `build-verification.json`. The prior inventory's own SHA256 equals the current inventory's `previous_inventory_sha256`.
2. Independently parsed ELF64 little-endian headers, program/section tables and symbol tables: target and observer are **AArch64 (183), ET_DYN (3)**, without `PT_INTERP`; both have non-W+X loads, nonexecutable stacks, RELRO and executable entrypoints. Raw oracle interpreter is likewise AArch64 ET_DYN, with its expected dynamic interpreter segment.
3. Target manifest symbols, instruction file offsets, virtual addresses, executable mapping and target hash/size join to actual target ELF bytes. The entire effect sequence is `d2800544 f9000064 f9000022 c89ffc02 d65f03c0`; post-effect is exactly 16 bytes after pre-effect. Both marker instructions are `d503201f`. Witness address lies in writable storage. Full manifest also equals the frozen pure generator's output.
4. `target_manifest.h` constants match those joins; the target digest string occurs in observer bytes. `cases.h` matches all 276 frozen trial IDs. This does not claim 276 trials ran.
5. Extracted **actual observer `obs_filter` / `.obs_filter` bytes** and compared them exactly with `target-filter.bpf`, `target_filter.h`, and the frozen generator. A separate userspace interpretation of the extracted BPF passed **1,482** request/TID/regset/high-bit/architecture/exec cases. No kernel filter was installed.
6. All three profile files are byte-for-byte canonical frozen-generator output with default-deny AArch64 policy. Reviewed their role-specific permissions; no x64 support is claimed.
7. Toolchain's target/observer digests equal raw binaries; all **nine** recorded native input files equal snapshot bytes and the retained inventory. Recorded compiler target is `aarch64-linux-gnu`, GCC 14.3.0, binutils 2.40.
8. Oracle manifest exactly equals prior retained manifest content and SHA256; its interpreter SHA256 equals the raw `oracle-python3.13` bytes. All seven worker payload files and their canonical aggregate digest match the snapshot and manifest. Builder and recipe input hashes were also checked against the inventory.
9. Recomputed the host-source, transport-source and oracle-worker source aggregate digests against the current inventory. The cleanup correction changes transport identity, requiring preparation regeneration as documented.
10. Checked retained pull/build commands and log SHA256s, including pinned image digests, `linux/arm64`, `--network none`, `--pull=never`, and explicit recipe ignore files. Read the recipes/logs and extraction record. No target/observer/worker RUN command appears in the artifact builds. These are static joins to retained provenance, not a new observation of Podman state.

Key artifact digests:

| Artifact | SHA256 |
|---|---|
| target | `819cde0b38e12b235acbef2b3741551af86683ff2c3ec5186b5a45b710e5da45` |
| observer | `e407a02c67ac7a0cd192e612afe5ad643701c16821f6854e3a11b040e55cfb61` |
| target manifest | `a19b4096f0f268742ebc98bf879e12fbb0de1124a1c8144959496f6b8a615e43` |
| compiled filter | `701824ff5f8ccafa359f711d2714b23c8a303789cd1987a3a85aba19a3667b93` |
| toolchain | `e953b3c89a20ebeca25ebbcce8d30486aa10174db1d9986977bed786aa47f9dc` |
| oracle manifest | `3f51809ebddbad24cbe03611e67b678df003121b77aff0743de0251baf538131` |
| oracle interpreter | `cb4548d280f38c2b1df4cba45f12c90a034729e08fce5bf1a223c58add0e21e3` |

## Other reviewed paths and limits

- Checked request/output limits, nonblocking pipe dispatch, deadline checks before selection/writes and after wait, nonzero exit/stderr refusals, exact signed-frame count/canonical encoding/MAC/order/challenge/anchor bindings, and conservative lease expiry across different controller/Linux clock epochs.
- Checked private directory/file modes, no-follow/single-link/regular-file enforcement, stable named-inode reads, per-exchange credential copies, pre/post credential and SSH-binary checks, supplied numeric hosts/keys/service identities, source and deployment digests, forced-command identity/code inventory checks, and backend pre-actor/daemon/outer-authorizer joins.
- The existing tests cover ordinary partial/extra/truncated output, timeout and descendant-held pipes, credential drift, missing source authorization, preparation mismatch at construction and backend preflight. The new probes address gaps those tests do not cover.
- **Observed non-blocking preflight limitation:** private SSH identity validation checks only size and OpenSSH envelope delimiters (`context_provision.py:139–141,153–155`), not the decoded key or its match to `ssh_client_key`. The deliberate non-key fixture still receives `PASS_PRIVATE_BUNDLE` (retained in `probe-preparation.json`). This is not an SSH authentication bypass; possession of a staging bundle is not installation/authentication evidence. Consider an offline decoded-key/key-pair check before installation.

## Untested / not authorized by this review

- No live SSH connection/authentication, host collector, target, observer, oracle worker, tracing, native calibration, Docker/Podman workload or host-policy change was performed by this reviewer.
- Actual host account/install ownership/ancestors, effective live sshd policy, out-of-band key provenance, host interpreter runtime, daemon/kernel placement and MAC-to-Linux reachability remain operator/runtime obligations.
- Actual ELF load bias, runtime seccomp enforcement, filesystem/kernel availability, ptrace feasibility, hardware breakpoints, native timing, whole-trial confinement, watchdog remote absence and complete experiment acceptance remain unproven.
- Static-library hashes are joined through the retained toolchain record and reproduced artifact identity; library archive bytes/base layers were not separately extracted or rebuilt by this reviewer. Linker warnings about glibc-dependent `dlopen`/resolver paths remain runtime considerations, not a claimed calibrated result.
- There is no independent observation of live build engine/rootless/network state or extraction-container absence here, only retained provenance/extraction records and independently verified resulting bytes.
- The planned x64 implementation is outside this current ARM64 increment; its absence is **not** a current-source defect.
- No publication, merge, deployment or runtime execution approval is issued. Even a future `GO_FOR_CONTINUED_IMPLEMENTATION` would cover source/static artifacts only.

## Retained evidence SHA256

| File in this directory | SHA256 |
|---|---|
| `source-before.json` | `d61ce96b88ceee4d8a294e8d76ceb3b3e827f4a891ec0c157f9b6cc7f2df9c9e` |
| `source-after.json` | `2752c6429f1e1d58610e7693886c9d693688369e88c55917a5a84a14b580bc9c` |
| `c0-tests.json` | `94bd6ddf268cf9c877868019319fae2268da6869386bfc9ffba15ec4ed8b88d9` |
| `c0-tests.log` | `65fe3cdceca74ac7596f594596c1de5a75fa21a7f46ac4a155ee232acd4d6f33` |
| `test-verification.json` | `256cb994935d936437d4d7ac242ddff4d1f84bcc1a051b62af9a5a8c07eabada` |
| `artifact-checks.json` | `d0e089b240c7014b236b2e994a9a9a86192f6d7274f5c733a310ea66b87d1bdf` |
| `probe-cleanup.json` | `c6beef3e05c0c59d46557c26870fede66261965dddfa7d6bdab90a8092ddc80e` |
| `probe-preparation.json` | `a84a327ebad902089d4ff6a59dfc77bc82df400dcca7de7dd862a555773b75f1` |
| `probe_cleanup.py` | `8a2d92834d1a9f3e66cc7a8b4b9245ae9bba836dd5934736b6e67ebc5907031d` |
| `probe_preparation.py` | `97e716db65ce9c223312aa92799a6bafa4b489cdbe4ec3d57adbaf2624a9a35c` |
| `check_artifacts.py` | `61d50ed17805e2373018887b87c4540d0f364f9974b7688fc3efe449b3760159` |

`SHA256SUMS` records the finalized machine-readable and Markdown review hashes as well as all retained probe/test evidence. Those two review hashes are also returned to the parent session.

## Next step

The implementation has useful closed contracts and reproducible static evidence. Consider the minimal setup-state guard and exited-leader regressions for COR-SSH-002, then re-run the frozen-source gate on a new inventory. Preserve the successful selector-order correction. Resolve or explicitly constrain the reusable-source binding behavior before relying on that helper across preparation revisions.
