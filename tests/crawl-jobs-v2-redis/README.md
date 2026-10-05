# Crawl Jobs V2 M4 fixture harness

This non-shipped harness has an offline compiler (`harness.py`) and a bounded
closed-case execution controller (`controller.py`). The compiler starts nothing.
The controller can create disposable infrastructure only through its explicit
`run` command with a separately reviewed, digest-bound execution approval.
Python 3.10+ and its standard library are sufficient for local tests.
Implementation is locally tested; bounded real-Redis smoke, claim/release and all
13 bootstrap/ACL negative cases pass. Broader M4 acceptance remains pending.

**Bootstrap/ACL Step 2 (2026-09-23): locally complete.** The closed registry now
includes 13 additional negative cases. All 95 harness tests, the 82 planned
admission variants and independent Go/canonical-Lua race checks pass locally.
At Step 2, the new cases had only offline/simulated evidence. Their changed source
scope received separate correctness/security GO from Step 3, with no actionable
findings. Subsequent image/CI/execution results are recorded below. See the
[current package status](../../docs/crawl-jobs-v2-plan.md#step-2-local-implementation-result-2026-09-23).

**Step 3 independent review: GO for image/CI preparation.** Both reviewers
verified all 81 frozen file hashes and 15 recipes unchanged. Their independent
checks, the correctness review's explicitly incomplete broad test invocation,
and scope limits are in the
[dated review](../../docs/crawl-jobs-v2-m4-bootstrap-acl-review-2026-09-23.md).

**Step 4 local image gate: PASS.** The new arm64 harness validates all 62 files
and 15 recipes, stopped-role admission, memory and isolation. PC01 selects the
refreshed `ledger-claim-release-v1` plan; exact cleanup was independently checked.
See the [preparation report](../../docs/crawl-jobs-v2-m4-bootstrap-acl-image-preparation-2026-09-23.md).
PR #11 merged as `9b6b8f9`; the bootstrap/ACL checkpoint is now published as
`6340401` on draft PR #12 with all fourteen required checks passing. Downloaded
amd64 preparation and the complete 475-root race inventory were independently
verified. See the [post-CI record](../../docs/crawl-jobs-v2-m4-bootstrap-acl-ci-2026-09-23.md).
That Step 4 handoff preceded the separate PC01 approval and execution below.

**Step 5 refreshed PC01 execution: PASS.** The owner separately approved and
requested one attempt on `6340401`. Fixture `0a1a9641a6044e4dfde80a5c3d381216`
passed all nine claim/release calls, 46 ACL denials, complete state/expiry checks
and the actual holder-plus-exec process predicates in all five stages. All six
credentials were revoked; four containers and two volumes were independently
confirmed absent. Approval is consumed. See the
[PC01 result](../../docs/crawl-jobs-v2-m4-pc01-run-2026-09-23.md).

**Step 6 PC01 evidence review (2026-09-24): accepted as scoped control.**
All 12 case assertions and 17 partial requirement mappings are supported by the
retained evidence; the six exact resources were rechecked absent. At that review,
all 13 negative cases were unrun, and no full requirement/operation variant or M4-P3 gate was
closed. PR #12 merged after PC01 as tree-identical `320bce3`. See the
[evidence review and P01 handoff](../../docs/crawl-jobs-v2-m4-pc01-evidence-review-2026-09-24.md).

**P01 separately authorized execution (2026-09-24): PASS.** Exact selected-case
preparation and 14 protected checks passed on published `963b67b`; all 81 reviewed
sources remain unchanged. Fixture `f3c66e07c31db6d3a141c083ba70c56d` returned two
`CRAWL_V2_INVALID_STATE` rejections, with complete unchanged state, zero delta in
all 33 counters and 46 authority `NOPERM` results. Six-role revocation and
independent absence of all four containers/two volumes passed. Approval is
consumed. See the [preparation](../../docs/crawl-jobs-v2-m4-p01-preparation-2026-09-24.md)
and [run result](../../docs/crawl-jobs-v2-m4-p01-run-2026-09-24.md).
P01's subsequent scoped evidence review was accepted.

**Remaining twelve cases (2026-09-24): all PASS with scoped reviews accepted.**
The owner approved all exact case artifacts, then separately requested sequential
execution. Each case ran once on unchanged `963b67b`, with predecessor review and
cleanup required before continuing. Results include 62 exact negative calls,
seven measured positive controls, three administrative prefix operations and
368 direct ACL denials. All 77 per-case roles were revoked; all 48 containers and
24 volumes were independently found absent. The twelve approvals are consumed.
See the [results/coverage report](../../docs/crawl-jobs-v2-m4-negative-remainder-2026-09-24.md)
and [exact artifacts](../../docs/evidence/m4-negative-remainder-2026-09-24/README.md).
All 14 package Redis cases (PC01 plus 13 negatives) now have accepted scoped
PASS evidence. Zero negatives remain unrun. The subsequent reconciliation is below.

**M4-P3 readiness (2026-09-25): accepted within checkpoint scope.** Independent
correctness/security reviews accepted the fourteen-case matrix plus exact 82 H/I
variants, seven host supplements and nine target-image predicate controls. Their
offline/simulated/captured-metadata classifications remain explicit; no new Redis
server was started for that reconciliation. See the
[readiness assessment](../../docs/crawl-jobs-v2-m4-readiness-2026-09-25.md).
Full M4-P4/P5 acceptance remains open.

**Historical recovery foundation (2026-09-25): offline only.** `recovery_oracle.py` and
`test_recovery_oracle.py` project thirteen pre-I/O recovery/fencing steps over the
existing claim fixture. Six Python tests and 26 independent Go/canonical-Lua
invocations pass, including the race check; corrected code has scoped GO. The
prospective case was not registered and its process lifecycle was not implemented
at that checkpoint; the executable image then had 62 inputs/15 recipes.
The [full M4 inventory](planning/full-m4-acceptance-v1.json) is planning-only.

**Recovery lifecycle (2026-09-27): single real run PASS, scoped evidence accepted.**
`ledger-worker-death-pre-io-v1` is the sixteenth closed case.
It uses an acknowledged parked claimant, actual container termination proof,
a distinct replacement, bounded Redis-TIME waiting and thirteen operation
assertions with 38 counters/46 ACL denials. The image scope is now 66 files.
The corrected 90-file review closes command-liveness, stderr, isolation-receipt
and journal-integrity findings. All 128 harness/21 script tests, 52 canonical
invocations under Go race, vet and source pins pass. New arm64 preparation and
all fourteen protected checks pass on published `c009282` (draft PR #15).
After separate artifact approval and execution decision, fixture
`d02e7fa6424d446089fd1333206bb294` passed in 79,457 ms. The claimant was observed
stopped 654 ms after host receipt; twenty Redis-TIME observations crossed its
60-second lease. Independent correctness/security reviews accepted the scoped
evidence. All six roles were revoked, the 55-action journal reconciled by bytes
and file identity, and all seven owned resources independently found absent.
The approval is consumed. See [run](../../docs/crawl-jobs-v2-m4-recovery-run-2026-09-27.md),
[review](../../docs/crawl-jobs-v2-m4-recovery-review-2026-09-27.md) and
[preparation](../../docs/crawl-jobs-v2-m4-recovery-preparation-2026-09-27.md).
Local fakes remain simulations; full M4-P4/P5 acceptance remains open.

**Request lifecycle (2026-09-28): single real run PASS; scoped evidence accepted.**
`ledger-request-lifecycle-v1` is the seventeenth closed case.
One lease contains robots then document reservations: six mutations, four expected
RESP errors, ten replays and two read-only rate-integrity passes. It checks 33
accounting counters, first-start/document history, exact terminal expiry and 46
authority denials with whole-DB reads. Historical START returns its original
snapshots with no I/O permission after FINISH; grants are synthetic and unused.

The initial review identified a missing production-adapter failure dispatch and
unbound failure labels. Both are corrected and independently re-reviewed GO.
Actual adapter-parser tests now preserve ambiguous START/FINISH prefixes and
reject contradictory failure progress. Corrected checks pass 140 harness tests,
21 script tests, four Go roots under race (44 new/114 total lifecycle invocations),
vet and strict source pins. Selected arm64 preparation passed on 69 files/all
17 recipes. PR #16 merged as `561774f` with the reviewed tree unchanged; all
fourteen main-push checks and the 477-root race inventory passed. Separate
artifact approval and execution decision then authorized fixture
`1d8b4e394dffdada8518e189538449f4`: one invocation, exit 0, **12,944 ms**.

Independent correctness/security reviews accept the retained evidence: all 22
calls, 33 counter fields/deltas, history/rate projections, terminal expiries and
46 denials match. Six-role revocation, the original 28-action journal/file identity
and current exact-name/label absence of four containers/two volumes are verified.
The approval is consumed. Six public response fingerprints are independently
recomputed; sixteen reservation-bearing replies, including START permission bits,
remain dependent on the reviewed producer's private comparisons. See the
[run and evidence](../../docs/crawl-jobs-v2-m4-request-run-2026-09-28.md) and
[preparation checkpoint](../../docs/crawl-jobs-v2-m4-request-lifecycle-2026-09-28.md).
This zero-interval case does not prove positive-interval blocking, cross-worker
concurrency, actual fetches, request durability or after-I/O recovery.

**Positive-interval case (2026-09-30): accepted scoped real PASS.** `ledger-positive-interval-v1`
is the eighteenth closed case. Group and origin both use the matched group's
8,000-ms interval; global remains zero. The 24 calls contain six mutations, four
errors, eleven replays, one group-first rate denial and two integrity passes.
`rate_before` retains twelve rows; observer-only `rate_clock` reads the same Redis
run at most twenty times; `rate_after` binds the complete 24-row prefix and wait
before checking 46 authority denials. Sleeps are capped at two seconds and do not
substitute for Redis TIME. Missed denial/replay windows fail rather than relabel.

Three fresh Go facade profiles check deadline−1 denial and deadline/deadline+1
admission with independently constructed policy, identities, wires and state
comparisons: 60 canonical invocations. The full 150-test Python harness and 21
script tests pass. An aggregate existing-Go regression command timed out at 900
seconds; the same four roots subsequently passed in separately bounded race
batches without removing assertions. Vet and strict Lua/bundle checks pass.
Source review covers 101 files; image checks cover 71 files/all 18 recipes.
All six metadata resources are independently absent. Published `7431599` on
draft PR #17 passes 14/14 protected checks, with all 478 race roots accounted for
(477 pass/one allowed optional skip). See the
[preparation checkpoint](../../docs/crawl-jobs-v2-m4-positive-interval-2026-09-29.md).

The initial approval expired unused, before reservation. After explicit renewal
and a separate execution decision, fixture `99e1a00b…` ran once in 21,736 ms:
all 24 calls/33 counters/46 denials pass. Group blocking was observed 7,432 ms
before the exact deadline; four clock samples crossed it by 818 ms and admission
followed 1,793 ms after it. Measurement spans 12,748 ms within the original lease.
Both independent reviewers accept the scoped evidence. Six-role revocation,
original 33-action journal and exact name/label absence of four containers/two
volumes pass. Ten distinct runtime envelopes cover twelve completions; two ready
completions are journal-only. The renewed approval is consumed. See the
[run and evidence](../../docs/crawl-jobs-v2-m4-positive-run-2026-09-30.md).
Independent origin blocking, shared concurrency and policy tightening retain
separate coverage obligations; full M4 remains open.

**Shared group capacity (2026-10-01): GO for offline foundation only.** After
PR #17's same-tree merge as `7da55b1` and main CI verification, the new
`shared_capacity_specs.py` / `shared_capacity.py` model two distinct runs/jobs,
owners/tokens and origins sharing one group slot. The complete union is 90
positions (89 fixture-owned, 45 setup entries); REQUEST wires stay at 57 keys.
The 21-call main trace checks pending/started capacity denial, release through
FINISH, peer-safe replays, independent accounting and both retained leases.
The nine-call cancellation sibling checks the same `RESERVATION_CANCELLED`
status on initial/replay without refunding B's held capacity.

Independent Go derives policies/identities/literal wires and validates complete
state, schemas, counters, per-scope history and proposed ACL trace predicates.
Five profiles pass **81 canonical Lua invocations under race**. The initial full
160-test harness and 21 script tests pass; after two test-only improvements, the
final 11-test shared module, Go race roots and vet pass. Current discovery is 161
Python tests/480 Go roots; a new full-161 harness invocation is not claimed.
Both independent reviewers retain scoped offline GO on the 105-file strengthened
inventory. See the [checkpoint/evidence](../../docs/crawl-jobs-v2-m4-shared-group-capacity-2026-10-01.md).

At the offline foundation checkpoint, scenarios numbered 21 but executable cases
remained 18 and the reader lacked the 90-position shape. Its immutable evidence
retains that scope; the subsequent lifecycle implementation is recorded below.

**Shared group lifecycle (2026-10-01): implemented, independent GO for image/CI
preparation.** The nineteenth case fixes runtime dispatch to the 21-call finish
trace. `shared_executor.py` verifies BOOT/probe/setup binding, all 90 positions,
60 distinct counters, safe job/history/expiry/rate projections and 46 authority
denials. The real worker and `Docker.stage` parser retain only valid failure
prefixes; ambiguous commands are not retried. Existing four-container/two-volume,
six-role and 300/30/60-second bounds remain. Cancellation/reversed siblings are
offline controls, not runtime selectors.

All 171 harness tests, 21 script tests, 81 canonical Go/Lua invocations under race,
vet and pins pass. Independent correctness/security reviews are GO on 107 frozen
files, with no actionable findings. Registry/image scope is now **19 cases / 21
scenarios / 74 image files**. At this source-review checkpoint no real shared case
had run; subsequent preparation, publication and execution are recorded below.
See the [lifecycle checkpoint/evidence](../../docs/crawl-jobs-v2-m4-shared-group-lifecycle-2026-10-01.md).

**Shared-group selected preparation (2026-10-01): PASS.** Pinned-base,
network-disabled arm64 build yields harness `03bf146…`; explicit shared-case plan
`52659399…` and recipe `05bfdbbe…` bind all 74 image files/19 recipes to the reviewed
107-file source. Init/executor image-check memory peaks are 48,857,088 /
48,828,416 bytes. Four metadata-role containers remained stopped; all four names
and two volumes were independently absent. Only Python image checks and Redis
`--version` ran. See the [preparation and exact artifacts](../../docs/crawl-jobs-v2-m4-shared-group-preparation-2026-10-01.md).
At this preparation checkpoint, scoped publication/protected CI and fresh
artifact/execution decisions remained pending. Their completion is recorded below.

**Shared-group single run (2026-10-02): accepted scoped real PASS.** The 49-file
checkpoint was published as `c381287` on draft PR #18 with 14/14 protected checks.
Downloaded evidence accounts for 480 compiled race roots (479 passes and the sole
allowed optional native-parity skip), 171 harness/21 script tests and all 74 image
files/19 recipes. CI's amd64 preparation explicitly selects claim/release; the
selected shared-case arm64 preparation remains separate.

The first approval expired unused. After explicit renewal and a subsequent
execution decision, fixture `e47d57da…` ran once in **15,186 ms**: all 21 calls,
three capacity denials, 60 counters and 46 authority negatives pass. The complete
Redis measurement spans 4,508 ms. Independent correctness/security reviews accept
the scoped evidence. Six-role reachable-server revocation, the original
28-action/2,904-byte journal and exact name/label absence of four containers/two
volumes pass. The renewed approval is consumed. See the
[result and evidence](../../docs/crawl-jobs-v2-m4-shared-group-run-2026-10-02.md).

Seven public response hashes are independently reconstructed; fourteen private
replies and unexported START permissions remain pinned-runtime attestations.
Five retained stage envelopes cover seven completions, with two journal-only
ready completions. This serial logical-owner case does not establish simultaneous
workers, per-owner credential isolation or independent origin/global saturation.
Cancellation/reversed traces remain offline controls; full M4 remains open.

**Shared-group publication and merged-main gate (2026-10-03): PASS.** Result
checkpoint `c8c5ee2` adds 15 byte-identical result exports and status updates.
PR #18's two-commit/67-file scope was squash-merged as `d3b241e`, with the same
reviewed tree. All fourteen main-push checks pass; downloaded evidence matches
480 compiled race roots (479 passes/one allowed optional native-parity skip),
171 harness/21 script tests without Python skips, and 74 image files/all 19 recipes.
PHP warning classifications are retained. The CI image preparer still explicitly
selects claim/release; the original shared-group run remains bound to `c381287`.
See the [merge checkpoint and exact exports](../../docs/evidence/m4-shared-group-merge-2026-10-03/README.md).

**Shared-group cancellation local implementation checkpoint (2026-10-05).**
`ledger-shared-group-cancellation-v1` is the twentieth closed case. Its fixed
nine-call `CANCEL_STEPS` trace checks successful pending cancellation, immediate
replay, B's admission and historical A replay while B holds the group slot. A
records zero starts; B establishes first-start history. Complete 90-position state,
60 counters, exact lease/tombstone expiry and 46 authority denials remain bounded.

The case has its own fixture/policy identity and `SGCANCEL01`–`SGCANCEL09` receipt
prefix. The plan's closed case selects the trace; request profiles/selectors remain
rejected. The original 21-call finish case retains its semantics. Actual worker
serialization and adapter parsing validate partial prefixes, including ambiguous
cancellation/replays without retry. An explicit ACL case-id/fixture guard rejects
cross-case builder substitution.

Local checks pass the full 183-test pre-guard harness, 21 scripts, 81 canonical Lua
invocations under Go race, vet and strict pins. After the final ACL guard/two
cross-case assertions, all 33 shared-family tests pass; no second full 183-test run is
claimed. Twelve new methods cover eighteen state/reply faults, five ambiguous
operation profiles and all ten prefix boundaries. Registry/scenario/image-path
counts are **20/22/74**; existing image COPY inputs already contain the runtime
modules. Source hashes changed, so new image/CI evidence is required.

See the [local implementation and verification](../../docs/crawl-jobs-v2-m4-shared-group-cancellation-2026-10-05.md).
At that implementation checkpoint independent source review was pending. The
subsequent correctness/security reviews both return **GO for image/CI preparation**
on the same 108-file inventory. Independent checks include 33 final-byte family
tests, 18 canonical cancellation race calls, 22 artifact scenarios and 88 security
controls. No final-byte full 183-test run is claimed. See the
[review result and exact records](../../docs/crawl-jobs-v2-m4-shared-group-cancellation-review-2026-10-05.md).
At that source-review checkpoint fresh selected images were next. The subsequent
preparation result follows; no real cancellation case has run and full M4 is open.

**Cancellation selected arm64 image preparation (2026-10-05): PASS.** New
harness/stand-in `sha256:e57da19e8b99ffbd630545730b3883d00f6088a5f56068ca0b5aed2bba32d0ee`
matches all 74 image files and all 20 recipes. The preparer explicitly selected
`ledger-shared-group-cancellation-v1`; its plan recompiles exactly and the reviewed
recipe/108-file source remain unchanged. Four metadata containers stayed stopped;
all six metadata resources are independently absent. Only networkless image
validation and Redis `--version` executed. See the
[preparation report and exports](../../docs/crawl-jobs-v2-m4-shared-group-cancellation-preparation-2026-10-05.md).
Scoped publication/exact CI and separate artifact/execution decisions are next.

**Final independent re-review (2026-09-21): GO for image preparation only.** The
four original findings and a closed-peer follow-up are fixed and re-reviewed.
See the [closure report](../../docs/crawl-jobs-v2-m4-rereview-2026-09-21.md).
The [original NO-GO report](../../docs/crawl-jobs-v2-m4-review-2026-09-21.md) and
reproduction scripts remain dated evidence. Image and exact-revision CI gates
now pass; see the [bounded approval record](../../docs/crawl-jobs-v2-m4-ci-approval-2026-09-22.md)
for its recorded scope. The subsequent single smoke attempt
[failed in init](../../docs/crawl-jobs-v2-m4-smoke-report-2026-09-22.md), before Redis
startup; cleanup was verified. The one-case approval is used and must not be reused.

**Init correction (2026-09-22): GO for scoped publication and revised-image
validation.** Docker's `CAP_CHOWN` spelling is now admitted only as the equivalent
singleton init capability. Captured-metadata regressions, value-free diagnostics
and stopped-role image-preparation checks pass independent review. The rebuilt
image and new checkpoint CI pass; see the
[new CI/approval record](../../docs/crawl-jobs-v2-m4-init-fix-ci-2026-09-22.md) and
[diagnosis/review report](../../docs/crawl-jobs-v2-m4-init-fix-2026-09-22.md).

**Bounded execution (2026-09-22): PASS.** After PR #10 merged, the owner explicitly
requested execution at the exact approved, byte-identical revision `a02991c`.
Fixture `f9692c58d9f07689660a97fbc70ea973` passed probe/restart, BOOT/replay,
empty maintenance, 21 ACL denials and six-role revocation. All four containers and
two volumes were directly confirmed absent. The approval is consumed. See the
[passing report](../../docs/crawl-jobs-v2-m4-smoke-pass-2026-09-22.md).

**Bounded claim/release execution (2026-09-23): PASS.** The owner separately
approved and requested one `ledger-claim-release-v1` attempt on unchanged
checkpoint `b4bda19`. Fixture `f59af8adfe82573b64b8dfda427a0b00` passed nine
transitions, 46 ACL denials and full typed state/absolute-expiry checks. Receipts
retain 33 counters per snapshot and 28 controller actions, including 11 cleanup
actions. All six credentials were revoked; four containers and two volumes were
independently confirmed absent. Approval is consumed; see the
[dated result](../../docs/crawl-jobs-v2-m4-claim-run-2026-09-23.md).

## Implemented commands

From the repository root:

```bash
python3 -B tests/crawl-jobs-v2-redis/harness.py inventory
python3 -B tests/crawl-jobs-v2-redis/harness.py prepare \
  --inputs /absolute/path/to/reviewed-inputs.json \
  --expected-input-sha256 <sha256-of-exact-input-file>
python3 -B tests/crawl-jobs-v2-redis/harness.py validate \
  --plan /absolute/path/to/offline-plan.json
python3 -B tests/crawl-jobs-v2-redis/harness.py validate \
  --plan /absolute/path/to/offline-plan.json \
  --setup /absolute/path/to/offline-setup-projection.json
```

Compiler output is JSON on stdout; validation errors have a value-redacted message and
nonzero exit code. Commands write no files. The input hash binds bytes; supplying
a matching hash is not owner approval or proof of artifact provenance.

`inventory` checks the actual 43 canonical sources, contract, generated Go pins,
and shared fixture before reporting all 52 gate variants as `not_run`. It also
extracts section 17 requirements verbatim with stable content-bound IDs, empty
test/evidence mappings and `unimplemented` status. This is a coverage backlog,
not a report of tests that ran.

## Closed input schema

`prepare` accepts **exactly** these fields; unknown, missing, duplicate or
coerced fields fail:

| Field | Required value |
|---|---|
| `format_version` | Integer `1` (not boolean) |
| `scenario` | One of the 17 closed names in `harness.SCENARIOS`: the original four plus the 13 names in `negative_specs.CASES` |
| `redis_version` | Exact Redis 7 numeric version, at most 64 ASCII bytes |
| `redis_image` | Nonzero lowercase `sha256:<64 hex>` declaration |
| `harness_image` | Nonzero lowercase `sha256:<64 hex>` declaration |
| `standin_image` | Nonzero lowercase `sha256:<64 hex>` declaration for all seven unavailable participant roles |

No endpoint, marker, contract override, script source/path, render rule, secret,
or operator-selected setup key is accepted. Files/JSON are bounded at 2 MiB;
duplicate keys, floats, nonfinite numbers, invalid UTF-8 and lone surrogates fail.
There are no external JSON Schema/runtime dependencies: these closed schemas
and exact-reconstruction checks are enforced by the compiler and adversarial tests.

The compiler recomputes test descriptors, normal guard/compatibility RECORDs,
image-role mappings and source/config identities. All four evidence fields are
unmeasured test descriptors in this offline recipe. Real measured inputs and
BOOT evidence must be added through reviewed execution recipes later.

Every result is `purpose=conformance_only`, `execution_authorized=false`,
`release_eligible=false`, `measurement_status=not_measured`, with image and
isolation verification `pending`. A syntactically valid image digest does not
prove an OCI artifact exists, was built from this code, or is reviewed. Those
checks require the pinned target-image preparation gate. The unit-test image
hashes are fabricated lexical controls, never execution inputs.

`validate` rebuilds the entire plan using its closed inputs and current source
pins. Changed descriptors, flags, RECORD order, unknown fields, stale compiler,
config or contract bytes fail even if a caller recomputes their individual hashes.
An approved executor must additionally pin the exact reviewed plan digest;
offline self-consistency alone is not authority.

## Setup projection and ACL compiler APIs

`ledger_setup(plan, redis_time_ms)` produces an **offline projection** of the
four permitted direct ledger authority writes: active compatibility, contract,
stored guard and empty-legacy retirement. It emits named synthetic empty
artifacts, exact types/field bytes/digests, persistent expiry rules and required
candidate/freeze/legacy absence. It never writes durability or asserts BOOT
approval. The supplied time is an input, explicitly `time_observation_verified=false`;
the future runner must capture Redis TIME before materializing real setup.

`validate_setup` rederives that exact inventory and every byte. It rejects
additional or missing keys, duplicate writes, changed type/expiry/evidence,
zero guard evidence and plan substitution. This API intentionally rejects both
administrative profiles: their success markers must be produced by canonical
INSTALL/RETIRE/PROMOTE, with separately reviewed synthetic legacy fixtures.
It is not yet a general run/job/stage/maximum-shape setup engine.

`ledger_acl_selectors(wire_keys, data_read, data_write)` compiles a concrete,
bounded **ledger selector fragment**, excluding wildcard/ACL-token injection,
authority-as-data grants, legacy keys and unlisted wire keys. An EVALSHA-only
selector admits the wire inventory; separate selectors permit candidate/freeze
TYPE only, other authority reads, and bounded data commands. It contains no
credential or broad root command grant. The future case compiler must prove its
exact derived-key membership; this helper cannot infer it from arbitrary keys.

This fragment is not a complete ACL file. Provisioning, exact source loading,
BOOT, observation, administrative and revocation roles still need reviewed
integration. In particular, a successful static selector test is not proof of
Redis 7 command admission. The first approved real-Redis experiment must prove
outer EVALSHA and inner-command behavior together.

`redis.conf` pins the intended configuration bytes, not a Redis image. It uses
port `0`, a private Unix socket, a required externally provisioned ACL file,
400 MiB maxmemory, AOF/always/no-truncation/noeviction, and disables blanket flush,
module loading and debugging. The first-case controller verifies 528 MiB Redis
container memory and the actual image/configuration before its probe.

## First execution slice: `ledger-smoke-v1`

The implementation covers canonical BOOT and empty-inventory
`CJ2_MAINTAIN_RATE_SCOPES`. It does not implement the full 43-operation matrix.

| File | Responsibility |
|---|---|
| `runtime_case.py` | Closed smoke/claim-release recipes, six role ACLs, exact source allowlists and approval binding |
| `claim_release.py` / `claim_executor.py` | Private claim fixture/oracle plus bounded live setup, typed snapshots and redacted claim/release measurement |
| `resp.py` | Unix-socket-only RESP2; 2 MiB request/256 KiB reply bounds, command deadlines, redacted errors, no automatic write retry |
| `executor.py` | Fixed init/readiness/probe/resume/measure/revoke stages inside networkless containers |
| `controller.py` | Reviewed-commit/image admission, exact owned Docker resources, crash/restart sequence, evidence and cleanup |
| `Dockerfile.execution` and its ignore file | Narrow test-image context; digest-pinned Python base supplied during reviewed image preparation |

The read-only recipe command starts nothing:

```bash
python3 -B tests/crawl-jobs-v2-redis/controller.py recipe
python3 -B tests/crawl-jobs-v2-redis/controller.py recipe --case ledger-claim-release-v1
```

It includes code/configuration fingerprints, the exact wire/storage inventory,
role selectors and limits. The ledger role is read-only for this empty-inventory
case. BOOT alone can mutate durability; setup can write only the four authority
records and the temporary persistence probe. The loader loads only the two pinned
canonical sources. The revoker has `ACL DELUSER`, not permission to recreate users.
Setup/loader/BOOT credentials are revoked before measurement.

### Lifecycle

1. Validate the exact approved plan/recipe/revision, expiry and architecture.
   Require every source input tracked and clean at the reviewed commit. Inspect
   already-local Linux image IDs; refuse missing images and never pull/build.
2. Persist an `INCOMPLETE` intent with exact resource names before any Docker
   mutation. Create two uniquely named/labeled volumes and a restricted init
   helper; verify emptiness before writing hashed ACL credentials/configuration.
3. Remove the init helper. Start a networkless executor and Redis, each with
   explicit UID, memory/CPU/PID/capability/read-only/restart/mount settings.
   The executor mounts only the private control volume read-only; it receives no
   data volume, host bind, Docker socket, production credential or external route.
4. Verify actual Redis version/configuration/AOF/standalone/memory state. Write
   one bounded acknowledged probe at the pre-BOOT contract key. SIGKILL Redis,
   restart the same container/volume and require a new run ID with exact probe
   bytes. Remove the probe before canonical BOOT approval and its exact replay.
5. Capture Redis TIME, compile/install/verify the exact ledger setup, and revoke
   setup/loader/BOOT access. Run active empty maintenance twice; compare complete
   key inventories and persistent-state hashes before/after each call. Check 21
   candidate/freeze content-read, write, delete, expiry and rename denials.
6. Stop, wait for and remove the actual executor container before revocation.
   Start a fresh, differently named, networkless cleanup helper with only the
   private control volume. Revoke every role; use a bounded receive-only probe
   to observe held-session EOF/reset, then require rejected fresh authentication
   while Redis remains reachable. The probe never sends PING to an already closed
   peer; data, permission errors and timeouts are not disconnection proof.
   Remove and verify only exact owned resources, with workers before Redis and
   volumes. Failed quiescence prevents revocation; any unproved cleanup
   invalidates the case result.

The init helper alone uses UID 0 with only CHOWN capability to initialize fresh
volume ownership. Redis and the executor use `65534:65534` with all capabilities
dropped. Both have network mode `none`. The controller communicates only with
the local Docker Unix socket, never a caller-selected Docker host.
Admission accepts only `CHOWN` or Docker's canonical `CAP_CHOWN` spelling for that
single init capability; no generic prefix normalization or additional capability
is allowed.

Execution is bounded at 300 seconds (approval may choose less), capped by the
approval expiry. Approval is revalidated after revision checks and intent
journaling; every Docker dispatch checks both monotonic and wall-clock limits.
Each exec process also has a hard stage timer (at most 30 seconds) covering stdin,
validation, Redis I/O and output. Startup polling stays inside one timed read-only
stage, rather than retrying an ambiguous `docker exec` from the controller.

Cleanup has a separate 60-second budget and may create its cleanup-only helper
after ordinary execution approval expires. It never restarts Redis or the old
executor. Killing the attaching CLI is not treated as worker termination.
SIGINT/SIGTERM enter cleanup, and later signals cannot skip it. An uncatchable
controller/host failure can leave resources: the durable
intent identifies them, and absence of a completed teardown report means no
valid evidence. This slice provides no automatic recovery/prune command.

### Image and execution approval gates

The corrected immutable arm64 image preparation passes; see the
[init correction report](../../docs/crawl-jobs-v2-m4-init-fix-2026-09-22.md) and
[new exact artifacts](../../docs/evidence/m4-init-fix-2026-09-22/README.md).
Preparation first verifies all four stopped container specifications using the
runtime admission path, checks created/PID-zero state, and proves cleanup. These
metadata containers never start; failure prevents further checks or artifact export.
The Dockerfile defaults to a reviewed immutable Python 3.13.15 index. Rebuild from
the reviewed tree using that base or an explicitly reviewed immutable override.
Inspect the resulting daemon-local image ID and the selected Redis image ID;
put those exact nonzero `sha256:...` IDs in a new offline plan. This first case
requires `standin_image == harness_image`. Tags/remote references are not runtime
identities, and this controller never starts participant stand-in services.

The execution approval is external, not generated or checked in by the harness.
It has exactly these fields:

| Field | Constraint |
|---|---|
| `version`, `case`, `approved` | Integer `1`, the exact selected name in the 16-case `runtime_case.CASES` registry, boolean `true` |
| `operator` | Reviewed operator label, 1–64 ASCII letters/digits/underscore/dot/hyphen |
| `commit` | Exact clean, tracked 40-hex Git revision |
| `plan_sha256`, `recipe_sha256` | Exact canonical offline-plan and current recipe digests |
| `expires_at_ms` | Valid through the whole execution budget, no more than 24 hours ahead |
| `max_seconds` | Integer 1–300 |
| `architecture` | `amd64` or `arm64`, matching both local images |

After a separate execution approval, the available command is:

```bash
python3 -B tests/crawl-jobs-v2-redis/controller.py run \
  --plan /absolute/path/to/reviewed-plan.json \
  --approval /absolute/path/to/approved-execution.json \
  --expected-approval-sha256 <sha256-of-exact-approval-file> \
  --evidence-dir /absolute/path/to/existing-private-evidence-directory
```

The original smoke invocation failed in init. The separately approved corrected
smoke, original claim/release, refreshed PC01, P01 and the remaining twelve
negative cases each ran once and passed under their own approvals. The dated
reports preserve all outcomes. All seventeen one-case approvals are consumed.
A matching input hash does not create
owner approval or permission to retry; further cases need fresh reviewed scope
and exact-artifact execution authority.

The controller writes exclusive mode-0600 intent/final JSON files outside the
fixture volumes, plus a bounded/fsynced controller-action JSONL journal. Reports bind identities, actual container checks, probe/BOOT
evidence, setup/time observations, state comparisons, denials, revocation and
destruction receipts. Credentials travel via bounded stdin, never command-line
arguments, environment variables or report fields. Errors retain only closed
failure codes and admission-check names, omitting inspected values and raw server/
Docker diagnostics. `m4_accepted` is always false: the successful smoke,
claim/release and bootstrap/ACL cases cannot certify the wider M4 matrix. Injected fake backends always emit
`evidence_kind=simulated` and `case_evidence_valid=false`.

## Verification

```bash
python3 -B -m unittest discover -s tests/crawl-jobs-v2-redis -v
```

The tests cover artifact drift, zero/forged evidence, illegal setup, role and
ACL injection, schema/size violations, deterministic output, CLI redaction and
independent Python guard formulas. Execution tests additionally cover admission,
fragmented/oversized/truncated RESP, probe loss, boot/replay, role revocation,
ambiguous Docker creates, failed cleanup, interruptions, evidence files and
redaction using fakes. `test_review_regressions.py` covers the four review
counterexamples, worker quiescence ordering, a real local Python stage-timer
exit, process-group aborts, strict peer-disconnection proof and expiry between
create/start. A real local Unix socket-pair regression covers orderly peer close
before the probe. Two target-kernel network regressions and eight container-admission
regressions, nine offline claim/release tests and fourteen integration regressions
bring the local suite to 76
tests at Step 3. Three review-follow-up regressions bring the current suite to
79 tests; it starts no Docker or Redis.
`test_container_admission.py` replays captured Docker metadata, rejects broader
capabilities, checks bounded diagnostics, and verifies metadata-only preparation
and failure cleanup. In `services/spider`:

```bash
GOPROXY=off GOTOOLCHAIN=go1.25.13 go test -mod=readonly \
  ./internal/database/crawljobsv2 -run '^TestM4OfflineArtifacts$' -count=1
```

That test consumes Python-generated artifacts through the ordinary Go codecs
and active transport, verifies zero rejection, and compares the 52 variants
against the independent literal Go wire oracle. It also compares every BOOT and
active-maintenance EVALSHA part and RESP size with the Go constructors. Ordinary structural acceptance
of test hashes demonstrates why final release provenance is a separate gate.
The Python suite runs in protected `required-tests`; the Go cross-check runs in
normal Spider tests. Docker copies these files into the **builder only**; the
runtime image copies only the Spider binary and existing configuration.

## Offline claim/release fixture (Step 2)

`claim_release.py` implements the private, deterministic `ledger-claim-release-v1`
projection. Compile a normal offline plan with scenario `ledger-claim-release`,
then supply a closed input object with `fixture_id`, `redis_time_ms`, `owner_a`,
`owner_b`, `token_a`, `token_b` and `wrong_token`. Time must be an exact bounded
integer; IDs/tokens have fixed lowercase hex shapes, and owners/tokens cannot be
reused within the case. Input time is not asserted to be a live observation.

| API | Purpose |
|---|---|
| `compile_fixture(plan, inputs)` | Private setup, fixed policy/source descriptors, derived identities and all 58 possible keys |
| `validate_fixture(plan, fixture)` | Exact reconstruction; reject altered, missing, extra, reordered or mismatched content |
| `wire_requests(plan, fixture, boot_epoch)` | Nine binary-safe CLAIM/RELEASE EVALSHA requests; no transport |
| `expected_sequence(plan, fixture, times)` | Expected replies and complete 57-key managed state for CR01–CR09, including absolute expiries |
| `validate_state(plan, fixture, times, step, observed)` | Reject partial or altered managed-state projections; steps are zero-indexed |
| `public_summary(plan, fixture)` | Redacted digest/count/provenance projection suitable for evidence output |

The 58th key, durability, belongs to bootstrap. No direct setup row supplies it.
The claim executor separately checks that BOOT record, the entire live inventory
including unknown-key absence, and the case-bound state/expiry projections.
The controller and worker enforce the actual approval/lifecycle/stage limits.
Full fixture/state/wire objects contain private
tokens and identifiers and must not be logged; use `public_summary()` instead.

`test_claim_release.py` checks adversarial artifacts, state/expiry drift, strict
time bounds, no I/O and public redaction. `TestM4ClaimReleaseOffline` independently
reconstructs identities and every wire byte in Go, then compares the Python
oracle to unchanged canonical Lua in the existing in-memory command facade.
Two vectors exercise all nine transitions with delayed/increasing and large equal
timestamps. This is offline conformance, not target Redis/AOF/ACL acceptance.

```bash
GOPROXY=off GOTOOLCHAIN=go1.25.13 go test -mod=readonly -race -timeout 180s \
  ./internal/database/crawljobsv2 -run '^TestM4(OfflineArtifacts|ClaimReleaseOffline)$' -count=1
```

Run that command from `services/spider`. The Python fixture/vector modules are
included only in the Spider builder's test inputs.

## Bounded claim/release integration (Step 3)

The runtime now selects exactly one of the two known cases from the validated
plan. The external approval must name that case and its current recipe hash;
case-swapped approvals and Docker ownership labels fail closed.

For claim/release, the controller generates fresh private owner/token material.
Init derives exact ACL keys from it before Redis starts. Resume repeats the
persistence/BOOT sequence, captures setup time, finalizes the private fixture,
checks ACL time independence and installs its validated 26-key setup. All three
setup roles are revoked before measurement.

`claim_executor.py` enforces the full 58-key inventory, fixed typed records,
bounded scans/reads, exact indexes and absolute expiry times. Its one timed stage
performs CR01–CR09, then all 46 authority-denial probes. Stage output contains
status labels, timestamps, digests, hashed references and a fixed 33-counter
before/after/delta projection. Counter types and exact per-step relationships are
validated before retention. The controller rejects known credential/private
values and binds both successful and failed measurement receipts to the setup fixture.

Ordinary measurement errors return a closed FAIL receipt with completed-step
prefixes; hard process termination or malformed/missing output may provide no
prefix. `<fixture>.actions.jsonl` independently retains completed controller
actions, including cleanup quiescence, revocation and resource-absence checks.
Cleanup journal/callback errors invalidate PASS but do not prevent later teardown.
The journal is not an internal Lua crash-boundary trace. Every failure still
requires worker-first quiescence/revocation/destruction, and unproved cleanup
invalidates the case.

The fake-backed lifecycle covers success, partial failures, no retry after an
ambiguous result, corruption, expiry drift, ACL escalation/denial failures,
interruptions, private-output rejection, case ownership and journal failures.
The independent Go test also checks actual canonical command traces against
the generated ledger selector inventory. These checks do not certify real
Redis selector parsing, filesystem behavior or target-image performance.

At the claim checkpoint, execution-image allowlisting covered 57 files and the
image checker validated both original recipes. `scripts/prepare-crawl-jobs-v2-images.py --case
ledger-claim-release-v1` selects the claim case when preparing approved images.
Independent review and follow-up re-review are GO. Corrected arm64 image validation
also passes; see the [claim-specific preparation report](../../docs/crawl-jobs-v2-m4-claim-image-preparation-2026-09-23.md).
Checkpoint `b4bda19` is published on draft PR #11 with all fourteen required
checks passing and claim-specific amd64 evidence verified; see the
[CI record](../../docs/crawl-jobs-v2-m4-claim-ci-2026-09-23.md) and
[Step 4 review report](../../docs/crawl-jobs-v2-m4-claim-review-2026-09-23.md).
The separately approved single real-Redis case then passed on that unchanged
checkpoint, with independent cleanup checks and a consumed approval. Its
[execution result](../../docs/crawl-jobs-v2-m4-claim-run-2026-09-23.md) records
the measured scope and exact evidence identities.

## Bootstrap/ACL negative implementation (next-gate Step 2)

| Module | Responsibility |
|---|---|
| `negative_specs.py` | Closed case IDs, source lists, expected outcomes, extra roles and measured sequences |
| `negative_cases.py` | Private P/S fixtures, W/B wires, BOOT provenance comparison, independent fresh admin state projections |
| `bounded_state.py` | Whole-DB 2/58/71/90-position typed reads, bounded fields/members and absolute expiry comparisons |
| `negative_executor.py` | Setup/role retirement, negative calls, canonical positive-control dispatch, redacted prefix/measurement receipts and strict controller validation |
| `admission.py` | Closed image environment and process/program predicates, storage emptiness and permitted same-case volume attachments |
| `recovery_specs.py` / `recovery_oracle.py` | Closed recovery case/phases and independently checked complete state/expiry projections |
| `recovery_executor.py` | Claim-and-park, post-death observation, Redis-time and recovery/fencing/ACL phases with strict receipts |
| `parked_command.py` | Bounded attached-command handshake, non-reaping child liveness and separately bounded exit reconciliation |
| `request_specs.py` / `request_oracle.py` | Closed 22-call same-lease robots/document fixture, exact REQUEST wires and full state/history/expiry projections |
| `request_executor.py` | Synthetic START/FINISH measurement, strict error/time and public-prefix reconstruction, bounded whole-DB comparisons |
| `rate_specs.py` / `rate_executor.py` | Closed 8-second rate case, three phases, observer-only bounded clock wait, exact cross-phase receipt binding and failure prefixes |
| `shared_capacity_specs.py` / `shared_capacity.py` | Closed two-run shared-group-capacity oracle and ledger/observer ACL projection; fixed finish trace for runtime, cancellation/reversed offline controls |
| `shared_executor.py` | Bounded combined setup/90-position reads, 60-counter redacted receipts, exact 21-call execution and strict failed-prefix validation |

The original planning packet remains an immutable, non-executable specification.
The executable registry separately contains smoke, claim/release, the 13 fixed
negative cases, pre-I/O recovery, bounded request lifecycle, positive-interval
admission and shared-group capacity. There is no arbitrary case, key, mutation, interval or source
parameter. P/S setup is separately labeled invalid stored state; observers may
read its exact bounded bytes while ledger marker permissions stay restricted.
Administrative candidate/freeze/retirement/guard states come from canonical
scripts under separate short-lived release/migration roles. Revocation remains
worker-first, and the revoker is last even in seven/eight-role cases.

The current execution-image allowlist is **74 files**. All 19 recipes are checked
by `image_check.py`, including their case-specific credential inventories.
Controller admission binds the exact entrypoint/command and environment digest,
rejects DNS/host/port/bind/privilege deviations and checks volume attachments
before and after start. The executor checks the private process inventory;
cleanup refuses to remove a volume still attached to an unowned container.
Prior paths have image-validation and actual scoped Redis-case observations bound
to their historical revisions. The request slice now has a separately authorized
real-run PASS at merged `561774f`, with scoped independent acceptance. H/I
negatives retain the offline/predicate evidence classes stated above.
The positive-interval delta now has separately approved real-run PASS at published
`7431599`, after exact-revision CI and fresh execution decisions. Its approval is
consumed; old run bindings remain historical and the next case needs fresh gates.
Shared-group-capacity source now has its own selected arm64 preparation, while
publication/CI and actual execution remain pending. Prior case execution bindings
and older image records remain historical.

Read-only recipe inspection, from repository root:

```bash
python3 -B tests/crawl-jobs-v2-redis/controller.py recipe --case bootstrap-rejections-v1
python3 -B tests/crawl-jobs-v2-redis/controller.py recipe --case ledger-promote-denied-v1
```

At the bootstrap/ACL implementation checkpoint, the complete Python suite had
**95 tests**, including every planned H/I variant
and all 13 simulated lifecycles. Four new Go test roots run **70 canonical Lua
invocations** with independent wire/response/state checks; four additional
RETIRE/PROMOTE outer-denial checks use the offline selector oracle and do not
claim target Redis ACL execution. Run from `services/spider`:

```bash
GOPROXY=off GOTOOLCHAIN=go1.25.13 go test -mod=readonly -race -timeout 900s \
  ./internal/database/crawljobsv2 -run '^TestM4(Negative|AdministrativeDenial)' -count=1
```

The Go test timeout does not change the 300-second case, 30-second stage or
60-second cleanup limits. Public receipts retain no private fixture or wire
values, and simulated results always set `case_evidence_valid=false`.

## Remaining acceptance work

The [implementation plan](../../docs/crawl-jobs-v2-plan.md) owns progress.
The completed bounded slice is
[`ledger-claim-release-v1`](../../docs/crawl-jobs-v2-plan.md#next-bounded-slice-ledger-claim-release-v1):
one ready job, two claim/release cycles, exact replay, stale ownership, complete
state/expiry checks and write-capable authority ACL denials. The
[planning inventory](planning/claim-release-v1.json) records all 104 requirement
entries and 52 operation/gate variants, existing partial smoke evidence and
twelve planned assertions. It remains the initial planning snapshot, not an
executable artifact or mutable run-status inventory. Steps 1–6 are complete:
implementation, independent review, image/artifact validation, protected CI and
the separately authorized real-Redis case all pass. Full M4 remains open.

The corrected first-case execution layer and target-discovered corrections have
scoped independent GO. The init admission defect is corrected and re-reviewed;
rebuilt-image content, memory and stopped-role isolation checks pass. Protected
CI and both separately authorized real-Redis cases pass. Next are the remaining
M4-P3 bootstrap/ACL negatives: candidate/freeze presence, valid administrator
denial under ledger credentials, malformed gates and isolation negatives.
Step 1's [`bootstrap-acl-negatives-v1` specification](../../docs/crawl-jobs-v2-plan.md#next-bounded-package-bootstrap-acl-negatives-v1)
and [planning packet](planning/bootstrap-acl-negatives-v1.json) define 13 new
Redis cases, one refreshed existing positive control, and separate offline/
admission assertions. Step 2 implements the closed cases and passes local
verification; the planning JSON itself remains rejected as an execution plan. Its full
104-requirement/52-variant inventory reference and historical PASS hashes remain
explicit. Step 3 correctness/security reviews are GO with no actionable findings.
Step 4's arm64/amd64 preparation, publication and all required CI checks pass on
`6340401`; the separately approved Step 5 PC01 invocation now also passes with
independently verified cleanup and consumed approval. Step 6 accepts that scoped
control and preserved all 13 negatives as unrun at that review. P01's later
selected-case preparation on `963b67b` and separately approved single attempt
also pass, with zero state/accounting delta, 46 denials and verified cleanup.
P01's scoped review was accepted. The remaining twelve cases subsequently passed
individual preparation, exact approval, separately requested sequential execution
and scoped evidence review, with all approvals consumed and resources absent.
All 14 package Redis cases pass; zero negatives remain unrun. September 25's
independently reviewed reconciliation accepts narrow M4-P3 bootstrap readiness.
The full M4 inventory assigns 104 requirements and 52 gate variants to twelve
work packages. The first recovery lifecycle subsequently passed publication/CI,
separate owner decisions and one real pre-I/O worker-death case with independent
evidence acceptance and cleanup. Existing accepted evidence keeps its original
bindings; that consumed approval cannot authorize another case. The subsequent
request lifecycle subsequently passed main CI, new artifact/execution decisions
and one real run with independent scoped acceptance; its approval is also consumed.

Broader job/lease/stage/commit behavior, administrative transitions, maximum shapes,
crash-boundary coverage, benchmarks and final release/image admission remain
subsequent gates. Fake results never substitute for those observations.

Run the new offline-only checks from this directory and `services/spider`, respectively:

```bash
python3 -B -m unittest -v test_recovery_oracle
GOPROXY=off GOTOOLCHAIN=go1.25.13 go test -mod=readonly -race -timeout 900s \
  ./internal/database/crawljobsv2 -run '^TestM4RecoveryOracleOffline$' -count=1
```
