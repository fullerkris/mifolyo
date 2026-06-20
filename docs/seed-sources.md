# Seed Sources

MiFolyo should use curated seed sources rather than broad web crawling. The goal is to start from high-signal entry points and let controlled outlink discovery expand the index.

## Manual Seeds

Tracked file: `seeds/manual-seeds.csv`

Use this for founder-selected domains and topic-specific communities. Manual seeds should stay small enough to review by hand.

## Curlie

Primary site: `https://curlie.org`

Data page: `https://curlie.org/docs/en/rdf.html`

Notes:
- Curlie is the practical successor to DMOZ.
- Current downloadable data appears to be compressed TSV even where legacy pages mention RDF.
- License/attribution must be verified before production use.
- Importer should treat Curlie as its own source format rather than assuming exact DMOZ RDF compatibility.

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

Recommended v1 use:
- Discover outbound URLs from selected high-signal subreddits.
- Do not index Reddit pages themselves by default.
- Convert every Reddit listing or post page to its JSON endpoint by appending `.json`, then extract outbound URLs from the JSON payload.

Starter subreddits:
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
- Prefer official APIs or manually reviewed exports if Reddit access becomes unreliable.
- Store only the extracted outbound URLs as crawl seeds; keep Reddit metadata as source/detail fields.
