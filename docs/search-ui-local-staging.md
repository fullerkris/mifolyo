# Search UI local staging

This runs the real Query Engine, forum API, MongoDB, PostgreSQL, Redis and Caddy
in the dedicated `mifolyo-search-ui-staging` Compose project. The index and forum
contain controlled, explicitly labelled seed data. Requests use the application
controllers and databases; the browser-fixture router and HTTP mocks are absent.

- Search UI: <http://127.0.0.1:18081>
- Forum: <http://127.0.0.1:18082>
- Search terms: **staging**, **search**, **documentation**; use exact search for
  **ux05 café C++** to review highlighting and metadata edge cases.
- Data: 25 baseline documentation URLs plus four synthetic `example.test` UX-05
  entries. The first baseline result has 23 discussions and one persisted reply
  per discussion. Metadata and conversations are synthetic.

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
posting. Result-presentation checks cover literal/Unicode highlighting, late
matches in indexed snippets, missing/malformed metadata and expandable titles
and URLs in both themes. Recovery tests briefly stop/restart **this project's**
forum and MongoDB services and verify error/retry behavior. They restore the services in
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

## Result highlighting and metadata

- Title and excerpt matches use case-insensitive literal terms, with Unicode word
  boundaries (CJK terms can match without spaces). Exact search retains literal
  punctuation such as `C++`; spelling-corrected results use the corrected query.
- Metadata stays plain text. Blade escapes every segment before wrapping matches
  in `<mark>`; indexed markup and query punctuation cannot become executable HTML
  or regular-expression syntax. JSON search responses retain the raw index data.
- Excerpts use a roughly 240-character window around a query match rather than
  always taking the page's introduction. A description with more distinct matching
  terms takes precedence over the summary; ties prefer the summary. Whitespace is
  normalized, absent/malformed fields fall back cleanly, and clipping is marked.
- Cards show the source domain and a compact path/query/fragment. Long titles and
  URLs have keyboard/touch-operable native **Full title / Full URL** disclosure.
  Source/discussion links retain the exact indexed HTTP(S) URL. Missing titles use
  the domain or **Untitled page**; missing excerpts have an explicit fallback.
- Highlighting, excerpts and metadata disclosure render on the server and work
  without JavaScript. Both themes have dedicated highlight colors; long text wraps
  within narrow cards.

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

## UX-05 validation — 2026-10-15

- Safe title/excerpt highlighting, query-focused snippet selection, compact URL
  paths and expandable long metadata are implemented on `feature/search-ui`.
  The displayed query preserves literal punctuation such as `C++`.
- Query Engine PHP/MongoDB: **75 passed, 486 assertions**. Coverage includes
  literal/Unicode/CJK matches, late-match windows, relevant descriptions,
  malformed/missing metadata, escaped markup, exact source URL identity and
  unmodified JSON metadata.
- Browser fixtures: **34 passed, 2 expected mobile skips** for desktop-only panel
  race checks. The final presentation checks passed again on desktop/mobile
  (**6 passes**), including keyboard disclosure and JavaScript-disabled 320px and
  1440px contexts after the displayed-query refinement.
- Real-service staging: **20 passed, 0 skipped** (1.5 minutes), including search,
  discussions/counts, persisted account/thread/reply journeys, and both real
  outage recoveries. Final corrected-query/presentation checks passed on the
  rebuilt image (**4 passes**, desktop/mobile).
- Reviewed cards and expanded metadata at **320/768/1440px in both themes**;
  no horizontal overflow or browser JavaScript errors. Screenshots disable
  transitions to capture settled theme colors. Final captures are under
  `services/query-engine/test-results/staging-ux05-final/`.
- Four isolated UX-05 entries supplement the 25-result baseline; baseline search
  pagination and discussion counts passed unchanged. Search `/search/exact` for
  `ux05 café C++` to reproduce the metadata cases.
- Production asset/application builds, focused PHP formatting (seven files),
  and whitespace checks passed. Both readiness endpoints return HTTP 200 after
  validation; all six long-running staging services are healthy.
- PR #27 remains subject to the owner's merge hold. UX-06 secondary-view styling
  is the next Search UI refinement.

Validated local image IDs (not registry digests):

```text
query: sha256:c0c09679aca7738499b10eb7435b5a57c887aabf80f625224fcd82cfeaada7c7
forum: sha256:2b826e97a30a1bcd121a8a302a7487c851f1cda6442d1d4d16cd5a2366c08c74
```

## UX-05 dependency-audit follow-up — 2026-10-15

Hosted Required Checks at `79f41d0` passed build and smoke, but its Composer
audit failed. Local lockfile audits reproduced HttpFoundation CVE-2026-108082
in both services and development-only Symfony YAML CVE-2026-109413 in Query
Engine. The missing C0 test artifact followed the early audit failure.

Applied compatible patch releases in the existing dependency lines:

- Query Engine: `symfony/http-foundation` 7.4.17 → **7.4.19** and
  `symfony/yaml` 7.4.17 → **7.4.18**.
- Forum: `symfony/http-foundation` 8.1.5 → **8.1.7**.

Both services pass `composer validate --strict` and `composer audit --locked`
(including development dependencies). Post-patch service tests pass: **75 Query
Engine tests / 486 assertions** with MongoDB and **135 forum tests / 596
assertions** with isolated SQLite. Browser fixtures pass **34 checks**, with the
same two expected mobile skips. Both application images rebuilt successfully.

The full post-patch real-service staging suite passes **20 checks, 0 skipped**
(1.5 minutes), including both outage recoveries. Both readiness endpoints return
HTTP 200 and all six long-running services are healthy after the run. Updated
screenshots are retained under `test-results/staging-ux05-patched/` in Query
Engine; the previous UX-05 visual-review record remains above.

Local image IDs for the dependency patches (not registry digests):

```text
query: sha256:4ef6bc7577bde5bb0fb30c495af02a2c3383c259f500300395d0a8236f844e67
forum: sha256:bd4ce860a792bc1e00046559cf4569e280a7bbd7f3686d47a7549a82e5fbd261
```
