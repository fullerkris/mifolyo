package crawljobsv2

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Independent production codecs and the literal wire oracle consume Python's
// test-only compiler output. This starts neither Redis nor a runtime service.
func TestM4OfflineArtifacts(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../.."))
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "python3", "-B", filepath.Join(root, "tests/crawl-jobs-v2-redis/test_harness.py"), "--go-vectors")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("offline Python artifact compiler: %v", err)
	}
	var vectors struct {
		Cases []struct {
			Scenario       string   `json:"scenario"`
			CoreHex        string   `json:"core_hex"`
			MarkerHex      string   `json:"marker_hex"`
			CoreSHA256     Digest   `json:"core_sha256"`
			ManifestSHA256 Digest   `json:"manifest_sha256"`
			StoredGuardHex string   `json:"stored_guard_hex"`
			LegacyHex      string   `json:"legacy_hex"`
			ActivePartsHex []string `json:"active_parts_hex"`
			ActiveRESPSize uint64   `json:"active_resp_size"`
			BootPartsHex   []string `json:"boot_parts_hex"`
			BootRESPSize   uint64   `json:"boot_resp_size"`
			Variants       []struct {
				Operation OperationName `json:"operation"`
				Gate      string        `json:"gate"`
				Status    string        `json:"execution_status"`
			} `json:"variants"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(output, &vectors); err != nil || len(vectors.Cases) != 3 {
		t.Fatal("invalid offline artifact inventory")
	}
	contract, err := ContractSHA256()
	if err != nil {
		t.Fatal(err)
	}
	decode := func(value string) []byte {
		t.Helper()
		raw, err := hex.DecodeString(value)
		if err != nil {
			t.Fatal("invalid fixture hex")
		}
		return raw
	}
	variantNames := map[wireOracleGateVariant]string{
		wireOracleUngated: "ungated", wireOracleBootOnly: "boot_only", wireOracleActive: "active",
		wireOracleCandidateBeforeRetirement: "candidate_before_retirement",
		wireOracleCandidateAfterRetirement:  "candidate_after_retirement",
	}
	for _, test := range vectors.Cases {
		t.Run(test.Scenario, func(t *testing.T) {
			core, err := DecodeGuardCore(decode(test.CoreHex))
			if err != nil || core.ContractSHA256() != contract {
				t.Fatal("Go rejected current-contract nonzero test core")
			}
			if sum, err := core.SHA256(); err != nil || sum != test.CoreSHA256 {
				t.Fatal("Go/Python guard identity differs")
			}
			marker, err := DecodeCompatibilityMarker(decode(test.MarkerHex))
			if err != nil {
				t.Fatal(err)
			}
			if sum, err := marker.ManifestSHA256(); err != nil || sum != test.ManifestSHA256 {
				t.Fatal("Go/Python compatibility identity differs")
			}
			artifact, err := marker.Artifact()
			if err != nil || artifact.CommitGuardDigest() != test.CoreSHA256 {
				t.Fatal("compatibility does not bind the test core")
			}
			if _, err := DecodeProvisionalGuardCore(decode(test.CoreHex)); err == nil {
				t.Fatal("nonzero test core entered historical zero-sentinel control")
			}
			for _, field := range []string{"maximum_shape_sha256", "memory_fixture_sha256", "lua_benchmark_sha256", "aof_crash_evidence_sha256"} {
				record, _ := core.Record()
				for i := range record {
					if record[i].Name == field {
						record[i].Value = []byte(ZeroSHA256)
					}
				}
				raw, err := encodeBoundedRecord(record, maxSmallAuthorityRecordBytes)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := DecodeGuardCore(raw); err == nil {
					t.Fatal("zero evidence accepted by normal codec")
				}
			}
			if test.Scenario == "ledger-smoke" {
				guard, err := DecodeStoredCommitGuard(decode(test.StoredGuardHex))
				if err != nil {
					t.Fatal(err)
				}
				legacy, err := DecodeLegacyRetirementRecord(decode(test.LegacyHex))
				if err != nil {
					t.Fatal(err)
				}
				gate, err := NewTransportGate(OperationMaintainRateScopes, TransportGateInput{
					Mode: GateActive, BootEpoch: strings.Repeat("1", 32), Contract: contract,
					Compatibility: &marker, CommitGuard: &guard, Legacy: &legacy,
				})
				if err != nil {
					t.Fatalf("normal transport rejects offline ledger projection: %v", err)
				}
				bundle, err := AuthoritativeScriptBindingSet()
				if err != nil {
					t.Fatal(err)
				}
				active, err := NewMaintainRateScopesWireRequest(gate, 0)
				if err != nil {
					t.Fatal(err)
				}
				boot, err := NewApproveBootWireRequest(ApproveBootWireInput{
					CurrentRedisRunID: strings.Repeat("b", 40), ProposedBootEpoch: strings.Repeat("1", 32),
					EvidenceSHA256: Digest(strings.Repeat("2", 64)), EvidenceAtMS: 1000, ApprovalMode: ApprovalInitial,
				})
				if err != nil {
					t.Fatal(err)
				}
				for _, wire := range []struct {
					request OperationWireRequest
					parts   []string
					size    uint64
				}{{active, test.ActivePartsHex, test.ActiveRESPSize}, {boot, test.BootPartsHex, test.BootRESPSize}} {
					built, err := BuildEvalSHARequest(bundle, wire.request)
					if err != nil {
						t.Fatal(err)
					}
					parts := [][]byte{[]byte("EVALSHA"), []byte(built.ScriptSHA1()), []byte(strconv.Itoa(len(built.Keys())))}
					parts = append(parts, built.Keys()...)
					parts = append(parts, built.Arguments()...)
					actual := make([][]byte, len(wire.parts))
					for i, part := range wire.parts {
						actual[i] = decode(part)
					}
					if len(actual) != len(parts) || built.SerializedSize() != wire.size {
						t.Fatal("execution recipe differs from Go EVALSHA wire/RESP oracle")
					}
					for i := range parts {
						// nil and empty both encode the required zero-byte bulk string.
						if !bytes.Equal(actual[i], parts[i]) {
							t.Fatalf("execution wire mismatch for %s at part %d", built.Operation(), i)
						}
					}
				}
			}
			expected := make(map[string]bool)
			for _, operation := range wireOracleOperationExpectations() {
				for _, variant := range operation.variants {
					expected[string(operation.operation)+"/"+variantNames[variant]] = true
				}
			}
			if len(test.Variants) != 52 || len(expected) != 52 {
				t.Fatal("wrong gate inventory size")
			}
			for _, variant := range test.Variants {
				key := string(variant.Operation) + "/" + variant.Gate
				if !expected[key] || variant.Status != "not_run" {
					t.Fatal("invented gate, duplicate entry or false acceptance")
				}
				delete(expected, key)
			}
			if len(expected) != 0 {
				t.Fatal("missing gate variants")
			}
		})
	}
}
