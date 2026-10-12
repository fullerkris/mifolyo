# C0 pinned-SSH review and cleanup correction — 2026-10-11

**Original checkpoint: local COR-SSH-001 remediation verified. Subsequent final
source/static-artifact re-review is complete on the corrected 202-test snapshot.**
Resumed the pushed `1e956962596f7bb86143d45dd62ebf863c0b64eb`
checkpoint on `feature/crawl-jobs-v2-c0-context-provisioning`, based on `64c98a7`.
The intake worktree was clean and all 45 package hashes matched the retained
[implementation inventory](evidence/m4-c0-context-provisioning-2026-10-11/source-inventory.json).
The cleanup correction and this successor evidence are local, uncommitted changes.

**Subsequent artifact reconstruction:** the owner-approved
[Podman/QEMU build and independent re-review](crawl-jobs-v2-m4-c0-podman-review-2026-10-11.md)
reproduces all 11 native ARM64 outputs and the oracle manifest/interpreter exactly.
Actual static artifact joins pass. The [subsequent remediation](crawl-jobs-v2-m4-c0-ssh-remediation-2026-10-11.md)
closes the reviewers' follow-ups with 202 tests and two scoped GO verdicts.
The earlier verification table below retains its
recording-time scope.

## Review finding and correction

This session reviewed private provisioning and bundle validation, service/owned-file
admission, SSH configuration and credential/client pins, bounded exchange and
cleanup, preparation/backend joins, and retained source/evidence bindings.

| Finding | Reproduced boundary defect | Local correction |
|---|---|---|
| COR-SSH-001 — medium | `exchange_process()` started the child before constructing its selector, outside the cleanup guard. An injected selector-allocation failure left a real benign child alive and all three pipes open | Construct the selector before spawn; recheck the deadline; guard spawn and pipe setup, closing the selector even if spawn fails and reaping/closing any successfully started child on setup failure |

The probe used an owned Python sleeper and an injected `ENFILE` error, not a live
SSH connection. The probe and regression fixture explicitly disposed of their
children. The retained red run has **one failing regression and two passing
controls**. The same three test IDs pass after correction: selector failure cannot
start a child, spawn failure closes any created selector, and pipe-setup failure
reaps the child and closes its handles.

The only package changes are `context_transport.py` and
`test_context_provisioning.py`. This session performed both review and remediation;
it does not supply separate final correctness/security reviewer verdicts.

## Verification and artifact continuity

| Check | Result |
|---|---|
| Original source/evidence intake | All 45 package hashes and five retained evidence exports verified |
| Fresh original C0 suite | 194 passes |
| Corrected C0 suite | **197 passes**; all 194 original IDs retained, three additions, no failures/errors/skips/expected failures |
| Resource handling | Full corrected suite ran with ResourceWarnings treated as errors |
| Script suite / independent digest vectors | **31 passes / PASS** at intake; their inputs are unchanged by the correction |
| Native/oracle build inputs | Nine native inputs and ten oracle source/recipe inputs unchanged; host/oracle source bundles unchanged |
| Raw ARM64 artifacts / fresh builds | Pending: this Linux/x86_64 workstation has no Docker executable; original raw artifacts are referenced in the prior macOS private workspace |

Hash-checking the retained build report and its source bindings does not independently
inspect the raw ELF/manifest/BPF outputs. The original artifact-only build evidence
retains its original scope and bytes. No host installation, live SSH authentication
or native actor execution occurred in this review.

| Identity | SHA-256 |
|---|---|
| Corrected 45-file source inventory | `f53058a955de01027efcdbaa6adace7d6904197c845e5886b25e7d9db41bc227` |
| Review/remediation record | `25dcb6ea3fa8b3642cf9fd1d06d2c3f988dab7272e7e894a58abb6bbc64a60be` |
| Corrected C0 test report | `94bd6ddf268cf9c877868019319fae2268da6869386bfc9ffba15ec4ed8b88d9` |
| Original-source red regression report | `78dde4da355071fbd4656517cfe383cc54e871cdda99c9ed60737bdf7b0c5574` |
| Corrected transport bundle | `1f83f90e21d13092ff8ac146d2415681afceb15de7ac5780b308413b0024abdd` |

Preparation packets must bind the corrected `runtime_context_transport` identity.
See the [evidence index](evidence/m4-c0-context-provisioning-review-2026-10-11/README.md)
and [manifest](evidence/m4-c0-context-provisioning-review-2026-10-11/manifest.json).
Private logs and the evidence assembly script are under
`/tmp/opencode/m4-c0-context-provisioning-review-2026-10-11/`.

## Next gates

1. **Completed subsequently:** Podman ARM64 reconstruction and both final
   source/static-artifact reviews pass on the linked 202-test remediation snapshot.
2. Make the separate scoped PR/publication decision; the existing remote checkpoint
   predates this cleanup correction.
3. Identify/admit the actual Linux/AArch64 host, then perform operator installation
   and live SSH/context acquisition.
4. Complete runtime-image admission, full trial/timing/cleanup assembly and separately
   authorized calibration. Full M4 remains open.
