package crawljobsv2

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

type m4NegativeWire struct {
	Parts []string `json:"parts_hex"`
	Size  uint64   `json:"resp_size"`
}

type m4NegativeVector struct {
	Case    string `json:"case"`
	Fixture struct {
		Worker    m4ClaimFixture `json:"worker"`
		Inventory []string       `json:"key_inventory"`
	} `json:"fixture"`
	Initial       map[string]*m4ClaimEntry `json:"initial"`
	ACL           map[string][]string      `json:"acl_rules"`
	PositiveClaim m4NegativeWire           `json:"positive_claim_wire"`
	PositiveBoot  m4NegativeWire           `json:"positive_boot_wire"`
	Admin         struct {
		Core   string `json:"core_hex"`
		Marker string `json:"marker_hex"`
	} `json:"admin_artifacts"`
	Steps []struct {
		Operation OperationName            `json:"operation"`
		Actor     string                   `json:"actor"`
		Wire      m4NegativeWire           `json:"wire"`
		At        uint64                   `json:"at_ms"`
		Error     string                   `json:"error"`
		Reply     []string                 `json:"reply"`
		State     map[string]*m4ClaimEntry `json:"state"`
	} `json:"steps"`
}

// Four roots distribute the new conformance work through the exhaustive CI
// shards. These are unchanged canonical Lua executions in a command facade,
// not target Redis ACL, isolation, allocator or AOF acceptance.
func TestM4NegativeStoredStateOffline(t *testing.T)  { m4NegativeGroup(t, "stored") }
func TestM4NegativeWireOffline(t *testing.T)         { m4NegativeGroup(t, "wire") }
func TestM4NegativeBootOffline(t *testing.T)         { m4NegativeGroup(t, "boot") }
func TestM4AdministrativeDenialOffline(t *testing.T) { m4NegativeGroup(t, "admin") }

func m4NegativeParts(t *testing.T, wire m4NegativeWire) (parts, keys, args []string) {
	t.Helper()
	for _, encoded := range wire.Parts {
		raw, err := hex.DecodeString(encoded)
		if err != nil {
			t.Fatal("invalid vector framing")
		}
		parts = append(parts, string(raw))
	}
	if len(parts) < 4 || parts[0] != "EVALSHA" {
		t.Fatal("invalid EVALSHA framing")
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil || n < 1 || n > 71 || len(parts) < 3+n {
		t.Fatal("invalid declared keys")
	}
	size := len("*" + strconv.Itoa(len(parts)) + "\r\n")
	for _, part := range parts {
		size += len("$"+strconv.Itoa(len(part))+"\r\n") + len(part) + 2
	}
	if uint64(size) != wire.Size || size > 2*1024*1024 {
		t.Fatal("independent RESP bound mismatch")
	}
	return parts, parts[3 : 3+n], parts[3+n:]
}

func m4NegativeBuilt(t *testing.T, bundle ScriptBindingSet, request OperationWireRequest, err error, wire m4NegativeWire) {
	t.Helper()
	if err != nil {
		t.Fatal("independent Go constructor rejected positive control", err)
	}
	built, err := BuildEvalSHARequest(bundle, request)
	if err != nil {
		t.Fatal(err)
	}
	parts, _, _ := m4NegativeParts(t, wire)
	want := [][]byte{[]byte("EVALSHA"), []byte(built.ScriptSHA1()), []byte(strconv.Itoa(len(built.Keys())))}
	want = append(want, built.Keys()...)
	want = append(want, built.Arguments()...)
	if len(parts) != len(want) || wire.Size != built.SerializedSize() {
		t.Fatal("independent Go wire shape mismatch")
	}
	for i, part := range want {
		if !bytes.Equal(part, []byte(parts[i])) {
			t.Fatalf("independent wire mismatch at part %d", i)
		}
	}
}

func m4NegativeClaim(t *testing.T, bundle ScriptBindingSet, vector m4NegativeVector) {
	t.Helper()
	f := vector.Fixture.Worker
	runID, jobID := RunID(f.Inputs.FixtureID), JobID(utils.URLIDV1("https://m4-fixture.invalid/document"))
	lineage := RateScopeID(digestFramed("mifolyo:m4:claim-release:lineage:v1", []byte(runID))[:32])
	groupScope, _ := DeriveGroupScopeID(lineage)
	group := PolicyGroup{GroupID: "fixture", RateScopeID: lineage, GroupScopeID: groupScope, RequestStartLimit: 10, Concurrency: 1, IntervalMS: 0}
	document, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestDocument,
		Target: RequestTarget{URLID: jobID, CanonicalURL: "https://m4-fixture.invalid/document"}, Depth: 1,
		GroupID: group.GroupID, RateScopeID: lineage, GroupConcurrency: 1, OriginConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	target := RequestTarget{URLID: JobID(utils.URLIDV1("https://m4-fixture.invalid/robots.txt")), CanonicalURL: "https://m4-fixture.invalid/robots.txt"}
	robots, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestRobots, Target: target, Depth: 1,
		GroupID: group.GroupID, RateScopeID: lineage, GroupConcurrency: 1, OriginConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	runRecord := m4ClaimRecord(t, f.Initial[f.BaseKey].Fields)
	policy, err := (transportAuthority{seal: &redisTransportAuthoritySeal}).parseRunPolicyAuthority(runID, runRecord, []PolicyGroup{group})
	if err != nil {
		t.Fatal(err)
	}
	encode := func(key string) []byte {
		raw, e := EncodeRecord(m4ClaimRecord(t, f.Initial[key].Fields))
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	marker, e1 := DecodeCompatibilityMarker(encode(ContractsActiveKey))
	guard, e2 := DecodeStoredCommitGuard(encode(CommitGuardKey))
	legacy, e3 := DecodeLegacyRetirementRecord(encode(LegacyRetirementKey))
	if e1 != nil || e2 != nil || e3 != nil {
		t.Fatal("independent gate codecs reject fixture")
	}
	contract, _ := ContractSHA256()
	gate, err := NewTransportGate(OperationTryClaim, TransportGateInput{Mode: GateActive, BootEpoch: strings.Repeat("7", 32),
		Contract: contract, Compatibility: &marker, CommitGuard: &guard, Legacy: &legacy})
	if err != nil {
		t.Fatal(err)
	}
	lease := LeaseIdentity{RunID: runID, JobID: jobID, OwnerID: OwnerID(f.Inputs.OwnerA), Token: LeaseToken(f.Inputs.TokenA), Fence: 1}
	policyRecord := map[string]string{}
	for _, field := range runRecord {
		policyRecord[field.Name] = string(field.Value)
	}
	intent := ReservationIntent{Lease: lease, RequestOrdinal: 1, Target: target, CrawlPolicyDigest: Digest(policyRecord["crawl_policy_sha256"]), Decision: robots}
	request, err := NewTryClaimWireRequest(gate, policy, TryClaimTransitionInput{
		Job: SourceJob{JobID: jobID, CanonicalURL: "https://m4-fixture.invalid/document", ScoreText: "0", Depth: 1,
			GroupID: group.GroupID, RateScopeID: lineage, Decision: document}, Lease: lease, ExpectedPriorFence: 0, InitialIntent: intent})
	m4NegativeBuilt(t, bundle, request, err, vector.PositiveClaim)
	reservations := []string{f.Identities["a"].Reservation, f.Identities["b"].Reservation}
	m4ClaimInventory(t, f, robots, reservations)
}

func m4NegativeStoredRecord(t *testing.T, r *sharedLuaRedis, key string, schema RecordSchema) []byte {
	t.Helper()
	record := adminLuaRecord(t, r, key, schema)
	raw, err := EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func m4NegativeAdminWire(t *testing.T, bundle ScriptBindingSet, vector m4NegativeVector, r *sharedLuaRedis, op OperationName, wire m4NegativeWire) {
	t.Helper()
	_, _, args := m4NegativeParts(t, wire)
	decode := func(value string) []byte {
		raw, err := hex.DecodeString(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	marker, err := DecodeCompatibilityMarker(decode(vector.Admin.Marker))
	if err != nil {
		t.Fatal(err)
	}
	contract, _ := ContractSHA256()
	input := TransportGateInput{Mode: GateBootOnly, BootEpoch: strings.Repeat("7", 32)}
	if op != OperationInstallCandidateMarkers {
		stored, err := DecodeCompatibilityMarker(m4NegativeStoredRecord(t, r, ContractsCandidateKey, SchemaCompatibilityMarker))
		if err != nil {
			t.Fatal(err)
		}
		freeze, err := DecodeAdminFreezeRecord(m4NegativeStoredRecord(t, r, AdminFreezeKey, SchemaAdminFreeze))
		if err != nil {
			t.Fatal(err)
		}
		input.Mode, input.Contract, input.Compatibility, input.AdminFreeze = GateCandidate, contract, &stored, &freeze
		input.CandidatePhase = CandidateBeforeLegacyRetirement
		if op == OperationPromoteCandidateContracts {
			legacy, err := DecodeLegacyRetirementRecord(m4NegativeStoredRecord(t, r, LegacyRetirementKey, SchemaLegacyRetirement))
			if err != nil {
				t.Fatal(err)
			}
			input.Legacy, input.CandidatePhase = &legacy, CandidateAfterLegacyRetirement
		}
	}
	gate, err := NewTransportGate(op, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) < 8 {
		t.Fatal("missing admin semantic arguments")
	}
	nonceHash := sha256.Sum256([]byte(vector.Case + ":" + vector.Fixture.Worker.Inputs.FixtureID + ":freeze"))
	nonce := hex.EncodeToString(nonceHash[:])[:32]
	var request OperationWireRequest
	switch op {
	case OperationInstallCandidateMarkers:
		request, err = NewInstallCandidateMarkersWireRequest(gate, InstallCandidateMarkersWireInput{
			FreezeNonce: nonce, ProcessStopEvidenceSHA256: Digest(args[8]), ContractSHA256: contract, Compatibility: marker})
	case OperationRetireLegacyKeys:
		if len(args) != 23 {
			t.Fatal("RETIRE argument inventory")
		}
		a := args[7:]
		request, err = NewRetireLegacyKeysWireRequest(gate, RetireLegacyKeysWireInput{FreezeNonce: nonce,
			BackupSHA256: Digest(a[1]), V1Count: 0, V1URLFieldCount: 0, V1DepthFieldCount: 0, V1SourceSHA256: Digest(a[5]),
			V1QueueEvidenceSHA256: Digest(a[6]), V1URLsEvidenceSHA256: Digest(a[7]), V1DepthsEvidenceSHA256: Digest(a[8]),
			SpiderQueueType: LegacyRedisType("none"), SpiderQueueCount: 0, SpiderQueueEvidenceSHA256: Digest(a[11]),
			SignalQueueType: LegacyRedisType("none"), SignalQueueCount: 0, SignalQueueEvidenceSHA256: Digest(a[14])})
	case OperationPromoteCandidateContracts:
		core, e := DecodeGuardCore(decode(vector.Admin.Core))
		if e != nil {
			t.Fatal(e)
		}
		request, err = NewPromoteCandidateContractsWireRequest(gate, PromoteCandidateContractsWireInput{FreezeNonce: nonce, GuardCore: core})
	default:
		t.Fatal("unexpected admin operation")
	}
	m4NegativeBuilt(t, bundle, request, err, wire)
}

func m4NegativeState(t *testing.T, r *sharedLuaRedis, expected map[string]*m4ClaimEntry) {
	t.Helper()
	present := 0
	for key, want := range expected {
		got, exists := r.data[key]
		if want == nil {
			if exists {
				t.Fatal("unexpected key in complete negative-state inventory", key)
			}
			continue
		}
		present++
		if !exists || got.kind != want.Type || got.expireAt != want.Expiry {
			t.Fatal("negative type/expiry differs", key)
		}
		switch want.Type {
		case "hash":
			fields := map[string]string{}
			for _, field := range m4ClaimRecord(t, want.Fields) {
				fields[field.Name] = string(field.Value)
			}
			if !reflect.DeepEqual(fields, got.hash) {
				t.Fatal("negative hash differs", key)
			}
		case "string":
			if want.Value != got.value {
				t.Fatal("negative string differs", key)
			}
		case "set":
			var wanted []string
			m4ClaimMembers(t, want.Members, &wanted)
			actual := []string{}
			for member := range r.sets[key] {
				actual = append(actual, member)
			}
			sort.Strings(actual)
			if !reflect.DeepEqual(actual, wanted) {
				t.Fatal("negative set differs", key)
			}
		case "zset":
			var wanted [][]string
			m4ClaimMembers(t, want.Members, &wanted)
			actual := [][]string{}
			for _, member := range r.orderedZSet(key) {
				actual = append(actual, []string{member, strconv.FormatFloat(r.zsets[key][member], 'f', -1, 64)})
			}
			if !reflect.DeepEqual(actual, wanted) {
				t.Fatal("negative index differs", key)
			}
		default:
			t.Fatal("unlisted expected type")
		}
	}
	if present != len(r.data) {
		t.Fatal("unlisted state outside negative fixture")
	}
}

func m4NegativeCodes(t *testing.T, group, name string) []string {
	t.Helper()
	stored := map[string]string{
		"ledger-candidate-compat-present-v1": "INVALID_STATE", "ledger-candidate-contract-present-v1": "INVALID_STATE",
		"ledger-admin-freeze-present-v1": "INVALID_STATE", "ledger-active-compat-missing-v1": "COMPATIBILITY_MISMATCH",
		"ledger-active-contract-wrong-type-v1": "WRONG_TYPE", "ledger-active-contract-mismatch-v1": "CONTRACT_MISMATCH",
		"ledger-guard-mismatch-v1": "IMMUTABLE_MISMATCH", "ledger-active-compat-extra-field-v1": "INVALID_STATE"}
	var codes []string
	switch group {
	case "stored":
		code, ok := stored[name]
		if !ok {
			t.Fatal("unknown stored-state case")
		}
		codes = []string{code}
	case "wire":
		if name != "ledger-wire-negatives-v1" {
			t.Fatal("wrong wire case")
		}
		codes = strings.Fields("INVALID_ARGUMENT BOOT_UNAPPROVED INVALID_ARGUMENT COMPATIBILITY_MISMATCH INVALID_ARGUMENT INVALID_ARGUMENT INVALID_ARGUMENT INVALID_ARGUMENT CONTRACT_MISMATCH INVALID_ARGUMENT INVALID_ARGUMENT INVALID_ARGUMENT")
	case "boot":
		if name != "bootstrap-rejections-v1" {
			t.Fatal("wrong bootstrap case")
		}
		codes = strings.Fields("INVALID_ARGUMENT INVALID_IDENTIFIER INVALID_ARGUMENT BOOT_UNAPPROVED BOOT_UNAPPROVED BOOT_UNAPPROVED INVALID_IDENTIFIER INVALID_ARGUMENT INVALID_ARGUMENT")
	case "admin":
		switch name {
		case "ledger-install-denied-v1":
			return []string{"CRAWL_V2_BOOT_UNAPPROVED", "CRAWL_V2_BOOT_UNAPPROVED", ""}
		case "ledger-retire-denied-v1":
			return []string{"", "NOPERM", "NOPERM", ""}
		case "ledger-promote-denied-v1":
			return []string{"", "", "NOPERM", "NOPERM", ""}
		default:
			t.Fatal("wrong admin case")
		}
	}
	result := []string{}
	for _, code := range codes {
		result = append(result, "CRAWL_V2_"+code, "CRAWL_V2_"+code)
	}
	if group == "wire" || group == "boot" {
		result = append(result, "", "")
	}
	return result
}

func m4NegativeGroup(t *testing.T, group string) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	path := filepath.Join(fixtureRepositoryRoot(t), "tests/crawl-jobs-v2-redis/test_negative_cases.py")
	raw, err := exec.CommandContext(ctx, "python3", "-B", path, "--go-vectors", group).Output()
	if err != nil || len(raw) > 2*1024*1024 {
		t.Fatal("bounded offline negative vector generation failed", err)
	}
	var payload struct {
		Purpose    string             `json:"purpose"`
		Authorized bool               `json:"execution_authorized"`
		Vectors    []m4NegativeVector `json:"vectors"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Purpose != "offline_public_test_vectors" || payload.Authorized ||
		len(payload.Vectors) != map[string]int{"stored": 8, "wire": 1, "boot": 1, "admin": 3}[group] {
		t.Fatal("negative vector inventory")
	}
	bundle, err := AuthoritativeScriptBindingSet()
	if err != nil {
		t.Fatal(err)
	}
	sources, sha1s := map[OperationName]string{}, map[OperationName]string{}
	for _, binding := range bundle.bindings {
		sources[binding.operation], sha1s[binding.operation] = binding.source, binding.redisSHA1
	}
	seen := map[string]bool{}
	for _, vector := range payload.Vectors {
		if seen[vector.Case] {
			t.Fatal("duplicate negative case")
		}
		seen[vector.Case] = true
		t.Run(vector.Case, func(t *testing.T) {
			codes := m4NegativeCodes(t, group, vector.Case)
			if len(vector.Steps) != len(codes) {
				t.Fatal("missing negative/positive control")
			}
			r := &sharedLuaRedis{data: map[string]bootLuaEntry{}, sets: map[string]map[string]bool{}, zsets: map[string]map[string]float64{},
				lists: map[string][]string{}, runID: strings.Repeat("b", 40), used: 1000000, maximum: 400 * 1024 * 1024}
			for key, entry := range vector.Initial {
				m4ClaimLoad(t, r, key, entry)
			}
			if group == "stored" || group == "wire" {
				m4NegativeClaim(t, bundle, vector)
			}
			var boot *bootLuaRedis
			if group == "boot" {
				boot = bootLuaNewRedis()
				boot.data = map[string]bootLuaEntry{}
				boot.runID = r.runID
				_, _, args := m4NegativeParts(t, vector.PositiveBoot)
				at, err := strconv.ParseUint(args[3], 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				request, err := NewApproveBootWireRequest(ApproveBootWireInput{CurrentRedisRunID: r.runID, ProposedBootEpoch: strings.Repeat("7", 32),
					EvidenceSHA256: Digest(args[2]), EvidenceAtMS: at, ApprovalMode: ApprovalInitial})
				m4NegativeBuilt(t, bundle, request, err, vector.PositiveBoot)
				if sources[OperationApproveBoot] != bootLuaSource {
					t.Fatal("BOOT facade does not use canonical bytes")
				}
			}
			for index, step := range vector.Steps {
				parts, keys, args := m4NegativeParts(t, step.Wire)
				if parts[1] != sha1s[step.Operation] || step.Error != codes[index] {
					t.Fatal("source/rejection inventory substitution")
				}
				if group == "admin" {
					m4NegativeAdminWire(t, bundle, vector, r, step.Operation, step.Wire)
				}
				before, writes := r.snapshot(), r.writes
				allowed := m4ClaimACLAllows(vector.ACL[step.Actor], "EVALSHA", keys)
				if step.Error == "NOPERM" {
					// Offline selector proof ONLY: Redis outer EVALSHA admission
					// itself must still be measured on the reviewed target image.
					if allowed || step.Actor != "ledger" {
						t.Fatal("outer admin key denial is vacuous")
					}
					m4NegativeState(t, r, step.State)
					continue
				}
				if !allowed {
					t.Fatal("outer denial would mask intended canonical rejection")
				}
				var result bootLuaResult
				if boot != nil {
					boot.setTime(step.At)
					result = bootLuaRun(t, boot, keys, args)
					r.data, r.writes = boot.data, boot.writes
				} else {
					r.now, r.denyAt = step.At, 0
					if group == "admin" && step.Actor == "ledger" {
						r.denyAt = 1
					}
					result = workerLuaRun(t, r, sources[step.Operation], keys, args)
					for _, call := range r.trace {
						command, touched := call.name, []string{}
						if command == "INFO" {
							command += "|" + call.args[0]
						} else if command != "TIME" {
							touched = []string{call.args[0]}
							if command == "RENAME" {
								touched = append(touched, call.args[1])
							}
						}
						permitted := m4ClaimACLAllows(vector.ACL[step.Actor], command, touched)
						if call.acl && r.denyAt == 1 {
							if permitted || r.aclCount != 1 || r.attempts != 0 {
								t.Fatal("INSTALL denial missed actual authority write")
							}
						} else if !permitted {
							t.Fatal("ACL misses canonical command", command)
						}
					}
				}
				if result.runtimeErr != nil {
					t.Fatal("canonical Lua runtime error", result.runtimeErr)
				}
				if step.Error != "" {
					if !reflect.DeepEqual(result.raw, bootLuaErrorReply("ERR "+step.Error)) || r.writes != writes || !reflect.DeepEqual(before, r.snapshot()) {
						t.Fatalf("wrong rejection or state mutation at step %d: %v", index, result.raw)
					}
				} else {
					want := make([]any, len(step.Reply))
					for i, value := range step.Reply {
						want[i] = value
					}
					if !reflect.DeepEqual(result.raw, want) || ValidateOperationResponse(step.Operation, result.raw) != nil {
						t.Fatal("positive response oracle differs")
					}
				}
				m4NegativeState(t, r, step.State)
			}
		})
	}
}
