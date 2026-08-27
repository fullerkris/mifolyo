# Seed Sources

MiFolyo should use curated seed sources rather than broad web crawling. The goal is to start from high-signal entry points and let controlled outlink discovery expand the index.

## Manual Seeds

Tracked file: `seeds/manual-seeds.csv`

Use this for founder-selected domains and topic-specific communities. Manual seeds should stay small enough to review by hand.

Current disabled manual targets:
- `https://www.bbc.com/news`
- `https://www.khanacademy.org/`
- `https://www.politifact.com/`

The records remain in the catalog with `enabled: false` for provenance. Their
hosts are assigned to the crawl policy's disabled `disabled-sites` group, so
they are also denied at scheduler admission before DNS. Re-enabling a catalog
record alone is insufficient; both controls require a new review.

## Curlie

Primary site: [Curlie](https://curlie.org/)

Official pages:
- [Download Curlie Directory Data](https://curlie.org/docs/en/rdf.html)
- [Directory download](https://curlie.org/directory-dl)
- [Curlie Directory License and required attribution](https://curlie.org/docs/en/license.html)

Notes:
- Curlie is the practical successor to DMOZ.
- The current official archive is tar/gzip-compressed, UTF-8 tab-separated data. The `*-c.tsv` files contain website entries and the `*-s.tsv` files contain categories; their IDs provide the join between entries and categories.
- Curlie says it strives to publish a fresh database copy every month, so treat the cadence as approximately monthly rather than as a guaranteed schedule.
- Directory categories and listed-site data are licensed under [CC BY 3.0 Unported](https://creativecommons.org/licenses/by/3.0/). Any use must include Curlie's required attribution from the official license page, including page-level HTML attribution for public-facing uses.
- The existing `services/dmoz-importer` parses the historical DMOZ `content.rdf.u8` RDF dump and is incompatible with Curlie's current TSV archive. A Curlie importer must parse the documented TSV files and ID relationship instead of sending this data through the DMOZ parser.

## Hacker News

API base: `https://hn.algolia.com/api/v1/`

Useful endpoints:
- `https://hn.algolia.com/api/v1/search?tags=story&numericFilters=points>100`
- `https://hn.algolia.com/api/v1/search_by_date?tags=story&numericFilters=points>50`

Use HN to discover outbound URLs from high-score stories. Do not index HN comments as core search results for v1.

## Wikipedia External Links

Dump index: `https://dumps.wikimedia.org/enwiki/latest/`

Relevant dump pattern: `enwiki-latest-externallinks.sql.gz`

Notes:
- This source is large.
- Start with filtered extraction by trusted domains or by Wikipedia categories/pages related to MiFolyo launch topics.
- Prefer extracting outbound URLs rather than indexing Wikipedia pages as a substitute for MiFolyo's own community layer.

## Common Crawl

Index API: `https://index.commoncrawl.org/`

Index collections: `https://index.commoncrawl.org/collinfo.json`

Notes:
- Use later and selectively.
- Do not start with broad Common Crawl ingestion.
- Best v1 use: discover additional URLs from already-trusted domains.

## Reddit / Old Reddit

Old Reddit base: `https://old.reddit.com`

Current status (checked 2026-08-19):
- Both `https://www.reddit.com/robots.txt` and
  `https://old.reddit.com/robots.txt` declare `User-agent: *` and
  `Disallow: /`.
- The `reddit-crawler` policy group is present but disabled. Do not enqueue,
  fetch, render, or index Reddit pages without approved access and a newly
  reviewed policy.
- `services/seed-importer/reddit.py` accepts only single-source approved local
  exports matching `contracts/reddit-export-v1.schema.json`. It does not make
  unauthenticated Reddit requests.

Pending URL expansion after approval:
- `https://www.reddit.com/r/games`
- `https://old.reddit.com/r/games`
- `https://www.reddit.com/r/games.json`
- `https://old.reddit.com/r/games.json`

A trailing slash is removed before `.json` is appended, so both input spellings
`/r/games` and `/r/games/` produce the same `/r/games.json` URL. The four host
and representation variants above are distinct crawl targets, not canonical
aliases. Queries are preserved and fragments are removed.
The expansion helper is tested but is not connected to the feeder or spider
while the policy group remains disabled.

Hermetic robots verification:
- `TestHermeticRedditVariantsHonorDisallowAllWithoutPageRequests` enables the
  group only inside a Go test and queues the four `www`, `old`, and `.json`
  variants above.
- A local TLS server, synthetic public DNS answer, pinned local dialer, and
  in-memory Redis exercise the real scheduler, request gate, secure fetcher,
  robots manager, and cache without contacting Reddit.
- The fixture returns `User-agent: *` and `Disallow: /`. The asserted result is
  two robots requests, one per origin, zero page requests, zero stored pages,
  and two committed outbound attempts.
- The checked-in `reddit-crawler` group remains disabled. This test is evidence
  of fail-closed behavior, not authorization to crawl Reddit.
- `testOnlyAllowRobotsForGroups` can allow named groups only inside Go tests so
  local page fixtures can exercise post-robots crawl behavior. It is defined in
  `_test.go`, has no policy, CLI, or environment switch, and is excluded from
  the production spider binary.
- `TestHermeticTestOnlyRobotsOverrideAllowsSelectedGroupPageFetch` proves the
  selected test group can fetch one local HTML fixture without a robots request;
  `TestTestOnlyRobotsOverrideDelegatesNonSelectedGroups` proves every other
  group still delegates to normal robots enforcement.

Pending starter subreddits after approved access:
- `https://old.reddit.com/r/AskHistorians`
- `https://old.reddit.com/r/AskScience`
- `https://old.reddit.com/r/science`
- `https://old.reddit.com/r/programming`
- `https://old.reddit.com/r/webdev`
- `https://old.reddit.com/r/photography`
- `https://old.reddit.com/r/Design`
- `https://old.reddit.com/r/DataIsBeautiful`

Important:
- Respect `robots.txt` and rate limits.
- Prefer an approved Reddit Data API integration or manually reviewed export.
- Store only the extracted outbound URLs as crawl seeds; keep Reddit metadata as source/detail fields.
