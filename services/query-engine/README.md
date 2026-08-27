# Query Engine

The Query Engine is the main API and web interface for the Moogle search engine. It provides endpoints and web pages for searching web pages and images, retrieving page metadata, exploring page connections (outlinks and backlinks), and viewing search statistics. The Query Engine is built with Laravel and serves as the bridge between users and the indexed data stored in MongoDB.

## Features

- **Keyword Search**: Search for web pages using keywords, ranked by TF-IDF and PageRank.
- **Image Search**: Search for images using keywords and view associated metadata.
- **Suggestions**: Provides search suggestions and fuzzy matching for misspelled queries.
- **Page Connections**: Explore outlinks and backlinks for any indexed page.
- **Statistics**: View search statistics, top searches, and random page recommendations.
- **Web Interface**: User-friendly frontend for searching and browsing results.
- **REST API**: JSON endpoints for integration with other services or clients.

## Setup

### Using Docker

The recommended way to run the Query Engine is with Docker. This ensures all dependencies are handled and the service runs in an isolated environment.

1. **Install Docker**:  
   Follow the instructions for your OS on the [Docker website](https://docs.docker.com/get-docker/).

2. **Configure Environment Variables**:  
   Create a `.env` file in the `services/query-engine` directory using `.env.example` as the starting point. The local Docker setup uses PostgreSQL for MiFolyo application data and MongoDB for Moogle index/search data:
   ```env
   APP_NAME=MiFolyo
   APP_KEY=base64:your_app_key_here
   APP_ENV=local
   APP_DEBUG=true
   APP_URL=http://localhost

   DB_CONNECTION=pgsql
   DB_HOST=postgres
   DB_PORT=5432
   DB_DATABASE=mifolyo
   DB_USERNAME=mifolyo
   DB_PASSWORD=mifolyo

   MONGODB_URI=mongodb://mongo:27017
   MONGODB_DATABASE=mifolyo_index

   CACHE_STORE=redis
   QUEUE_CONNECTION=redis
   REDIS_CLIENT=predis
   REDIS_HOST=redis
   REDIS_PASSWORD=null
   REDIS_PORT=6379
   ```

3. **Build and Run**:  
   In the `services/query-engine` directory, run:
   ```bash
   docker compose build
   docker compose up
   ```

### Without Docker
PHP 8.4.1 or newer is required, matching `composer.json`, the PHP 8.4 Docker
runtime, and the PHP 8.4 CI job. The process of running the Query Engine without
Docker is a bit more involved, as it requires setting up the environment
manually. For now, refer to the official Laravel documentation for setting up a
Laravel application locally: [Laravel Installation](https://laravel.com/docs/installation).

## Legacy search-term retirement

Raw search-term telemetry is retired. The one-time cleanup is an explicit,
idempotent operator action:

```bash
php artisan security:purge-legacy-search-terms
```

Run it only from the release checklist in
`../../docs/immutable-pipeline-release-cutover.md`; it is never run by startup,
migrations, scheduling, or deployment. It deletes only logical `top_searches`
through Laravel's configured prefixed Redis connection, preserves
`total_searches`, and reports no stored content. Never use Redis `FLUSHDB` or
`FLUSHALL` for this cleanup.

## Frontend Dependency Security

The query image uses `npm ci` so `package-lock.json` is the deterministic input
to every frontend build. Do not replace it with `npm install` in the Dockerfile
or regenerate the lockfile without rerunning the audit and production build.
The Docker context excludes `.env` and every `.env.*` file except
`.env.example`; environment-specific credentials and endpoints must enter at
runtime, never through an image layer.

### Remediation baseline

The 2026-08-09 remediation reduced the runtime-image npm audit from 10
vulnerable packages (2 critical, 7 high, and 1 moderate) to zero. Direct
dependency floors remain within their existing major versions, while the
lockfile resolves the following patched graph:

| Package | Resolved version | Security result |
|---|---:|---|
| `axios` | `1.19.0` | Replaces versions affected by SSRF, proxy/header leakage, prototype-pollution gadgets, and denial of service advisories |
| `follow-redirects` | `1.16.0` | Fixes cross-domain authentication-header leakage |
| `form-data` | `4.0.6` | Fixes weak multipart-boundary generation and CRLF injection |
| `concurrently` | `9.2.4` | Removes the vulnerable Lodash dependency and selects patched `shell-quote` |
| `shell-quote` | `1.9.0` | Fixes command injection and quadratic-complexity denial of service |
| `vite` | `6.4.3` | Fixes development-server file-read and filesystem-deny bypass advisories |
| `postcss` | `8.5.26` | Fixes source-map path traversal, arbitrary file reads, and CSS output XSS |
| `rollup` | `4.62.4` | Fixes arbitrary file writes through path traversal |
| `nanoid` | `3.3.18` | Fixes non-terminating custom generator cases |
| `picomatch` | `2.3.2` and `4.0.5` | Fixes regular-expression denial of service and unsafe glob matching |

This is a point-in-time baseline, not a permanent guarantee. The npm advisory
database changes independently of the repository, so rerun the gate for every
dependency update and immediately before image promotion.

### Verification gate

With Node.js available, validate the source dependency graph and production
assets from this directory:

```bash
npm ci
npm audit --audit-level=low
npm run build
```

From the repository root, rebuild and audit the exact isolated-test image:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  build query-engine
docker run --rm --entrypoint npm \
  mifolyo-v1-baseline-test-query-engine:local \
  audit --audit-level=low
```

Both audits must report `found 0 vulnerabilities`, the Vite production build
must complete, and the rebuilt application must pass `/api/health/ready` before
promotion. Do not use `npm audit fix --force`; review major-version changes
separately and validate their application behavior.

### Non-root asset handling

The image builds frontend assets before switching to `query-engine-user`, then
recursively assigns `public/` to `query-engine-user:www-data` and pre-creates
`/assets` with the same ownership. In the isolated stack, `query-assets` runs as
`1000:33` with no network and copies that output into the shared `query-public`
volume. Caddy mounts the volume read-only. This ownership transition is
required for repeatable startup from a fresh volume; do not solve asset
permission failures by running the copier as root.
