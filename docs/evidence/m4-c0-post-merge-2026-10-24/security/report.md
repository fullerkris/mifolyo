# MiFolyo C0: exact merged-main images and preserved C0-ARCH-05 sync

**Date:** 2026-10-24  
**Reviewer:** Security Engineer, independent read-only review  
**Verdict:** **GO_FOR_CONTINUED_IMPLEMENTATION**  
**Open security findings:** 0 in the reviewed scope.

This verdict covers the retained static verification of the six exact merged-main image artifacts and preservation of the already-reviewed C0-ARCH-05 candidate during branch synchronization. It does **not** certify a runtime adapter, native host admission, native calibration, execution authorization, or M4 acceptance.

## 1. Scope and evidence trust

- Exact main commit: `e8a0a415ac2b370319cd57f4c5262ce616a536fb`; tree: `aaf57fc74e5799d1b5d21f58cce18747623a7676`.
- Retained GitHub run: `39858333396`, `push` / `main`, attempt `1`, repository `fullerkris/mifolyo`, repository ID `1220983139`; two successful architecture jobs.
- Frozen packet: `out/c0-hosted-images-2026-10-24/review-inputs.json`, SHA-256 `d63b6e8d9b9bab7d0a883d2cbdaa59eb7a8de8050e56dae28b447a2e638a315d`.
- All **428 packet files / 729,668,911 bytes** matched their recorded sizes and SHA-256 values before and after review. No symlink occurred along their checked paths. The before/after file maps are equal.
- An additional **213 supporting files** were inventoried before and after the independent checks and checked again at completion: the 120 durable candidate copies, 55 partial initial snapshot files, six prior-review metadata files, and 32 prior image-set/stable-artifact/content files. No changes occurred. The old review metadata was also matched to exact-main Git blobs; old image files were checked against the retained manifest.

The supplied packet hash, local exact-commit Git objects, and retained GitHub responses are the trust anchors. Matching the downloaded ZIP bytes to GitHub's **retained** digest metadata establishes consistency with that evidence; this review did not reauthenticate GitHub online or establish an independent runner/signing attestation. These are task-specific retained verification tools, not a proposed general service for attacker-selected archives.

### Boundaries reviewed

| Boundary | Checked protection |
|---|---|
| Run/repository identity to archive bytes | Exact commit/run/attempt/repository/job/artifact IDs, metadata digest, pinned SHA-256, byte count |
| ZIP names to local output paths | Closed, canonical relative paths; no links/special entries; entry/size limits; exclusive file creation; dedicated output directory |
| Image paths to verifier host | No image-path extraction or image execution; archive data interpreted as records and inert tar bytes |
| Image bytes to imported validators | 55 package + 3 auxiliary source files independently matched to exact-main Git blobs and the reviewed inventory |
| C0-ARCH-05 candidate to new Git base | Durable copy, original-base backup ref, retained stash, source hashes, recorded restoration delta, and test-report continuity |

## 2. Actual checks and results

### Download and source binding — PASS

Reviewed `download.py:25-100`, `verify.py:18-23,175-225`, the exact-main workflow, and the imported archive/content/build-artifact validators.

- Independently streamed and CRC-checked every original ZIP member, hashed its uncompressed bytes, and compared those bytes to both the extracted file and download inventory: **84 aarch64 + 85 x86_64 files**, 324,382,137 uncompressed bytes. No extra extracted files, unsafe names, encrypted members, symlinks, or special member types were present.
- Both ZIPs contain regular files only. The four native executable payloads have executable metadata inside their ZIPs but remain **non-executable local files** after the downloader's ordinary file writes; this review did not execute them.
- Rechecked **58 exact-main source blobs** against the frozen source snapshot and prior inventory SHA-256 `1f0e82a5ca98f02bf36deb4292501a0b97a38ac8bf567fd170fc09c8d4890376`. The package has exactly 55 tracked files; the other three files are the actual reviewed auxiliary paths. No live C0-ARCH-05 additions were substituted into image verification.
- Both hosted reports bind the exact commit, the same **288 test IDs**, 55 source hashes, and exact runner hash. Inventories equal started/passed lists; no skips, failures, errors, or expected/unexpected outcomes were hidden. The retained x86 workflow includes the simulated debug-register control, not native calibration.

### Archive parsing, layer replay, and content — PASS

Reviewed `verify.py:42-113,228-256` and frozen `image_artifacts.py:31-49,52-187,189-391`.

- Revalidated all **six** Docker archives/config identities, architecture/configuration metadata, and **14** raw uncompressed layer digests with the reviewed validators.
- Independently constructed an in-memory semantic map from each original layer, rather than relying on the new replay's reported success. Compared **every** final member's type, UID/GID, mode, link target, size, and regular-file SHA-256 against the already-retained flattened tar.
- Matched **13,171 final members** in total: aarch64 oracle 6,581, x86_64 oracle 6,586, and one member in each of the four native images. Full-map digests are retained in `replay-checks.json`.
- No whiteouts, sparse files, device/FIFO entries, type-changing replacements, or replacement of an existing hardlink target were present. The only PAX keys were `path` and `linkpath`; no unmodeled capability/xattr metadata was discarded in these exact inputs. Replacement paths and root-header lists independently match `verification.json`.
- Each oracle has two hardlinks (`perl5.36.0` to `perl`, and `uncompress` to `gunzip`). Targets precede aliases, are regular files, retain matching inode metadata, and are not replaced later. Final member parents are literal directories, not symlink-shadowed paths.
- Recomputed all six complete content records with the exact reviewed validators and compared canonical records and hashes to hosted evidence. Rechecked **1,585 ELF records**, including **40 required-closure records** across the six images. Required dependency closure passed without executing ELF files or using a dynamic loader.
- The **12 stable native/oracle artifacts per platform** are byte-identical to the old reviewed artifacts. All six declared/ELF payload hashes equal their old reviewed payload hashes. This is **not** a claim that old and new portable archive bytes or config IDs are identical.

### Root metadata semantics — PASS, deliberately separate evidence

- All six hosted root records are UID/GID `0:0`, mode **0555**, corroborated by two retained stopped-container host observations per image, ordered around export.
- Both oracle base layers carry a **0755** root header. The old reviewed oracle content records also report **0755**; the old native roots are **0555**.
- The replay records layer root headers separately and does not synthesize an assertion that those headers determine the host-observed root. Its flattened tars contain no root header; `ImageTree` receives the separately checked host observation. The resulting complete hosted content records match exactly.
- This evidence does not establish the root mode a future engine/host will expose. Native admission must measure its own conditions; neither the host observation nor a layer header is a substitute for that admission.

### Privilege, lifecycle, and no-execution boundaries — PASS

Reviewed `verify.py:116-172`, frozen `image_build.py:18-28,107-165,168-232`, and the exact workflow/Dockerfiles.

- Independently hashed and checked all **122 command logs**, including closed allowed operations, actual argument shapes, and lifecycle order for **ten** inspection containers.
- All creates specify the inert inspection entrypoint, network `none`, read-only filesystem, UID/GID `999:999`, capability drop `ALL`, no-new-privileges, and no healthcheck. Image builds use network `none` and pinned, separately pulled bases; the recorded builder is rootless Podman **4.9.3**.
- Each owned inspection container has three consistent stopped inspections, PID zero, zero `StartedAt`, removal, and a subsequent empty owned-ID query. The six role exports have matching before/after root observations. The other four stopped containers only supply build artifacts by copy.
- The retained command stream and reviewed builder code are consistent with **never starting the inspection/C0 entrypoints**. This is evidence verification, not a forensic proof that no unrecorded action occurred on a hosted runner. Hosted construction necessarily executed build tools; the x86 simulated helper also ran in its workflow. Those facts are not conflated with starting C0 runtime actors.
- This review itself made **zero image-engine calls, remote calls, native calibration runs, or image-path extractions**. It imported only frozen validation modules and loaded only the `reconstruct` function's AST for small controls, not the verifier/downloader entrypoints. A Python audit guard prohibited process/network calls and writes outside this review directory during replay checks; no prohibited calls occurred.

### Branch synchronization and candidate preservation — PASS

- Current branch and `origin/main` resolve to exact main. Backup branch `backup/c0-native-calibration-before-main-2026-10-24` resolves to original base `babb2180d05698e64b7d9baf37f9cab14b3c0f7f`.
- Retained stash `2475fda26223c43485b933089274476e497be3ab` has the expected original-base parent and separate untracked parent. Its index parent equals the original base tree.
- All **120 durable copies** match the frozen before manifest in bytes, length, and mode. The stash independently contains the same candidate bytes/modes: **2 tracked changes + 118 untracked paths**, with no candidate path omitted.
- All **61 reviewed package/script paths** still match the prior final-source inventory. The restored roadmap equals the before-copy roadmap plus exactly the **six upstream Go-security lines**; the same addition is independently present in the old-base-to-main Git blobs.
- The retained successful sync record reports 119 byte-identical restored paths plus the roadmap delta. At review time, the two explicitly mutable chronology documents had subsequent edits; they are not represented as still byte-identical or certified by this verdict. The frozen source/evidence and recovery copies remain unchanged.
- The two retained umask reports are byte-identical to the historical **318-ID / 59-source-hash** report, with complete started/passed lists and bound log hashes. This review checked report/source continuity; it did **not** rerun these broad suites.
- The initial helper's nonexistent auxiliary path is transparently retained in `initial-check-failure.json` and `check-sync-initial.py:55-59`. Its 55 partial snapshot files match main. The corrected helper selects the three real auxiliary paths from the reviewed inventory and produces the independently verified 58-file snapshot. The partial snapshot is not imported by `verify.py`.
- Git reads used no optional locks or lazy fetching. The checked refs and index remained unchanged; index SHA-256: `1516b692036491e65496c39bbb37c4dc89d3b7ba146daa096e60026b41118358`. No stash application, pull, reset, checkout, staging, commit, or other Git mutation was performed by this review. Historical pull/conflict-free claims were not reenacted; preserved contents and resulting bindings were checked directly.

## 3. Targeted independent controls

**18/18 passed:** two positive controls and sixteen expected rejections. These are small local synthetic fixtures, not a redundant rerun of the prior broad test suites.

Positive controls: valid hardlink replay/resolution; valid portable archive/config/layer validation.

Expected rejections: whiteout; parent traversal; absolute member; normalized duplicate; type-changing replacement; existing hardlink-target replacement; hardlink to symlink; hardlink metadata mismatch; special device; duplicate root; inspected-config mismatch; config-ID mismatch; tampered layer digest; wrong architecture; conflicting layer/host root metadata; flattened symlink parent.

The exact replay function was tested with assertions enabled. The retained verification scripts use `assert` and are **not** assessed for operation under `python -O`, adversarial concurrent mutation, or reuse as a generic untrusted-archive service. No changes to those scripts are required for the supplied exact-artifact task on the evidence reviewed.

## 4. Findings and limits

**No actionable security vulnerability or blocking evidence inconsistency was demonstrated in this exact scope; no remediation patch is requested.** The root-mode difference and initial helper failure are correctly separated/disclosed, not suppressed successes.

Excluded: fresh network/GitHub verification; fresh builds or image-engine loads; generic archive-service hardening; broad parser fuzzing; a new dependency/CVE audit; compiler or hosted-runner attestation; native target/observer/oracle execution; runtime adapter certification; remote/native preflight; native admission/calibration; modifications to current chronology documents. Static payload/ELF checks cannot establish runtime feasibility or native admission.

Continue implementation only under these limits. Different artifacts, source bindings, replay semantics, or runtime claims need their own review.

## 5. Key identities and retained outputs

| Input | SHA-256 |
|---|---|
| aarch64 artifact ID `12461066588`, ZIP | `7cac65afbed201004f1417def2a5c2ea28f1e0e8766ef8c8e1cf4e6d158e22f8` |
| x86_64 artifact ID `12459699018`, ZIP | `6b30fdb8d9661fd08d9acd6eaecf0ecf81e45eff603a3e84c61778d8a28ee8e9` |
| `download.py` | `61ad001df8ef365069cc4b25689d224a41b37fe07bc7a7c6dbeb36a8475da651` |
| `verify.py` | `e0a6a6ebe1b449c53baf55042c942a4f713c140712912d71b2438096153d7f07` |
| `verification.json` | `309ac683b4180bd3aa89b937f2216c13a62e2c79c6619697c9c5e6812751e698` |
| `before.json` | `28f2b3d1386d479708d7a47bb5dad9f9e2216bc05f0aaefdfeb81e423e456763` |
| `sync-verification.json` | `2db1c726cc04fa2280fa22fb4ef0792f15457854e02511ca506bfc0e17844cae` |
| `test-continuity.json` | `cbe7db52ada235b1a72e3b3906abd9ec7b90058015f39e1ac1067cb526a20146` |

All new files are confined to `out/c0-hosted-images-2026-10-24/security-review/`:

- `check_packet.py`, `integrity-before.json`, `integrity-after.json`: full frozen-input integrity checks.
- `check_evidence.py`, `evidence-checks.json`: source/Git/ZIP/preservation checks and read-only Git command list.
- `supplemental-integrity-before.json`, `supplemental-integrity-after.json`, `final-boundaries.json`: supporting-input and final boundary checks.
- `check_replay.py`, `replay-checks.json`, `fixtures/`: complete member comparisons, static content checks, command-lifecycle checks, and 18 targeted controls.
- `result.json`: machine-readable verdict, scope limits, checks, image identities, input hashes, and hashes of this report and generated evidence.

No source, retained input, or Git object/ref/index was modified. No work was delegated.
