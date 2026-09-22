-- Source-owned run_records operation. Assemble P/core, URL, Run, Job first.
-- No key-plan extension: Read owns the read-only global-lease exception.
local Run, Job, R = CJ.Run, CJ.Job, CJ.Read
CJ.Reply.register("CJ2_ENQUEUE_BATCH",function(ctx,status,tail)
    if #tail ~= 4 then return nil, "INVALID_ARGUMENT" end
    local added, count, revision = P.parse_decimal(tail[1]),P.parse_decimal(tail[3]),P.parse_decimal(tail[4])
    if added == nil or added > ctx.request.n.record_count or tail[2] ~= "0" or
       count == nil or count > 10000 or count < added or revision == nil or revision == 0 or
       (status ~= "OK" and status ~= "EXISTS_IDENTICAL") or
       (status == "EXISTS_IDENTICAL") ~= (added == 0) then return nil, "INVALID_ARGUMENT" end
    return true
end)

local function prepare()
    local ctx, code = CJ.Context.open(CJ.Wire.run_spec("CJ2_ENQUEUE_BATCH",{"run_id","record_count"}),KEYS,ARGV)
    if not ctx then return nil, code end
    local gate, err = CJ.Gate.check(ctx)
    if not gate then return nil, err end
    local run, failure = Run.load(ctx,ctx.request.v.run_id)
    if not run then return nil, failure end
    local mutable, mutation_error = Run.mutable(ctx,run)
    if not mutable then return nil, mutation_error end
    if run.v.state ~= "loading" then return nil, "INVALID_STATE" end

    -- Bind the complete source batch once. The helper reparses RECORD fields;
    -- redundant caller .v/.n tables are not authority and input is never sorted.
    local sources, source_error = Job.sources(ctx.request.records,run)
    if not sources then return nil, source_error end
    local ids, leases = {}, {}
    for i, source in ipairs(sources) do
        ids[i],leases[i] = source.v.job_id,run.run_id..":"..source.v.job_id
    end
    local view = {ctx=ctx}
    -- All twelve selected memberships are explicit receipts, even when absent.
    -- A zero cardinality or an unrequested nil cannot stand in for false.
    for _, def in ipairs({{"jobs","set",10000},{"job_order","zset",10000},
        {"ready","zset",10000},{"ready_at","zset",10000},{"leased","zset",64},
        {"leased_at","zset",64},{"delayed","zset",10000},{"completed","zset",10000},
        {"dead","zset",10000},{"cancelled","zset",10000},{"commit_backpressure","zset",10},
        {"active_leases","zset",64}}) do
        local global = def[1] == "active_leases"
        local key = global and "mifolyo:crawl:v2:active_leases" or Run.key(ctx,def[1])
        local fact, index_error = R.members(ctx,key,def[2],global and leases or ids,def[3],global and 97 or 64)
        if not fact then return nil, index_error end
        view[def[1]] = fact
    end
    local fresh, groups = {}, {}
    for i, source in ipairs(sources) do
        local stored, read_error = R.fixed_hash(ctx,ctx.keys.job_keys[i],"job")
        if not stored then return nil, read_error end
        view.job = stored
        local job, check_error = Job.check(view,run,source.v.job_id)
        if not job then return nil, check_error end
        local initial, initial_error = Job.initial_record(ctx,run,source)
        if not initial then return nil, initial_error end
        if job.exists then
            if job.v.canonical_url ~= source.v.canonical_url then return nil, "URL_ID_COLLISION" end
            for _, field in ipairs(source.fields) do
                if job.v[field[1]] ~= field[2] then return nil, "IMMUTABLE_MISMATCH" end
            end
            -- Loading cannot have executed. Check the entire initial shape,
            -- including B/G, retained witnesses, and cleared optional fields.
            -- Its original creation clock is preserved, never refreshed on retry.
            if job.n.created_at_ms < run.n.created_at_ms or job.n.created_at_ms > run.n.last_activity_at_ms or
               job.n.updated_at_ms ~= job.n.created_at_ms then return nil, "INVALID_STATE" end
            for _, field in ipairs(initial.fields) do
                if field[1] ~= "created_at_ms" and field[1] ~= "updated_at_ms" and job.v[field[1]] ~= field[2] then
                    return nil, "INVALID_STATE"
                end
            end
        else
            fresh[#fresh+1] = {key=ctx.keys.job_keys[i],record=initial}
            local group = source.v.group_id
            groups[group] = (groups[group] or 0)+1
        end
    end
    local total = P.safe_add(run.n.job_count,#fresh)
    if not total or total > run.n.expected_seed_count or total > 10000 then return nil, "LIMIT_EXCEEDED" end
    local plan, plan_error = CJ.Plan.new(ctx)
    if not plan then return nil, plan_error end
    if #fresh == 0 then
        return Run.finish(ctx,plan,"EXISTS_IDENTICAL",{"0","0",run.v.job_count,run.v.load_revision})
    end

    -- Every source/hash/index has now passed. Descriptors are inert until the
    -- entire plan (including its LAST ACL check) is assessed and sealed.
    for _, item in ipairs(fresh) do
        local added, add_error = Run.hset(plan,item.key,item.record.v)
        if not added then return nil, add_error end
        local v = item.record.v
        for _, argv in ipairs({{"SADD",ctx.keys.run_jobs,v.job_id},
            {"ZADD",ctx.keys.run_job_order,"0",v.job_id},
            {"ZADD",ctx.keys.run_ready,v.score_text,v.job_id},
            {"ZADD",ctx.keys.run_ready_at,ctx.now_text,v.job_id}}) do
            local ok, write_error = CJ.Plan.add(plan,argv,"ordinary")
            if not ok then return nil, write_error end
        end
    end
    local delta, delta_error = Run.plan_delta(ctx,run)
    if not delta then return nil, delta_error end
    local ok, accumulate_error = Run.accumulate(delta,{run={job_count=#fresh,open_job_count=#fresh,load_revision=1},
        maps={group_open_jobs=groups},indexes={jobs=#fresh,job_order=#fresh,ready=#fresh,ready_at=#fresh}})
    if not ok then return nil, accumulate_error end
    local set, set_error = Run.set(delta,{last_activity_at_ms=ctx.now_text})
    if not set then return nil, set_error end
    local post, flush_error = Run.flush(ctx,plan,delta)
    if not post then return nil, flush_error end
    return Run.finish(ctx,plan,"OK",{P.format_decimal(#fresh),"0",post.v.job_count,post.v.load_revision})
end
local execution, code = prepare()
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
