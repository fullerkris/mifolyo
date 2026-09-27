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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

// Python projects complete states independently; Go constructs each wire and
// executes unchanged embedded canonical Lua against an in-memory command facade.
// Advancing this facade's clock is NOT elapsed Redis time or process-death evidence.
func TestM4RecoveryOracleOffline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	path := filepath.Join(fixtureRepositoryRoot(t), "tests/crawl-jobs-v2-redis/test_recovery_oracle.py")
	raw, err := exec.CommandContext(ctx, "python3", "-B", path, "--go-vectors").Output()
	if err != nil || len(raw) > 2*1024*1024 {
		t.Fatal("offline recovery vector generation failed or exceeded bound")
	}
	var packet struct {
		Purpose    string `json:"purpose"`
		Authorized bool   `json:"execution_authorized"`
		Vectors    []struct {
			Fixture    m4ClaimFixture      `json:"fixture"`
			Times      []uint64            `json:"times"`
			Operations []OperationName     `json:"operations"`
			ACLRules   map[string][]string `json:"acl_rules"`
			Expected   []struct {
				ID        string                   `json:"assertion_id"`
				Operation OperationName            `json:"operation"`
				Reply     []string                 `json:"reply"`
				State     map[string]*m4ClaimEntry `json:"state"`
			} `json:"expected"`
			Wires []struct {
				Parts []string `json:"parts_hex"`
				Size  uint64   `json:"resp_size"`
			} `json:"wires"`
		} `json:"vectors"`
	}
	if json.Unmarshal(raw, &packet) != nil || packet.Purpose != "offline_public_recovery_vectors" || packet.Authorized || len(packet.Vectors) != 2 {
		t.Fatal("invalid offline recovery packet")
	}
	ops := []OperationName{OperationTryClaim, OperationRecoverExpired, OperationRecoverExpired, OperationRecoverExpired,
		OperationTryClaim, OperationTryClaim, OperationTryClaim, OperationReleaseBeforeIO, OperationRenewLease,
		OperationRenewLease, OperationReleaseBeforeIO, OperationReleaseBeforeIO, OperationRecoverExpired}
	bundle, err := AuthoritativeScriptBindingSet()
	if err != nil {
		t.Fatal(err)
	}
	contract, _ := ContractSHA256()
	for vectorIndex, vector := range packet.Vectors {
		t.Run(strconv.Itoa(vectorIndex), func(t *testing.T) {
			f := vector.Fixture
			if f.Case != "ledger-claim-release-v1" || f.Authorized || f.Measured != "not_measured" ||
				len(vector.Times) != 13 || len(vector.Expected) != 13 || len(vector.Wires) != 13 ||
				!reflect.DeepEqual(vector.Operations, ops) || len(f.Initial) != 57 || len(f.Inventory) != 58 {
				t.Fatal("unexpected recovery vector scope")
			}
			if vector.Times[1] != vector.Times[0]+59999 || vector.Times[2] != vector.Times[0]+60000+uint64(vectorIndex) {
				t.Fatal("missing exact/beyond-expiry boundary control")
			}
			runID := RunID(f.Inputs.FixtureID)
			lineage := RateScopeID(digestFramed("mifolyo:m4:claim-release:lineage:v1", []byte(runID))[:32])
			groupScope, _ := DeriveGroupScopeID(lineage)
			group := PolicyGroup{GroupID: "fixture", RateScopeID: lineage, GroupScopeID: groupScope, RequestStartLimit: 10, Concurrency: 1}
			const document = "https://m4-fixture.invalid/document"
			const robots = "https://m4-fixture.invalid/robots.txt"
			jobID := JobID(utils.URLIDV1(document))
			documentDecision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestDocument,
				Target: RequestTarget{URLID: jobID, CanonicalURL: document}, Depth: 1, GroupID: "fixture", RateScopeID: lineage, GroupConcurrency: 1, OriginConcurrency: 1})
			if err != nil {
				t.Fatal(err)
			}
			robotsTarget := RequestTarget{URLID: JobID(utils.URLIDV1(robots)), CanonicalURL: robots}
			robotsDecision, err := NewPolicyDecision(PolicyDecisionInput{RequestKind: RequestRobots, Target: robotsTarget,
				Depth: 1, GroupID: "fixture", RateScopeID: lineage, GroupConcurrency: 1, OriginConcurrency: 1})
			if err != nil {
				t.Fatal(err)
			}
			source := SourceJob{JobID: jobID, CanonicalURL: document, ScoreText: "0", Depth: 1, GroupID: "fixture", RateScopeID: lineage, Decision: documentDecision}
			runRecord := m4ClaimRecord(t, f.Initial[f.BaseKey].Fields)
			policy, err := (transportAuthority{seal: &redisTransportAuthoritySeal}).parseRunPolicyAuthority(runID, runRecord, []PolicyGroup{group})
			if err != nil {
				t.Fatal(err)
			}
			run := map[string]string{}
			for _, field := range runRecord {
				run[field.Name] = string(field.Value)
			}
			markerBytes, _ := EncodeRecord(m4ClaimRecord(t, f.Initial[ContractsActiveKey].Fields))
			guardBytes, _ := EncodeRecord(m4ClaimRecord(t, f.Initial[CommitGuardKey].Fields))
			legacyBytes, _ := EncodeRecord(m4ClaimRecord(t, f.Initial[LegacyRetirementKey].Fields))
			marker, e1 := DecodeCompatibilityMarker(markerBytes)
			guard, e2 := DecodeStoredCommitGuard(guardBytes)
			legacy, e3 := DecodeLegacyRetirementRecord(legacyBytes)
			if e1 != nil || e2 != nil || e3 != nil {
				t.Fatal("invalid gate control")
			}
			gateInput := TransportGateInput{Mode: GateActive, BootEpoch: strings.Repeat("7", 32), Contract: contract,
				Compatibility: &marker, CommitGuard: &guard, Legacy: &legacy}
			leases := []LeaseIdentity{
				{RunID: runID, JobID: jobID, OwnerID: OwnerID(f.Inputs.OwnerA), Token: LeaseToken(f.Inputs.TokenA), Fence: 1},
				{RunID: runID, JobID: jobID, OwnerID: OwnerID(f.Inputs.OwnerB), Token: LeaseToken(f.Inputs.TokenB), Fence: 2},
			}
			claims := make([]TryClaimTransitionInput, 2)
			for i := range leases {
				intent := ReservationIntent{Lease: leases[i], RequestOrdinal: uint64(i + 1), Target: robotsTarget,
					CrawlPolicyDigest: Digest(run["crawl_policy_sha256"]), Decision: robotsDecision}
				claims[i] = TryClaimTransitionInput{Job: source, Lease: leases[i], ExpectedPriorFence: uint64(i), InitialIntent: intent}
			}
			r := &sharedLuaRedis{data: map[string]bootLuaEntry{}, sets: map[string]map[string]bool{}, zsets: map[string]map[string]float64{},
				lists: map[string][]string{}, now: f.Inputs.Time, runID: strings.Repeat("a", 40), used: 1000000, maximum: 400 * 1024 * 1024}
			for key, entry := range f.Initial {
				m4ClaimLoad(t, r, key, entry)
			}
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
			for i, op := range ops {
				gate, err := NewTransportGate(op, gateInput)
				if err != nil {
					t.Fatal(err)
				}
				who := 0
				if i == 4 || i == 5 || i == 9 || i == 10 || i == 11 {
					who = 1
				}
				var request OperationWireRequest
				switch op {
				case OperationTryClaim:
					request, err = NewTryClaimWireRequest(gate, policy, claims[who])
				case OperationRecoverExpired:
					request, err = NewRecoverExpiredWireRequest(gate, runID)
				case OperationRenewLease:
					request, err = NewRenewLeaseWireRequest(gate, leases[who])
				case OperationReleaseBeforeIO:
					request, err = NewReleaseBeforeIOWireRequest(gate, ReleaseBeforeIOTransitionInput{Lease: leases[who]})
				}
				if err != nil {
					t.Fatal(err)
				}
				built, err := BuildEvalSHARequest(bundle, request)
				if err != nil {
					t.Fatal(err)
				}
				parts := [][]byte{[]byte("EVALSHA"), []byte(built.ScriptSHA1()), []byte(strconv.Itoa(len(built.Keys())))}
				parts = append(parts, built.Keys()...)
				parts = append(parts, built.Arguments()...)
				wire := vector.Wires[i]
				if built.SerializedSize() != wire.Size || len(parts) != len(wire.Parts) || built.SerializedSize() > 65536 {
					t.Fatal("recovery wire size mismatch")
				}
				for n, part := range parts {
					want, err := hex.DecodeString(wire.Parts[n])
					if err != nil || !bytes.Equal(part, want) {
						t.Fatalf("wire mismatch at step %d part %d", i, n)
					}
				}
				keys, args := runLuaParts(t, request, nil)
				if !m4ClaimACLAllows(vector.ACLRules["ledger"], "EVALSHA", keys) {
					t.Fatal("proposed selector rejects outer recovery wire")
				}
				r.now = vector.Times[i]
				beforeWrites := r.writes
				var canonical string
				for _, binding := range bundle.bindings {
					if binding.operation == op {
						canonical = binding.source
					}
				}
				if canonical == "" {
					t.Fatal("canonical recovery source missing")
				}
				reply := sharedLuaNoError(t, workerLuaRun(t, r, canonical, keys, args)).([]any)
				want := make([]any, len(vector.Expected[i].Reply))
				for n, value := range vector.Expected[i].Reply {
					want[n] = value
				}
				responseErr := ValidateOperationResponse(op, reply)
				if op == OperationRenewLease && reply[0] == string(StatusRenewed) {
					if r.data[f.JobKey].hash["active_stage_commit_id"] != "" {
						t.Fatal("recovery control unexpectedly acquired a stage")
					}
					responseErr = ValidateRenewLeaseResponse(NewUnstagedRenewLeaseResponseContext(), reply)
				}
				if vector.Expected[i].ID != fmt.Sprintf("RCV%02d", i+1) || vector.Expected[i].Operation != op ||
					responseErr != nil || !reflect.DeepEqual(reply, want) {
					t.Fatalf("response oracle differs at step %d: got %v, want %v, schema error %v", i, reply, want, responseErr)
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
						t.Fatalf("proposed ACL lacks canonical %s at step %d", command, i)
					}
				}
				if (i == 1 || i == 3 || i == 5 || i == 6 || i == 7 || i == 11 || i == 12) && r.writes != beforeWrites {
					t.Fatalf("read-only recovery/replay step %d wrote", i)
				}
				m4ClaimState(t, r, f, vector.Expected[i].State, protectedBoot, i+1)
			}
		})
	}
}
