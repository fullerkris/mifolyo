# Spider

> [!IMPORTANT]
> **Status: current V1 runtime.** The commands, Redis keys, and lifecycle below
> describe V1 only. M1/M2 merged through PR #9 (`d914a93`). The complete dormant
> M3 implementation, including all 43 canonical Lua operations and the sealed
> bundle, is committed and pushed as `81028ca`; local verification passed, not
> M3 protected-PR or real-Redis acceptance. M4 still requires reviewed fixture,
> ACL and bootstrap clarifications plus explicit approval. No V2 runtime wiring,
> migration, deployment, rendering activation or crawling is authorized. See the
> [parent remediation plan](../../docs/spider-render-remediation-plan-2026-09-01.md)
> and [F3 implementation plan](../../docs/crawl-jobs-v2-plan.md).

The Go spider consumes the current V1 Redis crawl queue, fetches
policy-approved web pages, extracts links and images, and writes crawl data back
to Redis. It is a bounded educational crawler, not a general-purpose or
production-scale web crawling service.

> [!WARNING]
> A successful build does not authorize a crawl. The bounded-run command below
> and `docs/v1-baseline-crawl-test-checklist.md` are retained as historical V1
> protocol context. F1, F2, F4, and F6 have not started. F5 code acceptance passed
> protected PR #9 checks; retained-data reconciliation and V2 integration remain
> gated. The V1 safety requirements,
> including portless root development databases and isolated queue metadata,
> remain relevant evidence but cannot authorize another run.

## Security model

The spider fails closed at startup and before each outbound request:

- A strict V1 JSON domain-group policy is required. Unknown fields, malformed
  values, empty user agents, disabled groups, and unmatched domains are denied.
- Static admission rejects literal IPs, local/reserved names, and non-default
  ports. Ambiguous paths, encoded separators, encoded percent signs, invalid
  UTF-8, and dot segments are denied before DNS.
- Proxy environment variables are rejected. The transport resolves each hop
  once, rejects the entire DNS answer set if any address is prohibited, dials
  only approved numeric addresses, and verifies the connected remote address.
- HTTPS verifies the hostname and certificate with TLS 1.2 or newer. HTTP/2,
  cookies, compression, keep-alives, protocol switching, and non-identity
  response content coding are disabled or rejected.
- Redirects are manual, bounded, cycle-checked, protected against HTTPS
  downgrade, and re-matched against policy before DNS or network I/O.
- Robots policy is checked for the initial page and every page redirect.
  `robots.txt` fetches use the same request budgets and secure transport;
  redirects must stay on the original host and enter normal page path policy.
- Every production group must resolve to the exact `MiFolyoBot/1.0` identity;
  startup rejects global or per-group user-agent overrides.
- Robots bodies are limited to 512 KiB and bounded by line, line-length,
  user-agent, rule, and expanded rule-association counts before parsing. Invalid
  UTF-8, malformed escapes, and parser failures use the group's configured
  typed fallback. The process-local cache retains at most 32 origins. The
  spider executable rejects every policy group whose fallback is not
  `on_error: "deny"`, including outside baseline mode.
- HTML discovery uses a streaming tokenizer capped at 100,000 tokens, 2,000
  unique links, 1,000 unique images, 2,048-byte URL attributes, and 1,024-byte
  retained image alt text. Exceeding a limit rejects discovery for that page.
  Image metadata is retained only for statically eligible, policy-approved
  URLs. Downstream image indexing never dereferences those URLs.
- JavaScript rendering is separately allowlisted by exact host and path. The
  browser worker has no network namespace or data-store credentials. Stage 2a
  admits only policy-approved scripts and stylesheets through the Go broker and
  rejects every other browser subrequest. A render failure publishes no page,
  links, images, or visited marker.

Application checks cannot observe every NAT, DNAT, NAT64, or publicly numbered
internal route. Keep outbound firewall controls and `CRAWL_DENY_CIDRS` as
defense in depth.

## Domain-group policy

The portable schema is `contracts/crawl-policy-v1.schema.json`. Example and
baseline policies are in `config/`. Policies are loaded once at startup; there
is no hot reload.

Each group controls:

- exact or apex-and-subdomain host matching
- enabled state and scheduling priority
- allowed schemes and allow/deny path prefixes
- maximum discovery depth
- minimum request interval, concurrency, and per-batch request cap
- redirect mode and maximum hops
- robots mode, error fallback, cache TTL, and user-agent identity; the
  production executable requires `MiFolyoBot/1.0`

Validate a policy without Redis or network access:

```bash
go run ./cmd/spider \
  --validate-policy \
  --crawl-policy-file ./config/crawl-policy-v1.example.json
```

The checked-in baseline has 67 enabled host rules. BBC, Khan Academy, and
PolitiFact are isolated in the explicitly disabled `disabled-sites` group, and
Reddit remains in the disabled `reddit-crawler` group. Matching URLs are denied
before DNS. The complete policy is additionally
pinned to an approved exact SHA-256:

```bash
go run ./cmd/spider \
  --validate-policy \
  --validate-baseline-policy \
  --crawl-policy-file ./config/crawl-policy-v1.baseline.json
```

The required digest is
`50648954d0264f7ac4fdda174178db488e86e335a0b63fdcc448da7bc218bae3`.

## JavaScript rendering scope

Stage 1 networkless inline rendering and Stage 2a brokered scripts and
stylesheets are implemented. The spider strictly loads
`contracts/render-policy-v1.schema.json`, matches enabled exact-host/path rules,
and sends already-authorized document bytes to `services/render-worker` over
IPC V2 on a length-prefixed Unix socket. The worker launches a fresh sandboxed
Chromium process and returns a bounded frozen DOM without browser network
access.

For a `brokered` rule, every external script or stylesheet is paused and sent
back to Go. The page-bound broker independently reapplies the exact render
resource rule, the originating crawl group and depth, robots, the same global
and group request gate, DNS/address controls, TLS verification, direct-response
limits, strict MIME, and UTF-8 validation. Stage 2a permits only canonical
HTTPS GET resources with no query, fragment, non-default port, or redirect. Any
denial or load/execution failure rejects the page without a static fallback.

Static and rendered HTML are stored separately, and the indexer persists
rendered-page source/DOM pairs in its dedicated `page_artifacts` collection
with the exact render-policy SHA-256. It uses broader semantic text extraction
only for rendered pages.

Indexer publications are immutable. Page, outlinks, and image-manifest keys
include a SHA-256 publication version plus the same base64url canonical URL.
Image payload keys additionally include the base64url canonical image URL. The
digest covers sorted image URL/alt records, so an image-only change creates a
new publication. Every page has a manifest, including explicit zero-image
manifests. Page queue entries reference the exact page key. An older claim
therefore cannot delete a newer crawl of the same URL. Publish retries are
deduplicated by one per-publication string
marker with a seven-day TTL rather than an unbounded hash. The retention window
must exceed the spider's supported persistence-retry horizon; retries inside
the window neither recreate consumed data nor duplicate queue entries.

Redis authentication is required by default. Only an isolated local stack may
use the exact `ALLOW_INSECURE_DATASTORES=true` opt-in; connection, read, and
write operations are bounded to five seconds.

Rendering remains disabled by default:

```env
RENDER_POLICY_FILE=/app/config/render-policy-v1.disabled.json
RENDER_SOCKET=/run/mifolyo-render/renderer.sock
```

The `render` Compose profile starts only the networkless worker. An enabled
rule requires that profile and cannot be combined with
`--validate-baseline-policy`. Startup accepts implemented `inline_only` and
zero-redirect `brokered` rules outside baseline mode, but the checked-in render
policy contains no rules. Images, fonts, media, data requests, and arbitrary
third-party resources remain unsupported. See
`docs/javascript-crawling-v1-scope.md` and `services/render-worker/README.md`
for the remaining rollout gates. No checked-in policy or this implementation
authorizes a public rendered crawl.

The
[Spider and Render Worker remediation plan](../../docs/spider-render-remediation-plan-2026-09-01.md)
blocks another crawl until its acceptance gates pass. The
[F3 implementation plan](../../docs/crawl-jobs-v2-plan.md) owns durable-job
status and sequencing.

## Configuration

```env
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0
USER_AGENT=MiFolyoBot/1.0
CRAWL_POLICY_FILE=/app/config/crawl-policy-v1.baseline.json
CRAWL_QUEUE_KEY=mifolyo:crawl:v1:queue
CRAWL_URLS_KEY=mifolyo:crawl:v1:urls
CRAWL_DEPTHS_KEY=mifolyo:crawl:v1:depths
CRAWL_DENY_CIDRS=
RENDER_POLICY_FILE=/app/config/render-policy-v1.disabled.json
RENDER_SOCKET=/run/mifolyo-render/renderer.sock
STARTING_URL=
```

`STARTING_URL` is an explicit development override. It is unset in Compose and
must still pass domain policy. A crawl executed with
`--validate-baseline-policy` forbids this override and requires the documented
user agent and V1 Redis keys. Validation-only mode performs no queue or network
work, so it ignores the value.
`CRAWL_DENY_CIDRS` is an optional comma-separated list of additional canonical
CIDRs to deny.

## Build and test

The conformance suite requires **exact Go 1.25.13**, not simply any newer Go
release: its URL/Unicode oracle checks the pinned toolchain and module versions.
It also requires **Python 3.10+** as `python3` on `PATH`; required-checks CI uses
Python 3.13. The Python verifier uses only the standard library and checked-in
inputs. Run these native commands from `services/spider` in a full checkout:

```bash
GOTOOLCHAIN=go1.25.13 go test -timeout 30m ./...
GOTOOLCHAIN=go1.25.13 go test -race -timeout 90m ./...
GOTOOLCHAIN=go1.25.13 go vet ./...
GOTOOLCHAIN=go1.25.13 go build -o spider ./cmd/spider
```

These package budgets accommodate exhaustive in-memory Lua tests under Go's
race detector; they do not change Redis's separate latency requirements. Leave
`SPIDER_REDIS_INTEGRATION_ADDR` and `RENDER_WORKER_INTEGRATION_SOCKET` unset for
the service-free suite. Those existing integration tests remain opt-in and need
separately approved disposable infrastructure. LuaJIT is optional for the native
Lua parity check, not a substitute for the required Go/Python conformance tests.
See the [shared Lua contributor guide](internal/database/crawljobsv2/lua_src/README.md#assembly)
for read-only generator checks and pinned Unicode maintenance.

Docker builds use the **repository root** as their context. The Dockerfile-specific
allowlist admits only Spider and its root-level conformance inputs. Python,
the generators, fixtures, normative document and seed catalog remain builder-only. The full Go
suite runs during the build, including the root-fixture and seed-policy checks.
The protected `required-build` job builds this same image.

The runtime image includes CA certificates and runs as `65534:65534`. From the
repository root, build the image and then validate its policy without networking
(the build itself may download dependencies):

```bash
docker build -t mifolyo-spider --file services/spider/Dockerfile .
docker run --rm --network none --read-only mifolyo-spider \
  ./spider --validate-policy --validate-baseline-policy \
  --crawl-policy-file /app/config/crawl-policy-v1.baseline.json
```

The service-level Compose file is a current V1 deployment artifact, not a local
build file. It requires `MIFOLYO_SPIDER_IMAGE` to be the exact approved
`ghcr.io/fullerkris/mifolyo/spider@sha256:<64 lowercase hex>` reference from the
reviewed release digest artifact. The
[legacy cutover runbook](../../docs/immutable-pipeline-release-cutover.md)
records the retired V1 page/image procedure for historical interpretation only;
it is not current deployment authorization. Use the root Compose file for local
source builds.

A future V2 release must use one exact reviewed compatibility manifest plus
every image field and contract, Lua source-set, guard-core, and commit-guard
artifact required by the
[Crawl Jobs V2 contract](../../docs/crawl-jobs-v2.md) and F3 plan. Those
authorities own exact membership. Its ordinary rollback boundary is the first
successful `CJ2_START_REQUEST`, recorded before DNS, not service startup or
first publication. No V2 release procedure is implemented here.

## Historical V1 bounded-run example

Both repository Compose definitions isolate the spider behind the separate
`crawl` profile. Its default Compose command validates policy and exits; it does
not crawl. The following command records the old V1 invocation shape; do not
execute it. F3 M6 must replace active execution guidance with tested V2
commands:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile crawl run --rm spider \
  ./spider --once --max-concurrency 2 --max-pages 10 \
  --validate-baseline-policy
```

This historical command is not runnable authorization. The authorization
recorded in the 2026-08-18 report was consumed and does not authorize a repeat
batch.

`--max-pages` limits all outbound attempts in one batch, including robots and
redirect requests. First-hop global and group capacity is reserved before a
destructive claim. Unused reservations are refunded, and a later capacity or
cancellation denial requeues the original candidate at the same score and
depth. All runs are capped at 2 workers and 10 attempts per batch;
`--validate-baseline-policy` additionally rejects continuous execution and
runtime identity or queue-key overrides.

## Current V1 queue lifecycle boundary

The feeder atomically maintains:

```text
mifolyo:crawl:v1:queue  ZSET(url_id => score)
mifolyo:crawl:v1:urls   HASH(url_id => canonical_url)
mifolyo:crawl:v1:depths HASH(url_id => canonical depth from 0 through 9007199254740991)
```

The scheduler inspects all three values in bulk and claims a member only if its
score, URL, and depth still match. A scheduler snapshot is capped at 10,000
pending IDs; exceeding that bound fails closed. Non-finite scores and missing
depth metadata fail closed. The V1 feeder can restore missing depths but
intentionally refuses to overwrite corrupt existing metadata. Selective depth
deletion followed by feeder replay is a historical repair mechanism, not an
approved procedure for retained evidence. Do not repair or replay that state;
any separate disposable-development repair requires explicit review, stopped
writers and verified targets. Retained-state work must follow the parent plan's
matched freeze, backup, restore-test and reset gates.

These structures are pending-work admission, not a durable crawl-job ledger.
They do not provide authoritative cancellation, leases, ACK/NACK, retries,
dead-letter handling, or crash recovery. Capacity/cancellation failures are
requeued, but an ordinary fetch failure or process crash can still require a
feeder replay. Do not use `FLUSHDB` or `FLUSHALL` as a crawl reset.

Completed crawl batches use idempotent Redis persistence and a deterministic
publication ID. The spider retries failed page, link, image, and publication
phases with capped backoff while retaining the batch in memory. One atomic Lua
operation validates all page and image-manifest hashes, writes a bounded-TTL
publication marker, and publishes page keys, so a lost Redis acknowledgement
cannot duplicate or replay an already consumed batch inside that window.
