package crawljobsv2

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

var jobLuaOutcomeOperations = []OperationName{OperationRejectReady, OperationReleaseBeforeIO, OperationRetry,
	OperationDead, OperationCancelJob, OperationCompleteNoOutput}

func jobLuaOutcomeWire(t *testing.T, a gateArtifacts, authority RunPolicyAuthority, source SourceJob, lease LeaseIdentity,
	operation OperationName, reason Reason) OperationWireRequest {
	t.Helper()
	gate, err := NewTransportGate(operation, activeGateInput(a))
	if err != nil {
		t.Fatal(err)
	}
	var request OperationWireRequest
	switch operation {
	case OperationRejectReady:
		request, err = NewRejectReadyWireRequest(gate, authority, RejectReadyTransitionInput{RunID: lease.RunID, Job: source, Reason: reason})
	case OperationReleaseBeforeIO:
		request, err = NewReleaseBeforeIOWireRequest(gate, ReleaseBeforeIOTransitionInput{Lease: lease})
	case OperationRetry:
		request, err = NewRetryWireRequest(gate, RetryTransitionInput{Lease: lease, Reason: reason})
	case OperationDead:
		request, err = NewDeadWireRequest(gate, DeadTransitionInput{Lease: lease, Reason: reason})
	case OperationCancelJob:
		request, err = NewCancelJobWireRequest(gate, CancelJobTransitionInput{Lease: lease, Reason: reason})
	case OperationCompleteNoOutput:
		request, err = NewCompleteNoOutputWireRequest(gate, CompleteNoOutputTransitionInput{Lease: lease, Reason: reason})
	default:
		t.Fatal("unknown outcome operation")
	}
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestJobLuaOutcomeIdentitiesAndReasonMatrix(t *testing.T) {
	t.Parallel()
	vm := jobLuaNew(t)
	a := newGateArtifacts(t)
	source := jobLuaSourceValue(t, "https://example.com/path")
	groups := []PolicyGroup{jobLuaGroup(source)}
	ledger, _ := jobLuaBinding(t, vm, groups)
	authority := newAuthenticatedTestRunPolicyAuthority(t, RunID(strings.Repeat("1", 32)),
		Digest(strings.Repeat("a", 64)), Digest(strings.Repeat("b", 64)), groups)
	lease := LeaseIdentity{RunID: RunID(strings.Repeat("1", 32)), JobID: source.JobID,
		OwnerID: OwnerID(strings.Repeat("0", 32)), Token: LeaseToken(strings.Repeat("0", 64)), Fence: 3}
	for _, operation := range jobLuaOutcomeOperations {
		for reason := range reasons {
			t.Run(string(operation)+"/"+string(reason), func(t *testing.T) {
				want := ValidateTransitionReason(operation, reason) == nil
				valid, code := vm.invoke(t, "Job", "outcome_reason", lua.LString(operation), lua.LString(reason))
				if code != lua.LNil || valid != lua.LBool(want) {
					t.Fatalf("reason matrix Go=%t Lua=%v/%v", want, valid, code)
				}
				if !want {
					return
				}
				request := jobLuaOutcomeWire(t, a, authority, source, lease, operation, reason)
				values := jobLuaValues(vm, request.semantic)
				derived, code := vm.invoke(t, "Job", "outcome_identity", lua.LString(operation), values, ledger)
				if code != lua.LNil || derived != values.RawGetString("transition_id") {
					t.Fatalf("transition differs from Go authority: %v/%v", derived, code)
				}
				for _, field := range []string{"run_id", "job_id", "owner_id", "lease_token", "fence"} {
					if operation == OperationRejectReady && (field == "owner_id" || field == "lease_token" || field == "fence") {
						continue
					}
					changed := jobLuaValues(vm, request.semantic)
					replacement := strings.Repeat("e", 64)
					if field == "run_id" || field == "owner_id" {
						replacement = strings.Repeat("e", 32)
					} else if field == "fence" {
						replacement = "4"
					}
					changed.RawSetString(field, lua.LString(replacement))
					other, _ := vm.invoke(t, "Job", "outcome_identity", lua.LString(operation), changed, ledger)
					if other == derived {
						t.Fatalf("identity did not bind %s", field)
					}
				}
			})
		}
	}
}

type jobLuaOutcomeFixture struct {
	*recordsLuaFixture
	job   Record
	lease LeaseIdentity
}

func jobLuaOutcomeFixtureNew(t *testing.T, deliveries int) *jobLuaOutcomeFixture {
	t.Helper()
	f := recordsLuaNew(t, false, 1, 1, 1)
	source := f.jobs[0]
	job := recordsLuaInitial(t, source, f.r.now-1000)
	lease := LeaseIdentity{RunID: f.input.RunID, JobID: source.JobID, OwnerID: OwnerID(strings.Repeat("3", 32)),
		Token: LeaseToken(strings.Repeat("4", 64)), Fence: Fence(max(1, deliveries))}
	v := f.r.data[runLuaKey("")].hash
	for key, value := range map[string]string{"state": "active", "job_count": "1", "open_job_count": "1", "audit_revision": "1",
		"audit_count": "1", "audit_cursor": string(source.JobID), "audit_complete": "1", "sealed_at_ms": canonicalDecimal(f.r.now - 900),
		"activated_at_ms": canonicalDecimal(f.r.now - 800), "last_activity_at_ms": canonicalDecimal(f.r.now - 100)} {
		v[key] = value
	}
	f.r.data[runLuaKey("group_open_jobs")].hash[string(source.GroupID)] = "1"
	f.r.setHash(runLuaKey("audit_group_counts"), Record{textField(string(source.GroupID), "1")})
	f.r.setSet(runLuaKey("jobs"), []string{string(source.JobID)})
	f.r.setZSet(runLuaKey("job_order"), map[string]float64{string(source.JobID): 0})
	if deliveries < 0 {
		score, _ := source.ScoreText.Float64()
		f.r.setZSet(runLuaKey("ready"), map[string]float64{string(source.JobID): score})
		f.r.setZSet(runLuaKey("ready_at"), map[string]float64{string(source.JobID): float64(f.r.now - 1000)})
	} else {
		fence := canonicalDecimal(uint64(lease.Fence))
		for index, value := range map[int]string{jobStateIndex: "leased", jobClaimCountIndex: fence, jobLeaseFenceIndex: fence,
			jobLeaseOwnerIndex: string(lease.OwnerID), jobLeaseTokenIndex: string(lease.Token), jobLeaseStartedAtMSIndex: canonicalDecimal(f.r.now - 300),
			jobLeaseExpiresAtMSIndex: canonicalDecimal(f.r.now + 30000), jobNextRequestOrdinalIndex: canonicalDecimal(uint64(lease.Fence) + 1),
			jobDeliveryAttemptsIndex: strconv.Itoa(deliveries), jobRequestStartsIndex: strconv.Itoa(deliveries),
			jobLeaseRequestStartsBaselineIndex: strconv.Itoa(max(0, deliveries-1)), jobRetryCountIndex: strconv.Itoa(max(0, deliveries-1)),
			jobUpdatedAtMSIndex: canonicalDecimal(f.r.now - 100)} {
			recordAuthoritySet(job, index, value)
		}
		v["claims_total"], v["reservation_creations_total"] = fence, fence
		v["last_execution_at_ms"] = canonicalDecimal(f.r.now - 300)
		v["request_starts"], v["retries_total"] = strconv.Itoa(deliveries), strconv.Itoa(max(0, deliveries-1))
		f.r.data[runLuaKey("group_started")].hash[string(source.GroupID)] = strconv.Itoa(deliveries)
		f.r.data[runLuaKey("retry_reason_counts")].hash["request_timeout"] = strconv.Itoa(max(0, deliveries-1))
		if deliveries > 0 {
			recordAuthoritySet(job, jobLeaseDeliveryStartedIndex, "1")
			recordAuthoritySet(job, jobLastRequestStartedAtMSIndex, canonicalDecimal(f.r.now-200))
			v["last_request_started_at_ms"], v["last_execution_at_ms"] = canonicalDecimal(f.r.now-200), canonicalDecimal(f.r.now-200)
		}
		f.r.setZSet(runLuaKey("leased"), map[string]float64{string(source.JobID): float64(f.r.now + 30000)})
		f.r.setZSet(runLuaKey("leased_at"), map[string]float64{string(source.JobID): float64(f.r.now - 300)})
		f.r.setZSet("mifolyo:crawl:v2:active_leases", map[string]float64{string(f.input.RunID) + ":" + string(source.JobID): float64(f.r.now + 30000)})
	}
	if err := ValidateRecord(SchemaJob, job); err != nil {
		t.Fatal(err)
	}
	f.r.setHash(wireOracleRunJobKey(lease.RunID, lease.JobID), job)
	runLuaRecord(t, f.r)
	return &jobLuaOutcomeFixture{f, job, lease}
}

func TestJobLuaPlanInsertCoalesced(t *testing.T) {
	t.Parallel()
	f := recordsLuaNew(t, false, 2, 2, 1)
	keys, args := f.wire(t, OperationEnqueueBatch, f.jobs, "", 0)
	script := recordsLuaCore(t) + `
local registered,code=CJ.Reply.register("CJ2_ENQUEUE_BATCH",function(ctx,status,tail)
    if status~="OK" or #tail~=4 or tail[1]~="2" or tail[2]~="0" or tail[3]~="2" or tail[4]~="2" then return nil,"INVALID_ARGUMENT" end
    return true
end)
if not registered then return CJ.Context.reject(code) end
local function prepare()
    local ctx,code=CJ.Context.open(CJ.Wire.run_spec("CJ2_ENQUEUE_BATCH",{"run_id","record_count"}),KEYS,ARGV)
    if not ctx then return nil,code end
    local gate,code=CJ.Gate.check(ctx); if not gate then return nil,code end
    local run,code=CJ.Run.load(ctx,ctx.request.v.run_id); if not run then return nil,code end
    local sources,code=CJ.Job.sources(ctx.request.records,run); if not sources then return nil,code end
    local plan,code=CJ.Plan.new(ctx); if not plan then return nil,code end
    local delta,code=CJ.Run.plan_delta(ctx,run); if not delta then return nil,code end
    for i,source in ipairs(sources) do
        local job,code=CJ.Job.load(ctx,run,source.v.job_id); if not job then return nil,code end
        local inserted,code=CJ.Job.plan_insert(ctx,plan,delta,run,source,i==1 and "ordinary" or nil); if not inserted then return nil,code end
    end
    local ok,code=CJ.Run.accumulate(delta,{run={load_revision=1}}); if not ok then return nil,code end
    local post,code=CJ.Run.flush(ctx,plan,delta); if not post then return nil,code end
    return CJ.Run.finish(ctx,plan,"OK",{"2","0",post.v.job_count,post.v.load_revision})
end
local execution,code=prepare(); if not execution then return CJ.Context.reject(code) end
for i=1,execution.count do redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc)) end
return execution.reply`
	result := recordsLuaRun(t, f.r, script, keys, args)
	raw := sharedLuaNoError(t, result)
	if err := ValidateOperationResponse(OperationEnqueueBatch, raw); err != nil {
		t.Fatal(err)
	}
	for _, source := range f.jobs {
		expected := recordsLuaInitial(t, source, f.r.now)
		actual := f.r.data[wireOracleRunJobKey(f.input.RunID, source.JobID)].hash
		for _, field := range expected {
			if actual[field.Name] != string(field.Value) {
				t.Fatalf("new job field %s differs", field.Name)
			}
		}
	}
	runLuaRecord(t, f.r)
	counts := map[string]int{}
	for _, call := range f.r.trace {
		if !call.acl && call.name == "HSET" {
			counts[call.args[0]]++
		}
	}
	if counts[runLuaKey("")] != 1 || counts[runLuaKey("group_open_jobs")] != 1 || f.r.data[runLuaKey("group_open_jobs")].hash[string(f.jobs[0].GroupID)] != "2" {
		t.Fatal("insert effects were not coalesced into one Run delta")
	}
	sharedLuaAssertTrace(t, f.r, f.r.attempts)
	if !f.r.returnedPrebuilt {
		t.Fatal("response was not prebuilt")
	}
	before := f.r.snapshot()
	// Repeating the effect-only insert is rejected, not a second counter charge.
	result = recordsLuaRun(t, f.r, script, keys, args)
	if result.runtimeErr != nil || result.raw != bootLuaErrorReply("ERR CRAWL_V2_INVALID_STATE") || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatalf("duplicate insert: %v/%v", result.raw, result.runtimeErr)
	}
}

func TestJobLuaOutcomeFixtureRecords(t *testing.T) {
	t.Parallel()
	for _, attempts := range []int{-1, 0, 1, 2, 3} {
		t.Run(fmt.Sprint(attempts), func(t *testing.T) { jobLuaOutcomeFixtureNew(t, attempts) })
	}
}

func jobLuaOutcomeSource(t *testing.T, operation OperationName) string {
	t.Helper()
	source := recordsLuaCore(t)
	for _, module := range []struct{ name, file string }{{"Request", "ledger_request"}, {"StageOutput", "stage_output"}, {"Stage", "ledger_stage"}} {
		source += "CJ." + module.name + "=(function()\n" + string(primitiveLuaRead(t, "lua_src/"+module.file+".lua")) + "\nend)()\n"
	}
	return source + string(primitiveLuaRead(t, "lua_src/ops/"+strings.ToLower(string(operation))+".lua"))
}

func jobLuaOutcomeExecute(t *testing.T, f *jobLuaOutcomeFixture, operation OperationName, reason Reason, mutate func([]string, []string)) bootLuaResult {
	t.Helper()
	request := jobLuaOutcomeWire(t, f.a, f.authority, f.jobs[0], f.lease, operation, reason)
	keys, args := runLuaParts(t, request, nil)
	if len(keys) != 44 {
		t.Fatalf("WORK key count = %d, want 44", len(keys))
	}
	if mutate != nil {
		mutate(keys, args)
	}
	return recordsLuaRun(t, f.r, jobLuaOutcomeSource(t, operation), keys, args)
}

func jobLuaOutcomeRead(t *testing.T, f *jobLuaOutcomeFixture) Record {
	t.Helper()
	names, err := RecordSchemaFields(SchemaJob)
	if err != nil {
		t.Fatal(err)
	}
	values := f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash
	if len(values) != 54 {
		t.Fatal("Job post-state is not exactly 54 fields")
	}
	record := make(Record, len(names))
	for i, name := range names {
		value, found := values[name]
		if !found {
			t.Fatalf("missing field %s", name)
		}
		record[i] = textField(name, value)
	}
	if err := ValidateRecord(SchemaJob, record); err != nil {
		t.Fatalf("Go rejected actual Job post-state: %v", err)
	}
	runLuaRecord(t, f.r)
	return record
}

func jobLuaOutcomeReply(t *testing.T, f *jobLuaOutcomeFixture, operation OperationName, reason Reason, status Status, tail ...string) Record {
	t.Helper()
	raw := sharedLuaNoError(t, jobLuaOutcomeExecute(t, f, operation, reason, nil))
	if err := ValidateOperationResponse(operation, raw); err != nil {
		t.Fatalf("Go response oracle: %v (%v)", err, raw)
	}
	want := []any{string(status), canonicalDecimal(f.r.now)}
	for _, v := range tail {
		want = append(want, v)
	}
	if !reflect.DeepEqual(raw, want) {
		t.Fatalf("reply = %v, want %v", raw, want)
	}
	sharedLuaAssertTrace(t, f.r, f.r.attempts)
	if f.r.attempts > 0 && !f.r.returnedPrebuilt {
		t.Fatal("response was not prebuilt before execution")
	}
	return jobLuaOutcomeRead(t, f)
}

func jobLuaOutcomeReject(t *testing.T, f *jobLuaOutcomeFixture, op OperationName, reason Reason, expected ErrorCode, mutate func([]string, []string)) {
	t.Helper()
	before := f.r.snapshot()
	result := jobLuaOutcomeExecute(t, f, op, reason, mutate)
	if result.runtimeErr != nil {
		t.Fatal(result.runtimeErr)
	}
	if result.raw != bootLuaErrorReply("ERR CRAWL_V2_"+string(expected)) {
		t.Fatalf("rejection = %v, want %s", result.raw, string(expected))
	}
	if f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("rejection mutated state")
	}
	sharedLuaAssertTrace(t, f.r, 0)
}

func jobLuaOutcomeCancelRun(f *jobLuaOutcomeFixture, reason Reason) {
	v := f.r.data[runLuaKey("")].hash
	if reason == ReasonAuthorizationExpired {
		v["authorization_expires_at_ms"] = canonicalDecimal(f.r.now)
	} else {
		v["state"], v["terminal_reason"] = "cancelled", string(reason)
		v["cancelled_at_ms"], v["last_activity_at_ms"] = canonicalDecimal(f.r.now-50), canonicalDecimal(f.r.now-50)
	}
}

func TestJobLuaOutcomeHandlersBranches(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		operation OperationName
		reason    Reason
		attempts  int
		status    Status
		state     string
	}{
		{OperationRejectReady, ReasonPolicyDenied, -1, StatusDead, "dead"},
		{OperationReleaseBeforeIO, ReasonNone, 0, StatusReleasedReady, "ready"},
		{OperationRetry, ReasonRequestTimeout, 1, StatusRetryScheduled, "delayed"},
		{OperationRetry, ReasonHTTP5xx, 2, StatusRetryScheduled, "delayed"},
		{OperationRetry, ReasonDNSTemporary, 3, StatusDead, "dead"},
		{OperationDead, ReasonHTTP4xx, 1, StatusDead, "dead"},
		{OperationCancelJob, ReasonOperatorCancelled, 1, StatusCancelled, "cancelled"},
		{OperationCompleteNoOutput, ReasonAlreadyVisited, 1, StatusCompleted, "completed"},
	} {
		t.Run(string(test.operation)+"/"+fmt.Sprint(test.attempts), func(t *testing.T) {
			f := jobLuaOutcomeFixtureNew(t, test.attempts)
			if test.operation == OperationCancelJob {
				jobLuaOutcomeCancelRun(f, test.reason)
			}
			at := canonicalDecimal(f.r.now)
			tail := []string{at, string(test.reason)}
			if test.operation == OperationRejectReady {
				tail = []string{string(test.reason)}
			} else if test.operation == OperationReleaseBeforeIO {
				tail = []string{at}
			} else if test.operation == OperationRetry {
				if test.attempts == 3 {
					tail = []string{at, "retry_exhausted", string(test.reason)}
				} else {
					delay := uint64(30000)
					if test.attempts == 2 {
						delay = 120000
					}
					tail = []string{canonicalDecimal(f.r.now + delay), strconv.Itoa(test.attempts), string(test.reason)}
				}
			}
			post := jobLuaOutcomeReply(t, f, test.operation, test.reason, test.status, tail...)
			if string(post[jobStateIndex].Value) != test.state {
				t.Fatal("wrong primary state")
			}
			for _, index := range []int{jobClaimCountIndex, jobDeliveryAttemptsIndex, jobRequestStartsIndex, jobLeaseRequestStartsBaselineIndex,
				jobNextRequestOrdinalIndex, jobLastRequestStartedAtMSIndex, jobLastDocumentRequestStartedAtMSIndex, jobLastDocumentRequestFenceIndex,
				jobLastDocumentTargetURLIDIndex, jobLastDocumentTargetURLIndex, jobLastDocumentTargetDigestIndex, jobLeaseFenceIndex,
				jobLastStageCommitIDIndex, jobLastStageFenceIndex, jobPreIORecoveriesIndex} {
				if string(post[index].Value) != string(f.job[index].Value) {
					t.Fatalf("retained history changed: %s", post[index].Name)
				}
			}
			id, base := string(f.lease.JobID), runLuaKey("")
			for _, name := range []string{"ready", "leased", "delayed", "completed", "dead", "cancelled"} {
				_, found := f.r.zsets[base+":"+name][id]
				if found != (test.state == name) {
					t.Fatalf("incorrect %s membership", name)
				}
			}
			if _, found := f.r.zsets[base+":leased_at"][id]; found {
				t.Fatal("lease age membership retained")
			}
			if _, found := f.r.zsets["mifolyo:crawl:v2:active_leases"][string(f.lease.RunID)+":"+id]; found {
				t.Fatal("global lease retained")
			}
			// Replay remains the historical result even after expiry/cancellation,
			// including a delayed response whose deadline is now in the past.
			f.r.now += 200000
			jobLuaOutcomeCancelRun(f, ReasonSourceCancelled)
			before := f.r.snapshot()
			jobLuaOutcomeReply(t, f, test.operation, test.reason, test.status, tail...)
			if f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("exact replay mutated state")
			}
		})
	}
}

func TestJobLuaOutcomeCancellationOverrides(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationRetry, OperationDead, OperationCompleteNoOutput, OperationCancelJob} {
		for _, reason := range []Reason{ReasonOperatorCancelled, ReasonSourceCancelled, ReasonAuthorizationExpired} {
			t.Run(string(op)+"/"+string(reason), func(t *testing.T) {
				f := jobLuaOutcomeFixtureNew(t, 1)
				jobLuaOutcomeCancelRun(f, reason)
				submitted := ReasonRequestTimeout
				if op == OperationDead {
					submitted = ReasonHTTP4xx
				} else if op == OperationCompleteNoOutput {
					submitted = ReasonAlreadyVisited
				} else if op == OperationCancelJob {
					submitted = reason
				}
				beforeFailure := "http_5xx"
				f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash["last_failure_reason"] = beforeFailure
				post := jobLuaOutcomeReply(t, f, op, submitted, StatusCancelled, canonicalDecimal(f.r.now), string(reason))
				if string(post[jobLastFailureReasonIndex].Value) != beforeFailure {
					t.Fatal("cancellation erased underlying failure evidence")
				}
				request := jobLuaOutcomeWire(t, f.a, f.authority, f.jobs[0], f.lease, op, submitted)
				if string(post[jobLastTransitionIDIndex].Value) != string(request.semantic[len(request.semantic)-1].Value) {
					t.Fatal("authoritative cancellation relabelled the requested identity")
				}
			})
		}
	}
	for _, op := range []OperationName{OperationRejectReady, OperationReleaseBeforeIO} {
		for _, expired := range []bool{false, true} {
			t.Run(string(op)+"/definitive/"+fmt.Sprint(expired), func(t *testing.T) {
				attempts, reason := 0, ReasonNone
				if op == OperationRejectReady {
					attempts, reason = -1, ReasonPolicyDenied
				}
				f := jobLuaOutcomeFixtureNew(t, attempts)
				cancel, status := ReasonOperatorCancelled, StatusRunCancelled
				if expired {
					cancel, status = ReasonAuthorizationExpired, StatusAuthorizationExpired
				}
				jobLuaOutcomeCancelRun(f, cancel)
				before := f.r.snapshot()
				jobLuaOutcomeReply(t, f, op, reason, status)
				if !reflect.DeepEqual(before, f.r.snapshot()) {
					t.Fatal("definitive pre-outcome status mutated state")
				}
			})
		}
	}
}

func TestJobLuaOutcomePreconditionsIdentityAndFailures(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationReleaseBeforeIO, OperationRetry, OperationDead, OperationCancelJob, OperationCompleteNoOutput} {
		t.Run(string(op), func(t *testing.T) {
			reason, attempts := ReasonHTTP4xx, 1
			switch op {
			case OperationReleaseBeforeIO:
				reason, attempts = ReasonNone, 0
			case OperationRetry:
				reason = ReasonHTTP429
			case OperationCancelJob:
				reason = ReasonOperatorCancelled
			case OperationCompleteNoOutput:
				reason = ReasonAlreadyVisited
			}
			for _, field := range []string{"owner_id", "lease_token", "fence"} {
				f := jobLuaOutcomeFixtureNew(t, attempts)
				if op == OperationCancelJob {
					jobLuaOutcomeCancelRun(f, reason)
				}
				before := f.r.snapshot()
				if field == "owner_id" {
					f.lease.OwnerID = OwnerID(strings.Repeat("e", 32))
				} else if field == "lease_token" {
					f.lease.Token = LeaseToken(strings.Repeat("e", 64))
				} else {
					f.lease.Fence++
				}
				jobLuaOutcomeReply(t, f, op, reason, StatusLeaseLost, string(f.job[jobLeaseFenceIndex].Value))
				if !reflect.DeepEqual(before, f.r.snapshot()) {
					t.Fatal("wrong identity mutated state")
				}
			}
			f := jobLuaOutcomeFixtureNew(t, attempts)
			jobLuaOutcomeReject(t, f, op, reason, ErrorImmutableMismatch, func(_, args []string) { args[len(args)-1] = strings.Repeat("e", 64) })
			f = jobLuaOutcomeFixtureNew(t, attempts)
			jobLuaOutcomeReject(t, f, op, reason, ErrorInvalidArgument, func(keys, _ []string) { keys[len(keys)-1] = "unrelated:job" })
		})
	}
	f := jobLuaOutcomeFixtureNew(t, 0)
	jobLuaOutcomeReject(t, f, OperationRetry, ReasonRequestTimeout, ErrorInvalidState, nil)
	f = jobLuaOutcomeFixtureNew(t, 1)
	jobLuaOutcomeReject(t, f, OperationReleaseBeforeIO, ReasonNone, ErrorInvalidState, nil)
	f = jobLuaOutcomeFixtureNew(t, 1)
	jobLuaOutcomeReject(t, f, OperationCancelJob, ReasonOperatorCancelled, ErrorInvalidArgument, nil)
}

func TestJobLuaOutcomePrewriteAndPartialFailure(t *testing.T) {
	t.Parallel()
	control := jobLuaOutcomeFixtureNew(t, 1)
	jobLuaOutcomeReply(t, control, OperationDead, ReasonHTTP4xx, StatusDead, canonicalDecimal(control.r.now), string(ReasonHTTP4xx))
	calls := control.r.attempts
	if calls < 2 {
		t.Fatal("outcome unexpectedly has no meaningful write sequence")
	}
	f := jobLuaOutcomeFixtureNew(t, 1)
	f.r.denyAt = calls
	jobLuaOutcomeReject(t, f, OperationDead, ReasonHTTP4xx, ErrorBootUnapproved, nil)
	f = jobLuaOutcomeFixtureNew(t, 1)
	f.r.used = f.r.maximum - CommitMemoryReservationBytes
	jobLuaOutcomeReject(t, f, OperationDead, ReasonHTTP4xx, ErrorMemoryHeadroomLow, nil)
	f = jobLuaOutcomeFixtureNew(t, 1)
	f.r.failAt, f.r.failAfter = 2, true
	before := f.r.snapshot()
	result := jobLuaOutcomeExecute(t, f, OperationDead, ReasonHTTP4xx, nil)
	if result.runtimeErr == nil || !strings.Contains(result.runtimeErr.Error(), bootLuaWriteErr) || f.r.writes != 2 || reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("post-write fault was concealed or described as rollback", result.runtimeErr)
	}
	if f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash["state"] != "dead" {
		t.Fatal("earlier successful Job write did not persist")
	}
}

func TestJobLuaOutcomeEveryCallerReason(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationRejectReady, OperationDead, OperationRetry} {
		for reason := range reasons {
			if ValidateTransitionReason(op, reason) != nil {
				continue
			}
			t.Run(string(op)+"/"+string(reason), func(t *testing.T) {
				attempts := 1
				if op == OperationRejectReady {
					attempts = -1
				}
				f := jobLuaOutcomeFixtureNew(t, attempts)
				if reason == ReasonReservationLimitExhausted {
					f.r.data[runLuaKey("")].hash["reservation_creations_total"] = "100"
					f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash["next_request_ordinal"] = "101"
				}
				if op == OperationRejectReady {
					jobLuaOutcomeReply(t, f, op, reason, StatusDead, string(reason))
				} else if op == OperationDead {
					jobLuaOutcomeReply(t, f, op, reason, StatusDead, canonicalDecimal(f.r.now), string(reason))
				} else {
					jobLuaOutcomeReply(t, f, op, reason, StatusRetryScheduled, canonicalDecimal(f.r.now+30000), "1", string(reason))
				}
			})
		}
	}
}

func TestJobLuaOutcomeReservationLimitPredicates(t *testing.T) {
	t.Parallel()
	f := jobLuaOutcomeFixtureNew(t, 1)
	jobLuaOutcomeReject(t, f, OperationDead, ReasonReservationLimitExhausted, ErrorInvalidArgument, nil)
	f = jobLuaOutcomeFixtureNew(t, 0)
	f.r.data[runLuaKey("")].hash["reservation_creations_total"] = "100"
	jobLuaOutcomeReject(t, f, OperationDead, ReasonReservationLimitExhausted, ErrorInvalidArgument, nil)
	f = jobLuaOutcomeFixtureNew(t, 1)
	f.r.data[runLuaKey("")].hash["reservation_creations_total"] = "100"
	f.r.data[runLuaKey("")].hash["request_starts"] = "10"
	f.r.data[runLuaKey("group_started")].hash[string(f.jobs[0].GroupID)] = "10"
	j := f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash
	j["request_starts"], j["next_request_ordinal"] = "10", "101"
	// Reserve's run-start budget rejection precedes its creation-limit branch.
	jobLuaOutcomeReject(t, f, OperationDead, ReasonReservationLimitExhausted, ErrorInvalidArgument, nil)
}

func TestJobLuaOutcomeReadyWitnessAndActiveWork(t *testing.T) {
	t.Parallel()
	f := jobLuaOutcomeFixtureNew(t, -1)
	f.jobs[0].ScoreText = "1"
	jobLuaOutcomeReject(t, f, OperationRejectReady, ReasonPolicyScopeChanged, ErrorImmutableMismatch, nil)
	for _, op := range []OperationName{OperationRetry, OperationDead, OperationCancelJob, OperationCompleteNoOutput} {
		t.Run(string(op), func(t *testing.T) {
			f := jobLuaOutcomeFixtureNew(t, 0)
			reason := ReasonHTTP4xx
			switch op {
			case OperationRetry:
				reason = ReasonRequestTimeout
			case OperationCancelJob:
				reason = ReasonSourceCancelled
				jobLuaOutcomeCancelRun(f, reason)
			case OperationCompleteNoOutput:
				reason = ReasonAlreadyVisited
			}
			f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash["active_reservation_id"] = strings.Repeat("a", 64)
			jobLuaOutcomeReject(t, f, op, reason, ErrorInvalidState, nil)
		})
	}
	// A live stage is never implicitly aborted/deleted by a standard outcome.
	f = jobLuaOutcomeFixtureNew(t, 1)
	commit := strings.Repeat("6", 64)
	job := f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash
	job["active_stage_commit_id"], job["last_stage_commit_id"], job["last_stage_fence"] = commit, commit, "1"
	f.r.setHash(sharedLuaSlots, Record{textField(commit, "50331648:"+string(f.lease.RunID)+":"+string(f.lease.JobID)+":1:0")})
	jobLuaOutcomeReject(t, f, OperationDead, ReasonOutputInvalid, ErrorInvalidState, nil)
}

func TestJobLuaOutcomeSafetyFallback(t *testing.T) {
	t.Parallel()
	f := jobLuaOutcomeFixtureNew(t, 1)
	f.r.used = f.r.maximum - CommitMemoryReservationBytes - 32768
	jobLuaOutcomeReply(t, f, OperationDead, ReasonHTTP4xx, StatusDead, canonicalDecimal(f.r.now), string(ReasonHTTP4xx))
	f = jobLuaOutcomeFixtureNew(t, -1)
	f.r.used = f.r.maximum - CommitMemoryReservationBytes - 32768
	jobLuaOutcomeReject(t, f, OperationRejectReady, ReasonPolicyDenied, ErrorMemoryHeadroomLow, nil)
}

func jobLuaOutcomeAborted(t *testing.T, f *jobLuaOutcomeFixture, deadline uint64) string {
	t.Helper()
	commit := Digest(strings.Repeat("6", 64))
	transition, err := DeriveAbortStageTransitionID(AbortStageTransitionInput{Lease: f.lease, CommitID: commit})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := DeriveTargetDigest(RequestTarget{URLID: f.jobs[0].JobID, CanonicalURL: f.jobs[0].CanonicalURL})
	if err != nil {
		t.Fatal(err)
	}
	j := f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash
	for key, value := range map[string]string{"last_stage_commit_id": string(commit), "last_stage_fence": canonicalDecimal(uint64(f.lease.Fence)),
		"last_document_request_started_at_ms": j["last_request_started_at_ms"], "last_document_request_fence": j["lease_fence"],
		"last_document_target_url_id": string(f.jobs[0].JobID), "last_document_target_url": f.jobs[0].CanonicalURL, "last_document_target_digest": string(digest),
		"last_transition_id": string(transition), "last_transition_status": "STAGE_ABORTED", "commit_backpressure_reason": "memory_headroom_low",
		"commit_backpressure_fence": j["lease_fence"], "commit_backpressure_started_at_ms": canonicalDecimal(f.r.now - 100), "commit_backpressure_deadline_ms": canonicalDecimal(deadline)} {
		j[key] = value
	}
	f.r.setZSet(runLuaKey("commit_backpressure"), map[string]float64{string(f.jobs[0].JobID): float64(f.r.now - 100)})
	f.r.setHash(sharedLuaSlots, Record{textField(string(commit), "32768:"+string(f.lease.RunID)+":"+string(f.lease.JobID)+":"+canonicalDecimal(uint64(f.lease.Fence))+":3")})
	jobLuaOutcomeRead(t, f)
	return string(commit)
}

func TestJobLuaOutcomeAbortedStageCoverage(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationRetry, OperationDead, OperationCancelJob, OperationCompleteNoOutput} {
		t.Run(string(op), func(t *testing.T) {
			f := jobLuaOutcomeFixtureNew(t, 1)
			commit := jobLuaOutcomeAborted(t, f, f.r.now-1)
			reason, status := ReasonHTTP4xx, StatusDead
			tail := []string{canonicalDecimal(f.r.now), string(reason)}
			if op == OperationRetry {
				reason, status = ReasonDownstreamBackpressure, StatusRetryScheduled
				tail = []string{canonicalDecimal(f.r.now + 30000), "1", string(reason)}
			} else if op == OperationCancelJob {
				reason, status = ReasonOperatorCancelled, StatusCancelled
				jobLuaOutcomeCancelRun(f, reason)
				tail[1] = string(reason)
			} else if op == OperationCompleteNoOutput {
				reason, status = ReasonAlreadyVisited, StatusCompleted
				tail[1] = string(reason)
			}
			before := jobLuaOutcomeRead(t, f)
			post := jobLuaOutcomeReply(t, f, op, reason, status, tail...)
			for _, index := range []int{jobLeaseRequestStartsBaselineIndex, jobRequestStartsIndex, jobLeaseFenceIndex,
				jobLastDocumentRequestStartedAtMSIndex, jobLastDocumentRequestFenceIndex, jobLastDocumentTargetURLIDIndex,
				jobLastDocumentTargetURLIndex, jobLastDocumentTargetDigestIndex, jobLastStageCommitIDIndex, jobLastStageFenceIndex} {
				if string(before[index].Value) != string(post[index].Value) {
					t.Fatalf("abort outcome reset history %s", before[index].Name)
				}
			}
			if f.r.data[sharedLuaSlots].hash[commit] != "" {
				t.Fatal("aborted terminal slot not released")
			}
			var writes []bootLuaCommand
			for _, call := range f.r.trace {
				if !call.acl && sharedLuaIsWrite(call.name) {
					writes = append(writes, call)
				}
			}
			last := writes[len(writes)-1]
			if last.name != "HDEL" || !reflect.DeepEqual(last.args, []string{sharedLuaSlots, commit}) {
				t.Fatal("slot was not released after all covered writes", last)
			}
		})
	}
	f := jobLuaOutcomeFixtureNew(t, 1)
	jobLuaOutcomeAborted(t, f, f.r.now+1)
	jobLuaOutcomeReject(t, f, OperationRetry, ReasonDownstreamBackpressure, ErrorInvalidArgument, nil)
	f = jobLuaOutcomeFixtureNew(t, 1)
	commit := jobLuaOutcomeAborted(t, f, f.r.now-1)
	f.r.setHash("mifolyo:crawl:v2:stage:"+commit+":page", Record{textField("untrusted", "must-not-delete")})
	jobLuaOutcomeReject(t, f, OperationDead, ReasonOutputInvalid, ErrorInvalidState, nil)
}

func jobLuaOutcomePending(t *testing.T, f *jobLuaOutcomeFixture) ReservationID {
	t.Helper()
	s := f.jobs[0]
	intent := ReservationIntent{Lease: f.lease, RequestOrdinal: 1, Target: RequestTarget{URLID: s.JobID, CanonicalURL: s.CanonicalURL},
		CrawlPolicyDigest: f.input.CrawlPolicySHA256, Decision: s.Decision}
	id, err := DeriveReservationID(f.authority, intent)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := reservationIntentFields(f.authority, intent)
	if err != nil {
		t.Fatal(err)
	}
	v := map[string]string{"protocol_version": "2", "reservation_id": string(id), "run_id": string(f.lease.RunID), "job_id": string(f.lease.JobID),
		"owner_id": string(f.lease.OwnerID), "lease_token": string(f.lease.Token), "lease_fence": canonicalDecimal(uint64(f.lease.Fence)),
		"state": "pending", "created_at_ms": canonicalDecimal(f.r.now - 300), "started_at_ms": "0", "terminal_at_ms": "0",
		"delivery_attempts_after_start": "0", "job_starts_after_start": "0", "run_starts_after_start": "0", "group_starts_after_start": "0",
		"expires_at_ms": canonicalDecimal(f.r.now + 30000)}
	for _, field := range fields {
		v[field.Name] = string(field.Value)
	}
	record := recordAuthorityReservationRecord(t, "pending")
	for i := range record {
		value, found := v[record[i].Name]
		if !found {
			t.Fatalf("missing reservation fixture field %s", record[i].Name)
		}
		record[i].Value = []byte(value)
	}
	if err := ValidateRecord(SchemaReservation, record); err != nil {
		t.Fatal(err)
	}
	f.r.setHash("mifolyo:crawl:v2:reservation:"+string(id), record)
	f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash["active_reservation_id"] = string(id)
	f.r.data[runLuaKey("")].hash["pending_request_reservations"] = "1"
	f.r.data[runLuaKey("group_pending")].hash[string(s.GroupID)] = "1"
	origin, err := DeriveCanonicalOrigin(s.CanonicalURL)
	if err != nil {
		t.Fatal(err)
	}
	inventory := map[string]float64{}
	for _, scope := range []struct {
		id          Digest
		kind, value string
		concurrency uint64
		interval    uint64
	}{
		{s.Decision.GlobalScopeID, "global", "global", s.Decision.GlobalConcurrency, s.Decision.GlobalIntervalMS},
		{s.Decision.GroupScopeID, "group", string(s.RateScopeID), s.Decision.GroupConcurrency, s.Decision.GroupIntervalMS},
		{s.Decision.OriginScopeID, "origin", string(origin), s.Decision.OriginConcurrency, s.Decision.OriginIntervalMS},
	} {
		r := requestLuaRateRecord(t, scope.kind, scope.value)
		for index, value := range map[int]string{rateScopeEffectiveConcurrencyIndex: canonicalDecimal(scope.concurrency),
			rateScopeEffectiveIntervalMSIndex: canonicalDecimal(scope.interval), rateScopeNextAllowedMSIndex: "0", rateScopeLastStartedAtMSIndex: "0",
			rateScopeActiveCountIndex: "1", rateScopePendingCountIndex: "1", rateScopeStartedCountIndex: "0",
			rateScopeConcurrencySourceSHA256Index: string(f.input.CrawlPolicySHA256), rateScopeIntervalSourceSHA256Index: string(f.input.CrawlPolicySHA256),
			rateScopeUpdatedAtMSIndex: canonicalDecimal(f.r.now - 300)} {
			recordAuthoritySet(r, index, value)
		}
		if err := ValidateRecord(SchemaRateScope, r); err != nil {
			t.Fatal(err)
		}
		key := "mifolyo:crawl:v2:rate:" + string(scope.id)
		f.r.setHash(key, r)
		for _, suffix := range []string{"active", "pending"} {
			f.r.setZSet(key+":"+suffix, map[string]float64{string(id): float64(f.r.now + 30000)})
		}
		inventory[string(scope.id)] = float64(f.r.now - 300)
	}
	f.r.setZSet("mifolyo:crawl:v2:rate_scopes", inventory)
	return id
}

func TestJobLuaOutcomeReleasePendingCapacity(t *testing.T) {
	t.Parallel()
	f := jobLuaOutcomeFixtureNew(t, 0)
	id := jobLuaOutcomePending(t, f)
	jobLuaOutcomeReply(t, f, OperationReleaseBeforeIO, ReasonNone, StatusReleasedReady, canonicalDecimal(f.r.now))
	run := f.r.data[runLuaKey("")].hash
	if run["pending_request_reservations"] != "0" || run["reservation_creations_total"] != "1" || run["request_starts"] != "0" || run["retries_total"] != "0" {
		t.Fatal("release refunded a creation or consumed I/O/retry")
	}
	q := f.r.data["mifolyo:crawl:v2:reservation:"+string(id)]
	if q.hash["state"] != "cancelled" || q.hash["terminal_at_ms"] != canonicalDecimal(f.r.now) || q.expireAt != int64(f.r.now+86400000) {
		t.Fatal("pending request was not exactly tombstoned", q)
	}
	for _, scope := range []Digest{f.jobs[0].Decision.GlobalScopeID, f.jobs[0].Decision.GroupScopeID, f.jobs[0].Decision.OriginScopeID} {
		key := "mifolyo:crawl:v2:rate:" + string(scope)
		v := f.r.data[key].hash
		if v["active_count"] != "0" || v["pending_count"] != "0" || v["started_count"] != "0" || v["last_started_at_ms"] != "0" {
			t.Fatal("scope release changed request history or kept capacity")
		}
		for _, suffix := range []string{"active", "pending", "started"} {
			if _, exists := f.r.zsets[key+":"+suffix][string(id)]; exists {
				t.Fatal("released reservation remains indexed")
			}
		}
	}
	runWrites, jobWrites := 0, 0
	for _, call := range f.r.trace {
		if !call.acl && call.name == "HSET" && call.args[0] == runLuaKey("") {
			runWrites++
		}
		if !call.acl && call.name == "HSET" && call.args[0] == wireOracleRunJobKey(f.lease.RunID, f.lease.JobID) {
			jobWrites++
		}
	}
	if runWrites != 1 || jobWrites != 1 {
		t.Fatalf("Request/Job effects require one Run delta and one composed Job write: %d/%d", runWrites, jobWrites)
	}
	before := f.r.snapshot()
	f.r.now++
	jobLuaOutcomeReply(t, f, OperationReleaseBeforeIO, ReasonNone, StatusReleasedReady, canonicalDecimal(f.r.now-1))
	if f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("release replay refreshed the old reservation tombstone")
	}
}

func jobLuaOutcomePromotedHistory(t *testing.T) *jobLuaOutcomeFixture {
	t.Helper()
	f := jobLuaOutcomeFixtureNew(t, 1)
	jobLuaOutcomeAborted(t, f, f.r.now-1)
	transition, err := DeriveRetryTransitionID(RetryTransitionInput{Lease: f.lease, Reason: ReasonRequestTimeout})
	if err != nil {
		t.Fatal(err)
	}
	f.r.now += 40000
	j := f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash
	for key, value := range map[string]string{"state": "ready", "lease_owner": "", "lease_token": "", "lease_started_at_ms": "0",
		"lease_expires_at_ms": "0", "lease_delivery_started": "0", "commit_backpressure_fence": "0", "commit_backpressure_reason": "none",
		"commit_backpressure_started_at_ms": "0", "commit_backpressure_deadline_ms": "0", "retry_count": "1", "last_reason": "request_timeout",
		"last_failure_reason": "request_timeout", "last_transition_id": string(transition), "last_transition_status": "RETRY_SCHEDULED",
		"updated_at_ms": canonicalDecimal(f.r.now - 1)} {
		j[key] = value
	}
	v := f.r.data[runLuaKey("")].hash
	v["retries_total"], v["last_activity_at_ms"], v["last_execution_at_ms"] = "1", canonicalDecimal(f.r.now-1), canonicalDecimal(f.r.now-40000)
	f.r.data[runLuaKey("retry_reason_counts")].hash["request_timeout"] = "1"
	for _, key := range []string{runLuaKey("leased"), runLuaKey("leased_at"), runLuaKey("commit_backpressure"), sharedLuaSlots, "mifolyo:crawl:v2:active_leases"} {
		f.r.removeKey(key)
	}
	score, _ := f.jobs[0].ScoreText.Float64()
	f.r.setZSet(runLuaKey("ready"), map[string]float64{string(f.jobs[0].JobID): score})
	f.r.setZSet(runLuaKey("ready_at"), map[string]float64{string(f.jobs[0].JobID): float64(f.r.now - 1)})
	f.job = jobLuaOutcomeRead(t, f)
	return f
}

func TestJobLuaOutcomePromotedReplayAndReadyFreeze(t *testing.T) {
	t.Parallel()
	f := jobLuaOutcomePromotedHistory(t)
	before := f.r.snapshot()
	jobLuaOutcomeReply(t, f, OperationRetry, ReasonRequestTimeout, StatusLeaseLost, "1")
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("old retry replay rewrote a promoted job")
	}
	post := jobLuaOutcomeReply(t, f, OperationRejectReady, ReasonPolicyScopeChanged, StatusDead, string(ReasonPolicyScopeChanged))
	for _, index := range []int{jobLeaseRequestStartsBaselineIndex, jobRequestStartsIndex, jobLastStageCommitIDIndex, jobLastStageFenceIndex, jobLeaseFenceIndex} {
		if string(post[index].Value) != string(f.job[index].Value) {
			t.Fatalf("ready rejection erased prior freeze history %s", post[index].Name)
		}
	}
}

func TestJobLuaOutcomeNewFencePreIOPreservesOldWitness(t *testing.T) {
	t.Parallel()
	f := jobLuaOutcomePromotedHistory(t)
	f.r.now += 10
	id := string(f.jobs[0].JobID)
	j := f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash
	for key, value := range map[string]string{"state": "leased", "lease_owner": string(f.lease.OwnerID), "lease_token": strings.Repeat("5", 64),
		"lease_fence": "2", "claim_count": "2", "lease_started_at_ms": canonicalDecimal(f.r.now - 1), "lease_expires_at_ms": canonicalDecimal(f.r.now + 59999),
		"lease_request_starts_baseline": "1", "next_request_ordinal": "3", "last_reason": "none",
		"last_transition_id": strings.Repeat("b", 64), "last_transition_status": "CLAIMED", "updated_at_ms": canonicalDecimal(f.r.now - 1)} {
		j[key] = value
	}
	v := f.r.data[runLuaKey("")].hash
	v["claims_total"], v["reservation_creations_total"] = "2", "2"
	v["last_activity_at_ms"], v["last_execution_at_ms"] = canonicalDecimal(f.r.now-1), canonicalDecimal(f.r.now-1)
	f.r.removeKey(runLuaKey("ready"))
	f.r.removeKey(runLuaKey("ready_at"))
	f.r.setZSet(runLuaKey("leased"), map[string]float64{id: float64(f.r.now + 59999)})
	f.r.setZSet(runLuaKey("leased_at"), map[string]float64{id: float64(f.r.now - 1)})
	f.r.setZSet("mifolyo:crawl:v2:active_leases", map[string]float64{string(f.lease.RunID) + ":" + id: float64(f.r.now + 59999)})
	before := f.r.snapshot()
	jobLuaOutcomeReply(t, f, OperationReleaseBeforeIO, ReasonNone, StatusLeaseLost, "2")
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("old release cleared a new lease")
	}
	f.lease.Fence, f.lease.Token = 2, LeaseToken(strings.Repeat("5", 64))
	prior := jobLuaOutcomeRead(t, f)
	post := jobLuaOutcomeReply(t, f, OperationReleaseBeforeIO, ReasonNone, StatusReleasedReady, canonicalDecimal(f.r.now))
	for _, index := range []int{jobLeaseRequestStartsBaselineIndex, jobRequestStartsIndex, jobDeliveryAttemptsIndex, jobRetryCountIndex,
		jobLastDocumentRequestStartedAtMSIndex, jobLastDocumentRequestFenceIndex, jobLastDocumentTargetURLIndex, jobLastStageCommitIDIndex, jobLastStageFenceIndex} {
		if string(post[index].Value) != string(prior[index].Value) {
			t.Fatalf("new-fence pre-I/O release reset %s", post[index].Name)
		}
	}
}

func TestJobLuaOutcomeSlotBudgetAndNoDeletionCredit(t *testing.T) {
	t.Parallel()
	for _, extra := range []uint64{0, 1} {
		t.Run(fmt.Sprint(extra), func(t *testing.T) {
			f := jobLuaOutcomeFixtureNew(t, 1)
			jobLuaOutcomeAborted(t, f, f.r.now-1)
			// All Job/Run growth is slot-covered, but admission still includes
			// the ORIGINAL 32768-byte slot remainder and the full commit reserve.
			f.r.used = f.r.maximum - CommitMemoryReservationBytes - 32768 + extra
			if extra == 0 {
				jobLuaOutcomeReply(t, f, OperationDead, ReasonOutputInvalid, StatusDead, canonicalDecimal(f.r.now), string(ReasonOutputInvalid))
			} else {
				jobLuaOutcomeReject(t, f, OperationDead, ReasonOutputInvalid, ErrorMemoryHeadroomLow, nil)
			}
		})
	}
	f := jobLuaOutcomeFixtureNew(t, 1)
	commit := jobLuaOutcomeAborted(t, f, f.r.now-1)
	f.r.data[sharedLuaSlots].hash[commit] = "1:" + string(f.lease.RunID) + ":" + string(f.lease.JobID) + ":1:3"
	jobLuaOutcomeReject(t, f, OperationDead, ReasonOutputInvalid, ErrorMemoryHeadroomLow, nil)
	f = jobLuaOutcomeFixtureNew(t, 1)
	commit = jobLuaOutcomeAborted(t, f, f.r.now-1)
	f.r.data[sharedLuaSlots].hash[commit] = "32768:" + string(f.lease.RunID) + ":" + string(f.lease.JobID) + ":1:1"
	jobLuaOutcomeReject(t, f, OperationDead, ReasonOutputInvalid, ErrorStageInvalid, nil)
}

func TestJobLuaOutcomeCancellationAndLeasePrecedence(t *testing.T) {
	t.Parallel()
	f := jobLuaOutcomeFixtureNew(t, 0)
	jobLuaOutcomeCancelRun(f, ReasonSourceCancelled)
	jobLuaOutcomeCancelRun(f, ReasonAuthorizationExpired)
	// An effective cancellation need not manufacture a current-fence START just
	// because the requested operation was RETRY. Stored source cancellation wins
	// over later authorization expiry and retains the submitted RETRY identity.
	jobLuaOutcomeReply(t, f, OperationRetry, ReasonRequestTimeout, StatusCancelled, canonicalDecimal(f.r.now), string(ReasonSourceCancelled))
	f = jobLuaOutcomeFixtureNew(t, 1)
	f.r.now += 30000 // exact lease expiry, not just a strict greater-than case
	jobLuaOutcomeCancelRun(f, ReasonAuthorizationExpired)
	before := f.r.snapshot()
	jobLuaOutcomeReply(t, f, OperationDead, ReasonHTTP4xx, StatusLeaseLost, "1")
	if !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("expired owner terminalized a job instead of leaving recovery authority")
	}
}

var jobLuaOutcomeHistoricalReasons = []Reason{ReasonRunBudgetExhaustedAfterIO, ReasonGroupBudgetExhaustedAfterIO,
	ReasonCapacityBlockedAfterIO, ReasonRateBlockedAfterIO}

// These histories execute the real Request fragments before the real Job RETRY.
// Go constructors validate wire/identity structure, NOT a past denial. M5 must
// still map the pinned client's typed result to the matching closed reason. Lua
// checks present lease/state authority; it does not attest this historical fact.
func jobLuaOutcomeWorkerView(t *testing.T, f *workerLuaFixture, intent ReservationIntent) *jobLuaOutcomeFixture {
	t.Helper()
	for _, source := range f.sources {
		if source.JobID == intent.Lease.JobID {
			job := workerLuaHashRecord(t, f.r, wireOracleRunJobKey(f.runID, source.JobID), SchemaJob)
			return &jobLuaOutcomeFixture{recordsLuaFixture: &recordsLuaFixture{r: f.r, a: f.a,
				input: CreateRunWireInput{RunID: f.runID, PolicyGroups: f.groups}, authority: f.policy, jobs: []SourceJob{source}},
				job: job, lease: intent.Lease}
		}
	}
	t.Fatal("owning source job not found")
	return nil
}

func jobLuaOutcomePinGroupLimit(t *testing.T, f *workerLuaFixture, groupID GroupID, limit uint64) {
	t.Helper()
	base := requestLuaPrefix + "run:" + string(f.runID)
	if f.r.data[base].hash["reservation_creations_total"] != "0" {
		t.Fatal("test policy must be pinned before requests")
	}
	found := false
	for i := range f.groups {
		if f.groups[i].GroupID == groupID {
			f.groups[i].RequestStartLimit, found = limit, true
		}
	}
	if !found {
		t.Fatal("unknown fixture policy group")
	}
	digest, err := DerivePolicyGroupMapDigest(f.groups)
	if err != nil {
		t.Fatal(err)
	}
	f.r.data[base].hash["policy_group_map_sha256"] = string(digest)
	f.r.data[base+":group_limits"].hash[string(groupID)] = canonicalDecimal(limit)
	f.policy, err = newTestTransportAuthority().parseRunPolicyAuthority(f.runID, workerLuaHashRecord(t, f.r, base, SchemaRun), f.groups)
	if err != nil {
		t.Fatal(err)
	}
}

func jobLuaOutcomeObservedDenial(t *testing.T, f *workerLuaFixture, op OperationName, intent ReservationIntent, status Status) []any {
	t.Helper()
	before := f.r.snapshot()
	reply := workerLuaReply(t, f, op, intent, status)
	if reply[len(reply)-1] != "1" {
		t.Fatal("fixture did not observe an after-I/O denial", reply)
	}
	id, err := DeriveReservationID(f.policy, intent)
	if err != nil {
		t.Fatal(err)
	}
	jobKey, reservationKey := wireOracleRunJobKey(f.runID, intent.Lease.JobID), requestLuaPrefix+"reservation:"+string(id)
	if !reflect.DeepEqual(before.data[jobKey], f.r.data[jobKey]) || !reflect.DeepEqual(before.data[reservationKey], f.r.data[reservationKey]) {
		t.Fatal("denial invented a job/reservation history record")
	}
	if op == OperationReserveRequest {
		if _, exists := f.r.data[reservationKey]; exists {
			t.Fatal("denied reservation was materialized")
		}
	}
	return reply
}

func jobLuaOutcomeHistoricalRetry(t *testing.T, f *workerLuaFixture, intent ReservationIntent, reason Reason) {
	t.Helper()
	view := jobLuaOutcomeWorkerView(t, f, intent)
	due := canonicalDecimal(f.r.now + 30000)
	post := jobLuaOutcomeReply(t, view, OperationRetry, reason, StatusRetryScheduled, due, "1", string(reason))
	for _, index := range []int{jobLeaseRequestStartsBaselineIndex, jobRequestStartsIndex, jobDeliveryAttemptsIndex,
		jobNextRequestOrdinalIndex, jobLastDocumentRequestFenceIndex, jobLastDocumentTargetURLIndex, jobLastDocumentTargetDigestIndex} {
		if string(post[index].Value) != string(view.job[index].Value) {
			t.Fatalf("historical denial retry changed %s", post[index].Name)
		}
	}
	if string(post[jobLastReasonIndex].Value) != string(reason) || string(post[jobLastFailureReasonIndex].Value) != string(reason) ||
		f.r.data[runLuaKey("retry_reason_counts")].hash[string(reason)] != "1" {
		t.Fatal("historical reason was reclassified or not counted exactly once")
	}
	before := f.r.snapshot()
	f.r.now++
	jobLuaOutcomeReply(t, view, OperationRetry, reason, StatusRetryScheduled, due, "1", string(reason))
	if f.r.attempts != 0 || !reflect.DeepEqual(before, f.r.snapshot()) {
		t.Fatal("historical denial replay duplicated effects")
	}
}

func TestJobLuaOutcomeRetryAfterAvailabilityReturns(t *testing.T) {
	t.Parallel()
	for _, reason := range jobLuaOutcomeHistoricalReasons {
		t.Run(string(reason), func(t *testing.T) {
			count, interval, status := 2, uint64(0), StatusRunBudgetExhausted
			switch reason {
			case ReasonGroupBudgetExhaustedAfterIO:
				status = StatusGroupBudgetExhausted
			case ReasonCapacityBlockedAfterIO:
				count, status = 3, StatusCapacityBlocked
			case ReasonRateBlockedAfterIO:
				count, interval, status = 1, 100, StatusRateBlocked
			}
			f := workerLuaFixtureNew(t, count, interval)
			base := requestLuaPrefix + "run:" + string(f.runID)
			if reason == ReasonRunBudgetExhaustedAfterIO {
				f.r.data[base].hash["max_request_starts"] = "2"
			} else if reason == ReasonGroupBudgetExhaustedAfterIO {
				jobLuaOutcomePinGroupLimit(t, f, "default", 2)
			}
			i := workerLuaIntent(t, f, 0, 1, RequestDocument)
			workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
			workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
			workerLuaReply(t, f, OperationFinishRequest, i, StatusFinished)
			var peers []ReservationIntent
			for j := 1; j < count; j++ {
				peer := workerLuaIntent(t, f, j, 1, RequestDocument)
				workerLuaReply(t, f, OperationTryClaim, peer, StatusClaimed)
				peers = append(peers, peer)
			}
			i.RequestOrdinal = 2
			jobLuaOutcomeObservedDenial(t, f, OperationReserveRequest, i, status)
			if len(peers) > 0 {
				workerLuaReply(t, f, OperationCancelReservation, peers[0], StatusReservationCancelled)
			} else {
				f.r.now += interval // rate availability returns at the exact deadline
			}
			run := f.r.data[base].hash
			switch reason {
			case ReasonRunBudgetExhaustedAfterIO:
				if run["request_starts"] != "1" || run["pending_request_reservations"] != "0" || run["max_request_starts"] != "2" {
					t.Fatal("run budget did not return after peer cancellation")
				}
			case ReasonGroupBudgetExhaustedAfterIO:
				if f.r.data[base+":group_started"].hash["default"] != "1" || f.r.data[base+":group_pending"].hash["default"] != "0" ||
					f.r.data[base+":group_limits"].hash["default"] != "2" {
					t.Fatal("group budget did not return after peer cancellation")
				}
			case ReasonCapacityBlockedAfterIO:
				global := f.r.data[requestLuaPrefix+"rate:"+string(DeriveGlobalScopeID())].hash
				if global["active_count"] != "1" || global["effective_concurrency"] != "2" {
					t.Fatal("global request capacity did not return")
				}
			case ReasonRateBlockedAfterIO:
				for _, scope := range []Digest{i.Decision.GroupScopeID, i.Decision.OriginScopeID} {
					deadline, err := strconv.ParseUint(f.r.data[requestLuaPrefix+"rate:"+string(scope)].hash["next_allowed_ms"], 10, 64)
					if err != nil || deadline > f.r.now {
						t.Fatal("rate deadline has not elapsed")
					}
				}
			}
			jobLuaOutcomeHistoricalRetry(t, f, i, reason)
		})
	}
}

func TestJobLuaOutcomeRetryRedirectedGroupDenial(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 2, 0)
	workerLuaAddChargedGroup(t, f)
	jobLuaOutcomePinGroupLimit(t, f, "other", 1)
	target, peer := workerLuaIntent(t, f, 0, 1, RequestDocument), workerLuaIntent(t, f, 1, 1, RequestDocument)
	for _, intent := range []ReservationIntent{target, peer} {
		workerLuaReply(t, f, OperationTryClaim, intent, StatusClaimed)
		workerLuaReply(t, f, OperationStartRequest, intent, StatusStarted)
		workerLuaReply(t, f, OperationFinishRequest, intent, StatusFinished)
	}
	target, peer = workerLuaOtherGroupIntent(t, f, target), workerLuaOtherGroupIntent(t, f, peer)
	workerLuaReply(t, f, OperationReserveRequest, peer, StatusReserved)
	denial := jobLuaOutcomeObservedDenial(t, f, OperationReserveRequest, target, StatusGroupBudgetExhausted)
	if denial[2] != "other" {
		t.Fatal("denied request did not charge the redirected group", denial)
	}
	base := requestLuaPrefix + "run:" + string(f.runID)
	if f.r.data[base+":group_pending"].hash["other"] != "1" || f.r.data[base+":group_open_jobs"].hash["other"] != "0" ||
		f.r.data[base+":group_open_jobs"].hash["default"] != "2" || f.r.data[base+":group_started"].hash["default"] != "2" {
		t.Fatal("source/charged group distinction was lost")
	}
	workerLuaReply(t, f, OperationCancelReservation, peer, StatusReservationCancelled)
	if f.r.data[base+":group_pending"].hash["other"] != "0" {
		t.Fatal("redirected group availability did not return")
	}
	jobLuaOutcomeHistoricalRetry(t, f, target, ReasonGroupBudgetExhaustedAfterIO)
	if f.r.data[base+":group_open_jobs"].hash["default"] != "2" || f.r.data[base+":group_open_jobs"].hash["other"] != "0" {
		t.Fatal("retry moved open-job accounting to the denied request group")
	}
}

// Fixture-only second run, pinned before any of its requests. Its real blocked
// CLAIM tightens shared scopes; no Lua parser, permission or rate check is mocked.
func jobLuaOutcomeTighteningPeer(t *testing.T, f *workerLuaFixture) *workerLuaFixture {
	t.Helper()
	other := workerLuaFixtureNew(t, 1, 1000)
	oldID, oldBase := other.runID, requestLuaPrefix+"run:"+string(other.runID)
	other.runID = RunID(strings.Repeat("8", 32))
	newBase := requestLuaPrefix + "run:" + string(other.runID)
	other.groups[0].Concurrency = 1
	other.sources[0].Decision.GroupConcurrency, other.sources[0].Decision.OriginConcurrency = 1, 1
	source, err := completeSourceJobRecord(other.sources[0])
	if err != nil {
		t.Fatal(err)
	}
	digest, err := DerivePolicyGroupMapDigest(other.groups)
	if err != nil {
		t.Fatal(err)
	}
	for key, entry := range other.r.data {
		if key != oldBase && !strings.HasPrefix(key, oldBase+":") {
			continue
		}
		newKey := newBase + strings.TrimPrefix(key, oldBase)
		if entry.kind == "hash" {
			if strings.Contains(key, ":job:") {
				entry.hash["run_id"] = string(other.runID)
				for _, field := range source {
					entry.hash[field.Name] = string(field.Value)
				}
			} else if key == oldBase {
				entry.hash["policy_group_map_sha256"], entry.hash["crawl_policy_sha256"] = string(digest), strings.Repeat("b", 64)
			} else if key == oldBase+":group_concurrency" {
				entry.hash["default"] = "1"
			}
		}
		f.r.data[newKey] = entry
		if members, ok := other.r.sets[key]; ok {
			f.r.sets[newKey] = members
		}
		if members, ok := other.r.zsets[key]; ok {
			f.r.zsets[newKey] = members
		}
	}
	f.r.zsets[RunsKey][string(other.runID)] = other.r.zsets[RunsKey][string(oldID)]
	f.r.sets[ActiveRunsKey][string(other.runID)], f.r.sets[UnarchivedRunsKey][string(other.runID)] = true, true
	other.r = f.r
	other.policy, err = newTestTransportAuthority().parseRunPolicyAuthority(other.runID, workerLuaHashRecord(t, other.r, newBase, SchemaRun), other.groups)
	if err != nil {
		t.Fatal(err)
	}
	return other
}

func TestJobLuaOutcomeRetryAfterStartRateBlockAndCancel(t *testing.T) {
	t.Parallel()
	f := workerLuaFixtureNew(t, 1, 0)
	i := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, i, StatusClaimed)
	workerLuaReply(t, f, OperationStartRequest, i, StatusStarted)
	workerLuaReply(t, f, OperationFinishRequest, i, StatusFinished)
	i.RequestOrdinal = 2
	workerLuaReply(t, f, OperationReserveRequest, i, StatusReserved)
	other := jobLuaOutcomeTighteningPeer(t, f)
	workerLuaReply(t, other, OperationTryClaim, workerLuaIntent(t, other, 0, 1, RequestDocument), StatusCapacityBlocked)
	denial := jobLuaOutcomeObservedDenial(t, f, OperationStartRequest, i, StatusRateBlocked)
	view := jobLuaOutcomeWorkerView(t, f, i)
	jobLuaOutcomeReject(t, view, OperationRetry, ReasonRateBlockedAfterIO, ErrorInvalidState, nil)
	workerLuaReply(t, f, OperationCancelReservation, i, StatusReservationCancelled)
	id, err := DeriveReservationID(f.policy, i)
	if err != nil {
		t.Fatal(err)
	}
	q := workerLuaHashRecord(t, f.r, requestLuaPrefix+"reservation:"+string(id), SchemaReservation)
	if string(q[reservationStateIndex].Value) != "cancelled" || string(q[reservationStartedAtMSIndex].Value) != "0" ||
		string(q[reservationJobStartsAfterStartIndex].Value) != "0" {
		t.Fatal("denied START was charged or not cancelled")
	}
	deadline, err := strconv.ParseUint(denial[3].(string), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	f.r.now = deadline // no current rate block remains, but the past result is valid
	jobLuaOutcomeHistoricalRetry(t, f, i, ReasonRateBlockedAfterIO)
	base := requestLuaPrefix + "run:" + string(f.runID)
	if f.r.data[base].hash["request_starts"] != "1" || f.r.data[base].hash["reservation_creations_total"] != "2" ||
		f.r.data[base].hash["pending_request_reservations"] != "0" {
		t.Fatal("START block/cancel/retry reset a start, refunded creation, or kept pending capacity")
	}
}

func TestJobLuaOutcomeHistoricalReasonsStillRequireLeaseAndStart(t *testing.T) {
	t.Parallel()
	for _, reason := range jobLuaOutcomeHistoricalReasons {
		t.Run(string(reason), func(t *testing.T) {
			f := jobLuaOutcomeFixtureNew(t, 0)
			jobLuaOutcomeReject(t, f, OperationRetry, reason, ErrorInvalidState, nil)
			f = jobLuaOutcomeFixtureNew(t, 1)
			f.lease.Token = LeaseToken(strings.Repeat("e", 64))
			before := f.r.snapshot()
			jobLuaOutcomeReply(t, f, OperationRetry, reason, StatusLeaseLost, "1")
			if !reflect.DeepEqual(before, f.r.snapshot()) {
				t.Fatal("caller history assertion bypassed lease fencing")
			}
			f = jobLuaOutcomeFixtureNew(t, 1)
			f.r.data[wireOracleRunJobKey(f.lease.RunID, f.lease.JobID)].hash["active_reservation_id"] = strings.Repeat("a", 64)
			jobLuaOutcomeReject(t, f, OperationRetry, reason, ErrorInvalidState, nil)
		})
	}
	for _, reason := range []Reason{ReasonLeaseExpiredAfterIO, ReasonRetryExhausted, ReasonPreIORecoveryExhausted, ReasonReservationLimitExhausted} {
		t.Run("excluded/"+string(reason), func(t *testing.T) {
			f := jobLuaOutcomeFixtureNew(t, 1)
			jobLuaOutcomeReject(t, f, OperationRetry, ReasonRequestTimeout, ErrorInvalidArgument,
				func(_, args []string) { args[len(args)-2] = string(reason) })
		})
	}
}

// Real CLAIM -> START -> FINISH -> BEGIN -> chunks -> SEAL -> later blocked
// COMMIT. Only approved fixture setup precedes the chain; the Go output authority
// is constructed from the actual START response and retained document witness.
func jobLuaLaterBackpressureStage(t *testing.T) (*workerLuaFixture, *stageOpsFixture) {
	t.Helper()
	f := workerLuaFixtureNew(t, 1, 0)
	renderBytes := testDenyAllRenderPolicyArtifact()
	f.r.data[runLuaKey("")].hash["render_policy_sha256"] = string(plainSHA256(renderBytes))
	var err error
	f.policy, err = newTestTransportAuthority().parseRunPolicyAuthority(f.runID, workerLuaHashRecord(t, f.r, runLuaKey(""), SchemaRun), f.groups)
	if err != nil {
		t.Fatal(err)
	}
	intent := workerLuaIntent(t, f, 0, 1, RequestDocument)
	workerLuaReply(t, f, OperationTryClaim, intent, StatusClaimed)
	f.r.now++
	started := workerLuaReply(t, f, OperationStartRequest, intent, StatusStarted)
	f.r.now++
	workerLuaReply(t, f, OperationFinishRequest, intent, StatusFinished)
	f.r.now++
	response, err := newTestTransportAuthority().parseStartRequestResponse(f.policy, intent, started)
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
	jobKey := wireOracleRunJobKey(f.runID, source.JobID)
	j := f.r.data[jobKey].hash
	witness, err := newTestTransportAuthority().parseFinalDocumentWitness(intent.Lease, []string{
		j["last_document_request_started_at_ms"], j["last_document_request_fence"], j["last_document_target_url_id"],
		j["last_document_target_url"], j["last_document_target_digest"], j["request_starts"], j["last_request_started_at_ms"],
		j["lease_request_starts_baseline"], j["state"], j["lease_owner"], j["lease_token"], j["lease_fence"], j["active_reservation_id"],
	})
	if err != nil {
		t.Fatal(err)
	}
	render, err := NewRenderPolicyAuthorization(f.policy, renderBytes)
	if err != nil {
		t.Fatal(err)
	}
	outputContext, err := NewOutputContext(f.policy, source, transcript, witness, render)
	if err != nil {
		t.Fatal(err)
	}
	s := &stageOpsFixture{r: &stageOpsRedis{f.r}, a: f.a, context: outputContext, source: source, lease: intent.Lease,
		output: CrawlOutput{Page: OutputPage{NormalizedURL: source.CanonicalURL, HTML: []byte("<html>later backpressure</html>"), ContentType: "text/html", StatusCode: 200}},
		runKey: runLuaKey(""), jobKey: jobKey}
	s.rebuildOutput(t)
	s.stageAll(t, false)
	before := f.r.snapshot()
	f.r.now += 1000
	s.fullQueue()
	reply := s.expect(t, OperationCommit, nil, StatusDownstreamBackpressure)
	stage := stageOpsValidateHash(t, s.r, s.prefix+"meta", SchemaStageMeta)
	expires, err := strconv.ParseUint(string(stage[stageExpiresAtMSIndex].Value), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	deadline := min(f.r.now+MaxCommitBackpressureMilliseconds, expires-CommitBackpressureBeforeStageExpiryMilliseconds)
	if !reflect.DeepEqual(reply, []any{string(StatusDownstreamBackpressure), canonicalDecimal(f.r.now), "pages_queue_full", canonicalDecimal(f.r.now), canonicalDecimal(deadline)}) {
		t.Fatal("incorrect first backpressure timestamp/deadline", reply)
	}
	allowed := map[string]bool{"commit_backpressure_fence": true, "commit_backpressure_reason": true,
		"commit_backpressure_started_at_ms": true, "commit_backpressure_deadline_ms": true}
	for field, value := range before.data[jobKey].hash {
		if !allowed[field] && f.r.data[jobKey].hash[field] != value {
			t.Fatalf("first blocked COMMIT broadened Job writes: %s", field)
		}
	}
	if !reflect.DeepEqual(before.data[s.runKey], f.r.data[s.runKey]) {
		t.Fatal("first blocked COMMIT changed the general Run record")
	}
	for _, call := range f.r.trace {
		if call.acl || call.name != "HSET" {
			continue
		}
		if call.args[0] == s.runKey {
			t.Fatal("blocked COMMIT planned an unnecessary Run HSET")
		}
		if call.args[0] == s.jobKey {
			for i := 1; i < len(call.args); i += 2 {
				if !allowed[call.args[i]] {
					t.Fatal("blocked COMMIT wrote a non-backpressure Job field", call.args[i])
				}
			}
		}
	}
	jobUpdated, _ := strconv.ParseUint(j["updated_at_ms"], 10, 64)
	runActivity, _ := strconv.ParseUint(f.r.data[s.runKey].hash["last_activity_at_ms"], 10, 64)
	if jobUpdated >= f.r.now || runActivity >= f.r.now {
		t.Fatal("regression fixture did not separate dedicated/general clocks")
	}
	workerLuaHashRecord(t, f.r, jobKey, SchemaJob)
	workerLuaHashRecord(t, f.r, s.runKey, SchemaRun)
	return f, s
}

func jobLuaReadBackpressureClock(t *testing.T, f *workerLuaFixture, s *stageOpsFixture) bootLuaResult {
	t.Helper()
	request := jobLuaOutcomeWire(t, f.a, f.policy, s.source, s.lease, OperationDead, ReasonOutputInvalid)
	keys, args := runLuaParts(t, request, nil)
	// Read-only exercise of the actual Job reader, without requiring an outcome
	// to be admissible while this stage remains active. No semantic replacements.
	source := workerLuaCore(t) + `
local ctx,code=CJ.Context.open(CJ.Wire.worker_spec("CJ2_DEAD"),KEYS,ARGV)
if not ctx then return CJ.Context.reject(code) end
local gate; gate,code=CJ.Gate.check(ctx); if not gate then return CJ.Context.reject(code) end
local run; run,code=CJ.Run.load(ctx,ctx.request.v.run_id); if not run then return CJ.Context.reject(code) end
local job; job,code=CJ.Job.load(ctx,run,ctx.request.v.job_id); if not job then return CJ.Context.reject(code) end
local valid; valid,code=CJ.Job.at(ctx,run,job); if not valid then return CJ.Context.reject(code) end
return {job.v.commit_backpressure_started_at_ms,job.v.updated_at_ms,run.v.last_activity_at_ms}`
	before := s.r.snapshot()
	result := stageOpsRun(t, s.r, source, keys, args)
	if s.r.attempts != 0 || !reflect.DeepEqual(before, s.r.snapshot()) {
		t.Fatal("Job clock validation mutated state")
	}
	stageOpsAssertTrace(t, s.r)
	return result
}

func TestJobLuaOutcomeLaterBackpressureClockAndRecovery(t *testing.T) {
	t.Parallel()
	f, original := jobLuaLaterBackpressureStage(t)
	t.Run("reader_before_general_activity", func(t *testing.T) {
		s := original.clone()
		got := sharedLuaNoError(t, jobLuaReadBackpressureClock(t, f, s))
		j := s.r.data[s.jobKey].hash
		want := []any{j["commit_backpressure_started_at_ms"], j["updated_at_ms"], s.r.data[s.runKey].hash["last_activity_at_ms"]}
		if !reflect.DeepEqual(got, want) {
			t.Fatal("dedicated backpressure clock was not retained", got)
		}
	})
	for _, cancelled := range []bool{false, true} {
		t.Run("recovery/cancelled="+strconv.FormatBool(cancelled), func(t *testing.T) {
			s := original.clone()
			beforeJob := stageOpsValidateHash(t, s.r, s.jobKey, SchemaJob)
			if cancelled {
				request, err := NewCancelRunWireRequest(runLuaGate(t, f.a, OperationCancelRun, false), CancelRunWireInput{RunID: f.runID, Reason: ReasonOperatorCancelled})
				keys, args := runLuaParts(t, request, err)
				reply := sharedLuaNoError(t, stageOpsRun(t, s.r, runLuaSource(t, OperationCancelRun), keys, args))
				if err := ValidateOperationResponse(OperationCancelRun, reply); err != nil {
					t.Fatal(err)
				}
				stageOpsAssertTrace(t, s.r)
			}
			expires, err := strconv.ParseUint(s.r.data[s.jobKey].hash["lease_expires_at_ms"], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			s.r.now = expires
			request, err := NewRecoverExpiredWireRequest(runLuaGate(t, f.a, OperationRecoverExpired, false), f.runID)
			keys, args := runLuaParts(t, request, err)
			reply := sharedLuaNoError(t, stageOpsRun(t, s.r, maintenanceLuaSource(t, OperationRecoverExpired), keys, args))
			if err := ValidateOperationResponse(OperationRecoverExpired, reply); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reply, []any{"BATCH_DONE", canonicalDecimal(expires), "1", "0"}) {
				t.Fatal("later-time backpressure recovery did not process the lease", reply)
			}
			stageOpsAssertTrace(t, s.r)
			post := stageOpsValidateHash(t, s.r, s.jobKey, SchemaJob)
			stageOpsValidateHash(t, s.r, s.runKey, SchemaRun)
			wantState, wantReason := "delayed", "lease_expired_after_io"
			if cancelled {
				wantState, wantReason = "cancelled", "operator_cancelled"
			}
			if string(post[jobStateIndex].Value) != wantState || string(post[jobLastReasonIndex].Value) != wantReason ||
				string(post[jobCommitBackpressureStartedAtMSIndex].Value) != "0" || string(post[jobCommitBackpressureReasonIndex].Value) != "none" {
				t.Fatal("recovery did not clear the dedicated block while choosing the correct outcome")
			}
			for _, index := range []int{jobLeaseRequestStartsBaselineIndex, jobRequestStartsIndex, jobLeaseFenceIndex, jobDeliveryAttemptsIndex,
				jobLastDocumentRequestStartedAtMSIndex, jobLastDocumentRequestFenceIndex, jobLastDocumentTargetURLIDIndex,
				jobLastDocumentTargetURLIndex, jobLastDocumentTargetDigestIndex, jobLastStageCommitIDIndex, jobLastStageFenceIndex} {
				if string(post[index].Value) != string(beforeJob[index].Value) {
					t.Fatalf("recovery reset retained history %s", post[index].Name)
				}
			}
			if len(s.r.zsets[s.runKey+":commit_backpressure"]) != 0 || len(s.r.zsets[ActiveLeasesKey]) != 0 ||
				s.r.data[StageSlotsKey].hash[string(s.commit)] != "" {
				t.Fatal("recovery retained backpressure, lease or slot ownership")
			}
		})
	}
}

func TestJobLuaOutcomeBackpressureIndependentClockBounds(t *testing.T) {
	t.Parallel()
	f, original := jobLuaLaterBackpressureStage(t)
	for _, kind := range []string{"future", "before_lease", "at_lease_expiry", "foreign_fence", "before_stage", "wrong_deadline"} {
		t.Run(kind, func(t *testing.T) {
			s := original.clone()
			j := s.r.data[s.jobKey].hash
			started, err := strconv.ParseUint(j["commit_backpressure_started_at_ms"], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "future":
				started = s.r.now + 1
			case "before_lease":
				at, _ := strconv.ParseUint(j["lease_started_at_ms"], 10, 64)
				started = at - 1
			case "at_lease_expiry":
				started, _ = strconv.ParseUint(j["lease_expires_at_ms"], 10, 64)
				s.r.now = started + 1 // past at read time, but invalid at original admission
			case "foreign_fence":
				j["commit_backpressure_fence"] = "0"
			case "before_stage":
				at, _ := strconv.ParseUint(s.r.data[s.prefix+"meta"].hash["created_at_ms"], 10, 64)
				started = at - 1
			}
			j["commit_backpressure_started_at_ms"] = canonicalDecimal(started)
			j["commit_backpressure_deadline_ms"] = canonicalDecimal(started + MaxCommitBackpressureMilliseconds)
			s.r.zsets[s.runKey+":commit_backpressure"][string(s.lease.JobID)] = float64(started)
			if kind == "wrong_deadline" {
				// Still inside the Job-only 120s bound; Stage must require the
				// EXACT min(start+120s, stage expiry-10s), not just a loose range.
				j["commit_backpressure_deadline_ms"] = canonicalDecimal(started + MaxCommitBackpressureMilliseconds - 1)
			}
			if kind == "before_stage" || kind == "wrong_deadline" {
				s.reject(t, OperationCommit, nil, nil, ErrorStageInvalid)
			} else {
				result := jobLuaReadBackpressureClock(t, f, s)
				if result.runtimeErr != nil || result.raw != bootLuaErrorReply("ERR CRAWL_V2_INVALID_STATE") {
					t.Fatalf("bad dedicated clock/fence accepted: %v/%v", result.raw, result.runtimeErr)
				}
			}
		})
	}
}
