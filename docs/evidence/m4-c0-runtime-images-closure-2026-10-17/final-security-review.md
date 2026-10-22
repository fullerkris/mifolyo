# C0-ARCH-04 SEC-06 parent-record closure review

**Scoped verdict: GO_FOR_CONTINUED_IMPLEMENTATION.**

**SEC-01..06 are closed for the reviewed static-admission defects. No unresolved
blocker was found in the bounded constructor delta.** This is not runtime, hosted
CI, image-loading, publishing, or deployment acceptance.

**Date:** 2026-10-17. **HEAD:** `7ef2041e2844aa34bec54da61c2e8469c1382bac`.
**Branch:** `feature/crawl-jobs-v2-c0-runtime-images`.

## Scope and integrity

Reviewed the literal-parent constructor pass, its ordering before hardlink
validation, and the added regression. Independently repeated the prior negative
fixtures with matching positives, the loader-execute controls, and retained-image
static validation.

No networking, engine commands, helper execution, container/image loading or
execution, unchanged native C review/execution, worker main/collector execution,
system-policy changes, deployment, repository edits, or frozen-source edits were
performed. No archives were extracted. All new files are confined to:

`/tmp/opencode/c0-runtime-images-closure-2026-10-17/security/`.

Before/after verification matched all **55 package + 3 auxiliary files** in both
repository and snapshot, with the supplied HEAD/branch. The validator change from
the 2afe7326 snapshot was verified byte-for-byte as exactly the new literal-parent
pass; the existing all-hardlink pass and resolver remain unchanged. Only
`image_artifacts.py` and `test_image_review.py` differ from that snapshot.

Inventory SHA-256:
`1f0e82a5ca98f02bf36deb4292501a0b97a38ac8bf567fd170fc09c8d4890376`.

Preservation checks also matched **282 prior review-tree entries** (79 initial,
68 remediation, 79 final, 56 completion), both retained image-set trees (**150
entries each**), and the current continuity/test evidence. Prior contents, link
targets and recorded directory modes remain unchanged. Documentation outside the
inventory was not treated as source drift.

## Why the remaining SEC-06 bypass is closed

The integrated control is at `image_artifacts.py:125–134`:

```python
for name in self.entries:
    parent = str(PurePosixPath(name).parent)
    require(parent == "/" or
            self.entries.get(parent, {}).get("kind") == "directory",
            "IMAGE_MEMBER_PARENT")
for name, row in self.entries.items():
    if row.get("hardlink"):
        self.resolve(name)
```

The first pass checks **every entry**, including directory entries, against the
complete map. Each non-root parent is a literal directory and has a strictly
shorter canonical pathname. Repeating that property up the finite parent chain
reaches the root without encountering a link or missing ancestor. The entire
parent pass completes before hardlink lookup begins.

Consequently, resolving a hardlink's declared member name cannot redirect through
an ancestor and skip the record being validated. The existing target-type,
link-bearing-target, and UID/GID/mode checks then apply to that exact hardlink.
No host-path traversal or filesystem mutation is introduced by this map check.
Forward directory/target declarations work because both passes run after reading
the complete archive map.

### Exact prior reproduction

Read the original `parent-redirect-conflict.tar` unchanged, SHA-256:
`bff3872ac94c209349e1fcb237d4e93bed9f774e682282309009da0cc0c6e600`.

Its `/alias-parent/hosts` hardlink sits beneath `/alias-parent → etc`. It now raises
**`IMAGE_MEMBER_PARENT` during construction**, using valid explicit root metadata
(UID/GID 0, mode 0755). The matching-metadata version with the same redirected
parent also rejects, appropriately excluding that ambiguous flattened-map shape.

The corresponding literal-directory-parent fixture passes full content admission.
Matching direct aliases, aliases preceding their target, and a directory and target
both appearing after an alias also pass.

### Additional constructor checks

- **16 combinations** covering hardlink, regular-file, directory and symlink
  children beneath symlink, hardlink, regular-file or missing parents all reject
  with `IMAGE_MEMBER_PARENT`.
- A directory with a symlinked ancestor rejects even when its child's immediate
  parent is a directory. Both archive declaration orders were tested.
- Equivalent multi-level literal-directory maps pass in both orders.
- Three unused hardlinks with forward directories/targets pass when metadata
  matches. A mismatch introduced separately into each of the three causes
  constructor refusal, confirming validation is not limited to the first alias
  or the selected role's required-file path.
- The original unused-alias fixture still raises `IMAGE_HARDLINK_METADATA`.
  Mode/UID/GID mismatch cases reject in both target/alias declaration orders.

These checks directly address the remaining record-binding bypass. They are not
a claim of complete arbitrary tar/POSIX filesystem emulation.

## Closure matrix

| Finding/control | Result with meaningful positives |
|---|---|
| SEC-01 | Original portable path collision rejects; matching config archive passes. |
| SEC-02 | Original symlink/`..` case rejects the wrong source with `IMAGE_ORACLE_SOURCE`; expected actual source bytes pass. |
| SEC-03 | Repeated roots reject; 20,000 headers pass and 20,001 reject. |
| SEC-04 | Hardlink to symlink rejects; ordinary regular hardlink and symbolic-source cases pass. |
| SEC-05 | Root alias conflicts/duplicates reject; matching aliases pass with and without explicit root observation. |
| SEC-06 | Both original metadata and parent-record bypasses reject; direct, literal-parent, forward and matching-metadata positives pass. |
| Loader execute / COR004 | Non-executable interpreter rejects in both visit orders; executable interpreter and read-only ordinary library pass; reserved `PT_INTERP` dependency name rejects. |

Rootless old fixtures were supplied valid root observations, so missing root
evidence did not mask any closure.

## Test results

- **32/32** repository image-specific unit tests passed, with real subprocess APIs
  blocked by the test harness.
- **11/11** independent closure tests passed, with no errors or skips. A second
  fresh-directory run also passed.
- **43 distinct targeted unit tests passed.** Counts exclude the repeated run and
  do not inflate the number of tests by counting subcases separately.
- Both retained 022/077 records are source-bound **288-pass** reports retaining all
  previous 287 IDs plus exactly the added parent-invariant regression. The entire
  suite was not independently executed under this bounded review.

Host test Python was **3.14.8**. The image Python/runtime was not executed.

## Retained-image continuity and static checks

Continuity record verified at its expected SHA-256:
`a8f6aee7dee482c03b925bef48a57e2ed5eb5d46edc61ea11f67c9790b5211f7`.

Independent checks verified:

1. The exact two-file host-validator/test delta from the historical **14ac223d…**
   build inventory, unchanged closed image-construction inputs, and unchanged
   historical source copies/maps. Old maps were **not relabelled** as current code.
2. Current-code static validation of **all six actual images** against expected
   artifact/source bytes, metadata and root observations. All six canonical content
   records remain **byte-for-byte unchanged**.
3. Unchanged image/config IDs, portable archive bytes and layer checksums, matching
   both image-set records and the supplied continuity record.
4. **122 recorded commands**, log hashes, owned-object identities, never-started
   inspection states, **12 root observations**, **10 cleanup records**, and empty
   scoped absence-check logs. All retained image-set entries also match the prior
   independent preservation record.

These are read-only checks of retained evidence, not fresh live engine or cleanup
observations. The fixed root helper and stored commands were not executed. The
producer's `independent_re_review="pending"` marker was left untouched; this
separate report records the scoped independent decision.

## Evidence and reproduction

Principal evidence files:

- `inputs-before.json`, `inputs-after.json`.
- `independent-tests.json`, `repeat-02/independent-tests.json`, their logs and inert
  fixtures produced by `test_closure.py`.
- `existing-image-tests.json` and `.log`.
- `artifact-checks.json` and `.log`.

`review.json` records complete source/evidence SHA-256 values and this report's
hash. `report-sha256.txt` records both report hashes. All prior review files remain
separate and unchanged.

Reproduce the bounded independent tests with a fresh simple run name:

```sh
umask 077
PYTHONDONTWRITEBYTECODE=1 \
TMPDIR=/tmp/opencode/c0-runtime-images-closure-2026-10-17/security \
C0_CLOSURE_REVIEW_RUN=independent-repeat-03 \
python3 -B -W error::ResourceWarning \
  /tmp/opencode/c0-runtime-images-closure-2026-10-17/security/test_closure.py
```

Expected: exit 0, 11 tests passed. Earlier fixtures are read-only inputs; new
fixtures and results are confined to the new run directory.

## Scope of GO

No unresolved blocker was found in this reviewed constructor delta and its
specified static-admission regressions. No out-of-directory mutation was observed.
The decision applies to these inventory-bound bytes and supports **continued
implementation only**.

No exhaustive parser fuzzing, resource-exhaustion exercise, formal proof of all
tar/ELF/loader behavior, actual image loading, live engine/helper race test, hosted
image matrix, native runtime/hardware/timing acceptance, publishing or deployment
was performed. C0-ARCH-05/06 remain separate unexecuted gates. This review grants
neither execution nor publishing authorization; runtime acceptance remains false.
