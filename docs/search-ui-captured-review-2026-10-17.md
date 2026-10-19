# Search UI captured-web review — 2026-10-17

## Scope and environment

The owner selected **captured pages** for pre-merge validation of UX-06 PR #28
and COM-01 PR #29. This review uses application head
`18ff77d269043e8aaf78ea0cf4cecc0c1f6372df` in the separate local Compose project
`mifolyo-search-ui-captured-review`.

- Search UI: <http://127.0.0.1:18083>
- Forum API: <http://127.0.0.1:18084>
- MongoDB and PostgreSQL database: `mifolyo_search_ui_captured_review`, each in
  this project's own container and named volumes.
- Local sign-in: `reader@captured.mifolyo.test` /
  `local-staging-only-password`.
- The deployed application/configuration/resource/lockfile bytes matched the
  reviewed `18ff77d` revision: **77 Query Engine files and 106 forum files**, zero mismatches.

Validated image IDs:

```text
query: sha256:69bce813a4adfad77a3fdc966effaa86df9e697916b056dac666649faf1dc305
forum: sha256:c340b41de504141562b60f698b54f03d5ac5d445af9f28e05fa16dd3eba10dd7
```

## Retained input and replay

The source is the retained `mifolyo-v1-baseline-test` result from
2026-08-18, exported read-only. Its two searchable pages are:

- <https://go.dev/doc/> — Documentation - The Go Programming Language
- <https://www.loc.gov/> — Home | Library of Congress

Copied search records: **2 metadata, 496 word postings, 480 dictionary entries,
2 outlink documents (136 Go links and 95 Library of Congress links), 231 backlink
documents, and 2 PageRank records**. Their original crawl timestamps remain.

The retained Redis reference sets contain **86 image/source associations**:
17 for Go and 69 for the Library of Congress. An offline adapter uses the current
Image model and image-indexer word-weight helper to derive 86 image records and
12,240 image word postings from those URLs and the captured page keywords.
Original image bytes and alt text were not retained; alt text stays empty.
This adapter prepares review data; it does not assert a new pipeline publication.

This is a replay of captured index data, not a fresh web crawl or page-HTML
reindex. The third historical capture, the Google Fonts JavaScript shell, remains
outside the searchable input. During automated browser checks, external image
requests are blocked and preview fallback is exercised with exact source/image
destinations retained. Successful remote-thumbnail loading is outside this run.

Input SHA-256 values:

```text
search records:   801e2c83504bc8c3c2d7a50f74c2b4256cdc2290ad14359f8af9c805d5204a08
image references: cce1d5838f5d690510c0ff4edf4f50d04dea3be03aca5ab963651721ab492b9d
retained queue:   cfd49e8f8be06897b74aef1d1904727b2f15e6540419f8beb697bb3d3d322f7a
```

## Verification

**8 browser journeys passed, 0 failed, 0 skipped, 0 retries** in 37.4 seconds,
using real Query Engine/forum controllers, MongoDB and PostgreSQL. Each of the
following ran on desktop and 320px mobile:

1. Captured web result titles, excerpts/highlighting and exact source URLs;
   outgoing-link pagination, incoming backlinks, bundled map and search return.
2. Image-reference results and source association; all 86 associations across
   five desktop pages without duplication; mobile pagination, unavailable-preview
   fallback, discussion navigation and exact Web/Images return context.
3. Source-linked thread creation and nested replies; set/change/remove votes,
   concurrent duplicate submissions, independent two-reader totals, persisted
   selection after reload, keyboard focus and unfinished reply retention. Token
   rotation returns through sign-in to the exact comment/reply/image-search
   destination without automatically replaying the vote.
4. Search, image references, Page links, thread/reply creation and ordinary
   thread/comment voting forms with JavaScript disabled.

Responsive assertions passed at **320/768/1440px in both themes**, including
44px vote targets and no horizontal overflow. Representative screenshots were
visually inspected; 26 automated captures plus five focused review captures are
retained. The focused visual probe reported zero JavaScript errors.

The source search records, image-reference sets and retained crawl queue compare
**byte-for-byte equal before and after testing**. The review stack's six services
are healthy, and both its readiness endpoints and the original staging readiness
endpoints return HTTP 200.

## Finding — Related images preview fallback

**Minor visual follow-up:** when remote thumbnails are unavailable, the Web
results' **Related images** sidebar displays native broken-image icons. All five
observed thumbnails completed with `naturalWidth=0` and lacked preview-failure
handling. The main Images results correctly show **Preview unavailable**.

- Reproduction: open `/search/exact?q=library` while blocking external image
  requests, then inspect the Related images sidebar in either theme.
- Location: `services/query-engine/resources/views/search-results.blade.php:60`.
- Evidence: `related-image-fallback-finding-light.png`,
  `related-image-fallback-finding-dark.png`, and `visual-finding.json`.
- Follow-up implementation: `feature/search-ui-account-completion` applies the
  existing unavailable-preview treatment to sidebar thumbnails, retains exact
  source links, and uses a single-column layout on narrow mobile screens.
  Its verification is recorded in `docs/search-ui-local-staging.md`. The captured
  stack and screenshots above remain the original `18ff77d` review evidence.

The functional journeys pass; visual acceptance retains this follow-up. The
review does not establish fresh-crawl or remote-image-loading acceptance.

## Local evidence

Evidence is under the Git-ignored directory:

```text
services/query-engine/test-results/captured-review-2026-10-17/
```

It includes the captured records, image-reference adapter output, SHA-256
manifests, deployed application binding, browser results, and screenshots.

Useful review destinations:

- [Captured web results](http://127.0.0.1:18083/search/exact?q=library)
- [Captured image references](http://127.0.0.1:18083/images?q=fashion)
- [Persisted voting conversation](http://127.0.0.1:18083/threads/3?from=%2Fimages%3Fq%3Dlibrary%26page%3D2)

The locally retained `tools/` directory contains the exact export/import helpers,
Compose override, browser suite and focused visual probe. From `services/query-engine`,
rerun the browser checks against the running review stack with a fresh output:

```sh
CAPTURED_REVIEW_OUTPUT="$PWD/test-results/captured-review-rerun" npx playwright test --config test-results/captured-review-2026-10-17/tools/playwright.config.mjs
```

To stop this review stack while retaining its data, run from the repository root:

```sh
docker compose -p mifolyo-search-ui-captured-review -f scripts/docker/search-ui-staging.compose.yml -f services/query-engine/test-results/captured-review-2026-10-17/tools/compose.override.yml stop
```

## Delivery update

The owner merged PR #28 into `main` as `e26e5c1` on 2026-10-17 at 12:18:27 UTC.
All 23 actual merged-main CI jobs passed: [Unit Tests](https://github.com/fullerkris/mifolyo/actions/runs/39032004534)
and [Required Checks](https://github.com/fullerkris/mifolyo/actions/runs/39032004529).
PR #29 merged into `feature/search-ui-ux06` as `0ec446a` at 12:18:43 UTC;
it did not put voting into `main`.

The follow-up branch `feature/search-ui-account-completion` is based on `e26e5c1`
and carries voting (`631d830`, replayed from `2e3eac2`) and its CI fixture fix
(`e2e0621`, replayed from `18ff77d`), plus the sidebar fallback, author levels
and account reset/deletion flows. Its integration PR requires its own owner
merge decision and subsequent merged-main verification.
