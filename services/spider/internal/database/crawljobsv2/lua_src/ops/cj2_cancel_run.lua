local Run = CJ.Run
CJ.Reply.register("CJ2_CANCEL_RUN",function(ctx,status,tail)
    local at = P.parse_decimal(tail[1])
    if (status == "CANCELLED" or status == "EXISTS_IDENTICAL") and #tail == 2 and at ~= nil and at > 0 and at <= ctx.now_ms and
       (tail[2] == "operator_cancelled" or tail[2] == "source_cancelled" or tail[2] == "authorization_expired") then return true end
    return nil, "INVALID_ARGUMENT"
end)
local function prepare()
    local ctx, code = CJ.Context.open(CJ.Wire.run_spec("CJ2_CANCEL_RUN",{"run_id","reason"}),KEYS,ARGV)
    if not ctx then return nil, code end
    local reason = ctx.request.v.reason
    if reason ~= "operator_cancelled" and reason ~= "source_cancelled" and reason ~= "authorization_expired" then
        return nil, "INVALID_ARGUMENT"
    end
    local gate, err = CJ.Gate.check(ctx)
    if not gate then return nil, err end
    local run, failure = Run.load(ctx,ctx.request.v.run_id)
    if not run then return nil, failure end
    local mutable, mutation_error = Run.mutable(ctx,run)
    if not mutable then return nil, mutation_error end
    -- Only the maintenance constructor supplies this closed reason. Redis must
    -- independently reject a forged premature expiry, even on a replay.
    if reason == "authorization_expired" and ctx.now_ms < run.n.authorization_expires_at_ms then return nil, "INVALID_ARGUMENT" end
    local plan, plan_error = CJ.Plan.new(ctx)
    if not plan then return nil, plan_error end
    if run.v.state == "cancelled" then
        if reason ~= run.v.terminal_reason then return nil, "IMMUTABLE_MISMATCH" end
        return Run.finish(ctx,plan,"EXISTS_IDENTICAL",{run.v.cancelled_at_ms,run.v.terminal_reason})
    end
    if run.v.state ~= "loading" and run.v.state ~= "auditing" and run.v.state ~= "sealed" and run.v.state ~= "active" then
        return nil, "INVALID_STATE"
    end
    local delta, delta_error = Run.plan_delta(ctx,run)
    if not delta then return nil, delta_error end
    local ok, set_error = Run.set(delta,{state="cancelled",cancelled_at_ms=ctx.now_text,terminal_reason=reason,last_activity_at_ms=ctx.now_text})
    if not ok then return nil, set_error end
    -- Safety fallback covers only this bounded existing-run HSET. No new jobs,
    -- no reserve refund, no scan, and no second admission implementation here.
    local post, flush_error = Run.flush(ctx,plan,delta,"cancel_run")
    if not post then return nil, flush_error end
    return Run.finish(ctx,plan,"CANCELLED",{post.v.cancelled_at_ms,post.v.terminal_reason})
end
local execution, code = prepare()
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
