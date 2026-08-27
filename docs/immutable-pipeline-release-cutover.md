# Immutable Page/Image Pipeline Release Cutover

This runbook is required for the incompatible immutable publication protocol
shared by the Spider, Indexer, and Image Indexer. It is a stop-and-drain
cutover, not a rolling deployment. **Never run old and new protocol processes
together, and do not add dual-read compatibility.**

The deployed service Compose files require service-specific image variables
whose values are exact `ghcr.io/fullerkris/mifolyo/<service>@sha256:<64
lowercase hex>` references. Use the reviewed `release-image.env` files
downloaded from the release workflow; never substitute a tag, including the
validated release tag. The release tag (for example `v2026.08.26`) is
organizational metadata for build, change, and rollback records only. PageRank
is not part of the incompatible queue-protocol cutover below, but its deployment
Compose contract is validated in the same preflight. The root
`docker-compose.yml` and isolated baseline Compose file remain source-built,
local development definitions and are not production release inputs.

## Roles, keys, and rollback boundary

The Spider produces immutable page publications on `pages_queue`. The Indexer
consumes that queue and produces immutable image-manifest publications on
`image_indexer_queue`. The Image Indexer consumes those manifests.

| Purpose | Page pipeline | Image pipeline |
|---|---|---|
| Source list | `pages_queue` | `image_indexer_queue` |
| In-flight list | `pages_queue:processing` | `image_indexer_queue:processing` |
| Bounded dead-letter list | `pages_queue:dead` | `image_indexer_queue:dead` |
| Renewable owner lock | `pages_queue:indexer_owner` | `image_indexer_queue:owner` |
| Fencing epoch | `pages_queue:indexer_fence_epoch` | `image_indexer_queue:fence_epoch` |

There are two rollback zones:

1. **Before the new Spider is allowed to produce:** stop the new services and
   restart all three old images together. This rollback may be automated if any
   image, startup, lock, MongoDB, or queue verification fails.
2. **After the new Spider publishes any immutable work:** never point an old
   consumer at the live Redis/MongoDB state. Roll forward, or stop every
   producer and consumer and restore the matched pre-cutover Redis and MongoDB
   backups together. Restoring only one store is not a rollback.

Record the exact UTC time at which the new Spider is started; that is the
irreversible live-state boundary. Query traffic may continue during the drain,
but a coordinated restore requires a full write freeze for every application
sharing the restored Redis database or MongoDB database.

## 1. Release preflight (non-destructive)

Run from a trusted deployment checkout. Supply production secrets through the
approved secret manager; do not put them in shell history or Compose files.
Download and review the four release digest artifacts before copying their
`release-image.env` files into the corresponding deployment directories. Each
file may contain only its service-specific image variable, the release metadata
line, and the generated comment. In `--env-file` mode, the validator deliberately
discards ambient image variables: all four reviewed artifacts must be present,
and each must contain exactly one distinct deployment image entry.

```bash
set -eu
export MIFOLYO_RELEASE_TAG=v2026.08.26 # organizational/change metadata only
export PREVIOUS_MIFOLYO_RELEASE_TAG=v2026.08.01 # rollback record metadata only
export DEPLOY_ROOT="$HOME/SearchEngine"
export PREVIOUS_DEPLOY_ROOT="$HOME/SearchEngine/previous-reviewed-release"

# Required: validates metadata syntax and every downloaded full digest,
# renders all four deployed Compose files, and verifies pull_policy: always and
# absence of a local build. It rejects tags and wrong repositories/services.
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

Attach the four exact pulled `RepoDigests`, the reviewed current and previous
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

## 2. Stop producers, then drain old consumers

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

## 3. Stop old consumers and take matched backups

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

## 4. Deploy the three-image release as one change set

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

# Last pre-boundary gate: automatically stop the new consumers and restore all
# three previous images if any check above fails. Only then start the producer.
$spider_compose up -d --no-build --force-recreate spider-service
date -u +%Y-%m-%dT%H:%M:%SZ # record irreversible live-state boundary
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

## 5. Post-deploy verification and monitoring

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

Keep producers paused for the observation gate. Before the Spider boundary,
deployment automation must stop all three new containers and restart all three
previous image digests when a critical check fails. After the boundary, alerts
must stop new producers and page an operator; they must **not** automatically
start an incompatible old image against live state.

The pre-boundary rollback action is concrete and all-or-nothing. Feeders remain
disabled while automation runs the following consumer-first sequence with the
reviewed previous digest env files:

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

Re-run lock, queue, image-ID, and MongoDB checks before re-enabling feeders.
This sequence is prohibited after the recorded new-Spider boundary.

Resume feeders and crawl triggers only after the observation gate is signed
off. Continue queue-depth, stale-processing, lock-renewal, dead-letter, error
rate, and datastore-latency monitoring through the release window.

## 6. One-time legacy search-term purge

The query engine no longer records raw search terms. After its release is
deployed, run this explicit command once from the query-engine application
container during this checklist:

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

## Completion checklist

- [ ] Four reviewed current digest env files and release metadata recorded.
- [ ] Compose rendering, CI, security scans, and restore rehearsal passed.
- [ ] All feeders/triggers and old Spider replicas stopped.
- [ ] Both old source and processing queue pairs were zero twice.
- [ ] Old consumers stopped; owner locks expired.
- [ ] Matched Redis/MongoDB backups and checksums recorded.
- [ ] New consumers verified before the new Spider started.
- [ ] New queue, processing, dead-letter, owner, and fence key types verified.
- [ ] Irreversible producer-start timestamp and rollback zone recorded.
- [ ] Monitoring and pre-boundary automated rollback were armed.
- [ ] Explicit legacy search-term purge result recorded (never auto-run).
- [ ] Feeders/triggers resumed only after signed verification.
