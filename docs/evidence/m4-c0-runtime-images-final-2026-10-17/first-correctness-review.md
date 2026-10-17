# C0-ARCH-04 — final bounded correctness review

**Verdict: GO_FOR_CONTINUED_IMPLEMENTATION**  
Date: 2026-10-17. **All COR-001 through COR-006 findings are closed within this review's static-image scope. No new blocker was found in the materially changed logic.** This is not runtime acceptance, deployment authority, registry-publication approval or a hosted-CI result.

The follow-up correctly carries executable intent through required interpreter edges, checks access before visited-node deduplication, prevents collision with the reserved interpreter edge, and handles normalized root names through the root checks. Conservative hardlink handling preserves ordinary direct hardlinks without treating a symlink inode as its referent. The positive controls are important: ordinary read-only shared libraries, legitimate root aliases, direct regular-file hardlinks and normal symbolic-link chains still pass.

## Reviewed identities and preservation

- Repository HEAD: `7ef2041e2844aa34bec54da61c2e8469c1382bac`; branch `feature/crawl-jobs-v2-c0-runtime-images`.
- Final source inventory SHA-256: `83288b8fd89fcbd6453626df75b6b48dd02150fda5def59d0942d92dc6483a6b`.
- Artifact-revalidation record SHA-256: `40e1106f132e054fa7654a94bba30ff85a3567b738f057527e24bba21704b851`.
- **58/58 inventoried repository/snapshot file pairs match before and after**: 55 package files and three auxiliary files. The snapshot and package inventories are exact.
- **1,027 protected prior files remain unchanged**, covering both earlier review/source roots, both pass5 image sets, both reused remediation image sets and durable retention under `out/c0-runtime-images-2026-10-17/14ac223d10a72/`.
- All new writes are confined to `/tmp/opencode/c0-runtime-images-final-2026-10-17/correctness/`. No prior reports, probes, fixtures, source files or artifacts were edited.

The initial after-check was deliberately strict about all files in the final review's parent directory. It detected two **new, unrelated sibling utilities**, `persist-images.py` and `export-review.py`, not changes to any existing reviewed file. The original baseline was preserved; the final check requires exact equality for every originally present input and all protected prior files, and records these additions separately. Neither utility was read as code or executed. See `integrity-difference.json` and `inventory-after.json`. Unrelated Git-status changes were not compared.

## Closure results

| Finding | Final status | Evidence |
|---|---|---|
| **COR-001 — Docker native PATH** | **CLOSED, retained** | Podman-only driver, CLI and workflow are byte-unchanged from the reviewed remediation. Native empty-Env positive and PATH-refusal controls pass; all six actual metadata checks pass. Current image tests retain unsupported-Docker refusal coverage. |
| **COR-002 — host-extension leakage into x64 test** | **CLOSED, retained** | `build_oracle.py` and `test_x64_runtime.py` are unchanged. Current image tests pass the private extension-inventory/foreign-ABI controls, using the real validator and inert files. No hosted ARM run is claimed. |
| **COR-003 — portable normalized duplicates** | **CLOSED, retained** | Original positive portable fixture still passes; original config and layer alias collisions still refuse with `IMAGE_SAVE_DUPLICATE_PATH`. |
| **COR-004 — required file/interpreter accessibility** | **CLOSED** | Old loader-mode failure now refuses. Both architectures, both visitation orders, alias traversal, readable/non-executable loaders and unreadable loaders are covered. Ordinary mode-0644 shared libraries still pass. Both actual oracle trees reject the same RAM-only loader-mode mutation that previously passed. |
| **COR-005 — dependency search semantics** | **CLOSED within the documented pinned-base model** | Original symlink-directory positive and RPATH/RUNPATH negative retain the correct result; current regression tests retain explicit legacy-RPATH refusal and malformed-link controls. No full dynamic-loader emulation is claimed. |
| **COR-006 — normalized root aliases** | **CLOSED** | Canonical/alias root positives pass with and without an external observation; alias duplicates in both orders and conflicting observations refuse. Exact old failing archives now refuse. Root aliases never populate `entries['/']`. |

### COR-004 and reserved-edge checks

Reviewed `image_artifacts.py:227-229,271-282`.

- Required `PT_INTERP` edges carry `executable=True`.
- `tree.file(path, executable=executable)` runs **before** the visited-node early exit. An earlier ordinary/read-only visit therefore cannot grant a later interpreter edge executable status.
- Inert loader fixtures for **AArch64 and x86-64** accept mode 0555/0755; reject mode 0644/0744 for missing execute access; reject mode 0111 for missing read access. Both required-list visitation orders were tested.
- A read-only ordinary `DT_NEEDED` library remains valid. The reserved literal `DT_NEEDED="PT_INTERP"` refuses with and without an actual interpreter; the ordinary lookalike `PT_INTERP.so` remains valid and need not be executable.
- Alias-directory access is still checked, including when the resolved loader was already visited.
- For each **real oracle tree**, the original mode-0755 loader passes. Changing only its in-memory mode to 0644 now raises `IMAGE_FILE_EXECUTABLE`; restoring the row returns the original byte-identical content record. **No archive or filesystem permission was changed.**

### COR-006 and hardlink checks

Reviewed `image_artifacts.py:89-101,126-158`.

- `.`, `./.`, `.//.` and `././` roots share root observation, duplicate and permission handling. Valid single roots pass; duplicates refuse even when their metadata agrees; conflicting observations refuse with `IMAGE_ROOT_CHANGED`.
- A restrictive root without an external observation fails the actual directory-access check, rather than being silently ignored. Headerless fixtures receive legitimate root metadata; no unrelated missing-root refusal was counted as closure.
- Direct regular-file hardlinks with matching mode/UID/GID pass at modes 0644 and 0755. Each metadata field was independently corrupted and correctly refused with `IMAGE_HARDLINK_METADATA`.
- Hardlink targets involving a symlink inode, symlink prefix, another hardlink, a missing target or a directory refuse. Ordinary symlink chains and a symlink to a valid direct hardlink remain supported.
- The stricter hardlink model is intentional and conservative; no materially changed acceptance path produced a blocker in these controls or the actual archives.

## Construction continuity and six-image replay

The retained image sets intentionally carry the **14ac223d** source map. The final map differs in exactly:

1. `tests/crawl-jobs-v2-observer/image_artifacts.py`
2. `tests/crawl-jobs-v2-observer/test_image_review.py`

I independently recomputed that delta and matched it to both the final inventory and the supplied revalidation record. The **21 closed construction inputs**—native compiler inputs, Dockerfiles/ignore files and oracle copied/build sources—are unchanged. The construction driver and all three auxiliary files are unchanged. Neither changed host-side file is an image construction input. Accordingly, reuse is justified; an image rebuild is not required for this delta.

Using the **final validator and retained observed root metadata**, I replayed all six actual retained images:

| Check | Result |
|---|---|
| Native/oracle artifacts, canonical profiles/BPF/manifest, compiled observer binding and source identities | PASS |
| Role OS/architecture, image ID, user, entrypoint, command, environment and hooks | PASS for all six |
| Portable config hash → image ID; config → inspect; diff IDs → inspected RootFS; actual layer hashes | PASS for all six |
| Independent in-memory portable-layer overlay → flattened tree | **Zero differences** in all six inventories |
| Canonical final content records → retained content files | **All six are byte-identical**, not merely equivalent parsed objects |
| Required ELF dependency closure | Native: one role ELF each. Oracle: 18 required nodes each; no unresolved required edge |
| Full oracle ELF inventory | ARM: 790; x64: 791. Optional Tcl/Tk absence and ET_REL object handling remain unchanged |
| Root metadata | Root-owned native roots 0555; oracle roots 0755; before/after observations match the content and inspection-container record |
| Historical command/cleanup joins | All 61 log records per set match hashes; five distinct never-started inspections/removals per set, three consistent state inspections each and empty absence-query logs |
| Durable retained copies | All **103 manifest-declared files** match recorded sizes/hashes; image artifacts equal the reviewed temporary sets; retained source equals the original construction inventory |

The overlay comparison covers file-content hashes, kinds, UID/GID, modes and image-local link targets; not timestamps/xattrs. Export root metadata is joined from the retained observations because ordinary exports omit the root header. No fresh root observation, helper execution or engine inspection occurred.

## Tests and limits

- **90/90 independent probe groups PASS**, with unique IDs and paired positive/negative controls. Both real-tree loader mutations also refuse correctly.
- **30/30 current image unit tests PASS**, run against the frozen final snapshot with `-B -W error::ResourceWarning` and temporary files only inside this new review directory.
- Both supplied full source-bound reports are **286 PASS**, retain every one of the **283 prior IDs**, add the three stated regression methods, and have no skips/errors/failures. Those full suites were verified, **not rerun here**.
- No engine commands, builds, pulls, image/container mutations, loads/starts, mounts/unmounts, mounted-root helper execution, C0 actor/collector/worker-main execution, tracing, installed filters or deployment.
- Existing engine lifecycle tests use fake engines. The root helper is only retained command text in the evidence; it was not executed, even with mocks, during this final review.
- Archive members were read as streams only, never extracted onto the host or executed. No previous review script was run.
- The native hosted image matrix remains **unexecuted**. Existing observations and cleanup are historical evidence, not new live-engine or native-host admission proof.
- Dynamic-loader execution/symbol versions, lazy loading, confinement, hardware behavior and calibration remain C0-ARCH-05; registry publication remains C0-ARCH-06.

## Reproduction and next step

```sh
R=/tmp/opencode/c0-runtime-images-final-2026-10-17/correctness
PYTHONDONTWRITEBYTECODE=1 python3 -B -W error::ResourceWarning "$R/probes.py"
PYTHONDONTWRITEBYTECODE=1 python3 -B -W error::ResourceWarning "$R/replay.py"
PYTHONDONTWRITEBYTECODE=1 python3 -B "$R/integrity.py" after
```

`review.json` contains stable finding closure, exact source/artifact hashes, evidence-manifest hashes and limits. `probes.json`, `replay.json` and the integrity manifests retain the detailed checks. **Continue implementation/documentation with these closures preserved; obtain separate approval and evidence for hosted/runtime work.**
