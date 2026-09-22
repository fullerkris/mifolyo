-- Run ledger module factory. Lexical P/CJ; no Redis access during registration.
-- The record has 59 fields. Identity belongs to the validated key, never a
-- synthetic run_id hash field. Archive/purge evidence is deliberately retained.
-- Assembly: shared core/aliases, this source as CJ.Run, then optional Job and
-- operation chunks. No URL module is required. Registration is pure and must
-- precede the first Context.open.
--
-- Integration API (all fallible calls return value/code):
--   validate(v) -> complete schema projection; validate_ledger(run) -> true/code
--   load(ctx,run_id) -> {run_id,key,v,n,record,groups,group_ids,maps,reasons,
--                        indexes,inventory,all_open_groups_exhausted}
--     groups is the normal Schemas.groups projection (ordered/by_id/count/digest).
--     Map/reason facts retain explicit Read receipts plus parsed .n/.sum.
--     Index facts prove cardinalities, NOT individual job membership.
--   mutable(ctx,run) -> rejects purge progress and future activity/archive time
--   live(ctx,run) -> bounded lease/slot/expiry facts and owned/drained counts
--   plan_delta(ctx,run), accumulate(handle,{run={},maps={},indexes={}},coverage?),
--     set(handle,{lifecycle_field=text},coverage?), begin_audit(handle),
--     member(handle,"active_runs"|"unarchived_runs",boolean), flush(ctx,plan,handle)
--   key(ctx,suffix), hset(plan,key,values,coverage), finish(ctx,plan,status,tail)
-- One delta handle per run/batch; flush validates the aggregate post-state and
-- emits one HSET per changed hash for ordinary callers (unchanged). RECOVER must
-- supply the exact Plan.recovery_unit handle to EVERY accumulate/set call and
-- omit flush's whole-plan coverage argument. Nonzero deltas and changed lifecycle
-- values record all contributing handles without cloning their opaque identity.
-- Flush computes each final field once, then partitions disjoint fields by their
-- first real contributor: hash/field byte order, owner first-contribution order.
-- Before insertion, Plan.validate_unit authenticates EVERY contributing handle,
-- including shared-only/no-op contributors, against the exact destination plan.
-- Plan.add receives each partition's original opaque owner. Core assessment
-- independently derives counter contributions from final Job/Request effects
-- and charges that field's actual G once, whether slotted or unslotted. Run never
-- guesses slot ownership, conflates source and charged-request groups, supplies
-- G, or substitutes a convenient safety owner. Any error aborts the whole plan.
-- Job/Request/stage planners still must prove
-- each selected membership, own their index descriptors and authorization,
-- and call the shared Gate before load. These helpers do not authorize an
-- operation, inspect reservation/job/stage hashes, or execute writes themselves.
local Run, I, S, R, C = {}, CJ.Identities, CJ.Schemas, CJ.Read, CJ.Context
local function words(text)
    local result = {}
    for word in string.gmatch(text, "%S+") do result[#result+1] = word end
    return result
end
local names = words([[protocol_version contract_sha256 state source_kind source_sha256
expected_seed_count authorization_sha256 authorization_scope_sha256 authorization_expires_at_ms
canonicalization_version canonicalization_sha256 crawl_policy_version crawl_policy_sha256
render_policy_version render_policy_sha256 policy_group_count policy_group_map_sha256 max_jobs
max_request_starts global_concurrency_limit max_delivery_attempts job_count open_job_count
request_starts reservation_creations_total pending_request_reservations started_request_reservations
claims_total retries_total recovered_leases_total renewal_rejections_total completed_total dead_total
cancelled_total output_commits_total load_revision audit_revision audit_count audit_cursor audit_complete
created_at_ms sealed_at_ms activated_at_ms budget_exhausted_at_ms cancelled_at_ms completed_at_ms
finalized_at_ms last_activity_at_ms last_execution_at_ms last_request_started_at_ms
last_terminal_transition_at_ms retention_anchor_ms archived_at_ms archive_sha256 purge_state
purge_evidence_sha256 purge_started_at_ms purged_job_count terminal_reason]])
local numeric = words([[expected_seed_count authorization_expires_at_ms canonicalization_version
crawl_policy_version render_policy_version policy_group_count max_jobs max_request_starts
global_concurrency_limit max_delivery_attempts job_count open_job_count request_starts
reservation_creations_total pending_request_reservations started_request_reservations claims_total
retries_total recovered_leases_total renewal_rejections_total completed_total dead_total cancelled_total
output_commits_total load_revision audit_revision audit_count created_at_ms sealed_at_ms activated_at_ms
budget_exhausted_at_ms cancelled_at_ms completed_at_ms finalized_at_ms last_activity_at_ms
last_execution_at_ms last_request_started_at_ms last_terminal_transition_at_ms retention_anchor_ms
archived_at_ms purge_started_at_ms purged_job_count]])
local digests = words([[contract_sha256 source_sha256 authorization_sha256 authorization_scope_sha256
canonicalization_sha256 crawl_policy_sha256 render_policy_sha256 policy_group_map_sha256]])
local time_fields = words([[sealed_at_ms activated_at_ms budget_exhausted_at_ms cancelled_at_ms
completed_at_ms finalized_at_ms last_execution_at_ms last_request_started_at_ms
last_terminal_transition_at_ms retention_anchor_ms archived_at_ms purge_started_at_ms]])
local bounds, limits = {}, {protocol_version=1,state=16,source_kind=12,audit_cursor=64,
    audit_complete=1,archive_sha256=64,purge_state=11,purge_evidence_sha256=64,terminal_reason=64}
for _, name in ipairs(numeric) do limits[name] = 16 end
for _, name in ipairs(digests) do limits[name] = 64 end
for i, name in ipairs(names) do bounds[i] = limits[name] end
local function one(value, ...)
    for _, candidate in ipairs({...}) do if value == candidate then return true end end
    return false
end
local function zero(n, fields)
    for _, field in ipairs(fields) do if n[field] ~= 0 then return false end end
    return true
end
local pre_execution = words([[request_starts reservation_creations_total pending_request_reservations
started_request_reservations claims_total retries_total recovered_leases_total renewal_rejections_total
completed_total dead_total output_commits_total last_execution_at_ms last_request_started_at_ms]])
local function audit_not_started(v, n)
    return n.audit_revision == 0 and n.audit_count == 0 and v.audit_cursor == "" and v.audit_complete == "0"
end
local function audit_in_progress(v, n)
    if n.audit_revision == 0 or n.audit_revision ~= n.load_revision or
       n.job_count > n.expected_seed_count or n.audit_count > n.job_count then return false end
    if v.audit_complete == "1" then return n.audit_count == n.job_count and n.job_count == n.expected_seed_count end
    return n.job_count == 0 or n.audit_count ~= n.job_count
end
local function audit_complete(v, n, discoveries)
    return n.audit_revision > 0 and n.audit_revision == n.load_revision and
        n.audit_count == n.expected_seed_count and v.audit_complete == "1" and
        ((discoveries and n.job_count >= n.expected_seed_count) or (not discoveries and n.job_count == n.expected_seed_count))
end
local function cancelled_prefix(v, n)
    if n.activated_at_ms > 0 then return audit_complete(v,n,true) end
    if n.sealed_at_ms > 0 then return audit_complete(v,n,false) end
    if n.audit_revision > 0 then return audit_in_progress(v,n) end
    return n.job_count <= n.expected_seed_count and audit_not_started(v,n)
end
local function purge_clear(v, n)
    return v.purge_state == "none" and v.purge_evidence_sha256 == "" and n.purge_started_at_ms == 0 and n.purged_job_count == 0
end
local function budget_reason(v, n)
    if v.terminal_reason == "request_budget_exhausted" then return n.request_starts == n.max_request_starts end
    if v.terminal_reason == "reservation_limit_exhausted" then
        return n.request_starts < n.max_request_starts and n.reservation_creations_total == 100
    end
    return v.terminal_reason == "group_budgets_exhausted" and n.request_starts < n.max_request_starts and n.reservation_creations_total < 100
end
local function finalization(n)
    return n.finalized_at_ms > 0 and n.retention_anchor_ms == math.max(n.finalized_at_ms,
        n.last_request_started_at_ms,n.last_terminal_transition_at_ms,n.last_activity_at_ms)
end
-- Pure complete record relations. The registration adapter below explicitly
-- requires every canonical numeric parse supplied by Schemas.project; missing
-- parses are never zero. This predicate is private, not a registration callback.
local function valid_record(v, n)
    for _, field in ipairs(digests) do if not I.digest(v[field]) then return false end end
    if v.protocol_version ~= "2" or not one(v.source_kind,"mongo","v1_migration") or
       not one(v.state,"loading","auditing","sealed","active","completed","budget_exhausted","cancelled","archived") then return false end
    -- Checked additions reject overflow, never round a malicious aggregate.
    local terminal = P.safe_add(n.completed_total,n.dead_total)
    terminal = terminal and P.safe_add(terminal,n.cancelled_total)
    local reservations = P.safe_add(n.pending_request_reservations,n.started_request_reservations)
    if not terminal or not reservations or n.expected_seed_count > 10000 or
       (v.source_kind == "v1_migration" and n.expected_seed_count == 0) or
       n.policy_group_count < 1 or n.policy_group_count > 64 or n.max_jobs ~= 10000 or
       n.max_request_starts < 1 or n.max_request_starts > 10 or n.global_concurrency_limit ~= 2 or
       n.max_delivery_attempts ~= 3 or n.job_count > n.max_jobs or n.open_job_count > n.job_count or
       n.request_starts > n.max_request_starts or n.reservation_creations_total > 100 or
       n.request_starts > n.reservation_creations_total or reservations > n.reservation_creations_total or
       n.claims_total > n.reservation_creations_total or n.retries_total > n.claims_total or
       n.retries_total > n.request_starts or n.recovered_leases_total > n.claims_total or
       terminal > n.job_count or n.open_job_count ~= n.job_count-terminal or n.output_commits_total > n.completed_total or
       n.load_revision == 0 or n.audit_revision > n.load_revision or n.audit_count > n.job_count or
       n.purged_job_count > n.job_count or n.created_at_ms == 0 or n.last_activity_at_ms < n.created_at_ms then return false end
    if n.authorization_expires_at_ms <= n.created_at_ms or n.authorization_expires_at_ms-n.created_at_ms > 86400000 or
       v.canonicalization_version ~= "1" or v.crawl_policy_version ~= "2" or n.render_policy_version == 0 or
       not one(v.audit_complete,"0","1") or (v.audit_cursor == "") ~= (n.audit_count == 0) or
       (v.audit_cursor ~= "" and not I.hex(v.audit_cursor,64)) then return false end
    if (n.request_starts == 0) ~= (n.last_request_started_at_ms == 0) or
       (terminal == 0) ~= (n.last_terminal_transition_at_ms == 0) or
       n.last_execution_at_ms > n.last_activity_at_ms or n.last_request_started_at_ms > n.last_activity_at_ms or
       n.last_terminal_transition_at_ms > n.last_activity_at_ms then return false end
    for _, field in ipairs(time_fields) do
        if n[field] ~= 0 and n[field] < n.created_at_ms then return false end
    end
    for _, field in ipairs(words("sealed_at_ms activated_at_ms budget_exhausted_at_ms cancelled_at_ms completed_at_ms finalized_at_ms")) do
        if n[field] > n.last_activity_at_ms then return false end
    end
    if n.activated_at_ms > 0 and (n.sealed_at_ms == 0 or n.activated_at_ms < n.sealed_at_ms) or
       n.budget_exhausted_at_ms > 0 and (n.activated_at_ms == 0 or n.budget_exhausted_at_ms < n.activated_at_ms) or
       n.completed_at_ms > 0 and (n.activated_at_ms == 0 or n.completed_at_ms < n.activated_at_ms) then return false end
    if (v.archive_sha256 == "") ~= (n.archived_at_ms == 0) or
       (v.archive_sha256 ~= "" and not I.digest(v.archive_sha256)) then return false end
    if v.purge_state == "none" then
        if not purge_clear(v,n) then return false end
    elseif v.purge_state == "in_progress" then
        if not I.digest(v.purge_evidence_sha256) or n.purge_started_at_ms == 0 or
           n.purged_job_count == 0 or n.purged_job_count >= n.job_count then return false end
    else return false end
    local prefix = n.activated_at_ms > 0 and n.activated_at_ms or (n.sealed_at_ms > 0 and n.sealed_at_ms or n.created_at_ms)
    if n.cancelled_at_ms > 0 and n.cancelled_at_ms < prefix or
       n.finalized_at_ms > 0 and n.finalized_at_ms < math.max(n.budget_exhausted_at_ms,n.cancelled_at_ms,n.completed_at_ms) or
       n.archived_at_ms > 0 and n.archived_at_ms < n.finalized_at_ms then return false end
    local state = v.state
    if state == "loading" or state == "auditing" then
        return (state == "loading" and audit_not_started(v,n) or state == "auditing" and audit_in_progress(v,n)) and
            n.job_count <= n.expected_seed_count and zero(n,pre_execution) and terminal == 0 and
            zero(n,words("sealed_at_ms activated_at_ms budget_exhausted_at_ms cancelled_at_ms completed_at_ms finalized_at_ms retention_anchor_ms archived_at_ms")) and
            v.terminal_reason == "none" and purge_clear(v,n)
    elseif state == "sealed" then
        return audit_complete(v,n,false) and zero(n,pre_execution) and terminal == 0 and n.sealed_at_ms > 0 and
            zero(n,words("activated_at_ms budget_exhausted_at_ms cancelled_at_ms completed_at_ms finalized_at_ms retention_anchor_ms archived_at_ms")) and
            v.terminal_reason == "none" and purge_clear(v,n)
    elseif state == "active" then
        return audit_complete(v,n,true) and n.sealed_at_ms > 0 and n.activated_at_ms > 0 and
            zero(n,words("budget_exhausted_at_ms cancelled_at_ms completed_at_ms finalized_at_ms retention_anchor_ms archived_at_ms")) and
            v.terminal_reason == "none" and purge_clear(v,n)
    elseif state == "completed" then
        return audit_complete(v,n,true) and n.sealed_at_ms > 0 and n.activated_at_ms > 0 and n.open_job_count == 0 and
            reservations == 0 and n.completed_at_ms > 0 and n.completed_at_ms == n.finalized_at_ms and
            zero(n,words("budget_exhausted_at_ms cancelled_at_ms archived_at_ms")) and v.terminal_reason == "all_jobs_terminal" and
            finalization(n) and purge_clear(v,n)
    elseif state == "budget_exhausted" then
        return audit_complete(v,n,true) and n.sealed_at_ms > 0 and n.activated_at_ms > 0 and n.open_job_count > 0 and
            reservations == 0 and n.budget_exhausted_at_ms > 0 and n.budget_exhausted_at_ms == n.finalized_at_ms and
            zero(n,words("cancelled_at_ms completed_at_ms archived_at_ms")) and budget_reason(v,n) and finalization(n) and purge_clear(v,n)
    elseif state == "cancelled" then
        if not cancelled_prefix(v,n) or n.cancelled_at_ms == 0 or
           not zero(n,words("budget_exhausted_at_ms completed_at_ms archived_at_ms")) or
           not one(v.terminal_reason,"authorization_expired","operator_cancelled","source_cancelled") then return false end
        if n.activated_at_ms == 0 and not zero(n,pre_execution) then return false end
        if n.finalized_at_ms == 0 then
            if n.retention_anchor_ms ~= 0 then return false end
        elseif n.open_job_count ~= 0 or reservations ~= 0 or not finalization(n) then return false end
        if v.purge_state == "in_progress" and (v.source_kind ~= "v1_migration" or n.activated_at_ms ~= 0 or
           n.open_job_count ~= 0 or n.request_starts ~= 0 or n.reservation_creations_total ~= 0 or n.claims_total ~= 0 or reservations ~= 0) then return false end
        return true
    elseif state == "archived" then
        if n.archived_at_ms == 0 or v.archive_sha256 == "" or reservations ~= 0 or not finalization(n) or
           (v.purge_state == "in_progress" and v.purge_evidence_sha256 ~= v.archive_sha256) then return false end
        if v.terminal_reason == "all_jobs_terminal" then
            return audit_complete(v,n,true) and n.open_job_count == 0 and n.sealed_at_ms > 0 and n.activated_at_ms > 0 and
                n.completed_at_ms > 0 and n.completed_at_ms == n.finalized_at_ms and zero(n,words("budget_exhausted_at_ms cancelled_at_ms"))
        elseif one(v.terminal_reason,"request_budget_exhausted","reservation_limit_exhausted","group_budgets_exhausted") then
            return audit_complete(v,n,true) and n.open_job_count > 0 and n.sealed_at_ms > 0 and n.activated_at_ms > 0 and
                n.budget_exhausted_at_ms > 0 and n.budget_exhausted_at_ms == n.finalized_at_ms and
                zero(n,words("cancelled_at_ms completed_at_ms")) and budget_reason(v,n)
        elseif one(v.terminal_reason,"authorization_expired","operator_cancelled","source_cancelled") then
            return cancelled_prefix(v,n) and n.open_job_count == 0 and n.cancelled_at_ms > 0 and
                zero(n,words("budget_exhausted_at_ms completed_at_ms")) and (n.activated_at_ms > 0 or zero(n,pre_execution))
        end
    end
    return false
end
local registered, registration_error = S.register("run", {names=names,bounds=bounds}, function(v,n)
    for _, field in ipairs(numeric) do
        if n[field] == nil then return nil, "INVALID_NUMBER" end
    end
    if not valid_record(v,n) then return nil, "INVALID_ARGUMENT" end
    return true
end)
if not registered then return nil, registration_error end
function Run.validate(values)
    return S.project("run",values)
end
Run.names = names
Run.numeric = numeric
function Run.key(ctx, name)
    return name == "run" and ctx.keys.run or ctx.keys["run_"..name]
end
Run.group_maps = words([[group_limits group_rate_scope_ids group_scope_ids group_concurrency group_interval_ms
group_started group_pending group_active_started group_open_jobs]])
Run.retry_reasons = words([[request_timeout dns_temporary dial_temporary request_temporary http_429 http_5xx
robots_temporary renderer_temporary downstream_backpressure capacity_blocked_after_io run_budget_exhausted_after_io
group_budget_exhausted_after_io rate_blocked_after_io lease_expired_after_io worker_shutdown_after_io]])
Run.recovery_reasons = words("ready delayed dead cancelled")
Run.completion_reasons = words("published already_visited")
Run.dead_reasons = words([[policy_denied policy_scope_changed robots_denied robots_invalid job_malformed
url_identity_mismatch static_url_denied dns_prohibited http_4xx response_invalid body_too_large html_invalid
discovery_limit renderer_permanent output_invalid run_job_limit reservation_limit_exhausted retry_exhausted
pre_io_recovery_exhausted protocol_corrupt]])
Run.cancel_reasons = words("authorization_expired operator_cancelled source_cancelled")
Run.disposition_reasons = {}
for _, list in ipairs({Run.completion_reasons,Run.dead_reasons,Run.cancel_reasons}) do
    for _, name in ipairs(list) do Run.disposition_reasons[#Run.disposition_reasons+1] = name end
end
local reason_sets = {retry_reason_counts=Run.retry_reasons,recovery_outcome_counts=Run.recovery_reasons,
    disposition_reason_counts=Run.disposition_reasons}
-- Every reason field is present, including zeros. Dynamic reads bound Redis
-- replies first; exact closed field validation happens before any arithmetic.
local function sum_fields(fact, fields, maximum)
    if type(fact) ~= "table" or not fact.exists or not fact.complete or fact.kind ~= "hash" or
       fact.count ~= #fields or type(fact.v) ~= "table" then return nil, "COUNTER_CORRUPT" end
    local result, sum = {}, 0
    for _, field in ipairs(fields) do
        local value = P.parse_decimal(fact.v[field])
        if not value or value > maximum then return nil, "COUNTER_CORRUPT" end
        sum = P.safe_add(sum,value)
        if not sum then return nil, "COUNTER_CORRUPT" end
        result[field] = value
    end
    return {n=result,sum=sum}
end
local index_defs = {{"jobs","set",10000},{"job_order","zset",10000},{"ready","zset",10000},
    {"ready_at","zset",10000},{"leased","zset",64},{"leased_at","zset",64},{"delayed","zset",10000},
    {"commit_backpressure","zset",10},{"completed","zset",10000},{"dead","zset",10000},{"cancelled","zset",10000},
    {"visited_depth","hash",10000},{"visited_urls","hash",10000}}
Run.index_defs = index_defs
local function score_number(text)
    -- Timestamp scores are exact nonnegative integers, not scheduling priorities.
    -- Redis may render an exact large integer in exponent form. Native conversion
    -- is safe only with finite/integer/range checks; never accept NaN/infinity.
    local value = type(text) == "number" and text or I.redis_score(text)
    if not I.integer(value,P.limits.max_integer) then return nil end
    return value
end
Run.score_number = score_number
local function project_groups(v, n, maps, ids)
    local bytes = {}
    for _, name in ipairs(Run.group_maps) do
        local fact = maps[name]
        if type(fact) ~= "table" or not fact.exists or fact.kind ~= "hash" or not fact.complete or
           fact.count ~= n.policy_group_count or type(fact.v) ~= "table" then return nil, "COUNTER_CORRUPT" end
        for _, id in ipairs(ids) do
            if type(fact.v[id]) ~= "string" then return nil, "COUNTER_CORRUPT" end
        end
    end
    for i, id in ipairs(ids) do
        local group = S.project("policy_group",{group_id=id,rate_scope_id=maps.group_rate_scope_ids.v[id],
            group_scope_id=maps.group_scope_ids.v[id],request_start_limit=maps.group_limits.v[id],
            concurrency=maps.group_concurrency.v[id],interval_ms=maps.group_interval_ms.v[id]})
        if not group then return nil, "COUNTER_CORRUPT" end
        local encoded, code = S.encode(group)
        if not encoded then return nil, code end
        bytes[i] = encoded
    end
    local groups = S.groups(bytes)
    if not groups or groups.digest ~= v.policy_group_map_sha256 then return nil, "IMMUTABLE_MISMATCH" end
    return groups
end
function Run.inventory(ctx, run_id)
    local result = {}
    for _, def in ipairs({{"runs","zset",128},{"active_runs","set",16},{"unarchived_runs","set",100}}) do
        local fact, code = R.all_members(ctx,ctx.keys[def[1]],def[2],def[3],32)
        if not fact then return nil, code end
        for _, member in ipairs(fact.ordered) do
            if not I.hex(member,32) then return nil, "STATE_INDEX_CORRUPT" end
            if def[2] == "zset" then
                local timestamp = score_number(fact.scores[member])
                if not timestamp or timestamp == 0 then return nil, "STATE_INDEX_CORRUPT" end
            end
        end
        result[def[1]] = fact
    end
    for _, member in ipairs(result.active_runs.ordered) do
        if not result.unarchived_runs.members[member] then return nil, "STATE_INDEX_CORRUPT" end
    end
    for _, member in ipairs(result.unarchived_runs.ordered) do
        if not result.runs.members[member] then return nil, "STATE_INDEX_CORRUPT" end
    end
    result.run_id = run_id
    return result
end
-- Pure cross-record accounting, also used on the aggregated post-state by
-- flush. No job scan: inventory cardinalities are the bounded ledger proof.
function Run.validate_ledger(run)
    if type(run) ~= "table" or not I.hex(run.run_id,32) or type(run.maps) ~= "table" or
       type(run.indexes) ~= "table" or type(run.reasons) ~= "table" or type(run.inventory) ~= "table" then
        return nil, "COUNTER_CORRUPT"
    end
    local record = Run.validate(run.v)
    if not record then return nil, "COUNTER_CORRUPT" end
    local n, v, maps, ix = record.n, record.v, run.maps, run.indexes
    local ids = run.group_ids
    if I.dense(ids,64) ~= n.policy_group_count then return nil, "COUNTER_CORRUPT" end
    for i, id in ipairs(ids) do
        if not I.group(id) or (i > 1 and ids[i-1] >= id) then return nil, "COUNTER_CORRUPT" end
    end
    local groups, group_error = project_groups(v,n,maps,ids)
    if not groups then return nil, group_error end
    -- Never trust a caller-edited numeric/sum convenience projection. This also
    -- validates every aggregated post-state against the same per-map bounds.
    for _, name in ipairs(Run.group_maps) do
        local maximum = name == "group_interval_ms" and 3600000 or (name == "group_concurrency" and 32 or
            (name == "group_open_jobs" and 10000 or 10))
        if name ~= "group_rate_scope_ids" and name ~= "group_scope_ids" then
            local parsed, code = sum_fields(maps[name],ids,maximum)
            if not parsed then return nil, code end
            maps[name].n, maps[name].sum = parsed.n, parsed.sum
        end
    end
    if n.audit_revision == 0 then
        local fact = maps.audit_group_counts
        if type(fact) ~= "table" or fact.exists ~= false or fact.kind ~= "none" or
           not fact.complete or fact.count ~= 0 then return nil, "COUNTER_CORRUPT" end
    else
        local parsed, code = sum_fields(maps.audit_group_counts,ids,10000)
        if not parsed then return nil, code end
        maps.audit_group_counts.n, maps.audit_group_counts.sum = parsed.n, parsed.sum
    end
    for name, fields in next, reason_sets, nil do
        local parsed, code = sum_fields(run.reasons[name],fields,10000)
        if not parsed then return nil, code end
        run.reasons[name].n, run.reasons[name].sum = parsed.n, parsed.sum
    end
    for _, def in ipairs(index_defs) do
        local fact = ix[def[1]]
        if type(fact) ~= "table" or not I.integer(fact.count,def[3]) then return nil, "STATE_INDEX_CORRUPT" end
    end
    local sums = {group_started=0,group_pending=0,group_active_started=0,group_open_jobs=0,audit_group_counts=0}
    local all_exhausted = n.open_job_count > 0
    for _, id in ipairs(ids) do
        local started, pending, active, open = maps.group_started.n[id],maps.group_pending.n[id],
            maps.group_active_started.n[id],maps.group_open_jobs.n[id]
        local limit = maps.group_limits.n[id]
        -- Open jobs retain their immutable source group; reservations count the
        -- request group, which may differ for redirects/resources. Their shared
        -- bound is the run-wide leased count below, not this group's open jobs.
        if limit == 0 or maps.group_concurrency.n[id] == 0 or started+pending > limit or active > started or
           pending+active > maps.group_concurrency.n[id] then return nil, "COUNTER_CORRUPT" end
        if open > 0 and started < limit then all_exhausted = false end
        for name in next, sums, nil do
            local value = name == "audit_group_counts" and (n.audit_revision == 0 and 0 or maps[name].n[id]) or maps[name].n[id]
            sums[name] = P.safe_add(sums[name],value)
            if not sums[name] then return nil, "COUNTER_CORRUPT" end
        end
        if one(v.state,"auditing","sealed") then
            if maps.audit_group_counts.n[id] > open or (v.audit_complete == "1" and maps.audit_group_counts.n[id] ~= open) then
                return nil, "COUNTER_CORRUPT"
            end
        end
    end
    if sums.group_started ~= n.request_starts or sums.group_pending ~= n.pending_request_reservations or
       sums.group_active_started ~= n.started_request_reservations or sums.group_open_jobs ~= n.open_job_count or
        sums.audit_group_counts ~= n.audit_count or n.request_starts+n.pending_request_reservations > n.max_request_starts or
        n.request_starts+n.pending_request_reservations > n.reservation_creations_total or
       n.started_request_reservations > n.request_starts or
       n.pending_request_reservations+n.started_request_reservations > n.global_concurrency_limit or
       n.output_commits_total > n.request_starts then return nil, "COUNTER_CORRUPT" end
    if v.terminal_reason == "group_budgets_exhausted" and not all_exhausted then return nil, "COUNTER_CORRUPT" end
    local reasons = run.reasons
    if reasons.retry_reason_counts.sum ~= n.retries_total or reasons.recovery_outcome_counts.sum ~= n.recovered_leases_total then
        return nil, "COUNTER_CORRUPT"
    end
    local function reason_sum(fields)
        local sum = 0
        for _, field in ipairs(fields) do sum = sum+reasons.disposition_reason_counts.n[field] end
        return sum
    end
    if reason_sum(Run.completion_reasons) ~= n.completed_total or reason_sum(Run.dead_reasons) ~= n.dead_total or
       reason_sum(Run.cancel_reasons) ~= n.cancelled_total or reasons.disposition_reason_counts.n.published ~= n.output_commits_total or
       reasons.recovery_outcome_counts.n.delayed ~= reasons.retry_reason_counts.n.lease_expired_after_io or
       reasons.recovery_outcome_counts.n.dead > n.dead_total or reasons.recovery_outcome_counts.n.cancelled > n.cancelled_total or
       reasons.disposition_reason_counts.n.pre_io_recovery_exhausted > reasons.recovery_outcome_counts.n.dead or
       reasons.disposition_reason_counts.n.pre_io_recovery_exhausted*3 > n.recovered_leases_total or
       reasons.disposition_reason_counts.n.retry_exhausted*3 > n.request_starts then
        return nil, "COUNTER_CORRUPT"
    end
    local remaining = n.job_count-n.purged_job_count
    local primary = ix.ready.count+ix.leased.count+ix.delayed.count+ix.completed.count+ix.dead.count+ix.cancelled.count
    if ix.jobs.count ~= remaining or ix.job_order.count ~= remaining or primary ~= remaining or
       ix.ready_at.count ~= ix.ready.count or ix.leased_at.count ~= ix.leased.count or
       ix.commit_backpressure.count > ix.leased.count or ix.visited_depth.count ~= ix.visited_urls.count or
       ix.visited_depth.count > n.output_commits_total*6 then return nil, "STATE_INDEX_CORRUPT" end
    if v.purge_state == "none" then
        if ix.ready.count+ix.leased.count+ix.delayed.count ~= n.open_job_count or ix.completed.count ~= n.completed_total or
           ix.dead.count ~= n.dead_total or ix.cancelled.count ~= n.cancelled_total then return nil, "STATE_INDEX_CORRUPT" end
    end
    if n.pending_request_reservations+n.started_request_reservations > ix.leased.count or ix.leased.count > n.claims_total then
        return nil, "COUNTER_CORRUPT"
    end
    if n.activated_at_ms == 0 and (ix.leased.count > 0 or ix.delayed.count > 0 or ix.commit_backpressure.count > 0) or
       n.finalized_at_ms > 0 and (ix.leased.count > 0 or ix.commit_backpressure.count > 0) then return nil, "STATE_INDEX_CORRUPT" end
    local inv = run.inventory
    for _, name in ipairs({"runs","active_runs","unarchived_runs"}) do
        local fact = inv[name]
        if type(fact) ~= "table" or not fact.complete or type(fact.members) ~= "table" then
            return nil, "STATE_INDEX_CORRUPT"
        end
    end
    for _, def in ipairs({{"runs",128},{"active_runs",16},{"unarchived_runs",100}}) do
        local fact, count = inv[def[1]], 0
        if type(fact) ~= "table" or not fact.complete or type(fact.members) ~= "table" or
           not I.integer(fact.count,def[2]) then return nil, "STATE_INDEX_CORRUPT" end
        for id, present in next, fact.members, nil do
            if not I.hex(id,32) or type(present) ~= "boolean" then return nil, "STATE_INDEX_CORRUPT" end
            if present then
                count = count+1
                if def[1] == "runs" then
                    local at = fact.scores and score_number(fact.scores[id])
                    if not at or at == 0 then return nil, "STATE_INDEX_CORRUPT" end
                elseif def[1] == "active_runs" then
                    if not inv.unarchived_runs.members[id] then return nil, "STATE_INDEX_CORRUPT" end
                elseif not inv.runs.members[id] then return nil, "STATE_INDEX_CORRUPT" end
            end
        end
        if count ~= fact.count then return nil, "STATE_INDEX_CORRUPT" end
    end
    if not inv.runs.members[run.run_id] or score_number(inv.runs.scores[run.run_id]) ~= n.created_at_ms or
       (inv.active_runs.members[run.run_id] == true) ~= (n.finalized_at_ms == 0) or
       (inv.unarchived_runs.members[run.run_id] == true) ~= (v.state ~= "archived") then return nil, "STATE_INDEX_CORRUPT" end
    if v.terminal_reason == "authorization_expired" and n.cancelled_at_ms < n.authorization_expires_at_ms then
        return nil, "COUNTER_CORRUPT"
    end
    if v.state == "archived" then
        local eligible = P.safe_add(n.retention_anchor_ms,2592000000)
        if not eligible or n.archived_at_ms < eligible then return nil, "COUNTER_CORRUPT" end
        if v.purge_state == "in_progress" then
            local purge_eligible = P.safe_add(n.archived_at_ms,604800000)
            if not purge_eligible or n.purge_started_at_ms < purge_eligible then return nil, "COUNTER_CORRUPT" end
        end
    end
    run.n, run.record, run.groups, run.all_open_groups_exhausted = n, record, groups, all_exhausted
    return true
end
function Run.load(ctx, run_id)
    if not C.preparing(ctx) or not I.hex(run_id,32) then return nil, "INVALID_IDENTIFIER" end
    local requested = ctx.request.v.run_id
    if ctx.operation == "CJ2_RETIRE_LEGACY_KEYS" or ctx.operation == "CJ2_PROMOTE_CANDIDATE_CONTRACTS" then
        -- Admin has no run_id semantic scalar. Only the core's private wire or
        -- complete-inventory binding qualifies; public bound_run_id is not proof.
        requested = C.bound_run(ctx)
    end
    if requested ~= run_id or ctx.keys.run ~= "mifolyo:crawl:v2:run:"..run_id then return nil, "INVALID_IDENTIFIER" end
    local record, code = R.fixed_hash(ctx,ctx.keys.run,"run")
    if not record then return nil, code end
    if not record.exists then return nil, "INVALID_STATE" end
    if record.v.contract_sha256 ~= ctx.request.gate.contract then return nil, "CONTRACT_MISMATCH" end
    if ctx.request.gate.mode == "candidate" and (record.v.source_kind ~= "v1_migration" or record.n.activated_at_ms ~= 0 or
       record.n.finalized_at_ms ~= 0 or record.n.archived_at_ms ~= 0) then
        return nil, "INVALID_STATE"
    end
    if ctx.request.gate.mode == "active" and record.v.source_kind == "v1_migration" then
        local guard, legacy = ctx.request.gate.records.commit_guard,ctx.request.gate.records.legacy_retirement
        if record.n.sealed_at_ms == 0 or guard.v.cutover_mode ~= "v1_migration" or guard.v.candidate_run_id ~= run_id or
           legacy.n.v1_count ~= record.n.expected_seed_count or legacy.v.v1_source_sha256 ~= record.v.source_sha256 then
            return nil, "IMMUTABLE_MISMATCH"
        end
    end
    local run = {run_id=run_id,key=ctx.keys.run,v=record.v,n=record.n,record=record,maps={},reasons={},indexes={}}
    local ids = {}
    for _, name in ipairs(Run.group_maps) do
        local fact, err = R.dynamic_hash(ctx,Run.key(ctx,name),64,128,64)
        if not fact then return nil, err end
        if not fact.exists or fact.count ~= record.n.policy_group_count then return nil, "COUNTER_CORRUPT" end
        run.maps[name] = fact
        if name == "group_limits" then
            for _, id in ipairs(fact.names) do
                if not I.group(id) then return nil, "COUNTER_CORRUPT" end
                ids[#ids+1] = id
            end
            table.sort(ids)
        end
    end
    run.group_ids = ids
    if run.n.audit_revision == 0 then
        local absent, err = R.absent(ctx,Run.key(ctx,"audit_group_counts"),"hash")
        if not absent then return nil, err end
        run.maps.audit_group_counts = absent
    else
        local fact, err = R.dynamic_hash(ctx,Run.key(ctx,"audit_group_counts"),64,128,16)
        if not fact then return nil, err end
        local parsed, failure = sum_fields(fact,ids,10000)
        if not parsed then return nil, failure end
        fact.n, fact.sum = parsed.n, parsed.sum
        run.maps.audit_group_counts = fact
    end
    for name, fields in next, reason_sets, nil do
        local fact, err = R.dynamic_hash(ctx,Run.key(ctx,name),#fields,64,16)
        if not fact then return nil, err end
        local parsed, failure = sum_fields(fact,fields,10000)
        if not parsed then return nil, failure end
        fact.n, fact.sum = parsed.n, parsed.sum
        run.reasons[name] = fact
    end
    for _, def in ipairs(index_defs) do
        local fact, err = R.cardinality(ctx,Run.key(ctx,def[1]),def[2],def[3])
        if not fact then return nil, err end
        run.indexes[def[1]] = fact
    end
    local inv, failure = Run.inventory(ctx,run_id)
    if not inv then return nil, failure end
    run.inventory = inv
    local valid, err = Run.validate_ledger(run)
    if not valid then return nil, err end
    return run
end
local function copy(value)
    if type(value) ~= "table" then return value end
    local result = {}
    for key, child in next, value, nil do result[key] = copy(child) end
    return result
end
local function sorted_keys(map)
    local result = {}
    for key in next, map, nil do result[#result+1] = key end
    table.sort(result)
    return result
end
function Run.hset(plan, key, values, coverage)
    local argv = {"HSET",key}
    for _, field in ipairs(sorted_keys(values)) do
        argv[#argv+1], argv[#argv+2] = field, values[field]
    end
    if #argv == 2 then return true end
    return CJ.Plan.add(plan,argv,coverage or "ordinary")
end
local mutable_fields = {}
for _, field in ipairs(words([[state job_count open_job_count request_starts reservation_creations_total
pending_request_reservations started_request_reservations claims_total retries_total recovered_leases_total
renewal_rejections_total completed_total dead_total cancelled_total output_commits_total load_revision
audit_revision audit_count audit_cursor audit_complete sealed_at_ms activated_at_ms budget_exhausted_at_ms
cancelled_at_ms completed_at_ms finalized_at_ms last_activity_at_ms last_execution_at_ms
last_request_started_at_ms last_terminal_transition_at_ms retention_anchor_ms archived_at_ms archive_sha256
purge_state purge_evidence_sha256 purge_started_at_ms purged_job_count terminal_reason]])) do mutable_fields[field] = true end
local counter_fields = {}
for _, field in ipairs(words([[job_count open_job_count request_starts reservation_creations_total pending_request_reservations
started_request_reservations claims_total retries_total recovered_leases_total renewal_rejections_total completed_total
dead_total cancelled_total output_commits_total load_revision audit_count purged_job_count]])) do counter_fields[field] = true end
local delta_maps = {group_started=true,group_pending=true,group_active_started=true,group_open_jobs=true,audit_group_counts=true,
    retry_reason_counts=true,recovery_outcome_counts=true,disposition_reason_counts=true}
local ledgers = {}
-- Reusable batch API: one accumulator per run per operation. accumulate accepts
-- signed run/map/index deltas; set accepts non-counter lifecycle fields. flush
-- validates the complete projected ledger ONCE. Ordinary callers get one HSET
-- per changed hash; recovery gets disjoint field partitions, never independently
-- computed per-job replacements from the same original shared counter.
-- Job-owning planners remain responsible for their own exact index descriptors;
-- index deltas here validate their aggregate effect, not job membership proof.
function Run.plan_delta(ctx, run)
    if not C.preparing(ctx) or type(run) ~= "table" or run.run_id ~= ctx.request.v.run_id or run.key ~= ctx.keys.run then
        return nil, "INVALID_STATE"
    end
    local valid, code = Run.validate_ledger(run)
    if not valid then return nil, code end
    local handle = {}
    ledgers[handle] = {ctx=ctx,before=copy(run),run={},maps={},indexes={},sets={},inventory={},audit_init=false,flushed=false,
        recovery=ctx.operation == "CJ2_RECOVER_EXPIRED",contributors={},owners={},owner_seen={}}
    return handle
end
local function delta_value(old, amount)
    if type(amount) ~= "number" or amount ~= math.floor(amount) or math.abs(amount) > P.limits.max_integer then return nil end
    if amount >= 0 then return P.safe_add(old,amount) end
    if old < -amount then return nil end
    return old+amount
end
local function aggregate(map, field, amount)
    if type(amount) ~= "number" or amount ~= math.floor(amount) or math.abs(amount) > P.limits.max_integer then return false end
    local prior = map[field] or 0
    if amount > 0 and prior > P.limits.max_integer-amount or amount < 0 and prior < -P.limits.max_integer-amount then return false end
    map[field] = prior+amount
    return true
end
local function contribution_coverage(ledger, coverage)
    if ledger.recovery then
        -- Shape is not authentication. Core Plan.validate_unit authenticates all
        -- recorded handles at flush; never read public fields as job/slot/G proof.
        if type(coverage) ~= "table" or getmetatable(coverage) ~= nil then return nil, "INVALID_ARGUMENT" end
    elseif coverage ~= nil and coverage ~= "ordinary" then return nil, "INVALID_ARGUMENT" end
    return true
end
local function remember_owner(ledger, coverage)
    if not ledger.owner_seen[coverage] then
        ledger.owners[#ledger.owners+1] = coverage
        ledger.owner_seen[coverage] = true
    end
end
local function contribute(ledger, name, field, coverage)
    local fields = ledger.contributors[name]
    if not fields then fields = {}; ledger.contributors[name] = fields end
    local owners = fields[field]
    if not owners then owners = {}; fields[field] = owners end
    for _, owner in ipairs(owners) do if owner == coverage then return end end
    owners[#owners+1] = coverage
end
function Run.accumulate(handle, changes, coverage)
    local ledger = ledgers[handle]
    if not ledger or ledger.flushed or not C.preparing(ledger.ctx) then return nil, "INVALID_STATE" end
    local covered, coverage_error = contribution_coverage(ledger,coverage)
    if not covered then return nil, coverage_error end
    if type(changes) ~= "table" or getmetatable(changes) ~= nil then return nil, "INVALID_ARGUMENT" end
    for name, value in next, changes, nil do
        if (name ~= "run" and name ~= "maps" and name ~= "indexes") or type(value) ~= "table" or
           getmetatable(value) ~= nil then return nil, "INVALID_ARGUMENT" end
    end
    -- A rejected aggregation cannot leave half a batch in the handle.
    local next_run, next_maps, next_indexes = copy(ledger.run),copy(ledger.maps),copy(ledger.indexes)
    for field, amount in next, changes.run or {}, nil do
        if not counter_fields[field] or not aggregate(next_run,field,amount) then return nil, "COUNTER_CORRUPT" end
    end
    for name, fields in next, changes.maps or {}, nil do
        if not delta_maps[name] or type(fields) ~= "table" then return nil, "INVALID_ARGUMENT" end
        next_maps[name] = next_maps[name] or {}
        for field, amount in next, fields, nil do
            local fact = ledger.before.maps[name] or ledger.before.reasons[name]
            if not fact or not fact.n or fact.n[field] == nil or not aggregate(next_maps[name],field,amount) then return nil, "COUNTER_CORRUPT" end
        end
    end
    for name, amount in next, changes.indexes or {}, nil do
        if not ledger.before.indexes[name] or not aggregate(next_indexes,name,amount) then return nil, "STATE_INDEX_CORRUPT" end
    end
    ledger.run, ledger.maps, ledger.indexes = next_run,next_maps,next_indexes
    if ledger.recovery then
        remember_owner(ledger,coverage)
        for field, amount in next, changes.run or {}, nil do
            if amount ~= 0 then contribute(ledger,"run",field,coverage) end
        end
        for name, fields in next, changes.maps or {}, nil do
            for field, amount in next, fields, nil do
                if amount ~= 0 then contribute(ledger,name,field,coverage) end
            end
        end
    end
    return true
end
function Run.set(handle, values, coverage)
    local ledger = ledgers[handle]
    if not ledger or ledger.flushed or not C.preparing(ledger.ctx) then return nil, "INVALID_STATE" end
    local covered, coverage_error = contribution_coverage(ledger,coverage)
    if not covered then return nil, coverage_error end
    if type(values) ~= "table" or getmetatable(values) ~= nil then return nil, "INVALID_ARGUMENT" end
    for field, value in next, values, nil do
        if not mutable_fields[field] or counter_fields[field] or type(value) ~= "string" or
           (ledger.sets[field] and ledger.sets[field] ~= value) then return nil, "INVALID_ARGUMENT" end
    end
    for field, value in next, values, nil do ledger.sets[field] = value end
    if ledger.recovery then
        remember_owner(ledger,coverage)
        for field, value in next, values, nil do
            if ledger.before.v[field] ~= value then contribute(ledger,"run",field,coverage) end
        end
    end
    return true
end
function Run.begin_audit(handle)
    local ledger = ledgers[handle]
    if not ledger or ledger.flushed or not C.preparing(ledger.ctx) or ledger.before.v.state ~= "loading" or
       ledger.before.n.audit_revision ~= 0 then return nil, "INVALID_STATE" end
    ledger.audit_init = true
    return true
end
function Run.member(handle, inventory, present)
    local ledger = ledgers[handle]
    if not ledger or ledger.flushed or not C.preparing(ledger.ctx) then return nil, "INVALID_STATE" end
    if not one(inventory,"active_runs","unarchived_runs") or type(present) ~= "boolean" then
        return nil, "INVALID_ARGUMENT"
    end
    ledger.inventory[inventory] = present
    return true
end
function Run.flush(ctx, plan, handle, coverage)
    local ledger = ledgers[handle]
    if not ledger or ledger.ctx ~= ctx or ledger.flushed or not C.preparing(ctx) then return nil, "INVALID_STATE" end
    -- Recovery may not relabel an ordinary/missing contribution, use a blanket
    -- owner, or sneak inventory/audit creation into an owner's closed footprint.
    if ledger.recovery and (coverage ~= nil or ledger.audit_init or next(ledger.inventory) ~= nil) then
        return nil, "INVALID_ARGUMENT"
    end
    if ledger.recovery then
        if type(CJ.Plan.validate_unit) ~= "function" then return nil, "INVALID_STATE" end
        for _, owner in ipairs(ledger.owners) do
            local valid, code = CJ.Plan.validate_unit(plan,owner)
            if not valid then return nil, code end
        end
    end
    local post, writes = copy(ledger.before), {}
    writes.run = {}
    for field, value in next, ledger.sets, nil do post.v[field] = value; writes.run[field] = value end
    for field, amount in next, ledger.run, nil do
        local value = delta_value(post.n[field],amount)
        if not value then return nil, "COUNTER_CORRUPT" end
        post.v[field], writes.run[field] = P.format_decimal(value),P.format_decimal(value)
    end
    if ledger.audit_init then
        post.maps.audit_group_counts = {exists=true,kind="hash",complete=true,count=#post.group_ids,v={},n={},sum=0}
        writes.audit_group_counts = {}
        for _, id in ipairs(post.group_ids) do
            post.maps.audit_group_counts.v[id],post.maps.audit_group_counts.n[id],writes.audit_group_counts[id] = "0",0,"0"
        end
    end
    for name, fields in next, ledger.maps, nil do
        local fact = post.maps[name] or post.reasons[name]
        writes[name] = writes[name] or {}
        for field, amount in next, fields, nil do
            local value = delta_value(fact.n[field],amount)
            if not value then return nil, "COUNTER_CORRUPT" end
            fact.n[field],fact.v[field],writes[name][field] = value,P.format_decimal(value),P.format_decimal(value)
        end
    end
    for name, fields in next, reason_sets, nil do
        local parsed = sum_fields(post.reasons[name],fields,10000)
        if not parsed then return nil, "COUNTER_CORRUPT" end
        post.reasons[name].sum = parsed.sum
    end
    for name, amount in next, ledger.indexes, nil do
        local value = delta_value(post.indexes[name].count,amount)
        if not value or value > 10000 then return nil, "STATE_INDEX_CORRUPT" end
        post.indexes[name].count = value
    end
    for name, present in next, ledger.inventory, nil do
        local fact = post.inventory[name]
        local prior = fact.members[post.run_id] == true
        fact.members[post.run_id] = present or nil
        if present ~= prior then fact.count = fact.count+(present and 1 or -1) end
    end
    local valid, code = Run.validate_ledger(post)
    if not valid then return nil, code end
    -- Prebuild all partitions before descriptor insertion. Each final field is
    -- emitted once, regardless of how many jobs contributed its aggregate delta.
    local partitions = {}
    for _, name in ipairs(sorted_keys(writes)) do
        local values = writes[name]
        local old = name == "run" and ledger.before.v or ((ledger.before.maps[name] or ledger.before.reasons[name]).v or {})
        for field, value in next, values, nil do if coverage ~= "cancel_run" and old[field] == value then values[field] = nil end end
        if ledger.recovery then
            local by_owner = {}
            for _, field in ipairs(sorted_keys(values)) do
                local owners = ledger.contributors[name] and ledger.contributors[name][field]
                local owner = owners and owners[1]
                if not owner then return nil, "INVALID_STATE" end
                if not by_owner[owner] then by_owner[owner] = {} end
                by_owner[owner][field] = values[field]
            end
            for _, owner in ipairs(ledger.owners) do
                if by_owner[owner] then partitions[#partitions+1] = {name=name,values=by_owner[owner],coverage=owner} end
            end
        else partitions[#partitions+1] = {name=name,values=values,coverage=coverage} end
    end
    -- Plan rechecks each selected unit against this exact plan. Growth and
    -- slot/safety admission remain exclusively core-owned; no caller budget.
    for _, partition in ipairs(partitions) do
        local added, err = Run.hset(plan,Run.key(ctx,partition.name),partition.values,partition.coverage)
        if not added then return nil, err end
    end
    for _, name in ipairs(sorted_keys(ledger.inventory)) do
        local present = ledger.inventory[name]
        if (ledger.before.inventory[name].members[post.run_id] == true) ~= present then
            local added, err = CJ.Plan.add(plan,{present and "SADD" or "SREM",ctx.keys[name],post.run_id},coverage or "ordinary")
            if not added then return nil, err end
        end
    end
    ledger.flushed = true
    return post
end
function Run.finish(ctx, plan, status, tail)
    local reply, code = CJ.Reply.build(ctx,status,tail)
    if not reply then return nil, code end
    local assessment, err = CJ.Plan.assess(ctx,plan)
    if not assessment then return nil, err end
    return CJ.Plan.seal(ctx,plan,assessment,reply)
end
function Run.mutable(ctx, run)
    if not C.preparing(ctx) or run.v.purge_state ~= "none" or run.n.last_activity_at_ms > ctx.now_ms or
       run.n.archived_at_ms > ctx.now_ms then return nil, "INVALID_STATE" end
    return true
end
-- Maintenance-only extra keys: absence is an explicit reader fact. Enumerate
-- at most 64 lease composites and four slots, never an unbounded job inventory.
function Run.live(ctx, run)
    local global, code = R.all_members(ctx,ctx.keys.active_leases,"zset",64,97)
    if not global then return nil, code end
    local leased, err = R.all_members(ctx,Run.key(ctx,"leased"),"zset",64,64)
    if not leased then return nil, err end
    local ages, failure = R.all_members(ctx,Run.key(ctx,"leased_at"),"zset",64,64)
    if not ages then return nil, failure end
    local backpressure, bp_error = R.all_members(ctx,Run.key(ctx,"commit_backpressure"),"zset",10,64)
    if not backpressure then return nil, bp_error end
    local owned = 0
    for _, composite in ipairs(global.ordered) do
        local owner, job = string.match(composite,"^([^:]+):([^:]+)$")
        local deadline = score_number(global.scores[composite])
        if not I.hex(owner,32) or not I.hex(job,64) or not deadline or deadline == 0 or
           not run.inventory.active_runs.members[owner] then return nil, "STATE_INDEX_CORRUPT" end
        if owner == run.run_id then
            if not leased.members[job] or score_number(leased.scores[job]) ~= deadline then return nil, "STATE_INDEX_CORRUPT" end
            owned = owned+1
        end
    end
    if owned ~= leased.count then return nil, "STATE_INDEX_CORRUPT" end
    for _, job in ipairs(leased.ordered) do
        local deadline, started = score_number(leased.scores[job]),score_number(ages.scores[job])
        if not I.hex(job,64) or not ages.members[job] or not deadline or not started or
           started < run.n.created_at_ms or started >= deadline or started > run.n.last_activity_at_ms or
           started > ctx.now_ms then return nil, "STATE_INDEX_CORRUPT" end
    end
    for _, job in ipairs(backpressure.ordered) do
        local started = score_number(backpressure.scores[job])
        -- First COMMIT backpressure writes its dedicated clock/index without
        -- advancing general Run activity. Bound it by the owning lease age and
        -- this invocation's TIME, not last_activity_at_ms.
        if not leased.members[job] or not started or started < score_number(ages.scores[job]) or
           started > ctx.now_ms then
            return nil, "STATE_INDEX_CORRUPT"
        end
    end
    local slots, slot_error = R.slots(ctx)
    if not slots then return nil, slot_error end
    local owned_slots, commits, owners = 0, {}, {}
    for commit, slot in next, slots.records, nil do
        local owner = slot.run_id..":"..slot.job_id
        if not run.inventory.active_runs.members[slot.run_id] or not global.members[owner] or owners[owner] then
            return nil, "STATE_INDEX_CORRUPT"
        end
        owners[owner] = true
        commits[#commits+1] = commit
        if slot.run_id == run.run_id then
            if not leased.members[slot.job_id] then return nil, "STATE_INDEX_CORRUPT" end
            owned_slots = owned_slots+1
        end
    end
    -- At most one stage per started fence: <=10 starts * <=128 retained runs.
    -- Only the <=4 live slot IDs are selected; historical residue is not scanned.
    local expiry, expiry_error = R.members(ctx,ctx.keys.stage_expiry,"zset",commits,1280,64)
    if not expiry then return nil, expiry_error end
    for commit, slot in next, slots.records, nil do
        if slot.abort_unlinked_keys > 0 then
            if expiry.members[commit] then return nil, "STATE_INDEX_CORRUPT" end
        else
            local deadline = score_number(expiry.scores[commit])
            local lease_deadline = score_number(global.scores[slot.run_id..":"..slot.job_id])
            if not expiry.members[commit] or not deadline or not lease_deadline or deadline < lease_deadline then
                return nil, "STATE_INDEX_CORRUPT"
            end
        end
    end
    return {leases=global,slots=slots,stage_expiry=expiry,owned_leases=owned,owned_slots=owned_slots,
        drained=owned == 0 and owned_slots == 0 and run.n.pending_request_reservations == 0 and run.n.started_request_reservations == 0}
end
return Run
