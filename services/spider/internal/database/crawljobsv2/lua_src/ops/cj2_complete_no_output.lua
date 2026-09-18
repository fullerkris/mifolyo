local registered, registration_error = CJ.Job.register_outcome("CJ2_COMPLETE_NO_OUTPUT")
if not registered then return CJ.Context.reject(registration_error) end
local function prepare()
    local spec, code = CJ.Wire.worker_spec("CJ2_COMPLETE_NO_OUTPUT")
    if not spec then return nil, code end
    local ctx, err = CJ.Context.open(spec,KEYS,ARGV)
    if not ctx then return nil, err end
    return CJ.Job.prepare_outcome(ctx)
end
local execution, code = prepare()
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
