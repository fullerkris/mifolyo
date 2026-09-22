# Backlinks Processor

> [!IMPORTANT]
> **Status — current V1; F5 code accepted.** PR #9 passed all 14 protected checks,
> including `required-tests`, and merged as `d914a93`. That acceptance covers the
> merged F5 repair, not retained-datastore reconciliation, Crawl Jobs V2
> integration, or unrelated current worktree changes. F3 runtime/consumer
> integration has not started; these procedures are not V2 wiring or
> authorization. See the
> [parent remediation plan](../../docs/spider-render-remediation-plan-2026-09-01.md)
> and [F3 implementation plan](../../docs/crawl-jobs-v2-plan.md).

> [!CAUTION]
> Do not start the processor or apply reconciliation under the current F3 gates.
> Runtime and write examples below are retained for separately approved,
> isolated V1 development only. Unit and disposable-datastore tests remain the
> local evidence path; they do not authorize operation.

The Backlinks Processor maintains MongoDB's additive historical `backlinks`
projection from Redis sets produced by the Spider. MongoDB `outlinks` remains
the authoritative graph used by PageRank.

## Persistence Contract

For each `backlinks:<canonical-target-url>` set, the processor:

1. Uses cursor-based `SCAN` and `SSCAN`; their `COUNT` values are hints, not
   point-in-time snapshot or response-size guarantees.
2. Admits at most 256 exact raw set members and 512 KiB of encoded URL data to
   one batch. Canonical URLs are limited to 2,048 bytes.
3. Preflights the resulting MongoDB document below the 12 MiB operational
   ceiling and performs an insert-only creation or exact-document-CAS
   `$addToSet` update with majority write concern.
4. Calls `SREM` only after MongoDB acknowledges the batch, and only for the raw
   members admitted to that batch. It never deletes the whole Redis key.

A MongoDB failure leaves Redis unchanged. A crash after MongoDB acknowledgment
but before `SREM` safely replays because `$addToSet` is idempotent. Members added
after a snapshot remain pending. Rejected data stays in Redis and logs only a
stable reason and hashed work reference.

### Buffer bounds

Partial SSCAN pages and retry snapshots share an 8 MiB raw-member byte budget.
Before scanning, the processor reserves a full response (at most 1,024 members
and 2 MiB), prioritizes the first waiting scan, and drains buffered work under
backpressure with interruptible retry waits. Overlarge responses leave both
the cursor and Redis unchanged. This bounds retained raw data, not Python/MongoDB
overhead or a Redis response's transient allocation before validation.

## Configuration

Set these environment variables:

```env
REDIS_HOST=<redis-host>
REDIS_PORT=6379
REDIS_USERNAME=<redis-username>
REDIS_PASSWORD=<redis-password>
REDIS_DB=0
MONGO_HOST=<mongo-host>
MONGO_PORT=27017
MONGO_DB=mifolyo_index
MONGO_USERNAME=<mongo-username>
MONGO_PASSWORD=<mongo-password>
```

Authentication is fail-closed. Passwordless datastores require the exact local
test opt-in `ALLOW_INSECURE_DATASTORES=true`; do not set it in production.
Connections and operations use bounded timeouts.

## Local V1 run example (currently blocked)

```bash
python -m pip install --requirement requirements.txt
python main.py
```

The processor handles `SIGINT` and `SIGTERM`, finishes an already-started
MongoDB/Redis acknowledgment sequence, and otherwise exits at a bounded scan or
backoff boundary.

## Local V1 reconciliation (apply currently blocked)

Run the offline report with the Spider, Indexer, and Backlinks Processor
stopped:

```bash
python reconcile.py
```

The report compares authoritative `outlinks` edges to the additive historical
projection and reports missing and extra historical edges. Report mode performs
no writes and exits `2` while current outlink edges are missing.

After a matched backup and operator review, add only missing reverse edges by
confirming the exact configured database:

```bash
python reconcile.py --apply --confirm-database mifolyo_index
```

Reconciliation never removes extra historical backlinks. Run report mode again
afterward and require `missing_after=0`.

## Tests

```bash
python -m unittest discover -s tests -v
```

Set `BACKLINKS_REDIS_INTEGRATION_ADDR` and
`BACKLINKS_MONGO_INTEGRATION_URI` to **new disposable datastores** to run the
datastore acceptance tests. Redis addresses and URIs default to DB15; an explicit
different database is rejected before connecting. Each test processes only its
UUID-namespaced Redis targets and uses a unique MongoDB database. Cleanup removes
only test-owned keys and that database. Never point these tests at retained
evidence or production. CI sets both values and fails if a test is skipped.
