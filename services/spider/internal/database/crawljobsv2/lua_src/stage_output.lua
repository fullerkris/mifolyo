-- Pure output codecs for ledger_stage.lua. Load as CJ.StageOutput after URL and
-- before Stage/Context.open. No Redis, clocks, JSON library, or protocol changes.
-- Go authority_records.go/chunk.go/source_output.go are the value oracle.
-- IMPORTANT: last_crawled is NOT RFC3339: the normative Go layout is
-- "Mon, 02 Jan 2006 15:04:05 UTC". Images have no filename/association-ID field;
-- their identity is ImageDataKey(publication, canonical page, canonical source).
-- Do not decode URL percent escapes, normalize filenames, or substitute origins
-- for URL identity. CJ.URL implements the pinned Go/Unicode-15 fixed points.
local O, P, I, S, URL = {}, CJ.P, CJ.Identities, CJ.Schemas, CJ.URL
local floor, sub, byte, char, concat = math.floor, string.sub, string.byte, string.char, table.concat
local function text(s, bound) return type(s) == "string" and #s <= bound and P.validate_text(s) == true end
local function canonical(s) return text(s,2048) and URL.check_canonical(s,1) end
local function control(s)
    -- unicode.IsControl: C0, DEL and C1, not all format/space characters.
    return string.find(s,"[%z\1-\31\127]") ~= nil or string.find(s,"\194[\128-\159]") ~= nil
end
local spaces = {" ","\t","\n","\v","\f","\r","\194\133","\194\160","\225\154\128",
    "\226\128\128","\226\128\129","\226\128\130","\226\128\131","\226\128\132","\226\128\133",
    "\226\128\134","\226\128\135","\226\128\136","\226\128\137","\226\128\138",
    "\226\128\168","\226\128\169","\226\128\175","\226\129\159","\227\128\128"}
local function trim(s, left_only)
    local a,b = 1,#s
    while a <= b do
        local found = false
        for _,w in ipairs(spaces) do if sub(s,a,a+#w-1) == w then a=a+#w; found=true; break end end
        if not found then break end
    end
    if not left_only then
        while b >= a do
            local found = false
            for _,w in ipairs(spaces) do if sub(s,b-#w+1,b) == w then b=b-#w; found=true; break end end
            if not found then break end
        end
    end
    return sub(s,a,b)
end
local tspecial = '()<>@,;:\\"/[]?='
local function token(s)
    local i=1
    while i<=#s do
        local b=byte(s,i)
        if b<=32 or b>=127 or string.find(tspecial,sub(s,i,i),1,true) then break end
        i=i+1
    end
    return sub(s,1,i-1),sub(s,i)
end
local function percent(s)
    local out,i={},1
    while i<=#s do
        if sub(s,i,i)=="%" then
            local h=sub(s,i+1,i+2)
            if #h~=2 or string.find(h,"[^0-9a-fA-F]") then return nil end
            out[#out+1]=char(tonumber(h,16)); i=i+3
        else out[#out+1]=sub(s,i,i); i=i+1 end
    end
    return concat(out)
end
local function encoded_parameter(s)
    local charset,_,value=string.match(s,"^([^']*)'([^']*)'(.*)$")
    if not charset then return nil end
    charset=string.lower(charset)
    if charset~="utf-8" and charset~="us-ascii" then return nil end
    return percent(value)
end
-- Exact accepted subset of mime.ParseMediaType, INCLUDING its equal duplicate
-- parameters, Unicode whitespace, trailing semicolon and RFC2231 continuations.
-- A regexp accepting only the common spelling would disagree with Go.
function O.content_type(s)
    if not text(s,1024) or s=="" or trim(s)~=s then return nil,"INVALID_ARGUMENT" end
    local pos=string.find(s,";",1,true)
    local base=pos and sub(s,1,pos-1) or s
    if string.lower(trim(base))~="text/html" then return nil,"INVALID_ARGUMENT" end
    local rest=pos and sub(s,pos) or ""
    local params,continuation={},{}
    while #rest>0 do
        rest=trim(rest,true)
        if rest=="" or trim(rest)==";" then break end
        if sub(rest,1,1)~=";" then return nil,"INVALID_ARGUMENT" end
        local key,tail=token(trim(sub(rest,2),true))
        key=string.lower(key); tail=trim(tail,true)
        if key=="" or sub(tail,1,1)~="=" then return nil,"INVALID_ARGUMENT" end
        tail=trim(sub(tail,2),true)
        local value
        if sub(tail,1,1)=='"' then
            local out,i,closed={},2,false
            while i<=#tail do
                local c=sub(tail,i,i)
                if c=='"' then value,rest,closed=concat(out),sub(tail,i+1),true; break end
                if c=="\\" and i<#tail and string.find(tspecial,sub(tail,i+1,i+1),1,true) then
                    i=i+1; c=sub(tail,i,i)
                elseif c=="\r" or c=="\n" then return nil,"INVALID_ARGUMENT" end
                out[#out+1]=c; i=i+1
            end
            if not closed then return nil,"INVALID_ARGUMENT" end
        else
            value,rest=token(tail)
            if value=="" then return nil,"INVALID_ARGUMENT" end
        end
        local map=params
        local star=string.find(key,"*",1,true)
        if star then
            local name=sub(key,1,star-1)
            if not continuation[name] then continuation[name]={} end
            map=continuation[name]
        end
        if map[key]~=nil and map[key]~=value then return nil,"INVALID_ARGUMENT" end
        map[key]=value
    end
    for name,map in next,continuation,nil do
        if map[name.."*"]~=nil then
            local value=encoded_parameter(map[name.."*"])
            if value~=nil then params[name]=value end
        else
            local parts,valid={},false
            for n=0,1024 do -- each part consumes input bytes, bounded above by 1024
                local key=name.."*"..P.format_decimal(n)
                if map[key]~=nil then parts[#parts+1]=map[key]; valid=true
                elseif map[key.."*"]~=nil then
                    local value
                    if n==0 then value=encoded_parameter(map[key.."*"]) else value=percent(map[key.."*"]) end
                    parts[#parts+1]=value or ""; valid=true
                else break end
            end
            if valid then params[name]=concat(parts) end
        end
    end
    for k,v in next,params,nil do if k~="charset" or string.lower(v)~="utf-8" then return nil,"INVALID_ARGUMENT" end end
    return true
end

local months={"Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sep","Oct","Nov","Dec"}
local weekdays={"Sun","Mon","Tue","Wed","Thu","Fri","Sat"}
local function pad(n,width)
    local s=P.format_decimal(n)
    return string.rep("0",math.max(0,width-#s))..s
end
-- Gregorian civil-date conversion, integer days from Unix epoch. All operands
-- stay below MAX_EXACT even for the maximum Redis millisecond input.
local function civil(days)
    local z=days+719468
    local era=floor(z/146097)
    local doe=z-era*146097
    local yoe=floor((doe-floor(doe/1460)+floor(doe/36524)-floor(doe/146096))/365)
    local y=yoe+era*400
    local doy=doe-(365*yoe+floor(yoe/4)-floor(yoe/100))
    local mp=floor((5*doy+2)/153)
    local d=doy-floor((153*mp+2)/5)+1
    local m=mp+(mp<10 and 3 or -9)
    if m<=2 then y=y+1 end
    return y,m,d
end
local function days_from(y,m,d)
    if m<=2 then y=y-1 end
    local era=floor(y/400); local yo=y-era*400
    local mp=m+(m>2 and -3 or 9)
    return era*146097+yo*365+floor(yo/4)-floor(yo/100)+floor((153*mp+2)/5)+d-1-719468
end
local function date_text(days,seconds)
    local y,m,d=civil(days)
    return weekdays[(days+4)%7+1]..", "..pad(d,2).." "..months[m].." "..pad(y,4).." "..
        pad(floor(seconds/3600),2)..":"..pad(floor(seconds/60)%60,2)..":"..pad(seconds%60,2).." UTC"
end
function O.last_crawled(milliseconds)
    local ms=P.parse_decimal(milliseconds)
    if ms==nil then return nil,"INVALID_NUMBER" end
    local seconds=floor(ms/1000)
    return date_text(floor(seconds/86400),seconds%86400)
end
local function valid_date(s)
    if not text(s,29) or #s~=29 then return false end
    local w,d,m,y,h,mi,se=string.match(s,"^(%a%a%a), (%d%d) (%a%a%a) (%d%d%d%d) (%d%d):(%d%d):(%d%d) UTC$")
    if not w then return false end
    local month
    for i=1,12 do if months[i]==m then month=i end end
    d,y,h,mi,se=tonumber(d),tonumber(y),tonumber(h),tonumber(mi),tonumber(se)
    if not month or d<1 or d>31 or h>23 or mi>59 or se>59 then return false end
    return date_text(days_from(y,month,d),h*3600+mi*60+se)==s
end
local alphabet="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
local function b64(s)
    local out={}
    for i=1,#s,3 do
        local a,b,c=byte(s,i,i+2); local n=a*65536+(b or 0)*256+(c or 0)
        for j=1,(c and 4 or b and 3 or 2) do
            local k=floor(n/2^(24-6*j))%64+1; out[#out+1]=sub(alphabet,k,k)
        end
    end
    return concat(out)
end
local function unb64(s)
    if #s==0 or #s>2731 or #s%4==1 or string.find(s,"[^A-Za-z0-9_-]") then return nil end
    local out,n,bits={},0,0
    for i=1,#s do
        local p=string.find(alphabet,sub(s,i,i),1,true)
        n=n*64+p-1; bits=bits+6
        if bits>=8 then bits=bits-8; out[#out+1]=char(floor(n/2^bits)); n=n%2^bits end
    end
    local raw=concat(out)
    if n~=0 or b64(raw)~=s then return nil end
    return raw
end
function O.keys(publication,page,source)
    if not I.digest(publication) or not canonical(page) or (source~=nil and not canonical(source)) then
        return nil,"INVALID_IDENTIFIER"
    end
    local suffix=publication..":"..b64(page)
    local result={page="page_data:"..suffix,outlinks="outlinks:"..suffix,manifest="page_images:"..suffix}
    if source~=nil then result.image="image_data:"..suffix..":"..b64(source) end
    return result
end
local page_names={"normalized_url","html","original_html","content_type","status_code","last_crawled","rendered",
    "render_policy_rule","render_policy_sha256","publication_id"}
local page_bounds={2048,5242880,5242880,1024,3,29,5,128,64,64}
local nonblob={"normalized_url","content_type","status_code","last_crawled","rendered","render_policy_rule","render_policy_sha256","publication_id"}
local image_names={"contract_version","publication_id","normalized_page_url","normalized_source_url","alt"}
local manifest_names={"contract_version","publication_id","normalized_url","image_count","image_keys"}
local function page_fields(v)
    if not canonical(v.normalized_url) or not O.content_type(v.content_type) or
       not string.match(v.status_code,"^[123][0-9][0-9]$") or not valid_date(v.last_crawled) or
       not I.digest(v.publication_id) then return nil,"INVALID_ARGUMENT" end
    if v.rendered=="false" then
        if v.render_policy_rule~="" or v.render_policy_sha256~="" then return nil,"INVALID_ARGUMENT" end
    elseif v.rendered=="true" then
        if not text(v.render_policy_rule,128) or v.render_policy_rule=="" or control(v.render_policy_rule) or
           not I.digest(v.render_policy_sha256) then return nil,"INVALID_ARGUMENT" end
    else return nil,"INVALID_ARGUMENT" end
    return true
end
local function final_page(v)
    local ok,code=page_fields(v); if not ok then return nil,code end
    if #v.html+#v.original_html>10485760 or (v.rendered=="false" and v.original_html~="") or
       (v.rendered=="true" and v.original_html=="") then return nil,"INVALID_ARGUMENT" end
    return true
end
local function image(v)
    if v.contract_version~="1" or not I.digest(v.publication_id) or not canonical(v.normalized_page_url) or
       not canonical(v.normalized_source_url) or not text(v.alt,1024) then return nil,"INVALID_ARGUMENT" end
    return true
end
function O.manifest(v)
    if type(v)~="table" or v.contract_version~="1" or not I.digest(v.publication_id) or not canonical(v.normalized_url) or
       not text(v.image_keys,393216) then return nil,"INVALID_ARGUMENT" end
    local count=P.parse_decimal(v.image_count)
    if not count or count>64 then return nil,"LIMIT_EXCEEDED" end
    local keys,sources={},{}
    local s=v.image_keys
    if count==0 then if s~="[]" then return nil,"INVALID_ARGUMENT" end; return {keys=keys,sources=sources} end
    if sub(s,1,2)~='["' or sub(s,-2)~='"]' then return nil,"INVALID_ARGUMENT" end
    local prefix="image_data:"..v.publication_id..":"..b64(v.normalized_url)..":"
    local pos=2
    for i=1,count do
        if sub(s,pos,pos)~='"' then return nil,"INVALID_ARGUMENT" end
        local finish=string.find(s,'"',pos+1,true)
        if not finish then return nil,"INVALID_ARGUMENT" end
        local key=sub(s,pos+1,finish-1)
        if sub(key,1,#prefix)~=prefix then return nil,"INVALID_ARGUMENT" end
        local source=unb64(sub(key,#prefix+1))
        if not source or not canonical(source) or (i>1 and source<=sources[i-1]) then return nil,"INVALID_ARGUMENT" end
        keys[i],sources[i]=key,source
        pos=finish+1
        if i<count then if sub(s,pos,pos)~="," then return nil,"INVALID_ARGUMENT" end; pos=pos+1 end
    end
    if pos~=#s or sub(s,pos)~="]" then return nil,"INVALID_ARGUMENT" end
    return {keys=keys,sources=sources}
end
local defs={
    final_page={names=page_names,bounds=page_bounds,validate=final_page},
    final_image={names=image_names,bounds={1,64,2048,2048,1024},validate=image},
    image_manifest={names=manifest_names,bounds={1,64,2048,2,393216},validate=function(v) if O.manifest(v) then return true end; return nil,"INVALID_ARGUMENT" end},
    page_fields={names=nonblob,bounds={2048,1024,3,29,5,128,64,64},validate=page_fields}
}
for _,name in ipairs({"final_page","final_image","image_manifest"}) do
    local d=defs[name]; local ok,code=S.register(name,{names=d.names,bounds=d.bounds},d.validate)
    if not ok then return nil,code end
end
-- Binary RECORD or Wire.record projection; never trust redundant .v or .n.
function O.record(kind,input)
    local d=defs[kind]
    if not d then return nil,"INVALID_ARGUMENT" end
    if kind~="page_fields" then
        local encoded=input
        if type(input)=="table" then encoded=P.record(input.fields,16777216) end
        if type(encoded)~="string" then return nil,"INVALID_ARGUMENT" end
        return S.decode(kind,encoded)
    end
    local encoded=input
    if type(input)=="table" then encoded=P.record(input.fields,16384) end
    if type(encoded)~="string" then return nil,"INVALID_ARGUMENT" end
    local fields=P.decode_record(encoded,d.names,16384)
    if not fields then return nil,"INVALID_ARGUMENT" end
    local v={}
    for i,name in ipairs(d.names) do
        if not text(fields[i][2],d.bounds[i]) then return nil,"INVALID_ARGUMENT" end
        v[name]=fields[i][2]
    end
    local ok,code=page_fields(v); if not ok then return nil,code end
    return {fields=fields,v=v}
end
return O
