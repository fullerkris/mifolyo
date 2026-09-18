# MiFolyo Environments

MiFolyo has three intentionally separate operating environments. Never reuse
Compose projects, volumes, credentials, or database endpoints between them.

> [!IMPORTANT]
> V1 remains the current runtime. Crawl Jobs V2 is dormant implementation work;
> its completed M3 source bundle does not authorize runtime wiring, migration,
> deployment, candidate promotion, rendering activation, or crawling.

F3 M1/M2 and the F5 code repair passed protected PR #9 checks and merged as
`d914a93`. M3 implements all 43 canonical Lua sources and the sealed,
zero-argument `AuthoritativeScriptBindingSet()` factory. It is locally verified,
committed, and pushed as `81028ca` on `feature/crawl-jobs-v2-lua`, not accepted
through an M3 PR or protected CI. M4 still requires real-Redis fixture, ACL, and
bootstrap clarifications plus explicit approval. F1, F2, F4, and F6 are not
started. Render Stages 0, 1, and 2a remain implemented but disabled; Stage 3 is
not approved. See the
[parent remediation plan](spider-render-remediation-plan-2026-09-01.md) and
[primary Crawl Jobs V2 plan](crawl-jobs-v2-plan.md) for evidence and current gates.

| Environment | Definition | Access | Data policy |
|---|---|---|---|
| Development local | Root `docker-compose.yml` | Developer workstation | Long-lived developer state; not a cleanup target for baseline tests |
| Isolated test local | `scripts/docker/v1-baseline.compose.yml`, project `mifolyo-v1-baseline-test` | Caddy only, on loopback port `18080` by default | Project-scoped test state; preserve retained evidence until a separately approved, backup/restore-tested reset |
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
The historical isolated V1 procedure required those root services to be stopped
or proven portless. That preflight remains useful defense-in-depth evidence, but
it does not authorize a crawl.

The following root-project examples are development reference, not current
startup authorization. Do not use them to prepare a crawl or activate optional
pipeline, crawl, batch, image-pipeline, or render profiles:

```bash
docker compose config --quiet
docker compose up -d
docker compose ps
```

## Isolated test local (historical V1 procedure; blocked)

> [!CAUTION]
> The environment description remains useful for inspecting the current V1
> runtime, but the build, mutation, feed, consumer, crawl, ranking, and cleanup
> commands below are a historical V1 procedure. Do not execute them as a current
> runbook. Parent-plan Phase 6, aligned with F3 milestone M6, must replace them
> with tested V2 instructions before a future authorized run.

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
networks remain outside the process's complete visibility. Passing the host
inventory and historical checklist preflights cannot authorize a crawl or
reactivate the V1 procedure.

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

> [!CAUTION]
> This is a historical V1 execution sequence, not current startup authority. Do
> not run these commands as preparation for a crawl or V2 cutover.

The historical procedure ran commands from the repository root and validated
all profiles before creating anything:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools --profile pipeline --profile ranking --profile crawl \
  --profile image-pipeline --profile render config --quiet
```

It repeated the command without `--quiet` to review the fully resolved port,
network, volume, command, and environment model before a test run.

It built the images required by a bounded baseline crawl and deliberately
excluded the deferred image indexer:

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

The V1 image requires runtime user `65534:65534`. The later checked-in baseline
policy pins SHA-256
`50648954d0264f7ac4fdda174178db488e86e335a0b63fdcc448da7bc218bae3`.
The policy includes 67 enabled host rules plus disabled `disabled-sites` and
`reddit-crawler` groups. This is a post-run target pin, not the policy executed
by the [2026-08-18 run](v1-baseline-crawl-test-report-2026-08-18.md), which began
with 70 enabled seeds. That report remains strict **FAIL**; neither the later
pin nor the 67-enabled/3-disabled target establishes a fresh accepted baseline.

Render Stages 0, 1, and 2a are implemented but disabled; Stage 3 is not approved.
The render policy has no rules, the worker must remain stopped, and no rendering
activation is authorized.

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

The historical sequence started only the core search application, not profiled
tooling or the spider:

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

> [!CAUTION]
> The commands in this section describe the blocked V1 queue path. Parent-plan
> Phase 6 and F3 milestone M6 must replace them with accepted V2 keys, policy,
> preflight, compatibility-manifest, and rollback commands. Do not execute the
> V1 feed, consumer, or crawl path.

The historical sequence first inspected the rebuild plan without mutating
MongoDB:

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

The historical sequence previewed and then fed the isolated V1 queue:

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

The Backlinks Processor's acknowledged, idempotent snapshot-removal repair
passed F5 code acceptance through protected PR #9 and merged as `d914a93`, as
recorded in the [parent plan](spider-render-remediation-plan-2026-09-01.md).
That acceptance does not reconcile retained data or authorize consumer startup.
The command below remains V1 history, not permission to start it or any producer:

```bash
docker compose \
  --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile pipeline up -d \
  indexer backlinks-processor
```

PageRank is a separate one-shot batch behind the `ranking` profile. It must not
run concurrently with the spider or indexer. Historical step 8A of
`docs/v1-baseline-crawl-test-checklist.md` required a stably empty `pages_queue`
and a successful final Indexer flush before ranking. Its first invocation is
read-only validation; publication requires the exact reported graph SHA-256.
Neither invocation is authorized by this historical procedure.

`image-indexer` is isolated behind the separate `image-pipeline` profile and
was not started for the historical V1 baseline. The historical procedure also
excluded external image fetching. The current Image Indexer performs no HTTP
requests or image-byte decoding; it reconciles Spider-authorized metadata only.
That current behavior does not reactivate this blocked baseline procedure or
authorize V2 Image Indexer startup.

The stack defines no implicit starting URL. Do not add an ad hoc target to a
baseline run; it must consume only the reviewed V1 queue. This environment
guide does not authorize or invoke a crawl. Section 7 of
`docs/v1-baseline-crawl-test-checklist.md` is a historical V1 step and must not
be executed, even with a new authorization. A future run requires the tested V2
replacement produced in parent-plan Phase 6 after all implementation gates
pass. A normal `pipeline` start cannot launch the V1 spider, and a `crawl`
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

> [!CAUTION]
> This destructive command is historical. F1 and F2 have not started; preserve
> the retained evidence and do not clean these volumes outside the parent
> plan's future matched writer freeze, backup, restore-test, and reset sequence.
> Backups must remain outside project volumes, and execution requires explicit
> future approval; the command's project restriction is not that approval.

The historical procedure preserved required logs and counts, then confirmed
that `ps --all` listed only the fixed V1 test project. Its full-reset command
was:

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
explicit rollback plan that respects the protocol boundary below. Restarting
an old image alone does not restore compatible datastore state.

The legacy three/four-image digest-env procedure in
[`immutable-pipeline-release-cutover.md`](immutable-pipeline-release-cutover.md)
is preserved as historical V1 context, not current release policy. A future V2
release must use one reviewed exact-byte compatibility-manifest artifact that
binds all required protocol, policy, publication, IPC, configuration, and guard
fields to the protocol-defined image field for every named participant; a tag
or hand-counted image set is insufficient. Parent-plan Phase 6 and F3
milestones M6-M7 own the replacement, and no deployment is authorized. The
future V2 ordinary rollback boundary is the first successful
`CJ2_START_REQUEST` before DNS, not Spider startup or first page publication.

The current [release workflow](../.github/workflows/build-docker-images.yml)
starts build/push jobs on `release.published`, including prereleases. The
[Indexer NLP redistribution gate](../services/indexer/nlp/README.md#offline-verification-and-distribution-gate)
blocks that image before registry login/push, but other matrix images may still
publish. Prereleases skip only the server-copy job; that job copies Compose and
digest files, not a V2 cutover. See the
[current automation warning](immutable-pipeline-release-cutover.md#current-release-automation-warning).
Publishing a release or prerelease is not a dry run and is not authorized here.
