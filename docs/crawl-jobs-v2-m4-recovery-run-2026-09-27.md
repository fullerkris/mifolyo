# Pre-I/O worker-death recovery — 2026-09-27

**PASS; independent correctness/security reviews accept this case's scoped
evidence.** One invocation of `ledger-worker-death-pre-io-v1` ran on the exact
approved Linux/arm64 artifacts. All six credentials were revoked, the original
retained journal reconciled, and five containers plus two volumes independently
confirmed absent. The one-use approval is **consumed and non-reusable**.

Full M4-P4/P5 acceptance remains open. The [implementation plan](crawl-jobs-v2-plan.md)
owns current status; this dated report records the completed observation.

## Artifact and authority bindings

| Item | Identity |
|---|---|
| Fixture | `d02e7fa6424d446089fd1333206bb294` |
| Executed commit | `c009282ba5473e89984e95466efec37bf7ab2a3a` |
| Published checkpoint | [Draft PR #15](https://github.com/fullerkris/mifolyo/pull/15), approved 41-file scope |
| Corrected 90-file review | `b5fe64eee5fded5bd3a946d7a58790617d67c95a0447138ba54a79c709aeb888` |
| Plan | `1e00625d41c9f4baa99936abd9a936b454b3e29f54c7ec181e95932d0041acd8` |
| Recipe | `ac0d4fb95391062e271c26d6c8bf3d70d3994470b3273fea6b78751bbe6da987` |
| Harness / stand-in | `sha256:b164fb8610949e2a31393b8897c2a4bfe620f636f24c636caa20b590acad8da2` |
| Redis 7.4.11 | `sha256:24e81cffaba832bcd71068a6ff772a531076bafdbb1d684195766ae9b6511f5c` |
| One-use approval | `3e6919830768e56029b730c437a398b061bff415567897404b38348a29d77f05` |
| Separate execution decision | `e8ee07f6bcdf6bf6413b17256d2173a22ab2163efc72693a184216c5d81d1b3f` |
| CI verification | `4d35850d1c1a460c82dbd43883ee5b883f93441137fb4bf77ee7f6eb36b471fe` |

All fourteen protected checks passed before execution. Downloaded eight-shard
race evidence matched all 476 compiled roots: 475 passes and only the explicitly
optional `TestJobLuaNativeFactoryParity` skipped. The tested merge and published
head share tree `70aeeb2c6db38fb467fa767546d8d8fb8f82a7da`. The amd64 CI preparation
selects PC01 while verifying all 66 image files/16 recipes; the independent arm64
preparation supplies the recovery-specific five-role admission evidence.

Operator `kfuller` via OpenCode approved the artifact set, then separately selected
**Execute approved case**. The private approval window was
**18:04:43.123–19:04:43.123 UTC**. Live source/CI/image/resource checks preceded the
exclusive reservation. The decision was recorded at 18:27:46.554 UTC; invocation
ran **18:27:46.662–18:29:06.119 UTC**, exit 0, **79,457 ms**. No automatic retry
occurred. The 300-second case, 30-second stage and separate 60-second cleanup
bounds, unchanged protocol constants and approved memory limits were retained.

## Worker death and real expiry

| Observation | Result |
|---|---:|
| Claimant PID inside worker A | 30 |
| Host complete-receipt to kill dispatch | 489 ms |
| Host complete-receipt to observed container stop | **654 ms**, within 1,000 ms |
| Attached-command reconciliation after observed stop | 0 ms at recorded millisecond resolution, within 5 seconds |
| Container / attached-command exit | 137 / 137 |
| Stopped PID / OOMKilled | 0 / false |
| Redis-TIME waiting observations | 20 |
| Claim A | `1790533677298` ms |
| Original lease expiry | `1790533737298` ms |
| Early recovery | `1790533679928` ms; 57,370 ms before expiry |
| Final waiting TIME | `1790533738401` ms; 1,103 ms after expiry |
| Due recovery | `1790533740008` ms; 2,710 ms after expiry |

The claimant's process-inventory fingerprint was independently reconstructed from
its PID and approved command. Worker A's admitted container ID matches the
non-OOM death proof; worker B has a distinct ID and starts after A's removal.
These are reviewed retained observations of the live run. The later reviewers
did not re-observe already-terminated processes. The timing origin is host receipt
of the complete frame, not Redis acknowledgment time.

## Thirteen case assertions

| Assertions | Observed scope |
|---|---|
| RCV01–02 | A claims fence 1; pre-expiry recovery processes zero; replacement-observed claim state and early-recovery state agree |
| RCV03–04 | Exactly one ready recovery; pending capacity refunded while history is retained; replay has no further effects |
| RCV05–06 | B obtains fence 2 and a distinct reservation; claim replay preserves state and deadlines |
| RCV07–09 | Stale A claim/release preserve B's state; stale renewal changes only the renewal-rejection counter in the public counter projection |
| RCV10 | B renewal extends the logical deadline with no accounting change |
| RCV11–13 | B release refunds pending capacity; release replay and drained recovery preserve final state and expiries |

Independent literal expectations checked all **38 counter values/deltas** per
snapshot. Final claims/reservation creations are 2/2; fence/next ordinal 2/3;
pre-I/O recoveries/recovered leases/ready recoveries 1/1/1; renewal rejections 1.
All pending/active reservation capacity is zero. Request starts, delivery attempts,
retries, output commits and terminal-job counters remain zero; one job remains open.

A's expired reservation physically expires at `1790620140008` ms, exactly
86,400,000 ms after recovery, while retaining its original logical deadline.
B's cancelled reservation physically expires at `1790620141094` ms, exactly
86,400,000 ms after release. Renewal moves B's logical deadline from
`1790533800361` to `1790533800936` ms. Replays do not refresh terminal expiries.
These are exact absolute-expiry observations, not a one-day elapsed-time test.

All **46 authority-denial receipts** match the independently enumerated
role/command inventory, report `NOPERM`, and retain the final-state fingerprint.

## Revocation, journal and cleanup

Independent security review checked 27 distinct retained stage envelopes,
including all twenty clock envelopes. Only init has UID 0/CHOWN; runtime envelopes
report UID 65534, zero effective capabilities, no external routes and two expected
processes. Two ready completions are journalled without retained envelopes; their
admission relies on the frozen execution path.

Setup/loader/BOOT were revoked early. The final fresh helper verified all six
ordinary roles while Redis was reachable; both workers were already quiesced.
Revoker-last follows the reviewed fixed iteration, not the sorted JSON map order.

The **55-action** external journal exactly matches the report, including sequence,
canonical bytes, hash and the original file-identity hash. Twelve actions span
cleanup from the worker-quiescence receipt. Fresh read-only Docker inspections
found every container absent by **both name and ID**, both volumes absent, and
fixture/case label listings empty. The evidence secret scan found zero matches.

## Independent reviews and retained evidence

| Artifact | SHA-256 |
|---|---|
| Report | `afe9bd92aea51e14a935c44f99c653f486a97889e3da3f30c20891ca89f51775` |
| Correctness ACCEPT | `b1d4dd3ac8d4603b41066fc9eb6976af8bff2eac869a4386b75848f839cbe25d` |
| Defensive-security ACCEPT | `774060690af38a2aae1373bdd28dd8dc42ceb2ae80e37bcf88452cfcd2061694` |
| Independent journal/resource postcheck | `cba49dea86c45ecddbae96dd3a24c65a24acbc451f3abc72e1adab9fa9b9bb4d` |
| Consumed disposition | `1b607793e3b13addfa99d6a45ac9190fba0138a8fd7e90fc5510e8414c19478e` |

The [evidence index](evidence/m4-recovery-run-2026-09-27/README.md) links exact
exports. Original 0600 artifacts, approval, reservation, review checkers and
private 0700 evidence directory remain under
`/private/var/folders/bg/k5cdrp4s64j32mt9h8b9t39h0000gn/T/opencode/m4-recovery-execution-2026-09-27/`.

## Scope and next work

Full private-state/reply comparisons ran inside the reviewed executor/controller;
hashes cannot reconstruct discarded owners, tokens, reservation IDs or full state.
The correctness review independently derives ten public reply fingerprints;
three reservation-bearing private replies remain opaque. Worker-death receipts
and controller journals are not internal Lua crash traces. Actual timings establish
before/after expiry, not exact live E−1/E/E+1 cuts or operation benchmarks.

This closes this pre-I/O synthetic-worker case only. Request-start/finish and rate
accounting, after-I/O recovery with nonzero histories, remaining lifecycle/admin,
crash/AOF/restore, maximum shapes and latency evidence remain. Application V2 stays
dormant; full M4 is unaccepted. These subsequent run exports/status updates are
local and uncommitted; the published execution checkpoint remains `c009282`.
