local registered, registration_error = CJ.Request.register_worker("CJ2_CANCEL_RESERVATION")
if not registered then return CJ.Context.reject(registration_error) end
local function prepare()
    local spec, code = CJ.Wire.worker_spec("CJ2_CANCEL_RESERVATION")
    if not spec then return nil, code end
    local ctx, err = CJ.Context.open(spec,KEYS,ARGV)
    if not ctx then return nil, err end
    return CJ.Request.prepare_terminal(ctx)
end
local execution, code = prepare()
if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply
