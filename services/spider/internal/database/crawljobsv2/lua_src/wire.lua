local I, S = CJ.Identities, CJ.Schemas
local W = {}
W.authority_names = {"durability", "active_compatibility", "active_contract", "candidate_compatibility",
    "candidate_contract", "commit_guard", "legacy_retirement", "admin_freeze"}
W.authority_keys = {"mifolyo:crawl:v2:durability", "mifolyo:contracts:active", "mifolyo:crawl:v2:contract",
    "mifolyo:contracts:candidate", "mifolyo:crawl:v2:contract:candidate", "mifolyo:crawl:v2:commit_guard",
    "mifolyo:crawl:v2:legacy_retirement", "mifolyo:crawl:v2:admin_freeze"}
W.gate_fields = {"gate_mode", "expected_boot_epoch", "expected_contract_sha256_or_empty",
    "expected_compatibility_record_or_empty", "expected_commit_guard_record_or_empty",
    "expected_legacy_retirement_record_or_empty", "expected_admin_freeze_record_or_empty"}
-- This is the closed gate-mode inventory, NOT an operation implementation set.
W.modes = {
    CJ2_INSTALL_CANDIDATE_MARKERS="boot_only", CJ2_RETIRE_LEGACY_KEYS="candidate",
    CJ2_PROMOTE_CANDIDATE_CONTRACTS="candidate", CJ2_MARK_PLANNED_SHUTDOWN="active",
    CJ2_CREATE_RUN="both", CJ2_ENQUEUE_BATCH="both", CJ2_BEGIN_RUN_AUDIT="both", CJ2_AUDIT_RUN_BATCH="both",
    CJ2_SEAL_RUN="both", CJ2_ACTIVATE_RUN="active", CJ2_REJECT_READY="active", CJ2_TRY_CLAIM="active",
    CJ2_RENEW_LEASE="active", CJ2_RESERVE_REQUEST="active", CJ2_START_REQUEST="active", CJ2_FINISH_REQUEST="active",
    CJ2_CANCEL_RESERVATION="active", CJ2_RELEASE_BEFORE_IO="active", CJ2_RETRY="active", CJ2_DEAD="active",
    CJ2_CANCEL_JOB="active", CJ2_COMPLETE_NO_OUTPUT="active", CJ2_BEGIN_STAGE="active", CJ2_STAGE_PAGE_FIELDS="active",
    CJ2_STAGE_PAGE_BLOB="active", CJ2_STAGE_OUTLINKS_BATCH="active", CJ2_STAGE_DISCOVERIES_BATCH="active",
    CJ2_STAGE_ALIASES_BATCH="active", CJ2_STAGE_IMAGES_BATCH="active", CJ2_STAGE_IMAGE_MANIFEST="active",
    CJ2_ABORT_STAGE="active", CJ2_SEAL_STAGE="active", CJ2_COMMIT="active", CJ2_PROMOTE_DUE="active",
    CJ2_RECOVER_EXPIRED="active", CJ2_CANCEL_RUN="both", CJ2_CANCEL_BATCH="both", CJ2_FINALIZE_RUN="active",
    CJ2_ARCHIVE_RUN="active", CJ2_PURGE_RUN_BATCH="both", CJ2_CLEAN_STAGE="active", CJ2_MAINTAIN_RATE_SCOPES="active"
}
local install_fields = {"freeze_nonce", "process_stop_evidence_sha256", "contract_sha256"}
local compat = S.get("compatibility_marker")
for i = 1, #compat.names do install_fields[#install_fields + 1] = compat.names[i] end
W.install = {operation="CJ2_INSTALL_CANDIDATE_MARKERS", fields=install_fields,
    key_names=W.authority_names, key_values=W.authority_keys, request_limit=2097152, tail="none"}

local run_plans = {
    CJ2_CREATE_RUN="run", CJ2_BEGIN_RUN_AUDIT="run", CJ2_SEAL_RUN="run", CJ2_CANCEL_RUN="run",
    CJ2_ENQUEUE_BATCH="run_records", CJ2_AUDIT_RUN_BATCH="run_records", CJ2_ACTIVATE_RUN="activate",
    CJ2_PROMOTE_DUE="maintenance", CJ2_RECOVER_EXPIRED="maintenance", CJ2_CANCEL_BATCH="maintenance",
    CJ2_FINALIZE_RUN="maintenance", CJ2_ARCHIVE_RUN="archive"
}
local run_suffixes = {"", "jobs", "job_order", "ready", "ready_at", "leased", "leased_at", "delayed",
    "commit_backpressure", "completed", "dead", "cancelled", "group_limits", "group_rate_scope_ids", "group_scope_ids",
    "group_concurrency", "group_interval_ms", "group_started", "group_pending", "group_active_started", "group_open_jobs",
    "audit_group_counts", "retry_reason_counts", "recovery_outcome_counts", "disposition_reason_counts", "visited_depth", "visited_urls"}
local legacy_names = {"legacy_queue","legacy_urls","legacy_depths","legacy_spider_queue","legacy_signal_queue"}
local legacy_keys = {"mifolyo:crawl:v1:queue","mifolyo:crawl:v1:urls","mifolyo:crawl:v1:depths","spider_queue","signal_queue"}
local downstream_names = {"pages_queue","pages_processing","pages_dead","images_queue","images_processing","images_dead","pages_owner","images_owner"}
local downstream_keys = {"pages_queue","pages_queue:processing","pages_queue:dead","image_indexer_queue",
    "image_indexer_queue:processing","image_indexer_queue:dead","pages_queue:indexer_owner","image_indexer_queue:owner"}
local source_fields = {"job_id","canonical_url","score_text","depth","group_id","rate_scope_id","group_scope_id",
    "initial_origin_scope_id","policy_decision_sha256"}
local function words(text)
    local result = {}; for word in string.gmatch(text,"%S+") do result[#result+1] = word end; return result
end
local lease_fields = "run_id job_id owner_id lease_token fence"
local chunk_fields = lease_fields .. " commit_id chunk_kind chunk_ordinal chunk_digest record_count"
local remaining_shapes = {}
local function shape(operation,family,plan,fields,tail,maximum,kinds)
    remaining_shapes[operation] = {family=family,plan=plan,fields=words(fields),record_fields=tail and words(tail),maximum=maximum,kinds=kinds}
end
shape("CJ2_REJECT_READY","worker","job","run_id job_id canonical_url score_text depth group_id rate_scope_id group_scope_id initial_origin_scope_id policy_decision_sha256 reason transition_id")
shape("CJ2_TRY_CLAIM","worker","request_intent",[[run_id job_id canonical_url score_text depth job_group_id job_rate_scope_id job_group_scope_id job_initial_origin_scope_id job_policy_decision_sha256 expected_prior_fence fence owner_id lease_token request_ordinal request_kind target_url_id canonical_target_url target_digest crawl_policy_sha256 policy_decision_sha256 group_id rate_scope_id global_scope_id group_scope_id origin_scope_id global_concurrency global_interval_ms group_concurrency group_interval_ms origin_concurrency origin_interval_ms transition_id]])
shape("CJ2_RENEW_LEASE","worker","job",lease_fields)
shape("CJ2_RESERVE_REQUEST","worker","request_intent",lease_fields .. " request_ordinal request_kind target_url_id canonical_target_url target_digest crawl_policy_sha256 policy_decision_sha256 group_id rate_scope_id global_scope_id group_scope_id origin_scope_id global_concurrency global_interval_ms group_concurrency group_interval_ms origin_concurrency origin_interval_ms")
for _,op in ipairs({"CJ2_START_REQUEST","CJ2_FINISH_REQUEST","CJ2_CANCEL_RESERVATION"}) do shape(op,"worker","request_receipt",lease_fields .. " reservation_id") end
shape("CJ2_RELEASE_BEFORE_IO","worker","job",lease_fields .. " transition_id")
for _,op in ipairs({"CJ2_RETRY","CJ2_DEAD","CJ2_CANCEL_JOB","CJ2_COMPLETE_NO_OUTPUT"}) do shape(op,"worker","job",lease_fields .. " reason transition_id") end
shape("CJ2_BEGIN_STAGE","stage","stage",lease_fields .. " commit_id publication_id output_digest request_starts_baseline request_starts_generation expected_page_fields expected_outlinks expected_discoveries expected_aliases expected_images")
shape("CJ2_STAGE_PAGE_FIELDS","stage","stage",chunk_fields,"normalized_url content_type status_code last_crawled rendered render_policy_rule render_policy_sha256 publication_id",1,{page_fields=true})
shape("CJ2_STAGE_PAGE_BLOB","stage","stage",chunk_fields,"field_name field_bytes",1,{html=true,original_html=true})
shape("CJ2_STAGE_OUTLINKS_BATCH","stage","stage",chunk_fields,"target_url",64,{outlinks=true})
shape("CJ2_STAGE_DISCOVERIES_BATCH","stage","stage",chunk_fields,"job_id canonical_url depth score_text group_id rate_scope_id group_scope_id initial_origin_scope_id policy_decision_sha256",64,{discoveries=true})
shape("CJ2_STAGE_ALIASES_BATCH","stage","stage",chunk_fields,"url_id canonical_url depth",5,{aliases=true})
shape("CJ2_STAGE_IMAGES_BATCH","stage","stage",chunk_fields,"normalized_source_url alt",64,{images=true})
shape("CJ2_STAGE_IMAGE_MANIFEST","stage","stage",chunk_fields,"contract_version publication_id normalized_url image_count image_keys",1,{image_manifest=true})
shape("CJ2_ABORT_STAGE","stage","stage",lease_fields .. " commit_id transition_id")
shape("CJ2_SEAL_STAGE","stage","stage",lease_fields .. " commit_id verified_output_digest verified_manifest_chunk_digest")
shape("CJ2_COMMIT","stage","commit",lease_fields .. " commit_id")
for _,op in ipairs({"CJ2_PROMOTE_DUE","CJ2_RECOVER_EXPIRED","CJ2_CANCEL_BATCH"}) do shape(op,"maintenance","maintenance","run_id") end
shape("CJ2_PURGE_RUN_BATCH","maintenance","purge","run_id evidence_sha256 expected_first_job_id_or_empty")
shape("CJ2_CLEAN_STAGE","maintenance","clean_stage","expected_commit_id expected_cleanup_due_at_ms")
shape("CJ2_MAINTAIN_RATE_SCOPES","maintenance","rate_maintenance","rank_offset")
local function remaining_spec(operation,family)
    local def = type(operation) == "string" and remaining_shapes[operation]
    if not def or def.family ~= family then return nil,"INVALID_ARGUMENT" end
    local spec = {operation=operation,key_plan=def.plan,fields={},tail="none",request_limit=2097152}
    for i,k in ipairs(def.fields) do spec.fields[i] = k end
    if def.record_fields then
        spec.tail,spec.count_field,spec.minimum,spec.maximum = "records","record_count",1,def.maximum
        spec.record_fields = {}; for i,k in ipairs(def.record_fields) do spec.record_fields[i] = k end
        spec.request_limit,spec.record_limit = 524288,524288
        if operation == "CJ2_STAGE_PAGE_BLOB" then
            spec.request_limit,spec.record_limit,spec.binary_records = 5373952,5373952,true
        end
    elseif operation == "CJ2_COMMIT" then spec.request_limit = 65536 end
    return spec
end
function W.worker_spec(operation) return remaining_spec(operation,"worker") end
function W.stage_spec(operation) return remaining_spec(operation,"stage") end
function W.maintenance_spec(operation) return remaining_spec(operation,"maintenance") end
function W.rate_keys(id)
    if not I.digest(id) then return nil,"INVALID_IDENTIFIER" end
    local base = "mifolyo:crawl:v2:rate:" .. id
    return {scope=base,active=base..":active",pending=base..":pending",started=base..":started",ordered={base,base..":active",base..":pending",base..":started"}}
end
function W.stage_keys(id)
    if not I.digest(id) then return nil,"INVALID_IDENTIFIER" end
    local base, result = "mifolyo:crawl:v2:stage:"..id..":", {ordered={},images={},kind={}}
    local suffixes = {"meta","keys","page","outlinks","discoveries","discovery_records","discovery_depths","aliases","image_manifest"}
    local kinds = {"hash","list","hash","set","zset","hash","hash","hash","hash"}
    for i,suffix in ipairs(suffixes) do local key=base..suffix;result[suffix],result.ordered[i],result.kind[key]=key,key,kinds[i] end
    for i=1,64 do local key=base.."image:"..P.format_decimal(i-1);result.images[i],result.ordered[9+i],result.kind[key]=key,key,"hash" end
    return result
end
local reservation_identity_fields = words([[run_id job_id fence lease_token request_ordinal request_kind target_url_id target_digest crawl_policy_sha256 policy_decision_sha256 group_id rate_scope_id global_scope_id group_scope_id origin_scope_id global_concurrency global_interval_ms group_concurrency group_interval_ms origin_concurrency origin_interval_ms]])
function W.request_identity(v)
    if type(v)~="table" or not CJ.URL then return nil,"INVALID_ARGUMENT" end
    for _,k in ipairs({"run_id","job_id","owner_id","lease_token","rate_scope_id"}) do
        if not I.hex(v[k],(k=="job_id" or k=="lease_token") and 64 or 32) then return nil,"INVALID_IDENTIFIER" end
    end
    for _,k in ipairs({"fence","request_ordinal","global_concurrency","global_interval_ms","group_concurrency","group_interval_ms","origin_concurrency","origin_interval_ms"}) do
        if P.parse_decimal(v[k])==nil then return nil,"INVALID_NUMBER" end
    end
    if not I.positive(v.fence) or not I.positive(v.request_ordinal) or not I.group(v.group_id) or
       (v.request_kind~="robots" and v.request_kind~="document" and v.request_kind~="redirect" and v.request_kind~="render_resource") then return nil,"INVALID_ARGUMENT" end
    local url,code=CJ.URL.check_canonical(v.canonical_target_url,1);if not url then return nil,code end
    local origin=CJ.URL.derive_origin(v.canonical_target_url)
    if not origin or v.target_url_id~=url.url_id or v.target_digest~=I.framed("mifolyo:request-target:v2",{url.url_id,v.canonical_target_url}) or
       v.global_scope_id~=I.global_scope() or v.group_scope_id~=I.group_scope(v.rate_scope_id) or
       v.origin_scope_id~=I.framed("mifolyo:rate:origin:v2",{origin}) then return nil,"IMMUTABLE_MISMATCH" end
    for _,k in ipairs({"target_digest","crawl_policy_sha256","policy_decision_sha256"}) do if not I.digest(v[k]) then return nil,"INVALID_IDENTIFIER" end end
    local values={};for i,k in ipairs(reservation_identity_fields) do values[i]=v[k] end
    return I.framed("mifolyo:request-reservation:v2",values)
end
local function remaining_lexical(spec,request)
    local def=remaining_shapes[spec.operation]
    if not def or spec.key_plan~=def.plan or #spec.fields~=#def.fields then return nil,"INVALID_ARGUMENT" end
    for i,k in ipairs(def.fields) do if spec.fields[i]~=k then return nil,"INVALID_ARGUMENT" end end
    local v=request.v
    for _,k in ipairs({"run_id","owner_id","rate_scope_id","job_rate_scope_id"}) do if v[k]~=nil and not I.hex(v[k],32) then return nil,"INVALID_IDENTIFIER" end end
    for _,k in ipairs({"job_id","lease_token"}) do if v[k]~=nil and not I.hex(v[k],64) then return nil,"INVALID_IDENTIFIER" end end
    for _,k in ipairs({"commit_id","expected_commit_id","reservation_id","publication_id","output_digest","transition_id","chunk_digest","verified_output_digest","verified_manifest_chunk_digest","evidence_sha256"}) do
        if v[k]~=nil and not I.digest(v[k]) then return nil,"INVALID_IDENTIFIER" end
    end
    for _,k in ipairs({"fence","expected_prior_fence","request_ordinal","chunk_ordinal","depth","request_starts_baseline","request_starts_generation","expected_page_fields","expected_outlinks","expected_discoveries","expected_aliases","expected_images","expected_cleanup_due_at_ms","rank_offset"}) do
        if v[k]~=nil then local n=P.parse_decimal(v[k]);if n==nil then return nil,"INVALID_NUMBER" end;request.n[k]=n end
    end
    if v.fence~=nil and request.n.fence==0 then return nil,"INVALID_NUMBER" end
    if v.expected_first_job_id_or_empty~=nil and v.expected_first_job_id_or_empty~="" and not I.hex(v.expected_first_job_id_or_empty,64) then return nil,"INVALID_IDENTIFIER" end
    if def.record_fields then
        if spec.tail~="records" or spec.count_field~="record_count" or spec.minimum~=1 or spec.maximum~=def.maximum or I.dense(spec.record_fields,128)~=#def.record_fields then return nil,"INVALID_ARGUMENT" end
        for i,k in ipairs(def.record_fields) do if spec.record_fields[i]~=k then return nil,"INVALID_ARGUMENT" end end
        if not def.kinds[v.chunk_kind] then return nil,"INVALID_ARGUMENT" end
        if spec.operation=="CJ2_STAGE_PAGE_BLOB" then
            if spec.binary_records~=true or request.records[1].v.field_name~=v.chunk_kind then return nil,"INVALID_ARGUMENT" end
            -- Framing is binary; Stage.validate_chunk owns UTF-8/size semantics
            -- of field_bytes. Field name itself is still exact protocol text.
        elseif spec.binary_records then return nil,"INVALID_ARGUMENT" end
    elseif spec.tail~="none" then return nil,"INVALID_ARGUMENT" end
    if spec.operation=="CJ2_TRY_CLAIM" and P.safe_add(request.n.expected_prior_fence,1)~=request.n.fence then return nil,"INVALID_ARGUMENT" end
    return true
end

local admin_shapes = {
    CJ2_RETIRE_LEGACY_KEYS = {plan="retire",fields={"freeze_nonce","backup_sha256","v1_count","v1_url_field_count",
        "v1_depth_field_count","v1_source_sha256","v1_queue_evidence_sha256","v1_urls_evidence_sha256",
        "v1_depths_evidence_sha256","spider_queue_type","spider_queue_count","spider_queue_evidence_sha256",
        "signal_queue_type","signal_queue_count","signal_queue_evidence_sha256","confirmation_text"}},
    CJ2_PROMOTE_CANDIDATE_CONTRACTS = {plan="promote",fields={"freeze_nonce","commit_guard_sha256","protocol_version",
        "contract_sha256","redis_version","redis_config_sha256","maximum_shape_sha256","memory_fixture_sha256",
        "lua_benchmark_sha256","aof_crash_evidence_sha256","cutover_mode","candidate_run_id","approved"}},
    CJ2_MARK_PLANNED_SHUTDOWN = {plan="shutdown",fields={"planned_shutdown_nonce","process_stop_evidence_sha256","active_run_count"}}
}
function W.admin_spec(operation)
    if type(operation) ~= "string" or not admin_shapes[operation] then return nil, "INVALID_ARGUMENT" end
    local def, spec = admin_shapes[operation], {operation=operation,fields={},tail="none",request_limit=2097152}
    spec.key_plan = def.plan
    for i = 1, #def.fields do spec.fields[i] = def.fields[i] end
    if def.plan == "shutdown" then
        spec.tail, spec.count_field, spec.minimum, spec.maximum = "run_ids", "active_run_count", 0, 16
    end
    return spec
end
-- Pure, identifier-checked derivation. Context uses this only after its clock
-- and (for RETIRE's internal binding) complete private inventory receipts.
function W.run_keys(run_id)
    if not I.hex(run_id,32) then return nil, "INVALID_IDENTIFIER" end
    local result, base = {names={},values={}}, "mifolyo:crawl:v2:run:" .. run_id
    for i = 1, #run_suffixes do
        local suffix, key, name = run_suffixes[i], base, "run"
        if i > 1 then key, name = base .. ":" .. suffix, "run_" .. suffix end
        result.names[i], result.values[i] = name, key
    end
    return result
end
local function admin_lexical(spec, request)
    local def = admin_shapes[spec.operation]
    if not def or spec.key_plan ~= def.plan or #spec.fields ~= #def.fields then return nil, "INVALID_ARGUMENT" end
    for i = 1, #def.fields do if spec.fields[i] ~= def.fields[i] then return nil, "INVALID_ARGUMENT" end end
    local v = request.v
    local function digest(value)
        if not I.hex(value,64) then return nil, "INVALID_IDENTIFIER" end
        if not I.digest(value) then return nil, "INVALID_ARGUMENT" end
        return true
    end
    if def.plan == "shutdown" then
        if spec.tail ~= "run_ids" or spec.count_field ~= "active_run_count" or spec.minimum ~= 0 or spec.maximum ~= 16 then return nil, "INVALID_ARGUMENT" end
        if not I.hex(v.planned_shutdown_nonce,32) then return nil, "INVALID_IDENTIFIER" end
        return digest(v.process_stop_evidence_sha256)
    end
    if spec.tail ~= "none" then return nil, "INVALID_ARGUMENT" end
    if not I.hex(v.freeze_nonce,32) then return nil, "INVALID_IDENTIFIER" end
    if def.plan == "retire" then
        for _, field in ipairs({"backup_sha256","v1_source_sha256","v1_queue_evidence_sha256","v1_urls_evidence_sha256",
            "v1_depths_evidence_sha256","spider_queue_evidence_sha256","signal_queue_evidence_sha256"}) do
            local ok, code = digest(v[field]); if not ok then return nil, code end
        end
        for _, field in ipairs({"v1_count","v1_url_field_count","v1_depth_field_count","spider_queue_count","signal_queue_count"}) do
            local number = P.parse_decimal(v[field])
            if number == nil then return nil, "INVALID_NUMBER" end
            request.n[field] = number
        end
        if v.spider_queue_type ~= "none" and v.spider_queue_type ~= "list" and v.spider_queue_type ~= "zset" then return nil, "INVALID_ARGUMENT" end
        if v.signal_queue_type ~= "none" and v.signal_queue_type ~= "list" then return nil, "INVALID_ARGUMENT" end
        -- Counts/observations/confirmation matching remain the RETIRE planner's
        -- responsibility. This is the closed wire/lexical boundary, not deletion.
    else
        local ok, code = digest(v.commit_guard_sha256); if not ok then return nil, code end
        if v.candidate_run_id ~= "" and not I.hex(v.candidate_run_id,32) then return nil, "INVALID_IDENTIFIER" end
        local core, err = S.project("guard_core",v)
        if not core then return nil, err end
    end
    return true
end
local function derive_admin_keys(spec, request)
    local valid, code = admin_lexical(spec,request)
    if not valid then return nil, code end
    local plan = spec.key_plan
    local result = {names={},values={},run_keys={},job_keys={},jobs_by_id={},shutdown_runs={},shutdown_by_id={}}
    local function add(name,value)
        result.names[#result.names+1] = name
        result.values[#result.values+1] = value
    end
    for i = 1, 8 do add(W.authority_names[i],W.authority_keys[i]) end
    if plan == "shutdown" then
        for _, name in ipairs({"active_runs","active_leases","stage_slots","stage_expiry","rate_scopes"}) do add(name,"mifolyo:crawl:v2:" .. name) end
        local scope, err = I.global_scope()
        if not scope then return nil, err end
        result.global_scope_id = scope
        local base = "mifolyo:crawl:v2:rate:" .. scope
        add("global_rate",base); add("global_rate_active",base .. ":active")
        add("global_rate_pending",base .. ":pending"); add("global_rate_started",base .. ":started")
        add("pages_owner","pages_queue:indexer_owner"); add("images_owner","image_indexer_queue:owner")
        for i = 1, #request.repeated do
            local run_id = request.repeated[i]
            -- Tail decoder has checked every ID and the strict byte ordering.
            if not I.hex(run_id,32) then return nil, "INVALID_IDENTIFIER" end
            local run = "mifolyo:crawl:v2:run:" .. run_id
            local pair = {run_id=run_id,run=run,leased=run .. ":leased"}
            add("shutdown_run_" .. P.format_decimal(i),pair.run)
            add("shutdown_leased_" .. P.format_decimal(i),pair.leased)
            result.shutdown_runs[i], result.shutdown_by_id[run_id] = pair, pair
        end
    else
        for _, name in ipairs({"runs","active_runs","unarchived_runs"}) do add(name,"mifolyo:crawl:v2:" .. name) end
        if plan == "promote" then
            for _, name in ipairs({"first_request_start","active_leases","stage_expiry","stage_slots","rate_scopes"}) do add(name,"mifolyo:crawl:v2:" .. name) end
        end
        for i = 1, 5 do add(legacy_names[i],legacy_keys[i]) end
        if plan == "promote" then
            for i = 1, 8 do add(downstream_names[i],downstream_keys[i]) end
            if request.v.candidate_run_id ~= "" then
                local binding, err = W.run_keys(request.v.candidate_run_id)
                if not binding then return nil, err end
                for i = 1, #binding.names do add(binding.names[i],binding.values[i]) end
                result.run_keys, result.bound_run_id = binding.values, request.v.candidate_run_id
            end
        end
    end
    return result
end

function W.run_spec(operation, fields, tail_config)
    if type(operation) ~= "string" or not run_plans[operation] then return nil, "INVALID_ARGUMENT" end
    local count = I.dense(fields,128)
    if not count or count == 0 then return nil, "INVALID_ARGUMENT" end
    local spec = {operation=operation,fields={},key_plan=run_plans[operation],request_limit=2097152,tail="none"}
    local seen = {}
    for i = 1, count do
        if type(fields[i]) ~= "string" or not string.match(fields[i],"^[a-z][a-z0-9_]*$") or seen[fields[i]] then return nil, "INVALID_ARGUMENT" end
        spec.fields[i], seen[fields[i]] = fields[i], true
    end
    if not seen.run_id then return nil, "INVALID_ARGUMENT" end
    if operation == "CJ2_CREATE_RUN" then
        spec.tail, spec.count_field, spec.minimum, spec.maximum = "records", "policy_group_count", 1, 64
        spec.record_fields, spec.record_limit = S.get("policy_group").names, 16384
    elseif run_plans[operation] == "run_records" then
        spec.tail, spec.count_field, spec.record_fields, spec.record_limit = "records", "record_count", {}, 16384
        spec.minimum, spec.maximum = 1, 500
        if operation == "CJ2_AUDIT_RUN_BATCH" then spec.minimum, spec.maximum = 0, 100 end
        for i = 1, #source_fields do spec.record_fields[i] = source_fields[i] end
    end
    -- Optional config is an assertion of the closed inferred framing, not an
    -- escape hatch to enlarge a protocol batch or flatten binary RECORDs.
    if tail_config ~= nil then
        if type(tail_config) ~= "table" or getmetatable(tail_config) ~= nil then return nil, "INVALID_ARGUMENT" end
        for k, v in next, tail_config, nil do
            if k == "record_fields" then
                if not spec.record_fields or I.dense(v,128) ~= #spec.record_fields then return nil, "INVALID_ARGUMENT" end
                for i = 1, #spec.record_fields do if v[i] ~= spec.record_fields[i] then return nil, "INVALID_ARGUMENT" end end
            elseif k ~= "tail" and k ~= "count_field" and k ~= "minimum" and k ~= "maximum" and k ~= "record_limit" then
                return nil, "INVALID_ARGUMENT"
            elseif spec[k] ~= v then return nil, "INVALID_ARGUMENT" end
        end
    end
    return spec
end

local function derive_run_keys(spec, request)
    local plan = spec.key_plan
    if run_plans[spec.operation] ~= plan then return nil, "INVALID_ARGUMENT" end
    local run_id = request.v.run_id
    if not I.hex(run_id,32) then return nil, "INVALID_IDENTIFIER" end
    local names, values, run_keys, job_keys, jobs_by_id = {}, {}, {}, {}, {}
    local function add(name, value) names[#names+1], values[#values+1] = name, value end
    for i = 1, 8 do add(W.authority_names[i],W.authority_keys[i]) end
    for _, name in ipairs({"runs","active_runs","unarchived_runs"}) do add(name,"mifolyo:crawl:v2:" .. name) end
    if plan == "maintenance" or plan == "archive" then
        for _, name in ipairs({"active_leases","stage_expiry","stage_slots"}) do add(name,"mifolyo:crawl:v2:" .. name) end
        if plan == "maintenance" then add("rate_scopes","mifolyo:crawl:v2:rate_scopes") end
    end
    local base = "mifolyo:crawl:v2:run:" .. run_id
    for i = 1, #run_suffixes do
        local suffix, key, name = run_suffixes[i], base, "run"
        if i > 1 then key, name = base .. ":" .. suffix, "run_" .. suffix end
        add(name,key); run_keys[i] = key
    end
    if plan == "activate" then for i = 1, 5 do add(legacy_names[i],legacy_keys[i]) end end
    if plan == "archive" then for i = 1, 8 do add(downstream_names[i],downstream_keys[i]) end end
    if plan == "run_records" then
        local minimum, maximum = 1, 500
        if spec.operation == "CJ2_AUDIT_RUN_BATCH" then minimum, maximum = 0, 100 end
        if spec.tail ~= "records" or spec.count_field ~= "record_count" or spec.minimum ~= minimum or spec.maximum ~= maximum or
           I.dense(spec.record_fields,9) ~= 9 then return nil, "INVALID_ARGUMENT" end
        for i = 1, 9 do if spec.record_fields[i] ~= source_fields[i] then return nil, "INVALID_ARGUMENT" end end
        local previous = nil
        for i = 1, #request.records do
            local id = request.records[i].v.job_id
            if not I.hex(id,64) then return nil, "INVALID_IDENTIFIER" end
            if previous and id <= previous then return nil, "INVALID_ARGUMENT" end
            local key = base .. ":job:" .. id
            add("record_job_" .. P.format_decimal(i),key)
            job_keys[i], jobs_by_id[id], previous = key, key, id
        end
    end
    return {names=names,values=values,run_keys=run_keys,job_keys=job_keys,jobs_by_id=jobs_by_id}
end

local function derive_remaining_keys(spec,request,keys)
    local ok,code=remaining_lexical(spec,request);if not ok then return nil,code end
    local plan,v=spec.key_plan,request.v
    local d={names={},values={},run_keys={},job_keys={},jobs_by_id={}}
    local function add(name,key) d.names[#d.names+1]=name;d.values[#d.values+1]=key end
    for i=1,8 do add(W.authority_names[i],W.authority_keys[i]) end
    local function globals(list) for _,name in ipairs(list) do add(name,"mifolyo:crawl:v2:"..name) end end
    if plan=="clean_stage" then globals({"stage_expiry","stage_slots"})
    elseif plan=="rate_maintenance" then globals({"rate_scopes"})
    else
        globals({"runs","active_runs","unarchived_runs"})
        if plan=="maintenance" then globals({"active_leases","stage_expiry","stage_slots","rate_scopes"})
        elseif plan=="purge" then globals({"first_request_start","active_leases","stage_expiry","stage_slots"})
        else globals({"first_request_start","active_leases","stage_expiry","stage_slots","rate_scopes"}) end
        local run,err=W.run_keys(v.run_id);if not run then return nil,err end
        for i,name in ipairs(run.names) do add(name,run.values[i]) end
        d.run_keys=run.values
        if plan=="purge" then
            if v.expected_first_job_id_or_empty~="" then add("job",run.values[1]..":job:"..v.expected_first_job_id_or_empty) end
        elseif plan~="maintenance" then add("job",run.values[1]..":job:"..v.job_id) end
    end
    if plan=="request_intent" or plan=="request_receipt" then
        local id,err=v.reservation_id,nil
        if plan=="request_intent" then id,err=W.request_identity(v) end
        if not id then return nil,err end
        d.reservation_id=id;add("reservation","mifolyo:crawl:v2:reservation:"..id)
        if plan=="request_receipt" then
            if #keys~=57 then return nil,"INVALID_ARGUMENT" end
            d.pending_request={};for i=46,57 do d.pending_request[i-45]=keys[i] end
        else
            d.scopes={}
            for _,kind in ipairs({"global","group","origin"}) do
                local scope,why=W.rate_keys(v[kind.."_scope_id"]);if not scope then return nil,why end
                d.scopes[kind]=scope
                add(kind.."_rate",scope.scope);add(kind.."_rate_active",scope.active)
                add(kind.."_rate_pending",scope.pending);add(kind.."_rate_started",scope.started)
            end
        end
    elseif plan=="stage" or plan=="commit" or plan=="clean_stage" then
        local stage,err=W.stage_keys(plan=="clean_stage" and v.expected_commit_id or v.commit_id);if not stage then return nil,err end
        d.stage=stage
        for _,name in ipairs({"meta","keys","page","outlinks","discoveries","discovery_records","discovery_depths","aliases","image_manifest"}) do add("stage_"..name,stage[name]) end
        for i=1,64 do add("stage_image_"..P.format_decimal(i-1),stage.images[i]) end
        if plan=="commit" then add("pages_queue","pages_queue") end
    end
    return d
end

function W.record(bytes, names, maximum, text_values)
    -- Bare binary RECORD projection for future handlers: no semantic claim.
    local fields = P.decode_record(bytes, names, maximum, text_values)
    if not fields then return nil, "INVALID_ARGUMENT" end
    local v = {}
    for i = 1, #fields do v[fields[i][1]] = fields[i][2] end
    return {fields=fields, v=v}
end
function W.gate(operation, args)
    local count = I.dense(args,1024)
    if not count or count < 7 then return nil, "INVALID_ARGUMENT" end
    for i = 1, 7 do
        if type(args[i]) ~= "string" or #args[i] > 16384 then return nil, "INVALID_ARGUMENT" end
    end
    local mode = args[1]
    local allowed = W.modes[operation]
    if not allowed or (allowed ~= mode and not (allowed == "both" and (mode == "active" or mode == "candidate"))) then
        return nil, "INVALID_ARGUMENT"
    end
    if not I.hex(args[2],32) then return nil, "INVALID_IDENTIFIER" end
    local g = {mode=mode, boot_epoch=args[2], contract=args[3], records={}}
    if mode == "boot_only" then
        for i = 3, 7 do if args[i] ~= "" then return nil, "INVALID_ARGUMENT" end end
        return g
    end
    if not I.digest(args[3]) then return nil, "INVALID_ARGUMENT" end
    local schemas = {"compatibility_marker", "commit_guard", "legacy_retirement", "admin_freeze"}
    for i = 1, 4 do
        local bytes = args[i + 3]
        if bytes == "" then g.records[schemas[i]] = false
        else
            local projection, code = S.decode(schemas[i], bytes)
            if not projection then return nil, code end
            g.records[schemas[i]] = projection
        end
    end
    local marker, guard, legacy, freeze = g.records.compatibility_marker, g.records.commit_guard,
        g.records.legacy_retirement, g.records.admin_freeze
    if not marker then return nil, "COMPATIBILITY_MISMATCH" end
    if mode == "candidate" then
        if guard or not freeze then return nil, "ADMIN_FREEZE_REQUIRED" end
        if freeze.v.candidate_manifest_sha256 ~= marker.v.manifest_sha256 or
           freeze.v.candidate_contract_sha256 ~= g.contract then return nil, "IMMUTABLE_MISMATCH" end
        if legacy then
            if (operation ~= "CJ2_RETIRE_LEGACY_KEYS" and operation ~= "CJ2_PROMOTE_CANDIDATE_CONTRACTS") or
               legacy.v.freeze_nonce ~= freeze.v.freeze_nonce then return nil, "INVALID_ARGUMENT" end
        elseif operation == "CJ2_PROMOTE_CANDIDATE_CONTRACTS" then return nil, "INVALID_ARGUMENT" end
    else
        if not guard or not legacy or freeze then return nil, "INVALID_ARGUMENT" end
        local core, code = S.project("guard_core", guard.v)
        if not core then return nil, code end
        local encoded = S.encode(core)
        if not encoded then return nil, "INVALID_STATE" end
        local digest = P.sha256(encoded)
        if not digest then return nil, "INVALID_STATE" end
        if guard.v.contract_sha256 ~= g.contract then return nil, "CONTRACT_MISMATCH" end
        if guard.v.compatibility_manifest_sha256 ~= marker.v.manifest_sha256 or
           guard.v.redis_config_sha256 ~= marker.v.redis_config_sha256 or digest ~= marker.v.commit_guard_sha256 then
            return nil, "COMPATIBILITY_MISMATCH"
        end
        if (guard.v.cutover_mode == "fresh" and legacy.n.v1_count ~= 0) or
           (guard.v.cutover_mode == "v1_migration" and legacy.n.v1_count == 0) then return nil, "INVALID_ARGUMENT" end
    end
    return g
end
local function bulk_size(s)
    local length = P.format_decimal(#s)
    return #s + #length + 5
end
function W.request_size(keys, args)
    local nk, na = I.dense(keys, 1024), I.dense(args, 1024)
    if not nk or not na then return nil, "INVALID_ARGUMENT" end
    local count, key_count = P.format_decimal(nk + na + 3), P.format_decimal(nk)
    local size = #count + 3 + bulk_size("EVALSHA") + bulk_size(string.rep("a",40)) + bulk_size(key_count)
    for _, values in ipairs({keys, args}) do
        for i = 1, #values do
            if type(values[i]) ~= "string" then return nil, "INVALID_ARGUMENT" end
            if #values[i] > 5373952 then return nil, "COMMAND_BOUNDS_EXCEEDED" end
            size = size + bulk_size(values[i])
        end
    end
    return size
end
-- spec is source-owned, never supplied in ARGV. Structural framing only;
-- each operation must perform its explicit semantic validation before planning.
function W.decode(spec, keys, args)
    if type(spec) ~= "table" or type(spec.operation) ~= "string" or not W.modes[spec.operation] then return nil, "INVALID_ARGUMENT" end
    local nf = I.dense(spec.fields,128)
    local actual_keys, actual_args = I.dense(keys,1024), I.dense(args,1024)
    if not nf or not actual_keys or not actual_args or actual_args < 7 + nf then return nil, "INVALID_ARGUMENT" end
    if spec.tail ~= "none" and spec.tail ~= "records" and spec.tail ~= "run_ids" then return nil, "INVALID_ARGUMENT" end
    if spec.tail == "none" and actual_args ~= 7 + nf then return nil, "INVALID_ARGUMENT" end
    local limit = 2097152
    if spec.operation == "CJ2_STAGE_PAGE_BLOB" then limit = 5373952
    elseif spec.operation == "CJ2_COMMIT" then limit = 65536
    elseif spec.operation == "CJ2_STAGE_PAGE_FIELDS" or spec.operation == "CJ2_STAGE_OUTLINKS_BATCH" or
       spec.operation == "CJ2_STAGE_DISCOVERIES_BATCH" or spec.operation == "CJ2_STAGE_ALIASES_BATCH" or
       spec.operation == "CJ2_STAGE_IMAGES_BATCH" or spec.operation == "CJ2_STAGE_IMAGE_MANIFEST" then limit = 524288 end
    if spec.request_limit ~= limit then return nil, "INVALID_ARGUMENT" end
    local size, code = W.request_size(keys, args)
    if not size then return nil, code end
    if size > spec.request_limit then return nil, "COMMAND_BOUNDS_EXCEEDED" end
    local gate, err = W.gate(spec.operation,args)
    if not gate then return nil, err end
    local request = {v={}, n={}, records={}, repeated={}, gate=gate, bytes=size}
    for i = 1, nf do
        local name, value = spec.fields[i], args[7+i]
        if type(name) ~= "string" or request.v[name] ~= nil or P.validate_text(value) ~= true then return nil, "INVALID_ARGUMENT" end
        request.v[name] = value
    end
    if spec.tail ~= "none" then
        if type(spec.count_field) ~= "string" or not I.integer(spec.minimum,10000) or not I.integer(spec.maximum,10000) or
           spec.minimum > spec.maximum then return nil, "INVALID_ARGUMENT" end
        if spec.tail == "records" and (not I.dense(spec.record_fields,128) or
           not I.integer(spec.record_limit,5373952)) then return nil, "INVALID_ARGUMENT" end
        local count = P.parse_decimal(request.v[spec.count_field])
        if not count then return nil, "INVALID_NUMBER" end
        if count < spec.minimum or count > spec.maximum then return nil, "LIMIT_EXCEEDED" end
        if actual_args ~= 7 + nf + count then return nil, "INVALID_ARGUMENT" end
        request.n[spec.count_field] = count
        for i = 1, count do
            local value = args[7+nf+i]
            if spec.tail == "records" then
                local binary = spec.operation=="CJ2_STAGE_PAGE_BLOB" and spec.binary_records==true
                local record, failure = W.record(value,spec.record_fields,spec.record_limit,not binary)
                if not record then return nil, failure end
                request.records[i] = record
            else
                if not I.hex(value,32) or (i > 1 and value <= request.repeated[i-1]) then return nil, "INVALID_ARGUMENT" end
                request.repeated[i] = value
            end
        end
    end
    -- All key derivation happens after TIME (Context.open), complete RESP size
    -- admission and scalar/record lexical decoding. Never concatenate bad IDs.
    local key_names, key_values, derived = spec.key_names, spec.key_values, nil
    if spec.key_plan ~= nil then
        if key_names ~= nil or key_values ~= nil then return nil, "INVALID_ARGUMENT" end
        local err
        if admin_shapes[spec.operation] then derived, err = derive_admin_keys(spec,request)
        elseif remaining_shapes[spec.operation] then derived, err = derive_remaining_keys(spec,request,keys)
        else derived, err = derive_run_keys(spec,request) end
        if not derived then return nil, err end
        key_names, key_values = derived.names, derived.values
    end
    local nk, nv = I.dense(key_names,1024), I.dense(key_values,1024)
    local expected_keys = derived and derived.pending_request and 57 or nk
    if not nk or nk ~= nv or actual_keys ~= expected_keys then return nil, "INVALID_ARGUMENT" end
    local named, allowed = {}, {}
    for i = 1, nk do
        if type(key_names[i]) ~= "string" or named[key_names[i]] or keys[i] ~= key_values[i] then return nil, "INVALID_ARGUMENT" end
        named[key_names[i]], allowed[keys[i]] = keys[i], true
    end
    if derived then
        named.run_keys, named.job_keys, named.jobs_by_id = derived.run_keys, derived.job_keys, derived.jobs_by_id
        if derived.shutdown_runs then named.shutdown_runs, named.shutdown_by_id = derived.shutdown_runs, derived.shutdown_by_id end
        if derived.stage then named.stage,named.stage_images = derived.stage,derived.stage.images end
        if derived.scopes then named.scopes = derived.scopes end
    end
    return {request=request, keys=named, allowed=allowed,
        bound_run_id=derived and derived.bound_run_id or nil,global_scope_id=derived and derived.global_scope_id or nil,
        pending_request=derived and derived.pending_request or nil,reservation_id=derived and derived.reservation_id or nil}
end
return W
