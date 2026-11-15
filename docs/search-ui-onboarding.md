# Search UI onboarding — 2026-10-27

Implemented on `feature/search-ui-onboarding`, based on main
`e8a0a415ac2b370319cd57f4c5262ce616a536fb`. This checkpoint describes the local
working tree and dedicated Search UI staging deployment; publication/hosted CI
and release-environment acceptance are separate delivery steps.

## Reader experience

- New registrations enroll automatically. Existing accounts are not backfilled.
  Default registration returns home; an existing validated search, source or
  thread/reply destination is retained.
- **Welcome to MiFolyo** appears on eligible reading pages until **Got it** or
  the first successful nonempty HTML web/exact/image search. Empty, invalid,
  failed and JSON searches do not complete it. A successful search with zero
  results still counts as a search. There is no blocking tour.
- **Discuss** guidance appears inline beside the first usable web result's
  Discuss action. It completes after a successful desktop list load (including
  an empty list) or opening a valid discussion/thread page. Failed desktop loads
  leave it available. The full discussion list continues to require JavaScript;
  visiting the discussion/thread page completes the step without JavaScript.
- **Voting** guidance appears once per eligible thread page, next to the original
  post's vote controls, and explains that threads and replies can be voted on.
  Locked/removed content, missing scores and signed-out views do not show it.
  Only a committed `1` or `-1` vote on a post or comment completes this step.
  Failed votes do not complete it; vote removal never reopens it.
- Discuss and voting hints expire exactly seven days after registration.
  Welcome remains eligible until its completion event. Each completed step stays
  complete across refresh, sign-out/sign-in and other browsers/devices.
- Welcome dismissal and voting support ordinary HTML forms. JavaScript adds
  inline dismissal, retry feedback, duplicate-submit protection and focus
  retention. Storage-disabled browsing works. Returning focus/visibility or a
  restored BFCache page refreshes hints against the current account.

## State and API

The forum stores four nullable timestamps on `users`: `onboarding_started_at`,
`onboarding_welcome_at`, `onboarding_discuss_at`, `onboarding_vote_at`. Both API and
legacy web registrations enroll; factories/imports and existing users do not.
The timestamps are hidden from serialized users. Auth responses expose only
`onboarding: {welcome, discuss, vote}`; **true means guidance is still eligible**.

Authenticated `POST /api/auth/onboarding` accepts `welcome` or `discuss`.
Completion is idempotent and revalidates the current bearer token under an account
row lock. Vote completion happens inside the existing vote transaction rather
than accepting a browser "voted" claim. Account reset/deletion and vote writes
coordinate through the same account-first lock order.

The Query Engine reuses its existing `/api/auth/me` lookup on each request, so
progress is not cached in the browser session. `GET /onboarding` returns
`{user_id, data: {welcome, discuss, vote}}`; CSRF-protected `POST /onboarding`
requires `step` and the rendered `user_id`. A stale account ID returns 409. HTML
forms also supply a validated local `return_to`. Both routes are rate-limited;
responses are private/no-store. Invalid/unavailable state does not show guidance,
and a failed automatic save does not interrupt search or reading.

## Deployment and review

Apply `2026_10_27_000000_add_onboarding_progress_to_users` before serving the updated
forum application. Deploy the forum API before the Query Engine and build its
assets with `npm ci && npm run build`. The additive migration has no account
backfill. New API clients tolerate missing onboarding state, but the new forum
code requires the migrated schema.

The [dedicated local staging runbook](search-ui-local-staging.md) describes image
builds and startup. Both application images and Query Engine assets were rebuilt;
PostgreSQL reports the onboarding migration applied in batch 2. The original
`staging_reader` account was checked and remains unenrolled.

Review at <http://127.0.0.1:18081>: create an account, dismiss or search, choose
Discuss, then open a thread and vote. Use a new account to replay the initial
experience; deleting browser storage does not reset account progress.

## Verification

| Check | Result |
|---|---|
| Forum onboarding/auth/vote PHP regressions | 19 passed / 285 assertions |
| Final forum legacy-registration/onboarding checks | 7 passed / 64 assertions (includes the same five onboarding tests) |
| Query Engine onboarding/search/account/thread/vote PHP regressions | 52 passed / 378 assertions |
| Final Query Engine onboarding checks | 10 passed / 71 assertions, including the added expired-HTML-form reading return |
| Search UI, result presentation and voting browser fixtures | 48 passed; two existing desktop-only scenarios skipped on mobile |
| New real-service onboarding journeys | 8 passed across desktop/mobile; zero suite retries |
| Real forum outage/recovery | 1 passed; welcome and voting stay retryable during the outage and complete after explicit retry |
| Presentation | Welcome/Discuss/vote captured at 320/768/1440px in both themes; no horizontal overflow; keyboard dismissal/vote focus checks passed |
| Build and style | Query Engine npm build; both application images; focused Pint and whitespace checks passed |
| Deployed source binding | 17 changed/new Query Engine and 6 forum runtime files match the checkout; zero mismatches |

The real-service journeys exercise registration return URLs, failure/retry,
first search, empty/full discussions, confirmed voting/removal, independent
browser login, account switching, CSRF/account mismatch, blocked storage and
no-JavaScript forms. Failure injection in `onboarding.spec.js` deliberately
replaces selected browser responses with 503; successful paths use real services.
Headless Chromium keeps tabs visible/focused, so focus and persisted-pageshow
notifications are explicitly dispatched when checking the refresh handlers.
PHP tests cover exact seven-day expiry, malformed state, expired credentials,
idempotence, rejected/zero/nonzero votes and invalid-first-result guidance.

Artifacts (ignored): `services/query-engine/test-results/onboarding-staging-verified/`,
`onboarding-browser-regression/`, `onboarding-outage-verified/`, and
`onboarding-source-binding.json` in the same test-results directory.

Reproduce the new checks from each service directory:

```sh
# Either service (forum uses isolated SQLite; Query Engine fakes the API here)
php artisan test --filter=OnboardingTest

# Query Engine, against the dedicated loopback staging stack
npm run test:staging -- onboarding.spec.js --project=desktop --project=mobile
npm run test:staging -- recovery.spec.js --project=recovery --no-deps --grep 'a vote survives'
```

The recovery test briefly stops/restarts only the dedicated staging forum. Run it
after other staging journeys finish. Auth journeys honor actual server backoff;
the test suite's retry setting remains zero.

This increment implements the selected welcome/Discuss/voting guidance. Optional
interest selection, a guidelines page, broader empty-state work and release/beta
acceptance remain tracked under the broader onboarding/product roadmap.
