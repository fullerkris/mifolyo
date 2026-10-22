# C0-ARCH-04 remediation — independent correctness re-review

**Verdict: NEEDS_REMEDIATION**  
Date: 2026-10-17. Original findings: `C0ARCH04-COR-001` through `005`.

The remediation is substantial: **all original concrete reproducers now behave correctly**, and **all six corrected image artifacts pass the actual artifact/source/config/cleanup joins**. The Podman-only construction path, architecture-isolated extension fixtures, normalized portable-member checks and component-ordered link resolution address the original problems directly.

I cannot mark every finding fully closed yet. **COR-004 remains partially open:** a required ELF interpreter (`PT_INTERP`) is still accepted without execute permission. I also found **COR-006**, a root-header normalization gap in the new root-metadata handling. These are bounded static-validator correctness issues, not evidence that either supplied image set contains a bad payload. **The supplied images have executable loaders and do not contain the conflicting root headers used by the probes.**

## Preservation and source identity

- HEAD remains `7ef2041e2844aa34bec54da61c2e8469c1382bac` on `feature/crawl-jobs-v2-c0-runtime-images`.
- Corrected inventory SHA-256: `14ac223d10a72ff859d653c29bf721e35af320fc3dbfe823a4c8176180576670`.
- **58/58 repository/snapshot files match before and after:** 55 package files plus the three auxiliary files. Inventories are exact; unrelated Git-status changes were not compared.
- Both corrected image source snapshots match the 55-file package map, SHA-256 `2f4a4e5e51b4dc3e490b50266e9fcad279583307b9cd03eb7e0b5ea6dab8437a`.
- **All 49 files in the original `correctness/` directory remain byte-identical**, including reports, probes, reference text, logs and fixtures. The original scripts were not run or edited. Original inert fixtures were read directly where appropriate.
- **299 reviewed evidence files remain unchanged.** Full hashes are retained in `actual-verification.json` and `review.json`.
- New outputs exist only under `/tmp/opencode/c0-runtime-images-remediation-2026-10-17/correctness/`.

## Original finding closure

| Finding | Status | Independent checks |
|---|---|---|
| **COR-001 — Docker native PATH** | **CLOSED** | Docker is refused before engine access/output creation; Podman-only flags preserve empty native Env and absent labels; rootful preflight refuses before source copying/pull. CI selects rootless Podman. All six real image configs pass. |
| **COR-002 — ARM-host extension leakage in x64 test** | **CLOSED** | The original x64 test now passes with the same inert ARM extension injected into `sys.modules`. Real extension validation accepts both matching ABIs and refuses opposite ABIs before output creation. Worker main/Engine were never called. |
| **COR-003 — Portable normalized duplicate paths** | **CLOSED** | The original positive portable archive is accepted; both original conflicting `config.json`/`./config.json` and `layer.tar`/`./layer.tar` archives now refuse specifically with `IMAGE_SAVE_DUPLICATE_PATH`. |
| **COR-004 — Configured-user file/path accessibility** | **PARTIALLY CLOSED** | Every original native/source/extension/directory permission reproducer now refuses, with valid positive controls and legitimate root observations. Readable-but-not-executable native files also refuse. Required `PT_INTERP` execute access remains unchecked; details below. |
| **COR-005 — Dependency search semantics** | **CLOSED within the stated pinned-base model** | The original symlinked RUNPATH search succeeds; the old RPATH/RUNPATH false-positive refuses with `IMAGE_LIBRARY_MISSING`. A valid RUNPATH-positive case succeeds, active legacy RPATH explicitly refuses, and dangling/cyclic/ambiguous searches refuse. |

For every headerless original rootfs fixture, the probes supplied the explicit legitimate root observation `{uid: 0, gid: 0, mode: 0755}`. Positive native and oracle fixtures succeeded first. **No missing-root refusal was credited as fixing another finding.** Root-header probes use a valid single-root control or an explicit root observation.

## Remaining issues

### 🟡 C0ARCH04-COR-004 — required ELF interpreter execute permission remains unchecked

**Priority:** P2, unchanged from the original permission finding. **Closure:** partial; prevents satisfying the requested all-original-findings-closed condition.

**Locations:** `tests/crawl-jobs-v2-observer/image_artifacts.py:253-272`, especially `tree.file(path)` at line 265; contrast the main Python executable check at line 299.

`PT_INTERP` is added to the dependency graph, but traversal checks it only with `file(..., executable=False)`. Thus a root-owned mode-0644 loader passes all required-closure checks for UID/GID 999. Linux opens the ELF interpreter with `open_exec()`, which requests `MAY_EXEC`; read permission alone is insufficient. This is a static permission fact, not deferred runtime calibration.

**Independent evidence:**

1. `004-pt-interp-mode-0o755` is a valid positive inert oracle fixture and passes.
2. Changing only its loader mode to 0644 still passes (`004-pt-interp-mode-0o644`), with a valid root observation.
3. The same issue reproduces with **both actual oracle trees in memory**: changing only the loader row's mode from 0755 to 0644 leaves `verify_content()` accepting and returning the same payload digest. The original mode was restored in memory; no archive was edited.
   - ARM: `/usr/lib/aarch64-linux-gnu/ld-linux-aarch64.so.1`
   - x64: `/usr/lib/x86_64-linux-gnu/ld-linux-x86-64.so.2`

**Source corroboration:** retained Linux v6.18 `fs/binfmt_elf.c:907` calls `open_exec(elf_interpreter)`; `fs/exec.c:764-775` initializes `.acc_mode = MAY_EXEC`. The actual supplied loaders are mode 0755 and pass the reviewer's additional execute-bit check.

**Suggestion:** carry execute intent for required `PT_INTERP` edges, or explicitly call `tree.file(interpreter_path, executable=True)` for those required edges. Preserve alias-directory access checks. Ordinary shared libraries should still need read access, not indiscriminately require execute bits. Add both the 0755 positive and 0644 negative cases.

### 🟡 C0ARCH04-COR-006 — normalized root aliases bypass root deduplication and observed-metadata agreement

**Priority:** P2 / new correctness finding. **Actual image sets:** unaffected; the retained flattened exports omit root headers and use matching observed metadata.

**Location:** `tests/crawl-jobs-v2-observer/image_artifacts.py:89-98`.

Root handling occurs before name normalization and recognizes only the raw spellings `""` and `"."` after trimming trailing slashes. A member named `"./."`, `".//."` or `"././"` subsequently normalizes to `/` but is stored in `entries["/"]`, rather than checked as the root. `_resolve()` and `directory_access("/")` use `self.root`, ignoring that conflicting row.

**Independent evidence:**

- A single `.` root header with mode 0755 plus a valid target passes.
- A second exact `.` header with mode 0700 correctly refuses with `IMAGE_ROOT_DUPLICATE`.
- A second header with any of the three normalized aliases above, also mode 0700, **passes complete native content verification**, retaining `root_directory.mode = 0755` despite the conflicting root member.
- An archive containing only a `./.` root header with mode 0700 also **passes against an explicit observed root mode 0755**, bypassing `IMAGE_ROOT_CHANGED`.

The issue is not host extraction: none was performed. It is that the closed archive/root observation join accepts contradictory metadata for the same normalized path.

**Suggestion:** identify root paths after the same normalization used for ordinary members, then apply root count, duplicate and observation-agreement checks. Alternatively, explicitly refuse noncanonical root aliases. Do not permit an ordinary `entries["/"]` row alongside `self.root`. Add canonical-root, alias-duplicate and alias-versus-observation tests.

## Actual corrected artifacts inspected

Both `/tmp/opencode/c0-images-{aarch64,x86_64}-remediation-2026-10-17/` sets were read in full, without engine access.

| Check | Result |
|---|---|
| Source snapshot / inventory / platform digest | PASS on both architectures |
| Historical native artifact continuity | All 11 native artifact hashes per architecture equal the C0-ARCH-03 build record |
| Target instruction manifest, profiles, BPF, compiled observer filter/target binding, compiler/source records | PASS using actual static verifiers |
| Oracle source, interpreter and extension identities | PASS on both architectures |
| Six role configs / image IDs / environment / hooks | PASS |
| Portable config hashes vs IDs; config vs inspect; diff IDs vs inspected RootFS; actual layer hashes | PASS for all six |
| Independent in-memory layer overlay vs flattened tree | **Zero differences** for all six across file content hashes, kinds, UID/GID, modes and raw image-local link target sequences |
| Base-layer prefix | Oracle layers match the pinned Python base, followed by the source-copy layer; native images each have one layer |
| Native ELF inventories | One exact static-PIE role ELF per image |
| Oracle full ELF inventories | ARM 790 / x64 791; required closure 18 each; no unresolved required dependency |
| Optional dependency/object handling | Only unused `_tkinter` lacks Tcl/Tk; `config/python.o` remains recorded as ET_REL |
| Actual required-file permissions | PASS, including separate reviewer checks of executable PT_INTERP loaders |
| Actual root observations | Root-owned native roots 0555, oracle roots 0755; matching before/after records and content metadata |
| Command/log records | All 61 commands per set have matching log hashes and successful return codes |
| Never-started / cleanup records | Five owned inspections/removals per set; three consistent never-started inspections each and empty absence-query logs |

### New root observation path

Each role has two logged helper commands surrounding its export. I checked the exact helper script and owned container ID against the source and the root metadata against `inspection_container.root_directory` and the content record. The retained commands invoke local `podman unshare`, then mount/stat/unmount the owned filesystem; there is no container `start`, `run` or `exec` operation.

The helper was independently exercised **only with fake subprocess/stat implementations**. Success, mount failure, stat failure and unmount failure all attempt the owned unmount in `finally`; failures do not produce a successful helper return. Existing image tests additionally cover root-observation drift and strict never-started fields. No real mount/unmount or namespace helper was run by this reviewer.

Podman v4.9.3 documentation corroborates the rootless `unshare`/mount/unmount usage and includes the `--unsetenv`/`--identity-label` build options. Public runner manifests list Podman for both selected runner families. This is static validation, **not a hosted CI result**. The supplied local builder logs identify Podman 5.8.7 on linux/amd64; they are not evidence of native ARM-host CI execution.

## Tests, controls and scope limits

- **27/27 current image unit tests executed successfully**, using the frozen snapshot with `-B -W error::ResourceWarning` and temporary files only under this review directory.
- **56 independent closure/control groups completed**: 51 matched the requested behavior; the five mismatches are one inert PT_INTERP permission case and four normalized-root cases. Both real-tree in-memory PT_INTERP checks reproduce the permission issue separately.
- Both supplied full reports are **283 PASS**, source/runner-hash bound, without skips/errors/failures. **All 272 original IDs plus 11 regressions are retained.** The full suites were checked, not rerun here.
- Original reports/probes/fixtures were preserved, not amended to match the remediation.
- No repository/snapshot/image archive edits, commits, pushes, builds, pulls, creates, starts, loads, live inspections, real mounts/unmounts, ptrace, installed filters, real C0 worker/main/collector calls, live SSH or runtime calibration.
- `tarfile.extractfile()` was used for data-stream reads only; nothing from an image archive was extracted onto the host or executed.
- Container/root/cleanup observations are independently joined within existing evidence, not newly observed on an engine. Overlay comparisons do not cover timestamps/xattrs. Root metadata comes from the retained helper observations rather than a nonexistent export root header.
- Full loader ABI/symbol-version execution, lazy loading, confinement, timing and hardware calibration remain C0-ARCH-05. Registry publication remains C0-ARCH-06. No execution authority is granted.

## Reproduction and next step

```sh
R=/tmp/opencode/c0-runtime-images-remediation-2026-10-17/correctness
PYTHONDONTWRITEBYTECODE=1 python3 -B -W error::ResourceWarning "$R/probe_closures.py"
PYTHONDONTWRITEBYTECODE=1 python3 -B "$R/verify_actual.py"
PYTHONDONTWRITEBYTECODE=1 python3 -B "$R/verify_integrity.py"
```

The scripts are fixed to the new review directory for all writes; old fixtures are read-only. Machine-readable findings, closure results, preserved original-file hashes, actual artifact hashes and reference hashes are in `review.json`, `closure-probes.json`, `actual-verification.json` and `references.json`.

**Next step:** finish the required-interpreter permission check and normalize/refuse root aliases before root handling, add the paired regression controls, then freeze a new inventory for a bounded re-review. The six verified corrected image sets remain useful evidence; no rebuild or runtime execution was requested or performed by this review.
