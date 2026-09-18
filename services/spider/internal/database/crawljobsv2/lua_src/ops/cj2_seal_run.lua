local Run, I = CJ.Run, CJ.Identities
CJ.Reply.register("CJ2_SEAL_RUN",function(ctx,status,tail)
    local count = P.parse_decimal(tail[1])
    if (status == "SEALED" or status == "EXISTS_IDENTICAL") and #tail == 2 and
       count ~= nil and count <= 10000 and I.digest(tail[2]) then return true end
    return nil, "INVALID_ARGUMENT"
end)
local function prepare()
    local ctx, code = CJ.Context.open(CJ.Wire.run_spec("CJ2_SEAL_RUN",{"run_id","expected_job_count","source_sha256"}),KEYS,ARGV)
    if not ctx then return nil, code end
    local count = P.parse_decimal(ctx.request.v.expected_job_count)
    if not count then return nil, "INVALID_NUMBER" end
    if count > 10000 or not I.digest(ctx.request.v.source_sha256) then return nil, "INVALID_ARGUMENT" end
    local gate, err = CJ.Gate.check(ctx)
    if not gate then return nil, err end
    local run, failure = Run.load(ctx,ctx.request.v.run_id)
    if not run then return nil, failure end
    local mutable, mutation_error = Run.mutable(ctx,run)
    if not mutable then return nil, mutation_error end
    if ctx.request.v.source_sha256 ~= run.v.source_sha256 or count ~= run.n.job_count or count ~= run.n.expected_seed_count then
        return nil, "IMMUTABLE_MISMATCH"
    end
    local plan, plan_error = CJ.Plan.new(ctx)
    if not plan then return nil, plan_error end
    if run.v.state == "sealed" then return Run.finish(ctx,plan,"EXISTS_IDENTICAL",{run.v.job_count,run.v.source_sha256}) end
    if run.v.state ~= "auditing" or run.v.audit_complete ~= "1" then return nil, "INVALID_STATE" end
    local delta, delta_error = Run.plan_delta(ctx,run)
    if not delta then return nil, delta_error end
    local ok, set_error = Run.set(delta,{state="sealed",sealed_at_ms=ctx.now_text,last_activity_at_ms=ctx.now_text})
    if not ok then return nil, set_error end
    local post, flush_error = Run.flush(ctx,plan,delta)
    if not post then return nil, flush_error end
    return Run.finish(ctx,plan,"SEALED",{post.v.job_count,post.v.source_sha256})
end
local execution, code = prepare()
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
