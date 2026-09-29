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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

// Independent Go identities, literal wires and schemas execute embedded canonical
// Lua against a command facade. Synthetic START grants never perform network I/O.
func TestM4RequestLifecycleOffline(t *testing.T) {
	m4RequestLifecycleProfiles(t, false)
}

// Each profile owns a fresh command facade. The positive-rate deadline controls
// share identical fixture/prefix inputs, rather than retrying a mutated branch.
func m4RequestLifecycleProfiles(t *testing.T, positive bool) {
	ops := []OperationName{OperationTryClaim, OperationTryClaim, OperationFinishRequest, OperationReserveRequest,
		OperationStartRequest, OperationStartRequest, OperationStartRequest, OperationFinishRequest, OperationFinishRequest,
		OperationStartRequest, OperationMaintainRateScopes, OperationReserveRequest, OperationReserveRequest, OperationFinishRequest,
		OperationStartRequest, OperationStartRequest, OperationStartRequest, OperationFinishRequest, OperationFinishRequest,
		OperationFinishRequest, OperationStartRequest, OperationMaintainRateScopes}
	statuses := strings.Fields(`CLAIMED ALREADY_CLAIMED CRAWL_V2_INVALID_STATE CRAWL_V2_INVALID_STATE CRAWL_V2_IMMUTABLE_MISMATCH
		STARTED ALREADY_STARTED FINISHED ALREADY_FINISHED ALREADY_STARTED BATCH_DONE RESERVED ALREADY_RESERVED ALREADY_FINISHED
		STARTED ALREADY_STARTED ALREADY_STARTED CRAWL_V2_IMMUTABLE_MISMATCH FINISHED ALREADY_FINISHED ALREADY_STARTED BATCH_DONE`)
	mutations := map[int]bool{0: true, 5: true, 7: true, 11: true, 14: true, 18: true}
	errorSteps := map[int]bool{2: true, 3: true, 4: true, 17: true}
	profiles := []string{"spaced", "same-ms"}
	caseID, purpose, packetFile, assertionPrefix := "ledger-request-lifecycle-v1", "offline_public_request_vectors", "test_request_oracle.py", "REQ"
	interval := uint64(0)
	if positive {
		profiles = []string{"before", "at", "after"}
		caseID, purpose, packetFile, assertionPrefix = "ledger-positive-interval-v1", "offline_public_rate_vectors", "test_rate_oracle.py", "RATE"
		interval = 8000
		ops = []OperationName{OperationTryClaim, OperationTryClaim, OperationFinishRequest, OperationReserveRequest,
			OperationStartRequest, OperationStartRequest, OperationStartRequest, OperationFinishRequest, OperationFinishRequest,
			OperationStartRequest, OperationMaintainRateScopes, OperationReserveRequest, OperationReserveRequest, OperationReserveRequest,
			OperationFinishRequest, OperationStartRequest, OperationReserveRequest, OperationStartRequest, OperationStartRequest,
			OperationFinishRequest, OperationFinishRequest, OperationFinishRequest, OperationStartRequest, OperationMaintainRateScopes}
		statuses = strings.Fields(`CLAIMED ALREADY_CLAIMED CRAWL_V2_INVALID_STATE CRAWL_V2_INVALID_STATE CRAWL_V2_IMMUTABLE_MISMATCH
			STARTED ALREADY_STARTED FINISHED ALREADY_FINISHED ALREADY_STARTED BATCH_DONE RATE_BLOCKED RESERVED ALREADY_RESERVED
			ALREADY_FINISHED STARTED ALREADY_RESERVED ALREADY_STARTED ALREADY_STARTED CRAWL_V2_IMMUTABLE_MISMATCH
			FINISHED ALREADY_FINISHED ALREADY_STARTED BATCH_DONE`)
		mutations = map[int]bool{0: true, 5: true, 7: true, 12: true, 15: true, 20: true}
		errorSteps = map[int]bool{2: true, 3: true, 4: true, 19: true}
	}
	bundle, err := AuthoritativeScriptBindingSet()
	if err != nil {
		t.Fatal(err)
	}
	contract, _ := ContractSHA256()
	for _, profile := range profiles {
		t.Run(profile, func(t *testing.T) {
			count := len(ops)
			if positive && profile == "before" {
				count = 12
			}
			ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
			defer cancel()
			path := filepath.Join(fixtureRepositoryRoot(t), "tests/crawl-jobs-v2-redis", packetFile)
			raw, err := exec.CommandContext(ctx, "python3", "-B", path, "--go-vectors", profile).Output()
			if err != nil || len(raw) > 2*1024*1024 {
				t.Fatal("request vectors failed or exceeded their packet bound", err)
			}
			var vector struct {
				Purpose      string              `json:"purpose"`
				Profile      string              `json:"profile"`
				Authorized   bool                `json:"execution_authorized"`
				Fixture      m4ClaimFixture      `json:"fixture"`
				ACL          map[string][]string `json:"acl_rules"`
				Observations []struct {
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
				Wires []m4NegativeWire `json:"wires"`
			}
			if json.Unmarshal(raw, &vector) != nil || vector.Purpose != purpose || vector.Profile != profile || vector.Authorized ||
				len(vector.Expected) != count || len(vector.Observations) != count || len(vector.Wires) != count {
				t.Fatal("invalid request vector envelope")
			}
			f := vector.Fixture
			if f.Case != caseID || f.Authorized || f.Measured != "not_measured" || len(f.Initial) != 57 || !reflect.DeepEqual(f.Bootstrap, []string{DurabilityKey}) {
				t.Fatal("invalid request fixture scope")
			}
			runID := RunID(f.Inputs.FixtureID)
			lineage := RateScopeID(digestFramed("mifolyo:m4:claim-release:lineage:v1", []byte(runID))[:32])
			groupScope, _ := DeriveGroupScopeID(lineage)
			group := PolicyGroup{GroupID: "fixture", RateScopeID: lineage, GroupScopeID: groupScope, RequestStartLimit: 10, Concurrency: 1, IntervalMS: interval}
			groupRecord, _ := policyGroupRecord(group)
			if !reflect.DeepEqual(groupRecord, m4ClaimRecord(t, f.Group)) {
				t.Fatal("independent group definition mismatch")
			}
			const document = "https://m4-fixture.invalid/document"
			const robots = "https://m4-fixture.invalid/robots.txt"
			jobID := JobID(utils.URLIDV1(document))
			targets := []RequestTarget{{URLID: JobID(utils.URLIDV1(robots)), CanonicalURL: robots}, {URLID: jobID, CanonicalURL: document}}
			decisions := make([]PolicyDecision, 2)
			for i, kind := range []RequestKind{RequestRobots, RequestDocument} {
				decisions[i], err = NewPolicyDecision(PolicyDecisionInput{RequestKind: kind, Target: targets[i], Depth: 1, GroupID: "fixture", RateScopeID: lineage, GroupConcurrency: 1, OriginConcurrency: 1, GroupIntervalMS: interval, OriginIntervalMS: interval})
				if err != nil {
					t.Fatal(err)
				}
			}
			source := SourceJob{JobID: jobID, CanonicalURL: document, ScoreText: "0", Depth: 1, GroupID: "fixture", RateScopeID: lineage, Decision: decisions[1]}
			sourceRecord, _ := sourceJobRecord(source)
			if !reflect.DeepEqual(sourceRecord, m4ClaimRecord(t, f.Source)) || f.RunID != string(runID) || f.JobID != string(jobID) ||
				f.BaseKey != "mifolyo:crawl:v2:run:"+string(runID) || f.JobKey != f.BaseKey+":job:"+string(jobID) {
				t.Fatal("independent source/identity mismatch")
			}
			runRecord := m4ClaimRecord(t, f.Initial[f.BaseKey].Fields)
			if ValidateRecord(SchemaRun, runRecord) != nil || ValidateRecord(SchemaJob, m4ClaimRecord(t, f.Initial[f.JobKey].Fields)) != nil {
				t.Fatal("initial schema mismatch")
			}
			runFields := map[string]string{}
			for _, field := range runRecord {
				runFields[field.Name] = string(field.Value)
			}
			if positive {
				// Literal test policy, independent of Python's descriptor constructor.
				descriptor, e := json.Marshal(map[string]any{"purpose": "conformance_only", "case": caseID, "kind": "crawl_policy", "release_eligible": false,
					"global_concurrency": 2, "global_interval_ms": 0, "group_concurrency": 1, "group_interval_ms": 8000, "origin_concurrency": 1, "origin_interval_ms": 8000})
				if e != nil {
					t.Fatal(e)
				}
				digest := sha256.Sum256(append(descriptor, '\n'))
				if runFields["crawl_policy_sha256"] != hex.EncodeToString(digest[:]) {
					t.Fatal("positive policy descriptor mismatch")
				}
			}
			sourceDigest, _ := DeriveSourceDigest([]SourceJob{source})
			groupDigest, _ := DerivePolicyGroupMapDigest([]PolicyGroup{group})
			if runFields["source_sha256"] != string(sourceDigest) || runFields["policy_group_map_sha256"] != string(groupDigest) || runFields["contract_sha256"] != string(contract) {
				t.Fatal("run digest mismatch")
			}
			policy, err := (transportAuthority{seal: &redisTransportAuthoritySeal}).parseRunPolicyAuthority(runID, runRecord, []PolicyGroup{group})
			if err != nil {
				t.Fatal(err)
			}
			encode := func(key string) []byte {
				value, e := EncodeRecord(m4ClaimRecord(t, f.Initial[key].Fields))
				if e != nil {
					t.Fatal(e)
				}
				return value
			}
			marker, e1 := DecodeCompatibilityMarker(encode(ContractsActiveKey))
			guard, e2 := DecodeStoredCommitGuard(encode(CommitGuardKey))
			legacy, e3 := DecodeLegacyRetirementRecord(encode(LegacyRetirementKey))
			if e1 != nil || e2 != nil || e3 != nil {
				t.Fatal("gate record mismatch")
			}
			gateInput := TransportGateInput{Mode: GateActive, BootEpoch: strings.Repeat("7", 32), Contract: contract, Compatibility: &marker, CommitGuard: &guard, Legacy: &legacy}
			lease := LeaseIdentity{RunID: runID, JobID: jobID, OwnerID: OwnerID(f.Inputs.OwnerA), Token: LeaseToken(f.Inputs.TokenA), Fence: 1}
			intents, reservations := make([]ReservationIntent, 2), make([]string, 2)
			for i, label := range []string{"a", "b"} {
				intents[i] = ReservationIntent{Lease: lease, RequestOrdinal: uint64(i + 1), Target: targets[i], CrawlPolicyDigest: Digest(runFields["crawl_policy_sha256"]), Decision: decisions[i]}
				q, e := DeriveReservationID(policy, intents[i])
				if e != nil {
					t.Fatal(e)
				}
				reservations[i] = string(q)
				if f.Identities[label].Reservation != string(q) {
					t.Fatal("same-lease reservation derivation mismatch")
				}
			}
			claimInput := TryClaimTransitionInput{Job: source, Lease: lease, ExpectedPriorFence: 0, InitialIntent: intents[0]}
			claimID, err := DeriveTryClaimTransitionID(policy, claimInput)
			if err != nil || f.Identities["a"].Claim != string(claimID) {
				t.Fatal("claim transition mismatch")
			}
			m4ClaimInventory(t, f, decisions[0], reservations)
			suffix := func(i int) []string {
				d := decisions[i]
				digest, e := DerivePolicyDecisionDigest(d)
				if e != nil {
					t.Fatal(e)
				}
				return []string{strconv.Itoa(i + 1), string(d.RequestKind), string(targets[i].URLID), targets[i].CanonicalURL, string(d.TargetDigest),
					runFields["crawl_policy_sha256"], string(digest), "fixture", string(lineage), string(d.GlobalScopeID), string(d.GroupScopeID), string(d.OriginScopeID), "2", "0", "1", strconv.FormatUint(interval, 10), "1", strconv.FormatUint(interval, 10)}
			}
			r := &sharedLuaRedis{data: map[string]bootLuaEntry{}, sets: map[string]map[string]bool{}, zsets: map[string]map[string]float64{},
				lists: map[string][]string{}, now: f.Inputs.Time, runID: strings.Repeat("a", 40), used: 1000000, maximum: 400 * 1024 * 1024}
			for key, entry := range f.Initial {
				m4ClaimLoad(t, r, key, entry)
			}
			boot := bootLuaEntry{kind: "hash", expireAt: -1, hash: map[string]string{
				"schema_version": "1", "boot_state": "approved", "approved_redis_run_id": r.runID, "boot_epoch": gateInput.BootEpoch,
				"approved_at_ms": strconv.FormatUint(f.Inputs.Time-400, 10), "planned_shutdown_nonce": "", "planned_shutdown_evidence_sha256": "", "last_approval_mode": "initial",
				"consumed_planned_shutdown_nonce": "", "rehearsal_evidence_sha256": strings.Repeat("b", 64), "rehearsal_at_ms": strconv.FormatUint(f.Inputs.Time-400, 10), "acknowledged_loss_bound": "0"}}
			r.data[DurabilityKey] = boot
			protectedBoot := boot
			protectedBoot.hash = map[string]string{}
			for name, value := range boot.hash {
				protectedBoot.hash[name] = value
			}
			for i, op := range ops[:count] {
				who := 0
				if (!positive && (i == 3 || i == 11 || i == 12 || i == 14 || i == 15 || i >= 17)) ||
					(positive && (i == 3 || i == 11 || i == 12 || i == 13 || i == 15 || i == 16 || i == 17 || i >= 19)) {
					who = 1
				}
				gate, e := NewTransportGate(op, gateInput)
				if e != nil {
					t.Fatal(e)
				}
				var request OperationWireRequest
				var semantic []string
				switch op {
				case OperationTryClaim:
					request, err = NewTryClaimWireRequest(gate, policy, claimInput)
					decisionDigest, _ := DerivePolicyDecisionDigest(decisions[1])
					semantic = []string{string(runID), string(jobID), document, "0", "1", "fixture", string(lineage), string(decisions[1].GroupScopeID), string(decisions[1].OriginScopeID), string(decisionDigest), "0", "1", f.Inputs.OwnerA, f.Inputs.TokenA}
					semantic = append(semantic, suffix(0)...)
					semantic = append(semantic, string(claimID))
				case OperationReserveRequest:
					request, err = NewReserveRequestWireRequest(gate, policy, intents[who])
					semantic = append([]string{string(runID), string(jobID), f.Inputs.OwnerA, f.Inputs.TokenA, "1"}, suffix(who)...)
				case OperationStartRequest:
					request, err = NewStartRequestWireRequest(gate, policy, intents[who])
				case OperationFinishRequest:
					request, err = NewFinishRequestWireRequest(gate, policy, intents[who])
				case OperationMaintainRateScopes:
					request, err = NewMaintainRateScopesWireRequest(gate, 0)
					semantic = []string{"0"}
				}
				if err != nil {
					t.Fatal(err)
				}
				if semantic == nil {
					semantic = []string{string(runID), string(jobID), f.Inputs.OwnerA, f.Inputs.TokenA, "1", reservations[who]}
				}
				built, err := BuildEvalSHARequest(bundle, request)
				if err != nil {
					t.Fatal(err)
				}
				keys, args := runLuaParts(t, request, nil)
				wantKeys := append([]string(nil), f.WorkKeys...)
				wantKeys = append(wantKeys, "mifolyo:crawl:v2:reservation:"+reservations[who])
				for _, scope := range f.Scopes {
					for _, ending := range []string{"", ":active", ":pending", ":started"} {
						wantKeys = append(wantKeys, "mifolyo:crawl:v2:rate:"+scope+ending)
					}
				}
				if op == OperationMaintainRateScopes {
					wantKeys = append(wireOracleAuthorityKeys(), "mifolyo:crawl:v2:rate_scopes")
				}
				if !reflect.DeepEqual(keys, wantKeys) || len(args) != len(semantic)+7 || !reflect.DeepEqual(args[7:], semantic) {
					t.Fatalf("literal wire layout mismatch step %d", i)
				}
				if i == 4 || (!positive && i == 17) || (positive && i == 19) {
					args[10] = f.Inputs.WrongToken
				} // Valid Q, deliberately mismatched lease token.
				parts := []string{"EVALSHA", built.ScriptSHA1(), strconv.Itoa(len(keys))}
				parts = append(parts, keys...)
				parts = append(parts, args...)
				wire := vector.Wires[i]
				if len(parts) != len(wire.Parts) || wire.Size != built.SerializedSize() || wire.Size > 65536 {
					t.Fatal("wire size mismatch")
				}
				for n, part := range parts {
					value, e := hex.DecodeString(wire.Parts[n])
					if e != nil || !bytes.Equal(value, []byte(part)) {
						t.Fatalf("wire bytes mismatch step %d part %d", i, n)
					}
				}
				if !m4ClaimACLAllows(vector.ACL["ledger"], "EVALSHA", keys) {
					t.Fatal("outer key selector mismatch")
				}
				observation := vector.Observations[i]
				wantedBefore := uint64(1000001)
				width := uint64(0)
				if profile == "spaced" || positive {
					wantedBefore += uint64(i * 3)
					width = 2
				}
				if positive && i >= 11 {
					width = 0
					if i == 11 {
						wantedBefore = 1000017 + 8000 - 1
					} else {
						wantedBefore = 1000017 + 8000 + uint64((i-12)*3)
						if profile == "after" {
							wantedBefore++
						}
					}
				}
				if observation.Before != wantedBefore || observation.After != wantedBefore+width || (observation.Now == nil) != errorSteps[i] {
					t.Fatal("missing precise time vector")
				}
				r.now = wantedBefore + width/2
				if observation.Now != nil && *observation.Now != r.now {
					t.Fatal("response time vector differs")
				}
				before, writes := r.snapshot(), r.writes
				beforeJob := workerLuaHashRecord(t, r, f.JobKey, SchemaJob)
				var canonical string
				for _, binding := range bundle.bindings {
					if binding.operation == op {
						canonical = binding.source
					}
				}
				if canonical == "" {
					t.Fatal("canonical source missing")
				}
				result := workerLuaRun(t, r, canonical, keys, args)
				expected := vector.Expected[i]
				if result.runtimeErr != nil || expected.ID != fmt.Sprintf("%s%02d", assertionPrefix, i+1) || expected.Operation != op {
					t.Fatal("canonical execution or identity failure", result.runtimeErr)
				}
				if errorSteps[i] {
					var want map[string]string
					if json.Unmarshal(expected.Reply, &want) != nil || !reflect.DeepEqual(want, map[string]string{"error": statuses[i]}) || result.raw != bootLuaErrorReply("ERR "+statuses[i]) {
						t.Fatalf("unexpected error at step %d: %v", i, result.raw)
					}
				} else {
					var want []any
					if json.Unmarshal(expected.Reply, &want) != nil || want[0] != statuses[i] || !reflect.DeepEqual(result.raw, want) || ValidateOperationResponse(op, result.raw) != nil {
						t.Fatalf("reply mismatch at step %d: got %v want %v", i, result.raw, want)
					}
				}
				if positive && i == 11 {
					want := []any{"RATE_BLOCKED", strconv.FormatUint(r.now, 10), string(groupScope), "1008017", "1"}
					if !reflect.DeepEqual(result.raw, want) {
						t.Fatal("positive denial scope/deadline/after-IO tail mismatch")
					}
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
						t.Fatalf("ACL lacks %s at step %d", command, i)
					}
				}
				if !mutations[i] && (r.writes != writes || !reflect.DeepEqual(before, r.snapshot())) {
					t.Fatalf("read-only error/replay/maintenance mutated at step %d", i)
				}
				m4ClaimState(t, r, f, expected.State, protectedBoot, i+1)
				afterJob := workerLuaHashRecord(t, r, f.JobKey, SchemaJob)
				if ValidateJobRequestStartsTransition(op, beforeJob, afterJob) != nil {
					t.Fatal("invalid request-start generation transition")
				}
				if i >= 5 {
					first := workerLuaHashRecord(t, r, FirstRequestStartKey, SchemaFirstRequestStart)
					start := *vector.Observations[5].Now
					want, e := NewFirstRequestStartEvidence(runID, jobID, 1, start)
					if e != nil {
						t.Fatal(e)
					}
					wantedRecord, e := want.Record()
					if e != nil || !reflect.DeepEqual(first, wantedRecord) {
						t.Fatal("first-start schema or immutability mismatch")
					}
				}
			}
		})
	}
}
