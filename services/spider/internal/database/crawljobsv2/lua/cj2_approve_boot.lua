-- Proposed CJ2_APPROVE_BOOT source; dormant, not an operationally approved bundle.
-- Sections 2.2/2.3/10.2 isolate this operation to TIME, INFO SERVER, and the
-- durability hash. In particular, do not read markers or stage_slots for the
-- ordinary memory-admission inequality. No evidence artifact bytes are supplied
-- here: validate their digests and immutable propagation, not invented proofs.

-- Prevalidation/build. All failures here precede the sole mutation below.
local durability_key = "mifolyo:crawl:v2:durability"
local max_integer_text = "9007199254740991"
local max_integer = 9007199254740991
local max_request_bytes = 2097152
local max_evidence_age_ms = 2592000000

local function reject(code)
    return redis.error_reply("ERR CRAWL_V2_" .. code)
end

local function uint(value)
    if type(value) ~= "string" or #value == 0 or #value > 16 then
        return nil
    end
    if value ~= "0" and not string.match(value, "^[1-9][0-9]*$") then
        return nil
    end
    if #value == 16 and value > max_integer_text then
        return nil
    end
    return tonumber(value)
end

local function positive_uint(value)
    local number = uint(value)
    return number ~= nil and number > 0
end

local function hex(value, length)
    return type(value) == "string" and #value == length
        and string.match(value, "^[0-9a-f]+$") ~= nil
end

local function evidence(value)
    return hex(value, 64) and string.find(value, "[1-9a-f]") ~= nil
end

local function approval_mode(value)
    return value == "initial" or value == "planned" or value == "unclean_rehearsal"
end

-- TIME is called even for rejected input, and never called a second time.
local clock = redis.call("TIME")
if type(clock) ~= "table" or #clock ~= 2 then
    return reject("INVALID_NUMBER")
end
local seconds = uint(clock[1])
local microseconds = uint(clock[2])
if seconds == nil or microseconds == nil or seconds > 9007199254740
    or microseconds > 999999 then
    return reject("INVALID_NUMBER")
end
local base_ms = seconds * 1000
local fraction_ms = math.floor(microseconds / 1000)
if fraction_ms > max_integer - base_ms then
    return reject("INVALID_NUMBER")
end
local now_ms = base_ms + fraction_ms
if now_ms == 0 then
    return reject("INVALID_NUMBER")
end
local now_text = string.format("%.0f", now_ms)

if #KEYS ~= 1 or #ARGV ~= 8 then
    return reject("INVALID_ARGUMENT")
end

-- Exact RESP EVALSHA framing: *12, EVALSHA, a 40-byte SHA-1, key count "1",
-- one key, and eight arguments. The first four framed parts total 72 bytes.
local function bulk_size(value)
    return #value + 5 + #string.format("%.0f", #value)
end
if type(KEYS[1]) ~= "string" or #KEYS[1] > max_request_bytes then
    return reject("COMMAND_BOUNDS_EXCEEDED")
end
local request_bytes = 72 + bulk_size(KEYS[1])
for i = 1, 8 do
    if type(ARGV[i]) ~= "string" then
        return reject("INVALID_ARGUMENT")
    end
    if #ARGV[i] > max_request_bytes then
        return reject("COMMAND_BOUNDS_EXCEEDED")
    end
    request_bytes = request_bytes + bulk_size(ARGV[i])
end
if request_bytes > max_request_bytes then
    return reject("COMMAND_BOUNDS_EXCEEDED")
end
if KEYS[1] ~= durability_key then
    return reject("INVALID_ARGUMENT")
end
if not hex(ARGV[1], 40) or not hex(ARGV[2], 32) or not hex(ARGV[3], 64) then
    return reject("INVALID_IDENTIFIER")
end
if not evidence(ARGV[3]) then
    return reject("INVALID_ARGUMENT")
end
if not positive_uint(ARGV[4]) then
    return reject("INVALID_NUMBER")
end
if ARGV[5] ~= "0" or not approval_mode(ARGV[8]) then
    return reject("INVALID_ARGUMENT")
end
if ARGV[8] == "planned" then
    if not hex(ARGV[6], 32) or not hex(ARGV[7], 64) then
        return reject("INVALID_IDENTIFIER")
    end
    if not evidence(ARGV[7]) then
        return reject("INVALID_ARGUMENT")
    end
elseif ARGV[6] ~= "" or ARGV[7] ~= "" then
    return reject("INVALID_ARGUMENT")
end
local evidence_at_ms = uint(ARGV[4])
if evidence_at_ms > now_ms or now_ms - evidence_at_ms > max_evidence_age_ms then
    return reject("BOOT_UNAPPROVED")
end

local server = redis.call("INFO", "SERVER")
if type(server) ~= "string" then
    return reject("BOOT_UNAPPROVED")
end
local actual_run_id = nil
for line in string.gmatch(server .. "\n", "([^\n]*)\n") do
    if string.sub(line, 1, 7) == "run_id:" then
        if actual_run_id ~= nil then
            return reject("BOOT_UNAPPROVED")
        end
        actual_run_id = string.gsub(string.sub(line, 8), "\r$", "")
    end
end
if not hex(actual_run_id, 40) or actual_run_id ~= ARGV[1] then
    return reject("BOOT_UNAPPROVED")
end

local fields = {
    "schema_version", "boot_state", "approved_redis_run_id", "boot_epoch",
    "approved_at_ms", "planned_shutdown_nonce", "planned_shutdown_evidence_sha256",
    "last_approval_mode", "consumed_planned_shutdown_nonce",
    "rehearsal_evidence_sha256", "rehearsal_at_ms", "acknowledged_loss_bound"
}
local field_max_bytes = {1, 10, 40, 32, 16, 32, 64, 17, 32, 64, 16, 1}
local key_type = redis.call("TYPE", durability_key)
if type(key_type) ~= "table" or type(key_type.ok) ~= "string" then
    return reject("INVALID_STATE")
end
if key_type.ok ~= "none" and key_type.ok ~= "hash" then
    return reject("WRONG_TYPE")
end
local stored = nil
if key_type.ok == "hash" then
    if redis.call("HLEN", durability_key) ~= 12 then
        return reject("INVALID_STATE")
    end
    -- No HGETALL: unknown field names/values can be arbitrarily large. Bound
    -- every known value before the one fixed-width bulk read. A missing empty
    -- field is distinguishable from an empty bulk string in the HMGET result.
    for i = 1, 12 do
        local length = redis.call("HSTRLEN", durability_key, fields[i])
        if type(length) ~= "number" or length < 0 or length > field_max_bytes[i]
            or length ~= math.floor(length) then
            return reject("INVALID_STATE")
        end
    end
    stored = redis.call("HMGET", durability_key, unpack(fields, 1, 12))
    if type(stored) ~= "table" or #stored ~= 12 then
        return reject("INVALID_STATE")
    end
    for i = 1, 12 do
        if type(stored[i]) ~= "string" or #stored[i] > field_max_bytes[i] then
            return reject("INVALID_STATE")
        end
    end
    if stored[1] ~= "1" or not hex(stored[3], 40) or not hex(stored[4], 32)
        or not positive_uint(stored[5]) or not approval_mode(stored[8])
        or not evidence(stored[10]) or not positive_uint(stored[11]) or stored[12] ~= "0"
        or (stored[6] ~= "" and not hex(stored[6], 32))
        or (stored[7] ~= "" and not evidence(stored[7]))
        or (stored[9] ~= "" and not hex(stored[9], 32)) then
        return reject("INVALID_STATE")
    end
    if stored[2] == "planned" then
        if stored[6] == "" or stored[7] == "" or stored[9] ~= "" then
            return reject("INVALID_STATE")
        end
    elseif stored[2] == "approved" then
        if stored[8] == "planned" then
            if stored[6] ~= "" or stored[7] == "" or stored[9] == "" then
                return reject("INVALID_STATE")
            end
        elseif stored[6] ~= "" or stored[7] ~= "" or stored[9] ~= "" then
            return reject("INVALID_STATE")
        end
    elseif stored[2] == "unapproved" then
        if stored[6] ~= "" or stored[7] ~= "" or stored[9] ~= "" then
            return reject("INVALID_STATE")
        end
    else
        return reject("INVALID_STATE")
    end
end

-- Full proposed post-state. Non-planned requests already proved both optional
-- inputs empty; planned requests preserve the evidence and consume the nonce.
local values = {
    "1", "approved", ARGV[1], ARGV[2], now_text, "", ARGV[7], ARGV[8],
    ARGV[6], ARGV[3], ARGV[4], "0"
}
if stored ~= nil and stored[2] == "approved" and stored[3] == actual_run_id then
    -- approved_at_ms is validated stored Redis time, not the replay's TIME.
    -- Every other field must be the exact post-state of this request.
    for i = 1, 12 do
        if i ~= 5 and stored[i] ~= values[i] then
            return reject("IMMUTABLE_MISMATCH")
        end
    end
    return {"EXISTS_IDENTICAL", now_text, ARGV[2]}
end
if ARGV[8] == "initial" then
    if stored ~= nil then
        return reject("BOOT_UNAPPROVED")
    end
else
    if stored == nil or stored[3] == actual_run_id then
        return reject("BOOT_UNAPPROVED")
    end
    if stored[4] == ARGV[2] then
        return reject("IMMUTABLE_MISMATCH")
    end
    if ARGV[8] == "planned" then
        if stored[2] ~= "planned" then
            return reject("BOOT_UNAPPROVED")
        end
        if stored[6] ~= ARGV[6] or stored[7] ~= ARGV[7] then
            return reject("IMMUTABLE_MISMATCH")
        end
    end
end

local write = {"HSET", durability_key}
for i = 1, 12 do
    write[2 * i + 1] = fields[i]
    write[2 * i + 2] = values[i]
end
local response = {"OK", now_text, ARGV[2]}
if #write ~= 26 then
    return reject("INVALID_STATE")
end
if redis.acl_check_cmd(unpack(write, 1, 26)) ~= true then
    return reject("BOOT_UNAPPROVED")
end

-- Mutation: fixed, fully built call; unexpected write errors propagate. Redis
-- does not roll back an applied write, and no error is reclassified as replay.
redis.call(unpack(write, 1, 26))
return response
