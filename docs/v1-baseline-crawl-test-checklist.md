# V1 Baseline Crawl Test Checklist

Use this checklist for a bounded crawl of the 67 enabled V1 manual seeds in the
70-record catalog, using the disposable local environment defined by
`scripts/docker/v1-baseline.compose.yml`. This is a search-only test, not
approval for Curlie or other untrusted bulk sources.

The spider's DNS-pinned transport, redirect revalidation, robots enforcement,
and exact baseline-policy validation are implemented and tested. That removes
the former code-level blocker, but it does not authorize a crawl by itself.
Section 7 remains operationally blocked until sections 1 through 6, including
the host-publication and three-key queue preflight, are completed and recorded.
The policy also contains disabled `disabled-sites` and `reddit-crawler` groups.
BBC, Khan Academy, PolitiFact, and Reddit are not part of the 67 enabled
baseline domains and must remain disabled.

> [!IMPORTANT]
> The first authorization was consumed by the run recorded on 2026-08-18.
> This checklist does not authorize a repeat. Before any future section 7 run,
> obtain a new authorization and record explicit dispositions for every prior
> robots, challenge, or usage-term issue, including BBC and Khan Academy.

## Test objective

Verify that the V1 pipeline preserves canonical absolute URLs from
`crawl_seeds` through Redis, the spider, indexers, MongoDB metadata, PageRank,
and search results. Forum behavior is out of scope and no forum service is in this
environment. The spider may enqueue image references, but external image
fetching and image indexing are explicitly out of scope.
JavaScript rendering is also out of scope. Stage 1 inline rendering exists, but
the checked-in render policy is empty and the `render` profile must not run for
this static baseline.

A run is limited to one spider batch:

```text
--once --max-concurrency 2 --max-pages 10
```

`--max-pages` is a hard global outbound-attempt budget. First-hop global and
domain-group capacity is reserved before a queue claim; an unused reservation
is refunded. Every robots, page, and redirect request consumes a slot, and a
later capacity denial requeues the original candidate at the same score and
depth. Concurrency cannot raise the batch above ten outbound attempts.

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
- The V1 queue, URL map, and depth map do not contain the same 67 enabled baseline IDs
  before the crawl, or any baseline depth is not canonical `0`.
- A queue ID resolves to a different canonical URL than the corresponding MongoDB seed.
- The spider attempts to fetch a literal IP, local hostname, private address, or unexpected non-default port.
- An opaque 64-character URL ID appears as a human-facing page or image URL.
- The spider is not bounded by `--once --max-concurrency 2 --max-pages 10`.
- `image-indexer` is running or any service fetches an externally supplied image URL.
- A render worker or browser process is running, or the spider attempts a
  browser-originated resource request.
- A Reddit URL is queued or fetched while `reddit-crawler` is disabled.
- Crawl-derived records are being written into a production or otherwise shared environment.
- PageRank publication starts while the spider or indexer is running, before
  the indexer's final buffer flush is proven successful, or while
  `pages_queue` is non-zero.
- PageRank input contains a malformed or non-absolute URL identity, changes
  between validation and publication, or produces a non-finite, negative, or
  non-normalized rank.

## Known limitations

- Redis currently removes a job before crawl acknowledgement. Capacity and
  cancellation failures are requeued, but a process crash or ordinary fetch
  failure can still lose pending work until the seed feeder is rerun.
- Application address checks cannot observe every NAT, DNAT, NAT64, or
  publicly numbered internal route. Keep the host-publication inventory and
  outbound network controls as defense in depth.
- Crawl/data network separation removes baseline MongoDB/PostgreSQL Compose DNS
  resolution from the spider; it does not block access to databases published
  through the Docker host gateway or another host address.
- The image indexer does not yet have an SSRF-hardened fetch path. Keep its
  separate `image-pipeline` profile disabled.
- External-resource brokering is implemented but remains unauthorized and
  disabled for the baseline. Keep the render policy empty and follow
  `docs/javascript-crawling-v1-scope.md` before approving any rendered crawl.
- Durable leases, ACK/NACK, retries, cancellation, and dead-letter handling are not implemented.
- Existing legacy crawl-derived MongoDB and Redis data can mix identity
  versions. Do not attach legacy or development volumes to this isolated
  project.
- The `backlinks` collection is an additive historical projection and is not
  authoritative for PageRank. Ranking must derive the current reverse graph
  from `outlinks` instead.

## 1. Record the test context

- [ ] Test date and operator recorded.
- [ ] Current branch recorded.
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
  --profile tools --profile pipeline --profile ranking --profile crawl --profile image-pipeline config --quiet
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile tools --profile pipeline --profile ranking --profile crawl build --pull
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
- [ ] The spider belongs only to the `crawl` profile; it is absent from
  `pipeline`, and its default Compose command is validation-only.
- [ ] The spider image runtime user is `65534:65534`, includes its CA bundle,
  and validates in a read-only container with networking disabled.
- [ ] Baseline policy validation reports SHA-256
  `50648954d0264f7ac4fdda174178db488e86e335a0b63fdcc448da7bc218bae3`.
- [ ] Baseline policy reports 67 enabled host rules plus disabled
  `disabled-sites` and `reddit-crawler` groups; all matching URLs are denied
  before DNS.
- [ ] `render-policy-v1.disabled.json` contains no render rules, the `render`
  profile is inactive, and the spider image contains no browser binary.
- [ ] No default `STARTING_URL` is configured.
- [ ] The spider uses `MiFolyoBot/1.0` and the exact V1 queue, URL, and depth
  keys; baseline execution rejects overrides.
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
70 direct manual records (67 enabled, 3 disabled)
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
- [ ] Exactly 67 records have `enabled: true`; BBC News, Khan Academy, and
  PolitiFact have `enabled: false`.
- [ ] All 70 records have `schema_version: 1` and `canonicalization_version: 1`.
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

- [ ] Feeder sees 67 enabled records.
- [ ] Feeder skips zero invalid records.
- [ ] `mifolyo:crawl:v1:queue` contains 67 baseline IDs before the crawl.
- [ ] `mifolyo:crawl:v1:urls` contains 67 URL mappings before the crawl.
- [ ] `mifolyo:crawl:v1:depths` contains 67 depth mappings before the crawl.
- [ ] Every queued ID has both URL and depth mappings, and every depth is the
  canonical string `0`.
- [ ] A sample ID resolves to the expected canonical absolute URL.

Example read-only verification:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  exec -T redis redis-cli EVAL '
local depths = redis.call("HVALS", "mifolyo:crawl:v1:depths")
local all_zero = 1
for _, depth in ipairs(depths) do
  if depth ~= "0" then
    all_zero = 0
    break
  end
end
local queue_ids = redis.call("ZRANGE", "mifolyo:crawl:v1:queue", 0, -1)
local missing_metadata = 0
for _, url_id in ipairs(queue_ids) do
  if redis.call("HEXISTS", "mifolyo:crawl:v1:urls", url_id) ~= 1 or
     redis.call("HEXISTS", "mifolyo:crawl:v1:depths", url_id) ~= 1 then
    missing_metadata = missing_metadata + 1
  end
end
return {
  #queue_ids,
  redis.call("HLEN", "mifolyo:crawl:v1:urls"),
  redis.call("HLEN", "mifolyo:crawl:v1:depths"),
  missing_metadata,
  all_zero
}' 0
```

Expected output is `67`, `67`, `67`, `0`, and `1`. The final two values prove
every queued ID has both mappings and every initial depth is the canonical
string `0`. Equal counts then exclude extra hash-only IDs.

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

> [!WARNING]
> Do not execute this section until every checkbox in sections 1 through 6 has
> passed in the current test context. Transport hardening is complete, but the
> host-publication and isolated queue/depth gates remain mandatory.
> The 2026-08-18 authorization cannot be reused.

Immediately before invoking the spider, recheck that root development data
stores have not restarted with published ports:

```bash
docker compose --file docker-compose.yml ps mongo redis postgres
```

- [ ] Root development MongoDB, Redis, and PostgreSQL remain stopped or portless.
- [ ] No image indexer container or external image fetcher is running.
- [ ] No render worker is running and no disabled-site or Reddit URL is pending
  in the V1 queue.

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile crawl run --rm spider \
  ./spider --once --max-concurrency 2 --max-pages 10 --validate-baseline-policy
```

- [ ] The resolved spider environment uses the V1 queue, URL, and depth keys.
- [ ] Each popped opaque ID resolves to a canonical absolute URL.
- [ ] No more than 10 outbound attempts are committed across robots, pages, and
  redirects.
- [ ] The loaded policy digest exactly matches the approved baseline digest.
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
- [ ] The V1 queue, URL-map, and depth-map counts after outlink discovery are recorded.

## 8A. Validate and publish PageRank

This batch operates only on the isolated MongoDB data already produced by the
bounded crawl. It does not start the spider, make an external request, or
authorize another crawl.

Before ranking:

- [ ] No spider container exists or is running.
- [ ] `pages_queue` is `0` on two checks at least five seconds apart.
- [ ] The indexer completed its final MongoDB buffer flush without an error and
  was then stopped cleanly; an empty Redis queue alone is not proof of a flush.
- [ ] The backlinks processor is stopped. PageRank does not consume its
  additive `backlinks` projection.
- [ ] A read-only validation pass reports a non-empty graph and a deterministic
  graph SHA-256 without writing a `pagerank` collection.
- [ ] Every URL referenced by searchable `words` records exists in `metadata`.
- [ ] The graph SHA-256 is unchanged immediately before publication.
- [ ] No stale `pagerank_locks` publication document exists. Publication holds
  this single-writer lock from before graph loading through activation.

The PageRank graph and algorithm must satisfy all of these requirements:

- [ ] Rankable nodes are exactly the canonical absolute HTTP/HTTPS `_id`
  values in `metadata`.
- [ ] Edges are unique source-target pairs from current `outlinks` where both
  endpoints are rankable nodes; unindexed sources and targets are excluded.
- [ ] A missing, empty, or fully filtered outlink set is treated as a dangling
  node and its rank mass is redistributed across all rankable nodes.
- [ ] The damping factor, convergence tolerance, maximum iterations,
  algorithm version, canonicalization version, graph counts, and graph hash
  are recorded.
- [ ] Empty or malformed input, non-convergence, a detected input change before
  activation, or a pre-activation MongoDB error exits non-zero without changing
  the active output. Producers remain operationally quiesced because they do
  not participate in the PageRank publisher lock.

Run the batch only through the isolated `ranking` profile. Capture the graph
hash from the first command and provide that exact value to the second:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile ranking run --rm page-rank
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile ranking run --rm page-rank \
  ./page-rank --publish \
  --expected-graph-sha256=<validated-sha256> \
  --confirm-target=mongo:27017/mifolyo_index/pagerank
```

After ranking:

- [ ] Publication atomically replaces the complete `pagerank` collection;
  stale rows from an older graph cannot survive.
- [ ] `pagerank` and `metadata` contain exactly the same URL IDs and document
  counts.
- [ ] Every rank is finite, non-negative, no greater than `1`, and the sum is
  within `1e-12` of `1`.
- [ ] Every row records one run ID, graph SHA-256, algorithm version, and
  canonicalization version, and a descending rank index exists.
- [ ] The independently recomputed stationary L1 residual is at most `1e-10`.
- [ ] No staging collection remains after successful publication.
- [ ] No publication lock document remains after a successful publication.
- [ ] Repeating the batch against the unchanged graph reports
  `already_current` and leaves the active ranks unchanged.
- [ ] If the filtered graph has no edges, every node has rank `1/N` within
  `1e-12`.

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
- [ ] PageRank is normalized before weighting and applied to the complete
  matching result set before pagination.
- [ ] The top-ranked-page endpoint returns a URL present in `metadata` whose
  rank equals the maximum active PageRank value.

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
**JavaScript rendering:** Disabled
**Policy SHA-256:**
**Spider image digest:**
**Reddit crawler group:** Disabled

## Before

- crawl_seeds count:
- V1 queue count:
- V1 URL-map count:
- V1 depth-map count / values:
- pages_queue count:
- image_indexer_queue count:
- Legacy data present:
- Mongo/Redis/Postgres/query API readiness:

## Execution

- Outbound attempts (robots/pages/redirects):
- Pages fetched successfully:
- Pages indexed:
- Image references queued:
- Images fetched/indexed: N/A (deferred)
- Fetch or parse failures:
- Unexpected redirects:

## PageRank checks

- Input graph SHA-256:
- Nodes / internal edges / filtered targets / dangling nodes:
- Iterations / convergence residual / rank sum:
- Published run ID:
- Metadata/PageRank ID-set comparison:
- Idempotent rerun result:

## Search checks

| Query | Expected | Actual | Result |
|---|---|---|---|
| | | | |

## Issues

- None / list issue references

## After

- V1 queue count:
- V1 URL-map count:
- V1 depth-map count:
- pages_queue count:
- MongoDB metadata delta:
- Cleanup performed:

## Verdict

PASS / PASS WITH ISSUES / FAIL
```
