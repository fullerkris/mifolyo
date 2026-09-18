local Run, I = CJ.Run, CJ.Identities
CJ.Reply.register("CJ2_ARCHIVE_RUN",function(ctx,status,tail)
    local at = P.parse_decimal(tail[1])
    if not at or at == 0 then return nil, "INVALID_ARGUMENT" end
    if status == "NOT_DUE" and #tail == 1 and at > ctx.now_ms then return true end
    if (status == "ARCHIVED" or status == "EXISTS_IDENTICAL") and #tail == 2 and at <= ctx.now_ms and I.digest(tail[2]) then return true end
    return nil, "INVALID_ARGUMENT"
end)
local function prepare()
    local ctx, code = CJ.Context.open(CJ.Wire.run_spec("CJ2_ARCHIVE_RUN",{"run_id","archive_sha256","confirmation_text"}),KEYS,ARGV)
    if not ctx then return nil, code end
    local v = ctx.request.v
    if not I.digest(v.archive_sha256) then return nil, "INVALID_ARGUMENT" end
    if v.confirmation_text ~= v.run_id..":"..v.archive_sha256 then return nil, "IMMUTABLE_MISMATCH" end
    local gate, err = CJ.Gate.check(ctx)
    if not gate then return nil, err end
    local run, failure = Run.load(ctx,v.run_id)
    if not run then return nil, failure end
    local mutable, mutation_error = Run.mutable(ctx,run)
    if not mutable then return nil, mutation_error end
    local live, live_error = Run.live(ctx,run)
    if not live then return nil, live_error end
    if not live.drained or run.n.finalized_at_ms == 0 then return nil, "INVALID_STATE" end
    local eligible = P.safe_add(run.n.retention_anchor_ms,2592000000)
    if not eligible then return nil, "INVALID_NUMBER" end
    local plan, plan_error = CJ.Plan.new(ctx)
    if not plan then return nil, plan_error end
    if run.v.state == "archived" then
        if v.archive_sha256 ~= run.v.archive_sha256 then return nil, "IMMUTABLE_MISMATCH" end
        if run.n.archived_at_ms < eligible then return nil, "INVALID_STATE" end
        return Run.finish(ctx,plan,"EXISTS_IDENTICAL",{run.v.archived_at_ms,run.v.archive_sha256})
    end
    if run.v.state ~= "completed" and run.v.state ~= "budget_exhausted" and run.v.state ~= "cancelled" then return nil, "INVALID_STATE" end
    if ctx.now_ms < eligible then return Run.finish(ctx,plan,"NOT_DUE",{P.format_decimal(eligible)}) end
    if live.slots.count ~= 0 or live.stage_expiry.count ~= 0 then return nil, "INVALID_STATE" end
    -- Independent bounded stopped-drain checks. Dead lists may retain OTHER
    -- runs' evidence; per-job absence/reconciliation/backlink scans are certified
    -- by the stopped export digest, not invented or performed by this script.
    for _, name in ipairs({"pages_queue","pages_processing","images_queue","images_processing"}) do
        local fact, read_error = CJ.Read.cardinality(ctx,ctx.keys[name],"list",0)
        if not fact then return nil, read_error end
        if fact.count ~= 0 then return nil, "INVALID_STATE" end
    end
    -- There is no protocol-wide bound or emptiness requirement for other runs'
    -- dead letters. Only TYPE is needed here, not a fabricated empty receipt or
    -- a scan/cardinality ceiling on the stopped export's external evidence.
    for _, name in ipairs({"pages_dead","images_dead"}) do
        local kind, type_error = CJ.Read.key_type(ctx,ctx.keys[name])
        if not kind then return nil, type_error end
        if kind ~= "none" and kind ~= "list" then return nil, "WRONG_TYPE" end
    end
    for _, name in ipairs({"pages_owner","images_owner"}) do
        local absent, absent_error = CJ.Read.absent(ctx,ctx.keys[name],"string")
        if not absent then return nil, absent_error end
    end
    local delta, delta_error = Run.plan_delta(ctx,run)
    if not delta then return nil, delta_error end
    -- Frozen finalization/activity/retention evidence is never reopened.
    local ok, set_error = Run.set(delta,{state="archived",archived_at_ms=ctx.now_text,archive_sha256=v.archive_sha256})
    if not ok then return nil, set_error end
    local removed, remove_error = Run.member(delta,"unarchived_runs",false)
    if not removed then return nil, remove_error end
    local post, flush_error = Run.flush(ctx,plan,delta)
    if not post then return nil, flush_error end
    return Run.finish(ctx,plan,"ARCHIVED",{post.v.archived_at_ms,post.v.archive_sha256})
end
local execution, code = prepare()
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
