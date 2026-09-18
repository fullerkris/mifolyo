package crawljobsv2

import (
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Explicit lexical assembly of the actual owned fragments, NOT generated
// canonical sources, a shadow implementation, or a production script binding.
func runLuaCore(t *testing.T) string {
	t.Helper()
	if runtime.Version() != "go1.25.13" {
		t.Fatalf("Run Lua conformance requires pinned Go 1.25.13, got %s", runtime.Version())
	}
	return sharedLuaCore(t) + "CJ.Run = (function()\n" + string(primitiveLuaRead(t, "lua_src/ledger_run.lua")) + "\nend)()\n"
}

func runLuaSource(t *testing.T, operation OperationName) string {
	t.Helper()
	return runLuaCore(t) + string(primitiveLuaRead(t, "lua_src/ops/"+strings.ToLower(string(operation))+".lua"))
}

func TestRunLuaCompleteSchemaOracle(t *testing.T) {
	t.Parallel()
	var records []Record
	var labels []string
	add := func(label string, record Record) {
		records = append(records, record)
		labels = append(labels, label)
	}
	for _, state := range []string{"loading", "auditing", "sealed", "active", "completed", "budget_exhausted", "cancelled", "archived"} {
		record := recordAuthorityRunRecord(t, state)
		add(state, record)
		for i, field := range record {
			for _, replacement := range []string{"", "0", "1", "2", "10", "64", "100", "10000", "9007199254740991", "00", "-1", "9007199254740992", strings.Repeat("z", 129), strings.Repeat("0", 64), ZeroSHA256, "\xff"} {
				changed := cloneRecord(record)
				changed[i].Value = []byte(replacement)
				add(state+"/"+field.Name+"/"+fmt.Sprintf("%q", replacement), changed)
			}
		}
	}
	// Legal cancelled prefixes, including an interrupted audit, preserve history.
	for _, state := range []string{"loading", "auditing", "sealed", "active"} {
		record := recordAuthorityRunRecord(t, state)
		record[runStateIndex].Value = []byte("cancelled")
		record[runCancelledAtMSIndex].Value = []byte("600")
		record[runLastActivityAtMSIndex].Value = []byte("600")
		record[runTerminalReasonIndex].Value = []byte("source_cancelled")
		add("cancelled-from-"+state, record)
	}
	for _, state := range []string{"loading", "auditing", "sealed", "active", "completed"} {
		record := recordAuthorityRunRecord(t, state)
		for _, name := range []string{"expected_seed_count", "job_count", "open_job_count", "request_starts", "reservation_creations_total", "claims_total", "completed_total", "dead_total", "output_commits_total", "audit_count", "last_request_started_at_ms", "last_terminal_transition_at_ms"} {
			for i := range record {
				if record[i].Name == name {
					record[i].Value = []byte("0")
				}
			}
		}
		record[runAuditCursorIndex].Value = nil
		add("empty-"+state, record)
	}
	// Framing has to reject missing/extra fields, not merely project them away.
	base := recordAuthorityRunRecord(t, "loading")
	add("missing", cloneRecord(base[:58]))
	add("identity-is-not-a-field", append(cloneRecord(base), textField("run_id", strings.Repeat("a", 32))))
	args := make([]string, len(records))
	want := make([]any, len(records))
	for i, record := range records {
		args[i] = string(primitiveLuaEncoded(t, record))
		want[i] = "rejected"
		if ValidateRecord(SchemaRun, record) == nil {
			want[i] = "valid"
		}
	}
	source := runLuaCore(t) + `local result = {}
for i=1,#ARGV do
    local record = CJ.Schemas.decode("run",ARGV[i])
    result[i] = record and "valid" or "rejected"
end
return result`
	// Bound each in-memory VM invocation too; this is codec differential testing,
	// not a claim that a Redis operation admits thousands of wire records.
	for offset := 0; offset < len(args); offset += 256 {
		end := min(offset+256, len(args))
		r := sharedLuaNewRedis()
		before := r.snapshot()
		got := sharedLuaNoError(t, sharedLuaRun(t, r, source, nil, args[offset:end])).([]any)
		if len(got) != end-offset || len(r.trace) != 0 || !reflect.DeepEqual(before, r.snapshot()) {
			t.Fatal("pure registration/schema validation performed Redis I/O or changed result shape")
		}
		for i := range got {
			if got[i] != want[offset+i] {
				t.Errorf("%s: Lua %s, independent Go validator %s", labels[offset+i], got[i], want[offset+i])
			}
		}
	}
}

func runLuaParts(t *testing.T, request OperationWireRequest, err error) ([]string, []string) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	k, a, _, err := request.validatedWireParts()
	if err != nil {
		t.Fatal(err)
	}
	keys, args := make([]string, len(k)), make([]string, len(a))
	for i := range k {
		keys[i] = string(k[i])
	}
	for i := range a {
		args[i] = string(a[i])
	}
	return keys, args
}

func runLuaReply(t *testing.T, r *sharedLuaRedis, op OperationName, keys, args []string, status string, tail ...string) {
	t.Helper()
	result := sharedLuaRun(t, r, runLuaSource(t, op), keys, args)
	if _, failed := result.raw.(bootLuaErrorReply); failed {
		t.Logf("last read: %v", r.trace[len(r.trace)-1])
	}
	got := sharedLuaNoError(t, result)
	if err := ValidateOperationResponse(op, got); err != nil {
		t.Fatalf("Go response oracle: %v", err)
	}
	want := []any{status, strconv.FormatUint(r.now, 10)}
	for _, value := range tail {
		want = append(want, value)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response: %v, want %v", got, want)
	}
	runLuaTrace(t, r)
}

const runLuaID = "11111111111111111111111111111111"

var runLuaMapNames = strings.Fields(`group_limits group_rate_scope_ids group_scope_ids group_concurrency group_interval_ms group_started group_pending group_active_started group_open_jobs`)
var runLuaRetryNames = strings.Fields(`request_timeout dns_temporary dial_temporary request_temporary http_429 http_5xx robots_temporary renderer_temporary downstream_backpressure capacity_blocked_after_io run_budget_exhausted_after_io group_budget_exhausted_after_io rate_blocked_after_io lease_expired_after_io worker_shutdown_after_io`)
var runLuaRecoveryNames = strings.Fields(`ready delayed dead cancelled`)
var runLuaDispositionNames = strings.Fields(`published already_visited policy_denied policy_scope_changed robots_denied robots_invalid job_malformed url_identity_mismatch static_url_denied dns_prohibited http_4xx response_invalid body_too_large html_invalid discovery_limit renderer_permanent output_invalid run_job_limit reservation_limit_exhausted retry_exhausted pre_io_recovery_exhausted protocol_corrupt authorization_expired operator_cancelled source_cancelled`)

func runLuaKey(suffix string) string {
	key := "mifolyo:crawl:v2:run:" + runLuaID
	if suffix != "" {
		key += ":" + suffix
	}
	return key
}

func runLuaFixture(t *testing.T, candidate bool) (*sharedLuaRedis, gateArtifacts, CreateRunWireInput) {
	t.Helper()
	r := sharedLuaNewRedis()
	a := newGateArtifacts(t)
	if candidate {
		a = newMigrationGateArtifacts(t, 2)
	}
	r.data[DurabilityKey].hash["boot_epoch"] = a.bootEpoch
	marker, _ := a.marker.Record()
	if candidate {
		freeze, _ := a.freeze.Record()
		r.setHash(ContractsCandidateKey, marker)
		r.setHash(AdminFreezeKey, freeze)
		r.data[CrawlContractCandidateKey] = bootLuaEntry{kind: "string", value: string(a.contract), expireAt: -1}
	} else {
		guard, _ := a.guard.Record()
		legacy, _ := a.legacy.Record()
		r.setHash(ContractsActiveKey, marker)
		r.setHash(CommitGuardKey, guard)
		r.setHash(LegacyRetirementKey, legacy)
		r.data[CrawlContractKey] = bootLuaEntry{kind: "string", value: string(a.contract), expireAt: -1}
	}
	// Remove only exact stopped-operation canaries. Unrelated sentinels remain
	// in the entire-state snapshot and must survive every operation/error/replay.
	for _, key := range []string{"mifolyo:crawl:v1:queue", "mifolyo:crawl:v2:active_leases", "pages_queue"} {
		delete(r.data, key)
	}
	lineage := RateScopeID(strings.Repeat("2", 32))
	scope, err := DeriveGroupScopeID(lineage)
	if err != nil {
		t.Fatal(err)
	}
	groups := []PolicyGroup{{GroupID: "research/é", RateScopeID: lineage, GroupScopeID: scope, RequestStartLimit: 10, Concurrency: 2, IntervalMS: 1000}}
	mapDigest, err := DerivePolicyGroupMapDigest(groups)
	if err != nil {
		t.Fatal(err)
	}
	digest := Digest(strings.Repeat("a", 64))
	input := CreateRunWireInput{
		RunID: RunID(runLuaID), SourceKind: SourceMongo, SourceSHA256: digest, ExpectedSeedCount: 2,
		AuthorizationSHA256: digest, AuthorizationScopeSHA256: digest, AuthorizationExpiresAtMS: bootLuaNow + 3600000,
		CanonicalizationVersion: 1, CanonicalizationSHA256: digest, CrawlPolicyVersion: 2, CrawlPolicySHA256: digest,
		RenderPolicyVersion: 1, RenderPolicySHA256: digest, PolicyGroupMapSHA256: mapDigest,
		MaxJobs: 10000, MaxRequestStarts: 10, GlobalConcurrencyLimit: 2, MaxDeliveryAttempts: 3, PolicyGroups: groups,
	}
	if candidate {
		input.SourceKind = SourceV1Migration
	}
	return r, a, input
}

func runLuaGate(t *testing.T, a gateArtifacts, op OperationName, candidate bool) TransportGate {
	t.Helper()
	input := activeGateInput(a)
	if candidate {
		input = candidateGateInput(a, CandidateBeforeLegacyRetirement, nil)
	}
	gate, err := NewTransportGate(op, input)
	if err != nil {
		t.Fatal(err)
	}
	return gate
}

// Case data only; collection command semantics belong to the shared facade.
func runLuaCollection(r *sharedLuaRedis, key, kind string, members map[string]string) {
	if len(members) == 0 {
		r.removeKey(key)
		return
	}
	if kind == "set" {
		ids := make([]string, 0, len(members))
		for member := range members {
			ids = append(ids, member)
		}
		r.setSet(key, ids)
		return
	}
	if kind == "zset" {
		scores := map[string]float64{}
		for member, text := range members {
			score, err := strconv.ParseFloat(text, 64)
			if err != nil {
				panic(err)
			}
			scores[member] = score
		}
		r.setZSet(key, scores)
		return
	}
	r.data[key] = bootLuaEntry{kind: kind, hash: members, expireAt: -1}
}

func runLuaRecord(t *testing.T, r *sharedLuaRedis) Record {
	t.Helper()
	names, _ := RecordSchemaFields(SchemaRun)
	entry := r.data[runLuaKey("")]
	if entry.kind != "hash" || len(entry.hash) != 59 {
		t.Fatalf("Run is not a complete 59-field hash: %d fields", len(entry.hash))
	}
	record := make(Record, len(names))
	for i, name := range names {
		value, ok := entry.hash[name]
		if !ok {
			t.Fatalf("missing field %s", name)
		}
		record[i] = textField(name, value)
	}
	if err := ValidateRecord(SchemaRun, record); err != nil {
		t.Fatalf("independent Go run codec rejected Lua post-state: %v", err)
	}
	return record
}

func runLuaSeed(t *testing.T, r *sharedLuaRedis, input CreateRunWireInput, state string) {
	t.Helper()
	record := recordAuthorityRunRecord(t, state)
	// Fixture timestamps are translated, not derived from any Lua operation.
	for i, field := range record {
		if strings.HasSuffix(field.Name, "_ms") && string(field.Value) != "0" {
			value, err := strconv.ParseUint(string(field.Value), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			record[i].Value = []byte(strconv.FormatUint(bootLuaNow-1000+value, 10))
		}
	}
	record[runSourceKindIndex].Value = []byte(input.SourceKind)
	record[runPolicyGroupMapSHA256Index].Value = []byte(input.PolicyGroupMapSHA256)
	record[runAuthorizationExpiresAtMSIndex].Value = []byte(strconv.FormatUint(input.AuthorizationExpiresAtMS, 10))
	if state == "archived" {
		record[runArchivedAtMSIndex].Value = []byte(strconv.FormatUint(bootLuaNow-400+2592000000, 10))
		r.now = bootLuaNow + 2592000000
	}
	r.setHash(runLuaKey(""), record)
	v := r.data[runLuaKey("")].hash
	n := func(name string) int { value, _ := strconv.Atoi(v[name]); return value }
	id := string(input.PolicyGroups[0].GroupID)
	groupValues := map[string]string{
		"group_limits": "10", "group_rate_scope_ids": string(input.PolicyGroups[0].RateScopeID), "group_scope_ids": string(input.PolicyGroups[0].GroupScopeID),
		"group_concurrency": "2", "group_interval_ms": "1000", "group_started": v["request_starts"], "group_pending": v["pending_request_reservations"],
		"group_active_started": v["started_request_reservations"], "group_open_jobs": v["open_job_count"],
	}
	for key, value := range groupValues {
		runLuaCollection(r, runLuaKey(key), "hash", map[string]string{id: value})
	}
	if n("audit_revision") > 0 {
		runLuaCollection(r, runLuaKey("audit_group_counts"), "hash", map[string]string{id: v["audit_count"]})
	}
	for key, names := range map[string][]string{"retry_reason_counts": runLuaRetryNames, "recovery_outcome_counts": runLuaRecoveryNames, "disposition_reason_counts": runLuaDispositionNames} {
		fields := map[string]string{}
		for _, name := range names {
			fields[name] = "0"
		}
		runLuaCollection(r, runLuaKey(key), "hash", fields)
	}
	disposition := r.data[runLuaKey("disposition_reason_counts")].hash
	disposition["published"] = v["output_commits_total"]
	disposition["already_visited"] = strconv.Itoa(n("completed_total") - n("output_commits_total"))
	disposition["policy_denied"] = v["dead_total"]
	disposition["operator_cancelled"] = v["cancelled_total"]
	jobs, order := map[string]string{}, map[string]string{}
	index := 0
	for _, item := range []struct{ key, field string }{{"ready", "open_job_count"}, {"completed", "completed_total"}, {"dead", "dead_total"}, {"cancelled", "cancelled_total"}} {
		members := map[string]string{}
		for i := 0; i < n(item.field); i++ {
			index++
			job := fmt.Sprintf("%064x", index)
			jobs[job], order[job], members[job] = "1", "0", strconv.FormatUint(bootLuaNow-500, 10)
			if item.key == "ready" {
				members[job] = "1"
			}
		}
		runLuaCollection(r, runLuaKey(item.key), "zset", members)
		if item.key == "ready" {
			ages := map[string]string{}
			for job := range members {
				ages[job] = strconv.FormatUint(bootLuaNow-500, 10)
			}
			runLuaCollection(r, runLuaKey("ready_at"), "zset", ages)
		}
	}
	runLuaCollection(r, runLuaKey("jobs"), "set", jobs)
	runLuaCollection(r, runLuaKey("job_order"), "zset", order)
	runLuaCollection(r, "mifolyo:crawl:v2:runs", "zset", map[string]string{runLuaID: v["created_at_ms"]})
	if n("finalized_at_ms") == 0 {
		runLuaCollection(r, "mifolyo:crawl:v2:active_runs", "set", map[string]string{runLuaID: "1"})
	}
	if state != "archived" {
		runLuaCollection(r, "mifolyo:crawl:v2:unarchived_runs", "set", map[string]string{runLuaID: "1"})
	}
	runLuaRecord(t, r)
}

func runLuaTrace(t *testing.T, r *sharedLuaRedis) {
	t.Helper()
	writes, times := 0, 0
	var acl []bootLuaCommand
	for _, call := range r.trace {
		write := call.name == "HSET" || call.name == "ZADD" || call.name == "SADD" || call.name == "SREM"
		if writes > 0 && (!write || call.acl) {
			t.Fatalf("read/ACL after first write: %s", call.name)
		}
		if call.acl {
			acl = append(acl, call)
			continue
		}
		if write {
			if writes >= len(acl) || acl[writes].name != call.name || !reflect.DeepEqual(acl[writes].args, call.args) {
				t.Fatal("write not covered by exact prior ACL descriptor")
			}
			writes++
		}
		if call.name == "TIME" {
			times++
		}
		if call.name == "SCAN" || call.name == "HSCAN" || call.name == "HGETALL" || call.name == "KEYS" {
			t.Fatal("unbounded or untyped read")
		}
		if (call.name == "ZRANGE" || call.name == "SMEMBERS") && len(call.args) > 0 && (call.args[0] == runLuaKey("jobs") || call.args[0] == runLuaKey("job_order") || call.args[0] == runLuaKey("ready")) {
			t.Fatal("run-only operation scanned job inventory")
		}
	}
	if times != 1 || len(r.trace) == 0 || r.trace[0].name != "TIME" {
		t.Fatal("TIME is not the unique first Redis call")
	}
	if writes > 0 && !r.returnedPrebuilt {
		t.Fatal("response was not preconstructed before ACL/write phase")
	}
}

func runLuaReject(t *testing.T, r *sharedLuaRedis, op OperationName, keys, args []string, expected ErrorCode) {
	t.Helper()
	runLuaRejectSource(t, r, runLuaSource(t, op), keys, args, expected)
}

func runLuaRejectSource(t *testing.T, r *sharedLuaRedis, source string, keys, args []string, expected ErrorCode) {
	t.Helper()
	before := r.snapshot()
	result := sharedLuaRun(t, r, source, keys, args)
	if result.runtimeErr != nil {
		t.Fatalf("expected closed prewrite error, got %v", result.runtimeErr)
	}
	errorReply, ok := result.raw.(bootLuaErrorReply)
	if !ok {
		t.Fatalf("expected rejection, got %v", result.raw)
	}
	code, err := ParseErrorCode(strings.TrimPrefix(string(errorReply), "ERR CRAWL_V2_"))
	if err != nil || expected != "" && code != expected {
		t.Fatalf("error %s, expected %s", errorReply, expected)
	}
	if !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("rejection mutated entire Redis snapshot")
	}
	runLuaTrace(t, r)
}

func runLuaReplay(t *testing.T, r *sharedLuaRedis, op OperationName, keys, args []string, status string, tail ...string) {
	t.Helper()
	r.now += 100
	r.maximum, r.denyAt = 1, 1
	before := r.snapshot()
	runLuaReply(t, r, op, keys, args, status, tail...)
	if !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("exact replay mutated entire Redis snapshot/expiry/accounting")
	}
	for _, call := range r.trace {
		if call.acl || call.name == "INFO" && call.args[0] == "MEMORY" {
			t.Fatal("replay entered mutation admission")
		}
	}
	r.maximum, r.denyAt = 400*1024*1024, 0
}

func TestRunLuaCreateAndReplay(t *testing.T) {
	t.Parallel()
	for _, candidate := range []bool{false, true} {
		t.Run(fmt.Sprintf("candidate=%t", candidate), func(t *testing.T) {
			r, a, input := runLuaFixture(t, candidate)
			request, err := NewCreateRunWireRequest(runLuaGate(t, a, OperationCreateRun, candidate), input)
			keys, args := runLuaParts(t, request, err)
			before := r.snapshot()
			runLuaReply(t, r, OperationCreateRun, keys, args, "CREATED", runLuaID)
			runLuaRecord(t, r)
			if r.writes-before.writes != 16 {
				t.Fatalf("CREATE wrote %d descriptors, expected 16", r.writes-before.writes)
			}
			v := r.data[runLuaKey("")].hash
			if v["state"] != "loading" || v["created_at_ms"] != strconv.FormatUint(bootLuaNow, 10) || v["load_revision"] != "1" || v["job_count"] != "0" {
				t.Fatal("wrong CREATE post-state")
			}
			if _, ok := r.data[runLuaKey("audit_group_counts")]; ok {
				t.Fatal("CREATE prematurely created audit map")
			}
			r.now = input.AuthorizationExpiresAtMS + 1 // replay before new lifetime admission
			runLuaReplay(t, r, OperationCreateRun, keys, args, "EXISTS_IDENTICAL", runLuaID)
			changed := append([]string(nil), args...)
			changed[9] = strings.Repeat("b", 64)
			runLuaReject(t, r, OperationCreateRun, keys, changed, ErrorImmutableMismatch)
		})
	}
}

func TestRunLuaBeginSealActivateCancelAndReplay(t *testing.T) {
	t.Parallel()
	for _, candidate := range []bool{false, true} {
		for _, op := range []OperationName{OperationBeginRunAudit, OperationSealRun, OperationActivateRun, OperationCancelRun} {
			if candidate && op == OperationActivateRun {
				continue
			}
			t.Run(fmt.Sprintf("%s/candidate=%t", op, candidate), func(t *testing.T) {
				r, a, input := runLuaFixture(t, candidate)
				gate := runLuaGate(t, a, op, candidate)
				var request OperationWireRequest
				var err error
				var status string
				var tail []string
				switch op {
				case OperationBeginRunAudit:
					runLuaSeed(t, r, input, "loading")
					request, err = NewBeginRunAuditWireRequest(gate, input.RunID)
					status, tail = "AUDIT_STARTED", []string{"2", "2"}
				case OperationSealRun:
					runLuaSeed(t, r, input, "auditing")
					v := r.data[runLuaKey("")].hash
					v["audit_count"], v["audit_complete"] = "2", "1"
					r.data[runLuaKey("audit_group_counts")].hash[string(input.PolicyGroups[0].GroupID)] = "2"
					request, err = NewSealRunWireRequest(gate, SealRunWireInput{RunID: input.RunID, ExpectedJobCount: 2, SourceSHA256: input.SourceSHA256})
					status, tail = "SEALED", []string{"2", string(input.SourceSHA256)}
				case OperationActivateRun:
					runLuaSeed(t, r, input, "sealed")
					request, err = NewActivateRunWireRequest(gate, ActivateRunWireInput{RunID: input.RunID, SourceSHA256: input.SourceSHA256, AuthorizationSHA256: input.AuthorizationSHA256, CrawlPolicySHA256: input.CrawlPolicySHA256, RenderPolicySHA256: input.RenderPolicySHA256, CanonicalizationSHA256: input.CanonicalizationSHA256})
					status, tail = "ACTIVATED", []string{strconv.FormatUint(r.now, 10)}
				case OperationCancelRun:
					runLuaSeed(t, r, input, "auditing")
					request, err = NewCancelRunWireRequest(gate, CancelRunWireInput{RunID: input.RunID, Reason: ReasonOperatorCancelled})
					status, tail = "CANCELLED", []string{strconv.FormatUint(r.now, 10), "operator_cancelled"}
				}
				keys, args := runLuaParts(t, request, err)
				runLuaReply(t, r, op, keys, args, status, tail...)
				runLuaRecord(t, r)
				if op == OperationActivateRun {
					r.now = input.AuthorizationExpiresAtMS + 1
				}
				runLuaReplay(t, r, op, keys, args, "EXISTS_IDENTICAL", tail...)
			})
		}
	}
}

func TestRunLuaFinalizePrecedenceAndImmutableReplay(t *testing.T) {
	t.Parallel()
	for _, branch := range []string{"completed", "request", "creation", "groups", "cancelled", "not_due", "cancel_draining"} {
		t.Run(branch, func(t *testing.T) {
			r, a, input := runLuaFixture(t, false)
			state := "active"
			if branch == "cancelled" || branch == "cancel_draining" {
				state = "cancelled"
			}
			runLuaSeed(t, r, input, state)
			v := r.data[runLuaKey("")].hash
			id := string(input.PolicyGroups[0].GroupID)
			status, reason := "NOT_DUE", "none"
			if state == "cancelled" {
				v["finalized_at_ms"], v["retention_anchor_ms"] = "0", "0"
				r.setSet("mifolyo:crawl:v2:active_runs", []string{runLuaID})
				status, reason = "CANCELLED", "operator_cancelled"
			}
			switch branch {
			case "completed":
				// Every budget is exhausted too. Completion MUST win.
				v["open_job_count"], v["completed_total"] = "0", "2"
				v["request_starts"], v["reservation_creations_total"] = "10", "100"
				r.data[runLuaKey("group_started")].hash[id] = "10"
				r.data[runLuaKey("group_open_jobs")].hash[id] = "0"
				r.data[runLuaKey("disposition_reason_counts")].hash["already_visited"] = "1"
				r.removeKey(runLuaKey("ready"))
				r.removeKey(runLuaKey("ready_at"))
				r.zsets[runLuaKey("completed")][fmt.Sprintf("%064x", 1)] = float64(r.now - 500)
				status, reason = "COMPLETED", "all_jobs_terminal"
			case "request":
				v["request_starts"], v["reservation_creations_total"] = "10", "100"
				r.data[runLuaKey("group_started")].hash[id] = "10"
				status, reason = "RUN_BUDGET_EXHAUSTED", "request_budget_exhausted"
			case "creation":
				v["reservation_creations_total"] = "100"
				status, reason = "RUN_RESERVATION_LIMIT_EXHAUSTED", "reservation_limit_exhausted"
			case "groups":
				// Exhaust the only open group below both run-wide limits.
				input.PolicyGroups[0].RequestStartLimit = 2
				digest, err := DerivePolicyGroupMapDigest(input.PolicyGroups)
				if err != nil {
					t.Fatal(err)
				}
				v["policy_group_map_sha256"] = string(digest)
				r.data[runLuaKey("group_limits")].hash[id] = "2"
				status, reason = "GROUP_BUDGET_EXHAUSTED", "group_budgets_exhausted"
			case "cancel_draining":
				v["open_job_count"], v["cancelled_total"], v["request_starts"], v["reservation_creations_total"] = "1", "0", "10", "100"
				r.data[runLuaKey("group_started")].hash[id] = "10"
				r.data[runLuaKey("group_open_jobs")].hash[id] = "1"
				r.data[runLuaKey("disposition_reason_counts")].hash["operator_cancelled"] = "0"
				r.removeKey(runLuaKey("cancelled"))
				r.setZSet(runLuaKey("ready"), map[string]float64{fmt.Sprintf("%064x", 3): 1})
				r.setZSet(runLuaKey("ready_at"), map[string]float64{fmt.Sprintf("%064x", 3): float64(r.now - 500)})
				status, reason = "NOT_DUE", "none"
			}
			runLuaRecord(t, r)
			request, err := NewFinalizeRunWireRequest(runLuaGate(t, a, OperationFinalizeRun, false), input.RunID)
			keys, args := runLuaParts(t, request, err)
			at := strconv.FormatUint(r.now, 10)
			if status == "NOT_DUE" {
				at = "0"
			}
			before := r.snapshot()
			runLuaReply(t, r, OperationFinalizeRun, keys, args, status, at, reason)
			runLuaRecord(t, r)
			if status == "NOT_DUE" {
				if !reflect.DeepEqual(before, r.snapshot()) {
					t.Fatal("NOT_DUE mutated state")
				}
			} else {
				if r.sets["mifolyo:crawl:v2:active_runs"][runLuaID] || r.data[runLuaKey("")].hash["retention_anchor_ms"] != at {
					t.Fatal("finalization failed to close inventory/retention")
				}
			}
			runLuaReplay(t, r, OperationFinalizeRun, keys, args, status, at, reason)
		})
	}
}

func TestRunLuaArchiveRetentionStoppedDrainAndReplay(t *testing.T) {
	t.Parallel()
	r, a, input := runLuaFixture(t, false)
	runLuaSeed(t, r, input, "completed")
	v := r.data[runLuaKey("")].hash
	anchor, _ := strconv.ParseUint(v["retention_anchor_ms"], 10, 64)
	digest := Digest(strings.Repeat("f", 64))
	request, err := NewArchiveRunWireRequest(runLuaGate(t, a, OperationArchiveRun, false), ArchiveRunWireInput{RunID: input.RunID, ArchiveSHA256: digest})
	keys, args := runLuaParts(t, request, err)
	r.now = anchor + 2592000000 - 1
	before := r.snapshot()
	runLuaReply(t, r, OperationArchiveRun, keys, args, "NOT_DUE", strconv.FormatUint(anchor+2592000000, 10))
	if !reflect.DeepEqual(before, r.snapshot()) {
		t.Fatal("premature archive changed evidence")
	}
	r.now++
	// Unrelated dead-letter entries must not be discarded or confused with a
	// global queue drain; the operator export certifies this run's reconciliation.
	r.setList("pages_queue:dead", []string{"page_data:other-run:evidence"})
	r.setList("image_indexer_queue:dead", []string{"page_images:other-run:evidence"})
	at := strconv.FormatUint(r.now, 10)
	runLuaReply(t, r, OperationArchiveRun, keys, args, "ARCHIVED", at, string(digest))
	runLuaRecord(t, r)
	if r.data[runLuaKey("")].hash["retention_anchor_ms"] != strconv.FormatUint(anchor, 10) || r.sets["mifolyo:crawl:v2:unarchived_runs"][runLuaID] {
		t.Fatal("archive reopened retention or retained unarchived membership")
	}
	// Producer liveness is not a new requirement for a no-write archive replay.
	r.data["pages_queue:indexer_owner"] = bootLuaEntry{kind: "string", value: "resumed-owner", expireAt: -1}
	runLuaReplay(t, r, OperationArchiveRun, keys, args, "EXISTS_IDENTICAL", at, string(digest))
	changed := append([]string(nil), args...)
	changed[8], changed[9] = strings.Repeat("e", 64), runLuaID+":"+strings.Repeat("e", 64)
	runLuaReject(t, r, OperationArchiveRun, keys, changed, ErrorImmutableMismatch)
}

var runLuaOperations = []OperationName{OperationCreateRun, OperationBeginRunAudit, OperationSealRun, OperationActivateRun, OperationCancelRun, OperationFinalizeRun, OperationArchiveRun}

func runLuaOperationCase(t *testing.T, op OperationName) (*sharedLuaRedis, []string, []string) {
	t.Helper()
	r, a, input := runLuaFixture(t, false)
	gate := runLuaGate(t, a, op, false)
	var request OperationWireRequest
	var err error
	switch op {
	case OperationCreateRun:
		request, err = NewCreateRunWireRequest(gate, input)
	case OperationBeginRunAudit:
		runLuaSeed(t, r, input, "loading")
		request, err = NewBeginRunAuditWireRequest(gate, input.RunID)
	case OperationSealRun:
		runLuaSeed(t, r, input, "auditing")
		v := r.data[runLuaKey("")].hash
		v["audit_count"], v["audit_complete"] = "2", "1"
		r.data[runLuaKey("audit_group_counts")].hash[string(input.PolicyGroups[0].GroupID)] = "2"
		request, err = NewSealRunWireRequest(gate, SealRunWireInput{RunID: input.RunID, SourceSHA256: input.SourceSHA256, ExpectedJobCount: 2})
	case OperationActivateRun:
		runLuaSeed(t, r, input, "sealed")
		request, err = NewActivateRunWireRequest(gate, ActivateRunWireInput{RunID: input.RunID, SourceSHA256: input.SourceSHA256, AuthorizationSHA256: input.AuthorizationSHA256, CrawlPolicySHA256: input.CrawlPolicySHA256, RenderPolicySHA256: input.RenderPolicySHA256, CanonicalizationSHA256: input.CanonicalizationSHA256})
	case OperationCancelRun:
		runLuaSeed(t, r, input, "active")
		request, err = NewCancelRunWireRequest(gate, CancelRunWireInput{RunID: input.RunID, Reason: ReasonOperatorCancelled})
	case OperationFinalizeRun:
		runLuaSeed(t, r, input, "active")
		v := r.data[runLuaKey("")].hash
		v["request_starts"], v["reservation_creations_total"] = "10", "10"
		r.data[runLuaKey("group_started")].hash[string(input.PolicyGroups[0].GroupID)] = "10"
		request, err = NewFinalizeRunWireRequest(gate, input.RunID)
	case OperationArchiveRun:
		runLuaSeed(t, r, input, "completed")
		r.now += 2592000000
		request, err = NewArchiveRunWireRequest(gate, ArchiveRunWireInput{RunID: input.RunID, ArchiveSHA256: Digest(strings.Repeat("f", 64))})
	default:
		t.Fatal("unrecognized test operation")
	}
	keys, args := runLuaParts(t, request, err)
	return r, keys, args
}

func TestRunLuaAllHandlersWireAndGateRejections(t *testing.T) {
	t.Parallel()
	for _, op := range runLuaOperations {
		for _, mutation := range []string{"missing_arg", "extra_arg", "bad_id", "wrong_key", "missing_key", "wrong_boot", "wrong_contract", "authority_drift"} {
			t.Run(string(op)+"/"+mutation, func(t *testing.T) {
				r, keys, args := runLuaOperationCase(t, op)
				switch mutation {
				case "missing_arg":
					args = args[:len(args)-1]
				case "extra_arg":
					args = append(args, "extra")
				case "bad_id":
					args[7] = "../" + runLuaID
				case "wrong_key":
					keys[len(keys)-1] += ":not-the-key"
				case "missing_key":
					keys = keys[:len(keys)-1]
				case "wrong_boot":
					r.runID = strings.Repeat("e", 40)
				case "wrong_contract":
					r.data[CrawlContractKey] = bootLuaEntry{kind: "string", value: strings.Repeat("d", 64), expireAt: -1}
				case "authority_drift":
					r.data[CommitGuardKey].hash["maximum_shape_sha256"] = strings.Repeat("e", 64)
				}
				runLuaReject(t, r, op, keys, args, "")
			})
		}
	}
}

func TestRunLuaAllHandlersRejectPartialAndCorruptLedgers(t *testing.T) {
	t.Parallel()
	mutations := []struct {
		name string
		edit func(*sharedLuaRedis)
	}{
		{"missing_run_field", func(r *sharedLuaRedis) { delete(r.data[runLuaKey("")].hash, "last_execution_at_ms") }},
		{"extra_run_id", func(r *sharedLuaRedis) { r.data[runLuaKey("")].hash["run_id"] = runLuaID }},
		{"noncanonical_counter", func(r *sharedLuaRedis) { r.data[runLuaKey("")].hash["claims_total"] = "00" }},
		{"wrong_contract_binding", func(r *sharedLuaRedis) { r.data[runLuaKey("")].hash["contract_sha256"] = strings.Repeat("f", 64) }},
		{"partial_purge", func(r *sharedLuaRedis) { r.data[runLuaKey("")].hash["purge_state"] = "in_progress" }},
		{"missing_group_map", func(r *sharedLuaRedis) { r.removeKey(runLuaKey("group_pending")) }},
		{"extra_group", func(r *sharedLuaRedis) { r.data[runLuaKey("group_open_jobs")].hash["not-installed"] = "0" }},
		{"group_open_sum", func(r *sharedLuaRedis) { r.data[runLuaKey("group_open_jobs")].hash["research/é"] = "9999" }},
		{"group_started_sum", func(r *sharedLuaRedis) { r.data[runLuaKey("group_started")].hash["research/é"] = "1" }},
		{"group_scope_derived", func(r *sharedLuaRedis) {
			r.data[runLuaKey("group_scope_ids")].hash["research/é"] = strings.Repeat("f", 64)
		}},
		{"group_limit_digest", func(r *sharedLuaRedis) { r.data[runLuaKey("group_limits")].hash["research/é"] = "9" }},
		{"missing_reason_field", func(r *sharedLuaRedis) { delete(r.data[runLuaKey("retry_reason_counts")].hash, "http_429") }},
		{"extra_reason_field", func(r *sharedLuaRedis) { r.data[runLuaKey("disposition_reason_counts")].hash["invented"] = "0" }},
		{"retry_reason_sum", func(r *sharedLuaRedis) { r.data[runLuaKey("retry_reason_counts")].hash["request_timeout"] = "1" }},
		{"recovery_reason_sum", func(r *sharedLuaRedis) { r.data[runLuaKey("recovery_outcome_counts")].hash["ready"] = "1" }},
		{"disposition_class_sum", func(r *sharedLuaRedis) { r.data[runLuaKey("disposition_reason_counts")].hash["source_cancelled"] = "1" }},
		{"jobs_inventory_missing", func(r *sharedLuaRedis) { r.removeKey(runLuaKey("jobs")) }},
		{"order_inventory_missing", func(r *sharedLuaRedis) { r.removeKey(runLuaKey("job_order")) }},
		{"wrong_secondary_type", func(r *sharedLuaRedis) { r.data[runLuaKey("ready_at")] = bootLuaEntry{kind: "string", value: "bad"} }},
		{"wrong_visited_type", func(r *sharedLuaRedis) {
			r.data[runLuaKey("visited_urls")] = bootLuaEntry{kind: "string", value: "bad"}
		}},
		{"visited_pair_count", func(r *sharedLuaRedis) {
			r.data[runLuaKey("visited_depth")] = bootLuaEntry{kind: "hash", hash: map[string]string{strings.Repeat("e", 64): "0"}}
		}},
		{"missing_runs_membership", func(r *sharedLuaRedis) { r.removeKey("mifolyo:crawl:v2:runs") }},
		{"wrong_creation_score", func(r *sharedLuaRedis) { r.zsets["mifolyo:crawl:v2:runs"][runLuaID]-- }},
		{"wrong_active_membership", func(r *sharedLuaRedis) {
			if r.sets["mifolyo:crawl:v2:active_runs"][runLuaID] {
				r.removeKey("mifolyo:crawl:v2:active_runs")
			} else {
				r.setSet("mifolyo:crawl:v2:active_runs", []string{runLuaID})
			}
		}},
		{"missing_unarchived_membership", func(r *sharedLuaRedis) { r.removeKey("mifolyo:crawl:v2:unarchived_runs") }},
	}
	for _, op := range runLuaOperations {
		for _, mutation := range mutations {
			t.Run(string(op)+"/"+mutation.name, func(t *testing.T) {
				r, keys, args := runLuaOperationCase(t, op)
				if op == OperationCreateRun {
					// Existing CREATE replay must validate the complete progressed
					// ledger, not just the immutable original request fields.
					_, _, input := runLuaFixture(t, false)
					runLuaSeed(t, r, input, "loading")
				}
				mutation.edit(r)
				runLuaReject(t, r, op, keys, args, "")
			})
		}
	}
}

func TestRunLuaAllHandlersPreflightAndPartialWriteFailure(t *testing.T) {
	t.Parallel()
	for _, op := range runLuaOperations {
		t.Run(string(op), func(t *testing.T) {
			r, keys, args := runLuaOperationCase(t, op)
			before := r.writes
			sharedLuaNoError(t, sharedLuaRun(t, r, runLuaSource(t, op), keys, args))
			calls := r.writes - before
			if calls < 1 {
				t.Fatal("fixture failed to exercise a mutation")
			}
			runLuaTrace(t, r)
			r, keys, args = runLuaOperationCase(t, op)
			r.denyAt = calls // even the LAST descriptor must fail before ALL writes
			runLuaReject(t, r, op, keys, args, "")
			if r.aclCount != calls || r.attempts != 0 {
				t.Fatal("last ACL denial did not preflight the whole plan")
			}
			r, keys, args = runLuaOperationCase(t, op)
			r.maximum = 1
			runLuaReject(t, r, op, keys, args, ErrorMemoryHeadroomLow)
			for _, after := range []bool{false, true} {
				r, keys, args = runLuaOperationCase(t, op)
				original := r.snapshot()
				r.failAt, r.failAfter = calls, after
				result := sharedLuaRun(t, r, runLuaSource(t, op), keys, args)
				if result.runtimeErr == nil || !strings.Contains(result.runtimeErr.Error(), bootLuaWriteErr) {
					t.Fatalf("unexpected executor failure swallowed: %v", result)
				}
				wantWrites := calls - 1
				if after {
					wantWrites++
				}
				if r.writes-original.writes != wantWrites {
					t.Fatal("executor rolled back or performed extra writes")
				}
				if wantWrites > 0 && reflect.DeepEqual(original, r.snapshot()) {
					t.Fatal("partial writes were incorrectly hidden")
				}
			}
		})
	}
}

func TestRunLuaAuthorizationAndCancellationSafetyFloor(t *testing.T) {
	t.Parallel()
	for _, offset := range []int64{-1, 0, 60000, 86400000, 86400001} {
		r, a, input := runLuaFixture(t, false)
		input.AuthorizationExpiresAtMS = uint64(int64(r.now) + offset)
		request, err := NewCreateRunWireRequest(runLuaGate(t, a, OperationCreateRun, false), input)
		keys, args := runLuaParts(t, request, err)
		if offset > 0 && offset <= 86400000 {
			runLuaReply(t, r, OperationCreateRun, keys, args, "CREATED", runLuaID)
		} else {
			runLuaReject(t, r, OperationCreateRun, keys, args, ErrorInvalidArgument)
		}
	}
	for _, remaining := range []uint64{0, 59999, 60000} {
		r, keys, args := runLuaOperationCase(t, OperationActivateRun)
		r.data[runLuaKey("")].hash["authorization_expires_at_ms"] = strconv.FormatUint(r.now+remaining, 10)
		if remaining == 60000 {
			runLuaReply(t, r, OperationActivateRun, keys, args, "ACTIVATED", strconv.FormatUint(r.now, 10))
		} else {
			runLuaReject(t, r, OperationActivateRun, keys, args, ErrorInvalidState)
		}
	}
	for _, state := range []string{"loading", "auditing", "sealed", "active"} {
		t.Run("cancel-"+state, func(t *testing.T) {
			r, a, input := runLuaFixture(t, false)
			runLuaSeed(t, r, input, state)
			gate := runLuaGate(t, a, OperationCancelRun, false)
			request, err := NewAuthorizationExpiredCancelRunWireRequest(gate, input.RunID)
			keys, args := runLuaParts(t, request, err)
			r.now = input.AuthorizationExpiresAtMS - 1
			runLuaReject(t, r, OperationCancelRun, keys, args, ErrorInvalidArgument)
			r.now++
			at := strconv.FormatUint(r.now, 10)
			runLuaReply(t, r, OperationCancelRun, keys, args, "CANCELLED", at, "authorization_expired")
			runLuaRecord(t, r)
			runLuaReplay(t, r, OperationCancelRun, keys, args, "EXISTS_IDENTICAL", at, "authorization_expired")
			args[8] = "operator_cancelled"
			runLuaReject(t, r, OperationCancelRun, keys, args, ErrorImmutableMismatch)
		})
	}
	for _, short := range []uint64{0, 1} {
		r, keys, args := runLuaOperationCase(t, OperationCancelRun)
		// Independently count complete replacement value bytes. Existing fields
		// and key incur no name/element/key allocation; no deletion credits.
		growth := uint64(3 * (len("cancelled") + len("operator_cancelled") + 2*len(strconv.FormatUint(r.now, 10))))
		r.maximum = r.used + 67108864 + growth - short
		if short == 1 {
			runLuaReject(t, r, OperationCancelRun, keys, args, ErrorMemoryHeadroomLow)
		} else {
			runLuaReply(t, r, OperationCancelRun, keys, args, "CANCELLED", strconv.FormatUint(r.now, 10), "operator_cancelled")
			if r.attempts != 1 {
				t.Fatal("safety cancellation wrote jobs/indexes or more than one existing-run HSET")
			}
		}
	}
	// Same-millisecond cancellation still supplies all four safety fields. An
	// identical last_activity value costs zero, but is not omitted from the plan.
	r, keys, args := runLuaOperationCase(t, OperationCancelRun)
	r.data[runLuaKey("")].hash["last_activity_at_ms"] = strconv.FormatUint(r.now, 10)
	runLuaReply(t, r, OperationCancelRun, keys, args, "CANCELLED", strconv.FormatUint(r.now, 10), "operator_cancelled")
}

func TestRunLuaEmptyMongoAndCandidateMinimum(t *testing.T) {
	t.Parallel()
	r, a, input := runLuaFixture(t, false)
	input.ExpectedSeedCount = 0
	request, err := NewCreateRunWireRequest(runLuaGate(t, a, OperationCreateRun, false), input)
	keys, args := runLuaParts(t, request, err)
	runLuaReply(t, r, OperationCreateRun, keys, args, "CREATED", runLuaID)
	runLuaRecord(t, r)
	request, err = NewBeginRunAuditWireRequest(runLuaGate(t, a, OperationBeginRunAudit, false), input.RunID)
	keys, args = runLuaParts(t, request, err)
	runLuaReply(t, r, OperationBeginRunAudit, keys, args, "AUDIT_STARTED", "1", "0")
	// AUDIT_RUN_BATCH is NOT implemented by these run-only fragments. Seed its
	// independently validated complete empty-audit outcome to exercise SEAL.
	r.data[runLuaKey("")].hash["audit_complete"] = "1"
	runLuaRecord(t, r)
	request, err = NewSealRunWireRequest(runLuaGate(t, a, OperationSealRun, false), SealRunWireInput{RunID: input.RunID, SourceSHA256: input.SourceSHA256})
	keys, args = runLuaParts(t, request, err)
	runLuaReply(t, r, OperationSealRun, keys, args, "SEALED", "0", string(input.SourceSHA256))
	request, err = NewActivateRunWireRequest(runLuaGate(t, a, OperationActivateRun, false), ActivateRunWireInput{RunID: input.RunID, SourceSHA256: input.SourceSHA256, AuthorizationSHA256: input.AuthorizationSHA256, CrawlPolicySHA256: input.CrawlPolicySHA256, RenderPolicySHA256: input.RenderPolicySHA256, CanonicalizationSHA256: input.CanonicalizationSHA256})
	keys, args = runLuaParts(t, request, err)
	runLuaReply(t, r, OperationActivateRun, keys, args, "ACTIVATED", strconv.FormatUint(r.now, 10))
	request, err = NewFinalizeRunWireRequest(runLuaGate(t, a, OperationFinalizeRun, false), input.RunID)
	keys, args = runLuaParts(t, request, err)
	runLuaReply(t, r, OperationFinalizeRun, keys, args, "COMPLETED", strconv.FormatUint(r.now, 10), "all_jobs_terminal")
	runLuaRecord(t, r)
	r.now += 2592000000
	request, err = NewArchiveRunWireRequest(runLuaGate(t, a, OperationArchiveRun, false), ArchiveRunWireInput{RunID: input.RunID, ArchiveSHA256: Digest(strings.Repeat("f", 64))})
	keys, args = runLuaParts(t, request, err)
	runLuaReply(t, r, OperationArchiveRun, keys, args, "ARCHIVED", strconv.FormatUint(r.now, 10), args[8])
	runLuaRecord(t, r)
	r, a, input = runLuaFixture(t, true)
	request, err = NewCreateRunWireRequest(runLuaGate(t, a, OperationCreateRun, true), input)
	keys, args = runLuaParts(t, request, err)
	args[10] = "0" // hostile wire bypasses the independent Go constructor
	runLuaReject(t, r, OperationCreateRun, keys, args, ErrorInvalidArgument)
}

func TestRunLuaCreateInventoryCapsAndPartialAbsence(t *testing.T) {
	t.Parallel()
	for _, capacity := range []struct{ total, active, unarchived int }{{128, 0, 0}, {16, 16, 16}, {100, 0, 100}} {
		r, keys, args := runLuaOperationCase(t, OperationCreateRun)
		runs, active, unarchived := map[string]float64{}, []string{}, []string{}
		for i := 0; i < capacity.total; i++ {
			id := fmt.Sprintf("%032x", i+256)
			runs[id] = float64(r.now - 1000)
			if i < capacity.active {
				active = append(active, id)
			}
			if i < capacity.unarchived {
				unarchived = append(unarchived, id)
			}
		}
		r.setZSet("mifolyo:crawl:v2:runs", runs)
		r.setSet("mifolyo:crawl:v2:active_runs", active)
		r.setSet("mifolyo:crawl:v2:unarchived_runs", unarchived)
		runLuaReject(t, r, OperationCreateRun, keys, args, ErrorLimitExceeded)
	}
	for _, suffix := range append(append([]string{}, runLuaMapNames...), "audit_group_counts", "visited_urls", "retry_reason_counts", "disposition_reason_counts") {
		r, keys, args := runLuaOperationCase(t, OperationCreateRun)
		r.data[runLuaKey(suffix)] = bootLuaEntry{kind: "hash", hash: map[string]string{"partial": "0"}, expireAt: -1}
		runLuaReject(t, r, OperationCreateRun, keys, args, ErrorInvalidState)
	}
}

func TestRunLuaStoppedArchiveRejectsEveryUndrainedOptionalKey(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"pages_queue", "pages_queue:processing", "image_indexer_queue", "image_indexer_queue:processing", "pages_queue:indexer_owner", "image_indexer_queue:owner", "mifolyo:crawl:v2:stage_expiry"} {
		t.Run(key, func(t *testing.T) {
			r, keys, args := runLuaOperationCase(t, OperationArchiveRun)
			switch {
			case strings.Contains(key, "owner"):
				r.data[key] = bootLuaEntry{kind: "string", value: "live-owner", expireAt: -1}
			case strings.HasSuffix(key, "stage_expiry"):
				r.setZSet(key, map[string]float64{strings.Repeat("e", 64): float64(r.now - 1)})
			default:
				r.setList(key, []string{"remaining-work"})
			}
			runLuaReject(t, r, OperationArchiveRun, keys, args, "")
		})
	}
	for _, key := range []string{"pages_queue:dead", "image_indexer_queue:dead", "pages_queue:indexer_owner", "image_indexer_queue:owner"} {
		r, keys, args := runLuaOperationCase(t, OperationArchiveRun)
		r.data[key] = bootLuaEntry{kind: "hash", hash: map[string]string{"wrong": "type"}}
		runLuaReject(t, r, OperationArchiveRun, keys, args, ErrorWrongType)
	}
}

func TestRunLuaActivationLegacyAndMigrationBindings(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"mifolyo:crawl:v1:queue", "mifolyo:crawl:v1:urls", "mifolyo:crawl:v1:depths", "spider_queue", "signal_queue"} {
		r, keys, args := runLuaOperationCase(t, OperationActivateRun)
		r.data[key] = bootLuaEntry{kind: "string", value: "legacy-work", expireAt: -1}
		runLuaReject(t, r, OperationActivateRun, keys, args, "")
	}
	r, a, input := runLuaFixture(t, true)
	runLuaSeed(t, r, input, "sealed")
	// Independently install the Go promotion post-state. There is no promotion
	// stub or private alternate gate in the tested Lua.
	r.removeKey(ContractsCandidateKey)
	r.removeKey(CrawlContractCandidateKey)
	r.removeKey(AdminFreezeKey)
	marker, _ := a.marker.Record()
	guard, _ := a.guard.Record()
	legacy, _ := a.legacy.Record()
	r.setHash(ContractsActiveKey, marker)
	r.setHash(CommitGuardKey, guard)
	r.setHash(LegacyRetirementKey, legacy)
	r.data[CrawlContractKey] = bootLuaEntry{kind: "string", value: string(a.contract), expireAt: -1}
	request, err := NewActivateRunWireRequest(runLuaGate(t, a, OperationActivateRun, false), ActivateRunWireInput{RunID: input.RunID, SourceSHA256: input.SourceSHA256, AuthorizationSHA256: input.AuthorizationSHA256, CrawlPolicySHA256: input.CrawlPolicySHA256, RenderPolicySHA256: input.RenderPolicySHA256, CanonicalizationSHA256: input.CanonicalizationSHA256})
	keys, args := runLuaParts(t, request, err)
	runLuaReply(t, r, OperationActivateRun, keys, args, "ACTIVATED", strconv.FormatUint(r.now, 10))
	runLuaReplay(t, r, OperationActivateRun, keys, args, "EXISTS_IDENTICAL", strconv.FormatUint(bootLuaNow, 10))
}

func TestRunLuaMaximumPolicyGroupsAndExactWireMap(t *testing.T) {
	t.Parallel()
	for _, candidate := range []bool{false, true} {
		t.Run(fmt.Sprintf("candidate=%t", candidate), func(t *testing.T) {
			r, a, input := runLuaFixture(t, candidate)
			input.PolicyGroups = nil
			for i := 0; i < 64; i++ {
				lineage := RateScopeID(fmt.Sprintf("%032x", i+1))
				scope, err := DeriveGroupScopeID(lineage)
				if err != nil {
					t.Fatal(err)
				}
				input.PolicyGroups = append(input.PolicyGroups, PolicyGroup{
					GroupID: GroupID(fmt.Sprintf("g%02d/", i) + strings.Repeat("é", 62)), RateScopeID: lineage,
					GroupScopeID: scope, RequestStartLimit: uint64(i%10 + 1), Concurrency: uint64(i%32 + 1), IntervalMS: uint64(i%2) * 3600000,
				})
			}
			var err error
			input.PolicyGroupMapSHA256, err = DerivePolicyGroupMapDigest(input.PolicyGroups)
			if err != nil {
				t.Fatal(err)
			}
			request, err := NewCreateRunWireRequest(runLuaGate(t, a, OperationCreateRun, candidate), input)
			keys, args := runLuaParts(t, request, err)
			for _, bad := range []string{"unordered", "duplicate", "truncated", "extra_record", "zero_digest", "provisional_digest", "map_mismatch", "bad_scope"} {
				changed := append([]string(nil), args...)
				switch bad {
				case "unordered":
					changed[26], changed[27] = changed[27], changed[26]
				case "duplicate":
					changed[27] = changed[26]
				case "truncated":
					changed[26] = changed[26][:len(changed[26])-1]
				case "extra_record":
					changed = append(changed, changed[26])
				case "zero_digest":
					changed[20] = strings.Repeat("0", 64)
				case "provisional_digest":
					changed[20] = ZeroSHA256
				case "map_mismatch":
					changed[20] = strings.Repeat("e", 64)
				case "bad_scope":
					record, err := policyGroupRecord(input.PolicyGroups[0])
					if err != nil {
						t.Fatal(err)
					}
					record[2].Value = []byte(strings.Repeat("f", 64))
					changed[26] = string(primitiveLuaEncoded(t, record))
				}
				t.Run(bad, func(t *testing.T) { runLuaReject(t, r, OperationCreateRun, keys, changed, "") })
			}
			runLuaReply(t, r, OperationCreateRun, keys, args, "CREATED", runLuaID)
			runLuaRecord(t, r)
			for _, suffix := range runLuaMapNames {
				if len(r.data[runLuaKey(suffix)].hash) != 64 {
					t.Fatalf("incomplete 64-group map %s", suffix)
				}
			}
			runLuaReplay(t, r, OperationCreateRun, keys, args, "EXISTS_IDENTICAL", runLuaID)
			request, err = NewBeginRunAuditWireRequest(runLuaGate(t, a, OperationBeginRunAudit, candidate), input.RunID)
			keys, args = runLuaParts(t, request, err)
			runLuaReply(t, r, OperationBeginRunAudit, keys, args, "AUDIT_STARTED", "1", "0")
			if len(r.data[runLuaKey("audit_group_counts")].hash) != 64 {
				t.Fatal("audit initialization truncated the maximum group set")
			}
		})
	}
}

func TestRunLuaCandidateGateRestrictions(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationCreateRun, OperationBeginRunAudit, OperationSealRun, OperationCancelRun} {
		for _, mutation := range []string{"active_markers", "missing_freeze", "changed_freeze", "retired", "wrong_source", "executed"} {
			t.Run(string(op)+"/"+mutation, func(t *testing.T) {
				r, a, input := runLuaFixture(t, true)
				gate := runLuaGate(t, a, op, true)
				var request OperationWireRequest
				var err error
				switch op {
				case OperationCreateRun:
					runLuaSeed(t, r, input, "loading")
					request, err = NewCreateRunWireRequest(gate, input)
				case OperationBeginRunAudit:
					runLuaSeed(t, r, input, "loading")
					request, err = NewBeginRunAuditWireRequest(gate, input.RunID)
				case OperationSealRun:
					runLuaSeed(t, r, input, "sealed")
					request, err = NewSealRunWireRequest(gate, SealRunWireInput{RunID: input.RunID, ExpectedJobCount: 2, SourceSHA256: input.SourceSHA256})
				case OperationCancelRun:
					runLuaSeed(t, r, input, "sealed")
					request, err = NewCancelRunWireRequest(gate, CancelRunWireInput{RunID: input.RunID, Reason: ReasonSourceCancelled})
				}
				keys, args := runLuaParts(t, request, err)
				switch mutation {
				case "active_markers":
					marker, _ := a.marker.Record()
					r.setHash(ContractsActiveKey, marker)
				case "missing_freeze":
					r.removeKey(AdminFreezeKey)
				case "changed_freeze":
					r.data[AdminFreezeKey].hash["freeze_nonce"] = strings.Repeat("f", 32)
				case "retired":
					legacy, _ := a.legacy.Record()
					r.setHash(LegacyRetirementKey, legacy)
				case "wrong_source":
					r.data[runLuaKey("")].hash["source_kind"] = "mongo"
				case "executed":
					// A fully valid executed record must still fail the candidate
					// run-data gate, not just a conveniently malformed record.
					r.removeKey(runLuaKey("audit_group_counts"))
					runLuaSeed(t, r, input, "active")
				}
				runLuaReject(t, r, op, keys, args, "")
			})
		}
	}
	for _, op := range []OperationName{OperationActivateRun, OperationFinalizeRun, OperationArchiveRun} {
		r, keys, args := runLuaOperationCase(t, op)
		candidate, a, _ := runLuaFixture(t, true)
		gate := runLuaGate(t, a, OperationBeginRunAudit, true)
		request, err := NewBeginRunAuditWireRequest(gate, RunID(runLuaID))
		_, candidateArgs := runLuaParts(t, request, err)
		copy(args[:7], candidateArgs[:7])
		// The wire gate rejects the mode before inspecting a run or active data.
		r.data = candidate.data
		runLuaReject(t, r, op, keys, args, ErrorInvalidArgument)
	}
}

func TestRunLuaLifecycleStateAndImmutableReplayMatrix(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"loading", "auditing", "sealed", "active", "completed", "budget_exhausted", "cancelled", "archived"} {
		for _, op := range runLuaOperations {
			t.Run(state+"/"+string(op), func(t *testing.T) {
				r, _, input := runLuaFixture(t, false)
				runLuaSeed(t, r, input, state)
				_, keys, args := runLuaOperationCase(t, op)
				v := r.data[runLuaKey("")].hash
				status, tail := "", []string(nil)
				switch op {
				case OperationCreateRun:
					status, tail = "EXISTS_IDENTICAL", []string{runLuaID}
				case OperationBeginRunAudit:
					if state == "loading" {
						status, tail = "AUDIT_STARTED", []string{"2", "2"}
					} else if state == "auditing" {
						status, tail = "EXISTS_IDENTICAL", []string{"2", "2"}
					}
				case OperationSealRun:
					if state == "sealed" {
						status, tail = "EXISTS_IDENTICAL", []string{"2", string(input.SourceSHA256)}
					} // auditing fixture is intentionally incomplete
				case OperationActivateRun:
					if state == "sealed" {
						status, tail = "ACTIVATED", []string{strconv.FormatUint(r.now, 10)}
					} else if state == "active" {
						status, tail = "EXISTS_IDENTICAL", []string{v["activated_at_ms"]}
					}
				case OperationCancelRun:
					if state == "loading" || state == "auditing" || state == "sealed" || state == "active" {
						status, tail = "CANCELLED", []string{strconv.FormatUint(r.now, 10), "operator_cancelled"}
					} else if state == "cancelled" {
						status, tail = "EXISTS_IDENTICAL", []string{v["cancelled_at_ms"], "operator_cancelled"}
					}
				case OperationFinalizeRun:
					if state == "active" {
						status, tail = "NOT_DUE", []string{"0", "none"}
					} else if state == "completed" || state == "budget_exhausted" || state == "cancelled" {
						status = map[string]string{"completed": "COMPLETED", "budget_exhausted": "RUN_BUDGET_EXHAUSTED", "cancelled": "CANCELLED"}[state]
						tail = []string{v["finalized_at_ms"], v["terminal_reason"]}
					}
				case OperationArchiveRun:
					if state == "completed" || state == "budget_exhausted" || state == "cancelled" {
						anchor, _ := strconv.ParseUint(v["retention_anchor_ms"], 10, 64)
						r.now = anchor + 2592000000
						status, tail = "ARCHIVED", []string{strconv.FormatUint(r.now, 10), args[8]}
					} else if state == "archived" {
						args[8], args[9] = v["archive_sha256"], runLuaID+":"+v["archive_sha256"]
						status, tail = "EXISTS_IDENTICAL", []string{v["archived_at_ms"], v["archive_sha256"]}
					}
				}
				if status == "" {
					runLuaReject(t, r, op, keys, args, "")
					return
				}
				if status == "EXISTS_IDENTICAL" || status == "NOT_DUE" || op == OperationFinalizeRun {
					runLuaReplay(t, r, op, keys, args, status, tail...)
				} else {
					runLuaReply(t, r, op, keys, args, status, tail...)
				}
				runLuaRecord(t, r)
			})
		}
	}
}

// Fixture of one already-leased job, not an implementation of claim. The
// run-only handler must use bounded receipts; it cannot inspect a job hash.
func runLuaLiveFixture(t *testing.T, stage bool, pending bool) (*sharedLuaRedis, []string, []string) {
	t.Helper()
	r, a, input := runLuaFixture(t, false)
	runLuaSeed(t, r, input, "active")
	job := fmt.Sprintf("%064x", 1)
	deadline := r.now + 60000
	r.removeKey(runLuaKey("ready"))
	r.removeKey(runLuaKey("ready_at"))
	r.setZSet(runLuaKey("leased"), map[string]float64{job: float64(deadline)})
	r.setZSet(runLuaKey("leased_at"), map[string]float64{job: float64(r.now - 500)})
	r.setZSet(ActiveLeasesKey, map[string]float64{runLuaID + ":" + job: float64(deadline)})
	if pending {
		r.data[runLuaKey("")].hash["pending_request_reservations"] = "1"
		r.data[runLuaKey("")].hash["reservation_creations_total"] = "3"
		r.data[runLuaKey("group_pending")].hash[string(input.PolicyGroups[0].GroupID)] = "1"
	}
	if stage {
		commit := strings.Repeat("e", 64)
		r.data[sharedLuaSlots] = bootLuaEntry{kind: "hash", hash: map[string]string{commit: "32768:" + runLuaID + ":" + job + ":1:0"}, expireAt: -1}
		r.setZSet(StageExpiryKey, map[string]float64{commit: float64(r.now + 900000)})
		r.setZSet(runLuaKey("commit_backpressure"), map[string]float64{job: float64(r.now - 450)})
	}
	runLuaRecord(t, r)
	request, err := NewFinalizeRunWireRequest(runLuaGate(t, a, OperationFinalizeRun, false), input.RunID)
	keys, args := runLuaParts(t, request, err)
	return r, keys, args
}

func TestRunLuaFinalizeLiveLeaseStageReceipts(t *testing.T) {
	t.Parallel()
	for _, variant := range []string{"lease", "pending", "started", "stage", "abort", "expired"} {
		t.Run(variant, func(t *testing.T) {
			r, keys, args := runLuaLiveFixture(t, variant == "stage" || variant == "abort", variant == "pending")
			if variant == "started" {
				r.data[runLuaKey("")].hash["started_request_reservations"] = "1"
				r.data[runLuaKey("group_active_started")].hash["research/é"] = "1"
			}
			if variant == "abort" {
				for id, value := range r.data[sharedLuaSlots].hash {
					r.data[sharedLuaSlots].hash[id] = strings.TrimSuffix(value, ":0") + ":73"
				}
				r.removeKey(StageExpiryKey)
			}
			if variant == "expired" {
				r.now += 60001 // logical deadlines do not remove lease evidence
			}
			runLuaReplay(t, r, OperationFinalizeRun, keys, args, "NOT_DUE", "0", "none")
		})
	}
	for _, mutation := range []string{"global_absent", "global_score", "global_fraction", "age_absent", "age_future", "age_deadline", "unknown_owner", "backpressure_orphan", "slot_orphan", "slot_duplicate", "slot_bad_fence", "expiry_absent", "expiry_early", "aborted_with_expiry", "pending_without_creation"} {
		t.Run(mutation, func(t *testing.T) {
			r, keys, args := runLuaLiveFixture(t, true, false)
			job, commit := fmt.Sprintf("%064x", 1), strings.Repeat("e", 64)
			switch mutation {
			case "global_absent":
				r.removeKey(ActiveLeasesKey)
			case "global_score":
				r.zsets[ActiveLeasesKey][runLuaID+":"+job]++
			case "global_fraction":
				r.zsets[ActiveLeasesKey][runLuaID+":"+job] += 0.5
			case "age_absent":
				r.removeKey(runLuaKey("leased_at"))
			case "age_future":
				r.zsets[runLuaKey("leased_at")][job] = float64(r.now + 1)
			case "age_deadline":
				r.zsets[runLuaKey("leased_at")][job] = r.zsets[runLuaKey("leased")][job]
			case "unknown_owner":
				r.zsets[ActiveLeasesKey][strings.Repeat("f", 32)+":"+job] = float64(r.now + 60000)
			case "backpressure_orphan":
				r.setZSet(runLuaKey("commit_backpressure"), map[string]float64{strings.Repeat("f", 64): float64(r.now - 450)})
			case "slot_orphan":
				r.data[sharedLuaSlots].hash[commit] = "32768:" + runLuaID + ":" + strings.Repeat("f", 64) + ":1:0"
			case "slot_duplicate":
				r.data[sharedLuaSlots].hash[strings.Repeat("f", 64)] = r.data[sharedLuaSlots].hash[commit]
			case "slot_bad_fence":
				r.data[sharedLuaSlots].hash[commit] = "32768:" + runLuaID + ":" + job + ":0:0"
			case "expiry_absent":
				r.removeKey(StageExpiryKey)
			case "expiry_early":
				r.zsets[StageExpiryKey][commit] = float64(r.now + 59999)
			case "aborted_with_expiry":
				r.data[sharedLuaSlots].hash[commit] = "32768:" + runLuaID + ":" + job + ":1:1"
			case "pending_without_creation":
				r.data[runLuaKey("")].hash["pending_request_reservations"] = "1"
				r.data[runLuaKey("group_pending")].hash["research/é"] = "1"
			}
			runLuaReject(t, r, OperationFinalizeRun, keys, args, "")
		})
	}
}

func TestRunLuaBackpressureDedicatedClockBounds(t *testing.T) {
	t.Parallel()
	r, keys, args := runLuaLiveFixture(t, true, false)
	job := fmt.Sprintf("%064x", 1)
	// The dedicated backpressure clock advances without a general Run mutation.
	r.zsets[runLuaKey("commit_backpressure")][job] = float64(r.now)
	runLuaReplay(t, r, OperationFinalizeRun, keys, args, "NOT_DUE", "0", "none")
	for _, mutation := range []string{"before_lease_age", "future", "fractional", "orphan", "lease_age_after_activity"} {
		t.Run(mutation, func(t *testing.T) {
			r, keys, args := runLuaLiveFixture(t, true, false)
			r.zsets[runLuaKey("commit_backpressure")][job] = float64(r.now)
			switch mutation {
			case "before_lease_age":
				r.zsets[runLuaKey("commit_backpressure")][job] = r.zsets[runLuaKey("leased_at")][job] - 1
			case "future":
				r.zsets[runLuaKey("commit_backpressure")][job] = float64(r.now + 1)
			case "fractional":
				r.zsets[runLuaKey("commit_backpressure")][job] -= 0.5
			case "orphan":
				r.setZSet(runLuaKey("commit_backpressure"), map[string]float64{strings.Repeat("f", 64): float64(r.now)})
			case "lease_age_after_activity":
				// Unlike backpressure, a claim still advances general activity.
				activity, _ := strconv.ParseUint(r.data[runLuaKey("")].hash["last_activity_at_ms"], 10, 64)
				r.zsets[runLuaKey("leased_at")][job] = float64(activity + 1)
			}
			runLuaReject(t, r, OperationFinalizeRun, keys, args, ErrorStateIndexCorrupt)
		})
	}
}

// Starts from an independently seeded READY job, then executes actual CLAIM,
// START, FINISH, all stage operations, SEAL and a blocked COMMIT. No post-claim
// Job/Run/stage/backpressure state is synthesized or "repaired" by the test.
func runLuaActualBlockedCommit(t *testing.T) *stageOpsFixture {
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
	raw := workerLuaReply(t, f, OperationStartRequest, intent, StatusStarted)
	f.r.now++
	workerLuaReply(t, f, OperationFinishRequest, intent, StatusFinished)
	f.r.now++
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
	render, err := NewRenderPolicyAuthorization(f.policy, renderBytes)
	if err != nil {
		t.Fatal(err)
	}
	context, err := NewOutputContext(f.policy, source, transcript, witness, render)
	if err != nil {
		t.Fatal(err)
	}
	s := &stageOpsFixture{r: &stageOpsRedis{f.r}, a: f.a, context: context, source: source, lease: intent.Lease,
		output: CrawlOutput{Page: OutputPage{NormalizedURL: source.CanonicalURL, HTML: []byte("<html>run recovery</html>"), ContentType: "text/html", StatusCode: 200}},
		runKey: runLuaKey(""), jobKey: jobKey}
	s.rebuildOutput(t)
	s.stageAll(t, false)
	f.r.now += 1000
	s.fullQueue()
	before := f.r.snapshot()
	s.expect(t, OperationCommit, nil, StatusDownstreamBackpressure)
	after := f.r.snapshot()
	bp := f.r.data[jobKey].hash["commit_backpressure_started_at_ms"]
	if bp != strconv.FormatUint(f.r.now, 10) || f.r.zsets[s.runKey+":commit_backpressure"][string(source.JobID)] != float64(f.r.now) {
		t.Fatal("blocked COMMIT did not record its dedicated timestamp/index")
	}
	updated, _ := strconv.ParseUint(before.data[jobKey].hash["updated_at_ms"], 10, 64)
	activity, _ := strconv.ParseUint(before.data[s.runKey].hash["last_activity_at_ms"], 10, 64)
	if updated >= f.r.now || activity >= f.r.now {
		t.Fatal("regression fixture did not advance backpressure beyond both general clocks")
	}
	controls := map[string]bool{"commit_backpressure_fence": true, "commit_backpressure_reason": true,
		"commit_backpressure_started_at_ms": true, "commit_backpressure_deadline_ms": true}
	for field, value := range after.data[jobKey].hash {
		if !controls[field] && before.data[jobKey].hash[field] != value {
			t.Fatalf("blocked COMMIT broadened its Job mutation to %s", field)
		}
	}
	allowed := map[string]bool{jobKey: true, s.runKey + ":commit_backpressure": true, StageSlotsKey: true}
	for _, call := range f.r.trace {
		if !call.acl && stageOpsWrite(call.name) && !allowed[call.args[0]] {
			t.Fatalf("blocked COMMIT wrote outside narrow backpressure controls: %s", call.args[0])
		}
	}
	// All other records, outputs, queues, scopes and TTLs must remain untouched,
	// especially Run.last_activity_at_ms and Job.updated_at_ms.
	for key := range allowed {
		delete(before.data, key)
		delete(before.sets, key)
		delete(before.zsets, key)
		delete(before.lists, key)
		delete(after.data, key)
		delete(after.sets, key)
		delete(after.zsets, key)
		delete(after.lists, key)
	}
	before.writes = after.writes
	if !reflect.DeepEqual(before, after) {
		t.Fatal("blocked COMMIT modified unrelated or general Run state")
	}
	stageOpsValidateHash(t, s.r, jobKey, SchemaJob)
	runLuaRecord(t, f.r)
	return s
}

func TestRunLuaActualBackpressureCrashRecovery(t *testing.T) {
	t.Parallel()
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "cancelled"}[cancelled], func(t *testing.T) {
			s := runLuaActualBlockedCommit(t)
			r := s.r.sharedLuaRedis
			if cancelled {
				r.now++
				request, err := NewCancelRunWireRequest(runLuaGate(t, s.a, OperationCancelRun, false), CancelRunWireInput{RunID: s.lease.RunID, Reason: ReasonOperatorCancelled})
				keys, args := runLuaParts(t, request, err)
				runLuaReply(t, r, OperationCancelRun, keys, args, "CANCELLED", strconv.FormatUint(r.now, 10), "operator_cancelled")
			}
			job := r.data[s.jobKey].hash
			due, err := strconv.ParseUint(job["lease_expires_at_ms"], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			r.now = due // Worker crash: no renewal or synthetic timestamp repair.
			request, err := NewRecoverExpiredWireRequest(runLuaGate(t, s.a, OperationRecoverExpired, false), s.lease.RunID)
			keys, args := runLuaParts(t, request, err)
			for _, corruption := range []string{"backpressure_index_score", "backpressure_fence"} {
				bad := maintenanceLuaCopyRedis(r)
				if corruption == "backpressure_index_score" {
					bad.zsets[s.runKey+":commit_backpressure"][string(s.lease.JobID)]++
				} else {
					bad.data[s.jobKey].hash["commit_backpressure_fence"] = "2"
				}
				runLuaRejectSource(t, bad, maintenanceLuaSource(t, OperationRecoverExpired), keys, args, "")
			}
			before := r.snapshot()
			got := sharedLuaNoError(t, stageOpsRun(t, s.r, maintenanceLuaSource(t, OperationRecoverExpired), keys, args))
			if err := ValidateOperationResponse(OperationRecoverExpired, got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, []any{"BATCH_DONE", strconv.FormatUint(due, 10), "1", "0"}) {
				t.Fatalf("blocked COMMIT -> crash -> recovery failed: %v", got)
			}
			maintenanceLuaTrace(t, r)
			runLuaRecord(t, r)
			stageOpsValidateHash(t, s.r, s.jobKey, SchemaJob)
			state, reason := "delayed", "lease_expired_after_io"
			if cancelled {
				state, reason = "cancelled", "operator_cancelled"
			}
			job = r.data[s.jobKey].hash
			if job["state"] != state || job["last_reason"] != reason || r.data[s.runKey].hash["recovered_leases_total"] != "1" ||
				r.data[s.runKey+":recovery_outcome_counts"].hash[state] != "1" ||
				job["commit_backpressure_reason"] != "none" || job["commit_backpressure_started_at_ms"] != "0" ||
				len(r.zsets[s.runKey+":commit_backpressure"]) != 0 || len(r.zsets[s.runKey+":leased"]) != 0 || len(r.zsets[ActiveLeasesKey]) != 0 {
				t.Fatal("recovery stranded backpressure/lease evidence or applied the wrong disposition")
			}
			if _, exists := r.data[StageSlotsKey]; exists || r.data[s.prefix+"meta"].hash["abandoned"] != "1" ||
				!reflect.DeepEqual(before.zsets[StageExpiryKey], r.zsets[StageExpiryKey]) || !reflect.DeepEqual(before.lists[PagesQueueKey], r.lists[PagesQueueKey]) {
				t.Fatal("recovery failed to release its slot or changed retained cleanup/queue evidence")
			}
			for _, field := range []string{"lease_fence", "lease_request_starts_baseline", "request_starts", "next_request_ordinal", "last_stage_commit_id", "last_stage_fence"} {
				if job[field] != before.data[s.jobKey].hash[field] {
					t.Fatalf("recovery changed retained %s", field)
				}
			}
			before = r.snapshot()
			got = sharedLuaNoError(t, stageOpsRun(t, s.r, maintenanceLuaSource(t, OperationRecoverExpired), keys, args))
			if !reflect.DeepEqual(got, []any{"BATCH_DONE", strconv.FormatUint(due, 10), "0", "0"}) || !reflect.DeepEqual(before, r.snapshot()) {
				t.Fatal("crash-recovery replay repeated a disposition or wrote state")
			}
		})
	}
}

// Both jobs belong to source group A: one completed, one leased. After a
// recorded start in A, the leased job reserves a redirect/resource request in B.
// Request accounting moves to B; immutable source/open-job accounting does not.
func runLuaCrossGroupReservationFixture(t *testing.T, started bool) (*sharedLuaRedis, []string, []string) {
	t.Helper()
	r, a, input := runLuaFixture(t, false)
	runLuaSeed(t, r, input, "active")
	requestGroup := input.PolicyGroups[0]
	requestGroup.GroupID, requestGroup.RateScopeID = "zz-request", RateScopeID(strings.Repeat("3", 32))
	var err error
	requestGroup.GroupScopeID, err = DeriveGroupScopeID(requestGroup.RateScopeID)
	if err != nil {
		t.Fatal(err)
	}
	input.PolicyGroups = append(input.PolicyGroups, requestGroup)
	input.PolicyGroupMapSHA256, err = DerivePolicyGroupMapDigest(input.PolicyGroups)
	if err != nil {
		t.Fatal(err)
	}
	v := r.data[runLuaKey("")].hash
	v["policy_group_count"], v["policy_group_map_sha256"] = "2", string(input.PolicyGroupMapSHA256)
	v["job_count"], v["dead_total"], v["reservation_creations_total"] = "2", "0", "3"
	r.data[runLuaKey("disposition_reason_counts")].hash["policy_denied"] = "0"
	r.removeKey(runLuaKey("dead"))
	delete(r.sets[runLuaKey("jobs")], fmt.Sprintf("%064x", 3))
	delete(r.zsets[runLuaKey("job_order")], fmt.Sprintf("%064x", 3))
	for suffix, value := range map[string]string{
		"group_limits": "10", "group_rate_scope_ids": string(requestGroup.RateScopeID), "group_scope_ids": string(requestGroup.GroupScopeID),
		"group_concurrency": "2", "group_interval_ms": "1000", "group_started": "0", "group_pending": "0",
		"group_active_started": "0", "group_open_jobs": "0", "audit_group_counts": "0",
	} {
		r.data[runLuaKey(suffix)].hash[string(requestGroup.GroupID)] = value
	}
	if started {
		v["request_starts"], v["started_request_reservations"] = "3", "1"
		r.data[runLuaKey("group_started")].hash[string(requestGroup.GroupID)] = "1"
		r.data[runLuaKey("group_active_started")].hash[string(requestGroup.GroupID)] = "1"
	} else {
		v["pending_request_reservations"] = "1"
		r.data[runLuaKey("group_pending")].hash[string(requestGroup.GroupID)] = "1"
	}
	job := fmt.Sprintf("%064x", 1)
	r.removeKey(runLuaKey("ready"))
	r.removeKey(runLuaKey("ready_at"))
	r.setZSet(runLuaKey("leased"), map[string]float64{job: float64(r.now + 60000)})
	r.setZSet(runLuaKey("leased_at"), map[string]float64{job: float64(r.now - 500)})
	r.setZSet(ActiveLeasesKey, map[string]float64{runLuaID + ":" + job: float64(r.now + 60000)})
	runLuaRecord(t, r) // Independent complete Go schema validation, not a Lua bypass.
	request, err := NewCancelRunWireRequest(runLuaGate(t, a, OperationCancelRun, false), CancelRunWireInput{RunID: input.RunID, Reason: ReasonOperatorCancelled})
	keys, args := runLuaParts(t, request, err)
	return r, keys, args
}

func TestRunLuaCrossGroupReservationCancellation(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"pending", "started"} {
		t.Run(phase, func(t *testing.T) {
			r, keys, args := runLuaCrossGroupReservationFixture(t, phase == "started")
			at := strconv.FormatUint(r.now, 10)
			expected := r.snapshot()
			v := expected.data[runLuaKey("")].hash
			v["state"], v["terminal_reason"], v["cancelled_at_ms"], v["last_activity_at_ms"] = "cancelled", "operator_cancelled", at, at
			expected.writes++
			runLuaReply(t, r, OperationCancelRun, keys, args, "CANCELLED", at, "operator_cancelled")
			if !reflect.DeepEqual(expected, r.snapshot()) || r.attempts != 1 {
				t.Fatal("cross-group cancellation changed source/open-job, request, lease, or other retained evidence")
			}
			if r.data[runLuaKey("group_open_jobs")].hash["research/é"] != "1" || r.data[runLuaKey("group_open_jobs")].hash["zz-request"] != "0" {
				t.Fatal("open-job ownership moved from the immutable source group to the request group")
			}
			runLuaRecord(t, r)
			runLuaReplay(t, r, OperationCancelRun, keys, args, "EXISTS_IDENTICAL", at, "operator_cancelled")
		})
	}
}

func TestRunLuaCrossGroupReservationRejectsInvalidGlobalCounts(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"pending", "started"} {
		for _, violation := range []string{"run_sum", "leased_count", "global_concurrency"} {
			t.Run(phase+"/"+violation, func(t *testing.T) {
				started := phase == "started"
				r, keys, args := runLuaCrossGroupReservationFixture(t, started)
				v := r.data[runLuaKey("")].hash
				counter, groupCounter := "pending_request_reservations", "group_pending"
				if started {
					counter, groupCounter = "started_request_reservations", "group_active_started"
				}
				v[counter] = "2"
				if violation != "run_sum" {
					// Two reservations fit the global and per-group concurrency
					// limits, but cannot fit the fixture's single leased job.
					v["reservation_creations_total"] = "4"
					r.data[runLuaKey(groupCounter)].hash["zz-request"] = "2"
					if started {
						v["request_starts"] = "4"
						r.data[runLuaKey("group_started")].hash["zz-request"] = "2"
					}
				}
				if violation == "global_concurrency" {
					// All three reservations have leased jobs and each request
					// group is within its own limit; only the run-wide cap fails.
					v[counter], v["reservation_creations_total"], v["claims_total"] = "3", "5", "4"
					v["job_count"], v["open_job_count"] = "4", "3"
					r.data[runLuaKey(groupCounter)].hash["research/é"] = "1"
					r.data[runLuaKey("group_open_jobs")].hash["research/é"] = "3"
					if started {
						v["request_starts"] = "5"
						r.data[runLuaKey("group_started")].hash["research/é"] = "3"
					}
					for _, n := range []int{3, 4} {
						job := fmt.Sprintf("%064x", n)
						r.sets[runLuaKey("jobs")][job] = true
						r.zsets[runLuaKey("job_order")][job] = 0
						r.zsets[runLuaKey("leased")][job] = float64(r.now + 60000)
						r.zsets[runLuaKey("leased_at")][job] = float64(r.now - 500)
						r.zsets[ActiveLeasesKey][runLuaID+":"+job] = float64(r.now + 60000)
					}
				}
				runLuaRecord(t, r) // Cross-ledger rejection, not a malformed Run hash.
				runLuaReject(t, r, OperationCancelRun, keys, args, ErrorCounterCorrupt)
			})
		}
	}
}

func runLuaDrainedUnexecuted(t *testing.T, r *sharedLuaRedis, input CreateRunWireInput, prefix string) {
	t.Helper()
	runLuaSeed(t, r, input, prefix)
	v := r.data[runLuaKey("")].hash
	v["state"], v["terminal_reason"], v["open_job_count"], v["cancelled_total"] = "cancelled", "source_cancelled", "0", "2"
	for _, name := range []string{"cancelled_at_ms", "last_terminal_transition_at_ms", "last_activity_at_ms"} {
		v[name] = strconv.FormatUint(r.now, 10)
	}
	r.data[runLuaKey("group_open_jobs")].hash[string(input.PolicyGroups[0].GroupID)] = "0"
	r.data[runLuaKey("disposition_reason_counts")].hash["source_cancelled"] = "2"
	r.removeKey(runLuaKey("ready"))
	r.removeKey(runLuaKey("ready_at"))
	r.setZSet(runLuaKey("cancelled"), map[string]float64{fmt.Sprintf("%064x", 1): float64(r.now), fmt.Sprintf("%064x", 2): float64(r.now)})
	runLuaRecord(t, r)
}

func TestRunLuaPreExecutionCancellationFinalizeAndArchive(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"loading", "auditing", "sealed"} {
		t.Run(prefix, func(t *testing.T) {
			r, a, input := runLuaFixture(t, false)
			runLuaDrainedUnexecuted(t, r, input, prefix)
			audit := r.data[runLuaKey("")].hash["audit_count"]
			request, err := NewFinalizeRunWireRequest(runLuaGate(t, a, OperationFinalizeRun, false), input.RunID)
			keys, args := runLuaParts(t, request, err)
			at := strconv.FormatUint(r.now, 10)
			runLuaReply(t, r, OperationFinalizeRun, keys, args, "CANCELLED", at, "source_cancelled")
			runLuaRecord(t, r)
			runLuaReplay(t, r, OperationFinalizeRun, keys, args, "CANCELLED", at, "source_cancelled")
			r.now += 2592000000
			request, err = NewArchiveRunWireRequest(runLuaGate(t, a, OperationArchiveRun, false), ArchiveRunWireInput{RunID: input.RunID, ArchiveSHA256: Digest(strings.Repeat("f", 64))})
			keys, args = runLuaParts(t, request, err)
			runLuaReply(t, r, OperationArchiveRun, keys, args, "ARCHIVED", strconv.FormatUint(r.now, 10), args[8])
			runLuaRecord(t, r)
			if r.data[runLuaKey("")].hash["audit_count"] != audit {
				t.Fatal("finalization/archive rewrote the preserved audit prefix")
			}
		})
	}
}

func runLuaPurgeFixture(t *testing.T, candidate bool) (*sharedLuaRedis, gateArtifacts, CreateRunWireInput) {
	t.Helper()
	r, a, input := runLuaFixture(t, candidate)
	primary := "completed"
	if candidate {
		runLuaDrainedUnexecuted(t, r, input, "auditing")
		primary = "cancelled"
	} else {
		runLuaSeed(t, r, input, "archived")
		r.now += 604800000
	}
	v := r.data[runLuaKey("")].hash
	v["purge_state"], v["purged_job_count"], v["purge_started_at_ms"] = "in_progress", "1", strconv.FormatUint(r.now, 10)
	v["purge_evidence_sha256"] = v["archive_sha256"]
	if candidate {
		v["purge_evidence_sha256"] = strings.Repeat("c", 64)
	}
	job := fmt.Sprintf("%064x", 1)
	delete(r.sets[runLuaKey("jobs")], job)
	delete(r.zsets[runLuaKey("job_order")], job)
	delete(r.zsets[runLuaKey(primary)], job)
	runLuaRecord(t, r)
	return r, a, input
}

func TestRunLuaPurgeReadValidationAndMutationExclusion(t *testing.T) {
	t.Parallel()
	// A proper reduced inventory is readable evidence, not corrupt normal state.
	// None of these seven operations may continue, reset, or repair the purge.
	for _, candidate := range []bool{false, true} {
		r, a, input := runLuaPurgeFixture(t, candidate)
		request, err := NewBeginRunAuditWireRequest(runLuaGate(t, a, OperationBeginRunAudit, candidate), input.RunID)
		keys, args := runLuaParts(t, request, err)
		source := runLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.run_spec("CJ2_BEGIN_RUN_AUDIT",{"run_id"}),KEYS,ARGV))
assert(CJ.Gate.check(ctx)); local run,code=CJ.Run.load(ctx,ctx.request.v.run_id)
if not run then return CJ.Context.reject(code) end
return {run.v.purge_state,run.v.purged_job_count,P.format_decimal(run.indexes.jobs.count)}`
		before := r.snapshot()
		got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
		remaining := "2"
		if candidate {
			remaining = "1"
		}
		if !reflect.DeepEqual(got, []any{"in_progress", "1", remaining}) || !reflect.DeepEqual(before, r.snapshot()) {
			t.Fatal("purge-aware load changed or misread retained evidence")
		}
		runLuaTrace(t, r)
		for _, mutation := range []string{"missing_evidence", "zero_purged", "all_purged", "missing_start", "inventory_not_reduced", "primary_total"} {
			r, _, _ = runLuaPurgeFixture(t, candidate)
			switch mutation {
			case "missing_evidence":
				r.data[runLuaKey("")].hash["purge_evidence_sha256"] = ""
			case "zero_purged":
				r.data[runLuaKey("")].hash["purged_job_count"] = "0"
			case "all_purged":
				r.data[runLuaKey("")].hash["purged_job_count"] = r.data[runLuaKey("")].hash["job_count"]
			case "missing_start":
				r.data[runLuaKey("")].hash["purge_started_at_ms"] = "0"
			case "inventory_not_reduced":
				r.sets[runLuaKey("jobs")][fmt.Sprintf("%064x", 1)] = true
			case "primary_total":
				r.removeKey(runLuaKey("cancelled"))
				r.removeKey(runLuaKey("dead"))
			}
			runLuaRejectSource(t, r, source, keys, args, "")
		}
	}
	for _, op := range runLuaOperations {
		r, _, _ := runLuaPurgeFixture(t, false)
		_, keys, args := runLuaOperationCase(t, op)
		runLuaReject(t, r, op, keys, args, ErrorInvalidState)
	}
	r, a, input := runLuaPurgeFixture(t, true)
	request, err := NewCancelRunWireRequest(runLuaGate(t, a, OperationCancelRun, true), CancelRunWireInput{RunID: input.RunID, Reason: ReasonSourceCancelled})
	keys, args := runLuaParts(t, request, err)
	runLuaReject(t, r, OperationCancelRun, keys, args, ErrorInvalidState)
}

func TestRunLuaBoundedTenThousandJobLedger(t *testing.T) {
	t.Parallel()
	r, a, input := runLuaFixture(t, false)
	runLuaSeed(t, r, input, "loading")
	v := r.data[runLuaKey("")].hash
	v["expected_seed_count"], v["job_count"], v["open_job_count"] = "10000", "10000", "10000"
	r.data[runLuaKey("group_open_jobs")].hash["research/é"] = "10000"
	jobs, order, ready, age := make([]string, 10000), map[string]float64{}, map[string]float64{}, map[string]float64{}
	for i := range jobs {
		id := fmt.Sprintf("%064x", i+1)
		jobs[i], order[id], ready[id], age[id] = id, 0, 1, float64(r.now-500)
	}
	r.setSet(runLuaKey("jobs"), jobs)
	r.setZSet(runLuaKey("job_order"), order)
	r.setZSet(runLuaKey("ready"), ready)
	r.setZSet(runLuaKey("ready_at"), age)
	request, err := NewBeginRunAuditWireRequest(runLuaGate(t, a, OperationBeginRunAudit, false), input.RunID)
	keys, args := runLuaParts(t, request, err)
	runLuaReply(t, r, OperationBeginRunAudit, keys, args, "AUDIT_STARTED", "2", "10000")
	runLuaRecord(t, r)
	// An excess is detected by cardinality before any enumeration or mutation.
	r.zsets[runLuaKey("ready")][strings.Repeat("f", 64)] = 1
	runLuaReject(t, r, OperationBeginRunAudit, keys, args, ErrorLimitExceeded)
}

func TestRunLuaDeltaHelpersAggregateAndRejectAtomically(t *testing.T) {
	t.Parallel()
	r, a, input := runLuaFixture(t, false)
	runLuaSeed(t, r, input, "loading")
	request, err := NewCancelRunWireRequest(runLuaGate(t, a, OperationCancelRun, false), CancelRunWireInput{RunID: input.RunID, Reason: ReasonOperatorCancelled})
	keys, args := runLuaParts(t, request, err)
	runLuaReply(t, r, OperationCancelRun, keys, args, "CANCELLED", strconv.FormatUint(r.now, 10), "operator_cancelled")
	r.now += 100
	// Test the public accumulator, not a shadow CANCEL_BATCH implementation or a
	// claim that CANCEL_RUN itself changes jobs. Job planners own the descriptors
	// and selected membership proofs; Run owns one aggregate per affected hash.
	source := runLuaCore(t) + `
assert(CJ.Reply.register("CJ2_CANCEL_RUN",function(ctx,status,tail)
    if status=="EXISTS_IDENTICAL" and #tail==2 and CJ.Identities.positive(tail[1]) and tail[2]=="operator_cancelled" then return true end
    return nil,"INVALID_ARGUMENT"
end))
local ctx=assert(CJ.Context.open(CJ.Wire.run_spec("CJ2_CANCEL_RUN",{"run_id","reason"}),KEYS,ARGV))
assert(CJ.Gate.check(ctx)); local run=assert(CJ.Run.load(ctx,ctx.request.v.run_id)); local plan=assert(CJ.Plan.new(ctx))
local delta=assert(CJ.Run.plan_delta(ctx,run)); local group=run.group_ids[1]
local bad,code=CJ.Run.accumulate(delta,{run={cancelled_total=100},maps={unknown={x=1}}})
assert(not bad and code=="INVALID_ARGUMENT")
bad,code=CJ.Run.accumulate(delta,{run={cancelled_total=1},typo={}}); assert(not bad and code=="INVALID_ARGUMENT")
bad,code=CJ.Run.set(delta,{source_sha256=string.rep("b",64),last_activity_at_ms="999"}); assert(not bad and code=="INVALID_ARGUMENT")
bad,code=CJ.Run.set(delta,{cancelled_total="2"}); assert(not bad and code=="INVALID_ARGUMENT")
local ids={string.rep("0",63).."1",string.rep("0",63).."2"}
for _,name in ipairs({"ready","ready_at","cancelled"}) do
    assert(CJ.Read.members(ctx,CJ.Run.key(ctx,name),"zset",ids,10000,64))
end
for i=1,2 do
    assert(CJ.Run.accumulate(delta,{run={open_job_count=-1,cancelled_total=1},
        maps={group_open_jobs={[group]=-1},disposition_reason_counts={operator_cancelled=1}},
        indexes={ready=-1,ready_at=-1,cancelled=1}}))
end
assert(CJ.Run.set(delta,{last_activity_at_ms=ctx.now_text,last_terminal_transition_at_ms=ctx.now_text}))
assert(CJ.Plan.add(plan,{"ZREM",ctx.keys.run_ready,ids[1],ids[2]},"ordinary"))
assert(CJ.Plan.add(plan,{"ZREM",ctx.keys.run_ready_at,ids[1],ids[2]},"ordinary"))
assert(CJ.Plan.add(plan,{"ZADD",ctx.keys.run_cancelled,ctx.now_text,ids[1],ctx.now_text,ids[2]},"ordinary"))
local post=assert(CJ.Run.flush(ctx,plan,delta))
assert(post.n.cancelled_total==2 and post.maps.group_open_jobs.n[group]==0)
bad,code=CJ.Run.accumulate(delta,{run={cancelled_total=1}}); assert(not bad and code=="INVALID_STATE")
bad,code=CJ.Run.flush(ctx,plan,delta); assert(not bad and code=="INVALID_STATE")
local execution=assert(CJ.Run.finish(ctx,plan,"EXISTS_IDENTICAL",{post.v.cancelled_at_ms,post.v.terminal_reason}))
for i=1,execution.count do redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc)) end
return execution.reply`
	got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
	if err := ValidateOperationResponse(OperationCancelRun, got); err != nil {
		t.Fatal(err)
	}
	runLuaRecord(t, r)
	counts := map[string]int{}
	for _, call := range r.trace {
		if !call.acl && call.name == "HSET" {
			counts[call.args[0]]++
		}
	}
	if !reflect.DeepEqual(counts, map[string]int{runLuaKey(""): 1, runLuaKey("group_open_jobs"): 1, runLuaKey("disposition_reason_counts"): 1}) {
		t.Fatalf("counter updates were not coalesced into one HSET per hash: %v", counts)
	}
	// Independently reload the actual post-state through a complete run handler.
	runLuaReplay(t, r, OperationCancelRun, keys, args, "EXISTS_IDENTICAL", strconv.FormatUint(bootLuaNow, 10), "operator_cancelled")
	request, err = NewFinalizeRunWireRequest(runLuaGate(t, a, OperationFinalizeRun, false), input.RunID)
	keys, args = runLuaParts(t, request, err)
	runLuaReply(t, r, OperationFinalizeRun, keys, args, "CANCELLED", strconv.FormatUint(r.now, 10), "operator_cancelled")
}

func TestRunLuaTimeSizeAndRetentionOverflowRejections(t *testing.T) {
	t.Parallel()
	for _, op := range runLuaOperations {
		for _, mutation := range []string{"zero_clock", "future_activity", "oversized_request"} {
			t.Run(string(op)+"/"+mutation, func(t *testing.T) {
				r, keys, args := runLuaOperationCase(t, op)
				if op == OperationCreateRun {
					_, _, input := runLuaFixture(t, false)
					runLuaSeed(t, r, input, "loading")
				}
				switch mutation {
				case "zero_clock":
					r.now = 0
				case "future_activity":
					r.now, _ = strconv.ParseUint(r.data[runLuaKey("")].hash["last_activity_at_ms"], 10, 64)
					r.now--
				case "oversized_request":
					args[7] = strings.Repeat("a", 2097152)
				}
				runLuaReject(t, r, op, keys, args, "")
				if mutation != "future_activity" && len(r.trace) != 1 {
					t.Fatal("invalid clock/oversized request performed reads after the sole TIME")
				}
			})
		}
	}
	r, keys, args := runLuaOperationCase(t, OperationArchiveRun)
	v := r.data[runLuaKey("")].hash
	r.now = MaxExactInteger
	for _, name := range []string{"completed_at_ms", "finalized_at_ms", "last_activity_at_ms", "retention_anchor_ms"} {
		v[name] = strconv.FormatUint(r.now, 10)
	}
	runLuaRecord(t, r)
	runLuaReject(t, r, OperationArchiveRun, keys, args, ErrorInvalidNumber)
}

func TestRunLuaOpenGroupBudgetSelectionAndDelayedEvidence(t *testing.T) {
	t.Parallel()
	for _, blocked := range []bool{false, true} {
		r, a, input := runLuaFixture(t, false)
		runLuaSeed(t, r, input, "active")
		second := input.PolicyGroups[0]
		second.GroupID, second.RequestStartLimit = "zz-other", 1
		input.PolicyGroups[0].RequestStartLimit = 2
		input.PolicyGroups = append(input.PolicyGroups, second)
		digest, err := DerivePolicyGroupMapDigest(input.PolicyGroups)
		if err != nil {
			t.Fatal(err)
		}
		v := r.data[runLuaKey("")].hash
		v["policy_group_count"], v["policy_group_map_sha256"] = "2", string(digest)
		r.data[runLuaKey("group_limits")].hash["research/é"] = "2"
		for suffix, value := range map[string]string{
			"group_limits": "1", "group_rate_scope_ids": string(second.RateScopeID), "group_scope_ids": string(second.GroupScopeID),
			"group_concurrency": "2", "group_interval_ms": "1000", "group_started": "0", "group_pending": "0",
			"group_active_started": "0", "group_open_jobs": "0", "audit_group_counts": "0",
		} {
			r.data[runLuaKey(suffix)].hash[string(second.GroupID)] = value
		}
		if blocked {
			// The unexhausted group owns the remaining job; it cannot be ignored.
			r.data[runLuaKey("group_open_jobs")].hash["research/é"] = "0"
			r.data[runLuaKey("group_open_jobs")].hash[string(second.GroupID)] = "1"
		}
		// Budget finalization deliberately retains delayed work. It must neither
		// reject it as a lease nor promote/delete it while recording finalization.
		r.removeKey(runLuaKey("ready"))
		r.removeKey(runLuaKey("ready_at"))
		delayed := map[string]float64{fmt.Sprintf("%064x", 1): float64(r.now + 120000)}
		r.setZSet(runLuaKey("delayed"), delayed)
		request, err := NewFinalizeRunWireRequest(runLuaGate(t, a, OperationFinalizeRun, false), input.RunID)
		keys, args := runLuaParts(t, request, err)
		if blocked {
			runLuaReplay(t, r, OperationFinalizeRun, keys, args, "NOT_DUE", "0", "none")
		} else {
			at := strconv.FormatUint(r.now, 10)
			runLuaReply(t, r, OperationFinalizeRun, keys, args, "GROUP_BUDGET_EXHAUSTED", at, "group_budgets_exhausted")
			runLuaRecord(t, r)
			runLuaReplay(t, r, OperationFinalizeRun, keys, args, "GROUP_BUDGET_EXHAUSTED", at, "group_budgets_exhausted")
			r.now += 2592000000
			request, err = NewArchiveRunWireRequest(runLuaGate(t, a, OperationArchiveRun, false), ArchiveRunWireInput{RunID: input.RunID, ArchiveSHA256: Digest(strings.Repeat("f", 64))})
			keys, args = runLuaParts(t, request, err)
			runLuaReply(t, r, OperationArchiveRun, keys, args, "ARCHIVED", strconv.FormatUint(r.now, 10), args[8])
			runLuaRecord(t, r)
		}
		if !reflect.DeepEqual(r.zsets[runLuaKey("delayed")], delayed) {
			t.Fatal("finalization/archive modified retained delayed evidence")
		}
	}
}

func TestRunLuaCancellationReserveIncludesEveryStageSlot(t *testing.T) {
	t.Parallel()
	for _, short := range []uint64{0, 1} {
		r, _, _ := runLuaLiveFixture(t, true, false)
		_, keys, args := runLuaOperationCase(t, OperationCancelRun)
		growth := uint64(3 * (len("cancelled") + len("operator_cancelled") + 2*len(strconv.FormatUint(r.now, 10))))
		r.maximum = r.used + 32768 + 67108864 + growth - short
		if short == 1 {
			runLuaReject(t, r, OperationCancelRun, keys, args, ErrorMemoryHeadroomLow)
		} else {
			before := r.snapshot()
			runLuaReply(t, r, OperationCancelRun, keys, args, "CANCELLED", strconv.FormatUint(r.now, 10), "operator_cancelled")
			if r.data[sharedLuaSlots].hash[strings.Repeat("e", 64)] != before.data[sharedLuaSlots].hash[strings.Repeat("e", 64)] ||
				!reflect.DeepEqual(r.zsets[ActiveLeasesKey], before.zsets[ActiveLeasesKey]) || r.attempts != 1 {
				t.Fatal("bounded cancellation released a slot or touched leased work")
			}
		}
	}
}

func TestRunLuaDeltaHelperReceiptAndPhaseGuards(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`run.maps.group_limits.v[run.group_ids[1]]="9"; local v,c=CJ.Run.plan_delta(ctx,run); assert(not v and c=="IMMUTABLE_MISMATCH")`,
		`run.maps.group_scope_ids.v[run.group_ids[1]]=string.rep("f",64); local v,c=CJ.Run.plan_delta(ctx,run); assert(not v and c=="COUNTER_CORRUPT")`,
		`run.maps.group_open_jobs.n[run.group_ids[1]]=9999; run.reasons.retry_reason_counts.sum=9999; assert(CJ.Run.plan_delta(ctx,run)); assert(run.maps.group_open_jobs.n[run.group_ids[1]]==1 and run.reasons.retry_reason_counts.sum==0)`,
		`run.run_id=string.rep("f",32); local v,c=CJ.Run.plan_delta(ctx,run); assert(not v and c=="INVALID_STATE")`,
		`local h=assert(CJ.Run.plan_delta(ctx,run)); assert(CJ.Run.accumulate(h,{run={open_job_count=-2}})); local v,c=CJ.Run.flush(ctx,assert(CJ.Plan.new(ctx)),h); assert(not v and c=="COUNTER_CORRUPT")`,
		`local h=assert(CJ.Run.plan_delta(ctx,run)); assert(CJ.Context.seal(ctx));
          for _,f in ipairs({function()return CJ.Run.accumulate(h,{})end,function()return CJ.Run.set(h,{})end,
              function()return CJ.Run.begin_audit(h)end,function()return CJ.Run.member(h,"active_runs",true)end,
              function()return CJ.Run.plan_delta(ctx,run)end,function()return CJ.Run.mutable(ctx,run)end}) do
              local v,c=f(); assert(not v and c=="INVALID_STATE") end`,
	} {
		r, keys, args := runLuaOperationCase(t, OperationCancelRun)
		before := r.snapshot()
		source := runLuaCore(t) + `local ctx=assert(CJ.Context.open(CJ.Wire.run_spec("CJ2_CANCEL_RUN",{"run_id","reason"}),KEYS,ARGV));
assert(CJ.Gate.check(ctx)); local run=assert(CJ.Run.load(ctx,ctx.request.v.run_id)); ` + body + `;return {"checked"}`
		got := sharedLuaNoError(t, sharedLuaRun(t, r, source, keys, args))
		if !reflect.DeepEqual(got, []any{"checked"}) || !reflect.DeepEqual(before, r.snapshot()) {
			t.Fatal("helper rejection or verification changed Redis state")
		}
		runLuaTrace(t, r)
	}
}

func TestRunLuaChangedReplayScalarsNeverEnterAdmission(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationName{OperationCreateRun, OperationSealRun, OperationActivateRun, OperationCancelRun, OperationArchiveRun} {
		positions := map[OperationName][]int{
			OperationCreateRun: {9, 10, 11, 12, 13, 15, 17, 18, 19, 22}, OperationSealRun: {8, 9},
			OperationActivateRun: {8, 9, 10, 11, 12}, OperationCancelRun: {8}, OperationArchiveRun: {8, 9},
		}[op]
		for _, position := range positions {
			t.Run(fmt.Sprintf("%s/arg%d", op, position), func(t *testing.T) {
				r, keys, args := runLuaOperationCase(t, op)
				sharedLuaNoError(t, sharedLuaRun(t, r, runLuaSource(t, op), keys, args))
				changed := append([]string(nil), args...)
				changed[position] = strings.Repeat("b", 64)
				if op == OperationCreateRun && position == 10 || op == OperationSealRun && position == 8 {
					changed[position] = "1"
				} else if op == OperationCreateRun && position == 13 {
					changed[position] = strconv.FormatUint(r.now+60000, 10)
				} else if op == OperationCreateRun && position == 18 {
					changed[position] = "2"
				} else if op == OperationCreateRun && position == 22 {
					changed[position] = "9"
				} else if op == OperationCancelRun {
					changed[position] = "source_cancelled"
				} else if op == OperationArchiveRun && position == 8 {
					changed[9] = runLuaID + ":" + changed[8]
				}
				r.maximum, r.denyAt = 1, 1
				runLuaReject(t, r, op, keys, changed, ErrorImmutableMismatch)
				if r.aclCount != 0 {
					t.Fatal("conflicting replay reached ACL admission")
				}
			})
		}
	}
}

func TestRunLuaFinalizeBoundedForeignLeasesAndSlots(t *testing.T) {
	t.Parallel()
	for _, count := range []int{64, 65} {
		r, keys, args := runLuaOperationCase(t, OperationFinalizeRun)
		owner := strings.Repeat("2", 32)
		r.zsets[RunsKey][owner] = float64(r.now - 1000)
		r.sets[ActiveRunsKey][owner], r.sets[UnarchivedRunsKey][owner] = true, true
		leases := map[string]float64{}
		for i := 1; i <= count; i++ {
			leases[owner+":"+fmt.Sprintf("%064x", i)] = float64(r.now + 60000)
		}
		r.setZSet(ActiveLeasesKey, leases)
		commit := strings.Repeat("e", 64)
		r.data[sharedLuaSlots] = bootLuaEntry{kind: "hash", hash: map[string]string{commit: "32768:" + owner + ":" + fmt.Sprintf("%064x", 1) + ":1:0"}, expireAt: -1}
		r.setZSet(StageExpiryKey, map[string]float64{commit: float64(r.now + 900000)})
		if count == 65 {
			runLuaReject(t, r, OperationFinalizeRun, keys, args, ErrorLimitExceeded)
			for _, call := range r.trace {
				if call.name == "ZRANGE" && call.args[0] == ActiveLeasesKey {
					t.Fatal("oversized global lease set was enumerated before rejecting")
				}
			}
		} else {
			runLuaReply(t, r, OperationFinalizeRun, keys, args, "RUN_BUDGET_EXHAUSTED", strconv.FormatUint(r.now, 10), "request_budget_exhausted")
			if !reflect.DeepEqual(r.zsets[ActiveLeasesKey], leases) || !r.sets[ActiveRunsKey][owner] || len(r.data[sharedLuaSlots].hash) != 1 {
				t.Fatal("run-local finalization touched another run's live work")
			}
		}
	}
}

// Real Go-validated Run/Job/Stage records and real core recovery units. Only
// fixture facts are seeded: no grants, memory policy, unit, or growth callbacks
// are replaced. SOURCE groups are independently chosen by the fixture, never
// inferred from slot ownership or a reservation's charged group.
func runLuaRecoveryCoverageFixture(t *testing.T, slots []bool, groupCount int) (*recordsLuaFixture, []string, []string) {
	t.Helper()
	count := len(slots)
	countText := strconv.Itoa(count)
	f := recordsLuaNew(t, false, count, count, groupCount)
	f.seed(t, f.jobs)
	v := f.r.data[runLuaKey("")].hash
	v["state"], v["audit_revision"], v["audit_count"], v["audit_complete"], v["audit_cursor"] = "active", "2", countText, "1", string(f.jobs[count-1].JobID)
	v["sealed_at_ms"], v["activated_at_ms"] = strconv.FormatUint(f.r.now-90, 10), strconv.FormatUint(f.r.now-85, 10)
	v["claims_total"], v["reservation_creations_total"], v["request_starts"] = countText, countText, countText
	v["last_request_started_at_ms"], v["last_execution_at_ms"] = strconv.FormatUint(f.r.now-20, 10), strconv.FormatUint(f.r.now-20, 10)
	leases, ages, global := map[string]float64{}, map[string]float64{}, map[string]float64{}
	groupCounts := map[string]int{}
	for i, source := range f.jobs {
		id := string(source.JobID)
		job := f.r.data[runLuaKey("job:"+id)].hash
		job["state"], job["claim_count"], job["lease_fence"], job["next_request_ordinal"] = "leased", "1", "1", "2"
		job["lease_owner"], job["lease_token"] = strings.Repeat("d", 32), fmt.Sprintf("%064x", i+1)
		job["lease_started_at_ms"], job["lease_expires_at_ms"], job["updated_at_ms"] = strconv.FormatUint(f.r.now-80, 10), strconv.FormatUint(f.r.now, 10), strconv.FormatUint(f.r.now-10, 10)
		job["delivery_attempts"], job["request_starts"], job["lease_delivery_started"] = "1", "1", "1"
		job["last_request_started_at_ms"], job["last_document_request_started_at_ms"], job["last_document_request_fence"] = v["last_request_started_at_ms"], v["last_request_started_at_ms"], "1"
		job["last_document_target_url_id"], job["last_document_target_url"], job["last_document_target_digest"] = id, source.CanonicalURL, string(source.Decision.TargetDigest)
		leases[id], ages[id], global[runLuaID+":"+id] = float64(f.r.now), float64(f.r.now-80), float64(f.r.now)
		groupCounts[string(source.GroupID)]++
		maintenanceLuaJobRecord(t, f.r, source.JobID)
	}
	f.r.removeKey(runLuaKey("ready"))
	f.r.removeKey(runLuaKey("ready_at"))
	f.r.setZSet(runLuaKey("leased"), leases)
	f.r.setZSet(runLuaKey("leased_at"), ages)
	f.r.setZSet(ActiveLeasesKey, global)
	audit := Record{}
	for _, group := range f.input.PolicyGroups {
		count := strconv.Itoa(groupCounts[string(group.GroupID)])
		f.r.data[runLuaKey("group_started")].hash[string(group.GroupID)] = count
		audit = append(audit, textField(string(group.GroupID), count))
	}
	f.r.setHash(runLuaKey("audit_group_counts"), audit)
	allSlots, expiry := Record{}, map[string]float64{}
	for i, slotted := range slots {
		if slotted {
			commit, due := maintenanceLuaStage(t, f, i)
			allSlots = append(allSlots, textField(string(commit), f.r.data[StageSlotsKey].hash[string(commit)]))
			expiry[string(commit)] = float64(due)
		}
	}
	if len(allSlots) > 0 {
		f.r.setHash(StageSlotsKey, allSlots)
		f.r.setZSet(StageExpiryKey, expiry)
	}
	v["state"], v["terminal_reason"], v["cancelled_at_ms"] = "cancelled", "operator_cancelled", strconv.FormatUint(f.r.now-1, 10)
	v["last_activity_at_ms"] = v["cancelled_at_ms"]
	runLuaRecord(t, f.r)
	request, err := NewRecoverExpiredWireRequest(runLuaGate(t, f.a, OperationRecoverExpired, false), f.input.RunID)
	keys, args := runLuaParts(t, request, err)
	return f, keys, args
}

func runLuaRecoveryCoverageSource(t *testing.T, scenario string, execute bool) string {
	t.Helper()
	// A test composition of actual helpers, NOT an alternate maintenance handler.
	// This caller explicitly owns job/index descriptors and passes their same unit
	// to Run. Neither Plan nor Memory is mocked or supplied a numeric G allowance.
	source := maintenanceLuaCore(t) + `
assert(CJ.Maintenance.register("CJ2_RECOVER_EXPIRED"))
local ctx=assert(CJ.Maintenance.open("CJ2_RECOVER_EXPIRED",KEYS,ARGV))
local run=assert(CJ.Run.load(ctx,ctx.request.v.run_id))
local live=assert(CJ.Run.live(ctx,run))
local page=assert(CJ.Read.due(ctx,ctx.keys.run_leased,ctx.now_text,4,64,64))
assert(#page.ordered==2 or #page.ordered==4)
local plan=assert(CJ.Plan.new(ctx)); assert(CJ.Plan.set_policy(plan,"recovery"))
local delta=assert(CJ.Run.plan_delta(ctx,run)); local jobs,stages,units={},{},{}
for i,id in ipairs(page.ordered) do
    assert(CJ.Context.bind_job(ctx,id)); jobs[i]=assert(CJ.Job.load(ctx,run,id))
    stages[i]=assert(CJ.Context.bind_stage(ctx,id))
    if stages[i].exists then assert(CJ.Read.fixed_hash(ctx,stages[i].keys.meta,"stage_meta")) end
    units[i]=assert(CJ.Plan.recovery_unit(plan,id))
end
local scenario=` + strconv.Quote(scenario) + `
local coverage={};for i,unit in ipairs(units) do coverage[i]=unit end
if scenario=="unknown" or scenario=="losing_unknown" then coverage[2]={growth=0,remaining=50331648}
elseif scenario=="wrong_real_owner" then coverage[1],coverage[2]=units[2],units[1]
elseif scenario=="foreign_plan" then
    local other=assert(CJ.Plan.new(ctx));assert(CJ.Plan.set_policy(other,"recovery"))
    coverage[2]=assert(CJ.Plan.recovery_unit(other,jobs[2].v.job_id))
elseif scenario=="public_fields" then
    for _,unit in ipairs(units) do unit.remaining=9007199254740991;unit.growth=0;unit.job_id="not-authority" end
elseif scenario=="rejected_changes" then
    local ok,code=CJ.Run.accumulate(delta,{run={cancelled_total=100},maps={unknown={x=1}}},{})
    assert(not ok and code=="INVALID_ARGUMENT")
    ok,code=CJ.Run.set(delta,{source_sha256="bad",last_activity_at_ms="1"},{})
    assert(not ok and code=="INVALID_ARGUMENT")
elseif scenario=="zero_contribution" then
    -- A zero delta cannot steal another group's first REAL contribution. An
    -- unchanged lifecycle value likewise produces no descriptor or allocation.
    assert(CJ.Run.accumulate(delta,{run={cancelled_total=0},maps={group_open_jobs={[jobs[2].v.group_id]=0}}},units[1]))
    assert(CJ.Run.set(delta,{last_execution_at_ms=run.v.last_execution_at_ms},units[2]))
end
local function prepare()
    for i,job in ipairs(jobs) do
        local unit=units[i]
        local post=assert(CJ.Job.outcome_record(ctx,job,{state="cancelled",last_reason="operator_cancelled",last_transition_id="",last_transition_status=""}))
        local changed={};for field,value in next,post.v,nil do if value~=job.v[field] then changed[field]=value end end
        assert(CJ.Run.hset(plan,job.key,changed,unit))
        for _,argv in ipairs({{"ZREM",ctx.keys.run_leased,job.v.job_id},{"ZREM",ctx.keys.run_leased_at,job.v.job_id},
            {"ZREM",ctx.keys.active_leases,run.run_id..":"..job.v.job_id},
            {"ZADD",ctx.keys.run_cancelled,ctx.now_text,job.v.job_id}}) do assert(CJ.Plan.add(plan,argv,unit)) end
        if stages[i].exists then assert(CJ.Run.hset(plan,stages[i].keys.meta,{abandoned="1"},unit)) end
        local owner=coverage[i]
        if i==2 and scenario=="missing" then owner=nil end
        if i==2 and scenario=="numeric_coverage" then owner=0 end
        if i==2 and scenario=="mixed_ordinary" then owner="ordinary" end
        local changes={run={open_job_count=-1,cancelled_total=1,recovered_leases_total=1},
            maps={group_open_jobs={[job.v.group_id]=-1},disposition_reason_counts={operator_cancelled=1},recovery_outcome_counts={cancelled=1}},
            indexes={leased=-1,leased_at=-1,cancelled=1}}
        if i==2 and scenario=="losing_unknown" then
            -- The real unit owns its source-group field, but a forged token
            -- contributes ONLY shared fields. It must not disappear silently.
            assert(CJ.Run.accumulate(delta,{maps=changes.maps,indexes=changes.indexes},unit))
            changes={run=changes.run}
        end
        local ok,code=CJ.Run.accumulate(delta,changes,owner);if not ok then return nil,code end
        if i==2 and scenario=="missing_set" then owner=nil end
        if i==2 and scenario=="mixed_set" then owner="ordinary" end
        local times={last_activity_at_ms=ctx.now_text,last_terminal_transition_at_ms=ctx.now_text}
        if scenario=="lifecycle_partition" and i<#jobs then times.last_terminal_transition_at_ms=nil end
        ok,code=CJ.Run.set(delta,times,owner)
        if not ok then return nil,code end
    end
    local whole=scenario=="blanket" and units[1] or nil
    local post,code=CJ.Run.flush(ctx,plan,delta,whole);if not post then return nil,code end
    assert(post.n.cancelled_total==#jobs and post.n.recovered_leases_total==#jobs and post.n.open_job_count==0)
    local assessment;assessment,code=CJ.Plan.assess(ctx,plan);if not assessment then return nil,code end
    return assessment
end
local assessment,code=prepare()
if not assessment then return CJ.Context.reject(code) end
`
	if !execute {
		return source + `return {P.format_decimal(assessment.growth),P.format_decimal(assessment.covered_growth),P.format_decimal(assessment.uncovered_growth),assessment.admission}`
	}
	return source + `local reply=assert(CJ.Reply.build(ctx,"BATCH_DONE",{P.format_decimal(#jobs),"0"}))
local execution=assert(CJ.Plan.seal(ctx,plan,assessment,reply))
for i=1,execution.count do redis.call(unpack(execution.calls[i].argv,1,execution.calls[i].argc)) end
return execution.reply`
}

func TestRunLuaRecoveryFieldCoveragePartitions(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"unslotted", "mixed", "slot_first", "all_slots", "shared_only", "four_slots"} {
		for _, scenario := range []string{"normal", "public_fields", "rejected_changes", "zero_contribution", "lifecycle_partition"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				slotOwners := []bool{mode == "slot_first" || mode == "all_slots" || mode == "shared_only", mode == "mixed" || mode == "all_slots" || mode == "shared_only"}
				groups := 2
				if mode == "shared_only" {
					groups = 1
				} else if mode == "four_slots" {
					slotOwners, groups = []bool{true, true, true, true}, 4
				}
				f, keys, args := runLuaRecoveryCoverageFixture(t, slotOwners, groups)
				before := f.r.snapshot()
				assessment := sharedLuaNoError(t, stageOpsRun(t, &stageOpsRedis{f.r}, runLuaRecoveryCoverageSource(t, scenario, false), keys, args)).([]any)
				if !reflect.DeepEqual(before, f.r.snapshot()) {
					t.Fatal("preparation/assessment mutated Redis")
				}
				growth, _ := strconv.ParseUint(assessment[0].(string), 10, 64)
				covered, _ := strconv.ParseUint(assessment[1].(string), 10, 64)
				uncovered, _ := strconv.ParseUint(assessment[2].(string), 10, 64)
				if growth != covered+uncovered || mode == "unslotted" && covered != 0 ||
					(mode == "all_slots" || mode == "shared_only" || mode == "four_slots") && uncovered != 0 ||
					(mode == "mixed" || mode == "slot_first") && (covered == 0 || uncovered == 0) {
					t.Fatalf("incorrect coverage split: %v", assessment)
				}
				// Exact safety boundary: charging slotted growth twice fails here.
				slots := uint64(0)
				for _, slotted := range slotOwners {
					if slotted {
						slots += 32768
					}
				}
				f.r.maximum = f.r.used + slots + 67108864 + uncovered - 1
				short := stageOpsRun(t, &stageOpsRedis{f.r}, runLuaRecoveryCoverageSource(t, scenario, true), keys, args)
				if short.runtimeErr != nil || short.raw != bootLuaErrorReply("ERR CRAWL_V2_MEMORY_HEADROOM_LOW") ||
					!reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
					t.Fatalf("one-byte memory shortage failed to reject the entire batch: %v / %v", short.raw, short.runtimeErr)
				}
				f.r.maximum++
				got := sharedLuaNoError(t, stageOpsRun(t, &stageOpsRedis{f.r}, runLuaRecoveryCoverageSource(t, scenario, true), keys, args))
				if err := ValidateOperationResponse(OperationRecoverExpired, got); err != nil {
					t.Fatal(err)
				}
				maintenanceLuaTrace(t, f.r)
				runLuaRecord(t, f.r)
				fields, calls := map[string]bool{}, map[string]int{}
				var logical, elements, newKeys uint64
				created := map[string]bool{}
				for _, call := range f.r.trace {
					if call.acl {
						continue
					}
					switch call.name {
					case "HSET":
						key := call.args[0]
						calls[key]++
						for i := 1; i < len(call.args); i += 2 {
							identity := key + "\x00" + call.args[i]
							if fields[identity] {
								t.Fatalf("field overwritten twice instead of coalesced: %s", identity)
							}
							fields[identity] = true
							if before.data[key].hash[call.args[i]] != call.args[i+1] {
								logical += uint64(len(call.args[i+1]))
							}
						}
					case "ZADD":
						key := call.args[0]
						if _, exists := before.data[key]; !exists && !created[key] {
							logical += uint64(len(key))
							newKeys++
							created[key] = true
						}
						for i := 1; i < len(call.args); i += 2 {
							logical += uint64(len(call.args[i]) + len(call.args[i+1]))
							elements++
						}
					}
				}
				if growth != 3*logical+1024*newKeys+256*elements {
					t.Fatal("G differs from independent final descriptor byte/element accounting")
				}
				runPartitions := 1
				if scenario == "lifecycle_partition" {
					runPartitions = 2
				}
				if calls[runLuaKey("group_open_jobs")] != groups || calls[runLuaKey("")] != runPartitions || calls[runLuaKey("recovery_outcome_counts")] != 1 || calls[runLuaKey("disposition_reason_counts")] != 1 {
					t.Fatalf("wrong disjoint-owner/shared-field partitions: %v", calls)
				}
				for _, job := range f.jobs {
					maintenanceLuaJobRecord(t, f.r, job.JobID)
					if f.r.data[runLuaKey("group_open_jobs")].hash[string(job.GroupID)] != "0" {
						t.Fatal("source-group open contribution was lost")
					}
				}
				if _, exists := f.r.data[StageSlotsKey]; exists {
					t.Fatal("core did not release the covered slot")
				}
			})
		}
	}
}

func TestRunLuaRecoveryCoverageFailsClosed(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"missing", "mixed_ordinary", "missing_set", "mixed_set", "numeric_coverage", "unknown", "foreign_plan", "losing_unknown", "wrong_real_owner", "blanket"} {
		t.Run(scenario, func(t *testing.T) {
			f, keys, args := runLuaRecoveryCoverageFixture(t, []bool{false, false}, 2)
			before := f.r.snapshot()
			result := stageOpsRun(t, &stageOpsRedis{f.r}, runLuaRecoveryCoverageSource(t, scenario, true), keys, args)
			if result.runtimeErr != nil {
				t.Fatal(result.runtimeErr)
			}
			expected := bootLuaErrorReply("ERR CRAWL_V2_INVALID_ARGUMENT")
			if result.raw != expected || !reflect.DeepEqual(before, f.r.snapshot()) || f.r.attempts != 0 {
				t.Fatalf("bad recovery coverage did not fail closed: %v", result.raw)
			}
			maintenanceLuaTrace(t, f.r)
		})
	}
}
