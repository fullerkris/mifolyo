-- Pure helpers. P is the lexically inlined primitives module; no Redis access.
local I = {}
function I.integer(n, maximum)
    return type(maximum) == "number" and maximum >= 0 and maximum <= P.limits.max_integer and
        maximum == math.floor(maximum) and type(n) == "number" and n >= 0 and n <= maximum and n == math.floor(n)
end
function I.dense(a, maximum)
    if not I.integer(maximum,P.limits.max_integer) or type(a) ~= "table" or getmetatable(a) ~= nil then return nil, "INVALID_ARGUMENT" end
    local count = 0
    for k in next, a, nil do
        if not I.integer(k, maximum) or k == 0 then return nil, "INVALID_ARGUMENT" end
        count = count + 1
        if count > maximum then return nil, "LIMIT_EXCEEDED" end
    end
    for i = 1, count do
        if rawget(a, i) == nil then return nil, "INVALID_ARGUMENT" end
    end
    return count
end
function I.hex(s, width)
    return type(s) == "string" and #s == width and string.match(s, "^[0-9a-f]+$") ~= nil
end
function I.digest(s)
    return I.hex(s, 64) and string.find(s, "[1-9a-f]") ~= nil
end
function I.image(s)
    return type(s) == "string" and string.sub(s, 1, 7) == "sha256:" and I.digest(string.sub(s, 8))
end
function I.positive(s)
    local n = P.parse_decimal(s)
    return n ~= nil and n > 0
end
function I.group(s)
    return type(s) == "string" and #s > 0 and #s <= 128 and P.validate_text(s, true) == true
end
-- Canonical section 3 scheduling score, not Redis's native double spelling.
function I.score(s)
    if type(s) ~= "string" or #s == 0 or #s > 13 then return nil, "INVALID_NUMBER" end
    if s == "0" then return 0 end
    local text = s
    if string.sub(text,1,1) == "-" then text = string.sub(text,2) end
    local whole, fraction = string.match(text,"^([0-9]+)%.([0-9]+)$")
    if not whole then whole = string.match(text,"^([0-9]+)$") end
    if not whole or (#whole > 1 and string.sub(whole,1,1) == "0") or
       (fraction and (#fraction > 6 or string.sub(fraction,-1) == "0")) or
       (whole == "0" and not fraction) then return nil, "INVALID_NUMBER" end
    local value = tonumber(s)
    if not value or value < -1000 or value > 10000 then return nil, "INVALID_NUMBER" end
    return value
end
-- Redis may return e.g. 0.10000000000000001 or exponent notation. These are
-- numeric observations; operation validators bind them to canonical score_text.
function I.redis_score(s)
    if type(s) ~= "string" or #s == 0 or #s > 64 then return nil, "INVALID_NUMBER" end
    if not string.match(s,"^[%+%-]?[0-9]+%.?[0-9]*$") and
       not string.match(s,"^[%+%-]?[0-9]+%.?[0-9]*[eE][%+%-]?[0-9]+$") then return nil, "INVALID_NUMBER" end
    -- Gopher-Lua 1.1.1's public tonumber selects ParseFloat only when a dot
    -- occurs, unlike native Lua's strtod. Normalize ONLY a lexically validated
    -- scientific integer mantissa. This preserves native Lua's binary64 value
    -- (including signed zero/subnormals) and does not alter retained raw text.
    local mantissa, exponent = string.match(s,"^([%+%-]?[0-9]+)([eE][%+%-]?[0-9]+)$")
    local parse_text = s
    if mantissa then parse_text = mantissa .. ".0" .. exponent end
    local value = tonumber(parse_text)
    -- Native Lua's math.huge is infinity; gopher-lua 1.1.1 uses MaxFloat64.
    -- Self-subtraction is zero for every finite binary64, including MaxFloat64,
    -- and NaN for infinities. Do not reject a valid finite boundary value.
    if not value or value ~= value or value - value ~= 0 then return nil, "INVALID_NUMBER" end
    -- The integer branch of gopher-lua's tonumber also drops the sign of -0.
    -- Preserve the sign of a validated negative zero without changing its text
    -- or doing arithmetic scaling of any nonzero mantissa.
    if value == 0 and string.sub(s,1,1) == "-" then
        -- Use the decimal parser, not a folded -0 literal (which gopher-lua's
        -- constant table may coalesce with +0).
        value = tonumber("-0.0")
        if value == nil then return nil, "INVALID_NUMBER" end
    end
    return value
end
-- ZADD also carries exact unsigned timestamps/counters outside score's range.
function I.zadd_score(s)
    local value = P.parse_decimal(s)
    if value ~= nil then return value end
    return I.score(s)
end
function I.framed(domain, values)
    local count, code = I.dense(values, 128)
    if not count then return nil, code end
    if type(domain) ~= "string" or #domain > 128 then return nil, "INVALID_ARGUMENT" end
    local prefix = P.frame(domain)
    if not prefix then return nil, "INVALID_ARGUMENT" end
    local parts, size = {prefix}, #prefix
    for i = 1, count do
        if type(values[i]) ~= "string" or #values[i] > 16384 then return nil, "INVALID_ARGUMENT" end
        local framed = P.frame(values[i])
        if not framed then return nil, "INVALID_ARGUMENT" end
        size = size + #framed
        if size > 2097152 then return nil, "LIMIT_EXCEEDED" end
        parts[i + 1] = framed
    end
    local digest = P.sha256(table.concat(parts))
    if not digest then return nil, "INVALID_STATE" end
    return digest
end
function I.global_scope()
    return I.framed("mifolyo:rate:global:v2", {})
end
function I.group_scope(rate_scope_id)
    if not I.hex(rate_scope_id, 32) then return nil, "INVALID_IDENTIFIER" end
    return I.framed("mifolyo:rate:group:v2", {rate_scope_id})
end
-- INFO is a Redis-owned reply, not a client-controlled authority artifact.
-- Reject duplicate selected fields; never use substring/unanchored matches.
function I.info(text, names)
    local count = I.dense(names, 16)
    if not count or type(text) ~= "string" or #text > 65536 then return nil, "INVALID_STATE" end
    local selected, result = {}, {}
    for i = 1, count do selected[names[i]] = true end
    for line in string.gmatch(text .. "\n", "([^\n]*)\n") do
        local name, value = string.match(line, "^([^:]+):(.*)$")
        if name and selected[name] then
            if result[name] ~= nil then return nil, "INVALID_STATE" end
            result[name] = string.gsub(value, "\r$", "")
        end
    end
    for i = 1, count do
        if result[names[i]] == nil then return nil, "INVALID_STATE" end
    end
    return result
end
return I
