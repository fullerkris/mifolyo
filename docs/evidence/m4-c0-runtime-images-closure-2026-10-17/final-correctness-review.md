# C0-ARCH-04 — bounded COR-007 closure review

**Verdict: GO_FOR_CONTINUED_IMPLEMENTATION**  
**COR-007 is CLOSED. COR-001–006 remain closed. No blocker was found in the reviewed constructor change.** This verdict is limited to source/static-image correctness; it grants no engine, native-runtime, deployment or publication authority.

## Source binding and preservation

- HEAD: `7ef2041e2844aa34bec54da61c2e8469c1382bac`; branch `feature/crawl-jobs-v2-c0-runtime-images`.
- Closure inventory SHA-256: `1f0e82a5ca98f02bf36deb4292501a0b97a38ac8bf567fd170fc09c8d4890376`.
- Artifact-revalidation SHA-256: `a8f6aee7dee482c03b925bef48a57e2ed5eb5d46edc61ea11f67c9790b5211f7`.
- Compared with the completion snapshot, only `image_artifacts.py` and `test_image_review.py` differ: the five-line literal-parent check and its added regression method.
- **58/58 inventoried repository/snapshot file pairs match before and after**: 55 package files plus three auxiliary files; exact inventories.
- **All 1,429 protected prior files remain unchanged**, including all prior reviews/probes/snapshots and retained image artifacts. The prior COR-007 report JSON remains `12559ac96078b1f18b202debb86772480ada3330c6c17b75e220c11b7ce51990`.
- New outputs are confined to `/tmp/opencode/c0-runtime-images-closure-2026-10-17/correctness/`. No prior review script was run or edited. Unrelated Git-status changes were not compared.

## COR-007 closure

**Reviewed location:** `tests/crawl-jobs-v2-observer/image_artifacts.py:125-134`.

The new pass checks the literal parent of **every stored entry** before starting the all-hardlink pass. Each parent must be `/` or a stored directory. Since parent names have fewer canonical path components and every parent entry is itself checked, this establishes directory ancestry all the way to the root—not just one level. A symlink/file ancestor can no longer redirect `resolve(name)` away from the hardlink row under audit.

Validation still occurs **after the entire archive map is parsed**. Therefore entry order does not invalidate legitimate forward parent or target references. Normal symlink queries remain supported: the restriction concerns the ancestry of stored archive entries, not references that resolve through symlinks to real stored paths.

### Independent controls

**80/80 independent groups PASS**, using inert archives and valid root headers:

- All **eight exact prior COR-007 counterexamples**, read unchanged from the previous review, now refuse at construction with **`IMAGE_MEMBER_PARENT`**: mode, UID, GID and missing-target variants on both ABIs.
- Original direct-hardlink positives still pass before and after target entries. Original mode/UID/GID mismatches still refuse with `IMAGE_HARDLINK_METADATA`.
- Stored hardlink, regular-file, directory and symlink children refuse when their parent is a symlink, a file or missing.
- Deepest-child-first fixtures refuse when an immediate directory parent has an invalid higher ancestor. This directly tests the whole-map invariant rather than assuming that checking one parent is enough.
- Nested literal directories, aliases and targets pass in all tested before/after order combinations. Accepted aliases return the expected bytes.
- Literal `.hidden` and `..literal` directory names remain valid.
- Valid stored symlinks, queries through a directory symlink to a direct hardlink, and queries through multiple symbolic links still pass. The fix does not disable ordinary reference resolution.

No missing-root, wrong-architecture or unrelated content refusal was counted as closure. Positive native fixtures pass full `verify_content()`; the malformed-ancestry cases refuse specifically at the new parent check.

## Prior findings

| Finding | Status |
|---|---|
| COR-001 — supported builder/native environment | **CLOSED, retained** |
| COR-002 — architecture-isolated extension fixture | **CLOSED, retained** |
| COR-003 — normalized portable member duplicates | **CLOSED, retained** |
| COR-004 — configured-user/interpreter accessibility | **CLOSED, retained** |
| COR-005 — dependency search semantics | **CLOSED within the documented pinned-base model** |
| COR-006 — normalized root aliases | **CLOSED, retained** |
| COR-007 — shadowed hardlink constructor lookup | **CLOSED by this review** |

The underlying corrections for COR-001–006 are unchanged, and their current image regression tests pass. The new ancestry rule strengthens the archive map before traversal; it does not remove the prior permission, interpreter-intent, root, dependency or hardlink-target checks.

## Retained image replay and continuity

The current validator was applied to the six retained images using their **existing observed root metadata**, not a new helper invocation.

| Check | Result |
|---|---|
| Construction continuity | All **21 construction inputs**, driver, recipes, worker/profile sources and auxiliary files unchanged |
| Source maps | Old image source maps remain correctly bound to the 14ac223d construction snapshot; only the declared host validator/test delta differs |
| Actual source/native/oracle artifact joins | PASS |
| Six role metadata/config/image-ID joins | PASS |
| Portable config hashes, config versus inspection, diff IDs and actual layer hashes | PASS |
| Current content records versus retained files | **All six are byte-identical** |
| Independent ancestry check of actual maps | Every ancestor of every entry checked: 6,581 ARM oracle entries, 6,586 x64 oracle entries, and one entry in each native role image |
| Actual hardlinks | Two per oracle; literal ancestry and shared mode/UID/GID match. None in native images |
| Root observations | Root-owned 0555 native roots / 0755 oracle roots; existing before/after observations match role records |
| Existing command/cleanup proof | 61 matching log records per set; five distinct never-started inspection removals each, three state inspections each, and empty absence queries |

The image/archive hashes also match the previous GO evidence. The prior independent layer-overlay comparison is retained by unchanged bytes; it was not recomputed in this bounded closure pass. No rebuild was needed or performed by this reviewer.

## Tests and limits

- **32/32 current image unit tests PASS**, using the frozen closure snapshot, `-B -W error::ResourceWarning` and temporary files only under this new directory.
- Both supplied full source-bound runs are **288 PASS**, retain all **287 prior IDs**, and add exactly the stated ancestry regression method. No skips/errors/failures. Those complete suites were **verified, not rerun here**.
- No engine commands/mutations, builds, pulls, image loads/starts, mounts/unmounts, mounted-root helper execution, real C0 actor/worker-main/collector execution, tracing, installed filters, deployment or runtime calibration.
- Tests used inert data/private fixtures and fake engines. Archive member reads were stream reads only; nothing was extracted onto the host or executed.
- Root/container/cleanup facts are historical observations joined within retained evidence, not new live observations. Timestamps/xattrs and full loader execution remain outside this review's claims.
- **Native hosted image CI remains unexecuted. Native/runtime acceptance and C0-ARCH-05 calibration are not established. C0-ARCH-06 publication remains separately gated.**

## Reproduction and next step

```sh
R=/tmp/opencode/c0-runtime-images-closure-2026-10-17/correctness
PYTHONDONTWRITEBYTECODE=1 python3 -B -W error::ResourceWarning "$R/probes.py"
PYTHONDONTWRITEBYTECODE=1 python3 -B -W error::ResourceWarning "$R/replay.py"
PYTHONDONTWRITEBYTECODE=1 python3 -B "$R/integrity.py" after
```

`review.json` retains the stable finding closure, exact source/artifact identities, test evidence and preservation-manifest hashes. The fix addresses the reported lookup substitution while preserving valid forward references. **Continue implementation/documentation with these closures preserved; obtain separate authority and evidence for hosted/native work.**
