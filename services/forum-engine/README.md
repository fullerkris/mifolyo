# MiFolyo Forum Engine

Laravel service for MiFolyo's community platform (communities, posts, comments, moderation).

## Requirements

- Docker and Docker Compose plugin

## Quick Start (Repo Root)

1. From the repository root, start infra + service:

   ```bash
   ./scripts/fullstack.sh up
   ```

   This command auto-creates `services/forum-engine/.env` and generates `APP_KEY` if needed.

2. Open the app at `http://localhost:8000`.

3. View logs:

   ```bash
   ./scripts/fullstack.sh logs
   ```

## Service-Only Start (from this directory)

1. From `services/forum-engine`, create an env file:

   ```bash
   cp .env.example .env
   ```

2. Generate an application key:

   ```bash
   php artisan key:generate
   ```

   If PHP is not installed locally, run:

   ```bash
   docker compose run --rm forum-engine php artisan key:generate
   ```

3. From repo root, start shared infra only:

   ```bash
   docker compose -f scripts/docker/infra.compose.yml -p mifolyo-stack up -d postgres redis
   ```

4. In this directory, start only forum service:

   ```bash
   docker compose up --build forum-engine
   ```

## Services in `docker-compose.yml`

- `forum-engine` (Laravel app only)

## Notes

- Current scaffold defaults to PostgreSQL and Redis.
- Shared infra is defined in `scripts/docker/infra.compose.yml`.
- Search functionality is integrated through external API contracts later; this service does not
  implement search engine internals.

## Health Endpoints

- `GET /api/health/live`
- `GET /api/health/ready`

Responses include `X-Request-Id` for request tracing.

## Rate Limits

- `throttle:auth` for register/login
- `throttle:api-read` for read endpoints
- `throttle:api-write` for write endpoints
- `throttle:mod-actions` for moderation actions

## Search Gateway Config

Configured in `config/services.php` under `search` and backed by env vars:

- `SEARCH_API_BASE_URL`
- `SEARCH_API_TIMEOUT_SECONDS`
- `SEARCH_API_CONNECT_TIMEOUT_SECONDS`
- `SEARCH_API_RETRY_ATTEMPTS`
- `SEARCH_API_RETRY_DELAY_MS`

## Current API Endpoints

- `GET /api/feeds/home` (supports `sort=hot|new|top`, `per_page`)
- `GET /api/feeds/community/{community:slug}` (supports `sort=hot|new|top`, `per_page`)
- `POST /api/auth/register`
- `POST /api/auth/login`
- `GET /api/auth/me` (Bearer token)
- `POST /api/auth/logout` (Bearer token)
- `GET /api/communities`
- `POST /api/communities` (Bearer token)
- `POST /api/communities/{community:slug}/join` (Bearer token)
- `POST /api/posts` (Bearer token)
- `GET /api/posts/{post}`
- `GET /api/posts/{post}/comments`
- `POST /api/posts/{post}/comments` (Bearer token)
- `POST /api/votes` (Bearer token)
- `POST /api/reports` (Bearer token)
- `GET /api/mod/queue` (Bearer token, moderator/owner)
- `POST /api/mod/actions` (Bearer token, moderator/owner)
