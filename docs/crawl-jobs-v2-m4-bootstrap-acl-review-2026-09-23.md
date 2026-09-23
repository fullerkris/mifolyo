# Bootstrap/ACL Step 3 independent review — 2026-09-23

**Final decision: correctness GO and security GO for image/CI preparation.**
Both independent reviewers found **no actionable findings** in the frozen scope.
No source correction or follow-up re-review was needed. Full M4 acceptance and
new real-Redis execution remain later gates.

## Scope and exact identity

- Package: `bootstrap-acl-negatives-v1`, the Step 2 implementation described in
  the [primary plan](crawl-jobs-v2-plan.md#step-2-local-implementation-result-2026-09-23).
- Branch: `feature/crawl-jobs-v2-claim-release`.
- Base HEAD: `b4bda19f07bb22f37737508cd10424b75690d6f5`; implementation changes are local.
- Scope: **81 source/test/planning files**, including the new untracked modules
  and tests, image preparation and builder allowlists. Unrelated user work and
  mutable status roll-ups are outside the source inventory.
- Both reviewers verified all **81 worktree hashes before and after review**,
  all 81 preserved source copies, all **15 recipe hashes**, **62 execution-image
  inputs**, and the unchanged canonical identities. Coordinator closeout also
  confirmed the source hashes and recipe bindings unchanged.
- Correctness session: `ses_f30c0143fffebKuGdWm0VoSyAK`.
- Security session: `ses_f30c01384ffeZxJx2pHHcSK7LN`.

| Artifact | SHA-256 |
|---|---|
| Initial and final unchanged source inventory | `dbe881b2c269535b633188986c6f3adbdac0226df23528290bc4554c93277113` |
| Review closeout / verdicts | `1e0e8d4ccd0aa27abc18e538731020563b3617b2d00c643aaf4052aecd374eb7` |
| Exact correctness checks export | `a69159bfcc6b4b84e8409a8966350b6c1a476b3027d0dbbc4639676654fd1b86` |
| Exact security results export | `59a1241d5bbeebaa9aeb884dc67edac9846c227c583e3de0d8d9e5e49f33e9b0` |

The [review evidence package](evidence/m4-bootstrap-acl-review-2026-09-23/README.md)
retains exact-byte inventories and reviewer summaries. There is no corrected
inventory because the reviewed source did not change.

## Independent review outcomes

| Review | Outcome | Principal areas examined |
|---|---|---|
| Correctness | **GO; no actionable COR finding** | Negative reachability, wire/fixture/oracle independence, full invalid-state snapshots and expiries, canonical admin prefixes/controls, role lifetimes, success/failure receipts, deadlines, cleanup and packaging |
| Security | **GO; no actionable SEC finding** | Literal ACL boundaries, distinct admin roles, setup retirement, BOOT provenance, receipt substitution/redaction, admission, volume sharing/ownership, ambiguity handling, bounded I/O and revocation |

The reviewers preserved the responsibility split: Lua validates BOOT digest
structure and bindings; the controller's actual probe/restart observations and
later evidence review establish rehearsal provenance. Administrative negatives
require valid preconditions and same-wire positive controls. RETIRE/PROMOTE outer
key-denial checks are not mislabeled as inner Lua mutation-preflight tests.

## Verification actually performed by the reviewers

| Reviewer | Independent observation |
|---|---|
| Correctness | New four Go/Lua roots passed `-race -timeout 900s -count=1` in **76.243 s**: 70 canonical calls plus four explicitly offline selector checks |
| Correctness | Go offline-artifact root passed in **2.558 s**, covering all 17 offline scenarios |
| Correctness | Negative fixture/admission modules: **10 tests PASS, 21.944 s**, including all 82 H/I variants |
| Correctness | All 13 simulated lifecycle subcases completed PASS; three focused failure-prefix/receipt-binding methods passed in **56.896 s**, and the interruption/cleanup/journal method passed in **47.919 s** |
| Correctness | Existing review-regression, container-admission and image-environment modules: **27 tests PASS, 3.229 s** |
| Correctness | Separate scratch facade passed 13 typed initial snapshots, 13 unknown-key rejections, eight repaired-negative-state rejections, 14 expiry rejections including a 1 ms tombstone extension, 12 literal wire-position checks, nine admin-prefix and 13 role-inventory controls |
| Security | Reviewer-owned probe suite: **11 tests PASS, 104.252 s**, zero failures/errors; no author test-helper imports |
| Security | All 13 ACL/role layouts and **3,024 literal authority-denial checks**; 91 BOOT-provenance forgeries rejected; success/failure resume binding checked across 13 cases |
| Security | **56 forged positive-control success/failure receipts rejected**; ambiguous/wrong/successful/mutating outcomes stop after the first negative dispatch; setup regrant, bounds, metadata/process/approval substitutions, sharing, cleanup, revocation and RESP/redaction controls pass |

**Incomplete review invocation:** the correctness review's broad
`python3 -B -m unittest -v test_negative_execution` command reached its
**360-second review-command timeout** during the larger fault-matrix method.
Its preceding all-13-lifecycles method passed; no assertion failure was printed
before timeout. The whole module and unfinished method are **not** recorded as
passed in that invocation. Subsequent focused checks are listed separately.

The correctness reviewer completed **42 distinct Python test methods** overall,
including the one completed method from that incomplete invocation. Neither
reviewer's result is a fresh run of the coordinator's complete 95-test suite.
The earlier complete-suite PASS (648.365 s), 21-script PASS, vet and secret-scan
triage remain supporting Step 2 evidence with their original provenance.

Both reviews used Python `-B` and the approved temporary directory. Go used the
cached `GOTOOLCHAIN=go1.25.13`, `GOPROXY=off` and readonly modules. Probes were
offline/fake-backed; no Docker/Redis command, image build/pull, service, retained
datastore, external endpoint, Git publication or application activation was used.

## Retained reproduction material

Root:

```text
/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-bootstrap-acl-review-2026-09-23/
```

- `initial.json`, `initial-source/` and `context/`: original inventory, source
  copies and review-time plan/README context.
- `correctness-01/review.md`, `checks.json`, `verify_manifest.py` and
  `probe_correctness.py`: exact correctness assessment, command outcomes and probes.
- `security-01/report.md`, `results.json`, `verify_inventory.py` and
  `security_probes.py`: independent security assessment and guarded probes.
- `verdicts.json`: final scoped decisions and hashes of all eight private review
  artifacts. Exported check/result JSON files match reviewer originals exactly.

Coordinator closeout verified all four exports and eight private review-artifact
hashes. The scoped export scan found one generic-key match, independently verified
as the existing public `cj2_retire_legacy_keys.lua` source SHA-256
`702b096b843d80cd1c85186cf79fafa29c08766ea22c599af29877e1e8013fc7`.
The report scan found no leaks; zero unresolved findings remain. Scan outputs
and deterministic triage are retained in the private review directory.

## Step 4 handoff and limits

Prepare and validate fresh immutable images and the selected case's plan/recipe
from these reviewed bytes, then perform the authorized scoped publication and
exact-revision protected-CI process. The preparer must explicitly select the
intended case; a passing default smoke image check does not establish the new
case's execution behavior. Preserve the reviewed 62-file image scope and all
case/role/recipe bindings.

Actual target Redis ACL parsing, network/process/environment metadata, memory
peaks, stage timing, persistence rehearsal and teardown are still unmeasured for
these new cases. Keep the 300-second case, 30-second stage, 60-second cleanup and
existing memory bounds. Complete target checks and obtain fresh exact-artifact
approval plus a separate execution request before any case runs. Historical
consumed approvals and prior image/recipe identities are not reusable authority.

This scoped GO does not close the broader ledger/admin/crash/maximum-shape/latency
matrix, M5/M7 production-provenance integration, or full M4. Review and status
records remain local and uncommitted; the primary plan owns the current gate.
