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
- **Page Rank**: Calculates PageRank over backlink data.
- **TF-IDF**: Calculates term frequency-inverse document frequency weights.
- **Query Engine**: Laravel service that returns ranked search results using TF-IDF and PageRank.

The local development stack is defined in the root `docker-compose.yml` and supports optional `pipeline` and `batch` profiles.

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
  --profile tools --profile pipeline --profile image-pipeline config --quiet
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools --profile pipeline build --pull
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

The future bounded spider command is recorded in the checklist but is currently
blocked. Do not invoke it until DNS-pinned address authorization and redirect
revalidation are implemented and tested.

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile pipeline run --rm spider \
  ./spider --once --max-concurrency 2 --max-pages 10
```

Do not enable the separate `image-pipeline` profile for the baseline.
External image fetching is deferred until the image fetch path validates DNS
and resolved IPs, revalidates redirects, and blocks private and host targets.

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

Before preparing the first 70-seed V1 baseline crawl, complete the isolation,
preflight, evidence, and cleanup steps in
`docs/v1-baseline-crawl-test-checklist.md`. Spider execution remains blocked
until DNS-pinned address authorization and redirect revalidation are
implemented and tested.

Preview the enabled records, then write them to the V1 Redis structures:

```bash
docker compose --profile batch run --rm seed-importer \
  python feed.py --dry-run --limit 1000
docker compose --profile batch run --rm seed-importer \
  python feed.py --limit 1000
```

The feeder writes URL IDs to the versioned sorted set
`mifolyo:crawl:v1:queue` and canonical URL lookups to
`mifolyo:crawl:v1:urls`. Replays preserve the best queue priority. To inspect
the pending count without changing data:

```bash
docker compose exec redis redis-cli ZCARD mifolyo:crawl:v1:queue
```

The current root development file publishes its MongoDB, Redis, and PostgreSQL
ports. Do **not** run either spider command below with those publications in
place. A root-stack crawl also requires the pending fetch-time SSRF controls.
The data stores may remain available to the pipeline only through a reviewed
portless configuration. Starting the downstream consumers is safe, but the
spider command below is recorded for future use and must not run yet:

```bash
docker compose --profile pipeline up -d indexer backlinks-processor
docker compose --profile pipeline run --rm spider
```

Do not start the development image indexer for this baseline either. External
image retrieval remains deferred pending the SSRF-hardened fetch path described
in `docs/environments.md`.

For a one-off development target, `STARTING_URL` remains an explicit per-run
override rather than a stack default:

```bash
docker compose --profile pipeline run --rm \
  -e STARTING_URL=https://example.org/ spider
```

**Never run Redis `FLUSHDB` or `FLUSHALL` to reset crawling.** Redis database
0 is shared with other pipeline and application state. The V1 key namespace
exists to isolate this queue; blanket flushing can destroy unrelated data.

### Batch ranking jobs

Run batch ranking jobs:

```bash
docker compose --profile batch run --rm tfidf
docker compose --profile batch run --rm page-rank
```

The local spider identifies itself as `MiFolyoBot/1.0` and the root Compose
spider command is bounded for development with
`--once --max-concurrency 2 --max-pages 10`.

## Notes

MiFolyo is in active rebuild. The search stack and forum stack are being reconciled into one Laravel-centered private-beta foundation.
