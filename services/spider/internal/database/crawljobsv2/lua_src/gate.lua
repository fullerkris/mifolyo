local G, C, R, W, S, I = {}, CJ.Context, CJ.Read, CJ.Wire, CJ.Schemas, CJ.Identities
local function exact(stored, expected)
    if not stored.exists or not expected then return false end
    for i = 1, #expected.fields do
        local field = expected.fields[i]
        if stored.v[field[1]] ~= field[2] then return false end
    end
    return true
end
local function promotion_core(ctx)
    local v, gate = ctx.request.v, ctx.request.gate
    local freeze, marker, legacy = gate.records.admin_freeze, gate.records.compatibility_marker, gate.records.legacy_retirement
    if gate.mode ~= "candidate" or not freeze or not marker or not legacy then return nil, "INVALID_ARGUMENT" end
    if not I.hex(v.freeze_nonce,32) or v.freeze_nonce ~= freeze.v.freeze_nonce or v.freeze_nonce ~= legacy.v.freeze_nonce then
        return nil, "IMMUTABLE_MISMATCH"
    end
    local core, code = S.project("guard_core",v)
    if not core then return nil, code end
    if core.v.contract_sha256 ~= gate.contract then return nil, "CONTRACT_MISMATCH" end
    local encoded, err = S.encode(core)
    if not encoded then return nil, err end
    local digest = P.sha256(encoded)
    if not digest then return nil, "INVALID_STATE" end
    if v.commit_guard_sha256 ~= digest or marker.v.commit_guard_sha256 ~= digest or
       marker.v.redis_config_sha256 ~= core.v.redis_config_sha256 then return nil, "COMMIT_GUARD_UNAPPROVED" end
    if (core.v.cutover_mode == "fresh" and legacy.n.v1_count ~= 0) or
       (core.v.cutover_mode == "v1_migration" and legacy.n.v1_count == 0) then return nil, "IMMUTABLE_MISMATCH" end
    return core
end
local function promoted_receipt(ctx, boot, core)
    -- Any active residue selects the post-state check; partial/conflicting
    -- promotion never falls back to a fresh candidate mutation or repairs keys.
    local active, code = R.project(ctx,{
        {name="active_compatibility",key=ctx.keys.active_compatibility,kind="hash",schema="compatibility_marker"},
        {name="active_contract",key=ctx.keys.active_contract,kind="string",maximum=64,digest=true},
        {name="commit_guard",key=ctx.keys.commit_guard,kind="hash",schema="commit_guard"}
    })
    if not active then return nil, code end
    local marker, contract, guard = active.active_compatibility, active.active_contract, active.commit_guard
    if not marker.exists and not contract.exists and not guard.exists then return false end
    if not marker.exists or not contract.exists or not guard.exists then return nil, "IMMUTABLE_MISMATCH" end
    local gate = ctx.request.gate
    if not exact(marker,gate.records.compatibility_marker) then return nil, "COMPATIBILITY_MISMATCH" end
    if contract.value ~= gate.contract then return nil, "CONTRACT_MISMATCH" end
    for i = 1, #core.fields do
        local field = core.fields[i]
        if guard.v[field[1]] ~= field[2] then return nil, "IMMUTABLE_MISMATCH" end
    end
    if guard.v.compatibility_manifest_sha256 ~= marker.v.manifest_sha256 then return nil, "COMPATIBILITY_MISMATCH" end
    local post, err = R.project(ctx,{
        {name="candidate_compatibility",key=ctx.keys.candidate_compatibility,kind="absent",expected_type="hash"},
        {name="candidate_contract",key=ctx.keys.candidate_contract,kind="absent",expected_type="string"},
        {name="admin_freeze",key=ctx.keys.admin_freeze,kind="absent",expected_type="hash"},
        {name="legacy_retirement",key=ctx.keys.legacy_retirement,kind="hash",schema="legacy_retirement"}
    })
    if not post then return nil, err end
    if not exact(post.legacy_retirement,gate.records.legacy_retirement) then return nil, "IMMUTABLE_MISMATCH" end
    for name, fact in next, post, nil do active[name] = fact end
    active.durability, active.receipt_only, active.kind = boot, true, "promoted"
    local locked, failure = C.lock_receipt(ctx,"promoted")
    if not locked then return nil, failure end
    return active
end
function G.check(ctx)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    -- The AUTH prefix is fixed even for source-owned future operation specs.
    for i = 1, 8 do
        if ctx.keys[W.authority_names[i]] ~= W.authority_keys[i] then return nil, "INVALID_ARGUMENT" end
    end
    local boot, code = R.fixed_hash(ctx,ctx.keys.durability,"durability")
    if not boot then return nil, code end
    if not boot.exists then return nil, "BOOT_UNAPPROVED" end
    local server = C.call(ctx,"INFO","SERVER")
    local info = I.info(server,{"run_id"})
    local gate = ctx.request.gate
    if not info or not I.hex(info.run_id,40) or
       boot.v.approved_redis_run_id ~= info.run_id or boot.v.boot_epoch ~= gate.boot_epoch then return nil, "BOOT_UNAPPROVED" end
    local planned_receipt = false
    if boot.v.boot_state ~= "approved" then
        if ctx.operation ~= "CJ2_MARK_PLANNED_SHUTDOWN" or gate.mode ~= "active" or boot.v.boot_state ~= "planned" then
            return nil, "BOOT_UNAPPROVED"
        end
        local v = ctx.request.v
        if not I.hex(v.planned_shutdown_nonce,32) or not I.digest(v.process_stop_evidence_sha256) or
           boot.v.planned_shutdown_nonce ~= v.planned_shutdown_nonce or
           boot.v.planned_shutdown_evidence_sha256 ~= v.process_stop_evidence_sha256 then return nil, "IMMUTABLE_MISMATCH" end
        planned_receipt = true
    end
    if ctx.operation == "CJ2_PROMOTE_CANDIDATE_CONTRACTS" then
        local core, err = promotion_core(ctx)
        if not core then return nil, err end
        local receipt, failure = promoted_receipt(ctx,boot,core)
        if receipt == nil then return nil, failure end
        if receipt then return receipt end
    end
    local needs = {}
    local function hash(name,schema)
        needs[#needs+1] = {name=name,key=ctx.keys[name],kind="hash",schema=schema}
    end
    local function absent(name,kind)
        needs[#needs+1] = {name=name,key=ctx.keys[name],kind="absent",expected_type=kind}
    end
    local function contract(name)
        needs[#needs+1] = {name=name,key=ctx.keys[name],kind="string",maximum=64,digest=true}
    end
    if gate.mode == "boot_only" then
        if ctx.operation ~= "CJ2_INSTALL_CANDIDATE_MARKERS" then return nil, "INVALID_ARGUMENT" end
        absent("active_compatibility","hash"); absent("active_contract","string"); absent("commit_guard","hash")
        absent("legacy_retirement","hash")
        hash("candidate_compatibility","compatibility_marker"); contract("candidate_contract"); hash("admin_freeze","admin_freeze")
    elseif gate.mode == "candidate" then
        absent("active_compatibility","hash"); absent("active_contract","string"); absent("commit_guard","hash")
        hash("candidate_compatibility","compatibility_marker"); contract("candidate_contract"); hash("admin_freeze","admin_freeze")
        if gate.records.legacy_retirement then hash("legacy_retirement","legacy_retirement") else absent("legacy_retirement","hash") end
    elseif gate.mode == "active" then
        absent("candidate_compatibility","hash"); absent("candidate_contract","string"); absent("admin_freeze","hash")
        hash("active_compatibility","compatibility_marker"); contract("active_contract")
        hash("commit_guard","commit_guard"); hash("legacy_retirement","legacy_retirement")
    else return nil, "INVALID_ARGUMENT" end
    local view, err = R.project(ctx,needs)
    if not view then return nil, err end
    view.durability = boot
    view.receipt_only, view.kind = false, gate.mode
    if gate.mode ~= "boot_only" then
        local marker = gate.mode == "active" and view.active_compatibility or view.candidate_compatibility
        local stored_contract = gate.mode == "active" and view.active_contract or view.candidate_contract
        if not exact(marker,gate.records.compatibility_marker) then return nil, "COMPATIBILITY_MISMATCH" end
        if not stored_contract.exists or stored_contract.value ~= gate.contract then return nil, "CONTRACT_MISMATCH" end
        for _, pair in ipairs({{"commit_guard","commit_guard"},{"legacy_retirement","legacy_retirement"},{"admin_freeze","admin_freeze"}}) do
            local expected = gate.records[pair[2]]
            if expected and not exact(view[pair[1]],expected) then return nil, "IMMUTABLE_MISMATCH" end
        end
    end
    if planned_receipt then
        local locked, failure = C.lock_receipt(ctx,"planned_shutdown")
        if not locked then return nil, failure end
        view.receipt_only, view.kind = true, "planned_shutdown"
    end
    -- INSTALL's only absence exception is checked by its planner: complete
    -- identical candidate pair + freeze, never repair of a partial post-state.
    return view
end
return G
