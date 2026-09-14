# Monitoring

> [!IMPORTANT]
> **Status — implemented V1 scaler, currently blocked.** This process reads V1
> backlog state and invokes service-scaling commands; it is not the read-only
> Crawl Jobs V2 monitor. Do not start it during F3 foundation or Lua work. F3
> Monitoring integration is a later milestone and has not started. The local F5
> cursor-scan repair remains subject to protected `required-tests` PR
> acceptance. See the
> [parent remediation plan](../../docs/spider-render-remediation-plan-2026-09-01.md)
> and [F3 implementation plan](../../docs/crawl-jobs-v2-plan.md).

The implemented V1 loop reads crawl, indexer, and backlink backlog state, then
invokes Compose scaling for Spider, Indexer, and Backlinks Processor instances.
It also consumes the V1 termination signal. This mutating scaler behavior is
incompatible with the required read-only V2 Monitoring contract and must not be
reused as V2 wiring.

The crawl backlog is read with `ZCARD` from `CRAWL_QUEUE_KEY`. The variable is
optional and defaults to the V1 queue, `mifolyo:crawl:v1:queue`, so monitoring
and the spider observe the same versioned backlog. Set it explicitly only when
all V1 queue producers and consumers are intentionally configured to use the
same alternate key. Never point this process at Crawl Jobs V2 keys.

### Bounded V1 backlink scan (F5)

Each monitoring tick issues at most one `SCAN MATCH backlinks:* COUNT 100` call
and accounts for at most 100 target keys. Replies of up to 1,024 keys are accepted,
matching the F5 processor's response bound. Only an integer pending-key count is
retained from each page; ticks that drain pending keys do not issue another
`SCAN`. The cursor and running count continue across ticks, including empty
nonterminal pages. A count is published only when both the cursor and pending-key
count are zero (including a genuinely empty pass); incomplete or failed scans
leave backlink scaling unchanged. Spider and Indexer scaling are unchanged.

Redis errors and replies containing more than 1,024 keys leave scan progress
unchanged for retry on the next tick; oversized replies report the stable reason
`backlinks_scan_response_too_large`. No backlink keys are modified or removed.
A persistently oversized page stalls completion rather than publishing a partial
count. Large or sparse keyspaces can also delay completion under these limits.

`SCAN` is not a snapshot and may return duplicates, so completed counts are
approximate. Counts reset after each full pass; no keys or deduplication set are
retained across ticks. State is constant-size, but Redis replies are decoded
before the size check: `COUNT` is only a hint, and the container memory limit
remains the final wire-response guard.
