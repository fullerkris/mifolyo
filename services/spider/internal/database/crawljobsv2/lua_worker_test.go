package crawljobsv2

import (
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
	lua "github.com/yuin/gopher-lua"
)

// Actual source assembly, deliberately independent of canonical recipes. No
// replacement Wire, Context, Read, Stage, Plan, admission or digest functions.
func workerLuaCore(t *testing.T) string {
	t.Helper()
	if runtime.Version() != "go1.25.13" {
		t.Fatal("worker differential oracle requires Go 1.25.13")
	}
	s := sharedLuaCore(t)
	s += "local D=(function()\n" + string(primitiveLuaRead(t, "lua_src/unicode_data.lua")) + "\nend)()\n"
	s += "CJ.URL=(function(P,D)\n" + string(primitiveLuaRead(t, "lua_src/url.lua")) + "\nend)(P,D)\n"
	for _, module := range [][2]string{{"Run", "ledger_run"}, {"Job", "ledger_job"}, {"Request", "ledger_request"},
		{"StageOutput", "stage_output"}, {"Stage", "ledger_stage"}} {
		s += "CJ." + module[0] + "=(function()\n" + string(primitiveLuaRead(t, "lua_src/"+module[1]+".lua")) + "\nend)()\n"
	}
	return s + "CJ.Rate=CJ.Request.Rate\n"
}

func workerLuaSource(t *testing.T, op OperationName) string {
	t.Helper()
	return workerLuaCore(t) + string(primitiveLuaRead(t, "lua_src/ops/"+strings.ToLower(string(op))+".lua"))
}

// Only an additional Redis command facade, not an expiry/admission implementation
// in Lua. The parent shared facade may later own this command as well.
func workerLuaCommand(r *sharedLuaRedis, l *lua.LState, acl bool) int {
	if l.Get(1) != lua.LString("PEXPIREAT") {
		return r.command(l, acl)
	}
	args := []string{l.CheckString(2), l.CheckString(3)}
	if l.GetTop() != 3 {
		l.RaiseError("PEXPIREAT arity")
		return 0
	}
	r.trace = append(r.trace, bootLuaCommand{name: "PEXPIREAT", args: args, acl: acl})
	if acl {
		r.aclCount++
		r.prebuilt = sharedLuaExecutionReply(l)
		if r.prebuilt == nil {
			l.RaiseError("expiry ACL before complete execution")
			return 0
		}
		l.Push(lua.LBool(r.denyAt != r.aclCount))
		return 1
	}
	r.attempts++
	if r.failAt == r.attempts && !r.failAfter {
		l.RaiseError(bootLuaWriteErr)
		return 0
	}
	at, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || at <= int64(r.now) {
		l.RaiseError("invalid absolute expiry")
		return 0
	}
	entry, exists := r.data[args[0]]
	if exists {
		entry.expireAt = at
		r.data[args[0]] = entry
	}
	r.writes++
	if r.failAt == r.attempts && r.failAfter {
		l.RaiseError(bootLuaWriteErr)
		return 0
	}
	if exists {
		l.Push(lua.LNumber(1))
	} else {
		l.Push(lua.LNumber(0))
	}
	return 1
}

func workerLuaRun(t *testing.T, r *sharedLuaRedis, source string, keys, args []string) bootLuaResult {
	t.Helper()
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	lua.OpenMath(l)
	lua.OpenString(l)
	lua.OpenTable(l)
	l.SetTop(0)
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "collectgarbage", "print"} {
		l.SetGlobal(name, lua.LNil)
	}
	l.SetGlobal("bit", primitiveLuaBitOp(l))
	l.SetContext(luaTestContext(t))
	r.trace, r.aclCount, r.attempts, r.prebuilt = nil, 0, 0, nil
	redis := l.NewTable()
	l.SetFuncs(redis, map[string]lua.LGFunction{
		"call":          func(l *lua.LState) int { return workerLuaCommand(r, l, false) },
		"acl_check_cmd": func(l *lua.LState) int { return workerLuaCommand(r, l, true) },
		"error_reply": func(l *lua.LState) int {
			table := l.NewTable()
			table.RawSetString("err", lua.LString(l.CheckString(1)))
			l.Push(table)
			return 1
		},
	})
	l.SetGlobal("redis", redis)
	l.SetGlobal("KEYS", bootLuaStrings(l, keys))
	l.SetGlobal("ARGV", bootLuaStrings(l, args))
	fn, err := l.Load(strings.NewReader(source), "@worker-operation-test.lua")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}); err != nil {
		return bootLuaResult{runtimeErr: err}
	}
	r.returnedPrebuilt = r.prebuilt != nil && r.prebuilt == l.Get(-1)
	return bootLuaResult{raw: bootLuaRESP(l.Get(-1))}
}

type workerLuaFixture struct {
	r       *sharedLuaRedis
	a       gateArtifacts
	runID   RunID
	groups  []PolicyGroup
	sources []SourceJob
	policy  RunPolicyAuthority
}

func workerLuaHashRecord(t *testing.T, r *sharedLuaRedis, key string, schema RecordSchema) Record {
	t.Helper()
	names, _ := RecordSchemaFields(schema)
	entry := r.data[key]
	if entry.kind != "hash" || len(entry.hash) != len(names) {
		t.Fatalf("incomplete %s record", schema)
	}
	record := make(Record, len(names))
	for i, name := range names {
		value, exists := entry.hash[name]
		if !exists {
			t.Fatalf("missing %s.%s", schema, name)
		}
		record[i] = textField(name, value)
	}
	if err := ValidateRecord(schema, record); err != nil {
		t.Fatalf("Go rejected stored %s: %v", schema, err)
	}
	return record
}

func workerLuaFixtureNew(t *testing.T, count int, interval uint64) *workerLuaFixture {
	t.Helper()
	r, a, _ := runLuaFixture(t, false)
	lineage := RateScopeID(strings.Repeat("2", 32))
	scope, _ := DeriveGroupScopeID(lineage)
	groups := []PolicyGroup{{GroupID: "default", RateScopeID: lineage, GroupScopeID: scope, RequestStartLimit: 10, Concurrency: 3, IntervalMS: interval}}
	groupDigest, _ := DerivePolicyGroupMapDigest(groups)
	f := &workerLuaFixture{r: r, a: a, runID: RunID(runLuaID), groups: groups}
	base, ids := requestLuaPrefix+"run:"+runLuaID, []string{}
	for i := 0; i < count; i++ {
		url := fmt.Sprintf("https://example.com/page-%d", i)
		target := RequestTarget{URLID: JobID(utils.URLIDV1(url)), CanonicalURL: url}
		decision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestDocument, Target: target, Depth: 2, GroupID: "default", RateScopeID: lineage,
			GroupConcurrency: 3, OriginConcurrency: 3, GroupIntervalMS: interval, OriginIntervalMS: interval})
		if err != nil {
			t.Fatal(err)
		}
		source := SourceJob{JobID: target.URLID, CanonicalURL: url, ScoreText: "0", Depth: 2, GroupID: "default", RateScopeID: lineage, Decision: decision}
		f.sources = append(f.sources, source)
		job := recordAuthorityJobRecord(t, "ready")
		fields, _ := completeSourceJobRecord(source)
		jobLuaApplySource(job, fields)
		job[jobCreatedAtMSIndex].Value, job[jobUpdatedAtMSIndex].Value = []byte(canonicalDecimal(r.now-100)), []byte(canonicalDecimal(r.now-100))
		r.setHash(base+":job:"+string(source.JobID), job)
		ids = append(ids, string(source.JobID))
	}
	sort.Strings(ids)
	record := recordAuthorityRunRecord(t, "active")
	for index, value := range map[int]string{runContractSHA256Index: string(a.contract), runExpectedSeedCountIndex: strconv.Itoa(count), runJobCountIndex: strconv.Itoa(count),
		runOpenJobCountIndex: strconv.Itoa(count), runRequestStartsIndex: "0", runReservationCreationsTotalIndex: "0", runClaimsTotalIndex: "0", runCompletedTotalIndex: "0",
		runDeadTotalIndex: "0", runOutputCommitsTotalIndex: "0", runLoadRevisionIndex: "1", runAuditRevisionIndex: "1", runAuditCountIndex: strconv.Itoa(count),
		runAuditCursorIndex: ids[len(ids)-1], runPolicyGroupMapSHA256Index: string(groupDigest), runCreatedAtMSIndex: canonicalDecimal(r.now - 400),
		runSealedAtMSIndex: canonicalDecimal(r.now - 300), runActivatedAtMSIndex: canonicalDecimal(r.now - 200), runLastActivityAtMSIndex: canonicalDecimal(r.now - 100),
		runAuthorizationExpiresAtMSIndex: canonicalDecimal(r.now + 3600000), runLastExecutionAtMSIndex: "0", runLastRequestStartedAtMSIndex: "0", runLastTerminalTransitionAtMSIndex: "0"} {
		recordAuthoritySet(record, index, value)
	}
	r.setHash(base, record)
	r.setSet(ActiveRunsKey, []string{runLuaID})
	r.setSet(UnarchivedRunsKey, []string{runLuaID})
	r.setZSet(RunsKey, map[string]float64{runLuaID: float64(r.now - 400)})
	r.setSet(base+":jobs", ids)
	order, ready, at := map[string]float64{}, map[string]float64{}, map[string]float64{}
	for _, id := range ids {
		order[id], ready[id], at[id] = 0, 0, float64(r.now-100)
	}
	r.setZSet(base+":job_order", order)
	r.setZSet(base+":ready", ready)
	r.setZSet(base+":ready_at", at)
	for _, name := range []string{"leased", "leased_at", "delayed", "completed", "dead", "cancelled", "commit_backpressure", "visited_urls", "visited_depth"} {
		r.removeKey(base + ":" + name)
	}
	for _, key := range []string{ActiveLeasesKey, StageSlotsKey, StageExpiryKey, RateScopesKey, FirstRequestStartKey} {
		r.removeKey(key)
	}
	for name, value := range map[string]string{"group_limits": "10", "group_rate_scope_ids": string(lineage), "group_scope_ids": string(scope),
		"group_concurrency": "3", "group_interval_ms": canonicalDecimal(interval), "group_started": "0", "group_pending": "0", "group_active_started": "0",
		"group_open_jobs": strconv.Itoa(count), "audit_group_counts": strconv.Itoa(count)} {
		r.setHash(base+":"+name, Record{textField("default", value)})
	}
	for name, fields := range map[string][]string{"retry_reason_counts": runLuaRetryNames, "recovery_outcome_counts": runLuaRecoveryNames, "disposition_reason_counts": runLuaDispositionNames} {
		record := Record{}
		for _, field := range fields {
			record = append(record, textField(field, "0"))
		}
		r.setHash(base+":"+name, record)
	}
	var err error
	f.policy, err = (transportAuthority{seal: &redisTransportAuthoritySeal}).parseRunPolicyAuthority(f.runID, record, groups)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func workerLuaIntent(t *testing.T, f *workerLuaFixture, index int, ordinal uint64, kind RequestKind) ReservationIntent {
	t.Helper()
	source := f.sources[index]
	decision := source.Decision
	decision.RequestKind = kind
	return ReservationIntent{Lease: LeaseIdentity{RunID: f.runID, JobID: source.JobID, OwnerID: OwnerID(fmt.Sprintf("%032x", index+4)),
		Token: LeaseToken(fmt.Sprintf("%064x", index+5)), Fence: 1}, RequestOrdinal: ordinal,
		Target:            RequestTarget{URLID: source.JobID, CanonicalURL: source.CanonicalURL},
		CrawlPolicyDigest: Digest(f.r.data[requestLuaPrefix+"run:"+string(f.runID)].hash["crawl_policy_sha256"]), Decision: decision}
}

func workerLuaRequest(t *testing.T, f *workerLuaFixture, op OperationName, intent ReservationIntent) OperationWireRequest {
	t.Helper()
	gate := runLuaGate(t, f.a, op, false)
	var request OperationWireRequest
	var err error
	switch op {
	case OperationTryClaim:
		var source SourceJob
		for _, candidate := range f.sources {
			if candidate.JobID == intent.Lease.JobID {
				source = candidate
			}
		}
		request, err = NewTryClaimWireRequest(gate, f.policy, TryClaimTransitionInput{Job: source, Lease: intent.Lease,
			ExpectedPriorFence: uint64(intent.Lease.Fence) - 1, InitialIntent: intent})
	case OperationReserveRequest:
		request, err = NewReserveRequestWireRequest(gate, f.policy, intent)
	case OperationStartRequest:
		request, err = NewStartRequestWireRequest(gate, f.policy, intent)
	case OperationFinishRequest:
		request, err = NewFinishRequestWireRequest(gate, f.policy, intent)
	case OperationCancelReservation:
		request, err = NewCancelReservationWireRequest(gate, f.policy, intent)
	case OperationRenewLease:
		request, err = NewRenewLeaseWireRequest(gate, intent.Lease)
	case OperationReleaseBeforeIO:
		request, err = NewReleaseBeforeIOWireRequest(gate, ReleaseBeforeIOTransitionInput{Lease: intent.Lease})
	default:
		t.Fatal("unknown worker operation")
	}
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func workerLuaCall(t *testing.T, f *workerLuaFixture, op OperationName, intent ReservationIntent) bootLuaResult {
	t.Helper()
	keys, args := runLuaParts(t, workerLuaRequest(t, f, op, intent), nil)
	return workerLuaRun(t, f.r, workerLuaSource(t, op), keys, args)
}

func workerLuaTrace(t *testing.T, r *sharedLuaRedis) {
	t.Helper()
	clock, writes, acl := 0, 0, []bootLuaCommand{}
	for _, c := range r.trace {
		write := sharedLuaIsWrite(c.name) || c.name == "PEXPIREAT"
		if c.name == "TIME" {
			clock++
		}
		if c.acl {
			if writes != 0 {
				t.Fatal("ACL after mutation")
			}
			acl = append(acl, c)
		} else if write {
			if len(acl) != r.aclCount || writes >= len(acl) || acl[writes].name != c.name || !reflect.DeepEqual(acl[writes].args, c.args) {
				t.Fatal("write was not prebuilt and ACL-checked")
			}
			writes++
		} else if writes != 0 {
			t.Fatal("read after mutation")
		}
	}
	if clock != 1 {
		t.Fatalf("TIME count %d", clock)
	}
	if writes > 0 && !r.returnedPrebuilt {
		t.Fatal("reply constructed after mutation")
	}
}

func workerLuaReply(t *testing.T, f *workerLuaFixture, op OperationName, intent ReservationIntent, status Status) []any {
	t.Helper()
	result := workerLuaCall(t, f, op, intent)
	got := sharedLuaNoError(t, result).([]any)
	if got[0] != string(status) {
		t.Fatalf("%s: %v, want %s", op, got, status)
	}
	var err error
	if op == OperationRenewLease && status == StatusRenewed {
		context := NewUnstagedRenewLeaseResponseContext()
		job := f.r.data[requestLuaPrefix+"run:"+string(f.runID)+":job:"+string(intent.Lease.JobID)].hash
		if commit := job["active_stage_commit_id"]; commit != "" {
			meta := workerLuaHashRecord(t, f.r, requestLuaPrefix+"stage:"+commit+":meta", SchemaStageMeta)
			expires, _ := strconv.ParseUint(string(meta[stageExpiresAtMSIndex].Value), 10, 64)
			context, err = NewStagedRenewLeaseResponseContext(RedisMilliseconds(expires))
			if err != nil {
				t.Fatal(err)
			}
		}
		err = ValidateRenewLeaseResponse(context, got)
	} else {
		err = ValidateOperationResponse(op, got)
	}
	if err != nil {
		t.Fatalf("Go response oracle: %v", err)
	}
	workerLuaTrace(t, f.r)
	workerLuaHashRecord(t, f.r, requestLuaPrefix+"run:"+string(f.runID), SchemaRun)
	workerLuaHashRecord(t, f.r, requestLuaPrefix+"run:"+string(f.runID)+":job:"+string(intent.Lease.JobID), SchemaJob)
	return got
}

func TestWorkerLuaClaimIdentityMatchesGoConstructor(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	f := workerLuaFixtureNew(t, 1, 0)
	intent := workerLuaIntent(t, f, 0, 1, RequestDocument)
	request := workerLuaRequest(t, f, OperationTryClaim, intent)
	if len(request.semantic) != 33 {
		t.Fatal("claim semantic field count drift")
	}
	got, code := vm.invoke(t, "Request", "claim_identity", jobLuaValues(vm.jobLuaVM, request.semantic))
	if code != lua.LNil || got != lua.LString(request.semantic[32].Value) {
		t.Fatalf("Go claim identity: %v/%v", got, code)
	}
	for i, field := range request.semantic {
		changed := cloneRecord(request.semantic)
		changed[i].Value = append(changed[i].Value, 'x')
		if got, _ := vm.invoke(t, "Request", "claim_identity", jobLuaValues(vm.jobLuaVM, changed)); got != lua.LNil {
			t.Fatalf("changed claim identity field %s accepted", field.Name)
		}
	}
}

func TestWorkerLuaClosedEntryPointsAndNoDenialHook(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	request := vm.cj.RawGetString("Request").(*lua.LTable)
	if request.RawGetString("check_outcome_block") != lua.LNil {
		t.Fatal("Request must not synthesize a historical RETRY denial proof")
	}
	jobSource := string(primitiveLuaRead(t, "lua_src/ledger_job.lua"))
	if strings.Contains(jobSource, "Request.check_outcome_block") {
		t.Fatal("Job still depends on an undefined historical-denial hook")
	}
	for _, method := range []string{"load_live", "plan_terminal"} {
		if _, ok := request.RawGetString(method).(*lua.LFunction); !ok {
			t.Fatalf("missing reusable Request method %s", method)
		}
	}
	const executor = `for i=1,execution.count do
    redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc))
end
return execution.reply`
	for op, method := range map[OperationName]string{
		OperationTryClaim: "prepare_claim", OperationRenewLease: "prepare_renew", OperationReserveRequest: "prepare_reserve",
		OperationStartRequest: "prepare_start", OperationFinishRequest: "prepare_terminal", OperationCancelReservation: "prepare_terminal",
	} {
		if _, ok := request.RawGetString(method).(*lua.LFunction); !ok {
			t.Fatalf("%s has no real Request preparation method", op)
		}
		fragment := string(primitiveLuaRead(t, "lua_src/ops/"+strings.ToLower(string(op))+".lua"))
		if !strings.Contains(fragment, `CJ.Wire.worker_spec("`+string(op)+`")`) ||
			!strings.Contains(fragment, "return CJ.Request."+method+"(ctx)") ||
			!strings.HasSuffix(strings.TrimSpace(fragment), executor) || strings.Count(fragment, "redis.call(") != 1 {
			t.Fatalf("%s is not bound to its closed wire/preparation/fixed executor", op)
		}
	}
}

func TestWorkerLuaClaimAndStart(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	intent := workerLuaIntent(t, f, 0, 1, RequestDocument)
	for _, test := range []struct {
		op     OperationName
		status Status
	}{{OperationTryClaim, StatusClaimed}, {OperationTryClaim, StatusAlreadyClaimed}, {OperationStartRequest, StatusStarted}, {OperationStartRequest, StatusAlreadyStarted}} {
		request := workerLuaRequest(t, f, test.op, intent)
		keys, args := runLuaParts(t, request, nil)
		got := sharedLuaNoError(t, workerLuaRun(t, f.r, workerLuaSource(t, test.op), keys, args)).([]any)
		if got[0] != string(test.status) {
			t.Fatalf("prepared %s: %v", test.op, got)
		}
		if err := ValidateOperationResponse(test.op, got); err != nil {
			t.Fatal(err)
		}
		workerLuaTrace(t, f.r)
	}
}

func TestWorkerLuaSixOperationLifecycle(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	before := f.r.snapshot()
	workerLuaReply(t, f, OperationTryClaim, i, StatusAlreadyClaimed)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("claim replay mutated state")
	}
	f.r.now += 1000
	workerLuaReply(t, f, OperationRenewLease, i, StatusRenewed)
	workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
	before = f.r.snapshot()
	workerLuaReply(t, f, OperationStartRequest, i, StatusAlreadyStarted)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("START replay mutated state")
	}
	workerLuaReply(t, f, OperationFinishRequest, i, StatusFinished)
	before = f.r.snapshot()
	f.r.now++
	workerLuaReply(t, f, OperationFinishRequest, i, StatusAlreadyFinished)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("FINISH replay extended TTL or changed state")
	}
	i.RequestOrdinal, i.Decision.RequestKind = 2, RequestRenderResource
	workerLuaReply(t, f, OperationReserveRequest, i, StatusReserved)
	workerLuaReply(t, f, OperationReserveRequest, i, StatusAlreadyReserved)
	workerLuaReply(t, f, OperationCancelReservation, i, StatusReservationCancelled)
	before = f.r.snapshot()
	f.r.now++
	workerLuaReply(t, f, OperationCancelReservation, i, StatusReservationCancelled)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("cancel replay mutated state")
	}
	jobKey := requestLuaPrefix + "run:" + string(f.runID) + ":job:" + string(i.Lease.JobID)
	documentTime := f.r.data[jobKey].hash["last_document_request_started_at_ms"]
	i.RequestOrdinal = 3 // ordinal two was cancelled, not a recorded START
	workerLuaReply(t, f, OperationReserveRequest, i, StatusReserved)
	second := workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
	if !reflect.DeepEqual(second[4:8], []any{"1", "2", "2", "2"}) || f.r.data[jobKey].hash["lease_request_starts_baseline"] != "0" ||
		f.r.data[jobKey].hash["last_document_request_started_at_ms"] != documentTime {
		t.Fatal("resource START recounted a delivery, reset B or replaced the document witness")
	}
	workerLuaReject(t, f, OperationCancelReservation, i, ErrorInvalidState)
	workerLuaReply(t, f, OperationFinishRequest, i, StatusFinished)
}

func TestWorkerLuaTwoWorkerCapacityAndNonPruning(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 3, 0)
	a, b, c := workerLuaIntent(t, f, 0, 1, RequestDocument), workerLuaIntent(t, f, 1, 1, RequestDocument), workerLuaIntent(t, f, 2, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, a, StatusClaimed)
	workerLuaReply(t, f, OperationTryClaim, b, StatusClaimed)
	before := f.r.snapshot()
	workerLuaReply(t, f, OperationTryClaim, c, StatusCapacityBlocked)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("capacity block created or pruned anything")
	}
	f.r.now += 60000
	before = f.r.snapshot()
	workerLuaReply(t, f, OperationTryClaim, c, StatusCapacityBlocked)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("logical expiry was pruned by claim")
	}
}

func workerLuaReject(t *testing.T, f *workerLuaFixture, op OperationName, intent ReservationIntent, expected ErrorCode) {
	t.Helper()
	before := f.r.snapshot()
	result := workerLuaCall(t, f, op, intent)
	if result.runtimeErr != nil {
		t.Fatal(result.runtimeErr)
	}
	want := bootLuaErrorReply("ERR CRAWL_V2_" + string(expected))
	if result.raw != want || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
		t.Fatalf("rejection %s: %v want %v (mutation attempts %d)", op, result.raw, want, f.r.attempts)
	}
	workerLuaTrace(t, f.r)
}

func TestWorkerLuaClaimMutationMatrixAndRedaction(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	request := workerLuaRequest(t, f, OperationTryClaim, i)
	keys, args := runLuaParts(t, request, nil)
	source := workerLuaSource(t, OperationTryClaim)
	for index := range args {
		bad := append([]string(nil), args...)
		bad[index] = "SENSITIVE_TEST_CANARY"
		before := f.r.snapshot()
		result := workerLuaRun(t, f.r, source, keys, bad)
		if result.runtimeErr != nil {
			t.Fatalf("argument %d caused Lua exception: %v", index, result.runtimeErr)
		}
		errorReply, ok := result.raw.(bootLuaErrorReply)
		if !ok || strings.Contains(string(errorReply), "SENSITIVE") || !strings.HasPrefix(string(errorReply), "ERR CRAWL_V2_") {
			t.Fatalf("argument %d not a redacted rejection: %v", index, result.raw)
		}
		if !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
			t.Fatalf("argument %d mutated state", index)
		}
		workerLuaTrace(t, f.r)
	}
	for _, index := range []int{0, 16, 43, 44, 45, 56} {
		bad := append([]string(nil), keys...)
		bad[index] += ":wrong"
		before := f.r.snapshot()
		result := workerLuaRun(t, f.r, source, bad, args)
		if result.runtimeErr != nil {
			t.Fatal(result.runtimeErr)
		}
		if _, ok := result.raw.(bootLuaErrorReply); !ok || !reflect.DeepEqual(before, f.r.snapshot()) {
			t.Fatalf("wrong wire key %d accepted", index)
		}
		workerLuaTrace(t, f.r)
	}
}

func TestWorkerLuaACLAndPostWriteErrors(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	writes := f.r.aclCount
	if writes < 10 {
		t.Fatal("claim did not construct complete mutation")
	}
	for _, deny := range []int{1, writes / 2, writes} {
		f := workerLuaFixtureNew(t, 1, 0)
		f.r.denyAt = deny
		before := f.r.snapshot()
		result := workerLuaCall(t, f, OperationTryClaim, i)
		if result.runtimeErr != nil {
			t.Fatal(result.runtimeErr)
		}
		if _, ok := result.raw.(bootLuaErrorReply); !ok || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
			t.Fatalf("ACL denial %d was not pre-mutation: %v", deny, result.raw)
		}
	}
	for _, at := range []int{1, 2, writes} {
		f := workerLuaFixtureNew(t, 1, 0)
		f.r.failAt, f.r.failAfter = at, true
		before := f.r.snapshot()
		result := workerLuaCall(t, f, OperationTryClaim, i)
		if result.runtimeErr == nil || reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != at {
			t.Fatalf("mutation error at %d swallowed, rolled back or continued: %v", at, result)
		}
	}
}

func TestWorkerLuaRenewalStaleCounterAndNoShortening(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	bad := i
	bad.Lease.Token = LeaseToken(strings.Repeat("f", 64))
	base := requestLuaPrefix + "run:" + string(f.runID)
	activity := f.r.data[base].hash["last_activity_at_ms"]
	for expected := 1; expected <= 2; expected++ {
		workerLuaReply(t, f, OperationRenewLease, bad, StatusLeaseLost)
		if f.r.data[base].hash["renewal_rejections_total"] != strconv.Itoa(expected) || f.r.data[base].hash["last_activity_at_ms"] != activity {
			t.Fatal("stale renewal counter or activity changed incorrectly")
		}
	}
	// A self-consistent held deadline later than the prescribed formula must be
	// rejected, never max(existing, prescribed). Every index remains aligned.
	id, _ := DeriveReservationID(f.policy, i)
	deadline := f.r.now + 61000
	f.r.data[base+":job:"+string(i.Lease.JobID)].hash["lease_expires_at_ms"] = canonicalDecimal(deadline)
	f.r.data[requestLuaPrefix+"reservation:"+string(id)].hash["expires_at_ms"] = canonicalDecimal(deadline)
	f.r.zsets[base+":leased"][string(i.Lease.JobID)] = float64(deadline)
	f.r.zsets[ActiveLeasesKey][string(f.runID)+":"+string(i.Lease.JobID)] = float64(deadline)
	for _, scope := range []Digest{i.Decision.GlobalScopeID, i.Decision.GroupScopeID, i.Decision.OriginScopeID} {
		for _, suffix := range []string{"active", "pending"} {
			f.r.zsets[requestLuaPrefix+"rate:"+string(scope)+":"+suffix][string(id)] = float64(deadline)
		}
	}
	workerLuaReject(t, f, OperationRenewLease, i, ErrorInvalidState)
	f.r.now = deadline
	workerLuaReply(t, f, OperationRenewLease, i, StatusLeaseLost)
	if f.r.data[base].hash["renewal_rejections_total"] != "3" {
		t.Fatal("expired renewal did not count exactly once")
	}
}

func TestWorkerLuaOriginalStartSnapshotsAndPrivateParser(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	started := workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
	transport := transportAuthority{seal: &redisTransportAuthoritySeal, requestIOSession: &requestIOAuthoritySession{knownUnused: true}}
	parsed, err := transport.parseStartRequestResponse(f.policy, i, started)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.IOPermit(); err != nil {
		t.Fatal(err)
	}
	workerLuaReply(t, f, OperationFinishRequest, i, StatusFinished)
	// Legal pre-state after the finished request's lease ends and a newer fence
	// is claimed. The old document witness, G and B stay durably retained.
	base := requestLuaPrefix + "run:" + string(f.runID)
	jobKey := base + ":job:" + string(i.Lease.JobID)
	f.r.now++
	job := f.r.data[jobKey].hash
	job["lease_fence"], job["claim_count"], job["lease_request_starts_baseline"] = "2", "2", "1"
	job["lease_delivery_started"], job["lease_token"], job["lease_owner"] = "0", strings.Repeat("e", 64), strings.Repeat("d", 32)
	job["lease_started_at_ms"], job["updated_at_ms"], job["next_request_ordinal"] = canonicalDecimal(f.r.now), canonicalDecimal(f.r.now), "3"
	job["last_transition_id"], job["last_transition_status"] = "", ""
	f.r.zsets[base+":leased_at"][string(i.Lease.JobID)] = float64(f.r.now)
	f.r.data[base].hash["claims_total"], f.r.data[base].hash["reservation_creations_total"], f.r.data[base].hash["last_activity_at_ms"] = "2", "2", canonicalDecimal(f.r.now)
	before := f.r.snapshot()
	historical := workerLuaReply(t, f, OperationStartRequest, i, StatusAlreadyStarted)
	if !reflect.DeepEqual(historical[2:8], started[2:8]) || historical[8] != "0" || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("historical START changed immutable snapshots or granted I/O")
	}
	if _, err := transport.parseStartRequestResponse(f.policy, i, historical); err != nil {
		t.Fatalf("private Go START parser rejected historical receipt: %v", err)
	}
}

func TestWorkerLuaBlockedBudgetCreationAndGroupPrecedence(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
	workerLuaReply(t, f, OperationFinishRequest, i, StatusFinished)
	i.RequestOrdinal = 2
	base := requestLuaPrefix + "run:" + string(f.runID)
	f.r.data[base].hash["max_request_starts"], f.r.data[base].hash["reservation_creations_total"] = "1", "100"
	before := f.r.snapshot()
	workerLuaReply(t, f, OperationReserveRequest, i, StatusRunBudgetExhausted)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("budget-only block changed ledger")
	}
	f.r.data[base].hash["max_request_starts"] = "10"
	before = f.r.snapshot()
	workerLuaReply(t, f, OperationReserveRequest, i, StatusRunReservationLimitExhausted)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("creation-only block changed ledger")
	}
	f.r.data[base].hash["reservation_creations_total"] = "1"
	f.groups[0].RequestStartLimit = 1
	digest, _ := DerivePolicyGroupMapDigest(f.groups)
	f.r.data[base].hash["policy_group_map_sha256"] = string(digest)
	f.r.data[base+":group_limits"].hash["default"] = "1"
	var err error
	f.policy, err = (transportAuthority{seal: &redisTransportAuthoritySeal}).parseRunPolicyAuthority(f.runID,
		workerLuaHashRecord(t, f.r, base, SchemaRun), f.groups)
	if err != nil {
		t.Fatal(err)
	}
	before = f.r.snapshot()
	workerLuaReply(t, f, OperationReserveRequest, i, StatusGroupBudgetExhausted)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("group-only block changed ledger")
	}
}

func TestWorkerLuaCancellationAndAuthorizationDoNotStrandCapacity(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationFinishRequest, OperationCancelReservation} {
		for _, why := range []string{"cancelled", "expired"} {
			f := workerLuaFixtureNew(t, 1, 0)
			i := workerLuaIntent(t, f, 0, 1, RequestDocument)
			workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
			if op == OperationFinishRequest {
				workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
			}
			base := requestLuaPrefix + "run:" + string(f.runID)
			if why == "cancelled" {
				f.r.data[base].hash["state"], f.r.data[base].hash["terminal_reason"], f.r.data[base].hash["cancelled_at_ms"] = "cancelled", "operator_cancelled", canonicalDecimal(f.r.now)
			} else {
				f.r.data[base].hash["authorization_expires_at_ms"] = canonicalDecimal(f.r.now)
			}
			status := StatusFinished
			if op == OperationCancelReservation {
				status = StatusReservationCancelled
			}
			workerLuaReply(t, f, op, i, status)
			id, _ := DeriveReservationID(f.policy, i)
			receipt := f.r.data[requestLuaPrefix+"reservation:"+string(id)]
			if receipt.expireAt != int64(f.r.now+86400000) || f.r.data[base].hash["pending_request_reservations"] != "0" || f.r.data[base].hash["started_request_reservations"] != "0" {
				t.Fatalf("%s/%s did not release exact held capacity", op, why)
			}
		}
	}
}

func TestWorkerLuaOverlappingRunTighteningAndStartDeadline(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	other := workerLuaFixtureNew(t, 1, 1000)
	oldID, oldBase := other.runID, requestLuaPrefix+"run:"+string(other.runID)
	other.runID = RunID(strings.Repeat("8", 32))
	newBase := requestLuaPrefix + "run:" + string(other.runID)
	other.groups[0].Concurrency = 1
	other.sources[0].Decision.GroupConcurrency, other.sources[0].Decision.OriginConcurrency = 1, 1
	for key, entry := range other.r.data {
		if key == oldBase || strings.HasPrefix(key, oldBase+":") {
			newKey := newBase + strings.TrimPrefix(key, oldBase)
			if entry.kind == "hash" {
				if strings.Contains(key, ":job:") {
					entry.hash["run_id"] = string(other.runID)
					source, _ := completeSourceJobRecord(other.sources[0])
					for _, field := range source {
						entry.hash[field.Name] = string(field.Value)
					}
				} else if key == oldBase {
					digest, _ := DerivePolicyGroupMapDigest(other.groups)
					entry.hash["policy_group_map_sha256"], entry.hash["crawl_policy_sha256"] = string(digest), strings.Repeat("b", 64)
				} else if key == oldBase+":group_concurrency" {
					entry.hash["default"] = "1"
				}
			}
			f.r.data[newKey] = entry
			if set, exists := other.r.sets[key]; exists {
				f.r.sets[newKey] = set
			}
			if set, exists := other.r.zsets[key]; exists {
				f.r.zsets[newKey] = set
			}
		}
	}
	f.r.zsets[RunsKey][string(other.runID)] = other.r.zsets[RunsKey][string(oldID)]
	f.r.sets[ActiveRunsKey][string(other.runID)], f.r.sets[UnarchivedRunsKey][string(other.runID)] = true, true
	other.r = f.r
	var err error
	other.policy, err = (transportAuthority{seal: &redisTransportAuthoritySeal}).parseRunPolicyAuthority(other.runID,
		workerLuaHashRecord(t, other.r, newBase, SchemaRun), other.groups)
	if err != nil {
		t.Fatal(err)
	}
	j := workerLuaIntent(t, other, 0, 1, RequestDocument)
	before := f.r.snapshot()
	workerLuaReply(t, other, OperationTryClaim, j, StatusCapacityBlocked)
	// Strict tightening is the sole permitted blocked side effect.
	for key, entry := range before.data {
		if key == RateScopesKey || strings.HasPrefix(key, requestLuaPrefix+"rate:") {
			continue
		}
		if !reflect.DeepEqual(entry, f.r.data[key]) {
			t.Fatalf("blocked tightening changed non-scope key %s", key)
		}
	}
	for _, scope := range []Digest{i.Decision.GroupScopeID, i.Decision.OriginScopeID} {
		hash := f.r.data[requestLuaPrefix+"rate:"+string(scope)].hash
		if hash["effective_concurrency"] != "1" || hash["effective_interval_ms"] != "1000" || hash["pending_count"] != "1" {
			t.Fatal("tightening removed grandfathered pending capacity")
		}
	}
	workerLuaReply(t, f, OperationTryClaim, i, StatusAlreadyClaimed)
	workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
	workerLuaReply(t, f, OperationFinishRequest, i, StatusFinished)
	i.RequestOrdinal = 2
	workerLuaReply(t, f, OperationReserveRequest, i, StatusRateBlocked)
}

func workerLuaVisitedFixture(t *testing.T) *workerLuaFixture {
	t.Helper()
	f := workerLuaFixtureNew(t, 2, 0)
	base := requestLuaPrefix + "run:" + string(f.runID)
	peer, target := f.sources[1], f.sources[0]
	publication, _ := DerivePublicationID(PublicationIdentity{RunID: f.runID, JobID: peer.JobID, Fence: 1, OutputDigest: Digest(strings.Repeat("7", 64))})
	commit, _ := DeriveCommitID(CommitIdentity{RunID: f.runID, JobID: peer.JobID, OwnerID: OwnerID(strings.Repeat("3", 32)),
		Fence: 1, Token: LeaseToken(strings.Repeat("4", 64)), PublicationID: publication, RequestStartsBaseline: 0, RequestStartsGeneration: 2})
	pageKey, _ := PageDataKey(publication, target.CanonicalURL)
	at := canonicalDecimal(f.r.now - 100)
	job := f.r.data[base+":job:"+string(peer.JobID)].hash
	for name, value := range map[string]string{"state": "completed", "claim_count": "1", "lease_fence": "1", "delivery_attempts": "1", "request_starts": "2",
		"next_request_ordinal": "3", "last_request_started_at_ms": at, "last_document_request_started_at_ms": at, "last_document_request_fence": "1",
		"last_document_target_url_id": string(target.JobID), "last_document_target_url": target.CanonicalURL, "last_document_target_digest": string(target.Decision.TargetDigest),
		"last_stage_commit_id": string(commit), "last_stage_fence": "1", "output_digest": strings.Repeat("7", 64), "publication_id": string(publication), "commit_id": string(commit),
		"published_page_key": pageKey, "last_reason": "published", "completed_at_ms": at} {
		job[name] = value
	}
	workerLuaHashRecord(t, f.r, base+":job:"+string(peer.JobID), SchemaJob)
	delete(f.r.zsets[base+":ready"], string(peer.JobID))
	delete(f.r.zsets[base+":ready_at"], string(peer.JobID))
	f.r.setZSet(base+":completed", map[string]float64{string(peer.JobID): float64(f.r.now - 100)})
	for name, value := range map[string]string{"open_job_count": "1", "completed_total": "1", "output_commits_total": "1", "request_starts": "2", "reservation_creations_total": "2",
		"claims_total": "1", "last_execution_at_ms": at, "last_request_started_at_ms": at, "last_terminal_transition_at_ms": at} {
		f.r.data[base].hash[name] = value
	}
	f.r.data[base+":group_open_jobs"].hash["default"], f.r.data[base+":group_started"].hash["default"] = "1", "2"
	f.r.data[base+":disposition_reason_counts"].hash["published"] = "1"
	f.r.setHash(base+":visited_urls", Record{textField(string(target.JobID), target.CanonicalURL), textField(string(peer.JobID), peer.CanonicalURL)})
	f.r.setHash(base+":visited_depth", Record{textField(string(target.JobID), "2"), textField(string(peer.JobID), "2")})
	return f
}

func TestWorkerLuaVisitedAndInspectedSourceBeforeCapacity(t *testing.T) {
	t.Parallel()
	f := workerLuaVisitedFixture(t)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	base := requestLuaPrefix + "run:" + string(f.runID)
	before := f.r.data[base].hash["reservation_creations_total"]
	workerLuaReply(t, f, OperationTryClaim, i, StatusVisitedCompleted)
	if f.r.data[base].hash["reservation_creations_total"] != before || f.r.data[base].hash["claims_total"] != "1" ||
		f.r.data[base+":job:"+string(i.Lease.JobID)].hash["lease_request_starts_baseline"] != "0" {
		t.Fatal("visited completion claimed or reserved work")
	}
	snapshot := f.r.snapshot()
	workerLuaReply(t, f, OperationTryClaim, i, StatusVisitedCompleted)
	if !reflect.DeepEqual(snapshot, f.r.snapshot()) {
		t.Fatal("visited replay mutated ledger")
	}
	for _, test := range []string{"missing_depth", "wrong_url", "noncanonical_depth", "changed_source_depth"} {
		f := workerLuaVisitedFixture(t)
		i := workerLuaIntent(t, f, 0, 1, RequestDocument)
		switch test {
		case "missing_depth":
			delete(f.r.data[base+":visited_depth"].hash, string(i.Lease.JobID))
		case "wrong_url":
			f.r.data[base+":visited_urls"].hash[string(i.Lease.JobID)] = "https://elsewhere.example/"
		case "noncanonical_depth":
			f.r.data[base+":visited_depth"].hash[string(i.Lease.JobID)] = "02"
		case "changed_source_depth":
			f.sources[0].Depth, f.sources[0].Decision.Depth, i.Decision.Depth = 3, 3, 3
		}
		before := f.r.snapshot()
		result := workerLuaCall(t, f, OperationTryClaim, i)
		if result.runtimeErr != nil {
			t.Fatal(result.runtimeErr)
		}
		if _, ok := result.raw.(bootLuaErrorReply); !ok || !reflect.DeepEqual(before, f.r.snapshot()) {
			t.Fatalf("bad visited/source %s accepted", test)
		}
	}
}

func TestWorkerLuaOrdinaryMemoryCannotSpendSafety(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	f.r.maximum = f.r.used + 67108864 + 16777216
	workerLuaReject(t, f, OperationTryClaim, i, ErrorMemoryHeadroomLow)
	f.r.maximum = 400 * 1024 * 1024
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	f.r.now++
	f.r.maximum = f.r.used + 67108864 + 1024*1024
	workerLuaReject(t, f, OperationRenewLease, i, ErrorMemoryHeadroomLow)
	workerLuaReject(t, f, OperationStartRequest, i, ErrorMemoryHeadroomLow)
}

func workerLuaStageFixture(t *testing.T) (*workerLuaFixture, ReservationIntent, Digest, uint64) {
	t.Helper()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
	workerLuaReply(t, f, OperationFinishRequest, i, StatusFinished)
	created, expires := f.r.now+1000, f.r.now+901000
	output := Digest(strings.Repeat("7", 64))
	publication, _ := DerivePublicationID(PublicationIdentity{RunID: f.runID, JobID: i.Lease.JobID, Fence: 1, OutputDigest: output})
	commit, _ := DeriveCommitID(CommitIdentity{RunID: f.runID, JobID: i.Lease.JobID, OwnerID: i.Lease.OwnerID, Fence: 1, Token: i.Lease.Token,
		PublicationID: publication, RequestStartsBaseline: 0, RequestStartsGeneration: 1})
	token, _ := DeriveTokenDigest(i.Lease)
	meta := recordAuthorityUnsealedStageRecord(t, "0")
	for field, value := range map[int]string{stageRunIDIndex: string(f.runID), stageJobIDIndex: string(i.Lease.JobID), stageOwnerIDIndex: string(i.Lease.OwnerID),
		stageTokenDigestIndex: string(token), stageCommitIDIndex: string(commit), stagePublicationIDIndex: string(publication), stageOutputDigestIndex: string(output),
		stageCreatedAtMSIndex: canonicalDecimal(created), stageExpiresAtMSIndex: canonicalDecimal(expires)} {
		recordAuthoritySet(meta, field, value)
	}
	if err := ValidateRecord(SchemaStageMeta, meta); err != nil {
		t.Fatal(err)
	}
	stagePrefix := requestLuaPrefix + "stage:" + string(commit) + ":"
	f.r.setHash(stagePrefix+"meta", meta)
	f.r.setList(stagePrefix+"keys", []string{stagePrefix + "meta", stagePrefix + "keys"})
	for _, key := range []string{stagePrefix + "meta", stagePrefix + "keys"} {
		entry := f.r.data[key]
		entry.expireAt = int64(expires)
		f.r.data[key] = entry
	}
	f.r.setHash(StageSlotsKey, Record{textField(string(commit), "50000000:"+string(f.runID)+":"+string(i.Lease.JobID)+":1:0")})
	f.r.setZSet(StageExpiryKey, map[string]float64{string(commit): float64(expires)})
	base := requestLuaPrefix + "run:" + string(f.runID)
	job := f.r.data[base+":job:"+string(i.Lease.JobID)].hash
	job["active_stage_commit_id"], job["last_stage_commit_id"], job["last_stage_fence"] = string(commit), string(commit), "1"
	job["last_transition_id"], job["last_transition_status"], job["lease_expires_at_ms"] = "", "", canonicalDecimal(expires-10000)
	job["updated_at_ms"], f.r.data[base].hash["last_activity_at_ms"] = canonicalDecimal(expires-70000), canonicalDecimal(expires-70000)
	f.r.zsets[base+":leased"][string(i.Lease.JobID)], f.r.zsets[ActiveLeasesKey][string(f.runID)+":"+string(i.Lease.JobID)] = float64(expires-10000), float64(expires-10000)
	f.r.now = expires - 30000
	return f, i, commit, expires
}

func TestWorkerLuaRenewalStageCapAndAbortedNoReopen(t *testing.T) {
	t.Parallel()
	f, i, commit, expires := workerLuaStageFixture(t)
	reply := workerLuaReply(t, f, OperationRenewLease, i, StatusRenewed)
	if reply[2] != canonicalDecimal(expires) {
		t.Fatal("renewal exceeded/changed absolute stage cap")
	}
	// Exact aborted-stage slot: all keys absent, expiry absent, source-bound abort
	// transition retained. The empty active pointer must NOT reopen renewal.
	for _, key := range wireOracleStageKeys(commit) {
		f.r.removeKey(key)
	}
	f.r.removeKey(StageExpiryKey)
	f.r.data[StageSlotsKey].hash[string(commit)] = "50000000:" + string(f.runID) + ":" + string(i.Lease.JobID) + ":1:2"
	transition, err := DeriveAbortStageTransitionID(AbortStageTransitionInput{Lease: i.Lease, CommitID: commit})
	if err != nil {
		t.Fatal(err)
	}
	job := f.r.data[requestLuaPrefix+"run:"+string(f.runID)+":job:"+string(i.Lease.JobID)].hash
	job["active_stage_commit_id"], job["last_transition_status"], job["last_transition_id"] = "", "STAGE_ABORTED", string(transition)
	workerLuaReject(t, f, OperationRenewLease, i, ErrorInvalidState)
}

func TestWorkerLuaExpiredStagedRenewalCounter(t *testing.T) {
	t.Parallel()
	f, i, _, expires := workerLuaStageFixture(t)
	f.r.now = expires - 10000 // lease is due, stage still has ten seconds of life
	workerLuaReply(t, f, OperationRenewLease, i, StatusLeaseLost)
	if f.r.data[requestLuaPrefix+"run:"+string(f.runID)].hash["renewal_rejections_total"] != "1" {
		t.Fatal("validated expired staged renewal was not counted")
	}
}

func TestWorkerLuaFirstReplacementAndFirstEvidenceCorruption(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	workerLuaReply(t, f, OperationCancelReservation, i, StatusReservationCancelled)
	i.RequestOrdinal, i.Decision.RequestKind = 2, RequestRedirect
	workerLuaReject(t, f, OperationReserveRequest, i, ErrorImmutableMismatch)
	i.Decision.RequestKind = RequestDocument
	workerLuaReply(t, f, OperationReserveRequest, i, StatusReserved)
	workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
	id, _ := DeriveReservationID(f.policy, i)
	key := requestLuaPrefix + "reservation:" + string(id)
	if f.r.data[key].hash["job_starts_after_start"] != "1" || f.r.data[key].hash["delivery_attempts_after_start"] != "1" {
		t.Fatal("cancelled first intent consumed a START or delivery")
	}
	f.r.removeKey(FirstRequestStartKey)
	workerLuaReject(t, f, OperationStartRequest, i, ErrorStateIndexCorrupt)
}

func TestWorkerLuaLateCorruptionPreemptsBlockedStatus(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 2, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
	workerLuaReply(t, f, OperationFinishRequest, i, StatusFinished)
	base := requestLuaPrefix + "run:" + string(f.runID)
	f.r.data[base].hash["max_request_starts"] = "1" // would ordinarily block next claim
	other := workerLuaIntent(t, f, 1, 1, RequestDocument)
	// The last scope is still validated even when a higher-precedence budget is
	// already exhausted. No monotonic tightening may repair this corruption.
	key := requestLuaPrefix + "rate:" + string(i.Decision.OriginScopeID)
	f.r.data[key].hash["scope_witness"] = "https://different.example:443"
	before := f.r.snapshot()
	result := workerLuaCall(t, f, OperationTryClaim, other)
	if result.runtimeErr != nil {
		t.Fatal(result.runtimeErr)
	}
	if _, ok := result.raw.(bootLuaErrorReply); !ok || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
		t.Fatal("late scope corruption became a blocked status or mutation")
	}
}

func TestWorkerLuaLeaseAndStageCapacityPrecedence(t *testing.T) {
	t.Parallel()
	for _, count := range []int{4, 64} {
		f := workerLuaFixtureNew(t, 1, 0)
		i := workerLuaIntent(t, f, 0, 1, RequestDocument)
		other := strings.Repeat("9", 32)
		f.r.zsets[RunsKey][other] = float64(f.r.now - 400)
		f.r.sets[ActiveRunsKey][other], f.r.sets[UnarchivedRunsKey][other] = true, true
		leases, slots := map[string]float64{}, Record{}
		for n := 1; n <= count; n++ {
			job := fmt.Sprintf("%064x", n)
			leases[other+":"+job] = float64(f.r.now + 30000)
			if n <= 4 {
				slots = append(slots, textField(fmt.Sprintf("%064x", n+100), "50000000:"+other+":"+job+":1:0"))
			}
		}
		f.r.setZSet(ActiveLeasesKey, leases)
		f.r.setHash(StageSlotsKey, slots)
		before := f.r.snapshot()
		status := StatusStageCapacityBlocked
		if count == 64 {
			status = StatusLeaseCapacityBlocked
		}
		workerLuaReply(t, f, OperationTryClaim, i, status)
		if !reflect.DeepEqual(before, f.r.snapshot()) {
			t.Fatal("capacity-only block changed ledger or materialized a scope")
		}
	}
}

func TestWorkerLuaCanonicalURLIDNAAndIPBoundary(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"https://xn--bcher-kva.example:443/été", "http://example.com:80/", "https://example.com/a%FF?q=%2F", "https://example.com:8443/"} {
		identity, err := utils.CanonicalizeURLV1(raw)
		if err != nil {
			t.Fatal(err)
		}
		f := workerLuaFixtureNew(t, 1, 0)
		base, prior := requestLuaPrefix+"run:"+string(f.runID), f.sources[0]
		target := RequestTarget{URLID: JobID(identity.URLID), CanonicalURL: identity.CanonicalURL}
		decision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestDocument, Target: target, Depth: 2, GroupID: "default",
			RateScopeID: prior.RateScopeID, GroupConcurrency: 3, OriginConcurrency: 3})
		if err != nil {
			t.Fatal(err)
		}
		f.sources[0] = SourceJob{JobID: target.URLID, CanonicalURL: target.CanonicalURL, Depth: 2, ScoreText: "0", GroupID: "default", RateScopeID: prior.RateScopeID, Decision: decision}
		job := workerLuaHashRecord(t, f.r, base+":job:"+string(prior.JobID), SchemaJob)
		source, _ := completeSourceJobRecord(f.sources[0])
		jobLuaApplySource(job, source)
		f.r.removeKey(base + ":job:" + string(prior.JobID))
		f.r.setHash(base+":job:"+string(target.URLID), job)
		f.r.setSet(base+":jobs", []string{string(target.URLID)})
		for _, index := range []string{"job_order", "ready", "ready_at"} {
			score := f.r.zsets[base+":"+index][string(prior.JobID)]
			f.r.setZSet(base+":"+index, map[string]float64{string(target.URLID): score})
		}
		f.r.data[base].hash["audit_cursor"] = string(target.URLID)
		i := workerLuaIntent(t, f, 0, 1, RequestDocument)
		workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
		workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
	}
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	request := workerLuaRequest(t, f, OperationTryClaim, i)
	keys, args := runLuaParts(t, request, nil)
	url := "https://127.0.0.1/"
	target := RequestTarget{URLID: JobID(utils.URLIDV1(url)), CanonicalURL: url}
	digest, err := DeriveTargetDigest(target)
	if err != nil {
		t.Fatal("IP URL identity must remain valid", err)
	}
	for index, field := range request.semantic {
		switch field.Name {
		case "canonical_target_url":
			args[7+index] = url
		case "target_url_id":
			args[7+index] = string(target.URLID)
		case "target_digest":
			args[7+index] = string(digest)
		}
	}
	before := f.r.snapshot()
	result := workerLuaRun(t, f.r, workerLuaSource(t, OperationTryClaim), keys, args)
	if result.runtimeErr != nil {
		t.Fatal(result.runtimeErr)
	}
	if _, ok := result.raw.(bootLuaErrorReply); !ok || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("IP request acquired rate/lease authority")
	}
}

func TestWorkerLuaRenewalCounterExcludesMissingUnleasedAndCorrupt(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	before := f.r.snapshot()
	workerLuaReply(t, f, OperationRenewLease, i, StatusLeaseLost)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("unleased job counted a renewal rejection")
	}
	missing := i
	missing.Lease.JobID = JobID(strings.Repeat("f", 64))
	request := workerLuaRequest(t, f, OperationRenewLease, missing)
	keys, args := runLuaParts(t, request, nil)
	got := sharedLuaNoError(t, workerLuaRun(t, f.r, workerLuaSource(t, OperationRenewLease), keys, args)).([]any)
	if !reflect.DeepEqual(got, []any{string(StatusLeaseLost), canonicalDecimal(f.r.now), "0"}) || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("missing job counted a renewal rejection")
	}
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	key := requestLuaPrefix + "run:" + string(f.runID) + ":job:" + string(i.Lease.JobID)
	f.r.data[key].hash["lease_fence"] = "2" // contradicts claim_count, not a stale caller
	before = f.r.snapshot()
	result := workerLuaCall(t, f, OperationRenewLease, i)
	if result.runtimeErr != nil {
		t.Fatal(result.runtimeErr)
	}
	if _, ok := result.raw.(bootLuaErrorReply); !ok || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("corrupt job counted a renewal rejection")
	}
}

func TestWorkerLuaTerminalSafetyAdmissionAndLastACL(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationFinishRequest, OperationCancelReservation} {
		setup := func() (*workerLuaFixture, ReservationIntent) {
			f := workerLuaFixtureNew(t, 1, 0)
			i := workerLuaIntent(t, f, 0, 1, RequestDocument)
			workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
			if op == OperationFinishRequest {
				workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
			}
			f.r.maximum = f.r.used + 67108864 + 1024*1024 // only safety headroom
			return f, i
		}
		status := StatusFinished
		if op == OperationCancelReservation {
			status = StatusReservationCancelled
		}
		f, i := setup()
		workerLuaReply(t, f, op, i, status)
		writes := f.r.aclCount
		if writes < 10 {
			t.Fatal("terminal operation omitted its three-scope mutation")
		}
		f, i = setup()
		f.r.denyAt = writes
		workerLuaReject(t, f, op, i, ErrorBootUnapproved)
		f, i = setup()
		f.r.maximum = f.r.used + 67108864
		workerLuaReject(t, f, op, i, ErrorMemoryHeadroomLow)
		f, i = setup()
		f.r.failAt, f.r.failAfter = writes, true
		before := f.r.snapshot()
		result := workerLuaCall(t, f, op, i)
		if result.runtimeErr == nil || reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != writes {
			t.Fatal("terminal last-write failure did not propagate as partial mutation")
		}
	}
}

func TestWorkerLuaReusableReleasePending(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	f.r.now++
	workerLuaReply(t, f, OperationReleaseBeforeIO, i, StatusReleasedReady)
	base := requestLuaPrefix + "run:" + string(f.runID)
	jobKey := base + ":job:" + string(i.Lease.JobID)
	id, _ := DeriveReservationID(f.policy, i)
	reservationKey := requestLuaPrefix + "reservation:" + string(id)
	q := workerLuaHashRecord(t, f.r, reservationKey, SchemaReservation)
	if string(q[reservationStateIndex].Value) != "cancelled" || f.r.data[reservationKey].expireAt != int64(f.r.now+86400000) ||
		f.r.data[jobKey].hash["active_reservation_id"] != "" || f.r.data[jobKey].hash["state"] != "ready" ||
		f.r.data[base].hash["pending_request_reservations"] != "0" || f.r.data[base].hash["reservation_creations_total"] != "1" ||
		f.r.data[base].hash["request_starts"] != "0" {
		t.Fatal("release did not compose the exact pending-capacity effects")
	}
	jobWrites := 0
	for _, call := range f.r.trace {
		if !call.acl && call.name == "HSET" && call.args[0] == jobKey {
			jobWrites++
		}
	}
	if jobWrites != 1 {
		t.Fatalf("Request/Job duplicated the pointer wipe: %d job HSETs", jobWrites)
	}
	before := f.r.snapshot()
	f.r.now++
	workerLuaReply(t, f, OperationReleaseBeforeIO, i, StatusReleasedReady)
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("release replay re-applied terminal effects or extended the tombstone")
	}
}

// These sources inspect the reusable builder contract only, not a replacement
// worker handler: actual clocked wire/gates/readers/planner, no execution loop,
// no fake authority/coverage/reply validator and no completed-operation status.
func workerLuaHelperSource(t *testing.T, op OperationName, body string) string {
	t.Helper()
	return workerLuaCore(t) + `
local ctx,code=CJ.Context.open(CJ.Wire.worker_spec(` + jobLuaQuote([]byte(op)) + `),KEYS,ARGV)
if not ctx then return CJ.Context.reject(code) end
local gate; gate,code=CJ.Gate.check(ctx); if not gate then return CJ.Context.reject(code) end
local run; run,code=CJ.Run.load(ctx,ctx.request.v.run_id); if not run then return CJ.Context.reject(code) end
local job; job,code=CJ.Read.fixed_hash(ctx,ctx.keys.job,"job"); if not job then return CJ.Context.reject(code) end
` + body
}

func TestWorkerLuaReusableLoadDoesNotTrustPublicPointer(t *testing.T) {
	t.Parallel()
	for _, empty := range []bool{false, true} {
		for _, forged := range []bool{false, true} {
			f := workerLuaFixtureNew(t, 1, 0)
			i := workerLuaIntent(t, f, 0, 1, RequestDocument)
			workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
			if empty {
				workerLuaReply(t, f, OperationCancelReservation, i, StatusReservationCancelled)
			}
			request := workerLuaRequest(t, f, OperationReleaseBeforeIO, i)
			keys, args := runLuaParts(t, request, nil)
			body := ""
			if forged {
				value := ""
				if empty {
					value = strings.Repeat("f", 64)
				}
				body += "job.v.active_reservation_id=" + jobLuaQuote([]byte(value)) + "\n"
			}
			body += `job.n={active_reservation_id="not authority"}
local held,err=CJ.Request.load_live(ctx,run,job)
if held==nil then return CJ.Context.reject(err) end
if held==false then return {"empty"} end
return {held.v.reservation_id,held.v.state,held.v.request_ordinal}`
			before := f.r.snapshot()
			result := workerLuaRun(t, f.r, workerLuaHelperSource(t, OperationReleaseBeforeIO, body), keys, args)
			if result.runtimeErr != nil {
				t.Fatal(result.runtimeErr)
			}
			if !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
				t.Fatal("load_live mutated ledger")
			}
			if forged {
				if _, ok := result.raw.(bootLuaErrorReply); !ok {
					t.Fatal("public pointer fabricated empty/live evidence")
				}
			} else if empty {
				if !reflect.DeepEqual(result.raw, []any{"empty"}) {
					t.Fatalf("known empty pointer: %v", result.raw)
				}
			} else {
				id, _ := DeriveReservationID(f.policy, i)
				if !reflect.DeepEqual(result.raw, []any{string(id), "pending", "1"}) {
					t.Fatalf("held record: %v", result.raw)
				}
			}
		}
	}
}

func TestWorkerLuaReusableTerminalEffectsNoFlushSealOrJobWrite(t *testing.T) {
	t.Parallel()
	for _, terminal := range []string{"cancelled", "finished"} {
		f := workerLuaFixtureNew(t, 1, 0)
		i := workerLuaIntent(t, f, 0, 1, RequestDocument)
		workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
		op, previous := OperationCancelReservation, "pending"
		if terminal == "finished" {
			workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
			op, previous = OperationFinishRequest, "started"
		}
		f.r.now++
		request := workerLuaRequest(t, f, op, i)
		keys, args := runLuaParts(t, request, nil)
		body := `
local held,err=CJ.Request.load_live(ctx,run,job); if not held then return CJ.Context.reject(err) end
local plan; plan,err=CJ.Plan.new(ctx); if not plan then return CJ.Context.reject(err) end
local ok; ok,err=CJ.Plan.set_policy(plan,"safety"); if not ok then return CJ.Context.reject(err) end
local delta; delta,err=CJ.Run.plan_delta(ctx,run); if not delta then return CJ.Context.reject(err) end
local effects; effects,err=CJ.Request.plan_terminal(ctx,plan,delta,run,job,` + jobLuaQuote([]byte(terminal)) + `,"ordinary")
if not effects then return CJ.Context.reject(err) end
-- An unflushed delta is still open for its outcome caller's additional changes.
ok,err=CJ.Run.accumulate(delta,{}); if not ok then return CJ.Context.reject(err) end
-- No job update/flush/assessment/seal is done on the caller's behalf.
if not CJ.Context.preparing(ctx) then return CJ.Context.reject("INVALID_STATE") end
ok,err=CJ.Run.hset(plan,job.key,effects.job_fields,"ordinary"); if not ok then return CJ.Context.reject(err) end
local encoded; encoded,err=CJ.Schemas.encode(effects.record); if not encoded then return CJ.Context.reject(err) end
return {effects.reservation_id,effects.previous_state,effects.state,effects.charged_group_id,effects.source_group_id,
  effects.job_key,effects.job_fields.active_reservation_id,effects.job_fields.updated_at_ms,effects.tombstone_expires_at_ms,
  encoded,effects.scopes.global.v.active_count,effects.scopes.group.v.pending_count,effects.scopes.origin.v.started_count}`
		before := f.r.snapshot()
		result := workerLuaRun(t, f.r, workerLuaHelperSource(t, op, body), keys, args)
		got := sharedLuaNoError(t, result).([]any)
		id, _ := DeriveReservationID(f.policy, i)
		jobKey := requestLuaPrefix + "run:" + string(f.runID) + ":job:" + string(i.Lease.JobID)
		wantRecord := workerLuaHashRecord(t, f.r, requestLuaPrefix+"reservation:"+string(id), SchemaReservation)
		recordAuthoritySet(wantRecord, reservationStateIndex, terminal)
		recordAuthoritySet(wantRecord, reservationTerminalAtMSIndex, canonicalDecimal(f.r.now))
		if err := ValidateRecord(SchemaReservation, wantRecord); err != nil {
			t.Fatal(err)
		}
		encoded, _ := EncodeRecord(wantRecord)
		want := []any{string(id), previous, terminal, "default", "default", jobKey, "", canonicalDecimal(f.r.now),
			canonicalDecimal(f.r.now + 86400000), string(encoded), "0", "0", "0"}
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.aclCount != 0 || f.r.attempts != 0 {
			t.Fatalf("incorrect/unexpected executed terminal effects: %v", got)
		}
	}
}

func TestWorkerLuaReusableTerminalRejectsDuplicateAndInvalidCoverage(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"duplicate", "numeric", "forged_unit", "early_expired", "wrong_state"} {
		f := workerLuaFixtureNew(t, 1, 0)
		i := workerLuaIntent(t, f, 0, 1, RequestDocument)
		workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
		request := workerLuaRequest(t, f, OperationCancelReservation, i)
		keys, args := runLuaParts(t, request, nil)
		body := `
local held,err=CJ.Request.load_live(ctx,run,job); if not held then return CJ.Context.reject(err) end
local plan; plan,err=CJ.Plan.new(ctx); if not plan then return CJ.Context.reject(err) end
local delta; delta,err=CJ.Run.plan_delta(ctx,run); if not delta then return CJ.Context.reject(err) end
local state,coverage="cancelled","ordinary"
`
		switch bad {
		case "duplicate":
			body += `local first; first,err=CJ.Request.plan_terminal(ctx,plan,delta,run,job,state,coverage); if not first then return CJ.Context.reject(err) end` + "\n"
		case "numeric":
			body += "coverage=0\n"
		case "forged_unit":
			body += "coverage={}\n"
		case "early_expired":
			body += "state='expired'\n"
		case "wrong_state":
			body += "state='finished'\n"
		}
		body += `local result; result,err=CJ.Request.plan_terminal(ctx,plan,delta,run,job,state,coverage)
if not result then return CJ.Context.reject(err) end
return {"unexpected acceptance"}`
		before := f.r.snapshot()
		result := workerLuaRun(t, f.r, workerLuaHelperSource(t, OperationCancelReservation, body), keys, args)
		if result.runtimeErr != nil {
			t.Fatal(result.runtimeErr)
		}
		if _, ok := result.raw.(bootLuaErrorReply); !ok || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
			t.Fatalf("terminal effects accepted %s", bad)
		}
	}
}

func workerLuaAddChargedGroup(t *testing.T, f *workerLuaFixture) {
	t.Helper()
	base := requestLuaPrefix + "run:" + string(f.runID)
	if f.r.data[base].hash["reservation_creations_total"] != "0" {
		t.Fatal("group fixture must be pinned before any worker request")
	}
	group := f.groups[0]
	group.GroupID = "other"
	f.groups = append(f.groups, group) // same lineage: all three scopes can be shared
	digest, err := DerivePolicyGroupMapDigest(f.groups)
	if err != nil {
		t.Fatal(err)
	}
	f.r.data[base].hash["policy_group_count"], f.r.data[base].hash["policy_group_map_sha256"] = "2", string(digest)
	for name, value := range map[string]string{"group_limits": canonicalDecimal(group.RequestStartLimit), "group_rate_scope_ids": string(group.RateScopeID),
		"group_scope_ids": string(group.GroupScopeID), "group_concurrency": canonicalDecimal(group.Concurrency), "group_interval_ms": canonicalDecimal(group.IntervalMS),
		"group_started": "0", "group_pending": "0", "group_active_started": "0", "group_open_jobs": "0", "audit_group_counts": "0"} {
		f.r.data[base+":"+name].hash["other"] = value
	}
	f.policy, err = (transportAuthority{seal: &redisTransportAuthoritySeal}).parseRunPolicyAuthority(f.runID, workerLuaHashRecord(t, f.r, base, SchemaRun), f.groups)
	if err != nil {
		t.Fatal(err)
	}
}

func workerLuaOtherGroupIntent(t *testing.T, f *workerLuaFixture, i ReservationIntent) ReservationIntent {
	t.Helper()
	target := RequestTarget{CanonicalURL: "https://example.com/redirected", URLID: JobID(utils.URLIDV1("https://example.com/redirected"))}
	decision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestRedirect, Target: target, Depth: i.Decision.Depth,
		GroupID: f.groups[1].GroupID, RateScopeID: f.groups[1].RateScopeID, GroupConcurrency: f.groups[1].Concurrency, OriginConcurrency: f.groups[1].Concurrency,
		GroupIntervalMS: f.groups[1].IntervalMS, OriginIntervalMS: f.groups[1].IntervalMS})
	if err != nil {
		t.Fatal(err)
	}
	i.RequestOrdinal, i.Target, i.Decision = 2, target, decision
	return i
}

func TestWorkerLuaReusableTerminalChargesRequestGroupNotSource(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	workerLuaAddChargedGroup(t, f)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
	workerLuaReply(t, f, OperationFinishRequest, i, StatusFinished)
	i = workerLuaOtherGroupIntent(t, f, i)
	workerLuaReply(t, f, OperationReserveRequest, i, StatusReserved)
	base := requestLuaPrefix + "run:" + string(f.runID)
	if f.r.data[base+":group_open_jobs"].hash["other"] != "0" || f.r.data[base+":group_pending"].hash["other"] != "1" {
		t.Fatal("bad source/charged-group fixture")
	}
	workerLuaReply(t, f, OperationCancelReservation, i, StatusReservationCancelled)
	if f.r.data[base+":group_open_jobs"].hash["default"] != "1" || f.r.data[base+":group_pending"].hash["other"] != "0" ||
		f.r.data[base+":group_started"].hash["default"] != "1" || f.r.data[base+":group_started"].hash["other"] != "0" {
		t.Fatal("terminal effect refunded starts or changed source-group open accounting")
	}
}

func TestWorkerLuaReusableExpiredEffectsShareScopesAndRunDelta(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 2, 0)
	workerLuaAddChargedGroup(t, f)
	pending, started := workerLuaIntent(t, f, 0, 1, RequestDocument), workerLuaIntent(t, f, 1, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, pending, StatusClaimed)
	workerLuaReply(t, f, OperationTryClaim, started, StatusClaimed)
	workerLuaReply(t, f, OperationStartRequest, started, StatusStarted)
	workerLuaReply(t, f, OperationFinishRequest, started, StatusFinished)
	started = workerLuaOtherGroupIntent(t, f, started)
	workerLuaReply(t, f, OperationReserveRequest, started, StatusReserved)
	workerLuaReply(t, f, OperationStartRequest, started, StatusStarted)
	f.r.now += 60000
	request, err := NewRecoverExpiredWireRequest(runLuaGate(t, f.a, OperationRecoverExpired, false), f.runID)
	if err != nil {
		t.Fatal(err)
	}
	keys, args := runLuaParts(t, request, nil)
	// Exercise real RECOVER key/unit permissions and Run contributor attribution,
	// but deliberately stop at assessment: this is request-effects verification,
	// not an implementation/acceptance claim for the maintenance outcome handler.
	source := workerLuaCore(t) + `
local ctx,code=CJ.Context.open(CJ.Wire.maintenance_spec("CJ2_RECOVER_EXPIRED"),KEYS,ARGV)
if not ctx then return CJ.Context.reject(code) end
local gate; gate,code=CJ.Gate.check(ctx); if not gate then return CJ.Context.reject(code) end
local run; run,code=CJ.Run.load(ctx,ctx.request.v.run_id); if not run then return CJ.Context.reject(code) end
local selected; selected,code=CJ.Read.all_members(ctx,ctx.keys.run_leased,"zset",64,64)
if not selected then return CJ.Context.reject(code) end
local plan; plan,code=CJ.Plan.new(ctx); if not plan then return CJ.Context.reject(code) end
local ok; ok,code=CJ.Plan.set_policy(plan,"recovery"); if not ok then return CJ.Context.reject(code) end
local delta; delta,code=CJ.Run.plan_delta(ctx,run); if not delta then return CJ.Context.reject(code) end
local last,effects
for _,id in ipairs(selected.ordered) do
    local binding; binding,code=CJ.Context.bind_job(ctx,id); if not binding then return CJ.Context.reject(code) end
    local job; job,code=CJ.Read.fixed_hash(ctx,binding.key,"job"); if not job then return CJ.Context.reject(code) end
    local held; held,code=CJ.Request.load_live(ctx,run,job); if not held then return {"load_live",code} end
    local unit; unit,code=CJ.Plan.recovery_unit(plan,id); if not unit then return {"recovery_unit",code} end
    effects,code=CJ.Request.plan_terminal(ctx,plan,delta,run,job,"expired",unit)
    if not effects then return {"plan_terminal",code} end
    -- The fixture caller composes the two valid job outcomes. Core intentionally
    -- will not assess request expiry as a complete recovery without them. Use
    -- the real pure Job builder; Request emits no duplicate job HSET/flush.
    local changes={last_transition_id="",last_transition_status=""}
    local counters={recovered_leases_total=1}
    local maps={recovery_outcome_counts={}}
    local indexes={leased=-1,leased_at=-1}
    local calls={{"ZREM",ctx.keys.run_leased,id},{"ZREM",ctx.keys.run_leased_at,id},
        {"ZREM",ctx.keys.active_leases,run.run_id..":"..id}}
    if held.v.state=="pending" then
        changes.state,changes.last_reason="ready","none"
        changes.pre_io_recoveries=CJ.P.format_decimal(job.n.pre_io_recoveries+1)
        maps.recovery_outcome_counts.ready=1; indexes.ready,indexes.ready_at=1,1
        calls[#calls+1]={"ZADD",ctx.keys.run_ready,job.v.score_text,id}
        calls[#calls+1]={"ZADD",ctx.keys.run_ready_at,ctx.now_text,id}
    else
        changes.state,changes.last_reason,changes.last_failure_reason="delayed","lease_expired_after_io","lease_expired_after_io"
        changes.retry_count=CJ.P.format_decimal(job.n.retry_count+1)
        changes.not_before_ms=CJ.P.format_decimal(CJ.P.safe_add(ctx.now_ms,30000))
        counters.retries_total=1; maps.recovery_outcome_counts.delayed=1; indexes.delayed=1
        maps.retry_reason_counts={lease_expired_after_io=1}
        calls[#calls+1]={"ZADD",ctx.keys.run_delayed,changes.not_before_ms,id}
        ok,code=CJ.Run.set(delta,{last_execution_at_ms=ctx.now_text},unit); if not ok then return {"caller_time",code} end
    end
    local post; post,code=CJ.Job.outcome_record(ctx,job,changes); if not post then return {"caller_record",code} end
    for field,value in pairs(effects.job_fields) do if post.v[field]~=value then return {"caller_pointer_mismatch",field} end end
    ok,code=CJ.Run.hset(plan,job.key,post.v,unit); if not ok then return {"caller_job",code} end
    for _,argv in ipairs(calls) do ok,code=CJ.Plan.add(plan,argv,unit); if not ok then return {"caller_index",code} end end
    ok,code=CJ.Run.accumulate(delta,{run=counters,maps=maps,indexes=indexes},unit); if not ok then return {"caller_delta",code} end
    last=effects
end
local post; post,code=CJ.Run.flush(ctx,plan,delta); if not post then return {"caller_flush",code} end
local assessed; assessed,code=CJ.Plan.assess(ctx,plan); if not assessed then return {"caller_assess",code} end
return {post.v.pending_request_reservations,post.v.started_request_reservations,post.v.request_starts,post.v.reservation_creations_total,
    post.maps.group_pending.v.default,post.maps.group_active_started.v.other,post.maps.group_open_jobs.v.default,post.maps.group_open_jobs.v.other,
    last.scopes.global.v.active_count,last.scopes.group.v.pending_count,last.scopes.origin.v.started_count,
    last.record.v.state,last.tombstone_expires_at_ms,CJ.P.format_decimal(assessed.growth)}
`
	before := f.r.snapshot()
	got := sharedLuaNoError(t, workerLuaRun(t, f.r, source, keys, args)).([]any)
	want := []any{"0", "0", "2", "3", "0", "0", "2", "0", "0", "0", "0", "expired", canonicalDecimal(f.r.now + 86400000)}
	if len(got) != len(want)+1 || !reflect.DeepEqual(got[:len(want)], want) || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 || f.r.aclCount != 0 {
		t.Fatalf("shared terminal effects lost a contribution or executed: %v", got)
	}
	growth, err := strconv.ParseUint(got[len(want)].(string), 10, 64)
	if err != nil || growth == 0 {
		t.Fatal("request effects did not reach the actual core allocation simulation")
	}
}

func TestWorkerLuaRenewalInspectionExpiredDataAndCorruption(t *testing.T) {
	t.Parallel()
	t.Run("expired_metadata_is_inspection_only", func(t *testing.T) {
		f, i, commit, expires := workerLuaStageFixture(t)
		for _, key := range wireOracleStageKeys(commit) {
			f.r.removeKey(key) // simulate natural common absolute key expiry
		}
		f.r.now = expires + 1
		base := requestLuaPrefix + "run:" + string(f.runID)
		beforeJob := workerLuaHashRecord(t, f.r, base+":job:"+string(i.Lease.JobID), SchemaJob)
		beforeSlot := f.r.data[StageSlotsKey].hash[string(commit)]
		beforeExpiry := f.r.zsets[StageExpiryKey][string(commit)]
		activity := f.r.data[base].hash["last_activity_at_ms"]
		workerLuaReply(t, f, OperationRenewLease, i, StatusLeaseLost)
		if f.r.data[base].hash["renewal_rejections_total"] != "1" || f.r.data[base].hash["last_activity_at_ms"] != activity ||
			!reflect.DeepEqual(beforeJob, workerLuaHashRecord(t, f.r, base+":job:"+string(i.Lease.JobID), SchemaJob)) ||
			f.r.data[StageSlotsKey].hash[string(commit)] != beforeSlot || f.r.zsets[StageExpiryKey][string(commit)] != beforeExpiry {
			t.Fatal("expired-stage inspection renewed, spent a slot or changed activity")
		}
		f.r.maximum = f.r.used + 50000000 + 67108864 + 1024*1024
		workerLuaReject(t, f, OperationRenewLease, i, ErrorMemoryHeadroomLow)
	})
	t.Run("bad_expired_stage_does_not_count", func(t *testing.T) {
		f, i, commit, expires := workerLuaStageFixture(t)
		f.r.now = expires - 10000
		f.r.data[requestLuaPrefix+"stage:"+string(commit)+":meta"].hash["token_digest"] = strings.Repeat("d", 64)
		before := f.r.snapshot()
		result := workerLuaCall(t, f, OperationRenewLease, i)
		if result.runtimeErr != nil {
			t.Fatal(result.runtimeErr)
		}
		if _, ok := result.raw.(bootLuaErrorReply); !ok || !reflect.DeepEqual(before, f.r.snapshot()) {
			t.Fatal("corrupted expired stage counted a definitive rejection")
		}
	})
	t.Run("expired_aborted_never_reopens_or_counts", func(t *testing.T) {
		f, i, commit, expires := workerLuaStageFixture(t)
		for _, key := range wireOracleStageKeys(commit) {
			f.r.removeKey(key)
		}
		f.r.removeKey(StageExpiryKey)
		f.r.data[StageSlotsKey].hash[string(commit)] = "50000000:" + string(f.runID) + ":" + string(i.Lease.JobID) + ":1:2"
		id, err := DeriveAbortStageTransitionID(AbortStageTransitionInput{Lease: i.Lease, CommitID: commit})
		if err != nil {
			t.Fatal(err)
		}
		job := f.r.data[requestLuaPrefix+"run:"+string(f.runID)+":job:"+string(i.Lease.JobID)].hash
		job["active_stage_commit_id"], job["last_transition_id"], job["last_transition_status"] = "", string(id), "STAGE_ABORTED"
		f.r.now = expires - 10000
		workerLuaReject(t, f, OperationRenewLease, i, ErrorInvalidState)
	})
}
