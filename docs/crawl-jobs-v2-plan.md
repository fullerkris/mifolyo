# Crawl Jobs V2 Implementation Plan

**Finding:** F3 - durable crawl-job leases and recovery

**Last updated:** 2026-09-14

**Working branch:** `feature/crawl-jobs-v2-foundation`

**Initial checkpoint:** `e4372a66201b8767bcca4d7476c30c5b7999922c`

**Status:** WIP/no-merge; M1 passes locally after the approved transcript amendment
and independent re-review; M2 checkpoint is pushed and remote-verified

**Current next gate:** M3 requires a separate authoritative-Lua work decision.
The scoped M2 foundation checkpoint is preserved as
`0989001d15c9a84a00464fd55ddd857650eda85e`; local HEAD and the remote feature-branch
ref were verified identical after push. That checkpoint request excluded PR
creation. On 2026-09-14 the owner separately authorized scoped reliability
commit/push and a draft PR with protected checks. Publication is in progress;
authoritative Lua and operational activation remain unauthorized.

**Activation status:** Blocked; no authoritative Lua, runtime integration,
migration, deployment, or crawl has occurred or is authorized by this plan

## Document ownership

This is the single mutable implementation plan and current status source for
Crawl Jobs V2. Update this file as F3 progresses instead of adding another dated
plan or copying mutable status into the protocol.

- [`crawl-jobs-v2.md`](crawl-jobs-v2.md) is the digest-bound normative protocol.
  It owns exact constants, wire grammar, records, transitions, Redis
  requirements, cutover rules, and acceptance evidence. It takes precedence if
  this plan differs.
- [`spider-render-remediation-plan-2026-09-01.md`](spider-render-remediation-plan-2026-09-01.md)
  is the parent F1 through F6 plan and contains only an F3 status roll-up.
- [`crawl-jobs-v2-foundation-status-2026-09-07.md`](crawl-jobs-v2-foundation-status-2026-09-07.md)
  is a preserved dated checkpoint. Its counts and review state are historical,
  not the current plan.
- [`v1-baseline-crawl-test-report-2026-08-18.md`](v1-baseline-crawl-test-report-2026-08-18.md)
  remains immutable strict FAIL evidence.
- [`v1-baseline-crawl-test-checklist.md`](v1-baseline-crawl-test-checklist.md)
  retains V1 commands as historical protocol context until a tested V2
  checklist replaces its active execution sections.
- [`scrapped&ChangedPlannings.md`](scrapped&ChangedPlannings.md) is the
  non-authoritative archive for superseded planning decisions. It does not
  override this plan or the normative protocol.

## Objective

Replace the nondurable V1 crawl queue with an exact at-least-once run/job
protocol. Every destructive claim must have a durable expiring lease; every
request start must be recorded before network I/O; every terminal job must have
a stable disposition; and every visible output plus job acknowledgment must be
published by one lease-fenced atomic commit.

The implementation must fail closed across crashes, retries, stale workers,
lost replies, policy changes, Redis restarts, overlapping runs, and bounded
resource pressure. Mixed V1/V2 operation is forbidden.

## Non-negotiable boundaries

- Keep the package dormant until its current phase gate passes.
- Do not start Spider, Render Worker, Backlinks Processor, feeder, consumers,
  Monitoring, or crawl-admin while completing foundation or Lua work.
- Do not mutate retained Redis/MongoDB evidence, install candidate markers,
  migrate V1 state, deploy, or issue a public request.
- Keep `services/spider/internal/database/redis_client.go` and existing V1
  behavior unchanged during the dormant foundation and Lua phases.
- Keep the Render Worker disabled under the checked-in deny-all render policy.
- Reject `ZERO_SHA256` in every production/runtime authority path. Its only
  permitted use is the isolated non-authoritative fixture GuardCore exception
  defined by normative section 5.1.
- Use Redis `TIME` for authoritative timestamps and deadlines.
- Require standalone Redis 7, AOF, `appendfsync always`,
  `aof-load-truncated no`, `noeviction`, bounded sizing, and fail-closed restart
  approval before operational acceptance.
- Treat the first successful `CJ2_START_REQUEST`, recorded before DNS, as the
  irreversible ordinary rollback boundary.
- Preserve the historical V1 FAIL report and retained datastore counts as
  evidence; never reuse them as an accepted baseline.

## Change-control governance

- Current F3 work is WIP and no-merge. Local checks are supporting evidence;
  protected-main acceptance requires the exact pushed commit to pass the
  protected `required-tests` context on a PR targeting `main`.
- A draft F3 PR may be opened or updated only after scoped verification and a
  scoped secret scan pass, the reviewed files are committed and pushed, and
  local/remote commit identity is verified. Repeat that sequence before each
  draft update.
- Opening or updating a draft PR is review intake only. It does not authorize
  merge, authoritative Lua work, runtime wiring or activation, candidate
  promotion, migration, deployment, rendering, or crawling.

## Approved transcript amendment

On 2026-09-11 the project owner explicitly selected **Approve revision** for the
request-history/publication correction, dormant implementation, contract digests,
tests, and independent review. This approval did not authorize authoritative Lua,
crawling, rendering, migration, or deployment.

The preactivation V2 amendment preserves the semantic output/publication digests
and downstream page/image grammar. It adds a retained per-fence start baseline,
uses the existing cumulative request-start count as terminal generation, and
binds both to commit identity. A thirteen-field authenticated witness proves the
full lease and complete interval; successful BEGIN is the atomic compare-and-
freeze boundary. The retained stage fence prevents new starts after abort.
Stage writes, seal, first commit, and completed replay retain exact identity
checks. There is no legacy-shape fallback, new operation, or new key family.

The Go foundation now makes BEGIN derive all digests and counts from actual
validated output, retains exact source/witness evidence, and shares one bounded
I/O-permit registry across separately parsed replies in a lease session. These
are dormant construction and prevalidation checks. Atomic Redis enforcement,
real reconnect/I/O behavior, and full stage reread acceptance remain M3-M5 work.

## Current checkpoint

| Area | Current state |
|---|---|
| Normative protocol | Approved preactivation transcript amendment; exact-document/empty-Lua contract SHA-256 `1996c4519c93c8fc6da6c78eacc74afbc8eeb4506ff23bad3deaa887e5d43987` |
| Shared conformance | Fixture v2 with one baseline, 39 named positive cases, and 139 negative cases; independent Go and Python consumers pass |
| Dormant Go package | Implemented under `services/spider/internal/database/crawljobsv2`; no runtime import or activation wiring |
| Operation wire family | All 43 operations and 52 gate variants have closed constructors and independent inventory coverage |
| Schemas and constants | All 16 record schemas, every allowed response/status schema, and all 63 protocol constants are independently pinned |
| Authority model | Authenticated transcript genesis/terminal witness, output-derived BEGIN metadata, full lease/source/stage binding, replay-shared one-use permits, run-policy authority, and fail-closed redaction are implemented and reviewed |
| Ledger validation | Baseline/delivery/fence history, post-abort freeze, exact retained witnesses, strict worker expiry, completed replay, prior retry/backpressure/reason fixes, and zero-sentinel checks pass the final aggregate and counterexample replays |
| Script sources | No authoritative `.lua` files exist; production cannot construct an executable `ScriptBindingSet` until embedded reviewed sources provide the private sealed bundle |
| Runtime behavior | No operational/retained datastore mutation, service start, migration, deployment, candidate marker, rendering activation, or crawl has occurred |
| Review status | Final independent correctness, conformance, and defensive reviews returned scoped GO on 2026-09-11; no outstanding BLOCKER/HIGH/MEDIUM findings from those reviews |
| Git state | Reviewed foundation checkpoint `0989001d15c9a84a00464fd55ddd857650eda85e` is pushed and remote-verified; unrelated worktree changes remain local |

## Latest local verification

The final amended source/fixture tree passed the following matrix on 2026-09-11.
Go commands ran from `services/spider` with `GOPROXY=off` and the cached pinned
`GOTOOLCHAIN=go1.25.13` (`darwin/arm64`). Python and scoped Git checks ran from the
repository root. Python bytecode was disabled for the verifier and redirected
outside the worktree for the syntax check.

```text
go test ./internal/database/crawljobsv2 -count=10
go test -race ./internal/database/crawljobsv2 -count=3
go test -shuffle=on ./internal/database/crawljobsv2 -count=10
go test ./internal/database/crawljobsv2 -cover -count=1
go vet ./...
go test ./... -count=1
go build ./internal/database/crawljobsv2
python3 scripts/verify-crawl-jobs-v2-digests.py
python3 -m py_compile scripts/verify-crawl-jobs-v2-digests.py
gofmt -d ./internal/database/crawljobsv2
git diff --check -- docs/crawl-jobs-v2.md contracts/crawl-jobs-v2/digest-vectors.json scripts/verify-crawl-jobs-v2-digests.py services/spider/internal/database/crawljobsv2
```

Final package statement coverage is **79.0%**. Normal and shuffled package runs
each passed ten repetitions; the race run passed three. Full Spider module
tests, vet, package build, Python verification/syntax, and formatting passed
without excluded failing tests.

The fixture inventory SHA-256 is
`56797748de64aa57618104bb5135d0300c9d192f41995e743ddb319219a248df`;
the exact fixture-byte SHA-256 is
`056ca8c8031c2714d24800db09bf7a78d25c752d8964173b2ebaae03b0682155`.
The original ordered 37-positive/118-negative inventory is preserved. Negative
cases include both production-boundary and fixture-grammar checks: page-section
cardinality is grammar-only, alias counts exercise the production projection,
and the oversized manifest control proves size-guard precedence rather than a
fully valid maximum-size manifest. Synthetic render vectors prove serialization,
not production artifact authorization. Counts alone are not runtime acceptance.

Independent final reviews replayed the prior reservation-depth and typed
staged/unstaged renewal counterexamples plus omitted starts, stale generations,
same-millisecond witnesses, repeated START replies, BEGIN metadata substitution,
exact source/witness mismatch, abort freeze, expiry boundaries, historical
delivery admission, and precise count-error controls. All reported findings were
fixed and rechecked; the correctness, conformance, and defensive verdicts are GO
for M1's dormant scope. Scoped filename-only credential-signature and redaction
reviews found no exposure; this is not a repository-history secret audit.

The earlier 77.8% aggregate and five-fix package pass remain historical evidence
in the log below. Exploratory system-Go-1.25.4 runs encountered runtime GC
crashes of undetermined cause; those runs are not final acceptance evidence.
The final matrix and final independent reviews used pinned Go 1.25.13 and passed.

## Milestone status

| Milestone | Status | Exit condition |
|---|---|---|
| M0: Protocol and dormant foundation | Complete | Normative contract, fixture, Go package, Python verifier, and initial WIP checkpoint exist |
| M1: Foundation release gate | Complete locally | Approved amendment, final aggregate verification, and three scoped independent GO reviews; no outstanding review findings |
| M2: Reviewed foundation checkpoint | Complete | Scoped 66-file checkpoint secret-scanned, tested from the index export, committed, pushed, and remote identity verified |
| M3: Authoritative Lua transitions | Not started; separate authorization required | All 43 exact sources and sealed bindings pass Go/Python/Lua conformance without runtime activation |
| M4: Real Redis 7 acceptance | Blocked by M3 | Idempotency, fencing, crash, AOF, memory, and latency evidence passes on disposable infrastructure |
| M5: Runtime and consumer integration | Blocked by M4 and F4-F6 | Spider, feeder, consumers, Monitoring, Compose, and crawl-admin use only the accepted V2 protocol |
| M6: Migration, runbooks, and rollback | Blocked by M5 | Stopped migration and rollback rehearsal pass; active docs contain tested V2 commands and no active V1 path |
| M7: Immutable release gate | Blocked by M6 | Final digests, manifests, images, backups, CI, and authorization/report templates are reviewed |
| M8: Bounded crawl | Not authorized | A new explicit site/run authorization and dated test report exist after every prior gate passes |

## Immediate to-do list

- [x] Write and review the normative protocol before implementation.
- [x] Add the dormant Go foundation and independent shared-vector verifier.
- [x] Push the initial WIP checkpoint to a dedicated feature branch.
- [x] Remediate transport, authority, wire, record, conformance, redaction, and
  script-source trust findings from prior review rounds.
- [x] Make the run-pinned policy proof mandatory at every source, discovery,
  reservation, transcript, response, and wire consumer.
- [x] Reject `ZERO_SHA256` at every production authority boundary while
  preserving only the exact provisional fixture exception.
- [x] Pass the latest aggregate normal, race, shuffled, full-module, vet, build,
  Python, formatting, and diff checks.
- [x] Obtain the 2026-09-11 independent correctness, conformance, and defensive
  Lua-start verdict; it returned NO-GO.
- [x] Locally remediate the five implementation-level HIGH findings: exact retry
  replay, post-abort backpressure state, authorization-expiry cancellation,
  terminal reason classes, and initial-document/source binding.
- [x] Add the directly related retry/recovery counter validations identified by
  the same review.
- [x] Obtain explicit owner approval to revise the digest-bound protocol to add transcript
  genesis, terminal-generation, stage-freeze, and seal/commit freshness binding.
- [x] Implement the approved normative amendment and update every affected
  digest, fixture, schema, constructor, and independent oracle.
- [x] Resolve every remaining BLOCKER/HIGH finding and explicitly disposition
  any medium residual before starting Lua.
- [x] Record the NO-GO review result and current local verification evidence in
  this plan.
- [x] Run the complete normal, race, shuffled, full-module, vet, build, Python,
  formatting, and diff matrix against the combined final tree.
- [x] Obtain a new independent GO/NO-GO review after the protocol blocker and
  every implementation finding are closed.
- [x] Complete the scoped credential-signature and sensitive-surface review;
  repeat the scan on the exact M2 checkpoint scope before publication.
- [x] Obtain explicit commit/push authorization for the M2 checkpoint.
- [x] Stage only the F3 normative contract, fixture, verifier, plan/status consolidation,
  and `crawljobsv2` files; leave unrelated worktree changes untouched.
- [x] Commit and push the reviewed foundation remediation checkpoint.
- [ ] Begin dormant authoritative Lua source work only after M1 and M2 pass.

## M1: Final foundation release gate

### Review scope

- Exact run-policy provenance and mandatory policy checks at each consumer.
- Replay-shared one-use request-start I/O authority and the amended authenticated
  genesis, terminal-generation, and request-transcript-to-stage boundary.
- Render artifact bytes, run pin, URL matcher, and exact rule identity.
- Candidate/active transport phases and stopped-world sequencing.
- Every operation-specific KEYS/ARGV/record/chunk shape and request-size limit.
- Fixed run, job, reservation, rate-scope, and stage record relations.
- Exact output, publication, commit, stage TTL, page-key, and alias derivation.
- Complete source-set and contract trust anchor for future Lua bindings.
- Production `ZERO_SHA256` rejection and provisional fixture isolation.
- Stable errors, value-redacted diagnostics, and independent schema/constant
  oracles.

### Exit checklist

- [x] Independent correctness review reports no BLOCKER/HIGH issue.
- [x] Independent conformance review replays every prior counterexample and
  reports no BLOCKER/HIGH issue.
- [x] Independent defensive review reports no BLOCKER/HIGH authority issue.
- [x] The Python verifier syntax gate passes on the same final tree:
  `python3 -m py_compile scripts/verify-crawl-jobs-v2-digests.py`.
- [x] Go and Python independently verify one baseline, 39 named positive, and
  139 negative shared-fixture cases with pinned inventory SHA-256
  `56797748de64aa57618104bb5135d0300c9d192f41995e743ddb319219a248df`.
- [x] The independent conformance review explicitly names, replays, and records
  both context counterexamples:
  - **Reservation-depth mismatch:** standalone reservation-record validation
    does not prove depth because that record has no depth field; a valid
    decision digest for a depth different from the owning job must fail at the
    context-bearing transition boundary.
  - **Renew-lease staged/unstaged context ambiguity:** a `RENEWED` envelope must
    fail generic validation and pass only with the exact typed context,
    including replay at the stage-capped deadline.
- [x] All local verification commands pass on the same final tree.
- [x] No authoritative Lua source, runtime wiring, retained datastore mutation, or
  activation is included in the foundation checkpoint.

## M2: Foundation checkpoint preservation

The owner explicitly requested committing and pushing the reviewed checkpoint.
M2 completed on 2026-09-12: checkpoint
`0989001d15c9a84a00464fd55ddd857650eda85e` was pushed to
`origin/feature/crawl-jobs-v2-foundation`, and `git ls-remote` returned the same
full identity as local HEAD. That request did not include PR creation or Lua
work. The later 2026-09-14 request authorizes draft-PR publication only, not Lua
or activation. Repeat scoped verification and review any further source changes
before another checkpoint.

The exact staged snapshot passed the full Spider module tests, foundation race
tests, Go vet, and independent Python verifier using cached Go 1.25.13. The
66-file staged allowlist and filename-only credential scan passed. This is a
local checkpoint gate, not protected-main CI or container-build acceptance.

The checkpoint includes the complete parent plan and archive as supporting
planning documents. Other application, frontend, agent, and operational-doc
changes remain outside the commit. The root README is staged only for the F3
warning and checkpoint paragraph.

Checkpoint packaging limit, resolved locally on 2026-09-14: Spider now builds
from a narrowly allowlisted repository-root context with repository-relative
builder paths. Both Compose build definitions, release CI, renderer-integration
CI, and protected `required-build` use the same Dockerfile/context. The builder
includes the exact F3 fixture/document, shared URL fixture, and manual seed
catalog; none enters the runtime image.

The builder and final image built successfully on Linux/arm64 with Go 1.25.13.
The full Dockerfile Go suite passed, including F3 conformance and the seed-policy
test previously skipped in service-only builds. Only the existing opt-in V1
Redis and Chromium integration tests skipped; neither was activated for this
packaging pass. Networkless shell-only image checks verified builder inputs,
credential-file exclusion, runtime-only contents, CA certificates, and the
unchanged `65534:65534` runtime user. `docker build --check` passed without
warnings. Local normal/race module tests, shuffled F3 tests, vet, the independent
Python digest verifier, Compose resolution, release-Compose static validation,
and actionlint also passed. The normative protocol, F3 fixture, dormant package,
and existing V1 Redis client remain unchanged.

These are local worktree results, not protected-main or release acceptance.
No commit, push, or PR was created by that validation pass. A feature-branch
push does not itself trigger the configured main/legacy-branch push workflows;
the exact reviewed revision still needs protected PR checks.

The following allowlist records the completed M2 foundation checkpoint scope.
The separately authorized 2026-09-14 reliability publication adds F5 code/tests,
bounded Monitoring, paired URL fixtures, Spider build packaging and callers,
required CI coverage, and related documentation hunks only. Frontend, agent,
generated-output, and unrelated operational-document changes remain local.
Exact-revision check results belong in the draft PR; none is claimed by this
pre-publication snapshot. The draft must remain WIP/no-merge.

1. Inspect scoped status and diff without resetting or cleaning the worktree.
2. Remove only generated artifacts created by this work, if any.
3. Run a scoped secret and sensitive-data scan.
4. Stage only:
   - `docs/crawl-jobs-v2.md`
   - `contracts/crawl-jobs-v2/digest-vectors.json`
   - `scripts/verify-crawl-jobs-v2-digests.py`
   - `services/spider/internal/database/crawljobsv2/`
   - `docs/crawl-jobs-v2-plan.md`
   - `docs/crawl-jobs-v2-foundation-status-2026-09-07.md` (existing checkpoint-label changes only; do not rewrite its dated evidence)
   - `docs/scrapped&ChangedPlannings.md`
   - the F3 roll-up changes in `docs/spider-render-remediation-plan-2026-09-01.md`
   - the F3 status paragraph in the root `README.md`
5. Re-run scoped tests and inspect the staged diff.
6. Commit without amending the initial checkpoint.
7. Push the feature branch and verify local/remote commit identity.
8. Only then may a draft F3 PR be opened or updated for review and protected
   checks. Keep it draft/no-merge; repeat steps 1 through 7 before updating its
   commit, and do not treat the PR as runtime or crawl authorization.

## M3: Authoritative Lua transitions

### Source and trust-anchor rules

- Add exactly one canonical reviewed source for each of the 43 operations.
- Embed exact source bytes in the trusted Go package; production callers may
  never provide source bytes or expected hashes.
- Generate and independently verify each canonical source name, Redis SHA-1,
  source SHA-256, ordered source-set digest, and approved contract digest.
- Construct the package-private sealed `ScriptBindingSet` only from those
  embedded reviewed sources.
- On `NOSCRIPT`, load bytes from the same sealed bundle and verify Redis's
  returned SHA-1 before retrying.
- Reject missing, extra, duplicated, renamed, reordered, swapped, or one-byte
  modified sources.
- Keep every script dormant and unwired until all 43 sources pass shared
  conformance.

### Implementation groups

| Group | Operations | Required behavior |
|---|---|---|
| Administrative and candidate | `CJ2_APPROVE_BOOT`, `CJ2_INSTALL_CANDIDATE_MARKERS`, `CJ2_RETIRE_LEGACY_KEYS`, `CJ2_PROMOTE_CANDIDATE_CONTRACTS`, `CJ2_MARK_PLANNED_SHUTDOWN` | Exact boot, freeze, compatibility, retirement, promotion, and nonce authority |
| Run lifecycle | `CJ2_CREATE_RUN`, `CJ2_ENQUEUE_BATCH`, `CJ2_BEGIN_RUN_AUDIT`, `CJ2_AUDIT_RUN_BATCH`, `CJ2_SEAL_RUN`, `CJ2_ACTIVATE_RUN` | Immutable run pins, source admission, complete audit, seal, and activation |
| Worker and requests | `CJ2_REJECT_READY`, `CJ2_TRY_CLAIM`, `CJ2_RENEW_LEASE`, `CJ2_RESERVE_REQUEST`, `CJ2_START_REQUEST`, `CJ2_FINISH_REQUEST`, `CJ2_CANCEL_RESERVATION`, `CJ2_RELEASE_BEFORE_IO`, `CJ2_RETRY`, `CJ2_DEAD`, `CJ2_CANCEL_JOB`, `CJ2_COMPLETE_NO_OUTPUT` | Leases, fences, reservations, request budgets, rate scopes, retries, and terminal outcomes |
| Staging and commit | `CJ2_BEGIN_STAGE`, `CJ2_STAGE_PAGE_FIELDS`, `CJ2_STAGE_PAGE_BLOB`, `CJ2_STAGE_OUTLINKS_BATCH`, `CJ2_STAGE_DISCOVERIES_BATCH`, `CJ2_STAGE_ALIASES_BATCH`, `CJ2_STAGE_IMAGES_BATCH`, `CJ2_STAGE_IMAGE_MANIFEST`, `CJ2_ABORT_STAGE`, `CJ2_SEAL_STAGE`, `CJ2_COMMIT` | Bounded immutable staging and one atomic idempotent publication/ACK |
| Maintenance | `CJ2_PROMOTE_DUE`, `CJ2_RECOVER_EXPIRED`, `CJ2_CANCEL_RUN`, `CJ2_CANCEL_BATCH`, `CJ2_FINALIZE_RUN`, `CJ2_ARCHIVE_RUN`, `CJ2_PURGE_RUN_BATCH`, `CJ2_CLEAN_STAGE`, `CJ2_MAINTAIN_RATE_SCOPES` | Bounded recovery, cancellation, finalization, retention, purge, cleanup, and rate maintenance |

### Lua acceptance checklist

- [ ] Every script validates its exact key count, key grammar, ARGV grammar,
  transport gate, record shape, constants, and bounds before mutation.
- [ ] Every mutating script uses Redis `TIME` and never caller wall time.
- [ ] Active and candidate mode behavior matches the closed operation matrix.
- [ ] Every idempotent replay compares the full immutable request identity.
- [ ] Every failure path performs no partial mutation.
- [ ] Every response matches one allowed operation/status schema exactly.
- [ ] Go, Python, and Lua agree on every positive and negative vector.
- [ ] The contract digest is regenerated from the exact protocol bytes and
  complete ASCII-sorted Lua source set.

## M4: Disposable Redis 7 acceptance

Use newly created, isolated, non-production infrastructure with no public route,
production data, production credential, or reusable volume.

### Required tests

- [ ] SIGKILL immediately after claim recovers the job after lease expiry.
- [ ] SIGKILL during fetch and publication recovers without partial output.
- [ ] A crash after atomic commit but before response delivery leaves exactly
  one completed job, notification, and discovered-job set.
- [ ] A stale fence/token cannot renew, publish, ACK, NACK, or clean a stage.
- [ ] Retryable failures use exact backoff and reach one dead record at the
  exact delivery limit.
- [ ] Known terminal denials make no DNS or network request.
- [ ] Crashes before reservation, after reservation, and after recorded request
  start preserve every run/group request-start bound.
- [ ] Independent workers cannot exceed shared concurrency or complete one
  lease twice.
- [ ] Overlapping runs keep separate run budgets while sharing durable rate
  scopes and stricter unexpired deadlines.
- [ ] Redis restart preserves ready, leased, delayed, completed, dead,
  cancelled, reservation, stage, and rate state with zero acknowledged-write
  loss in the tested process-crash model.
- [ ] Idle maintenance and service restarts perform no unnecessary write and
  leave `signal_queue` absent.
- [ ] Maximum-shape memory, request, response, key, and stage bounds pass under
  the approved Redis configuration.
- [ ] Lua p99 at the maximum approved shape remains strictly below 100 ms
  without increasing Redis's Lua time limit.
- [ ] Provisional fixture bootstrap and teardown meet every normative section
  5.1 isolation and credential-destruction requirement.

### Evidence outputs

- Exact Redis image digest and full configuration artifact.
- Fixture manifest and all setup/teardown checksums.
- Per-operation conformance, race, crash, and idempotency results.
- Maximum-shape input digest, memory evidence, Lua benchmark, AOF/crash
  evidence, and restart-approval evidence.
- Explicit restore scope and separate backup/restore rehearsal results.

## M5: Runtime and consumer integration

Integrate only after M4 passes. Do not create a compatibility layer that permits
mixed V1/V2 operation.

### Required components

- Spider database, crawler, controllers, and command wiring.
- Seed Importer V2 feeder and source/audit workflow.
- Indexer and Image Indexer V2 publication limits and acknowledgment behavior.
- Backlinks Processor additive acknowledged projection.
- Monitoring run/job/rate/stage metrics with bounded cursor scans.
- Compose definitions, health/readiness checks, immutable runtime images, and
  least-privilege Redis ACLs.
- A stopped crawl-admin tool for boot approval, candidate operations,
  migration, archive/purge, and evidence collection.

### Integration acceptance

- [ ] No runtime image reads, writes, recreates, waits on, or deletes
  `signal_queue`; only stopped retirement may delete the legacy key.
- [ ] Monitoring reports ready, leased, delayed, completed, dead, cancelled,
  oldest age, starts, reservations, retries, recoveries, and renewal failures.
- [ ] Output remains invisible until the fenced commit publishes every final
  key, backlink effect, discovery, and exact page notification atomically.
- [ ] Downstream consumers preserve source/processing/dead-list identities and
  never acknowledge before durable downstream persistence.
- [ ] Rendering remains disabled unless the separate F4/F6 and render rollout
  gates pass.
- [ ] Full module, repository, race, security, and protected PR suites pass.

## M6: Migration, active documentation, and rollback

### Stopped migration

- Freeze every producer, consumer, scheduler, trigger, scaler, and ordinary
  Monitoring process.
- Create matched checksummed MongoDB/Redis backups outside project volumes and
  prove restore in a separate inspection context.
- Admit only valid pending V1 members whose URL and depth metadata match.
- Treat hash-only V1 identities and mismatched metadata as evidence, not
  migratable work.
- Produce candidate run/source digests and complete stopped-world evidence.
- Never dual-read, dual-write, or run V1 and V2 workers together.

### Documentation replacement

- Replace active V1 checklist execution sections with tested V2 commands,
  expected keys/states, policy checks, stop conditions, rollback commands, and
  a V2 report template.
- Update `docs/environments.md`, the root README, Spider README, Seed Importer
  README, Compose guidance, and release cutover runbook together.
- Preserve V1 documents only as clearly marked historical context.
- Resolve the older immutable cutover runbook's rollback boundary and image-set
  assumptions in favor of the normative V2 contract.

### Rollback

- Before the first successful `CJ2_START_REQUEST`, restore the matched V1
  snapshot, old release, and old credentials only while all writers are stopped.
- After the first successful `CJ2_START_REQUEST`, stop all producers and
  consumers and restore the coordinated Redis/MongoDB backup or roll forward.
- Never point a V1 Spider at V2 keys and never restore provisional fixture state.

## M7: Immutable release gate

M7 establishes immutable release readiness only. It does not grant or exercise
site/run authority. Actual authorization and bounded execution remain a
separate M8 decision.

- [ ] Final contract, source-set, guard-core, commit-guard, compatibility,
  fixture, policy, and evidence digests are independently reproduced.
- [ ] Every required participant image is immutable, scanned, recorded in the
  compatibility manifest, and built from the reviewed commit.
- [ ] Candidate installation occurs only while the system is stopped and every
  required marker/freeze/legacy predicate is proven.
- [ ] Stop, drain, backup, restore, migration, rollback, crash, and restart
  rehearsals pass with linked evidence.
- [ ] Protected CI passes for the exact promoted commit and the complete
  reviewed compatibility-manifest artifact, including every protocol-defined
  image digest field.
- [ ] F1/F2 data reset, F4 exact scope, F5 consumer acceptance, and F6 indexing
  disposition are complete.
- [ ] The authorization request and V2 report template are reviewed and ready
  without granting or exercising crawl authority.

## M8: Authorized bounded crawl

- [ ] M1 through M7 have passed against the exact promoted commit and complete
  reviewed compatibility-manifest artifact.
- [ ] A new site/run authorization names the exact seeds, policies, budgets,
  images, expiry, and operator.
- [ ] One bounded static batch follows the accepted V2 checklist; the Render
  Worker remains stopped unless its separate activation gate also passes.
- [ ] A new dated report records every preflight, result, stop condition, and
  post-run count.
- [ ] No historical authorization or failed baseline evidence is reused.

## Dependencies

| Dependency | Why F3 waits |
|---|---|
| F1 retained catalog reconciliation | A run cannot be authorized from the stale 70-enabled catalog |
| F2 disposable datastore reset | The retained post-test queue is evidence, not a fresh V2 baseline |
| F4 exact URL and redirect policy | Durable execution cannot make broad scope acceptable |
| F5 acknowledged backlink persistence | The idempotent acknowledged-before-removal repair is implemented and locally verified; runtime integration still waits for a passing protected `required-tests` PR run |
| F6 JavaScript-shell disposition | The bounded static baseline needs an approved indexing outcome without enabling rendering by default |
| Render rollout gate | JavaScript execution requires separate policy, image, sandbox, terms, robots, and authorization evidence |

F3 foundation remediation may proceed while F1, F2, F4, and F6 remain
operationally blocked. M1 passes locally and M2 is remote-verified; Lua has not
been authorized or started. M5 hermetic code/consumer integration waits for M4 and
F4-F6. In the parent plan, compatible code and runbooks precede the F1/F2
operational reset so retained evidence is preserved and the environment is reset
once. Migration rehearsal and candidate promotion require that accepted fresh
state; release readiness and any crawl still require all applicable gates.
Rendering remains disabled unless its separate activation requirements pass.

## Evidence log

| Date | Evidence | Result |
|---|---|---|
| 2026-09-07 | Initial dormant foundation checkpoint | Pushed as `e4372a66201b8767bcca4d7476c30c5b7999922c`; no runtime wiring |
| 2026-09-09 | Prior independent correctness, conformance, and defensive review rounds | Deterministic authority and ledger findings identified; Lua-start remained NO-GO |
| 2026-09-10 | Aggregate verification after policy-consumer and zero-sentinel remediation | Normal/race/shuffle/full tests, vet, build, Python verifier, formatting, and diff checks passed; coverage 77.8% |
| 2026-09-11 | Independent correctness, conformance, and defensive review | NO-GO: six HIGH findings; five are implementation-level and one requires a transcript/stage protocol decision |
| 2026-09-11 | Local implementation-level review remediation | Exact retry replay, post-abort state, authorization-expiry cancellation, terminal reason/counter, and initial-document binding fixes applied; combined package test passed, aggregate replay and re-review pending |
| 2026-09-11 | Explicit project-owner protocol-revision approval | Approved dormant contract, implementation, digests, tests, and review only; no Lua or operational activation |
| 2026-09-11 | Preactivation transcript amendment | Added authenticated B/G completeness, thirteen-field witness, atomic BEGIN freeze contract, B/G-bound commit, and replay-shared local I/O permits; semantic output/publication grammar unchanged |
| 2026-09-11 | First amendment re-review | BEGIN still accepted caller-selected digest/count metadata; exact-witness, abort/expiry/delivery-record, and negative-oracle findings required follow-up; no M1 acceptance inferred from passing tests |
| 2026-09-11 | Final review remediation | BEGIN derives metadata from real output; exact source/witness, retained freeze, strict expiry, prior-delivery claim admission, and exact count/size-error controls fixed and counterexamples replayed |
| 2026-09-11 | Final independent correctness, conformance, and defensive reviews | Three scoped GO verdicts on the dormant foundation; every reported HIGH and MEDIUM closed; no runtime/Redis atomic acceptance claimed |
| 2026-09-11 | Final pinned-toolchain aggregate | Go 1.25.13 normal x10, race x3, shuffle x10, full module, vet, build, Python verifier/syntax, formatting, and scoped diff checks pass; package coverage 79.0%; 39 named positives plus baseline and 139 negatives |
| 2026-09-12 | M2 scoped checkpoint publication | `0989001d15c9a84a00464fd55ddd857650eda85e`; 66 intended files; exact staged snapshot tests and secrets gate passed; push succeeded and local/remote identity matched |

## Definition of done

- [ ] M1 through M8 pass with linked immutable evidence.
- [ ] All 43 Lua transitions and every Go/Python/Lua/Redis conformance case agree.
- [ ] Every destructive claim, request start, stage, output, terminal transition,
  and maintenance action is durable, bounded, fenced, and idempotent.
- [ ] No active procedure, image, credential, or queue path permits mixed V1/V2
  operation.
- [ ] The accepted Redis configuration demonstrates zero acknowledged-write loss
  within the stated crash model and restore scope.
- [ ] The active checklist, environment guide, report template, release
  workflow, and cutover runbook describe only the tested V2 path.
- [ ] F1 through F6 parent-plan gates pass.
- [ ] Explicit authorization and a new dated report exist before any crawl.
- [ ] The historical 2026-08-18 FAIL report remains unchanged.
