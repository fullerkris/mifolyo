# C0-ARCH-04 final bounded Python re-review

**Scoped decision: NEEDS_REMEDIATION.**

**Date:** 2026-10-17. **HEAD:** `7ef2041e2844aa34bec54da61c2e8469c1382bac`.

SEC-01..05 are closed for their reported defects, with matching positive cases.
COR004's interpreter-execute checks also pass. One remaining blocker exists in the
new hardlink-metadata invariant: it is enforced only when the alias is traversed,
not when an unused alias refers to a required file. A GO/no-new-blocker statement
would therefore be unsupported.

## Scope and preservation

This was a bounded re-review of the two-file Python validator/test delta and its
interaction with the unchanged archive/configuration/file/cleanup path. No new
networking, engine command, helper execution, image loading/execution, unchanged
native C review, worker main/collector execution, system policy change, or
deployment occurred. No repository or frozen source was edited. No archive was
extracted. A benign shared-inode text-file check and its permission changes were
confined to the new private fixture directory; no fixture program was executed.

Before/after verification matched all **55 package + 3 auxiliary files** in both
repository and final snapshot, with the supplied HEAD/branch. The exact delta from
the preceding inventory is only:

- `image_artifacts.py`: `61df24f2…` → `106a355c…`.
- `test_image_review.py`: `f7420296…` → `1a7f0785…`.

Final inventory SHA-256:
`83288b8fd89fcbd6453626df75b6b48dd02150fda5def59d0942d92dc6483a6b`.

The original security tree's **79 entries**, remediation security tree's **68
entries**, and both retained image-set trees (**150 entries each**) matched before
and after. Final-root continuity/test evidence also remained unchanged.
Documentation outside the bound inventory was not treated as source drift.

All new probes, fixtures and reports are under:
`/tmp/opencode/c0-runtime-images-final-2026-10-17/security/`.

## Closure and positive-control results

| Check | Result |
|---|---|
| SEC-01: normalized portable collisions | Original collision raises `IMAGE_SAVE_DUPLICATE_PATH`; original matching config archive passes. |
| SEC-02: symlink then `..` | Original wrong-source join raises `IMAGE_ORACLE_SOURCE`, using valid root metadata; the actual located source bytes pass a matching positive. |
| SEC-03: root/member accounting | Repeated roots reject; exactly 20,000 members pass and 20,001 reject with `IMAGE_DUPLICATE_OR_COUNT`. |
| SEC-04: hardlink to symlink | Original case raises `IMAGE_HARDLINK_LINK_TARGET`; direct regular hardlink and symbolic-source cases pass. Link-bearing prefixes, chained aliases, directory and dangling targets refuse. |
| SEC-05: normalized root identity | Conflicting and duplicate aliases reject with the root-specific codes; valid aliases pass with and without an explicit observation, and do not appear as `entries["/"]`. |
| Referenced hardlink metadata | UID, GID and mode mismatches reject with `IMAGE_HARDLINK_METADATA`; matching 0644/0755 aliases pass. The unused-alias gap is SEC-06 below. |
| COR004: interpreter execute intent | A 0644 `PT_INTERP` file rejects in both visit orders, including after an earlier non-executable library visit. A 0755 interpreter and an ordinary 0644 `DT_NEEDED` library pass. |
| Reserved dependency key | `DT_NEEDED="PT_INTERP"` rejects; a non-reserved similar name (`PT_INTERP.so`) remains valid parser input. |

Old fixtures without a root marker were supplied explicit valid fixture root
metadata (UID/GID 0, mode 0755). No closure relies on an unrelated missing-root
failure. The execute-intent trace explicitly records a read-only loader visit,
then a subsequent execute-required visit before visited-set deduplication.

## Remaining finding: C0-IMG-SEC-06

**Severity:** Medium — static admission integrity.
**Classification:** CWE-20, incomplete input validation.
**Location:** `image_artifacts.py:126–158` (lazy alias validation),
`237–282` (required-closure traversal), `293–298` (native content checks).

### Cause and impact

The new hardlink metadata comparison runs in `_resolve()` only when that hardlink
pathname is traversed. `verify_content()` does not traverse every archive hardlink.
Its native-file inventory restricts regular-file rows, but permits additional
link rows. Consequently an unused hardlink can carry metadata conflicting with a
required file's inode while whole-content admission still succeeds.

This is a violation of the intended inode-metadata consistency check, not a claim
of a compromised retained image or remote exploit. The prerequisite is malformed
or substituted candidate archive data reaching static admission. No private-output
compromise or unauthorized runtime execution was established.

### Reproducible inert proof

Supply valid root metadata and this small archive:

```text
target        regular inert ELF parser fixture, uid=0 gid=0 mode=0755
unused-alias  hardlink to target,             uid=0 gid=0 mode=0700
```

Observed using the final frozen code:

1. `tree.file("/unused-alias")` correctly raises `IMAGE_HARDLINK_METADATA`.
2. `verify_content(tree, "x86_64", "target", {"target": fixture}, {})` succeeds,
   because it verifies `/target` but never visits `/unused-alias`.
3. Its **entire canonical content record is byte-identical** to the positive
   archive whose two modes both equal 0755. Shared content SHA-256:
   `18359ca24129e49759f738007a3d6fecf57cb782d5c8d999b4fadf09c8eb8136`.

The negative fixture is
`repeat-02/fixtures/unused-hardlink-conflict.tar`, SHA-256:
`a8926a206e90cb0618f18632db0671600eaebb98ef6df42fcdefc6883ccce070`.
The matching positive is `repeat-02/fixtures/unused-hardlink-positive.tar`.

An explicitly constructed benign text-file counterpart confirmed that both names
share an inode and changing the alias mode to 0700 changes the target mode too.
It did not extract or load an archive. An archive consumer applying alias metadata
could therefore change the required inode's accessibility; the behavior of any
specific image loader was **not** exercised. Independently of loader behavior,
the validator accepts conflicting inode metadata that its own alias check rejects.
Both returned runtime/authorization flags remain false.

### Concrete remediation

Validate all hardlink bindings once the full archive map exists, not only those
encountered in required-file traversal. At the end of `ImageTree.__init__()`:

```python
self.total_bytes = total
for name, row in self.entries.items():
    if row.get("hardlink"):
        self.resolve(name)
```

This reuses the existing bounded resolver, link-target policy and metadata
comparison, after forward targets are available. It performs no host-path lookup
or extraction. Retain the matching unused-alias positive and the failing negative
regression when integrating the change.

`candidate-fix.patch` contains this proposed guard. An in-memory-only test in
`check_candidate_fix.py` verifies that it rejects the bad alias with
`IMAGE_HARDLINK_METADATA`, preserves matching readable/executable aliases, and
leaves **all six retained content records byte-identical**. Both ordinary hardlinks
in each real oracle image also pass. **No fix was applied to frozen/repository
sources.**

## Independent retained-artifact continuity

The supplied continuity record was verified at its expected SHA-256:
`40e1106f132e054fa7654a94bba30ff85a3567b738f057527e24bba21704b851`.
It was not merely accepted as a claim:

- Compared the complete retained source maps/copies to the **14ac223d…** inventory,
  then independently verified the precise two-file delta to the final inventory.
- Checked both closed build-input lists and unchanged recipes. Neither changed
  host-validator/test file is an image-construction input. `image_build.py`, CI,
  worker and artifact-generator inputs remain unchanged.
- Re-ran final-code content/metadata/config/layer checks against **all six actual
  images**, joining each root observation and the expected artifact/source bytes.
  All retained content records remain byte-identical; portable IDs and bytes match
  both the old image-set records and the supplied continuity record.
- Verified retained command-log hashes, object identities and never-started state:
  **122 commands, 12 root observations, 10 cleanup records** across the two sets.
  Recorded removals and empty absence checks agree. None of those commands or the
  root helper was run by this review.
- Kept the old full source maps unchanged and explicitly associated them with the
  historical validator/test hashes. They were **not relabelled as final sources**.

The continuity record's producer-side `independent_re_review="pending"` marker
remains untouched; this separate report records the independent result.

## Test evidence and limits

- **30/30** repository image-specific unit tests pass, with real subprocess APIs
  blocked by the test harness.
- **11** independent tests: **10 pass, 1 reproduces SEC-06**, no errors/skips.
  A fresh-directory rerun reproduces the defect and confirms identical positive/
  negative content records. Total distinct unit tests: **41; 40 pass, 1 fails**.
- Both retained 022/077 records are source-bound **286-pass** reports and retain all
  previous 283 IDs plus exactly three. The whole suite was not independently run;
  unchanged native and unrelated functionality remain outside scope.
- Inputs, source bytes, previous outputs, actual image files and continuity evidence
  match the before/after preservation checks.

Primary evidence: `inputs-before.json`, `inputs-after.json`,
`artifact-checks.json`, `existing-image-tests.json`,
`repeat-02/independent-tests.json`, their logs, and `candidate-fix-results.json`.
`review.json` records full source/evidence hashes and this report's hash.
`report-sha256.txt` records both report hashes.

To reproduce only the bounded inert checks, choose a fresh run name:

```sh
umask 077
PYTHONDONTWRITEBYTECODE=1 \
TMPDIR=/tmp/opencode/c0-runtime-images-final-2026-10-17/security \
C0_FINAL_REVIEW_RUN=independent-repeat-03 \
python3 -B -W error::ResourceWarning \
  /tmp/opencode/c0-runtime-images-final-2026-10-17/security/test_final.py
```

Expected on this snapshot: exit 1, 11 tests, only the unused-hardlink regression
fails. Earlier fixtures are read-only inputs; new files stay under the new run
directory.

Host tests used Python **3.14.8**. No out-of-directory mutation or host escape was
demonstrated. No exhaustive parser/POSIX proof, real extraction/loading, live
helper/engine race check, memory-exhaustion exercise, hosted matrix, native runtime,
hardware/timing acceptance, publishing or deployment was performed. C0-ARCH-05/06
remain separate gates. The supplied images still pass their checks; the remaining
blocker concerns an unhandled negative input in host-side static admission.

**Next step:** enforce all-alias validation and repeat the bounded checks. A narrow
host-validator correction is sufficient for the demonstrated defect; this review
does not authorize any execution or rebuilding/publishing of images.
