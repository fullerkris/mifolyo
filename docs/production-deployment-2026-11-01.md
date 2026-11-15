# Production deployment checkpoint — 2026-11-01

**Status: images built and backups captured; startup/cutover not executed.**
Two attempts to upload the startup script expired waiting for SSH-MCP tool
approval (`APPROVAL_UNAVAILABLE`, 600 seconds each). Final remote inspection
confirmed the original eight running containers, including the legacy web site,
still running. No new production application containers have started.

## Owner decisions

- Use existing Docker host `moogle-main` (`moogleMain@100.99.200.105:4927`).
- Replace Moogle while retaining its data and rollback resources.
- Separate test-server work is deferred at the owner's request.
- SMTP configuration is deferred; password-reset delivery is incomplete.
- C0 remains paused. Its original 322-file candidate was rechecked unchanged,
  with an empty original Git index and all historical evidence preserved.

## Completed

1. Fetched all refs/tags and pulled main before preparation. The local production
   worktree and clean remote `/home/moogleMain/mifolyo` are at
   `e8a0a415ac2b370319cd57f4c5262ce616a536fb`. Production changes are local,
   uncommitted, on `feature/production-deploy-2026-11-01`.
2. Inspected retained MongoDB 7.0.32 database `moogle`: 9,547 pages, 2,126,970
   word rows, 4,026,352 image-word rows, 5,921 images, 110,170 dictionary terms,
   4,731 outlink and PageRank documents each, 1,172,473 backlinks. Sample field
   shapes and query indexes match the query implementation; live application
   compatibility checks remain pending.
3. Captured fresh backups in
   `/home/moogleMain/mifolyo-production/backups/precutover-20261101T193403Z`:

   | Capture | Bytes | SHA-256 |
   | --- | ---: | --- |
   | `mongo.archive.gz` | 161,574,142 | `186a468344adc67921261dd246ce86bc292389051a84afb978e73298da17c403` |
   | `compose-pipeline-redis-1.rdb` | 7,184,484 | `180c638a3301f9be80930b55b1863f9b3e927e451ca37ad5702a9493de9dcd7e` |
   | `compose-query-redis-1.rdb` | 171 | `2f9b8fcf8228e997f8bbbf1e59946b5da2a65d9265a40bc7768d950b98a8ea40` |

   Full MongoDB gzip integrity and both Redis `redis-check-rdb` checks passed.
   The private manifest also covers original container configuration, legacy
   Compose files, and new service configuration. Restore rehearsal is deferred.
   Rollback image tags retain the prior query, Caddy, and client images.
4. Created private random application/database/Redis credentials directly on
   the host, retaining the existing MongoDB query connection. Configuration is
   under `/home/moogleMain/mifolyo-production/config` (0700 directory / 0600 files).
5. Prepared and hash-verified six deployment/seeder files in the remote release
   `/home/moogleMain/mifolyo-production/releases/20261101-e8a0a41`.
   `docker compose ... config --quiet` passed with the actual configuration.
6. Built both production images successfully (build exit 0):

   | Image | Docker image identity |
   | --- | --- |
   | `mifolyo-production-query:20261101-e8a0a41` | `sha256:a3bdcfc0a6363cb5ae3073c3a4b4b4bd3165fb44d5bf2cec2124a8979c473ea4` |
   | `mifolyo-production-forum:20261101-e8a0a41` | `sha256:c67461737cbbb64f75d24cf1d7a491dd80d1f7e8373de1173d40335a8f04b4d5` |

   Both use non-root `app`, PHP-FPM, built assets, and production-only Composer
   dependencies. Build evidence: `build-20261101.log` / `build-20261101.exit`
   under `/home/moogleMain/mifolyo-production`.

## Resume

When the owner can approve SSH operations, follow the
[production runbook](production-deployment.md): start PostgreSQL/Redis, migrate
the new forum database, seed only `general`, and start the application/gateway
and PostgreSQL backup service. Then verify search, forum reads, account pages,
assets and HTTPS behavior before replacing web ingress.

Expected endpoint: `https://srv1459482.tail11b93a.ts.net`. Tailscale 1.104.2 is
connected, DNS name matches, and Serve remains unconfigured. No operator user
is configured, so enabling Serve may require the privileged SSH tool. Ensure
approval precedes stopping legacy ingress. A local cutover helper with automatic
rollback is prepared at `out/production-deploy-2026-11-01/cutover.py`; it has not
been uploaded or executed. Final startup/HTTPS checks are still required.

Existing MongoDB, Redis, crawler, and stopped indexing/ranking jobs retain their
original activation state. This application deployment does not upgrade the
legacy crawler pipeline. New PostgreSQL backups will begin after startup.

## Retained transport history

- Streaming SFTP was unavailable because `defaults.transferRoot` is unset.
  Production source files were instead uploaded as text and verified by SHA-256.
- An unused base64 overlay upload at
  `/home/moogleMain/mifolyo-production-overlay-20261101.b64` was superseded by
  the verified per-file transfers. It was never decoded or extracted; do not
  use it as a release artifact.
- The initial foreground backup exceeded SSH's 60-second command limit.
  Its partial directory `precutover-20261101T193052Z` remains separate from the
  successful background capture.
- The backup preparation script had an initial transport quoting correction
  before execution. The successful script is on the host at
  `/home/moogleMain/mifolyo-prepare-production-20261101.py`; it is one-time
  initialization and must not be rerun against an existing configuration.
- Startup-script upload timed out twice. No startup, migrations, seeding,
  ingress switch, or retirement of legacy containers followed those failures.

Obsidian production checkpoint, roadmap, work journal, and C0 direction notes
were updated chronologically through MCPVault.

## Owner approved resumption; SSH tool approval still unavailable

The owner explicitly approved SSH startup in chat. Read-only checks confirmed
the original containers running, Tailscale Serve still empty, and the production
image build exit still 0. Two further attempts to upload the startup script each
returned `APPROVAL_UNAVAILABLE` after 600 seconds, including a retry after
explaining the separate SSH-MCP approval prompt. This brings startup-upload
approval timeouts to four. The chat approval is recorded, but the tool has not
received its required approval response. No startup or cutover was executed.
