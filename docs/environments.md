# MiFolyo environments

Keep Compose projects, volumes, credentials and database endpoints separate
between environments.

| Environment | Definition | Access | Data policy |
|---|---|---|---|
| Development local | Root `docker-compose.yml` | Developer workstation | Long-lived developer state |
| Search UI staging local | `scripts/docker/search-ui-staging.compose.yml`, project `mifolyo-search-ui-staging` | Search `http://127.0.0.1:18081`, forum `http://127.0.0.1:18082`, Mailpit `http://127.0.0.1:18085` | Dedicated databases and synthetic seed data |
| Retained V1 baseline | `scripts/docker/v1-baseline.compose.yml`, project `mifolyo-v1-baseline-test` | Caddy on loopback port `18080` by default | Retained evidence; historical execution/reset procedures are blocked |
| Production | Tailscale-only host | `https://srv1459482.tail11b93a.ts.net` | Durable production state with backups |

## Search UI staging

Use the [local staging runbook](search-ui-local-staging.md) for the integrated
search/community application. It builds Query Engine and Forum Engine, starts
their dedicated stores, and supplies explicit seed and readiness commands.
Its recovery tests temporarily interrupt only the staging project's services.

## Development local

The root `docker-compose.yml` is a separate general development environment.
It may contain developer data and optional forum, pipeline and batch services.
For an approved local development environment, inspect the configuration and
start the default services with:

```bash
docker compose config --quiet
docker compose up -d
docker compose ps
```

Keep the optional `pipeline`, `crawl`, `batch` and `render` profiles stopped.
The root file publishes MongoDB, Redis and PostgreSQL ports; do not run a
root-stack Spider while those publications exist. Use the isolated staging
project for the application journey rather than reusing retained crawl data.

The root and V1 baseline Compose files explicitly use
`ALLOW_INSECURE_DATASTORES=true` for local-only unauthenticated MongoDB/Redis.
Do not carry that exception into shared or production deployments.
Never use blanket Redis flushes or volume-prune commands against these projects.

## Retained V1 baseline

V1 is the current runtime; Crawl Jobs V2 is dormant implementation work.
C0 runtime remains owner-paused. Native ARM64 calibration and separate
test-server work remain deferred.

The old baseline build, seed, feed, consumer, crawl, ranking and reset sequences
are historical records, not current startup instructions. Preserve the retained
containers, volumes and evidence. A future run requires the accepted replacement
procedure and its own execution decision.

The original environment runbook and baseline checklists are in the
[documentation archive](README.md#archives). Their original text remains
available for reproduction at the recorded source revision.

## GitHub Actions databases

`unit-tests.yml` and `required-checks.yml` use fresh per-job MongoDB containers
pinned to
`mongo:9.0.4@sha256:dc28ed5a9d0fc616903c3f463bb1e53b92b49f441721b91926e87d90bf957ce8`.
`scripts/verify-ci-mongodb.sh` checks the service's expected version and ping
before integration tests. Update the image digest and expected version together.
The retained V1 environment uses MongoDB 8; CI results do not revalidate it.

## Tailscale-only production

The supported application endpoint is
<https://srv1459482.tail11b93a.ts.net>. Its underlying Tailscale address is
`100.99.200.105`; use the MagicDNS HTTPS name for application access.
Tailscale Serve can terminate HTTPS and forward to a loopback-bound application,
with tailnet ACLs restricting access to approved operators and users.

Keep MongoDB, Redis and PostgreSQL on private container networks. Do not expose
their ports publicly or through the tailnet, and do not reuse a local Compose
project as a production definition.

Production changes require the deployment-specific configuration, backups and
restore checks, health monitoring and a compatible rollback procedure.
The November 1 preparation, separate startup branch and deployed `production.sh`
setup have different configurations; use the reconciled deployment source before
choosing commands. This runbook supplies environment boundaries, not a new
production deployment procedure.

A future V2 release requires one reviewed exact-byte compatibility manifest.
Mixed V1/V2 operation is prohibited; the first successful `CJ2_START_REQUEST`
before DNS is the ordinary rollback boundary. Rendering remains disabled.
The [protocol](crawl-jobs-v2.md) defines the compatibility contract.

The [release workflow](../.github/workflows/build-docker-images.yml) starts
build/push jobs on `release.published`, including prereleases. Publishing a
release is an operational action. The
[Indexer NLP distribution gate](../services/indexer/nlp/README.md#offline-verification-and-distribution-gate)
applies independently.
