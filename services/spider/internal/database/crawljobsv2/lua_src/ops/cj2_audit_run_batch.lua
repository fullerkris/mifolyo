-- Source-owned bounded audit; no scan, caller-count authority, or replay cache.
local Run, Job, R, I = CJ.Run, CJ.Job, CJ.Read, CJ.Identities
CJ.Reply.register("CJ2_AUDIT_RUN_BATCH",function(ctx,status,tail)
    if #tail ~= 3 then return nil, "INVALID_ARGUMENT" end
    local checked, count = P.parse_decimal(tail[1]),P.parse_decimal(tail[2])
    if checked ~= ctx.request.n.record_count or count == nil or count > 10000 or count < checked or
       (status ~= "BATCH_MORE" and status ~= "BATCH_DONE") or
       (status == "BATCH_MORE" and (checked == 0 or not I.hex(tail[3],64))) or
       (status == "BATCH_DONE" and tail[3] ~= "") then return nil, "INVALID_ARGUMENT" end
    return true
end)

local function prepare()
    local ctx, code = CJ.Context.open(CJ.Wire.run_spec("CJ2_AUDIT_RUN_BATCH",
        {"run_id","expected_prior_cursor","expected_prior_count","record_count"}),KEYS,ARGV)
    if not ctx then return nil, code end
    local prior, cursor, count = P.parse_decimal(ctx.request.v.expected_prior_count),
        ctx.request.v.expected_prior_cursor,ctx.request.n.record_count
    if prior == nil or prior > 10000 then return nil, "INVALID_NUMBER" end
    if (cursor ~= "" and not I.hex(cursor,64)) or (prior == 0) ~= (cursor == "") then
        return nil, "INVALID_ARGUMENT"
    end
    local gate, err = CJ.Gate.check(ctx)
    if not gate then return nil, err end
    local run, failure = Run.load(ctx,ctx.request.v.run_id)
    if not run then return nil, failure end
    local mutable, mutation_error = Run.mutable(ctx,run)
    if not mutable then return nil, mutation_error end
    -- Run.load validates frozen revision, all zero execution counters, complete
    -- bounded group/reason maps, and aggregate index/counter contributions.
    if run.v.state ~= "auditing" or run.n.audit_revision ~= run.n.load_revision then return nil, "INVALID_STATE" end
    if count == 0 and (run.n.job_count ~= 0 or run.n.expected_seed_count ~= 0 or prior ~= 0 or cursor ~= "") then
        return nil, "INVALID_ARGUMENT"
    end
    local sources = {}
    if count > 0 then
        local source_error
        sources, source_error = Job.sources(ctx.request.records,run)
        if not sources then return nil, source_error end
        if sources[1].v.job_id <= cursor then return nil, "IMMUTABLE_MISMATCH" end
    end
    local finish_count = prior+count -- bounded <=10100, before any counter write
    local finish_cursor = count == 0 and "" or sources[count].v.job_id
    local replay = finish_count == run.n.audit_count and finish_cursor == run.v.audit_cursor
    local advancing = prior == run.n.audit_count and cursor == run.v.audit_cursor and run.v.audit_complete == "0"
    if not replay and not advancing then return nil, "IMMUTABLE_MISMATCH" end
    if finish_count > run.n.job_count then return nil, "IMMUTABLE_MISMATCH" end

    -- Authenticate the caller's count using bounded rank reads, not arithmetic
    -- against their cursor alone. Compare lex AND rank pages (with one lookahead)
    -- so neither a fabricated prior count nor an omitted tail can certify DONE.
    if prior > 0 then
        local boundary, boundary_error = R.page(ctx,ctx.keys.run_job_order,"zset",prior-1,1,10000,64)
        if not boundary then return nil, boundary_error end
        if boundary.ordered[1] ~= cursor then return nil, "IMMUTABLE_MISMATCH" end
        if boundary.scores[cursor] ~= 0 then return nil, "STATE_INDEX_CORRUPT" end
    end
    local page, page_error = R.lex_page(ctx,ctx.keys.run_job_order,cursor,count+1,10000,64)
    if not page then return nil, page_error end
    local ranked, rank_error = R.page(ctx,ctx.keys.run_job_order,"zset",prior,count+1,10000,64)
    if not ranked then return nil, rank_error end
    if #page.ordered ~= #ranked.ordered or #page.ordered < count then return nil, "STATE_INDEX_CORRUPT" end
    for i, id in ipairs(page.ordered) do
        if not I.hex(id,64) or ranked.ordered[i] ~= id or ranked.scores[id] ~= 0 then return nil, "STATE_INDEX_CORRUPT" end
        if i <= count and sources[i].v.job_id ~= id then return nil, "IMMUTABLE_MISMATCH" end
    end
    local done = #page.ordered == count
    if done ~= (finish_count == run.n.job_count) then return nil, "STATE_INDEX_CORRUPT" end
    -- Empty initial and empty completed requests have the same cursor/count;
    -- only the persisted audit_complete bit distinguishes first execution.
    if replay and not advancing and (run.v.audit_complete == "1") ~= done then return nil, "IMMUTABLE_MISMATCH" end

    local ids, leases, contributions = {}, {}, {}
    for i, source in ipairs(sources) do
        ids[i],leases[i] = source.v.job_id,run.run_id..":"..source.v.job_id
        local group = source.v.group_id
        contributions[group] = (contributions[group] or 0)+1
    end
    local view = {ctx=ctx}
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
    for i, source in ipairs(sources) do
        local stored, read_error = R.fixed_hash(ctx,ctx.keys.job_keys[i],"job")
        if not stored then return nil, read_error end
        view.job = stored
        local job, check_error = Job.check(view,run,source.v.job_id)
        if not job then return nil, check_error end
        if not job.exists then return nil, "STATE_INDEX_CORRUPT" end
        if job.v.canonical_url ~= source.v.canonical_url then return nil, "URL_ID_COLLISION" end
        for _, field in ipairs(source.fields) do
            if job.v[field[1]] ~= field[2] then return nil, "IMMUTABLE_MISMATCH" end
        end
        local initial, initial_error = Job.initial_record(ctx,run,source)
        if not initial then return nil, initial_error end
        if job.n.created_at_ms < run.n.created_at_ms or job.n.created_at_ms > run.n.last_activity_at_ms or
           job.n.updated_at_ms ~= job.n.created_at_ms then return nil, "INVALID_STATE" end
        for _, field in ipairs(initial.fields) do
            if field[1] ~= "created_at_ms" and field[1] ~= "updated_at_ms" and job.v[field[1]] ~= field[2] then
                return nil, "INVALID_STATE"
            end
        end
    end
    local sum = 0
    for _, id in ipairs(run.group_ids) do
        local current, contribution = run.maps.audit_group_counts.n[id],contributions[id] or 0
        local target = advancing and current+contribution or current
        -- Replay must account for this batch in the already-recorded groups;
        -- no double addition and no negative hypothetical prior contribution.
        if not advancing and current < contribution then return nil, "COUNTER_CORRUPT" end
        if target > run.maps.group_open_jobs.n[id] or (done and target ~= run.maps.group_open_jobs.n[id]) then
            return nil, "COUNTER_CORRUPT"
        end
        sum = sum+target
    end
    if sum ~= finish_count or (done and (sum ~= run.n.job_count or sum ~= run.n.expected_seed_count)) then
        return nil, "COUNTER_CORRUPT"
    end
    local plan, plan_error = CJ.Plan.new(ctx)
    if not plan then return nil, plan_error end
    local status, response_cursor = done and "BATCH_DONE" or "BATCH_MORE",done and "" or finish_cursor
    if not advancing then
        return Run.finish(ctx,plan,status,{P.format_decimal(count),run.v.audit_count,response_cursor})
    end
    local delta, delta_error = Run.plan_delta(ctx,run)
    if not delta then return nil, delta_error end
    local ok, accumulate_error = Run.accumulate(delta,{run={audit_count=count},maps={audit_group_counts=contributions}})
    if not ok then return nil, accumulate_error end
    local set, set_error = Run.set(delta,{audit_cursor=finish_cursor,audit_complete=done and "1" or "0",last_activity_at_ms=ctx.now_text})
    if not set then return nil, set_error end
    local post, flush_error = Run.flush(ctx,plan,delta)
    if not post then return nil, flush_error end
    return Run.finish(ctx,plan,status,{P.format_decimal(count),post.v.audit_count,response_cursor})
end
local execution, code = prepare()
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
