# Search UI local staging

This runs the real Query Engine, forum API, MongoDB, PostgreSQL, Redis, Caddy and Mailpit
in the dedicated `mifolyo-search-ui-staging` Compose project. The index and forum
contain controlled, explicitly labelled seed data. Requests use the application
controllers and databases; the browser-fixture router and HTTP mocks are absent.

- Search UI: <http://127.0.0.1:18081>
- Forum: <http://127.0.0.1:18082>
- Local reset-email inbox: <http://127.0.0.1:18085> (Mailpit; no external delivery)
- Search terms: **staging**, **search**, **documentation**; use exact search for
  **ux05 café C++** to review highlighting and metadata edge cases.
- Image search: <http://127.0.0.1:18081/images?q=ux06> (also **staging** or
  **gallery**). Choose **Page links** on the first `staging` web result to explore
  25 outgoing and 25 incoming connections.
- Data: 25 baseline documentation URLs plus four synthetic `example.test` UX-05
  entries. The first baseline result has 23 discussions and one persisted reply
  per discussion. There are also 25 page-image associations using local synthetic
  artwork in different aspect ratios, including one deliberately missing preview.
  Metadata, connections and conversations are synthetic.

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

Only Caddy and the Mailpit inbox publish ports, all on loopback. The data and application networks
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
and URLs in both themes. Image/link discovery checks cover pagination, broken
previews, map rendering, no-JavaScript browsing and exact-search return navigation.
Voting checks cover persisted thread/nested-comment scores, selected votes,
change/removal, duplicate writes, token rotation, keyboard use, both themes,
no-JavaScript forms and explicit retry after a real forum outage. Account checks
exercise real SMTP reset links, single-use token consumption, password-confirmed
deletion, revoked credentials, retained discussions/other-reader votes, and
concurrent PostgreSQL reset/deletion/voting. The account forms run with and without
JavaScript on desktop and mobile. Onboarding checks cover welcome dismissal,
first search, Discuss and first-vote completion, failure/retry, account switching,
cross-session persistence, both themes, and ordinary HTML forms. Lifecycle refresh
events are delivered explicitly in headless Chromium, where all tabs stay visible.
Recovery tests briefly stop/restart **this project's**
forum, MongoDB and mail services and verify error/retry behavior. They restore the services in
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
  thread or account settings. Return destinations are restricted to local reading
  routes and `/account`. `/sign-out`
  is a CSRF-protected POST and clears the browser session and saved drafts.
- The Query Engine holds the forum bearer token encrypted in its server session.
  Browser writes use the session cookie and CSRF token through `/community/threads`
  and `/threads/{id}/comments` or `/threads/{id}/votes`. No manual API token is
  needed in the UI.
- The existing forum issues one active token per account. A new login rotates it;
  the previous session must sign in again. Submitted drafts survive that flow.
  An uncertain write keeps its draft and asks the reader to check before retrying.
- Authentication, thread creation, and replies support ordinary HTML form posts.
  The source discussion list still uses JavaScript. The forum enforces private
  community membership on source lists and thread/comment reads, and public read
  endpoints reject expired forum tokens.
- Repository CI runs PHP/MongoDB tests and desktop/mobile browser-fixture checks.
  `npm run test:staging` separately validates the real local services.

## Account recovery, deletion and visible levels

- **Forgot your password?** on sign-in opens `/forgot-password`. The generic
  result preserves the intended search/thread/reply destination. Open the email
  in the local Mailpit inbox at port 18085; its link lands on the Search UI and
  redirects to a clean `/reset-password` URL. Reset tokens stay encrypted in the
  server session rather than appearing in form fields or subsequent links.
- Reset requires matching passwords, consumes the email token once, revokes old
  credentials and returns to sign-in. A stale reset tab cannot submit another
  tab's flow. Invalid/expired links, throttling and service/mail outages have
  recovery messages; passwords are never flashed back into inputs.
- The signed-in username opens **Your account**, showing the current email and
  existing forum level. Deletion requires the current password and an explicit
  checkbox. It clears identity, sessions and votes while keeping posts/comments
  under **Community member**; affected vote totals and membership counts are
  corrected transactionally. Other readers' votes remain intact.
- Thread cards, full threads and nested comments show **Level N** from the API.
  Missing/removed authors have no fabricated level. This increment implements
  presentation, not level calculation, eligibility or vote weighting.
- All forms support ordinary HTML submission, both themes and narrow screens.
  Account/recovery pages are private/non-cacheable and suppress referrers.
- Deployments must set the forum's public `SEARCH_UI_URL` and a working mail
  transport. Staging uses the pinned Mailpit image, `MAIL_MAILER=smtp` and
  `SEARCH_UI_URL=http://127.0.0.1:18081`; `SEARCH_UI_STAGING_MAIL_PORT` can change
  the inbox's loopback port. The forum API contract is in its README.

## Lightweight onboarding

Create a new account to see the welcome banner; the seeded reader predates
onboarding and stays unenrolled. **Got it** or a successful nonempty HTML search
clears the welcome. The first usable web result carries an inline Discuss hint;
opening its desktop discussion list or a discussion/thread page completes it.
An eligible thread shows voting guidance until the first committed nonzero
post/comment vote. Discuss/voting guidance expires after seven days, and completed
steps stay complete across sign-out/sign-in. All state lives on the forum account.

See [the onboarding behavior and verification record](search-ui-onboarding.md).
The new migration runs automatically when this staging forum starts. The recovery
vote scenario also checks retryable welcome/vote guidance during a real forum outage.

## Thread and comment voting

- Full thread pages show upvote/downvote controls and net scores for the original
  post and each visible comment, including nested replies. The pressed state
  identifies your vote; selecting it again removes the vote. Removed comments
  show an unavailable message, and locked threads/comments have disabled controls.
- Signed-out readers can see scores and **Sign in to vote**. Authentication
  returns to the selected thread/comment anchor, reply target and validated
  web/image search origin. It does not submit a vote automatically.
- `POST /threads/{id}/votes` uses the encrypted session-held forum token and CSRF
  protection. It binds every target to the route's thread, validates the response
  identity/value/score, and returns only the display data needed by the browser.
- JavaScript adds a saving state, duplicate-submit protection, retained keyboard
  focus and live status feedback. Only confirmed scores/selections replace the
  displayed values, and an unfinished reply stays in place while voting.
- **Retry vote** resubmits the same explicit `1`, `-1`, or `0` after an uncertain
  response. No automatic write retry occurs. A 429 honors `Retry-After`; expired
  sessions offer sign-in, and access/removed/locked/expired-page responses offer
  a readable conversation reload. Ordinary HTML forms also set/change/remove
  votes and preserve the conversation, comment and search origin.
- The forum's post/comment reads include private viewer-specific vote state;
  nested-comment selections use one batched lookup. The vote transaction checks
  current access and locked/removed state under row locks before changing totals.
  The API contract is documented in `services/forum-engine/README.md`.

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

## Image search and Page links

- `/images?q=…` uses the shared header, Web/Images navigation, themes, responsive
  image cards and 20-result pagination. Source, full-image, discussion and Page
  links actions retain the indexed HTTP(S) destinations, including supported
  legacy scheme-less image records. Invalid URLs render without active links;
  broken previews leave source navigation available.
- Web results' **Related images** sidebar uses the same unavailable-preview
  fallback, with exact source links and single-column cards on narrow screens.
- Image ordering has a deterministic association-ID tie-breaker; images shared
  by different source pages retain their separate source associations. Plain-text
  metadata is normalized/escaped and long image titles have native disclosure.
- `/page-links?url=…` presents outgoing and incoming links with independent
  totals, direction selection and pagination. Duplicate/invalid edges are filtered;
  unindexed titles and missing link data have explicit fallbacks. Empty index data
  is not presented as proof that a source has no links on the web.
- The optional connection map is bounded to the current 20-link page plus up to
  20 links in the other direction. Cytoscape is bundled and loaded only on opening
  the map. It uses theme colors, directed edges, reciprocal-link labels, node
  selection, fit/zoom controls and a page-reload recovery action. Paginated lists
  remain available when JavaScript or the map download is unavailable.
- A validated `from` destination preserves exact web-search query/mode/page through
  image search, Page links, discussion/thread reading and sign-in. Image origins
  can retain one web-search origin; nested image return chains are discarded.
  The legacy `/api/search_images`, `/api/search_images_force` and
  `/api/page-connections` HTML routes continue to render the shared views.
- Validation, throttling and MongoDB failures have shared-layout recovery views.
  Retry keeps the image query/page or source/direction/page and search origin;
  HTTP status and `Retry-After` are preserved. JSON validation stays JSON.
  The new browser image/Page links routes have separate throttle buckets, so
  browsing one view cannot consume the other view's or web search's budget.
  Staging navigation honors the actual server backoff when a limit is reached.
- Staging artwork is served by Caddy from the read-only
  `scripts/fixtures/search-ui-media/` bind mount. Production images do not include
  these staging fixtures.

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

## UX-06 validation — 2026-10-15

- Shared image-search and Page links views are implemented on
  `feature/search-ui-ux06`, based on `main` at
  `142334acbd72134536c42b2b1c2a34256b1227a3`. GitHub reports that the owner merged
  PR #27 at 17:03:38 UTC on October 15. Its merged tree matched the previous
  `68f1440` feature head exactly. UX-06 is a separate review increment.
- Query Engine PHP/MongoDB: **85 passed, 646 assertions**. Coverage includes
  stable image association pagination, safe metadata/URLs, filtered/deduplicated
  incoming/outgoing links, bounded map enrichment, exact navigation context,
  validation/outage recovery and independent browser-view rate limits.
- Browser fixtures: **46 passed, 2 expected mobile skips** for the existing
  desktop-only panel races. The secondary-view checks cover both themes,
  pagination, broken previews, source/discussion/thread return, native map
  disclosure, zoom/fit, failed map download and reload, and no-JavaScript browsing.
- Real-service staging: **26 passed, 0 skipped** (1.7 minutes). This includes all
  existing account/thread/reply/count/return journeys, real image associations,
  outgoing/incoming pagination, graph rendering and both service outage cases.
  The MongoDB outage check now verifies query, source, direction, page and return
  preservation for all three discovery views before recovering against the index.
- Initial staging checks reproduced shared numeric-throttle buckets between web
  search and the new routes. Separate image/Page links buckets fix that collision;
  the staging navigation helper honors `Retry-After` and verifies retained retry
  parameters. A strict locator was also narrowed to the actual thread body.
- Production Query Engine image/asset builds, a zero-finding npm dependency audit,
  focused PHP formatting (12 files), and Git whitespace checks pass.
- Full-suite captures: `services/query-engine/test-results/staging-ux06-final/`
  and `services/query-engine/test-results/ux06-browser-final/`.
- Reviewed the final rebuilt views at **320/768/1440px in both themes**. No
  horizontal overflow or browser JavaScript errors were observed. Keyboard map
  opening, touch/mouse node selection and fit controls passed. The final image
  fallback refinement exposes the unavailable label to assistive technology only
  when the preview is absent/failed; loaded and failed cases passed at all three
  widths. Captures: `services/query-engine/test-results/staging-ux06-layouts/`.
- Both readiness endpoints returned HTTP 200 after the final walkthrough; all
  six long-running staging services are healthy. UX-06 remains unmerged for review.

Final local Query Engine image ID (not a registry digest):

```text
sha256:80e3e55fd0a6ceec87016d0f96f0dfb94abe9bc576b7963d7ec344e8723f3638
```

## COM-01 voting validation — 2026-10-15

- Thread and nested-comment voting is implemented on
  `feature/search-ui-com01-voting`, based on UX-06 head `37fa178`. UX-06 PR #28
  remains open; all 20 Unit Tests jobs and three Required Checks jobs passed at
  that exact head. The voting increment retains the owner's merge hold.
- Query Engine PHP/MongoDB: **95 passed, 727 assertions**. Forum API: **139 passed**
  with isolated SQLite. Coverage includes explicit set/change/removal and repeated
  writes, private membership, fresh authentication, viewer-specific reads, one
  batched nested-comment vote query, locked/removed/cross-thread targets, proxy
  credentials, CSRF, safe return anchors, malformed responses and error recovery.
- Browser fixtures: **60 passed, two expected mobile skips** for existing
  desktop-only panel races. New checks cover selected scores, nested independence,
  keyboard focus, unfinished replies, saving/duplicate prevention, a lost
  successful response followed by same-value retry, 429 backoff, malformed success,
  expired/forbidden/missing/locked states and no-JavaScript forms.
- Rebuilt real-service staging: **33 passed, 0 skipped** (2.7 minutes). Votes and
  counters persist in PostgreSQL through changes, removals, repeat submissions
  and reloads. Token rotation returns to the exact comment/reply/search origin
  without replaying a vote. Desktop/mobile HTML voting and a real forum-outage
  retry pass, alongside all prior discovery/account/reply/count/recovery journeys.
- The first expanded run passed all new voting checks but one existing index
  recovery check received a per-worker MongoDB connection timeout just after
  readiness recovered. The check now exercises the actual retry link within a
  bounded 15-second recovery window, verifying its exact destination; the final
  full run passes. This adds no automatic vote retries.
- Reviewed the rebuilt thread view at **320/768/1440px in both themes**. The
  original-post upvote and nested-comment downvote render selected, controls
  remain at least 44px, and keyboard voting retains focus. No horizontal overflow
  or browser JavaScript errors were observed. Review conversation: `/threads/94`.
- Evidence: `services/query-engine/test-results/com01-browser-final/`,
  `staging-com01-final/`, and `staging-com01-layouts/` (ignored by Git).
- Both application images and production frontend assets built successfully;
  npm audit reports zero vulnerabilities. Focused PHP formatting and whitespace
  checks pass. Both readiness endpoints return HTTP 200 and all six long-running
  staging services are healthy after recovery testing and the visual walkthrough.

Validated local image IDs (not registry digests):

```text
query: sha256:69bce813a4adfad77a3fdc966effaa86df9e697916b056dac666649faf1dc305
forum: sha256:c340b41de504141562b60f698b54f03d5ac5d445af9f28e05fa16dd3eba10dd7
```

### Hosted CI follow-up — fixture publication race

At voting implementation head `2e3eac2`, all 20
[Unit Tests jobs](https://github.com/fullerkris/mifolyo/actions/runs/38787942820)
passed. [Required Checks](https://github.com/fullerkris/mifolyo/actions/runs/38787942798)
passed build and smoke; its test job passed the forum suite (139 tests / 762
assertions), Composer audit and formatting, then failed one existing crawler
regression, `test_nonreaping_exit_observation_and_inherited_open_pipes`.

The test waited for marker-file existence, but `Path.write_text` creates the file
before writing its contents. CI observed the empty intermediate file (`''`
instead of `'True'`). Publishing a completed sibling file with atomic rename
closes that window. The assertions, process checks and deadlines are preserved.
The subsequent missing C0 artifact was a consequence of the earlier failed step.

A controlled 100ms open-before-write delay reproduced the original failure in
Linux/Python 3.13; the corrected fixture passes with the same injected delay.
All 18 related recovery-review/parked-command tests pass on macOS and Linux,
including Linux-native filesystem and Docker `--init` runs. The latter supplies
a normal parent process for the existing worker-PID-greater-than-one invariant.
Updated exact-head hosted results are tracked in PR #29 and the Obsidian notes.

## Voting integration, presentation and account flows — 2026-10-17

`feature/search-ui-account-completion` starts from owner-merged UX-06 main
`e26e5c1`. Voting and the atomic CI marker correction are replayed as `631d830`
and `e2e0621`. This integration also delivers the Related images fallback,
API-provided author levels, reset-email forms and password-confirmed deletion.
The captured-web report is published in
[`search-ui-captured-review-2026-10-17.md`](search-ui-captured-review-2026-10-17.md),
with the distinct merge destinations of PRs #28/#29 corrected.

| Check | Verified result |
| --- | --- |
| Forum PHPUnit | 149 passed / 840 assertions |
| Query Engine PHPUnit with isolated MongoDB databases | 106 passed / 861 assertions; final account return-link regression also passed 5 focused tests / 49 assertions |
| Full desktop/mobile browser suite | 66 passed, two existing desktop-panel-only mobile skips; no failures |
| Final presentation check | Six desktop/mobile checks passed after the last CSS adjustment |
| Real-service staging | 39 passed, zero failures/skips/suite retries |
| Final real-service sidebar/account-return probe | Six 320/768/1440px light/dark layouts passed, five unavailable previews per layout, exact source links and keyboard focus; nested account-reset return retained |
| Build and dependencies | Production assets and both application images built; both Composer audits found no advisories; npm build install audit found zero vulnerabilities |
| Formatting/configuration | Forum Pint, focused Query Engine Pint, Compose validation and whitespace checks passed |
| Deployed source binding | 90 Query Engine and 127 forum application/resource/configuration/migration/lockfile paths matched the checkout; zero mismatches |

The account checks use real Mailpit SMTP and PostgreSQL: one-time reset token
consumption, invalid/expired/used links, current-password confirmation, stale
credentials, concurrent deletion/voting, retained anonymous conversations and
other-reader vote totals. Desktop/mobile journeys pass with and without
JavaScript. SMTP outage recovery verifies that a failed send does not leave a
token cooldown blocking immediate delivery retry. PHP tests cover CSRF,
credential flashing, safe returns, private state, and stale token refresh/logout.

The first expanded staging run passed all new account journeys but found two
test-harness recovery assumptions: the shared-reader setup encountered the
replies-unavailable state before the intended outage, and image search could
reach a different FPM worker still reconnecting to MongoDB. The vote-outage
fixture now uses a fresh account; all three index surfaces follow their real
retry links within the existing bounded recovery window. The final full run
passes all 39 checks. Original failure traces remain retained.

Final deployed images:

```text
query: sha256:2f7155ce179e18e49e6b95e798892f28c1cd4376275b89b246703287a8f23489
forum: sha256:82ba2dbe8ffc979cd7a268d95d4219c30cb18abbe0b45f7fe2a9dcd3ed2e1b12
```

Local ignored evidence under `services/query-engine/test-results/`:

- `account-browser-final/` and `account-presentation-verified/`
- `account-staging-verified/` (final); `account-staging-final/` (initial findings)
- `account-sidebar-probe/` and its adjacent `.mjs` reproduction script
- `account-source-binding.json`

The separate captured-review stack still runs its reviewed `18ff77d` images;
retained crawl/index inputs and unrelated stacks were preserved. All 23 UX-06
merged-main jobs passed at `e26e5c1` (runs `39032004534` / `39032004529`). This
follow-up's owner merge and actual merged-main CI remain separate delivery steps.
Release-environment email delivery/beta acceptance and TRUST-02 level rules remain
open.
