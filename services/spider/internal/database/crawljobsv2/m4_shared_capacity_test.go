package crawljobsv2

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

type m4SharedActor struct {
	m4ClaimFixture
	URL      string `json:"url"`
	Robots   string `json:"robots_url"`
	Origin   string `json:"origin"`
	Identity struct {
		Owner       string `json:"owner_id"`
		Token       string `json:"lease_token"`
		Fence       string `json:"fence"`
		Reservation string `json:"reservation_id"`
		Claim       string `json:"claim_transition_id"`
	} `json:"identity"`
}

type m4SharedFixture struct {
	m4ClaimFixture
	Actors map[string]m4SharedActor `json:"actors"`
}

func TestM4SharedGroupCapacityOffline(t *testing.T) {
	for _, profile := range []string{"spaced", "same-ms", "reversed"} {
		t.Run(profile, func(t *testing.T) { m4SharedProfile(t, profile) })
	}
}

func TestM4SharedGroupCancellationOffline(t *testing.T) {
	for _, profile := range []string{"cancel-spaced", "cancel-same-ms"} {
		t.Run(profile, func(t *testing.T) { m4SharedProfile(t, profile) })
	}
}

// Independent identities, literal layouts and numeric invariants supplement the
// complete Python state oracle. Only canonical Lua executes transitions; this
// command facade is not target Redis, runtime ACL or simultaneous-worker proof.
func m4SharedProfile(t *testing.T, profile string) {
	t.Helper()
	const caseID = "ledger-shared-group-capacity-v1"
	ops := []OperationName{OperationTryClaim, OperationTryClaim, OperationTryClaim, OperationTryClaim,
		OperationCancelReservation, OperationStartRequest, OperationStartRequest, OperationTryClaim,
		OperationCancelReservation, OperationFinishRequest, OperationFinishRequest, OperationTryClaim,
		OperationTryClaim, OperationFinishRequest, OperationStartRequest, OperationStartRequest,
		OperationStartRequest, OperationFinishRequest, OperationFinishRequest, OperationStartRequest, OperationMaintainRateScopes}
	actors := strings.Fields("a a b b a a a b a a a b b a a b b b b b a")
	statuses := strings.Fields(`CLAIMED ALREADY_CLAIMED CAPACITY_BLOCKED CAPACITY_BLOCKED CRAWL_V2_IMMUTABLE_MISMATCH
		STARTED ALREADY_STARTED CAPACITY_BLOCKED CRAWL_V2_INVALID_STATE CRAWL_V2_IMMUTABLE_MISMATCH FINISHED CLAIMED
		ALREADY_CLAIMED ALREADY_FINISHED ALREADY_STARTED STARTED ALREADY_STARTED FINISHED ALREADY_FINISHED ALREADY_STARTED BATCH_DONE`)
	mutations := map[int]bool{0: true, 5: true, 10: true, 11: true, 15: true, 17: true}
	faults := map[int]string{4: "owner", 9: "token"}
	trace := "finish"
	if strings.HasPrefix(profile, "cancel-") {
		trace = "cancel"
		ops = []OperationName{OperationTryClaim, OperationTryClaim, OperationCancelReservation, OperationCancelReservation,
			OperationTryClaim, OperationCancelReservation, OperationStartRequest, OperationFinishRequest, OperationMaintainRateScopes}
		actors = strings.Fields("a b a a b a b b a")
		statuses = strings.Fields("CLAIMED CAPACITY_BLOCKED RESERVATION_CANCELLED RESERVATION_CANCELLED CLAIMED RESERVATION_CANCELLED STARTED FINISHED BATCH_DONE")
		mutations, faults = map[int]bool{0: true, 2: true, 4: true, 6: true, 7: true}, map[int]string{}
	} else if profile == "reversed" {
		trace = "finish-reversed"
		for i, actor := range actors {
			actors[i] = "a"
			if actor == "a" {
				actors[i] = "b"
			}
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	path := filepath.Join(fixtureRepositoryRoot(t), "tests/crawl-jobs-v2-redis/test_shared_capacity.py")
	raw, err := exec.CommandContext(ctx, "python3", "-B", path, "--go-vectors", profile).Output()
	if err != nil || len(raw) > 2*1024*1024 {
		t.Fatal("shared vectors failed or exceeded the closed bound", err)
	}
	var vector struct {
		Purpose    string              `json:"purpose"`
		Profile    string              `json:"profile"`
		Trace      string              `json:"trace"`
		Authorized bool                `json:"execution_authorized"`
		Fixture    m4SharedFixture     `json:"fixture"`
		ACL        map[string][]string `json:"acl_rules"`
		Wires      []m4NegativeWire    `json:"wires"`
		Times      []struct {
			Before uint64  `json:"started_at_ms"`
			Now    *uint64 `json:"now_ms"`
			After  uint64  `json:"finished_at_ms"`
		} `json:"observations"`
		Expected []struct {
			ID        string                   `json:"assertion_id"`
			Operation OperationName            `json:"operation"`
			Reply     json.RawMessage          `json:"reply"`
			State     map[string]*m4ClaimEntry `json:"state"`
		} `json:"expected"`
	}
	if json.Unmarshal(raw, &vector) != nil || vector.Purpose != "offline_public_shared_capacity_vectors" || vector.Authorized ||
		vector.Profile != profile || vector.Trace != trace || len(vector.Expected) != len(ops) || len(vector.Wires) != len(ops) || len(vector.Times) != len(ops) {
		t.Fatal("invalid shared vector envelope")
	}
	f := vector.Fixture
	if f.Case != caseID || f.Authorized || f.Measured != "not_measured" || len(f.Actors) != 2 || len(f.Initial) != 89 ||
		len(f.Inventory) != 90 || !reflect.DeepEqual(f.Bootstrap, []string{DurabilityKey}) || f.Inputs.Time != 1000000 || f.Inputs.FixtureID != strings.Repeat("1", 32) {
		t.Fatal("shared fixture scope")
	}
	if f.Inputs.OwnerA != strings.Repeat("2", 32) || f.Inputs.OwnerB != strings.Repeat("3", 32) ||
		f.Inputs.TokenA != strings.Repeat("4", 64) || f.Inputs.TokenB != strings.Repeat("5", 64) || f.Inputs.WrongToken != strings.Repeat("6", 64) {
		t.Fatal("independent public synthetic identity controls")
	}
	for _, key := range wireOracleAuthorityKeys() {
		for _, command := range []string{"SET", "HSET", "DEL", "EXPIRE", "RENAME"} {
			if m4ClaimACLAllows(vector.ACL["ledger"], command, []string{key}) {
				t.Fatal("proposed ledger ACL permits authority mutation")
			}
		}
	}
	for _, key := range []string{ContractsCandidateKey, CrawlContractCandidateKey, AdminFreezeKey} {
		for _, command := range []string{"GET", "HGET"} {
			if m4ClaimACLAllows(vector.ACL["ledger"], command, []string{key}) {
				t.Fatal("proposed ledger ACL permits candidate/freeze data reads")
			}
		}
	}
	bundle, err := AuthoritativeScriptBindingSet()
	if err != nil {
		t.Fatal(err)
	}
	contract, _ := ContractSHA256()
	lineage := RateScopeID(digestFramed("mifolyo:m4:shared-group-capacity:lineage:v1", []byte(f.Inputs.FixtureID))[:32])
	groupScope, _ := DeriveGroupScopeID(lineage)
	group := PolicyGroup{GroupID: "fixture", RateScopeID: lineage, GroupScopeID: groupScope, RequestStartLimit: 10, Concurrency: 1, IntervalMS: 0}
	groupRecord, _ := policyGroupRecord(group)
	if !reflect.DeepEqual(groupRecord, m4ClaimRecord(t, f.Group)) {
		t.Fatal("independent shared group mismatch")
	}
	policies := map[string]RunPolicyAuthority{}
	intents := map[string]ReservationIntent{}
	claims := map[string]TryClaimTransitionInput{}
	suffixes := map[string][]string{}
	claimSemantics := map[string][]string{}
	allKeys := map[string]bool{}
	for _, label := range []string{"a", "b"} {
		a := f.Actors[label]
		rid := RunID(digestFramed("mifolyo:m4:shared-group-capacity:run:v1", []byte(f.Inputs.FixtureID), []byte(label))[:32])
		document, robots := "https://m4-capacity-"+label+".invalid/document", "https://m4-capacity-"+label+".invalid/robots.txt"
		jid := JobID(utils.URLIDV1(document))
		if a.RunID != string(rid) || a.JobID != string(jid) || a.URL != document || a.Robots != robots || a.Origin != "https://m4-capacity-"+label+".invalid:443" ||
			a.BaseKey != "mifolyo:crawl:v2:run:"+string(rid) || a.JobKey != a.BaseKey+":job:"+string(jid) {
			t.Fatal("independent actor identity")
		}
		targets := []RequestTarget{{URLID: jid, CanonicalURL: document}, {URLID: JobID(utils.URLIDV1(robots)), CanonicalURL: robots}}
		decisions := make([]PolicyDecision, 2)
		for i, kind := range []RequestKind{RequestDocument, RequestRobots} {
			decisions[i], err = NewPolicyDecision(PolicyDecisionInput{RequestKind: kind, Target: targets[i], Depth: 1,
				GroupID: "fixture", RateScopeID: lineage, GroupConcurrency: 1, OriginConcurrency: 1})
			if err != nil {
				t.Fatal(err)
			}
		}
		source := SourceJob{JobID: jid, CanonicalURL: document, Depth: 1, ScoreText: "0", GroupID: "fixture", RateScopeID: lineage, Decision: decisions[0]}
		sourceRecord, _ := sourceJobRecord(source)
		if !reflect.DeepEqual(sourceRecord, m4ClaimRecord(t, a.Source)) {
			t.Fatal("independent per-run source")
		}
		runRecord := m4ClaimRecord(t, f.Initial[a.BaseKey].Fields)
		runFields := m4SharedFields(runRecord)
		if ValidateRecord(SchemaRun, runRecord) != nil || ValidateRecord(SchemaJob, m4ClaimRecord(t, f.Initial[a.JobKey].Fields)) != nil {
			t.Fatal("initial run/job schema")
		}
		for field, value := range map[string]string{"state": "active", "job_count": "1", "open_job_count": "1", "max_jobs": "10000",
			"max_request_starts": "10", "global_concurrency_limit": "2", "authorization_expires_at_ms": "1600000",
			"claims_total": "0", "request_starts": "0", "reservation_creations_total": "0", "pending_request_reservations": "0", "started_request_reservations": "0"} {
			if runFields[field] != value {
				t.Fatal("initial eligibility/accounting", label, field)
			}
		}
		descriptor, _ := json.Marshal(map[string]any{"purpose": "conformance_only", "case": caseID, "kind": "crawl_policy", "release_eligible": false,
			"global_concurrency": 2, "global_interval_ms": 0, "group_concurrency": 1, "group_interval_ms": 0, "origin_concurrency": 1, "origin_interval_ms": 0})
		policySum := sha256.Sum256(append(descriptor, '\n'))
		sourceDigest, _ := DeriveSourceDigest([]SourceJob{source})
		groupDigest, _ := DerivePolicyGroupMapDigest([]PolicyGroup{group})
		if runFields["crawl_policy_sha256"] != hex.EncodeToString(policySum[:]) || runFields["source_sha256"] != string(sourceDigest) ||
			runFields["policy_group_map_sha256"] != string(groupDigest) || runFields["contract_sha256"] != string(contract) {
			t.Fatal("independent run policy/source digest")
		}
		policy, err := (transportAuthority{seal: &redisTransportAuthoritySeal}).parseRunPolicyAuthority(rid, runRecord, []PolicyGroup{group})
		if err != nil {
			t.Fatal(err)
		}
		owner, token := f.Inputs.OwnerA, f.Inputs.TokenA
		if label == "b" {
			owner, token = f.Inputs.OwnerB, f.Inputs.TokenB
		}
		if a.Identity.Owner != owner || a.Identity.Token != token || a.Identity.Fence != "1" {
			t.Fatal("owner/token binding")
		}
		lease := LeaseIdentity{RunID: rid, JobID: jid, OwnerID: OwnerID(owner), Token: LeaseToken(token), Fence: 1}
		intent := ReservationIntent{Lease: lease, RequestOrdinal: 1, Target: targets[1], CrawlPolicyDigest: Digest(runFields["crawl_policy_sha256"]), Decision: decisions[1]}
		qid, e := DeriveReservationID(policy, intent)
		claimInput := TryClaimTransitionInput{Job: source, Lease: lease, ExpectedPriorFence: 0, InitialIntent: intent}
		claimID, e2 := DeriveTryClaimTransitionID(policy, claimInput)
		if e != nil || e2 != nil || a.Identity.Reservation != string(qid) || a.Identity.Claim != string(claimID) {
			t.Fatal("independent reservation/claim derivation")
		}
		policies[label], intents[label], claims[label] = policy, intent, claimInput
		d := decisions[1]
		decisionDigest, _ := DerivePolicyDecisionDigest(d)
		suffixes[label] = []string{"1", "robots", string(targets[1].URLID), robots, string(d.TargetDigest), runFields["crawl_policy_sha256"],
			string(decisionDigest), "fixture", string(lineage), string(d.GlobalScopeID), string(groupScope), string(d.OriginScopeID), "2", "0", "1", "0", "1", "0"}
		documentDigest, _ := DerivePolicyDecisionDigest(decisions[0])
		claimSemantics[label] = []string{string(rid), string(jid), document, "0", "1", "fixture", string(lineage), string(groupScope), string(d.OriginScopeID), string(documentDigest), "0", "1", owner, token}
		work := wireOracleAuthorityKeys()
		for _, name := range strings.Fields("runs active_runs unarchived_runs first_request_start active_leases stage_expiry stage_slots rate_scopes") {
			work = append(work, "mifolyo:crawl:v2:"+name)
		}
		work = append(work, a.BaseKey)
		for _, name := range strings.Fields("jobs job_order ready ready_at leased leased_at delayed commit_backpressure completed dead cancelled group_limits group_rate_scope_ids group_scope_ids group_concurrency group_interval_ms group_started group_pending group_active_started group_open_jobs audit_group_counts retry_reason_counts recovery_outcome_counts disposition_reason_counts visited_depth visited_urls") {
			work = append(work, a.BaseKey+":"+name)
		}
		work = append(work, a.JobKey)
		if len(work) != 44 || !reflect.DeepEqual(work, a.WorkKeys) || !reflect.DeepEqual(a.Scopes, []string{string(d.GlobalScopeID), string(groupScope), string(d.OriginScopeID)}) {
			t.Fatal("literal WORK/scope order")
		}
		for _, key := range work {
			allKeys[key] = true
		}
		allKeys["mifolyo:crawl:v2:reservation:"+string(qid)] = true
		for _, scope := range a.Scopes {
			for _, ending := range []string{"", ":active", ":pending", ":started"} {
				allKeys["mifolyo:crawl:v2:rate:"+scope+ending] = true
			}
		}
	}
	inventory := make([]string, 0, len(allKeys))
	for key := range allKeys {
		inventory = append(inventory, key)
		_, present := f.Initial[key]
		if present != (key != DurabilityKey) {
			t.Fatal("combined ownership")
		}
	}
	sort.Strings(inventory)
	if len(inventory) != 90 || !reflect.DeepEqual(inventory, f.Inventory) || f.Actors["a"].RunID == f.Actors["b"].RunID || f.Actors["a"].Scopes[2] == f.Actors["b"].Scopes[2] {
		t.Fatal("independent combined inventory")
	}
	if !reflect.DeepEqual(f.Scopes, []string{f.Actors["a"].Scopes[0], string(groupScope), f.Actors["a"].Scopes[2], f.Actors["b"].Scopes[2]}) {
		t.Fatal("combined scope order")
	}
	encode := func(key string) []byte {
		value, err := EncodeRecord(m4ClaimRecord(t, f.Initial[key].Fields))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	marker, e1 := DecodeCompatibilityMarker(encode(ContractsActiveKey))
	guard, e2 := DecodeStoredCommitGuard(encode(CommitGuardKey))
	legacy, e3 := DecodeLegacyRetirementRecord(encode(LegacyRetirementKey))
	if e1 != nil || e2 != nil || e3 != nil {
		t.Fatal("gate records")
	}
	gateInput := TransportGateInput{Mode: GateActive, BootEpoch: strings.Repeat("7", 32), Contract: contract, Compatibility: &marker, CommitGuard: &guard, Legacy: &legacy}
	r := &sharedLuaRedis{data: map[string]bootLuaEntry{}, sets: map[string]map[string]bool{}, zsets: map[string]map[string]float64{}, lists: map[string][]string{},
		now: f.Inputs.Time, runID: strings.Repeat("a", 40), used: 1000000, maximum: 400 * 1024 * 1024}
	for key, entry := range f.Initial {
		m4ClaimLoad(t, r, key, entry)
	}
	boot := bootLuaEntry{kind: "hash", expireAt: -1, hash: map[string]string{"schema_version": "1", "boot_state": "approved", "approved_redis_run_id": r.runID,
		"boot_epoch": gateInput.BootEpoch, "approved_at_ms": "999600", "planned_shutdown_nonce": "", "planned_shutdown_evidence_sha256": "", "last_approval_mode": "initial",
		"consumed_planned_shutdown_nonce": "", "rehearsal_evidence_sha256": strings.Repeat("b", 64), "rehearsal_at_ms": "999600", "acknowledged_loss_bound": "0"}}
	r.data[DurabilityKey] = boot
	protectedBoot := boot
	protectedBoot.hash = map[string]string{}
	for name, value := range boot.hash {
		protectedBoot.hash[name] = value
	}
	type counters struct{ claims, starts, pending, started, terminal uint64 }
	counts := map[string]*counters{"a": {}, "b": {}}
	claimTimes, terminalTimes := map[string]uint64{}, map[string]uint64{}
	scopeUpdates, scopeStarts := map[string]uint64{}, map[string]uint64{}
	firstActor, firstTime := "", uint64(0)
	peerReads := false
	for i, op := range ops {
		label := actors[i]
		a := f.Actors[label]
		peer := f.Actors["a"]
		if label == "a" {
			peer = f.Actors["b"]
		}
		gate, err := NewTransportGate(op, gateInput)
		if err != nil {
			t.Fatal(err)
		}
		var request OperationWireRequest
		semantic := []string{a.RunID, a.JobID, a.Identity.Owner, a.Identity.Token, "1", a.Identity.Reservation}
		switch op {
		case OperationTryClaim:
			request, err = NewTryClaimWireRequest(gate, policies[label], claims[label])
			semantic = append(append(append([]string{}, claimSemantics[label]...), suffixes[label]...), a.Identity.Claim)
		case OperationStartRequest:
			request, err = NewStartRequestWireRequest(gate, policies[label], intents[label])
		case OperationFinishRequest:
			request, err = NewFinishRequestWireRequest(gate, policies[label], intents[label])
		case OperationCancelReservation:
			request, err = NewCancelReservationWireRequest(gate, policies[label], intents[label])
		case OperationMaintainRateScopes:
			request, err = NewMaintainRateScopesWireRequest(gate, 0)
			semantic = []string{"0"}
		}
		if err != nil {
			t.Fatal(err)
		}
		built, err := BuildEvalSHARequest(bundle, request)
		if err != nil {
			t.Fatal(err)
		}
		keys, args := runLuaParts(t, request, nil)
		wantKeys := append(append([]string{}, a.WorkKeys...), "mifolyo:crawl:v2:reservation:"+a.Identity.Reservation)
		for _, scope := range a.Scopes {
			for _, ending := range []string{"", ":active", ":pending", ":started"} {
				wantKeys = append(wantKeys, "mifolyo:crawl:v2:rate:"+scope+ending)
			}
		}
		if op == OperationMaintainRateScopes {
			wantKeys = append(wireOracleAuthorityKeys(), "mifolyo:crawl:v2:rate_scopes")
		}
		if !reflect.DeepEqual(keys, wantKeys) || len(args) != len(semantic)+7 || !reflect.DeepEqual(args[7:], semantic) {
			t.Fatal("literal shared wire layout", i)
		}
		if faults[i] == "owner" {
			args[9] = peer.Identity.Owner
		} else if faults[i] == "token" {
			args[10] = peer.Identity.Token
		}
		parts := append(append([]string{"EVALSHA", built.ScriptSHA1(), strconv.Itoa(len(keys))}, keys...), args...)
		if len(parts) != len(vector.Wires[i].Parts) || vector.Wires[i].Size != built.SerializedSize() || vector.Wires[i].Size > 65536 {
			t.Fatal("serialized shared wire bound")
		}
		for n, value := range parts {
			exported, err := hex.DecodeString(vector.Wires[i].Parts[n])
			if err != nil || !bytes.Equal(exported, []byte(value)) {
				t.Fatal("independent literal shared wire bytes", i, n)
			}
		}
		if !m4ClaimACLAllows(vector.ACL["ledger"], "EVALSHA", keys) {
			t.Fatal("outer shared selector")
		}
		errorStep := strings.HasPrefix(statuses[i], "CRAWL_V2_")
		beforeTime, width := uint64(1000001), uint64(0)
		if profile != "same-ms" && profile != "cancel-same-ms" {
			beforeTime += uint64(i * 3)
			width = 2
		}
		observation := vector.Times[i]
		if observation.Before != beforeTime || observation.After != beforeTime+width || (observation.Now == nil) != errorStep {
			t.Fatal("independent observed times")
		}
		r.now = beforeTime + width/2
		if observation.Now != nil && *observation.Now != r.now {
			t.Fatal("reply time")
		}
		before, writes := r.snapshot(), r.writes
		var canonical string
		for _, binding := range bundle.bindings {
			if binding.operation == op {
				canonical = binding.source
			}
		}
		result := workerLuaRun(t, r, canonical, keys, args)
		expected := vector.Expected[i]
		if result.runtimeErr != nil || expected.ID != fmt.Sprintf("SGC%02d", i+1) || expected.Operation != op {
			t.Fatal("canonical shared execution", i, result.runtimeErr)
		}
		if errorStep {
			var want map[string]string
			if json.Unmarshal(expected.Reply, &want) != nil || !reflect.DeepEqual(want, map[string]string{"error": statuses[i]}) || result.raw != bootLuaErrorReply("ERR "+statuses[i]) {
				t.Fatal("shared rejection", i, result.raw)
			}
		} else {
			var want []any
			if json.Unmarshal(expected.Reply, &want) != nil || want[0] != statuses[i] || !reflect.DeepEqual(result.raw, want) || ValidateOperationResponse(op, result.raw) != nil {
				t.Fatalf("shared reply %d: got %v want %v", i, result.raw, want)
			}
		}
		if statuses[i] == "CAPACITY_BLOCKED" && !reflect.DeepEqual(result.raw, []any{"CAPACITY_BLOCKED", strconv.FormatUint(r.now, 10), string(groupScope), "1", "1", "0"}) {
			t.Fatal("exact shared group denial; global has spare capacity and peer origin is absent")
		}
		workerLuaTrace(t, r)
		for _, call := range r.trace {
			command, touched := call.name, []string{}
			if command == "INFO" {
				command += "|" + call.args[0]
			} else if command != "TIME" {
				touched = []string{call.args[0]}
			}
			if !m4ClaimACLAllows(vector.ACL["ledger"], command, touched) {
				t.Fatal("proposed ACL lacks canonical inner access", i, command)
			}
			if statuses[i] == "CAPACITY_BLOCKED" && len(touched) == 1 && (touched[0] == peer.BaseKey || touched[0] == "mifolyo:crawl:v2:reservation:"+peer.Identity.Reservation) {
				if sharedLuaIsWrite(call.name) {
					t.Fatal("peer read authority escalated to a write")
				}
				peerReads = true
			}
		}
		if !mutations[i] && (r.writes != writes || !reflect.DeepEqual(before, r.snapshot())) {
			t.Fatal("shared error/block/replay mutated state", i)
		}
		m4SharedState(t, r, f, expected.State, protectedBoot, i)
		count := counts[label]
		if mutations[i] {
			switch statuses[i] {
			case "CLAIMED":
				count.claims, count.pending, claimTimes[label] = 1, 1, r.now
			case "STARTED":
				count.starts, count.pending, count.started = 1, 0, 1
				if firstActor == "" {
					firstActor, firstTime = label, r.now
				}
			case "FINISHED", "RESERVATION_CANCELLED":
				count.pending, count.started, count.terminal, terminalTimes[label] = 0, 0, 1, r.now
			}
			for _, scope := range a.Scopes {
				scopeUpdates[scope] = r.now
				if statuses[i] == "STARTED" {
					scopeStarts[scope] = r.now
				}
			}
		}
		for scopeIndex, scope := range f.Scopes {
			key := "mifolyo:crawl:v2:rate:" + scope
			if scopeUpdates[scope] == 0 {
				if _, present := r.data[key]; present {
					t.Fatal("blocked peer origin materialized")
				}
				continue
			}
			pending, started := counts["a"].pending+counts["b"].pending, counts["a"].started+counts["b"].started
			if scopeIndex >= 2 {
				owner := "a"
				if scopeIndex == 3 {
					owner = "b"
				}
				pending, started = counts[owner].pending, counts[owner].started
			}
			concurrency, deadline := uint64(1), scopeStarts[scope]
			if scopeIndex == 0 {
				concurrency, deadline = 2, 0
			}
			for field, value := range map[string]uint64{"active_count": pending + started, "pending_count": pending,
				"started_count": started, "effective_concurrency": concurrency, "effective_interval_ms": 0,
				"last_started_at_ms": scopeStarts[scope], "next_allowed_ms": deadline, "updated_at_ms": scopeUpdates[scope]} {
				if r.data[key].hash[field] != strconv.FormatUint(value, 10) {
					t.Fatal("independent scope accounting/history", i, scopeIndex, field)
				}
			}
			if r.zsets["mifolyo:crawl:v2:rate_scopes"][scope] != float64(scopeUpdates[scope]) {
				t.Fatal("scope inventory timestamp diverged")
			}
		}
		for who, peerCount := range counts {
			actor := f.Actors[who]
			run := r.data[actor.BaseKey].hash
			job := r.data[actor.JobKey].hash
			for field, value := range map[string]uint64{"claims_total": peerCount.claims, "reservation_creations_total": peerCount.claims,
				"request_starts": peerCount.starts, "pending_request_reservations": peerCount.pending, "started_request_reservations": peerCount.started} {
				if run[field] != strconv.FormatUint(value, 10) {
					t.Fatal("independent per-run accounting", i, who, field)
				}
			}
			if job["request_starts"] != strconv.FormatUint(peerCount.starts, 10) || job["delivery_attempts"] != strconv.FormatUint(peerCount.starts, 10) ||
				job["lease_request_starts_baseline"] != "0" || job["next_request_ordinal"] != strconv.FormatUint(1+peerCount.claims, 10) || job["last_document_request_started_at_ms"] != "0" {
				t.Fatal("independent job generation/history")
			}
			if peerCount.claims > 0 {
				expiry := claimTimes[who] + 60000
				reservation := r.data["mifolyo:crawl:v2:reservation:"+actor.Identity.Reservation]
				if job["state"] != "leased" || job["lease_expires_at_ms"] != strconv.FormatUint(expiry, 10) || reservation.hash["expires_at_ms"] != job["lease_expires_at_ms"] ||
					r.zsets["mifolyo:crawl:v2:active_leases"][actor.RunID+":"+actor.JobID] != float64(expiry) {
					t.Fatal("peer lease lost or renewed")
				}
				physical := int64(-1)
				if peerCount.terminal > 0 {
					physical = int64(terminalTimes[who] + 86400000)
				}
				if reservation.expireAt != physical {
					t.Fatal("terminal expiry refunded or extended")
				}
			}
		}
		if firstActor != "" {
			first, err := NewFirstRequestStartEvidence(RunID(f.Actors[firstActor].RunID), JobID(f.Actors[firstActor].JobID), 1, firstTime)
			if err != nil {
				t.Fatal(err)
			}
			record, _ := first.Record()
			if !reflect.DeepEqual(workerLuaHashRecord(t, r, FirstRequestStartKey, SchemaFirstRequestStart), record) {
				t.Fatal("peer overwrote first request start")
			}
		}
	}
	if !peerReads || len(r.zsets["mifolyo:crawl:v2:active_leases"]) != 2 || len(r.zsets["mifolyo:crawl:v2:rate_scopes"]) != 4 {
		t.Fatal("missing peer validation or preserved global inventory")
	}
}

func m4SharedFields(record Record) map[string]string {
	result := map[string]string{}
	for _, field := range record {
		result[field.Name] = string(field.Value)
	}
	return result
}

func m4SharedState(t *testing.T, r *sharedLuaRedis, f m4SharedFixture, expected map[string]*m4ClaimEntry, boot bootLuaEntry, step int) {
	t.Helper()
	if len(expected) != 89 || !reflect.DeepEqual(r.data[DurabilityKey], boot) {
		t.Fatal("combined ownership/BOOT")
	}
	count := 1
	for key, want := range expected {
		got, exists := r.data[key]
		if want == nil {
			if exists {
				t.Fatal("unexpected shared key", step, key)
			}
			continue
		}
		count++
		if !exists || got.kind != want.Type || got.expireAt != want.Expiry {
			t.Fatal("shared type/expiry", step, key)
		}
		switch want.Type {
		case "hash":
			record := m4ClaimRecord(t, want.Fields)
			if !reflect.DeepEqual(m4SharedFields(record), got.hash) {
				t.Fatal("shared full hash state", step, key)
			}
			var schema RecordSchema
			for _, actor := range f.Actors {
				if key == actor.BaseKey {
					schema = SchemaRun
				} else if key == actor.JobKey {
					schema = SchemaJob
				}
			}
			if strings.HasPrefix(key, "mifolyo:crawl:v2:reservation:") {
				schema = SchemaReservation
			} else if strings.HasPrefix(key, "mifolyo:crawl:v2:rate:") {
				schema = SchemaRateScope
			} else if key == FirstRequestStartKey {
				schema = SchemaFirstRequestStart
			}
			if schema != "" && ValidateRecord(schema, record) != nil {
				t.Fatal("shared schema", step, schema)
			}
		case "string":
			if want.Value != got.value {
				t.Fatal("shared string")
			}
		case "set":
			var members []string
			m4ClaimMembers(t, want.Members, &members)
			actual := []string{}
			for member := range r.sets[key] {
				actual = append(actual, member)
			}
			sort.Strings(actual)
			if !reflect.DeepEqual(actual, members) {
				t.Fatal("shared set", step)
			}
		case "zset":
			var members [][]string
			m4ClaimMembers(t, want.Members, &members)
			actual := [][]string{}
			for _, member := range r.orderedZSet(key) {
				actual = append(actual, []string{member, strconv.FormatFloat(r.zsets[key][member], 'f', -1, 64)})
			}
			if !reflect.DeepEqual(actual, members) {
				t.Fatal("shared zset", step, key)
			}
		}
	}
	if len(r.data) != count {
		t.Fatal("unexpected key outside combined inventory")
	}
}
