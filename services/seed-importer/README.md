# Seed Importer

Utilities for discovering crawl seeds from curated external sources.

## Reddit JSON Discovery

`reddit.py` reads Reddit discovery rows from `seeds/manual-seeds.csv`, converts each Reddit URL to `old.reddit.com`, appends `.json`, fetches the JSON payload, and stores outbound non-Reddit URLs in MongoDB as crawl seeds.

Reddit pages are discovery sources only. They are not written as crawl targets by default.

Example:

```bash
docker compose run --rm seed-importer python reddit.py --dry-run
```

Fetch each discovered post page as JSON too:

```bash
docker compose run --rm seed-importer python reddit.py --dry-run --crawl-post-pages --include-comment-urls
```

Write discovered outbound URLs to MongoDB collection `crawl_seeds`:

```bash
docker compose run --rm seed-importer python reddit.py --min-score 50
```

Useful flags:
- `--min-score 25` keeps low-signal posts out of the crawl queue.
- `--delay 2.0` rate-limits Reddit requests.
- `--crawl-post-pages` converts post permalinks to `.json` and fetches them.
- `--include-comment-urls` extracts URLs from comment bodies in fetched post JSON.
- `--dry-run` prints stats without writing to MongoDB.

## Crawl Seed Feeder

`feed.py` moves pending crawl seeds into Redis sorted set `spider_queue`, which is consumed by the existing Go spider.

Feed MongoDB `crawl_seeds` records with `status=pending_crawl`:

```bash
docker compose run --rm seed-importer python feed.py --limit 1000
```

Dry-run the manual CSV seed list without crawling Reddit discovery rows:

```bash
docker compose run --rm seed-importer python feed.py --source csv --dry-run
```

Feed the manual CSV seed list into the spider queue:

```bash
docker compose run --rm seed-importer python feed.py --source csv --limit 100
```

Notes:
- The spider uses lower Redis sorted-set scores first.
- Manual priority `1` maps to Redis score `0`, priority `2` maps to score `1`, and so on.
- CSV mode skips Reddit URLs by default because Reddit is discovery-only. Use `reddit.py` to extract outbound URLs first.
