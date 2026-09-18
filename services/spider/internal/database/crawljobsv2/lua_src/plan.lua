local L, C, I, R = {}, CJ.Context, CJ.Identities, CJ.Read
local plans, assessments, units = {}, {}, {}
local SLOT="mifolyo:crawl:v2:stage_slots"
-- 500-job enqueue needs at least 500*(HSET job + SADD jobs + ZADD order +
-- ZADD ready + ZADD ready_at)=2500 descriptors plus bounded shared updates.
-- 4096 covers that concrete shape; this is not an increased protocol batch.
local MAX_CALLS, MAX_ARGC, MAX_INPUT_ARGC = 4096, 258, 1002
local function descriptor(argv,ctx)
    local argc, code = I.dense(argv,MAX_INPUT_ARGC)
    if not argc then return nil, code end
    if argc < 2 then return nil, "INVALID_ARGUMENT" end
    local bytes, copied = 0, {}
    for i = 1, argc do
        if type(argv[i]) ~= "string" then return nil, "INVALID_ARGUMENT" end
        bytes = bytes + #argv[i]
        if bytes > (ctx.operation=="CJ2_STAGE_PAGE_BLOB" and 5373952 or 2097152) then return nil, "COMMAND_BOUNDS_EXCEEDED" end
        copied[i] = argv[i]
    end
    local command, width = copied[1], 0
    if command == "HSET" or command == "ZADD" then
        if argc < 4 or argc % 2 ~= 0 then return nil, "INVALID_ARGUMENT" end
        width = 2
    elseif command == "SADD" or command == "SREM" or command == "ZREM" or command == "HDEL" or command=="LPUSH" then
        if argc < 3 then return nil, "INVALID_ARGUMENT" end
        width = 1
    elseif command == "SET" or command == "RENAME" then
        if argc ~= 3 or (command == "RENAME" and copied[2] == copied[3]) then return nil, "INVALID_ARGUMENT" end
    elseif command == "UNLINK" or command == "PERSIST" then
        if argc ~= 2 then return nil, "INVALID_ARGUMENT" end
    elseif command=="PEXPIREAT" or command=="EXPIRE" then
        if argc~=3 then return nil,"INVALID_ARGUMENT" end
        local n=P.parse_decimal(copied[3]);if not n or n==0 then return nil,"INVALID_NUMBER" end
        if command=="PEXPIREAT" then
            if n<=ctx.now_ms then return nil,"INVALID_ARGUMENT" end
        elseif n>86400 or not P.safe_add(ctx.now_ms,n*1000) then return nil,"INVALID_NUMBER" end
    else return nil, "INVALID_ARGUMENT" end
    if width > 0 and command~="LPUSH" then
        local seen = {}
        for i = 3, argc, width do
            local member = copied[i]
            if command == "ZADD" then
                if I.zadd_score(copied[i]) == nil then return nil, "INVALID_NUMBER" end
                member = copied[i+1]
            end
            if seen[member] then return nil, "INVALID_ARGUMENT" end
            seen[member] = true
        end
    end
    return {argv=copied,argc=argc,width=width}
end
function L.new(ctx)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    local handle = {}
    plans[handle] = {ctx=ctx,calls={},state="building"}
    return handle
end
function L.set_policy(plan,mode,proof)
    local state=plans[plan]
    if not state or not C.preparing(state.ctx) or state.state~="building" or #state.calls~=0 or state.policy then return nil,"INVALID_STATE" end
    local policy,code=CJ.Memory.policy(state.ctx,mode,proof)
    if not policy then return nil,code end
    state.policy,state.units,state.unit_ids=policy,{},{}
    return true
end
function L.recovery_unit(plan,job_id)
    local state=plans[plan]
    if not state or not C.preparing(state.ctx) or state.state~="building" or not state.policy or state.policy.mode~="recovery" then return nil,"INVALID_STATE" end
    if state.unit_ids[job_id] then return nil,"INVALID_ARGUMENT" end
    if #state.units>=100 then return nil,"LIMIT_EXCEEDED" end
    local p,code=CJ.Memory.recovery_unit(state.ctx,job_id);if not p then return nil,code end
    local handle={};units[handle]={plan=plan,policy=p};state.units[#state.units+1]=handle;state.unit_ids[job_id]=true
    return handle
end
function L.validate_unit(plan,unit)
    local state,proof=plans[plan],units[unit]
    if not state or not C.preparing(state.ctx) or state.state~="building" or not state.policy or state.policy.mode~="recovery" then return nil,"INVALID_STATE" end
    if not proof or proof.plan~=plan then return nil,"INVALID_ARGUMENT" end
    return true
end
function L.add(plan, argv, coverage)
    local state = plans[plan]
    if not state or not C.preparing(state.ctx) or state.state ~= "building" then return nil, "INVALID_STATE" end
    local writable, restriction = C.plan_check(state.ctx,#state.calls+1)
    if not writable then return nil, restriction end
    local unit
    if state.policy and state.policy.mode=="recovery" then
        unit=units[coverage]
        if not unit or unit.plan~=plan then return nil,"INVALID_ARGUMENT" end
    elseif coverage ~= "ordinary" and coverage ~= "cancel_run" then return nil, "INVALID_ARGUMENT" end
    if state.policy and not unit and coverage~="ordinary" then return nil,"INVALID_ARGUMENT" end
    local call, code = descriptor(argv,state.ctx)
    if not call then return nil, code end
    if call.argv[2]==SLOT then return nil,"INVALID_ARGUMENT" end
    if not C.can_write(state.ctx,call.argv[2]) or (call.argv[1] == "RENAME" and not C.can_write(state.ctx,call.argv[3])) then return nil, "INVALID_ARGUMENT" end
    if coverage == "cancel_run" and (state.ctx.operation ~= "CJ2_CANCEL_RUN" or call.argv[1] ~= "HSET" or
       call.argv[2] ~= state.ctx.keys.run or call.argc ~= 10 or #state.calls ~= 0) then return nil, "INVALID_ARGUMENT" end
    if state.coverage and not unit and state.coverage ~= coverage then return nil, "INVALID_ARGUMENT" end
    if state.coverage == "cancel_run" then return nil, "INVALID_ARGUMENT" end
    -- Validate/copy the ENTIRE logical descriptor before splitting. No silent
    -- truncation at the unpack ceiling, and no partially-added failed command.
    local built = {}
    if call.argc <= MAX_ARGC then built[1] = {argv=call.argv,argc=call.argc}
    else
        if call.width == 0 then return nil, "COMMAND_BOUNDS_EXCEEDED" end
        for offset = 3, call.argc, 256 do
            local finish, parts = math.min(offset+255,call.argc), {call.argv[1],call.argv[2]}
            for i = offset, finish do parts[#parts+1] = call.argv[i] end
            built[#built+1] = {argv=parts,argc=#parts}
        end
    end
    if #state.calls + #built > MAX_CALLS then return nil, "LIMIT_EXCEEDED" end
    for i = 1, #built do
        built[i].unit=unit and coverage or nil
        built[i].effect_policy=unit and unit.policy or state.policy
        state.calls[#state.calls+1] = built[i]
    end
    state.coverage = unit and "recovery" or coverage
    return true
end
local function empty(key)
    return {key=key,kind="none",exists=false,count=0,complete=true,v={},members={},scores={},score_text={},ttl_ms=-2}
end
local function known(fact, values, name)
    if not values then return nil, "INVALID_STATE" end
    local value = values[name]
    if value == nil then
        if fact.complete then return false end
        return nil, "INVALID_STATE"
    end
    return value
end
local function cancel_eligible(ctx, state, pre)
    if state.coverage ~= "cancel_run" then return false end
    if #state.calls ~= 1 or not pre.exists or pre.kind ~= "hash" or pre.schema ~= "run" or pre.complete ~= true then return nil, "INVALID_STATE" end
    local v, expected = pre.v, {state="cancelled",cancelled_at_ms=ctx.now_text,last_activity_at_ms=ctx.now_text,
        terminal_reason=ctx.request.v.reason}
    if expected.terminal_reason ~= "operator_cancelled" and expected.terminal_reason ~= "source_cancelled" and
       expected.terminal_reason ~= "authorization_expired" then return nil, "INVALID_ARGUMENT" end
    if v.state ~= "loading" and v.state ~= "auditing" and v.state ~= "sealed" and v.state ~= "active" then return nil, "INVALID_STATE" end
    if v.cancelled_at_ms ~= "0" or v.terminal_reason ~= "none" then return nil, "INVALID_STATE" end
    local argv = state.calls[1].argv
    for i = 3, #argv, 2 do
        if expected[argv[i]] == nil or argv[i+1] ~= expected[argv[i]] or type(v[argv[i]]) ~= "string" then return nil, "INVALID_ARGUMENT" end
    end
    return true
end
local function simulate(ctx,calls)
    local logical, keys, elements, projected, charges, delta_budgets = 0, 0, 0, {}, {}, {}
    local function get(key)
        if projected[key] then return projected[key] end
        local pre, code = R.snapshot(ctx,key)
        if not pre then return nil, code end
        projected[key] = pre
        return pre
    end
    for i = 1, #calls do
        local before_logical,before_keys,before_elements=logical,keys,elements
        local argv = calls[i].argv
        local command, key = argv[1], argv[2]
        local pre, code = get(key)
        if not pre then return nil, code end
        if calls[i].effect_policy then
            local p=calls[i].effect_policy
            if not delta_budgets[p] then delta_budgets[p]={} end
            local ok,err=CJ.Memory.unit_delta(ctx,p,argv,pre,delta_budgets[p]);if not ok then return nil,err end
        end
        if command == "UNLINK" then
            projected[key] = empty(key) -- no deletion credits
        elseif command == "PERSIST" then
            if pre.ttl_ms == nil then return nil, "INVALID_STATE" end
            if pre.exists then pre.ttl_ms,pre.expires_at_ms = -1,false end
        elseif command=="PEXPIREAT" or command=="EXPIRE" then
            if not pre.exists or pre.ttl_ms==nil then return nil,"INVALID_STATE" end
            local at=P.parse_decimal(argv[3])
            if command=="EXPIRE" then at=P.safe_add(ctx.now_ms,at*1000) end
            if not at or at<=ctx.now_ms then return nil,"INVALID_NUMBER" end
            pre.ttl_ms,pre.expires_at_ms=at-ctx.now_ms,at
        elseif command == "RENAME" then
            local destination, err = get(argv[3])
            if not destination then return nil, err end
            if not pre.exists then return nil, "INVALID_STATE" end
            if destination.exists then return nil, "DESTINATION_EXISTS" end
            -- Deliberately non-overwriting rename subset. Carry the entire
            -- receipt (including unknown fields and TTL) to the new position.
            logical, keys = logical + #key + #argv[3], keys + 1
            pre.key, projected[argv[3]], projected[key] = argv[3], pre, empty(key)
        else
            local wanted
            if command == "HSET" or command == "HDEL" then wanted = "hash"
            elseif command == "SADD" or command == "SREM" then wanted = "set"
            elseif command == "ZADD" or command == "ZREM" then wanted = "zset"
            elseif command=="LPUSH" then wanted="list"
            else wanted = "string" end
            if pre.kind ~= "none" and pre.kind ~= wanted then return nil, "WRONG_TYPE" end
            local creates = command == "HSET" or command == "SET" or command == "SADD" or command == "ZADD" or command=="LPUSH"
            if not pre.exists and creates then
                keys, logical = keys + 1, logical + #key
                pre.kind, pre.exists, pre.ttl_ms = wanted, true, -1
            end
            if command=="LPUSH" then
                if pre.count==nil then return nil,"INVALID_STATE" end
                local count=P.safe_add(pre.count,#argv-2);if not count then return nil,"INVALID_NUMBER" end
                for j=3,#argv do
                    logical,elements=logical+#argv[j],elements+1
                    if pre.complete then
                        if not pre.items then pre.items={} end
                        table.insert(pre.items,1,argv[j])
                    end
                end
                pre.count=count
            elseif command == "SET" then
                if not pre.complete then return nil, "INVALID_STATE" end
                if pre.value ~= argv[3] then logical = logical + #argv[3] end
                pre.value, pre.ttl_ms,pre.expires_at_ms = argv[3], -1,false
            else
                if pre.count == nil then return nil, "INVALID_STATE" end
                local width = (command == "HSET" or command == "ZADD") and 2 or 1
                for j = 3, #argv, width do
                    local field, value = argv[j], argv[j+1]
                    if command == "HSET" or command == "HDEL" then
                        local old, err = known(pre,pre.v,field)
                        if old == nil then return nil, err end
                        if command == "HSET" then
                            if old == false then
                                local count=P.safe_add(pre.count,1);if not count then return nil,"INVALID_NUMBER" end
                                logical, elements, pre.count = logical + #field, elements + 1, count
                            end
                            if old ~= value then logical = logical + #value end
                            pre.v[field] = value
                        elseif old ~= false then pre.v[field], pre.count = false, pre.count - 1 end
                    else
                        local score
                        if command == "ZADD" then
                            score = I.zadd_score(field)
                            if score == nil then return nil, "INVALID_NUMBER" end
                            field = value
                        end
                        local old, err = known(pre,pre.members,field)
                        if old == nil then return nil, err end
                        if command == "SADD" or command == "ZADD" then
                            if old == false then
                                local count=P.safe_add(pre.count,1);if not count then return nil,"INVALID_NUMBER" end
                                logical, elements, pre.count = logical + #field, elements + 1, count
                            end
                            if command == "ZADD" then
                                if old and type(pre.scores[field]) ~= "number" then return nil, "INVALID_STATE" end
                                if old == false or pre.scores[field] ~= score then logical = logical + #argv[j] end
                                pre.scores[field], pre.score_text[field] = score, argv[j]
                            end
                            pre.members[field] = true
                        elseif old then
                            pre.members[field], pre.count = false, pre.count - 1
                            if wanted == "zset" then pre.scores[field], pre.score_text[field] = false, false end
                        end
                    end
                end
                if pre.count == 0 then projected[key] = empty(key) end
            end
        end
        charges[i]=3*(logical-before_logical)+1024*(keys-before_keys)+256*(elements-before_elements)
    end
    local growth=3*logical+1024*keys+256*elements
    if not I.integer(growth,P.limits.max_integer) then return nil,"INVALID_NUMBER" end
    return {growth=growth,new_logical_bytes=logical,new_keys=keys,new_elements=elements,projected=projected,charges=charges}
end
local function append_call(calls,argv)
    local out={};for i,call in ipairs(calls) do out[i]=call end
    out[#out+1]={argv=argv,argc=#argv};return out
end
-- Exactly eight possible widths; independently simulate each final candidate.
-- No old-width overcharge, numeric caller G, mutation, or temporary debit write.
local function debit(ctx,calls,p)
    local base,code=simulate(ctx,calls);if not base then return nil,code end
    if base.growth>p.remaining then return nil,"MEMORY_HEADROOM_LOW" end
    local suffix=":"..p.run_id..":"..p.job_id..":"..p.fence..":"..P.format_decimal(p.abort_unlinked_keys)
    local function try(remaining)
        if not I.integer(remaining,50331648) then return nil end
        local value=P.format_decimal(remaining)..suffix
        local result_calls=append_call(calls,{"HSET",SLOT,p.commit_id,value})
        local result=simulate(ctx,result_calls)
        if result and p.remaining-remaining==result.growth then return {calls=result_calls,cost=result,remaining=remaining} end
        return nil
    end
    -- An identical no-growth replacement is distinct from changed full bytes.
    local zero=try(p.remaining);if zero then return zero end
    local f,err=R.snapshot(ctx,SLOT);if not f then return nil,err end
    local old,why=known(f,f.v,p.commit_id);if old==nil then return nil,why end
    local overhead=0
    if not f.exists then overhead=1024+3*#SLOT end
    if old==false then overhead=overhead+256+3*#p.commit_id end
    for width=1,8 do
        local growth=base.growth+overhead+3*(width+#suffix)
        local remaining=p.remaining-growth
        if remaining>=0 then
            local text=P.format_decimal(remaining)
            if text and #text==width then local solved=try(remaining);if solved then return solved end end
        end
    end
    return nil,"INVALID_STATE"
end
local function finish_slots(ctx,state,calls,cost)
    local policy=state.policy
    if not policy then return {calls=calls,cost=cost,remaining_by_commit={}} end
    if #calls==0 then return {calls=calls,cost=cost,remaining=policy.remaining,remaining_by_commit={}} end
    if policy.mode=="begin" or policy.mode=="stage" or policy.mode=="abort" or policy.mode=="backpressure" then
        local solved,code=debit(ctx,calls,policy);if not solved then return nil,code end
        local floor=(policy.mode=="begin" or policy.mode=="stage") and 65536 or (policy.mode=="backpressure" and 32768 or 0)
        if solved.remaining<floor then return nil,"MEMORY_HEADROOM_LOW" end
        if policy.mode=="abort" then
            local future,err=CJ.Memory.post_abort_bound(ctx,policy);if not future then return nil,err end
            if solved.cost.growth+future>32768 or policy.remaining<solved.cost.growth+future then return nil,"MEMORY_HEADROOM_LOW" end
            solved.post_abort_bound=future
        end
        solved.remaining_by_commit={[policy.commit_id]=solved.remaining}
        return solved
    end
    local removals,seen={},{}
    local function removal(p,growth)
        if p.commit_id then
            if seen[p.commit_id] or growth>p.remaining then return nil,"MEMORY_HEADROOM_LOW" end
            seen[p.commit_id]=true;removals[#removals+1]=p.commit_id
        end
        return true
    end
    local covered,uncovered=0,cost.growth
    local unit_growth={}
    if policy.mode=="slot_end" then
        local maximum,code=CJ.Memory.post_abort_bound(ctx,policy);if not maximum then return nil,code end
        if cost.growth>32768 or cost.growth>maximum then return nil,"MEMORY_HEADROOM_LOW" end
        local ok,code=removal(policy,cost.growth);if not ok then return nil,code end
        covered,uncovered=cost.growth,0
    elseif policy.mode=="commit" then
        -- Commit G comes from the independent commit reserve, not this slot.
        removals[1]=policy.commit_id
    elseif policy.mode=="recovery" then
        uncovered=0
        for i,call in ipairs(calls) do
            if not units[call.unit] then return nil,"INVALID_STATE" end
            unit_growth[call.unit]=(unit_growth[call.unit] or 0)+cost.charges[i]
        end
        for _,unit in ipairs(state.units) do
            local p,growth=units[unit].policy,unit_growth[unit] or 0
            if p.commit_id then
                if growth>32768 then return nil,"MEMORY_HEADROOM_LOW" end
                if p.abort_unlinked_keys>0 then
                    local maximum,code=CJ.Memory.post_abort_bound(ctx,p);if not maximum then return nil,code end
                    if growth>maximum then return nil,"MEMORY_HEADROOM_LOW" end
                end
                local ok,code=removal(p,growth);if not ok then return nil,code end
                covered=covered+growth
            else uncovered=uncovered+growth end
        end
    end
    local result_calls=calls
    -- Removal is last, after every covered effect; never HSET an intermediate
    -- remainder on a slot that this invocation will remove.
    for _,commit in ipairs(removals) do result_calls=append_call(result_calls,{"HDEL",SLOT,commit}) end
    local final,code=simulate(ctx,result_calls);if not final then return nil,code end
    if final.growth~=cost.growth then return nil,"INVALID_STATE" end
    return {calls=result_calls,cost=final,covered_growth=covered,uncovered_growth=uncovered,remaining_by_commit={}}
end
local function required(observation,parts)
    local n=observation.used
    for _,part in ipairs(parts) do n=P.safe_add(n,part);if not n then return nil end end
    return n
end
local function policy_post(ctx,p,cost,calls)
    for _,fact in next,cost.projected,nil do
        if fact.exists and fact.kind=="hash" and fact.schema then
            if not CJ.Schemas.project(fact.schema,fact.v) then return nil,"INVALID_STATE" end
        end
    end
    if p.mode=="safety" and ctx.operation=="CJ2_RELEASE_BEFORE_IO" and p.reservation then
        local clear_at
        for i,call in ipairs(calls) do
            local a=call.argv
            if a[1]=="HSET" and a[2]==p.job_key then for n=3,#a,2 do
                if (a[n]=="state" and a[n+1]~="leased") or
                   ((a[n]=="lease_owner" or a[n]=="lease_token") and a[n+1]=="") or
                   ((a[n]=="lease_started_at_ms" or a[n]=="lease_expires_at_ms") and a[n+1]=="0") then clear_at=clear_at or i end
            end end
        end
        if not clear_at then return nil,"INVALID_STATE" end
        local prefix={};for i=1,clear_at-1 do prefix[i]=calls[i] end
        local before,code=simulate(ctx,prefix);if not before then return nil,code end
        local q=p.reservation.v
        local record=before.projected["mifolyo:crawl:v2:reservation:"..q.reservation_id]
        if not record or not record.exists or record.v.state~="cancelled" or record.v.terminal_at_ms~=ctx.now_text or
           record.expires_at_ms~=P.safe_add(ctx.now_ms,86400000) then return nil,"RESERVATION_CORRUPT" end
        for _,kind in ipairs({"global","group","origin"}) do
            local scope=CJ.Wire.rate_keys(q[kind.."_scope_id"])
            for _,name in ipairs({"active","pending"}) do
                local index=before.projected[scope[name]]
                if not index or not index.members or index.members[q.reservation_id]==true or
                   (index.members[q.reservation_id]==nil and not index.complete) then return nil,"RATE_STATE_CORRUPT" end
            end
            local initial=R.snapshot(ctx,scope.scope);local final=before.projected[scope.scope]
            if not initial or not final or P.parse_decimal(final.v.active_count)~=P.parse_decimal(initial.v.active_count)-1 or
               P.parse_decimal(final.v.pending_count)~=P.parse_decimal(initial.v.pending_count)-1 or
               final.v.started_count~=initial.v.started_count then return nil,"RATE_STATE_CORRUPT" end
        end
    elseif p.mode=="begin" then
        local meta=cost.projected[p.keys.meta];local inv=cost.projected[p.keys.keys]
        local job=cost.projected[p.job_key];local expiry=cost.projected["mifolyo:crawl:v2:stage_expiry"]
        local record=meta and meta.exists and CJ.Schemas.project("stage_meta",meta.v)
        if not record or CJ.Schemas.encode(record)~=CJ.Schemas.encode(p.begin.meta) or not inv or not inv.exists or inv.count~=2 or not inv.items or
           not job or not job.exists or job.v.active_stage_commit_id~=p.commit_id or job.v.last_stage_commit_id~=p.commit_id or job.v.last_stage_fence~=p.fence or
           meta.expires_at_ms~=p.begin.expires_at_ms or inv.expires_at_ms~=p.begin.expires_at_ms or
           not expiry or expiry.members[p.commit_id]~=true or expiry.scores[p.commit_id]~=p.begin.expires_at_ms then return nil,"STAGE_INVALID" end
        if not ((inv.items[1]==p.keys.meta and inv.items[2]==p.keys.keys) or (inv.items[2]==p.keys.meta and inv.items[1]==p.keys.keys)) then return nil,"STAGE_INVALID" end
    elseif p.mode=="stage" then
        for key,fact in next,cost.projected,nil do
            if p.keys.kind[key] and fact.exists and fact.expires_at_ms~=p.meta.n.expires_at_ms then return nil,"STAGE_INVALID" end
        end
        local meta=cost.projected[p.keys.meta]
        if meta and not CJ.Schemas.project("stage_meta",meta.v) then return nil,"STAGE_INVALID" end
        local inv=cost.projected[p.keys.keys]
        if inv and inv.items then
            local seen={};for _,key in ipairs(inv.items) do if not p.keys.kind[key] or seen[key] then return nil,"STAGE_INVALID" end;seen[key]=true end
        end
    end
    return true
end
function L.assess(ctx, plan)
    local state = plans[plan]
    if not state or state.ctx ~= ctx or not C.preparing(ctx) or state.state ~= "building" then return nil, "INVALID_STATE" end
    local writable, restriction = C.plan_check(ctx,#state.calls)
    if not writable then return nil, restriction end
    local safety=false
    if state.coverage=="cancel_run" then
        local pre,code=R.snapshot(ctx,ctx.keys.run);if not pre then return nil,code end
        safety,code=cancel_eligible(ctx,state,pre);if not safety then return nil,code end
    end
    local mandatory = ctx.operation=="CJ2_BEGIN_STAGE" or ctx.operation=="CJ2_ABORT_STAGE" or ctx.operation=="CJ2_SEAL_STAGE" or
        ctx.operation=="CJ2_COMMIT" or ctx.operation=="CJ2_RECOVER_EXPIRED" or string.sub(ctx.operation,1,10)=="CJ2_STAGE_"
    if #state.calls>0 and mandatory and not state.policy then return nil,"INVALID_STATE" end
    if state.policy then
        for _,call in ipairs(state.calls) do
            local policy=state.policy.mode=="recovery" and units[call.unit] and units[call.unit].policy or state.policy
            if not policy or policy.mode=="recovery" then return nil,"INVALID_STATE" end
            local ok,err=CJ.Memory.footprint(ctx,policy,call.argv);if not ok then return nil,err end
        end
    end
    local cost,code=simulate(ctx,state.calls);if not cost then return nil,code end
    if state.policy and state.policy.mode=="recovery" and #state.calls>0 then
        local policies={};for i,unit in ipairs(state.units) do policies[i]=units[unit].policy end
        local checked,why=CJ.Memory.recovery_effects(ctx,policies,state.calls,cost.projected)
        if not checked then return nil,why end
    end
    local final,finish_error=finish_slots(ctx,state,state.calls,cost);if not final then return nil,finish_error end
    if #final.calls>MAX_CALLS then return nil,"LIMIT_EXCEEDED" end
    cost=final.cost
    if state.policy and #state.calls>0 then
        local valid,err=policy_post(ctx,state.policy,cost,final.calls);if not valid then return nil,err end
    end
    local logical,keys,elements=cost.new_logical_bytes,cost.new_keys,cost.new_elements
    -- 4096 descriptors * <=2 MiB per input and bounded key/element overhead
    -- keep products below 2^53. All additions to INFO values are checked.
    local growth, admission = 3*logical + 1024*keys + 256*elements, "ordinary"
    if not I.integer(growth,P.limits.max_integer) then return nil, "INVALID_NUMBER" end
    local deletion_only=#final.calls>0 and state.policy==nil and state.coverage=="ordinary"
    for _,call in ipairs(final.calls) do
        if not ({UNLINK=true,HDEL=true,SREM=true,ZREM=true,PERSIST=true})[call.argv[1]] then deletion_only=false end
    end
    if deletion_only then
        if growth~=0 then return nil,"INVALID_STATE" end
        admission="deletion_only"
    elseif #final.calls > 0 then
        local observation, code = CJ.Memory.observe(ctx)
        if not observation then return nil, code end
        local mode=state.policy and state.policy.mode
        local charge=growth
        if mode=="begin" then
            local slots,err=CJ.Read.slots(ctx);if not slots then return nil,err end
            if slots.count>=4 then return nil,"LIMIT_EXCEEDED" end
            local sum=required(observation,{observation.slots,50331648,67108864,16777216})
            if not sum or sum>observation.maximum then return nil,"MEMORY_HEADROOM_LOW" end
            admission="begin"
        elseif mode=="stage" or mode=="abort" or mode=="backpressure" then
            local sum=required(observation,{observation.slots,67108864,16777216})
            if not sum or sum>observation.maximum then return nil,"MEMORY_HEADROOM_LOW" end
            admission=mode
        elseif mode=="commit" then
            if state.policy.queue_count>=5000 then return nil,"LIMIT_EXCEEDED" end
            local sum=required(observation,{observation.slots,16777216,growth})
            if growth>67108864 or not sum or sum>observation.maximum then return nil,"MEMORY_HEADROOM_LOW" end
            admission="commit"
        else
            if mode=="slot_end" or mode=="recovery" then charge=final.uncovered_growth;safety=true end
            if mode=="safety" then safety=true end
            local base=required(observation,{observation.slots,67108864,charge})
            if not base then return nil,"MEMORY_HEADROOM_LOW" end
            local ordinary=P.safe_add(base,16777216)
            if not ordinary or ordinary>observation.maximum then
                if not safety or base>observation.maximum then return nil,"MEMORY_HEADROOM_LOW" end
                admission=mode and (mode.."_safety") or "cancel_run_safety"
            elseif mode then admission=mode end
        end
    end
    local assessment = {growth=growth,new_logical_bytes=logical,new_keys=keys,new_elements=elements,admission=admission,
        remaining=final.remaining,covered_growth=final.covered_growth or 0,uncovered_growth=final.uncovered_growth or growth,
        post_abort_bound=final.post_abort_bound}
    assessments[assessment] = {plan=plan,growth=growth,logical=logical,keys=keys,elements=elements,admission=admission,
        remaining=assessment.remaining,covered_growth=assessment.covered_growth,uncovered_growth=assessment.uncovered_growth,post_abort_bound=assessment.post_abort_bound}
    state.state, state.assessment,state.calls = "assessed", assessment,final.calls
    return assessment
end
function L.seal(ctx, plan, assessment, reply)
    local state, proof = plans[plan], assessments[assessment]
    if not state or state.ctx ~= ctx or not C.preparing(ctx) or state.state ~= "assessed" or not proof or proof.plan ~= plan or
       state.assessment ~= assessment or assessment.growth ~= proof.growth or assessment.new_logical_bytes ~= proof.logical or
        assessment.new_keys ~= proof.keys or assessment.new_elements ~= proof.elements or assessment.admission ~= proof.admission or
        assessment.remaining~=proof.remaining or assessment.covered_growth~=proof.covered_growth or assessment.uncovered_growth~=proof.uncovered_growth or
        assessment.post_abort_bound~=proof.post_abort_bound then return nil, "INVALID_STATE" end
    local writable, restriction = C.plan_check(ctx,#state.calls)
    if not writable then return nil, restriction end
    local count = I.dense(reply,9)
    if not count or count < 2 then return nil, "INVALID_ARGUMENT" end
    local tail = {}
    for i = 3, count do tail[i-2] = reply[i] end
    local checked, code = C.Reply.build(ctx,reply[1],tail)
    if not checked then return nil, code end
    for i = 1, count do if checked[i] ~= reply[i] then return nil, "INVALID_ARGUMENT" end end
    local calls = state.calls
    local execution = {calls=calls,count=#calls,reply=checked}
    for i = 1, #calls do
        local call = calls[i]
        local ok, permitted = pcall(redis.acl_check_cmd,unpack(call.argv,1,call.argc))
        if not ok or permitted ~= true then return nil, "BOOT_UNAPPROVED" end
    end
    state.state = "sealed"
    local sealed, err = C.seal(ctx)
    if not sealed then return nil, err end
    return execution
end
return L
