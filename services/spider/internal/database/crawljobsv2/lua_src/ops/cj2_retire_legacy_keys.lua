local execution, code = CJ.Admin.prepare("CJ2_RETIRE_LEGACY_KEYS",KEYS,ARGV)
if not execution then return CJ.Context.reject(code) end
-- Sealed descriptors/reply only. A later Redis error propagates; no rollback.
for i = 1, execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
