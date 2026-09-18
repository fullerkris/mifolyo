-- CREATE: exact immutable replay precedes fresh authorization/capacity admission.
local Run, I = CJ.Run, CJ.Identities
local fields = {"run_id","source_kind","source_sha256","expected_seed_count","authorization_sha256",
    "authorization_scope_sha256","authorization_expires_at_ms","canonicalization_version","canonicalization_sha256",
    "crawl_policy_version","crawl_policy_sha256","render_policy_version","render_policy_sha256","policy_group_map_sha256",
    "max_jobs","max_request_starts","global_concurrency_limit","max_delivery_attempts","policy_group_count"}
CJ.Reply.register("CJ2_CREATE_RUN",function(ctx,status,tail)
    if (status == "CREATED" or status == "EXISTS_IDENTICAL") and #tail == 1 and
       I.hex(tail[1],32) and tail[1] == ctx.request.v.run_id then return true end
    return nil, "INVALID_ARGUMENT"
end)
local function prepare()
    local spec = CJ.Wire.run_spec("CJ2_CREATE_RUN",fields)
    local ctx, code = CJ.Context.open(spec,KEYS,ARGV)
    if not ctx then return nil, code end
    local v, numbers = ctx.request.v, {}
    for _, field in ipairs({"expected_seed_count","authorization_expires_at_ms","canonicalization_version","crawl_policy_version",
        "render_policy_version","max_jobs","max_request_starts","global_concurrency_limit","max_delivery_attempts","policy_group_count"}) do
        numbers[field] = P.parse_decimal(v[field])
        if not numbers[field] then return nil, "INVALID_NUMBER" end
    end
    for _, field in ipairs({"source_sha256","authorization_sha256","authorization_scope_sha256","canonicalization_sha256",
        "crawl_policy_sha256","render_policy_sha256","policy_group_map_sha256"}) do
        if not I.digest(v[field]) then return nil, "INVALID_ARGUMENT" end
    end
    if numbers.expected_seed_count > 10000 or numbers.authorization_expires_at_ms == 0 or v.canonicalization_version ~= "1" or
       v.crawl_policy_version ~= "2" or numbers.render_policy_version == 0 or v.max_jobs ~= "10000" or
       numbers.max_request_starts < 1 or numbers.max_request_starts > 10 or v.global_concurrency_limit ~= "2" or
       v.max_delivery_attempts ~= "3" then return nil, "INVALID_ARGUMENT" end
    if (ctx.request.gate.mode == "candidate" and (v.source_kind ~= "v1_migration" or numbers.expected_seed_count == 0)) or
       (ctx.request.gate.mode == "active" and v.source_kind ~= "mongo") then return nil, "INVALID_ARGUMENT" end
    local encoded = {}
    for i, record in ipairs(ctx.request.records) do
        local group, err = CJ.Schemas.project("policy_group",record.v)
        if not group then return nil, err end
        encoded[i] = CJ.Schemas.encode(group)
    end
    local groups, group_error = CJ.Schemas.groups(encoded)
    if not groups then return nil, group_error end
    if groups.count ~= numbers.policy_group_count or groups.digest ~= v.policy_group_map_sha256 then return nil, "IMMUTABLE_MISMATCH" end
    local gate, gate_error = CJ.Gate.check(ctx)
    if not gate then return nil, gate_error end
    local existing, read_error = CJ.Read.fixed_hash(ctx,ctx.keys.run,"run")
    if not existing then return nil, read_error end
    local plan, plan_error = CJ.Plan.new(ctx)
    if not plan then return nil, plan_error end
    if existing.exists then
        local run, err = Run.load(ctx,v.run_id)
        if not run then return nil, err end
        local mutable, mutation_error = Run.mutable(ctx,run)
        if not mutable then return nil, mutation_error end
        for i=2,#fields do if run.v[fields[i]] ~= v[fields[i]] then return nil, "IMMUTABLE_MISMATCH" end end
        return Run.finish(ctx,plan,"EXISTS_IDENTICAL",{v.run_id})
    end
    local inventory, inv_error = Run.inventory(ctx,v.run_id)
    if not inventory then return nil, inv_error end
    for _, name in ipairs({"runs","active_runs","unarchived_runs"}) do
        if inventory[name].members[v.run_id] then return nil, "STATE_INDEX_CORRUPT" end
    end
    -- All 27 run keys must be absent. An interrupted executor is corruption,
    -- never permission to repair a partial CREATE or overwrite existing maps.
    for _, name in ipairs(Run.group_maps) do
        local absent, err = CJ.Read.absent(ctx,Run.key(ctx,name),"hash")
        if not absent then return nil, err end
    end
    for _, name in ipairs({"audit_group_counts","retry_reason_counts","recovery_outcome_counts","disposition_reason_counts"}) do
        local absent, err = CJ.Read.absent(ctx,Run.key(ctx,name),"hash")
        if not absent then return nil, err end
    end
    for _, def in ipairs(Run.index_defs) do
        local absent, err = CJ.Read.absent(ctx,Run.key(ctx,def[1]),def[2])
        if not absent then return nil, err end
    end
    if inventory.runs.count >= 128 or inventory.active_runs.count >= 16 or inventory.unarchived_runs.count >= 100 then
        return nil, "LIMIT_EXCEEDED"
    end
    if numbers.authorization_expires_at_ms <= ctx.now_ms or numbers.authorization_expires_at_ms-ctx.now_ms > 86400000 then
        return nil, "INVALID_ARGUMENT"
    end
    local values = {}
    for _, field in ipairs(Run.numeric) do values[field] = "0" end
    for i=2,#fields do values[fields[i]] = v[fields[i]] end
    values.protocol_version,values.contract_sha256,values.state = "2",ctx.request.gate.contract,"loading"
    values.load_revision,values.audit_cursor,values.audit_complete = "1","","0"
    values.created_at_ms,values.last_activity_at_ms = ctx.now_text,ctx.now_text
    values.archive_sha256,values.purge_state,values.purge_evidence_sha256,values.terminal_reason = "","none","","none"
    local record, record_error = Run.validate(values)
    if not record then return nil, record_error end
    local added, add_error = Run.hset(plan,ctx.keys.run,record.v)
    if not added then return nil, add_error end
    local group_fields = {group_limits="request_start_limit",group_rate_scope_ids="rate_scope_id",group_scope_ids="group_scope_id",
        group_concurrency="concurrency",group_interval_ms="interval_ms"}
    for _, name in ipairs(Run.group_maps) do
        local map = {}
        for _, group in ipairs(groups.ordered) do map[group.v.group_id] = group_fields[name] and group.v[group_fields[name]] or "0" end
        local ok, err = Run.hset(plan,Run.key(ctx,name),map)
        if not ok then return nil, err end
    end
    for _, pair in ipairs({{"retry_reason_counts",Run.retry_reasons},{"recovery_outcome_counts",Run.recovery_reasons},
        {"disposition_reason_counts",Run.disposition_reasons}}) do
        local map = {}
        for _, reason in ipairs(pair[2]) do map[reason] = "0" end
        local ok, err = Run.hset(plan,Run.key(ctx,pair[1]),map)
        if not ok then return nil, err end
    end
    for _, argv in ipairs({{"ZADD",ctx.keys.runs,ctx.now_text,v.run_id},{"SADD",ctx.keys.active_runs,v.run_id},
        {"SADD",ctx.keys.unarchived_runs,v.run_id}}) do
        local ok, err = CJ.Plan.add(plan,argv,"ordinary")
        if not ok then return nil, err end
    end
    return Run.finish(ctx,plan,"CREATED",{v.run_id})
end
local execution, code = prepare()
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
