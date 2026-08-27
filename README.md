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

Production deployment Compose files for Spider, Indexer, Image Indexer, and
PageRank require service-specific
`ghcr.io/fullerkris/mifolyo/<service>@sha256:<64 lowercase hex>` values from the
reviewed release `release-image.env` artifacts. Release tags are organizational
metadata, not deployment identities. Use
`docker compose --env-file release-image.env` and verify the pulled image's
exact `RepoDigest` before cutover, following the stop/drain/backup procedure in
`docs/immutable-pipeline-release-cutover.md`. The page/image queue protocol is
not rolling-upgrade compatible; never mix release generations or add dual-read
behavior. Root Compose builds remain local and are not production artifacts.

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

## Isolated V1 Baseline Test

Use `scripts/docker/v1-baseline.compose.yml` for the disposable, search-only
V1 crawl test. It has the fixed/default project name
`mifolyo-v1-baseline-test`, no forum service, no data-store ports published by
this project, and no implicit crawl target. Only Caddy is available on
`127.0.0.1:${MIFOLYO_V1_TEST_HTTP_PORT:-18080}`.

The spider cannot resolve this project's `mongo` or `postgres` names through
Compose DNS, but its egress-capable crawl network can still reach services
published on the Docker host. Before any crawl, the root development MongoDB,
Redis, and PostgreSQL services must be stopped or their host port publications
must be removed by a reviewed local configuration:

```bash
docker compose --file docker-compose.yml ps mongo redis postgres
docker compose --file docker-compose.yml stop mongo redis postgres
docker compose --file docker-compose.yml ps mongo redis postgres
```

The final status must show those services stopped, or show approved running
services with no host-published ports. Also inventory other local database
containers before proceeding.

Every command supplies the file and project explicitly so it cannot operate on
the root development stack. Configure, build, start, inspect, and read logs:

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

`query-assets` and `query-engine` both use the explicit image tag
`mifolyo-v1-baseline-test-query-engine:local`; `query-engine` is the sole build
owner and `query-assets` consumes that image. Recreate both services after a
query image build and compare their running image IDs as required by
`docs/v1-baseline-crawl-test-checklist.md`; a mutable tag alone does not prove
that existing containers use the same image build.

`/up` is HTTP/PHP liveness only, not dependency readiness. Before rebuilding,
feeding, or crawling, run all of these read-only checks; PostgreSQL must report
`t`, Redis must report `PONG`, and both API calls must return successful JSON:

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

The query readiness endpoints currently prove read-only query-engine-to-MongoDB
access only; the direct Redis and PostgreSQL checks are therefore mandatory.

Preview the baseline replacement, then use the test-only environment guard and
the exact local target confirmation for the approved rebuild:

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

Preview and feed the reviewed catalog, then start only the downstream
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
The command remains an operator-gated procedure: do not invoke it until every
pre-crawl stop condition and checkbox in the baseline checklist has passed.

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile crawl run --rm spider \
  ./spider --once --max-concurrency 2 --max-pages 10 --validate-baseline-policy
```

The optional `image-pipeline` consumes only immutable, spider-authorized
normalized metadata. It has no outbound image fetch or image decoder path.
Enable it only when image-indexing behavior is part of the reviewed test scope.

After preserving logs and test evidence, the only approved full cleanup is the
following project-restricted command. It deliberately deletes this test
project's disposable volumes; never run it with the root Compose file:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  down --volumes --remove-orphans
```

Do not run the crawl or cleanup until all stop conditions in
`docs/v1-baseline-crawl-test-checklist.md` have been reviewed. Full lifecycle,
network-isolation, and verification commands are in `docs/environments.md`.
The query image's npm remediation record, deterministic build rule, and
non-root asset ownership model are documented in
`services/query-engine/README.md`.

## Local Development

Start the core local search stack (the profiled crawl and batch services remain
stopped):

```bash
docker compose up -d
```

Spider startup does not contain a default `STARTING_URL`; starting the core
stack therefore cannot silently seed a crawl. The normal V1 development flow
is to populate the seed catalog, feed its enabled records, and only then start
the profiled pipeline.

### Safe V1 baseline bootstrap

Start the two seed-importer dependencies and build the importer image:

```bash
docker compose up -d mongo redis
docker compose --profile batch build seed-importer
```

Validate the tracked baseline without connecting to MongoDB, then merge it
into `mifolyo_index.crawl_seeds`:

```bash
docker compose --profile batch run --rm seed-importer \
  python crawl_seeds.py bootstrap --dry-run
docker compose --profile batch run --rm seed-importer \
  python crawl_seeds.py bootstrap
```

Bootstrap is replay-safe and performs a compatibility preflight. It refuses to
rewrite a nonempty incompatible legacy collection.

### Guarded development rebuild (optional)

A rebuild replaces only the V1 `mifolyo_index.crawl_seeds` collection. Inspect
the dry-run first:

```bash
docker compose --profile batch run --rm seed-importer \
  python crawl_seeds.py rebuild --dry-run
```

Execution requires both the exact process environment guard
`MIFOLYO_ENV=development` and a confirmation bound to the parsed MongoDB
host, database, and collection. For the root Compose stack, the exact command
is:

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

### Feed and prepare a bounded crawl

Before preparing a 67-enabled-seed V1 baseline crawl from the 70-record catalog,
complete the isolation,
preflight, evidence, and cleanup steps in
`docs/v1-baseline-crawl-test-checklist.md`. Fetch-time transport controls are
implemented, but the crawl remains blocked until the environment-specific
host-publication, queue/depth, image, and evidence checks pass.

Preview the enabled records, then write them to the V1 Redis structures:

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
ports. Do **not** run either spider command below with those publications in
place. The data stores may remain available to the pipeline only through a
reviewed portless configuration. Starting the downstream consumers is safe;
the spider remains operator-gated even though its fetch-time controls are now
implemented. Its Compose default only validates policy, so a crawl also
requires the explicit bounded command override:

```bash
docker compose --profile pipeline up -d indexer backlinks-processor
docker compose --profile crawl run --rm spider \
  ./spider --once --max-concurrency 2 --max-pages 10 --validate-baseline-policy
```

Do not start the development image indexer for this baseline either. External
image retrieval remains deferred pending the SSRF-hardened fetch path described
in `docs/environments.md`.

For a one-off development target, `STARTING_URL` remains an explicit per-run
override rather than a stack default:

```bash
docker compose --profile crawl run --rm \
  -e STARTING_URL=https://archive.org/ spider \
  ./spider --once --max-concurrency 2 --max-pages 10
```

This development override deliberately omits `--validate-baseline-policy`,
which rejects unreviewed starting URLs.

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
baseline. The release workflow promotes the worker image only; it does not
deploy or activate the required socket and sandbox topology. See
`services/render-worker/README.md` before changing the disabled policy or
starting the profile.

### Batch ranking jobs

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

For the isolated baseline environment, use the stricter procedure in
`docs/v1-baseline-crawl-test-checklist.md` instead of the root Compose project.

The local spider identifies itself as `MiFolyoBot/1.0`. Its root Compose
default is validation-only; the explicit development crawl shown above is
bounded by `--once --max-concurrency 2 --max-pages 10`.

## Notes

MiFolyo is in active rebuild. The search stack and forum stack are being reconciled into one Laravel-centered private-beta foundation.
