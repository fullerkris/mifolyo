package crawljobsv2

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// Actual source chunks and the shared command-semantic facade only. No Redis,
// network, canonical recipe changes, semantic callbacks, grants or gate mocks.
func maintenanceLuaCore(t *testing.T) string {
	t.Helper()
	return recordsLuaCore(t) +
		"CJ.Request=(function()\n" + string(primitiveLuaRead(t, "lua_src/ledger_request.lua")) + "\nend)()\n" +
		"CJ.StageOutput=(function()\n" + string(primitiveLuaRead(t, "lua_src/stage_output.lua")) + "\nend)()\n" +
		"CJ.Stage=(function()\n" + string(primitiveLuaRead(t, "lua_src/ledger_stage.lua")) + "\nend)()\n" +
		"CJ.Maintenance=(function()\n" + string(primitiveLuaRead(t, "lua_src/ledger_maintenance.lua")) + "\nend)()\n"
}

func maintenanceLuaSource(t *testing.T, op OperationName) string {
	t.Helper()
	return maintenanceLuaCore(t) + string(primitiveLuaRead(t, "lua_src/ops/"+strings.ToLower(string(op))+".lua"))
}

func maintenanceLuaWire(t *testing.T, f *recordsLuaFixture, op OperationName, first JobID) ([]string, []string) {
	t.Helper()
	gate := runLuaGate(t, f.a, op, f.candidate)
	var req OperationWireRequest
	var err error
	switch op {
	case OperationPromoteDue:
		req, err = NewPromoteDueWireRequest(gate, f.input.RunID)
	case OperationRecoverExpired:
		req, err = NewRecoverExpiredWireRequest(gate, f.input.RunID)
	case OperationCancelBatch:
		req, err = NewCancelBatchWireRequest(gate, f.input.RunID)
	case OperationPurgeRunBatch:
		req, err = NewPurgeRunBatchWireRequest(gate, PurgeRunBatchWireInput{RunID: f.input.RunID,
			EvidenceSHA256: Digest(strings.Repeat("b", 64)), ExpectedFirstJobID: first})
	case OperationCleanStage:
		req, err = NewCleanStageWireRequest(gate, CleanStageWireInput{ExpectedCommitID: Digest(strings.Repeat("c", 64)), ExpectedCleanupDueAtMS: f.r.now})
	case OperationMaintainRateScopes:
		req, err = NewMaintainRateScopesWireRequest(gate, 0)
	default:
		t.Fatalf("unknown operation %s", op)
	}
	return runLuaParts(t, req, err)
}

func maintenanceLuaNew(t *testing.T, n int, candidate bool) *recordsLuaFixture {
	t.Helper()
	f := recordsLuaNew(t, candidate, n, n, 1)
	f.seed(t, f.jobs)
	if !candidate {
		v := f.r.data[runLuaKey("")].hash
		v["state"], v["audit_revision"], v["audit_count"], v["audit_complete"] = "active", v["load_revision"], strconv.Itoa(n), "1"
		v["audit_cursor"] = ""
		if n > 0 {
			v["audit_cursor"] = string(f.jobs[n-1].JobID)
		}
		v["sealed_at_ms"], v["activated_at_ms"] = strconv.FormatUint(f.r.now-600, 10), strconv.FormatUint(f.r.now-500, 10)
		f.r.setHash(runLuaKey("audit_group_counts"), Record{textField(string(f.input.PolicyGroups[0].GroupID), strconv.Itoa(n))})
	}
	runLuaRecord(t, f.r)
	return f
}

func maintenanceLuaJobRecord(t *testing.T, r *sharedLuaRedis, id JobID) Record {
	t.Helper()
	v := r.data[runLuaKey("job:"+string(id))].hash
	names, err := RecordSchemaFields(SchemaJob)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != len(names) {
		t.Fatal("not a complete job hash")
	}
	record := make(Record, len(names))
	for i, name := range names {
		record[i] = textField(name, v[name])
	}
	if err := ValidateRecord(SchemaJob, record); err != nil {
		t.Fatalf("Go job oracle rejected %s: %v", id, err)
	}
	return record
}

func maintenanceLuaDelay(t *testing.T, f *recordsLuaFixture, n int) {
	t.Helper()
	ready, ages, delayed := map[string]float64{}, map[string]float64{}, map[string]float64{}
	for i, job := range f.jobs {
		id := string(job.JobID)
		if i < n {
			v := f.r.data[runLuaKey("job:"+id)].hash
			// Equal deadlines exercise Redis's byte-ID tie breaker.
			due := f.r.now - uint64(i%3)
			v["state"], v["not_before_ms"] = "delayed", strconv.FormatUint(due, 10)
			delayed[id] = float64(due)
		} else {
			ready[id], _ = strconv.ParseFloat(string(job.ScoreText), 64)
			ages[id] = float64(f.r.now - 100)
		}
		maintenanceLuaJobRecord(t, f.r, job.JobID)
	}
	f.r.setZSet(runLuaKey("ready"), ready)
	f.r.setZSet(runLuaKey("ready_at"), ages)
	f.r.setZSet(runLuaKey("delayed"), delayed)
}

func maintenanceLuaTrace(t *testing.T, r *sharedLuaRedis) {
	t.Helper()
	writeNames := map[string]bool{"HSET": true, "HDEL": true, "ZADD": true, "ZREM": true, "SADD": true, "SREM": true, "UNLINK": true, "PEXPIREAT": true}
	var acl []bootLuaCommand
	writes, clocks := 0, 0
	for _, call := range r.trace {
		if call.acl {
			if writes > 0 {
				t.Fatal("ACL after first write")
			}
			acl = append(acl, call)
			continue
		}
		if writeNames[call.name] {
			if writes >= len(acl) || call.name != acl[writes].name || !reflect.DeepEqual(call.args, acl[writes].args) {
				t.Fatal("write without exact preflight ACL")
			}
			writes++
		} else if writes > 0 {
			t.Fatalf("read %s after first write", call.name)
		}
		if call.name == "TIME" {
			clocks++
		}
		if call.name == "SCAN" || call.name == "HSCAN" || call.name == "KEYS" || call.name == "HGETALL" {
			t.Fatalf("unbounded command: %s", call.name)
		}
	}
	if clocks != 1 || len(r.trace) == 0 || r.trace[0].name != "TIME" {
		t.Fatal("clock must be first and unique")
	}
	if writes > 0 && !r.returnedPrebuilt {
		t.Fatal("reply was not prebuilt before the first write")
	}
}

func maintenanceLuaReply(t *testing.T, f *recordsLuaFixture, op OperationName, keys, args []string, status string, count, more int) {
	t.Helper()
	got := sharedLuaNoError(t, stageOpsRun(t, &stageOpsRedis{f.r}, maintenanceLuaSource(t, op), keys, args))
	if err := ValidateOperationResponse(op, got); err != nil {
		t.Fatal(err)
	}
	want := []any{status, strconv.FormatUint(f.r.now, 10), strconv.Itoa(count), strconv.Itoa(more)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response %v, want %v", got, want)
	}
	maintenanceLuaTrace(t, f.r)
}

func maintenanceLuaReject(t *testing.T, f *recordsLuaFixture, op OperationName, keys, args []string) ErrorCode {
	t.Helper()
	before := f.r.snapshot()
	result := stageOpsRun(t, &stageOpsRedis{f.r}, maintenanceLuaSource(t, op), keys, args)
	if result.runtimeErr != nil {
		t.Fatalf("not a redacted prewrite error: %v", result.runtimeErr)
	}
	reply, ok := result.raw.(bootLuaErrorReply)
	if !ok {
		t.Fatalf("expected closed rejection, got %v", result.raw)
	}
	code, err := ParseErrorCode(strings.TrimPrefix(string(reply), "ERR CRAWL_V2_"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
		t.Fatal("rejection changed state or attempted a write")
	}
	maintenanceLuaTrace(t, f.r)
	return code
}

func TestMaintenanceLuaPromoteMaximumAndReplay(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaNew(t, 101, false)
	maintenanceLuaDelay(t, f, 101)
	before := f.r.snapshot()
	keys, args := maintenanceLuaWire(t, f, OperationPromoteDue, "")
	maintenanceLuaReply(t, f, OperationPromoteDue, keys, args, "BATCH_MORE", 100, 1)
	ordered := append([]SourceJob(nil), f.jobs...)
	sort.Slice(ordered, func(i, j int) bool {
		a, b := before.zsets[runLuaKey("delayed")][string(ordered[i].JobID)], before.zsets[runLuaKey("delayed")][string(ordered[j].JobID)]
		return a < b || a == b && ordered[i].JobID < ordered[j].JobID
	})
	for i, job := range ordered {
		v := f.r.data[runLuaKey("job:"+string(job.JobID))].hash
		maintenanceLuaJobRecord(t, f.r, job.JobID)
		if i < 100 {
			priority, _ := strconv.ParseFloat(string(job.ScoreText), 64)
			if v["state"] != "ready" || v["not_before_ms"] != "0" || f.r.zsets[runLuaKey("ready")][string(job.JobID)] != priority ||
				f.r.zsets[runLuaKey("ready_at")][string(job.JobID)] != float64(f.r.now) {
				t.Fatal("priority/age/prefix promotion mismatch")
			}
		} else if v["state"] != "delayed" {
			t.Fatal("promoted outside the bounded selected prefix")
		}
	}
	runLuaRecord(t, f.r)
	maintenanceLuaReply(t, f, OperationPromoteDue, keys, args, "BATCH_DONE", 1, 0)
	before = f.r.snapshot()
	f.r.maximum, f.r.denyAt = 1, 1
	maintenanceLuaReply(t, f, OperationPromoteDue, keys, args, "BATCH_DONE", 0, 0)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("idle replay mutated state")
	}
}

func TestMaintenanceLuaLateJobAndLastACLFailBeforeWrites(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationPromoteDue, OperationCancelBatch} {
		t.Run(string(op), func(t *testing.T) {
			fixture := func() *recordsLuaFixture {
				f := maintenanceLuaNew(t, 3, false)
				maintenanceLuaDelay(t, f, 3)
				if op == OperationCancelBatch {
					v := f.r.data[runLuaKey("")].hash
					v["state"], v["terminal_reason"], v["cancelled_at_ms"] = "cancelled", "source_cancelled", strconv.FormatUint(f.r.now-10, 10)
					v["last_activity_at_ms"] = v["cancelled_at_ms"]
				}
				return f
			}
			f := fixture()
			keys, args := maintenanceLuaWire(t, f, op, "")
			maintenanceLuaReply(t, f, op, keys, args, "BATCH_DONE", 3, 0)
			lastACL := f.r.aclCount
			f = fixture()
			f.r.denyAt = lastACL
			maintenanceLuaReject(t, f, op, keys, args)
			if f.r.aclCount != lastACL {
				t.Fatal("did not reach the last ACL denial")
			}
			f = fixture()
			delete(f.r.data[runLuaKey("job:"+string(f.jobs[2].JobID))].hash, "lease_request_starts_baseline")
			maintenanceLuaReject(t, f, op, keys, args)
			f = fixture()
			f.r.failAt, f.r.failAfter = 2, true
			result := recordsLuaRun(t, f.r, maintenanceLuaSource(t, op), keys, args)
			if result.runtimeErr == nil || f.r.attempts != 2 || f.r.writes != 2 {
				t.Fatal("post-write integrity failure was hidden or rolled back")
			}
		})
	}
}

func TestMaintenanceLuaCancelBoundOrderingAndFrozenGroups(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaNew(t, 103, false)
	maintenanceLuaDelay(t, f, 4)
	v := f.r.data[runLuaKey("")].hash
	v["state"], v["terminal_reason"], v["cancelled_at_ms"] = "cancelled", "operator_cancelled", strconv.FormatUint(f.r.now-1, 10)
	v["last_activity_at_ms"] = v["cancelled_at_ms"]
	f.r.now = f.input.AuthorizationExpiresAtMS // stored cancellation still wins
	keys, args := maintenanceLuaWire(t, f, OperationCancelBatch, "")
	maintenanceLuaReply(t, f, OperationCancelBatch, keys, args, "BATCH_MORE", 100, 1)
	if len(f.r.zsets[runLuaKey("ready")]) != 0 || len(f.r.zsets[runLuaKey("delayed")]) != 3 ||
		v["open_job_count"] != "3" || v["cancelled_total"] != "100" ||
		f.r.data[runLuaKey("group_open_jobs")].hash[string(f.input.PolicyGroups[0].GroupID)] != "3" ||
		f.r.data[runLuaKey("disposition_reason_counts")].hash["operator_cancelled"] != "100" {
		t.Fatal("cancellation did not aggregate shared counters / select ready first")
	}
	for _, job := range f.jobs {
		maintenanceLuaJobRecord(t, f.r, job.JobID)
	}
	runLuaRecord(t, f.r)
	maintenanceLuaReply(t, f, OperationCancelBatch, keys, args, "BATCH_DONE", 3, 0)
	before := f.r.snapshot()
	maintenanceLuaReply(t, f, OperationCancelBatch, keys, args, "BATCH_DONE", 0, 0)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("repeated cancellation changed records")
	}
}

func TestMaintenanceLuaCleanStaleEarlyAndExpiredEmpty(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"absent", "other-first", "future", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			f := maintenanceLuaNew(t, 0, false)
			id := strings.Repeat("c", 64)
			keys, args := maintenanceLuaWire(t, f, OperationCleanStage, "")
			want, count, more := "BATCH_DONE", 0, 0
			switch scenario {
			case "other-first":
				f.r.setZSet(StageExpiryKey, map[string]float64{strings.Repeat("a", 64): float64(f.r.now), id: float64(f.r.now)})
				want, more = "BATCH_MORE", 1
			case "future":
				f.r.setZSet(StageExpiryKey, map[string]float64{id: float64(f.r.now + 1)})
				args[len(args)-1] = strconv.FormatUint(f.r.now+1, 10)
			case "expired":
				f.r.setZSet(StageExpiryKey, map[string]float64{id: float64(f.r.now)})
				count = 1
			}
			before := f.r.snapshot()
			maintenanceLuaReply(t, f, OperationCleanStage, keys, args, want, count, more)
			if count == 0 && !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("stale/early pair mutated state")
			}
			for _, call := range f.r.trace {
				if count == 0 && len(call.args) > 0 && strings.HasPrefix(call.args[0], "mifolyo:crawl:v2:stage:") {
					t.Fatal("stale/early pair read a bundle instead of yielding")
				}
			}
		})
	}
}

func TestMaintenanceLuaRateReadOnlyMaximumAndCursor(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaNew(t, 0, false)
	index := map[string]float64{}
	for i := 0; i < 101; i++ {
		r := requestLuaRateRecord(t, "group", fmt.Sprintf("%032x", i+1))
		for j := range r {
			switch r[j].Name {
			case "active_count", "pending_count", "started_count", "next_allowed_ms", "last_started_at_ms":
				r[j].Value = []byte("0")
			case "updated_at_ms":
				r[j].Value = []byte(strconv.FormatUint(f.r.now-1, 10))
			}
		}
		if err := ValidateRecord(SchemaRateScope, r); err != nil {
			t.Fatal(err)
		}
		id := string(r[rateScopeIDIndex].Value)
		f.r.setHash("mifolyo:crawl:v2:rate:"+id, r)
		index[id] = float64(f.r.now - 1)
	}
	f.r.setZSet(RateScopesKey, index)
	keys, args := maintenanceLuaWire(t, f, OperationMaintainRateScopes, "")
	before := f.r.snapshot()
	f.r.maximum, f.r.denyAt = 1, 1
	maintenanceLuaReply(t, f, OperationMaintainRateScopes, keys, args, "BATCH_MORE", 100, 1)
	args[len(args)-1] = "100"
	maintenanceLuaReply(t, f, OperationMaintainRateScopes, keys, args, "BATCH_DONE", 1, 0)
	args[len(args)-1] = "9007199254740991"
	maintenanceLuaReply(t, f, OperationMaintainRateScopes, keys, args, "BATCH_DONE", 0, 0)
	if !reflect.DeepEqual(before, f.r.snapshot()) || f.r.aclCount != 0 {
		t.Fatal("diagnostic integrity pass wrote / performed admission")
	}
}

func maintenanceLuaPurgeFixture(t *testing.T, n int, candidate bool) *recordsLuaFixture {
	t.Helper()
	f := maintenanceLuaNew(t, n, candidate)
	at := strconv.FormatUint(f.r.now-50, 10)
	v := f.r.data[runLuaKey("")].hash
	v["state"], v["terminal_reason"], v["cancelled_at_ms"], v["last_activity_at_ms"] = "cancelled", "operator_cancelled", at, at
	v["open_job_count"], v["cancelled_total"], v["last_terminal_transition_at_ms"] = "0", strconv.Itoa(n), at
	f.r.data[runLuaKey("group_open_jobs")].hash[string(f.input.PolicyGroups[0].GroupID)] = "0"
	f.r.data[runLuaKey("disposition_reason_counts")].hash["operator_cancelled"] = strconv.Itoa(n)
	cancelled := map[string]float64{}
	for _, source := range f.jobs {
		job := f.r.data[runLuaKey("job:"+string(source.JobID))].hash
		job["state"], job["last_reason"], job["cancelled_at_ms"], job["updated_at_ms"] = "cancelled", "operator_cancelled", at, at
		cancelled[string(source.JobID)] = float64(f.r.now - 50)
		maintenanceLuaJobRecord(t, f.r, source.JobID)
	}
	f.r.removeKey(runLuaKey("ready"))
	f.r.removeKey(runLuaKey("ready_at"))
	f.r.setZSet(runLuaKey("cancelled"), cancelled)
	if !candidate {
		v["state"], v["finalized_at_ms"], v["retention_anchor_ms"], v["last_activity_at_ms"] = "archived", at, at, at
		archived := f.r.now - 50 + 30*86400000
		v["archived_at_ms"], v["archive_sha256"] = strconv.FormatUint(archived, 10), strings.Repeat("b", 64)
		f.r.now = archived + 7*86400000
		f.r.removeKey(ActiveRunsKey)
		f.r.removeKey(UnarchivedRunsKey)
	}
	runLuaRecord(t, f.r)
	return f
}

func TestMaintenanceLuaPurgeResumeFreezeAndFinalEvidence(t *testing.T) {
	t.Parallel()
	for _, candidate := range []bool{false, true} {
		t.Run(fmt.Sprintf("candidate=%t", candidate), func(t *testing.T) {
			f := maintenanceLuaPurgeFixture(t, 101, candidate)
			keys, args := maintenanceLuaWire(t, f, OperationPurgeRunBatch, f.jobs[0].JobID)
			frozen := map[string]string{}
			for k, v := range f.r.data[runLuaKey("")].hash {
				frozen[k] = v
			}
			maintenanceLuaReply(t, f, OperationPurgeRunBatch, keys, args, "BATCH_MORE", 100, 1)
			v := f.r.data[runLuaKey("")].hash
			if v["purge_state"] != "in_progress" || v["purged_job_count"] != "100" || v["purge_evidence_sha256"] != strings.Repeat("b", 64) ||
				len(f.r.sets[runLuaKey("jobs")]) != 1 || len(f.r.zsets[runLuaKey("job_order")]) != 1 {
				t.Fatal("missing explicit bounded purge progress")
			}
			for name, value := range frozen {
				if !strings.HasPrefix(name, "purge") && v[name] != value {
					t.Fatalf("purge rewrote frozen original evidence %s", name)
				}
			}
			runLuaRecord(t, f.r)
			maintenanceLuaReject(t, f, OperationPurgeRunBatch, keys, args) // ambiguous nonfinal stale first ID
			keys, args = maintenanceLuaWire(t, f, OperationPurgeRunBatch, f.jobs[100].JobID)
			changed := append([]string(nil), args...)
			changed[len(changed)-2] = strings.Repeat("d", 64)
			maintenanceLuaReject(t, f, OperationPurgeRunBatch, keys, changed)
			maintenanceLuaReply(t, f, OperationPurgeRunBatch, keys, args, "PURGED", 1, 0)
			before := f.r.snapshot()
			f.r.maximum, f.r.denyAt = 1, 1
			maintenanceLuaReply(t, f, OperationPurgeRunBatch, keys, args, "PURGED", 0, 0)
			if !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("final replay mutated keys")
			}
			// A single surviving exact run key defeats external-evidence replay.
			f.r.setHash(runLuaKey("visited_urls"), Record{textField(strings.Repeat("a", 64), "https://example.com/")})
			maintenanceLuaReject(t, f, OperationPurgeRunBatch, keys, args)
		})
	}
}

func TestMaintenanceLuaPurgeRetentionAndLateCorruption(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaPurgeFixture(t, 2, false)
	keys, args := maintenanceLuaWire(t, f, OperationPurgeRunBatch, f.jobs[0].JobID)
	f.r.now-- // seven days is inclusive, one millisecond early is not
	maintenanceLuaReject(t, f, OperationPurgeRunBatch, keys, args)
	f.r.now++
	delete(f.r.data[runLuaKey("job:"+string(f.jobs[1].JobID))].hash, "last_stage_fence")
	maintenanceLuaReject(t, f, OperationPurgeRunBatch, keys, args)
	if _, present := f.r.data[runLuaKey("job:"+string(f.jobs[0].JobID))]; !present {
		t.Fatal("late bad job partially deleted prefix")
	}
}

func TestMaintenanceLuaRecoveryBranchRecords(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		pre, starts int
		cancel      string
		expired     bool
		state       string
		reason      string
		delay       uint64
	}{
		{0, 0, "", false, "ready", "none", 0}, {1, 0, "", false, "ready", "none", 0},
		{2, 0, "", false, "dead", "pre_io_recovery_exhausted", 0},
		{0, 1, "", false, "delayed", "lease_expired_after_io", 30000},
		{0, 2, "", false, "delayed", "lease_expired_after_io", 120000},
		{0, 3, "", false, "dead", "retry_exhausted", 0},
		{2, 0, "source_cancelled", true, "cancelled", "source_cancelled", 0},
		{0, 3, "", true, "cancelled", "authorization_expired", 0},
	} {
		t.Run(fmt.Sprintf("pre%d-starts%d-%s-expired%t", scenario.pre, scenario.starts, scenario.cancel, scenario.expired), func(t *testing.T) {
			f := maintenanceLuaNew(t, 1, false)
			if scenario.expired {
				f.r.now = f.input.AuthorizationExpiresAtMS
			}
			if scenario.cancel != "" {
				v := f.r.data[runLuaKey("")].hash
				v["state"], v["terminal_reason"], v["cancelled_at_ms"] = "cancelled", scenario.cancel, strconv.FormatUint(f.r.now-1, 10)
				v["last_activity_at_ms"] = v["cancelled_at_ms"]
			}
			record := recordsLuaInitial(t, f.jobs[0], bootLuaNow-100)
			claims := scenario.pre + max(1, scenario.starts)
			values := map[string]string{"state": "leased", "lease_owner": strings.Repeat("d", 32), "lease_token": strings.Repeat("e", 64),
				"lease_fence": strconv.Itoa(claims), "claim_count": strconv.Itoa(claims), "next_request_ordinal": strconv.Itoa(claims + 1),
				"pre_io_recoveries": strconv.Itoa(scenario.pre), "delivery_attempts": strconv.Itoa(scenario.starts), "request_starts": strconv.Itoa(scenario.starts),
				"lease_started_at_ms": strconv.FormatUint(bootLuaNow-80, 10), "lease_expires_at_ms": strconv.FormatUint(bootLuaNow, 10),
				"updated_at_ms": strconv.FormatUint(bootLuaNow-10, 10)}
			if scenario.starts > 0 {
				values["lease_request_starts_baseline"], values["lease_delivery_started"] = strconv.Itoa(scenario.starts-1), "1"
				values["last_request_started_at_ms"] = strconv.FormatUint(bootLuaNow-20, 10)
			}
			for i := range record {
				if v, ok := values[record[i].Name]; ok {
					record[i].Value = []byte(v)
				}
			}
			if err := ValidateRecord(SchemaJob, record); err != nil {
				t.Fatal(err)
			}
			keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
			// A pure branch/record oracle, NOT a success stand-in for the recovery
			// handler. Real execution/memory/ownership is tested independently.
			source := maintenanceLuaCore(t) + `
local ctx,code=CJ.Maintenance.open("CJ2_RECOVER_EXPIRED",KEYS,ARGV)
if not ctx then return CJ.Context.reject(code) end
local run; run,code=CJ.Run.load(ctx,ctx.request.v.run_id)
if not run then return CJ.Context.reject(code) end
local job=CJ.Schemas.decode("job",` + jobLuaQuote(primitiveLuaEncoded(t, record)) + `)
local outcome; outcome,code=CJ.Maintenance.recovery_outcome(ctx,run,job)
if not outcome then return CJ.Context.reject(code) end
local changes={state=outcome.state,last_reason=outcome.reason,last_transition_id="",last_transition_status=""}
if outcome.failure_reason then changes.last_failure_reason=outcome.failure_reason end
if outcome.pre_io_recoveries then changes.pre_io_recoveries=P.format_decimal(outcome.pre_io_recoveries) end
if outcome.not_before_ms then changes.not_before_ms=outcome.not_before_ms end
if outcome.retry then changes.retry_count=P.format_decimal(job.n.retry_count+1) end
local post; post,code=CJ.Job.outcome_record(ctx,job,changes)
if not post then return CJ.Context.reject(code) end
return CJ.Schemas.encode(post)`
			before := f.r.snapshot()
			got := sharedLuaNoError(t, recordsLuaRun(t, f.r, source, keys, args)).(string)
			names, _ := RecordSchemaFields(SchemaJob)
			decoded, err := decodeExactRecord([]byte(got), names, len(got))
			if err != nil || ValidateRecord(SchemaJob, decoded) != nil {
				t.Fatalf("Go record oracle rejected recovery proposal: %v", err)
			}
			if string(decoded[jobStateIndex].Value) != scenario.state || string(decoded[jobLastReasonIndex].Value) != scenario.reason {
				t.Fatal("wrong deterministic recovery disposition")
			}
			if scenario.delay > 0 && string(decoded[jobNotBeforeMSIndex].Value) != strconv.FormatUint(f.r.now+scenario.delay, 10) {
				t.Fatal("wrong exact retry interval")
			}
			for _, i := range []int{jobLeaseRequestStartsBaselineIndex, jobRequestStartsIndex, jobLeaseFenceIndex, jobLastStageCommitIDIndex, jobLastStageFenceIndex} {
				if decoded[i].Name != record[i].Name || string(decoded[i].Value) != string(record[i].Value) {
					t.Fatal("recovery proposal reset retained fence/history")
				}
			}
			if !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("pure proposal test wrote state")
			}
		})
	}
}

func maintenanceLuaLeased(t *testing.T, n int, reservationState string) *recordsLuaFixture {
	t.Helper()
	f := maintenanceLuaNew(t, n, false)
	leases, ages, global := map[string]float64{}, map[string]float64{}, map[string]float64{}
	scopes := map[string]Record{}
	active, pending, started := map[string]map[string]float64{}, map[string]map[string]float64{}, map[string]map[string]float64{}
	for i, source := range f.jobs {
		id := string(source.JobID)
		v := f.r.data[runLuaKey("job:"+id)].hash
		v["state"], v["claim_count"], v["lease_fence"], v["next_request_ordinal"] = "leased", "1", "1", "2"
		v["lease_owner"], v["lease_token"] = strings.Repeat("d", 32), fmt.Sprintf("%064x", i+1)
		v["lease_started_at_ms"], v["lease_expires_at_ms"], v["updated_at_ms"] = strconv.FormatUint(f.r.now-80, 10), strconv.FormatUint(f.r.now, 10), strconv.FormatUint(f.r.now-10, 10)
		leases[id], ages[id], global[runLuaID+":"+id] = float64(f.r.now), float64(f.r.now-80), float64(f.r.now)
		if reservationState != "" {
			intent := ReservationIntent{Lease: LeaseIdentity{RunID: f.input.RunID, JobID: source.JobID, OwnerID: OwnerID(v["lease_owner"]), Token: LeaseToken(v["lease_token"]), Fence: 1},
				RequestOrdinal: 1, Target: RequestTarget{URLID: source.JobID, CanonicalURL: source.CanonicalURL}, CrawlPolicyDigest: f.input.CrawlPolicySHA256, Decision: source.Decision}
			q, err := DeriveReservationID(f.authority, intent)
			if err != nil {
				t.Fatal(err)
			}
			req, err := NewReserveRequestWireRequest(runLuaGate(t, f.a, OperationReserveRequest, false), f.authority, intent)
			if err != nil {
				t.Fatal(err)
			}
			fields := operationWireSemanticValues(req.semantic)
			fields["lease_fence"] = fields["fence"]
			fields["reservation_id"], fields["created_at_ms"], fields["expires_at_ms"] = string(q), v["lease_started_at_ms"], v["lease_expires_at_ms"]
			record := recordAuthorityReservationRecord(t, reservationState)
			for j := range record {
				if value, ok := fields[record[j].Name]; ok {
					record[j].Value = []byte(value)
				}
			}
			v["active_reservation_id"] = string(q)
			if reservationState == "started" {
				at := strconv.FormatUint(f.r.now-20, 10)
				v["delivery_attempts"], v["request_starts"], v["lease_delivery_started"] = "1", "1", "1"
				v["last_request_started_at_ms"], v["last_document_request_started_at_ms"], v["last_document_request_fence"] = at, at, "1"
				v["last_document_target_url_id"], v["last_document_target_url"], v["last_document_target_digest"] = id, source.CanonicalURL, string(source.Decision.TargetDigest)
				for j := range record {
					switch record[j].Name {
					case "started_at_ms":
						record[j].Value = []byte(at)
					case "delivery_attempts_after_start", "job_starts_after_start":
						record[j].Value = []byte("1")
					case "run_starts_after_start", "group_starts_after_start":
						record[j].Value = []byte(strconv.Itoa(i + 1))
					}
				}
			}
			if err := ValidateRecord(SchemaReservation, record); err != nil {
				t.Fatal(err)
			}
			f.r.setHash("mifolyo:crawl:v2:reservation:"+string(q), record)
			for _, kind := range []string{"global", "group", "origin"} {
				sid := fields[kind+"_scope_id"]
				if scopes[sid] == nil {
					witness := "global"
					if kind == "group" {
						witness = string(source.RateScopeID)
					}
					if kind == "origin" {
						origin, err := DeriveCanonicalOrigin(source.CanonicalURL)
						if err != nil {
							t.Fatal(err)
						}
						witness = string(origin)
					}
					r := requestLuaRateRecord(t, kind, witness)
					for j := range r {
						switch r[j].Name {
						case "effective_concurrency":
							r[j].Value = []byte("2")
						case "effective_interval_ms", "active_count", "pending_count", "started_count", "next_allowed_ms", "last_started_at_ms":
							r[j].Value = []byte("0")
						case "updated_at_ms":
							r[j].Value = []byte(strconv.FormatUint(f.r.now-10, 10))
						}
					}
					scopes[sid], active[sid], pending[sid], started[sid] = r, map[string]float64{}, map[string]float64{}, map[string]float64{}
				}
				active[sid][string(q)] = float64(f.r.now)
				if reservationState == "pending" {
					pending[sid][string(q)] = float64(f.r.now)
				} else {
					started[sid][string(q)] = float64(f.r.now)
				}
			}
		}
		maintenanceLuaJobRecord(t, f.r, source.JobID)
	}
	f.r.removeKey(runLuaKey("ready"))
	f.r.removeKey(runLuaKey("ready_at"))
	f.r.setZSet(runLuaKey("leased"), leases)
	f.r.setZSet(runLuaKey("leased_at"), ages)
	f.r.setZSet(ActiveLeasesKey, global)
	v := f.r.data[runLuaKey("")].hash
	v["claims_total"], v["reservation_creations_total"], v["last_activity_at_ms"] = strconv.Itoa(n), strconv.Itoa(n), strconv.FormatUint(f.r.now-10, 10)
	group := string(f.input.PolicyGroups[0].GroupID)
	if reservationState == "pending" {
		v["pending_request_reservations"] = strconv.Itoa(n)
		f.r.data[runLuaKey("group_pending")].hash[group] = strconv.Itoa(n)
	} else if reservationState == "started" {
		v["request_starts"], v["started_request_reservations"], v["last_request_started_at_ms"] = strconv.Itoa(n), strconv.Itoa(n), strconv.FormatUint(f.r.now-20, 10)
		f.r.data[runLuaKey("group_started")].hash[group], f.r.data[runLuaKey("group_active_started")].hash[group] = strconv.Itoa(n), strconv.Itoa(n)
	}
	inventory := map[string]float64{}
	for sid, record := range scopes {
		for j := range record {
			switch record[j].Name {
			case "active_count":
				record[j].Value = []byte(strconv.Itoa(len(active[sid])))
			case "pending_count":
				record[j].Value = []byte(strconv.Itoa(len(pending[sid])))
			case "started_count":
				record[j].Value = []byte(strconv.Itoa(len(started[sid])))
			case "last_started_at_ms":
				if reservationState == "started" {
					record[j].Value = []byte(strconv.FormatUint(f.r.now-20, 10))
				}
			case "next_allowed_ms":
				if reservationState == "started" && sid != string(DeriveGlobalScopeID()) {
					record[j].Value = []byte(strconv.FormatUint(f.r.now-20, 10))
				}
			}
		}
		if err := ValidateRecord(SchemaRateScope, record); err != nil {
			t.Fatal(err)
		}
		key := "mifolyo:crawl:v2:rate:" + sid
		f.r.setHash(key, record)
		f.r.setZSet(key+":active", active[sid])
		f.r.setZSet(key+":pending", pending[sid])
		f.r.setZSet(key+":started", started[sid])
		inventory[sid] = float64(f.r.now - 10)
	}
	f.r.setZSet(RateScopesKey, inventory)
	runLuaRecord(t, f.r)
	return f
}

func TestMaintenanceLuaRecoveryMaximumAndSharedScopes(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"", "pending", "started"} {
		t.Run("reservation="+state, func(t *testing.T) {
			n := 2
			if state == "" {
				n = 64
			}
			f := maintenanceLuaLeased(t, n, state)
			keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
			maintenanceLuaReply(t, f, OperationRecoverExpired, keys, args, "BATCH_DONE", n, 0)
			v := f.r.data[runLuaKey("")].hash
			if v["recovered_leases_total"] != strconv.Itoa(n) || v["pending_request_reservations"] != "0" || v["started_request_reservations"] != "0" ||
				len(f.r.zsets[ActiveLeasesKey]) != 0 || len(f.r.zsets[runLuaKey("leased")]) != 0 {
				t.Fatal("incomplete aggregate expiry/recovery")
			}
			for _, source := range f.jobs {
				maintenanceLuaJobRecord(t, f.r, source.JobID)
			}
			for key, entry := range f.r.data {
				if strings.HasPrefix(key, "mifolyo:crawl:v2:rate:") && entry.kind == "hash" && entry.hash["active_count"] != "" && entry.hash["active_count"] != "0" {
					t.Fatal("shared scope release overwritten from original counter")
				}
				if strings.HasPrefix(key, "mifolyo:crawl:v2:reservation:") && entry.hash["reservation_id"] != "" && (entry.hash["state"] != "expired" || entry.expireAt != int64(f.r.now+86400000)) {
					t.Fatal("wrong terminal reservation/TTL")
				}
			}
			runLuaRecord(t, f.r)
			before := f.r.snapshot()
			maintenanceLuaReply(t, f, OperationRecoverExpired, keys, args, "BATCH_DONE", 0, 0)
			if !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("recovery replay repeated counters")
			}
		})
	}
}

func maintenanceLuaStage(t *testing.T, f *recordsLuaFixture, index int) (Digest, uint64) {
	t.Helper()
	id := f.jobs[index].JobID
	job := f.r.data[runLuaKey("job:"+string(id))].hash
	lease := LeaseIdentity{RunID: f.input.RunID, JobID: id, OwnerID: OwnerID(job["lease_owner"]), Token: LeaseToken(job["lease_token"]), Fence: 1}
	output := Digest(strings.Repeat("7", 64))
	pub, err := DerivePublicationID(PublicationIdentity{RunID: f.input.RunID, JobID: id, Fence: 1, OutputDigest: output})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := DeriveCommitID(CommitIdentity{RunID: f.input.RunID, JobID: id, OwnerID: lease.OwnerID, Token: lease.Token, Fence: 1,
		PublicationID: pub, RequestStartsBaseline: 0, RequestStartsGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	token, err := DeriveTokenDigest(lease)
	if err != nil {
		t.Fatal(err)
	}
	// The worker finished this exact request before BEGIN. Retain its tombstone
	// and cumulative starts; remove only its three capacity memberships.
	q := job["active_reservation_id"]
	if q != "" {
		key := "mifolyo:crawl:v2:reservation:" + q
		e := f.r.data[key]
		e.hash["state"], e.hash["terminal_at_ms"] = "finished", strconv.FormatUint(f.r.now-6, 10)
		e.expireAt = int64(f.r.now - 6 + 86400000)
		f.r.data[key] = e
		for _, kind := range []string{"global", "group", "origin"} {
			sid := e.hash[kind+"_scope_id"]
			scopeKey := "mifolyo:crawl:v2:rate:" + sid
			for _, suffix := range []string{"active", "started"} {
				delete(f.r.zsets[scopeKey+":"+suffix], q)
				if len(f.r.zsets[scopeKey+":"+suffix]) == 0 {
					f.r.removeKey(scopeKey + ":" + suffix)
				}
			}
			sv := f.r.data[scopeKey].hash
			for _, name := range []string{"active_count", "started_count"} {
				n, _ := strconv.Atoi(sv[name])
				sv[name] = strconv.Itoa(n - 1)
			}
			sv["updated_at_ms"] = e.hash["terminal_at_ms"]
			f.r.zsets[RateScopesKey][sid] = float64(f.r.now - 6)
		}
		rv := f.r.data[runLuaKey("")].hash
		n, _ := strconv.Atoi(rv["started_request_reservations"])
		rv["started_request_reservations"] = strconv.Itoa(n - 1)
		gv := f.r.data[runLuaKey("group_active_started")].hash
		n, _ = strconv.Atoi(gv[e.hash["group_id"]])
		gv[e.hash["group_id"]] = strconv.Itoa(n - 1)
	}
	job["active_reservation_id"], job["active_stage_commit_id"], job["last_stage_commit_id"], job["last_stage_fence"] = "", string(commit), string(commit), "1"
	job["updated_at_ms"] = strconv.FormatUint(f.r.now-5, 10)
	f.r.data[runLuaKey("")].hash["last_activity_at_ms"] = job["updated_at_ms"]
	meta := recordAuthorityUnsealedStageRecord(t, "0")
	due := f.r.now - 5 + 900000
	values := map[string]string{"run_id": runLuaID, "job_id": string(id), "owner_id": string(lease.OwnerID), "lease_fence": "1",
		"token_digest": string(token), "commit_id": string(commit), "publication_id": string(pub), "output_digest": string(output),
		"created_at_ms": job["updated_at_ms"], "expires_at_ms": strconv.FormatUint(due, 10), "request_starts_baseline": "0", "request_starts_generation": "1"}
	for i := range meta {
		if v, ok := values[meta[i].Name]; ok {
			meta[i].Value = []byte(v)
		}
	}
	if err := ValidateRecord(SchemaStageMeta, meta); err != nil {
		t.Fatal(err)
	}
	base := "mifolyo:crawl:v2:stage:" + string(commit) + ":"
	f.r.setHash(base+"meta", meta)
	f.r.setList(base+"keys", []string{base + "meta", base + "keys"})
	for _, key := range []string{base + "meta", base + "keys"} {
		e := f.r.data[key]
		e.expireAt = int64(due)
		f.r.data[key] = e
	}
	if _, exists := f.r.data[StageSlotsKey]; !exists {
		f.r.setHash(StageSlotsKey, Record{textField(string(commit), "32768:"+runLuaID+":"+string(id)+":1:0")})
	} else {
		f.r.data[StageSlotsKey].hash[string(commit)] = "32768:" + runLuaID + ":" + string(id) + ":1:0"
	}
	if _, exists := f.r.data[StageExpiryKey]; !exists {
		f.r.setZSet(StageExpiryKey, map[string]float64{string(commit): float64(due)})
	} else {
		f.r.zsets[StageExpiryKey][string(commit)] = float64(due)
	}
	maintenanceLuaJobRecord(t, f.r, id)
	return commit, due
}

func TestMaintenanceLuaCleanupOwnerBlocksAtCommonExpiry(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaLeased(t, 1, "started")
	commit, due := maintenanceLuaStage(t, f, 0)
	f.r.now = due
	// Model Redis expiration before taking the no-write snapshot.
	base := "mifolyo:crawl:v2:stage:" + string(commit) + ":"
	f.r.removeKey(base + "meta")
	f.r.removeKey(base + "keys")
	request, err := NewCleanStageWireRequest(runLuaGate(t, f.a, OperationCleanStage, false), CleanStageWireInput{ExpectedCommitID: commit, ExpectedCleanupDueAtMS: due})
	keys, args := runLuaParts(t, request, err)
	before := f.r.snapshot()
	maintenanceLuaReply(t, f, OperationCleanStage, keys, args, "BATCH_MORE", 0, 1)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("cleanup stole the overdue owner's terminal floor")
	}
	f.r.data[StageSlotsKey].hash[string(commit)] = "32000:" + runLuaID + ":" + string(f.jobs[0].JobID) + ":1:2"
	maintenanceLuaReject(t, f, OperationCleanStage, keys, args)
}

func TestMaintenanceLuaRecoverySlotFloorAndExpiredMetadata(t *testing.T) {
	t.Parallel()
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("metadataExpired=%t", expired), func(t *testing.T) {
			f := maintenanceLuaLeased(t, 1, "started")
			commit, due := maintenanceLuaStage(t, f, 0)
			metaKey := "mifolyo:crawl:v2:stage:" + string(commit) + ":meta"
			if expired {
				f.r.now = due
				f.r.removeKey(metaKey)
				f.r.removeKey("mifolyo:crawl:v2:stage:" + string(commit) + ":keys")
			}
			keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
			// No ordinary/safety allocation headroom: the exact owning slot must
			// cover its materialization without charging those bytes a second time.
			f.r.maximum = f.r.used + 32768 + 67108864
			maintenanceLuaReply(t, f, OperationRecoverExpired, keys, args, "BATCH_DONE", 1, 0)
			if _, present := f.r.data[StageSlotsKey]; present {
				t.Fatal("recovery retained/recreated the terminal slot")
			}
			if !expired && f.r.data[metaKey].hash["abandoned"] != "1" {
				t.Fatal("live stage metadata was not abandoned")
			}
			if f.r.zsets[StageExpiryKey][string(commit)] != float64(due) {
				t.Fatal("recovery changed the common physical cleanup deadline")
			}
			maintenanceLuaJobRecord(t, f.r, f.jobs[0].JobID)
			last := f.r.trace[len(f.r.trace)-1]
			if last.name != "HDEL" || last.args[0] != StageSlotsKey {
				t.Fatal("slot was released before covered mutations")
			}
		})
	}
	f := maintenanceLuaLeased(t, 1, "started")
	commit, _ := maintenanceLuaStage(t, f, 0)
	f.r.data[StageSlotsKey].hash[string(commit)] = "1:" + runLuaID + ":" + string(f.jobs[0].JobID) + ":1:0"
	keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
	maintenanceLuaReject(t, f, OperationRecoverExpired, keys, args)
}

func TestMaintenanceLuaRecoveryAbortedTerminalReservation(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaLeased(t, 1, "started")
	commit, _ := maintenanceLuaStage(t, f, 0)
	id := f.jobs[0].JobID
	job := f.r.data[runLuaKey("job:"+string(id))].hash
	transition, err := DeriveAbortStageTransitionID(AbortStageTransitionInput{Lease: LeaseIdentity{RunID: f.input.RunID,
		JobID: id, OwnerID: OwnerID(job["lease_owner"]), Token: LeaseToken(job["lease_token"]), Fence: 1}, CommitID: commit})
	if err != nil {
		t.Fatal(err)
	}
	job["active_stage_commit_id"], job["last_transition_status"], job["last_transition_id"] = "", "STAGE_ABORTED", string(transition)
	job["updated_at_ms"] = strconv.FormatUint(f.r.now-2, 10)
	f.r.data[runLuaKey("")].hash["last_activity_at_ms"] = job["updated_at_ms"]
	for _, key := range wireOracleStageKeys(commit) {
		f.r.removeKey(key)
	}
	f.r.removeKey(StageExpiryKey)
	// A valid abort may retain more than 32 KiB. The actual recovery cost,
	// not the size of the remaining reservation, is limited to that floor.
	f.r.data[StageSlotsKey].hash[string(commit)] = "65536:" + runLuaID + ":" + string(id) + ":1:2"
	maintenanceLuaJobRecord(t, f.r, id)
	keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
	maintenanceLuaReply(t, f, OperationRecoverExpired, keys, args, "BATCH_DONE", 1, 0)
	if _, present := f.r.data[StageSlotsKey]; present {
		t.Fatal("aborted terminal reservation was not released")
	}
	if job["last_stage_commit_id"] != string(commit) || job["last_stage_fence"] != "1" {
		t.Fatal("recovery erased abort/fence history")
	}
}

func TestMaintenanceLuaNeverFollowsInventoryCanary(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaNew(t, 0, false)
	id := strings.Repeat("c", 64)
	base := "mifolyo:crawl:v2:stage:" + id + ":"
	canary := "mifolyo:crawl:v2:run:" + strings.Repeat("a", 32) + ":job:" + strings.Repeat("b", 64)
	f.r.setHash(canary, Record{textField("secret", "leave untouched")})
	f.r.setList(base+"keys", []string{canary})
	f.r.setZSet(StageExpiryKey, map[string]float64{id: float64(f.r.now)})
	keys, args := maintenanceLuaWire(t, f, OperationCleanStage, "")
	maintenanceLuaReject(t, f, OperationCleanStage, keys, args)
	for _, call := range f.r.trace {
		for _, arg := range call.args {
			if arg == canary {
				t.Fatal("inventory text became a key permission/read/write")
			}
		}
	}
}

func TestMaintenanceLuaRateExpiredReservationNotPruned(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaLeased(t, 2, "pending")
	keys, args := maintenanceLuaWire(t, f, OperationMaintainRateScopes, "")
	before := f.r.snapshot()
	maintenanceLuaReply(t, f, OperationMaintainRateScopes, keys, args, "BATCH_DONE", len(f.r.zsets[RateScopesKey]), 0)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("rate diagnostic expired/pruned reservation capacity")
	}
	// Corrupt the last sorted selected scope, after earlier scopes validate.
	ids := []string{}
	for id := range f.r.zsets[RateScopesKey] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	last := "mifolyo:crawl:v2:rate:" + ids[len(ids)-1]
	f.r.data[last].hash["active_count"] = "0"
	maintenanceLuaReject(t, f, OperationMaintainRateScopes, keys, args)
}

func TestMaintenanceLuaRecoveryRejectsEntireLateBatchAndGlobalOverflow(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaLeased(t, 65, "")
	keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
	maintenanceLuaReject(t, f, OperationRecoverExpired, keys, args)
	f = maintenanceLuaLeased(t, 2, "pending")
	keys, args = maintenanceLuaWire(t, f, OperationRecoverExpired, "")
	second := f.r.data[runLuaKey("job:"+string(f.jobs[1].JobID))].hash
	id := second["active_reservation_id"]
	delete(f.r.data["mifolyo:crawl:v2:reservation:"+id].hash, "job_starts_after_start")
	maintenanceLuaReject(t, f, OperationRecoverExpired, keys, args)
}

func TestMaintenanceLuaUnrelatedCanariesSurviveEffectiveBatches(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationPromoteDue, OperationCancelBatch, OperationPurgeRunBatch} {
		t.Run(string(op), func(t *testing.T) {
			f := maintenanceLuaPurgeFixture(t, 1, false)
			if op != OperationPurgeRunBatch {
				f = maintenanceLuaNew(t, 1, false)
				maintenanceLuaDelay(t, f, 1)
				if op == OperationCancelBatch {
					v := f.r.data[runLuaKey("")].hash
					v["state"], v["terminal_reason"], v["cancelled_at_ms"], v["last_activity_at_ms"] = "cancelled", "source_cancelled", strconv.FormatUint(f.r.now, 10), strconv.FormatUint(f.r.now, 10)
				}
			}
			canaries := []string{"unrelated:private", "mifolyo:crawl:v2:run:" + strings.Repeat("a", 32) + ":job:" + strings.Repeat("b", 64),
				"mifolyo:crawl:v2:stage:" + strings.Repeat("a", 64) + ":page", "mifolyo:crawl:v2:reservation:" + strings.Repeat("f", 64)}
			for _, key := range canaries {
				f.r.setHash(key, Record{textField("private", "untouched")})
			}
			first := JobID("")
			status := "BATCH_DONE"
			if op == OperationPurgeRunBatch {
				first, status = f.jobs[0].JobID, "PURGED"
			}
			keys, args := maintenanceLuaWire(t, f, op, first)
			maintenanceLuaReply(t, f, op, keys, args, status, 1, 0)
			for _, key := range canaries {
				if f.r.data[key].hash["private"] != "untouched" {
					t.Fatal("unrelated key changed")
				}
			}
			for _, call := range f.r.trace {
				for _, key := range canaries {
					for _, arg := range call.args {
						if key == arg {
							t.Fatal("unrelated canary was read or used by a descriptor")
						}
					}
				}
			}
		})
	}
}

func maintenanceLuaCopyRedis(r *sharedLuaRedis) *sharedLuaRedis {
	s := r.snapshot()
	clone := *r
	clone.data, clone.sets, clone.zsets, clone.lists = s.data, s.sets, s.zsets, s.lists
	clone.trace, clone.prebuilt, clone.override = nil, nil, nil
	clone.writes, clone.attempts, clone.aclCount, clone.denyAt, clone.failAt = 0, 0, 0, 0, 0
	clone.failAfter, clone.returnedPrebuilt = false, false
	return &clone
}

func maintenanceLuaCleanWire(t *testing.T, a gateArtifacts, commit Digest, due uint64) ([]string, []string) {
	t.Helper()
	req, err := NewCleanStageWireRequest(runLuaGate(t, a, OperationCleanStage, false), CleanStageWireInput{
		ExpectedCommitID: commit, ExpectedCleanupDueAtMS: due})
	return runLuaParts(t, req, err)
}

// Model the materialized PTTL=0 boundary at the frozen script clock. The shared
// facade's eager expiry normally removes a key at <=now; here its lookup clock
// is one millisecond earlier while TIME/PTTL report the exact boundary. This is
// command-response semantics only: no source, gate, permission, receipt, Stage
// predicate, memory admission or requested argument is replaced. All mutations
// still execute through the normal facade, including actual UNLINK return values.
func maintenanceLuaAtExpiry(t *testing.T, r *sharedLuaRedis, source string, keys, args []string, at uint64) bootLuaResult {
	t.Helper()
	r.now = at - 1
	r.override = func(l *lua.LState, command string, argv []string) lua.LValue {
		if command == "TIME" {
			return bootLuaStrings(l, []string{strconv.FormatUint(at/1000, 10), strconv.FormatUint(at%1000*1000, 10)})
		}
		if command == "PTTL" && len(argv) == 1 {
			if e, found := r.data[argv[0]]; found && e.expireAt > 0 {
				return lua.LNumber(e.expireAt - int64(at))
			}
		}
		return nil
	}
	defer func() { r.override = nil; r.now = at }()
	return stageOpsRun(t, &stageOpsRedis{r}, source, keys, args)
}

func TestMaintenanceLuaActualCommitToMaterializedCleanup(t *testing.T) {
	t.Parallel()
	f := stageOpsFixtureNew(t, 2, 2, 2)
	f.stageAll(t, false)
	f.expect(t, OperationCommit, nil, StatusCommitted)
	job := stageOpsValidateHash(t, f.r, f.jobKey, SchemaJob)
	if string(job[jobLeaseOwnerIndex].Value) != "" || string(job[jobLeaseTokenIndex].Value) != "" {
		t.Fatal("actual COMMIT did not clear the worker lease")
	}
	due := uint64(f.r.zsets[StageExpiryKey][string(f.commit)])
	keys, args := maintenanceLuaCleanWire(t, f.a, f.commit, due)
	r := maintenanceLuaCopyRedis(f.r.sharedLuaRedis)
	before := r.snapshot()
	// A second due bundle demonstrates that cleanup removes exactly the expected
	// pair, and does not silently choose/erase another bundle in the same call.
	other := strings.Repeat("f", 64)
	r.zsets[StageExpiryKey][other] = float64(due)
	before = r.snapshot()
	r.maximum, r.denyAt = 1, 0 // actual deletion-only descriptors must bypass INFO, not ACL
	result := maintenanceLuaAtExpiry(t, r, maintenanceLuaSource(t, OperationCleanStage), keys, args, due)
	got := sharedLuaNoError(t, result)
	if err := ValidateOperationResponse(OperationCleanStage, got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []any{"BATCH_MORE", canonicalDecimal(due), "1", "1"}) {
		t.Fatalf("materialized committed cleanup response: %v", got)
	}
	maintenanceLuaTrace(t, r)
	unlinked := 0
	for _, call := range r.trace {
		if !call.acl && call.name == "UNLINK" {
			unlinked++
			if !strings.HasPrefix(call.args[0], f.prefix) {
				t.Fatal("cleanup escaped the exact commit prefix")
			}
		}
		if call.name == "INFO" && call.args[0] == "MEMORY" {
			t.Fatal("deletion-only cleanup entered memory admission")
		}
	}
	if unlinked != 6 || r.zsets[StageExpiryKey][other] != float64(due) {
		t.Fatalf("cleanup did not unlink its six committed residual keys: %d", unlinked)
	}
	if _, exists := r.zsets[StageExpiryKey][string(f.commit)]; exists {
		t.Fatal("cleanup retained the now-empty bundle")
	}
	// Every non-stage record/output/queue/backlink/other-run canary is unchanged.
	after := r.snapshot()
	for key, value := range before.data {
		if strings.HasPrefix(key, f.prefix) || key == StageExpiryKey {
			continue
		}
		if !reflect.DeepEqual(value, after.data[key]) || !reflect.DeepEqual(before.sets[key], after.sets[key]) ||
			!reflect.DeepEqual(before.zsets[key], after.zsets[key]) || !reflect.DeepEqual(before.lists[key], after.lists[key]) {
			t.Fatalf("cleanup changed non-residue key %s", key)
		}
	}
	maintenanceLuaJobRecord(t, r, f.lease.JobID)
	// The old response/pair can be retried, but may not consume the second bundle.
	follow := &recordsLuaFixture{r: r, a: f.a}
	before = r.snapshot()
	maintenanceLuaReply(t, follow, OperationCleanStage, keys, args, "BATCH_MORE", 0, 1)
	if !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("stale completed pair selected a different bundle")
	}
}

func TestMaintenanceLuaCommittedCleanupFencesAndCorruption(t *testing.T) {
	t.Parallel()
	f := stageOpsFixtureNew(t, 1, 1, 1)
	f.stageAll(t, false)
	f.expect(t, OperationCommit, nil, StatusCommitted)
	due := uint64(f.r.zsets[StageExpiryKey][string(f.commit)])
	keys, args := maintenanceLuaCleanWire(t, f.a, f.commit, due)
	for _, tc := range []struct {
		name   string
		edit   func(*sharedLuaRedis)
		status string
		more   int
	}{
		{"early", func(r *sharedLuaRedis) { r.now = due - 1 }, "BATCH_DONE", 0},
		{"changed same ID deadline", func(r *sharedLuaRedis) { r.now = due; r.zsets[StageExpiryKey][string(f.commit)] = float64(due + 1) }, "BATCH_DONE", 0},
		{"new earliest ID", func(r *sharedLuaRedis) {
			r.now = due
			r.zsets[StageExpiryKey][strings.Repeat("0", 63)+"1"] = float64(due)
		}, "BATCH_MORE", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := maintenanceLuaCopyRedis(f.r.sharedLuaRedis)
			tc.edit(r)
			// A stale pair must not inspect this bundle, even if its retained
			// metadata has concurrently become unusable for a current proof.
			r.data[f.prefix+"meta"].hash["owner_id"] = "invalid"
			before := r.snapshot()
			maintenanceLuaReply(t, &recordsLuaFixture{r: r, a: f.a}, OperationCleanStage, keys, args, tc.status, 0, tc.more)
			if !reflect.DeepEqual(before, r.snapshot()) {
				t.Fatal("stale/early committed pair changed state")
			}
			for _, call := range r.trace {
				if len(call.args) > 0 && (strings.HasPrefix(call.args[0], f.prefix) || call.args[0] == f.jobKey || call.args[0] == StageSlotsKey) {
					t.Fatal("stale pair inspected residue or ownership")
				}
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*sharedLuaRedis)
	}{
		{"wrong retained commit", func(r *sharedLuaRedis) { r.data[f.jobKey].hash["commit_id"] = strings.Repeat("e", 64) }},
		{"missing retained job", func(r *sharedLuaRedis) { r.removeKey(f.jobKey) }},
		{"wrong frozen baseline", func(r *sharedLuaRedis) { r.data[f.prefix+"meta"].hash["request_starts_baseline"] = "1" }},
		{"missing inventory at boundary with live residue", func(r *sharedLuaRedis) { r.removeKey(f.prefix + "keys") }},
		{"foreign inventory key", func(r *sharedLuaRedis) { r.lists[f.prefix+"keys"][0] = f.jobKey }},
		{"unknown prefix key", func(r *sharedLuaRedis) { r.lists[f.prefix+"keys"][0] = f.prefix + "unknown" }},
		{"persistent stage residue", func(r *sharedLuaRedis) {
			e := r.data[f.prefix+"aliases"]
			e.expireAt = -1
			r.data[f.prefix+"aliases"] = e
		}},
		{"TTL exceeds cleanup deadline", func(r *sharedLuaRedis) { e := r.data[f.prefix+"aliases"]; e.expireAt++; r.data[f.prefix+"aliases"] = e }},
		{"late ACL denial", func(r *sharedLuaRedis) { r.denyAt = 7 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := maintenanceLuaCopyRedis(f.r.sharedLuaRedis)
			tc.edit(r)
			before := r.snapshot()
			result := maintenanceLuaAtExpiry(t, r, maintenanceLuaSource(t, OperationCleanStage), keys, args, due)
			if result.runtimeErr != nil {
				t.Fatal(result.runtimeErr)
			}
			err, rejected := result.raw.(bootLuaErrorReply)
			if !rejected {
				t.Fatalf("corrupt committed residue accepted: %v", result.raw)
			}
			if _, parseErr := ParseErrorCode(strings.TrimPrefix(string(err), "ERR CRAWL_V2_")); parseErr != nil {
				t.Fatal(parseErr)
			}
			if !reflect.DeepEqual(before, r.snapshot()) || r.attempts != 0 {
				t.Fatal("late committed-residue rejection partially deleted keys")
			}
			if tc.name == "late ACL denial" && r.aclCount != r.denyAt {
				t.Fatal("cleanup rejected before the intended last ACL check")
			}
			maintenanceLuaTrace(t, r)
		})
	}
	// Strictly expired metadata/inventory cannot hide a still-live foreign or
	// persistent key among the closed 73 names. Nor can an unknown inventory path
	// ever become a Redis read target, even when the bundle is due.
	r := maintenanceLuaCopyRedis(f.r.sharedLuaRedis)
	r.now = due + 1
	for _, key := range wireOracleStageKeys(f.commit) {
		r.removeKey(key)
	}
	r.setHash(f.prefix+"aliases", Record{textField("unknown", "canary")})
	maintenanceLuaReject(t, &recordsLuaFixture{r: r, a: f.a}, OperationCleanStage, keys, args)
}

func maintenanceLuaAddCounter(t *testing.T, values map[string]string, field string, delta int) {
	t.Helper()
	text, exists := values[field]
	old, err := strconv.Atoi(text)
	if !exists || err != nil || old+delta < 0 {
		t.Fatalf("invalid fixture counter %s=%q delta=%d", field, text, delta)
	}
	values[field] = strconv.Itoa(old + delta)
}

// A complete pinned multigroup fixture; all immutable source identities are
// recomputed with Go before any request/stage history is attached.
func maintenanceLuaGroupedLeases(t *testing.T, n, groups int) *recordsLuaFixture {
	t.Helper()
	f := maintenanceLuaLeased(t, n, "")
	f.input.PolicyGroups = nil
	for i := 0; i < groups; i++ {
		rate := RateScopeID(fmt.Sprintf("%032x", i+1))
		scope, err := DeriveGroupScopeID(rate)
		if err != nil {
			t.Fatal(err)
		}
		f.input.PolicyGroups = append(f.input.PolicyGroups, PolicyGroup{GroupID: GroupID(fmt.Sprintf("g%02d/é", i)),
			RateScopeID: rate, GroupScopeID: scope, RequestStartLimit: 10, Concurrency: 2, IntervalMS: 0})
	}
	digest, err := DerivePolicyGroupMapDigest(f.input.PolicyGroups)
	if err != nil {
		t.Fatal(err)
	}
	f.input.PolicyGroupMapSHA256 = digest
	run := f.r.data[runLuaKey("")].hash
	run["policy_group_count"], run["policy_group_map_sha256"] = strconv.Itoa(groups), string(digest)
	for _, name := range append(append([]string(nil), runLuaMapNames...), "audit_group_counts") {
		values := map[string]string{}
		for _, group := range f.input.PolicyGroups {
			value := "0"
			switch name {
			case "group_limits":
				value = "10"
			case "group_rate_scope_ids":
				value = string(group.RateScopeID)
			case "group_scope_ids":
				value = string(group.GroupScopeID)
			case "group_concurrency":
				value = "2"
			}
			values[string(group.GroupID)] = value
		}
		runLuaCollection(f.r, runLuaKey(name), "hash", values)
	}
	for i, old := range f.jobs {
		group := f.input.PolicyGroups[i%groups]
		source := recordsLuaJob(t, group, old.CanonicalURL, old.Depth, old.ScoreText)
		record := maintenanceLuaJobRecord(t, f.r, old.JobID)
		sourceRecord, err := completeSourceJobRecord(source)
		if err != nil {
			t.Fatal(err)
		}
		jobLuaApplySource(record, sourceRecord)
		f.r.setHash(runLuaKey("job:"+string(source.JobID)), record)
		f.jobs[i] = source
		maintenanceLuaAddCounter(t, f.r.data[runLuaKey("group_open_jobs")].hash, string(group.GroupID), 1)
		maintenanceLuaAddCounter(t, f.r.data[runLuaKey("audit_group_counts")].hash, string(group.GroupID), 1)
	}
	record := runLuaRecord(t, f.r)
	f.authority, err = newTestTransportAuthority().parseRunPolicyAuthority(f.input.RunID, record, f.input.PolicyGroups)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Add known prior attempts/recoveries to a leased fixture. Every retained field
// and closed reason total is specified, including the starts baseline and the
// reservation ordinal floor; this does not mutate a Lua result into authority.
func maintenanceLuaDelivery(t *testing.T, f *recordsLuaFixture, index, attempts, pre int) {
	t.Helper()
	job := f.r.data[runLuaKey("job:"+string(f.jobs[index].JobID))].hash
	claims := pre + max(1, attempts)
	job["claim_count"], job["lease_fence"], job["next_request_ordinal"] = strconv.Itoa(claims), strconv.Itoa(claims), strconv.Itoa(claims+1)
	job["pre_io_recoveries"], job["delivery_attempts"], job["request_starts"] = strconv.Itoa(pre), strconv.Itoa(attempts), strconv.Itoa(attempts)
	run := f.r.data[runLuaKey("")].hash
	maintenanceLuaAddCounter(t, run, "claims_total", claims-1)
	maintenanceLuaAddCounter(t, run, "reservation_creations_total", claims-1)
	maintenanceLuaAddCounter(t, run, "recovered_leases_total", pre)
	maintenanceLuaAddCounter(t, f.r.data[runLuaKey("recovery_outcome_counts")].hash, "ready", pre)
	if attempts > 0 {
		job["lease_request_starts_baseline"], job["lease_delivery_started"], job["retry_count"] = strconv.Itoa(attempts-1), "1", strconv.Itoa(attempts-1)
		job["last_request_started_at_ms"], job["last_document_request_started_at_ms"] = canonicalDecimal(f.r.now-20), canonicalDecimal(f.r.now-20)
		job["last_document_request_fence"] = job["lease_fence"]
		job["last_document_target_url_id"], job["last_document_target_url"], job["last_document_target_digest"] = string(f.jobs[index].JobID), f.jobs[index].CanonicalURL, string(f.jobs[index].Decision.TargetDigest)
		run["last_request_started_at_ms"] = job["last_request_started_at_ms"]
		maintenanceLuaAddCounter(t, run, "request_starts", attempts)
		maintenanceLuaAddCounter(t, run, "retries_total", attempts-1)
		maintenanceLuaAddCounter(t, f.r.data[runLuaKey("group_started")].hash, job["group_id"], attempts)
		maintenanceLuaAddCounter(t, f.r.data[runLuaKey("retry_reason_counts")].hash, "request_timeout", attempts-1)
	}
	maintenanceLuaJobRecord(t, f.r, f.jobs[index].JobID)
	runLuaRecord(t, f.r)
}

func TestMaintenanceLuaRecoveryAllSlotMultipleGroups(t *testing.T) {
	t.Parallel()
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelled=%t", cancelled), func(t *testing.T) {
			f := maintenanceLuaGroupedLeases(t, 4, 4)
			commits := []Digest{}
			for i := range f.jobs {
				maintenanceLuaDelivery(t, f, i, 1, 0)
				commit, _ := maintenanceLuaStage(t, f, i)
				commits = append(commits, commit)
			}
			if cancelled {
				v := f.r.data[runLuaKey("")].hash
				v["state"], v["terminal_reason"], v["cancelled_at_ms"], v["last_activity_at_ms"] = "cancelled", "source_cancelled", canonicalDecimal(f.r.now-1), canonicalDecimal(f.r.now-1)
			}
			keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
			// Four owning floors, zero uncovered growth budget. A single slot may
			// not pay for the other three jobs' counter contributions.
			f.r.maximum = f.r.used + 4*32768 + 67108864
			maintenanceLuaReply(t, f, OperationRecoverExpired, keys, args, "BATCH_DONE", 4, 0)
			if len(f.r.data[StageSlotsKey].hash) != 0 || len(f.r.zsets[StageExpiryKey]) != 4 {
				t.Fatal("all-slot recovery removed residue or stranded a reservation")
			}
			v := f.r.data[runLuaKey("")].hash
			if v["recovered_leases_total"] != "4" || (cancelled && (v["cancelled_total"] != "4" || v["open_job_count"] != "0")) ||
				(!cancelled && (v["retries_total"] != "4" || v["open_job_count"] != "4")) {
				t.Fatal("all-slot aggregate state disagrees")
			}
			for _, commit := range commits {
				meta := f.r.data["mifolyo:crawl:v2:stage:"+string(commit)+":meta"].hash
				if meta["abandoned"] != "1" || meta["request_starts_baseline"] != "0" || meta["request_starts_generation"] != "1" {
					t.Fatal("recovery modified frozen stage history")
				}
			}
			for _, job := range f.jobs {
				maintenanceLuaJobRecord(t, f.r, job.JobID)
			}
			runLuaRecord(t, f.r)
		})
	}
}

func TestMaintenanceLuaRecoveryAllOutcomesInOneBatch(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaGroupedLeases(t, 6, 3)
	for i, shape := range [][2]int{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {2, 0}, {3, 0}} {
		maintenanceLuaDelivery(t, f, i, shape[0], shape[1])
	}
	keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
	maintenanceLuaReply(t, f, OperationRecoverExpired, keys, args, "BATCH_DONE", 6, 0)
	want := []string{"ready", "ready", "dead", "delayed", "delayed", "dead"}
	for i, source := range f.jobs {
		record := maintenanceLuaJobRecord(t, f.r, source.JobID)
		if string(record[jobStateIndex].Value) != want[i] {
			t.Fatalf("outcome %d differs", i)
		}
	}
	runLuaRecord(t, f.r)
	v := f.r.data[runLuaKey("")].hash
	if v["recovered_leases_total"] != "9" || v["dead_total"] != "2" || v["retries_total"] != "5" || v["request_starts"] != "6" {
		t.Fatal("mixed outcome counts were not accumulated exactly")
	}
}

func maintenanceLuaLaterPending(t *testing.T, f *recordsLuaFixture, index, chargedGroup int) {
	t.Helper()
	source := f.jobs[index]
	job := f.r.data[runLuaKey("job:"+string(source.JobID))].hash
	group := f.input.PolicyGroups[chargedGroup]
	targetURL := "https://shared.example/redirect"
	targetSource := recordsLuaJob(t, group, targetURL, source.Depth, source.ScoreText)
	target := RequestTarget{URLID: targetSource.JobID, CanonicalURL: targetURL}
	decision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestRedirect, Target: target, Depth: source.Depth,
		GroupID: group.GroupID, RateScopeID: group.RateScopeID, GroupConcurrency: group.Concurrency, OriginConcurrency: group.Concurrency,
		GroupIntervalMS: group.IntervalMS, OriginIntervalMS: group.IntervalMS})
	if err != nil {
		t.Fatal(err)
	}
	lease := LeaseIdentity{RunID: f.input.RunID, JobID: source.JobID, OwnerID: OwnerID(job["lease_owner"]), Token: LeaseToken(job["lease_token"]), Fence: 1}
	intent := ReservationIntent{Lease: lease, RequestOrdinal: 2, Target: target, CrawlPolicyDigest: f.input.CrawlPolicySHA256, Decision: decision}
	q, err := DeriveReservationID(f.authority, intent)
	if err != nil {
		t.Fatal(err)
	}
	req, err := NewReserveRequestWireRequest(runLuaGate(t, f.a, OperationReserveRequest, false), f.authority, intent)
	if err != nil {
		t.Fatal(err)
	}
	values := operationWireSemanticValues(req.semantic)
	values["reservation_id"], values["lease_fence"], values["created_at_ms"], values["expires_at_ms"] = string(q), "1", canonicalDecimal(f.r.now-4), canonicalDecimal(f.r.now)
	record := recordAuthorityReservationRecord(t, "pending")
	for i := range record {
		if value, ok := values[record[i].Name]; ok {
			record[i].Value = []byte(value)
		}
	}
	if err := ValidateRecord(SchemaReservation, record); err != nil {
		t.Fatal(err)
	}
	f.r.setHash("mifolyo:crawl:v2:reservation:"+string(q), record)
	job["active_reservation_id"], job["next_request_ordinal"], job["updated_at_ms"] = string(q), "3", canonicalDecimal(f.r.now-3)
	run := f.r.data[runLuaKey("")].hash
	run["last_activity_at_ms"] = job["updated_at_ms"]
	maintenanceLuaAddCounter(t, run, "pending_request_reservations", 1)
	maintenanceLuaAddCounter(t, run, "reservation_creations_total", 1)
	maintenanceLuaAddCounter(t, f.r.data[runLuaKey("group_pending")].hash, string(group.GroupID), 1)
	if _, exists := f.r.data[RateScopesKey]; !exists {
		f.r.setZSet(RateScopesKey, map[string]float64{values["global_scope_id"]: float64(f.r.now - 3)})
	}
	for _, kind := range []string{"global", "group", "origin"} {
		sid := values[kind+"_scope_id"]
		key := "mifolyo:crawl:v2:rate:" + sid
		if _, exists := f.r.data[key]; !exists {
			witness := "global"
			if kind == "group" {
				witness = string(group.RateScopeID)
			}
			if kind == "origin" {
				witness = "https://shared.example:443"
			}
			scope := requestLuaRateRecord(t, kind, witness)
			for i := range scope {
				switch scope[i].Name {
				case "effective_concurrency":
					scope[i].Value = []byte("2")
				case "effective_interval_ms", "next_allowed_ms", "last_started_at_ms", "active_count", "pending_count", "started_count":
					scope[i].Value = []byte("0")
				case "updated_at_ms":
					scope[i].Value = []byte(job["updated_at_ms"])
				}
				if scope[i].Name == "last_started_at_ms" || kind != "global" && scope[i].Name == "next_allowed_ms" {
					scope[i].Value = []byte(run["last_request_started_at_ms"])
				}
			}
			if err := ValidateRecord(SchemaRateScope, scope); err != nil {
				t.Fatal(err)
			}
			f.r.setHash(key, scope)
		}
		sv := f.r.data[key].hash
		maintenanceLuaAddCounter(t, sv, "active_count", 1)
		maintenanceLuaAddCounter(t, sv, "pending_count", 1)
		sv["updated_at_ms"] = job["updated_at_ms"]
		f.r.zsets[RateScopesKey][sid] = float64(f.r.now - 3)
		for _, suffix := range []string{"active", "pending"} {
			indexKey := key + ":" + suffix
			if _, exists := f.r.data[indexKey]; !exists {
				f.r.setZSet(indexKey, map[string]float64{string(q): float64(f.r.now)})
			} else {
				f.r.zsets[indexKey][string(q)] = float64(f.r.now)
			}
		}
	}
	maintenanceLuaJobRecord(t, f.r, source.JobID)
	runLuaRecord(t, f.r)
}

func maintenanceLuaMixed64(t *testing.T) *recordsLuaFixture {
	t.Helper()
	f := maintenanceLuaGroupedLeases(t, 64, 4)
	for i := 0; i < 6; i++ {
		maintenanceLuaDelivery(t, f, i, 1, 0)
	}
	for i := 0; i < 4; i++ {
		maintenanceLuaStage(t, f, i)
	}
	maintenanceLuaLaterPending(t, f, 4, 3)
	maintenanceLuaLaterPending(t, f, 5, 3)
	return f
}

// Attribute actual descriptor growth independently from Lua's private units.
// Shared fields use their first real source/request contributor in selected
// lease order. The generic Go growth oracle advances in exact command order.
func maintenanceLuaRecoveryCosts(t *testing.T, before, after *sharedLuaRedis, trace []bootLuaCommand) (map[string]uint64, uint64, uint64) {
	t.Helper()
	ids := []string{}
	for id := range before.zsets[runLuaKey("leased")] {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := before.zsets[runLuaKey("leased")][ids[i]], before.zsets[runLuaKey("leased")][ids[j]]
		return a < b || a == b && ids[i] < ids[j]
	})
	fields, keys, slots := map[string]string{}, map[string]string{}, map[string]string{}
	field := func(key, name, owner string) {
		if fields[key+"\x00"+name] == "" {
			fields[key+"\x00"+name] = owner
		}
	}
	for _, id := range ids {
		jobKey := runLuaKey("job:" + id)
		j, post := before.data[jobKey].hash, after.data[jobKey].hash
		keys[jobKey] = id
		field(runLuaKey(""), "recovered_leases_total", id)
		field(runLuaKey(""), "last_activity_at_ms", id)
		field(runLuaKey("recovery_outcome_counts"), post["state"], id)
		if post["state"] == "delayed" {
			field(runLuaKey(""), "retries_total", id)
			field(runLuaKey(""), "last_execution_at_ms", id)
			field(runLuaKey("retry_reason_counts"), post["last_reason"], id)
		} else if post["state"] != "ready" {
			field(runLuaKey(""), "open_job_count", id)
			field(runLuaKey(""), post["state"]+"_total", id)
			field(runLuaKey(""), "last_terminal_transition_at_ms", id)
			field(runLuaKey("group_open_jobs"), j["group_id"], id)
			field(runLuaKey("disposition_reason_counts"), post["last_reason"], id)
		}
		if q := j["active_reservation_id"]; q != "" {
			qKey := "mifolyo:crawl:v2:reservation:" + q
			keys[qKey] = id
			res := before.data[qKey].hash
			runField, groupMap := "pending_request_reservations", "group_pending"
			if res["state"] == "started" {
				runField, groupMap = "started_request_reservations", "group_active_started"
			}
			field(runLuaKey(""), runField, id)
			field(runLuaKey(groupMap), res["group_id"], id)
			for _, kind := range []string{"global", "group", "origin"} {
				sid := res[kind+"_scope_id"]
				scopeKey := "mifolyo:crawl:v2:rate:" + sid
				if keys[scopeKey] == "" {
					keys[scopeKey] = id
				}
				field(RateScopesKey, sid, id)
			}
		}
	}
	for commit, value := range before.data[StageSlotsKey].hash {
		parts := strings.Split(value, ":")
		slots[parts[2]] = commit
		keys["mifolyo:crawl:v2:stage:"+commit+":meta"] = parts[2]
	}
	state, costs := before.snapshot(), map[string]uint64{}
	writtenRunFields := map[string]bool{}
	removing := false
	for _, call := range trace {
		if call.acl || !sharedLuaIsWrite(call.name) {
			continue
		}
		key := call.args[0]
		if key == StageSlotsKey {
			if call.name != "HDEL" {
				t.Fatal("nonterminal slot write")
			}
			removing = true
			if stageOpsGrowth(t, state, []bootLuaCommand{call}) != 0 {
				t.Fatal("slot removal charged twice")
			}
			continue
		}
		if removing {
			t.Fatal("covered mutation after slot release")
		}
		owner := keys[key]
		if owner == "" {
			switch call.name {
			case "HSET":
				for i := 1; i < len(call.args); i += 2 {
					fieldID := key + "\x00" + call.args[i]
					candidate := fields[fieldID]
					if candidate == "" || owner != "" && owner != candidate {
						t.Fatal("field grouped under a noncontributor", key, call.args[i])
					}
					if writtenRunFields[fieldID] {
						t.Fatal("shared final Run/group/reason field was written more than once", key, call.args[i])
					}
					writtenRunFields[fieldID] = true
					owner = candidate
				}
			case "ZADD", "ZREM":
				member := call.args[len(call.args)-1]
				if key == RateScopesKey {
					owner = fields[key+"\x00"+member]
				} else if key == ActiveLeasesKey {
					owner = strings.Split(member, ":")[1]
				} else if strings.HasPrefix(key, runLuaKey("")+":") {
					owner = member
				} else {
					owner = keys["mifolyo:crawl:v2:reservation:"+member]
				}
			}
		}
		if owner == "" {
			t.Fatal("unattributable descriptor", call)
		}
		costs[owner] += stageOpsGrowth(t, state, []bootLuaCommand{call})
	}
	covered, uncovered := uint64(0), uint64(0)
	for id, cost := range costs {
		if slots[id] != "" {
			covered += cost
		} else {
			uncovered += cost
		}
	}
	return costs, covered, uncovered
}

func TestMaintenanceLuaRecoveryMixed64PartitionedMemoryAndChargedGroup(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaMixed64(t)
	before := maintenanceLuaCopyRedis(f.r)
	keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
	maintenanceLuaReply(t, f, OperationRecoverExpired, keys, args, "BATCH_DONE", 64, 0)
	if len(f.r.zsets[runLuaKey("delayed")]) != 6 || len(f.r.zsets[runLuaKey("ready")]) != 58 || f.r.data[runLuaKey("")].hash["pending_request_reservations"] != "0" {
		t.Fatal("mixed batch lost a job or request capacity")
	}
	for _, group := range f.input.PolicyGroups {
		if f.r.data[runLuaKey("group_pending")].hash[string(group.GroupID)] != "0" || f.r.data[runLuaKey("group_open_jobs")].hash[string(group.GroupID)] != "16" {
			t.Fatal("source open-job and charged request groups were mixed")
		}
	}
	if f.r.data[runLuaKey("")].hash["request_starts"] != "6" || f.r.data[runLuaKey("")].hash["reservation_creations_total"] != "66" {
		t.Fatal("expiry refunded cumulative request starts or reservation creations")
	}
	for key, old := range before.data {
		if old.hash["scope_id"] != "" {
			stageOpsValidateHash(t, &stageOpsRedis{f.r}, key, SchemaRateScope)
			for _, name := range []string{"effective_concurrency", "effective_interval_ms", "next_allowed_ms", "last_started_at_ms", "scope_witness", "concurrency_source_sha256", "interval_source_sha256"} {
				if f.r.data[key].hash[name] != old.hash[name] {
					t.Fatal("expiry relaxed persistent rate state", name)
				}
			}
		}
		if old.hash["reservation_id"] != "" {
			stageOpsValidateHash(t, &stageOpsRedis{f.r}, key, SchemaReservation)
			if f.r.data[key].hash["state"] != "expired" || f.r.data[key].expireAt != int64(f.r.now+86400000) {
				t.Fatal("missing exact expired receipt retention")
			}
		}
	}
	costs, covered, uncovered := maintenanceLuaRecoveryCosts(t, before, f.r, f.r.trace)
	if covered == 0 || uncovered == 0 {
		t.Fatal("mixed test did not cover both reserves")
	}
	for id, value := range before.data[StageSlotsKey].hash {
		parts := strings.Split(value, ":")
		if costs[parts[2]] == 0 || costs[parts[2]] > 32768 {
			t.Fatalf("slot %s cost outside its floor", id)
		}
	}
	for _, exact := range []bool{true, false} {
		r := maintenanceLuaCopyRedis(before)
		r.maximum = r.used + 4*32768 + 67108864 + uncovered
		if !exact {
			r.maximum--
		}
		copyFixture := *f
		copyFixture.r = r
		if exact {
			maintenanceLuaReply(t, &copyFixture, OperationRecoverExpired, keys, args, "BATCH_DONE", 64, 0)
		} else {
			if code := maintenanceLuaReject(t, &copyFixture, OperationRecoverExpired, keys, args); code != ErrorMemoryHeadroomLow {
				t.Fatal("one-byte-short uncovered budget did not fail memory admission", code)
			}
		}
	}
	// Exact independent per-slot budgets must also work, not merely the larger
	// standard floor. No budget or computed G is supplied to Lua as an argument.
	exactSlots := maintenanceLuaCopyRedis(before)
	for commit, value := range exactSlots.data[StageSlotsKey].hash {
		parts := strings.Split(value, ":")
		parts[0] = canonicalDecimal(costs[parts[2]])
		exactSlots.data[StageSlotsKey].hash[commit] = strings.Join(parts, ":")
	}
	exactFixture := *f
	exactFixture.r = exactSlots
	maintenanceLuaReply(t, &exactFixture, OperationRecoverExpired, keys, args, "BATCH_DONE", 64, 0)
	// Other slots' spare bytes and the safety reserve cannot pay this owner's G.
	r := maintenanceLuaCopyRedis(before)
	for commit, value := range r.data[StageSlotsKey].hash {
		parts := strings.Split(value, ":")
		parts[0] = canonicalDecimal(costs[parts[2]] - 1)
		r.data[StageSlotsKey].hash[commit] = strings.Join(parts, ":")
		break
	}
	copyFixture := *f
	copyFixture.r = r
	if code := maintenanceLuaReject(t, &copyFixture, OperationRecoverExpired, keys, args); code != ErrorMemoryHeadroomLow {
		t.Fatal("owning slot's shortfall was not independently enforced", code)
	}
}

func TestMaintenanceLuaRecoveryLateStageCorruptionAndNonexpiredLease(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaGroupedLeases(t, 3, 3)
	maintenanceLuaDelivery(t, f, 2, 1, 0)
	commit, due := maintenanceLuaStage(t, f, 2)
	keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
	for _, edit := range []func(*sharedLuaRedis){
		func(r *sharedLuaRedis) {
			r.data["mifolyo:crawl:v2:stage:"+string(commit)+":meta"].hash["request_starts_generation"] = "2"
		},
		func(r *sharedLuaRedis) {
			r.data[StageSlotsKey].hash[string(commit)] = "32768:" + runLuaID + ":" + string(f.jobs[0].JobID) + ":1:0"
		},
		func(r *sharedLuaRedis) { r.zsets[StageExpiryKey][string(commit)] = float64(due + 1) },
	} {
		r := maintenanceLuaCopyRedis(f.r)
		edit(r)
		clone := *f
		clone.r = r
		maintenanceLuaReject(t, &clone, OperationRecoverExpired, keys, args)
	}
	// A valid later lease is not selected or given a recovery unit at all.
	r := maintenanceLuaCopyRedis(f.r)
	jobID := string(f.jobs[2].JobID)
	r.data[runLuaKey("job:"+jobID)].hash["lease_expires_at_ms"] = canonicalDecimal(r.now + 1000)
	r.zsets[runLuaKey("leased")][jobID], r.zsets[ActiveLeasesKey][runLuaID+":"+jobID] = float64(r.now+1000), float64(r.now+1000)
	clone := *f
	clone.r = r
	before := r.snapshot()
	maintenanceLuaReply(t, &clone, OperationRecoverExpired, keys, args, "BATCH_DONE", 2, 0)
	if !reflect.DeepEqual(before.data[runLuaKey("job:"+jobID)], r.data[runLuaKey("job:"+jobID)]) ||
		!reflect.DeepEqual(before.data[StageSlotsKey], r.data[StageSlotsKey]) || !reflect.DeepEqual(before.zsets[StageExpiryKey], r.zsets[StageExpiryKey]) {
		t.Fatal("recovery touched a nonexpired job or its stage")
	}
}

func TestMaintenanceLuaRecoveryCancellationSeparatesSourceAndRequestGroups(t *testing.T) {
	t.Parallel()
	for _, recorded := range []bool{false, true} {
		t.Run(fmt.Sprintf("recordedCancellation=%t", recorded), func(t *testing.T) {
			f := maintenanceLuaGroupedLeases(t, 3, 4)
			for i := range f.jobs {
				maintenanceLuaDelivery(t, f, i, 1, 0)
			}
			commit, _ := maintenanceLuaStage(t, f, 2)
			maintenanceLuaLaterPending(t, f, 0, 3)
			maintenanceLuaLaterPending(t, f, 1, 3)
			// The request-charged fourth group owns no source jobs at all.
			if f.r.data[runLuaKey("group_open_jobs")].hash[string(f.input.PolicyGroups[3].GroupID)] != "0" {
				t.Fatal("test did not separate source and request groups")
			}
			run := f.r.data[runLuaKey("")].hash
			reason := "authorization_expired"
			if recorded {
				reason = "operator_cancelled"
				run["state"], run["terminal_reason"], run["cancelled_at_ms"] = "cancelled", reason, canonicalDecimal(f.r.now-1)
				run["last_activity_at_ms"] = run["cancelled_at_ms"]
			}
			f.r.now = f.input.AuthorizationExpiresAtMS
			// Physical expiry before the bounded call is not a scripted cleanup
			// write and must not fabricate or replace the historical snapshots.
			for _, key := range wireOracleStageKeys(commit) {
				f.r.removeKey(key)
			}
			keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
			maintenanceLuaReply(t, f, OperationRecoverExpired, keys, args, "BATCH_DONE", 3, 0)
			for _, group := range f.input.PolicyGroups {
				for _, suffix := range []string{"group_pending", "group_active_started", "group_open_jobs"} {
					if f.r.data[runLuaKey(suffix)].hash[string(group.GroupID)] != "0" {
						t.Fatal("source/request group release was mixed or lost", suffix)
					}
				}
			}
			if run["request_starts"] != "3" || run["reservation_creations_total"] != "5" || run["cancelled_total"] != "3" ||
				f.r.data[runLuaKey("disposition_reason_counts")].hash[reason] != "3" || f.r.data[runLuaKey("recovery_outcome_counts")].hash["cancelled"] != "3" {
				t.Fatal("authoritative cancellation counters disagreed")
			}
			for _, source := range f.jobs {
				job := maintenanceLuaJobRecord(t, f.r, source.JobID)
				if string(job[jobLastReasonIndex].Value) != reason || string(job[jobRequestStartsIndex].Value) != "1" ||
					string(job[jobLeaseFenceIndex].Value) != "1" || string(job[jobLeaseRequestStartsBaselineIndex].Value) != "0" {
					t.Fatal("cancellation reset history or used the wrong reason")
				}
			}
			runLuaRecord(t, f.r)
		})
	}
}

func TestMaintenanceLuaRecoveredResidueDoesNotRebindNewFence(t *testing.T) {
	t.Parallel()
	f := maintenanceLuaGroupedLeases(t, 1, 1)
	maintenanceLuaDelivery(t, f, 0, 1, 0)
	commit, due := maintenanceLuaStage(t, f, 0)
	keys, args := maintenanceLuaWire(t, f, OperationRecoverExpired, "")
	maintenanceLuaReply(t, f, OperationRecoverExpired, keys, args, "BATCH_DONE", 1, 0)
	jobID := string(f.jobs[0].JobID)
	jobKey := runLuaKey("job:" + jobID)
	job := f.r.data[jobKey].hash
	// Complete Go-validated newer-fence state: the earlier stage still describes
	// B=0/G=1, while this claim has B=1/G=1 and a lease past the OLD cleanup time.
	job["state"], job["not_before_ms"], job["claim_count"], job["lease_fence"], job["next_request_ordinal"] = "leased", "0", "2", "2", "3"
	job["lease_request_starts_baseline"], job["lease_owner"], job["lease_token"] = "1", strings.Repeat("f", 32), strings.Repeat("d", 64)
	job["lease_started_at_ms"], job["lease_expires_at_ms"], job["updated_at_ms"] = canonicalDecimal(due-1000), canonicalDecimal(due+60000), canonicalDecimal(due-1000)
	run := f.r.data[runLuaKey("")].hash
	run["claims_total"], run["reservation_creations_total"], run["last_activity_at_ms"] = "2", "2", job["updated_at_ms"]
	f.r.removeKey(runLuaKey("delayed"))
	f.r.setZSet(runLuaKey("leased"), map[string]float64{jobID: float64(due + 60000)})
	f.r.setZSet(runLuaKey("leased_at"), map[string]float64{jobID: float64(due - 1000)})
	f.r.setZSet(ActiveLeasesKey, map[string]float64{runLuaID + ":" + jobID: float64(due + 60000)})
	maintenanceLuaJobRecord(t, f.r, f.jobs[0].JobID)
	runLuaRecord(t, f.r)
	keys, args = maintenanceLuaCleanWire(t, f.a, commit, due)
	before := f.r.snapshot()
	got := sharedLuaNoError(t, maintenanceLuaAtExpiry(t, f.r, maintenanceLuaSource(t, OperationCleanStage), keys, args, due))
	if !reflect.DeepEqual(got, []any{"BATCH_DONE", canonicalDecimal(due), "1", "0"}) {
		t.Fatal("historical recovery residue was treated as current output authority", got)
	}
	maintenanceLuaTrace(t, f.r)
	for _, key := range []string{jobKey, runLuaKey(""), runLuaKey("leased"), runLuaKey("leased_at"), ActiveLeasesKey} {
		if !reflect.DeepEqual(before.data[key], f.r.data[key]) || !reflect.DeepEqual(before.zsets[key], f.r.zsets[key]) {
			t.Fatal("cleanup changed the newer fence", key)
		}
	}
	for _, call := range f.r.trace {
		if len(call.args) > 0 && call.args[0] == jobKey {
			t.Fatal("historical abandoned residue consulted a newer job interval")
		}
	}
}

func TestMaintenanceLuaActualMaximumStageRecoveryAnd73KeyCleanup(t *testing.T) {
	t.Parallel()
	f := stageOpsFixtureNew(t, 256, 128, 64)
	f.stageAll(t, false)
	meta := stageOpsValidateHash(t, f.r, f.prefix+"meta", SchemaStageMeta)
	if string(meta[stageKeyCountIndex].Value) != "73" || len(f.r.lists[f.prefix+"keys"]) != 73 {
		t.Fatal("fixture did not materialize the full closed stage inventory")
	}
	due := uint64(f.r.zsets[StageExpiryKey][string(f.commit)])
	f.r.now += 60000
	request, err := NewRecoverExpiredWireRequest(runLuaGate(t, f.a, OperationRecoverExpired, false), f.lease.RunID)
	keys, args := runLuaParts(t, request, err)
	fixture := &recordsLuaFixture{r: f.r.sharedLuaRedis, a: f.a}
	maintenanceLuaReply(t, fixture, OperationRecoverExpired, keys, args, "BATCH_DONE", 1, 0)
	if f.r.data[f.prefix+"meta"].hash["abandoned"] != "1" || len(f.r.lists[f.prefix+"keys"]) != 73 {
		t.Fatal("recovery altered physical stage content instead of abandoning ownership")
	}
	keys, args = maintenanceLuaCleanWire(t, f.a, f.commit, due)
	r := maintenanceLuaCopyRedis(f.r.sharedLuaRedis)
	r.denyAt = 74 // the expiry ZREM after all 73 exact key unlinks
	before := r.snapshot()
	result := maintenanceLuaAtExpiry(t, r, maintenanceLuaSource(t, OperationCleanStage), keys, args, due)
	if result.runtimeErr != nil || result.raw != bootLuaErrorReply("ERR CRAWL_V2_BOOT_UNAPPROVED") || r.aclCount != 74 ||
		r.attempts != 0 || !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("maximum cleanup last-ACL denial was not atomic", result.raw, result.runtimeErr)
	}
	r = maintenanceLuaCopyRedis(f.r.sharedLuaRedis)
	r.maximum = 1
	got := sharedLuaNoError(t, maintenanceLuaAtExpiry(t, r, maintenanceLuaSource(t, OperationCleanStage), keys, args, due))
	if err := ValidateOperationResponse(OperationCleanStage, got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []any{"BATCH_DONE", canonicalDecimal(due), "1", "0"}) {
		t.Fatal("maximum cleanup response", got)
	}
	maintenanceLuaTrace(t, r)
	unlinked := map[string]bool{}
	for _, call := range r.trace {
		if !call.acl && call.name == "UNLINK" {
			if unlinked[call.args[0]] {
				t.Fatal("same stage key unlinked twice")
			}
			unlinked[call.args[0]] = true
		}
	}
	if len(unlinked) != 73 {
		t.Fatal("cleanup did not remove exactly its 73 closed keys")
	}
	for _, key := range wireOracleStageKeys(f.commit) {
		if !unlinked[key] || r.data[key].kind != "" {
			t.Fatal("validated stage key survived cleanup", key)
		}
	}
	if _, exists := r.zsets[StageExpiryKey][string(f.commit)]; exists {
		t.Fatal("empty stage inventory member survived")
	}
	stageOpsValidateHash(t, &stageOpsRedis{r}, f.jobKey, SchemaJob)
	stageOpsValidateHash(t, &stageOpsRedis{r}, f.runKey, SchemaRun)
}
