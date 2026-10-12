# C0 pinned-SSH review remediation — 2026-10-11

**Status: remediation and independent re-review complete —
GO_FOR_CONTINUED_IMPLEMENTATION from both reviewers.**
The owner authorized the three follow-ups from the
[Podman source/artifact reviews](crawl-jobs-v2-m4-c0-podman-review-2026-10-11.md).
The branch remains `feature/crawl-jobs-v2-c0-context-provisioning` at pushed
`1e95696`; all subsequent corrections and evidence remain local and uncommitted.

**Publication authorization — 2026-10-12:** the owner requested the next scoped
publication step: commit the reviewed corrections/evidence, push the feature
branch, open a PR against main and verify its exact-revision hosted CI. The
45 reviewed package files retain inventory `2ecf2c68…`; publication does not
change the source/static-artifact review scope. Resulting commit, PR and CI
identities are recorded in the current Obsidian checkpoint/work journal.

## Findings and corrections

| Finding | Verified defect | Local correction |
|---|---|---|
| COR-SSH-002 — high, blocking | An empty selector before first registration was mistaken for observed EOF; early reaping could strand the exited leader's pipe-holding descendant | Require completed pipe setup before the early non-killing reap; incomplete setup kills the owned process group before reaping |
| COR-SSH-003 — medium | A reused provisioned source could issue a lease for a preparation with different deployment/transport identities | Retain private copies of admitted source/deployment/transport bindings; snapshot and validate each acquisition preparation before authorization or exchange |
| QA-ENV-001 — informational | `mkdir(0755)` produced 0700 under umask 077 in the simulated install fixture | Explicitly chmod that directory to 0755 inside its private temporary parent |

The earlier COR-SSH-001 selector-before-spawn correction is preserved. Only
`context_transport.py` and `test_context_provisioning.py` change within the 45-file
package. Generic host-context acquisition, production ownership/mode checks and
native/oracle build inputs are unchanged.

## Regression and full-suite verification

The retained pre-fix run reproduces both cleanup failures, deployment/transport
reuse mismatches, receipt-based rebinding and the umask fixture error; matching
source reuse is the passing control. Five new test IDs extend the suite:

- First nonblocking setup failure with an exited, unreaped leader and real
  pipe-holding descendant: group kill precedes reaping and the inherited pipe closes.
- First selector-registration failure with the same owned process topology.
- Reuse rejects changed source/deployment/transport bindings before authorization
  or I/O, for copied and in-place mutated preparations.
- Matching reuse produces fresh challenges and supports unrelated target-artifact
  changes while preserving each lease's preparation binding.
- Mutating the public provisioning receipt cannot rebind the source.

| Check | Result |
|---|---|
| Full suite, umask 022 | **202/202 PASS**, 13.293 seconds |
| Full suite, umask 077 | **202/202 PASS**, 13.223 seconds |
| Previous coverage | All 197 prior IDs preserved and passed in both runs |
| Failures/errors/skips/ResourceWarnings | None; ResourceWarnings treated as errors |
| Build-input continuity | Nine native and ten oracle source/recipe inputs unchanged |
| Retained ARM64 outputs | All 11 native outputs and oracle manifest/interpreter unchanged; prior independent static reviews remain bound to those exact bytes |

Tests use owned benign Python processes/private files and simulated context data.
No native actor, oracle worker, real collector or live SSH authentication was run.
The unchanged build inputs required no new artifact build for this delta.

## Identities and evidence

- Corrected 45-file inventory:
  `2ecf2c68b6a3293cc3b34314e23022b3f35c2eda69f9f8efa0391728804a8edf`.
- Local remediation record:
  `be92677e3a523e0659800cbd6cf59944f6f1ad23994cd9175fcc485f1308d99b`.
- Corrected transport bundle:
  `20b53849d3617f1a7f1dc2897cd2ab3fed48990b8df188317c8c136d8276869a`.
- Preserved ARM64 reconstruction verification:
  `569dfdc93881a44d18dc6b8b2d1bdfbc1a5a2453153257de0160751d86d09318`.

The [evidence index](evidence/m4-c0-ssh-remediation-2026-10-11/README.md) and
[local manifest](evidence/m4-c0-ssh-remediation-2026-10-11/local-manifest.json) bind
eight exact exports. Local-manifest identity:
`c73c5e058acdc9f4a2a46e157b58e2b9adf5aa30d8dde1cbb2c167d00d37e8bf`.
Both full-suite report files have identity
`1d8d1cd2b3cecf39bbc61652968971a40c853902c2d08df91cb42502fa6494fc`;
separate logs and the remediation record preserve the different umask runs.

Preparation packets must use the corrected transport identity. Host-source and
oracle-worker bundle identities are unchanged. The original failed/reviewed
snapshots and report verdicts retain their original bytes and recording-time scope.

Private tests, source snapshot, verification scripts and reviewer directories:
`/tmp/opencode/m4-c0-ssh-remediation-2026-10-11/`.

## Final independent re-review

Both original reviewers close COR-SSH-002/003 and QA-ENV-001, preserve COR-SSH-001,
and return **GO_FOR_CONTINUED_IMPLEMENTATION** on the exact `2ecf2c68…` inventory.
No new in-scope findings were reported. Each independently verified unchanged
source/runner hashes and native/oracle artifact continuity before/after review.

| Reviewer | Full-suite result | Focused verification | Review SHA-256 |
|---|---|---|---|
| Correctness | **202/202 PASS**, once under umask 022 | Original counterexample replays, six boundary controls, focused umask-077 fixture pass and owned-descendant absence | `39ce04e0b319406164d1c14732baa5cc3186d704fe9704676c4d78ae933d1412` |
| Security | **202/202 PASS**, once under umask 077 | Before/after cleanup and binding probes, all setup positions, caller/receipt mutation and valid-reuse controls; descriptor delta zero | `8fa8df79a42d43dcb8770aa14a83ad6beaf5deaa78ff4b59db797b321dfcace1` |

The [final gate](evidence/m4-c0-ssh-remediation-2026-10-11/verification.json) has identity
`29a2e53843d8ca60137599836e4a3f54a466b647f0412f3d6b39cfbac81fe60a`.
The [final manifest](evidence/m4-c0-ssh-remediation-2026-10-11/manifest.json) binds
45 exact local/reviewer exports, identity
`244d84a29b5d7db0c37b9a7a3c13fa338b450c6cd28674c7c835b85f2ca4b566`.
Separate execution logs retain independent provenance even though all clean
202-test JSON reports have identical source/inventory/outcome bytes. Initial
failed runs and NEEDS_REMEDIATION evidence remain unchanged.

GO covers source/static artifacts. The staging validator's private-key envelope
check remains distinct from decoded key-pair/live authentication admission, and
actual host/native runtime acceptance remains open.

## Next gates

1. Separate scoped PR/publication decision for the reviewed snapshot and its exact
   CI/merge verification.
2. Actual host/context provisioning, runtime-image admission, full trial assembly
   and native calibration; ARM64/x64 runtime delivery remains C0-ARCH-02–06 in the
   [implementation plan](crawl-jobs-v2-plan.md#dual-architecture-c0-runtime-plan--2026-10-11).
