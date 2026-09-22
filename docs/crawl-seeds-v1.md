# Crawl Seeds V1

> [!IMPORTANT]
> **Status - current V1 reference.** The crawler runtime still uses V1, while
> F1 and F2 remain not started. This catalog and its feeder do not create or
> authorize Crawl Jobs V2 work. See the
> [parent remediation plan](spider-render-remediation-plan-2026-09-01.md) and
> [F3 implementation plan](crawl-jobs-v2-plan.md) for current gates.

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
noncanonical depth metadata fail closed. A feeder replay can restore a missing
field, but intentionally refuses to overwrite a corrupt existing value.

The former selective-depth-deletion and feeder-replay procedure is historical
disposable-development context only, not an approved retained-evidence repair.
Do not selectively delete depth metadata or replay the feeder against retained
evidence. Preserve it for the parent plan's matched writer freeze, backup,
restore-test, and separately approved reset sequence; this reference supplies
no recovery or crawl authorization.

The spider fetches the exact canonical URL without reconstructing or forcing a
scheme. Opaque URL IDs remain queue identifiers and must not replace human-usable
page URLs in the search index.

## Lifecycle boundary

`crawl_seeds` contains discovery state only: `enabled`, provenance, priority, categories, and timestamps. Queue leases, attempts, retries, crawl errors, recrawl scheduling, and terminal outcomes require a separate durable crawl-job runtime. Until an accepted V2 runtime replaces V1, rerunning the feeder intentionally re-enqueues enabled seeds idempotently.

The retained isolated state and the required durable-job remediation are
recorded in the
[Spider and Render Worker remediation plan](spider-render-remediation-plan-2026-09-01.md).
A feeder replay is reconciliation, not a queue reset: it does not remove
discovered jobs or prove that claimed work reached a terminal state. Do not run
another crawl through the historical V1 path. A future run requires every
parent-plan gate, the tested V2 procedure, and new explicit site/run
authorization under F3 M8; M1/M2 and F5 code acceptance plus dormant M3
completion do not authorize it.

## Development rebuild contract

The rebuild implementation stages and validates a replacement for only
`mifolyo_index.crawl_seeds`. It requires a development/test environment guard
and exact target confirmation; those checks are not execution permission. It
must not delete the MongoDB volume, flush Redis, reset forum data, or modify
search-index collections. Retained-state reconciliation remains blocked by the
parent plan's matched freeze, backup, restore-test sequence and future approval.

The checked-in deterministic baseline defines 70 direct records from
`seeds/manual-seeds.csv`: 67 are enabled, while BBC News, Khan Academy, and
PolitiFact have target state `enabled: false`. The V1 feeder atomically removes
IDs from the queue, URL map, and depth map only for records already marked
disabled in MongoDB; it does not reconcile the catalog. The checked-in policy
independently assigns their hosts to the disabled `disabled-sites` group and
denies admission before DNS. The eight `manual_reddit_discovery` rows are not
direct crawl targets. The matching `reddit-crawler` group remains disabled;
the robots responses recorded on 2026-08-19 disallowed all crawling. Approved
local JSON exports can still provide offline discovery provenance. Legacy DMOZ
data is excluded.

The 70-record, 67-enabled/3-disabled catalog is the later target, not the
retained state. The [2026-08-18 report](v1-baseline-crawl-test-report-2026-08-18.md)
records 70 enabled seeds and a strict **FAIL**. F1 has not run, and bootstrap
preserves existing operator state, so no fresh accepted 67/3 baseline is implied.
The execution commands in `docs/v1-baseline-crawl-test-checklist.md` are
historical protocol context, not authorization for another crawl. F3 M6 must
replace them with tested V2 procedures before any future bounded run.
