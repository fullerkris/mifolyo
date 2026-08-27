# Render Worker

The render worker executes policy-approved JavaScript in Headless Chromium and
returns the frozen DOM to the Go spider over a Unix socket. The worker remains a
separate, networkless service with no host port, crawler credentials, or policy
authority. External bytes can enter Chromium only through the spider's resource
broker.

## Implemented Scope

IPC V2 supports both `inline_only` and `brokered` render jobs:

- The spider fetches and authorizes the original HTML before rendering.
- One fresh Chromium browser and context are created per render job, with the
  browser cache and service workers disabled.
- The initial document is fulfilled from supplied HTML under a response-level
  `default-src 'none'` Content Security Policy.
- Inline jobs permit inline scripts and styles only. Their resource host lists
  and all resource limits must be zero.
- Brokered jobs may permit exact HTTPS script and stylesheet hosts. Every
  non-navigation GET script or stylesheet is serialized through the Unix-socket
  broker and fulfilled from returned bytes. The worker never continues a
  browser request to the network.
- Resource responses expose only normalized `Content-Type`,
  `Cache-Control: no-store`, and `X-Content-Type-Options: nosniff` headers.
- Fetch, XHR, WebSocket, EventSource, WebRTC, workers, beacons, popups,
  downloads, cookies, secondary documents, unsupported resource types and
  methods, and JavaScript navigation remain denied and fail the whole render.
- `data:` and `blob:` code, unapproved script provenance, shadow DOM, and
  unapproved stylesheets also fail the render.
- Render time, settle time, source and output bytes, DOM nodes, console output,
  resource requests, per-resource bytes, aggregate resource bytes, protocol
  frame size, and concurrency are bounded.
- Broker operations are serialized, drained before and after scripts are
  disabled, and must be empty before success. CDP remains attached and script
  execution remains disabled until Chromium closes.

After scripts are disabled, DOM nodes are counted in a CDP-created isolated
world with pristine native accessors. Traversal covers template contents and
reads the actual frozen tree rather than reparsing serialized HTML or trusting
page-overridable APIs.

## Sandbox

The container runs as `65534:65534`, drops all Linux capabilities, uses a
read-only root filesystem, and has only bounded `/tmp` and `/dev/shm` tmpfs
mounts. Compose sets `network_mode: none` and `no-new-privileges:true`.

Chromium runs with Playwright's `chromiumSandbox: true`. The worker passes
`--disable-setuid-sandbox` so Chromium uses its unprivileged user-namespace
sandbox; it never passes `--no-sandbox`. `seccomp_profile.json` is derived from
Playwright's published Docker profile and permits the namespace syscalls needed
by that sandbox while retaining a default-deny syscall policy. `chroot` is also
allowed because Chromium requires it after creating the unprivileged namespace.

The Unix socket is mode `0660` in a project-scoped volume. The spider mounts
that volume read-only and both processes use the same unprivileged UID/GID.

## IPC V2 Protocol

Every message is one UTF-8 JSON object framed by a four-byte unsigned
big-endian payload length. Frames are capped at 32 MiB. One connection carries:

1. One `render_start` frame from the spider.
2. Zero or more serialized `resource_intent` / `resource_reply` exchanges.
3. One terminal `render_result` frame from the worker.

Frame readers preserve fragmentation but reject early, repeated, unsolicited,
partial, or trailing bytes outside the exact protocol phase. Only one resource
intent may be outstanding, and intent IDs are consecutive integers starting at
one.

`render_start` contains exactly `version`, `kind`, `job_id`, `mode`,
`effective_url`, `html`, `resource_hosts`, and `limits`. Resource host arrays
must be sorted, unique, lowercase public DNS names. All nine V2 limit fields are
required, and inline/brokered invariants are checked before Chromium starts.

`resource_intent` contains exactly `version`, `kind`, `job_id`, `intent_id`,
`url`, `method`, and `resource_type`. `resource_reply` contains exactly
`version`, `kind`, `job_id`, `intent_id`, `status`, `status_code`,
`content_type`, `body_base64`, `body_bytes`, and `error_code`. Successful replies
require status 200, non-empty valid UTF-8 bytes, canonical padded base64, a
valid bounded content type, and matching body and aggregate counts. The only
error reply is the bodyless neutral `resource_denied` form.

`render_result` contains exactly `version`, `kind`, `job_id`, `status`, `html`,
`dom_nodes`, `console_bytes`, `resource_requests`, `resource_bytes`, and
`error_code`. Duplicate and unknown JSON members, invalid UTF-8, stale job or
intent IDs, malformed base64, and out-of-bound counters are rejected.

## Tests

Run protocol tests:

```bash
npm ci --ignore-scripts
npm test
```

Build and run real Chromium with no network and the production sandbox profile:

```bash
docker build -t mifolyo-render-worker:test .
docker run --rm \
  --network none \
  --read-only \
  --user 65534:65534 \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --security-opt seccomp=seccomp_profile.json \
  --tmpfs /tmp:rw,noexec,nosuid,size=256m \
  --tmpfs /dev/shm:rw,nosuid,size=256m \
  mifolyo-render-worker:test node smoke.mjs
```

The smoke test preserves all Stage 1 inline allow/deny cases and adds hermetic
broker fixtures for approved and denied scripts/stylesheets, SRI and execution
failures, delayed async execution, service workers, Cookie Store, request
storms, image denial, and Fetch denial. It verifies rendered mutations,
resource counts, and byte counts without contacting a public site. CI also runs
real Go-to-worker Unix-socket cases for inline rendering, brokered success, and
neutral broker denial.

## Activation Boundary

Stage 2a implements the worker transport and the page-bound crawler resource
broker. A public brokered crawl still requires an enabled exact-host/path
policy, recorded policy and promoted image digests, site authorization,
deployment-host sandbox validation, and a fresh crawl approval. Enabling the
Compose `render` profile alone does not authorize any page or resource.
