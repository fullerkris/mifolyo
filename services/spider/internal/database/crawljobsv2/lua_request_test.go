package crawljobsv2

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
	lua "github.com/yuin/gopher-lua"
)

// Actual P/Unicode15/URL/core/Run/Job/Request chunks, not Go replacements for
// validation or identity derivation. The command facade never contacts Redis.
type requestLuaVM struct{ *jobLuaVM }

const requestLuaPrefix = "mifolyo:crawl:v2:"

func requestLuaNew(t *testing.T) *requestLuaVM {
	t.Helper()
	vm := &requestLuaVM{jobLuaNew(t)} // also requires the Go 1.25.13 oracle
	vm.load(t, "Request", "ledger_request")
	vm.cj.RawSetString("Rate", vm.cj.RawGetString("Request").(*lua.LTable).RawGetString("Rate"))
	return vm
}

func requestLuaCompare(t *testing.T, vm *requestLuaVM, schema RecordSchema, record Record) bool {
	t.Helper()
	want := ValidateRecord(schema, record)
	encoded, err := EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	got, code := vm.invoke(t, "Schemas", "decode", lua.LString(schema), lua.LString(encoded))
	if (got != lua.LNil) != (want == nil) {
		t.Fatalf("schema=%s Lua=%v/%v Go=%v record=%v", schema, got, code, want, record)
	}
	if got == lua.LNil {
		if _, err := ParseErrorCode(code.String()); err != nil {
			t.Fatalf("non-closed rejection: %v", code)
		}
		return false
	}
	jobLuaAssertRecord(t, got, record)
	roundtrip, code := vm.invoke(t, "Schemas", "encode", got)
	if code != lua.LNil || roundtrip != lua.LString(encoded) {
		t.Fatalf("roundtrip: %v/%v", roundtrip, code)
	}
	n := got.(*lua.LTable).RawGetString("n").(*lua.LTable)
	for _, f := range record {
		d, err := ParseUnsignedDecimal(string(f.Value))
		if err == nil {
			number, _ := d.Uint64()
			if n.RawGetString(f.Name) != lua.LNumber(number) {
				t.Fatalf("missing numeric projection %s", f.Name)
			}
		} else if n.RawGetString(f.Name) != lua.LNil {
			t.Fatalf("invented numeric projection %s", f.Name)
		}
	}
	return true
}

func requestLuaRateRecord(t *testing.T, kind, witness string) Record {
	t.Helper()
	r := recordAuthorityRateScopeRecord(t)
	var id Digest
	var err error
	switch kind {
	case "global":
		id = DeriveGlobalScopeID()
		recordAuthoritySet(r, rateScopeEffectiveConcurrencyIndex, "2")
		recordAuthoritySet(r, rateScopeEffectiveIntervalMSIndex, "0")
		recordAuthoritySet(r, rateScopeNextAllowedMSIndex, "0")
	case "group":
		id, err = DeriveGroupScopeID(RateScopeID(witness))
	case "origin":
		id, err = DeriveOriginScopeID(CanonicalOrigin(witness))
	default:
		t.Fatal("bad test scope kind")
	}
	if err != nil {
		t.Fatal(err)
	}
	recordAuthoritySet(r, rateScopeIDIndex, string(id))
	recordAuthoritySet(r, rateScopeKindIndex, kind)
	recordAuthoritySet(r, rateScopeWitnessIndex, witness)
	return r
}

func TestRequestLuaSchemasAllStatesEveryField(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	records := map[string]Record{}
	for _, state := range []string{"pending", "started", "finished", "cancelled", "expired"} {
		records[state] = recordAuthorityReservationRecord(t, state)
	}
	expiredStarted := cloneRecord(records["started"])
	recordAuthoritySet(expiredStarted, reservationStateIndex, "expired")
	recordAuthoritySet(expiredStarted, reservationTerminalAtMSIndex, "1200")
	records["expired_started"] = expiredStarted
	records["global"] = requestLuaRateRecord(t, "global", "global")
	records["group"] = requestLuaRateRecord(t, "group", strings.Repeat("2", 32))
	records["origin"] = requestLuaRateRecord(t, "origin", "https://example.com:443")
	for name, record := range records {
		t.Run(name, func(t *testing.T) {
			schema := SchemaReservation
			if len(record) == 14 {
				schema = SchemaRateScope
			} else if len(record) != 34 {
				t.Fatal("incomplete record")
			}
			if !requestLuaCompare(t, vm, schema, record) {
				t.Fatal("valid baseline rejected")
			}
			definition, code := vm.invoke(t, "Schemas", "get", lua.LString(schema))
			if code != lua.LNil {
				t.Fatal(code)
			}
			def := definition.(*lua.LTable)
			for i, field := range record {
				if def.RawGetString("names").(*lua.LTable).RawGetInt(i+1) != lua.LString(field.Name) {
					t.Fatal("schema order")
				}
				for _, value := range []string{"", "0", "00", "-1", "1e0", "9007199254740992", "not-a-value", "\xff", "\x00"} {
					candidate := cloneRecord(record)
					recordAuthoritySet(candidate, i, value)
					t.Run(field.Name+"/"+fmt.Sprintf("%x", value), func(t *testing.T) {
						requestLuaCompare(t, vm, schema, candidate)
					})
				}
				missing := append(cloneRecord(record[:i]), cloneRecord(record[i+1:])...)
				requestLuaCompare(t, vm, schema, missing)
				wrongName := cloneRecord(record)
				wrongName[i].Name += "_unknown"
				requestLuaCompare(t, vm, schema, wrongName)
				// Bounds are enforced by the actual Redis reader and codec, not
				// just by the lexical validator's happy path.
				tooLong := cloneRecord(record)
				bound := int(def.RawGetString("bounds").(*lua.LTable).RawGetInt(i + 1).(lua.LNumber))
				recordAuthoritySet(tooLong, i, strings.Repeat("x", bound+1))
				requestLuaCompare(t, vm, schema, tooLong)
			}
			requestLuaCompare(t, vm, schema, append(cloneRecord(record), textField("extra", "0")))
		})
	}
}

func TestRequestLuaNumericRelationsAndOriginOracle(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	for _, field := range []int{reservationCreatedAtMSIndex, reservationStartedAtMSIndex, reservationTerminalAtMSIndex,
		reservationDeliveryAttemptsAfterStartIndex, reservationJobStartsAfterStartIndex, reservationRunStartsAfterStartIndex,
		reservationGroupStartsAfterStartIndex, reservationExpiresAtMSIndex} {
		for _, value := range []string{"0", "1", "2", "3", "4", "10", "11", "99", "100", "199", "200", "299", "300", "999", "1000", "1001", "9007199254740991"} {
			for _, state := range []string{"pending", "started", "finished", "cancelled", "expired"} {
				r := recordAuthorityReservationRecord(t, state)
				recordAuthoritySet(r, field, value)
				requestLuaCompare(t, vm, SchemaReservation, r)
			}
		}
	}
	for _, origin := range []string{
		"https://example.com:443", "http://example.com:80", "https://example.com:8443", "http://localhost:80",
		"https://xn--bcher-kva.example:443", "https://example.com", "https://example.com:0443", "https://EXAMPLE.com:443",
		"https://example.com.:443", "https://example.com:443/", "https://example.com:443?", "https://example.com:443#",
		"https://user@example.com:443", "https://127.0.0.1:443", "http://[::1]:80", "https://bücher.example:443",
		"https://example.com:0", "https://example.com:65536", "https://example.com:65535", "ftp://example.com:21",
		"https://a\\b:443", "https://a%2eb:443", "https://example.com:443\n", "https://example.com:443/path",
	} {
		want, err := DeriveOriginScopeID(CanonicalOrigin(origin))
		got, code := vm.invoke(t, "Rate", "origin_scope", lua.LString(origin))
		if (got != lua.LNil) != (err == nil) || err == nil && got != lua.LString(want) {
			t.Fatalf("origin %q: %v/%v Go=%v/%v", origin, got, code, want, err)
		}
	}
	for _, url := range []string{"https://127.0.0.1/", "http://[::1]/", "https://example.com/a%FF", "https://example.com/"} {
		id := JobID(utils.URLIDV1(url))
		want, err := DeriveTargetDigest(RequestTarget{URLID: id, CanonicalURL: url})
		got, code := vm.invoke(t, "Request", "target_digest", lua.LString(id), lua.LString(url))
		if (got != lua.LNil) != (err == nil) || err == nil && got != lua.LString(want) {
			t.Fatalf("target %q: %v/%v Go=%v", url, got, code, err)
		}
	}
	for _, last := range []uint64{0, 1, MaxExactInteger - 3600000, MaxExactInteger - 99, MaxExactInteger} {
		for _, interval := range []uint64{0, 1, 100, 3600000, 3600001} {
			for _, next := range []uint64{0, 100, MaxExactInteger} {
				r := requestLuaRateRecord(t, "origin", "https://example.com:443")
				for field, value := range map[int]uint64{rateScopeLastStartedAtMSIndex: last, rateScopeUpdatedAtMSIndex: MaxExactInteger,
					rateScopeEffectiveIntervalMSIndex: interval, rateScopeNextAllowedMSIndex: next} {
					recordAuthoritySet(r, field, strconv.FormatUint(value, 10))
				}
				requestLuaCompare(t, vm, SchemaRateScope, r)
			}
		}
	}
}

type requestLuaFixture struct {
	run, job, reservation Record
	groups                []PolicyGroup
	source                SourceJob
	intent                ReservationIntent
	policy                RunPolicyAuthority
	now                   uint64
}

func requestLuaFixtureNew(t *testing.T, state string) *requestLuaFixture {
	t.Helper()
	source := jobLuaSourceValue(t, "https://example.com/path")
	source.Depth, source.Decision.Depth = 2, 2
	group := jobLuaGroup(source)
	run := recordAuthorityRunRecord(t, "active")
	groupDigest, err := DerivePolicyGroupMapDigest([]PolicyGroup{group})
	if err != nil {
		t.Fatal(err)
	}
	for field, value := range map[int]string{runPolicyGroupMapSHA256Index: string(groupDigest), runJobCountIndex: "2", runOpenJobCountIndex: "2",
		runRequestStartsIndex: "0", runReservationCreationsTotalIndex: "1", runPendingRequestReservationsIndex: "1", runClaimsTotalIndex: "1",
		runCompletedTotalIndex: "0", runDeadTotalIndex: "0", runOutputCommitsTotalIndex: "0", runLastRequestStartedAtMSIndex: "0",
		runLastTerminalTransitionAtMSIndex: "0", runAuthorizationExpiresAtMSIndex: "1000000"} {
		recordAuthoritySet(run, field, value)
	}
	lease := LeaseIdentity{RunID: RunID(strings.Repeat("1", 32)), JobID: source.JobID, OwnerID: OwnerID(strings.Repeat("4", 32)),
		Token: LeaseToken(strings.Repeat("5", 64)), Fence: 1}
	policy, err := (transportAuthority{seal: &redisTransportAuthoritySeal}).parseRunPolicyAuthority(lease.RunID, run, []PolicyGroup{group})
	if err != nil {
		t.Fatal(err)
	}
	intent := ReservationIntent{Lease: lease, RequestOrdinal: 1, Target: RequestTarget{URLID: source.JobID, CanonicalURL: source.CanonicalURL},
		CrawlPolicyDigest: Digest(strings.Repeat("a", 64)), Decision: source.Decision}
	id, err := DeriveReservationID(policy, intent)
	if err != nil {
		t.Fatal(err)
	}
	reservation := recordAuthorityReservationRecord(t, state)
	for field, value := range map[int]string{reservationIDIndex: string(id), reservationJobIDIndex: string(source.JobID),
		reservationCreatedAtMSIndex: "500", reservationExpiresAtMSIndex: "60500"} {
		recordAuthoritySet(reservation, field, value)
	}
	job := recordAuthorityJobRecord(t, "ready")
	sourceRecord, _ := completeSourceJobRecord(source)
	jobLuaApplySource(job, sourceRecord)
	for field, value := range map[int]string{jobStateIndex: "leased", jobLeaseOwnerIndex: string(lease.OwnerID), jobLeaseTokenIndex: string(lease.Token),
		jobLeaseFenceIndex: "1", jobClaimCountIndex: "1", jobNextRequestOrdinalIndex: "2", jobActiveReservationIDIndex: string(id),
		jobLeaseStartedAtMSIndex: "500", jobLeaseExpiresAtMSIndex: "60500", jobUpdatedAtMSIndex: "550"} {
		recordAuthoritySet(job, field, value)
	}
	if state == "started" || state == "finished" {
		recordAuthoritySet(reservation, reservationStartedAtMSIndex, "550")
		recordAuthoritySet(reservation, reservationRunStartsAfterStartIndex, "1")
		for field, value := range map[int]string{jobRequestStartsIndex: "1", jobDeliveryAttemptsIndex: "1", jobLeaseDeliveryStartedIndex: "1",
			jobLastRequestStartedAtMSIndex: "550", jobLastDocumentRequestStartedAtMSIndex: "550", jobLastDocumentRequestFenceIndex: "1",
			jobLastDocumentTargetURLIDIndex: string(source.JobID), jobLastDocumentTargetURLIndex: source.CanonicalURL,
			jobLastDocumentTargetDigestIndex: string(source.Decision.TargetDigest)} {
			recordAuthoritySet(job, field, value)
		}
		recordAuthoritySet(run, runRequestStartsIndex, "1")
		recordAuthoritySet(run, runPendingRequestReservationsIndex, "0")
		recordAuthoritySet(run, runStartedRequestReservationsIndex, "1")
		recordAuthoritySet(run, runLastRequestStartedAtMSIndex, "550")
	}
	now := uint64(600)
	if state == "finished" || state == "cancelled" || state == "expired" {
		terminal := "580"
		if state == "expired" {
			terminal, now = "60500", 60600
		}
		recordAuthoritySet(reservation, reservationTerminalAtMSIndex, terminal)
		recordAuthoritySet(job, jobActiveReservationIDIndex, "")
		recordAuthoritySet(job, jobUpdatedAtMSIndex, terminal)
		recordAuthoritySet(run, runLastActivityAtMSIndex, terminal)
		recordAuthoritySet(run, runPendingRequestReservationsIndex, "0")
		recordAuthoritySet(run, runStartedRequestReservationsIndex, "0")
	}
	for schema, record := range map[RecordSchema]Record{SchemaRun: run, SchemaJob: job, SchemaReservation: reservation} {
		if err := ValidateRecord(schema, record); err != nil {
			t.Fatalf("fixture %s/%s: %v", state, schema, err)
		}
	}
	return &requestLuaFixture{run: run, job: job, reservation: reservation, source: source, groups: []PolicyGroup{group}, intent: intent, policy: policy, now: now}
}

func requestLuaBinding(t *testing.T, vm *requestLuaVM, f *requestLuaFixture) (*lua.LTable, *lua.LTable) {
	t.Helper()
	groups := vm.state.NewTable()
	for i, group := range f.groups {
		record, err := policyGroupRecord(group)
		if err != nil {
			t.Fatal(err)
		}
		groups.RawSetInt(i+1, jobLuaEncoded(t, record))
	}
	g, code := vm.invoke(t, "Schemas", "groups", groups)
	if code != lua.LNil {
		t.Fatal(code)
	}
	run := vm.state.NewTable()
	run.RawSetString("run_id", lua.LString(f.intent.Lease.RunID))
	run.RawSetString("v", jobLuaValues(vm.jobLuaVM, f.run))
	run.RawSetString("groups", g)
	job := vm.state.NewTable()
	job.RawSetString("v", jobLuaValues(vm.jobLuaVM, f.job))
	return run, job
}

func TestRequestLuaIntentAndBindingConstructors(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	f := requestLuaFixtureNew(t, "pending")
	run, job := requestLuaBinding(t, vm, f)
	wireFixture := newWireOracleFixture(t)
	for _, kind := range []RequestKind{RequestRobots, RequestDocument, RequestRedirect, RequestRenderResource} {
		for _, ordinal := range []uint64{1, 10, 100} {
			intent := f.intent
			intent.RequestOrdinal = ordinal
			intent.Decision.RequestKind = kind
			if kind == RequestRobots {
				intent.Target = RequestTarget{URLID: JobID(utils.URLIDV1("https://example.com/robots.txt")), CanonicalURL: "https://example.com/robots.txt"}
				intent.Decision, _ = NewPolicyDecision(PolicyDecisionInput{RequestKind: kind, Target: intent.Target, Depth: f.source.Depth,
					GroupID: f.source.GroupID, RateScopeID: f.source.RateScopeID, GroupConcurrency: 3, OriginConcurrency: 3,
					GroupIntervalMS: 100, OriginIntervalMS: 100})
			}
			fields, err := reservationIntentFields(f.policy, intent)
			if err != nil {
				t.Fatal(err)
			}
			// Production Go field encoder, with actual authenticated run binding.
			leaseFields, _ := operationWireLeaseFields(intent.Lease)
			semantic := append(leaseFields, fields...)
			wire, err := NewReserveRequestWireRequest(wireOracleGate(t, wireFixture, OperationReserveRequest, wireOracleActive), f.policy, intent)
			if err != nil || !reflect.DeepEqual(wire.semantic, semantic) {
				t.Fatalf("production reserve constructor: %v", err)
			}
			values := jobLuaValues(vm.jobLuaVM, semantic)
			got, code := vm.invoke(t, "Request", "intent", values, run, job, lua.LFalse)
			if code != lua.LNil {
				t.Fatalf("intent %s/%d: %v", kind, ordinal, code)
			}
			wantID, _ := DeriveReservationID(f.policy, intent)
			wantToken, _ := DeriveTokenDigest(intent.Lease)
			if got.(*lua.LTable).RawGetString("reservation_id") != lua.LString(wantID) ||
				got.(*lua.LTable).RawGetString("token_digest") != lua.LString(wantToken) {
				t.Fatal("control identity differs from Go")
			}
			jobLuaAssertRecord(t, got, semantic)
			built, code := vm.invoke(t, "Request", "build_intent", run, job, jobLuaValues(vm.jobLuaVM, leaseFields), lua.LString(kind),
				lua.LString(intent.Target.CanonicalURL), lua.LString(intent.Decision.GroupID), lua.LString(strconv.FormatUint(ordinal, 10)), lua.LFalse)
			if code != lua.LNil {
				t.Fatalf("builder: %v", code)
			}
			jobLuaAssertRecord(t, built, semantic)
			initial, code := vm.invoke(t, "Request", "intent", values, run, job, lua.LTrue)
			_, claimErr := DeriveTryClaimTransitionID(f.policy, TryClaimTransitionInput{Job: f.source, Lease: intent.Lease,
				ExpectedPriorFence: 0, InitialIntent: intent})
			if (initial != lua.LNil) != (claimErr == nil) {
				t.Fatalf("initial binding %s: Lua %v Go %v", kind, code, claimErr)
			}
			claimWire, wireErr := NewTryClaimWireRequest(wireOracleGate(t, wireFixture, OperationTryClaim, wireOracleActive), f.policy,
				TryClaimTransitionInput{Job: f.source, Lease: intent.Lease, ExpectedPriorFence: 0, InitialIntent: intent})
			if (initial != lua.LNil) != (wireErr == nil) || wireErr == nil && claimWire.keyContext.reservationID != wantID {
				t.Fatalf("production claim constructor: %v", wireErr)
			}
			for op, constructor := range map[OperationName]func(TransportGate, RunPolicyAuthority, ReservationIntent) (OperationWireRequest, error){
				OperationStartRequest: NewStartRequestWireRequest, OperationFinishRequest: NewFinishRequestWireRequest,
				OperationCancelReservation: NewCancelReservationWireRequest,
			} {
				wire, err := constructor(wireOracleGate(t, wireFixture, op, wireOracleActive), f.policy, intent)
				if err != nil || wire.keyContext.reservationID != wantID || len(wire.semantic) != 6 {
					t.Fatalf("production %s constructor: %v", op, err)
				}
			}
			renew, err := NewRenewLeaseWireRequest(wireOracleGate(t, wireFixture, OperationRenewLease, wireOracleActive), intent.Lease)
			if err != nil {
				t.Fatal(err)
			}
			if token, code := vm.invoke(t, "Request", "token_digest", jobLuaValues(vm.jobLuaVM, renew.semantic)); code != lua.LNil || token != lua.LString(wantToken) {
				t.Fatalf("production renewal identity: %v", code)
			}
			for i, field := range semantic {
				bad := cloneRecord(semantic)
				recordAuthoritySet(bad, i, "wrong")
				if value, _ := vm.invoke(t, "Request", "intent", jobLuaValues(vm.jobLuaVM, bad), run, job, lua.LFalse); value != lua.LNil {
					t.Fatalf("bad intent field %s accepted", field.Name)
				}
			}
		}
	}
	// A standalone reservation binds the supplied decision digest, but cannot
	// invent its absent depth. Re-ID a wrong-depth intent: schema accepts, context
	// and intent validator must reject using the OWNING job's immutable depth.
	wrong := f.intent
	wrong.Decision.Depth++
	id, _ := DeriveReservationID(f.policy, wrong)
	digest, _ := DerivePolicyDecisionDigest(wrong.Decision)
	bad := cloneRecord(f.reservation)
	recordAuthoritySet(bad, reservationIDIndex, string(id))
	recordAuthoritySet(bad, reservationPolicyDecisionSHA256Index, string(digest))
	if !requestLuaCompare(t, vm, SchemaReservation, bad) {
		t.Fatal("standalone validator invented depth")
	}
	fields, _ := reservationIntentFields(f.policy, wrong)
	leaseFields, _ := operationWireLeaseFields(wrong.Lease)
	if value, _ := vm.invoke(t, "Request", "intent", jobLuaValues(vm.jobLuaVM, append(leaseFields, fields...)), run, job, lua.LFalse); value != lua.LNil {
		t.Fatal("owning depth ignored")
	}
}

func requestLuaStore(t *testing.T, f *requestLuaFixture) (*sharedLuaRedis, []string) {
	t.Helper()
	r := sharedLuaNewRedis()
	r.now = f.now
	base := requestLuaPrefix + "run:" + string(f.intent.Lease.RunID)
	jobID := string(f.intent.Lease.JobID)
	id := string(f.reservation[reservationIDIndex].Value)
	r.setHash(base, f.run)
	r.setHash(base+":job:"+jobID, f.job)
	key := requestLuaPrefix + "reservation:" + id
	r.setHash(key, f.reservation)
	state := string(f.reservation[reservationStateIndex].Value)
	if state != "pending" && state != "started" {
		e := r.data[key]
		at, _ := strconv.ParseInt(string(f.reservation[reservationTerminalAtMSIndex].Value), 10, 64)
		e.expireAt = at + 86400000
		r.data[key] = e
	}
	keys := []string{base, base + ":job:" + jobID, key, RateScopesKey}
	for _, pair := range [][2]string{{"group_limits", "request_start_limit"}, {"group_rate_scope_ids", "rate_scope_id"},
		{"group_scope_ids", "group_scope_id"}, {"group_concurrency", "concurrency"}, {"group_interval_ms", "interval_ms"},
		{"group_started", ""}, {"group_pending", ""}, {"group_active_started", ""}, {"group_open_jobs", ""}} {
		values := Record{}
		for _, g := range f.groups {
			group, _ := policyGroupRecord(g)
			v := operationWireSemanticValues(group)[pair[1]]
			if pair[1] == "" {
				v = "0"
				index := map[string]int{"group_started": runRequestStartsIndex, "group_pending": runPendingRequestReservationsIndex,
					"group_active_started": runStartedRequestReservationsIndex, "group_open_jobs": runOpenJobCountIndex}[pair[0]]
				charged := GroupID(f.reservation[reservationGroupIDIndex].Value)
				if pair[0] == "group_open_jobs" {
					charged = GroupID(f.job[jobGroupIDIndex].Value)
				}
				if charged == g.GroupID {
					v = string(f.run[index].Value)
				}
			}
			values = append(values, textField(string(g.GroupID), v))
		}
		key := base + ":" + pair[0]
		keys = append(keys, key)
		r.setHash(key, values)
	}
	for _, name := range jobLuaIndexNames {
		key := base + ":" + name
		if name == "active_leases" {
			key = ActiveLeasesKey
		}
		keys = append(keys, key)
	}
	// The other source job is unselected but counted, as it would be on the
	// actual worker key plan. No cardinality-only fact authenticates its fields.
	peer := jobLuaSourceValue(t, "https://example.com/unselected")
	peerRecord := recordAuthorityJobRecord(t, "ready")
	peerSource, _ := completeSourceJobRecord(peer)
	jobLuaApplySource(peerRecord, peerSource)
	r.setHash(base+":job:"+string(peer.JobID), peerRecord)
	r.setSet(base+":jobs", []string{jobID, string(peer.JobID)})
	r.setZSet(base+":job_order", map[string]float64{jobID: 0, string(peer.JobID): 0})
	r.setZSet(base+":ready", map[string]float64{string(peer.JobID): 0})
	r.setZSet(base+":ready_at", map[string]float64{string(peer.JobID): 200})
	leaseExpiry, _ := strconv.ParseFloat(string(f.job[jobLeaseExpiresAtMSIndex].Value), 64)
	leaseStarted, _ := strconv.ParseFloat(string(f.job[jobLeaseStartedAtMSIndex].Value), 64)
	r.setZSet(base+":leased", map[string]float64{jobID: leaseExpiry})
	r.setZSet(base+":leased_at", map[string]float64{jobID: leaseStarted})
	r.setZSet(ActiveLeasesKey, map[string]float64{string(f.intent.Lease.RunID) + ":" + jobID: leaseExpiry})
	inventory := map[string]float64{}
	for _, kind := range []string{"global", "group", "origin"} {
		witness := "global"
		if kind == "group" {
			witness = string(f.reservation[reservationRateScopeIDIndex].Value)
		} else if kind == "origin" {
			origin, err := DeriveCanonicalOrigin(string(f.reservation[reservationCanonicalTargetURLIndex].Value))
			if err != nil {
				t.Fatal(err)
			}
			witness = string(origin)
		}
		scope := requestLuaRateRecord(t, kind, witness)
		started := string(f.reservation[reservationStartedAtMSIndex].Value)
		at, _ := strconv.ParseUint(started, 10, 64)
		for field, value := range map[int]string{rateScopeLastStartedAtMSIndex: started, rateScopeUpdatedAtMSIndex: string(f.run[runLastActivityAtMSIndex].Value),
			rateScopeNextAllowedMSIndex: "0"} {
			recordAuthoritySet(scope, field, value)
		}
		if kind != "global" && at != 0 {
			recordAuthoritySet(scope, rateScopeNextAllowedMSIndex, strconv.FormatUint(at+100, 10))
		}
		if state == "pending" || state == "started" {
			recordAuthoritySet(scope, rateScopeActiveCountIndex, "1")
			field := rateScopePendingCountIndex
			if state == "started" {
				field = rateScopeStartedCountIndex
			}
			recordAuthoritySet(scope, field, "1")
		}
		scopeID := string(scope[rateScopeIDIndex].Value)
		key := requestLuaPrefix + "rate:" + scopeID
		r.setHash(key, scope)
		keys = append(keys, key, key+":active", key+":pending", key+":started")
		inventory[scopeID], _ = strconv.ParseFloat(string(scope[rateScopeUpdatedAtMSIndex].Value), 64)
		if state == "pending" || state == "started" {
			expiry, _ := strconv.ParseFloat(string(f.reservation[reservationExpiresAtMSIndex].Value), 64)
			r.setZSet(key+":active", map[string]float64{id: expiry})
			r.setZSet(key+":"+state, map[string]float64{id: expiry})
		}
	}
	r.setZSet(RateScopesKey, inventory)
	sort.Strings(keys)
	return r, keys
}

// An explicit, test-owned source spec using an actually permitted boot_only
// operation. It grants the held keys through Context.open, NOT by modifying
// Wire.modes/ctx.allowed or replacing the production reader. This is deliberately
// not a worker wire-plan implementation or a claim about the production gate.
func requestLuaOpen(t *testing.T, vm *requestLuaVM, r *sharedLuaRedis, keys []string, runID string) *lua.LTable {
	t.Helper()
	redis := vm.state.NewTable()
	redis.RawSetString("call", vm.state.NewFunction(func(l *lua.LState) int { return r.command(l, false) }))
	vm.env.RawSetString("redis", redis)
	spec := vm.state.NewTable()
	spec.RawSetString("operation", lua.LString(OperationInstallCandidateMarkers))
	spec.RawSetString("fields", bootLuaStrings(vm.state, []string{"run_id"}))
	spec.RawSetString("tail", lua.LString("none"))
	spec.RawSetString("request_limit", lua.LNumber(2097152))
	names := make([]string, len(keys))
	for i, key := range keys {
		names[i] = fmt.Sprintf("held_%d", i)
		if key == requestLuaPrefix+"run:"+runID {
			names[i] = "run"
		}
	}
	spec.RawSetString("key_names", bootLuaStrings(vm.state, names))
	spec.RawSetString("key_values", bootLuaStrings(vm.state, keys))
	ctx, code := vm.invoke(t, "Context", "open", spec, bootLuaStrings(vm.state, keys),
		bootLuaStrings(vm.state, []string{"boot_only", strings.Repeat("1", 32), "", "", "", "", "", runID}))
	if code != lua.LNil {
		t.Fatal(code)
	}
	return ctx.(*lua.LTable)
}

func requestLuaSelect(t *testing.T, vm *requestLuaVM, ctx *lua.LTable, f *requestLuaFixture, keys []string, omit string) lua.LValue {
	t.Helper()
	id := string(f.reservation[reservationIDIndex].Value)
	base := requestLuaPrefix + "run:" + string(f.intent.Lease.RunID)
	for _, key := range keys {
		if key == omit {
			continue
		}
		var value, code lua.LValue
		schema := ""
		switch {
		case key == base:
			schema = "run"
		case key == base+":job:"+string(f.intent.Lease.JobID):
			schema = "job"
		case strings.HasPrefix(key, requestLuaPrefix+"reservation:"):
			schema = "reservation"
		case strings.HasPrefix(key, requestLuaPrefix+"rate:") && strings.Count(key, ":") == 4:
			schema = "rate_scope"
		}
		if schema != "" {
			value, code = vm.invoke(t, "Read", "fixed_hash", ctx, lua.LString(key), lua.LString(schema))
		} else if strings.Contains(key, ":group_") {
			value, code = vm.invoke(t, "Read", "dynamic_hash", ctx, lua.LString(key), lua.LNumber(64), lua.LNumber(128), lua.LNumber(64))
		} else {
			kind, maximum := "zset", 10000
			members := []string{string(f.intent.Lease.JobID)}
			if key == base+":jobs" {
				kind = "set"
			}
			if strings.HasPrefix(key, requestLuaPrefix+"rate:") {
				maximum, members = 32, []string{id}
			}
			if key == RateScopesKey {
				maximum, members = 100000, []string{string(f.reservation[reservationGlobalScopeIDIndex].Value),
					string(f.reservation[reservationGroupScopeIDIndex].Value), string(f.reservation[reservationOriginScopeIDIndex].Value)}
			}
			if key == ActiveLeasesKey {
				members = []string{string(f.intent.Lease.RunID) + ":" + string(f.intent.Lease.JobID)}
			}
			value, code = vm.invoke(t, "Read", "members", ctx, lua.LString(key), lua.LString(kind), bootLuaStrings(vm.state, members), lua.LNumber(maximum), lua.LNumber(97))
		}
		if code != lua.LNil {
			return code
		}
		if value == lua.LNil {
			t.Fatal("reader returned nil without code")
		}
		if omit != key+"/ttl" {
			if _, code := vm.invoke(t, "Read", "ttl", ctx, lua.LString(key)); code != lua.LNil {
				return code
			}
		}
	}
	return lua.LNil
}

func requestLuaCheck(t *testing.T, vm *requestLuaVM, f *requestLuaFixture, method, omit string,
	mutate func(*sharedLuaRedis), edit func(*lua.LTable, *lua.LTable, *lua.LTable)) (lua.LValue, lua.LValue) {
	t.Helper()
	r, keys := requestLuaStore(t, f)
	if mutate != nil {
		mutate(r)
	}
	ctx := requestLuaOpen(t, vm, r, keys, string(f.intent.Lease.RunID))
	if code := requestLuaSelect(t, vm, ctx, f, keys, omit); code != lua.LNil {
		return lua.LNil, code
	}
	view := vm.state.NewTable()
	view.RawSetString("ctx", ctx)
	id := lua.LString(f.reservation[reservationIDIndex].Value)
	view.RawSetString("reservation_ids", bootLuaStrings(vm.state, []string{string(id)}))
	run, job := requestLuaBinding(t, vm, f)
	if edit != nil {
		edit(view, run, job)
	}
	before := len(r.trace)
	beforeState := r.snapshot()
	var got, code lua.LValue
	switch method {
	case "check_live":
		got, code = vm.invoke(t, "Request", method, view, run, job, id)
	case "check_receipt":
		got, code = vm.invoke(t, "Request", method, view, id)
	case "check_scope":
		got, code = vm.invoke(t, "Rate", method, view, lua.LString(f.reservation[reservationGroupScopeIDIndex].Value))
	default:
		t.Fatal("unknown helper")
	}
	if len(r.trace) != before || !reflect.DeepEqual(beforeState, r.snapshot()) || r.writes != 0 {
		t.Fatal("pure receipt helper read or mutated Redis")
	}
	return got, code
}

func TestRequestLuaPrivateReceiptsAndCurrentLease(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	for _, state := range []string{"pending", "started", "finished", "cancelled", "expired"} {
		f := requestLuaFixtureNew(t, state)
		got, code := requestLuaCheck(t, vm, f, "check_receipt", "", nil, nil)
		if code != lua.LNil {
			t.Fatalf("receipt %s: %v", state, code)
		}
		jobLuaAssertRecord(t, got, f.reservation)
		if _, code := requestLuaCheck(t, vm, f, "check_scope", "", nil, nil); code != lua.LNil {
			t.Fatalf("selected reservation state %s: %v", state, code)
		}
		_, code = requestLuaCheck(t, vm, f, "check_live", "", nil, nil)
		if (code == lua.LNil) != (state == "pending" || state == "started") {
			t.Fatalf("live %s: %v", state, code)
		}
	}
	f := requestLuaFixtureNew(t, "started")
	_, keys := requestLuaStore(t, f)
	for _, omit := range keys {
		got, code := requestLuaCheck(t, vm, f, "check_live", omit, nil, nil)
		if got != lua.LNil || code == lua.LNil {
			t.Fatalf("missing private receipt %s accepted", omit)
		}
	}
	for _, omit := range []string{requestLuaPrefix + "reservation:" + string(f.reservation[reservationIDIndex].Value),
		requestLuaPrefix + "rate:" + string(f.reservation[reservationGroupScopeIDIndex].Value), RateScopesKey} {
		if got, _ := requestLuaCheck(t, vm, f, "check_live", omit+"/ttl", nil, nil); got != lua.LNil {
			t.Fatalf("missing TTL %s accepted", omit)
		}
	}
	// Public projections/facts/numeric fields cannot fabricate or modify evidence.
	got, code := requestLuaCheck(t, vm, f, "check_live", "", nil, func(view, run, job *lua.LTable) {
		view.RawSetString("reservation", jobLuaValues(vm.jobLuaVM, f.reservation))
		view.RawSetString("facts", vm.state.NewTable())
		run.RawSetString("n", vm.state.NewTable())
		job.RawSetString("n", vm.state.NewTable())
	})
	if got == lua.LNil || code != lua.LNil {
		t.Fatalf("public conveniences used as authority: %v", code)
	}
	if got, _ := requestLuaCheck(t, vm, f, "check_live", "", nil, func(_ *lua.LTable, run, _ *lua.LTable) {
		run.RawGetString("v").(*lua.LTable).RawSetString("request_starts", lua.LString("2"))
	}); got != lua.LNil {
		t.Fatal("changed run record accepted")
	}
	// Logical expiry is retained for recovery, not deleted or treated as an
	// absent reservation. Time/authorization gates are separate from consistency.
	f.now = 60500
	got, code = requestLuaCheck(t, vm, f, "check_live", "", nil, nil)
	if code != lua.LNil || got.(*lua.LTable).RawGetString("eligible_now") != lua.LFalse ||
		got.(*lua.LTable).RawGetString("logical_expired") != lua.LTrue {
		t.Fatalf("logical expiry: %v/%v", got, code)
	}
}

func TestRequestLuaHistoricalStartDoesNotUseNewFenceBaseline(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	f := requestLuaFixtureNew(t, "finished")
	for field, value := range map[int]string{jobLeaseFenceIndex: "2", jobClaimCountIndex: "2", jobLeaseOwnerIndex: strings.Repeat("6", 32),
		jobLeaseTokenIndex: strings.Repeat("7", 64), jobLeaseRequestStartsBaselineIndex: "1", jobLeaseDeliveryStartedIndex: "0",
		jobNextRequestOrdinalIndex: "3", jobLeaseStartedAtMSIndex: "590", jobUpdatedAtMSIndex: "590"} {
		recordAuthoritySet(f.job, field, value)
	}
	for field, value := range map[int]string{runClaimsTotalIndex: "2", runReservationCreationsTotalIndex: "2", runLastActivityAtMSIndex: "590"} {
		recordAuthoritySet(f.run, field, value)
	}
	got, code := requestLuaCheck(t, vm, f, "check_receipt", "", nil, nil)
	if code != lua.LNil {
		t.Fatal(code)
	}
	jobLuaAssertRecord(t, got, f.reservation)
	if got.(*lua.LTable).RawGetString("n").(*lua.LTable).RawGetString("job_starts_after_start") != lua.LNumber(1) {
		t.Fatal("historical post-start snapshot replaced")
	}
	if got, _ := requestLuaCheck(t, vm, f, "check_live", "", nil, nil); got != lua.LNil {
		t.Fatal("historical tombstone authorized current ownership")
	}
	// The decision must still use the owning job depth; re-ID'd wrong depth is
	// structurally valid yet cannot become a historical receipt.
	wrong := f.intent
	wrong.Decision.Depth++
	id, _ := DeriveReservationID(f.policy, wrong)
	digest, _ := DerivePolicyDecisionDigest(wrong.Decision)
	recordAuthoritySet(f.reservation, reservationIDIndex, string(id))
	recordAuthoritySet(f.reservation, reservationPolicyDecisionSHA256Index, string(digest))
	if got, _ := requestLuaCheck(t, vm, f, "check_receipt", "", nil, nil); got != lua.LNil {
		t.Fatal("historical receipt invented decision depth")
	}
}

func TestRequestLuaRateCountsMembershipAndGrandfathering(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	f := requestLuaFixtureNew(t, "pending")
	id := string(f.reservation[reservationIDIndex].Value)
	key := requestLuaPrefix + "rate:" + string(f.reservation[reservationGroupScopeIDIndex].Value)
	for _, test := range []struct {
		name string
		edit func(*sharedLuaRedis)
	}{
		{"counter", func(r *sharedLuaRedis) { r.data[key].hash["active_count"] = "2" }},
		{"inventory", func(r *sharedLuaRedis) {
			delete(r.zsets[RateScopesKey], string(f.reservation[reservationGroupScopeIDIndex].Value))
		}},
		{"inventory time", func(r *sharedLuaRedis) {
			r.zsets[RateScopesKey][string(f.reservation[reservationGroupScopeIDIndex].Value)]++
		}},
		{"missing active", func(r *sharedLuaRedis) { r.removeKey(key + ":active") }},
		{"wrong expiry", func(r *sharedLuaRedis) { r.zsets[key+":pending"][id]++ }},
		{"both secondary", func(r *sharedLuaRedis) { r.setZSet(key+":started", map[string]float64{id: 60500}) }},
		{"fractional", func(r *sharedLuaRedis) { r.zsets[key+":active"][id] = 60500.5 }},
		{"negative zero", func(r *sharedLuaRedis) { r.zsets[key+":active"][id] = math.Copysign(0, -1) }},
		{"future scope", func(r *sharedLuaRedis) { r.data[key].hash["updated_at_ms"] = "601" }},
		{"relaxed concurrency", func(r *sharedLuaRedis) { r.data[key].hash["effective_concurrency"] = "4" }},
		{"relaxed interval", func(r *sharedLuaRedis) { r.data[key].hash["effective_interval_ms"] = "99" }},
		{"wrong witness", func(r *sharedLuaRedis) { r.data[key].hash["scope_witness"] = strings.Repeat("3", 32) }},
		{"scope expiry", func(r *sharedLuaRedis) { e := r.data[key]; e.expireAt = 1000; r.data[key] = e }},
		{"index expiry", func(r *sharedLuaRedis) { e := r.data[key+":active"]; e.expireAt = 1000; r.data[key+":active"] = e }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got, _ := requestLuaCheck(t, vm, f, "check_scope", "", test.edit, nil); got != lua.LNil {
				t.Fatal("corrupt scope accepted")
			}
		})
	}
	// Two members admitted under concurrency 3 / interval 0 may survive a
	// tightening to 1 / 100. Do not add active<=effective or pending<=1 rules.
	zero := requestLuaFixtureNew(t, "pending")
	zero.intent.Decision.GroupIntervalMS, zero.intent.Decision.OriginIntervalMS = 0, 0
	zero.groups[0].IntervalMS = 0
	zero.policy = newAuthenticatedTestRunPolicyAuthority(t, zero.intent.Lease.RunID, zero.intent.CrawlPolicyDigest,
		plainSHA256(testDenyAllRenderPolicyArtifact()), zero.groups)
	firstID, _ := DeriveReservationID(zero.policy, zero.intent)
	dd, _ := DerivePolicyDecisionDigest(zero.intent.Decision)
	recordAuthoritySet(zero.reservation, reservationIDIndex, string(firstID))
	recordAuthoritySet(zero.reservation, reservationPolicyDecisionSHA256Index, string(dd))
	recordAuthoritySet(zero.reservation, reservationGroupIntervalMSIndex, "0")
	recordAuthoritySet(zero.reservation, reservationOriginIntervalMSIndex, "0")
	second := cloneRecord(zero.reservation)
	other := zero.intent
	other.RequestOrdinal = 2
	secondID, _ := DeriveReservationID(zero.policy, other)
	recordAuthoritySet(second, reservationIDIndex, string(secondID))
	recordAuthoritySet(second, reservationRequestOrdinalIndex, "2")
	r, keys := requestLuaStore(t, zero)
	secondKey := requestLuaPrefix + "reservation:" + string(secondID)
	keys = append(keys, secondKey)
	r.setHash(secondKey, second)
	r.data[key].hash["effective_concurrency"] = "1"
	r.data[key].hash["active_count"], r.data[key].hash["pending_count"] = "2", "2"
	r.zsets[key+":active"][string(secondID)] = 60500
	r.zsets[key+":pending"][string(secondID)] = 60500
	ctx := requestLuaOpen(t, vm, r, keys, string(f.intent.Lease.RunID))
	if code := requestLuaSelect(t, vm, ctx, zero, keys, ""); code != lua.LNil {
		t.Fatal(code)
	}
	for _, suffix := range []string{"active", "pending", "started"} {
		if _, code := vm.invoke(t, "Read", "members", ctx, lua.LString(key+":"+suffix), lua.LString("zset"),
			bootLuaStrings(vm.state, []string{string(secondID)}), lua.LNumber(32), lua.LNumber(64)); code != lua.LNil {
			t.Fatal(code)
		}
	}
	view := vm.state.NewTable()
	view.RawSetString("ctx", ctx)
	view.RawSetString("reservation_ids", bootLuaStrings(vm.state, []string{string(firstID), string(secondID)}))
	got, code := vm.invoke(t, "Rate", "check_scope", view, lua.LString(f.reservation[reservationGroupScopeIDIndex].Value))
	if got == lua.LNil || code != lua.LNil {
		t.Fatalf("grandfathering rejected: %v", code)
	}
}

func TestRequestLuaTombstoneTimesAndNoImplicitPermissions(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	for _, state := range []string{"pending", "started", "finished", "cancelled", "expired"} {
		f := requestLuaFixtureNew(t, state)
		key := requestLuaPrefix + "reservation:" + string(f.reservation[reservationIDIndex].Value)
		for _, ttl := range []int64{-1, 0, 1, 86400000, 86400001} {
			got, _ := requestLuaCheck(t, vm, f, "check_receipt", "", func(r *sharedLuaRedis) {
				e := r.data[key]
				e.expireAt = -1
				if ttl >= 0 {
					e.expireAt = int64(r.now) + ttl
				}
				r.data[key] = e
			}, nil)
			want := (state == "pending" || state == "started") && ttl == -1
			if (got != lua.LNil) != want {
				t.Fatalf("TTL %s/%d accepted=%v", state, ttl, got != lua.LNil)
			}
		}
	}
	f := requestLuaFixtureNew(t, "pending")
	r, keys := requestLuaStore(t, f)
	ctx := requestLuaOpen(t, vm, r, keys[:1], string(f.intent.Lease.RunID))
	key := requestLuaPrefix + "reservation:" + string(f.reservation[reservationIDIndex].Value)
	ctx.RawGetString("allowed").(*lua.LTable).RawSetString(key, lua.LTrue)
	if got, _ := vm.invoke(t, "Read", "fixed_hash", ctx, lua.LString(key), lua.LString("reservation")); got != lua.LNil {
		t.Fatal("public allowed broadened the actual wire-key reader")
	}
	view := vm.state.NewTable()
	view.RawSetString("ctx", ctx)
	view.RawSetString("reservation", jobLuaValues(vm.jobLuaVM, f.reservation))
	if got, _ := vm.invoke(t, "Request", "check_receipt", view, lua.LString(f.reservation[reservationIDIndex].Value)); got != lua.LNil {
		t.Fatal("forged receipt accepted")
	}
	vm.invoke(t, "Context", "seal", ctx)
	if got, _ := vm.invoke(t, "Request", "check_receipt", view, lua.LString(f.reservation[reservationIDIndex].Value)); got != lua.LNil {
		t.Fatal("sealed context accepted")
	}
}

func requestLuaRebind(t *testing.T, f *requestLuaFixture) {
	t.Helper()
	digest, err := DerivePolicyGroupMapDigest(f.groups)
	if err != nil {
		t.Fatal(err)
	}
	recordAuthoritySet(f.run, runPolicyGroupMapSHA256Index, string(digest))
	recordAuthoritySet(f.run, runPolicyGroupCountIndex, strconv.Itoa(len(f.groups)))
	f.policy, err = (transportAuthority{seal: &redisTransportAuthoritySeal}).parseRunPolicyAuthority(f.intent.Lease.RunID, f.run, f.groups)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := reservationIntentFields(f.policy, f.intent)
	if err != nil {
		t.Fatal(err)
	}
	lease, _ := operationWireLeaseFields(f.intent.Lease)
	values := operationWireSemanticValues(append(lease, fields...))
	values["lease_fence"] = values["fence"]
	id, err := DeriveReservationID(f.policy, f.intent)
	if err != nil {
		t.Fatal(err)
	}
	values["reservation_id"] = string(id)
	for i, field := range f.reservation {
		if value, ok := values[field.Name]; ok {
			recordAuthoritySet(f.reservation, i, value)
		}
	}
	if string(f.job[jobActiveReservationIDIndex].Value) != "" {
		recordAuthoritySet(f.job, jobActiveReservationIDIndex, string(id))
	}
}

func TestRequestLuaChargedGroupIsNotSourceOpenGroup(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	f := requestLuaFixtureNew(t, "started")
	// The source group retains its open job. The subsequent redirect is charged
	// to a different, run-pinned group which has ZERO source open jobs.
	target := RequestTarget{CanonicalURL: "https://elsewhere.example/redirect", URLID: JobID(utils.URLIDV1("https://elsewhere.example/redirect"))}
	decision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestRedirect, Target: target, Depth: f.source.Depth,
		GroupID: "redirect", RateScopeID: RateScopeID(strings.Repeat("6", 32)), GroupConcurrency: 3, OriginConcurrency: 3,
		GroupIntervalMS: 100, OriginIntervalMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	f.groups = append(f.groups, PolicyGroup{GroupID: decision.GroupID, RateScopeID: decision.RateScopeID,
		GroupScopeID: decision.GroupScopeID, RequestStartLimit: 10, Concurrency: 3, IntervalMS: 100})
	f.intent.Target, f.intent.Decision, f.intent.RequestOrdinal = target, decision, 2
	recordAuthoritySet(f.run, runReservationCreationsTotalIndex, "2")
	recordAuthoritySet(f.run, runPendingRequestReservationsIndex, "1")
	recordAuthoritySet(f.run, runStartedRequestReservationsIndex, "0")
	recordAuthoritySet(f.run, runLastActivityAtMSIndex, "560")
	recordAuthoritySet(f.job, jobNextRequestOrdinalIndex, "3")
	recordAuthoritySet(f.job, jobUpdatedAtMSIndex, "560")
	recordAuthoritySet(f.reservation, reservationStateIndex, "pending")
	recordAuthoritySet(f.reservation, reservationCreatedAtMSIndex, "560")
	for _, field := range []int{reservationStartedAtMSIndex, reservationDeliveryAttemptsAfterStartIndex,
		reservationJobStartsAfterStartIndex, reservationRunStartsAfterStartIndex, reservationGroupStartsAfterStartIndex} {
		recordAuthoritySet(f.reservation, field, "0")
	}
	requestLuaRebind(t, f)
	base := requestLuaPrefix + "run:" + string(f.intent.Lease.RunID)
	edit := func(r *sharedLuaRedis) {
		r.data[base+":group_started"].hash["default"] = "1"
		r.data[base+":group_started"].hash["redirect"] = "0"
		global := requestLuaPrefix + "rate:" + string(DeriveGlobalScopeID())
		r.data[global].hash["last_started_at_ms"] = "550"
	}
	got, code := requestLuaCheck(t, vm, f, "check_live", "", edit, nil)
	if got == lua.LNil || code != lua.LNil {
		t.Fatalf("legitimate request-group/source-group split rejected: %v", code)
	}
	// A first pending redirect, or a replacement initial robots intent charged
	// to this other group, is NOT allowed. Bindings remain cryptographically valid.
	for _, kind := range []RequestKind{RequestRedirect, RequestRobots} {
		f.intent.Decision.RequestKind = kind
		for _, field := range []int{jobRequestStartsIndex, jobDeliveryAttemptsIndex, jobLeaseDeliveryStartedIndex, jobLastRequestStartedAtMSIndex,
			jobLastDocumentRequestStartedAtMSIndex, jobLastDocumentRequestFenceIndex} {
			recordAuthoritySet(f.job, field, "0")
		}
		for _, field := range []int{jobLastDocumentTargetURLIDIndex, jobLastDocumentTargetURLIndex, jobLastDocumentTargetDigestIndex} {
			recordAuthoritySet(f.job, field, "")
		}
		recordAuthoritySet(f.run, runRequestStartsIndex, "0")
		recordAuthoritySet(f.run, runLastRequestStartedAtMSIndex, "0")
		requestLuaRebind(t, f)
		if got, _ := requestLuaCheck(t, vm, f, "check_live", "", nil, nil); got != lua.LNil {
			t.Fatalf("first %s switched immutable source group", kind)
		}
	}
}

func TestRequestLuaLiveCounterOwnerAndClockEdges(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	f := requestLuaFixtureNew(t, "started")
	base := requestLuaPrefix + "run:" + string(f.intent.Lease.RunID)
	jobKey := base + ":job:" + string(f.intent.Lease.JobID)
	reservationKey := requestLuaPrefix + "reservation:" + string(f.reservation[reservationIDIndex].Value)
	for _, test := range []struct {
		name string
		edit func(*sharedLuaRedis)
	}{
		{"record owner", func(r *sharedLuaRedis) { r.data[reservationKey].hash["owner_id"] = strings.Repeat("7", 32) }},
		{"job token", func(r *sharedLuaRedis) { r.data[jobKey].hash["lease_token"] = strings.Repeat("7", 64) }},
		{"active pointer", func(r *sharedLuaRedis) { r.data[jobKey].hash["active_reservation_id"] = strings.Repeat("e", 64) }},
		{"next ordinal", func(r *sharedLuaRedis) { r.data[jobKey].hash["next_request_ordinal"] = "3" }},
		{"group budget", func(r *sharedLuaRedis) { r.data[base+":group_started"].hash["default"] = "11" }},
		{"group snapshot", func(r *sharedLuaRedis) {
			r.data[reservationKey].hash["group_starts_after_start"] = "2"
			r.data[reservationKey].hash["run_starts_after_start"] = "2"
		}},
		{"missing started", func(r *sharedLuaRedis) { r.data[base+":group_active_started"].hash["default"] = "0" }},
		{"source open count", func(r *sharedLuaRedis) { r.data[base+":group_open_jobs"].hash["default"] = "0" }},
		{"run latest start", func(r *sharedLuaRedis) { r.data[base].hash["last_request_started_at_ms"] = "549" }},
		{"job latest start", func(r *sharedLuaRedis) {
			r.data[jobKey].hash["last_request_started_at_ms"] = "551"
			r.data[jobKey].hash["updated_at_ms"] = "551"
		}},
		{"future run", func(r *sharedLuaRedis) { r.data[base].hash["last_activity_at_ms"] = "601" }},
		{"future reservation", func(r *sharedLuaRedis) { r.data[reservationKey].hash["started_at_ms"] = "601" }},
		{"lease alignment", func(r *sharedLuaRedis) { r.data[reservationKey].hash["expires_at_ms"] = "60501" }},
		{"lease before activation", func(r *sharedLuaRedis) {
			r.data[jobKey].hash["lease_started_at_ms"] = "399"
			r.zsets[base+":leased_at"][string(f.intent.Lease.JobID)] = 399
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got, _ := requestLuaCheck(t, vm, f, "check_live", "", test.edit, nil); got != lua.LNil {
				t.Fatal("contradictory live relation accepted")
			}
		})
	}
	// Already-held capacity survives the cumulative creation limit, run budget
	// exhaustion, cancellation and authorization expiry. The future operation
	// chooses which transitions remain allowed; this foundation checks consistency.
	for _, state := range []string{"creation_limit", "run_budget", "cancelled", "authorization_expired"} {
		f := requestLuaFixtureNew(t, "started")
		wantEligible := true
		switch state {
		case "creation_limit":
			recordAuthoritySet(f.run, runReservationCreationsTotalIndex, "100")
		case "run_budget":
			recordAuthoritySet(f.run, runMaxRequestStartsIndex, "1")
		case "cancelled":
			recordAuthoritySet(f.run, runStateIndex, "cancelled")
			recordAuthoritySet(f.run, runCancelledAtMSIndex, "560")
			recordAuthoritySet(f.run, runLastActivityAtMSIndex, "560")
			recordAuthoritySet(f.run, runTerminalReasonIndex, "operator_cancelled")
			wantEligible = false
		case "authorization_expired":
			recordAuthoritySet(f.run, runAuthorizationExpiresAtMSIndex, "599")
			wantEligible = false
		}
		got, code := requestLuaCheck(t, vm, f, "check_live", "", nil, nil)
		if code != lua.LNil || got.(*lua.LTable).RawGetString("eligible_now") != lua.LBool(wantEligible) {
			t.Fatalf("%s stranded held capacity: %v", state, code)
		}
	}
}

func TestRequestLuaBuildersTighteningAndClockMath(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	f := requestLuaFixtureNew(t, "pending")
	r, keys := requestLuaStore(t, f)
	ctx := requestLuaOpen(t, vm, r, keys, string(f.intent.Lease.RunID))
	run, job := requestLuaBinding(t, vm, f)
	lease, _ := operationWireLeaseFields(f.intent.Lease)
	fields, _ := reservationIntentFields(f.policy, f.intent)
	values := jobLuaValues(vm.jobLuaVM, append(lease, fields...))
	before := len(r.trace)
	got, code := vm.invoke(t, "Request", "pending_record", ctx, run, job, values, lua.LTrue, lua.LString("60600"))
	if code != lua.LNil {
		t.Fatal(code)
	}
	want := cloneRecord(f.reservation)
	recordAuthoritySet(want, reservationCreatedAtMSIndex, "600")
	recordAuthoritySet(want, reservationExpiresAtMSIndex, "60600")
	jobLuaAssertRecord(t, got, want)
	if err := ValidateRecord(SchemaReservation, want); err != nil {
		t.Fatal(err)
	}
	for _, expiry := range []string{"0", "599", "600", "0601", "9007199254740992"} {
		if got, _ := vm.invoke(t, "Request", "pending_record", ctx, run, job, values, lua.LTrue, lua.LString(expiry)); got != lua.LNil {
			t.Fatalf("bad deadline %s", expiry)
		}
	}
	if len(r.trace) != before {
		t.Fatal("pure builder touched Redis")
	}
	// No caller-supplied depth or pre-parsed numeric map may change the decision.
	values.RawSetString("depth", lua.LString("99"))
	if got, code := vm.invoke(t, "Request", "intent", values, run, job, lua.LTrue); got == lua.LNil || code != lua.LNil {
		t.Fatal("non-protocol caller depth became authority")
	}
	for _, kind := range []string{"global", "group", "origin"} {
		witness := "global"
		if kind == "group" {
			witness = strings.Repeat("2", 32)
		} else if kind == "origin" {
			witness = "https://example.com:443"
		}
		record := requestLuaRateRecord(t, kind, witness)
		for _, tuple := range [][2]string{{"1", "200"}, {"32", "0"}, {"3", "100"}, {"2", "0"}, {"0", "0"}, {"33", "0"}, {"1", "3600001"}} {
			p := vm.state.NewTable()
			p.RawSetString("v", jobLuaValues(vm.jobLuaVM, record))
			got, code := vm.invoke(t, "Rate", "tighten", p, lua.LString(tuple[0]), lua.LString(tuple[1]), lua.LString(strings.Repeat("c", 64)), lua.LString("600"))
			valid := tuple[0] != "0" && tuple[0] != "33" && tuple[1] != "3600001" && (kind != "global" || tuple == [2]string{"2", "0"})
			if (got != lua.LNil) != valid {
				t.Fatalf("tighten %s/%v: %v", kind, tuple, code)
			}
			if !valid {
				continue
			}
			out := got.(*lua.LTable).RawGetString("v").(*lua.LTable)
			expected := cloneRecord(record)
			cc, _ := strconv.Atoi(tuple[0])
			iv, _ := strconv.Atoi(tuple[1])
			changed := false
			if kind != "global" && cc < 3 {
				recordAuthoritySet(expected, rateScopeEffectiveConcurrencyIndex, tuple[0])
				recordAuthoritySet(expected, rateScopeConcurrencySourceSHA256Index, strings.Repeat("c", 64))
				changed = true
			}
			if kind != "global" && iv > 100 {
				recordAuthoritySet(expected, rateScopeEffectiveIntervalMSIndex, tuple[1])
				recordAuthoritySet(expected, rateScopeIntervalSourceSHA256Index, strings.Repeat("c", 64))
				recordAuthoritySet(expected, rateScopeNextAllowedMSIndex, strconv.Itoa(300+iv))
				changed = true
			}
			if changed {
				recordAuthoritySet(expected, rateScopeUpdatedAtMSIndex, "600")
			}
			jobLuaAssertRecord(t, got, expected)
			if err := ValidateRecord(SchemaRateScope, expected); err != nil {
				t.Fatalf("Go rejects projected record: %v (%v)", err, out)
			}
		}
	}
	for _, test := range []struct{ last, next, updated, now string }{
		{"300", "400", "300", "299"}, // backwards clock
		{"9007199254740990", "9007199254740991", "9007199254740990", "9007199254740991"}, // checked addition
	} {
		record := requestLuaRateRecord(t, "group", strings.Repeat("2", 32))
		recordAuthoritySet(record, rateScopeLastStartedAtMSIndex, test.last)
		recordAuthoritySet(record, rateScopeNextAllowedMSIndex, test.next)
		recordAuthoritySet(record, rateScopeUpdatedAtMSIndex, test.updated)
		recordAuthoritySet(record, rateScopeEffectiveIntervalMSIndex, "1")
		p := vm.state.NewTable()
		p.RawSetString("v", jobLuaValues(vm.jobLuaVM, record))
		if err := ValidateRecord(SchemaRateScope, record); err != nil {
			t.Fatal(err)
		}
		if got, _ := vm.invoke(t, "Rate", "tighten", p, lua.LString("1"), lua.LString("100"), lua.LString(strings.Repeat("c", 64)), lua.LString(test.now)); got != lua.LNil {
			t.Fatal("skew/overflow accepted")
		}
	}
}

func TestRequestLuaSelectedMemberProofAndUnmaterializedScope(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	f := requestLuaFixtureNew(t, "pending")
	id := string(f.reservation[reservationIDIndex].Value)
	scopeID := string(f.reservation[reservationGroupScopeIDIndex].Value)
	key := requestLuaPrefix + "rate:" + scopeID
	for _, test := range []string{"partial", "private_missing", "absent", "orphan", "max32", "too_many"} {
		r, keys := requestLuaStore(t, f)
		omit := key + ":pending"
		if test == "absent" || test == "orphan" {
			for _, k := range []string{key, key + ":active", key + ":pending", key + ":started"} {
				r.removeKey(k)
			}
			r.removeKey(requestLuaPrefix + "reservation:" + id)
			if test == "absent" {
				delete(r.zsets[RateScopesKey], scopeID)
			}
			omit = ""
		} else if test == "private_missing" {
			omit = requestLuaPrefix + "reservation:" + id
		} else if test == "max32" || test == "too_many" {
			count := 32
			if test == "too_many" {
				count++
			}
			for i := 1; i < count; i++ {
				other := fmt.Sprintf("%064x", i)
				r.zsets[key+":active"][other], r.zsets[key+":pending"][other] = 60500, 60500
			}
			r.data[key].hash["active_count"], r.data[key].hash["pending_count"] = strconv.Itoa(count), strconv.Itoa(count)
			omit = ""
		}
		ctx := requestLuaOpen(t, vm, r, keys, string(f.intent.Lease.RunID))
		selectCode := requestLuaSelect(t, vm, ctx, f, keys, omit)
		if selectCode != lua.LNil {
			if test != "too_many" {
				t.Fatalf("selection %s: %v", test, selectCode)
			}
			continue
		}
		view := vm.state.NewTable()
		view.RawSetString("ctx", ctx)
		view.RawSetString("reservation_ids", bootLuaStrings(vm.state, []string{id}))
		if test == "partial" {
			// Cardinality+TTL alone is not a receipt for the selected pending
			// member. Forging the public table cannot complete the private proof.
			fact, code := vm.invoke(t, "Read", "cardinality", ctx, lua.LString(omit), lua.LString("zset"), lua.LNumber(32))
			if code != lua.LNil {
				t.Fatal(code)
			}
			vm.invoke(t, "Read", "ttl", ctx, lua.LString(omit))
			fact.(*lua.LTable).RawGetString("members").(*lua.LTable).RawSetString(id, lua.LTrue)
			fact.(*lua.LTable).RawGetString("scores").(*lua.LTable).RawSetString(id, lua.LNumber(60500))
			view.RawSetString("pending", fact)
		}
		got, code := vm.invoke(t, "Rate", "check_scope", view, lua.LString(scopeID))
		want := test == "absent" || test == "max32"
		if (got != lua.LNil) != want {
			t.Fatalf("selected proof %s: %v/%v", test, got, code)
		}
		if test == "absent" && got.(*lua.LTable).RawGetString("exists") != lua.LFalse {
			t.Fatal("unmaterialized scope invented a record")
		}
		if test == "max32" && got.(*lua.LTable).RawGetString("complete_membership") != lua.LFalse {
			t.Fatal("selected proof claimed complete coverage")
		}
	}
}

func TestRequestLuaCurrentFenceBGAndSnapshots(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	for _, tuple := range [][4]uint64{
		// B, G, delivery attempts, fence; all cumulative counters are retained.
		{0, 1, 1, 2}, {1, 2, 2, 2}, {2, 3, 3, 3}, {9, 10, 2, 2},
	} {
		f := requestLuaFixtureNew(t, "started")
		b, g, deliveries, fence := tuple[0], tuple[1], tuple[2], tuple[3]
		f.intent.Lease.Fence, f.intent.RequestOrdinal = Fence(fence), 100
		for field, value := range map[int]uint64{jobLeaseFenceIndex: fence, jobClaimCountIndex: fence, jobDeliveryAttemptsIndex: deliveries,
			jobLeaseRequestStartsBaselineIndex: b, jobRequestStartsIndex: g, jobLastDocumentRequestFenceIndex: fence, jobNextRequestOrdinalIndex: 101} {
			recordAuthoritySet(f.job, field, strconv.FormatUint(value, 10))
		}
		for field, value := range map[int]uint64{runClaimsTotalIndex: fence, runRequestStartsIndex: g, runReservationCreationsTotalIndex: 100} {
			recordAuthoritySet(f.run, field, strconv.FormatUint(value, 10))
		}
		for field, value := range map[int]uint64{reservationDeliveryAttemptsAfterStartIndex: deliveries, reservationJobStartsAfterStartIndex: g,
			reservationRunStartsAfterStartIndex: g, reservationGroupStartsAfterStartIndex: g} {
			recordAuthoritySet(f.reservation, field, strconv.FormatUint(value, 10))
		}
		requestLuaRebind(t, f)
		for schema, record := range map[RecordSchema]Record{SchemaRun: f.run, SchemaJob: f.job, SchemaReservation: f.reservation} {
			if err := ValidateRecord(schema, record); err != nil {
				t.Fatalf("oracle B/G %v/%s: %v", tuple, schema, err)
			}
		}
		if got, code := requestLuaCheck(t, vm, f, "check_live", "", nil, nil); got == lua.LNil || code != lua.LNil {
			t.Fatalf("valid current B/G %v: %v", tuple, code)
		}
		if b > 0 {
			// Valid as a standalone receipt, but not this fence's active/latest
			// request. No first_count-1 inference or counter substitution allowed.
			recordAuthoritySet(f.reservation, reservationJobStartsAfterStartIndex, strconv.FormatUint(b, 10))
			recordAuthoritySet(f.reservation, reservationDeliveryAttemptsAfterStartIndex, "1")
			if !requestLuaCompare(t, vm, SchemaReservation, f.reservation) {
				t.Fatal("bad test snapshot")
			}
			if got, _ := requestLuaCheck(t, vm, f, "check_live", "", nil, nil); got != lua.LNil {
				t.Fatal("older fence snapshot passed current B/G")
			}
		}
	}
	f := requestLuaFixtureNew(t, "started")
	recordAuthoritySet(f.reservation, reservationStateIndex, "expired")
	recordAuthoritySet(f.reservation, reservationTerminalAtMSIndex, "60500")
	recordAuthoritySet(f.job, jobActiveReservationIDIndex, "")
	recordAuthoritySet(f.job, jobUpdatedAtMSIndex, "60500")
	recordAuthoritySet(f.run, runStartedRequestReservationsIndex, "0")
	recordAuthoritySet(f.run, runLastActivityAtMSIndex, "60500")
	f.now = 60600
	if got, code := requestLuaCheck(t, vm, f, "check_receipt", "", nil, nil); got == lua.LNil || code != lua.LNil {
		t.Fatalf("expired START snapshot lost: %v", code)
	} else {
		jobLuaAssertRecord(t, got, f.reservation)
	}
}

func TestRequestLuaMaximumPolicyAndRecomputedTupleMutations(t *testing.T) {
	t.Parallel()
	vm := requestLuaNew(t)
	f := requestLuaFixtureNew(t, "pending")
	f.source.Depth, f.source.Decision.Depth, f.intent.Decision.Depth = MaxExactInteger, MaxExactInteger, MaxExactInteger
	f.source.Decision.GroupConcurrency, f.source.Decision.OriginConcurrency = 32, 32
	f.source.Decision.GroupIntervalMS, f.source.Decision.OriginIntervalMS = 3600000, 3600000
	f.intent.Decision = f.source.Decision
	f.groups[0] = jobLuaGroup(f.source)
	for i := 1; i < 64; i++ {
		lineage := RateScopeID(fmt.Sprintf("%032x", i+16))
		scope, _ := DeriveGroupScopeID(lineage)
		f.groups = append(f.groups, PolicyGroup{GroupID: GroupID(fmt.Sprintf("group-%02d", i)), RateScopeID: lineage,
			GroupScopeID: scope, RequestStartLimit: 10, Concurrency: 32, IntervalMS: 3600000})
	}
	source, err := completeSourceJobRecord(f.source)
	if err != nil {
		t.Fatal(err)
	}
	jobLuaApplySource(f.job, source)
	requestLuaRebind(t, f)
	run, job := requestLuaBinding(t, vm, f)
	lease, _ := operationWireLeaseFields(f.intent.Lease)
	fields, _ := reservationIntentFields(f.policy, f.intent)
	semantic := append(lease, fields...)
	got, code := vm.invoke(t, "Request", "intent", jobLuaValues(vm.jobLuaVM, semantic), run, job, lua.LTrue)
	if code != lua.LNil || got == lua.LNil {
		t.Fatalf("maximum complete group map/depth/tuple: %v", code)
	}
	want, _ := DeriveReservationID(f.policy, f.intent)
	if got.(*lua.LTable).RawGetString("reservation_id") != lua.LString(want) {
		t.Fatal("maximum identity differs")
	}
	// Keep the reservation ID internally consistent while breaking tuple or
	// lexical relations. Rejecting only a stale ID would not test these rules.
	for _, test := range []struct {
		field int
		value string
	}{
		{reservationRequestOrdinalIndex, "0"}, {reservationRequestOrdinalIndex, "101"}, {reservationLeaseFenceIndex, "0"},
		{reservationGlobalConcurrencyIndex, "1"}, {reservationGlobalIntervalMSIndex, "1"},
		{reservationGroupConcurrencyIndex, "0"}, {reservationGroupConcurrencyIndex, "33"},
		{reservationOriginConcurrencyIndex, "1"}, {reservationGroupIntervalMSIndex, "3600001"},
		{reservationOriginIntervalMSIndex, "99"}, {reservationRequestKindIndex, "Document"},
		{reservationGlobalScopeIDIndex, string(f.intent.Decision.GroupScopeID)}, {reservationGroupIDIndex, "\x01"},
	} {
		r := cloneRecord(f.reservation)
		recordAuthoritySet(r, test.field, test.value)
		values := make([]string, len(r))
		for i, f := range r {
			values[i] = string(f.Value)
		}
		recordAuthoritySet(r, reservationIDIndex, string(deriveReservationRecordID(values)))
		if requestLuaCompare(t, vm, SchemaReservation, r) {
			t.Fatalf("internally rehashed bad tuple %s", r[test.field].Name)
		}
	}
}
