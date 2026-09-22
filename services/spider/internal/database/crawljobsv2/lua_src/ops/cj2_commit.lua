-- All publication preflight, admission, reply and ACL work finishes before this
-- tail. Unexpected executor failure propagates; Redis rollback is NOT claimed.
local execution, code = CJ.Stage.prepare("CJ2_COMMIT", KEYS, ARGV)
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
