package crawljobsv2

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// Actual lexical fragments, not recipes, a Go transition emulator or a fixture
// bypass. All evidence below is production-shaped nonzero *test data*, never
// operational proof. Real Redis allocator/ACL/latency/durability acceptance and
// process-stop/backlink-scan evidence remain separately gated.
func adminLuaCore(t *testing.T) string {
	t.Helper()
	return runLuaCore(t) + "CJ.Admin = (function()\n" + string(primitiveLuaRead(t, "lua_src/ledger_admin.lua")) + "\nend)()\n"
}

func adminLuaSource(t *testing.T, op OperationName) string {
	t.Helper()
	return adminLuaCore(t) + string(primitiveLuaRead(t, "lua_src/ops/"+strings.ToLower(string(op))+".lua"))
}

const adminLuaPrefix = "mifolyo:crawl:v2:"

var adminLuaLegacyKeys = []string{"mifolyo:crawl:v1:queue", "mifolyo:crawl:v1:urls", "mifolyo:crawl:v1:depths", "spider_queue", "signal_queue"}
var adminLuaQueues = []string{"pages_queue", "pages_queue:processing", "pages_queue:dead", "image_indexer_queue", "image_indexer_queue:processing", "image_indexer_queue:dead"}
var adminLuaOwners = []string{"pages_queue:indexer_owner", "image_indexer_queue:owner"}

func adminLuaCandidate(t *testing.T, migration bool) (*sharedLuaRedis, gateArtifacts) {
	t.Helper()
	r, a, input := runLuaFixture(t, true)
	if migration {
		runLuaSeed(t, r, input, "sealed")
	} else {
		a = newGateArtifacts(t)
	}
	marker, _ := a.marker.Record()
	freeze, _ := a.freeze.Record()
	r.setHash(ContractsCandidateKey, marker)
	r.setHash(AdminFreezeKey, freeze)
	return r, a
}

func adminLuaRetireInput(t *testing.T, a gateArtifacts) RetireLegacyKeysWireInput {
	t.Helper()
	i, err := a.legacy.Input()
	if err != nil {
		t.Fatal(err)
	}
	return RetireLegacyKeysWireInput{
		FreezeNonce: i.FreezeNonce, BackupSHA256: i.BackupSHA256, V1Count: i.V1Count,
		V1URLFieldCount: i.V1URLFieldCount, V1DepthFieldCount: i.V1DepthFieldCount, V1SourceSHA256: i.V1SourceSHA256,
		V1QueueEvidenceSHA256: i.V1QueueEvidenceSHA256, V1URLsEvidenceSHA256: i.V1URLsEvidenceSHA256, V1DepthsEvidenceSHA256: i.V1DepthsEvidenceSHA256,
		SpiderQueueType: i.SpiderQueueType, SpiderQueueCount: i.SpiderQueueCount, SpiderQueueEvidenceSHA256: i.SpiderQueueEvidenceSHA256,
		SignalQueueType: i.SignalQueueType, SignalQueueCount: i.SignalQueueCount, SignalQueueEvidenceSHA256: i.SignalQueueEvidenceSHA256,
	}
}

func adminLuaRetireWire(t *testing.T, a gateArtifacts, input RetireLegacyKeysWireInput, replay bool) ([]string, []string) {
	t.Helper()
	phase, legacy := CandidateBeforeLegacyRetirement, (*LegacyRetirementRecord)(nil)
	if replay {
		phase, legacy = CandidateAfterLegacyRetirement, &a.legacy
	}
	gate, err := NewTransportGate(OperationRetireLegacyKeys, candidateGateInput(a, phase, legacy))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRetireLegacyKeysWireRequest(gate, input)
	return runLuaParts(t, request, err)
}

func adminLuaPromoteWire(t *testing.T, a gateArtifacts) ([]string, []string) {
	t.Helper()
	gate, err := NewTransportGate(OperationPromoteCandidateContracts, candidateGateInput(a, CandidateAfterLegacyRetirement, &a.legacy))
	if err != nil {
		t.Fatal(err)
	}
	core, _ := a.guard.GuardCore()
	request, err := NewPromoteCandidateContractsWireRequest(gate, PromoteCandidateContractsWireInput{FreezeNonce: a.freeze.FreezeNonce(), GuardCore: core})
	return runLuaParts(t, request, err)
}

func adminLuaMarkWire(t *testing.T, a gateArtifacts, ids []RunID) ([]string, []string) {
	t.Helper()
	gate, err := NewTransportGate(OperationMarkPlannedShutdown, activeGateInput(a))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewMarkPlannedShutdownWireRequest(gate, MarkPlannedShutdownWireInput{
		PlannedShutdownNonce: strings.Repeat("7", 32), ProcessStopEvidenceSHA256: Digest(strings.Repeat("8", 64)), ActiveRunIDs: ids,
	})
	return runLuaParts(t, request, err)
}

func adminLuaRecord(t *testing.T, r *sharedLuaRedis, key string, schema RecordSchema) Record {
	t.Helper()
	names, err := RecordSchemaFields(schema)
	if err != nil {
		t.Fatal(err)
	}
	entry := r.data[key]
	if entry.kind != "hash" || len(entry.hash) != len(names) {
		t.Fatalf("%s: incomplete %s record", key, schema)
	}
	record := make(Record, len(names))
	for i, name := range names {
		v, ok := entry.hash[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		record[i] = textField(name, v)
	}
	if err := ValidateRecord(schema, record); err != nil {
		t.Fatalf("independent Go authority schema: %v", err)
	}
	return record
}

func adminLuaTrace(t *testing.T, r *sharedLuaRedis, writes int) {
	t.Helper()
	sharedLuaAssertTrace(t, r, writes)
	if len(r.trace) == 0 || r.trace[0].name != "TIME" {
		t.Fatal("TIME was not first, including malformed request")
	}
	for _, call := range r.trace {
		for _, key := range adminLuaLegacyKeys {
			if len(call.args) > 0 && call.args[0] == key {
				switch call.name {
				case "TYPE", "ZCARD", "HLEN", "LLEN", "UNLINK":
				default:
					t.Fatalf("bulk legacy read or forbidden mutation %s", call.name)
				}
			}
		}
		if call.name == "SMEMBERS" || call.name == "ZRANGE" || call.name == "LRANGE" {
			if strings.Contains(call.args[0], ":run:") {
				t.Fatal("admin enumerated a job/source index")
			}
		}
	}
	if writes > 0 && !r.returnedPrebuilt {
		t.Fatal("reply was not completely built before ACL/first write")
	}
}

func adminLuaReply(t *testing.T, r *sharedLuaRedis, op OperationName, keys, args []string, status string, writes int, tail ...string) {
	t.Helper()
	beforeWrites := r.writes
	got := sharedLuaNoError(t, sharedLuaRun(t, r, adminLuaSource(t, op), keys, args))
	if err := ValidateOperationResponse(op, got); err != nil {
		t.Fatalf("independent Go response oracle: %v", err)
	}
	want := []any{status, strconv.FormatUint(r.now, 10)}
	for _, value := range tail {
		want = append(want, value)
	}
	if !reflect.DeepEqual(got, want) || r.writes-beforeWrites != writes {
		t.Fatalf("got %v (%d writes), want %v (%d)", got, r.writes-beforeWrites, want, writes)
	}
	adminLuaTrace(t, r, writes)
}

func adminLuaReject(t *testing.T, r *sharedLuaRedis, op OperationName, keys, args []string, code ErrorCode) {
	t.Helper()
	sharedLuaRejectSource(t, r, adminLuaSource(t, op), keys, args, code)
	adminLuaTrace(t, r, 0)
}

func adminLuaUnchangedExcept(t *testing.T, before, after sharedLuaSnapshot, keys ...string) {
	t.Helper()
	ignored := map[string]bool{}
	for _, key := range keys {
		ignored[key] = true
	}
	strip := func(s sharedLuaSnapshot) sharedLuaSnapshot {
		out := sharedLuaSnapshot{bootLuaSnapshot: bootLuaSnapshot{data: map[string]bootLuaEntry{}}, sets: map[string]map[string]bool{}, zsets: map[string]map[string]float64{}, lists: map[string][]string{}}
		for key, value := range s.data {
			if !ignored[key] {
				out.data[key] = value
			}
		}
		for key, value := range s.sets {
			if !ignored[key] {
				out.sets[key] = value
			}
		}
		for key, value := range s.zsets {
			if !ignored[key] {
				out.zsets[key] = value
			}
		}
		for key, value := range s.lists {
			if !ignored[key] {
				out.lists[key] = value
			}
		}
		return out
	}
	if !reflect.DeepEqual(strip(before), strip(after)) {
		t.Fatal("unexpected data/index/TTL/canary change")
	}
}

// Populate opaque legacy bytes. They must never enter Lua's UTF-8/source/hash
// code path: only independently supplied type/count/evidence is authorized here.
func adminLuaLegacyData(r *sharedLuaRedis, input RetireLegacyKeysWireInput) string {
	counts := []uint64{input.V1Count, input.V1URLFieldCount, input.V1DepthFieldCount, input.SpiderQueueCount, input.SignalQueueCount}
	kinds := []string{"zset", "hash", "hash", string(input.SpiderQueueType), string(input.SignalQueueType)}
	bits := ""
	for i, key := range adminLuaLegacyKeys {
		r.removeKey(key)
		if counts[i] == 0 {
			bits += "0"
			continue
		}
		bits += "1"
		values, list := map[string]string{}, []string{}
		for j := uint64(0); j < counts[i]; j++ {
			values[fmt.Sprintf("\xff\x00%d", j)] = "1"
			list = append(list, "\xff\x00opaque legacy")
		}
		if kinds[i] == "list" {
			r.setList(key, list)
		} else {
			runLuaCollection(r, key, kinds[i], values)
		}
	}
	return bits
}

func adminLuaLegacyFromState(t *testing.T, r *sharedLuaRedis) LegacyRetirementRecord {
	t.Helper()
	record, err := DecodeLegacyRetirementRecord(primitiveLuaEncoded(t, adminLuaRecord(t, r, LegacyRetirementKey, SchemaLegacyRetirement)))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func adminLuaMaterializedRate(t *testing.T, r *sharedLuaRedis) string {
	t.Helper()
	scope := string(DeriveGlobalScopeID())
	key := adminLuaPrefix + "rate:" + scope
	record := recordAuthorityRateScopeRecord(t)
	changes := map[string]string{"scope_id": scope, "scope_kind": "global", "scope_witness": "global", "effective_concurrency": "2",
		"effective_interval_ms": "0", "next_allowed_ms": "0", "last_started_at_ms": strconv.FormatUint(r.now-1, 10), "updated_at_ms": strconv.FormatUint(r.now, 10)}
	for i, field := range record {
		if v, ok := changes[field.Name]; ok {
			record[i].Value = []byte(v)
		}
	}
	if err := ValidateRecord(SchemaRateScope, record); err != nil {
		t.Fatal(err)
	}
	r.setHash(key, record)
	r.setZSet(adminLuaPrefix+"rate_scopes", map[string]float64{scope: float64(r.now), strings.Repeat("b", 64): float64(r.now - 1)})
	return key
}

func TestAdminLuaRetireFreshMigrationBitmapsAndReplay(t *testing.T) {
	t.Parallel()
	// All achievable five-bit combinations: migration requires both metadata
	// hashes; fresh retirement may independently delete orphan URL/depth fields.
	for mask := 0; mask < 32; mask++ {
		if mask&1 != 0 && mask&6 != 6 {
			continue
		}
		for _, spiderKind := range []LegacyRedisType{LegacyTypeList, LegacyTypeZSet} {
			t.Run(fmt.Sprintf("mask=%02d/spider=%s", mask, spiderKind), func(t *testing.T) {
				r, a := adminLuaCandidate(t, mask&1 != 0)
				input := adminLuaRetireInput(t, a)
				// Distinct evidence makes confirmation ordering meaningful.
				input.BackupSHA256, input.V1QueueEvidenceSHA256 = Digest(strings.Repeat("1", 64)), Digest(strings.Repeat("2", 64))
				input.V1URLsEvidenceSHA256, input.V1DepthsEvidenceSHA256 = Digest(strings.Repeat("3", 64)), Digest(strings.Repeat("4", 64))
				input.SpiderQueueEvidenceSHA256, input.SignalQueueEvidenceSHA256 = Digest(strings.Repeat("5", 64)), Digest(strings.Repeat("6", 64))
				if mask&2 != 0 && input.V1URLFieldCount == 0 {
					input.V1URLFieldCount = 1
				}
				if mask&4 != 0 && input.V1DepthFieldCount == 0 {
					input.V1DepthFieldCount = 1
				}
				if mask&8 != 0 {
					input.SpiderQueueType, input.SpiderQueueCount = spiderKind, 2
				}
				if mask&16 != 0 {
					input.SignalQueueType, input.SignalQueueCount = LegacyTypeList, 2
				}
				bits := adminLuaLegacyData(r, input)
				keys, args := adminLuaRetireWire(t, a, input, false)
				if len(keys) != 16 || len(args) != 23 {
					t.Fatal("RETIRE wire shape")
				}
				before := r.snapshot()
				adminLuaReply(t, r, OperationRetireLegacyKeys, keys, args, "LEGACY_RETIRED", 1+strings.Count(bits, "1"), bits, string(input.V1SourceSHA256))
				allowed := append([]string{LegacyRetirementKey}, adminLuaLegacyKeys...)
				adminLuaUnchangedExcept(t, before, r.snapshot(), allowed...)
				var deleted []string
				for _, call := range r.trace {
					if !call.acl && call.name == "UNLINK" {
						deleted = append(deleted, call.args[0])
					}
				}
				var wantDeleted []string
				for i, key := range adminLuaLegacyKeys {
					if bits[i] == '1' {
						wantDeleted = append(wantDeleted, key)
					}
					if _, present := r.data[key]; present {
						t.Fatal("literal legacy key survived")
					}
				}
				if !reflect.DeepEqual(deleted, wantDeleted) {
					t.Fatal("UNLINK order differs from bitmap or deletes absent key")
				}
				a.legacy = adminLuaLegacyFromState(t, r)
				// Before-retirement authority cannot silently become a replay.
				adminLuaReject(t, r, OperationRetireLegacyKeys, keys, args, "")
				keys, args = adminLuaRetireWire(t, a, input, true)
				before = r.snapshot()
				r.now++
				r.maximum = 1 // Receipt is not memory/ACL admission.
				r.denyAt = 1
				adminLuaReply(t, r, OperationRetireLegacyKeys, keys, args, "EXISTS_IDENTICAL", 0, bits, string(input.V1SourceSHA256))
				if !reflect.DeepEqual(before, r.snapshot()) || r.aclCount != 0 {
					t.Fatal("retirement replay mutated/reapproved evidence")
				}
			})
		}
	}
}

func TestAdminLuaPromoteBothModesAndAdvancedPoststateReplay(t *testing.T) {
	t.Parallel()
	for _, migration := range []bool{false, true} {
		t.Run(fmt.Sprint(migration), func(t *testing.T) {
			r, a := adminLuaCandidate(t, migration)
			legacy, _ := a.legacy.Record()
			r.setHash(LegacyRetirementKey, legacy)
			keys, args := adminLuaPromoteWire(t, a)
			wantKeys := 29
			if migration {
				wantKeys = 56
				r.data[runLuaKey("")].hash["authorization_expires_at_ms"] = strconv.FormatUint(r.now+60000, 10)
			}
			if len(keys) != wantKeys || len(args) != 20 {
				t.Fatal("PROMOTE wire shape")
			}
			manifest, _ := a.marker.ManifestSHA256()
			before := r.snapshot()
			adminLuaReply(t, r, OperationPromoteCandidateContracts, keys, args, "CONTRACTS_PROMOTED", 4, string(manifest), string(a.contract), args[8])
			adminLuaUnchangedExcept(t, before, r.snapshot(), CommitGuardKey, ContractsCandidateKey, ContractsActiveKey, CrawlContractCandidateKey, CrawlContractKey, AdminFreezeKey)
			if !reflect.DeepEqual(r.data[ContractsActiveKey], before.data[ContractsCandidateKey]) || !reflect.DeepEqual(r.data[CrawlContractKey], before.data[CrawlContractCandidateKey]) {
				t.Fatal("RENAME did not preserve complete values/TTL")
			}
			guard := adminLuaRecord(t, r, CommitGuardKey, SchemaCommitGuard)
			core, _ := a.guard.GuardCore()
			expected, err := NewStoredCommitGuard(core, manifest, r.now)
			if err != nil {
				t.Fatal(err)
			}
			expectedRecord, _ := expected.Record()
			if string(primitiveLuaEncoded(t, guard)) != string(primitiveLuaEncoded(t, expectedRecord)) {
				t.Fatal("guard differs from independent Go constructor")
			}
			for _, key := range []string{ContractsCandidateKey, CrawlContractCandidateKey, AdminFreezeKey} {
				if _, present := r.data[key]; present {
					t.Fatal("candidate/freeze residue")
				}
			}
			// Realistic runtime can advance or even archive the migration run. The
			// receipt must not load it, inspect stage/rate/queue drain, or refresh
			// approved_at_ms. Poison values ensure those paths cannot pass by luck.
			for _, key := range keys[8:] {
				isLegacy := false
				for _, legacy := range adminLuaLegacyKeys {
					isLegacy = isLegacy || key == legacy
				}
				if !isLegacy {
					r.removeKey(key)
					r.data[key] = bootLuaEntry{kind: "string", value: "advanced runtime, not a pre-cutover shape", expireAt: -1}
				}
			}
			before = r.snapshot()
			r.now += 100000
			r.used, r.maximum, r.lazyfree, r.denyAt = 999, 1, 123, 1
			adminLuaReply(t, r, OperationPromoteCandidateContracts, keys, args, "EXISTS_IDENTICAL", 0, string(manifest), string(a.contract), args[8])
			if !reflect.DeepEqual(before, r.snapshot()) {
				t.Fatal("promotion receipt changed state")
			}
			for _, call := range r.trace {
				if call.name == "INFO" && call.args[0] == "MEMORY" {
					t.Fatal("promotion receipt observed memory")
				}
				for _, key := range keys[8:] {
					if strings.HasPrefix(key, adminLuaPrefix) && len(call.args) > 0 && call.args[0] == key {
						t.Fatal("promotion receipt read runtime inventory/run/stage/rate")
					}
				}
			}
		})
	}
}

func TestAdminLuaMarkFullRunsRatesAndSameProcessReceipt(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 1, 16} {
		for _, materialized := range []bool{false, true} {
			t.Run(fmt.Sprintf("runs=%d/rate=%t", count, materialized), func(t *testing.T) {
				r, a, input := runLuaFixture(t, false)
				ids := make([]RunID, count)
				states := []string{"loading", "auditing", "sealed", "active"}
				for i := range ids {
					state := states[i%len(states)]
					if state == "active" && !materialized {
						// Just activated, before any reservation: global rate has not
						// materialized yet. Historical starts require durable scope.
						runLuaSeed(t, r, input, "sealed")
						v := r.data[runLuaKey("")].hash
						v["state"], v["activated_at_ms"], v["last_activity_at_ms"] = "active", strconv.FormatUint(r.now, 10), strconv.FormatUint(r.now, 10)
					} else {
						runLuaSeed(t, r, input, state)
					}
					ids[i] = RunID(fmt.Sprintf("%032x", i+1))
					record := runLuaRecord(t, r)
					r.setHash(adminLuaPrefix+"run:"+string(ids[i]), record)
				}
				members := make([]string, count)
				for i := range ids {
					members[i] = string(ids[i])
				}
				r.setSet(adminLuaPrefix+"active_runs", members)
				if materialized {
					adminLuaMaterializedRate(t, r)
				}
				// Prior planned approval is legal. A subsequent mark replaces its
				// evidence and clears the consumed nonce, not rehearsal history.
				boot := r.data[DurabilityKey].hash
				boot["last_approval_mode"] = "planned"
				boot["consumed_planned_shutdown_nonce"] = strings.Repeat("3", 32)
				boot["planned_shutdown_evidence_sha256"] = strings.Repeat("4", 64)
				keys, args := adminLuaMarkWire(t, a, ids)
				if len(keys) != 19+2*count || len(args) != 10+count {
					t.Fatal("MARK wire shape")
				}
				before := r.snapshot()
				adminLuaReply(t, r, OperationMarkPlannedShutdown, keys, args, "OK", 1, args[7])
				adminLuaUnchangedExcept(t, before, r.snapshot(), DurabilityKey)
				adminLuaRecord(t, r, DurabilityKey, SchemaDurability)
				for field, old := range before.data[DurabilityKey].hash {
					want := old
					switch field {
					case "boot_state":
						want = "planned"
					case "planned_shutdown_nonce":
						want = args[7]
					case "planned_shutdown_evidence_sha256":
						want = args[8]
					case "consumed_planned_shutdown_nonce":
						want = ""
					}
					if boot[field] != want {
						t.Fatalf("incorrect planned record field %s", field)
					}
				}
				before = r.snapshot()
				r.now += 100000
				r.maximum, r.denyAt = 1, 1
				adminLuaReply(t, r, OperationMarkPlannedShutdown, keys, args, "EXISTS_IDENTICAL", 0, args[7])
				if !reflect.DeepEqual(before, r.snapshot()) {
					t.Fatal("planned retry wrote or refreshed approval")
				}
			})
		}
	}
}

func TestAdminLuaPromoteRequiresLazyfreeAndSingleMemoryObservation(t *testing.T) {
	t.Parallel()
	for _, info := range []string{
		"used_memory:1000000\r\nmaxmemory:419430400\r\n", // Missing is not zero.
		"used_memory:1000000\r\nmaxmemory:419430400\r\nlazyfree_pending_objects:01\r\n",
		"used_memory:1000000\r\nmaxmemory:419430400\r\nlazyfree_pending_objects:-1\r\n",
		"used_memory:1000000\r\nmaxmemory:419430400\r\nlazyfree_pending_objects:1\r\n",
		"used_memory:1000000\r\nmaxmemory:419430400\r\nlazyfree_pending_objects:0\r\nlazyfree_pending_objects:0\r\n",
	} {
		r, a := adminLuaCandidate(t, false)
		legacy, _ := a.legacy.Record()
		r.setHash(LegacyRetirementKey, legacy)
		keys, args := adminLuaPromoteWire(t, a)
		r.override = func(_ *lua.LState, name string, args []string) lua.LValue {
			if name == "INFO" && args[0] == "MEMORY" {
				return lua.LString(info)
			}
			return nil
		}
		adminLuaReject(t, r, OperationPromoteCandidateContracts, keys, args, "")
	}
}

func adminLuaSetScalar(t *testing.T, op OperationName, args []string, field, value string) []string {
	t.Helper()
	out := append([]string(nil), args...)
	for i, name := range operationWireSpecifications[op].semanticFields {
		if name == field {
			out[7+i] = value
			return out
		}
	}
	t.Fatalf("unknown scalar %s", field)
	return nil
}

func adminLuaConfirm(args []string) {
	values := []string{}
	for _, i := range []int{7, 8, 9, 12, 13, 14, 15, 18, 21} {
		values = append(values, args[i])
	}
	args[22] = strings.Join(values, ":")
}

func adminLuaRetireAll(t *testing.T) (*sharedLuaRedis, gateArtifacts, RetireLegacyKeysWireInput, []string, []string) {
	t.Helper()
	r, a := adminLuaCandidate(t, true)
	input := adminLuaRetireInput(t, a)
	input.SpiderQueueType, input.SpiderQueueCount = LegacyTypeZSet, 2
	input.SignalQueueType, input.SignalQueueCount = LegacyTypeList, 2
	adminLuaLegacyData(r, input)
	keys, args := adminLuaRetireWire(t, a, input, false)
	return r, a, input, keys, args
}

func adminLuaPromoteReady(t *testing.T, migration bool) (*sharedLuaRedis, gateArtifacts, []string, []string) {
	t.Helper()
	r, a := adminLuaCandidate(t, migration)
	legacy, _ := a.legacy.Record()
	r.setHash(LegacyRetirementKey, legacy)
	keys, args := adminLuaPromoteWire(t, a)
	return r, a, keys, args
}

func adminLuaMarkReady(t *testing.T) (*sharedLuaRedis, gateArtifacts, []string, []string) {
	t.Helper()
	r, a, input := runLuaFixture(t, false)
	runLuaSeed(t, r, input, "active")
	adminLuaMaterializedRate(t, r)
	keys, args := adminLuaMarkWire(t, a, []RunID{RunID(runLuaID)})
	return r, a, keys, args
}

func TestAdminLuaRetireInvalidConfirmedDeletion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ field, value string }{
		{"freeze_nonce", strings.Repeat("e", 32)},
		{"v1_count", "10001"}, {"v1_count", "3"}, {"v1_count", "0"},
		{"v1_url_field_count", "20001"}, {"v1_url_field_count", "1"}, {"v1_url_field_count", "3"},
		{"v1_depth_field_count", "20001"}, {"v1_depth_field_count", "1"}, {"v1_depth_field_count", "3"},
		{"spider_queue_count", "10001"}, {"spider_queue_count", "0"}, {"spider_queue_type", "none"}, {"spider_queue_type", "list"},
		{"signal_queue_count", "10001"}, {"signal_queue_count", "0"}, {"signal_queue_type", "none"}, {"signal_queue_type", "zset"},
		{"v1_source_sha256", strings.Repeat("f", 64)},
	} {
		t.Run(test.field+"/"+test.value, func(t *testing.T) {
			r, _, _, keys, args := adminLuaRetireAll(t)
			args = adminLuaSetScalar(t, OperationRetireLegacyKeys, args, test.field, test.value)
			adminLuaConfirm(args) // Even an exact confirmation cannot override state/limits.
			adminLuaReject(t, r, OperationRetireLegacyKeys, keys, args, "")
		})
	}
	for _, field := range []string{"backup_sha256", "v1_source_sha256", "v1_queue_evidence_sha256", "v1_urls_evidence_sha256", "v1_depths_evidence_sha256", "spider_queue_evidence_sha256", "signal_queue_evidence_sha256"} {
		for _, value := range []string{ZeroSHA256, "", strings.Repeat("A", 64)} {
			r, _, _, keys, args := adminLuaRetireAll(t)
			args = adminLuaSetScalar(t, OperationRetireLegacyKeys, args, field, value)
			adminLuaConfirm(args)
			adminLuaReject(t, r, OperationRetireLegacyKeys, keys, args, "")
		}
		// Syntactically valid changed evidence must also change confirmation.
		r, _, _, keys, args := adminLuaRetireAll(t)
		args = adminLuaSetScalar(t, OperationRetireLegacyKeys, args, field, strings.Repeat("f", 64))
		adminLuaReject(t, r, OperationRetireLegacyKeys, keys, args, "")
	}
	for _, confirmation := range []string{"", "yes", "CONFIRM", " ", strings.Repeat("a", 10000)} {
		r, _, _, keys, args := adminLuaRetireAll(t)
		args[22] = confirmation
		adminLuaReject(t, r, OperationRetireLegacyKeys, keys, args, "")
	}
	for _, key := range adminLuaLegacyKeys {
		for _, kind := range []string{"none", "string", "set", "hash", "list", "zset", "stream"} {
			r, _, _, keys, args := adminLuaRetireAll(t)
			if r.data[key].kind == kind {
				continue
			}
			r.removeKey(key)
			if kind != "none" {
				r.data[key] = bootLuaEntry{kind: kind, value: "wrong legacy type"}
			}
			adminLuaReject(t, r, OperationRetireLegacyKeys, keys, args, "")
		}
	}
	// Positive request + absence alone cannot claim an earlier call.
	r, _, _, keys, args := adminLuaRetireAll(t)
	for _, key := range adminLuaLegacyKeys {
		r.removeKey(key)
	}
	adminLuaReject(t, r, OperationRetireLegacyKeys, keys, args, "")
}

func TestAdminLuaRetireImmutableReplayAndResurrectedKeys(t *testing.T) {
	t.Parallel()
	r, a, input, keys, args := adminLuaRetireAll(t)
	adminLuaReply(t, r, OperationRetireLegacyKeys, keys, args, "LEGACY_RETIRED", 6, "11111", string(input.V1SourceSHA256))
	a.legacy = adminLuaLegacyFromState(t, r)
	keys, args = adminLuaRetireWire(t, a, input, true)
	for _, field := range operationWireSpecifications[OperationRetireLegacyKeys].semanticFields {
		if field == "confirmation_text" {
			continue
		}
		value := "1"
		if strings.HasSuffix(field, "sha256") {
			value = strings.Repeat("e", 64)
		}
		if field == "freeze_nonce" {
			value = strings.Repeat("e", 32)
		}
		if strings.HasSuffix(field, "type") {
			value = "none"
		}
		changed := adminLuaSetScalar(t, OperationRetireLegacyKeys, args, field, value)
		adminLuaConfirm(changed)
		adminLuaReject(t, r, OperationRetireLegacyKeys, keys, changed, "")
	}
	for _, key := range adminLuaLegacyKeys {
		r.data[key] = bootLuaEntry{kind: "string", value: "resurrection"}
		adminLuaReject(t, r, OperationRetireLegacyKeys, keys, args, "")
		r.removeKey(key)
	}
	for field, old := range r.data[LegacyRetirementKey].hash {
		r.data[LegacyRetirementKey].hash[field] = "!"
		adminLuaReject(t, r, OperationRetireLegacyKeys, keys, args, "")
		r.data[LegacyRetirementKey].hash[field] = old
	}
}

func TestAdminLuaCandidateRunInventoryAndLedgerCorruption(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationRetireLegacyKeys, OperationPromoteCandidateContracts} {
		makeCase := func() (*sharedLuaRedis, []string, []string) {
			if op == OperationRetireLegacyKeys {
				r, _, _, keys, args := adminLuaRetireAll(t)
				return r, keys, args
			}
			r, _, keys, args := adminLuaPromoteReady(t, true)
			return r, keys, args
		}
		for _, inventory := range []string{"runs", "active_runs", "unarchived_runs"} {
			for _, mutation := range []string{"missing", "different", "multiple", "wrong_type", "score_zero", "score_fraction"} {
				if inventory != "runs" && strings.HasPrefix(mutation, "score") {
					continue
				}
				t.Run(string(op)+"/"+inventory+"/"+mutation, func(t *testing.T) {
					r, keys, args := makeCase()
					key := adminLuaPrefix + inventory
					switch mutation {
					case "missing":
						r.removeKey(key)
					case "wrong_type":
						r.removeKey(key)
						r.data[key] = bootLuaEntry{kind: "string", value: "bad"}
					case "score_zero":
						r.zsets[key][runLuaID] = 0
					case "score_fraction":
						r.zsets[key][runLuaID] = 0.5
					default:
						id := strings.Repeat("2", 32)
						if inventory == "runs" {
							r.zsets[key][id] = float64(r.now)
							if mutation == "different" {
								delete(r.zsets[key], runLuaID)
							}
						} else {
							r.sets[key][id] = true
							if mutation == "different" {
								delete(r.sets[key], runLuaID)
							}
						}
					}
					adminLuaReject(t, r, op, keys, args, "")
				})
			}
		}
		for _, mutation := range []string{"source", "mongo", "count", "audit_incomplete", "audit_revision", "missing_field", "unknown_field", "claims", "outputs", "future", "ready", "leased", "group", "reasons", "contract"} {
			t.Run(string(op)+"/run/"+mutation, func(t *testing.T) {
				r, keys, args := makeCase()
				v := r.data[runLuaKey("")].hash
				switch mutation {
				case "source":
					v["source_sha256"] = strings.Repeat("b", 64)
				case "mongo":
					v["source_kind"] = "mongo"
				case "count":
					v["expected_seed_count"] = "3"
				case "audit_incomplete":
					v["audit_complete"] = "0"
				case "audit_revision":
					v["audit_revision"] = "1"
				case "missing_field":
					delete(v, "archive_sha256")
				case "unknown_field":
					v["run_id"] = runLuaID
				case "claims":
					v["claims_total"] = "1"
				case "outputs":
					v["output_commits_total"] = "1"
				case "future":
					v["last_activity_at_ms"] = strconv.FormatUint(r.now+1, 10)
				case "ready":
					r.removeKey(runLuaKey("ready"))
				case "leased":
					r.setZSet(runLuaKey("leased"), map[string]float64{strings.Repeat("1", 64): float64(r.now + 1000)})
				case "group":
					for id := range r.data[runLuaKey("group_open_jobs")].hash {
						r.data[runLuaKey("group_open_jobs")].hash[id] = "1"
					}
				case "reasons":
					r.data[runLuaKey("disposition_reason_counts")].hash["published"] = "1"
				case "contract":
					v["contract_sha256"] = strings.Repeat("b", 64)
				}
				adminLuaReject(t, r, op, keys, args, "")
			})
		}
		// Zero-source retirement/fresh promotion forbid *any* V2 run, even if
		// only a single inventory still references it.
		for _, name := range []string{"runs", "active_runs", "unarchived_runs"} {
			r, a := adminLuaCandidate(t, false)
			var keys, args []string
			if op == OperationRetireLegacyKeys {
				keys, args = adminLuaRetireWire(t, a, adminLuaRetireInput(t, a), false)
			} else {
				legacy, _ := a.legacy.Record()
				r.setHash(LegacyRetirementKey, legacy)
				keys, args = adminLuaPromoteWire(t, a)
			}
			if name == "runs" {
				r.setZSet(adminLuaPrefix+name, map[string]float64{runLuaID: float64(r.now)})
			} else {
				r.setSet(adminLuaPrefix+name, []string{runLuaID})
			}
			adminLuaReject(t, r, op, keys, args, "")
		}
	}
}

func TestAdminLuaPromoteEveryFreshDrainAndAuthorizationBoundary(t *testing.T) {
	t.Parallel()
	for _, migration := range []bool{false, true} {
		for _, key := range append(append(append([]string{}, adminLuaLegacyKeys...), adminLuaQueues...), adminLuaOwners...) {
			r, _, keys, args := adminLuaPromoteReady(t, migration)
			r.data[key] = bootLuaEntry{kind: "string", value: "not drained"}
			adminLuaReject(t, r, OperationPromoteCandidateContracts, keys, args, "")
		}
		for _, key := range adminLuaQueues {
			r, _, keys, args := adminLuaPromoteReady(t, migration)
			r.setList(key, []string{"pending work"})
			adminLuaReject(t, r, OperationPromoteCandidateContracts, keys, args, "")
		}
		for _, name := range []string{"first_request_start", "active_leases", "stage_slots", "stage_expiry", "rate_scopes"} {
			r, _, keys, args := adminLuaPromoteReady(t, migration)
			kind := "zset"
			if name == "stage_slots" || name == "first_request_start" {
				kind = "hash"
			}
			runLuaCollection(r, adminLuaPrefix+name, kind, map[string]string{strings.Repeat("e", 64): "1"})
			adminLuaReject(t, r, OperationPromoteCandidateContracts, keys, args, "")
		}
	}
	for _, remaining := range []uint64{0, 59999, 60000, 60001} {
		r, a, keys, args := adminLuaPromoteReady(t, true)
		r.data[runLuaKey("")].hash["authorization_expires_at_ms"] = strconv.FormatUint(r.now+remaining, 10)
		if remaining < 60000 {
			adminLuaReject(t, r, OperationPromoteCandidateContracts, keys, args, "")
		} else {
			manifest, _ := a.marker.ManifestSHA256()
			adminLuaReply(t, r, OperationPromoteCandidateContracts, keys, args, "CONTRACTS_PROMOTED", 4, string(manifest), string(a.contract), args[8])
		}
	}
}

func TestAdminLuaMarkCompleteDrainOnFreshAndReplay(t *testing.T) {
	t.Parallel()
	mutations := []string{"global_lease", "run_lease", "pending", "started", "slots", "expiry", "owner_pages", "owner_images",
		"missing_run", "finalized_run", "foreign_run_contract", "missing_inventory", "different_inventory", "extra_inventory", "rate_active", "rate_pending", "rate_started",
		"missing_rate", "missing_rate_member", "missing_rate_and_inventory", "wrong_rate_score", "rate_hash_wrong_type", "rate_inventory_wrong_type", "rate_extra_field", "rate_missing_empty_field"}
	for _, replay := range []bool{false, true} {
		for _, mutation := range mutations {
			t.Run(fmt.Sprintf("replay=%t/%s", replay, mutation), func(t *testing.T) {
				r, _, keys, args := adminLuaMarkReady(t)
				if replay {
					adminLuaReply(t, r, OperationMarkPlannedShutdown, keys, args, "OK", 1, args[7])
				}
				global := string(DeriveGlobalScopeID())
				rateKey := adminLuaPrefix + "rate:" + global
				v := r.data[runLuaKey("")].hash
				switch mutation {
				case "global_lease":
					r.setZSet(adminLuaPrefix+"active_leases", map[string]float64{runLuaID + ":" + strings.Repeat("1", 64): float64(r.now + 1000)})
				case "run_lease":
					r.setZSet(runLuaKey("leased"), map[string]float64{strings.Repeat("1", 64): float64(r.now + 1000)})
				case "pending":
					v["pending_request_reservations"] = "1"
					v["reservation_creations_total"] = "3"
				case "started":
					v["started_request_reservations"] = "1"
				case "slots":
					runLuaCollection(r, adminLuaPrefix+"stage_slots", "hash", map[string]string{strings.Repeat("1", 64): "0:" + runLuaID + ":" + strings.Repeat("2", 64) + ":1:1"})
				case "expiry":
					r.setZSet(adminLuaPrefix+"stage_expiry", map[string]float64{strings.Repeat("1", 64): float64(r.now)})
				case "owner_pages":
					r.data[adminLuaOwners[0]] = bootLuaEntry{kind: "string", value: "owner"}
				case "owner_images":
					r.data[adminLuaOwners[1]] = bootLuaEntry{kind: "string", value: "owner"}
				case "missing_run":
					r.removeKey(runLuaKey(""))
				case "finalized_run":
					r.setHash(runLuaKey(""), recordAuthorityRunRecord(t, "completed"))
				case "foreign_run_contract":
					v["contract_sha256"] = strings.Repeat("b", 64)
				case "missing_inventory":
					r.removeKey(adminLuaPrefix + "active_runs")
				case "different_inventory":
					r.setSet(adminLuaPrefix+"active_runs", []string{strings.Repeat("2", 32)})
				case "extra_inventory":
					r.sets[adminLuaPrefix+"active_runs"][strings.Repeat("2", 32)] = true
				case "rate_active", "rate_pending", "rate_started":
					r.setZSet(rateKey+":"+strings.TrimPrefix(mutation, "rate_"), map[string]float64{strings.Repeat("3", 64): float64(r.now + 1000)})
				case "missing_rate":
					r.removeKey(rateKey)
				case "missing_rate_member":
					delete(r.zsets[adminLuaPrefix+"rate_scopes"], global)
				case "missing_rate_and_inventory":
					r.removeKey(rateKey)
					r.removeKey(adminLuaPrefix + "rate_scopes")
				case "wrong_rate_score":
					r.zsets[adminLuaPrefix+"rate_scopes"][global] = 0.5
				case "rate_hash_wrong_type":
					r.removeKey(rateKey)
					r.setList(rateKey, []string{"bad"})
				case "rate_inventory_wrong_type":
					r.removeKey(adminLuaPrefix + "rate_scopes")
					r.setList(adminLuaPrefix+"rate_scopes", []string{"bad"})
				case "rate_extra_field":
					r.data[rateKey].hash["unknown"] = "0"
				case "rate_missing_empty_field":
					delete(r.data[rateKey].hash, "next_allowed_ms")
				}
				adminLuaReject(t, r, OperationMarkPlannedShutdown, keys, args, "")
			})
		}
	}
}

func TestAdminLuaMarkGlobalRateFixedShapeAndRelations(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ field, value string }{
		{"protocol_version", "1"}, {"scope_id", strings.Repeat("a", 64)}, {"scope_kind", "group"}, {"scope_witness", "Global"},
		{"effective_concurrency", "1"}, {"effective_interval_ms", "1"}, {"next_allowed_ms", "1"},
		{"active_count", "1"}, {"pending_count", "1"}, {"started_count", "1"},
		{"updated_at_ms", "0"}, {"updated_at_ms", strconv.FormatUint(bootLuaNow+1, 10)}, {"last_started_at_ms", strconv.FormatUint(bootLuaNow+1, 10)},
		{"concurrency_source_sha256", ZeroSHA256}, {"interval_source_sha256", ""},
		{"active_count", "00"}, {"started_count", "9007199254740992"},
	} {
		t.Run(test.field+"/"+test.value, func(t *testing.T) {
			r, _, keys, args := adminLuaMarkReady(t)
			rateKey := adminLuaPrefix + "rate:" + string(DeriveGlobalScopeID())
			r.data[rateKey].hash[test.field] = test.value
			adminLuaReject(t, r, OperationMarkPlannedShutdown, keys, args, "")
		})
	}
	for _, schema := range []RecordSchema{SchemaRun, SchemaRateScope} {
		fields, _ := RecordSchemaFields(schema)
		for _, field := range fields {
			t.Run(string(schema)+"/"+field, func(t *testing.T) {
				r, _, keys, args := adminLuaMarkReady(t)
				key := runLuaKey("")
				if schema == SchemaRateScope {
					key = adminLuaPrefix + "rate:" + string(DeriveGlobalScopeID())
				}
				r.data[key].hash[field] = "!"
				adminLuaReject(t, r, OperationMarkPlannedShutdown, keys, args, "")
			})
		}
	}
	// Missing global scope is legitimate only with absent global secondary keys;
	// no absent hash or count may default a dangling reservation to zero.
	for _, suffix := range []string{"active", "pending", "started"} {
		r, a, _ := runLuaFixture(t, false)
		keys, args := adminLuaMarkWire(t, a, nil)
		r.setZSet(adminLuaPrefix+"rate:"+string(DeriveGlobalScopeID())+":"+suffix, map[string]float64{strings.Repeat("2", 64): float64(r.now + 1000)})
		adminLuaReject(t, r, OperationMarkPlannedShutdown, keys, args, "")
	}
}

func TestAdminLuaMarkRequestIdentityAndPlannedBootIsolation(t *testing.T) {
	t.Parallel()
	r, a, keys, args := adminLuaMarkReady(t)
	adminLuaReply(t, r, OperationMarkPlannedShutdown, keys, args, "OK", 1, args[7])
	for _, change := range []struct{ field, value string }{
		{"planned_shutdown_nonce", strings.Repeat("9", 32)}, {"process_stop_evidence_sha256", strings.Repeat("9", 64)}, {"active_run_count", "0"},
	} {
		adminLuaReject(t, r, OperationMarkPlannedShutdown, keys, adminLuaSetScalar(t, OperationMarkPlannedShutdown, args, change.field, change.value), "")
	}
	// A different but fully Go-valid sorted run/key list must still match Redis.
	otherKeys, otherArgs := adminLuaMarkWire(t, a, []RunID{RunID(strings.Repeat("2", 32))})
	adminLuaReject(t, r, OperationMarkPlannedShutdown, otherKeys, otherArgs, "")
	otherKeys, otherArgs = adminLuaMarkWire(t, a, nil)
	adminLuaReject(t, r, OperationMarkPlannedShutdown, otherKeys, otherArgs, "")
	for _, field := range []string{"approved_redis_run_id", "boot_epoch", "planned_shutdown_nonce", "planned_shutdown_evidence_sha256", "consumed_planned_shutdown_nonce"} {
		old := r.data[DurabilityKey].hash[field]
		value := strings.Repeat("e", len(old))
		if field == "consumed_planned_shutdown_nonce" {
			value = strings.Repeat("e", 32)
		}
		r.data[DurabilityKey].hash[field] = value
		adminLuaReject(t, r, OperationMarkPlannedShutdown, keys, args, "")
		r.data[DurabilityKey].hash[field] = old
	}
	r.runID = strings.Repeat("f", 40)
	adminLuaReject(t, r, OperationMarkPlannedShutdown, keys, args, "")
	r.runID = strings.Repeat("a", 40)
	// Planned is not a fresh approved gate for a different operation.
	promoteKeys, promoteArgs := adminLuaPromoteWire(t, a)
	adminLuaReject(t, r, OperationPromoteCandidateContracts, promoteKeys, promoteArgs, ErrorBootUnapproved)
	// Nonce reuse from the most recently consumed planned approval is rejected.
	r, _, keys, args = adminLuaMarkReady(t)
	v := r.data[DurabilityKey].hash
	v["last_approval_mode"], v["planned_shutdown_evidence_sha256"], v["consumed_planned_shutdown_nonce"] = "planned", strings.Repeat("b", 64), args[7]
	adminLuaReject(t, r, OperationMarkPlannedShutdown, keys, args, "")
}

func TestAdminLuaPrivateRunBindingCannotBeMutated(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationRetireLegacyKeys, OperationPromoteCandidateContracts} {
		for _, mutation := range []string{
			`ctx.bound_run_id=other; ctx.request.v.run_id=other; ctx.request.v.candidate_run_id=other; ctx.keys.run="mifolyo:crawl:v2:run:"..other; id=other`,
			`ctx.keys.run="mifolyo:crawl:v2:run:"..other`,
			`ctx.bound_run_id=other; id=other`,
		} {
			var r *sharedLuaRedis
			var keys, args []string
			if op == OperationRetireLegacyKeys {
				r, _, _, keys, args = adminLuaRetireAll(t)
			} else {
				r, _, keys, args = adminLuaPromoteReady(t, true)
			}
			source := adminLuaCore(t) + `
local ctx=assert(CJ.Context.open(assert(CJ.Wire.admin_spec("` + string(op) + `")),KEYS,ARGV))
assert(CJ.Gate.check(ctx))
local id="` + runLuaID + `"
if ctx.operation=="CJ2_RETIRE_LEGACY_KEYS" then
 assert(CJ.Run.inventory(ctx,id)); assert(CJ.Context.bind_run_read(ctx,id))
end
assert(CJ.Context.bound_run(ctx)==id)
local other=string.rep("2",32)
` + mutation + `
local run,code=CJ.Run.load(ctx,id)
if not run then return CJ.Context.reject(code) end
return {"unexpected"}`
			sharedLuaRejectSource(t, r, source, keys, args, ErrorInvalidIdentifier)
			adminLuaTrace(t, r, 0)
		}
	}
	// Public projections cannot grant the private binding in the first place.
	r, _, _, keys, args := adminLuaRetireAll(t)
	source := adminLuaCore(t) + `
local ctx=assert(CJ.Context.open(assert(CJ.Wire.admin_spec("CJ2_RETIRE_LEGACY_KEYS")),KEYS,ARGV))
assert(CJ.Gate.check(ctx))
ctx.bound_run_id="` + runLuaID + `"; ctx.request.v.run_id=ctx.bound_run_id
ctx.keys.run="mifolyo:crawl:v2:run:"..ctx.bound_run_id
ctx.allowed[ctx.keys.run]=true
local run,code=CJ.Run.load(ctx,ctx.bound_run_id)
if not run then return CJ.Context.reject(code) end
return {"unexpected"}`
	sharedLuaRejectSource(t, r, source, keys, args, ErrorInvalidIdentifier)
	// Complete RETIRE proof still grants no write to any of the 27 run keys.
	source = adminLuaCore(t) + `
local ctx=assert(CJ.Context.open(assert(CJ.Wire.admin_spec("CJ2_RETIRE_LEGACY_KEYS")),KEYS,ARGV))
assert(CJ.Gate.check(ctx)); assert(CJ.Run.inventory(ctx,"` + runLuaID + `"))
assert(CJ.Context.bind_run_read(ctx,"` + runLuaID + `")); assert(CJ.Run.load(ctx,"` + runLuaID + `"))
local plan=assert(CJ.Plan.new(ctx))
for _,key in ipairs(ctx.keys.run_keys) do
 ctx.allowed[key]=true
 local ok= CJ.Plan.add(plan,{"UNLINK",key},"ordinary"); assert(ok==nil)
end
return {"read only"}`
	before := r.snapshot()
	got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	if !reflect.DeepEqual(got, []any{"read only"}) || !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("private read-only binding broadened")
	}
}

func adminLuaReady(t *testing.T, op OperationName) (*sharedLuaRedis, []string, []string, string, int, []string) {
	t.Helper()
	switch op {
	case OperationRetireLegacyKeys:
		r, _, input, keys, args := adminLuaRetireAll(t)
		return r, keys, args, "LEGACY_RETIRED", 6, []string{"11111", string(input.V1SourceSHA256)}
	case OperationPromoteCandidateContracts:
		r, a, keys, args := adminLuaPromoteReady(t, true)
		manifest, _ := a.marker.ManifestSHA256()
		return r, keys, args, "CONTRACTS_PROMOTED", 4, []string{string(manifest), string(a.contract), args[8]}
	case OperationMarkPlannedShutdown:
		r, _, keys, args := adminLuaMarkReady(t)
		return r, keys, args, "OK", 1, []string{args[7]}
	}
	t.Fatal("unknown admin operation")
	return nil, nil, nil, "", 0, nil
}

func TestAdminLuaWireBoundariesTimeFirst(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationRetireLegacyKeys, OperationPromoteCandidateContracts, OperationMarkPlannedShutdown} {
		t.Run(string(op), func(t *testing.T) {
			r, keys, args, _, _, _ := adminLuaReady(t, op)
			for length := 0; length < len(args); length++ {
				adminLuaReject(t, r, op, keys, args[:length], "")
			}
			adminLuaReject(t, r, op, keys, append(append([]string(nil), args...), "extra"), "")
			for i := range keys {
				changed := append([]string(nil), keys...)
				changed[i] = "unrelated:set"
				adminLuaReject(t, r, op, changed, args, "")
			}
			adminLuaReject(t, r, op, keys[:len(keys)-1], args, "")
			adminLuaReject(t, r, op, append(append([]string(nil), keys...), "unrelated"), args, "")
			for i := 0; i < len(args); i++ {
				changed := append([]string(nil), args...)
				changed[i] = "\xff"
				adminLuaReject(t, r, op, keys, changed, "")
			}
			oversize := append([]string(nil), args...)
			oversize[len(oversize)-1] = strings.Repeat("x", 2097153)
			adminLuaReject(t, r, op, keys, oversize, "")
		})
	}
	// Sorted run IDs are exact submitted bytes; Lua never sorts/deduplicates.
	r, a, _, _ := adminLuaMarkReady(t)
	keys, args := adminLuaMarkWire(t, a, []RunID{RunID(strings.Repeat("1", 32)), RunID(strings.Repeat("2", 32))})
	for _, mutation := range []string{"reverse", "duplicate", "bad_id", "count17", "count01"} {
		changed := append([]string(nil), args...)
		switch mutation {
		case "reverse":
			changed[10], changed[11] = changed[11], changed[10]
		case "duplicate":
			changed[11] = changed[10]
		case "bad_id":
			changed[10] = strings.Repeat("A", 32)
		case "count17":
			changed[9] = "17"
		case "count01":
			changed[9] = "01"
		}
		adminLuaReject(t, r, OperationMarkPlannedShutdown, keys, changed, "")
	}
}

func TestAdminLuaCompleteAuthorityNoProvisionalOrPartialState(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationRetireLegacyKeys, OperationPromoteCandidateContracts, OperationMarkPlannedShutdown} {
		for _, replay := range []bool{false, true} {
			r, keys, args, status, writes, tail := adminLuaReady(t, op)
			if replay {
				adminLuaReply(t, r, op, keys, args, status, writes, tail...)
				if op == OperationRetireLegacyKeys {
					a := newMigrationGateArtifacts(t, 2)
					a.legacy = adminLuaLegacyFromState(t, r)
					keys, args = adminLuaRetireWire(t, a, adminLuaRetireInput(t, a), true)
				}
			}
			for _, key := range keys[:8] {
				entry, exists := r.data[key]
				if !exists {
					r.data[key] = bootLuaEntry{kind: "string", value: "partial conflicting authority"}
					adminLuaReject(t, r, op, keys, args, "")
					r.removeKey(key)
					continue
				}
				delete(r.data, key)
				adminLuaReject(t, r, op, keys, args, "")
				r.data[key] = entry
				if entry.kind == "hash" {
					for field, old := range entry.hash {
						entry.hash[field] = "!"
						adminLuaReject(t, r, op, keys, args, "")
						entry.hash[field] = old
					}
					entry.hash["unknown"] = "0"
					adminLuaReject(t, r, op, keys, args, "")
					delete(entry.hash, "unknown")
				} else {
					changed := entry
					changed.value = strings.Repeat("b", 64)
					r.data[key] = changed
					adminLuaReject(t, r, op, keys, args, "")
					r.data[key] = entry
				}
			}
		}
	}
}

func TestAdminLuaPromoteExactEveryScalarIncludingReceiptIdentity(t *testing.T) {
	t.Parallel()
	for _, migration := range []bool{false, true} {
		for _, replay := range []bool{false, true} {
			r, a, keys, args := adminLuaPromoteReady(t, migration)
			if replay {
				manifest, _ := a.marker.ManifestSHA256()
				adminLuaReply(t, r, OperationPromoteCandidateContracts, keys, args, "CONTRACTS_PROMOTED", 4, string(manifest), string(a.contract), args[8])
			}
			for _, field := range operationWireSpecifications[OperationPromoteCandidateContracts].semanticFields {
				value := "0"
				switch field {
				case "freeze_nonce", "candidate_run_id":
					value = strings.Repeat("e", 32)
				case "redis_version":
					value = "7.2.6"
				case "cutover_mode":
					if migration {
						value = "fresh"
					} else {
						value = "v1_migration"
					}
				default:
					if strings.HasSuffix(field, "sha256") {
						value = strings.Repeat("e", 64)
					}
				}
				adminLuaReject(t, r, OperationPromoteCandidateContracts, keys, adminLuaSetScalar(t, OperationPromoteCandidateContracts, args, field, value), "")
				if strings.HasSuffix(field, "sha256") {
					adminLuaReject(t, r, OperationPromoteCandidateContracts, keys, adminLuaSetScalar(t, OperationPromoteCandidateContracts, args, field, ZeroSHA256), "")
				}
			}
		}
	}
}

// Independent accounting over Redis command semantics, not admin transitions:
// full replacement bytes, new key/field bytes and overhead; UNLINK has no credit;
// RENAME moves payload without recharging it, but charges both names/new key.
func adminLuaDescriptorGrowth(t *testing.T, before sharedLuaSnapshot, trace []bootLuaCommand) uint64 {
	t.Helper()
	logical, newKeys, newElements := 0, 0, 0
	for _, call := range trace {
		if call.acl || !sharedLuaIsWrite(call.name) {
			continue
		}
		key := call.args[0]
		entry, exists := before.data[key]
		switch call.name {
		case "HSET":
			if !exists {
				newKeys++
				logical += len(key)
			}
			for i := 1; i < len(call.args); i += 2 {
				field, value := call.args[i], call.args[i+1]
				old, found := entry.hash[field]
				if !found {
					newElements++
					logical += len(field)
				}
				if !found || old != value {
					logical += len(value)
				}
			}
		case "RENAME":
			newKeys++
			logical += len(key) + len(call.args[1])
		case "UNLINK":
		default:
			t.Fatalf("unexpected admin write %s", call.name)
		}
	}
	return uint64(3*logical + 1024*newKeys + 256*newElements)
}

func TestAdminLuaPrebuiltPlanAccountingACLAndNoninitialWriteFailure(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationRetireLegacyKeys, OperationPromoteCandidateContracts, OperationMarkPlannedShutdown} {
		t.Run(string(op), func(t *testing.T) {
			r, keys, args, status, writes, tail := adminLuaReady(t, op)
			before := r.snapshot()
			adminLuaReply(t, r, op, keys, args, status, writes, tail...)
			growth := adminLuaDescriptorGrowth(t, before, r.trace)
			if growth == 0 {
				t.Fatal("zero growth for effective admin mutation")
			}
			var commands []string
			for _, call := range r.trace {
				if !call.acl && sharedLuaIsWrite(call.name) {
					commands = append(commands, call.name)
				}
			}
			if op == OperationPromoteCandidateContracts && !reflect.DeepEqual(commands, []string{"HSET", "RENAME", "RENAME", "UNLINK"}) {
				t.Fatal("wrong promotion descriptor order")
			}
			for _, delta := range []uint64{0, 1} {
				r, keys, args, status, writes, tail := adminLuaReady(t, op)
				r.maximum = r.used + 67108864 + 16777216 + growth - delta
				if delta == 1 {
					adminLuaReject(t, r, op, keys, args, ErrorMemoryHeadroomLow)
				} else {
					adminLuaReply(t, r, op, keys, args, status, writes, tail...)
				}
			}
			// Every descriptor, especially the LAST one, is ACL-preflighted before
			// the first write; denied destructive plans leave all canaries intact.
			for denied := 1; denied <= writes; denied++ {
				r, keys, args, _, _, _ := adminLuaReady(t, op)
				r.denyAt = denied
				adminLuaReject(t, r, op, keys, args, ErrorBootUnapproved)
				if r.aclCount != denied || r.attempts != 0 {
					t.Fatal("ACL denial did not stop before execution")
				}
			}
			for at := 1; at <= writes; at++ {
				for _, after := range []bool{false, true} {
					r, keys, args, _, _, _ := adminLuaReady(t, op)
					r.failAt, r.failAfter = at, after
					before := r.snapshot()
					result := sharedLuaRun(t, r, adminLuaSource(t, op), keys, args)
					applied := at - 1
					if after {
						applied++
					}
					if result.runtimeErr == nil || !strings.Contains(result.runtimeErr.Error(), bootLuaWriteErr) || result.raw != nil ||
						r.attempts != at || r.writes != applied || r.aclCount != writes {
						t.Fatal("executor masked failure, missed preflight, or pretended rollback")
					}
					if reflect.DeepEqual(before, r.snapshot()) != (applied == 0) {
						t.Fatal("unexpected rollback / lost partial-write effects")
					}
					sharedLuaAssertTrace(t, r, writes)
					if applied > 0 && op == OperationRetireLegacyKeys {
						if _, present := r.data[LegacyRetirementKey]; !present {
							t.Fatal("retirement evidence was rolled back")
						}
					}
					if applied > 0 && op == OperationPromoteCandidateContracts {
						if _, present := r.data[CommitGuardKey]; !present {
							t.Fatal("guard was rolled back")
						}
						if applied < writes {
							r.failAt = 0
							adminLuaReject(t, r, op, keys, args, "") // Never repair partial promotion.
						}
					}
					if applied == writes {
						r.failAt = 0
						if op == OperationRetireLegacyKeys {
							a := newMigrationGateArtifacts(t, 2)
							a.legacy = adminLuaLegacyFromState(t, r)
							keys, args = adminLuaRetireWire(t, a, adminLuaRetireInput(t, a), true)
						}
						completed := r.snapshot()
						adminLuaReply(t, r, op, keys, args, "EXISTS_IDENTICAL", 0, tail...)
						if !reflect.DeepEqual(completed, r.snapshot()) {
							t.Fatal("completed lost-response reconciliation wrote state")
						}
					}
				}
			}
		})
	}
}

func TestAdminLuaMaximumMigrationAndFullAdminChain(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 1, 10000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			r, a := adminLuaCandidate(t, count > 0)
			if count > 0 {
				a = newMigrationGateArtifacts(t, uint64(count))
				// Populate bounded retained state, not 10,000 request/source
				// arguments. The actual Run ledger must check it without scans.
				v := r.data[runLuaKey("")].hash
				for _, field := range []string{"expected_seed_count", "job_count", "open_job_count", "audit_count"} {
					v[field] = strconv.Itoa(count)
				}
				v["audit_cursor"] = fmt.Sprintf("%064x", count)
				for _, name := range []string{"group_open_jobs", "audit_group_counts"} {
					for group := range r.data[runLuaKey(name)].hash {
						r.data[runLuaKey(name)].hash[group] = strconv.Itoa(count)
					}
				}
				jobs, order, ready, ages := []string{}, map[string]float64{}, map[string]float64{}, map[string]float64{}
				for i := 1; i <= count; i++ {
					id := fmt.Sprintf("%064x", i)
					jobs = append(jobs, id)
					order[id] = 0
					ready[id] = 1
					ages[id] = float64(r.now - 500)
				}
				r.setSet(runLuaKey("jobs"), jobs)
				r.setZSet(runLuaKey("job_order"), order)
				r.setZSet(runLuaKey("ready"), ready)
				r.setZSet(runLuaKey("ready_at"), ages)
				runLuaRecord(t, r)
			}
			input := adminLuaRetireInput(t, a)
			input.V1URLFieldCount, input.V1DepthFieldCount = 20000, 20000
			input.SpiderQueueType, input.SpiderQueueCount = LegacyTypeList, 10000
			input.SignalQueueType, input.SignalQueueCount = LegacyTypeList, 10000
			bits := adminLuaLegacyData(r, input)
			keys, args := adminLuaRetireWire(t, a, input, false)
			adminLuaReply(t, r, OperationRetireLegacyKeys, keys, args, "LEGACY_RETIRED", 1+strings.Count(bits, "1"), bits, string(input.V1SourceSHA256))
			a.legacy = adminLuaLegacyFromState(t, r)
			keys, args = adminLuaPromoteWire(t, a)
			manifest, _ := a.marker.ManifestSHA256()
			adminLuaReply(t, r, OperationPromoteCandidateContracts, keys, args, "CONTRACTS_PROMOTED", 4, string(manifest), string(a.contract), args[8])
			var err error
			a.guard, err = DecodeStoredCommitGuard(primitiveLuaEncoded(t, adminLuaRecord(t, r, CommitGuardKey, SchemaCommitGuard)))
			if err != nil {
				t.Fatal(err)
			}
			ids := []RunID{}
			if count > 0 {
				ids = append(ids, RunID(runLuaID))
			}
			keys, args = adminLuaMarkWire(t, a, ids)
			adminLuaReply(t, r, OperationMarkPlannedShutdown, keys, args, "OK", 1, args[7])
			r.now += 86400000 // Receipt needs no new approval/authorization freshness.
			adminLuaReply(t, r, OperationMarkPlannedShutdown, keys, args, "EXISTS_IDENTICAL", 0, args[7])
		})
	}
}

func TestAdminLuaBoundedReadFailuresAndReplyValidators(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationRetireLegacyKeys, OperationPromoteCandidateContracts, OperationMarkPlannedShutdown} {
		for _, kind := range []string{"TIME", "SERVER", "HLEN", "HSTRLEN", "HMGET", "GET", "ZCARD", "SCARD"} {
			r, keys, args, _, _, _ := adminLuaReady(t, op)
			seen := false
			r.override = func(L *lua.LState, name string, args []string) lua.LValue {
				match := name == kind || kind == "SERVER" && name == "INFO" && args[0] == "SERVER"
				if !match {
					return nil
				}
				seen = true
				if name == "HLEN" || name == "HSTRLEN" || name == "ZCARD" || name == "SCARD" {
					return lua.LNumber(100001)
				}
				if name == "HMGET" {
					return bootLuaArray(L, lua.LFalse)
				}
				return lua.LString("not a bounded valid reply")
			}
			adminLuaReject(t, r, op, keys, args, "")
			if !seen {
				t.Fatalf("unexercised read override %s/%s", op, kind)
			}
		}
		r, keys, args, status, _, tail := adminLuaReady(t, op)
		source := adminLuaCore(t) + `
local ctx=assert(CJ.Context.open(assert(CJ.Wire.admin_spec("` + string(op) + `")),KEYS,ARGV))
local tail=` + sharedLuaLiteralArray(tail) + `
assert(CJ.Reply.build(ctx,"` + status + `",tail))
assert(CJ.Reply.build(ctx,"EXISTS_IDENTICAL",tail))
assert(not CJ.Reply.build(ctx,"OK_NOT_REALLY",tail))
assert(not CJ.Reply.build(ctx,"` + status + `",{}))
for i=1,#tail do
 local old=tail[i]; tail[i]="different"; assert(not CJ.Reply.build(ctx,"` + status + `",tail)); tail[i]=old
end
tail[#tail+1]="extra"; assert(not CJ.Reply.build(ctx,"` + status + `",tail))
return {"closed"}`
		before := r.snapshot()
		got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
		if !reflect.DeepEqual(got, []any{"closed"}) || !reflect.DeepEqual(before, r.snapshot()) {
			t.Fatal("permissive response validator")
		}
	}
}

func TestAdminLuaPromotionReceiptAfterActualRunActivation(t *testing.T) {
	t.Parallel()
	for _, migration := range []bool{false, true} {
		t.Run(fmt.Sprint(migration), func(t *testing.T) {
			r, a, keys, args := adminLuaPromoteReady(t, migration)
			manifest, _ := a.marker.ManifestSHA256()
			adminLuaReply(t, r, OperationPromoteCandidateContracts, keys, args, "CONTRACTS_PROMOTED", 4, string(manifest), string(a.contract), args[8])
			var err error
			a.guard, err = DecodeStoredCommitGuard(primitiveLuaEncoded(t, adminLuaRecord(t, r, CommitGuardKey, SchemaCommitGuard)))
			if err != nil {
				t.Fatal(err)
			}
			_, _, input := runLuaFixture(t, migration)
			if !migration {
				// Fresh mode can now create a Mongo run; it could not do so before
				// promotion. Seed audited state, then execute the real activation.
				runLuaSeed(t, r, input, "sealed")
			}
			gate := runLuaGate(t, a, OperationActivateRun, false)
			request, err := NewActivateRunWireRequest(gate, ActivateRunWireInput{
				RunID: input.RunID, SourceSHA256: input.SourceSHA256, AuthorizationSHA256: input.AuthorizationSHA256,
				CrawlPolicySHA256: input.CrawlPolicySHA256, RenderPolicySHA256: input.RenderPolicySHA256, CanonicalizationSHA256: input.CanonicalizationSHA256,
			})
			runKeys, runArgs := runLuaParts(t, request, err)
			runLuaReply(t, r, OperationActivateRun, runKeys, runArgs, "ACTIVATED", strconv.FormatUint(r.now, 10))
			runLuaRecord(t, r)
			for _, key := range adminLuaQueues {
				r.setList(key, []string{strings.Repeat("e", 64)})
			}
			for _, key := range adminLuaOwners {
				r.data[key] = bootLuaEntry{kind: "string", value: "runtime consumer", expireAt: -1}
			}
			r.now += 10000000 // Even expired run authorization is not replay approval.
			r.maximum, r.lazyfree, r.denyAt = 1, 10, 1
			before := r.snapshot()
			adminLuaReply(t, r, OperationPromoteCandidateContracts, keys, args, "EXISTS_IDENTICAL", 0, string(manifest), string(a.contract), args[8])
			if !reflect.DeepEqual(before, r.snapshot()) {
				t.Fatal("post-activation promotion receipt mutated runtime")
			}
		})
	}
}
