-- Stage validation/preparation API. Foundation uses Core rev3 receipts; the
-- operation planners below require the Core rev4 wire/Plan/derived-key APIs.
-- Load after Identities/Schemas/Context/Read/Run/Job/URL/StageOutput, before
-- Context.open. No Redis calls during registration. check_*/validate_* remain
-- pure/private-receipt-only. select/prepare explicitly read and build inert
-- descriptors; execution occurs only in the eleven operation fragments.
--
-- Stage.keys(commit) -> closed 73-key plan {ordered,kind,meta,keys,page,...,
-- images[1..64]}; indices map to image:0..63. Only validated IDs derive keys.
-- Stage.validate(values) -> complete 43-field schema projection.
-- Stage.check_owned({ctx=ctx}, runLedger, jobProjection, lease, commit) -> stage.
--   lease is {run_id,job_id,owner_id,lease_token,fence}, ALL strings. runLedger
--   is Run.load's {run_id,v,groups}; jobProjection is Job.check's full projection.
--   Caller MUST first select private R receipts for run, five immutable group
--   maps, job + ALL Job.check indices, stage_slots (including this explicit
--   field), stage_expiry (explicit member), and ALL 73 stage keys, including
--   explicit absences. Existing keys need complete contents AND R.ttl receipts.
--   meta uses bounded dynamic_hash(43,64,64) when the handler must classify
--   COUNTER_CORRUPT before schema rejection; fixed_hash(stage_meta) also works
--   for schema-valid snapshots. Partial page uses dynamic_hash; collections
--   use all_members/dynamic_hash; inventory uses R.page(list,0,73,73,128).
--   Returned .v/.n/.bundle/.remaining are diagnostic copies, NOT allocation or
--   publication authority. An internal proof binds the returned handle to ctx.
-- Stage.check_aborted(view,run,job,lease,commit) -> terminal-slot-only proof.
--   Requires exact abort transition, positive original count, ALL 73 keys absent
--   and explicit absent expiry member. No stage, execution or publication grant.
-- Stage.check_renewal_state(ctx,runLedger,jobProjection) -> inspection/code.
--   RENEW-only, validation-only, NO reads/writes/plans or implicit permissions.
--   Caller first gates, Run.load/Job.check, and selects complete stage_slots
--   receipts (R.slots). For an active or current-aborted stage also call
--   Context.bind_stage(ctx,job_id), then Stage.select(ctx,commit) (or equivalent
--   explicit slot-field, expiry-member, all 73 keys + existing TTL receipts).
--   The stored private job supplies owner/token/fence: stale request credentials
--   are NOT substituted. Returns {kind="renewal_inspection",inspection_only=true,
--   state="none"|"owned"|"expired"|"aborted",has_stage,commit_id,expires_at_ms}.
--   No stage/expiry is false, NOT a guessed zero. Owned means materialized and
--   stage-unexpired; the stored lease itself MAY already be expired. Expired
--   means ALL keys explicitly absent, original stage_expiry due, matching owner
--   slot/history and stored lease deadline <= stage deadline. Missing metadata
--   never synthesizes B/G, publication, output or token-digest proof.
--   Worker must check this BEFORE a stale/expired rejection counter; aborted
--   always returns INVALID_STATE without that counter, even after lease expiry.
--   Successful inspection is NOT caller eligibility, spending, publication or
--   renewal authority: worker still owns active run/auth, exact caller lease,
--   rejection-counter eligibility and prescribed deadline/nonshortening checks.
--   Inspection objects NEVER enter the allocation proof registry; slot_proof,
--   publication_proof, begin_proof and validate_chunk reject them even if edited.
-- Stage.check_residue(view,runOrNil,jobOrNil,leaseOrNil,commit) -> deletion-only.
--   Explicit absent slot required. Existing meta/inventory are checked; committed
--   residue needs retained run/job/lease and recomputed commit, recovered residue
--   uses historical meta (NEVER current B/G). Missing meta/inventory is admissible
--   only at the explicit cleanup deadline. All 73 key/TTL receipts still required.
--   This validates residue, NOT cleanup admission: the handler must establish
--   the unchanged earliest pair and now >= returned cleanup_due_at_ms before
--   deleting anything. A future-due materialized residue is inspectable, not due.
--   Slot-free residue alone accepts materialized PTTL=0 at EXACTLY the proved
--   cleanup deadline. Positive TTLs must still match that absolute deadline;
--   future-due zero, persistent/negative TTLs and any extension remain invalid.
--   Owned stages (including renewal inspection) still require positive TTLs.
-- Stage.check_cleanup_residue(view,commit) is the token-free CLEAN variant.
--   With retained committed metadata it requires an explicit private complete
--   job receipt derived from meta.run_id/job_id (Context.bind_stage_owner grants
--   reads). It compares retained commit/publication/output/B/G/fence and computes
--   the exact residual deadline. A cleared raw token cannot be reconstructed;
--   this is deletion-only retained evidence, NEVER a COMMIT replay/publication
--   capability. It reads no final output and needs no Run.load on archived state.
-- Stage.check_committed(view,run,job,lease,commit) -> retained-job receipt only.
--   Full-ledger inspection API; NOT COMMIT's early replay gate. Does NOT read
--   stage/output or compare cleared live lease fields.
-- Stage.committed_receipt(ctx,lease,commit) -> early COMMIT retained receipt.
--   Requires only an explicit private complete typed job and selected run
--   purge_state="none" receipt. No Run.load, membership, authorization, current
--   lease or stage/output admission; the handler must call Gate first.
-- Stage.begin_record(ctx,run,job,lease,input) -> initial schema/keys/identities.
--   input is the opened request projection {v=...}; explicit absence required.
--   The returned object is ALSO a private context-bound BEGIN proof handle.
-- Stage.begin_proof(prepared,ctx) -> copied {kind="begin",key,commit_id,run_id,
--   job_id,fence,remaining=50331648,abort_unlinked_keys=0,expires_at_ms,key_count,
--   meta,job,stage_keys}. This is a prospective reservation, NOT an existing
--   slot receipt or allocation admission. No numeric caller G is accepted.
--   It is issued only after full source/lease/B/G/identity/absence validation.
--   Stage.slot_proof(prepared,ctx) also accepts this BEGIN handle, for Core's
--   single proof-dispatch API. Copies/edited public .meta cannot forge it.
-- Stage.publication_proof(owned,ctx) -> copied semantic run/job/commit/publication
--   IDs, page_url, outlinks, image source URLs and discovery ID/URL pairs. Only
--   a sealed, context-bound owned stage qualifies. Core may derive/grant the
--   closed COMMIT output/backlink/job keys from this proof; no arbitrary paths
--   or multi-MiB payloads are returned as authority.
-- Stage.validate_chunk(ctx,run,job,stage,input) -> prepared records/targets,
--   digest field, exact logical data/key deltas, next_meta and replay flag.
--   input is Wire.request {v,records}; .v contains the full lease and chunk
--   scalars. stage MUST be a check_owned handle from this ctx. All kinds supported;
--   operation/kind, slices, complete source ordering and EVERY replay byte checked.
--   Handlers pass ctx.request; Context.open's source-owned spec must use the
--   normative exact RESP request ceiling (blob 5373952, other chunks 524288).
-- Stage.slot_proof(stage,ctx) -> copied validated owner/remaining/count facts only.
--   This is NOT Plan admission and accepts NO numeric caller G. The parent memory
--   planner must solve self-inclusive remaining-slot G exactly (at most 8 widths,
--   fail if no fixed point), never old-width-overcharge. No abort+postoutcome
--   <=32768 proof is claimed here. Deletions/replays create no allocation credit.
-- Stage.select(ctx,commit) -> {ctx,keys,slots}, explicit bounded reads of the
--   complete closed stage bundle + TTL/slot/expiry receipts. Grants NO permissions.
-- Stage.prepare(literal_operation,KEYS,ARGV) -> sealed execution/code for exactly
--   the eleven stage/publication operations. Core rev4 owns clocked wire/gates,
--   bind_commit, Plan.set_policy, exact slot settlement and all ACL preflights.
--
-- Trust boundary: recompute available control identities; NEVER SHA multi-MiB
-- HTML. Chunk/output/source/artifact evidence digests are trusted-client bulk
-- exceptions, not hostile-client attestation. Client owns transcript completeness
-- and enabled render-rule matching; Lua binds final witness, B/G and pinned digest.
-- Core owns Gate/Plan/memory/ACL, including the post-abort descriptor upper bound.
-- Maintenance owns recovery ordering, earliest cleanup-pair selection and expired
-- owner handling. Derived output names do not grant Context permissions.
local Stage,P,I,S,R,C,O,Job={ },CJ.P,CJ.Identities,CJ.Schemas,CJ.Read,CJ.Context,CJ.StageOutput,CJ.Job
if type(O)~="table" or type(O.record)~="function" or type(Job)~="table" or type(Job.check)~="function" then
    return nil,"INVALID_STATE"
end
local floor=math.floor
local MAX=P.limits.max_integer
local ROOT="mifolyo:crawl:v2:"
local SLOT,EXPIRY=ROOT.."stage_slots",ROOT.."stage_expiry"
local function plain(t) return type(t)=="table" and getmetatable(t)==nil end
local function copy(t)
    if type(t)~="table" then return t end
    local r={}; for k,v in next,t,nil do r[k]=copy(v) end; return r
end
local function text(s,b) return type(s)=="string" and #s<=b and P.validate_text(s)==true end
local function decimal(n) return P.format_decimal(n) end
local function fields(v,names)
    local result={}; for i,k in ipairs(names) do result[i]={k,v[k]} end; return result
end
local names={"protocol_version","run_id","job_id","owner_id","lease_fence","token_digest","commit_id","publication_id","output_digest",
    "request_starts_baseline","request_starts_generation","created_at_ms","expires_at_ms","sealed","sealed_at_ms","abandoned",
    "expected_page_fields","expected_outlinks","expected_discoveries","expected_aliases","expected_images","page_fields_written",
    "html_written","original_html_written","outlinks_written","discoveries_written","aliases_written","images_written","manifest_written",
    "data_bytes","key_count","page_fields_chunk_digest","html_chunk_digest","original_html_chunk_digest","outlinks_chunk_0_digest",
    "outlinks_chunk_1_digest","outlinks_chunk_2_digest","outlinks_chunk_3_digest","discoveries_chunk_0_digest","discoveries_chunk_1_digest",
    "aliases_chunk_0_digest","images_chunk_0_digest","manifest_chunk_digest"}
local bounds={1,32,64,32,16,64,64,64,64}
for i=10,31 do bounds[i]=16 end
for i=32,43 do bounds[i]=64 end
local function publication(run,job,fence,output)
    return I.framed("mifolyo:page-publication:v2",{run,job,fence,output})
end
local function key_count(n)
    return 2+(n.page_fields_written>0 and 1 or 0)+(n.outlinks_written>0 and 1 or 0)+
        (n.discoveries_written>0 and 3 or 0)+(n.aliases_written>0 and 1 or 0)+n.images_written+n.manifest_written
end
local function chunks(v,prefix,maximum,written,sealed,expected)
    local count,empty=0,false
    for i=0,maximum-1 do
        if v[prefix..decimal(i).."_digest"]=="" then empty=true
        elseif empty then return false else count=count+1 end
    end
    if written==0 then return count==0 end
    if count==0 or written<count or written>count*64 then return false end
    return not sealed or count==floor((expected+63)/64)
end
local function validate(v,n)
    for i=10,31 do if n[names[i]]==nil then return nil,"INVALID_NUMBER" end end
    local fence=P.parse_decimal(v.lease_fence)
    if v.protocol_version~="2" or not I.hex(v.run_id,32) or not I.hex(v.job_id,64) or not I.hex(v.owner_id,32) or
       not fence or fence==0 then return nil,"INVALID_IDENTIFIER" end
    for i=6,9 do if not I.digest(v[names[i]]) then return nil,"INVALID_IDENTIFIER" end end
    if n.request_starts_baseline>=n.request_starts_generation or n.request_starts_generation>10 then return nil,"COUNTER_CORRUPT" end
    if publication(v.run_id,v.job_id,v.lease_fence,v.output_digest)~=v.publication_id then return nil,"IMMUTABLE_MISMATCH" end
    if n.created_at_ms==0 or n.created_at_ms>MAX-900000 or n.expires_at_ms~=n.created_at_ms+900000 or
       (v.sealed~="0" and v.sealed~="1") or (v.abandoned~="0" and v.abandoned~="1") or
       n.html_written>1 or n.original_html_written>1 or n.manifest_written>1 or n.expected_page_fields~=10 or
       n.expected_outlinks>256 or n.expected_discoveries>128 or n.expected_aliases<1 or n.expected_aliases>5 or n.expected_images>64 or
       n.page_fields_written>10 or n.outlinks_written>n.expected_outlinks or n.discoveries_written>n.expected_discoveries or
       n.aliases_written>n.expected_aliases or n.images_written>n.expected_images or n.data_bytes>14680064 or
       n.key_count<2 or n.key_count>73 or n.key_count~=key_count(n) or ((n.data_bytes==0)~=(n.key_count==2)) then
        return nil,"STAGE_INVALID"
    end
    local sealed=v.sealed=="1"
    if (not sealed and n.sealed_at_ms~=0) or (sealed and (n.sealed_at_ms<n.created_at_ms or n.sealed_at_ms>=n.expires_at_ms)) then
        return nil,"STAGE_INVALID"
    end
    for i=32,43 do if v[names[i]]~="" and not I.digest(v[names[i]]) then return nil,"INVALID_IDENTIFIER" end end
    local nonblob=n.page_fields_written-n.html_written-n.original_html_written
    if (nonblob~=0 and nonblob~=8) or ((v.page_fields_chunk_digest~="")~=(nonblob==8)) or
       ((v.html_chunk_digest~="")~=(n.html_written==1)) or ((v.original_html_chunk_digest~="")~=(n.original_html_written==1)) or
       ((v.aliases_chunk_0_digest~="")~=(n.aliases_written>0)) or ((v.images_chunk_0_digest~="")~=(n.images_written>0)) or
       ((v.manifest_chunk_digest~="")~=(n.manifest_written==1)) or
       not chunks(v,"outlinks_chunk_",4,n.outlinks_written,sealed,n.expected_outlinks) or
       not chunks(v,"discoveries_chunk_",2,n.discoveries_written,sealed,n.expected_discoveries) then return nil,"STAGE_INVALID" end
    if sealed and (n.page_fields_written~=10 or n.html_written~=1 or n.original_html_written~=1 or
       n.outlinks_written~=n.expected_outlinks or n.discoveries_written~=n.expected_discoveries or
       n.aliases_written~=n.expected_aliases or n.images_written~=n.expected_images or n.manifest_written~=1 or n.data_bytes==0) then
        return nil,"STAGE_INVALID"
    end
    return true
end
local registered,registration_error=S.register("stage_meta",{names=names,bounds=bounds},validate)
if not registered then return nil,registration_error end
function Stage.validate(v) return S.project("stage_meta",v) end
local suffixes={"meta","keys","page","outlinks","discoveries","discovery_records","discovery_depths","aliases","image_manifest"}
local kinds={"hash","list","hash","set","zset","hash","hash","hash","hash"}
function Stage.keys(commit)
    if not I.digest(commit) then return nil,"INVALID_IDENTIFIER" end
    local result={ordered={},kind={},images={}}
    local prefix=ROOT.."stage:"..commit..":"
    for i,suffix in ipairs(suffixes) do
        local key=prefix..suffix
        result[suffix],result.ordered[i],result.kind[key]=key,key,kinds[i]
    end
    for i=1,64 do
        local key=prefix.."image:"..decimal(i-1)
        result.images[i],result.ordered[9+i],result.kind[key]=key,key,"hash"
    end
    return result
end
local function lease_valid(l)
    return plain(l) and I.hex(l.run_id,32) and I.hex(l.job_id,64) and I.hex(l.owner_id,32) and
        I.hex(l.lease_token,64) and P.parse_decimal(l.fence)~=nil and P.parse_decimal(l.fence)>0
end
local function identities(l,v)
    return {
        token_digest=I.framed("mifolyo:lease-token:v2",{l.run_id,l.job_id,l.fence,l.lease_token}),
        publication_id=publication(l.run_id,l.job_id,l.fence,v.output_digest),
        commit_id=I.framed("mifolyo:crawl-commit:v2",{l.run_id,l.job_id,l.fence,l.lease_token,v.publication_id,
            v.request_starts_baseline,v.request_starts_generation})}
end
local function abort_id(l,commit)
    local section=P.section("arguments",{{{"owner_id",l.owner_id},{"commit_id",commit}}},16384)
    local payload=P.sha256(P.frame("mifolyo:transition-payload:v2")..section)
    return I.framed("mifolyo:crawl-transition:v2",{"CJ2_ABORT_STAGE",l.run_id,l.job_id,l.fence,l.lease_token,"none",payload})
end
local function clock(ctx)
    return C.preparing(ctx) and I.integer(ctx.now_ms,MAX) and ctx.now_ms>0 and P.parse_decimal(ctx.now_text)==ctx.now_ms
end
local function backpressure_deadline(start,expires)
    -- min(start+120000, expires-10000), without an inexact intermediate at
    -- MAX_EXACT. Stage expiry is >900000, so the cap is a positive integer.
    local cap=expires-10000
    if cap<=start or cap-start<=120000 then return cap end
    return P.safe_add(start,120000)
end
local function residual_deadline(completed,expires)
    if expires<=completed or expires-completed<=60000 then return expires end
    return P.safe_add(completed,60000)
end
local function snapshot(ctx,key,kind,maximum,complete)
    local fact,code=R.snapshot(ctx,key)
    if not fact then return nil,code end
    if fact.exists==false then
        if fact.kind~="none" or fact.complete~=true or fact.count~=0 then return nil,"INVALID_STATE" end
    elseif fact.exists~=true or fact.kind~=kind or not I.integer(fact.count,maximum) or fact.count==0 then
        return nil,"STAGE_INVALID"
    end
    if complete and not fact.complete then return nil,"INVALID_STATE" end
    return fact
end
local function indexed(ctx,key,member,maximum)
    local f,code=snapshot(ctx,key,"zset",maximum,false)
    if not f then return nil,code end
    if not plain(f.members) or type(f.members[member])~="boolean" or not plain(f.scores) or not plain(f.score_text) then
        return nil,"INVALID_STATE"
    end
    if not f.members[member] then
        if f.scores[member]~=false or f.score_text[member]~=false then return nil,"INVALID_STATE" end
        return {present=false}
    end
    local n=I.redis_score(f.score_text[member])
    if not I.integer(n,MAX) or n==0 or f.scores[member]~=n then return nil,"STAGE_INVALID" end
    return {present=true,score=n}
end
local function slot_inventory(ctx)
    local f,code=snapshot(ctx,SLOT,"hash",4,true)
    if not f then return nil,code end
    if not plain(f.v) then return nil,"INVALID_STATE" end
    local records,count={},0
    for id,value in next,f.v,nil do
        if value~=false then
            count=count+1
            if not I.hex(id,64) or not text(value,126) then return nil,"STAGE_INVALID" end
            local r,run,job,fence,a=string.match(value,"^([^:]+):([^:]+):([^:]+):([^:]+):([^:]+)$")
            local rn,fn,an=P.parse_decimal(r),P.parse_decimal(fence),P.parse_decimal(a)
            if not rn or rn>50331648 or not fn or fn==0 or not an or an>73 or not I.hex(run,32) or not I.hex(job,64) then
                return nil,"STAGE_INVALID"
            end
            records[id]={remaining=rn,run_id=run,job_id=job,fence=fence,abort_unlinked_keys=an,value=value}
        end
    end
    if count~=f.count then return nil,"INVALID_STATE" end
    return {records=records,count=count,selected=f.v}
end
local function slot(ctx,commit)
    local inventory,code=slot_inventory(ctx)
    if not inventory then return nil,code end
    if inventory.selected[commit]==nil then return nil,"INVALID_STATE" end
    return inventory.records[commit] or {absent=true}
end
local function same_record(a,b)
    if not plain(a) or not plain(b) or a.schema~=b.schema or not plain(a.v) or not plain(b.v) then return false end
    local def=S.get(a.schema); if not def then return false end
    for _,name in ipairs(def.names) do if a.v[name]~=b.v[name] then return false end end
    return true
end
local index_names={"jobs","job_order","ready","ready_at","leased","leased_at","delayed","completed","dead","cancelled","commit_backpressure"}
local function binding(ctx,run,job,l)
    if not clock(ctx) or not lease_valid(l) or not plain(run) or run.run_id~=l.run_id or not plain(job) then return nil,"INVALID_STATE" end
    local base=ROOT.."run:"..l.run_id
    local view={ctx=ctx,job={key=base..":job:"..l.job_id},active_leases={key=ROOT.."active_leases"}}
    for _,k in ipairs(index_names) do view[k]={key=base..":"..k} end
    local checked,code=Job.check(view,run,l.job_id)
    if not checked then return nil,code end
    if not checked.exists or not same_record(job,checked) then return nil,"INVALID_STATE" end
    return checked
end
local function live(ctx,job,l)
    local v,n=job.v,job.n
    if v.state~="leased" or v.lease_owner~=l.owner_id or v.lease_token~=l.lease_token or v.lease_fence~=l.fence or
       n.lease_expires_at_ms<=ctx.now_ms then return nil,"LEASE_LOST" end
    if v.active_reservation_id~="" or v.lease_delivery_started~="1" or v.last_document_request_fence~=l.fence then
        return nil,"INVALID_STATE"
    end
    return true
end
local function active(ctx,run)
    return run.v.state=="active" and P.parse_decimal(run.v.authorization_expires_at_ms)>ctx.now_ms and run.v.purge_state=="none"
end
local function all_keys(ctx,keys)
    local facts={}
    for _,key in ipairs(keys.ordered) do
        local f,code=snapshot(ctx,key,keys.kind[key],key==keys.keys and 73 or 896,true)
        if not f then return nil,code end
        facts[key]=f
    end
    return facts
end
local function expiry(f,ctx,absolute)
    return f.exists and I.integer(f.ttl_ms,MAX) and f.ttl_ms>0 and absolute>=ctx.now_ms and f.ttl_ms==absolute-ctx.now_ms
end
local function inventory(facts,keys,expected,ctx,absolute,allow_missing,residue)
    local inv=facts[keys.keys]
    if not inv.exists or not plain(inv.items) or I.dense(inv.items,73)~=inv.count then return nil,"INVALID_STATE" end
    local seen,count={},0
    for _,key in ipairs(inv.items) do
        if not keys.kind[key] or seen[key] or not expected[key] then return nil,"STAGE_INVALID" end
        seen[key]=true
    end
    for key in next,expected,nil do count=count+1; if not seen[key] then return nil,"STAGE_INVALID" end end
    if inv.count~=count or not seen[keys.meta] or not seen[keys.keys] then return nil,"STAGE_INVALID" end
    for _,key in ipairs(keys.ordered) do
        local f=facts[key]
        if f.exists then
            -- Only check_residue supplies this source-owned mode, AFTER proving
            -- slot absence and the committed/recovered absolute cleanup time.
            -- Zero is a materialized exact-boundary receipt, not persistence or
            -- a blanket overdue-key exception; live expiry() is unchanged.
            local due_zero=residue==true and f.ttl_ms==0 and absolute==ctx.now_ms
            if not seen[key] or (not expiry(f,ctx,absolute) and not due_zero) then return nil,"STAGE_INVALID" end
        elseif expected[key] and not (allow_missing and allow_missing[key]) then return nil,"STAGE_INVALID" end
    end
    return true
end
local function expected_keys(keys,n)
    local e={[keys.meta]=true,[keys.keys]=true}
    if n.page_fields_written>0 then e[keys.page]=true end
    if n.outlinks_written>0 then e[keys.outlinks]=true end
    if n.discoveries_written>0 then e[keys.discoveries],e[keys.discovery_records],e[keys.discovery_depths]=true,true,true end
    if n.aliases_written>0 then e[keys.aliases]=true end
    for i=1,n.images_written do e[keys.images[i]]=true end
    if n.manifest_written>0 then e[keys.image_manifest]=true end
    return e
end
local function hash_shape(f,expected)
    local count=0
    for k,v in next,f.v,nil do
        if v~=false then if not expected[k] then return false end; count=count+1 end
    end
    local n=0
    for k in next,expected,nil do n=n+1; if type(f.v[k])~="string" then return false end end
    return f.count==count and count==n and f.exists==(n>0)
end
local function ordered(f)
    local out={}
    for m,present in next,f.members,nil do if present then out[#out+1]=m end end
    if #out~=f.count then return nil end
    table.sort(out); return out
end
local nonblob={"normalized_url","content_type","status_code","last_crawled","rendered","render_policy_rule","render_policy_sha256","publication_id"}
local discovery_names={"job_id","canonical_url","depth","score_text","group_id","rate_scope_id","group_scope_id","initial_origin_scope_id","policy_decision_sha256"}
local discovery_suffixes={"canonical_url","score_text","group_id","rate_scope_id","group_scope_id","initial_origin_scope_id","policy_decision_sha256"}
local alias_names={"url_id","canonical_url","depth"}
local function page_context(v,run,job,meta)
    return v.normalized_url==job.v.last_document_target_url and v.publication_id==meta.v.publication_id and
        v.last_crawled==O.last_crawled(job.v.last_document_request_started_at_ms) and
        (v.rendered~="true" or v.render_policy_sha256==run.v.render_policy_sha256)
end
local function read_record(input,names,limits,maximum)
    local encoded=input
    if plain(input) then encoded=P.record(input.fields,maximum) end
    if type(encoded)~="string" then return nil,"INVALID_ARGUMENT" end
    local f=P.decode_record(encoded,names,maximum)
    if not f then return nil,"INVALID_ARGUMENT" end
    local v={}
    for i,name in ipairs(names) do
        if not text(f[i][2],limits[i]) then return nil,"INVALID_ARGUMENT" end
        v[name]=f[i][2]
    end
    return {v=v,fields=f}
end
local function alias(v,depth)
    local url=CJ.URL.check_canonical(v.canonical_url,1)
    return url and I.hex(v.url_id,64) and v.url_id==url.url_id and P.parse_decimal(v.depth)~=nil and (depth==nil or v.depth==depth)
end
local function data_bytes(facts,keys)
    local sum=0
    for _,key in ipairs(keys.ordered) do
        local f=facts[key]
        if f.exists and key~=keys.meta and key~=keys.keys then
            sum=sum+#key
            if f.kind=="hash" then
                for field,v in next,f.v,nil do if v~=false then sum=sum+#field+#v end end
            elseif f.kind=="set" then
                for m,v in next,f.members,nil do if v then sum=sum+#m end end
            elseif f.kind=="zset" then
                for m,v in next,f.members,nil do
                    if v then
                        local score=facts[keys.discovery_records].v[m..":score_text"]
                        if type(score)~="string" then return nil end
                        sum=sum+#m+#score
                    end
                end
            end
            if sum>14680064 then return nil end
        end
    end
    return sum
end
-- Full partial-stage projection; no payload/hash is treated as opaque authority.
local function bundle(ctx,run,job,meta,keys,facts)
    local v,n=meta.v,meta.n
    local expected=expected_keys(keys,n)
    local ok,code=inventory(facts,keys,expected,ctx,n.expires_at_ms)
    if not ok then return nil,code end
    local page=facts[keys.page]
    local shape={}
    if v.page_fields_chunk_digest~="" then for _,k in ipairs(nonblob) do shape[k]=true end end
    if n.html_written==1 then shape.html=true end
    if n.original_html_written==1 then shape.original_html=true end
    if not hash_shape(page,shape) then return nil,"STAGE_INVALID" end
    if shape.normalized_url then
        local record=O.record("page_fields",{fields=fields(page.v,nonblob)})
        if not record or not page_context(record.v,run,job,meta) then return nil,"STAGE_INVALID" end
    end
    for _,name in ipairs({"html","original_html"}) do
        if shape[name] and not text(page.v[name],5242880) then return nil,"STAGE_INVALID" end
    end
    if shape.html and shape.original_html and #page.v.html+#page.v.original_html>10485760 then return nil,"LIMIT_EXCEEDED" end
    if shape.normalized_url and shape.original_html and
       ((page.v.rendered=="true")~=(#page.v.original_html>0)) then return nil,"STAGE_INVALID" end
    if n.page_fields_written==10 and not S.project("final_page",page.v) then return nil,"STAGE_INVALID" end
    local outlinks=ordered(facts[keys.outlinks])
    if not outlinks or #outlinks~=n.outlinks_written then return nil,"STAGE_INVALID" end
    local backlink_keys={}
    for i,url in ipairs(outlinks) do
        if not CJ.URL.check_canonical(url,1) or url==job.v.last_document_target_url then return nil,"STAGE_INVALID" end
        backlink_keys[i]="backlinks:"..url
    end
    local ids=ordered(facts[keys.discoveries])
    if not ids or #ids~=n.discoveries_written then return nil,"STAGE_INVALID" end
    local discoveries,record_shape,depth_shape,discovery_job_keys={},{},{},{}
    for i,id in ipairs(ids) do
        if not I.hex(id,64) then return nil,"STAGE_INVALID" end
        local values={job_id=id,depth=facts[keys.discovery_depths].v[id]}
        depth_shape[id]=true
        for _,k in ipairs(discovery_suffixes) do record_shape[id..":"..k]=true; values[k]=facts[keys.discovery_records].v[id..":"..k] end
        local source=Job.discovery({fields=fields(values,discovery_names)},run)
        local score=source and I.score(source.v.score_text)
        local native=facts[keys.discoveries].score_text[id]
        if not source or score~=facts[keys.discoveries].scores[id] or I.redis_score(native)~=score or
           (score==0 and string.sub(native,1,1)=="-") then return nil,"STAGE_INVALID" end
        discoveries[i]=source -- .fields deliberately in SOURCE order for new-job constructors
        discovery_job_keys[i]=ROOT.."run:"..run.run_id..":job:"..id
    end
    if not hash_shape(facts[keys.discovery_records],record_shape) or not hash_shape(facts[keys.discovery_depths],depth_shape) then
        return nil,"STAGE_INVALID"
    end
    local aliases,alias_shape,alias_ids={},{},{}
    for name,value in next,facts[keys.aliases].v,nil do
        if value~=false then
            local id,suffix=string.match(name,"^([0-9a-f]+):([a-z_]+)$")
            if not I.hex(id,64) or (suffix~="canonical_url" and suffix~="depth") then return nil,"STAGE_INVALID" end
            if suffix=="canonical_url" then alias_ids[#alias_ids+1]=id end
        end
    end
    table.sort(alias_ids)
    if #alias_ids~=n.aliases_written then return nil,"STAGE_INVALID" end
    for i,id in ipairs(alias_ids) do
        local values={url_id=id,canonical_url=facts[keys.aliases].v[id..":canonical_url"],depth=facts[keys.aliases].v[id..":depth"]}
        if not alias(values,job.v.depth) then return nil,"STAGE_INVALID" end
        aliases[i]={v=values,fields=fields(values,alias_names)}
        alias_shape[id..":canonical_url"],alias_shape[id..":depth"]=true,true
    end
    if not hash_shape(facts[keys.aliases],alias_shape) then return nil,"STAGE_INVALID" end
    if #aliases>0 and (not alias_shape[job.v.job_id..":canonical_url"] or
       not alias_shape[job.v.last_document_target_url_id..":canonical_url"] or #aliases~=n.expected_aliases) then return nil,"STAGE_INVALID" end
    local images,image_keys={},{}
    for i=1,n.images_written do
        local image=S.project("final_image",facts[keys.images[i]].v)
        if not image or facts[keys.images[i]].count~=5 or image.v.publication_id~=v.publication_id or
           image.v.normalized_page_url~=job.v.last_document_target_url or
           (i>1 and image.v.normalized_source_url<=images[i-1].v.normalized_source_url) then return nil,"STAGE_INVALID" end
        images[i]=image
        image_keys[i]=O.keys(v.publication_id,job.v.last_document_target_url,image.v.normalized_source_url).image
    end
    if #images>0 and #images~=n.expected_images then return nil,"STAGE_INVALID" end
    local manifest
    if n.manifest_written==1 then
        manifest=S.project("image_manifest",facts[keys.image_manifest].v)
        local list=manifest and O.manifest(manifest.v)
        if not manifest or facts[keys.image_manifest].count~=5 or manifest.v.publication_id~=v.publication_id or
           manifest.v.normalized_url~=job.v.last_document_target_url or manifest.n.image_count~=n.expected_images or
           #list.keys~=#image_keys then return nil,"STAGE_INVALID" end
        for i,key in ipairs(image_keys) do if list.keys[i]~=key then return nil,"STAGE_INVALID" end end
    end
    -- Progress schemas permit partial batches; materialized state additionally
    -- must correspond to the fixed contiguous slices required by the norm.
    for _,kind in ipairs({"outlinks","discoveries"}) do
        local count=0
        for ordinal=0,(kind=="outlinks" and 3 or 1) do
            if v[kind.."_chunk_"..decimal(ordinal).."_digest"]~="" then
                local size=math.min(64,n["expected_"..kind]-64*ordinal)
                if size<=0 then return nil,"STAGE_INVALID" end
                count=count+size
            end
        end
        if count~=n[kind.."_written"] then return nil,"STAGE_INVALID" end
    end
    local bytes=data_bytes(facts,keys)
    if not bytes or bytes~=n.data_bytes then return nil,"STAGE_INVALID" end
    return {page=page,outlinks=outlinks,backlink_keys=backlink_keys,discoveries=discoveries,discovery_job_keys=discovery_job_keys,
        aliases=aliases,images=images,image_keys=image_keys,
        manifest=manifest,data_bytes=bytes,final_keys=O.keys(v.publication_id,job.v.last_document_target_url)}
end
local proofs={}
local function proof(ctx,kind,meta,keys,slot,extra)
    local result={kind=kind,v=meta and copy(meta.v),n=meta and copy(meta.n),keys=copy(keys),
        remaining=slot and slot.remaining,abort_unlinked_keys=slot and slot.abort_unlinked_keys}
    for k,v in next,extra or {},nil do result[k]=copy(v) end
    proofs[result]={ctx=ctx,kind=kind,meta=copy(meta),keys=copy(keys),slot=copy(slot),extra=copy(extra)}
    return result
end
function Stage.begin_proof(prepared,ctx)
    local p=proofs[prepared]
    if not p or p.kind~="begin" or p.ctx~=ctx or not clock(ctx) then return nil,"INVALID_STATE" end
    local v=p.meta.v
    return {kind="begin",key=SLOT,commit_id=v.commit_id,run_id=v.run_id,job_id=v.job_id,fence=v.lease_fence,
        remaining=50331648,abort_unlinked_keys=0,expires_at_ms=p.meta.n.expires_at_ms,key_count=2,
        meta=copy(p.meta),job=copy(p.extra.job),stage_keys=copy(p.keys.ordered)}
end
function Stage.slot_proof(stage,ctx)
    local p=proofs[stage]
    if p and p.kind=="begin" then return Stage.begin_proof(stage,ctx) end
    if not p or (p.kind~="owned" and p.kind~="aborted") or p.ctx~=ctx or not clock(ctx) or not p.slot or p.slot.absent then return nil,"INVALID_STATE" end
    local out=copy(p.slot)
    out.kind,out.key=p.kind,SLOT
    out.commit_id=p.meta and p.meta.v.commit_id or p.extra.commit_id
    out.expires_at_ms=p.meta and p.meta.n.expires_at_ms
    out.key_count=p.meta and p.meta.n.key_count or p.slot.abort_unlinked_keys
    out.meta,out.job,out.stage_keys=copy(p.meta),copy(p.extra.job),copy(p.keys.ordered)
    return out
end
function Stage.publication_proof(stage,ctx)
    local p=proofs[stage]
    if not p or p.kind~="owned" or p.ctx~=ctx or not clock(ctx) or p.meta.v.sealed~="1" then return nil,"INVALID_STATE" end
    local v,b=p.meta.v,p.extra.bundle
    local result={run_id=v.run_id,job_id=v.job_id,commit_id=v.commit_id,publication_id=v.publication_id,
        page_url=p.extra.job.v.last_document_target_url,outlinks=copy(b.outlinks),images={},discoveries={}}
    for i,image in ipairs(b.images) do result.images[i]=image.v.normalized_source_url end
    for i,source in ipairs(b.discoveries) do result.discoveries[i]={job_id=source.v.job_id,canonical_url=source.v.canonical_url} end
    return result
end
-- Shared MATERIALIZED-stage validation, not a live-lease or allocation proof.
-- Both callers establish the exact slot/job tuple first. There is deliberately
-- no public "allow_expired" switch and this helper still requires live stage TTLs.
local function materialized(ctx,run,j,l,commit,keys,facts)
    local raw=facts[keys.meta]
    if not raw.exists or raw.count~=43 then return nil,"STAGE_INVALID" end
    -- Establish full current ownership BEFORE classifying B/G drift.
    local v=raw.v
    local token=I.framed("mifolyo:lease-token:v2",{l.run_id,l.job_id,l.fence,l.lease_token})
    if v.run_id~=l.run_id or v.job_id~=l.job_id or v.owner_id~=l.owner_id or v.lease_fence~=l.fence or
       v.commit_id~=commit or v.token_digest~=token then return nil,"STAGE_INVALID" end
    if v.request_starts_baseline~=j.v.lease_request_starts_baseline or v.request_starts_generation~=j.v.request_starts then
        return nil,"COUNTER_CORRUPT"
    end
    local meta=S.project("stage_meta",v)
    if not meta then return nil,"STAGE_INVALID" end
    -- Every distinct alias needs a represented document/redirect START. The
    -- transcript itself stays client-owned, but its frozen length is available.
    if meta.n.expected_aliases>meta.n.request_starts_generation-meta.n.request_starts_baseline then return nil,"STAGE_INVALID" end
    local ids=identities(l,v)
    if ids.commit_id~=commit or ids.publication_id~=v.publication_id or v.abandoned~="0" or meta.n.expires_at_ms<=ctx.now_ms or
       meta.n.sealed_at_ms>ctx.now_ms or
       meta.n.created_at_ms>ctx.now_ms or meta.n.created_at_ms<j.n.last_request_started_at_ms then return nil,"STAGE_INVALID" end
    local ix,code=indexed(ctx,EXPIRY,commit,100000); if not ix then return nil,code end
    if not ix.present or ix.score~=meta.n.expires_at_ms then return nil,"STAGE_INVALID" end
    if j.v.commit_backpressure_reason~="none" then
        local deadline=backpressure_deadline(j.n.commit_backpressure_started_at_ms,meta.n.expires_at_ms)
        if not deadline or j.n.commit_backpressure_started_at_ms<meta.n.created_at_ms or j.n.commit_backpressure_started_at_ms>ctx.now_ms or
           j.n.commit_backpressure_deadline_ms~=deadline then return nil,"STAGE_INVALID" end
    end
    local b; b,code=bundle(ctx,run,j,meta,keys,facts); if not b then return nil,code end
    return {meta=meta,bundle=b,identities=ids}
end
function Stage.check_owned(view,run,job,l,commit)
    if not plain(view) or not I.digest(commit) then return nil,"INVALID_ARGUMENT" end
    local ctx=view.ctx
    local j,code=binding(ctx,run,job,l); if not j then return nil,code end
    local ok; ok,code=live(ctx,j,l); if not ok then return nil,code end
    local s; s,code=slot(ctx,commit); if not s then return nil,code end
    if s.absent or s.abort_unlinked_keys~=0 or s.run_id~=l.run_id or s.job_id~=l.job_id or s.fence~=l.fence or
       j.v.active_stage_commit_id~=commit or j.v.last_stage_commit_id~=commit or j.v.last_stage_fence~=l.fence then return nil,"STAGE_INVALID" end
    local keys=Stage.keys(commit)
    local facts; facts,code=all_keys(ctx,keys); if not facts then return nil,code end
    local checked; checked,code=materialized(ctx,run,j,l,commit,keys,facts); if not checked then return nil,code end
    local meta,b,ids=checked.meta,checked.bundle,checked.identities
    return proof(ctx,"owned",meta,keys,s,{bundle=b,lease=l,run=run,job=j,facts=facts,identities=ids})
end
local function empty_bundle(ctx,keys)
    local facts,code=all_keys(ctx,keys); if not facts then return nil,code end
    for _,key in ipairs(keys.ordered) do if facts[key].exists then return nil,"STAGE_INVALID" end end
    return true
end
function Stage.check_aborted(view,run,job,l,commit)
    if not plain(view) or not I.digest(commit) then return nil,"INVALID_ARGUMENT" end
    local ctx=view.ctx
    local j,code=binding(ctx,run,job,l); if not j then return nil,code end
    local ok; ok,code=live(ctx,j,l); if not ok then return nil,code end
    local s; s,code=slot(ctx,commit); if not s then return nil,code end
    if s.absent or s.abort_unlinked_keys<2 or s.run_id~=l.run_id or s.job_id~=l.job_id or s.fence~=l.fence or
       j.v.active_stage_commit_id~="" or j.v.last_stage_commit_id~=commit or j.v.last_stage_fence~=l.fence or
       j.v.last_transition_status~="STAGE_ABORTED" or j.v.last_transition_id~=abort_id(l,commit) then return nil,"LEASE_LOST" end
    local ix; ix,code=indexed(ctx,EXPIRY,commit,100000); if not ix then return nil,code end
    if ix.present then return nil,"STAGE_INVALID" end
    local keys=Stage.keys(commit)
    ok,code=empty_bundle(ctx,keys); if not ok then return nil,code end
    return proof(ctx,"aborted",nil,keys,s,{transition_id=j.v.last_transition_id,lease=l,job=j,commit_id=commit})
end
function Stage.check_renewal_state(ctx,run,job)
    if not clock(ctx) or ctx.operation~="CJ2_RENEW_LEASE" or not plain(ctx.request) or not plain(ctx.request.v) or
       not plain(run) or not I.hex(ctx.request.v.run_id,32) or not I.hex(ctx.request.v.job_id,64) or
       run.run_id~=ctx.request.v.run_id then return nil,"INVALID_STATE" end
    local a=ctx.request.v
    local raw,code=R.snapshot(ctx,ROOT.."run:"..a.run_id..":job:"..a.job_id)
    if not raw then return nil,code end
    if not raw.exists or not raw.complete or raw.kind~="hash" or raw.schema~="job" or not plain(raw.v) then return nil,"INVALID_STATE" end
    local v=raw.v
    -- Identity is selected from the PRIVATE stored job, not the renewal caller
    -- and not an edited job.v projection. binding revalidates all Job/Run facts.
    local l={run_id=v.run_id,job_id=v.job_id,owner_id=v.lease_owner,lease_token=v.lease_token,fence=v.lease_fence}
    if v.state~="leased" or v.run_id~=a.run_id or v.job_id~=a.job_id then return nil,"INVALID_STATE" end
    local j; j,code=binding(ctx,run,job,l); if not j then return nil,code end
    v=j.v
    local slots; slots,code=slot_inventory(ctx); if not slots then return nil,code end
    local commit=v.active_stage_commit_id
    local aborted=commit=="" and j.n.last_stage_fence>0 and v.last_stage_fence==v.lease_fence
    if aborted then commit=v.last_stage_commit_id end
    -- A second slot for this job, or one left on an older fence, is corruption,
    -- not a reason to increment a stale caller's rejection counter.
    for id,s in next,slots.records,nil do
        if s.run_id==v.run_id and s.job_id==v.job_id and (id~=commit or s.fence~=v.lease_fence) then return nil,"STAGE_INVALID" end
    end
    local result={kind="renewal_inspection",inspection_only=true,state="none",has_stage=false,commit_id=false,expires_at_ms=false}
    if commit=="" then return result end
    if not I.digest(commit) or v.last_stage_commit_id~=commit or v.last_stage_fence~=v.lease_fence or
       v.active_reservation_id~="" or v.lease_delivery_started~="1" or v.last_document_request_fence~=v.lease_fence then return nil,"STAGE_INVALID" end
    local s; s,code=slot(ctx,commit); if not s then return nil,code end
    if s.absent or s.run_id~=v.run_id or s.job_id~=v.job_id or s.fence~=v.lease_fence then return nil,"STAGE_INVALID" end
    local ix; ix,code=indexed(ctx,EXPIRY,commit,100000); if not ix then return nil,code end
    local keys=Stage.keys(commit)
    local facts; facts,code=all_keys(ctx,keys); if not facts then return nil,code end
    result.has_stage,result.commit_id=true,commit
    if aborted then
        if s.abort_unlinked_keys<2 or ix.present or v.last_transition_status~="STAGE_ABORTED" or
           v.last_transition_id~=abort_id(l,commit) then return nil,"STAGE_INVALID" end
        for _,key in ipairs(keys.ordered) do if facts[key].exists then return nil,"STAGE_INVALID" end end
        result.state="aborted"
        return result
    end
    if s.abort_unlinked_keys~=0 or not ix.present or j.n.lease_expires_at_ms>ix.score then return nil,"STAGE_INVALID" end
    result.expires_at_ms=ix.score
    if facts[keys.meta].exists then
        local checked; checked,code=materialized(ctx,run,j,l,commit,keys,facts)
        if not checked then return nil,code end
        result.state="owned"
    else
        -- Original owned-stage expiry is still the score: neither COMMIT nor
        -- recovery may retain an owner slot with a shortened residual deadline.
        -- After it, the norm permits the exact slot/history in place of missing
        -- meta. Do NOT fabricate lost snapshots or claim a cryptographic check
        -- of the vanished output/token/commit inputs. This authorizes NO writes.
        if ctx.now_ms<ix.score or ix.score<=900000 or ix.score-900000<j.n.last_request_started_at_ms then return nil,"STAGE_INVALID" end
        for _,key in ipairs(keys.ordered) do if facts[key].exists then return nil,"STAGE_INVALID" end end
        if v.commit_backpressure_reason~="none" then
            local started=j.n.commit_backpressure_started_at_ms
            if started<ix.score-900000 or started>ctx.now_ms or
               j.n.commit_backpressure_deadline_ms~=backpressure_deadline(started,ix.score) then return nil,"STAGE_INVALID" end
        end
        result.state="expired"
    end
    return result
end
function Stage.check_committed(view,run,job,l,commit)
    if not plain(view) or not I.digest(commit) then return nil,"INVALID_ARGUMENT" end
    local j,code=binding(view.ctx,run,job,l); if not j then return nil,code end
    local v=j.v
    if run.v.purge_state~="none" then return nil,"INVALID_STATE" end
    if v.state~="completed" or v.last_reason~="published" or v.commit_id~=commit or v.last_stage_commit_id~=commit or
       v.lease_fence~=l.fence or v.last_stage_fence~=l.fence then return nil,"LEASE_LOST" end
    local expected=I.framed("mifolyo:crawl-commit:v2",{l.run_id,l.job_id,l.fence,l.lease_token,v.publication_id,
        v.lease_request_starts_baseline,v.request_starts})
    if expected~=commit then return nil,"LEASE_LOST" end
    return {kind="committed_receipt",publication_id=v.publication_id,commit_id=commit,completed_at_ms=v.completed_at_ms}
end
local function cleanup_committed(ctx,meta,commit)
    local v=meta.v
    local raw,code=R.snapshot(ctx,ROOT.."run:"..v.run_id..":job:"..v.job_id)
    if not raw then return nil,code end
    local job=raw.exists and raw.complete and raw.kind=="hash" and raw.schema=="job" and S.project("job",raw.v)
    if not job then return nil,"STAGE_INVALID" end
    local j,n=job.v,job.n
    if j.state~="completed" or j.last_reason~="published" or j.run_id~=v.run_id or j.job_id~=v.job_id or
       j.commit_id~=commit or j.last_stage_commit_id~=commit or j.publication_id~=v.publication_id or j.output_digest~=v.output_digest or
       j.last_stage_fence~=v.lease_fence or j.lease_fence~=v.lease_fence or
       j.lease_request_starts_baseline~=v.request_starts_baseline or j.request_starts~=v.request_starts_generation or
       v.sealed~="1" or v.abandoned~="0" or n.completed_at_ms<meta.n.sealed_at_ms or n.completed_at_ms>ctx.now_ms or
       n.completed_at_ms>=meta.n.expires_at_ms then return nil,"STAGE_INVALID" end
    return {completed_at_ms=j.completed_at_ms,publication_id=j.publication_id}
end
function Stage.check_residue(view,run,job,l,commit)
    if not plain(view) or not clock(view.ctx) or not I.digest(commit) then return nil,"INVALID_ARGUMENT" end
    local ctx=view.ctx
    local s,code=slot(ctx,commit); if not s then return nil,code end
    if not s.absent then return nil,"STAGE_INVALID" end
    local ix; ix,code=indexed(ctx,EXPIRY,commit,100000); if not ix then return nil,code end
    local keys=Stage.keys(commit)
    local facts; facts,code=all_keys(ctx,keys); if not facts then return nil,code end
    local raw,inv=facts[keys.meta],facts[keys.keys]
    local deletes={}
    for _,key in ipairs(keys.ordered) do if facts[key].exists then deletes[#deletes+1]=key end end
    if not ix.present then
        if #deletes~=0 then return nil,"STAGE_INVALID" end
        return {kind="absent",deletion_only=true,delete_keys={},key_count=0}
    end
    local meta,kind
    if raw.exists then
        meta=S.project("stage_meta",raw.v)
        if not meta or raw.count~=43 or meta.v.commit_id~=commit or ix.score>meta.n.expires_at_ms then return nil,"STAGE_INVALID" end
        if meta.v.abandoned=="1" then
            kind="recovered_residue"
            if ix.score~=meta.n.expires_at_ms then return nil,"STAGE_INVALID" end
            -- Historical B/G, owner and fence are not rebound to a newer claim.
        else
            local receipt
            if l==nil then
                receipt,code=cleanup_committed(ctx,meta,commit)
                if not receipt then return nil,code end
            else
                receipt,code=Stage.check_committed(view,run,job,l,commit)
                if not receipt then return nil,code end
                local ids=identities(l,meta.v)
                if meta.v.sealed~="1" or meta.v.run_id~=l.run_id or meta.v.job_id~=l.job_id or
                   meta.v.lease_fence~=l.fence or meta.v.owner_id~=l.owner_id or ids.token_digest~=meta.v.token_digest or
                   ids.commit_id~=commit or meta.v.publication_id~=receipt.publication_id or meta.v.output_digest~=job.v.output_digest or
                   meta.v.request_starts_baseline~=job.v.lease_request_starts_baseline or meta.v.request_starts_generation~=job.v.request_starts then return nil,"STAGE_INVALID" end
            end
            local limit=residual_deadline(P.parse_decimal(receipt.completed_at_ms),meta.n.expires_at_ms)
            if not limit or ix.score~=limit then return nil,"STAGE_INVALID" end
            kind="committed_residue"
        end
    else kind="expired_residue" end
    if not raw.exists or not inv.exists then
        if ctx.now_ms<ix.score then return nil,"STAGE_INVALID" end
        -- At/after deadline, retained-state expiry is not permission to follow
        -- arbitrary inventory paths. Every possible closed key was selected.
        if #deletes>0 then return nil,"STAGE_INVALID" end
    else
        local expected=expected_keys(keys,meta.n)
        local moved={}
        if kind=="committed_residue" then
            moved[keys.page],moved[keys.outlinks],moved[keys.image_manifest]=true,true,true
            for _,key in ipairs(keys.images) do moved[key]=true end
            for key in next,moved,nil do if facts[key].exists then return nil,"STAGE_INVALID" end end
        end
        local ok; ok,code=inventory(facts,keys,expected,ctx,ix.score,moved,true)
        if not ok then return nil,code end
    end
    return {kind=kind,deletion_only=true,delete_keys=deletes,key_count=#deletes,cleanup_due_at_ms=ix.score,meta=meta}
end
function Stage.check_cleanup_residue(view,commit)
    return Stage.check_residue(view,nil,nil,nil,commit)
end

function Stage.begin_record(ctx,run,job,l,input)
    if not plain(input) or not plain(input.v) then return nil,"INVALID_ARGUMENT" end
    local j,code=binding(ctx,run,job,l); if not j then return nil,code end
    local ok; ok,code=live(ctx,j,l); if not ok then return nil,code end
    if not active(ctx,run) or j.v.active_stage_commit_id~="" or j.n.last_stage_fence>=P.parse_decimal(l.fence) then return nil,"INVALID_STATE" end
    local a=input.v
    for _,name in ipairs({"run_id","job_id","owner_id","lease_token","fence"}) do if a[name]~=l[name] then return nil,"IMMUTABLE_MISMATCH" end end
    if a.request_starts_baseline~=j.v.lease_request_starts_baseline or a.request_starts_generation~=j.v.request_starts then return nil,"STAGE_INVALID" end
    local expires=P.safe_add(ctx.now_ms,900000); if not expires then return nil,"INVALID_NUMBER" end
    local v={}
    for i,name in ipairs(names) do v[name]=(i>=10 and i<=31) and "0" or "" end
    for _,name in ipairs({"run_id","job_id","owner_id","commit_id","publication_id","output_digest","request_starts_baseline",
        "request_starts_generation","expected_page_fields","expected_outlinks","expected_discoveries","expected_aliases","expected_images"}) do v[name]=a[name] end
    v.protocol_version,v.lease_fence,v.created_at_ms,v.expires_at_ms,v.key_count="2",l.fence,ctx.now_text,decimal(expires),"2"
    if not I.digest(v.output_digest) or not I.digest(v.publication_id) or not I.digest(v.commit_id) then return nil,"INVALID_IDENTIFIER" end
    local ids=identities(l,v); v.token_digest=ids.token_digest
    if ids.publication_id~=v.publication_id or ids.commit_id~=v.commit_id then return nil,"IMMUTABLE_MISMATCH" end
    local meta; meta,code=S.project("stage_meta",v); if not meta then return nil,code end
    if meta.n.expected_aliases>meta.n.request_starts_generation-meta.n.request_starts_baseline then return nil,"STAGE_INVALID" end
    local keys=Stage.keys(v.commit_id)
    ok,code=empty_bundle(ctx,keys); if not ok then return nil,code end
    local s; s,code=slot(ctx,v.commit_id); if not s then return nil,code end
    local ix; ix,code=indexed(ctx,EXPIRY,v.commit_id,100000); if not ix then return nil,code end
    if not s.absent or ix.present then return nil,"STAGE_INVALID" end
    local result={meta=meta,keys=keys,identities=ids,inventory={keys.meta,keys.keys},expires_at_ms=expires}
    proofs[result]={ctx=ctx,kind="begin",meta=copy(meta),keys=copy(keys),extra={job=copy(j)}}
    return result
end

local operations={page_fields="CJ2_STAGE_PAGE_FIELDS",html="CJ2_STAGE_PAGE_BLOB",original_html="CJ2_STAGE_PAGE_BLOB",
    outlinks="CJ2_STAGE_OUTLINKS_BATCH",discoveries="CJ2_STAGE_DISCOVERIES_BATCH",aliases="CJ2_STAGE_ALIASES_BATCH",
    images="CJ2_STAGE_IMAGES_BATCH",image_manifest="CJ2_STAGE_IMAGE_MANIFEST"}
local function digest_field(kind,ordinal)
    if kind=="image_manifest" then return "manifest_chunk_digest" end
    if kind=="outlinks" or kind=="discoveries" or kind=="aliases" or kind=="images" then return kind.."_chunk_"..decimal(ordinal).."_digest" end
    return kind.."_chunk_digest"
end
local function hash_target(key,f) return {key=key,kind="hash",fields=f} end
function Stage.validate_chunk(ctx,run,job,stage,input)
    local p=proofs[stage]
    if not p or p.kind~="owned" or p.ctx~=ctx or not clock(ctx) or not plain(input) or not plain(input.v) then return nil,"INVALID_STATE" end
    local a,l,meta,keys=input.v,p.extra.lease,p.meta,p.keys
    if not plain(run) or run.run_id~=p.extra.run.run_id or not same_record({schema="run",v=run.v},{schema="run",v=p.extra.run.v}) or
       not same_record(job,p.extra.job) then return nil,"IMMUTABLE_MISMATCH" end
    -- Use only the privately saved Run/Job policy projection after comparison.
    run,job=p.extra.run,p.extra.job
    if not active(ctx,run) or meta.v.sealed~="0" or meta.v.abandoned~="0" or meta.n.expires_at_ms<=ctx.now_ms then return nil,"STAGE_INVALID" end
    for _,name in ipairs({"run_id","job_id","owner_id","lease_token","fence"}) do if a[name]~=l[name] then return nil,"LEASE_LOST" end end
    if a.commit_id~=meta.v.commit_id then return nil,"IMMUTABLE_MISMATCH" end
    local kind,ordinal=a.chunk_kind,P.parse_decimal(a.chunk_ordinal)
    local count=I.dense(input.records,64)
    if not operations[kind] or ctx.operation~=operations[kind] or ordinal==nil or not count or count==0 or
       P.parse_decimal(a.record_count)~=count or not I.digest(a.chunk_digest) then return nil,"INVALID_ARGUMENT" end
    local limit=(kind=="html" or kind=="original_html") and 5373952 or 524288
    if not plain(ctx.request) or not I.integer(ctx.request.bytes,limit) or ctx.request.bytes==0 then return nil,"LIMIT_EXCEEDED" end
    local maximum=(kind=="outlinks" and 4 or kind=="discoveries" and 2 or 1)
    if ordinal>=maximum then return nil,"LIMIT_EXCEEDED" end
    local single=kind=="page_fields" or kind=="html" or kind=="original_html" or kind=="image_manifest"
    if single and count~=1 then return nil,"INVALID_ARGUMENT" end
    if not single then
        local expected=meta.n["expected_"..kind]
        local size=math.min(64,expected-64*ordinal)
        if count~=size then return nil,"LIMIT_EXCEEDED" end
    end
    local field=digest_field(kind,ordinal)
    local prior=meta.v[field]
    local replay=prior~=""
    if replay and prior~=a.chunk_digest then return nil,"IMMUTABLE_MISMATCH" end
    if not replay and ordinal>0 and meta.v[digest_field(kind,ordinal-1)]=="" then return nil,"STAGE_INVALID" end
    local result={kind=kind,ordinal=ordinal,records={},targets={},digest=a.chunk_digest,digest_field=field,replay=replay,
        digest_trust="client_bulk",commit_id=meta.v.commit_id,publication_id=meta.v.publication_id,
        expires_at_ms=meta.n.expires_at_ms,
        framing={domain="mifolyo:stage-chunk:v2",section="records",ordinal=a.chunk_ordinal},
        data_delta=0,key_delta=0,new_keys={}}
    local b=p.extra.bundle
    local previous
    if kind=="outlinks" or kind=="discoveries" then
        local list=b[kind]; local start=64*ordinal
        if #list<start then return nil,"STAGE_INVALID" end
        if start>0 then previous=kind=="outlinks" and list[start] or list[start].v.job_id end
    end
    for i,raw in ipairs(input.records) do
        local record,code
        if kind=="page_fields" then
            record,code=O.record("page_fields",raw)
            if not record then return nil,code end
            if not page_context(record.v,run,job,meta) then return nil,"STAGE_INVALID" end
            local original=b.page.v.original_html
            if type(original)=="string" and ((record.v.rendered=="true")~=(#original>0)) then return nil,"STAGE_INVALID" end
            result.targets[1]=hash_target(keys.page,record.fields)
        elseif kind=="html" or kind=="original_html" then
            record,code=read_record(raw,{"field_name","field_bytes"},{13,5242880},5373952)
            if not record then return nil,code end
            if record.v.field_name~=kind then return nil,"INVALID_ARGUMENT" end
            local other=b.page.v[kind=="html" and "original_html" or "html"]
            if type(other)=="string" and #other+#record.v.field_bytes>10485760 then return nil,"LIMIT_EXCEEDED" end
            if kind=="original_html" and type(b.page.v.rendered)=="string" and
               ((b.page.v.rendered=="true")~=(#record.v.field_bytes>0)) then return nil,"STAGE_INVALID" end
            result.targets[1]=hash_target(keys.page,{{kind,record.v.field_bytes}})
        elseif kind=="outlinks" then
            record,code=read_record(raw,{"target_url"},{2048},16384)
            if not record then return nil,code end
            local url=record.v.target_url
            if not CJ.URL.check_canonical(url,1) or url==job.v.last_document_target_url or (previous and url<=previous) then return nil,"INVALID_ARGUMENT" end
            if replay and b.outlinks[64*ordinal+i]~=url then return nil,"IMMUTABLE_MISMATCH" end
            previous=url
            record.backlink_key="backlinks:"..url
            result.targets[#result.targets+1]={key=keys.outlinks,kind="set",member=url}
        elseif kind=="discoveries" then
            -- Job.discovery returns SOURCE-order fields; preserve the distinct
            -- nine-field CHUNK order in result.records and source in .source.
            record,code=read_record(raw,discovery_names,{64,2048,16,12,128,32,64,64,64},16384)
            if not record then return nil,code end
            local source; source,code=Job.discovery(raw,run); if not source then return nil,code end
            record.source=source
            local id=source.v.job_id
            if previous and id<=previous then return nil,"INVALID_ARGUMENT" end
            if replay and b.discoveries[64*ordinal+i].v.job_id~=id then return nil,"IMMUTABLE_MISMATCH" end
            previous=id
            record.job_key=ROOT.."run:"..run.run_id..":job:"..id
            local df={}
            for _,k in ipairs(discovery_suffixes) do df[#df+1]={id..":"..k,source.v[k]} end
            result.targets[#result.targets+1]=hash_target(keys.discovery_records,df)
            result.targets[#result.targets+1]=hash_target(keys.discovery_depths,{{id,source.v.depth}})
            result.targets[#result.targets+1]={key=keys.discoveries,kind="zset",member=id,score=source.v.score_text}
        elseif kind=="aliases" then
            record,code=read_record(raw,alias_names,{64,2048,16},16384)
            if not record then return nil,code end
            if not alias(record.v,job.v.depth) or (previous and record.v.url_id<=previous) then return nil,"INVALID_ARGUMENT" end
            previous=record.v.url_id
            result.targets[#result.targets+1]=hash_target(keys.aliases,{{previous..":canonical_url",record.v.canonical_url},{previous..":depth",record.v.depth}})
        elseif kind=="images" then
            record,code=read_record(raw,{"normalized_source_url","alt"},{2048,1024},16384)
            if not record then return nil,code end
            local url=record.v.normalized_source_url
            if not CJ.URL.check_canonical(url,1) or (previous and url<=previous) then return nil,"INVALID_ARGUMENT" end
            previous=url
            local image=S.project("final_image",{contract_version="1",publication_id=meta.v.publication_id,
                normalized_page_url=job.v.last_document_target_url,normalized_source_url=url,alt=record.v.alt})
            if not image then return nil,"INVALID_ARGUMENT" end
            record.output=image; record.final_key=O.keys(meta.v.publication_id,job.v.last_document_target_url,url).image
            result.targets[#result.targets+1]=hash_target(keys.images[i],image.fields)
        else -- closed image_manifest branch, never a permissive unknown-kind fallback
            record,code=O.record("image_manifest",raw)
            if not record then return nil,code end
            local list=O.manifest(record.v)
            if meta.n.images_written~=meta.n.expected_images or record.v.publication_id~=meta.v.publication_id or
               record.v.normalized_url~=job.v.last_document_target_url or record.n.image_count~=meta.n.expected_images or
               #list.keys~=#b.image_keys then return nil,"STAGE_INVALID" end
            for j,key in ipairs(list.keys) do if key~=b.image_keys[j] then return nil,"IMMUTABLE_MISMATCH" end end
            result.targets[1]=hash_target(keys.image_manifest,record.fields)
        end
        result.records[i]=record
    end
    if kind=="aliases" then
        local source,final=false,false
        for _,record in ipairs(result.records) do
            if record.v.url_id==job.v.job_id and record.v.canonical_url==job.v.canonical_url then source=true end
            if record.v.url_id==job.v.last_document_target_url_id and record.v.canonical_url==job.v.last_document_target_url then final=true end
        end
        if not source or not final then return nil,"STAGE_INVALID" end
    end
    -- Every replay byte, including discovery score_text/depth and image alt.
    -- Complete private facts prove absence; numeric/cardinality hints cannot.
    local touched={}
    for _,target in ipairs(result.targets) do
        local f=p.extra.facts[target.key]
        if not f or not keys.kind[target.key] then return nil,"INVALID_STATE" end
        if not touched[target.key] then
            touched[target.key]=true
            if not f.exists then
                result.key_delta=result.key_delta+1; result.data_delta=result.data_delta+#target.key
                result.new_keys[#result.new_keys+1]=target.key
            end
        end
        if target.kind=="hash" then
            for _,pair in ipairs(target.fields) do
                local old=f.v[pair[1]]
                if replay then if old~=pair[2] then return nil,"IMMUTABLE_MISMATCH" end
                else
                    if old~=nil and old~=false then return nil,"IMMUTABLE_MISMATCH" end
                    result.data_delta=result.data_delta+#pair[1]+#pair[2]
                end
            end
        else
            if replay then
                if f.members[target.member]~=true or (target.kind=="zset" and f.scores[target.member]~=I.score(target.score)) then return nil,"IMMUTABLE_MISMATCH" end
            else
                if f.members[target.member]==true then return nil,"IMMUTABLE_MISMATCH" end
                result.data_delta=result.data_delta+#target.member+(target.score and #target.score or 0)
            end
        end
    end
    if replay then
        if result.key_delta~=0 or result.data_delta~=0 then return nil,"STAGE_INVALID" end
        result.next_meta=copy(meta); return result
    end
    local v=copy(meta.v)
    local function inc(k,n) v[k]=decimal(meta.n[k]+n) end
    if kind=="page_fields" then inc("page_fields_written",8)
    elseif kind=="html" or kind=="original_html" then inc("page_fields_written",1); inc(kind.."_written",1)
    elseif kind=="image_manifest" then inc("manifest_written",1)
    else inc(kind.."_written",count) end
    v[field]=a.chunk_digest
    inc("key_count",result.key_delta); inc("data_bytes",result.data_delta)
    local next_meta,code=S.project("stage_meta",v)
    if not next_meta then return nil,code end
    result.next_meta=next_meta
    return result
end

-- Operation support below has explicit reads and inert Plan construction. The
-- validation APIs above still perform no Redis calls. No helper executes a plan;
-- the eleven source fragments have only the reviewed numeric execution tail.
local Run=CJ.Run
local stage_operation_kinds={CJ2_BEGIN_STAGE="begin",CJ2_STAGE_PAGE_FIELDS="chunk",CJ2_STAGE_PAGE_BLOB="chunk",
    CJ2_STAGE_OUTLINKS_BATCH="chunk",CJ2_STAGE_DISCOVERIES_BATCH="chunk",CJ2_STAGE_ALIASES_BATCH="chunk",
    CJ2_STAGE_IMAGES_BATCH="chunk",CJ2_STAGE_IMAGE_MANIFEST="chunk",CJ2_ABORT_STAGE="abort",CJ2_SEAL_STAGE="seal",CJ2_COMMIT="commit"}
local function input_lease(ctx)
    local v=ctx.request.v
    local l={run_id=v.run_id,job_id=v.job_id,owner_id=v.owner_id,lease_token=v.lease_token,fence=v.fence}
    if not lease_valid(l) or not I.digest(v.commit_id) then return nil,"INVALID_IDENTIFIER" end
    return l
end
local function job_key(l) return ROOT.."run:"..l.run_id..":job:"..l.job_id end
local function read_job(ctx,l)
    local key=job_key(l)
    local raw,code=R.dynamic_hash(ctx,key,54,64,2806)
    if not raw then return nil,code end
    if not raw.exists then return raw end
    if raw.count~=54 then return nil,"INVALID_STATE" end
    local B,G=P.parse_decimal(raw.v.lease_request_starts_baseline),P.parse_decimal(raw.v.request_starts)
    if B==nil or G==nil or B>G or B>=10 or G>10 then return nil,"COUNTER_CORRUPT" end
    if (raw.v.state=="leased" and (raw.v.lease_delivery_started=="0" or raw.v.lease_delivery_started=="1") and
        ((raw.v.lease_delivery_started=="1")~=(G>B))) or
       (raw.v.lease_fence=="0" and (B~=0 or G~=0)) or
       (raw.v.last_stage_fence==raw.v.lease_fence and raw.v.lease_fence~="0" and G<=B) then return nil,"COUNTER_CORRUPT" end
    local job=S.project("job",raw.v)
    if not job or job.v.run_id~=l.run_id or job.v.job_id~=l.job_id then return nil,"INVALID_STATE" end
    -- Install the real typed private receipt only after the contextual counter
    -- classification above. Missing fields/extra fields are never repaired.
    return R.fixed_hash(ctx,key,"job")
end
local function select_jobs(ctx,run,ids)
    local count=I.dense(ids,129)
    if not count or count==0 then return nil,"INVALID_ARGUMENT" end
    local leases={}
    for i,id in ipairs(ids) do
        if not I.hex(id,64) then return nil,"INVALID_IDENTIFIER" end
        leases[i]=run.run_id..":"..id
    end
    local view={ctx=ctx}
    for _,def in ipairs({{"jobs","set",10000},{"job_order","zset",10000},{"ready","zset",10000},
        {"ready_at","zset",10000},{"leased","zset",64},{"leased_at","zset",64},{"delayed","zset",10000},
        {"completed","zset",10000},{"dead","zset",10000},{"cancelled","zset",10000},
        {"commit_backpressure","zset",10},{"active_leases","zset",64}}) do
        local global=def[1]=="active_leases"
        local key=global and ROOT.."active_leases" or Run.key(ctx,def[1])
        local fact,code=R.members(ctx,key,def[2],global and leases or ids,def[3],global and 97 or 64)
        if not fact then return nil,code end
        view[def[1]]=fact
    end
    return view
end
local function read_stage(ctx,commit)
    local keys,code=Stage.keys(commit); if not keys then return nil,code end
    local slots; slots,code=R.slots(ctx); if not slots then return nil,code end
    local selected; selected,code=R.hash_fields(ctx,SLOT,{commit},4,64,126); if not selected then return nil,code end
    local ix; ix,code=R.members(ctx,EXPIRY,"zset",{commit},100000,64); if not ix then return nil,code end
    for _,key in ipairs(keys.ordered) do
        local fact
        if key==keys.keys then fact,code=R.page(ctx,key,"list",0,73,73,128)
        elseif key==keys.outlinks then fact,code=R.all_members(ctx,key,"set",256,2048)
        elseif key==keys.discoveries then fact,code=R.all_members(ctx,key,"zset",128,64)
        elseif key==keys.page then
            -- Known field names only; all length checks precede bounded HMGET.
            fact,code=R.hash_fields(ctx,key,nonblob,10,64,2048)
            if fact then fact,code=R.hash_fields(ctx,key,{"html","original_html"},10,64,5242880) end
        elseif key==keys.meta then fact,code=R.dynamic_hash(ctx,key,43,64,64)
        elseif key==keys.discovery_records then fact,code=R.dynamic_hash(ctx,key,896,100,2048)
        elseif key==keys.discovery_depths then fact,code=R.dynamic_hash(ctx,key,128,64,16)
        elseif key==keys.aliases then fact,code=R.dynamic_hash(ctx,key,10,100,2048)
        elseif key==keys.image_manifest then fact,code=R.fixed_hash(ctx,key,"image_manifest")
        else fact,code=R.fixed_hash(ctx,key,"final_image") end
        if not fact then
            if code=="INVALID_STATE" or code=="LIMIT_EXCEEDED" then code="STAGE_INVALID" end
            return nil,code
        end
        local ttl; ttl,code=R.ttl(ctx,key); if not ttl then return nil,code end
    end
    return {ctx=ctx,keys=keys,slots=slots}
end
function Stage.select(ctx,commit) return read_stage(ctx,commit) end
-- Minimal COMMIT replay read set: full retained job plus the explicit run purge
-- field, NOT Run.load, authorization, current lease, output keys or stage keys.
function Stage.committed_receipt(ctx,l,commit)
    if not clock(ctx) or not lease_valid(l) or not I.digest(commit) then return nil,"INVALID_ARGUMENT" end
    local raw,code=R.snapshot(ctx,job_key(l)); if not raw then return nil,code end
    local run; run,code=R.snapshot(ctx,ROOT.."run:"..l.run_id); if not run then return nil,code end
    if not run.exists or run.kind~="hash" or not plain(run.v) or run.v.purge_state~="none" then return nil,"INVALID_STATE" end
    if not raw.exists then return nil,"LEASE_LOST" end
    local j=raw.complete and raw.schema=="job" and S.project("job",raw.v)
    if not j or j.v.run_id~=l.run_id or j.v.job_id~=l.job_id then return nil,"INVALID_STATE" end
    local v=j.v
    if v.state~="completed" or v.last_reason~="published" or v.commit_id~=commit or v.last_stage_commit_id~=commit or
       v.lease_fence~=l.fence or v.last_stage_fence~=l.fence then return nil,"LEASE_LOST" end
    local expected=I.framed("mifolyo:crawl-commit:v2",{l.run_id,l.job_id,l.fence,l.lease_token,v.publication_id,
        v.lease_request_starts_baseline,v.request_starts})
    if expected~=commit then return nil,"LEASE_LOST" end
    if j.n.completed_at_ms>ctx.now_ms then return nil,"INVALID_STATE" end
    return {publication_id=v.publication_id,commit_id=commit,completed_at_ms=v.completed_at_ms}
end
local function scalar_input(ctx,kind)
    local v=ctx.request.v
    if kind=="begin" then
        for _,name in ipairs({"request_starts_baseline","request_starts_generation","expected_page_fields","expected_outlinks",
            "expected_discoveries","expected_aliases","expected_images"}) do
            if P.parse_decimal(v[name])==nil then return nil,"INVALID_NUMBER" end
        end
        local B,G=P.parse_decimal(v.request_starts_baseline),P.parse_decimal(v.request_starts_generation)
        if B>=G or G>10 then return nil,"INVALID_ARGUMENT" end
        if not I.digest(v.publication_id) or not I.digest(v.output_digest) then return nil,"INVALID_IDENTIFIER" end
        if v.expected_page_fields~="10" or P.parse_decimal(v.expected_outlinks)>256 or P.parse_decimal(v.expected_discoveries)>128 or
           P.parse_decimal(v.expected_aliases)<1 or P.parse_decimal(v.expected_aliases)>5 or P.parse_decimal(v.expected_images)>64 then
            return nil,"LIMIT_EXCEEDED"
        end
    elseif kind=="seal" then
        if not I.digest(v.verified_output_digest) or not I.digest(v.verified_manifest_chunk_digest) then return nil,"INVALID_IDENTIFIER" end
    elseif kind=="abort" then
        if not I.digest(v.transition_id) then return nil,"INVALID_IDENTIFIER" end
        if abort_id(v,v.commit_id)~=v.transition_id then return nil,"IMMUTABLE_MISMATCH" end
    elseif kind=="chunk" then
        if operations[v.chunk_kind]~=ctx.operation or not I.digest(v.chunk_digest) then return nil,"INVALID_ARGUMENT" end
        local ordinal,count=P.parse_decimal(v.chunk_ordinal),P.parse_decimal(v.record_count)
        if ordinal==nil or count==nil then return nil,"INVALID_NUMBER" end
        if count<1 or count>64 or ordinal>(v.chunk_kind=="outlinks" and 3 or v.chunk_kind=="discoveries" and 1 or 0) then return nil,"LIMIT_EXCEEDED" end
    end
    return true
end
local function job_changes(plan,job,changes)
    local v=copy(job.v)
    for name,value in next,changes,nil do v[name]=value end
    local post,code=S.project("job",v); if not post then return nil,code end
    local changed={}
    for name,value in next,changes,nil do if job.v[name]~=value then changed[name]=value end end
    local ok; ok,code=Run.hset(plan,job.key,changed); if not ok then return nil,code end
    return post
end
local function meta_changes(plan,key,before,after)
    local changed={}
    for _,pair in ipairs(after.fields) do if before.v[pair[1]]~=pair[2] then changed[pair[1]]=pair[2] end end
    return Run.hset(plan,key,changed)
end
local function finish_assessed(ctx,plan,assessment,status,tail)
    local reply,code=C.Reply.build(ctx,status,tail); if not reply then return nil,code end
    return CJ.Plan.seal(ctx,plan,assessment,reply)
end
local function empty_reply(ctx,status,tail)
    local plan,code=CJ.Plan.new(ctx); if not plan then return nil,code end
    local assessment; assessment,code=CJ.Plan.assess(ctx,plan); if not assessment then return nil,code end
    return finish_assessed(ctx,plan,assessment,status,tail)
end
local function lost(ctx,job)
    return empty_reply(ctx,"LEASE_LOST",{job and job.exists and job.v.lease_fence or "0"})
end
local function authorized_status(ctx,run)
    if run.v.state=="cancelled" then return "RUN_CANCELLED" end
    if run.n.authorization_expires_at_ms<=ctx.now_ms then return "AUTHORIZATION_EXPIRED" end
    if run.v.state~="active" then return nil,"INVALID_STATE" end
    return false
end
local function complete_stage(stage)
    local v,n=stage.v,stage.n
    if n.page_fields_written~=10 or n.html_written~=1 or n.original_html_written~=1 or n.manifest_written~=1 or
       n.outlinks_written~=n.expected_outlinks or n.discoveries_written~=n.expected_discoveries or
       n.aliases_written~=n.expected_aliases or n.images_written~=n.expected_images or n.data_bytes==0 then return nil,"STAGE_INVALID" end
    -- Reuse the exact sealed schema requirements for both first seal and commit.
    local check=copy(v)
    check.sealed="1"
    if check.sealed_at_ms=="0" then check.sealed_at_ms=check.created_at_ms end
    if not S.project("stage_meta",check) then return nil,"STAGE_INVALID" end
    return true
end
local function reply_validator(ctx,status,tail)
    local family=stage_operation_kinds[ctx.operation]
    if not family then return nil,"INVALID_ARGUMENT" end
    if status=="LEASE_LOST" then
        if #tail~=1 or P.parse_decimal(tail[1])==nil then return nil,"INVALID_ARGUMENT" end
        return true
    end
    if status=="AUTHORIZATION_EXPIRED" or status=="RUN_CANCELLED" then
        if family=="abort" or #tail~=0 then return nil,"INVALID_ARGUMENT" end
        return true
    end
    local v=ctx.request.v
    if family=="begin" then
        if status=="STAGE_CAPACITY_BLOCKED" then
            local count=P.parse_decimal(tail[2])
            if #tail~=3 or (tail[1]~="stage_slots_full" and tail[1]~="memory_headroom_low") or not count or count>4 or
               tail[3]~="4" or (tail[1]=="stage_slots_full" and count~=4) then return nil,"INVALID_ARGUMENT" end
            return true
        end
        local expires,remaining=P.parse_decimal(tail[2]),P.parse_decimal(tail[3])
        if #tail~=3 or (status~="STAGE_BEGUN" and status~="EXISTS_IDENTICAL") or tail[1]~=v.commit_id or
           not expires or expires<=ctx.now_ms or expires-ctx.now_ms>900000 or
           (status=="STAGE_BEGUN" and expires-ctx.now_ms~=900000) or not remaining or remaining<65536 or remaining>50331648 then return nil,"INVALID_ARGUMENT" end
    elseif family=="chunk" then
        local ordinal,count,bytes,keys,remaining=P.parse_decimal(tail[3]),P.parse_decimal(tail[4]),P.parse_decimal(tail[5]),P.parse_decimal(tail[6]),P.parse_decimal(tail[7])
        if #tail~=7 or (status~="STAGED" and status~="EXISTS_IDENTICAL") or tail[1]~=v.commit_id or
           tail[2]~=v.chunk_kind or operations[tail[2]]~=ctx.operation or tail[3]~=v.chunk_ordinal or tail[4]~=v.record_count or
           not ordinal or not count or count<1 or count>64 or not bytes or bytes>14680064 or not keys or keys<2 or keys>73 or
           not remaining or remaining<65536 or remaining>50331648 then return nil,"INVALID_ARGUMENT" end
    elseif family=="seal" then
        local bytes,keys=P.parse_decimal(tail[2]),P.parse_decimal(tail[3])
        if #tail~=3 or (status~="SEALED" and status~="EXISTS_IDENTICAL") or tail[1]~=v.commit_id or
           not bytes or bytes==0 or bytes>14680064 or not keys or keys<5 or keys>73 then return nil,"INVALID_ARGUMENT" end
    elseif family=="abort" then
        local count=P.parse_decimal(tail[2])
        if #tail~=2 or (status~="STAGE_ABORTED" and status~="EXISTS_IDENTICAL") or tail[1]~=v.commit_id or
           not count or count<2 or count>73 then return nil,"INVALID_ARGUMENT" end
    elseif status=="COMMITTED" or status=="ALREADY_COMMITTED" then
        local at=P.parse_decimal(tail[3])
        if #tail~=3 or not I.digest(tail[1]) or tail[2]~=v.commit_id or not at or at==0 or at>ctx.now_ms or
           (status=="COMMITTED" and at~=ctx.now_ms) then return nil,"INVALID_ARGUMENT" end
    elseif status=="DOWNSTREAM_BACKPRESSURE" then
        local at,deadline=P.parse_decimal(tail[2]),P.parse_decimal(tail[3])
        if #tail~=3 or (tail[1]~="pages_queue_full" and tail[1]~="memory_headroom_low") or not at or at==0 or at>ctx.now_ms or
           not deadline or deadline==0 or (deadline>at and deadline-at>120000) then return nil,"INVALID_ARGUMENT" end
    else return nil,"INVALID_ARGUMENT" end
    return true
end
for operation in next,stage_operation_kinds,nil do
    local ok,code=C.Reply.register(operation,reply_validator)
    if not ok then return nil,code end
end
-- Core integration seam. Plan owns slot settlement and its exact self-inclusive
-- G solver; these planners NEVER supply a numeric growth or replacement slot.
-- Rev4 Plan.set_policy takes a source-owned mode and an opaque Stage handle;
-- begin independently calls begin_record from its own private checked Job/Run.
-- Plan.assess appends/settles slot descriptors and returns exact .remaining.
local function stage_plan(ctx,mode,handle)
    local plan,code=CJ.Plan.new(ctx); if not plan then return nil,code end
    local modes={stage_begin="begin",stage_data="stage",stage_abort="abort",stage_backpressure="backpressure",commit="commit"}
    if not modes[mode] then return nil,"INVALID_ARGUMENT" end
    if mode=="stage_begin" then handle=nil end
    local ok; ok,code=CJ.Plan.set_policy(plan,modes[mode],handle)
    if not ok then return nil,code end
    return plan
end
local function remaining(assessment)
    local n=assessment.remaining
    if not I.integer(n,50331648) then return nil,"INVALID_STATE" end
    return decimal(n)
end
local function add(plan,argv) return CJ.Plan.add(plan,argv,"ordinary") end
local function run_activity(ctx,plan,run)
    local delta,code=Run.plan_delta(ctx,run); if not delta then return nil,code end
    local ok; ok,code=Run.set(delta,{last_activity_at_ms=ctx.now_text}); if not ok then return nil,code end
    return Run.flush(ctx,plan,delta)
end
local begin_immutable={"run_id","job_id","owner_id","commit_id","publication_id","output_digest","request_starts_baseline",
    "request_starts_generation","expected_page_fields","expected_outlinks","expected_discoveries","expected_aliases","expected_images"}
local function prepare_begin(ctx,run,job,l)
    local a=ctx.request.v
    if job.v.active_stage_commit_id~="" and job.v.active_stage_commit_id~=a.commit_id then return nil,"INVALID_STATE" end
    if job.v.active_stage_commit_id=="" and job.n.last_stage_fence>=P.parse_decimal(l.fence) then return nil,"INVALID_STATE" end
    local view,code=read_stage(ctx,a.commit_id); if not view then return nil,code end
    if job.v.active_stage_commit_id==a.commit_id then
        local raw; raw,code=R.snapshot(ctx,view.keys.meta); if not raw then return nil,code end
        if not raw.exists then return nil,"STAGE_INVALID" end
        -- Input equality BEFORE deriving identities from altered BEGIN fields.
        for _,name in ipairs(begin_immutable) do if raw.v[name]~=a[name] then return nil,"IMMUTABLE_MISMATCH" end end
        if raw.v.lease_fence~=l.fence then return nil,"IMMUTABLE_MISMATCH" end
        local owned; owned,code=Stage.check_owned(view,run,job,l,a.commit_id); if not owned then return nil,code end
        return empty_reply(ctx,"EXISTS_IDENTICAL",{a.commit_id,owned.v.expires_at_ms,decimal(owned.remaining)})
    end
    local prepared; prepared,code=Stage.begin_record(ctx,run,job,l,ctx.request); if not prepared then return nil,code end
    if view.slots.count==4 then return empty_reply(ctx,"STAGE_CAPACITY_BLOCKED",{"stage_slots_full","4","4"}) end
    local plan; plan,code=stage_plan(ctx,"stage_begin",prepared); if not plan then return nil,code end
    local ok; ok,code=Run.hset(plan,prepared.keys.meta,prepared.meta.v); if not ok then return nil,code end
    for _,argv in ipairs({{"LPUSH",prepared.keys.keys,prepared.keys.keys,prepared.keys.meta},
        {"PEXPIREAT",prepared.keys.meta,prepared.meta.v.expires_at_ms},{"PEXPIREAT",prepared.keys.keys,prepared.meta.v.expires_at_ms},
        {"ZADD",EXPIRY,prepared.meta.v.expires_at_ms,a.commit_id}}) do
        ok,code=add(plan,argv); if not ok then return nil,code end
    end
    local post; post,code=job_changes(plan,job,{active_stage_commit_id=a.commit_id,last_stage_commit_id=a.commit_id,
        last_stage_fence=l.fence,updated_at_ms=ctx.now_text}); if not post then return nil,code end
    post,code=run_activity(ctx,plan,run); if not post then return nil,code end
    local assessment; assessment,code=CJ.Plan.assess(ctx,plan)
    if not assessment then
        if code=="MEMORY_HEADROOM_LOW" then return empty_reply(ctx,"STAGE_CAPACITY_BLOCKED",{"memory_headroom_low",decimal(view.slots.count),"4"}) end
        return nil,code
    end
    local r; r,code=remaining(assessment); if not r then return nil,code end
    return finish_assessed(ctx,plan,assessment,"STAGE_BEGUN",{a.commit_id,prepared.meta.v.expires_at_ms,r})
end
local function prepare_chunk(ctx,run,job,l)
    local view,code=read_stage(ctx,ctx.request.v.commit_id); if not view then return nil,code end
    local stage; stage,code=Stage.check_owned(view,run,job,l,ctx.request.v.commit_id); if not stage then return nil,code end
    local prepared; prepared,code=Stage.validate_chunk(ctx,run,job,stage,ctx.request); if not prepared then return nil,code end
    local a=ctx.request.v
    local tail={a.commit_id,a.chunk_kind,a.chunk_ordinal,a.record_count,prepared.next_meta.v.data_bytes,prepared.next_meta.v.key_count,decimal(stage.remaining)}
    if prepared.replay then return empty_reply(ctx,"EXISTS_IDENTICAL",tail) end
    local plan; plan,code=stage_plan(ctx,"stage_data",stage); if not plan then return nil,code end
    -- Coalesce each distinct key once, preserving first-key and canonical record
    -- order. Plan owns bounded command splitting; no record is silently dropped.
    local ordered_calls,by_key={},{}
    for _,target in ipairs(prepared.targets) do
        local argv=by_key[target.key]
        if not argv then
            argv={target.kind=="hash" and "HSET" or target.kind=="set" and "SADD" or "ZADD",target.key}
            by_key[target.key]=argv; ordered_calls[#ordered_calls+1]=argv
        end
        if target.kind=="hash" then for _,pair in ipairs(target.fields) do argv[#argv+1]=pair[1]; argv[#argv+1]=pair[2] end
        elseif target.kind=="zset" then argv[#argv+1]=target.score; argv[#argv+1]=target.member
        else argv[#argv+1]=target.member end
    end
    local ok
    for _,argv in ipairs(ordered_calls) do ok,code=add(plan,argv); if not ok then return nil,code end end
    if #prepared.new_keys>0 then
        local inventory={"LPUSH",stage.keys.keys}
        for _,key in ipairs(prepared.new_keys) do
            inventory[#inventory+1]=key
            ok,code=add(plan,{"PEXPIREAT",key,stage.v.expires_at_ms}); if not ok then return nil,code end
        end
        ok,code=add(plan,inventory); if not ok then return nil,code end
    end
    ok,code=meta_changes(plan,stage.keys.meta,stage,prepared.next_meta); if not ok then return nil,code end
    local assessment; assessment,code=CJ.Plan.assess(ctx,plan); if not assessment then return nil,code end
    tail[7],code=remaining(assessment); if not tail[7] then return nil,code end
    return finish_assessed(ctx,plan,assessment,"STAGED",tail)
end
local function prepare_seal(ctx,run,job,l)
    local a=ctx.request.v
    local view,code=read_stage(ctx,a.commit_id); if not view then return nil,code end
    local stage; stage,code=Stage.check_owned(view,run,job,l,a.commit_id); if not stage then return nil,code end
    if a.verified_output_digest~=stage.v.output_digest or a.verified_manifest_chunk_digest~=stage.v.manifest_chunk_digest then return nil,"STAGE_INVALID" end
    local ok; ok,code=complete_stage(stage); if not ok then return nil,code end
    local tail={a.commit_id,stage.v.data_bytes,stage.v.key_count}
    if stage.v.sealed=="1" then return empty_reply(ctx,"EXISTS_IDENTICAL",tail) end
    local plan; plan,code=stage_plan(ctx,"stage_data",stage); if not plan then return nil,code end
    local v=copy(stage.v); v.sealed,v.sealed_at_ms="1",ctx.now_text
    local post; post,code=S.project("stage_meta",v); if not post then return nil,code end
    ok,code=meta_changes(plan,stage.keys.meta,stage,post); if not ok then return nil,code end
    local assessment; assessment,code=CJ.Plan.assess(ctx,plan); if not assessment then return nil,code end
    return finish_assessed(ctx,plan,assessment,"SEALED",tail)
end
local function prepare_abort(ctx,run,job,l)
    local a=ctx.request.v
    -- A newer transition cannot be mistaken for an abort replay, even when an
    -- old reservation or some residual physical bytes still happen to exist.
    if job.v.active_stage_commit_id=="" and (job.v.last_stage_commit_id~=a.commit_id or job.v.last_stage_fence~=l.fence or
       job.v.last_transition_status~="STAGE_ABORTED" or job.v.last_transition_id~=a.transition_id) then return lost(ctx,job) end
    local view,code=read_stage(ctx,a.commit_id); if not view then return nil,code end
    if job.v.active_stage_commit_id=="" then
        local aborted; aborted,code=Stage.check_aborted(view,run,job,l,a.commit_id)
        if not aborted then if code=="LEASE_LOST" then return lost(ctx,job) end; return nil,code end
        return empty_reply(ctx,"EXISTS_IDENTICAL",{a.commit_id,decimal(aborted.abort_unlinked_keys)})
    end
    local stage; stage,code=Stage.check_owned(view,run,job,l,a.commit_id); if not stage then return nil,code end
    -- Abort does not erase backpressure evidence. The following RETRY owner
    -- enforces the prescribed downstream_backpressure deadline/reason; cancel
    -- and authorization-expiry dispositions must still be able to abort now.
    local plan; plan,code=stage_plan(ctx,"stage_abort",stage); if not plan then return nil,code end
    local inv=R.snapshot(ctx,stage.keys.keys)
    for _,key in ipairs(inv.items) do
        local ok; ok,code=add(plan,{"UNLINK",key}); if not ok then return nil,code end
    end
    local ok; ok,code=add(plan,{"ZREM",EXPIRY,a.commit_id}); if not ok then return nil,code end
    local post; post,code=job_changes(plan,job,{active_stage_commit_id="",last_transition_id=a.transition_id,
        last_transition_status="STAGE_ABORTED",last_reason="none",updated_at_ms=ctx.now_text}); if not post then return nil,code end
    post,code=run_activity(ctx,plan,run); if not post then return nil,code end
    -- Core stage_abort appends the exact remaining:owner:original_count slot
    -- replacement and proves actual abort G + its reviewed post-outcome bound.
    local assessment; assessment,code=CJ.Plan.assess(ctx,plan); if not assessment then return nil,code end
    return finish_assessed(ctx,plan,assessment,"STAGE_ABORTED",{a.commit_id,stage.v.key_count})
end
local function backpressure(ctx,run,job,stage,reason)
    if job.v.commit_backpressure_reason~="none" then
        return empty_reply(ctx,"DOWNSTREAM_BACKPRESSURE",{job.v.commit_backpressure_reason,
            job.v.commit_backpressure_started_at_ms,job.v.commit_backpressure_deadline_ms})
    end
    local deadline=backpressure_deadline(ctx.now_ms,stage.n.expires_at_ms)
    if not I.integer(deadline,MAX) or deadline==0 then return nil,"INVALID_NUMBER" end
    local plan,code=stage_plan(ctx,"stage_backpressure",stage); if not plan then return nil,code end
    local post; post,code=job_changes(plan,job,{commit_backpressure_fence=job.v.lease_fence,commit_backpressure_reason=reason,
        commit_backpressure_started_at_ms=ctx.now_text,commit_backpressure_deadline_ms=decimal(deadline)})
    if not post then return nil,code end
    local ok; ok,code=add(plan,{"ZADD",ctx.keys.run_commit_backpressure,ctx.now_text,job.v.job_id}); if not ok then return nil,code end
    local delta; delta,code=Run.plan_delta(ctx,run); if not delta then return nil,code end
    ok,code=Run.accumulate(delta,{indexes={commit_backpressure=1}}); if not ok then return nil,code end
    post,code=Run.flush(ctx,plan,delta); if not post then return nil,code end
    local assessment; assessment,code=CJ.Plan.assess(ctx,plan); if not assessment then return nil,code end
    return finish_assessed(ctx,plan,assessment,"DOWNSTREAM_BACKPRESSURE",{reason,ctx.now_text,decimal(deadline)})
end
local function prepare_publication(ctx,run,job,stage)
    local b=stage.bundle
    -- Rev4 derives permissions from the private Stage semantic proof, not a
    -- caller-edited key list or public ctx.allowed extension.
    local bound,code=C.bind_commit(ctx,stage); if not bound then return nil,code end
    local function destination(key,kind)
        local actual,err=R.key_type(ctx,key); if not actual then return nil,err end
        if actual~="none" then return nil,actual==kind and "DESTINATION_EXISTS" or "WRONG_TYPE" end
        return R.absent(ctx,key,kind)
    end
    for _,pair in ipairs({{b.final_keys.page,"hash"},{b.final_keys.outlinks,"set"},{b.final_keys.manifest,"hash"}}) do
        local absent; absent,code=destination(pair[1],pair[2]); if not absent then return nil,code end
    end
    for _,key in ipairs(b.image_keys) do
        local absent; absent,code=destination(key,"hash"); if not absent then return nil,code end
    end
    for _,key in ipairs(b.backlink_keys) do
        local member; member,code=R.members(ctx,key,"set",{job.v.last_document_target_url},MAX,2048)
        if not member then return nil,code end
    end
    local fresh,groups,ids={},{},{}
    for i,source in ipairs(b.discoveries) do ids[i]=source.v.job_id end
    if #ids>0 then
        local view; view,code=select_jobs(ctx,run,ids); if not view then return nil,code end
        for i,source in ipairs(b.discoveries) do
            local stored; stored,code=read_job(ctx,{run_id=run.run_id,job_id=source.v.job_id}); if not stored then return nil,code end
            view.job=stored
            local existing; existing,code=Job.check(view,run,source.v.job_id); if not existing then return nil,code end
            if existing.exists then
                if existing.v.canonical_url~=source.v.canonical_url then return nil,"URL_ID_COLLISION" end
                -- First admission wins. In particular, never compare/rewrite
                -- later score/depth/group data as if this were an ENQUEUE replay.
            else
                local initial; initial,code=Job.initial_record(ctx,run,source); if not initial then return nil,code end
                fresh[#fresh+1]={key=b.discovery_job_keys[i],record=initial}
                groups[source.v.group_id]=(groups[source.v.group_id] or 0)+1
            end
        end
    end
    local count=P.safe_add(run.n.job_count,#fresh)
    if not count or count>10000 then return nil,"LIMIT_EXCEEDED" end
    local alias_ids={}
    for i,a in ipairs(b.aliases) do alias_ids[i]=a.v.url_id end
    local depths; depths,code=R.hash_fields(ctx,ctx.keys.run_visited_depth,alias_ids,10000,64,16); if not depths then return nil,code end
    local urls; urls,code=R.hash_fields(ctx,ctx.keys.run_visited_urls,alias_ids,10000,64,2048); if not urls then return nil,code end
    local depth_changes,url_changes,new_aliases={},{},0
    for _,a in ipairs(b.aliases) do
        local id=a.v.url_id
        local old_depth,old_url=depths.v[id],urls.v[id]
        if old_depth==nil or old_url==nil then return nil,"INVALID_STATE" end
        if (old_depth==false)~=(old_url==false) then return nil,"STATE_INDEX_CORRUPT" end
        if old_depth==false then
            new_aliases=new_aliases+1; depth_changes[id],url_changes[id]=a.v.depth,a.v.canonical_url
        else
            if old_url~=a.v.canonical_url then return nil,"URL_ID_COLLISION" end
            local depth=P.parse_decimal(old_depth)
            if depth==nil then return nil,"COUNTER_CORRUPT" end
            if P.parse_decimal(a.v.depth)<depth then depth_changes[id]=a.v.depth end
        end
    end
    if depths.count+new_aliases>10000 or urls.count+new_aliases>10000 then return nil,"LIMIT_EXCEEDED" end
    local queue; queue,code=R.cardinality(ctx,"pages_queue","list",MAX); if not queue then return nil,code end
    return {fresh=fresh,groups=groups,depth_changes=depth_changes,url_changes=url_changes,new_aliases=new_aliases,queue_count=queue.count}
end
local function commit_plan(ctx,run,job,stage,publication)
    local plan,code=stage_plan(ctx,"commit",stage); if not plan then return nil,code end
    local b,keys=stage.bundle,stage.keys
    local ok
    local function move(source,destination)
        local yes,err=add(plan,{"RENAME",source,destination}); if not yes then return nil,err end
        return add(plan,{"PERSIST",destination})
    end
    -- Normative publication order. All descriptors remain inert through the
    -- LAST source/destination/aggregate/memory/reply/ACL validation.
    ok,code=move(keys.page,b.final_keys.page); if not ok then return nil,code end
    if #b.outlinks>0 then ok,code=move(keys.outlinks,b.final_keys.outlinks); if not ok then return nil,code end end
    for i,key in ipairs(b.image_keys) do ok,code=move(keys.images[i],key); if not ok then return nil,code end end
    ok,code=move(keys.image_manifest,b.final_keys.manifest); if not ok then return nil,code end
    for _,key in ipairs(b.backlink_keys) do
        ok,code=add(plan,{"SADD",key,job.v.last_document_target_url}); if not ok then return nil,code end
    end
    for _,item in ipairs(publication.fresh) do
        local v=item.record.v
        ok,code=Run.hset(plan,item.key,v); if not ok then return nil,code end
        for _,argv in ipairs({{"SADD",ctx.keys.run_jobs,v.job_id},{"ZADD",ctx.keys.run_job_order,"0",v.job_id},
            {"ZADD",ctx.keys.run_ready,v.score_text,v.job_id},{"ZADD",ctx.keys.run_ready_at,ctx.now_text,v.job_id}}) do
            ok,code=add(plan,argv); if not ok then return nil,code end
        end
    end
    ok,code=Run.hset(plan,ctx.keys.run_visited_urls,publication.url_changes); if not ok then return nil,code end
    ok,code=Run.hset(plan,ctx.keys.run_visited_depth,publication.depth_changes); if not ok then return nil,code end
    ok,code=add(plan,{"LPUSH","pages_queue",b.final_keys.page}); if not ok then return nil,code end
    local post; post,code=job_changes(plan,job,{state="completed",last_reason="published",lease_owner="",lease_token="",
        lease_started_at_ms="0",lease_expires_at_ms="0",lease_delivery_started="0",active_stage_commit_id="",
        commit_backpressure_fence="0",commit_backpressure_reason="none",commit_backpressure_started_at_ms="0",commit_backpressure_deadline_ms="0",
        output_digest=stage.v.output_digest,publication_id=stage.v.publication_id,commit_id=stage.v.commit_id,published_page_key=b.final_keys.page,
        last_transition_id=stage.v.commit_id,last_transition_status="COMMITTED",completed_at_ms=ctx.now_text,updated_at_ms=ctx.now_text})
    if not post then return nil,code end
    for _,argv in ipairs({{"ZREM",ctx.keys.run_leased,job.v.job_id},{"ZREM",ctx.keys.run_leased_at,job.v.job_id},
        {"ZADD",ctx.keys.run_completed,ctx.now_text,job.v.job_id},{"ZREM",ROOT.."active_leases",run.run_id..":"..job.v.job_id},
        {"ZREM",ctx.keys.run_commit_backpressure,job.v.job_id}}) do
        ok,code=add(plan,argv); if not ok then return nil,code end
    end
    local groups=copy(publication.groups)
    groups[job.v.group_id]=(groups[job.v.group_id] or 0)-1
    local delta; delta,code=Run.plan_delta(ctx,run); if not delta then return nil,code end
    local n=#publication.fresh
    ok,code=Run.accumulate(delta,{run={job_count=n,open_job_count=n-1,completed_total=1,output_commits_total=1},
        maps={group_open_jobs=groups,disposition_reason_counts={published=1}},
        indexes={jobs=n,job_order=n,ready=n,ready_at=n,leased=-1,leased_at=-1,completed=1,
            commit_backpressure=job.n.commit_backpressure_started_at_ms>0 and -1 or 0,
            visited_depth=publication.new_aliases,visited_urls=publication.new_aliases}})
    if not ok then return nil,code end
    ok,code=Run.set(delta,{last_activity_at_ms=ctx.now_text,last_execution_at_ms=ctx.now_text,last_terminal_transition_at_ms=ctx.now_text})
    if not ok then return nil,code end
    post,code=Run.flush(ctx,plan,delta); if not post then return nil,code end
    -- Core appends the slot removal AFTER all descriptors, including the
    -- residual expiry writes. Its commit inequality includes the original S;
    -- no released reservation or deletion is credited against publication G.
    local residual=stage.n.expires_at_ms
    if residual-ctx.now_ms>60000 then residual=P.safe_add(ctx.now_ms,60000) end
    if not residual then return nil,"INVALID_NUMBER" end
    ok,code=add(plan,{"ZADD",EXPIRY,decimal(residual),stage.v.commit_id}); if not ok then return nil,code end
    local unrenamed={keys.meta,keys.keys,keys.aliases}
    if stage.n.discoveries_written>0 then
        unrenamed[#unrenamed+1],unrenamed[#unrenamed+2],unrenamed[#unrenamed+3]=keys.discoveries,keys.discovery_records,keys.discovery_depths
    end
    for _,key in ipairs(unrenamed) do ok,code=add(plan,{"PEXPIREAT",key,decimal(residual)}); if not ok then return nil,code end end
    return plan
end
local function prepare_commit(ctx,run,job,l)
    local a=ctx.request.v
    local view,code=read_stage(ctx,a.commit_id); if not view then return nil,code end
    local stage; stage,code=Stage.check_owned(view,run,job,l,a.commit_id); if not stage then return nil,code end
    if stage.v.sealed~="1" then return nil,"STAGE_UNSEALED" end
    local ok; ok,code=complete_stage(stage); if not ok then return nil,code end
    if job.n.commit_backpressure_started_at_ms>0 and ctx.now_ms>=job.n.commit_backpressure_deadline_ms then
        return backpressure(ctx,run,job,stage,job.v.commit_backpressure_reason)
    end
    local publication; publication,code=prepare_publication(ctx,run,job,stage); if not publication then return nil,code end
    if publication.queue_count>=5000 then return backpressure(ctx,run,job,stage,"pages_queue_full") end
    local plan; plan,code=commit_plan(ctx,run,job,stage,publication); if not plan then return nil,code end
    local assessment; assessment,code=CJ.Plan.assess(ctx,plan)
    if not assessment then
        if code=="MEMORY_HEADROOM_LOW" then return backpressure(ctx,run,job,stage,"memory_headroom_low") end
        return nil,code
    end
    return finish_assessed(ctx,plan,assessment,"COMMITTED",{stage.v.publication_id,a.commit_id,ctx.now_text})
end
-- Source-owned dispatch is closed; no client-selected operation or callback can
-- supply mutation descriptors, policies, reply validators, or derived paths.
function Stage.prepare(operation,wire_keys,wire_args)
    local family=stage_operation_kinds[operation]
    if not family then return nil,"INVALID_ARGUMENT" end
    local spec,code=CJ.Wire.stage_spec(operation); if not spec then return nil,code end
    local ctx; ctx,code=C.open(spec,wire_keys,wire_args); if not ctx then return nil,code end
    local gate; gate,code=CJ.Gate.check(ctx); if not gate then return nil,code end
    local l; l,code=input_lease(ctx); if not l then return nil,code end
    local ok; ok,code=scalar_input(ctx,family); if not ok then return nil,code end
    local job; job,code=read_job(ctx,l); if not job then return nil,code end
    -- Even a retained completed receipt may not cross an in-progress purge.
    local purge; purge,code=R.hash_fields(ctx,ctx.keys.run,{"purge_state"},59,64,11); if not purge then return nil,code end
    if not purge.exists or purge.v.purge_state~="none" then return nil,"INVALID_STATE" end
    if not job.exists then return lost(ctx,job) end
    if family=="commit" and job.v.state=="completed" then
        local receipt; receipt,code=Stage.committed_receipt(ctx,l,ctx.request.v.commit_id)
        if not receipt then if code=="LEASE_LOST" then return lost(ctx,job) end; return nil,code end
        return empty_reply(ctx,"ALREADY_COMMITTED",{receipt.publication_id,receipt.commit_id,receipt.completed_at_ms})
    end
    if job.v.state~="leased" or job.v.lease_owner~=l.owner_id or job.v.lease_token~=l.lease_token or
       job.v.lease_fence~=l.fence or job.n.lease_expires_at_ms<=ctx.now_ms then return lost(ctx,job) end
    local run; run,code=Run.load(ctx,l.run_id); if not run then return nil,code end
    ok,code=Run.mutable(ctx,run); if not ok then return nil,code end
    local rate_inventory; rate_inventory,code=R.cardinality(ctx,ROOT.."rate_scopes","zset",100000)
    if not rate_inventory then return nil,code end
    local view; view,code=select_jobs(ctx,run,{l.job_id}); if not view then return nil,code end
    view.job=job
    job,code=Job.check(view,run,l.job_id); if not job then return nil,code end
    if job.n.updated_at_ms>ctx.now_ms then return nil,"INVALID_STATE" end
    if family~="abort" then
        local status; status,code=authorized_status(ctx,run)
        if status==nil then return nil,code end
        if status then return empty_reply(ctx,status,{}) end
    end
    ok,code=live(ctx,job,l); if not ok then if code=="LEASE_LOST" then return lost(ctx,job) end; return nil,code end
    if family=="begin" then return prepare_begin(ctx,run,job,l)
    elseif family=="chunk" then return prepare_chunk(ctx,run,job,l)
    elseif family=="seal" then return prepare_seal(ctx,run,job,l)
    elseif family=="abort" then return prepare_abort(ctx,run,job,l)
    else return prepare_commit(ctx,run,job,l) end
end
return Stage
