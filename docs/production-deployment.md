# Production deployment with the retained Moogle index

Deployment status is recorded in the
[2026-11-01 checkpoint](production-deployment-2026-11-01.md).

The production application uses `scripts/docker/production.compose.yml`.
Query and forum run as non-root PHP-FPM containers, with a Caddy gateway,
authenticated persistent Redis, PostgreSQL 16, and a PostgreSQL backup container.
The query service joins the existing MongoDB network using its retained query
credentials. The existing MongoDB, MongoDB backups, and crawl pipeline remain
managed by their original Compose project.

Production URL: `https://srv1459482.tail11b93a.ts.net` (Tailscale only).
Caddy publishes only `127.0.0.1:18080`; its forum listener is internal.
Tailscale Serve terminates HTTPS. The gateway supplies the HTTPS FastCGI
parameters, so generated URLs and secure session cookies use HTTPS.

## Release layout

On `moogle-main`:

- `/home/moogleMain/mifolyo`: clean checkout of merged main.
- `/home/moogleMain/mifolyo-production/releases/<release>`: application source
  plus the production deployment files, built into release-tagged images.
- `/home/moogleMain/mifolyo-production/config`: private service environment
  files (directory mode 0700, files 0600).
- `/home/moogleMain/mifolyo-production/backups`: pre-cutover MongoDB/Redis
  snapshots, rollback configuration, and daily PostgreSQL custom-format dumps.

Use `scripts/docker/production.env.example` for the nonsecret Compose settings.
Service files are loaded in Compose's `raw` format (Compose >= 2.30):

| File | Values |
| --- | --- |
| `query.env` | Independent random `APP_KEY`, retained `MONGODB_URI` and `MONGODB_DATABASE`, new `REDIS_PASSWORD` |
| `forum.env` | Independent random `APP_KEY`, `DB_PASSWORD`, SMTP settings |
| `postgres.env` | `POSTGRES_USER=postgres`, `POSTGRES_DB=postgres`, random `POSTGRES_PASSWORD`, matching `FORUM_DB_PASSWORD` |
| `redis.env` | Matching random `REDIS_PASSWORD` (hex, suitable for Redis configuration) |
| `backup.env` | `PGHOST=postgres`, `PGDATABASE=mifolyo_forum`, `PGUSER=mifolyo`, matching `PGPASSWORD` |
| `compose.env` | Release tag, production URL, configuration/backup directories, existing MongoDB network |

The PostgreSQL initializer creates a nonsuperuser application role and a separate
community database. Query sessions and cache use Redis, not the forum tables.
Preserve application keys and all persistent volumes across releases.

## Start a release

Run from the release directory:

```sh
dc() {
    docker compose --env-file /home/moogleMain/mifolyo-production/config/compose.env \
        -f scripts/docker/production.compose.yml "$@"
}
dc config --quiet
dc build --pull query-engine forum-engine
dc up -d --wait postgres redis
dc run --rm --no-deps forum-engine php artisan migrate --force
dc run --rm --no-deps forum-engine php artisan db:seed --force --class=ProductionCommunitySeeder
dc up -d query-engine forum-engine query-assets forum-assets gateway postgres-backup
```

The production seeder creates only the public `general` community, idempotently.
It does not create sample accounts, credentials, posts, or comments.
Do not use the development `DatabaseSeeder` / `BaselineDataSeeder` here.

Before switching ingress, verify both readiness endpoints, indexed-page counts,
representative text/image queries, forum reads, account pages, and built assets.
Keep the prior web containers and image IDs available until cutover succeeds.

```sh
curl --fail http://127.0.0.1:18080/api/health/ready
curl --fail http://127.0.0.1:18080/api/stats
dc exec -T gateway wget -qO- http://127.0.0.1:8080/api/health/ready
```

After stopping the old web gateway/client, publish the new gateway with:

```sh
tailscale serve --bg --https=443 http://127.0.0.1:18080
tailscale serve status
```

Verify the HTTPS endpoint from a tailnet client. Disable restart on the retired
web containers after successful cutover so a host reboot cannot resurrect their
old published ports. The original container configuration is retained privately
with the pre-cutover backup.

## Backups and rollback

The 2026-11-01 deployment captures all MongoDB databases, both live Redis
instances, legacy Compose files, original container configurations, and new
configuration. MongoDB gzip checks and Redis RDB checks establish file integrity;
restore rehearsal is a separate check, deferred by the owner to their test server.
An interrupted first capture is retained separately and is not the successful
pre-cutover backup.

The existing daily MongoDB backup service continues. The new PostgreSQL backup
service writes a custom-format dump, archive listing, and SHA-256 daily, without
pruning existing backups. PostgreSQL backups are on the same host; off-host
recovery and restore rehearsals remain follow-up work.

For rollback, disable Tailscale Serve's new port-443 mapping and restart the
retained `compose-caddy-1`, `compose-laravel-1`, and `moogle-client` containers.
Restore their recorded restart policies if keeping the rollback in service.
Stop the new application's gateway and PHP services; retain its PostgreSQL/Redis
volumes and backups, including any accounts created since cutover. The original
MongoDB and crawl Redis are used in place and need no data migration rollback.
Do not run `down -v` or prune retained production resources.

## Email deferred by the owner

Until SMTP is supplied, `forum.env` uses the SMTP mailer with a closed loopback
port. Delivery fails with the application's temporary-unavailability response;
reset tokens are not logged and no test mailbox is deployed. Search, registration,
sign-in, and discussions do not require SMTP.

Configure `MAIL_HOST`, `MAIL_PORT`, `MAIL_SCHEME`, `MAIL_USERNAME`,
`MAIL_PASSWORD`, `MAIL_FROM_ADDRESS`, and `MAIL_FROM_NAME` in the private file,
then recreate `forum-engine` so its cached configuration is rebuilt. Verify
delivery to an owner-controlled mailbox before marking recovery operational.

## Environment boundary

The owner deferred separate test-server work on 2026-11-01. Production startup
and operational health checks do not resume the paused C0 calibration work.
Crawl Jobs V2 and rendering remain dormant; this deployment does not change
the legacy crawler/indexer runtime or their activation state.
