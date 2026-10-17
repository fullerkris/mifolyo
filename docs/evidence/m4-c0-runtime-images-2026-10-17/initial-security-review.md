# MiFolyo C0-ARCH-04 — scoped read-only security review

**Decision: NEEDS_REMEDIATION** for Python static archive/content admission.

**Date:** 2026-10-17. **HEAD:** `7ef2041e2844aa34bec54da61c2e8469c1382bac`.
**Branch:** `feature/crawl-jobs-v2-c0-runtime-images`.

The review completed within the narrowed scope. Two inert fixtures produce false
content/configuration admission; a third bypasses the advertised member-count
limit. These are validator defects, not evidence that either retained image set
is malicious, that runtime authorization was bypassed, or that a live engine was
compromised.

## Scope and integrity

Reviewed the changed Python logic in `image_artifacts.py`, `image_build.py`, and
`build_oracle.py`, plus the image CLI, offline-test runner, and CI wrapper.
Inspected retained JSON/archive data read-only. Did not review or exercise the
unchanged native C implementation in this scoped review.

Inventory SHA-256:
`f789846f36fd23c21cad7b7ef461ec1dbb5521e0ad6d8948ae5cace2fde9dec4`.
All **54 package + 3 auxiliary files** matched in both the repository and frozen
snapshot before and after the scoped tests. A subsequent checksum-only comparison
also matched **264 retained evidence/source/artifact files** against the initial
verification. Documentation outside the bound inventory was not treated as drift.

During the narrowed review: no engine command, network request, container/image
mutation, native execution, worker main/collector, tracing, filter installation,
deployment, or repository edit occurred. Test command responses were mocked.
Archive contents were never extracted onto the host. One pathname check explicitly
created benign text files and symlinks inside the private review directory; it
did not execute their contents. Earlier preliminary external documentation lookups
are not relied on for this scoped verdict or claimed as live engine verification.

All reports, probes, and generated fixtures are under:
`/tmp/opencode/c0-runtime-images-2026-10-17/security/`.

## Findings

| Stable ID | Severity | Confirmed defect |
|---|---|---|
| C0-IMG-SEC-01 | Medium | Portable archive paths are deduplicated before, rather than after, pathname normalization |
| C0-IMG-SEC-02 | Medium | Link targets lose `..` before earlier symlinks are resolved, binding the wrong source bytes |
| C0-IMG-SEC-03 | Low | Repeated root-directory headers bypass the rootfs member-count/duplicate checks |

Severity concerns **static admission integrity/availability**. The prerequisite
for the first two is that malformed or substituted candidate archive bytes reach
the parser; no unauthenticated remote input route or compromise of private build
outputs was established. No Critical/High runtime-exploitation claim is made.

### C0-IMG-SEC-01 — ambiguous portable configuration identity

**Location:** `image_artifacts.py:270–283`, configuration check at `284–287`.
**Classification:** CWE-180, validation before canonicalization.

`archive_record()` keys members by raw `member.name`. It accepts both
`config.json` and `./config.json`, containing different bytes. The supplied image
identity authenticates only the first spelling, although both address the same
normalized filesystem name. The raw archive hash does bind the entire ambiguous
archive; it does **not** make its reported configuration identity unambiguous.

**Reproduction/evidence:**
`unit-run-02/inert-fixtures/portable-config-alias.tar` contains an inert valid layer,
a good config, and a later differently spelled config with a different entrypoint.
`archive_record()` succeeds with:

- Admitted config: `sha256:c042848616c4d95ef39ed85d1fd9a9aabb0398dfa920ef335bce90db0a4ef8df`.
- Config in an in-memory normalized-name, last-member-wins view:
  `sha256:ede681586c7adba8a5f4d291e814de12a2a5eae25835a01212977b9ba383305b`.
- Alternate entrypoint: `/inert-substitution-marker`.

No extraction or engine loading was performed. The verified failure is acceptance
of conflicting configuration payloads for one canonical path; downstream engine
behavior was not experimentally tested. A consumer which materializes normalized
names can use different bytes than those authenticated by this function.

**Concrete remediation:** retain the existing type/path/layer checks, but reject
duplicate **normalized** names before building the member map or reading JSON:

```python
# Inside archive_record(), before the member loop:
seen_paths = set()

# Inside the loop, after the existing member/path checks, before insertion:
key = normalized(member.name)
require(key not in seen_paths, "IMAGE_SAVE_DUPLICATE_PATH")
seen_paths.add(key)
members[member.name] = member
```

This leaves supported, noncolliding legacy layer aliases intact. The detached
prototype rejects the fixture, and its guard accepts all six retained portable
archives. Add the failing regression before changing the production code.

### C0-IMG-SEC-02 — source identity disagrees with actual pathname resolution

**Location:** `image_artifacts.py:51–61`, `90–94`, `102–116`, source comparison
at `243–245`.
**Classification:** CWE-59, incorrect link resolution.

Construction stores `normalized(base + target)`, discarding `..` before following
preceding symlink components. This changes filesystem semantics; lexical
normalization is not equivalent to resolving a pathname through links.

**Reproduction/evidence:** `unit-run-02/inert-fixtures/oracle-link-dotdot.tar`:

```text
/oracle/worker.py -> ../jump/../worker.py
/jump            -> foreign/subdir
/worker.py               = expected inert source
/foreign/worker.py       = different inert source
```

With otherwise conforming inert source and ELF parser fixtures,
`verify_content()` succeeds and declares the expected `/worker.py` hash:
`12ded7d07c41abaa6b8ee908a0c83e56ed386393ed6bdcad406628eb8d66b152`.

A normal read of the explicitly constructed benign counterpart under the private
test directory resolves instead to `foreign/worker.py`, hash:
`67e533f08cb5cb33a5186578b153063464246023f5d03ba452f20237f2f3660a`.
Thus source admission can certify bytes other than those located by the source
pathname. Neither source was executed. Returned authority/runtime flags remain
false; this does not demonstrate runtime admission bypass.

**Concrete remediation:** preserve each link's component sequence and resolve
components in filesystem order, with bounded link expansion. Do not blanket-ban
all `..` links: each retained oracle archive has 135 legitimate such links.

Replace storage of the normalized target at line 94 with:

```python
raw_target = base + target
normalized(raw_target)  # Validate bounds/characters/lexical escapes only.
row.update(kind="link", target=raw_target)  # Preserve the original sequence.
```

Replace `ImageTree.resolve()` with the following bounded, image-local resolver
and add `from collections import deque`:

```python
def resolve(self, path):
    normalized(path)  # Validate without replacing its component sequence.
    pending, resolved, links = deque(path.split("/")), [], 0
    while pending:
        part = pending.popleft()
        if part in ("", "."):
            continue
        if part == "..":
            require(bool(resolved), "IMAGE_PATH_ESCAPE")
            resolved.pop()
            continue
        prefix = "/" + "/".join([*resolved, part])
        row = self.entries.get(prefix)
        require(row is not None, "IMAGE_FILE_MISSING")
        if row["kind"] == "link":
            links += 1
            require(links <= 40, "IMAGE_LINK_DEPTH")
            combined = row["target"] + (
                "/" + "/".join(pending) if pending else "")
            require(len(combined) <= 4096, "IMAGE_PATH")
            pending, resolved = deque(combined.split("/")), []
        else:
            require(not pending or row["kind"] == "directory",
                    "IMAGE_PATH_PARENT")
            resolved.append(part)
    result = "/" + "/".join(resolved)
    require(result in self.entries, "IMAGE_FILE_MISSING")
    return result
```

The detached prototype locates the different fixture bytes correctly. It also
reproduces both retained oracle content records byte-for-byte, preserving the
legitimate parent-component links. This is a targeted proposed correction, not a
claim of complete POSIX hardlink or dynamic-loader emulation. Preserve and expand
the existing link/cycle/hardlink tests when integrating it.

### C0-IMG-SEC-03 — root headers escape the member budget

**Location:** `image_artifacts.py:72–79`.
**Classification:** CWE-400, incomplete resource accounting.

The root-directory branch continues before checking duplicate/count limits.
Repeated `.` headers are neither counted nor deduplicated. The ordinary fixture
has **20,002 members** but is accepted with **one** reported tree entry.
It is only **10,250,240 bytes**, within the raw archive-size ceiling.

**Evidence:** `unit-run-02/inert-fixtures/repeated-root-headers.tar` and the
`test_required_root_headers_count_toward_archive_bound` regression.
This establishes a count-limit bypass and underreported member count, not an
observed OOM. The raw-file ceiling still bounds input size; memory exhaustion was
not attempted. Repeated entries nevertheless consume parser work/state outside
the advertised 20,000-member budget.

**Concrete remediation:** charge every yielded member before special cases and
allow at most one root marker:

```python
root_seen = False
for member_count, member in enumerate(archive, 1):
    require(member_count <= 20000, "IMAGE_DUPLICATE_OR_COUNT")
    name = member.name.rstrip("/")
    if name in ("", "."):
        require(member.isdir() and not root_seen, "IMAGE_ROOT_DUPLICATE")
        root_seen = True
        continue
    # Existing path, metadata, size, link and content checks follow unchanged.
```

The detached guard rejects the fixture and accepts all six retained rootfs
archives. This fixes the demonstrated yielded-member gap; it is not a proof of
resource safety for every internal PAX/GNU metadata-processing path.

## Controls that passed

- **16/16** existing `test_image_artifacts.py` unit tests passed.
- **8/8** new non-regression control tests passed. Wrong config identity, changed
  layer bytes, foreign platform and duplicate JSON keys reject.
- Rootfs absolute/traversal paths, normalized duplicate names, whiteouts,
  special members, oversized file headers, host symlinks and multi-linked archive
  files reject in the exercised cases. The original target file stayed unchanged.
- Role metadata refuses unexpected native environment, users, hooks, labels and
  volumes. Returned admission summaries do not grant execution authority.
- Mocked create timeout, malformed create output and export timeout recover only
  the expected named/identified stopped object. Foreign/already-started objects
  are not exported or removed. A still-present object after removal cannot produce
  cleanup success. Exceptions still propagate.
- Mocked command timeout and oversized output cannot return command success. These
  tests do not establish live process cancellation or bounded capture memory.
- Static reading found no worker-main invocation or reader/client construction in
  `build_oracle.py`'s import-recording path. The CLI is a guarded dispatcher; the CI
  workflow has read-only contents permissions and does not itself publish images.
  Neither wrapper was run end-to-end.

The **three required-regression tests fail on the frozen source**, specifically
because `Invalid` is not raised. There are no test errors or skips. A second run
reproduced all three failures. In total, 27 distinct unit tests were exercised:
24 passed and 3 exposed the defects above. The existing full-suite reports state
272 passes; that entire suite was **not** rerun under this narrowed scope.

## Evidence and reproduction

Primary machine-readable evidence:

- `inventory-resumed-before.json`, `inventory-after.json`.
- `retained-verification.json` (initial read-only retained-file verification).
- `retained-unchanged-after.json` (264 checksum matches after testing).
- `existing-image-unit-tests.log`.
- `unit-run-02/security-unit-results.json`, `security-unit-tests-rerun.log`.
- `remediation-prototype-results.json`, `remediation-prototype.log`.
- `test_security_review.py`, `remediation_prototype.py`, and
  `check_remediation_prototype.py`.

`review.json` records the source and evidence SHA-256 values, detailed findings,
and this Markdown report's hash. `report-sha256.txt` records both report hashes.

To reproduce without overwriting evidence, choose a fresh simple run name:

```sh
umask 077
PYTHONDONTWRITEBYTECODE=1 \
TMPDIR=/tmp/opencode/c0-runtime-images-2026-10-17/security \
C0_SECURITY_RUN=independent-repeat-01 \
python3 -B -W error::ResourceWarning \
  /tmp/opencode/c0-runtime-images-2026-10-17/security/test_security_review.py
```

Expected on the reviewed snapshot: exit 1, 11 tests, 8 passes, and the three
identified assertion failures. This command uses inert fixtures and mocked
command responses only. The detached remediation helpers are **not applied** to
repository code. Their retained-file checks are compatibility evidence, not an
independent production implementation.

## Limits and release meaning

No filesystem escape or external-file mutation was demonstrated in the scoped
tests. The demonstrated path bug is an **image-internal identity mismatch**, not
host archive extraction. The successful retained image sets still pass their
existing static checks; the findings concern missing negative guards.

Host test Python was **3.14.8**. Python 3.13.15 in the images, actual engine load
semantics, live command interruption races, daemon/backend selection, native code,
runtime/hardware/timing acceptance, registry publishing and deployment were not
validated. No exhaustive parser fuzzing, OOM exercise, or arbitrary hardlink
semantics proof was performed. C0-ARCH-05 and C0-ARCH-06 remain separate gates.

Remediate C0-IMG-SEC-01/02 and the bounded-reader gap, retain these negative tests,
and repeat scoped verification before treating static admission as ready. This
review does not grant any execution or publishing authorization.
