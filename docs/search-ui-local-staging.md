# Search UI local staging

This runs the real Query Engine, forum API, MongoDB, PostgreSQL, Redis and Caddy
in the dedicated `mifolyo-search-ui-staging` Compose project. The index and forum
contain controlled, explicitly labelled seed data. Requests use the application
controllers and databases; the browser-fixture router and HTTP mocks are absent.

- Search UI: <http://127.0.0.1:18081>
- Forum: <http://127.0.0.1:18082>
- Search terms: **staging**, **search**, **documentation**
- Data: 25 indexed documentation URLs; the first result has 23 discussions and
  one persisted reply per discussion. Metadata and conversations are synthetic.

## Start and seed

Run from the repository root with Docker Desktop running:

```sh
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml build query-engine forum-engine
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml up -d --wait --wait-timeout 180
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml exec -T query-engine php /staging/seed.php query
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml exec -T forum-engine php /staging/seed.php forum
```

The application images build from this checkout, including compiled frontend
assets. The forum applies its migrations to the project-owned PostgreSQL
database at startup. Query sessions use files; query cache/telemetry use Redis.
Forum sessions/cache use PostgreSQL. The seed is repeatable and preserves
discussions created during manual or browser validation.
MongoDB server selection rescans for up to two seconds so every PHP worker can
recover after an index restart, even when its persistent topology recorded an outage.

Only Caddy publishes ports, both on loopback. The data and application networks
are internal, and all database/asset volumes are project-owned. This definition
contains only the Search UI and forum stack. Its known local-only application
keys and database credentials are for this loopback environment.

## Validate

```sh
curl --fail http://127.0.0.1:18081/api/health/ready
curl --fail http://127.0.0.1:18082/api/health/ready
```

From `services/query-engine`, after `npm ci` and `npx playwright install chromium`:

```sh
npm run test:staging
```

The suite checks real search ranking/pagination, dictionary correction, query
encoding/history, source-linked discussions and their pagination, mobile
navigation, theme persistence, and login plus thread creation persisted through
the proxy. It also checks account registration/sign-out, full thread pages,
nested replies, draft recovery after token rotation, CSRF rejection, and
posting without JavaScript. Exact-search return checks cover the query/mode/page,
selected source, scroll, desktop panel, and live counts after sign-in/thread/reply
posting. Recovery tests briefly stop/restart **this project's** forum and
MongoDB services and verify error/retry behavior. They restore the services in
`finally` blocks. Run the suite when the local staging UI can be interrupted.

Tests verify the staging response marker before operating and use fixed
loopback endpoints. Write tests create source-specific threads and registration
tests create uniquely named staging accounts. The exact-search return check adds
a discussion to an indexed source on page two; its count assertion uses the
existing count so reruns are safe. Screenshots and failure traces go to
`services/query-engine/test-results/staging/` (ignored by Git and image builds).

For manual posting, use **Sign in** on the Search UI with
`reader@staging.mifolyo.test` / `local-staging-only-password`, or choose **Create an
account**. Select **Start a thread**, then **Open your conversation** to read and
reply. Discussion cards also link to full conversations. Browser tests sign out
after completing their account journeys.

## Account and conversation behavior

- `/threads/{id}` renders the complete source-linked post and its nested comments.
  Reply links select a parent; **Reply to the thread instead** clears that target.
  Locked threads and locked/removed comments cannot receive replies. Removed
  comments retain their visible children and show a removal placeholder.
- `/sign-in` and `/register` return readers to their selected source, search, or
  thread. Return destinations are restricted to local reading routes. `/sign-out`
  is a CSRF-protected POST and clears the browser session and saved drafts.
- The Query Engine holds the forum bearer token encrypted in its server session.
  Browser writes use the session cookie and CSRF token through `/community/threads`
  and `/threads/{id}/comments`. No manual API token is needed in the UI.
- The existing forum issues one active token per account. A new login rotates it;
  the previous session must sign in again. Submitted drafts survive that flow.
  An uncertain write keeps its draft and asks the reader to check before retrying.
- Authentication, thread creation, and replies support ordinary HTML form posts.
  The source discussion list still uses JavaScript. The forum enforces private
  community membership on source lists and thread/comment reads, and public read
  endpoints reject expired forum tokens.
- Repository CI runs PHP/MongoDB tests and desktop/mobile browser-fixture checks.
  `npm run test:staging` separately validates the real local services.

## Search return navigation and result counts

- Discussion and thread pages provide **Back to results** when opened from a
  search. The validated `from` URL preserves the original query, exact/normal
  search mode, and results page through thread/reply links and authentication.
  Direct-entry discussion/thread links retain their usual source/Home fallback.
- Tab-local history/session storage restores the selected result, scroll position,
  and desktop discussion panel (including its sort and page). The last ten search
  journeys are retained, with a two-hour restoration window. Browser Back/Forward
  and explicit return links work; closing a restored panel returns keyboard focus
  to the source result. Without storage or JavaScript, the return link still keeps
  the query/mode/page.
- Result cards load conversation counts through one CSRF-protected
  `POST /community/thread-counts` request for up to 20 source URLs. The Query Engine
  calls the forum's `POST /api/threads/counts` using its server-held session token.
  Request: `{"urls":["https://example.com/source"]}`; response:
  `{"data":[{"url":"https://example.com/source","count":0}]}`.
- The forum uses the same canonical source identity and private-membership/removed
  content filters as its source thread list, in one grouped database query.
  Unsupported identities return `null`; failures remain unavailable rather than
  becoming zero. Responses are private and non-cacheable.
- Badges distinguish loading (`…`), confirmed zero (`0`), and unavailable (`—`).
  **Retry counts** recovers independently of search results. Counts refresh on
  return navigation and after loading/creating a source conversation; a late
  batch response cannot overwrite a fresher source total.

## Inspect and stop

```sh
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml ps
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml logs --tail=80 query-engine forum-engine caddy
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml stop
```

`stop` preserves staging data. Use the `up` command above to resume. Rebuild the
images after source changes and recreate the asset-copy service when deploying
fresh assets:

```sh
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml build query-engine forum-engine
docker compose -p mifolyo-search-ui-staging -f scripts/docker/search-ui-staging.compose.yml up -d --force-recreate query-assets query-engine forum-engine caddy
```

## Validation record — 2026-10-14

- Application UI baseline: `6d49f5a` on `feature/search-ui`.
- Fresh builds completed for both application images. Query runtime: PHP
  `8.4.26`, MongoDB extension `1.21.0`.
- `npm run test:staging`: **10 passed, 0 skipped** (29.9 seconds).
  Desktop/mobile search and discussions, encoded queries/history, spelling
  opt-out, empty states, source-isolated persisted writes, and both actual
  service outage/recovery cases passed.
- Query image `npm audit --audit-level=low`: **0 vulnerabilities**.
- Post-validation readiness: HTTP **200** from both services; all six
  long-running containers healthy. The one-shot asset copier exited successfully.
- Screenshots reviewed for desktop results, mobile discussions, and both
  unavailable-service states; retained under the ignored staging test output.
- This is local integration validation with controlled data. No merge or remote
  deployment was performed.

Validated local image IDs (not registry digests):

```text
query: sha256:fa6cd911c14ab4dfcc8464f5e9ce971a63626af3e39042130be230689c9e7953
forum: sha256:ee4c1a18cba2e363d8f6de079bb03cf3031ecfe3637b463031c01ace348a7e9f
```

## Thread/account validation — 2026-10-14

- Draft PR: <https://github.com/fullerkris/mifolyo/pull/27>. Merge hold remains active.
- Query Engine PHP/MongoDB suite: **61 passed, 371 assertions**.
- Forum suite (isolated SQLite, including shared URL fixtures): **133 passed,
  573 assertions**. Private membership, expired tokens, removed content, and
  locked/cross-thread/depth-limited replies are covered.
- Browser-fixture suite: **21 passed**, one expected mobile skip for the
  desktop-only panel race test. These checks are now included in repository CI.
- Real-service staging suite: **14 passed, 0 skipped** (40.6 seconds), including
  registration, sign-in/out, persisted threads and nested replies, token-rotation
  draft recovery, CSRF rejection, no-JavaScript writes, and both outage recoveries.
- The first outage run exposed stale per-worker MongoDB server selection after a
  restart. Setting `serverSelectionTryOnce=false` with the existing two-second
  timeout fixed recovery; the complete staging suite then passed.
- Reviewed thread and registration screenshots; checked thread layouts at 320,
  768, and 1440 pixels in both themes and the account form at 320 pixels. No
  horizontal overflow or JavaScript errors were observed.
- Both application images rebuilt successfully; query npm audit reported zero
  vulnerabilities. Focused PHP formatting and Git whitespace checks passed.
- Both readiness endpoints returned HTTP 200 after testing; all six long-running
  staging containers were healthy.

Validated local image IDs for the thread/account implementation:

```text
query: sha256:f6e23869dc841b4b711bc15d34d1c0e7a489e154b4af246eb6b3749faf58f7a9
forum: sha256:c8e5c52875a6592ee11341f160d4d6d36e7f92907c06350e97380833cced17e5
```

## UX-03 / UX-04 validation — 2026-10-14

- Exact-search return navigation and batched result-card conversation counts are
  implemented on `feature/search-ui`. PR #27 is open and ready for review; the
  owner's merge hold remains active.
- Query Engine PHP/MongoDB: **66 passed, 426 assertions**. This includes bounded
  local search destinations, thread/reply/reauthentication return context, count
  response validation, CSRF rejection, and throttled sign-in retry context.
- Forum: **135 passed, 596 assertions**. Counts share the thread list's visibility
  filter and canonical URL/version identity; tests cover private membership,
  removed content, hash collisions, unsupported identities, batch bounds, expired
  tokens, and a single grouped count query for 20 URLs.
- Browser fixtures: **28 passed, 2 expected mobile skips** for desktop-only panel
  race checks. Exact query/mode/page, scroll, source, panel sort/page, browser
  Back/Forward, explicit return, restricted storage, zero/unavailable/retry counts,
  and late-batch versus fresh-source totals are covered.
- Real-service staging: **18 passed, 0 skipped** (1.6 minutes). Verified live
  counts before/after persisted threads, exact-results return after sign-in and
  replies, reauthentication drafts, no-JavaScript return links, and both service
  outage recoveries. Forum outages leave search usable and counts unavailable;
  independent count retry recovers after the forum restarts.
- The expanded staging suite reached the real sign-in rate limit and exposed a
  retry link that dropped the destination. The fix and regression check pass;
  staging now honors the server's `Retry-After` before submitting again.
- Browser fixtures now retain file sessions for real CSRF validation on the
  count route. The staging seed's MongoDB guard now matches the recovery-enabled
  URI; the query seed ran successfully and retained all 25 sources.
- Reviewed badges and return links at **320/768/1440px in both themes**: no
  horizontal overflow or browser JavaScript errors. Final return-link spacing,
  touch targets and desktop panel restoration passed focused walkthroughs after
  the full suite. Screenshots: `test-results/staging/ux03-ux04-layouts/`.
- Both application images built successfully; focused PHP formatting and Git
  whitespace checks passed. Both readiness endpoints returned HTTP 200 and all
  six long-running staging containers were healthy after recovery testing.

Validated local image IDs (not registry digests):

```text
query: sha256:914f6be467c7cef91496ef598115199c0e1949312635bd7d5f2e1241b62ad97b
forum: sha256:2b826e97a30a1bcd121a8a302a7487c851f1cda6442d1d4d16cd5a2366c08c74
```
