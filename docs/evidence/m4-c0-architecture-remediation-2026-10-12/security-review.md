# Independent security re-review — C0-ARCH-02 remediation

**2026-10-12 — GO_FOR_CONTINUED_IMPLEMENTATION**

**COR-ARCH-001/002/003 are reproduced on the initial snapshot and verified fixed on the corrected snapshot. No new in-scope blocking finding identified.** This GO covers source/static artifacts only: **not publication, live-host admission, native execution, or M4 acceptance**.

## History and assessment correction

The initial security GO and all original evidence are preserved, not rewritten. Its report SHA-256 remains `9f7ef408e625e1d4401ac7f345a4a5926603fbe777818f997775b0438afbe364` (JSON), `112354ced20cc42afcf27c02d805bb4297ac467f89a3912aced73755b582d4ed` (Markdown). The initial correctness report matches `cfec175b78f6a21f47a6407ffec213a1d2ee09e64cf25941defc168a6796fbe4`.

**The initial security assessment missed these three entrypoint/consistency gaps.** Matching supplied artifacts and normal-path refusals did not establish standalone record-mode correctness, direct startup validation, or mutable-anchor consistency. In particular, the initial report's reliance on creation provenance did not discharge the direct `open_actor()` entrypoint's own image check. Public-anchor consistency should not have been treated as interchangeable with arbitrary private-field mutation. The findings below are genuine, reproducible contract gaps under the documented trusted-controller model, without asserting MAC forgery, Python sandbox escape, malicious-root resistance, or demonstrated native exploitation.

This re-review evaluates the corrected bytes; the initial GO is historical evidence, not proof those gaps were absent.

## Exact scope and integrity

- Snapshot: `/tmp/opencode/m4-c0-architecture-remediation-2026-10-12/snapshot`.
- Inventory: `a02661da8cb299ff60e4ad9752ea53329b65c384d7f8e15853641dcf6db426cc` — **47 package files plus original runner**.
- Initial inventory: `b5388bbdf282e286caec43f6a5e12bc5e0129a6630f43bd2c2b15b405b0d7bfb` — 46 package files.
- Runner: `9afb715125f734f5b905da63dde5c9787fc5997b2e5a73a0bf62d781b23aef84`, equal to the merged-base, both snapshots, and repository copy.
- Branch: `feature/crawl-jobs-v2-c0-architecture-contracts`; HEAD/base `ced6e1cff36101f76fb54904b6b0fa2cf33cb48f`; edits remain uncommitted.
- Delta exactly matches the supplied `remediation.patch`: `build_tools.py`, `docker_backend.py`, `runtime_context.py`, `test_owned_resources.py`, plus new `test_architecture_review.py`.
- **Before/after PASS:** both complete source inventories, corrected working-tree package equality, runner, both native artifact sets, reused oracle files, source/platform joins, exact remediation delta, and **59 original security/correctness evidence files**. No drift. Supplementary local verification hash `3617c193c2bbe652ccbc1a526d7710a020e28fc62aa2f808d906bf917b79ea87` was checked but not substituted for independent evidence.

All line references below are relative to `tests/crawl-jobs-v2-observer/` in the indicated snapshot.

## Finding closures

### COR-ARCH-001 — High / P1 — VERIFIED FIXED

**Defect:** direct/snapshot actor startup could dispatch without inspecting the actual image's architecture. An uninterrupted, already-validated create-then-start path was not shown exploitable; the public direct-start path nevertheless lacked its own guard. Later native admission is too late to prevent process startup.

**Initial location:** `docker_backend.py:251–278,333–334`; image checks existed only at `219–227` in create.

**Reproduction:** the probe supplies an ARM64 daemon, otherwise-valid registered resource snapshot and simulated actual image metadata. Across target/observer/oracle, the old direct-start path dispatches despite amd64/missing/alias architecture, wrong OS/ID, absent image/config, OnBuild, volumes, environment or healthcheck. It performs **zero image inspections**. No real container or native program is started.

**Correction:** common full image predicate at `docker_backend.py:197–206`, called from create at `230` and startup at `281`, before stream creation at `337–338`:

```python
# After the expected scope/container has been admitted, before dispatch:
self._admit_image(expected, deadline)
```

The helper requires exact image ID, Linux/OCI ARM64 architecture, admissible Config/OnBuild/Volumes/Healthcheck and expected environment digest. It reuses rather than weakens the existing create policy.

**Verification:** **72 cases per revision** (12 inputs × three roles × create/start). Initial source: 33 invalid create refusals, but all 33 invalid direct starts dispatched. Corrected source: **66 invalid cases refused before dispatch**, six matching ARM64 controls allowed; one actual-image metadata inspection per operation. Scope/container/profile checks and the external authorizer remain in place. `FakeDocker.inspect()` now supplies image metadata instead of bypassing the new guard.

### COR-ARCH-002 — Medium / P2 — VERIFIED FIXED

**Defect:** mutable public anchor state could change between request creation, callback processing and lease construction, yielding inconsistent authenticated evidence or permitting a legacy anchor. This required the injected/trusted callback and test-key signing in the repro; it is not an unauthenticated MAC bypass. Existing downstream native architecture checks would reject the mixed lease.

**Initial locations:** `runtime_context.py:47–60,71–97,109–114,125–176`.

**Reproduction:** an exchange callback returns a correctly test-key-authenticated x64 KERNEL response and updates public `source.anchor`. Old `acquire()` and `view()` accept a lease whose request/binding say `aarch64` but context/receipt say `x86_64`; receipt anchor hash differs from the request. Separate authorization/pre-acquisition/read callbacks demonstrate version-1 anchor acceptance. Direct `request()`/endpoint also accept a correctly signed request bound to a legacy anchor.

**Correction locations:** `runtime_context.py:47–60,72–98,112–118,129–184`. Each operation validates and snapshots anchor/key; the parser validates the anchor; callbacks cannot cause validation/receipt construction to follow changed public state; context, binding and preparation architectures are directly joined. Relevant applied checks:

```python
# Source acquisition, before authorization/exchange:
anchor, key = validate_anchor(self.anchor), self._key
mac(key, b"")

# After authorization and after exchange (and before endpoint sealing):
require(canonical(self.anchor) == canonical(anchor), "CONTEXT_ANCHOR_CHANGED")

# After authenticated context validation:
require(context["architecture"] == anchor["architecture"] == binding["architecture"],
        "CONTEXT_ARCHITECTURE")

# ContextLease.view:
require(self._context["architecture"] == preparation["architecture"],
        "CONTEXT_LEASE_BINDING")
```

All request/MAC/response/receipt work uses the validated local snapshots; the endpoint also snapshots its public `key` attribute (the symmetric HMAC key). The complete applied code, including operation-local variable names, is retained in `verified-fixes.patch`.

**Verification:** original mixed-architecture and version-downgrade counterexamples now refuse. Direct legacy parser/endpoint calls refuse before kernel reads. **40 expanded lifecycle cases** cover version, architecture, daemon/source/boot digests, device, nested namespaces/filter counts, extra and missing fields across pre-acquisition/authorization/exchange/read phases. Invalid pre-acquisition schemas/bindings refuse before authorization; changed authorization state refuses before exchange/challenge use. Validly shaped but mismatched anchors cannot satisfy the endpoint. Three canonical-equivalent anchor-replacement controls still succeed. A mid-read public endpoint-key change is isolated to the original operation key. Relabeling the public lease binding cannot return ARM context for x64 preparation. An old host-source digest refuses before authorization/I/O.

### COR-ARCH-003 — Medium / P2 — VERIFIED FIXED

**Defect:** standalone/final record mode could label the toolchain `aarch64`, hash an x64 target, and retain a stale target instruction manifest. The normal pinned build's preceding manifest stage and the supplied valid artifacts did not establish record-mode validation.

**Initial location:** `build_tools.py:107–129`.

**Reproduction:** retain real ARM64 observer/BPF/manifest in a private directory, replace target with inert x64 interpreter ELF bytes, and stub compiler/library metadata calls. Old record mode writes an ARM64 platform record whose target hash disagrees with the target manifest. No compiler or ELF is executed.

**Correction:** `build_tools.py:108–110,129–132` validates one target byte snapshot and its exact canonical regenerated instruction manifest **before compiler metadata/output**, then hashes that same snapshot:

```python
target = (out / "target").read_bytes()
manifest = target_manifest(target, architecture)
require((out / "target-manifest.json").read_bytes() == canonical(manifest),
        "BUILD_TARGET_MANIFEST")
# The existing final record now uses sha(target), not another filesystem read.
```

**Verification:** nine cases on each revision. Corrected foreign target, stale target, legacy/foreign-architecture/foreign-platform/extra-field/noncanonical manifests all refuse: **seven refusals, zero compiler-query calls and no toolchain output**. Matching ARM64 accepts. Replacing the private target during the simulated compiler callback no longer changes the recorded target hash: it remains the validated snapshot matching the manifest. This is not an atomic attestation of a concurrently writable directory; output isolation/revalidation remains a caller obligation.

### Reproduction and exact remediation evidence

`probe_counterexamples.py initial` and `probe_counterexamples.py corrected` run in separate `python3 -B -W error::ResourceWarning` interpreters. Their `PASS` means the expected old defect was reproduced or corrected behavior verified, **not** that old source is approved. Results: `counterexamples-initial.json`, `counterexamples-corrected.json`; corresponding logs retained.

`verified-fixes.patch` contains the exact three production-file fixes as a standard `a/`/`b/` unified diff, generated from both frozen snapshots, **not applied by this reviewer**. SHA-256: `5e7947990f7082a56c057003a4dc62929eb431e9e0d4dcf16772f38f8bae8cb7`.

**New findings:** none. **Outstanding remediation for these three findings:** none.

## Independent tests and preserved security controls

The original snapshot runner was invoked **once** from the corrected snapshot, under **umask 077**, `TMPDIR=/tmp/opencode`, `PYTHONDONTWRITEBYTECODE=1`:

```sh
python3 -B -W error::ResourceWarning scripts/test-crawl-jobs-v2-observer.py \
  --report /tmp/opencode/m4-c0-architecture-remediation-2026-10-12/security/tests.json
```

**238/238 PASS**, 13.328 seconds; no failures, errors, skips, expected failures, unexpected successes or ResourceWarnings. **All 229 prior IDs passed**, exactly nine new review tests. Independently compared all **229 prior test-method ASTs: unchanged**, so assertions were not weakened. No independent second full-suite/umask-022 run is claimed.

Preserved original probes were replayed against corrected source using an in-memory path redirection; original scripts were not changed/run with their original output paths. Separate replay provenance/logs are retained.

- Eight boundary groups passed, with **280 expected `Invalid` refusals**: schema/alias/downgrade, signed frame joins/replay, per-acquisition bindings, unsupported runtime/build/SSH activation, all native architecture combinations, profile/image confusion, fragmented interpreter headers/uname, and cleanup.
- **COR-SSH-001 remains fixed:** selector allocation precedes child spawn. Allocation failure launches no child.
- **COR-SSH-002 remains fixed:** six setup-failure positions with owned benign Python exited-but-unreaped leaders and pipe-holding children still kill the group before reaping; real inherited-pipe EOF, closed pipes/selector, FD delta **0**. Not remote absence proof.
- **COR-SSH-003 remains fixed:** private provisioning binding copies, per-acquisition preparation snapshots and callback-before-I/O checks remain; copied/in-place mutations refuse, valid reuse/fresh challenges work.
- **QA-ENV-001 remains fixed:** full umask-077 suite and nine owned simulated-install/host-service-anchor-interpreter cases pass; private 0700 parent with explicit 0755 install fixture and 0444 source checks. No production permission checks relaxed.
- Baseline control-function ASTs, three-line handshake, native nonce/expiry/emit bounds, shared limits, ARM64 filter/profile bytes and watchdog continuity pass. Version-1 control messages remain intentional; legacy admission/context schemas reject. x64 metadata remains non-authorizing; direct SSH `__call__`, builders, profiles/filter, scopes/readers/endpoints/streams still refuse unsupported activation.

Evidence: `tests.json`, `tests.log`, `test-verification.json`, `previous-boundary-probes.json`, `host-join.json`, `continuity.json`, `replay-*-provenance.json`.

## Static artifacts and source/platform joins

All **ten native ELF/manifest/header/BPF/profile outputs are byte-identical** to the initial artifacts. Re-ran static ELF/instruction/BPF/profile/source joins against the corrected snapshot, not just manifest-label checks:

- Native ELF64/LE, EM_AARCH64, static PIE, no PT_INTERP, non-executable stack, RELRO, no W+X PT_LOAD; target instruction sequence, marker returns and witness symbol storage match the manifest/header. Observer contains the exact target digest and 272-byte/34-instruction filter.
- **3,092 simulated BPF checks** pass, including foreign audit architectures and high-word confusion. No BPF installed.
- Exact target/observer/oracle profiles, cases/header, v2 admission strings and compile-time ARM64/LE/64-bit guard retained.
- Regenerated toolchain: `4926c2e34b92a025c27e0fecdfd64c597da94d53a78d9a4231d51f8c8f0e5eae`. Its **only record-content difference is `sources.build_tools.py`**; all nine source inputs match the corrected freeze. Compiler/library metadata are unchanged records, not independently remeasured installations.
- Target: `07ae68522b7e87560bda4d32fcb0f244c74fd748f7cd9ccfe1ac3cf2321deab5`; observer: `53800d2281f57341269a0f1c9686809883241ed01872338b0544bf0439e88120`.
- Oracle's **ten source/recipe inputs unchanged**; reused manifest `6168e220727cb6377e8e637740bb8c297bf7bee929ab19aac6db90aa6fbcbfa5` and interpreter `cb4548d280f38c2b1df4cba45f12c90a034729e08fce5bf1a223c58add0e21e3` rehashed. Oracle ELF remains ARM64 with `/lib/ld-linux-aarch64.so.1`; dynamic dependency closure is not established.
- New host-source bundle: `ffae141857d61996ab5e5b53e8b55402a94b3caed39fa623665467610d81d836`. Regenerate dependent host deployment/preparation bindings. Transport `72ca166c9680cf4e37142c83335995ba396deec4d978923f23b83bda2cc98001`, oracle-worker `f55a4c825523fc12e6d5bc3f5efaa97c8266a1ef640e4454c912346d36d3a86a`, and both canonical platform descriptors/digests remain unchanged.

Evidence: `artifacts.json`, `artifact-continuity.json`, integrity reports. The static detail JSON is identical to the original because the ten detailed artifacts/invariants are unchanged; independent replay provenance and the new toolchain/source-delta report distinguish this fresh verification.

## Remaining boundaries and authority

1. Root/operator and arbitrary trusted-controller code remain trust roots. These fixes enforce public data/operation consistency, not a Python sandbox. An injected fixture with the signing key is not proof an unauthenticated actor can forge a host response. Static summaries/leases do not grant execution approval.
2. No claim of complete ELF/runtime validation from header classification; no live image-content/dependency, non-emulated kernel, load-bias, seccomp/LSM/Yama, hardware breakpoint or timing/calibration proof. Hashing validated byte snapshots is not a filesystem transaction or atomic multi-artifact attestation.
3. x64 implementation remains C0-ARCH-03; host/runtime/backend admission remains gated. No Podman runtime adapter was added. Supplied artifact-only builds do not demonstrate Docker/Podman workload feasibility.
4. Actual independent collector/daemon placement, installed source/interpreter ownership, effective live sshd policy, decoded key-pair/live pinned-SSH authentication, full trial assembly and independent remote cleanup/absence proof remain open. No policy weakening or privilege fallback is recommended.
5. No native actor, real oracle worker/collector, live SSH, container workload, compiler rebuild, tracing/calibration, BPF install or host-policy mutation was performed by this reviewer. The suite's `ssh -G` is offline configuration inspection. Only owned benign Python processes/files/pipes/sockets, compiler/transport stubs, simulated metadata and static byte reads were used.
6. No repo/source/docs/Obsidian edits and no subagents. All new outputs are confined to `/tmp/opencode/m4-c0-architecture-remediation-2026-10-12/security/`; original reports/probes/snapshots remain unchanged. A new revision requires a fresh integrity-bound gate.

## Principal evidence hashes

| File | SHA-256 |
|---|---|
| `tests.json` | `d6bf46767e6c7909789e53ce008dc0e26ebd60f4c3e3f76fe4837747ceab38c4` |
| `tests.log` | `2b9c2f75d14937615c6ba80e289f3df10e05d780ababed4f7a76346cd14f8135` |
| `counterexamples-initial.json` | `b233ed5ed9b0a04d4ed5031ec79df1937bce1fa174f6b4c19d5243fcbd15377d` |
| `counterexamples-corrected.json` | `7e4ab4c78c66fed170f63fd28741aa3344e76b417c068edfc65aff5d5f159bb5` |
| `previous-boundary-probes.json` | `8b3fd102a6994ab2b8cdf9301ce95b97b222f4f59eea4947868194f9b2ae4fb5` |
| `artifact-continuity.json` | `27244a5d05825f4084e73c7bdda54f0c829487d1a7816f6abd930b96718f1f18` |
| `integrity-before.json` | `9cec9bfbe9b3c3604a9193729b6cfb4b99114e78a240abe11de5c340607f61b8` |
| `integrity-after.json` | `57b510d3cdc53ef0325d554873b15bee472d88fe2854b5a707a7f5a59a10ec47` |

`review.json` provides the machine-readable disposition and evidence hashes. `SHA256SUMS` binds the complete new review directory, including reports and probe sources.
