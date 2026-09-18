# PageRank

> [!IMPORTANT]
> **Status — current V1 offline ranking job; outside F3.** PageRank is not a
> Crawl Jobs V2 producer or consumer and is not in the F3 implementation path.
> Its commands and image do not authorize a V2 run. See the
> [parent remediation plan](../../docs/spider-render-remediation-plan-2026-09-01.md)
> and [F3 implementation plan](../../docs/crawl-jobs-v2-plan.md).

The PageRank service computes authority scores for searchable pages in MongoDB.
It is a one-shot batch job, not a long-running or horizontally scaled service.

## Graph contract

- Rankable nodes are the canonical HTTP/HTTPS `_id` values in `metadata`.
- Edges come from `outlinks`, with duplicate edges removed.
- Sources or targets outside `metadata` are excluded.
- Missing, empty, or fully filtered outlinks make a node dangling. Dangling
  rank is redistributed across all nodes.
- The additive `backlinks` collection is not an input.

The algorithm uses damping `0.85`, uniform initialization, an L1 convergence
tolerance of `1e-12`, and a maximum of 1000 iterations. Empty or malformed
input and non-convergence fail without changing active results.

## V1 publication capability (currently blocked)

> [!CAUTION]
> Do not publish against the retained evidence or under the current F3 gates.
> The mutating command below documents V1 capability only and requires a
> separately reviewed, isolated V1 change with graph producers stopped and
> durably flushed; this README does not grant that approval.

Running the binary without flags is read-only. It validates the graph,
calculates ranks, and prints a deterministic graph SHA-256 and summary as JSON.

Publication requires the exact validated hash:

```bash
./page-rank --publish \
  --expected-graph-sha256=<validated-sha256> \
  --confirm-target=mongo:27017/mifolyo_index/pagerank
```

The service writes a complete staging collection, creates the descending rank
index, revalidates the input, and atomically replaces `pagerank`. Every active
document records its run ID, graph hash, algorithm version, canonicalization
version, convergence data, and publication time. Repeating an unchanged run
returns `already_current` without replacing the collection.

Publication acquires a MongoDB single-writer lock before loading the graph, so
a concurrent publisher fails closed. A hard-killed publisher can leave a stale
lock that must be reviewed and removed manually. Graph producers do not honor
this lock: stop and durably flush them before validation and publication, and
keep them stopped until activation completes.

If activation has an unknown outcome, the command deliberately retains the
lock. Verify the active run and confirm that no PageRank process remains before
removing that lock by its recorded owner.

## Configuration

```bash
MONGO_HOST=localhost
MONGO_PORT=27017
MONGO_DB=test
MONGO_USERNAME=<required-user>
MONGO_PASSWORD=<required-password>
```

Credentials are applied through the MongoDB driver rather than interpolated
into a connection URI. Username and password must be supplied together, and
missing authentication fails closed. An isolated local test environment may
explicitly opt in to unauthenticated MongoDB with the exact setting
`ALLOW_INSECURE_DATASTORES=true`; never use that flag in production or a shared
environment. The root and V1 baseline Compose files make this local-only
exception explicit because their data stores are localhost-bound or isolated
on internal networks.

The service-level Compose file is a current V1 deployment artifact. The
PageRank image remains digest-pinned, but its validation alongside the legacy
page/image pipeline does not make PageRank a Crawl Jobs V2 participant. The
[legacy cutover runbook](../../docs/immutable-pipeline-release-cutover.md)
records the retired V1 procedure for historical interpretation only; it is not
current deployment or publication authorization.

A future V2 release must use one exact reviewed compatibility manifest plus
every image field and contract, Lua source-set, guard-core, and commit-guard
artifact required by the
[Crawl Jobs V2 contract](../../docs/crawl-jobs-v2.md) and F3 plan. Those
authorities determine exact membership; do not derive it from this README. The
future ordinary rollback boundary is the first successful
`CJ2_START_REQUEST`, recorded before DNS.

The historical V1 release procedure pulled the exact PageRank image with its
reviewed digest environment file. This command is retained as history and must
not be used as current release instruction:

```bash
docker compose --env-file services/page-rank/release-image.env \
  --file services/page-rank/docker-compose.yml pull
```

`MIFOLYO_PAGE_RANK_IMAGE` must exactly identify
`ghcr.io/fullerkris/mifolyo/page-rank` by a lowercase SHA-256 digest. Tags,
alternate repositories, wrong services, and malformed digests are rejected.

## Historical V1 isolated baseline run

The following command is retained as V1 protocol context from
`docs/v1-baseline-crawl-test-checklist.md`; it is not authorization to rerun the
failed baseline. The old local path used the isolated Compose project's
`ranking` profile:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile ranking run --rm page-rank
```

The historical procedure captured `graph_sha256` before running the explicit
publication command. Its validation command did not create or update
`pagerank`.

## Development

Go toolchain 1.25.13 is required. The module pins `toolchain go1.25.13` so Go's
toolchain selection uses the same patched release as CI.

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/page-rank
go run ./cmd/page-rank
```

The MongoDB publication test is opt-in and creates then removes a dedicated
`mifolyo_pagerank_contract_*` database:

```bash
PAGERANK_MONGO_INTEGRATION_URI=mongodb://localhost:27017 \
  go test -run TestPublishPageRankIntegration -count=1 ./cmd/page-rank
```
