# C0 context/oracle independent review — 2026-10-09

**Status: independent correctness/security review complete —
GO_FOR_CONTINUED_IMPLEMENTATION. All three findings are closed; no actionable
findings remain.** The corrected package has **40 files and 161 passing tests**.
The owner authorized separate correctness/security reviewers and updates to the
existing project and MCPVault documentation. Work remains local on
`feature/crawl-jobs-v2-c0-context-oracle`, based on `2ff03af`.

## Initial independent findings

Both reviewers verified the same immutable 1,677-file snapshot and its 39-file
package inventory, independently passed all 149 existing tests, and returned
`NEEDS_REMEDIATION`. The original implementation checkpoint and failed/probe
records retain their original bytes and version bindings.

| Finding | Proven boundary defect | Implemented correction |
|---|---|---|
| COR-CTX-001 — high | The final oracle poll could outlive observer admission or the context lease before G | All readiness polls precede final expiry checks; failures clear all three cached admissions. Oracle operation deadlines also recheck after polling |
| COR-CTX-002 — medium | Equal receipt/lower-cut timestamps incorrectly established ACK ordering | Timestamp-only credit requires strict `< lower`; equality and zero-width ties remain unknown |
| SEC-CTX-001 — medium | Chained CLOSE responses could erase completion/error history or invent a cut without HELD | Controller-side monotonic client history, immutable cut classification, derived late status and matched-HELD requirement for no-reply credit |

Initial intake: `be961ae84551e2f3f4b1048a4a70f5f12b7356b312276fac9a26995d81bf5e59`.
Correctness: `80e03a35f09f9ba53c76746b581557f8aea5e02092587c5c8374b9ffbae0215d`.
Security: `814645cde86463ad003b78102619d80673dbe341c5a93cf15672ebee51fccce2`.

## Regression and preservation scope

The corrected package has **40 files and 161 tests**: all 149 previous test IDs
plus 12 new regressions/positive controls. The initial red run reproduced the
boundary defects. One intermediate benign-responder run raised Darwin
`LOCAL_PROCESS_CLEANUP`; its log is retained. That fixture now publishes output
EOF while its leader remains alive for controlled disposal. Production cleanup
refusals and all original tests remain intact.

No native actor/oracle sandbox, tracing or real Redis experiment was executed.
Artifact rebuilds execute only the manifest builder. The unchanged native inputs
and artifacts are checked by hash/static joins rather than rerunning binaries.

## Exact-byte final re-review

Each reviewer independently ran the full 161-test suite once, retained all 149
previous IDs and verified the 1,679-file corrected snapshot before/after review.
Correctness replayed the unchanged original expiry/socket probes against both
old and corrected bytes; security rejected all three byte-identical historical
attack sequences using the real corrected bridge.

Correctness additionally checked 792 worker/history exchanges, 16 history
mutations, 11 chained contradictions, 15 legitimate/missing-match controls and
six stop-generation negative controls. Security checked seven additional malicious
sequences, 11 honest controls and seven deadline controls. Its pinned 48-file scan
had zero matches and no suppressions. These fixtures use simulated admission/stop
metadata and benign Python processes/sockets; they do not establish native runtime
feasibility.

Local script tests: **31 pass**. Independent digest vectors: **PASS**. The new
oracle source manifest was rebuilt without running the worker. All nine native
build inputs and eleven original artifacts remain byte-identical; ELF/manifest,
compiled BPF and OCI-profile joins pass. The interpreter identity is recorded,
not an independent audit of the full runtime image or its libraries.

| Identity | SHA-256 |
|---|---|
| Corrected 40-file source inventory | `99d9dd6f2c475cd83960c66028016d7d508acadd3563f2e440c3bf6ff012c4c3` |
| Corrected review intake | `0bc3175b5d13ecb5239095abaae51fecd5bbbed3df219116636611c97a39d02f` |
| Final correctness review | `1ffa557b61e979bce1455927fd4d5c5859c3aa4febb9f718a9f4d7eed2190bb6` |
| Final security review | `7a85d1801797b57e3183ae811843d9b826103865840105199bc86a4112bc355e` |
| Combined final verdicts | `cf04c87324bf8a02e37eb2e3099443c49ee0c25cf545d85686f60c1ef33e2ad1` |
| Local remediation verification | `83ac4a4e1822c38ba08210ebf307e2f929e4712dd61a4ebc9acd1b201ef47f94` |
| Artifact binding verification | `8b7817ff8d2fef399dd0d7d0725e9828374b3aaed44daa1f13b16906cef4b96d` |
| Current oracle worker bundle | `30f1a092422391317453ea624ca7532c46d4d13607c4261b8d80fc50eef6583b` |

The [evidence index](evidence/m4-c0-context-oracle-review-2026-10-09/README.md)
and [manifest](evidence/m4-c0-context-oracle-review-2026-10-09/manifest.json) bind
ten byte-identical exports, including original/final reviews. The local verification
record's pending-review field is its recording-time state; the final verdicts
supersede it. Reviewed package bytes remain unchanged by later documentation updates.

## Remaining gates

1. **Publication preparation complete:** the [53-path candidate](crawl-jobs-v2-m4-c0-context-oracle-publication-preparation-2026-10-09.md)
   preserves this reviewed package and passes fresh 161 C0 / 31 script checks.
   An owner publication decision is next; the candidate remains unstaged,
   uncommitted and unpublished, and main still has 113 C0 tests.
2. Provision/admit the Linux host source/anchor/key/transport and exact runtime
   images, including the actual oracle OCI sandbox.
3. Complete trial/timing assembly, whole-trial monitoring and independent cleanup/
   absence proof, then obtain separate exact-artifact/execution approval for calibration.

Full M4 remains open. See the [implementation checkpoint](crawl-jobs-v2-m4-c0-context-oracle-2026-10-09.md)
and [current plan](crawl-jobs-v2-plan.md). Private review snapshots, reports and logs:
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-c0-context-oracle-review-2026-10-09/`.
