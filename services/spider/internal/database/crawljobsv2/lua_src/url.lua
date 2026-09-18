-- Dormant, pure support chunk. Build-time lexical factory: (function(P,D)
-- <these exact bytes> end)(P,D). P=primitives, D=pinned unicode_data. No loaders.
-- Algorithms adapted from Go Authors' x/net/idna and x/text/unicode/norm;
-- BSD-3-Clause and Unicode notices are in licenses/. See unicode-provenance.json.
-- Portions Copyright 2011 The Go Authors. All rights reserved.
-- Portions Copyright 2016 The Go Authors. All rights reserved.
-- This validates FIXED POINTS of Go CanonicalizeURLV1, never arbitrary input.
-- API: check_canonical(s, 1) -> components,nil or nil,closed_code.
--      derive_origin(s) -> DNS origin,nil or nil,closed_code (IPs forbidden).
-- P and D must be trusted build-time dependencies, NEVER caller/ARGV objects.
-- Closed codes: INVALID_ARGUMENT = wrong argument type/version or incompatible
-- factory interface/identity; INVALID_IDENTIFIER = ANY invalid/noncanonical URL, including size,
-- UTF-8, IDNA, or IP origin. These are support codes, NOT operation responses:
-- callers map them according to input versus persisted-state contract context.
-- version is the NUMBER 1 (not the wire string "1"); no implicit/default version.
-- port is the effective NUMBER; host excludes IPv6 brackets. Identity permits
-- local names, non-default ports and IPs; origin only adds the IP restriction.
local byte, char, sub, concat, floor = string.byte, string.char, string.sub, table.concat, math.floor
local expected_data = "cj2-url-v1/go1.25.13/x-net-v0.58.0/x-text-v0.41.0/unicode-15.0.0/data-1"
local configured = type(P) == "table" and type(D) == "table" and D.identity == expected_data and
    type(P.validate_text) == "function" and type(P.sha256) == "function" and
    type(P.parse_decimal) == "function" and type(P.format_decimal) == "function" and
    type(D.get) == "function" and type(D.compose) == "function" and
    type(D.props) == "string" and type(D.decomp) == "string"
local get, compose, props, decomp, validate_text, sha256, parse_decimal, format_decimal
if configured then
    get, compose, props, decomp = D.get, D.compose, D.props, D.decomp
    validate_text, sha256 = P.validate_text, P.sha256
    parse_decimal, format_decimal = P.parse_decimal, P.format_decimal
end
local function uint(s, pos, n)
    local v = 0
    for i = pos, pos + n - 1 do v = v * 256 + byte(s, i) end
    return v
end
local function bit_set(n, b) return floor(n / b) % 2 == 1 end
local function alnum(b) return b and (b >= 48 and b <= 57 or b >= 97 and b <= 122) end
local function hex(b)
    if b and b >= 48 and b <= 57 then return b - 48 end
    if b and b >= 65 and b <= 70 then return b - 55 end
    return nil
end
local function utf8_scalar(cp)
    if cp < 128 then return char(cp) end
    if cp < 2048 then return char(192 + floor(cp/64), 128 + cp%64) end
    if cp < 65536 then return char(224 + floor(cp/4096), 128 + floor(cp/64)%64, 128 + cp%64) end
    return char(240 + floor(cp/262144), 128 + floor(cp/4096)%64, 128 + floor(cp/64)%64, 128 + cp%64)
end

-- RFC 3492, with precisely x/net's signed-int32 arithmetic ceiling. A label's
-- 59-byte payload bounds all loops and the decoded array to at most 59 scalars.
local function adapt(delta, points, first)
    if first then delta = floor(delta/700) else delta = floor(delta/2) end
    delta = delta + floor(delta/points)
    local k = 0
    while delta > 455 do delta = floor(delta/35); k = k + 36 end
    return k + floor(36 * delta / (delta + 38))
end
local function threshold(k, bias)
    if k <= bias then return 1 end
    if k >= bias + 26 then return 26 end
    return k - bias
end
local function digit(b)
    if b and b >= 97 and b <= 122 then return b - 97 end
    if b and b >= 48 and b <= 57 then return b - 22 end
    return nil
end
local function digit_byte(d)
    if d < 26 then return char(d + 97) end
    return char(d + 22)
end
local function puny_decode(s)
    local delimiter = 0
    for j = 1, #s do if byte(s,j) == 45 then delimiter = j end end
    if #s == 0 or delimiter == 1 or delimiter == #s then return nil end
    local out = {}
    for j = 1, delimiter - 1 do out[#out+1] = byte(s,j) end
    local pos, i, n, bias = delimiter + 1, 0, 128, 72
    while pos <= #s do
        local old_i, w, k = i, 1, 36
        while true do
            local d = digit(byte(s,pos))
            if not d or d > floor((2147483647 - i) / w) then return nil end
            pos, i = pos + 1, i + d * w
            local t = threshold(k, bias)
            if d < t then break end
            if w > floor(2147483647 / (36-t)) then return nil end
            w, k = w * (36-t), k + 36
        end
        local count = #out + 1
        if count > 59 then return nil end
        bias = adapt(i-old_i, count, old_i == 0)
        n, i = n + floor(i/count), i%count
        if n > 1114111 or n >= 55296 and n <= 57343 then return nil end
        for j = #out, i+1, -1 do out[j+1] = out[j] end
        out[i+1], i = n, i+1
    end
    return out
end
local function puny_encode(input)
    local out, b = {}, 0
    for j = 1, #input do
        if input[j] < 128 then out[#out+1] = char(input[j]); b = b + 1 end
    end
    local h, delta, n, bias = b, 0, 128, 72
    if b > 0 then out[#out+1] = "-" end
    while h < #input do
        local m = 2147483647
        for j = 1, #input do local r = input[j]; if r >= n and r < m then m = r end end
        if m-n > floor((2147483647-delta)/(h+1)) then return nil end
        delta, n = delta + (m-n)*(h+1), m
        for j = 1, #input do
            local r = input[j]
            if r < n then
                if delta == 2147483647 then return nil end
                delta = delta + 1
            elseif r == n then
                local q, k = delta, 36
                while true do
                    local t = threshold(k,bias)
                    if q < t then break end
                    out[#out+1] = digit_byte(t + (q-t)%(36-t))
                    q, k = floor((q-t)/(36-t)), k + 36
                end
                out[#out+1] = digit_byte(q)
                bias, delta, h = adapt(delta,h+1,h == b), 0, h+1
            end
        end
        delta, n = delta + 1, n + 1
    end
    return concat(out)
end

-- Properties: {scalar, compressed-leading-CCC, NFC flags, nLead, decomp offset,
-- decomp length}. Compressed CCCs preserve order. Assignment after composition
-- deliberately clears properties, exactly as x/text's reorderBuffer does.
local function norm_info(cp)
    local p = get(cp)
    return {cp, byte(props,p+2), byte(props,p+3), byte(props,p+4), uint(props,p+5,3), byte(props,p+8)}
end
local function quick_span(input, start)
    local i, last_start, last_cc, ss = start, start, 0, 0
    while i <= #input do
        if input[i][1] < 128 then
            repeat i = i + 1 until i > #input or input[i][1] >= 128
            last_start, last_cc, ss = i-1, 0, 0
        else
            local p = input[i]
            ss = ss + p[4]
            if ss > 30 then return last_start, false end
            if p[4] == 0 then
                ss, last_start = p[3]%4, i
            elseif last_cc > p[2] then return last_start, false end
            if bit_set(p[3],16) then return last_start, false end
            last_cc, i = p[2], i+1
        end
    end
    return i, true
end
local function compose_buffer(b)
    local k, starter, hangul = 2, 1, false
    for i = 2, #b do
        local p = b[i]
        if p[1] >= 4352 and p[1] <= 4607 then hangul = true end
        local combined = 0
        if hangul or bit_set(p[3],8) then
            local prev_cc, cc = b[k-1][2], p[2]
            if prev_cc == 0 then starter = k-1 end
            if not (starter ~= k-1 and prev_cc >= cc) then
                local a, c = b[starter][1], p[1]
                if hangul then
                    if a >= 4352 and a < 4371 and c >= 4449 and c < 4470 then
                        combined = 44032 + (a-4352)*588 + (c-4449)*28
                    elseif a >= 44032 and a < 55204 and (a-44032)%28 == 0 and c > 4519 and c < 4547 then
                        combined = a + c - 4519
                    end
                else combined = compose(a,c) end
            end
        end
        if combined ~= 0 then b[starter] = {combined,0,0}
        else b[k], k = p, k+1 end
    end
    for i = #b, k, -1 do b[i] = nil end
end
local function nfc_normal(runes)
    local input = {}
    for i = 1, #runes do input[i] = norm_info(runes[i]) end
    local bp, all = quick_span(input,1)
    if all then return true end
    local ss, buffer, compare_pos = 0, {}, bp
    local function flush()
        compose_buffer(buffer)
        for i = 1, #buffer do
            if not input[compare_pos] or buffer[i][1] ~= input[compare_pos][1] then return false end
            compare_pos = compare_pos + 1
        end
        buffer = {}
        return true
    end
    local function ordered(p)
        if #buffer >= 32 then return false end
        local j = #buffer + 1
        if p[2] > 0 then
            while j > 1 and buffer[j-1][2] > p[2] do buffer[j] = buffer[j-1]; j = j-1 end
        end
        buffer[j] = p
        return true
    end
    local function insert(p)
        local cp = p[1]
        if cp >= 44032 and cp < 55204 then
            local r = cp-44032
            buffer[#buffer+1] = {4352+floor(r/588),0,0}
            buffer[#buffer+1] = {4449+floor(r/28)%21,0,0}
            if r%28 ~= 0 then buffer[#buffer+1] = {4519+r%28,0,0} end
            return #buffer <= 32
        end
        if not bit_set(p[3],4) then return ordered(p) end
        for j = 0, p[6]-1 do
            local q = norm_info(uint(decomp,p[5]+j*3+1,3))
            if q[2] == 0 and not bit_set(q[3],8) and #buffer > 0 and not flush() then return false end
            if not ordered(q) then return false end
        end
        return true
    end
    local function stream(p)
        ss = ss + p[4]
        if ss > 30 then ss = 0; return 2 end
        if p[4] == 0 then ss = p[3]%4; return 1 end
        return 0
    end
    while bp <= #input do
        compare_pos = bp
        local p, sp = input[bp], bp
        local state = stream(p)
        if state == 2 then
            if not ordered({847,0,0}) then return false end
        else
            if not insert(p) then return false end
            sp = sp + 1
            while sp <= #input do
                p = input[sp]
                state = stream(p)
                if state == 1 then break end
                if state == 2 then
                    if not ordered({847,0,0}) then return false end
                    break
                end
                if not insert(p) then return false end
                sp = sp + 1
            end
        end
        if not flush() then return false end
        bp = quick_span(input,sp)
    end
    return true
end

-- x/net/idna joinStates including ZERO DEFAULTS. Columns: U,L,D,T,R,ZWJ,ZWNJ,V.
-- States are translated from Go's 0-based enum to Lua's 1-based indices.
local join_states = {
    {1,3,3,1,1,6,6,2}, {1,3,3,1,1,1,1,1},
    {1,3,3,3,1,6,5,4}, {1,3,3,3,1,1,1,1},
    {1,6,3,5,1,6,6,5}, {6,6,6,6,6,6,6,6}
}
-- Bidi class numeric values are pinned by x/text/unicode/bidi/trieval.go:
-- L=0,R=1,EN=2,ES=3,ET=4,AN=5,CS=6,B=7,S=8,WS=9,ON=10,BN=11,NSM=12,AL=13.
local function valid_alabel(label)
    local runes = puny_decode(sub(label,5))
    if not runes or #runes == 0 then return false end
    local text, nonascii, rtl, joiners = {}, false, false, false
    local flags, classes = {}, {}
    for i = 1, #runes do
        local cp = runes[i]
        local p = get(cp)
        local f, bc = byte(props,p), byte(props,p+1)
        if f%2 == 0 then return false end
        flags[i], classes[i], text[i] = f, bc, utf8_scalar(cp)
        nonascii = nonascii or cp >= 128
        rtl = rtl or bc == 1 or bc == 5 or bc == 13
        joiners = joiners or cp == 8204 or cp == 8205
    end
    if not nonascii or not nfc_normal(runes) then return false end
    local s = concat(text)
    if #s > 4 and byte(s,3) == 45 and byte(s,4) == 45 or byte(s,1) == 45 or byte(s,#s) == 45 then return false end
    if bit_set(flags[1],2) then return false end
    if joiners then
        local st = 1
        for i = 1, #runes do
            local jt = floor(flags[i]/8)
            if runes[i] == 8205 then jt = 5 elseif runes[i] == 8204 then jt = 6 end
            st = join_states[st][jt+1]
            if bit_set(flags[i],4) then st = join_states[st][8] end
        end
        if st == 6 or st == 5 then return false end
    end
    if rtl then
        -- A label containing RTL classes can only finish the RFC5893 DFA on
        -- the RTL branch. Do not apply the LTR branch to non-RTL labels.
        if classes[1] ~= 1 and classes[1] ~= 13 then return false end
        local en, an, final = false, false, false
        for i = 1, #classes do
            local c = classes[i]
            if c == 2 then en = true elseif c == 5 then an = true end
            if c == 1 or c == 13 or c == 2 or c == 5 then final = true
            elseif c == 3 or c == 4 or c == 6 or c == 10 or c == 11 then final = false
            elseif c ~= 12 then return false end
        end
        if not final or en and an then return false end
    end
    return puny_encode(runes) == sub(label,5)
end

local function ipv4(s)
    local count, start = 0, 1
    for i = 1, #s+1 do
        if i == #s+1 or byte(s,i) == 46 then
            local length, n = i-start, 0
            if length < 1 or length > 3 or length > 1 and byte(s,start) == 48 then return false end
            for j = start, i-1 do local b = byte(s,j); if b < 48 or b > 57 then return false end; n = n*10+b-48 end
            if n > 255 then return false end
            count, start = count+1, i+1
        end
    end
    return count == 4
end
local function ipv6(s)
    local i, count, ellipsis = 1, 0, false
    if sub(s,1,2) == "::" then i, ellipsis = 3, true end
    while i <= #s do
        local start, dotted = i, false
        while i <= #s and byte(s,i) ~= 58 do dotted = dotted or byte(s,i) == 46; i = i+1 end
        local part = sub(s,start,i-1)
        if #part == 0 then return false end
        if dotted then
            if i <= #s or not ipv4(part) then return false end
            count = count + 2
        else
            if #part > 4 then return false end
            for j = 1, #part do
                local b = byte(part,j)
                if not (b >= 48 and b <= 57 or b >= 97 and b <= 102) then return false end
            end
            count = count + 1
        end
        if count > 8 then return false end
        if i <= #s then
            i = i+1
            if i > #s then return false end
            if byte(s,i) == 58 then
                if ellipsis then return false end
                ellipsis, i = true, i+1
            end
        end
    end
    return ellipsis and count < 8 or not ellipsis and count == 8
end
local function dns_or_ipv4(host)
    if #host == 0 or #host > 253 then return false end
    local count, numeric, start = 0, true, 1
    for i = 1, #host+1 do
        if i == #host+1 or byte(host,i) == 46 then
            local label = sub(host,start,i-1)
            if #label < 1 or #label > 63 or not alnum(byte(label,1)) or not alnum(byte(label,#label)) then return false end
            for j = 1, #label do
                local b = byte(label,j)
                if not alnum(b) and b ~= 45 then return false end
                numeric = numeric and b >= 48 and b <= 57
            end
            if sub(label,1,4) == "xn--" and not valid_alabel(label) then return false end
            count, start = count+1, i+1
        end
    end
    if count == 4 and numeric then return ipv4(host), true end
    return true, false
end
local function component(s, start)
    local i = start
    local safe = "/:@!$&'()*+,;=-._~"
    while i <= #s do
        local b = byte(s,i)
        if b == 63 then i = i+1
        elseif b == 37 then
            local h, l = hex(byte(s,i+1)), hex(byte(s,i+2))
            if not h or not l then return false end
            local n = h*16+l
            if n < 32 or n == 127 then return false end
            if n == 194 and byte(s,i+3) == 37 then
                local a, z = hex(byte(s,i+4)), hex(byte(s,i+5))
                if a and z and a*16+z >= 128 and a*16+z <= 159 then return false end
            end
            i = i+3
        else
            local ok = alnum(b) or b >= 65 and b <= 90
            if not ok then for j = 1, #safe do if b == byte(safe,j) then ok = true; break end end end
            if not ok then return false end
            i = i+1
        end
    end
    return true -- The first '?' splits path/query; subsequent '?' are query-safe.
end
local function check_canonical(s, version)
    if not configured or type(s) ~= "string" or version ~= 1 then return nil, "INVALID_ARGUMENT" end
    if #s > 2048 or #s == 0 then return nil, "INVALID_IDENTIFIER" end
    local valid = validate_text(s)
    if not valid then return nil, "INVALID_IDENTIFIER" end
    -- Canonical output is ASCII. Unicode is represented by validated A-labels
    -- or escaped component bytes, which are intentionally NOT UTF-8-decoded.
    for j = 1, #s do local b = byte(s,j); if b < 33 or b > 126 or b == 92 or b == 35 then return nil, "INVALID_IDENTIFIER" end end
    local scheme, start
    if sub(s,1,7) == "http://" then scheme, start = "http", 8
    elseif sub(s,1,8) == "https://" then scheme, start = "https", 9
    else return nil, "INVALID_IDENTIFIER" end
    local stop = start
    while stop <= #s and byte(s,stop) ~= 47 and byte(s,stop) ~= 63 do stop = stop+1 end
    if stop == start or byte(s,stop) ~= 47 or not component(s,stop) then return nil, "INVALID_IDENTIFIER" end
    local authority, host, port_text, is_ip = sub(s,start,stop-1), nil, nil, false
    if byte(authority,1) == 91 then
        local close = 2
        while close <= #authority and byte(authority,close) ~= 93 do close = close+1 end
        if close > #authority then return nil, "INVALID_IDENTIFIER" end
        host = sub(authority,2,close-1)
        if not ipv6(host) then return nil, "INVALID_IDENTIFIER" end
        is_ip = true
        if close < #authority then
            if byte(authority,close+1) ~= 58 then return nil, "INVALID_IDENTIFIER" end
            port_text = sub(authority,close+2)
        end
    else
        local colon
        for j = 1, #authority do if byte(authority,j) == 58 then if colon then return nil, "INVALID_IDENTIFIER" end; colon = j end end
        host = authority
        if colon then host, port_text = sub(authority,1,colon-1), sub(authority,colon+1) end
        local ok
        ok, is_ip = dns_or_ipv4(host)
        if not ok then return nil, "INVALID_IDENTIFIER" end
    end
    local port = 80
    if scheme == "https" then port = 443 end
    if port_text ~= nil then
        local n = parse_decimal(port_text)
        if not n or n == 0 or n > 65535 or n == port then return nil, "INVALID_IDENTIFIER" end
        port = n
    end
    local id = sha256("mifolyo-url:v1\000" .. s)
    if not id then return nil, "INVALID_ARGUMENT" end
    local result = {canonical_url=s, url_id=id, scheme=scheme, host=host,
                    port=port, explicit_port=port_text ~= nil, is_ip=is_ip}
    if not is_ip then result.origin = scheme .. "://" .. host .. ":" .. format_decimal(port) end
    return result
end
local function derive_origin(s)
    local result, err = check_canonical(s,1)
    if not result then return nil, err end
    if result.is_ip then return nil, "INVALID_IDENTIFIER" end
    return result.origin
end
return {check_canonical=check_canonical, derive_origin=derive_origin}
