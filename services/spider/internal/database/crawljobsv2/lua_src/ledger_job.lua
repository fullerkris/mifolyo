-- Shared M3 Job schema, selected-ledger proof and prebuilt Job effects.
-- Schema/source/check remain pure; load performs explicit bounded reads. Job
-- effects never execute writes, read TIME, or compute allocation credit/G.
-- Assembly supplies CJ.P, CJ.Identities, CJ.Schemas and CJ.URL. Source binding
-- also needs CJ.Run; context-bound helpers need CJ.Context/CJ.Read. Load the
-- registration chunks before Context.open closes the schema registry.
local Job, P, I, S, URL = {}, CJ.P, CJ.Identities, CJ.Schemas, CJ.URL
local names = {
    "protocol_version", "run_id", "job_id", "url_id", "canonical_url", "depth", "score_text", "state",
    "group_id", "rate_scope_id", "group_scope_id", "initial_origin_scope_id", "policy_decision_sha256",
    "claim_count", "delivery_attempts", "request_starts", "lease_request_starts_baseline", "retry_count",
    "pre_io_recoveries", "next_request_ordinal", "last_request_started_at_ms", "last_document_request_started_at_ms",
    "last_document_request_fence", "last_document_target_url_id", "last_document_target_url", "last_document_target_digest",
    "last_reason", "last_failure_reason", "lease_owner", "lease_token", "lease_fence", "lease_started_at_ms",
    "lease_expires_at_ms", "lease_delivery_started", "active_reservation_id", "active_stage_commit_id",
    "last_stage_commit_id", "last_stage_fence", "not_before_ms", "commit_backpressure_fence", "commit_backpressure_reason",
    "commit_backpressure_started_at_ms", "commit_backpressure_deadline_ms", "output_digest", "publication_id", "commit_id",
    "published_page_key", "last_transition_id", "last_transition_status", "created_at_ms", "updated_at_ms",
    "completed_at_ms", "dead_at_ms", "cancelled_at_ms"
}
local bounds = {
    1,32,64,64,2048,16,12,9,128,32,64,64,64,16,16,16,16,16,16,16,16,16,16,64,2048,64,
    32,32,32,64,16,16,16,1,64,64,64,16,16,16,32,16,16,64,64,64,2806,64,32,16,16,16,16,16
}
local numeric = {
    "depth", "claim_count", "delivery_attempts", "request_starts", "lease_request_starts_baseline", "retry_count",
    "pre_io_recoveries", "next_request_ordinal", "last_request_started_at_ms", "last_document_request_started_at_ms",
    "last_document_request_fence", "lease_fence", "lease_started_at_ms", "lease_expires_at_ms", "lease_delivery_started",
    "last_stage_fence", "not_before_ms", "commit_backpressure_fence", "commit_backpressure_started_at_ms",
    "commit_backpressure_deadline_ms", "created_at_ms", "updated_at_ms", "completed_at_ms", "dead_at_ms", "cancelled_at_ms"
}
local function words(text)
    local result = {}
    for word in string.gmatch(text, "%S+") do result[word] = true end
    return result
end
local states = words("ready leased delayed completed dead cancelled")
local retryable = words([[request_timeout dns_temporary dial_temporary request_temporary http_429 http_5xx
robots_temporary renderer_temporary downstream_backpressure capacity_blocked_after_io run_budget_exhausted_after_io
group_budget_exhausted_after_io rate_blocked_after_io lease_expired_after_io worker_shutdown_after_io]])
local dead_reasons = words([[policy_denied policy_scope_changed robots_denied robots_invalid job_malformed
url_identity_mismatch static_url_denied dns_prohibited http_4xx response_invalid body_too_large html_invalid discovery_limit
renderer_permanent output_invalid run_job_limit reservation_limit_exhausted retry_exhausted pre_io_recovery_exhausted protocol_corrupt]])
local cancel_reasons = words("authorization_expired operator_cancelled source_cancelled")
local reasons = words("none published already_visited all_jobs_terminal request_budget_exhausted group_budgets_exhausted")
local function known_reason(value)
    return reasons[value] or retryable[value] or dead_reasons[value] or cancel_reasons[value]
end
local statuses = words([[OK CREATED EXISTS_IDENTICAL CANDIDATE_INSTALLED LEGACY_RETIRED CONTRACTS_PROMOTED SEALED ACTIVATED
AUDIT_STARTED CLAIMED ALREADY_CLAIMED NO_CANDIDATE VISITED_COMPLETED RESERVED ALREADY_RESERVED STARTED ALREADY_STARTED
FINISHED ALREADY_FINISHED RESERVATION_CANCELLED RELEASED_READY RENEWED RETRY_SCHEDULED STAGE_BEGUN STAGED STAGE_ABORTED
COMPLETED DEAD CANCELLED COMMITTED ALREADY_COMMITTED CAPACITY_BLOCKED RATE_BLOCKED LEASE_CAPACITY_BLOCKED STAGE_CAPACITY_BLOCKED
RUN_BUDGET_EXHAUSTED RUN_RESERVATION_LIMIT_EXHAUSTED GROUP_BUDGET_EXHAUSTED DOWNSTREAM_BACKPRESSURE AUTHORIZATION_EXPIRED
RUN_CANCELLED LEASE_LOST NOT_DUE BATCH_MORE BATCH_DONE ARCHIVED PURGED]])

local score = I.score -- shared exact ParseScoreText grammar, not permissive tonumber
local function target(id, url)
    if not I.hex(id, 64) then return nil end
    local identity = URL.check_canonical(url, 1)
    if not identity or identity.url_id ~= id then return nil end
    return I.framed("mifolyo:request-target:v2", {id, url})
end
local function origin_scope(url)
    local origin = URL.derive_origin(url) -- canonical identity alone permits IPs; admission does not.
    if not origin then return nil end
    return I.framed("mifolyo:rate:origin:v2", {origin})
end
local function section_digest(domain, label, fields)
    local section = P.section(label, {fields}, 16384)
    local prefix = P.frame(domain)
    if not section or not prefix then return nil end
    return P.sha256(prefix .. section)
end
local function abort_identity(v)
    local payload = section_digest("mifolyo:transition-payload:v2", "arguments",
        {{"owner_id", v.lease_owner}, {"commit_id", v.last_stage_commit_id}})
    if not payload then return nil end
    return I.framed("mifolyo:crawl-transition:v2", {"CJ2_ABORT_STAGE", v.run_id, v.job_id,
        v.lease_fence, v.lease_token, "none", payload})
end
local function base64url(s)
    local alphabet, out = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", {}
    for i = 1, #s, 3 do
        local a, b, c = string.byte(s, i, i + 2)
        local n = a * 65536 + (b or 0) * 256 + (c or 0)
        for j = 1, (c and 4 or b and 3 or 2) do
            local index = math.floor(n / 2 ^ (24 - 6 * j)) % 64 + 1
            out[#out + 1] = string.sub(alphabet, index, index)
        end
    end
    return table.concat(out)
end

-- S performs exact shape/text/bounds projection; this callback checks every
-- lexical and cross-field relation available from the 54-field hash ALONE.
-- It intentionally cannot authenticate policy tuples, reservation records,
-- stage slots/deadline caps, complete transcripts or a cleared lease's commit.
local function validate(v, n)
    -- Schemas supplies private copies and parses canonical unsigned decimals.
    -- An absent parse is an error, not a zero/default or caller-supplied .n.
    for _, k in ipairs(numeric) do
        if n[k] == nil then return nil, "INVALID_NUMBER" end
    end
    if v.protocol_version ~= "2" or not I.hex(v.run_id, 32) or v.url_id ~= v.job_id or
       not target(v.job_id, v.canonical_url) or not states[v.state] or not I.group(v.group_id) or
       not I.hex(v.rate_scope_id, 32) or not I.digest(v.group_scope_id) or
       not I.digest(v.initial_origin_scope_id) or not I.digest(v.policy_decision_sha256) or
       I.group_scope(v.rate_scope_id) ~= v.group_scope_id or origin_scope(v.canonical_url) ~= v.initial_origin_scope_id or
       score(v.score_text) == nil then return nil, "INVALID_ARGUMENT" end
    local state, B, G, deliveries, fence = v.state, n.lease_request_starts_baseline, n.request_starts, n.delivery_attempts, n.lease_fence
    if fence ~= n.claim_count or deliveries > 3 or n.lease_delivery_started > 1 or deliveries > n.claim_count or
       G < deliveries or G > 10 or n.retry_count > deliveries or n.retry_count >= 3 or n.pre_io_recoveries > 3 or
       n.pre_io_recoveries + deliveries > n.claim_count or n.next_request_ordinal == 0 or n.next_request_ordinal > 101 or
       n.next_request_ordinal <= n.claim_count or n.next_request_ordinal <= G or n.last_stage_fence > fence or
       n.commit_backpressure_fence > fence or n.created_at_ms == 0 or n.updated_at_ms < n.created_at_ms or
       B > G or B >= 10 or (n.claim_count == 0 and (B ~= 0 or G ~= 0)) then return nil, "INVALID_ARGUMENT" end
    local current, prior_min = G > B and 1 or 0, B > 0 and 1 or 0
    if deliveries < prior_min + current or deliveries > B + current or
       (fence > 0 and (deliveries - current >= fence or deliveries - current >= 3)) or
       ((G == 0) ~= (n.last_request_started_at_ms == 0)) or n.last_request_started_at_ms > n.updated_at_ms then
        return nil, "INVALID_ARGUMENT"
    end
    local document_empty = n.last_document_request_started_at_ms == 0 and n.last_document_request_fence == 0 and
        v.last_document_target_url_id == "" and v.last_document_target_url == "" and v.last_document_target_digest == ""
    if not document_empty then
        if n.last_document_request_started_at_ms == 0 or n.last_document_request_fence == 0 or
           not I.digest(v.last_document_target_digest) or
           target(v.last_document_target_url_id, v.last_document_target_url) ~= v.last_document_target_digest then
            return nil, "INVALID_ARGUMENT"
        end
    end
    if n.last_document_request_started_at_ms > n.last_request_started_at_ms or n.last_document_request_fence > fence or
       not known_reason(v.last_reason) or not known_reason(v.last_failure_reason) then return nil, "INVALID_ARGUMENT" end
    if state == "leased" then
        if not I.hex(v.lease_owner, 32) or not I.hex(v.lease_token, 64) or fence == 0 or n.lease_started_at_ms == 0 or
           n.lease_expires_at_ms <= n.lease_started_at_ms or n.lease_started_at_ms > n.updated_at_ms or
           ((n.lease_delivery_started == 1) ~= (G > B)) then return nil, "INVALID_ARGUMENT" end
    elseif v.lease_owner ~= "" or v.lease_token ~= "" or n.lease_started_at_ms ~= 0 or n.lease_expires_at_ms ~= 0 or
       n.lease_delivery_started ~= 0 then return nil, "INVALID_ARGUMENT" end
    if n.lease_delivery_started == 1 and (deliveries == 0 or G == 0 or n.last_request_started_at_ms == 0) then
        return nil, "INVALID_ARGUMENT"
    end
    if v.active_reservation_id ~= "" and (not I.digest(v.active_reservation_id) or state ~= "leased") then
        return nil, "INVALID_ARGUMENT"
    end
    for _, k in ipairs({"active_stage_commit_id", "last_stage_commit_id", "output_digest", "publication_id", "commit_id", "last_transition_id"}) do
        if v[k] ~= "" and not I.digest(v[k]) then return nil, "INVALID_ARGUMENT" end
    end
    if (v.last_stage_commit_id == "") ~= (n.last_stage_fence == 0) then return nil, "INVALID_ARGUMENT" end
    local frozen = n.last_stage_fence > 0 and n.last_stage_fence == fence
    if frozen and (G <= B or (state == "leased" and (v.active_reservation_id ~= "" or n.lease_delivery_started ~= 1))) then
        return nil, "INVALID_ARGUMENT"
    end
    if v.active_stage_commit_id ~= "" and (state ~= "leased" or v.active_reservation_id ~= "" or
       v.active_stage_commit_id ~= v.last_stage_commit_id or n.last_stage_fence ~= fence or n.lease_delivery_started ~= 1) then
        return nil, "INVALID_ARGUMENT"
    end
    local aborted = state == "leased" and v.active_reservation_id == "" and v.active_stage_commit_id == "" and
        v.last_stage_commit_id ~= "" and n.last_stage_fence == fence and n.last_document_request_fence == fence and
        n.lease_delivery_started == 1 and v.last_reason == "none" and v.last_transition_status == "STAGE_ABORTED" and
        abort_identity(v) == v.last_transition_id
    if (v.last_transition_status == "STAGE_ABORTED" or (state == "leased" and frozen and v.active_stage_commit_id == "")) and
       not aborted then return nil, "INVALID_ARGUMENT" end
    if v.commit_backpressure_reason == "none" then
        if n.commit_backpressure_fence ~= 0 or n.commit_backpressure_started_at_ms ~= 0 or n.commit_backpressure_deadline_ms ~= 0 then
            return nil, "INVALID_ARGUMENT"
        end
    elseif v.commit_backpressure_reason ~= "pages_queue_full" and v.commit_backpressure_reason ~= "memory_headroom_low" then
        return nil, "INVALID_ARGUMENT"
    elseif state ~= "leased" or (v.active_stage_commit_id == "" and not aborted) or n.commit_backpressure_fence ~= fence or
       n.commit_backpressure_started_at_ms == 0 or n.commit_backpressure_deadline_ms == 0 or
       (n.commit_backpressure_deadline_ms > n.commit_backpressure_started_at_ms and
        n.commit_backpressure_deadline_ms - n.commit_backpressure_started_at_ms > 120000) then return nil, "INVALID_ARGUMENT" end
    local published = v.output_digest ~= "" or v.publication_id ~= "" or v.commit_id ~= "" or v.published_page_key ~= ""
    if published then
        if state ~= "completed" or v.output_digest == "" or v.publication_id == "" or v.commit_id == "" or v.published_page_key == "" or
           n.last_stage_fence == 0 or n.last_stage_fence ~= fence or n.last_document_request_fence ~= n.last_stage_fence or G <= B or
           v.commit_id ~= v.last_stage_commit_id or v.last_reason ~= "published" then return nil, "INVALID_ARGUMENT" end
        local publication = I.framed("mifolyo:page-publication:v2", {v.run_id, v.job_id, v.last_stage_fence, v.output_digest})
        if publication ~= v.publication_id or v.published_page_key ~= "page_data:" .. publication .. ":" .. base64url(v.last_document_target_url) then
            return nil, "INVALID_ARGUMENT"
        end
    elseif state == "completed" and v.last_reason ~= "already_visited" then return nil, "INVALID_ARGUMENT" end
    if (v.last_transition_id == "") ~= (v.last_transition_status == "") or
       (v.last_transition_status ~= "" and not statuses[v.last_transition_status]) then return nil, "INVALID_ARGUMENT" end
    if (n.pre_io_recoveries == 3) ~= (state == "dead" and v.last_reason == "pre_io_recovery_exhausted") or
       (v.last_reason == "retry_exhausted" and state ~= "dead") then return nil, "INVALID_ARGUMENT" end
    local reason, failure, status = v.last_reason, v.last_failure_reason, v.last_transition_status
    if state == "completed" then
        if reason ~= "published" and reason ~= "already_visited" then return nil, "INVALID_ARGUMENT" end
        if status ~= "" and not (status == "COMMITTED" and reason == "published") and
           not ((status == "VISITED_COMPLETED" or status == "COMPLETED") and reason == "already_visited") then return nil, "INVALID_ARGUMENT" end
    elseif state == "dead" then
        if not dead_reasons[reason] or (status ~= "" and status ~= "DEAD") then return nil, "INVALID_ARGUMENT" end
        if reason == "retry_exhausted" then
            if deliveries ~= 3 or not retryable[failure] then return nil, "INVALID_ARGUMENT" end
        elseif reason == "pre_io_recovery_exhausted" then
            if deliveries >= 3 or (failure ~= "none" and failure ~= "pre_io_recovery_exhausted" and not retryable[failure]) then
                return nil, "INVALID_ARGUMENT"
            end
        elseif failure ~= reason then return nil, "INVALID_ARGUMENT" end
    elseif state == "cancelled" and (not cancel_reasons[reason] or (status ~= "" and status ~= "CANCELLED")) then
        return nil, "INVALID_ARGUMENT"
    end
    if (state == "delayed") ~= (n.not_before_ms > 0) or
       (state == "completed") ~= (n.completed_at_ms > 0) or
       (state == "dead") ~= (n.dead_at_ms > 0) or
       (state == "cancelled") ~= (n.cancelled_at_ms > 0) or
       n.completed_at_ms > n.updated_at_ms or n.dead_at_ms > n.updated_at_ms or n.cancelled_at_ms > n.updated_at_ms then
        return nil, "INVALID_ARGUMENT"
    end
    return true
end

-- Trusted source registration only; the core owns projection/framing/read bounds.
local registered, registration_error = S.register("job", {names=names, bounds=bounds}, validate)
if not registered then return nil, registration_error end

local source_names = {"job_id", "canonical_url", "score_text", "depth", "group_id", "rate_scope_id",
    "group_scope_id", "initial_origin_scope_id", "policy_decision_sha256"}
local discovery_names = {"job_id", "canonical_url", "depth", "score_text", "group_id", "rate_scope_id",
    "group_scope_id", "initial_origin_scope_id", "policy_decision_sha256"}
local function plain(t) return type(t) == "table" and getmetatable(t) == nil end
local function record_values(record, ordered_names)
    local bytes = record
    if plain(record) then bytes = P.record(record.fields, 16384) end
    if type(bytes) ~= "string" then return nil, "INVALID_ARGUMENT" end
    local fields = P.decode_record(bytes, ordered_names, 16384)
    if not fields then return nil, "INVALID_ARGUMENT" end
    local v = {}
    for i, name in ipairs(ordered_names) do v[name] = fields[i][2] end
    return {v=v, fields=fields}
end

-- runLedger is Run.load's {run_id,v,groups={ordered,...}} result (or an
-- equivalent pure projection for offline validation). Recompute, never trust
-- supplied .n, .by_id, .digest or typed names. This establishes consistency,
-- not Redis authority; Job.check additionally uses private reader receipts.
local function policy(run)
    if not plain(run) or not I.hex(run.run_id, 32) or not plain(run.groups) or not CJ.Run then
        return nil, "INVALID_STATE"
    end
    local record = CJ.Run.validate(run.v)
    if not record then return nil, "INVALID_STATE" end
    local count = I.dense(run.groups.ordered, 64)
    if not count or count == 0 then return nil, "INVALID_STATE" end
    local bytes = {}
    for i = 1, count do
        local g = run.groups.ordered[i]
        if not plain(g) then return nil, "INVALID_STATE" end
        local group = S.project("policy_group", g.v)
        if not group then return nil, "IMMUTABLE_MISMATCH" end
        bytes[i] = S.encode(group)
        if not bytes[i] then return nil, "INVALID_STATE" end
    end
    local groups = S.groups(bytes)
    if not groups or groups.count ~= record.n.policy_group_count or groups.digest ~= record.v.policy_group_map_sha256 then
        return nil, "IMMUTABLE_MISMATCH"
    end
    return {run_id=run.run_id, record=record, groups=groups}
end
local function context_run(ctx, binding)
    if not CJ.Context or not CJ.Context.preparing(ctx) or not plain(ctx.keys) or
       not plain(ctx.request) or not plain(ctx.request.v) or ctx.request.v.run_id ~= binding.run_id or
       ctx.keys.run ~= "mifolyo:crawl:v2:run:" .. binding.run_id or
       not I.integer(ctx.now_ms, P.limits.max_integer) or ctx.now_ms == 0 or
       P.parse_decimal(ctx.now_text) ~= ctx.now_ms then return nil, "INVALID_STATE" end
    return true
end
local function source_bound(record, binding, ordered_names)
    local source, code = record_values(record, ordered_names)
    if not source then return nil, code end
    local v = source.v
    if not I.hex(v.job_id, 64) or not I.group(v.group_id) or not I.hex(v.rate_scope_id, 32) or
       not I.digest(v.group_scope_id) or not I.digest(v.initial_origin_scope_id) or not I.digest(v.policy_decision_sha256) then
        return nil, "INVALID_IDENTIFIER"
    end
    local identity = URL.check_canonical(v.canonical_url, 1)
    if not identity then return nil, "INVALID_IDENTIFIER" end
    if identity.url_id ~= v.job_id then return nil, "URL_ID_COLLISION" end
    local depth, priority = P.parse_decimal(v.depth), score(v.score_text)
    if depth == nil or priority == nil then return nil, "INVALID_NUMBER" end
    local origin = origin_scope(v.canonical_url)
    if not origin then return nil, "INVALID_IDENTIFIER" end
    local group = binding.groups.by_id[v.group_id]
    if not group or group.v.rate_scope_id ~= v.rate_scope_id or group.v.group_scope_id ~= v.group_scope_id or
       origin ~= v.initial_origin_scope_id then return nil, "IMMUTABLE_MISMATCH" end
    local global, digest = I.global_scope(), target(v.job_id, v.canonical_url)
    if not global or not digest then return nil, "INVALID_STATE" end
    if global == v.group_scope_id or global == origin or v.group_scope_id == origin then return nil, "IMMUTABLE_MISMATCH" end
    local decision = section_digest("mifolyo:policy-decision:v2", "decision", {
        {"request_kind", "document"}, {"target_url_id", v.job_id}, {"target_digest", digest}, {"depth", v.depth},
        {"group_id", v.group_id}, {"rate_scope_id", v.rate_scope_id}, {"global_scope_id", global},
        {"group_scope_id", v.group_scope_id}, {"origin_scope_id", origin},
        {"global_concurrency", binding.record.v.global_concurrency_limit}, {"global_interval_ms", "0"},
        {"group_concurrency", group.v.concurrency}, {"group_interval_ms", group.v.interval_ms},
        {"origin_concurrency", group.v.concurrency}, {"origin_interval_ms", group.v.interval_ms}
    })
    if not decision then return nil, "INVALID_STATE" end
    if decision ~= v.policy_decision_sha256 then return nil, "IMMUTABLE_MISMATCH" end
    local fields = {}
    for i, name in ipairs(source_names) do fields[i] = {name, v[name]} end
    return {v=v, n={depth=depth, score_text=priority}, fields=fields}
end

-- Input is binary RECORD or {fields=ordered_pairs} (Wire.record's projection
-- is accepted; its redundant .v/.n are never authority). Output is the exact
-- nine-field ENQUEUE order. No run id, URL id or other invented record fields.
-- A single source cannot establish ordering: callers must enforce strict job-id
-- order across their complete batch/cursor, or use the bounded sources helper.
function Job.source(record, runLedger)
    local binding, code = policy(runLedger)
    if not binding then return nil, code end
    return source_bound(record, binding, source_names)
end
function Job.discovery(record, runLedger)
    local binding, code = policy(runLedger)
    if not binding then return nil, code end
    return source_bound(record, binding, discovery_names)
end
function Job.sources(records, runLedger)
    local count = I.dense(records, 500)
    if not count or count == 0 then return nil, "INVALID_ARGUMENT" end
    local binding, code = policy(runLedger)
    if not binding then return nil, code end
    local result, previous = {}, nil
    for i = 1, count do
        local source, err = source_bound(records[i], binding, source_names)
        if not source then return nil, err end
        if previous and source.v.job_id <= previous then return nil, "INVALID_ARGUMENT" end
        result[i], previous = source, source.v.job_id
    end
    return result
end

-- Pure constructor, not admission: caller establishes job absence, run state,
-- aggregate capacities, authorization, and all writes via the common planner.
-- Context.open owns the one server clock. Never accept a source/client clock.
-- The opened request/key must name this run, including discovered-job creation.
function Job.initial_record(ctx, runLedger, source)
    if not CJ.Context or not CJ.Context.preparing(ctx) then return nil, "INVALID_STATE" end
    local binding, code = policy(runLedger)
    if not binding then return nil, code end
    local bound, failure = context_run(ctx, binding)
    if not bound then return nil, failure end
    local admitted, err = source_bound(source, binding, source_names)
    if not admitted then return nil, err end
    if ctx.now_ms < binding.record.n.created_at_ms then return nil, "INVALID_STATE" end
    local v = {}
    for _, name in ipairs(names) do v[name] = "" end
    for _, name in ipairs(numeric) do v[name] = "0" end
    for _, name in ipairs(source_names) do v[name] = admitted.v[name] end
    v.protocol_version, v.run_id, v.url_id, v.state = "2", binding.run_id, admitted.v.job_id, "ready"
    v.last_reason, v.last_failure_reason, v.commit_backpressure_reason = "none", "none", "none"
    v.next_request_ordinal, v.created_at_ms, v.updated_at_ms = "1", ctx.now_text, ctx.now_text
    return S.project("job", v)
end

local index_defs = {{"jobs", "set", 10000}, {"job_order", "zset", 10000}, {"ready", "zset", 10000},
    {"ready_at", "zset", 10000}, {"leased", "zset", 64}, {"leased_at", "zset", 64},
    {"delayed", "zset", 10000}, {"completed", "zset", 10000}, {"dead", "zset", 10000},
    {"cancelled", "zset", 10000}, {"commit_backpressure", "zset", 10}, {"active_leases", "zset", 64}}
local function receipt(ctx, selected, key)
    if not plain(selected) or selected.key ~= key then return nil, "INVALID_STATE" end
    -- snapshot is receipt-only: this function cannot trigger Redis reads.
    return CJ.Read.snapshot(ctx, key)
end
local function same_score(a, b)
    return type(a) == "number" and a == b and (a ~= 0 or 1 / a == 1 / b)
end
local function membership(fact, kind, maximum, member)
    if not plain(fact) or not I.integer(fact.count, maximum) or not plain(fact.members) or
       (fact.kind ~= kind and not (fact.kind == "none" and fact.exists == false and fact.count == 0)) then
        return nil, "STATE_INDEX_CORRUPT"
    end
    local present = fact.members[member]
    -- Even a cardinality of zero is not a selected member receipt. Ask R.members
    -- explicitly; it records false for absence. Unrequested nil is an API error.
    if type(present) ~= "boolean" then return nil, "INVALID_STATE" end
    if present and (fact.exists ~= true or fact.count == 0) then return nil, "STATE_INDEX_CORRUPT" end
    local value = false
    if kind == "zset" then
        if not plain(fact.scores) or not plain(fact.score_text) then return nil, "INVALID_STATE" end
        if present then
            value = I.redis_score(fact.score_text[member])
            -- Preserve binary64 zero's sign even in a host Lua interpreter whose
            -- tonumber uses an integer fast path for "-0". No canonical job
            -- score/timestamp is negative zero (Go ValidateRedisScore is bit-exact).
            if value == nil or (value == 0 and string.sub(fact.score_text[member], 1, 1) == "-") or
               not same_score(value, fact.scores[member]) then return nil, "STATE_INDEX_CORRUPT" end
        elseif fact.scores[member] ~= false or fact.score_text[member] ~= false then return nil, "INVALID_STATE" end
    end
    return {present=present, score=value}
end

-- view = {ctx=the_open_context, job=R.fixed_hash(...,"job"), jobs=...,
-- job_order=..., ready=..., ready_at=..., leased=..., leased_at=..., delayed=...,
-- completed=..., dead=..., cancelled=..., commit_backpressure=..., active_leases=...}.
-- Each index must be an explicit R.members receipt selecting this job (global
-- lease member is run_id:job_id); selected public tables never replace private
-- facts. runLedger is Run.load from the SAME ctx. No facts are fetched here.
-- Returns the complete S.project result plus exists/key/kind; explicit fully
-- unindexed absence returns {exists=false,key,kind="none"} for insert planning.
-- Run.validate_ledger owns aggregate accounting. Request/scope/stage/transcript
-- cross-record checks and worker stale-lease decisions remain their owners'.
-- Core API rev3 grants ENQUEUE/AUDIT read-only access to the literal global
-- active_leases key without changing their 38+n wire keys. Callers still select
-- its membership explicitly through R.members; the grant is not an absence
-- receipt or Plan write permission. Other operations need their own key authority.
function Job.check(view, runLedger, job_id)
    if not plain(view) or not CJ.Context or not CJ.Context.preparing(view.ctx) or not CJ.Read or
       not I.hex(job_id, 64) then return nil, "INVALID_STATE" end
    local binding, code = policy(runLedger)
    if not binding then return nil, code end
    local bound, failure = context_run(view.ctx, binding)
    if not bound then return nil, failure end
    local ctx, base = view.ctx, "mifolyo:crawl:v2:run:" .. binding.run_id
    local run = CJ.Read.snapshot(ctx, base)
    if not run or run.exists ~= true or run.schema ~= "run" or not run.complete or
       S.encode(binding.record) ~= S.encode(run) then return nil, "INVALID_STATE" end
    -- Only the immutable five policy maps are needed here; no guessed budgets,
    -- scopes or other cross-run facts. Their complete private receipts bind the
    -- supplied pure group projections to the stored pinned run.
    for _, pair in ipairs({{"group_limits", "request_start_limit"}, {"group_rate_scope_ids", "rate_scope_id"},
        {"group_scope_ids", "group_scope_id"}, {"group_concurrency", "concurrency"}, {"group_interval_ms", "interval_ms"}}) do
        local map = CJ.Read.snapshot(ctx, base .. ":" .. pair[1])
        if not map or not map.complete or map.kind ~= "hash" or map.count ~= binding.groups.count or not plain(map.v) then
            return nil, "INVALID_STATE"
        end
        for _, group in ipairs(binding.groups.ordered) do
            if map.v[group.v.group_id] ~= group.v[pair[2]] then return nil, "IMMUTABLE_MISMATCH" end
        end
    end
    local key = base .. ":job:" .. job_id
    local job, err = receipt(ctx, view.job, key)
    if not job then return nil, err end
    if not job.complete then return nil, "INVALID_STATE" end
    local ix = {}
    for _, def in ipairs(index_defs) do
        local member, index_key = job_id, base .. ":" .. def[1]
        if def[1] == "active_leases" then member, index_key = binding.run_id .. ":" .. job_id, "mifolyo:crawl:v2:active_leases" end
        local fact, failure = receipt(ctx, view[def[1]], index_key)
        if not fact then return nil, failure end
        ix[def[1]], failure = membership(fact, def[2], def[3], member)
        if not ix[def[1]] then return nil, failure end
    end
    if job.exists == false and job.kind == "none" then
        for _, def in ipairs(index_defs) do if ix[def[1]].present then return nil, "STATE_INDEX_CORRUPT" end end
        return {exists=false, key=key, kind="none"}
    end
    if job.exists ~= true or job.kind ~= "hash" or job.schema ~= "job" then return nil, "INVALID_STATE" end
    local checked = S.project("job", job.v)
    if not checked or checked.v.run_id ~= binding.run_id or checked.v.job_id ~= job_id then return nil, "INVALID_STATE" end
    local v, n, fields = checked.v, checked.n, {}
    for i, name in ipairs(source_names) do fields[i] = {name, v[name]} end
    local source = source_bound({fields=fields}, binding, source_names)
    if not source then return nil, "IMMUTABLE_MISMATCH" end
    if not ix.jobs.present or not ix.job_order.present or not same_score(ix.job_order.score, 0) then return nil, "STATE_INDEX_CORRUPT" end
    local expected = {ready=score(v.score_text), leased=n.lease_expires_at_ms, delayed=n.not_before_ms,
        completed=n.completed_at_ms, dead=n.dead_at_ms, cancelled=n.cancelled_at_ms}
    for _, state in ipairs({"ready", "leased", "delayed", "completed", "dead", "cancelled"}) do
        if ix[state].present ~= (v.state == state) or (ix[state].present and not same_score(ix[state].score, expected[state])) then
            return nil, "STATE_INDEX_CORRUPT"
        end
    end
    if ix.ready_at.present ~= (v.state == "ready") or (ix.ready_at.present and
       (not I.integer(ix.ready_at.score, n.updated_at_ms) or ix.ready_at.score < n.created_at_ms)) or
       ix.leased_at.present ~= (v.state == "leased") or (ix.leased_at.present and not same_score(ix.leased_at.score, n.lease_started_at_ms)) or
       ix.active_leases.present ~= (v.state == "leased") or (ix.active_leases.present and not same_score(ix.active_leases.score, n.lease_expires_at_ms)) or
       ix.commit_backpressure.present ~= (n.commit_backpressure_started_at_ms > 0) or
       (ix.commit_backpressure.present and not same_score(ix.commit_backpressure.score, n.commit_backpressure_started_at_ms)) then
        return nil, "STATE_INDEX_CORRUPT"
    end
    checked.exists, checked.key, checked.kind = true, key, "hash"
    return checked
end

local function copy_values(v)
    local out = {}; for k, value in next, v, nil do out[k] = value end; return out
end
local ROOT = "mifolyo:crawl:v2:"
local function job_view(ctx, run_id, job_id)
    local base = ROOT .. "run:" .. run_id
    local view = {ctx=ctx,job={key=base .. ":job:" .. job_id}}
    for _, def in ipairs(index_defs) do
        view[def[1]] = {key=def[1] == "active_leases" and ROOT .. def[1] or base .. ":" .. def[1]}
    end
    return view
end
-- All keys are derived from validated identity, never a caller-selected role.
-- Commit/maintenance callers must obtain their core-owned derived-key grant
-- before using this reader for a job not explicitly present on their wire.
function Job.load(ctx, run, job_id)
    if not plain(run) or not I.hex(run.run_id,32) or not I.hex(job_id,64) or
       not CJ.Context.preparing(ctx) then return nil,"INVALID_STATE" end
    local view = job_view(ctx,run.run_id,job_id)
    local job, code = CJ.Read.fixed_hash(ctx,view.job.key,"job")
    if not job then return nil,code end
    view.job = job
    for _, def in ipairs(index_defs) do
        local global = def[1] == "active_leases"
        local member = global and run.run_id .. ":" .. job_id or job_id
        local fact, err = CJ.Read.members(ctx,view[def[1]].key,def[2],{member},def[3],global and 97 or 64)
        if not fact then return nil,err end
        view[def[1]] = fact
    end
    return Job.check(view,run,job_id)
end
-- Mutation/replay timestamp and per-job contribution checks supplement the
-- clock-independent fixed-record validator. No live-lease eligibility here.
function Job.at(ctx, run, job)
    if not CJ.Context.preparing(ctx) or not plain(run) or not plain(job) or not job.exists or
       not I.hex(run.run_id,32) or run.key ~= ctx.keys.run or run.key ~= ROOT .. "run:" .. run.run_id then return nil,"INVALID_STATE" end
    local record, owner = S.project("job",job.v),CJ.Run.validate(run.v)
    if not record or not owner or record.v.run_id ~= run.run_id or owner.v.purge_state ~= "none" then return nil,"INVALID_STATE" end
    local n, rn = record.n,owner.n
    if n.created_at_ms < rn.created_at_ms or n.updated_at_ms > rn.last_activity_at_ms or rn.last_activity_at_ms > ctx.now_ms or
       n.claim_count > rn.claims_total or n.request_starts > rn.request_starts or n.retry_count > rn.retries_total or
       n.pre_io_recoveries > rn.recovered_leases_total or n.next_request_ordinal-1 > rn.reservation_creations_total then
        return nil,"COUNTER_CORRUPT"
    end
    for _, field in ipairs({"last_request_started_at_ms","last_document_request_started_at_ms","lease_started_at_ms",
        "completed_at_ms","dead_at_ms","cancelled_at_ms"}) do
        if n[field] ~= 0 and (n[field] < n.created_at_ms or n[field] > n.updated_at_ms) then return nil,"INVALID_STATE" end
    end
    -- First blocked COMMIT deliberately leaves Job.updated_at_ms and general Run
    -- activity untouched. Its dedicated clock must have been recorded during
    -- this lease, and cannot be in this invocation's future. This does not require
    -- the lease to remain live now: expiry recovery validates the same history.
    -- S.project above still binds the block to this fence/stage and its bounded
    -- deadline. Stage's owned/recovery proof checks the exact stage-expiry cap;
    -- that capped deadline may already be elapsed and is NOT a general timestamp.
    local blocked = n.commit_backpressure_started_at_ms
    if blocked > 0 and (blocked < n.lease_started_at_ms or blocked >= n.lease_expires_at_ms or blocked > ctx.now_ms) then
        return nil,"INVALID_STATE"
    end
    if n.last_request_started_at_ms > rn.last_request_started_at_ms or
       (job.v.state == "leased" and (rn.activated_at_ms == 0 or n.lease_started_at_ms < rn.activated_at_ms)) then
        return nil,"INVALID_STATE"
    end
    if job.v.state == "ready" or job.v.state == "leased" or job.v.state == "delayed" then
        local map = plain(run.maps) and run.maps.group_open_jobs
        local contribution = plain(map) and plain(map.v) and P.parse_decimal(map.v[job.v.group_id])
        if not contribution or contribution < 1 or rn.open_job_count < 1 then return nil,"COUNTER_CORRUPT" end
    else
        local map = plain(run.reasons) and run.reasons.disposition_reason_counts
        local contribution = plain(map) and plain(map.v) and P.parse_decimal(map.v[job.v.last_reason])
        if not contribution or contribution < 1 or rn[job.v.state .. "_total"] < 1 or
           n[job.v.state .. "_at_ms"] > rn.last_terminal_transition_at_ms then return nil,"COUNTER_CORRUPT" end
    end
    return true
end

local outcome_ops = words("CJ2_REJECT_READY CJ2_RELEASE_BEFORE_IO CJ2_RETRY CJ2_DEAD CJ2_CANCEL_JOB CJ2_COMPLETE_NO_OUTPUT")
local ready_reasons = words("policy_denied policy_scope_changed job_malformed url_identity_mismatch static_url_denied")
function Job.outcome_reason(operation, reason)
    if operation == "CJ2_REJECT_READY" then return ready_reasons[reason] == true end
    if operation == "CJ2_RELEASE_BEFORE_IO" then return reason == "none" end
    if operation == "CJ2_RETRY" then return retryable[reason] == true and reason ~= "lease_expired_after_io" end
    if operation == "CJ2_DEAD" then
        return dead_reasons[reason] == true and reason ~= "policy_scope_changed" and reason ~= "retry_exhausted" and
            reason ~= "pre_io_recovery_exhausted"
    end
    if operation == "CJ2_CANCEL_JOB" then return cancel_reasons[reason] == true end
    return operation == "CJ2_COMPLETE_NO_OUTPUT" and reason == "already_visited"
end
-- Bounded control identity, exactly Go deriveLeaseTransitionID / reject-ready.
-- REJECT needs the pinned source; all other payloads contain owner_id ONLY.
function Job.outcome_identity(operation, v, run)
    if not outcome_ops[operation] or not plain(v) then return nil,"INVALID_ARGUMENT" end
    local reason = operation == "CJ2_RELEASE_BEFORE_IO" and "none" or v.reason
    if not Job.outcome_reason(operation,reason) then return nil,"INVALID_ARGUMENT" end
    if not I.hex(v.run_id,32) or not I.hex(v.job_id,64) then return nil,"INVALID_IDENTIFIER" end
    local fields, fence, token = {},v.fence,v.lease_token
    if operation == "CJ2_REJECT_READY" then
        if not plain(run) or run.run_id ~= v.run_id then return nil,"IMMUTABLE_MISMATCH" end
        local source_fields = {}; for i, name in ipairs(source_names) do source_fields[i] = {name,v[name]} end
        local source, code = Job.source({fields=source_fields},run)
        if not source then return nil,code end
        for i=2,#source.fields do fields[#fields+1] = source.fields[i] end
        fence,token = "0",""
    else
        if not I.hex(v.owner_id,32) or not I.hex(token,64) or not I.positive(fence) then return nil,"INVALID_IDENTIFIER" end
        fields = {{"owner_id",v.owner_id}}
    end
    local payload = section_digest("mifolyo:transition-payload:v2","arguments",fields)
    if not payload then return nil,"INVALID_STATE" end
    return I.framed("mifolyo:crawl-transition:v2",{operation,v.run_id,v.job_id,fence,token,reason,payload})
end
function Job.cancellation(ctx, run)
    if run.v.state == "cancelled" then
        if not cancel_reasons[run.v.terminal_reason] then return nil,"INVALID_STATE" end
        return run.v.terminal_reason
    end
    if run.v.state == "active" and ctx.now_ms >= run.n.authorization_expires_at_ms then return "authorization_expired" end
    return false
end

local clear_numbers = {"lease_started_at_ms","lease_expires_at_ms","lease_delivery_started","not_before_ms",
    "commit_backpressure_fence","commit_backpressure_started_at_ms","commit_backpressure_deadline_ms"}
local clear_text = {"lease_owner","lease_token","active_reservation_id","active_stage_commit_id"}
-- Source-owned proposal builder, NOT admission. Caller proves why the outcome is
-- legal and supplies the exact transition identity (maintenance can clear it).
-- Every retained history field is copied, never recreated from defaults.
function Job.outcome_record(ctx, job, changes)
    if not CJ.Context.preparing(ctx) or not plain(job) or not plain(changes) then return nil,"INVALID_STATE" end
    local original = S.project("job",job.v)
    if not original then return nil,"INVALID_STATE" end
    local v = copy_values(original.v)
    for _, field in ipairs(clear_numbers) do v[field] = "0" end
    for _, field in ipairs(clear_text) do v[field] = "" end
    v.commit_backpressure_reason,v.updated_at_ms = "none",ctx.now_text
    local allowed = words([[state last_reason last_failure_reason last_transition_id last_transition_status
not_before_ms retry_count pre_io_recoveries output_digest publication_id commit_id published_page_key]])
    for field,value in next,changes,nil do
        if not allowed[field] or type(value) ~= "string" then return nil,"INVALID_ARGUMENT" end
        v[field] = value
    end
    for _, state in ipairs({"completed","dead","cancelled"}) do
        v[state .. "_at_ms"] = v.state == state and ctx.now_text or "0"
    end
    return S.project("job",v)
end
local planned_jobs = {}
local function plan_roles(ctx,run,job_id)
    if not plain(run) or not I.hex(run.run_id,32) or not I.hex(job_id,64) or
       not CJ.Context.preparing(ctx) or run.key ~= ROOT .. "run:" .. run.run_id or ctx.keys.run ~= run.key or
       ctx.request.v.run_id ~= run.run_id then return nil,"INVALID_STATE" end
    for _, name in ipairs({"jobs","job_order","ready","ready_at","leased","leased_at","delayed","completed","dead","cancelled",
        "commit_backpressure","group_open_jobs","retry_reason_counts","recovery_outcome_counts","disposition_reason_counts"}) do
        if CJ.Run.key(ctx,name) ~= run.key .. ":" .. name then return nil,"INVALID_STATE" end
    end
    local key = run.key .. ":job:" .. job_id
    if not CJ.Context.can_write(ctx,key) then return nil,"INVALID_STATE" end
    return key
end
local function mark_plan(plan,key)
    if planned_jobs[plan] and planned_jobs[plan][key] then return nil,"INVALID_STATE" end
    if not planned_jobs[plan] then planned_jobs[plan] = {} end
    planned_jobs[plan][key] = true
    return true
end
-- Reusable maintenance/commit effects API. Requires prior Job.load/check facts
-- and a COMPLETE proposed Job record. Does not authorize a reason, free request
-- capacity, validate a stage, remove a slot, flush Run, or select memory coverage.
-- Those proofs belong to the caller/Request/Stage/core; coverage is passed through
-- unchanged. One external Run delta is accumulated and flushed once per batch.
function Job.plan_outcome(ctx, plan, delta, run, job, proposed, coverage)
    if coverage == nil then coverage = "ordinary" end
    if not plain(job) or not plain(proposed) or not plain(job.v) then return nil,"INVALID_ARGUMENT" end
    local key, code = plan_roles(ctx,run,job.v.job_id); if not key then return nil,code end
    local before, err = Job.check(job_view(ctx,run.run_id,job.v.job_id),run,job.v.job_id)
    if not before or not before.exists or S.encode(before) ~= S.encode(job) then return nil,err or "INVALID_STATE" end
    local valid, why = Job.at(ctx,run,before); if not valid then return nil,why end
    local post = S.project("job",proposed.v)
    if not post then return nil,"INVALID_STATE" end
    local v,n,old,on = post.v,post.n,before.v,before.n
    local edges = {ready={completed=true,dead=true,cancelled=true},delayed={ready=true,cancelled=true},
        leased={ready=true,delayed=true,completed=true,dead=true,cancelled=true}}
    if not edges[old.state] or not edges[old.state][v.state] or v.updated_at_ms ~= ctx.now_text then return nil,"INVALID_STATE" end
    local mutable = words([[state last_reason last_failure_reason lease_owner lease_token lease_started_at_ms lease_expires_at_ms
lease_delivery_started active_reservation_id active_stage_commit_id not_before_ms commit_backpressure_fence commit_backpressure_reason
commit_backpressure_started_at_ms commit_backpressure_deadline_ms last_transition_id last_transition_status updated_at_ms
completed_at_ms dead_at_ms cancelled_at_ms retry_count pre_io_recoveries output_digest publication_id commit_id published_page_key]])
    for _, field in ipairs(names) do if not mutable[field] and v[field] ~= old[field] then return nil,"IMMUTABLE_MISMATCH" end end
    for _, field in ipairs(clear_text) do if v[field] ~= "" then return nil,"INVALID_STATE" end end
    for _, field in ipairs(clear_numbers) do
        if field ~= "not_before_ms" and v[field] ~= "0" then return nil,"INVALID_STATE" end
    end
    if v.commit_backpressure_reason ~= "none" or n.retry_count-on.retry_count ~= (v.state == "delayed" and 1 or 0) or
       (n.pre_io_recoveries ~= on.pre_io_recoveries and not (ctx.operation == "CJ2_RECOVER_EXPIRED" and
        old.state == "leased" and on.request_starts == on.lease_request_starts_baseline and n.pre_io_recoveries == on.pre_io_recoveries+1)) then
        return nil,"COUNTER_CORRUPT"
    end
    if old.state == "leased" and v.state == "ready" and on.request_starts ~= on.lease_request_starts_baseline then return nil,"INVALID_STATE" end
    if v.state == "delayed" then
        local delay = on.delivery_attempts == 1 and 30000 or (on.delivery_attempts == 2 and 120000 or nil)
        if not delay or on.request_starts <= on.lease_request_starts_baseline or not retryable[v.last_reason] or
           v.last_failure_reason ~= v.last_reason or P.safe_add(ctx.now_ms,delay) ~= n.not_before_ms then return nil,"INVALID_STATE" end
    end
    for _, state in ipairs({"completed","dead","cancelled"}) do
        if v.state == state and v[state .. "_at_ms"] ~= ctx.now_text then return nil,"INVALID_STATE" end
    end
    if v.last_reason == "published" then
        local commit = I.framed("mifolyo:crawl-commit:v2",{v.run_id,v.job_id,v.lease_fence,old.lease_token,
            v.publication_id,v.lease_request_starts_baseline,v.request_starts})
        if v.state ~= "completed" or old.active_stage_commit_id ~= v.commit_id or commit ~= v.commit_id then return nil,"STAGE_INVALID" end
    elseif v.output_digest ~= "" or v.publication_id ~= "" or v.commit_id ~= "" or v.published_page_key ~= "" then return nil,"INVALID_STATE" end
    local changed = {}; for _, field in ipairs(names) do if v[field] ~= old[field] then changed[field] = v[field] end end
    local calls = {{"ZREM",run.key .. ":" .. old.state,v.job_id}}
    local indexes = {[old.state]=-1,[v.state]=1}
    if old.state == "ready" then calls[#calls+1] = {"ZREM",run.key .. ":ready_at",v.job_id}; indexes.ready_at = -1 end
    if old.state == "leased" then
        if ctx.keys.active_leases ~= ROOT .. "active_leases" then return nil,"INVALID_STATE" end
        calls[#calls+1] = {"ZREM",run.key .. ":leased_at",v.job_id}
        calls[#calls+1] = {"ZREM",ROOT .. "active_leases",run.run_id .. ":" .. v.job_id}
        indexes.leased_at = -1
    end
    if on.commit_backpressure_started_at_ms > 0 then
        calls[#calls+1] = {"ZREM",run.key .. ":commit_backpressure",v.job_id}; indexes.commit_backpressure = -1
    end
    local at = v.state == "ready" and v.score_text or (v.state == "delayed" and v.not_before_ms or v[v.state .. "_at_ms"])
    calls[#calls+1] = {"ZADD",run.key .. ":" .. v.state,at,v.job_id}
    if v.state == "ready" then calls[#calls+1] = {"ZADD",run.key .. ":ready_at",ctx.now_text,v.job_id}; indexes.ready_at = 1 end
    local counters,maps,times = {},{},{last_activity_at_ms=ctx.now_text}
    if v.state == "delayed" then
        counters.retries_total,maps.retry_reason_counts,times.last_execution_at_ms = 1,{[v.last_reason]=1},ctx.now_text
    elseif v.state ~= "ready" then
        counters.open_job_count,counters[v.state .. "_total"] = -1,1
        maps.group_open_jobs,maps.disposition_reason_counts = {[old.group_id]=-1},{[v.last_reason]=1}
        times.last_terminal_transition_at_ms = ctx.now_text
        if v.last_reason == "published" then counters.output_commits_total,times.last_execution_at_ms = 1,ctx.now_text end
        if ctx.operation == "CJ2_RETRY" and v.last_reason == "retry_exhausted" then times.last_execution_at_ms = ctx.now_text end
    end
    if ctx.operation == "CJ2_RECOVER_EXPIRED" then
        counters.recovered_leases_total,maps.recovery_outcome_counts = 1,{[v.state]=1}
    end
    local marked, e = mark_plan(plan,key); if not marked then return nil,e end
    local ok, failure = CJ.Run.accumulate(delta,{run=counters,maps=maps,indexes=indexes},coverage); if not ok then return nil,failure end
    ok,failure = CJ.Run.set(delta,times,coverage); if not ok then return nil,failure end
    ok,failure = CJ.Run.hset(plan,key,changed,coverage); if not ok then return nil,failure end
    for _, argv in ipairs(calls) do ok,failure = CJ.Plan.add(plan,argv,coverage); if not ok then return nil,failure end end
    return post
end
-- Initial insert effects for ENQUEUE/discovery. The caller owns loading/commit
-- admission, source digest/load_revision and aggregate batch limits. No guessed
-- absence: all twelve private selected memberships and the hash must be absent.
function Job.plan_insert(ctx, plan, delta, run, source, coverage)
    if coverage == nil then coverage = "ordinary" end
    local initial,code = Job.initial_record(ctx,run,source); if not initial then return nil,code end
    local key,err = plan_roles(ctx,run,initial.v.job_id); if not key then return nil,err end
    local before,why = Job.check(job_view(ctx,run.run_id,initial.v.job_id),run,initial.v.job_id)
    if not before or before.exists then return nil,why or "INVALID_STATE" end
    if run.n.last_activity_at_ms > ctx.now_ms then return nil,"INVALID_STATE" end
    local marked,e = mark_plan(plan,key); if not marked then return nil,e end
    local ok,failure = CJ.Run.accumulate(delta,{run={job_count=1,open_job_count=1},maps={group_open_jobs={[initial.v.group_id]=1}},
        indexes={jobs=1,job_order=1,ready=1,ready_at=1}},coverage)
    if not ok then return nil,failure end
    ok,failure = CJ.Run.set(delta,{last_activity_at_ms=ctx.now_text},coverage); if not ok then return nil,failure end
    ok,failure = CJ.Run.hset(plan,key,initial.v,coverage); if not ok then return nil,failure end
    for _, argv in ipairs({{"SADD",run.key .. ":jobs",initial.v.job_id},{"ZADD",run.key .. ":job_order","0",initial.v.job_id},
        {"ZADD",run.key .. ":ready",initial.v.score_text,initial.v.job_id},{"ZADD",run.key .. ":ready_at",ctx.now_text,initial.v.job_id}}) do
        ok,failure = CJ.Plan.add(plan,argv,coverage); if not ok then return nil,failure end
    end
    return initial
end

local function outcome_reply(ctx,status,tail)
    local op,a = ctx.operation,ctx.request.v
    local function past(i)
        local n = P.parse_decimal(tail[i]); return n and n > 0 and n <= ctx.now_ms
    end
    if op ~= "CJ2_REJECT_READY" and status == "LEASE_LOST" and #tail == 1 and P.parse_decimal(tail[1]) ~= nil then return true end
    if op == "CJ2_REJECT_READY" then
        if status == "DEAD" and #tail == 1 and tail[1] == a.reason and ready_reasons[tail[1]] then return true end
    elseif op == "CJ2_RELEASE_BEFORE_IO" then
        if status == "RELEASED_READY" and #tail == 1 and past(1) then return true end
    elseif op == "CJ2_RETRY" then
        if status == "RETRY_SCHEDULED" and #tail == 3 and tail[3] == a.reason and Job.outcome_reason(op,tail[3]) then
            local deadline,attempt = P.parse_decimal(tail[1]),P.parse_decimal(tail[2])
            local delay = attempt == 1 and 30000 or (attempt == 2 and 120000 or nil)
            if deadline and delay and deadline > delay and deadline-delay <= ctx.now_ms then return true end
        elseif status == "DEAD" and #tail == 3 and past(1) and tail[2] == "retry_exhausted" and
            tail[3] == a.reason and Job.outcome_reason(op,tail[3]) then return true end
    elseif status == (op == "CJ2_DEAD" and "DEAD" or "COMPLETED") and op ~= "CJ2_CANCEL_JOB" and
        #tail == 2 and past(1) and tail[2] == a.reason and Job.outcome_reason(op,tail[2]) then return true end
    if (op == "CJ2_REJECT_READY" or op == "CJ2_RELEASE_BEFORE_IO") and
       (status == "AUTHORIZATION_EXPIRED" or status == "RUN_CANCELLED") and #tail == 0 then return true end
    if op ~= "CJ2_REJECT_READY" and op ~= "CJ2_RELEASE_BEFORE_IO" and status == "CANCELLED" and
       #tail == 2 and past(1) and cancel_reasons[tail[2]] and (op ~= "CJ2_CANCEL_JOB" or tail[2] == a.reason) then return true end
    return nil,"INVALID_ARGUMENT"
end
function Job.register_outcome(operation)
    if not outcome_ops[operation] then return nil,"INVALID_ARGUMENT" end
    return CJ.Context.Reply.register(operation,outcome_reply)
end
local function outcome_result(operation,job)
    local v = job.v
    if operation == "CJ2_REJECT_READY" and v.state == "dead" and v.last_transition_status == "DEAD" then return "DEAD",{v.last_reason} end
    if operation == "CJ2_RELEASE_BEFORE_IO" and v.state == "ready" and v.last_transition_status == "RELEASED_READY" then
        return "RELEASED_READY",{v.updated_at_ms}
    end
    if operation == "CJ2_RETRY" then
        if v.state == "delayed" and v.last_transition_status == "RETRY_SCHEDULED" then return "RETRY_SCHEDULED",{v.not_before_ms,v.delivery_attempts,v.last_reason} end
        if v.state == "dead" and v.last_reason == "retry_exhausted" and v.last_transition_status == "DEAD" then
            return "DEAD",{v.dead_at_ms,v.last_reason,v.last_failure_reason}
        end
    elseif operation == "CJ2_DEAD" and v.state == "dead" and v.last_transition_status == "DEAD" then return "DEAD",{v.dead_at_ms,v.last_reason}
    elseif operation == "CJ2_COMPLETE_NO_OUTPUT" and v.state == "completed" and v.last_transition_status == "COMPLETED" and
        v.last_reason == "already_visited" then return "COMPLETED",{v.completed_at_ms,v.last_reason} end
    if operation ~= "CJ2_REJECT_READY" and operation ~= "CJ2_RELEASE_BEFORE_IO" and v.state == "cancelled" and v.last_transition_status == "CANCELLED" then
        return "CANCELLED",{v.cancelled_at_ms,v.last_reason}
    end
    return nil
end
local function outcome_stage(ctx,run,job)
    local slots,code = CJ.Read.slots(ctx); if not slots then return nil,code end
    local owned
    for commit,slot in next,slots.records,nil do
        if slot.run_id == run.run_id and slot.job_id == job.v.job_id then
            if owned or slot.fence ~= job.n.lease_fence then return nil,"STAGE_INVALID" end
            owned = commit
            if slot.abort_unlinked_keys == 0 then return nil,"INVALID_STATE" end
        end
    end
    if job.v.active_stage_commit_id ~= "" then return nil,"INVALID_STATE" end
    if not owned then
        if job.v.state == "leased" and job.n.last_stage_fence > 0 and job.n.last_stage_fence == job.n.lease_fence then return nil,"STAGE_INVALID" end
        return false
    end
    if job.v.state ~= "leased" then return nil,"STAGE_INVALID" end
    if owned ~= job.v.last_stage_commit_id or not CJ.Stage then return nil,"STAGE_INVALID" end
    local bound,err = CJ.Context.bind_stage(ctx,job.v.job_id); if not bound then return nil,err end
    local keys,why = CJ.Stage.keys(owned); if not keys then return nil,why end
    local fact,e = CJ.Read.hash_fields(ctx,ROOT .. "stage_slots",{owned},4,64,126); if not fact then return nil,e end
    fact,e = CJ.Read.members(ctx,ROOT .. "stage_expiry","zset",{owned},100000,64); if not fact then return nil,e end
    -- An aborted stage permits ONLY absence reads, never deleting a guessed
    -- stage prefix. All 73 exact key roles are checked by Stage.check_aborted.
    for _,key in ipairs(keys.ordered) do
        fact,e = CJ.Read.absent(ctx,key,keys.kind[key]); if not fact then return nil,e end
    end
    local proof, failure = CJ.Stage.check_aborted({ctx=ctx},run,job,{run_id=run.run_id,job_id=job.v.job_id,
        owner_id=job.v.lease_owner,lease_token=job.v.lease_token,fence=job.v.lease_fence},owned)
    if not proof then return nil,failure == "LEASE_LOST" and "STAGE_INVALID" or failure end
    return proof
end
local function retry_condition(ctx,job,reason)
    if reason == "downstream_backpressure" then
        if job.n.last_stage_fence < job.n.lease_fence then return true end
        if job.v.commit_backpressure_reason ~= "none" and job.v.commit_backpressure_fence == job.v.lease_fence and
           ctx.now_ms >= job.n.commit_backpressure_deadline_ms then return true end
        return nil,"INVALID_ARGUMENT"
    end
    -- The four run/group budget, capacity and rate *_after_io caller reasons
    -- assert a matching PAST typed blocked result from the pinned
    -- client. RETRY carries only lease/reason/transition ID; denied attempts
    -- deliberately persist no job/reservation receipt. Availability may already
    -- have returned, and a redirect may have charged a different group/origin.
    -- Do not re-check today's availability or invent a historical attestation.
    -- Gate, identity, live lease, G>B/delivery=1, request/stage exclusion and
    -- cancellation precedence remain server checks. M5 still MUST implement the
    -- typed client mapping (§9.3/§10.1); Go constructors do not authenticate it.
    return true
end
-- Actual six-operation preparation. Fragments own only spec/open + the fixed
-- executor. Replays are reconciled BEFORE active lease/run admission, but AFTER
-- exact control identity, source, complete Job/Run receipts and timestamp proof.
function Job.prepare_outcome(ctx)
    local op,a = ctx.operation,ctx.request.v
    if not outcome_ops[op] then return nil,"INVALID_ARGUMENT" end
    local gate,code = CJ.Gate.check(ctx); if not gate then return nil,code end
    local run,err = CJ.Run.load(ctx,a.run_id); if not run then return nil,err end
    local identity,why = Job.outcome_identity(op,a,run); if not identity then return nil,why end
    if not I.digest(a.transition_id) or identity ~= a.transition_id then return nil,"IMMUTABLE_MISMATCH" end
    local mutable,e = CJ.Run.mutable(ctx,run); if not mutable then return nil,e end
    local job,je = Job.load(ctx,run,a.job_id); if not job then return nil,je end
    local plan,pe = CJ.Plan.new(ctx); if not plan then return nil,pe end
    local function finish(status,tail) return CJ.Run.finish(ctx,plan,status,tail or {}) end
    if not job.exists then
        if op == "CJ2_REJECT_READY" then return nil,"INVALID_STATE" end
        return finish("LEASE_LOST",{"0"})
    end
    local at,ae = Job.at(ctx,run,job); if not at then return nil,ae end
    if op == "CJ2_REJECT_READY" then
        for _,field in ipairs(source_names) do if a[field] ~= job.v[field] then return nil,"IMMUTABLE_MISMATCH" end end
    end
    if job.v.last_transition_id == identity then
        if op ~= "CJ2_REJECT_READY" and a.fence ~= job.v.lease_fence then return finish("LEASE_LOST",{job.v.lease_fence}) end
        local status,tail = outcome_result(op,job)
        if status then return finish(status,tail) end
        if op ~= "CJ2_REJECT_READY" then return finish("LEASE_LOST",{job.v.lease_fence}) end
        return nil,"INVALID_STATE"
    end
    if op ~= "CJ2_REJECT_READY" and (job.v.state ~= "leased" or job.v.lease_owner ~= a.owner_id or job.v.lease_token ~= a.lease_token or
        job.v.lease_fence ~= a.fence or job.n.lease_expires_at_ms <= ctx.now_ms) then return finish("LEASE_LOST",{job.v.lease_fence}) end
    local cancellation,ce = Job.cancellation(ctx,run); if cancellation == nil then return nil,ce end
    if run.v.state ~= "active" and run.v.state ~= "cancelled" then return nil,"INVALID_STATE" end
    if (op == "CJ2_REJECT_READY" or op == "CJ2_RELEASE_BEFORE_IO") and cancellation then
        -- Section 10.3 RELEASE has only definitive no-tail cancellation/expiry
        -- responses. Its caller then cancels pending capacity and uses CANCEL_JOB;
        -- inventing a CANCELLED tuple here would violate the closed Go wire.
        return finish(run.v.state == "cancelled" and "RUN_CANCELLED" or "AUTHORIZATION_EXPIRED")
    end
    if op == "CJ2_REJECT_READY" and job.v.state ~= "ready" then return nil,"INVALID_STATE" end
    if op == "CJ2_RELEASE_BEFORE_IO" and (job.n.lease_delivery_started ~= 0 or job.n.request_starts ~= job.n.lease_request_starts_baseline) then
        return nil,"INVALID_STATE"
    end
    if op ~= "CJ2_RELEASE_BEFORE_IO" and job.v.active_reservation_id ~= "" then return nil,"INVALID_STATE" end
    if op == "CJ2_CANCEL_JOB" and (not cancellation or a.reason ~= cancellation) then return nil,"INVALID_ARGUMENT" end
    local stage,se = outcome_stage(ctx,run,job); if stage == nil then return nil,se end
    local changes = {last_transition_id=identity}
    if cancellation then changes.state,changes.last_reason,changes.last_transition_status = "cancelled",cancellation,"CANCELLED"
    elseif op == "CJ2_RELEASE_BEFORE_IO" then changes.state,changes.last_reason,changes.last_transition_status = "ready","none","RELEASED_READY"
    elseif op == "CJ2_RETRY" then
        if job.n.request_starts <= job.n.lease_request_starts_baseline or job.n.lease_delivery_started ~= 1 then return nil,"INVALID_STATE" end
        local valid,re = retry_condition(ctx,job,a.reason); if not valid then return nil,re end
        changes.last_failure_reason = a.reason
        if job.n.delivery_attempts == 3 then
            changes.state,changes.last_reason,changes.last_transition_status = "dead","retry_exhausted","DEAD"
        else
            local deadline = P.safe_add(ctx.now_ms,job.n.delivery_attempts == 1 and 30000 or 120000)
            if not deadline then return nil,"INVALID_NUMBER" end
            changes.state,changes.last_reason,changes.last_transition_status = "delayed",a.reason,"RETRY_SCHEDULED"
            changes.not_before_ms,changes.retry_count = P.format_decimal(deadline),P.format_decimal(job.n.retry_count+1)
        end
    elseif op == "CJ2_COMPLETE_NO_OUTPUT" then changes.state,changes.last_reason,changes.last_transition_status = "completed","already_visited","COMPLETED"
    else
        if a.reason == "reservation_limit_exhausted" and
           (job.n.request_starts <= job.n.lease_request_starts_baseline or run.n.reservation_creations_total ~= 100 or
            run.n.request_starts+run.n.pending_request_reservations >= run.n.max_request_starts) then return nil,"INVALID_ARGUMENT" end
        changes.state,changes.last_reason,changes.last_failure_reason,changes.last_transition_status = "dead",a.reason,a.reason,"DEAD"
    end
    local proposed,ve = Job.outcome_record(ctx,job,changes); if not proposed then return nil,ve end
    local delta,de = CJ.Run.plan_delta(ctx,run); if not delta then return nil,de end
    if job.v.active_reservation_id ~= "" then
        if not CJ.Request or type(CJ.Request.load_live) ~= "function" or type(CJ.Request.plan_terminal) ~= "function" then return nil,"INVALID_STATE" end
        local held,he = CJ.Request.load_live(ctx,run,job)
        if not held then return nil,he end
        if held.v.state ~= "pending" or held.logical_expired then return nil,"RESERVATION_CORRUPT" end
    end
    local coverage = "ordinary"
    if stage then
        -- The core owns both self-inclusive G and the final slot removal. Never
        -- substitute public .remaining, duplicate an allocation formula, or use
        -- ordinary admission for writes promised to be covered by this slot.
        local selected,me = CJ.Plan.set_policy(plan,"slot_end",stage); if not selected then return nil,me end
    elseif op ~= "CJ2_REJECT_READY" then
        local selected,me = CJ.Plan.set_policy(plan,"safety"); if not selected then return nil,me end
    end
    if job.v.active_reservation_id ~= "" then
        local released,re = CJ.Request.plan_terminal(ctx,plan,delta,run,job,"cancelled",coverage)
        if not released then return nil,re end
        -- Request has already planned capacity/tombstone effects and contributed
        -- to delta. Compose ONLY its returned Job fields into our one Job write.
        if released.job_key ~= job.key or not plain(released.job_fields) or
           released.job_fields.active_reservation_id ~= "" or released.job_fields.updated_at_ms ~= ctx.now_text then
            return nil,"INVALID_STATE"
        end
        local values = copy_values(proposed.v)
        for field,value in next,released.job_fields,nil do
            if field ~= "active_reservation_id" and field ~= "updated_at_ms" then return nil,"INVALID_STATE" end
            values[field] = value
        end
        proposed,re = S.project("job",values); if not proposed then return nil,re end
    end
    local post,oe = Job.plan_outcome(ctx,plan,delta,run,job,proposed,coverage); if not post then return nil,oe end
    local flushed,fe = CJ.Run.flush(ctx,plan,delta,coverage); if not flushed then return nil,fe end
    local status,tail = outcome_result(op,post)
    if not status then return nil,"INVALID_STATE" end
    return finish(status,tail)
end

return Job
