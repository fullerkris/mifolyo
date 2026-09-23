# Spider and Render Worker Remediation Plan

**Original review date:** 2026-09-01

**Last updated:** 2026-09-23 (UTC; M4-P3 bootstrap/ACL Step 4 arm64 PC01 validation PASS)

**Reviewed baseline:** `main` / `44d8b09a364a1f60032e1f4faccf160813f4dd04`

**Status:** In progress; no crawl, migration, deployment, or rendering activation
is authorized

**Scope:** Spider, Render Worker, crawl policy, crawl queue, Backlinks Processor,
and static indexing behavior

This is the single mutable implementation plan and status roll-up for findings
F1 through F6. It began as a read-only review of the retained isolated test
state. No later implementation evidence recorded here authorizes a crawl or
rendering activation.

## Document ownership

- This file owns the current F1 through F6 status, sequencing, dependencies,
  and overall acceptance gate.
- [`crawl-jobs-v2-plan.md`](crawl-jobs-v2-plan.md) is the single mutable F3
  implementation plan. This file retains only the F3 roll-up and dependency.
- [`crawl-jobs-v2.md`](crawl-jobs-v2.md) is the digest-bound normative F3
  protocol. It is not a mutable project plan and takes precedence over either
  plan if wording differs.
- [`javascript-crawling-v1-scope.md`](javascript-crawling-v1-scope.md) defines
  the renderer's technical capability and rollout boundary. Mutable stage
  status is summarized here.
- [`crawl-jobs-v2-foundation-status-2026-09-07.md`](crawl-jobs-v2-foundation-status-2026-09-07.md)
  is a dated F3 checkpoint, not the current plan.
- [`scrapped&ChangedPlannings.md`](scrapped&ChangedPlannings.md) is the
  non-authoritative archive for superseded planning decisions. It does not
  override this plan or any document that owns current status or protocol.

The historical test report at
[`v1-baseline-crawl-test-report-2026-08-18.md`](v1-baseline-crawl-test-report-2026-08-18.md)
remains immutable evidence of its original strict **FAIL**. Do not revise that
report as remediation work progresses. Record later evidence in a new dated test
report.

## Authorization boundary

Section 7 of
[`v1-baseline-crawl-test-checklist.md`](v1-baseline-crawl-test-checklist.md) is
permanently retired as an execution path. Completing Findings 1 through 6 does
not reactivate that V1 step. A future bounded crawl may occur only through F3
M8 after every parent-plan gate passes and a new site/run authorization is
recorded. A public rendered crawl also remains subject to every gate in
[`javascript-crawling-v1-scope.md`](javascript-crawling-v1-scope.md).

The checked-in policies are technical limits, not permission to access a site.
The authorization consumed on 2026-08-18 cannot be reused.

## Change-control governance

- Remaining remediation work is WIP and needs separate merge authorization;
  accepted PR #9/#10 checkpoints and their limited scope are recorded below.
  Local verification is supporting evidence, not protected-main acceptance.
- Protected-main acceptance requires the exact reviewed commit to pass the
  protected `required-tests` PR context and every applicable plan gate.
- A draft F3 PR may be opened or updated only after the scoped verification,
  secret scan, commit, and push sequence in M2 of
  [`crawl-jobs-v2-plan.md`](crawl-jobs-v2-plan.md) is complete and local/remote
  commit identity is verified. Repeat that sequence before each draft update.
- Opening or updating a draft PR does not authorize merge, authoritative Lua
  work, runtime activation, migration, deployment, rendering, or crawling.

## Read-only evidence snapshot

The retained state was inspected on 2026-09-01 without mutation.

| Evidence | Observed state |
|---|---|
| Spider container | Not created or running in `mifolyo-v1-baseline-test` |
| Render Worker container | Not created or running in `mifolyo-v1-baseline-test` |
| Crawl policy | SHA-256 `50648954d0264f7ac4fdda174178db488e86e335a0b63fdcc448da7bc218bae3` |
| Render policy | SHA-256 `c056088aac41aa52aa2555b2feb8815ef360585f6c8db0cb804029d5e1ae4ea6`; default deny with zero rules |
| Mongo seed catalog | 70 total, 70 enabled, 0 disabled |
| Expected seed catalog | 70 total, 67 enabled, 3 disabled |
| Redis V1 queue | 261 pending: 64 at depth 0 and 197 at depth 1 |
| Redis V1 metadata | 267 URL mappings and 267 depth mappings |
| Claimed hash-only identities | 6 |
| Searchable metadata | 2 pages: `https://go.dev/doc/` and `https://www.loc.gov/` |
| Non-indexable fetched shell | `https://fonts.google.com/` |
| Root development data stores | MongoDB, Redis, and PostgreSQL currently publish loopback host ports |

The queue and mapping counts match the post-run state recorded on 2026-08-18.
They are not a fresh 67-seed baseline. The six hash-only identities are the three
fetched seeds plus BBC News, Khan Academy, and PolitiFact. The last three are no
longer queued and are denied by the current disabled policy group, but the
retained Mongo records still incorrectly say `enabled: true`.

## Findings summary

| ID | Finding | Severity | Gate |
|---|---|---|---|
| F1 | Retained catalog has 70 enabled seeds instead of 67 enabled and 3 disabled | High | Data reconciliation required before feeding |
| F2 | Retained Redis state is a post-test queue, not a fresh baseline | High | Disposable project reset required before feeding |
| F3 | Spider claims have no durable lease, retry, or dead-letter recovery | Blocker | Crawl-job protocol change required before another crawl |
| F4 | Enabled host and path scope is broader than the reviewed seed list | Blocker | Narrowed policy contract and review required before another crawl |
| F5 | Historical Backlinks Processor delete-first persistence was retry-unsafe | Blocker | Code repair passed protected PR #9 checks and merged; retained-state repair and V2 integration remain separately gated |
| F6 | Static paragraph-only extraction cannot index JavaScript application shells | High | Metadata fallback, replacement seed, or separately authorized rendering decision required |

## Current status

| Item | Status | Current evidence / next gate |
|---|---|---|
| F1 seed reconciliation | Not started | Retained evidence remains 70 enabled records; execute only during the matched freeze/backup/reset sequence |
| F2 disposable Redis reset | Not started | Retained V1 state remains historical post-test evidence and must not be reused or selectively repaired |
| F3 durable Crawl Jobs V2 | M1/M2 through PR #9, dormant M3/M4 preparation through PR #10 and claim checkpoint through PR #11 merged; historical smoke/claim real cases PASS | Bootstrap/ACL review GO and fresh arm64 PC01 image/artifact validation PASS; fresh branch preserves pending work; scoped publication/protected CI next; evidence lives in [`crawl-jobs-v2-plan.md`](crawl-jobs-v2-plan.md) |
| F4 exact crawl scope | Not started | No crawl-policy V2 schema or approved exact-URL policy is present |
| F5 backlink persistence | Code acceptance passed and merged | PR #9 passed protected checks and merged as `d914a93`; no retained datastore reconciliation or V2 consumer activation is implied |
| F6 JavaScript-shell indexing | Not started | No static-extraction policy schema or approved metadata-fallback configuration is present |
| Render Stages 0, 1, and 2a | Implemented; disabled | Hermetic inline and brokered rendering exists, but the checked-in render policy remains deny-all and public rendering is unauthorized |
| Render Stage 3 | Not approved | Requires a reviewed protocol extension, shadow evidence, exact policy, and all F3/F4/F6 activation gates |

## F1: Reconcile the retained seed catalog

### Root cause

The retained isolated volume contains the catalog used by the 2026-08-18 run,
when all 70 direct records were enabled. Current source construction correctly
defines BBC News, Khan Academy, and PolitiFact as disabled, but bootstrap logic
preserves existing operator state. A normal bootstrap or feeder replay therefore
does not correct this retained collection.

### Target state

- Exactly 70 schema V1 records exist.
- Exactly 67 records have `enabled: true`.
- BBC News, Khan Academy, and PolitiFact have `enabled: false`.
- The validator, required indexes, canonical identities, and provenance pass the
  existing seed-importer checks.

### Plan

1. Close Caddy ingress, stop the Query Engine, and freeze every other isolated
   writer: Spider, seed feeder, Indexer, Backlinks Processor, Image Indexer,
   PageRank, and TF-IDF. Record their final states and verify no one-off writer
   remains. Keep only the data stores needed for logical backup running.
2. Create one named matched evidence set during that freeze. It contains a full
   `mifolyo_index` MongoDB dump and Redis persistence snapshot, both stored
   outside project-scoped volumes with recorded SHA-256 checksums and locations.
   Also retain a collection-level `crawl_seeds` dump from the same freeze for
   F1-only rollback. Prove the full pair and collection dump restore in a
   separate inspection context before mutation.
3. Run the existing guarded rebuild dry-run from section 4 of the baseline
   checklist.
4. Confirm the dry-run reports 70 direct records, 67 enabled, 3 disabled, and 8
   Reddit discovery rows excluded.
5. Run the guarded rebuild only against the isolated
   `mongo:27017/mifolyo_index/crawl_seeds` target.
6. Capture the exact disabled URL set, schema versions, validator, and index
   names after replacement.

No application code change is required for this local reconciliation. A shared
or production catalog would require a separate conditional migration of the
three exact IDs rather than the test-only rebuild command.

### Acceptance gate

- [ ] MongoDB reports `total=70`, `enabled=67`, and `disabled=3`.
- [ ] The disabled set is exactly BBC News, Khan Academy, and PolitiFact.
- [ ] All 70 records remain schema and canonicalization version 1.
- [ ] Caddy ingress was closed and Query Engine plus every other isolated writer
  remained stopped for the entire backup and rebuild.
- [ ] Matched MongoDB and Redis backups exist outside project volumes, have
  recorded checksums, and passed a separate restore rehearsal.

### Rollback

Stop the isolated services and restore the collection-level `crawl_seeds` dump
from the named matched evidence set. Do not restore the old catalog into the new
baseline after Finding 2 is completed.

## F2: Replace the retained post-test Redis state

### Root cause

Cleanup was intentionally not performed after the historical test. The feeder
is additive: it can restore enabled seeds and remove explicitly disabled seed
IDs, but it cannot prove that 197 discovered jobs, claimed identities, page
publications, image publications, backlink sets, and signal entries together
represent a fresh run.

### Target state

- Before feeding, every crawl and downstream queue in the isolated project is
  empty.
- After feeding, the ready queue, URL map, and depth map contain the same 67
  enabled seed IDs.
- Every initial depth is canonical string `0`.
- No discovered, hash-only, processing, dead-letter, image, backlink, or signal
  residue exists from the historical run.

### Plan

1. Complete F1 and preserve the historical Redis counts in the mandatory matched
   MongoDB/Redis backup set created while every writer is frozen.
2. Do not use `FLUSHDB`, `FLUSHALL`, generic volume pruning, or selective key
   deletion as a reset.
3. Verify the backup set is outside all project-scoped volumes, compare its
   checksums with the recorded values, and complete the restore rehearsal.
4. Stop the fixed `mifolyo-v1-baseline-test` project.
5. Use only the project-restricted cleanup command in `docs/environments.md`:

   ```bash
   docker compose \
     --project-name mifolyo-v1-baseline-test \
     --file scripts/docker/v1-baseline.compose.yml \
     down --volumes --remove-orphans
   ```

6. Recreate the isolated core, rebuild the catalog, and feed the accepted crawl
   job protocol once.
7. Fail the preflight instead of repairing in place if any count or ID set is
   larger than the reviewed enabled-seed set.

The historical V1 queue must not be migrated into the new baseline. A future
production queue migration belongs to F3 and requires stop-and-drain evidence.

### Acceptance gate

- [ ] Every writer was frozen before the matched backup and remained stopped
  until cleanup completed.
- [ ] The matched backup is stored outside project volumes and passed checksum
  and restore verification before `down --volumes`.
- [ ] All isolated pipeline queue counts are zero before feeding.
- [ ] The enabled Mongo ID set equals the queue, URL-map, and depth-map ID sets.
- [ ] Each structure contains exactly 67 IDs after feeding.
- [ ] Every depth is `0` and every URL is the expected canonical seed URL.
- [ ] No legacy V1 discovered or hash-only identity was copied forward.

### Rollback

Keep the named full MongoDB/Redis matched evidence set in a separate inspection
context. Never restore it over a newly accepted baseline.

## F3: Add durable crawl-job leases and recovery

The root blocker remains the V1 queue's lack of durable in-flight state,
fencing, retry recovery, terminal dispositions, and atomic publication. The
target remains an at-least-once run/job state machine with Redis-time leases,
request reservations, bounded staging, and one lease-fenced atomic commit.

[`crawl-jobs-v2-plan.md`](crawl-jobs-v2-plan.md) now owns the complete mutable F3
implementation plan, current checkpoint, ordered work list, test matrix,
integration sequence, migration, rollback, and release gates.
[`crawl-jobs-v2.md`](crawl-jobs-v2.md) remains the normative authority for exact
wire grammar, limits, transitions, records, Redis configuration, and evidence.

### Current roll-up

- The digest-bound protocol, shared fixture, independent Go/Python verifier, and
  dormant Go foundation were merged through PR #9 as `d914a93`. Current M3 work
  uses `feature/crawl-jobs-v2-lua`; its scoped checkpoint was committed and pushed
  as `81028ca12a1763d46df72fc54759d99a0ea3b561` on 2026-09-18, with matching
  local/remote identity. No M3 PR was created or merge/activation authorized.
- The initial WIP checkpoint is commit
  `e4372a66201b8767bcca4d7476c30c5b7999922c`. The reviewed amendment and foundation
  remediation are preserved in remote-verified checkpoint
  `0989001d15c9a84a00464fd55ddd857650eda85e`.
- The project owner explicitly approved the 2026-09-11 preactivation
  transcript/stage protocol amendment and dormant implementation. The amended
  contract specifies authenticated genesis/terminal generation, atomic BEGIN
  freeze, and exact seal/commit/replay binding without changing downstream
  semantic output/publication grammar.
- The **2026-09-11 historical foundation** pinned-Go-1.25.13 aggregate passed
  normal, race, shuffled, full Spider, vet, build, Python fixture/syntax,
  formatting and scoped diff checks. Its coverage was 79.0%, with 39 named
  positives plus baseline and 139 negatives; these are not current M3 counts or
  coverage. Evidence-type limits remain in the F3 plan.
- Foundation BEGIN output-preparation, exact-witness, abort/expiry/delivery, and
  test-oracle findings were fixed and independently replayed. Final correctness,
  conformance, and defensive reviews each returned scoped GO, with no outstanding
  findings from those reviews.
- M1/M2 passed protected PR #9 checks and merged. On 2026-09-15 the owner
  authorized dormant M3 implementation and the narrow existing-wire/renewal-clock
  clarification. That date's initial BOOT-only slice (1/43), pure primitives and
  private reload helpers passed scoped tests and code/protocol review; the dated
  evidence is retained in the F3 plan, not presented as current completion.
- **Current M3:** All 43 canonical sources and the complete sealed, zero-argument
  `AuthoritativeScriptBindingSet()` factory are implemented and merged through
  PR #10 as `ff2457e`, still dormant. Dated normal/Docker/full-module race evidence
  and independent source reviews are preserved in the primary plan. All fourteen
  protected checks passed on `a02991c`; eight race reports account for all 470
  roots with 469 passes and the one permitted optional native-Lua skip.
- Activation is blocked by the remaining gates, not an absent bundle. M4's four
  fixture proposals were approved September 21; the normative amendment and
  offline compiler/tests and first ledger-smoke executor are implemented locally.
  The original four findings and a closed-peer follow-up are now corrected.
  Independent final correctness/security re-review returns GO for image
  preparation only. Subsequent immutable image validation and exact-revision CI
  pass. The first approved bounded attempt failed in init before Redis startup;
  cleanup was verified and the one-case approval is used.
  Its capability-spelling defect is now corrected and independently re-reviewed;
  revised-image validation and new checkpoint CI pass. After separate owner
  authorization, the corrected ledger-smoke case passes with actual Redis
  probe/BOOT/maintenance/ACL/revocation evidence and all six resources confirmed
  absent. Full M4 remains open; this small case does not authorize runtime
  integration, retained datastore mutation, application/service activation,
  migration, deployment, candidate promotion or crawling.

### Parent-plan acceptance gate

- [x] The final independent foundation review permits dormant authoritative Lua
  authoring.
- [x] All 43 canonical Lua sources and the sealed zero-argument factory are
  implemented locally and pass normal in-memory acceptance.
- [x] Record the final current-tree full-module M3 race pass.
- [ ] The exact Lua transitions and sealed source bundle pass real Redis 7
  conformance after reviewed execution-harness completion and explicit M4 approval.
- [ ] Lease, retry, staging, commit, crash, AOF, memory, and maximum-shape gates
  pass with recorded evidence.
- [ ] Spider, feeder, downstream consumers, Monitoring, Compose, migration, and
  crawl-admin integration pass without a mixed V1/V2 path.
- [ ] The active V2 checklist, release packaging, stop-and-drain rehearsal,
  rollback evidence, and explicit authorization are complete.

### Rollback boundary

Before the first successful `CJ2_START_REQUEST`, restore the matched V1
snapshot, old release, and old credential set only while all writers are stopped.
That request-start transition, recorded before DNS, is the irreversible boundary
for ordinary rollback. Never
point a V1 Spider at V2 keys or run both generations.

## F4: Narrow host, path, redirect, and discovery scope

### Root cause

Every enabled V1 host rule uses `apex_and_subdomains`; the listed hostname and
all descendants are therefore eligible. Enabled groups have empty path
allowlists, which means all unambiguous paths and queries are allowed. Redirect
mode is `same_group`, so a redirect can cross between unrelated hosts in the
same category.

### Target state

- Every enabled seed uses its exact canonical hostname.
- Initial policy admits only reviewed exact canonical seed URLs, including an
  explicit query decision, and explicit directed redirect destinations.
- Redirects remain on the same host unless a specific cross-host transition is
  separately reviewed.
- Initial validation uses depth 0. Depth-1 discovery is enabled later per site,
  with segment-bounded path prefixes and explicit evidence.
- Disabled rules may remain broad because their only result is denial.

### Plan

1. Add a crawl-policy V2 contract that supports exact canonical URL rules on
   each host rule while retaining group-level runtime budgets. Query matching is
   deny by default; an allowed query must be represented exactly in a URL rule.
2. Generate the initial baseline policy from the 67 canonical enabled seed URLs
   so host and path scope cannot drift independently from the catalog.
3. Use `match: exact`, exact path-and-query URL rules, `max_depth: 0`, and no
   general redirect permission for initial validation.
4. Represent each required redirect as a directed edge from one exact canonical
   source URL to one exact canonical destination URL. Do not broaden an entire
   host or category group to accommodate one transition.
5. Add per-site reviewed path prefixes only after robots, terms, query behavior,
   user-generated areas, and GET side effects are assessed.
6. Pin and record the new policy digest. Keep V1 policy only for rollback
   inspection; baseline execution must fail if V1 is supplied after cutover.
7. Send jobs denied by a changed policy to the F3 dead-letter state with stable
   reason `policy_scope_changed`; do not leave them pending forever.

Primary implementation areas are `contracts/crawl-policy-v2.schema.json`,
`services/spider/internal/crawlpolicy`, Spider baseline validation, seed policy
contract tests, and both Compose files.

### Required tests

- All 67 exact seed URLs match at depth 0.
- BBC News, Khan Academy, PolitiFact, Reddit, and unmatched hosts are denied.
- Synthetic descendant subdomains are denied.
- Unlisted paths and every depth-1 URL are denied in the initial policy.
- Every arbitrary query suffix on an allowed path is denied. An exact query is
  admitted only when that complete canonical URL is explicitly reviewed.
- Cross-host same-category redirects are denied.
- Unlisted same-host redirects and reversed directed edges are denied.
- `/robots.txt` remains fetchable only through the existing robots-specific
  authorization path.

### Acceptance gate

- [ ] The enabled policy contains zero `apex_and_subdomains` rules.
- [ ] Every enabled rule has explicit exact path-and-query URL scope; query
  handling never falls through to path-only authorization.
- [ ] Every allowed redirect is a reviewed directed exact-URL edge.
- [ ] Seed and policy generation produces a deterministic reviewed digest.
- [ ] All denial tests pass before DNS or browser fulfillment.
- [ ] A site-by-site scope inventory and authorization record exists.

### Rollback

Restore policy and Spider image together with all crawling stopped. Returning to
the broad V1 policy is not permission to resume crawling; keep crawling disabled
until a forward fix is approved.

## F5: Make backlink persistence acknowledge before removal

### Historical root cause

Before the local repair, the Backlinks Processor read Redis sets, deleted their
whole keys, and then wrote to MongoDB. A MongoDB error or process exit could
lose the snapshot. A new member added after `SMEMBERS` but before `DEL` could
also be removed without being included in the persisted batch. At that time,
the service had no behavioral test suite in CI.

### Current implementation status

The idempotency repair described below passed protected PR #9 checks and merged
as `d914a93`. This is code acceptance, not retained-data reconciliation, V2
consumer integration, or permission to start the pipeline. The dated local
evidence below preserves the earlier checkpoint status.

### Target state

Backlinks remain an idempotent additive projection. MongoDB acknowledges a
bounded snapshot before Redis removes only the exact persisted members.
Concurrent additions remain pending.

### Implemented repair design

The repair's code and automated tests passed protected PR #9 acceptance. The
requirements below also include operational prerequisites and retained-data
reconciliation, which have not been executed or accepted by that code merge:

1. Stop the current Backlinks Processor before any producer resumes.
2. Replace every `KEYS backlinks:*` path, including Monitoring, with `SCAN`.
   Use a count hint of 100 and process at most 100 target keys per cycle. Redis
   `COUNT` is a hint rather than a hard response bound or point-in-time
   snapshot; reject an unexpectedly large response, retain all source work, and
   rely on the container memory limit as the final wire-response guard.
3. Read set members with `SSCAN`, subject to the same cursor semantics. Split
   admitted results into persistence/ACK batches of at most 256 members and 512
   KiB of encoded URL data. Reject members above the canonical 2,048-byte URL
   limit and preflight the resulting MongoDB document below a 12 MiB operational
   ceiling, leaving rejected or oversized work in Redis with a stable alert
   reason. Partial pages and retry snapshots share an 8 MiB raw-member budget;
   reserve a full admissible page before scanning and use interruptible
   backpressure while buffered work drains. This is not a total-RSS bound.
4. Persist a new target with an insert-only exact document, or update an
   existing target with one exact-document-CAS `$addToSet: {$each: [...]}`
   operation. Require an acknowledged result in either case.
5. After acknowledgment, use `SREM` for only the members in that snapshot. Do
   not `DEL` the key.
6. Treat a crash after MongoDB and before `SREM` as a safe replay; `$addToSet`
   keeps it idempotent.
7. Add datastore authentication policy and bounded connection/read/write
   timeouts consistent with the Spider and newer consumers.
8. Add unit, Redis integration, Mongo integration, concurrent-add, failure, and
   graceful-shutdown tests. Promote Backlinks Processor from dependency-audit
   only to a required behavioral CI job.
9. Add an offline reconciliation command that backfills missing edges from
   authoritative MongoDB `outlinks` into the additive historical backlink
   projection. It must not claim exact equality or remove stale historical
   edges; PageRank continues to use `outlinks` as its authority.

Primary implementation areas are `services/backlinks-processor/main.py`,
`services/backlinks-processor/data/redis_client.py`,
`services/backlinks-processor/data/mongo_client.py`, its README, new tests, and
`.github/workflows/unit-tests.yml`. Monitoring's backlog scan in
`services/monitoring/src/main.rs` is part of F5.

### Acceptance gate

Checked items record the merged PR #9 code and automated-test evidence, not
acceptance of unrelated local edits or retained-state reconciliation. The dated
local results below preserve their original, pre-merge scope.

- [x] MongoDB failure removes zero Redis members.
- [x] A crash after MongoDB acknowledgment creates no duplicate edge.
- [x] A member added concurrently after the snapshot remains in Redis.
- [x] No production path issues `DEL` for `backlinks:*`.
- [ ] Every current outlink edge has the expected reverse backlink after
  additive reconciliation; extra historical backlinks are permitted and
  reported.
- [x] Key, member, byte, and MongoDB document bounds are enforced and tested.
- [x] Behavioral tests are required by protected CI.

### Local implementation evidence (2026-09-04)

- `python3 -B -m unittest discover -s tests -v`: 31 unit tests passed; seven
  integration tests are intentionally skipped without datastore addresses.
- With disposable Redis 7 and MongoDB 8 instances, all seven integration tests
  passed, covering concurrent-add retention, multi-page cursor processing,
  crash/replay and ambiguous-ACK idempotency, insert/CAS conflicts, and additive
  reconciliation.
- `cargo fmt --check` and `cargo check --locked` passed for Monitoring after its
  cursor-based backlog scan replaced `KEYS`.
- `.github/workflows/unit-tests.yml` now provides both datastores and refuses
  skipped Backlinks Processor integration tests. The same suite is part of the
  branch-protected `required-tests` context. A passing protected PR run is still
  required before this acceptance gate is complete.

### Local follow-up verification (2026-09-14)

- Closed the aggregate retry-buffer memory gap and bounded Monitoring to at
  most one SCAN and 100 accounted keys per tick. Monitoring drains accepted
  pages of up to 1,024 keys across ticks and never publishes a partial or failed
  scan as zero. Both paths retain their cursors on rejected oversized replies.
- Integration tests now require Redis DB15 before connecting and process only
  UUID-namespaced target keys. A sentinel proves unrelated backlink work stays
  untouched. The multi-page test forces hashtable encoding and checks a nonzero
  SSCAN cursor instead of assuming COUNT is a hard bound.
- All **53 Backlinks Processor tests passed with zero skips**: 46 unit tests and
  7 real-datastore tests. The integration/isolation module also passed five
  sequential repeats and two concurrent runs. Coverage includes applied writes
  with lost replies, unacknowledged writes, write-concern failures, bounded
  outage/recovery buffering, and completion of an already-started shutdown ACK.
- Tests ran on Linux/arm64 with Python 3.13.14, pymongo 4.18.1, redis-py 8.1.0,
  and idna 3.18. The non-root, read-only test runner had a 384 MiB memory limit.
  Redis 7 and MongoDB 8 used a new internal network, no published ports, and
  tmpfs-only datastore storage. Test keys/databases were checked empty after
  cleanup; the test containers and network were removed.
- Redis image: `redis@sha256:ff02b58f971e7d7d156a1267e283fcbbeee91773b6aa36c49dac28ecfe28eadf`.
- MongoDB image: `mongo@sha256:81a1c8842a09589fc8d5f285266f3340bf4abdf66700ba22988f14cc9b2b3118`.
- Monitoring passed six tests, `cargo fmt --check`, and
  `cargo clippy --locked -- -D warnings` using Rust 1.85.1. Its tests now run in
  both the general unit workflow and protected `required-tests`; backlink CI
  continues to reject skipped tests. Changed workflows passed actionlint.
- Independent scoped correctness and security reviews found no actionable
  findings. These are local worktree checks, not a full-history secret audit,
  protected-PR result, merge approval, or runtime authorization.

Docker startup was observed with the retained baseline Backlinks Processor
already running. With explicit owner approval, only that processor was stopped;
its restart policy and the other existing containers were left unchanged. No
retained datastore queries, reconciliation, or reset were performed, so this
pass does not attest that historical datastore contents remained unchanged.

### Rollback

Stop the processor and retain the Redis backlog. Do not roll back to the
delete-first implementation. Restore MongoDB only if the reconciliation tool,
not normal idempotent projection, wrote incorrect data.

## F6: Define bounded indexing for JavaScript shells

### Root cause

Static indexing intentionally extracts paragraph text only. The Google Fonts
response retained on 2026-08-18 was a JavaScript application shell with title
and description metadata but no paragraph text. The indexer classified it as
permanently non-indexable. Rendering is implemented but disabled, and browser
data APIs needed by some applications remain unsupported.

### Decision

Do not enable public rendering to make the static baseline pass. Implement an
explicit metadata-only fallback for individually approved canonical URLs, and
retain rendering as a separate capability with separate authorization.

### Target state

- Normal static pages retain paragraph-only indexing.
- An exact approved URL with no paragraph text may index bounded title and
  description metadata with explicit `extraction_mode: static_metadata`.
- Non-approved shells remain non-indexable.
- Search results never imply that dynamically loaded application content was
  indexed when only metadata was available.

### Plan

1. Add `contracts/static-extraction-policy-v1.schema.json` and a reviewed
   `services/indexer/config/static-extraction-v1.json`. Each rule contains an
   ID and exact canonical URL; startup records the policy SHA-256.
2. Bound and normalize `<title>`, `og:title`, `description`, and
   `og:description`. Permit at most 512 UTF-8 bytes of title, 2,048 bytes of
   description, 4,096 combined normalized bytes, 256 indexed tokens, and a
   100-token summary.
3. Persist `extraction_mode`, extraction rule ID, policy version, policy digest,
   and a stable non-indexable or fallback reason in metadata and structured
   logs.
4. Add a synthetic app-shell fixture modeled on the retained Google Fonts
   structure. Do not replay the legacy Redis page hash manually.
5. First make Indexer metadata readers tolerant of the additive provenance
   fields, and add compatibility tests against records with and without them.
6. Expose `extraction_mode` through the Query Engine API and label metadata-only
   results in the UI without implying that dynamic application content was
   indexed.
7. Verify the fixture is searchable only when its exact URL is approved and that
   no Render Worker or browser request occurs.
8. Keep Stage 1 networkless inline rendering and Stage 2a brokered scripts and
   stylesheets behind `render-policy-v1.disabled.json` until the separate rollout
   gates pass.
9. If metadata-only results are insufficient, replace the seed with an approved
   server-rendered source or open a separately authorized rendered-crawl plan.

Primary implementation areas are `services/indexer/utils/utils.py`,
`services/indexer/main.py`, the metadata model and persistence layer, Indexer
tests and fixtures, `services/query-engine/app/Http/Controllers`, the search
result view and API tests, Compose configuration, and the JavaScript crawl
scope.

### Acceptance gate

- [ ] The approved app-shell fixture produces the exact expected fixture tokens,
  stays within every byte/token bound, and has a non-empty summary with
  `extraction_mode=static_metadata`.
- [ ] The same fixture remains non-indexable when its URL is not approved.
- [ ] Existing static and rendered extraction tests remain unchanged and pass.
- [ ] Indexer-to-MongoDB-to-Query-API-to-UI testing proves search exposes and
  labels metadata-only extraction.
- [ ] Metadata records contain the extraction rule ID and verified policy digest.
- [ ] No Render Worker starts and no browser-originated request occurs.
- [ ] Any future public rendering follows the separate render rollout gate.

### Rollback

Disable the exact-URL fallback while retaining readers that tolerate its
persisted provenance fields. Do not deploy an older strict `Metadata.from_dict`
reader over those records. If a full binary rollback is required, restore the
matched metadata backup. Remove or replace records only through an audited
recrawl or restore, not ad hoc Redis deletion.

## Delivery sequence

| Phase | Work | Findings | Exit condition |
|---|---|---|---|
| 0 | Preserve the read-only historical evidence and keep all execution blocked | F1, F2 | The failed baseline remains reproducible without running a crawl |
| 1 | Backlinks Processor code repair accepted through PR #9 | F5 | Complete for code/CI: merged as `d914a93` after protected checks; retained-data reconciliation and V2 integration remain gated |
| 2 | Complete Crawl Jobs V2 milestones M1 through M4 | F3 | Foundation, Lua, and disposable Redis lease/retry/dead-letter/crash gates pass without runtime integration or migration |
| 3 | Specify and implement exact host/path policy V2 | F4 | All 67 seeds pass and broader hosts/paths fail before DNS |
| 4 | Implement exact-URL metadata fallback | F6 | Hermetic app-shell acceptance tests pass without rendering |
| 5 | Integrate compatible Spider, feeder, Monitoring, Indexer, Query Engine, Backlinks Processor, and consumer behavior | F3-F6 | Full hermetic V2 integration passes with no mixed V1/V2 path |
| 6 | Replace active V1 operational docs and prepare immutable packaging | F3-F6 | V2 checklist, report template, compatibility manifest, digests, deployment definitions, and rollback commands are reviewed |
| 7 | Freeze writers, preserve matched restore-tested evidence, execute F1/F2 reset, and rehearse stopped migration/rollback | F1-F4 | Fresh accepted state contains exactly 67 depth-0 seeds and the stopped transition is reproducible |
| 8 | Run final hermetic, crash-injection, protected-CI, and immutable release gates | F3-F6 | Every required test and promoted artifact passes against the same reviewed commit |
| 9 | Obtain new site and run authorization for one bounded static batch | All | Every rewritten V2 checklist gate passes in a new dated report |

F5 code acceptance and F3 M1/M2 passed through PR #9. Dormant M3 and reviewed M4
preparation merged through PR #10. The first smoke attempt's init FAIL is retained;
the corrected, separately approved real-Redis ledger smoke case now passes with
verified cleanup. Broader M4 conformance and later-case approvals remain pending.
F4 and the hermetic part of F6 can proceed before runtime integration.
F1 and F2 execute only after compatible code and runbooks are ready
so the disposable environment is reset once. Crawl Jobs V2 milestones M5
through M7 align with parent phases 5 through 8 and wait for the stated F4
through F6 dependencies.

Parent phase 8 and F3 M7 establish release readiness only; neither grants crawl
authority. Actual authorization and one bounded crawl belong only to parent
phase 9 and F3 M8 after every prior gate passes.

Phase 6 must update `.github/workflows/build-docker-images.yml` and the release
validator so the exact V2 compatibility manifest records immutable digests for
Spider, Seed Importer, crawl-admin, Indexer, Image Indexer, Backlinks Processor,
and Monitoring, plus either an immutable Render Worker digest or the literal
`disabled`, as required by the normative protocol. Monitoring therefore needs
an immutable image and deployment definition before cutover. Query Engine is a
separately promoted application artifact needed to expose F6 provenance; it is
not a V2 compatibility-manifest participant. Production must not depend on an
unrecorded local source build. The manifest also records every protocol,
policy, publication, IPC, Redis-configuration, and guard field required by
[`crawl-jobs-v2.md`](crawl-jobs-v2.md).

## Overall definition of done

- [ ] F1 through F6 acceptance evidence is linked from this plan.
- [ ] Protected CI includes Spider crash recovery, policy scope, backlink
  persistence, and app-shell indexing tests.
- [ ] The root development data stores are stopped or portless before any
  isolated Spider run.
- [ ] The Render Worker remains stopped for the static baseline.
- [ ] Promoted images are referenced by immutable digest and deployed only with
  their compatible queue generation.
- [ ] The active checklist, environment guide, report template, release workflow,
  and cutover runbook describe only the accepted V2 execution path.
- [ ] Stop, drain, backup, restore, migration, and rollback rehearsals pass.
- [ ] A new authorization and a new dated crawl report exist.
- [ ] The historical 2026-08-18 **FAIL** report remains unchanged.
