local R, C, I, S = {}, CJ.Context, CJ.Identities, CJ.Schemas
local caches = {}
local slot_key = "mifolyo:crawl:v2:stage_slots"
local cardinal_commands = {hash="HLEN",set="SCARD",zset="ZCARD",list="LLEN"}
local function copy(value)
    if type(value) ~= "table" then return value end
    local result = {}
    for k, v in next, value, nil do result[k] = copy(v) end
    return result
end
local function absent(key)
    return {key=key,kind="none",exists=false,count=0,complete=true,v={},members={},scores={},score_text={},ttl_ms=-2}
end
local function cache(ctx)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    if not caches[ctx] then caches[ctx] = {types={},facts={}} end
    return caches[ctx]
end
local function bounded_text(s, maximum)
    return type(s) == "string" and #s <= maximum and P.validate_text(s) == true
end
function R.key_type(ctx, key)
    local state, code = cache(ctx)
    if not state then return nil, code end
    if not C.can_read(ctx,key) then return nil, "INVALID_ARGUMENT" end
    if state.types[key] then return state.types[key] end
    local reply, err = C.call(ctx,"TYPE",key)
    if not reply then return nil, err end
    if type(reply) ~= "table" or type(reply.ok) ~= "string" then return nil, "INVALID_STATE" end
    local t = reply.ok
    if t ~= "none" and t ~= "hash" and t ~= "string" and t ~= "set" and t ~= "zset" and t ~= "list" and t ~= "stream" then return nil, "WRONG_TYPE" end
    state.types[key] = t
    if t == "none" then state.facts[key] = absent(key) end
    return t
end
function R.cardinality(ctx, key, kind, maximum)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    if not cardinal_commands[kind] or not I.integer(maximum,P.limits.max_integer) then return nil, "INVALID_ARGUMENT" end
    local actual, code = R.key_type(ctx,key)
    if not actual then return nil, code end
    local state = caches[ctx]
    if actual == "none" then return copy(state.facts[key]) end
    if actual ~= kind then return nil, "WRONG_TYPE" end
    local fact = state.facts[key]
    local count = fact and fact.count
    if count == nil then count = C.call(ctx,cardinal_commands[kind],key) end
    if not I.integer(count,P.limits.max_integer) or count == 0 then return nil, "INVALID_STATE" end
    if count > maximum then return nil, "LIMIT_EXCEEDED" end
    if not fact then fact = {key=key,kind=kind,exists=true,complete=false,v={},members={},scores={},score_text={}} end
    fact.count, state.facts[key] = count, fact
    return copy(fact)
end
local function complete_if_covered(fact, values)
    local present = 0
    for _, value in next, values, nil do if value ~= false then present = present + 1 end end
    if present > fact.count then return nil, "INVALID_STATE" end
    if present == fact.count then fact.complete = true end
    return true
end
function R.fixed_hash(ctx, key, schema)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    local def = S.get(schema)
    if not def then return nil, "INVALID_ARGUMENT" end
    local kind, code = R.key_type(ctx,key)
    if not kind then return nil, code end
    local state = caches[ctx]
    if kind == "none" then return copy(state.facts[key]) end
    if kind ~= "hash" then return nil, "WRONG_TYPE" end
    local existing = state.facts[key]
    if existing and existing.schema == schema then return copy(existing) end
    if existing and existing.schema ~= nil then return nil, "INVALID_STATE" end
    local count = existing and existing.count
    if count == nil then count = C.call(ctx,"HLEN",key) end
    if count ~= #def.names then return nil, "INVALID_STATE" end
    local values = {}
    if existing and existing.complete then
        for i = 1, #def.names do values[def.names[i]] = existing.v[def.names[i]] end
    else
        local lengths = {}
        for i = 1, #def.names do
            local length = C.call(ctx,"HSTRLEN",key,def.names[i])
            if not I.integer(length,def.bounds[i]) then return nil, "INVALID_STATE" end
            lengths[i] = length
        end
        local raw = C.call(ctx,"HMGET",key,unpack(def.names,1,#def.names))
        if I.dense(raw,#def.names) ~= #def.names then return nil, "INVALID_STATE" end
        for i = 1, #def.names do
            if type(raw[i]) ~= "string" or #raw[i] ~= lengths[i] then return nil, "INVALID_STATE" end
            if existing and existing.v[def.names[i]] ~= nil and existing.v[def.names[i]] ~= raw[i] then return nil, "INVALID_STATE" end
            values[def.names[i]] = raw[i]
        end
    end
    local fact = S.project(schema,values)
    if not fact then return nil, "INVALID_STATE" end
    fact.kind, fact.key, fact.exists, fact.complete, fact.count = "hash", key, true, true, count
    if existing then fact.ttl_ms = existing.ttl_ms end
    state.facts[key] = fact
    return copy(fact)
end
-- Selected hash fields, explicit false for absent, nil for unrequested. Every
-- selected value length is checked before any HMGET; requests are chunked, not
-- truncated, at 256 fields per Redis call.
function R.hash_fields(ctx, key, fields, maximum, field_bytes, value_bytes)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    local count = I.dense(fields,20000)
    if not count or not I.integer(field_bytes,5373952) or not I.integer(value_bytes,10485760) then return nil, "INVALID_ARGUMENT" end
    local seen = {}
    for i = 1, count do
        if not bounded_text(fields[i],field_bytes) or seen[fields[i]] then return nil, "INVALID_ARGUMENT" end
        seen[fields[i]] = true
    end
    local pre, code = R.cardinality(ctx,key,"hash",maximum)
    if not pre then return nil, code end
    local fact = caches[ctx].facts[key]
    local missing, lengths = {}, {}
    for i = 1, count do
        local field = fields[i]
        if fact.v[field] == nil and not fact.complete then
            local length = C.call(ctx,"HSTRLEN",key,field)
            if not I.integer(length,value_bytes) then return nil, "INVALID_STATE" end
            missing[#missing+1], lengths[field] = field, length
        elseif fact.v[field] ~= nil and fact.v[field] ~= false and not bounded_text(fact.v[field],value_bytes) then
            return nil, "INVALID_STATE"
        end
    end
    for offset = 1, #missing, 256 do
        local finish = math.min(offset+255,#missing)
        local raw = C.call(ctx,"HMGET",key,unpack(missing,offset,finish))
        if I.dense(raw,256) ~= finish-offset+1 then return nil, "INVALID_STATE" end
        for i = offset, finish do
            local value, field = raw[i-offset+1], missing[i]
            if value == false then
                if lengths[field] ~= 0 then return nil, "INVALID_STATE" end
            elseif not bounded_text(value,value_bytes) or #value ~= lengths[field] then return nil, "INVALID_STATE" end
            fact.v[field] = value
        end
    end
    if fact.exists then
        local ok, err = complete_if_covered(fact,fact.v)
        if not ok then return nil, err end
    end
    -- Complete absence is safe to expose explicitly for requested fields too.
    for i = 1, count do if fact.complete and fact.v[fields[i]] == nil then fact.v[fields[i]] = false end end
    return copy(fact)
end
function R.dynamic_hash(ctx, key, maximum, field_bytes, value_bytes)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    if not I.integer(maximum,20000) or not I.integer(field_bytes,5373952) or not I.integer(value_bytes,10485760) then return nil, "INVALID_ARGUMENT" end
    local pre, code = R.cardinality(ctx,key,"hash",maximum)
    if not pre then return nil, code end
    local fact, names = caches[ctx].facts[key], {}
    if fact.complete then
        for field, value in next, fact.v, nil do if value ~= false then names[#names+1] = field end end
    else
        -- Cardinality is bounded first. Redis has no HKEYS field-length probe:
        -- hostile name bytes can still allocate a large reply (README caveat).
        names = C.call(ctx,"HKEYS",key)
        if I.dense(names,maximum) ~= fact.count then return nil, "INVALID_STATE" end
    end
    local seen = {}
    for i = 1, #names do
        if not bounded_text(names[i],field_bytes) or seen[names[i]] then return nil, "INVALID_STATE" end
        seen[names[i]] = true
    end
    table.sort(names)
    local result, err = R.hash_fields(ctx,key,names,maximum,field_bytes,value_bytes)
    if not result then return nil, err end
    if not result.complete then return nil, "INVALID_STATE" end
    for i = 1, #names do if result.v[names[i]] == false then return nil, "INVALID_STATE" end end
    result.names = names
    return result
end
function R.string(ctx, key, maximum, digest)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    if not I.integer(maximum,5373952) or type(digest) ~= "boolean" then return nil, "INVALID_ARGUMENT" end
    local kind, code = R.key_type(ctx,key)
    if not kind then return nil, code end
    local state = caches[ctx]
    if kind == "none" then return copy(state.facts[key]) end
    if kind ~= "string" then return nil, "WRONG_TYPE" end
    local fact = state.facts[key]
    if not fact or not fact.complete then
        local length = C.call(ctx,"STRLEN",key)
        if not I.integer(length,maximum) then return nil, "INVALID_STATE" end
        local value = C.call(ctx,"GET",key)
        if type(value) ~= "string" or #value ~= length then return nil, "INVALID_STATE" end
        local old = fact
        fact = {key=key,kind="string",exists=true,complete=true,value=value}
        if old then fact.ttl_ms = old.ttl_ms end
    end
    if #fact.value > maximum or (digest and not I.digest(fact.value)) then return nil, "INVALID_STATE" end
    state.facts[key] = fact
    return copy(fact)
end
function R.absent(ctx, key, expected_type)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    if expected_type ~= "string" and not cardinal_commands[expected_type] then return nil, "INVALID_ARGUMENT" end
    local kind, code = R.key_type(ctx,key)
    if not kind then return nil, code end
    if kind ~= "none" then
        if kind ~= expected_type then return nil, "WRONG_TYPE" end
        return nil, "INVALID_STATE"
    end
    return copy(caches[ctx].facts[key])
end
local function remember_member(fact, member, present, score, raw)
    local old = fact.members[member]
    if old ~= nil and old ~= present then return nil, "INVALID_STATE" end
    if fact.complete and old == nil and present then return nil, "INVALID_STATE" end
    if present and fact.kind == "zset" and fact.scores[member] ~= nil and fact.scores[member] ~= score then return nil, "INVALID_STATE" end
    fact.members[member] = present
    if fact.kind == "zset" then fact.scores[member], fact.score_text[member] = score, raw end
    return complete_if_covered(fact,fact.members)
end
function R.members(ctx, key, kind, members, maximum, member_bytes)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    local count = I.dense(members,10000)
    if not count or (kind ~= "set" and kind ~= "zset") or not I.integer(member_bytes,5373952) then return nil, "INVALID_ARGUMENT" end
    local seen = {}
    for i = 1, count do
        if not bounded_text(members[i],member_bytes) or seen[members[i]] then return nil, "INVALID_ARGUMENT" end
        seen[members[i]] = true
    end
    local pre, code = R.cardinality(ctx,key,kind,maximum)
    if not pre then return nil, code end
    local fact = caches[ctx].facts[key]
    for i = 1, count do
        local member = members[i]
        if fact.members[member] == nil then
            if fact.complete then
                fact.members[member], fact.scores[member], fact.score_text[member] = false, false, false
            else
                local present, score, raw
                if kind == "set" then
                    raw = C.call(ctx,"SISMEMBER",key,member)
                    if raw ~= 0 and raw ~= 1 then return nil, "INVALID_STATE" end
                    present = raw == 1
                else
                    raw = C.call(ctx,"ZSCORE",key,member)
                    present, score = raw ~= false, false
                    if present then
                        score = I.redis_score(raw)
                        if score == nil then return nil, "INVALID_STATE" end
                    end
                end
                local ok, err = remember_member(fact,member,present,score,raw)
                if not ok then return nil, err end
            end
        end
    end
    return copy(fact)
end
local function collect_zset(fact, raw, expected, member_bytes)
    if I.dense(raw,200000) ~= 2*expected then return nil, "INVALID_STATE" end
    local ordered, previous, previous_score = {}, nil, nil
    for i = 1, expected do
        local member, text = raw[2*i-1], raw[2*i]
        local score = I.redis_score(text)
        if not bounded_text(member,member_bytes) or score == nil or (previous and
           (score < previous_score or (score == previous_score and member <= previous))) then return nil, "INVALID_STATE" end
        local ok, code = remember_member(fact,member,true,score,text)
        if not ok then return nil, code end
        ordered[i], previous, previous_score = member, member, score
    end
    return ordered
end
function R.all_members(ctx, key, kind, maximum, member_bytes)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    if (kind ~= "set" and kind ~= "zset") or not I.integer(maximum,100000) or not I.integer(member_bytes,5373952) then return nil, "INVALID_ARGUMENT" end
    local pre, code = R.cardinality(ctx,key,kind,maximum)
    if not pre then return nil, code end
    local fact, ordered = caches[ctx].facts[key], {}
    if fact.complete then
        for member, present in next, fact.members, nil do if present then ordered[#ordered+1] = member end end
        if kind == "zset" then table.sort(ordered,function(a,b) return fact.scores[a] < fact.scores[b] or fact.scores[a] == fact.scores[b] and a < b end)
        else table.sort(ordered) end
    elseif kind == "set" then
        ordered = C.call(ctx,"SMEMBERS",key)
        if I.dense(ordered,maximum) ~= fact.count then return nil, "INVALID_STATE" end
        for i = 1, #ordered do if not bounded_text(ordered[i],member_bytes) then return nil, "INVALID_STATE" end end
        table.sort(ordered)
        for i = 1, #ordered do
            if not bounded_text(ordered[i],member_bytes) or (i > 1 and ordered[i] == ordered[i-1]) then return nil, "INVALID_STATE" end
            local ok, err = remember_member(fact,ordered[i],true)
            if not ok then return nil, err end
        end
    else
        local raw = C.call(ctx,"ZRANGE",key,"0",P.format_decimal(fact.count-1),"WITHSCORES")
        local err
        ordered, err = collect_zset(fact,raw,fact.count,member_bytes)
        if not ordered then return nil, err end
    end
    for i = 1, #ordered do if not bounded_text(ordered[i],member_bytes) then return nil, "INVALID_STATE" end end
    local result = copy(fact); result.ordered = ordered
    return result
end
function R.page(ctx, key, kind, offset, limit, maximum, member_bytes)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    if (kind ~= "list" and kind ~= "zset") or not I.integer(offset,100000) or not I.integer(limit,500) or
       limit == 0 or not I.integer(member_bytes,5373952) then return nil, "INVALID_ARGUMENT" end
    local pre, code = R.cardinality(ctx,key,kind,maximum)
    if not pre then return nil, code end
    local fact, ordered = caches[ctx].facts[key], {}
    local count = math.max(0,math.min(limit,fact.count-offset))
    if count > 0 then
        if kind == "zset" then
            local raw = C.call(ctx,"ZRANGE",key,P.format_decimal(offset),P.format_decimal(offset+count-1),"WITHSCORES")
            local err
            ordered, err = collect_zset(fact,raw,count,member_bytes)
            if not ordered then return nil, err end
        else
            ordered = C.call(ctx,"LRANGE",key,P.format_decimal(offset),P.format_decimal(offset+count-1))
            if I.dense(ordered,500) ~= count then return nil, "INVALID_STATE" end
            for i = 1, count do if not bounded_text(ordered[i],member_bytes) then return nil, "INVALID_STATE" end end
            if offset == 0 and count == fact.count then fact.complete, fact.items = true, copy(ordered) end
        end
    end
    local result = copy(fact); result.ordered, result.offset = ordered, offset
    return result
end
-- Exclusive byte cursor on a score-zero job_order index. Checks each selected
-- score, not a claim to have validated every unselected member's index semantics.
function R.lex_page(ctx, key, after, limit, maximum, member_bytes)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    if not I.integer(member_bytes,5373952) or not bounded_text(after,member_bytes) or not I.integer(limit,500) or limit == 0 then return nil, "INVALID_ARGUMENT" end
    local pre, code = R.cardinality(ctx,key,"zset",maximum)
    if not pre then return nil, code end
    local fact, ordered = caches[ctx].facts[key], {}
    if fact.exists then
        local start = after == "" and "-" or "(" .. after
        ordered = C.call(ctx,"ZRANGEBYLEX",key,start,"+","LIMIT","0",P.format_decimal(limit))
        local count = I.dense(ordered,limit)
        if not count or count > fact.count then return nil, "INVALID_STATE" end
        local previous = after
        for i = 1, count do
            if not bounded_text(ordered[i],member_bytes) or ordered[i] <= previous then return nil, "INVALID_STATE" end
            previous = ordered[i]
        end
        local selected, err = R.members(ctx,key,"zset",ordered,maximum,member_bytes)
        if not selected then return nil, err end
        for i = 1, count do if selected.members[ordered[i]] ~= true or selected.scores[ordered[i]] ~= 0 then return nil, "INVALID_STATE" end end
    end
    local result = copy(fact); result.ordered = ordered
    return result
end
function R.ttl(ctx, key)
    local kind, code = R.key_type(ctx,key)
    if not kind then return nil, code end
    local state, ttl = caches[ctx], nil
    if kind == "none" then return copy(state.facts[key]) end
    local fact = state.facts[key]
    if fact then ttl = fact.ttl_ms end
    if ttl == nil then ttl = C.call(ctx,"PTTL",key) end
    if ttl ~= -1 and not I.integer(ttl,P.limits.max_integer) then return nil, "INVALID_STATE" end
    if not fact then fact = {key=key,kind=kind,exists=true,complete=false,v={},members={},scores={},score_text={}} end
    fact.ttl_ms, state.facts[key] = ttl, fact
    if ttl==-1 then fact.expires_at_ms=false
    else
        local absolute=P.safe_add(ctx.now_ms,ttl)
        if not absolute then return nil,"INVALID_NUMBER" end
        fact.expires_at_ms=absolute
    end
    return copy(fact)
end
-- Bounded score-prefix selection, never a scan of all retained jobs. Select
-- up to 101 for a 100-job maintenance batch when the handler needs a more bit.
function R.due(ctx,key,through,limit,maximum,member_bytes)
    if not C.preparing(ctx) then return nil,"INVALID_STATE" end
    if P.parse_decimal(through)==nil or not I.integer(limit,500) or limit==0 or not I.integer(member_bytes,5373952) then return nil,"INVALID_ARGUMENT" end
    local pre,code=R.cardinality(ctx,key,"zset",maximum);if not pre then return nil,code end
    local fact,ordered=caches[ctx].facts[key],{}
    if fact.exists then
        local raw=C.call(ctx,"ZRANGEBYSCORE",key,"-inf",through,"WITHSCORES","LIMIT","0",P.format_decimal(limit))
        local count=I.dense(raw,2*limit)
        if not count or count%2~=0 or count/2>fact.count then return nil,"INVALID_STATE" end
        local err;ordered,err=collect_zset(fact,raw,count/2,member_bytes);if not ordered then return nil,err end
        for _,member in ipairs(ordered) do if fact.scores[member]>P.parse_decimal(through) then return nil,"INVALID_STATE" end end
    end
    local result=copy(fact);result.ordered=ordered;return result
end
-- Private receipts include complete and partial facts. TYPE alone remains
-- insufficient. Plan must prove coverage per field/member, never assume nil.
function R.snapshot(ctx, key)
    local state, code = cache(ctx)
    if not state then return nil, code end
    if not state.facts[key] then return nil, "INVALID_STATE" end
    return copy(state.facts[key])
end
function R.project(ctx, needs)
    if not C.preparing(ctx) then return nil, "INVALID_STATE" end
    local count, code = I.dense(needs,1024)
    if not count then return nil, code end
    local view = {}
    for i = 1, count do
        local need = needs[i]
        if type(need) ~= "table" or type(need.name) ~= "string" or view[need.name] then return nil, "INVALID_ARGUMENT" end
        local fact, err
        if need.kind == "hash" then fact, err = R.fixed_hash(ctx,need.key,need.schema)
        elseif need.kind == "string" then fact, err = R.string(ctx,need.key,need.maximum,need.digest)
        elseif need.kind == "absent" then fact, err = R.absent(ctx,need.key,need.expected_type)
        elseif need.kind == "cardinality" then fact, err = R.cardinality(ctx,need.key,need.collection,need.maximum)
        elseif need.kind == "members" then fact, err = R.members(ctx,need.key,need.collection,need.members,need.maximum,need.member_bytes)
        elseif need.kind == "all_members" then fact, err = R.all_members(ctx,need.key,need.collection,need.maximum,need.member_bytes)
        elseif need.kind == "dynamic_hash" then fact, err = R.dynamic_hash(ctx,need.key,need.maximum,need.field_bytes,need.value_bytes)
        elseif need.kind == "ttl" then fact, err = R.ttl(ctx,need.key)
        else return nil, "INVALID_ARGUMENT" end
        if not fact then return nil, err end
        view[need.name] = fact
    end
    for name, fact in next, view, nil do ctx.selected[name] = copy(fact) end
    return view
end
function R.slots(ctx)
    local fact, code = R.dynamic_hash(ctx,slot_key,4,64,126)
    if not fact then
        if code == "LIMIT_EXCEEDED" then code = "INVALID_STATE" end
        return nil, code
    end
    local sum, records = 0, {}
    for i = 1, #fact.names do
        local field, value = fact.names[i], fact.v[fact.names[i]]
        if not I.hex(field,64) then return nil, "INVALID_STATE" end
        local remaining, run, job, fence, aborted = string.match(value,"^([^:]+):([^:]+):([^:]+):([^:]+):([^:]+)$")
        local r, f, a = P.parse_decimal(remaining), P.parse_decimal(fence), P.parse_decimal(aborted)
        if not r or r > 50331648 or not f or f == 0 or not a or a > 73 or not I.hex(run,32) or not I.hex(job,64) then return nil, "INVALID_STATE" end
        local next_sum = P.safe_add(sum,r)
        if not next_sum then return nil, "INVALID_STATE" end
        sum = next_sum
        records[field] = {remaining_bytes=r,run_id=run,job_id=job,fence=f,abort_unlinked_keys=a}
    end
    return {sum=sum,count=fact.count,records=records}
end
return R
