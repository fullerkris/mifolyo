# JavaScript Crawling V1 Scope

**Status:** Stages 0, 1, and 2a are implemented and disabled by default. The
networkless worker can execute inline JavaScript and Go-brokered external
scripts and stylesheets. No public rendered crawl or baseline rendering is
authorized by this document.

## Motivation

The static crawler can fetch a JavaScript application shell without seeing the
content created after script execution. The first baseline run demonstrated
this with Google Fonts: the response contained metadata and an empty
`<gf-root>`, but no paragraph text for the indexer.

The Sitebulb guide, [How to Crawl JavaScript Websites](https://sitebulb.com/resources/guides/how-to-crawl-javascript-websites/),
describes the relevant workflow: use an HTML crawler for server-rendered sites,
use a Chrome-based crawler when important content appears only after script
execution, and compare the original response with the rendered DOM. MiFolyo
will preserve that response-versus-render distinction, but it cannot give a
browser unrestricted network access without discarding the spider's current
security guarantees.

## Goals

- Render only exact, reviewed host and path rules.
- Keep Go `securefetch` as the only component allowed to reach the network.
- Count every robots, document, redirect, script, stylesheet, and data request
  against explicit global and crawl-group budgets.
- Apply URL policy, robots policy, DNS/address checks, TLS verification,
  redirect checks, and response limits before every network request.
- Persist the original response and rendered DOM as distinguishable artifacts.
- Compare static and rendered extraction for text, links, metadata, and images.
- Fail closed without publishing a partial or silently static page when a
  render-required job fails.

## Non-goals

- Bypassing bot challenges, login walls, paywalls, CAPTCHAs, or site terms.
- Authenticated browsing, cookies, form submission, POST requests, or sessions.
- Browser-native networking, DNS, redirects, downloads, WebSockets, WebRTC,
  service workers, popups, iframes, or JavaScript-driven top navigation.
- Fetching external images, fonts, media, or arbitrary third-party resources.
- Enabling rendering in the existing V1 baseline crawl.

An empty response is not sufficient reason to render automatically. It may be
a legitimate shell, an error page, or an anti-bot challenge. Rendering must be
enabled by an exact reviewed rule in a separately approved render policy.

## Trust Boundary

A normal Playwright or Chrome `goto` call is not acceptable. It would use the
browser's resolver and transport, make uncounted subresource requests, follow
unreviewed redirects, and expose Redis credentials and the spider network to
hostile JavaScript.

The required topology is:

```text
Redis queue
    |
Go spider
    |-- crawl policy, robots, request gate, securefetch --> public web
    |
    `-- bounded Unix-socket protocol --> networkless render worker
                                           |
                                       Chromium
                                           |
                                  paused resource intents
                                           |
                                  Go resource broker
                                           `--> existing controls
```

The render worker must have no network namespace, Redis credentials, database
credentials, host ports, or data-store mounts. A shared, project-scoped Unix
socket is its only communication path. The Go broker treats every worker
message as untrusted and binds it to the active page job; the worker cannot
choose its crawl group, depth, policy, or budget.

The implemented worker is `services/render-worker`. It launches a fresh
sandboxed Chromium process for each job, communicates only through the shared
Unix socket, and is serialized by the spider to one active render. Compose runs
it as `65534:65534` with a read-only root, all capabilities dropped,
`no-new-privileges`, bounded tmpfs mounts, and `network_mode: none`. Chromium's
unprivileged namespace sandbox remains enabled through the checked-in
Playwright-derived seccomp profile; the worker does not pass `--no-sandbox`.

## Policy Contract

`contracts/render-policy-v1.schema.json` defines a separate, fail-closed
contract. Page and resource rules use exact hosts plus either exact paths or
non-root path prefixes; subdomain-wide or path-wide authorization is not
representable. Resource types are attached to each resource rule, and V1 can
authorize only scripts and stylesheets.
`services/spider/config/render-policy-v1.disabled.json` is the only
current configuration and contains no rules. The implemented loader rejects
unknown fields, duplicate members, invalid UTF-8, trailing JSON, duplicate rule
IDs, and overlapping host rules in the same way as crawl-policy V1.

The render policy can narrow crawl policy but never expand it. A resource host
listed in render policy is not crawlable unless the originating crawl group and
resource broker also authorize it. The V1 constants require GET-only requests,
robots checks for resources, and denial of cookies, service workers,
WebSockets, WebRTC, downloads, popups, secondary documents, and script-driven
navigation.

## Delivery Stages

### Stage 0: contract and fixtures

Implemented:

- Strict render-policy loader and matcher.
- Renderer interface in `CrawlerConfig`, disabled by default.
- Versioned, length-delimited Unix-socket request/response protocol.
- Static-versus-rendered provenance fields without changing static output.
- `--validate-baseline-policy` rejects any enabled render rule.

### Stage 1: networkless inline rendering

Implemented:

- Fetch the document through the existing `FetchAuthorized` path.
- Supply the approved document bytes and effective URL to a fresh browser
  profile in the networkless worker.
- Abort every browser resource request.
- Enforce a response-level `default-src 'none'` CSP, reject child frames,
  `data:`/`blob:` code, dynamic imports, and shadow DOM, and count the actual
  frozen DOM, including template contents, in a browser-protocol isolated world
  with pristine native accessors.
- Reject `javascript:` URL execution and other scripts without approved
  document or generated-inline provenance using browser-protocol events.
- Execute inline JavaScript only, wait for the bounded settle interval, disable
  further script execution before inspection and serialization, and keep it
  disabled through browser teardown.
- Reject oversized, invalid, timed-out, or navigation-attempting renders.

Stage 1 proves process and protocol isolation. It will not render applications
whose scripts are external.

### Stage 2: brokered scripts and stylesheets

Stage 2a is implemented:

- Pause every supported resource before browser network I/O and serialize each
  intent through IPC V2. At most one intent is outstanding.
- Bound browser admission before queueing broker work. Denied intents and
  successful replies use Go-authoritative request and byte counters.
- Permit only canonical HTTPS GET scripts and stylesheets from exact resource
  hosts and explicit exact paths or non-root path prefixes. Query strings,
  fragments, user information, non-default ports, and redirects are denied.
- Bind the broker to the fetched page's effective URL, matched render rule,
  original crawl depth and group, and existing request gate.
- Reapply crawl policy, render policy, robots, group/global request capacity,
  DNS pinning, address denial, TLS, direct status/body limits, aggregate limits,
  strict JavaScript/CSS MIME, and UTF-8 validation before fulfillment.
- Return only normalized `Content-Type`, `Cache-Control: no-store`, and
  `X-Content-Type-Options: nosniff`; no origin cookies, authentication, proxy,
  download, cache, or alternate-service headers reach Chromium.
- Require approved scripts and stylesheets to load, parse, and execute or apply
  successfully. SRI, syntax, runtime, module, CSS import, or provenance failures
  reject the complete render.
- Use a neutral `resource_denied` reply and exact terminal frame on policy
  denial. Early, stale, duplicate, partial, or trailing protocol data is fatal.
- Abort and publish nothing on any policy, capacity, protocol, or render error.
  Resource capacity exhaustion requeues the original URL at its original score
  and depth.

### Stage 3: reviewed data requests

- Extend the contract in a reviewed V2 before adding same-origin GET `fetch`
  support for exact approved paths. Render-policy V1 cannot authorize data
  requests.
- Keep workers, service workers, beacon requests, state-changing methods,
  images, fonts, media, and third-party APIs denied.
- Run in shadow mode and compare response versus render output before allowing
  rendered pages into the indexer.
- Enable one exact host/path at a time with a newly reviewed policy digest.

## Hard Limits

The schema sets absolute ceilings. An enabled rule may only choose lower values.

| Limit | V1 ceiling |
|---|---:|
| Render wall time | 30 seconds |
| Settle interval | 5 seconds |
| Resource intents | 64 |
| Aggregate resource bytes | 32 MiB |
| One resource body | 5 MiB |
| Rendered DOM | 5 MiB |
| DOM nodes | 100,000 |
| Schema resource redirect ceiling | 3 |
| Implemented Stage 2a resource redirects | 0 |
| Retained console output | 64 KiB |

The operator-level outbound budget remains authoritative. If a crawl group or
batch has fewer available attempts than a render rule permits, the lower
remaining budget wins and the page is requeued without partial publication.

## Integration Map

- `services/spider/cmd/spider/main.go`: loads render policy, constructs the
  worker client, rejects unsupported rule shapes, and forbids enabled rules in
  the static baseline.
- `services/spider/internal/crawler/crawler.go`: owns renderer and provenance
  interfaces plus serialized render concurrency.
- `services/spider/internal/crawler/crawl.go`: renders after the secure document
  fetch and before link extraction or page persistence.
- `services/spider/internal/crawler/resource_broker.go`: binds every brokered
  resource to the page rule, original depth/group, robots policy, and existing
  request gate.
- `services/spider/internal/crawler/request_gate.go`: applies the claimed job's
  group and global request budget through rendering.
- `services/spider/internal/securefetch/`: provides the direct, bounded broker
  response without adding a second HTTP client.
- `services/spider/internal/robotsguard/`: applies robots to brokered resources
  and rejects explicit, padded, or prolog-prefixed HTML challenge documents as
  robots policies.
- `services/indexer/utils/utils.py`: has a separately tested visible-text
  extraction path for rendered content that does not require `<p>` tags.
- `services/indexer/data/mongo_client.py`: persists the original response and
  rendered DOM in the dedicated `page_artifacts` collection before Redis
  cleanup.
- `services/render-worker/`: contains the Chromium worker, protocol tests,
  sandbox profile, and hermetic browser smoke test.
- `scripts/docker/v1-baseline.compose.yml`: contains an opt-in, networkless,
  non-root, read-only `render` profile; that profile remains off in baseline runs.

## Required Tests

Implemented for Stages 0 through 2a:

- Strict policy and IPC parsing, duplicate and unknown fields, stale job IDs,
  oversized messages, exact host/path matching, and output limits.
- Synthetic inline rendering, external-resource denial, Fetch API denial,
  `data:`/`blob:` and child-frame denial, `javascript:` URL and forged
  `sourceURL` denial, tamper-resistant DOM limits, rendered link extraction,
  digest-bound artifact provenance, and no publication after a render error.
- Container evidence for no network namespace, non-root execution, dropped
  capabilities, read-only root, bounded tmpfs, retained Chromium namespace
  sandbox, and no data-store credentials.
- Exact IPC V2 success and neutral denial over a real Unix socket between Go and
  networkless Chromium, including brokered script and stylesheet DOM effects.
- Resource render/crawl-policy mismatch, group mismatch, robots denial,
  redirects, DNS/address denial, MIME ambiguity, invalid UTF-8, per-body,
  aggregate, request-count, and capacity-exhaustion coverage.
- Popup, service-worker, Cookie Store, WebSocket, SRI, syntax/runtime error,
  delayed async script, CSS, request-storm, console, and protocol-phase denial.
- Exact requeue of the original candidate after global or group capacity
  exhaustion, with no page, link, image, alias, or visited-state publication.

Regression and rollout gates for Stage 3 or any public activation:

- Existing DNS rebinding, mixed public/private answer, redirect, HTTPS
  downgrade, non-default port, ambiguous path, and private-address defenses must
  remain blocked before browser fulfillment.
- Expanded synthetic pages attempt any newly supported browser or data API; all
  unsupported operations must remain fail closed.
- Repeat container evidence for the promoted image to prove the worker has no
  network route, data-store secret, host port, writable root, or exposed
  debugging endpoint and retains the Chromium sandbox.
- Every fulfilled resource must continue to consume exactly one existing gate
  slot; capacity exhaustion must requeue the original URL at the same score and
  depth.
- Render failures must continue to publish no page, links, images, or aliases.
- Static crawling must remain behaviorally unchanged when no render rule
  matches.
- Shadow comparisons record static and rendered hashes, text/link counts,
  resource count, aggregate bytes, timing, and the terminal render state.

## Rollout Gate

No implementation may crawl a public JavaScript site until all stages required
for that site's resources pass hermetic tests, the renderer image is pinned and
scanned, a new render policy is reviewed, its digest is recorded, and the
site's robots and usage terms authorize the crawl. Rendering is a content
capability, not an authorization bypass.
