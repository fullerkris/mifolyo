local Run = CJ.Run
local statuses = {all_jobs_terminal="COMPLETED",request_budget_exhausted="RUN_BUDGET_EXHAUSTED",
    reservation_limit_exhausted="RUN_RESERVATION_LIMIT_EXHAUSTED",group_budgets_exhausted="GROUP_BUDGET_EXHAUSTED",
    operator_cancelled="CANCELLED",source_cancelled="CANCELLED",authorization_expired="CANCELLED"}
CJ.Reply.register("CJ2_FINALIZE_RUN",function(ctx,status,tail)
    if #tail ~= 2 then return nil, "INVALID_ARGUMENT" end
    if status == "NOT_DUE" and tail[1] == "0" and tail[2] == "none" then return true end
    local at = P.parse_decimal(tail[1])
    if statuses[tail[2]] == status and at ~= nil and at > 0 and at <= ctx.now_ms then return true end
    return nil, "INVALID_ARGUMENT"
end)
local function prepare()
    local ctx, code = CJ.Context.open(CJ.Wire.run_spec("CJ2_FINALIZE_RUN",{"run_id"}),KEYS,ARGV)
    if not ctx then return nil, code end
    local gate, err = CJ.Gate.check(ctx)
    if not gate then return nil, err end
    local run, failure = Run.load(ctx,ctx.request.v.run_id)
    if not run then return nil, failure end
    local mutable, mutation_error = Run.mutable(ctx,run)
    if not mutable then return nil, mutation_error end
    local live, live_error = Run.live(ctx,run)
    if not live then return nil, live_error end
    local plan, plan_error = CJ.Plan.new(ctx)
    if not plan then return nil, plan_error end
    if run.v.state == "archived" then return nil, "INVALID_STATE" end
    if run.n.finalized_at_ms > 0 then
        if not live.drained then return nil, "STATE_INDEX_CORRUPT" end
        return Run.finish(ctx,plan,statuses[run.v.terminal_reason],{run.v.finalized_at_ms,run.v.terminal_reason})
    end
    if run.v.state ~= "active" and run.v.state ~= "cancelled" then return nil, "INVALID_STATE" end
    local reason, state, timestamp
    if live.drained then
        if run.v.state == "cancelled" then
            if run.n.open_job_count == 0 then state,reason = "cancelled",run.v.terminal_reason end
        elseif run.n.open_job_count == 0 then
            state,reason,timestamp = "completed","all_jobs_terminal","completed_at_ms"
        elseif run.n.request_starts == run.n.max_request_starts then
            state,reason,timestamp = "budget_exhausted","request_budget_exhausted","budget_exhausted_at_ms"
        elseif run.n.reservation_creations_total == 100 then
            state,reason,timestamp = "budget_exhausted","reservation_limit_exhausted","budget_exhausted_at_ms"
        elseif run.all_open_groups_exhausted then
            state,reason,timestamp = "budget_exhausted","group_budgets_exhausted","budget_exhausted_at_ms"
        end
    end
    if not state then return Run.finish(ctx,plan,"NOT_DUE",{"0","none"}) end
    local delta, delta_error = Run.plan_delta(ctx,run)
    if not delta then return nil, delta_error end
    local values = {state=state,terminal_reason=reason,finalized_at_ms=ctx.now_text,last_activity_at_ms=ctx.now_text,
        retention_anchor_ms=P.format_decimal(math.max(ctx.now_ms,run.n.last_request_started_at_ms,run.n.last_terminal_transition_at_ms,run.n.last_activity_at_ms))}
    if timestamp then values[timestamp] = ctx.now_text end
    local ok, set_error = Run.set(delta,values)
    if not ok then return nil, set_error end
    local removed, remove_error = Run.member(delta,"active_runs",false)
    if not removed then return nil, remove_error end
    local post, flush_error = Run.flush(ctx,plan,delta)
    if not post then return nil, flush_error end
    return Run.finish(ctx,plan,statuses[reason],{post.v.finalized_at_ms,reason})
end
local execution, code = prepare()
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
