# Single shared-group cancellation execution — 2026-10-06

**PASS; independent correctness and security reviews accept this case's scoped
evidence.** `ledger-shared-group-cancellation-v1` ran once on published `22317dc`.
All nine calls, 60 counter fields and 46 authority denials pass. Six-role revocation,
the original journal and absence of four containers/two volumes are verified.
The renewed approval is **consumed and non-reusable**. Review finalized October 7
after a connection interruption; no case rerun occurred. Full M4 remains open.

## Exact authority and execution

| Binding | Identity |
|---|---|
| Fixture | `cda9d3372d72a70b1bf3349b6ee2462f` |
| Executed commit | `22317dc21017f6157e5338a23c685a908be0bce4` |
| Published/tested tree | `914a46ab10c8200adec1ce9a70b4b1878569021d` |
| Source inventory, 108 files | `2221812ca56635055b8acb6271b2fd0fbf27f8d603703171c54fc58bb14062d4` |
| Renewed artifact request | `69e70525ccb341ab106296ebcb807e7a301b931e6781189f0e49613927e4b7fa` |
| Renewed approval | `ea6fb96a007bfe6f9564303370787f95da6c50458906b53e6b9aec734df5cc13` |
| Separate execution decision | `cceea29a0aa9c3fd61ce0a7c43801a238991624a89168b3566d831056eed61c0` |
| One-use reservation | `8069b24fe9822846bdd7d0c12a3766765daa821ec8b4668f3fd7c10b9a0b15c8` |
| Invocation record | `612a131756e7558830930d65ab13bd601be77d8f551ad72a9c674d07609d816a` |
| Original report | `ee0019889e946dcffa55ec93940d4773ea4a3033ddcf0fdd880d3ef27b282be4` |
| Scoped evidence reconciliation | `55278ceec762d04f0a25d95ef3477eb81e54a8d468146bdbac1e1d75c9710cb9` |
| Consumed disposition | `edfc4508721f46ac56efff2920725bf67dca6bb8f79b75d1835497eb47ad74d7` |

The original approval `8a1714be…` could no longer accommodate its full 300-second
budget when execution was requested. It was refused **before reservation or
controller invocation**, without reducing the budget or extending its expiry.
The captured refusal check also occurred after its absolute expiry. Its exact
`insufficient_window_unused` closed disposition remains preserved and non-reusable.

The owner explicitly approved a fresh 16-artifact packet, then separately selected
**Execute approved renewed case**. Source, case, images, plan, recipe, operator
and bounds were unchanged; only the approved time window and evidence destination
were renewed. Approval expiry was **2026-10-06 21:22:37.244 UTC**. The single
invocation ran **20:39:45.648–20:39:59.777 UTC**, **14,130 ms**, exit 0. The integer
wall-clock bracket is 14,129 ms; the monotonic elapsed value is rounded separately.

Operator: `kfuller`, local Linux/arm64 Docker. Bounds: 300-second case, 30-second
stages/full measurement and separate 60-second cleanup; four networkless containers,
two volumes and six credential roles. Historical approvals were not reused and
there was no automatic retry.

## Source, images and CI

The [publication/CI gate](crawl-jobs-v2-m4-cancellation-ci-2026-10-06.md) binds draft
[PR #20](https://github.com/fullerkris/mifolyo/pull/20) and all fourteen passing
protected contexts. Downloaded evidence verifies 480 compiled race roots,
479 passes/one allowed optional native-parity skip, all eleven M4 roots, 183
final-source harness tests and 21 scripts. PHP warning classifications remain
explicit in that report. CI is not an additional real cancellation run.

| Selected runtime artifact | Identity |
|---|---|
| Harness / stand-in | `sha256:e57da19e8b99ffbd630545730b3883d00f6088a5f56068ca0b5aed2bba32d0ee` |
| Redis 7.4.11 | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Plan | `da7ab623673d9b221a8e47a9f4c77fcf983af3636badb90016a03af37542d7c3` |
| Recipe | `b36d4bb7d2a4e657afbcda6a9cc3160b68678b19d590f7fb8c75b1f4de6d7a4f` |

The [selected arm64 preparation](crawl-jobs-v2-m4-shared-group-cancellation-preparation-2026-10-05.md)
and fresh execution checks bind these images to the reviewed bytes. The CI amd64
preparer selects claim/release while verifying all 74 image files/20 recipes;
it is not cancellation-specific amd64 execution evidence.

## Measured cancellation result

| Calls | Verified behavior |
|---|---|
| SGCANCEL01–02 | A CLAIM; B blocked by A's pending shared-group reservation |
| SGCANCEL03–04 | A CANCEL releases pending capacity; immediate replay changes nothing |
| SGCANCEL05–06 | B claims the freed slot; historical A CANCEL preserves B's reservation |
| SGCANCEL07–08 | B START establishes first-start history; FINISH releases capacity without refunding history |
| SGCANCEL09 | Read-only maintenance processes four scopes, `more=0` |

There are **five mutations, one capacity denial, two replays, one maintenance and
zero expected errors**. Initial/replayed cancellation statuses are all
`RESERVATION_CANCELLED`; idempotency is established from unchanged state/counter/
expiry projections, not status alone. Response hashes need not match across
replays because their response time changes.

Independent literal formulas check **1,680 counter integers**: 60 initial plus
60 before/after/delta fields for each of nine calls. A retains zero starts and
deliveries; B records one start/delivery. Both runs retain one claim and one
reservation creation; creation is not refunded. Both final jobs remain leased.
All 46 authority probes return `NOPERM` and retain the final state fingerprint.

Two distinct runs/jobs/origins share one group; concurrency is global/group/origin
**2/1/1**, with zero intervals. The sole block is exactly the shared group with
active count/effective concurrency 1 and `after_io=0` for blocked B. Replay after
B's claim does not refund B's slot or clear its active reservation.

## Redis-time / expiry projections

| Event | A | B |
|---|---:|---:|
| CLAIM | `1791319194631` | `1791319195133` |
| CANCEL / FINISH | `1791319194812` | `1791319195435` |
| START | Not started | `1791319195314` |
| Lease expiry | `1791319254631` | `1791319255133` |
| Terminal physical expiry | `1791405594812` | `1791405595435` |

Leases remain claim + 60,000 ms. Terminal physical expiries are cancellation /
FINISH + 86,400,000 ms and are not extended by replays. B owns the first-start
record; document history remains empty for these synthetic robots intents.

Measurement spans **3,323 ms**, from `1791319194467` to `1791319197790`, within
the 30,000-ms bound. The nine-call brackets span 1,023 ms. These are bounded case
observations, not maximum-shape latency or p99 acceptance.

## Teardown, original evidence and independent acceptance

- Six-role live retirement/reconnect and held-session receipts pass. Setup,
  loader and BOOT retire early; the measurement worker is quiesced before the
  fresh revocation helper, with revoker-last ordering bound to source.
- The original **28-action / 2,904-byte** canonical journal matches the report,
  hash and original device/inode identity. Seven stage-completion actions have
  five retained envelopes; both `ready` completions are journal-only.
- All four exact container names and two volume names are independently absent;
  six fixture/case/image-check label queries are empty. Absence was rechecked on
  October 7 after the connection interruption. No unrecorded by-ID checks are claimed.
- Gitleaks scans the byte-identical original report, intent, journal and two
  controller logs: **five files / 97,810 bytes / zero findings**, no suppression.
- Security accepts scoped evidence in sealed review
  `5bcaaa7ab0d44a1dddecc1d5a89450bad907c67a99ca89dc03ab0132d6e1b5bd`.
  Correctness continuation independently rechecks the interrupted predecessor's
  work and accepts in
  `d7b9778744a9e6ef22f91a3b51b9e70064d6dac2516772c907be97d3ba8b4fef`.
  All original evidence/source bytes and predecessor review files remain preserved.

Only the blocked and maintenance normalized responses are independently
reconstructed from public data. Seven private-ID-bearing replies, START permission
bits, raw whole-state/private-membership equality and private fixture hashes
remain pinned-runtime attestations. No discarded credential material was invented.
The START grant is synthetic and unused: no DNS, fetch, render or publication is
demonstrated. The pre-BOOT probe is not all-state request AOF crash durability;
absolute expiry is not observed one-day aging; serial logical owners are not
simultaneous workers or per-owner Redis credentials.

The [exact evidence index](evidence/m4-cancellation-run-2026-10-06/README.md) retains
the run, both authority paths and final consumed disposition. Runtime acceptance
remains **case-scoped**, with `m4_accepted=false`. The next publication/merge and
broader M4/C0 work retain their separate gates. These result/status records are
local follow-up changes; no new commit, push, merge or C0 tracing was performed.
