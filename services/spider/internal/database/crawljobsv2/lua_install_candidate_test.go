package crawljobsv2

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/ast"
	"github.com/yuin/gopher-lua/parse"
)

// No private fake ScriptBindingSet and no production authority constructor.
//
//go:embed lua/cj2_install_candidate_markers.lua
var installLuaSource string

var installLuaMarkerFields = strings.Fields(`manifest_version manifest_sha256 crawl_jobs crawl_policy canonicalization page_publication image_manifest backlink_projection render_ipc signal_queue global_request_concurrency redis_config_sha256 commit_guard_sha256 spider_image seed_importer_image crawl_admin_image indexer_image image_indexer_image backlinks_processor_image monitoring_image render_worker_image`)

func installLuaWire(t *testing.T) ([]string, []string) {
	t.Helper()
	artifacts := newGateArtifacts(t)
	gate, err := NewTransportGate(OperationInstallCandidateMarkers, TransportGateInput{Mode: GateBootOnly, BootEpoch: bootLuaArgs(ApprovalInitial)[1]})
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewInstallCandidateMarkersWireRequest(gate, InstallCandidateMarkersWireInput{
		FreezeNonce: artifacts.freeze.FreezeNonce(), ProcessStopEvidenceSHA256: Digest(strings.Repeat("a", 64)),
		ContractSHA256: artifacts.contract, Compatibility: artifacts.marker,
	})
	if err != nil {
		t.Fatal(err)
	}
	k, a, records, err := request.validatedWireParts()
	if err != nil || records != 0 {
		t.Fatalf("wire oracle error %v records=%d", err, records)
	}
	keys, args := make([]string, len(k)), make([]string, len(a))
	for i := range k {
		keys[i] = string(k[i])
	}
	for i := range a {
		args[i] = string(a[i])
	}
	wantKeys := []string{DurabilityKey, ContractsActiveKey, CrawlContractKey, ContractsCandidateKey, CrawlContractCandidateKey, CommitGuardKey, LegacyRetirementKey, AdminFreezeKey}
	if len(args) != 31 || !reflect.DeepEqual(keys, wantKeys) || !reflect.DeepEqual(installLuaMarkerFields, compatibilityMarkerFieldNames()) {
		t.Fatal("INSTALL oracle key/scalar/marker shape drift")
	}
	return keys, args
}

func installLuaRecords(args []string, at uint64) (Record, Record) {
	marker := make(Record, 21)
	for i, name := range installLuaMarkerFields {
		marker[i] = textField(name, args[10+i])
	}
	freeze := Record{textField("protocol_version", "2"), textField("freeze_nonce", args[7]), textField("process_stop_evidence_sha256", args[8]), textField("candidate_manifest_sha256", args[11]), textField("candidate_contract_sha256", args[9]), textField("created_at_ms", strconv.FormatUint(at, 10))}
	return marker, freeze
}

func installLuaSeed(r *sharedLuaRedis, args []string, at uint64) {
	marker, freeze := installLuaRecords(args, at)
	r.setHash(ContractsCandidateKey, marker)
	r.setHash(AdminFreezeKey, freeze)
	r.data[CrawlContractCandidateKey] = bootLuaEntry{kind: "string", value: args[9], expireAt: -1}
}

func installLuaPost(t *testing.T, r *sharedLuaRedis, args []string, at uint64) {
	t.Helper()
	marker, freeze := installLuaRecords(args, at)
	for _, test := range []struct {
		key    string
		fields Record
		schema RecordSchema
	}{{ContractsCandidateKey, marker, SchemaCompatibilityMarker}, {AdminFreezeKey, freeze, SchemaAdminFreeze}} {
		entry := r.data[test.key]
		actual := make(Record, len(test.fields))
		if entry.kind != "hash" || len(entry.hash) != len(test.fields) {
			t.Fatal("wrong post-state shape")
		}
		for i, field := range test.fields {
			value, found := entry.hash[field.Name]
			if !found || value != string(field.Value) {
				t.Fatalf("wrong post-state %s field %s", test.schema, field.Name)
			}
			actual[i] = textField(field.Name, value)
		}
		if err := ValidateRecord(test.schema, actual); err != nil {
			t.Fatalf("Go semantic codec rejected actual Lua post-state: %v", err)
		}
	}
	if value := r.data[CrawlContractCandidateKey]; value.kind != "string" || value.value != args[9] {
		t.Fatal("wrong candidate contract")
	}
}

func installLuaAccept(t *testing.T, r *sharedLuaRedis, keys, args []string, status Status) {
	t.Helper()
	before := r.snapshot()
	got := sharedLuaNoError(t, sharedLuaRun(t, r, installLuaSource, keys, args))
	if err := ValidateOperationResponse(OperationInstallCandidateMarkers, got); err != nil {
		t.Fatalf("Go response oracle: %v", err)
	}
	want := []any{string(status), strconv.FormatUint(r.now, 10), args[11], args[9]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actual response %v want %v", got, want)
	}
	if status == StatusExistsIdentical {
		if !reflect.DeepEqual(before, r.snapshot()) {
			t.Fatal("replay wrote data/expiry/counter")
		}
		for _, call := range r.trace {
			if call.acl || call.name == "INFO" && call.args[0] == "MEMORY" || len(call.args) > 0 && call.args[0] == StageSlotsKey {
				t.Fatal("identical replay entered write/memory admission")
			}
		}
		sharedLuaAssertTrace(t, r, 0)
	} else {
		if r.writes != before.writes+3 || !r.returnedPrebuilt {
			t.Fatal("INSTALL did not perform three prebuilt writes and return prebuilt reply")
		}
		installLuaPost(t, r, args, r.now)
		after := r.snapshot()
		for _, key := range []string{ContractsCandidateKey, CrawlContractCandidateKey, AdminFreezeKey} {
			delete(before.data, key)
			delete(after.data, key)
		}
		if !reflect.DeepEqual(before.data, after.data) {
			t.Fatal("INSTALL changed unrelated state")
		}
		sharedLuaAssertTrace(t, r, 3)
	}
	allowed := map[string]bool{StageSlotsKey: true}
	for _, key := range keys {
		allowed[key] = true
	}
	for _, call := range r.trace {
		if call.name != "INFO" && call.name != "TIME" && !allowed[call.args[0]] {
			t.Fatal("INSTALL inspected legacy/unrelated key")
		}
	}
}

func installLuaReject(t *testing.T, r *sharedLuaRedis, keys, args []string, code ErrorCode) {
	t.Helper()
	before := r.snapshot()
	result := sharedLuaRun(t, r, installLuaSource, keys, args)
	if result.runtimeErr != nil {
		t.Fatalf("expected closed preflight error, got runtime failure: %v", result.runtimeErr)
	}
	if !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("rejection wrote data/expiry/canaries")
	}
	text, ok := result.raw.(bootLuaErrorReply)
	if !ok || !strings.HasPrefix(string(text), "ERR CRAWL_V2_") {
		t.Fatalf("not a closed error: %v", result.raw)
	}
	parsed, err := ParseErrorCode(strings.TrimPrefix(string(text), "ERR CRAWL_V2_"))
	if err != nil || code != "" && code != parsed || strings.Contains(string(text), bootLuaSecret) {
		t.Fatalf("code got %q want %q", parsed, code)
	}
	sharedLuaAssertTrace(t, r, 0)
}

func TestInstallLuaCompleteVerticalSliceAndReplay(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	r := sharedLuaNewRedis()
	installLuaAccept(t, r, keys, args, StatusCandidateInstalled)
	r.now += 10000
	r.denyAt = 1
	r.maximum = 1
	// Exact replay must preserve creation time and even unusual physical expiry
	// metadata, and must not inspect/admit a new allocation or repair stage_slots.
	entry := r.data[AdminFreezeKey]
	entry.expireAt = int64(r.now + 60000)
	r.data[AdminFreezeKey] = entry
	r.data[StageSlotsKey] = bootLuaEntry{kind: "string", value: bootLuaSecret}
	installLuaAccept(t, r, keys, args, StatusExistsIdentical)
	installLuaPost(t, r, args, bootLuaNow)
}

func TestInstallLuaValidChangedArtifactAndEnabledRender(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	// Change each mutable artifact field and correctly rehash the complete Go
	// artifact. A new valid artifact is still NOT the old installation's replay.
	for _, index := range []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30} {
		changed := append([]string(nil), args...)
		changed[index] = strings.Repeat("f", 64)
		if index >= 23 {
			changed[index] = "sha256:" + changed[index]
		}
		marker, _ := installLuaRecords(changed, bootLuaNow)
		artifact := append(Record{marker[0]}, marker[2:]...)
		changed[11] = string(plainSHA256(primitiveLuaEncoded(t, artifact)))
		marker, _ = installLuaRecords(changed, bootLuaNow)
		if err := ValidateRecord(SchemaCompatibilityMarker, marker); err != nil {
			t.Fatal(err)
		}
		r := sharedLuaNewRedis()
		installLuaSeed(r, args, bootLuaNow)
		installLuaReject(t, r, keys, changed, ErrorImmutableMismatch)
		r = sharedLuaNewRedis()
		installLuaAccept(t, r, keys, changed, StatusCandidateInstalled)
		installLuaAccept(t, r, keys, changed, StatusExistsIdentical)
	}
}

func TestInstallLuaClosedInputAndMalformedWire(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprintf("key_%d", i), func(t *testing.T) {
			changed := append([]string(nil), keys...)
			changed[i] += "bad"
			installLuaReject(t, sharedLuaNewRedis(), changed, args, ErrorInvalidArgument)
		})
	}
	for _, count := range []int{0, 1, 7, 9} {
		changed := append(append([]string(nil), keys...), "extra")
		installLuaReject(t, sharedLuaNewRedis(), changed[:count], args, ErrorInvalidArgument)
	}
	for count := 0; count < 35; count++ {
		if count == 31 {
			continue
		}
		changed := append(append([]string(nil), args...), "extra", "extra", "extra", "extra")
		installLuaReject(t, sharedLuaNewRedis(), keys, changed[:count], ErrorInvalidArgument)
	}
	for _, mode := range []string{"active", "candidate", "BOOT_ONLY", "", "boot_only\x00"} {
		changed := append([]string(nil), args...)
		changed[0] = mode
		installLuaReject(t, sharedLuaNewRedis(), keys, changed, ErrorInvalidArgument)
	}
	for i := 2; i < 7; i++ {
		for _, bad := range []string{"x", string(make([]byte, 8)), args[11]} {
			changed := append([]string(nil), args...)
			changed[i] = bad
			installLuaReject(t, sharedLuaNewRedis(), keys, changed, ErrorInvalidArgument)
		}
	}
	for _, i := range []int{1, 7, 8, 9} {
		for _, bad := range []string{"", strings.ToUpper(args[i]), args[i] + "0", args[i][:len(args[i])-1], strings.Repeat("g", len(args[i]))} {
			changed := append([]string(nil), args...)
			changed[i] = bad
			installLuaReject(t, sharedLuaNewRedis(), keys, changed, ErrorInvalidIdentifier)
		}
	}
	for _, i := range []int{8, 9} {
		changed := append([]string(nil), args...)
		changed[i] = ZeroSHA256
		installLuaReject(t, sharedLuaNewRedis(), keys, changed, ErrorInvalidArgument)
	}
	for i, name := range installLuaMarkerFields {
		t.Run(name, func(t *testing.T) {
			for _, bad := range []string{"", bootLuaSecret, "\xff", args[10+i] + "\x00"} {
				changed := append([]string(nil), args...)
				changed[10+i] = bad
				marker, _ := installLuaRecords(changed, bootLuaNow)
				if err := ValidateRecord(SchemaCompatibilityMarker, marker); err == nil {
					t.Fatal("bad fixture passed Go")
				}
				installLuaReject(t, sharedLuaNewRedis(), keys, changed, "")
			}
		})
	}
	for _, i := range []int{11, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30} {
		changed := append([]string(nil), args...)
		changed[i] = ZeroSHA256
		if i >= 23 {
			changed[i] = "sha256:" + ZeroSHA256
		}
		installLuaReject(t, sharedLuaNewRedis(), keys, changed, ErrorInvalidArgument)
	}
}

func TestInstallLuaCompleteStateNotPartialRepair(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for mask := 1; mask < 7; mask++ {
		t.Run(fmt.Sprintf("partial_%03b", mask), func(t *testing.T) {
			r := sharedLuaNewRedis()
			installLuaSeed(r, args, bootLuaNow)
			for i, key := range []string{ContractsCandidateKey, CrawlContractCandidateKey, AdminFreezeKey} {
				if mask&(1<<i) == 0 {
					delete(r.data, key)
				}
			}
			installLuaReject(t, r, keys, args, ErrorImmutableMismatch)
		})
	}
	for _, key := range []string{ContractsActiveKey, CrawlContractKey, CommitGuardKey, LegacyRetirementKey} {
		for _, replay := range []bool{false, true} {
			r := sharedLuaNewRedis()
			if replay {
				installLuaSeed(r, args, bootLuaNow)
			}
			kind := "hash"
			if key == CrawlContractKey {
				kind = "string"
			}
			r.data[key] = bootLuaEntry{kind: kind, value: args[9], hash: map[string]string{"private": bootLuaSecret}}
			installLuaReject(t, r, keys, args, ErrorInvalidState)
		}
	}
	for _, key := range keys {
		for _, kind := range []string{"list", "set", "zset", "stream"} {
			r := sharedLuaNewRedis()
			r.data[key] = bootLuaEntry{kind: kind, value: bootLuaSecret}
			installLuaReject(t, r, keys, args, ErrorWrongType)
		}
	}
	for _, index := range []int{7, 8, 9} {
		r := sharedLuaNewRedis()
		installLuaSeed(r, args, bootLuaNow)
		changed := append([]string(nil), args...)
		changed[index] = strings.Repeat("f", len(args[index]))
		installLuaReject(t, r, keys, changed, ErrorImmutableMismatch)
	}
	for _, field := range []string{"freeze_nonce", "process_stop_evidence_sha256", "candidate_manifest_sha256", "candidate_contract_sha256"} {
		r := sharedLuaNewRedis()
		installLuaSeed(r, args, bootLuaNow)
		r.data[AdminFreezeKey].hash[field] = strings.Repeat("f", len(r.data[AdminFreezeKey].hash[field]))
		installLuaReject(t, r, keys, args, ErrorImmutableMismatch)
	}
	r := sharedLuaNewRedis()
	installLuaSeed(r, args, bootLuaNow)
	r.data[CrawlContractCandidateKey] = bootLuaEntry{kind: "string", value: strings.Repeat("f", 64)}
	installLuaReject(t, r, keys, args, ErrorImmutableMismatch)
}

func TestInstallLuaStoredHashesCompleteBoundedAndGoValidated(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for _, key := range []string{DurabilityKey, ContractsCandidateKey, AdminFreezeKey} {
		seed := sharedLuaNewRedis()
		installLuaSeed(seed, args, bootLuaNow)
		for field, value := range seed.data[key].hash {
			for _, mutation := range []string{"missing", "unknown_replacement", "oversized", "invalid"} {
				t.Run(key+"/"+field+"/"+mutation, func(t *testing.T) {
					r := sharedLuaNewRedis()
					installLuaSeed(r, args, bootLuaNow)
					hash := r.data[key].hash
					switch mutation {
					case "missing":
						delete(hash, field)
					case "unknown_replacement":
						delete(hash, field)
						hash[strings.Repeat("private", 10000)] = strings.Repeat(bootLuaSecret, 10000)
					case "oversized":
						hash[field] = strings.Repeat("x", 1000)
					case "invalid":
						hash[field] = "!"
						if value == "!" {
							t.Fatal("invalid fixture")
						}
					}
					installLuaReject(t, r, keys, args, ErrorInvalidState)
					for _, call := range r.trace {
						if (mutation == "missing" || mutation == "oversized") && call.name == "HMGET" && call.args[0] == key {
							t.Fatal("bad cardinality/oversized value reached bulk read")
						}
					}
				})
			}
		}
	}
	for _, value := range []string{"", strings.Repeat("x", 65), ZeroSHA256, strings.Repeat("A", 64)} {
		r := sharedLuaNewRedis()
		installLuaSeed(r, args, bootLuaNow)
		r.data[CrawlContractCandidateKey] = bootLuaEntry{kind: "string", value: value}
		installLuaReject(t, r, keys, args, ErrorInvalidState)
	}
}

func TestInstallLuaBootActualProcessEpochAndFullShape(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for _, replay := range []bool{false, true} {
		for _, mutation := range []string{"missing", "process", "epoch", "planned", "unapproved", "planned_relation", "zero_evidence"} {
			r := sharedLuaNewRedis()
			if replay {
				installLuaSeed(r, args, bootLuaNow)
			}
			code := ErrorBootUnapproved
			switch mutation {
			case "missing":
				delete(r.data, DurabilityKey)
			case "process":
				r.runID = strings.Repeat("f", 40)
			case "epoch":
				r.data[DurabilityKey].hash["boot_epoch"] = strings.Repeat("f", 32)
			case "planned":
				h := r.data[DurabilityKey].hash
				h["boot_state"] = "planned"
				h["planned_shutdown_nonce"] = strings.Repeat("f", 32)
				h["planned_shutdown_evidence_sha256"] = strings.Repeat("f", 64)
			case "unapproved":
				r.data[DurabilityKey].hash["boot_state"] = "unapproved"
			case "planned_relation":
				r.data[DurabilityKey].hash["last_approval_mode"] = "planned"
				code = ErrorInvalidState
			case "zero_evidence":
				r.data[DurabilityKey].hash["rehearsal_evidence_sha256"] = ZeroSHA256
				code = ErrorInvalidState
			}
			installLuaReject(t, r, keys, args, code)
		}
	}
	for _, info := range []string{"", "run_id:" + strings.Repeat("a", 40) + "\nrun_id:" + strings.Repeat("a", 40), "other_run_id:" + strings.Repeat("a", 40), "run_id:" + strings.Repeat("A", 40)} {
		r := sharedLuaNewRedis()
		r.override = func(_ *lua.LState, name string, a []string) lua.LValue {
			if name == "INFO" && a[0] == "SERVER" {
				return lua.LString(info)
			}
			return nil
		}
		installLuaReject(t, r, keys, args, ErrorBootUnapproved)
	}
}

func installLuaGrowth(args []string, at uint64) uint64 {
	marker, freeze := installLuaRecords(args, at)
	logical := len(ContractsCandidateKey) + len(CrawlContractCandidateKey) + len(AdminFreezeKey) + len(args[9])
	for _, record := range []Record{marker, freeze} {
		for _, field := range record {
			logical += len(field.Name) + len(field.Value)
		}
	}
	return uint64(3*logical + 3*1024 + 27*256)
}

func TestInstallLuaMemoryExactBoundaryAndSlots(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for slots := 0; slots <= 4; slots++ {
		for _, delta := range []int64{-1, 0, 1} {
			t.Run(fmt.Sprintf("slots_%d_delta_%d", slots, delta), func(t *testing.T) {
				r := sharedLuaNewRedis()
				sum := uint64(0)
				if slots > 0 {
					hash := map[string]string{}
					for i := 0; i < slots; i++ {
						remaining := []uint64{0, 1, 32768, 50331648}[i]
						sum += remaining
						hash[strings.Repeat(strconv.Itoa(i+1), 64)] = fmt.Sprintf("%d:%s:%s:9007199254740991:%d", remaining, strings.Repeat("2", 32), strings.Repeat("3", 64), i)
					}
					r.data[StageSlotsKey] = bootLuaEntry{kind: "hash", hash: hash}
				}
				r.maximum = uint64(int64(r.used+sum+67108864+16777216+installLuaGrowth(args, r.now)) + delta)
				if delta < 0 {
					installLuaReject(t, r, keys, args, ErrorMemoryHeadroomLow)
				} else {
					installLuaAccept(t, r, keys, args, StatusCandidateInstalled)
				}
			})
		}
	}
	for _, maximum := range []uint64{0, 1, 67108864, 9007199254740992} {
		r := sharedLuaNewRedis()
		r.maximum = maximum
		installLuaReject(t, r, keys, args, ErrorMemoryHeadroomLow)
	}
	r := sharedLuaNewRedis()
	r.used = 9007199254740991
	r.maximum = r.used
	installLuaReject(t, r, keys, args, ErrorMemoryHeadroomLow)
	for _, memory := range []string{"", "used_memory:1\nmaxmemory:0", "used_memory:01\nmaxmemory:999999999", "used_memory:1e3\nmaxmemory:999999999", "used_memory:1\nused_memory:1\nmaxmemory:999999999", "used_memory:1\nmaxmemory:999999999\nmaxmemory:999999999"} {
		r := sharedLuaNewRedis()
		r.override = func(_ *lua.LState, name string, a []string) lua.LValue {
			if name == "INFO" && a[0] == "MEMORY" {
				return lua.LString(memory)
			}
			return nil
		}
		installLuaReject(t, r, keys, args, ErrorMemoryHeadroomLow)
	}
}

func TestInstallLuaRejectsMalformedSlotsBeforeWrites(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	base := []string{"50331648", strings.Repeat("a", 32), strings.Repeat("b", 64), "1", "0"}
	for index := range base {
		for _, bad := range []string{"", "01", "-1", "9007199254740992", bootLuaSecret} {
			value := append([]string(nil), base...)
			value[index] = bad
			r := sharedLuaNewRedis()
			r.data[StageSlotsKey] = bootLuaEntry{kind: "hash", hash: map[string]string{strings.Repeat("c", 64): strings.Join(value, ":")}}
			installLuaReject(t, r, keys, args, ErrorInvalidState)
		}
	}
	for _, value := range []string{strings.Join(base, ":") + ":extra", "50331649:" + strings.Join(base[1:], ":"), strings.Join(base[:3], ":") + ":0:0", strings.Join(base[:4], ":") + ":74"} {
		r := sharedLuaNewRedis()
		r.data[StageSlotsKey] = bootLuaEntry{kind: "hash", hash: map[string]string{strings.Repeat("c", 64): value}}
		installLuaReject(t, r, keys, args, ErrorInvalidState)
	}
	for _, key := range []string{"", strings.Repeat("A", 64), strings.Repeat("c", 63), strings.Repeat("huge", 20000)} {
		r := sharedLuaNewRedis()
		r.data[StageSlotsKey] = bootLuaEntry{kind: "hash", hash: map[string]string{key: strings.Join(base, ":")}}
		installLuaReject(t, r, keys, args, ErrorInvalidState)
	}
	r := sharedLuaNewRedis()
	hash := map[string]string{}
	for i := 0; i < 5; i++ {
		hash[strings.Repeat(strconv.Itoa(i), 64)] = strings.Join(base, ":")
	}
	r.data[StageSlotsKey] = bootLuaEntry{kind: "hash", hash: hash}
	installLuaReject(t, r, keys, args, ErrorInvalidState)
	for _, call := range r.trace {
		if call.name == "HKEYS" {
			t.Fatal("five-slot hash enumerated before count rejection")
		}
	}
	r = sharedLuaNewRedis()
	r.data[StageSlotsKey] = bootLuaEntry{kind: "string", value: bootLuaSecret}
	installLuaReject(t, r, keys, args, ErrorWrongType)
}

func TestInstallLuaACLAllThreeWritesBeforeMutation(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for denied := 1; denied <= 3; denied++ {
		t.Run(strconv.Itoa(denied), func(t *testing.T) {
			r := sharedLuaNewRedis()
			r.denyAt = denied
			installLuaReject(t, r, keys, args, ErrorBootUnapproved)
			if r.aclCount != denied || r.prebuilt == nil || r.attempts != 0 {
				t.Fatal("ACL failure not before all writes with prebuilt execution")
			}
		})
	}
	for _, reply := range []lua.LValue{lua.LNumber(-1), lua.LFalse, lua.LString("changed result")} {
		r := sharedLuaNewRedis()
		r.override = func(_ *lua.LState, name string, _ []string) lua.LValue {
			if name == "SET" || name == "HSET" {
				return reply
			}
			return nil
		}
		installLuaAccept(t, r, keys, args, StatusCandidateInstalled)
	}
}

func TestInstallLuaUnexpectedWriteErrorKeepsEarlierWrites(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for failed := 1; failed <= 3; failed++ {
		for _, after := range []bool{false, true} {
			r := sharedLuaNewRedis()
			r.failAt, r.failAfter = failed, after
			before := r.snapshot()
			result := sharedLuaRun(t, r, installLuaSource, keys, args)
			if result.runtimeErr == nil || !strings.Contains(result.runtimeErr.Error(), bootLuaWriteErr) || strings.Contains(result.runtimeErr.Error(), "CRAWL_V2_") || result.raw != nil {
				t.Fatal("unexpected write error suppressed/reclassified")
			}
			applied := failed - 1
			if after {
				applied++
			}
			if r.writes != applied {
				t.Fatal("test incorrectly modeled rollback")
			}
			ordered := []string{ContractsCandidateKey, CrawlContractCandidateKey, AdminFreezeKey}
			for i, key := range ordered {
				_, exists := r.data[key]
				if exists != (i < applied) {
					t.Fatal("earlier mutation was lost or later write occurred")
				}
			}
			got := r.snapshot()
			for _, key := range ordered {
				delete(before.data, key)
				delete(got.data, key)
			}
			if !reflect.DeepEqual(before.data, got.data) {
				t.Fatal("write error changed unrelated canaries")
			}
			sharedLuaAssertTrace(t, r, 3)
			if applied > 0 && applied < 3 {
				r.failAt = 0
				installLuaReject(t, r, keys, args, ErrorImmutableMismatch)
			}
		}
	}
}

func TestInstallLuaRESPBoundsAndWrongReplyShapes(t *testing.T) {
	t.Parallel()
	keys, args := installLuaWire(t)
	for _, delta := range []int{-1, 0, 1} {
		changed := append([]string(nil), args...)
		padding := 2096000
		for tries := 0; tries < 4; tries++ {
			changed[0] = strings.Repeat("x", padding)
			padding += 2097152 + delta - bootLuaMeasuredSize(t, keys, changed)
		}
		changed[0] = strings.Repeat("x", padding)
		if bootLuaMeasuredSize(t, keys, changed) != 2097152+delta {
			t.Fatal("bad measured RESP fixture")
		}
		code := ErrorInvalidArgument
		if delta > 0 {
			code = ErrorCommandBoundsExceeded
		}
		installLuaReject(t, sharedLuaNewRedis(), keys, changed, code)
	}
	for _, command := range []string{"TIME", "TYPE", "HLEN", "HSTRLEN", "HMGET", "STRLEN", "GET"} {
		t.Run(command, func(t *testing.T) {
			r := sharedLuaNewRedis()
			installLuaSeed(r, args, bootLuaNow)
			r.override = func(_ *lua.LState, name string, _ []string) lua.LValue {
				if name == command {
					return lua.LFalse
				}
				return nil
			}
			installLuaReject(t, r, keys, args, "")
		})
	}
	// Full 12-field HMGET with a missing empty value is NOT the valid empty bulk.
	r := sharedLuaNewRedis()
	r.override = func(L *lua.LState, name string, a []string) lua.LValue {
		if name != "HMGET" || a[0] != DurabilityKey {
			return nil
		}
		v := make([]lua.LValue, 12)
		for i, f := range bootLuaFields {
			v[i] = lua.LString(r.data[DurabilityKey].hash[f])
		}
		v[5] = lua.LFalse
		return bootLuaArray(L, v...)
	}
	installLuaReject(t, r, keys, args, ErrorInvalidState)
}

func TestInstallLuaCanonicalRecipeAndExecutorAST(t *testing.T) {
	t.Parallel()
	root := filepath.Clean(filepath.Join("..", "..", "..", "..", ".."))
	script := filepath.Join(root, "scripts", "generate-crawl-jobs-v2-lua.py")
	before := sha256.Sum256([]byte(bootLuaSource))
	check := exec.Command("python3", script, "--check")
	out, err := check.CombinedOutput()
	if err != nil {
		t.Fatalf("assembler --check: %v %s", err, out)
	}
	if !bytes.Contains(out, []byte("COMPLETE SOURCE INVENTORY: 43/43")) {
		t.Fatalf("assembler did not verify the full source inventory: %s", out)
	}
	complete := exec.Command("python3", script, "--check", "--require-complete")
	if out, err := complete.CombinedOutput(); err != nil || !bytes.Contains(out, []byte("COMPLETE SOURCE INVENTORY: 43/43")) {
		t.Fatalf("complete source inventory failed verification: %v %s", err, out)
	}
	if got := sha256.Sum256(primitiveLuaRead(t, "lua/cj2_approve_boot.lua")); got != before {
		t.Fatal("assembler changed BOOT")
	}
	statements, err := parse.Parse(strings.NewReader(installLuaSource), "install.lua")
	if err != nil {
		t.Fatal(err)
	}
	loop, ok := statements[len(statements)-2].(*ast.NumberForStmt)
	if !ok || len(loop.Stmts) != 1 {
		t.Fatal("executor is not a one-call numeric loop")
	}
	stmt, ok := loop.Stmts[0].(*ast.FuncCallStmt)
	if !ok {
		t.Fatal("mutation reply is consumed")
	}
	call, ok := stmt.Expr.(*ast.FuncCallExpr)
	if !ok || len(call.Args) != 1 {
		t.Fatal("non-prebuilt mutation")
	}
	attr, ok := call.Func.(*ast.AttrGetExpr)
	if !ok || !bootLuaASTIdent(attr.Object, "redis") {
		t.Fatal("executor calls a helper")
	}
	method, ok := attr.Key.(*ast.StringExpr)
	if !ok || method.Value != "call" {
		t.Fatal("executor can swallow unexpected write error")
	}
	unpack, ok := call.Args[0].(*ast.FuncCallExpr)
	if !ok || !bootLuaASTIdent(unpack.Func, "unpack") || len(unpack.Args) != 3 {
		t.Fatal("unbounded descriptor unpack")
	}
	ret, ok := statements[len(statements)-1].(*ast.ReturnStmt)
	if !ok || len(ret.Exprs) != 1 {
		t.Fatal("post-write response construction")
	}
	reply, ok := ret.Exprs[0].(*ast.AttrGetExpr)
	if !ok || !bootLuaASTIdent(reply.Object, "execution") {
		t.Fatal("not returning prebuilt execution reply")
	}
	key, ok := reply.Key.(*ast.StringExpr)
	if !ok || key.Value != "reply" {
		t.Fatal("not returning prebuilt reply")
	}
	// Whole canonical bytes, not a hash over only the small operation tail.
	t.Logf("canonical INSTALL bytes=%d sha256=%x", len(installLuaSource), sha256.Sum256([]byte(installLuaSource)))
}
