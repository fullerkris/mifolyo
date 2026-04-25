# MiFolyo Forum Service Scaffold Checklist

Use this checklist while implementing the first working version of `services/forum-engine`.

## Repository and Infra

- [x] Create `services/forum-engine/` Laravel project directory
- [x] Add `services/forum-engine/.env.example`
- [x] Add `services/forum-engine/Dockerfile`
- [x] Add `services/forum-engine/docker-compose.yml`
- [x] Add `scripts/docker/infra.compose.yml` for Postgres and Redis
- [x] Add `scripts/fullstack.sh` with `up/down/logs/reset`

## Routing and Health

- [x] Add `services/forum-engine/routes/api.php`
- [x] Implement `GET /api/health/live`
- [x] Implement `GET /api/health/ready`

## Data Layer

- [x] Migration: `users` (Laravel default or adjusted)
- [x] Migration: `communities`
- [x] Migration: `community_memberships`
- [x] Migration: `posts`
- [x] Migration: `comments`
- [x] Migration: `votes`
- [x] Migration: `reports`
- [x] Migration: `moderation_actions`
- [x] Add seeders for baseline test data

## Core API Endpoints

- [x] `POST /api/auth/register`
- [x] `POST /api/auth/login`
- [x] `GET /api/communities`
- [x] `POST /api/communities`
- [x] `POST /api/communities/{community}/join`
- [x] `GET /api/feeds/home`
- [x] `GET /api/feeds/community/{slug}`
- [x] `POST /api/posts`
- [x] `GET /api/posts/{post}`
- [x] `POST /api/posts/{post}/comments`
- [x] `POST /api/votes`
- [x] `POST /api/reports`
- [x] `GET /api/mod/queue`
- [x] `POST /api/mod/actions`
- [x] `GET /api/mod/actions` (moderation history endpoint)

## App Layers

- [x] `app/Models/*` for core entities
- [x] `app/Http/Controllers/*` by feature area
- [x] `app/Http/Requests/*` validation classes
- [x] `app/Policies/*` authorization rules
- [x] `app/Services/FeedService.php`
- [x] `app/Services/SearchGateway.php` (external integration only)
- [x] `app/Jobs/*` for async tasks

## Reliability and Safety

- [x] Configure per-endpoint rate limits
- [x] Add moderation action audit logging
- [x] Add structured request logs with request ID and user ID
- [x] Add timeout and retry config for SearchGateway

## Tests and CI

- [x] Add feature tests for auth and community flows
- [x] Add feature tests for post/comment/vote flows
- [x] Add moderation policy and report-flow tests
- [x] Add unit tests for feed ranking helpers
- [x] Add `.github/workflows/required-checks.yml`
- [x] Add Docker smoke test in CI

## Exit Criteria

- [x] Full local stack boots with one command
- [x] Health checks return expected status
- [x] Core community flows pass manual smoke test
- [ ] Required CI checks are green
