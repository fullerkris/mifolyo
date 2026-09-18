-- Canonical source ordering and contiguous final-shaped image payloads.
local execution, code = CJ.Stage.prepare("CJ2_STAGE_IMAGES_BATCH", KEYS, ARGV)
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
