# Single shared-group-capacity execution — 2026-10-02

**PASS; independent correctness and defensive-security reviews accept this
case's scoped evidence.** `ledger-shared-group-capacity-v1` ran once on published
`c381287`. All 21 calls, three group-capacity denials, 60 counter fields and 46
authority denials pass. Six roles were revoked, the original journal reconciled,
and four containers/two volumes independently confirmed absent. The renewed
approval is **consumed and non-reusable**. Full M4 remains open.

## Exact bindings and authority

| Item | Identity |
|---|---|
| Fixture | `e47d57da09b95aec8dd2735186574aad` |
| Executed published commit | `c381287384d41f243516a851dd6ded6f56b9fcad` |
| Tree | `7de6991b3987b612d0916e3699fc2bdc66e29d3e` |
| Base | `7da55b14b32c79ec4b2b95b0a9fd31dda40f0571` |
| Source inventory, 107 files | `373f143179c558781922dedd2a90e9f65b461532610ca04e659fdcbe3fdea9d4` |
| Plan | `526593999b3eefe48bf0492cb5221603d5475ab1686460944253c44668b0f4eb` |
| Recipe | `05bfdbbea0bf5166b332e8e68225d466dcd1b076bf171fca30d062b946d88dc4` |
| Harness / stand-in | `sha256:03bf14679ffdda67c7940ffca0cfd52d38fcf5b9d8e0ca4f173b3fbe2e76d84d` |
| Redis 7.4.11 | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| Renewed artifact request | `b4e6deb16ea7c7f3ea7fc4d6ffb4c9f82ba7aefa09a1f853fcc0dcaefbd2b959` |
| One-use approval | `1e7732b25782b3cb1a882720a42db956f585f112f3ad5b388b7612dfd8e0398e` |
| Separate execution decision | `8c8c526dbb88d312914a84908a33b5731b3f5416a015709f7419c6ae600c0e53` |
| Protected PR CI verification | `098d5e7f7a918ae6bf23fcc2b5bed35e0e67f82f672f00743194d16bd4b614b9` |

The owner-approved 49-file foundation/lifecycle/preparation checkpoint was
published on [draft PR #18](https://github.com/fullerkris/mifolyo/pull/18).
All fourteen protected checks passed. Downloaded eight-shard reports account for
480 compiled roots: 479 passes and only the allowed optional
`TestJobLuaNativeFactoryParity` skip. All seven M4 roots pass once. Python logs
confirm 171 harness/21 script tests, with zero skips.

CI PHP results retain their warning classifications: forum 129 tests/547 assertions
comprise 85 warning results and 44 warning-free passes; query 31 tests/193 assertions
comprise 30 warning results and one warning-free pass. Neither suite failed or
skipped tests. Raw command blocks are bound to successful job-step metadata/times.
The tested merge `c1ba97ad71cfb4432aaf2d772a4ea37ec18e94a6` has the same tree as
the published revision, with parents `7da55b1` and `c381287`. CI's amd64 preparer
explicitly selects claim/release while checking all 74 files/19 recipes. The
separately selected shared-group arm64 preparation supplies this real case.

The original artifact approval `db4f784f…` expired at **20:14:40.961 UTC** before
the execution request was checked. Preflight refused it before reservation or
controller invocation; its unchanged bytes and **expired-unused** disposition are
retained. The owner explicitly approved a new 16-artifact packet with unchanged
case/source/images/plan/recipe/bounds and a fresh destination, then separately
selected **Execute approved case**.

Renewed approval window: **20:40:53.472–21:40:53.472 UTC**. Operator: `kfuller`
via OpenCode, local Linux/arm64 Docker. Fresh source, CI, image, resource and
validity checks preceded exclusive reservation. The one invocation ran
**20:47:32.508–20:47:47.694 UTC**, **15,186 ms**, exit 0, with no automatic retry.

Bounds remain a 300-second case, 30-second stages/full observed measurement and
separate 60-second cleanup; four networkless disposable containers, two volumes,
six credential roles and private Unix transport. Memory caps are init 128 MiB,
executor/revocation 256 MiB, Redis 528 MiB with 400 MiB maxmemory. These caps and
earlier image-check peaks are not maximum-shape runtime-memory measurements.

## Measured result

Two distinct runs/jobs/logical owners have different origins and share one
immutable group lineage. Global/group/origin concurrency caps are **2/1/1**, with
all intervals zero. Each run receives one unrenewed 60-second lease.

| Calls | Verified scope |
|---|---|
| SGC01–04 | A claim/replay; B blocked twice while A's reservation is pending |
| SGC05–07 | A CANCEL with B owner rejects; A START/replay |
| SGC08–10 | B blocked while A is started; started CANCEL and wrong-token FINISH reject |
| SGC11–15 | A FINISH; B claim/replay; historical A FINISH/START preserve B's held slot |
| SGC16–20 | B START/replay, FINISH/replay and historical START |
| SGC21 | Read-only maintenance processes all four scopes, with no remainder |

All **21 calls** match: six mutations, three capacity denials, three expected
errors, eight exact replays and one maintenance pass. Error reply timestamps are
null; their before/after Redis observations are separately retained.

Every capacity denial returns the exact normalized bulk-string tuple
`[CAPACITY_BLOCKED, now_ms, shared_group_scope_id, 1, 1, 0]` and preserves the complete
prior state fingerprint. Global occupancy remains below 2 and B's distinct origin
remains absent. `after_io=0` is relative to blocked B's fence, including after A
START; it does not deny that A has a recorded synthetic start.

Independent formulas verify all **60 initial counter values and 3,780
before/after/delta values**, 3,840 integers total. Each actor has 24 run/job/per-run-
group fields; four scopes supply 12 more. Final counters show one claim/fence,
one reservation creation, next ordinal 2, one cumulative start and one delivery
attempt per actor. Reservation pending/started occupancy is zero, while both jobs
remain leased. FINISH releases reservation capacity without ending leases or
refunding cumulative starts/creations. Replays cannot refund the peer's slot.

Six mutations change state fingerprints and fifteen nonmutations preserve them.
First-start history remains A's original start; robots-only traces keep document
history empty. All 46 direct authority negatives return `NOPERM` and preserve the
final full-state fingerprint.

## Redis-time boundaries and exact expiry

The following are Redis milliseconds, distinct from host receipt timestamps:

| Event | Actor A | Actor B |
|---|---:|---:|
| Claim | `1790974061320` | `1790974062519` |
| START | `1790974061781` | `1790974062926` |
| FINISH | `1790974062327` | `1790974063187` |
| Lease expiry | `1790974121320` | `1790974122519` |
| Terminal physical expiry | `1791060462327` | `1791060463187` |

Lease deadlines are claim + 60,000 ms, unchanged by replays. Terminal key expiries
are FINISH + 86,400,000 ms, likewise unchanged. This is absolute-expiry evidence,
not observed one-day aging.

The complete measurement spans **4,508 ms**, from `1790974061161` to
`1790974065669`, within the 30,000-ms limit. Operation brackets span 2,273 ms.
At measurement completion A/B retain 55,651 / 56,850 ms on their original leases.
The separate 15,186-ms invocation duration is host/controller time. Neither is
maximum-shape latency or p99 evidence.

## Teardown and independent review

Seven stage-completion actions have **five retained runtime envelopes**; both
`ready` completions are journal-only. Isolation receipts admit two processes and
zero external routes: init is UID 0 with only CHOWN; later stages use UID 65534 with
zero effective capabilities. Nine declared inactive kernel fallback interfaces
are admitted by the reviewed predicate, not counted as active routes.

Setup/loader/BOOT retire early. The worker is stopped with PID 0 and removed before
the fresh revocation helper is created. Six-role revocation uses reachable-server
session/reconnect proof; final receipts distinguish already-revoked early roles.
Revoker-last ordering comes from the bound source, not sorted JSON map order.
Revocation precedes Redis/volume destruction.

The **28-action / 2,904-byte original journal** matches canonical report bytes,
sequence, hash and original device/inode hash. Eleven actions cover cleanup.
Independent read-only checks confirm all four exact container names and two
volume names absent; six fixture/case/image-check label listings are empty.
Container IDs were not exported, so no by-ID absence is claimed. Export copies
retain journal bytes, not original file identity.

Gitleaks 8.24.3 scanned byte-identical copies of report, intent, journal and both
controller logs: **five files, 192,925 bytes, zero findings**, with no added
suppressions. Both independent reviews return **ACCEPT**, with no blockers or
findings; the separate authority/source/CI/journal/live-resource audit passes.
The first local provenance-reader attempt rejected valid empty stderr as an
empty JSON artifact. Its checker was corrected to allow bounded empty log files;
only the read-only postcheck was repeated, never the case/controller.

| Artifact | SHA-256 |
|---|---|
| Report | `36f695ab28e2ec25a15aee93695218519882911df8d0109f4f255ba9cc96a641` |
| Original journal bytes | `76e922707e2b83694021351cac83f6f3da6b417829d0f1a0c8b3ece171474d61` |
| Provenance postcheck | `f9178c3dd10a6b2b3edff33328183f6fae28c382af5a7480d1ad6edaab81d2fc` |
| Correctness ACCEPT | `8b43cade94e68084761ca44b051c2e9c491b5520506e1ab177dfc901e19f1c42` |
| Defensive-security ACCEPT | `cd46b16740eb0940135c471f9aad62ac1fd31152c2c3a4747ea7363388e9f5fc` |
| Final postcheck | `155e516cad81b5df9fec63807cc5b5a20e5323cba2762b39608471e042cb8d16` |
| Consumed disposition | `cd0fab015183ea8f5fbb790441fcd3f865cb42a703c1e0cbe6c0fa492ce10828` |

## Evidence boundaries and next work

Seven normalized logical-response hashes are independently reconstructed: three
capacity denials, three errors and maintenance. Fourteen reservation-bearing
replies remain opaque. **START `io_permission` is not publicly exported**;
private reply/state/identity comparisons rely on the reviewed pinned producer.
Private credentials/owners/tokens, raw process inventories and Docker inspections
cannot be reconstructed from fingerprints.

Both logical actors share one ledger credential in a serial program. This does
not establish simultaneous physical-worker contention or per-owner credential
isolation. Positive cancellation and reversed-contender traces remain offline
controls. Synthetic grants were unused; no external HTTP/DNS or application permit
was exercised. The initial persistence probe precedes setup/START and does not
establish post-START/all-state AOF durability or internal Lua crash-boundary cuts.

Independent origin/global saturation, broader shared concurrency, policy tightening,
budgets, after-I/O recovery, remaining protocol/admin, crash/AOF/restore, maximum
shapes and latency acceptance remain open. The [implementation plan](crawl-jobs-v2-plan.md)
owns current status; application V2 stays dormant and full M4 is unaccepted.

The [byte-identical exports](evidence/m4-shared-group-run-2026-10-02/README.md) and
status updates were prepared after executed `c381287`; their later publication
does not rebind that execution. Private originals, approvals and review scripts
remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-shared-group-execution-renewal-2026-10-02/`.
The expired-unused approval remains under sibling `m4-shared-group-execution-2026-10-02/`;
original and renewed approval packets are retained separately.
