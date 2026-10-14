# Search UI local staging

This runs the real Query Engine, forum API, MongoDB, PostgreSQL, Redis and Caddy
in the dedicated `mifolyo-search-ui-staging` Compose project. The index and forum
contain controlled, explicitly labelled seed data. Requests use the application
controllers and databases; the browser-fixture router and HTTP mocks are absent.

- Search UI: <http://127.0.0.1:18081>
- Forum: <http://127.0.0.1:18082>
- Search terms: **staging**, **search**, **documentation**
- Data: 25 indexed documentation URLs; the first result has 23 discussions and
  one persisted reply per discussion. Metadata and conversations are synthetic.

## Start and seed

Run from the repository root with Docker Desktop running:

```sh
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml build query-engine forum-engine
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml up -d --wait --wait-timeout 180
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml exec -T query-engine php /staging/seed.php query
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml exec -T forum-engine php /staging/seed.php forum
```

The application images build from this checkout, including compiled frontend
assets. The forum applies its migrations to the project-owned PostgreSQL
database at startup. Query sessions use files; query cache/telemetry use Redis.
Forum sessions/cache use PostgreSQL. The seed is repeatable and preserves
discussions created during manual or browser validation.

Only Caddy publishes ports, both on loopback. The data and application networks
are internal, and all database/asset volumes are project-owned. This definition
contains only the Search UI and forum stack. Its known local-only application
keys and database credentials are for this loopback environment.

## Validate

```sh
curl --fail http://127.0.0.1:18081/api/health/ready
curl --fail http://127.0.0.1:18082/api/health/ready
```

From `services/query-engine`, after `npm ci` and `npx playwright install chromium`:

```sh
npm run test:staging
```

The suite checks real search ranking/pagination, dictionary correction, query
encoding/history, source-linked discussions and their pagination, mobile
navigation, theme persistence, and login plus thread creation persisted through
the proxy. Recovery tests briefly stop/restart **this project's** forum and
MongoDB services and verify error/retry behavior. They restore the services in
`finally` blocks. Run the suite when the local staging UI can be interrupted.

Tests verify the staging response marker before operating and use fixed
loopback endpoints. They create a new source-specific thread for each desktop
and mobile write test. Screenshots and failure traces go to
`services/query-engine/test-results/staging/` (ignored by Git and image builds).

For manual beta posting, the seeded forum login is
`reader@staging.mifolyo.test` / `local-staging-only-password`. Obtain its temporary
token with `POST http://127.0.0.1:18082/api/auth/login` and paste it into **Start a
thread → Forum API token** on the Search UI. Browser tests log out their tokens.

## Inspect and stop

```sh
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml ps
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml logs --tail=80 query-engine forum-engine caddy
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml stop
```

`stop` preserves staging data. Use the `up` command above to resume. Rebuild the
images after source changes and recreate the asset-copy service when deploying
fresh assets:

```sh
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml build query-engine forum-engine
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml up -d --force-recreate query-assets query-engine forum-engine caddy
```

## Validation record — 2026-10-14

- Application UI baseline: `6d49f5a` on `feature/search-ui`.
- Fresh builds completed for both application images. Query runtime: PHP
  `8.4.26`, MongoDB extension `1.21.0`.
- `npm run test:staging`: **10 passed, 0 skipped** (29.9 seconds).
  Desktop/mobile search and discussions, encoded queries/history, spelling
  opt-out, empty states, source-isolated persisted writes, and both actual
  service outage/recovery cases passed.
- Query image `npm audit --audit-level=low`: **0 vulnerabilities**.
- Post-validation readiness: HTTP **200** from both services; all six
  long-running containers healthy. The one-shot asset copier exited successfully.
- Screenshots reviewed for desktop results, mobile discussions, and both
  unavailable-service states; retained under the ignored staging test output.
- This is local integration validation with controlled data. No merge or remote
  deployment was performed.

Validated local image IDs (not registry digests):

```text
query: sha256:fa6cd911c14ab4dfcc8464f5e9ce971a63626af3e39042130be230689c9e7953
forum: sha256:ee4c1a18cba2e363d8f6de079bb03cf3031ecfe3637b463031c01ace348a7e9f
```
