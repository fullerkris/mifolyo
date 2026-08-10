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
the 70 direct `manual` CSV rows:

```bash
docker compose run --rm seed-importer python crawl_seeds.py bootstrap
```

Bootstrap performs a read-only compatibility preflight before `collMod` or
index creation. A nonempty legacy/incompatible collection fails unchanged;
automatic production migration is intentionally out of scope.

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

Reddit pages are discovery sources, not direct crawl targets. `reddit.py`
unwraps `out.reddit.com` URLs and excludes Reddit-owned destinations before
calling the shared V1 canonicalizer. Every observation is merged into
`sources[]` by a stable source key; existing manual or Reddit provenance is
not discarded.

```bash
docker compose run --rm seed-importer python reddit.py --dry-run
docker compose run --rm seed-importer python reddit.py --min-score 50
```

Useful options:

- `--crawl-post-pages` fetches discovered post permalinks as JSON.
- `--include-comment-urls` extracts links from comment bodies.
- `--delay 2.0` rate-limits remote Reddit requests.
- `--input-json /app/testdata/reddit-listing.json` performs an offline run.

## Redis V1 feeder

`feed.py` reads only enabled MongoDB seed records. In one transactional Redis
pipeline per batch it writes:

```text
mifolyo:crawl:v1:queue  ZSET(url_id => priority - 1)
mifolyo:crawl:v1:urls   HASH(url_id => canonical_url)
```

`ZADD LT` preserves the best (lowest) score across replays. The feeder does
not read the CSV directly and never writes crawl status back to MongoDB.

```bash
docker compose run --rm seed-importer python feed.py --dry-run --limit 1000
docker compose run --rm seed-importer python feed.py --limit 1000
```

## Tests

Run the standard-library suite from the service directory:

```bash
python -m unittest discover -s tests -v
```
