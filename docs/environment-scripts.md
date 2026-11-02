# Application environment scripts

These entrypoints start the integrated search/community application. The crawler,
indexing/ranking tools and render worker have their own lifecycle; the existing
V1/V2 restrictions in `docs/environments.md` still apply to those components.

## Requirements

- Bash 3.2+, Python 3.9+, curl, Docker Engine/Desktop and Docker Compose **2.24+**
  (including `up --wait` and JSON configuration output).
- Run the scripts on the Docker host, from any working directory. Remote Docker
  contexts are not supported by the loopback HTTP checks.
- Available local ports and enough Docker memory for two PHP apps and three stores.

## Local test application

```bash
scripts/test-env.sh up
scripts/test-env.sh status
scripts/test-env.sh check
scripts/test-env.sh logs forum-engine
scripts/test-env.sh stop
scripts/test-env.sh up --no-build
```

The default project is **`mifolyo-app-test`**, with dedicated images, networks and
volumes. It does not reuse the existing `mifolyo-search-ui-staging` project or the
retained crawl baseline. URLs:

- Search: <http://127.0.0.1:18181>
- Forum: <http://127.0.0.1:18182>
- Mailpit inbox: <http://127.0.0.1:18185>

`up` builds both apps, refreshes shared assets, waits for healthy services, applies
forum migrations through its startup command, and runs the existing repeatable
staging seed. Search for **staging**, **search**, or **documentation**. Sign in as
`reader@staging.mifolyo.test` / `local-staging-only-password`, or register a local
account. `seed` reruns fixtures while preserving manually created discussions.

To run another instance, set all four variables consistently for every command:

```bash
export MIFOLYO_TEST_PROJECT=mifolyo-app-test-review
export SEARCH_UI_STAGING_HTTP_PORT=18281
export SEARCH_UI_STAGING_FORUM_PORT=18282
export SEARCH_UI_STAGING_MAIL_PORT=18285
scripts/test-env.sh up
```

The underlying staging Compose defaults remain compatible with its existing
18081/18082/18085 runbook when used directly. Its browser suite still targets
those original fixed ports; `test-env.sh check` verifies this script's actual ports.

`stop` retains data. The scripts intentionally have no volume-reset command.
Query sessions and Redis caches in this test definition remain disposable.

## Production application

The dedicated definition is `scripts/docker/production.compose.yml`:

| Service | Runtime/storage |
| --- | --- |
| Query Engine | Existing PHP 8.4 FPM image; Redis-backed sessions/cache |
| Forum Engine | `Dockerfile.production`, non-root PHP 8.4 FPM, no dev Composer dependencies |
| Forum worker | Same forum image; PostgreSQL queues `moderation,default` |
| MongoDB | MongoDB 8, durable volume, root administrator and read-only query user |
| PostgreSQL | PostgreSQL 16, durable forum database |
| Redis | Redis 7, password authentication and persistent AOF |
| Caddy | Serves Query UI; internal-only forum API listener |
| Asset initialization | Two one-shot containers, each using its matching app image |
| Migrations | One-shot forum command, completed before app/worker startup |

### First configuration

```bash
cp scripts/docker/production.env.example .env.production
chmod 600 .env.production
```

Fill in the example's required values. Generate **two different** application
keys with `openssl rand -base64 32` and prepend `base64:`. Generate each database
password separately with `openssl rand -hex 32`. Keep application keys and database
credentials stable across restarts: environment changes do not rotate credentials
inside existing MongoDB/PostgreSQL volumes. Use provider-supplied SMTP credentials.
The environment file is consumed by Compose, never executed as shell code.

Set `MIFOLYO_PUBLIC_URL` to the host's HTTPS Tailscale `.ts.net` origin. Only
`127.0.0.1:18090` is published by default. Datastores and the forum API are private.

```bash
scripts/production-env.sh config
scripts/production-env.sh up
scripts/production-env.sh check
scripts/production-env.sh status
```

`config` validates without printing credentials. `up` pulls the selected images
before stopping the old application. It then starts/waits for stores, migrates
with app writers stopped, refreshes assets, recreates apps/worker/Caddy, and checks
readiness. This is a **single-host maintenance deployment**, not zero downtime.
Migration or asset-copy failure exits nonzero and leaves the application stopped
for diagnosis; there is no automatic schema rollback.

Select release-specific tags or image digests for the two app images. This change
does not publish application images via the existing crawler release workflow.
Until a dedicated application publication workflow is configured, build from a
reviewed checkout on the target architecture, using unique local image tags:

```bash
# Set MIFOLYO_QUERY_IMAGE and MIFOLYO_FORUM_IMAGE to your release-specific tags first.
scripts/production-env.sh build
scripts/production-env.sh up --no-pull
```

Alternatively, build/tag/push those two images to your registry and use ordinary
`up` on the target host. Build operations require tags, not digest-only references.
Do not publish a GitHub release merely to deploy these apps: the existing release
workflow publishes the separately governed pipeline images.

Fresh production volumes contain an empty index and migrated forum schema. Startup
does not import production data or create demo users/communities. Restore the intended
index/database backups or provision real content through the application separately.
Existing server volumes are not automatically adopted; inventory and map/restore them
before replacing an existing deployment. Keep backup/restore procedures from
`docs/environments.md` in place.

### Tailscale HTTPS and email

On the production host, after joining the intended tailnet:

```bash
tailscale serve --bg http://127.0.0.1:18090
scripts/production-env.sh check-public
```

Use the configured HTTP port if changed. Caddy marks Query Engine requests as HTTPS
because this listener sits behind Tailscale Serve; secure session cookies are always
enabled. Tailnet access rules remain host/operator configuration. No public listener
or Tailscale Funnel is created by these scripts.

The forum has outbound networking for SMTP. Validate a real password-reset delivery
and its HTTPS link after configuring the provider. `check` verifies local application
and datastore readiness; it does not claim SMTP delivery or Tailscale availability.

### Other lifecycle commands

```bash
scripts/production-env.sh logs forum-worker
scripts/production-env.sh stop
scripts/production-env.sh --env-file /secure/location/mifolyo.env up
```

Always use the same env file and project. `--project mifolyo-production-<suffix>`
allows a separate instance. Every command explicitly selects its Compose file,
environment file and project; ambient `COMPOSE_FILE`/profiles/project are ignored.
Standard exported application variables still override Compose env-file values.

## Verification

Configuration and failure-path regression tests (no containers started):

```bash
bash -n scripts/test-env.sh scripts/production-env.sh scripts/lib/environment.sh
python3 -m unittest discover -s scripts/tests -p test_environment_scripts.py -v
```

Actual lifecycle verification must additionally exercise fresh startup, repeated
startup, stop/resume, seeded search, account/community writes, queue processing,
mail delivery to a local sink, dependency failures, and data persistence. A local
production-topology pass does not establish readiness of the existing remote host,
its real SMTP provider, or its Tailscale HTTPS endpoint.

For the production smoke fixture, create a private temporary env file using
`production_environment()` from `scripts/tests/test_environment_scripts.py`.
It generates random keys/passwords and selects local `:smoke` image tags, port
18190, and `https://environment-smoke.example.ts.net` (not a real endpoint).
Build and start it with `--project mifolyo-production-smoke`, then start a local
SMTP sink on that project's outbound network:

```bash
docker run -d --name mifolyo-environment-smoke-mail \
  --network mifolyo-production-smoke_mail-egress \
  -p 127.0.0.1:18195:8025 \
  axllent/mailpit:v1.27@sha256:e22dce5b36f93c77082e204a3942fb6b283b7896e057458400a4c88344c3df68
docker compose -p mifolyo-production-smoke --env-file /path/to/smoke.env \
  -f scripts/docker/production.compose.yml exec -T \
  -e MIFOLYO_PRODUCTION_SMOKE=1 query-engine php < scripts/tests/production-smoke.php
```

The PHP fixture creates local accounts/content, exercises the forum API and reset
email, stores a Redis session marker, and verifies that MongoDB rejects query-user
writes. Its explicit environment/URL guards prevent use with ordinary production
configuration. Stop the smoke project and its SMTP sink after verification.

### Local verification — 2026-11-02

Platform: macOS with ARM64 Linux containers, Docker Compose 5.1.3. Based on
`origin/main` at `e8a0a41`; unrelated onboarding work was not included.

| Check | Result |
| --- | --- |
| Bash syntax, ShellCheck 0.11.0, Git whitespace | PASS |
| Configuration/failure-path regression suite | 15 passed |
| Test app image builds, fresh/repeated startup, readiness | PASS |
| Seeded search, compiled assets and alternate-port image URLs | PASS |
| Forum account/community/post/reply/vote/report/moderation smoke | PASS |
| Test stop/resume and repeatable seed | PASS; user/post/comment counts preserved |
| Production image builds and authenticated datastore initialization | PASS |
| Final production fresh-volume startup | PASS; all 16 migrations and application checks |
| Production API/reset-email smoke | PASS; local SMTP delivery with configured HTTPS link |
| Asynchronous moderation queue | PASS; processed jobs, no failed jobs |
| Production private ports, secure cookies, matching asset/app images | PASS |
| Query MongoDB role | PASS; read works, write denied |
| Production dependency failure and full stop/resume | PASS; PostgreSQL counts and Redis-backed session preserved |
| Query frontend dependency audit | **FAIL**: inherited `follow-redirects` 1.16.1, GHSA-mrwp-f3q8-3g9p (high) |
| Remote deployment, actual Tailscale HTTPS, real email provider | Not exercised |

The first production attempt exposed a Compose one-shot wait race. It was corrected
by selecting exited containers explicitly and checking `docker wait` exit values;
the subsequent fresh-volume startup passed. The initial SMTP fixture expected 200
instead of the API's documented 202; the assertion and CLI exception exit handling
were corrected before the final successful run. Dependency lockfiles were unchanged;
the audit finding requires a separate dependency repair before production promotion.
