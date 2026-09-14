# Indexer

The Indexer is a core service in the Moogle search engine pipeline. Its job is to process crawled web pages from the Spider, extract and index relevant data, and store it in MongoDB for fast retrieval by other services. The Indexer builds the inverted index, manages metadata, and prepares data for ranking and querying.

For a rendered page, the indexer also stores the latest original response and
rendered DOM in MongoDB's dedicated `page_artifacts` collection before deleting
the transient Redis page hash. These untrusted HTML artifacts remain separate
from the search-facing `metadata` collection and include the render-policy rule
and SHA-256 plus the crawl timestamp. Each artifact has an `expires_at` value
30 days after its crawl timestamp. Startup creates the `expires_at_ttl` MongoDB
TTL index, so MongoDB removes expired artifacts asynchronously after that
finite retention period.

## Queue durability and deployment invariant

The spider publishes immutable
`page_data:<publication-sha256>:<base64url-normalized-url>` page hashes and
matching versioned outlinks sets. The indexer atomically claims the oldest
`pages_queue` item into `pages_queue:processing` with a one-second bounded wait.
Completion deletes only that exact publication, so completing crawl A cannot
read or delete a newer queued crawl B for the same canonical URL.

Each page also has an immutable `page_images:<publication-sha256>:<base64url>`
manifest, including explicit zero-image manifests. Completion and permanent
skip queue that manifest key (never the canonical URL), then clean only the
exact page and outlinks keys. Manifest and image payload hashes remain durable
until the image indexer ACKs them.

A claim remains durable until all required MongoDB writes are acknowledged.
Each indexable recrawl removes stale word postings and replaces canonical-URL
metadata and outlinks, including an explicit empty outlinks array. A permanent
non-indexable recrawl removes all prior words, metadata, and outlinks before its
Redis ACK. These operations are idempotent, and rendered artifacts remain in
their separate canonical-URL collection.

Completion Lua validates every key type, verifies ownership, checks claim
membership, allocates downstream image work, and only then removes the claim
and exact immutable inputs. This order avoids losing work if allocation fails;
normal retries cannot duplicate publication because the claim is already gone.

The application enforces one active indexer with a random owner-token Redis
lock (60-second TTL, renewed at least every 10 seconds and around Mongo work).
Every successful acquisition atomically increments a type-checked monotonic
fencing epoch. Canonical Mongo words, metadata, outlinks, rendered artifacts,
stale-word cleanup, and permanent deletion carry epoch guards that accept
legacy no-epoch documents but reject higher-epoch state; duplicate-key fencing
conflicts are retryable and never ACK Redis work.
Recovery, release, completion, skip, work inspection, and quarantine are
owner-fenced. An overlapping instance waits in 250ms bounded intervals. If the
lock expires and a replacement takes ownership, the old token cannot ACK,
clean, release, or delete the replacement's lock.

Every valid publication also atomically inserts its canonical URL into the
shared `canonical_url_ownership_locks` MongoDB collection before any canonical
write. The unique process/publication token and URL lock remain held through
the owner-fenced Redis completion, skip, or quarantine decision. These locks
deliberately have no TTL and are never stolen: a hard-killed process leaves a
fail-closed stale lock, and later epochs must not mutate that URL. Operators
must inspect the recorded service/publication/owner, verify that no matching
indexer remains, and reconcile the Redis processing claim and Mongo state
before manually deleting the exact stale lock. Never bulk-delete or age out
this collection. Epoch checks remain defense in depth; standalone Mongo is
supported without transactions.

Transient fetch, HTML decode, MongoDB, and Redis failures get one atomic claim
release attempt; the process then exits nonzero instead of retrying in memory.
If Redis cannot release the claim, it stays in the processing list for startup
recovery. Startup recovery runs in bounded 100-entry Lua batches. SIGTERM only
requests shutdown: claims use one owner-fenced nonblocking Lua `RPOPLPUSH` and
100ms polling (never a destructive blocking pop), while bounded
database/client timeouts allow exit without retry loops, and a claim acquired
during shutdown remains recoverable. Empty-queue crawler signaling requires
both the source and processing lists to be empty.

Queue values that do not strictly match the immutable publication-key format
are atomically moved to `pages_queue:dead` without being interpreted as Redis
keys; the dead-letter list retains at most 1,000 values. A valid-key notification
with an absent or malformed page hash is also quarantined and cannot modify
canonical Mongo state. Only a successfully decoded page that explicitly parses
as non-indexable may remove prior searchable state.

## English NLP security boundary

Indexer uses a reviewed, inference-only subset of NLTK 3.10.3 rather than the
full `nltk` dependency. The vulnerable model training/load/save APIs named by
`GHSA-8mgp-746c-j5xp` / `PYSEC-2026-3740` are physically absent. This is code
removal, not an upstream patch or an audit exception. Source provenance,
licenses, retained methods, adaptations, hashes, and maintenance ownership are
recorded in [`nlp/README.md`](nlp/README.md) and `nlp/component.json`.

Default English Punkt sentence splitting and `NLTKWordTokenizer` behavior are
preserved against the pinned reference. Lowercase/stopword/alphanumeric filtering,
10,000-character chunks, summaries, keyword frequencies, URL boosts, and claim
handling are unchanged. Missing or corrupt NLP data raises an error; it must
never become an empty successful result that deletes searchable state.

**Private builds/tests only; data-bearing image distribution is blocked.**
Upstream marks the English model/stopword packages' redistribution rights as
unclarified. Owner-approved private provisioning is not legal clearance. Corpus
bytes stay out of Git and Docker contexts; explicit setup fetches two immutable,
size/hash-verified archives and installs only five fixed plaintext files. Runtime
does not download, search user caches, fall back to installed NLTK, or load pickles.

From `services/indexer`, inside a development virtual environment:

```sh
NLP_CACHE="$(mktemp -d "${TMPDIR:-/tmp}/mifolyo-nlp.XXXXXX")"
python -B tools/vendor_nlp.py --cache "$NLP_CACHE" --fetch --provision-data
python -B verify_nlp.py
python -I -B -m unittest discover -s tests -p test_nlp_compatibility.py -v
```

The same cache can provision offline by omitting `--fetch`. Keep it private and
outside the repository. A clean checkout needs provisioning before NLP imports.
Use `-B` for native commands: source verification rejects unexpected bytecode.
Docker performs this setup during its private build, not at consumer startup.

The protected Indexer job keeps its dependency audit unchanged and additionally
requires source/data verification, negative security checks, and independent
offline compatibility tests. `pip-audit` does not inspect vendored code; these
are separate evidence layers. The release workflow runs
`python3 services/indexer/verify_nlp.py --source-only --distribution` before
Indexer registry login or push. That command must fail until data rights are
resolved and the distribution decision is separately reviewed.

Local worktree validation on 2026-09-14 passed all 54 Indexer tests with zero
skips using disposable Redis 7/MongoDB 8, plus 25 verifier mutation checks and
96 component/provisioning checks. The 34-case reference fixture protects tokens,
chunk boundaries, HTML extraction, and postings; it contains hashes and synthetic
examples, not corpus bytes. Reference regeneration and repeated offline parity
passed. The unmodified dependency audit reported no known vulnerabilities. These
results do not constitute protected-PR acceptance or deployment/crawl authority.

## Setup

The service-level Compose file is a deployment artifact and requires the exact
approved `MIFOLYO_INDEXER_IMAGE=ghcr.io/fullerkris/mifolyo/indexer@sha256:<64
lowercase hex>` reference from the reviewed release digest artifact. It must be
cut over with the matching Spider and Image Indexer digests; this queue protocol
does not permit a rolling or mixed-version deployment. Follow
`../../docs/immutable-pipeline-release-cutover.md`. Root Compose remains the local
source-build path.

### Using Docker

The recommended way to run the Indexer is with Docker. This ensures all dependencies are handled and the service runs in an isolated environment.

1. **Install Docker**:  
   Follow the instructions for your OS on the [Docker website](https://docs.docker.com/get-docker/).

2. **Configure Environment Variables**:  
   Create a `variables.env` file in the `services/indexer` directory with the following content (adjust as needed):
   ```env
    REDIS_HOST=<your_redis_host>
    REDIS_PORT=<your_redis_port>         # default: 6379
    REDIS_USERNAME=<your_redis_username> # optional for password-only Redis auth
    REDIS_PASSWORD=<your_redis_password>
    REDIS_DB=<your_redis_db>             # default: 0
    MONGO_HOST=<your_mongo_host>
    MONGO_PORT=<your_mongo_port>         # default: 27017
    MONGO_DB=<your_mongo_db>
    MONGO_USERNAME=<your_mongo_username>
    MONGO_PASSWORD=<your_mongo_password>
    ```

    MongoDB username/password values must be supplied together. Redis requires
    a password; an ACL username may accompany it. Missing authentication fails
    startup by default. Only an isolated local test deployment may explicitly
    set `ALLOW_INSECURE_DATASTORES=true`; the exact lowercase value is required.
    Do not set this flag in production or shared environments.
3. **Pull and Run a Release**:
   After completing the runbook stop/drain/backup gates, use the reviewed
   downloaded digest env file with the service Compose file:
    ```bash
    docker compose --env-file release-image.env pull
    docker compose --env-file release-image.env up -d --no-build
    ```

### Without Docker

If you prefer not to use Docker, you can run the Indexer directly on your machine. Ensure you have all dependencies installed. It is recommended to use a virtual environment to avoid conflicts with other Python packages.

1. **Install Dependencies**:  
   Install the required packages using `pip`:
   ```bash
   pip install -r requirements.txt
   ```
2. **Configure Environment Variables**:  
   Set up the environment variables in your shell or create a `.env` file in the `services/indexer` directory.
3. **Run the Indexer**:  
   Execute the Indexer script:
   ```bash
   python indexer.py
   ```
