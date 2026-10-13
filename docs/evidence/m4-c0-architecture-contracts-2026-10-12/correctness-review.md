# C0-ARCH-02 — independent correctness review

**Verdict: NEEDS_REMEDIATION**

The migration is well scoped: canonical architecture descriptors are closed and deterministic, native-frame and preparation versions are explicit, and the normal admission path rejects mixed architecture. The independent suite passed **229/229** with all **202 baseline IDs** retained. The supplied ARM64 artifacts also pass the independent static joins. Three boundary gaps remain: startup does not check image architecture, mutable anchors can detach a signed context from its request, and standalone build-record generation can label a foreign target as ARM64.

This is a source/static-artifact correctness decision, **not** publication approval, host admission, runtime acceptance, or native execution authorization. Deferred x64 implementation and host selection are not findings.

## Findings

### 🔴 COR-ARCH-001 — P1: actor startup lacks the image-architecture check

**Location:** `tests/crawl-jobs-v2-observer/docker_backend.py:251–278,333–334`; compare the image checks at `219–222` in `create_container`.

`open_actor` validates the daemon architecture and container ownership/configuration, but never inspects the referenced image. Neither the resource snapshot nor the backend holds an image-admission receipt that proves `create_container` performed that check. Thus direct startup from an otherwise-valid registered snapshot can reach the start transport when the image metadata is `amd64` or lacks architecture, while declaring the stream `aarch64`.

**Reproduction:** `probes.py::startup_image_join` uses the frozen package's synthetic Docker fixture, an arm64 daemon, and a runner that supplies amd64 image metadata if inspected. `open_actor` returns a stream and calls the fake factory with `docker ... container start ...`; **zero image inspections** occur. A missing image architecture has the same result. The arm64 positive control also succeeds. No actual Docker command or actor runs.

**Why:** This leaves the promised image/daemon/scope join incomplete at an activation entrypoint. A later native-frame rejection is too late to prevent process startup. The normal same-image create-then-start path may be safe because its image ID was checked earlier; the demonstrated gap is the direct/snapshot startup path, not mutable Docker image IDs.

**Minimal fix:** Consider factoring the existing image identity/OS/architecture validation into a helper and applying it to the exact referenced image before `open_actor` invokes `stream_factory`, or requiring an immutable verified image-admission binding at that boundary. Add startup regressions for foreign/missing image architecture and an arm64 positive control. Keep Docker as the runtime backend.

**Evidence:** `probes.json`: `startup_image_amd64`, `startup_image_None`, `startup_image_arm64`.

### 🔴 COR-ARCH-002 — P2: an in-flight anchor can change the meaning of a signed context

**Location:** `runtime_context.py:125–146,163–176`; related endpoint checks at `71–91` and lease view at `109–114` (all under `tests/crawl-jobs-v2-observer/`).

Acquisition snapshots the preparation, but repeatedly reads the mutable public `self.anchor` across authorization and exchange callbacks. The response architecture is compared with that current anchor, not the architecture and anchor digest of the actual signed request. Anchor schema/version is only validated at construction.

**Reproduction:** `probes.py::source_response_mutation` starts with a valid aarch64 source, endpoint and preparation. Its synthetic exchange returns a correctly test-key-authenticated KERNEL payload relabeled `x86_64`, while updating `source.anchor["architecture"]` to `x86_64`. Acquisition succeeds, and `lease.view` accepts the original preparation:

| Binding | Returned value |
|---|---|
| Signed request architecture | `aarch64` |
| Lease binding architecture | `aarch64` |
| Lease context and receipt architecture | `x86_64` |
| Receipt anchor digest equals request anchor digest | **false** |

A second probe changes source and endpoint anchor versions to `1` during authorization; the exchange still yields a lease. The corresponding construction-time validator rejects version 1, so this is a lifecycle validation gap.

**Why:** An accepted lease can contradict its authenticated request and preparation, and its receipt can describe an anchor that was never in that request. This is a public-state/callback consistency issue, not a claim that Python objects are a sandbox against arbitrary code or that a remote party can forge a MAC without the key. The demonstrated mixed lease would still be rejected by the backend's later context-architecture check; no native activation bypass is claimed for this finding.

**Minimal fix:** Consider validating and freezing a local anchor snapshot before callbacks in both `ContextSource.acquire` and `LinuxContextEndpoint.exchange`, and use it for all request, response and receipt comparisons. Explicitly join response architecture to the snapshotted preparation/request as well. Refuse anchor changes during an operation or keep the entire operation bound to the original validated anchor. Add signed-response mutation and anchor-version downgrade regressions; retain the preparation deep-copy protection.

**Evidence:** `probes.json`: `context_callback_architecture_drift`, `context_callback_anchor_downgrade`. The positive control `context_callback_preparation_is_copied` verifies the existing preparation-copy protection remains effective.

### 🔴 COR-ARCH-003 — P2: record mode can emit an ARM64 toolchain record for an x86_64 target

**Location:** `tests/crawl-jobs-v2-observer/build_tools.py:107–129`, especially `126–128`.

The `record` branch parses the observer with the selected architecture, but reads the target only to hash it. It does not check that the current target is that architecture or matches the existing target instruction manifest. Consequently, its new version-2 `architecture`/`platform_sha256` fields can contradict the target they identify.

**Reproduction:** `probes.py::build_record_target_join` places the supplied ARM64 observer/BPF and target manifest/header in a private output directory, but supplies a real x86_64 Python ELF as inert `target` bytes. With only compiler metadata/library queries simulated, the real `build_tools.main(... record ... aarch64)` succeeds. It emits a version-2 aarch64 record hashing that x86_64 target, while the retained target manifest hashes the original ARM64 target. Neither ELF nor any compiler is executed by the probe.

**Why:** The standalone build entrypoint can issue a self-inconsistent architecture-bound artifact set. The normal pinned Dockerfile validates the target in the preceding manifest stage, and the **supplied artifacts are consistent**; this finding concerns missing validation at record generation if outputs are mixed, replaced or the entrypoint is invoked separately.

**Minimal fix:** Consider reading the target once, validating it with `target_manifest(target_bytes, architecture)`, and comparing that canonical manifest to `target-manifest.json` before writing `toolchain.json`. Hash that same validated byte snapshot. Add foreign-target and stale/legacy-manifest negative tests, preserving the existing correct ARM64 output.

**Evidence:** `probes.json`: `record_mode_mixed_target`; its `private_probe_directory` identifies the retained inert inputs and false record.

## Verified checks

### Source integrity and baseline preservation

- Verified inventory SHA256 `b5388bbdf282e286caec43f6a5e12bc5e0129a6630f43bd2c2b15b405b0d7bfb` and supplementary verification SHA256 `c4b6c596b76147d8ac3d2c3f959d728a0836e649b355a8b511439b6f906ed864`.
- Independently hashed **every one of the 46 package files in both snapshot and repository**, and the runner, before and after the checks. Package bytes remain identical to the inventory. The snapshot has exactly 47 files, no extra bytecode and no symlinks.
- Runner matches the repository and merged base `ced6e1cff36101f76fb54904b6b0fa2cf33cb48f`: `9afb715125f734f5b905da63dde5c9787fc5997b2e5a73a0bf62d781b23aef84`.
- Compared the complete tracked package diff to `source-delta.patch` and reviewed the new test file separately.
- Confirmed all prior assertion-call ASTs in the six changed existing test files are preserved; the fixture migrations did not remove assertions.
- `exchange_process`, `ssh_argv`, and the complete `_ProvisionedContextSource` class have unchanged ASTs versus the merged base. Thus the COR-SSH-001/002 cleanup and COR-SSH-003 reusable-source corrections remain in place, with their regressions passing. The explicit `root.chmod(0o755)` umask fixture fix remains intact.

### Independent suite and additional probes

Executed once from the frozen snapshot:

```sh
umask 022
TMPDIR=/tmp/opencode python3 -B -W error::ResourceWarning \
  scripts/test-crawl-jobs-v2-observer.py \
  --report /tmp/opencode/m4-c0-architecture-contracts-2026-10-12/correctness/tests.json
```

**229/229 PASS**, 16.390 seconds, Python 3.14.7. Inventory, started and passed IDs agree. No skips, failures, errors, expected failures, unexpected successes, ResourceWarnings or ignored exceptions. All 202 merged baseline IDs pass; exactly 27 added IDs belong to `test_architecture_contracts`. The deterministic JSON report matching the author's SHA does not substitute for this independent execution; its distinct log is retained.

The new suite covers positive canonical platform/preparation/deployment metadata, aliases/coercions, legacy schemas, mixed native/context/stream bindings, profiles, ELF headers, fragmented executable reads, unsupported builders/collectors/streams, and direct x64 SSH refusal before client/process I/O. The independent probes additionally enumerate the full 16-way preparation/context/binding/IDENTITY architecture product: only all-aarch64 is accepted. Correctly MAC-signed version-1 KERNEL, NAMESPACE, WINDOW and SEALED frames each reject. Preparation mutation during authorization cannot relabel the in-flight lease.

The coverage gaps are the three findings above: image checks are tested at creation but not foreign-image startup; callback tests do not cover the mutable anchor; build negatives test unsupported requested architecture rather than mismatched actual target bytes in record mode.

### Actual static artifacts

Without executing them, independently parsed the supplied target, observer and oracle interpreter as **ELF64 LE / ET_DYN / machine 183**. Verified:

- Target SHA `07ae68522b7e87560bda4d32fcb0f244c74fd748f7cd9ccfe1ac3cf2321deab5`; observer SHA `53800d2281f57341269a0f1c9686809883241ed01872338b0544bf0439e88120`.
- Target instruction-manifest symbols, file offsets, raw instruction words and executable mapping against an independent ELF table reader; manifest version/platform/hash/size and every generated numeric header macro; observer contains the target hash.
- Actual observer `obs_filter` bytes at file offset **3498664**: **272 bytes / 34 instructions**, exactly equal to generated and exported BPF, SHA `701824ff5f8ccafa359f711d2714b23c8a303789cd1987a3a85aba19a3667b93`.
- All three exported profiles and BPF equal the generators from both the snapshot and merged base. Generated case/filter headers also match.
- Extracted all four native printf frame templates from both ELFs; simulated substitution matches the Python canonical version-2 fixtures. All non-validator cases fit the 1024-byte frame bound with those fixture observations. This is static format validation, not native collection.
- Toolchain version/platform/target/observer joins and all nine recorded native source files. Oracle manifest version/platform and seven payload source files, aggregate source digest, and actual interpreter SHA `cb4548d280f38c2b1df4cba45f12c90a034729e08fce5bf1a223c58add0e21e3`. Oracle manifest SHA is `6168e220727cb6377e8e637740bb8c297bf7bee929ab19aac6db90aa6fbcbfa5`.
- Independently recomputed both canonical platform digests and host-source, transport-source and oracle-worker source digests. Build-record source inventories, artifact hashes and log hashes match actual supplied bytes.

The initial static-check harness run had a reviewer-only regex error parsing digits in SHA field names. It was corrected and the complete static checks rerun successfully; `static-checks-attempt1.log` preserves that history. It was not a package test failure.

## Untested boundaries and constraints

- No native target/observer, raw oracle interpreter, real oracle worker, real host collector, live SSH, Docker/Podman workload, tracing, calibration, installation or host-policy mutation was performed. The requested suite uses benign Python processes with simulated observations and offline `ssh -G`, not live SSH authentication.
- No independent rebuild, image extraction, compiler/library-byte verification, rootless builder-state inspection or container-inventory observation was performed. Supplied build logs/metadata are corroborating evidence, not independent runtime/build-environment attestation.
- Native runtime-image feasibility, actual image/daemon/kernel placement, OCI enforcement, effective installed sshd/service policy, hardware tracing, timing and remote cleanup remain unaccepted. The ELF-header classifier is not treated as full ELF admission.
- The author's full umask-077 run was not independently repeated. This review independently ran the required full suite under 022 and verified preservation of the explicit umask fix.
- Reports/probes are confined to this private mode-0700 directory. The requested suite used its owned temporary fixtures under `TMPDIR=/tmp/opencode`. No repository, frozen package, documentation or Obsidian changes, and no subagent spawning, were made by this reviewer.

## Evidence identities

| File | SHA256 |
|---|---|
| `integrity-before.json` | `24b805df60ea84e40ec35c66a0c77d15177c0d6cde4e03eab2d63a26a5e4db3f` |
| `integrity-after.json` | `627f12af8bada418fd5adb2b38876ae209d78102c5c7c6a0ec06255dcc3e28ff` |
| `tests.json` | `c426dc3172d90a4592cfbf8549092ed17c11e79b2245589d65308838be884eaa` |
| `tests.log` | `5c91070fc5792db831a844f421fbcd5202bcf50e4ce74722285ad32bf6688db7` |
| `test-verification.json` | `4bc088c00f51499202f426e6fe50545c95a076cb72f8157383da4e376e87cf41` |
| `artifact-checks.json` | `781ef9e83dfd7e9e91742e6f1bbc90f4273a6e8657848d2e42099be87cf04294` |
| `probes.json` | `12fa8e1d915abdfc0885b8036e389b5ed8601e6507c2f975301dbaaffb793f3f` |
| `probes-recheck.log` | `6b18e22193daa77a8ef9d1c2927dcaf9fc183e15243de07d3145ce3abe5b9490` |

Final report and complete private evidence hashes are in `SHA256SUMS` and returned to the parent session.

## Next step

The canonical mapping, ordinary-path validation and artifact preservation provide a solid base. Close the three demonstrated boundary gaps, add their focused regressions, regenerate affected source/artifact identities, then repeat the correctness gate. Keep publication and native runtime authorization separate.
