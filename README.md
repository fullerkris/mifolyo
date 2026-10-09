# MiFolyo

MiFolyo is a community-first search and discussion platform. This repository currently contains two major foundations:

- Built off of IonelPopJara's Moogle search engine stack for crawling, indexing, ranking, and querying web pages.
- A Laravel based forum/community service scaffold for posts, comments, voting, reports, moderation, and search integration.

## Search Engine Foundation

The search stack is based on Moogle, an educational search engine inspired by early web architecture. It uses Redis for crawl queues and temporary pipeline data, MongoDB for indexed search data, PostgreSQL for MiFolyo application/community data, and Laravel for the query engine.

### Search Services

- **Spider**: Crawls pages, extracts links and images, and writes crawl data to Redis.
- **Indexer**: Builds the inverted index and page metadata in MongoDB.
- **Image Indexer**: Indexes images discovered during crawling.
- **Backlinks Processor**: Transfers backlink data from Redis to MongoDB.
- **Page Rank**: Calculates PageRank for searchable metadata using the current outlink graph.
- **TF-IDF**: Calculates term frequency-inverse document frequency weights.
- **Query Engine**: Laravel service that returns ranked search results using TF-IDF and PageRank.

The local development stack is defined in the root `docker-compose.yml` and
supports optional `pipeline`, `crawl`, and `batch` profiles. The spider is not
part of `pipeline`; its `crawl` profile defaults to policy validation only.
The root and isolated baseline Compose files explicitly set
`ALLOW_INSECURE_DATASTORES=true` for their unauthenticated local-only MongoDB
and Redis instances. Spider, Indexer, Image Indexer, and PageRank otherwise fail closed when datastore
authentication is absent; this exception must not be copied to production or
shared deployments.

### Crawl runtime and authorization status

> [!IMPORTANT]
> V1 remains the current runtime. Crawl Jobs V2 has a complete dormant M3 source
> implementation, not an activated runtime. V2 integration, migration, deployment,
> candidate promotion, rendering activation, and crawling remain unauthorized.

M1/M2 merged through PR #9 as `d914a93`. M3's 43 canonical Lua operations and
sealed source bundle, together with the reviewed M4 preparation, merged through
PR #10 as `ff2457e` after all fourteen protected checks passed on `a02991c`.
The separately authorized corrected `ledger-smoke-v1` case now **passes** real
Redis probe/restart, BOOT/replay, empty maintenance, ACL and cleanup checks. All
four containers and two volumes were confirmed absent. The original init FAIL is
preserved; the successful case's one-use approval is consumed. The separately
approved `ledger-claim-release-v1` case also **passes**: nine transitions,
46 ACL denials and complete state/expiry checks, with all six resources
independently confirmed absent and its approval consumed. It ran on unchanged
checkpoint `b4bda19`, published with all fourteen required CI checks passing and
subsequently merged through PR #11 as tree-identical `9b6b8f9`. See the
[claim/release result](docs/crawl-jobs-v2-m4-claim-run-2026-09-23.md).
The remaining M4-P3 bootstrap/ACL negative package is implemented and locally
verified: 95 harness tests and independent Go/Lua race checks pass. Independent
correctness/security reviews now both return GO with no actionable findings.
Checkpoint `6340401` was published through PR #12 with arm64/amd64 PC01 image
evidence and all fourteen required CI checks passing. Fresh exact-artifact owner
approval and a separate execution decision then authorized one refreshed PC01
attempt: **PASS**, with runtime process/admission checks and independent cleanup
verification. Its approval is consumed. Step 6 accepts PC01 as the scoped positive
control; PR #12's subsequent merge `320bce3` has the same tree. P01 preparation
and protected CI then passed on PR #13's published `963b67b`. Its separately
approved single attempt **passed** both presence rejections and all 46 authority
denials, with zero accounting changes and independently verified cleanup.
P01's scoped evidence review passed. The remaining twelve cases were then
individually approved and run sequentially: **all PASS**, with 62 exact negative
calls, seven measured positive controls, 368 ACL denials and independently
verified cleanup. All approvals are consumed; **zero negative Redis cases remain
unrun** in the package. See the [results and coverage](docs/crawl-jobs-v2-m4-negative-remainder-2026-09-24.md).
The September 25 admission reconciliation and independent reviews accept
**M4-P3 bootstrap readiness** within the frozen checkpoint scope. The full
104-requirement/43-operation/52-variant M4 inventory is recorded. The reviewed
worker-death recovery lifecycle was published as `c009282` on draft PR #15 with
all fourteen protected checks passing. Its separately approved single real case
**passed**: thirteen operations, 38 counters, 46 authority denials and actual
lease expiry after worker termination. Correctness/security evidence reviews
accepted the scoped result; all six credentials were revoked, all seven resources
independently found absent, and its one-use approval consumed. See the
[recovery result](docs/crawl-jobs-v2-m4-recovery-run-2026-09-27.md),
[source review](docs/crawl-jobs-v2-m4-recovery-review-2026-09-27.md) and
[image preparation](docs/crawl-jobs-v2-m4-recovery-preparation-2026-09-27.md).
The bounded request lifecycle is now **accepted for its single real-run scope**:
robots then document under one lease, 22 calls, 33 counters, first/document
history, exact expiry and 46 authority denials. It ran once in 12,944 ms on
merged `561774f`, after passing main CI and separate artifact/execution decisions.
Independent correctness/security reviews accepted the evidence; six-role
revocation, the 28-action journal and absence of all six resources were verified.
Its approval is consumed. See the [request run](docs/crawl-jobs-v2-m4-request-run-2026-09-28.md)
and [implementation/preparation checkpoint](docs/crawl-jobs-v2-m4-request-lifecycle-2026-09-28.md).
The positive-interval slice now has **accepted scoped real evidence** at published
`7431599`, retained through merged PR #17, after all fourteen protected checks passed. Its single
21,736-ms invocation verifies 24 calls, an exact 8-second group-first block,
four observer clock samples and due admission/replays, 33 counters and 46 denials.
Independent correctness/security reviews accept the result; six-role revocation,
the original 33-action journal and exact six-resource absence pass. The initial
approval expired unused; a separately approved renewal and execution decision
preceded the run. The renewed approval is consumed. See the
[positive-interval result](docs/crawl-jobs-v2-m4-positive-run-2026-09-30.md) and
[preparation checkpoint](docs/crawl-jobs-v2-m4-positive-interval-2026-09-29.md).
PR #17 merged as `7da55b1` with tree continuity and passing main CI. The
shared-group-capacity case has **accepted scoped real evidence** at executed
`c381287`, preserved through result publication `c8c5ee2` and PR #18's merge as
`d3b241e`. Two runs with different origins share one group slot: the single
15,186-ms invocation verifies 21 calls,
three capacity denials, 60 counters and 46 authority denials. Independent
correctness/security reviews accept the result; six-role revocation, the original
28-action journal and exact name/label absence of four containers/two volumes pass.
The initial approval expired unused; explicit renewal and a separate execution
decision preceded the run. The renewed approval is consumed. That checkpoint's
reviewed scope covers 107 files, with 171 harness/21 script tests and 81 canonical race calls;
selected arm64 preparation binds 74 image files/19 recipes. See the
[shared-group result](docs/crawl-jobs-v2-m4-shared-group-run-2026-10-02.md),
[image preparation](docs/crawl-jobs-v2-m4-shared-group-preparation-2026-10-01.md),
[lifecycle checkpoint](docs/crawl-jobs-v2-m4-shared-group-lifecycle-2026-10-01.md)
and [offline foundation](docs/crawl-jobs-v2-m4-shared-group-capacity-2026-10-01.md).
**PR #18 publication, merge and main-CI gates are complete.** The October 3
squash merge has the same tree as reviewed `c8c5ee2`; all fourteen main-push checks
pass, with 480 race roots accounted for (479 passes/one allowed optional skip),
171 harness/21 script tests and 74-file/19-recipe image evidence. The
[merge checkpoint](docs/evidence/m4-shared-group-merge-2026-10-03/README.md)
retains the exact records. PR #19 subsequently merged documentation/CI cleanup as
`e80d005`, with exact main CI verified and the historical branches archived.
**`ledger-shared-group-cancellation-v1` now has accepted scoped real evidence** at
`22317dc`. Its single nine-call run verifies pending cancellation, peer-safe replays,
A's zero starts, B's first-start history, 60 counters and 46 authority denials.
The full 183-test pre-guard harness, 21 scripts and 81 canonical Go/Lua race invocations
pass; a final ACL case-binding guard then passes all 33 shared-family regressions.
Independent correctness/security source reviews returned **GO for image/CI
preparation**, with all 108 source hashes unchanged. Fresh selected arm64 image
preparation also **passes**: both checks match 74 files/all 20 recipes, stopped-role
admission passes, and all six metadata resources are independently absent.
Registry scope is 20 cases/22 scenarios. **PR #20 is now merged as `ab5f21a`**,
with the same tree as reviewed final head `4e699ec`. All fourteen required
merged-main checks and twenty-three workflow jobs pass. Downloaded evidence
verifies 480 Go roots, the final-source 183-test harness/21 scripts and 74 image
files/all 20 recipes. See the
[merge/main-CI checkpoint](docs/crawl-jobs-v2-m4-cancellation-merge-2026-10-07.md),
[original source-publication CI](docs/crawl-jobs-v2-m4-cancellation-ci-2026-10-06.md) and
[cancellation image preparation](docs/crawl-jobs-v2-m4-shared-group-cancellation-preparation-2026-10-05.md).
See the [cancellation implementation checkpoint](docs/crawl-jobs-v2-m4-shared-group-cancellation-2026-10-05.md).
The [source-review result](docs/crawl-jobs-v2-m4-shared-group-cancellation-review-2026-10-05.md)
retains the final-byte checks and the pre-/post-ACL-guard verification chronology.
The first approval was refused before reservation when its full-budget window no
longer fit. An explicitly approved renewal and separate execution decision then
authorized one 14,130-ms run. Independent correctness/security reviews accept the
result; six-role revocation, the original 28-action journal and exact six-resource
absence pass. The renewed approval is consumed. See the
[cancellation run/evidence](docs/crawl-jobs-v2-m4-cancellation-run-2026-10-06.md).
The owner-selected C0 worktree now includes the
owned-resource, bounded-stream and watchdog layer. Its 22-file package passes
78 tests, with final correctness/security GO for continued implementation after
remediation. Native runtime admission, oracle/client integration and full trial
dispatch remain next; observer feasibility and D02 owner decisions remain open.
Full M4 acceptance and application integration remain pending. The
[Crawl Jobs V2 plan](docs/crawl-jobs-v2-plan.md) owns current progress. Historical
V1 commands below do not authorize another crawl.

Current plan state: F1, F2, F4, and F6 are not started. F5's idempotency repair
passed protected PR #9 checks and merged; retained-data reconciliation and V2
consumer integration remain separately gated.
Render Stages 0, 1, and 2a are implemented but disabled. Stage 3 is not
approved. Use the
[parent remediation plan](docs/spider-render-remediation-plan-2026-09-01.md)
and [Crawl Jobs V2 plan](docs/crawl-jobs-v2-plan.md) for current status and
gates.

### Future V2 release compatibility

A future V2 release must be selected by one reviewed exact-byte
compatibility-manifest artifact. It binds every required protocol, policy,
publication, IPC, configuration, and guard field to the protocol-defined image
field for every named participant. Validate the manifest as one unit; release
tags and a hand-maintained three- or four-image set are not deployment
identities.
`render_worker_image=disabled` remains mandatory unless a separate rendering
activation is approved.

The future V2 cutover remains coordinated rather than rolling; mixed V1/V2
operation and dual-read compatibility are prohibited. Root Compose builds
remain local development artifacts and are not production release inputs.

The legacy digest-env cutover is preserved only as historical V1 context in
[`docs/immutable-pipeline-release-cutover.md`](docs/immutable-pipeline-release-cutover.md).
Parent-plan Phase 6 and F3 milestones M6-M7 own its future replacement. For V2,
the first successful `CJ2_START_REQUEST`, recorded before DNS, is the ordinary
rollback boundary; Spider startup or first page publication is not.

## Forum Engine Foundation

The forum/community scaffold lives in `services/forum-engine` and provides the foundation for MiFolyo's discussion layer.

Planning docs:

- `docs/forum-service-architecture.md`
- `docs/forum-service-implementation-plan.md`
- `docs/forum-service-scaffold-checklist.md`

Supporting scripts:

- `scripts/docker/infra.compose.yml`
- `scripts/fullstack.sh`
- `scripts/smoke-forum.sh`

Environment boundaries and production access policy are documented in
`docs/environments.md`.

## Historical V1 Baseline Test Procedure (Blocked)

> [!CAUTION]
> This section preserves the V1 command sequence for historical review. It is
> not the active V2 procedure and does not authorize preparation, mutation, or
> another crawl. Parent-plan Phase 6 must replace active operational guidance
> with tested V2 commands before any future authorized run.

The historical disposable, search-only V1 crawl test used
`scripts/docker/v1-baseline.compose.yml`. It used the fixed/default project name
`mifolyo-v1-baseline-test`, no forum service, no data-store ports published by
this project, and no implicit crawl target. Only Caddy is available on
`127.0.0.1:${MIFOLYO_V1_TEST_HTTP_PORT:-18080}`.

The retained post-test data is inspection evidence, not a fresh baseline. Do
not use the commands below against it; follow the linked parent and F3 plans for
the blocked work and future documentation replacement.

These reference commands are not an execution transcript. The
[2026-08-18 report](docs/v1-baseline-crawl-test-report-2026-08-18.md) records 70
enabled seeds and a strict FAIL. The later 67-enabled/3-disabled target below
has not been accepted as a fresh retained baseline.

The V1 spider cannot resolve this project's `mongo` or `postgres` names through
Compose DNS, but its egress-capable crawl network can still reach services
published on the Docker host. The historical preflight required the root
development MongoDB, Redis, and PostgreSQL services to be stopped or their host
port publications to be removed by a reviewed local configuration:

```bash
docker compose --file docker-compose.yml ps mongo redis postgres
docker compose --file docker-compose.yml stop mongo redis postgres
docker compose --file docker-compose.yml ps mongo redis postgres
```

The recorded final status had to show those services stopped, or show approved
running services with no host-published ports. The operator also inventoried
other local database containers.

Each preserved command supplied the file and project explicitly so it could not
operate on the root development stack. The historical configure, build, start,
inspection, and log sequence was:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools --profile pipeline --profile crawl --profile image-pipeline config --quiet
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools --profile pipeline --profile crawl build --pull
docker run --rm --entrypoint npm \
  mifolyo-v1-baseline-test-query-engine:local \
  audit --audit-level=low
docker image inspect mifolyo-v1-baseline-test-query-engine:local \
  --format 'runtime-user={{.Config.User}}'
docker run --rm --user 0:0 --entrypoint /bin/sh \
  mifolyo-v1-baseline-test-query-engine:local -ec \
  'test -z "$(find /var/www -maxdepth 1 -name ".env*" ! -name ".env.example" -print -quit)" && test -z "$(find /var/www/public \( ! -user query-engine-user -o ! -group www-data \) -print -quit)"'
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  up -d mongo redis postgres query-assets query-engine caddy
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml ps --all
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  logs --tail=200 mongo redis postgres query-assets query-engine caddy
docker inspect \
  mifolyo-v1-baseline-test-query-assets-1 \
  mifolyo-v1-baseline-test-query-engine-1 \
  --format '{{.Name}} {{.Image}}'
```

`query-assets` and `query-engine` both used the explicit image tag
`mifolyo-v1-baseline-test-query-engine:local`; `query-engine` is the sole build
owner and `query-assets` consumes that image. Recreate both services after a
query image build and compare their running image IDs as required by
`docs/v1-baseline-crawl-test-checklist.md`; a mutable tag alone does not prove
that existing containers use the same image build.

`/up` was HTTP/PHP liveness only, not dependency readiness. The historical
procedure required all of these read-only checks before rebuilding, feeding, or
crawling; PostgreSQL had to report `t`, Redis had to report `PONG`, and both API
calls had to return successful JSON:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml ps --all
curl --fail --show-error \
  "http://127.0.0.1:${MIFOLYO_V1_TEST_HTTP_PORT:-18080}/up"
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  exec -T mongo mongosh --quiet --eval '
const ok = db.adminCommand({ping: 1}).ok;
printjson({mongoReady: ok === 1});
quit(ok === 1 ? 0 : 1);'
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  exec -T redis redis-cli --raw PING
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  exec -T postgres psql --no-psqlrc \
  --username mifolyo --dbname mifolyo --tuples-only \
  --command "SELECT to_regclass('public.migrations') IS NOT NULL AS migrations_ready;"
curl --fail --show-error \
  "http://127.0.0.1:${MIFOLYO_V1_TEST_HTTP_PORT:-18080}/api/health/ready"
curl --fail --show-error \
  "http://127.0.0.1:${MIFOLYO_V1_TEST_HTTP_PORT:-18080}/api/stats"
```

Within that V1 procedure, the query readiness endpoints proved read-only
query-engine-to-MongoDB access only; the direct Redis and PostgreSQL checks were
therefore mandatory.

The historical sequence previewed the baseline replacement, then used the
test-only environment guard and exact local target confirmation for its guarded
rebuild:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools run --rm seed-importer \
  python crawl_seeds.py rebuild --dry-run
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools run --rm -e MIFOLYO_ENV=test \
  seed-importer python crawl_seeds.py rebuild \
  --confirm-rebuild mongo:27017/mifolyo_index/crawl_seeds
```

It then previewed and fed the reviewed catalog before starting the downstream
consumers:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools run --rm seed-importer \
  python feed.py --dry-run --limit 1000
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools run --rm seed-importer python feed.py --limit 1000
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile pipeline up -d indexer backlinks-processor
```

The spider now enforces DNS-pinned address authorization, redirect
revalidation, robots policy, and the exact approved baseline policy digest.
That implementation does not authorize the command below. It is historical and
must not be invoked, even if every old checklist item is completed.

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile crawl run --rm spider \
  ./spider --once --max-concurrency 2 --max-pages 10 --validate-baseline-policy
```

The optional `image-pipeline` consumes only immutable, spider-authorized
normalized metadata. It has no outbound image fetch or image decoder path.
The historical procedure enabled it only when image-indexing behavior was part
of the reviewed test scope; no such activation is authorized now.

The historical full-cleanup step was the following project-restricted command
after logs and test evidence were preserved. It deliberately deletes this test
project's disposable volumes and is not currently authorized:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  down --volumes --remove-orphans
```

Do not run the crawl or cleanup commands. Their V1 context, lifecycle,
network-isolation, and verification details remain in the historical checklist
and `docs/environments.md`.
The query image's npm remediation record, deterministic build rule, and
non-root asset ownership model are documented in
`services/query-engine/README.md`.

## Local Development

The core-only startup command below is a reference for a separately approved
local development environment, not permission to change retained test state
or prepare a crawl. Profiled crawl and batch services remain stopped:

```bash
docker compose up -d
```

Spider startup does not contain a default `STARTING_URL`; starting the core
stack therefore cannot silently seed a crawl. The normal V1 development flow
historically populated the seed catalog, fed its enabled records, and only then
started the profiled pipeline.

> [!CAUTION]
> The V1 bootstrap, rebuild, feed, and Spider commands below are legacy
> references, not crawl authorization. Do not use them to alter retained test
> evidence or prepare a run. Tested V2 replacements belong to parent-plan
> Phase 6 after the implementation and acceptance gates pass.

### Historical V1 baseline bootstrap reference

The historical bootstrap started the two seed-importer dependencies and built
the importer image:

```bash
docker compose up -d mongo redis
docker compose --profile batch build seed-importer
```

It validated the tracked baseline without connecting to MongoDB, then merged it
into `mifolyo_index.crawl_seeds`:

```bash
docker compose --profile batch run --rm seed-importer \
  python crawl_seeds.py bootstrap --dry-run
docker compose --profile batch run --rm seed-importer \
  python crawl_seeds.py bootstrap
```

Bootstrap is replay-safe and performs a compatibility preflight. It refuses to
rewrite a nonempty incompatible legacy collection.

### Historical V1 guarded development rebuild reference

The guarded rebuild replaced only the V1 `mifolyo_index.crawl_seeds`
collection. Its procedure inspected the dry-run first:

```bash
docker compose --profile batch run --rm seed-importer \
  python crawl_seeds.py rebuild --dry-run
```

Historical execution required both the exact process environment guard
`MIFOLYO_ENV=development` and a confirmation bound to the parsed MongoDB
host, database, and collection. For the root Compose stack, the exact command
was:

```bash
docker compose --profile batch run --rm \
  -e MIFOLYO_ENV=development \
  seed-importer python crawl_seeds.py rebuild \
  --confirm-rebuild mongo:27017/mifolyo_index/crawl_seeds
```

Do not put `MIFOLYO_ENV=development` in the Compose defaults. A different
MongoDB target requires the exact confirmation token printed by its dry-run;
neither a CLI environment label nor a generic confirmation can bypass these
guards.

### Historical V1 feed and bounded-crawl reference (do not execute)

This subsection preserves the later, unexecuted 67-enabled/3-disabled V1 target
for the 70-record catalog, not the recorded August 18 execution. The checklist remains at
`docs/v1-baseline-crawl-test-checklist.md`; completing its old checkboxes does
not permit execution. A future crawl requires the accepted V2 procedure and a
new explicit authorization.

The V1 procedure previewed the enabled records, then wrote them to these Redis
structures:

```bash
docker compose --profile batch run --rm seed-importer \
  python feed.py --dry-run --limit 1000
docker compose --profile batch run --rm seed-importer \
  python feed.py --limit 1000
```

The feeder atomically writes URL IDs to `mifolyo:crawl:v1:queue`, canonical URL
lookups to `mifolyo:crawl:v1:urls`, and initial depth `0` to
`mifolyo:crawl:v1:depths`. Replays preserve the best queue priority and
shallowest depth. To inspect the pending count without changing data:

```bash
docker compose exec redis redis-cli ZCARD mifolyo:crawl:v1:queue
```

The current root development file publishes its MongoDB, Redis, and PostgreSQL
ports. The following V1 consumer and Spider commands are historical and must
not be run. The old procedure additionally required a reviewed portless
configuration and an explicit bounded command because the Compose default only
validated policy:

```bash
docker compose --profile pipeline up -d indexer backlinks-processor
docker compose --profile crawl run --rm spider \
  ./spider --once --max-concurrency 2 --max-pages 10 --validate-baseline-policy
```

Do not start the development Image Indexer for this baseline either. The current
service has no outbound image fetch or decoder path and reconciles only
Spider-authorized metadata. That implementation does not reactivate the
historical procedure; see `docs/environments.md`.

For a historical one-off development target, `STARTING_URL` was an explicit
per-run override rather than a stack default. This command is not authorized:

```bash
docker compose --profile crawl run --rm \
  -e STARTING_URL=https://archive.org/ spider \
  ./spider --once --max-concurrency 2 --max-pages 10
```

That historical development override deliberately omitted
`--validate-baseline-policy`, which rejects unreviewed starting URLs.

**Never run Redis `FLUSHDB` or `FLUSHALL` to reset crawling.** Redis database
0 is shared with other pipeline and application state. The V1 key namespace
exists to isolate this queue; blanket flushing can destroy unrelated data.

### JavaScript renderer

The optional `render` profile provides a separate Headless Chromium worker for
exact-host/path `inline_only` and brokered script/stylesheet rules. Chromium has
no network namespace or data-store credentials; brokered resources are fetched
only by the page-bound crawler authorization path. Rendering is disabled by
`services/spider/config/render-policy-v1.disabled.json`; the static baseline
also rejects every enabled render rule.

Render Stages 0, 1, and 2a are implemented but disabled. Stage 3 is not
approved, and no rendering activation is authorized. See the parent remediation
plan for the current gate rather than treating worker availability as approval.

The worker and its sandbox can be tested without contacting a public site:

```bash
docker build -t mifolyo-render-worker:test services/render-worker
docker run --rm --network none --read-only --user 65534:65534 \
  --cap-drop ALL --security-opt no-new-privileges:true \
  --security-opt seccomp=services/render-worker/seccomp_profile.json \
  --tmpfs /tmp:rw,noexec,nosuid,size=256m \
  --tmpfs /dev/shm:rw,nosuid,size=256m \
  mifolyo-render-worker:test node smoke.mjs
```

External script/style brokering is implemented but is not authorized for the
baseline. Existing image publication does not deploy or activate the required
socket and sandbox topology. See
`services/render-worker/README.md` before changing the disabled policy or
starting the profile.

### Batch ranking jobs

> [!CAUTION]
> The commands in this section document local V1 capability only. Do not run the
> mutating TF-IDF or PageRank publication against retained evidence or under the
> current F3 gates; a separately reviewed isolated V1 change is required.

Run TF-IDF and perform a read-only PageRank validation:

```bash
docker compose --profile batch run --rm tfidf
docker compose --profile batch run --rm page-rank
```

PageRank publication is intentionally a separate, hash-bound command. Stop and
flush graph producers first, capture `graph_sha256` from validation, and then
run:

```bash
docker compose --profile batch run --rm page-rank \
  ./page-rank --publish \
  --expected-graph-sha256=<validated-sha256> \
  --confirm-target=mongo:27017/mifolyo_index/pagerank
```

The isolated baseline historically used the stricter procedure in
`docs/v1-baseline-crawl-test-checklist.md` instead of the root Compose project;
neither path grants current execution authority.

The local spider identifies itself as `MiFolyoBot/1.0`. Its root Compose
default is validation-only. The historical development crawl shown above was
bounded by `--once --max-concurrency 2 --max-pages 10` but is not authorized.

## Notes

MiFolyo is in active rebuild. The search stack and forum stack are being reconciled into one Laravel-centered private-beta foundation.
