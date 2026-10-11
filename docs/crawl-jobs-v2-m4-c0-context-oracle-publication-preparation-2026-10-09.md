# C0 context/oracle scoped publication preparation — 2026-10-09

**Prepared for an owner publication decision; publication is pending.** The
candidate remains local on `feature/crawl-jobs-v2-c0-context-oracle`, based on
`2ff03afa8950500de36762bdc5d41de8f2736030`. This step prepares the reviewed
source/evidence for publication and exact-revision CI.

## Prepared scope

The candidate contains **53 paths**: 14 modified tracked files and 39 additions.

| Group | Paths | Content |
|---|---:|---|
| Context/oracle package delta | 27 | 13 modified package files and 14 additions; all 40 package files match the independently reviewed inventory |
| Implementation/review documentation and evidence | 20 | Existing plan/checkpoints and retained initial/final review records |
| Publication-preparation checkpoint/evidence | 6 | This report, evidence index/manifest and three exact verification exports |

All 47 paths from the review handoff are carried forward. Four current status
documents advance to publication readiness; the other 43 carry-forward paths
are byte-identical. The entire reviewed package, original evidence JSON, CI
workflows/runner, normative protocol, Go security changes and native artifacts
retain their identities. There is no additional source remediation or new review
verdict in this preparation step.

The private publication handoff binds the complete path/hash list, a read-only
candidate copy and the prospective Git tree produced using a **separate private
index**. The actual worktree index remains empty. Tree and final-scope hashes live
outside the candidate they bind, avoiding circular self-hashes.

## Verification

| Check | Result |
|---|---|
| Current main | Still `2ff03af`, tree `a1fbc6f4ae3d30b149ef6625be789328a2596e14` |
| Main Required Checks / Unit Tests | Runs `37949378598` / `37949378487` remain successful, attempt 1 |
| Branch protection | All 14 expected strict contexts; admin enforcement and linear history; no force-push/deletion |
| Fresh C0 publication check | **161 passed**; identical reviewed ID inventory and all 40 source hashes; zero skips/errors/expected failures or ResourceWarnings |
| Fresh script suite / digest vectors | **31 passed / PASS** |
| Retained artifact joins | All nine native inputs, eleven native artifacts and oracle manifest match; ELF/manifest, compiled BPF and OCI-profile joins verified |
| Source review | Both final GO_FOR_CONTINUED_IMPLEMENTATION verdicts remain bound to unchanged source |

The existing repository-root Python 3.13 C0 CI step discovers the expanded suite,
has a five-minute timeout, treats ResourceWarnings as errors, and retains its
source-bound report with `if-no-files-found: error`. No workflow changes are
needed. The main report still describes **113 tests**; the candidate's **161-test**
hosted report must be verified after an authorized publication.

The [evidence index](evidence/m4-c0-context-oracle-publication-preparation-2026-10-09/README.md)
binds the fresh local results and CI-readiness record. The final private handoff
also checks exact exports, relative links, the frozen scope and a pinned
zero-suppression secret scan. Existing failures/reviews remain preserved.

## Version bindings

| Identity | SHA-256 |
|---|---|
| Reviewed 40-file source inventory | `99d9dd6f2c475cd83960c66028016d7d508acadd3563f2e440c3bf6ff012c4c3` |
| Combined independent review verdicts | `cf04c87324bf8a02e37eb2e3099443c49ee0c25cf545d85686f60c1ef33e2ad1` |
| Prior review/documentation handoff | `ad8e869402aef9d5a452943dad3cf86dad2633ae699712d5032157edc77243b8` |
| Preparation intake | `f94c842afe81bd62f55fe076ead95d222f1e3a2e079b45df58796c5afcebafc2` |
| Fresh prerequisite verification | `b9f6b6f59341f655f2f602df1c6d2657e1d82a24ff38b06e49ee3b25757b6e00` |
| CI-readiness record | `61eb7ddbc297d3cb9b36dbf08e77a930710b97e18b7ef3b216b39409711efe85` |
| Main CI gate | `8f2afe16f2482c79ab6a8c5776e7140beb38c82696d4e5d4a2f943f0d9a18213` |

## Publication handoff

The next decision is authorization to commit this exact candidate, push the
feature branch and open a PR against main. A proposed title/body and explicit
path list are retained privately. Before publication, recheck current main,
candidate bytes and the real index; reconcile any drift rather than silently
expanding the scope. After publication, verify the exact PR head/tested revision,
tree, all protected contexts/jobs, 161-test C0 artifact and existing retained
image/race/integration/audit evidence. Merge is a separate owner decision.

This preparation performs no commit, push, PR creation, runtime deployment,
native/oracle sandbox execution, tracing or new Redis experiment. Trusted
Linux-source provisioning/transport, exact runtime-image/OCI admission, full
trial/timing/confinement and cleanup evidence, and separately authorized native
calibration remain later work. Full M4 remains open.

Existing project status and the six current MiFolyo vault notes are updated through
MCPVault; imported documentation stays a dated snapshot. See the
[source-review checkpoint](crawl-jobs-v2-m4-c0-context-oracle-review-2026-10-09.md)
and [current plan](crawl-jobs-v2-plan.md).

Private preparation records and final handoff:
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-c0-context-oracle-publication-preparation-2026-10-09/`.
