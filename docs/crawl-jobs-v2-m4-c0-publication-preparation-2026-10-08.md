# C0 scoped publication preparation — 2026-10-08

**Status: scoped local checks pass; the publication candidate is prepared.**
The candidate is on `feature/crawl-jobs-v2-c0-observer`, based at verified main
`ab5f21a7421f3ed48cfb32481613fa5656dcb8a4`. It remains unstaged, uncommitted and
unpushed at recording time. Publication authorization and exact published-revision
CI are the next gates, followed by a separate merge decision.

## Exact scope

The [publication scope](evidence/m4-c0-publication-preparation-2026-10-08/publication-scope.json)
binds 118 source/history/integration paths and names six closeout paths: **124
candidate paths** in total, consisting of two tracked modifications and 122 new
files relative to the base.

| Group | Paths | Purpose |
|---|---:|---|
| Current C0 package | 26 | Native artifacts' sources, contracts, adapters, admission and offline tests |
| Inherited D01/D02 design dependency | 22 | Design proposals, static evidence and independent reviews linked by C0 |
| C0 historical reports/evidence | 66 | Foundation, adapters, reconciliation, admission, independent review and fixture-fix chronology |
| CI integration | 3 | Required workflow step, runner and runner regression tests |
| Current plan | 1 | Update the current baseline, C0 status and remaining gates |
| This closeout | 6 | Dated report, evidence index/manifest and three exact JSON exports |

The 114-path incoming overlay is byte-identical to the fixture-fix handoff. All
26 package files, nine native compiler inputs and eleven retained native artifacts
are unchanged by publication preparation. The original design worktree's 22 paths,
372 unrelated primary-worktree paths, and primary documentation PR #21 are preserved.
The inherited design records are included because the package and review chronology
depend on them. Large private research dumps, native binaries and build directories
remain outside this publication scope.

Earlier reports describe their own recording-time state and source/test inventory.
Their bytes and initial refusal reports remain intact; they are not retrospective
claims about the current snapshot.

## CI coverage added

`required-tests` in `.github/workflows/required-checks.yml` now has a dedicated
repository-root step using Python 3.13 and:

```sh
python -B -W error::ResourceWarning scripts/test-crawl-jobs-v2-observer.py \
  --report /absolute/output/c0-offline-tests.json
```

The runner discovers the complete C0 Python suite relative to its own repository
location. It rejects empty/duplicate discovery, incomplete execution, failures,
errors, skips (including class/setup/subtest skips), expected failures and unexpected
successes. It records the exact discovered/started/passed IDs, runner hash, package
hashes and CI revision, and rejects package changes during the run.

A pinned upload step retains `c0-offline-tests.json` even on failure and rejects a
missing report. The new test step has a five-minute ceiling. All earlier workflow
steps, triggers, permissions, jobs and fourteen protected contexts retain their
original structure; `.github/workflows/unit-tests.yml` is unchanged. Native builds,
Docker operations and actor execution are not part of this new step.

## Local validation

| Check | Result |
|---|---|
| C0 via the new runner | **113 passed**, all previous test IDs preserved; no skips or ResourceWarnings |
| Script suite | **31 passed**, including ten runner-gate regression tests |
| Independent digest vectors | **PASS** |
| Workflow structure | YAML parsing and comparison prove exactly two added steps; existing structure preserved |
| Native/source preservation | 26 package files, nine inputs and eleven artifacts match the latest handoff |

The [verification record](evidence/m4-c0-publication-preparation-2026-10-08/verification.json)
binds commands, source hashes, log digests and the local interpreter version. Local
checks used Python 3.12; hosted Python 3.13 CI for the new candidate remains pending.
`actionlint` was unavailable; no actionlint result is claimed.

An additional unchanged Redis-harness recheck was interrupted by the private
driver's **300-second** limit. Its buffered partial output was not retained, so no
partial count or new 183-test PASS is inferred. The verified main CI log records
the complete 183-test suite taking **2,496.716 seconds**, above that local budget.
All 108 reviewed cancellation/harness source files still match. The existing Redis
CI step remains intact, and its verified main evidence remains bound to `ab5f21a`.
This optional interrupted recheck is retained separately from the passing scoped
C0 checks; no suite assertion, timeout or protected check was weakened.

## Review and version bindings

| Record | SHA-256 |
|---|---|
| Current 26-file package inventory | `01c5a7318274d26d3868d40ff71205f1193de123914dc9907ffb62eb0f3693ac` |
| Independent admission-review verdicts | `af0ca377acd01f70a4c8c856313be88e7b6155607ac66c162750284b9f965422` |
| Latest fixture-fix handoff | `1486fe5eb5612653c18ebb944226320a62694789085f34e72b10227014f007ef` |
| Publication scope | `e9b5a368c73e64ac7028cdc975b32b254985db6436eec6774eb4601b6d8a3ffa` |
| Local verification | `cb1edb12de0f5606cc2e55996d42c9aa2bb39ef857d3a947bb1004243a868ab6` |
| Source-bound C0 test result | `be5850259e9b03e5bcecc1c48ded2238aca83575d54397601ac4a0dd9cef11c6` |

Both independent admission reviewers returned **GO_FOR_CONTINUED_IMPLEMENTATION**
for the 112-test snapshot after remediation. The subsequent
[fixture-only fix](crawl-jobs-v2-m4-c0-fixture-sync-2026-10-08.md) adds one test while
preserving production bytes. These existing verdicts do not constitute a new
independent review of the fixture change or the CI integration.

## Publication boundary and remaining work

The [evidence index](evidence/m4-c0-publication-preparation-2026-10-08/README.md)
binds three byte-identical exports. The private final handoff records the complete
124-path map, manifest/link validation, exact-scope secret-scan disposition and
preservation checks. Source/hash changes after this checkpoint require refreshing
the candidate bindings before publication.

At intake, remote main remains `ab5f21a`; there is no remote C0 branch or C0 PR.
[PR #21](https://github.com/fullerkris/mifolyo/pull/21) remains open at `e9e6aed`.
Recheck main and that PR before publication: both branches update the current plan,
so a later base advance requires explicit reconciliation and refreshed validation.

Next publication steps are exact-scope commit/push/PR authorization, verification
of the new revision's protected CI and retained C0 result, then a separate merge
decision. Current tests exercise offline contracts, simulated Docker and benign
Python processes. No native actor, tracing, Redis case or broader M4 experiment ran.
Trusted Linux runtime-context acquisition, the oracle/client-closure bridge, full
trial dispatch, runtime images and approval/execution assembly remain open. The
normative protocol is unchanged and full M4 is not accepted.

Private records:
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-c0-publication-preparation-2026-10-08/`.
