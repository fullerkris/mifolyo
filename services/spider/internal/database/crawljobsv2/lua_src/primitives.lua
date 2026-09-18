-- Dormant CrawlJobsV2 core: tests / future build-time inlining only.
-- Not a canonical operation, runtime module loader, or executable Redis bundle.
-- Every API returns value, nil on success or nil, stable_error on rejection.
-- These are implementation safety ceilings, NOT replacement protocol limits or
-- evidence of the Redis p99 gate. Callers must enforce their smaller codec limits.
local MAX_INTEGER = 9007199254740991
local MAX_BYTES = 16 * 1024 * 1024
local MAX_FIELDS = 128
local MAX_RECORDS = 10000
local floor, byte, char = math.floor, string.byte, string.char
local sub, concat = string.sub, table.concat
local bitop = bit -- Redis's global LuaBitOp: signed 32-bit results, not bit32.

local function integer(n)
    return type(n) == "number" and n >= 0 and n <= MAX_INTEGER and n == floor(n)
end

local function parse_decimal(s)
    if type(s) ~= "string" or #s == 0 or #s > 16 or
       (#s > 1 and byte(s, 1) == 48) or
       (#s == 16 and s > "9007199254740991") then
        return nil, "INVALID_NUMBER"
    end
    local n = 0
    for i = 1, #s do
        local d = byte(s, i) - 48
        if d < 0 or d > 9 then return nil, "INVALID_NUMBER" end
        -- The lexical bound above prevents rounding an out-of-range decimal.
        n = n * 10 + d
    end
    return n
end

local function format_decimal(n)
    if not integer(n) then return nil, "INVALID_NUMBER" end
    if n == 0 then return "0" end
    local s = ""
    repeat
        local q = floor(n / 10)
        -- Subtract first: adding ASCII '0' to n can cross 2^53 and round.
        s = char(48 + (n - q * 10)) .. s
        n = q
    until n == 0
    return s -- At most 16 digits; never Lua's exponent/rounded tostring form.
end

local function safe_add(a, b)
    if not integer(a) or not integer(b) or a > MAX_INTEGER - b then
        return nil, "INVALID_NUMBER"
    end
    return a + b
end

-- Lua's # and ipairs silently accept holes / ignore hash keys. Inspect raw keys
-- with a bounded walk before allocating output. Metatables are never evaluated.
local function array_count(a, maximum)
    if type(a) ~= "table" or getmetatable(a) ~= nil then
        return nil, "INVALID_ARRAY"
    end
    local n = 0
    for k in next, a do
        if not integer(k) or k < 1 then return nil, "INVALID_ARRAY" end
        if k > maximum or n == maximum then return nil, "LIMIT_EXCEEDED" end
        n = n + 1
    end
    for i = 1, n do
        if rawget(a, i) == nil then return nil, "INVALID_ARRAY" end
    end
    return n
end

local function time_ms(reply)
    local n = array_count(reply, 2)
    if n ~= 2 then return nil, "INVALID_TIME" end
    local seconds = parse_decimal(reply[1])
    local micros = parse_decimal(reply[2])
    if not seconds or not micros or micros > 999999 or
       seconds > 9007199254740 then return nil, "INVALID_TIME" end
    local result = safe_add(seconds * 1000, floor(micros / 1000))
    if not result then return nil, "INVALID_TIME" end
    return result -- Decodes supplied TIME bytes; never obtains time itself.
end

local function bound(maximum)
    if maximum == nil then return MAX_BYTES end
    if not integer(maximum) or maximum > MAX_BYTES then
        return nil, "INVALID_ARGUMENT"
    end
    return maximum
end

local function bounded_string(s, maximum)
    if type(s) ~= "string" then return nil, "INVALID_ARGUMENT" end
    local limit, err = bound(maximum)
    if not limit then return nil, err end
    if #s > limit then return nil, "LIMIT_EXCEEDED" end
    return limit
end

-- UTF-8 scalar validation, without Lua 5.3's utf8 library. General record text
-- allows controls (e.g. HTML / alt); group/rule codecs may reject Unicode Cc.
-- This does NOT validate or normalize URLs, IDNA, NFC, or per-field semantics.
local function validate_text(s, reject_controls)
    local ok, err = bounded_string(s)
    if not ok then return nil, err end
    if reject_controls ~= nil and type(reject_controls) ~= "boolean" then
        return nil, "INVALID_ARGUMENT"
    end
    local i = 1
    while i <= #s do
        local a = byte(s, i)
        local width, cp, minimum
        if a < 128 then
            width, cp, minimum = 1, a, 0
        elseif a >= 194 and a <= 223 then
            width, cp, minimum = 2, a - 192, 128
        elseif a >= 224 and a <= 239 then
            width, cp, minimum = 3, a - 224, 2048
        elseif a >= 240 and a <= 244 then
            width, cp, minimum = 4, a - 240, 65536
        else
            return nil, "INVALID_UTF8"
        end
        if #s - i + 1 < width then return nil, "INVALID_UTF8" end
        for j = 1, width - 1 do
            local b = byte(s, i + j)
            if b < 128 or b > 191 then return nil, "INVALID_UTF8" end
            cp = cp * 64 + b - 128
        end
        if cp < minimum or cp > 1114111 or (cp >= 55296 and cp <= 57343) then
            return nil, "INVALID_UTF8"
        end
        if reject_controls and (cp < 32 or (cp >= 127 and cp <= 159)) then
            return nil, "CONTROL_CHARACTER"
        end
        i = i + width
    end
    return true
end

local function printable_name(s)
    if type(s) ~= "string" or #s == 0 or #s > MAX_BYTES then return false end
    for i = 1, #s do
        local b = byte(s, i)
        if b < 32 or b > 126 then return false end
    end
    return true
end

local function u64(n)
    if not integer(n) then return nil, "INVALID_NUMBER" end
    local bytes = {}
    for i = 8, 1, -1 do
        local q = floor(n / 256)
        bytes[i] = char(n - q * 256)
        n = q
    end
    return concat(bytes)
end

local function read_u64(s, pos)
    if #s - pos + 1 < 8 then return nil, "INVALID_ENCODING" end
    local n = 0
    for i = pos, pos + 7 do
        local b = byte(s, i)
        -- Check BEFORE multiplying, including for hostile full-width U64s.
        if n > floor((MAX_INTEGER - b) / 256) then
            return nil, "INVALID_NUMBER"
        end
        n = n * 256 + b
    end
    return n, pos + 8
end

local function decode_u64(s)
    if type(s) ~= "string" then return nil, "INVALID_ARGUMENT" end
    -- Fixed width has just one encoding; a ninth leading zero is NOT accepted.
    if #s ~= 8 then return nil, "INVALID_ENCODING" end
    local n, pos = read_u64(s, 1)
    if not n then return nil, pos end
    return n
end

local function frame(s, maximum)
    local limit, err = bounded_string(s, maximum)
    if not limit then return nil, err end
    if #s > limit - 8 then return nil, "LIMIT_EXCEEDED" end
    return u64(#s) .. s
end

local function read_frame(s, pos)
    local n, next_pos = read_u64(s, pos)
    if not n then return nil, next_pos end
    -- Prefix bytes need not be UTF-8. Never slice or allocate from an unchecked
    -- declared length; subtraction also avoids offset/length overflow.
    if n > #s - next_pos + 1 then return nil, "INVALID_ENCODING" end
    return sub(s, next_pos, next_pos + n - 1), next_pos + n
end

local function decode_frame(s, maximum)
    local limit, err = bounded_string(s, maximum)
    if not limit then return nil, err end
    local value, pos = read_frame(s, 1)
    if not value then return nil, pos end
    if pos ~= #s + 1 then return nil, "INVALID_ENCODING" end
    return value
end

-- Records are dense arrays of dense {name, binary_value} pairs, not maps.
local function record_size(fields, maximum)
    local n, err = array_count(fields, MAX_FIELDS)
    if not n then return nil, err end
    local size, seen = 8, {}
    if size > maximum then return nil, "LIMIT_EXCEEDED" end
    for i = 1, n do
        local pair_count, pair_err = array_count(fields[i], 2)
        if not pair_count then return nil, pair_err end
        if pair_count ~= 2 then return nil, "INVALID_ARRAY" end
        local name, value = fields[i][1], fields[i][2]
        if not printable_name(name) then return nil, "INVALID_NAME" end
        if seen[name] then return nil, "DUPLICATE_NAME" end
        if type(value) ~= "string" then return nil, "INVALID_ARGUMENT" end
        if #name > maximum - size - 16 or
           #value > maximum - size - 16 - #name then
            return nil, "LIMIT_EXCEEDED"
        end
        size = size + 16 + #name + #value
        seen[name] = true
    end
    return size, n
end

local function encode_record(fields, n)
    local parts = {u64(n)}
    for i = 1, n do
        local name, value = fields[i][1], fields[i][2]
        parts[#parts + 1] = u64(#name)
        parts[#parts + 1] = name
        parts[#parts + 1] = u64(#value)
        parts[#parts + 1] = value
    end
    return concat(parts)
end

local function record(fields, maximum)
    local limit, err = bound(maximum)
    if not limit then return nil, err end
    local size, n = record_size(fields, limit)
    if not size then return nil, n end
    return encode_record(fields, n)
end

local function section(label, records, maximum)
    local limit, err = bound(maximum)
    if not limit then return nil, err end
    if not printable_name(label) then return nil, "INVALID_NAME" end
    local n, array_err = array_count(records, MAX_RECORDS)
    if not n then return nil, array_err end
    local size = 16 + #label
    if size > limit then return nil, "LIMIT_EXCEEDED" end
    -- Preflight the ENTIRE collection before allocating encoded records/output.
    for i = 1, n do
        local record_bytes, record_err = record_size(records[i], limit - size - 8)
        if not record_bytes then return nil, record_err end
        size = size + 8 + record_bytes
    end
    local parts = {u64(#label), label, u64(n)}
    for i = 1, n do
        local encoded = encode_record(records[i], #records[i])
        parts[#parts + 1] = u64(#encoded)
        parts[#parts + 1] = encoded
    end
    return concat(parts)
end

-- Exact expected names/order are mandatory. UTF-8 values are required by
-- default; explicit false selects the Go bare binary codec, not a text codec.
-- Empty transport-marker sentinels are a caller concern, not zero-field records.
local function decode_record(s, names, maximum, text_values)
    local limit, err = bounded_string(s, maximum)
    if not limit then return nil, err end
    if text_values ~= nil and type(text_values) ~= "boolean" then
        return nil, "INVALID_ARGUMENT"
    end
    local n, array_err = array_count(names, MAX_FIELDS)
    if not n then return nil, array_err end
    local count, pos = read_u64(s, 1)
    if not count then return nil, pos end
    if count > MAX_FIELDS then return nil, "LIMIT_EXCEEDED" end
    if count ~= n then return nil, "INVALID_ENCODING" end
    local seen = {}
    for i = 1, n do
        if not printable_name(names[i]) then return nil, "INVALID_NAME" end
        if seen[names[i]] then return nil, "DUPLICATE_NAME" end
        seen[names[i]] = true
    end
    local fields = {}
    for i = 1, n do
        local name, next_pos = read_frame(s, pos)
        if not name then return nil, next_pos end
        if name ~= names[i] then return nil, "INVALID_ENCODING" end
        local value, value_pos = read_frame(s, next_pos)
        if not value then return nil, value_pos end
        if text_values ~= false then
            local valid, text_err = validate_text(value)
            if not valid then return nil, text_err end
        end
        fields[i] = {name, value}
        pos = value_pos
    end
    if pos ~= #s + 1 then return nil, "INVALID_ENCODING" end
    return fields
end

local SHA_K = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
}

local function sha256(s)
    local ok, err = bounded_string(s)
    if not ok then return nil, err end
    if type(bitop) ~= "table" or type(bitop.tobit) ~= "function" or
       type(bitop.band) ~= "function" or type(bitop.bxor) ~= "function" or
       type(bitop.bnot) ~= "function" or type(bitop.rshift) ~= "function" or
       type(bitop.ror) ~= "function" then return nil, "BIT_UNAVAILABLE" end
    local tobit, band, bxor = bitop.tobit, bitop.band, bitop.bxor
    local bnot, rshift, ror = bitop.bnot, bitop.rshift, bitop.ror
    local h = {0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
               0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19}
    local w = {} -- One fixed 64-word schedule, reused for every block.
    local function block(input, pos)
        for i = 1, 16 do
            local a, b, c, d = byte(input, pos, pos + 3)
            w[i] = tobit(((a * 256 + b) * 256 + c) * 256 + d)
            pos = pos + 4
        end
        for i = 17, 64 do
            local x, y = w[i - 15], w[i - 2]
            local s0 = bxor(ror(x, 7), ror(x, 18), rshift(x, 3))
            local s1 = bxor(ror(y, 17), ror(y, 19), rshift(y, 10))
            w[i] = tobit(w[i - 16] + s0 + w[i - 7] + s1)
        end
        local a, b, c, d, e, f, g, hh = h[1], h[2], h[3], h[4], h[5], h[6], h[7], h[8]
        for i = 1, 64 do
            local s1 = bxor(ror(e, 6), ror(e, 11), ror(e, 25))
            local ch = bxor(band(e, f), band(bnot(e), g))
            -- All sums stay far below 2^53. tobit reduces modulo 2^32 and
            -- returns SIGNED words; never treat rshift as arithmetic shift.
            local t1 = tobit(hh + s1 + ch + SHA_K[i] + w[i])
            local s0 = bxor(ror(a, 2), ror(a, 13), ror(a, 22))
            local maj = bxor(band(a, b), band(a, c), band(b, c))
            local t2 = tobit(s0 + maj)
            hh, g, f, e, d, c, b, a = g, f, e, tobit(d + t1), c, b, a, tobit(t1 + t2)
        end
        h[1], h[2], h[3], h[4] = tobit(h[1] + a), tobit(h[2] + b), tobit(h[3] + c), tobit(h[4] + d)
        h[5], h[6], h[7], h[8] = tobit(h[5] + e), tobit(h[6] + f), tobit(h[7] + g), tobit(h[8] + hh)
    end
    local full = #s - #s % 64
    for pos = 1, full, 64 do block(s, pos) end
    -- Only the last 1-2 blocks are copied, never a padded copy of the full input.
    local tail = sub(s, full + 1) .. char(128) .. string.rep(char(0), (55 - #s) % 64) .. u64(#s * 8)
    for pos = 1, #tail, 64 do block(tail, pos) end
    local out = {}
    for i = 1, 8 do
        local v = h[i]
        -- Format unsigned BYTES: %x on a negative BitOp word is host-width dependent.
        out[i] = string.format("%02x%02x%02x%02x", band(rshift(v, 24), 255),
            band(rshift(v, 16), 255), band(rshift(v, 8), 255), band(v, 255))
    end
    return concat(out)
end

return {
    limits = {max_integer = MAX_INTEGER, max_bytes = MAX_BYTES,
              max_fields = MAX_FIELDS, max_records = MAX_RECORDS},
    parse_decimal = parse_decimal, format_decimal = format_decimal,
    safe_add = safe_add, time_ms = time_ms, validate_text = validate_text,
    u64 = u64, decode_u64 = decode_u64, frame = frame, decode_frame = decode_frame,
    record = record, section = section, decode_record = decode_record, sha256 = sha256
}
