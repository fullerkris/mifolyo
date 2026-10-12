# C0-ARCH-02 remediation — independent correctness re-review

**Verdict: GO_FOR_CONTINUED_IMPLEMENTATION**

**COR-ARCH-001, COR-ARCH-002 and COR-ARCH-003 are closed on the new frozen source. No new blocking correctness finding was identified.** The fixes are narrow: shared image admission closes the startup gap without duplicating policy, operation-local anchor/key snapshots preserve signed-context bindings, and record generation now validates and hashes the same target bytes.

This GO covers **reviewed source and supplied static artifacts only**. It is not publication/merge approval, installation acceptance, permission to execute native actors, or runtime/calibration acceptance. The original NEEDS_REMEDIATION reports and counterexamples remain preserved as historical evidence.

## Closure decisions

All source locations below are under `tests/crawl-jobs-v2-observer/` in the new snapshot.

| Finding | Status | Remediated location | Independent evidence |
|---|---|---|---|
| **COR-ARCH-001 / P1** — direct startup skipped image architecture | **CLOSED** | `docker_backend.py:197–206,230,279–281,337–338` | Original amd64/missing-architecture startup counterexamples now inspect the selected image and refuse with `IMAGE_IDENTITY` before the fake start factory. Arm64 still succeeds. Independent fresh/snapshot startup controls cover all three roles and malformed/foreign image identity, OS, config, environment, healthcheck/defaults, missing image and inspection failure. |
| **COR-ARCH-002 / P2** — mutable anchors detached context from request | **CLOSED** | `runtime_context.py:47–60,72–98,112–118,129–184` | Original signed x64 response plus public-anchor mutation now refuses without a lease or wire-budget consumption. Original authorization-time version downgrade refuses before challenge consumption/exchange. Source and endpoint schemas are revalidated, nested drift is rejected, and valid request/context/receipt/lease joins survive equal-value anchor replacement and reuse. |
| **COR-ARCH-003 / P2** — record mode mislabeled a foreign target | **CLOSED** | `build_tools.py:107–110,128–132` | The original real x86_64 ELF input now refuses with `ELF_ARCHITECTURE` before compiler queries or `toolchain.json`. Stale target, legacy/foreign/noncanonical manifest controls also refuse before queries/output. Real supplied ARM64 bytes with their exact manifest succeed under simulated compiler metadata. |

No correction requires x64 runtime support or changes Docker's role as runtime backend. Metadata-only x64 remains intentional.

## Original counterexample replay

`replay.py` preserves the original input sequences. Relative paths retarget imports and outputs to this remediation review; expected post-fix assertions/results are updated. `replay-delta.patch` records the exact differences from the preserved original `correctness/probes.py`.

- **Startup:** same synthetic registered target snapshot, arm64 daemon, and image responses `amd64`, absent architecture, and `arm64`. Each image is now inspected once. Both negatives perform zero fake starts; the positive performs one.
- **Signed context mutation:** same test-key-authenticated x64 KERNEL response and source-anchor mutation. Result is `CONTEXT_ACQUISITION_REJECTED`, no lease, zero consumed response bytes/frames.
- **Anchor downgrade:** same source/endpoint version-1 mutation from the authorizer. Result is `CONTEXT_ANCHOR_CHANGED`, no lease, no challenge or wire-budget consumption.
- **Build record:** same inert x86_64 Python ELF plus ARM64 observer/BPF/manifest/header input bytes. Result is `ELF_ARCHITECTURE`, zero compiler calls, no toolchain record. These private copies are never executed.
- **Retained controls:** preparation mutation remains isolated from the current lease; all four correctly signed legacy context frame variants reject; only the all-aarch64 tuple passes the 16-way native architecture product.

All **nine replay result groups** pass their post-fix expectations. The original x86_64 probe input hash is still `0d64bd6d66d68dac91cdadafc46e22d4f09f22bdc997aaae870f42865b024a3e`.

## Additional independent boundary controls

`boundaries.json` records **76 passing cases** separate from the replay:

- **40 startup controls:** 13 image scenarios for each of target/observer/oracle, plus the higher-level `open_oracle` foreign-image path. Startup uses a fresh backend and a serialized resource snapshot, not a preceding creation receipt. The exact role image is inspected; missing/error/foreign metadata does not fall back or reach the fake start factory. No resource creation is needed.
- **16 schema/lifecycle controls:** source acquisition and authenticated request/endpoint handling reject legacy, boolean, missing or extra fields, invalid filter counts and aliases before authorization/exchange or reader access as appropriate. Canonical x64 request metadata remains parsable, but the endpoint refuses before observation.
- **9 drift/recovery controls:** version, architecture and nested namespace mutations at authorization, exchange and the endpoint's second kernel observation reject. Restoring valid configuration permits a subsequent acquisition; challenge accounting remains correct.
- **4 positive/defensive controls:** equal-value anchor replacement and valid reuse preserve fresh challenges and request/receipt joins; source and endpoint key mutation fault injections preserve the current operation's original key snapshot and reject the next mismatched exchange; the explicit lease-context architecture check rejects an injected inconsistent private context. Private-state injections test consistency, not a Python security sandbox.
- **7 build-record controls:** valid actual ARM64 target/observer bytes succeed; stale target and legacy, foreign-platform, foreign-architecture or noncanonical manifests reject before compiler queries/output. A target replaced during a simulated compiler query is read only once and the record retains the original validated target hash, rather than hashing the foreign replacement. A later raw-file hash comparison detects that replacement; this is snapshot consistency, not a claim of atomic filesystem sealing.

## Independent test run and regression preservation

Executed the complete frozen suite **once**:

```sh
umask 022
TMPDIR=/tmp/opencode python3 -B -W error::ResourceWarning \
  scripts/test-crawl-jobs-v2-observer.py \
  --report /tmp/opencode/m4-c0-architecture-remediation-2026-10-12/correctness/tests.json
```

**238/238 PASS**, 13.139 seconds, local Python 3.14.7. Inventory/started/passed IDs agree. No failures, errors, skips, expected failures, unexpected successes, ResourceWarnings, ignored exceptions or tracebacks.

All **229 previous IDs** and the **202 merged baseline IDs** remain. Exactly **nine** new IDs are in `test_architecture_review`. All existing test assertion ASTs are unchanged. The only existing test-file change is `FakeDocker` supplying image metadata; no assertion was removed.

`context_transport.py`, `test_context_provisioning.py` and `test_architecture_contracts.py` are byte-identical to the initial architecture freeze. Thus COR-SSH-001/002/003, the explicit umask fixture fix, and the direct unsupported-architecture SSH guard remain preserved, with their regressions passing.

The author's red report was hash-checked: nine methods, seven failing methods before the production corrections and two passing positive methods. Its source hashes match the pre-remediation production files. The author's 022/077 final JSON reports were also hash-checked and match the independently generated deterministic report. Neither substitutes for the retained independent execution log. A second independent full umask-077 run was not performed.

## Source and historical-evidence integrity

- New inventory SHA256: `a02661da8cb299ff60e4ad9752ea53329b65c384d7f8e15853641dcf6db426cc`.
- Supplementary verification SHA256: `3617c193c2bbe652ccbc1a526d7710a020e28fc62aa2f808d906bf917b79ea87`.
- Hashed every **47 new package files plus runner**, both snapshot and repository, before and after review. Package bytes match the inventory throughout. The new snapshot has exactly **48 files**, no symlinks and no extra bytecode.
- Independently recomputed the snapshot delta and matched `remediation.patch`: four changed paths plus `test_architecture_review.py`. Base remains `ced6e1cff36101f76fb54904b6b0fa2cf33cb48f` on the stated branch.
- Runner remains `9afb715125f734f5b905da63dde5c9787fc5997b2e5a73a0bf62d781b23aef84`, matching both snapshots, repository and merged base.
- Rehashed the original **46-file package plus runner** and every **32 files** in the original correctness evidence manifest before and after this review. No original report, probe, private inert input, or snapshot byte changed. Original `SHA256SUMS` remains `5a49eec71edefb9be5cc35f22e5c9b6f18a5d60efa9d8f72de54117173d4bc61`.
- All new reviewer files are under this mode-0700 `correctness/` directory. The explicitly requested suite used its owned temporary fixtures under `TMPDIR=/tmp/opencode`. No repository, snapshot, docs or Obsidian edits, and no subagents, were used by this reviewer.

## Actual artifact and source joins

`static_checks.py` reads actual artifacts and source files, not merely the author's PASS fields.

- Rehashed all **11 new native outputs**. Exactly **10** are byte-identical to the initial architecture build: target/observer, manifest/header, profiles, BPF and generated case/filter headers. Only `toolchain.json` changes, to `4926c2e34b92a025c27e0fecdfd64c597da94d53a78d9a4231d51f8c8f0e5eae`.
- Independently parsed target and observer as ELF64 LE / ET_DYN / AArch64 machine **183**, with no program interpreter or W+X load segment. Target SHA remains `07ae68522b7e87560bda4d32fcb0f244c74fd748f7cd9ccfe1ac3cf2321deab5`; observer remains `53800d2281f57341269a0f1c9686809883241ed01872338b0544bf0439e88120`.
- Regenerated the target manifest with the remediated pure generator. Independently joined its symbols, instruction bytes, offsets and executable mapping to ELF tables; checked generated numeric header macros and the target hash embedded in observer.
- Extracted actual observer BPF at offset **3498664**, **272 bytes / 34 instructions**. It matches exported and regenerated BPF, SHA `701824ff5f8ccafa359f711d2714b23c8a303789cd1987a3a85aba19a3667b93`. All three exact profile files match their generators and unchanged prior bytes.
- Verified all **nine native toolchain source inputs**. Only its `build_tools.py` source hash differs; all non-source record fields, including actual artifact hashes, compiler strings, platform digest and recorded library digests, are unchanged. Raw compiler/library bytes were not independently retrieved.
- Rehashed the **ten unchanged oracle inputs**: seven payload files plus builder/recipe/ignore file. Reused oracle manifest SHA remains `6168e220727cb6377e8e637740bb8c297bf7bee929ab19aac6db90aa6fbcbfa5`; actual raw interpreter SHA remains `cb4548d280f38c2b1df4cba45f12c90a034729e08fce5bf1a223c58add0e21e3`. Its header independently parses as ELF64 LE / ET_DYN / machine 183, and its hash joins to the manifest. Payload source hashes and aggregate digest match the new snapshot.
- Recomputed both platform digests: unchanged. Recomputed host-source digest: **`ffae141857d61996ab5e5b53e8b55402a94b3caed39fa623665467610d81d836`**. Transport and oracle-worker digests remain respectively `72ca166c9680cf4e37142c83335995ba396deec4d978923f23b83bda2cc98001` and `f55a4c825523fc12e6d5bc3f5efaa97c8266a1ef640e4454c912346d36d3a86a`.
- Checked fresh native build-record source/artifact hashes and log integrity, plus unchanged oracle build-input/log continuity. Prior detailed native-frame format checks retain byte continuity through unchanged ELFs; they are not represented as fresh native execution.

## Untested boundaries

- No native actor, raw ARM64 interpreter, real oracle workload, real host collector, live SSH connection, Docker/Podman workload, host-policy mutation, tracing or calibration was executed. The suite uses benign Python processes with simulated observations/witnesses and offline `ssh -G` configuration inspection.
- No independent rebuild or container/image extraction, rootless-builder-state inspection, container-inventory check or compiler/library-byte extraction was performed. Build metadata/logs are corroborating static evidence, not fresh build-environment attestation.
- Installed host-service/sshd policy, actual image/daemon/kernel placement, OCI enforcement, hardware tracing feasibility, timing and independent remote cleanup remain unaccepted.
- Artifact records bind validated snapshots; arbitrary concurrent writes to output directories are not prevented. Actual supplied output hashes were independently checked stable before and after this review.
- Python object internals are not a sandbox against arbitrary in-process code. The ELF-header classifier is not full ELF/runtime admission. Deferred x64 runtime implementation and host selection are not defects.

## Evidence identities

| File | SHA256 |
|---|---|
| `integrity-before.json` | `74b62f1a90de1ad10432e1da96d94ff4d76b144200caa9f91ed1dbd85c095200` |
| `integrity-after.json` | `15318faf32e65b0471c0f837d3a18a1b56aa3df1467d9c2967a87d5dbd863861` |
| `tests.json` | `d6bf46767e6c7909789e53ce008dc0e26ebd60f4c3e3f76fe4837747ceab38c4` |
| `tests.log` | `1d9988d0d4d03077a7cc770d7674f9a6ab83f8452842deb850e0ac35965abafb` |
| `replay.json` | `7d0575f5fc3a69ca9f6c9283e6da2889623d0ae542556a0c26b0f6cb809cc8a2` |
| `boundaries.json` | `7eaec2cea0b8b331cc9809aab42cb9a3dde55c4b99cd7f8aa9a4d41f1603b222` |
| `artifact-checks.json` | `d6001963dd10e96cde6953828fa0470ff88d31c801d9d82ca89896d65232051a` |
| `test-verification.json` | `02fc02d2fc98d46208715593eca8937e93dac1a7887a11ed1c3c8d052971f001` |
| `replay-delta.patch` | `5172ff7e30d2a613b09d670af6db2395d8244760e711abab69a7760f7c9e9481` |

Final `review.json`, `review.md` and all retained scripts/logs/inputs are covered by this directory's `SHA256SUMS`; report and manifest hashes are returned to the parent.

## Next step

The three demonstrated corrections satisfy this correctness gate. Continue implementation with the new host-source and toolchain identities, retaining the separate publication and actual host/native-runtime approval gates.
