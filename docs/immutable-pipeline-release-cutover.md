# Immutable Pipeline Cutover: V2 Direction and Historical V1 Procedure

## Current status and future V2 release contract

> [!IMPORTANT]
> V1 remains the current runtime. Crawl Jobs V2 is dormant implementation work;
> its completed M3 source bundle does not authorize runtime wiring, migration,
> deployment, candidate promotion, rendering activation, or crawling. This file
> does not provide an executable V2 cutover.

F3 M1/M2 and the F5 code repair passed protected PR #9 checks and merged as
`d914a93`. M3 implements all 43 canonical Lua sources and the sealed,
zero-argument `AuthoritativeScriptBindingSet()` factory. It is locally verified,
committed, and pushed as `81028ca` on `feature/crawl-jobs-v2-lua`, not accepted
through an M3 PR or protected CI. M4 still requires real-Redis fixture, ACL, and
bootstrap clarifications plus explicit approval. F1, F2, F4, and F6 are not
started. Render Stages 0, 1, and 2a remain implemented but disabled; Stage 3 is
not approved. The [primary Crawl Jobs V2 plan](crawl-jobs-v2-plan.md) owns the
current evidence and gates.

A future V2 cutover is coordinated and manifest-driven. One reviewed exact-byte
compatibility-manifest artifact must bind every required protocol, policy,
publication, IPC, configuration, and guard field to the protocol-defined image
field for every named participant. The manifest and its digest are accepted as
one complete unit; release tags and an independently assembled three- or
four-image list are not release identities. `render_worker_image=disabled`
remains mandatory unless rendering receives separate approval.

The [parent remediation plan](spider-render-remediation-plan-2026-09-01.md)
Phase 6 and [Crawl Jobs V2 plan](crawl-jobs-v2-plan.md) milestones M6-M7 own the
tested replacement for this document. The future V2 ordinary rollback boundary
is the first successful `CJ2_START_REQUEST`, durably recorded before DNS;
Spider startup and first page publication are not that boundary.

## Current release automation warning

The checked-in [release workflow](../.github/workflows/build-docker-images.yml)
triggers on `release.published`, including prereleases. Its matrix builds and
pushes images; the prerelease condition skips only the server-copy job, not
build/push. The Indexer entry is blocked before registry login/push by the
[NLP redistribution gate](../services/indexer/nlp/README.md#offline-verification-and-distribution-gate).
Other matrix entries can still publish independently because `fail-fast` is
`false`; an Indexer failure is not an all-images publication block.

The job named `deploy` depends on successful builds and only copies Compose and
digest env files to the server. It does not perform stop/drain, matched backups,
service startup, manifest promotion, or a V2 cutover. Publishing a release or
prerelease is not a dry run and does not satisfy the V2 release gates. Do not
trigger it to validate dormant work; this document authorizes no release.

## Historical V1 procedure (do not execute)

> [!CAUTION]
> Every role table, numbered step, command, and checklist below is retained only
> to interpret V1 change records. It is not current release policy. Do not
> execute it for V1 or V2 deployment or promotion. Preserve this material as
> history until the reviewed Phase 6 replacement exists.

The historical runbook covered the incompatible immutable publication protocol
shared by the V1 Spider, Indexer, and Image Indexer. It used a stop-and-drain
cutover rather than a rolling deployment and prohibited mixed generations and
dual-read compatibility.

The deployed service Compose files still require service-specific image
variables whose values are exact
`ghcr.io/fullerkris/mifolyo/<service>@sha256:<64 lowercase hex>` references. The
historical procedure used reviewed `release-image.env` files downloaded from
the release workflow and did not substitute a tag, including the validated
release tag. The release tag (for example `v2026.08.26`) was organizational
metadata for build, change, and rollback records only. PageRank was not part of
the incompatible queue-protocol cutover below, but its deployment Compose
contract was validated in the same preflight. The root `docker-compose.yml`
and isolated baseline Compose file remain source-built, local development
definitions and are not production release inputs.

### Historical V1 roles, keys, and boundary record

The current V1 runtime still uses these roles and keys: the Spider produces
immutable page publications on `pages_queue`. The Indexer consumes that queue
and produces immutable image-manifest publications on `image_indexer_queue`.
The Image Indexer consumes those manifests. These runtime details do not
authorize the historical cutover.

| Purpose | Page pipeline | Image pipeline |
|---|---|---|
| Source list | `pages_queue` | `image_indexer_queue` |
| In-flight list | `pages_queue:processing` | `image_indexer_queue:processing` |
| Bounded dead-letter list | `pages_queue:dead` | `image_indexer_queue:dead` |
| Renewable owner lock | `pages_queue:indexer_owner` | `image_indexer_queue:owner` |
| Fencing epoch | `pages_queue:indexer_fence_epoch` | `image_indexer_queue:fence_epoch` |

The V1 procedure recorded Spider startup as a conservative proxy for its
live-state boundary because it had no durable V2 request-start record. That
proxy is historical evidence only. It must not be carried into V2 release
logic, and first page publication is not a V2 boundary signal.

For future V2 rollback decisions, use only the first successful
`CJ2_START_REQUEST` and its immutable boundary key. Before that transition, the
matched V1 snapshot, release, and credentials may be restored together while
all writers are stopped. After it, stop every producer and consumer and either
restore the matched Redis/MongoDB backup as one coordinated rollback or roll
forward. Never restore only one store.

### Historical V1 step 1: Release preflight (non-destructive)

The historical procedure ran from a trusted deployment checkout and supplied
production secrets through the approved secret manager rather than shell
history or Compose files. It downloaded and reviewed four legacy release digest
artifacts before copying their
`release-image.env` files into the corresponding deployment directories. Each
file could contain only its service-specific image variable, the release
metadata line, and the generated comment. In `--env-file` mode, the validator
deliberately discarded ambient image variables: all four legacy artifacts had
to be present, and each had to contain exactly one distinct deployment image
entry.

```bash
set -eu
export MIFOLYO_RELEASE_TAG=v2026.08.26 # organizational/change metadata only
export PREVIOUS_MIFOLYO_RELEASE_TAG=v2026.08.01 # rollback record metadata only
export DEPLOY_ROOT="$HOME/SearchEngine"
export PREVIOUS_DEPLOY_ROOT="$HOME/SearchEngine/previous-reviewed-release"

# Historical V1 validator: validates metadata syntax and every downloaded full
# digest, renders all four legacy Compose files, and verifies pull_policy:
# always and absence of a local build. It rejects tags and wrong repositories.
bash scripts/validate-release-compose.sh \
  --env-file "$DEPLOY_ROOT/spider/release-image.env" \
  --env-file "$DEPLOY_ROOT/indexer/release-image.env" \
  --env-file "$DEPLOY_ROOT/image-indexer/release-image.env" \
  --env-file "$DEPLOY_ROOT/page-rank/release-image.env" \
  "$MIFOLYO_RELEASE_TAG"
bash scripts/validate-release-compose.sh \
  --env-file "$PREVIOUS_DEPLOY_ROOT/spider/release-image.env" \
  --env-file "$PREVIOUS_DEPLOY_ROOT/indexer/release-image.env" \
  --env-file "$PREVIOUS_DEPLOY_ROOT/image-indexer/release-image.env" \
  --env-file "$PREVIOUS_DEPLOY_ROOT/page-rank/release-image.env" \
  "$PREVIOUS_MIFOLYO_RELEASE_TAG"

for service in spider indexer image-indexer page-rank; do
  env_file="$DEPLOY_ROOT/$service/release-image.env"
  compose_file="services/$service/docker-compose.yml"
  expected="$(docker compose --env-file "$env_file" --file "$compose_file" config --images)"
  docker compose --env-file "$env_file" --file "$compose_file" config --quiet
  docker pull "$expected"
  matched=false
  while IFS= read -r repo_digest; do
    if [ "$repo_digest" = "$expected" ]; then matched=true; fi
  done <<EOF
$(docker image inspect "$expected" --format '{{range .RepoDigests}}{{println .}}{{end}}')
EOF
  test "$matched" = true # abort unless the pulled RepoDigest is exact
done
```

The historical evidence attached the four exact pulled `RepoDigests`, the reviewed current and previous
digest env files, CI test and vulnerability-scan results, and evidence of a
recent isolated Redis/MongoDB restore rehearsal to the change ticket. Abort if
any digest is missing or different, an image is unsigned when signing is
required, or an image has an unresolved critical vulnerability. A matching
release tag is not image-integrity evidence.

Set deployment paths and read-only datastore clients for the remaining steps:

```bash
export REDIS_URL='rediss://<secret-managed-production-redis-url>/<db>'
export MONGO_URI='mongodb://<secret-managed-production-mongodb-uri>/mifolyo_index'

spider_compose="docker compose --env-file $DEPLOY_ROOT/spider/release-image.env --project-name mifolyo-spider --file $DEPLOY_ROOT/spider/docker-compose.yml"
indexer_compose="docker compose --env-file $DEPLOY_ROOT/indexer/release-image.env --project-name mifolyo-indexer --file $DEPLOY_ROOT/indexer/docker-compose.yml"
image_compose="docker compose --env-file $DEPLOY_ROOT/image-indexer/release-image.env --project-name mifolyo-image-indexer --file $DEPLOY_ROOT/image-indexer/docker-compose.yml"
previous_spider_compose="docker compose --env-file $PREVIOUS_DEPLOY_ROOT/spider/release-image.env --project-name mifolyo-spider --file $DEPLOY_ROOT/spider/docker-compose.yml"
previous_indexer_compose="docker compose --env-file $PREVIOUS_DEPLOY_ROOT/indexer/release-image.env --project-name mifolyo-indexer --file $DEPLOY_ROOT/indexer/docker-compose.yml"
previous_image_compose="docker compose --env-file $PREVIOUS_DEPLOY_ROOT/image-indexer/release-image.env --project-name mifolyo-image-indexer --file $DEPLOY_ROOT/image-indexer/docker-compose.yml"
```

Confirm all queue keys are `none` or `list`; owner/fence keys must be `none` or
`string`. Capture source, processing, and dead-letter counts without printing
queue values:

```bash
redis-cli --no-auth-warning -u "$REDIS_URL" --raw EVAL '
local list_keys = {
  "pages_queue", "pages_queue:processing", "pages_queue:dead",
  "image_indexer_queue", "image_indexer_queue:processing",
  "image_indexer_queue:dead"
}
local scalar_keys = {
  "pages_queue:indexer_owner", "pages_queue:indexer_fence_epoch",
  "image_indexer_queue:owner", "image_indexer_queue:fence_epoch"
}
local result = {}
for _, key in ipairs(list_keys) do
  local kind = redis.call("TYPE", key)["ok"]
  if kind ~= "none" and kind ~= "list" then
    return redis.error_reply(key .. " has unexpected type " .. kind)
  end
  table.insert(result, key .. "=" .. kind .. ":" ..
    (kind == "list" and redis.call("LLEN", key) or 0))
end
for _, key in ipairs(scalar_keys) do
  local kind = redis.call("TYPE", key)["ok"]
  if kind ~= "none" and kind ~= "string" then
    return redis.error_reply(key .. " has unexpected type " .. kind)
  end
  table.insert(result, key .. "=" .. kind)
end
return result' 0
```

Record both dead-letter counts as the release baseline. Do not delete or print
dead-letter entries during this change.

### Historical V1 step 2: Stop producers, then drain old consumers

1. Disable every scheduler, seed feeder, manual crawl trigger, and retry job
   that can feed the Spider. Confirm no one-off producer container is running.
2. Stop every old Spider replica. This freezes production of `pages_queue`:

   ```bash
   $spider_compose stop spider-service
   $spider_compose ps --all
   ```

3. Leave exactly the old Indexer and old Image Indexer running. The Indexer is
   temporarily both the page consumer and image producer; stopping it early
   would strand old page work. Wait for it to empty page source/processing and
   for the old Image Indexer to empty image source/processing.
4. Run the following check twice, at least ten seconds apart. Every returned
   value must be `0` both times:

   ```bash
   redis-cli --no-auth-warning -u "$REDIS_URL" --raw EVAL '
   return {
     redis.call("LLEN", "pages_queue"),
     redis.call("LLEN", "pages_queue:processing"),
     redis.call("LLEN", "image_indexer_queue"),
     redis.call("LLEN", "image_indexer_queue:processing")
   }' 0
   sleep 10
   # Repeat the same EVAL and attach both outputs to the change ticket.
   ```

If the counts do not reach zero, or either dead-letter count rises, stop the
cutover and investigate with the old release. Never clear, rename, move, or
flush queue keys to force a zero. In particular, never use `FLUSHDB` or
`FLUSHALL`.

### Historical V1 step 3: Stop old consumers and take matched backups

After the two stable-zero checks, stop both consumers and verify all old
pipeline containers are stopped:

```bash
$indexer_compose stop indexer-service
$image_compose stop image-indexer-service
$spider_compose ps --all
$indexer_compose ps --all
$image_compose ps --all
```

Wait at least 65 seconds (one owner-lock TTL), then require both owner locks to
be absent. A surviving lock means an old consumer is still active or shutdown
was incomplete:

```bash
sleep 65
test "$(redis-cli --no-auth-warning -u "$REDIS_URL" EXISTS \
  pages_queue:indexer_owner image_indexer_queue:owner)" = "0"
```

Take Redis and MongoDB backups inside the same producer/consumer freeze. Store
them in encrypted, access-controlled backup storage:

```bash
umask 077
backup_dir="/secure/backups/mifolyo-$(date -u +%Y%m%dT%H%M%SZ)-$MIFOLYO_RELEASE_TAG"
mkdir -p "$backup_dir"
redis-cli --no-auth-warning -u "$REDIS_URL" --rdb "$backup_dir/redis.rdb"
mongodump --uri="$MONGO_URI" --archive="$backup_dir/mongo.archive.gz" --gzip
test -s "$backup_dir/redis.rdb"
test -s "$backup_dir/mongo.archive.gz"
sha256sum "$backup_dir/redis.rdb" "$backup_dir/mongo.archive.gz" \
  > "$backup_dir/SHA256SUMS"
sha256sum --check "$backup_dir/SHA256SUMS"
```

Record the backup URI, checksums, Redis database number, MongoDB database, tool
versions, and completion time. Do not proceed if either backup or checksum
verification fails.

### Historical V1 step 4: Deploy the legacy pipeline as one change set

Pull and verify all images before starting any service. Start consumers first,
verify their new owner locks, then start the Spider last. Do not resume feeders
or manual triggers between these commands.

```bash
$spider_compose pull spider-service
$indexer_compose pull indexer-service
$image_compose pull image-indexer-service

$image_compose up -d --no-build --force-recreate image-indexer-service
$indexer_compose up -d --no-build --force-recreate indexer-service

test "$(redis-cli --no-auth-warning -u "$REDIS_URL" TYPE pages_queue:indexer_owner)" = "string"
test "$(redis-cli --no-auth-warning -u "$REDIS_URL" TYPE image_indexer_queue:owner)" = "string"
test "$(redis-cli --no-auth-warning -u "$REDIS_URL" PTTL pages_queue:indexer_owner)" -gt 0
test "$(redis-cli --no-auth-warning -u "$REDIS_URL" PTTL image_indexer_queue:owner)" -gt 0

# Historical V1 pre-start gate: stop the new consumers and restore the previous
# V1 images if any check above fails. Only then start the V1 producer.
$spider_compose up -d --no-build --force-recreate spider-service
date -u +%Y-%m-%dT%H:%M:%SZ # historical V1 startup record; not the V2 boundary
```

Verify each running container reports the exact configured digest reference and
capture its local content-addressable image ID:

```bash
for spec in \
  "$spider_compose:spider-service" \
  "$indexer_compose:indexer-service" \
  "$image_compose:image-indexer-service"; do
  command=${spec%:*}
  service=${spec##*:}
  container_id=$($command ps -q "$service")
  test -n "$container_id"
  expected=$($command config --images)
  actual=$(docker inspect "$container_id" --format '{{.Config.Image}}')
  test "$actual" = "$expected"
  docker inspect "$container_id" --format '{{.Config.Image}} {{.Image}}'
done
```

### Historical V1 step 5: Post-deploy verification and monitoring

Run the type/count preflight EVAL again. Additionally require:

- source, processing, and dead-letter keys remain `none` or `list`;
- owner and fencing keys are strings while each consumer is healthy;
- both owner-lock TTLs remain positive and renew within 10 seconds;
- dead-letter counts do not exceed the recorded baseline without an explained,
  retained forensic item;
- after controlled producer resumption, source/processing counts converge to
  zero and MongoDB acknowledgements show no fencing or duplicate-key errors;
- container restart count, nonzero exits, Redis/MongoDB errors, processing-list
  age, dead-letter growth, and owner-lock absence are monitored and alerted.

The historical procedure kept producers paused for an observation gate and
used its conservative V1 startup proxy to control automatic rollback. That
proxy and its three-container automation are not V2 policy. Future V2
automation must use the exact compatibility manifest and the successful
`CJ2_START_REQUEST` boundary described above.

The historical pre-boundary rollback was all-or-nothing. Feeders remained
disabled during the following consumer-first sequence with the reviewed
previous digest env files; it is not current rollback authorization:

```bash
$spider_compose stop spider-service
$indexer_compose stop indexer-service
$image_compose stop image-indexer-service

$previous_indexer_compose pull indexer-service
$previous_image_compose pull image-indexer-service
$previous_spider_compose pull spider-service
$previous_image_compose up -d --no-build --force-recreate image-indexer-service
$previous_indexer_compose up -d --no-build --force-recreate indexer-service
$previous_spider_compose up -d --no-build --force-recreate spider-service
```

The historical sequence required repeat lock, queue, image-ID, and MongoDB
checks before feeder resumption. It must not be used for V2. After the first
successful `CJ2_START_REQUEST`, ordinary restoration of a V1 release is
prohibited unless the matched Redis/MongoDB backup is restored under a
coordinated write freeze.

The historical procedure required signed observation evidence before feeder
and crawl-trigger resumption, with continued queue-depth, stale-processing,
lock-renewal, dead-letter, error-rate, and datastore-latency monitoring. Those
checks do not authorize resumption under the current gates.

### Historical V1 step 6: One-time legacy search-term purge

The query engine no longer records raw search terms. The historical checklist
included this explicit one-time purge from the query-engine application
container after deployment; it is retained here as history, not execution
authority:

```bash
php artisan security:purge-legacy-search-terms
```

The command passes only logical `top_searches` to Laravel's configured default
Redis connection, so the configured nonempty `REDIS_PREFIX` is applied exactly
once. It never reads or reports stored terms, preserves logical
`total_searches`, is safe to repeat, and fails closed if the prefix is empty.
It is intentionally absent from startup, migration, scheduler, and deployment
automation. Never replace it with a key scan, `FLUSHDB`, or `FLUSHALL`.

Record only the command exit code and whether it reported deleted or already
absent; do not capture Redis key content.

### Historical V1 completion checklist

- [ ] Four reviewed legacy digest env files and release metadata recorded.
- [ ] Compose rendering, CI, security scans, and restore rehearsal passed.
- [ ] All feeders/triggers and old Spider replicas stopped.
- [ ] Both old source and processing queue pairs were zero twice.
- [ ] Old consumers stopped; owner locks expired.
- [ ] Matched Redis/MongoDB backups and checksums recorded.
- [ ] New consumers verified before the new Spider started.
- [ ] New queue, processing, dead-letter, owner, and fence key types verified.
- [ ] Historical V1 producer-start timestamp and rollback proxy recorded; it is
  not treated as the future V2 boundary.
- [ ] Monitoring and pre-boundary automated rollback were armed.
- [ ] Explicit legacy search-term purge result recorded (never auto-run).
- [ ] Feeders/triggers resumed only after signed verification.
