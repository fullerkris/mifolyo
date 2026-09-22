-- One bounded UTF-8 blob; no Lua bulk SHA or mutation-result branch.
local execution, code = CJ.Stage.prepare("CJ2_STAGE_PAGE_BLOB", KEYS, ARGV)
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
