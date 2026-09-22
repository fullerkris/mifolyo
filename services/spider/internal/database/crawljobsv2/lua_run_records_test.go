package crawljobsv2

import (
	"fmt"
	"math"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
	lua "github.com/yuin/gopher-lua"
)

// Only the actual source chunks are assembled. No recipe/canonical output,
// network, Redis process, Go semantic callback, or private framework substitute.
func recordsLuaCore(t *testing.T) string {
	t.Helper()
	if runtime.Version() != "go1.25.13" {
		t.Fatalf("run-record oracle requires Go 1.25.13, got %s", runtime.Version())
	}
	return sharedLuaCore(t) +
		"local D=(function()\n" + string(primitiveLuaRead(t, "lua_src/unicode_data.lua")) + "\nend)()\n" +
		"CJ.URL=(function(P,D)\n" + string(primitiveLuaRead(t, "lua_src/url.lua")) + "\nend)(P,D)\n" +
		"CJ.Run=(function()\n" + string(primitiveLuaRead(t, "lua_src/ledger_run.lua")) + "\nend)()\n" +
		"CJ.Job=(function()\n" + string(primitiveLuaRead(t, "lua_src/ledger_job.lua")) + "\nend)()\n"
}

func recordsLuaSource(t *testing.T, op OperationName) string {
	t.Helper()
	return recordsLuaCore(t) + string(primitiveLuaRead(t, "lua_src/ops/"+strings.ToLower(string(op))+".lua"))
}

// Pure Lua SHA/URL validation at 500 records is NOT a Redis latency benchmark.
func recordsLuaRun(t *testing.T, r *sharedLuaRedis, source string, keys, args []string) bootLuaResult {
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
			result := L.NewTable()
			result.RawSetString("err", lua.LString(L.CheckString(1)))
			L.Push(result)
			return 1
		},
	})
	L.SetGlobal("redis", redis)
	L.SetGlobal("KEYS", bootLuaStrings(L, keys))
	L.SetGlobal("ARGV", bootLuaStrings(L, args))
	fn, err := L.Load(strings.NewReader(source), "@run-record-fragments.lua")
	if err != nil {
		t.Fatal(err)
	}
	if err := L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}); err != nil {
		return bootLuaResult{runtimeErr: err}
	}
	r.returnedPrebuilt = r.prebuilt != nil && r.prebuilt == L.Get(-1)
	return bootLuaResult{raw: bootLuaRESP(L.Get(-1))}
}

type recordsLuaFixture struct {
	r         *sharedLuaRedis
	a         gateArtifacts
	input     CreateRunWireInput
	authority RunPolicyAuthority
	candidate bool
	jobs      []SourceJob
}

func recordsLuaNew(t *testing.T, candidate bool, expected, sources, groupCount int) *recordsLuaFixture {
	t.Helper()
	r, a, input := runLuaFixture(t, candidate)
	input.ExpectedSeedCount = uint64(expected)
	input.PolicyGroups = nil
	for i := 0; i < groupCount; i++ {
		rate := RateScopeID(fmt.Sprintf("%032x", i+1))
		scope, err := DeriveGroupScopeID(rate)
		if err != nil {
			t.Fatal(err)
		}
		input.PolicyGroups = append(input.PolicyGroups, PolicyGroup{GroupID: GroupID(fmt.Sprintf("g%02d/é", i)),
			RateScopeID: rate, GroupScopeID: scope, RequestStartLimit: 10, Concurrency: 2, IntervalMS: uint64(i * 100)})
	}
	var err error
	input.PolicyGroupMapSHA256, err = DerivePolicyGroupMapDigest(input.PolicyGroups)
	if err != nil {
		t.Fatal(err)
	}
	// Construct the exact expected binding from Go, never from a Lua projection.
	record := recordAuthorityRunRecord(t, "loading")
	values := map[string]string{
		"contract_sha256": string(a.contract), "source_kind": string(input.SourceKind),
		"source_sha256": string(input.SourceSHA256), "expected_seed_count": strconv.Itoa(expected),
		"authorization_sha256": string(input.AuthorizationSHA256), "authorization_scope_sha256": string(input.AuthorizationScopeSHA256),
		"authorization_expires_at_ms": strconv.FormatUint(input.AuthorizationExpiresAtMS, 10),
		"canonicalization_sha256":     string(input.CanonicalizationSHA256), "crawl_policy_sha256": string(input.CrawlPolicySHA256),
		"render_policy_sha256": string(input.RenderPolicySHA256), "policy_group_count": strconv.Itoa(groupCount),
		"policy_group_map_sha256": string(input.PolicyGroupMapSHA256), "job_count": "0", "open_job_count": "0", "load_revision": "1",
		"created_at_ms": strconv.FormatUint(r.now-1000, 10), "last_activity_at_ms": strconv.FormatUint(r.now-1000, 10),
	}
	for i := range record {
		if value, ok := values[record[i].Name]; ok {
			record[i].Value = []byte(value)
		}
	}
	if err := ValidateRecord(SchemaRun, record); err != nil {
		t.Fatal(err)
	}
	authority, err := newTestTransportAuthority().parseRunPolicyAuthority(input.RunID, record, input.PolicyGroups)
	if err != nil {
		t.Fatal(err)
	}
	r.setHash(runLuaKey(""), record)
	for _, name := range runLuaMapNames {
		fields := map[string]string{}
		for _, group := range input.PolicyGroups {
			value := "0"
			switch name {
			case "group_limits":
				value = strconv.FormatUint(group.RequestStartLimit, 10)
			case "group_rate_scope_ids":
				value = string(group.RateScopeID)
			case "group_scope_ids":
				value = string(group.GroupScopeID)
			case "group_concurrency":
				value = strconv.FormatUint(group.Concurrency, 10)
			case "group_interval_ms":
				value = strconv.FormatUint(group.IntervalMS, 10)
			}
			fields[string(group.GroupID)] = value
		}
		runLuaCollection(r, runLuaKey(name), "hash", fields)
	}
	for name, names := range map[string][]string{"retry_reason_counts": runLuaRetryNames, "recovery_outcome_counts": runLuaRecoveryNames, "disposition_reason_counts": runLuaDispositionNames} {
		fields := map[string]string{}
		for _, field := range names {
			fields[field] = "0"
		}
		runLuaCollection(r, runLuaKey(name), "hash", fields)
	}
	r.setZSet("mifolyo:crawl:v2:runs", map[string]float64{runLuaID: float64(r.now - 1000)})
	r.setSet("mifolyo:crawl:v2:active_runs", []string{runLuaID})
	r.setSet("mifolyo:crawl:v2:unarchived_runs", []string{runLuaID})
	f := &recordsLuaFixture{r: r, a: a, input: input, authority: authority, candidate: candidate}
	for i := 0; i < sources; i++ {
		group := input.PolicyGroups[i%groupCount]
		url := fmt.Sprintf("https://example.com:8443/p/%06d?q=%%2F", i)
		if i%2 == 1 {
			url = fmt.Sprintf("http://xn--bcher-kva.example/p/%06d", i)
		}
		f.jobs = append(f.jobs, recordsLuaJob(t, group, url, uint64(i%5), ScoreText([]string{"0.1", "-1000", "10000", "0.000001", "-0.000001"}[i%5])))
	}
	sort.Slice(f.jobs, func(i, j int) bool { return f.jobs[i].JobID < f.jobs[j].JobID })
	return f
}

func recordsLuaJob(t *testing.T, group PolicyGroup, url string, depth uint64, score ScoreText) SourceJob {
	t.Helper()
	decision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestDocument,
		Target: RequestTarget{URLID: JobID(utils.URLIDV1(url)), CanonicalURL: url}, Depth: depth,
		GroupID: group.GroupID, RateScopeID: group.RateScopeID, GroupConcurrency: group.Concurrency,
		OriginConcurrency: group.Concurrency, GroupIntervalMS: group.IntervalMS, OriginIntervalMS: group.IntervalMS})
	if err != nil {
		t.Fatal(err)
	}
	return SourceJob{JobID: decision.TargetURLID, CanonicalURL: url, Depth: depth, ScoreText: score,
		GroupID: group.GroupID, RateScopeID: group.RateScopeID, Decision: decision}
}

func recordsLuaInitial(t *testing.T, job SourceJob, at uint64) Record {
	t.Helper()
	record := recordAuthorityJobRecord(t, "ready")
	source, err := completeSourceJobRecord(job)
	if err != nil {
		t.Fatal(err)
	}
	jobLuaApplySource(record, source)
	recordAuthoritySet(record, jobCreatedAtMSIndex, strconv.FormatUint(at, 10))
	recordAuthoritySet(record, jobUpdatedAtMSIndex, strconv.FormatUint(at, 10))
	if err := ValidateRecord(SchemaJob, record); err != nil {
		t.Fatal(err)
	}
	return record
}

func (f *recordsLuaFixture) wire(t *testing.T, op OperationName, jobs []SourceJob, cursor JobID, count uint64) ([]string, []string) {
	t.Helper()
	gate := runLuaGate(t, f.a, op, f.candidate)
	var request OperationWireRequest
	var err error
	if op == OperationEnqueueBatch {
		request, err = NewEnqueueBatchWireRequest(gate, f.authority, f.input.RunID, jobs)
	} else {
		request, err = NewAuditRunBatchWireRequest(gate, f.authority, AuditRunBatchWireInput{
			RunID: f.input.RunID, ExpectedPriorCursor: cursor, ExpectedPriorCount: count, Jobs: jobs})
	}
	keys, args := runLuaParts(t, request, err)
	// Literal reviewed key oracle, not production key-plan introspection.
	want := append(wireOracleAuthorityKeys(), "mifolyo:crawl:v2:runs", "mifolyo:crawl:v2:active_runs", "mifolyo:crawl:v2:unarchived_runs")
	want = append(want, wireOracleRunKeys(f.input.RunID)...)
	ordered := append([]SourceJob(nil), jobs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].JobID < ordered[j].JobID })
	for _, job := range ordered {
		want = append(want, wireOracleRunJobKey(f.input.RunID, job.JobID))
	}
	if len(keys) != 38+len(jobs) || !reflect.DeepEqual(keys, want) {
		t.Fatal("38+n closed key plan differs from literal oracle")
	}
	offset := 9
	if op == OperationAuditRunBatch {
		offset = 11
		if args[8] != string(cursor) || args[9] != strconv.FormatUint(count, 10) {
			t.Fatal("audit prior-position scalar order drift")
		}
	}
	if len(args) != offset+len(jobs) || args[7] != runLuaID || args[offset-1] != strconv.Itoa(len(jobs)) {
		t.Fatal("semantic wire shape drift")
	}
	for i, job := range ordered {
		record, err := completeSourceJobRecord(job)
		if err != nil {
			t.Fatal(err)
		}
		if args[offset+i] != string(primitiveLuaEncoded(t, record)) || jobLuaSourceOracle(f.authority, record) != nil {
			t.Fatal("source binding is not the exact Go nine-field record")
		}
	}
	return keys, args
}

func recordsLuaReply(t *testing.T, r *sharedLuaRedis, op OperationName, keys, args []string, status string, tail ...string) {
	t.Helper()
	result := recordsLuaRun(t, r, recordsLuaSource(t, op), keys, args)
	got := sharedLuaNoError(t, result)
	if err := ValidateOperationResponse(op, got); err != nil {
		t.Fatal("Go response oracle", err)
	}
	want := []any{status, strconv.FormatUint(r.now, 10)}
	for _, value := range tail {
		want = append(want, value)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response %v, want %v", got, want)
	}
	recordsLuaTrace(t, r)
	runLuaRecord(t, r)
}

func recordsLuaTrace(t *testing.T, r *sharedLuaRedis) {
	t.Helper()
	sharedLuaAssertTrace(t, r, r.attempts)
	if len(r.trace) == 0 || r.trace[0].name != "TIME" {
		t.Fatal("TIME must be first")
	}
	if r.attempts > 0 && !r.returnedPrebuilt {
		t.Fatal("response not fully prebuilt before writes")
	}
	for _, call := range r.trace {
		if call.name == "SMEMBERS" && call.args[0] == runLuaKey("jobs") {
			t.Fatal("selected-job validation enumerated the job inventory")
		}
		if call.name == "ZRANGE" {
			start, _ := strconv.Atoi(call.args[1])
			end, _ := strconv.Atoi(call.args[2])
			if call.args[0] == runLuaKey("job_order") && (end < start || end-start+1 > 101) {
				t.Fatal("audit rank proof is not bounded")
			}
		}
		if call.name == "ZRANGEBYLEX" {
			limit, _ := strconv.Atoi(call.args[5])
			if limit < 1 || limit > 101 {
				t.Fatal("audit lex page exceeds batch+lookahead")
			}
		}
		if sharedLuaIsWrite(call.name) && call.args[0] == "mifolyo:crawl:v2:active_leases" {
			t.Fatal("read-only lease exception became a write key")
		}
	}
}

func recordsLuaReject(t *testing.T, r *sharedLuaRedis, op OperationName, keys, args []string, expected ErrorCode) {
	t.Helper()
	before := r.snapshot()
	result := recordsLuaRun(t, r, recordsLuaSource(t, op), keys, args)
	if result.runtimeErr != nil {
		t.Fatal("prewrite rejection escaped", result.runtimeErr)
	}
	reply, ok := result.raw.(bootLuaErrorReply)
	if !ok {
		t.Fatalf("expected rejection, got %v", result.raw)
	}
	code, err := ParseErrorCode(strings.TrimPrefix(string(reply), "ERR CRAWL_V2_"))
	if err != nil || expected != "" && code != expected {
		t.Fatalf("rejected %s, want %s", reply, expected)
	}
	if !reflect.DeepEqual(before, r.snapshot()) || r.attempts != 0 {
		t.Fatal("preflight failure mutated datastore")
	}
	recordsLuaTrace(t, r)
}

func recordsLuaReplay(t *testing.T, r *sharedLuaRedis, op OperationName, keys, args []string, status string, tail ...string) {
	t.Helper()
	r.now++
	r.maximum, r.denyAt = 1, 1
	before := r.snapshot()
	recordsLuaReply(t, r, op, keys, args, status, tail...)
	if !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("replay changed full snapshot, counters, B/G, or TTL")
	}
	for _, call := range r.trace {
		if call.acl || call.name == "INFO" && call.args[0] == "MEMORY" || len(call.args) > 0 && call.args[0] == sharedLuaSlots {
			t.Fatal("no-op replay performed allocation admission")
		}
	}
	r.maximum, r.denyAt = 400*1024*1024, 0
}

func recordsLuaAssertJobs(t *testing.T, f *recordsLuaFixture, jobs []SourceJob, at uint64) {
	t.Helper()
	for _, job := range jobs {
		key := wireOracleRunJobKey(f.input.RunID, job.JobID)
		want := recordsLuaInitial(t, job, at)
		entry := f.r.data[key]
		if entry.kind != "hash" || len(entry.hash) != 54 {
			t.Fatal("not a complete 54-field job")
		}
		for _, field := range want {
			if value, present := entry.hash[field.Name]; !present || value != string(field.Value) {
				t.Fatalf("job %s field %s: %q != %q", job.JobID, field.Name, value, field.Value)
			}
		}
		score, _ := strconv.ParseFloat(string(job.ScoreText), 64)
		if !f.r.sets[runLuaKey("jobs")][string(job.JobID)] || f.r.zsets[runLuaKey("job_order")][string(job.JobID)] != 0 ||
			f.r.zsets[runLuaKey("ready")][string(job.JobID)] != score || f.r.zsets[runLuaKey("ready_at")][string(job.JobID)] != float64(at) {
			t.Fatal("initial membership/score/time differs from Go expectation")
		}
	}
}

// Controlled serial/concurrent prestate: complete Go fixture, not output from
// the handler under test. Unselected records are never read by these operations.
func (f *recordsLuaFixture) seed(t *testing.T, jobs []SourceJob) {
	t.Helper()
	ids, order, ready, ages := []string{}, map[string]float64{}, map[string]float64{}, map[string]float64{}
	groups := map[string]int{}
	for _, job := range jobs {
		id := string(job.JobID)
		f.r.setHash(wireOracleRunJobKey(f.input.RunID, job.JobID), recordsLuaInitial(t, job, f.r.now-100))
		ids, order[id], ages[id] = append(ids, id), 0, float64(f.r.now-100)
		ready[id], _ = strconv.ParseFloat(string(job.ScoreText), 64)
		groups[string(job.GroupID)]++
	}
	f.r.setSet(runLuaKey("jobs"), ids)
	f.r.setZSet(runLuaKey("job_order"), order)
	f.r.setZSet(runLuaKey("ready"), ready)
	f.r.setZSet(runLuaKey("ready_at"), ages)
	for _, g := range f.input.PolicyGroups {
		f.r.data[runLuaKey("group_open_jobs")].hash[string(g.GroupID)] = strconv.Itoa(groups[string(g.GroupID)])
	}
	v := f.r.data[runLuaKey("")].hash
	v["job_count"], v["open_job_count"], v["load_revision"] = strconv.Itoa(len(jobs)), strconv.Itoa(len(jobs)), "2"
	v["last_activity_at_ms"] = strconv.FormatUint(f.r.now-100, 10)
	runLuaRecord(t, f.r)
}

func (f *recordsLuaFixture) begin(t *testing.T) {
	t.Helper()
	request, err := NewBeginRunAuditWireRequest(runLuaGate(t, f.a, OperationBeginRunAudit, f.candidate), f.input.RunID)
	keys, args := runLuaParts(t, request, err)
	v := f.r.data[runLuaKey("")].hash
	recordsLuaReply(t, f.r, OperationBeginRunAudit, keys, args, "AUDIT_STARTED", v["load_revision"], v["job_count"])
}

func TestRunRecordsLuaEnqueueAndImmutableReplay(t *testing.T) {
	t.Parallel()
	for _, candidate := range []bool{false, true} {
		t.Run(fmt.Sprintf("candidate=%t", candidate), func(t *testing.T) {
			f := recordsLuaNew(t, candidate, 5, 5, 2)
			keys, args := f.wire(t, OperationEnqueueBatch, f.jobs[:2], "", 0)
			at := f.r.now
			recordsLuaReply(t, f.r, OperationEnqueueBatch, keys, args, "OK", "2", "0", "2", "2")
			recordsLuaAssertJobs(t, f, f.jobs[:2], at)
			// TTLs belong to the retained state and must survive exact replay.
			for _, key := range []string{runLuaKey(""), runLuaKey("ready"), wireOracleRunJobKey(f.input.RunID, f.jobs[0].JobID)} {
				entry := f.r.data[key]
				entry.expireAt = int64(f.r.now + 100000)
				f.r.data[key] = entry
			}
			recordsLuaReplay(t, f.r, OperationEnqueueBatch, keys, args, "EXISTS_IDENTICAL", "0", "0", "2", "2")
			keys, args = f.wire(t, OperationEnqueueBatch, f.jobs[1:], "", 0)
			at = f.r.now
			recordsLuaReply(t, f.r, OperationEnqueueBatch, keys, args, "OK", "3", "0", "5", "3")
			recordsLuaAssertJobs(t, f, f.jobs[2:], at)
			recordsLuaReplay(t, f.r, OperationEnqueueBatch, keys, args, "EXISTS_IDENTICAL", "0", "0", "5", "3")
			for _, field := range []string{"request_starts", "claims_total", "reservation_creations_total", "last_execution_at_ms"} {
				if f.r.data[runLuaKey("")].hash[field] != "0" {
					t.Fatal("enqueue mutated execution state", field)
				}
			}
			// Aggregate each run/group hash once, never once per job.
			keys, args = f.wire(t, OperationEnqueueBatch, f.jobs, "", 0)
			f.begin(t)
			recordsLuaReject(t, f.r, OperationEnqueueBatch, keys, args, ErrorInvalidState)
		})
	}
}

func TestRunRecordsLuaEnqueueMaximumBatch(t *testing.T) {
	t.Parallel()
	f := recordsLuaNew(t, false, 500, 500, 2)
	keys, args := f.wire(t, OperationEnqueueBatch, f.jobs, "", 0)
	at := f.r.now
	recordsLuaReply(t, f.r, OperationEnqueueBatch, keys, args, "OK", "500", "0", "500", "2")
	recordsLuaAssertJobs(t, f, f.jobs, at)
	if f.r.attempts != 2502 {
		t.Fatalf("500 jobs must write all 2500 job/index descriptors plus two aggregate hashes; got %d", f.r.attempts)
	}
	for _, key := range []string{runLuaKey(""), runLuaKey("group_open_jobs")} {
		count := 0
		for _, call := range f.r.trace {
			if !call.acl && call.name == "HSET" && call.args[0] == key {
				count++
			}
		}
		if count != 1 {
			t.Fatal("run/group deltas not aggregated exactly once", key, count)
		}
	}
	recordsLuaReplay(t, f.r, OperationEnqueueBatch, keys, args, "EXISTS_IDENTICAL", "0", "0", "500", "2")
}

func TestRunRecordsLuaAuditPagesAndReplays(t *testing.T) {
	t.Parallel()
	for _, candidate := range []bool{false, true} {
		t.Run(fmt.Sprintf("candidate=%t", candidate), func(t *testing.T) {
			f := recordsLuaNew(t, candidate, 103, 103, 3)
			f.seed(t, f.jobs)
			f.begin(t)
			keys, args := f.wire(t, OperationAuditRunBatch, f.jobs[:100], "", 0)
			cursor := string(f.jobs[99].JobID)
			recordsLuaReply(t, f.r, OperationAuditRunBatch, keys, args, "BATCH_MORE", "100", "100", cursor)
			recordsLuaReplay(t, f.r, OperationAuditRunBatch, keys, args, "BATCH_MORE", "100", "100", cursor)
			firstKeys, firstArgs := keys, args
			keys, args = f.wire(t, OperationAuditRunBatch, f.jobs[100:], f.jobs[99].JobID, 100)
			recordsLuaReply(t, f.r, OperationAuditRunBatch, keys, args, "BATCH_DONE", "3", "103", "")
			v := f.r.data[runLuaKey("")].hash
			if v["audit_cursor"] != string(f.jobs[102].JobID) || v["audit_complete"] != "1" || v["audit_revision"] != "2" || v["load_revision"] != "2" {
				t.Fatal("wrong frozen audit post-state")
			}
			for _, group := range f.input.PolicyGroups {
				id := string(group.GroupID)
				if f.r.data[runLuaKey("audit_group_counts")].hash[id] != f.r.data[runLuaKey("group_open_jobs")].hash[id] {
					t.Fatal("audit group contribution mismatch")
				}
			}
			recordsLuaReplay(t, f.r, OperationAuditRunBatch, keys, args, "BATCH_DONE", "3", "103", "")
			recordsLuaReject(t, f.r, OperationAuditRunBatch, firstKeys, firstArgs, ErrorImmutableMismatch)
		})
	}
}

func TestRunRecordsLuaEmptyAudit(t *testing.T) {
	t.Parallel()
	f := recordsLuaNew(t, false, 0, 0, 1)
	f.begin(t)
	keys, args := f.wire(t, OperationAuditRunBatch, nil, "", 0)
	recordsLuaReply(t, f.r, OperationAuditRunBatch, keys, args, "BATCH_DONE", "0", "0", "")
	if f.r.data[runLuaKey("")].hash["audit_complete"] != "1" {
		t.Fatal("empty first execution did not finish audit")
	}
	recordsLuaReplay(t, f.r, OperationAuditRunBatch, keys, args, "BATCH_DONE", "0", "0", "")
	for _, expected := range []int{1, 2} {
		f := recordsLuaNew(t, false, expected, 1, 1)
		if expected == 2 {
			f.seed(t, f.jobs)
		}
		f.begin(t)
		keys, args := f.wire(t, OperationAuditRunBatch, nil, "", 0)
		recordsLuaReject(t, f.r, OperationAuditRunBatch, keys, args, ErrorInvalidArgument)
	}
}

func TestRunRecordsLuaWireAndLateSourceRejections(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationEnqueueBatch, OperationAuditRunBatch} {
		for _, mutation := range []string{"missing_arg", "extra_arg", "wrong_key", "bad_run", "duplicate", "unsorted", "truncated", "wrong_field_order", "extra_field", "empty", "too_many", "late_policy", "url_collision", "noncanonical_url", "bad_score", "bad_depth", "bad_group", "bad_rate", "bad_origin", "bad_group_scope"} {
			t.Run(string(op)+"/"+mutation, func(t *testing.T) {
				f := recordsLuaNew(t, false, 3, 3, 2)
				if op == OperationAuditRunBatch {
					f.seed(t, f.jobs)
					f.begin(t)
				}
				keys, args := f.wire(t, op, f.jobs, "", 0)
				offset := 9
				if op == OperationAuditRunBatch {
					offset = 11
				}
				last := offset + 2
				record, _ := completeSourceJobRecord(f.jobs[2])
				expected := ErrorCode("")
				switch mutation {
				case "missing_arg":
					args = args[:len(args)-1]
				case "extra_arg":
					args = append(args, "extra")
				case "wrong_key":
					keys[len(keys)-1] += ":other"
				case "bad_run":
					args[7] = "../bad"
				case "duplicate":
					args[last] = args[last-1]
				case "unsorted":
					args[last], args[last-1] = args[last-1], args[last]
				case "truncated":
					args[last] = args[last][:len(args[last])-1]
				case "wrong_field_order":
					record[2], record[3] = record[3], record[2]
				case "extra_field":
					record = append(record, textField("url_id", string(f.jobs[2].JobID)))
				case "empty":
					args, keys = args[:offset], keys[:38]
					args[offset-1] = "0"
				case "too_many":
					args[offset-1] = "501"
					if op == OperationAuditRunBatch {
						args[offset-1] = "101"
					}
				case "late_policy":
					record[8].Value = []byte(strings.Repeat("f", 64))
					expected = ErrorImmutableMismatch
				case "url_collision":
					record[1].Value = []byte("https://example.com/a-different-url")
					expected = ErrorURLIDCollision
				case "noncanonical_url":
					record[1].Value = []byte("HTTPS://example.com/")
				case "bad_score":
					record[2].Value = []byte("1e2")
				case "bad_depth":
					record[3].Value = []byte("01")
				case "bad_group":
					record[4].Value = []byte("unknown")
				case "bad_rate":
					record[5].Value = []byte(strings.Repeat("f", 32))
				case "bad_group_scope":
					record[6].Value = []byte(strings.Repeat("f", 64))
				case "bad_origin":
					record[7].Value = []byte(strings.Repeat("f", 64))
				}
				switch mutation {
				case "wrong_field_order", "extra_field", "late_policy", "url_collision", "noncanonical_url", "bad_score", "bad_depth", "bad_group", "bad_rate", "bad_group_scope", "bad_origin":
					args[last] = string(primitiveLuaEncoded(t, record))
				}
				recordsLuaReject(t, f.r, op, keys, args, expected)
			})
		}
	}
}

func TestRunRecordsLuaChangedImmutableNotReconciled(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationEnqueueBatch, OperationAuditRunBatch} {
		for _, change := range []string{"score", "depth", "group"} {
			t.Run(string(op)+"/"+change, func(t *testing.T) {
				f := recordsLuaNew(t, false, 3, 3, 2)
				f.seed(t, f.jobs[2:]) // late existing job after two valid absent inserts
				if op == OperationAuditRunBatch {
					f.seed(t, f.jobs)
					f.begin(t)
				}
				jobs := append([]SourceJob(nil), f.jobs...)
				job := jobs[2]
				switch change {
				case "score":
					job.ScoreText = "17"
				case "depth":
					for _, g := range f.input.PolicyGroups {
						if g.GroupID == job.GroupID {
							job = recordsLuaJob(t, g, job.CanonicalURL, job.Depth+1, job.ScoreText)
						}
					}
				case "group":
					for _, g := range f.input.PolicyGroups {
						if g.GroupID != job.GroupID {
							job = recordsLuaJob(t, g, job.CanonicalURL, job.Depth, job.ScoreText)
							break
						}
					}
				}
				jobs[2] = job
				keys, args := f.wire(t, op, jobs, "", 0)
				recordsLuaReject(t, f.r, op, keys, args, ErrorImmutableMismatch)
			})
		}
	}
}

func TestRunRecordsLuaEverySelectedIndexAndPartialHash(t *testing.T) {
	t.Parallel()
	mutations := []string{"jobs", "job_order", "ready", "ready_at", "leased", "leased_at", "delayed", "completed", "dead", "cancelled", "commit_backpressure", "active_leases", "missing_job", "partial_job", "extra_job_field", "job_execution", "wrong_job_type", "group_counter", "group_map_65", "run_execution", "revision"}
	for _, op := range []OperationName{OperationEnqueueBatch, OperationAuditRunBatch} {
		for _, mutation := range mutations {
			t.Run(string(op)+"/"+mutation, func(t *testing.T) {
				f := recordsLuaNew(t, false, 3, 3, 2)
				f.seed(t, f.jobs)
				if op == OperationAuditRunBatch {
					f.begin(t)
				}
				keys, args := f.wire(t, op, f.jobs, "", 0)
				id := string(f.jobs[2].JobID)
				key := wireOracleRunJobKey(f.input.RunID, f.jobs[2].JobID)
				switch mutation {
				case "jobs":
					delete(f.r.sets[runLuaKey("jobs")], id)
					f.r.sets[runLuaKey("jobs")][strings.Repeat("f", 64)] = true // preserve cardinality
				case "job_order", "ready", "ready_at":
					f.r.zsets[runLuaKey(mutation)][id]++
				case "active_leases":
					f.r.setZSet("mifolyo:crawl:v2:active_leases", map[string]float64{runLuaID + ":" + id: float64(f.r.now + 60000)})
				case "missing_job":
					f.r.removeKey(key)
				case "partial_job":
					delete(f.r.data[key].hash, "active_stage_commit_id")
				case "extra_job_field":
					f.r.data[key].hash["invented"] = "0"
				case "wrong_job_type":
					f.r.data[key] = bootLuaEntry{kind: "string", value: "bad", expireAt: -1}
				case "job_execution":
					// Individually schema-valid recovered-ready state contradicts the
					// run's zero execution history; the full initial shape must reject.
					v := f.r.data[key].hash
					v["claim_count"], v["lease_fence"], v["pre_io_recoveries"], v["next_request_ordinal"] = "1", "1", "1", "2"
				case "group_counter":
					f.r.data[runLuaKey("group_open_jobs")].hash[string(f.jobs[2].GroupID)] = "0"
				case "group_map_65":
					for i := 0; i < 65; i++ {
						f.r.data[runLuaKey("group_limits")].hash[fmt.Sprintf("injected%02d", i)] = "0"
					}
				case "run_execution":
					f.r.data[runLuaKey("")].hash["claims_total"] = "1"
				case "revision":
					f.r.data[runLuaKey("")].hash["audit_revision"] = "1"
				default:
					f.r.setZSet(runLuaKey(mutation), map[string]float64{id: float64(f.r.now)})
				}
				recordsLuaReject(t, f.r, op, keys, args, "")
			})
		}
	}
}

func TestRunRecordsLuaControlledConcurrentPartialBatch(t *testing.T) {
	t.Parallel()
	f := recordsLuaNew(t, false, 4, 4, 2)
	f.seed(t, []SourceJob{f.jobs[1], f.jobs[3]}) // another caller already inserted a subset
	keys, args := f.wire(t, OperationEnqueueBatch, f.jobs, "", 0)
	recordsLuaReply(t, f.r, OperationEnqueueBatch, keys, args, "OK", "2", "0", "4", "3")
	recordsLuaAssertJobs(t, f, []SourceJob{f.jobs[1], f.jobs[3]}, f.r.now-100)
	recordsLuaAssertJobs(t, f, []SourceJob{f.jobs[0], f.jobs[2]}, f.r.now)
	recordsLuaReplay(t, f.r, OperationEnqueueBatch, keys, args, "EXISTS_IDENTICAL", "0", "0", "4", "3")
	// A late orphan hash / membership is not permission to repair partial writes.
	for _, orphan := range []string{"hash", "index", "global_lease"} {
		t.Run(orphan, func(t *testing.T) {
			f := recordsLuaNew(t, false, 4, 4, 2)
			f.seed(t, f.jobs[:1])
			id := string(f.jobs[3].JobID)
			switch orphan {
			case "hash":
				f.r.setHash(wireOracleRunJobKey(f.input.RunID, f.jobs[3].JobID), recordsLuaInitial(t, f.jobs[3], f.r.now-100))
			case "index":
				f.r.zsets[runLuaKey("ready")][id] = 1
			case "global_lease":
				f.r.setZSet("mifolyo:crawl:v2:active_leases", map[string]float64{runLuaID + ":" + id: float64(f.r.now + 60000)})
			}
			keys, args := f.wire(t, OperationEnqueueBatch, f.jobs, "", 0)
			recordsLuaReject(t, f.r, OperationEnqueueBatch, keys, args, ErrorStateIndexCorrupt)
		})
	}
}

func TestRunRecordsLuaAuditReplayMustRevalidate(t *testing.T) {
	t.Parallel()
	for _, final := range []bool{false, true} {
		for _, mutation := range []string{"source_score", "index_score", "global_lease", "load_revision", "group_counts", "prior_count", "prior_cursor", "skipped_prefix", "skipped_tail"} {
			t.Run(fmt.Sprintf("final=%t/%s", final, mutation), func(t *testing.T) {
				f := recordsLuaNew(t, false, 3, 3, 2)
				f.seed(t, f.jobs)
				f.begin(t)
				n, status, cursor := 2, "BATCH_MORE", string(f.jobs[1].JobID)
				if final {
					n, status, cursor = 3, "BATCH_DONE", ""
				}
				keys, args := f.wire(t, OperationAuditRunBatch, f.jobs[:n], "", 0)
				recordsLuaReply(t, f.r, OperationAuditRunBatch, keys, args, status, strconv.Itoa(n), strconv.Itoa(n), cursor)
				switch mutation {
				case "source_score":
					record, _ := completeSourceJobRecord(f.jobs[n-1])
					record[2].Value = []byte("17")
					args[10+n] = string(primitiveLuaEncoded(t, record))
				case "index_score":
					f.r.zsets[runLuaKey("ready")][string(f.jobs[n-1].JobID)] = 17
				case "global_lease":
					f.r.setZSet("mifolyo:crawl:v2:active_leases", map[string]float64{runLuaID + ":" + string(f.jobs[n-1].JobID): float64(f.r.now + 60000)})
				case "load_revision":
					f.r.data[runLuaKey("")].hash["load_revision"] = "3"
				case "group_counts":
					f.r.data[runLuaKey("audit_group_counts")].hash[string(f.jobs[0].GroupID)] = "0"
				case "prior_count":
					args[9] = "1"
				case "prior_cursor":
					args[8] = string(f.jobs[0].JobID)
				case "skipped_prefix":
					// End equals current, but prior cursor's claimed rank is false.
					keys, args = f.wire(t, OperationAuditRunBatch, f.jobs[1:n], JobID(strings.Repeat("0", 64)), 1)
				case "skipped_tail":
					// Preserve cardinality while moving the end member ahead of the
					// purported final page; replay cannot rely on caller arithmetic.
					delete(f.r.zsets[runLuaKey("job_order")], string(f.jobs[n-1].JobID))
					f.r.zsets[runLuaKey("job_order")][strings.Repeat("f", 64)] = 0
				}
				recordsLuaReject(t, f.r, OperationAuditRunBatch, keys, args, "")
			})
		}
	}
}

func TestRunRecordsLuaAuditFinalAccountingAndTail(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"incomplete_source", "wrong_group_distribution", "omitted_member", "tail_score", "wrong_rank", "finished_zero"} {
		t.Run(mutation, func(t *testing.T) {
			f := recordsLuaNew(t, false, 3, 3, 2)
			jobs := f.jobs
			if mutation == "incomplete_source" {
				jobs = jobs[:2]
			}
			f.seed(t, jobs)
			f.begin(t)
			keys, args := f.wire(t, OperationAuditRunBatch, jobs, "", 0)
			switch mutation {
			case "wrong_group_distribution":
				counts := f.r.data[runLuaKey("group_open_jobs")].hash
				counts[string(f.input.PolicyGroups[0].GroupID)], counts[string(f.input.PolicyGroups[1].GroupID)] = "0", "3"
			case "omitted_member":
				keys, args = f.wire(t, OperationAuditRunBatch, []SourceJob{jobs[0], jobs[2]}, "", 0)
			case "tail_score":
				keys, args = f.wire(t, OperationAuditRunBatch, jobs[:2], "", 0)
				f.r.zsets[runLuaKey("job_order")][string(jobs[2].JobID)] = 1
			case "wrong_rank":
				// Coherent count/sum but corrupt cursor: a forged prior count must
				// be checked independently even on an advancing, non-replay call.
				v := f.r.data[runLuaKey("")].hash
				v["audit_count"], v["audit_cursor"] = "1", string(jobs[1].JobID)
				f.r.data[runLuaKey("audit_group_counts")].hash[string(jobs[0].GroupID)] = "1"
				keys, args = f.wire(t, OperationAuditRunBatch, jobs[2:], jobs[1].JobID, 1)
			case "finished_zero":
				recordsLuaReply(t, f.r, OperationAuditRunBatch, keys, args, "BATCH_DONE", "3", "3", "")
				keys, args = f.wire(t, OperationAuditRunBatch, nil, jobs[2].JobID, 3)
			}
			recordsLuaReject(t, f.r, OperationAuditRunBatch, keys, args, "")
		})
	}
}

func TestRunRecordsLuaLimitsAndMaximumGroups(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ count, expected int }{{2, 1}, {9999, 10000}, {10000, 10000}} {
		f := recordsLuaNew(t, false, test.expected, 2, 1)
		if test.count >= 9999 {
			// Bounded selected reads must not enumerate the 10,000-member
			// inventory. Supply a controlled full-count prestate, not 10k hashes.
			ids, scores, ages := []string{}, map[string]float64{}, map[string]float64{}
			for i := 0; i < test.count; i++ {
				id := fmt.Sprintf("%064x", i)
				ids, scores[id], ages[id] = append(ids, id), 0, float64(f.r.now-100)
			}
			f.r.setSet(runLuaKey("jobs"), ids)
			f.r.setZSet(runLuaKey("job_order"), scores)
			f.r.setZSet(runLuaKey("ready"), scores)
			f.r.setZSet(runLuaKey("ready_at"), ages)
			v := f.r.data[runLuaKey("")].hash
			v["job_count"], v["open_job_count"], v["load_revision"] = strconv.Itoa(test.count), strconv.Itoa(test.count), "2"
			v["last_activity_at_ms"] = strconv.FormatUint(f.r.now-100, 10)
			f.r.data[runLuaKey("group_open_jobs")].hash[string(f.input.PolicyGroups[0].GroupID)] = strconv.Itoa(test.count)
		}
		jobs := f.jobs
		if test.count == 9999 {
			jobs = jobs[:1]
		}
		keys, args := f.wire(t, OperationEnqueueBatch, jobs, "", 0)
		if test.count == 9999 {
			recordsLuaReply(t, f.r, OperationEnqueueBatch, keys, args, "OK", "1", "0", "10000", "3")
			recordsLuaAssertJobs(t, f, jobs, f.r.now)
		} else {
			recordsLuaReject(t, f.r, OperationEnqueueBatch, keys, args, ErrorLimitExceeded)
		}
	}
	f := recordsLuaNew(t, false, 2, 2, 1)
	f.r.data[runLuaKey("")].hash["load_revision"] = "9007199254740991"
	keys, args := f.wire(t, OperationEnqueueBatch, f.jobs, "", 0)
	recordsLuaReject(t, f.r, OperationEnqueueBatch, keys, args, ErrorCounterCorrupt)
	f = recordsLuaNew(t, false, 64, 64, 64)
	keys, args = f.wire(t, OperationEnqueueBatch, f.jobs, "", 0)
	recordsLuaReply(t, f.r, OperationEnqueueBatch, keys, args, "OK", "64", "0", "64", "2")
	f.begin(t)
	keys, args = f.wire(t, OperationAuditRunBatch, f.jobs, "", 0)
	recordsLuaReply(t, f.r, OperationAuditRunBatch, keys, args, "BATCH_DONE", "64", "64", "")
	for _, group := range f.input.PolicyGroups {
		if f.r.data[runLuaKey("audit_group_counts")].hash[string(group.GroupID)] != "1" {
			t.Fatal("maximum group map was truncated")
		}
	}
}

func TestRunRecordsLuaCompleteOverBoundWireBatches(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationEnqueueBatch, OperationAuditRunBatch} {
		t.Run(string(op), func(t *testing.T) {
			maximum := 500
			if op == OperationAuditRunBatch {
				maximum = 100
			}
			f := recordsLuaNew(t, false, maximum+1, maximum+1, 1)
			keys, args := f.wire(t, op, f.jobs[:maximum], "", 0)
			record, err := completeSourceJobRecord(f.jobs[maximum])
			if err != nil {
				t.Fatal(err)
			}
			args = append(args, string(primitiveLuaEncoded(t, record)))
			keys = append(keys, wireOracleRunJobKey(f.input.RunID, f.jobs[maximum].JobID))
			countAt := 8
			if op == OperationAuditRunBatch {
				countAt = 10
			}
			args[countAt] = strconv.Itoa(maximum + 1)
			recordsLuaReject(t, f.r, op, keys, args, "")
			if len(f.r.trace) != 1 {
				t.Fatal("over-bound wire reached stored-state reads")
			}
		})
	}
}

func TestRunRecordsLuaNativeRedisScoresAndGlobalLeaseRead(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"0.10000000000000001", "1e-1", "0.10000000000000002", "nan", "+inf"} {
		t.Run(text, func(t *testing.T) {
			f := recordsLuaNew(t, false, 1, 1, 1)
			f.seed(t, f.jobs)
			// Unrelated live global lease is not an assertion that all leases are
			// absent; the selected run:job must have an explicit false receipt.
			f.r.setZSet("mifolyo:crawl:v2:active_leases", map[string]float64{strings.Repeat("e", 32) + ":" + strings.Repeat("f", 64): float64(f.r.now + 60000)})
			f.r.override = func(L *lua.LState, name string, args []string) lua.LValue {
				if name == "ZSCORE" && args[0] == runLuaKey("ready") {
					return lua.LString(text)
				}
				return nil
			}
			keys, args := f.wire(t, OperationEnqueueBatch, f.jobs, "", 0)
			number, err := strconv.ParseFloat(text, 64)
			if err == nil && math.Float64bits(number) == math.Float64bits(0.1) {
				recordsLuaReplay(t, f.r, OperationEnqueueBatch, keys, args, "EXISTS_IDENTICAL", "0", "0", "1", "2")
			} else {
				recordsLuaReject(t, f.r, OperationEnqueueBatch, keys, args, "")
			}
		})
	}
	for _, kind := range []string{"string", "set", "hash"} {
		f := recordsLuaNew(t, false, 1, 1, 1)
		f.r.data["mifolyo:crawl:v2:active_leases"] = bootLuaEntry{kind: kind, value: "not-empty", hash: map[string]string{"bad": "0"}}
		keys, args := f.wire(t, OperationEnqueueBatch, f.jobs, "", 0)
		recordsLuaReject(t, f.r, OperationEnqueueBatch, keys, args, ErrorWrongType)
	}
}

func TestRunRecordsLuaPreflightAndExecutorFailureBoundary(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationEnqueueBatch, OperationAuditRunBatch} {
		t.Run(string(op), func(t *testing.T) {
			fixture := func() (*recordsLuaFixture, []string, []string) {
				f := recordsLuaNew(t, false, 3, 3, 2)
				if op == OperationAuditRunBatch {
					f.seed(t, f.jobs)
					f.begin(t)
				}
				keys, args := f.wire(t, op, f.jobs, "", 0)
				return f, keys, args
			}
			f, keys, args := fixture()
			sharedLuaNoError(t, recordsLuaRun(t, f.r, recordsLuaSource(t, op), keys, args))
			calls := f.r.attempts
			if calls < 2 {
				t.Fatal("fixture failed to exercise multi-write mutation")
			}
			f, keys, args = fixture()
			f.r.denyAt = calls
			recordsLuaReject(t, f.r, op, keys, args, "")
			if f.r.aclCount != calls {
				t.Fatal("last ACL descriptor was not preflighted")
			}
			f, keys, args = fixture()
			f.r.maximum = 1
			recordsLuaReject(t, f.r, op, keys, args, ErrorMemoryHeadroomLow)
			for _, after := range []bool{false, true} {
				f, keys, args = fixture()
				before := f.r.snapshot()
				f.r.failAt, f.r.failAfter = calls, after
				result := recordsLuaRun(t, f.r, recordsLuaSource(t, op), keys, args)
				if result.runtimeErr == nil || !strings.Contains(result.runtimeErr.Error(), bootLuaWriteErr) {
					t.Fatalf("executor failure was swallowed: %v", result)
				}
				want := calls - 1
				if after {
					want++
				}
				if f.r.writes-before.writes != want || reflect.DeepEqual(before, f.r.snapshot()) {
					t.Fatal("postwrite error incorrectly rolled back or hid prior writes")
				}
			}
		})
	}
}

func TestRunRecordsLuaGateRejections(t *testing.T) {
	t.Parallel()
	for _, candidate := range []bool{false, true} {
		for _, op := range []OperationName{OperationEnqueueBatch, OperationAuditRunBatch} {
			for _, mutation := range []string{"wrong_boot", "contract", "authority", "wrong_source", "state"} {
				t.Run(fmt.Sprintf("candidate=%t/%s/%s", candidate, op, mutation), func(t *testing.T) {
					f := recordsLuaNew(t, candidate, 2, 2, 1)
					if op == OperationAuditRunBatch {
						f.seed(t, f.jobs)
						f.begin(t)
					}
					keys, args := f.wire(t, op, f.jobs, "", 0)
					switch mutation {
					case "wrong_boot":
						f.r.runID = strings.Repeat("e", 40)
					case "contract":
						key := CrawlContractKey
						if candidate {
							key = CrawlContractCandidateKey
						}
						f.r.data[key] = bootLuaEntry{kind: "string", value: strings.Repeat("f", 64)}
					case "authority":
						if candidate {
							f.r.data[AdminFreezeKey].hash["freeze_nonce"] = strings.Repeat("f", 32)
						} else {
							f.r.data[CommitGuardKey].hash["maximum_shape_sha256"] = strings.Repeat("f", 64)
						}
					case "wrong_source":
						kind := "v1_migration"
						if candidate {
							kind = "mongo"
						}
						f.r.data[runLuaKey("")].hash["source_kind"] = kind
					case "state":
						v := f.r.data[runLuaKey("")].hash
						if op == OperationEnqueueBatch {
							f.begin(t)
						} else {
							v["state"], v["audit_revision"], v["audit_count"], v["audit_cursor"], v["audit_complete"] = "loading", "0", "0", "", "0"
							f.r.removeKey(runLuaKey("audit_group_counts"))
						}
					}
					recordsLuaReject(t, f.r, op, keys, args, "")
				})
			}
		}
	}
}

// The same actual context/read/Job modules also prove the receipt contract
// itself. No ctx.selected tables, forged .v/.n, or complete empty cardinalities
// may authorize an unrequested selected-job absence.
const recordsLuaReceiptPrelude = `
local ctx=assert(CJ.Context.open(CJ.Wire.run_spec("CJ2_ENQUEUE_BATCH",{"run_id","record_count"}),KEYS,ARGV))
assert(CJ.Gate.check(ctx))
local run=assert(CJ.Run.load(ctx,ctx.request.v.run_id))
local sources=assert(CJ.Job.sources(ctx.request.records,run))
local id=sources[1].v.job_id
local view={ctx=ctx,job=assert(CJ.Read.fixed_hash(ctx,ctx.keys.job_keys[1],"job"))}
`

func TestRunRecordsLuaPrivateSelectedReceipts(t *testing.T) {
	t.Parallel()
	for _, omitted := range append([]string{""}, jobLuaIndexNames...) {
		t.Run("omitted="+omitted, func(t *testing.T) {
			f := recordsLuaNew(t, false, 1, 1, 1)
			keys, args := f.wire(t, OperationEnqueueBatch, f.jobs, "", 0)
			source := recordsLuaCore(t) + recordsLuaReceiptPrelude + `
local omitted=` + strconv.Quote(omitted) + `
for _,d in ipairs({{"jobs","set",10000},{"job_order","zset",10000},{"ready","zset",10000},
 {"ready_at","zset",10000},{"leased","zset",64},{"leased_at","zset",64},{"delayed","zset",10000},
 {"completed","zset",10000},{"dead","zset",10000},{"cancelled","zset",10000},
 {"commit_backpressure","zset",10},{"active_leases","zset",64}}) do
 local global=d[1]=="active_leases"
 local key=global and "mifolyo:crawl:v2:active_leases" or CJ.Run.key(ctx,d[1])
 local member=global and run.run_id..":"..id or id
 if d[1]==omitted then
  view[d[1]]=assert(CJ.Read.cardinality(ctx,key,d[2],d[3]))
  view[d[1]].members[member]=false
  view[d[1]].scores[member]=false
  view[d[1]].score_text[member]=false
 else
  view[d[1]]=assert(CJ.Read.members(ctx,key,d[2],{member},d[3],global and 97 or 64))
  assert(view[d[1]].members[member]==false)
 end
end
run.n={job_count=9999}; run.groups.by_id={}; run.groups.digest="caller fiction"
local job,code=CJ.Job.check(view,run,id)
if omitted=="" then assert(job and job.exists==false and code==nil); return {"selected absence"} end
assert(job==nil and code=="INVALID_STATE")
return {code}
`
			before := f.r.snapshot()
			sharedLuaNoError(t, recordsLuaRun(t, f.r, source, keys, args))
			if !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("receipt validation changed stored state")
			}
			recordsLuaTrace(t, f.r)
		})
	}
}

func TestRunRecordsLuaSourceMetadataAndReadOnlyLeaseAuthority(t *testing.T) {
	t.Parallel()
	f := recordsLuaNew(t, false, 1, 1, 1)
	keys, args := f.wire(t, OperationEnqueueBatch, f.jobs, "", 0)
	source := recordsLuaCore(t) + recordsLuaReceiptPrelude + `
local record=ctx.request.records[1]
record.v={policy_decision_sha256="invented",canonical_url="https://attacker.invalid/"}
record.n={depth=9007199254740991}
run.n=false; run.groups.by_id=false; run.groups.digest=false
local batch=assert(CJ.Job.sources({record},run))
assert(batch[1].v.job_id==id and batch[1].v.policy_decision_sha256==sources[1].v.policy_decision_sha256)
record.fields[9][2]=string.rep("f",64)
record.v=sources[1].v
local bad,code=CJ.Job.sources({record},run)
assert(bad==nil and code=="IMMUTABLE_MISMATCH")
local key="mifolyo:crawl:v2:active_leases"
local lease=assert(CJ.Read.members(ctx,key,"zset",{run.run_id..":"..id},64,97))
assert(lease.members[run.run_id..":"..id]==false and ctx.allowed[key]==nil)
local plan=assert(CJ.Plan.new(ctx))
local ok,failure=CJ.Plan.add(plan,{"ZADD",key,ctx.now_text,run.run_id..":"..id},"ordinary")
assert(ok==nil and failure=="INVALID_ARGUMENT")
ctx.allowed[key]=true -- public mutation must not turn read authority into writes
ok,failure=CJ.Plan.add(plan,{"ZADD",key,ctx.now_text,run.run_id..":"..id},"ordinary")
assert(ok==nil and failure=="INVALID_ARGUMENT")
return {"closed"}
`
	before := f.r.snapshot()
	sharedLuaNoError(t, recordsLuaRun(t, f.r, source, keys, args))
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("pure source/authority checks wrote state")
	}
	recordsLuaTrace(t, f.r)
}
