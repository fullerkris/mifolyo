# Crawl Jobs V2

## Status and normative language

This document is the normative contract for MiFolyo Crawl Jobs V2. An
implementation is conformant only when its code, Redis scripts, deployment
configuration, operational procedures, and tests satisfy this document.

The terms **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are to
be interpreted as normative requirements. All text values are UTF-8 Redis bulk
strings unless stated otherwise.

Crawl Jobs V2 is an at-least-once Redis job ledger. It replaces the V1 crawl
queue; it does not add a reader for V1 state. The existing immutable page and
image publication grammar remains the downstream contract for the minimum
compatible change.

This preactivation V2 amendment binds staging to a complete, frozen request-start
interval. It retains `protocol_version=2`, the existing hash domain labels, and
the semantic output/publication and downstream contracts. It adds no operation
or key family and changes no Lua response arity. Implementations MUST use the
exact record, request, projection, and digest shapes in this document; missing
fields, earlier shapes, and the earlier commit formula MUST NOT be accepted
through a backward fallback. The changed exact document bytes require
regeneration and independent
review of the contract digest and affected bindings/vectors under section 4.
This amendment does not authorize Lua authoring, runtime integration or
activation, migration, deployment, or crawling; those remain separately gated.

## 1. Scope and invariants

Crawl Jobs V2 MUST provide all of the following:

1. An explicit, authorization-bound crawl run.
2. Durable `ready`, `leased`, `delayed`, `completed`, `dead`, and `cancelled`
   job dispositions.
3. Expiring owner/token/fence leases.
4. A durable distinction between a job delivery attempt and each outbound
   request start made during that delivery.
5. Run-scoped global and per-policy-group request-start budgets.
6. Global, group, and per-origin concurrency/rate state shared by every run and
   Spider replica.
7. Request capacity reserved before a destructive claim, and a durable request
   start recorded before DNS or any other network I/O.
8. Token-scoped, TTL-bound output staging.
9. One lease-fenced atomic commit that publishes the page, outlinks, every
   image payload and manifest, backlinks, discovered jobs, and the exact page
   notification, then completes and ACKs the crawl job.
10. Bounded retries, maintenance, staging, command size, and Redis script time.
11. A stopped V1 migration and a stop-and-drain cutover; never mixed V1/V2
    operation.

At all times:

- A job MUST be represented in exactly one primary state index.
- A terminal job MUST have exactly one stable disposition: `completed`, `dead`,
  or `cancelled`.
- A stale worker MUST NOT renew, start a request, release live capacity, retry,
  publish, complete, dead-letter, or cancel a newer lease.
- Client time MUST NOT determine leases, retries, authorization expiry, rate
  deadlines, or state timestamps. Lua transitions MUST use Redis `TIME`.
- Raw values necessarily travel on the authenticated Redis wire. Canonical
  URLs, HTML, lease tokens, reservation IDs, authorization contents, and
  arbitrary stored values MUST NOT be rendered into application logs,
  telemetry, traces, exception text, or Lua error replies. Redis `MONITOR` and
  command-argument logging MUST be unavailable to service credentials.
- At most four stage slots may exist. A stage slot is reserved by
  `CJ2_BEGIN_STAGE` and released only by commit, the lease-ending transition
  immediately after abort, or expired-lease recovery.
- Publication requires a complete authenticated request-start interval for the
  exact lease. Successful `CJ2_BEGIN_STAGE` atomically compares and freezes that
  interval; `last_stage_fence` prevents any further request reservation or start
  on that fence, including after abort or stage-key removal.
- A run creates at most 100 unique reservation records. Creation, cancellation,
  expiry, tombstone deletion, and replay never restore that finite capacity.

## 2. Deployment assumptions

### 2.1 Redis topology

Crawl Jobs V2 supports one authoritative Redis 7 standalone, non-cluster
instance. Redis Cluster and automatic Redis failover are unsupported because
the required atomic commit touches existing output and backlink keys that do
not share a Redis Cluster hash slot.

Startup MUST fail unless `INFO cluster` reports `cluster_enabled:0`. Enabling
Redis Cluster requires a new protocol and a coordinated output-key migration;
it MUST NOT be treated as a deployment-only change.

The instance admits no unreviewed writer. Any non-crawl service that can allocate
on it must be included in the retained/downstream fixtures, ACL review,
compatibility manifest, and common memory-admission inequality; otherwise it
uses a separate Redis instance. An unrelated writer may not consume the commit
or lease-safety reserve.

### 2.2 Persistence and acknowledged-write loss

The authoritative Redis MUST use:

```text
appendonly yes
appendfsync always
aof-use-rdb-preamble yes
aof-load-truncated no
no-appendfsync-on-rewrite no
maxmemory-policy noeviction
stop-writes-on-bgsave-error yes
```

Redis data MUST be stored on a persistent volume. The deployment MUST reserve
memory using the following V2.0 constants and formula:

```text
STAGE_MEMORY_RESERVATION_BYTES = 50,331,648       # 48 MiB per stage
STAGE_CONTROL_RESERVATION_BYTES = 65,536          # held inside each stage slot
TERMINAL_CONTROL_FLOOR_BYTES = 32,768              # never spent by backpressure
COMMIT_MEMORY_RESERVATION_BYTES = 67,108,864      # 64 MiB
LEASE_SAFETY_RESERVATION_BYTES = 16,777,216       # 16 MiB
MAX_STAGE_SLOTS = 4

required_maxmemory =
  retained_state_budget_bytes +
  downstream_backlog_budget_bytes +
  (MAX_STAGE_SLOTS * STAGE_MEMORY_RESERVATION_BYTES) +
  COMMIT_MEMORY_RESERVATION_BYTES +
  LEASE_SAFETY_RESERVATION_BYTES
```

The first two budgets MUST be measured from the maximum approved retained-run
and queue fixtures, rounded up to MiB, recorded in the release sizing artifact,
and checked at startup. Together they must upper-bound the fixture's complete
pre-stage Redis `used_memory`, not only serialized payload bytes or allocation
deltas; this includes the empty-process baseline, allocator/fragmentation
allowance, global control/rate/reservation/tombstone state, and every other
non-stage key admitted on the instance. No Redis allocation may be left as an
unbudgeted third category. That artifact also records exact per-queue item and
byte bounds for every downstream source, processing, and dead-letter list; these
bounds govern consumer admission and alerts even where Crawl Jobs does not write
the queue directly. The sizing artifact also proves that the cumulative maximum
persistent growth of every safety transition needed to drain all protocol-
bounded live leases/reservations is no more than the 16 MiB safety reserve.
Its retained-state fixture includes 100 reservation records for each of the 100
maximum unarchived runs. Because a run can create at most 100 reservations and a
replay never creates another, at most 100 terminal reservation tombstones can
exist logically for one run and at most 10,000 across all unarchived runs;
candidate runs count toward the same bound. Archive cannot occur until long
after the 24-hour tombstone TTL. Lazy-expiration memory not yet physically freed
remains visible in `used_memory` and receives no sizing credit.
`maxmemory` MUST be nonzero and at least
`required_maxmemory`. The Redis container limit MUST be at least
`maxmemory + 128 MiB`. For the isolated acceptance stack the retained-state and
downstream budgets together MUST NOT exceed 128 MiB, `maxmemory` MUST be at
least 400 MiB, and the container limit MUST be at least 528 MiB.

`mifolyo:crawl:v2:stage_slots` records the remaining portion of each initially
48 MiB stage reservation and its bounded owner tuple.
In every inequality, `all_unmaterialized_stage_reservations` is the exact sum of
the current `remaining_bytes` values in that at-most-four-field hash, including
the prospective script's own slot when it already exists.
For a prospective write, scripts calculate a conservative allocation bound:

```text
G = 3 * new_logical_bytes
    + 1024 * new_redis_keys
    + 256 * new_hash_fields_set_members_zset_members_and_list_elements
```

`new_logical_bytes` is the sum of UTF-8 byte lengths of newly allocated key
names, field names, values, members, and canonical score text; overwriting an
identical value contributes zero. `CJ2_BEGIN_STAGE` requires:

```text
used_memory + existing_unmaterialized_stage_reservations
            + STAGE_MEMORY_RESERVATION_BYTES
            + COMMIT_MEMORY_RESERVATION_BYTES
            + LEASE_SAFETY_RESERVATION_BYTES <= maxmemory
```

Every other allocating V2 mutation outside commit/stage materialization requires:

```text
used_memory + all_unmaterialized_stage_reservations
            + COMMIT_MEMORY_RESERVATION_BYTES
            + LEASE_SAFETY_RESERVATION_BYTES + G <= maxmemory
```

The same routine inequality applies to every allocation-capable Redis script in
the pinned Indexer, Image Indexer, Backlinks Processor, feeder, Spider, and
crawl-admin images, including owner-lock creation and queue handoff/dead-letter
paths. Queue cardinality bounds alone do not reserve memory. A deletion-only
consumer path needs no `G`, but a move/handoff may not credit bytes it plans to
delete when proving a new allocation. `CJ2_APPROVE_BOOT` remains the isolated
section 10.2 exception that may call `TIME`/`INFO` and inspect only the
durability hash.

Deletion-only portions do not claim negative growth. This keeps the commit and
lease-safety reserves unavailable to routine ledger growth.

Every stage data/seal write requires
`used_memory + all_unmaterialized_stage_reservations +
COMMIT_MEMORY_RESERVATION_BYTES + LEASE_SAFETY_RESERVATION_BYTES <= maxmemory`,
plus `G` not greater than `remaining-STAGE_CONTROL_RESERVATION_BYTES`, and
atomically subtracts `G`; the
control reserve is available only to abort, its following lease-ending
transition, recovery, and the first bounded backpressure record; that
record must leave the terminal-control floor.
An abort, recovery, or lease-ending transition backed by an aborted stage slot
computes its own `G`, requires it to fit the current control
remainder, and atomically subtracts it; deletion contributes no negative growth.
Abort retains the resulting slot reservation until the immediately following
lease-ending transition removes it. After a backpressure record, the aggregate
`G` of abort plus that transition, or of one recovery transition, may spend no
more than the retained 32 KiB floor. Because materialization is covered by the
matching slot, these paths use `G <= remaining` instead of adding `G` again to
the ordinary inequality; removing the slot occurs only after all covered writes.
The release sizing artifact records the exact maximum abort `G` and the maximum
post-abort `G` across retry/dead/cancel/no-output lease endings and expiry
recovery, and proves their sum is at most 32 KiB. Abort requires
`remaining_before >= actual_abort_G + maximum_post_abort_outcome_G` and stores
the remainder; the selected following transition or recovery independently
requires its actual `G` to fit that remainder.
Stage cleanup runs only after commit or recovery has removed the slot and is
deletion-only.

Sizing MUST include `lease_request_starts_baseline` in every new-job allocation
and retained-job fixture, including jobs created by discovery at commit, and
any allocation from replacing its value on a new claim. `G_begin` includes both
`request_starts_baseline` and `request_starts_generation` in stage metadata.
Their field-name/value bytes and hash-field overhead are not free bookkeeping.
The additional BEGIN arguments also count toward the exact RESP command bound.
These fields add no key or downstream payload field; metadata remains excluded
only from the logical `data_bytes` limit, not from memory admission. All existing
memory reservations, control floors, key limits, and command limits remain
unchanged and require evidence for the amended shapes.

The safety reserve is available only to `CJ2_FINISH_REQUEST`,
`CJ2_CANCEL_RESERVATION`, `CJ2_RELEASE_BEFORE_IO`, non-stage job outcomes,
`CJ2_RECOVER_EXPIRED`, and the constant-size `CJ2_CANCEL_RUN` transition when
the ordinary inequality would fail. Such a path requires its complete `G` to
satisfy:

```text
used_memory + all_unmaterialized_stage_reservations
            + COMMIT_MEMORY_RESERVATION_BYTES + G <= maxmemory
```

It therefore spends safety headroom but never commit headroom. No ordinary
allocation or first commit may proceed until the full 16 MiB safety reserve is
restored. The maximum-64 global lease invariant, two-request global concurrency,
at-most-16 active runs, and release fixture MUST prove that all listed safety-
transition growth fits cumulatively within that reserve.
A mixed recovery batch precomputes each job's growth: a matching stage/aborted
slot covers only that job's portion, while the aggregate for jobs without a slot
uses the safety inequality. No bytes are charged to both reserves.
A first commit calculates the same bound over all final key names, new discovery
records/index members, new backlink members, and the queue element. Renamed
payload bytes contribute zero, but both old and new key-name bytes contribute.
It requires `G <= COMMIT_MEMORY_RESERVATION_BYTES` and:

```text
used_memory + all_unmaterialized_stage_reservations
            + LEASE_SAFETY_RESERVATION_BYTES + G <= maxmemory
```

`used_memory` and `maxmemory` come from one pre-mutation `INFO MEMORY` result
obtained by the script; `CONFIG` is a no-script command and MUST NOT be called
from Lua. The release memory fixture MUST prove that
actual `used_memory` growth never exceeds `G`. Any failed inequality is
rejected before consumer-visible mutation using the operation's defined blocked
status or `MEMORY_HEADROOM_LOW`; no script may opt into `allow-oom` execution.

The accepted loss bound for an acknowledged request-start or shared-rate
transition is **zero acknowledged writes** in the tested Redis process-crash
model. A mutation whose response is lost is ambiguous, not acknowledged, and
MUST be reconciled using its idempotency identity. A deployment MUST NOT claim
protection from storage-controller, filesystem, or host failure beyond its
recorded restore rehearsal.

`PING` is liveness only. It is not a durability or protocol readiness check.
Startup MUST additionally verify `cluster_enabled:0`, every exact configuration
value above, `aof_enabled:1`, `aof_last_write_status:ok`, a successfully loaded
multipart AOF manifest, and absence of any truncated-AOF recovery warning. With
`aof-load-truncated no`, a truncated or corrupt AOF MUST prevent Redis from
becoming ready; an operator MUST NOT repair, truncate, or regenerate it in
place. Restore or an explicitly rehearsed recovery is required.

This deliberately trades write throughput and automatic availability for one
atomic keyspace and the tested process-crash loss bound. Operators scale Spider
workers only within the measured Redis/AOF budget; they do not add replicas,
failover, or relax fsync as an unreviewed performance fix.

### 2.3 Redis restart approval

The durability key is:

```text
mifolyo:crawl:v2:durability  HASH
```

It has these fields:

| Field | Value |
|---|---|
| `schema_version` | `1` |
| `boot_state` | `approved`, `planned`, or `unapproved` |
| `approved_redis_run_id` | Exact lowercase 40-hex Redis `INFO server` run ID |
| `boot_epoch` | 32 lowercase hexadecimal characters |
| `approved_at_ms` | Redis time |
| `planned_shutdown_nonce` | Empty or 32 lowercase hexadecimal characters |
| `planned_shutdown_evidence_sha256` | Empty or exact stopped-process evidence digest |
| `last_approval_mode` | `initial`, `planned`, or `unclean_rehearsal` |
| `consumed_planned_shutdown_nonce` | Matching consumed nonce or empty |
| `rehearsal_evidence_sha256` | Lowercase SHA-256 |
| `rehearsal_at_ms` | Redis time recorded by the approval tool |
| `acknowledged_loss_bound` | Exactly `0` |

In `planned` boot state, the planned nonce and evidence digest are nonempty and
the consumed nonce is empty. Successful `planned` approval clears the pending
nonce, copies it to the consumed field, preserves its process-stop evidence, and
sets `last_approval_mode=planned`. `initial` and `unclean_rehearsal` approval
store both planned fields and the consumed nonce empty. A later mark replaces
the prior planned evidence and clears the prior consumed nonce atomically.

Every runtime client MUST inspect `INFO server` on every new TCP connection,
including transparent pool reconnects. It MUST issue no mutation unless the
current Redis run ID equals `approved_redis_run_id`, `boot_state=approved`, and
the mounted durability evidence digest equals `rehearsal_evidence_sha256`.
Every runtime and candidate-mode mutating Lua script also receives and validates
the current `boot_epoch` and independently compares the actual `INFO SERVER`
run ID with the approved hash before its first write.

`CJ2_APPROVE_BOOT` is the sole unapproved-boot exception. It is executable only
by the boot-admin credential, obtains the actual run ID from `INFO server`, and
calls `TIME` exactly once, and may read or mutate only the durability hash. It
does not require either
contract-marker pair. The random proposed boot epoch is supplied by the
approval tool; Lua does not generate randomness. All other administrative
exceptions are finite and are defined in section 10.2.

A planned Redis restart requires all Redis-mutating producers and consumers to
be stopped, all owner locks, active request reservations, leases, stage slots,
and stage-cleanup residue to be zero, and `CJ2_MARK_PLANNED_SHUTDOWN` to record a
single-use nonce. The new Redis process still requires explicit boot approval
before clients start.

A changed Redis run ID without the matching planned-shutdown nonce is an
unclean restart. It MUST remain `unapproved` until an operator supplies evidence
from a successful, current abrupt-restart rehearsal showing zero acknowledged
request/rate-state loss. Rehearsal evidence expires after 30 days. Normal
Spider, feeder, consumer, and Monitoring credentials MUST NOT be able to approve
a boot.

`boot_state` is persisted evidence, not an automatic restart detector. Whenever
the actual Redis run ID differs from `approved_redis_run_id`, the effective boot
state is `unapproved` regardless of the stored field. In that state only
`CJ2_APPROVE_BOOT` may replace the stale approval: `planned` mode additionally
requires the persisted matching nonce, while an unplanned restart requires
`unclean_rehearsal` mode and current evidence.

## 3. Fixed protocol limits

| Limit | Value |
|---|---:|
| Lease TTL | 60,000 ms |
| Lease renewal interval | 10,000 ms |
| Maximum delivery attempts | 3 |
| Delay after failed delivery attempt 1 | 30,000 ms |
| Delay after failed delivery attempt 2 | 120,000 ms |
| Maximum pre-I/O expired-lease recoveries | 3 |
| Maximum request starts per run | 10 |
| Maximum reservation creations per run | 100 |
| Maximum request starts per group | Policy value, from 1 through 10 |
| Current global active-request limit | 2 across all runs |
| Maximum supported scope concurrency | 32 |
| Maximum supported scope interval | 3,600,000 ms |
| Global scope interval | Exactly 0 ms |
| Maximum policy groups per run | 64 |
| Maximum policy group ID | 128 UTF-8 bytes |
| Maximum render-policy rule ID | 128 UTF-8 bytes |
| Maximum durable rate scopes | 100,000 |
| Maximum active leases across all runs | 64 |
| Maximum jobs per run | 10,000 |
| Maximum active runs | 16 |
| Maximum retained, unarchived runs | 100 |
| Maximum total run records pending purge | 128 |
| Maximum `pages_queue` depth before first commit | 5,000 |
| Feeder enqueue batch | 500 jobs |
| Run audit batch | 100 jobs |
| Ready metadata page | 128 jobs |
| Maintenance batch | 100 jobs or keys |
| Maximum outlinks per job | 256 |
| Maximum discoveries per job | 128 |
| Maximum images per page | 64 |
| Maximum total document/effective aliases per job | 5 |
| Maximum canonical URL | 2,048 UTF-8 bytes |
| Maximum page or rendered DOM | 5 MiB each |
| Maximum combined `html` and `original_html` | 10 MiB |
| Maximum image alt | 1,024 UTF-8 bytes |
| Maximum serialized image manifest | 393,216 bytes (384 KiB) |
| Stage TTL | 900,000 ms (15 minutes) |
| Committed residual-stage cleanup TTL | At most 60,000 ms |
| Commit backpressure deadline | At most 120,000 ms and 10,000 ms before stage expiry |
| Maximum simultaneous stage slots | 4 |
| Stage memory reservation | 50,331,648 bytes (48 MiB) |
| Reserved stage-control portion | 65,536 bytes (64 KiB) |
| Terminal stage-control floor | 32,768 bytes (32 KiB) |
| Commit memory reservation | 67,108,864 bytes (64 MiB) |
| Lease safety reservation | 16,777,216 bytes (16 MiB) |
| Maximum stage aggregate logical data | 14,680,064 bytes (14 MiB) |
| Maximum stage keys | 73 |
| Reservation tombstone TTL | 86,400 seconds |
| Maximum authorization window | 24 hours from run creation |
| Minimum authorization time remaining at activation | 60 seconds |
| Maximum ordinary serialized `EVALSHA` request | 2 MiB |
| Maximum page-blob `EVALSHA` request | 5,373,952 bytes (5.125 MiB) |
| Maximum commit `EVALSHA` request | 64 KiB |
| Maximum non-blob stage batch | 64 records and 512 KiB serialized request |
| Maximum accepted Lua p99 at maximum approved shape | strictly below 100 ms |

Serialized request size means the complete RESP command bytes for `EVALSHA`,
including script SHA-1, key count, every key/argument, length prefixes, and CRLF
overhead. Client-library object estimates are not the acceptance measurement.

Integers stored or processed by Lua MUST be canonical unsigned decimal strings
from `0` through `9007199254740991`. Positive identifiers such as a fence MUST
be from `1` through `9007199254740991`. Scores MUST be finite values in the
inclusive range `[-1000, 10000]`, represented in hashes and digest inputs by
canonical `score_text` matching exactly
`0|-?(?:[1-9][0-9]*(?:\.[0-9]{0,5}[1-9])?|0\.[0-9]{0,5}[1-9])`.
Thus fractions such as `0.5` and `-0.5` are valid, while trailing fractional
zeroes are removed. Leading zeroes, exponent notation, `+`, `-0`, NaN, and
infinities are forbidden. ZSET scores are accepted only when parsing their
stored Redis value and the corresponding `score_text` produces the same finite
binary64 value. All limits reject the whole operation; output is never
truncated to fit.

`redis_now_ms` is exactly `TIME.seconds*1000 + floor(TIME.microseconds/1000)`.
Scripts validate that the result and every derived deadline remain within the
exact integer range before mutation; sub-millisecond precision is discarded.

If the maximum approved output shape cannot meet the 100 ms p99 script gate on
the target Redis configuration, the release MUST lower one or more input bounds,
regenerate the contract and compatibility digests, and rerun all tests. It MUST
NOT raise Redis's Lua time limit, split externally visible commit effects, or
waive the gate.

The p99 gate is measured over at least 1,000 isolated maximum-shape invocations
from complete client command write through complete reply read on the target
Redis/container/storage configuration, including `appendfsync always`. The
artifact also records Redis command CPU time separately; warmup, failures, and
blocked-AOF samples are reported rather than discarded.

The section 5.1 disposable acceptance fixture is the only environment in which
real-Redis maximum-shape evidence may run before final markers exist. A real
candidate compatibility marker may be installed only after those benchmarks and
all other evidence gates have passed and every provisional sentinel has been
replaced in newly hashed final artifacts. If the p99 is 100 ms or greater, no
candidate may be installed and implementation constants MUST NOT silently differ
from this table.

## 4. Identifiers and digests

| Name | Exact form |
|---|---|
| `run_id` | `[0-9a-f]{32}`, generated from 128 cryptographically random bits |
| `job_id` | `[0-9a-f]{64}`; equal to the run-pinned canonical URL ID |
| `url_id` | Same value as `job_id` |
| `owner_id` | `[0-9a-f]{32}`, one random value per Spider process start |
| `lease_token` | `[0-9a-f]{64}`, 256 random bits per claim attempt |
| Boot, freeze, or planned-shutdown nonce | `[0-9a-f]{32}`, 128 random bits |
| `fence` | Positive per-job monotonic integer |
| `transition_id` | Lowercase SHA-256 defined below; binds operation payload and reason |
| `reservation_id` | Lowercase SHA-256 defined below; binds the exact outbound intent |
| `token_digest` | Lowercase SHA-256 defined below |
| `target_digest` | Lowercase SHA-256 defined below |
| `scope_id` | Lowercase SHA-256 defined below |
| `rate_scope_id` | `[0-9a-f]{32}` immutable policy lineage from crawl-policy V2 |
| `chunk_digest` | Lowercase SHA-256 of one exact stage-data request payload |
| `output_digest` | Lowercase SHA-256 of the canonical staged output |
| `publication_id` | Lowercase SHA-256 defined below |
| `commit_id` | Lowercase SHA-256 defined below |
| Contract/policy/evidence digest | `[0-9a-f]{64}` over exact artifact bytes |

For canonicalization version `1`, `job_id`, `url_id`, and every
`target_url_id` are exactly the URL ID defined by
[`url-canonicalization-v1.md`](url-canonicalization-v1.md):

```text
sha256(exact_bytes("mifolyo-url:v1\0") || exact_UTF8_bytes(canonical_url))
```

Here `\0` is one zero byte. This existing URL-ID formula deliberately does not
use `F`; changing it requires a new canonicalization version and a stopped
migration rather than a Crawl Jobs implementation detail.

All protocol hashes in this section use this binary encoding:

```text
U64(n)  = n as an unsigned 64-bit big-endian integer
F(x)    = U64(byte_length(x)) || exact_bytes(x)
RECORD(fields...) = U64(field_count) || F(field_name_1) || F(value_1) || ...
SECTION(label, records...) = F(label) || U64(record_count) ||
                             F(RECORD(record_1)) || ... || F(RECORD(record_n))
```

Field names, section labels, domain labels, identifiers, and canonical numbers
are ASCII. Other values are exact UTF-8 bytes. Every variable element, including
an encoded record, is framed with `F`; counts are `U64`. No delimiter or native
integer encoding may be substituted.

The exact identity hashes are:

```text
global scope = sha256(F("mifolyo:rate:global:v2"))
group scope  = sha256(F("mifolyo:rate:group:v2") || F(rate_scope_id))
origin scope = sha256(F("mifolyo:rate:origin:v2") || F(canonical_origin))

target_digest = sha256(
  F("mifolyo:request-target:v2") || F(target_url_id) || F(canonical_target_url))

token_digest = sha256(
  F("mifolyo:lease-token:v2") || F(run_id) || F(job_id) ||
  F(canonical_decimal(fence)) || F(lease_token))

transition_payload_digest = sha256(
  F("mifolyo:transition-payload:v2") || SECTION("arguments", one record whose
  fields are the ordered semantic names/values defined below))

transition_id = sha256(
  F("mifolyo:crawl-transition:v2") || F(operation_name) || F(run_id) ||
  F(job_id) || F(canonical_decimal(fence_or_zero)) || F(token_or_empty) ||
  F(reason_or_none) || F(transition_payload_digest))

reservation_id = sha256(
  F("mifolyo:request-reservation:v2") || F(run_id) || F(job_id) ||
  F(canonical_decimal(fence)) || F(lease_token) ||
  F(canonical_decimal(request_ordinal)) || F(request_kind) ||
  F(target_url_id) || F(target_digest) || F(crawl_policy_sha256) ||
  F(policy_decision_sha256) || F(group_id) || F(rate_scope_id) ||
  F(global_scope_id) || F(group_scope_id) || F(origin_scope_id) ||
  F(canonical_decimal(global_concurrency)) || F("0") ||
  F(canonical_decimal(group_concurrency)) || F(canonical_decimal(group_interval_ms)) ||
  F(canonical_decimal(origin_concurrency)) || F(canonical_decimal(origin_interval_ms)))

publication_id = sha256(
  F("mifolyo:page-publication:v2") || F(run_id) || F(job_id) ||
  F(canonical_decimal(fence)) || F(output_digest))

commit_id = sha256(
  F("mifolyo:crawl-commit:v2") || F(run_id) || F(job_id) ||
  F(canonical_decimal(fence)) || F(lease_token) || F(publication_id) ||
  F(canonical_decimal(request_starts_baseline)) ||
  F(canonical_decimal(request_starts_generation)))

chunk_digest = sha256(
  F("mifolyo:stage-chunk:v2") || F(commit_id) || F(chunk_kind) ||
  F(canonical_decimal(chunk_ordinal)) ||
  SECTION("records", operation-specific records in submitted canonical order))

policy_decision_sha256 = sha256(
  F("mifolyo:policy-decision:v2") ||
  SECTION("decision", one record with ordered fields request_kind, target_url_id,
  target_digest, depth, group_id, rate_scope_id, global_scope_id,
  group_scope_id, origin_scope_id, global_concurrency, global_interval_ms,
  group_concurrency, group_interval_ms, origin_concurrency,
  origin_interval_ms))

policy_group_map_sha256 = sha256(
  F("mifolyo:policy-group-map:v2") || SECTION("groups", records in group-ID
  byte order with ordered fields group_id, rate_scope_id, group_scope_id,
  request_start_limit, concurrency, interval_ms))
```

The commit's `request_starts_baseline` and `request_starts_generation` are the
authenticated interval certified under section 8.3 and frozen by BEGIN under
section 10.5. They MUST satisfy
`0 <= request_starts_baseline < request_starts_generation <= 10` before commit
identity derivation. The unchanged `mifolyo:crawl-commit:v2` domain now has those
two additional framed canonical-decimal inputs in the exact order above.
Different valid intervals with the same semantic output/publication therefore
have different commit IDs and stage-key identities. Chunk digests and abort
transition payload/transition digests change through their existing commit-ID
inputs; their formulas do not acquire separate interval fields. Reservation,
policy, source, output, publication, and token digest formulas are unchanged.

`request_kind` is exactly `robots`, `document`, `redirect`, or
`render_resource`. All numeric values in the decision and group-map records are
canonical decimal. Source and discovery job bindings use `request_kind=document`;
an outbound reservation decision uses its actual request kind. A blob chunk's
one record has ordered fields `field_name`
and `field_bytes`; other chunk record fields are exactly those defined by its
stage operation. A transition that has no lease uses fence `0` and an empty
token. `reason_or_none` is always present. The corresponding transition section
defines each payload field order. Starting from the logical operation request,
payload fields exclude boot/marker transport gates, `run_id`, `job_id`, `fence`,
`lease_token`, `reason`, and `transition_id`; every other field, including
`owner_id`, remains in request order. An operation with none uses one zero-field
record. A changed semantic argument or reason therefore has a different
transition ID without a recursive transition-ID input.

The canonical origin is `scheme://ASCII-host:effective-port`, with the default
port included. Origins are stored only where needed for policy validation and
MUST NOT be logged.
For every request/source/discovery policy check, Lua derives that origin from
the submitted canonical URL's scheme and authority under canonicalization V1,
adding port `80` for HTTP or `443` for HTTPS when the canonical URL omits the
default port. It rejects user information, a malformed authority, or a literal
IP target, then recomputes the origin scope ID; it never trusts a caller-selected
origin scope or performs DNS.

The output digest is SHA-256 over `F("mifolyo:crawl-output:v2")` followed by
these five typed sections in this exact order:

1. `SECTION("page", one record)`, whose ordered fields are `normalized_url`,
   `html`, `original_html`, `content_type`, `status_code`, `last_crawled`,
   `rendered`, `render_policy_rule`, and `render_policy_sha256`;
2. `SECTION("outlinks", records byte-sorted by canonical target URL)`, each
   with the one field `target_url`;
3. `SECTION("images", records byte-sorted by canonical source URL)`, each with
   `normalized_source_url` and `alt`;
4. `SECTION("discoveries", records byte-sorted by job ID)`, each with `job_id`,
   `canonical_url`, `depth`, `score_text`, `group_id`, `rate_scope_id`, and
   `policy_decision_sha256`;
5. `SECTION("aliases", records byte-sorted by URL ID)`, each with `url_id`,
   `canonical_url`, and `depth`.

The aliases section contains one deduplicated record for the job's original
canonical URL and for every distinct validated canonical document/redirect
target in the successful chain, including the final effective URL. It excludes
robots and render-resource targets, contains from one through five records, and
uses the job's exact depth for every record. Each URL ID is recomputed with the
run-pinned canonicalization contract; a same-ID/different-URL pair is a
collision, not an alias merge.

The digest excludes the request-start baseline/generation as well as every
derived `publication_id` field, final Redis key, image manifest field, backlink,
queue notification, and other deterministic projection. This removes the
publication-ID cycle: semantic output is digested first, then `publication_id`
is derived, then `commit_id` additionally binds the authenticated request-start
interval, and only then are the final-shaped page, payload, and manifest staged.
Counts and record framing make different section partitions byte-distinct.

The source digest is SHA-256 over `F("mifolyo:crawl-source:v2")` followed by
`SECTION("jobs", records byte-sorted by job ID)`. Each source record has, in
order, `job_id`, `canonical_url`, `score_text`, `depth`, `group_id`,
`rate_scope_id`, and `policy_decision_sha256`. Mongo seed priority `1`, `2`, or
`3` maps exactly to score text `0`, `1`, or `2`; a stopped V1 migration retains
the validated canonical V1 score text. Invalid enabled source records abort the
feed; they are never silently skipped.

The source/output digest records intentionally omit the separately stored
`group_scope_id` and `initial_origin_scope_id`: Lua recomputes them from the
group lineage and canonical URL, and `policy_decision_sha256` plus the group-map
digest binds them. Enqueue, audit, staging, seal, and commit still carry and
validate the complete nine-field job/discovery record; the omission is not an
optional wire shape.

The contract digest is SHA-256 over `F("mifolyo:crawl-contract:v2")`, followed
by a `SECTION` labeled `document` containing one record whose only field is
`document_bytes` with the exact UTF-8 bytes of this file, followed by a
`SECTION` labeled `lua` containing records sorted by ASCII source name with
ordered fields `source_name` and `source_bytes`. Compatibility-manifest hashing
is plain SHA-256 over the exact reviewed artifact bytes. Shared positive and
negative vectors MUST cover every identity,
empty and maximum sections, all score forms, changed reason/payload rejection,
the page/publication derivation order, baseline/generation commit binding and
legacy-shape/formula rejection, and the acyclic guard-core/compatibility
derivation. Unchanged semantic output MUST retain its output/publication hashes
when only the request-start interval changes. Candidate marker installation is
forbidden until Go, Python, the independent verifier, and the Lua implementation
under real Redis agree on every vector.

## 5. Compatibility manifests, candidate mode, and commit guard

The active and candidate compatibility keys are:

```text
mifolyo:contracts:active     HASH
mifolyo:contracts:candidate  HASH
```

Each marker uses exactly these protocol fields plus the listed image fields:

```text
manifest_version=1
manifest_sha256=<sha256 of reviewed compatibility manifest bytes>
crawl_jobs=2
crawl_policy=2
canonicalization=1
page_publication=1
image_manifest=1
backlink_projection=1
render_ipc=2
signal_queue=retired
global_request_concurrency=2
redis_config_sha256=<64 lowercase hex>
commit_guard_sha256=<64 lowercase hex>
spider_image=sha256:<64 lowercase hex>
seed_importer_image=sha256:<64 lowercase hex>
crawl_admin_image=sha256:<64 lowercase hex>
indexer_image=sha256:<64 lowercase hex>
image_indexer_image=sha256:<64 lowercase hex>
backlinks_processor_image=sha256:<64 lowercase hex>
monitoring_image=sha256:<64 lowercase hex>
render_worker_image=disabled or sha256:<64 lowercase hex>
```

The reviewed compatibility-manifest artifact contains every field above except
the Redis-envelope field `manifest_sha256`; that field is the SHA-256 of the
artifact's exact bytes. The artifact does contain `commit_guard_sha256`.
Before any role-specific action, every named image must verify that its own
immutable runtime digest equals its exact manifest field; deployment tags are
not identities. Outside section 5.1's non-shipped fixture harness, the stopped
admin performs the same check against `crawl_admin_image` before any boot,
release, migration, activation, archive, or purge command.

`render_worker_image` MUST be `disabled` while rendering is disabled. Enabling
rendering requires an exact Render Worker digest in a newly reviewed manifest;
an IPC version alone is insufficient.
`crawl_admin_image` is the one reviewed deployable image containing boot,
release, migration, archive, and purge commands; each command still requires its
separate service credential and operation-specific role. The section 5.1
harness is test-only and is not a second deployable admin image.
The TF-IDF batch image is intentionally absent: it is a MongoDB-only offline
postprocessor, is not a V2 queue producer or consumer, and receives neither a
Redis credential nor Redis network access. Its unused legacy constant is not a
compatibility contract. Adding any Redis command path or pipeline role to that
image requires a newly reviewed manifest and explicit image field.

The corresponding crawl-contract markers are:

```text
mifolyo:crawl:v2:contract            STRING <contract digest>
mifolyo:crawl:v2:contract:candidate  STRING <same proposed contract digest>
```

`mifolyo:crawl:v2:admin_freeze` is a fixed-shape hash containing exactly
`protocol_version=2`, `freeze_nonce`, `process_stop_evidence_sha256`,
`candidate_manifest_sha256`, `candidate_contract_sha256`, and `created_at_ms`.
The hashed process-stop artifact contains the exact stopped process/image and
revoked-credential inventory, all six downstream list type/count observations,
both owner-lock observations, and the completed backlink-scan count/digest under
that same write freeze. Promotion directly rechecks the bounded lists, locks,
and lazy-free count; because the frozen credentials permit no intervening
producer, the artifact is the authoritative proof for the otherwise multi-page
backlink scan.
`mifolyo:crawl:v2:legacy_retirement` is a fixed-shape hash containing exactly
`protocol_version=2`, `freeze_nonce`, `backup_sha256`, `v1_count`,
`v1_url_field_count`, `v1_depth_field_count`, `v1_source_sha256`,
`v1_queue_evidence_sha256`, `v1_urls_evidence_sha256`,
`v1_depths_evidence_sha256`,
`spider_queue_type`, `spider_queue_count`, `spider_queue_evidence_sha256`,
`signal_queue_type`, `signal_queue_count`, `signal_queue_evidence_sha256`,
`deleted_bitmap`, and `retired_at_ms`. Counts and timestamps use canonical
decimal; Redis types are the lowercase values admitted by section 13; digests
and nonce use their section 4 lexical forms. Runtime clients compare every field
with the mounted, exact cutover-record artifact whose plain SHA-256 is configured
as `LEGACY_RETIREMENT_RECORD_SHA256`; they do not hash Redis `HGETALL` iteration
order.

The commit guard is:

```text
mifolyo:crawl:v2:commit_guard  HASH
```

It contains exactly `protocol_version=2`, `contract_sha256`,
`compatibility_manifest_sha256`, `redis_version`, `redis_config_sha256`,
`maximum_shape_sha256`, `memory_fixture_sha256`, `lua_benchmark_sha256`,
`aof_crash_evidence_sha256`, `cutover_mode`, `candidate_run_id`,
`approved_at_ms`, and `approved=1`. `cutover_mode` is `fresh` or
`v1_migration`; `candidate_run_id` is empty only for `fresh`. The evidence
digests bind the exact maximum-shape input, memory measurements, Lua
p50/p95/p99/max measurements, ACL/configuration, and process-kill/AOF outcomes.

To avoid a digest cycle, `commit_guard_sha256` is computed first over the exact
bytes of a reviewed guard-core artifact containing every guard field except
`compatibility_manifest_sha256` and `approved_at_ms`. The compatibility artifact
then includes that core digest and is hashed. Promotion adds the resulting
compatibility digest and Redis timestamp to the stored guard. Thus the active
marker binds the guard core and images, while the stored guard binds back to the
final compatibility artifact without either hash being self-referential.
Runtime clients reconstruct the guard-core field set from the stored guard,
verify its mounted exact artifact bytes/digest, and separately compare the
stored compatibility digest; they never hash unordered Redis field iteration.

Active-mode mutations require approved boot plus exact active compatibility,
contract, and commit-guard values compiled into the immutable release, exact
legacy-retirement evidence, and candidate/freeze keys absent. Runtime scripts do
not name or inspect legacy keys; promotion and explicit activation establish
their absence, and runtime ACLs deny those key names.
Candidate-mode mutations require approved boot, exact
candidate markers, absent active markers, the stopped-world freeze evidence,
and the migration-admin credential. Marker mismatch is
`COMPATIBILITY_MISMATCH` or `CONTRACT_MISMATCH` and permits no state mutation.

`signal_queue=retired` means every named runtime producer/consumer image MUST
neither read, write, recreate, wait on, nor delete `signal_queue`. In particular,
the target Indexer MUST remove its historical idle `LPUSH`; queue-state
observation is the only V2 wakeup mechanism. The stopped `crawl_admin_image` has
the sole exception: candidate-mode `CJ2_RETIRE_LEGACY_KEYS` may evidence and
delete that exact key before promotion, and promotion/activation may perform an
exact read-only absence check. No active-mode admin command may read its contents
or mutate it.

Candidate markers may coexist with legacy keys only while every producer,
consumer, scheduler, trigger, scaler, and ordinary Monitoring process is
stopped. They do not authorize execution or output. The stopped cutover tool
atomically promotes the candidate pair and installs the commit guard only after
the exact five legacy keys in section 13 are absent. An old binary ignores all
markers; immutable image digests, service-specific ACLs, and recorded
process-stop evidence therefore remain part of the safety boundary.

### 5.1 Non-authoritative acceptance-fixture bootstrap

Generating maximum-shape, memory, Lua, AOF, and crash evidence has one narrow
bootstrap dependency: those tests must execute the authoritative active-mode Lua
paths before the evidence-derived final guard and compatibility artifact can
exist. The sole exception is a non-authoritative acceptance-fixture harness. It
MAY directly install provisional active control records and bounded fixture data
only when all of these predicates are established before Redis starts:

- Redis and its volume are newly created for this evidence run, contain no
  production or retained data, and will never be attached to another deployment.
- The Redis network namespace has no route, DNS, proxy, ingress, or egress path
  to an external target or production network. No crawler, feeder, consumer,
  runtime/crawl-admin image, production secret, or production credential is
  present; all fixture credentials are freshly generated and valid only for this
  disposable Redis fixture.
- Redis uses the exact target version/configuration. The harness is a separately
  reviewed test-only image and accepts no caller-supplied marker bytes. Its setup
  manifest enumerates and digests every directly written key/value and proves
  that fixture state stays within the section 3 key, run, job, reservation,
  stage, queue, byte, and command bounds.

Define `ZERO_SHA256` as exactly 64 ASCII `0` bytes. The harness constructs the
provisional records deterministically as follows:

1. It uses the authoritative proposed contract digest, Redis/config values,
   protocol values, and reviewed target image digests. In the exact guard-core
   shape from section 5, only fields that are as yet unavailable among
   `maximum_shape_sha256`, `memory_fixture_sha256`,
   `lua_benchmark_sha256`, or `aof_crash_evidence_sha256` are replaced by
   `ZERO_SHA256`; every available evidence field uses its real digest.
   At least one of those four fields must be the sentinel or the fixture builder
   refuses setup, so it cannot install an all-nonzero final production record.
   `cutover_mode=fresh`, `candidate_run_id` is empty, and `approved=1`.
2. It computes the provisional `commit_guard_sha256` over those exact guard-core
   bytes using the ordinary section 5 rule. It then serializes the exact
   compatibility artifact with that guard digest and computes the provisional
   `manifest_sha256` by the ordinary rule. No test-only field is added to either
   production-shaped record.
3. Before boot approval, the setup credential writes an exact bounded
   setup-manifest persistence probe, records its acknowledged bytes, `SIGKILL`s
   Redis, restarts the same disposable volume, and proves zero loss. The hash of
   that current, nonzero preliminary rehearsal artifact may approve this fixture
   boot through the authoritative `CJ2_APPROVE_BOOT`; it is not the final
   guard's AOF/crash evidence. The setup credential then directly writes only the
   exact active compatibility hash, active contract string, fixed-shape commit
   guard (using the provisional compatibility digest and one recorded Redis
   setup time), a deterministic `v1_count=0`/five-key-absent legacy-retirement
   fixture record, and the setup-manifest-enumerated section 6 fixture keys.
   Candidate markers and `admin_freeze` remain absent. Empty-key evidence
   digests and every other synthetic digest are hashes of named canonical
   fixture artifacts, never zero sentinels.
4. The harness drops the setup credential before testing. Every behavior under
   measurement uses the unchanged authoritative Lua source and normal mutation
   path; each script validates the complete supplied/stored provisional gate
   records exactly. Direct setup or teardown is not transition-correctness
   evidence. No Lua source recognizes a fixture mode, sentinel, or bypass.

The provisional records are not candidate or production artifacts, cannot be
promoted, and are never crawl authorization. Synthetic fixture activation may
exercise ledger scripts, including `CJ2_START_REQUEST`, but the harness has no
external request capability and its response grants no permission outside that
isolated process. The harness has no candidate-key or marker-admin operation,
and its credential is not accepted outside the disposable Redis. Runtime and
crawl-admin images MUST reject `ZERO_SHA256`, the provisional manifest/guard
digests, fixture authorization artifacts, and any fixture setup manifest; none
is copied into an image, deployment secret, candidate marker, or release bundle.

After bounded evidence export, the harness revokes every fixture credential and
proves authenticated reconnect fails while Redis is still reachable locally,
then stops Redis and destroys the container and volume; the evidence records
each teardown result. Release assembly replaces every zero sentinel with the
resulting evidence digest,
recomputes the final guard core and compatibility artifact, and records the
provisional-to-final derivation. Real `CJ2_INSTALL_CANDIDATE_MARKERS` remains
forbidden until all final evidence fields are nonzero and the final guard,
manifest, image, and contract digests have passed independent review. No byte
from the destroyed provisional keyspace may be promoted or restored.

## 6. Redis keyspace

`R` denotes a run ID, `J` a job ID, `S` a scope ID, `Q` a reservation ID, and
`C` a commit ID.

### 6.1 Global keys

| Key | Type | Contents |
|---|---|---|
| `mifolyo:contracts:active` | HASH | Active compatibility manifest |
| `mifolyo:contracts:candidate` | HASH | Stopped-cutover candidate compatibility manifest |
| `mifolyo:crawl:v2:contract:candidate` | STRING | Candidate contract digest |
| `mifolyo:crawl:v2:contract` | STRING | Contract digest |
| `mifolyo:crawl:v2:durability` | HASH | Approved Redis boot |
| `mifolyo:crawl:v2:admin_freeze` | HASH | Stopped-world nonce and evidence digest |
| `mifolyo:crawl:v2:legacy_retirement` | HASH | Immutable five-key retirement evidence |
| `mifolyo:crawl:v2:commit_guard` | HASH | Approved maximum-shape proof |
| `mifolyo:crawl:v2:runs` | ZSET | `run_id => created_at_ms` |
| `mifolyo:crawl:v2:active_runs` | SET | Unfinalized loading, auditing, sealed, active, or cancelled run IDs |
| `mifolyo:crawl:v2:unarchived_runs` | SET | Every created run not yet archived; at most 100 |
| `mifolyo:crawl:v2:first_request_start` | HASH | Irreversible cutover boundary |
| `mifolyo:crawl:v2:active_leases` | ZSET | At most 64 `run_id:job_id => lease_expires_at_ms` members |
| `mifolyo:crawl:v2:stage_expiry` | ZSET | `commit_id => cleanup_due_at_ms`; at most 1,280 from 128 retained runs times ten starts |
| `mifolyo:crawl:v2:stage_slots` | HASH | At most four `commit_id => remaining_bytes:run_id:job_id:fence:abort_unlinked_keys` records; final field is `0` until abort |
| `mifolyo:crawl:v2:rate_scopes` | ZSET | Durable `scope_id => updated_at_ms` inventory |
| `mifolyo:crawl:v2:rate:S` | HASH | Shared rate-scope state |
| `mifolyo:crawl:v2:rate:S:active` | ZSET | `reservation_id => expires_at_ms` |
| `mifolyo:crawl:v2:rate:S:pending` | ZSET | Pending reservation IDs and expiries |
| `mifolyo:crawl:v2:rate:S:started` | ZSET | Started reservation IDs and expiries |
| `mifolyo:crawl:v2:reservation:Q` | HASH | Request reservation/tombstone |

### 6.2 Run keys

Let `B=mifolyo:crawl:v2:run:R`.

| Key | Type | Contents |
|---|---|---|
| `B` | HASH | Run record and counters |
| `B:jobs` | SET | All job IDs in the run |
| `B:job_order` | ZSET | All job IDs at score `0`, read lexicographically for audit/purge |
| `B:ready` | ZSET | `job_id => scheduling score` |
| `B:ready_at` | ZSET | `job_id => entered-ready time` |
| `B:leased` | ZSET | `job_id => lease expiry` |
| `B:leased_at` | ZSET | `job_id => claim time` |
| `B:delayed` | ZSET | `job_id => retry-not-before time` |
| `B:commit_backpressure` | ZSET | `job_id => first blocked commit time` |
| `B:completed` | ZSET | `job_id => completion time` |
| `B:dead` | ZSET | `job_id => dead-letter time` |
| `B:cancelled` | ZSET | `job_id => cancellation time` |
| `B:group_limits` | HASH | `group_id => per-run request-start limit` |
| `B:group_rate_scope_ids` | HASH | `group_id => 32-hex immutable lineage` |
| `B:group_scope_ids` | HASH | `group_id => derived SHA-256 shared scope ID` |
| `B:group_concurrency` | HASH | `group_id => submitted concurrency limit` |
| `B:group_interval_ms` | HASH | `group_id => submitted interval` |
| `B:group_started` | HASH | `group_id => recorded starts` |
| `B:group_pending` | HASH | `group_id => unstarted reservations` |
| `B:group_active_started` | HASH | `group_id => started but unfinished reservations` |
| `B:group_open_jobs` | HASH | `group_id => ready + leased + delayed jobs` |
| `B:audit_group_counts` | HASH | `group_id => source jobs validated by the frozen run audit` |
| `B:retry_reason_counts` | HASH | Closed retry-reason fields; sum equals `retries_total` |
| `B:recovery_outcome_counts` | HASH | `ready`, `delayed`, `dead`, `cancelled`; sum equals recovered total |
| `B:disposition_reason_counts` | HASH | Closed terminal reasons; sum equals completed + dead + cancelled |
| `B:visited_depth` | HASH | `url_id => shallowest committed depth` |
| `B:visited_urls` | HASH | `url_id => canonical URL` collision witness |
| `B:job:J` | HASH | Authoritative job record |

### 6.3 Staging keys

Let `T=mifolyo:crawl:v2:stage:C`.

| Key | Type | Contents |
|---|---|---|
| `T:meta` | HASH | Stage identity, frozen request interval, counts, digest, size, seal state |
| `T:keys` | LIST | Exact stage keys for bounded cleanup |
| `T:page` | HASH | Final-shaped page hash |
| `T:outlinks` | SET | Canonical outlinks; absent means canonical empty set |
| `T:discoveries` | ZSET | `job_id => score` |
| `T:discovery_records` | HASH | Seven suffixed fields per job: canonical URL, score text, group/rate/scope IDs, and decision digest |
| `T:discovery_depths` | HASH | `job_id => depth` |
| `T:aliases` | HASH | Two fixed suffixed fields per alias containing canonical URL and depth |
| `T:image_manifest` | HASH | Final-shaped image manifest |
| `T:image:N` | HASH | Final-shaped image payload, `N=0..63` |

Every stage key MUST receive the same absolute expiry when created. Replays do
not extend it. `T:keys` includes `T:meta` and itself, contains no duplicate and
no non-stage key, and is updated in the same Lua mutation that first creates a
stage key. The authoritative stage-key count, including metadata and cleanup
keys, MUST not exceed 73.

### 6.4 Preserved downstream grammar

The V2 commit MUST publish the existing immutable output forms:

```text
pages_queue                                                     LIST
page_data:<publication_id>:<base64url-page>                     HASH
outlinks:<publication_id>:<base64url-page>                      SET or absent-empty
page_images:<publication_id>:<base64url-page>                   HASH
image_data:<publication_id>:<base64url-page>:<base64url-image>  HASH
backlinks:<canonical-target-url>                                SET
```

The complete downstream list inventory governed by the V2 release sizing,
drain, and promotion gates is exactly:

```text
pages_queue
pages_queue:processing
pages_queue:dead
image_indexer_queue
image_indexer_queue:processing
image_indexer_queue:dead
```

The item grammar is fixed for every lifecycle position:

| Source, processing, or dead list | Exact item |
|---|---|
| `pages_queue`, `pages_queue:processing`, `pages_queue:dead` | Canonical `page_data:<publication_id>:<base64url-page>` key |
| `image_indexer_queue`, `image_indexer_queue:processing`, `image_indexer_queue:dead` | Canonical `page_images:<publication_id>:<base64url-page>` key |

Claim, recovery, handoff, and dead-letter movement preserve that exact item
byte-for-byte; no list uses an envelope, error suffix, or payload value.

The corresponding owner locks checked during stopped operations are exactly
`pages_queue:indexer_owner` and `image_indexer_queue:owner`. Their persistent
fence-epoch keys are not work queues and are not deleted by cutover.

Base64url is canonical UTF-8 base64url without padding. The Indexer continues to
enqueue the exact
`page_images:...` key onto `image_indexer_queue` only after page reconciliation.
The crawl commit MUST NOT enqueue the image manifest directly.

The final page hash has exactly these ten fields and no others:

```text
normalized_url, html, original_html, content_type, status_code,
last_crawled, rendered, render_policy_rule, render_policy_sha256,
publication_id
```

Their exact value contract is:

- `normalized_url` is the canonical effective document URL, equals the URL
  decoded from the final page key, and is at most 2,048 UTF-8 bytes.
- `html` is valid UTF-8 and at most 5 MiB. `original_html` is valid UTF-8 and at
  most 5 MiB; their combined byte length is at most 10 MiB.
- `content_type` is the exact validated, trimmed, unambiguous response value,
  from 1 through 1,024 UTF-8 bytes. It parses as `text/html` with either no
  parameter or exactly one case-insensitive `charset=utf-8` parameter.
- `status_code` is exactly three ASCII digits representing `100..399`.
- `last_crawled` is derived from the job's Redis-recorded final document request
  start, truncated to whole seconds and formatted in UTC with the exact Go
  layout `Mon, 02 Jan 2006 15:04:05 UTC`. Client wall time is forbidden.
- `rendered` is exactly `true` or `false`. For `false`, `original_html`,
  `render_policy_rule`, and `render_policy_sha256` are empty. For `true`,
  `original_html` is nonempty, `render_policy_rule` is 1 through 128 valid UTF-8
  bytes without control characters, and
  `render_policy_sha256` is the exact lowercase 64-hex run-pinned digest.
- `publication_id` equals the ID in the page key and the staged identity.

The final document request is the successful `document` or `redirect` request
whose response supplied `normalized_url`, `status_code`, `content_type`, and
the unrendered HTML. `CJ2_START_REQUEST` records each document/redirect start's
Redis timestamp and target identity/canonical-URL witness in the job; a start
does not attest network success. The pinned client MUST establish that the last
document/redirect event in the complete section 8.3 transcript supplied that
successful response. A later failed document/redirect cannot be ignored in favor
of an earlier response. Seal and first commit require page `normalized_url` to
equal the exact current-fence witness and reject a page whose timestamp,
document fence, URL ID, digest, or effective identity does not match. These
checks are additional to the frozen baseline/generation checks in section 10.5;
equal timestamps alone are not freshness evidence.

Outlinks are produced by resolving extracted HTML links against the canonical
effective page URL with canonicalization V1. Invalid links are rejected from
the output; valid links are canonicalized, deduplicated by exact UTF-8 bytes,
and the effective page URL itself is removed. Other redirect aliases are not
removed. More than 256 unique resulting links rejects the job as
`discovery_limit`; it is never truncated. A nonempty set is persisted exactly
under the publication-scoped outlink key; zero outlinks means that key is
absent.

The image manifest and payload remain contract version `1` and retain their
current exact five-field shapes:

```text
manifest: contract_version, publication_id, normalized_url,
          image_count, image_keys
payload:  contract_version, publication_id, normalized_page_url,
           normalized_source_url, alt
```

Every normalized image source is canonical, at most 2,048 bytes, and unique
within the page. Duplicate normalized sources reject output rather than select
an alt value. `alt` is valid UTF-8 and at most 1,024 bytes. Payloads and manifest
entries are ordered by normalized source URL UTF-8 bytes. `image_keys` is exact
compact UTF-8 JSON with no insignificant whitespace: `[]` or
`["<key>","<key>"]`; because final keys are ASCII base64url strings, no JSON
escape alternatives are permitted. `image_count` is canonical unsigned decimal
and equals the array length. A zero-image page still has an explicit five-field
manifest with `image_count=0` and `image_keys=[]`. More than 64 unique images or
a manifest above 393,216 bytes rejects output; neither is truncated.
These are intentional V2 tightenings of the current Image Indexer's 1,000-image
and 4 MiB manifest acceptance limits. The target pinned Image Indexer must change
its constants, validators, documentation, and golden tests to the V2 values;
the existing image digest is not compatible merely because the wire shape is
unchanged.

For each outlink target, the commit adds `normalized_url` to
`backlinks:<canonical-target-url>`. These sets are additive pending projection
state: concurrent additions remain, the Backlinks Processor removes only an
exact MongoDB-acknowledged member with `SREM`, and no V2 component deletes an
entire backlink key. Backlinks and their eventual projection are not part of
`output_digest` because they are deterministic consequences of the outlink set.

`CJ2_COMMIT` performs one `LPUSH pages_queue <exact-page-key>`. The compatible
Indexer claims with the existing source/processing-list protocol, validates the
page and outlinks, reconciles MongoDB, then atomically enqueues the exact
manifest key on `image_indexer_queue` while ACKing and deleting only that page
and outlink publication. It never uses `signal_queue`. The Image Indexer retains
its existing manifest/payload validation and acknowledged deletion behavior.
Every downstream claim/recovery/handoff/dead-letter script preserves the exact
combined source/processing/dead-list item and byte bounds from the release sizing
artifact. A handoff that would add new downstream work above a bound retains the
current processing claim and emits backpressure; it never ACKs first.

## 7. Record schemas

### 7.1 Run record

All fields are required. Empty timestamps are stored as `0`, not omitted.

| Field | Constraint |
|---|---|
| `protocol_version` | Exactly `2` |
| `contract_sha256` | Exact active contract digest |
| `state` | Run state defined below |
| `source_kind` | `mongo` or `v1_migration` |
| `source_sha256` | Digest of sorted admitted seed records |
| `expected_seed_count` | `0..10000` |
| `authorization_sha256` | Digest of reviewed authorization artifact |
| `authorization_scope_sha256` | Digest of exact run/site scope |
| `authorization_expires_at_ms` | Future Redis timestamp, at most 24 hours after creation |
| `canonicalization_version` | Current release: `1` |
| `canonicalization_sha256` | Digest of canonicalization contract/vectors |
| `crawl_policy_version` | Current release: `2` |
| `crawl_policy_sha256` | Digest of exact loaded policy bytes |
| `render_policy_version` | Exact reviewed render-policy contract version |
| `render_policy_sha256` | Digest even when every render rule is disabled |
| `policy_group_count` | `1..64` |
| `policy_group_map_sha256` | Digest of exact ordered group tuples |
| `max_jobs` | Exactly `10000` for V2.0 |
| `max_request_starts` | `1..10`; bounded baseline uses `10` |
| `global_concurrency_limit` | Exactly active compatibility value; current release `2` |
| `max_delivery_attempts` | Exactly `3` |
| `job_count` | Current total jobs |
| `open_job_count` | Ready + leased + delayed jobs |
| `request_starts` | Durable run-global starts |
| `reservation_creations_total` | First creation of any request reservation; `0..100` |
| `pending_request_reservations` | Durable unstarted reservations |
| `started_request_reservations` | Durable started but unfinished reservations |
| `claims_total` | Successful new claims |
| `retries_total` | Delayed retry transitions |
| `recovered_leases_total` | Expired leases processed |
| `renewal_rejections_total` | Definitive stale/expired renewal results |
| `completed_total` | Completed jobs |
| `dead_total` | Dead jobs |
| `cancelled_total` | Cancelled jobs |
| `output_commits_total` | Successful first commits |
| `load_revision` | Starts at `1` on run creation; incremented on every effective enqueue mutation |
| `audit_revision` | `0` before audit or exact positive frozen loading revision |
| `audit_count` | Number of lexicographically audited jobs |
| `audit_cursor` | Empty or last audited job ID |
| `audit_complete` | `0` or `1` |
| `created_at_ms` | Redis time |
| `sealed_at_ms` | Redis time or `0` |
| `activated_at_ms` | Redis time or `0` |
| `budget_exhausted_at_ms` | Redis time or `0` |
| `cancelled_at_ms` | Redis time or `0` |
| `completed_at_ms` | Redis time or `0` |
| `finalized_at_ms` | Redis time when terminal run accounting is closed, or `0` |
| `last_activity_at_ms` | Redis time of the latest effective state mutation |
| `last_execution_at_ms` | Latest claim/start/retry/commit transition or `0` |
| `last_request_started_at_ms` | Latest durable request start or `0` |
| `last_terminal_transition_at_ms` | Latest job terminal transition or `0` |
| `retention_anchor_ms` | Frozen maximum activity anchor when the run finalizes |
| `archived_at_ms` | Redis time or `0` |
| `archive_sha256` | Empty or exported evidence digest |
| `purge_state` | `none` or `in_progress` |
| `purge_evidence_sha256` | Empty or the immutable archive/candidate-abort digest |
| `purge_started_at_ms` | Redis time or `0` |
| `purged_job_count` | Jobs removed by bounded purge; `0..job_count` |
| `terminal_reason` | `none` or stable run reason |

`purge_state=none` requires an empty purge digest and both purge numbers `0`.
`in_progress` requires a lowercase SHA-256, positive Redis start time, and
`0<purged_job_count<job_count`; a batch that removes the final job deletes the
whole run in the same script and never leaves an observable count equal to
`job_count`.

### 7.2 Job record

All fields are required and fixed-shape. A field not applicable to the current
state uses the exact sentinel named below; when none is named, it is the empty
string for text or `0` for a number.

| Field | Constraint |
|---|---|
| `protocol_version` | `2` |
| `run_id`, `job_id`, `url_id` | Exact identifiers; `job_id=url_id` |
| `canonical_url` | Canonical, at most 2,048 UTF-8 bytes |
| `depth` | Canonical nonnegative integer |
| `score_text` | Canonical finite value in protocol range; ZSET score must round-trip |
| `state` | Job state defined below |
| `group_id` | Required policy ID, immutable before first ready membership |
| `rate_scope_id` | Required 32-hex group lineage, immutable within the run |
| `group_scope_id` | Required SHA-256 scope ID |
| `initial_origin_scope_id` | Required SHA-256 scope ID |
| `policy_decision_sha256` | Required exact initial policy decision digest |
| `claim_count` | Number of new leases |
| `delivery_attempts` | Leases on which at least one request start was recorded |
| `request_starts` | Cumulative recorded starts for this job; `0..10`, never reset |
| `lease_request_starts_baseline` | Cumulative job starts captured immediately before the last issued fence; initially `0`, retained until a new claim |
| `retry_count` | Number of `leased -> delayed` transitions |
| `pre_io_recoveries` | Expired leases that recorded no request start |
| `next_request_ordinal` | Monotonic reservation ordinal |
| `last_request_started_at_ms` | Redis time of latest request start or `0` |
| `last_document_request_started_at_ms` | Redis time for final/current document hop or `0` |
| `last_document_request_fence` | Fence of that document hop or `0` |
| `last_document_target_url_id` | URL ID of that document hop or empty |
| `last_document_target_url` | Exact canonical target URL of that document hop or empty |
| `last_document_target_digest` | Target digest of that document hop or empty |
| `last_reason` | `none` or stable completion/dead/cancellation/transition reason |
| `last_failure_reason` | Stable underlying failure reason or `none` |
| `lease_owner` | Owner ID or empty |
| `lease_token` | Token or empty |
| `lease_fence` | Last issued fence; never decremented |
| `lease_started_at_ms` | Claim time or `0` |
| `lease_expires_at_ms` | Expiry or `0` |
| `lease_delivery_started` | `0` or `1` for current fence |
| `active_reservation_id` | One reservation ID or empty |
| `active_stage_commit_id` | Current fence's stage commit ID or empty |
| `last_stage_commit_id` | Most recently begun stage ID or empty; never reused |
| `last_stage_fence` | Fence that most recently began a stage or `0`; monotonic and a request-admission freeze for that fence even after abort |
| `not_before_ms` | Delayed deadline or `0` |
| `commit_backpressure_fence` | Current blocked commit fence or `0` |
| `commit_backpressure_reason` | `none`, `pages_queue_full`, or `memory_headroom_low` |
| `commit_backpressure_started_at_ms` | First blocked commit Redis time or `0` |
| `commit_backpressure_deadline_ms` | `min(first blocked time + 120000, active stage expires_at_ms - 10000)` or `0` |
| `output_digest` | Empty or SHA-256 |
| `publication_id` | Empty or SHA-256 |
| `commit_id` | Empty or SHA-256 |
| `published_page_key` | Empty or exact final page queue value for archive reconciliation |
| `last_transition_id` | Empty or SHA-256 idempotency identity |
| `last_transition_status` | Empty or stable response status |
| `created_at_ms`, `updated_at_ms` | Redis time |
| `completed_at_ms`, `dead_at_ms`, `cancelled_at_ms` | Redis time or `0` |

Within request-transcript predicates, define `B=lease_request_starts_baseline`
and `G=request_starts`. These are counts, distinct from section 6's run-key
prefix `B` and section 2's allocation bound `G`. Every job requires
`0 <= B <= G <= 10`. A new seed or discovered job has `B=G=0`; a never-claimed
job with `lease_fence=0` retains those values. A new successful claim atomically
captures its pre-claim `G` as `B`; existing admission limits require `B<10`.
Claim replay, blocked claim, and claim-time visited completion do not change B.
Only a first successful request-start transition increments G, and it never
changes B. For a leased job:

```text
lease_delivery_started = 0  iff G = B
lease_delivery_started = 1  iff G > B
```

Finish, reservation cancellation, release, retry, abort, recovery, terminal job
transitions, and cleanup MUST NOT reset or replace B or G. Only a later new
claim replaces B; a new fence never resets G. Clearing **active lease fields**
means clearing `lease_owner` and `lease_token` and setting
`lease_started_at_ms`, `lease_expires_at_ms`, and `lease_delivery_started` to
`0`. It MUST retain `lease_fence`, `lease_request_starts_baseline`,
`request_starts`, `last_stage_commit_id`, and `last_stage_fence`. Document
witnesses retain their own fence until replaced by a later document/redirect
start; neither a timestamp comparison nor an older document witness establishes
a start on the current fence.

If `last_stage_fence=lease_fence>0`, then `G>B` and `active_reservation_id` is
empty even when `active_stage_commit_id` is empty after abort. A still-leased
frozen job with no active stage is legal only with its exact aborted-stage
terminal reservation. A published completed job retains B/G for its completed
commit identity and stage-independent replay; they are not optional terminal
sentinels.

### 7.3 Reservation record

| Field | Constraint |
|---|---|
| `protocol_version` | `2` |
| `reservation_id` | Exact ID |
| `run_id`, `job_id`, `owner_id`, `lease_token`, `lease_fence` | Exact lease identity |
| `request_ordinal` | Positive, monotonic within job |
| `state` | `pending`, `started`, `finished`, `cancelled`, or `expired` |
| `request_kind` | `robots`, `document`, `redirect`, or `render_resource` |
| `target_url_id`, `canonical_target_url`, `target_digest` | Exact outbound target identity and collision witness |
| `crawl_policy_sha256`, `policy_decision_sha256` | Exact run-pinned decision |
| `group_id` | Policy group charged for the start |
| `rate_scope_id` | Immutable 32-hex group lineage |
| `global_scope_id`, `group_scope_id`, `origin_scope_id` | Exact three scopes |
| `global_concurrency`, `global_interval_ms` | Exact tuple; interval is `0` |
| `group_concurrency`, `group_interval_ms` | Exact run-pinned tuple |
| `origin_concurrency`, `origin_interval_ms` | Exact run-pinned tuple |
| `created_at_ms`, `started_at_ms`, `terminal_at_ms` | Redis time or `0`; terminal time is set for `finished`, `cancelled`, or `expired` |
| `delivery_attempts_after_start`, `job_starts_after_start` | Exact post-start response snapshot or `0` |
| `run_starts_after_start`, `group_starts_after_start` | Exact post-start response snapshot or `0` |
| `expires_at_ms` | Current lease-aligned expiry |

Only one reservation may be active for a job. This matches the synchronous
secure-fetch gate and the render protocol's one outstanding broker intent. The
reservation ID and target digest are recomputed from the immutable intent fields
and canonical-URL witness in section 4 before creation; server state, response
snapshots, and timestamps are excluded. An existing ID with any changed
immutable field is `IMMUTABLE_MISMATCH`. Finished, cancelled,
and expired reservation hashes remain as idempotency tombstones for 24 hours.
Pending and started reservation hashes MUST have no Redis key expiry:
`expires_at_ms` and the three scope-index scores are logical recovery deadlines,
so the authoritative record must still exist when recovery runs after that
deadline. The 24-hour key TTL is applied only in the same transition that makes
the reservation terminal; idempotent replays never extend it.
One first creation corresponds to one unique reservation key and increments the
owning run's `reservation_creations_total` exactly once. The fixed run limit
therefore bounds all active and terminal reservation records to 100 per run and
bounds terminal tombstone churn to at most 100 keys per run, with at most 10,000
across the 100 unarchived runs. Expiry may reduce the present key count but never
decrements the cumulative counter or permits another creation.

The per-fence baseline is not a reservation field or a claim/start response
field. It is authenticated through the job projection in section 8.3. Historical
START replay returns this reservation's original post-start snapshots, not the
current job counters, and MUST NOT validate those snapshots against a newer
fence's baseline or substitute that baseline into the reply.

### 7.4 Rate-scope record

| Field | Constraint |
|---|---|
| `protocol_version` | `2` |
| `scope_id` | Exact key scope ID |
| `scope_kind` | `global`, `group`, or `origin` |
| `scope_witness` | `global`, exact `rate_scope_id`, or exact canonical origin, according to kind |
| `effective_concurrency` | `1..32` |
| `effective_interval_ms` | `0..3600000` |
| `next_allowed_ms` | Redis timestamp or `0`; never decreased in V2.0 |
| `last_started_at_ms` | Latest recorded start in this scope or `0` |
| `active_count` | Exact cardinality of `:active` |
| `pending_count` | Exact cardinality of `:pending` |
| `started_count` | Exact cardinality of `:started` |
| `concurrency_source_sha256` | Policy digest that most recently lowered concurrency |
| `interval_source_sha256` | Policy digest that most recently raised interval |
| `updated_at_ms` | Redis time |

Each active reservation appears in `:active` and exactly one of `:pending` or
`:started`; all three hash counters MUST equal their index cardinalities. Scope
records and their `rate_scopes` inventory members are durable monotonic evidence
and MUST NOT be removed or relaxed during V2.0 normal operation. A secondary
ZSET disappears naturally when its final validated member is removed; no
maintenance operation deletes a nonempty index. Creation of a new
scope fails without mutation when the 100,000-scope inventory is full. Any
future compaction or relaxation requires all crawl processes stopped, a reviewed
new protocol/migration, and new compatibility evidence.

A derived scope that has never admitted a reservation is absent from both the
inventory and keyspace and is not corruption. Monitoring reports zero active,
pending, started, and next-allowed values for that unmaterialized scope. Any
scope hash without its exact inventory member, inventory member without its
fixed-shape hash, or secondary index without both is corruption.

### 7.5 Stage metadata and accounting

`T:meta` has exactly these fixed fields. Empty chunk digests are stored as the
empty string, never omitted:

```text
protocol_version, run_id, job_id, owner_id, lease_fence, token_digest,
commit_id, publication_id, output_digest,
request_starts_baseline, request_starts_generation,
created_at_ms, expires_at_ms, sealed, sealed_at_ms, abandoned,
expected_page_fields, expected_outlinks, expected_discoveries,
expected_aliases, expected_images,
page_fields_written, html_written, original_html_written,
outlinks_written, discoveries_written, aliases_written, images_written,
manifest_written, data_bytes, key_count,
page_fields_chunk_digest, html_chunk_digest, original_html_chunk_digest,
outlinks_chunk_0_digest, outlinks_chunk_1_digest,
outlinks_chunk_2_digest, outlinks_chunk_3_digest,
discoveries_chunk_0_digest, discoveries_chunk_1_digest,
aliases_chunk_0_digest, images_chunk_0_digest, manifest_chunk_digest
```

`request_starts_baseline` and `request_starts_generation` are immutable
canonical-decimal snapshots satisfying
`0 <= request_starts_baseline < request_starts_generation <= 10`. BEGIN copies
them only after comparison with the job's B/G and binds them into `commit_id`.
Owned-stage writes, seal, and first commit require the exact current-fence
job/stage equality in section 10.5. Recovery does not rewrite these snapshots.
After recovery and a new claim they describe a historical fence, not the current
job interval; cleanup MUST NOT apply current-fence equality to that residue.

`expected_page_fields` is exactly `10`, `expected_aliases` is `1..5`, and the
other expected counts are zero through their section 3 maxima. Outlink chunks
are fixed contiguous slices of at most 64 byte-sorted records,
discovery chunks are fixed contiguous slices of at most 64 job-ID-sorted
records; aliases use one nonempty batch and images fit one optional batch. A
zero-count outlink, discovery, or image collection has no chunk and its
corresponding digest fields remain empty. The page non-blob
fields form one chunk; `html` and `original_html` are independent blob chunks.

`data_bytes` is the exact sum of application data in `T:page`, `T:outlinks`,
the three discovery keys, `T:aliases`, `T:image_manifest`, and every `T:image:N`:
key-name bytes, hash field-name/value bytes, members, and canonical score text.
`T:meta` and `T:keys` bookkeeping bytes are excluded from this logical stage
limit but included in the section 2 allocation bound. This exclusion avoids a
self-referential counter. `key_count` includes `T:meta` and `T:keys`. Every
first write computes its exact positive deltas; replays compare every supplied
byte and have zero deltas. Counts, bytes, chunk digests, key inventory, expiry,
and the global slot reservation are changed atomically.

In lifecycle predicates, an **owned stage** means an exact job active-stage
field plus its matching `stage_slots` owner record. A committed or recovered
residual bundle whose job field and slot were removed is cleanup residue, not an
owned stage. It remains in `stage_expiry` until bounded cleanup, remains visible
to memory/age monitoring, and grants no execution or publication authority.
An **aborted-stage terminal reservation** is the matching slot retained after
`CJ2_ABORT_STAGE`: the job's active-stage field is empty, its current fence and
`last_stage_commit_id` match the slot, and its last transition is that exact
abort; `abort_unlinked_keys` is its original positive `key_count`. It contains no
stage keys or expiry member, grants no stage authority, and may be consumed only
by the next lease-ending transition or expired-lease recovery.
Every begin/data/seal/first-commit path and cleanup check of an existing owner
slot requires `abort_unlinked_keys=0`; only abort changes it to a positive value,
and only a lease-ending transition or recovery may remove a slot with that
positive value. Exact post-abort replay instead requires that retained positive
count, and completed-COMMIT replay requires no slot or stage keys.
`T:meta.expires_at_ms` is the immutable original 15-minute stage expiry. A first
commit may shorten the residual keys' physical expiry and the `stage_expiry`
cleanup-due score, but never rewrites that metadata field; cleanup therefore
validates a committed residual due time as no later than the original expiry and
against the job's exact completed commit identity.

## 8. State machines

### 8.1 Run states

```text
loading -> auditing -> sealed -> active -> completed -> archived
   |          |          |        |\-> budget_exhausted -> archived
   |          |          |        \-> cancelled -> archived
   \----------\----------\----------> cancelled -> archived
```

- `loading`: only the feeder or stopped migration may create/update jobs.
- `auditing`: loading is frozen; bounded lexicographic audit batches may only
  validate exact submitted records and advance the audit cursor.
- `sealed`: source count and digest are fixed; jobs remain unclaimable.
- `active`: claims, request starts, retries, recovery, and commits are allowed
  subject to authorization and budgets.
- `budget_exhausted`: no request reservation can be created; no lease exists,
  and unprocessed ready or delayed jobs remain durable evidence. Its terminal
  reason identifies the run request-start limit, reservation-creation limit, or
  exhausted open-job groups that made further execution impossible.
- `completed`: no ready, leased, or delayed jobs remain; every job is terminal.
- `cancelled`: no new claim, request start, retry, or commit is allowed. Bounded
  maintenance moves remaining work to job `cancelled`.
- `archived`: immutable exported evidence exists; no execution is allowed.

Authorization expiry is evaluated inside claim, start, renew, retry, and commit
scripts. At or after the exact expiry, the run is treated as cancelled even if
the cancellation maintenance script has not yet run. A request already in
progress is cancelled by its local authorization deadline. Output not committed
before expiry MUST be suppressed.

Reaching a run or group request-start limit does not cancel a lease that already
exists. Its current worker may finish an already started request, renew, stage,
commit without another request, or use the exact after-I/O retry disposition.
Only creation of a new reservation is denied. This prevents budget accounting
from stranding a lease.

### 8.2 Job states

```text
ready -> leased -> completed
  |        |  \--> delayed -> ready
  |        |  \--> dead
  |        |  \--> cancelled
  |        \-----> ready       (release/recovery before I/O only)
  |\------------> completed   (durably already visited)
  |\------------> dead        (malformed/denied before claim)
  \-------------> cancelled

delayed -> cancelled
```

`completed`, `dead`, and `cancelled` are terminal within a run. A terminal URL
may be crawled only in a new run with a new authorization.

Every leased job has exactly one matching
`active_leases[run_id:job_id]=lease_expires_at_ms` member, and no other job has
one. The composite is unambiguous because both identifier grammars exclude
colons. Its global cardinality is at most 64; expired entries remain counted
until exact recovery, just like per-run leased membership. This bounds recovery
and safety-reserve exposure; it is separate from the stricter active-request and
four-stage concurrency limits.

Every job is policy-bound before its first ready membership. `group_id`,
`rate_scope_id`, group scope, initial origin scope, and policy-decision digest
are immutable thereafter. Every ready, leased, or delayed job contributes one
to both run `open_job_count` and its `group_open_jobs` field; every first
terminal transition decrements both exactly once, increments exactly one of
`completed_total`, `dead_total`, or `cancelled_total`, and increments exactly
one matching closed `disposition_reason_counts` field. This accounting applies
equally to claim-time visited completion, ready rejection, worker outcome,
recovery, and cancellation maintenance.

`commit_backpressure` is a secondary index, not a primary state. A member exists
if and only if its leased job has the same nonzero
`commit_backpressure_started_at_ms` and current
`commit_backpressure_fence=lease_fence` plus a non-`none` closed reason; its
score is that start time. Commit,
retry, recovery, dead-letter, and cancellation remove it exactly when they clear
those job fields. Its cardinality cannot exceed the run's ten-start bound.

### 8.3 Delivery attempts versus request starts

A successful new **claim** with status `CLAIMED` issues one lease and increments
`claim_count`, and atomically captures the job's current cumulative
`request_starts` as `lease_request_starts_baseline`. Claim-time visited completion
issues no lease, changes no baseline, and is not a claim counter increment.
Neither path increments a delivery attempt or request budget.

A **delivery attempt** is one lease/fence on which at least one request start is
recorded. `delivery_attempts` increments exactly once, in the first successful
`CJ2_START_REQUEST` for that fence.

A **request start** is the durable pre-I/O charge for a `robots`, `document`,
`redirect`, or approved `render_resource` request. Each first successful
`CJ2_START_REQUEST` pending-to-started transition increments exactly once:

- the job's `request_starts`;
- the run's `request_starts`;
- the run/group `group_started` count.

Exact replay increments none of them. A successful ledger start is not a
successful network response: DNS/dial failures, timeouts, HTTP failures,
cancellation after START, and a granted start never used for I/O all remain
counted. The job's `lease_request_starts_baseline` is unchanged. "Before I/O",
"after I/O", and the response bit `after_io` in ledger lifecycle predicates mean
before or after the first recorded start on the fence, not proof that a socket
was used or a response succeeded.

That first transition also moves the run/group active reservation counters from
pending to started and sets job and run `last_request_started_at_ms`. A
`document` or `redirect` start additionally replaces the job's
`last_document_request_started_at_ms`, document fence, target URL ID, target
digest, and exact canonical target URL. Those fields provide the only permitted
source for final `last_crawled` and effective-page identity.

One delivery may therefore consume several of the run's ten request starts.
Retry exhaustion uses delivery attempts. Run and group budgets use request
starts. An unstarted reservation is counted as pending to prevent
over-reservation; cancellation/expiry refunds only that pending request-start
budget and never refunds a reservation creation.

`next_request_ordinal` starts at `1`. Claim/reserve uses its current value in the
reservation identity and increments it exactly once only when a new pending
reservation is created. Exact replay never increments it; cancellation, finish,
expiry, retry, and a new fence never reset it.

`reservation_creations_total` starts at `0` and increments in the same atomic
mutation that first creates any reservation, including the initial reservation
created by `CJ2_TRY_CLAIM` and each later one created by
`CJ2_RESERVE_REQUEST`. Replays and claim-time visited completion do not increment
it. Every successful new claim creates exactly one initial reservation, so at
all times `claims_total <= reservation_creations_total <= 100`; claim count is
therefore bounded by the same run-global limit. V2 adds no separate per-job
reservation-creation limit: a job's existing monotonic ordinal supplies identity
while the one run counter supplies the finite retention bound.

Reaching 100 prevents only a first creation. It does not invalidate an existing
pending/started reservation or current lease: that reservation may still start
or finish, and a worker may stage and commit already obtained output without
another request. The counter never decreases and cancellation, expiry, or
tombstone TTL never restores creation capacity.

#### Request-transcript completeness and terminal witness

A candidate request transcript is a bounded immutable sequence of authenticated
recorded starts for one exact run/job/owner/token/fence. Each event binds its
reservation ID, ordinal, kind, canonical target and digest, run-pinned policy
decision, Redis start time, and original post-start job/run/group/delivery
snapshots. Caller-created arrays, counts, timestamps, or a guessed
`first_count-1` baseline are not authority. Historical client names such as
`SuccessfulDocumentRequest` refer to successful ledger starts, not evidence that
I/O succeeded; robots and render-resource starts are represented too.

After request work has stopped and the active reservation has been finished or
cancelled, the private authenticated transport obtains the final document
witness with one `HMGET` on the exact job key derived internally from the lease's
run/job identity. The exact ordered thirteen-field projection is:

```text
last_document_request_started_at_ms
last_document_request_fence
last_document_target_url_id
last_document_target_url
last_document_target_digest
request_starts
last_request_started_at_ms
lease_request_starts_baseline
state
lease_owner
lease_token
lease_fence
active_reservation_id
```

Every element is a required canonical bulk string; missing, additional,
reordered, or legacy projection fields are rejected. Parsing requires
`state=leased`, exact returned owner/token/fence equality with the requested
lease, an empty `active_reservation_id`, and `0 <= B < G <= 10`. The document
fence must equal that positive lease fence, its target URL/ID/digest must
recompute exactly, and
`0 < last_document_request_started_at_ms <= last_request_started_at_ms`.
The projection is not a new transition or Lua response envelope, does not freeze
the job, and does not prove that lease/authorization remains live after the read.

Transcript construction may produce a provisional prefix. Only output-context
construction (`NewOutputContext` in Go) certifies completeness, requiring for
length `n` and zero-based event index `i`:

```text
1 <= n <= 10
first.job_request_starts = B + 1
event[i].job_request_starts = B + 1 + i
last.job_request_starts = G
n = G - B
```

Every event must authenticate the same full lease identity and exact run-policy
binding. Reservation ordinals are strictly increasing but may have gaps for
reservations that never started; they are not start counts. Start timestamps
are nondecreasing, not necessarily distinct. Delivery-attempt snapshots agree
within the fence; run snapshots and each revisited group's snapshots increase
but need not be contiguous because other jobs may start between events.

The source-document event must bind the exact admitted source URL, depth, and
policy lineage. The final document/redirect event must match the projected
document timestamp/fence/target witness and supply the successful response
required by section 6.4. The last event, which may instead be a robots or
render-resource start, must match projected G and `last_request_started_at_ms`.
All recorded starts must be represented even if their network attempt failed,
was cancelled, or never began. A failed required document/redirect takes its
existing typed outcome path, not publication of an earlier response. An
independently permissible nonfatal resource failure still contributes a counted
non-alias event; this rule does not relax any failure or render policy. Missing
authenticated event evidence or unknown response success suppresses output;
neither counter arithmetic nor a reconciliation-only receipt can manufacture it.

`OutputContext` MUST privately retain the full lease identity, authenticated B/G,
source/policy binding, final document witness, and derived aliases. BEGIN, output
preparation, stage chunks, and independent pre-seal verification MUST consume
that same binding, not relabel an older context/output with caller-selected B/G.
A pure commit-hash helper is not output authority. The semantic output digest
still contains only section 4's five sections.

In a consistent ledger, the unique post-start job counts for a fence are exactly
`B+1` through G. Authenticated coverage of that interval proves no recorded
start was omitted or duplicated, including equal-millisecond events and a
redirect returning to an earlier target. Successful BEGIN then compares B/G
atomically and freezes further starts; a stale projection alone cannot do so.
This count proof relies on the section 10.1 trusted-image boundary and section
10.3's one-per-reservation I/O permit. It is not a hostile-client attestation of
network success, response bytes, or redirect semantics and requires no hash
chain or additional operation.

### 8.4 Reservation states

```text
pending -> started -> finished
   |          \----> expired
   |\--------------> cancelled
   \---------------> expired
```

Only `pending` and `started` appear in rate-scope active ZSETs. A stale worker
MUST NOT remove either state. Normal finish/cancel requires the current lease;
recovery removes it only when its Redis expiry is due.

### 8.5 Monotonic shared-rate semantics

Crawl-policy V2 MUST assign every group an immutable `rate_scope_id` matching
`[0-9a-f]{32}`. A run's reviewed group map contains at most 64 tuples in group-ID
byte order:

```text
(group_id, rate_scope_id, group_scope_id, request_start_limit, concurrency,
 interval_ms)
```

Group IDs are 1 through 128 UTF-8 bytes without control characters. Limits are
within section 3. The global tuple is always `(concurrency=2, interval_ms=0)`.
Every stored/submitted `group_scope_id` must equal the section 4 derivation for
its lineage.
The origin tuple uses the matched group's concurrency and interval. Claim and
reserve recompute all three scope IDs and require every submitted tuple and
`policy_decision_sha256` to match the run-pinned map; last-writer-wins is
forbidden. The global, group, and origin scope IDs for one intent must be
pairwise distinct; a domain-separated hash collision is `INVALID_ARGUMENT`
before any scope is read or mutated.

The initial robots/document intent embedded in a claim is not a new grouping
decision. Its group ID, rate lineage, and group scope equal the job's immutable
source-document fields. Its canonical target origin must derive the job's
`initial_origin_scope_id`; robots inherits that same source binding even though
its request-kind/target-specific policy-decision digest is distinct. This keeps
every open job charged to the immutable group used by group-open finalization.

On first materialization, the script stores the exact kind-specific
`scope_witness`. Every later access recomputes the scope ID and requires both
kind and witness to match. A same-scope-ID/different-witness event is
`RATE_STATE_CORRUPT`; raw origin witnesses remain prohibited from logs and
metric labels.

When a scope is first admitted, its effective tuple is the submitted tuple.
Thereafter normal operation applies exactly:

```text
effective_concurrency = min(stored_effective_concurrency, submitted_concurrency)
effective_interval_ms = max(stored_effective_interval_ms, submitted_interval_ms)
next_allowed_ms = max(stored_next_allowed_ms,
                      last_started_at_ms + effective_interval_ms)
```

The global interval remains zero. A lower concurrency never removes live
members; new reservations wait until `active_count < effective_concurrency`. A
larger interval applies conservatively to the latest prior start as shown. Start
then sets `last_started_at_ms=now` and advances group/origin
`next_allowed_ms=max(existing, now+effective_interval_ms)`; it does not advance
the zero-interval global deadline. Effective concurrency never increases,
effective interval never decreases, and `next_allowed_ms` never decreases in
V2.0, even after a scope becomes idle. There is no automatic or operator
relaxation command in this protocol.

For already-existing scopes, a fully validated claim/reserve persists any
strict `min`/`max` tightening above even when the final result is
`CAPACITY_BLOCKED`, `RATE_BLOCKED`, `STAGE_CAPACITY_BLOCKED`,
`LEASE_CAPACITY_BLOCKED`, `RUN_BUDGET_EXHAUSTED`,
`RUN_RESERVATION_LIMIT_EXHAUSTED`, or `GROUP_BUDGET_EXHAUSTED`. This is the sole
allowed side effect of those blocked results: it changes
no reservation, job, run/group budget, or active/pending/started membership. It
prevents an older run from readmitting work under the superseded looser tuple. A
missing scope is created only with a successful reservation. Invalid/corrupt
input and any failed memory bound mutate nothing.

For a group or origin scope with nonzero effective interval, no new reservation
is admitted while any pending reservation exists. Pending members already
admitted under a prior zero interval remain conservatively counted after a
tightening; no additional member is added until all leave pending. A blocked
reservation receives `RATE_BLOCKED` with a reported deadline equal to the
maximum of `next_allowed_ms` and all existing pending expiries, and may poll
because an earlier start/cancel can
change availability. Zero-interval scopes may have multiple pending members only
within effective concurrency. This pre-start serialization prevents two callers
that observed the same old deadline from starting together.

A pending reservation is capacity, not irrevocable permission to start under an
obsolete interval. `CJ2_START_REQUEST` rechecks the current group/origin
`next_allowed_ms`. If tightening left multiple pending reservations, at most one
can start at a deadline; that start advances the deadline before another is
considered. A blocked pending reservation remains pending and returns the exact
current rate deadline without consuming a start.

Claim/reserve evaluates scopes in fixed global, group, origin order and, within
each scope, corruption, concurrency, pending-interval conflict, then time
deadline. Start uses the same scope order for corruption and then the current
group/origin time deadlines, without re-reserving concurrency. The first
blocked condition determines the stable response.

After exact replay and run/job validation, claim/reserve blocked-status
precedence is run request-start budget, the run-global reservation-creation
limit, group request-start budget, the first global/group/origin scope
condition in the order above, then claim-only global lease capacity and stage
capacity in that order. A prevalidation
error, including failed command or memory bounds, takes precedence over every
status and permits no monotonic tightening; clients never choose the result by
argument ordering.

Claims and reserves never prune any reservation index. An expired member still
consumes capacity until `CJ2_RECOVER_EXPIRED` validates the owning reservation,
lease, all three scope indexes/counters, and run/group counters and transitions
them together. This operation is the sole expiry owner.

## 9. Stable response, error, and reason codes

### 9.1 Lua response envelope

Every non-error Lua response is an array:

```text
[STATUS, redis_now_ms, ...operation-specific scalar values]
```

Clients MUST switch exhaustively on these stable statuses:

```text
OK
CREATED
EXISTS_IDENTICAL
CANDIDATE_INSTALLED
LEGACY_RETIRED
CONTRACTS_PROMOTED
SEALED
ACTIVATED
AUDIT_STARTED
CLAIMED
ALREADY_CLAIMED
NO_CANDIDATE
VISITED_COMPLETED
RESERVED
ALREADY_RESERVED
STARTED
ALREADY_STARTED
FINISHED
ALREADY_FINISHED
RESERVATION_CANCELLED
RELEASED_READY
RENEWED
RETRY_SCHEDULED
STAGE_BEGUN
STAGED
STAGE_ABORTED
COMPLETED
DEAD
CANCELLED
COMMITTED
ALREADY_COMMITTED
CAPACITY_BLOCKED
RATE_BLOCKED
LEASE_CAPACITY_BLOCKED
STAGE_CAPACITY_BLOCKED
RUN_BUDGET_EXHAUSTED
RUN_RESERVATION_LIMIT_EXHAUSTED
GROUP_BUDGET_EXHAUSTED
DOWNSTREAM_BACKPRESSURE
AUTHORIZATION_EXPIRED
RUN_CANCELLED
LEASE_LOST
NOT_DUE
BATCH_MORE
BATCH_DONE
ARCHIVED
PURGED
```

`LEASE_LOST`, `AUTHORIZATION_EXPIRED`, and `RUN_CANCELLED` are definitive. A
transport timeout is ambiguous and MUST NOT be converted to one of those
statuses by the client.
`RUN_RESERVATION_LIMIT_EXHAUSTED` is a permanent result for new reservation
creation in that run, but does not revoke an existing lease or reservation.

All response scalars are Redis bulk strings containing canonical values. A
status not listed for an operation is a protocol error. The complete response
tail ordering is authoritative here:

| Operation/status | Exact response array |
|---|---|
| `CJ2_APPROVE_BOOT` / `OK`, `EXISTS_IDENTICAL` | `[status, now_ms, boot_epoch]` |
| `CJ2_MARK_PLANNED_SHUTDOWN` / `OK`, `EXISTS_IDENTICAL` | `[status, now_ms, planned_nonce]` |
| `CJ2_INSTALL_CANDIDATE_MARKERS` / `CANDIDATE_INSTALLED`, `EXISTS_IDENTICAL` | `[status, now_ms, manifest_sha256, contract_sha256]` |
| `CJ2_RETIRE_LEGACY_KEYS` / `LEGACY_RETIRED`, `EXISTS_IDENTICAL` | `[status, now_ms, deleted_bitmap, source_sha256]` |
| `CJ2_PROMOTE_CANDIDATE_CONTRACTS` / `CONTRACTS_PROMOTED`, `EXISTS_IDENTICAL` | `[status, now_ms, manifest_sha256, contract_sha256, commit_guard_sha256]` |
| `CJ2_CREATE_RUN` / `CREATED`, `EXISTS_IDENTICAL` | `[status, now_ms, run_id]` |
| `CJ2_ENQUEUE_BATCH` / `OK`, `EXISTS_IDENTICAL` | `[status, now_ms, new_jobs, reconciled_jobs, job_count, load_revision]` |
| `CJ2_BEGIN_RUN_AUDIT` / `AUDIT_STARTED`, `EXISTS_IDENTICAL` | `[status, now_ms, audit_revision, job_count]` |
| `CJ2_AUDIT_RUN_BATCH` / `BATCH_MORE`, `BATCH_DONE` | `[status, now_ms, checked, audit_count, audit_cursor_or_empty]` |
| `CJ2_SEAL_RUN` / `SEALED`, `EXISTS_IDENTICAL` | `[status, now_ms, job_count, source_sha256]` |
| `CJ2_ACTIVATE_RUN` / `ACTIVATED`, `EXISTS_IDENTICAL` | `[status, now_ms, activated_at_ms]` |
| `CJ2_REJECT_READY` / `DEAD` | `[status, now_ms, reason]` |
| `CJ2_TRY_CLAIM` / `CLAIMED`, `ALREADY_CLAIMED` | `[status, now_ms, fence, lease_expires_at_ms, reservation_id, reservation_expires_at_ms]` |
| `CJ2_TRY_CLAIM` / `VISITED_COMPLETED` | `[status, now_ms, completed_at_ms]` |
| claim/reserve / `CAPACITY_BLOCKED` | `[status, now_ms, scope_id, active_count, effective_concurrency, after_io]` |
| claim/reserve/start / `RATE_BLOCKED` | `[status, now_ms, scope_id, next_allowed_ms, after_io]` |
| claim/reserve / `RUN_BUDGET_EXHAUSTED` | `[status, now_ms, started, pending, limit, after_io]` |
| claim/reserve / `RUN_RESERVATION_LIMIT_EXHAUSTED` | `[status, now_ms, reservation_creations_total, maximum_reservation_creations, after_io]` |
| claim/reserve / `GROUP_BUDGET_EXHAUSTED` | `[status, now_ms, group_id, started, pending, limit, after_io]` |
| claim / `LEASE_CAPACITY_BLOCKED` | `[status, now_ms, active_leases, maximum_active_leases]` |
| claim or `CJ2_BEGIN_STAGE` / `STAGE_CAPACITY_BLOCKED` | `[status, now_ms, blocked_reason, active_stage_slots, maximum_stage_slots]` |
| `CJ2_RENEW_LEASE` / `RENEWED` | `[status, now_ms, lease_expires_at_ms]` |
| `CJ2_RESERVE_REQUEST` / `RESERVED`, `ALREADY_RESERVED` | `[status, now_ms, reservation_id, expires_at_ms]` |
| `CJ2_START_REQUEST` / `STARTED`, `ALREADY_STARTED` | `[status, now_ms, reservation_id, started_at_ms, delivery_attempts, job_request_starts, run_request_starts, group_request_starts, io_permission]` |
| `CJ2_FINISH_REQUEST` / `FINISHED`, `ALREADY_FINISHED` | `[status, now_ms, reservation_id]` |
| `CJ2_CANCEL_RESERVATION` / `RESERVATION_CANCELLED` | `[status, now_ms, reservation_id]` |
| `CJ2_RELEASE_BEFORE_IO` / `RELEASED_READY` | `[status, now_ms, ready_at_ms]` |
| `CJ2_RETRY` / `RETRY_SCHEDULED` | `[status, now_ms, not_before_ms, delivery_attempts, reason]` |
| `CJ2_RETRY` / `DEAD` | `[status, now_ms, dead_at_ms, retry_exhausted, last_failure_reason]` |
| `CJ2_RETRY` / `CANCELLED` | `[status, now_ms, cancelled_at_ms, reason]` |
| `CJ2_COMPLETE_NO_OUTPUT`, `CJ2_DEAD`, `CJ2_CANCEL_JOB` / matching terminal status | `[status, now_ms, terminal_at_ms, reason]` |
| `CJ2_BEGIN_STAGE` / `STAGE_BEGUN`, `EXISTS_IDENTICAL` | `[status, now_ms, commit_id, expires_at_ms, memory_reservation_remaining_bytes]` |
| any `CJ2_STAGE_*` / `STAGED`, `EXISTS_IDENTICAL` | `[status, now_ms, commit_id, chunk_kind, chunk_ordinal, accepted_records, data_bytes, key_count, memory_reservation_remaining_bytes]` |
| `CJ2_SEAL_STAGE` / `SEALED`, `EXISTS_IDENTICAL` | `[status, now_ms, commit_id, data_bytes, key_count]` |
| `CJ2_ABORT_STAGE` / `STAGE_ABORTED`, `EXISTS_IDENTICAL` | `[status, now_ms, commit_id, unlinked_keys]` |
| `CJ2_COMMIT` / `COMMITTED`, `ALREADY_COMMITTED` | `[status, now_ms, publication_id, commit_id, completed_at_ms]` |
| blocked `CJ2_COMMIT` / `DOWNSTREAM_BACKPRESSURE` | `[status, now_ms, blocked_reason, blocked_started_at_ms, blocked_deadline_ms]` |
| `CJ2_PROMOTE_DUE`, `CJ2_RECOVER_EXPIRED`, `CJ2_CANCEL_BATCH`, `CJ2_CLEAN_STAGE`, `CJ2_MAINTAIN_RATE_SCOPES` | `[BATCH_MORE or BATCH_DONE, now_ms, processed, more]` |
| `CJ2_CANCEL_RUN` / `CANCELLED`, `EXISTS_IDENTICAL` | `[status, now_ms, cancelled_at_ms, reason]` |
| `CJ2_FINALIZE_RUN` / `COMPLETED`, `RUN_BUDGET_EXHAUSTED`, `RUN_RESERVATION_LIMIT_EXHAUSTED`, `GROUP_BUDGET_EXHAUSTED`, `CANCELLED`, `NOT_DUE` | `[status, now_ms, finalized_at_ms_or_zero, terminal_reason]` |
| `CJ2_ARCHIVE_RUN` / `ARCHIVED`, `EXISTS_IDENTICAL` | `[status, now_ms, archived_at_ms, archive_sha256]` |
| `CJ2_ARCHIVE_RUN` / `NOT_DUE` | `[status, now_ms, eligible_at_ms]` |
| `CJ2_PURGE_RUN_BATCH` / `BATCH_MORE`, effective `PURGED` | `[status, now_ms, removed_jobs, more]` |
| `CJ2_PURGE_RUN_BATCH` absent-state final replay / `PURGED` | `[PURGED, now_ms, 0, 0]` |
| `CJ2_PURGE_RUN_BATCH` / `NOT_DUE` | `[status, now_ms, eligible_at_ms]` |
| `NO_CANDIDATE`, `AUTHORIZATION_EXPIRED`, `RUN_CANCELLED` | `[status, now_ms]` |
| `LEASE_LOST` | `[status, now_ms, current_fence_or_zero]` |

`after_io`, `more`, and `io_permission` are exactly `0` or `1`.
`maximum_reservation_creations` is the literal canonical decimal bulk string
`100`.
Where the response includes a `more` scalar, `BATCH_MORE` carries `more=1` and
`BATCH_DONE` or `PURGED` carries `more=0`.
In the `CJ2_RETRY` third-delivery `DEAD` response, the fourth element shown as
`retry_exhausted` is the exact literal Redis bulk string `retry_exhausted`, not
a boolean, field name, or caller-supplied value.
For `CJ2_CLEAN_STAGE`, `processed` is `1` only when the expected bundle is
logically removed and otherwise `0`; for the other batch operations it is the
number of jobs or scope records transitioned/validated.
`deleted_bitmap` is five ASCII
bits in the legacy-key order in section 13: `1` means the evidenced key existed
and was submitted to `UNLINK`, while `0` means its validated type was `none`.
Operation request fields are also
authoritative semantic suffixes: they appear in the order named in the
corresponding transition section, and the transition payload record uses those
same names and order. Every operation except `CJ2_APPROVE_BOOT` first carries
this exact seven-field transport gate prefix:

```text
gate_mode
expected_boot_epoch
expected_contract_sha256_or_empty
expected_compatibility_record_or_empty
expected_commit_guard_record_or_empty
expected_legacy_retirement_record_or_empty
expected_admin_freeze_record_or_empty
```

`gate_mode` is exactly `boot_only`, `candidate`, or `active`, as fixed for the
operation. Each nonempty record is the binary `RECORD` encoding from section 4,
with names and values in the exact field order specified in section 5; it is an
explicit exception to the default UTF-8 argument rule. Lua boundedly decodes the
record, requires the Redis hash to have exactly that field count, and compares
every named value. An empty record is a zero-byte bulk string and requires the
corresponding key to be absent unless the operation explicitly defines an
idempotent post-state replay.

`boot_only` requires the contract argument and all four artifact records empty;
operation-specific rules decide which marker keys must be absent. `candidate`
requires the exact candidate contract, compatibility record, and freeze record,
with the guard absent; its legacy record is empty before retirement and exact
after retirement. `active` requires the exact active contract, compatibility,
guard, and legacy-retirement records and an empty freeze record. Candidate keys
must be absent in active mode. These transport fields are excluded from every
identity and semantic field count. Lua source tests MUST assert every gate
prefix, semantic request field list, and response vector; source and generated
Go/Python bindings are rejected if any differ from this document or each other.

The operation-to-mode mapping is closed. `CJ2_APPROVE_BOOT` has no prefix;
`CJ2_INSTALL_CANDIDATE_MARKERS` uses `boot_only`; and
`CJ2_RETIRE_LEGACY_KEYS` plus `CJ2_PROMOTE_CANDIDATE_CONTRACTS` use `candidate`
(promotion's exact lost-response replay validates its explicitly defined active
post-state). The eight candidate run-data operations listed in section 10.2 use
`candidate` only for that finite migration/abort path and otherwise use
`active`. Every other mutating operation and every monitoring `EVALSHA_RO` query
uses `active`; no caller may select another mode for an operation.

### 9.2 Redis error codes

Prevalidation failures use only `ERR CRAWL_V2_<CODE>` with one of:

```text
BOOT_UNAPPROVED
COMPATIBILITY_MISMATCH
CONTRACT_MISMATCH
WRONG_TYPE
INVALID_ARGUMENT
INVALID_IDENTIFIER
INVALID_NUMBER
INVALID_STATE
IMMUTABLE_MISMATCH
URL_ID_COLLISION
LIMIT_EXCEEDED
COUNTER_CORRUPT
STATE_INDEX_CORRUPT
RESERVATION_CORRUPT
RATE_STATE_CORRUPT
STAGE_INVALID
STAGE_UNSEALED
DESTINATION_EXISTS
OUTPUT_CONTRACT_MISMATCH
COMMAND_BOUNDS_EXCEEDED
MEMORY_HEADROOM_LOW
RATE_SCOPE_CAPACITY_EXCEEDED
ADMIN_FREEZE_REQUIRED
COMMIT_GUARD_UNAPPROVED
```

Request-transcript and freeze failures use these existing codes:

| Condition | Result |
|---|---|
| Noncanonical or out-of-exact-integer-range input baseline/generation | `INVALID_NUMBER` |
| Canonical input pair not satisfying `0 <= baseline < generation <= 10` | `INVALID_ARGUMENT` |
| Valid but stale B/G on a first BEGIN against an otherwise valid current job | `STAGE_INVALID`; no stage, freeze, or other mutation |
| Active request reservation on a first BEGIN | `INVALID_STATE` |
| Changed immutable BEGIN input, including B/G, under an existing active stage ID | `IMMUTABLE_MISMATCH` |
| New reservation/start, or non-replay second BEGIN, on a frozen current fence | `INVALID_STATE` |
| Invalid stored job B/G relation, or an established current owned stage whose stored B/G disagrees with its job | `COUNTER_CORRUPT` |
| Stale ownership or a different completed commit identity | `LEASE_LOST`, except an explicitly permitted exact reconciliation |

After gate, input-shape, and lexical validation, operation-specific exact replay
ordering applies: historical START and completed COMMIT reconciliation precede
live-lease/admission checks, identical active BEGIN precedes one-stage/admission
checks, post-abort replay precedes stage-key existence checks, and identical
seal replay precedes the unsealed-state requirement. An active-stage replay
still validates its current owner, frozen tuple, and applicable lifetime gates.
A current owned stage must be established before classifying stored tuple drift
as corruption; a merely stale caller snapshot is not corruption. No caller or
script may repair a failure by silently replacing B/G or changing an existing
stage's identity. All listed errors precede mutation, including monotonic rate
tightening and commit-backpressure bookkeeping.

Errors MUST NOT append key names supplied by untrusted data or stored values.
Any corruption error blocks the affected run; clients MUST NOT guess a repair.

### 9.3 Stable disposition reasons

Completion reasons:

```text
published
already_visited
```

Retryable failure reasons:

```text
request_timeout
dns_temporary
dial_temporary
request_temporary
http_429
http_5xx
robots_temporary
renderer_temporary
downstream_backpressure
capacity_blocked_after_io
run_budget_exhausted_after_io
group_budget_exhausted_after_io
rate_blocked_after_io
lease_expired_after_io
worker_shutdown_after_io
```

Dead-letter reasons:

```text
policy_denied
policy_scope_changed
robots_denied
robots_invalid
job_malformed
url_identity_mismatch
static_url_denied
dns_prohibited
http_4xx
response_invalid
body_too_large
html_invalid
discovery_limit
renderer_permanent
output_invalid
run_job_limit
reservation_limit_exhausted
retry_exhausted
pre_io_recovery_exhausted
protocol_corrupt
```

Cancellation reasons:

```text
authorization_expired
operator_cancelled
source_cancelled
```

Run terminal reasons are `all_jobs_terminal`, `request_budget_exhausted`,
`reservation_limit_exhausted`, `group_budgets_exhausted`,
`authorization_expired`, `operator_cancelled`, and `source_cancelled`.
Implementations MUST map typed crawler, secure-fetch,
robots, renderer, policy, budget, and HTTP outcomes to these closed sets; they
MUST NOT classify by matching free-form error text.

`capacity_blocked_after_io`, `run_budget_exhausted_after_io`,
`group_budget_exhausted_after_io`, and `rate_blocked_after_io` are legal only
after at least one start on the current fence and only after the corresponding
reserve result. `downstream_backpressure` additionally requires either this
fence's `CJ2_BEGIN_STAGE` capacity block or its durable commit-backpressure
deadline. `retry_exhausted` is
written only by the third-delivery branch of retry/recovery;
`pre_io_recovery_exhausted` only by the third pre-I/O expiry recovery; and
`policy_scope_changed` only for a source/ready job that no longer matches the
exact reviewed policy binding before DNS. Misusing a reason is
`INVALID_ARGUMENT`, not a different classification. `protocol_corrupt` is for a
typed malformed per-job worker result while the Redis ledger remains
consistent; a `CRAWL_V2_*_CORRUPT` script error instead stops the affected run
and is never converted into a job disposition.

`reservation_limit_exhausted` is legal only for `CJ2_DEAD` on a current lease
whose fence already performed I/O, has no active reservation or stage, and just
received `RUN_RESERVATION_LIMIT_EXHAUSTED` while attempting the next required
request. It is terminal because the run-global counter never decreases; it is
not a retry reason. A pre-I/O lease receiving that status must instead use
`CJ2_RELEASE_BEFORE_IO`.

The caller/server reason matrix is closed:

| Transition | Permitted reason source |
|---|---|
| `CJ2_REJECT_READY` | Caller: `policy_denied`, `policy_scope_changed`, `job_malformed`, `url_identity_mismatch`, or `static_url_denied` |
| `CJ2_RETRY` | Caller: any retryable reason except `lease_expired_after_io`; the four `*_after_io` capacity/budget/rate reasons additionally require their matching blocked response |
| `CJ2_DEAD` | Caller: any dead-letter reason except `policy_scope_changed`, `retry_exhausted`, or `pre_io_recovery_exhausted` |
| `CJ2_COMPLETE_NO_OUTPUT` | Caller: only `already_visited`; `CJ2_COMMIT` alone writes `published` |
| `CJ2_CANCEL_RUN` | Caller: `operator_cancelled` or `source_cancelled`; maintenance alone supplies `authorization_expired` |
| `CJ2_CANCEL_JOB` | Caller: exactly the run's recorded, or authorization-derived effective, cancellation reason |
| `CJ2_RECOVER_EXPIRED` | Server: `lease_expired_after_io`, `retry_exhausted`, `pre_io_recovery_exhausted`, or the run's cancellation reason according to its branch |

No other operation/reason pairing is valid. `CJ2_CANCEL_BATCH` copies the run's
reason, and visited claim completion writes `already_visited` without accepting
a caller-selected reason.

## 10. Lua transition contract

These named operations, including the bounded stage operations below, are the
only operations permitted to mutate Crawl Jobs V2 state outside the exact
disposable setup/teardown exception in section 5.1. Direct `HSET`, `SET`, `ZADD`,
`SADD`, `LPUSH`, expiry, rename, or deletion by a service client is forbidden
even for staging; section 5.1 does not authorize a runtime or crawl-admin client
to bypass a transition.

`NOSCRIPT` is fail-closed, not permission to fall back to `EVAL` or alternate
source. The client re-runs connection/boot/marker checks, loads the exact
SHA-256-verified source with `SCRIPT LOAD`, verifies the returned Redis SHA-1,
and only then retries the same `EVALSHA` identity.

### 10.1 Common script rules

Every script MUST call Redis `TIME` exactly once and have distinct
**prevalidation/build** and **mutation** phases.
Redis does not roll back writes made before a Lua runtime error, so a script
MUST validate all of the following before its first write:

- the exact permitted boot and active/candidate marker mode;
- key count, argument count, serialized command bound, and batch bound;
- every key type it may read or mutate;
- canonical identifier and numeric forms;
- run/job/reservation state and exact index membership;
- owner, token, fence, and nonexpired lease where applicable;
- authorization and request budgets where applicable;
- retained job baseline/start relations, the request-admission freeze, and the
  exact current-fence stage baseline/generation where applicable;
- every immutable existing value and every destination type;
- stage-slot and memory-reservation arithmetic where applicable;
- memory headroom for the exact bounded mutation growth.

Before the first write, a script MUST also build every Lua table, destination
key, canonical argument array, mutation call descriptor, and complete response
array it will use. Array lengths and `unpack` ranges are checked against fixed
limits. After the first write there may be no parsing, sorting, hashing, string
concatenation, table growth, destination derivation, memory calculation,
response construction, or branch dependent on a Redis mutation reply. The
mutation phase consists only of the prebuilt, fixed-order bounded Redis calls
whose key types, ACL permissions, argument forms, integer ranges, destination
conditions, and allocation bounds were already proved. Return values may be
discarded or compared only in test assertions; they cannot choose a later
write.

The authoritative Lua source is accepted only after static review proves this
post-first-write property and maximum-shape real-Redis tests exercise every
call boundary. Unexpected script errors after the first mutation remain a
protocol-integrity incident requiring all writers to stop and coordinated
restore/reconciliation; they are not described as an atomic rollback. A release
with any such observed path MUST NOT receive a commit guard or active marker.

Scripts MUST NOT use `KEYS`, unbounded `SCAN`, unbounded collection reads,
client timestamps, random Lua values, or user-provided retry delays. `KEYS[]`
must contain only fixed control/run/stage keys; dynamic output keys are derived
from validated bounded stage data because Redis Cluster is unsupported.

The reviewed Lua sources include one bounded SHA-256 implementation and MUST
recompute every control identity whose complete bounded input is available to a
transition: URL IDs; target, token, and scope digests; policy-decision and group-
map digests; transition payload/transition IDs; reservation IDs; and publication
and commit IDs. Redis SHA-1 helpers are not substitutes. Client-supplied
`chunk_digest`, `source_sha256`, `output_digest`, and artifact/evidence digests
are the explicit exceptions because their source can be multi-MiB or span
bounded batches. Compatible clients calculate those values using section 4;
shared vectors, exact immutable propagation, byte-for-byte replay checks, run
audit, and independent pre-seal reads verify them. Lua does not recalculate
SHA-256 over multi-MiB HTML. The pinned Spider/feeder image is therefore part of
that bulk-digest correctness trust boundary. The pinned Spider also authenticates
and checks complete request-event coverage, enforces one I/O attempt per
reservation despite separately parsed replay replies, and establishes network
outcomes and response-byte/redirect provenance. Lua's B/G compare-and-freeze and
subsequent tuple checks bind that trusted-client proof to publication; they do
not attest those network facts independently. This trust does not weaken lease
fencing: only Redis decides whether the immutable stage can be sealed or
published. Adding a hash chain without changing the credential/image boundary
would not make a hostile client trustworthy.

Redis ACL does not provide privilege elevation inside Lua: a caller permitted
to run a script must also be permitted to invoke each Redis command/key touched
by that script, so ACL alone cannot distinguish a conforming `EVALSHA` from a
direct data command using the same credential. V2 therefore uses ACL to deny
administrative commands, unrelated keyspaces, marker promotion, boot approval,
and legacy retirement, while immutable image review and static/integration tests
enforce script-only data mutation. Treat compromise of a runtime credential or
image as compromise of the ledger. Making script-only mutation a hostile-client
security boundary would require a separately authenticated transition gateway
and a new architecture decision; this release does not pretend the ACL supplies
that property.

### 10.2 Administrative and run preparation transitions

#### `CJ2_APPROVE_BOOT`

Boot-admin only and the sole operation allowed while boot is unapproved. Its
ordered request fields are `current_redis_run_id`, `proposed_boot_epoch`,
`evidence_sha256`, `evidence_at_ms`, `loss_bound`, `planned_nonce_or_empty`,
`planned_shutdown_evidence_sha256_or_empty`, and `approval_mode`. It compares
`current_redis_run_id` with the actual `INFO server`
run ID, validates the exact evidence digest, age, zero loss bound, and either the
`initial` mode against an absent durability hash, the `planned` mode's matching
nonce and process-stop evidence digest, or explicit `unclean_rehearsal` mode.
The first effective `planned` or `unclean_rehearsal` approval also requires the
actual run ID to differ from the previously approved run ID; aborting a planned
shutdown therefore requires completing a Redis restart rather than relabeling
the same process. The post-state replay below instead requires the now-approved
run ID to equal the actual one.
Initial mode still requires current
abrupt-restart evidence; existing unrelated/legacy keys do not make it a restart
approval shortcut. It prebuilds the entire durability hash, then changes only
that hash: a fresh tool-supplied `boot_epoch`, `boot_state=approved`, and
consumed planned nonce plus the approval mode, consumed-nonce, and preserved
process-stop evidence. It cannot
read or mutate crawl, marker, stage, rate, or output keys. Repeating the exact
approval is `EXISTS_IDENTICAL` after first validating the current run ID and the
complete already-approved durability record; this narrow post-state replay does
not reopen approval to another input. Conflicting input fails.

#### `CJ2_INSTALL_CANDIDATE_MARKERS`

Release-admin only. Its ordered request fields are `freeze_nonce`,
`process_stop_evidence_sha256`, `contract_sha256`, then every compatibility
marker field from `manifest_version` through `render_worker_image` in the exact
section 5 order. No separately named marker value is repeated. It requires
approved boot, active compatibility/contract/commit-guard
keys absent, candidate keys absent or identical, every crawl producer and
consumer stopped, and a reviewed process-stop evidence digest. It creates the
exact candidate marker pair and `admin_freeze` record atomically. It authorizes
no request, claim, stage, output, or runtime consumer mutation.

The finite candidate **run-data** exception set is exactly `CJ2_CREATE_RUN`,
`CJ2_ENQUEUE_BATCH`, `CJ2_BEGIN_RUN_AUDIT`, `CJ2_AUDIT_RUN_BATCH`,
`CJ2_SEAL_RUN`, `CJ2_CANCEL_RUN`, `CJ2_CANCEL_BATCH`, and
`CJ2_PURGE_RUN_BATCH`, only for `source_kind=v1_migration`. Each requires the
migration-admin credential, approved boot, exact candidate markers, active
markers absent, and the exact `admin_freeze` nonce/evidence. No other operation
may mutate candidate run data. Candidate-marker installation, exact legacy
retirement, and promotion are the separate administrative exceptions defined in
this section; they cannot claim, stage, or publish.

#### `CJ2_RETIRE_LEGACY_KEYS`

Migration-admin only. Its ordered request fields are `freeze_nonce`,
`backup_sha256`, `v1_count`, `v1_url_field_count`, `v1_depth_field_count`,
`v1_source_sha256`, `v1_queue_evidence_sha256`,
`v1_urls_evidence_sha256`, `v1_depths_evidence_sha256`, `spider_queue_type`, `spider_queue_count`,
`spider_queue_evidence_sha256`, `signal_queue_type`, `signal_queue_count`, and
`signal_queue_evidence_sha256`, followed by `confirmation_text`. The confirmation
is exactly the colon-joined sequence `freeze_nonce`, `backup_sha256`, `v1_count`,
`v1_source_sha256`, then the queue, URLs, depths, historical-spider, and signal
evidence digests in that order. It prevalidates the exact five literal keys
and their cardinalities against the stopped tool's independently re-read section
13 evidence. For `v1_count=0`, all three V2 run inventories must be empty. For a
positive count, they must contain exactly one audited/sealed candidate-mode V1
migration run whose expected count, job count, and source digest match the
request. It prebuilds a retirement record containing all request digests, the
five-bit presence/deletion bitmap, and Redis time, and atomically stores that
record while `UNLINK`ing only those keys. Lua does not re-read/hash up to 10,000
bulk records in this bounded operation; the frozen process set, restricted
migration-admin image, unchanged before/after digest, and evidence artifact are
part of this administrative trust boundary. A wrong type/cardinality or changed
evidence aborts all deletion. Exact replay requires the immutable record and all
five keys absent; absence without that record is not proof of an earlier call.
It never uses a pattern, `SCAN`, `FLUSHDB`, or `FLUSHALL`.

#### `CJ2_PROMOTE_CANDIDATE_CONTRACTS`

Release-admin only. Its ordered request fields are `freeze_nonce`,
`commit_guard_sha256`, then the guard-core fields in this exact order:
`protocol_version`, `contract_sha256`, `redis_version`, `redis_config_sha256`,
`maximum_shape_sha256`, `memory_fixture_sha256`, `lua_benchmark_sha256`,
`aof_crash_evidence_sha256`, `cutover_mode`, `candidate_run_id`, and `approved`.
The contract digest must match the candidate contract. The operation obtains
`compatibility_manifest_sha256` from the candidate marker and
`approved_at_ms` from its one Redis `TIME` call; neither is caller supplied.
`cutover_mode` is `fresh` or `v1_migration`. In `fresh` mode the candidate run
ID is empty, the retirement record has `v1_count=0`, and `runs`, `active_runs`,
and `unarchived_runs` contain no V2 run.
In `v1_migration` mode all three inventories contain exactly the one supplied
candidate run; it is sealed and audited, has `source_kind=v1_migration`, and its
source digest, expected count, and job count equal the retirement record whose
`v1_count` is from 1 through 10,000, with at least 60 seconds of authorization
remaining. In either mode, `first_request_start` and the global stage-slot,
stage-expiry, and
rate-scope and active-lease inventories are absent; every present run has zero claims, request
starts, reservation creations, active reservations, leases, stages, and output
commits.

The operation also requires approved boot, exact candidate markers, active
markers and guard absent, all five legacy keys absent, unchanged freeze
evidence, `lazyfree_pending_objects=0`, every reviewed downstream source,
processing, and dead-letter list empty, every consumer owner lock absent, and a
complete stopped-world backlink scan with zero pending members. It also requires
exact immutable legacy-retirement evidence, exact target-image/config digests,
and approved maximum-shape, memory, Lua, AOF, and process-kill evidence. Every
guard evidence digest must be nonzero and unequal to section 5.1's
`ZERO_SHA256`; no provisional guard/manifest digest is accepted. Its one
mutation phase installs the prebuilt
commit guard, renames the candidate compatibility hash to active, renames the
candidate contract string to active, and removes `admin_freeze`. No client can
observe a partially promoted pair because the script is atomic. Exact replay
checks the stored guard's identical cutover mode/run identity before the
pre-promotion inventory gates and returns `EXISTS_IDENTICAL`; it can therefore
reconcile a lost response without requiring a now-active system to resemble its
pre-promotion state.

#### `CJ2_MARK_PLANNED_SHUTDOWN`

Operator-only. Its ordered semantic request fields are
`planned_shutdown_nonce`, `process_stop_evidence_sha256`, `active_run_count`,
then the active run IDs in byte order. Its fixed key list includes
`active_runs`, those at-most-16 run hashes and leased indexes, the global
active-lease, stage-slot, and stage-expiry indexes, the global rate scope and its
three reservation indexes, and the exact reviewed Indexer and Image Indexer
owner-lock keys. The process-stop evidence accounts for the lock-free,
concurrency-safe Backlinks Processor replicas. Its hashed artifact binds the
nonce, current Redis run ID and boot epoch, sorted active-run list, exact stopped
process/image inventory, and the independently observed zero reservation,
lease, stage-slot, stage-expiry, owner-lock, feeder, and audit counts. It requires
approved boot, exact active markers, matching active-run inventory, zero
per-run/global leases and reservations, empty stage-slot and stage-expiry
indexes, absent owner locks, and stopped feeder/audit activity, then records
`boot_state=planned` and a single-use tool-supplied nonce.
It stores the submitted process-stop digest in
`planned_shutdown_evidence_sha256` in the same mutation.
It does not shut Redis down. An exact lost-response retry may return
`EXISTS_IDENTICAL` while that same Redis process still has the complete planned
record and nonce; it performs no write and does not permit any other operation
to treat `planned` as an approved runtime state. This no-write reconciliation is
the sole post-state exception to the operation's approved-boot gate: it first
requires the unchanged actual Redis run ID, boot epoch, active records, and full
planned-shutdown request/record.

#### `CJ2_CREATE_RUN`

Its ordered request fields are `run_id`, `source_kind`, `source_sha256`,
`expected_seed_count`, `authorization_sha256`, `authorization_scope_sha256`,
`authorization_expires_at_ms`, `canonicalization_version`,
`canonicalization_sha256`, `crawl_policy_version`, `crawl_policy_sha256`,
`render_policy_version`, `render_policy_sha256`, `policy_group_map_sha256`,
`max_jobs`, `max_request_starts`, `global_concurrency_limit`,
`max_delivery_attempts`, then `policy_group_count` and the exact group tuples in
group-ID byte order. State, counters, revisions, and Redis timestamps are never
client fields. Before adding the run, it requires fewer than 16 active runs,
fewer than 100 unarchived runs, and fewer than 128 total `runs` members, plus a
valid future authorization no more than 24 hours away, all pinned digests, one
through 64 exact crawl-policy V2 group tuples, and limits within this contract. Group-map
fields and zero-valued
cumulative-started, pending, active-started, and open counters are installed in
this same bounded operation. `reservation_creations_total` is installed as
canonical `0`. It also creates `retry_reason_counts`,
`recovery_outcome_counts`, and `disposition_reason_counts` with exactly every
closed field initialized to canonical `0`; `audit_group_counts` remains absent
until audit begins. Purge fields start as `none`, empty, `0`, and `0` in schema
order. The map digest must match
`policy_group_map_sha256`. It creates one `loading` run and adds it to `runs`,
`active_runs`, and `unarchived_runs`, with `load_revision=1` and
`audit_revision=0`. Active mode is used for Mongo sources;
candidate mode is permitted only for stopped V1 migration as defined above.
Candidate-mode creation requires `expected_seed_count` from 1 through 10,000;
a zero-member cutover uses `fresh` mode and creates no candidate run.
Repeating an exact request returns `EXISTS_IDENTICAL`; any field difference is
`IMMUTABLE_MISMATCH`.

#### `CJ2_ENQUEUE_BATCH`

Its ordered request fields are `run_id`, `record_count`, then at most 500
ordered records; `record_count` is from 1 through 500. Each record is
`(job_id, canonical_url, score_text, depth,
group_id, rate_scope_id, group_scope_id, initial_origin_scope_id,
policy_decision_sha256)` in that exact order. The operation requires a `loading`
run, and every record is already policy-bound. It prevalidates the entire batch,
URL identity, submitted group tuple against the run map, decision digest, final
`job_count <= expected_seed_count <= max_jobs`, state key
types, duplicates, source ordering, and existing immutable values before any
write. New jobs receive fixed-shape hashes, `jobs`/`job_order`, ready/ready-at
membership, `lease_request_starts_baseline=0` and `request_starts=0`, and
increment run/group open counts. Exact replays are no-ops, including for the
baseline and start counter. A URL mismatch is `URL_ID_COLLISION`; any changed binding is
`IMMUTABLE_MISMATCH`. Source jobs are immutable once inserted—score/depth
reconciliation is not applied by feeder replay.

#### `CJ2_BEGIN_RUN_AUDIT` and `CJ2_AUDIT_RUN_BATCH`

`CJ2_BEGIN_RUN_AUDIT` receives only `run_id` after the common gate fields and
changes `loading -> auditing`, freezes `load_revision` as
`audit_revision`, initializes an empty cursor/count, and creates
`B:audit_group_counts` with exactly one zero field for every run group. No
enqueue is permitted afterward. `CJ2_AUDIT_RUN_BATCH` accepts `run_id`,
`expected_prior_cursor`, `expected_prior_count`, `record_count` from 0 through
100, then that many records after the cursor with the exact nine-field enqueue-
record shape and order. Zero records are valid only for the initial/final audit
of an empty source run; a nonempty run requires at least one record per batch.
It reads `job_order` lexicographically, requires byte-for-byte
agreement with the supplied records and every job hash, ready index, group map,
and counter contribution, then atomically increments the selected groups'
bounded audit counters and advances the cursor. It returns `BATCH_DONE` only
after exactly `job_count` records, every audit-group count equals the matching
`group_open_jobs` value, and both sums equal
`job_count=expected_seed_count`. This replaces an unbounded 10,000-job seal
script. A failed or
changed record mutates no audit state. If a response is lost, the exact request
whose computed end cursor/count equal current audit state is revalidated and
returns its original `BATCH_MORE` or `BATCH_DONE` without advancing twice; any
other stale prior cursor is `IMMUTABLE_MISMATCH`.

#### `CJ2_SEAL_RUN`

Its ordered request fields are `run_id`, `expected_job_count`, and
`source_sha256`. It requires `auditing`, `audit_complete=1`,
`audit_revision=load_revision`, `audit_count=job_count=expected_seed_count`, and
every audit-group count equal to its frozen group-open count, plus the exact
immutable source digest already independently recomputed over the
audited record stream. It performs only constant-size run/counter checks, then
sets `state=sealed` and `sealed_at_ms`. Exact replay returns
`EXISTS_IDENTICAL`; a changed digest or count fails without mutation.

#### `CJ2_ACTIVATE_RUN`

Its ordered request fields are `run_id`, `confirmation_text`,
`authorization_sha256`, `crawl_policy_sha256`, `render_policy_sha256`, and
`canonicalization_sha256`. It requires the section 12 exact confirmation and at
least 60 seconds of authorization remaining. It validates the submitted/local
artifact digests and the already installed bounded policy group map. It requires exact
active markers and commit guard and absence of all five literal legacy keys:
the three namespaced V1 keys, literal historical `spider_queue`, and retired
`signal_queue`, plus exact immutable retirement evidence. It changes only
`sealed -> active`. Normal workers MUST NOT
autoactivate a sealed run. Exact replay against the same active run is
`EXISTS_IDENTICAL`.

### 10.3 Claim and request transitions

Unless an operation below explicitly replaces it, `lease_identity` means the
ordered request fields `run_id`, `job_id`, `owner_id`, `lease_token`, and
`fence`. Renew receives exactly that identity. Start, finish, and reservation
cancel append `reservation_id`. Lease-ending outcome operations append their
operation-specific fields in prose order and then `transition_id`; a reason, if
present, precedes the transition ID. Common boot/marker arguments are transport
gate fields and are not part of `transition_payload_digest`.

#### `CJ2_REJECT_READY`

Moves one ready job directly to dead only for a pre-network terminal reason
permitted by the reason matrix, including malformed identity, policy denial, or
`policy_scope_changed`, and only for an active run with unexpired authorization.
It validates the exact inspected URL, score, depth, and
immutable policy binding, removes ready/open counters, and creates no lease or
reservation. It sets both `last_reason` and `last_failure_reason` to the submitted
dead-letter reason. Its ordered fields are `run_id`, `job_id`, `canonical_url`,
`score_text`, `depth`, `group_id`, `rate_scope_id`, `group_scope_id`,
`initial_origin_scope_id`, `policy_decision_sha256`, `reason`, then
`transition_id` (fence `0`, empty token). It is idempotent by its
payload-and-reason-bound transition ID.

#### `CJ2_TRY_CLAIM`

Its ordered request fields are `run_id`, `job_id`, `canonical_url`,
`score_text`, `depth`, `job_group_id`, `job_rate_scope_id`,
`job_group_scope_id`, `job_initial_origin_scope_id`,
`job_policy_decision_sha256`, `expected_prior_fence`, `fence`, `owner_id`,
`lease_token`, then the complete first-intent suffix from
`CJ2_RESERVE_REQUEST`—`request_ordinal`, `request_kind`, `target_url_id`,
`canonical_target_url`, `target_digest`, `crawl_policy_sha256`,
`policy_decision_sha256`, `group_id`, `rate_scope_id`, `global_scope_id`,
`group_scope_id`, `origin_scope_id`, `global_concurrency`,
`global_interval_ms`, `group_concurrency`, `group_interval_ms`,
`origin_concurrency`, `origin_interval_ms`, and finally `transition_id`. The
script requires
`fence=expected_prior_fence+1`, validates all candidate, lease, reservation, and
transition identities against current state using transition reason `none`, and
does not accept a submitted `reservation_id`. The first intent is chosen without
network I/O. Its `request_kind` is exactly `robots`, or `document` only when a
valid local robots decision means no robots request is needed; claim rejects
`redirect` and `render_resource`. That initial intent must use the job's
immutable `group_id`, `rate_scope_id`, and `group_scope_id`; a robots request
inherits the source document binding and cannot independently select another
group. For either
initial kind, the target's recomputed canonical origin and submitted
`origin_scope_id` must equal the job's immutable `initial_origin_scope_id`.
The intent-specific policy-decision digest is still recomputed over its actual
request kind and target. If that intent changes before start, the worker must
cancel it and reserve the replacement; it cannot reuse the reservation ID.

Before mutation it MUST:

1. Require an active, unexpired run.
2. Validate the job hash and ready/ready-at membership against the inspected
   URL, score, and depth.
3. Validate any `visited_urls[url_id]` witness against the candidate canonical
   URL. If it matches and `visited_depth[url_id] <= depth`, atomically
   terminalize the job as `completed/already_visited` and return
   `VISITED_COMPLETED` without capacity; a different witness is
   `URL_ID_COLLISION`, and presence of only one visited field is
   `STATE_INDEX_CORRUPT`. The terminal branch stores the claim transition ID so
   an exact lost-response replay returns the original completion without a
   lease.
4. Require `request_starts + pending_request_reservations <
   max_request_starts`.
5. Require `reservation_creations_total < 100`. At `100`, return
   `RUN_RESERVATION_LIMIT_EXHAUSTED` with `after_io=0`; do not remove ready
   membership or create a lease/reservation. The only possible write is the
   fully prevalidated monotonic scope tightening permitted by section 8.5.
6. Require `group_started[group_id] + group_pending[group_id] <
   group_limits[group_id]`.
7. Validate every submitted policy/rate tuple against the run map, load or
   monotonically tighten each scope under section 8.5, and validate exact
   active/pending/started cardinalities. It never prunes an expired member.
8. Require concurrency capacity and no conflicting pending interval
   reservation; require Redis time at or beyond every `next_allowed_ms`.
9. Require `delivery_attempts < 3`, fewer than 64 global active leases, fewer
   than four stage slots, the durable rate-scope inventory below its bound for
   any new scope, and a safely representable fence increment. A full lease
   inventory returns `LEASE_CAPACITY_BLOCKED`; a full stage inventory returns
   `STAGE_CAPACITY_BLOCKED`, both before reserving request capacity.

The mutation phase first creates the exact pending reservation and inserts it
into every scope's `active` and `pending` indexes, increments all active/pending
scope counters plus `pending_request_reservations` and
`group_pending[group_id]`, increments `reservation_creations_total` exactly
once, updates the rate-scope inventory, then removes ready membership, writes
the per-run lease with `lease_request_starts_baseline` equal to the prevalidated
pre-claim job `request_starts`, and inserts
the exact global active-lease member at the same expiry. These effects are one
atomic script. Any blocked status may persist only the section 8.5 monotonic
tightening of existing scopes; an error changes nothing.

A new claim increments `claim_count` and fence but not delivery attempts or
request starts. The captured baseline is a server field, not a claim argument
or response field. Repeating with the same owner, token, candidate, and exact
active pending outbound reservation returns `ALREADY_CLAIMED` with the existing
fence and expiry and changes no counter or baseline. This exact-current replay
is checked before new budget, reservation-creation, capacity, rate, or stage
availability. Another
identity receives `LEASE_LOST` or `NO_CANDIDATE`.

#### `CJ2_RENEW_LEASE`

Requires the exact active owner/token/fence and `now < lease_expires_at_ms`.
Without a stage, it sets the job lease and leased ZSET score to `now+60000`.
With an active stage, it first validates the exact metadata, frozen
baseline/generation equality with the job, slot, and expiry membership and sets
both lease deadlines to
`min(now+60000, stage_expires_at_ms)`; a lease can never be renewed beyond its
stage's absolute lifetime. In either branch it validates and sets the matching
global active-lease score to the same deadline. If one reservation is active, it extends the
reservation and, for each of its three scopes, the
`:active` score and exact matching `:pending` or `:started` secondary score to
the same expiry. It
never shortens a deadline. Budget exhaustion does not prevent renewal.
Authorization expiry or run cancellation returns the corresponding definitive
status and does not renew.
An exact aborted-stage terminal reservation makes renewal `INVALID_STATE`; only
the serialized lease-ending transition or later expiry recovery may proceed.

For a lexically valid request against an existing unfinalized run/job, each
definitive expired-lease or owner/token/fence rejection increments
`renewal_rejections_total` once before returning `LEASE_LOST`; malformed,
marker-failed, missing, terminal, or corruption paths do not. This is an
invocation counter rather than an idempotent business transition, so a caller
retry after an ambiguous transport result may increment it again. It never
changes lease state, activity/execution timestamps, or the retention anchor.

#### `CJ2_RESERVE_REQUEST`

Creates a deterministic pending reservation for a subsequent robots, document,
redirect, or render-resource request. Its ordered fields are `run_id`, `job_id`,
`owner_id`, `lease_token`, `fence`, `request_ordinal`, `request_kind`,
`target_url_id`, `canonical_target_url`, `target_digest`,
`crawl_policy_sha256`, `policy_decision_sha256`, `group_id`, `rate_scope_id`,
`global_scope_id`, `group_scope_id`, `origin_scope_id`, then global, group, and
origin concurrency/interval pairs in the exact field-name order used by the
reservation identity in section 4. Reservation state, response snapshots, and
Redis timestamps are server fields. It validates the
current lease, exact run group map, monotonic scope transition, run/group
budgets, `reservation_creations_total < 100`, all active/pending/started indexes
and counters, and the one-active-reservation invariant. A first creation inserts
into `active` and `pending` and increments scope pending/active counters,
`pending_request_reservations`, `group_pending[group_id]`, and
`reservation_creations_total` exactly once, but does not consume a start. It never
prunes expiry state. A blocked result has only the narrowly permitted monotonic
tightening side effect in section 8.5.

New reservation creation additionally requires `last_stage_fence < fence` under
the exact current lease. `last_stage_fence=fence` is `INVALID_STATE` before
budget/capacity/rate blocked results or any scope tightening. An empty
`active_stage_commit_id` does not reopen admission after abort. Exact active
reservation replay still requires a consistent job/reservation pre-state; a
frozen job cannot validly own a pending or started reservation.

While `lease_delivery_started=0`, a replacement first intent is limited to
`robots` or `document` and must retain the same immutable job group, rate
lineage, group scope, and initial-origin consistency required of the claim-
embedded intent. Redirect and render-resource intents require an earlier request
start on the fence. Cancelling the claim's pending intent therefore cannot move
the open job to another group before its first start.

If a budget denial occurs after a start on this fence, the response has
`after_io=1`; the worker MUST use the matching
`run_budget_exhausted_after_io` or `group_budget_exhausted_after_io` retry
transition rather than wait for lease expiry. A concurrency-capacity or rate
block has `after_io=1` in the same circumstance. The worker may renew and poll
capacity, or wait until the returned rate deadline, only within its bounded
operation deadline; otherwise it uses `capacity_blocked_after_io` or
`rate_blocked_after_io`. At the 100-creation limit, a new intent returns
`RUN_RESERVATION_LIMIT_EXHAUSTED` without creating a reservation and with
`after_io=lease_delivery_started`. A pre-I/O lease then releases ready. An
after-I/O lease may finish/commit using already obtained data without another
request; if another request is required, it uses `CJ2_DEAD` with
`reservation_limit_exhausted`, never retry, because capacity cannot return.
Once no lease or reservation remains, `CJ2_FINALIZE_RUN` records the active run
as `budget_exhausted` with that same run terminal reason unless an earlier
finalization predicate wins.
Exact replay is `ALREADY_RESERVED`; any changed intent
under the same ID is `IMMUTABLE_MISMATCH`. Exact active-reservation replay is
checked before the creation limit and current budget/capacity/rate availability,
increments no counter, and creates no new permission; `CJ2_START_REQUEST` still
applies its current-interval and request-freeze gates.

#### `CJ2_START_REQUEST`

The first pending-to-started transition requires an active, unexpired
authorization, current lease, matching `pending` reservation, and
`last_stage_fence < fence`. A frozen current fence rejects a new start with
`INVALID_STATE` before any rate blocked result or mutation, including after
abort. Before mutation it revalidates all three scope records and requires Redis
time at or beyond the current group/origin rate deadlines.
If blocked, the reservation remains pending and the response's `after_io` bit
reflects `lease_delivery_started`; the worker renews and waits only within its
operation deadline. With `after_io=0` it may instead cancel the reservation and
release before I/O; with `after_io=1` it cancels the reservation and uses
`CJ2_RETRY` with `rate_blocked_after_io`. In one successful transaction it:

1. decrements `pending_request_reservations` and `group_pending[group_id]`, then
   increments `started_request_reservations` and
   `group_active_started[group_id]`;
2. increments job, run, and cumulative `group_started` request starts exactly
   once, retaining `lease_request_starts_baseline` unchanged;
3. increments `delivery_attempts` only if this is the first request start on the
   current fence, and marks `lease_delivery_started=1`;
4. changes the reservation to `started`, moving it from each scope's `pending`
   index to its `started` index without changing `active_count`;
5. decrements each rate scope's pending count and increments its started count;
6. advances group/origin `next_allowed_ms` to at least
   `now+effective_interval_ms`, updates `last_started_at_ms`, and never changes
   the global zero-interval deadline;
7. after prevalidating that the global evidence key is absent or an exact
   fixed-shape hash, creates its complete five-field record with one prebuilt
   `HSET` if this is the first V2 request start;
8. records job/run latest request-start fields and, for `document` or
   `redirect`, the job's document timestamp/fence, canonical target URL, and
   target identity;
9. stores the four exact post-start counter snapshots in the reservation for
   stable idempotent responses.

The first-request record contains exactly `protocol_version=2`, `run_id`,
`job_id`, `lease_fence`, and `started_at_ms`. It contains no URL or token. Only an
authenticated `STARTED` or `ALREADY_STARTED` response with `io_permission=1`,
consumed through the one-use lease-session permit below, can authorize DNS or
request I/O. The caller MUST perform no DNS lookup before that grant.

After common boot/marker and input validation, idempotency is checked against
the exact reservation/tombstone before current-lease, authorization, freeze, or
rate admission failure. An exact repeat of a recorded start returns
`ALREADY_STARTED` with that reservation's original start timestamp and four
post-start snapshots and changes no counter or deadline, even after later legal
job transitions. It MUST NOT reconstruct those snapshots from the current job
or compare them against a newer fence's baseline. `io_permission=1` is returned
only while the reservation is still `started`, is the job's exact active
reservation, and has the exact current unexpired lease, active unexpired run
authorization, and `last_stage_fence < fence`. Otherwise an exact historical
receipt returns `0` and is reconciliation only; a contradictory stored live
reservation/frozen-job pre-state fails ledger validation, not permission
issuance. The permission bit is current eligibility, not an immutable start
snapshot. No replay grants permission for a different intent. The third delivery
attempt may start, but no fourth delivery may be claimed.

The compatible client MUST keep one shared opaque I/O-permit state per
reservation identity in the exact run/job/owner/token/fence lease session.
Issuing/using a permit is atomic and one-use across value copies, separately
parsed replies, concurrent retry callers, and connection reconnects. A second
permitted reply MUST reuse the same state, never mint a new one. Retain that
state, including consumed or intentionally unused grants and their observed
outcomes, until the session is irrevocably closed; the run's ten-start limit
bounds these entries. Before FINISH or session freeze, any intentionally unused
grant MUST be irrevocably retired so a retained permit or delayed reply cannot
later start I/O. Used/retired state cannot return to unused. Network/HTTP
automatic retries or redirects MUST NOT
perform another request under an already used grant: every additional outbound
attempt requires its own admitted reservation and recorded start.

If the original START reply is lost and the existing local session proves that
I/O never began, the caller may reconcile the same reservation; an eligible
`io_permission=1` reply can supply its still-unused one permit. If that permit
was already issued or used, another reply cannot issue another permit.
`io_permission=0` never authorizes I/O, recreates a success event, or repairs
missing output evidence. A lost FINISH reply is reconciled by FINISH without
redoing I/O. If prior execution or the required local permit/event evidence is
uncertain or lost, the client MUST suppress further I/O and publication on that
fence rather than reconstruct permission from Redis counts or a fresh local
session. Existing permitted lease-ending paths apply only while ownership is
certain; uncertain ownership is left to expiry recovery. A process restart does
not resume an old owner session or reset one-use state to unused.

#### `CJ2_FINISH_REQUEST`

Requires the exact current lease and `started` reservation. It removes the
reservation from all active scopes, clears the job's active reservation, sets
the reservation to `finished` with `terminal_at_ms=now`, removes it from each `active` and `started`
index, decrements each scope's exact active/started counters plus
`started_request_reservations` and `group_active_started[group_id]`, and applies
its 24-hour tombstone TTL. It does not change the job baseline, refund
request-start counters, or shorten rate deadlines. `finished` records capacity
release, not network success; failed or unused recorded starts remain counted.
Tombstone idempotency is checked first, so exact replay is
`ALREADY_FINISHED` even after a later job transition.

Finish is allowed after run cancellation or authorization expiry so a current
worker can conservatively release its own capacity. A stale lease cannot finish
or release a live reservation; expiry recovery owns that operation.

#### `CJ2_CANCEL_RESERVATION`

Requires the exact current lease and `pending` reservation. It removes scope
membership from each `active` and `pending` index, decrements each scope's exact
active/pending counters, `pending_request_reservations`, and
`group_pending[group_id]`, clears the job's active reservation, and
writes a `cancelled` tombstone with `terminal_at_ms=now`. It consumes no request
start, changes no job baseline, and does not change the job state. It cannot
cancel a started reservation.
Exact tombstone replay
returns `RESERVATION_CANCELLED` before lease checks and never touches a newer
reservation. Like finish, cancellation of the current worker's own pending
reservation remains allowed after run cancellation or authorization expiry so
capacity is not intentionally stranded.

#### `CJ2_RELEASE_BEFORE_IO`

Its ordered request fields are `lease_identity`, then `transition_id`; its
transition reason is `none`. It requires a current leased job for which
`lease_delivery_started=0` in an active, unexpired run. It first
cancels any matching pending reservation, then moves `leased -> ready`, clears
the active lease fields defined in section 7.2 while retaining B/G and fence
history, removes the exact global active-lease member, and
records the idempotent transition. It consumes no
delivery attempt or request start. It is used for graceful shutdown or local
cancellation before any request starts; it is not a terminal job cancellation.
Transition idempotency is checked before lease state while it remains the job's
last effective transition, so an immediate exact replay returns
`RELEASED_READY`. After any newer claim/transition it returns `LEASE_LOST` and
can never release the newer fence; V2 does not retain an unbounded transition
history.

If run cancellation or authorization expiry wins first, the worker cancels its
pending reservation and uses `CJ2_CANCEL_JOB`; it never puts work back into a
cancelled run's ready index.

### 10.4 Job outcome transitions

`CJ2_DEAD` and `CJ2_COMPLETE_NO_OUTPUT` require an active run and unexpired
authorization. If cancellation/expiry won the race, they perform
`CJ2_CANCEL_JOB` semantics with the run's cancellation reason instead of writing
a dead/completed disposition. Reconciliation of an already terminal exact
transition remains idempotent. In every such race, the submitted transition ID
continues to identify the requested operation and its submitted reason, while
the stored job disposition/reason and reason counter use the authoritative run
cancellation reason. An exact replay therefore returns the same cancellation
result; the script never relabels the supplied transition identity as though the
caller had requested a different operation. The same rule applies to
`CJ2_RETRY` when its cancellation/expiry branch wins.

For every lease-ending operation in this subsection, an exact aborted-stage
terminal reservation is a permitted pre-state. The script charges its complete
prebuilt mutation `G` to that slot and removes the slot only after all covered
writes; any mismatched slot is corruption. With no such reservation it uses the
ordinary non-stage allocation inequality or the narrowly defined safety reserve.
Every branch removes the exact per-run and global active-lease memberships when
it clears the active lease fields defined in section 7.2. None clears the
retained baseline, cumulative starts, lease fence, or last-stage history; abort
and its following outcome do not reopen request admission on that fence.

#### `CJ2_RETRY`

Its ordered request fields are `lease_identity`, `reason`, then `transition_id`.
It requires the current lease, no active reservation, at least one request start
on the current fence, an empty `active_stage_commit_id`, no owned stage slot,
and either no slot for this job/fence or its exact aborted-stage terminal
reservation, plus one closed
retryable reason valid for the observed typed outcome. A worker with a stage
must first call `CJ2_ABORT_STAGE`; expired-lease recovery may instead abandon it
as specified below. If the run is cancelled or authorization has expired, retry
performs `CJ2_CANCEL_JOB` semantics instead. Reaching a request budget does not
block this lease-ending transition.

For `downstream_backpressure`, the script requires either
`last_stage_fence < fence` because begin was blocked before creating any stage,
or matching commit-backpressure fields retained after abort with
`now >= commit_backpressure_deadline_ms`. It rejects an early abandonment of a
blocked commit and any unrelated previously begun stage under that reason.

- `delivery_attempts=1`: set `not_before_ms=now+30000`.
- `delivery_attempts=2`: set `not_before_ms=now+120000`.
- `delivery_attempts=3`: move to dead with `last_reason=retry_exhausted` and the
  supplied reason in `last_failure_reason`.

Delayed transitions increment `retry_count`, run `retries_total`, and exactly
one closed field in `retry_reason_counts`; their sum MUST remain equal. They
clear active lease and commit-backpressure fields, remove matching
commit-backpressure membership and any exact aborted-stage terminal reservation,
and set both `last_reason` and
`last_failure_reason` to the supplied retry reason. The third-delivery dead
branch instead sets `last_reason=retry_exhausted` while retaining the supplied
reason as `last_failure_reason`. The delay is computed by Lua; callers cannot
supply or jitter it. Exact `transition_id` replay returns the
original result without adding membership or counters while it is still the
job's last transition. A replay after a newer effective transition returns
`LEASE_LOST` without mutation.

#### `CJ2_DEAD`

Its ordered request fields are `lease_identity`, `reason`, then `transition_id`.
It requires the current lease, no active reservation or active stage, no slot
except an exact aborted-stage terminal reservation, and a closed terminal reason
legal for `CJ2_DEAD`. It atomically removes leased/open
membership, writes one dead membership with both `last_reason` and
`last_failure_reason` equal to the submitted reason, clears active lease and
commit-backpressure fields, and
removes matching commit-backpressure membership and that terminal reservation,
then increments terminal and
exact disposition-reason counters once. Dead entries
are not trimmed. Exact replay returns `DEAD` without mutation.

#### `CJ2_CANCEL_JOB`

Its worker form has ordered request fields `lease_identity`, `reason`, then
`transition_id`. It requires the current lease with no active reservation or
active stage and no slot except an exact aborted-stage terminal reservation;
ready/delayed selection belongs only to `CJ2_CANCEL_BATCH`, while expired-lease
selection belongs only to `CJ2_RECOVER_EXPIRED`.
It atomically removes leased/open membership, writes one cancelled membership
and reason, clears active lease and commit-backpressure fields, decrements
run/group open counts, removes matching commit-backpressure membership and that
terminal reservation, and increments one
disposition-reason counter. A current worker aborts its
active stage first; expired recovery may abandon it. Completed and dead jobs
remain unchanged. A currently started reservation must finish or expire before
cancellation can terminalize the job.

#### `CJ2_COMPLETE_NO_OUTPUT`

Its ordered request fields are `lease_identity`, `reason`, then `transition_id`.
It requires the current lease, no active reservation, an empty
`active_stage_commit_id`, no slot except an exact aborted-stage terminal
reservation, and the reason `already_visited`. It moves the job to
completed without page publication, clears active lease and commit-backpressure
fields and their secondary membership, removes that terminal reservation, and is
idempotent by transition ID.

### 10.5 Stage and commit transitions

Ordinary stage writes are forbidden. Every worker stage operation in this
subsection requires approved boot and exact active markers. Except for the
explicit completed-COMMIT reconciliation below, each requires the exact current
unexpired owner/token/fence, no active request reservation, and deterministic
commit/publication/output/token identities. First BEGIN requires no active
stage. Data writes, including their replays, require an unsealed/unabandoned
stage; first seal requires an unsealed/unabandoned stage, identical seal replay
accepts its sealed post-state, abort accepts either seal state, and first commit
requires a sealed/unabandoned stage. BEGIN, data writes, seal, and first commit
also require active unexpired authorization. The explicit post-abort replay
validates its retained terminal slot instead of requiring deleted stage keys.
Each operation validates all affected key types and its complete section 2
allocation inequality before mutation. A stage uses one absolute
`expires_at_ms=begin_redis_time+900000`; every newly created stage key receives
that exact `PEXPIREAT` in its creation script. No replay or renewal extends it.

For an owned stage, every data write, seal, first commit, and active-stage replay
MUST establish the exact metadata/slot/job owner tuple and validate:

```text
job.last_stage_fence = job.lease_fence = request.fence = stage.lease_fence
job.active_stage_commit_id = job.last_stage_commit_id = stage.commit_id = request.commit_id
job.active_reservation_id = ""
job.lease_delivery_started = 1
stage.request_starts_baseline = job.lease_request_starts_baseline
stage.request_starts_generation = job.request_starts
0 <= stage.request_starts_baseline < stage.request_starts_generation <= 10
```

Stage run/job/owner and token digest must match the exact lease, and the section
4 publication and commit IDs are recomputed using the metadata's output digest
and B/G, not caller-selected replacement snapshots. First abort and renewal of
an owned stage also validate this frozen binding. Established current-stage
baseline/generation drift is `COUNTER_CORRUPT`, including before a replay or
blocked-commit bookkeeping write; it cannot be repaired by re-reading newer
counts into the stage. Completed-commit and post-abort receipts use their specified retained
identities instead. Historical recovered residue is subject to section 10.6's
cleanup rules, not current-fence publication predicates.

Each stage-data request includes, in order, `run_id`, `job_id`, `owner_id`,
`lease_token`, `fence`, `commit_id`, its operation-specific chunk kind and
ordinal, `chunk_digest`, record count, and exact records. A first chunk write
stores its digest and bytes. An exact replay compares every byte and returns
`EXISTS_IDENTICAL`; a changed digest or byte is `IMMUTABLE_MISMATCH`. All
record/count/byte/key/memory limits are checked before the whole chunk is
accepted. No operation partially accepts or truncates a chunk.

The closed `chunk_kind` values are `page_fields`, `html`, `original_html`,
`outlinks`, `discoveries`, `aliases`, `images`, and `image_manifest`. Ordinal is
`0` except for the contiguous outlink/discovery chunks. Operation name and chunk
kind must match; a generic caller-selected kind is rejected.

#### `CJ2_BEGIN_STAGE`

Its ordered request fields are `lease_identity`, `commit_id`, `publication_id`,
`output_digest`, `request_starts_baseline`, `request_starts_generation`,
`expected_page_fields`, `expected_outlinks`, `expected_discoveries`,
`expected_aliases`, and `expected_images`. The client MUST derive B/G from its
authenticated section 8.3 `OutputContext` and derive/verify the output digest,
publication ID, commit ID, and expected counts from that same context and
validated semantic output. A wire constructor accepting only caller-selected
counts/digests without this binding is insufficient. The reply shape is
unchanged; the returned commit ID binds the interval.

After gate, input-shape/lexical, scalar-pair, current-lease, and record validation,
identical-active replay is checked before the one-stage-per-fence, slot-capacity,
and new-admission checks. For an existing active stage ID, compare every
immutable BEGIN input, including B/G, against metadata before recomputing an
identity from changed caller fields: changed input under that ID is
`IMMUTABLE_MISMATCH`, not a new first-BEGIN request. An exact active replay also
requires the frozen job/stage tuple and original lifetime to
remain valid, even if the stage has since been sealed. It returns
`EXISTS_IDENTICAL` with the original expiry and current remaining reservation,
never extending expiry or replenishing memory.

First BEGIN requires an empty job `active_stage_commit_id`,
`last_stage_fence < fence`, no stage for this commit, no active request
reservation, `lease_delivery_started=1`,
`last_document_request_fence=fence`, and empty stage destinations. Before slot
or memory admission and before any mutation it MUST require:

```text
request.request_starts_baseline = job.lease_request_starts_baseline
request.request_starts_generation = job.request_starts
0 <= request.request_starts_baseline < request.request_starts_generation <= 10
```

A valid but stale pair is `STAGE_INVALID`, not a freeze or a counter repair.
If another start finished after the witness read, its increment of G invalidates
that BEGIN even when its timestamp/target is unchanged. A pending or started
reservation makes first BEGIN `INVALID_STATE`; a cancelled unstarted reservation
contributes no start and does not itself invalidate an otherwise matching
interval. The client may replace a provisional context only through complete authenticated
revalidation, never by patching B/G alone. Admission then requires fewer than
four entries in `stage_slots` and the full 48 MiB reservation.

Successful BEGIN is the atomic compare-and-freeze linearization point. It
creates the fixed-shape `T:meta` with the exact compared B/G,
creates `T:keys` containing exactly `T:meta` and `T:keys`, applies their common
absolute expiry, inserts `commit_id` into `stage_expiry`, and inserts
`commit_id => <50331648-G_begin>:<run_id>:<job_id>:<fence>:0` into `stage_slots`,
where all fields are canonical and `G_begin` includes all new metadata
(including both interval fields), inventory, and job-field allocation and leaves
at least the 64 KiB control portion. Colons are unambiguous because each
component's grammar excludes them. It sets the job's active/last stage commit ID
to `commit_id` and `last_stage_fence=fence` in that same mutation, without
changing job B/G. From then on reserve/start MUST reject new request admission
on that fence, even if abort, recovery, commit, or cleanup removes stage keys or
the active-stage field. The freeze cannot be undone or replaced by a second
BEGIN. A different BEGIN, or BEGIN after abort, on the still-current frozen
lease is `INVALID_STATE`; after ownership ends it is `LEASE_LOST`. Only the
existing lease-ending/recovery path followed by a new claim may resume work,
capturing a new baseline and deriving a new fence/commit identity.

A lost BEGIN response is reconciled with the same identity while the stage is
active; the client MUST NOT infer that requests remain admissible from the lost
reply. Once abort, recovery, or first commit clears the active field, that commit
ID cannot begin again. Completed publication is reconciled through COMMIT, not
by recreating its stage.

If a race fills the fourth slot, or the admission inequality fails after network
I/O, begin returns `STAGE_CAPACITY_BLOCKED` with `stage_slots_full` or
`memory_headroom_low` and changes nothing, including B/G and `last_stage_fence`.
Because begin already requires no active reservation, the worker immediately
uses `CJ2_RETRY` with `downstream_backpressure`; it does not renew indefinitely
while holding fetched output in process memory.

#### `CJ2_STAGE_PAGE_FIELDS` and `CJ2_STAGE_PAGE_BLOB`

`CJ2_STAGE_PAGE_FIELDS` accepts one record containing the eight non-blob final
page fields: `normalized_url`, `content_type`, `status_code`, `last_crawled`,
`rendered`, `render_policy_rule`, `render_policy_sha256`, and `publication_id`.
It validates every section 6.4 relation, including the job's recorded final
document target/start time. `CJ2_STAGE_PAGE_BLOB` accepts exactly one field,
either `html` or `original_html`, plus its exact bytes. Each blob is at most
5 MiB, the combined values are at most 10 MiB, and the serialized command is at
most 5,373,952 bytes. The two operations incrementally create the same
`T:page` hash; each field may be written once or identically replayed.

#### Bounded non-blob stage batches

The operations are `CJ2_STAGE_OUTLINKS_BATCH`,
`CJ2_STAGE_DISCOVERIES_BATCH`, `CJ2_STAGE_ALIASES_BATCH`, and
`CJ2_STAGE_IMAGES_BATCH`. Each accepts at most 64 records and a serialized
request of at most 512 KiB. Chunks and records must be in the exact canonical
order from section 4, with contiguous zero-based chunk ordinals. Outlinks use
at most four chunks, discoveries at most two, aliases exactly one, and images
zero or one. The exact ordered record fields are: outlinks—`target_url`;
discoveries—`job_id`, `canonical_url`, `depth`, `score_text`, `group_id`,
`rate_scope_id`, `group_scope_id`, `initial_origin_scope_id`, and
`policy_decision_sha256`; aliases—`url_id`, `canonical_url`, and `depth`;
images—`normalized_source_url` and `alt`. Discovery records include their
complete policy binding and are checked against the run group map.
Every outlink, discovery job ID, alias URL ID/URL, and image source is unique
within its complete logical collection; duplicates reject the chunk before any
write. `T:discovery_records` stores exactly the seven fields
`<job_id>:canonical_url`, `:score_text`, `:group_id`, `:rate_scope_id`,
`:group_scope_id`, `:initial_origin_scope_id`, and
`:policy_decision_sha256` for each record; suffix parsing is unambiguous because
the job ID is fixed at 64 ASCII hex bytes. Alias staging similarly stores exact
`<url_id>:canonical_url` and `<url_id>:depth` fields and rejects duplicate IDs
or URLs with changed values. Image records create `T:image:N` hashes with
contiguous `N=0..count-1`; duplicate source URLs or final payload keys reject
the whole batch. These operations create/update only the bounded stage keys
listed in section 6.3.

#### `CJ2_STAGE_IMAGE_MANIFEST`

Accepts one record ordered as `contract_version`, `publication_id`,
`normalized_url`, `image_count`, and `image_keys` after all image payload
chunks. It
requires `image_count=expected_images`, exact compact JSON equal to the ordered
final payload-key list, a maximum of 393,216 bytes, and exact publication/page
identity. It creates `T:image_manifest` even when image count is zero.

#### `CJ2_ABORT_STAGE`

Requires the current unexpired lease and an exact unsealed/sealed stage before
its common absolute expiry; stale or expired ownership disposition belongs only
to recovery, and cleanup may delete residue only after recovery removes the
slot. It prevalidates
the ordered `lease_identity`, `commit_id`, and `transition_id` using transition
reason `none`, then the entire
available at-most-73-key inventory, and unlinks exactly those stage keys,
removes the `stage_expiry` entry, clears only the matching
`active_stage_commit_id` from the job, and stores the abort transition. It MUST
retain `last_stage_commit_id`, `last_stage_fence`, the job baseline, and cumulative
request starts: abort never permits another request or stage on that fence.
It deducts its proved `G` but retains the exact
`stage_slots` owner field with the remaining terminal reservation and changes
its final component from `0` to the prevalidated positive `key_count`; it does
not release that memory to a racing allocator. After common gates, input, and
current unexpired lease validation, exact post-abort replay is checked before
requiring materialized stage keys. It requires that same terminal reservation
and returns `EXISTS_IDENTICAL` with the stored count using the job's last
transition identity while no newer transition exists; otherwise it is
`LEASE_LOST`. It never touches a final output key.
It does not clear commit-backpressure fields or membership. The immediately
following lease-ending transition validates and clears that evidence and
removes the retained slot after all covered writes; if the worker dies first,
expired-lease recovery does so.

#### `CJ2_SEAL_STAGE`

Before calling seal, the compatible client MUST re-read every stage key in
bounded pages, recompute the output digest and compact manifest independently of
its write buffers, and discard those buffers. It retains only the context and
identity evidence needed to verify the read: metadata B/G, full lease, commit,
publication, and output identities MUST match the same authenticated
`OutputContext` used for BEGIN. Verification MUST read and compare the actual
staged aliases and all other semantic sections; substituting context-derived
aliases while ignoring the stored alias data is forbidden. A fresh section 8.3
job witness must still match that context; Lua independently enforces the frozen
tuple at seal, so a race with recovery cannot make a read confer ownership.
The ordered seal request contains the lease identity, `commit_id`,
`verified_output_digest`, and `verified_manifest_chunk_digest`. The script
requires the current lease, active unexpired run authorization, no active
reservation, and matching unabandoned `T:meta`. It validates:

- `commit_id`, publication ID, output digest, token digest, run, job, and fence;
- the exact frozen job/stage baseline/generation and last/active-stage relations
  above, including on identical seal replay;
- page, outlink, discovery, alias, manifest, payload, and key-list types;
- exact completed chunk set, counts, canonical orders, image keys, field shapes,
  publication IDs, final document timestamp/fence/target witness, and policy
  bindings;
- all individual and aggregate byte limits;
- every stage key's exact positive absolute expiry and membership in `T:keys`;
- at most 73 stage keys, at most 14 MiB logical data, an exact stage-slot
  reservation, and internally consistent section 2 stage accounting;
- both submitted verification digests equal the immutable stage metadata.

This detects write/re-read disagreement in the pinned client but is not a
hostile-client attestation; section 10.1's immutable-image trust boundary still
applies.

After those checks, repeating an identical seal against its sealed post-state
returns `EXISTS_IDENTICAL` before the first-seal unsealed-state requirement;
changed verification data is `STAGE_INVALID`, and stored frozen-tuple drift is
`COUNTER_CORRUPT`. First seal requires `sealed=0` and changes only `sealed=1`,
`sealed_at_ms`, and the slot's remaining bytes by the exact seal `G`; the stage
was already inventoried at begin. Neither first seal nor replay changes B/G or
expiry. No stage-data operation accepts a sealed stage.

#### `CJ2_COMMIT`

Its ordered request fields are `lease_identity` followed by `commit_id`; all
bulk output is read from the sealed stage, keeping the serialized request within
64 KiB. There are no separate baseline/generation arguments: the commit ID binds
them, and first commit reads them from metadata. This is the only operation that
may expose crawl output.

After approved boot, exact active markers/commit guard, input validation, and
retained-job validation, completed-commit idempotency is checked before live
lease, active-run, authorization, stage, destination, or queue admission checks.
The existing run purge-in-progress rejection remains in force.
For a job already completed by publication, recompute the section 4 commit ID
using the request's run/job/fence/token and the retained job's `publication_id`,
`lease_request_starts_baseline`, and `request_starts`. Require the requested
fence to equal the retained job/last-stage fence and the recomputed ID to equal
both the requested `commit_id` and stored job `commit_id=last_stage_commit_id`.
An exact match returns `ALREADY_COMMITTED` with the original publication ID,
commit ID, and completion time and changes nothing. It does not compare against
cleared active lease owner/token/expiry fields, require the old transcript or
stage/output keys, or recreate a stage, slot, output, or notification. The
request token participates in identity recomputation, not renewal of authority.
A different valid requested identity, or a completed job without publication,
returns `LEASE_LOST`. Missing/purged job state cannot establish this receipt and
MUST NOT be reported as `ALREADY_COMMITTED`.

For a first commit, the prevalidation phase MUST validate:

1. current, unexpired owner/token/fence;
2. active run and unexpired authorization;
3. no active request reservation;
4. exact sealed stage identity, frozen job/stage baseline/generation and
   last/active-stage equality, and every stage bound;
5. exact approved commit guard and the section 2 growth/headroom inequalities;
6. `pages_queue` type and length below 5,000;
7. final page, outlink, manifest, and every image destination are absent;
8. every backlink destination is absent or a set;
9. every discovery's URL identity and policy binding/group tuple, plus each
   existing job's internally consistent immutable values, state-index
   membership, and same canonical URL;
10. new discovered jobs keep run `job_count <= 10000`;
11. every run/group open count, terminal/reason count, state index, rate index,
    exact global active-lease member, stage slot, job
    `active_stage_commit_id`, and counter needed by mutation;
12. exact final page/image field forms, outlink/backlink members, alias effects,
    destination names, all mutation argument arrays, and the complete response.

No mutation may occur until all output, discovery, state, destination, ACL,
memory, command-bound, and response construction completes. The script performs
no bulk SHA-256, policy parsing, URL resolution, sorting, JSON parsing, or
allocation-prone construction after this point. Maximum-shape static review and
the commit guard MUST prove that every remaining call is fixed, bounded, and
non-failing on the exact Redis version/configuration. No `allow-oom` flag is
permitted.

The mutation phase MUST atomically perform all of the following:

1. `RENAME` the staged page to its final `page_data` key and `PERSIST` it.
2. For nonempty outlinks, rename and persist the exact final `outlinks` set;
   absence is the canonical empty outlink representation.
3. Rename and persist every image payload.
4. Rename and persist the image manifest, including an explicit zero-image
   manifest when applicable.
5. For every outlink target, `SADD` the page's canonical effective URL to the
   corresponding `backlinks:<target>` set.
6. Add or reconcile every discovered job. New jobs receive job hashes and ready
   indexes, exact immutable policy binding, `lease_request_starts_baseline=0`,
   `request_starts=0`, and run/group open counters.
   For an existing job with the same canonical URL, first admission wins:
   score, depth, policy binding, state, and indexes are unchanged regardless of
   the later valid discovery record. A same-ID/different-URL collision rejects
   the commit. Existing jobs are never reopened or moved between groups.
7. Validate/store the canonical URL collision witness and update
   `visited_depth` for every staged alias (including the original and final
   effective URLs), retaining the shallowest depth.
8. `LPUSH` exactly one final page key to `pages_queue`.
9. Move the crawl job from leased to completed with `last_reason=published`.
10. Store output, publication, and commit digests plus the exact final page key;
    decrement run/group open counts; clear active lease and backpressure fields;
    retain job B/G, lease fence, and last-stage identity for completed replay;
    remove the exact global active-lease member;
    remove matching commit-backpressure membership; increment completion,
    disposition-reason, and first-commit counters once.
11. Clear the job's matching active-stage field, remove the global stage slot,
    calculate `residual_expiry_ms=min(original_expires_at_ms,
    redis_now_ms+60000)`, set the stage-expiry score to that value, and set every
    unrenamed residual stage key's absolute expiry to that same value. This
    applies to metadata, inventory, discovery, and alias staging keys left for
    cleanup; final output keys MUST have no stage TTL.

The script MUST NOT publish a page marker, publish output in a second command,
or ACK the job separately. A lost response is retried with the same commit ID.
There can be one completed job, one page notification, and one admitted set of
discovered jobs.

If queue or memory admission blocks commit, no consumer-visible mutation occurs.
On the first such result for a fence, `CJ2_COMMIT` may atomically set only the
job's `commit_backpressure_fence`, Redis-time start, exact
`min(start+120000, stage_expires_at_ms-10000)` deadline, the first blocked reason, and
matching job/start-time membership in the run
`commit_backpressure` index, then return `DOWNSTREAM_BACKPRESSURE` with blocked
reason `pages_queue_full` or `memory_headroom_low`. Replays return the same
durable first reason/start/deadline while any admission condition remains
blocked. The first record deducts its proved `G` from the stage's
reserved control portion and must leave at least the 32 KiB terminal floor; a
replay allocates nothing. The worker may renew and
retry commit only while `now < deadline`. At or after it, while the lease remains
valid, the worker aborts the stage and calls `CJ2_RETRY` with
`downstream_backpressure`; if lease expiry wins that race, ordinary recovery
owns the disposition. A process restart
cannot reset the deadline. This internal timestamp mutation is not a first
commit and exposes no output.

The script validates the frozen tuple before any backpressure result or record,
then checks an existing matching backpressure deadline before current
queue/memory admission. At or after that deadline it returns the same durable
`DOWNSTREAM_BACKPRESSURE` record and MUST NOT publish even if capacity has since
recovered; only abort plus the next-fence retry, or ordinary expired-lease
recovery, may continue the job.

The required evidence uses real Redis 7 with the exact ACL, standalone/AOF/
noeviction configuration and maximum stage. It injects client loss and Redis
`SIGKILL` before the script, during every prevalidation class, after command
submission, and after acknowledgment, then restores the same AOF volume. It
must prove one of three safe outcomes: startup remains blocked on detected AOF
damage; no first commit exists; or one complete page/outlink/image/backlink/
discovery/notification/completion transaction exists. A partial accepted
transaction is forbidden. If static proof, memory growth, Lua p99, AOF replay,
or process-kill evidence fails, candidate promotion is forbidden; this protocol
does not claim Redis rollback and MUST remain unapproved.

For every injection after the client received acknowledgment, only a successful
restart with the complete transition satisfies the zero-loss claim; a blocked
restart is safe but does not pass the release's acknowledged-write evidence.

### 10.6 Maintenance, cancellation, and retention transitions

#### `CJ2_PROMOTE_DUE`

Its semantic request contains only `run_id`. It moves at most 100 `delayed` jobs
with score `<= Redis TIME` to ready/ready-at, selected by ascending deadline and
then job-ID bytes.
It restores each job's immutable scheduling score, sets `ready_at` to this
operation's Redis time, and clears `not_before_ms`. It validates every selected
job and its immutable group/open contribution before
mutation. It performs no promotion after the run is budget-exhausted or
cancelled. It returns `BATCH_MORE` when another due item exists, otherwise
`BATCH_DONE`.

#### `CJ2_RECOVER_EXPIRED`

Its semantic request contains only `run_id`. It processes at most 100 leased
jobs whose lease is due and whose active
reservation is absent or also due, selected by ascending lease expiry and then
job-ID bytes. It prevalidates all selected jobs before any write. For each exact
job with a matching active reservation, it expires that
reservation from all three `active` and pending/started indexes, reconciles every
exact counter, and writes an `expired` tombstone with `terminal_at_ms=now`. A
pending reservation refunds pending request-start budget but not its cumulative
reservation creation; a started
reservation never refunds starts or shortens rate deadlines. It then applies:

- no request start on the fence: increment `pre_io_recoveries`; the first two
  recoveries return the job to ready, while the third moves it to dead with
  `pre_io_recovery_exhausted`;
- request started and fewer than three delivery attempts: use the exact 30- or
  120-second retry transition with `lease_expired_after_io`;
- third delivery attempt: dead with `last_reason=retry_exhausted` and
  `last_failure_reason=lease_expired_after_io`;
- cancelled/expired run: cancelled with the run's cancellation reason.

Every branch clears active lease and commit-backpressure fields and removes
matching commit-backpressure membership plus the exact global active-lease member
as part of the same transition. Every branch retains the job's baseline,
cumulative request starts, lease fence, and last-stage history. No recovery or later cleanup
resets these values or permits more requests on the recovered fence.

If the expired fence owns an active stage slot, recovery validates live metadata
including its frozen B/G against that still-owning job/fence before clearing
ownership, and marks it `abandoned=1` without changing either snapshot; if the
common stage expiry has passed and metadata is already absent, the exact slot
owner tuple is sufficient. It leaves the
`stage_expiry` score at the unchanged common absolute expiry, removes its
unmaterialized stage-slot reservation, and leaves physical keys to bounded
cleanup at that deadline. If instead the job has the exact aborted-stage
terminal reservation, recovery validates the abort transition and removes that
slot; its stage keys and expiry member are already absent. It never renames a
stage key. It clears only the matching job active-stage field. A later claim
captures the retained cumulative starts as its new baseline; any recovered
stage then describes an older fence and cannot be reclassified as current output
authority by comparing or replacing its snapshots with the new job interval.
Recovery MUST NOT release a nonexpired or differently owned reservation or stage.
Recovery increments `recovered_leases_total` and exactly one
`recovery_outcome_counts` field once per recovered job; the closed fields
are `ready`, `delayed`, `dead`, and `cancelled`, and their sum equals the total.
Its delayed branch also increments `retries_total` and the
job's `retry_count` plus the `lease_expired_after_io` retry-reason field exactly
once, preserving the common retry-reason sum invariant.

#### `CJ2_CANCEL_RUN`

Its ordered semantic request fields are `run_id` and `reason`. It changes
loading, auditing, sealed, or active runs to `cancelled`, records one
closed run reason, and prevents claim/start/retry/commit immediately. It does
not iterate all jobs. Current workers may finish or recover reservations and
abort stages but cannot publish. Repeating the same cancellation is idempotent;
changing the reason is an immutable mismatch.

#### `CJ2_CANCEL_BATCH`

Its semantic request contains only `run_id`. It moves at most 100 jobs total in
a cancelled run to job `cancelled`, selecting ready jobs first
by score/job ID and then delayed jobs by deadline/job ID. It applies exact
group/open/disposition counters after prevalidating the entire selected batch,
and never touches leased jobs, reservations,
stages, or completed/dead jobs. `CJ2_RECOVER_EXPIRED`, which runs first in the
maintenance order, is the sole operation that expires a leased job or
reservation and uses its cancelled-run branch when applicable.

#### `CJ2_CLEAN_STAGE`

Its ordered semantic request fields are `expected_commit_id` and
`expected_cleanup_due_at_ms`, obtained from a one-member read of `stage_expiry`.
Earliest means ascending score and then commit-ID bytes. It acts only when that
exact pair is still the earliest member and
`redis_now_ms >= expected_cleanup_due_at_ms`. A concurrently removed/changed or
not-yet-due pair returns a zero-processed `BATCH_MORE` or `BATCH_DONE`, according
to whether another due entry remains, rather than selecting a different bundle.
If a matching stage slot remains, the script validates
`abort_unlinked_keys=0`, the exact leased job/fence/active-stage ownership, and a
matching per-run/global lease deadline no later than this cleanup deadline. A
later or inconsistent lease is corruption. Any still-present metadata must
match that owning job's frozen B/G; metadata absence at the common expiry is
handled by recovery's existing slot-owner rule, not by synthesizing snapshots.
It then returns zero-processed `BATCH_MORE` without unlinking or releasing
anything; `CJ2_RECOVER_EXPIRED` must perform the lease disposition and remove
the reserved slot first. A positive abort count with an expiry member is also
corruption. This ordering prevents another allocator from consuming the
terminal floor needed by recovery.

After the slot is absent, the bundle is commit or recovery residue and cleanup
is deletion-only. Cleanup MUST NOT mutate any job baseline, cumulative start
counter, lease fence, or last-stage history. A recovered stage's B/G remain
historical: a newer job fence or new baseline is not a mismatch requiring repair,
nor grounds to apply current-fence publication predicates to that residue. The
exact commit-ID prefix, inventory, expiry, and residue-ownership rules still
apply; no historical stage can authorize new requests or publication. Cleanup
unlinks at most its 73 validated stage keys per invocation
and never follows a key outside the exact
`mifolyo:crawl:v2:stage:<commit_id>:` prefix. It removes the `stage_expiry`
member only after the bundle is logically empty. If `T:keys` exists, every
listed key must have the exact prefix and is unlinked. If metadata or `T:keys`
is absent at or after the recorded cleanup deadline, the script may remove the
inventory member: every unrenamed stage key has an absolute `PEXPIREAT` no later
than that score, so Redis treats it as expired even if physical lazy deletion is
pending. Missing metadata or inventory before the cleanup deadline is
corruption. Current `used_memory` still includes any not-yet-freed allocation,
so later admission cannot ignore it.

#### `CJ2_MAINTAIN_RATE_SCOPES`

Its semantic request is the canonical process-local nonnegative `rank_offset`.
It reads at most 100 IDs from
`rate_scopes`. For each, it validates the scope hash and exact active/pending/
started cardinalities. It is a read-only integrity pass: Redis automatically
removes a ZSET key when its last member is removed, so there is no empty index
key to clean. It never changes inventory scores, a scope record, an effective
limit/deadline, or a reservation; `CJ2_RECOVER_EXPIRED` is the sole reservation-
expiry owner. The caller advances by `processed` while `BATCH_MORE` and resets
its local offset after `BATCH_DONE`. Inventory growth or concurrent
`updated_at_ms` score changes can make a diagnostic pass skip or repeat a scope,
but cannot affect safety because every mutating
reservation operation performs exact scope validation. An idle pass performs no
write.

#### `CJ2_FINALIZE_RUN`

Its semantic request contains only `run_id`.

- Changes active to `completed` only when `open_job_count=0`, terminal counters
  sum to `job_count`, primary index cardinalities agree, and there are no leases,
  delays, commit-backpressure members, pending/started reservations, or stages
  owned by the run. It sets `completed_at_ms=now` and
  `terminal_reason=all_jobs_terminal`.
- Changes active to `budget_exhausted` when no reservation is pending/started,
  no job is leased, no stage is owned by the run, and at least one of these is
  true: `request_starts >= max_request_starts`;
  `reservation_creations_total=100`; or every one of at most 64 groups with
  `group_open_jobs>0` has `group_started >= group_limits`. Ready and delayed
  jobs remain indexed as evidence. The status/reason is respectively
   `RUN_BUDGET_EXHAUSTED`/`request_budget_exhausted`,
   `RUN_RESERVATION_LIMIT_EXHAUSTED`/`reservation_limit_exhausted`, or
   `GROUP_BUDGET_EXHAUSTED`/`group_budgets_exhausted`. If multiple predicates
   are true, that listed order wins deterministically. It sets
   `budget_exhausted_at_ms=now`.
- Finalizes a cancelled run only when `open_job_count=0`, terminal counters sum
  to `job_count`, primary indexes agree, and no lease, delay, reservation, or
  owned stage remains.

Run state selects the cancelled branch. For an active run, all-jobs-terminal
completion is evaluated before budget exhaustion; among budget predicates, the
run request-start limit is evaluated before the reservation-creation limit,
which is evaluated before the all-open-groups predicate.

Every branch first validates all primary-index cardinalities, run/group
counters, `claims_total <= reservation_creations_total <= 100`, and closed
reason sums. All checks use bounded counters, at most 64
group fields, the at-most-four stage slots, the at-most-64 global lease members
with no composite for this run, and constant-many index
cardinalities; finalization never scans jobs. It records
`finalized_at_ms=now`,
`retention_anchor_ms=max(last_request_started_at_ms,
last_terminal_transition_at_ms, last_activity_at_ms, now)`, removes completed,
budget-exhausted, and fully cancelled runs from `active_runs`, and retains all
evidence.

#### `CJ2_ARCHIVE_RUN` and `CJ2_PURGE_RUN_BATCH`

A run MUST remain online until Redis time is at least 30 days beyond its frozen
`retention_anchor_ms`. Archive additionally requires:

- no per-run or global lease member, pending/started reservation, or stage owned
  by the run;
- global `stage_slots` and `stage_expiry` are empty after the stopped maintenance
  drain, so no active, aborted, committed, or recovered stage residue remains;
- downstream page/image source and processing queues drained while every
  crawl/output producer is stopped, and a complete bounded backlink scan cycle
  under that same freeze proves zero pending members;
- every published job's retained final-page-key evidence is absent from source,
  processing, and dead-letter lists, and bounded reconciliation proves its exact
  page/outlink/manifest keys and image-payload key prefix absent after compatible
  consumer ACKs;
- a complete exported run report containing all jobs, counters, reasons,
  policy/authorization digests, `reservation_creations_total`, the fixed limit
  `100`, and an archive SHA-256; its bounded job scan proves that the sum of
  `(next_request_ordinal-1)` over all jobs equals the run counter and is at most
  100;
- operator confirmation bound to run ID and archive digest.

`CJ2_ARCHIVE_RUN` has ordered semantic request fields `run_id`,
`archive_sha256`, and `confirmation_text`, where confirmation is exactly
`<run_id>:<archive_sha256>`. The stopped archiver establishes the bounded
downstream/reconciliation facts above and includes their evidence digests in the
export whose digest is submitted; Lua independently validates the bounded run
counters/state but does not scan all jobs or output keys. The operation writes
the immutable archive digest/state and removes exactly that run from
`unarchived_runs`; all purge fields must still have their initial values. Purge
may begin seven days later.

`CJ2_PURGE_RUN_BATCH` has ordered semantic request fields `run_id`,
`evidence_sha256`, and `expected_first_job_id_or_empty`; the evidence is the
stored archive digest, or the candidate-abort report digest for the sole
exception below. The expected ID comes from a one-member lexicographic
`job_order` read and fences each batch against an ambiguous prior response. It
removes at most 100 known job hashes and members from the run's own
`job_order`/`jobs` inventory and their primary/age/backpressure index membership.
On the first effective batch it atomically sets `purge_state=in_progress`, the
exact evidence digest, Redis start time, and `purged_job_count` equal to that
batch's prevalidated removal count. Every later batch requires the same evidence and validates
`SCARD(jobs)=ZCARD(job_order)=job_count-purged_job_count` and that the remaining
primary indexes have that same total. Original run/group/reason/audit counters
remain frozen archive/candidate-abort evidence while the explicit purge marker makes the reduced
live inventories non-corrupt. Every mutating operation other than exact purge
continuation rejects a run with `purge_state=in_progress`.

It never uses `SCAN`, touches
another run, removes unexpired rate state, or removes
`first_request_start`. After the job inventories and primary indexes become
empty, the final batch unlinks the fixed run-level record, group/counter maps,
visited maps, and now-empty indexes and removes its `runs` member. If that final
response is lost, a retry may return `PURGED` only
when the caller supplies the same applicable archive/abort-report digest and
every exact run key/index membership is absent; this is external-evidence reconciliation, not a
claim that Redis retained a purge tombstone. This absent-state final replay is
exactly `[PURGED, now_ms, 0, 0]`. After a lost nonfinal response, the
caller re-reads the immutable purge fields and current first job; the advanced
`purged_job_count` and first member reconcile the batch before the caller
continues. Other runs may operate concurrently; every reader treats the explicit
purge fields as a valid bounded-retention state rather than applying normal
archive counter/cardinality equality to the reduced inventories.

The sole retention exception is a never-executed candidate-mode run aborted
during the stopped migration window. After `CJ2_CANCEL_RUN` and bounded
`CJ2_CANCEL_BATCH` leave zero open jobs, leases, reservations, stages, and
request starts, and `reservation_creations_total=0`, migration-admin
`CJ2_PURGE_RUN_BATCH` may remove it immediately
under the exact candidate markers/freeze nonce. Before the first purge batch,
the stopped admin tool exports and hashes the complete candidate-abort report;
that digest is supplied unchanged to every batch. The operation uses the same
100-job inventory bound; its final batch removes the candidate
run from `active_runs`, `unarchived_runs`, and `runs`. This exception cannot
match an active-marker run or any run represented in `first_request_start`.
Lost-final-response reconciliation uses the same all-keys-absent rule with that
abort report digest.

## 11. Scheduling and worker lifecycle

The scheduler MUST read ready IDs in pages of 128 and MUST fail closed if a run
contains more than 10,000 jobs or more than 10,000 ready members. It may inspect
at most 10,001 IDs to detect the violation. Metadata reads are pipelined in the
same bounded pages. Local ordering remains policy priority, stable group ID,
queue score, then job ID; only `CJ2_TRY_CLAIM` decides ownership.
Policy priority here is the lower-first scheduling priority of the job's group
from the exact run-pinned crawl-policy artifact; it is not a mutable Redis value
or a substitute for the job's queue score.

Every worker MUST:

1. start a heartbeat immediately after claim;
2. renew no later than every 10 seconds through fetch, robots, optional render,
   extraction, staging, and commit;
3. set its job context deadline to the earlier of operation deadline and
   authorization expiry;
4. cancel the context immediately on definitive lease loss, run cancellation,
   authorization expiry, or any renewal transport error;
5. suppress every mutation except an idempotent commit-status reconciliation
   after lease loss;
6. stop heartbeat immediately after any acknowledged lease-ending response:
   `RELEASED_READY`, `RETRY_SCHEDULED`, `COMPLETED`, `DEAD`, `CANCELLED`,
   `COMMITTED`, or `ALREADY_COMMITTED`; a transport-ambiguous response is not
   treated as acknowledgment.

Each exact lease session owns the shared per-reservation permit states described
in section 10.3. Connection reconnects and separately decoded replay responses
MUST NOT create independent permit sessions. Request I/O must have stopped and
its typed outcome/evidence be accounted for before releasing the reservation or
building a publishable transcript; a finished reservation alone is not proof of
successful I/O.

The worker sequence is: finish/cancel outstanding request work and reconcile its
reservation; obtain the atomic final-job projection and certify `OutputContext`;
BEGIN with that exact B/G; write the bound stage; independently re-read and
verify it; seal; then commit. Client request admission and BEGIN are serialized
under the lease-session transition mutex, in addition to Redis's atomic freeze.
Admission is held closed while BEGIN's response is ambiguous until exact
reconciliation establishes its result; the worker cannot assume a lost reply
means the fence is unfrozen. Successful BEGIN permanently closes request
admission for that session, including after abort. A stale BEGIN pair may be
replaced only through complete transcript/context revalidation before any stage
has begun, within the existing bounded operation deadline. Lost or uncertain
local permit/event evidence suppresses further I/O and publication on that
fence; uncertain lease ownership remains recovery-owned. Exact completed COMMIT
reconciliation requires only its retained identity, not reconstruction of lost
output evidence.

The authorization timer is a local monotonic duration calculated as
`authorization_expires_at_ms-redis_now_ms` from the latest validated Lua
response. After renew it sets the authorization deadline to the earlier of its
existing monotonic deadline and monotonic-now plus the new nonnegative duration;
it never extends the deadline because Redis time moved backward. The worker MUST
NOT compare authorization against its wall clock. A nonpositive duration
cancels the context immediately.

On `SIGTERM`/`SIGINT`:

- stop new candidate reads and claims immediately;
- release a never-started lease with `CJ2_RELEASE_BEFORE_IO`;
- cancel an active request, finish its reservation if the lease is still
  current, and retry it as `worker_shutdown_after_io`;
- a successfully staged result MAY commit during a maximum 20-second shutdown
  grace while lease, run, and authorization remain valid;
- continue heartbeat during the grace and exit within 30 seconds;
- after uncertain lease ownership, exit without NACK or capacity release.

If shutdown or any failure requires retry/dead/cancel while a stage exists, the
worker first proves its lease remains current, invokes `CJ2_ABORT_STAGE`, then
invokes the required lease-ending transition under the same client transition
mutex; heartbeat renewal is serialized and cannot interleave. After definitive
or uncertain lease loss it aborts only local work, performs no Redis stage abort
or other mutation, and leaves stale stage disposition/cleanup to recovery and
maintenance. A current worker never leaves
an aborted-stage terminal reservation intentionally occupied: only the
lease-ending transition releases it, while an ambiguous abort is reconciled by
exact replay and uncertain lease ownership is left to recovery.

Every continuously running Spider invokes, before its first claim and at least
once per 10 seconds, in this order: expired-lease/reservation recovery, run-
expiry cancellation, cancelled-run batches, due-delay promotion, stage cleanup,
rate-scope maintenance, and run finalization. Each invocation obeys its batch
bound, loops only while `BATCH_MORE` and its maintenance time budget remains,
and yields before claiming. Multiple replicas may perform these idempotent
operations concurrently. Monitoring is read-only and does not own lifecycle
progress. Archive/purge and marker operations remain stopped operator tasks.

The recovery phase covers every member of the at-most-16 `active_runs` set, not
only the run from which that Spider claims. If its bounded recovery sweep cannot
finish within the maintenance budget, that cycle skips global stage cleanup. A
zero-processed `CJ2_CLEAN_STAGE` blocked by an owner slot causes the caller to
yield to the next recovery phase rather than spin on the unchanged earliest
member.

The scaler keeps at least one exact-digest Spider in maintenance-only mode while
the global active-lease index, `stage_slots`, or `stage_expiry` is nonempty,
including after the configured run finalizes. That replica still uses an
explicit retained run ID and all active gates, performs no claim for a
non-active run, and may stop only after all three inventories are empty and no
other scaling input requires it. This prevents an active lease, an aborted-stage
terminal reservation, or committed/recovered residue from depending on a future
crawl for recovery or cleanup.

## 12. Feeder and activation workflow

The seed catalog remains discovery input, not the job ledger. The V2 feeder
MUST operate only on an explicit run ID and MUST never select a latest run.

The feeder workflow is:

1. Load only enabled MongoDB seed records in stable priority/time/ID order.
2. Fail the whole feed if any enabled record is invalid; V2 never inherits the
   V1 feeder's skip-and-continue behavior.
3. Validate each record with the run-pinned canonicalization and crawl-policy V2
   implementation, bind its exact group/rate tuple before ready, and map Mongo
   priority `1..3` to score text `0..2` as defined in section 4.
4. Compute the exact section 4 source digest over job-ID-sorted bound records.
5. Call `CJ2_CREATE_RUN` with authorization, canonicalization, crawl/render
   policy digests, and the complete bounded group map.
6. Call `CJ2_ENQUEUE_BATCH` in batches of at most 500.
7. Re-read `job_order` in bounded pages and independently recompute the same
   record stream and digest.
8. Call `CJ2_BEGIN_RUN_AUDIT`; call `CJ2_AUDIT_RUN_BATCH` with at most 100 exact
   records until `BATCH_DONE`.
9. Call `CJ2_SEAL_RUN` with exact count and digest.
10. Exit. The feeder MUST NOT activate the run or start the Spider.

Disabled catalog records are absent from a fresh run. A feeder replay may modify
only the same loading run with byte-identical records and immutable metadata; a
changed score, depth, binding, or source requires a new run ID. Once auditing
begins, no feeder mutation is allowed.

Activation is a separate, explicit, one-shot operator action after all startup,
durability, compatibility, policy, authorization, queue, and isolation checks.
Its confirmation text MUST be exactly:

```text
<run_id>:<source_sha256>:<authorization_sha256>:<crawl_policy_sha256>
```

No default command in Compose may activate or execute a crawl.

## 13. Stopped V1 migration

The exact legacy-key retirement set, in bitmap order, is:

```text
mifolyo:crawl:v1:queue   ZSET
mifolyo:crawl:v1:urls    HASH
mifolyo:crawl:v1:depths  HASH
spider_queue             historical literal key; never an alias for the namespaced ZSET
signal_queue             retired wakeup LIST
```

Legacy evidence uses:

```text
sha256(F("mifolyo:legacy-key-evidence:v2") || F(literal_key_name) ||
       F(redis_type) || SECTION("entries", canonical records))
```

`none` has zero records; a list has ordered fields `index`, `value` in ascending
index order; a ZSET has ordered fields `member`, `score_text` sorted by member
bytes, with score text validated canonically; and a hash has ordered fields
`field`, `value` sorted by field bytes. The namespaced queue accepts only `none`
or `zset`, each namespaced metadata key only `none` or `hash`, historical
`spider_queue` only `none`, `list`, or `zset`, and `signal_queue` only `none` or
`list`. The queue and either literal key above 10,000 entries, either metadata
hash above 20,000 fields, any other type, a noncanonical ZSET score, or a
scan/page response above 2 MiB blocks
cutover for separate offline disposition. Reads use pages of at most 500 and the
same bounded streaming RESP behavior as the hash audit. Evidence artifacts
contain counts/digests, never raw entries. Legacy field/member/value bytes are
opaque for this evidence calculation and are an explicit exception to the V2
UTF-8 text rule.

The first three are the only V1 job source. Literal `spider_queue` is a
separately evidenced historical key and MUST NOT be interpreted as
`mifolyo:crawl:v1:queue`. Neither it nor `signal_queue` contains migratable job
work. Existing downstream `pages_queue`, image queues, processing/dead lists,
and immutable output keys are not in this retirement set; they are drained and
retained/consumed under their own contracts.

Migration MUST be a separate stopped administrative tool. It is not linked into
the Spider's execution path and does not create dual reads.

Before migration, every feeder, Spider, Indexer, Image Indexer, Backlinks
Processor, trigger, scheduler, scaler, and ordinary Monitoring process MUST be
stopped. An approved boot and exact candidate markers/freeze evidence must be
installed while active markers remain absent. The migration-admin tool MUST:

1. validate exact V1 key types;
2. read no more than 10,001 queue members in bounded pages;
3. reject the migration if pending work exceeds 10,000;
4. require every queued member's URL/depth metadata, canonical ID, canonical
   score text, enabled catalog record, and exact pinned-policy binding to agree;
5. reject either URL/depth hash above 20,000 fields, then inspect with
   `HSCAN COUNT 128`; because Redis `COUNT` is a hint, reject any returned page
   above 512 entries or 2 MiB using a streaming RESP decoder that closes the
   connection at the byte bound, keep at most 4 MiB of scan/work buffers, and
   compute orphan-evidence shards over at most 1,000 IDs (20,000 total per hash),
   storing only each shard's count and digest. Report but never migrate hash
   fields lacking queue membership;
6. compute and record the exact section 4 source digest before writing;
7. create one candidate-mode loading `source_kind=v1_migration` run with its
   bounded group map and enqueue at most 500 policy-bound jobs per operation;
8. re-read V1 and require unchanged count, values, and digest;
9. freeze V2 loading, run all bounded audit batches, and seal the candidate run;
10. re-read V1 once more and require the same digest before retirement;
11. leave all five legacy keys unchanged until `CJ2_RETIRE_LEGACY_KEYS`.

Any invalid or changing queued member aborts before a V2 run is sealed. Partial
loading/auditing V2 state remains unclaimable and may be cancelled/purged only
through the finite candidate-mode operations before retry or coordinated
restore.

A separately evidenced inventory records a content digest for each of the five
literal keys, plus the admitted types and cardinalities above, without logging
values. The first three digests are stored as `v1_queue_evidence_sha256`,
`v1_urls_evidence_sha256`, and `v1_depths_evidence_sha256`; they complement, not
replace, the policy-bound V1 source digest. Any nonempty `spider_queue` requires
an explicit historical disposition; it is never copied into V2. With all
processes still frozen, `CJ2_RETIRE_LEGACY_KEYS` may delete only the five exact
keys after a matched Redis/Mongo backup and confirmation containing the V1 count
and source digest plus all five key-evidence digests in the exact
`CJ2_RETIRE_LEGACY_KEYS` confirmation grammar. It MUST NOT use
`FLUSHDB`, `FLUSHALL`, a wildcard, or an alias-derived key name.

Legacy keys, candidate markers, and an unclaimable candidate V2 run may coexist
only during this stopped migration window. Active compatibility/contract/guard
keys and run activation MUST remain absent. After all five keys are proven
absent, `CJ2_PROMOTE_CANDIDATE_CONTRACTS` atomically installs active authority.
No producer or consumer may run during coexistence.

For a fresh non-migration run, the V1 queue count must already be zero and no V2
run is created while legacy keys exist. The same stopped retirement and
candidate-marker promotion occur first; only then does the active-mode feeder
create, audit, and seal the fresh Mongo source run. Nonzero pending V1 work must
use the validated migration path rather than be relabeled as a fresh cutover.

## 14. Startup and Compose requirements

### 14.1 Spider startup order

Execution mode MUST complete these checks before spawning a goroutine:

1. Decode and validate exact crawl and render policies without network access.
2. Calculate policy, render-policy, and canonicalization digests.
3. Connect with authenticated Redis credentials and bounded timeouts.
4. Verify standalone Redis, every exact section 2 setting including
   `aof-load-truncated no`, loaded AOF status, noeviction, the release sizing
   formula, and approved current Redis run ID/evidence.
5. Verify exact active compatibility, crawl-contract, Redis-config, and commit-
   guard values; reject every section 5.1 sentinel/provisional/fixture artifact;
   candidate markers and `admin_freeze` must be absent.
6. Require the exact immutable legacy-retirement evidence and release proof that
   promotion/activation observed all five literal legacy names absent. The
   Spider has no command path or ACL access for those names.
7. Verify the explicit run, authorization expiry, state, all pinned digests,
   global tuple, complete bounded policy group map, open counters, and rate-scope
   lineages.
8. Verify script source SHA-256 values and load them with `SCRIPT LOAD`.
9. Run bounded recovery and state-consistency preflight.
10. Only then start maintenance and workers.

Worker goroutines require the configured run to be `active`. A configured
completed, budget-exhausted, cancelled (whether or not finalized), or
archived-but-unpurged run is accepted only for the section 11 maintenance-only
mode; that mode never claims and exits only when the global active-lease,
stage-slot, and stage-expiry inventories are empty and all due lifecycle work is
complete. It validates the immutable authorization
artifact/digest but does not require a terminal run's authorization timestamp to
remain in the future; expiry grants no execution permission.

Validation-only mode performs no Redis or network operation. Execution mode
MUST reject the presence of `CRAWL_QUEUE_KEY`, `CRAWL_URLS_KEY`,
`CRAWL_DEPTHS_KEY`, or `STARTING_URL` rather than ignoring them.

Required execution configuration includes:

```text
CRAWL_RUN_ID
CRAWL_JOBS_CONTRACT_SHA256
COMPATIBILITY_MANIFEST_SHA256
REDIS_CONFIG_SHA256
COMMIT_GUARD_SHA256
REDIS_DURABILITY_EVIDENCE_SHA256
LEGACY_RETIREMENT_RECORD_SHA256
CRAWL_AUTHORIZATION_FILE
CRAWL_POLICY_FILE
RENDER_POLICY_FILE
```

### 14.2 Consumer startup gates

Before any Redis or MongoDB mutation, the target Indexer, Image Indexer,
Backlinks Processor, and Monitoring image MUST verify approved boot, the exact
active compatibility manifest, its own immutable image digest, required output
contract versions, standalone Redis, absence of candidate/freeze state, and the
exact legacy-retirement evidence plus release absence proof. They reject every
section 5.1 sentinel/provisional/fixture artifact. Their ACLs and code contain no
access path for the five legacy names. A mismatch exits before
acquiring an owner lock or processing work.

The Indexer MUST have no code path that reads, writes, recreates, deletes, or
waits on `signal_queue`; starting or restarting it against empty queues must
perform no Redis write. It continues to use exact `pages_queue` and
`pages_queue:processing` handoff and only enqueues an image manifest as specified
in section 6.4. It observes queue state with read-only commands and acquires/
renews its existing fenced owner lock only after work is observed; a losing race
releases/expires that lock without touching queue contents. Monitoring is
read-only. These gates make the images currently
listed in an active manifest the only consumers allowed before run activation.

### 14.3 Compose

Every crawl-capable Compose definition MUST:

- start Redis with the exact persistence and noeviction settings in section 2;
- use a persistent named volume and no cluster mode;
- disable or rename `FLUSHDB` and `FLUSHALL`;
- provide authenticated service-specific credentials outside isolated tests;
- provide a Redis container limit and `maxmemory` that satisfy the approved
  section 2 formula, four stage reservations, commit and lease-safety
  reservations, and 128 MiB process margin; the isolated test stack minimums
  are 400 MiB `maxmemory` and 528 MiB container limit;
- keep the Spider behind an explicit profile with a validation-only default
  command;
- contain no section 5.1 harness image, fixture credential, provisional artifact,
  or fixture setup path in any deployable profile;
- contain no V1 crawl key variables and no default run activation;
- grant runtime services no candidate-marker, boot-approval, legacy-retirement,
  marker-promotion, unrelated-keyspace, `CONFIG SET`/`REWRITE`/`RESETSTAT`, or
  flush permission; permit `CONFIG GET` only for startup/config-gate reads, and
  scope required data commands/key patterns as narrowly as Redis ACL permits; direct
  data-command prohibition is enforced by the immutable-image boundary described
  in section 10.1, not falsely claimed as ACL privilege elevation;
- keep rendering and external image fetching disabled unless separately
  authorized;
- expose health only after protocol, boot, and run readiness pass.

`appendfsync everysec`, an ephemeral Redis volume, a successful `PING`, or a
managed-service marketing claim without restore evidence does not pass this
contract.

## 15. Monitoring contract

Monitoring is read-only. It MUST use explicit configured run IDs, at most 16,
and MUST NOT select an arbitrary latest run, invoke maintenance, mutate a
heartbeat key, or read/write V1 keys or `signal_queue`. It validates exact key
types and reports corruption rather than repairing or silently treating a wrong
type as zero.

Bounded run/counter, rate-scope, and memory/stage consistency snapshots use
reviewed SHA-256-verified `EVALSHA_RO` sources with no write command and the same
active gate records; they are monitoring queries, not transition operations.
This prevents a concurrent legal transition from creating a false mixed-time
corruption result. Backlink cursor scanning remains the separately defined
multi-scrape approximation below.
The configured set MUST contain every member of the bounded `active_runs` set;
additional configured IDs may name retained terminal runs. An unconfigured
active member is a failed protocol gate, not a run selected implicitly.
Separately, one bounded global snapshot may inspect all at-most-128 `runs`
members solely to validate/count retained states and explicit purge progress; it
exports no per-run series for an unconfigured terminal ID and is not latest-run
selection.

Every exported `*_ref` is the first 16 bytes (32 lowercase hexadecimal
characters) of HMAC-SHA-256 under the mounted deployment monitoring key over
`F(reference_kind) || F(raw_identifier)`. The key is not stored in Redis or
logged. A collision within the current scrape suppresses both ambiguous series
and raises `monitoring_identity_collision`.

For each run it reports:

```text
run_ref
run_state
purge_state
purged_job_count
authorization_seconds_remaining
audit_count
audit_complete
job_count
open_job_count
ready
leased
delayed
due_delayed
completed
dead
cancelled
oldest_ready_age_seconds
oldest_lease_age_seconds
expired_leases
request_starts
reservation_creations_total
reservation_creation_limit
reservation_creations_remaining
pending_request_reservations
started_request_reservations
request_starts_remaining
group open, started, pending, limit, and starts remaining by hashed group reference
claims_total
retries_total
recovered_leases_total
renewal_rejections_total
output_commits_total
commit_backpressure_jobs
oldest_commit_backpressure_seconds
retry, recovery, and disposition reason counts over their closed field sets
```

For every non-purging run, Monitoring checks and exposes whether all primary index
cardinalities match the run counters, their sum equals `job_count`,
`open_job_count=ready+leased+delayed`, group-open sums equal open jobs, and each
closed reason-counter sum equals its parent total. It requires
`claims_total <= reservation_creations_total <= 100` and reports remaining
creation capacity as `100-reservation_creations_total`. It also checks every
bounded commit-backpressure member against the corresponding leased job fields.
Every job it inspects must satisfy section 7.2's retained baseline/start and
freeze relations; owned-stage consistency checks also compare the metadata
interval with that exact current job/fence. Historical recovered residue is not
compared against a newer job interval. These checks add no metric or response
shape and do not expand the bounded job/stage inventories read by a snapshot.
A mismatch emits one `state_corrupt` condition and pages an operator; it never
guesses which side is correct.
Across configured runs that are members of the global `active_runs` inventory,
including an unfinalized cancelled run, the sum of validated leased
cardinalities must equal the at-most-64 global active-lease cardinality, and
every composite member, score, and owning job must agree exactly.
When `audit_revision=0`, `audit_group_counts` must be absent. When it is nonzero,
the hash has exactly the run's group field set and sums to `audit_count`; this
also covers a run cancelled partway through audit. Once `audit_complete=1`, it
sums to `audit_count=expected_seed_count` and remains immutable even though
discoveries later increase `job_count` and live `group_open_jobs` changes.
For an explicitly purging retained run, Monitoring instead requires the exact
evidence/start fields, `purged_job_count>0`, and
`jobs=job_order=job_count-purged_job_count`, with the remaining primary-index
sum equal to that same value. Frozen archive counters and audit fields remain
reported but are not compared to the deliberately reduced inventories. The
bounded global snapshot validates the same reservation-counter relation for
every retained run and requires the sum across `unarchived_runs` to be at most
10,000.

It also reports:

```text
redis_boot_state
redis_run_id_match
aof_loaded
aof_enabled
aof_last_write_status
active_contract_match
active_compatibility_match
redis_config_match
commit_guard_approved
candidate_or_freeze_present
memory_headroom_bytes
stage_slots
stage_reserved_bytes
oldest_stage_age_seconds
stage_cleanup_backlog
oldest_stage_cleanup_overdue_seconds
rate_scope_inventory_size
active_leases
unarchived_runs
unarchived_reservation_creations
retained_runs
purges_in_progress
first_request_start_present
pages_queue_depth
page_processing_depth
page_dead_letter_depth
image_queue_depth
image_processing_depth
image_dead_letter_depth
backlink_backlog
rate_active by hashed scope reference
rate_pending by hashed scope reference
rate_started by hashed scope reference
rate_next_allowed_seconds by hashed scope reference
```

Per-scope metrics are limited to the global scope, group scopes named by the
configured runs' bounded group maps, and origin scopes named by currently active
reservations, with at most 256 scope records read per scrape; idle origin scopes
are represented only in the aggregate durable-inventory cardinality.
Continuation across configured run group maps is read-only and process-local. Active
reservation scopes are found without a key scan by reading the known global
scope's `:active` ZSET, inspecting at most three members to detect a violation
of its two-member bound, and those exact reservation hashes.
Monitoring MUST NOT export raw scope IDs as labels; it uses the common keyed
reference above.

There is no new backlink-target index in V2. `backlink_backlog` is therefore the
last completed read-only, process-local scan-cycle observed aggregate,
accompanied by its age and `scan_complete` flag—not an instantaneous exact
gauge. Duplicate keys are suppressed within a page; a cycle concurrent with
mutation remains approximate. Each scrape makes
at most one `SCAN MATCH backlinks:* COUNT 100` call and one `SCARD` for each
returned key; since `COUNT` is a hint, a reply over 100 keys or 2 MiB is
cut off by a bounded streaming RESP decoder and raises
`monitoring_scan_bound_exceeded`. Monitoring stores no scan cursor or aggregate
in Redis and never uses `KEYS`.

Required metric names are:

```text
mifolyo_crawl_run_info{run_ref,state}
mifolyo_crawl_purge_in_progress{run_ref}
mifolyo_crawl_purged_jobs{run_ref}
mifolyo_crawl_jobs{state,run_ref}
mifolyo_crawl_state_consistent{run_ref}
mifolyo_crawl_authorization_seconds_remaining{run_ref}
mifolyo_crawl_audit_jobs{run_ref}
mifolyo_crawl_audit_complete{run_ref}
mifolyo_crawl_oldest_ready_seconds{run_ref}
mifolyo_crawl_oldest_lease_seconds{run_ref}
mifolyo_crawl_due_delayed_jobs{run_ref}
mifolyo_crawl_expired_leases{run_ref}
mifolyo_crawl_request_starts_total{run_ref,group_ref}
mifolyo_crawl_request_start_limit{run_ref,group_ref}
mifolyo_crawl_request_starts_remaining{run_ref,group_ref}
mifolyo_crawl_reservation_creations_total{run_ref}
mifolyo_crawl_reservation_creation_limit{run_ref}
mifolyo_crawl_reservation_creations_remaining{run_ref}
mifolyo_crawl_request_reservations{run_ref,group_ref,state}
mifolyo_crawl_group_open_jobs{run_ref,group_ref}
mifolyo_crawl_claims_total{run_ref}
mifolyo_crawl_retries_total{run_ref,reason}
mifolyo_crawl_recovered_leases_total{run_ref,outcome}
mifolyo_crawl_dispositions_total{run_ref,state,reason}
mifolyo_crawl_renewal_rejections_total{run_ref}
mifolyo_crawl_stale_token_rejections_total{operation}
mifolyo_crawl_output_commits_total{run_ref}
mifolyo_crawl_commit_backpressure_jobs{run_ref,reason}
mifolyo_crawl_oldest_commit_backpressure_seconds{run_ref}
mifolyo_crawl_memory_headroom_bytes
mifolyo_crawl_stage_slots
mifolyo_crawl_stage_reserved_bytes
mifolyo_crawl_oldest_stage_seconds
mifolyo_crawl_stage_cleanup_backlog
mifolyo_crawl_oldest_stage_cleanup_overdue_seconds
mifolyo_crawl_rate_scope_inventory
mifolyo_crawl_active_leases
mifolyo_crawl_unarchived_runs
mifolyo_crawl_unarchived_reservation_creations
mifolyo_crawl_retained_runs{state}
mifolyo_crawl_purges_in_progress
mifolyo_crawl_first_request_start_present
mifolyo_crawl_downstream_queue_depth{queue}
mifolyo_crawl_backlink_backlog
mifolyo_crawl_backlink_scan_complete
mifolyo_crawl_backlink_scan_age_seconds
mifolyo_crawl_lua_duration_seconds{operation}
mifolyo_crawl_rate_active{scope_ref}
mifolyo_crawl_rate_reservations{scope_ref,state}
mifolyo_crawl_rate_next_allowed_seconds{scope_ref}
mifolyo_crawl_protocol_gate{gate}
```

`mifolyo_crawl_stale_token_rejections_total` is a process-local Spider metric;
`mifolyo_crawl_lua_duration_seconds` is emitted by each pinned client that calls
an operation. They are not inferred from Redis and are aggregated across
instances by the metrics backend. Every other name above is emitted by the
read-only Monitoring service from the bounded Redis snapshots. The durable
renewal-rejection counter remains the authoritative run total for renewal calls
specifically.

For request-start and reservation metrics, `group_ref="all"` is the run-global
series; all other `group_ref` values use the HMAC-derived group reference.
Reservation state is `pending` or `started`. The reservation-creation metrics
are respectively the stored cumulative counter, constant `100`, and
`100-reservation_creations_total`; none is inferred from currently present
tombstone keys. The `jobs` state label is exactly
`all`, `open`, `ready`, `leased`, `delayed`, `completed`, `dead`, or `cancelled`;
the first two expose the stored aggregate counters and the others expose primary
index cardinalities. The one `run_info` series whose `state` matches the run has
value `1`. Purge-in-progress is `1` exactly for `purge_state=in_progress`, and
the purged-jobs series is its canonical counter (otherwise `0`).
`mifolyo_crawl_unarchived_runs` is the exact `SCARD` of that global
inventory; `mifolyo_crawl_unarchived_reservation_creations` is the bounded sum
of those runs' validated cumulative counters and cannot exceed 10,000.
`mifolyo_crawl_retained_runs` counts `runs` members by validated run state and
`mifolyo_crawl_purges_in_progress` counts their validated explicit purge markers.
The `queue` label uses one of the six exact downstream list names in section 6.4.
The closed `gate` labels for `mifolyo_crawl_protocol_gate` are
`boot_approved`, `redis_run_id_match`, `aof_loaded`, `aof_enabled`,
`aof_last_write_ok`, `active_contract_match`, `active_compatibility_match`,
`redis_config_match`, `commit_guard_approved`, `candidate_freeze_absent`,
and `legacy_retirement_match`. Each has value `0` or `1`;
the gate metric is not a replacement for any numeric metric listed above.

`mifolyo_crawl_stage_reserved_bytes` is the sum of the four-or-fewer remaining
reservation values in `stage_slots`, including an aborted-stage terminal
reservation awaiting its lease-ending transition or recovery. The exact signed
calculation is:

```text
memory_headroom_bytes = maxmemory - used_memory - stage_reserved_bytes
                        - COMMIT_MEMORY_RESERVATION_BYTES
                        - LEASE_SAFETY_RESERVATION_BYTES
```

Negative values are reported rather than clamped. Both values use one
internally consistent read sample.
`mifolyo_crawl_oldest_stage_seconds` considers only owned active-stage slots and
their validated metadata; the snapshot instead validates an aborted terminal
reservation against its job, abort transition, current fence, and lease. Cleanup
backlog is the exact `ZCARD(stage_expiry)`;
its overdue age is zero when empty or
`max(0, redis_now_ms-earliest_cleanup_due_at_ms)/1000`.

Until a metrics endpoint is reviewed, stable structured log records satisfying
these names are sufficient. Scaling considers `ready + due_delayed +
expired_leases` and holds one maintenance replica whenever
`active_leases>0`, `stage_slots>0`, or `stage_cleanup_backlog>0`, but an invalid
protocol gate or state-consistency result forces the desired Spider count to
zero. Repeated idle periods MUST perform no Redis write and `signal_queue` must
remain absent.

Alerts are mandatory for a protocol gate at zero; candidate/freeze presence or
legacy-retirement mismatch; AOF/config/run-ID mismatch; state corruption; an
authorization window below 60 seconds with open jobs; a stage older than 12
minutes; stage cleanup overdue for 30 seconds; four occupied stage slots for 30
seconds; signed memory headroom below 32 MiB for 30 seconds; commit backpressure
older than 90 seconds; any expired lease/reservation for 30 seconds; rate-scope
inventory at 90,000; reservation creations at 90 for a run with open jobs;
unarchived reservation creations at 9,000; global active leases at 58;
unarchived/total retained runs
at 90/115; downstream queue
depth at 90% of its bound (`pages_queue=4500`, with other exact queue-depth
bounds taken from the release sizing artifact); or Lua p99 at 80 ms over five
minutes. Crossing an alert threshold grants no mutation or automatic repair
authority.

Operational logs may contain only stable event/status/reason codes, hashed run,
job, group, origin, and owner references, fence, attempt counts, and durations.
They MUST NOT contain canonical URLs, raw Redis values, HTML, tokens,
reservation IDs, authorization contents, or wrapped exception messages.

## 16. Cutover and rollback

### 16.1 Stop-and-drain cutover

The release is coordinated; it is not rolling.

1. Stop every V1 feeder, trigger, scheduler, scaler, and Spider so no new crawl
   or downstream work can be created.
2. Keep only the reviewed old consumers running long enough to drain page,
   image, backlink, source, processing, and dead-letter work to stable zero
   twice, with an explicit disposition for any dead-letter evidence.
3. Stop Indexer, Image Indexer, Backlinks Processor, and Monitoring and prove all
   owner locks absent or expired. Revoke the old release's Redis credentials,
   prove their authentication fails, and record the process-stop inventory.
4. Take matched, checksummed Redis and MongoDB backups inside that write freeze
   and prove a restore in an isolated environment.
5. Approve the current Redis boot using current rehearsal evidence, then install
   the reviewed candidate compatibility/contract markers and freeze record.
6. For V1 migration only, create, enqueue, re-read, audit, and seal the
   unclaimable candidate run while proving V1 unchanged. For a fresh Mongo run,
   create no run yet.
7. Inventory and explicitly retire all five exact legacy keys with
   `CJ2_RETIRE_LEGACY_KEYS`; prove each literal key absent and wait for
   `lazyfree_pending_objects=0`; export/checksum the exact immutable retirement
   record for every target runtime.
8. Atomically install the commit guard and promote the candidate markers with
   `CJ2_PROMOTE_CANDIDATE_CONTRACTS`, supplying the exact `fresh` or
   `v1_migration` mode and candidate run identity; prove candidate/freeze state
   absent.
9. For the fresh-run path, run the active-mode feeder now to create, enqueue,
   independently re-read, audit, and seal the explicit Mongo run.
10. Start exact-digest Backlinks Processor, Image Indexer, Indexer, and
    Monitoring images first; verify every startup gate and empty drained queue.
11. Validate and explicitly activate the sealed V2 run.
12. Start exact-digest V2 Spider replicas last with the explicit run ID.
13. On the first successful request start, record the immutable boundary key and
    timestamp in the run report; absence before then is also an asserted gate.

### 16.2 Irreversible boundary

The irreversible rollback boundary is the first successful
`CJ2_START_REQUEST`, not the first page publication. That script both consumes
a durable request start and creates `mifolyo:crawl:v2:first_request_start`
before DNS. Once it exists, an externally observable network action may have
occurred even when no output was committed.

Before that key exists, all V2 services may be stopped and the matched V1
snapshot, old release, and old credential set restored together.

After that key exists:

- never point a V1 Spider or feeder at unreverted live V2 state;
- never restore only Redis or only MongoDB;
- stop every producer and consumer, then restore the matched Redis/Mongo backup
  as one coordinated rollback, or roll forward;
- retain the authorization and request-start evidence even when no page was
  published.

## 17. Required tests and acceptance evidence

Authoritative script tests MUST use real Redis 7. Miniredis may test Go/Python
validation and DTOs but is not evidence for Lua atomicity, `TIME`, AOF, or crash
recovery.

### 17.1 State and idempotency

- Every legal state transition and every illegal edge.
- Every stable response and error code.
- Whole-batch enqueue rejection without partial writes.
- Loading freezes at `audit_revision`; changed, omitted, duplicated, or out-of-
  order audit records mutate no cursor, and a 10,000-job run seals without an
  unbounded script.
- Post-seal discoveries may increase `job_count` and live group-open counts but
  never change the frozen source audit count, per-group audit counts, or source
  digest.
- Job membership in exactly one primary state index after every operation.
- Every lease has one exact global active-lease member; the 65th concurrent
  claim is blocked without removing ready work, and renewal/outcome/recovery
  updates or removes both lease indexes atomically.
- Run/open/terminal and every group-open, pending, active-started, cumulative-
  started, audit-group, retry-reason, recovery-outcome, and disposition-reason
  equality after every operation and exact replay.
- Same claim token returns the same fence and reservation.
- New seed and discovered jobs initialize `lease_request_starts_baseline=0` and
  `request_starts=0`. New claim captures the pre-claim cumulative job starts
  exactly once; claim replay, blocked claim, and visited completion do not
  replace that baseline. A pre-I/O lease has `G=B`, and a started lease has
  `G>B`, even when prior fences consumed starts or all timestamps are equal.
- Finish, pending cancellation, release, retry, abort, recovery, completion,
  dead-letter, cancellation maintenance, and stage cleanup retain B/G and fence
  history. A later claim alone replaces B with the retained G, without resetting
  G. Missing baseline fields and invalid stored baseline/start relations fail
  closed rather than receiving a zero/default repair.
- Reserve/start/finish/cancel retries change counters exactly once; every first
  claim/reserve creation increments `reservation_creations_total` once and no
  replay does.
- Creation 100 succeeds within the other gates. Attempted creation 101 through
  claim and reserve returns `RUN_RESERVATION_LIMIT_EXHAUSTED` with the exact
  response tail, preserves ready/lease/reservation state, and changes only a
  separately valid monotonic rate tightening. Repeated pending cancel/release on
  one job can reach the run limit, proving there is no hidden per-job cap.
- At the creation limit, an existing reservation can start/finish and fetched
  output can commit without a new request. A pre-I/O lease releases ready; an
  after-I/O job requiring another request becomes dead exactly once with
  `reservation_limit_exhausted`, never delayed, and an otherwise open drained
  run finalizes with the matching reservation-limit status/reason and stated
  precedence.
- Delivery attempts increment once per fence while every request start consumes
  run and group budget.
- Failed DNS/dial/HTTP attempts, timeout, post-START cancellation, and an unused
  recorded START still consume the job/run/group starts and the fence's delivery
  attempt. FINISH and its replay do not attest network success or refund starts.
- Attempt 1 retries after exactly 30 seconds, attempt 2 after exactly 120
  seconds, and attempt 3 creates exactly one dead record whose response fourth
  element is the literal bulk string `retry_exhausted`.
- Cancellation is terminal and distinct from release-before-I/O.
- Authorization expiry prevents claim, start, retry, and first commit.
- Finalization uses only bounded counters/group fields, leaves budget-exhausted
  jobs as evidence, freezes the retention anchor, and refuses premature archive
  or purge.
- Archive export recomputes `sum(next_request_ordinal-1)` across its bounded job
  scan, requires equality with `reservation_creations_total <= 100`, and rejects
  a changed counter, ordinal, or fixed-limit value even after tombstones expire.

### 17.2 Crash injection

- SIGKILL immediately after claim recovers after lease expiry without consuming
  a delivery attempt or request start.
- SIGKILL immediately after recorded request start preserves the consumed job,
  run, and group counters, retained baseline, and rate deadline.
- Lost START response with a locally known unused session reconciles the same
  reservation and can yield only its one eligible permit. Two separately parsed
  permitted replies, concurrent replay callers, copied replies/permits, and
  connection reconnects share one permit state and cannot issue/use it twice.
  An intentionally unused grant retired before FINISH/freeze cannot be revived
  by a retained permit or delayed reply.
  The permission bit may change to zero without changing the original start
  snapshots. Lost FINISH reply never repeats I/O. Missing or uncertain local
  permit/event state, including process restart, never resumes old-fence I/O or
  manufactures successful output evidence.
- Historical START replay after a newer fence captures another baseline returns
  only its own original timestamp/counter snapshots with `io_permission=0` and
  changes no newer job/reservation. Finished/expired, authorization-expired, and
  cancelled-run reconciliation cannot mint a permit or a successful response
  event. A missing/expired tombstone cannot be reconstructed from current counts.
- SIGKILL during DNS/fetch/render produces no visible partial output.
- SIGKILL after each bounded stage-write point leaves only expiring invisible
  stage data.
- Stage begin/write/seal/abort lost-response replays preserve byte-identical
  stage state and return their defined reconciliation statuses; changed chunks
  fail, expiries never extend, and crash/recovery/cleanup never
  leave more than four stage slots or follow an unvalidated key. Abandoned
  stages retain their original cleanup deadline; a first commit changes only
  its residual bundle's deadline to the exact common at-most-60-second expiry.
- BEGIN lost-response replay checks the identical active stage before one-stage
  and new-capacity admission, including after seal, and retains the same B/G,
  expiry, and current slot remainder. Changed immutable fields under that ID
  fail as `IMMUTABLE_MISMATCH`; a different BEGIN on the frozen current fence is
  `INVALID_STATE`. Ambiguous BEGIN holds local request admission closed.
- Race a reservation/start against BEGIN: an outstanding pending/started
  reservation prevents BEGIN, a start finished after the witness read rejects
  stale B/G with `STAGE_INVALID`, and BEGIN winning first prevents every new
  reservation/start. Failed BEGIN, including slot/memory capacity blocks,
  changes no baseline, count, freeze, stage key, or slot. Cancelling an unstarted
  reservation between read and BEGIN does not create a false transcript gap.
- Every renewal while a stage is active is capped at that stage's original
  absolute expiry. At the cleanup deadline a still-owning job's lease is due,
  cleanup never moves that job, and expired-lease recovery performs its one
  disposition.
- SIGKILL after an acknowledged abort but before its lease-ending outcome leaves
  the terminal slot reserved; active-lease/stage-slot scaling keeps maintenance
  available until expiry recovery removes it exactly once without stage keys or
  an expiry member.
- After abort, the unchanged `last_stage_fence` rejects new reserve/start and
  restaging even though the active-stage field and keys are absent. Renewal is
  still forbidden. Exact abort replay precedes materialized-key checks, and
  only the serialized lease-ending transition or expiry recovery releases its
  terminal slot. Neither path resets B/G or last-stage history.
- Recovery validates any still-present owned metadata interval before clearing
  ownership, then retains its snapshots while abandoning it. After a new fence
  captures a new baseline, cleanup of the older residual stage succeeds under
  its own identity/expiry rules without comparing it to, mutating, or releasing
  the new fence or stage. Expired metadata is not replaced with current counts.
- Client death and Redis process `SIGKILL` at every prevalidation/mutation/
  acknowledgment boundary of maximum-shape commit yield, after same-volume AOF
  restart, either fail-closed startup on AOF damage, no first commit, or one
  complete commit, never an accepted subset.
- A dropped commit response followed by retry leaves one completed job, one page
  notification, one backlink/discovery effect set, and all exact image/page
  objects.
- Exact completed COMMIT replay after stage cleanup, reservation tombstone
  expiry, downstream output deletion, and run authorization expiry/cancellation
  recomputes identity from the supplied fence/token and retained publication/B/G
  before live-state or stage/destination checks. It returns only the original
  receipt under valid boot/marker/guard gates and recreates nothing. Wrong
  token/fence/commit ID, no-output completion, and missing/purged job state cannot
  return `ALREADY_COMMITTED`.
- A stale fence cannot renew, reserve, start, finish, release, retry, dead-letter,
  cancel, stage, abort its former or any newer stage, seal, commit, or ACK new
  effects; the explicitly allowed exact historical receipts perform no mutation
  and grant no I/O or publication authority. Recovery owns stale-stage
  disposition.
- A live worker cannot reset commit backpressure's first Redis timestamp and, at
  the persisted deadline no later than 120 seconds, either aborts the stage
  before exactly one retry while its lease remains valid or loses the race to
  exactly one expired-lease recovery. Earlier worker death leaves that timestamp
  and stage durable until recovery atomically abandons them and performs exactly
  one lease-expiry disposition.

### 17.3 Shared budgets and rate scopes

- Two independent Spider clients and processes contending for one job produce
  one lease.
- Same-run and overlapping-run workers never exceed global, group, or origin
  concurrency.
- Run A budget exhaustion does not consume run B's budget.
- Both runs still share origin/group active capacity and next-allowed deadlines.
- Crashes before reservation, after reservation, and after start preserve the
  ten-start run limit, 100-creation run limit, exact policy group limit, and
  one-increment-per-creation accounting.
- A claim rejects a changed initial group ID, rate lineage, group scope, or
  source-origin scope before mutation. Initial robots and document intents with
  the inherited immutable job binding pass; cancelling and replacing the
  pre-I/O intent cannot change that binding or use redirect/render-resource, and
  group-open budget finalization remains exact.
- A policy transition preserves the stricter active concurrency and unexpired
  interval; lowering concurrency never releases live work.
- Tightening a zero interval while multiple reservations are pending makes
  `CJ2_START_REQUEST` serialize them against the new deadline; no two starts use
  the obsolete interval.
- Expired reservations recover, while nonexpired reservations remain counted.
- Pending cancellation refunds pending run/group budgets; started finish/expiry
  never refunds cumulative starts or shortens a deadline, and no terminal path
  refunds `reservation_creations_total`.
- Scope records never relax or disappear, integrity maintenance is bounded and
  read-only, witness collisions fail closed, and the 100,001st lineage is
  rejected without mutation.

### 17.4 Atomic output contract

- Maximum and empty page/outlink/image/discovery/alias shapes.
- Exact digest vectors agree in Go, Python, Lua, and the independent verifier for
  unsigned-64 framing, empty sections, Unicode byte order, score text, URL,
  target, token, scope, policy-decision, group-map, transition, reservation,
  chunk, source, output, publication, and commit identities.
- Commit vectors append exactly `F(canonical_decimal(request_starts_baseline))`
  then `F(canonical_decimal(request_starts_generation))` under the unchanged
  `mifolyo:crawl-commit:v2` domain. Changing either valid value while preserving
  semantic output/publication changes commit ID, stage keys, chunk digests, and
  abort transition payload/ID, but not output/publication hashes or downstream
  grammar. Earlier commit formulas, omitted interval fields, reordered fields,
  and old contract digests have no backward fallback.
- The final witness is exactly the thirteen-field section 8.3 job projection
  from one authenticated read on the internally derived job key. Wrong arity,
  missing/non-bulk/noncanonical values, wrong state, active reservation, changed
  owner/token/fence, cross-run/job binding, and invalid target/digest/timestamp
  relations cannot construct output authority. Caller-supplied raw projections
  or a guessed baseline are not production authority.
- Transcript closure rejects an omitted initial robots or document start even
  when the remaining suffix is contiguous and ends at G, and rejects omitted
  middle/final events, duplicates, reordered events, a terminal stale prefix,
  and substituted older-fence events. Full nonzero-baseline transcripts pass;
  legal unstarted-reservation ordinal gaps and interleaved run/group starts do
  not require contiguous ordinals or run/group counters.
- Equal-millisecond document-to-redirect-to-original-target chains and trailing
  robots/render-resource starts require the complete `B+1..G` interval. Failed,
  cancelled, and unused STARTs cannot be omitted. A failed required document or
  redirect cannot publish an earlier response; a separately permitted nonfatal
  resource failure is counted without becoming an alias. Unknown success or
  missing authenticated event evidence suppresses output.
- Output preparation, BEGIN, every chunk family, and independent pre-seal
  verification preserve the same opaque full-lease/B/G `OutputContext` binding.
  Relabelling stale context/output with a newer scalar pair is rejected. The
  verifier compares actual staged aliases, rather than substituting expected
  context aliases while ignoring changed stored data.
- Every stage-data write, active-stage replay, seal/replayed seal, and first
  commit rejects an established current stage whose frozen B/G differs from the
  job with `COUNTER_CORRUPT`, without data, seal, backpressure, or publication
  mutation. Current active/last-stage and fence equality is also mandatory.
  First BEGIN with a valid stale input pair is instead `STAGE_INVALID` without
  freezing; invalid canonical input relations are `INVALID_ARGUMENT`, and
  invalid numeric encoding is `INVALID_NUMBER`.
- Page, outlinks, all payloads, manifest, backlinks, discoveries, notification,
  and job completion become visible in the same Redis transaction.
- URL-ID/canonical-witness mismatches in source jobs, request reservations,
  discoveries, and visited aliases fail before mutation; every visited-depth
  entry has the matching canonical-URL witness.
- Empty outlinks are represented by absence and zero images by an explicit
  manifest.
- Wrong destination types, existing immutable destinations, invalid manifests,
  insufficient memory headroom, and full `pages_queue` expose no output and do
  not change job disposition, counters, queues, or destinations; only the exact
  first-block backpressure record and secondary membership permitted by section
  10.5 may change.
- The exact ten-field page, five-field manifest/payload, base64url key grammar,
  compact JSON, publication IDs, final-document timestamp/fence/target witness,
  queue values, and backlink members pass byte-for-byte golden tests in the
  target Indexer, Image Indexer, and Backlinks Processor images.
- Source, processing, and dead-list tests accept only canonical `page_data:...`
  items for all three page lists and canonical `page_images:...` items for all
  three image lists; cross-family values and envelopes are rejected without
  moving an item.
- `CJ2_COMMIT` uses no more than 73 staged keys and a 64 KiB request; its static
  proof finds no parsing, allocation, reply-dependent branch, or fallible
  unproved call after the first write.
- Every operation at its maximum approved shape, including commit, stage,
  enqueue, claim/request, cancellation, and maintenance scripts, measures
  strictly below 100 ms p99 on the target configuration; command sizes remain
  within section 3 limits.

### 17.5 Redis durability and restart

Using a disposable Redis configured exactly as section 2 requires, create
ready, leased, delayed, completed, dead, cancelled, pending reservation,
started reservation, and next-allowed states. SIGKILL Redis after acknowledged
writes, restart the same AOF volume, and prove zero acknowledged state loss.

- A new Redis run ID blocks every client before mutation.
- A persisted `boot_state=approved` with a different actual Redis run ID is
  treated as effectively unapproved and only the reviewed approval path can
  replace it.
- Planned restart persists and requires its nonce plus exact process-stop
  evidence digest and explicit approval; the unchanged Redis process or a
  changed digest cannot consume the nonce.
- Unclean restart cannot be approved without current rehearsal evidence.
- Corrupt/truncated AOF or failed AOF status keeps startup blocked.
- Restore rehearsal records exact Redis version, configuration, timestamps,
  checksums, and observed loss bound.
- Four maximum logical stages plus the maximum retained/downstream fixtures and
  a maximum commit plus the 16 MiB lease-safety reserve remain within every
  reservation inequality; measured growth never exceeds `G` and OOM injection
  occurs only before visible mutation.
- Maximum amended fixtures include the retained job baseline field, its claim
  replacement allocation, both stage interval fields in `G_begin`, new discovery
  job baseline initialization in commit growth, and the added BEGIN RESP bytes.
  No field/key allocation is omitted as bookkeeping; metadata exclusion from
  logical `data_bytes` gives no memory-admission credit. The amended shapes fit
  the unchanged key, memory/control-floor, command-size, and script-time limits.
- The retained-state fixture includes 100 reservation records for each of 100
  unarchived runs, proves at most 10,000 logical terminal tombstones, and
  includes allocator/lazy-expiration memory in the measured bound.
- Maximum data/seal writes preserve the 64 KiB stage-control portion; one
  backpressure record still leaves the 32 KiB terminal floor sufficient for
  tested abort plus lease-ending transition, or one recovery. Abort retains
  the decremented slot until that outcome, so a racing allocator cannot consume
  the terminal floor; worker death lets recovery remove it exactly once, and
  subsequent stage cleanup is deletion-only.
- With routine allocation forced against the safety floor, every maximum live
  reservation can finish/cancel and every non-stage lease can release,
  retry/dead/cancel/complete or recover; ordinary allocation and commit remain
  blocked until the full floor is restored.

### 17.6 Migration, cutover, and operations

- Hash-only V1 identities are reported and never migrated.
- Invalid or changing V1 pending state prevents sealing.
- Migration copy/re-read/audit never changes V1 keys or activates V2; only the
  separately confirmed retirement script changes the five legacy keys.
- Candidate markers authorize only the finite stopped-world migration set;
  claims, request starts, stages, outputs, and consumers remain blocked.
- Empty, malformed, reordered, or mismatched binary gate records fail before
  mutation in every boot-only, candidate, and active operation.
- Retirement validates and reports the exact five-bit literal-key bitmap, all
  five key-content digests, and both historical literal-key type/count values;
  changed evidence rejects the whole deletion, with no wildcard or alias use.
- Any namespaced V1 crawl key, literal `spider_queue`, or `signal_queue` blocks
  marker promotion and V2 activation. Runtime startup instead verifies the exact
  promoted retirement evidence and has no code path or ACL access to those names.
- Old credentials are revoked and fail authentication; old binaries and wrong
  compatibility/image digests fail the release preflight.
- Repeated idle periods and Spider/consumer restarts perform no unnecessary
  write and never create `signal_queue`; static and integration tests find no
  runtime signal command and allow only the stopped admin's exact retirement
  evidence/deletion plus promotion/activation `TYPE none` checks.
- Pre-boundary rollback and post-boundary coordinated restore are rehearsed.
- A crash after any nonfinal purge batch leaves explicit immutable purge
  progress that monitoring and other runtime readers recognize, and resumes from
  the fenced first job without changing frozen archive counters; a lost final response reconciles
  only from complete run-key absence plus the same evidence digest and returns
  exactly `[PURGED, now_ms, 0, 0]`.
- The rollback boundary test asserts that only a successful
  `CJ2_START_REQUEST`, before DNS, creates the immutable global evidence; claim,
  reserve, stage, publication absence, or service startup does not move it.
- Monitoring reports every required count, age, budget, recovery, durability,
  and rate field without raw URLs.
- All Spider crash/concurrency tests pass under `go test -race` and protected CI
  refuses skipped real-Redis tests.

### 17.7 Non-authoritative fixture bootstrap

The section 5.1 exception has its own mandatory acceptance suite:

1. **Positive setup:** start a newly created empty-volume Redis in a namespace
   with asserted zero ingress, egress, DNS, proxy, and production-network routes;
   prove no production data, secret, credential, runtime/crawl-admin image, or
   consumer is mounted/running. Generate process-local fixture credentials,
   perform the bounded acknowledged-write/`SIGKILL`/same-volume preliminary
   persistence probe, require its nonzero current evidence digest, and approve
   the restarted boot through `CJ2_APPROVE_BOOT`. Build the exact provisional
   guard core and compatibility bytes twice, and require byte-identical output
   and hashes. Every unavailable named evidence field is exactly `ZERO_SHA256`,
   no other field is zero-sentinel, and recomputation yields the installed guard
   and manifest digests.
2. **Exact install/use:** directly install only the setup-manifest-enumerated
   active control and bounded fixture keys, verify their exact types, field
   counts, values, and aggregate digest, then revoke the setup credential.
   Unchanged authoritative Lua sources accept byte-identical supplied/stored
   provisional records and reject one-bit changes, omitted/extra fields,
   reordered binary records, a wrong-length or nonzero placeholder, and a fixture
   key not listed in the setup manifest.
3. **Isolation negatives:** setup refuses a nonempty, reused, shared, or
   retention-designated volume, a routable interface, available external
   DNS/HTTP, any production credential or data, or any runtime/crawl-admin
   process. The fixture credential cannot access
   candidate keys or invoke `CJ2_INSTALL_CANDIDATE_MARKERS`,
   `CJ2_RETIRE_LEGACY_KEYS`, or `CJ2_PROMOTE_CANDIDATE_CONTRACTS`. Synthetic run
   activation and `CJ2_START_REQUEST` are
   permitted only as a Lua state test; socket/DNS instrumentation proves that no
   external request capability or request occurs. The builder also refuses an
   all-nonzero guard, so it cannot directly install final production marker
   values under the provisional exception.
4. **Artifact rejection:** offline startup-validation tests for every runtime and
   crawl-admin image reject a zero evidence sentinel, provisional guard/manifest
   digest, fixture setup manifest, or fixture authorization artifact before any
   Redis connection or mutation. Attempts
   to submit provisional bytes to `CJ2_INSTALL_CANDIDATE_MARKERS` or treat them
   as authorization fail; no provisional artifact has a promotion path.
5. **Export:** export and seal evidence binding the contract, Lua source,
   Redis/config, setup manifest, provisional hashes, test results, and produced
   nonzero evidence digests. No final marker is installed while the fixture
   keyspace or credentials still exist.
6. **Teardown:** immediately after export, revoke/delete every fixture
   credential and prove each authenticated reconnect fails while the local Redis
   process remains reachable; then stop Redis, destroy its container and volume,
   and prove the volume no longer exists. A test that skips or cannot prove any
   teardown step invalidates its evidence and cannot produce a final release
   artifact.
7. **Final assembly:** only after successful teardown, replace every sentinel,
   recompute the final guard and compatibility artifacts twice, prove all
   evidence digests are nonzero and only the documented evidence-derived
   fields/hashes changed, and prove real candidate installation accepts only
   those independently reviewed final bytes.

## 18. Repository integration points

The implementation replaces the V1 lifecycle currently centered in:

- `services/spider/internal/database/redis_client.go`
- `services/spider/internal/crawler/scheduler.go`
- `services/spider/internal/crawler/crawl.go`
- `services/spider/internal/crawler/request_gate.go`
- `services/spider/internal/crawlpolicy/runtime.go`
- `services/spider/internal/controllers/`
- `services/spider/cmd/spider/main.go`
- `services/crawl-admin/` (new stopped boot/release/migration/retention tool)
- a new non-shipped acceptance-fixture harness implementing only section 5.1
- `services/seed-importer/feed.py`
- `services/indexer/data/redis_client.py`
- `services/indexer/main.py`
- `services/image-indexer/data/redis_client.py`
- `services/image-indexer/main.py`
- `services/backlinks-processor/data/redis_client.py`
- `services/backlinks-processor/main.py`
- `services/monitoring/src/main.rs`

The implementation MUST retain the current canonical URL, secure-fetch,
robots, render-policy, immutable page/image validation, and downstream
consumer checks except where this contract explicitly replaces their
process-local lifecycle or signaling behavior.

No crawl is authorized merely because this contract or its implementation
exists. The run's explicit unexpired authorization, pinned policy artifacts,
durability evidence, compatible immutable images, and every active operational
gate remain mandatory.
