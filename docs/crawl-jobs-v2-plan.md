# Crawl Jobs V2 Implementation Plan

**Finding:** F3 - durable crawl-job leases and recovery

**Last updated:** 2026-09-21 (UTC; immutable images validated, scoped publication authorized)

**Working branch:** `feature/crawl-jobs-v2-lua`

**Initial checkpoint:** `e4372a66201b8767bcca4d7476c30c5b7999922c`

**Status:** M1/M2 foundation merged through PR #9; all 43 canonical M3 operations
and the complete sealed, zero-argument `AuthoritativeScriptBindingSet()` factory
are implemented and dormant. M3 source implementation and local verification
are complete, including the full Spider module race suite, and the checkpoint
is committed and pushed as `81028ca`.

**Current next gate:** Immutable Linux/arm64 harness and Redis images are prepared
and validated. Target-discovered packaging/network/memory corrections and CI
sharding have scoped independent publication GO. The owner authorized a scoped
commit/push and draft update of PR #10. Complete protected CI on that revision,
then obtain separate exact-artifact execution approval. No real-Redis fixture
has run. See the [image-preparation report](crawl-jobs-v2-m4-image-preparation-2026-09-21.md),
[re-review report](crawl-jobs-v2-m4-rereview-2026-09-21.md); the
[original NO-GO report](crawl-jobs-v2-m4-review-2026-09-21.md) remains historical evidence.

**Preparation update (2026-09-21):** The owner requested work on M4 readiness and
then explicitly approved all four proposals, permitting continued implementation.
The [M4 readiness decision package](#m4-readiness-decision-package-2026-09-21)
below records the decision and implementation sequence. Approval covers the
protocol amendment and offline harness; starting Redis acceptance remains a
separate decision after verification.

The owner subsequently requested first-case implementation, independent review,
then correction of all four findings with regression tests and independent
re-review. That remediation is locally complete; its scoped final GO does not
grant publication or real-Redis execution authority.

PR #9 passed all 14 protected checks and merged as
`d914a93f9ade5b63182ecf02092c1a1e74633713`. On 2026-09-15 the owner authorized
dormant M3 implementation and the narrow wire/renewal clarification below.
The working branch starts from that merged tree; unrelated local changes are
retained. On 2026-09-18 the owner authorized a scoped M3 commit and push to
`feature/crawl-jobs-v2-lua`. Publication completed at
`81028ca12a1763d46df72fc54759d99a0ea3b561`; local HEAD and the remote branch were
verified identical. No M3 PR was created, and publication did not authorize
merge, M4 execution or activation.

**Activation status:** Blocked by M4 real-Redis acceptance and the later
integration/release gates, not by an absent source bundle. No runtime
integration, migration, deployment, or crawl is authorized.

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
  permitted use is the historical non-executable GuardCore serialization control
  defined by normative section 5.1. Executable test guards also require nonzero
  digests; their test provenance cannot become release authority.
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
are dormant construction and prevalidation checks. Real-Redis enforcement,
real reconnect/I/O behavior, and full stage reread acceptance remain M4/M5 work.

## Approved M3 clarification

On 2026-09-15 the owner approved documenting the existing reviewed Go KEYS/ARGV
layout, including one binary RECORD argument per group/source/chunk record, and
clarifying renewal under a backward Redis clock. The prescribed successful
deadline formulas remain unchanged; any proposed shortening of a matching stored
deadline requires `INVALID_STATE` and no writes, including counters/timestamps.
No new operation, key family, field identity, response shape, or permission was
added. The document matrix is checked against the independent literal Go oracle.

The owner approved in-memory Lua conformance for this implementation slice.
Sections 5.1/17.7's real-Redis fixture restrictions were not relaxed or bypassed.
At that checkpoint, the current-document/empty-Lua vector and its dependent
guard chain were regenerated after the approved document change. That vector
remains FOUNDATION framing only, not source authority. The current fixture
retains it as a primitive case; guard cases now explicitly bind the complete
`canonical-lua-bundle` case instead.

## Current checkpoint

| Area | Current state |
|---|---|
| Normative protocol | Includes the owner-approved 2026-09-21 M4 bootstrap/ACL/manifest/administrative amendment; current contract and bundle pins regenerated; Lua sources unchanged |
| Shared conformance | Fixture v2 with one baseline, 40 named positive cases and 157 negative cases; independent Go/Python verification and scoped in-memory Lua conformance pass; the foundation inventory remains preserved |
| Dormant Go package | Implemented under `services/spider/internal/database/crawljobsv2`; no runtime import or activation wiring |
| Operation wire family | All 43 operations and 52 gate variants have closed constructors and independent inventory coverage |
| Schemas and constants | All 16 record schemas, every allowed response/status schema, and all 63 protocol constants are independently pinned |
| Authority model | Authenticated transcript genesis/terminal witness, output-derived BEGIN metadata, full lease/source/stage binding, replay-shared one-use permits, run-policy authority, and fail-closed redaction are implemented and reviewed |
| Ledger validation | Baseline/delivery/fence history, post-abort freeze, exact retained witnesses, strict worker expiry, completed replay, prior retry/backpressure/reason fixes and zero-sentinel checks pass local normal conformance and reviewed counterexample replays |
| Script sources | All 43 canonical operations are implemented: unchanged BOOT passthrough plus 42 exact generated sources; both strict source/bundle generator checks pass |
| Authoritative bundle | Complete zero-argument `AuthoritativeScriptBindingSet()` validates embedded sources against fixed generated pins and returns fresh private sealed bindings; no caller-supplied sources, hashes or paths |
| Local acceptance | September 18 M3 full race/Docker results remain dated evidence; September 21 amendment/offline verification is recorded below; no protected-PR or real-Redis acceptance is claimed |
| Runtime behavior | No M4 real-Redis acceptance run, operational/retained datastore mutation, application/service activation, migration, deployment, candidate marker, rendering activation, or crawl is part of this M3 work |
| M4 preparation | Offline compiler and corrected first executor under `tests/crawl-jobs-v2-redis/`; 43 Python tests plus independent Go wire/race checks pass; target image/isolation/ACL/BOOT/teardown evidence pending |
| Review status | September 18 M3 reviews remain scoped GO. September 21 remediation closes COR-1/COR-2, SEC-1/SEC-2 and the closed-peer follow-up; independent correctness/security re-review is GO for image preparation only |
| Git state | M3 checkpoint `81028ca12a1763d46df72fc54759d99a0ea3b561` is committed, pushed and remote-verified on `feature/crawl-jobs-v2-lua`, based on PR #9 merge `d914a93`; no M3 PR or merge authorization |

### Current source and fixture identities (regenerated 2026-09-21)

| Identity | SHA-256 |
|---|---|
| Canonical `contract_sha256` | `394d4bdbd9c800e167cd20a6ccb5435b2a170cded207d3856810ab5e038151e0` |
| Ordered `source_set_sha256` | `10a4f753395a6d1bccc587194ef2af6346ca3c03f8a8faf71081abc800ac13b8` |
| `bundle_seal_sha256` | `c19086df95454484f00b1865f5bafcdecde3907eeea1a937d546de94f79f9309` |
| Fixture inventory: 40 named positives, 157 negatives, plus baseline | `8c360cf46c283ec1111e9c4a4e9896a2424afc10824566e174bdfcbabc6c15f8` |
| Retained `contract-current-document-empty-lua` primitive, not canonical authority | `afa58848b4cf5f0e7434b9f6008d69d9f1cdeb6776a3b8f0e78bb50f3a97ee36` |

The September 18 M3 contract was
`df171381f4f1bb6d8daec60b9173254f8ae77cfc24907d0a69b279cab89fa562`
with bundle seal
`7ec16509119c44a02eafa8c342fe777dfa6b769937c1681efbabb2fc36823614`.
Its dated full race/Docker results remain evidence for that checkpoint. The
September 21 source-set digest is identical; the normative document and dependent
contract/bundle/guard fixture identities changed.

Current guard-core/guard-chain fixtures explicitly reference the canonical
bundle. The empty-Lua case remains a foundation framing control, not a fallback
contract or guard authority. These fixed local pins establish source identity,
not commit-guard approval, operational evidence or runtime activation.

## Historical foundation verification (2026-09-11)

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

Package statement coverage at this **2026-09-11 foundation checkpoint** was
**79.0%**, not a current M3 coverage measurement. Normal and shuffled package runs
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
| M1: Foundation release gate | Complete and merged | Approved amendment, independent GO reviews, and passing protected PR #9 checks before merge |
| M2: Reviewed foundation checkpoint | Complete | Scoped 66-file checkpoint secret-scanned, tested from the index export, committed, pushed, and remote identity verified |
| M3: Authoritative Lua transitions | Complete locally: 43/43 sources, sealed factory, source conformance and full-module race verification; checkpoint `81028ca` pushed, still dormant | Complete source/pin and in-memory Go/Python/Lua checks plus the final current-tree race result, without runtime activation |
| M4: Real Redis 7 acceptance | Execution not started; first-executor findings closed and independently re-reviewed GO for image preparation; image/CI/execution-approval gates remain | Idempotency, fencing, crash, AOF, memory, and latency evidence passes on disposable infrastructure |
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
- [x] Pass the 2026-09-11 foundation aggregate normal, race, shuffled, full-module,
  vet, build, Python, formatting, and diff checks.
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
  formatting, and diff matrix against the combined final foundation tree.
- [x] Obtain a new independent GO/NO-GO review after the protocol blocker and
  every implementation finding are closed.
- [x] Complete the scoped credential-signature and sensitive-surface review;
  repeat the scan on the exact M2 checkpoint scope before publication.
- [x] Obtain explicit commit/push authorization for the M2 checkpoint.
- [x] For the completed M2 checkpoint, stage only the F3 normative contract,
  fixture, verifier, plan/status consolidation and `crawljobsv2` files; leave
  unrelated worktree changes untouched.
- [x] Commit and push the reviewed foundation remediation checkpoint.
- [x] Begin dormant Lua source work after M1/M2 and explicit M3 authorization.
- [x] Implement all 43 canonical transitions and the complete sealed,
  zero-argument authoritative-bundle factory with independently verified pins.
- [x] Pass local normal source conformance and the latest full normal Docker
  suite; obtain scoped code and final-byte security GO reviews.
- [x] Record the final current-tree full Spider module race pass and duration;
  do not reuse historical race passes as current evidence.
- [x] Commit and push the scoped M3 checkpoint, verifying the exact staged
  snapshot and matching local/remote commit identity at `81028ca`.
- [x] Inspect the four M4 fixture blockers against the current protocol and
  source, and draft the 2026-09-21 readiness decision package.
- [x] Obtain owner approval for D1-D4 and implement the normative amendment.
- [x] Implement the offline preparation compiler and adversarial/interoperability tests.
- [x] Implement and locally test the bounded ledger-smoke execution slice.
- [x] Obtain separate correctness/security review and reproduce its four findings.
- [x] Resolve COR-1/COR-2 and SEC-1/SEC-2, add regressions and obtain re-review.
- [x] Correct the closed-peer follow-up found during re-review and obtain final
  correctness/security GO for image preparation only.
- [ ] Complete amended-artifact and execution-harness review, then obtain explicit
  M4 execution approval before starting disposable Redis acceptance.

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

### Current local implementation (2026-09-17)

All 43 operations in the existing [implementation groups](#implementation-groups)
are implemented. This includes BOOT plus four other administrative operations;
ENQUEUE_BATCH/AUDIT_RUN_BATCH plus the 13 other operations across the Run
lifecycle and Maintenance rows; all 12 worker/request operations; and all 11
stage/commit operations. Support chunks are not additional canonical operations.

- Shared core revision 4 supplies clocked decoding, private read receipts,
  authenticated derived-key grants, inert descriptor planning, memory admission
  and ACL sealing. Run/Job/Request/Stage/Admin/Maintenance modules implement the
  operation-specific state and replay predicates. Recovery accounting preserves
  the distinction between the immutable job source group and the reservation's
  charged request group.
- Reviewed fixes cover historical blocked-after-I/O RETRY reasons after
  availability returns, dedicated backpressure timestamps without inventing
  unrelated Job/Run timestamp updates, and post-abort bounds based on actual
  descriptor G, including retained retry-history replacement costs. In-memory
  tests exercise the real planners and canonical/fragment multi-operation chains.
- `scripts/generate-crawl-jobs-v2-lua.py` assembles 42 exact sources and preserves
  BOOT as an unchanged passthrough. `scripts/generate-crawl-jobs-v2-bundle.py`
  independently requires the literal 43-file inventory and exact normative
  document bytes, then pins source names, Redis SHA-1, source SHA-256, ordered
  source-set digest, contract digest and bundle seal. Missing/extra/stale sources
  or pins fail closed; there are no placeholder operations.
- `AuthoritativeScriptBindingSet()` constructs the complete sealed Go binding
  set only from embedded bytes checked against those fixed pins. The private
  SCRIPT LOAD preparation/reply helpers retain the same sealed source/retry
  identity but do not perform I/O or fresh connection/boot/marker checks.
  Dispatch remains unwired.

From the repository root, both read-only checks pass:

```bash
python3 -B scripts/generate-crawl-jobs-v2-lua.py --check --require-complete
python3 -B scripts/generate-crawl-jobs-v2-bundle.py --check
```

The [shared Lua core guide](../services/spider/internal/database/crawljobsv2/lua_src/README.md)
records the cumulative API and module responsibilities. Complete source identity
and local normal acceptance do not authorize runtime activation or satisfy M4.

### Current local verification (2026-09-18)

This is local dormant-worktree evidence, not protected-PR or real-Redis
acceptance. The current full-module race run passed independently of the
historical foundation/initial-slice results.

| Check | Recorded result and scope |
|---|---|
| Python tests | PASS: 13/13 |
| Independent digest verifier | PASS: 40 named positives, 157 negatives and one baseline; guards explicitly bind the canonical bundle |
| Strict generators | PASS: exact 43-source assembly inventory and fixed bundle pins; read-only commands above |
| Formatting and workflow lint | PASS: scoped whitespace, `gofmt -d` and actionlint checks |
| Earlier full normal Spider suite | PASS: 687.941 s, before the parallel/context test revision; not the latest revised-tree result |
| Latest Docker full normal Go suite | PASS, freshly executed after the final harness changes: `go test -timeout 30m -v ./...`; test step 391.9 s, total build 404.31 s |
| Docker static build check | PASS: `docker build --check`, no warnings |
| Local Docker image | `sha256:f7c6a77769fcdb67fbf0c8918a0792ca4496db3f2f54e63511fc84f650a153a6`; `linux/arm64`; runtime user `65534:65534` |
| Scoped Gitleaks scans | PASS: Gitleaks 8.24.3 over current V2 source/tests, scripts, both changed workflows and this plan; zero findings, not a repository-history scan |
| Independent code review | Scoped in-memory GO for both canonical sources and fragments, including multi-operation chains |
| Independent security review | Final-byte GO; bundle-freshness finding cleared |
| Final current-tree full Spider race suite | PASS: `go test -mod=readonly -json -race -timeout 90m ./... -count=1`; `crawljobsv2` 2571.353 s (42 min 51 s); all 12 packages with tests passed, no race reports |
| Final full-module vet | PASS: `go vet -mod=readonly ./...` |

The earlier full race attempt failed a worker VM's 15-second context and then
reached the 60-minute package timeout while most Lua tests still ran serially.
A focused profile attributed less than 1% of CPU time to compilation; execution
and race-instrumentation costs dominated, so no compilation/result cache or
replacement validator was introduced.

All 306 new Lua top-level tests now run in parallel with isolated VMs/fixtures;
state-sharing subtests remain sequential. The shared `luaTestContext` uses
`t.Context()`, the Go test deadline when present, and cleanup cancellation instead
of independent short VM deadlines. External Python/native-Lua process timeouts
remain unchanged. The former failing capacity-recovery case passed twice with
two CPUs under the race detector (71.31 s and 74.58 s), followed by the full
module pass. CI retains every package/test under `-race` with a 90-minute package
budget, and required checks now also run all 13 independent Python unit tests.
No payload, assertion, exhaustive case or race instrumentation was removed.
These are harness changes, not a relaxation or measurement of the real-Redis
maximum-shape p99 requirement of strictly less than 100 ms.

The retained local race JSON records zero skipped `crawljobsv2` tests. Only the
existing opt-in V1 Redis and Chromium integration tests skipped elsewhere in
the module; both integration environment variables were explicitly unset.
The two helper packages without test files are not missing conformance cases.

The Docker log is clipped at 2 MiB, so it does not provide a complete per-test
count or skip inventory. Opt-in Redis V1 and LuaJIT skips were observed.
Render Worker opt-in skips are also expected when not enabled, but are not
claimed as observed in that clipped log. The build starts no application,
service or real Redis instance, and ignored NLTK data did not enter the build
allowlist. The image is local build evidence, not an approved release image or
permission to run it.

**Local M3 result:** Source implementation and in-memory verification are
complete. Verification does not itself grant publication, protected-PR
acceptance, M4 approval or operational acceptance. The separate checkpoint
publication authorization is recorded above. No application/service was started.

### Initial local slice (2026-09-15)

Historical evidence for the initial 1/43 slice, not the current source inventory
or current race result. Implemented and independently reviewed at that checkpoint:

- Proposed `CJ2_APPROVE_BOOT` Lua: exact initial/planned/unclean approval and
  replay, actual server identity, evidence age and nonce validation, bounded
  complete durability reads, one TIME call, ACL preflight, and one fully prebuilt
  HSET. It reads/mutates no other datastore key. Its in-memory test facade
  explicitly checks no mutation on rejection and does not assume rollback after
  an unexpected mutation error.
- Side-effect-free Lua numeric/TIME decoding, UTF-8 checking, U64/F/RECORD/SECTION
  framing and bounded SHA-256 primitives. Tests compare actual Lua with Go
  codecs, Go SHA-256, and shared fixture values. This support file is not an
  additional operation or a runtime module loader; URL/IDNA semantics are not
  implemented by these primitives.
- Private Go SCRIPT LOAD preparation and exact returned-SHA verification bound
  to the complete originating bundle seal, with defensive retry copies and
  redaction. Invalid UTF-8 script sources are rejected. These helpers perform no
  I/O and do not prove fresh connection/boot/marker rechecks; actual dispatch
  remains unwired.

The slice passes the pinned Go 1.25.13 full package and full Spider module
tests, a full package race run, three shuffled package runs, full-module vet,
package build, formatting, and scoped whitespace checks. The independent Python
digest verifier passes. The BOOT, primitive, and loader tests execute in memory;
no real Redis instance or acceptance fixture was started. Existing synthetic Go
bundle factories are still test-only identity controls, not source acceptance.

**At the 2026-09-15 checkpoint, M3 was not complete.** There was no production
authoritative bundle/accessor or generated complete 43-source manifest/seal,
and there were no placeholders for missing operations. The proposed BOOT file
was embedded only by tests. The remaining transitions, URL/IDNA validation,
shared memory/ledger planners, canonical-source Go/Python/Lua execution coverage
and complete bundle digests still needed implementation. Those historical tests
did not establish the then-missing gates; the current implementation and final
race pass are recorded above.

### Deferred real-Redis fixture decisions

The following records the original four blockers. D1-D4 were approved on
2026-09-21 and amended normative sections 5.1/17.7 now define their resolution.
Actual execution still requires completed harness/artifact review and explicit
approval; these historical descriptions are not the current amended rules:

- Active gate validation checks candidate-key absence, but the current fixture
  credential wording prohibits candidate-key access. Required read-only absence
  checks versus prohibited writes need a reviewed clarification.
- The harness owns its setup manifest, while unchanged Lua has no manifest
  input or fixture mode. Clarify responsibility for manifest-key enforcement
  without adding a Lua bypass.
- The provisional harness forbids candidate installation/retirement/promotion,
  while administrative source acceptance needs a noncircular isolated test
  procedure before final release evidence exists. No alternate administrative
  fixture or synthetic final production approval is authorized here.
- The provisional zero-evidence GuardCore exception does not waive compatibility
  validation or production `ZERO_SHA256` rejection. Resolve the fixture's
  compatibility/bootstrap path explicitly; do not manufacture final evidence or
  weaken normal authority checks to make the harness run.

### M4 readiness decision package (2026-09-21)

**Status: owner-approved on 2026-09-21; amendment and offline layer implemented
locally.** The owner explicitly approved these proposals and continued
implementation. The decision rationale below is retained; amended normative
sections 5.1 and 17.7 now control. The administrative profile and nonzero test
bootstrap are explicit test-only exceptions. Their approval does not authorize
a real-Redis run.

#### Source-grounded blockers

Paths in this table are relative to
`services/spider/internal/database/crawljobsv2/`.

| Decision | Observed conflict | Source anchor |
|---|---|---|
| D1: candidate absence and ACLs | Active gates must inspect candidate/freeze absence, while section 17.7 forbids fixture credential access to candidate keys | `lua_src/gate.lua`, `G.check`, active branch; protocol sections 5 and 17.7(3) |
| D2: manifest enforcement | Section 17.7(2) attributes rejection of unlisted fixture keys to unchanged Lua, but the wire has no setup-manifest input | `lua_src/wire.lua`; protocol section 10.1.1's closed KEYS/ARGV layouts |
| D3: administrative acceptance | The source inventory includes INSTALL/RETIRE/PROMOTE, but the only real-Redis fixture exception prohibits them | `script_bundle.go`, `AuthoritativeScriptBindingSet`; protocol sections 5.1 and 17.7(3) |
| D4: bootstrap authority | Section 5.1 requires at least one zero evidence digest, but the normal stored-guard and transport paths reject it | `authority_records.go`, `GuardCore.validate`, `NewStoredCommitGuard`, `DecodeStoredCommitGuard`; `transport_gate.go`, `populateActiveGate`; `lua_src/schemas.lua`, `valid_guard`; `lua_src/identities.lua`, `I.digest` |

`ProvisionalGuardCore` is a separate Go serialization type, not a usable active
transport authority. Encoding its bytes by hand does not fix D4: Lua independently
rejects the zero fields. The in-memory source tests do not establish that the
section 5.1 bootstrap can run on Redis.

#### D1 proposal: command-scoped key access, with absence-only semantics

Amend the fixture restriction to permit the exact candidate/freeze absence
checks required by the normal active gate. For a ledger executor, the only
content-level operation on these three names is `TYPE`, with `none` required:

- `mifolyo:contracts:candidate`
- `mifolyo:crawl:v2:contract:candidate`
- `mifolyo:crawl:v2:admin_freeze`

ACL design must account for the outer `EVALSHA` as well as inner Redis calls.
Redis 7.2's [EVALSHA command specification](https://github.com/redis/redis/blob/7.2/src/commands/evalsha.json)
marks declared keys `RW`, `ACCESS`, and `UPDATE`. Simply granting `%R~` on
candidate keys is therefore not a sufficient design for the existing wire.
This is source-level guidance, not verification of the eventual pinned Redis
image.

Propose separate Redis 7 ACL selectors: an `EVALSHA`-only selector admits the
exact declared key inventory, a read selector admits the required authority
reads, and mutation selectors admit only the relevant data keys. The outer
selector grants no `HSET`, `SET`, deletion, expiry, or rename command. No broad
root selector may recombine those commands with authority-key write access.
BOOT and administrative test executors receive separate short-lived roles.

The first approved Redis experiment must prove this selector composition on
the exact image: normal active `EVALSHA` succeeds; direct candidate/freeze
reads beyond `TYPE`, writes, deletion, expiry, and rename fail; administrative
Lua cannot mutate those keys using the ledger credential. Presence of any of
the three keys rejects an active transition without writes. Apply the same
command/key separation to active authority records and any operation-specific
legacy absence checks. If this cannot be enforced, stop and revise the ACL/wire
design; broad marker write grants are not an automatic fallback.

ACLs still do not enforce script-only mutation of permitted data keys. The
reviewed immutable harness owns the operation/source allowlist, as the existing
section 10.1 trust model requires.

#### D2 proposal: harness owns setup scope; Lua owns transition scope

Replace section 17.7(2)'s unlisted-key assertion with two separately tested
responsibilities:

1. **Harness admission:** before direct setup, validate a bounded, immutable
   setup manifest listing exact key names, types, field/value bytes, expiry
   rules, and their digests. List required-absent keys separately. Redis-derived
   setup times must be recorded once in a finalized manifest before the writes
   that use them; no caller wall time substitutes for Redis time. Reject missing,
   extra, duplicated, wrong-type, oversized, or mismatched setup entries. Verify
   the complete installed inventory before revoking setup access.
2. **Lua admission:** exercise the unchanged wire's exact supplied keys and
   authenticated derived-key rules. Lua rejects invalid transition keys and
   identities; it does not parse or enforce an external fixture manifest.

Each case also declares a bounded inventory of possible derived output keys,
including initially absent jobs, stages, reservations, page/image publications,
and backlinks. Compare actual post-state against that independently derived
inventory. The ACL admits only the case's necessary keys/commands. Post-state
checks detect scope violations; they do not replace pre-write authorization.
Negative stored-state cases use their own reviewed setup manifest and fresh
fixture, not restored setup access during a measured sequence.

Keep the manifest outside the production Redis grammar. Add no Lua argument,
runtime fixture flag, key family, or script bypass. Determinism means identical
bytes for identical captured inputs, not equal timestamps across independent
Redis instances.

#### D3 proposal: separate ledger and administrative conformance profiles

Extend the fixture exception to two explicitly non-deployable profiles, using
different fresh volumes and credentials:

| Profile | Setup and allowed measurements | Required exclusion |
|---|---|---|
| Ledger | Preliminary persistence probe, real BOOT approval, bounded direct active-control/data setup, then canonical active transitions, replay, memory, latency, and crash tests | Candidate installation/retirement/promotion cannot mutate state with the ledger executor |
| Administrative | Preliminary persistence probe, real BOOT approval, bounded synthetic legacy/data setup, then canonical INSTALL, candidate run preparation/audit, RETIRE, PROMOTE, and their replay/failure paths | No retained V1 data, deployable crawl-admin, production release artifacts, or operational migration |

The administrative profile covers both `fresh` and `v1_migration` shapes. It
derives backup, stopped-writer, empty/nonempty legacy, and retirement evidence
from its actual isolated test state. Successful candidate and promoted records
must be produced by the scripts under test, not directly installed as proof of
success. Direct malformed-state setup is separately identified as a negative
fixture. After test promotion, only bounded ledger assertions are permitted;
the resulting Redis state is destroyed with the fixture.

This explicitly changes the current blanket prohibition on fixture candidate
operations. Production candidate installation still requires final reviewed
release evidence. Synthetic administrative conformance is not a production
cutover or rollback rehearsal; those remain M6/M7 gates.

#### D4 proposal: distinguish test inputs from acceptance evidence

Recommend replacing the zero-sentinel *executable bootstrap* with a reviewed
test-only artifact profile that the ordinary nonzero validators can consume.
Retain zero-sentinel cases as rejection/serialization controls; do not let
`ProvisionalGuardCore` become a production `StoredCommitGuard`.

The proposed test profile has these requirements:

1. Use the actual canonical contract/source bundle, exact Redis version/config,
   real maximum-shape input digest when available, and real preliminary
   persistence-probe evidence for BOOT. BOOT evidence is never synthetic.
2. For a measurement unavailable before execution, use the nonzero SHA-256 of
   an explicit, reviewed **test-input descriptor**, not a fabricated result.
   Its domain is `mifolyo:crawl:v2:m4-test-input:v1`; it names the evidence field,
   scenario, contract/source/config identities and `purpose=conformance_only`.
   Its bytes state `measurement_status=not_measured` and contain no PASS claim.
   The harness permits only the four currently provisional evidence fields to
   use this profile. An outer immutable fixture envelope records every such
   substitution and the resulting guard/compatibility digests.
3. Keep all ordinary RECORD fields, digest formulas, cross-binding, and nonzero
   validation unchanged. No Lua branch recognizes the fixture envelope or a
   test mode. A nonzero hash proves identity, not measurement or release approval.
4. Resolve the image bootstrap explicitly: final M5/M7 participant images do not
   yet constitute an accepted V2 release. For missing roles, the proposed
   exception permits actual immutable digests of reviewed test-only stand-in
   images, mapped by role in the outer envelope. Do not invent OCI digests,
   represent V1 images as V2-compatible, or start those role services. Rendering
   remains `disabled`. Test compatibility proves record binding only.
5. The harness must be unable to select a production target, accept arbitrary
   marker bytes, or export an installable release bundle. Its reviewed fixture
   recipes and artifact pins select the test records. Production artifact
   admission must positively require the independently reviewed final evidence
   and participant image identities; rejecting only zeros is insufficient.
   Final assembly rejects any test descriptor, stand-in role, fixture envelope,
   or test guard/manifest identity in the release authority chain. Ordinary
   structural Go/Lua codecs alone cannot identify a nonzero synthetic digest.
6. Export actual measurements in a separate evidence bundle, bind them to the
   exact test inputs, and complete teardown before considering them for release
   assembly. M7 builds a new final guard/manifest from accepted measurements and
   final images, never by promoting or restoring fixture state. Record which
   evidence must be rerun when source, configuration, shape, ACLs, or relevant
   participant behavior changes.

This supersedes the current fixture builder's all-nonzero prohibition and the
requirement that final target images exist before M4. It also changes the claim
that every synthetic artifact is rejected by Lua itself: rejection of test
provenance belongs to release/image admission, while Lua validates exact trusted
artifacts. These amendments were approved by the owner on September 21. Replacing
zero fields with arbitrary hashes outside the approved descriptor recipe is
still prohibited.

#### Approval and implementation sequence

| Step | Deliverable | Exit gate |
|---|---|---|
| M4-P0 | This decision package with D1-D4 and source anchors | Owner reviews and explicitly approves or revises the proposed amendments; no Redis starts |
| M4-P1 | Amend normative sections 5/5.1, 10.1/10.1.1, 17.7, affected image/bootstrap wording and section 18 harness scope; align Go/Python fixtures and independent oracles | Current-document contract/bundle/fixture identities regenerated and checked; production zero rejection retained; scoped review and relevant normal/race tests pass |
| M4-P2 | Implement a non-shipped harness under proposed `tests/crawl-jobs-v2-redis/`, fixture-envelope/setup schemas, offline validation, selector matrix, pinned image/config, and evidence/teardown reporting | Review deterministic setup, source-only loading, role separation, hard bounds, zero external networking, and failure cleanup; explicit approval names the first real-Redis slice |
| M4-P3 | Fresh Redis bootstrap, ACL feasibility, BOOT, one minimal active transition/replay, and credential/volume teardown | Real results prove the harness can exercise canonical sources without relaxed validators or marker-write privileges |
| M4-P4 | Full ledger and administrative matrix, restart/AOF/crash injection, maximum-shape memory and timing | All required cases pass on the same reviewed artifacts, with measured evidence and no skipped required cases |
| M4-P5 | Exact-revision protected PR checks and evidence review | M4 accepted; any M5 work still requires its stated F4-F6 dependencies |

M4-P1 changes normative document bytes, so even a source-neutral amendment changes
the contract and bundle pins. Use the existing strict generators and independent
verifier; update the current identity table only after approved regeneration.
The September 18 identities and passes remain historical M3 evidence, not a pass
for an amended contract. PR publication and merge retain their existing explicit
authorization requirements.

The first execution approval should name the exact commit, Redis and harness
image digests, ACL/config and fixture-envelope digests, local-only transport,
resource/time limits, cases, evidence destination, and teardown procedure. A
same-volume crash restart is allowed within one case; cross-case or cross-run
volume reuse is not. A proposed networkless Unix-socket transport must prove
that its private socket mount reaches only this fixture Redis, with no host or
Docker socket access inside the executor. The lifecycle controller alone owns
the exact disposable resources and handles cleanup on error or interruption.

#### Acceptance coverage and evidence ownership

The existing required-test list below remains necessary. Expand it into a
machine-readable case inventory before implementation, mapping every normative
section 17 requirement to a test and evidence artifact:

- Cover all 43 operations and all 52 allowed gate variants, including candidate
  phases and receipt-only replays. Shared Go/Python vectors and an independent
  post-state oracle must agree with canonical-source Redis execution.
- Separate protocol state tests from later application integration. M4 may kill
  a synthetic worker at a recorded START or simulated fetch boundary; actual
  Spider DNS/fetch/render, consumer persistence, Monitoring, and image startup
  behavior require M5/M6 evidence. No application-level requirement is marked
  passed merely because a ledger simulation passes.
- For internal commit-boundary crash tests, identify how each boundary is
  observed without editing canonical Lua or changing production Redis semantics.
  Random process kills or kills between client calls alone do not prove every
  internal write boundary. Unobservable boundaries remain an acceptance blocker
  until a reviewed method exists.
- Use real Redis time and original lease/retry/expiry constants. Record the
  target-image ACL behavior, restart run ID, boot approval, acknowledged-write
  receipts, lost-response reconciliation, and restored key/state equality.
  A damaged AOF must fail closed rather than being silently repaired.
- Keep the section 2.2 memory floors: isolated retained/downstream budgets at
  most 128 MiB combined, Redis `maxmemory` at least 400 MiB, and container limit
  at least `maxmemory + 128 MiB`. Measure allocator growth and all maximum
  reservation/stage/queue shapes; serialized sizes alone are insufficient.
- Record benchmark methodology, hardware/architecture, exact inputs, warmups,
  sample count, per-operation p50/p95/p99/max, and measurement overhead. Separate
  server script timing from client round-trip latency. The maximum-shape Lua
  p99 remains strictly below 100 ms over at least 1,000 complete
  command-write-to-reply-read samples including AOF, as normative section 3
  requires. Redis CPU time is reported separately and cannot replace that gate.
  A reduced shape, higher Lua time limit, or cached result cannot satisfy it.
- Export a bounded report containing exact source/contract/image/config/ACL
  identities, setup and test artifact digests, per-case outcomes, measurement
  artifacts, and teardown receipts. Exclude passwords, tokens, and raw secrets.
  Retain failed-case evidence with a failing verdict; cleanup failure invalidates
  acceptance. Revoke every fixture identity and prove reconnect failure while
  Redis is reachable, then prove destruction of its container, volume and private
  transport resources.

#### Review checklist

- [x] D1 selector design and exact absence-read semantics approved.
- [x] D2 harness/Lua responsibility split and derived-key inventory requirement approved.
- [x] D3 isolated administrative exception approved for both cutover shapes.
- [x] D4 nonzero test-artifact and stand-in image exception approved, including
  positive release-provenance checks and required rejection tests.
- [x] Independently review the first execution slice for correctness and security;
  both reviews returned NO-GO, recorded in the dated report.
- [x] Close all four first-executor findings and the closed-peer follow-up;
  obtain scoped independent GO for image preparation only.
- [ ] Normative amendment and regenerated identities independently reviewed.
- [ ] Harness, evidence schema, crash-boundary method and benchmark plan reviewed.
- [ ] Explicit M4-P3 execution approval recorded against exact artifacts.

The checked decisions record owner approval and scoped first-executor review.
Broader M4 protocol/evidence review and actual execution approval remain separate
unchecked gates; local GO does not close them.

#### M4-P2 implementation checklist

The owner requested the next implementation slice after the offline pass.
This authorizes implementation and local testing; M4-P3 execution remains gated.
Checked implementation items below record code and local fake-backed tests, not
observed Redis behavior or completion of the full M4 matrix.
The original review found defects despite those passes. Remediation and final
independent re-review now close the items below; image preparation is next.

- [x] Apply the approved normative amendment and regenerate dependent identities.
- [x] Implement deterministic offline plans, setup projections and validation.
- [x] Pass the offline Python, independent Go/Python, full normal and scoped race checks.
- [x] Define the first bounded ledger-smoke case and exact per-role key/command ACLs.
- [x] Implement bounded Unix-socket RESP transport and canonical-source loading.
- [x] Implement real-image/configuration/isolation admission with no implicit pull.
- [x] Implement a new-volume persistence probe, SIGKILL/restart and canonical BOOT.
- [x] Implement the minimal active operation/replay path with complete state checks.
- [x] Implement bounded evidence and failure/interruption cleanup, including
  credential revocation/reconnect and exact-resource destruction verification.
- [x] Test transport, admission, ambiguous failures and cleanup using local fakes.
- [x] Complete independent first-executor correctness/security review (NO-GO).
- [x] COR-1: terminate and wait for an in-container stage before revocation after
  a timeout; add an executor-side stage deadline.
- [x] COR-2: terminate surviving members of the owned subprocess group even when
  its leader has exited.
- [x] SEC-1: require observed peer disconnection for held-session termination;
  reject Redis error replies and ambiguous timeouts as proof.
- [x] SEC-2: enforce approval expiry after slow preflight and at every Docker
  mutation dispatch, preserving a separate cleanup budget.
- [x] Replay all four counterexamples, pass new regressions and obtain independent re-review.
- [x] Fix the closed-Unix-peer follow-up with a bounded receive-only probe;
  retain data/error/timeout rejection and pass real local IPC regression/re-review.
- [x] After GO review, build and validate immutable images and record exact source/memory evidence.
- [x] Independently review target-discovered image and CI corrections; close the nested-skip finding.
- [x] Obtain owner authorization for scoped checkpoint commit/push and draft PR #10 update.
- [ ] Publish the scoped checkpoint and pass all applicable protected CI contexts on that revision.
- [ ] Record separate approval for the exact first real-Redis case/artifacts.
- [ ] Run that approved first case and review actual probe, ACL and teardown evidence.

The initial active operation is the empty-inventory
`CJ2_MAINTAIN_RATE_SCOPES` path. The approved run will test active transport and
no-write replay after a real BOOT mutation; it cannot establish job/claim/stage/commit acceptance.
Administrative and maximum-shape cases remain subsequent M4 work.

**Historical pre-amendment verification:** Read-only Lua assembly with both `--check` and
`--require-complete`, bundle pin (`--check`), and independent digest-vector
verification passed on 2026-09-21; `git diff --check` passed. The bundle still
reports the September 18 contract/source/seal identities, 43/43 operations, and
40 positive/157 negative cases plus baseline. These checks validate the existing
source/fixture identities, not the proposed bootstrap design. No Redis instance
was started and no real-Redis acceptance result is claimed.

#### Implemented offline scope (2026-09-21)

`tests/crawl-jobs-v2-redis/harness.py` compiles three closed planning recipes,
canonical nonzero test descriptors, guard/compatibility RECORDs and a bounded
ledger-authority setup projection. Validation recomputes every byte against
current source/contract/generated pins. Unknown fields, changed records, zero
guards, unauthorized setup keys, stale artifacts and ACL-token injection fail.
There is no endpoint, Redis client, process launcher, setup writer or release
exporter. Every plan declares execution false, measurements absent and
image/isolation verification pending.

At this initial offline checkpoint, the inventory has 43 operations/52 variants and content-bound section 17
requirements, all explicitly unexecuted. The ACL compiler produces ledger
fragments only; complete per-case/derived-key and provisioning/BOOT/admin roles
remain execution-layer work. `redis.conf` pins configuration bytes without
claiming an image exists or was tested. Administrative success states are never
directly generated as setup evidence.

Ten adversarial Python tests and `TestM4OfflineArtifacts` exercise schema,
provenance, setup and ACL failures, compare independent Python guard formulas,
and pass generated artifacts through normal Go codecs/transport and the literal
52-variant oracle. The Python suite is wired into protected `required-tests`;
the Go cross-check is part of normal Spider tests. Docker includes the three
required inputs in its builder allowlist only. See the
[harness README](../tests/crawl-jobs-v2-redis/README.md) for commands, schemas and
limits. Local tests do not establish actual image, Redis ACL, BOOT, isolation,
measurement or teardown behavior.

M4-P1 is implemented locally, pending review. The later first-case execution
slice below extends M4-P2 beyond this initial offline checkpoint. M4-P3 still
requires explicit execution approval after applicable artifact/review gates pass.

#### Local amendment and offline verification (2026-09-21)

| Check | Result |
|---|---|
| Offline harness Python suite | PASS: 10/10, including independent Python guard formulas and adversarial artifact/setup/ACL cases |
| Existing Python suite | PASS: 13/13 |
| Full Spider normal suite | PASS: `go test -mod=readonly -timeout 30m ./... -count=1`; final V2 package duration 155.473 s, cached pinned Go 1.25.13 |
| Scoped race suite | PASS: M4 cross-language test, normative wire oracle, all-constructor oracle, and matching guard/transport/canonical-bundle tests; 12.881 s; not a new full-module race claim |
| Full-module vet and Go formatting | PASS: `go vet -mod=readonly ./...`, changed-file `gofmt -d` |
| Strict Lua assembly, bundle pins, independent digest verifier | PASS: 43/43 sources; 40 positive/157 negative shared cases plus baseline; identities recorded above |
| Inventory CLI | PASS: 52 operation/gate entries and 104 section-17 requirement entries, explicitly unexecuted; execution authorization false |
| Whitespace | PASS: `git diff --check` |

The final scoped race command was:

```text
GOPROXY=off GOTOOLCHAIN=go1.25.13 go test -mod=readonly -race -timeout 15m ./internal/database/crawljobsv2 -run 'Test(M4OfflineArtifacts|ProtocolWireLayoutReviewedOracle|OperationWireAllConstructorsReadOnlyOracle|.*(GuardCore|StoredCommitGuard|TransportGate|CanonicalBundle|AuthoritativeScriptBinding).*)$' -count=1
```

The new normal/race evidence is local. Docker packaging was updated but no new
image build is claimed; workflow lint tools were unavailable locally, and
protected PR checks were not run. Existing opt-in integration tests were not
activated. No real Redis instance, lifecycle fixture or acceptance run was
started. Independent amendment/implementation review was still pending at that
checkpoint; the later first-executor review result is recorded below.

#### First execution slice: local implementation and verification

The owner requested updating the checklist and implementing the next slice.
`controller.py`, `executor.py`, `runtime_case.py` and `resp.py` now implement
`ledger-smoke-v1`. The new execution Dockerfile has a narrowly allowlisted build
context; it has not been built or assigned a reviewed image identity here.

The case uses six separate ACL roles, a pre-BOOT acknowledged persistence probe,
SIGKILL and same-volume restart, canonical BOOT plus exact replay, four bounded
authority setup writes, and two empty `CJ2_MAINTAIN_RATE_SCOPES` calls. Complete
key/state snapshots must remain identical across maintenance/replay and 21
candidate/freeze ACL denials. Setup/loader/BOOT access is revoked before that
measurement. Direct administrative success, jobs, claims, stages, commits,
maximum shapes and benchmarks are outside this first case.

The controller requires a hash-bound, unexpired approval, the exact clean tracked
commit, and already-local immutable Linux image IDs. It uses only the local
Docker Unix socket, new owned named volumes, network mode `none`, explicit
resource/user/capability settings and no host bind mounts or implicit image pull.
The executor receives only its private Unix socket/control volume. An init helper
alone has UID 0 and CHOWN to initialize empty volume ownership; execution uses
UID/GID 65534 with all capabilities dropped.

Evidence includes a durable pre-mutation `INCOMPLETE` intent, inspected container
identities/settings, probe/BOOT/setup observations, state comparisons, ACL
denials and credential/destruction receipts. SIGINT/SIGTERM enter bounded cleanup;
lost-create replies still leave exact resource names eligible for ownership-
checked cleanup. Every failed or unproved teardown invalidates the case. An
uncatchable controller/host failure leaves an incomplete intent for recovery,
not acceptance evidence. Reports never mark M4 accepted; fake backends cannot
produce valid real-Redis evidence.

Local verification for this slice:

| Check | Result |
|---|---|
| Python offline/execution suites | PASS: 26/26 (10 existing offline tests plus 16 execution/transport/lifecycle tests), using fakes and bounded local subprocess controls only |
| Independent Go wire/RESP cross-check | PASS: canonical BOOT and active-maintenance command parts and serialized sizes agree with normal Go constructors, alongside the existing artifact/gate checks |
| Targeted Go race check | PASS: `go test -mod=readonly -race -timeout 15m ./internal/database/crawljobsv2 -run '^TestM4OfflineArtifacts$' -count=1`; 3.220 s |
| Full-module vet and changed Go formatting | PASS |
| Bundle pins and independent digest verifier | PASS; normative document, 43 canonical sources and their current identities unchanged by this execution slice |
| Read-only recipe CLI | PASS: one named case, six roles, two canonical sources; execution authorization false |
| Whitespace | PASS: `git diff --check` |

The initial wire test compared Go nil slices with Python zero-length bytes using
structural slice equality. It was corrected to byte equality because both encode
the same required zero-byte RESP bulk string; every command part and total RESP
size remain independently checked. No protocol or runtime validator was relaxed.

This is local implementation evidence, not a Docker build, protected PR result,
independent review or real-Redis run. The subsequent independent review below
returned NO-GO; remediation and re-review now precede image preparation. See the
[execution harness README](../tests/crawl-jobs-v2-redis/README.md#first-execution-slice-ledger-smoke-v1)
for the lifecycle, approval schema and remaining matrix.

#### Initial independent first-executor review (2026-09-21; historical NO-GO)

Separate Code Reviewer and Security Engineer sessions inspected the current
worktree and reproduced four findings. The coordinator replayed every finding
using the same implementation bytes and local fakes. The consolidated verdict
is **NO-GO for advancing this revision to image preparation or real-Redis
execution**. Existing 26-test Python and Go wire/race checks still pass; they do
not cover the reproduced cases.

| ID | Severity | Required correction |
|---|---|---|
| COR-1 | HIGH | Timed-out `docker exec` may outlive the attaching CLI and overlap revocation; quiesce the actual worker before teardown |
| COR-2 | MEDIUM | An exited subprocess leader causes `killpg` to be skipped even when children hold pipes open |
| SEC-1 | MEDIUM | Held-session errors/timeouts are accepted as termination without observing peer disconnection |
| SEC-2 | MEDIUM | Slow preflight and Docker calls can dispatch Redis start after approval expires |

The [dated review report](crawl-jobs-v2-m4-review-2026-09-21.md) contains exact
file/line references, reviewed-byte identities, reproduction commands, evidence
limits and regression expectations. The two standalone scripts under
`tests/crawl-jobs-v2-redis/review/` preserve the counterexamples; their successful
exit means a defect was reproduced, not that acceptance passed. No implementation
fixes or normative changes were included in this review.

The scoped security scan found no production secret/private key in its thirteen
files; the sole URI-shaped candidate was a negative test without userinfo. This
was not a history or environment-secret audit. A local memory observation also
requires checking the init container's target Linux peak during later reviewed
image validation; it was not classified as a confirmed OOM finding.

#### Remediation and final independent re-review (2026-09-21)

The owner requested fixing all four findings, regression tests and independent
re-review. COR-1 now has a hard worker timer plus owned-container stop/wait/
zero-PID/remove before a differently named cleanup helper. Startup polling is
inside one timed read-only stage. Worker-first destruction also covers failed
revocation. COR-2 now terminates the owned process group on abort without first
polling/reaping its leader.

SEC-2 revalidates approval after revision verification and intent journaling,
caps execution by wall-clock approval expiry and a monotonic deadline, and
checks dispatch boundaries. The independent 60-second cleanup budget is retained
solely for quiescence, the fresh revocation helper and resource destruction.

The first SEC-1 fix correctly rejected ambiguous errors but exposed a new HIGH
correctness issue: writing PING to an already-closed Unix peer could fail with
EPIPE before observing EOF. The final probe is bounded and receive-only. Only
receive EOF/reset proves termination; buffered/live data, timeouts and local
socket errors reject, and fresh authentication must still fail appropriately.
A real local AF_UNIX socket-pair regression covers the closed-peer case.

Both independent reviewers returned **GO for image preparation only** on the
final corrected bytes, with no actionable finding left from their scoped reviews.
The [re-review report](crawl-jobs-v2-m4-rereview-2026-09-21.md) records closure,
the intermediate follow-up, exact hashes and validation limits. The original
report and dated reproductions are preserved, not rewritten as passes.

| Final check | Result |
|---|---|
| Python suite | PASS: 43 tests (26 prior plus 17 review regressions); author 20.454 s, independent correctness reviewer 19.991 s |
| Go artifact/wire race check | PASS: author 3.198 s, independent correctness reviewer 3.093 s |
| Independent security targeted suite | PASS: 12 tests, 3.456 s; additional receive-only IPC/error and expiry probes pass |
| Independent OS probes | PASS: owned process-group termination and stage exit 124 with blocked stdin/validation/output; no service or listener |
| Bundle pins/digest verifier | PASS; normative document and canonical source identities unchanged by these fixes |
| Scope | No Docker/Redis execution, image build, protected-CI run or Git publication; target semantics and resource limits remain pending |

The corrected recipe SHA-256 is
`e001d497c1bdccb061a19f265ba854812d1da4e5e551bc0d9873ae9773fefc4d`.
The read-only recipe still says `execution_authorized=false`. A future approval
must bind the corrected artifacts and exact clean tracked revision. Validate
target Linux memory, including the 128 MiB init limit, during reviewed image
preparation; local traced allocations do not certify that limit.

#### Immutable image preparation and publication gate (2026-09-21)

Actual image checks now pass for Python 3.13.15/Redis 7.4.11 on Linux/arm64.
The retained [image evidence](evidence/m4-image-prep-2026-09-21/README.md) binds
55 exact files per image check, both daemon-local image IDs, source identities,
memory observations and verified temporary-container cleanup. The emitted offline
plan SHA-256 is `bf22f79cd2b23e09aa77fa288b70c190c7c04ef24384d5c48ea5d5a885c362c4`;
the updated recipe is `2cfb26736d94c9c05188989e8da814272c7a662ac0441d045ff941c5508b078b`.
These replace the prior recipe only for future exact-artifact approval.

The first broad-context candidate was rejected/deleted. File-only Docker ignore
rules now prevent parent-directory exceptions from including unrelated trees.
The target kernel's finite DOWN fallback tunnel devices are admitted only with
no active external interface, no IPv4 routes, restricted loopback/reject IPv6
routes and unchanged capability limits. Streaming hash construction preserves
every canonical digest while reducing observed init memory from about 127 MiB
to about 47 MiB under the unchanged 128 MiB limit.

The existing PR's 90-minute full race timeout is addressed by eight deterministic
shards covering all 470 compiled V2 test roots exactly once, with actual outcome
verification and no non-allowlisted nested/root skip. The protected Spider
context fails on any missing/failed shard and still runs all other packages.
The full 90-minute per-shard budget and every race assertion remain; Redis's
separate latency gate is unchanged. A review finding in nested skip collection
was fixed and independently re-reviewed GO before publication.

Current local checks: 45 harness tests, 19 script tests, pinned bundle/digest
verification, actionlint, and actual image validation pass. Exact-index checks
and a scoped credential scan precede the authorized commit/push. PR #10 was
converted to draft as requested. Protected results and execution approval are
not inferred from these local passes. Redis ran only `--version`, never as a
server; no retained datastore was queried or changed by these checks.

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

Every operation in this unchanged 43-operation inventory is implemented locally;
the required behaviors remain subject to the separate M4 real-Redis gates.

| Group | Operations | Required behavior |
|---|---|---|
| Administrative and candidate | `CJ2_APPROVE_BOOT`, `CJ2_INSTALL_CANDIDATE_MARKERS`, `CJ2_RETIRE_LEGACY_KEYS`, `CJ2_PROMOTE_CANDIDATE_CONTRACTS`, `CJ2_MARK_PLANNED_SHUTDOWN` | Exact boot, freeze, compatibility, retirement, promotion, and nonce authority |
| Run lifecycle | `CJ2_CREATE_RUN`, `CJ2_ENQUEUE_BATCH`, `CJ2_BEGIN_RUN_AUDIT`, `CJ2_AUDIT_RUN_BATCH`, `CJ2_SEAL_RUN`, `CJ2_ACTIVATE_RUN` | Immutable run pins, source admission, complete audit, seal, and activation |
| Worker and requests | `CJ2_REJECT_READY`, `CJ2_TRY_CLAIM`, `CJ2_RENEW_LEASE`, `CJ2_RESERVE_REQUEST`, `CJ2_START_REQUEST`, `CJ2_FINISH_REQUEST`, `CJ2_CANCEL_RESERVATION`, `CJ2_RELEASE_BEFORE_IO`, `CJ2_RETRY`, `CJ2_DEAD`, `CJ2_CANCEL_JOB`, `CJ2_COMPLETE_NO_OUTPUT` | Leases, fences, reservations, request budgets, rate scopes, retries, and terminal outcomes |
| Staging and commit | `CJ2_BEGIN_STAGE`, `CJ2_STAGE_PAGE_FIELDS`, `CJ2_STAGE_PAGE_BLOB`, `CJ2_STAGE_OUTLINKS_BATCH`, `CJ2_STAGE_DISCOVERIES_BATCH`, `CJ2_STAGE_ALIASES_BATCH`, `CJ2_STAGE_IMAGES_BATCH`, `CJ2_STAGE_IMAGE_MANIFEST`, `CJ2_ABORT_STAGE`, `CJ2_SEAL_STAGE`, `CJ2_COMMIT` | Bounded immutable staging and one atomic idempotent publication/ACK |
| Maintenance | `CJ2_PROMOTE_DUE`, `CJ2_RECOVER_EXPIRED`, `CJ2_CANCEL_RUN`, `CJ2_CANCEL_BATCH`, `CJ2_FINALIZE_RUN`, `CJ2_ARCHIVE_RUN`, `CJ2_PURGE_RUN_BATCH`, `CJ2_CLEAN_STAGE`, `CJ2_MAINTAIN_RATE_SCOPES` | Bounded recovery, cancellation, finalization, retention, purge, cleanup, and rate maintenance |

### Lua acceptance checklist

Checked items record implemented behavior and proven local in-memory checks,
not real-Redis timing, allocator, durability or operational acceptance.

- [x] Implement exact key counts/grammar, ARGV grammar, transport gates, record
  shapes, constants and bounds before mutation across all 43 scripts; exercise
  them through normal in-memory source conformance and independent review.
- [x] Use Redis `TIME`, not caller wall time, in mutating scripts; verify clock
  handling in memory without claiming actual Redis script/PTTL timing evidence.
- [x] Implement and test the closed active/candidate operation matrix.
- [x] Implement full immutable replay identity checks and test operation replays
  and canonical/fragment multi-operation chains.
- [x] Expected validation/preflight rejections perform zero writes. Tests also
  verify that an unexpected execution fault propagates without rolling back
  earlier writes; this is not a blanket no-partial-mutation guarantee.
- [x] Implement and check the closed operation/status response schemas.
- [x] Independently verify the baseline, 40 named positive and 157 negative
  fixture cases in Go/Python; pass scoped Lua primitive/operation differential
  and source-chain checks. This is not a claim of real-Redis vector execution.
- [x] Regenerate and check the exact-document, complete ASCII-sorted Lua contract
  digest, ordered source-set identity, fixed Go pins and sealed zero-argument
  factory; retain the empty-Lua primitive without using it as guard authority.
- [x] Obtain scoped in-memory code GO and final-byte security GO reviews; pass
  local normal acceptance, including the latest full normal Docker Go suite.
- [x] Record a passing result and duration for the final current-tree full
  Spider module race command, with no skipped Crawl Jobs V2 tests.

## M4: Disposable Redis 7 acceptance

**Real-Redis execution not started or approved.** D1-D4 are approved; the
amendment, offline compiler and first-case executor are implemented locally.
The final independent re-review and image preparation pass for the first case.
M4 waits for exact-revision protected CI and explicit execution approval,
followed by actual first-case and full-matrix acceptance.
The [2026-09-21 readiness package](#m4-readiness-decision-package-2026-09-21)
records scope and remaining gates; it is not Redis acceptance evidence.

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
| F5 acknowledged backlink persistence | The repair passed protected PR #9 checks and merged; V2 consumer integration and retained-state work remain separate gates |
| F6 JavaScript-shell disposition | The bounded static baseline needs an approved indexing outcome without enabling rendering by default |
| Render rollout gate | JavaScript execution requires separate policy, image, sandbox, terms, robots, and authorization evidence |

F3 foundation remediation may proceed while F1, F2, F4, and F6 remain
operationally blocked. M1/M2 are merged; all 43 dormant M3 sources and the sealed
factory are implemented and verified locally, including the final full-module
race pass. M4's approved amendment and offline layer are implemented locally;
the first-case executor and immutable image validation also pass locally.
Protected CI and execution approval remain pending. The first-executor
findings and closed-peer follow-up are fixed; independent final re-review permits
image preparation only.
M5 hermetic code/consumer integration waits
for M4 and F4-F6. In the parent plan, compatible code and runbooks precede the F1/F2
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
| 2026-09-14 | PR #9 protected acceptance and merge | All 14 protected checks passed at `bb70fb4`; merged as `d914a93f9ade5b63182ecf02092c1a1e74633713`; no runtime activation implied |
| 2026-09-15 | Explicit M3 and narrow protocol-clarification approval | Existing wire layout documented; renewal shortening fails without writes; in-memory Lua conformance allowed, real-Redis fixture decisions still gated |
| 2026-09-15 | Initial M3 implementation and independent review | BOOT (1/43), pure Lua primitives, and private source-bound reload helpers pass combined normal/race/shuffle/full-module/vet/build/Python checks; code/protocol scoped GO; no complete bundle, Redis acceptance, commit, or push |
| 2026-09-17 | Complete local M3 source implementation and sealed bundle | All 43 canonical operations and zero-argument `AuthoritativeScriptBindingSet()` implemented; strict generators and canonical guard-bound 40-positive/157-negative fixture verification pass; empty-Lua foundation primitive retained |
| 2026-09-17 | Local normal acceptance and independent re-review | Earlier full normal Spider pass recorded; canonical/fragment multi-operation code review and final-byte security review return scoped GO; superseded by the September 18 final verification matrix, not real-Redis acceptance or runtime activation |
| 2026-09-17 | Initial full-module M3 race attempt | Worker VM deadline failure and 60-minute package timeout exposed harness scheduling/budget limits; not acceptance evidence; later profiled and corrected without dropping tests |
| 2026-09-18 | Final local M3 verification | Full Spider race suite passes (`crawljobsv2` 2571.353 s, zero skipped V2 tests); fresh Docker normal suite passes (391.9 s); vet, Python 13/13, digest/assembly checks, formatting and workflow lint pass; no M3 staging, commit, push, PR or activation |
| 2026-09-18 | Scoped M3 checkpoint publication authorization | Owner requested committing and pushing the verified M3 changes; unrelated worktree changes remain outside the checkpoint; no PR, merge, M4 execution or activation authorization |
| 2026-09-18 | M3 checkpoint publication completed | `81028ca12a1763d46df72fc54759d99a0ea3b561`, 151 scoped files; exact-index normal Spider tests, vet, Python tests and generator/digest checks passed; credential scan detections were triaged as four unchanged synthetic fixture values; local and remote branch identities match; no PR or activation |
| 2026-09-21 | M4 readiness preparation | Owner requested preparation; source review confirms candidate-ACL, manifest-responsibility, administrative-fixture and zero-guard bootstrap conflicts; D1-D4 amendment proposals and M4-P0 through P5 sequence drafted for review; no normative amendment or Redis execution |
| 2026-09-21 | Owner approval and offline implementation | Owner approved D1-D4 and continued implementation; normative bootstrap/ACL/manifest/admin exception amended, dependent pins/fixtures regenerated, offline compiler and adversarial/Go interoperability tests added; execution harness and real-Redis approval remain pending |
| 2026-09-21 | First bounded execution slice | Owner requested the next slice; ledger-smoke controller/executor, six-role ACLs, Unix RESP, boot/probe/restart, state checks, evidence and cleanup implemented; 26 Python tests and independent Go wire/race checks pass locally; no Docker build, Redis run or protected-PR acceptance |
| 2026-09-21 | Independent correctness/security review | Separate reviewers returned NO-GO: COR-1 HIGH and COR-2/SEC-1/SEC-2 MEDIUM; all four counterexamples replayed by the coordinator; no implementation fixes or infrastructure execution; dated report and remediation checklist added |
| 2026-09-21 | Remediation and first re-review | Original four findings corrected with 41 local tests; re-review closed the original findings but found a HIGH closed-Unix-peer/PING regression; combined readiness remained NO-GO |
| 2026-09-21 | Final independent re-review | Receive-only disconnect proof and actual local IPC regression close the follow-up; 43 Python tests and Go wire/race verification pass; correctness and security both GO for image preparation only; no real Redis or image/CI acceptance claimed |
| 2026-09-21 | Immutable image preparation | Tightened build context, target-kernel network checks and bounded-memory hashing; exact 55-file validation and memory checks pass on immutable arm64 images; all canonical digests unchanged; offline plan/recipe/evidence emitted without run approval |
| 2026-09-21 | Publication/CI preparation | Owner authorized scoped commit/push/draft PR #10 update; PR converted to draft; exhaustive eight-shard race CI addresses prior aggregate timeout; nested-skip review finding fixed and re-reviewed GO; protected results pending publication |

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
