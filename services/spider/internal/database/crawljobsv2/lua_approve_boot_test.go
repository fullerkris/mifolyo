package crawljobsv2

import (
	"bytes"
	"crypto/sha1" // Redis EVALSHA framing only; this does not seal a source bundle.
	_ "embed"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/ast"
	"github.com/yuin/gopher-lua/parse"
)

// Test the exact proposed bytes, without a ScriptBindingSet, source substitution,
// fixture flag, Redis server, socket, or operational approval. The foundation's
// empty-Lua digest fixture does not bind this source. M3 remains incomplete.
//
//go:embed lua/cj2_approve_boot.lua
var bootLuaSource string

const (
	bootLuaKey      = "mifolyo:crawl:v2:durability"
	bootLuaNow      = uint64(1789488000123)
	bootLuaMaxAge   = uint64(2592000000)
	bootLuaMaxUint  = uint64(9007199254740991)
	bootLuaMaxBytes = 2097152
	bootLuaSecret   = "PRIVATE-BOOT-TEST-CANARY"
	bootLuaWriteErr = "bootLua injected unexpected datastore write failure"
)

var bootLuaFields = []string{
	"schema_version", "boot_state", "approved_redis_run_id", "boot_epoch",
	"approved_at_ms", "planned_shutdown_nonce", "planned_shutdown_evidence_sha256",
	"last_approval_mode", "consumed_planned_shutdown_nonce",
	"rehearsal_evidence_sha256", "rehearsal_at_ms", "acknowledged_loss_bound",
}

// This is a deliberately tiny RESP2 Redis facade, not a second boot transition.
// Only the seven commands BOOT may need are implemented. Hash state, physical
// expiry metadata, and unrelated counter canaries are exclusively in memory.
type bootLuaEntry struct {
	kind     string
	hash     map[string]string
	value    string
	expireAt int64
}

type bootLuaCommand struct {
	name string
	args []string
	acl  bool
}

type bootLuaSnapshot struct {
	data   map[string]bootLuaEntry
	writes int
}

type bootLuaRedis struct {
	data             map[string]bootLuaEntry
	seconds          string
	microseconds     string
	runID            string
	aclAllowed       bool
	trace            []bootLuaCommand
	writes           int
	overrides        map[string]func(*lua.LState) lua.LValue
	writeFailure     string // "before" or "after" applying HSET: injected, not rollback.
	prebuiltReply    *lua.LTable
	returnedPrebuilt bool
}

type bootLuaErrorReply string
type bootLuaStatusReply string

type bootLuaResult struct {
	raw        any
	runtimeErr error
}

func bootLuaNewRedis() *bootLuaRedis {
	r := &bootLuaRedis{
		data: map[string]bootLuaEntry{
			// Deliberately unusable for ordinary boot/marker/memory gates.
			"mifolyo:contracts:active":       {kind: "list", value: bootLuaSecret},
			"mifolyo:contracts:candidate":    {kind: "string", value: bootLuaSecret},
			"mifolyo:crawl:v2:contract":      {kind: "string", value: bootLuaSecret},
			"mifolyo:crawl:v2:stage_slots":   {kind: "string", value: bootLuaSecret},
			"mifolyo:crawl:v2:active_leases": {kind: "zset", value: bootLuaSecret},
			"mifolyo:crawl:v2:rate:canary":   {kind: "hash", hash: map[string]string{"started": "7", "pending": "2"}},
			"mifolyo:crawl:v2:run:canary":    {kind: "hash", hash: map[string]string{"request_starts": "9", "claims_total": "64"}},
			"mifolyo:crawl:v1:queue":         {kind: "zset", value: bootLuaSecret},
			"pages_queue":                    {kind: "list", value: bootLuaSecret},
			"page_data:canary":               {kind: "hash", hash: map[string]string{"html": bootLuaSecret}, expireAt: -1},
		},
		runID: strings.Repeat("a", 40), aclAllowed: true,
		overrides: make(map[string]func(*lua.LState) lua.LValue),
	}
	r.setTime(bootLuaNow)
	return r
}

func (r *bootLuaRedis) setTime(ms uint64) {
	r.seconds = strconv.FormatUint(ms/1000, 10)
	r.microseconds = strconv.FormatUint(ms%1000*1000, 10)
}

func (r *bootLuaRedis) snapshot() bootLuaSnapshot {
	snapshot := bootLuaSnapshot{data: make(map[string]bootLuaEntry, len(r.data)), writes: r.writes}
	for key, entry := range r.data {
		if entry.hash != nil {
			copied := make(map[string]string, len(entry.hash))
			for field, value := range entry.hash {
				copied[field] = value
			}
			entry.hash = copied
		}
		snapshot.data[key] = entry
	}
	return snapshot
}

func bootLuaArray(L *lua.LState, values ...lua.LValue) *lua.LTable {
	table := L.NewTable()
	for i, value := range values {
		table.RawSetInt(i+1, value)
	}
	return table
}

func bootLuaStrings(L *lua.LState, values []string) *lua.LTable {
	table := L.NewTable()
	for i, value := range values {
		table.RawSetInt(i+1, lua.LString(value))
	}
	return table
}

func bootLuaStatus(L *lua.LState, value string) *lua.LTable {
	table := L.NewTable()
	table.RawSetString("ok", lua.LString(value))
	return table
}

// Never stringify Redis/Lua integers, nulls, booleans, errors, or status tables
// into bulk strings. Redis Lua's default RESP2 conversion is intentional here.
func bootLuaRESP(value lua.LValue) any {
	switch value := value.(type) {
	case lua.LString:
		return string(value)
	case lua.LNumber:
		return int64(value)
	case lua.LBool:
		if value {
			return int64(1)
		}
		return nil
	case *lua.LTable:
		if text, ok := value.RawGetString("err").(lua.LString); ok {
			return bootLuaErrorReply(text)
		}
		if text, ok := value.RawGetString("ok").(lua.LString); ok {
			return bootLuaStatusReply(text)
		}
		array := make([]any, 0, value.Len())
		for i := 1; ; i++ {
			item := value.RawGetInt(i)
			if item == lua.LNil {
				break
			}
			array = append(array, bootLuaRESP(item))
		}
		return array
	default:
		return nil
	}
}

func bootLuaPrebuiltTable(L *lua.LState, expected []any) *lua.LTable {
	frame, ok := L.GetStack(1)
	if !ok {
		return nil
	}
	// Inspect actual caller registers, not debug variable names: gopher-lua
	// 1.1.1's local-name metadata includes expired loop variables at this point.
	// No Lua table is synthesized here. Its identity must survive until return.
	for index := 1; index <= 256; index++ {
		name, value := L.GetLocal(frame, index)
		if name == "" {
			break
		}
		if table, ok := value.(*lua.LTable); ok && reflect.DeepEqual(bootLuaRESP(table), expected) {
			return table
		}
	}
	return nil
}

func (r *bootLuaRedis) command(L *lua.LState, acl bool) int {
	parts := make([]string, L.GetTop())
	for i := range parts {
		value, ok := L.Get(i + 1).(lua.LString)
		if !ok {
			L.RaiseError("bootLua command argument is not a bulk string")
			return 0
		}
		parts[i] = string(value)
	}
	if len(parts) == 0 {
		L.RaiseError("bootLua missing command")
		return 0
	}
	name, args := strings.ToUpper(parts[0]), parts[1:]
	r.trace = append(r.trace, bootLuaCommand{name: name, args: args, acl: acl})
	valid := false
	switch name {
	case "TIME":
		valid = len(args) == 0
	case "INFO":
		valid = reflect.DeepEqual(args, []string{"SERVER"})
	case "TYPE", "HLEN":
		valid = len(args) == 1 && args[0] == bootLuaKey
	case "HSTRLEN":
		valid = len(args) == 2 && args[0] == bootLuaKey
	case "HMGET":
		valid = len(args) == 13 && args[0] == bootLuaKey && reflect.DeepEqual(args[1:], bootLuaFields)
	case "HSET":
		valid = len(args) == 25 && args[0] == bootLuaKey
		if valid {
			for i, field := range bootLuaFields {
				valid = valid && args[2*i+1] == field
			}
		}
	}
	if !valid || acl && name != "HSET" {
		L.RaiseError("bootLua forbidden command or shape")
		return 0
	}
	if acl {
		// Observe the actual caller frame, not a reconstructed test response:
		// both descriptors must already exist when checking write permission.
		write := bootLuaPrebuiltTable(L, bootLuaAnyStrings(parts))
		reply := bootLuaPrebuiltTable(L, []any{"OK", parts[11], parts[9]})
		if write == nil || reply == nil {
			L.RaiseError("bootLua mutation descriptor or response not prebuilt")
			return 0
		}
		r.prebuiltReply = reply
		L.Push(lua.LBool(r.aclAllowed))
		return 1
	}
	entry, exists := r.data[bootLuaKey]
	if name == "HLEN" || name == "HSTRLEN" || name == "HMGET" || name == "HSET" {
		if exists && entry.kind != "hash" {
			L.RaiseError("WRONGTYPE")
			return 0
		}
	}
	var result lua.LValue
	switch name {
	case "TIME":
		result = bootLuaArray(L, lua.LString(r.seconds), lua.LString(r.microseconds))
	case "INFO":
		result = lua.LString("# Server\r\nredis_version:7.0.0\r\nrun_id:" + r.runID + "\r\nuptime_in_seconds:1\r\n")
	case "TYPE":
		kind := "none"
		if exists {
			kind = entry.kind
		}
		result = bootLuaStatus(L, kind)
	case "HLEN":
		result = lua.LNumber(len(entry.hash))
	case "HSTRLEN":
		result = lua.LNumber(len(entry.hash[args[1]]))
	case "HMGET":
		values := make([]lua.LValue, len(args)-1)
		for i, field := range args[1:] {
			if value, found := entry.hash[field]; found {
				values[i] = lua.LString(value)
			} else {
				values[i] = lua.LFalse // RESP2 nil bulk, not the string "false".
			}
		}
		result = bootLuaArray(L, values...)
	case "HSET":
		if r.writeFailure == "before" {
			L.RaiseError(bootLuaWriteErr)
			return 0
		}
		if r.prebuiltReply == nil || !r.aclAllowed {
			L.RaiseError("bootLua write without preflight")
			return 0
		}
		if !exists {
			entry = bootLuaEntry{kind: "hash", hash: make(map[string]string), expireAt: -1}
		}
		added := 0
		for i := 1; i < len(args); i += 2 {
			if _, found := entry.hash[args[i]]; !found {
				added++
			}
			entry.hash[args[i]] = args[i+1]
		}
		r.data[bootLuaKey] = entry // HSET preserves an existing key's expiry.
		r.writes++
		if r.writeFailure == "after" {
			// Deliberate fault injection after application. This is NOT a claim
			// that Redis HSET normally applies then errors, or can roll back Lua.
			L.RaiseError(bootLuaWriteErr)
			return 0
		}
		result = lua.LNumber(added)
	}
	if override := r.overrides[name]; override != nil {
		result = override(L)
	}
	L.Push(result)
	return 1
}

func bootLuaAnyStrings(values []string) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

func bootLuaRun(t *testing.T, r *bootLuaRedis, keys, args []string) bootLuaResult {
	t.Helper()
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()
	for _, library := range []struct {
		name string
		open lua.LGFunction
	}{{lua.BaseLibName, lua.OpenBase}, {lua.StringLibName, lua.OpenString}, {lua.MathLibName, lua.OpenMath}, {lua.TabLibName, lua.OpenTable}} {
		L.Push(L.NewFunction(library.open))
		L.Push(lua.LString(library.name))
		L.Call(1, 0)
	}
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "collectgarbage", "print"} {
		L.SetGlobal(name, lua.LNil)
	}
	L.SetContext(luaTestContext(t))
	r.trace, r.prebuiltReply, r.returnedPrebuilt = nil, nil, false
	redis := L.NewTable()
	L.SetFuncs(redis, map[string]lua.LGFunction{
		"call":          func(L *lua.LState) int { return r.command(L, false) },
		"acl_check_cmd": func(L *lua.LState) int { return r.command(L, true) },
		"error_reply": func(L *lua.LState) int {
			text, ok := L.Get(1).(lua.LString)
			if !ok || L.GetTop() != 1 {
				L.RaiseError("bootLua invalid error reply")
				return 0
			}
			table := L.NewTable()
			table.RawSetString("err", text)
			L.Push(table)
			return 1
		},
	})
	L.SetGlobal("redis", redis)
	L.SetGlobal("KEYS", bootLuaStrings(L, keys))
	L.SetGlobal("ARGV", bootLuaStrings(L, args))
	function, err := L.Load(strings.NewReader(bootLuaSource), "@lua/cj2_approve_boot.lua")
	if err != nil {
		t.Fatalf("proposed Lua does not compile: %v", err)
	}
	err = L.CallByParam(lua.P{Fn: function, NRet: 1, Protect: true})
	bootLuaAssertTrace(t, r)
	if err != nil {
		return bootLuaResult{runtimeErr: err}
	}
	value := L.Get(-1)
	r.returnedPrebuilt = r.prebuiltReply != nil && value == r.prebuiltReply
	return bootLuaResult{raw: bootLuaRESP(value)}
}

func bootLuaAssertTrace(t *testing.T, r *bootLuaRedis) {
	t.Helper()
	timeCalls, infoCalls, writes := 0, 0, 0
	var aclArgs []string
	lengths := make(map[string]bool)
	countChecked := false
	for _, command := range r.trace {
		if writes != 0 {
			t.Fatal("Redis call after the sole mutation")
		}
		if command.acl {
			aclArgs = command.args
			continue
		}
		switch command.name {
		case "TIME":
			timeCalls++
		case "INFO":
			infoCalls++
			if !reflect.DeepEqual(command.args, []string{"SERVER"}) {
				t.Fatal("boot queried an INFO section other than SERVER")
			}
		case "TYPE", "HLEN", "HSTRLEN", "HMGET", "HSET":
			if command.args[0] != bootLuaKey {
				t.Fatal("boot touched a key other than durability")
			}
		default:
			t.Fatal("boot used a command outside its isolated exception")
		}
		switch command.name {
		case "HLEN":
			countChecked = true
		case "HSTRLEN":
			if !countChecked {
				t.Fatal("field inspection before cardinality bound")
			}
			lengths[command.args[1]] = true
		case "HMGET":
			if len(lengths) != 12 {
				t.Fatal("bulk retrieval before all twelve value lengths were bounded")
			}
		case "HSET":
			writes++
			if !reflect.DeepEqual(command.args, aclArgs) {
				t.Fatal("write differs from the fully preflighted ACL descriptor")
			}
		}
	}
	if timeCalls != 1 || infoCalls > 1 {
		t.Fatalf("clock/server call counts: TIME=%d INFO=%d", timeCalls, infoCalls)
	}
}

func bootLuaArgs(mode ApprovalMode) []string {
	args := []string{
		strings.Repeat("a", 40), strings.Repeat("b", 32), strings.Repeat("c", 64),
		strconv.FormatUint(bootLuaNow-1234, 10), "0", "", "", string(mode),
	}
	if mode == ApprovalPlanned {
		args[5], args[6] = strings.Repeat("d", 32), strings.Repeat("e", 64)
	}
	return args
}

func bootLuaPostHash(args []string, at uint64) map[string]string {
	values := []string{"1", "approved", args[0], args[1], strconv.FormatUint(at, 10), "", args[6], args[7], args[5], args[2], args[3], "0"}
	hash := make(map[string]string, 12)
	for i, field := range bootLuaFields {
		hash[field] = values[i]
	}
	return hash
}

func bootLuaSeed(r *bootLuaRedis, state BootState, mode ApprovalMode) {
	args := bootLuaArgs(mode)
	args[0], args[1] = strings.Repeat("1", 40), strings.Repeat("2", 32)
	// Old evidence may have expired. A new approval supplies current evidence;
	// BOOT must not demand that a stale previous process's evidence be current.
	args[2], args[3] = strings.Repeat("3", 64), "1"
	hash := bootLuaPostHash(args, 2)
	hash["boot_state"] = string(state)
	if state == BootPlanned {
		hash["planned_shutdown_nonce"] = strings.Repeat("d", 32)
		hash["planned_shutdown_evidence_sha256"] = strings.Repeat("e", 64)
		hash["consumed_planned_shutdown_nonce"] = ""
	} else if state == BootUnapproved {
		hash["planned_shutdown_nonce"], hash["planned_shutdown_evidence_sha256"], hash["consumed_planned_shutdown_nonce"] = "", "", ""
	}
	r.data[bootLuaKey] = bootLuaEntry{kind: "hash", hash: hash, expireAt: -1}
}

func bootLuaPrepare(mode ApprovalMode) (*bootLuaRedis, []string) {
	r := bootLuaNewRedis()
	if mode == ApprovalPlanned {
		bootLuaSeed(r, BootPlanned, ApprovalInitial)
	} else if mode == ApprovalUncleanRehearsal {
		bootLuaSeed(r, BootApproved, ApprovalInitial)
	}
	return r, bootLuaArgs(mode)
}

func bootLuaDecodeHash(hash map[string]string) (DurabilityRecord, error) {
	if len(hash) != len(bootLuaFields) {
		return DurabilityRecord{}, ErrInvalidSchema
	}
	record := make(Record, len(bootLuaFields))
	for i, field := range bootLuaFields {
		value, found := hash[field]
		if !found {
			return DurabilityRecord{}, ErrInvalidSchema
		}
		record[i] = Field{Name: field, Value: []byte(value)}
	}
	encoded, err := EncodeRecord(record)
	if err != nil {
		return DurabilityRecord{}, err
	}
	return DecodeDurabilityRecord(encoded)
}

func bootLuaAssertPost(t *testing.T, r *bootLuaRedis, args []string, approvedAt uint64) {
	t.Helper()
	entry, exists := r.data[bootLuaKey]
	if !exists || entry.kind != "hash" || !reflect.DeepEqual(entry.hash, bootLuaPostHash(args, approvedAt)) {
		t.Fatal("durability differs from the complete twelve-field approved post-state")
	}
	record, err := bootLuaDecodeHash(entry.hash)
	if err != nil {
		t.Fatalf("Go durability codec rejected actual Lua post-state: %v", err)
	}
	input, err := record.Input()
	evidenceAt, parseErr := strconv.ParseUint(args[3], 10, 64)
	// DecodeDurabilityRecord validates the record in isolation. Check the actual
	// request/server/time context separately, without inventing tool authority.
	want := DurabilityRecordInput{
		BootState: BootApproved, ApprovedRedisRunID: r.runID, BootEpoch: args[1], ApprovedAtMS: approvedAt,
		PlannedShutdownEvidenceSHA256: Digest(args[6]), LastApprovalMode: ApprovalMode(args[7]),
		ConsumedPlannedShutdownNonce: args[5], RehearsalEvidenceSHA256: Digest(args[2]), RehearsalAtMS: evidenceAt,
	}
	if err != nil || parseErr != nil || input != want {
		t.Fatal("decoded durability does not match the actual boot approval context")
	}
}

func bootLuaAccept(t *testing.T, r *bootLuaRedis, args []string, status Status, now uint64) {
	t.Helper()
	before := r.snapshot()
	result := bootLuaRun(t, r, []string{bootLuaKey}, args)
	if result.runtimeErr != nil {
		t.Fatalf("unexpected Lua error: %v", result.runtimeErr)
	}
	// This is the existing Go envelope oracle (there is no exported function
	// named ValidateResponseEnvelope in this foundation). BOOT needs no lease context.
	if err := ValidateOperationResponse(OperationApproveBoot, result.raw); err != nil {
		t.Fatalf("Go response oracle rejected actual Lua RESP2 response: %v", err)
	}
	want := []any{string(status), strconv.FormatUint(now, 10), args[1]}
	if !reflect.DeepEqual(result.raw, want) {
		t.Fatal("Lua response does not have the exact bulk-string BOOT envelope")
	}
	after := r.snapshot()
	if status == StatusExistsIdentical {
		if !reflect.DeepEqual(before, after) {
			t.Fatal("exact replay mutated state, expiry, or write counter")
		}
		for _, call := range r.trace {
			if call.acl || call.name == "HSET" {
				t.Fatal("read-only replay attempted write admission")
			}
		}
	} else {
		if after.writes != before.writes+1 || !r.returnedPrebuilt {
			t.Fatal("approval did not issue exactly one write and return the prebuilt response")
		}
		bootLuaAssertPost(t, r, args, now)
		delete(before.data, bootLuaKey)
		delete(after.data, bootLuaKey)
		if !reflect.DeepEqual(before.data, after.data) {
			t.Fatal("approval changed unrelated state or counters")
		}
	}
	if len(r.trace) < 2 || r.trace[1].name != "INFO" {
		t.Fatal("successful approval/replay did not inspect actual INFO SERVER")
	}
}

func bootLuaReject(t *testing.T, r *bootLuaRedis, keys, args []string, code ErrorCode) {
	t.Helper()
	before := r.snapshot() // Every expected rejection snapshots ALL datastore state.
	result := bootLuaRun(t, r, keys, args)
	if !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("rejected operation mutated datastore state, expiry, or write counter")
	}
	if result.runtimeErr != nil {
		t.Fatalf("validation failure escaped as an unexpected Lua error: %v", result.runtimeErr)
	}
	reply, ok := result.raw.(bootLuaErrorReply)
	if !ok || !strings.HasPrefix(string(reply), "ERR CRAWL_V2_") || strings.Contains(string(reply), bootLuaSecret) {
		t.Fatal("rejection was not a redacted closed protocol error")
	}
	parsed, err := ParseErrorCode(strings.TrimPrefix(string(reply), "ERR CRAWL_V2_"))
	if err != nil || code != "" && parsed != code {
		t.Fatalf("rejection error code: got %q, want %q, parse error %v", parsed, code, err)
	}
	for _, call := range r.trace {
		if !call.acl && call.name == "HSET" {
			t.Fatal("validation rejection attempted a datastore write")
		}
	}
}

func TestBootLuaGoWireAndPositiveReplay(t *testing.T) {
	t.Parallel()
	fields, err := RecordSchemaFields(SchemaDurability)
	if err != nil || !reflect.DeepEqual(fields, bootLuaFields) || DurabilityKey != bootLuaKey {
		t.Fatal("independent boot schema/key expectation differs from Go")
	}
	for _, mode := range []ApprovalMode{ApprovalInitial, ApprovalPlanned, ApprovalUncleanRehearsal} {
		t.Run(string(mode), func(t *testing.T) {
			r, expected := bootLuaPrepare(mode)
			input := ApproveBootWireInput{
				CurrentRedisRunID: expected[0], ProposedBootEpoch: expected[1], EvidenceSHA256: Digest(expected[2]),
				EvidenceAtMS: bootLuaNow - 1234, PlannedNonce: expected[5],
				PlannedShutdownEvidenceSHA256: Digest(expected[6]), ApprovalMode: mode,
			}
			request, err := NewApproveBootWireRequest(input)
			if err != nil {
				t.Fatal(err)
			}
			// Extract the real constructor's unbound wire parts. Do not manufacture
			// a private fake 43-source seal merely to exercise these eight ARGVs.
			keys, argv, count, err := request.validatedWireParts()
			if err != nil || count != 0 || len(argv) != 8 || !reflect.DeepEqual(keys, [][]byte{[]byte(bootLuaKey)}) {
				t.Fatal("Go BOOT constructor wire shape differs")
			}
			args := make([]string, len(argv))
			for i := range argv {
				args[i] = string(argv[i])
			}
			if !reflect.DeepEqual(args, expected) {
				t.Fatal("Go BOOT constructor ARGV order differs")
			}
			bootLuaAccept(t, r, args, StatusOK, bootLuaNow)
			r.setTime(bootLuaNow + 10001)
			r.aclAllowed = false // No prospective write on exact post-state replay.
			bootLuaAccept(t, r, args, StatusExistsIdentical, bootLuaNow+10001)
			bootLuaAssertPost(t, r, args, bootLuaNow)
		})
	}
}

func TestBootLuaFirstApprovalStateModeProcessEpochMatrix(t *testing.T) {
	t.Parallel()
	for _, state := range []BootState{"", BootApproved, BootPlanned, BootUnapproved} {
		for _, previousMode := range []ApprovalMode{ApprovalInitial, ApprovalPlanned, ApprovalUncleanRehearsal} {
			if state == "" && previousMode != ApprovalInitial {
				continue
			}
			for _, mode := range []ApprovalMode{ApprovalInitial, ApprovalPlanned, ApprovalUncleanRehearsal} {
				for _, newProcess := range []bool{false, true} {
					for _, freshEpoch := range []bool{false, true} {
						name := fmt.Sprintf("%s/%s/%s/new_process=%t/fresh_epoch=%t", state, previousMode, mode, newProcess, freshEpoch)
						t.Run(name, func(t *testing.T) {
							r, args := bootLuaNewRedis(), bootLuaArgs(mode)
							if state != "" {
								bootLuaSeed(r, state, previousMode)
								if _, err := bootLuaDecodeHash(r.data[bootLuaKey].hash); err != nil {
									t.Fatal("matrix pre-state not Go-codec valid")
								}
								if !newProcess {
									r.runID = strings.Repeat("1", 40)
									args[0] = r.runID
								}
								if !freshEpoch {
									args[1] = strings.Repeat("2", 32)
								}
							}
							allowed := state == "" && mode == ApprovalInitial || state != "" && newProcess && freshEpoch &&
								(mode == ApprovalUncleanRehearsal || mode == ApprovalPlanned && state == BootPlanned)
							if allowed {
								bootLuaAccept(t, r, args, StatusOK, bootLuaNow)
							} else {
								bootLuaReject(t, r, []string{bootLuaKey}, args, "")
							}
						})
					}
				}
			}
		}
	}
}

func TestBootLuaConflictingCurrentRunInputs(t *testing.T) {
	t.Parallel()
	for _, mode := range []ApprovalMode{ApprovalInitial, ApprovalPlanned, ApprovalUncleanRehearsal} {
		for index := 0; index < 8; index++ {
			t.Run(fmt.Sprintf("%s/argument_%d", mode, index+1), func(t *testing.T) {
				r, args := bootLuaPrepare(mode)
				bootLuaAccept(t, r, args, StatusOK, bootLuaNow)
				code := ErrorImmutableMismatch
				switch index {
				case 0:
					args[0], code = strings.Repeat("f", 40), ErrorBootUnapproved
				case 1:
					args[1] = strings.Repeat("f", 32)
				case 2:
					args[2] = strings.Repeat("f", 64)
				case 3:
					args[3] = strconv.FormatUint(bootLuaNow-1235, 10)
				case 4:
					args[4], code = "1", ErrorInvalidArgument
				case 5:
					args[5] = strings.Repeat("f", 32)
					if mode != ApprovalPlanned {
						code = ErrorInvalidArgument
					}
				case 6:
					args[6] = strings.Repeat("f", 64)
					if mode != ApprovalPlanned {
						code = ErrorInvalidArgument
					}
				case 7:
					args[5], args[6], args[7] = "", "", "initial"
					if mode == ApprovalInitial {
						args[7] = "unclean_rehearsal"
					}
				}
				bootLuaReject(t, r, []string{bootLuaKey}, args, code)
			})
		}
	}
}

func TestBootLuaPlannedNonceEvidenceAndRestart(t *testing.T) {
	t.Parallel()
	for _, index := range []int{5, 6} {
		t.Run(fmt.Sprintf("changed_planned_argument_%d", index), func(t *testing.T) {
			r, args := bootLuaPrepare(ApprovalPlanned)
			args[index] = strings.Repeat("f", len(args[index]))
			bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorImmutableMismatch)
		})
	}
	t.Run("same_process_cannot_abort_planned_shutdown", func(t *testing.T) {
		r, args := bootLuaPrepare(ApprovalPlanned)
		r.runID, args[0] = strings.Repeat("1", 40), strings.Repeat("1", 40)
		bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorBootUnapproved)
		args[5], args[6], args[7] = "", "", "unclean_rehearsal"
		bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorBootUnapproved)
	})
	t.Run("consumed_nonce_is_not_a_second_restart_permission", func(t *testing.T) {
		r, args := bootLuaPrepare(ApprovalPlanned)
		bootLuaAccept(t, r, args, StatusOK, bootLuaNow)
		r.runID = strings.Repeat("f", 40)
		bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorBootUnapproved)
		args[0], args[1] = r.runID, strings.Repeat("4", 32)
		bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorBootUnapproved)
	})
}

func TestBootLuaExactKeyAndArgumentCounts(t *testing.T) {
	t.Parallel()
	for _, keys := range [][]string{nil, {bootLuaKey, bootLuaKey}, {bootLuaKey, "pages_queue"}, {"pages_queue"}, {bootLuaKey + ":suffix"}, {bootLuaSecret}} {
		t.Run(fmt.Sprintf("keys_%d_%d", len(keys), len(strings.Join(keys, ""))), func(t *testing.T) {
			bootLuaReject(t, bootLuaNewRedis(), keys, bootLuaArgs(ApprovalInitial), ErrorInvalidArgument)
		})
	}
	for count := 0; count <= 15; count++ {
		if count == 8 {
			continue
		}
		t.Run(fmt.Sprintf("argc_%d", count), func(t *testing.T) {
			args := append(bootLuaArgs(ApprovalInitial), make([]string, 8)...)
			bootLuaReject(t, bootLuaNewRedis(), []string{bootLuaKey}, args[:count], ErrorInvalidArgument)
		})
	}
}

func TestBootLuaInputLexicalRejections(t *testing.T) {
	t.Parallel()
	for _, spec := range []struct {
		index int
		width int
	}{{0, 40}, {1, 32}, {2, 64}, {5, 32}, {6, 64}} {
		for i, bad := range []string{
			"", strings.Repeat("a", spec.width-1), strings.Repeat("a", spec.width+1),
			strings.Repeat("A", spec.width), strings.Repeat("g", spec.width),
			strings.Repeat("a", spec.width-1) + "\x00", strings.Repeat("a", spec.width-1) + "\n",
			strings.Repeat("a", spec.width-1) + "\xff", strings.Repeat("é", spec.width/2), bootLuaSecret,
		} {
			t.Run(fmt.Sprintf("hex_%d_%d", spec.index, i), func(t *testing.T) {
				r, args := bootLuaPrepare(ApprovalPlanned)
				args[spec.index] = bad
				bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorInvalidIdentifier)
			})
		}
	}
	for i, bad := range []string{"", "0", "00", "01", "+1", "-1", "1.0", "1e3", " 1", "1 ", "1\n", "NaN", "inf", "-inf", "0x10", "9007199254740992", "9999999999999999", "10000000000000000", "\x001", "١"} {
		t.Run(fmt.Sprintf("evidence_time_%d", i), func(t *testing.T) {
			args := bootLuaArgs(ApprovalInitial)
			args[3] = bad
			bootLuaReject(t, bootLuaNewRedis(), []string{bootLuaKey}, args, ErrorInvalidNumber)
		})
	}
	for _, index := range []int{2, 6} {
		t.Run(fmt.Sprintf("zero_evidence_%d", index), func(t *testing.T) {
			r, args := bootLuaPrepare(ApprovalPlanned)
			args[index] = strings.Repeat("0", 64)
			bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorInvalidArgument)
		})
	}
	for i, bad := range []string{"", "1", "00", "-0", "+0", "0.0", "0e0", " 0", "0\n", "9007199254740991", bootLuaSecret} {
		t.Run(fmt.Sprintf("literal_zero_loss_bound_%d", i), func(t *testing.T) {
			args := bootLuaArgs(ApprovalInitial)
			args[4] = bad
			bootLuaReject(t, bootLuaNewRedis(), []string{bootLuaKey}, args, ErrorInvalidArgument)
		})
	}
	for i, bad := range []string{"", "INITIAL", "unclean", "planned ", " initial", "initial\x00", bootLuaSecret} {
		t.Run(fmt.Sprintf("closed_mode_%d", i), func(t *testing.T) {
			args := bootLuaArgs(ApprovalInitial)
			args[7] = bad
			bootLuaReject(t, bootLuaNewRedis(), []string{bootLuaKey}, args, ErrorInvalidArgument)
		})
	}
	for _, mode := range []ApprovalMode{ApprovalInitial, ApprovalUncleanRehearsal} {
		for _, index := range []int{5, 6} {
			t.Run(fmt.Sprintf("%s/nonempty_planned_%d", mode, index), func(t *testing.T) {
				r, args := bootLuaPrepare(mode)
				args[index] = bootLuaArgs(ApprovalPlanned)[index]
				bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorInvalidArgument)
			})
		}
	}
}

func TestBootLuaDoesNotInventIdentifierOrEvidenceRestrictions(t *testing.T) {
	t.Parallel()
	t.Run("zero_hex_identifiers_are_not_zero_evidence", func(t *testing.T) {
		r, args := bootLuaPrepare(ApprovalPlanned)
		r.runID, args[0], args[1], args[5] = strings.Repeat("0", 40), strings.Repeat("0", 40), strings.Repeat("0", 32), strings.Repeat("0", 32)
		args[2], args[6] = strings.Repeat("0", 63)+"1", strings.Repeat("0", 63)+"1"
		r.data[bootLuaKey].hash["planned_shutdown_nonce"] = args[5]
		r.data[bootLuaKey].hash["planned_shutdown_evidence_sha256"] = args[6]
		bootLuaAccept(t, r, args, StatusOK, bootLuaNow)
		bootLuaAccept(t, r, args, StatusExistsIdentical, bootLuaNow)
	})
	t.Run("new_epoch_not_new_evidence_digest_is_required", func(t *testing.T) {
		r, args := bootLuaPrepare(ApprovalUncleanRehearsal)
		r.data[bootLuaKey].hash["rehearsal_evidence_sha256"] = args[2]
		r.data[bootLuaKey].hash["rehearsal_at_ms"] = args[3]
		bootLuaAccept(t, r, args, StatusOK, bootLuaNow)
	})
	t.Run("no_unstated_boot_ttl_repair_or_rejection", func(t *testing.T) {
		r, args := bootLuaPrepare(ApprovalUncleanRehearsal)
		entry := r.data[bootLuaKey]
		entry.expireAt = int64(bootLuaNow + 60000)
		r.data[bootLuaKey] = entry
		bootLuaAccept(t, r, args, StatusOK, bootLuaNow)
		bootLuaAccept(t, r, args, StatusExistsIdentical, bootLuaNow)
		if r.data[bootLuaKey].expireAt != entry.expireAt {
			t.Fatal("HSET unexpectedly changed physical expiry metadata")
		}
	})
}

func TestBootLuaEvidenceAgeBoundaries(t *testing.T) {
	t.Parallel()
	for _, mode := range []ApprovalMode{ApprovalInitial, ApprovalPlanned, ApprovalUncleanRehearsal} {
		for _, age := range []int64{-1, 0, 1, int64(bootLuaMaxAge) - 1, int64(bootLuaMaxAge), int64(bootLuaMaxAge) + 1} {
			t.Run(fmt.Sprintf("%s/age_%d", mode, age), func(t *testing.T) {
				r, args := bootLuaPrepare(mode)
				args[3] = strconv.FormatInt(int64(bootLuaNow)-age, 10)
				if age >= 0 && age <= int64(bootLuaMaxAge) {
					bootLuaAccept(t, r, args, StatusOK, bootLuaNow)
					bootLuaAccept(t, r, args, StatusExistsIdentical, bootLuaNow)
				} else {
					bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorBootUnapproved)
				}
			})
		}
		t.Run(string(mode)+"/replay_does_not_refresh_expired_evidence", func(t *testing.T) {
			r, args := bootLuaPrepare(mode)
			args[3] = strconv.FormatUint(bootLuaNow, 10)
			bootLuaAccept(t, r, args, StatusOK, bootLuaNow)
			r.setTime(bootLuaNow + bootLuaMaxAge)
			bootLuaAccept(t, r, args, StatusExistsIdentical, bootLuaNow+bootLuaMaxAge)
			r.setTime(bootLuaNow + bootLuaMaxAge + 1)
			bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorBootUnapproved)
			r.setTime(bootLuaNow - 1)
			bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorBootUnapproved)
		})
	}
}

func TestBootLuaRedisTimeExactArithmetic(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		seconds, micros string
		now             uint64
	}{
		{"0", "1000", 1}, {"1", "0", 1000}, {"1", "1", 1000}, {"1", "999", 1000},
		{"1", "1000", 1001}, {"1789488000", "123456", bootLuaNow},
		{"1789488000", "999999", 1789488000999},
		{"9007199254740", "0", 9007199254740000},
		{"9007199254740", "991000", bootLuaMaxUint}, {"9007199254740", "991999", bootLuaMaxUint},
	} {
		t.Run(test.seconds+"_"+test.micros, func(t *testing.T) {
			r, args := bootLuaPrepare(ApprovalInitial)
			r.seconds, r.microseconds, args[3] = test.seconds, test.micros, strconv.FormatUint(test.now, 10)
			bootLuaAccept(t, r, args, StatusOK, test.now)
			bootLuaAccept(t, r, args, StatusExistsIdentical, test.now)
		})
	}
	for i, test := range [][2]string{
		{"0", "0"}, {"0", "999"}, {"9007199254740", "992000"}, {"9007199254741", "0"},
		{"9007199254740991", "0"}, {"9007199254740992", "0"}, {"1", "1000000"},
		{"-1", "0"}, {"1", "-1"}, {"01", "0"}, {"1", "00"}, {"+1", "0"},
		{"1e3", "0"}, {"NaN", "0"}, {"inf", "0"}, {"1.0", "0"}, {"", "0"},
		{"1", ""}, {"1", "0.5"}, {"1", "1e3"}, {"1", "NaN"}, {bootLuaSecret, "0"},
	} {
		t.Run(fmt.Sprintf("bad_clock_%d", i), func(t *testing.T) {
			r := bootLuaNewRedis()
			r.seconds, r.microseconds = test[0], test[1]
			bootLuaReject(t, r, []string{bootLuaKey}, bootLuaArgs(ApprovalInitial), ErrorInvalidNumber)
		})
	}
}

func TestBootLuaActualInfoServerIdentity(t *testing.T) {
	t.Parallel()
	id := strings.Repeat("a", 40)
	for i, info := range []string{
		"", "# Server\r\n", "other_run_id:" + id + "\r\n", "RUN_ID:" + id + "\r\n",
		"run_id:" + strings.Repeat("A", 40) + "\r\n", "run_id:" + strings.Repeat("f", 40) + "\r\n",
		"run_id:" + id + "\x00\r\n", "run_id:" + id + " \r\n", "run_id:" + id[:39] + "\r\n",
		"run_id:" + id + "\r\nrun_id:" + id + "\r\n",
		"run_id:" + id + "\r\nrun_id:" + strings.Repeat("f", 40) + "\r\n", bootLuaSecret,
	} {
		t.Run(fmt.Sprintf("invalid_info_%d", i), func(t *testing.T) {
			r := bootLuaNewRedis()
			r.overrides["INFO"] = func(*lua.LState) lua.LValue { return lua.LString(info) }
			bootLuaReject(t, r, []string{bootLuaKey}, bootLuaArgs(ApprovalInitial), ErrorBootUnapproved)
		})
	}
	for i, info := range []string{"run_id:" + id, "run_id:" + id + "\n", "# Server\r\nrun_id:" + id + "\r\n", "other_run_id:invalid\nrun_id:" + id + "\n"} {
		t.Run(fmt.Sprintf("anchored_info_%d", i), func(t *testing.T) {
			r := bootLuaNewRedis()
			r.overrides["INFO"] = func(*lua.LState) lua.LValue { return lua.LString(info) }
			bootLuaAccept(t, r, bootLuaArgs(ApprovalInitial), StatusOK, bootLuaNow)
		})
	}
}

func TestBootLuaWrongDurabilityTypes(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"string", "list", "set", "zset", "stream"} {
		for _, mode := range []ApprovalMode{ApprovalInitial, ApprovalPlanned, ApprovalUncleanRehearsal} {
			t.Run(kind+"/"+string(mode), func(t *testing.T) {
				r := bootLuaNewRedis()
				r.data[bootLuaKey] = bootLuaEntry{kind: kind, value: bootLuaSecret}
				bootLuaReject(t, r, []string{bootLuaKey}, bootLuaArgs(mode), ErrorWrongType)
				for _, call := range r.trace {
					if call.name == "HLEN" || call.name == "HMGET" || call.name == "HSTRLEN" {
						t.Fatal("wrong-type preflight attempted a hash read")
					}
				}
			})
		}
	}
}

func TestBootLuaDurabilityMissingExtraAndOversizedFields(t *testing.T) {
	t.Parallel()
	maxBytes := []int{1, 10, 40, 32, 16, 32, 64, 17, 32, 64, 16, 1}
	for index, field := range bootLuaFields {
		for _, mutation := range []string{"missing", "replacement", "extra", "oversized"} {
			t.Run(field+"/"+mutation, func(t *testing.T) {
				r, args := bootLuaPrepare(ApprovalInitial)
				r.data[bootLuaKey] = bootLuaEntry{kind: "hash", hash: bootLuaPostHash(args, bootLuaNow), expireAt: -1}
				hash := r.data[bootLuaKey].hash
				switch mutation {
				case "missing":
					delete(hash, field)
				case "replacement":
					delete(hash, field)
					// Still twelve fields, including a potentially enormous unknown
					// name/value which must never be returned by a collection read.
					hash[strings.Repeat("x", 65536)] = strings.Repeat(bootLuaSecret, 4096)
				case "extra":
					hash[bootLuaSecret] = "extra"
				case "oversized":
					hash[field] = strings.Repeat("x", maxBytes[index]+1)
				}
				if _, err := bootLuaDecodeHash(hash); err == nil {
					t.Fatal("malformed hash unexpectedly passed the Go codec")
				}
				bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorInvalidState)
				if mutation != "replacement" {
					for _, call := range r.trace {
						if call.name == "HMGET" {
							t.Fatal("unbounded/cardinality-invalid record reached bulk retrieval")
						}
						if mutation != "oversized" && call.name == "HSTRLEN" {
							t.Fatal("wrong cardinality did not fail before field reads")
						}
					}
				}
			})
		}
	}
}

func TestBootLuaDurabilityLexicalCorruptionBeforeReplay(t *testing.T) {
	t.Parallel()
	badNumbers := []string{"", "0", "00", "01", "+1", "-1", "1.0", "1e3", "NaN", "inf", "1\n", "9007199254740992", "9999999999999999", "10000000000000000"}
	badFields := map[string][]string{
		"schema_version":                   {"", "0", "2"},
		"boot_state":                       {"", "APPROVED", "invalid"},
		"approved_redis_run_id":            {"", strings.Repeat("A", 40), strings.Repeat("g", 40)},
		"boot_epoch":                       {"", strings.Repeat("A", 32), strings.Repeat("g", 32)},
		"approved_at_ms":                   badNumbers,
		"planned_shutdown_nonce":           {"x", strings.Repeat("A", 32), strings.Repeat("g", 32)},
		"planned_shutdown_evidence_sha256": {"x", strings.Repeat("A", 64), strings.Repeat("0", 64)},
		"last_approval_mode":               {"", "INITIAL", "unapproved"},
		"consumed_planned_shutdown_nonce":  {"x", strings.Repeat("A", 32), strings.Repeat("g", 32)},
		"rehearsal_evidence_sha256":        {"", strings.Repeat("A", 64), strings.Repeat("0", 64)},
		"rehearsal_at_ms":                  badNumbers,
		"acknowledged_loss_bound":          {"", "1", "00", "0.0", "-0"},
	}
	for _, mode := range []ApprovalMode{ApprovalInitial, ApprovalPlanned, ApprovalUncleanRehearsal} {
		for _, field := range bootLuaFields {
			for i, bad := range append(append([]string(nil), badFields[field]...), bootLuaSecret, "\xff", "\x00") {
				t.Run(fmt.Sprintf("%s/%s/%d", mode, field, i), func(t *testing.T) {
					r, args := bootLuaPrepare(mode)
					// Exact replay identity except this one corrupted field.
					r.data[bootLuaKey] = bootLuaEntry{kind: "hash", hash: bootLuaPostHash(args, bootLuaNow), expireAt: -1}
					r.data[bootLuaKey].hash[field] = bad
					if _, err := bootLuaDecodeHash(r.data[bootLuaKey].hash); err == nil {
						t.Fatal("lexically corrupt record unexpectedly passed Go")
					}
					bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorInvalidState)
				})
			}
		}
	}
}

func TestBootLuaFullDurabilityStateShapeAgainstGoCodec(t *testing.T) {
	t.Parallel()
	for _, state := range []BootState{BootApproved, BootPlanned, BootUnapproved} {
		for _, mode := range []ApprovalMode{ApprovalInitial, ApprovalPlanned, ApprovalUncleanRehearsal} {
			for bits := 0; bits < 8; bits++ {
				t.Run(fmt.Sprintf("%s/%s/pending_evidence_consumed_%03b", state, mode, bits), func(t *testing.T) {
					r, args := bootLuaPrepare(ApprovalUncleanRehearsal)
					hash := r.data[bootLuaKey].hash
					hash["boot_state"], hash["last_approval_mode"] = string(state), string(mode)
					for index, field := range []string{"planned_shutdown_nonce", "planned_shutdown_evidence_sha256", "consumed_planned_shutdown_nonce"} {
						hash[field] = ""
						if bits&(1<<index) != 0 {
							width := 32
							if index == 1 {
								width = 64
							}
							hash[field] = strings.Repeat("e", width)
						}
					}
					_, err := bootLuaDecodeHash(hash)
					if err == nil {
						bootLuaAccept(t, r, args, StatusOK, bootLuaNow)
					} else {
						bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorInvalidState)
					}
				})
			}
		}
	}
}

func bootLuaMeasuredSize(t *testing.T, keys, args []string) int {
	t.Helper()
	digest := sha1.Sum([]byte(bootLuaSource))
	sha := fmt.Sprintf("%x", digest)
	parts := append([]string{"EVALSHA", sha, strconv.Itoa(len(keys))}, keys...)
	parts = append(parts, args...)
	var wire bytes.Buffer
	fmt.Fprintf(&wire, "*%d\r\n", len(parts))
	for _, part := range parts {
		fmt.Fprintf(&wire, "$%d\r\n", len(part))
		wire.WriteString(part)
		wire.WriteString("\r\n")
	}
	byteKeys, byteArgs := make([][]byte, len(keys)), make([][]byte, len(args))
	for i, key := range keys {
		byteKeys[i] = []byte(key)
	}
	for i, arg := range args {
		byteArgs[i] = []byte(arg)
	}
	size, err := EvalSHASerializedSize(sha, byteKeys, byteArgs)
	if err != nil || size != uint64(wire.Len()) {
		t.Fatal("Go RESP accounting differs from measured complete command bytes")
	}
	return wire.Len()
}

func TestBootLuaMeasuredRESPCommandBound(t *testing.T) {
	t.Parallel()
	for _, delta := range []int{-1, 0, 1} {
		t.Run(fmt.Sprintf("limit%+d", delta), func(t *testing.T) {
			args := bootLuaArgs(ApprovalInitial)
			padding := bootLuaMaxBytes - 1000
			for tries := 0; tries < 3; tries++ {
				args[7] = strings.Repeat("x", padding)
				padding += bootLuaMaxBytes + delta - bootLuaMeasuredSize(t, []string{bootLuaKey}, args)
			}
			args[7] = strings.Repeat("x", padding)
			if bootLuaMeasuredSize(t, []string{bootLuaKey}, args) != bootLuaMaxBytes+delta {
				t.Fatal("test did not construct exact RESP byte boundary")
			}
			// No lexically valid BOOT is this large. At/below the size boundary,
			// the later invalid-mode check must win; one extra RESP byte must
			// reject at command admission, not after parsing the oversized input.
			code := ErrorInvalidArgument
			if delta > 0 {
				code = ErrorCommandBoundsExceeded
			}
			bootLuaReject(t, bootLuaNewRedis(), []string{bootLuaKey}, args, code)
		})
	}
	for index := -1; index < 8; index++ {
		t.Run(fmt.Sprintf("oversized_part_%d", index), func(t *testing.T) {
			keys, args := []string{bootLuaKey}, bootLuaArgs(ApprovalInitial)
			// UTF-8 byte count, not character count; payload itself is 2 MiB.
			large := strings.Repeat("é", bootLuaMaxBytes/2)
			if index < 0 {
				keys[0] = large
			} else {
				args[index] = large
			}
			if bootLuaMeasuredSize(t, keys, args) <= bootLuaMaxBytes {
				t.Fatal("oversized RESP fixture is not oversized")
			}
			bootLuaReject(t, bootLuaNewRedis(), keys, args, ErrorCommandBoundsExceeded)
		})
	}
}

func TestBootLuaRedisReplyTypesAreNotAutoStringified(t *testing.T) {
	t.Parallel()
	wrongValues := []struct {
		name  string
		value func(*lua.LState) lua.LValue
	}{
		{"nil", func(*lua.LState) lua.LValue { return lua.LNil }},
		{"false", func(*lua.LState) lua.LValue { return lua.LFalse }},
		{"true", func(*lua.LState) lua.LValue { return lua.LTrue }},
		{"integer", func(*lua.LState) lua.LValue { return lua.LNumber(12) }},
		{"string", func(*lua.LState) lua.LValue { return lua.LString("12") }},
		{"status", func(L *lua.LState) lua.LValue { return bootLuaStatus(L, "hash") }},
	}
	for _, command := range []string{"TIME", "INFO", "HLEN", "HSTRLEN", "HMGET"} {
		for _, wrong := range wrongValues {
			if (command == "HLEN" || command == "HSTRLEN") && wrong.name == "integer" {
				continue // These commands actually return integers to Lua.
			}
			t.Run(command+"/"+wrong.name, func(t *testing.T) {
				r, args := bootLuaPrepare(ApprovalUncleanRehearsal)
				r.overrides[command] = wrong.value
				code := ErrorInvalidState
				if command == "TIME" {
					code = ErrorInvalidNumber
				} else if command == "INFO" {
					code = ErrorBootUnapproved
				}
				bootLuaReject(t, r, []string{bootLuaKey}, args, code)
			})
		}
	}
	for i, value := range []float64{-1, 1.5, math.Inf(1), math.NaN()} {
		t.Run(fmt.Sprintf("HSTRLEN/bad_number_%d", i), func(t *testing.T) {
			r, args := bootLuaPrepare(ApprovalUncleanRehearsal)
			r.overrides["HSTRLEN"] = func(*lua.LState) lua.LValue { return lua.LNumber(value) }
			bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorInvalidState)
		})
	}
	for i, makeReply := range []func(*lua.LState) lua.LValue{
		func(*lua.LState) lua.LValue { return lua.LString("hash") },
		func(*lua.LState) lua.LValue { return lua.LFalse },
		func(L *lua.LState) lua.LValue { return bootLuaArray(L, lua.LString("hash")) },
		func(L *lua.LState) lua.LValue {
			reply := L.NewTable()
			reply.RawSetString("ok", lua.LNumber(1))
			return reply
		},
	} {
		t.Run(fmt.Sprintf("TYPE/non_status_%d", i), func(t *testing.T) {
			r, args := bootLuaPrepare(ApprovalUncleanRehearsal)
			r.overrides["TYPE"] = makeReply
			bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorInvalidState)
		})
	}
	for _, index := range []int{1, 2} {
		for _, wrong := range wrongValues {
			if wrong.name == "string" {
				continue // TIME's two scalar values really are bulk strings.
			}
			t.Run(fmt.Sprintf("TIME/element_%d/%s", index, wrong.name), func(t *testing.T) {
				r := bootLuaNewRedis()
				r.overrides["TIME"] = func(L *lua.LState) lua.LValue {
					reply := bootLuaArray(L, lua.LString(r.seconds), lua.LString(r.microseconds))
					reply.RawSetInt(index, wrong.value(L))
					return reply
				}
				bootLuaReject(t, r, []string{bootLuaKey}, bootLuaArgs(ApprovalInitial), ErrorInvalidNumber)
			})
		}
	}
	for _, count := range []int{0, 1, 3} {
		t.Run(fmt.Sprintf("TIME/arity_%d", count), func(t *testing.T) {
			r := bootLuaNewRedis()
			r.overrides["TIME"] = func(L *lua.LState) lua.LValue {
				return bootLuaStrings(L, []string{r.seconds, r.microseconds, "0"}[:count])
			}
			bootLuaReject(t, r, []string{bootLuaKey}, bootLuaArgs(ApprovalInitial), ErrorInvalidNumber)
		})
	}
	for fieldIndex, field := range bootLuaFields {
		for _, wrong := range wrongValues {
			if wrong.name == "string" {
				continue
			}
			t.Run("HMGET/"+field+"/"+wrong.name, func(t *testing.T) {
				r, args := bootLuaPrepare(ApprovalUncleanRehearsal)
				r.overrides["HMGET"] = func(L *lua.LState) lua.LValue {
					values := make([]string, 12)
					for i, name := range bootLuaFields {
						values[i] = r.data[bootLuaKey].hash[name]
					}
					reply := bootLuaStrings(L, values)
					reply.RawSetInt(fieldIndex+1, wrong.value(L))
					return reply
				}
				bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorInvalidState)
			})
		}
	}
	// Prove that the facade itself cannot launder non-bulk Lua output into a Go
	// success envelope. These are response conversion tests, not alternate Lua.
	L := lua.NewState()
	defer L.Close()
	L.SetContext(luaTestContext(t))
	for _, wrong := range wrongValues {
		if wrong.name == "string" {
			continue
		}
		t.Run("response_oracle/"+wrong.name, func(t *testing.T) {
			reply := bootLuaArray(L, lua.LString("OK"), wrong.value(L), lua.LString(strings.Repeat("b", 32)))
			err := ValidateOperationResponse(OperationApproveBoot, bootLuaRESP(reply))
			if !errors.Is(err, ErrResponseScalarType) && !(wrong.name == "nil" && errors.Is(err, ErrResponseArity)) {
				t.Fatalf("non-bulk response scalar was laundered or misclassified: %v", err)
			}
		})
	}
}

func TestBootLuaACLPreflightAndPrebuiltMutation(t *testing.T) {
	t.Parallel()
	for _, mode := range []ApprovalMode{ApprovalInitial, ApprovalPlanned, ApprovalUncleanRehearsal} {
		t.Run(string(mode)+"/denied", func(t *testing.T) {
			r, args := bootLuaPrepare(mode)
			r.aclAllowed = false
			bootLuaReject(t, r, []string{bootLuaKey}, args, ErrorBootUnapproved)
			if len(r.trace) == 0 || !r.trace[len(r.trace)-1].acl || r.prebuiltReply == nil {
				t.Fatal("ACL rejection was not made against the prebuilt write/response")
			}
		})
		for _, reply := range []lua.LNumber{0, 12, 987654} {
			t.Run(fmt.Sprintf("%s/discard_write_reply_%d", mode, int(reply)), func(t *testing.T) {
				r, args := bootLuaPrepare(mode)
				// HSET replies are native integers. Changing the returned count
				// cannot affect the already built bulk-string response or writes.
				r.overrides["HSET"] = func(*lua.LState) lua.LValue { return reply }
				bootLuaAccept(t, r, args, StatusOK, bootLuaNow)
			})
		}
	}
}

func TestBootLuaUnexpectedWriteFailureIsNotRollbackOrValidation(t *testing.T) {
	t.Parallel()
	for _, mode := range []ApprovalMode{ApprovalInitial, ApprovalPlanned, ApprovalUncleanRehearsal} {
		for _, failure := range []string{"before", "after"} {
			t.Run(string(mode)+"/"+failure, func(t *testing.T) {
				r, args := bootLuaPrepare(mode)
				r.writeFailure = failure
				before := r.snapshot()
				result := bootLuaRun(t, r, []string{bootLuaKey}, args)
				if result.runtimeErr == nil || !strings.Contains(result.runtimeErr.Error(), bootLuaWriteErr) || result.raw != nil {
					t.Fatal("unexpected datastore failure was suppressed or reclassified as a protocol reply")
				}
				if strings.Contains(result.runtimeErr.Error(), "ERR CRAWL_V2_") {
					t.Fatal("datastore incident was converted into an expected validation error")
				}
				if failure == "before" {
					if !reflect.DeepEqual(before, r.snapshot()) {
						t.Fatal("before-application failure unexpectedly applied data")
					}
				} else {
					if reflect.DeepEqual(before, r.snapshot()) || r.writes != before.writes+1 {
						t.Fatal("test pretended Redis rolls back an already applied mutation")
					}
					bootLuaAssertPost(t, r, args, bootLuaNow)
					// The invocation above is still an integrity incident, not a
					// successful result. Inspecting post-state in this isolated
					// test can reconcile identity; it grants no operational gate.
					r.writeFailure = ""
					r.setTime(bootLuaNow + 1)
					bootLuaAccept(t, r, args, StatusExistsIdentical, bootLuaNow+1)
				}
			})
		}
	}
}

func TestBootLuaMutationTailIsOneDiscardedCallThenPrebuiltReturn(t *testing.T) {
	t.Parallel()
	statements, err := parse.Parse(strings.NewReader(bootLuaSource), "cj2_approve_boot.lua")
	if err != nil || len(statements) < 2 {
		t.Fatal("cannot parse actual proposed Lua for mutation-tail review")
	}
	// AST checks, not a comment/search assertion: no allocation, expression
	// evaluation, concatenation, hashing, table growth, or reply-dependent branch
	// can occur after the final fixed call. Dynamic facade checks above prove the
	// descriptor AND the returned table exist at ACL preflight before this call.
	callStatement, ok := statements[len(statements)-2].(*ast.FuncCallStmt)
	if !ok {
		t.Fatal("mutation reply is not discarded")
	}
	call, ok := callStatement.Expr.(*ast.FuncCallExpr)
	if !ok || len(call.Args) != 1 {
		t.Fatal("mutation is not one bounded prebuilt call")
	}
	function, ok := call.Func.(*ast.AttrGetExpr)
	if !ok || !bootLuaASTIdent(function.Object, "redis") {
		t.Fatal("mutation is not a Redis call")
	}
	method, ok := function.Key.(*ast.StringExpr)
	if !ok || method.Value != "call" {
		t.Fatal("unexpected write error can be swallowed by mutation call")
	}
	unpack, ok := call.Args[0].(*ast.FuncCallExpr)
	if !ok || !bootLuaASTIdent(unpack.Func, "unpack") || len(unpack.Args) != 3 || !bootLuaASTIdent(unpack.Args[0], "write") {
		t.Fatal("mutation does not unpack the prebuilt descriptor")
	}
	start, startOK := unpack.Args[1].(*ast.NumberExpr)
	end, endOK := unpack.Args[2].(*ast.NumberExpr)
	if !startOK || !endOK || start.Value != "1" || end.Value != "26" {
		t.Fatal("mutation unpack range is not fixed to twenty-six prebuilt bulk strings")
	}
	ret, ok := statements[len(statements)-1].(*ast.ReturnStmt)
	if !ok || len(ret.Exprs) != 1 || !bootLuaASTIdent(ret.Exprs[0], "response") {
		t.Fatal("post-write code does more than return the prebuilt response")
	}
}

func bootLuaASTIdent(expression ast.Expr, name string) bool {
	identifier, ok := expression.(*ast.IdentExpr)
	return ok && identifier.Value == name
}
