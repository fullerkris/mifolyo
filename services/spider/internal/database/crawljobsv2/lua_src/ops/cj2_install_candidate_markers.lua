-- Complete INSTALL planner. Evidence is exact trusted-tool propagation, NOT
-- proof that this script observed process stops or authorized a deployment.
local function prepare()
    local ctx, code = CJ.Context.open(CJ.Wire.install,KEYS,ARGV)
    if not ctx then return nil, code end
    local v = ctx.request.v
    if not CJ.Identities.hex(v.freeze_nonce,32) then return nil, "INVALID_IDENTIFIER" end
    if not CJ.Identities.hex(v.process_stop_evidence_sha256,64) or not CJ.Identities.hex(v.contract_sha256,64) then
        return nil, "INVALID_IDENTIFIER"
    end
    if not CJ.Identities.digest(v.process_stop_evidence_sha256) or not CJ.Identities.digest(v.contract_sha256) then
        return nil, "INVALID_ARGUMENT"
    end
    local marker, err = CJ.Schemas.project("compatibility_marker",v)
    if not marker then return nil, err end
    local view, failure = CJ.Gate.check(ctx)
    if not view then return nil, failure end
    local candidate, contract, freeze = view.candidate_compatibility, view.candidate_contract, view.admin_freeze
    local replay = candidate.exists and contract.exists and freeze.exists
    -- Validate the complete post-state FIRST, before fresh absence/admission.
    if replay then
        for i = 1, #marker.fields do
            local field = marker.fields[i]
            if candidate.v[field[1]] ~= field[2] then return nil, "IMMUTABLE_MISMATCH" end
        end
        if contract.value ~= v.contract_sha256 or freeze.v.freeze_nonce ~= v.freeze_nonce or
           freeze.v.process_stop_evidence_sha256 ~= v.process_stop_evidence_sha256 or
           freeze.v.candidate_manifest_sha256 ~= v.manifest_sha256 or freeze.v.candidate_contract_sha256 ~= v.contract_sha256 then
            return nil, "IMMUTABLE_MISMATCH"
        end
    elseif candidate.exists or contract.exists or freeze.exists then
        return nil, "IMMUTABLE_MISMATCH"
    end
    local plan, plan_err = CJ.Plan.new(ctx)
    if not plan then return nil, plan_err end
    if not replay then
        local frozen, freeze_err = CJ.Schemas.project("admin_freeze",{
            protocol_version="2",freeze_nonce=v.freeze_nonce,process_stop_evidence_sha256=v.process_stop_evidence_sha256,
            candidate_manifest_sha256=v.manifest_sha256,candidate_contract_sha256=v.contract_sha256,created_at_ms=ctx.now_text})
        if not frozen then return nil, freeze_err end
        local marker_write, freeze_write = {"HSET",ctx.keys.candidate_compatibility}, {"HSET",ctx.keys.admin_freeze}
        for i = 1, #marker.fields do
            marker_write[#marker_write+1] = marker.fields[i][1]
            marker_write[#marker_write+1] = marker.fields[i][2]
        end
        for i = 1, #frozen.fields do
            freeze_write[#freeze_write+1] = frozen.fields[i][1]
            freeze_write[#freeze_write+1] = frozen.fields[i][2]
        end
        for _, argv in ipairs({marker_write,{"SET",ctx.keys.candidate_contract,v.contract_sha256},freeze_write}) do
            local added, add_err = CJ.Plan.add(plan,argv,"ordinary")
            if not added then return nil, add_err end
        end
    end
    local reply, reply_err = CJ.Context.Reply.build(ctx,replay and "EXISTS_IDENTICAL" or "CANDIDATE_INSTALLED",
        {v.manifest_sha256,v.contract_sha256})
    if not reply then return nil, reply_err end
    local assessment, assess_err = CJ.Plan.assess(ctx,plan)
    if not assessment then return nil, assess_err end
    return CJ.Plan.seal(ctx,plan,assessment,reply)
end
local execution, code = prepare()
if not execution then return CJ.Context.reject(code) end
-- SEALED EXECUTOR: no reads, callbacks, hashes, derivations, table construction,
-- reply-dependent branching, or error swallowing. Redis errors do NOT roll back.
for i = 1, execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
