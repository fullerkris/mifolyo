package crawljobsv2

import (
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// Command semantics only: no Go copy of an INSTALL/gate/ledger transition.
// Existing BOOT helpers provide binary-safe RESP conversion and hash snapshots.
type sharedLuaRedis struct {
	data             map[string]bootLuaEntry
	sets             map[string]map[string]bool
	zsets            map[string]map[string]float64
	lists            map[string][]string
	trace            []bootLuaCommand
	now              uint64
	runID            string
	used, maximum    uint64
	lazyfree         uint64
	writes, attempts int
	aclCount, denyAt int
	failAt           int
	failAfter        bool
	override         func(*lua.LState, string, []string) lua.LValue
	prebuilt         *lua.LTable
	returnedPrebuilt bool
}

const sharedLuaSlots = "mifolyo:crawl:v2:stage_slots"

func sharedLuaNewRedis() *sharedLuaRedis {
	canaries := bootLuaNewRedis()
	for _, key := range []string{ContractsActiveKey, ContractsCandidateKey, CrawlContractKey, sharedLuaSlots} {
		delete(canaries.data, key)
	}
	canaries.data[bootLuaKey] = bootLuaEntry{kind: "hash", hash: bootLuaPostHash(bootLuaArgs(ApprovalInitial), bootLuaNow-1000), expireAt: -1}
	r := &sharedLuaRedis{data: canaries.data, sets: map[string]map[string]bool{}, zsets: map[string]map[string]float64{}, lists: map[string][]string{}, now: bootLuaNow, runID: strings.Repeat("a", 40), used: 1000000, maximum: 400 * 1024 * 1024}
	r.setSet("unrelated:set", []string{"canary", "untouched"})
	r.setZSet("unrelated:zset", map[string]float64{"canary": 0.1})
	r.setList("unrelated:list", []string{"canary", "canary"})
	return r
}

type sharedLuaSnapshot struct {
	bootLuaSnapshot
	sets  map[string]map[string]bool
	zsets map[string]map[string]float64
	lists map[string][]string
}

func (r *sharedLuaRedis) snapshot() sharedLuaSnapshot {
	s := sharedLuaSnapshot{bootLuaSnapshot: (&bootLuaRedis{data: r.data, writes: r.writes}).snapshot(), sets: map[string]map[string]bool{}, zsets: map[string]map[string]float64{}, lists: map[string][]string{}}
	for key, values := range r.sets {
		s.sets[key] = map[string]bool{}
		for v, p := range values {
			s.sets[key][v] = p
		}
	}
	for key, values := range r.zsets {
		s.zsets[key] = map[string]float64{}
		for v, p := range values {
			s.zsets[key][v] = p
		}
	}
	for key, values := range r.lists {
		s.lists[key] = append([]string(nil), values...)
	}
	return s
}

func (r *sharedLuaRedis) removeKey(key string) {
	delete(r.data, key)
	delete(r.sets, key)
	delete(r.zsets, key)
	delete(r.lists, key)
}

func (r *sharedLuaRedis) setSet(key string, members []string) {
	r.removeKey(key)
	if len(members) == 0 {
		return
	}
	r.data[key] = bootLuaEntry{kind: "set", expireAt: -1}
	r.sets[key] = map[string]bool{}
	for _, m := range members {
		r.sets[key][m] = true
	}
}

func (r *sharedLuaRedis) setZSet(key string, members map[string]float64) {
	r.removeKey(key)
	if len(members) == 0 {
		return
	}
	r.data[key] = bootLuaEntry{kind: "zset", expireAt: -1}
	r.zsets[key] = map[string]float64{}
	for m, s := range members {
		r.zsets[key][m] = s
	}
}

func (r *sharedLuaRedis) setList(key string, values []string) {
	r.removeKey(key)
	if len(values) == 0 {
		return
	}
	r.data[key] = bootLuaEntry{kind: "list", expireAt: -1}
	r.lists[key] = append([]string(nil), values...)
}

func sharedLuaIsWrite(name string) bool {
	switch name {
	case "HSET", "SET", "UNLINK", "HDEL", "SADD", "SREM", "ZADD", "ZREM", "RENAME", "PERSIST", "PEXPIREAT", "EXPIRE", "LPUSH":
		return true
	}
	return false
}

func sharedLuaScore(score float64) string {
	if math.IsInf(score, 1) {
		return "inf"
	}
	if math.IsInf(score, -1) {
		return "-inf"
	}
	return strconv.FormatFloat(score, 'g', 17, 64)
}

func (r *sharedLuaRedis) orderedZSet(key string) []string {
	values := r.zsets[key]
	members := make([]string, 0, len(values))
	for m := range values {
		members = append(members, m)
	}
	sort.Slice(members, func(i, j int) bool {
		return values[members[i]] < values[members[j]] || values[members[i]] == values[members[j]] && members[i] < members[j]
	})
	return members
}

func sharedLuaRange(values []string, start, end int64) []string {
	n := int64(len(values))
	if start < 0 {
		start += n
	}
	if end < 0 {
		end += n
	}
	if start < 0 {
		start = 0
	}
	if end >= n {
		end = n - 1
	}
	if start >= n || end < 0 || start > end {
		return nil
	}
	return values[start : end+1]
}

func (r *sharedLuaRedis) setHash(key string, record Record) {
	hash := make(map[string]string, len(record))
	for _, field := range record {
		hash[field.Name] = string(field.Value)
	}
	r.data[key] = bootLuaEntry{kind: "hash", hash: hash, expireAt: -1}
}

func sharedLuaExecutionReply(L *lua.LState) *lua.LTable {
	for level := 1; level <= 6; level++ {
		frame, ok := L.GetStack(level)
		if !ok {
			break
		}
		for index := 1; index <= 256; index++ {
			name, value := L.GetLocal(frame, index)
			if name == "" {
				break
			}
			if table, ok := value.(*lua.LTable); ok {
				if reply, ok := table.RawGetString("reply").(*lua.LTable); ok {
					if table.RawGetString("calls").Type() == lua.LTTable && table.RawGetString("count").Type() == lua.LTNumber {
						return reply
					}
				}
			}
		}
	}
	return nil
}

func (r *sharedLuaRedis) command(L *lua.LState, acl bool) int {
	parts := make([]string, L.GetTop())
	for i := range parts {
		value, ok := L.Get(i + 1).(lua.LString)
		if !ok {
			L.RaiseError("shared facade command argument is not a bulk string")
			return 0
		}
		parts[i] = string(value)
	}
	if len(parts) == 0 {
		L.RaiseError("missing command")
		return 0
	}
	name, args := parts[0], parts[1:]
	r.trace = append(r.trace, bootLuaCommand{name: name, args: args, acl: acl})
	write := sharedLuaIsWrite(name)
	if acl {
		if !write {
			L.RaiseError("ACL checked a non-descriptor command")
			return 0
		}
		r.aclCount++
		r.prebuilt = sharedLuaExecutionReply(L)
		if r.prebuilt == nil {
			L.RaiseError("ACL check preceded prebuilt execution/reply")
			return 0
		}
		L.Push(lua.LBool(r.denyAt != r.aclCount))
		return 1
	}
	if write {
		r.attempts++
		if r.failAt == r.attempts && !r.failAfter {
			L.RaiseError(bootLuaWriteErr)
			return 0
		}
	}
	var result lua.LValue
	var entry bootLuaEntry
	var exists bool
	if len(args) > 0 {
		entry, exists = r.data[args[0]]
		// expireAt<=0 is the legacy facade's persistent/unset representation.
		if exists && entry.expireAt > 0 && uint64(entry.expireAt) <= r.now {
			r.removeKey(args[0])
			entry, exists = bootLuaEntry{}, false
		}
	}
	if name == "HLEN" || name == "HSTRLEN" || name == "HKEYS" || name == "HMGET" || name == "HSET" || name == "HDEL" {
		if exists && entry.kind != "hash" {
			L.RaiseError("WRONGTYPE")
			return 0
		}
	}
	for kind, commands := range map[string][]string{"set": {"SCARD", "SISMEMBER", "SMEMBERS", "SADD", "SREM"}, "zset": {"ZCARD", "ZSCORE", "ZRANGE", "ZRANGEBYLEX", "ZRANGEBYSCORE", "ZADD", "ZREM"}, "list": {"LLEN", "LRANGE", "LPUSH"}} {
		for _, command := range commands {
			if name == command && exists && entry.kind != kind {
				L.RaiseError("WRONGTYPE")
				return 0
			}
		}
	}
	if name == "STRLEN" || name == "GET" {
		if exists && entry.kind != "string" {
			L.RaiseError("WRONGTYPE")
			return 0
		}
	}
	switch name {
	case "TIME":
		if len(args) != 0 {
			L.RaiseError("TIME arity")
			return 0
		}
		result = bootLuaStrings(L, []string{strconv.FormatUint(r.now/1000, 10), strconv.FormatUint(r.now%1000*1000, 10)})
	case "INFO":
		if len(args) != 1 {
			L.RaiseError("INFO arity")
			return 0
		}
		if args[0] == "SERVER" {
			result = lua.LString("# Server\r\nrun_id:" + r.runID + "\r\n")
		} else if args[0] == "MEMORY" {
			result = lua.LString(fmt.Sprintf("# Memory\r\nused_memory:%d\r\nmaxmemory:%d\r\nlazyfree_pending_objects:%d\r\n", r.used, r.maximum, r.lazyfree))
		} else {
			L.RaiseError("forbidden INFO section")
			return 0
		}
	case "TYPE":
		if len(args) != 1 {
			L.RaiseError("TYPE arity")
			return 0
		}
		kind := "none"
		if exists {
			kind = entry.kind
		}
		result = bootLuaStatus(L, kind)
	case "HLEN":
		if len(args) != 1 {
			L.RaiseError("HLEN arity")
			return 0
		}
		result = lua.LNumber(len(entry.hash))
	case "HSTRLEN":
		if len(args) != 2 {
			L.RaiseError("HSTRLEN arity")
			return 0
		}
		result = lua.LNumber(len(entry.hash[args[1]]))
	case "HKEYS":
		if len(args) != 1 {
			L.RaiseError("HKEYS arity")
			return 0
		}
		fields := make([]string, 0, len(entry.hash))
		for field := range entry.hash {
			fields = append(fields, field)
		}
		sort.Strings(fields) // Redis ordering is unspecified; reader must not depend on it.
		result = bootLuaStrings(L, fields)
	case "HMGET":
		if len(args) < 2 {
			L.RaiseError("HMGET arity")
			return 0
		}
		values := make([]lua.LValue, len(args)-1)
		for i, field := range args[1:] {
			values[i] = lua.LFalse
			if value, found := entry.hash[field]; found {
				values[i] = lua.LString(value)
			}
		}
		result = bootLuaArray(L, values...)
	case "STRLEN":
		if len(args) != 1 {
			L.RaiseError("STRLEN arity")
			return 0
		}
		result = lua.LNumber(len(entry.value))
	case "GET":
		if len(args) != 1 {
			L.RaiseError("GET arity")
			return 0
		}
		result = lua.LFalse
		if exists {
			result = lua.LString(entry.value)
		}
	case "HSET":
		if len(args) < 3 || len(args)%2 != 1 {
			L.RaiseError("HSET arity")
			return 0
		}
		if !exists {
			entry = bootLuaEntry{kind: "hash", hash: map[string]string{}, expireAt: -1}
		}
		added := 0
		for i := 1; i < len(args); i += 2 {
			if _, found := entry.hash[args[i]]; !found {
				added++
			}
			entry.hash[args[i]] = args[i+1]
		}
		r.data[args[0]] = entry
		result = lua.LNumber(added)
	case "HDEL":
		if len(args) < 2 {
			L.RaiseError("HDEL arity")
			return 0
		}
		removed := 0
		for _, field := range args[1:] {
			if _, ok := entry.hash[field]; ok {
				delete(entry.hash, field)
				removed++
			}
		}
		if exists && len(entry.hash) == 0 {
			r.removeKey(args[0])
		}
		result = lua.LNumber(removed)
	case "SCARD":
		if len(args) != 1 {
			L.RaiseError("SCARD arity")
			return 0
		}
		result = lua.LNumber(len(r.sets[args[0]]))
	case "SISMEMBER":
		if len(args) != 2 {
			L.RaiseError("SISMEMBER arity")
			return 0
		}
		result = lua.LNumber(0)
		if r.sets[args[0]][args[1]] {
			result = lua.LNumber(1)
		}
	case "SMEMBERS":
		if len(args) != 1 {
			L.RaiseError("SMEMBERS arity")
			return 0
		}
		members := []string{}
		for m := range r.sets[args[0]] {
			members = append(members, m)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(members)))
		result = bootLuaStrings(L, members)
	case "SADD", "SREM":
		if len(args) < 2 {
			L.RaiseError("set mutation arity")
			return 0
		}
		members := r.sets[args[0]]
		changed := 0
		if name == "SADD" {
			if !exists {
				entry = bootLuaEntry{kind: "set", expireAt: -1}
				members = map[string]bool{}
				r.data[args[0]], r.sets[args[0]] = entry, members
			}
			for _, m := range args[1:] {
				if !members[m] {
					members[m] = true
					changed++
				}
			}
		} else {
			for _, m := range args[1:] {
				if members[m] {
					delete(members, m)
					changed++
				}
			}
			if exists && len(members) == 0 {
				r.removeKey(args[0])
			}
		}
		result = lua.LNumber(changed)
	case "ZCARD":
		if len(args) != 1 {
			L.RaiseError("ZCARD arity")
			return 0
		}
		result = lua.LNumber(len(r.zsets[args[0]]))
	case "ZSCORE":
		if len(args) != 2 {
			L.RaiseError("ZSCORE arity")
			return 0
		}
		result = lua.LFalse
		if score, ok := r.zsets[args[0]][args[1]]; ok {
			result = lua.LString(sharedLuaScore(score))
		}
	case "ZRANGE":
		if len(args) != 4 || args[3] != "WITHSCORES" {
			L.RaiseError("ZRANGE shape")
			return 0
		}
		start, e1 := strconv.ParseInt(args[1], 10, 64)
		end, e2 := strconv.ParseInt(args[2], 10, 64)
		if e1 != nil || e2 != nil {
			L.RaiseError("invalid range")
			return 0
		}
		members := sharedLuaRange(r.orderedZSet(args[0]), start, end)
		values := []string{}
		for _, m := range members {
			values = append(values, m, sharedLuaScore(r.zsets[args[0]][m]))
		}
		result = bootLuaStrings(L, values)
	case "ZRANGEBYSCORE":
		if len(args) != 7 || args[1] != "-inf" || args[3] != "WITHSCORES" || args[4] != "LIMIT" || args[5] != "0" {
			L.RaiseError("ZRANGEBYSCORE shape")
			return 0
		}
		maximum, e1 := strconv.ParseFloat(args[2], 64)
		limit, e2 := strconv.Atoi(args[6])
		if e1 != nil || e2 != nil || limit < 0 {
			L.RaiseError("score range arguments")
			return 0
		}
		values := []string{}
		for _, member := range r.orderedZSet(args[0]) {
			if r.zsets[args[0]][member] <= maximum && len(values)/2 < limit {
				values = append(values, member, sharedLuaScore(r.zsets[args[0]][member]))
			}
		}
		result = bootLuaStrings(L, values)
	case "ZRANGEBYLEX":
		if len(args) != 6 || args[2] != "+" || args[3] != "LIMIT" || args[4] != "0" {
			L.RaiseError("ZRANGEBYLEX shape")
			return 0
		}
		limit, err := strconv.Atoi(args[5])
		if err != nil || limit < 0 || args[1] != "-" && !strings.HasPrefix(args[1], "(") {
			L.RaiseError("lex arguments")
			return 0
		}
		members := r.orderedZSet(args[0])
		values := []string{}
		for _, m := range members {
			if args[1] == "-" || m > args[1][1:] {
				if len(values) < limit {
					values = append(values, m)
				}
			}
		}
		result = bootLuaStrings(L, values)
	case "ZADD":
		if len(args) < 3 || len(args)%2 != 1 {
			L.RaiseError("ZADD arity")
			return 0
		}
		scores := make([]float64, (len(args)-1)/2)
		for i := 1; i < len(args); i += 2 {
			score, err := strconv.ParseFloat(args[i], 64)
			if err != nil || math.IsNaN(score) {
				L.RaiseError("invalid ZADD score")
				return 0
			}
			scores[(i-1)/2] = score
		}
		members := r.zsets[args[0]]
		if !exists {
			entry = bootLuaEntry{kind: "zset", expireAt: -1}
			members = map[string]float64{}
			r.data[args[0]], r.zsets[args[0]] = entry, members
		}
		added := 0
		for i := 1; i < len(args); i += 2 {
			if _, ok := members[args[i+1]]; !ok {
				added++
			}
			members[args[i+1]] = scores[(i-1)/2]
		}
		result = lua.LNumber(added)
	case "ZREM":
		if len(args) < 2 {
			L.RaiseError("ZREM arity")
			return 0
		}
		members := r.zsets[args[0]]
		removed := 0
		for _, m := range args[1:] {
			if _, ok := members[m]; ok {
				delete(members, m)
				removed++
			}
		}
		if exists && len(members) == 0 {
			r.removeKey(args[0])
		}
		result = lua.LNumber(removed)
	case "LPUSH":
		if len(args) < 2 {
			L.RaiseError("LPUSH arity")
			return 0
		}
		if !exists {
			r.data[args[0]] = bootLuaEntry{kind: "list", expireAt: -1}
		}
		values := r.lists[args[0]]
		for _, value := range args[1:] {
			values = append([]string{value}, values...)
		}
		r.lists[args[0]] = values
		result = lua.LNumber(len(values))
	case "LLEN":
		if len(args) != 1 {
			L.RaiseError("LLEN arity")
			return 0
		}
		result = lua.LNumber(len(r.lists[args[0]]))
	case "LRANGE":
		if len(args) != 3 {
			L.RaiseError("LRANGE arity")
			return 0
		}
		start, e1 := strconv.ParseInt(args[1], 10, 64)
		end, e2 := strconv.ParseInt(args[2], 10, 64)
		if e1 != nil || e2 != nil {
			L.RaiseError("invalid LRANGE")
			return 0
		}
		result = bootLuaStrings(L, sharedLuaRange(r.lists[args[0]], start, end))
	case "PTTL":
		if len(args) != 1 {
			L.RaiseError("PTTL arity")
			return 0
		}
		result = lua.LNumber(-2)
		if exists {
			result = lua.LNumber(-1)
			if entry.expireAt > 0 {
				result = lua.LNumber(entry.expireAt - int64(r.now))
			}
		}
	case "PEXPIREAT", "EXPIRE":
		if len(args) != 2 {
			L.RaiseError("expiry arity")
			return 0
		}
		n, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			L.RaiseError("expiry integer")
			return 0
		}
		if name == "EXPIRE" {
			n = int64(r.now) + n*1000
		}
		result = lua.LNumber(0)
		if exists {
			if n <= int64(r.now) {
				r.removeKey(args[0])
			} else {
				entry.expireAt = n
				r.data[args[0]] = entry
			}
			result = lua.LNumber(1)
		}
	case "PERSIST":
		if len(args) != 1 {
			L.RaiseError("PERSIST arity")
			return 0
		}
		result = lua.LNumber(0)
		if exists && entry.expireAt > 0 {
			entry.expireAt = -1
			r.data[args[0]] = entry
			result = lua.LNumber(1)
		}
	case "RENAME":
		if len(args) != 2 {
			L.RaiseError("RENAME arity")
			return 0
		}
		if !exists {
			L.RaiseError("no such key")
			return 0
		}
		if args[0] != args[1] {
			set, zset, list := r.sets[args[0]], r.zsets[args[0]], r.lists[args[0]]
			r.removeKey(args[0])
			r.removeKey(args[1])
			r.data[args[1]] = entry
			if set != nil {
				r.sets[args[1]] = set
			}
			if zset != nil {
				r.zsets[args[1]] = zset
			}
			if list != nil {
				r.lists[args[1]] = list
			}
		}
		result = bootLuaStatus(L, "OK")
	case "SET":
		if len(args) != 2 {
			L.RaiseError("SET arity")
			return 0
		}
		r.removeKey(args[0])
		r.data[args[0]] = bootLuaEntry{kind: "string", value: args[1], expireAt: -1}
		result = bootLuaStatus(L, "OK")
	case "UNLINK":
		if len(args) != 1 {
			L.RaiseError("UNLINK arity")
			return 0
		}
		r.removeKey(args[0])
		result = lua.LNumber(0)
		if exists {
			result = lua.LNumber(1)
		}
	default:
		L.RaiseError("forbidden command %s", name)
		return 0
	}
	if write {
		r.writes++
		if r.failAt == r.attempts && r.failAfter {
			L.RaiseError(bootLuaWriteErr)
			return 0
		}
	}
	if r.override != nil {
		if replacement := r.override(L, name, args); replacement != nil {
			result = replacement
		}
	}
	L.Push(result)
	return 1
}

func sharedLuaRun(t *testing.T, r *sharedLuaRedis, source string, keys, args []string) bootLuaResult {
	t.Helper()
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()
	lua.OpenBase(L)
	lua.OpenMath(L)
	lua.OpenString(L)
	lua.OpenTable(L)
	L.SetTop(0)
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "collectgarbage", "print"} {
		L.SetGlobal(name, lua.LNil)
	}
	L.SetGlobal("bit", primitiveLuaBitOp(L))
	L.SetContext(luaTestContext(t))
	r.trace, r.aclCount, r.attempts, r.prebuilt, r.returnedPrebuilt = nil, 0, 0, nil, false
	redis := L.NewTable()
	L.SetFuncs(redis, map[string]lua.LGFunction{
		"call":          func(L *lua.LState) int { return r.command(L, false) },
		"acl_check_cmd": func(L *lua.LState) int { return r.command(L, true) },
		"error_reply": func(L *lua.LState) int {
			table := L.NewTable()
			table.RawSetString("err", lua.LString(L.CheckString(1)))
			L.Push(table)
			return 1
		},
	})
	L.SetGlobal("redis", redis)
	L.SetGlobal("KEYS", bootLuaStrings(L, keys))
	L.SetGlobal("ARGV", bootLuaStrings(L, args))
	fn, err := L.Load(strings.NewReader(source), "@shared-canonical-test.lua")
	if err != nil {
		t.Fatal(err)
	}
	if err := L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}); err != nil {
		return bootLuaResult{runtimeErr: err}
	}
	r.returnedPrebuilt = r.prebuilt != nil && r.prebuilt == L.Get(-1)
	return bootLuaResult{raw: bootLuaRESP(L.Get(-1))}
}

func sharedLuaCore(t *testing.T) string {
	t.Helper()
	source := "local P = (function()\n" + string(primitiveLuaRead(t, "lua_src/primitives.lua")) + "end)()\nlocal CJ = {P=P}\n"
	for _, module := range []string{"Identities", "Schemas", "Wire", "Context", "Read", "Gate", "Memory", "Plan"} {
		source += "CJ." + module + " = (function()\n" + string(primitiveLuaRead(t, "lua_src/"+strings.ToLower(module)+".lua")) + "end)()\n"
	}
	return source + "CJ.ReadProjectedState=CJ.Read.project\nCJ.Reply=CJ.Context.Reply\n"
}

func sharedLuaNoError(t *testing.T, result bootLuaResult) any {
	t.Helper()
	if result.runtimeErr != nil {
		t.Fatal(result.runtimeErr)
	}
	if err, ok := result.raw.(bootLuaErrorReply); ok {
		t.Fatalf("unexpected rejection: %s", err)
	}
	return result.raw
}

func sharedLuaAssertTrace(t *testing.T, r *sharedLuaRedis, writesExpected int) {
	t.Helper()
	timeCalls, memoryCalls, serverCalls, writes := 0, 0, 0, 0
	types, counts, sizes := map[string]bool{}, map[string]bool{}, map[string]map[string]bool{}
	var acl []bootLuaCommand
	for _, call := range r.trace {
		write := sharedLuaIsWrite(call.name)
		if writes > 0 && (call.acl || !write) {
			t.Fatal("read/ACL/build-dependent call after first write")
		}
		if call.acl {
			acl = append(acl, call)
			continue
		}
		if write {
			if len(acl) != writesExpected || writes >= len(acl) || !reflect.DeepEqual(call.args, acl[writes].args) || call.name != acl[writes].name {
				t.Fatal("all exact ACL descriptors were not prebuilt and checked before first write")
			}
			writes++
			continue
		}
		switch call.name {
		case "TIME":
			timeCalls++
		case "INFO":
			if call.args[0] == "MEMORY" {
				memoryCalls++
			} else {
				serverCalls++
			}
		case "TYPE":
			types[call.args[0]] = true
		case "HLEN", "STRLEN", "SCARD", "ZCARD", "LLEN":
			if !types[call.args[0]] {
				t.Fatal("cardinality/length read before TYPE")
			}
			counts[call.args[0]] = true
		case "HKEYS", "SMEMBERS", "SISMEMBER", "ZSCORE", "ZRANGE", "ZRANGEBYLEX", "ZRANGEBYSCORE", "LRANGE":
			if !counts[call.args[0]] {
				t.Fatal("collection retrieval before cardinality bound")
			}
		case "PTTL":
			if !types[call.args[0]] {
				t.Fatal("PTTL before TYPE")
			}
		case "HSTRLEN":
			if !counts[call.args[0]] {
				t.Fatal("field length before HLEN bound")
			}
			if sizes[call.args[0]] == nil {
				sizes[call.args[0]] = map[string]bool{}
			}
			sizes[call.args[0]][call.args[1]] = true
		case "HMGET":
			for _, field := range call.args[1:] {
				if !sizes[call.args[0]][field] {
					t.Fatal("HMGET field not bounded before bulk retrieval")
				}
			}
		case "GET":
			if !counts[call.args[0]] {
				t.Fatal("GET before STRLEN")
			}
		default:
			t.Fatalf("unexpected read %s", call.name)
		}
	}
	if timeCalls != 1 || memoryCalls > 1 || serverCalls > 1 {
		t.Fatalf("TIME/INFO counts %d/%d/%d", timeCalls, memoryCalls, serverCalls)
	}
}

func TestSharedLuaLedgerGrowthAndProjectedCallTypes(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for _, test := range []struct {
		name, body              string
		logical, keys, elements int
		code                    string
	}{
		{"fresh_hash_string", `add({"HSET",k,"a","one","bb","two"}); add({"SET",s,"value"})`, len(ContractsCandidateKey) + len(CrawlContractCandidateKey) + 14, 2, 2, ""},
		{"same_descriptor_again", `add({"HSET",k,"a","one"}); add({"HSET",k,"a","one"})`, len(ContractsCandidateKey) + 4, 1, 1, ""},
		{"changed_full_value", `add({"HSET",k,"a","long"}); add({"HSET",k,"a","x"})`, len(ContractsCandidateKey) + 6, 1, 1, ""},
		{"new_field_only", `add({"HSET",k,"a","one"}); add({"HSET",k,"b",""})`, len(ContractsCandidateKey) + 5, 1, 2, ""},
		{"string_replacement", `add({"SET",s,"long"}); add({"SET",s,"x"}); add({"SET",s,"x"})`, len(CrawlContractCandidateKey) + 5, 1, 0, ""},
		{"unlink_no_credit", `add({"SET",s,"long"}); add({"UNLINK",s}); add({"SET",s,"x"})`, 2*len(CrawlContractCandidateKey) + 5, 2, 0, ""},
		{"projected_wrong_type", `add({"SET",k,"x"}); add({"HSET",k,"a","v"})`, 0, 0, 0, "WRONG_TYPE"},
		{"unlink_type_reset", `add({"SET",k,"x"}); add({"UNLINK",k}); add({"HSET",k,"a","v"})`, 2*len(ContractsCandidateKey) + 3, 2, 1, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := sharedLuaNewRedis()
			source := sharedLuaCore(t) + `
local ctx = assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV))
local k,s=ctx.keys.candidate_compatibility,ctx.keys.candidate_contract
assert(CJ.Read.absent(ctx,k,"hash")); assert(CJ.Read.absent(ctx,s,"string"))
local p=assert(CJ.Plan.new(ctx))
local function add(a) assert(CJ.Plan.add(p,a,"ordinary")) end
` + test.body + `
local a,code=CJ.Plan.assess(ctx,p)
if not a then return CJ.Context.reject(code) end
return {P.format_decimal(a.growth),P.format_decimal(a.new_logical_bytes),P.format_decimal(a.new_keys),P.format_decimal(a.new_elements)}
`
			before := r.snapshot()
			result := sharedLuaRun(t, r, source, keys, args)
			if !reflect.DeepEqual(before, r.snapshot()) {
				t.Fatal("assessment wrote datastore")
			}
			if result.runtimeErr != nil {
				t.Fatal(result.runtimeErr)
			}
			if test.code != "" {
				if result.raw != bootLuaErrorReply("ERR CRAWL_V2_"+test.code) {
					t.Fatalf("got %v", result.raw)
				}
			} else {
				want := []any{strconv.Itoa(3*test.logical + 1024*test.keys + 256*test.elements), strconv.Itoa(test.logical), strconv.Itoa(test.keys), strconv.Itoa(test.elements)}
				if !reflect.DeepEqual(result.raw, want) {
					t.Fatalf("growth got %v want %v", result.raw, want)
				}
			}
			sharedLuaAssertTrace(t, r, 0)
		})
	}
}

func TestSharedLuaLedgerCompleteReceiptsAndPrivateCopies(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for _, body := range []string{
		`local a,c=CJ.Plan.add(p,{"SET",k,"x"},0); return {a==nil and c or "accepted"}`,
		`local a,c=CJ.Plan.add(p,{"SET",k,"x"},"free"); return {a==nil and c or "accepted"}`,
		`assert(CJ.Read.key_type(ctx,k)); assert(CJ.Plan.add(p,{"HSET",k,"schema_version","1"},"ordinary")); local a,c=CJ.Plan.assess(ctx,p); return {a==nil and c or "accepted"}`,
		`local a,c=CJ.Plan.add(p,{"INCR",k},"ordinary"); return {a==nil and c or "accepted"}`,
		`local a,c=CJ.Plan.add(p,{"RENAME",k,k},"ordinary"); return {a==nil and c or "accepted"}`,
		`local a,c=CJ.Plan.add(p,{"SET",k,"x","NX"},"ordinary"); return {a==nil and c or "accepted"}`,
		`local a,c=CJ.Plan.add(p,{"HSET",k,"f","v","f","w"},"ordinary"); return {a==nil and c or "accepted"}`,
		`local a,c=CJ.Plan.add(p,{[1]="SET",[2]=k,[4]="x"},"ordinary"); return {a==nil and c or "accepted"}`,
		`local a,c=CJ.Plan.add(p,{"SET",k,123},"ordinary"); return {a==nil and c or "accepted"}`,
		`local a,c=CJ.Plan.add(p,{"UNLINK","unrelated"},"ordinary"); return {a==nil and c or "accepted"}`,
	} {
		r := sharedLuaNewRedis()
		before := r.snapshot()
		source := sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV)); local k=ctx.keys.durability; local p=assert(CJ.Plan.new(ctx)); ` + body
		result := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args)).([]any)
		if result[0] != "INVALID_ARGUMENT" && result[0] != "INVALID_STATE" {
			t.Fatalf("not closed: %v", result)
		}
		if !reflect.DeepEqual(before, r.snapshot()) {
			t.Fatal("invalid descriptor changed datastore")
		}
	}
	// Reading a full fixed hash distinguishes a stored empty value from absence.
	// Neither the published receipt nor the descriptor's caller-owned table can
	// be changed to reduce G after Plan.add/Read.fixed_hash have copied them.
	r := sharedLuaNewRedis()
	source := sharedLuaCore(t) + `
local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV)); local k=ctx.keys.durability
local fact=assert(CJ.Read.fixed_hash(ctx,k,"durability")); fact.v.rehearsal_evidence_sha256="x"
local p=assert(CJ.Plan.new(ctx)); local a={"HSET",k,"rehearsal_evidence_sha256","x","planned_shutdown_nonce",""}
assert(CJ.Plan.add(p,a,"ordinary")); a[4]="changed after add"
local result=assert(CJ.Plan.assess(ctx,p)); return {P.format_decimal(result.growth)}
`
	if got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args)); !reflect.DeepEqual(got, []any{"3"}) {
		t.Fatalf("receipt/descriptor alias or full replacement miscount: %v", got)
	}
}

func TestSharedLuaSealClosesAllContextBoundWork(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	source := sharedLuaCore(t) + `
local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV))
local p=assert(CJ.Plan.new(ctx)); local a=assert(CJ.Plan.assess(ctx,p))
local reply=assert(CJ.Reply.build(ctx,"EXISTS_IDENTICAL",{ctx.request.v.manifest_sha256,ctx.request.v.contract_sha256}))
local fake,c=CJ.Plan.seal(ctx,p,{growth=0},reply); assert(fake==nil and c=="INVALID_STATE")
a.growth=1; local changed,d=CJ.Plan.seal(ctx,p,a,reply); assert(changed==nil and d=="INVALID_STATE"); a.growth=0
local e=assert(CJ.Plan.seal(ctx,p,a,reply)); assert(ctx.phase=="sealed")
for _,fn in ipairs({function() return CJ.Read.project(ctx,{}) end, function() return CJ.Read.key_type(ctx,ctx.keys.durability) end,
 function() return CJ.Read.snapshot(ctx,ctx.keys.durability) end, function() return CJ.Plan.new(ctx) end,
 function() return CJ.Plan.add(p,{"UNLINK",ctx.keys.durability},"ordinary") end, function() return CJ.Plan.assess(ctx,p) end,
 function() return CJ.Plan.seal(ctx,p,a,reply) end, function() return CJ.Memory.observe(ctx) end,
 function() return CJ.Gate.check(ctx) end, function() return CJ.Reply.build(ctx,"EXISTS_IDENTICAL",{}) end,
 function() return CJ.Context.call(ctx,"INFO","MEMORY") end}) do
 local v,code=fn(); assert(v==nil and code=="INVALID_STATE")
end
return e.reply
`
	r := sharedLuaNewRedis()
	sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	if len(r.trace) != 1 || r.trace[0].name != "TIME" {
		t.Fatal("sealed helper read or empty plan did memory/ACL admission")
	}
}

func TestSharedLuaBinaryGroupProjectionAndGoIdentity(t *testing.T) {
	t.Parallel()
	groups := []PolicyGroup{}
	args := []string{}
	for i, name := range []string{"a", "équipe"} {
		rate := RateScopeID(strings.Repeat(strconv.Itoa(i+1), 32))
		scope, err := DeriveGroupScopeID(rate)
		if err != nil {
			t.Fatal(err)
		}
		group := PolicyGroup{GroupID: GroupID(name), RateScopeID: rate, GroupScopeID: scope, RequestStartLimit: 10, Concurrency: 32, IntervalMS: 3600000}
		record, err := policyGroupRecord(group)
		if err != nil {
			t.Fatal(err)
		}
		groups = append(groups, group)
		args = append(args, string(primitiveLuaEncoded(t, record)))
	}
	want, err := DerivePolicyGroupMapDigest(groups)
	if err != nil {
		t.Fatal(err)
	}
	source := sharedLuaCore(t) + `local result,code=CJ.Schemas.groups(ARGV); if not result then return CJ.Context.reject(code) end; return {result.digest,P.format_decimal(result.count)}`
	r := sharedLuaNewRedis()
	got := sharedLuaNoError(t, sharedLuaRun(t, r, source, nil, args))
	if !reflect.DeepEqual(got, []any{string(want), "2"}) || len(r.trace) != 0 {
		t.Fatalf("pure Go/Lua group projection mismatch: %v", got)
	}
	badRecords := [][]string{nil, {args[1], args[0]}, {args[0], args[0]}, {args[0] + "x"}, {args[0][:len(args[0])-1]}, {""}, {string(make([]byte, 8))}, {strings.Repeat("\xff", 8)}}
	for _, bad := range badRecords {
		result := sharedLuaRun(t, r, source, nil, bad)
		if result.runtimeErr != nil {
			t.Fatal(result.runtimeErr)
		}
		if _, ok := result.raw.(bootLuaErrorReply); !ok {
			t.Fatal("malformed binary group record accepted")
		}
	}
	binary := Record{{Name: "bytes", Value: []byte{0, 255, 128, 10}}}
	source = sharedLuaCore(t) + `local p,c=CJ.Wire.record(ARGV[1],{"bytes"},64,false); if not p then return CJ.Context.reject(c) end; return {p.v.bytes}`
	got = sharedLuaNoError(t, sharedLuaRun(t, r, source, nil, []string{string(primitiveLuaEncoded(t, binary))}))
	if !reflect.DeepEqual(got, []any{string(binary[0].Value)}) {
		t.Fatal("binary RECORD was laundered through text codec")
	}
}

func TestSharedLuaAuthorityGatesAgainstGo(t *testing.T) {
	t.Parallel()
	for _, migration := range []bool{false, true} {
		artifacts := newGateArtifacts(t)
		if migration {
			artifacts = newMigrationGateArtifacts(t, 3)
		}
		for _, mode := range []string{"active", "candidate_before", "candidate_after"} {
			t.Run(fmt.Sprintf("migration=%t/%s", migration, mode), func(t *testing.T) {
				r := sharedLuaNewRedis()
				r.data[bootLuaKey].hash["boot_epoch"] = artifacts.bootEpoch
				op := OperationRetry
				input := activeGateInput(artifacts)
				if mode == "candidate_before" {
					op = OperationCreateRun
					input = candidateGateInput(artifacts, CandidateBeforeLegacyRetirement, nil)
				}
				if mode == "candidate_after" {
					// Exercise the ordinary candidate-after-retirement authority
					// gate here; PROMOTE's full semantic/replay boundary has its
					// own admin_spec-based tests below.
					op = OperationRetireLegacyKeys
					input = candidateGateInput(artifacts, CandidateAfterLegacyRetirement, &artifacts.legacy)
				}
				gate, err := NewTransportGate(op, input)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := gate.Arguments()
				args := make([]string, 7)
				for i := range raw {
					args[i] = string(raw[i])
				}
				marker, _ := artifacts.marker.Record()
				legacy, _ := artifacts.legacy.Record()
				freeze, _ := artifacts.freeze.Record()
				guard, _ := artifacts.guard.Record()
				if mode == "active" {
					r.setHash(ContractsActiveKey, marker)
					r.setHash(CommitGuardKey, guard)
					r.setHash(LegacyRetirementKey, legacy)
					r.data[CrawlContractKey] = bootLuaEntry{kind: "string", value: string(artifacts.contract)}
				} else {
					r.setHash(ContractsCandidateKey, marker)
					r.setHash(AdminFreezeKey, freeze)
					r.data[CrawlContractCandidateKey] = bootLuaEntry{kind: "string", value: string(artifacts.contract)}
					if mode == "candidate_after" {
						r.setHash(LegacyRetirementKey, legacy)
					}
				}
				keys, _ := installLuaWire(t)
				source := sharedLuaCore(t) + `local spec={operation="` + string(op) + `",fields={},key_names=CJ.Wire.authority_names,key_values=CJ.Wire.authority_keys,request_limit=2097152,tail="none"}; local ctx,c=CJ.Context.open(spec,KEYS,ARGV); if not ctx then return CJ.Context.reject(c) end; local v,e=CJ.Gate.check(ctx); if not v then return CJ.Context.reject(e) end; return {"checked"}`
				before := r.snapshot()
				got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
				if !reflect.DeepEqual(got, []any{"checked"}) || !reflect.DeepEqual(before, r.snapshot()) {
					t.Fatal("gate mismatch/mutation")
				}
				sharedLuaAssertTrace(t, r, 0)
				// Every nonempty artifact slot must be exact RECORD bytes, not its
				// framing-free hash values, empty record sentinel, or appended bytes.
				for i := 3; i < 7; i++ {
					if args[i] == "" {
						continue
					}
					for _, bad := range []string{args[i] + "x", string(make([]byte, 8)), args[i][1:]} {
						changed := append([]string(nil), args...)
						changed[i] = bad
						result := sharedLuaRun(t, r, source, keys, changed)
						if result.runtimeErr != nil {
							t.Fatal(result.runtimeErr)
						}
						if _, ok := result.raw.(bootLuaErrorReply); !ok {
							t.Fatal("malformed gate record admitted")
						}
						if !reflect.DeepEqual(before, r.snapshot()) {
							t.Fatal("gate rejection mutated datastore")
						}
					}
				}
				// Same valid shape but different immutable stored digest rejects.
				if mode == "active" {
					r.data[CommitGuardKey].hash["memory_fixture_sha256"] = strings.Repeat("f", 64)
				} else {
					r.data[AdminFreezeKey].hash["process_stop_evidence_sha256"] = strings.Repeat("f", 64)
				}
				before = r.snapshot()
				result := sharedLuaRun(t, r, source, keys, args)
				if result.runtimeErr != nil {
					t.Fatal(result.runtimeErr)
				}
				if _, ok := result.raw.(bootLuaErrorReply); !ok || !reflect.DeepEqual(before, r.snapshot()) {
					t.Fatal("stored authority drift accepted or mutated")
				}
			})
		}
	}
}

func TestSharedLuaAuthoritySchemaCodecsAgainstGo(t *testing.T) {
	t.Parallel()
	artifacts := newGateArtifacts(t)
	artifact, _ := artifacts.marker.Artifact()
	core, _ := artifacts.guard.GuardCore()
	first, err := NewFirstRequestStartEvidence(RunID(strings.Repeat("a", 32)), JobID(strings.Repeat("b", 64)), Fence(1), 1)
	if err != nil {
		t.Fatal(err)
	}
	durability, err := bootLuaDecodeHash(sharedLuaNewRedis().data[DurabilityKey].hash)
	if err != nil {
		t.Fatal(err)
	}
	type codec interface{ Record() (Record, error) }
	fixtures := []struct {
		schema RecordSchema
		value  codec
	}{
		{SchemaCompatibilityArtifact, artifact}, {SchemaCompatibilityMarker, artifacts.marker},
		{SchemaGuardCore, core}, {SchemaCommitGuard, artifacts.guard}, {SchemaLegacyRetirement, artifacts.legacy},
		{SchemaAdminFreeze, artifacts.freeze}, {SchemaFirstRequestStart, first}, {SchemaDurability, durability},
	}
	source := sharedLuaCore(t) + `
local p,c=CJ.Schemas.decode(ARGV[1],ARGV[2]); if not p then return CJ.Context.reject(c) end
local encoded,e=CJ.Schemas.encode(p); if not encoded then return CJ.Context.reject(e) end
return {encoded}
`
	for _, fixture := range fixtures {
		t.Run(string(fixture.schema), func(t *testing.T) {
			record, err := fixture.value.Record()
			if err != nil {
				t.Fatal(err)
			}
			encoded := string(primitiveLuaEncoded(t, record))
			r := sharedLuaNewRedis()
			got := sharedLuaNoError(t, sharedLuaRun(t, r, source, nil, []string{string(fixture.schema), encoded}))
			if !reflect.DeepEqual(got, []any{encoded}) || len(r.trace) != 0 {
				t.Fatal("Lua/Go authority record disagreement or impure codec")
			}
			for index := range record {
				for _, bad := range []string{"!", "\xff", "\x00"} {
					changed := cloneRecord(record)
					changed[index].Value = []byte(bad)
					if ValidateRecord(fixture.schema, changed) == nil {
						t.Fatal("Go unexpectedly accepted negative authority record")
					}
					result := sharedLuaRun(t, r, source, nil, []string{string(fixture.schema), string(primitiveLuaEncoded(t, changed))})
					if result.runtimeErr != nil {
						t.Fatal(result.runtimeErr)
					}
					if _, ok := result.raw.(bootLuaErrorReply); !ok {
						t.Fatalf("Lua accepted Go-invalid authority field %s=%q: %v", changed[index].Name, bad, result.raw)
					}
				}
			}
		})
	}
	// Semantic relations that pass basic string/hex/decimal shape validation.
	for _, fixture := range fixtures {
		record, _ := fixture.value.Record()
		mutations := map[string]string{}
		switch fixture.schema {
		case SchemaCompatibilityArtifact:
			mutations["spider_image"] = "sha256:" + ZeroSHA256
		case SchemaCompatibilityMarker:
			mutations["manifest_sha256"] = strings.Repeat("f", 64)
		case SchemaGuardCore:
			mutations["maximum_shape_sha256"] = ZeroSHA256
			mutations["redis_version"] = "7..2"
		case SchemaCommitGuard:
			mutations["candidate_run_id"] = strings.Repeat("a", 32)
			mutations["approved_at_ms"] = "0"
		case SchemaLegacyRetirement:
			mutations["deleted_bitmap"] = "10000"
			mutations["signal_queue_count"] = "1"
		case SchemaAdminFreeze:
			mutations["created_at_ms"] = "0"
		case SchemaFirstRequestStart:
			mutations["lease_fence"] = "0"
		case SchemaDurability:
			mutations["last_approval_mode"] = "planned"
			mutations["rehearsal_evidence_sha256"] = ZeroSHA256
		}
		for field, bad := range mutations {
			changed := cloneRecord(record)
			for i := range changed {
				if changed[i].Name == field {
					changed[i].Value = []byte(bad)
				}
			}
			if ValidateRecord(fixture.schema, changed) == nil {
				t.Fatal("Go semantic negative fixture accepted")
			}
			result := sharedLuaRun(t, sharedLuaNewRedis(), source, nil, []string{string(fixture.schema), string(primitiveLuaEncoded(t, changed))})
			if result.runtimeErr != nil {
				t.Fatal(result.runtimeErr)
			}
			if _, ok := result.raw.(bootLuaErrorReply); !ok {
				t.Fatalf("Lua accepted invalid %s relation %s", fixture.schema, field)
			}
		}
	}
}

func TestSharedLuaPublicHelpersCloseMalformedInput(t *testing.T) {
	t.Parallel()
	source := sharedLuaCore(t) + `
local checks={
 function() return CJ.Identities.dense({1},nil) end,
 function() return CJ.Identities.dense({[2]="gap"},2) end,
 function() return CJ.Identities.info(false,{"run_id"}) end,
 function() return CJ.Identities.framed("domain",{1}) end,
 function() return CJ.Identities.group_scope(false) end,
 function() return CJ.Schemas.get("run") end,
 function() return CJ.Schemas.project("admin_freeze",false) end,
 function() return CJ.Schemas.encode({}) end,
 function() return CJ.Schemas.groups({[1]="",[3]=""}) end,
 function() return CJ.Wire.gate("CJ2_INSTALL_CANDIDATE_MARKERS",nil) end,
 function() return CJ.Wire.record(false,{},16,true) end,
 function() return CJ.Wire.decode({},nil,nil) end,
 function() return CJ.Wire.request_size({}, {false}) end,
 function() return CJ.Read.fixed_hash({},"key","durability") end,
 function() return CJ.Read.string({},"key",64,true) end,
 function() return CJ.Read.absent({},"key","hash") end,
 function() return CJ.Read.slots({}) end,
 function() return CJ.Plan.new({}) end,
 function() return CJ.Plan.add({}, {},"ordinary") end,
 function() return CJ.Plan.assess({}, {}) end,
 function() return CJ.Plan.seal({}, {},{}, {}) end,
 function() return CJ.Context.call({},"GET","key") end
}
for _,fn in ipairs(checks) do local value,code=fn(); assert(value==nil and type(code)=="string") end
return {"closed"}
`
	r := sharedLuaNewRedis()
	if got := sharedLuaNoError(t, sharedLuaRun(t, r, source, nil, nil)); !reflect.DeepEqual(got, []any{"closed"}) || len(r.trace) != 0 {
		t.Fatal("fallible helper raised/defaulted or read datastore")
	}
	keys, args := installLuaWire(t)
	source = sharedLuaCore(t) + `
local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV))
for _,command in ipairs({{"TIME"},{"SET",ctx.keys.candidate_contract,"x"},{"CONFIG","GET","maxmemory"}}) do
 local value,code=CJ.Context.call(ctx,unpack(command)); assert(value==nil and code=="INVALID_ARGUMENT")
end
local first=assert(CJ.Memory.observe(ctx)); first.used=0; first.maximum=9007199254740991; first.slots=0
local second=assert(CJ.Memory.observe(ctx)); assert(second.used~=first.used and second.maximum~=first.maximum)
return {"closed"}
`
	r = sharedLuaNewRedis()
	sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	sharedLuaAssertTrace(t, r, 0)
}

func TestSharedLuaGroupSemanticLimitsAndScopeRecomputation(t *testing.T) {
	t.Parallel()
	rate := RateScopeID(strings.Repeat("a", 32))
	scope, err := DeriveGroupScopeID(rate)
	if err != nil {
		t.Fatal(err)
	}
	group := PolicyGroup{GroupID: "group", RateScopeID: rate, GroupScopeID: scope, RequestStartLimit: 10, Concurrency: 32, IntervalMS: 3600000}
	record, err := policyGroupRecord(group)
	if err != nil {
		t.Fatal(err)
	}
	source := sharedLuaCore(t) + `local v,c=CJ.Schemas.groups(ARGV); if not v then return CJ.Context.reject(c) end; return {v.digest}`
	for index, bads := range map[int][]string{
		0: {"", strings.Repeat("é", 65), "\n", "\xc2\x85"},
		1: {strings.Repeat("A", 32), "0"}, 2: {strings.Repeat("f", 64), ZeroSHA256},
		3: {"0", "11", "01", "1e1", "9007199254740992"}, 4: {"0", "33", "-1"}, 5: {"3600001", "00"},
	} {
		for _, bad := range bads {
			changed := cloneRecord(record)
			changed[index].Value = []byte(bad)
			result := sharedLuaRun(t, sharedLuaNewRedis(), source, nil, []string{string(primitiveLuaEncoded(t, changed))})
			if result.runtimeErr != nil {
				t.Fatal(result.runtimeErr)
			}
			if _, ok := result.raw.(bootLuaErrorReply); !ok {
				t.Fatalf("group invalid field %d accepted", index)
			}
		}
	}
	// Exercise the actual maximum group projection, not a zero/one-only stub.
	groups := make([]PolicyGroup, 64)
	args := make([]string, 64)
	for i := range groups {
		groups[i] = group
		groups[i].GroupID = GroupID(fmt.Sprintf("g%02d", i))
		record, _ := policyGroupRecord(groups[i])
		args[i] = string(primitiveLuaEncoded(t, record))
	}
	want, err := DerivePolicyGroupMapDigest(groups)
	if err != nil {
		t.Fatal(err)
	}
	got := sharedLuaNoError(t, sharedLuaRun(t, sharedLuaNewRedis(), source, nil, args))
	if !reflect.DeepEqual(got, []any{string(want)}) {
		t.Fatal("maximum group projection digest mismatch")
	}
	result := sharedLuaRun(t, sharedLuaNewRedis(), source, nil, append(args, args[0]))
	if result.runtimeErr != nil {
		t.Fatal(result.runtimeErr)
	}
	if _, ok := result.raw.(bootLuaErrorReply); !ok {
		t.Fatal("65 groups accepted")
	}
}

func TestSharedLuaWireProjectionAndClosedInventory(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	r := sharedLuaNewRedis()
	source := sharedLuaCore(t) + `
local ctx,c=CJ.Context.open(CJ.Wire.install,KEYS,ARGV); if not ctx then return CJ.Context.reject(c) end
assert(#ctx.request.records==0 and #ctx.request.repeated==0 and next(ctx.request.n)==nil)
assert(ctx.keys.durability==KEYS[1] and ctx.keys.admin_freeze==KEYS[8])
return {P.format_decimal(ctx.request.bytes),ctx.request.v.freeze_nonce,ctx.request.v.manifest_sha256}
`
	want := []any{strconv.Itoa(bootLuaMeasuredSize(t, keys, args)), args[7], args[11]}
	if got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args)); !reflect.DeepEqual(got, want) {
		t.Fatalf("typed context/RESP mismatch %v", got)
	}
	// Check the literal gate inventory against the existing closed Go oracle.
	for _, operation := range operationWireOrder {
		if operation == OperationApproveBoot {
			continue
		}
		for _, mode := range []GateMode{GateBootOnly, GateCandidate, GateActive} {
			// Invalid/incomplete artifacts can still reject a permitted mode;
			// compare mode metadata independently of those semantic gates.
			source = sharedLuaCore(t) + `local m=CJ.Wire.modes[ARGV[1]]; local allowed=m==ARGV[2] or m=="both" and (ARGV[2]=="active" or ARGV[2]=="candidate"); return {allowed and "yes" or "no"}`
			got := sharedLuaNoError(t, sharedLuaRun(t, r, source, nil, []string{string(operation), string(mode)}))
			yes := "no"
			if ValidateGateMode(operation, mode) == nil {
				yes = "yes"
			}
			if !reflect.DeepEqual(got, []any{yes}) {
				t.Fatal("closed gate inventory differs from Go")
			}
		}
	}
	// Source-owned typed tail specification: test the framing engine alone, not
	// a CREATE_RUN handler or a production key-layout authority for that operation.
	artifacts := newGateArtifacts(t)
	gate, err := NewTransportGate(OperationCreateRun, candidateGateInput(artifacts, CandidateBeforeLegacyRetirement, nil))
	if err != nil {
		t.Fatal(err)
	}
	prefix, _ := gate.Arguments()
	tailArgs := make([]string, 7)
	for i := range prefix {
		tailArgs[i] = string(prefix[i])
	}
	record := Record{textField("group_id", "group"), textField("rate_scope_id", strings.Repeat("a", 32)), textField("group_scope_id", strings.Repeat("b", 64)), textField("request_start_limit", "1"), textField("concurrency", "1"), textField("interval_ms", "0")}
	tailArgs = append(tailArgs, "1", string(primitiveLuaEncoded(t, record)))
	source = sharedLuaCore(t) + `
local spec={operation="CJ2_CREATE_RUN",fields={"policy_group_count"},key_names=CJ.Wire.authority_names,key_values=CJ.Wire.authority_keys,
request_limit=2097152,tail="records",count_field="policy_group_count",minimum=1,maximum=64,
record_fields={"group_id","rate_scope_id","group_scope_id","request_start_limit","concurrency","interval_ms"},record_limit=1024}
local ctx,c=CJ.Context.open(spec,KEYS,ARGV); if not ctx then return CJ.Context.reject(c) end
return {P.format_decimal(ctx.request.n.policy_group_count),ctx.request.records[1].v.group_id}
`
	if got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, tailArgs)); !reflect.DeepEqual(got, []any{"1", "group"}) {
		t.Fatal("binary record tail projection differs")
	}
	for _, bad := range []string{"0", "2", "01", "65", "9007199254740992"} {
		changed := append([]string(nil), tailArgs...)
		changed[7] = bad
		result := sharedLuaRun(t, r, source, keys, changed)
		if result.runtimeErr != nil {
			t.Fatal(result.runtimeErr)
		}
		if _, ok := result.raw.(bootLuaErrorReply); !ok {
			t.Fatal("bad typed-tail count accepted")
		}
	}
}

func sharedRunWire(t *testing.T, operation OperationName, count int) (keys, args, fields []string) {
	t.Helper()
	artifacts := newGateArtifacts(t)
	gate, err := NewTransportGate(operation, activeGateInput(artifacts))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := gate.Arguments()
	for _, value := range raw {
		args = append(args, string(value))
	}
	spec := operationWireSpecifications[operation]
	fields = append([]string(nil), spec.semanticFields...)
	runID := RunID(strings.Repeat("1", 32))
	for _, field := range fields {
		value := ""
		switch field {
		case "run_id":
			value = string(runID)
		case "record_count", "policy_group_count":
			value = strconv.Itoa(count)
		case "reason":
			value = "operator_cancelled"
		}
		args = append(args, value)
	}
	ids := make([]JobID, 0, count)
	for i := 0; i < count; i++ {
		record := make(Record, len(spec.recordFields))
		id := fmt.Sprintf("%064x", i+1)
		for j, field := range spec.recordFields {
			value := ""
			if field == "job_id" {
				value = id
			}
			if field == "group_id" {
				value = fmt.Sprintf("g%03d", i)
			}
			record[j] = textField(field, value)
		}
		args = append(args, string(primitiveLuaEncoded(t, record)))
		if operation != OperationCreateRun {
			ids = append(ids, JobID(id))
		}
	}
	// Existing Go key oracle only, not a fabricated production binding/request.
	expected, err := expectedOperationWireKeys(OperationWireRequest{operation: operation, keyContext: operationWireKeyContext{runID: runID, recordJobIDs: ids}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range expected {
		keys = append(keys, string(key))
	}
	return
}

func sharedLuaLiteralArray(values []string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Quote(value)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func sharedLuaRejectSource(t *testing.T, r *sharedLuaRedis, source string, keys, args []string, code ErrorCode) {
	t.Helper()
	before := r.snapshot()
	result := sharedLuaRun(t, r, source, keys, args)
	if result.runtimeErr != nil {
		t.Fatalf("expected closed helper rejection: %v", result.runtimeErr)
	}
	if !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("helper rejection mutated data/collection/TTL/counter canaries")
	}
	text, ok := result.raw.(bootLuaErrorReply)
	if !ok {
		t.Fatalf("expected rejection, got %v", result.raw)
	}
	parsed, err := ParseErrorCode(strings.TrimPrefix(string(text), "ERR CRAWL_V2_"))
	if err != nil || code != "" && parsed != code {
		t.Fatalf("code %s want %s", text, code)
	}
	for _, call := range r.trace {
		if sharedLuaIsWrite(call.name) && !call.acl {
			t.Fatal("rejected preparation attempted a write")
		}
	}
}

func TestSharedLuaRegistrationContracts(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	source := sharedLuaCore(t) + `
local def={names={"count","name"},bounds={16,4}}
local observation
assert(CJ.Schemas.register("run",def,function(v,n)
 observation=tostring(v.count)..":"..tostring(n.count)..":"..tostring(v.name)
 if n.count==nil or n.count>10 then return nil,"COUNTER_CORRUPT" end
 v.name="evil"; n.count=999; return true
end))
def.names[1]="changed";def.bounds[2]=0
local exported=assert(CJ.Schemas.get("run"));exported.names[1]="changed"
local p,pe=CJ.Schemas.project("run",{count="1",name="good",ignored="not projected"});assert(p,"initial projection: "..tostring(pe).." "..tostring(observation))
assert(p.n.count==1 and p.v.name=="good" and p.v.ignored==nil and #p.fields==2)
local encoded,ee=CJ.Schemas.encode(p);assert(encoded,"encode: "..tostring(ee));local decoded,de=CJ.Schemas.decode("run",encoded);assert(decoded,"decode: "..tostring(de))
for _,bad in ipairs({"01","-1","11","9007199254740992"}) do
 local v,c=CJ.Schemas.project("run",{count=bad,name="good"});assert(v==nil and c=="COUNTER_CORRUPT")
end
for _,name in ipairs({"run","durability","compatibility_marker","unknown"}) do
 local v,c=CJ.Schemas.register(name,{names={"x"},bounds={1}},function() return true end)
 assert(v==nil and c=="INVALID_ARGUMENT")
end
assert(CJ.Schemas.register("job",{names={"x"},bounds={1}},function() error("PRIVATE") end))
local v,c=CJ.Schemas.project("job",{x="x"});assert(v==nil and c=="INVALID_STATE")
assert(CJ.Reply.register("CJ2_TRY_CLAIM",function(ctx,status,tail)
 if status~="NO_CANDIDATE" or #tail~=0 then return nil,"INVALID_ARGUMENT" end;return true
end))
local duplicate,dc=CJ.Reply.register("CJ2_TRY_CLAIM",function() return true end);assert(duplicate==nil and dc=="INVALID_ARGUMENT")
local overwrite,oc=CJ.Reply.register("CJ2_INSTALL_CANDIDATE_MARKERS",function() return true end);assert(overwrite==nil and oc=="INVALID_ARGUMENT")
local unknown,uc=CJ.Reply.register("UNKNOWN",function() return true end);assert(unknown==nil and uc=="INVALID_ARGUMENT")
local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV))
local late,lc=CJ.Schemas.register("rate_scope",{names={"x"},bounds={1}},function() return true end);assert(late==nil and lc=="INVALID_STATE")
local lr,rc=CJ.Reply.register("CJ2_CANCEL_RUN",function() return true end);assert(lr==nil and rc=="INVALID_STATE")
return {"registered"}
`
	r := sharedLuaNewRedis()
	got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	if !reflect.DeepEqual(got, []any{"registered"}) || len(r.trace) != 1 {
		t.Fatal("registration contract did datastore work")
	}
	for _, def := range []string{`{names={"a","a"},bounds={1,1}}`, `{names={"a"},bounds={}}`, `{names={"a"},bounds={-1}}`, `{names={"a"},bounds={"1"}}`, `{names={"bad name"},bounds={1}}`, `{names={"a","b"},bounds={10485760,10485760}}`} {
		source = sharedLuaCore(t) + `local v,c=CJ.Schemas.register("run",` + def + `,function() return true end); if not v then return CJ.Context.reject(c) end;return {"unexpected"}`
		sharedLuaRejectSource(t, sharedLuaNewRedis(), source, nil, nil, "")
	}
	// The registration engine permits output bounds above the small authority
	// codec limit, without registering an actual output semantic implementation.
	source = sharedLuaCore(t) + `assert(CJ.Schemas.register("final_page",{names={"html"},bounds={20000}},function()return true end)); local v=assert(CJ.Schemas.project("final_page",{html=string.rep("x",20000)})); local s=assert(CJ.Schemas.encode(v)); return {P.format_decimal(#assert(CJ.Schemas.decode("final_page",s)).v.html)}`
	got = sharedLuaNoError(t, sharedLuaRun(t, sharedLuaNewRedis(), source, nil, nil))
	if !reflect.DeepEqual(got, []any{"20000"}) {
		t.Fatal("registered codec used a partial small-record bound")
	}
}

func TestSharedLuaTableCopySemantics(t *testing.T) {
	t.Parallel()
	source := sharedLuaCore(t) + `local a={};a.count=P.parse_decimal("1");local b={};for k,v in next,a,nil do b[k]=v end;local f,v=next(a);local nf={};for _,field in ipairs({"count","name"}) do local number=P.parse_decimal(({count="1",name="good"})[field]);if number then nf[field]=number end end;local nc={};for k,v in next,nf,nil do nc[k]=v end;return {tostring(f),tostring(v),tostring(b.count),tostring(nf.count),tostring(nc.count)}`
	if got := sharedLuaNoError(t, sharedLuaRun(t, sharedLuaNewRedis(), source, nil, nil)); !reflect.DeepEqual(got, []any{"count", "1", "1", "1", "1"}) {
		t.Fatal("iterator control/copy drift")
	}
}

func TestSharedLuaRegisteredVariableRepliesSealAndBulkTypes(t *testing.T) {
	t.Parallel()
	// Test the registration machinery, not semantic validators for unimplemented
	// transitions. Each validator below is test-source-owned and not assembled.
	keys, args := installLuaWire(t)
	for count := 0; count <= 7; count++ {
		source := sharedLuaCore(t) + fmt.Sprintf(`
assert(CJ.Reply.register("CJ2_TRY_CLAIM",function(ctx,status,tail)
 if status~="NO_CANDIDATE" or #tail~=%d then return nil,"INVALID_ARGUMENT" end
 if #tail>0 then tail[1]="modified callback copy" end;return true
end))
local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));ctx.operation="CJ2_TRY_CLAIM"
local tail={};for i=1,%d do tail[i]="v" end
local reply=assert(CJ.Reply.build(ctx,"NO_CANDIDATE",tail));tail[1]="modified input"
local p=assert(CJ.Plan.new(ctx));local a=assert(CJ.Plan.assess(ctx,p));local e=assert(CJ.Plan.seal(ctx,p,a,reply))
return e.reply
`, count, count)
		r := sharedLuaNewRedis()
		got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args)).([]any)
		if len(got) != count+2 || got[0] != "NO_CANDIDATE" || got[1] != strconv.FormatUint(r.now, 10) {
			t.Fatal("variable reply arity/clock mismatch")
		}
		for _, value := range got[2:] {
			if value != "v" {
				t.Fatal("reply validator/input mutation changed the response")
			}
		}
		if count == 0 {
			if err := ValidateOperationResponse(OperationTryClaim, got); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tail := range []string{`{false}`, `{1}`, `{[2]="hole"}`, `{"a","b","c","d","e","f","g","h"}`} {
		source := sharedLuaCore(t) + `assert(CJ.Reply.register("CJ2_TRY_CLAIM",function()return true end));local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));ctx.operation="CJ2_TRY_CLAIM";local v,c=CJ.Reply.build(ctx,"NO_CANDIDATE",` + tail + `);if not v then return CJ.Context.reject(c) end;return v`
		sharedLuaRejectSource(t, sharedLuaNewRedis(), source, keys, args, ErrorInvalidArgument)
	}
}

func TestSharedLuaRunKeyPlansMatchGoAndClockBeforeBadID(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		op    OperationName
		count int
	}{
		{OperationCreateRun, 1}, {OperationBeginRunAudit, 0}, {OperationSealRun, 0}, {OperationCancelRun, 0},
		{OperationActivateRun, 0}, {OperationPromoteDue, 0}, {OperationRecoverExpired, 0}, {OperationCancelBatch, 0},
		{OperationFinalizeRun, 0}, {OperationArchiveRun, 0}, {OperationEnqueueBatch, 500}, {OperationAuditRunBatch, 0}, {OperationAuditRunBatch, 100},
	} {
		t.Run(fmt.Sprintf("%s_%d", test.op, test.count), func(t *testing.T) {
			keys, args, fields := sharedRunWire(t, test.op, test.count)
			source := sharedLuaCore(t) + `local spec=assert(CJ.Wire.run_spec("` + string(test.op) + `",` + sharedLuaLiteralArray(fields) + `));local ctx,c=CJ.Context.open(spec,KEYS,ARGV);if not ctx then return CJ.Context.reject(c) end; local count=0;for key in next,ctx.allowed,nil do count=count+1 end;assert(ctx.keys.run=="mifolyo:crawl:v2:run:"..ctx.request.v.run_id); assert(ctx.keys.run_keys[1]==ctx.keys.run and ctx.keys.run_keys[27]==ctx.keys.run_visited_urls);for i,key in ipairs(ctx.keys.job_keys) do assert(key==ctx.keys["record_job_"..P.format_decimal(i)] and key==ctx.keys.jobs_by_id[ctx.request.records[i].v.job_id]) end;return {P.format_decimal(count),P.format_decimal(#ctx.keys.run_keys),P.format_decimal(#ctx.keys.job_keys)}`
			r := sharedLuaNewRedis()
			got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
			jobCount := test.count
			if test.op == OperationCreateRun {
				jobCount = 0
			}
			if !reflect.DeepEqual(got, []any{strconv.Itoa(len(keys)), "27", strconv.Itoa(jobCount)}) || len(r.trace) != 1 || r.trace[0].name != "TIME" {
				t.Fatalf("key plan differs from Go or read before clock: got=%v expected_keys=%d expected_jobs=%d trace=%v", got, len(keys), jobCount, r.trace)
			}
			for _, bad := range []string{"", strings.Repeat("A", 32), strings.Repeat("1", 31), "../private"} {
				changed := append([]string(nil), args...)
				changed[7] = bad
				sharedLuaRejectSource(t, r, source, keys, changed, ErrorInvalidIdentifier)
				if len(r.trace) != 1 || r.trace[0].name != "TIME" {
					t.Fatal("bad ID did not get precisely one clock read")
				}
			}
			changedKeys := append([]string(nil), keys...)
			changedKeys[len(changedKeys)-1] += "suffix"
			sharedLuaRejectSource(t, r, source, changedKeys, args, ErrorInvalidArgument)
			if test.op == OperationEnqueueBatch || test.op == OperationAuditRunBatch && test.count > 0 {
				changed := append([]string(nil), args...)
				tail := 7 + len(fields)
				changed[tail], changed[tail+1] = changed[tail+1], changed[tail]
				sharedLuaRejectSource(t, r, source, keys, changed, ErrorInvalidArgument)
			}
		})
	}
}

func TestSharedLuaScoreParsersAgainstGoAndNativeRedisSpelling(t *testing.T) {
	t.Parallel()
	source := sharedLuaCore(t) + `local n,c=CJ.Identities.score(ARGV[1]);if n==nil then return CJ.Context.reject(c) end;return {n==tonumber(ARGV[1]) and "match" or "mismatch"}`
	for _, text := range []string{"0", "0.1", "-0.1", "10000", "-1000", "9999.999999", "0.000001", "-0.000001", "01", "-0", "+1", "1.0", "0.100000", "1e2", "-1000.000001", "10000.000001", "0.0000001", "nan", "inf", " 1", "1\n"} {
		_, goErr := ParseScoreText(text)
		r := sharedLuaNewRedis()
		result := sharedLuaRun(t, r, source, nil, []string{text})
		if result.runtimeErr != nil {
			t.Fatal(result.runtimeErr)
		}
		if goErr == nil {
			if !reflect.DeepEqual(result.raw, []any{"match"}) {
				t.Fatalf("Lua rejected Go score %q: %v", text, result.raw)
			}
		} else {
			if _, ok := result.raw.(bootLuaErrorReply); !ok {
				t.Fatalf("Lua accepted Go-invalid score %q", text)
			}
		}
	}
	keys, args := installLuaWire(t)
	r := sharedLuaNewRedis()
	r.setZSet(ContractsCandidateKey, map[string]float64{"a": 0.1, "b": 1789488000123})
	source = sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local v=assert(CJ.Read.members(ctx,ctx.keys.candidate_compatibility,"zset",{"a","b","absent"},3,16));assert(v.scores.a==assert(CJ.Identities.score("0.1")) and v.scores.b==1789488000123 and v.members.absent==false and v.complete);return {v.score_text.a}`
	got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args)).([]any)
	if got[0] == "0.1" || ValidateRedisScore(ScoreText("0.1"), got[0].(string)) != nil {
		t.Fatal("test did not exercise valid noncanonical native double spelling")
	}
	sharedLuaAssertTrace(t, r, 0)
}

func TestSharedLuaCollectionReadsBoundsCompletenessAndCopies(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	r := sharedLuaNewRedis()
	r.setHash(ContractsCandidateKey, Record{textField("z", ""), textField("a", "1")})
	r.setSet(CrawlContractCandidateKey, []string{"b", "a"})
	r.setZSet(AdminFreezeKey, map[string]float64{"a": 0, "b": 0, "c": 1})
	source := sharedLuaCore(t) + `
local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local k=ctx.keys.candidate_compatibility
local card=assert(CJ.Read.cardinality(ctx,k,"hash",2));assert(card.count==2 and not card.complete and card.v.a==nil)
local h=assert(CJ.Read.hash_fields(ctx,k,{"z","absent"},2,10,10));assert(not h.complete and h.v.z=="" and h.v.absent==false and h.v.a==nil)
h.v.a="forged";h.complete=true
local all=assert(CJ.Read.dynamic_hash(ctx,k,2,10,10));assert(all.complete and all.names[1]=="a" and all.names[2]=="z" and all.v.a=="1")
local set=assert(CJ.Read.members(ctx,ctx.keys.candidate_contract,"set",{"a","absent"},2,8));assert(set.members.a and set.members.absent==false and set.members.b==nil and not set.complete)
local every=assert(CJ.Read.all_members(ctx,ctx.keys.candidate_contract,"set",2,8));assert(every.complete and every.ordered[1]=="a" and every.ordered[2]=="b")
local z=assert(CJ.Read.page(ctx,ctx.keys.admin_freeze,"zset",1,1,3,8));assert(z.ordered[1]=="b" and z.scores.b==0 and not z.complete)
local lex=assert(CJ.Read.lex_page(ctx,ctx.keys.admin_freeze,"a",1,3,8));assert(lex.ordered[1]=="b")
local allz=assert(CJ.Read.all_members(ctx,ctx.keys.admin_freeze,"zset",3,8));assert(allz.complete and #allz.ordered==3 and allz.ordered[3]=="c")
local absent=assert(CJ.Read.members(ctx,ctx.keys.commit_guard,"set",{"x"},0,1));assert(not absent.exists and absent.kind=="none" and absent.complete and absent.count==0 and absent.members.x==false)
return {"projected"}
`
	before := r.snapshot()
	got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	if !reflect.DeepEqual(got, []any{"projected"}) || !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("projection changed data or returned incorrect facts")
	}
	sharedLuaAssertTrace(t, r, 0)
	r = sharedLuaNewRedis()
	r.setList(ContractsCandidateKey, []string{"a", "a", "b"})
	source = sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local p=assert(CJ.Read.page(ctx,ctx.keys.candidate_compatibility,"list",0,500,3,1));assert(p.complete and #p.ordered==3 and p.ordered[1]==p.ordered[2]);return {"list"}`
	sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	sharedLuaAssertTrace(t, r, 0)
	for _, test := range []struct {
		kind, read string
		seed       func(*sharedLuaRedis)
	}{
		{"hash", `CJ.Read.dynamic_hash(ctx,k,1,1,1)`, func(r *sharedLuaRedis) {
			r.setHash(ContractsCandidateKey, Record{textField("a", "1"), textField("b", "2")})
		}},
		{"set", `CJ.Read.all_members(ctx,k,"set",1,1)`, func(r *sharedLuaRedis) { r.setSet(ContractsCandidateKey, []string{"a", "b"}) }},
		{"zset", `CJ.Read.page(ctx,k,"zset",0,1,1,1)`, func(r *sharedLuaRedis) { r.setZSet(ContractsCandidateKey, map[string]float64{"a": 0, "b": 1}) }},
		{"list", `CJ.Read.page(ctx,k,"list",0,1,1,1)`, func(r *sharedLuaRedis) { r.setList(ContractsCandidateKey, []string{"a", "b"}) }},
	} {
		t.Run(test.kind, func(t *testing.T) {
			r := sharedLuaNewRedis()
			test.seed(r)
			source := sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local k=ctx.keys.candidate_compatibility;local v,c=` + test.read + `;if not v then return CJ.Context.reject(c) end;return {"unexpected"}`
			sharedLuaRejectSource(t, r, source, keys, args, ErrorLimitExceeded)
			if len(r.trace) != 3 {
				t.Fatal("collection enumerated before rejecting excessive cardinality")
			}
		})
	}
}

func sharedCollectionPlan(t *testing.T, reads, commands string, execute bool) string {
	source := sharedLuaCore(t) + `
local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local k,s=ctx.keys.candidate_compatibility,ctx.keys.candidate_contract
` + reads + `
local p=assert(CJ.Plan.new(ctx));local function add(argv) assert(CJ.Plan.add(p,argv,"ordinary")) end
` + commands + `
local a,c=CJ.Plan.assess(ctx,p);if not a then return CJ.Context.reject(c) end
`
	if !execute {
		return source + `return {P.format_decimal(a.growth),P.format_decimal(a.new_keys),P.format_decimal(a.new_elements)}`
	}
	return source + `
local reply=assert(CJ.Reply.build(ctx,"CANDIDATE_INSTALLED",{ctx.request.v.manifest_sha256,ctx.request.v.contract_sha256}))
local e,code=CJ.Plan.seal(ctx,p,a,reply);if not e then return CJ.Context.reject(code) end
for i=1,e.count do redis.call(unpack(e.calls[i].argv,1,e.calls[i].argc)) end
return e.reply
`
}

func TestSharedLuaCollectionLedgerGrowthAndNativeMutations(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for _, test := range []struct {
		name, reads, commands              string
		seed                               func(*sharedLuaRedis)
		logical, newKeys, elements, writes int
		verify                             func(*testing.T, *sharedLuaRedis)
	}{
		{"set_idempotent_and_new", `assert(CJ.Read.members(ctx,k,"set",{"a","bb"},1,2))`, `add({"SADD",k,"a","bb"});add({"SADD",k,"bb"})`, func(r *sharedLuaRedis) { r.setSet(ContractsCandidateKey, []string{"a"}) }, 2, 0, 1, 2, func(t *testing.T, r *sharedLuaRedis) {
			if len(r.sets[ContractsCandidateKey]) != 2 {
				t.Fatal("SADD semantics")
			}
		}},
		{"set_remove_last_recreate", `assert(CJ.Read.members(ctx,k,"set",{"a"},1,1))`, `add({"SREM",k,"a"});add({"SADD",k,"a"})`, func(r *sharedLuaRedis) { r.setSet(ContractsCandidateKey, []string{"a"}) }, len(ContractsCandidateKey) + 1, 1, 1, 2, nil},
		{"zscore_numeric_same_changed", `assert(CJ.Read.members(ctx,k,"zset",{"a","b"},1,1))`, `add({"ZADD",k,"0.1","a"});add({"ZADD",k,"0.2","a","1789488000123","b"})`, func(r *sharedLuaRedis) { r.setZSet(ContractsCandidateKey, map[string]float64{"a": 0.1}) }, 3 + 1 + 13, 0, 1, 2, func(t *testing.T, r *sharedLuaRedis) {
			if r.zsets[ContractsCandidateKey]["a"] != 0.2 || r.zsets[ContractsCandidateKey]["b"] != 1789488000123 {
				t.Fatal("ZADD score semantics")
			}
		}},
		{"zrem_last_recreate", `assert(CJ.Read.members(ctx,k,"zset",{"a"},1,1))`, `add({"ZREM",k,"a"});add({"ZADD",k,"0.1","a"})`, func(r *sharedLuaRedis) { r.setZSet(ContractsCandidateKey, map[string]float64{"a": 0.1}) }, len(ContractsCandidateKey) + 4, 1, 1, 2, nil},
		{"hdel_last_changes_type", `assert(CJ.Read.dynamic_hash(ctx,k,1,1,1))`, `add({"HDEL",k,"a"});add({"SADD",k,"b"})`, func(r *sharedLuaRedis) { r.setHash(ContractsCandidateKey, Record{textField("a", "")}) }, len(ContractsCandidateKey) + 1, 1, 1, 2, func(t *testing.T, r *sharedLuaRedis) {
			if r.data[ContractsCandidateKey].kind != "set" || !r.sets[ContractsCandidateKey]["b"] {
				t.Fatal("last HDEL did not remove Redis hash key")
			}
		}},
		{"rename_preserves_payload_ttl", `assert(CJ.Read.members(ctx,k,"zset",{"a","b"},1,1));assert(CJ.Read.ttl(ctx,k));assert(CJ.Read.absent(ctx,s,"string"))`, `add({"RENAME",k,s});add({"ZADD",s,"0.1","a","0","b"});add({"PERSIST",s})`, func(r *sharedLuaRedis) {
			r.setZSet(ContractsCandidateKey, map[string]float64{"a": 0.1})
			e := r.data[ContractsCandidateKey]
			e.expireAt = int64(r.now + 5000)
			r.data[ContractsCandidateKey] = e
		}, len(ContractsCandidateKey) + len(CrawlContractCandidateKey) + 2, 1, 1, 3, func(t *testing.T, r *sharedLuaRedis) {
			if _, ok := r.data[ContractsCandidateKey]; ok || r.data[CrawlContractCandidateKey].expireAt != -1 || len(r.zsets[CrawlContractCandidateKey]) != 2 {
				t.Fatal("RENAME/PERSIST payload/TTL semantics")
			}
		}},
		{"deletions_no_negative_credit", `assert(CJ.Read.dynamic_hash(ctx,k,1,1,3));assert(CJ.Read.absent(ctx,s,"set"))`, `add({"HDEL",k,"a"});add({"SADD",s,"x"});add({"SREM",s,"x"})`, func(r *sharedLuaRedis) { r.setHash(ContractsCandidateKey, Record{textField("a", "big")}) }, len(CrawlContractCandidateKey) + 1, 1, 1, 3, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := sharedLuaNewRedis()
			test.seed(r)
			before := r.snapshot()
			got := sharedLuaNoError(t, sharedLuaRun(t, r, sharedCollectionPlan(t, test.reads, test.commands, false), keys, args))
			want := []any{strconv.Itoa(3*test.logical + 1024*test.newKeys + 256*test.elements), strconv.Itoa(test.newKeys), strconv.Itoa(test.elements)}
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(before, r.snapshot()) {
				t.Fatalf("descriptor accounting got %v want %v", got, want)
			}
			sharedLuaNoError(t, sharedLuaRun(t, r, sharedCollectionPlan(t, test.reads, test.commands, true), keys, args))
			if !r.returnedPrebuilt {
				t.Fatal("not prebuilt reply")
			}
			sharedLuaAssertTrace(t, r, test.writes)
			if test.verify != nil {
				test.verify(t, r)
			}
		})
	}
}

func TestSharedLuaPartialReceiptsAndRenameFailuresStayBeforeWrites(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for _, test := range []struct {
		reads, commands string
		seed            func(*sharedLuaRedis)
		code            ErrorCode
	}{
		{`local v=assert(CJ.Read.members(ctx,k,"set",{"a"},2,1));v.members.b=false;v.complete=true`, `add({"SREM",k,"a"});add({"SADD",k,"b"})`, func(r *sharedLuaRedis) { r.setSet(ContractsCandidateKey, []string{"a", "b"}) }, ErrorInvalidState},
		{`assert(CJ.Read.cardinality(ctx,k,"hash",2))`, `add({"HSET",k,"unknown","v"})`, func(r *sharedLuaRedis) {
			r.setHash(ContractsCandidateKey, Record{textField("a", "1"), textField("b", "2")})
		}, ErrorInvalidState},
		{`assert(CJ.Read.members(ctx,k,"zset",{"a"},2,1))`, `add({"ZADD",k,"2","b"})`, func(r *sharedLuaRedis) { r.setZSet(ContractsCandidateKey, map[string]float64{"a": 0, "b": 1}) }, ErrorInvalidState},
		{`assert(CJ.Read.members(ctx,k,"set",{"a"},2,1))`, `add({"SREM",k,"a"});add({"HSET",k,"field","value"})`, func(r *sharedLuaRedis) { r.setSet(ContractsCandidateKey, []string{"a", "b"}) }, ErrorWrongType},
		{`assert(CJ.Read.string(ctx,k,10,false));assert(CJ.Read.string(ctx,s,10,false))`, `add({"RENAME",k,s})`, func(r *sharedLuaRedis) {
			r.data[ContractsCandidateKey] = bootLuaEntry{kind: "string", value: "x"}
			r.data[CrawlContractCandidateKey] = bootLuaEntry{kind: "string", value: "y"}
		}, ErrorDestinationExists},
		{`assert(CJ.Read.absent(ctx,k,"hash"));assert(CJ.Read.absent(ctx,s,"hash"))`, `add({"RENAME",k,s})`, func(r *sharedLuaRedis) {}, ErrorInvalidState},
		{`assert(CJ.Read.dynamic_hash(ctx,k,1,1,1));assert(CJ.Read.absent(ctx,s,"hash"))`, `add({"RENAME",k,s});add({"PERSIST",s})`, func(r *sharedLuaRedis) { r.setHash(ContractsCandidateKey, Record{textField("a", "1")}) }, ErrorInvalidState},
	} {
		r := sharedLuaNewRedis()
		test.seed(r)
		sharedLuaRejectSource(t, r, sharedCollectionPlan(t, test.reads, test.commands, true), keys, args, test.code)
	}
}

func TestSharedLuaCollectionMalformedRepliesAndPhaseClosure(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for _, test := range []struct {
		command, reader string
		kind            string
		replacement     func(*lua.LState) lua.LValue
	}{
		{"SCARD", `CJ.Read.members(ctx,k,"set",{"a"},2,1)`, "set", func(*lua.LState) lua.LValue { return lua.LString("1") }},
		{"SISMEMBER", `CJ.Read.members(ctx,k,"set",{"a"},2,1)`, "set", func(*lua.LState) lua.LValue { return lua.LFalse }},
		{"SMEMBERS", `CJ.Read.all_members(ctx,k,"set",2,1)`, "set", func(L *lua.LState) lua.LValue { return bootLuaArray(L, lua.LFalse, lua.LString("a")) }},
		{"SMEMBERS", `CJ.Read.all_members(ctx,k,"set",2,1)`, "set", func(L *lua.LState) lua.LValue { return bootLuaStrings(L, []string{"a", "a"}) }},
		{"ZSCORE", `CJ.Read.members(ctx,k,"zset",{"a"},2,1)`, "zset", func(*lua.LState) lua.LValue { return lua.LString("nan") }},
		{"ZSCORE", `CJ.Read.members(ctx,k,"zset",{"a"},2,1)`, "zset", func(*lua.LState) lua.LValue { return lua.LString("inf") }},
		{"ZRANGE", `CJ.Read.all_members(ctx,k,"zset",2,1)`, "zset", func(L *lua.LState) lua.LValue { return bootLuaStrings(L, []string{"b", "1", "a", "0"}) }},
		{"ZRANGE", `CJ.Read.page(ctx,k,"zset",0,1,2,1)`, "zset", func(L *lua.LState) lua.LValue { return bootLuaStrings(L, []string{"a", "0", "b", "1"}) }},
		{"LRANGE", `CJ.Read.page(ctx,k,"list",0,2,2,1)`, "list", func(L *lua.LState) lua.LValue { return bootLuaStrings(L, []string{"too long", "a"}) }},
		{"HMGET", `CJ.Read.dynamic_hash(ctx,k,2,1,1)`, "hash", func(L *lua.LState) lua.LValue { return bootLuaArray(L, lua.LFalse, lua.LString("2")) }},
		{"HSTRLEN", `CJ.Read.dynamic_hash(ctx,k,2,1,1)`, "hash", func(*lua.LState) lua.LValue { return lua.LNumber(2) }},
		{"HKEYS", `CJ.Read.dynamic_hash(ctx,k,2,1,1)`, "hash", func(L *lua.LState) lua.LValue { return bootLuaStrings(L, []string{"a", "oversized"}) }},
		{"PTTL", `CJ.Read.ttl(ctx,k)`, "hash", func(*lua.LState) lua.LValue { return lua.LNumber(-2) }},
	} {
		t.Run(test.command+"/"+test.kind, func(t *testing.T) {
			r := sharedLuaNewRedis()
			switch test.kind {
			case "set":
				r.setSet(ContractsCandidateKey, []string{"a", "b"})
			case "zset":
				r.setZSet(ContractsCandidateKey, map[string]float64{"a": 0, "b": 1})
			case "list":
				r.setList(ContractsCandidateKey, []string{"a", "b"})
			case "hash":
				r.setHash(ContractsCandidateKey, Record{textField("a", "1"), textField("b", "2")})
			}
			r.override = func(L *lua.LState, name string, a []string) lua.LValue {
				if name == test.command && a[0] == ContractsCandidateKey {
					return test.replacement(L)
				}
				return nil
			}
			source := sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local k=ctx.keys.candidate_compatibility;local v,c=` + test.reader + `;if not v then return CJ.Context.reject(c) end;return {"unexpected"}`
			sharedLuaRejectSource(t, r, source, keys, args, ErrorInvalidState)
		})
	}
	// A score-zero lex page must reject a nonzero selected score.
	r := sharedLuaNewRedis()
	r.setZSet(ContractsCandidateKey, map[string]float64{"a": 1})
	source := sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local v,c=CJ.Read.lex_page(ctx,ctx.keys.candidate_compatibility,"",1,1,1);if not v then return CJ.Context.reject(c) end;return {"unexpected"}`
	sharedLuaRejectSource(t, r, source, keys, args, ErrorInvalidState)
	source = sharedLuaCore(t) + `
local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local k=ctx.keys.candidate_compatibility
local p=assert(CJ.Plan.new(ctx));local a=assert(CJ.Plan.assess(ctx,p));local reply=assert(CJ.Reply.build(ctx,"EXISTS_IDENTICAL",{ctx.request.v.manifest_sha256,ctx.request.v.contract_sha256}));assert(CJ.Plan.seal(ctx,p,a,reply))
for _,f in ipairs({function()return CJ.Read.cardinality(ctx,k,"set",0)end,function()return CJ.Read.members(ctx,k,"set",{},0,1)end,
 function()return CJ.Read.all_members(ctx,k,"set",0,1)end,function()return CJ.Read.hash_fields(ctx,k,{},0,1,1)end,
 function()return CJ.Read.dynamic_hash(ctx,k,0,1,1)end,function()return CJ.Read.page(ctx,k,"list",0,1,0,1)end,
 function()return CJ.Read.lex_page(ctx,k,"",1,0,1)end,function()return CJ.Read.ttl(ctx,k)end}) do
 local value,code=f();assert(value==nil and code=="INVALID_STATE")
end
return {"closed"}
`
	r = sharedLuaNewRedis()
	sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	if len(r.trace) != 1 {
		t.Fatal("sealed collection API accessed datastore")
	}
}

func TestSharedLuaLogical500ElementCommandsSplitWithoutTruncation(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for _, command := range []string{"HSET", "SADD", "ZADD", "HDEL", "SREM", "ZREM"} {
		t.Run(command, func(t *testing.T) {
			r := sharedLuaNewRedis()
			logical := len(ContractsCandidateKey)
			expectedCalls := 2
			kind := "set"
			if command == "HSET" || command == "HDEL" {
				kind = "hash"
			}
			if command == "ZADD" || command == "ZREM" {
				kind = "zset"
			}
			members := make([]string, 500)
			hash := Record{}
			zset := map[string]float64{}
			for i := range members {
				members[i] = fmt.Sprintf("%03d", i+1)
				logical += len(members[i])
				if command == "HSET" || command == "ZADD" {
					logical++
					expectedCalls = 4
				}
				hash = append(hash, textField(members[i], "v"))
				zset[members[i]] = 0
			}
			deleting := command == "HDEL" || command == "SREM" || command == "ZREM"
			reads := `assert(CJ.Read.absent(ctx,k,"` + kind + `"))`
			if deleting {
				if kind == "hash" {
					r.setHash(ContractsCandidateKey, hash)
					reads = `assert(CJ.Read.dynamic_hash(ctx,k,500,3,1))`
				} else {
					if kind == "set" {
						r.setSet(ContractsCandidateKey, members)
					} else {
						r.setZSet(ContractsCandidateKey, zset)
					}
					reads = `assert(CJ.Read.all_members(ctx,k,"` + kind + `",500,3))`
				}
			}
			commands := `local argv={"` + command + `",k};for i=1,500 do local member=string.format("%03d",i);`
			if command == "ZADD" {
				commands += `argv[#argv+1]="0";argv[#argv+1]=member;`
			} else {
				commands += `argv[#argv+1]=member;`
				if command == "HSET" {
					commands += `argv[#argv+1]="v";`
				}
			}
			commands += `end;add(argv)`
			before := r.snapshot()
			got := sharedLuaNoError(t, sharedLuaRun(t, r, sharedCollectionPlan(t, reads, commands, false), keys, args))
			want := []any{strconv.Itoa(3*logical + 1024 + 256*500), "1", "500"}
			if deleting {
				want = []any{"0", "0", "0"}
			}
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(before, r.snapshot()) {
				t.Fatalf("500-element G %v want %v", got, want)
			}
			sharedLuaNoError(t, sharedLuaRun(t, r, sharedCollectionPlan(t, reads, commands, true), keys, args))
			sharedLuaAssertTrace(t, r, expectedCalls)
			for _, call := range r.trace {
				if sharedLuaIsWrite(call.name) && len(call.args)+1 > 258 {
					t.Fatal("unpack ceiling exceeded")
				}
			}
			if deleting {
				if _, ok := r.data[ContractsCandidateKey]; ok {
					t.Fatal("last-element key was not removed")
				}
			} else {
				count := len(r.sets[ContractsCandidateKey])
				if kind == "hash" {
					count = len(r.data[ContractsCandidateKey].hash)
				}
				if kind == "zset" {
					count = len(r.zsets[ContractsCandidateKey])
				}
				if count != 500 {
					t.Fatal("batch silently truncated")
				}
			}
		})
	}
}

func TestSharedLuaConcrete2500DescriptorBudgetAndSplitACL(t *testing.T) {
	t.Parallel()
	// Accounting/executor maximum-shape harness, NOT an ENQUEUE implementation.
	keys, args, fields := sharedRunWire(t, OperationEnqueueBatch, 500)
	source := sharedLuaCore(t) + `
assert(CJ.Reply.register("CJ2_ENQUEUE_BATCH",function(ctx,status,tail)
 if status~="OK" or #tail~=4 then return nil,"INVALID_ARGUMENT" end;return true
end))
local spec=assert(CJ.Wire.run_spec("CJ2_ENQUEUE_BATCH",` + sharedLuaLiteralArray(fields) + `))
local ctx=assert(CJ.Context.open(spec,KEYS,ARGV));local p=assert(CJ.Plan.new(ctx))
for _,key in ipairs({ctx.keys.run_jobs,ctx.keys.run_job_order,ctx.keys.run_ready,ctx.keys.run_ready_at}) do assert(CJ.Read.absent(ctx,key,"set")) end
for i,key in ipairs(ctx.keys.job_keys) do
 assert(CJ.Read.absent(ctx,key,"hash"));local id=ctx.request.records[i].v.job_id
 for _,argv in ipairs({{"HSET",key,"field","value"},{"SADD",ctx.keys.run_jobs,id},
 {"ZADD",ctx.keys.run_job_order,"0",id},{"ZADD",ctx.keys.run_ready,"0",id},{"ZADD",ctx.keys.run_ready_at,ctx.now_text,id}}) do assert(CJ.Plan.add(p,argv,"ordinary")) end
end
local a=assert(CJ.Plan.assess(ctx,p));local reply=assert(CJ.Reply.build(ctx,"OK",{"500","0","500","1"}))
local e=assert(CJ.Plan.seal(ctx,p,a,reply));assert(e.count==2500)
for i=1,e.count do redis.call(unpack(e.calls[i].argv,1,e.calls[i].argc)) end
return e.reply
`
	r := sharedLuaNewRedis()
	got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	if err := ValidateOperationResponse(OperationEnqueueBatch, got); err != nil {
		t.Fatal(err)
	}
	if r.writes != 2500 || len(r.sets[keys[12]]) != 500 {
		t.Fatal("500*5-call execution truncated")
	}
	sharedLuaAssertTrace(t, r, 2500)
	keys, args = installLuaWire(t)
	reads := `assert(CJ.Read.absent(ctx,k,"hash"))`
	commands := `local a={"HSET",k};for i=1,500 do a[#a+1]=P.format_decimal(i);a[#a+1]="v" end;add(a)`
	for _, denied := range []int{2, 3, 4} {
		r := sharedLuaNewRedis()
		r.denyAt = denied
		sharedLuaRejectSource(t, r, sharedCollectionPlan(t, reads, commands, true), keys, args, ErrorBootUnapproved)
		if r.attempts != 0 || r.aclCount != denied {
			t.Fatal("split command wrote before complete ACL preflight")
		}
	}
	// No partial append if a duplicate occurs in a later would-be split.
	source = sharedLuaCore(t) + `
local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local p=assert(CJ.Plan.new(ctx));local a={"HSET",ctx.keys.candidate_compatibility}
for i=1,500 do a[#a+1]=P.format_decimal(i);a[#a+1]="v" end;a[1001]="1"
local v,c=CJ.Plan.add(p,a,"ordinary");assert(v==nil and c=="INVALID_ARGUMENT")
local assessment=assert(CJ.Plan.assess(ctx,p));return {P.format_decimal(assessment.growth)}
`
	r = sharedLuaNewRedis()
	if got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args)); !reflect.DeepEqual(got, []any{"0"}) || len(r.trace) != 1 {
		t.Fatal("invalid split descriptor partially appended")
	}
	source = sharedLuaCore(t) + `
local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local p=assert(CJ.Plan.new(ctx))
for i=1,4096 do assert(CJ.Plan.add(p,{"UNLINK",ctx.keys.candidate_contract},"ordinary")) end
local v,c=CJ.Plan.add(p,{"UNLINK",ctx.keys.candidate_contract},"ordinary");if not v then return CJ.Context.reject(c) end;return {"unexpected"}
`
	sharedLuaRejectSource(t, sharedLuaNewRedis(), source, keys, args, ErrorLimitExceeded)
}

func sharedCancelPlan(t *testing.T, coverage, amend string, execute bool) string {
	// Minimal test-owned schema proves registration/receipt/coverage mechanics.
	// It is not the separately owned complete run semantic validator.
	source := sharedLuaCore(t) + `
assert(CJ.Schemas.register("run",{names={"state","cancelled_at_ms","last_activity_at_ms","terminal_reason"},bounds={16,16,16,32}},
 function(v,n) if n.cancelled_at_ms==nil or n.last_activity_at_ms==nil then return nil,"INVALID_NUMBER" end;return true end))
assert(CJ.Reply.register("CJ2_CANCEL_RUN",function(ctx,status,tail)
 if status~="CANCELLED" or #tail~=2 or tail[1]~=ctx.now_text or tail[2]~=ctx.request.v.reason then return nil,"INVALID_ARGUMENT" end;return true end))
local spec=assert(CJ.Wire.run_spec("CJ2_CANCEL_RUN",{"run_id","reason"}))
local ctx=assert(CJ.Context.open(spec,KEYS,ARGV));assert(CJ.Read.fixed_hash(ctx,ctx.keys.run,"run"))
local p=assert(CJ.Plan.new(ctx));local argv={"HSET",ctx.keys.run,"state","cancelled","terminal_reason",ctx.request.v.reason,"cancelled_at_ms",ctx.now_text,"last_activity_at_ms",ctx.now_text}
` + amend + `
local added,ac=CJ.Plan.add(p,argv,"` + coverage + `");if not added then return CJ.Context.reject(ac) end
local a,c=CJ.Plan.assess(ctx,p);if not a then return CJ.Context.reject(c) end
`
	if !execute {
		return source + `return {P.format_decimal(a.growth),a.admission,P.format_decimal(a.new_keys),P.format_decimal(a.new_elements)}`
	}
	return source + `
local reply=assert(CJ.Reply.build(ctx,"CANCELLED",{ctx.now_text,ctx.request.v.reason}));local e,c=CJ.Plan.seal(ctx,p,a,reply);if not e then return CJ.Context.reject(c) end
for i=1,e.count do redis.call(unpack(e.calls[i].argv,1,e.calls[i].argc)) end
return e.reply
`
}

func TestSharedLuaCancelRunSafetyIsNarrowAndExact(t *testing.T) {
	t.Parallel()
	keys, args, _ := sharedRunWire(t, OperationCancelRun, 0)
	runKey := keys[11]
	seed := func(r *sharedLuaRedis) {
		r.setHash(runKey, Record{textField("state", "active"), textField("cancelled_at_ms", "0"), textField("last_activity_at_ms", "1"), textField("terminal_reason", "none")})
		r.setHash(StageSlotsKey, Record{textField(strings.Repeat("c", 64), "65536:"+strings.Repeat("a", 32)+":"+strings.Repeat("b", 64)+":1:0")})
	}
	growth := uint64(3 * (len("cancelled") + len(args[8]) + 2*len(strconv.FormatUint(bootLuaNow, 10))))
	for _, extra := range []int64{-1, 0, 16777215, 16777216} {
		t.Run(strconv.FormatInt(extra, 10), func(t *testing.T) {
			r := sharedLuaNewRedis()
			seed(r)
			r.maximum = uint64(int64(r.used+65536+67108864+growth) + extra)
			source := sharedCancelPlan(t, "cancel_run", "", false)
			if extra < 0 {
				sharedLuaRejectSource(t, r, source, keys, args, ErrorMemoryHeadroomLow)
				return
			}
			before := r.snapshot()
			got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
			admission := "cancel_run_safety"
			if extra == 16777216 {
				admission = "ordinary"
			}
			want := []any{strconv.FormatUint(growth, 10), admission, "0", "0"}
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(before, r.snapshot()) {
				t.Fatalf("cancel G/admission got %v want %v", got, want)
			}
			if extra < 16777216 {
				sharedLuaRejectSource(t, r, sharedCancelPlan(t, "ordinary", "", false), keys, args, ErrorMemoryHeadroomLow)
			}
			got = sharedLuaNoError(t, sharedLuaRun(t, r, sharedCancelPlan(t, "cancel_run", "", true), keys, args))
			if err := ValidateOperationResponse(OperationCancelRun, got); err != nil {
				t.Fatal(err)
			}
			sharedLuaAssertTrace(t, r, 1)
			if r.data[runKey].hash["state"] != "cancelled" || r.data[runKey].hash["cancelled_at_ms"] != strconv.FormatUint(r.now, 10) {
				t.Fatal("cancel safety did not execute prebuilt mutation")
			}
		})
	}
	for _, amend := range []string{`argv[3]="job_count"`, `argv[4]="active"`, `argv[8]="1"`, `argv[6]="changed reason"`, `argv[2]=ctx.keys.active_contract`, `argv[#argv+1]="extra";argv[#argv+1]="value"`, `ctx.operation="CJ2_CREATE_RUN"`} {
		r := sharedLuaNewRedis()
		seed(r)
		sharedLuaRejectSource(t, r, sharedCancelPlan(t, "cancel_run", amend, true), keys, args, ErrorInvalidArgument)
	}
	for _, change := range []string{"absent", "cancelled", "partial_receipt"} {
		r := sharedLuaNewRedis()
		seed(r)
		source := sharedCancelPlan(t, "cancel_run", "", true)
		if change == "absent" {
			r.removeKey(runKey)
		} else if change == "cancelled" {
			r.data[runKey].hash["state"] = "cancelled"
		} else {
			source = strings.Replace(source, `assert(CJ.Read.fixed_hash(ctx,ctx.keys.run,"run"))`, `assert(CJ.Read.dynamic_hash(ctx,ctx.keys.run,4,32,32))`, 1)
		}
		sharedLuaRejectSource(t, r, source, keys, args, ErrorInvalidState)
	}
	// No second call or mix can borrow the constant-size transition's reserve.
	r := sharedLuaNewRedis()
	seed(r)
	amend := `assert(CJ.Plan.add(p,{"HSET",ctx.keys.run,"state","active"},"ordinary"))`
	sharedLuaRejectSource(t, r, sharedCancelPlan(t, "cancel_run", amend, true), keys, args, ErrorInvalidArgument)
}

func TestSharedLuaExtendedWriteErrorDoesNotRollback(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	reads := `assert(CJ.Read.absent(ctx,k,"set"));assert(CJ.Read.absent(ctx,s,"set"))`
	commands := `add({"SADD",k,"a"});add({"RENAME",k,s});add({"SREM",s,"a"})`
	for failed := 1; failed <= 3; failed++ {
		r := sharedLuaNewRedis()
		r.failAt = failed
		result := sharedLuaRun(t, r, sharedCollectionPlan(t, reads, commands, true), keys, args)
		if result.runtimeErr == nil || !strings.Contains(result.runtimeErr.Error(), bootLuaWriteErr) || result.raw != nil {
			t.Fatal("late write failure reclassified")
		}
		if r.writes != failed-1 {
			t.Fatal("earlier writes were rolled back")
		}
		if failed == 2 && !r.sets[ContractsCandidateKey]["a"] {
			t.Fatal("earlier SADD lost")
		}
		if failed == 3 && !r.sets[CrawlContractCandidateKey]["a"] {
			t.Fatal("earlier RENAME lost")
		}
		sharedLuaAssertTrace(t, r, 3)
	}
}

func TestSharedLuaAssemblerExplicitRegistrationAndURLFactories(t *testing.T) {
	t.Parallel()
	script := filepath.Clean(filepath.Join("..", "..", "..", "..", "..", "scripts", "generate-crawl-jobs-v2-lua.py"))
	// No files or new canonical recipes: use the assembler's explicit recipe
	// function in memory, then replace only the test's operation tail with probes.
	program := `import importlib.util, pathlib, sys
p=pathlib.Path(sys.argv[1]).resolve()
s=importlib.util.spec_from_file_location("cj2_assembly_test",p)
m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
root=p.parents[1]/"services/spider/internal/database/crawljobsv2/lua_src"
recipe=m.implemented("ops/cj2_install_candidate_markers.lua",(("Extra","identities.lua"),),True)
assembled=m.assemble(root,*recipe)
assert assembled==m.assemble(root,*recipe)
assert len(m.INVENTORY)==43 and set(m.RECIPES)==set(m.INVENTORY)
tail=(root/recipe[1]).read_bytes()
assert assembled.endswith(tail)
sys.stdout.buffer.write(assembled[:-len(tail)])
`
	out, err := exec.Command("python3", "-B", "-c", program, script).CombinedOutput()
	if err != nil {
		t.Fatalf("explicit recipe: %v %s", err, out)
	}
	source := string(out) + `local u=assert(CJ.URL.check_canonical("https://example.org/",1));assert(CJ.Extra.hex(string.rep("a",32),32));assert(CJ.Schemas.register("run",{names={"x"},bounds={1}},function()return true end));return {u.canonical_url,u.origin}`
	r := sharedLuaNewRedis()
	got := sharedLuaNoError(t, sharedLuaRun(t, r, source, nil, nil))
	if !reflect.DeepEqual(got, []any{"https://example.org/", "https://example.org:443"}) || len(r.trace) != 0 {
		t.Fatal("factory dependencies or module registration order failed")
	}
}

func sharedAdminWire(t *testing.T, operation OperationName, migration bool, runCount int) (keys, args []string, artifacts gateArtifacts) {
	t.Helper()
	artifacts = newGateArtifacts(t)
	if migration {
		artifacts = newMigrationGateArtifacts(t, 3)
	}
	var request OperationWireRequest
	var err error
	switch operation {
	case OperationRetireLegacyKeys:
		gate, e := NewTransportGate(operation, candidateGateInput(artifacts, CandidateBeforeLegacyRetirement, nil))
		if e != nil {
			t.Fatal(e)
		}
		input, e := artifacts.legacy.Input()
		if e != nil {
			t.Fatal(e)
		}
		request, err = NewRetireLegacyKeysWireRequest(gate, RetireLegacyKeysWireInput{
			FreezeNonce: input.FreezeNonce, BackupSHA256: input.BackupSHA256, V1Count: input.V1Count, V1URLFieldCount: input.V1URLFieldCount, V1DepthFieldCount: input.V1DepthFieldCount,
			V1SourceSHA256: input.V1SourceSHA256, V1QueueEvidenceSHA256: input.V1QueueEvidenceSHA256, V1URLsEvidenceSHA256: input.V1URLsEvidenceSHA256, V1DepthsEvidenceSHA256: input.V1DepthsEvidenceSHA256,
			SpiderQueueType: input.SpiderQueueType, SpiderQueueCount: input.SpiderQueueCount, SpiderQueueEvidenceSHA256: input.SpiderQueueEvidenceSHA256,
			SignalQueueType: input.SignalQueueType, SignalQueueCount: input.SignalQueueCount, SignalQueueEvidenceSHA256: input.SignalQueueEvidenceSHA256,
		})
	case OperationPromoteCandidateContracts:
		gate, e := NewTransportGate(operation, candidateGateInput(artifacts, CandidateAfterLegacyRetirement, &artifacts.legacy))
		if e != nil {
			t.Fatal(e)
		}
		core, e := artifacts.guard.GuardCore()
		if e != nil {
			t.Fatal(e)
		}
		request, err = NewPromoteCandidateContractsWireRequest(gate, PromoteCandidateContractsWireInput{FreezeNonce: artifacts.freeze.FreezeNonce(), GuardCore: core})
	case OperationMarkPlannedShutdown:
		gate, e := NewTransportGate(operation, activeGateInput(artifacts))
		if e != nil {
			t.Fatal(e)
		}
		ids := make([]RunID, runCount)
		for i := range ids {
			ids[i] = RunID(fmt.Sprintf("%032x", i+1))
		}
		request, err = NewMarkPlannedShutdownWireRequest(gate, MarkPlannedShutdownWireInput{PlannedShutdownNonce: strings.Repeat("e", 32), ProcessStopEvidenceSHA256: Digest(strings.Repeat("f", 64)), ActiveRunIDs: ids})
	default:
		t.Fatal("unsupported admin test operation")
	}
	if err != nil {
		t.Fatal(err)
	}
	k, a, _, err := request.validatedWireParts()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range k {
		keys = append(keys, string(v))
	}
	for _, v := range a {
		args = append(args, string(v))
	}
	return
}

func sharedAdminState(t *testing.T, r *sharedLuaRedis, a gateArtifacts, active, legacy, planned bool, args []string) {
	t.Helper()
	r.data[DurabilityKey].hash["boot_epoch"] = a.bootEpoch
	marker, e := a.marker.Record()
	if e != nil {
		t.Fatal(e)
	}
	if active {
		r.setHash(ContractsActiveKey, marker)
		guard, e := a.guard.Record()
		if e != nil {
			t.Fatal(e)
		}
		r.setHash(CommitGuardKey, guard)
		r.data[CrawlContractKey] = bootLuaEntry{kind: "string", value: string(a.contract), expireAt: -1}
	} else {
		r.setHash(ContractsCandidateKey, marker)
		freeze, e := a.freeze.Record()
		if e != nil {
			t.Fatal(e)
		}
		r.setHash(AdminFreezeKey, freeze)
		r.data[CrawlContractCandidateKey] = bootLuaEntry{kind: "string", value: string(a.contract), expireAt: -1}
	}
	if legacy {
		record, e := a.legacy.Record()
		if e != nil {
			t.Fatal(e)
		}
		r.setHash(LegacyRetirementKey, record)
	}
	if planned {
		h := r.data[DurabilityKey].hash
		h["boot_state"] = "planned"
		h["planned_shutdown_nonce"] = args[7]
		h["planned_shutdown_evidence_sha256"] = args[8]
		h["consumed_planned_shutdown_nonce"] = ""
		if _, e := bootLuaDecodeHash(h); e != nil {
			t.Fatal(e)
		}
	}
}

func sharedAdminOpenSource(t *testing.T, operation OperationName) string {
	return sharedLuaCore(t) + `local spec=assert(CJ.Wire.admin_spec("` + string(operation) + `"));local ctx,code=CJ.Context.open(spec,KEYS,ARGV);if not ctx then return CJ.Context.reject(code) end;`
}

func TestSharedLuaScientificScoresBinary64NoSkip(t *testing.T) {
	t.Parallel()
	source := sharedLuaCore(t) + `local n,c=CJ.Identities.redis_score(ARGV[1]);if n==nil then return CJ.Context.reject(c) end;return {string.format("%.17g",n)}`
	values := []string{"0", "+0", "-0", "0.0", "-0.0", "0.1", "-0.1", "0.10000000000000001", "1.", "1.e-1"}
	for _, mantissa := range []string{"1", "-1", "+1", "0", "-0", "+0"} {
		for _, exponent := range []string{"e-1", "E+0", "e+1", "e-308", "E-323", "e-324", "e-325", "e308", "E+309"} {
			values = append(values, mantissa+exponent, mantissa+".0"+exponent)
		}
	}
	for _, pair := range [][2]string{{"5", "-324"}, {"-5", "-324"}, {"25", "-325"}, {"22250738585072014", "-324"}, {"2225073858507201", "-323"}, {"17976931348623157", "292"}, {"17976931348623159", "292"}, {"9007199254740991", "0"}, {"9007199254740993", "0"}, {"1", "-9999"}, {"-1", "-9999"}} {
		values = append(values, pair[0]+"e"+pair[1], pair[0]+".0e"+pair[1])
	}
	for _, text := range values {
		t.Run(text, func(t *testing.T) {
			want, err := strconv.ParseFloat(text, 64)
			result := sharedLuaRun(t, sharedLuaNewRedis(), source, nil, []string{text})
			if result.runtimeErr != nil {
				t.Fatal(result.runtimeErr)
			}
			if err != nil || math.IsNaN(want) || math.IsInf(want, 0) {
				if result.raw != bootLuaErrorReply("ERR CRAWL_V2_INVALID_NUMBER") {
					t.Fatalf("nonfinite %q accepted: %v", text, result.raw)
				}
				return
			}
			got := sharedLuaNoError(t, result).([]any)
			value, e := strconv.ParseFloat(got[0].(string), 64)
			if e != nil || math.Float64bits(value) != math.Float64bits(want) {
				t.Fatalf("%q bits=%016x want=%016x", text, math.Float64bits(value), math.Float64bits(want))
			}
		})
	}
	for _, bad := range []string{"1e", "1e--1", "1e-1 ", " 1e-1", "1e-1\x00", "0x1p-1", "nan", "inf", "-inf", strings.Repeat("1", 65)} {
		sharedLuaRejectSource(t, sharedLuaNewRedis(), source, nil, []string{bad}, ErrorInvalidNumber)
	}
	// Normalization never alters the actual Redis spelling retained in receipts.
	keys, args := installLuaWire(t)
	r := sharedLuaNewRedis()
	r.setZSet(ContractsCandidateKey, map[string]float64{"a": 0.1})
	r.override = func(_ *lua.LState, name string, a []string) lua.LValue {
		if name == "ZSCORE" && a[0] == ContractsCandidateKey {
			return lua.LString("1e-1")
		}
		return nil
	}
	source = sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local f=assert(CJ.Read.members(ctx,ctx.keys.candidate_compatibility,"zset",{"a"},1,1));assert(f.scores.a==assert(CJ.Identities.score("0.1")));return {f.score_text.a}`
	if got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args)); !reflect.DeepEqual(got, []any{"1e-1"}) {
		t.Fatal("raw Redis score was rewritten")
	}
}

func TestSharedLuaRunRecordInternalLeaseReadNeverGrantsWrites(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationEnqueueBatch, OperationAuditRunBatch} {
		keys, args, fields := sharedRunWire(t, op, 1)
		r := sharedLuaNewRedis()
		member := strings.Repeat("1", 32) + ":" + strings.Repeat("2", 64)
		r.setZSet(ActiveLeasesKey, map[string]float64{member: float64(bootLuaNow + 60000)})
		source := sharedLuaCore(t) + `local spec=assert(CJ.Wire.run_spec("` + string(op) + `",` + sharedLuaLiteralArray(fields) + `));local ctx=assert(CJ.Context.open(spec,KEYS,ARGV))
local key="mifolyo:crawl:v2:active_leases";assert(ctx.keys.active_leases==key and ctx.allowed[key]==nil)
local fact=assert(CJ.Read.members(ctx,key,"zset",{"` + member + `"},64,97));assert(fact.members["` + member + `"] and fact.scores["` + member + `"]==1789488060123)
local p=assert(CJ.Plan.new(ctx));ctx.allowed[key]=true
local v,c=CJ.Plan.add(p,{"ZREM",key,"` + member + `"},"ordinary");assert(v==nil and c=="INVALID_ARGUMENT")
for _,outside in ipairs({key..":extra","unrelated:set","mifolyo:crawl:v2:rate_scopes"}) do
 ctx.allowed[outside]=true;local value,code=CJ.Read.key_type(ctx,outside);assert(value==nil and code=="INVALID_ARGUMENT")
 local raw,rc=CJ.Context.call(ctx,"TYPE",outside);assert(raw==nil and rc=="INVALID_ARGUMENT")
end
return {"read-only"}`
		before := r.snapshot()
		got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
		if !reflect.DeepEqual(got, []any{"read-only"}) || !reflect.DeepEqual(before, r.snapshot()) {
			t.Fatal("internal read changed wire/state authority")
		}
		sharedLuaAssertTrace(t, r, 0)
	}
	keys, args, fields := sharedRunWire(t, OperationCreateRun, 1)
	source := sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(assert(CJ.Wire.run_spec("CJ2_CREATE_RUN",` + sharedLuaLiteralArray(fields) + `)),KEYS,ARGV));ctx.operation="CJ2_ENQUEUE_BATCH";ctx.allowed["mifolyo:crawl:v2:active_leases"]=true;local v,c=CJ.Read.key_type(ctx,"mifolyo:crawl:v2:active_leases");if not v then return CJ.Context.reject(c) end;return {"unexpected"}`
	sharedLuaRejectSource(t, sharedLuaNewRedis(), source, keys, args, ErrorInvalidArgument)
}

func TestSharedLuaAdminWireExactGoLayoutsAndLexicalBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		op                        OperationName
		migration                 bool
		count, wantKeys, wantArgs int
	}{
		{OperationRetireLegacyKeys, false, 0, 16, 23}, {OperationRetireLegacyKeys, true, 0, 16, 23},
		{OperationPromoteCandidateContracts, false, 0, 29, 20}, {OperationPromoteCandidateContracts, true, 0, 56, 20},
		{OperationMarkPlannedShutdown, false, 0, 19, 10}, {OperationMarkPlannedShutdown, false, 1, 21, 11}, {OperationMarkPlannedShutdown, false, 16, 51, 26},
	} {
		t.Run(fmt.Sprintf("%s/%t/%d", tc.op, tc.migration, tc.count), func(t *testing.T) {
			keys, args, _ := sharedAdminWire(t, tc.op, tc.migration, tc.count)
			if len(keys) != tc.wantKeys || len(args) != tc.wantArgs {
				t.Fatal("Go wire expectation drift")
			}
			source := sharedAdminOpenSource(t, tc.op) + `
local names=assert(CJ.Wire.admin_spec(ctx.operation)).fields
if ctx.operation=="CJ2_PROMOTE_CANDIDATE_CONTRACTS" then
 if ctx.request.v.candidate_run_id~="" then assert(CJ.Context.bound_run(ctx)==ctx.request.v.candidate_run_id and ctx.bound_run_id==ctx.request.v.candidate_run_id and #ctx.keys.run_keys==27)
 else local id,c=CJ.Context.bound_run(ctx);assert(id==nil and c=="INVALID_STATE" and ctx.keys.run==nil) end
elseif ctx.operation=="CJ2_MARK_PLANNED_SHUTDOWN" then
 assert(ctx.global_scope_id==assert(CJ.Identities.global_scope()) and #ctx.keys.shutdown_runs==#ctx.request.repeated)
 for i,id in ipairs(ctx.request.repeated) do local p=ctx.keys.shutdown_runs[i];assert(p==ctx.keys.shutdown_by_id[id] and p.run_id==id and p.run==ctx.keys["shutdown_run_"..P.format_decimal(i)] and p.leased==ctx.keys["shutdown_leased_"..P.format_decimal(i)]) end
end
local count=0;for k in next,ctx.allowed,nil do count=count+1 end
return {table.concat(names,","),P.format_decimal(count),P.format_decimal(ctx.request.bytes)}
`
			r := sharedLuaNewRedis()
			got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
			want := []any{strings.Join(operationWireSpecifications[tc.op].semanticFields, ","), strconv.Itoa(tc.wantKeys), strconv.Itoa(bootLuaMeasuredSize(t, keys, args))}
			if !reflect.DeepEqual(got, want) || len(r.trace) != 1 || r.trace[0].name != "TIME" {
				t.Fatalf("admin wire got %v want %v", got, want)
			}
			for _, index := range []int{0, len(keys) - 1} {
				changed := append([]string(nil), keys...)
				changed[index] += "suffix"
				sharedLuaRejectSource(t, r, source, changed, args, ErrorInvalidArgument)
			}
			changed := append([]string(nil), args...)
			changed[7] = "INVALID-NONCE"
			sharedLuaRejectSource(t, r, source, keys, changed, "")
			if len(r.trace) != 1 {
				t.Fatal("bad ID did not get TIME first and no other reads")
			}
			if tc.op == OperationMarkPlannedShutdown && tc.count > 1 {
				changed = append([]string(nil), args...)
				changed[10], changed[11] = changed[11], changed[10]
				sharedLuaRejectSource(t, r, source, keys, changed, ErrorInvalidArgument)
			}
			if tc.op == OperationPromoteCandidateContracts {
				for _, index := range []int{8, 13, 14, 15, 16} {
					changed = append([]string(nil), args...)
					changed[index] = ZeroSHA256
					sharedLuaRejectSource(t, r, source, keys, changed, "")
				}
				changed = append([]string(nil), args...)
				changed[18] = strings.Repeat("A", 32)
				sharedLuaRejectSource(t, r, source, keys, changed, ErrorInvalidIdentifier)
			}
			if tc.op == OperationRetireLegacyKeys {
				for _, index := range []int{9, 10, 11, 17, 20} {
					changed = append([]string(nil), args...)
					changed[index] = "01"
					sharedLuaRejectSource(t, r, source, keys, changed, ErrorInvalidNumber)
				}
			}
		})
	}
}

func TestSharedLuaRetireBindingRequiresPrivateSoleInventoryReceipts(t *testing.T) {
	t.Parallel()
	keys, args, _ := sharedAdminWire(t, OperationRetireLegacyKeys, true, 0)
	runID := strings.Repeat("1", 32)
	otherID := strings.Repeat("2", 32)
	seed := func(r *sharedLuaRedis) {
		r.setZSet(RunsKey, map[string]float64{runID: 1})
		r.setSet(ActiveRunsKey, []string{runID})
		r.setSet(UnarchivedRunsKey, []string{runID})
		key, _ := RunKey(RunID(runID))
		r.setHash(key, Record{textField("canary", "retained")})
	}
	selectAll := `for _,entry in ipairs({{ctx.keys.runs,"zset",128},{ctx.keys.active_runs,"set",16},{ctx.keys.unarchived_runs,"set",100}}) do assert(CJ.Read.all_members(ctx,entry[1],entry[2],entry[3],32)) end;`
	source := sharedAdminOpenSource(t, OperationRetireLegacyKeys) + selectAll + `
assert(ctx.keys.run==nil);assert(CJ.Context.bind_run_read(ctx,"` + runID + `"));assert(CJ.Context.bind_run_read(ctx,"` + runID + `"))
assert(CJ.Context.bound_run(ctx)=="` + runID + `" and ctx.bound_run_id=="` + runID + `" and #ctx.keys.run_keys==27)
local f=assert(CJ.Read.dynamic_hash(ctx,ctx.keys.run,1,16,32));assert(f.v.canary=="retained")
local p=assert(CJ.Plan.new(ctx))
for _,key in ipairs(ctx.keys.run_keys) do assert(ctx.allowed[key]==nil);ctx.allowed[key]=true;local v,c=CJ.Plan.add(p,{"UNLINK",key},"ordinary");assert(v==nil and c=="INVALID_ARGUMENT") end
ctx.bound_run_id="` + otherID + `";assert(CJ.Context.bound_run(ctx)=="` + runID + `")
local rebound,rc=CJ.Context.bind_run_read(ctx,"` + otherID + `");assert(rebound==nil and rc=="IMMUTABLE_MISMATCH")
for _,key in ipairs({ctx.keys.run..":job:"..string.rep("a",64),"mifolyo:crawl:v2:run:` + otherID + `"}) do
 local v,c=CJ.Read.key_type(ctx,key);assert(v==nil and c=="INVALID_ARGUMENT")
end
return {"bound-read-only"}`
	r := sharedLuaNewRedis()
	seed(r)
	before := r.snapshot()
	got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	if !reflect.DeepEqual(got, []any{"bound-read-only"}) || !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("binding mutated datastore or failed exact scope")
	}
	for _, which := range []string{"unrequested", "cardinality_only", "missing", "different", "multiple", "zero_score"} {
		t.Run(which, func(t *testing.T) {
			r := sharedLuaNewRedis()
			seed(r)
			selectCode := selectAll
			switch which {
			case "unrequested":
				selectCode = ""
			case "cardinality_only":
				selectCode = `for _,e in ipairs({{ctx.keys.runs,"zset"},{ctx.keys.active_runs,"set"},{ctx.keys.unarchived_runs,"set"}}) do local f=assert(CJ.Read.cardinality(ctx,e[1],e[2],128));f.complete=true;f.members={};f.members["` + runID + `"] = true;ctx.selected[e[1]]=f end;`
			case "missing":
				r.removeKey(UnarchivedRunsKey)
			case "different":
				r.setSet(ActiveRunsKey, []string{otherID})
			case "multiple":
				r.setSet(ActiveRunsKey, []string{runID, otherID})
			case "zero_score":
				r.setZSet(RunsKey, map[string]float64{runID: 0})
			}
			source := sharedAdminOpenSource(t, OperationRetireLegacyKeys) + selectCode + `local v,c=CJ.Context.bind_run_read(ctx,"` + runID + `");if not v then return CJ.Context.reject(c) end;return {"unexpected"}`
			sharedLuaRejectSource(t, r, source, keys, args, "")
			for _, call := range r.trace {
				if call.name != "TIME" && call.name != "INFO" && strings.HasPrefix(call.args[0], "mifolyo:crawl:v2:run:") {
					t.Fatal("unproved binding touched a run key")
				}
			}
		})
	}
	keys, args, fields := sharedRunWire(t, OperationCancelRun, 0)
	source = sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(assert(CJ.Wire.run_spec("CJ2_CANCEL_RUN",` + sharedLuaLiteralArray(fields) + `)),KEYS,ARGV));ctx.operation="CJ2_RETIRE_LEGACY_KEYS";ctx.request.gate.mode="candidate";local v,c=CJ.Context.bind_run_read(ctx,"` + runID + `");if not v then return CJ.Context.reject(c) end;return {"unexpected"}`
	sharedLuaRejectSource(t, sharedLuaNewRedis(), source, keys, args, ErrorInvalidArgument)
}

func sharedAdminGateSource(t *testing.T, op OperationName, seal bool) string {
	source := sharedLuaCore(t)
	if seal {
		source += `assert(CJ.Reply.register("` + string(op) + `",function(ctx,status,tail) if status~="EXISTS_IDENTICAL" then return nil,"INVALID_ARGUMENT" end;return true end))`
	}
	source += `local ctx,code=CJ.Context.open(assert(CJ.Wire.admin_spec("` + string(op) + `")),KEYS,ARGV);if not ctx then return CJ.Context.reject(code) end;local view,err=CJ.Gate.check(ctx);if not view then return CJ.Context.reject(err) end;`
	if !seal {
		return source + `return {view.kind,view.receipt_only and "1" or "0"}`
	}
	return source + `
assert(view.receipt_only);view.receipt_only=false;ctx.receipt_only=false
local p=assert(CJ.Plan.new(ctx));local write,denied=CJ.Plan.add(p,{"UNLINK",ctx.keys.active_contract},"ordinary");assert(write==nil and denied=="INVALID_STATE")
local tail
if ctx.operation=="CJ2_PROMOTE_CANDIDATE_CONTRACTS" then tail={ctx.request.gate.records.compatibility_marker.v.manifest_sha256,ctx.request.gate.contract,ctx.request.v.commit_guard_sha256}
else tail={ctx.request.v.planned_shutdown_nonce} end
local reply=assert(CJ.Reply.build(ctx,"EXISTS_IDENTICAL",tail));local a=assert(CJ.Plan.assess(ctx,p));local e=assert(CJ.Plan.seal(ctx,p,a,reply));assert(e.count==0)
for i=1,e.count do redis.call(unpack(e.calls[i].argv,1,e.calls[i].argc)) end
return e.reply
`
}

func TestSharedLuaPromoteAuthorityPoststateReplay(t *testing.T) {
	t.Parallel()
	for _, migration := range []bool{false, true} {
		t.Run(strconv.FormatBool(migration), func(t *testing.T) {
			keys, args, a := sharedAdminWire(t, OperationPromoteCandidateContracts, migration, 0)
			r := sharedLuaNewRedis()
			sharedAdminState(t, r, a, true, true, false, args)
			// A now-active system need not resemble the original pre-promotion drain.
			r.setSet(ActiveRunsKey, []string{strings.Repeat("b", 32)})
			r.data[StageSlotsKey] = bootLuaEntry{kind: "string", value: bootLuaSecret}
			r.maximum = 1
			r.denyAt = 1
			before := r.snapshot()
			got := sharedLuaNoError(t, sharedLuaRun(t, r, sharedAdminGateSource(t, OperationPromoteCandidateContracts, true), keys, args))
			if err := ValidateOperationResponse(OperationPromoteCandidateContracts, got); err != nil {
				t.Fatal(err)
			}
			want := []any{"EXISTS_IDENTICAL", strconv.FormatUint(r.now, 10), args[3] /* replaced below */, args[2], args[8]}
			manifest, _ := a.marker.ManifestSHA256()
			want[2] = string(manifest)
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(before, r.snapshot()) {
				t.Fatal("promotion receipt changed state or returned wrong bulk tail")
			}
			for _, call := range r.trace {
				if call.acl || call.name == "INFO" && call.args[0] == "MEMORY" {
					t.Fatal("promotion receipt entered allocation/ACL admission")
				}
				if call.name != "TIME" && call.name != "INFO" {
					allowed := false
					for _, key := range keys[:8] {
						allowed = allowed || call.args[0] == key
					}
					if !allowed {
						t.Fatal("post-promotion receipt read a fresh-state inventory")
					}
				}
			}
			sharedLuaAssertTrace(t, r, 0)
			// Identical normal candidate state remains a normal gate, not a no-write
			// receipt and not an implemented promotion transition.
			r = sharedLuaNewRedis()
			sharedAdminState(t, r, a, false, true, false, args)
			got = sharedLuaNoError(t, sharedLuaRun(t, r, sharedAdminGateSource(t, OperationPromoteCandidateContracts, false), keys, args))
			if !reflect.DeepEqual(got, []any{"candidate", "0"}) {
				t.Fatal("fresh promotion incorrectly entered receipt mode")
			}
		})
	}
	keys, args, a := sharedAdminWire(t, OperationPromoteCandidateContracts, false, 0)
	for activeMask := 0; activeMask < 8; activeMask++ {
		for candidateMask := 0; candidateMask < 8; candidateMask++ {
			if activeMask == 7 && candidateMask == 0 || activeMask == 0 && candidateMask == 7 {
				continue
			}
			r := sharedLuaNewRedis()
			sharedAdminState(t, r, a, true, true, false, args)
			marker, _ := a.marker.Record()
			freeze, _ := a.freeze.Record()
			r.setHash(ContractsCandidateKey, marker)
			r.setHash(AdminFreezeKey, freeze)
			r.data[CrawlContractCandidateKey] = bootLuaEntry{kind: "string", value: string(a.contract)}
			for i, key := range []string{ContractsActiveKey, CrawlContractKey, CommitGuardKey} {
				if activeMask&(1<<i) == 0 {
					r.removeKey(key)
				}
			}
			for i, key := range []string{ContractsCandidateKey, CrawlContractCandidateKey, AdminFreezeKey} {
				if candidateMask&(1<<i) == 0 {
					r.removeKey(key)
				}
			}
			sharedLuaRejectSource(t, r, sharedAdminGateSource(t, OperationPromoteCandidateContracts, false), keys, args, "")
		}
	}
	for _, mutation := range []string{"process", "epoch", "planned_boot", "guard_hash", "freeze_nonce", "stored_core", "stored_manifest", "stored_legacy", "candidate_residue"} {
		r := sharedLuaNewRedis()
		sharedAdminState(t, r, a, true, true, false, args)
		changed := append([]string(nil), args...)
		switch mutation {
		case "process":
			r.runID = strings.Repeat("f", 40)
		case "epoch":
			r.data[DurabilityKey].hash["boot_epoch"] = strings.Repeat("f", 32)
		case "planned_boot":
			h := r.data[DurabilityKey].hash
			h["boot_state"] = "planned"
			h["planned_shutdown_nonce"] = strings.Repeat("e", 32)
			h["planned_shutdown_evidence_sha256"] = strings.Repeat("f", 64)
		case "guard_hash":
			changed[8] = strings.Repeat("f", 64)
		case "freeze_nonce":
			changed[7] = strings.Repeat("f", 32)
		case "stored_core":
			r.data[CommitGuardKey].hash["memory_fixture_sha256"] = strings.Repeat("f", 64)
		case "stored_manifest":
			r.data[CommitGuardKey].hash["compatibility_manifest_sha256"] = strings.Repeat("f", 64)
		case "stored_legacy":
			r.data[LegacyRetirementKey].hash["backup_sha256"] = strings.Repeat("f", 64)
		case "candidate_residue":
			r.data[CrawlContractCandidateKey] = bootLuaEntry{kind: "string", value: string(a.contract)}
		}
		sharedLuaRejectSource(t, r, sharedAdminGateSource(t, OperationPromoteCandidateContracts, false), keys, changed, "")
	}
	// A different, valid guard core/cutover identity must not reconcile the stored
	// guard. Use a Go-validated migration request against the fresh post-state.
	mKeys, mArgs, _ := sharedAdminWire(t, OperationPromoteCandidateContracts, true, 0)
	r := sharedLuaNewRedis()
	sharedAdminState(t, r, a, true, true, false, args)
	sharedLuaRejectSource(t, r, sharedAdminGateSource(t, OperationPromoteCandidateContracts, false), mKeys, mArgs, "")
}

func TestSharedLuaMarkPlannedAuthorityReceiptAndIsolation(t *testing.T) {
	t.Parallel()
	keys, args, a := sharedAdminWire(t, OperationMarkPlannedShutdown, false, 2)
	for _, planned := range []bool{false, true} {
		r := sharedLuaNewRedis()
		sharedAdminState(t, r, a, true, true, planned, args)
		if !planned {
			got := sharedLuaNoError(t, sharedLuaRun(t, r, sharedAdminGateSource(t, OperationMarkPlannedShutdown, false), keys, args))
			if !reflect.DeepEqual(got, []any{"active", "0"}) {
				t.Fatal("approved MARK gate was weakened")
			}
			continue
		}
		before := r.snapshot()
		r.maximum = 1
		r.denyAt = 1
		got := sharedLuaNoError(t, sharedLuaRun(t, r, sharedAdminGateSource(t, OperationMarkPlannedShutdown, true), keys, args))
		if err := ValidateOperationResponse(OperationMarkPlannedShutdown, got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []any{"EXISTS_IDENTICAL", strconv.FormatUint(r.now, 10), args[7]}) || !reflect.DeepEqual(before, r.snapshot()) {
			t.Fatal("planned receipt changed persisted state/time")
		}
		for _, call := range r.trace {
			if call.acl || call.name == "INFO" && call.args[0] == "MEMORY" {
				t.Fatal("MARK authority receipt attempted allocation")
			}
		}
		sharedLuaAssertTrace(t, r, 0)
	}
	for _, mutation := range []string{"process", "epoch", "nonce", "evidence", "consumed", "unapproved", "active_contract", "active_guard", "legacy", "freeze_present"} {
		r := sharedLuaNewRedis()
		sharedAdminState(t, r, a, true, true, true, args)
		switch mutation {
		case "process":
			r.runID = strings.Repeat("f", 40)
		case "epoch":
			r.data[DurabilityKey].hash["boot_epoch"] = strings.Repeat("f", 32)
		case "nonce":
			r.data[DurabilityKey].hash["planned_shutdown_nonce"] = strings.Repeat("a", 32)
		case "evidence":
			r.data[DurabilityKey].hash["planned_shutdown_evidence_sha256"] = strings.Repeat("a", 64)
		case "consumed":
			r.data[DurabilityKey].hash["consumed_planned_shutdown_nonce"] = strings.Repeat("b", 32)
		case "unapproved":
			r.data[DurabilityKey].hash["boot_state"] = "unapproved"
		case "active_contract":
			r.data[CrawlContractKey] = bootLuaEntry{kind: "string", value: strings.Repeat("f", 64)}
		case "active_guard":
			r.data[CommitGuardKey].hash["memory_fixture_sha256"] = strings.Repeat("f", 64)
		case "legacy":
			r.removeKey(LegacyRetirementKey)
		case "freeze_present":
			f, _ := a.freeze.Record()
			r.setHash(AdminFreezeKey, f)
		}
		sharedLuaRejectSource(t, r, sharedAdminGateSource(t, OperationMarkPlannedShutdown, false), keys, args, "")
	}
	// The exception cannot make planned boot usable by ordinary run operations.
	k, v, fields := sharedRunWire(t, OperationCancelRun, 0)
	r := sharedLuaNewRedis()
	sharedAdminState(t, r, a, true, true, true, args)
	source := sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(assert(CJ.Wire.run_spec("CJ2_CANCEL_RUN",` + sharedLuaLiteralArray(fields) + `)),KEYS,ARGV));local view,c=CJ.Gate.check(ctx);if not view then return CJ.Context.reject(c) end;return {"unexpected"}`
	sharedLuaRejectSource(t, r, source, k, v, ErrorBootUnapproved)
}

func TestSharedLuaReceiptRestrictionCatchesPlansPreparedBeforeGate(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationPromoteCandidateContracts, OperationMarkPlannedShutdown} {
		for _, assessed := range []bool{false, true} {
			keys, args, a := sharedAdminWire(t, op, false, 0)
			r := sharedLuaNewRedis()
			sharedAdminState(t, r, a, true, true, op == OperationMarkPlannedShutdown, args)
			source := sharedLuaCore(t) + `assert(CJ.Reply.register("` + string(op) + `",function()return true end));local ctx=assert(CJ.Context.open(assert(CJ.Wire.admin_spec("` + string(op) + `")),KEYS,ARGV));assert(CJ.Read.string(ctx,ctx.keys.active_contract,64,true));local p=assert(CJ.Plan.new(ctx));assert(CJ.Plan.add(p,{"SET",ctx.keys.active_contract,ctx.request.gate.contract},"ordinary"));local assessment;`
			if assessed {
				source += `assessment=assert(CJ.Plan.assess(ctx,p));`
			}
			source += `local view=assert(CJ.Gate.check(ctx));assert(view.receipt_only);view.receipt_only=false;ctx.receipt_only=false;`
			if assessed {
				source += `local reply=assert(CJ.Reply.build(ctx,"EXISTS_IDENTICAL",{}));local v,c=CJ.Plan.seal(ctx,p,assessment,reply);if not v then return CJ.Context.reject(c) end;return {"unexpected"}`
			} else {
				source += `local v,c=CJ.Plan.assess(ctx,p);if not v then return CJ.Context.reject(c) end;return {"unexpected"}`
			}
			sharedLuaRejectSource(t, r, source, keys, args, ErrorInvalidState)
			if r.aclCount != 0 || r.attempts != 0 {
				t.Fatal("receipt-only gate left a prior plan writable")
			}
		}
	}
}

func TestSharedLuaLazyfreeObservationUsesOneInfoAndNeverDefaults(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for _, count := range []uint64{0, 1, 9007199254740991} {
		r := sharedLuaNewRedis()
		r.lazyfree = count
		source := sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local a=assert(CJ.Memory.observe(ctx));assert(a.lazyfree_pending_objects==nil);local b=assert(CJ.Memory.observe(ctx,true));b.used=0;local c=assert(CJ.Memory.observe(ctx,true));assert(c.used~=0);return {P.format_decimal(c.lazyfree_pending_objects)}`
		got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
		if !reflect.DeepEqual(got, []any{strconv.FormatUint(count, 10)}) {
			t.Fatal("lazyfree was absent/defaulted")
		}
		calls := 0
		for _, call := range r.trace {
			if call.name == "INFO" && call.args[0] == "MEMORY" {
				calls++
			}
		}
		if calls != 1 {
			t.Fatal("lazyfree requested another INFO MEMORY")
		}
	}
	for _, suffix := range []string{"", "lazyfree_pending_objects:01\r\n", "lazyfree_pending_objects:-1\r\n", "lazyfree_pending_objects:9007199254740992\r\n", "lazyfree_pending_objects:0\r\nlazyfree_pending_objects:0\r\n"} {
		r := sharedLuaNewRedis()
		r.override = func(_ *lua.LState, name string, a []string) lua.LValue {
			if name == "INFO" && a[0] == "MEMORY" {
				return lua.LString("used_memory:1\r\nmaxmemory:999999999\r\n" + suffix)
			}
			return nil
		}
		source := sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));assert(CJ.Memory.observe(ctx));local v,c=CJ.Memory.observe(ctx,true);if not v then return CJ.Context.reject(c) end;return {"unexpected"}`
		sharedLuaRejectSource(t, r, source, keys, args, ErrorInvalidState)
	}
}

func sharedRuntimeCore(t *testing.T, foundations bool) string {
	t.Helper()
	source := sharedLuaCore(t) + "local D=(function()\n" + string(primitiveLuaRead(t, "lua_src/unicode_data.lua")) + "end)()\nCJ.URL=(function(P,D)\n" + string(primitiveLuaRead(t, "lua_src/url.lua")) + "end)(P,D)\n"
	if foundations {
		for _, module := range [][2]string{{"Run", "ledger_run"}, {"Job", "ledger_job"}, {"Request", "ledger_request"}, {"StageOutput", "stage_output"}, {"Stage", "ledger_stage"}} {
			source += "CJ." + module[0] + "=(function()\n" + string(primitiveLuaRead(t, "lua_src/"+module[1]+".lua")) + "end)()\n"
		}
	}
	return source
}

func TestSharedLuaRev4WireMatchesEveryRemainingGoConstructor(t *testing.T) {
	t.Parallel()
	fixture := newWireOracleFixture(t)
	workers := map[OperationName]bool{OperationRejectReady: true, OperationTryClaim: true, OperationRenewLease: true, OperationReserveRequest: true, OperationStartRequest: true, OperationFinishRequest: true, OperationCancelReservation: true, OperationReleaseBeforeIO: true, OperationRetry: true, OperationDead: true, OperationCancelJob: true, OperationCompleteNoOutput: true}
	stage := map[OperationName]bool{OperationBeginStage: true, OperationStagePageFields: true, OperationStagePageBlob: true, OperationStageOutlinksBatch: true, OperationStageDiscoveriesBatch: true, OperationStageAliasesBatch: true, OperationStageImagesBatch: true, OperationStageImageManifest: true, OperationAbortStage: true, OperationSealStage: true, OperationCommit: true}
	maintenance := map[OperationName]bool{OperationPromoteDue: true, OperationRecoverExpired: true, OperationCancelBatch: true, OperationPurgeRunBatch: true, OperationCleanStage: true, OperationMaintainRateScopes: true}
	for _, expect := range wireOracleOperationExpectations() {
		family := ""
		if workers[expect.operation] {
			family = "worker_spec"
		}
		if stage[expect.operation] {
			family = "stage_spec"
		}
		if maintenance[expect.operation] {
			family = "maintenance_spec"
		}
		if family == "" {
			continue
		}
		t.Run(string(expect.operation), func(t *testing.T) {
			gate := wireOracleGate(t, fixture, expect.operation, wireOracleActive)
			request, err := wireOracleConstructRequest(fixture, expect.operation, wireOracleActive, gate)
			if err != nil {
				t.Fatal(err)
			}
			k, a, _, err := request.validatedWireParts()
			if err != nil {
				t.Fatal(err)
			}
			keys, args := []string{}, []string{}
			for _, key := range k {
				keys = append(keys, string(key))
			}
			for _, arg := range a {
				args = append(args, string(arg))
			}
			source := sharedRuntimeCore(t, false) + `local spec=assert(CJ.Wire.` + family + `("` + string(expect.operation) + `"));local ctx,c=CJ.Context.open(spec,KEYS,ARGV);if not ctx then return CJ.Context.reject(c) end;local n=0;for key in next,ctx.allowed,nil do n=n+1 end;return {table.concat(spec.fields,","),P.format_decimal(n),ctx.keys_pending and "1" or "0",P.format_decimal(#ctx.request.records)}`
			r := sharedLuaNewRedis()
			got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
			count := len(keys)
			pending := "0"
			if expect.operation == OperationStartRequest || expect.operation == OperationFinishRequest || expect.operation == OperationCancelReservation {
				count = 45
				pending = "1"
			}
			want := []any{strings.Join(expect.semanticFields, ","), strconv.Itoa(count), pending, strconv.Itoa(expect.tailCount)}
			if !reflect.DeepEqual(got, want) || len(r.trace) != 1 {
				t.Fatalf("wire got %v want %v trace=%v", got, want, r.trace)
			}
			changed := append([]string(nil), keys...)
			changed[0] += "bad"
			sharedLuaRejectSource(t, r, source, changed, args, ErrorInvalidArgument)
		})
	}
}

func TestSharedLuaRev4ExpiryPushAndBoundedDueSemantics(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	r := sharedLuaNewRedis()
	reads := `assert(CJ.Read.absent(ctx,k,"list"));assert(CJ.Read.absent(ctx,s,"list"))`
	commands := `add({"LPUSH",k,"a","b","a"});add({"PEXPIREAT",k,P.format_decimal(ctx.now_ms+5000)});add({"RENAME",k,s});add({"PERSIST",s});add({"EXPIRE",s,"86400"})`
	source := sharedCollectionPlan(t, reads, commands, true)
	sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	sharedLuaAssertTrace(t, r, 5)
	if !reflect.DeepEqual(r.lists[CrawlContractCandidateKey], []string{"a", "b", "a"}) || r.data[CrawlContractCandidateKey].expireAt != int64(r.now+86400000) {
		t.Fatal("LPUSH/expiry/rename projected semantics")
	}
	r = sharedLuaNewRedis()
	r.setZSet(ContractsCandidateKey, map[string]float64{"a": 1, "b": 2, "c": 999999})
	source = sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local p=assert(CJ.Read.due(ctx,ctx.keys.candidate_compatibility,"2",101,3,1));assert(#p.ordered==2 and p.ordered[1]=="a" and p.ordered[2]=="b" and not p.complete);return {"due"}`
	sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	sharedLuaAssertTrace(t, r, 0)
	for _, command := range []string{`{"PEXPIREAT",k,"0"}`, `{"PEXPIREAT",k,ctx.now_text}`, `{"EXPIRE",k,"86401"}`, `{"EXPIRE",k,"01"}`, `{"EXPIRE",k,"9007199254740992"}`, `{"PEXPIREAT",k,"9007199254740992"}`} {
		source = sharedLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.install,KEYS,ARGV));local p=assert(CJ.Plan.new(ctx));local k=ctx.keys.candidate_contract;local v,c=CJ.Plan.add(p,` + command + `,"ordinary");if not v then return CJ.Context.reject(c) end;return {"unexpected"}`
		sharedLuaRejectSource(t, sharedLuaNewRedis(), source, keys, args, "")
	}
}

// Use the actual Stage proof implementation and actual shared Redis command
// facade, not a fake slot/G object or a copied operation transition.
func sharedStageMemoryFixture(t *testing.T, remaining uint64) (*stageLuaFixture, *sharedLuaRedis, *lua.LTable, lua.LValue, *lua.LTable) {
	t.Helper()
	f := stageLuaFixtureNew(t)
	f.vm.load(t, "Memory", "memory")
	f.r.hashes[StageSlotsKey][string(f.commit)] = strconv.FormatUint(remaining, 10) + ":" + string(f.lease.RunID) + ":" + string(f.lease.JobID) + ":1:0"
	ctx, view, job := f.open(t, "CJ2_STAGE_PAGE_BLOB", "")
	stage, code := f.check(t, view, job, "check_owned")
	if code != lua.LNil {
		t.Fatal(code)
	}
	input := f.input(t, ChunkHTML, 0, []Record{{textField("field_name", "html"), textField("field_bytes", "hello")}})
	request := ctx.RawGetString("request").(*lua.LTable)
	request.RawSetString("v", input.RawGetString("v"))
	prepared, code := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, stage, input)
	if code != lua.LNil {
		t.Fatal(code)
	}
	r := sharedLuaNewRedis()
	r.now = f.r.nowMS
	for key, hash := range f.r.hashes {
		record := Record{}
		for field, value := range hash {
			record = append(record, textField(field, value))
		}
		r.setHash(key, record)
	}
	for key, values := range f.r.sets {
		members := []string{}
		for member, present := range values {
			if present {
				members = append(members, member)
			}
		}
		r.setSet(key, members)
	}
	for key, values := range f.r.zsets {
		members := map[string]float64{}
		for member, text := range values {
			score, err := strconv.ParseFloat(text, 64)
			if err != nil {
				t.Fatal(err)
			}
			members[member] = score
		}
		r.setZSet(key, members)
	}
	for key, values := range f.r.lists {
		r.setList(key, values)
	}
	for key, ttl := range f.r.ttls {
		entry := r.data[key]
		entry.expireAt = int64(r.now) + ttl
		r.data[key] = entry
	}
	redis := f.vm.state.NewTable()
	redis.RawSetString("call", f.vm.state.NewFunction(func(l *lua.LState) int { return r.command(l, false) }))
	f.vm.env.RawSetString("redis", redis)
	return f, r, ctx, stage, prepared.(*lua.LTable)
}

func sharedStagePlan(t *testing.T, f *stageLuaFixture, ctx *lua.LTable, stage lua.LValue, prepared *lua.LTable) lua.LValue {
	t.Helper()
	vm := f.vm
	plan, code := vm.invoke(t, "Plan", "new", ctx)
	if code != lua.LNil {
		t.Fatal(code)
	}
	if _, code = vm.invoke(t, "Plan", "set_policy", plan, lua.LString("stage"), stage); code != lua.LNil {
		t.Fatal(code)
	}
	add := func(argv []string) {
		t.Helper()
		if _, c := vm.invoke(t, "Plan", "add", plan, bootLuaStrings(vm.state, argv), lua.LString("ordinary")); c != lua.LNil {
			t.Fatal(c)
		}
	}
	add([]string{"HSET", f.stagePrefix + "page", "html", "hello"})
	meta := stageLuaRecord(prepared.RawGetString("next_meta").(*lua.LTable))
	write := []string{"HSET", f.stagePrefix + "meta"}
	for _, field := range meta {
		write = append(write, field.Name, string(field.Value))
	}
	add(write)
	add([]string{"LPUSH", f.stagePrefix + "keys", f.stagePrefix + "page"})
	add([]string{"PEXPIREAT", f.stagePrefix + "page", "900500"})
	return plan
}

func TestSharedLuaRev4SelfInclusiveStageDebitAndWidthGaps(t *testing.T) {
	t.Parallel()
	f, r, ctx, stage, prepared := sharedStageMemoryFixture(t, 50000000)
	plan := sharedStagePlan(t, f, ctx, stage, prepared)
	before := r.snapshot()
	a, code := f.vm.invoke(t, "Plan", "assess", ctx, plan)
	if code != lua.LNil {
		t.Fatal(code)
	}
	assessment := a.(*lua.LTable)
	growth := uint64(assessment.RawGetString("growth").(lua.LNumber))
	remaining := uint64(assessment.RawGetString("remaining").(lua.LNumber))
	if remaining != 50000000-growth || !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("debit not self inclusive or assessment wrote")
	}
	// For this fixed descriptor sequence, changed-slot G is K+3*width. At a
	// decimal threshold, the three omitted integer roots must fail closed.
	k := growth - 24
	for _, width := range []int{6, 7, 8} {
		threshold := uint64(1)
		for i := 1; i < width; i++ {
			threshold *= 10
		}
		for _, delta := range []int64{-1, 0, 1, 2, 3} {
			t.Run(fmt.Sprintf("width%d/%d", width, delta), func(t *testing.T) {
				old := uint64(int64(threshold+k+uint64(3*(width-1))) + delta)
				f, r, ctx, handle, p := sharedStageMemoryFixture(t, old)
				plan := sharedStagePlan(t, f, ctx, handle, p)
				before := r.snapshot()
				a, code := f.vm.invoke(t, "Plan", "assess", ctx, plan)
				if delta >= 0 && delta <= 2 {
					if a != lua.LNil || code != lua.LString("INVALID_STATE") {
						t.Fatalf("width-gap got %v / %v", a, code)
					}
				} else {
					if code != lua.LNil {
						t.Fatal(code)
					}
					v := a.(*lua.LTable)
					g := uint64(v.RawGetString("growth").(lua.LNumber))
					left := uint64(v.RawGetString("remaining").(lua.LNumber))
					if old-left != g || g != k+uint64(3*len(strconv.FormatUint(left, 10))) {
						t.Fatal("old-width overcharge or incorrect fixed point")
					}
				}
				if !reflect.DeepEqual(before, r.snapshot()) {
					t.Fatal("solver mutated datastore")
				}
			})
		}
	}
	// The protocol-bounded cumulative shape remains eight digits; this arithmetic
	// proves why the abstract gap must still reject corrupt/synthetic low states.
	const maximumPayload = 12191296
	bound := uint64(3*(maximumPayload+65536) + 1024*75 + 256*2000)
	if bound != 37359296 || 50331648-bound-65536 != 12906816 {
		t.Fatal("approved-shape arithmetic drift")
	}
}

func sharedStageUseRedis(t *testing.T, f *stageLuaFixture) *sharedLuaRedis {
	t.Helper()
	r := sharedLuaNewRedis()
	r.now = f.r.nowMS
	for key, hash := range f.r.hashes {
		record := Record{}
		for field, value := range hash {
			record = append(record, textField(field, value))
		}
		r.setHash(key, record)
	}
	for key, values := range f.r.sets {
		a := []string{}
		for value, present := range values {
			if present {
				a = append(a, value)
			}
		}
		r.setSet(key, a)
	}
	for key, values := range f.r.zsets {
		a := map[string]float64{}
		for member, text := range values {
			n, e := strconv.ParseFloat(text, 64)
			if e != nil {
				t.Fatal(e)
			}
			a[member] = n
		}
		r.setZSet(key, a)
	}
	for key, values := range f.r.lists {
		r.setList(key, values)
	}
	for key, ttl := range f.r.ttls {
		if entry, ok := r.data[key]; ok {
			entry.expireAt = int64(r.now) + ttl
			r.data[key] = entry
		}
	}
	redis := f.vm.state.NewTable()
	redis.RawSetString("call", f.vm.state.NewFunction(func(l *lua.LState) int { return r.command(l, false) }))
	redis.RawSetString("acl_check_cmd", f.vm.state.NewFunction(func(l *lua.LState) int { return r.command(l, true) }))
	f.vm.env.RawSetString("redis", redis)
	return r
}

func sharedStageReplyRegistration(t *testing.T, f *stageLuaFixture, operation string) {
	t.Helper()
	vm := f.vm
	vm.cj.RawSetString("Reply", vm.cj.RawGetString("Context").(*lua.LTable).RawGetString("Reply"))
	// Stage's owner now registers its real reply codecs before Context.open.
	// Do not replace them with a test validator or modify the owned module.
}

func sharedStageExecute(t *testing.T, f *stageLuaFixture, ctx, assessment *lua.LTable, plan lua.LValue) lua.LValue {
	t.Helper()
	vm := f.vm
	remaining := strconv.FormatUint(uint64(assessment.RawGetString("remaining").(lua.LNumber)), 10)
	reply, code := vm.invoke(t, "Reply", "build", ctx, lua.LString("STAGE_BEGUN"), bootLuaStrings(vm.state, []string{string(f.commit), "900500", remaining}))
	if code != lua.LNil {
		t.Fatal(code)
	}
	execution, code := vm.invoke(t, "Plan", "seal", ctx, plan, assessment, reply)
	if code != lua.LNil {
		t.Fatal(code)
	}
	fn, err := vm.state.Load(strings.NewReader(`return function(e) for i=1,e.count do redis.call(unpack(e.calls[i].argv,1,e.calls[i].argc)) end;return e.reply end`), "sealed executor")
	if err != nil {
		t.Fatal(err)
	}
	vm.state.SetFEnv(fn, vm.env)
	if err := vm.state.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}); err != nil {
		t.Fatal(err)
	}
	executor := vm.state.Get(-1)
	vm.state.Pop(1)
	if err := vm.state.CallByParam(lua.P{Fn: executor, NRet: 1, Protect: true}, execution); err != nil {
		t.Fatal(err)
	}
	vm.state.Pop(1)
	return execution
}

func TestSharedLuaRev4BeginPolicyAllocatesAndDebitsActualDescriptors(t *testing.T) {
	t.Parallel()
	f := stageLuaFixtureNew(t)
	vm := f.vm
	vm.load(t, "Memory", "memory")
	sharedStageReplyRegistration(t, f, "CJ2_BEGIN_STAGE")
	for _, index := range []int{jobActiveStageCommitIDIndex, jobLastStageCommitIDIndex} {
		f.job[index].Value = []byte("")
	}
	f.job[jobLastStageFenceIndex].Value = []byte("0")
	f.r.hash(f.jobKey, f.job)
	for _, key := range f.stageKeys {
		delete(f.r.hashes, key)
		delete(f.r.sets, key)
		delete(f.r.zsets, key)
		delete(f.r.lists, key)
		delete(f.r.ttls, key)
	}
	delete(f.r.hashes, StageSlotsKey)
	delete(f.r.zsets, StageExpiryKey)
	ctx, _, _ := f.open(t, "CJ2_BEGIN_STAGE", "")
	v := f.leaseValue()
	for _, field := range f.meta {
		switch field.Name {
		case "commit_id", "publication_id", "output_digest", "request_starts_baseline", "request_starts_generation", "expected_page_fields", "expected_outlinks", "expected_discoveries", "expected_aliases", "expected_images":
			v.RawSetString(field.Name, lua.LString(field.Value))
		}
	}
	ctx.RawGetString("request").(*lua.LTable).RawSetString("v", v)
	r := sharedStageUseRedis(t, f)
	plan, code := vm.invoke(t, "Plan", "new", ctx)
	if code != lua.LNil {
		t.Fatal(code)
	}
	if _, code = vm.invoke(t, "Plan", "set_policy", plan, lua.LString("begin")); code != lua.LNil {
		t.Fatal(code)
	}
	checked, code := vm.invoke(t, "Context", "checked_job", ctx, lua.LString(f.lease.JobID))
	if code != lua.LNil {
		t.Fatal(code)
	}
	pair := checked.(*lua.LTable)
	begin, code := vm.invoke(t, "Stage", "begin_record", ctx, pair.RawGetString("run"), pair.RawGetString("job"), f.leaseValue(), ctx.RawGetString("request"))
	if code != lua.LNil {
		t.Fatal(code)
	}
	meta := stageLuaRecord(begin.(*lua.LTable).RawGetString("meta").(*lua.LTable))
	write := []string{"HSET", f.stagePrefix + "meta"}
	for _, field := range meta {
		write = append(write, field.Name, string(field.Value))
	}
	calls := [][]string{write, {"LPUSH", f.stagePrefix + "keys", f.stagePrefix + "keys", f.stagePrefix + "meta"}, {"PEXPIREAT", f.stagePrefix + "meta", "900500"}, {"PEXPIREAT", f.stagePrefix + "keys", "900500"}, {"ZADD", StageExpiryKey, "900500", string(f.commit)}, {"HSET", f.jobKey, "active_stage_commit_id", string(f.commit), "last_stage_commit_id", string(f.commit), "last_stage_fence", "1"}}
	for _, argv := range calls {
		if _, code = vm.invoke(t, "Plan", "add", plan, bootLuaStrings(vm.state, argv), lua.LString("ordinary")); code != lua.LNil {
			t.Fatal(code)
		}
	}
	a, code := vm.invoke(t, "Plan", "assess", ctx, plan)
	if code != lua.LNil {
		t.Fatal(code)
	}
	assessment := a.(*lua.LTable)
	g := uint64(assessment.RawGetString("growth").(lua.LNumber))
	remaining := uint64(assessment.RawGetString("remaining").(lua.LNumber))
	if g+remaining != 50331648 || remaining < 65536 || assessment.RawGetString("admission") != lua.LString("begin") {
		t.Fatal("BEGIN admission or self-inclusive debit")
	}
	sharedStageExecute(t, f, ctx, assessment, plan)
	if r.writes != 7 || r.data[StageSlotsKey].hash[string(f.commit)] != fmt.Sprintf("%d:%s:%s:1:0", remaining, string(f.lease.RunID), string(f.lease.JobID)) || r.data[f.stagePrefix+"meta"].expireAt != 900500 {
		t.Fatal("BEGIN descriptors did not execute exact post-state")
	}
	logical := 0
	elements := 0
	newKeys := 0
	for _, call := range r.trace {
		if call.acl {
			continue
		}
		switch call.name {
		case "HSET":
			key := call.args[0]
			if key != f.jobKey {
				logical += len(key)
				newKeys++
			}
			for i := 1; i < len(call.args); i += 2 {
				if key != f.jobKey {
					logical += len(call.args[i])
					elements++
				}
				logical += len(call.args[i+1])
			}
		case "LPUSH":
			logical += len(call.args[0])
			newKeys++
			for _, value := range call.args[1:] {
				logical += len(value)
				elements++
			}
		case "ZADD":
			logical += len(call.args[0]) + len(call.args[1]) + len(call.args[2])
			newKeys++
			elements++
		}
	}
	// last_stage_fence's old "0" is replaced; every planned job field was empty
	// or changed. The independent logical calculation includes the solved slot.
	if g != uint64(3*logical+1024*newKeys+256*elements) {
		t.Fatal("BEGIN G is not actual final descriptor growth")
	}
}

func TestSharedLuaRev4StagePolicyRejectsForgedOwnerAndOutsideEffects(t *testing.T) {
	t.Parallel()
	f, _, ctx, handle, p := sharedStageMemoryFixture(t, 50000000)
	vm := f.vm
	plan, code := vm.invoke(t, "Plan", "new", ctx)
	if code != lua.LNil {
		t.Fatal(code)
	}
	fake := vm.state.NewTable()
	fake.RawSetString("remaining", lua.LNumber(50331648))
	if v, c := vm.invoke(t, "Plan", "set_policy", plan, lua.LString("stage"), fake); v != lua.LNil || c == lua.LNil {
		t.Fatal("caller invented slot proof")
	}
	handle.(*lua.LTable).RawSetString("remaining", lua.LNumber(1))
	if _, code = vm.invoke(t, "Plan", "set_policy", plan, lua.LString("stage"), handle); code != lua.LNil {
		t.Fatal("public diagnostics contaminated private stage proof")
	}
	if v, c := vm.invoke(t, "Plan", "add", plan, bootLuaStrings(vm.state, []string{"HSET", StageSlotsKey, string(f.commit), "50331648:forged"}), lua.LString("ordinary")); v != lua.LNil || c != lua.LString("INVALID_ARGUMENT") {
		t.Fatal("caller added free slot mutation")
	}
	if _, code = vm.invoke(t, "Plan", "add", plan, bootLuaStrings(vm.state, []string{"HSET", f.base, "last_activity_at_ms", "500"}), lua.LString("ordinary")); code != lua.LNil {
		t.Fatal(code)
	}
	if v, c := vm.invoke(t, "Plan", "assess", ctx, plan); v != lua.LNil || c != lua.LString("INVALID_ARGUMENT") {
		t.Fatal("stage budget paid an unrelated run mutation")
	}
	_ = p
}

func TestSharedLuaRev4DeletionOnlyIsNotFreeAllocationCoverage(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	r := sharedLuaNewRedis()
	r.maximum = 1
	r.setSet(ContractsCandidateKey, []string{"a"})
	source := sharedCollectionPlan(t, `assert(CJ.Read.members(ctx,k,"set",{"a"},1,1))`, `add({"SREM",k,"a"})`, true)
	sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	for _, call := range r.trace {
		if call.name == "INFO" && call.args[0] == "MEMORY" {
			t.Fatal("deletion-only drain depended on allocation headroom")
		}
	}
	if _, ok := r.data[ContractsCandidateKey]; ok {
		t.Fatal("deletion did not remove last-member key")
	}
	r = sharedLuaNewRedis()
	r.maximum = 1
	r.setSet(ContractsCandidateKey, []string{"a"})
	source = sharedCollectionPlan(t, `assert(CJ.Read.members(ctx,k,"set",{"a"},1,1))`, `add({"SREM",k,"a"});add({"SADD",k,"a"})`, true)
	sharedLuaRejectSource(t, r, source, keys, args, ErrorMemoryHeadroomLow)
}

func TestSharedLuaRev4BlobFramingIsBinaryButStageValidatorOwnsText(t *testing.T) {
	t.Parallel()
	fixture := newWireOracleFixture(t)
	op := OperationStagePageBlob
	request, err := wireOracleConstructRequest(fixture, op, wireOracleActive, wireOracleGate(t, fixture, op, wireOracleActive))
	if err != nil {
		t.Fatal(err)
	}
	k, a, _, err := request.validatedWireParts()
	if err != nil {
		t.Fatal(err)
	}
	keys, args := []string{}, []string{}
	for _, key := range k {
		keys = append(keys, string(key))
	}
	for _, arg := range a {
		args = append(args, string(arg))
	}
	args[len(args)-1] = string(primitiveLuaEncoded(t, Record{textField("field_name", args[13]), {Name: "field_bytes", Value: []byte{0xff}}}))
	source := sharedRuntimeCore(t, false) + `local ctx,c=CJ.Context.open(assert(CJ.Wire.stage_spec("CJ2_STAGE_PAGE_BLOB")),KEYS,ARGV);if not ctx then return CJ.Context.reject(c) end;return {ctx.request.records[1].v.field_bytes}`
	if got := sharedLuaNoError(t, sharedLuaRun(t, sharedLuaNewRedis(), source, keys, args)); !reflect.DeepEqual(got, []any{string([]byte{0xff})}) {
		t.Fatal("binary blob framing was incorrectly defaulted to a text codec")
	}
	f := stageLuaFixtureNew(t)
	ctx, view, job := f.open(t, "CJ2_STAGE_PAGE_BLOB", "")
	handle, code := f.check(t, view, job, "check_owned")
	if code != lua.LNil {
		t.Fatal(code)
	}
	input := f.input(t, ChunkHTML, 0, []Record{{textField("field_name", "html"), textField("field_bytes", "valid")}})
	input.RawGetString("records").(*lua.LTable).RawSetInt(1, lua.LString(primitiveLuaEncoded(t, Record{textField("field_name", "html"), {Name: "field_bytes", Value: []byte{0xff}}})))
	if v, c := f.vm.invoke(t, "Stage", "validate_chunk", ctx, f.run, job, handle, input); v != lua.LNil || c == lua.LNil {
		t.Fatal("actual Stage validator accepted invalid UTF-8 bytes")
	}
}

func TestSharedLuaRev4WorkerPeerEvidenceIsReadOnlyAndBounded(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 2, 0)
	peer := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, peer, StatusClaimed)
	request := workerLuaRequest(t, f, OperationTryClaim, workerLuaIntent(t, f, 1, 1, RequestDocument))
	k, a, _, err := request.validatedWireParts()
	if err != nil {
		t.Fatal(err)
	}
	keys, args := []string{}, []string{}
	for _, key := range k {
		keys = append(keys, string(key))
	}
	for _, arg := range a {
		args = append(args, string(arg))
	}
	id, err := DeriveReservationID(f.policy, peer)
	if err != nil {
		t.Fatal(err)
	}
	scope := string(peer.Decision.GlobalScopeID)
	qkey := requestLuaPrefix + "reservation:" + string(id)
	jobKey := requestLuaPrefix + "run:" + string(peer.Lease.RunID) + ":job:" + string(peer.Lease.JobID)
	source := sharedRuntimeCore(t, true) + `
local ctx=assert(CJ.Context.open(assert(CJ.Wire.worker_spec("CJ2_TRY_CLAIM")),KEYS,ARGV))
local scope="` + scope + `";local keys=assert(CJ.Wire.rate_keys(scope))
for _,suffix in ipairs({"active","pending","started"}) do assert(CJ.Read.all_members(ctx,keys[suffix],"zset",32,64)) end
local ids=assert(CJ.Context.bind_scope_reservations(ctx,scope));assert(#ids==1 and ids[1]=="` + string(id) + `")
local q="` + qkey + `";local job="` + jobKey + `"
assert(CJ.Context.can_read(ctx,q) and CJ.Context.can_read(ctx,job))
assert(not CJ.Context.can_write(ctx,q) and not CJ.Context.can_write(ctx,job))
local p=assert(CJ.Plan.new(ctx));ctx.allowed[q]=true;ctx.allowed[job]=true
for _,key in ipairs({q,job}) do local v,c=CJ.Plan.add(p,{"UNLINK",key},"ordinary");assert(v==nil and c=="INVALID_ARGUMENT") end
local unrelated=string.rep("e",64);local v,c=CJ.Context.bind_scope_reservations(ctx,unrelated);assert(v==nil and c=="INVALID_ARGUMENT")
assert(not CJ.Context.can_read(ctx,"mifolyo:crawl:v2:rate:"..unrelated))
return {"read-only-peer"}
`
	before := f.r.snapshot()
	got := sharedLuaNoError(t, sharedLuaRun(t, f.r, source, keys, args))
	if !reflect.DeepEqual(got, []any{"read-only-peer"}) || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("peer evidence changed datastore")
	}
	// The grant cannot be requested from cardinalities/publicly forged views.
	source = sharedRuntimeCore(t, true) + `
local ctx=assert(CJ.Context.open(assert(CJ.Wire.worker_spec("CJ2_TRY_CLAIM")),KEYS,ARGV))
local keys=assert(CJ.Wire.rate_keys("` + scope + `"))
for _,suffix in ipairs({"active","pending","started"}) do local f=assert(CJ.Read.cardinality(ctx,keys[suffix],"zset",32));f.complete=true;f.members={["` + string(id) + `"]=true} end
local v,c=CJ.Context.bind_scope_reservations(ctx,"` + scope + `");if not v then return CJ.Context.reject(c) end;return {"unexpected"}
`
	sharedLuaRejectSource(t, f.r, source, keys, args, ErrorInvalidState)
	// A privately selected ID with a broken pending/active relation is corrupt,
	// not a new read grant, and is never pruned as expired capacity.
	f.r.removeKey(requestLuaPrefix + "rate:" + scope + ":pending")
	source = sharedRuntimeCore(t, true) + `
local ctx=assert(CJ.Context.open(assert(CJ.Wire.worker_spec("CJ2_TRY_CLAIM")),KEYS,ARGV));local keys=assert(CJ.Wire.rate_keys("` + scope + `"))
for _,suffix in ipairs({"active","pending","started"}) do assert(CJ.Read.all_members(ctx,keys[suffix],"zset",32,64)) end
local v,c=CJ.Context.bind_scope_reservations(ctx,"` + scope + `");if not v then return CJ.Context.reject(c) end;return {"unexpected"}
`
	sharedLuaRejectSource(t, f.r, source, keys, args, ErrorRateStateCorrupt)
}

func sharedRecoveryCorePlan(t *testing.T, f *recordsLuaFixture, aggregation string) (string, []string, []string) {
	t.Helper()
	if aggregation != "direct" && aggregation != "overshoot" && aggregation != "run" {
		t.Fatalf("unknown recovery test aggregation %q", aggregation)
	}
	keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
	source := sharedRuntimeCore(t, true) + `
local ctx=assert(CJ.Context.open(assert(CJ.Wire.maintenance_spec("CJ2_RECOVER_EXPIRED")),KEYS,ARGV))
assert(CJ.Gate.check(ctx));local run=assert(CJ.Run.load(ctx,ctx.request.v.run_id))
local due=assert(CJ.Read.due(ctx,ctx.keys.run_leased,ctx.now_text,100,10000,64))
local jobs={}
for i,id in ipairs(due.ordered) do
 assert(CJ.Context.bind_job(ctx,id));jobs[i]=assert(CJ.Job.load(ctx,run,id))
 local stage=assert(CJ.Context.bind_stage(ctx,id))
 if stage.exists then
  assert(CJ.Read.fixed_hash(ctx,stage.keys.meta,"stage_meta"));assert(CJ.Read.members(ctx,ctx.keys.stage_expiry,"zset",{stage.commit_id},1280,64))
 end
end
local p=assert(CJ.Plan.new(ctx));assert(CJ.Plan.set_policy(p,"recovery"));local units={}
for i,job in ipairs(jobs) do units[i]=assert(CJ.Plan.recovery_unit(p,job.v.job_id));assert(CJ.Plan.validate_unit(p,units[i])) end
local other=assert(CJ.Plan.new(ctx));assert(CJ.Plan.set_policy(other,"recovery"))
local valid,ec=CJ.Plan.validate_unit(other,units[1]);assert(valid==nil and ec=="INVALID_ARGUMENT")
valid,ec=CJ.Plan.validate_unit(p,{});assert(valid==nil and ec=="INVALID_ARGUMENT")
local delta=` + strconv.FormatBool(aggregation == "run") + ` and assert(CJ.Run.plan_delta(ctx,run))
for i,job in ipairs(jobs) do
 local after={};for name,value in next,job.v,nil do after[name]=value end
 after.state="delayed";after.last_reason="lease_expired_after_io";after.last_failure_reason="lease_expired_after_io"
 after.retry_count=P.format_decimal(job.n.retry_count+1);after.not_before_ms=P.format_decimal(ctx.now_ms+30000)
 after.updated_at_ms=ctx.now_text;after.lease_owner="";after.lease_token="";after.lease_started_at_ms="0";after.lease_expires_at_ms="0";after.lease_delivery_started="0";after.active_stage_commit_id=""
 local checked=assert(CJ.Schemas.project("job",after));local argv={"HSET",job.key}
 for _,pair in ipairs(checked.fields) do if job.v[pair[1]]~=pair[2] then argv[#argv+1]=pair[1];argv[#argv+1]=pair[2] end end
 assert(CJ.Plan.add(p,argv,units[i]))
 assert(CJ.Plan.add(p,{"ZREM",ctx.keys.run_leased,job.v.job_id},units[i]));assert(CJ.Plan.add(p,{"ZREM",ctx.keys.run_leased_at,job.v.job_id},units[i]))
 assert(CJ.Plan.add(p,{"ZREM",ctx.keys.active_leases,ctx.request.v.run_id..":"..job.v.job_id},units[i]))
 assert(CJ.Plan.add(p,{"ZADD",ctx.keys.run_delayed,after.not_before_ms,job.v.job_id},units[i]))
 if job.v.active_stage_commit_id~="" then assert(CJ.Plan.add(p,{"HSET","mifolyo:crawl:v2:stage:"..job.v.active_stage_commit_id..":meta","abandoned","1"},units[i])) end
 if delta then
  assert(CJ.Run.accumulate(delta,{run={recovered_leases_total=1,retries_total=1},maps={recovery_outcome_counts={delayed=1},retry_reason_counts={lease_expired_after_io=1}},indexes={leased=-1,leased_at=-1,delayed=1}},units[i]))
  assert(CJ.Run.set(delta,{last_activity_at_ms=ctx.now_text,last_execution_at_ms=ctx.now_text},units[i]))
 end
end
local n=#jobs
if delta then
 local post=assert(CJ.Run.flush(ctx,p,delta))
 assert(post.n.recovered_leases_total==run.n.recovered_leases_total+n and post.n.retries_total==run.n.retries_total+n)
else
assert(CJ.Plan.add(p,{"HSET",ctx.keys.run,"recovered_leases_total",P.format_decimal(run.n.recovered_leases_total+n),"retries_total",P.format_decimal(run.n.retries_total+n),"last_activity_at_ms",ctx.now_text,"last_execution_at_ms",ctx.now_text},units[1]))
assert(CJ.Plan.add(p,{"HSET",ctx.keys.run_recovery_outcome_counts,"delayed",P.format_decimal(run.reasons.recovery_outcome_counts.n.delayed+n)},units[1]))
assert(CJ.Plan.add(p,{"HSET",ctx.keys.run_retry_reason_counts,"lease_expired_after_io",P.format_decimal(run.reasons.retry_reason_counts.n.lease_expired_after_io+n)},units[1]))
end
`
	if aggregation == "overshoot" {
		source += `assert(CJ.Plan.add(p,{"HSET",ctx.keys.run,"recovered_leases_total",P.format_decimal(run.n.recovered_leases_total+n+1)},units[1]))`
	}
	source += `local a,c=CJ.Plan.assess(ctx,p);if not a then return CJ.Context.reject(c) end
local valid,code=CJ.Plan.validate_unit(p,units[1]);assert(valid==nil and code=="INVALID_STATE")
return {P.format_decimal(a.growth),P.format_decimal(a.covered_growth),P.format_decimal(a.uncovered_growth)}`
	return source, keys, args
}

func TestSharedLuaRev4RecoveryCoalescedFieldsUseActualContributors(t *testing.T) {
	t.Parallel()
	// Two genuine expired owned-stage receipts, with a coalesced final run/reason
	// field charged once to a real contributor. The test does not replace Run,
	// Job, Stage, SHA, descriptor simulation or the memory admission implementation.
	f := maintenanceLuaLeased(t, 2, "started")
	maintenanceLuaStage(t, f, 0)
	maintenanceLuaStage(t, f, 1)
	source, keys, args := sharedRecoveryCorePlan(t, f, "direct")
	before := f.r.snapshot()
	got := sharedLuaNoError(t, sharedLuaRun(t, f.r, source, keys, args)).([]any)
	g, _ := strconv.ParseUint(got[0].(string), 10, 64)
	covered, _ := strconv.ParseUint(got[1].(string), 10, 64)
	uncovered, _ := strconv.ParseUint(got[2].(string), 10, 64)
	if g == 0 || g != covered || uncovered != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("coalesced slotted fields were rejected/double charged/written")
	}
	source, keys, args = sharedRecoveryCorePlan(t, f, "overshoot")
	sharedLuaRejectSource(t, f.r, source, keys, args, ErrorCounterCorrupt)
}

func TestSharedLuaRev4RecoveryRunAccumulatorMatchesDescriptorGrowth(t *testing.T) {
	t.Parallel()
	// Compare the actual Run accumulator/flush with independently composed final
	// descriptors. This checks the shared interface, not Job.plan_outcome or the
	// maintenance handler, and never replaces either production implementation.
	f := maintenanceLuaLeased(t, 2, "started")
	maintenanceLuaStage(t, f, 0)
	maintenanceLuaStage(t, f, 1)
	before := f.r.snapshot()
	direct, keys, args := sharedRecoveryCorePlan(t, f, "direct")
	want := sharedLuaNoError(t, sharedLuaRun(t, f.r, direct, keys, args)).([]any)
	source, keys, args := sharedRecoveryCorePlan(t, f, "run")
	got := sharedLuaNoError(t, sharedLuaRun(t, f.r, source, keys, args))
	if !reflect.DeepEqual(got, want) || want[0] == "0" || want[0] != want[1] || want[2] != "0" {
		t.Fatalf("Run coalescing changed descriptor-derived coverage: got %v want %v", got, want)
	}
	if !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
		t.Fatal("Run/core preparation mutated Redis")
	}
	// Exact all-slot safety boundary: shared-field G belongs to its authenticated
	// contributor and must not be charged again as uncovered allocation.
	f.r.maximum = f.r.used + 2*32768 + 67108864 - 1
	sharedLuaRejectSource(t, f.r, source, keys, args, ErrorMemoryHeadroomLow)
	f.r.maximum++
	got = sharedLuaNoError(t, sharedLuaRun(t, f.r, source, keys, args))
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
		t.Fatal("Run/core coalesced recovery rejected exact headroom or wrote during assessment")
	}
}

func sharedPendingReleasePlan(t *testing.T, earlyClear bool) (*jobLuaOutcomeFixture, string, []string, []string) {
	t.Helper()
	f := jobLuaOutcomeFixtureNew(t, 0)
	jobLuaOutcomePending(t, f)
	request := jobLuaOutcomeWire(t, f.a, f.authority, f.jobs[0], f.lease, OperationReleaseBeforeIO, ReasonNone)
	keys, args := runLuaParts(t, request, nil)
	source := sharedRuntimeCore(t, true) + `
assert(CJ.Job.register_outcome("CJ2_RELEASE_BEFORE_IO"))
local ctx=assert(CJ.Context.open(assert(CJ.Wire.worker_spec("CJ2_RELEASE_BEFORE_IO")),KEYS,ARGV));assert(CJ.Gate.check(ctx))
local run=assert(CJ.Run.load(ctx,ctx.request.v.run_id));local job=assert(CJ.Job.load(ctx,run,ctx.request.v.job_id))
local held,hc=CJ.Context.bind_held_request(ctx,job.v.job_id);if not held then return CJ.Context.reject(hc) end;assert(held.exists and not CJ.Context.can_write(ctx,held.key))
local q=assert(CJ.Read.fixed_hash(ctx,held.key,"reservation"));assert(CJ.Read.ttl(ctx,held.key));local scopes=assert(CJ.Context.bind_held_scopes(ctx,held.reservation_id))
assert(CJ.Context.can_write(ctx,held.key))
for _,kind in ipairs({"global","group","origin"}) do local k=scopes[kind]
 assert(CJ.Read.fixed_hash(ctx,k.scope,"rate_scope"));assert(CJ.Read.ttl(ctx,k.scope))
 assert(CJ.Read.members(ctx,ctx.keys.rate_scopes,"zset",{q.v[kind.."_scope_id"]},100000,64));assert(CJ.Read.ttl(ctx,ctx.keys.rate_scopes))
 for _,name in ipairs({"active","pending","started"}) do assert(CJ.Read.members(ctx,k[name],"zset",{held.reservation_id},32,64));assert(CJ.Read.ttl(ctx,k[name])) end
end
assert(CJ.Request.check_live({ctx=ctx},run,job,held.reservation_id))
local p=assert(CJ.Plan.new(ctx));assert(CJ.Plan.set_policy(p,"safety"))
local function add(a)assert(CJ.Plan.add(p,a,"ordinary"))end
local post=assert(CJ.Job.outcome_record(ctx,job,{state="ready",last_reason="none",last_transition_id=ctx.request.v.transition_id,last_transition_status="RELEASED_READY"}))
local job_write={"HSET",job.key};for _,pair in ipairs(post.fields) do if pair[2]~=job.v[pair[1]] then job_write[#job_write+1]=pair[1];job_write[#job_write+1]=pair[2] end end
`
	if earlyClear {
		source += `add(job_write)`
	}
	source += `
add({"HSET",held.key,"state","cancelled","terminal_at_ms",ctx.now_text});add({"PEXPIREAT",held.key,P.format_decimal(ctx.now_ms+86400000)})
for _,kind in ipairs({"global","group","origin"}) do local k=scopes[kind];local before=assert(CJ.Read.snapshot(ctx,k.scope))
 add({"HSET",k.scope,"active_count",P.format_decimal(P.parse_decimal(before.v.active_count)-1),"pending_count",P.format_decimal(P.parse_decimal(before.v.pending_count)-1),"updated_at_ms",ctx.now_text})
 add({"ZREM",k.active,held.reservation_id});add({"ZREM",k.pending,held.reservation_id})
 add({"ZADD",ctx.keys.rate_scopes,ctx.now_text,q.v[kind.."_scope_id"]})
end
`
	if !earlyClear {
		source += `add(job_write)`
	}
	source += `
add({"ZREM",ctx.keys.run_leased,job.v.job_id});add({"ZREM",ctx.keys.run_leased_at,job.v.job_id});add({"ZREM",ctx.keys.active_leases,ctx.request.v.run_id..":"..job.v.job_id})
add({"ZADD",ctx.keys.run_ready,job.v.score_text,job.v.job_id});add({"ZADD",ctx.keys.run_ready_at,ctx.now_text,job.v.job_id})
add({"HSET",ctx.keys.run,"pending_request_reservations",P.format_decimal(run.n.pending_request_reservations-1),"last_activity_at_ms",ctx.now_text})
add({"HSET",ctx.keys.run_group_pending,q.v.group_id,P.format_decimal(run.maps.group_pending.n[q.v.group_id]-1)})
local a,c=CJ.Plan.assess(ctx,p);if not a then return CJ.Context.reject(c) end
local reply=assert(CJ.Reply.build(ctx,"RELEASED_READY",{ctx.now_text}));local e=assert(CJ.Plan.seal(ctx,p,a,reply))
for i=1,e.count do redis.call(unpack(e.calls[i].argv,1,e.calls[i].argc)) end;return e.reply
`
	return f, source, keys, args
}

func TestSharedLuaRev4PendingReleaseProofAndOrdering(t *testing.T) {
	t.Parallel()
	f, source, keys, args := sharedPendingReleasePlan(t, false)
	got := sharedLuaNoError(t, sharedLuaRun(t, f.r, source, keys, args))
	if err := ValidateOperationResponse(OperationReleaseBeforeIO, got); err != nil {
		t.Fatal(err)
	}
	job := f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash
	if job["state"] != "ready" || job["lease_owner"] != "" || job["active_reservation_id"] != "" {
		t.Fatal("pending release poststate")
	}
	f, source, keys, args = sharedPendingReleasePlan(t, true)
	sharedLuaRejectSource(t, f.r, source, keys, args, ErrorReservationCorrupt)
	f, source, keys, args = sharedPendingReleasePlan(t, false)
	args[9] = strings.Repeat("a", 32) // source wire owner, not a mutable public ctx flag
	sharedLuaRejectSource(t, f.r, source, keys, args, ErrorImmutableMismatch)
}

func sharedRenewalGrantSource(t *testing.T, body string) string {
	t.Helper()
	return sharedRuntimeCore(t, true) + `
local ctx=assert(CJ.Context.open(assert(CJ.Wire.worker_spec("CJ2_RENEW_LEASE")),KEYS,ARGV))
assert(CJ.Gate.check(ctx));local run=assert(CJ.Run.load(ctx,ctx.request.v.run_id))
local job=assert(CJ.Job.load(ctx,run,ctx.request.v.job_id))
` + body
}

func TestSharedLuaRev4RenewalHeldGrantsSeparateInspectionFromWrites(t *testing.T) {
	t.Parallel()
	for _, started := range []bool{false, true} {
		for _, caller := range []string{"live", "owner", "token", "fence", "expired", "expired_stale"} {
			t.Run(fmt.Sprintf("started=%t/%s", started, caller), func(t *testing.T) {
				f := workerLuaFixtureNew(t, 1, 0)
				i := workerLuaIntent(t, f, 0, 1, RequestDocument)
				workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
				if started {
					workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
				}
				request := i
				switch caller {
				case "owner":
					request.Lease.OwnerID = OwnerID(strings.Repeat("e", 32))
				case "token", "expired_stale":
					request.Lease.Token = LeaseToken(strings.Repeat("f", 64))
				case "fence":
					request.Lease.Fence++
				}
				if strings.HasPrefix(caller, "expired") {
					f.r.now += 60000 // equality with the stored deadline is expired
				}
				keys, args := runLuaParts(t, workerLuaRequest(t, f, OperationRenewLease, request), nil)
				source := sharedRenewalGrantSource(t, `
local expected_write=`+strconv.FormatBool(caller == "live")+`
local id=job.v.active_reservation_id;local key="mifolyo:crawl:v2:reservation:"..id
assert(not CJ.Context.can_read(ctx,key) and not CJ.Context.can_write(ctx,key))
local held=assert(CJ.Context.bind_held_request(ctx,job.v.job_id))
assert(held.exists and held.reservation_id==id and held.key==key)
assert(CJ.Context.can_read(ctx,key) and not CJ.Context.can_write(ctx,key))
local q=assert(CJ.Read.fixed_hash(ctx,key,"reservation"));assert(CJ.Read.ttl(ctx,key))
local scopes=assert(CJ.Context.bind_held_scopes(ctx,id))
local plan=assert(CJ.Plan.new(ctx))
local function permissions()
 assert(CJ.Context.can_read(ctx,key) and CJ.Context.can_write(ctx,key)==expected_write)
 for _,kind in ipairs({"global","group","origin"}) do
  assert(scopes[kind].scope=="mifolyo:crawl:v2:rate:"..q.v[kind.."_scope_id"])
  for _,k in ipairs(scopes[kind].ordered) do
   assert(CJ.Context.can_read(ctx,k) and CJ.Context.can_write(ctx,k)==expected_write)
   if not expected_write then
    ctx.allowed[k]=true;local ok,code=CJ.Plan.add(plan,{"UNLINK",k},"ordinary")
    assert(ok==nil and code=="INVALID_ARGUMENT" and not CJ.Context.can_write(ctx,k))
   end
  end
 end
 if not expected_write then
  ctx.allowed[key]=true;local ok,code=CJ.Plan.add(plan,{"UNLINK",key},"ordinary")
  assert(ok==nil and code=="INVALID_ARGUMENT" and not CJ.Context.can_write(ctx,key))
 end
end
permissions()
-- Full real Request inspection still runs for a stale or expired caller.
local live=assert(CJ.Request.load_live(ctx,run,job))
assert(live.v.reservation_id==id and live.v.owner_id==job.v.lease_owner and live.v.lease_token==job.v.lease_token)
assert(live.logical_expired==`+strconv.FormatBool(strings.HasPrefix(caller, "expired"))+`)
if not expected_write then
 -- Public credentials, operation and clock cannot launder the original caller
 -- into a live holder or recovery writer. The private TIME remains authoritative.
 ctx.request.v.owner_id=job.v.lease_owner;ctx.request.v.lease_token=job.v.lease_token;ctx.request.v.fence=job.v.lease_fence
 ctx.operation="CJ2_RECOVER_EXPIRED"
 if live.logical_expired then ctx.now_ms=job.n.lease_expires_at_ms-1;ctx.now_text=P.format_decimal(ctx.now_ms) end
 held=assert(CJ.Context.bind_held_request(ctx,job.v.job_id))
 scopes=assert(CJ.Context.bind_held_scopes(ctx,id));permissions()
end
local unrelated=string.rep("e",64);local other="mifolyo:crawl:v2:reservation:"..unrelated
ctx.allowed[other]=true
local ok,code=CJ.Context.bind_held_scopes(ctx,unrelated);assert(ok==nil and code=="INVALID_ARGUMENT")
ok,code=CJ.Context.bind_scope_reservations(ctx,unrelated);assert(ok==nil and code=="INVALID_ARGUMENT")
assert(not CJ.Context.can_read(ctx,other) and not CJ.Context.can_write(ctx,other))
-- Do not install a blanket receipt-only lock: the worker owns the subsequent
-- fully inspected, ordinary-admission run rejection-counter transition.
assert(CJ.Context.can_write(ctx,ctx.keys.run))
return {expected_write and "live-writer" or "inspection-only"}
`)
				before := f.r.snapshot()
				got := sharedLuaNoError(t, sharedLuaRun(t, f.r, source, keys, args))
				want := "inspection-only"
				if caller == "live" {
					want = "live-writer"
				}
				if !reflect.DeepEqual(got, []any{want}) || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 || f.r.aclCount != 0 {
					t.Fatal("renewal grant inspection wrote or changed its permission class")
				}
			})
		}
	}
}

func TestSharedLuaRev4RenewalHeldGrantRequiresPrivateCurrentJob(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 2, 0)
	i, peer := workerLuaIntent(t, f, 0, 1, RequestDocument), workerLuaIntent(t, f, 1, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	workerLuaReply(t, f, OperationTryClaim, peer, StatusClaimed)
	i.Lease.Token = LeaseToken(strings.Repeat("f", 64))
	keys, args := runLuaParts(t, workerLuaRequest(t, f, OperationRenewLease, i), nil)
	qkey := requestLuaPrefix + "reservation:" + f.r.data[wireOracleRunJobKey(i.Lease.RunID, i.Lease.JobID)].hash["active_reservation_id"]
	// A complete typed Job hash plus public forged membership is not Job.check's
	// private current-lease/index proof. It must not even grant the pointer read.
	source := sharedRuntimeCore(t, true) + `
local ctx=assert(CJ.Context.open(assert(CJ.Wire.worker_spec("CJ2_RENEW_LEASE")),KEYS,ARGV))
assert(CJ.Gate.check(ctx));assert(CJ.Run.load(ctx,ctx.request.v.run_id))
local job=assert(CJ.Read.fixed_hash(ctx,ctx.keys.job,"job"))
local selected=assert(CJ.Read.cardinality(ctx,ctx.keys.run_leased,"zset",64))
selected.complete=true;selected.members={[job.v.job_id]=true};selected.scores={[job.v.job_id]=job.n.lease_expires_at_ms}
ctx.selected[ctx.keys.run_leased]=selected
local held,code=CJ.Context.bind_held_request(ctx,job.v.job_id)
assert(held==nil and code=="INVALID_STATE" and not CJ.Context.can_read(ctx,"` + qkey + `"))
return {"private-receipts-required"}
`
	before := f.r.snapshot()
	got := sharedLuaNoError(t, sharedLuaRun(t, f.r, source, keys, args))
	if !reflect.DeepEqual(got, []any{"private-receipts-required"}) || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
		t.Fatal("partial/public job facts granted held access")
	}
	// Full peer evidence may be selected through the two-member global scope,
	// but never becomes a held request for this wire job, even after public edits.
	source = sharedRenewalGrantSource(t, `
assert(CJ.Request.load_live(ctx,run,job))
local peer="`+string(peer.Lease.JobID)+`";local key="mifolyo:crawl:v2:run:"..run.run_id..":job:"..peer
assert(CJ.Context.can_read(ctx,key) and not CJ.Context.can_write(ctx,key))
local record=assert(CJ.Read.snapshot(ctx,key));local id=record.v.active_reservation_id
assert(CJ.Context.can_read(ctx,"mifolyo:crawl:v2:reservation:"..id))
local own_id,own_request=job.v.job_id,job.v.active_reservation_id
ctx.request.v.job_id=peer;ctx.request.v.owner_id=record.v.lease_owner;ctx.request.v.lease_token=record.v.lease_token;ctx.request.v.fence=record.v.lease_fence
job.v.active_reservation_id=id;ctx.allowed[key]=true
local own=assert(CJ.Context.bind_held_request(ctx,own_id));assert(own.reservation_id==own_request and own.reservation_id~=id)
local held,code=CJ.Context.bind_held_request(ctx,peer);assert(held==nil and code=="IMMUTABLE_MISMATCH")
local scopes,err=CJ.Context.bind_held_scopes(ctx,id);assert(scopes==nil and err=="INVALID_ARGUMENT")
assert(not CJ.Context.can_write(ctx,key) and not CJ.Context.can_write(ctx,"mifolyo:crawl:v2:reservation:"..id))
return {"peer-remains-read-only"}
`)
	got = sharedLuaNoError(t, sharedLuaRun(t, f.r, source, keys, args))
	if !reflect.DeepEqual(got, []any{"peer-remains-read-only"}) || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
		t.Fatal("peer/public pointer broadened the renewal held grant")
	}
}

func TestSharedLuaRev4RenewalGrantsHeldScopesNotSourceScopes(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	workerLuaAddChargedGroup(t, f)
	// Pin a genuinely different charged lineage BEFORE requests, while keeping
	// the job's immutable source group unchanged. No production helper is replaced.
	group := &f.groups[1]
	group.RateScopeID = RateScopeID(strings.Repeat("3", 32))
	var err error
	group.GroupScopeID, err = DeriveGroupScopeID(group.RateScopeID)
	if err != nil {
		t.Fatal(err)
	}
	base := requestLuaPrefix + "run:" + string(f.runID)
	f.r.data[base+":group_rate_scope_ids"].hash[string(group.GroupID)] = string(group.RateScopeID)
	f.r.data[base+":group_scope_ids"].hash[string(group.GroupID)] = string(group.GroupScopeID)
	digest, err := DerivePolicyGroupMapDigest(f.groups)
	if err != nil {
		t.Fatal(err)
	}
	f.r.data[base].hash["policy_group_map_sha256"] = string(digest)
	f.policy, err = (transportAuthority{seal: &redisTransportAuthoritySeal}).parseRunPolicyAuthority(f.runID, workerLuaHashRecord(t, f.r, base, SchemaRun), f.groups)
	if err != nil {
		t.Fatal(err)
	}
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
	workerLuaReply(t, f, OperationFinishRequest, i, StatusFinished)
	i = workerLuaOtherGroupIntent(t, f, i)
	workerLuaReply(t, f, OperationReserveRequest, i, StatusReserved)
	i.Lease.Token = LeaseToken(strings.Repeat("f", 64))
	keys, args := runLuaParts(t, workerLuaRequest(t, f, OperationRenewLease, i), nil)
	source := sharedRenewalGrantSource(t, `
local held=assert(CJ.Request.load_live(ctx,run,job))
assert(held.v.group_id~=job.v.group_id and held.v.group_scope_id~=job.v.group_scope_id)
local source=assert(CJ.Wire.rate_keys(job.v.group_scope_id));local charged=assert(CJ.Wire.rate_keys(held.v.group_scope_id))
for _,key in ipairs(charged.ordered) do assert(CJ.Context.can_read(ctx,key) and not CJ.Context.can_write(ctx,key)) end
ctx.request.v.group_scope_id=job.v.group_scope_id;ctx.keys.scopes={group=source}
for _,key in ipairs(source.ordered) do
 ctx.allowed[key]=true;assert(not CJ.Context.can_read(ctx,key) and not CJ.Context.can_write(ctx,key))
 local value,code=CJ.Context.call(ctx,"TYPE",key);assert(value==nil and code=="INVALID_ARGUMENT")
end
local value,code=CJ.Context.bind_scope_reservations(ctx,job.v.group_scope_id);assert(value==nil and code=="INVALID_ARGUMENT")
value,code=CJ.Context.bind_rate(ctx,job.v.group_scope_id);assert(value==nil and code=="INVALID_ARGUMENT")
return {"held-scopes-only"}
`)
	before := f.r.snapshot()
	got := sharedLuaNoError(t, sharedLuaRun(t, f.r, source, keys, args))
	if !reflect.DeepEqual(got, []any{"held-scopes-only"}) || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
		t.Fatal("renewal inspection granted source-scope access")
	}
}

func TestSharedLuaRev4RenewalCorruptionPreemptsRejectionCounter(t *testing.T) {
	t.Parallel()
	for _, expired := range []bool{false, true} {
		for _, corrupt := range []string{"reservation_ttl", "late_scope_score", "job_index", "ordinary_headroom"} {
			t.Run(fmt.Sprintf("expired=%t/%s", expired, corrupt), func(t *testing.T) {
				f := workerLuaFixtureNew(t, 1, 0)
				i := workerLuaIntent(t, f, 0, 1, RequestDocument)
				workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
				id, err := DeriveReservationID(f.policy, i)
				if err != nil {
					t.Fatal(err)
				}
				if expired {
					f.r.now += 60000
				} else {
					i.Lease.Token = LeaseToken(strings.Repeat("f", 64))
				}
				var code ErrorCode
				switch corrupt {
				case "reservation_ttl":
					key := requestLuaPrefix + "reservation:" + string(id)
					entry := f.r.data[key]
					entry.expireAt = int64(f.r.now + 86400000)
					f.r.data[key] = entry
					code = ErrorReservationCorrupt
				case "late_scope_score":
					key := requestLuaPrefix + "rate:" + string(i.Decision.OriginScopeID) + ":active"
					f.r.zsets[key][string(id)]++ // final scope, after complete global/group inspection
					code = ErrorRateStateCorrupt
				case "job_index":
					f.r.zsets[ActiveLeasesKey][string(f.runID)+":"+string(i.Lease.JobID)]++
					code = ErrorStateIndexCorrupt
				case "ordinary_headroom":
					f.r.maximum = f.r.used + 67108864 + 1024*1024 // safety alone cannot fund this counter
					code = ErrorMemoryHeadroomLow
				}
				workerLuaReject(t, f, OperationRenewLease, i, code)
				if f.r.data[requestLuaPrefix+"run:"+string(f.runID)].hash["renewal_rejections_total"] != "0" {
					t.Fatal("corruption or insufficient ordinary headroom counted a renewal rejection")
				}
			})
		}
	}
}

// Permanent version of the external final-review reclaimed-delivery repro.
// Only initial authority/source facts are seeded. Every lease, request, retry,
// promotion and stage transition executes its actual current Lua implementation.
func sharedReclaimedAbortFixture(t *testing.T) (*workerLuaFixture, *stageOpsFixture) {
	t.Helper()
	f := workerLuaFixtureNew(t, 1, 0)
	f.r.data[runLuaKey("")].hash["render_policy_sha256"] = string(plainSHA256(testDenyAllRenderPolicyArtifact()))
	var err error
	f.policy, err = newTestTransportAuthority().parseRunPolicyAuthority(f.runID, workerLuaHashRecord(t, f.r, runLuaKey(""), SchemaRun), f.groups)
	if err != nil {
		t.Fatal(err)
	}
	intent := workerLuaIntent(t, f, 0, 1, RequestDocument)
	acquire := func() []any {
		workerLuaReply(t, f, OperationTryClaim, intent, StatusClaimed)
		f.r.now++
		raw := workerLuaReply(t, f, OperationStartRequest, intent, StatusStarted)
		f.r.now++
		workerLuaReply(t, f, OperationFinishRequest, intent, StatusFinished)
		f.r.now++
		return raw
	}
	acquire()
	request := jobLuaOutcomeWire(t, f.a, f.policy, f.sources[0], intent.Lease, OperationRetry, ReasonRequestTimeout)
	keys, args := runLuaParts(t, request, nil)
	result := sharedLuaNoError(t, stageOpsRun(t, &stageOpsRedis{f.r}, jobLuaOutcomeSource(t, OperationRetry), keys, args))
	if err := ValidateOperationResponse(OperationRetry, result); err != nil {
		t.Fatal(err)
	}
	retry := result.([]any)
	if retry[0] != string(StatusRetryScheduled) {
		t.Fatal(retry)
	}
	f.r.now, err = strconv.ParseUint(retry[2].(string), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	request, err = NewPromoteDueWireRequest(runLuaGate(t, f.a, OperationPromoteDue, false), f.runID)
	keys, args = runLuaParts(t, request, err)
	promoted := sharedLuaNoError(t, stageOpsRun(t, &stageOpsRedis{f.r}, maintenanceLuaSource(t, OperationPromoteDue), keys, args))
	if err := ValidateOperationResponse(OperationPromoteDue, promoted); err != nil {
		t.Fatal(err)
	}
	if values := promoted.([]any); values[0] != "BATCH_DONE" || values[2] != "1" {
		t.Fatal("retry was not promoted", promoted)
	}
	intent.Lease.Fence, intent.Lease.Token, intent.RequestOrdinal = 2, LeaseToken(strings.Repeat("b", 64)), 2
	raw := acquire()
	response, err := newTestTransportAuthority().parseStartRequestResponse(f.policy, intent, raw)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := response.IOPermit()
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewSuccessfulRequest(permit)
	if err != nil {
		t.Fatal(err)
	}
	source := f.sources[0]
	transcript, err := NewDocumentTranscript(f.policy, source, event)
	if err != nil {
		t.Fatal(err)
	}
	jobKey, err := RunJobKey(f.runID, source.JobID)
	if err != nil {
		t.Fatal(err)
	}
	j := f.r.data[jobKey].hash
	witness, err := newTestTransportAuthority().parseFinalDocumentWitness(intent.Lease, []string{
		j["last_document_request_started_at_ms"], j["last_document_request_fence"], j["last_document_target_url_id"],
		j["last_document_target_url"], j["last_document_target_digest"], j["request_starts"], j["last_request_started_at_ms"],
		j["lease_request_starts_baseline"], j["state"], j["lease_owner"], j["lease_token"], j["lease_fence"], j["active_reservation_id"],
	})
	if err != nil {
		t.Fatal(err)
	}
	render, err := NewRenderPolicyAuthorization(f.policy, testDenyAllRenderPolicyArtifact())
	if err != nil {
		t.Fatal(err)
	}
	outputContext, err := NewOutputContext(f.policy, source, transcript, witness, render)
	if err != nil {
		t.Fatal(err)
	}
	s := &stageOpsFixture{r: &stageOpsRedis{f.r}, a: f.a, context: outputContext, source: source, lease: intent.Lease,
		output: CrawlOutput{Page: OutputPage{NormalizedURL: source.CanonicalURL, HTML: []byte("<html>reclaimed</html>"), ContentType: "text/html", StatusCode: 200}},
		runKey: runLuaKey(""), jobKey: jobKey}
	s.rebuildOutput(t)
	s.expect(t, OperationBeginStage, nil, StatusStageBegun)
	f.r.now++
	j = f.r.data[jobKey].hash
	if j["lease_fence"] != "2" || j["delivery_attempts"] != "2" || j["retry_count"] != "1" ||
		j["lease_request_starts_baseline"] != "1" || j["request_starts"] != "2" ||
		j["last_reason"] != "request_timeout" || j["last_failure_reason"] != "request_timeout" {
		t.Fatal("actual reclaimed delivery lost the prior retry history")
	}
	return f, s
}

// Compose descriptors only to probe the real core footprint/admission boundary;
// do not replace Stage.prepare or inject a G, slot proof, policy, or validator.
// Rejected variants include earlier inert UNLINKs to verify pre-write rejection.
func sharedAbortFootprintSource(t *testing.T, reason, failure string, execute bool) string {
	t.Helper()
	source := sharedRuntimeCore(t, true) + `
local ctx=assert(CJ.Context.open(assert(CJ.Wire.stage_spec("CJ2_ABORT_STAGE")),KEYS,ARGV))
assert(CJ.Gate.check(ctx));local run=assert(CJ.Run.load(ctx,ctx.request.v.run_id))
local job=assert(CJ.Job.load(ctx,run,ctx.request.v.job_id));local a=ctx.request.v
local view=assert(CJ.Stage.select(ctx,a.commit_id))
local owned=assert(CJ.Stage.check_owned(view,run,job,a,a.commit_id))
local policy=assert(CJ.Memory.policy(ctx,"abort",owned))
local bound=assert(CJ.Memory.post_abort_bound(ctx,policy))
local plan=assert(CJ.Plan.new(ctx));assert(CJ.Plan.set_policy(plan,"abort",owned))
local inventory=assert(CJ.Read.snapshot(ctx,view.keys.keys))
for _,key in ipairs(inventory.items) do assert(CJ.Plan.add(plan,{"UNLINK",key},"ordinary")) end
assert(CJ.Plan.add(plan,{"ZREM",ctx.keys.stage_expiry,a.commit_id},"ordinary"))
local job_write={"HSET",job.key,"active_stage_commit_id","","last_transition_id",a.transition_id,
 "last_transition_status","STAGE_ABORTED","updated_at_ms",ctx.now_text,"last_reason",` + strconv.Quote(reason) + `}
`
	if failure != "" {
		source += `job_write[#job_write+1]="last_failure_reason";job_write[#job_write+1]=` + strconv.Quote(failure) + "\n"
	}
	source += `
assert(CJ.Plan.add(plan,job_write,"ordinary"))
assert(CJ.Plan.add(plan,{"HSET",ctx.keys.run,"last_activity_at_ms",ctx.now_text},"ordinary"))
local assessment,code=CJ.Plan.assess(ctx,plan);if not assessment then return CJ.Context.reject(code) end
assert(assessment.post_abort_bound==bound)
`
	if !execute {
		return source + `return {P.format_decimal(assessment.growth),P.format_decimal(assessment.remaining),P.format_decimal(bound)}`
	}
	return source + `
local reply=assert(CJ.Reply.build(ctx,"STAGE_ABORTED",{a.commit_id,owned.v.key_count}))
local execution=assert(CJ.Plan.seal(ctx,plan,assessment,reply))
for i=1,execution.count do redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc)) end
return execution.reply`
}

func sharedAbortAssessment(t *testing.T, s *stageOpsFixture) (growth, remaining, bound uint64) {
	t.Helper()
	keys, args := s.wire(t, OperationAbortStage, nil)
	before := s.r.snapshot()
	result := sharedLuaNoError(t, stageOpsRun(t, s.r, sharedAbortFootprintSource(t, "none", "", false), keys, args)).([]any)
	if len(result) != 3 || !reflect.DeepEqual(before, s.r.snapshot()) || s.r.attempts != 0 || s.r.aclCount != 0 {
		t.Fatal("abort assessment mutated state or skipped its bound")
	}
	values := []*uint64{&growth, &remaining, &bound}
	for i, value := range values {
		var err error
		*value, err = strconv.ParseUint(result[i].(string), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
	}
	return
}

func TestSharedLuaRev4AbortAfterRetryAndReclaim(t *testing.T) {
	t.Parallel()
	f, s := sharedReclaimedAbortFixture(t)
	growth, remaining, bound := sharedAbortAssessment(t, s)
	oldRemaining := s.remaining(t)
	before, growthBefore, withoutResetBefore := s.r.snapshot(), s.r.snapshot(), s.r.snapshot()
	originalCount := f.r.data[s.prefix+"meta"].hash["key_count"]
	s.expect(t, OperationAbortStage, nil, StatusStageAborted)
	actual := stageOpsGrowth(t, growthBefore, s.r.trace)
	if actual != growth || s.remaining(t) != remaining || oldRemaining-remaining != actual ||
		bound == 0 || actual+bound > TerminalStageControlFloorBytes || remaining < bound {
		t.Fatalf("abort/reset not included in exact debit/control proof: G=%d actual=%d remaining=%d bound=%d", growth, actual, remaining, bound)
	}
	// An accounting-only trace with the reset removed must omit exactly the
	// full replacement bytes of "none". This trace is NEVER executed/admitted.
	withoutReset := []bootLuaCommand{}
	resets := 0
	for _, call := range s.r.trace {
		if !call.acl && call.name == "HSET" && call.args[0] == s.jobKey {
			filtered := []string{call.args[0]}
			for i := 1; i < len(call.args); i += 2 {
				if call.args[i] == "last_reason" {
					if call.args[i+1] != "none" {
						t.Fatal("ABORT wrote a non-none reason")
					}
					resets++
				} else {
					filtered = append(filtered, call.args[i], call.args[i+1])
				}
			}
			call.args = filtered
		}
		withoutReset = append(withoutReset, call)
	}
	if resets != 1 || stageOpsGrowth(t, withoutResetBefore, withoutReset)+3*uint64(len("none")) != actual {
		t.Fatal("abort G omitted or discounted the reason reset")
	}
	stageOpsValidateHash(t, s.r, s.jobKey, SchemaJob)
	job := f.r.data[s.jobKey].hash
	if job["last_reason"] != "none" || job["last_failure_reason"] != "request_timeout" || job["active_stage_commit_id"] != "" ||
		job["last_transition_status"] != "STAGE_ABORTED" || !strings.HasSuffix(f.r.data[StageSlotsKey].hash[string(s.commit)], ":"+originalCount) {
		t.Fatal("abort did not preserve its exact history/terminal receipt")
	}
	changed := map[string]bool{"active_stage_commit_id": true, "last_transition_id": true, "last_transition_status": true, "last_reason": true, "updated_at_ms": true}
	for name, value := range before.data[s.jobKey].hash {
		if !changed[name] && job[name] != value {
			t.Fatalf("abort changed retained job field %s", name)
		}
	}
	for _, key := range wireOracleStageKeys(s.commit) {
		if _, exists := f.r.data[key]; exists {
			t.Fatal("abort retained a stage key")
		}
	}
	if _, exists := f.r.zsets[StageExpiryKey][string(s.commit)]; exists {
		t.Fatal("abort retained the stage expiry member")
	}
	replayed := f.r.snapshot()
	f.r.now++
	s.expect(t, OperationAbortStage, nil, StatusExistsIdentical)
	if !reflect.DeepEqual(replayed, f.r.snapshot()) || f.r.attempts != 0 {
		t.Fatal("reclaimed abort replay wrote or debited twice")
	}
	// Exercise a real subsequent outcome against that same retained slot, not
	// merely an asserted numeric allowance. The core independently rechecks G.
	request := jobLuaOutcomeWire(t, f.a, f.policy, s.source, s.lease, OperationRetry, ReasonRequestTimeout)
	keys, args := runLuaParts(t, request, nil)
	beforeOutcome := f.r.snapshot()
	result := sharedLuaNoError(t, stageOpsRun(t, s.r, jobLuaOutcomeSource(t, OperationRetry), keys, args))
	if err := ValidateOperationResponse(OperationRetry, result); err != nil {
		t.Fatal(err)
	}
	if result.([]any)[0] != string(StatusRetryScheduled) {
		t.Fatal(result)
	}
	outcomeGrowth := stageOpsGrowth(t, beforeOutcome, s.r.trace)
	if outcomeGrowth > bound || actual+outcomeGrowth > TerminalStageControlFloorBytes || outcomeGrowth > remaining {
		t.Fatal("actual following outcome escaped the derived abort control bound")
	}
	if _, exists := f.r.data[StageSlotsKey]; exists {
		t.Fatal("following outcome did not release the owned terminal slot")
	}
	stageOpsValidateHash(t, s.r, s.jobKey, SchemaJob)
	stageOpsAssertTrace(t, s.r)
	t.Logf("abort G=%d (includes %d reset bytes), derived post-outcome bound=%d, actual following G=%d", actual, len("none"), bound, outcomeGrowth)
}

func TestSharedLuaRev4AbortReasonFootprintRejectsOtherWrites(t *testing.T) {
	t.Parallel()
	_, s := sharedReclaimedAbortFixture(t)
	keys, args := s.wire(t, OperationAbortStage, nil)
	for _, test := range []struct{ name, reason, failure string }{
		{"invalid_reason", "not_a_reason", ""},
		{"empty_reason", "", ""},
		{"non_none_reason", "internal_error", ""},
		{"unchanged_non_none_reason", "request_timeout", ""},
		{"clear_failure_history", "none", "none"},
		{"replace_failure_history", "none", "internal_error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := s.r.snapshot()
			result := stageOpsRun(t, s.r, sharedAbortFootprintSource(t, test.reason, test.failure, true), keys, args)
			if result.runtimeErr != nil || result.raw != bootLuaErrorReply("ERR CRAWL_V2_INVALID_ARGUMENT") ||
				!reflect.DeepEqual(before, s.r.snapshot()) || s.r.attempts != 0 || s.r.aclCount != 0 {
				t.Fatalf("closed abort footprint failed before mutation: %v / %v", result.raw, result.runtimeErr)
			}
		})
	}
}

func TestSharedLuaRev4AbortControlBoundIncludesReasonReset(t *testing.T) {
	t.Parallel()
	_, s := sharedReclaimedAbortFixture(t)
	growth, remaining, bound := sharedAbortAssessment(t, s)
	parts := strings.Split(s.r.data[StageSlotsKey].hash[string(s.commit)], ":")
	parts[0], parts[4] = canonicalDecimal(remaining), s.r.data[s.prefix+"meta"].hash["key_count"]
	// Replace only the serialized remaining-width term of the measured final
	// descriptor. No production estimate or admission hook is supplied by Go.
	withoutSlot := growth - 3*uint64(len(strings.Join(parts, ":")))
	parts[0] = canonicalDecimal(bound)
	needed := bound + withoutSlot + 3*uint64(len(strings.Join(parts, ":")))
	if bound == 0 || needed > TerminalStageControlFloorBytes {
		t.Fatal("abort plus the registered-schema outcome bound exceeds the terminal floor")
	}
	parts[0], parts[4] = canonicalDecimal(needed-1), "0"
	s.r.data[StageSlotsKey].hash[string(s.commit)] = strings.Join(parts, ":")
	s.reject(t, OperationAbortStage, nil, nil, ErrorMemoryHeadroomLow)
	parts[0] = canonicalDecimal(needed)
	s.r.data[StageSlotsKey].hash[string(s.commit)] = strings.Join(parts, ":")
	before := s.r.snapshot()
	s.expect(t, OperationAbortStage, nil, StatusStageAborted)
	if s.remaining(t) != bound || stageOpsGrowth(t, before, s.r.trace)+bound != needed {
		t.Fatal("exact abort/reset plus following-outcome boundary was not conserved")
	}
}
