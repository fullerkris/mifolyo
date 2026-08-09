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

The V1 feeder uses two Redis structures:

```text
mifolyo:crawl:v1:queue  ZSET(url_id => priority - 1)
mifolyo:crawl:v1:urls   HASH(url_id => canonical_url)
```

The spider pops a URL ID, resolves its exact canonical URL from the hash, and fetches that URL without reconstructing or forcing a scheme. Opaque URL IDs remain queue identifiers and must not replace human-usable page URLs in the search index.

## Lifecycle boundary

`crawl_seeds` contains discovery state only: `enabled`, provenance, priority, categories, and timestamps. Queue leases, attempts, retries, crawl errors, recrawl scheduling, and terminal outcomes require a separate durable crawl-job contract. Until that contract exists, rerunning the feeder intentionally re-enqueues enabled seeds idempotently.

## Development rebuild

The rebuild command may drop and recreate only `mifolyo_index.crawl_seeds`, and must require both a development environment and explicit confirmation. It must not delete the MongoDB volume, flush Redis, reset forum data, or modify search-index collections.

The deterministic baseline imports the 70 direct targets from `seeds/manual-seeds.csv`. The eight `manual_reddit_discovery` rows configure Reddit discovery and are not direct crawl targets. Legacy DMOZ data is excluded.

Before running the first bounded 70-seed crawl, complete `docs/v1-baseline-crawl-test-checklist.md` and preserve its test report as evidence.
