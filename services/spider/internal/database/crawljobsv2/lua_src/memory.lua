local M, C, I = {}, CJ.Context, CJ.Identities
local observations = {}
local function copy(observation)
    return {used=observation.used,maximum=observation.maximum,slots=observation.slots,
        lazyfree_pending_objects=observation.lazyfree_pending_objects}
end
local function lazyfree(observation)
    local info = I.info(observation.raw,{"lazyfree_pending_objects"})
    if not info then return nil, "INVALID_STATE" end
    local value = P.parse_decimal(info.lazyfree_pending_objects)
    if value == nil then return nil, "INVALID_STATE" end
    observation.lazyfree_pending_objects = value
    return true
end
function M.observe(ctx, require_lazyfree)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    if require_lazyfree ~= nil and type(require_lazyfree) ~= "boolean" then return nil, "INVALID_ARGUMENT" end
    if observations[ctx] then
        if require_lazyfree then
            local ok, code = lazyfree(observations[ctx]); if not ok then return nil, code end
        end
        return copy(observations[ctx])
    end
    local slots, code = CJ.Read.slots(ctx)
    if not slots then return nil, code end
    local text = C.call(ctx,"INFO","MEMORY")
    local info = I.info(text,{"used_memory","maxmemory"})
    if not info then return nil, "MEMORY_HEADROOM_LOW" end
    local used, maximum = P.parse_decimal(info.used_memory), P.parse_decimal(info.maxmemory)
    if not used or not maximum or maximum == 0 then return nil, "MEMORY_HEADROOM_LOW" end
    observations[ctx] = {used=used,maximum=maximum,slots=slots.sum,raw=text}
    if require_lazyfree then
        local ok, code = lazyfree(observations[ctx]); if not ok then return nil, code end
    end
    return copy(observations[ctx])
end
-- No public accept(ctx, numeric_G) API. Plan owns the exact descriptor-derived
-- growth and performs admission from this one cached observation.
local ROOT="mifolyo:crawl:v2:"
local function clone(value)
    if type(value)~="table" then return value end
    local out={};for k,v in next,value,nil do out[k]=clone(v) end;return out
end
local function set(text)
    local out={};for word in string.gmatch(text,"%S+") do out[word]=true end;return out
end
local safety_ops=set("CJ2_FINISH_REQUEST CJ2_CANCEL_RESERVATION CJ2_RELEASE_BEFORE_IO CJ2_RETRY CJ2_DEAD CJ2_CANCEL_JOB CJ2_COMPLETE_NO_OUTPUT")
local outcome_ops=set("CJ2_RETRY CJ2_DEAD CJ2_CANCEL_JOB CJ2_COMPLETE_NO_OUTPUT")
local stage_ops=set("CJ2_STAGE_PAGE_FIELDS CJ2_STAGE_PAGE_BLOB CJ2_STAGE_OUTLINKS_BATCH CJ2_STAGE_DISCOVERIES_BATCH CJ2_STAGE_ALIASES_BATCH CJ2_STAGE_IMAGES_BATCH CJ2_STAGE_IMAGE_MANIFEST CJ2_SEAL_STAGE")
local job_fields=set([[state last_reason last_failure_reason retry_count pre_io_recoveries lease_owner lease_token lease_started_at_ms lease_expires_at_ms lease_delivery_started active_reservation_id active_stage_commit_id not_before_ms commit_backpressure_fence commit_backpressure_reason commit_backpressure_started_at_ms commit_backpressure_deadline_ms last_transition_id last_transition_status updated_at_ms completed_at_ms dead_at_ms cancelled_at_ms]])
local run_fields=set([[open_job_count retries_total recovered_leases_total completed_total dead_total cancelled_total pending_request_reservations started_request_reservations last_activity_at_ms last_execution_at_ms last_terminal_transition_at_ms]])
local request_fields=set("state terminal_at_ms")
local rate_fields=set("active_count pending_count started_count updated_at_ms")
local group_maps=set("group_pending group_active_started group_open_jobs")
local reason_maps=set("retry_reason_counts recovery_outcome_counts disposition_reason_counts")
local primary=set("ready ready_at delayed completed dead cancelled")
local removable=set("leased leased_at commit_backpressure")
local function job_policy(ctx,id,request_lease)
    local checked,code=C.checked_job(ctx,id);if not checked then return nil,code end
    local j=checked.job
    if not j.exists or j.v.state~="leased" then return nil,"INVALID_STATE" end
    if request_lease then
        local a=ctx.request.v
        if j.v.run_id~=a.run_id or j.v.job_id~=a.job_id or j.v.lease_owner~=a.owner_id or
           j.v.lease_token~=a.lease_token or j.v.lease_fence~=a.fence then return nil,"IMMUTABLE_MISMATCH" end
    end
    return {run=checked.run,job=j,run_id=j.v.run_id,job_id=id,fence=j.v.lease_fence,
        job_key=ROOT.."run:"..j.v.run_id..":job:"..id,run_key=ROOT.."run:"..j.v.run_id}
end
local function slot_match(ctx,p)
    local slots,code=CJ.Read.slots(ctx);if not slots then return nil,code end
    local s=slots.records[p.commit_id]
    if not s or s.run_id~=p.run_id or s.job_id~=p.job_id or s.fence~=P.parse_decimal(p.fence) or
       s.remaining_bytes~=p.remaining or s.abort_unlinked_keys~=p.abort_unlinked_keys then return nil,"STAGE_INVALID" end
    return true
end
-- Internal to Plan.set_policy: no admission here, no supplied numeric G.
function M.policy(ctx,mode,handle)
    if not C.preparing(ctx) then return nil,"INVALID_STATE" end
    if type(mode)~="string" or not ({recovery=true,safety=true,begin=true,stage=true,abort=true,backpressure=true,slot_end=true,commit=true})[mode] then return nil,"INVALID_ARGUMENT" end
    if mode=="recovery" then
        if ctx.operation~="CJ2_RECOVER_EXPIRED" or handle~=nil then return nil,"INVALID_ARGUMENT" end
        return {mode=mode}
    end
    local a=ctx.request.v
    local p,code=job_policy(ctx,a.job_id,true);if not p then return nil,code end
    p.mode=mode
    if mode=="safety" then
        if not safety_ops[ctx.operation] or handle~=nil then return nil,"INVALID_ARGUMENT" end
        local slots,err=CJ.Read.slots(ctx);if not slots then return nil,err end
        for _,s in next,slots.records,nil do
            if s.run_id==p.run_id and s.job_id==p.job_id and s.fence==P.parse_decimal(p.fence) then return nil,"STAGE_INVALID" end
        end
        if p.job.v.active_stage_commit_id~="" or p.job.n.last_stage_fence>=p.job.n.lease_fence then return nil,"STAGE_INVALID" end
        if ctx.operation=="CJ2_FINISH_REQUEST" or ctx.operation=="CJ2_CANCEL_RESERVATION" or
           (ctx.operation=="CJ2_RELEASE_BEFORE_IO" and p.job.v.active_reservation_id~="") then
            if not CJ.Request then return nil,"INVALID_STATE" end
            local id=ctx.operation=="CJ2_RELEASE_BEFORE_IO" and p.job.v.active_reservation_id or a.reservation_id
            local q,err=CJ.Request.check_live({ctx=ctx},p.run,p.job,id);if not q then return nil,C.error_code(err) end
            if ctx.operation=="CJ2_RELEASE_BEFORE_IO" and (q.v.state~="pending" or p.job.v.lease_delivery_started~="0") then return nil,"RESERVATION_CORRUPT" end
            p.reservation=q
        elseif p.job.v.active_reservation_id~="" then return nil,"RESERVATION_CORRUPT" end
        return p
    end
    if not CJ.Stage then return nil,"INVALID_STATE" end
    if mode=="begin" then
        if ctx.operation~="CJ2_BEGIN_STAGE" or handle~=nil then return nil,"INVALID_ARGUMENT" end
        local l={run_id=a.run_id,job_id=a.job_id,owner_id=a.owner_id,lease_token=a.lease_token,fence=a.fence}
        local begin,err=CJ.Stage.begin_record(ctx,p.run,p.job,l,ctx.request)
        if not begin then return nil,C.error_code(err) end
        p.begin,p.keys,p.commit_id,p.remaining,p.abort_unlinked_keys=begin,begin.keys,a.commit_id,50331648,0
        return p
    end
    local slot,err=CJ.Stage.slot_proof(handle,ctx);if not slot then return nil,err end
    if slot.run_id~=p.run_id or slot.job_id~=p.job_id or slot.fence~=p.fence then return nil,"STAGE_INVALID" end
    local ok;ok,err=slot_match(ctx,slot);if not ok then return nil,err end
    p.commit_id,p.remaining,p.abort_unlinked_keys=slot.commit_id,slot.remaining,slot.abort_unlinked_keys
    p.keys=CJ.Wire.stage_keys(slot.commit_id)
    if mode=="slot_end" then
        if not outcome_ops[ctx.operation] or slot.kind~="aborted" then return nil,"INVALID_ARGUMENT" end
        return p
    end
    if slot.kind~="owned" or a.commit_id~=slot.commit_id then return nil,"STAGE_INVALID" end
    local raw=CJ.Read.snapshot(ctx,p.keys.meta)
    local meta=raw and raw.complete and CJ.Schemas.project("stage_meta",raw.v)
    if not meta then return nil,"STAGE_INVALID" end
    p.meta=meta
    if mode=="stage" then
        if not stage_ops[ctx.operation] then return nil,"INVALID_ARGUMENT" end
    elseif mode=="abort" then
        if ctx.operation~="CJ2_ABORT_STAGE" then return nil,"INVALID_ARGUMENT" end
        p.abort_unlinked_keys=meta.n.key_count
    elseif mode=="backpressure" then
        if ctx.operation~="CJ2_COMMIT" or p.job.n.commit_backpressure_started_at_ms~=0 then return nil,"INVALID_ARGUMENT" end
    elseif mode=="commit" then
        if ctx.operation~="CJ2_COMMIT" or meta.v.sealed~="1" then return nil,"STAGE_UNSEALED" end
        local queue=CJ.Read.snapshot(ctx,"pages_queue")
        if not queue or not I.integer(queue.count,P.limits.max_integer) or
           not ((queue.kind=="none" and not queue.exists and queue.complete) or (queue.kind=="list" and queue.exists)) then return nil,"INVALID_STATE" end
        local targets,why=C.bind_commit(ctx,handle);if not targets then return nil,why end
        p.targets,p.queue_count=clone(targets),queue.count
    else return nil,"INVALID_ARGUMENT" end
    return p
end
function M.recovery_unit(ctx,id)
    if not C.preparing(ctx) or ctx.operation~="CJ2_RECOVER_EXPIRED" then return nil,"INVALID_ARGUMENT" end
    local p,code=job_policy(ctx,id,false);if not p then return nil,code end
    if p.job.n.lease_expires_at_ms>ctx.now_ms then return nil,"INVALID_STATE" end
    p.mode="recovery_unit"
    if p.job.v.active_reservation_id~="" then
        if not CJ.Request then return nil,"INVALID_STATE" end
        local q,err=CJ.Request.check_live({ctx=ctx},p.run,p.job,p.job.v.active_reservation_id)
        if not q or not q.logical_expired then return nil,C.error_code(err) end
        p.reservation=q
    end
    local slots,err=CJ.Read.slots(ctx);if not slots then return nil,err end
    local found
    for commit,s in next,slots.records,nil do
        if s.run_id==p.run_id and s.job_id==p.job_id and s.fence==p.job.n.lease_fence then
            if found then return nil,"STAGE_INVALID" end;found=commit
            p.commit_id,p.remaining,p.abort_unlinked_keys=commit,s.remaining_bytes,s.abort_unlinked_keys
        end
    end
    if found then
        local j=p.job.v;p.keys=CJ.Wire.stage_keys(found)
        local ix=CJ.Read.snapshot(ctx,ROOT.."stage_expiry")
        if not ix or not ix.members then return nil,"INVALID_STATE" end
        if p.abort_unlinked_keys>0 then
            local section=P.section("arguments",{{{"owner_id",j.lease_owner},{"commit_id",found}}},16384)
            local digest=section and P.sha256(P.frame("mifolyo:transition-payload:v2")..section)
            local transition=digest and I.framed("mifolyo:crawl-transition:v2",{"CJ2_ABORT_STAGE",p.run_id,id,p.fence,j.lease_token,"none",digest})
            if p.abort_unlinked_keys<2 or j.active_stage_commit_id~="" or j.last_stage_commit_id~=found or
               j.last_stage_fence~=p.fence or j.last_transition_status~="STAGE_ABORTED" or j.last_transition_id~=transition or
               ix.members[found]~=false then return nil,"STAGE_INVALID" end
            for _,key in ipairs(p.keys.ordered) do local f=CJ.Read.snapshot(ctx,key);if not f or f.exists or not f.complete then return nil,"STAGE_INVALID" end end
        else
            if j.active_stage_commit_id~=found or j.last_stage_commit_id~=found or j.last_stage_fence~=p.fence or ix.members[found]~=true then return nil,"STAGE_INVALID" end
            local at=ix.scores and ix.scores[found]
            if not I.integer(at,P.limits.max_integer) then return nil,"STAGE_INVALID" end
            local raw=CJ.Read.snapshot(ctx,p.keys.meta);if not raw then return nil,"INVALID_STATE" end
            if raw.exists then
                local meta=raw.complete and CJ.Schemas.project("stage_meta",raw.v)
                if not meta or meta.v.run_id~=p.run_id or meta.v.job_id~=id or meta.v.lease_fence~=p.fence or
                   meta.v.owner_id~=j.lease_owner or meta.v.request_starts_baseline~=j.lease_request_starts_baseline or
                   meta.v.request_starts_generation~=j.request_starts or meta.n.expires_at_ms~=at or
                   meta.v.token_digest~=I.framed("mifolyo:lease-token:v2",{p.run_id,id,p.fence,j.lease_token}) or
                   found~=I.framed("mifolyo:crawl-commit:v2",{p.run_id,id,p.fence,j.lease_token,meta.v.publication_id,j.lease_request_starts_baseline,j.request_starts}) then return nil,"STAGE_INVALID" end
                p.meta=meta
            elseif raw.kind~="none" or not raw.complete or ctx.now_ms<at then return nil,"STAGE_INVALID" end
        end
    elseif p.job.v.active_stage_commit_id~="" or p.job.n.last_stage_fence>=p.job.n.lease_fence then return nil,"STAGE_INVALID" end
    return p
end
local function changes(ctx,argv,key,schema,allowed)
    local before=CJ.Read.snapshot(ctx,key);local def=CJ.Schemas.get(schema)
    if argv[1]~="HSET" or not before or not before.complete or not before.exists or not def then return false end
    local bounds={};for i,name in ipairs(def.names) do bounds[name]=def.bounds[i] end
    for i=3,#argv,2 do
        local name,value=argv[i],argv[i+1]
        if type(before.v[name])~="string" or bounds[name]==nil or #value>bounds[name] then return false end
        if before.v[name]~=value and not allowed[name] then return false end
    end
    return true
end
local function member_call(argv,member,commands)
    if not commands[argv[1]] then return false end
    if argv[1]=="ZADD" then for i=4,#argv,2 do if argv[i]~=member then return false end end
    else for i=3,#argv do if argv[i]~=member then return false end end end
    return true
end
-- Closed per-owner footprint. It is deliberately stricter than a key-prefix
-- permission and never allows another job's records or index members to spend
-- this owner's slot. Transition semantics remain the operation planner's job.
function M.footprint(ctx,p,argv)
    local cmd,key=argv[1],argv[2]
    if key==ROOT.."stage_slots" then return nil,"INVALID_ARGUMENT" end -- only Plan inserts/removes slot descriptors
    if p.mode=="stage" or p.mode=="begin" then
        if p.mode=="stage" and ctx.operation=="CJ2_SEAL_STAGE" then
            if key==p.keys.meta and changes(ctx,argv,key,"stage_meta",set("sealed sealed_at_ms")) then return true end
            return nil,"INVALID_ARGUMENT"
        end
        if p.keys.kind[key] then
            if p.mode=="begin" and key~=p.keys.meta and key~=p.keys.keys then return nil,"INVALID_ARGUMENT" end
            if cmd~="HSET" and cmd~="SADD" and cmd~="ZADD" and cmd~="LPUSH" and cmd~="PEXPIREAT" then return nil,"INVALID_ARGUMENT" end
            if p.mode=="stage" and key==p.keys.meta and not changes(ctx,argv,key,"stage_meta",set([[page_fields_written html_written original_html_written outlinks_written discoveries_written aliases_written images_written manifest_written data_bytes key_count page_fields_chunk_digest html_chunk_digest original_html_chunk_digest outlinks_chunk_0_digest outlinks_chunk_1_digest outlinks_chunk_2_digest outlinks_chunk_3_digest discoveries_chunk_0_digest discoveries_chunk_1_digest aliases_chunk_0_digest images_chunk_0_digest manifest_chunk_digest]])) then return nil,"INVALID_ARGUMENT" end
            if cmd=="LPUSH" then for i=3,#argv do if not p.keys.kind[argv[i]] then return nil,"INVALID_ARGUMENT" end end end
            if cmd=="PEXPIREAT" and P.parse_decimal(argv[3])~=(p.begin and p.begin.expires_at_ms or p.meta.n.expires_at_ms) then return nil,"INVALID_ARGUMENT" end
            return true
        end
        if p.mode=="begin" and key==p.run_key and changes(ctx,argv,key,"run",set("last_activity_at_ms")) then return true end
        if p.mode=="begin" and key==p.job_key then
            if changes(ctx,argv,key,"job",set("active_stage_commit_id last_stage_commit_id last_stage_fence updated_at_ms")) then return true end
        elseif p.mode=="begin" and key==ROOT.."stage_expiry" and member_call(argv,p.commit_id,{ZADD=true}) then return true end
        return nil,"INVALID_ARGUMENT"
    end
    if p.mode=="abort" then
        if cmd=="UNLINK" and p.keys.kind[key] then return true end
        if key==ROOT.."stage_expiry" and member_call(argv,p.commit_id,{ZREM=true}) then return true end
        if key==p.job_key and changes(ctx,argv,key,"job",set("active_stage_commit_id last_transition_id last_transition_status last_reason updated_at_ms")) then
            -- A reclaimed delivery may retain its earlier retry reason. ABORT
            -- resets only last_reason to none; failure history stays immutable.
            for i=3,#argv,2 do if argv[i]=="last_reason" and argv[i+1]~="none" then return nil,"INVALID_ARGUMENT" end end
            return true
        end
        if key==p.run_key and changes(ctx,argv,key,"run",set("last_activity_at_ms")) then return true end
        return nil,"INVALID_ARGUMENT"
    end
    if p.mode=="backpressure" then
        if key==p.job_key and changes(ctx,argv,key,"job",set("commit_backpressure_fence commit_backpressure_reason commit_backpressure_started_at_ms commit_backpressure_deadline_ms updated_at_ms")) then return true end
        if key==p.run_key and changes(ctx,argv,key,"run",set("last_activity_at_ms")) then return true end
        if key==p.run_key..":commit_backpressure" and member_call(argv,p.job_id,{ZADD=true}) then return true end
        return nil,"INVALID_ARGUMENT"
    end
    if p.mode=="commit" then
        -- Commit already requires a full owned sealed proof and closed derived
        -- grants. Operations still validate destination absence and discovery
        -- accounting. No unrelated wire authority/control keys are admissible.
        if p.keys.kind[key] then
            if cmd=="RENAME" then
                local dest=argv[3]
                local allowed=(key==p.keys.page and dest==p.targets.page) or (key==p.keys.outlinks and dest==p.targets.outlinks) or
                    (key==p.keys.image_manifest and dest==p.targets.manifest)
                for i,image in ipairs(p.targets.images) do if key==p.keys.images[i] and dest==image then allowed=true end end
                if allowed then return true end
            elseif cmd=="PEXPIREAT" or cmd=="UNLINK" then return true end
        end
        if key=="pages_queue" and cmd=="LPUSH" and #argv==3 and argv[3]==p.targets.page then return true end
        if cmd=="PERSIST" then
            if key==p.targets.page or key==p.targets.outlinks or key==p.targets.manifest then return true end
            for _,image in ipairs(p.targets.images) do if key==image then return true end end
        end
        for _,backlink in ipairs(p.targets.backlinks) do if key==backlink and cmd=="SADD" and #argv==3 and argv[3]==p.targets.page_url then return true end end
        for _,job in next,p.targets.discovery_jobs,nil do if key==job and cmd=="HSET" then return true end end
        if key==p.job_key and changes(ctx,argv,key,"job",set("state last_reason lease_owner lease_token lease_started_at_ms lease_expires_at_ms lease_delivery_started active_reservation_id active_stage_commit_id commit_backpressure_fence commit_backpressure_reason commit_backpressure_started_at_ms commit_backpressure_deadline_ms output_digest publication_id commit_id published_page_key last_transition_id last_transition_status updated_at_ms completed_at_ms")) then return true end
        if key==p.run_key and changes(ctx,argv,key,"run",set("job_count open_job_count completed_total output_commits_total last_activity_at_ms last_execution_at_ms last_terminal_transition_at_ms")) then return true end
        for _,name in ipairs({"jobs","job_order","ready","ready_at"}) do if key==p.run_key..":"..name then
            if cmd~=(name=="jobs" and "SADD" or "ZADD") then return nil,"INVALID_ARGUMENT" end
            local first,step=name=="jobs" and 3 or 4,name=="jobs" and 1 or 2
            for i=first,#argv,step do if not p.targets.discovery_jobs[argv[i]] then return nil,"INVALID_ARGUMENT" end end
            return true
        end end
        for _,name in ipairs({"leased","leased_at","commit_backpressure","completed"}) do
            if key==p.run_key..":"..name and member_call(argv,p.job_id,name=="completed" and {ZADD=true} or {ZREM=true}) then return true end
        end
        if key==p.run_key..":visited_urls" or key==p.run_key..":visited_depth" then
            if cmd~="HSET" then return nil,"INVALID_ARGUMENT" end
            for i=3,#argv,2 do
                local alias=p.targets.aliases[argv[i]]
                if not alias or (key==p.run_key..":visited_urls" and argv[i+1]~=alias.canonical_url) or
                   (key==p.run_key..":visited_depth" and P.parse_decimal(argv[i+1])==nil) then return nil,"INVALID_ARGUMENT" end
            end
            return true
        end
        if key==p.run_key..":group_open_jobs" or key==p.run_key..":disposition_reason_counts" then
            local f=CJ.Read.snapshot(ctx,key);if cmd~="HSET" or not f or not f.complete then return nil,"INVALID_ARGUMENT" end
            for i=3,#argv,2 do if type(f.v[argv[i]])~="string" or P.parse_decimal(argv[i+1])==nil then return nil,"INVALID_ARGUMENT" end end
            return true
        end
        if key==ROOT.."active_leases" and member_call(argv,p.run_id..":"..p.job_id,{ZREM=true}) then return true end
        if key==ROOT.."stage_expiry" and member_call(argv,p.commit_id,{ZADD=true,ZREM=true}) then return true end
        return nil,"INVALID_ARGUMENT"
    end
    if key==p.job_key and changes(ctx,argv,key,"job",job_fields) then return true end
    if key==p.run_key and changes(ctx,argv,key,"run",run_fields) then return true end
    if key==ROOT.."active_leases" and member_call(argv,p.run_id..":"..p.job_id,{ZREM=true}) then return true end
    for name in next,primary,nil do if key==p.run_key..":"..name and member_call(argv,p.job_id,{ZADD=true,ZREM=true}) then return true end end
    for name in next,removable,nil do if key==p.run_key..":"..name and member_call(argv,p.job_id,{ZREM=true}) then return true end end
    for name in next,group_maps,nil do if key==p.run_key..":"..name and cmd=="HSET" then
        local existing=CJ.Read.snapshot(ctx,key)
        if not existing or not existing.exists or not existing.complete or existing.kind~="hash" then return nil,"INVALID_STATE" end
        for i=3,#argv,2 do
            local group=name=="group_open_jobs" and p.job.v.group_id or (p.reservation and p.reservation.v.group_id)
            if argv[i]~=group then return nil,"INVALID_ARGUMENT" end
            if type(existing.v[argv[i]])~="string" then return nil,"INVALID_STATE" end
            if P.parse_decimal(argv[i+1])==nil then return nil,"INVALID_NUMBER" end
        end
        return true
    end end
    for name in next,reason_maps,nil do if key==p.run_key..":"..name and cmd=="HSET" then
        local f=CJ.Read.snapshot(ctx,key);if not f or not f.complete then return nil,"INVALID_STATE" end
        for i=3,#argv,2 do if type(f.v[argv[i]])~="string" or P.parse_decimal(argv[i+1])==nil then return nil,"INVALID_ARGUMENT" end end
        return true
    end end
    if p.mode=="recovery_unit" and p.keys and p.meta and key==p.keys.meta and changes(ctx,argv,key,"stage_meta",set("abandoned")) then
        for i=3,#argv,2 do if argv[i]=="abandoned" and argv[i+1]~="1" then return nil,"INVALID_ARGUMENT" end end
        return true
    end
    if p.reservation then
        local q=p.reservation.v
        if key==ROOT.."reservation:"..q.reservation_id then
            if changes(ctx,argv,key,"reservation",request_fields) or ((cmd=="EXPIRE" and argv[3]=="86400") or (cmd=="PEXPIREAT" and P.parse_decimal(argv[3])==P.safe_add(ctx.now_ms,86400000))) then return true end
        end
        for _,kind in ipairs({"global","group","origin"}) do
            local keys=CJ.Wire.rate_keys(q[kind.."_scope_id"])
            if key==keys.scope and changes(ctx,argv,key,"rate_scope",rate_fields) then return true end
            for _,name in ipairs({"active","pending","started"}) do if key==keys[name] and member_call(argv,q.reservation_id,{ZREM=true}) then return true end end
        end
        if key==ROOT.."rate_scopes" and cmd=="ZADD" then
            for i=4,#argv,2 do if argv[i]~=q.global_scope_id and argv[i]~=q.group_scope_id and argv[i]~=q.origin_scope_id then return nil,"INVALID_ARGUMENT" end end
            return true
        end
    end
    return nil,"INVALID_ARGUMENT"
end
-- Bound the ENTIRE admitted post-abort outcome footprint from actual registered
-- schema widths, retained field names and closed possible destination indexes.
-- No caller supplies this number. It includes all six possible primary/age
-- additions (more than any one valid outcome), so an actual outcome is bounded.
function M.post_abort_bound(ctx,p)
    local logical,keys,elements=0,0,0
    for _,spec in ipairs({{"job",p.job_key,job_fields},{"run",p.run_key,run_fields}}) do
        local d=CJ.Schemas.get(spec[1]);local f=CJ.Read.snapshot(ctx,spec[2])
        if not d or not f or not f.complete then return nil,"INVALID_STATE" end
        for i,name in ipairs(d.names) do if spec[3][name] then
            if type(f.v[name])~="string" then return nil,"INVALID_STATE" end
            logical=logical+d.bounds[i]
        end end
    end
    for name in next,group_maps,nil do
        local key=p.run_key..":"..name;local f=CJ.Read.snapshot(ctx,key)
        if not f or not f.complete or type(f.v[p.job.v.group_id])~="string" then return nil,"INVALID_STATE" end
        logical=logical+16
    end
    for name in next,reason_maps,nil do
        local f=CJ.Read.snapshot(ctx,p.run_key..":"..name);if not f or not f.complete then return nil,"INVALID_STATE" end
        for _,value in next,f.v,nil do if value~=false then if P.parse_decimal(value)==nil then return nil,"COUNTER_CORRUPT" end;logical=logical+16 end end
    end
    for name in next,primary,nil do logical=logical+#(p.run_key..":"..name)+#p.job_id+16;keys=keys+1;elements=elements+1 end
    return 3*logical+1024*keys+256*elements
end
-- Single-job safety/slot-end policies track the SUM of their own projected
-- deltas, including interleaved descriptors. Recovery instead validates shared
-- aggregate deltas and real contributor ownership together in recovery_effects.
function M.unit_delta(ctx,p,argv,pre,budget)
    if p.mode~="recovery_unit" and p.mode~="slot_end" and p.mode~="safety" then return true end
    -- Coalesced recovery fields are checked against the actual participating
    -- jobs' projected effects by recovery_effects, including slotted owners.
    if p.mode=="recovery_unit" then return true end
    if argv[1]~="HSET" then return true end
    local key=argv[2];local directions={}
    if key==p.run_key then
        for name in next,run_fields,nil do
            if string.sub(name,-6)=="_at_ms" then directions[name]="time"
            elseif name=="open_job_count" or name=="pending_request_reservations" or name=="started_request_reservations" then directions[name]=-1
            else directions[name]=1 end
        end
    else
        for name in next,group_maps,nil do if key==p.run_key..":"..name then
            for i=3,#argv,2 do directions[argv[i]]=-1 end
        end end
        for name in next,reason_maps,nil do if key==p.run_key..":"..name then
            for i=3,#argv,2 do directions[argv[i]]=1 end
        end end
        if p.reservation then for _,kind in ipairs({"global","group","origin"}) do
            if key==ROOT.."rate:"..p.reservation.v[kind.."_scope_id"] then
                directions={active_count=-1,pending_count=-1,started_count=-1,updated_at_ms="time"}
            end
        end end
    end
    for i=3,#argv,2 do
        local field,value=argv[i],argv[i+1];local direction=directions[field]
        if direction then
            local old=pre.v and P.parse_decimal(pre.v[field]);local new=P.parse_decimal(value)
            if old==nil or new==nil then return nil,"COUNTER_CORRUPT" end
            if direction=="time" then
                if new~=old and value~=ctx.now_text then return nil,"INVALID_ARGUMENT" end
            else
                local id=key.."\0"..field
                local delta=(budget[id] or 0)+(new-old)
                if (direction==1 and (delta<0 or delta>1)) or (direction==-1 and (delta>0 or delta< -1)) then return nil,"COUNTER_CORRUPT" end
                budget[id]=delta
            end
        end
    end
    return true
end
-- Validate coalesced resource ownership without a caller budget or a caller
-- contributor list. Each contribution comes from an authenticated unit and its
-- actual schema-validated final Job/Request descriptor effects. One physical
-- HSET field may be owned by any real contributor; its G is charged exactly once
-- by Plan's descriptor simulation. Separate fields may have separate owners.
function M.recovery_effects(ctx,policies,calls,projected)
    local expected,owners,locations={},{},{}
    local function identify(key,field) return key.."\0"..field end
    local function contribute(p,key,field,amount)
        local id=identify(key,field)
        expected[id]=(expected[id] or 0)+amount
        if not owners[id] then owners[id]={} end
        owners[id][p]=true;locations[id]={key=key,field=field}
    end
    for _,p in ipairs(policies) do
        local raw=projected[p.job_key]
        local job=raw and raw.exists and CJ.Schemas.project("job",raw.v)
        if not job or job.v.run_id~=p.run_id or job.v.job_id~=p.job_id or job.v.lease_fence~=p.fence or
           job.v.lease_owner~="" or job.v.lease_token~="" or job.v.active_reservation_id~="" or
           job.v.updated_at_ms~=ctx.now_text or not ({ready=true,delayed=true,dead=true,cancelled=true})[job.v.state] then return nil,"INVALID_STATE" end
        contribute(p,p.run_key,"recovered_leases_total",1)
        contribute(p,p.run_key..":recovery_outcome_counts",job.v.state,1)
        if job.v.state=="dead" or job.v.state=="cancelled" then
            contribute(p,p.run_key,"open_job_count",-1)
            contribute(p,p.run_key,job.v.state.."_total",1)
            contribute(p,p.run_key..":group_open_jobs",p.job.v.group_id,-1)
            contribute(p,p.run_key..":disposition_reason_counts",job.v.last_reason,1)
        elseif job.v.state=="delayed" then
            contribute(p,p.run_key,"retries_total",1)
            contribute(p,p.run_key..":retry_reason_counts",job.v.last_failure_reason,1)
        end
        if p.reservation then
            local q=p.reservation.v
            local record=projected[ROOT.."reservation:"..q.reservation_id]
            local post=record and record.exists and CJ.Schemas.project("reservation",record.v)
            if not post or post.v.state~="expired" or post.v.terminal_at_ms~=ctx.now_text then return nil,"RESERVATION_CORRUPT" end
            local field=q.state=="pending" and "pending_request_reservations" or "started_request_reservations"
            local map=q.state=="pending" and "group_pending" or "group_active_started"
            contribute(p,p.run_key,field,-1)
            contribute(p,p.run_key..":"..map,q.group_id,-1)
            for _,kind in ipairs({"global","group","origin"}) do
                local key=ROOT.."rate:"..q[kind.."_scope_id"]
                contribute(p,key,"active_count",-1)
                contribute(p,key,q.state=="pending" and "pending_count" or "started_count",-1)
            end
        end
    end
    local current,original={},{ }
    local function resource(p,key,field)
        if key==p.run_key and run_fields[field] then return string.sub(field,-6)=="_at_ms" and "time" or "counter" end
        for name in next,group_maps,nil do if key==p.run_key..":"..name then return "counter" end end
        for name in next,reason_maps,nil do if key==p.run_key..":"..name then return "counter" end end
        if p.reservation then for _,kind in ipairs({"global","group","origin"}) do
            if key==ROOT.."rate:"..p.reservation.v[kind.."_scope_id"] and rate_fields[field] then return field=="updated_at_ms" and "time" or "counter" end
        end end
    end
    for _,call in ipairs(calls) do
        local p,a=call.effect_policy,call.argv
        if not p or p.mode~="recovery_unit" then return nil,"INVALID_STATE" end
        if a[1]=="HSET" then for i=3,#a,2 do
            local kind=resource(p,a[2],a[i])
            if kind then
                local id=identify(a[2],a[i])
                if current[id]==nil then
                    local fact=CJ.Read.snapshot(ctx,a[2]);local old=fact and fact.v and P.parse_decimal(fact.v[a[i]])
                    if old==nil then return nil,"COUNTER_CORRUPT" end
                    original[id],current[id]=old,old
                end
                local value=P.parse_decimal(a[i+1]);if value==nil then return nil,"COUNTER_CORRUPT" end
                if kind=="time" then
                    if value~=current[id] and a[i+1]~=ctx.now_text then return nil,"INVALID_ARGUMENT" end
                elseif value~=current[id] then
                    local total=expected[id]
                    if not total or not owners[id][p] then return nil,"INVALID_ARGUMENT" end
                    local change,done=value-current[id],value-original[id]
                    if (total>0 and (change<0 or done>total)) or (total<0 and (change>0 or done<total)) then return nil,"COUNTER_CORRUPT" end
                end
                current[id]=value
            end
        end end
    end
    for id,amount in next,expected,nil do
        local loc=locations[id];local before=CJ.Read.snapshot(ctx,loc.key)
        local after=projected[loc.key]
        local old=before and before.v and P.parse_decimal(before.v[loc.field])
        local new=after and after.v and P.parse_decimal(after.v[loc.field])
        if old==nil or new==nil or new-old~=amount then return nil,"COUNTER_CORRUPT" end
    end
    return true
end
return M
