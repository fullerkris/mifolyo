# Image Indexer

The image indexer reconciles image-search state from spider-authorized metadata.
It performs **no HTTP requests and no image-byte decoding**. Each Mongo image
`_id` is a deterministic SHA-256 page/source association and the actual source
is stored as `source_url`; query mapping preserves the public result shape
`{_id, page_url, alt, filename}` by returning `source_url` as public `_id`.
`filename` is derived locally from the normalized source URL.

## Publication contract

For each page publication the spider stores:

- `page_images:<publication-sha256>:<base64url-canonical-page-url>`: a manifest
  hash with `contract_version`, `publication_id`, `normalized_url`,
  `image_count`, and canonical JSON `image_keys`. A zero-image publication is a
  real manifest with count `0` and `[]`.
- `image_data:<publication-sha256>:<base64url-canonical-page-url>:<base64url-canonical-image-url>`:
  one metadata hash containing the normalized page/source URLs and bounded alt.

The same publication SHA-256 covers page data, sorted outlinks, and sorted image
URL/alt records. The page indexer's acknowledged completion queues the immutable
manifest key and removes only page/outlink inputs. Image inputs have no TTL and
remain until image ACK.

`image_indexer_queue` claims move atomically to
`image_indexer_queue:processing`. A renewable 60-second application-owner lock
has a monotonic fencing epoch; startup recovers abandoned claims in bounded
batches. Release, recovery, quarantine, ACK, and lock release are owner-fenced.
Invalid, absent, oversized, wrong-type, or corrupt work moves to the bounded
dead-letter list without touching MongoDB or deleting forensic inputs.

Each valid publication fully reconciles the page's `images` and `word_images`,
deletes only that page's associations, and writes an `image_page_state` record
even for zero images. Legacy source-keyed rows are removed only when their
recorded page recrawls. Every mutation carries an epoch guard. Redis ACK runs only after every
MongoDB result is acknowledged; one Lua operation then removes the exact claim,
manifest, and listed payload hashes. Lua validates all types and identities
before destructive commands.

The page/image pipeline shares the non-expiring
`canonical_url_ownership_locks` MongoDB collection. A unique
process/publication token acquires the canonical page URL atomically before
validation/reconciliation and holds it through Redis ACK or quarantine. Locks
have no TTL and cannot be stolen. A crash therefore leaves a fail-closed stale
lock: verify the recorded owner is gone, inspect the immutable Redis claim and
reconcile Mongo state, then manually delete only that exact lock. Never add TTL
or automatic cleanup to this collection. Higher epochs remain blocked while a
stale lock exists; epoch checks remain defense in depth without transactions,
so standalone Mongo is supported.

## Configuration

The service-level Compose file is a deployment artifact and requires the exact
approved `MIFOLYO_IMAGE_INDEXER_IMAGE` GHCR digest reference from the reviewed
release artifact. Deploy it only with the matching Spider and Indexer digests
using the atomic stop/drain/backup procedure in
`../../docs/immutable-pipeline-release-cutover.md`. Use root Compose for local source
builds; never mix old and new queue consumers.

```env
REDIS_HOST=<host>
REDIS_PORT=6379
REDIS_USERNAME=<optional ACL username>
REDIS_PASSWORD=<required password>
REDIS_DB=0
MONGO_HOST=<host>
MONGO_PORT=27017
MONGO_DB=mifolyo_index
MONGO_USERNAME=<required with MONGO_PASSWORD>
MONGO_PASSWORD=<required with MONGO_USERNAME>
```

Redis and MongoDB calls have five-second connection, selection, socket, read,
and write bounds. Missing datastore authentication fails startup. Only the
tracked, local-only Compose stacks opt in to unauthenticated stores with the
exact value `ALLOW_INSECURE_DATASTORES=true`.

Run tests, including real Redis DB15 lifecycle tests:

```bash
IMAGE_INDEXER_REDIS_INTEGRATION_ADDR=127.0.0.1:6379 \
  python -m unittest discover -s tests -v
```

Set `IMAGE_INDEXER_MONGO_INTEGRATION_URI=mongodb://127.0.0.1:27017` to include
the standalone-Mongo association, empty-recrawl, legacy, and ownership-race
suite. The page indexer equivalent is `INDEXER_MONGO_INTEGRATION_URI`.
