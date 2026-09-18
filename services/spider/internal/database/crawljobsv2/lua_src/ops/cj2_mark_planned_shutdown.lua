local execution, code = CJ.Admin.prepare("CJ2_MARK_PLANNED_SHUTDOWN",KEYS,ARGV)
if not execution then return CJ.Context.reject(code) end
-- Sealed descriptors/reply only. This records intent; it never stops Redis.
for i = 1, execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
