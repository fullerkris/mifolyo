# V1 Baseline Crawl Test Checklist

Use this checklist for the first bounded crawl of the 70 reviewed V1 manual
seeds in the disposable local environment defined by
`scripts/docker/v1-baseline.compose.yml`. This is a search-only test, not
approval for Curlie or other untrusted bulk sources.

The stack and pre-crawl sections may be validated now, but spider execution is
blocked until DNS-pinned address authorization and redirect revalidation are
implemented and their stop condition below is removed. Reviewed seed syntax is
not a substitute for fetch-time SSRF protection.

## Test objective

Verify that the V1 pipeline preserves canonical absolute URLs from
`crawl_seeds` through Redis, the spider, indexers, MongoDB metadata, search
results. Forum behavior is out of scope and no forum service is in this
environment. The spider may enqueue image references, but external image
fetching and image indexing are explicitly out of scope.

The first run is limited to one spider batch:

```text
--once --max-concurrency 2 --max-pages 10
```

`--max-pages` reserves attempts atomically before workers read the queue.
Invalid, visited, or failed entries consume an attempt, so concurrency cannot
raise the batch above ten outbound request slots.

## Stop conditions

Stop the test immediately if any of these conditions occur:

- The Compose project is not exactly `mifolyo-v1-baseline-test` or the Compose
  file is not exactly `scripts/docker/v1-baseline.compose.yml`.
- The configured MongoDB database or Redis namespace is not the isolated test target.
- A root development MongoDB, Redis, or PostgreSQL service is running with a
  host-published port, or other host database publications have not been inventoried.
- The rebuilt query image reports any npm audit vulnerability at any severity.
- DNS-pinned address authorization and redirect revalidation are not active in
  the spider transport.
- The query containers use different image IDs, the query runtime user is
  wrong, the public asset owner/group check fails, or `query-assets` lacks any
  required isolation control.
- A command would run `FLUSHDB`, `FLUSHALL`, delete a shared volume, or reset
  development, forum, account, or production data.
- The V1 queue and URL map do not contain the same 70 baseline IDs before the crawl.
- A queue ID resolves to a different canonical URL than the corresponding MongoDB seed.
- The spider attempts to fetch a literal IP, local hostname, private address, or unexpected non-default port.
- An opaque 64-character URL ID appears as a human-facing page or image URL.
- The spider is not bounded by `--once --max-concurrency 2 --max-pages 10`.
- `image-indexer` is running or any service fetches an externally supplied image URL.
- Crawl-derived records are being written into a production or otherwise shared environment.

## Known limitations

- Redis currently removes a job before crawl acknowledgement. A process crash or fetch failure can lose pending work until the seed feeder is rerun.
- DNS-pinned SSRF protection and redirect revalidation are not implemented.
  Do not invoke the spider until both controls are implemented and tested.
- Crawl/data network separation removes baseline MongoDB/PostgreSQL Compose DNS
  resolution from the spider; it does not block access to databases published
  through the Docker host gateway or another host address.
- The image indexer does not yet have an SSRF-hardened fetch path. Keep its
  separate `image-pipeline` profile disabled.
- Durable leases, ACK/NACK, retries, cancellation, and dead-letter handling are not implemented.
- Existing legacy crawl-derived MongoDB and Redis data can mix identity
  versions. Do not attach legacy or development volumes to this isolated
  project.

## 1. Record the test context

- [ ] Test date and operator recorded.
- [ ] Branch recorded: `crawl_seeds-and-url-rules-rebuild`.
- [ ] Commit SHA recorded, or the run explicitly identified as an uncommitted worktree test.
- [ ] `git status --short --branch` captured.
- [ ] Compose file recorded as `scripts/docker/v1-baseline.compose.yml`.
- [ ] Compose project recorded as `mifolyo-v1-baseline-test`.
- [ ] MongoDB database `mifolyo_index`, Redis database `0`, and V1 key namespace recorded.
- [ ] Scope recorded as **search only**.

## 2. Confirm data isolation

- [ ] All test named volumes resolve with the `mifolyo-v1-baseline-test_` project prefix.
- [ ] No volume is marked `external` and no development or production volume is mounted.
- [ ] Only Caddy publishes a host port, bound to `127.0.0.1`.
- [ ] MongoDB, Redis, and PostgreSQL publish no host ports.
- [ ] The spider resolves only Redis through baseline Compose DNS; baseline
  `mongo` and `postgres` names are absent from its `crawl` network.
- [ ] Root development MongoDB, Redis, and PostgreSQL are stopped, or a reviewed
  root configuration proves they have no host port publications.
- [ ] Other local containers have been inventoried for database publications.
- [ ] The stack contains no forum service; forum posts, users, and authentication data cannot be reset by this project.
- [ ] Redis database 0 is the isolated project instance and will not be flushed.
- [ ] The legacy `spider_queue` key will not be migrated into V1 or blanket-deleted.
- [ ] Cleanup will use only the explicit project-restricted command in section 11.

Inspect the root development services. If any are running with published
ports, stop them without removing their containers or volumes, then capture the
final status as test evidence:

```bash
docker compose --file docker-compose.yml ps mongo redis postgres
docker compose --file docker-compose.yml stop mongo redis postgres
docker compose --file docker-compose.yml ps mongo redis postgres
```

The final output must show all three stopped, or show approved running services
with no published host ports. Baseline Compose DNS isolation alone is not
sufficient.

Do not proceed when isolation is ambiguous.

## 3. Validate configuration and services

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
docker inspect mifolyo-v1-baseline-test-query-assets-1 \
  --format 'user={{.Config.User}} network={{.HostConfig.NetworkMode}} caps={{json .HostConfig.CapDrop}} read-only={{.HostConfig.ReadonlyRootfs}}'
docker inspect \
  mifolyo-v1-baseline-test-query-assets-1 \
  mifolyo-v1-baseline-test-query-engine-1 \
  --format '{{.Name}} {{.Image}}'
```

- [ ] Compose configuration validates.
- [ ] MongoDB 8 is reachable.
- [ ] Redis 7 is reachable.
- [ ] PostgreSQL 16 is reachable and the Laravel migrations completed before PHP-FPM started.
- [ ] The seed-importer image is built from the current worktree.
- [ ] The query image was installed from the lockfile with `npm ci`, and its
  runtime-image audit reports `found 0 vulnerabilities` at every severity.
- [ ] The query image contains no `.env*` file except the non-secret
  `.env.example` template.
- [ ] `query-assets` and `query-engine` resolve to the tag
  `mifolyo-v1-baseline-test-query-engine:local`; only `query-engine` owns the
  build definition; and both report the same running container image ID.
- [ ] The query image runtime user is `query-engine-user`.
- [ ] `query-assets` reports user `1000:33`, network `none`, dropped capability
  `ALL`, a read-only root filesystem, and exit status 0.
- [ ] Generated public assets are owned by `query-engine-user:www-data` and are
  served successfully from the project-scoped `query-public` volume.
- [ ] The spider command resolves to `--once --max-concurrency 2 --max-pages 10`.
- [ ] No default `STARTING_URL` is configured.
- [ ] `query-assets` exited successfully and Caddy serves the project-scoped `query-public` volume.
- [ ] Caddy is the only service with a published port: `127.0.0.1:${MIFOLYO_V1_TEST_HTTP_PORT:-18080}:80`.
- [ ] `image-indexer` belongs only to `image-pipeline`, which is not active for this test.

`/up` is a Laravel/Caddy liveness check only; it does not establish data-store
or query readiness. Before section 4, run every read-only check below. Expect a
successful `/up`, MongoDB ping, Redis `PONG`, PostgreSQL `t`, and successful JSON
from both query API endpoints:

```bash
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

The query endpoints currently exercise read-only query-engine-to-MongoDB access
only. They do not replace the direct Redis and PostgreSQL checks.

- [ ] `/up` liveness passed and was not treated as dependency readiness.
- [ ] MongoDB, Redis, PostgreSQL/migrations, and query API readiness checks all passed.

## 4. Validate and rebuild the baseline catalog

Preview the baseline and guarded target:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools run --rm seed-importer \
  python crawl_seeds.py rebuild --dry-run
```

Expected result:

```text
70 direct manual records
8 Reddit discovery rows excluded
```

When the isolated rebuild is approved, use the exact confirmation printed by
the dry-run. `MIFOLYO_ENV=test` must be supplied only to this one-off command;
it is intentionally absent from Compose defaults:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools run --rm \
  -e MIFOLYO_ENV=test \
  seed-importer python crawl_seeds.py rebuild \
  --confirm-rebuild mongo:27017/mifolyo_index/crawl_seeds
```

- [ ] Dry-run reports exactly 70 direct records and 8 excluded discovery rows.
- [ ] The confirmation token matches the actual host, database, and collection.
- [ ] Rebuild completes through staged validation and atomic replacement.
- [ ] `crawl_seeds` contains exactly 70 records.
- [ ] All 70 records have `enabled: true`, `schema_version: 1`, and `canonicalization_version: 1`.
- [ ] Required indexes and strict validation are present.

Example read-only verification:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
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
```

## 5. Re-feed and verify the V1 queue

Preview, then feed the enabled records:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools run --rm seed-importer \
  python feed.py --dry-run --limit 1000
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools run --rm seed-importer python feed.py --limit 1000
```

- [ ] Feeder sees 70 enabled records.
- [ ] Feeder skips zero invalid records.
- [ ] `mifolyo:crawl:v1:queue` contains 70 baseline IDs before the crawl.
- [ ] `mifolyo:crawl:v1:urls` contains 70 URL mappings before the crawl.
- [ ] Every queued ID has a URL mapping.
- [ ] A sample ID resolves to the expected canonical absolute URL.

Example read-only verification:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  exec -T redis redis-cli EVAL '
return {
  redis.call("ZCARD", "mifolyo:crawl:v1:queue"),
  redis.call("HLEN", "mifolyo:crawl:v1:urls")
}' 0
```

Expected output is `70` and `70`.

## 6. Start downstream consumers

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile pipeline up -d \
  indexer backlinks-processor
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile pipeline logs --tail=200 \
  indexer backlinks-processor
```

- [ ] Indexer is running and connected to Redis and MongoDB.
- [ ] Backlinks processor is running and connected to Redis and MongoDB.
- [ ] Image indexer is not running; the `image-pipeline` profile remains disabled.
- [ ] `pages_queue` starting length is recorded.
- [ ] Consumer logs show no startup or connection errors.

## 7. Run one bounded spider batch

**BLOCKED:** Do not execute this section while the DNS-pinned address
authorization and redirect-revalidation stop condition remains active. The
command is retained as the post-hardening acceptance procedure.

Immediately before invoking the spider, recheck that root development data
stores have not restarted with published ports:

```bash
docker compose --file docker-compose.yml ps mongo redis postgres
```

- [ ] Root development MongoDB, Redis, and PostgreSQL remain stopped or portless.
- [ ] No image indexer container or external image fetcher is running.

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile pipeline run --rm spider \
  ./spider --once --max-concurrency 2 --max-pages 10
```

- [ ] Spider reports the V1 queue key.
- [ ] Each popped opaque ID resolves to a canonical absolute URL.
- [ ] No more than 10 page attempts are reserved and no more than 10 outbound
  requests are made.
- [ ] HTTP and HTTPS schemes are preserved.
- [ ] No URL is reconstructed by prepending `https://` to an ID or existing absolute URL.
- [ ] Crawl output, errors, redirects, and status codes are captured in the test report.

## 8. Verify pipeline output

- [ ] `pages_queue` drains to zero or its remaining count and reason are recorded.
- [ ] Newly indexed metadata IDs are canonical absolute HTTP/HTTPS URLs.
- [ ] No newly indexed metadata ID is an opaque 64-character digest.
- [ ] Image URLs remain valid absolute URLs without duplicated schemes.
- [ ] Backlink and outlink records use canonical absolute URL identity.
- [ ] `image_indexer_queue` count is recorded but its entries were not fetched.
- [ ] No external image request was made and no image document was indexed.
- [ ] Failed URLs are listed for manual review because automatic retry state is not yet durable.
- [ ] The V1 queue and URL-map counts after outlink discovery are recorded.

## 9. Verify search behavior

Confirm Caddy and Laravel liveness before issuing representative search
queries. This repeats liveness evidence only; dependency readiness must already
have passed in section 3:

```bash
curl --fail --show-error \
  "http://127.0.0.1:${MIFOLYO_V1_TEST_HTTP_PORT:-18080}/up"
```

- [ ] At least three newly crawled pages are found with representative queries.
- [ ] Result links open the expected canonical URLs.
- [ ] Search snippets and titles correspond to the fetched pages.
- [ ] No result URL contains `https://https://` or a bare URL digest.
- [ ] Existing meaningful-result smoke queries still work.

Record each query, expected result, actual result, and pass/fail outcome.

## 10. Confirm the search-only boundary

- [ ] Resolved Compose services contain no forum service.
- [ ] No test step targeted a forum endpoint or forum PostgreSQL database.
- [ ] No development or production forum/account resource was created, changed, or removed.

## 11. Cleanup and restore a known state

- [ ] Logs, before/after counts, and the test report have been preserved.
- [ ] Do not run Redis `FLUSHDB` or `FLUSHALL`.
- [ ] The following status command lists only resources in the fixed V1 test project:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml ps --all
```

After those checks, the only approved full cleanup command is:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  down --volumes --remove-orphans
```

- [ ] Cleanup used the exact file and project above and removed only this test project's disposable containers, networks, and volumes.
- [ ] No generic `docker compose down`, `docker volume prune`, or `docker system prune` command was used.
- [ ] Development, forum/account, and production data remain unchanged.

## 12. Final verdict

Choose exactly one result:

- [ ] **PASS**: all required criteria passed and no stop condition occurred.
- [ ] **PASS WITH ISSUES**: the bounded pipeline worked, but documented non-blocking defects remain.
- [ ] **FAIL**: a stop condition occurred, identity integrity failed, or required evidence is missing.

`PASS WITH ISSUES` is allowed only for an observation explicitly classified as
non-blocking that does not fail a required checkbox or trigger a stop
condition. Any dependency audit, container hardening, data isolation, or URL
identity control failure is `FAIL`.

## Test report template

```markdown
# V1 Baseline Crawl Test Report

**Date:**
**Operator:**
**Branch / commit:**
**Environment:**
**Compose file:** `scripts/docker/v1-baseline.compose.yml`
**Compose project:** `mifolyo-v1-baseline-test`
**MongoDB database:**
**Redis database / queue key:**
**HTTP port:**
**Scope:** Search only
**Root development DB publication status:** Stopped / portless
**Image pipeline:** Disabled

## Before

- crawl_seeds count:
- V1 queue count:
- V1 URL-map count:
- pages_queue count:
- image_indexer_queue count:
- Legacy data present:
- Mongo/Redis/Postgres/query API readiness:

## Execution

- Pages attempted:
- Pages fetched successfully:
- Pages indexed:
- Image references queued:
- Images fetched/indexed: N/A (deferred)
- Fetch or parse failures:
- Unexpected redirects:

## Search checks

| Query | Expected | Actual | Result |
|---|---|---|---|
| | | | |

## Issues

- None / list issue references

## After

- V1 queue count:
- V1 URL-map count:
- pages_queue count:
- MongoDB metadata delta:
- Cleanup performed:

## Verdict

PASS / PASS WITH ISSUES / FAIL
```
