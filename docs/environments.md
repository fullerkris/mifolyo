# MiFolyo Environments

MiFolyo has three intentionally separate operating environments. Never reuse
Compose projects, volumes, credentials, or database endpoints between them.

| Environment | Definition | Access | Data policy |
|---|---|---|---|
| Development local | Root `docker-compose.yml` | Developer workstation | Long-lived developer state; not a cleanup target for baseline tests |
| Isolated test local | `scripts/docker/v1-baseline.compose.yml`, project `mifolyo-v1-baseline-test` | Caddy only, on loopback port `18080` by default | Disposable, project-scoped state |
| Production | Tailscale-only host | `https://srv1459482.tail11b93a.ts.net` | Durable production state with backups; never reused by local Compose |

## Development local

The root `docker-compose.yml` remains the general development environment. It
may contain developer data and optional forum, pipeline, and batch services.
Treat its databases and volumes as shared developer state: do not use the V1
test cleanup command against the development project and never use blanket
Redis flush commands.

The current root file publishes its MongoDB, Redis, and PostgreSQL ports. Do
not run a root-stack spider while those publications exist; use a reviewed
portless configuration so the running pipeline can still reach its stores.
Before an isolated V1 crawl, stop those root services or prove the active root
configuration is portless as required below.

Development commands continue to use the root file, for example:

```bash
docker compose config --quiet
docker compose up -d
docker compose ps
```

## Isolated test local

The V1 baseline environment is search-only: it contains MongoDB 8, Redis 7,
PostgreSQL 16, the Laravel query engine, Caddy, and opt-in crawl tooling. It has
no forum service. Its top-level Compose name is
`mifolyo-v1-baseline-test`; every lifecycle command below also supplies that
name explicitly so an ambient `COMPOSE_PROJECT_NAME` cannot redirect cleanup.

Only Caddy publishes a host port:

```text
127.0.0.1:${MIFOLYO_V1_TEST_HTTP_PORT:-18080} -> caddy:80
```

MongoDB, Redis, PostgreSQL, Laravel FPM, and pipeline workers have no published
ports. The six named volumes and all four networks receive the fixed Compose
project prefix and are not external. The networks enforce these paths:

- `crawl`: spider and Redis; permits crawler egress to reviewed web targets.
- `data` (internal): MongoDB, Redis, PostgreSQL, query engine, seed importer,
  text indexer, backlinks processor, and the deferred image indexer.
- `web` (internal): Caddy-to-query-engine application traffic.
- `ingress`: Caddy only; provides the loopback host-port gateway.

Redis is the only baseline service on both `crawl` and `data`. The spider
therefore cannot resolve the baseline `mongo` or `postgres` service names
through Compose DNS. This is not a complete host boundary: the crawl network
has egress, and host-published development databases may still be reachable
through Docker's host gateway or another host address. The query engine is on
`data` and `web`; Caddy bridges `web` and `ingress`. Redis also disables
`FLUSHDB` and `FLUSHALL` in this disposable stack.

### Container hardening controls

The isolated stack applies `no-new-privileges` to every service. Its externally
reachable and pipeline containers add narrower controls where their runtime
requirements allow them:

- Caddy drops all capabilities except `NET_BIND_SERVICE`, uses a read-only root
  filesystem, and writes only to bounded temporary filesystems.
- The query-engine image runs PHP-FPM as `query-engine-user`, drops all Linux
  capabilities, has no published port, and is reachable only through Caddy on
  the internal `web` network.
- The one-shot `query-assets` service runs as UID/GID `1000:33`, has no network,
  drops all capabilities, and uses a read-only root filesystem. It can write
  only to the project-scoped `query-public` volume and a bounded `/tmp`.
- The query image reassigns generated public assets to
  `query-engine-user:www-data` after the frontend build. This prevents
  root-owned build output from breaking non-root fresh-volume startup. It also
  pre-creates `/assets` with the same ownership so an empty named volume is
  writable before the one-shot copy begins.
- Profiled seed, pipeline, and crawl containers run as an explicit non-root
  user with dropped capabilities, read-only root filesystems, bounded temporary
  storage, and no host-published ports.
- MongoDB's `/data/db` and `/data/configdb`, Redis data, PostgreSQL data, and
  query public assets all use explicit project-scoped named volumes. No
  anonymous or external volume is part of the test lifecycle.

The query engine uses a test-stack entrypoint that runs migrations and starts
PHP-FPM without the image's generic cache-clear step. This is intentional:
Redis disables `FLUSHDB` and `FLUSHALL`, so startup must not invoke a framework
cache operation that depends on either command.

### Required host-publication preflight

Before **any** spider invocation, the root development MongoDB, Redis, and
PostgreSQL services must either be stopped or run from a reviewed configuration
that has no host `ports` publications. The isolated stack's network separation
does not compensate for development ports such as `27017`, `6379`, or `5432`
being published on the host.

The spider now implements DNS-pinned address authorization, numeric-address
dialing, remote-endpoint checks, TLS verification, redirect revalidation, and
fail-closed baseline robots policy. These application controls do not make the
host-publication check optional: NAT, host routing, or operator-configured
networks remain outside the process's complete visibility. Do not crawl until
the host inventory and every remaining checklist preflight have passed.

Inspect the root development services. If any are running with published
ports, stop them; this does not remove their containers or volumes:

```bash
docker compose --file docker-compose.yml ps mongo redis postgres
docker compose --file docker-compose.yml stop mongo redis postgres
docker compose --file docker-compose.yml ps mongo redis postgres
```

The final output must show all three stopped, or an approved portless root
configuration must be rendered and recorded. Also inventory other local
containers for database publications. Do not proceed merely because the V1
baseline data stores themselves have no published ports.

### Configure, build, and start

Run commands from the repository root. Validate all profiles before creating
anything:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools --profile pipeline --profile ranking --profile crawl \
  --profile image-pipeline --profile render config --quiet
```

Repeat the command without `--quiet` when reviewing the fully resolved port,
network, volume, command, and environment model before a test run.

Build the images required by a bounded baseline crawl. The deferred image
indexer is deliberately excluded:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools --profile pipeline --profile crawl build --pull
```

The spider belongs only to `crawl`, and its resolved default command must be
`./spider --validate-policy --validate-baseline-policy`. Validate the rebuilt
image without Redis or network access before any operational run:

```bash
docker image inspect mifolyo-v1-baseline-test-spider \
  --format 'runtime-user={{.Config.User}}'
docker run --rm --network none --read-only \
  mifolyo-v1-baseline-test-spider \
  ./spider --validate-policy --validate-baseline-policy \
  --crawl-policy-file /app/config/crawl-policy-v1.baseline.json \
  --render-policy-file /app/config/render-policy-v1.disabled.json
docker run --rm --network none --read-only \
  mifolyo-v1-baseline-test-spider sh -c \
  'test -s /etc/ssl/certs/ca-certificates.crt && test "$(id -u):$(id -g)" = "65534:65534"'
```

The required runtime user is `65534:65534`; policy validation must report SHA-256
`50648954d0264f7ac4fdda174178db488e86e335a0b63fdcc448da7bc218bae3`.
The policy includes 67 enabled host rules plus disabled `disabled-sites` and
`reddit-crawler` groups. Stage 1 JavaScript rendering is implemented under the
separate `render` profile, but the baseline policy file has no render rules and
the worker must remain stopped for this environment's static crawl.

The query-engine Dockerfile installs frontend dependencies with `npm ci`, not
`npm install`. The lockfile is therefore the exact dependency input to the
image build. Audit the built image before starting or promoting it:

```bash
docker run --rm --entrypoint npm \
  mifolyo-v1-baseline-test-query-engine:local \
  audit --audit-level=low
docker run --rm --user 0:0 --entrypoint /bin/sh \
  mifolyo-v1-baseline-test-query-engine:local -ec \
  'test -z "$(find /var/www -maxdepth 1 -name ".env*" ! -name ".env.example" -print -quit)" && test -z "$(find /var/www/public \( ! -user query-engine-user -o ! -group www-data \) -print -quit)"'
```

The acceptance results are `found 0 vulnerabilities` and a successful,
output-free secret/ownership check. The image may contain `.env.example`, but
no other `.env*` file. Any nonzero audit result, embedded environment file, or
incorrectly owned public file blocks test execution and promotion until
`package.json`, `package-lock.json`, `.dockerignore`, or the image build is
corrected; the production assets build successfully; and the rebuilt image
passes both checks. A source-lockfile audit alone is not sufficient because the
current single-stage image retains its frontend toolchain. The remediation
baseline and resolved package versions are recorded in
`services/query-engine/README.md`.

Start only the core search application. Profiled tooling and the spider do not
start here:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  up -d mongo redis postgres query-assets query-engine caddy
```

The one-shot `query-assets` service refreshes the project-scoped
`query-public` volume. Both `query-assets` and `query-engine` use the explicitly
identical image tag
`mifolyo-v1-baseline-test-query-engine:local`. Only `query-engine` owns the
build definition; `query-assets` consumes the resulting image, so a full
profile build cannot race two build outputs onto the shared tag. After a query
image build, recreate both services and compare their container image IDs; the
mutable tag does not update an already-running container. `query-engine` then
waits for healthy data stores, runs `php artisan migrate --force`, and starts
PHP-FPM. Caddy waits for live PHP-FPM before serving the shared public assets.

### Status and logs

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  ps --all

docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  logs --tail=200 mongo redis postgres query-assets query-engine caddy

docker inspect \
  mifolyo-v1-baseline-test-query-assets-1 \
  mifolyo-v1-baseline-test-query-engine-1 \
  --format '{{.Name}} {{.Image}}'
```

The two image IDs must match.

Follow pipeline logs separately after starting its consumers:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile pipeline logs --follow --tail=200 \
  indexer backlinks-processor
```

### Liveness and read-only readiness gate

`/up` is Laravel/Caddy liveness only. It proves that the HTTP and PHP-FPM path
can answer; it does **not** prove that MongoDB, Redis, PostgreSQL, migrations,
or the query data path are ready:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  ps --all

curl --fail --show-error \
  "http://127.0.0.1:${MIFOLYO_V1_TEST_HTTP_PORT:-18080}/up"
```

Run every check below before rebuilding, feeding, or crawling. They are
read-only. MongoDB must answer a ping, Redis must answer `PONG`, PostgreSQL must
return `t` for the migration-table query, and both query API requests must
return successful JSON:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  exec -T mongo mongosh --quiet --eval '
const ok = db.adminCommand({ping: 1}).ok;
printjson({mongoReady: ok === 1});
quit(ok === 1 ? 0 : 1);'

docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  exec -T redis redis-cli --raw PING

docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  exec -T postgres psql --no-psqlrc \
  --username mifolyo --dbname mifolyo --tuples-only \
  --command "SELECT to_regclass('public.migrations') IS NOT NULL AS migrations_ready;"

curl --fail --show-error \
  "http://127.0.0.1:${MIFOLYO_V1_TEST_HTTP_PORT:-18080}/api/health/ready"
curl --fail --show-error \
  "http://127.0.0.1:${MIFOLYO_V1_TEST_HTTP_PORT:-18080}/api/stats"
```

The current `/api/health/ready` and `/api/stats` paths exercise read-only
query-engine-to-MongoDB access. They do not cover Redis or PostgreSQL, which is
why the direct checks above remain mandatory.

### Seed catalog, feed, and bounded crawl

First inspect the rebuild plan; this does not mutate MongoDB:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools run --rm seed-importer \
  python crawl_seeds.py rebuild --dry-run
```

The following guarded rebuild is intentionally destructive only to the
isolated `mifolyo_index.crawl_seeds` collection. It requires the test
environment guard and the exact target printed by the dry-run. Do not place
`MIFOLYO_ENV=test` in Compose defaults:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools run --rm \
  -e MIFOLYO_ENV=test \
  seed-importer python crawl_seeds.py rebuild \
  --confirm-rebuild mongo:27017/mifolyo_index/crawl_seeds
```

Preview and then feed the isolated V1 queue:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools run --rm seed-importer \
  python feed.py --dry-run --limit 1000

docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools run --rm seed-importer \
  python feed.py --limit 1000
```

Start only the two approved long-running downstream consumers:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile pipeline up -d \
  indexer backlinks-processor
```

PageRank is a separate one-shot batch behind the `ranking` profile. It must not
run concurrently with the spider or indexer. After the crawl, confirm
`pages_queue` is stably empty, stop the indexer only after its final flush, and
follow section 8A of `docs/v1-baseline-crawl-test-checklist.md`. The first
PageRank invocation is read-only validation; publication requires its exact
reported graph SHA-256.

`image-indexer` is isolated behind the separate `image-pipeline` profile and
must not be started for the V1 baseline. It fetches externally supplied image
URLs; that behavior is deferred until an SSRF-hardened fetch path implements
DNS/IP validation, redirect revalidation, and private/host address blocking.
The spider may enqueue image references during this test, but no service may
fetch them.

The stack defines no implicit starting URL. Do not add an ad hoc target to a
baseline run; it must consume only the reviewed V1 queue. This environment
guide does not authorize or invoke a crawl. Complete the post-catalog
verification below, then use section 7 of
`docs/v1-baseline-crawl-test-checklist.md` only after a fresh authorization is
recorded. A normal `pipeline` start cannot launch the spider, and a `crawl`
profile start without an explicit override performs validation only.

### Post-catalog read-only data verification

After catalog creation, verify seed metadata and queue counts without changing
either store:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  exec -T mongo mongosh --quiet mifolyo_index --eval '
const c = db.crawl_seeds;
printjson({
  total: c.countDocuments({}),
  enabled: c.countDocuments({enabled: true}),
  schemaVersions: c.distinct("schema_version"),
  canonicalizationVersions: c.distinct("canonicalization_version"),
  indexes: c.getIndexes().map(index => index.name)
});'

docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  exec -T redis redis-cli EVAL '
return {
  redis.call("ZCARD", "mifolyo:crawl:v1:queue"),
  redis.call("HLEN", "mifolyo:crawl:v1:urls"),
  redis.call("HLEN", "mifolyo:crawl:v1:depths"),
  redis.call("LLEN", "pages_queue")
}' 0
```

### Project-restricted cleanup

Preserve required logs and counts first. Then confirm `ps --all` lists only
the fixed V1 test project. The only approved full reset is:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  down --volumes --remove-orphans
```

This removes only resources labeled for `mifolyo-v1-baseline-test`, including
its disposable named volumes. Never substitute the root development file,
omit the explicit project name, use `docker volume prune`, or use
`docker system prune`.

## Tailscale-only production

The production application endpoint is:

```text
https://srv1459482.tail11b93a.ts.net
```

Its underlying Tailscale address is `100.99.200.105`. The MagicDNS HTTPS name
is the supported endpoint; do not use the bare address as a public or TLS
application URL. Tailscale HTTPS (preferably Tailscale Serve terminating HTTPS
to a loopback-bound application) is recommended, with tailnet ACLs restricting
access to approved operators and users.

Production must not publish MongoDB, Redis, or PostgreSQL ports on the public
interface or the tailnet. Keep data stores on private container networks and
allow only the application tier to reach them. Do not expose the application
through public DNS, public load balancers, router port forwarding, or a public
firewall rule.

The local V1 Compose file is not a production deployment definition. Before a
production change, require durable backups and restore tests, health and error
rate monitoring, actionable alerts, secret-managed credentials, and an
automated rollback to the last known-good application image.

The incompatible immutable page/image publication protocol additionally
requires the atomic stop/drain/backup/deploy procedure and post-producer
rollback boundary in `docs/immutable-pipeline-release-cutover.md`. A normal
rolling deployment is prohibited for Spider, Indexer, and Image Indexer.
