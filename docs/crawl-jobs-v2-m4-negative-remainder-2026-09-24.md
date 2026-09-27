# Remaining bootstrap/ACL cases — 2026-09-24

**All twelve remaining cases PASS.** P01 and each newly executed case have
accepted scoped evidence reviews. The owner approved the exact per-case
artifacts, separately requested sequential execution, and each case ran once
with successful revocation and independently verified cleanup. All twelve
approvals are consumed. No negative Redis cases remain unrun in this package;
full M4 remains open.

## Review and shared prerequisites

The scoped coordinator review accepted P01's two exact active-gate rejections,
zero 33-counter deltas, 46 authority denials, bound probe/BOOT history, process
admission, consumed approval and six-resource cleanup. It rechecked original
hashes/receipts and independently inspected resource absence. Its SHA-256 is
`3eff5a0a48cea36645289ca10976b0065f827a99ff9df674e5e1bb468c3561f5`.
This adds observed partial evidence for `17.7/e763d134a36fb68c`, closing no full
requirement or operation variant. It is not final independent release review.

All 81 files in the independent source-review inventory
`dbe881b2c269535b633188986c6f3adbdac0226df23528290bc4554c93277113` and all 15
recipes still match. The remaining case definitions, literal response sequences,
source lists, role counts, limits and setup profiles agree with the immutable
package specification and current closed registry. No source correction was
needed. PC01 remains the accepted positive control on these source/image/ACL
policies.

Execution selects published **`963b67b73e2cd67ff73f559fa9984813b9d5946a`**.
Its published, tested PR-merge and final PR #13 merge trees are identical.
All 14 protected checks are green; retained eight-shard CI evidence accounts
for 475 roots (474 pass and the single allowed optional native-factory skip).
P01's [preparation record](crawl-jobs-v2-m4-p01-preparation-2026-09-24.md) supplies
the full CI/tree/source identities; its retained CI gate hash is
`223a6ae284ec1cdaf99265daf24c70f9d81d081300a32cf540ff8303c8b456a2`.

## Exact remaining scope

The [artifact manifest and preparations](evidence/m4-negative-remainder-2026-09-24/README.md)
bind every full plan/recipe/file hash. Manifest SHA-256:
**`8e43dfb7475e7fa5b42b5fbbc79e69b0b14272cdeaa4950fee7a85a263ebaca3`**.

| Order / label | Exact case | Required measured result |
|---|---|---|
| 1 / P02 | `ledger-candidate-contract-present-v1` | Two `CRAWL_V2_INVALID_STATE` rejections |
| 2 / P03 | `ledger-admin-freeze-present-v1` | Two `CRAWL_V2_INVALID_STATE` rejections |
| 3 / S01 | `ledger-active-compat-missing-v1` | Two `CRAWL_V2_COMPATIBILITY_MISMATCH` rejections |
| 4 / S02 | `ledger-active-contract-wrong-type-v1` | Two `CRAWL_V2_WRONG_TYPE` rejections |
| 5 / S03 | `ledger-active-contract-mismatch-v1` | Two `CRAWL_V2_CONTRACT_MISMATCH` rejections |
| 6 / S04 | `ledger-guard-mismatch-v1` | Two `CRAWL_V2_IMMUTABLE_MISMATCH` rejections |
| 7 / S05 | `ledger-active-compat-extra-field-v1` | Two `CRAWL_V2_INVALID_STATE` rejections |
| 8 / W | `ledger-wire-negatives-v1` | 24 specified rejections; same-instance `CLAIMED` / `RELEASED_READY` controls |
| 9 / B | `bootstrap-rejections-v1` | 18 specified BOOT rejections; actual-evidence `OK` / `EXISTS_IDENTICAL` controls |
| 10 / A01 | `ledger-install-denied-v1` | Two `CRAWL_V2_BOOT_UNAPPROVED` denials at Lua mutation ACL preflight; same-wire `CANDIDATE_INSTALLED` under release admin |
| 11 / A02 | `ledger-retire-denied-v1` | Canonical INSTALL prefix; two outer-key `NOPERM` denials; same-wire `LEGACY_RETIRED` under migration admin |
| 12 / A03 | `ledger-promote-denied-v1` | Canonical INSTALL/RETIRE prefix; two outer-key `NOPERM` denials; same-wire fresh `CONTRACTS_PROMOTED` under release admin |

The seven P/S cases and W each require 46 direct authority denials. Expected
totals are **62 measured negative calls, seven measured positive controls and
368 direct ACL probes**, plus three administrative prefix operations. Every
negative requires exact code/layer and complete unchanged state/accounting;
generic errors and ambiguous transport cannot count as success or trigger retry.

P/S are separately labeled negative stored-state fixtures, with only their
specified delta. W preserves ready state before its positive controls. B uses
only the probe/durability positions and no job. A cases use the separate fresh
administrative profile, empty legacy/downstream state and zero jobs; marker,
retirement and guard changes come only from canonical scripts. A01 has seven
roles; A02/A03 have eight. The positive controls operate solely within disposable
fixtures and do not promote application or retained data.

## Preparation and recorded decisions

All twelve explicitly selected arm64 preparations passed stopped-role admissions,
Redis version and 62-file/15-recipe image checks with the existing limits.
All 48 metadata containers stayed stopped; 72 metadata resources were independently
inspected absent. Preparation scan matches were 24 exact public RETIRE source
digest occurrences, with zero unresolved findings and no suppression changes.

Operator/platform: `fullerkris`, Linux/arm64. Harness:
`sha256:51bc4896057b8015e1bb67ff7e57448ed6356449c98a6df9100c06693de8e265`.
Redis 7.4.11:
`sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c`.

The owner selected **Approve all 12 cases**, then separately **Execute all 12
sequentially**. Each case received its own one-use approval. The shared window
was **18:50:06.241–20:50:06.241 UTC**; approval-set SHA-256 is
`851d86e76168fee51d6fd6ffe149d1e50b0d01182f5df5c136de18f70ceebbdb`, and the separate
execution-sequence decision is
`1cf9b1b03f8cf5c110ceec52277ddb8d406f4e2e547d721d462f3aa2bb8970e1`.

Invocation was supervised one case at a time, with each predecessor's reviewed
PASS, revocation and independent absence required before continuing. The rule
was to stop on failure or uncertainty; none occurred. Fresh resources/credentials
and unchanged 300/30/60-second limits,
128/256/528 MiB role limits and 400 MiB Redis maxmemory apply to every case.

The manifest is an immutable preparation checkpoint, not execution authority.
The controller's existing single-case approval schema and entrypoint remain in
use; no automatic batch dispatcher is introduced. Approval/reservation originals
remain private under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-negative-remainder-2026-09-24/cases/<label>/`.

## Actual execution and scoped review

The first case decision was **18:57:18.383 UTC** and the last final report was
written at **19:02:38.794 UTC**, all within the approval window. Every controller
was invoked once; per-case decision-to-report intervals ranged from **6,737 to
16,852 ms**, including cleanup. These are lifecycle intervals, not per-Lua
latency/benchmark acceptance measurements.

| Cases | Real result | Scope verified |
|---|---|---|
| P02, P03 | PASS / PASS | Correctly typed contract/freeze presence; exact active-gate rejections |
| S01–S05 | All five PASS | Missing, wrong-type, mismatched and extra-field stored state; exact designated errors |
| W | PASS | 24 exact rejections, then `CLAIMED` / `RELEASED_READY` same-instance controls and 46 ACL denials |
| B | PASS | 18 exact BOOT rejections, then `OK` / `EXISTS_IDENTICAL`; complete public two-key state additionally reconstructed |
| A01 | PASS | Two Lua mutation-preflight denials, followed by same-wire `CANDIDATE_INSTALLED`; seven-role teardown |
| A02 | PASS | Canonical INSTALL prefix; two outer-key denials, same-wire `LEGACY_RETIRED`; eight-role teardown |
| A03 | PASS | Canonical INSTALL/RETIRE prefix; two outer-key denials, same-wire fresh `CONTRACTS_PROMOTED`; eight-role teardown |

Across these twelve cases, verified totals are:

- **62 exact negative calls**, all with unchanged complete state and accounting.
- **Seven measured positive controls**, plus **three administrative prefix calls**.
- **368 direct authority probes**, all `NOPERM`, each with unchanged post-state.
- **77 per-case roles revoked**, including the administrative identities, with
  worker stop/wait/PID-zero/removal before fresh revoker and revoker last.
- **48 containers and 24 volumes independently absent**, in addition to the
  separate preparation-resource checks.
- **336 ordered controller actions**, including **132 cleanup actions**.

The coordinator checked each case before proceeding: exact artifact/approval
bindings; response code, actor, order and time bounds; literal before/after/delta
counters; ACL command/authority pairs; public probe/BOOT relationships;
administrative prefix/positive response hashes; actual container/process receipts;
revocation ordering; and direct inspection of every owned resource's absence.
Each run-evidence secret scan reported zero findings. Successful-path evidence
is not a claim that interruption/failure paths were exercised.

The complete exported packet scan found 24 copies of the public RETIRE source
digest and one checksum of the preparation-scan triage record. Every matched
location was verified against its source artifact, leaving zero unresolved
findings without suppressions or changes to approved bytes.

For P/S, all 33 counters stay at the ready-state baseline. W alone observes one
claim/reservation/fence and next ordinal 2, then zero pending capacity after
release; starts, deliveries and output commits remain zero. B and A cases have
no job and explicitly export counter applicability as `not_applicable`.

Full private worker-state/expiry equality was checked by the unchanged reviewed
executor/controller; exported hashes cannot recreate discarded private owners,
tokens and reservation identities. BOOT's two-key fixture has no such worker
material, so the postcheck also reconstructed its absent and approved state
hashes. Administrative reply/prefix hashes were independently recomputed from
public observations; full private namespace state remains bound to the runtime
oracle.

## Retained evidence and coverage

The [evidence index](evidence/m4-negative-remainder-2026-09-24/README.md) links all
twelve actual reports. Each case has six exact run exports (report, intent,
journal, execution decision, postcheck and invocation receipt), with every hash
bound in `results.json`. Approvals, reservations and final consumed dispositions
remain private, mode 0600, under the case workspace.

| Aggregate artifact | SHA-256 |
|---|---|
| Actual results and scoped reviews | `0d175db16e1e6fe0729a1c20dbe1f6f1c81158b9e877b98f732e8e809c707b3d` |
| Coverage ledger | `4cf0ff3823dab5aa164f5401cd16f645bb8a849f3df0e27b622f62c6aa5b99ca` |

The package's **14 real-Redis cases** now all pass: accepted PC01 control,
accepted P01 presence evidence, and these twelve accepted scoped results. The
coverage ledger retains **35 package case assertion IDs**, the seven partial
section-17.7 links and the complete **104-requirement / 52-operation-variant**
inventory reference. **Zero negative Redis cases remain unrun.** No full
requirement or operation variant is closed by this case-result roll-up.

**Next gate: package-level evidence/coverage reconciliation**, especially the
82 H/I offline/admission variants and any remaining M4-P3 obligations, before
planning the wider worker-death/lease-expiry matrix. H/I retains its actual
offline/simulated class; positive target isolation is not adverse-target evidence.
M4-P3/full M4 are not marked accepted by this record. Internal Lua crash
boundaries, all-transition AOF/restore, maximum shapes, ≥1,000-sample latency,
full/migration-shaped administration and final independent release provenance
remain separate gates. The [primary plan](crawl-jobs-v2-plan.md) owns current
status; original planning and prior run evidence stay immutable.
