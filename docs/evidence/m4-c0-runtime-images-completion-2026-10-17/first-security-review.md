# C0-ARCH-04 bounded SEC-06 closure review

**Scoped verdict: NEEDS_REMEDIATION. SEC-06 is partially remediated, not closed.**

**Date:** 2026-10-17. **HEAD:** `7ef2041e2844aa34bec54da61c2e8469c1382bac`.

The original unused-alias reproduction now rejects, including UID/GID/mode
mismatches and forward targets. The changed constructor nevertheless has a
record-binding gap: resolving a hardlink's name can follow a symlinked parent and
validate a different entry. A bounded constructor probe reproduces false admission
through that path. This is a remaining variant of SEC-06, not a new finding ID.

## Scope and preservation

Reviewed the changed constructor and added regression, retested SEC-01..05 and the
loader-execute controls, and revalidated retained artifacts as data. No networking,
engine command, helper execution, container/image loading or execution, native C
review/execution, worker main/collector execution, system-policy change, deployment,
repository edit, or frozen-source edit occurred. No tar extraction was performed.

All new writes are under:
`/tmp/opencode/c0-runtime-images-completion-2026-10-17/security/`.

Before/after checks matched:

- **55 package + 3 auxiliary files** in both repository and completion snapshot.
- Original review trees: **79 initial, 68 remediation, 79 final entries**, including
  reports, probes, fixtures, link targets, and directory modes.
- Both unchanged image-set trees: **150 entries each**.
- Completion-root continuity record and retained test JSON/logs.

Inventory SHA-256:
`2afe7326f090a3b8ba3ef882a8b3fa89d3e4dd73a9c8c074288d541364487043`.

Relative to the last reviewed 83288b8f snapshot, only the validator and image-review
test file differ. The validator delta was checked byte-for-byte: it is exactly the
new commented constructor pass, with the existing resolver and traversal-time
checks unchanged. Documentation outside the inventory was not treated as drift.

## What closes successfully

| Check | Independent result |
|---|---|
| Original SEC-06 archive | Constructor now raises `IMAGE_HARDLINK_METADATA`; original matching positive keeps its exact prior content hash. |
| UID/GID/mode matrix | All six mismatching cases reject in both target-before-alias and alias-before-target order; matching metadata succeeds in both orders. |
| Forward directories/targets | An alias preceding both its target directory and regular target succeeds after the full map is read; resolution does not mutate the entry map. |
| Other unused hardlinks | Dangling targets, hardlink cycles/chains, and directory targets reject during construction. |
| SEC-01 | Portable normalized-path collision rejects; matching config archive succeeds. |
| SEC-02 | Original symlink/`..` case rejects the wrong source; the actual located source bytes pass. |
| SEC-03 | Repeated roots reject; exactly 20,000 headers pass, and 20,001 reject. |
| SEC-04 | Hardlink to a symlink rejects; direct hardlink and symbolic-source positives pass. |
| SEC-05 | Conflicting/duplicate root aliases reject; root positives pass with and without separate observations. |
| Loader execute / COR004 | Non-executable interpreter rejects in both visit orders; executable interpreter and read-only ordinary library pass; reserved `PT_INTERP` dependency name rejects. |

Rootless old fixtures used an explicit valid fixture observation (UID/GID 0,
0755), so missing root evidence did not mask any closure check.

## SEC-06 remains open: the constructor can check a different entry

**Severity:** Medium — static admission integrity.
**Classification:** CWE-20 / CWE-59.
**Location:** `image_artifacts.py:125–129`, with path redirection at `131–163`.

The global loop iterates every hardlink **record**, but passes only its pathname
to `self.resolve(name)`. If a parent is a symlink, resolution can bypass the record
being iterated. Its declared target and metadata then never enter the resolver's
hardlink checks. The prior proposed global pass was therefore insufficient for
this name-shadowing case.

### Inert proof

With a valid explicit root observation, the fixture contains:

```text
/target              regular inert ELF parser fixture, mode 0755
/etc                 directory
/etc/hosts           ordinary inert text (allowed generated-file name)
/alias-parent        symlink -> etc
/alias-parent/hosts  hardlink -> target, mode 0700
```

The constructor reaches the last row but resolves its **name** to `/etc/hosts`,
not the hardlink's declared `/target`. It never visits the hardlink row during that
lookup. `ImageTree()` and whole `verify_content()` admission both succeed despite
the incompatible mode on the hardlink to the required inode.

Evidence in `repeat-02/independent-tests.json`:

- Raw hardlink entry: `/alias-parent/hosts`; declared target: `/target`.
- Name lookup actually checked: `/etc/hosts`.
- Required target mode: **0755**; unchecked hardlink mode: **0700**.
- Content record is **byte-identical** to the corresponding archive with matching
  0755 hardlink metadata; SHA-256:
  `1eb79db144f665aa85b730103f62364497af838d0903ac63622cdce4807ba1f2`.
- The same conflicting hardlink at an unshadowed name correctly raises
  `IMAGE_HARDLINK_METADATA`.
- The equivalent matching hardlink beneath a literal directory is a passing
  positive control.

Negative fixture: `repeat-02/fixtures/parent-redirect-conflict.tar`, SHA-256:
`bff3872ac94c209349e1fcb237d4e93bed9f774e682282309009da0cc0c6e600`.

This proves incomplete validation of the supplied archive map, not exploitation
of a live image loader. It requires malformed/substituted candidate archive data
to reach admission; no remote input route or private-output compromise was
established. No host escape or out-of-directory effect was demonstrated, and the
returned runtime/authorization flags remain false. The actual retained images
are not alleged to contain this fixture pattern.

### Concrete targeted remediation

For the closed flattened-image model, reject a hardlink member whose parent path
redirects elsewhere. This ensures resolving the member name actually encounters
the record being checked:

```python
for name, row in self.entries.items():
    if row.get("hardlink"):
        parent = str(PurePosixPath(name).parent)
        require(self.resolve(parent) == parent, "IMAGE_HARDLINK_PARENT_ALIAS")
        self.resolve(name)
```

`PurePosixPath` is already imported. This is an image-local check: no host lookup,
extraction or additional engine operation. Alternatively, validate the exact
record by identity rather than following its potentially redirected name.

`candidate-fix.patch` and the in-memory-only `check_candidate_fix.py` verify:

- Both conflicting and matching-metadata **redirected-parent** shapes reject as
  unsupported (`IMAGE_HARDLINK_PARENT_ALIAS`).
- Matching direct aliases, aliases beneath literal directories, and forward
  directory/target cases remain accepted.
- All **six retained content records remain byte-for-byte identical**, including
  the ordinary regular-file hardlinks in both oracle images.

No candidate fix was applied to the repository or snapshot. This tested restriction
is not a claim of complete arbitrary tar/POSIX filesystem emulation.

## Test and artifact evidence

- **31/31** image-specific repository unit tests pass with real subprocess APIs
  blocked in the harness.
- **11 independent tests: 10 pass, 1 reproduces the remaining SEC-06 variant**;
  no errors/skips. A second fresh-directory run reproduces it. Total distinct unit
  tests: **42, with 41 passes and one missing-refusal failure**.
- Both supplied 022/077 reports are source-bound **287-pass** records retaining
  all previous 286 IDs plus exactly the new constructor method. The full suite was
  not independently executed under this narrow review.

Updated continuity record verified at SHA-256:
`55a7c9f9738d942a745ab47626afe9ef157998039a2d2e8779bee24f992aa3fd`.

Independent artifact checks verified the exact host-validator/test delta from
the **14ac223d… build inventory**, unchanged closed construction inputs, unchanged
historical source copies/maps, and current-code admission of all six images.
Content records, image/config IDs, portable bytes and layer checksums agree with
the retained records and supplied continuity record. Old full build source maps
were **not relabelled** as completion sources.

All retained image-set entries also match the preceding independent preservation
record. The review rehashed **122 command logs**, rejoined **12 root observations**,
and verified the unchanged **10 cleanup records** and empty recorded absence
checks. These are historical retained checks, not new live observations; no stored
command or helper was executed. The supplied producer-side `pending` review marker
was left unchanged.

Primary evidence: `inputs-before.json`, `inputs-after.json`,
`repeat-02/independent-tests.json`, `existing-image-tests.json`,
`artifact-checks.json`, `candidate-fix-results.json`, their logs and fixtures.
`review.json` records full source/evidence hashes and this report's hash;
`report-sha256.txt` records both report hashes.

To reproduce in a fresh private run directory:

```sh
umask 077
PYTHONDONTWRITEBYTECODE=1 \
TMPDIR=/tmp/opencode/c0-runtime-images-completion-2026-10-17/security \
C0_COMPLETION_REVIEW_RUN=independent-repeat-03 \
python3 -B -W error::ResourceWarning \
  /tmp/opencode/c0-runtime-images-completion-2026-10-17/security/test_completion.py
```

Expected on this snapshot: exit 1, 11 tests, only the constructor record-redirection
regression fails. All prior inputs remain read-only.

## Limits and next gate

Host tests used Python **3.14.8**. No real extraction/loading, engine/helper race
check, exhaustion exercise, exhaustive archive proof, unchanged native review,
hosted image CI, runtime/hardware/timing acceptance, publishing or deployment was
performed. Other reviews' GO decisions were not substituted for these tests.

SEC-01..05 and the original direct-name SEC-06 reproduction are corrected. SEC-06's
all-alias invariant still needs the record-binding guard before closure. Hosted CI
and C0-ARCH-05/06 remain separate, unexecuted gates; no execution authorization is
issued by this report.
