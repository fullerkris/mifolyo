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
- `throttle:account-recovery` for password recovery/deletion (10/minute per IP)
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
- `POST /api/auth/onboarding` (Bearer token; complete `welcome` or `discuss`)
- `POST /api/auth/forgot-password`
- `POST /api/auth/reset-password`
- `DELETE /api/auth/account` (Bearer token, current password, confirmation)
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

## Account-backed onboarding

New API and web registrations set `onboarding_started_at`. The migration
`2026_10_27_000000_add_onboarding_progress_to_users` leaves existing accounts
unenrolled. Registration/login/refresh responses include `data.user.onboarding`;
`GET /api/auth/me` includes `data.onboarding`:

```json
{"welcome":true,"discuss":true,"vote":true}
```

Each boolean means **show this guidance**, not "step completed". Welcome remains
eligible until dismissed or a successful Search UI search. Discuss/vote guidance
also expires exactly seven days after enrollment. Completion timestamps are
private account fields; progress survives logout, token rotation and new devices.

`POST /api/auth/onboarding` accepts `{"step":"welcome"}` or
`{"step":"discuss"}` and returns the three booleans in `data`. It is authenticated,
write-rate-limited, idempotent and account-scoped. It rechecks the token under the
account row lock, coordinating with password reset/deletion and voting. Unsupported
steps (including `vote`) return 422. Existing unenrolled accounts remain unchanged.

Only a successfully committed nonzero post/comment vote completes voting guidance,
within the vote transaction. Failed/rejected votes and zero-value removals do not
complete it; removing a previously saved vote never reopens guidance. Responses
and the Search UI's `/onboarding` gateway are private and non-cacheable.

Deploy the forum migration before serving this version. See
[`docs/search-ui-onboarding.md`](../../docs/search-ui-onboarding.md) for browser
behavior, rollout and local verification.

## Search UI account lifecycle

Set `SEARCH_UI_URL` to the public Search UI origin, e.g. `https://search.example.com`
(no path, credentials, query or fragment), and configure a working Laravel mail
driver. The internal `SEARCH_API_BASE_URL` is a separate setting. SMTP connections
use `MAIL_TIMEOUT` (default 3 seconds); the browser gateway timeout must accommodate
mail delivery. The isolated Search UI staging stack uses a local Mailpit inbox.

- `POST /api/auth/forgot-password`: `email`, optional local `return_to` (maximum
  2048 characters). Existing, unknown and broker-throttled addresses return the
  same generic **202** response. Mail uses the configured origin rather than the
  request Host. Missing public-origin configuration or failed transport returns
  **503**; a transport failure rolls back reset-token creation, allowing a retry.
- `POST /api/auth/reset-password`: `email`, the emailed 64-character hex `token`,
  `password`, `password_confirmation`. Passwords require at least 8 characters.
  Success returns **200** with `reset: true`; invalid/expired/consumed links return
  **422** with a generic token error. Broker expiry is 60 minutes and request
  cooldown 60 seconds by default (`config/auth.php`). Tokens are hashed in storage.
- Reset consumes the token once, rotates the remember token, revokes the API
  token and deletes database-backed forum sessions. Login, token refresh,
  password reset and deletion coordinate on the account row; stale refresh or
  logout requests cannot resurrect or remove a subsequently issued token.
- `DELETE /api/auth/account`: `password` plus accepted `confirmation`. Success is
  **200** with `deleted: true`; wrong password/missing confirmation is **422**.
  The transaction rechecks the current API token and password, removes identity,
  reset tokens, sessions and votes, and recomputes affected vote/member totals.
  Posts/comments remain with a null author. Concurrent voting uses the same
  account → parent post → comment lock order. The legacy web profile deletion and
  password reset also use this shared lifecycle service.

The Search UI exposes CSRF-protected forms at `/forgot-password`, `/reset-password`
and `/account`. Reset secrets are encrypted in the server session after a clean-URL
redirect; only a per-form identifier reaches the reset form. Successful reset
requires a fresh sign-in and retains the validated reading destination/drafts.
Account deletion clears the browser session. Neither flow automatically replays
a discussion action. Private account/recovery pages use `no-store` and
`Referrer-Policy: no-referrer`.
