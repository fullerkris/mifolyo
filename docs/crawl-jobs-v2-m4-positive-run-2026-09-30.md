# Single positive-interval execution — 2026-09-30

**PASS; independent correctness and defensive-security reviews accept this
case's scoped evidence.** `ledger-positive-interval-v1` ran once on published
`7431599`. The exact group-first block, observer-only wait and due admission pass,
as do all 24 calls, 33 accounting fields and 46 authority denials. Six credentials
were revoked, the original journal reconciled, and four containers/two volumes
independently confirmed absent. The approval is **consumed and non-reusable**.
Full M4 remains open.

## Exact bindings and authority

| Item | Identity |
|---|---|
| Fixture | `99e1a00b4804b4e104e4d638cdb81b19` |
| Executed published commit | `743159909c89cc63b1a4b67cff4cca323cd85e26` |
| Tree | `ae159402a52eff94463d6f926ba7d5b9b158c8c9` |
| Source inventory, 101 files | `c4c97b7672e8cd813ca17e574d4ec5bfbb865c8752e77f6d5bcd593e378ed33b` |
| Plan | `ca4eafd255a559907c89a2ccc7b3e70d101370415577440c47702c08029cee1a` |
| Recipe | `7ddf11563a526574d12dcea0f97ba1c611f1cf49efbfa6774d9e6434f31381d8` |
| Harness / stand-in | `sha256:2d74e377e77eefddef7cb179d8f0f388650ae02f090f13ca5cf9ab91593f07fc` |
| Redis 7.4.11 | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Renewed artifact request | `fe46fded7eb85fa366723516eaa628a71ddc27bed4075a4471d3fb0eb4226647` |
| One-use approval | `9f71ed8e49f347859de3cbbf246dc1e5ad9e4d3b35343627194185112e5544b5` |
| Separate execution decision | `b2166ba20fd6cb5ea4420c126fbc5544259966a23dfdca73545c81bd77f372d1` |
| Protected PR CI verification | `b6bd0294da36a8612c24664e9d43a3d824cd361e68b1d5fb434fb406a647ff55` |

The owner-approved 47-file checkpoint was published on
[draft PR #17](https://github.com/fullerkris/mifolyo/pull/17). All fourteen protected
checks passed. Downloaded eight-shard evidence accounts for 478 compiled roots:
477 passed and only the explicitly optional `TestJobLuaNativeFactoryParity`
skipped. CI logs confirm 150 harness and 21 script tests with no Python skips.
The tested merge `a717023064d4a13db983d3bb5617d5ae1dc168ee` has the same tree as
the published revision. CI's amd64 preparer explicitly selects claim/release and
verifies 71 files/all 18 recipes; the real case uses the separately selected
positive-case arm64 preparation. The `UNKNOWN STEP` log-label issue was resolved
by binding the unique test command block to successful step metadata and times.

The first artifact approval, `abf36e51…`, expired at **15:52:18.805 UTC** before
the execution request was checked. Preflight refused it before reservation or any
controller invocation. Its original bytes and **expired-unused** disposition are
retained. The owner then approved a new 13-artifact packet with unchanged
source/images/plan/recipe/scope, fresh recording destination and explicit expiry
linkage, followed by the separate **Execute approved case** decision.

Renewed approval window: **17:12:43.157–18:12:43.157 UTC**. Operator: `kfuller`
via OpenCode, local Linux/arm64 Docker. Live source, CI, image, resource and expiry
checks preceded exclusive reservation. The one invocation ran
**17:27:10.093–17:27:31.829 UTC**, **21,736 ms**, exit 0. No automatic retry occurred.

Bounds remain a 300-second case, 30-second stages and separate 60-second cleanup;
four networkless containers, two disposable volumes, private Unix transport and
six credential roles. Memory limits remain init 128 MiB, executor/revocation
256 MiB, Redis 528 MiB with 400 MiB maxmemory. No runtime peak-memory measurement
is claimed from these caps or the earlier image-check peaks.

## Measured result

One fresh job and one unrenewed lease contain robots then document reservations.
Matching group/origin intervals are **8,000 ms**, global interval zero, concurrency
caps 2/1/1. Equal fresh deadlines make this a group-first blocking case.

| Calls | Verified scope |
|---|---|
| RATE01–05 | Claim/replay; pending FINISH, competing intent and wrong-token START reject |
| RATE06–11 | Robots START/replay, FINISH/replay, historical START and read-only integrity |
| RATE12 | Group-first `RATE_BLOCKED`, exact `robots START + 8000` deadline, `after_io=1`, complete prior state unchanged |
| Observer wait | Four same-run TIME observations, bound to the fixture, complete prefix, deadline and lease |
| RATE13–19 | Due RESERVE/replay, historical FINISH, document START and started/historical replays before the new deadline |
| RATE20–24 | Wrong-token FINISH rejects; document FINISH/replay, historical START and final integrity |

All **24 calls** match: six mutations, four expected errors, eleven exact
replays, one rate denial and two maintenance passes. The twelve-row prefix is
byte-identical within the full 24-row suffix result. Expected protocol replays
are distinct from the single controller invocation. Error reply times are null;
their before/after Redis observations remain separately recorded.

Independent formulas match **33 initial counter values and 2,376
before/after/delta values**. Final counters show one claim/fence, two reservation
creations, next ordinal 3, two cumulative starts, one delivery attempt and zero
active/pending/started reservation occupancy. Six mutations change full-state
fingerprints; all eighteen nonmutation rows preserve the preceding fingerprint.
FINISH/replays/errors/denial/maintenance neither refund starts or creations nor
shorten rate deadlines. First-start history remains robots; document history
appears only at its START and stays stable.

## Redis-time boundaries and exact expiry

The following are Redis milliseconds, not host receipt timestamps:

| Event | Timestamp | Relation |
|---|---:|---|
| Robots START | `1790789237831` | First group/origin deadline = START + 8000 |
| First deadline | `1790789245831` | Exact returned block deadline |
| RATE12 denial | `1790789238399` | 7,432 ms before deadline |
| Clock observation 1 | `1790789239240` | −6,591 ms |
| Clock observation 2 | `1790789242024` | −3,807 ms |
| Clock observation 3 | `1790789244817` | −1,014 ms |
| Clock observation 4 | `1790789246649` | +818 ms; verified crossing |
| RATE13 dispatch/admission | `1790789247624` | +1,793 ms; 975 ms after crossing observation |
| Document START | `1790789247876` | Second deadline = `1790789255876` |

The wait uses four of at most twenty observer-only observations. Source-derived
requested sleeps are 2,000 / 2,000 / 1,014 ms. Observed gaps of 2,784 / 2,793 /
1,832 ms include stage overhead and are **not measured sleep durations**.
The live case proves before/after ordering, not exact millisecond equality.

RATE17's started-reservation RESERVE replay, RATE18's document START replay and
RATE19's historical robots START have **7,893 / 7,793 / 7,682 ms** remaining before
the second deadline. RATE22/23 have 7,338 / 7,259 ms remaining. These replays do
not create another reservation/start or reset the deadlines.

The complete measurement spans **12,748 ms**, from `1790789237292` to
`1790789250040`, within 30,000 ms; operation brackets span 11,263 ms. The separate
21,736-ms invocation duration is host/controller time. Neither is p99 evidence.

The single lease runs from `1790789237441` to `1790789297441`, exactly 60,000 ms;
47,401 ms remains at measurement completion. Logical reservation expiries retain
that deadline. Physical terminal key expiries are:

- Robots: `1790875638050`, exactly FINISH + 86,400,000 ms.
- Document: `1790875648331`, exactly FINISH + 86,400,000 ms.

Replays retain those expiries. This is absolute-expiry evidence, not an elapsed
one-day test. All **46 direct authority-denial receipts** return `NOPERM` and
retain the final state fingerprint.

## Teardown and independent review

Twelve stage-completion actions have **ten distinct retained runtime envelopes**
and two journal-only `ready` completions. There are eleven stored envelope
occurrences because the last clock envelope is duplicated. Retained isolation
receipts show two processes and zero external routes; init is UID 0 with only
CHOWN, and subsequent stages are UID 65534 with zero effective capabilities.
Clock stages receive only the observer credential; source-bound grants are
read-only. The 46 direct negatives test ledger authority, not observer writes.

Setup/loader/BOOT retire early. The worker is stopped with PID 0 and removed
before the fresh revocation helper is created. All six final reconnects are
denied with Redis reachable. Revoker-last order comes from the bound source loop,
not sorted JSON map order. Revocation precedes Redis/volume destruction.

The **33-action, 3,348-byte original journal** matches canonical report bytes,
sequence, hash and original device/inode hash. Eleven actions cover cleanup.
Independent read-only checks confirm all four exact container names and two
volume names absent; six fixture/case/image-check label queries are empty.
Container IDs were not exported, so no by-ID absence is claimed. Exported journal
copies retain bytes, not the original file identity.

Gitleaks 8.24.3 scanned the three original evidence files plus execution decision
and invocation: **five files, 194,040 bytes, zero raw findings**. No findings were
dismissed or suppressed. Both independent reviews return **ACCEPT**, with no
actionable finding. The separate authority/source/CI audit also passes.

| Artifact | SHA-256 |
|---|---|
| Report | `56fe583637183546b37eb454078255ffa02ba2e56954f46e82176a8de5396653` |
| Provenance postcheck | `688a226b97e9d94959dabcc9ab095fad8e20481b12c273bdd8622236d90eba44` |
| Correctness ACCEPT | `9b3257940d3fb8b6d021e5c4ccbd2b79e833a81476e8decb6b3407250aed7e4e` |
| Defensive-security ACCEPT | `b762fe5934f2d452fb7ac4c452a4a6f32e584b0f081f93c755dd82a8ffaa5be9` |
| Final postcheck | `d90b55a5de859d6e53eec73a35e361673112a0c76d91790cbaebe98c4c1ad900` |
| Consumed disposition | `e42c5c3e7d4009bc7f0cc83d6c8b40b0e12a434eb914d86aee0d1a62650e697f` |

## Evidence boundaries and next work

Seven public response hashes can be reconstructed: four errors, two maintenance
replies and the rate denial. Seventeen reservation-bearing replies remain opaque.
**START `io_permission` is not publicly exported**; exact reply/permission and
private-state comparisons rely on the reviewed producer. The suffix-entry full
snapshot across the wait is likewise supported by its reviewed comparison and
successful receipt, not a new independent Redis read.

`after_io=1` means a recorded START on this fence. Synthetic grants were unused;
no HTTP/DNS request or application permit was exercised. Raw Docker/process
inventories, credentials and owner/reservation identities cannot be reconstructed
from hashes. These command-level observations are not internal Lua crash traces.
The preliminary persistence probe precedes request starts and does not establish
post-START crash durability.

Independent origin blocking, shared concurrency, policy tightening, budgets,
after-I/O recovery, broader protocol/admin, crash/AOF/restore, maximum-shape and
latency acceptance remain open. The [implementation plan](crawl-jobs-v2-plan.md)
owns current status. Application V2 remains dormant; full M4 is unaccepted.

The [byte-identical exports](evidence/m4-positive-run-2026-09-30/README.md) and
status updates are local and uncommitted after executed `7431599`. Private
originals, approvals, review scripts and checks remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-positive-execution-renewal-2026-09-30/`.
The expired-unused approval remains under the sibling
`m4-positive-execution-2026-09-30/`; both original and renewed approval packets
are retained separately.
