local registered,code=CJ.Maintenance.register("CJ2_CLEAN_STAGE")
if not registered then return CJ.Context.reject(code) end
local ctx; ctx,code=CJ.Maintenance.open("CJ2_CLEAN_STAGE",KEYS,ARGV)
if not ctx then return CJ.Context.reject(code) end
local execution; execution,code=CJ.Maintenance.clean(ctx)
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
