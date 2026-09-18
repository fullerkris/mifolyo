-- Section 10.2 admin planners, assembled AFTER shared core/aliases and CJ.Run.
-- API: Admin.prepare(operation, KEYS, ARGV) -> sealed execution / closed code.
-- No executor, alternate Redis gateway, fixture mode, or general rate framework.
-- Source/legacy digests, process stops, image/config approval and the stopped
-- backlink scan are exact trusted-tool artifacts. Lua does NOT observe external
-- processes, scan backlinks, or rehash the bulk legacy/source data.
local A, C, R, S, I, Run = {}, CJ.Context, CJ.Read, CJ.Schemas, CJ.Identities, CJ.Run
local RETIRE, PROMOTE, MARK = "CJ2_RETIRE_LEGACY_KEYS", "CJ2_PROMOTE_CANDIDATE_CONTRACTS", "CJ2_MARK_PLANNED_SHUTDOWN"
local legacy = {
    {"legacy_queue","zset","v1_count",10000},
    {"legacy_urls","hash","v1_url_field_count",20000},
    {"legacy_depths","hash","v1_depth_field_count",20000},
    {"legacy_spider_queue","list","spider_queue_count",10000},
    {"legacy_signal_queue","list","signal_queue_count",10000}
}
local confirmation_fields = {"freeze_nonce","backup_sha256","v1_count","v1_source_sha256",
    "v1_queue_evidence_sha256","v1_urls_evidence_sha256","v1_depths_evidence_sha256",
    "spider_queue_evidence_sha256","signal_queue_evidence_sha256"}
local function bitmap(n)
    local bits = {}
    for i, def in ipairs(legacy) do
        if n[def[3]] == nil then return nil end
        bits[i] = n[def[3]] > 0 and "1" or "0"
    end
    return table.concat(bits)
end
local validators = {
    [RETIRE] = function(ctx,status,tail)
        if (status ~= "LEGACY_RETIRED" and status ~= "EXISTS_IDENTICAL") or #tail ~= 2 or
           tail[1] ~= bitmap(ctx.request.n) or tail[2] ~= ctx.request.v.v1_source_sha256 or
           not I.digest(tail[2]) then return nil, "INVALID_ARGUMENT" end
        return true
    end,
    [PROMOTE] = function(ctx,status,tail)
        if (status ~= "CONTRACTS_PROMOTED" and status ~= "EXISTS_IDENTICAL") or #tail ~= 3 or
           tail[1] ~= ctx.request.gate.records.compatibility_marker.v.manifest_sha256 or
           tail[2] ~= ctx.request.v.contract_sha256 or tail[3] ~= ctx.request.v.commit_guard_sha256 then
            return nil, "INVALID_ARGUMENT"
        end
        for i = 1, 3 do if not I.digest(tail[i]) then return nil, "INVALID_ARGUMENT" end end
        return true
    end,
    [MARK] = function(ctx,status,tail)
        if (status ~= "OK" and status ~= "EXISTS_IDENTICAL") or #tail ~= 1 or
           tail[1] ~= ctx.request.v.planned_shutdown_nonce or not I.hex(tail[1],32) then return nil, "INVALID_ARGUMENT" end
        return true
    end
}
for _, operation in ipairs({RETIRE,PROMOTE,MARK}) do
    local ok, code = CJ.Reply.register(operation,validators[operation])
    if not ok then return nil, code end
end
local function copy(values)
    local result = {}
    for k, v in next, values, nil do result[k] = v end
    return result
end
local function exact(a, b)
    if not a.exists then return false end
    for _, field in ipairs(b.fields) do if a.v[field[1]] ~= field[2] then return false end end
    return true
end
local function empty(ctx, key, kind, maximum)
    local fact, code = R.cardinality(ctx,key,kind,maximum)
    if not fact then return nil, code end
    if fact.count ~= 0 then return nil, "INVALID_STATE" end
    return fact
end
local function owners_absent(ctx)
    for _, name in ipairs({"pages_owner","images_owner"}) do
        local fact, code = R.absent(ctx,ctx.keys[name],"string")
        if not fact then return nil, code end
    end
    return true
end
local function legacy_absent(ctx)
    for _, def in ipairs(legacy) do
        local fact, code = R.absent(ctx,ctx.keys[def[1]],def[2])
        if not fact then return nil, code end
    end
    return true
end
local function candidate_run(ctx, count, source, expected_id)
    -- Complete receipts, not just cardinalities, are required for RETIRE's
    -- read-only binding. None of these reads grants a run/job write permission.
    local inventory, code = Run.inventory(ctx,expected_id)
    if not inventory then return nil, code end
    local wanted = count == 0 and 0 or 1
    for _, name in ipairs({"runs","active_runs","unarchived_runs"}) do
        if inventory[name].count ~= wanted then return nil, "STATE_INDEX_CORRUPT" end
    end
    if count == 0 then return true end
    local id = inventory.runs.ordered[1]
    if not inventory.active_runs.members[id] or not inventory.unarchived_runs.members[id] or
       (expected_id ~= nil and expected_id ~= id) then return nil, "STATE_INDEX_CORRUPT" end
    if ctx.operation == RETIRE then
        local bound, err = C.bind_run_read(ctx,id)
        if not bound then return nil, err end
    end
    local bound, err = C.bound_run(ctx)
    if not bound then return nil, err end
    if bound ~= id then return nil, "IMMUTABLE_MISMATCH" end
    local run, failure = Run.load(ctx,id)
    if not run then return nil, failure end
    local mutable, invalid = Run.mutable(ctx,run)
    if not mutable then return nil, invalid end
    if run.v.state ~= "sealed" or run.v.source_kind ~= "v1_migration" or
       run.n.expected_seed_count ~= count or run.n.job_count ~= count or run.v.source_sha256 ~= source then
        return nil, "IMMUTABLE_MISMATCH"
    end
    -- Run's complete sealed schema/ledger also proves the audit revision/count,
    -- zero execution/reservation/output counters and zero per-run leased indexes.
    return run
end
local function retire(ctx, view, plan)
    local v, values, parts = ctx.request.v, copy(ctx.request.v), {}
    for i, field in ipairs(confirmation_fields) do parts[i] = v[field] end
    if v.confirmation_text ~= table.concat(parts,":") then return nil, "INVALID_ARGUMENT" end
    if v.freeze_nonce ~= view.admin_freeze.v.freeze_nonce then return nil, "IMMUTABLE_MISMATCH" end
    local replay = view.legacy_retirement.exists
    values.protocol_version, values.deleted_bitmap = "2", bitmap(ctx.request.n)
    values.retired_at_ms = replay and view.legacy_retirement.v.retired_at_ms or ctx.now_text
    -- Full retirement schema owns all count limits, type/count relations,
    -- nonzero evidence and exact bitmap rules, including orphan metadata.
    local record, code = S.project("legacy_retirement",values)
    if not record then return nil, code end
    if replay and not exact(view.legacy_retirement,record) then return nil, "IMMUTABLE_MISMATCH" end
    local run, err = candidate_run(ctx,record.n.v1_count,v.v1_source_sha256)
    if not run then return nil, err end
    if replay then
        local absent, failure = legacy_absent(ctx)
        if not absent then return nil, failure end
    else
        local present = {}
        for i, def in ipairs(legacy) do
            local kind = def[2]
            if i == 4 and v.spider_queue_type == "zset" then kind = "zset" end
            local fact, failure = R.cardinality(ctx,ctx.keys[def[1]],kind,def[4])
            if not fact then return nil, failure end
            if fact.count ~= record.n[def[3]] or (i == 4 and fact.kind ~= v.spider_queue_type) or
               (i == 5 and fact.kind ~= v.signal_queue_type) then return nil, "IMMUTABLE_MISMATCH" end
            present[i] = fact.exists
        end
        local added, failure = Run.hset(plan,ctx.keys.legacy_retirement,record.v)
        if not added then return nil, failure end
        for i, def in ipairs(legacy) do
            if present[i] then
                local added, failure = CJ.Plan.add(plan,{"UNLINK",ctx.keys[def[1]]},"ordinary")
                if not added then return nil, failure end
            end
        end
    end
    return Run.finish(ctx,plan,replay and "EXISTS_IDENTICAL" or "LEGACY_RETIRED",{record.v.deleted_bitmap,v.v1_source_sha256})
end
local function promote(ctx, view, plan)
    local v = ctx.request.v
    -- Gate already proved every surviving post-state authority field/core hash
    -- and irreversibly locked any receipt plan to zero writes. Do NOT inspect
    -- now-active run, lease, stage, rate or downstream state on this path.
    local absent, code = legacy_absent(ctx)
    if not absent then return nil, code end
    local tail = {ctx.request.gate.records.compatibility_marker.v.manifest_sha256,v.contract_sha256,v.commit_guard_sha256}
    if view.receipt_only then
        if view.kind ~= "promoted" then return nil, "INVALID_STATE" end
        return Run.finish(ctx,plan,"EXISTS_IDENTICAL",tail)
    end
    local retirement = view.legacy_retirement
    local run, failure = candidate_run(ctx,retirement.n.v1_count,retirement.v.v1_source_sha256,
        v.cutover_mode == "v1_migration" and v.candidate_run_id or nil)
    if not run then return nil, failure end
    if v.cutover_mode == "v1_migration" then
        local minimum = P.safe_add(ctx.now_ms,60000)
        if not minimum or run.n.authorization_expires_at_ms < minimum then return nil, "INVALID_STATE" end
    end
    for _, def in ipairs({{"first_request_start","hash"},{"active_leases","zset"},{"stage_slots","hash"},
        {"stage_expiry","zset"},{"rate_scopes","zset"}}) do
        local fact, err = R.absent(ctx,ctx.keys[def[1]],def[2])
        if not fact then return nil, err end
    end
    for _, name in ipairs({"pages_queue","pages_processing","pages_dead","images_queue","images_processing","images_dead"}) do
        local fact, err = empty(ctx,ctx.keys[name],"list",100000)
        if not fact then return nil, err end
    end
    local owners, owner_error = owners_absent(ctx)
    if not owners then return nil, owner_error end
    local memory, memory_error = CJ.Memory.observe(ctx,true)
    if not memory then return nil, memory_error end
    if memory.lazyfree_pending_objects ~= 0 then return nil, "INVALID_STATE" end
    local values = copy(v)
    values.compatibility_manifest_sha256, values.approved_at_ms = tail[1], ctx.now_text
    local guard, guard_error = S.project("commit_guard",values)
    if not guard then return nil, guard_error end
    local added, add_error = Run.hset(plan,ctx.keys.commit_guard,guard.v)
    if not added then return nil, add_error end
    for _, argv in ipairs({{"RENAME",ctx.keys.candidate_compatibility,ctx.keys.active_compatibility},
        {"RENAME",ctx.keys.candidate_contract,ctx.keys.active_contract},{"UNLINK",ctx.keys.admin_freeze}}) do
        local ok, err = CJ.Plan.add(plan,argv,"ordinary")
        if not ok then return nil, err end
    end
    return Run.finish(ctx,plan,"CONTRACTS_PROMOTED",tail)
end
-- MARK has only RATE(global), not arbitrary group/origin keys. Validate that
-- exact fixed 14-field specialization via shared bounded selected-field reads.
-- Do not register an incomplete general rate_scope schema or scan its inventory.
local rate_fields = {"protocol_version","scope_id","scope_kind","scope_witness","effective_concurrency",
    "effective_interval_ms","next_allowed_ms","last_started_at_ms","active_count","pending_count","started_count",
    "concurrency_source_sha256","interval_source_sha256","updated_at_ms"}
local function global_rate_drained(ctx, materialized)
    local scope, code = I.global_scope()
    if not scope then return nil, code end
    local inventory, err = R.members(ctx,ctx.keys.rate_scopes,"zset",{scope},100000,64)
    if not inventory then return nil, err end
    local record, failure = R.hash_fields(ctx,ctx.keys.global_rate,rate_fields,14,64,64)
    if not record then return nil, failure end
    if record.exists ~= (inventory.members[scope] == true) then return nil, "RATE_STATE_CORRUPT" end
    -- Every reservation admits the global scope. It is durable thereafter;
    -- active-run reservation history or another inventoried scope cannot be
    -- reconciled with a missing global record by assuming counters are zero.
    if not record.exists and (materialized or inventory.count ~= 0) then return nil, "RATE_STATE_CORRUPT" end
    if record.exists then
        if not record.complete or record.count ~= 14 then return nil, "RATE_STATE_CORRUPT" end
        local v, n = record.v, {}
        for _, field in ipairs(rate_fields) do
            if type(v[field]) ~= "string" then return nil, "RATE_STATE_CORRUPT" end
        end
        for _, field in ipairs({"effective_concurrency","effective_interval_ms","next_allowed_ms","last_started_at_ms",
            "active_count","pending_count","started_count","updated_at_ms"}) do
            n[field] = P.parse_decimal(v[field])
            if n[field] == nil then return nil, "RATE_STATE_CORRUPT" end
        end
        if v.protocol_version ~= "2" or v.scope_id ~= scope or v.scope_kind ~= "global" or v.scope_witness ~= "global" or
           n.effective_concurrency ~= 2 or n.effective_interval_ms ~= 0 or n.next_allowed_ms ~= 0 or
           n.active_count ~= 0 or n.pending_count ~= 0 or n.started_count ~= 0 or n.updated_at_ms == 0 or
           n.last_started_at_ms > n.updated_at_ms or n.updated_at_ms > ctx.now_ms or
           not I.digest(v.concurrency_source_sha256) or not I.digest(v.interval_source_sha256) or
           inventory.scores[scope] ~= n.updated_at_ms then return nil, "RATE_STATE_CORRUPT" end
    end
    for _, name in ipairs({"global_rate_active","global_rate_pending","global_rate_started"}) do
        local fact, invalid = empty(ctx,ctx.keys[name],"zset",32)
        if not fact then return nil, invalid end
    end
    return true
end
local function mark(ctx, view, plan)
    local inventory, code = R.all_members(ctx,ctx.keys.active_runs,"set",16,32)
    if not inventory then return nil, code end
    if inventory.count ~= ctx.request.n.active_run_count then return nil, "STATE_INDEX_CORRUPT" end
    local materialized = false
    for i, id in ipairs(ctx.request.repeated) do
        if inventory.ordered[i] ~= id then return nil, "STATE_INDEX_CORRUPT" end
        local pair = ctx.keys.shutdown_runs[i]
        local record, err = R.fixed_hash(ctx,pair.run,"run")
        if not record then return nil, err end
        if not record.exists then return nil, "STATE_INDEX_CORRUPT" end
        if record.v.contract_sha256 ~= ctx.request.gate.contract then return nil, "CONTRACT_MISMATCH" end
        if record.n.finalized_at_ms ~= 0 or record.n.pending_request_reservations ~= 0 or
           record.n.started_request_reservations ~= 0 then return nil, "INVALID_STATE" end
        if record.n.reservation_creations_total > 0 then materialized = true end
        local mutable, invalid = Run.mutable(ctx,record)
        if not mutable then return nil, invalid end
        if record.v.source_kind == "v1_migration" then
            local guard, retirement = view.commit_guard, view.legacy_retirement
            if record.n.sealed_at_ms == 0 or guard.v.cutover_mode ~= "v1_migration" or guard.v.candidate_run_id ~= id or
               retirement.n.v1_count ~= record.n.expected_seed_count or retirement.v.v1_source_sha256 ~= record.v.source_sha256 then
                return nil, "IMMUTABLE_MISMATCH"
            end
        end
        local leased, lease_error = empty(ctx,pair.leased,"zset",64)
        if not leased then return nil, lease_error end
    end
    for _, def in ipairs({{"active_leases","zset",64},{"stage_slots","hash",4},{"stage_expiry","zset",1280}}) do
        local fact, err = empty(ctx,ctx.keys[def[1]],def[2],def[3])
        if not fact then return nil, err end
    end
    local drained, drain_error = global_rate_drained(ctx,materialized)
    if not drained then return nil, drain_error end
    local owners, owner_error = owners_absent(ctx)
    if not owners then return nil, owner_error end
    local v = ctx.request.v
    if view.receipt_only then
        if view.kind ~= "planned_shutdown" then return nil, "INVALID_STATE" end
    else
        if v.planned_shutdown_nonce == view.durability.v.consumed_planned_shutdown_nonce then return nil, "IMMUTABLE_MISMATCH" end
        local values = copy(view.durability.v)
        values.boot_state, values.planned_shutdown_nonce = "planned", v.planned_shutdown_nonce
        values.planned_shutdown_evidence_sha256, values.consumed_planned_shutdown_nonce = v.process_stop_evidence_sha256, ""
        local post, invalid = S.project("durability",values)
        if not post then return nil, invalid end
        local added, failure = Run.hset(plan,ctx.keys.durability,{
            boot_state=post.v.boot_state,planned_shutdown_nonce=post.v.planned_shutdown_nonce,
            planned_shutdown_evidence_sha256=post.v.planned_shutdown_evidence_sha256,consumed_planned_shutdown_nonce=""})
        if not added then return nil, failure end
    end
    return Run.finish(ctx,plan,view.receipt_only and "EXISTS_IDENTICAL" or "OK",{v.planned_shutdown_nonce})
end
local planners = {[RETIRE]=retire,[PROMOTE]=promote,[MARK]=mark}
function A.prepare(operation, keys, args)
    local spec, code = CJ.Wire.admin_spec(operation)
    if not spec then return nil, code end
    local ctx, err = C.open(spec,keys,args)
    if not ctx then return nil, err end
    local view, failure = CJ.Gate.check(ctx)
    if not view then return nil, failure end
    local plan, invalid = CJ.Plan.new(ctx)
    if not plan then return nil, invalid end
    return planners[operation](ctx,view,plan)
end
return A
