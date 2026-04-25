# MiFolyo Forum Service Implementation Plan

This is an execution plan for building MiFolyo's forum/community service only.

## Delivery Phases

## Phase 1 - Foundation (Week 1)

### Outcomes

- Bootstrapped Laravel service under `services/forum-engine`
- Docker-based local runtime with PostgreSQL and Redis
- Health endpoints and basic CI checks

### Tasks

- Create Laravel app scaffold and environment template
- Add Dockerfile and `docker-compose.yml`
- Add `scripts/docker/infra.compose.yml` and `scripts/fullstack.sh`
- Implement `GET /api/health/live` and `GET /api/health/ready`
- Add first migration set and seeders
- Add smoke test that boots containers and checks health endpoints

## Phase 2 - Core Community Features (Weeks 2-3)

### Outcomes

- End-to-end flows for communities, posts, comments, and votes
- Profile and feed reads with pagination

### Tasks

- Build auth endpoints and session/token strategy
- Implement community CRUD and membership actions
- Implement post create/read/update/delete rules
- Implement threaded comments with depth guardrails
- Implement vote mutation endpoints and score aggregation jobs
- Implement home/community/user feeds with cursor pagination

## Phase 3 - Trust and Moderation (Week 3)

### Outcomes

- Moderation queue and action framework
- Report ingestion and abuse controls

### Tasks

- Add report submission and triage models/endpoints
- Implement moderator actions (remove, lock, warn)
- Add moderation audit trails and actor context
- Configure rate limiting for high-risk endpoints
- Add policy tests for moderator/non-moderator paths

## Phase 4 - Search Integration (Week 4)

### Outcomes

- API client integration with external search service
- Graceful fallback behavior when search is unavailable

### Tasks

- Add `SearchGateway` service class and DTO mapping
- Add timeout/retry/circuit-breaker behavior
- Add integration tests with mocked upstream responses
- Add UI/API fallback state contracts

## Phase 5 - Hardening and Release Readiness (Week 5)

### Outcomes

- Production-readiness checks in place
- Performance and migration safety validated

### Tasks

- Add query and index tuning for feed and comment traversal paths
- Add backup and migration rollback runbook
- Complete required CI checks and branch protection requirements
- Perform load sanity run and resolve P1 bottlenecks

## Implementation-Ready Work Breakdown

## 1) Bootstrap and Infra

- `services/forum-engine/` - Initialize Laravel app structure and baseline configs.
- `services/forum-engine/.env.example` - Add app, DB, Redis, queue, mail, and search-gateway env vars.
- `services/forum-engine/Dockerfile` - Multi-stage image for dependency install and app runtime.
- `services/forum-engine/docker-compose.yml` - App + optional local dependencies wiring.
- `scripts/docker/infra.compose.yml` - Shared Postgres and Redis containers for local development.
- `scripts/fullstack.sh` - `up/down/logs/reset` orchestration similar to `moogle` patterns.

## 2) Database and Models

- `services/forum-engine/database/migrations/*` - Create initial schema for users/communities/posts/comments/votes/moderation.
- `services/forum-engine/database/seeders/*` - Seed starter communities and admin user.
- `services/forum-engine/app/Models/*` - Add domain models and relationship mappings.

## 3) API and Domain Logic

- `services/forum-engine/routes/api.php` - Register versioned endpoints for auth, feeds, posts, comments, votes, reports, mod actions.
- `services/forum-engine/app/Http/Controllers/*` - Add controllers by domain slice.
- `services/forum-engine/app/Http/Requests/*` - Input validation request objects.
- `services/forum-engine/app/Policies/*` - Authorization policies for user and moderator actions.
- `services/forum-engine/app/Services/*` - Feed assembly, scoring, and search-gateway integration.
- `services/forum-engine/app/Jobs/*` - Async jobs for notifications, score recompute, and moderation processing.

## 4) Observability and Safety

- `services/forum-engine/app/Http/Middleware/*` - Request IDs, actor context, and rate-limit helpers.
- `services/forum-engine/config/logging.php` - Structured logging channels and context keys.
- `services/forum-engine/config/services.php` - External search gateway config block.

## 5) Test and CI

- `services/forum-engine/tests/Feature/*` - Endpoint behavior and auth/policy tests.
- `services/forum-engine/tests/Unit/*` - Service-layer and ranking helper tests.
- `.github/workflows/required-checks.yml` - Test/build/security/smoke workflow gates.

## Definition of Done Per Feature

- Endpoint contract documented and implemented
- Validation and authorization covered by tests
- Happy path and critical failure path tested
- Logging fields include actor and request context
- Feature works in local Docker stack

## Risks and Mitigations

- **Risk**: Feed queries degrade as content grows.
  - **Mitigation**: Add targeted DB indexes and cursor pagination early.
- **Risk**: Search dependency downtime impacts UX.
  - **Mitigation**: Circuit breaker + fallback states + cached recent results.
- **Risk**: Abuse/spam volume in early launch.
  - **Mitigation**: Rate limits, report queues, and moderator tooling in MVP.

## Immediate Next Build Steps

1. Scaffold `services/forum-engine` with Laravel and Docker files.
2. Add initial migrations/models/controllers for communities, posts, comments.
3. Wire `scripts/fullstack.sh` and first CI required-checks workflow.
