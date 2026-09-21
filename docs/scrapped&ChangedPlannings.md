# Scrapped and Changed Planning Archive

> [!IMPORTANT]
> **Status: non-authoritative archive — do not use this file to plan or execute work.**
> This file records superseded planning directions only. It is not a current
> plan, runbook, protocol, deployment or migration authorization, crawl or
> rendering authorization, readiness decision, or replacement for evidence.
> Do not execute an archived direction or infer approval from its disposition.

Use these sources instead:

- For current F1-F6 program status, sequencing, and gates, use the
  [Spider and Render Worker Remediation Plan](spider-render-remediation-plan-2026-09-01.md).
- For current F3 implementation status and remaining work, use the
  [Crawl Jobs V2 Implementation Plan](crawl-jobs-v2-plan.md).
- For the normative F3 contract, use [Crawl Jobs V2](crawl-jobs-v2.md). The
  protocol takes precedence over planning summaries when wording differs.
- Preserve the
  [2026-09-07 foundation status](crawl-jobs-v2-foundation-status-2026-09-07.md)
  and the
  [2026-08-18 V1 baseline report](v1-baseline-crawl-test-report-2026-08-18.md)
  as historical evidence/checkpoints. This archive does not update, invalidate,
  or replace their dated observations.

## Scrapped

### Pre-amendment transcript contract

- **Archive date:** 2026-09-11
- **Superseded direction:** Preserve the original V2 contract while deciding
  whether transcript genesis and stage freshness required a protocol amendment.
- **Replacement/current location:** The owner-approved amendment and its scope
  are recorded in the [F3 plan](crawl-jobs-v2-plan.md#approved-transcript-amendment);
  exact requirements remain in the [normative protocol](crawl-jobs-v2.md).
- **Disposition:** Superseded by the preactivation B/G amendment, not an
  implementation-only workaround. The old projection, commit formula, and
  independently supplied BEGIN metadata are not compatibility paths.
- **Specific reason:** A complete authenticated start interval, atomic stage
  freeze, output-derived BEGIN metadata, and exact replay binding are needed
  together. The earlier NO-GO findings remain historical evidence. This archive
  grants no checkpoint-publication, Lua, runtime, or crawl authority.

### Complete terminal-transcript readiness claim

- **Archive date:** 2026-09-11
- **Source document/section:** The former completeness/readiness position in the
  [F3 plan current checkpoint](crawl-jobs-v2-plan.md#current-checkpoint), the
  [M1 review scope](crawl-jobs-v2-plan.md#review-scope), and the then-pending
  Lua-start verdict in the [evidence log](crawl-jobs-v2-plan.md#evidence-log).
- **Superseded direction:** Treat the digest-bound output context and terminal
  job witness as complete terminal request-transcript binding, and treat the
  final independent Lua-start verdict as pending rather than as a known review
  result.
- **Replacement/current location:** The 2026-09-11 independent-review blocker is
  recorded in the [F3 current checkpoint](crawl-jobs-v2-plan.md#current-checkpoint),
  the [protocol-decision work item](crawl-jobs-v2-plan.md#immediate-to-do-list),
  and the [parent F3 current roll-up](spider-render-remediation-plan-2026-09-01.md#current-roll-up).
  At that checkpoint M1 remained NO-GO; an explicit protocol-revision decision
  was required before a new independent review. The later amendment is recorded
  separately above and in the current plan.
- **Disposition:** Scrapped and retired as a readiness claim. Preserve the dated
  foundation, local-test, and review evidence, but do not infer M1 acceptance,
  Lua-start readiness, or completion from it. At that checkpoint no protocol
  revision had been approved or made in [Crawl Jobs V2](crawl-jobs-v2.md).
- **Specific reason:** The 2026-09-11 reviews found that the current digest-bound
  protocol cannot atomically prove transcript genesis, terminal freshness, and
  stage freeze/seal/commit freshness. Client-side immutability and a terminal
  witness cannot prove that no request start was omitted or added around stage
  creation, sealing, and first commit, so an implementation-only workaround
  cannot close the blocker.

### First-page-publication rollback boundary

- **Archive date:** 2026-09-10
- **Source document/section:**
  [Immutable Pipeline Cutover — Historical V1 roles, keys, and boundary record](immutable-pipeline-release-cutover.md#historical-v1-roles-keys-and-boundary-record)
  and its [legacy pipeline deployment step](immutable-pipeline-release-cutover.md#historical-v1-step-4-deploy-the-legacy-pipeline-as-one-change-set).
- **Superseded direction:** Use the legacy producer-start/first immutable page
  publication boundary to decide whether ordinary image rollback remains safe.
- **Replacement/current location:**
  [Crawl Jobs V2 — Irreversible boundary](crawl-jobs-v2.md#162-irreversible-boundary)
  and the [F3 plan rollback rules](crawl-jobs-v2-plan.md#rollback).
- **Disposition:** Scrapped for Crawl Jobs V2; the legacy runbook remains
  historical context for its earlier page/image protocol.
- **Specific reason:** A successful `CJ2_START_REQUEST` records and consumes
  request authority before DNS. An external effect can therefore occur without
  any page publication. V2 ordinary rollback ends at the first successful
  `CJ2_START_REQUEST`, not at service start or first publication.

### V1 checklist as a V2 authorization or execution procedure

- **Archive date:** 2026-09-10
- **Source document/section:**
  [Historical V1 Baseline Crawl Test Checklist — Run one bounded spider batch](v1-baseline-crawl-test-checklist.md#historical-v1-step-7-run-one-bounded-spider-batch)
  and its V1 [configuration-through-pipeline sections](v1-baseline-crawl-test-checklist.md#historical-v1-step-3-validate-configuration-and-services).
- **Superseded direction:** Adapt or reuse the V1 checklist commands as the
  authorization and execution path for a V2 crawl after remediation.
- **Replacement/current location:**
  [F3 M6 — Migration, active documentation, and rollback](crawl-jobs-v2-plan.md#m6-migration-active-documentation-and-rollback),
  [F3 M8 — Authorized bounded crawl](crawl-jobs-v2-plan.md#m8-authorized-bounded-crawl),
  and the normative [V2 cutover and rollback contract](crawl-jobs-v2.md#16-cutover-and-rollback).
- **Disposition:** Retired from V2 authorization and execution; preserve it only
  as blocked V1 protocol context until tested V2 operational documents replace
  its active sections.
- **Specific reason:** The checklist uses V1 keys and nondurable claim behavior,
  and its 2026-08-18 authorization was consumed. Reusing it would permit a mixed
  V1/V2 path and would confuse historical test procedure with new run authority.

### Retained-state reuse or selective repair

- **Archive date:** 2026-09-10
- **Source document/section:**
  [Parent plan — Read-only evidence snapshot](spider-render-remediation-plan-2026-09-01.md#read-only-evidence-snapshot),
  [F2](spider-render-remediation-plan-2026-09-01.md#f2-replace-the-retained-post-test-redis-state),
  and the former re-feed direction represented by
  [historical V1 checklist step 5](v1-baseline-crawl-test-checklist.md#historical-v1-step-5-re-feed-and-verify-the-v1-queue).
- **Superseded direction:** Reuse, selectively delete, or incrementally repair
  the retained isolated Redis state, then feed or replay over it.
- **Replacement/current location:** The project-restricted destructive reset in
  [parent-plan F2](spider-render-remediation-plan-2026-09-01.md#f2-replace-the-retained-post-test-redis-state)
  before an accepted V2 feed/replay; any real V1 queue migration instead follows
  the stopped, validated [normative migration contract](crawl-jobs-v2.md#13-stopped-v1-migration).
- **Disposition:** Scrapped for the retained isolated baseline. Preserve a
  matched, restore-tested evidence set, then destroy and recreate only the fixed
  disposable project state.
- **Specific reason:** The retained state includes discovered jobs, hash-only
  identities, publications, backlink and signal residue, and post-run queue
  mappings. An additive feeder or selective deletion cannot prove a fresh,
  internally matched baseline. The dated retained counts remain valid evidence
  of that post-run state.

### Optional run-policy validation

- **Archive date:** 2026-09-10
- **Source document/section:** The earlier foundation interpretation of policy
  helpers under [Crawl Jobs V2 — Identifiers and digests](crawl-jobs-v2.md#4-identifiers-and-digests)
  and [monotonic shared-rate semantics](crawl-jobs-v2.md#85-monotonic-shared-rate-semantics).
- **Superseded direction:** Treat structural policy-decision validation as
  sufficient and make comparison with the run-pinned group map optional or a
  caller-managed convenience.
- **Replacement/current location:**
  [F3 plan — Current checkpoint](crawl-jobs-v2-plan.md#current-checkpoint),
  [M1 review scope](crawl-jobs-v2-plan.md#m1-final-foundation-release-gate),
  and normative validation at every source, discovery, reservation, transcript,
  response, and wire consumer in
  [claim and request transitions](crawl-jobs-v2.md#103-claim-and-request-transitions).
- **Disposition:** Scrapped; consumers must use authenticated, immutable
  run-policy authority and revalidate their actual input at the consumption
  boundary.
- **Specific reason:** A structurally valid group, scope, or policy digest can
  still be detached from the exact run record. Optional checking permits
  caller-selected policy lineage and validation/use gaps.

### Production acceptance of an all-zero SHA-256

- **Archive date:** 2026-09-10
- **Source document/section:** The earlier guard/manifest bootstrap direction
  now constrained by
  [Crawl Jobs V2 — Non-authoritative acceptance-fixture bootstrap](crawl-jobs-v2.md#51-non-authoritative-acceptance-fixture-bootstrap).
- **Superseded direction:** Accept a structurally valid 64-character all-zero
  SHA-256 in production-shaped authority records or production digest paths.
- **Replacement/current location:** The exact `ZERO_SHA256` exception and
  teardown/final-assembly requirements in
  [normative section 5.1](crawl-jobs-v2.md#51-non-authoritative-acceptance-fixture-bootstrap),
  plus the production rejection boundary in
  [F3 non-negotiable boundaries](crawl-jobs-v2-plan.md#non-negotiable-boundaries).
- **Disposition:** Scrapped for production, runtime, candidate, migration, and
  release authority. `ZERO_SHA256` is permitted only in the named unavailable
  evidence fields of the isolated, disposable, non-authoritative fixture.
- **Specific reason:** A zero sentinel is not cryptographic evidence. Accepting
  it at a production authority boundary could make provisional fixture state
  appear promotable. Final assembly must replace every sentinel with a nonzero
  evidence digest after fixture teardown.

### Zero-sentinel executable acceptance bootstrap

- **Archive date:** 2026-09-21
- **Superseded direction:** The September 10 entry above retained a disposable
  zero-evidence bootstrap exception. The old normative sections 5.1/17.7 required
  unchanged Lua to accept that guard while forbidding all fixture candidate
  administration and assigning setup-manifest enforcement to Lua.
- **Specific reason:** Normal Go stored-guard/transport and Lua validators reject
  zero evidence; Lua has no setup-manifest input; active gates require candidate
  absence reads; administrative source acceptance needs isolated candidate tests.
- **Replacement:** The owner-approved amendment in
  [normative section 5.1](crawl-jobs-v2.md#51-non-authoritative-acceptance-fixture-bootstrap)
  uses reviewed nonzero test-input descriptors, harness-owned setup scope,
  command-scoped ACLs and separate ledger/administrative profiles. Zero-sentinel
  vectors survive only as non-executable serialization/rejection controls.
  Production admission positively requires final evidence/image provenance.
- **Disposition:** The amendment and offline preparation layer are implemented
  locally. Real-Redis execution and acceptance remain separately gated in the
  [F3 plan](crawl-jobs-v2-plan.md#m4-readiness-decision-package-2026-09-21).

### Generic caller-selected script-source construction

- **Archive date:** 2026-09-10
- **Source document/section:** The initial source-ordering and empty-source-set
  direction recorded in
  [Foundation Status — Verification at this checkpoint](crawl-jobs-v2-foundation-status-2026-09-07.md#verification-at-this-checkpoint)
  and the earlier generic operation-wire construction approach.
- **Superseded direction:** Let callers provide a script SHA-1, source bytes,
  expected source hashes, or per-operation source binding when constructing an
  executable Redis request.
- **Replacement/current location:**
  [F3 M3 — Source and trust-anchor rules](crawl-jobs-v2-plan.md#source-and-trust-anchor-rules)
  and [Crawl Jobs V2 — Lua transition contract](crawl-jobs-v2.md#10-lua-transition-contract).
- **Disposition:** Scrapped in favor of closed operation constructors and a
  package-private, complete, sealed `ScriptBindingSet` built only from embedded,
  reviewed source bytes.
- **Specific reason:** Caller-selected source identity can bypass the reviewed
  operation/source/contract binding. No authoritative Lua files exist yet, so
  the production sealed bundle remains intentionally unconstructible and Redis
  execution remains blocked.

### Compatibility-manifest validation at first mutation

- **Archive date:** 2026-09-10
- **Source document/section:** The earlier common-script direction in
  [Crawl Jobs V2 — Common script rules](crawl-jobs-v2.md#101-common-script-rules),
  which treated pre-mutation validation as the first required compatibility
  boundary.
- **Superseded direction:** Defer compatibility-manifest validation until a
  service attempts its first Redis or MongoDB mutation.
- **Replacement/current location:** Manifest preregistration and role startup
  checks in
  [Crawl Jobs V2 — Compatibility manifests, candidate mode, and commit guard](crawl-jobs-v2.md#5-compatibility-manifests-candidate-mode-and-commit-guard),
  [Spider startup order](crawl-jobs-v2.md#141-spider-startup-order), and
  [consumer startup gates](crawl-jobs-v2.md#142-consumer-startup-gates).
- **Disposition:** Scrapped; validate and bind the exact manifest, guard,
  contract, own image digest, and release state before registering a role or
  constructing/dispatching its operational work.
- **Specific reason:** First-mutation validation is too late to establish which
  image and authority set may participate. Preregistration closes validation/use
  gaps and prevents an incompatible process from acquiring work or becoming an
  accepted consumer.

### Implicit reservation depth/context and permissive replay

- **Archive date:** 2026-09-10
- **Source document/section:** The earlier reservation interpretation across
  [Crawl Jobs V2 — Reservation identity](crawl-jobs-v2.md#4-identifiers-and-digests),
  [reservation states](crawl-jobs-v2.md#84-reservation-states), and
  [claim and request transitions](crawl-jobs-v2.md#103-claim-and-request-transitions).
- **Superseded direction:** Resolve missing reservation depth or source context
  from caller-provided objects, and treat a matching reservation ID/status as
  enough to replay authority.
- **Replacement/current location:** Explicit source/run-policy binding and
  initial-intent rules in
  [monotonic shared-rate semantics](crawl-jobs-v2.md#85-monotonic-shared-rate-semantics),
  exact request transition rules in
  [section 10.3](crawl-jobs-v2.md#103-claim-and-request-transitions), and replay
  acceptance gates in
  [state and idempotency tests](crawl-jobs-v2.md#171-state-and-idempotency).
- **Disposition:** Scrapped in favor of contract-bound depth/context resolution
  and full immutable-identity replay gates.
- **Specific reason:** Reservation records do not independently carry decision
  depth. The decision digest must bind depth and transitions must compare it with
  the authenticated owning job; initial robots/document intents must retain the
  job's run, group, rate lineage, group scope, and initial origin. A replay grants
  I/O only while the exact reservation remains started under the current lease;
  a tombstone replay is reconciliation only.

## Moved Or Changed

### Exact retried-response replay validation

- **Archive date:** 2026-09-11
- **Source document/section:** The earlier response-schema implementation
  assumption summarized by the
  [F3 current checkpoint](crawl-jobs-v2-plan.md#current-checkpoint) and reviewed
  under the [M1 review scope](crawl-jobs-v2-plan.md#review-scope).
- **Superseded direction:** Validate an exact retried
  (`CJ2_RETRY` / `RETRY_SCHEDULED`) response by requiring
  `not_before_ms == now_ms + delay`, including on a later lost-response replay.
- **Replacement/current location:** The local response validator now preserves
  the immutable original `not_before_ms` deadline on later exact replay and uses
  checked deadline-minus-delay validation to reject impossible, underflow, and
  non-exact/overflow shapes. The governing shapes and replay rules remain in the
  normative [Lua response envelope](crawl-jobs-v2.md#91-lua-response-envelope)
  and [job outcome transitions](crawl-jobs-v2.md#104-job-outcome-transitions);
  current status is recorded in the
  [F3 immediate work list](crawl-jobs-v2-plan.md#immediate-to-do-list).
- **Disposition:** Changed locally; focused and combined-package checks are
  supporting evidence only. Aggregate final-tree re-verification and independent
  re-review remain pending.
- **Specific reason:** `now_ms` is Redis time for the replay invocation, while
  `not_before_ms` is the immutable deadline from the original transition. Their
  fixed-delay equality is valid only on first execution, rejects a legitimate
  later exact replay, and can overflow near the maximum exact integer.

### Immediate post-abort commit-backpressure job shape

- **Archive date:** 2026-09-11
- **Source document/section:** The earlier job-record validation assumption
  summarized under [F3 ledger validation](crawl-jobs-v2-plan.md#current-checkpoint)
  and examined by the [M1 review scope](crawl-jobs-v2-plan.md#review-scope).
- **Superseded direction:** Require an active stage whenever durable
  commit-backpressure evidence is present, including immediately after
  `CJ2_ABORT_STAGE` has cleared the job's active-stage field.
- **Replacement/current location:** The local validator now admits only the
  exact required immediate post-`CJ2_ABORT_STAGE` shape: the active reservation
  and `active_stage_commit_id` are clear, while the current fence, last stage,
  document-start witness, `STAGE_ABORTED` status, and abort transition identity
  agree exactly. This follows the normative
  [aborted-stage terminal reservation](crawl-jobs-v2.md#75-stage-metadata-and-accounting)
  and [stage/commit transition](crawl-jobs-v2.md#105-stage-and-commit-transitions)
  rules; current status is in the
  [F3 immediate work list](crawl-jobs-v2-plan.md#immediate-to-do-list).
- **Disposition:** Changed locally; focused and combined-package checks are
  supporting evidence only. Aggregate final-tree re-verification and independent
  re-review remain pending.
- **Specific reason:** Abort intentionally clears active-stage authority but
  retains current-fence backpressure evidence and the exact aborted-stage slot
  until the immediately following lease-ending transition or expiry recovery.
  Requiring an active stage rejected that valid serialized intermediate state;
  admitting any broader cleared-stage shape would instead accept unrelated or
  stale evidence.

### Authorization-expiry run-cancellation construction

- **Archive date:** 2026-09-11
- **Source document/section:** The earlier closed-constructor completeness
  assumption in the
  [F3 operation-wire checkpoint](crawl-jobs-v2-plan.md#current-checkpoint), within
  the [M1 operation-shape review scope](crawl-jobs-v2-plan.md#review-scope).
- **Superseded direction:** Expose only the caller-selected `CJ2_CANCEL_RUN`
  constructor for `operator_cancelled` and `source_cancelled`, leaving no closed
  construction path for the maintenance-owned `authorization_expired` reason.
- **Replacement/current location:** A dedicated local closed constructor now
  emits `authorization_expired` through the existing `CJ2_CANCEL_RUN` operation;
  the caller-selected constructor remains restricted to `operator_cancelled` or
  `source_cancelled`. The authority split is defined by the normative
  [caller/server reason matrix](crawl-jobs-v2.md#93-stable-disposition-reasons)
  and [maintenance cancellation contract](crawl-jobs-v2.md#106-maintenance-cancellation-and-retention-transitions),
  with current status in the
  [F3 immediate work list](crawl-jobs-v2-plan.md#immediate-to-do-list).
- **Disposition:** Changed locally; focused and combined-package checks are
  supporting evidence only. Aggregate final-tree re-verification and independent
  re-review remain pending.
- **Specific reason:** The normative reason already exists and is reserved to
  maintenance, so omitting its constructor made the required transition
  unconstructible. Broadening caller-selected cancellation would let a caller
  assert server-derived expiry; a dedicated constructor supplies the fixed
  reason without adding an operation or weakening caller restrictions.

### Terminal reason classes and retry/recovery counter bounds

- **Archive date:** 2026-09-11
- **Source document/section:** The earlier schema and ledger-completeness
  assumptions in the [F3 current checkpoint](crawl-jobs-v2-plan.md#current-checkpoint)
  and the [M1 record-relation review scope](crawl-jobs-v2-plan.md#review-scope).
- **Superseded direction:** Accept a terminal job reason when it belonged to the
  global reason enum, without proving that its job state and last transition
  could produce that reason, and accept run/job retry and recovery counters
  without all absolute lifecycle bounds.
- **Replacement/current location:** Local validation now applies
  state/transition-specific completion, dead-letter, cancellation, retry-
  exhaustion, and pre-I/O-recovery reason classes, together with bounded
  retry/recovery relations among claims, request starts, delivery attempts,
  retries, and recovered leases. The governing fields and classes remain in the
  normative [run record](crawl-jobs-v2.md#71-run-record),
  [job record](crawl-jobs-v2.md#72-job-record),
  [delivery-attempt model](crawl-jobs-v2.md#83-delivery-attempts-versus-request-starts),
  and [stable disposition reasons](crawl-jobs-v2.md#93-stable-disposition-reasons);
  current status is in the
  [F3 immediate work list](crawl-jobs-v2-plan.md#immediate-to-do-list).
- **Disposition:** Changed locally; focused and combined-package checks are
  supporting evidence only. Aggregate final-tree re-verification and independent
  re-review remain pending.
- **Specific reason:** Global enum membership proves only lexical validity. It
  allowed impossible records such as cancellation reasons on dead jobs,
  exhaustion reasons at the wrong transition count, retries beyond claims or
  starts, recovered leases beyond claims, and retry/recovery totals beyond their
  finite protocol limits.

### Initial `RequestDocument` claim target binding

- **Archive date:** 2026-09-11
- **Source document/section:** The earlier claim-authority assumption in the
  [F3 authority-model checkpoint](crawl-jobs-v2-plan.md#current-checkpoint) and
  the [M1 policy-provenance review scope](crawl-jobs-v2-plan.md#review-scope).
- **Superseded direction:** Accept an initial `RequestDocument` intent for a
  different same-origin target when its standalone target and policy-decision
  data were otherwise valid.
- **Replacement/current location:** Local claim derivation and wire construction
  now require an initial `RequestDocument` target to equal the owning job's exact
  job ID and canonical URL and to carry its exact policy decision. Initial robots
  behavior remains distinct: it may target the robots URL while retaining the
  source job's group, rate lineage, group scope, initial origin, and its own
  kind/target-specific decision. These boundaries are defined in normative
  [monotonic shared-rate semantics](crawl-jobs-v2.md#85-monotonic-shared-rate-semantics)
  and [claim and request transitions](crawl-jobs-v2.md#103-claim-and-request-transitions);
  current status is in the
  [F3 immediate work list](crawl-jobs-v2-plan.md#immediate-to-do-list).
- **Disposition:** Changed locally; focused and combined-package checks are
  supporting evidence only. Aggregate final-tree re-verification and independent
  re-review remain pending.
- **Specific reason:** Same-origin equality proves a rate/origin boundary, not
  the document identity owned by the claimed job. Accepting another path could
  claim one job while authorizing a different document and detach later output
  from its source identity; applying exact document identity to robots would, in
  contrast, break the intentionally separate robots target while adding no
  lineage protection.

### Detailed F3 planning ownership

- **Archive date:** 2026-09-10
- **Source document/section:** The former monolithic detail under
  [Parent plan — F3](spider-render-remediation-plan-2026-09-01.md#f3-add-durable-crawl-job-leases-and-recovery).
- **Superseded direction:** Keep F3 phases, ordered implementation work, test
  matrix, migration, rollback, and release details inside the F1-F6 parent plan.
- **Replacement/current location:**
  [Crawl Jobs V2 Implementation Plan — Document ownership](crawl-jobs-v2-plan.md#document-ownership);
  the parent retains only the F3 roll-up and dependency, while
  [Crawl Jobs V2](crawl-jobs-v2.md) owns normative protocol detail.
- **Disposition:** Moved and deduplicated.
- **Specific reason:** F3 changes more frequently and at greater detail than the
  program roll-up. Separate ownership prevents status duplication and drift and
  makes protocol precedence explicit.

### Dated foundation status as the current source of truth

- **Archive date:** 2026-09-10
- **Source document/section:**
  [Foundation Status — Purpose](crawl-jobs-v2-foundation-status-2026-09-07.md#purpose)
  and [Work still required at this checkpoint](crawl-jobs-v2-foundation-status-2026-09-07.md#work-still-required-at-this-checkpoint).
- **Superseded direction:** Maintain the 2026-09-07 dated foundation status as
  the current F3 status and remaining-work list.
- **Replacement/current location:**
  [Crawl Jobs V2 Implementation Plan](crawl-jobs-v2-plan.md), especially its
  [Current checkpoint](crawl-jobs-v2-plan.md#current-checkpoint),
  [Milestone status](crawl-jobs-v2-plan.md#milestone-status), and
  [Evidence log](crawl-jobs-v2-plan.md#evidence-log).
- **Disposition:** Reclassified as an immutable historical checkpoint; do not
  update it to mirror later mutable status.
- **Specific reason:** Later remediation, verification, and review findings
  postdate 2026-09-07. The checkpoint's statements and counts remain valid for
  that date, but using them as current status would omit subsequent work.

### Parent plan's all-proposed/all-pending posture

- **Archive date:** 2026-09-10
- **Source document/section:** The original posture summarized by
  [Parent plan — Findings summary](spider-render-remediation-plan-2026-09-01.md#findings-summary)
  and superseded in [Current status](spider-render-remediation-plan-2026-09-01.md#current-status).
- **Superseded direction:** Describe every F1-F6 remediation and all renderer
  stages as proposed or pending.
- **Replacement/current location:**
  [Parent plan — Current status](spider-render-remediation-plan-2026-09-01.md#current-status)
  and the [F3 current checkpoint](crawl-jobs-v2-plan.md#current-checkpoint).
- **Disposition:** Changed to a mixed implementation-status roll-up: F3 is in
  progress; F5 is implemented locally with protected acceptance pending;
  renderer Stages 0/1/2a are implemented but disabled; F1, F2, F4, and F6 remain
  not started; renderer Stage 3 remains unapproved.
- **Specific reason:** Local implementation and test evidence now exists for
  only part of the program. The original proposed/pending posture was a valid
  observation when recorded, but it no longer distinguishes completed local
  work from unmet acceptance or activation gates.

### F3 phase numbering

- **Archive date:** 2026-09-10
- **Source document/section:** The broad phase-numbered F3 sequence formerly
  embedded under
  [Parent plan — F3](spider-render-remediation-plan-2026-09-01.md#f3-add-durable-crawl-job-leases-and-recovery).
- **Superseded direction:** Track protocol, foundation, Lua, Redis acceptance,
  integration, migration, release, and crawl work as broad parent-plan phases.
- **Replacement/current location:**
  [F3 plan — Milestone status](crawl-jobs-v2-plan.md#milestone-status), which
  defines M0 through M8.
- **Disposition:** Renamed and restructured as F3 milestones M0-M8; parent-plan
  phases remain the separate cross-finding delivery sequence.
- **Specific reason:** The milestone model distinguishes completed dormant
  foundation work from independent review, authoritative Lua, real Redis,
  integration, migration/docs, immutable release, and separately authorized
  execution. This prevents a broad "phase complete" label from implying runtime
  or crawl readiness.

### Legacy three/four-image release composition

- **Archive date:** 2026-09-10
- **Source document/section:**
  [Legacy cutover — Historical V1 release preflight](immutable-pipeline-release-cutover.md#historical-v1-step-1-release-preflight-non-destructive)
  (four digest artifacts including PageRank) and
  [deploy the legacy pipeline](immutable-pipeline-release-cutover.md#historical-v1-step-4-deploy-the-legacy-pipeline-as-one-change-set).
- **Superseded direction:** Treat Spider, Indexer, and Image Indexer as the
  incompatible release set, with PageRank as the fourth preflight artifact.
- **Replacement/current location:** The exact V2 field set in
  [Compatibility manifests, candidate mode, and commit guard](crawl-jobs-v2.md#5-compatibility-manifests-candidate-mode-and-commit-guard)
  and the [M7 immutable release gate](crawl-jobs-v2-plan.md#m7-immutable-release-gate).
- **Disposition:** Expanded and replaced for V2. The manifest binds Spider, Seed
  Importer, crawl-admin, Indexer, Image Indexer, Backlinks Processor, Monitoring,
  and a disabled-or-digested Render Worker, together with exact protocol values,
  Redis configuration, contract markers, guard-core/commit-guard, and evidence
  digests. PageRank remains intentionally outside the V2 Redis participant set.
- **Specific reason:** V2 safety depends on every queue producer, consumer,
  administrator, and observer that can mutate or govern the Redis generation,
  not only the three legacy page/image services. The older composition remains
  historically correct for its narrower cutover scope.

### F5 Backlinks Processor status and behavior

- **Archive date:** 2026-09-10
- **Source document/section:**
  [Parent plan — F5 root cause and plan](spider-render-remediation-plan-2026-09-01.md#f5-make-backlink-persistence-acknowledge-before-removal).
- **Superseded direction:** Treat delete-first, retry-unsafe backlink processing
  and its proposed repair as wholly unimplemented.
- **Replacement/current location:**
  [Parent plan — F5 local implementation evidence](spider-render-remediation-plan-2026-09-01.md#local-implementation-evidence-2026-09-04)
  and [Current status](spider-render-remediation-plan-2026-09-01.md#current-status).
- **Disposition:** Changed locally to acknowledged, idempotent snapshot removal;
  acceptance and activation remain pending.
- **Specific reason:** The implementation now persists with insert/CAS and
  `$addToSet`, then removes only acknowledged members with `SREM`; crash replay
  and concurrent additions are safe in local tests. Thirty-one unit tests and
  seven disposable datastore integration tests passed, but a protected
  `required-tests` PR result is still required. The delete-first observation
  remains valid evidence of the pre-fix behavior.

### Renderer implementation status

- **Archive date:** 2026-09-10
- **Source document/section:** The earlier wholly proposed renderer posture now
  superseded in
  [JavaScript Crawling V1 Scope — Delivery Stages](javascript-crawling-v1-scope.md#delivery-stages)
  and [Parent plan — Current status](spider-render-remediation-plan-2026-09-01.md#current-status).
- **Superseded direction:** Describe all renderer stages as proposed and
  unimplemented.
- **Replacement/current location:**
  [JavaScript Crawling V1 Scope](javascript-crawling-v1-scope.md), including
  [Stage 0](javascript-crawling-v1-scope.md#stage-0-contract-and-fixtures),
  [Stage 1](javascript-crawling-v1-scope.md#stage-1-networkless-inline-rendering),
  [Stage 2](javascript-crawling-v1-scope.md#stage-2-brokered-scripts-and-stylesheets),
  and [Stage 3](javascript-crawling-v1-scope.md#stage-3-reviewed-data-requests).
- **Disposition:** Stages 0, 1, and 2a are implemented but disabled; Stage 3 is
  unapproved.
- **Specific reason:** Hermetic inline and brokered script/stylesheet capability
  and tests now exist. The checked-in render policy remains deny-all, browser
  activation is separately gated, and no public rendered crawl is authorized.
  The earlier proposed status remains a valid observation of its earlier date.

### Shared-fixture inventory counts

- **Archive date:** 2026-09-10
- **Source document/section:**
  [Foundation Status — Scope completed](crawl-jobs-v2-foundation-status-2026-09-07.md#scope-completed),
  which records 34 positive and 99 exact-class negative cases.
- **Superseded direction:** Use 34/99 as the current shared-fixture inventory.
- **Replacement/current location:**
  [F3 plan — Current checkpoint](crawl-jobs-v2-plan.md#current-checkpoint), which
  records 37 positive and 118 exact-class negative cases.
- **Disposition:** Changed current count to 37/118; retain 34/99 unchanged in the
  dated checkpoint.
- **Specific reason:** Later authority, policy-consumer, zero-sentinel, and
  conformance remediation added cases. The 34/99 count was an accurate
  observation on 2026-09-07, not an error; it is simply not the current
  inventory.

### Documentation replacement phase

- **Archive date:** 2026-09-10
- **Source document/section:** The former Phase 5 references in
  [V1 checklist status notice](v1-baseline-crawl-test-checklist.md#historical-v1-baseline-crawl-test-checklist)
  and [Environments — Seed catalog, feed, and bounded crawl](environments.md#seed-catalog-feed-and-bounded-crawl).
- **Superseded direction:** Replace active V1 operational documentation during
  parent-plan Phase 5.
- **Replacement/current location:**
  [Parent plan — Delivery sequence](spider-render-remediation-plan-2026-09-01.md#delivery-sequence),
  where integration is Phase 5 and documentation replacement/immutable
  packaging is Phase 6, aligned with
  [F3 M6](crawl-jobs-v2-plan.md#m6-migration-active-documentation-and-rollback).
- **Disposition:** Moved from parent Phase 5 to parent Phase 6.
- **Specific reason:** Active commands, expected states, rollback steps, report
  templates, and package manifests must be written from the accepted integrated
  V2 path. Separating integration from documentation/package replacement avoids
  publishing procedures before their executable path is tested.

### Readiness/report preparation versus authorization and execution

- **Archive date:** 2026-09-10
- **Source document/section:** The earlier combined release/readiness posture in
  the parent F3 planning detail and V1 checklist report preparation, now split by
  [F3 M7](crawl-jobs-v2-plan.md#m7-immutable-release-gate) and
  [F3 M8](crawl-jobs-v2-plan.md#m8-authorized-bounded-crawl).
- **Superseded direction:** Treat prepared release evidence, a reviewed report
  template, or a readiness finding as authorization to migrate, deploy, or run a
  bounded crawl.
- **Replacement/current location:**
  [M7 — Immutable release gate](crawl-jobs-v2-plan.md#m7-immutable-release-gate)
  prepares and reviews artifacts explicitly without granting authority;
  [M8 — Authorized bounded crawl](crawl-jobs-v2-plan.md#m8-authorized-bounded-crawl)
  requires a new explicit site/run authorization before bounded execution. See
  also the [parent authorization boundary](spider-render-remediation-plan-2026-09-01.md#authorization-boundary).
- **Disposition:** Separated into readiness/report preparation and a later,
  explicit authorization plus bounded execution decision.
- **Specific reason:** Technical readiness proves that a candidate could satisfy
  controls; it does not grant permission, define current site scope, or replace
  an unexpired run authorization. Historical reports remain evidence, not
  reusable authority.

### Draft PR creation versus merge and runtime authorization

- **Archive date:** 2026-09-10
- **Source document/section:**
  [Foundation Status — Branch and review status](crawl-jobs-v2-foundation-status-2026-09-07.md#branch-and-review-status)
  and [F3 M2 — Foundation checkpoint preservation](crawl-jobs-v2-plan.md#m2-foundation-checkpoint-preservation).
- **Superseded direction:** Treat creation of a draft PR, branch backup, or local
  passing checks as one combined gate for merge, candidate promotion, runtime
  activation, or crawl authorization.
- **Replacement/current location:** M2 governs reviewed checkpoint preservation;
  [M7](crawl-jobs-v2-plan.md#m7-immutable-release-gate) separately governs
  protected CI and immutable release acceptance; and
  [M8](crawl-jobs-v2-plan.md#m8-authorized-bounded-crawl) separately governs
  explicit run authorization and execution.
- **Disposition:** Separated. A draft PR may expose work for collaboration only;
  it neither approves merge nor grants runtime, migration, deployment,
  rendering, or crawl authority.
- **Specific reason:** A draft PR does not satisfy independent foundation review,
  protected branch checks, final Lua/Redis acceptance, cross-finding dependencies,
  immutable artifact review, or site/run authorization.

### Service-local V1 commands as current operational guidance

- **Archive date:** 2026-09-10
- **Source document/section:** Mutation, startup, deployment, reconciliation, and
  publication examples retained in the [Seed Importer](../services/seed-importer/README.md),
  [Backlinks Processor](../services/backlinks-processor/README.md),
  [Indexer](../services/indexer/README.md),
  [Image Indexer](../services/image-indexer/README.md),
  [Spider](../services/spider/README.md),
  [PageRank](../services/page-rank/README.md), and
  [Query Engine](../services/query-engine/README.md) service documentation.
- **Superseded direction:** Present service-local V1 commands or the retired V1
  cutover runbook as current mutation, deployment, publication, or cleanup
  instructions while F3 foundation or Lua work is in progress.
- **Replacement/current location:** The explicit no-start/no-mutation boundary in
  [F3 non-negotiable boundaries](crawl-jobs-v2-plan.md#non-negotiable-boundaries),
  followed by tested active-document replacement in
  [F3 M6](crawl-jobs-v2-plan.md#m6-migration-active-documentation-and-rollback)
  and parent-plan Phase 6.
- **Disposition:** Changed to historical or separately approved isolated-V1
  examples. Harmless local validation and test commands remain available, but
  the examples do not authorize retained-state mutation, service startup,
  release promotion, or a crawl.
- **Specific reason:** A service README cannot override the current WIP/no-merge
  gate, the dormant F3 phase boundary, or the permanent retirement of the V1
  crawl procedure. Keeping commands for technical context is useful only when
  their non-authoritative status is unambiguous.

### Seed target definition versus retained datastore state

- **Archive date:** 2026-09-10
- **Source document/section:** [Seed Sources — Manual Seeds](seed-sources.md#manual-seeds)
  and the checked-in target catalog used by the Seed Importer.
- **Superseded direction:** Describe the intended 67-enabled/3-disabled catalog
  as if it were already the current retained datastore state.
- **Replacement/current location:** The target-versus-observed distinction in
  [Seed Sources](seed-sources.md#manual-seeds) and the retained-state evidence and
  acceptance gate in [parent-plan F1](spider-render-remediation-plan-2026-09-01.md#f1-reconcile-the-retained-seed-catalog).
- **Disposition:** Changed to identify 67/3 as the checked-in F1 target while the
  inspected retained datastore remains at 70 enabled records until F1 executes.
- **Specific reason:** Configuration intent is not deployment evidence. Treating
  the target as current state would conceal the exact F1 mismatch that must be
  reconciled and independently verified before any feed or crawl.

### Monitoring role and implementation description

- **Archive date:** 2026-09-10
- **Source document/section:** The former placeholder description in the
  [Monitoring README](../services/monitoring/README.md) and the earlier tendency
  to treat current Monitoring as a passive observer.
- **Superseded direction:** Describe the V1 Monitoring process as unimplemented,
  read-only, or suitable for reuse as the Crawl Jobs V2 monitor.
- **Replacement/current location:** The implemented V1 scaler description in the
  [Monitoring README](../services/monitoring/README.md), future integration in
  [F3 M5](crawl-jobs-v2-plan.md#m5-runtime-and-consumer-integration), and the
  normative [V2 Monitoring contract](crawl-jobs-v2.md#15-monitoring-contract).
- **Disposition:** Changed: the current V1 process reads backlog state and invokes
  Compose scaling commands, remains blocked during foundation/Lua work, and must
  not be reused as the required read-only V2 Monitoring implementation.
- **Specific reason:** Calling the existing scaler a placeholder understates
  active side effects; calling it a monitor overstates compatibility. V2 requires
  explicit run-scoped, bounded, read-only observations and separate startup
  authority.

### Query Engine compatibility-manifest membership

- **Archive date:** 2026-09-10
- **Source document/section:** The former parent-plan Phase 6 packaging language
  in [Delivery sequence](spider-render-remediation-plan-2026-09-01.md#delivery-sequence),
  which grouped Query Engine with Redis protocol participants.
- **Superseded direction:** Add Query Engine to the Crawl Jobs V2 compatibility
  manifest because it exposes F6 provenance.
- **Replacement/current location:** Exact manifest membership in
  [Crawl Jobs V2 section 5](crawl-jobs-v2.md#5-compatibility-manifests-candidate-mode-and-commit-guard)
  and corrected packaging guidance in the
  [parent delivery sequence](spider-render-remediation-plan-2026-09-01.md#delivery-sequence).
- **Disposition:** Changed. Query Engine remains a separately promoted
  application artifact for F6 API/UI work, not a protocol-defined V2 manifest
  image field. Monitoring is a required manifest participant and cannot be
  omitted at cutover.
- **Specific reason:** Planning shorthand cannot add or remove members from the
  digest-bound normative field set. Separating the application artifact from
  Redis participants preserves exact manifest validation without dropping F6
  release coordination.

### NewsAPI use of active sources and an existing feeder cadence

- **Archive date:** 2026-09-11
- **Source document/section:** The former goal and scheduled-run phases in the
  [NewsAPI Seed Integration Plan](newsapi-integration-plan.md).
- **Superseded direction:** Treat Hacker News and Wikipedia external links as
  actively ingested today, have Spider and Render Worker fetch article content,
  and attach NewsAPI to an existing feeder cadence after plan-tier approval.
- **Replacement/current location:** The deferred status, network-role boundary,
  and future feeder design in the
  [NewsAPI plan](newsapi-integration-plan.md), governed by the
  [parent plan](spider-render-remediation-plan-2026-09-01.md) and
  [F3 plan](crawl-jobs-v2-plan.md).
- **Disposition:** Changed to a proposed discovery source only. Documented source
  lists are not evidence of active ingestion; Spider owns approved network
  fetches, Render Worker remains networkless, and no V1 feeder schedule may be
  reused.
- **Specific reason:** F1/F2/F4 are not started, F3 M1 is NO-GO, and the V1
  feeder is blocked during foundation work. The previous wording overstated
  current ingestion and assigned direct fetching to a service that has no
  network authority.

### Seed bootstrap as a guaranteed 67/3 reconciliation

- **Archive date:** 2026-09-11
- **Source document/section:** The former Development baseline result in the
  [Seed Importer README](../services/seed-importer/README.md#development-baseline).
- **Superseded direction:** State that every `bootstrap` run necessarily leaves
  67 enabled and three disabled records.
- **Replacement/current location:** The qualified bootstrap and guarded-rebuild
  behavior in the
  [Seed Importer README](../services/seed-importer/README.md#development-baseline)
  and target-versus-observed distinction in
  [parent-plan F1](spider-render-remediation-plan-2026-09-01.md#f1-reconcile-the-retained-seed-catalog).
- **Disposition:** Changed: an empty catalog receives the checked-in 67/3 target,
  while bootstrap preserves operator-controlled states in an existing compatible
  catalog; only the guarded rebuild necessarily applies the target.
- **Specific reason:** The retained datastore remains at 70 enabled records.
  Claiming bootstrap always reconciles it would hide the exact F1 mismatch and
  contradict the implementation's deliberate state-preservation behavior.

### Query Engine service Compose configuration through `.env`

- **Archive date:** 2026-09-11
- **Source document/section:** The former Docker setup instructions in the
  [Query Engine README](../services/query-engine/README.md#using-docker).
- **Superseded direction:** Tell operators to create
  `services/query-engine/.env` from `.env.example` to configure the service-local
  Compose stack and claim that `.env.example` enters the Docker build context.
- **Replacement/current location:** The actual local-configuration description
  in the [Query Engine README](../services/query-engine/README.md#using-docker)
  and its Frontend Dependency Security section.
- **Disposition:** Removed as unsupported guidance. The service Compose file
  currently hardcodes local development values and has no `env_file`; the Docker
  ignore rules exclude `.env`, every `.env.*` file, and `.env.example`.
- **Specific reason:** Editing a file that Compose does not consume has no effect
  and can give a false impression that credentials or endpoints were changed.
  Documentation must describe the checked-in configuration rather than an
  unimplemented loading path.

### Image Indexer as an external image fetcher

- **Archive date:** 2026-09-11
- **Source document/section:** The former Image Indexer paragraphs in the
  [root README](../README.md) and
  [Environments — isolated test local](environments.md#isolated-test-local-historical-v1-procedure-blocked).
- **Superseded direction:** Describe the current Image Indexer as fetching
  externally supplied image URLs and waiting for a future SSRF-hardened fetch
  path.
- **Replacement/current location:** The historical/current distinction in
  [Environments](environments.md) and the current
  [Image Indexer contract](../services/image-indexer/README.md).
- **Disposition:** Changed: external image fetching remained out of scope for the
  historical baseline, and the current Image Indexer performs no HTTP requests
  or image-byte decoding; it reconciles Spider-authorized metadata.
- **Specific reason:** The old wording described an obsolete architecture in
  current tense and obscured the present no-network trust boundary. The service
  remains blocked for the historical baseline and has no V2 startup authority.

### Implicit Python `.env` loading for Indexer and TF-IDF

- **Archive date:** 2026-09-11
- **Source document/section:** The former non-Docker configuration steps in the
  [Indexer README](../services/indexer/README.md#without-docker-isolated-v1-development-only)
  and [TF-IDF README](../services/tfidf/README.md#without-docker).
- **Superseded direction:** Suggest that creating a service-local `.env` file is
  sufficient for `python main.py` to receive datastore configuration.
- **Replacement/current location:** The corrected process-environment steps in
  the same Indexer and TF-IDF README sections.
- **Disposition:** Removed as unsupported implicit behavior. Operators must
  export variables into the process environment or explicitly source a safe
  isolated-development file before invocation.
- **Specific reason:** Both Python runtimes read `os.getenv` and include no
  dotenv loader. An unexported `.env` file would be ignored, leading to defaults,
  failed startup, or connection to an unintended endpoint.
