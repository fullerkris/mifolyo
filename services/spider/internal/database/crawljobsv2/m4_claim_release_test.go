package crawljobsv2

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

type m4ClaimEntry struct {
	Type    string          `json:"type"`
	Fields  [][]string      `json:"fields"`
	Members json.RawMessage `json:"members"`
	Value   string          `json:"value"`
	Expiry  int64           `json:"expires_at_ms"`
}

type m4ClaimFixture struct {
	Case       string `json:"case"`
	Authorized bool   `json:"execution_authorized"`
	Measured   string `json:"measurement_status"`
	Inputs     struct {
		Time       uint64 `json:"redis_time_ms"`
		FixtureID  string `json:"fixture_id"`
		OwnerA     string `json:"owner_a"`
		OwnerB     string `json:"owner_b"`
		TokenA     string `json:"token_a"`
		TokenB     string `json:"token_b"`
		WrongToken string `json:"wrong_token"`
	} `json:"inputs"`
	RunID      string     `json:"run_id"`
	JobID      string     `json:"job_id"`
	BaseKey    string     `json:"base_key"`
	JobKey     string     `json:"job_key"`
	WorkKeys   []string   `json:"work_keys"`
	Inventory  []string   `json:"key_inventory"`
	Bootstrap  []string   `json:"bootstrap_owned_keys"`
	Scopes     []string   `json:"scope_ids"`
	Group      [][]string `json:"policy_group_fields"`
	Source     [][]string `json:"source_fields"`
	Identities map[string]struct {
		Reservation string `json:"reservation_id"`
		Claim       string `json:"claim_transition_id"`
		Release     string `json:"release_transition_id"`
	} `json:"identities"`
	Initial map[string]*m4ClaimEntry `json:"initial_state"`
}

func m4ClaimRecord(t *testing.T, pairs [][]string) Record {
	t.Helper()
	record := make(Record, len(pairs))
	seen := map[string]bool{}
	for i, pair := range pairs {
		if len(pair) != 2 || seen[pair[0]] {
			t.Fatal("invalid or duplicate fixture field")
		}
		seen[pair[0]] = true
		record[i] = textField(pair[0], pair[1])
	}
	return record
}

func m4ClaimMembers(t *testing.T, raw json.RawMessage, value any) {
	t.Helper()
	if err := json.Unmarshal(raw, value); err != nil {
		t.Fatal("invalid fixture collection")
	}
}

// The Redis facade supplies only in-memory command semantics. The expected
// transitions come from Python, and the operations execute unchanged embedded
// canonical Lua. This is deliberately NOT target Redis/ACL/AOF acceptance.
func TestM4ClaimReleaseOffline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	path := filepath.Join(fixtureRepositoryRoot(t), "tests/crawl-jobs-v2-redis/test_claim_release.py")
	raw, err := exec.CommandContext(ctx, "python3", "-B", path, "--go-vectors").Output()
	if err != nil || len(raw) > 2*1024*1024 {
		t.Fatal("offline claim fixture generation failed or exceeded bound")
	}
	var vectors struct {
		Vectors []struct {
			Fixture  m4ClaimFixture      `json:"fixture"`
			Times    []uint64            `json:"times"`
			ACLRules map[string][]string `json:"acl_rules"`
			Expected []struct {
				ID    string                   `json:"assertion_id"`
				Reply []string                 `json:"reply"`
				State map[string]*m4ClaimEntry `json:"state"`
			} `json:"expected"`
			Wires []struct {
				Parts []string `json:"parts_hex"`
				Size  uint64   `json:"resp_size"`
			} `json:"wires"`
		} `json:"vectors"`
	}
	if json.Unmarshal(raw, &vectors) != nil || len(vectors.Vectors) != 2 {
		t.Fatal("invalid claim fixture vector inventory")
	}
	bundle, err := AuthoritativeScriptBindingSet()
	if err != nil {
		t.Fatal(err)
	}
	contract, _ := ContractSHA256()
	for vectorIndex, vector := range vectors.Vectors {
		t.Run(strconv.Itoa(vectorIndex), func(t *testing.T) {
			f := vector.Fixture
			if f.Case != "ledger-claim-release-v1" || f.Authorized || f.Measured != "not_measured" ||
				len(vector.Times) != 9 || len(vector.Wires) != 9 || len(vector.Expected) != 9 ||
				!reflect.DeepEqual(f.Bootstrap, []string{DurabilityKey}) || len(f.Initial) != 57 {
				t.Fatal("wrong offline fixture scope")
			}
			runID := RunID(f.Inputs.FixtureID)
			lineage := RateScopeID(digestFramed("mifolyo:m4:claim-release:lineage:v1", []byte(runID))[:32])
			groupScope, _ := DeriveGroupScopeID(lineage)
			group := PolicyGroup{GroupID: "fixture", RateScopeID: lineage, GroupScopeID: groupScope,
				RequestStartLimit: 10, Concurrency: 1, IntervalMS: 0}
			groupRecord, _ := policyGroupRecord(group)
			if !reflect.DeepEqual(groupRecord, m4ClaimRecord(t, f.Group)) {
				t.Fatal("independent group record mismatch")
			}
			const document = "https://m4-fixture.invalid/document"
			const robots = "https://m4-fixture.invalid/robots.txt"
			jobID := JobID(utils.URLIDV1(document))
			documentDecision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestDocument,
				Target: RequestTarget{URLID: jobID, CanonicalURL: document}, Depth: 1, GroupID: group.GroupID,
				RateScopeID: lineage, GroupConcurrency: 1, OriginConcurrency: 1})
			if err != nil {
				t.Fatal(err)
			}
			robotsTarget := RequestTarget{URLID: JobID(utils.URLIDV1(robots)), CanonicalURL: robots}
			robotsDecision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestRobots, Target: robotsTarget,
				Depth: 1, GroupID: group.GroupID, RateScopeID: lineage, GroupConcurrency: 1, OriginConcurrency: 1})
			if err != nil {
				t.Fatal(err)
			}
			source := SourceJob{JobID: jobID, CanonicalURL: document, ScoreText: "0", Depth: 1,
				GroupID: group.GroupID, RateScopeID: lineage, Decision: documentDecision}
			sourceRecord, _ := sourceJobRecord(source)
			if !reflect.DeepEqual(sourceRecord, m4ClaimRecord(t, f.Source)) {
				t.Fatal("independent source record mismatch")
			}
			base := "mifolyo:crawl:v2:run:" + string(runID)
			if f.RunID != string(runID) || f.JobID != string(jobID) || f.BaseKey != base || f.JobKey != base+":job:"+string(jobID) {
				t.Fatal("independent run/job key mismatch")
			}
			runRecord := m4ClaimRecord(t, f.Initial[base].Fields)
			if ValidateRecord(SchemaRun, runRecord) != nil || ValidateRecord(SchemaJob, m4ClaimRecord(t, f.Initial[f.JobKey].Fields)) != nil {
				t.Fatal("Go rejected fixed run/job record shape or relations")
			}
			run := map[string]string{}
			for _, field := range runRecord {
				run[field.Name] = string(field.Value)
			}
			sourceDigest, _ := DeriveSourceDigest([]SourceJob{source})
			groupDigest, _ := DerivePolicyGroupMapDigest([]PolicyGroup{group})
			if run["source_sha256"] != string(sourceDigest) || run["policy_group_map_sha256"] != string(groupDigest) || run["contract_sha256"] != string(contract) {
				t.Fatal("independent source/group/contract identity mismatch")
			}
			policy, err := (transportAuthority{seal: &redisTransportAuthoritySeal}).parseRunPolicyAuthority(runID, runRecord, []PolicyGroup{group})
			if err != nil {
				t.Fatal("Go rejected fixture run policy")
			}
			markerBytes, _ := EncodeRecord(m4ClaimRecord(t, f.Initial[ContractsActiveKey].Fields))
			guardBytes, _ := EncodeRecord(m4ClaimRecord(t, f.Initial[CommitGuardKey].Fields))
			legacyBytes, _ := EncodeRecord(m4ClaimRecord(t, f.Initial[LegacyRetirementKey].Fields))
			marker, err := DecodeCompatibilityMarker(markerBytes)
			if err != nil {
				t.Fatal(err)
			}
			guard, err := DecodeStoredCommitGuard(guardBytes)
			if err != nil {
				t.Fatal(err)
			}
			legacy, err := DecodeLegacyRetirementRecord(legacyBytes)
			if err != nil {
				t.Fatal(err)
			}
			gateInput := TransportGateInput{Mode: GateActive, BootEpoch: strings.Repeat("7", 32), Contract: contract,
				Compatibility: &marker, CommitGuard: &guard, Legacy: &legacy}
			leases := []LeaseIdentity{
				{RunID: runID, JobID: jobID, OwnerID: OwnerID(f.Inputs.OwnerA), Token: LeaseToken(f.Inputs.TokenA), Fence: 1},
				{RunID: runID, JobID: jobID, OwnerID: OwnerID(f.Inputs.OwnerB), Token: LeaseToken(f.Inputs.TokenB), Fence: 2},
				{RunID: runID, JobID: jobID, OwnerID: OwnerID(f.Inputs.OwnerA), Token: LeaseToken(f.Inputs.WrongToken), Fence: 1},
			}
			claims := make([]TryClaimTransitionInput, 2)
			reservations := make([]string, 2)
			for i, label := range []string{"a", "b"} {
				intent := ReservationIntent{Lease: leases[i], RequestOrdinal: uint64(i + 1), Target: robotsTarget,
					CrawlPolicyDigest: Digest(run["crawl_policy_sha256"]), Decision: robotsDecision}
				claims[i] = TryClaimTransitionInput{Job: source, Lease: leases[i], ExpectedPriorFence: uint64(i), InitialIntent: intent}
				reservationID, e := DeriveReservationID(policy, intent)
				if e != nil {
					t.Fatal(e)
				}
				claimID, e := DeriveTryClaimTransitionID(policy, claims[i])
				if e != nil {
					t.Fatal(e)
				}
				releaseID, e := DeriveReleaseBeforeIOTransitionID(ReleaseBeforeIOTransitionInput{Lease: leases[i]})
				if e != nil {
					t.Fatal(e)
				}
				if f.Identities[label].Reservation != string(reservationID) || f.Identities[label].Claim != string(claimID) || f.Identities[label].Release != string(releaseID) {
					t.Fatal("independent reservation/transition identity mismatch")
				}
				reservations[i] = string(reservationID)
			}
			wrongID, _ := DeriveReleaseBeforeIOTransitionID(ReleaseBeforeIOTransitionInput{Lease: leases[2]})
			if f.Identities["wrong"].Release != string(wrongID) {
				t.Fatal("wrong-token control is not digest-valid")
			}
			m4ClaimInventory(t, f, robotsDecision, reservations)
			r := &sharedLuaRedis{data: map[string]bootLuaEntry{}, sets: map[string]map[string]bool{}, zsets: map[string]map[string]float64{},
				lists: map[string][]string{}, now: f.Inputs.Time, runID: strings.Repeat("a", 40), used: 1000000, maximum: 400 * 1024 * 1024}
			for key, entry := range f.Initial {
				m4ClaimLoad(t, r, key, entry)
			}
			// Explicit in-memory BOOT control, never supplied as a Python setup write.
			boot := bootLuaEntry{kind: "hash", expireAt: -1, hash: map[string]string{
				"schema_version": "1", "boot_state": "approved", "approved_redis_run_id": r.runID, "boot_epoch": gateInput.BootEpoch,
				"approved_at_ms": strconv.FormatUint(f.Inputs.Time-400, 10), "planned_shutdown_nonce": "", "planned_shutdown_evidence_sha256": "",
				"last_approval_mode": "initial", "consumed_planned_shutdown_nonce": "", "rehearsal_evidence_sha256": strings.Repeat("b", 64),
				"rehearsal_at_ms": strconv.FormatUint(f.Inputs.Time-400, 10), "acknowledged_loss_bound": "0"}}
			r.data[DurabilityKey] = boot
			protectedBoot := boot
			protectedBoot.hash = map[string]string{}
			for key, value := range boot.hash {
				protectedBoot.hash[key] = value
			}
			identities := []int{0, 0, 2, 0, 0, 1, 0, 1, 1}
			statuses := []Status{StatusClaimed, StatusAlreadyClaimed, StatusLeaseLost, StatusReleasedReady, StatusReleasedReady,
				StatusClaimed, StatusLeaseLost, StatusReleasedReady, StatusReleasedReady}
			for i, who := range identities {
				op := OperationReleaseBeforeIO
				if i == 0 || i == 1 || i == 5 {
					op = OperationTryClaim
				}
				gate, e := NewTransportGate(op, gateInput)
				if e != nil {
					t.Fatal(e)
				}
				var request OperationWireRequest
				if op == OperationTryClaim {
					request, e = NewTryClaimWireRequest(gate, policy, claims[who])
				} else {
					request, e = NewReleaseBeforeIOWireRequest(gate, ReleaseBeforeIOTransitionInput{Lease: leases[who]})
				}
				if e != nil {
					t.Fatal(e)
				}
				built, e := BuildEvalSHARequest(bundle, request)
				if e != nil {
					t.Fatal(e)
				}
				parts := [][]byte{[]byte("EVALSHA"), []byte(built.ScriptSHA1()), []byte(strconv.Itoa(len(built.Keys())))}
				parts = append(parts, built.Keys()...)
				parts = append(parts, built.Arguments()...)
				wire := vector.Wires[i]
				if len(parts) != len(wire.Parts) || built.SerializedSize() != wire.Size {
					t.Fatal("wire size mismatch")
				}
				for n, part := range parts {
					expected, decodeErr := hex.DecodeString(wire.Parts[n])
					if decodeErr != nil || !bytes.Equal(part, expected) {
						t.Fatalf("wire mismatch at step %d part %d", i+1, n)
					}
				}
				wireKeys := make([]string, len(built.Keys()))
				for n, key := range built.Keys() {
					wireKeys[n] = string(key)
				}
				if !m4ClaimACLAllows(vector.ACLRules["ledger"], "EVALSHA", wireKeys) {
					t.Fatal("ledger ACL rejects the outer wire")
				}
				r.now = vector.Times[i]
				beforeWrites := r.writes
				keys, args := runLuaParts(t, request, nil)
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
				reply := sharedLuaNoError(t, result).([]any)
				if reply[0] != string(statuses[i]) || ValidateOperationResponse(op, reply) != nil {
					t.Fatalf("invalid response at step %d", i+1)
				}
				want := make([]any, len(vector.Expected[i].Reply))
				for n, value := range vector.Expected[i].Reply {
					want[n] = value
				}
				if !reflect.DeepEqual(reply, want) {
					t.Fatalf("response oracle differs at step %d", i+1)
				}
				workerLuaTrace(t, r)
				for _, call := range r.trace {
					command, keys := call.name, []string{}
					if command == "INFO" {
						command += "|" + call.args[0]
					} else if command != "TIME" {
						keys = []string{call.args[0]}
					}
					if !m4ClaimACLAllows(vector.ACLRules["ledger"], command, keys) {
						t.Fatalf("ledger ACL lacks canonical %s at step %d", command, i+1)
					}
				}
				if i == 1 || i == 2 || i == 4 || i == 6 || i == 8 {
					if r.writes != beforeWrites {
						t.Fatalf("replay/rejection wrote at step %d", i+1)
					}
				}
				if vector.Expected[i].ID != fmt.Sprintf("CR%02d", i+1) {
					t.Fatal("assertion order mismatch")
				}
				m4ClaimState(t, r, f, vector.Expected[i].State, protectedBoot, i+1)
			}
		})
	}
}

// Independent literal-selector check over actual canonical command traces.
// This proves inventory completeness locally, not target Redis ACL semantics.
func m4ClaimACLAllows(rules []string, command string, keys []string) bool {
	text := strings.Join(rules, " ")
	pattern := regexp.MustCompile(`\(([^()]*)\)`)
	selectors := []string{pattern.ReplaceAllString(text, "")}
	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		selectors = append(selectors, match[1])
	}
	command = strings.ToLower(command)
	writes := map[string]bool{"evalsha": true, "hset": true, "set": true, "zadd": true, "zrem": true, "pexpireat": true}
	for _, selector := range selectors {
		allowed := false
		admitted := map[string]bool{}
		for _, token := range strings.Fields(selector) {
			if token == "+"+command {
				allowed = true
			}
			if strings.HasPrefix(token, "~") {
				admitted[strings.TrimPrefix(token, "~")] = true
			} else if strings.HasPrefix(token, "%R~") && !writes[command] {
				admitted[strings.TrimPrefix(token, "%R~")] = true
			}
		}
		if !allowed {
			continue
		}
		all := true
		for _, key := range keys {
			if !admitted[key] {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func m4ClaimInventory(t *testing.T, f m4ClaimFixture, decision PolicyDecision, reservations []string) {
	t.Helper()
	// Literal section 10.1.1 oracle, independent of production key-plan tables.
	work := []string{DurabilityKey, ContractsActiveKey, CrawlContractKey, ContractsCandidateKey, CrawlContractCandidateKey,
		CommitGuardKey, LegacyRetirementKey, AdminFreezeKey}
	for _, name := range strings.Fields("runs active_runs unarchived_runs first_request_start active_leases stage_expiry stage_slots rate_scopes") {
		work = append(work, "mifolyo:crawl:v2:"+name)
	}
	work = append(work, f.BaseKey)
	for _, name := range strings.Fields("jobs job_order ready ready_at leased leased_at delayed commit_backpressure completed dead cancelled group_limits group_rate_scope_ids group_scope_ids group_concurrency group_interval_ms group_started group_pending group_active_started group_open_jobs audit_group_counts retry_reason_counts recovery_outcome_counts disposition_reason_counts visited_depth visited_urls") {
		work = append(work, f.BaseKey+":"+name)
	}
	work = append(work, f.JobKey)
	if len(work) != 44 || !reflect.DeepEqual(work, f.WorkKeys) {
		t.Fatal("WORK key order mismatch")
	}
	scopes := []string{string(decision.GlobalScopeID), string(decision.GroupScopeID), string(decision.OriginScopeID)}
	if !reflect.DeepEqual(scopes, f.Scopes) {
		t.Fatal("scope order mismatch")
	}
	keys := append([]string(nil), work...)
	for _, id := range reservations {
		keys = append(keys, "mifolyo:crawl:v2:reservation:"+id)
	}
	for _, id := range scopes {
		for _, suffix := range []string{"", ":active", ":pending", ":started"} {
			keys = append(keys, "mifolyo:crawl:v2:rate:"+id+suffix)
		}
	}
	sort.Strings(keys)
	if len(keys) != 58 || !reflect.DeepEqual(keys, f.Inventory) {
		t.Fatal("complete derived-key union mismatch")
	}
	for _, key := range keys {
		_, present := f.Initial[key]
		if present != (key != DurabilityKey) {
			t.Fatal("fixture ownership mismatch")
		}
	}
}

func m4ClaimLoad(t *testing.T, r *sharedLuaRedis, key string, entry *m4ClaimEntry) {
	t.Helper()
	if entry == nil {
		return
	}
	if entry.Expiry != -1 {
		t.Fatal("initial fixture has an expiry")
	}
	switch entry.Type {
	case "hash":
		r.setHash(key, m4ClaimRecord(t, entry.Fields))
	case "string":
		r.data[key] = bootLuaEntry{kind: "string", value: entry.Value, expireAt: -1}
	case "set":
		var members []string
		m4ClaimMembers(t, entry.Members, &members)
		r.setSet(key, members)
	case "zset":
		var rows [][]string
		m4ClaimMembers(t, entry.Members, &rows)
		scores := map[string]float64{}
		for _, row := range rows {
			if len(row) != 2 {
				t.Fatal("invalid zset member")
			}
			score, err := strconv.ParseFloat(row[1], 64)
			if err != nil {
				t.Fatal("invalid zset score")
			}
			scores[row[0]] = score
		}
		r.setZSet(key, scores)
	default:
		t.Fatal("unknown fixture type")
	}
}

func m4ClaimState(t *testing.T, r *sharedLuaRedis, f m4ClaimFixture, expected map[string]*m4ClaimEntry, boot bootLuaEntry, step int) {
	t.Helper()
	if len(expected) != 57 || !reflect.DeepEqual(r.data[DurabilityKey], boot) {
		t.Fatal("state ownership/BOOT mutation")
	}
	count := 1
	for key, want := range expected {
		got, exists := r.data[key]
		if want == nil {
			if exists {
				t.Fatalf("unexpected key at step %d", step)
			}
			continue
		}
		count++
		if !exists || got.kind != want.Type || got.expireAt != want.Expiry {
			t.Fatalf("type/expiry oracle differs at step %d", step)
		}
		switch want.Type {
		case "hash":
			record := m4ClaimRecord(t, want.Fields)
			fields := map[string]string{}
			for _, field := range record {
				fields[field.Name] = string(field.Value)
			}
			if !reflect.DeepEqual(fields, got.hash) {
				t.Fatalf("hash state oracle differs at step %d for %s", step, key)
			}
			var schema RecordSchema
			switch {
			case key == f.BaseKey:
				schema = SchemaRun
			case key == f.JobKey:
				schema = SchemaJob
			case strings.HasPrefix(key, "mifolyo:crawl:v2:reservation:"):
				schema = SchemaReservation
			case strings.HasPrefix(key, "mifolyo:crawl:v2:rate:"):
				schema = SchemaRateScope
			}
			if schema != "" && ValidateRecord(schema, record) != nil {
				t.Fatalf("Go rejected %s at step %d", schema, step)
			}
		case "string":
			if want.Value != got.value {
				t.Fatal("string state mismatch")
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
				t.Fatal("set membership mismatch")
			}
		case "zset":
			var members [][]string
			m4ClaimMembers(t, want.Members, &members)
			actual := [][]string{}
			for _, member := range r.orderedZSet(key) {
				actual = append(actual, []string{member, strconv.FormatFloat(r.zsets[key][member], 'f', -1, 64)})
			}
			if !reflect.DeepEqual(actual, members) {
				t.Fatalf("zset oracle differs at step %d", step)
			}
		}
	}
	if count != len(r.data) {
		t.Fatal("unexpected key outside full fixture inventory")
	}
}
