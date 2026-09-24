# PC01 Step 6 evidence review — 2026-09-24

**Decision: ACCEPT PC01 as the refreshed positive control; proceed to P01
artifact preparation.** The reviewed evidence supports the prescribed
`ledger-claim-release-v1` control on the executed artifacts. All 13 negative
cases remain unrun. **M4-P3 and full M4 remain open.**

This is the coordinator's scoped case-evidence review. It does not replace the
independent measurement/provenance review required for eventual M7 release
assembly, and it grants no new execution or publication authority.

## Reviewed identity

| Item | Identity |
|---|---|
| Executed commit | `634040131b36e1cbbc2e251364dacbec2ae5dd01` |
| PC01 fixture | `0a1a9641a6044e4dfde80a5c3d381216` |
| Actual run report | `2e1d4817d835d5a76a42552ca31645f97b406843b0bd980a0651353843158e86` |
| Consumed approval | `3e034bd6f56cc595fd4d8cba566f2722a3428691f185cecfd59228dedde7a695` |
| Source-review inventory | `dbe881b2c269535b633188986c6f3adbdac0226df23528290bc4554c93277113` |
| This machine-readable evidence review | `b416011bf828111cecf2311a3adf1243e8e6b2ffe1c3ace9bce8c47ae8e461bb` |

All five [run-evidence exports](evidence/m4-pc01-run-2026-09-23/README.md) match
their mode-0600 private originals. The approval, separate execution decision,
single-use reservation and consumed disposition bind the same case, commit,
plan, recipe and final report. The recorded invocation completed within its
approval window. The read-only preflight's GitHub 503 preceded reservation and
fixture start; it does not constitute a second case invocation.

All **81 reviewed source hashes** and **15 recipe hashes** still match. Recorded
image/preparation and fourteen-check CI evidence bind the execution inputs. The
checkout at review time remained at `6340401` with clean tracked execution inputs.

PR #12 merged **after the run**, at **2026-09-23 20:24:56 UTC**, as
`320bce31db07e25758015b7466342fc73300f739`. GitHub's merge tree is
`1ebb7168db71ad488de66da157ab85978a3b6d45`, identical to the executed commit and
previously tested PR merge tree. This establishes source continuity; it is not a
new execution approval or a claim about later post-merge workflow results.

## Evidence assessment

The review recomputed public relationships directly from the retained report,
decision, intent, journal and postcheck. Counter expectations and ACL-pair
inventories were specified literally rather than delegated to the measurement
validator. No case, image build or mutation command was executed.

| Case assertion / area | Decision and evidence |
|---|---|
| CR01–CR09: claim/release sequence | Accepted within PC01 scope: all nine expected statuses, two claims/fences/reservations, exact replay/stale-token outcomes and numeric before/after/deltas |
| CR10: authority separation | 46 exact command/authority `NOPERM` pairs; every retained post-state hash equals the final control state |
| CR11: complete state/expiry checks | Reviewed executor assertions and retained state hashes support the fixed fixture; 33 literal counters per snapshot and tombstone absolute-expiry relationships rechecked |
| CR12: positive bootstrap and teardown | Bound probe/restart/BOOT/setup receipts, early setup-role retirement, actual admission/process observations, worker-first revocation and six-resource destruction verified |
| Source/artifact authority | Approval, plan/recipe, image environments, recorded CI and unchanged source inventory cross-bound; consumed approval cannot authorize another attempt |
| Process/resource limits | Five stages report the required two-process inventory, zero external routes and exact UID/capability predicates; admission specs retain approved memory/command/environment settings |
| Journal | All 28 ordered JSONL actions match the report, with probe → kill/restart → resume → measure → worker quiescence → fresh revoker → Redis/volume removal ordering |
| Cleanup corroboration | Original independent postcheck retained; a new read-only check on September 24 again found all four exact containers and both volumes absent, with empty fixture-filtered listings |

Claims/creations/fence follow `[1,1,1,1,1,2,2,2,2]`; pending capacity follows
`[1,1,1,0,0,1,1,0,0]`. Final next ordinal is 3, total/open job counts are 1,
and request starts, deliveries and output commits stay zero. Replays/rejections
have zero counter deltas and identical preceding state hashes. Pending
reservations have no Redis key expiry; cancelled tombstones expire at exactly
release time plus 86,400,000 ms, with no extension on replay.

## Coverage disposition

The [review ledger](evidence/m4-pc01-review-2026-09-24/README.md) links CR01–CR12
to the original **17 partial requirement mappings**. It retains the complete
104-requirement / 52-operation-variant inventory reference. **No full requirement
or operation variant is closed by this control.** The initial planning packets
and original run evidence remain unchanged.

| Package area | Current evidence / remaining work |
|---|---|
| PC01 refreshed control | **Accepted scoped real-Redis control**, with approval consumed |
| P01–P03 presence cases | Three implemented/reviewed cases; **no real runs** |
| S01–S05 stored-authority cases | Five implemented/reviewed cases; **no real runs** |
| W01–W12 wire case | Implemented/reviewed, including its positive control; **no real run** |
| B01–B09 BOOT case | Implemented/reviewed, including its positive control; **no real run** |
| A01–A03 administrative denials | Three implemented/reviewed cases; **no real runs or real admin positive controls** |
| H/I admission variants | Existing offline/simulated checks retain that evidence class; PC01 positive isolation does not execute the negative matrix |

In section 17.7, PC01 supplies partial evidence for positive setup/BOOT,
manifest ownership, ACL feasibility and successful export/teardown. Missing or
synthetic BOOT proof, malformed gates/state, candidate/freeze presence, valid admin
denials, adverse isolation, interrupted cleanup and broader administration remain
subject to their own mapped cases and evidence.

The evidence has clear limits: full private state was checked inside the reviewed
executor and cannot be reconstructed from exported hashes after private material
was discarded. Controller actions are not an independent internal Lua
crash-boundary trace. The preliminary probe does not establish all-transition
durability or restore behavior. Image-check memory peaks and configured runtime
limits do not establish maximum-shape memory acceptance, and the 12,176 ms /
4,388 ms lifecycle intervals are not the ≥1,000-sample Lua latency gate.

## P01 preparation handoff

| Item | Required scope |
|---|---|
| Case / scenario | `ledger-candidate-compat-present-v1` / `ledger-candidate-compat-present` |
| Profile | `ledger`, explicitly labeled negative stored state |
| Single setup delta | Add the case's compatibility-marker **hash** at `mifolyo:contracts:candidate`; all other negative-only keys remain absent |
| Size | 27 direct setup keys; 58 possible protocol positions, with durability BOOT-owned |
| Sources | Canonical `CJ2_APPROVE_BOOT` and `CJ2_TRY_CLAIM` only |
| Required outcome | Two otherwise valid CLAIM calls return `CRAWL_V2_INVALID_STATE`, each with complete unchanged state and zero accounting delta |
| ACL checks | 46 direct authority-denial probes; ledger retains TYPE-only candidate access and no authority writes |
| Current reviewed recipe | `7123ed770cb9c900319f412356652e7cb27eef6c3f2704818d3dee402fa4ee74` |
| Case-specific plan / validation | **Pending**; PC01's plan and preparation report are not relabeled as P01 artifacts |
| Authority | **Not requested; no execution authorized** |

The correct hash type is important: active-gate absence checking must produce
`INVALID_STATE`, not `WRONG_TYPE`. An outer `NOPERM`, `NOSCRIPT`, malformed-wire
error or unexpected success is not the planned P01 result. The second negative
call follows only a definite first result and unchanged-state check; ambiguous
transport failure still stops the case without retry.

The next action is to prepare/revalidate P01 using the explicit
`--case ledger-candidate-compat-present-v1` selector, the intended immutable
images, a fresh preparation output directory and the exact chosen reviewed
revision. Preserve 300/30/60-second limits and current memory bounds. Verify the
new plan/recipe/image/CI bindings, then obtain fresh case-specific owner approval
and a separate execution decision. Use new fixture resources and credentials.

Reassess affected control evidence if source, configuration, fixture shape, ACLs
or relevant image behavior changes. The verified merge does not waive that
requirement. The review/status records are retained in the documentation/evidence
checkpoint; the primary [plan](crawl-jobs-v2-plan.md) owns the next gate.
