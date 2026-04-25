# MiFolyo Forum Service Architecture

## Goal

Build the MiFolyo community platform as a standalone, production-ready service that delivers a
Reddit-like experience (communities, posts, comments, voting, moderation), while integrating with
an external search service maintained in a separate repository.

## Scope and Non-Goals

### In Scope

- Community and user identity management
- Post and comment creation, editing, and moderation
- Voting, ranking inputs, and feed generation
- Notifications, reports, and moderation workflows
- Search integration client for external web-search API

### Explicit Non-Goals

- Building crawler, indexer, ranking, or query internals for web search
- Recreating the external search engine data pipeline
- Owning search infrastructure beyond API-level integration

## Architecture Principles (Inspired by `moogle`)

- Service-first layout under `services/`
- Docker-first local development and smoke validation
- Health endpoints for orchestration and CI
- Required checks in GitHub Actions (test/build/security/smoke)
- Clear separation between domain service and infrastructure

## Proposed Service Layout

```text
mifolyo/
  docs/
  scripts/
    docker/
      infra.compose.yml
    fullstack.sh
  services/
    forum-engine/
      app/
      bootstrap/
      config/
      database/
      public/
      resources/
      routes/
      tests/
      docker-compose.yml
      Dockerfile
      .env.example
```

## Core Runtime Components

- **Forum API/Web (`forum-engine`)**: Laravel app that exposes API and initial web surfaces.
- **PostgreSQL**: Primary relational data store for users, communities, threads, and moderation.
- **Redis**: Cache, queue backend, rate limiting, and near-real-time counters.
- **Queue Workers**: Async jobs for notifications, ranking updates, and moderation workflows.
- **Scheduler**: Periodic jobs for hotness recompute, digest generation, and cleanup.
- **Search Gateway Client**: HTTP client to the external search service.

## Domain Model (Initial)

- `users`
- `communities`
- `community_memberships`
- `posts`
- `comments`
- `votes`
- `saved_items`
- `reports`
- `moderation_actions`
- `notifications`

## Access and Role Model

- **User roles**: user, moderator, admin
- **Community roles**: member, moderator, owner
- Policy-driven authorization for create/update/delete/moderation endpoints

## API Surface (Initial)

- `GET /api/health/live`
- `GET /api/health/ready`
- `POST /api/auth/register`
- `POST /api/auth/login`
- `GET /api/communities`
- `POST /api/communities`
- `POST /api/communities/{community}/join`
- `GET /api/feeds/home`
- `GET /api/feeds/community/{slug}`
- `POST /api/posts`
- `GET /api/posts/{post}`
- `POST /api/posts/{post}/comments`
- `POST /api/votes`
- `POST /api/reports`
- `GET /api/mod/queue`
- `GET /api/mod/actions`
- `POST /api/mod/actions`

## Integration Contract: External Search

- Forum service owns only a `SearchGateway` adapter and response mapping.
- Use short timeouts and retry policy with jittered backoff.
- Add circuit breaker behavior for resilience.
- Return graceful fallback states in forum UI/API when search is degraded.
- Capture basic usage metrics (query count, latency, status code distribution).

## Reliability and Observability Baseline

- Structured logs with request IDs and actor IDs
- Error tracking integration points
- Health checks for app, DB, and queue readiness
- Rate limit policies for auth, post creation, voting, and reporting

## CI/CD Baseline

`required-checks.yml` should include:

- Laravel tests
- Static analysis and code style checks
- Build validation (PHP dependencies + frontend assets)
- Security scan (filesystem/image)
- Smoke test via Docker Compose and health endpoints

## Security Baseline

- CSRF and auth hardening for web flows
- Token/session expiration and rotation strategy
- Input validation and output sanitization
- Moderation and abuse controls (rate limits, report queues, audit trails)

## MVP Exit Criteria

- User signup/login, community creation, posting, commenting, and voting work reliably
- Moderator queue and moderation actions are functional and auditable
- Feeds support pagination and stable sort behavior
- External search integration is available with graceful degradation
- CI required checks pass on pull requests and main
