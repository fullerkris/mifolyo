# C0 pinned-SSH review remediation evidence — 2026-10-11

The [remediation report](../../crawl-jobs-v2-m4-c0-ssh-remediation-2026-10-11.md)
records COR-SSH-002/003 and QA-ENV-001 corrections on the unchanged 45-file package
inventory shape. The [local manifest](local-manifest.json) binds eight exact exports:

- [source-inventory.json](source-inventory.json): corrected package hashes, two
  changed paths and unchanged native/oracle build-input identities.
- [remediation.json](remediation.json): findings, correction descriptions, both
  full-suite runs and retained ARM64 artifact continuity.
- [red-tests.json](red-tests.json) and [red-tests.log](red-tests.log): original-source
  regression failures, restrictive-umask fixture error and successful reuse control.
- [c0-tests-022.json](c0-tests-022.json) / [log](c0-tests-022.log): 202/202 passes under umask 022.
- [c0-tests-077.json](c0-tests-077.json) / [log](c0-tests-077.log): 202/202 passes under umask 077.

Both successful reports retain all 197 previous test IDs plus five additions.
Their JSON bytes match because source hashes, test inventory and outcomes match;
separate command/log records preserve the two umask executions. The original
[independent reviews](../m4-c0-podman-review-2026-10-11/initial-review-manifest.json)
remain unchanged, including security's original failed full run.

## Completed independent re-review

Both [correctness](correctness-review.md) and [security](security-review.md) return
**GO_FOR_CONTINUED_IMPLEMENTATION** on the exact corrected inventory, closing
COR-SSH-002/003 and QA-ENV-001 and preserving COR-SSH-001. Each ran 202/202 tests
independently, under umasks 022 and 077 respectively. The
[correctness manifest](correctness-manifest.json) binds 20 exact reviewer exports;
the [security manifest](security-manifest.json) binds 13.

The [final gate](verification.json) joins both verdicts to the source and retained
artifact continuity. The [final manifest](manifest.json) binds all 45 exact
local/reviewer exports. Local pending-review fields retain their recording-time
scope; the final gate supplies the subsequent verdict. Initial failed test and
review records remain unchanged. Publication and live runtime gates remain open.

Private snapshots/probes/reviewer outputs:
`/tmp/opencode/m4-c0-ssh-remediation-2026-10-11/`. Native artifacts and original
review probes remain in `/tmp/opencode/m4-c0-podman-arm64-2026-10-11/`.
