# C0-ARCH-04 — constructor-scan correctness confirmation

**Scoped verdict: NEEDS_REMEDIATION**

**COR-001–006 remain closed.** The intended direct-hardlink fix works, including forward references, and all six retained images still pass the completion validator with byte-identical content records. However, the new constructor scan does not necessarily inspect the hardlink row it is iterating: a symlink in the alias's own parent path can redirect resolution to another entry. This is **new finding COR-007**, confined to the requested all-hardlink enforcement. No defect was found in the supplied image sets themselves.

## Source and preservation

- HEAD: `7ef2041e2844aa34bec54da61c2e8469c1382bac`; branch `feature/crawl-jobs-v2-c0-runtime-images`.
- Completion inventory SHA-256: `2afe7326f090a3b8ba3ef882a8b3fa89d3e4dd73a9c8c074288d541364487043`.
- Completion artifact-revalidation SHA-256: `55a7c9f9738d942a745ab47626afe9ef157998039a2d2e8779bee24f992aa3fd`.
- Compared directly with the prior GO snapshot, only `image_artifacts.py` and `test_image_review.py` differ: the five-line post-parse scan and one regression method shown in the supplied delta.
- **58/58 repository/snapshot file pairs match before and after.** Package/snapshot inventories are exact.
- **All 1,253 protected prior files remain unchanged**, including all previous review roots, snapshots, pass5/reused image archives and durable retained artifacts. Prior GO report JSON remains `c245393c64a69cdc782b6a1a810c8d575b3aaf37c6f66a7b5bfe657fc5aef0fc`.
- All new writes are under `/tmp/opencode/c0-runtime-images-completion-2026-10-17/correctness/`. Original reviews/probes were neither edited nor executed.

## 🟡 C0ARCH04-COR-007 — resolving a hardlink's name can bypass its own row

**Priority:** P2 correctness issue; prevents confirming that the new constructor guard validates every declared hardlink. **Confidence:** high, reproduced on both ABIs. **Actual retained images affected:** none found.

**Location:** `tests/crawl-jobs-v2-observer/image_artifacts.py:127-129`, interacting with `_resolve()` at lines 140–155.

The constructor iterates `(name, row)` for each stored hardlink but validates it with `self.resolve(name)`. `_resolve()` follows directory symlinks while locating that name. It can therefore reach a different regular-file entry **without ever visiting the iterated hardlink row**, collecting its metadata or examining its declared target.

### Minimal inert example

All examples have a legitimate root-directory header, owned by `0:0`, mode 0755:

```text
target       regular inert ELF, mode 0755, uid/gid 0:0
dir          symlink -> .
dir/target   hardlink -> target, mode 0700, uid/gid 0:0
```

The scan calls `resolve('/dir/target')`. Resolving `/dir` redirects to `/`; the next component selects the ordinary `/target` row. Thus `hard_metadata` stays empty and the inconsistent `/dir/target` row is never validated.

**Observed:** both `ImageTree(...)` construction and full native `verify_content(...)` succeed. Replacing the unused alias's UID or GID also succeeds. More decisively, changing its declared hardlink target to the nonexistent `absent` still succeeds: that declared target is never examined at all. Every accepted bad fixture returns the same payload digest as its valid counterpart.

This was reproduced with four variants on **both AArch64 and x86-64**, without extraction or execution. The equivalent hardlink under a real directory correctly refuses metadata mismatches, demonstrating that the failure is the parent-symlink redirection—not missing root metadata, a bad ELF fixture or a general failure to validate hardlinks.

The archive namespace in this example is contradictory. Even if entries beneath symlink parents are intentionally unsupported, they should be refused rather than silently validated as a different entry. This finding does not depend on claiming how an external tar extractor would handle the contradiction.

**Suggestion:** consider validating each iterated hardlink row and its declared target explicitly, without substituting a different row through resolution of its own pathname. A conservative alternative is to require stored hardlink-member ancestry to consist of real directory rows before resolving the alias, rejecting shadowed members. Preserve the existing no-link-in-hardlink-target and metadata checks, and the post-parse timing that permits forward references. Add the parent-symlink mismatch and missing-target cases alongside the direct/real-directory positives.

**Reproducers:** `probes.py`; retained fixtures `inert-fixtures/{aarch64,x86_64}-shadowed-{mode,uid,gid,missing-target}.tar`; exact outcomes and resolutions in `probes.json`.

## Checks actually performed

| Check | Result |
|---|---|
| Independent constructor controls | 36 groups: **28 expected outcomes**, **8 false accepts** described above |
| Direct regular-file hardlinks | Matching metadata passes; mode/UID/GID mismatches refuse at construction |
| Forward references | Valid aliases preceding targets pass on both ABIs |
| Unused malformed targets | Missing targets, directory targets, symlink targets and hardlink chains refuse when the stored alias is actually reached |
| Real-directory ancestry | Valid nested alias passes; mismatched nested alias refuses |
| Current image tests | **31/31 PASS**, frozen snapshot, `-B -W error::ResourceWarning` |
| Supplied full local test reports | **287 PASS** under both umasks; all **286 prior IDs** retained; one new method; no skips/errors/failures |
| Construction continuity | All **21 construction inputs**, driver and auxiliary files unchanged; only the two host validator/test files differ |
| Six actual metadata/content records | PASS with completion code and retained root observations; every content file is **byte-identical** |
| Portable archives | Config hashes, configs versus inspection, RootFS diff IDs and layer hashes pass; archives equal the prior GO evidence |
| Existing root/cleanup logs | 61 hashed command records per set; matching before/after roots; five distinct never-started inspections/removals per set with absence proofs |
| Actual hardlink ancestry | Two hardlinks per oracle, none in native images; all four have real directory ancestors and matching target metadata, independently checked |

The full 287-test suites were **verified, not rerun**. The 31 image tests and 36 independent constructor groups are the tests executed during this confirmation. The previous independent portable-layer overlay result is retained by unchanged archive hashes; it was not recomputed in this small-delta pass.

## Prior closures and boundaries

COR-001/002/003/005 implementation and construction paths are unchanged. COR-004's interpreter execute-intent checks and COR-006's root handling are also unchanged; their current regression tests pass. **No prior correctness closure is reopened.** The new refusal concerns only completeness of the added all-hardlink constructor scan.

No engine command or mutation, mounted-root helper execution, image build/pull/load/start, real C0 worker/main/collector execution, tracing, filter installation, deployment or runtime calibration occurred. Tests used inert archives and fake engines only. Internal archive members were read as streams, never extracted onto the host. Historical image/root/cleanup evidence was read, not freshly observed on an engine. Native hosted CI and native/runtime acceptance remain unclaimed and separately gated.

## Reproduction and next step

```sh
R=/tmp/opencode/c0-runtime-images-completion-2026-10-17/correctness
PYTHONDONTWRITEBYTECODE=1 python3 -B -W error::ResourceWarning "$R/probes.py"
PYTHONDONTWRITEBYTECODE=1 python3 -B -W error::ResourceWarning "$R/replay.py"
PYTHONDONTWRITEBYTECODE=1 python3 -B "$R/integrity.py" after
```

The intended fix is sound for canonical direct aliases; finish the scan's binding to the actual stored row, retain the valid forward-reference behavior, and recheck this bounded case. The actual image artifacts remain useful, unchanged evidence; this report requests no rebuild or runtime operation.
