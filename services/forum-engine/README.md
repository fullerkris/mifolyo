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

## Thread and comment votes

`POST /api/votes` sets the authenticated user's vote explicitly:

```json
{"votable_type":"comment","votable_id":42,"value":1,"post_id":12}
```

- `votable_type`: `post` or `comment`; `votable_id`: a positive integer.
- `value`: `1` (upvote), `-1` (downvote), or `0` (remove the existing vote).
  Repeating the same value is a successful no-op, including repeated removals.
- Optional `post_id` binds the target to a thread. A mismatch returns 422.
  The Search UI proxy always supplies this from its route, ignoring browser input.
- Success returns `data.changed`, `data.viewer_vote`, `data.vote` (null after
  removal), and `data.votable` with `type`, `id`, `score`, `upvote_count` and
  `downvote_count`. Scores/counters change atomically under row locks.
- Authentication and private-community membership are required as applicable.
  Locked/removed posts and locked/removed comments reject writes with 423;
  missing resources return 404. Rate-limited requests return 429.
- `GET /api/posts/{post}` and every node in `GET /api/posts/{post}/comments`
  include `viewer_vote` (`-1`, `0`, `1`; `0` for guests/removed comments).
  Comment selections use one batched lookup for the whole tree. These reads and
  vote responses use `Cache-Control: private, no-store`.

Clients should retry an uncertain write with the **same explicit value** and
apply only confirmed response totals. Authentication recovery requires a fresh
user action to vote; it does not replay the failed request.
