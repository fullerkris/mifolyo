# MiFolyo C0-ARCH-04 — independent correctness review

**Verdict: NEEDS_REMEDIATION**  
Date: 2026-10-17. Scope: the frozen C0-ARCH-04 implementation and both pass5 runtime-image sets, not C0-ARCH-05 admission/calibration or C0-ARCH-06 publication.

The **six actual Podman-built images pass the read-only artifact replay**. Their portable layers reconstruct exactly the retained flattened filesystem inventories, including file hashes, ownership, modes and link targets. Native artifacts remain byte-identical to C0-ARCH-03. The stopped-container identity checks, explicit non-executable inspection entrypoint, source binding and refusal to remove foreign/started containers are good safeguards.

However, the proposed Docker CI path has two independently reproducible correctness problems: BuildKit supplies the native PATH that this implementation forbids, and the x64 oracle-manifest test now reads unmocked host-native extensions on the ARM runner. Three additional validator gaps were reproduced with inert archives. **No mismatched payload or hash was found in the two supplied pass5 sets.**

## Integrity and provenance

- Repository: `/var/home/kfuller/Documents/Git_Projects/mifolyo`.
- Branch: `feature/crawl-jobs-v2-c0-runtime-images`.
- HEAD before/after: `7ef2041e2844aa34bec54da61c2e8469c1382bac`.
- Source inventory SHA-256: `f789846f36fd23c21cad7b7ef461ec1dbb5521e0ad6d8948ae5cace2fde9dec4`.
- **57/57 inventoried repository and snapshot files match before and after**: 54 package files and three auxiliary files. Snapshot inventory and package inventory are exact. Unrelated Git-status changes were deliberately not compared.
- Both image-set source snapshots equal the 54-file inventory, with canonical source-map SHA-256 `405beb061481a32e899b762a408326a9fe7fd01d35e2b3f30485f6873b4eb0cb`.
- All **282 evidence files** hashed by the replay were rechecked unchanged at completion. Their hashes are retained in `evidence-verification.json` and `review.json`.
- All reviewer writes are under this new `correctness/` directory. Imports used `-B`/`PYTHONDONTWRITEBYTECODE=1`; temporary fixtures used its `tmp/` subdirectory.

## Findings

### 🔴 C0ARCH04-COR-001 — Docker/BuildKit cannot satisfy the empty native environment check

**Priority:** P1 / review blocker. **Confidence:** high; upstream source plus deterministic metadata/fake-command reproducer, not an executed Docker build.

**Locations:** `tests/crawl-jobs-v2-observer/image_build.py:80-81,133-138,179-180`; `image_artifacts.py:43-46`; `.github/workflows/c0-runtime-images.yml:52-56`.

The workflow selects `--engine docker`, and `Builder.run()` enables BuildKit. BuildKit's Dockerfile frontend initializes Linux `FROM scratch` images with `Config.Env = ["PATH=..."]`. The native Dockerfile does not remove it, and only the Podman build branch has `--unsetenv PATH`. The first native image therefore fails `IMAGE_ENVIRONMENT`, even when its ELF and all other metadata are correct. Both supplied successful sets use Podman, so they do not cover this route.

**Evidence:** retained BuildKit v0.20.2 `frontend/dockerfile/dockerfile2llb/image.go:27-39`, `convert.go:684-691`, and current `image.go` all show the default PATH. Probe `buildkit-scratch-path` feeds that exact kind of metadata into the real validator and observes `IMAGE_ENVIRONMENT`; its fake Docker build command has no environment-removal operation.

**Suggestion:** consider using the already-supported Podman construction path in CI, or implement an explicitly tested Docker-compatible construction/configuration path that preserves the existing empty-native-environment contract. Simply relaxing this check is insufficient: `owned_resources.py:49` also requires an empty native environment. Add a regression that models actual BuildKit output rather than only asserting build flags.

### 🔴 C0ARCH04-COR-002 — The x64 oracle-manifest unit test is host-architecture dependent

**Priority:** P1 / review blocker. **Confidence:** high; source-level cause reproduced with inert ARM extension metadata.

**Locations:** `tests/crawl-jobs-v2-observer/test_x64_runtime.py:181-194`; `build_oracle.py:41-45`; `.github/workflows/c0-runtime-images.yml:35-46`.

The test always requests `x86_64`, substitutes an x64 interpreter header and mocks `os.uname()`. Version 3 of the manifest builder also reads every loaded `ExtensionFileLoader` module, but the test does not replace that inventory. On a native ARM runner, those actual extension ELFs are AArch64, so the test raises `ORACLE_BUILD_ARCHITECTURE` before producing its manifest. The offline-control step consequently prevents the ARM image job from reaching the build.

**Reproducer:** `arm-ci-x64-manifest-fixture` runs the original test successfully on this x64 host, then reruns it with one `sys.modules` entry carrying an `ExtensionFileLoader` spec for an inert ARM ELF. The file is **never loaded or executed**. The original test fails with `ORACLE_BUILD_ARCHITECTURE`. Native ARM would supply real ARM extension files without that injection.

**Suggestion:** isolate the complete interpreter-and-extension fixture, or make the positive manifest test explicitly host-native while keeping separate synthetic x64/ARM validation tests. Avoid dropping or skipping this retained test on ARM.

### 🟡 C0ARCH04-COR-003 — Portable-archive duplicate detection uses raw, not normalized, paths

**Priority:** P2 / should fix. **Actual pass5 sets:** no normalized collisions found.

**Location:** `tests/crawl-jobs-v2-observer/image_artifacts.py:269-283`.

`archive_record()` rejects two members named `config.json`, but accepts `config.json` and `./config.json` with different contents. It similarly accepts `layer.tar` and `./layer.tar`. The manifest lookup and hashing use the first exact name; a filesystem consumer normalizes these spellings to the same destination. Thus the accepted record is not a closed proof of the config/layer bytes a portable-archive consumer will observe. A consumer might reject the ambiguity or consume the replacement; neither behavior establishes the identity just certified here.

**Reproducers:** `portable-normalized-config-duplicate` and `portable-normalized-layer-duplicate` are accepted, despite conflicting normalized content hashes. The exact-name duplicate control is correctly refused. No extraction or image load was used to demonstrate the ambiguity.

**Suggestion:** normalize and validate all archive member names before duplicate detection, or require canonical names and reject aliases. Apply the same policy to manifest references and the narrowly allowed legacy layer aliases. `ImageTree` already performs normalized duplicate detection for flattened filesystems.

### 🟡 C0ARCH04-COR-004 — File checks do not establish accessibility for UID/GID 999

**Priority:** P2 / should fix. **Actual pass5 sets:** declared files and required dependency paths pass the reviewer's additional permission checks.

**Locations:** `tests/crawl-jobs-v2-observer/image_artifacts.py:102-123,219,233,244-252`.

`file(executable=True)` accepts any execute bit, while also requiring the file to be owned by `0:0`. A root-owned mode-0700 or mode-0750 `/target` or `/observer` is therefore certified even though configured UID/GID `999:999` cannot execute it. Read access is not required for oracle source/dependency files, and directory search access is not checked.

**Reproducers:** both native roles pass with mode 0700/0750 and return the same payload digest as mode 0755. Oracle verification also accepts mode-0600 `worker.py`, a mode-0700 `/oracle` directory and a mode-0600 required extension. Mode-0600 native executables are correctly refused because they have no execute bit at all.

**Suggestion:** consider checking required read/execute permissions for the configured identity and search permissions on every traversed directory, including the root. This is statically knowable metadata, not C0-ARCH-05 runtime admission.

### 🟡 C0ARCH04-COR-005 — Required dependency closure does not preserve loader search semantics

**Priority:** P2 / should fix. **Actual pass5 sets:** neither reproduced corner case occurs in the required graph; each oracle has 18 required ELF nodes and no unresolved required edge.

**Locations:** `tests/crawl-jobs-v2-observer/image_artifacts.py:174-177,188-206`.

Two independent cases were reproduced:

1. `DT_RPATH` and `DT_RUNPATH` are merged into one ordered string list. An inert ELF with RPATH `/old-rpath` and RUNPATH `/new-runpath` is reported to have a complete required closure when its only dependency exists solely in `/old-rpath`. GNU `ld.so` ignores RPATH when RUNPATH is present; that dependency is actually missing from the applicable search. See the retained `ld.so(8)` reference.
2. A RUNPATH directory that is itself a symlink is missed: `candidate in tree.entries` is checked before resolving its directory components. `/alias/libc0fixtureonly.so` correctly resolves through `ImageTree.resolve()` to an existing `/real/libc0fixtureonly.so`, but the closure validator refuses it with `IMAGE_LIBRARY_MISSING`.

**Reproducers:** `rpath-runpath-false-positive-closure` and `runpath-symlink-directory-false-refusal`.

**Suggestion:** retain the dynamic-tag distinction and implement the supported search precedence, or explicitly refuse unsupported combinations. Resolve image-local candidate paths before deciding they are absent; do not treat malformed links as ordinary misses. A deliberately restricted pinned-base model is acceptable, but it should not claim a complete required closure for unsupported search semantics.

## Checks actually performed

| Check | Result |
|---|---|
| Current/snapshot source bytes, before and after | 57/57 match; exact inventories |
| Source snapshots within both image sets | 54/54 each match frozen package |
| Native artifacts vs historical C0-ARCH-03 build records | All 11 artifacts identical on each architecture |
| ARM oracle interpreter/source vs prior manifest | Identical; retained base inspect also names the old Python index |
| Canonical target manifests, profiles and BPF; compiled observer filter/target hash; toolchain/source records | Real static verification functions pass on both sets |
| Oracle v3 source/interpreter/extension identities | Both pass; 11 recorded extensions per architecture |
| Role metadata and image identity | All six pass with expected OS/architecture, UID/GID, entrypoint, command, environment and absent hooks |
| Portable config SHA vs image ID; config vs inspect; diff IDs vs inspect; layer hashes | All six pass |
| Portable layer overlay vs flattened rootfs | **Zero differences** across all six inventories: file hashes, kinds, owners, modes and normalized link targets |
| Python base layer prefix | Both oracle archives retain the pinned base's layers followed by the source-copy layer |
| Native ELF inventory | One exact static-PIE role ELF per image |
| Oracle full ELF inventory | ARM: 790; x64: 791. Required closure: 18 each |
| Optional unresolved edges | Only `_tkinter` → `libtk8.6.so`, `libtcl8.6.so`; not required. `config/python.o` is recorded as ET_REL |
| Actual required-path accessibility | Additional read/execute/search checks pass for the supplied sets |
| Existing command/log/cleanup joins | All 53 command records per set match log hashes; 5 stopped inspections/removals per set, 3 never-started inspections per container and empty absence-query logs |
| Lifecycle adversarial controls | 8 fake-engine failure scenarios fail closed; successful owned cleanup or refusal to remove unproven/foreign/started resources as appropriate |
| Existing new image tests executed independently | **16/16 pass**, `-W error::ResourceWarning`, frozen snapshot |
| Independent adversarial probe groups | 20 completed: 11 finding reproductions, 9 expected-refusal/lifecycle controls |
| Supplied complete test reports | Both 272 PASS reports are source/hash-bound; all 256 historical IDs retained; no skips, expected failures or missing starts/passes |
| CI static review | Separate native runner matrix and pinned action references checked; all three pinned `action.yml` URLs resolve. Blockers above remain |

The 272-test suites were **not rerun in this review**; their supplied JSON/logs and continuity were verified. The 16 image tests and the targeted original manifest test are the existing tests actually executed here. All new lifecycle engines were fake objects. No C0 worker main, Linux collector or live SSH exchange was invoked by the reviewer.

## Reproduction and retained evidence

From any directory, using the retained approved review directory:

```sh
R=/tmp/opencode/c0-runtime-images-2026-10-17/correctness
PYTHONDONTWRITEBYTECODE=1 python3 -B "$R/verify_evidence.py"
TMPDIR="$R/tmp" PYTHONDONTWRITEBYTECODE=1 python3 -B -W error::ResourceWarning "$R/adversarial_probes.py"
PYTHONDONTWRITEBYTECODE=1 python3 -B "$R/verify_integrity.py"
```

These scripts only read source/actual image evidence and write reviewer outputs or inert fixtures under `correctness/`. `adversarial-results.json` retains the fake engine commands and exact accepted/refused results; `inert-fixtures/` retains the archive reproducers. `references.json` records upstream URLs, response hashes and the versioned BuildKit source. `review.json` contains the full evidence hash map and machine-readable findings.

Two early verifier logs contain reviewer assertion failures caused by comparing Podman's unprefixed IDs to prefixed record IDs. Those assertions were corrected to normalize IDs; the retained final replay is `evidence-verification-final-pass2.log`. They are not implementation findings.

## Limits and next steps

- No repository/snapshot edits, commits or pushes; no image builds, pulls, creates, starts, loads, live image inspection or live container cleanup were performed during this review.
- No ptrace, installed filter, C0 worker/main/collector execution or live SSH. `tarfile.extractfile()` was used only for in-memory archive reads, never filesystem extraction.
- Overlay comparisons cover path kinds, file-content hashes, UID/GID, modes and normalized link targets, not timestamps or extended attributes. The flattened archives have no explicit root-directory member; root-directory metadata was not independently established by the added path-permission checks.
- Never-started/removed container facts are independently joined **within the retained evidence**, not freshly observed on an engine.
- Docker behavior was checked against upstream BuildKit source and modeled metadata. ARM-host test failure was reproduced using inert extension metadata. Neither a Docker build nor hosted CI was run.
- Full loader execution, symbol-version compatibility, lazy `dlopen`, runtime confinement, hardware behavior and calibration remain C0-ARCH-05 concerns. No registry parent-index query or C0-ARCH-06 publication was performed.

**Recommended next step:** preserve the verified pass5 evidence, remedy COR-001/COR-002 and add regression controls for COR-003–005. Then refresh the frozen inventory and repeat the bounded review. The existing artifact joins provide a solid base; this verdict is not permission to execute or publish C0 actors.
