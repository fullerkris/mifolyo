-- Request/Rate schemas, receipt checks, and six worker preparation paths.
-- The pure/receipt APIs below still perform no Redis calls or writes. Worker
-- prepare_* methods select bounded Read facts and build inert shared Plan calls;
-- only the six ops/cj2_* fragments contain the fixed mutation executor.
-- Assemble after P, core, Unicode15/CJ.URL, CJ.Run and CJ.Job, before open:
--   CJ.Request = (function() <this chunk> end)()
--   CJ.Rate = CJ.Request.Rate
-- Registers exactly reservation (34 fields) and rate_scope (14 fields).
-- All fallible APIs return value,nil or nil,closed_code. No direct Redis mutation.
--
-- Pure API:
--   validate(v), Rate.validate(v) -> Schemas projection {schema,v,n,fields}
--   target_digest(id,url), token_digest(lease), Rate.origin_scope(exact_origin)
--   intent(values,run,job,initial) -> {v,n,fields,reservation_id,token_digest,
--                                    origin,decision_fields}
--     values selects the 23 RESERVE semantic scalars (fence, not lease_fence).
--     run={run_id,v,groups={ordered={policy_group projections}}}; job={v=...}.
--     Revalidates Run/Job, complete pinned groups, source binding and decision
--     using ONLY job.depth. Never trusts .n, .by_id, .digest or values.depth.
--     initial is a required boolean: claim/pre-first-start replacement rules.
--   build_intent(run,job,lease,kind,url,group_id,ordinal_text,initial)
--     derives those same scalars from semantic inputs. lease has the five
--     lease_identity fields. Neither pure API authenticates caller objects.
--   pending_record(ctx,run,job,values,initial,expiry_text) -> complete record
--     uses Context's clock; the planner owns the prescribed lease deadline,
--     ordinal/fence increments, absence, budgets and atomic aggregate changes.
--   Rate.tighten(record,concurrency_text,interval_text,policy_digest,now_text)
--     -> complete unchanged-membership min/max projection, NOT admission.
--
-- Receipt API (prepare only; view.ctx is the sole context; public facts ignored):
--   check_receipt(view,id) -> full reservation projection + exists/key/kind,
--     token_digest, origin, live_state, logical_expired, tombstone_until_ms.
--     Requires private fixed_hash reservation + ttl; owning fixed_hash job/run;
--     complete five immutable run group-map receipts. Authenticates decision
--     depth/policy without forcing a current lease, B/G or current membership.
--     It preserves original START snapshots, including on a newer fence.
--     The handler must still compare the decoded request's full lease identity
--     to the stored run/job/owner/token/fence before choosing a replay response.
--     A valid receipt alone says nothing about the caller or network success.
--   check_live(view,run,job,id) -> same record + scopes, run, job, eligible_now.
--     Parent first validates the aggregate Run ledger (Run.load) and gates.
--     This helper owns request-related counters, not audit/reason/retention or
--     unrelated jobs' aggregate state reconciliation.
--     Additionally requires every Job.check selected index receipt, complete
--     group_started/pending/active_started/open_jobs maps, and the three Rate
--     checks below selecting id. run/job .v must equal private records; their
--     convenience fields are not evidence. Validates current ownership, latest
--     ordinal, per-fence snapshots, counters, source and charged groups, lease
--     alignment, document witness and all three rate memberships. Expired live
--     records remain valid for recovery; eligible_now is only lease/run/auth
--     eligibility, NOT rate admission or an I/O permit. Cancellation/expired
--     authorization do not prevent validating capacity release.
--   Rate.check_scope({ctx=ctx,reservation_ids={...}},scope_id)
--     Requires private scope fixed_hash (or explicit absence), ttl, inventory
--     membership and active/pending/started cardinalities + ttl. Every requested
--     or privately selected member needs coverage in all three indexes (complete
--     receipts prove absence) and a private reservation+ttl receipt, or explicit
--     record absence for a not-yet-admitted ID. Selected tombstones are checked.
--     Does not claim to authenticate unselected reservations. Returns full scope
--     record + indexes, inventory, complete_membership; explicit unmaterialized
--     absence is {exists=false,...}, never invented zero-valued schema fields.
--
-- Receipt checks never fetch missing evidence. The worker section selects it
-- explicitly and delegates every key permission and memory decision to Core.
-- Worker assembly additionally requires CJ.StageOutput/CJ.Stage and Core rev4:
--   Wire.worker_spec; Context.bind_request, bind_held_request, bind_held_scopes,
--   bind_scope_reservations, bind_stage; Plan absolute expiry/safety support.
-- RENEW's stale-rejection path needs READ-ONLY held-request inspection even for
-- an expired/mismatched caller lease; only a matching live caller may gain writes.
-- FINISH/CANCEL select Plan.set_policy(plan,"safety") before adding ordinary
-- descriptors; renewal and claim/reserve/start cannot spend that reserve.
-- Renewal additionally uses Stage.select/check_renewal_state. Its inspection
-- validates stored expired ownership but is NEVER a Plan spending capability.
--
-- Reusable effects API (no flushing/sealing/execution inside either helper):
--   load_live(ctx,run,job) -> check_live's complete reservation/scopes projection;
--     false,nil ONLY for the exact empty pointer of a privately checked Job;
--     nil,code for missing/contradictory records or unavailable read grants.
--     run is Run.load from this ctx; job is a complete Job projection. Re-selects
--     job + memberships, compares .v, and uses core closed held/peer bindings.
--     Empty public fields, .n edits and caller-created facts are not evidence.
--   plan_terminal(ctx,plan,delta,run,job,state,coverage) -> effects/code
--     state=cancelled requires pending; finished requires started; both require
--     the exact caller lease and now<expiry (run cancellation/auth expiry is OK).
--     expired is RECOVER-only, requires a due held record and an opaque recovery
--     unit. coverage is exactly "ordinary" or the caller's core unit, NEVER G.
--     Caller selects policy before any descriptors; load_live must precede the
--     safety policy and recovery_unit proofs (recovery mode itself may be earlier).
--     Adds reservation HSET + PEXPIREAT(now+86400000), exact three-scope removals,
--     cumulative scope counts and inventory updates. Accumulates the charged
--     group's pending/active-started and Run reservation decrement + activity in
--     the PROVIDED delta, passing coverage to Run.accumulate/Run.set unchanged.
--     Leaves cumulative starts/creations, B/G and SOURCE group_open_jobs alone.
--     NO job HSET: caller composes job_fields into its ONE job/outcome write.
--     NO Run.flush/Plan.assess/seal: caller flushes once after all batch effects.
--     One shared plan/delta per run, all terminal scope effects through this API;
--     private per-plan projections compose shared scopes, and duplicate Q rejects.
--     Discard plan AND delta on any failed helper (no rollback of inert builders).
--     Recovery callers omit flush's blanket coverage argument; Run owns field
--     attribution and core independently authenticates units, effects and growth.
--
-- effects = {changed=true,reservation_id,previous_state,state,charged_group_id,
--   source_group_id,job_key,job_fields={active_reservation_id="",updated_at_ms},
--   record=<complete terminal reservation>,scopes={global,group,origin} post-
--   projections,tombstone_expires_at_ms=<canonical text>,run_changes={run,maps}}.
-- Only job_fields still need applying. Record/scope descriptors and run_changes
-- are ALREADY planned/accumulated; returned diagnostics must not be re-applied.
--
-- Historical blocked RETRY reasons belong to Job's existing pinned-client trust
-- boundary. RETRY supplies no blocked target/group/scope tuple or denial receipt;
-- Request provides no invented historical proof or outcome-denial hook.
-- register_worker(op) precedes Context.open. prepare_claim/reserve/start/renew
-- and prepare_terminal return a sealed execution or closed error, not raw Redis
-- writes. Run.accumulate/flush own aggregate post-state checks. No pruning,
-- permission fallback, source recipe or source-set/bundle authority is supplied.
-- Integration is NOT complete merely because a fragment exists: peer scope read
-- grants, request safety coverage and expired-stage validation must be present
-- and pass the actual-handler tests. See the enabled worker tests, not recipes,
-- for the current acceptance status. No completion of all 43 operations is claimed.
local Request, Rate = {}, {}
local I, S, R, C, URL = CJ.Identities, CJ.Schemas, CJ.Read, CJ.Context, CJ.URL
local prefix, MAX, DAY = "mifolyo:crawl:v2:", P.limits.max_integer, 86400000
local function words(s)
    local a = {}; for w in string.gmatch(s,"%S+") do a[#a+1] = w end; return a
end
local function plain(t) return type(t) == "table" and getmetatable(t) == nil end
local function one(v, ...)
    for _, x in ipairs({...}) do if v == x then return true end end; return false
end
local names = words([[protocol_version reservation_id run_id job_id owner_id lease_token lease_fence
request_ordinal state request_kind target_url_id canonical_target_url target_digest crawl_policy_sha256
policy_decision_sha256 group_id rate_scope_id global_scope_id group_scope_id origin_scope_id global_concurrency
global_interval_ms group_concurrency group_interval_ms origin_concurrency origin_interval_ms created_at_ms
started_at_ms terminal_at_ms delivery_attempts_after_start job_starts_after_start run_starts_after_start
group_starts_after_start expires_at_ms]])
local bounds = {1,64,32,64,32,64,16,16,9,15,64,2048,64,64,64,128,32,64,64,64,
    16,16,16,16,16,16,16,16,16,16,16,16,16,16}
local numeric = words([[lease_fence request_ordinal global_concurrency global_interval_ms group_concurrency
group_interval_ms origin_concurrency origin_interval_ms created_at_ms started_at_ms terminal_at_ms
delivery_attempts_after_start job_starts_after_start run_starts_after_start group_starts_after_start expires_at_ms]])
local rate_names = words([[protocol_version scope_id scope_kind scope_witness effective_concurrency
effective_interval_ms next_allowed_ms last_started_at_ms active_count pending_count started_count
concurrency_source_sha256 interval_source_sha256 updated_at_ms]])
local rate_bounds = {1,64,6,2054,16,16,16,16,16,16,16,64,64,16}
local rate_numeric = words([[effective_concurrency effective_interval_ms next_allowed_ms last_started_at_ms
active_count pending_count started_count updated_at_ms]])
local intent_names = words([[run_id job_id owner_id lease_token fence request_ordinal request_kind target_url_id
canonical_target_url target_digest crawl_policy_sha256 policy_decision_sha256 group_id rate_scope_id global_scope_id
group_scope_id origin_scope_id global_concurrency global_interval_ms group_concurrency group_interval_ms
origin_concurrency origin_interval_ms]])
local decision_names = words([[request_kind target_url_id target_digest depth group_id rate_scope_id global_scope_id
group_scope_id origin_scope_id global_concurrency global_interval_ms group_concurrency group_interval_ms
origin_concurrency origin_interval_ms]])
local identity_names = words([[run_id job_id lease_fence lease_token request_ordinal request_kind target_url_id
target_digest crawl_policy_sha256 policy_decision_sha256 group_id rate_scope_id global_scope_id group_scope_id
origin_scope_id global_concurrency global_interval_ms group_concurrency group_interval_ms origin_concurrency origin_interval_ms]])
local policy_maps = {{"group_limits","request_start_limit"},{"group_rate_scope_ids","rate_scope_id"},
    {"group_scope_ids","group_scope_id"},{"group_concurrency","concurrency"},{"group_interval_ms","interval_ms"}}

function Request.target_digest(id, url)
    if not I.hex(id,64) then return nil, "INVALID_IDENTIFIER" end
    local target, code = URL.check_canonical(url,1)
    if not target then return nil, code end
    if target.url_id ~= id then return nil, "URL_ID_COLLISION" end
    -- URL identity deliberately still admits IP literals; only origins exclude them.
    return I.framed("mifolyo:request-target:v2",{id,url})
end
function Request.token_digest(lease)
    if not plain(lease) or not I.hex(lease.run_id,32) or not I.hex(lease.job_id,64) or
       not I.hex(lease.owner_id,32) or not I.hex(lease.lease_token,64) or not I.positive(lease.fence) then
        return nil, "INVALID_IDENTIFIER"
    end
    return I.framed("mifolyo:lease-token:v2",{lease.run_id,lease.job_id,lease.fence,lease.lease_token})
end
function Rate.origin_scope(origin)
    if type(origin) ~= "string" or #origin > 2054 then return nil, "INVALID_IDENTIFIER" end
    -- Go validateCanonicalOrigin's probe: no userinfo, path, query, fragment,
    -- brackets, omitted/zero-padded/default-port ambiguity or noncanonical host.
    local scheme, host, port = string.match(origin,"^(https?)://([^/:?#@%[%]]+):([0-9]+)$")
    if not scheme then return nil, "INVALID_IDENTIFIER" end
    local authority = host .. ":" .. port
    if (scheme == "http" and port == "80") or (scheme == "https" and port == "443") then authority = host end
    local derived = URL.derive_origin(scheme .. "://" .. authority .. "/")
    if derived ~= origin then return nil, "INVALID_IDENTIFIER" end
    return I.framed("mifolyo:rate:origin:v2",{origin})
end
local function reservation_id(v)
    local values = {}; for i, k in ipairs(identity_names) do values[i] = v[k] end
    return I.framed("mifolyo:request-reservation:v2",values)
end
local function immutable(v, n)
    if not I.hex(v.run_id,32) or not I.hex(v.job_id,64) or not I.hex(v.owner_id,32) or
       not I.hex(v.lease_token,64) or not I.positive(v.lease_fence) or not I.positive(v.request_ordinal) or
       n.request_ordinal > 100 or not one(v.request_kind,"robots","document","redirect","render_resource") or
       not I.digest(v.target_digest) or not I.digest(v.crawl_policy_sha256) or not I.digest(v.policy_decision_sha256) or
       not I.group(v.group_id) or not I.hex(v.rate_scope_id,32) then return nil, "INVALID_ARGUMENT" end
    local digest, code = Request.target_digest(v.target_url_id,v.canonical_target_url)
    if not digest then return nil, code end
    local origin = URL.derive_origin(v.canonical_target_url)
    if not origin then return nil, "INVALID_IDENTIFIER" end
    local scope = Rate.origin_scope(origin)
    if digest ~= v.target_digest or not scope or v.origin_scope_id ~= scope or
       v.global_scope_id ~= I.global_scope() or v.group_scope_id ~= I.group_scope(v.rate_scope_id) or
       v.global_scope_id == v.group_scope_id or v.global_scope_id == v.origin_scope_id or v.group_scope_id == v.origin_scope_id then
        return nil, "IMMUTABLE_MISMATCH"
    end
    if n.global_concurrency ~= 2 or n.global_interval_ms ~= 0 or not I.integer(n.group_concurrency,32) or
       n.group_concurrency == 0 or not I.integer(n.group_interval_ms,3600000) or
       n.origin_concurrency ~= n.group_concurrency or n.origin_interval_ms ~= n.group_interval_ms then
        return nil, "INVALID_ARGUMENT"
    end
    return origin
end
local function validate(v,n)
    for _, k in ipairs(numeric) do if n[k] == nil then return nil, "INVALID_NUMBER" end end
    if v.protocol_version ~= "2" or not I.digest(v.reservation_id) or
       not one(v.state,"pending","started","finished","cancelled","expired") then return nil, "INVALID_ARGUMENT" end
    local origin, code = immutable(v,n); if not origin then return nil, code end
    if reservation_id(v) ~= v.reservation_id then return nil, "IMMUTABLE_MISMATCH" end
    local c,s,t,e = n.created_at_ms,n.started_at_ms,n.terminal_at_ms,n.expires_at_ms
    local d,j,r,g = n.delivery_attempts_after_start,n.job_starts_after_start,n.run_starts_after_start,n.group_starts_after_start
    if c == 0 or e <= c or d > 3 or d > j or j > r or g > r or r > 10 or j > n.request_ordinal or
       (s == 0 and (d ~= 0 or j ~= 0 or r ~= 0 or g ~= 0)) or
       (s ~= 0 and (d == 0 or j == 0 or r == 0 or g == 0 or s < c or s >= e)) or
       (t ~= 0 and t < c) then return nil, "INVALID_ARGUMENT" end
    if (v.state == "pending" and (s ~= 0 or t ~= 0)) or (v.state == "started" and (s == 0 or t ~= 0)) or
       (v.state == "finished" and (s == 0 or t < s or t >= e)) or
       (v.state == "cancelled" and (s ~= 0 or t == 0 or t >= e)) or
       (v.state == "expired" and t < e) then return nil, "INVALID_ARGUMENT" end
    return true
end
local function validate_rate(v,n)
    for _, k in ipairs(rate_numeric) do if n[k] == nil then return nil, "INVALID_NUMBER" end end
    if v.protocol_version ~= "2" or not I.digest(v.scope_id) or not I.digest(v.concurrency_source_sha256) or
       not I.digest(v.interval_source_sha256) or n.effective_concurrency == 0 or n.effective_concurrency > 32 or
       n.effective_interval_ms > 3600000 or P.safe_add(n.pending_count,n.started_count) ~= n.active_count or
       n.updated_at_ms == 0 or n.last_started_at_ms > n.updated_at_ms then return nil, "INVALID_ARGUMENT" end
    local expected
    if v.scope_kind == "global" then
        expected = I.global_scope()
        if v.scope_witness ~= "global" or n.effective_concurrency ~= 2 or n.effective_interval_ms ~= 0 or
           n.next_allowed_ms ~= 0 then return nil, "INVALID_ARGUMENT" end
    elseif v.scope_kind == "group" then expected = I.group_scope(v.scope_witness)
    elseif v.scope_kind == "origin" then expected = Rate.origin_scope(v.scope_witness)
    else return nil, "INVALID_ARGUMENT" end
    if not expected or expected ~= v.scope_id then return nil, "IMMUTABLE_MISMATCH" end
    if v.scope_kind ~= "global" and n.last_started_at_ms ~= 0 then
        local deadline = P.safe_add(n.last_started_at_ms,n.effective_interval_ms)
        if not deadline or n.next_allowed_ms < deadline then return nil, "INVALID_NUMBER" end
    end
    return true
end
local ok, code = S.register("reservation",{names=names,bounds=bounds},validate)
if not ok then return nil, code end
ok, code = S.register("rate_scope",{names=rate_names,bounds=rate_bounds},validate_rate)
if not ok then return nil, code end
function Request.validate(v) return S.project("reservation",v) end
function Rate.validate(v) return S.project("rate_scope",v) end

local function policy(run, job)
    if not plain(run) or not I.hex(run.run_id,32) or not plain(run.groups) or not plain(job) then return nil, "INVALID_STATE" end
    local rr, jj = CJ.Run.validate(run.v), S.project("job",job.v)
    local count = I.dense(run.groups.ordered,64)
    if not rr or not jj or jj.v.run_id ~= run.run_id or not count or count == 0 then return nil, "INVALID_STATE" end
    local bytes = {}
    for i = 1, count do
        local entry = run.groups.ordered[i]
        local group = plain(entry) and S.project("policy_group",entry.v)
        if not group then return nil, "IMMUTABLE_MISMATCH" end
        bytes[i] = S.encode(group)
    end
    local groups = S.groups(bytes)
    if not groups or groups.count ~= rr.n.policy_group_count or groups.digest ~= rr.v.policy_group_map_sha256 then
        return nil, "IMMUTABLE_MISMATCH"
    end
    local binding = {run_id=run.run_id,v=rr.v,n=rr.n,groups=groups}
    local fields = {}
    for i,k in ipairs(words("job_id canonical_url score_text depth group_id rate_scope_id group_scope_id initial_origin_scope_id policy_decision_sha256")) do
        fields[i] = {k,jj.v[k]}
    end
    if not CJ.Job.source({fields=fields},binding) then return nil, "IMMUTABLE_MISMATCH" end
    return {run=binding,job=jj}
end
local function decision(v, depth)
    local fields = {}
    for i,k in ipairs(decision_names) do fields[i] = {k,k == "depth" and depth or v[k]} end
    local section = P.section("decision",{fields},16384)
    if not section then return nil, "INVALID_ARGUMENT" end
    return P.sha256(P.frame("mifolyo:policy-decision:v2") .. section), fields
end
local function initial_matches(v, j)
    return one(v.request_kind,"robots","document") and v.group_id == j.group_id and v.rate_scope_id == j.rate_scope_id and
        v.group_scope_id == j.group_scope_id and v.origin_scope_id == j.initial_origin_scope_id and
        (v.request_kind ~= "document" or (v.target_url_id == j.job_id and v.canonical_target_url == j.canonical_url))
end
function Request.intent(values, run, job, initial)
    if not plain(values) or type(initial) ~= "boolean" then return nil, "INVALID_ARGUMENT" end
    local binding, err = policy(run,job); if not binding then return nil, err end
    local v,n,fields = {},{},{}
    for i,k in ipairs(intent_names) do
        local x = values[k]
        if type(x) ~= "string" or #x > 2048 or not P.validate_text(x) then return nil, "INVALID_ARGUMENT" end
        v[k],n[k],fields[i] = x,P.parse_decimal(x),{k,x}
    end
    v.lease_fence,n.lease_fence = v.fence,n.fence
    local origin, failure = immutable(v,n); if not origin then return nil, failure end
    local group = binding.run.groups.by_id[v.group_id]
    if v.run_id ~= binding.run.run_id or v.job_id ~= binding.job.v.job_id or
       v.crawl_policy_sha256 ~= binding.run.v.crawl_policy_sha256 or not group or
       v.rate_scope_id ~= group.v.rate_scope_id or v.group_scope_id ~= group.v.group_scope_id or
       v.group_concurrency ~= group.v.concurrency or v.group_interval_ms ~= group.v.interval_ms then
        return nil, "IMMUTABLE_MISMATCH"
    end
    local digest, decision_fields = decision(v,binding.job.v.depth)
    if not digest or digest ~= v.policy_decision_sha256 or (initial and not initial_matches(v,binding.job.v)) then
        return nil, "IMMUTABLE_MISMATCH"
    end
    local id, token = reservation_id(v), Request.token_digest(v)
    if not id or not token then return nil, "INVALID_ARGUMENT" end
    v.lease_fence,n.lease_fence = nil,nil
    return {v=v,n=n,fields=fields,reservation_id=id,token_digest=token,origin=origin,decision_fields=decision_fields}
end
function Request.build_intent(run,job,lease,kind,url,group_id,ordinal,initial)
    local binding, err = policy(run,job); if not binding then return nil, err end
    if not Request.token_digest(lease) then return nil, "INVALID_IDENTIFIER" end
    local group = binding.run.groups.by_id[group_id]
    local target = URL.check_canonical(url,1)
    local origin = URL.derive_origin(url)
    if not group or not target or not origin then return nil, "INVALID_ARGUMENT" end
    local v = {run_id=lease.run_id,job_id=lease.job_id,owner_id=lease.owner_id,lease_token=lease.lease_token,fence=lease.fence,
        request_ordinal=ordinal,request_kind=kind,target_url_id=target.url_id,canonical_target_url=url,
        target_digest=Request.target_digest(target.url_id,url),crawl_policy_sha256=binding.run.v.crawl_policy_sha256,
        group_id=group_id,rate_scope_id=group.v.rate_scope_id,global_scope_id=I.global_scope(),group_scope_id=group.v.group_scope_id,
        origin_scope_id=Rate.origin_scope(origin),global_concurrency="2",global_interval_ms="0",
        group_concurrency=group.v.concurrency,group_interval_ms=group.v.interval_ms,
        origin_concurrency=group.v.concurrency,origin_interval_ms=group.v.interval_ms}
    v.policy_decision_sha256 = decision(v,binding.job.v.depth)
    return Request.intent(v,run,job,initial)
end
local function clock(ctx)
    return C.preparing(ctx) and I.integer(ctx.now_ms,MAX) and ctx.now_ms > 0 and P.parse_decimal(ctx.now_text) == ctx.now_ms
end
function Request.pending_record(ctx,run,job,values,initial,expiry)
    if not clock(ctx) then return nil, "INVALID_STATE" end
    local intent, err = Request.intent(values,run,job,initial); if not intent then return nil, err end
    if not plain(ctx.request) or not plain(ctx.request.v) or ctx.request.v.run_id ~= intent.v.run_id or
       ctx.keys.run ~= prefix.."run:"..intent.v.run_id or
       P.parse_decimal(run.v.created_at_ms) > ctx.now_ms or P.parse_decimal(job.v.created_at_ms) > ctx.now_ms then
        return nil, "INVALID_STATE"
    end
    local deadline = P.parse_decimal(expiry)
    if not deadline or deadline <= ctx.now_ms then return nil, "INVALID_NUMBER" end
    local v = {}
    for _,k in ipairs(names) do v[k] = intent.v[k] end
    v.protocol_version,v.reservation_id,v.lease_fence,v.state = "2",intent.reservation_id,intent.v.fence,"pending"
    v.created_at_ms,v.expires_at_ms = ctx.now_text,expiry
    for _,k in ipairs(words("started_at_ms terminal_at_ms delivery_attempts_after_start job_starts_after_start run_starts_after_start group_starts_after_start")) do v[k] = "0" end
    return Request.validate(v)
end
function Rate.tighten(record,concurrency,interval,policy_digest,now_text)
    local scope = plain(record) and Rate.validate(record.v)
    local cc,iv,now = P.parse_decimal(concurrency),P.parse_decimal(interval),P.parse_decimal(now_text)
    if not scope or not cc or cc == 0 or cc > 32 or not iv or iv > 3600000 or not now or
       now < scope.n.updated_at_ms or not I.digest(policy_digest) then return nil, "INVALID_ARGUMENT" end
    local v,n = scope.v,scope.n
    if v.scope_kind == "global" and (cc ~= 2 or iv ~= 0) then return nil, "INVALID_ARGUMENT" end
    local changed = false
    if cc < n.effective_concurrency then v.effective_concurrency,v.concurrency_source_sha256,changed = concurrency,policy_digest,true end
    if iv > n.effective_interval_ms then v.effective_interval_ms,v.interval_source_sha256,changed = interval,policy_digest,true end
    if v.scope_kind ~= "global" then
        local deadline = P.safe_add(n.last_started_at_ms,math.max(iv,n.effective_interval_ms))
        if not deadline then return nil, "INVALID_NUMBER" end
        if deadline > n.next_allowed_ms then v.next_allowed_ms,changed = P.format_decimal(deadline),true end
    end
    if changed then v.updated_at_ms = now_text end
    return Rate.validate(v)
end

local function snapshot(ctx,key)
    if not C.can_read(ctx,key) then return nil, "INVALID_STATE" end
    return R.snapshot(ctx,key)
end
local function fixed(ctx,key,schema)
    local f,err = snapshot(ctx,key); if not f then return nil,err end
    if f.exists ~= true or f.kind ~= "hash" or f.schema ~= schema or not f.complete then return nil,"INVALID_STATE" end
    local result = S.project(schema,f.v)
    if not result then return nil,"INVALID_STATE" end
    result.exists,result.kind,result.key,result.ttl_ms = true,"hash",key,f.ttl_ms
    return result
end
local function stored(ctx,id)
    if not I.digest(id) then return nil,"INVALID_IDENTIFIER" end
    local record,err = fixed(ctx,prefix.."reservation:"..id,"reservation"); if not record then return nil,err end
    local v,n = record.v,record.n
    if v.reservation_id ~= id or n.created_at_ms > ctx.now_ms or n.started_at_ms > ctx.now_ms or n.terminal_at_ms > ctx.now_ms then
        return nil,"RESERVATION_CORRUPT"
    end
    record.live_state = v.state == "pending" or v.state == "started"
    record.logical_expired = ctx.now_ms >= n.expires_at_ms
    if record.live_state then
        if record.ttl_ms ~= -1 then return nil,"RESERVATION_CORRUPT" end
    else
        local until_ms = P.safe_add(n.terminal_at_ms,DAY)
        if not until_ms or until_ms <= ctx.now_ms or not I.integer(record.ttl_ms,DAY) or
           record.ttl_ms ~= until_ms-ctx.now_ms then return nil,"RESERVATION_CORRUPT" end
        record.tombstone_until_ms = until_ms
    end
    return record
end
local function private_binding(ctx,run_id,job_id)
    local base = prefix.."run:"..run_id
    local rr,err = fixed(ctx,base,"run"); if not rr then return nil,err end
    local jj,failure = fixed(ctx,base..":job:"..job_id,"job"); if not jj then return nil,failure end
    local maps,ids = {},{}
    for _,pair in ipairs(policy_maps) do
        local f,code = snapshot(ctx,base..":"..pair[1]); if not f then return nil,code end
        if not f.complete or f.kind ~= "hash" or f.count ~= rr.n.policy_group_count then return nil,"INVALID_STATE" end
        maps[pair[1]] = f
    end
    for id,value in next,maps.group_limits.v,nil do if value ~= false then ids[#ids+1] = id end end
    table.sort(ids)
    if #ids ~= rr.n.policy_group_count then return nil,"IMMUTABLE_MISMATCH" end
    local ordered = {}
    for i,id in ipairs(ids) do
        local v = {group_id=id}
        for _,pair in ipairs(policy_maps) do v[pair[2]] = maps[pair[1]].v[id] end
        ordered[i] = {v=v}
    end
    return policy({run_id=run_id,v=rr.v,groups={ordered=ordered}},jj)
end
function Request.check_receipt(view,id)
    if not plain(view) or not clock(view.ctx) then return nil,"INVALID_STATE" end
    local record,err = stored(view.ctx,id); if not record then return nil,err end
    local v = record.v
    local binding,code = private_binding(view.ctx,v.run_id,v.job_id); if not binding then return nil,code end
    local values = {}; for _,k in ipairs(intent_names) do values[k] = k == "fence" and v.lease_fence or v[k] end
    local intent,failure = Request.intent(values,binding.run,binding.job,false)
    if not intent then return nil,failure end
    if record.n.created_at_ms < binding.job.n.created_at_ms or record.n.created_at_ms < binding.run.n.created_at_ms then
        return nil,"RESERVATION_CORRUPT"
    end
    -- Deliberately no current lease, active pointer, B/G or admission test here.
    record.token_digest,record.origin = intent.token_digest,intent.origin
    return record
end
local function index(ctx,key,maximum)
    local f,err = snapshot(ctx,key); if not f then return nil,err end
    if not I.integer(f.count,maximum) or not plain(f.members) or not plain(f.scores) or
       not ((f.exists == true and f.kind == "zset" and f.count > 0) or
            (f.exists == false and f.kind == "none" and f.count == 0 and f.complete)) then return nil,"RATE_STATE_CORRUPT" end
    if f.exists and f.ttl_ms ~= -1 then return nil,"RATE_STATE_CORRUPT" end
    return f
end
local function member(f,id)
    local present = f.members[id]
    if present == nil and f.complete then return false end
    if type(present) ~= "boolean" then return nil,"INVALID_STATE" end
    if not present then return false end
    local raw = f.score_text and f.score_text[id]
    local score = I.redis_score(raw)
    if not score or not I.integer(score,MAX) or score == 0 or score ~= f.scores[id] then return nil,"RATE_STATE_CORRUPT" end
    return score
end
local function scope_matches(scope,record)
    local s,r = scope.v,record.v
    local kind = s.scope_kind
    local witness = kind == "global" and "global" or (kind == "group" and r.rate_scope_id or URL.derive_origin(r.canonical_target_url))
    if r[kind.."_scope_id"] ~= s.scope_id or witness ~= s.scope_witness or
       scope.n.effective_concurrency > record.n[kind.."_concurrency"] or
       scope.n.effective_interval_ms < record.n[kind.."_interval_ms"] then return nil,"RATE_STATE_CORRUPT" end
    return true
end
function Rate.check_scope(view,id)
    if not plain(view) or not clock(view.ctx) or not I.digest(id) then return nil,"INVALID_STATE" end
    local count = I.dense(view.reservation_ids,100)
    if not count then return nil,"INVALID_ARGUMENT" end
    local ctx,key = view.ctx,prefix.."rate:"..id
    local raw,err = snapshot(ctx,key); if not raw then return nil,err end
    local inventory,code = index(ctx,prefix.."rate_scopes",100000); if not inventory then return nil,code end
    local at,failure = member(inventory,id); if at == nil then return nil,failure end
    local ix,selected = {},{}
    for _,name in ipairs({"active","pending","started"}) do
        ix[name],code = index(ctx,key..":"..name,32); if not ix[name] then return nil,code end
        for q in next,ix[name].members,nil do selected[q] = true end
    end
    for _,q in ipairs(view.reservation_ids) do
        if not I.digest(q) then return nil,"INVALID_IDENTIFIER" end
        selected[q] = true
    end
    local scope
    if raw.exists == false and raw.kind == "none" and raw.complete then
        if at ~= false or ix.active.count ~= 0 or ix.pending.count ~= 0 or ix.started.count ~= 0 then return nil,"RATE_STATE_CORRUPT" end
        scope = {exists=false,kind="none",key=key}
    else
        scope,code = fixed(ctx,key,"rate_scope"); if not scope then return nil,code end
        if scope.v.scope_id ~= id or at ~= scope.n.updated_at_ms or scope.n.updated_at_ms > ctx.now_ms or scope.ttl_ms ~= -1 or
           scope.n.active_count ~= ix.active.count or scope.n.pending_count ~= ix.pending.count or scope.n.started_count ~= ix.started.count or
           (scope.v.scope_kind == "global" and scope.n.active_count > 2) then return nil,"RATE_STATE_CORRUPT" end
        -- active_count may exceed a newly tightened effective_concurrency, and
        -- multiple pending members may survive a zero-to-positive interval change.
    end
    for q in next,selected,nil do
        if not I.digest(q) then return nil,"RATE_STATE_CORRUPT" end
        local a,e1 = member(ix.active,q); if a == nil then return nil,e1 end
        local p,e2 = member(ix.pending,q); if p == nil then return nil,e2 end
        local s,e3 = member(ix.started,q); if s == nil then return nil,e3 end
        if (a ~= false) ~= (p ~= false or s ~= false) or (p ~= false and s ~= false) or
           (p ~= false and p ~= a) or (s ~= false and s ~= a) then return nil,"RATE_STATE_CORRUPT" end
        local receipt,missing = snapshot(ctx,prefix.."reservation:"..q)
        if not receipt then return nil,missing end
        if receipt.exists then
            local reservation,why = stored(ctx,q); if not reservation then return nil,why end
            if not scope.exists or not scope_matches(scope,reservation) or
               (reservation.v.state == "pending") ~= (p ~= false) or (reservation.v.state == "started") ~= (s ~= false) or
               (a ~= false and a ~= reservation.n.expires_at_ms) or
               reservation.n.started_at_ms > scope.n.last_started_at_ms or
               scope.n.updated_at_ms < math.max(reservation.n.created_at_ms,reservation.n.terminal_at_ms) then
                return nil,"RATE_STATE_CORRUPT"
            end
        elseif a ~= false or receipt.kind ~= "none" or not receipt.complete then return nil,"RATE_STATE_CORRUPT"
        end
    end
    scope.indexes,scope.inventory = ix,inventory
    scope.complete_membership = ix.active.complete and ix.pending.complete and ix.started.complete
    return scope
end

local job_indexes = words("jobs job_order ready ready_at leased leased_at delayed completed dead cancelled commit_backpressure active_leases")
function Request.check_live(view,run,job,id)
    local record,err = Request.check_receipt(view,id); if not record then return nil,err end
    if not record.live_state then return nil,"RESERVATION_CORRUPT" end
    local ctx,v,n = view.ctx,record.v,record.n
    local binding,code = private_binding(ctx,v.run_id,v.job_id); if not binding then return nil,code end
    local rr = plain(run) and CJ.Run.validate(run.v)
    local jj = plain(job) and S.project("job",job.v)
    if not rr or not jj or run.run_id ~= v.run_id or S.encode(rr) ~= S.encode(CJ.Run.validate(binding.run.v)) or
       S.encode(jj) ~= S.encode(binding.job) then return nil,"INVALID_STATE" end
    local base,jv = prefix.."run:"..v.run_id,{ctx=ctx,job={key=prefix.."run:"..v.run_id..":job:"..v.job_id}}
    for _,name in ipairs(job_indexes) do jv[name] = {key=name == "active_leases" and prefix..name or base..":"..name} end
    local current,failure = CJ.Job.check(jv,binding.run,v.job_id); if not current then return nil,failure end
    local j,jn,rn = current.v,current.n,rr.n
    -- Job.check proves this job's exact memberships, not aggregate counts.
    -- Bind the run counters to those SAME private collection cardinalities.
    local counts = {}
    for _,name in ipairs(job_indexes) do
        local fact = snapshot(ctx,jv[name].key)
        if not fact then return nil,"INVALID_STATE" end
        counts[name] = fact.count
    end
    if counts.jobs ~= rn.job_count or counts.job_order ~= rn.job_count or
       counts.ready+counts.leased+counts.delayed ~= rn.open_job_count or
       counts.completed ~= rn.completed_total or counts.dead ~= rn.dead_total or counts.cancelled ~= rn.cancelled_total or
       counts.ready_at ~= counts.ready or counts.leased_at ~= counts.leased or counts.leased > rn.claims_total or
       counts.commit_backpressure > counts.leased or counts.active_leases < counts.leased then return nil,"COUNTER_CORRUPT" end
    if j.state ~= "leased" or j.active_reservation_id ~= id or j.lease_owner ~= v.owner_id or j.lease_token ~= v.lease_token or
       j.lease_fence ~= v.lease_fence or jn.last_stage_fence >= n.lease_fence or j.active_stage_commit_id ~= "" or
       jn.lease_expires_at_ms ~= n.expires_at_ms or n.created_at_ms < jn.lease_started_at_ms or
       jn.lease_started_at_ms < rn.activated_at_ms or
       n.created_at_ms > jn.updated_at_ms or jn.updated_at_ms > rn.last_activity_at_ms or rn.last_activity_at_ms > ctx.now_ms or
       not one(rr.v.state,"active","cancelled") or rn.finalized_at_ms ~= 0 then return nil,"RESERVATION_CORRUPT" end
    if n.request_ordinal+1 ~= jn.next_request_ordinal or jn.next_request_ordinal-1 > rn.reservation_creations_total or
       jn.claim_count > rn.claims_total or jn.request_starts > rn.request_starts or
       rn.request_starts+rn.pending_request_reservations > rn.max_request_starts or
       rn.request_starts+rn.pending_request_reservations > rn.reservation_creations_total or
       rn.started_request_reservations > rn.request_starts then return nil,"COUNTER_CORRUPT" end
    local maps,sums = {},{}
    for _,name in ipairs({"group_started","group_pending","group_active_started","group_open_jobs"}) do
        local f,why = snapshot(ctx,base..":"..name); if not f then return nil,why end
        if f.kind ~= "hash" or not f.complete or f.count ~= binding.run.groups.count then return nil,"COUNTER_CORRUPT" end
        maps[name],sums[name] = {},0
        for _,group in ipairs(binding.run.groups.ordered) do
            local value = P.parse_decimal(f.v[group.v.group_id])
            if not value or value > (name == "group_open_jobs" and 10000 or 10) then return nil,"COUNTER_CORRUPT" end
            maps[name][group.v.group_id],sums[name] = value,sums[name]+value
        end
    end
    for _,g in ipairs(binding.run.groups.ordered) do
        local k = g.v.group_id
        local pending,started,active = maps.group_pending[k],maps.group_started[k],maps.group_active_started[k]
        if pending+started > g.n.request_start_limit or active > started or pending+active > g.n.concurrency then
            return nil,"COUNTER_CORRUPT"
        end
        -- No pending/active <= group_open_jobs: later redirects may charge a
        -- different group; open_jobs remains charged to the immutable SOURCE.
    end
    if sums.group_started ~= rn.request_starts or sums.group_pending ~= rn.pending_request_reservations or
       sums.group_active_started ~= rn.started_request_reservations or sums.group_open_jobs ~= rn.open_job_count or
       rn.pending_request_reservations+rn.started_request_reservations > rn.global_concurrency_limit or
       rn.pending_request_reservations+rn.started_request_reservations > counts.leased or
       maps.group_open_jobs[j.group_id] < 1 or rn.reservation_creations_total == 0 then return nil,"COUNTER_CORRUPT" end
    local first
    if v.state == "pending" then
        if maps.group_pending[v.group_id] < 1 or n.request_ordinal <= jn.request_starts then return nil,"COUNTER_CORRUPT" end
        if n.created_at_ms < jn.last_request_started_at_ms then return nil,"RESERVATION_CORRUPT" end
        first = jn.request_starts == jn.lease_request_starts_baseline
    else
        if maps.group_active_started[v.group_id] < 1 or n.delivery_attempts_after_start ~= jn.delivery_attempts or
           n.job_starts_after_start ~= jn.request_starts or n.job_starts_after_start <= jn.lease_request_starts_baseline or
           n.run_starts_after_start > rn.request_starts or n.group_starts_after_start > maps.group_started[v.group_id] or
           n.delivery_attempts_after_start > n.lease_fence or n.started_at_ms ~= jn.last_request_started_at_ms or
           n.started_at_ms > rn.last_request_started_at_ms or jn.lease_delivery_started ~= 1 then return nil,"COUNTER_CORRUPT" end
        first = n.job_starts_after_start == jn.lease_request_starts_baseline+1
        if one(v.request_kind,"document","redirect") and (j.last_document_request_fence ~= v.lease_fence or
           j.last_document_request_started_at_ms ~= v.started_at_ms or j.last_document_target_url_id ~= v.target_url_id or
           j.last_document_target_url ~= v.canonical_target_url or j.last_document_target_digest ~= v.target_digest) then
            return nil,"RESERVATION_CORRUPT"
        end
    end
    if first and not initial_matches(v,j) then return nil,"IMMUTABLE_MISMATCH" end
    local scopes = {}
    for _,kind in ipairs({"global","group","origin"}) do
        local scope,why = Rate.check_scope({ctx=ctx,reservation_ids={id}},v[kind.."_scope_id"])
        if not scope then return nil,why end
        if not scope.exists or scope.v.scope_kind ~= kind or not scope_matches(scope,record) then return nil,"RATE_STATE_CORRUPT" end
        scopes[kind] = scope
    end
    if scopes.global.n.pending_count < rn.pending_request_reservations or
       scopes.global.n.started_count < rn.started_request_reservations or
       scopes.global.n.last_started_at_ms < rn.last_request_started_at_ms then return nil,"COUNTER_CORRUPT" end
    local group_pending,group_started = 0,0
    for _,g in ipairs(binding.run.groups.ordered) do
        if g.v.group_scope_id == v.group_scope_id then
            group_pending = group_pending+maps.group_pending[g.v.group_id]
            group_started = group_started+maps.group_active_started[g.v.group_id]
        end
    end
    if scopes.group.n.pending_count < group_pending or scopes.group.n.started_count < group_started then
        return nil,"COUNTER_CORRUPT"
    end
    record.scopes,record.run,record.job = scopes,binding.run,current
    record.eligible_now = not record.logical_expired and rr.v.state == "active" and ctx.now_ms < rn.authorization_expires_at_ms
    return record
end
-- Worker preparation support. These functions only construct validated records
-- and inert Plan descriptors. The six operation fragments own the fixed executor.
local Run, Job = CJ.Run, CJ.Job
local claim_names = words([[run_id job_id canonical_url score_text depth job_group_id job_rate_scope_id job_group_scope_id
job_initial_origin_scope_id job_policy_decision_sha256 expected_prior_fence fence owner_id lease_token request_ordinal
request_kind target_url_id canonical_target_url target_digest crawl_policy_sha256 policy_decision_sha256 group_id
rate_scope_id global_scope_id group_scope_id origin_scope_id global_concurrency global_interval_ms group_concurrency
group_interval_ms origin_concurrency origin_interval_ms transition_id]])
local function copy(v)
    if type(v) ~= "table" then return v end
    local out = {}; for k,x in next,v,nil do out[k] = copy(x) end; return out
end
local function decimal(n) return P.format_decimal(n) end
local function project_job(job,changes)
    local v = copy(job.v)
    for k,x in next,changes,nil do v[k] = x end
    return S.project("job",v)
end
local function project_request(record,changes)
    local v = copy(record.v)
    for k,x in next,changes,nil do v[k] = x end
    return Request.validate(v)
end
-- The 28 payload fields omit only run/job/fence/token/transition_id. Owner and
-- expected_prior_fence are retained, exactly as DeriveTryClaimTransitionID.
function Request.claim_identity(v)
    if not plain(v) or not Request.token_digest(v) or not I.digest(v.transition_id) then return nil,"INVALID_ARGUMENT" end
    local prior,fence = P.parse_decimal(v.expected_prior_fence),P.parse_decimal(v.fence)
    if prior == nil or P.safe_add(prior,1) ~= fence then return nil,"INVALID_NUMBER" end
    local fields = {}
    for _,k in ipairs(claim_names) do
        if type(v[k]) ~= "string" or #v[k] > 2048 or not P.validate_text(v[k]) then return nil,"INVALID_ARGUMENT" end
        if k ~= "run_id" and k ~= "job_id" and k ~= "fence" and k ~= "lease_token" and k ~= "transition_id" then
            fields[#fields+1] = {k,v[k]}
        end
    end
    local section = P.section("arguments",{fields},16384)
    if not section then return nil,"INVALID_ARGUMENT" end
    local payload = P.sha256(P.frame("mifolyo:transition-payload:v2")..section)
    local id = I.framed("mifolyo:crawl-transition:v2",{"CJ2_TRY_CLAIM",v.run_id,v.job_id,v.fence,v.lease_token,"none",payload})
    if id ~= v.transition_id then return nil,"IMMUTABLE_MISMATCH" end
    return id
end
local worker_ops = {CJ2_TRY_CLAIM=true,CJ2_RENEW_LEASE=true,CJ2_RESERVE_REQUEST=true,
    CJ2_START_REQUEST=true,CJ2_FINISH_REQUEST=true,CJ2_CANCEL_RESERVATION=true}
local function worker_reply(ctx,status,tail)
    local op,a = ctx.operation,ctx.request.v
    local claim,reserve,start = op == "CJ2_TRY_CLAIM",op == "CJ2_RESERVE_REQUEST",op == "CJ2_START_REQUEST"
    local function uint(i,max) local n = P.parse_decimal(tail[i]); return n and n <= max and n end
    local function past(i) local n = uint(i,ctx.now_ms); return n and n > 0 end
    local function future(i) local n = uint(i,MAX); return n and n > ctx.now_ms and n end
    local function bit(i) return tail[i] == "0" or tail[i] == "1" end
    local function after(i) return bit(i) and (not claim or tail[i] == "0") end
    local function budget(i)
        local s,p,l = uint(i,10),uint(i+1,10),uint(i+2,10)
        return s and p and l and l > 0 and s+p == l
    end
    if status == "LEASE_LOST" and #tail == 1 and uint(1,MAX) then return true end
    if one(status,"AUTHORIZATION_EXPIRED","RUN_CANCELLED") and #tail == 0 then return true end
    if claim then
        if status == "NO_CANDIDATE" and #tail == 0 then return true end
        if status == "VISITED_COMPLETED" and #tail == 1 and past(1) then return true end
        if one(status,"CLAIMED","ALREADY_CLAIMED") and #tail == 4 and tail[1] == a.fence and I.positive(tail[1]) and
           future(2) and P.safe_add(ctx.now_ms,60000) and uint(2,MAX) <= ctx.now_ms+60000 and tail[4] == tail[2] and
           I.digest(tail[3]) and (status ~= "CLAIMED" or uint(2,MAX) == ctx.now_ms+60000) then return true end
        if status == "LEASE_CAPACITY_BLOCKED" and #tail == 2 and tail[1] == "64" and tail[2] == "64" then return true end
        if status == "STAGE_CAPACITY_BLOCKED" and #tail == 3 and tail[1] == "stage_slots_full" and tail[2] == "4" and tail[3] == "4" then return true end
    elseif reserve and one(status,"RESERVED","ALREADY_RESERVED") and #tail == 2 and I.digest(tail[1]) and future(2) and
       P.safe_add(ctx.now_ms,60000) and uint(2,MAX) <= ctx.now_ms+60000 then return true
    elseif start and one(status,"STARTED","ALREADY_STARTED") and #tail == 7 and tail[1] == a.reservation_id and I.digest(tail[1]) and
       past(2) and uint(3,3) and uint(3,3) > 0 and uint(4,10) and uint(5,10) and uint(6,10) and uint(6,10) > 0 and
       uint(3,3) <= uint(4,10) and uint(4,10) <= uint(5,10) and uint(6,10) <= uint(5,10) and bit(7) and
       (status ~= "STARTED" or (tail[2] == ctx.now_text and tail[7] == "1")) then return true
    elseif op == "CJ2_FINISH_REQUEST" and one(status,"FINISHED","ALREADY_FINISHED") and #tail == 1 and
       I.digest(tail[1]) and tail[1] == a.reservation_id then return true
    elseif op == "CJ2_CANCEL_RESERVATION" and status == "RESERVATION_CANCELLED" and #tail == 1 and
       I.digest(tail[1]) and tail[1] == a.reservation_id then return true
    elseif op == "CJ2_RENEW_LEASE" and status == "RENEWED" and #tail == 1 and future(1) and
       P.safe_add(ctx.now_ms,60000) and uint(1,MAX) <= ctx.now_ms+60000 then return true end
    if (claim or reserve) then
        if status == "CAPACITY_BLOCKED" and #tail == 4 and I.digest(tail[1]) and uint(2,32) and uint(3,32) and
           uint(3,32) > 0 and uint(2,32) >= uint(3,32) and after(4) then return true end
        if status == "RUN_BUDGET_EXHAUSTED" and #tail == 4 and budget(1) and after(4) then return true end
        if status == "RUN_RESERVATION_LIMIT_EXHAUSTED" and #tail == 3 and tail[1] == "100" and tail[2] == "100" and after(3) then return true end
        if status == "GROUP_BUDGET_EXHAUSTED" and #tail == 5 and I.group(tail[1]) and budget(2) and after(5) then return true end
    end
    if (claim or reserve or start) and status == "RATE_BLOCKED" and #tail == 3 and I.digest(tail[1]) and I.positive(tail[2]) and
       after(3) and (not start or future(2)) then return true end
    return nil,"INVALID_ARGUMENT"
end
function Request.register_worker(op)
    if not worker_ops[op] then return nil,"INVALID_ARGUMENT" end
    return C.Reply.register(op,worker_reply)
end

-- Read selection helpers never broaden permissions: revision-4 Context bindings
-- are the only authority for derived keys; a missing grant fails in Read itself.
local function select_job(ctx,run,job_id)
    local id = job_id or ctx.request.v.job_id
    if not I.hex(id,64) or not I.hex(run.run_id,32) then return nil,"INVALID_IDENTIFIER" end
    local base = prefix.."run:"..run.run_id
    local view = {ctx=ctx}
    local job,err = R.fixed_hash(ctx,base..":job:"..id,"job"); if not job then return nil,err end
    view.job = job
    for _,name in ipairs(job_indexes) do
        local key,member_id,kind,maximum = base..":"..name,id,"zset",10000
        if name == "jobs" then kind = "set"
        elseif name == "leased" or name == "leased_at" then maximum = 64
        elseif name == "commit_backpressure" then maximum = 10
        elseif name == "active_leases" then key,member_id,maximum = prefix..name,run.run_id..":"..id,64 end
        view[name],err = R.members(ctx,key,kind,{member_id},maximum,97)
        if not view[name] then return nil,err end
    end
    return Job.check(view,run,id)
end
local function select_reservation(ctx,id)
    local key = prefix.."reservation:"..id
    local record,err = R.fixed_hash(ctx,key,"reservation"); if not record then return nil,err end
    local ttl,code = R.ttl(ctx,key); if not ttl then return nil,code end
    return record
end
local function lease_matches(job,a)
    return job.exists and job.v.state == "leased" and job.v.lease_owner == a.owner_id and
        job.v.lease_token == a.lease_token and job.v.lease_fence == a.fence
end
local function same_lease_record(record,a)
    local v = record.v
    return v.run_id == a.run_id and v.job_id == a.job_id and v.owner_id == a.owner_id and v.lease_token == a.lease_token and v.lease_fence == a.fence
end
local function same_intent(record,intent)
    if record.v.reservation_id ~= intent.reservation_id then return false end
    for _,k in ipairs(intent_names) do
        if (k == "fence" and record.v.lease_fence or record.v[k]) ~= intent.v[k] then return false end
    end
    return true
end
local function authority_status(ctx,run)
    if run.v.state == "cancelled" then return "RUN_CANCELLED" end
    if ctx.now_ms >= run.n.authorization_expires_at_ms then return "AUTHORIZATION_EXPIRED" end
    if run.v.state ~= "active" then return nil,"INVALID_STATE" end
    return false
end
local function write_projection(plan,key,before,post,coverage)
    local changes = {}
    for _,field in ipairs(post.fields) do
        if not before or before.v[field[1]] ~= field[2] then changes[field[1]] = field[2] end
    end
    if next(changes,nil) == nil then return true end
    return Run.hset(plan,key,changes,coverage or "ordinary")
end
local function add(plan,argv,coverage) return CJ.Plan.add(plan,argv,coverage or "ordinary") end
local function finish(ctx,plan,status,tail)
    return Run.finish(ctx,plan,status,tail or {})
end
local function run_delta(ctx,plan,run,changes,sets,coverage)
    local delta,err = Run.plan_delta(ctx,run); if not delta then return nil,err end
    local ok,code = Run.accumulate(delta,changes,coverage); if not ok then return nil,code end
    if sets then ok,code = Run.set(delta,sets,coverage); if not ok then return nil,code end end
    return Run.flush(ctx,plan,delta,coverage)
end
-- Construct a prospective new scope only on successful reservation creation.
local function new_scope(ctx,intent,kind)
    local v = intent.v
    local witness = kind == "global" and "global" or (kind == "group" and v.rate_scope_id or intent.origin)
    return Rate.validate({protocol_version="2",scope_id=v[kind.."_scope_id"],scope_kind=kind,scope_witness=witness,
        effective_concurrency=v[kind.."_concurrency"],effective_interval_ms=v[kind.."_interval_ms"],next_allowed_ms="0",
        last_started_at_ms="0",active_count="0",pending_count="0",started_count="0",concurrency_source_sha256=v.crawl_policy_sha256,
        interval_source_sha256=v.crawl_policy_sha256,updated_at_ms=ctx.now_text})
end
local function scope_posts(ctx,intent,scopes)
    local posts,new = {},0
    for _,kind in ipairs({"global","group","origin"}) do
        local old = scopes[kind]
        local witness = kind == "global" and "global" or (kind == "group" and intent.v.rate_scope_id or intent.origin)
        local post,err
        if old.exists then
            if old.v.scope_kind ~= kind or old.v.scope_witness ~= witness then return nil,"RATE_STATE_CORRUPT" end
            post,err = Rate.tighten(old,intent.v[kind.."_concurrency"],intent.v[kind.."_interval_ms"],intent.v.crawl_policy_sha256,ctx.now_text)
        else post,err = new_scope(ctx,intent,kind); new = new+1 end
        if not post then return nil,err end
        posts[kind] = post
    end
    if scopes.global.inventory.count+new > 100000 then return nil,"RATE_SCOPE_CAPACITY_EXCEEDED" end
    return posts
end
local function blocked(ctx,run,job,intent,scopes,posts,claim,slots,leases)
    local after = claim and "0" or job.v.lease_delivery_started
    local group = intent.v.group_id
    if run.n.request_starts+run.n.pending_request_reservations == run.n.max_request_starts then
        return "RUN_BUDGET_EXHAUSTED",{run.v.request_starts,run.v.pending_request_reservations,run.v.max_request_starts,after}
    end
    if run.n.reservation_creations_total == 100 then return "RUN_RESERVATION_LIMIT_EXHAUSTED",{"100","100",after} end
    local gs,gp,limit = run.maps.group_started.v[group],run.maps.group_pending.v[group],run.maps.group_limits.v[group]
    if P.parse_decimal(gs)+P.parse_decimal(gp) == P.parse_decimal(limit) then return "GROUP_BUDGET_EXHAUSTED",{group,gs,gp,limit,after} end
    for _,kind in ipairs({"global","group","origin"}) do
        local p,old = posts[kind],scopes[kind]
        if p.n.active_count >= p.n.effective_concurrency then
            return "CAPACITY_BLOCKED",{p.v.scope_id,p.v.active_count,p.v.effective_concurrency,after}
        end
        local deadline = p.n.next_allowed_ms
        if kind ~= "global" and p.n.effective_interval_ms > 0 and p.n.pending_count > 0 then
            for _,expiry in next,old.indexes.pending.scores,nil do if expiry ~= false then deadline = math.max(deadline,expiry) end end
            return "RATE_BLOCKED",{p.v.scope_id,decimal(deadline),after}
        end
        if ctx.now_ms < deadline then return "RATE_BLOCKED",{p.v.scope_id,decimal(deadline),after} end
    end
    if claim and leases.count == 64 then return "LEASE_CAPACITY_BLOCKED",{"64","64"} end
    if claim and slots.count == 4 then return "STAGE_CAPACITY_BLOCKED",{"stage_slots_full","4","4"} end
    return false
end
local function write_scopes(ctx,plan,scopes,posts,record,action,coverage)
    local written = {}
    for _,kind in ipairs({"global","group","origin"}) do
        local old,post = scopes[kind],posts[kind]
        if action ~= "tighten" or old.exists then
            local v = copy(post.v)
            local id,key = v.scope_id,prefix.."rate:"..v.scope_id
            if action == "reserve" then
                v.active_count,v.pending_count,v.updated_at_ms = decimal(post.n.active_count+1),decimal(post.n.pending_count+1),ctx.now_text
            elseif action == "start" then
                v.pending_count,v.started_count,v.last_started_at_ms,v.updated_at_ms = decimal(post.n.pending_count-1),decimal(post.n.started_count+1),ctx.now_text,ctx.now_text
                if kind ~= "global" then
                    local deadline = P.safe_add(ctx.now_ms,post.n.effective_interval_ms)
                    if not deadline then return nil,"INVALID_NUMBER" end
                    v.next_allowed_ms = decimal(math.max(post.n.next_allowed_ms,deadline))
                end
            elseif action == "finish" or action == "cancel" then
                local field = action == "finish" and "started_count" or "pending_count"
                v.active_count,v[field],v.updated_at_ms = decimal(post.n.active_count-1),decimal(post.n[field]-1),ctx.now_text
            elseif action == "renew" then v.updated_at_ms = ctx.now_text end
            local projected = Rate.validate(v)
            if not projected then return nil,"RATE_STATE_CORRUPT" end
            local ok,err = write_projection(plan,key,old.exists and old or nil,projected,coverage); if not ok then return nil,err end
            if not old.exists or old.v.updated_at_ms ~= projected.v.updated_at_ms then
                ok,err = add(plan,{"ZADD",prefix.."rate_scopes",projected.v.updated_at_ms,id},coverage); if not ok then return nil,err end
            end
            if action ~= "tighten" then
                local q,e = record.v.reservation_id,record.v.expires_at_ms
                local calls = {}
                if action == "reserve" then calls = {{"ZADD",key..":active",e,q},{"ZADD",key..":pending",e,q}}
                elseif action == "start" then calls = {{"ZREM",key..":pending",q},{"ZADD",key..":started",e,q}}
                elseif action == "finish" or action == "cancel" then calls = {{"ZREM",key..":active",q},{"ZREM",key..(action == "finish" and ":started" or ":pending"),q}}
                elseif action == "renew" then calls = {{"ZADD",key..":active",e,q},{"ZADD",key..":"..record.v.state,e,q}} end
                for _,argv in ipairs(calls) do ok,err = add(plan,argv,coverage); if not ok then return nil,err end end
            end
            -- A private per-plan terminal accumulator can use this post-state
            -- for the NEXT request without rewriting an original count twice.
            projected.exists,projected.kind,projected.key = true,"hash",key
            written[kind] = projected
        end
    end
    return written
end

local function select_scopes(ctx,values)
    local scopes = {}
    for _,kind in ipairs({"global","group","origin"}) do
        local id,key = values[kind.."_scope_id"],prefix.."rate:"..values[kind.."_scope_id"]
        local f,err = R.fixed_hash(ctx,key,"rate_scope")
        if not f then return nil,err == "INVALID_STATE" and "RATE_STATE_CORRUPT" or err end
        local materialized = f.exists
        f,err = R.ttl(ctx,key); if not f then return nil,err end
        f,err = R.members(ctx,prefix.."rate_scopes","zset",{id},100000,64); if not f then return nil,err end
        f,err = R.ttl(ctx,prefix.."rate_scopes"); if not f then return nil,err end
        local ids,seen = {},{}
        for _,suffix in ipairs({"active","pending","started"}) do
            local members,code = R.all_members(ctx,key..":"..suffix,"zset",32,64); if not members then return nil,code end
            local ttl,why = R.ttl(ctx,key..":"..suffix); if not ttl then return nil,why end
            for _,q in ipairs(members.ordered) do
                if not I.digest(q) then return nil,"RATE_STATE_CORRUPT" end
                if not seen[q] then ids[#ids+1],seen[q] = q,true end
            end
        end
        for _,q in ipairs(ids) do
            if not C.can_read(ctx,prefix.."reservation:"..q) then
                -- Core owns the selected-scope-member read grant. This is not a
                -- permission fallback: it must prove private index membership.
                local bound,why = C.bind_scope_reservations(ctx,id)
                if not bound then return nil,why end
            end
            local receipt,why = select_reservation(ctx,q); if not receipt then return nil,why end
        end
        if kind ~= "global" then
            local global = scopes.global
            if not global.exists and materialized then return nil,"RATE_STATE_CORRUPT" end
            -- The selected global index is complete and bounded by two. Every
            -- group/origin member must also be globally active, even when it
            -- belongs to another run. Conversely a globally held request for
            -- this scope cannot disappear from its secondary scope indexes.
            for _,q in ipairs(ids) do
                if global.indexes.active.members[q] ~= true then return nil,"RATE_STATE_CORRUPT" end
            end
            for q,present in next,global.indexes.active.members,nil do
                if present then
                    local held,why = stored(ctx,q); if not held then return nil,why end
                    if held.v[kind.."_scope_id"] == id and not seen[q] then ids[#ids+1],seen[q] = q,true end
                end
            end
        end
        scopes[kind],err = Rate.check_scope({ctx=ctx,reservation_ids=ids},id)
        if not scopes[kind] then return nil,err end
        if kind == "global" and not scopes[kind].exists and scopes[kind].inventory.count ~= 0 then return nil,"RATE_STATE_CORRUPT" end
    end
    return scopes
end
function Request.load_live(ctx,run,job)
    if not clock(ctx) or not plain(run) or not plain(job) or not plain(job.v) or not I.hex(job.v.job_id,64) then
        return nil,"INVALID_STATE"
    end
    local supplied = S.project("job",job.v)
    if not supplied then return nil,"INVALID_STATE" end
    local current,check_error = select_job(ctx,run,supplied.v.job_id)
    if not current then return nil,check_error end
    if not current.exists or S.encode(current) ~= S.encode(supplied) then return nil,"INVALID_STATE" end
    -- An empty pointer is known only from the complete, privately selected Job
    -- plus its memberships. Neither a public .v/.n edit nor no receipt means none.
    local q = current.v.active_reservation_id
    if q == "" then return false end
    local held_binding = one(ctx.operation,"CJ2_RENEW_LEASE","CJ2_RECOVER_EXPIRED","CJ2_RELEASE_BEFORE_IO")
    if held_binding then
        -- A batch may have read this Q as another scope's peer. Read permission
        -- is NOT held-request write permission: bind the current owner anyway.
        local bound,err = C.bind_held_request(ctx,current.v.job_id)
        if not bound then return nil,err end
        if not bound.exists or bound.reservation_id ~= q then return nil,"RESERVATION_CORRUPT" end
    end
    local receipt,err = select_reservation(ctx,q); if not receipt then return nil,err end
    if not receipt.exists then return nil,"RESERVATION_CORRUPT" end
    if held_binding then
        local bound,why = C.bind_held_scopes(ctx,q); if not bound then return nil,why end
    elseif one(ctx.operation,"CJ2_START_REQUEST","CJ2_FINISH_REQUEST","CJ2_CANCEL_RESERVATION") then
        local bound,why = C.bind_request(ctx); if not bound then return nil,why end
    end
    local scopes,why = select_scopes(ctx,receipt.v); if not scopes then return nil,why end
    return Request.check_live({ctx=ctx},run,current,q)
end
local function select_stage(ctx,run,job)
    local slots,err = R.slots(ctx); if not slots then return nil,err end
    local commit = job.v.active_stage_commit_id
    local aborted = commit == "" and job.n.last_stage_fence > 0 and job.v.last_stage_fence == job.v.lease_fence
    if aborted then commit = job.v.last_stage_commit_id end
    if commit ~= "" then
        local bound,code = C.bind_stage(ctx,job.v.job_id); if not bound then return nil,code end
        if not bound.exists or bound.commit_id ~= commit then return nil,"STAGE_INVALID" end
        local selected,why = CJ.Stage.select(ctx,commit); if not selected then return nil,why end
    end
    -- This RENEW-only inspection uses stored credentials and may validate an
    -- expired lease/stage. It is never handed to Plan as allocation authority.
    return CJ.Stage.check_renewal_state(ctx,run,job)
end
local function load_worker(ctx)
    local gate,err = CJ.Gate.check(ctx); if not gate then return nil,err end
    local run,code = Run.load(ctx,ctx.request.v.run_id); if not run then return nil,code end
    local job,why = select_job(ctx,run); if not job then return nil,why end
    if job.exists and (job.n.created_at_ms < run.n.created_at_ms or job.n.updated_at_ms > run.n.last_activity_at_ms) then
        return nil,"INVALID_STATE"
    end
    local plan,failure = CJ.Plan.new(ctx); if not plan then return nil,failure end
    return {ctx=ctx,run=run,job=job,plan=plan}
end
local function stage_admission_facts(ctx,run)
    local leases,err = R.all_members(ctx,prefix.."active_leases","zset",64,97); if not leases then return nil,err end
    local slots,code = R.slots(ctx); if not slots then return nil,code end
    for _,member_id in ipairs(leases.ordered) do
        local owner,id = string.match(member_id,"^([^:]+):([^:]+)$")
        local at = leases.scores[member_id]
        if not I.hex(owner,32) or not I.hex(id,64) or not run.inventory.active_runs.members[owner] or
           not I.integer(at,MAX) or at == 0 then return nil,"STATE_INDEX_CORRUPT" end
    end
    local owners = {}
    for _,slot in next,slots.records,nil do
        local owner = slot.run_id..":"..slot.job_id
        if not leases.members[owner] or owners[owner] then return nil,"STATE_INDEX_CORRUPT" end
        owners[owner] = true
    end
    return {leases=leases,slots=slots}
end
local function claim_source(a,run,job)
    local fields = {{"job_id",a.job_id},{"canonical_url",a.canonical_url},{"score_text",a.score_text},{"depth",a.depth},
        {"group_id",a.job_group_id},{"rate_scope_id",a.job_rate_scope_id},{"group_scope_id",a.job_group_scope_id},
        {"initial_origin_scope_id",a.job_initial_origin_scope_id},{"policy_decision_sha256",a.job_policy_decision_sha256}}
    local source,err = Job.source({fields=fields},run); if not source then return nil,err end
    if job.exists then
        for _,field in ipairs(source.fields) do
            if job.v[field[1]] ~= field[2] then return nil,"IMMUTABLE_MISMATCH" end
        end
    end
    return source
end
local function visit(ctx,run,job)
    local base,id = prefix.."run:"..run.run_id,job.v.job_id
    local urls,err = R.hash_fields(ctx,base..":visited_urls",{id},10000,64,2048); if not urls then return nil,err end
    local depths,code = R.hash_fields(ctx,base..":visited_depth",{id},10000,64,16); if not depths then return nil,code end
    local u,d = urls.v[id],depths.v[id]
    if (u == false) ~= (d == false) then return nil,"STATE_INDEX_CORRUPT" end
    if u == false then return false end
    if u ~= job.v.canonical_url then return nil,"URL_ID_COLLISION" end
    local n = P.parse_decimal(d); if n == nil then return nil,"STATE_INDEX_CORRUPT" end
    return n <= job.n.depth
end
local function claim_visited(w)
    local ctx,run,job,plan = w.ctx,w.run,w.job,w.plan
    local a,base = ctx.request.v,prefix.."run:"..run.run_id
    local post,err = project_job(job,{state="completed",completed_at_ms=ctx.now_text,updated_at_ms=ctx.now_text,
        last_reason="already_visited",last_transition_id=a.transition_id,last_transition_status="VISITED_COMPLETED"})
    if not post then return nil,err end
    for _,argv in ipairs({{"ZREM",base..":ready",a.job_id},{"ZREM",base..":ready_at",a.job_id},{"ZADD",base..":completed",ctx.now_text,a.job_id}}) do
        local ok,code = add(plan,argv); if not ok then return nil,code end
    end
    local ok,code = write_projection(plan,job.key,job,post); if not ok then return nil,code end
    local changed,why = run_delta(ctx,plan,run,{run={open_job_count=-1,completed_total=1},
        maps={group_open_jobs={[job.v.group_id]=-1},disposition_reason_counts={already_visited=1}},
        indexes={ready=-1,ready_at=-1,completed=1}}, {last_activity_at_ms=ctx.now_text,last_terminal_transition_at_ms=ctx.now_text})
    if not changed then return nil,why end
    return finish(ctx,plan,"VISITED_COMPLETED",{ctx.now_text})
end
local function create_reservation(w,intent,scopes,posts,claim)
    local ctx,run,job,plan = w.ctx,w.run,w.job,w.plan
    local deadline = claim and P.safe_add(ctx.now_ms,60000) or job.n.lease_expires_at_ms
    if not deadline then return nil,"INVALID_NUMBER" end
    local receipt,err = Request.pending_record(ctx,run,job,intent.v,claim or job.v.lease_delivery_started == "0",decimal(deadline))
    if not receipt then return nil,err end
    local changes = {active_reservation_id=intent.reservation_id,next_request_ordinal=decimal(job.n.next_request_ordinal+1),updated_at_ms=ctx.now_text}
    local indices = {}
    local changes_run = {pending_request_reservations=1,reservation_creations_total=1}
    local sets = {last_activity_at_ms=ctx.now_text}
    if claim then
        changes.state,changes.lease_owner,changes.lease_token,changes.lease_fence = "leased",intent.v.owner_id,intent.v.lease_token,intent.v.fence
        changes.claim_count,changes.lease_started_at_ms,changes.lease_expires_at_ms = decimal(job.n.claim_count+1),ctx.now_text,decimal(deadline)
        changes.lease_request_starts_baseline,changes.lease_delivery_started = job.v.request_starts,"0"
        changes.last_transition_id,changes.last_transition_status = ctx.request.v.transition_id,"CLAIMED"
        indices,changes_run.claims_total,sets.last_execution_at_ms = {ready=-1,ready_at=-1,leased=1,leased_at=1},1,ctx.now_text
    end
    local post,why = project_job(job,changes); if not post then return nil,why end
    local ok,code = write_projection(plan,prefix.."reservation:"..intent.reservation_id,nil,receipt); if not ok then return nil,code end
    ok,code = write_scopes(ctx,plan,scopes,posts,receipt,"reserve"); if not ok then return nil,code end
    local updated,failure = run_delta(ctx,plan,run,{run=changes_run,maps={group_pending={[intent.v.group_id]=1}},indexes=indices},sets)
    if not updated then return nil,failure end
    if claim then
        local base,id = prefix.."run:"..run.run_id,job.v.job_id
        for _,argv in ipairs({{"ZREM",base..":ready",id},{"ZREM",base..":ready_at",id},{"ZADD",base..":leased",decimal(deadline),id},
            {"ZADD",base..":leased_at",ctx.now_text,id},{"ZADD",prefix.."active_leases",decimal(deadline),run.run_id..":"..id}}) do
            ok,code = add(plan,argv); if not ok then return nil,code end
        end
    end
    ok,code = write_projection(plan,job.key,job,post); if not ok then return nil,code end
    if claim then return finish(ctx,plan,"CLAIMED",{intent.v.fence,decimal(deadline),intent.reservation_id,decimal(deadline)}) end
    return finish(ctx,plan,"RESERVED",{intent.reservation_id,decimal(deadline)})
end
function Request.prepare_claim(ctx)
    local id,err = Request.claim_identity(ctx.request.v); if not id then return nil,err end
    local w,code = load_worker(ctx); if not w then return nil,code end
    local run,job,a,plan = w.run,w.job,ctx.request.v,w.plan
    local source,why = claim_source(a,run,job); if not source then return nil,why end
    if not job.exists then return finish(ctx,plan,"NO_CANDIDATE") end
    local intent,failure = Request.intent(a,run,job,true); if not intent then return nil,failure end
    local receipt; receipt,code = select_reservation(ctx,intent.reservation_id); if not receipt then return nil,code end
    if receipt.exists and not same_intent(receipt,intent) then return nil,"IMMUTABLE_MISMATCH" end
    if job.v.state == "completed" and job.v.last_transition_status == "VISITED_COMPLETED" and job.v.last_transition_id == id then
        if receipt.exists then return nil,"RESERVATION_CORRUPT" end
        return finish(ctx,plan,"VISITED_COMPLETED",{job.v.completed_at_ms})
    end
    -- A competing claim cannot mutate or replay this leased job. Only the exact
    -- active identity needs its full current reservation proof; no read grant
    -- for an unrelated submitted intent is invented from the job's pointer.
    if job.v.state == "leased" and (not lease_matches(job,a) or job.v.active_reservation_id ~= intent.reservation_id or
       ctx.now_ms >= job.n.lease_expires_at_ms) then return finish(ctx,plan,"LEASE_LOST",{job.v.lease_fence}) end
    local current; current,code = Request.load_live(ctx,run,job); if current == nil then return nil,code end
    if current and same_intent(current,intent) and current.v.state == "pending" and lease_matches(job,a) and ctx.now_ms < job.n.lease_expires_at_ms then
        if job.v.last_transition_id ~= id or job.v.last_transition_status ~= "CLAIMED" then return nil,"IMMUTABLE_MISMATCH" end
        return finish(ctx,plan,"ALREADY_CLAIMED",{job.v.lease_fence,job.v.lease_expires_at_ms,intent.reservation_id,current.v.expires_at_ms})
    end
    local status,state_error = authority_status(ctx,run); if state_error then return nil,state_error end
    if status then return finish(ctx,plan,status) end
    if job.v.state ~= "ready" then
        if job.v.state == "leased" then return finish(ctx,plan,"LEASE_LOST",{job.v.lease_fence}) end
        return finish(ctx,plan,"NO_CANDIDATE")
    end
    if a.expected_prior_fence ~= job.v.lease_fence then return finish(ctx,plan,"LEASE_LOST",{job.v.lease_fence}) end
    if receipt.exists then return nil,"RESERVATION_CORRUPT" end
    local mutable; mutable,code = Run.mutable(ctx,run); if not mutable then return nil,code end
    local visited,visit_error = visit(ctx,run,job); if visited == nil then return nil,visit_error end
    if visited then return claim_visited(w) end
    if a.request_ordinal ~= job.v.next_request_ordinal or receipt.exists or job.n.delivery_attempts >= 3 then return nil,"INVALID_STATE" end
    local scopes; scopes,code = select_scopes(ctx,intent.v); if not scopes then return nil,code end
    local posts; posts,code = scope_posts(ctx,intent,scopes); if not posts then return nil,code end
    local capacity; capacity,code = stage_admission_facts(ctx,run); if not capacity then return nil,code end
    local blocked_status,tail = blocked(ctx,run,job,intent,scopes,posts,true,capacity.slots,capacity.leases)
    if blocked_status then
        local ok,why = write_scopes(ctx,plan,scopes,posts,nil,"tighten"); if not ok then return nil,why end
        return finish(ctx,plan,blocked_status,tail)
    end
    return create_reservation(w,intent,scopes,posts,true)
end
function Request.prepare_reserve(ctx)
    local w,err = load_worker(ctx); if not w then return nil,err end
    local run,job,a,plan = w.run,w.job,ctx.request.v,w.plan
    if not job.exists then return finish(ctx,plan,"LEASE_LOST",{"0"}) end
    local intent,code = Request.intent(a,run,job,job.v.state == "leased" and job.v.lease_delivery_started == "0")
    if not intent then return nil,code end
    local receipt; receipt,code = select_reservation(ctx,intent.reservation_id); if not receipt then return nil,code end
    if receipt.exists and not same_intent(receipt,intent) then return nil,"IMMUTABLE_MISMATCH" end
    if not lease_matches(job,a) or ctx.now_ms >= job.n.lease_expires_at_ms then return finish(ctx,plan,"LEASE_LOST",{job.v.lease_fence}) end
    if job.v.active_reservation_id ~= "" and job.v.active_reservation_id ~= intent.reservation_id then return nil,"INVALID_STATE" end
    local current; current,code = Request.load_live(ctx,run,job); if current == nil then return nil,code end
    if current and same_intent(current,intent) and lease_matches(job,a) and ctx.now_ms < job.n.lease_expires_at_ms then
        return finish(ctx,plan,"ALREADY_RESERVED",{intent.reservation_id,current.v.expires_at_ms})
    end
    if not lease_matches(job,a) or ctx.now_ms >= job.n.lease_expires_at_ms then return finish(ctx,plan,"LEASE_LOST",{job.v.lease_fence}) end
    if job.n.last_stage_fence >= job.n.lease_fence then return nil,"INVALID_STATE" end
    local status,state_error = authority_status(ctx,run); if state_error then return nil,state_error end
    if status then return finish(ctx,plan,status) end
    if current or receipt.exists or a.request_ordinal ~= job.v.next_request_ordinal then return nil,"INVALID_STATE" end
    local mutable; mutable,code = Run.mutable(ctx,run); if not mutable then return nil,code end
    local scopes; scopes,code = select_scopes(ctx,intent.v); if not scopes then return nil,code end
    local posts; posts,code = scope_posts(ctx,intent,scopes); if not posts then return nil,code end
    local status,tail = blocked(ctx,run,job,intent,scopes,posts,false)
    if status then
        local ok,why = write_scopes(ctx,plan,scopes,posts,nil,"tighten"); if not ok then return nil,why end
        return finish(ctx,plan,status,tail)
    end
    return create_reservation(w,intent,scopes,posts,false)
end
local function start_tail(record,permission)
    local v = record.v
    return {v.reservation_id,v.started_at_ms,v.delivery_attempts_after_start,v.job_starts_after_start,v.run_starts_after_start,v.group_starts_after_start,permission}
end
local function first_evidence(ctx,receipt,global)
    local first,code = R.fixed_hash(ctx,prefix.."first_request_start","first_request_start"); if not first then return nil,code end
    local ttl,err = R.ttl(ctx,prefix.."first_request_start"); if not ttl then return nil,err end
    if first.exists then
        if ttl.ttl_ms ~= -1 or first.n.started_at_ms > ctx.now_ms or
           (receipt.n.started_at_ms > 0 and first.n.started_at_ms > receipt.n.started_at_ms) or
           (global and first.n.started_at_ms > global.n.last_started_at_ms) then return nil,"STATE_INDEX_CORRUPT" end
    elseif receipt.n.started_at_ms > 0 or (global and global.n.last_started_at_ms > 0) then return nil,"STATE_INDEX_CORRUPT" end
    return first
end
function Request.prepare_start(ctx)
    local w,err = load_worker(ctx); if not w then return nil,err end
    local run,job,a,plan = w.run,w.job,ctx.request.v,w.plan
    if not job.exists then return finish(ctx,plan,"LEASE_LOST",{"0"}) end
    local raw,code = select_reservation(ctx,a.reservation_id); if not raw then return nil,code end
    if not raw.exists then return finish(ctx,plan,"LEASE_LOST",{job.v.lease_fence}) end
    local receipt; receipt,code = Request.check_receipt({ctx=ctx},a.reservation_id); if not receipt then return nil,code end
    if not same_lease_record(receipt,a) then return nil,"IMMUTABLE_MISMATCH" end
    local bound; bound,code = C.bind_request(ctx); if not bound then return nil,code end
    local live
    if receipt.live_state then
        local scopes; scopes,code = select_scopes(ctx,receipt.v); if not scopes then return nil,code end
        live,code = Request.check_live({ctx=ctx},run,job,a.reservation_id); if not live then return nil,code end
    end
    local first_start; first_start,code = first_evidence(ctx,receipt,live and live.scopes.global); if not first_start then return nil,code end
    if receipt.n.started_at_ms > 0 then
        return finish(ctx,plan,"ALREADY_STARTED",start_tail(receipt,live and live.eligible_now and "1" or "0"))
    end
    if not live or not lease_matches(job,a) or ctx.now_ms >= job.n.lease_expires_at_ms then return finish(ctx,plan,"LEASE_LOST",{job.v.lease_fence}) end
    if job.n.last_stage_fence >= job.n.lease_fence then return nil,"INVALID_STATE" end
    local status,state_error = authority_status(ctx,run); if state_error then return nil,state_error end
    if status then return finish(ctx,plan,status) end
    local mutable; mutable,code = Run.mutable(ctx,run); if not mutable then return nil,code end
    for _,kind in ipairs({"global","group","origin"}) do
        local scope = live.scopes[kind]
        if ctx.now_ms < scope.n.next_allowed_ms then return finish(ctx,plan,"RATE_BLOCKED",{scope.v.scope_id,scope.v.next_allowed_ms,job.v.lease_delivery_started}) end
    end
    local first = job.v.lease_delivery_started == "0"
    local record; record,code = project_request(receipt,{state="started",started_at_ms=ctx.now_text,
        delivery_attempts_after_start=decimal(job.n.delivery_attempts+(first and 1 or 0)),job_starts_after_start=decimal(job.n.request_starts+1),
        run_starts_after_start=decimal(run.n.request_starts+1),group_starts_after_start=decimal(run.maps.group_started.n[receipt.v.group_id]+1)})
    if not record then return nil,code end
    local changes = {delivery_attempts=record.v.delivery_attempts_after_start,request_starts=record.v.job_starts_after_start,
        lease_delivery_started="1",last_request_started_at_ms=ctx.now_text,updated_at_ms=ctx.now_text}
    if one(receipt.v.request_kind,"document","redirect") then
        changes.last_document_request_started_at_ms,changes.last_document_request_fence = ctx.now_text,a.fence
        changes.last_document_target_url_id,changes.last_document_target_url,changes.last_document_target_digest = receipt.v.target_url_id,receipt.v.canonical_target_url,receipt.v.target_digest
    end
    local post; post,code = project_job(job,changes); if not post then return nil,code end
    local ok; ok,code = write_projection(plan,receipt.key,receipt,record); if not ok then return nil,code end
    ok,code = write_scopes(ctx,plan,live.scopes,live.scopes,record,"start"); if not ok then return nil,code end
    ok,code = write_projection(plan,job.key,job,post); if not ok then return nil,code end
    local changed; changed,code = run_delta(ctx,plan,run,{run={pending_request_reservations=-1,started_request_reservations=1,request_starts=1},
        maps={group_pending={[receipt.v.group_id]=-1},group_active_started={[receipt.v.group_id]=1},group_started={[receipt.v.group_id]=1}}},
        {last_activity_at_ms=ctx.now_text,last_execution_at_ms=ctx.now_text,last_request_started_at_ms=ctx.now_text})
    if not changed then return nil,code end
    if not first_start.exists then
        local evidence = S.project("first_request_start",{protocol_version="2",run_id=a.run_id,job_id=a.job_id,lease_fence=a.fence,started_at_ms=ctx.now_text})
        if not evidence then return nil,"INVALID_STATE" end
        ok,code = write_projection(plan,prefix.."first_request_start",nil,evidence); if not ok then return nil,code end
    end
    return finish(ctx,plan,"STARTED",start_tail(record,"1"))
end
-- Shared terminal effects: no job write, Run.flush, assessment, seal or execution.
-- Per-plan scope projections are private, so two requests sharing a scope debit
-- two members/counter units, not the same original count twice. Every caller must
-- discard its plan/delta on failure; a failed build is not a rollback facility.
local terminal_plans = {}
function Request.plan_terminal(ctx,plan,delta,run,job,state,coverage)
    if not clock(ctx) or not one(state,"cancelled","finished","expired") or
       (coverage ~= "ordinary" and not plain(coverage)) then return nil,"INVALID_ARGUMENT" end
    local held,code = Request.load_live(ctx,run,job)
    if held == nil then return nil,code end
    if held == false then return nil,"INVALID_STATE" end
    local current = held.job
    if state == "expired" then
        if ctx.operation ~= "CJ2_RECOVER_EXPIRED" or coverage == "ordinary" or not held.logical_expired then return nil,"INVALID_STATE" end
    else
        if ctx.operation == "CJ2_RECOVER_EXPIRED" or held.logical_expired or not same_lease_record(held,ctx.request.v) or
           held.v.state ~= (state == "cancelled" and "pending" or "started") then return nil,"INVALID_STATE" end
    end
    local mutable,err = Run.mutable(ctx,held.run); if not mutable then return nil,err end
    local expires = P.safe_add(ctx.now_ms,DAY); if not expires then return nil,"INVALID_NUMBER" end
    local record; record,code = project_request(held,{state=state,terminal_at_ms=ctx.now_text}); if not record then return nil,code end
    local job_fields = {active_reservation_id="",updated_at_ms=ctx.now_text}
    if not project_job(current,job_fields) then return nil,"INVALID_STATE" end
    local prior = terminal_plans[plan]
    if prior and (prior.ctx ~= ctx or prior.delta ~= delta or prior.run_id ~= run.run_id or prior.records[held.v.reservation_id]) then
        return nil,"INVALID_STATE"
    end
    local scopes = {}
    for _,kind in ipairs({"global","group","origin"}) do
        local id = held.v[kind.."_scope_id"]
        scopes[kind] = prior and prior.scopes[id] or held.scopes[kind]
    end
    local pending = held.v.state == "pending"
    local changes = {run={},maps={}}
    changes.run[pending and "pending_request_reservations" or "started_request_reservations"] = -1
    changes.maps[pending and "group_pending" or "group_active_started"] = {[held.v.group_id]=-1}
    -- Coverage is the caller's exact opaque core unit (or ordinary), never a
    -- computed G, a copy of a unit, or the attribution of another request.
    local ok; ok,code = Run.accumulate(delta,changes,coverage); if not ok then return nil,code end
    ok,code = Run.set(delta,{last_activity_at_ms=ctx.now_text},coverage); if not ok then return nil,code end
    ok,code = write_projection(plan,held.key,held,record,coverage); if not ok then return nil,code end
    ok,code = add(plan,{"PEXPIREAT",held.key,decimal(expires)},coverage); if not ok then return nil,code end
    local posts; posts,code = write_scopes(ctx,plan,scopes,scopes,record,pending and "cancel" or "finish",coverage)
    if not posts then return nil,code end
    local memo = prior or {ctx=ctx,delta=delta,run_id=run.run_id,records={},scopes={}}
    for _,kind in ipairs({"global","group","origin"}) do memo.scopes[held.v[kind.."_scope_id"]] = posts[kind] end
    memo.records[held.v.reservation_id],terminal_plans[plan] = true,memo
    return {changed=true,reservation_id=held.v.reservation_id,previous_state=held.v.state,state=state,
        charged_group_id=held.v.group_id,source_group_id=current.v.group_id,job_key=current.key,
        job_fields=job_fields,record=record,scopes=copy(posts),tombstone_expires_at_ms=decimal(expires),run_changes=copy(changes)}
end
function Request.prepare_terminal(ctx)
    local w,err = load_worker(ctx); if not w then return nil,err end
    local run,job,a,plan = w.run,w.job,ctx.request.v,w.plan
    if not job.exists then return finish(ctx,plan,"LEASE_LOST",{"0"}) end
    local raw,code = select_reservation(ctx,a.reservation_id); if not raw then return nil,code end
    if not raw.exists then return finish(ctx,plan,"LEASE_LOST",{job.v.lease_fence}) end
    local receipt; receipt,code = Request.check_receipt({ctx=ctx},a.reservation_id); if not receipt then return nil,code end
    if not same_lease_record(receipt,a) then return nil,"IMMUTABLE_MISMATCH" end
    local bound; bound,code = C.bind_request(ctx); if not bound then return nil,code end
    local cancelling = ctx.operation == "CJ2_CANCEL_RESERVATION"
    local state,status = cancelling and "cancelled" or "finished",cancelling and "RESERVATION_CANCELLED" or "FINISHED"
    if receipt.v.state == state then return finish(ctx,plan,cancelling and status or "ALREADY_FINISHED",{a.reservation_id}) end
    if not receipt.live_state then return finish(ctx,plan,"LEASE_LOST",{job.v.lease_fence}) end
    local live; live,code = Request.load_live(ctx,run,job); if live == nil then return nil,code end
    if not live or live.v.reservation_id ~= a.reservation_id then return nil,"RESERVATION_CORRUPT" end
    if not lease_matches(job,a) or live.logical_expired then return finish(ctx,plan,"LEASE_LOST",{job.v.lease_fence}) end
    if receipt.v.state ~= (cancelling and "pending" or "started") then return nil,"INVALID_STATE" end
    local admitted,policy_error = CJ.Plan.set_policy(plan,"safety")
    if not admitted then return nil,policy_error end
    local coverage = "ordinary"
    local delta; delta,code = Run.plan_delta(ctx,run); if not delta then return nil,code end
    local effects; effects,code = Request.plan_terminal(ctx,plan,delta,run,job,state,coverage); if not effects then return nil,code end
    local post; post,code = project_job(job,effects.job_fields); if not post then return nil,code end
    local ok; ok,code = write_projection(plan,job.key,job,post,coverage); if not ok then return nil,code end
    local changed; changed,code = Run.flush(ctx,plan,delta); if not changed then return nil,code end
    return finish(ctx,plan,status,{a.reservation_id})
end
function Request.prepare_renew(ctx)
    local w,err = load_worker(ctx); if not w then return nil,err end
    local run,job,a,plan = w.run,w.job,ctx.request.v,w.plan
    if not job.exists then return finish(ctx,plan,"LEASE_LOST",{"0"}) end
    if job.v.state ~= "leased" or run.n.finalized_at_ms > 0 then return finish(ctx,plan,"LEASE_LOST",{job.v.lease_fence}) end
    local current,code = Request.load_live(ctx,run,job); if current == nil then return nil,code end
    local stage; stage,code = select_stage(ctx,run,job); if not stage then return nil,code end
    if stage.state == "aborted" then return nil,"INVALID_STATE" end
    if not lease_matches(job,a) or ctx.now_ms >= job.n.lease_expires_at_ms then
        local mutable,why = Run.mutable(ctx,run); if not mutable then return nil,why end
        local changed; changed,code = run_delta(ctx,plan,run,{run={renewal_rejections_total=1}})
        if not changed then return nil,code end
        return finish(ctx,plan,"LEASE_LOST",{job.v.lease_fence})
    end
    local status,state_error = authority_status(ctx,run); if state_error then return nil,state_error end
    if status then return finish(ctx,plan,status) end
    local mutable; mutable,code = Run.mutable(ctx,run); if not mutable then return nil,code end
    local deadline = P.safe_add(ctx.now_ms,60000); if not deadline then return nil,"INVALID_NUMBER" end
    if stage.has_stage then deadline = math.min(deadline,stage.expires_at_ms) end
    -- Request.check_live and Job.check already bound ALL matching scores to the
    -- stored lease expiry. Never replace this prescribed deadline with a max.
    if deadline < job.n.lease_expires_at_ms then return nil,"INVALID_STATE" end
    local text = decimal(deadline)
    local post; post,code = project_job(job,{lease_expires_at_ms=text,updated_at_ms=ctx.now_text}); if not post then return nil,code end
    if current then
        local record; record,code = project_request(current,{expires_at_ms=text}); if not record then return nil,code end
        local ok; ok,code = write_projection(plan,current.key,current,record); if not ok then return nil,code end
        ok,code = write_scopes(ctx,plan,current.scopes,current.scopes,record,"renew"); if not ok then return nil,code end
    end
    for _,argv in ipairs({{"ZADD",prefix.."run:"..run.run_id..":leased",text,job.v.job_id},
        {"ZADD",prefix.."active_leases",text,run.run_id..":"..job.v.job_id}}) do
        local ok; ok,code = add(plan,argv); if not ok then return nil,code end
    end
    local ok; ok,code = write_projection(plan,job.key,job,post); if not ok then return nil,code end
    local changed; changed,code = run_delta(ctx,plan,run,{},{last_activity_at_ms=ctx.now_text}); if not changed then return nil,code end
    return finish(ctx,plan,"RENEWED",{text})
end

Request.Rate = Rate
return Request
