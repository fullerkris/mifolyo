-- Complete validators ONLY for the authority schemas below and policy groups.
-- Run/job/reservation/rate/stage/output semantic validators are NOT supplied.
local I = CJ.Identities
local S = {}
local registered = {}
local registration_open = true
local extensible = {run=true, job=true, reservation=true, rate_scope=true, stage_meta=true,
    final_page=true, final_image=true, image_manifest=true}
local definitions = {
    compatibility_marker = {
        names = {"manifest_version", "manifest_sha256", "crawl_jobs", "crawl_policy", "canonicalization",
            "page_publication", "image_manifest", "backlink_projection", "render_ipc", "signal_queue",
            "global_request_concurrency", "redis_config_sha256", "commit_guard_sha256", "spider_image",
            "seed_importer_image", "crawl_admin_image", "indexer_image", "image_indexer_image",
            "backlinks_processor_image", "monitoring_image", "render_worker_image"},
        bounds = {1,64,1,1,1,1,1,1,1,7,1,64,64,71,71,71,71,71,71,71,71}
    },
    durability = {
        names = {"schema_version", "boot_state", "approved_redis_run_id", "boot_epoch", "approved_at_ms",
            "planned_shutdown_nonce", "planned_shutdown_evidence_sha256", "last_approval_mode",
            "consumed_planned_shutdown_nonce", "rehearsal_evidence_sha256", "rehearsal_at_ms", "acknowledged_loss_bound"},
        bounds = {1,10,40,32,16,32,64,17,32,64,16,1}
    },
    admin_freeze = {
        names = {"protocol_version", "freeze_nonce", "process_stop_evidence_sha256",
            "candidate_manifest_sha256", "candidate_contract_sha256", "created_at_ms"},
        bounds = {1,32,64,64,64,16}
    },
    guard_core = {
        names = {"protocol_version", "contract_sha256", "redis_version", "redis_config_sha256", "maximum_shape_sha256",
            "memory_fixture_sha256", "lua_benchmark_sha256", "aof_crash_evidence_sha256", "cutover_mode", "candidate_run_id", "approved"},
        bounds = {1,64,64,64,64,64,64,64,12,32,1}
    },
    commit_guard = {
        names = {"protocol_version", "contract_sha256", "compatibility_manifest_sha256", "redis_version",
            "redis_config_sha256", "maximum_shape_sha256", "memory_fixture_sha256", "lua_benchmark_sha256",
            "aof_crash_evidence_sha256", "cutover_mode", "candidate_run_id", "approved_at_ms", "approved"},
        bounds = {1,64,64,64,64,64,64,64,64,12,32,16,1}
    },
    legacy_retirement = {
        names = {"protocol_version", "freeze_nonce", "backup_sha256", "v1_count", "v1_url_field_count", "v1_depth_field_count",
            "v1_source_sha256", "v1_queue_evidence_sha256", "v1_urls_evidence_sha256", "v1_depths_evidence_sha256",
            "spider_queue_type", "spider_queue_count", "spider_queue_evidence_sha256", "signal_queue_type",
            "signal_queue_count", "signal_queue_evidence_sha256", "deleted_bitmap", "retired_at_ms"},
        bounds = {1,32,64,5,5,5,64,64,64,64,4,16,64,4,16,64,5,16}
    },
    first_request_start = {
        names = {"protocol_version", "run_id", "job_id", "lease_fence", "started_at_ms"},
        bounds = {1,32,64,16,16}
    },
    policy_group = {
        names = {"group_id", "rate_scope_id", "group_scope_id", "request_start_limit", "concurrency", "interval_ms"},
        bounds = {128,32,64,2,2,7}
    }
}
local artifact = {names = {}, bounds = {}}
for i = 1, #definitions.compatibility_marker.names do
    if i ~= 2 then
        artifact.names[#artifact.names + 1] = definitions.compatibility_marker.names[i]
        artifact.bounds[#artifact.bounds + 1] = definitions.compatibility_marker.bounds[i]
    end
end
definitions.compatibility_artifact = artifact

-- Called only by statically assembled, trusted ledger/output module chunks,
-- before Context.open. Validators are pure (v,n) -> true or nil/closed code.
function S.register(name, def, validator)
    if not registration_open then return nil, "INVALID_STATE" end
    if type(name) ~= "string" or not extensible[name] or definitions[name] or type(def) ~= "table" or
       getmetatable(def) ~= nil or type(validator) ~= "function" then return nil, "INVALID_ARGUMENT" end
    local count = I.dense(def.names,128)
    if not count or count == 0 or I.dense(def.bounds,128) ~= count then return nil, "INVALID_ARGUMENT" end
    local names, bounds, seen, maximum = {}, {}, {}, 8
    for i = 1, count do
        local field, bound = def.names[i], def.bounds[i]
        if type(field) ~= "string" or #field == 0 or #field > 128 or not string.match(field,"^[a-z][a-z0-9_]*$") or
           seen[field] or not I.integer(bound,10485760) then return nil, "INVALID_ARGUMENT" end
        seen[field], names[i], bounds[i] = true, field, bound
        maximum = maximum + 16 + #field + bound
        if maximum > P.limits.max_bytes then return nil, "LIMIT_EXCEEDED" end
    end
    definitions[name], registered[name] = {names=names,bounds=bounds,maximum=maximum}, validator
    return true
end
function S.finish_registration()
    registration_open = false
    return true
end

function S.get(name)
    if type(name) ~= "string" or not definitions[name] then return nil, "INVALID_ARGUMENT" end
    local def, result = definitions[name], {names={},bounds={}}
    result.maximum = def.maximum or 16384
    for i = 1, #def.names do result.names[i], result.bounds[i] = def.names[i], def.bounds[i] end
    return result
end
local function fields_for(def, values)
    local fields = {}
    for i = 1, #def.names do fields[i] = {def.names[i], values[def.names[i]]} end
    return fields
end
local function valid_compat(v)
    local constants = {manifest_version="1", crawl_jobs="2", crawl_policy="2", canonicalization="1",
        page_publication="1", image_manifest="1", backlink_projection="1", render_ipc="2", signal_queue="retired",
        global_request_concurrency="2"}
    for k, value in next, constants, nil do if v[k] ~= value then return false end end
    if not I.digest(v.redis_config_sha256) or not I.digest(v.commit_guard_sha256) then return false end
    for _, k in ipairs({"spider_image", "seed_importer_image", "crawl_admin_image", "indexer_image",
        "image_indexer_image", "backlinks_processor_image", "monitoring_image"}) do
        if not I.image(v[k]) then return false end
    end
    return v.render_worker_image == "disabled" or I.image(v.render_worker_image)
end
local function valid_guard(v)
    if v.protocol_version ~= "2" or v.approved ~= "1" or #v.redis_version > 64 or
       string.sub(v.redis_version, 1, 2) ~= "7." then return false end
    local dots = 0
    for part in string.gmatch(v.redis_version, "[^.]+") do
        if not string.match(part, "^[0-9]+$") then return false end
        dots = dots + 1
    end
    if dots < 2 or dots > 4 or string.find(v.redis_version, "..", 1, true) or
       string.sub(v.redis_version, -1) == "." then return false end
    for _, k in ipairs({"contract_sha256", "redis_config_sha256", "maximum_shape_sha256", "memory_fixture_sha256",
        "lua_benchmark_sha256", "aof_crash_evidence_sha256"}) do
        if not I.digest(v[k]) then return false end
    end
    return (v.cutover_mode == "fresh" and v.candidate_run_id == "") or
        (v.cutover_mode == "v1_migration" and I.hex(v.candidate_run_id, 32))
end
local function valid_durability(v)
    if v.schema_version ~= "1" or v.acknowledged_loss_bound ~= "0" or not I.hex(v.approved_redis_run_id,40) or
       not I.hex(v.boot_epoch,32) or not I.positive(v.approved_at_ms) or not I.positive(v.rehearsal_at_ms) or
       not I.digest(v.rehearsal_evidence_sha256) then return false end
    local mode = v.last_approval_mode
    if mode ~= "initial" and mode ~= "planned" and mode ~= "unclean_rehearsal" then return false end
    local pending, evidence, consumed = v.planned_shutdown_nonce, v.planned_shutdown_evidence_sha256, v.consumed_planned_shutdown_nonce
    if (pending ~= "" and not I.hex(pending,32)) or (evidence ~= "" and not I.digest(evidence)) or
       (consumed ~= "" and not I.hex(consumed,32)) then return false end
    if v.boot_state == "planned" then return pending ~= "" and evidence ~= "" and consumed == "" end
    if v.boot_state == "approved" and mode == "planned" then return pending == "" and evidence ~= "" and consumed ~= "" end
    if v.boot_state == "approved" or v.boot_state == "unapproved" then return pending == "" and evidence == "" and consumed == "" end
    return false
end
local function valid_legacy(v)
    if v.protocol_version ~= "2" or not I.hex(v.freeze_nonce,32) or not I.positive(v.retired_at_ms) then return false end
    for _, k in ipairs({"backup_sha256", "v1_source_sha256", "v1_queue_evidence_sha256", "v1_urls_evidence_sha256",
        "v1_depths_evidence_sha256", "spider_queue_evidence_sha256", "signal_queue_evidence_sha256"}) do
        if not I.digest(v[k]) then return false end
    end
    local count, urls, depths = P.parse_decimal(v.v1_count), P.parse_decimal(v.v1_url_field_count), P.parse_decimal(v.v1_depth_field_count)
    local spider, signal = P.parse_decimal(v.spider_queue_count), P.parse_decimal(v.signal_queue_count)
    if not count or not urls or not depths or not spider or not signal or count > 10000 or urls > 20000 or
       depths > 20000 or urls < count or depths < count or spider > 10000 or signal > 10000 then return false end
    local function legacy_type(kind, n, allow_zset)
        return (kind == "none" and n == 0) or ((kind == "list" or (allow_zset and kind == "zset")) and n > 0)
    end
    if not legacy_type(v.spider_queue_type, spider, true) or not legacy_type(v.signal_queue_type, signal, false) then return false end
    local bits = {}
    for i, present in ipairs({count > 0, urls > 0, depths > 0, v.spider_queue_type ~= "none", v.signal_queue_type ~= "none"}) do
        bits[i] = present and "1" or "0"
    end
    return v.deleted_bitmap == table.concat(bits)
end
-- A values map may contain operation-specific extra scalars. The returned
-- projection contains ONLY the complete schema's fields and parsed numbers.
function S.project(name, values)
    local def = S.get(name)
    if not def or type(values) ~= "table" or getmetatable(values) ~= nil then return nil, "INVALID_ARGUMENT" end
    local v, n = {}, {}
    for i = 1, #def.names do
        local k, value = def.names[i], values[def.names[i]]
        if type(value) ~= "string" or #value > def.bounds[i] or P.validate_text(value) ~= true then return nil, "INVALID_ARGUMENT" end
        v[k] = value
        local number = P.parse_decimal(value)
        if number then n[k] = number end
    end
    local valid = false
    if registered[name] then
        -- Give the validator private copies, so accidental writes cannot change
        -- the validated projection. n contains only successfully parsed uints;
        -- required numeric fields/ranges/relations are the validator's job.
        local vv, nn = {}, {}
        for k, value in next, v, nil do vv[k] = value end
        for k, value in next, n, nil do nn[k] = value end
        local ok, accepted, code = pcall(registered[name],vv,nn)
        if not ok then return nil, "INVALID_STATE" end
        if accepted ~= true then
            return nil, CJ.Context and CJ.Context.error_code(code) or "INVALID_ARGUMENT"
        end
        valid = true
    elseif name == "compatibility_artifact" or name == "compatibility_marker" then
        valid = valid_compat(v)
        if valid and name == "compatibility_marker" then
            if not I.digest(v.manifest_sha256) then return nil, "INVALID_ARGUMENT" end
            local encoded = P.record(fields_for(artifact, v), 16384)
            if not encoded then return nil, "INVALID_ARGUMENT" end
            local digest = P.sha256(encoded)
            if not digest then return nil, "INVALID_STATE" end
            if digest ~= v.manifest_sha256 then return nil, "COMPATIBILITY_MISMATCH" end
        end
    elseif name == "durability" then valid = valid_durability(v)
    elseif name == "admin_freeze" then
        valid = v.protocol_version == "2" and I.hex(v.freeze_nonce,32) and I.digest(v.process_stop_evidence_sha256) and
            I.digest(v.candidate_manifest_sha256) and I.digest(v.candidate_contract_sha256) and I.positive(v.created_at_ms)
    elseif name == "guard_core" or name == "commit_guard" then
        if not valid_guard(v) then return nil, "INVALID_ARGUMENT" end
        if name == "commit_guard" and (not I.digest(v.compatibility_manifest_sha256) or not I.positive(v.approved_at_ms)) then
            return nil, "INVALID_ARGUMENT"
        end
        valid = true
    elseif name == "legacy_retirement" then valid = valid_legacy(v)
    elseif name == "first_request_start" then
        valid = v.protocol_version == "2" and I.hex(v.run_id,32) and I.hex(v.job_id,64) and
            I.positive(v.lease_fence) and I.positive(v.started_at_ms)
    elseif name == "policy_group" then
        local scope = I.group_scope(v.rate_scope_id)
        valid = I.group(v.group_id) and scope ~= nil and scope == v.group_scope_id and
            n.request_start_limit ~= nil and n.request_start_limit >= 1 and n.request_start_limit <= 10 and
            n.concurrency ~= nil and n.concurrency >= 1 and n.concurrency <= 32 and
            n.interval_ms ~= nil and n.interval_ms <= 3600000
    end
    if not valid then return nil, "INVALID_ARGUMENT" end
    return {schema = name, v = v, n = n, fields = fields_for(def, v)}
end
function S.decode(name, bytes)
    local def = S.get(name)
    if not def then return nil, "INVALID_ARGUMENT" end
    local fields = P.decode_record(bytes, def.names, def.maximum)
    if not fields then return nil, "INVALID_ARGUMENT" end
    local values = {}
    for i = 1, #def.names do values[def.names[i]] = fields[i][2] end
    return S.project(name, values)
end
function S.encode(projection)
    if type(projection) ~= "table" then return nil, "INVALID_ARGUMENT" end
    local checked, code = S.project(projection.schema, projection.v)
    if not checked then return nil, code end
    local def = S.get(projection.schema)
    local encoded = P.record(checked.fields, def.maximum)
    if not encoded then return nil, "INVALID_ARGUMENT" end
    return encoded
end
function S.groups(bytes)
    local count, code = I.dense(bytes, 64)
    if not count then return nil, code end
    if count == 0 then return nil, "INVALID_ARGUMENT" end
    local records, ordered, by_id, previous = {}, {}, {}, nil
    for i = 1, count do
        local group, err = S.decode("policy_group", bytes[i])
        if not group then return nil, err end
        if previous and group.v.group_id <= previous then return nil, "INVALID_ARGUMENT" end
        previous = group.v.group_id
        ordered[i], records[i], by_id[previous] = group, group.fields, group
    end
    local section = P.section("groups", records, 65536)
    local prefix = P.frame("mifolyo:policy-group-map:v2")
    if not section or not prefix then return nil, "INVALID_ARGUMENT" end
    local digest = P.sha256(prefix .. section)
    if not digest then return nil, "INVALID_STATE" end
    return {ordered = ordered, by_id = by_id, count = count, digest = digest}
end
return S
