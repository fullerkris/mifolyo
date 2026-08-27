# Seed Importer

Python utilities for the V1 crawl seed catalog. URL identity follows the
shared vectors in `contracts/url-canonicalization/v1.json`; MongoDB records
follow `contracts/crawl-seed-v1.schema.json` with BSON dates.

## Development baseline

Validate the deterministic baseline without connecting to MongoDB:

```bash
docker compose run --rm seed-importer \
  python crawl_seeds.py bootstrap --dry-run
```

Create or tighten `mifolyo_index.crawl_seeds`, ensure its indexes, and merge
the 70 direct `manual` CSV rows. The resulting catalog contains 67 enabled
records and three disabled records: BBC News, Khan Academy, and PolitiFact.

```bash
docker compose run --rm seed-importer python crawl_seeds.py bootstrap
```

Bootstrap performs a read-only compatibility preflight before `collMod` or
index creation. A nonempty legacy/incompatible collection fails unchanged;
automatic production migration is intentionally out of scope.
Bootstrap also preserves an existing record's operator-controlled `enabled`
state. Use the guarded local rebuild when an existing development catalog must
adopt the three checked-in disabled states; do not use bootstrap as a hidden
enable/disable migration.

The eight `manual_reddit_discovery` rows are deliberately excluded. Because
the CSV has no source timestamp, baseline records use a fixed UTC epoch
observation time so clean local rebuilds are reproducible.

A rebuild creates and validates a complete temporary collection, then
atomically renames it over **only** `mifolyo_index.crawl_seeds`. A dry-run is
safe and reports the confirmation bound to the parsed host, database, and
collection:

```bash
docker compose run --rm seed-importer \
  python crawl_seeds.py rebuild --dry-run
```

Execution is limited to the documented local MongoDB hosts/database and
requires the process environment variable to be set explicitly to either
`MIFOLYO_ENV=development` or `MIFOLYO_ENV=test`; no CLI environment argument
can authorize it. For the Compose `mongo` host:

```bash
docker compose run --rm -e MIFOLYO_ENV=development seed-importer \
  python crawl_seeds.py rebuild \
  --confirm-rebuild mongo:27017/mifolyo_index/crawl_seeds
```

`MIFOLYO_ENV=test` is reserved for isolated local test runs. It does not relax
the local Mongo host/port/database allowlist or the exact
host/database/collection confirmation. `production`, `staging`, and an unset
or empty `MIFOLYO_ENV` are rejected before a staging collection is created.

For a host-local MongoDB URI, use the exact token reported by dry-run, such as
`localhost:27017/mifolyo_index/crawl_seeds`.

## Reddit JSON discovery

Reddit currently disallows all crawling for `User-agent: *` on both its `www`
and `old` hosts. Remote Reddit access is therefore disabled. `reddit.py`
processes only an approved local JSON export, unwraps `out.reddit.com` URLs,
and excludes Reddit-owned destinations before calling the shared V1
canonicalizer. Every observation is merged into `sources[]` by a stable source
key; existing manual or Reddit provenance is not discarded.

```bash
docker compose run --rm seed-importer python reddit.py \
  --input-json /app/testdata/reddit-listing.json \
  --dry-run
```

The export follows `contracts/reddit-export-v1.schema.json`: a single-source V1
envelope containing exactly
`schema_version`, `source_url`, `category`, `priority`, and `payload`. The
source URL and classification are therefore bound to the payload instead of
being supplied separately at import time. Files larger than 5 MiB, malformed
envelopes, invalid Reddit sources, and payloads without listing posts fail
before any MongoDB connection.

Useful options:

- `--include-comment-urls` extracts links from comment bodies.
- `--min-score 50` changes the minimum post score for the local export.

The pending URL expansion contract produces `www` HTML, `old` HTML, `www`
JSON, and `old` JSON variants. `/r/games/` becomes `/r/games.json`, not
`/r/games/.json`. Expansion does not authorize a fetch; the matching
`reddit-crawler` policy group remains disabled pending approved access.

## Redis V1 feeder

`feed.py` queues enabled MongoDB seed records and atomically removes explicitly
disabled catalog IDs from all three Redis crawl structures. In one atomic Lua
operation per batch it writes:

```text
mifolyo:crawl:v1:queue  ZSET(url_id => priority - 1)
mifolyo:crawl:v1:urls   HASH(url_id => canonical_url)
mifolyo:crawl:v1:depths HASH(url_id => canonical depth)
```

Replays preserve the best (lowest) score and shallowest depth while rejecting
corrupt existing URL or depth metadata. Disabled-ID reconciliation verifies any
existing URL mapping before removal, so a collision fails without a partial
mutation. The feeder does not read the CSV directly and never writes crawl
status back to MongoDB.

```bash
docker compose run --rm seed-importer python feed.py --dry-run --limit 1000
docker compose run --rm seed-importer python feed.py --limit 1000
```

## Tests

Install the declared dependencies, then run the suite from the service directory:

```bash
python -m pip install --requirement requirements.txt
python -m unittest discover -s tests -v
```
