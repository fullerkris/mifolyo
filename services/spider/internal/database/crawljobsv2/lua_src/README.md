# Shared Lua core API (M3, dormant)

## Current inventory and scope

All **43 canonical operation sources** in `../lua/` are implemented. BOOT is
the unchanged standalone passthrough; the other 42 are exact outputs of explicit
recipes in `scripts/generate-crawl-jobs-v2-lua.py` (repository-root path).

| Operation family | Canonical sources |
|---|---|
| Administrative and BOOT | 5 |
| Run lifecycle and records, including CANCEL_RUN, FINALIZE_RUN and ARCHIVE_RUN | 9 |
| Worker and requests | 12 |
| Stage and COMMIT | 11 |
| Bounded maintenance (the six rev4 operations below) | 6 |

The zero-argument Go `AuthoritativeScriptBindingSet()` factory is implemented;
it returns a sealed binding set from embedded sources checked against fixed
build-time pins. See [Assembly](#assembly) for source and bundle checks.

This remains **dormant, in-memory source conformance only**, not real-Redis or
operational acceptance. M4's fixture amendment is owner-approved: command-scoped
absence checks, harness-owned setup manifests, isolated administrative tests and
nonzero test-input descriptors. The offline compiler is implemented under
`tests/crawl-jobs-v2-redis/` at repository root, alongside a locally tested first
ledger-smoke executor. Its findings and closed-peer follow-up are fixed; final
independent correctness/security re-review returns GO for image preparation
only. Image validation now passes; protected CI and execution approval remain
pending. Real-Redis clock,
allocator, maximum-shape latency, ACL and crash/AOF evidence remain separate
gates. Source completeness and bundle sealing do not approve a commit guard,
authorize I/O, or activate runtime integration or the V1 client.

The revision headings and baseline conventions below record cumulative API
additions: read revision 2 together with the revision 3/4 extensions to commands,
key grants and memory coverage.

## Rev4 operation-owner interface

Core supplies the clocked wire, read/grant and memory/sealing APIs below.
Operation fragments in `ops/` and their `ledger_*.lua` planners own semantic,
state, replay and reply validation, and must return on every failed helper.

### Wire and deferred/derived bindings

* `Wire.worker_spec(op)`: REJECT_READY, TRY_CLAIM, RENEW_LEASE,
  RESERVE_REQUEST, START_REQUEST, FINISH_REQUEST, CANCEL_RESERVATION,
  RELEASE_BEFORE_IO, RETRY, DEAD, CANCEL_JOB, COMPLETE_NO_OUTPUT.
* `Wire.stage_spec(op)`: BEGIN_STAGE, all seven STAGE data variants,
  ABORT_STAGE, SEAL_STAGE, COMMIT.
* `Wire.maintenance_spec(op)`: PROMOTE_DUE, RECOVER_EXPIRED, CANCEL_BATCH,
  PURGE_RUN_BATCH, CLEAN_STAGE, MAINTAIN_RATE_SCOPES.

These return source-owned exact semantic arrays, tail RECORD orders/counts and
request ceilings from Go/the norm. All key derivation is inside clocked open.
WORK=44, REQUEST=57, WORK+STAGE=117, COMMIT=118, CLEAN=83,
RATE_MAINTENANCE=9, PURGE=42 or 43. Blob framing alone is binary: exact field_name
is checked by Wire; Stage.validate_chunk validates field_bytes UTF-8/size.

WORK names reuse AUTH/LIVE and the 27 `run_*` names plus `job`. Request names
add `reservation`, and `{global,group,origin}_rate` plus `_active`, `_pending`,
`_started`; `keys.scopes[kind]` has `{scope,active,pending,started,ordered}`.
`ctx.reservation_id` is derived for claim/reserve, supplied for the other three.
TRY's expected_prior_fence must safely increment to fence before deriving Q.
`Wire.request_identity(values)` performs pure URL/target/scope/control framing;
Request.intent still binds policy/depth/budgets to private Run/Job evidence.

Stage names are `stage_meta`, `stage_keys` (the LIST), `stage_page`,
`stage_outlinks`, `stage_discoveries`, `stage_discovery_records`,
`stage_discovery_depths`, `stage_aliases`, `stage_image_manifest`, and
`stage_image_0`..`stage_image_63`. `keys.stage` is the complete Stage.keys-shaped
bundle and `keys.stage_images` its 1-based image array. COMMIT adds pages_queue.
`Wire.stage_keys(commit)` / `Wire.rate_keys(scope)` are pure closed derivations.

START/FINISH/CANCEL initially validate only the known first 45 REQUEST keys;
the twelve supplied scope keys are privately retained but **not authorized**.
`Context.bind_request(ctx)` requires Request.check_receipt's private reservation,
TTL, Run, Job and five immutable policy-map receipts, checks the full supplied
lease identity, compares all twelve exact derived scope keys, then completes
the wire grant. A nonempty plan is impossible before this succeeds. A definitive
empty-plan rejection/reconciliation need not read unrequested scope fields.
`ctx.keys_pending` is diagnostic only, not an authority flag.

Closed derived-key helpers (all prepare-only, no arbitrary path/prefix grant):

* `Context.bind_job(ctx,job_id)` for PROMOTE_DUE/RECOVER/CANCEL_BATCH/PURGE:
  requires the selected private delayed/leased/ready-or-delayed/job_order
  membership respectively; due paths require a nonfuture exact deadline.
  At most 100 job IDs may bind per invocation. Returns `{run_id,job_id,key}`.
* `Context.bind_held_request(ctx,job_id)` for RENEW/RECOVER/RELEASE_BEFORE_IO:
  the private job pointer grants only its reservation key for reading, or returns
  `{exists=false}` for an empty pointer. RECOVER requires the privately bound
  selected job; RELEASE requires the original matching live wire lease and pre-I/O
  baseline. RENEW binds only the original wire **job ID**, revalidates the private
  Job/Run/policy/index receipts, and records the **stored current lease identity**.
  A stale owner/token/fence or logical expiry does not suppress inspection of that
  current held request. It never selects a peer job or trusts a public pointer.
  After reservation/TTL selection, `bind_held_scopes(ctx,id)` authenticates
  Request.check_receipt and the stored job/pointer/lease/deadline relationship.
  It derives only that reservation's three exact rate-key groups, not the job's
  source scopes or caller-chosen IDs. For RENEW, these and the reservation remain
  **read-only** unless the original private wire owner/token/fence matches the
  stored lease and its deadline is strictly after the private Context TIME.
  Public credential/operation/clock/allowed-map edits cannot upgrade this grant.
  Matching-live RENEW may obtain the derived write grant; handlers still own full
  request/scope/stage checks and renewal eligibility before adding any mutation.
  RELEASE retains its matching-live, nonexpired **pending** reservation and
  pre-I/O requirements. RECOVER's separate selected-job proof is unchanged.
* `Context.bind_rate(ctx,scope_id)` for MAINTAIN_RATE_SCOPES: private inventory
  member, max 100 scope IDs; all four keys are read-only. After complete bounded
  scope-index reads, `bind_scope_reservations(ctx,id)` grants read-only access to
  at most 32 selected reservation keys. Workers can use that same helper for a
  scope privately authorized by their validated wire or held reservation. The
  union must equal the complete active index, pending/started must be disjoint,
  and all selected scores must agree. Worker binding then explicitly selects
  the peer reservation/TTL, verifies its scope/state/deadline link, grants only
  its exact Run/Job/five immutable policy maps for reading, selects those bounded
  witnesses and calls Request.check_receipt. No peer grant adds write permission,
  changes current run binding, prunes expiry, or authorizes I/O. Unrequested,
  incomplete, fabricated or unrelated-scope receipts fail before peer reads.
* `Context.bind_stage(ctx,job_id)` for renewal/outcomes/recovery: private job
  active-stage (or exact abort-history) pointer + matching private slot owner.
  Grants the closed 73-key bundle for reading; RECOVER alone may also write meta.
* `Context.bind_stage_owner(ctx)` for CLEAN: derives a read-only Run/Job binding
  from the private slot or validated retained meta. It adds the 27 run keys,
  job key and required global inventory/lease reads, never Run/Job write rights.
* `Context.bind_commit(ctx,ownedStageHandle)` for COMMIT: authenticates the
  context-bound Stage.slot_proof and private sealed meta/output receipts, then
  derives only the final page/outlinks/manifest/images, backlinks and this run's
  discovery-job keys. Returned `keys.output` is diagnostic; private grants are
  independent. Visited hashes are already RUN keys; alias fields remain planner
  predicates, not arbitrary new-key grants.

`Context.checked_job(ctx,id)` reconstructs immutable Run/groups and Job.check
from private receipts without Redis reads. It is not Run's aggregate ledger
validation. Handlers must still use Run.load/validate_ledger and Request/Stage
full checks where required.

RENEW's read-only inspection is **not** an empty-plan replay lock or allocation/
I/O authority. Only after complete corruption inspection may the worker prepare
the ordinary-admission Run `renewal_rejections_total` increment for a definitive
stale/expired leased caller. Missing/unleased/corrupt state does not earn that
counter, and inspection supplies no safety/slot credit. Stage.check_renewal_state
is likewise inspection-only; it is not an owned-stage handle and cannot authorize
publication, allocation, stage writes, or reopening an aborted stage.

### Reads, commands, clock

`Read.due(ctx,key,through_text,limit,maximum,member_bytes)` is a bounded sorted
ZRANGEBYSCORE prefix (max 500; use 101 to decide more for a 100-job batch).
No complete 10k-job read is needed. Selected membership reads may use a larger
cardinality bound (e.g. additive backlinks); whole enumeration remains capped.
`Read.ttl` adds `expires_at_ms=now_ms+ttl_ms` for finite TTLs, false for persistent
keys. It still uses the one Context TIME. Actual Redis script/PTTL clock behavior
and allocator overhead remain real-Redis evidence gates, not facade guarantees.

New descriptors: `LPUSH` (each duplicate still allocates a list element),
`PEXPIREAT key positive_future_ms`, and `EXPIRE key positive_seconds<=86400`.
No options or reply-dependent branches. TTL receipts/projected creation are
required; expiry arguments and additions are checked before writes. Expiry
bookkeeping has no invented protocol logical-data term; real-Redis fixtures
must verify the full transition's G against actual allocator growth. PERSIST,
renames and last-element deletion continue to update projected key types/TTLs.
An ordinary plan consisting **only** of UNLINK/HDEL/SREM/ZREM/PERSIST is detected
from its actual descriptors as deletion-only: G must equal zero and it needs no
INFO/memory admission. No caller can request this as a coverage mode. A mixed
delete/recreate or allocating command never receives deletion credits or this
drain exception.

### Memory planning API

```
local plan = assert(Plan.new(ctx))
Plan.set_policy(plan, mode, proof_or_nil) -- before any add, once
Plan.add(plan, argv, "ordinary")
local assessment, code = Plan.assess(ctx,plan)
-- Build reply AFTER assess if it needs assessment.remaining.
local reply = Reply.build(ctx,status,tail)
local execution = Plan.seal(ctx,plan,assessment,reply)
```

Mode/proof:

* `begin`, nil: core invokes Stage.begin_record with reconstructed private Run/
  Job/lease/request evidence; caller's mutable BEGIN result is not accepted.
* `stage`, owned Stage handle: data/seal, preserves 65536 bytes and common TTL.
* `abort`, owned handle: deducts self-inclusive G, retains original positive
  key_count, and independently derives the maximum following-outcome footprint
  from registered schema widths, existing group/reason fields and closed possible
  index destinations. Actual abort G plus that bound must fit 32768 and remaining.
  The closed job footprint permits `last_reason` only with the literal value
  `none`, including reset of a prior delivery's retained retry reason. It cannot
  change `last_failure_reason`. The reset's actual replacement bytes enter the
  normal descriptor simulation/self-inclusive slot debit; no extra allowance or
  caller-supplied estimate is introduced into the abort-plus-outcome bound.
* `backpressure`, owned handle: COMMIT's first bounded bookkeeping only, leaves
  32768. No output writes/rename/LPUSH permitted by this policy.
* `slot_end`, aborted handle: actual outcome growth must fit that exact retained
  owner slot and 32768; slot removal is appended after all covered writes.
* `commit`, owned sealed handle: separate 64 MiB G bound and
  `U+S+16MiB+G<=M`, with old S (including own slot), no deletion credits.
* `safety`, nil: only FINISH/CANCEL_RES/RELEASE/non-stage outcomes, private checked
  current job and required Request checks, and no matching stage slot. Ordinary
  admission first, then `U+S+64MiB+G<=M`.
* `recovery`, nil: see units below. Existing ordinary and `cancel_run` coverage
  contracts remain unchanged. No caller supplies numeric G or free coverage.

Core owns every stage_slots mutation. **Do not add slot HSET/HDEL descriptors.**
BEGIN/data/seal/abort/backpressure append their solved replacement; removal paths
append HDEL last without a temporary decrement write. The solver tries at most
eight canonical remainder widths and re-simulates the final descriptor list to
prove `remaining = old_remaining - actual_G`. It never charges an old width and
silently stores a shorter one. No fixed point is INVALID_STATE, not a repair.
An identical replay should use an empty plan (no slot/INFO/ACL work).

Mixed recovery uses `unit=Plan.recovery_unit(plan,job_id)` after private reads,
then `Plan.add(plan,argv,unit)` for **every** effect belonging to that job.
Units are opaque, single-plan, once per job (max 100).
`Plan.validate_unit(plan,unit) -> true/code` is a read-only authenticator for
Run's contributor bookkeeping, including contributors that receive no descriptor.
It grants no G and makes no Redis call. Before admission, core derives each
unit's counter contributions from its private original Job/Request and actual
schema-validated final descriptor effects. A coalesced shared HSET field may be
assigned to **one actual contributing resource owner**, including a slot owner;
the complete replacement is charged exactly once to that unit. Different fields
may be split into different HSETs with different owners. Every aggregate delta
must equal the participating jobs' effects; unauthorized owners, overshoot,
missing effects and repeated/reversed counter changes fail closed. This permits
all-slot and mixed batches without numeric caller G or repeated partial values.
Sequential per-job replacements also remain valid when their actual deltas and
owners meet the same checks. Group ownership is exact: immutable source group
for group_open_jobs; the reservation's charged group for pending/active_started.
Reason-field owners must actually contribute that reason/outcome.
Each unit's footprint is closed to its job/reservation/index members and its
run/group/reason counters. Current private job+slot+live meta (or proved expired
meta absence) authenticate recovery coverage; this allocation proof does not
authorize publication or replace the handler's whole-batch state validation.
Slotted G is checked against only that slot; unslotted G is aggregated once for
ordinary/safety admission. Assessment exposes `covered_growth`,
`uncovered_growth`, `remaining` where relevant, and `post_abort_bound` for abort;
all are backed by private seal checks. No double charge or deletion credit.

When using the Run accumulator, pass that same opaque unit to **both**
`Run.accumulate(delta,changes,unit)` and `Run.set(delta,times,unit)` for each job,
then call `Run.flush(ctx,plan,delta)` without a blanket coverage argument. Helpers
such as Job.plan_outcome and Request.plan_terminal must forward their received
unit to those calls, not only to Plan.add/Run.hset. Missing/ordinary coverage on
a recovery contribution is INVALID_ARGUMENT; it cannot be repaired at flush or
by weakening Plan.validate_unit. A valid Run flush still needs core's independent
projected-effect and allocation checks before assessment succeeds.

Nonempty BEGIN/stage/abort/COMMIT/RECOVER plans require their policy. Slot writes
cannot be smuggled through ordinary coverage. All policy callbacks/validation,
fixed-point work, response construction and ACL checks finish before sealing.

### Foundation module integration boundaries

The Stage module (`ledger_stage.lua`) supplies
`Stage.check_cleanup_residue(view,commit)`, a token-free deletion-only proof.
CLEAN must use that API, not reconstruct a cleared token or use the
publication-capable committed-receipt helper. Core only grants its read-only
owner binding; Maintenance proves earliest/due and absence.
Stage also supplies private BEGIN/publication accessors. Core's BEGIN policy
continues to call begin_record itself from private receipts, and commit grants
continue to use the existing opaque owned handle plus private output receipts.

The Request module (`ledger_request.lua`) supplies `load_live` / `plan_terminal` for
RELEASE_BEFORE_IO's pending-capacity path. Core grants and the safety footprint
support these helpers. The complete cancelled tombstone, its absolute TTL
and all three scope active/pending removals and capacity-counter updates must
precede the first lease-clearing descriptor; shared Run ledger flush is still atomic.
Request prepares reservation/scope effects; the calling planner composes the
Job effects and flushes the shared Run ledger. Core owns grants, assessment and
sealing, not the operation's semantic eligibility checks.

## Admin/read-proof integration contract (API revision 3)

Core supplies RETIRE/PROMOTE/MARK wire/gate support. `ledger_admin.lua`
implements their planners and reply validators; the corresponding `ops/`
fragments execute their sealed plans. All three are in the complete 43-source
recipe inventory, with strict canonical-directory checking.

### Restricted internal reads and run binding

For **CJ2_ENQUEUE_BATCH and CJ2_AUDIT_RUN_BATCH only**, Context exposes
`ctx.keys.active_leases = "mifolyo:crawl:v2:active_leases"` as an internal,
read-only convenience. Their actual wire keys remain `38+n`: no argument or key
position is added. A complete job check needs the corresponding global lease
membership as well as the per-run/job facts. The ordered wire list is not an
exhaustive list of these literal, protocol-required internal reads.

`Context.can_read(ctx,key)` checks the private wire-key snapshot, rev4 derived
write/read-only grants, the stage_slots memory exception, the above two-operation
literal exception, and any validated RETIRE binding below. Read.key_type **and**
the low-level Context.call read gateway apply it. `Context.can_write(ctx,key)` requires the private original
wire-key set or a rev4 authenticated derived-write grant, AND public `ctx.allowed`,
with no receipt-only restriction present. It never includes internal read grants.
Editing public key/allowed/operation fields cannot broaden either private
permission set. No fixture/ACL rules change.

```
Context.bind_run_read(ctx, run_id) -> true / code
Context.bound_run(ctx)            -> privately bound run_id / code
```

`bind_run_read` is RETIRE + candidate mode only, during prepare. Before calling
it, the planner must select private complete receipts for the **literal**
`runs` ZSET, `active_runs` SET and `unarchived_runs` SET. Each must exist, contain
exactly one member, and that member must be exactly the supplied 32-hex run ID.
The runs score must be a positive exact-integer creation time. Cardinality-only
facts, missing/different members, multiple members, and caller-edited projections
do not qualify. Full membership coverage via all_members or selected members
with cardinality one is sufficient. The helper performs no inventory reads on
the caller's behalf; missing private receipts are an error.

After proof, it grants reads to exactly that run's 27 run keys and sets
`ctx.bound_run_id`, `ctx.keys.run`, all `run_<suffix>` names, and
`ctx.keys.run_keys`. It adds **nothing** to `ctx.allowed` or the private write
set. Rebinding to another run fails; binding the same proved ID is idempotent.
It grants no job-key reads. Use `Context.bound_run(ctx)` where a private binding
is needed rather than trusting a modified public convenience property.

PROMOTE's nonempty, validated `candidate_run_id` is already on the wire and
receives the same private/public run-ID binding during open. Fresh promotion has
no bound run (`bound_run` fails rather than defaulting to an empty ID). Its 27
keys, when present, remain ordinary wire keys; the binding adds no permissions.
`Run.load` in `ledger_run.lua` uses `Context.bound_run` for RETIRE/PROMOTE's
request/run-binding check; core's binding does not replace Run validation.

### Closed admin specifications

`Wire.admin_spec(operation) -> spec/code` accepts only:

| Operation | Semantic scalars / tail | Exact key plan |
|---|---|---|
| CJ2_RETIRE_LEGACY_KEYS | 16 / none | AUTH + RUNS + LEGACY = 16 |
| CJ2_PROMOTE_CANDIDATE_CONTRACTS | 13 / none | AUTH + LIVE + LEGACY + DOWNSTREAM + RUN*m = 29 or 56 |
| CJ2_MARK_PLANNED_SHUTDOWN | 3 / sorted run IDs (0..16) | AUTH + 5 globals + global RATE + OWNERS + run/leased pairs = 19+2*a |

Field arrays are the exact normative/Go constructor orders. RETIRE ends with
confirmation_text; PROMOTE is freeze_nonce, commit_guard_sha256, then the eleven
guard-core fields; MARK is planned_shutdown_nonce, process_stop_evidence_sha256,
active_run_count, then the repeated IDs. No new gate prefix or replay flag is
accepted. `Context.open` performs TIME first, then complete request-size and
scalar/record/identifier/number validation before deriving any ID-based keys.
RETIRE observation counts/confirmation relations and all datastore predicates
remain planner checks. PROMOTE's core shape rejects provisional zero evidence.

RETIRE/PROMOTE names reuse AUTH, RUNS, LIVE globals, LEGACY, DOWNSTREAM and RUN
names from revision 2. In particular LIVE order is `runs`, `active_runs`,
`unarchived_runs`, `first_request_start`, `active_leases`, `stage_expiry`,
`stage_slots`, `rate_scopes`; PROMOTE's optional 27 run keys follow DOWNSTREAM.

MARK names after AUTH, in wire order:
`active_runs`, `active_leases`, `stage_slots`, `stage_expiry`, `rate_scopes`,
`global_rate`, `global_rate_active`, `global_rate_pending`, `global_rate_started`,
`pages_owner`, `images_owner`, then `shutdown_run_1`, `shutdown_leased_1`, etc.
`ctx.global_scope_id` is the recomputed fixed global scope (not an ARGV scalar).
`ctx.keys.shutdown_runs` is a sorted array of `{run_id,run,leased}` pairs;
`ctx.keys.shutdown_by_id[id]` is the same pair indexed by ID. These convenience
tables add no wire keys. MARK receives only these two keys per run, not all 27.

### Closed post-state authority gates (no mutation permission)

`Gate.check(ctx)` retains normal active/candidate/INSTALL behavior and returns
`view.receipt_only=false`, `view.kind=gate_mode` for those ordinary gates.

* PROMOTE with an active post-state requires the full current approved boot,
  actual INFO SERVER run ID and expected epoch; the exact expected active
  compatibility, contract and legacy-retirement records; all eleven stored
  guard-core fields equal to the request; the matching core hash, config,
  compatibility digest, cutover mode and run identity; matching freeze nonce
  across request/expected freeze/retirement; and absent candidate pair/freeze.
  Partial active residue never falls back to a fresh mutation. Success returns
  `receipt_only=true`, `kind="promoted"` before fresh inventory/drain gates.
  Stored guard approval time is validated/preserved, not refreshed.
* MARK's only boot exception is a complete **planned** record for that same
  actual process/epoch, exact pending nonce and process-stop digest, empty
  consumed nonce, and unchanged normal active authority controls. Success
  returns `receipt_only=true`, `kind="planned_shutdown"`. No other operation
  accepts planned boot. The MARK planner must still validate the full sorted
  run list and required run/inventory/drain conditions before returning replay.

These are authority-layer receipts, not substitutes for complete admin planners.
PROMOTE replay compares every surviving authority record and all 13 semantic
fields. The removed freeze's process-stop digest/creation time are not stored in
the guard and cannot independently be recovered/compared after promotion;
the expected freeze is still fully decoded and nonce/manifest/contract bound.
This does not invent proof of process stops or authorize new evidence: the
existing exact trusted-tool artifact boundary remains, and the result cannot
write. Do not add fields, provisional artifacts, or fixture bypasses to fill
that information gap.

Gate installs a **private**, irreversible receipt-only restriction. Plan.add,
Plan.assess and Plan.seal all enforce an empty plan, including a plan created or
assessed before the gate. Changing `view.receipt_only` or a public ctx property
cannot clear it. Empty-plan sealing still requires a registered reply validator.
Normal and replay paths use only the usual seven-field prefix; no client flag
selects or grants a replay mode.

### Numeric and memory observations

`Identities.redis_score` retains finite native score parsing and raw score text.
For lexically valid scientific notation with an integer mantissa it passes an
equivalent dotted mantissa to tonumber (`1e-1` -> `1.0e-1`). This addresses
gopher-lua 1.1.1's dot-dependent ParseFloat selection without changing native
Lua's value, canonical `Identities.score` grammar, or stored `.score_text`.
Finite detection uses self-subtraction rather than math.huge: gopher-lua 1.1.1
sets that constant to MaxFloat64 rather than native Lua's infinity. Thus finite
boundary values are not accidentally rejected. A negative parsed zero retains
its sign (gopher-lua's integer branch otherwise loses the sign of plain `-0`);
nonzero mantissas are never arithmetically scaled. Signed plain/scientific zero,
subnormals, underflow, finite boundaries and overflow are tested without skips.

`Memory.observe(ctx, true)` additionally requires a canonical nonnegative
`lazyfree_pending_objects` from INFO MEMORY. It uses the same cached **single**
INFO result as ordinary admission, even if called after `observe(ctx)`.
Missing/malformed/duplicate lazyfree data fails closed; production never assumes
zero. The planner explicitly requires the returned value to be zero where
required. The in-memory facade supplies a real `lazyfree_pending_objects:0`
field by default and supports nonzero/invalid reply tests. No real-Redis
allocator, fixture, ACL or operational evidence is claimed.

## Run/admin integration contract (API revision 2)

Registration and key-plan metadata do **not** implement the corresponding
transitions by themselves. The implemented Run/Job/output modules and operation
fragments supply their semantic validation and planning; this core never
installs permissive placeholder semantic validators.

### Registration (before the first Context.open)

```
Schemas.register(name, {names={...}, bounds={...}}, validator) -> true/code
Reply.register(operation, validator)                         -> true/code
```

Schema names available for one-time trusted-source registration are exactly
`run`, `job`, `reservation`, `rate_scope`, `stage_meta`, `final_page`,
`final_image`, `image_manifest`. Existing authority/policy-group schemas cannot
be overwritten. Names/bounds are copied. Bounds are bytes, ordered with fields;
at most 128 fields, and their computed maximum framed RECORD must fit 16 MiB.
`Schemas.get` returns a copy, including `maximum` framed bytes. Registered
codecs use this bound (not the 16 KiB authority-record bound).

The pure validator is `validator(v,n) -> true` or `nil, CODE`. Before invoking
it, core checks **all** fields for presence, byte bounds and UTF-8. `n` contains
only successful canonical unsigned-decimal parses; there is no nil-to-zero
conversion. The validator must explicitly require numeric fields, ranges,
constants, identifier forms, and all schema relations. It gets private copies;
changing them cannot change the returned `{schema,fields,v,n}` projection.
Callback errors/unknown rejection codes close to `INVALID_STATE`.

The reply validator is `validator(ctx,status,tail) -> true` or `nil, CODE`.
Register once for a known prefixed operation (BOOT stays isolated). INSTALL's
validator cannot be replaced. Core always requires a dense array of 0..7 bulk
strings, invokes the validator before sealing, and returns a copied
`{status,ctx.now_text,...tail}`. Plan.seal accepts/revalidates 2..9 scalars.
Callbacks are trusted source code, never request data or sealed descriptors.
Context.open closes both registration windows after its first TIME read.

### Run key plans: derive inside the clocked decoder

```
local spec, code = CJ.Wire.run_spec(operation, ordered_semantic_fields,
                                    optional_tail_assertions)
local ctx, code = CJ.Context.open(spec, KEYS, ARGV)
```

`run_spec` performs source-metadata checks only. **Do not derive keys from ARGV
in handlers before open.** Inside open, TIME is first, then exact RESP size,
scalar/tail framing and lexical validation, then ID-checked key derivation and
exact key comparison. Bad/missing/noncanonical run/job IDs still see one TIME.
Alternatively supply a source-owned `spec.key_plan` below instead of
`key_names/key_values`; do not supply both. `run_spec` fixes 2 MiB admission.

| key_plan | Operations | Exact layout/count |
|---|---|---|
| `run` | CREATE_RUN, BEGIN_RUN_AUDIT, SEAL_RUN, CANCEL_RUN | AUTH + RUNS + RUN = 38 |
| `activate` | ACTIVATE_RUN | AUTH + RUNS + RUN + LEGACY = 43 |
| `maintenance` | PROMOTE_DUE, RECOVER_EXPIRED, CANCEL_BATCH, FINALIZE_RUN | AUTH + RUNS + active_leases + stage_expiry + stage_slots + rate_scopes + RUN = 42 |
| `archive` | ARCHIVE_RUN | AUTH + RUNS + active_leases + stage_expiry + stage_slots + RUN + DOWNSTREAM = 49 |
| `run_records` | ENQUEUE_BATCH, AUDIT_RUN_BATCH | AUTH + RUNS + RUN + JOB*n = 38+n |

The operation/key-plan mapping is closed. IDs are validated **before** any key
concatenation. CREATE defaults to `policy_group_count`, 1..64 six-field binary
group records; ENQUEUE to `record_count`, 1..500 complete nine-field source
records; AUDIT to the same source record order, 0..100. `run_spec` automatically
sets the exact tail config and 16384-byte per-record bound. An optional config
may only assert matching values (`tail`, `count_field`, `minimum`, `maximum`,
`record_fields`, `record_limit`), not enlarge or change framing. Other listed
operations have no tail. Operation modules still validate semantic scalars,
group digests, URLs/source bindings and persisted state.

`ctx.keys` exact string names:

* AUTH unchanged: `durability`, `active_compatibility`, `active_contract`,
  `candidate_compatibility`, `candidate_contract`, `commit_guard`,
  `legacy_retirement`, `admin_freeze`.
* RUNS: `runs`, `active_runs`, `unarchived_runs`.
* Optional globals: `active_leases`, `stage_expiry`, `stage_slots`, `rate_scopes`.
* RUN, in exact wire order: `run`, `run_jobs`, `run_job_order`, `run_ready`,
  `run_ready_at`, `run_leased`, `run_leased_at`, `run_delayed`,
  `run_commit_backpressure`, `run_completed`, `run_dead`, `run_cancelled`,
  `run_group_limits`, `run_group_rate_scope_ids`, `run_group_scope_ids`,
  `run_group_concurrency`, `run_group_interval_ms`, `run_group_started`,
  `run_group_pending`, `run_group_active_started`, `run_group_open_jobs`,
  `run_audit_group_counts`, `run_retry_reason_counts`,
  `run_recovery_outcome_counts`, `run_disposition_reason_counts`,
  `run_visited_depth`, `run_visited_urls`.
* LEGACY: `legacy_queue`, `legacy_urls`, `legacy_depths`,
  `legacy_spider_queue`, `legacy_signal_queue`.
* DOWNSTREAM: `pages_queue`, `pages_processing`, `pages_dead`, `images_queue`,
  `images_processing`, `images_dead`, `pages_owner`, `images_owner`.
* Record jobs: `record_job_1` ... `record_job_n` (1-based submitted order).
  Strictly increasing 64-hex job IDs; no sorting/deduplication of bad input.

Additional convenience projections, **not extra wire keys**:
`ctx.keys.run_keys` (27 strings), `ctx.keys.job_keys` (ordered repeated keys),
`ctx.keys.jobs_by_id` (job-ID -> key). The latter two are empty for non-record
plans. All actual wire keys are still in `ctx.allowed`.

### Bounded collection receipts

```
Read.cardinality(ctx,key,kind,maximum) -> fact/code
Read.members(ctx,key,kind,requested_members,maximum,member_bytes) -> fact/code
Read.all_members(ctx,key,kind,maximum,member_bytes) -> fact/code
Read.hash_fields(ctx,key,fields,max_fields,field_bytes,value_bytes) -> fact/code
Read.dynamic_hash(ctx,key,max_fields,field_bytes,value_bytes) -> fact/code
Read.page(ctx,key,kind,offset,limit,maximum,member_bytes) -> fact/code
Read.lex_page(ctx,key,exclusive_after_or_empty,limit,maximum,member_bytes) -> fact/code
Read.ttl(ctx,key) -> fact/code
```

Every context-bound entry checks prepare phase; every Redis read passes that
check again. TYPE precedes HLEN/SCARD/ZCARD/LLEN; cardinality bounds precede
membership/enumeration/page reads. `cardinality` supports hash/set/zset/list,
maximum <=100000; dynamic/hash-selected fields <=20000. `members` supports
set/zset and at most 10000 explicit requested members. Dynamic-hash field names
are byte-sorted; all selected HSTRLEN bounds precede chunked HMGET (256 fields
per call), and absent empty-valued fields remain distinguishable from empty
bulk strings. `absent` now also supports set/zset/list.

Facts have `exists`, actual `kind` (`none` for absent), `key`, `complete`, and
`count` when cardinality is known. Hash `.v[field]` is string or explicit false;
set/zset `.members[member]` is true/false; zset `.scores[member]` is the parsed
native finite double or false, `.score_text[member]` the native Redis spelling
or false. **nil means unrequested, unless complete=true proves its absence.**
Partial reads merge into private receipts and become complete only when all
present members are covered. Caller mutations cannot change cached evidence.
TYPE alone still does not authorize allocation accounting. PTTL receipts expose
`ttl_ms` (-1 persistent, -2 absent, otherwise nonnegative exact integer).

`all_members` returns `.ordered` (set byte order; zset score then byte order).
`dynamic_hash` returns `.names`. `page` supports list/zset, zero-based offset,
1..500 results and `.ordered`; lists may contain duplicates. `lex_page` is an
exclusive byte cursor over a score-zero job_order index and checks each selected
score, not a proof about every unselected member. Empty cursor starts at `-`.
No scan or implicit empty fallback; wrong types/missing receipts reject.

Redis has no field/member-length probe for HKEYS/SMEMBERS/ZRANGE/LRANGE.
Cardinality/page counts are bounded before retrieval and returned byte lengths
are checked, but hostile stored name/member bytes can still make the initial
reply large. Approved-writer/retained-state bounds remain necessary.

`Identities.score(text)` parses the exact section 3 canonical score grammar
(six fractional digits, [-1000,10000]); `redis_score(text)` accepts finite native
decimal/exponent representations, including Redis's 17-digit forms. Handlers
must compare numeric scores to independently validated canonical score_text.
`zadd_score(text)` admits a canonical score or an exact unsigned timestamp.

### Extended planner and narrow safety admission

Revision-2 whitelist: HSET, SET, UNLINK(single key), HDEL, SADD, SREM,
ZADD(no options), ZREM, RENAME(distinct keys, **absent destination only**),
PERSIST. See the rev4 command and memory-policy extensions above for later APIs.

`Plan.add` validates and copies each complete logical command before splitting:
at most 1002 string arguments, including the command and key, with
`sum(#argv[i]) <= 5373952` bytes for `CJ2_STAGE_PAGE_BLOB` and
`sum(#argv[i]) <= 2097152` bytes for every other operation. These existing
descriptor bounds count raw argument bytes, not serialized RESP bytes or total
plan bytes. `Wire.request_size` counts the complete serialized EVALSHA request:
command, SHA, key count, all KEYS/ARGV, and RESP framing. `Wire.decode` separately
limits those serialized bytes to 5373952 for `CJ2_STAGE_PAGE_BLOB`, 524288 for
non-blob STAGE data calls, 65536 for `CJ2_COMMIT`, and 2097152 for other shared-wire
operations. Neither bound replaces the other or the field/blob limits; this
documents the implementation, not a change to protocol limits.

`Plan.add` rejects duplicate fields/members (LPUSH retains duplicates) and splits
multi-element commands into descriptors of at most 258 arguments. It never
truncates a 500-element batch. At most 4096 descriptors per plan: 500 jobs * five
writes = 2500 plus bounded shared updates.

Accounting uses original and call-position projected **private** receipts.
Selected field/member coverage is required; a cardinality alone cannot prove
absence. Identical values/scores cost zero; changed values/scores are counted
in full. Deletions give no credit; last-element removal makes the key absent,
and subsequent recreation incurs key/name/element growth again. RENAME carries
the receipt and TTL to its new position, charges both key names and one new
destination-key overhead, but zero moved payload/element bytes. PERSIST requires
a TTL receipt and charges zero payload growth. Unknown source/destination facts
or an existing rename destination fail before any write.

`coverage="ordinary"` remains default by explicit argument. The sole extra
coverage is `"cancel_run"`: one HSET on an existing, complete validated
`ctx.keys.run` schema, for operation CJ2_CANCEL_RUN, containing **exactly**
`state=cancelled`, `terminal_reason=ctx.request.v.reason`,
`cancelled_at_ms=ctx.now_text`, `last_activity_at_ms=ctx.now_text` (any order).
Prior state must be loading/auditing/sealed/active, prior cancelled time 0 and
reason none. Reason is operator_cancelled/source_cancelled/authorization_expired;
the run handler still proves its authorization/state semantics. No additional
calls, fields, new key/field, or mixed coverage can use this exception. Ordinary
admission is attempted first; only this eligible constant-size plan may then
use `U+S+64MiB+G<=M`, preserving commit headroom. Assessment additionally exposes
`admission` (`ordinary` or `cancel_run_safety`), backed by private proof. It cannot
be edited to grant coverage. No other operation can spend the safety reserve.

Registration modules belong after the shared modules/aliases and before the
operation fragment. The assembler's explicit `implemented(fragment, modules=(),
with_url=False)` recipe hook includes such source-owned modules in order;
no operation is added to canonical recipes merely because its file exists.
`modules` is an ordered tuple of `(CJ_namespace, relative_lua_src_filename)`.
Core namespaces cannot be overwritten. `with_url=True` inlines the
Unicode chunk as `local D=(function() ... end)()` and the URL chunk as
`CJ.URL=(function(P,D) ... end)(P,D)` before extension modules. Both dependency
factories and extension registration are tested without creating extra canonical
files. Recipe changes require review and tests of the actual fragments, not
filesystem discovery.

Use an explicit nil control value in raw-next loops:
`for k,v in next,values,nil do ... end`. This is ordinary Lua 5.1 semantics and
avoids gopher-lua 1.1.1 stale implicit iterator-control behavior seen in the
receipt/registration tests. Copies must not lose a first map field. Also evaluate
a value before reassigning the local from which it is parsed (as in ZADD score
projection).

These pure **module chunks** are assembled at build time, not loaded by Redis.
`primitives.lua` is inlined as `local P = (function() ... end)()`.
Each subsequent chunk is inlined as
`CJ.Name = (function() ... end)()` with lexical access to `P` and `CJ`.
No `require`, `dofile`, runtime source lookup, or global module registration.

Canonical `../lua/` contains exactly the 43 operation sources listed above,
not support modules or stubs. The assembler owns explicit dependency order and
exact source bytes; the bundle generator and Go factory own fixed source
identity and seal validation. Neither substitutes for operation conformance or
the separate real-Redis, image and operational acceptance gates.

## Assembly

Use Python 3.10+ for the maintenance workflow below (CI tests the verifier with
3.13). From repository root, check assembly, bundle pins, and shared vectors
without writing repository files:

```bash
python3 -B scripts/generate-crawl-jobs-v2-lua.py --check --require-complete
python3 -B scripts/generate-crawl-jobs-v2-bundle.py --check
python3 -B scripts/verify-crawl-jobs-v2-digests.py
```

The Lua check compares all 42 assembled sources byte-for-byte with their explicit
recipes and validates the BOOT passthrough. It reports
`COMPLETE SOURCE INVENTORY: 43/43`; `--require-complete` makes missing recipes
fatal rather than merely reporting incompleteness. BOOT is never rewritten.

Bundle identity uses exactly the literal 43 canonical files and the exact bytes
of `docs/crawl-jobs-v2.md`. The bundle tool also requires the existing
[`contracts/crawl-jobs-v2/digest-vectors.json`](../../../../../../contracts/crawl-jobs-v2/digest-vectors.json)
as input; it does not generate the whole fixture from scratch. Its check verifies
generated Go pins, canonical/document digest cases, and dependent guard
identities. Pins cover each operation/source name, Redis SHA-1, source SHA-256,
ordered source-set SHA-256, contract SHA-256 and bundle seal.
`AuthoritativeScriptBindingSet()` in
`../script_bundle.go` validates the embedded inventory against these fixed
literals and returns fresh private bindings/seal; callers supply no sources,
hashes or paths. `ContractSHA256()` validates that bundle and returns the fixed
contract pin, not a runtime document hash. Private SCRIPT LOAD helpers retain
the same sealed source/retry identity but perform no I/O or fresh transport
checks; dispatch remains unwired.

The verifier reads inputs and writes no fixtures or pins. Its `--print-computed`
mode emits diagnostic JSON but skips the normal final expected-result comparison;
do not use it as a passing verification run or as fixture regeneration.

For approved changes that require regeneration, use this order: Unicode first
**only if an approved pin/data change requires it** (see below), then Lua, then
bundle pins and fixtures. After any required Unicode work, run from repository
root without `--check`:

```bash
python3 -B scripts/generate-crawl-jobs-v2-lua.py --require-complete
python3 -B scripts/generate-crawl-jobs-v2-bundle.py
```

The Lua generator writes only the 42 assembled sources, never BOOT. The bundle
generator writes stale Go pins to `../script_bundle_generated.go`, updates
selected canonical/document digest and dependent guard cases in the existing
fixture, and adds missing canonical-bundle negative cases. It preserves unrelated
fixture cases and formatting; it does not write Lua or the normative document.
Hashes cover the entire canonical source, including comments; no timestamps,
environment values, or filesystem enumeration determine source identity.
Recipes and tests, not merely files in a directory, define assembly. URL and
Unicode support stay in their own chunks; INSTALL has no URL dependency and
does not inline them. These checks establish source identity, not M4 acceptance
or runtime activation.

## Unicode and Python maintenance

The independent [`scripts/crawl_jobs_v2_url.py`](../../../../../../scripts/crawl_jobs_v2_url.py)
helper validates already-canonical V1 URL identities for dormant Crawl Jobs V2
conformance. It is not the active
[Seed Importer normalizer](../../../../../../services/seed-importer/url_identity.py)
and does not authorize network requests. You need Python **3.10+** because the
helper uses `dataclass(slots=True)`; the verifier and Python unit suite use only
the standard library and checked-in inputs.
[Required-checks CI](../../../../../../.github/workflows/required-checks.yml)
tests them with Python **3.13**.

[`unicode-provenance.json`](unicode-provenance.json) records the exact pins:
**Go 1.25.13**, **golang.org/x/net v0.58.0**, **golang.org/x/text v0.41.0**, and
**Unicode 15.0.0**. Retain the upstream notices in [`licenses/`](licenses/).
The Python helper reads [`unicode_data.lua`](unicode_data.lua) from one fixed
repository path relative to the helper, verifies its full-file SHA-256 and size,
and parses five literal tables. It never executes the Lua trailer or falls back
to host Unicode tables, Python's IDNA codec, or executable Lua. Missing or changed
data fails closed, even for an ASCII-only verification run.

[`tools/generate-unicode/main.go`](../tools/generate-unicode/main.go) checks or
regenerates the data and provenance. Put the Go 1.25.13 binary on `PATH` first:
`GOTOOLCHAIN=local` deliberately disables automatic toolchain selection. With
the pinned modules already cached, run from `services/spider`; set
`UNICODE_WORK_DIR` to an existing private temporary parent directory first:

```bash
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off go run -mod=readonly \
  ./internal/database/crawljobsv2/tools/generate-unicode \
  -work "$UNICODE_WORK_DIR" -check
```

`-check` compares outputs without writing repository files; it still creates
private temporary files and may populate the Go build cache. Only for an
approved regeneration, omit `-check` to write `unicode_data.lua`,
`unicode-provenance.json`, `licenses/Go-BSD-3-Clause.txt`, and
`licenses/Go-PATENTS.txt`. `-oracle <path>` also writes a raw oracle file, even
with `-check`. `-acquire-licenses` is a separate explicit network/write mode,
not part of offline checking or normal regeneration.

The generator hashes its own source, including comments, into provenance.
Keep documentation-only edits here rather than in that file. Unicode generation
does not update the Python helper's identity/hash/inventory pins: review those
together with any approved data change, then follow the Lua -> bundle/fixture
order in [Assembly](#assembly). Do not repin unexpected drift to make checks pass.

## Contract and conventions

All fallible APIs return `value, nil` or `nil, CODE`, where CODE is a closed
section 9.2 code without prefix. Predicates (`integer`, `hex`, `digest`, `image`,
`positive`, `group`, `Context.preparing`, `Context.can_read`, `Context.can_write`)
return booleans. No missing fact means
zero/empty/absent. `Context.reject(code)` builds a redacted protocol error and
maps an unknown code to `INVALID_STATE`. Expected preparation Redis errors are
closed; unexpected executor errors propagate and do **not** roll back writes.

Module order: Identities, Schemas, Wire, Context, Read, Gate, Memory, Plan.
The assembler also supplies `CJ.ReadProjectedState = CJ.Read.project` and
`CJ.Reply = CJ.Context.Reply`.

### Context and wire

* `Context.open(spec, KEYS, ARGV) -> ctx/code`: exactly one TIME, including
  rejected requests. `ctx.now_ms` and `ctx.now_text` are canonical Redis time;
  `ctx.phase` starts `prepare`. No other helper calls TIME.
* Source-owned `spec`: `operation`, ordered `fields`, ordered `key_names` and
  exact derived `key_values`, `request_limit`, `tail` (`none`, `records`,
  `run_ids`). A tail adds `count_field`, `minimum`, `maximum`; records add
  exact `record_fields`, `record_limit`. The spec is **not** client data.
  `Wire.install` is the complete INSTALL spec. `Wire.run_spec`, `Wire.admin_spec`
  and `key_plan` provide the listed clocked layouts; handlers perform explicit
  semantic validation and register a response validator. Structural decoding does
  not replace operation-level semantic, state or replay validation.
* Result: `ctx.operation`, `ctx.keys.<name>`, `ctx.allowed` (wire key set),
  `ctx.request.v` (semantic scalar strings), `.n` (only explicitly parsed
  count scalars), `.records` (dense `{fields,v}` binary RECORD projections),
  `.repeated` (strictly ordered run IDs), `.gate` (validated typed artifacts),
  `.bytes` (exact EVALSHA RESP size). INSTALL has 8 KEYS, 31 ARGV, 24 semantic
  scalars, including **21**, not 23, compatibility-marker fields.
* `Wire.request_size(keys,args)`, `Wire.record(bytes,names,maximum,text_values)`
  are pure. `record` supports binary values with explicit `false`; absent gate
  sentinels are zero-byte strings, not zero-field RECORDs. Framing validation
  alone does not establish URL, run, job, or output semantics.
* `Wire.gate(operation,args)` implements the closed 42 prefixed-operation mode
  inventory (BOOT is isolated), full authority record decoding/cross-binding,
  and candidate before/after-retirement restrictions. Inventory is metadata,
  not transition coverage. The prefix is unchanged for special replay requests;
  `Gate.check` (not Wire.gate) proves the revision-3 stored post-state and installs
  the empty-plan restriction. Admin planners still own their complete predicates.

### Schemas and pure identities

* `Schemas.get(name) -> {names,bounds,maximum}/code` returns copied source metadata.
* `Schemas.project(name,values) -> {schema,v,n,fields}/code` selects and fully
  validates a complete named schema, including state-dependent relations.
  Extra entries in the input values map are not projected (INSTALL supplies its
  scalar map); fixed Redis hashes and binary records enforce exact field count.
* `Schemas.decode(name,RECORD)`, `Schemas.encode(projection)` enforce exact
  ordered binary framing and the same semantics. Implemented: durability,
  compatibility artifact/marker (including artifact RECORD SHA-256 agreement),
  guard core/stored guard, legacy retirement, admin freeze, first request start,
  and the six-field policy group. Provisional zero-evidence artifacts reject.
* `Schemas.groups(binary_records) -> {ordered,by_id,count,digest}/code`:
  1..64 ordered unique group IDs, exact tuples, derived group scope and map hash.
  Run handlers use this pure projection; it does not authorize
  Redis group state or implement run validation.
* `Identities.framed(domain,values)`, `.global_scope()`, `.group_scope(id)`
  use P's SHA-256 and binary frames. Operation modules and `CJ.URL` supply the
  other control-identity and URL semantics. `dense`, `info` provide bounded structural
  validation. Other record schemas reject until explicitly registered by their
  trusted semantic module; registration alone is not implementation evidence.

### Reads and gates

`ReadProjectedState(ctx,needs) -> view/code` takes a dense array of explicit
typed requests:

```
{name="marker", key=ctx.keys.candidate_compatibility,
 kind="hash", schema="compatibility_marker"}
{name="contract", key=ctx.keys.candidate_contract,
 kind="string", maximum=64, digest=true}
{name="absent", key=ctx.keys.active_compatibility,
 kind="absent", expected_type="hash"}
```

Each result has `exists`, `kind`, `key`; present hashes add typed schema
projection fields, present strings add `value`. Absent is an explicit complete
fact, not nil. TYPE precedes all reads; exact HLEN and every known HSTRLEN bound
precede fixed-width HMGET. STRLEN precedes GET. Missing empty-valued fields fail.
No HGETALL. Complete/selected receipts are privately cached, public projections copied;
`ctx.selected` is diagnostic, not ledger authority. `Read.snapshot(ctx,key)`
fails for wholly unrequested state (TYPE alone is not allocation coverage).
Partial receipts explicitly track the selected facts; Plan rejects use of an
unrequested field/member unless complete=true establishes absence.
`Read.fixed_hash`, `.string`, `.absent`, `.key_type` expose the same readers.

`Gate.check(ctx) -> view/code` validates the full 12-field boot hash, actual
INFO SERVER run_id, epoch and approved state, then the exact relevant authority
records/absence conditions. It does not invent an ongoing rehearsal TTL or
claim to inspect process-stop artifacts not supplied on the wire. Active and
candidate common-state gates compare every value of the validated artifacts;
INSTALL's boot-only gate admits candidate/freeze **for its explicit planner's
post-state comparison**, not for automatic repair. INSTALL requires absent
legacy-retirement evidence as well as active pair and commit guard. It never
reads the five legacy data keys.

### Ledger, memory, seal and execution

* `Plan.new(ctx) -> plan/code` returns an opaque handle.
* `Plan.add(plan,argv,coverage) -> true/code` copies and, if needed, splits the
  logical descriptor under the [planner bounds](#extended-planner-and-narrow-safety-admission)
  above, including the blob-specific summed-argument limit and rev4 extensions.
  Keys need validated wire or authenticated derived-write grants. No arbitrary
  numeric G/free coverage; use the documented rev4 policy/unit rules or the
  narrowly validated `cancel_run` mode.
* `Plan.assess(ctx,plan) -> assessment/code` simulates those commands in order
  from complete or explicitly selected private receipts. Missing facts/wrong types fail.
  Identical values cost zero; changed values cost the **full replacement**;
  field/key names cost only when new, with 256/1024 element/key overhead and
  the 3x logical byte multiplier. UNLINK gives no credit; recreate is charged.
  The plan locks against additions after assessment. The assessment exposes
  `growth,new_logical_bytes,new_keys,new_elements,admission` for inspection, but a private
  identity/proof prevents fabrication or modification from authorizing writes.
* `Memory.observe(ctx)` reads bounded stage_slots and one INFO MEMORY. It is
  not an admission API. Assessment enforces checked exact-integer arithmetic:
  `U + S + 67108864 + 16777216 + G <= M`, M nonzero. Stage slots are the
  common-memory fixed-key exception outside INSTALL's eight wire keys; BOOT
  does not use this core. Slot grammar is exactly commit hex field and
  `remaining:run:job:positive_fence:abort_unlinked_keys`, remaining 0..50331648,
  abort count 0..73. This projection establishes arithmetic bounds, not stage
  ownership/lifecycle authority. HLEN<=4 precedes HKEYS; every value length is
  bounded before HMGET. **Redis has no hash-field-name length probe**: a hostile
  field name can allocate large HKEYS reply bytes despite the cardinality bound.
  Approved-writer/retained-state trust is still required; do not claim arbitrary
  hostile datastore byte safety. Known fixed hashes avoid that enumeration.
* Empty plans (identical INSTALL replay) skip slot/memory/ACL admission and
  issue zero writes. Replay still validates inputs, boot, and complete markers.
* `Reply.build(ctx,status,tail) -> bulk_string_array/code` implements INSTALL's
  two statuses/digest tails by default. Other operations require one-time,
  source-owned registration; an unregistered operation fails closed.
* `Plan.seal(ctx,plan,assessment,reply) -> execution/code`: validates assessment
  and reply, prebuilds all execution tables, ACL-checks every exact descriptor,
  then irreversibly changes ctx to `sealed`. All context-bound read/build APIs
  reject after sealing. Pure primitives must not be called by the executor.
  The returned execution object is consumed immediately by reviewed source;
  Lua tables are not an adversarial isolation boundary against modified source.

The only permitted tail is a numeric loop of
`redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))`, then
`return execution.reply`. No callbacks, helper calls, late key derivation,
memory/reply calculation, or mutation-result branches. A later Redis error
leaves earlier writes applied and propagates as an integrity incident. This
core claims no rollback, crash durability, or actual allocator upper bound.
