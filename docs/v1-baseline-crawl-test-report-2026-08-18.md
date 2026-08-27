# V1 Baseline Crawl Test Report

**Date:** 2026-08-18
**Operator:** OpenCode-assisted local run
**Branch / commit:** `main` / `90f44860586aacd60df0e3f1ca16dae05ea7c670`
**Worktree:** Uncommitted test worktree
**Environment:** Isolated local Docker Compose project
**Compose file:** `scripts/docker/v1-baseline.compose.yml`
**Compose project:** `mifolyo-v1-baseline-test`
**MongoDB database:** `mifolyo_index`
**Redis database / queue key:** `0` / `mifolyo:crawl:v1:queue`
**HTTP port:** `127.0.0.1:18080`
**Scope:** Search only
**Root development DB publication status:** Stopped
**Image pipeline:** Disabled

## Before

- Root development MongoDB, Redis, and PostgreSQL were stopped with their
  containers and volumes preserved.
- Host ports `27017`, `6379`, and `5432` had no listeners.
- Isolated MongoDB, Redis, PostgreSQL, Caddy, and query engine were healthy.
- `crawl_seeds` contained 70 enabled records with schema and canonicalization
  version 1, strict validation, and the required indexes.
- V1 crawl queue count: 70.
- V1 URL-map count: 70.
- V1 depth-map count: 70, all canonical string `0`.
- `pages_queue` count: 0.
- Policy SHA-256:
  `79ab8c6fe1e3dbedb91584b70c68bac6199ec0e60ee789ffab4d9b31b7211895`.
- Spider image:
  `sha256:ffb2b8902237ddd65e510dd47d4fad32ea413a376e7011107c275e7d01c6ec75`.
- `indexer` and `backlinks-processor` were running with zero restarts.
- Neither `spider` nor `image-indexer` was running.

## Execution

The authorized command was:

```bash
docker compose --project-name mifolyo-v1-baseline-test \
  --file scripts/docker/v1-baseline.compose.yml \
  --profile crawl run --rm spider \
  ./spider --once --max-concurrency 2 --max-pages 10 \
  --validate-baseline-policy
```

- The one-off spider exited successfully.
- Outbound attempts committed: 10.
- Pages fetched: 3.
- Fetched URLs: `https://www.loc.gov/`, `https://go.dev/doc/`, and
  `https://fonts.google.com/`.
- Robots enforcement failed closed for `https://www.bbc.com/news`,
  `https://www.politifact.com/`, and `https://www.khanacademy.org/`. These were
  fallback decisions after robots retrieval errors, not explicit `Disallow`
  matches.
- Link records persisted by the spider: 234.
- Image references persisted by the spider: 86.
- Page notifications published: 3.
- Unexpected redirects observed: none in the captured spider output.

## Pipeline Output

- `pages_queue` drained to 0.
- `https://go.dev/doc/` and `https://www.loc.gov/` indexed successfully.
- The indexer's below-threshold in-memory buffers were flushed with a graceful
  stop, after which the indexer was restarted successfully.
- MongoDB metadata records: 2.
- MongoDB word records: 496.
- MongoDB dictionary records: 480.
- MongoDB outlink records: 2, containing 231 links.
- MongoDB backlink records: 231.
- Invalid, digest-only, or duplicated-scheme metadata, outlink, and backlink
  identities: 0.
- Redis image reference hashes: 86; invalid or duplicated-scheme image URLs: 0.
- `image_indexer_queue` count: 2.
- No `images` collection exists and no image-indexer container ran.
- `https://fonts.google.com/` produced no processable text. The retained
  341,331-byte response is a JavaScript application shell whose body contains
  only an empty `<gf-root>` element and scripts. The indexer intentionally
  extracts paragraph elements, found zero `<p>` elements, and therefore
  generated an empty summary and zero tokens despite finding the title and
  description metadata. Its queue message was consumed, while
  `page_data:https://fonts.google.com/` remains in Redis without an expiry for
  manual review.

## Queue After Discovery

- V1 crawl queue count: 261.
- V1 URL-map count: 267.
- V1 depth-map count: 267.
- Pending depth distribution: 64 at depth 0 and 197 at depth 1.
- Claimed URL IDs retained as hash-only identity records: 6.
- Pending IDs missing URL or depth metadata: 0.
- Invalid pending identities, depths, or scores: 0.

## Follow-up Diagnosis

Follow-up requests were limited to the three `/robots.txt` endpoints. No page
crawl or second spider batch was run. The production policy, secure fetcher,
and robots manager were also exercised directly with `MiFolyoBot/1.0`; the
temporary diagnostic command was removed after use.

| Target | Current robots response | Production-path result | Safe disposition |
|---|---|---|---|
| `https://www.bbc.com/news` | `https://www.bbc.com/robots.txt` returns `200 text/plain`. The wildcard group does not disallow `/news`, although the file's comments and linked terms explicitly reject scraping, crawling, and systematic extraction. | Allowed by REP rules. The original crawl-time transport cause cannot be recovered from the redacted outer error; the current request succeeds. | Keep the seed disabled unless the BBC grants permission. A technical wildcard allow does not override the site's explicit usage statement. |
| `https://www.politifact.com/` | The `www` robots URL returns `301` to `https://politifact.com/robots.txt`. The apex file returns `200` and disallows `/wp-admin/`, allows `/wp-admin/admin-ajax.php`, and declares `Crawl-delay: 10`. | Fail closed with `robotsguard: fetch_failed` caused by `securefetch: matcher_denied`; the robots manager deliberately rejects a redirect that changes host. | Do not weaken redirect validation. A future approved policy may canonicalize the seed to the apex host, but it must also enforce the requested 10-second delay. The current journalism group interval is only one second. |
| `https://www.khanacademy.org/` | The robots endpoint returns `200 text/html` with a client challenge rather than robots directives. | Currently allowed because the REP parser sees no applicable rules in the challenge document. The original crawl-time transport cause cannot be recovered; the current request succeeds. | Keep the seed disabled. Do not treat the challenge as reliable robots authorization or bypass it; require a valid robots response or site permission first. |

The crawl-time log preserved `robotsguard: fetch_failed: fallback=deny` and the
outer `securefetch: authorize_hop: hop_denied`, but not the nested secure-fetch
reason. Consequently, the exact historical transport causes for BBC and Khan
Academy are not recoverable from the captured evidence. Their successful
follow-up transport checks do not justify replaying the claimed seeds.

## Search Checks

| Query | Expected | Actual | Result |
|---|---|---|---|
| `go` | Go documentation | `https://go.dev/doc/` with matching title and summary | Pass |
| `library` | Library of Congress | `https://www.loc.gov/` plus Go documentation | Pass |
| Third newly indexed page | At least 3 searchable pages | Only 2 pages indexed | Fail |

## Issues

1. `fonts.google.com` is a client-rendered application shell with no paragraph
   text in the fetched HTML. The indexer consumed its queue item but left its
   page data in Redis without retry or expiry. Passing this page would require
   an explicitly chosen metadata fallback, a bounded JavaScript renderer, or a
   replacement server-rendered baseline seed.
2. The indexer logs an idle Redis blocking-pop socket timeout about once per
   minute as an error, then pushes another `RESUME_CRAWL` signal. The signal
   queue reached 200 entries and continues growing while idle.
3. Only 2 of 3 fetched pages became searchable, so the required three-page
   search criterion did not pass.
4. The historical BBC and Khan Academy retrieval causes are not diagnosable
   from the current stable-only logs. A future run should log the nested stable
   secure-fetch reason without exposing raw URLs or transport text.
5. PolitiFact's cross-host robots redirect is safely rejected, and its declared
   10-second crawl delay is stricter than the current group interval.
6. Khan Academy's HTML client challenge is accepted as an empty robots policy.
   The seed must remain disabled unless challenge responses are made fail-closed
   or valid authorization is obtained.

## After

- Root development database containers remain stopped and database host ports
  remain closed.
- `indexer` and `backlinks-processor` are running with zero restarts.
- `image-indexer` and `spider` are not running.
- The isolated project remains running for inspection; cleanup was not
  performed.
- No forum or account resource was targeted.

## Verdict

**FAIL** under the strict checklist because the required three newly searchable
pages criterion did not pass. The crawler's policy, request bound, URL identity,
data isolation, and image-pipeline controls behaved as designed.

## Post-run Scope Change

After this run, the checked-in crawl policy gained a disabled
`reddit-crawler` group and a new approved policy digest of
`e6ddb4027b1167c92b0f298d929bf37abfd0b5fd522f2d3939356ea88ee33f20`.
The group matches Reddit URLs but denies them before DNS; both current Reddit
robots files disallow all crawlers. The policy and JavaScript rendering design
were not used by this recorded crawl. The original policy digest and spider
image above remain the evidence for this historical result, and any future
baseline run requires a rebuilt image and fresh report.

## Post-run PageRank Update

On 2026-08-19, PageRank was added to the existing isolated crawl result without
running another spider batch or making an external request.

- `pages_queue` was `0` on two checks five seconds apart.
- The indexer received SIGTERM, performed its final bulk operations, and exited
  with code `0` without a final flush error.
- The backlinks processor had no pending work and was stopped. It logged
  `Service stopped` but exited with code `1`; PageRank does not consume its
  additive `backlinks` projection.
- PageRank publication image:
  `sha256:f1b69ca7ed371aa25b2e3b54c6c06320719adbe9ae142f26e05da56b8c2851b4`.
- Graph SHA-256:
  `59af1d48c6f46753308db2fca6511c75f31b0c3c5cd4adbee16676012d02b026`.
- Graph input: 2 metadata nodes, 2 outlink documents, 0 internal edges, 231
  filtered unindexed targets, and 2 dangling nodes.
- Algorithm result: 1 iteration, stationary L1 residual `0`, and rank sum `1`.
- An independent MongoDB-side recomputation from filtered `outlinks` also
  reported stationary L1 residual `0`, maximum per-node residual `0`, and rank
  sum `1`.
- Published run ID:
  `20260819T161826Z_d0129fde7f78c57d64ef282d`.
- `https://go.dev/doc/` and `https://www.loc.gov/` each received rank `0.5`.
- The `metadata` and `pagerank` ID sets and counts match exactly, all ranks are
  finite and non-negative, `rank_desc` exists, and no staging collection
  remains.
- A repeated publication returned `already_current` with the same run ID.
- The subsequently hardened PageRank image
  `sha256:6cf3431e9b4eb01f2864f51518bd3860f7972982a38ce7a91595917ab8082fa3`
  enforces canonicalization V1 and a single-publisher lock. It validated the
  same graph, rejected a mismatched expected hash, returned `already_current`
  for the approved hash, and left zero publication lock records.
- Query image:
  `sha256:3192eac30d50e0df826228612a3c9bf41377cc0fd8204ddac76590aa25458c89`.
- The rebuilt query service applies normalized PageRank before pagination. A
  live isolated `go` query returned the Go page with PageRank `0.5` and combined
  score `0.4`; the top-ranked endpoint returned a URL present in metadata, and
  an invalid page number returned HTTP `422`.
- The stale root development Caddy that had retained all-interface host
  bindings was stopped. Only the isolated Caddy remains published, on
  `127.0.0.1:18080`.

This update does not change the original strict **FAIL** verdict: only two of
the three fetched pages are searchable.
