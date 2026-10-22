# C0-ARCH-04 corrected Python image admission — read-only re-review

**Scoped verdict: NEEDS_REMEDIATION.**

**Date:** 2026-10-17. **HEAD:** `7ef2041e2844aa34bec54da61c2e8469c1382bac`.
**Branch:** `feature/crawl-jobs-v2-c0-runtime-images`.

The three original reproduction cases are corrected, with meaningful positive
controls. However, two additional ordinary archive cases still produce false
static admission. Therefore this review does **not** establish a no-new-blocker
result or grant GO for static admission.

## Scope, restrictions, and preservation

Reviewed the corrected `image_artifacts.py`, `image_build.py`, `build_oracle.py`,
CLI/CI wrappers, and image-specific unit tests. Examined the fixed root-metadata
helper as **text only**, and checked its retained command arguments/results.

No networking, real engine command, root-metadata helper execution, container/image
mutation, native C review or execution, worker main/collector execution, system
policy change, deployment, or source edit was performed. Tests used inert archive
bytes, fake ELF parser inputs, mocked command results, and benign text files.
No archive was extracted to disk. One test explicitly constructed text files and
links inside the new private test directory to verify filesystem path semantics.

Corrected inventory SHA-256:
`14ac223d10a72ff859d653c29bf721e35af320fc3dbfe823a4c8176180576670`.

Before/after checks verified:

- **55 package + 3 auxiliary files** match the inventory in both frozen snapshot
  and repository; HEAD/branch remain the supplied values.
- Both corrected retained image-set trees are unchanged: **150 entries each**.
- All **79 entries** in the original security-output tree are unchanged, including
  original reports, probes, fixture bytes, and link targets.
- Concurrent documentation outside the inventory was not treated as source drift.

All new files are confined to:
`/tmp/opencode/c0-runtime-images-remediation-2026-10-17/security/`.

## Original findings: closure evidence

| ID | Scoped closure | Negative case and matching positive |
|---|---|---|
| C0-IMG-SEC-01 | Closed for the reported defect | Original config-name collision now raises `IMAGE_SAVE_DUPLICATE_PATH`; the original valid portable archive still authenticates the expected config ID. Existing tests also preserve the permitted legacy layer alias. |
| C0-IMG-SEC-02 | Closed for the reported symlink/`..` defect | Original chain now resolves `/foreign/worker.py` and raises **`IMAGE_ORACLE_SOURCE`**, not a missing-root error. Changing only the independently expected fixture source to the actual located bytes produces successful content admission. |
| C0-IMG-SEC-03 | Closed for the reported count/repeated-`.` defect | Original repeated roots raise `IMAGE_ROOT_DUPLICATE`. Exactly **20,000** yielded members, including a single root, succeed and are counted correctly; **20,001** raise `IMAGE_DUPLICATE_OR_COUNT`. |

The old rootless fixtures were supplied a legitimate explicit fixture observation
`{"uid":0,"gid":0,"mode":493}` (0755). The count-boundary fixtures contain an
explicit root header. Missing root evidence did **not** mask these checks.
These closures are specific to the established defects, not a certification of
all link or root-path handling; see the new cases below.

## New findings

Both findings are **Medium, static admission integrity**. The prerequisite is
malformed/substituted candidate archive data reaching the parser. No remote input
route, compromise of private outputs, live exploitation, or malicious retained
image is established. Neither defect grants execution authorization by itself.

### C0-IMG-SEC-04 — hardlink to a relative symlink binds the wrong source

**Location:** `image_artifacts.py:108–113,136–149,294–297`.
**Classification:** CWE-59, incorrect link resolution.

Hardlinks are still modeled as pathname redirects. `_resolve()` follows a hardlink
to its source pathname, then follows any symlink at that pathname in the original
parent directory. A real hardlink to a symlink aliases the **symlink inode**:
relative link text is interpreted at the hardlink's new pathname instead.
The final-file check at line 149 occurs after this incorrect traversal and does
not reject the symlink-inode case.

**Inert reproduction:**

```text
/src/worker.py        = expected inert source
/src/deep/link        -> ../worker.py
/oracle/worker.py     = hardlink to src/deep/link
/worker.py            = different inert source
```

With valid root observation and otherwise matching oracle fixtures,
`verify_content()` accepts `/oracle/worker.py` as `/src/worker.py`:

- Admitted source SHA-256:
  `12ded7d07c41abaa6b8ee908a0c83e56ed386393ed6bdcad406628eb8d66b152`.
- Actual benign counterpart resolves to `/worker.py`, SHA-256:
  `9923e75e179e2dcf23a9966d58e0794e428e64eb98fe9d93ad85b44a39e660e1`.

The test used `os.link(..., follow_symlinks=False)`, confirmed the two paths share
the same symlink inode, and read only benign text. No Python fixture content was
executed. Ordinary hardlinks to regular files and ordinary symbolic source aliases
both pass matching positive controls.

Evidence: `repeat-02/fixtures/hardlink-to-symlink.tar` and
`repeat-02/independent-tests.json`, observation `C0-IMG-SEC-04`.

**Concrete conservative remediation:** for this pinned-image model, reject a link
encountered while resolving a hardlink target, rather than pretending it is a
regular-file inode alias. Insert the guard before the existing link expansion:

```python
# In ImageTree._resolve():
if row["kind"] == "link":
    require(not hard_target, "IMAGE_HARDLINK_LINK_TARGET")
    links += 1
    require(links <= 40, "IMAGE_LINK_DEPTH")
    # Continue with the existing hard_target/target processing.
```

This deliberately refuses symlink/multihop hardlink targets, including link-bearing
target prefixes. Supporting them instead requires an explicit inode model; do not
silently dereference them. Both retained oracle images use only two direct regular
file hardlinks (`perl5.36.0 → perl`, `uncompress → gunzip`), which remain accepted.

### C0-IMG-SEC-05 — normalized root aliases bypass root-evidence validation

**Location:** `image_artifacts.py:89–97,155–158,305–306`.
**Classification:** CWE-180, validation before canonicalization.

The special root handling recognizes raw names `""` and `"."` before normalization.
An ordinary directory member named `./.` subsequently normalizes to `/` but is
stored in `entries["/"]`, not joined to `self.root`. Access checks and the content
report use `self.root` and ignore this conflicting root entry.

**Inert reproduction:**

- Supply an explicit root observation with mode **0755**.
- Include directory member `./.` with mode **0700**, followed by an inert target
  parser fixture.
- `ImageTree()` and `verify_content()` succeed, reporting **0755**, despite the
  root header's conflicting **0700** metadata.
- A second fixture contains both `.` (0755) and `./.` (0700). It too is accepted,
  although both names identify the root directory.

A matching `./.` header and observation is a passing positive. The normal `.`
header already rejects the same observation mismatch, proving the result is
spelling-dependent rather than a missing-root failure. Member counting itself
remains fixed; the gap is canonical root identity, duplicate detection and metadata
joining. This does not prove that the real observed retained roots are inaccessible.

Evidence: `repeat-02/fixtures/root-alias-conflict.tar`,
`root-alias-duplicate.tar`, and observation `C0-IMG-SEC-05`.

**Concrete remediation:** route a normalized `/` through the same root predicate,
before inserting it into `entries`. Immediately after `name = normalized(name)`:

```python
if name == "/":
    require(member.isdir() and member.size == 0 and not root_seen,
            "IMAGE_ROOT_DUPLICATE")
    require(self.root is None or self.root == row, "IMAGE_ROOT_CHANGED")
    self.root, root_seen = row, True
    continue
```

This preserves existing member charging and metadata validation, handles a valid
root alias with or without an independent observation, and prevents a second root
representation from entering `entries`.

## Tests and retained-artifact checks

- **27/27** existing image-specific unit tests passed, with real subprocess APIs
  blocked by a test guard. No full 283-test suite was executed by this review.
- **10** independent tests: **8 passed, 2 failed** on the two missing-refusal
  assertions above; no errors or skips. A fresh-directory second run reproduced
  the same result. Total distinct unit tests: **37; 35 passing, 2 exposing defects**.
- Docker selection rejects before execution/output construction. Mocked malformed
  root observations reject. Mocked root-observation timeout propagates while
  cleanup remains restricted to the owned stopped object. Existing controls cover
  started/foreign objects, ambiguous creates, metadata drift and failure cleanup.
- All **six corrected runtime images** independently revalidate against retained
  content, metadata, expected artifact bytes, portable config/layer checksums and
  exact source copies. Native artifacts were compared as hashes/bytes only; the
  unchanged C implementation was not reviewed or run.
- **122 retained commands**, **12 root observations**, and **10 cleanup records**
  were checked across the two sets. Every recorded command returned zero, log
  hashes match, five inspection objects per set are joined to expected image/name
  identities, inspection states are never-started, and recorded final absence
  checks are empty. Recorded root modes are 0555 for target/observer and 0755 for
  oracle; each role's before/after observations agree with its content record.
- Both retained 022/077 reports are source-bound **283-pass** records, retaining
  all prior 272 IDs plus exactly 11. This is validation of retained reports, not
  an independent rerun of their out-of-scope tests.

The root helper is a fixed Python string using local Podman mount/stat/unmount,
with `unmount` in `finally` and checked completion. Its arguments in all 12 retained
records match the reviewed helper and owned object ID. A zero helper result is
consistent with that checked cleanup path; there is no separate newly observed
mount-absence proof. The helper and stored commands were **not executed**.

CI now selects the rootless Podman route and checks rootless status. The new hosted
matrix remains unexecuted; retained evidence is local Podman 5.8.7, not proof of
hosted CI, live cleanup races, or future engine-version behavior.

## Tested candidate fixes — not applied

`candidate-fixes.patch` contains the two guards above. `check_candidate_fixes.py`
applies the exact source substitutions **in memory only** to the pure archive
module; repository and frozen files are untouched.

Results in `candidate-fix-results.json`:

- SEC-04 fixture rejects with `IMAGE_HARDLINK_LINK_TARGET`.
- SEC-05 conflicting/duplicate fixtures reject with `IMAGE_ROOT_CHANGED` and
  `IMAGE_ROOT_DUPLICATE` respectively.
- Regular hardlink and symbolic-source positives pass.
- Valid normalized root aliases pass with and without independent root metadata.
- All six retained content records remain **byte-for-byte unchanged** under the
  candidate guards, including both real regular-file hardlinks in each oracle.

These are targeted compatibility checks, not a committed remediation or proof of
complete tar/POSIX/loader semantics.

## Evidence, hashes, and reproducibility

`review.json` includes source hashes, original-finding closure evidence, new
finding details, all principal evidence hashes, and this report's SHA-256.
`report-sha256.txt` records both report hashes.

Primary evidence: `inputs-before.json`, `inputs-after.json`,
`corrected-set-checks.json`, `existing-image-tests.json`,
`repeat-02/independent-tests.json`, their logs, the inert fixture archives, and
`candidate-fix-results.json`. The initial retained-check harness log records a
format-comparison assertion; the corrected harness normalizes Podman's bare image
ID spelling and completes successfully in `corrected-set-checks-pass.log`.

Reproduce the independent tests using a **fresh** simple run name:

```sh
umask 077
PYTHONDONTWRITEBYTECODE=1 \
TMPDIR=/tmp/opencode/c0-runtime-images-remediation-2026-10-17/security \
C0_REREVIEW_RUN=independent-repeat-03 \
python3 -B -W error::ResourceWarning \
  /tmp/opencode/c0-runtime-images-remediation-2026-10-17/security/test_rereview.py
```

Expected against this snapshot: exit 1, 10 tests, eight passes and the two named
missing-refusal failures. Original fixtures are read-only inputs; all new fixtures
and results stay below the new review directory.

## Honest limits and release meaning

No host filesystem escape or out-of-directory mutation was demonstrated. SEC-04
is an image-internal source-binding error; SEC-05 is a contradictory-root-evidence
acceptance error. The supplied corrected images continue to pass their checks.
Returned authority/runtime flags remain false, including on the bad fixtures.

Host tests used Python **3.14.8**. No live loading, helper execution, engine/runtime
race testing, exhaustive archive fuzzing, memory exhaustion, unchanged native C
review, hardware/timing acceptance, publishing, or deployment was performed.
C0-ARCH-05/06 remain separate gates.

**Required next step:** integrate and regress the two additional guards (or an
equivalently sound implementation) and repeat scoped review. The original defects
are corrected, but a GO/no-new-blocker statement would be unsupported today.
