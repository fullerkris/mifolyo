# Search UI local staging

This starts Query Engine, Forum Engine, MongoDB, PostgreSQL, Redis, Caddy and
Mailpit in the dedicated `mifolyo-search-ui-staging` Compose project.
The application uses real controllers and databases with explicitly synthetic
index/community data.

- Search UI: <http://127.0.0.1:18081>
- Forum: <http://127.0.0.1:18082>
- Local reset-email inbox: <http://127.0.0.1:18085>
- Search terms: **staging**, **search**, **documentation**.
- Exact-search edge cases: **ux05 café C++**.
- Image search: <http://127.0.0.1:18081/images?q=ux06>.

Only Caddy and Mailpit publish host ports, all on loopback. Databases, networks
and volumes belong to this project. Its known local-only keys and credentials
must stay confined to this environment. Read the
[environment boundaries](environments.md) before starting.

## Start and seed

Run from the repository root with Docker and Docker Compose available:

```sh
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml build query-engine forum-engine
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml up -d --wait --wait-timeout 180
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml exec -T query-engine php /staging/seed.php query
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml exec -T forum-engine php /staging/seed.php forum
```

Both application images include compiled frontend assets. Forum migrations run
at startup. Query sessions use files and its cache uses Redis; forum sessions
and cache use PostgreSQL. The seed is repeatable and preserves discussions
created during manual or browser validation.

The baseline provides 25 documentation URLs, additional synthetic `example.test`
edge cases, source-linked conversations, image associations and page links.
The artwork is served from `scripts/fixtures/search-ui-media/`.

## Check readiness and browse

```sh
curl --fail http://127.0.0.1:18081/api/health/ready
curl --fail http://127.0.0.1:18082/api/health/ready
```

For manual posting, sign in with `reader@staging.mifolyo.test` and
`local-staging-only-password`, or create an account. Open a search result's
discussion list, start a thread, and follow **Open your conversation** to reply.
The seeded reader predates onboarding; create a new account to see welcome,
Discuss and first-vote guidance.

Password-reset emails go to Mailpit. The forum uses `MAIL_MAILER=smtp` and
`SEARCH_UI_URL=http://127.0.0.1:18081`; `SEARCH_UI_STAGING_MAIL_PORT` changes the
inbox's loopback port. Deployment environments require their own public
`SEARCH_UI_URL` and working mail transport.

## Run the real-service browser checks

From `services/query-engine`, install frontend dependencies and Chromium:

```sh
npm ci
npx playwright install chromium
npm run test:staging
```

These tests exercise search, account and conversation flows, voting, onboarding,
image/page-link discovery and actual outage recovery. They create staging data
and briefly stop/restart this project's forum, MongoDB and mail services,
restoring services in `finally` blocks. Run them when the staging UI can be
interrupted. The suite verifies the staging marker and fixed loopback endpoints.

Screenshots and failure traces are written to ignored
`services/query-engine/test-results/staging/`. Service/API details remain in the
[Query Engine README](../services/query-engine/README.md) and
[Forum Engine README](../services/forum-engine/README.md).

## Inspect, stop and resume

```sh
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml ps
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml logs --tail=80 query-engine forum-engine caddy
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml stop
```

`stop` preserves staging data. Repeat the `up` command to resume.
After source changes, rebuild the applications and recreate the asset-copy
service with the application containers:

```sh
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml build query-engine forum-engine
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml up -d --force-recreate query-assets query-engine forum-engine caddy
```

## Historical validation

Dated validation results, image IDs and implementation reports are preserved in
the [Obsidian documentation archive](README.md#archives), including the complete
original version of this runbook. They describe their recorded revisions.
