# MiFolyo

MiFolyo is a community-first search and discussion platform built on
IonelPopJara's Moogle search engine and a Laravel forum/community service.

## Start the local application

Use the [Search UI staging runbook](docs/search-ui-local-staging.md) to build,
start, seed, check and stop the integrated application. It runs the real Query
Engine and forum with project-owned databases and synthetic staging data.

- Search: <http://127.0.0.1:18081>
- Forum: <http://127.0.0.1:18082>
- Local email inbox: <http://127.0.0.1:18085>

Read [environment boundaries](docs/environments.md) before selecting a Compose
project. The root development stack, local staging, retained V1 baseline and
production have separate data and lifecycle requirements.

## Services

| Service | Purpose |
|---|---|
| [Query Engine](services/query-engine/README.md) | Search UI, search APIs and community integration |
| [Forum Engine](services/forum-engine/README.md) | Accounts, posts, comments, voting and moderation |
| [Spider](services/spider/README.md) | Crawling, policy enforcement and extraction |
| [Indexer](services/indexer/README.md) | Inverted index and page metadata |
| [Image Indexer](services/image-indexer/README.md) | Spider-authorized image metadata |
| [Backlinks Processor](services/backlinks-processor/README.md) | Backlink reconciliation |
| [PageRank](services/page-rank/README.md) | Graph ranking |
| [TF-IDF](services/tfidf/README.md) | Term and document-frequency weights |
| [Seed Importer](services/seed-importer/README.md) | Seed catalog tooling |
| [Render Worker](services/render-worker/README.md) | Optional isolated Chromium worker |
| [Monitoring](services/monitoring/README.md) | Pipeline monitoring |

## Crawl runtime

V1 remains the application runtime. Crawl Jobs V2 is dormant implementation
work; C0 runtime is owner-paused, native ARM64 calibration is deferred, and full
M4 acceptance and application integration remain open. Starting the application
does not authorize crawling, migration, candidate promotion or rendering.

The root Compose file has optional `pipeline`, `crawl`, `batch` and `render`
profiles. The Spider is separate from the pipeline and defaults to policy
validation. Preserve retained crawl evidence and leave these profiles stopped
unless their specific operation has been approved.

The canonical [Crawl Jobs V2 protocol](docs/crawl-jobs-v2.md) remains in the
repository because generators, Docker builds and conformance tests consume its
exact bytes.

## Documentation

README files, startup runbooks and build-required inputs live in Git.
Plans, status reports, reviews and repository agent definitions are archived in
the Obsidian notebook:

```text
Projects/MiFolyo/Archive/Archive-GitHub Docs/2026-11-15/
```

See the [documentation index](docs/README.md) for the retained files, source
manifests and historical-evidence recovery instructions.
