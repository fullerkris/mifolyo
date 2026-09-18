local C, I = {}, CJ.Identities
local contexts = {}
local registration_open = true
local stage_slots_key = "mifolyo:crawl:v2:stage_slots"
local active_leases_key = "mifolyo:crawl:v2:active_leases"
local errors = {}
for code in string.gmatch([[BOOT_UNAPPROVED COMPATIBILITY_MISMATCH CONTRACT_MISMATCH WRONG_TYPE INVALID_ARGUMENT
INVALID_IDENTIFIER INVALID_NUMBER INVALID_STATE IMMUTABLE_MISMATCH URL_ID_COLLISION LIMIT_EXCEEDED COUNTER_CORRUPT
STATE_INDEX_CORRUPT RESERVATION_CORRUPT RATE_STATE_CORRUPT STAGE_INVALID STAGE_UNSEALED DESTINATION_EXISTS
OUTPUT_CONTRACT_MISMATCH COMMAND_BOUNDS_EXCEEDED MEMORY_HEADROOM_LOW RATE_SCOPE_CAPACITY_EXCEEDED
ADMIN_FREEZE_REQUIRED COMMIT_GUARD_UNAPPROVED]], "%S+") do errors[code] = true end
function C.reject(code)
    if not errors[code] then code = "INVALID_STATE" end
    return redis.error_reply("ERR CRAWL_V2_" .. code)
end
function C.error_code(code)
    if type(code) == "string" and errors[code] then return code end
    return "INVALID_STATE"
end
function C.preparing(ctx)
    return type(ctx) == "table" and contexts[ctx] ~= nil and contexts[ctx].phase == "prepare" and ctx.phase == "prepare"
end
function C.can_read(ctx, key)
    if not C.preparing(ctx) or type(key) ~= "string" then return false end
    local state = contexts[ctx]
    if state.wire_allowed[key] or state.derived_write[key] or state.read_only[key] or key == stage_slots_key then return true end
    return key == active_leases_key and (state.operation == "CJ2_ENQUEUE_BATCH" or state.operation == "CJ2_AUDIT_RUN_BATCH")
end
function C.can_write(ctx, key)
    return C.preparing(ctx) and type(key) == "string" and (contexts[ctx].wire_allowed[key] == true or contexts[ctx].derived_write[key] == true) and
        ctx.allowed[key] == true and contexts[ctx].receipt_kind == nil
end
function C.plan_check(ctx, count)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    if not I.integer(count,P.limits.max_integer) then return nil, "INVALID_ARGUMENT" end
    if count > 4096 then return nil, "LIMIT_EXCEEDED" end
    if contexts[ctx].receipt_kind ~= nil and count ~= 0 then return nil, "INVALID_STATE" end
    if contexts[ctx].pending_request ~= nil and count ~= 0 then return nil,"INVALID_STATE" end
    return true
end
-- Restriction only: called by the common gate after proving the closed
-- post-state. No public/client flag can remove it or grant write authority.
function C.lock_receipt(ctx, kind)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    local state = contexts[ctx]
    if not ((kind == "promoted" and state.operation == "CJ2_PROMOTE_CANDIDATE_CONTRACTS" and state.mode == "candidate") or
        (kind == "planned_shutdown" and state.operation == "CJ2_MARK_PLANNED_SHUTDOWN" and state.mode == "active")) then
        return nil, "INVALID_ARGUMENT"
    end
    if state.receipt_kind and state.receipt_kind ~= kind then return nil, "INVALID_STATE" end
    state.receipt_kind = kind
    return true
end
function C.bound_run(ctx)
    if not C.preparing(ctx) or contexts[ctx].bound_run_id == nil then return nil, "INVALID_STATE" end
    return contexts[ctx].bound_run_id
end
function C.bind_run_read(ctx, run_id)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    local state = contexts[ctx]
    if state.operation ~= "CJ2_RETIRE_LEGACY_KEYS" or state.mode ~= "candidate" then return nil, "INVALID_ARGUMENT" end
    if not I.hex(run_id,32) then return nil, "INVALID_IDENTIFIER" end
    if state.bound_run_id and state.bound_run_id ~= run_id then return nil, "IMMUTABLE_MISMATCH" end
    -- Literal inventory keys and private reader receipts, never ctx.selected or
    -- caller-provided projections. Cardinality-only and wrong sole IDs reject.
    for _, entry in ipairs({{"mifolyo:crawl:v2:runs","zset"},
        {"mifolyo:crawl:v2:active_runs","set"},{"mifolyo:crawl:v2:unarchived_runs","set"}}) do
        local fact, code = CJ.Read.snapshot(ctx,entry[1])
        if not fact then return nil, code end
        if not fact.exists or fact.kind ~= entry[2] or fact.complete ~= true or fact.count ~= 1 or
           type(fact.members) ~= "table" or fact.members[run_id] ~= true then return nil, "STATE_INDEX_CORRUPT" end
        local present = 0
        for id, member in next, fact.members, nil do
            if member then
                if id ~= run_id or member ~= true then return nil, "STATE_INDEX_CORRUPT" end
                present = present + 1
            end
        end
        if present ~= 1 then return nil, "STATE_INDEX_CORRUPT" end
        if entry[2] == "zset" and (type(fact.scores) ~= "table" or not I.integer(fact.scores[run_id],P.limits.max_integer) or
           fact.scores[run_id] == 0) then return nil, "STATE_INDEX_CORRUPT" end
    end
    local binding, code = CJ.Wire.run_keys(run_id)
    if not binding then return nil, code end
    for i = 1, #binding.names do
        state.read_only[binding.values[i]] = true
        ctx.keys[binding.names[i]] = binding.values[i]
    end
    ctx.keys.run_keys = binding.values
    state.bound_run_id, ctx.bound_run_id = run_id, run_id
    return true
end
local ROOT="mifolyo:crawl:v2:"
local function grant(ctx,key,write)
    local state=contexts[ctx]
    if write then state.derived_write[key],ctx.allowed[key]=true,true else state.read_only[key]=true end
end
local function fixed(ctx,key,schema)
    local f,code=CJ.Read.snapshot(ctx,key);if not f then return nil,code end
    if not f.exists or f.kind~="hash" or f.schema~=schema or not f.complete then return nil,"INVALID_STATE" end
    local v=CJ.Schemas.project(schema,f.v);if not v then return nil,"INVALID_STATE" end
    v.exists,v.key,v.kind,v.ttl_ms=true,key,"hash",f.ttl_ms;return v
end
-- Pure reconstruction from private receipts. No hidden Redis reads or public
-- Run/Job objects as evidence. Aggregate Run accounting remains Run.load's job.
function C.checked_job(ctx,job_id)
    if not C.preparing(ctx) or not CJ.Job or not I.hex(job_id,64) then return nil,"INVALID_STATE" end
    local run_id=contexts[ctx].run_id or contexts[ctx].bound_run_id
    if not I.hex(run_id,32) then return nil,"INVALID_STATE" end
    local base=ROOT.."run:"..run_id
    local record,code=fixed(ctx,base,"run");if not record then return nil,code end
    local maps,ids={},{}
    local definitions={{"group_limits","request_start_limit"},{"group_rate_scope_ids","rate_scope_id"},{"group_scope_ids","group_scope_id"},{"group_concurrency","concurrency"},{"group_interval_ms","interval_ms"}}
    for _,pair in ipairs(definitions) do
        local f=CJ.Read.snapshot(ctx,base..":"..pair[1]);if not f or f.kind~="hash" or not f.complete or f.count~=record.n.policy_group_count then return nil,"INVALID_STATE" end
        maps[pair[1]]=f.v
    end
    for id,value in next,maps.group_limits,nil do if value~=false then ids[#ids+1]=id end end
    table.sort(ids);local bytes={}
    for i,id in ipairs(ids) do
        local v={group_id=id};for _,pair in ipairs(definitions) do v[pair[2]]=maps[pair[1]][id] end
        local g=CJ.Schemas.project("policy_group",v);if not g then return nil,"INVALID_STATE" end
        bytes[i]=CJ.Schemas.encode(g)
    end
    local groups=CJ.Schemas.groups(bytes)
    if not groups or groups.count~=record.n.policy_group_count or groups.digest~=record.v.policy_group_map_sha256 then return nil,"IMMUTABLE_MISMATCH" end
    local run={run_id=run_id,v=record.v,n=record.n,groups=groups}
    local view={ctx=ctx,job={key=base..":job:"..job_id},active_leases={key=active_leases_key}}
    for _,name in ipairs({"jobs","job_order","ready","ready_at","leased","leased_at","delayed","completed","dead","cancelled","commit_backpressure"}) do view[name]={key=base..":"..name} end
    local job,err=CJ.Job.check(view,run,job_id);if not job then return nil,err end
    return {run=run,job=job}
end
local function expose_scopes(ctx,v,write)
    local scopes={}
    for _,kind in ipairs({"global","group","origin"}) do
        local scope,code=CJ.Wire.rate_keys(v[kind.."_scope_id"]);if not scope then return nil,code end
        scopes[kind]=scope
    end
    for _,kind in ipairs({"global","group","origin"}) do
        contexts[ctx].authorized_scopes[v[kind.."_scope_id"]]=true
        for _,key in ipairs(scopes[kind].ordered) do grant(ctx,key,write) end
    end
    return scopes
end
function C.bind_request(ctx)
    if not C.preparing(ctx) or not CJ.Request then return nil,"INVALID_STATE" end
    local state=contexts[ctx]
    if not state.pending_request then
        if state.request_bound then return true end
        return nil,"INVALID_ARGUMENT"
    end
    local id=state.reservation_id
    local record,code=CJ.Request.check_receipt({ctx=ctx},id);if not record then return nil,code end
    local v,a=record.v,ctx.request.v
    for _,pair in ipairs({{"run_id","run_id"},{"job_id","job_id"},{"owner_id","owner_id"},{"lease_token","lease_token"},{"fence","lease_fence"}}) do
        if a[pair[1]]~=v[pair[2]] then return nil,"IMMUTABLE_MISMATCH" end
    end
    local scopes,at={},1
    for _,kind in ipairs({"global","group","origin"}) do
        local scope,err=CJ.Wire.rate_keys(v[kind.."_scope_id"]);if not scope then return nil,err end
        for _,key in ipairs(scope.ordered) do if state.pending_request[at]~=key then return nil,"INVALID_ARGUMENT" end;at=at+1 end
        scopes[kind]=scope
    end
    for kind,scope in next,scopes,nil do
        for _,key in ipairs(scope.ordered) do state.wire_allowed[key],ctx.allowed[key]=true,true end
        ctx.keys[kind.."_rate"],ctx.keys[kind.."_rate_active"]=scope.scope,scope.active
        ctx.keys[kind.."_rate_pending"],ctx.keys[kind.."_rate_started"]=scope.pending,scope.started
        state.authorized_scopes[v[kind.."_scope_id"]]=true
    end
    ctx.keys.scopes,ctx.keys_pending=scopes,false
    state.pending_request,state.request_bound=nil,true
    return true
end
function C.bind_job(ctx,job_id)
    if not C.preparing(ctx) then return nil,"INVALID_STATE" end
    local state=contexts[ctx];local op=state.operation
    local choices=({CJ2_PROMOTE_DUE={"delayed"},CJ2_RECOVER_EXPIRED={"leased"},CJ2_CANCEL_BATCH={"ready","delayed"},CJ2_PURGE_RUN_BATCH={"job_order"}})[op]
    if not choices or not I.hex(job_id,64) or not I.hex(state.run_id,32) then return nil,"INVALID_ARGUMENT" end
    local base=ROOT.."run:"..state.run_id;local found=false
    for _,name in ipairs(choices) do
        local f=CJ.Read.snapshot(ctx,base..":"..name)
        if not f or not f.members then return nil,"INVALID_STATE" end
        if f.members[job_id]==true then
            local score=f.scores and f.scores[job_id]
            if op=="CJ2_PROMOTE_DUE" or op=="CJ2_RECOVER_EXPIRED" then
                if not I.integer(score,P.limits.max_integer) or score>ctx.now_ms then return nil,"INVALID_STATE" end
            elseif op=="CJ2_PURGE_RUN_BATCH" and score~=0 then return nil,"STATE_INDEX_CORRUPT" end
            found=true
        end
    end
    if not found then return nil,"INVALID_STATE" end
    if not state.jobs[job_id] then
        if state.job_count>=100 then return nil,"LIMIT_EXCEEDED" end
        state.jobs[job_id],state.job_count=true,state.job_count+1
    end
    local key=base..":job:"..job_id;grant(ctx,key,true)
    ctx.keys.jobs_by_id[job_id]=key
    return {run_id=state.run_id,job_id=job_id,key=key}
end
local function matching_live_lease(state,job)
    local l,v=state.lease,job.v
    return v.state=="leased" and v.run_id==l.run_id and v.job_id==l.job_id and v.lease_owner==l.owner_id and
        v.lease_token==l.lease_token and v.lease_fence==l.fence and job.n.lease_expires_at_ms>state.now_ms
end
function C.bind_held_request(ctx,job_id)
    if not C.preparing(ctx) then return nil,"INVALID_STATE" end
    local state=contexts[ctx]
    if state.operation~="CJ2_RENEW_LEASE" and state.operation~="CJ2_RECOVER_EXPIRED" and state.operation~="CJ2_RELEASE_BEFORE_IO" then return nil,"INVALID_ARGUMENT" end
    if not I.hex(job_id,64) or not I.hex(state.run_id,32) then return nil,"INVALID_IDENTIFIER" end
    local key=ROOT.."run:"..state.run_id..":job:"..job_id
    local job,code=fixed(ctx,key,"job");if not job then return nil,code end
    if job.v.run_id~=state.run_id or job.v.job_id~=job_id then return nil,"IMMUTABLE_MISMATCH" end
    if state.operation=="CJ2_RECOVER_EXPIRED" then
        if not state.jobs[job_id] then return nil,"INVALID_ARGUMENT" end
    elseif state.operation=="CJ2_RENEW_LEASE" then
        -- A stale caller must inspect the CURRENT job/request/scopes before the
        -- worker may count its rejection. This is a read grant, not renewal or
        -- allocation authority, and cannot select a peer job or public pointer.
        if job_id~=state.lease.job_id or job.v.state~="leased" then return nil,"IMMUTABLE_MISMATCH" end
        local checked,err=C.checked_job(ctx,job_id);if not checked then return nil,err end
        job=checked.job
    else
        if not matching_live_lease(state,job) then return nil,"IMMUTABLE_MISMATCH" end
        if state.operation=="CJ2_RELEASE_BEFORE_IO" and (job.v.lease_delivery_started~="0" or job.n.request_starts~=job.n.lease_request_starts_baseline) then return nil,"INVALID_STATE" end
    end
    if job.v.active_reservation_id=="" then return {exists=false} end
    local id=job.v.active_reservation_id
    if not I.digest(id) then return nil,"RESERVATION_CORRUPT" end
    local reservation=ROOT.."reservation:"..id
    -- The stored pointer/lease grants a read only. bind_held_scopes authenticates
    -- the reservation/policy and separately tests the original caller for writes.
    grant(ctx,reservation,false);state.held[id]={job_id=job_id,owner_id=job.v.lease_owner,lease_token=job.v.lease_token,fence=job.v.lease_fence}
    return {exists=true,reservation_id=id,key=reservation}
end
function C.bind_held_scopes(ctx,id)
    if not C.preparing(ctx) or not CJ.Request or not I.digest(id) then return nil,"INVALID_STATE" end
    local state=contexts[ctx]
    if not state.held[id] then return nil,"INVALID_ARGUMENT" end
    local record,code=CJ.Request.check_receipt({ctx=ctx},id);if not record then return nil,code end
    local held=state.held[id]
    local job,err=fixed(ctx,ROOT.."run:"..state.run_id..":job:"..held.job_id,"job");if not job then return nil,err end
    local v=record.v
    if not record.live_state or job.v.state~="leased" or v.expires_at_ms~=job.v.lease_expires_at_ms or
       v.run_id~=state.run_id or v.job_id~=job.v.job_id or job.v.active_reservation_id~=id or v.owner_id~=held.owner_id or
       v.lease_token~=held.lease_token or v.lease_fence~=held.fence or v.owner_id~=job.v.lease_owner or
       v.lease_token~=job.v.lease_token or v.lease_fence~=job.v.lease_fence then return nil,"RESERVATION_CORRUPT" end
    if state.operation=="CJ2_RELEASE_BEFORE_IO" and (v.state~="pending" or record.logical_expired or job.v.lease_delivery_started~="0") then return nil,"RESERVATION_CORRUPT" end
    -- RENEW rejection inspection follows the privately stored lease, even after
    -- logical expiry or with a mismatched caller. Only the original matching
    -- LIVE caller can promote these derived keys to writable. Public ctx/record
    -- edits (including a moved clock) cannot upgrade a read-only binding.
    local write=state.operation~="CJ2_RENEW_LEASE" or matching_live_lease(state,job)
    local scopes,why=expose_scopes(ctx,v,write);if not scopes then return nil,why end
    grant(ctx,ROOT.."reservation:"..id,write)
    return scopes
end
function C.bind_rate(ctx,id)
    if not C.preparing(ctx) or contexts[ctx].operation~="CJ2_MAINTAIN_RATE_SCOPES" or not I.digest(id) then return nil,"INVALID_ARGUMENT" end
    local state=contexts[ctx];local f=CJ.Read.snapshot(ctx,ROOT.."rate_scopes")
    if not f or not f.members or f.members[id]~=true then return nil,"INVALID_STATE" end
    if not state.rates[id] then if state.rate_count>=100 then return nil,"LIMIT_EXCEEDED" end;state.rates[id],state.rate_count=true,state.rate_count+1 end
    local keys,code=CJ.Wire.rate_keys(id);if not keys then return nil,code end
    for _,key in ipairs(keys.ordered) do grant(ctx,key,false) end
    return keys
end
function C.bind_scope_reservations(ctx,id)
    if not C.preparing(ctx) then return nil,"INVALID_STATE" end
    local state=contexts[ctx]
    if not I.digest(id) or (not state.rates[id] and not state.authorized_scopes[id]) then return nil,"INVALID_ARGUMENT" end
    local keys,code=CJ.Wire.rate_keys(id);if not keys then return nil,code end
    local ids,seen,indexes={},{},{}
    for _,kind in ipairs({"active","pending","started"}) do
        local f=CJ.Read.snapshot(ctx,keys[kind]);if not f or not f.complete or not I.integer(f.count,32) or not f.members then return nil,"INVALID_STATE" end
        if not ((f.kind=="none" and not f.exists and f.count==0) or (f.kind=="zset" and f.exists and f.count>0)) then return nil,"RATE_STATE_CORRUPT" end
        indexes[kind]=f
        for q,present in next,f.members,nil do if present then
            if not I.digest(q) then return nil,"INVALID_IDENTIFIER" end
            if not seen[q] then seen[q]=true;ids[#ids+1]=q end
        end end
    end
    if #ids>32 or (id==I.global_scope() and #ids>2) then return nil,"RATE_STATE_CORRUPT" end
    local function selected(f,q)
        if f.members[q]~=true then return false end
        local score=f.scores and f.scores[q]
        if not I.integer(score,P.limits.max_integer) or score==0 or not f.score_text or I.redis_score(f.score_text[q])~=score then return nil end
        return score
    end
    for _,q in ipairs(ids) do
        local a,p,s=selected(indexes.active,q),selected(indexes.pending,q),selected(indexes.started,q)
        if a==nil or p==nil or s==nil or a==false or (p~=false)==(s~=false) or
           (p~=false and p~=a) or (s~=false and s~=a) then return nil,"RATE_STATE_CORRUPT" end
    end
    table.sort(ids)
    for _,q in ipairs(ids) do grant(ctx,ROOT.."reservation:"..q,false) end
    if state.authorized_scopes[id] then
        -- Worker evidence is selected here in bounded phases: authenticated
        -- scope membership -> full reservation -> exact peer policy witnesses.
        -- None of these reads grants peer mutations or rebinds the current run.
        for _,q in ipairs(ids) do
            local key=ROOT.."reservation:"..q
            local record,err=CJ.Read.fixed_hash(ctx,key,"reservation");if not record then return nil,err end
            if not record.exists or record.v.reservation_id~=q or
               (record.v.global_scope_id~=id and record.v.group_scope_id~=id and record.v.origin_scope_id~=id) or
               record.n.expires_at_ms~=indexes.active.scores[q] or
               (record.v.state=="pending")~=(indexes.pending.members[q]==true) or
               (record.v.state=="started")~=(indexes.started.members[q]==true) then return nil,"RESERVATION_CORRUPT" end
            local ttl,why=CJ.Read.ttl(ctx,key);if not ttl then return nil,why end
            local base=ROOT.."run:"..record.v.run_id;local job=base..":job:"..record.v.job_id
            grant(ctx,base,false);grant(ctx,job,false)
            for _,name in ipairs({"group_limits","group_rate_scope_ids","group_scope_ids","group_concurrency","group_interval_ms"}) do grant(ctx,base..":"..name,false) end
            local run,re=CJ.Read.fixed_hash(ctx,base,"run");if not run then return nil,re end
            local jf,je=CJ.Read.fixed_hash(ctx,job,"job");if not jf then return nil,je end
            if not run.exists or not jf.exists then return nil,"RESERVATION_CORRUPT" end
            for _,name in ipairs({"group_limits","group_rate_scope_ids","group_scope_ids","group_concurrency","group_interval_ms"}) do
                local f,e=CJ.Read.dynamic_hash(ctx,base..":"..name,64,128,64);if not f then return nil,e end
            end
            if not CJ.Request then return nil,"INVALID_STATE" end
            local checked,e=CJ.Request.check_receipt({ctx=ctx},q);if not checked then return nil,e end
            state.peer_reservations[q]=true
        end
    end
    return ids
end
function C.bind_stage(ctx,job_id)
    if not C.preparing(ctx) then return nil,"INVALID_STATE" end
    local state=contexts[ctx];local op=state.operation
    if not ({CJ2_RENEW_LEASE=true,CJ2_RETRY=true,CJ2_DEAD=true,CJ2_CANCEL_JOB=true,CJ2_COMPLETE_NO_OUTPUT=true,CJ2_RELEASE_BEFORE_IO=true,CJ2_RECOVER_EXPIRED=true})[op] then return nil,"INVALID_ARGUMENT" end
    if not I.hex(job_id,64) or not I.hex(state.run_id,32) then return nil,"INVALID_IDENTIFIER" end
    local job,code=fixed(ctx,ROOT.."run:"..state.run_id..":job:"..job_id,"job");if not job then return nil,code end
    local id=job.v.active_stage_commit_id
    if id=="" and job.v.last_transition_status=="STAGE_ABORTED" then id=job.v.last_stage_commit_id end
    if id=="" then return {exists=false} end
    local slots,err=CJ.Read.slots(ctx);if not slots then return nil,err end
    local slot=slots.records[id]
    if not slot or slot.run_id~=state.run_id or slot.job_id~=job_id or slot.fence~=job.n.lease_fence then return nil,"STAGE_INVALID" end
    local keys,why=CJ.Wire.stage_keys(id);if not keys then return nil,why end
    for _,key in ipairs(keys.ordered) do grant(ctx,key,op=="CJ2_RECOVER_EXPIRED" and key==keys.meta) end
    return {exists=true,commit_id=id,keys=keys}
end
function C.bind_stage_owner(ctx)
    if not C.preparing(ctx) or contexts[ctx].operation~="CJ2_CLEAN_STAGE" then return nil,"INVALID_ARGUMENT" end
    local id=ctx.request.v.expected_commit_id;local keys,code=CJ.Wire.stage_keys(id);if not keys then return nil,code end
    local slots,err=CJ.Read.slots(ctx);if not slots then return nil,err end
    local slot=slots.records[id];local run_id,job_id
    if slot then run_id,job_id=slot.run_id,slot.job_id
    else
        local raw=CJ.Read.snapshot(ctx,keys.meta)
        if not raw then return nil,"INVALID_STATE" end
        if not raw.exists then return {exists=false} end
        local meta=CJ.Schemas.project("stage_meta",raw.v)
        if not meta or meta.v.commit_id~=id then return nil,"STAGE_INVALID" end
        run_id,job_id=meta.v.run_id,meta.v.job_id
    end
    local run,why=CJ.Wire.run_keys(run_id);if not run then return nil,why end
    for i,key in ipairs(run.values) do grant(ctx,key,false);ctx.keys[run.names[i]]=key end
    for _,name in ipairs({"runs","active_runs","unarchived_runs","active_leases"}) do grant(ctx,ROOT..name,false);ctx.keys[name]=ROOT..name end
    local job=ROOT.."run:"..run_id..":job:"..job_id;grant(ctx,job,false)
    contexts[ctx].bound_run_id,ctx.bound_run_id=run_id,run_id;ctx.keys.run_keys,ctx.keys.job=run.values,job
    return {exists=true,run_id=run_id,job_id=job_id,job_key=job}
end
function C.bind_commit(ctx,stage)
    if not C.preparing(ctx) or contexts[ctx].operation~="CJ2_COMMIT" or not CJ.Stage or not CJ.StageOutput then return nil,"INVALID_ARGUMENT" end
    local owner,code=CJ.Stage.slot_proof(stage,ctx);if not owner then return nil,code end
    local a=ctx.request.v
    if owner.kind~="owned" or owner.run_id~=a.run_id or owner.job_id~=a.job_id or owner.fence~=a.fence or owner.commit_id~=a.commit_id then return nil,"STAGE_INVALID" end
    local keys,err=CJ.Wire.stage_keys(owner.commit_id);if not keys then return nil,err end
    local meta=CJ.Read.snapshot(ctx,keys.meta);local page=CJ.Read.snapshot(ctx,keys.page)
    if not meta or not page or not meta.complete or not page.complete or meta.kind~="hash" or page.kind~="hash" then return nil,"INVALID_STATE" end
    local m=CJ.Schemas.project("stage_meta",meta.v)
    local p=CJ.Schemas.project("final_page",page.v)
    if not m or not p or m.v.sealed~="1" or m.v.commit_id~=a.commit_id or p.v.publication_id~=m.v.publication_id then return nil,"STAGE_UNSEALED" end
    local output,why=CJ.StageOutput.keys(m.v.publication_id,p.v.normalized_url);if not output then return nil,why end
    local targets={page=output.page,outlinks=output.outlinks,manifest=output.manifest,page_url=p.v.normalized_url,images={},backlinks={},discovery_jobs={},aliases={}}
    local pending={output.page,output.outlinks,output.manifest}
    for i=1,m.n.expected_images do
        local f=CJ.Read.snapshot(ctx,keys.images[i]);local image=f and f.complete and CJ.Schemas.project("final_image",f.v)
        if not image or image.v.publication_id~=m.v.publication_id or image.v.normalized_page_url~=p.v.normalized_url then return nil,"STAGE_INVALID" end
        local derived=CJ.StageOutput.keys(m.v.publication_id,p.v.normalized_url,image.v.normalized_source_url)
        if not derived then return nil,"STAGE_INVALID" end
        targets.images[i],pending[#pending+1]=derived.image,derived.image
    end
    local out=CJ.Read.snapshot(ctx,keys.outlinks);local discoveries=CJ.Read.snapshot(ctx,keys.discoveries)
    if not out or not discoveries or not out.complete or not discoveries.complete or not out.members or not discoveries.members or
       out.count~=m.n.expected_outlinks or discoveries.count~=m.n.expected_discoveries then return nil,"STAGE_INVALID" end
    for url,present in next,out.members,nil do if present then
        if not CJ.URL.check_canonical(url,1) then return nil,"STAGE_INVALID" end
        local key="backlinks:"..url;targets.backlinks[#targets.backlinks+1],pending[#pending+1]=key,key
    end end
    table.sort(targets.backlinks)
    for id,present in next,discoveries.members,nil do if present then
        if not I.hex(id,64) then return nil,"STAGE_INVALID" end
        local key=ROOT.."run:"..owner.run_id..":job:"..id
        targets.discovery_jobs[id],pending[#pending+1]=key,key
    end end
    local aliases=CJ.Read.snapshot(ctx,keys.aliases)
    if not aliases or not aliases.complete then return nil,"INVALID_STATE" end
    for field,value in next,aliases.v,nil do if value~=false then
        local id=string.match(field,"^([0-9a-f]+):canonical_url$")
        if id then targets.aliases[id]={canonical_url=value,depth=aliases.v[id..":depth"]} end
    end end
    for _,key in ipairs(pending) do grant(ctx,key,true) end
    contexts[ctx].commit_targets=targets
    ctx.keys.output=targets
    return targets
end
function C.call(ctx, ...)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    local argv = {...}
    local argc = I.dense(argv,258)
    if not argc or argc < 2 then return nil, "INVALID_ARGUMENT" end
    for i = 1, argc do if type(argv[i]) ~= "string" then return nil, "INVALID_ARGUMENT" end end
    local command, valid = argv[1], false
    if command == "INFO" then valid = argc == 2 and (argv[2] == "SERVER" or argv[2] == "MEMORY")
    elseif command == "TYPE" or command == "HLEN" or command == "HKEYS" or command == "STRLEN" or command == "GET" or
       command == "SCARD" or command == "ZCARD" or command == "LLEN" or command == "SMEMBERS" or command == "PTTL" then valid = argc == 2
    elseif command == "HSTRLEN" or command == "SISMEMBER" or command == "ZSCORE" then valid = argc == 3
    elseif command == "HMGET" then valid = argc >= 3
    elseif command == "LRANGE" then valid = argc == 4
    elseif command == "ZRANGE" then valid = argc == 5 and argv[5] == "WITHSCORES"
    elseif command == "ZRANGEBYLEX" then valid = argc == 7 and argv[5] == "LIMIT" and argv[6] == "0"
    elseif command == "ZRANGEBYSCORE" then valid = argc == 8 and argv[5] == "WITHSCORES" and argv[6] == "LIMIT" and argv[7] == "0" end
    if not valid then return nil, "INVALID_ARGUMENT" end
    if command ~= "INFO" and not C.can_read(ctx,argv[2]) then return nil, "INVALID_ARGUMENT" end
    -- This read gateway cannot be used as a second TIME or a ledger bypass.
    local ok, result = pcall(redis.call, unpack(argv,1,argc))
    if not ok or result == nil or (type(result) == "table" and result.err ~= nil) then return nil, "INVALID_STATE" end
    return result
end
function C.open(spec, keys, args)
    -- The one clock read is performed even when input subsequently rejects.
    local ok, clock = pcall(redis.call, "TIME")
    if not ok then return nil, "INVALID_NUMBER" end
    local now = P.time_ms(clock)
    if not now or now == 0 then return nil, "INVALID_NUMBER" end
    local text = P.format_decimal(now)
    if not text then return nil, "INVALID_NUMBER" end
    registration_open = false
    CJ.Schemas.finish_registration()
    local decoded, code = CJ.Wire.decode(spec,keys,args)
    if not decoded then return nil, code end
    local ctx = {operation=spec.operation, phase="prepare", now_ms=now, now_text=text,
        request=decoded.request, keys=decoded.keys, allowed=decoded.allowed, selected={}}
    local wire_allowed = {}
    for key, present in next, decoded.allowed, nil do wire_allowed[key] = present end
    contexts[ctx] = {phase="prepare",operation=spec.operation,mode=decoded.request.gate.mode,wire_allowed=wire_allowed,now_ms=now,
        read_only={},derived_write={},bound_run_id=decoded.bound_run_id,run_id=decoded.request.v.run_id,
        pending_request=decoded.pending_request,reservation_id=decoded.reservation_id,jobs={},job_count=0,held={},rates={},rate_count=0,
        authorized_scopes={},peer_reservations={},lease={run_id=decoded.request.v.run_id,job_id=decoded.request.v.job_id,
            owner_id=decoded.request.v.owner_id,lease_token=decoded.request.v.lease_token,fence=decoded.request.v.fence}}
    if decoded.keys.scopes then
        for _,kind in ipairs({"global","group","origin"}) do
            local id=decoded.request.v[kind.."_scope_id"]
            if I.digest(id) then contexts[ctx].authorized_scopes[id]=true end
        end
    end
    ctx.reservation_id,ctx.keys_pending=decoded.reservation_id,decoded.pending_request~=nil
    ctx.bound_run_id, ctx.global_scope_id = decoded.bound_run_id, decoded.global_scope_id
    if spec.operation == "CJ2_ENQUEUE_BATCH" or spec.operation == "CJ2_AUDIT_RUN_BATCH" then
        ctx.keys.active_leases = active_leases_key -- convenience only, NOT in allowed
    end
    return ctx
end
function C.seal(ctx)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    contexts[ctx].phase, ctx.phase = "sealed", "sealed"
    return true
end
-- Reply validation is intentionally operation-specific; extend alongside each
-- new complete handler, not with a permissive status/tail default.
C.Reply = {}
local reply_validators = {
    CJ2_INSTALL_CANDIDATE_MARKERS = function(ctx,status,tail)
        if (status ~= "CANDIDATE_INSTALLED" and status ~= "EXISTS_IDENTICAL") or #tail ~= 2 or
           not I.digest(tail[1]) or not I.digest(tail[2]) then return nil, "INVALID_ARGUMENT" end
        return true
    end
}
function C.Reply.register(operation, validator)
    if not registration_open then return nil, "INVALID_STATE" end
    if type(operation) ~= "string" or not CJ.Wire.modes[operation] or reply_validators[operation] or
       type(validator) ~= "function" then return nil, "INVALID_ARGUMENT" end
    reply_validators[operation] = validator
    return true
end
function C.Reply.build(ctx, status, tail)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    if type(ctx.now_text) ~= "string" or P.parse_decimal(ctx.now_text) ~= ctx.now_ms or ctx.now_ms == 0 then return nil, "INVALID_STATE" end
    local count = I.dense(tail,7)
    if not count or type(status) ~= "string" or not reply_validators[ctx.operation] then return nil, "INVALID_ARGUMENT" end
    local copied, reply = {}, {status,ctx.now_text}
    for i = 1, count do
        if type(tail[i]) ~= "string" then return nil, "INVALID_ARGUMENT" end
        copied[i], reply[i+2] = tail[i], tail[i]
    end
    local ok, accepted, code = pcall(reply_validators[ctx.operation],ctx,status,copied)
    if not ok then return nil, "INVALID_STATE" end
    if accepted ~= true then return nil, C.error_code(code) end
    return reply
end
return C
