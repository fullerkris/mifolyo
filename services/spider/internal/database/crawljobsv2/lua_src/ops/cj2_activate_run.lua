local Run, I = CJ.Run, CJ.Identities
CJ.Reply.register("CJ2_ACTIVATE_RUN",function(ctx,status,tail)
    local at = P.parse_decimal(tail[1])
    if (status == "ACTIVATED" or status == "EXISTS_IDENTICAL") and #tail == 1 and at ~= nil and at > 0 and at <= ctx.now_ms then return true end
    return nil, "INVALID_ARGUMENT"
end)
local function prepare()
    local fields = {"run_id","confirmation_text","authorization_sha256","crawl_policy_sha256","render_policy_sha256","canonicalization_sha256"}
    local ctx, code = CJ.Context.open(CJ.Wire.run_spec("CJ2_ACTIVATE_RUN",fields),KEYS,ARGV)
    if not ctx then return nil, code end
    local v = ctx.request.v
    for i=3,#fields do if not I.digest(v[fields[i]]) then return nil, "INVALID_ARGUMENT" end end
    local gate, err = CJ.Gate.check(ctx)
    if not gate then return nil, err end
    local run, failure = Run.load(ctx,v.run_id)
    if not run then return nil, failure end
    local mutable, mutation_error = Run.mutable(ctx,run)
    if not mutable then return nil, mutation_error end
    for i=3,#fields do if v[fields[i]] ~= run.v[fields[i]] then return nil, "IMMUTABLE_MISMATCH" end end
    if v.confirmation_text ~= v.run_id..":"..run.v.source_sha256..":"..run.v.authorization_sha256..":"..run.v.crawl_policy_sha256 then
        return nil, "IMMUTABLE_MISMATCH"
    end
    -- Exact current active replay still verifies all five literal legacy keys,
    -- but does not require a new sixty-second window or re-run admission.
    for _, pair in ipairs({{"legacy_queue","zset"},{"legacy_urls","hash"},{"legacy_depths","hash"},
        {"legacy_spider_queue","list"},{"legacy_signal_queue","list"}}) do
        local absent, absent_error = CJ.Read.absent(ctx,ctx.keys[pair[1]],pair[2])
        if not absent then return nil, absent_error end
    end
    local plan, plan_error = CJ.Plan.new(ctx)
    if not plan then return nil, plan_error end
    if run.v.state == "active" then return Run.finish(ctx,plan,"EXISTS_IDENTICAL",{run.v.activated_at_ms}) end
    if run.v.state ~= "sealed" then return nil, "INVALID_STATE" end
    if run.n.authorization_expires_at_ms < ctx.now_ms or run.n.authorization_expires_at_ms-ctx.now_ms < 60000 then
        return nil, "INVALID_STATE"
    end
    local delta, delta_error = Run.plan_delta(ctx,run)
    if not delta then return nil, delta_error end
    local ok, set_error = Run.set(delta,{state="active",activated_at_ms=ctx.now_text,last_activity_at_ms=ctx.now_text})
    if not ok then return nil, set_error end
    local post, flush_error = Run.flush(ctx,plan,delta)
    if not post then return nil, flush_error end
    return Run.finish(ctx,plan,"ACTIVATED",{post.v.activated_at_ms})
end
local execution, code = prepare()
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
