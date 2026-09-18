-- Bounded section 10.6 planners. Assemble after Run, Job, Request, StageOutput,
-- Stage. This module owns no wire/gate/receipt/memory framework and performs no
-- Redis writes. Every descriptor is inert until common assessment + ACL sealing.
-- Recovery passes private contributor units through Run's aggregate planner;
-- core owns per-field cost attribution, reserve admission and final slot release.
local M, C, R, I, S, Run, Job = {}, CJ.Context, CJ.Read, CJ.Identities, CJ.Schemas, CJ.Run, CJ.Job
local ROOT, MAX = "mifolyo:crawl:v2:", P.limits.max_integer
local operations = {
    CJ2_PROMOTE_DUE={"run_id"}, CJ2_RECOVER_EXPIRED={"run_id"}, CJ2_CANCEL_BATCH={"run_id"},
    CJ2_PURGE_RUN_BATCH={"run_id","evidence_sha256","expected_first_job_id_or_empty"},
    CJ2_CLEAN_STAGE={"expected_commit_id","expected_cleanup_due_at_ms"}, CJ2_MAINTAIN_RATE_SCOPES={"rank_offset"}}
local function copy(v)
    if type(v)~="table" then return v end
    local out={}; for k,x in next,v,nil do out[k]=copy(x) end; return out
end
local function sorted(t)
    local out={}; for k in next,t,nil do out[#out+1]=k end; table.sort(out); return out
end
local function decimal(n) return P.format_decimal(n) end
local function timestamp(n) return I.integer(n,MAX) and n>0 end
local function add(plan,argv,coverage) return CJ.Plan.add(plan,argv,coverage or "ordinary") end
function M.register(op)
    if not operations[op] then return nil,"INVALID_ARGUMENT" end
    return CJ.Reply.register(op,function(ctx,status,tail)
        if #tail~=2 then return nil,"INVALID_ARGUMENT" end
        local count=P.parse_decimal(tail[1])
        if count==nil or count>(op=="CJ2_CLEAN_STAGE" and 1 or 100) then return nil,"INVALID_ARGUMENT" end
        if status=="BATCH_MORE" and tail[2]=="1" then return true end
        if status==(op=="CJ2_PURGE_RUN_BATCH" and "PURGED" or "BATCH_DONE") and tail[2]=="0" then return true end
        return nil,"INVALID_ARGUMENT"
    end)
end
function M.open(op,keys,args)
    if not operations[op] then return nil,"INVALID_ARGUMENT" end
    -- Key derivation is deliberately left inside the common clocked decoder.
    local spec,code=CJ.Wire.maintenance_spec(op); if not spec then return nil,code end
    local ctx,err=C.open(spec,keys,args); if not ctx then return nil,err end
    local gate,why=CJ.Gate.check(ctx); if not gate then return nil,why end
    return ctx
end
local function finish(ctx,plan,count,more)
    return Run.finish(ctx,plan,more and "BATCH_MORE" or (ctx.operation=="CJ2_PURGE_RUN_BATCH" and "PURGED" or "BATCH_DONE"),
        {decimal(count),more and "1" or "0"})
end
local function load(ctx,mutable)
    local run,code=Run.load(ctx,ctx.request.v.run_id); if not run then return nil,code end
    if mutable then local ok,err=Run.mutable(ctx,run); if not ok then return nil,err end end
    return run
end
local function due_page(ctx,key,limit,maximum)
    local page,code=R.page(ctx,key,"zset",0,limit,maximum,64); if not page then return nil,code end
    for _,id in ipairs(page.ordered) do
        if not I.hex(id,64) or not timestamp(page.scores[id]) then return nil,"STATE_INDEX_CORRUPT" end
    end
    return page
end
local index_defs={{"jobs","set",10000},{"job_order","zset",10000},{"ready","zset",10000},
    {"ready_at","zset",10000},{"leased","zset",64},{"leased_at","zset",64},{"delayed","zset",10000},
    {"completed","zset",10000},{"dead","zset",10000},{"cancelled","zset",10000},{"commit_backpressure","zset",10}}
-- The core grants only IDs selected by private bounded index receipts. Missing
-- rev4 grants fail CLOSED in R.fixed_hash; ctx.allowed is never extended here.
local function grant_jobs(ctx,ids)
    for _,id in ipairs(ids) do
        local granted,code=C.bind_job(ctx,id); if not granted then return nil,code end
    end
    return true
end
local function jobs(ctx,run,ids,nonleased)
    local ok,code=grant_jobs(ctx,ids); if not ok then return nil,code end
    local view={ctx=ctx}
    for _,def in ipairs(index_defs) do
        view[def[1]],code=R.members(ctx,Run.key(ctx,def[1]),def[2],ids,def[3],64)
        if not view[def[1]] then return nil,code end
    end
    local composites={}; for i,id in ipairs(ids) do composites[i]=run.run_id..":"..id end
    view.active_leases,code=R.members(ctx,ROOT.."active_leases","zset",composites,64,97)
    if not view.active_leases then return nil,code end
    local out,contributions={},{}
    local totals={claims_total=0,request_starts=0,retries_total=0,recovered_leases_total=0,reservation_creations_total=0}
    for _,id in ipairs(ids) do
        view.job,code=R.fixed_hash(ctx,ctx.keys.run..":job:"..id,"job"); if not view.job then return nil,code end
        local job
        -- Only the selected ready/delayed job hashes are read in cancellation.
        -- The shared checker uses exact index probes, never another leased job,
        -- reservation or stage record. No fake absent lease receipts are made.
        job,code=Job.check(view,run,id)
        if not job then return nil,code end
        if not job.exists or job.n.created_at_ms<run.n.created_at_ms or job.n.updated_at_ms>ctx.now_ms or
            job.n.updated_at_ms>run.n.last_activity_at_ms then return nil,"INVALID_STATE" end
        if job.n.last_request_started_at_ms>run.n.last_request_started_at_ms then return nil,"COUNTER_CORRUPT" end
        -- Retention freezes ORIGINAL counters; selected remaining contributions
        -- must still fit them, including on purge continuation where Job.at's
        -- ordinary mutation guard deliberately refuses purge_state=in_progress.
        for _,pair in ipairs({{"claim_count","claims_total"},{"request_starts","request_starts"},
            {"retry_count","retries_total"},{"pre_io_recoveries","recovered_leases_total"},
            {"next_request_ordinal","reservation_creations_total"}}) do
            local amount=job.n[pair[1]]-(pair[1]=="next_request_ordinal" and 1 or 0)
            totals[pair[2]]=totals[pair[2]]+amount
            if totals[pair[2]]>run.n[pair[2]] then return nil,"COUNTER_CORRUPT" end
        end
        if run.v.purge_state=="none" then local at; at,code=Job.at(ctx,run,job); if not at then return nil,code end end
        if nonleased and job.v.state~="ready" and job.v.state~="delayed" then return nil,"STATE_INDEX_CORRUPT" end
        if job.v.state=="ready" or job.v.state=="delayed" or job.v.state=="leased" then
            local group=job.v.group_id
            contributions[group]=(contributions[group] or 0)+1
            if run.maps.group_open_jobs.n[group]==nil or contributions[group]>run.maps.group_open_jobs.n[group] then
                return nil,"COUNTER_CORRUPT"
            end
        end
        out[#out+1]=job
    end
    return out
end
function M.promote(ctx)
    local run,code=load(ctx,true); if not run then return nil,code end
    local plan,err=CJ.Plan.new(ctx); if not plan then return nil,err end
    if run.v.state=="cancelled" or run.v.state=="budget_exhausted" or
        (run.v.state=="active" and ctx.now_ms>=run.n.authorization_expires_at_ms) then return finish(ctx,plan,0,false) end
    if run.v.state~="active" then return nil,"INVALID_STATE" end
    local page; page,code=due_page(ctx,ctx.keys.run_delayed,101,10000); if not page then return nil,code end
    local ids={}
    for _,id in ipairs(page.ordered) do
        if #ids<100 and page.scores[id]<=ctx.now_ms then ids[#ids+1]=id end
    end
    local more=#page.ordered>100 and page.scores[page.ordered[101]]<=ctx.now_ms
    local batch; batch,code=jobs(ctx,run,ids,false); if not batch then return nil,code end
    local post={}
    for i,job in ipairs(batch) do
        if job.v.state~="delayed" or job.n.not_before_ms>ctx.now_ms then return nil,"STATE_INDEX_CORRUPT" end
        local v=copy(job.v); v.state,v.not_before_ms,v.updated_at_ms="ready","0",ctx.now_text
        post[i],code=S.project("job",v); if not post[i] then return nil,code end
    end
    if #batch==0 then return finish(ctx,plan,0,more) end
    local delta; delta,code=Run.plan_delta(ctx,run); if not delta then return nil,code end
    local ok; ok,code=Run.accumulate(delta,{indexes={delayed=-#batch,ready=#batch,ready_at=#batch}}); if not ok then return nil,code end
    ok,code=Run.set(delta,{last_activity_at_ms=ctx.now_text}); if not ok then return nil,code end
    for i,job in ipairs(batch) do
        ok,code=Run.hset(plan,job.key,{state=post[i].v.state,not_before_ms="0",updated_at_ms=ctx.now_text}); if not ok then return nil,code end
        for _,argv in ipairs({{"ZREM",ctx.keys.run_delayed,job.v.job_id},
            {"ZADD",ctx.keys.run_ready,job.v.score_text,job.v.job_id},{"ZADD",ctx.keys.run_ready_at,ctx.now_text,job.v.job_id}}) do
            ok,code=add(plan,argv); if not ok then return nil,code end
        end
    end
    ok,code=Run.flush(ctx,plan,delta); if not ok then return nil,code end
    return finish(ctx,plan,#batch,more)
end
-- Pure effective reason. Stored cancellation wins over subsequent auth expiry.
function M.cancel_reason(ctx,run) return Job.cancellation(ctx,run) end
function M.cancel(ctx)
    local run,code=load(ctx,true); if not run then return nil,code end
    if run.v.state~="cancelled" then return nil,"INVALID_STATE" end
    local ready; ready,code=R.page(ctx,ctx.keys.run_ready,"zset",0,100,10000,64); if not ready then return nil,code end
    local ids={}; for _,id in ipairs(ready.ordered) do if not I.hex(id,64) then return nil,"STATE_INDEX_CORRUPT" end; ids[#ids+1]=id end
    if #ids<100 then
        local delayed; delayed,code=due_page(ctx,ctx.keys.run_delayed,100-#ids,10000); if not delayed then return nil,code end
        for _,id in ipairs(delayed.ordered) do ids[#ids+1]=id end
    end
    local batch; batch,code=jobs(ctx,run,ids,true); if not batch then return nil,code end
    local plan; plan,code=CJ.Plan.new(ctx); if not plan then return nil,code end
    local more=run.indexes.ready.count+run.indexes.delayed.count>#batch
    if #batch==0 then return finish(ctx,plan,0,more) end
    local delta; delta,code=Run.plan_delta(ctx,run); if not delta then return nil,code end
    -- Job owns complete state/age/disposition descriptors; the same delta handle
    -- is passed for EVERY selected job, and is flushed exactly once.
    local proposals={}
    for i,job in ipairs(batch) do
        proposals[i],code=Job.outcome_record(ctx,job,{state="cancelled",last_reason=M.cancel_reason(ctx,run),
            last_transition_id="",last_transition_status=""})
        if not proposals[i] then return nil,code end
    end
    for i,job in ipairs(batch) do
        if job.v.state~="ready" and job.v.state~="delayed" then return nil,"STATE_INDEX_CORRUPT" end
        local ok; ok,code=Job.plan_outcome(ctx,plan,delta,run,job,proposals[i])
        if not ok then return nil,code end
    end
    local ok; ok,code=Run.flush(ctx,plan,delta); if not ok then return nil,code end
    return finish(ctx,plan,#batch,more)
end
local function purge_evidence(ctx,run)
    local a,n,v=ctx.request.v,run.n,run.v
    if not I.digest(a.evidence_sha256) then return nil,"INVALID_ARGUMENT" end
    if v.purge_state=="in_progress" and v.purge_evidence_sha256~=a.evidence_sha256 then return nil,"IMMUTABLE_MISMATCH" end
    if n.last_activity_at_ms>ctx.now_ms or n.purge_started_at_ms>ctx.now_ms then return nil,"INVALID_STATE" end
    if ctx.request.gate.mode=="active" then
        local due=P.safe_add(n.archived_at_ms,604800000)
        if v.state~="archived" or not due or ctx.now_ms<due or v.archive_sha256~=a.evidence_sha256 then return nil,"INVALID_STATE" end
    else
        if v.source_kind~="v1_migration" or v.state~="cancelled" or n.activated_at_ms~=0 or n.last_execution_at_ms~=0 or
            n.request_starts~=0 or n.reservation_creations_total~=0 or n.claims_total~=0 or n.open_job_count~=0 or
            n.completed_total~=0 or n.dead_total~=0 or n.cancelled_total~=n.job_count then return nil,"INVALID_STATE" end
        local first,code=R.fixed_hash(ctx,ctx.keys.first_request_start,"first_request_start"); if not first then return nil,code end
        if first.exists then return nil,"INVALID_STATE" end
    end
    local live,code=Run.live(ctx,run); if not live then return nil,code end
    if not live.drained then return nil,"STATE_INDEX_CORRUPT" end
    -- Other runs may operate during archived purge. Their slots/residue are not
    -- this run's ownership and must neither block continuation nor be deleted.
    if ctx.request.gate.mode=="candidate" and (live.slots.count~=0 or live.stage_expiry.count~=0) then return nil,"STAGE_INVALID" end
    return true
end
function M.purge(ctx)
    local a=ctx.request.v
    if not I.digest(a.evidence_sha256) or (a.expected_first_job_id_or_empty~="" and not I.hex(a.expected_first_job_id_or_empty,64)) then
        return nil,"INVALID_ARGUMENT"
    end
    local raw,code=R.fixed_hash(ctx,ctx.keys.run,"run"); if not raw then return nil,code end
    local plan; plan,code=CJ.Plan.new(ctx); if not plan then return nil,code end
    if not raw.exists then
        -- No Redis tombstone is invented. Evidence remains an exact trusted-tool
        -- artifact boundary, under the unchanged full authority gate/context.
        for _,key in ipairs(ctx.keys.run_keys) do
            local kind; kind,code=R.key_type(ctx,key); if not kind then return nil,code end
            if kind~="none" then return nil,"STATE_INDEX_CORRUPT" end
        end
        local inv; inv,code=Run.inventory(ctx,a.run_id); if not inv then return nil,code end
        for _,name in ipairs({"runs","active_runs","unarchived_runs"}) do
            if inv[name].members[a.run_id] then return nil,"STATE_INDEX_CORRUPT" end
        end
        if a.expected_first_job_id_or_empty~="" then
            local key=ctx.keys.run..":job:"..a.expected_first_job_id_or_empty
            local absent; absent,code=R.absent(ctx,key,"hash"); if not absent then return nil,code end
            if absent.exists then return nil,"STATE_INDEX_CORRUPT" end
        end
        -- The absence of run keys alone is not proof that another ledger still
        -- owns a lease/slot for this run; explicitly check these bounded globals.
        local leases; leases,code=R.all_members(ctx,ctx.keys.active_leases,"zset",64,97); if not leases then return nil,code end
        for _,member in ipairs(leases.ordered) do
            local owner,id=string.match(member,"^([^:]+):([^:]+)$")
            if not I.hex(owner,32) or not I.hex(id,64) or not timestamp(leases.scores[member]) or owner==a.run_id then return nil,"STATE_INDEX_CORRUPT" end
        end
        local slots; slots,code=R.slots(ctx); if not slots then return nil,code end
        for _,slot in next,slots.records,nil do if slot.run_id==a.run_id then return nil,"STAGE_INVALID" end end
        if ctx.request.gate.mode=="candidate" then
            local first; first,code=R.fixed_hash(ctx,ctx.keys.first_request_start,"first_request_start"); if not first then return nil,code end
            if first.exists then return nil,"INVALID_STATE" end
        end
        return finish(ctx,plan,0,false)
    end
    local run; run,code=load(ctx,false); if not run then return nil,code end
    local eligible; eligible,code=purge_evidence(ctx,run); if not eligible then return nil,code end
    local page; page,code=R.lex_page(ctx,ctx.keys.run_job_order,"",100,10000,64); if not page then return nil,code end
    if (page.ordered[1] or "")~=a.expected_first_job_id_or_empty then return nil,"IMMUTABLE_MISMATCH" end
    local batch; batch,code=jobs(ctx,run,page.ordered,false); if not batch then return nil,code end
    local changes={jobs=-#batch,job_order=-#batch}
    local selected={}
    for _,job in ipairs(batch) do
        local v=job.v
        if selected[v.job_id] or v.state=="leased" or v.active_reservation_id~="" or v.active_stage_commit_id~="" or
            v.commit_backpressure_reason~="none" then return nil,"STATE_INDEX_CORRUPT" end
        selected[v.job_id]=true
        changes[v.state]=(changes[v.state] or 0)-1
        if v.state=="ready" then changes.ready_at=(changes.ready_at or 0)-1 end
        if ctx.request.gate.mode=="candidate" and (v.state~="cancelled" or job.n.claim_count~=0 or job.n.next_request_ordinal~=1 or
            job.n.request_starts~=0 or v.last_stage_commit_id~="") then return nil,"INVALID_STATE" end
    end
    local remaining=run.n.job_count-run.n.purged_job_count-#batch
    if remaining<0 or (#batch==0 and remaining~=0) then return nil,"COUNTER_CORRUPT" end
    local ok
    if remaining>0 then
        local delta; delta,code=Run.plan_delta(ctx,run); if not delta then return nil,code end
        ok,code=Run.accumulate(delta,{run={purged_job_count=#batch},indexes=changes}); if not ok then return nil,code end
        if run.v.purge_state=="none" then
            ok,code=Run.set(delta,{purge_state="in_progress",purge_evidence_sha256=a.evidence_sha256,purge_started_at_ms=ctx.now_text})
            if not ok then return nil,code end
        end
        -- Flush validates the reduced live inventory with ORIGINAL frozen counters.
        ok,code=Run.flush(ctx,plan,delta); if not ok then return nil,code end
    else
        -- The final post-state is ABSENCE, not an invalid in_progress record with
        -- purged_job_count==job_count (both Go and Run codecs forbid that shape).
        -- Prove every projected primary/age/inventory empty before any deletion.
        for _,def in ipairs(index_defs) do
            if run.indexes[def[1]].count+(changes[def[1]] or 0)~=0 then return nil,"STATE_INDEX_CORRUPT" end
        end
    end
    for _,job in ipairs(batch) do
        for _,argv in ipairs({{"UNLINK",job.key},{"SREM",ctx.keys.run_jobs,job.v.job_id},{"ZREM",ctx.keys.run_job_order,job.v.job_id},
            {"ZREM",Run.key(ctx,job.v.state),job.v.job_id}}) do
            ok,code=add(plan,argv); if not ok then return nil,code end
        end
        if job.v.state=="ready" then ok,code=add(plan,{"ZREM",ctx.keys.run_ready_at,job.v.job_id}); if not ok then return nil,code end end
    end
    if remaining==0 then
        for _,key in ipairs(ctx.keys.run_keys) do ok,code=add(plan,{"UNLINK",key}); if not ok then return nil,code end end
        for _,argv in ipairs({{"SREM",ctx.keys.active_runs,a.run_id},{"SREM",ctx.keys.unarchived_runs,a.run_id},{"ZREM",ctx.keys.runs,a.run_id}}) do
            ok,code=add(plan,argv); if not ok then return nil,code end
        end
    end
    return finish(ctx,plan,#batch,remaining>0)
end
-- Closed stage-key reads. Bounded inventory is checked by Stage against the
-- exact 73 names; an inventory value NEVER becomes a permission or read target.
local function stage_reads(ctx,commit)
    local selected,code=CJ.Stage.select(ctx,commit); if not selected then return nil,code end
    return selected.keys
end
-- Recovery's distinct stage predicate. Worker Stage.check_owned intentionally
-- rejects expired leases; using it here would strand its terminal reservation.
-- No stage content is published, repaired, renamed, or reinterpreted as current.
local function stage_owner(ctx,run,job,commit,cleanup_due)
    local slots,code=R.slots(ctx); if not slots then return nil,code end
    local slot=slots.records[commit]
    if not slot or slot.run_id~=run.run_id or slot.job_id~=job.v.job_id or slot.fence~=job.n.lease_fence or
        job.v.state~="leased" or job.v.active_reservation_id~="" or job.v.lease_delivery_started~="1" or
        job.v.last_stage_commit_id~=commit or job.v.last_stage_fence~=job.v.lease_fence then return nil,"STAGE_INVALID" end
    if not C.can_read(ctx,ROOT.."stage:"..commit..":meta") then
        local ok; ok,code=C.bind_stage(ctx,job.v.job_id); if not ok then return nil,code end
    end
    local expiry; expiry,code=R.members(ctx,ctx.keys.stage_expiry,"zset",{commit},1280,64); if not expiry then return nil,code end
    local keys; keys,code=CJ.Stage.keys(commit); if not keys then return nil,code end
    if slot.abort_unlinked_keys>0 then
        if cleanup_due or slot.abort_unlinked_keys<2 or expiry.members[commit] or
            job.v.active_stage_commit_id~="" or job.v.last_transition_status~="STAGE_ABORTED" then return nil,"STAGE_INVALID" end
        -- The complete Job codec already authenticates the exact ABORT control
        -- identity against this retained token/fence/commit (not just its status).
        for _,key in ipairs(keys.ordered) do
            local absent; absent,code=R.absent(ctx,key,keys.kind[key]); if not absent then return nil,code end
            if absent.exists then return nil,"STAGE_INVALID" end
        end
        return {commit_id=commit,meta_exists=false,kind="aborted",remaining=slot.remaining_bytes}
    end
    local due=expiry.scores[commit]
    if job.v.active_stage_commit_id~=commit or not expiry.members[commit] or not timestamp(due) or
        due<job.n.lease_expires_at_ms or (cleanup_due and cleanup_due~=due) then return nil,"STAGE_INVALID" end
    local raw; raw,code=R.dynamic_hash(ctx,keys.meta,43,64,64); if not raw then return nil,code end
    if not raw.exists then
        if ctx.now_ms<due then return nil,"STAGE_INVALID" end
        return {commit_id=commit,meta_exists=false,kind="expired_owner",remaining=slot.remaining_bytes}
    end
    local v=raw.v
    if v.run_id~=run.run_id or v.job_id~=job.v.job_id or v.owner_id~=job.v.lease_owner or v.lease_fence~=job.v.lease_fence or
        v.commit_id~=commit or v.token_digest~=CJ.Request.token_digest({run_id=run.run_id,job_id=job.v.job_id,
            owner_id=job.v.lease_owner,lease_token=job.v.lease_token,fence=job.v.lease_fence}) then return nil,"STAGE_INVALID" end
    if v.request_starts_baseline~=job.v.lease_request_starts_baseline or v.request_starts_generation~=job.v.request_starts then
        return nil,"COUNTER_CORRUPT"
    end
    local meta; meta,code=CJ.Stage.validate(v); if not meta then return nil,code end
    local expected=I.framed("mifolyo:crawl-commit:v2",{run.run_id,job.v.job_id,job.v.lease_fence,job.v.lease_token,
        v.publication_id,v.request_starts_baseline,v.request_starts_generation})
    local ttl; ttl,code=R.ttl(ctx,keys.meta); if not ttl then return nil,code end
    if raw.count~=43 or expected~=commit or v.abandoned~="0" or meta.n.expires_at_ms~=due or
        meta.n.created_at_ms<job.n.last_request_started_at_ms or meta.n.created_at_ms>ctx.now_ms or
        meta.n.sealed_at_ms>ctx.now_ms or job.v.last_document_request_fence~=job.v.lease_fence or
        meta.n.expected_aliases>meta.n.request_starts_generation-meta.n.request_starts_baseline or
        ttl.ttl_ms~=due-ctx.now_ms or ttl.ttl_ms<0 then return nil,"STAGE_INVALID" end
    if job.v.commit_backpressure_reason~="none" then
        local cap=P.safe_add(job.n.commit_backpressure_started_at_ms,120000)
        if not cap or job.n.commit_backpressure_started_at_ms<meta.n.created_at_ms or
            job.n.commit_backpressure_deadline_ms~=math.min(cap,due-10000) then return nil,"STAGE_INVALID" end
    end
    return {commit_id=commit,meta_exists=true,kind="active",remaining=slot.remaining_bytes,meta=meta}
end
local function cleanup_owner(ctx,commit,slot,due)
    -- No run/job ID is accepted on this wire. Core must grant this exact stored
    -- owner tuple from the selected private slot before any derived-key read.
    local bound,code=C.bind_stage_owner(ctx); if not bound then return nil,code end
    local base=ROOT.."run:"..slot.run_id
    local record; record,code=R.fixed_hash(ctx,base,"run"); if not record then return nil,code end
    local job; job,code=R.fixed_hash(ctx,base..":job:"..slot.job_id,"job"); if not job then return nil,code end
    if not record.exists or not job.exists or job.v.run_id~=slot.run_id or job.v.job_id~=slot.job_id or
        record.v.contract_sha256~=ctx.request.gate.contract or record.v.purge_state~="none" or record.n.finalized_at_ms~=0 or
        (record.v.state~="active" and record.v.state~="cancelled") or
        job.v.state~="leased" or job.n.lease_fence~=slot.fence or job.n.lease_expires_at_ms>due or
        job.n.created_at_ms<record.n.created_at_ms or job.n.updated_at_ms>record.n.last_activity_at_ms or
        record.n.last_activity_at_ms>ctx.now_ms then return nil,"STAGE_INVALID" end
    -- Pin source ownership using the shared policy/source validators, without
    -- pretending CLEAN has a run_id/lease_identity wire argument.
    local maps,group_ids={},{}
    local defs={{"group_limits","request_start_limit"},{"group_rate_scope_ids","rate_scope_id"},
        {"group_scope_ids","group_scope_id"},{"group_concurrency","concurrency"},{"group_interval_ms","interval_ms"}}
    for _,def in ipairs(defs) do
        local fact; fact,code=R.dynamic_hash(ctx,base..":"..def[1],64,128,64); if not fact then return nil,code end
        if not fact.exists or fact.count~=record.n.policy_group_count then return nil,"COUNTER_CORRUPT" end
        maps[def[1]]=fact
        if def[1]=="group_limits" then group_ids=fact.names end
    end
    local encoded={}
    for i,id in ipairs(group_ids) do
        local v={group_id=id}; for _,def in ipairs(defs) do v[def[2]]=maps[def[1]].v[id] end
        local group; group,code=S.project("policy_group",v); if not group then return nil,code end
        encoded[i],code=S.encode(group); if not encoded[i] then return nil,code end
    end
    local groups; groups,code=S.groups(encoded); if not groups then return nil,code end
    if groups.digest~=record.v.policy_group_map_sha256 then return nil,"IMMUTABLE_MISMATCH" end
    local run={run_id=slot.run_id,v=record.v,n=record.n,groups=groups}
    local fields={}
    for i,name in ipairs({"job_id","canonical_url","score_text","depth","group_id","rate_scope_id","group_scope_id",
        "initial_origin_scope_id","policy_decision_sha256"}) do fields[i]={name,job.v[name]} end
    local source; source,code=Job.source({fields=fields},run); if not source then return nil,code end
    for _,def in ipairs({{base..":leased",slot.job_id,job.n.lease_expires_at_ms,64,64},
        {base..":leased_at",slot.job_id,job.n.lease_started_at_ms,64,64},
        {ROOT.."active_leases",slot.run_id..":"..slot.job_id,job.n.lease_expires_at_ms,64,97}}) do
        local fact; fact,code=R.members(ctx,def[1],"zset",{def[2]},def[4],def[5]); if not fact then return nil,code end
        if not fact.members[def[2]] or fact.scores[def[2]]~=def[3] then return nil,"STATE_INDEX_CORRUPT" end
    end
    return stage_owner(ctx,run,job,commit,due)
end
function M.clean(ctx)
    local a=ctx.request.v
    local due=P.parse_decimal(a.expected_cleanup_due_at_ms)
    if not I.digest(a.expected_commit_id) or not due or due==0 then return nil,"INVALID_ARGUMENT" end
    local page,code=due_page(ctx,ctx.keys.stage_expiry,2,1280); if not page then return nil,code end
    local plan; plan,code=CJ.Plan.new(ctx); if not plan then return nil,code end
    local first=page.ordered[1]
    local any_due=first~=nil and page.scores[first]<=ctx.now_ms
    if first~=a.expected_commit_id or page.scores[first]~=due or ctx.now_ms<due then return finish(ctx,plan,0,any_due) end
    local slots; slots,code=R.slots(ctx); if not slots then return nil,code end
    local explicit; explicit,code=R.hash_fields(ctx,ctx.keys.stage_slots,{first},4,64,126); if not explicit then return nil,code end
    local slot=slots.records[first]
    if slot then
        -- Expired live ownership is NOT Stage.check_owned's worker authority.
        -- No lease/slot release can occur here, even after the common expiry.
        if slot.abort_unlinked_keys~=0 then return nil,"STAGE_INVALID" end
        local owned; owned,code=cleanup_owner(ctx,first,slot,due); if not owned then return nil,code end
        return finish(ctx,plan,0,true)
    end
    local keys; keys,code=stage_reads(ctx,first); if not keys then return nil,code end
    local meta; meta,code=R.snapshot(ctx,keys.meta); if not meta then return nil,code end
    if meta.exists then
        local checked; checked,code=CJ.Stage.validate(meta.v); if not checked then return nil,code end
        if checked.v.abandoned=="0" then
            -- Bind only the validated retained metadata owner. CLEAN has no raw
            -- worker token, and can neither recreate one nor gain publication
            -- authority from this deletion-only proof. Historical abandoned
            -- residue deliberately does not bind a possibly newer job fence.
            local owner; owner,code=C.bind_stage_owner(ctx); if not owner then return nil,code end
            if not owner.exists then return nil,"STAGE_INVALID" end
            local job; job,code=R.fixed_hash(ctx,owner.job_key,"job"); if not job then return nil,code end
            if not job.exists then return nil,"STAGE_INVALID" end
        end
    end
    local residue; residue,code=CJ.Stage.check_cleanup_residue({ctx=ctx},first); if not residue then return nil,code end
    if not residue.deletion_only or residue.cleanup_due_at_ms~=due then return nil,"STAGE_INVALID" end
    for _,key in ipairs(residue.delete_keys) do
        if not keys.kind[key] then return nil,"STAGE_INVALID" end
        local ok; ok,code=add(plan,{"UNLINK",key}); if not ok then return nil,code end
    end
    local ok; ok,code=add(plan,{"ZREM",ctx.keys.stage_expiry,first}); if not ok then return nil,code end
    local second=page.ordered[2]
    return finish(ctx,plan,1,second~=nil and page.scores[second]<=ctx.now_ms)
end
local function scope_reads(ctx,id)
    if not I.digest(id) then return nil,"RATE_STATE_CORRUPT" end
    if ctx.operation=="CJ2_MAINTAIN_RATE_SCOPES" then
        local ok,code=C.bind_rate(ctx,id); if not ok then return nil,code end
    end
    local key=ROOT.."rate:"..id
    local scope,code=R.fixed_hash(ctx,key,"rate_scope"); if not scope then return nil,code end
    if not scope.exists then return nil,"RATE_STATE_CORRUPT" end
    local ttl; ttl,code=R.ttl(ctx,key); if not ttl then return nil,code end
    local ids={}
    for _,name in ipairs({"active","pending","started"}) do
        local index; index,code=R.all_members(ctx,key..":"..name,"zset",32,64)
        if not index then return nil,code end
        ttl,code=R.ttl(ctx,key..":"..name); if not ttl then return nil,code end
        for _,reservation in ipairs(index.ordered) do
            if not I.digest(reservation) then return nil,"RATE_STATE_CORRUPT" end
            ids[reservation]=true
        end
    end
    local bound; bound,code=C.bind_scope_reservations(ctx,id); if not bound then return nil,code end
    for _,reservation in ipairs(sorted(ids)) do
        local record; record,code=R.fixed_hash(ctx,ROOT.."reservation:"..reservation,"reservation"); if not record then return nil,code end
        ttl,code=R.ttl(ctx,ROOT.."reservation:"..reservation); if not ttl then return nil,code end
    end
    return CJ.Request.Rate.check_scope({ctx=ctx,reservation_ids=sorted(ids)},id)
end
function M.rate(ctx)
    local offset=P.parse_decimal(ctx.request.v.rank_offset)
    if offset==nil then return nil,"INVALID_NUMBER" end
    -- Offsets above the bounded inventory are a valid exhausted diagnostic pass.
    local inventory,code=R.cardinality(ctx,ctx.keys.rate_scopes,"zset",100000); if not inventory then return nil,code end
    local plan; plan,code=CJ.Plan.new(ctx); if not plan then return nil,code end
    if offset>=inventory.count then return finish(ctx,plan,0,false) end
    local page; page,code=R.page(ctx,ctx.keys.rate_scopes,"zset",offset,100,100000,64); if not page then return nil,code end
    local ttl; ttl,code=R.ttl(ctx,ctx.keys.rate_scopes); if not ttl then return nil,code end
    for _,id in ipairs(page.ordered) do
        local scope; scope,code=scope_reads(ctx,id); if not scope then return nil,code end
    end
    return finish(ctx,plan,#page.ordered,offset+#page.ordered<inventory.count)
end
-- Server-selected outcome, never a client reason/attempt/deadline. This pure
-- branch selector is separately differential-tested; Job owns the record/index
-- mutation and Run owns the aggregate counters.
function M.recovery_outcome(ctx,run,job)
    local rr,jj=Run.validate(run.v),S.project("job",job.v)
    if not C.preparing(ctx) or not rr or not jj or jj.v.run_id~=run.run_id or jj.v.state~="leased" or
        jj.n.lease_expires_at_ms>ctx.now_ms or rr.n.finalized_at_ms~=0 or
        (rr.v.state~="active" and rr.v.state~="cancelled") then return nil,"INVALID_STATE" end
    local reason=M.cancel_reason(ctx,{v=rr.v,n=rr.n})
    if reason then return {state="cancelled",reason=reason,recovery=true} end
    if jj.n.lease_delivery_started==0 then
        if jj.n.pre_io_recoveries>=3 then return nil,"COUNTER_CORRUPT" end
        if jj.n.pre_io_recoveries==2 then
            return {state="dead",reason="pre_io_recovery_exhausted",failure_reason="pre_io_recovery_exhausted",
                pre_io_recoveries=3,recovery=true}
        end
        return {state="ready",reason="none",pre_io_recoveries=jj.n.pre_io_recoveries+1,recovery=true}
    end
    if jj.n.delivery_attempts==3 then
        return {state="dead",reason="retry_exhausted",failure_reason="lease_expired_after_io",recovery=true}
    end
    if jj.n.delivery_attempts~=1 and jj.n.delivery_attempts~=2 then return nil,"COUNTER_CORRUPT" end
    local due=P.safe_add(ctx.now_ms,jj.n.delivery_attempts==1 and 30000 or 120000)
    if not due then return nil,"INVALID_NUMBER" end
    return {state="delayed",reason="lease_expired_after_io",failure_reason="lease_expired_after_io",
        not_before_ms=decimal(due),retry=true,recovery=true}
end
function M.recover(ctx)
    local run,code=load(ctx,true); if not run then return nil,code end
    local live; live,code=Run.live(ctx,run); if not live then return nil,code end
    local plan; plan,code=CJ.Plan.new(ctx); if not plan then return nil,code end
    if run.n.finalized_at_ms>0 then
        if not live.drained then return nil,"STATE_INDEX_CORRUPT" end
        return finish(ctx,plan,0,false)
    end
    if run.v.state~="active" and run.v.state~="cancelled" then return nil,"INVALID_STATE" end
    local page; page,code=due_page(ctx,ctx.keys.run_leased,100,64); if not page then return nil,code end
    local ids={}; for _,id in ipairs(page.ordered) do if page.scores[id]<=ctx.now_ms then ids[#ids+1]=id end end
    local batch; batch,code=jobs(ctx,run,ids,false); if not batch then return nil,code end
    if #batch==0 then return finish(ctx,plan,0,false) end
    local prepared,seen_reservations={},{}
    for _,job in ipairs(batch) do
        if job.v.state~="leased" or job.n.lease_expires_at_ms>ctx.now_ms then return nil,"STATE_INDEX_CORRUPT" end
        local item={job=job}
        local id=job.v.active_reservation_id
        item.reservation,code=CJ.Request.load_live(ctx,run,job)
        if item.reservation==nil then return nil,code end
        if id~="" then
            if seen_reservations[id] then return nil,"RESERVATION_CORRUPT" end
            seen_reservations[id]=true
            -- Matching but nonexpired capacity is never released. The shared
            -- live checker additionally requires exact lease/reservation expiry.
            if not item.reservation or item.reservation.v.reservation_id~=id or item.reservation.n.expires_at_ms>ctx.now_ms then return nil,"RESERVATION_CORRUPT" end
        elseif item.reservation~=false then return nil,"RESERVATION_CORRUPT" end
        local owner_commit,owner_slot
        for commit,slot in next,live.slots.records,nil do
            if slot.run_id==run.run_id and slot.job_id==job.v.job_id then
                if owner_commit or slot.fence~=job.n.lease_fence then return nil,"STAGE_INVALID" end
                owner_commit,owner_slot=commit,slot
            end
        end
        if owner_commit then
            if id~="" then return nil,"STAGE_INVALID" end
            item.stage,code=stage_owner(ctx,run,job,owner_commit)
            if not item.stage then return nil,code end
            item.commit,item.slot=owner_commit,owner_slot
        elseif job.v.active_stage_commit_id~="" or
            (job.v.last_stage_commit_id~="" and job.v.last_stage_fence==job.v.lease_fence) then return nil,"STAGE_INVALID" end
        item.outcome,code=M.recovery_outcome(ctx,run,job); if not item.outcome then return nil,code end
        local changes={state=item.outcome.state,last_reason=item.outcome.reason,last_transition_id="",last_transition_status=""}
        if item.outcome.failure_reason then changes.last_failure_reason=item.outcome.failure_reason end
        if item.outcome.pre_io_recoveries then changes.pre_io_recoveries=decimal(item.outcome.pre_io_recoveries) end
        if item.outcome.not_before_ms then changes.not_before_ms=item.outcome.not_before_ms end
        if item.outcome.retry then changes.retry_count=decimal(job.n.retry_count+1) end
        item.proposed,code=Job.outcome_record(ctx,job,changes); if not item.proposed then return nil,code end
        prepared[#prepared+1]=item
    end
    -- Shared-memory partition admission is mandatory, not an ordinary-memory
    -- approximation. Private core units authenticate each selected owner.
    local policy; policy,code=CJ.Plan.set_policy(plan,"recovery"); if not policy then return nil,code end
    local delta; delta,code=Run.plan_delta(ctx,run); if not delta then return nil,code end
    local memory={}
    for _,item in ipairs(prepared) do
        local unit; unit,code=CJ.Plan.recovery_unit(plan,item.job.v.job_id); if not unit then return nil,code end
        memory[item.job.v.job_id]=unit
    end
    for _,item in ipairs(prepared) do
        if item.reservation then
            -- All selected jobs/scopes/stages have already validated. Request's
            -- private per-plan scope projection composes exact member releases;
            -- two jobs can never overwrite counts computed from the same old
            -- scope. It accumulates REQUEST-group, not SOURCE-group, counters.
            local effects; effects,code=CJ.Request.plan_terminal(ctx,plan,delta,run,item.job,"expired",memory[item.job.v.job_id])
            if not effects then return nil,code end
        end
        local ok; ok,code=Job.plan_outcome(ctx,plan,delta,run,item.job,item.proposed,memory[item.job.v.job_id]); if not ok then return nil,code end
    end
    -- Every Run/map/reason field is emitted once after the whole projected
    -- ledger validates. Its contributor comes from the actual Job/Request delta,
    -- not a batch default or an arbitrary stage owner's terminal reservation.
    local ok; ok,code=Run.flush(ctx,plan,delta); if not ok then return nil,code end
    for _,item in ipairs(prepared) do
        if item.stage then
            if item.stage.meta_exists then
                ok,code=Run.hset(plan,ROOT.."stage:"..item.commit..":meta",{abandoned="1"},memory[item.job.v.job_id]); if not ok then return nil,code end
            end
            -- Plan owns the final slot HDEL, after ALL covered descriptors.
        end
    end
    -- Global capacity bounds this selection to 64 (<100), so all due candidates
    -- fit in one call; nonexpired leases are deliberately left for a later pass.
    return finish(ctx,plan,#prepared,false)
end
return M
