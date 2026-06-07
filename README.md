# MiFolyo

MiFolyo is a community-first search and discussion platform. This repository currently contains two major foundations:

- A Moogle-derived search engine stack for crawling, indexing, ranking, and querying web pages.
- A Laravel forum/community service scaffold for posts, comments, voting, reports, moderation, and search integration.

## Search Engine Foundation

The search stack is based on Moogle, an educational search engine inspired by early web architecture. It uses Redis for crawl queues and temporary pipeline data, MongoDB for indexed search data, PostgreSQL for MiFolyo application/community data, and Laravel for the query engine.

### Search Services

- **Spider**: Crawls pages, extracts links and images, and writes crawl data to Redis.
- **Indexer**: Builds the inverted index and page metadata in MongoDB.
- **Image Indexer**: Indexes images discovered during crawling.
- **Backlinks Processor**: Transfers backlink data from Redis to MongoDB.
- **Page Rank**: Calculates PageRank over backlink data.
- **TF-IDF**: Calculates term frequency-inverse document frequency weights.
- **Query Engine**: Laravel service that returns ranked search results using TF-IDF and PageRank.

The local development stack is defined in the root `docker-compose.yml` and supports optional `pipeline` and `batch` profiles.

## Forum Engine Foundation

The forum/community scaffold lives in `services/forum-engine` and provides the foundation for MiFolyo's discussion layer.

Planning docs:

- `docs/forum-service-architecture.md`
- `docs/forum-service-implementation-plan.md`
- `docs/forum-service-scaffold-checklist.md`

Supporting scripts:

- `scripts/docker/infra.compose.yml`
- `scripts/fullstack.sh`
- `scripts/smoke-forum.sh`

## Local Development

Start the core local search stack:

```bash
docker compose up -d
```

Run the optional crawl/indexing pipeline services:

```bash
docker compose --profile pipeline up -d
```

Run batch ranking jobs:

```bash
docker compose --profile batch run --rm tfidf
docker compose --profile batch run --rm page-rank
```

The local spider identifies itself as `MiFolyoBot/1.0` and the root Compose spider command is bounded for development with `--max-concurrency 2 --max-pages 10 --once`.

## Notes

MiFolyo is in active rebuild. The search stack and forum stack are being reconciled into one Laravel-centered private-beta foundation.
