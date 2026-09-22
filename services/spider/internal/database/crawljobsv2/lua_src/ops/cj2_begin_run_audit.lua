local Run = CJ.Run
CJ.Reply.register("CJ2_BEGIN_RUN_AUDIT",function(ctx,status,tail)
    local count = P.parse_decimal(tail[2])
    if (status == "AUDIT_STARTED" or status == "EXISTS_IDENTICAL") and #tail == 2 and
       CJ.Identities.positive(tail[1]) and count ~= nil and count <= 10000 then return true end
    return nil, "INVALID_ARGUMENT"
end)
local function prepare()
    local ctx, code = CJ.Context.open(CJ.Wire.run_spec("CJ2_BEGIN_RUN_AUDIT",{"run_id"}),KEYS,ARGV)
    if not ctx then return nil, code end
    local gate, err = CJ.Gate.check(ctx)
    if not gate then return nil, err end
    local run, failure = Run.load(ctx,ctx.request.v.run_id)
    if not run then return nil, failure end
    local mutable, mutation_error = Run.mutable(ctx,run)
    if not mutable then return nil, mutation_error end
    local plan, plan_error = CJ.Plan.new(ctx)
    if not plan then return nil, plan_error end
    if run.v.state == "auditing" then
        return Run.finish(ctx,plan,"EXISTS_IDENTICAL",{run.v.audit_revision,run.v.job_count})
    end
    if run.v.state ~= "loading" then return nil, "INVALID_STATE" end
    local delta, delta_error = Run.plan_delta(ctx,run)
    if not delta then return nil, delta_error end
    local ok, set_error = Run.set(delta,{state="auditing",audit_revision=run.v.load_revision,last_activity_at_ms=ctx.now_text})
    if not ok then return nil, set_error end
    local begun, begin_error = Run.begin_audit(delta)
    if not begun then return nil, begin_error end
    local post, flush_error = Run.flush(ctx,plan,delta)
    if not post then return nil, flush_error end
    return Run.finish(ctx,plan,"AUDIT_STARTED",{post.v.audit_revision,post.v.job_count})
end
local execution, code = prepare()
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
