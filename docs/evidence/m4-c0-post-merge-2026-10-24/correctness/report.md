# MiFolyo C0 — separate read-only correctness review

**Verdict: GO_FOR_CONTINUED_IMPLEMENTATION**  
**Scope:** exact merged-main hosted image download verification and preservation of the already-reviewed C0-ARCH-05 preflight candidate during synchronization.  
**Not native runtime acceptance; not authorization to execute or calibrate.**

No blocking correctness findings were identified in this frozen packet. The exact artifact inventories, unchanged reviewed verifier, source-bound results, and retained backup/stash provide a strong, independently checkable preservation chain.

## Revision and integrity boundary

- Merged main / current feature HEAD / local `origin/main`: `e8a0a415ac2b370319cd57f4c5262ce616a536fb`.
- Git tree: `aaf57fc74e5799d1b5d21f58cce18747623a7676`, matching the supplied approved PR-tree identity.
- Backup branch: `backup/c0-native-calibration-before-main-2026-10-24` at `babb2180d05698e64b7d9baf37f9cab14b3c0f7f`.
- Retained stash: `2475fda26223c43485b933089274476e497be3ab`.
- Frozen packet SHA-256: `d63b6e8d9b9bab7d0a883d2cbdaa59eb7a8de8050e56dae28b447a2e638a315d`.
- All **428 bound files / 729,668,911 bytes** matched before and after review. An additional **329 relevant historical/backup files** were hashed before and after; none changed. Git refs, HEAD, branch, and index hash also remained unchanged, with no staged changes or unresolved index entries.
- Current chronology documents were being updated by the coordinating session as allowed. Those edits are not attributed to this review and do not supersede the frozen restored-plan snapshot or historical evidence.

## Checks actually performed

### 1. Exact source and revision binding

Read Git objects directly, rather than accepting `sync-verification.json` alone. All **55 package files plus three auxiliary files** in `verified-merged-source/` exactly match the merge commit and final C0-ARCH-04 inventory. The snapshot has exactly those 58 files. The 48 historical closure-evidence files also match their committed Git blobs. The supplied aggregate verification records were separately checked against the independent results.

### 2. Hosted provenance and downloaded bytes

Checked retained run, job, step, repository, and artifact metadata: run **39858333396**, **push/main**, **attempt 1**, exact merge SHA, both native architecture job labels, and successful required steps. The ARM64-only skip of the x64 simulated debug step is intentional; neither 288-test report contains skipped, expected-failure, unexpected-success, failure, or error entries.

Both ZIPs match their pinned GitHub metadata digests and byte counts. Independently streamed **every ZIP file entry**, exercising CRC checks and recomputing its SHA-256; compared exact ZIP inventories against extraction directories and download records. No missing or additional extracted files were found.

| Architecture | Artifact ID | ZIP bytes | Exact extracted files |
|---|---:|---:|---:|
| aarch64 | 12461066588 | 54,045,146 | 84 |
| x86_64 | 12459699018 | 55,015,916 | 85 |

Each hosted test report has exactly the historical **288 unique test IDs**, all started and passed, the exact **55-file** source inventory, and the reviewed runner hash. The retained x64 debug result states 29 simulated controls, not native tracing acceptance.

### 3. Independent image replay

Did **not** run the supplied download/verification helpers or invoke an engine. Independently checked all six portable archive configurations and **14 uncompressed layer digests**. A separately implemented layer map checked every reconstructed name, kind, file byte hash/size, link target, UID/GID, and mode: **13,171 final members** across six images. Checked the actual whiteout-free/replacement/hardlink constraints and PAX metadata present in these layers. The layer-root headers were retained separately, not substituted for stopped-container root observations.

Then replayed `archive_record`, `verify_artifacts`, `ImageTree`, and `verify_content` from the exact, unchanged merged-source verifier. All six canonical content records match the hosted records **byte for byte**, including their full-record digests. All **12 native/oracle artifact files per architecture** match the prior reviewed local bytes, and all six payload digests match the historical reviewed records. All 105 entries in the old durable-retention manifest were also independently verified.

Oracle ELF inventories contain **790 ARM64 / 791 x64** records; the required Python/oracle dependency closure contains **18 per architecture**, with no unresolved dependencies. Native target/observer each have one verified static ELF. This is not a claim that every optional Python extension is usable: the recorded, nonrequired `_tkinter` extension still has unresolved `libtk8.6.so`/`libtcl8.6.so`, unchanged from the reviewed baseline.

### 4. Recorded commands, stopped state, roots, and cleanup

Rehashed **61 command logs per architecture** and checked command arguments, architecture/base pins, inspection-image identities, and operation order—not just success flags. Checked the recorded rootless Podman **4.9.3** versions, restricted inspection creates, three never-started inspections for each of five owned containers per architecture, exact remove/absence checks, and all **10 cleanup records**.

Verified all **12 root observations**, two per final image, surrounding the corresponding export. Hosted observations report root **0555** for all roles. The oracle layer root header is **0755**, and the earlier local oracle observation is **0755**. These are three distinct evidence sources. Hosted versus historical oracle full content records differ **only in `root_directory`**; payload equality does not establish root equality.

### 5. Candidate restoration and test continuity

Verified all **120** retained before-snapshot files against their recorded bytes and modes, and against the stash's **two tracked / 118 untracked** paths. The stash first parent is the backup commit; its index-parent tree matches that base. `before.patch` exactly matches the stash's tracked binary diff. The branch reflog is consistent with the zero-new-commit rebase onto the merge commit.

The frozen restored plan differs from its preserved predecessor by exactly the same **six inserted upstream Go-security lines** as the old-base-to-main Git diff. This supports the recorded **119 exact / one plan-only** restoration result. Live chronology edits are separate: the two existing checkpoint documents had subsequently changed during review, while the **61 reviewed package/script hashes** and **111 historical evidence exports** remain intact.

Both retained integrated test reports match all **318 historical unique IDs**, all success/error-status lists, **59 source hashes**, and the runner hash. Parsed both logs and matched all 318 individual successful test IDs, final success summaries, and continuity-record hashes. No broad suites were rerun. These deterministic reports correctly retain `commit: "local"`; the separate Git/source packet binds the integrated baseline. The `022`/`077` labels identify the supplied runs; their process umasks were not independently measured in this review.

### 6. False-positive controls

**26 negative controls passed**: wrong but syntactically valid image IDs, swapped architectures, unsafe writable roots, mismatched oracle source/native artifact bytes, and config/layer byte corruption. The two deliberately damaged portable archives are inert fixtures under this review directory only.

Two additional root-boundary controls confirmed that substituting 0755 for the observed 0555 oracle root preserves the payload hash but changes the full content hash to the historical record. Thus this review cannot silently treat payload equivalence as complete root-metadata equivalence.

## Findings and preserved failures

- **🔴 Blockers: none.** No remediation is required for the exact reviewed packet.
- **🟡 Unresolved correctness findings: none within this scope.** Runtime admission and calibration remain separate work, not findings closed by this review.
- **💭 Preserved sync-harness failure:** `check-sync-initial.py:55–56` references nonexistent `scripts/prepare-crawl-jobs-v2-observer.py`. The retained failure record and 55-file partial `merged-source/` snapshot are consistent with that error. The corrected helper uses the reviewed three auxiliary paths and a separate 58-file `verified-merged-source/` snapshot. No product or restoration defect was inferred from this harness failure.
- **💭 Reviewer fixture correction:** the first wrong-config-ID probe used an all-zero digest, correctly rejected earlier as `DIGEST`, rather than the expected `IMAGE_SAVE_CONFIG`. The fixture was changed to a valid-but-wrong nonzero digest, and the complete checks then passed. The original failure log and exact correction/hash are retained in `image-checks.log` and `probe-adjustments.json`; it was not a product failure.

## Evidence and limitations

Primary new evidence: `audit-before.json`, `audit-after.json`, `source-sync-checks.json`, `image-checks.json`, `summary-bindings.json`, their logs, and the accompanying reproducible probe sources. `result.json` records detailed hashes; `review-artifact-sha256.json` seals the review deliverables and fixtures.

| Input/result | SHA-256 |
|---|---|
| ARM64 ZIP | `7cac65afbed201004f1417def2a5c2ea28f1e0e8766ef8c8e1cf4e6d158e22f8` |
| x64 ZIP | `6b30fdb8d9661fd08d9acd6eaecf0ecf81e45eff603a3e84c61778d8a28ee8e9` |
| Supplied hosted verification | `309ac683b4180bd3aa89b937f2216c13a62e2c79c6619697c9c5e6812751e698` |
| Supplied sync verification | `2db1c726cc04fa2280fa22fb4ef0792f15457854e02511ca506bfc0e17844cae` |
| Independent image checks | `20d80e3e062220d56385c9c7489b0f46b55ebe1a6dd01f067efed8ade8d9184b` |
| Independent sync checks | `5d79c8cd1618db2eb7e364374734f7a30e980f536de74be0fdf1bb97b6864f57` |
| Final before/after input audit | `26ddc6395a28b45fe1f1ef59be12db6a8a93ac38d61e0b571778872fe70ec768` |

This is a local read-only correctness review, not a fresh GitHub attestation or independent observation of the original hosted execution. No network/API fetch, pull, rebase, stash apply, container/host command, actor execution, calibration, additional agent, commit, or push was performed. Hosted actions are supported by the retained metadata/log chain and byte/source comparisons. The original exported container tars were not downloaded; reconstruction was checked against the portable archives and retained hosted content records. The layer replay is intentionally specific to these pinned whiteout-free archives, not a general OCI filesystem implementation. Metadata such as timestamps is not claimed identical across builds.

**Next step:** continue C0-ARCH-05 implementation with its reviewed inputs preserved. Eligible native hosts, runtime adapter/admission boundaries, calibration, and independent runtime acceptance remain outstanding and require their own authorization and evidence.
