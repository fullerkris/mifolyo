# Crawl Seeds V1

`mifolyo_index.crawl_seeds` is the source catalog for URLs eligible to enter MiFolyo's crawler. It is not a crawl-job ledger.

The authoritative portable schema is `contracts/crawl-seed-v1.schema.json`. MongoDB uses the same required fields with BSON dates for `discovered_at`, `updated_at`, and `sources[].observed_at`.

JSON Schema's `maxLength` counts characters rather than encoded bytes. The custom `x-maxUtf8Bytes` annotation records the V1 byte limit for portable consumers; importers and the MongoDB validator enforce that limit explicitly.

## Record identity

- One record exists per V1 canonical URL.
- `_id` is the V1 namespaced URL digest defined in `docs/url-canonicalization-v1.md`.
- `canonical_url` is retained as the exact URL to queue and fetch.
- Importers merge provenance by `sources[].key` rather than using first-writer-wins inserts.
- Top-level `priority` is the best, lowest source priority.
- Top-level `categories` is the sorted union of active source categories.

## Priorities

| Value | Meaning |
|---|---|
| `1` | Highest-value seed |
| `2` | Normal seed |
| `3` | Deferred or discovery-derived seed |

## Queue contract

The V1 feeder atomically maintains three Redis structures:

```text
mifolyo:crawl:v1:queue  ZSET(url_id => priority - 1)
mifolyo:crawl:v1:urls   HASH(url_id => canonical_url)
mifolyo:crawl:v1:depths HASH(url_id => canonical depth from 0 through 9007199254740991)
```

Seed records enter at depth `0`. Replays preserve both the lowest queue score
and the shallowest observed depth. The spider inspects all three values and
atomically claims a URL ID only while its score, canonical URL, and depth still
match the inspected snapshot. Non-finite scores and missing, out-of-range, or
noncanonical depth metadata fail closed. A normal feeder replay restores a
missing field. The feeder
intentionally refuses to overwrite a corrupt existing value: stop the spider,
verify the URL ID against MongoDB and `mifolyo:crawl:v1:urls`, remove only that
ID's field from `mifolyo:crawl:v1:depths`, replay the feeder, and rerun the
three-key equality check before crawling. The spider fetches the exact
canonical URL without reconstructing or forcing a scheme.
Opaque URL IDs remain queue identifiers and must not replace human-usable page
URLs in the search index.

## Lifecycle boundary

`crawl_seeds` contains discovery state only: `enabled`, provenance, priority, categories, and timestamps. Queue leases, attempts, retries, crawl errors, recrawl scheduling, and terminal outcomes require a separate durable crawl-job contract. Until that contract exists, rerunning the feeder intentionally re-enqueues enabled seeds idempotently.

## Development rebuild

The rebuild command may drop and recreate only `mifolyo_index.crawl_seeds`, and must require both a development environment and explicit confirmation. It must not delete the MongoDB volume, flush Redis, reset forum data, or modify search-index collections.

The deterministic baseline imports 70 direct records from
`seeds/manual-seeds.csv`: 67 are enabled, while BBC News, Khan Academy, and
PolitiFact are retained with `enabled: false`. The feeder atomically removes
their IDs from the queue, URL map, and depth map; their hosts are also assigned
to the crawl policy's disabled `disabled-sites` group, so any unreconciled
corrupt entry is denied before DNS. The eight `manual_reddit_discovery` rows are not direct
crawl targets. The matching `reddit-crawler` group is disabled because current
Reddit robots rules disallow all crawling. Approved local JSON exports can
still provide offline discovery provenance. Legacy DMOZ data is excluded.

Before any future bounded 67-seed crawl from this 70-record catalog, complete
`docs/v1-baseline-crawl-test-checklist.md`, obtain a new run authorization, and
preserve its test report as evidence.
